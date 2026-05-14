#!/usr/bin/env python3
"""Validate v2 visual evidence and run local browser sanity checks."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Literal
from urllib.parse import parse_qs, urljoin, urlparse


REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFEST = REPO_ROOT / "docs/operations/v2-visual-evidence/manifest.md"
EXPECTED_COLUMNS = [
    "Date",
    "Route",
    "Viewport",
    "Theme",
    "Data source",
    "File",
    "Verdict",
    "Notes",
]
VALID_VERDICTS = {"Needs review", "Accepted", "Rejected"}
VIEWPORT_RE = re.compile(r"^(?P<width>[1-9][0-9]*)x(?P<height>[1-9][0-9]*)$")
MockAPIProfile = Literal["none", "asset-workflows"]


@dataclass(frozen=True)
class ManifestRow:
    line_no: int
    date: str
    route: str
    viewport: str
    theme: str
    data_source: str
    file: str
    verdict: str
    notes: str


@dataclass(frozen=True)
class Viewport:
    label: str
    width: int
    height: int


def strip_code(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == "`" and value[-1] == "`":
        return value[1:-1].strip()
    return value


def split_markdown_row(line: str) -> list[str]:
    text = line.strip()
    if not text.startswith("|") or not text.endswith("|"):
        return []
    return [cell.strip() for cell in text.strip("|").split("|")]


def is_separator_row(cells: list[str]) -> bool:
    return bool(cells) and all(re.fullmatch(r":?-{3,}:?", cell) for cell in cells)


def parse_manifest(path: Path) -> tuple[list[str], list[ManifestRow], list[str]]:
    errors: list[str] = []
    rows: list[ManifestRow] = []
    lines = path.read_text(encoding="utf-8").splitlines()

    table_lines = [
        (line_no, line)
        for line_no, line in enumerate(lines, start=1)
        if line.strip().startswith("|")
    ]
    if len(table_lines) < 2:
        return [], [], [f"{path}: expected a markdown table with header and rows"]

    header_line_no, header_line = table_lines[0]
    header = split_markdown_row(header_line)
    if header != EXPECTED_COLUMNS:
        errors.append(
            f"{path}:{header_line_no}: expected columns {EXPECTED_COLUMNS}, got {header}"
        )

    separator_line_no, separator_line = table_lines[1]
    separator = split_markdown_row(separator_line)
    if not is_separator_row(separator):
        errors.append(f"{path}:{separator_line_no}: expected markdown separator row")

    for line_no, line in table_lines[2:]:
        cells = split_markdown_row(line)
        if len(cells) != len(EXPECTED_COLUMNS):
            errors.append(
                f"{path}:{line_no}: expected {len(EXPECTED_COLUMNS)} cells, got {len(cells)}"
            )
            continue
        rows.append(ManifestRow(line_no, *cells))

    return header, rows, errors


def validate_manifest(path: Path) -> list[str]:
    errors: list[str] = []
    if not path.exists():
        return [f"{path}: manifest does not exist"]

    _, rows, parse_errors = parse_manifest(path)
    errors.extend(parse_errors)
    evidence_root = path.parent.resolve()
    seen_files: dict[str, int] = {}

    if not rows:
        errors.append(f"{path}: no evidence rows found")

    for row in rows:
        location = f"{path}:{row.line_no}"

        try:
            dt.date.fromisoformat(row.date)
        except ValueError:
            errors.append(f"{location}: invalid Date {row.date!r}; use YYYY-MM-DD")

        route = strip_code(row.route)
        if not route.startswith("/"):
            errors.append(f"{location}: Route must start with '/', got {row.route!r}")

        if not VIEWPORT_RE.fullmatch(row.viewport):
            errors.append(
                f"{location}: Viewport must look like 1440x1000, got {row.viewport!r}"
            )

        if not row.theme:
            errors.append(f"{location}: Theme must not be empty")

        if not row.data_source:
            errors.append(f"{location}: Data source must not be empty")

        evidence_file = strip_code(row.file)
        if not evidence_file:
            errors.append(f"{location}: File must not be empty")
        elif evidence_file.startswith("/") or ".." in Path(evidence_file).parts:
            errors.append(f"{location}: File must be relative inside {evidence_root}")
        else:
            resolved = (evidence_root / evidence_file).resolve()
            try:
                resolved.relative_to(evidence_root)
            except ValueError:
                errors.append(f"{location}: File escapes {evidence_root}: {evidence_file}")
            if not resolved.exists():
                errors.append(f"{location}: referenced file does not exist: {evidence_file}")
            elif not resolved.is_file():
                errors.append(f"{location}: referenced path is not a file: {evidence_file}")
            elif resolved.suffix.lower() not in {".png", ".jpg", ".jpeg"}:
                errors.append(
                    f"{location}: screenshot file should be .png/.jpg/.jpeg: {evidence_file}"
                )
            if evidence_file in seen_files:
                errors.append(
                    f"{location}: duplicate File also referenced at line {seen_files[evidence_file]}: {evidence_file}"
                )
            else:
                seen_files[evidence_file] = row.line_no

        verdict = row.verdict.strip()
        if verdict not in VALID_VERDICTS:
            errors.append(
                f"{location}: Verdict must be one of {sorted(VALID_VERDICTS)}, got {row.verdict!r}"
            )

        if not row.notes:
            errors.append(f"{location}: Notes must not be empty")

    return errors


def parse_viewport(value: str) -> Viewport:
    match = VIEWPORT_RE.fullmatch(value)
    if not match:
        raise argparse.ArgumentTypeError(
            f"viewport must look like 1440x1000, got {value!r}"
        )
    return Viewport(
        label=value,
        width=int(match.group("width")),
        height=int(match.group("height")),
    )


def normalize_route(route: str) -> str:
    if not route.startswith("/"):
        return f"/{route}"
    return route


def target_url(base_url: str, route: str) -> str:
    base = base_url.rstrip("/") + "/"
    return urljoin(base, normalize_route(route).lstrip("/"))


def iso_date(days_from_today: int) -> str:
    return (dt.date.today() + dt.timedelta(days=days_from_today)).isoformat()


def iso_timestamp(days_from_today: int, hour: int = 8) -> str:
    target = dt.datetime.combine(
        dt.date.today() + dt.timedelta(days=days_from_today),
        dt.time(hour=hour, minute=0, tzinfo=dt.timezone.utc),
    )
    return target.isoformat().replace("+00:00", "Z")


def asset_workflow_providers() -> list[dict[str, object]]:
    return [
        {
            "provider_id": "provider_hetzner",
            "name": "Hetzner",
            "website": "https://www.hetzner.com",
            "panel_url": "https://console.hetzner.cloud",
            "account_hint": "ops-main",
            "country": "DE",
            "note": "Primary EU compute account.",
            "rating": 5,
            "labels": ["core", "eu"],
            "created_at": iso_timestamp(-120),
            "updated_at": iso_timestamp(-2),
        },
        {
            "provider_id": "provider_vultr",
            "name": "Vultr",
            "website": "https://www.vultr.com",
            "panel_url": "https://my.vultr.com",
            "account_hint": "edge-lab",
            "country": "US",
            "note": "Edge and migration candidates.",
            "rating": 4,
            "labels": ["edge", "backup"],
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp(-3),
        },
        {
            "provider_id": "provider_netcup",
            "name": "Netcup",
            "website": "https://www.netcup.de",
            "panel_url": "https://www.servercontrolpanel.de",
            "account_hint": "legacy-eu",
            "country": "DE",
            "note": "Legacy low-cost instances under review.",
            "rating": 3,
            "labels": ["legacy"],
            "created_at": iso_timestamp(-300),
            "updated_at": iso_timestamp(-7),
        },
    ]


def asset_workflow_vps_assets() -> list[dict[str, object]]:
    return [
        {
            "vps_id": "vps_ams_core",
            "display_name": "ams-core-01",
            "provider_id": "provider_hetzner",
            "provider_name": "Hetzner",
            "product_name": "CPX31",
            "order_ref": "HZ-2026-001",
            "country": "NL",
            "region": "EU-West",
            "city": "Amsterdam",
            "datacenter": "AMS1",
            "ipv4": "192.0.2.10",
            "ipv6": "2001:db8:10::1",
            "ssh_host": "ams-core-01.example.test",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Debian 12",
            "virtualization": "kvm",
            "lifecycle_status": "active",
            "usage_status": "in_use",
            "renewal_decision": "unreviewed",
            "importance": "critical",
            "labels": ["prod", "web"],
            "note": "Primary asset workflow fixture with complete facts.",
            "active_node_link_count": 2,
            "created_at": iso_timestamp(-100),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
        },
        {
            "vps_id": "vps_sjc_edge",
            "display_name": "sjc-edge-02",
            "provider_id": "provider_vultr",
            "provider_name": "Vultr",
            "product_name": "High Frequency 2 vCPU",
            "order_ref": "VU-2026-EDGE",
            "country": "US",
            "region": "US-West",
            "city": "San Jose",
            "datacenter": "SJC",
            "ipv4": "198.51.100.24",
            "ipv6": "",
            "ssh_host": "sjc-edge-02.example.test",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Ubuntu 24.04",
            "virtualization": "kvm",
            "lifecycle_status": "to_migrate",
            "usage_status": "standby",
            "renewal_decision": "migrate",
            "importance": "normal",
            "labels": ["edge", "migration"],
            "note": "Migration candidate with active subscription evidence.",
            "active_node_link_count": 1,
            "created_at": iso_timestamp(-80),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
        },
        {
            "vps_id": "vps_tokyo_lab",
            "display_name": "tokyo-lab-unlinked",
            "provider_id": None,
            "provider_name": "",
            "product_name": "",
            "order_ref": "",
            "country": "",
            "region": "",
            "city": "",
            "datacenter": "",
            "ipv4": "",
            "ipv6": "",
            "ssh_host": "",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "",
            "virtualization": "",
            "lifecycle_status": "testing",
            "usage_status": "unknown",
            "renewal_decision": "unreviewed",
            "importance": "low",
            "labels": ["needs-facts"],
            "note": "Fixture row intentionally missing subscription, provider, location, and access facts.",
            "active_node_link_count": 0,
            "created_at": iso_timestamp(-20),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
        },
        {
            "vps_id": "vps_fra_legacy",
            "display_name": "fra-legacy-cancel",
            "provider_id": "provider_netcup",
            "provider_name": "Netcup",
            "product_name": "RS 1000",
            "order_ref": "NC-LEGACY",
            "country": "DE",
            "region": "EU-Central",
            "city": "Frankfurt",
            "datacenter": "FRA",
            "ipv4": "203.0.113.7",
            "ipv6": "",
            "ssh_host": "",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Debian 11",
            "virtualization": "kvm",
            "lifecycle_status": "to_cancel",
            "usage_status": "idle",
            "renewal_decision": "cancel",
            "importance": "low",
            "labels": ["legacy", "cost-review"],
            "note": "Cancel queue fixture with auto-renew cancelled.",
            "active_node_link_count": 0,
            "created_at": iso_timestamp(-240),
            "updated_at": iso_timestamp(-4),
            "archived_at": None,
        },
        {
            "vps_id": "vps_archive_old",
            "display_name": "archive-old-2019",
            "provider_id": "provider_netcup",
            "provider_name": "Netcup",
            "product_name": "Legacy VPS",
            "order_ref": "NC-2019",
            "country": "DE",
            "region": "EU-Central",
            "city": "Nuremberg",
            "datacenter": "NBG",
            "ipv4": "203.0.113.40",
            "ipv6": "",
            "ssh_host": "",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Debian 10",
            "virtualization": "kvm",
            "lifecycle_status": "archived",
            "usage_status": "idle",
            "renewal_decision": "keep",
            "importance": "low",
            "labels": ["archived"],
            "note": "Archived fixture row for quick-view coverage.",
            "active_node_link_count": 0,
            "created_at": iso_timestamp(-900),
            "updated_at": iso_timestamp(-60),
            "archived_at": iso_timestamp(-30),
        },
    ]


def asset_workflow_subscriptions() -> list[dict[str, object]]:
    return [
        {
            "subscription_id": "sub_ams_core",
            "vps_id": "vps_ams_core",
            "price": 10.5,
            "currency": "USD",
            "billing_cycle": "monthly",
            "billing_months": 1,
            "monthly_price": 10.5,
            "started_at": iso_date(-100),
            "renew_at": iso_date(8),
            "auto_renew": True,
            "auto_renew_cancelled": False,
            "status": "active",
            "payment_method": "card-main",
            "note": "Renewal window fixture.",
            "created_at": iso_timestamp(-100),
            "updated_at": iso_timestamp(-1),
        },
        {
            "subscription_id": "sub_sjc_edge",
            "vps_id": "vps_sjc_edge",
            "price": 24.0,
            "currency": "USD",
            "billing_cycle": "quarterly",
            "billing_months": 3,
            "monthly_price": 8.0,
            "started_at": iso_date(-80),
            "renew_at": iso_date(21),
            "auto_renew": False,
            "auto_renew_cancelled": False,
            "status": "active",
            "payment_method": "paypal-edge",
            "note": "Migration candidate subscription.",
            "created_at": iso_timestamp(-80),
            "updated_at": iso_timestamp(-2),
        },
        {
            "subscription_id": "sub_fra_legacy",
            "vps_id": "vps_fra_legacy",
            "price": 5.0,
            "currency": "EUR",
            "billing_cycle": "monthly",
            "billing_months": 1,
            "monthly_price": 5.0,
            "started_at": iso_date(-240),
            "renew_at": iso_date(5),
            "auto_renew": True,
            "auto_renew_cancelled": True,
            "status": "active",
            "payment_method": "sepa",
            "note": "Cancel queue subscription with auto-renew cancelled.",
            "created_at": iso_timestamp(-240),
            "updated_at": iso_timestamp(-4),
        },
        {
            "subscription_id": "sub_archive_old",
            "vps_id": "vps_archive_old",
            "price": 3.0,
            "currency": "EUR",
            "billing_cycle": "monthly",
            "billing_months": 1,
            "monthly_price": 3.0,
            "started_at": iso_date(-900),
            "renew_at": iso_date(-20),
            "auto_renew": False,
            "auto_renew_cancelled": True,
            "status": "cancelled",
            "payment_method": "legacy-card",
            "note": "Archived subscription fixture.",
            "created_at": iso_timestamp(-900),
            "updated_at": iso_timestamp(-30),
        },
    ]


def asset_workflow_dashboard() -> dict[str, object]:
    return {
        "snapshot_generated_at": iso_timestamp(0),
        "total_node_count": 3,
        "total_target_count": 5,
        "abnormal_node_count": 1,
        "abnormal_target_count": 1,
        "severe_node_count": 0,
        "severe_target_count": 0,
        "maintenance_node_count": 0,
        "maintenance_target_count": 1,
        "pending_onboarding_node_count": 1,
        "paused_node_count": 0,
        "retired_node_count": 0,
        "paused_target_count": 1,
        "archived_target_count": 0,
        "recent_new_incident_count": 2,
        "recent_recovery_count": 1,
        "group_summaries": [
            {
                "group": "asset-fixture",
                "node_count": 3,
                "target_count": 5,
                "abnormal_node_count": 1,
                "abnormal_target_count": 1,
                "severe_node_count": 0,
                "severe_target_count": 0,
                "maintenance_node_count": 0,
                "maintenance_target_count": 1,
            }
        ],
        "notification_status": {
            "telegram_configured": True,
            "telegram_runtime_managed": True,
            "telegram_runtime_apply_active": True,
            "feishu_configured": False,
        },
        "asset_summary": {
            "renewal_due_30d_subscription_count": 3,
            "renewal_due_30d_vps_count": 3,
            "unreviewed_vps_count": 2,
            "to_cancel_vps_count": 1,
            "to_migrate_vps_count": 1,
            "unlinked_vps_count": 3,
            "abnormal_linked_vps_count": 1,
            "cost_by_currency": [
                {"currency": "USD", "monthly_total": 18.5, "yearly_total": 222.0},
                {"currency": "EUR", "monthly_total": 8.0, "yearly_total": 96.0},
            ],
        },
        "abnormal_nodes": [],
        "abnormal_targets": [],
        "recent_events": [],
        "new_incident_trend_24h": [0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0],
        "recovery_trend_24h": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0],
    }


def first_query_value(query: dict[str, list[str]], key: str) -> str | None:
    values = query.get(key)
    if not values:
        return None
    value = values[0].strip()
    return value or None


def filter_asset_workflow_vps(query: dict[str, list[str]]) -> list[dict[str, object]]:
    rows = asset_workflow_vps_assets()
    for key in ("provider_id", "lifecycle_status", "usage_status", "renewal_decision"):
        value = first_query_value(query, key)
        if value:
            rows = [row for row in rows if str(row.get(key) or "") == value]
    return rows


def filter_asset_workflow_subscriptions(
    query: dict[str, list[str]],
) -> list[dict[str, object]]:
    rows = asset_workflow_subscriptions()
    vps_id = first_query_value(query, "vps_id")
    if vps_id:
        rows = [row for row in rows if row["vps_id"] == vps_id]

    status = first_query_value(query, "status")
    if status:
        rows = [row for row in rows if row["status"] == status]

    renew_within_days = first_query_value(query, "renew_within_days")
    if renew_within_days:
        try:
            window_days = int(renew_within_days)
        except ValueError:
            window_days = None
        if window_days is not None:
            today = dt.date.today()
            rows = [
                row
                for row in rows
                if isinstance(row.get("renew_at"), str)
                and (
                    dt.date.fromisoformat(str(row["renew_at"])) - today
                ).days
                <= window_days
            ]

    if first_query_value(query, "sort") == "renew_at":
        rows.sort(key=lambda row: str(row.get("renew_at") or "9999-12-31"))
        if first_query_value(query, "order") == "desc":
            rows.reverse()
    return rows


def fulfill_json(route: object, status: int, body: object) -> None:
    route.fulfill(
        status=status,
        content_type="application/json; charset=utf-8",
        body=json.dumps(body, ensure_ascii=False),
    )


def fulfill_asset_workflow_api(route: object) -> None:
    request = route.request
    parsed = urlparse(request.url)
    path = parsed.path
    query = parse_qs(parsed.query)
    method = request.method.upper()

    if method == "GET" and path == "/api/auth/me":
        fulfill_json(
            route,
            200,
            {
                "user_id": "user_visual_evidence",
                "username": "visual-evidence",
                "role": "admin",
                "display_name": "Visual Evidence",
            },
        )
        return

    if method == "GET" and path == "/api/dashboard":
        fulfill_json(route, 200, asset_workflow_dashboard())
        return

    if method == "GET" and path == "/api/providers":
        fulfill_json(route, 200, asset_workflow_providers())
        return

    if method == "GET" and path == "/api/vps":
        fulfill_json(route, 200, filter_asset_workflow_vps(query))
        return

    if method == "GET" and path == "/api/subscriptions":
        fulfill_json(route, 200, filter_asset_workflow_subscriptions(query))
        return

    fulfill_json(
        route,
        404,
        {
            "error": "mock asset workflow API has no fixture for this request",
            "method": method,
            "path": path,
        },
    )


def install_mock_api_routes(page: object, profile: MockAPIProfile) -> None:
    if profile == "none":
        return
    if profile == "asset-workflows":
        page.route("**/api/**", fulfill_asset_workflow_api)
        return
    raise ValueError(f"unsupported mock API profile: {profile}")


def run_browser_sanity(
    base_url: str,
    routes: Iterable[str],
    viewports: Iterable[Viewport],
    timeout_ms: int,
    mock_api: MockAPIProfile,
) -> int:
    try:
        from playwright.sync_api import Error as PlaywrightError
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import sync_playwright
    except ModuleNotFoundError:
        print(
            "browser-sanity requires a locally installed Python Playwright package. "
            "The repository intentionally does not depend on it; install/use local "
            "browser tooling or record browser sanity as blocked.",
            file=sys.stderr,
        )
        return 2

    failures: list[str] = []
    with sync_playwright() as playwright:
        try:
            browser = playwright.chromium.launch()
        except PlaywrightError as exc:
            print(
                "browser-sanity could not launch Chromium through local Playwright. "
                "Install the browser runtime locally or record browser sanity as blocked.\n"
                f"{exc}",
                file=sys.stderr,
            )
            return 2

        try:
            for route in routes:
                for viewport in viewports:
                    url = target_url(base_url, route)
                    page = browser.new_page(
                        viewport={"width": viewport.width, "height": viewport.height}
                    )
                    install_mock_api_routes(page, mock_api)
                    try:
                        page.goto(url, wait_until="networkidle", timeout=timeout_ms)
                    except PlaywrightTimeoutError:
                        # Long-polling or failed API calls can prevent networkidle.
                        page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
                    try:
                        page.wait_for_function(
                            "() => document.body && document.body.innerText.trim().length > 20",
                            timeout=timeout_ms,
                        )
                    except PlaywrightTimeoutError:
                        pass

                    result = page.evaluate(
                        """
                        () => {
                          const doc = document.documentElement;
                          const body = document.body;
                          const viewportWidth = window.innerWidth;
                          const viewportHeight = window.innerHeight;
                          const bodyText = (body.innerText || '').trim().replace(/\\s+/g, ' ');
                          const pagePanels = document.querySelectorAll('.page-panel, .hero-panel, main, [role="main"]').length;

                          function visible(el) {
                            const style = window.getComputedStyle(el);
                            const rect = el.getBoundingClientRect();
                            return style.display !== 'none'
                              && style.visibility !== 'hidden'
                              && Number(style.opacity || '1') > 0
                              && rect.width > 0
                              && rect.height > 0;
                          }

                          function hasVisibleText(el) {
                            const text = (el.innerText || el.textContent || '').trim();
                            return text.length > 0 && visible(el);
                          }

                          const elements = Array.from(document.querySelectorAll('body *'));
                          const leafTextElements = elements.filter((el) => {
                            if (!hasVisibleText(el)) return false;
                            return !Array.from(el.children).some((child) => hasVisibleText(child));
                          });

                          const overflowingText = leafTextElements
                            .filter((el) => el.scrollWidth > el.clientWidth + 2 || el.scrollHeight > el.clientHeight + 2)
                            .slice(0, 8)
                            .map((el) => {
                              const rect = el.getBoundingClientRect();
                              return {
                                tag: el.tagName.toLowerCase(),
                                className: String(el.className || ''),
                                text: (el.innerText || el.textContent || '').trim().replace(/\\s+/g, ' ').slice(0, 80),
                                clientWidth: el.clientWidth,
                                scrollWidth: el.scrollWidth,
                                clientHeight: el.clientHeight,
                                scrollHeight: el.scrollHeight,
                                x: Math.round(rect.x),
                                y: Math.round(rect.y),
                              };
                            });

                          return {
                            currentUrl: window.location.href,
                            title: document.title,
                            bodyTextLength: bodyText.length,
                            bodyTextSample: bodyText.slice(0, 100),
                            docScrollWidth: doc.scrollWidth,
                            bodyScrollWidth: body.scrollWidth,
                            viewportWidth,
                            viewportHeight,
                            pagePanels,
                            overflowingText,
                          };
                        }
                        """
                    )
                    page.close()

                    route_failures: list[str] = []
                    if result["bodyTextLength"] < 20:
                        route_failures.append("blank-or-nearly-blank body text")
                    if result["docScrollWidth"] > viewport.width + 2:
                        route_failures.append(
                            f"document horizontal overflow {result['docScrollWidth']} > {viewport.width}"
                        )
                    if result["bodyScrollWidth"] > viewport.width + 2:
                        route_failures.append(
                            f"body horizontal overflow {result['bodyScrollWidth']} > {viewport.width}"
                        )
                    status = "PASS" if not route_failures else "FAIL"
                    warning_text = (
                        f" warnings={len(result['overflowingText'])}"
                        if result["overflowingText"]
                        else ""
                    )
                    mock_text = f" mock={mock_api}" if mock_api != "none" else ""
                    print(
                        f"{status} {normalize_route(route)} {viewport.label} "
                        f"text={result['bodyTextLength']} "
                        f"doc={result['docScrollWidth']} body={result['bodyScrollWidth']} "
                        f"panels={result['pagePanels']}{warning_text}{mock_text} "
                        f"url={result['currentUrl']}"
                    )
                    if result["overflowingText"]:
                        for item in result["overflowingText"]:
                            print(
                                "  warning overflow-risk "
                                f"{item['tag']}.{item['className']} "
                                f"{item['clientWidth']}x{item['clientHeight']} -> "
                                f"{item['scrollWidth']}x{item['scrollHeight']} "
                                f"at {item['x']},{item['y']}: {item['text']!r}"
                            )
                    if route_failures:
                        failures.append(
                            f"{normalize_route(route)} {viewport.label}: "
                            + "; ".join(route_failures)
                        )
        finally:
            browser.close()

    if failures:
        print("\nBrowser sanity failed:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)
        return 1
    return 0


def command_validate_manifest(args: argparse.Namespace) -> int:
    manifest = args.manifest.resolve()
    errors = validate_manifest(manifest)
    if errors:
        print("Visual evidence manifest validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    _, rows, _ = parse_manifest(manifest)
    print(f"Visual evidence manifest OK: {manifest} ({len(rows)} row(s))")
    return 0


def command_browser_sanity(args: argparse.Namespace) -> int:
    return run_browser_sanity(
        base_url=args.base_url,
        routes=args.route,
        viewports=args.viewport,
        timeout_ms=args.timeout_ms,
        mock_api=args.mock_api,
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Validate Houfeng v2 visual evidence and run local browser sanity."
    )
    subcommands = parser.add_subparsers(dest="command", required=True)

    validate = subcommands.add_parser(
        "validate-manifest",
        help="Validate docs/operations/v2-visual-evidence/manifest.md.",
    )
    validate.add_argument(
        "--manifest",
        type=Path,
        default=DEFAULT_MANIFEST,
        help=f"Manifest path (default: {DEFAULT_MANIFEST.relative_to(REPO_ROOT)}).",
    )
    validate.set_defaults(func=command_validate_manifest)

    sanity = subcommands.add_parser(
        "browser-sanity",
        help="Run a local-only browser sanity probe against a running preview URL.",
    )
    sanity.add_argument(
        "--base-url",
        required=True,
        help="Running preview URL, for example http://127.0.0.1:5178/.",
    )
    sanity.add_argument(
        "--route",
        action="append",
        required=True,
        help="Route to check. Repeat for multiple routes, for example --route /nodes.",
    )
    sanity.add_argument(
        "--viewport",
        action="append",
        type=parse_viewport,
        default=[],
        help="Viewport to check, e.g. 1440x1000. Repeat for multiple viewports. Defaults to 1440x1000 and 390x900.",
    )
    sanity.add_argument(
        "--timeout-ms",
        type=int,
        default=10_000,
        help="Navigation and body-text wait timeout in milliseconds.",
    )
    sanity.add_argument(
        "--mock-api",
        choices=["none", "asset-workflows"],
        default="none",
        help=(
            "Optional local-only API fixture profile. Use asset-workflows to "
            "render protected Asset Ledger routes without a running center."
        ),
    )
    sanity.set_defaults(func=command_browser_sanity)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if getattr(args, "command", None) == "browser-sanity" and not args.viewport:
        args.viewport = [parse_viewport("1440x1000"), parse_viewport("390x900")]
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Validate v2 visual evidence and run local browser sanity checks."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
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
MOCK_API_PROFILE_CHOICES = ("none", "asset-workflows", "observability-support")
MockAPIProfile = Literal["none", "asset-workflows", "observability-support"]


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


@dataclass(frozen=True)
class BrowserLogin:
    username: str
    password: str


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


def route_path(route: str) -> str:
    parsed = urlparse(normalize_route(route))
    return parsed.path or "/"


def target_url(base_url: str, route: str) -> str:
    base = base_url.rstrip("/") + "/"
    return urljoin(base, normalize_route(route).lstrip("/"))


def resolve_login_value(
    value: str | None,
    env_name: str | None,
    label: str,
) -> tuple[str | None, list[str]]:
    errors: list[str] = []
    if value and env_name:
        errors.append(f"{label}: use either a direct value or an env var, not both")
        return None, errors
    if env_name:
        env_value = os.environ.get(env_name)
        if not env_value:
            errors.append(f"{label}: environment variable {env_name!r} is not set or empty")
            return None, errors
        return env_value, errors
    if value:
        return value, errors
    return None, errors


def resolve_browser_login(args: argparse.Namespace) -> tuple[BrowserLogin | None, list[str]]:
    username, username_errors = resolve_login_value(
        args.login_username,
        args.login_username_env,
        "login username",
    )
    password, password_errors = resolve_login_value(
        args.login_password,
        args.login_password_env,
        "login password",
    )
    errors = username_errors + password_errors
    if errors:
        return None, errors

    if bool(username) != bool(password):
        return None, [
            "browser-sanity login requires both username and password; use "
            "--login-username or --login-username-env together with "
            "--login-password or --login-password-env"
        ]
    if username is None or password is None:
        return None, []
    return BrowserLogin(username=username, password=password), []


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


def observability_support_nodes() -> list[dict[str, object]]:
    return [
        {
            "node_id": "node_hkg_edge_01",
            "display_name": "hkg-edge-01",
            "group": "asset-prod",
            "region": "APAC",
            "city": "Hong Kong",
            "provider": "Hetzner",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "已绑定",
            "labels": ["prod", "edge", "vps-linked"],
            "note": "Severe node fixture for UX-5 abnormal evidence.",
            "current_health_status": "严重",
            "last_heartbeat_at": iso_timestamp_hours_ago(1),
            "last_sync_at": iso_timestamp_hours_ago(1),
            "current_active_incident_count": 3,
            "current_primary_issue_summary": "CPU 持续高位且心跳延迟，需要先核对 VPS 负载。",
            "created_at": iso_timestamp(-80),
            "updated_at": iso_timestamp_hours_ago(1),
        },
        {
            "node_id": "node_pending_sfo_02",
            "display_name": "sfo-pending-onboarding",
            "group": "asset-intake",
            "region": "US-West",
            "city": "San Francisco",
            "provider": "Vultr",
            "lifecycle_status": "待接入",
            "monitoring_status": "启用",
            "binding_status": "未绑定",
            "labels": ["onboarding", "needs-agent"],
            "note": "Pending onboarding fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": None,
            "last_sync_at": None,
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-6),
            "updated_at": iso_timestamp_hours_ago(6),
        },
        {
            "node_id": "node_ams_conflict_03",
            "display_name": "ams-conflict-03",
            "group": "asset-prod",
            "region": "EU-West",
            "city": "Amsterdam",
            "provider": "Hetzner",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "指纹变更待确认",
            "labels": ["prod", "binding-review"],
            "note": "Fingerprint conflict fixture.",
            "current_health_status": "关注",
            "last_heartbeat_at": iso_timestamp_hours_ago(5),
            "last_sync_at": iso_timestamp_hours_ago(5),
            "current_active_incident_count": 1,
            "current_primary_issue_summary": "等待确认新的主机指纹。",
            "created_at": iso_timestamp(-60),
            "updated_at": iso_timestamp_hours_ago(5),
        },
        {
            "node_id": "node_fra_maint_04",
            "display_name": "fra-maintenance-04",
            "group": "asset-ops",
            "region": "EU-Central",
            "city": "Frankfurt",
            "provider": "Netcup",
            "lifecycle_status": "在用",
            "monitoring_status": "维护中",
            "binding_status": "已绑定",
            "labels": ["maintenance", "db"],
            "note": "Maintenance window fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp_hours_ago(2),
            "last_sync_at": iso_timestamp_hours_ago(2),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-140),
            "updated_at": iso_timestamp_hours_ago(2),
        },
        {
            "node_id": "node_sin_paused_05",
            "display_name": "sin-paused-05",
            "group": "asset-observe",
            "region": "APAC",
            "city": "Singapore",
            "provider": "Manual",
            "lifecycle_status": "观察中",
            "monitoring_status": "暂停",
            "binding_status": "已绑定",
            "labels": ["paused", "cost-review"],
            "note": "Paused monitoring fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp_hours_ago(30),
            "last_sync_at": iso_timestamp_hours_ago(30),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-220),
            "updated_at": iso_timestamp_hours_ago(30),
        },
        {
            "node_id": "node_old_retired_06",
            "display_name": "old-retired-06",
            "group": "archive",
            "region": "EU-Central",
            "city": "Nuremberg",
            "provider": "Netcup",
            "lifecycle_status": "已退役",
            "monitoring_status": "暂停",
            "binding_status": "已绑定",
            "labels": ["archived", "legacy"],
            "note": "Retired node fixture for inventory completeness.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp(-45),
            "last_sync_at": iso_timestamp(-45),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-600),
            "updated_at": iso_timestamp(-45),
        },
    ]


def observability_support_targets() -> list[dict[str, object]]:
    return [
        {
            "target_id": "target_api_core",
            "name": "api-core.example.test",
            "target_type": "service",
            "host": "api-core.example.test",
            "base_port": 443,
            "execution_node_labels": ["prod", "edge"],
            "run_status": "启用",
            "group": "asset-prod",
            "labels": ["prod", "api", "vps-linked"],
            "note": "Abnormal API target fixture.",
            "current_health_status": "告警",
            "current_active_incident_count": 2,
            "last_success_at": iso_timestamp_hours_ago(7),
            "last_failure_at": iso_timestamp_hours_ago(1),
            "current_primary_issue_summary": "HTTP 5xx 持续出现，需结合 Node 与资产决策核对。",
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp_hours_ago(1),
        },
        {
            "target_id": "target_china_ref",
            "name": "china-reference-latency",
            "target_type": "china_reference",
            "host": "www.baidu.com",
            "base_port": 443,
            "execution_node_labels": ["cn-probe"],
            "run_status": "启用",
            "group": "network-reference",
            "labels": ["reference", "china", "notification"],
            "note": "Reference target with notification event coverage.",
            "current_health_status": "关注",
            "current_active_incident_count": 1,
            "last_success_at": iso_timestamp_hours_ago(4),
            "last_failure_at": iso_timestamp_hours_ago(2),
            "current_primary_issue_summary": "跨境参考延迟超过关注阈值。",
            "created_at": iso_timestamp(-70),
            "updated_at": iso_timestamp_hours_ago(2),
        },
        {
            "target_id": "target_www_maint",
            "name": "www-maintenance.example.test",
            "target_type": "service",
            "host": "www-maintenance.example.test",
            "base_port": 443,
            "execution_node_labels": ["prod"],
            "run_status": "维护中",
            "group": "asset-ops",
            "labels": ["maintenance", "web"],
            "note": "Maintenance target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": iso_timestamp_hours_ago(3),
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-120),
            "updated_at": iso_timestamp_hours_ago(3),
        },
        {
            "target_id": "target_docs_paused",
            "name": "docs-paused.example.test",
            "target_type": "service",
            "host": "docs-paused.example.test",
            "base_port": 443,
            "execution_node_labels": ["docs"],
            "run_status": "暂停",
            "group": "docs",
            "labels": ["paused", "docs"],
            "note": "Paused target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": iso_timestamp_hours_ago(48),
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-160),
            "updated_at": iso_timestamp_hours_ago(48),
        },
        {
            "target_id": "target_legacy_archived",
            "name": "legacy-archived.example.test",
            "target_type": "service",
            "host": "legacy-archived.example.test",
            "base_port": 80,
            "execution_node_labels": ["legacy"],
            "run_status": "已归档",
            "group": "archive",
            "labels": ["archived", "legacy"],
            "note": "Archived target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": iso_timestamp(-30),
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-500),
            "updated_at": iso_timestamp(-30),
        },
        {
            "target_id": "target_no_exec_labels",
            "name": "no-execution-label.example.test",
            "target_type": "service",
            "host": "no-execution-label.example.test",
            "base_port": 8443,
            "execution_node_labels": [],
            "run_status": "启用",
            "group": "asset-intake",
            "labels": ["needs-coverage"],
            "note": "Coverage-gap target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": None,
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-12),
            "updated_at": iso_timestamp_hours_ago(12),
        },
    ]


def observability_support_events() -> list[dict[str, object]]:
    return [
        {
            "event_id": "event_node_severe_started",
            "incident_id": "inc_node_hkg_cpu",
            "incident_class": "node_resource_pressure",
            "object_type": "node",
            "object_id": "node_hkg_edge_01",
            "event_type": "incident_started",
            "severity": "严重",
            "summary": "hkg-edge-01 CPU 与 load5 同时进入严重区间。",
            "created_at": iso_timestamp_hours_ago(1),
            "_labels": ["prod", "edge", "vps-linked"],
            "_notification_sent": True,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_api_escalated",
            "incident_id": "inc_target_api_5xx",
            "incident_class": "target_probe_failure",
            "object_type": "target",
            "object_id": "target_api_core",
            "event_type": "incident_escalated",
            "severity": "告警",
            "summary": "api-core.example.test HTTP 探测失败率升高。",
            "created_at": iso_timestamp_hours_ago(2),
            "_labels": ["prod", "api", "vps-linked"],
            "_notification_sent": True,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_china_notice",
            "incident_id": "inc_target_china_latency",
            "incident_class": "target_probe_failure",
            "object_type": "target",
            "object_id": "target_china_ref",
            "event_type": "incident_started",
            "severity": "关注",
            "summary": "中国参考入口延迟超过关注阈值，已发送通知样例。",
            "created_at": iso_timestamp_hours_ago(4),
            "_labels": ["reference", "china", "notification"],
            "_notification_sent": True,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_recovered",
            "incident_id": "inc_target_api_5xx",
            "incident_class": "target_probe_failure",
            "object_type": "target",
            "object_id": "target_api_core",
            "event_type": "incident_recovered",
            "severity": "正常",
            "summary": "api-core.example.test 探测恢复，保留用于 recovery filter。",
            "created_at": iso_timestamp_hours_ago(5),
            "_labels": ["prod", "api", "vps-linked"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_node_maintenance_entered",
            "incident_id": "runtime_node_fra_maint",
            "incident_class": "",
            "object_type": "node",
            "object_id": "node_fra_maint_04",
            "event_type": "node_monitoring_maintenance_entered",
            "severity": "",
            "summary": "fra-maintenance-04 进入维护窗口。",
            "created_at": iso_timestamp_hours_ago(6),
            "_labels": ["maintenance", "db"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_maintenance_exited",
            "incident_id": "runtime_target_www_maint",
            "incident_class": "",
            "object_type": "target",
            "object_id": "target_www_maint",
            "event_type": "target_maintenance_exited",
            "severity": "",
            "summary": "www-maintenance.example.test 退出维护窗口。",
            "created_at": iso_timestamp_hours_ago(7),
            "_labels": ["maintenance", "web"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_backfilled_node",
            "incident_id": "inc_backfilled_node_disk",
            "incident_class": "node_disk_pressure",
            "object_type": "node",
            "object_id": "node_hkg_edge_01",
            "event_type": "incident_started",
            "severity": "告警",
            "summary": "补传观测触发的磁盘压力事件，默认应被事件流排除。",
            "created_at": iso_timestamp_hours_ago(8),
            "_labels": ["prod", "edge", "backfilled"],
            "_notification_sent": False,
            "_is_backfilled": True,
        },
        {
            "event_id": "event_node_binding_confirmed",
            "incident_id": "binding_node_ams_conflict",
            "incident_class": "",
            "object_type": "node",
            "object_id": "node_ams_conflict_03",
            "event_type": "node_binding_rebind_confirmed",
            "severity": "关注",
            "summary": "ams-conflict-03 新指纹确认重新绑定。",
            "created_at": iso_timestamp_hours_ago(10),
            "_labels": ["prod", "binding-review"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_paused",
            "incident_id": "runtime_target_docs_paused",
            "incident_class": "",
            "object_type": "target",
            "object_id": "target_docs_paused",
            "event_type": "target_paused",
            "severity": "",
            "summary": "docs-paused.example.test 已暂停探测。",
            "created_at": iso_timestamp_hours_ago(12),
            "_labels": ["paused", "docs"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_archived",
            "incident_id": "runtime_target_legacy_archived",
            "incident_class": "",
            "object_type": "target",
            "object_id": "target_legacy_archived",
            "event_type": "target_archived",
            "severity": "",
            "summary": "legacy-archived.example.test 已归档。",
            "created_at": iso_timestamp_hours_ago(36),
            "_labels": ["archived", "legacy"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
    ]


def observability_support_dashboard() -> dict[str, object]:
    nodes = observability_support_nodes()
    targets = observability_support_targets()
    events = filter_observability_support_events({"limit": ["6"]})
    abnormal_nodes = [node for node in nodes if node["current_health_status"] != "正常"]
    abnormal_targets = [target for target in targets if target["current_health_status"] != "正常"]

    group_names = sorted(
        {
            str(item.get("group") or "未分组")
            for item in [*nodes, *targets]
        },
        key=lambda value: value or "未分组",
    )
    group_summaries: list[dict[str, object]] = []
    for group in group_names:
        group_nodes = [node for node in nodes if str(node.get("group") or "未分组") == group]
        group_targets = [target for target in targets if str(target.get("group") or "未分组") == group]
        group_summaries.append(
            {
                "group": group,
                "node_count": len(group_nodes),
                "target_count": len(group_targets),
                "abnormal_node_count": sum(1 for node in group_nodes if node["current_health_status"] != "正常"),
                "abnormal_target_count": sum(1 for target in group_targets if target["current_health_status"] != "正常"),
                "severe_node_count": sum(1 for node in group_nodes if node["current_health_status"] == "严重"),
                "severe_target_count": sum(1 for target in group_targets if target["current_health_status"] == "严重"),
                "maintenance_node_count": sum(1 for node in group_nodes if node["monitoring_status"] == "维护中"),
                "maintenance_target_count": sum(1 for target in group_targets if target["run_status"] == "维护中"),
            }
        )

    return {
        "snapshot_generated_at": iso_timestamp_hours_ago(0),
        "total_node_count": len(nodes),
        "total_target_count": len(targets),
        "abnormal_node_count": len(abnormal_nodes),
        "abnormal_target_count": len(abnormal_targets),
        "severe_node_count": sum(1 for node in nodes if node["current_health_status"] == "严重"),
        "severe_target_count": sum(1 for target in targets if target["current_health_status"] == "严重"),
        "maintenance_node_count": sum(1 for node in nodes if node["monitoring_status"] == "维护中"),
        "maintenance_target_count": sum(1 for target in targets if target["run_status"] == "维护中"),
        "pending_onboarding_node_count": sum(
            1
            for node in nodes
            if node["lifecycle_status"] == "待接入"
            or node["binding_status"] in ("未绑定", "指纹变更待确认")
        ),
        "paused_node_count": sum(1 for node in nodes if node["monitoring_status"] == "暂停"),
        "retired_node_count": sum(1 for node in nodes if node["lifecycle_status"] == "已退役"),
        "paused_target_count": sum(1 for target in targets if target["run_status"] == "暂停"),
        "archived_target_count": sum(1 for target in targets if target["run_status"] == "已归档"),
        "recent_new_incident_count": sum(1 for event in events if event["event_type"] == "incident_started"),
        "recent_recovery_count": sum(1 for event in events if event["event_type"] == "incident_recovered"),
        "group_summaries": group_summaries,
        "notification_status": {
            "telegram_configured": True,
            "telegram_runtime_managed": False,
            "telegram_runtime_apply_active": False,
            "feishu_configured": False,
        },
        "asset_summary": {
            "renewal_due_30d_subscription_count": 0,
            "renewal_due_30d_vps_count": 0,
            "unreviewed_vps_count": 0,
            "to_cancel_vps_count": 0,
            "to_migrate_vps_count": 0,
            "unlinked_vps_count": 0,
            "abnormal_linked_vps_count": 0,
            "cost_by_currency": [],
        },
        "abnormal_nodes": [
            {
                "node_id": node["node_id"],
                "display_name": node["display_name"],
                "group": node["group"],
                "region": node["region"],
                "city": node["city"],
                "provider": node["provider"],
                "lifecycle_status": node["lifecycle_status"],
                "monitoring_status": node["monitoring_status"],
                "current_health_status": node["current_health_status"],
                "last_heartbeat_at": node["last_heartbeat_at"],
                "current_active_incident_count": node["current_active_incident_count"],
                "current_primary_issue_summary": node["current_primary_issue_summary"],
            }
            for node in abnormal_nodes[:4]
        ],
        "abnormal_targets": [
            {
                "target_id": target["target_id"],
                "name": target["name"],
                "target_type": target["target_type"],
                "host": target["host"],
                "base_port": target["base_port"],
                "run_status": target["run_status"],
                "group": target["group"],
                "current_health_status": target["current_health_status"],
                "last_success_at": target["last_success_at"],
                "last_failure_at": target["last_failure_at"],
                "current_active_incident_count": target["current_active_incident_count"],
                "current_primary_issue_summary": target["current_primary_issue_summary"],
            }
            for target in abnormal_targets[:4]
        ],
        "recent_events": events,
        "new_incident_trend_24h": [0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 1],
        "recovery_trend_24h": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0],
    }


def observability_node_sparklines(query: dict[str, list[str]]) -> dict[str, object]:
    metrics = [
        metric.strip()
        for value in query.get("metrics", ["cpu_usage_pct,mem_used_pct,disk_used_pct"])
        for metric in value.split(",")
        if metric.strip()
    ]
    defaults: dict[str, list[float | None]] = {
        "cpu_usage_pct": [42, 45, 48, 52, 57, 63, 72, 81, 88, 93, 96, 94],
        "mem_used_pct": [55, 56, 58, 61, 63, 67, 71, 76, 81, 85, 88, 91],
        "disk_used_pct": [61, 62, 63, 64, 66, 68, 70, 72, 73, 74, 76, 78],
    }
    stable: dict[str, list[float | None]] = {
        "cpu_usage_pct": [18, 19, 20, 21, 21, 22, 20, 19, 18, 20, 21, 19],
        "mem_used_pct": [34, 35, 35, 36, 35, 36, 37, 36, 36, 35, 35, 36],
        "disk_used_pct": [40, 40, 41, 41, 42, 42, 42, 43, 43, 43, 44, 44],
    }
    nodes: dict[str, dict[str, list[float | None]]] = {}
    for node in observability_support_nodes():
        node_id = str(node["node_id"])
        source = defaults if node_id == "node_hkg_edge_01" else stable
        if node["monitoring_status"] == "暂停":
            source = {key: [None, None, None, None] for key in defaults}
        nodes[node_id] = {metric: source.get(metric, []) for metric in metrics}
    return {"nodes": nodes}


def observability_target_sparklines() -> dict[str, object]:
    return {
        "targets": {
            "target_api_core": {"latency": [120, 130, 180, 240, 300, 520, 860, 1100, 980, 760, 640, 720]},
            "target_china_ref": {"latency": [180, 210, 260, 310, 360, 390, 420, 450, 410, 380, 430, 470]},
            "target_www_maint": {"latency": [42, 41, 40, None, None, 39, 38, 37]},
            "target_docs_paused": {"latency": [None, None, None, None]},
            "target_legacy_archived": {"latency": []},
            "target_no_exec_labels": {"latency": [22, 24, 25, 23, 21, 24]},
        }
    }


def query_bool(query: dict[str, list[str]], key: str) -> bool:
    value = first_query_value(query, key)
    if value is None:
        return False
    return value.lower() in {"1", "true", "t", "yes", "y", "on"}


def parse_query_datetime(value: str | None) -> dt.datetime | None:
    if not value:
        return None
    normalized = value.replace("Z", "+00:00")
    try:
        parsed = dt.datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=dt.timezone.utc)
    return parsed.astimezone(dt.timezone.utc)


def event_datetime(event: dict[str, object]) -> dt.datetime | None:
    value = event.get("created_at")
    return parse_query_datetime(str(value)) if value else None


def public_event(event: dict[str, object]) -> dict[str, object]:
    return {key: value for key, value in event.items() if not key.startswith("_")}


def filter_observability_support_events(
    query: dict[str, list[str]],
) -> list[dict[str, object]]:
    rows = observability_support_events()
    include_backfilled = query_bool(query, "include_backfilled")
    if not include_backfilled:
        rows = [row for row in rows if not row.get("_is_backfilled")]

    for key in ("object_type", "object_id", "severity", "event_type"):
        value = first_query_value(query, key)
        if value:
            rows = [row for row in rows if str(row.get(key) or "") == value]

    created_from = parse_query_datetime(first_query_value(query, "created_from"))
    if created_from is not None:
        rows = [
            row for row in rows
            if (event_time := event_datetime(row)) is not None and event_time >= created_from
        ]
    created_to = parse_query_datetime(first_query_value(query, "created_to"))
    if created_to is not None:
        rows = [
            row for row in rows
            if (event_time := event_datetime(row)) is not None and event_time <= created_to
        ]

    label = first_query_value(query, "label")
    if label:
        rows = [row for row in rows if label in row.get("_labels", [])]
    if query_bool(query, "notification_only"):
        rows = [row for row in rows if row.get("_notification_sent")]
    if query_bool(query, "recovery_only"):
        rows = [row for row in rows if row.get("event_type") == "incident_recovered"]
    if query_bool(query, "maintenance_only"):
        maintenance_types = {
            "node_monitoring_maintenance_entered",
            "node_monitoring_maintenance_exited",
            "target_maintenance_entered",
            "target_maintenance_exited",
        }
        rows = [row for row in rows if row.get("event_type") in maintenance_types]

    rows.sort(key=lambda row: str(row.get("created_at") or ""), reverse=True)
    limit_value = first_query_value(query, "limit")
    try:
        limit = int(limit_value) if limit_value else 50
    except ValueError:
        limit = 50
    if limit <= 0:
        limit = 50
    return [public_event(row) for row in rows[:limit]]


def iso_timestamp_hours_ago(hours: int) -> str:
    target = dt.datetime.now(dt.timezone.utc) - dt.timedelta(hours=hours)
    return target.replace(microsecond=0).isoformat().replace("+00:00", "Z")


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


def fulfill_observability_support_api(route: object) -> None:
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
                "user_id": "user_observability_visual_evidence",
                "username": "observability-evidence",
                "role": "admin",
                "display_name": "Observability Evidence",
            },
        )
        return

    if method == "GET" and path == "/api/dashboard":
        fulfill_json(route, 200, observability_support_dashboard())
        return

    if method == "GET" and path == "/api/nodes":
        fulfill_json(route, 200, observability_support_nodes())
        return

    if method == "GET" and path == "/api/nodes/sparklines":
        fulfill_json(route, 200, observability_node_sparklines(query))
        return

    if method == "GET" and path == "/api/targets":
        fulfill_json(route, 200, observability_support_targets())
        return

    if method == "GET" and path == "/api/targets/sparklines":
        fulfill_json(route, 200, observability_target_sparklines())
        return

    if method == "GET" and path == "/api/events":
        fulfill_json(route, 200, {"items": filter_observability_support_events(query)})
        return

    fulfill_json(
        route,
        404,
        {
            "error": "mock observability support API has no fixture for this request",
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
    if profile == "observability-support":
        page.route("**/api/**", fulfill_observability_support_api)
        return
    raise ValueError(f"unsupported mock API profile: {profile}")


def login_browser_context(
    page: object,
    base_url: str,
    login: BrowserLogin,
    timeout_ms: int,
) -> None:
    response = page.request.post(
        target_url(base_url, "/api/auth/login"),
        data={"username": login.username, "password": login.password},
        timeout=timeout_ms,
    )
    if response.status < 200 or response.status >= 300:
        body = response.text().strip()
        if len(body) > 240:
            body = body[:240] + "..."
        detail = f": {body}" if body else ""
        raise RuntimeError(f"POST /api/auth/login returned {response.status}{detail}")


def run_browser_sanity(
    base_url: str,
    routes: Iterable[str],
    viewports: Iterable[Viewport],
    timeout_ms: int,
    mock_api: MockAPIProfile,
    login: BrowserLogin | None,
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
                    context = browser.new_context(
                        viewport={"width": viewport.width, "height": viewport.height}
                    )
                    page = context.new_page()
                    install_mock_api_routes(page, mock_api)
                    pre_navigation_failures: list[str] = []
                    if login is not None:
                        try:
                            login_browser_context(page, base_url, login, timeout_ms)
                        except (PlaywrightError, RuntimeError) as exc:
                            pre_navigation_failures.append(f"login failed: {exc}")

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
                    context.close()

                    route_failures = list(pre_navigation_failures)
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
                    if mock_api != "none" or login is not None:
                        current_path = urlparse(result["currentUrl"]).path or "/"
                        expected_path = route_path(route)
                        if current_path != expected_path:
                            route_failures.append(
                                f"unexpected final path {current_path!r}; expected {expected_path!r}"
                            )
                    status = "PASS" if not route_failures else "FAIL"
                    warning_text = (
                        f" warnings={len(result['overflowingText'])}"
                        if result["overflowingText"]
                        else ""
                    )
                    mock_text = f" mock={mock_api}" if mock_api != "none" else ""
                    auth_text = " auth=session-login" if login is not None else ""
                    print(
                        f"{status} {normalize_route(route)} {viewport.label} "
                        f"text={result['bodyTextLength']} "
                        f"doc={result['docScrollWidth']} body={result['bodyScrollWidth']} "
                        f"panels={result['pagePanels']}{warning_text}{mock_text}{auth_text} "
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
    login, login_errors = resolve_browser_login(args)
    if login is not None and args.mock_api != "none":
        login_errors.append("browser-sanity real login cannot be combined with --mock-api")
    if login_errors:
        print("Browser sanity configuration error:", file=sys.stderr)
        for error in login_errors:
            print(f"- {error}", file=sys.stderr)
        return 2

    return run_browser_sanity(
        base_url=args.base_url,
        routes=args.route,
        viewports=args.viewport,
        timeout_ms=args.timeout_ms,
        mock_api=args.mock_api,
        login=login,
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
        choices=MOCK_API_PROFILE_CHOICES,
        default="none",
        help=(
            "Optional local-only API fixture profile. Use asset-workflows for "
            "protected Asset Ledger routes or observability-support for Nodes, "
            "Targets, and Events without a running center."
        ),
    )
    sanity.add_argument(
        "--login-username",
        help="Username for an optional real center /api/auth/login session.",
    )
    sanity.add_argument(
        "--login-username-env",
        metavar="ENV_VAR",
        help="Environment variable containing the login username.",
    )
    sanity.add_argument(
        "--login-password",
        help=(
            "Password for an optional real center /api/auth/login session. "
            "Prefer --login-password-env to avoid exposing the value in process args."
        ),
    )
    sanity.add_argument(
        "--login-password-env",
        metavar="ENV_VAR",
        help="Environment variable containing the login password.",
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

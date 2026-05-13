#!/usr/bin/env python3
"""Validate v2 visual evidence and run local browser sanity checks."""

from __future__ import annotations

import argparse
import datetime as dt
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable
from urllib.parse import urljoin


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


def run_browser_sanity(
    base_url: str,
    routes: Iterable[str],
    viewports: Iterable[Viewport],
    timeout_ms: int,
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
                    print(
                        f"{status} {normalize_route(route)} {viewport.label} "
                        f"text={result['bodyTextLength']} "
                        f"doc={result['docScrollWidth']} body={result['bodyScrollWidth']} "
                        f"panels={result['pagePanels']}{warning_text} url={result['currentUrl']}"
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

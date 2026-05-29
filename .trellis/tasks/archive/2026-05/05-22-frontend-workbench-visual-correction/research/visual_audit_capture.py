#!/usr/bin/env python3
from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "scripts"))

import visual_evidence as ve  # noqa: E402

BASE_URL = "http://127.0.0.1:5178/"
OUT_DIR = ROOT / ".trellis/tasks/05-22-frontend-workbench-visual-correction/research/screenshots"
OUT_JSON = ROOT / ".trellis/tasks/05-22-frontend-workbench-visual-correction/research/visual-audit-raw.json"
VIEWPORTS = [(1440, 1000), (390, 900)]
ROUTE_GROUPS = [
    ("asset-workflows", ["/", "/vps", "/asset-decisions", "/providers", "/subscriptions"]),
    ("observability-support", ["/nodes", "/targets", "/events"]),
]


def capture() -> None:
    from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
    from playwright.sync_api import sync_playwright

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    results = []
    with sync_playwright() as p:
        browser = p.chromium.launch()
        for profile, routes in ROUTE_GROUPS:
            for width, height in VIEWPORTS:
                context = browser.new_context(viewport={"width": width, "height": height}, device_scale_factor=1)
                page = context.new_page()
                ve.install_mock_api_routes(page, profile)
                for route in routes:
                    url = ve.target_url(BASE_URL, route)
                    try:
                        page.goto(url, wait_until="networkidle", timeout=20000)
                    except PlaywrightTimeoutError:
                        page.goto(url, wait_until="domcontentloaded", timeout=20000)
                    page.wait_for_function("() => document.body && document.body.innerText.trim().length > 20", timeout=20000)
                    page.wait_for_timeout(250)
                    safe_route = route.strip("/") or "dashboard"
                    shot = OUT_DIR / f"{safe_route}-{profile}-{width}x{height}.png"
                    page.screenshot(path=str(shot), full_page=False)
                    data = page.evaluate(
                        """
                        () => {
                          const rect = (el) => {
                            if (!el) return null
                            const r = el.getBoundingClientRect()
                            return {x: Math.round(r.x), y: Math.round(r.y), width: Math.round(r.width), height: Math.round(r.height)}
                          }
                          const text = (el) => el ? (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 180) : null
                          const style = (el) => {
                            const s = window.getComputedStyle(el)
                            return {display: s.display, gap: s.gap, gridTemplateColumns: s.gridTemplateColumns, alignItems: s.alignItems, justifyContent: s.justifyContent}
                          }
                          const buttons = Array.from(document.querySelectorAll('button, a.btn')).map((el) => ({
                            text: text(el),
                            className: el.className ? String(el.className) : '',
                            rect: rect(el),
                            style: style(el),
                            role: el.tagName.toLowerCase(),
                            visible: !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length),
                          })).filter((item) => item.visible && item.rect && item.rect.y < window.innerHeight)
                          const selectors = ['.compact-header', '.compact-header__actions', '.compact-header__metrics', '.workbench-layout', '.workbench-main', '.workbench-aside', '.table-workbench', '.table-workbench__toolbar', '.table-workbench__tabs', '.table-workbench__chips', '.table-workbench__priority', '.table-workbench__content', '.table-workbench__aside', '.filter-bar', '.data-table']
                          const sections = selectors.map((selector) => ({
                            selector,
                            count: document.querySelectorAll(selector).length,
                            rects: Array.from(document.querySelectorAll(selector)).slice(0, 4).map((el) => ({rect: rect(el), style: style(el), text: text(el)})),
                          }))
                          const firstHeadings = Array.from(document.querySelectorAll('h1,h2,h3')).slice(0, 14).map((el) => ({level: el.tagName, text: text(el), rect: rect(el)}))
                          return {
                            title: document.title,
                            bodyTextLength: (document.body.innerText || '').length,
                            scrollWidth: document.documentElement.scrollWidth,
                            clientWidth: document.documentElement.clientWidth,
                            scrollHeight: document.documentElement.scrollHeight,
                            buttons,
                            sections,
                            firstHeadings,
                          }
                        }
                        """
                    )
                    results.append({
                        "profile": profile,
                        "route": route,
                        "viewport": f"{width}x{height}",
                        "screenshot": str(shot.relative_to(ROOT)),
                        "data": data,
                    })
                context.close()
        browser.close()
    OUT_JSON.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")


if __name__ == "__main__":
    capture()

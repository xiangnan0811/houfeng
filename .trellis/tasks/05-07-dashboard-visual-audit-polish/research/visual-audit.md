# Dashboard visual audit

Date: 2026-05-07

## Inputs

* User screenshots and feedback: Dashboard still feels messy and the previous finish point was not acceptable.
* Current code after `dashboard-decision-simplification`: `web/src/pages/DashboardPage.tsx`, `web/src/styles/pages.css`, `web/src/pages/DashboardPage.test.tsx`.
* Project specs: `.trellis/spec/web/*`, especially Dashboard data contract guidance.
* External UI checklist: Web Interface Guidelines, fetched from <https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md>.

## Current Shape

Current abnormal Dashboard renders three top-level visual blocks:

1. `FleetStatePanel`: status bar with title, description, generated timestamp, and 2-3 CTAs.
2. `DashboardSummaryStrip`: four equal summary chips.
3. `DashboardWorkbench`: `DetailSection` containing the attention queue.

This is better than the prior hero + KPI + rail composition, but it still makes the user parse multiple dashboard summaries before acting. The independent summary strip is now the main remaining clutter source.

## Findings

### 1. Summary strip still competes with the work queue

The four chips are compact, but as a full-width standalone grid they still read as a second Dashboard module. In abnormal state, the user needs the current decision and queue first. Counts like abnormal objects, severe count, 24h change, and maintenance should support that decision inline.

Implementation constraint: remove the standalone `Dashboard 摘要指标` section from abnormal state. Prefer inline metrics inside the status/command area or workbench header.

### 2. Current work queue still looks like a table page embedded in the homepage

`AttentionQueue` uses `DataTable` with six columns. That is powerful for list pages, but for 1-2 abnormal objects it creates a heavy table header and a lot of surrounding frame weight. A management homepage should show the top incident/object as a compact task list.

Implementation constraint: render abnormal attention items as compact action rows/cards or substantially reduce table chrome. Preserve row navigation, stopPropagation on action links, severity sorting, freshness, type/status, and deep links.

### 3. Labels are still too technical and duplicated

The page can show `Fleet State`, `Dashboard 摘要`, `处理队列`, and `当前需要处理` at once. Some structure labels are useful for accessibility, but visible labels should guide operators in Chinese. English eyebrows should not become decoration.

Implementation constraint: reduce visible label noise. Keep semantic `aria-label` where useful, but prefer direct Chinese operational copy in visible text.

### 4. Normal/maintenance management can remain, but should not become another card field

`RunningOverview` is closer to the goal than the abnormal state, but it still contains metrics plus management entries. This is acceptable if abnormal state is strongly focused and if the normal/maintenance layout stays compact.

Implementation constraint: do not expand normal/maintenance with Group, Recent, shortcut rail, or facts. If touched, keep entries line-based and low emphasis.

## Accessibility and Interaction Notes

* Links should remain `Link` for navigation, preserving Cmd/Ctrl click behavior.
* Compound row links need explicit keyboard/click propagation handling.
* Focus states already exist for many dashboard links in CSS; new interactive blocks need `:focus-visible`.
* Long hostnames, IDs, issue summaries, and location strings must be truncatable or wrap safely with `min-width: 0`.
* Loading text should keep ellipsis style consistent with current app copy.

## Converged Direction

This task should make abnormal state visually closer to:

1. Command/status bar with inline operational metrics.
2. One compact attention workbench with top abnormal objects and queue links.

It should not add a new page section, new card strip, new context rail, or new framework.

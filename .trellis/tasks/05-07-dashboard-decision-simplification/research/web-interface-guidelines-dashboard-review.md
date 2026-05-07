# Web Interface Guidelines Review: Dashboard Decision-Focused Simplification

## Source

Fetched 2026-05-07 from:
`https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md`

## Applied Guidance

The guideline set emphasizes reviewing UI by concrete user outcomes rather than by whether components exist. For this Dashboard task, the relevant checks are:

* The first screen must have a clear information hierarchy and not force users to scan many equal-weight blocks.
* Controls and navigation should be predictable and visible when they are the primary action, not spread across unrelated regions.
* Layout must withstand real content density without looking like a data dump.
* Empty/secondary/status information should not occupy the same visual weight as the primary task.
* Tests should encode the experience constraints that caused regressions, not only field presence.

## Current Dashboard Findings

* `FleetStatePanel` still uses a two-column hero with four boxed facts on the right. In the screenshot this makes metadata visually compete with the system state conclusion.
* The 5-card KPI strip creates a second row of equal-weight numeric cards immediately after the hero. This repeats hero facts and delays the actual work.
* `DashboardWorkbench` moved system shortcuts, Group summary, and recent events into a right rail, but they remain all visible at once. The page is still a dense information dump, only changed from vertical stacking to horizontal stacking.
* The abnormal state should answer one question first: "what do I handle now?" Current layout also asks the user to parse shortcuts, inventory context, recent events, KPI cards, and API metadata.
* Existing tests verify many right-rail details are present. That locks in the clutter instead of preventing it.

## Design Direction For This Task

* Severe/abnormal Dashboard should prioritize a single action surface:
  * Compact status header
  * One primary work queue or top incident card/list
  * A small number of direct route buttons
* Move secondary facts into progressive disclosure:
  * API/snapshot metadata becomes inline muted text, not boxed cards.
  * KPI cards become compact status chips or an optional summary row, not five large cards.
  * Group and recent events are hidden from abnormal first screen; access through links to Events/Nodes/Targets.
* Normal/maintenance Dashboard can show broader system overview, but still should not show all context blocks at once.
* Fresh install should remain onboarding-only with minimal global chrome.

## Test Implications

Tests should assert that:

* Abnormal state does not render `系统快捷入口`, `Group 摘要`, or `最近事件摘要` as always-visible headings.
* Abnormal state does not render a full `系统全局指标` card strip.
* Hero facts are not four boxed definition-list cards.
* The actionable queue and deep links still work.
* Normal state uses a compact management overview with a small number of obvious links.

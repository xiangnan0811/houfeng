# Update node chart tooltip timestamps

## Goal

Make node detail trend chart hover values match the new 5-second reporting cadence by showing timestamps at second precision in chart tooltips, while keeping axis tick labels at the current minute-level precision for readability.

## Requirements

* Update node detail page trend chart tooltip timestamps to include seconds.
* Keep chart axis tick labels unchanged at minute precision.
* Apply the change to all trend charts on the node detail page that expose hover tooltip timestamps.
* Reuse existing frontend formatting patterns where possible; do not introduce new dependencies or charting libraries.
* Keep UI copy and formatting consistent with the existing Chinese, high-density engineering-tool interface.

## Acceptance Criteria

* [ ] Hovering node detail trend chart data points shows timestamps including seconds.
* [ ] Trend chart axis tick labels still show minute-level labels and do not become visually denser.
* [ ] Existing chart behavior and data values remain unchanged.
* [ ] Relevant frontend tests cover the tooltip-vs-axis timestamp distinction.
* [ ] `make verify-web` passes.

## Definition of Done

* Implementation is on a non-main branch.
* Tests are added or updated for the timestamp formatting behavior.
* No new dependency, CSS framework, or broad chart refactor is introduced.
* PR goes through the normal branch and release follow-through if merged.

## Technical Approach

Locate the node detail trend chart implementation and identify the separate formatting paths for hover tooltip labels versus axis tick labels. Change only the tooltip timestamp formatter to include seconds, leaving axis formatter untouched.

## Decision (ADR-lite)

**Context:** Agent data now reports roughly every 5 seconds, so minute-only tooltip timestamps obscure which sample a hovered point represents. Axis labels still need to remain compact to avoid visual clutter.

**Decision:** Increase tooltip timestamp precision to seconds only on node detail trend charts; keep axis ticks at minute precision.

**Consequences:** Operators can distinguish 5-second samples from hover tooltips without making the chart axes noisy. This does not change backend data cadence, aggregation, sampling, or chart tick density.

## Out of Scope

* Changing chart axis tick format or density.
* Changing backend sampling, retention, or API response shapes.
* Redesigning chart visuals or adding a chart library.
* Applying a global date/time formatting policy change beyond the node detail trend chart tooltip need.

## Technical Notes

* Relevant package: `web`.
* Frontend style and quality constraints come from `.trellis/spec/web/` and `CLAUDE.md`.

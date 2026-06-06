# Implementation Plan

## Checklist

1. Load Trellis web specs and v2 visual authority with `trellis-before-dev`.
2. Inspect `AssetDecisionsPage.tsx`, `AssetDecisionsPage.test.tsx`, `web/src/index.css`, and current API types around manual groups, records, execution plan, and templates.
3. Add local derived helpers for decision path stages and manual group progress.
4. Add page-level decision path / scenario progression surface without changing API calls.
5. Enhance automatic group detail with scenario progression guidance.
6. Enhance manual group detail with progress/readiness summary.
7. Enhance record detail with source continuity and execution/readback framing.
8. Adjust CSS using existing asset-decision class patterns.
9. Update `AssetDecisionsPage.test.tsx` fixtures/assertions for the new surfaces and no-business-write invariant.
10. Update `.trellis/spec/web/state-and-data.md` and `docs/design/v2-houfeng/component-spec.md` with the scenario progression contract.
11. Run checks and visual sanity.
12. Finish Trellis task, commit, push PR, monitor CI, merge when green, and monitor release automation if triggered.

## Validation Commands

- `npm --prefix web run test -- AssetDecisionsPage --run`
- `npm --prefix web run lint`
- `npm --prefix web run build`
- `git diff --check`
- `python3 ./.trellis/scripts/task.py validate 06-06-asset-decisions-scenario-progression`
- Browser/visual sanity for `/asset-decisions?view=needs_decision&renew_within_days=30` on desktop and mobile, using project visual tooling or the in-app browser when local Playwright tooling is unavailable.

## Risky Files

- `web/src/pages/AssetDecisionsPage.tsx`: large component; keep helpers localized and avoid broad refactors.
- `web/src/index.css`: avoid global token or layout changes.
- `web/src/pages/AssetDecisionsPage.test.tsx`: keep existing coverage intact while adding focused assertions.
- `.trellis/spec/web/state-and-data.md` and `docs/design/v2-houfeng/component-spec.md`: update contracts, do not rewrite unrelated sections.

## Rollback Points

- If page-level path surface creates visual clutter, keep only modal-level progression summaries.
- If record source navigation cannot be made safe, display source continuity text without CTA.
- If CSS causes density regression, revert to existing asset-decision card/table patterns and keep content changes.

## Explicit Non-Goals

- No backend endpoint or migration.
- No batch execution.
- No automatic VPS/subscription/monitoring/target writes.
- No IP/routing/performance/CPU/IO/oversell logic.
- No broad redesign of other pages.

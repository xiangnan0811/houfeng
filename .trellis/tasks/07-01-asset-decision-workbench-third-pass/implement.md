# Implementation Plan

## Steps

1. Add failing tests for the stricter default cover:
   - automatic group and cost group default do not expose detail nav, member names, raw labels, or per-member actions;
   - manual group default hides edit/member/save/raw surfaces until details are opened;
   - saved record default hides execution/member/source/raw panels until details are opened;
   - group cards no longer expose the metric grid as default list structure.
2. Refactor `AssetDecisionsPage.tsx`:
   - add a stricter cover helper;
   - render detail nav only after explicit detail entry;
   - keep existing detail panels and mutations reachable after expansion;
   - simplify decision group cards.
3. Update `web/src/index.css`:
   - add/adjust cover and compact group-card styles;
   - preserve responsive behavior for desktop and <=390px mobile;
   - avoid new colors outside design tokens.
4. Run targeted tests and fix failures.
5. Run quality gates:
   - `git diff --check`
   - `python3 ./.trellis/scripts/task.py validate 07-01-asset-decision-workbench-third-pass`
   - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
   - `cd web && npm run lint`
   - `cd web && npm run test -- --run`
   - `cd web && npm run build`
6. Browser sanity:
   - start mock API and Vite without adding e2e dependencies;
   - inspect `/asset-decisions?view=cost&renew_within_days=30` on desktop and 390px mobile;
   - open the cost group default modal and then details;
   - record local-only screenshot/geometry evidence under `.tmp/` only.
7. Finish workflow:
   - commit product changes;
   - archive Trellis task and record session;
   - push branch, open PR, monitor checks, fix failures on same branch;
   - merge only after checks pass;
   - monitor main CI and Release Please/release/image publishing if triggered.

## Rollback Points

- If tests reveal the cover helper over-hides required actions, keep the helper but adjust action slots per source type.
- If CSS causes regressions outside asset decisions, limit selectors to `.asset-decision-*`.
- If browser sanity cannot run due local tool/runtime issue, report it as a local evidence gap after all automated checks pass.

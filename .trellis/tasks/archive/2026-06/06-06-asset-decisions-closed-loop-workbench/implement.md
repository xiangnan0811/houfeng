# Implementation Plan

## 1. Pre-Development Context

- Worktree: `/Users/weibo/Code/houfeng/.worktree/asset-decisions-closed-loop-workbench`
- Branch: `worktree/asset-decisions-closed-loop-workbench`
- Base: `origin/main` at `v0.42.0`
- Hooks: `sh scripts/setup-git-hooks.sh` 已在 worktree 内执行。
- Load before editing:
  - `.trellis/spec/guides/branch-workflow-governance.md`
  - `.trellis/spec/guides/cross-layer-thinking-guide.md`
  - `.trellis/spec/guides/code-reuse-thinking-guide.md`
  - `.trellis/spec/web/index.md`
  - `.trellis/spec/web/state-and-data.md`
  - `.trellis/spec/web/component-conventions.md`
  - `.trellis/spec/web/styling-guidelines.md`
  - `.trellis/spec/web/quality-guidelines.md`
  - `docs/design/v2-houfeng/component-spec.md`

## 2. Start Task

Run after planning review:

```bash
python3 ./.trellis/scripts/task.py start 06-06-asset-decisions-closed-loop-workbench
```

## 3. Inspect Existing Code Before Editing

- Re-read focused sections of:
  - `web/src/pages/AssetDecisionsPage.tsx`
  - `web/src/pages/AssetDecisionsPage.test.tsx`
  - `web/src/lib/types.ts`
  - `web/src/lib/api.ts`
  - `web/src/index.css`
  - `scripts/visual_evidence.py`

## 4. Implement Frontend View Model

- Add local derived types/functions near existing helper functions in `AssetDecisionsPage.tsx`:
  - `AssetDecisionNextWorkKind`
  - `AssetDecisionNextWorkItem`
  - `deriveClosedLoopMetrics(...)`
  - `deriveNextWorkItems(...)`
  - small formatting helpers only if existing helpers cannot cover the text.
- Search before adding helpers to avoid duplicating existing `readbackCountSummary`, `recordFollowupOpenCount`, `manualGroupStatusTone`, `readbackStatusTone`, and recommendation rendering.
- Ensure derivation uses only loaded rows and respects loading/error boundaries.

## 5. Add Closed Loop Surface

- Insert a compact closed-loop surface after the current focus summary and before the decision group list, or merge with the existing command panel if that produces less visual duplication.
- Surface should show:
  - next work item list, max 5 rows/cards.
  - item source badge and tone.
  - title, summary, meta.
  - action button opening existing detail flow.
  - empty state when no loaded work item exists.
  - partial error note only when one or more source surfaces failed.
- Use existing atoms/classes where possible (`Badge`, `FilterChip`, `PageStateView`, `btn`, `page-panel`, `asset-table__stack`).
- Avoid new card-in-card layouts; use a full-width panel with compact repeated rows.

## 6. Tighten Existing State Flow

- Remove duplicate `setOpenState('record_id', record.record_id)` in `submitRecordSave`.
- Verify successful transitions:
  - auto group -> record detail.
  - manual group -> record detail.
  - template -> manual group detail.
  - manual group -> template detail.
- If a transition causes URL/open-state drift, fix the smallest local logic path and add a test.

## 7. Tests

- Extend `AssetDecisionsPage.test.tsx` fixtures:
  - record with `execution_readback.status = drift`.
  - record with `execution_readback.status = blocked`.
  - record with `execution_readback.status = needs_evidence`.
  - active manual group.
  - active template.
- Add tests:
  - renders closed-loop/next-work surface.
  - prioritizes drift/blocked/needs_evidence before template fallback.
  - clicking a next-work record opens `/api/asset-decisions/records/{id}` and updates URL state.
  - clicking an auto-group item opens group detail.
  - no fake next-work rows are rendered when corresponding source state errors.
  - readback drift display does not issue business object PATCH requests.
  - existing save record payload and member followup payload remain unchanged.
- Update `scripts/visual_evidence.py` mock fixture only if browser sanity needs stable text/data for the new surface.

## 8. Specs And Docs

- Update `.trellis/spec/web/state-and-data.md`:
  - closed-loop/next-work surface consumes current loaded asset decision rows.
  - it is display-only and cannot write business objects.
  - error/loading boundaries.
- Update `docs/design/v2-houfeng/component-spec.md`:
  - add first-screen closed-loop guidance while preserving auto groups as primary surface.
- Do not update backend spec unless an actual backend contract changes.

## 9. Verification

Run focused checks first:

```bash
git diff --check
cd web && npm run lint
cd web && TMPDIR=$PWD/.tmp npm run test -- --run AssetDecisionsPage api
cd web && npm run build
```

Run broader checks before commit:

```bash
make verify-web
./scripts/verify.sh
```

Visual sanity:

```bash
TMPDIR="$PWD/.tmp/playwright" /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route '/asset-decisions?view=needs_decision&renew_within_days=30' \
  --viewport 1440x1000 \
  --viewport 390x900
```

If the Python/Playwright environment is unavailable, record the exact limitation and rely on unit/build checks.

## 10. Finish

- Run Trellis check guidance.
- Update specs if not already done.
- Commit task artifacts and code on the feature branch.
- Push branch, open PR, monitor PR CI.
- If merged, monitor main CI and release automation if triggered.

## Rollback Points

- If the derived work-item model becomes too broad, keep only record readback + first auto groups and defer template/manual fallback.
- If visual density is poor, keep the surface as a compact list rather than introducing larger cards.
- If backend data is insufficient for a desired priority, do not invent it; remove that priority rule or mark it as future work.

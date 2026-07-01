# Check Notes

## 2026-06-30

- Branch: `ux/asset-decisions-ia-redesign`.
- Scope: front-end IA and presentation only. No backend, API contract, database, or dependency changes.
- Standard browser sanity helper was attempted with:
  `TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5179/ --mock-api asset-workflows --route /asset-decisions --viewport 1440x1000 --viewport 390x900`
  It was blocked because local Python Playwright is not installed.
- Browser fallback: Vite preview on `http://127.0.0.1:5179/`, local mock asset workflow API on `127.0.0.1:8080`, headless Chromium CDP on `9222`.
  Checked `/asset-decisions` at `1440x1000` and `390x900`.
  Evidence: command summary, secondary nav, group scan, and all four secondary entry buttons rendered; old headings and over-certain `证据稳定` copy were absent; document horizontal overflow was `0`.
- Interaction/deep-link CDP check covered:
  `打开记录`, `打开场景`, `查看续费`, `查看单台队列`,
  `record_id`, `manual_group_id`, `template_id`, `view=renewal`, and legacy `view=single_queue`.
- Validation commands passed:
  - `cd web && npm run lint`
  - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
  - `cd web && npm run test -- --run`
  - `cd web && npm run build`
  - `git diff --check`
- Phase 3.3 spec sync updated `.trellis/spec/web/state-and-data.md`:
  - Asset Decisions default IA is now specified as portfolio command summary -> secondary workbench entry -> decision group scan.
  - Records/scenarios/renewal evidence/single queue must be controlled secondary workbenches, not default equal-weight sections.
  - Partial evidence source failures must degrade to an unknown/confirming state and must not claim `闭环稳定`, `证据稳定`, or invent missing evidence from failed sources.
- Fresh validation after spec sync:
  - `git diff --check`
  - `cd web && npm run lint`
  - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
  - `cd web && npm run test -- --run`
  - `cd web && npm run build`

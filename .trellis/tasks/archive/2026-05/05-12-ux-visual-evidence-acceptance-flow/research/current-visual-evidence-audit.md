# Current visual evidence audit

## Scope

Reviewed current UX-6 inputs and constraints:

- `docs/release/core-pages-product-ux-replan.md`
- `docs/release/current-state-and-next-stage-plan.md`
- `docs/release/v1-gap-checklist.md`
- `docs/release/next-phase-plan.md`
- `docs/operations/`
- `.trellis/spec/web/quality-guidelines.md`
- `.trellis/spec/web/styling-guidelines.md`
- `README.md`
- `CLAUDE.md`
- `web/package.json`
- `web/vite.config.ts`

## Findings

### Existing evidence

- `docs/operations/` currently contains five v2 one-time JPEG captures:
  - `Dashboard.jpg`
  - `节点列表页面.jpg`
  - `节点详情页面.jpg`
  - `目标列表页面.jpg`
  - `目标详情页面.jpg`
- These screenshots predate the full UX-1 to UX-5 sequence. They are useful historical v2 evidence but not a repeatable acceptance process for the current core page redesign.
- Asset Ledger pages are not represented in the current screenshot set, even though Asset Ledger is now the primary product path.

### Archived process should stay archived

- `docs/operations/v1-visual-verification.md` and `docs/operations/visual-evidence/` were archived because they were tied to v1-baseline/stitch references.
- Active specs already warn not to revive those paths, but they also still say a formal v2 screenshot workflow is not established.

### Tooling constraints

- `web/package.json` has no Playwright/Cypress/WebDriverIO dependency.
- `.trellis/spec/web/quality-guidelines.md` says not to add e2e/browser screenshot dependencies without a separate decision.
- Local external Playwright CLI may be available on some machines, but the repo should not rely on it for CI or required verification.
- `web/vite.config.ts` proxies `/api/*` to `VITE_API_TARGET`, defaulting to `http://127.0.0.1:8080`. Preview instructions should mention this so visual review is not blocked by silent API proxy mistakes.

### Current process gap

- Recent UX tasks started dev servers and sometimes used temporary Playwright sanity checks, but the requirement is scattered in task PRDs and journal entries.
- There is no active operations doc that tells future agents:
  - which pages count as core UX evidence,
  - what minimal evidence is required for a UI PR,
  - where to save screenshots when screenshots are captured,
  - how to report preview URL and limitations,
  - how to distinguish visual sanity from automated test quality.

## Recommendation

1. Add an active `docs/operations/v2-visual-evidence.md`.
2. Define three evidence levels:
   - Preview URL required for any user-visible UI change.
   - Browser sanity required for core page UX changes.
   - Screenshot capture required when the page structure, first viewport, density, theme, or cross-page workflow materially changes.
3. Use an evidence matrix for current core pages and a task evidence log template for future PRs.
4. Keep screenshots under `docs/operations/` with explicit names or a future `docs/operations/v2-visual-evidence/` directory, but do not move existing archived v1 assets back.
5. Update Trellis specs so future sessions know UX evidence is now defined, not pending.

## Constraints

- Do not introduce new repo dependencies.
- Do not claim screenshot evidence if no screenshot is actually captured.
- Do not treat Vitest/build as visual proof.
- Do not tie v2 evidence to archived v1/stitch references.
- Do not include release/publish workflow in UX-6.

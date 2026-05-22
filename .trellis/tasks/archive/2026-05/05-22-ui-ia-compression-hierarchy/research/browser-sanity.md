# Browser sanity: IA compression and hierarchy correction

- **Date**: 2026-05-22
- **Scope**: Dashboard, Nodes, VPS, Asset Decisions after correction pass
- **Goal**: verify less glow, less gradient, less explanation; more hierarchy, rhythm, workflow
- **Method**: local browser review was attempted by research agent against the running Vite dev server, then code/build sanity was rechecked after fixes. Full `make verify-web` passed after the blocking Nodes parse error was fixed.

## Verdict

**PASS with caveats.**

The initial browser pass found a blocking `/nodes` Vite transform error caused by missing commas in `nodeHelpers.ts`. That issue was fixed by the check pass, and the final `make verify-web` completed successfully: lint, 63 Vitest files / 495 tests, TypeScript build, and Vite build all passed.

## Route notes

### Dashboard `/`

- Main hierarchy is clearer: `今日第一步` remains the primary action surface.
- Dashboard copy has been compressed into status labels, counts, and queue rows rather than explanatory paragraphs.
- Refresh and auto-refresh stay visually secondary.
- Decorative noise is reduced: first-screen gradients/glow are not driving hierarchy.

### Nodes `/nodes`

- Blocking route error from the first browser pass is resolved by the final build.
- Hero and support copy are shorter; repeated explanations across Hero / SupportSurface / Toolbar were removed or compressed.
- Quick view + list remain the primary scanning workflow; batch, trend, refresh, and advanced filters stay secondary.
- Caveat: with severe incidents and global alert visible, the app shell plus alert still consume narrow-viewport vertical space before the list workflow.

### VPS `/vps`

- Inventory lens and table workflow are more prominent than explanatory text.
- Subscription evidence and field-filter context are compact labels.
- Create drawer copy was reduced while preserving form grouping and behavior.
- Caveat: mobile/narrow layout remains a usable desktop-workbench compression, not a mobile-first redesign.

### Asset Decisions `/asset-decisions`

- Priority queue remains the primary surface.
- Evidence boundary stays available via details, with shorter items.
- Hero links were downgraded to ghost buttons; row `处理` action is primary.
- Renewal evidence copy was reduced; candidate evidence remains secondary.

## Verification evidence

- Targeted tests after final copy edits:
  - `cd web && TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npx vitest run src/pages/DashboardPage.test.tsx src/pages/NodesPage.test.tsx src/pages/VPSPage.test.tsx src/pages/AssetDecisionsPage.test.tsx`
  - Result: 4 files / 49 tests passed.
- Full verification:
  - `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web`
  - Result: passed lint, tests, TypeScript build, and Vite build.
- Environment caveats:
  - Local Node is `v24.14.1`; `web/package.json` requires `22.x`.
  - `npm ci` reports one moderate audit item; dependencies were not changed for this UI/IA task.

## Remaining caveats

- Local sample data includes severe incidents, so the global critical alert increases first-screen density across all routes.
- Narrow viewport checks should be treated as “workflow remains usable,” not as a mobile-first redesign acceptance.

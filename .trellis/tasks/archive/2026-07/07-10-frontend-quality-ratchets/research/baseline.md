# Task 10 Fresh Baseline

## Coordinate

- Date: 2026-07-11 (Asia/Shanghai)
- Base: `origin/main@5633102739d22f18ae7c52c89e19b6e7d2f2a4d7`
- Branch/worktree: `codex/frontend-quality-ratchets` at `/home/murray/code/houfeng/.worktree/frontend-quality-ratchets`
- Runtime: Node `22.23.1`, npm `10.9.8`
- Git status before activation: clean; local HEAD and `origin/main` were identical.

## Dependency Gate

- Direct dependencies `frontend-csp-compat`, `frontend-accessibility-contracts`, `frontend-responsive-workflows` and `frontend-css-ownership` are archived.
- Task 9 consumed archived `frontend-asset-decisions-domains`; its implementation PR #365, release `v0.58.4`, post-release main CI and multi-arch image publication are recorded in the archived verification artifact.
- Base main CI run `29154612059` passed Go, Web and Docker image on `5633102739d22f18ae7c52c89e19b6e7d2f2a4d7`; Release Please run `29154612051` succeeded without opening another release PR.

## Full Baseline Gate

`env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify` passed:

- all Go fmt/vet/tests;
- ESLint with zero errors/warnings;
- 115 Vitest files / 769 tests;
- strict TypeScript build and Vite production build;
- npm install/audit with 0 vulnerabilities.

`npm --prefix web run css:analyze`, explicit `npm audit --include=dev`, task validation and `git diff --check` also passed.

## Production Measurements

All gzip values use Node `zlib.gzipSync(..., {level: 9})` against the fresh `web/dist`.

| Metric | File / scope | Actual bytes |
| --- | --- | ---: |
| Entry JS raw | `assets/index-RxOh6isP.js` | 355,132 |
| Entry JS gzip | `assets/index-RxOh6isP.js` | 110,565 |
| Entry CSS raw | `assets/index-D2jwkOe5.css` | 289,964 |
| Entry CSS gzip | `assets/index-D2jwkOe5.css` | 37,135 |
| Largest async JS raw | `assets/AssetDecisionsPage-CT00pswq.js` | 136,551 |
| Largest async JS gzip | `assets/AssetDecisionsPage-CT00pswq.js` | 31,953 |
| WOFF2 raw | 7 self-hosted fonts | 139,072 |

Task 9 CSS analyzer remains the sole CSS budget owner:

| Metric | Actual |
| --- | ---: |
| Source files / bytes | 26 / 311,063 |
| Rules / declarations | 2,107 / 8,517 |
| Repeated selectors / literal colors / `!important` | 151 / 247 / 11 |
| Production raw / gzip | 293,270 / 38,119 |

## Coverage Baseline And Ratchet

The exact `@vitest/coverage-v8@4.1.5` provider now inventories every production
`src/**/*.{ts,tsx}` file, including unimported files such as `src/main.tsx` at
0%, while excluding only colocated tests, declarations, setup code and audited
test fixtures. The first credible run established the committed global floor:

| Metric | Initial covered / total | Initial floor | Final verified covered / total | Final verified |
| --- | ---: | ---: | ---: | ---: |
| Statements | 8,237 / 10,408 | 79.14% | 8,281 / 10,408 | 79.56% |
| Branches | 6,821 / 9,706 | 70.27% | 6,898 / 9,706 | 71.06% |
| Functions | 2,668 / 3,382 | 78.88% | 2,671 / 3,382 | 78.97% |
| Lines | 7,342 / 8,851 | 82.95% | 7,354 / 8,851 | 83.08% |

Final critical-file branch evidence from 117 files / 820 tests:

| Critical owner | Covered / total | Branch coverage |
| --- | ---: | ---: |
| `src/lib/modalStack.ts` | 18 / 19 | 94.73% |
| `src/lib/useModalFocus.ts` | 71 / 78 | 91.02% |
| `src/components/atoms/Modal.tsx` | 27 / 27 | 100% |
| `src/pages/dashboard/dashboardRemoteState.ts` | 0 / 0 | 100% |
| `src/pages/dashboard/dashboardModel.ts` | 117 / 127 | 92.12% |
| `src/lib/apiRequest.ts` | 9 / 9 | 100% |
| `src/lib/auth-client.ts` | 10 / 10 | 100% |
| `src/lib/auth-context.tsx` | 2 / 2 | 100% |
| `src/pages/asset-decisions/hooks/useAssetDecisionRouteState.ts` | 28 / 30 | 93.33% |
| `src/pages/asset-decisions/hooks/useAssetDecisionPortfolio.ts` | 11 / 12 | 91.66% |
| `src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts` | 33 / 35 | 94.28% |
| `src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts` | 103 / 113 | 91.15% |
| `src/pages/asset-decisions/hooks/useAssetDecisionTemplates.ts` | 61 / 67 | 91.04% |
| `src/pages/asset-decisions/hooks/useAssetDecisionRecords.ts` | 99 / 107 | 92.52% |
| `src/pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.ts` | 58 / 62 | 93.54% |

Fail-closed proofs were run and restored: temporarily raising
`useModalFocus.ts` to 92% failed after all 820 tests passed because its actual
branch coverage was 91.02%; temporarily replacing the required
`modalStack.ts` budget key with a nonexistent path failed the source contract
with `src/lib/modalStack.ts must define a branch threshold`.

## TypeScript Probe Debt

The probes used the main app config with one or both options overridden on the CLI; expected exit code was 2 because the ratchets are not enabled yet.

| Probe | Total | Directory distribution |
| --- | ---: | --- |
| `noUncheckedIndexedAccess` | 176 | pages 75, components 41, styles 34, app 16, lib 7, security 3 |
| `exactOptionalPropertyTypes` | 33 | pages 23, lib 4, app 3, components 3 |
| Both | 209 | pages 98, components 44, styles 34, app 19, lib 11, security 3 |

Combined error codes: TS18048 48, TS2322 16, TS2339 2, TS2345 72, TS2352 1, TS2375 20, TS2379 9, TS2532 39, TS2538 1, TS2790 1.

## Final Owner Inventory

- Modal/focus: `src/lib/modalStack.ts`, `src/lib/useModalFocus.ts`, `src/components/atoms/Modal.tsx`.
- Tabs/panels: `src/components/atoms/Tabs.tsx`, `src/components/atoms/tabIds.ts`.
- Menus/skip link: `src/app/layout/TopBar.tsx`, `UserChip.tsx`, `AppShell.tsx` (`#main-content`).
- Dashboard: `src/pages/dashboard/dashboardRemoteState.ts`, `dashboardModel.ts`, shared fixtures and `DashboardPage.tsx`.
- Asset controllers: `useAssetDecisionRouteState`, `Portfolio`, `Groups`, `ManualGroups`, `Templates`, `Records`, `RenewalQueue`; each exposes `{state, commands}` and only route state owns URL mutation.
- CSS: `scripts/analyze-web-css.mjs`, `web/css-budget.json`, `web/css-owners.json`, 26 uniquely owned source CSS files.

## External Configuration State

- GitHub Actions default workflow permission is `read`; pull-request approval permission is disabled.
- No GitHub `staging` environment, environment variable or environment secret exists yet.
- GitHub API reports `main` is not currently protected. Task 10 must add required checks after the new browser check has appeared, and must configure the staging environment to allow deployments from `main` only.
- Missing staging URL/credentials is not a mock/browser implementation blocker, but it remains a hard blocker for the authenticated staging acceptance criterion, Gate C closure and task archive.

# 质量 Ratchet 与浏览器门设计

## 1. Design Principles

1. **失败关闭**：coverage 零匹配、fixture 未声明 API、bundle 无法识别入口、staging 版本不符都必须失败，不能降级成 warning。
2. **一项事实一个 owner**：Task 9 拥有 CSS AST；Task 10 拥有 coverage、bundle/font、正式 browser/axe/CSP CI。不要复制 analyzer 或 baseline。
3. **同一命令本地可复现**：CI 只编排 repository scripts/Make targets，不在 YAML 内藏一套不同逻辑。
4. **分清证据层级**：Vitest/fixture Playwright、部署 staging real-data、staging fault injection 分别记录，互不冒充。
5. **阈值来自 fresh baseline**：全局 coverage 与 bundle/CSS 预算取前置任务合并后的真实值；只有明确的高风险模块采用预先批准的 90% branch 目标。
6. **不以截图代替合同**：稳定 selector、role、focus、geometry、overflow 和 diagnostics 是 CI 断言；截图/trace 是失败诊断和 staging 审计附件。

## 1.1 Alternatives Considered

### A. Repository Playwright + Separate Browser Job（推荐）

优点是 production preview、CSP header、真实 Chromium focus/layout 与 PR required check 在同一可复现入口；fixture 可 fail closed，staging 可复用 support code。代价是新增约 1 个 browser download/cache 和独立 CI 时间。本设计采用此方案。

### B. 把现有 Python `visual_evidence.py` 直接搬进 CI

优点是复用现有 mock profiles、初始改动少；缺点是 Python Playwright 当前是机器级临时依赖，Node lockfile 无法固定 browser/toolchain，脚本同时承担大量 fixture 与 browser orchestration，required check 的依赖边界不清。保留它做 local/real-data helper，但不作为正式 CI owner。

### C. 只增加 Vitest/axe/jsdom，不跑真实浏览器

优点是最快且无 browser cache；缺点是无法捕获 CSP、真实 focus/inert、viewport clipping、document overflow、字体/资源和 console/network 错误，正好遗漏本轮多个 P1/P2 根因，因此不满足需求。

## 2. Target Architecture

```text
make verify-web
  -> toolchain contract / Node 22.23.1
  -> npm ci --include=dev
  -> ESLint (含 lib + Asset hooks type-aware rules)
  -> Vitest + V8 coverage + thresholds
  -> tsc -b (两项额外 strict flags 已进入主 tsconfig)
  -> Vite production build
  -> bundle/font budget
  -> Task 9 CSS AST budget

CI browser job
  -> npm ci
  -> Playwright-compatible Chromium install
  -> production build + Vite preview (精确 CSP header)
  -> fail-closed mock API fixtures
  -> route/state/a11y/keyboard/responsive/security specs
  -> failure-only report/screenshot/trace artifact

dispatch-only staging workflow
  -> assert /api/healthz.version == requested release
  -> real authentication and real-data baseline
  -> reversible UI mutation + cancel-only dangerous flow
  -> explicitly labelled frontend fault/state injection
  -> sanitized evidence artifact
```

## 3. Planned File Ownership

### Repository Gate Files

- Modify: `Makefile`, `.github/workflows/ci.yml`, `web/package.json`, `web/package-lock.json`, `web/.gitignore`.
- Modify: `web/vitest.config.ts`, `web/tsconfig.app.json`, `web/eslint.config.js`.
- Create: `web/coverage-budget.json`, `web/bundle-budget.json`.
- Create: `scripts/check-web-bundle-budget.mjs` and its deterministic synthetic-fixture test.
- Consume, do not duplicate: Task 9 `scripts/analyze-web-css.mjs` and `web/css-budget.json`.

### Browser Files

- Create: `web/playwright.config.ts`, `web/playwright.staging.config.ts`.
- Create: `web/e2e/fixtures/{contracts,profiles,router}.ts`.
- Create: `web/e2e/support/{diagnostics,geometry,auth}.ts`.
- Create: `web/e2e/{core-routes,page-states,accessibility,visual-contracts,security}.spec.ts`.
- Create: `web/e2e/staging/staging-smoke.spec.ts`.
- Approved: `.github/workflows/frontend-staging-smoke.yml`（用户已于 2026-07-10 确认机制）。

### Minimal Production Refactor

- Create: `web/src/lib/apiRequest.ts` and colocated test if the formal baseline confirms that whole-file `api.ts` threshold cannot express “request helpers”.
- Modify: `web/src/lib/api.ts` to import/re-export compatible primitives; modify `auth-client.ts` import only if necessary.
- No business API function signature, URL, method, body, credentials, 401 hook or error text may change.

### Specs And Operations

- Update through `trellis-update-spec`: all affected `.trellis/spec/web/*.md`.
- Modify: `docs/operations/ui-preview-and-browser-sanity.md`.
- Update evidence only after success: this task `implement.md` and parent Gate C section.

## 4. Coverage Design

### 4.1 Collection Boundary

`vitest.config.ts` uses V8 with an explicit production include:

```ts
include: ['src/**/*.{ts,tsx}']
exclude: [
  'src/**/*.test.{ts,tsx}',
  'src/test/**',
  'src/**/*.d.ts',
  'src/**/*TestFixtures.{ts,tsx}',
]
```

The exact excludes are reviewed against the final tree. A production module cannot be excluded merely because it is difficult to test. `reportsDirectory` is ignored; reporters include `text`, `json-summary` and `lcov` or equivalent machine-readable output.

### 4.2 Budget Shape

`web/coverage-budget.json` stores the audited values rather than hiding them in CI:

```json
{
  "global": {
    "statements": 0,
    "branches": 0,
    "functions": 0,
    "lines": 0
  },
  "critical": {
    "src/lib/modalStack.ts": { "branches": 90 }
  }
}
```

The zeros above describe shape only and are never committed as a baseline. Implementation replaces them with the first successful fresh measurements. `vitest.config.ts` reads the JSON and maps `global` plus each exact critical path into Vitest `coverage.thresholds`; `autoUpdate` stays false.

A contract test asserts:

- every critical key is an exact existing production file;
- Modal stack/focus, Dashboard model/RemoteState, request transport, auth client/context and all command-owning Asset hooks are present;
- no critical branch threshold is below 90;
- no global metric is missing or negative.

This prevents a rename or Task 8 hook split from silently matching zero files.

### 4.3 API Request Transport Seam

The current `api.ts` mixes approximately 150 lines of transport/error/JSON primitives with hundreds of endpoint wrappers. A 90% branch threshold on the entire 968-line façade would measure query variants rather than the approved “API request helpers”. The allowed seam is:

```ts
// apiRequest.ts
export class ApiError extends Error { ... }
export function setUnauthorizedHandler(...): void
export function requestJSON<T>(...): Promise<T>
export function requestEmpty(...): Promise<void>
export function postJSON<T>(...): Promise<T>
export function postJSONBody<T>(...): Promise<T>

// api.ts
export { ApiError, requestJSON, requestEmpty, ... } from './apiRequest'
```

`api.test.ts` transport cases move or are shared without weakening endpoint assertions. RED tests freeze credentials, cache, headers, 401 callback, empty/error bodies, malformed error JSON and successful JSON parsing before extraction.

## 5. Browser Harness Design

### 5.1 Runtime

- `@playwright/test` and `@axe-core/playwright` are exact dev dependencies in the lockfile.
- One Chromium project only; locale `zh-CN`, deterministic timezone, fixed color scheme/default viewport per project override.
- `webServer` starts `vite preview` on an explicit loopback port after a production build. `reuseExistingServer` is false in CI.
- CI sets `forbidOnly`, zero retries initially, bounded workers and deterministic timeouts. Flakes are fixed, not hidden behind retries.
- Browser cache key includes runner OS and `web/package-lock.json`; `playwright install --with-deps chromium` still verifies OS dependencies.

### 5.2 Fail-Closed API Router

Each profile is a typed table keyed by `METHOD pathname canonical-query`. Before navigation, tests route `**/api/**` and provide:

- `/api/auth/me` authenticated fixture;
- `/api/dashboard` shared Task 3 Dashboard fixtures;
- only the endpoints needed by the selected route/workflow;
- explicit delay, network failure or status response when a state test requests it.

Unknown requests receive a recognizable test-only 501 response and are appended to `unexpectedRequests`. `afterEach` fails if the list is non-empty. There is no catch-all `[]`, `{}` or `null` fallback.

Request expectations include method/path/query and, for mutations, sanitized body keys. This catches URL-state or minimum-refresh regressions without asserting secrets.

### 5.3 Diagnostics Collector

An init script installs listeners before application code:

- `securitypolicyviolation` -> directive, blocked origin/path, disposition;
- `unhandledrejection` -> redacted error class/message;
- page `console` -> error messages;
- Playwright `pageerror`, `requestfailed`, HTTP status >= 400.

Every test calls one final `expectDiagnosticsClean()` in `finally`/fixture teardown. Expected 503/404 injections are declared by method/path/status per test; all other errors fail. Authentication redirects, aborted route chunks and favicon failures are not broadly ignored—any necessary exception must be exact and documented.

The main document response must contain exactly the policy read from `internal/center/http/csp-policy.txt`. No duplicate policy string is maintained in Playwright.

### 5.4 Core Matrix

The broad matrix is exactly 27 route/viewport combinations:

| Routes | Viewports |
| --- | --- |
| `/`, `/vps`, `/asset-decisions`, `/monitoring`, `/targets`, `/events`, `/providers`, `/subscriptions`, `/settings` | `1440x1000`, `1024x768`, `390x900` |

Per route assertions are declared in a route manifest: expected URL, main landmark/heading, at least one workflow anchor, and any critical command text. Generic assertions cover nonblank main, no document overflow and no viewport-clipped primary command.

Detail routes remain in `docs/operations` as task-specific expansion points and staging evidence; they are not multiplied into every CI viewport unless a contract requires them.

### 5.5 State And Workflow Suites

- **Page states**: at least one route each proves pending loading, success-empty, structured error with retry, and non-empty success. Dashboard additionally covers critical/abnormal/maintenance/onboarding/stable and VPS 503 false-empty prevention.
- **Keyboard**: skip link moves focus to main; Tabs use ArrowLeft/Right/Home/End and correct panel relation; menus close with Escape and restore focus; nested Modal first Escape closes only top layer, second closes parent, focus/body lock remain correct.
- **Accessibility**: axe scans stable AppShell, Dashboard, Settings, VPS and Asset surfaces after data settles. serious/critical violations are zero; no broad rule disable.
- **Responsive contracts**: Dashboard primary action first viewport; Settings tabs; Asset secondary commands; Provider decision link; local table scroll region focus/label. Assertions use complete visible text and bounding boxes, not pixel snapshots.

## 6. Bundle, Font And CSS Budget Design

`scripts/check-web-bundle-budget.mjs` reads `web/dist/index.html` and hashed `web/dist/assets` after the production build.

Metrics:

1. entry JS gzip bytes from the module script referenced by `index.html`;
2. entry CSS gzip bytes from the stylesheet referenced by `index.html`;
3. maximum gzip bytes among non-entry JS chunks;
4. total raw bytes for `dist/fonts/**/*.woff2`.

The script uses one deterministic Node `zlib.gzipSync` configuration both for baseline and checks. It validates exactly one entry JS, at least one entry CSS, at least one async chunk and the expected nonzero font set. It reports top offenders on failure.

`web/bundle-budget.json` is generated only through an explicit `--write-baseline` mode. Default mode never edits it. Any PR increasing a value must contain the build diff, reason, owner and an explicit budget-file hunk; CI cannot auto-bump.

Task 9's `npm run css:analyze` remains the sole CSS source/AST/raw/gzip budget. Task 10 wires both checks after `npm run build` in `make verify-web` and CI.

## 7. TypeScript And Type-Aware ESLint Design

### 7.1 Staged Compiler Profiles

Temporary profiles extend `tsconfig.app.json` and enable both options for progressively larger root sets:

1. `tsconfig.ratchet-lib.json` -> `src/lib/**`;
2. `tsconfig.ratchet-atoms.json` -> atoms plus transitive lib;
3. `tsconfig.ratchet-dashboard.json` -> dashboard plus transitive shared modules;
4. route profiles or one final app profile -> remaining app/pages.

Imports remain type-checked transitively. Each stage is committed only after zero errors and full tests. When the final app profile is green, both options move into `tsconfig.app.json` and temporary profiles are deleted. This leaves one permanent compiler truth, not a growing forest of profiles.

Fix policy:

- guard indexed reads and prove array/object membership;
- omit optional properties instead of passing `undefined` when wire semantics require omission;
- preserve explicit `null` where backend PATCH distinguishes clear from omitted;
- use discriminated unions/exhaustive switches rather than assertions;
- test any changed request body and UI branch.

### 7.2 ESLint

A flat-config block with type information targets `src/lib/**/*.{ts,tsx}` and `src/pages/asset-decisions/hooks/**/*.{ts,tsx}`. It enables an explicit, reviewable rule set such as floating/misused promises, await-thenable, unnecessary assertions and switch exhaustiveness rather than importing a noisy global preset and disabling half of it.

`tsconfigRootDir` and project service/config are explicit. Generated/config/e2e files receive the correct Node/browser globals in separate blocks. Lint remains zero-warning and is still invoked by the normal `lint` script.

## 8. CI Design

### Existing `web` Job

`make verify-web` remains the command portal and adds coverage/type/budget/CSS checks without duplicating logic in YAML. Toolchain source tests are extended to prove the new package scripts and CI Node pin remain connected.

### New `web-browser` Job

- checkout -> setup Node from `.node-version` with npm cache -> `npm ci --include=dev`;
- cache/install matching Chromium -> build/preview -> `npm run test:e2e`;
- upload Playwright report, screenshots and traces only on failure, with short retention;
- no secrets and no staging access, so fork/PR runs are safe.

Both jobs are independently required. Browser failures do not get hidden inside a long generic web log.

## 9. Staging Evidence Design

### Approved Workflow

用户已于 2026-07-10 确认此方案。`frontend-staging-smoke.yml` is `workflow_dispatch` only, uses GitHub environment `staging`, and accepts `expected_version`. It reads:

- variable `HOUFENG_STAGING_BASE_URL`;
- secrets `HOUFENG_STAGING_USERNAME`, `HOUFENG_STAGING_PASSWORD`.

Because the repository is public and a manual dispatch can select a ref, the environment's deployment policy admits only `main`. A secret-free preflight rejects any other ref before the environment job is eligible; repository workflow permissions remain `contents: read`. This prevents a modified feature-ref workflow from becoming a credential path rather than relying on authors to select the right branch.

The workflow uses one `frontend-staging-smoke` concurrency group with `cancel-in-progress: false`. Runs serialize because the real lane temporarily mutates one approved Settings field, and a new run must never cancel an older run between save and restore. The workflow fails with missing variable names only; it never prints values. `/api/healthz.version` must match `expected_version` before login.

### Evidence Lanes

1. **Real environment lane**: real login, actual Dashboard and core routes, actual response headers, theme, cancel-only nested confirmation, reversible Settings save/restore.
2. **Deployed-frontend injection lane**: after real login on the same deployed assets/origin, intercept selected GETs for five Dashboard modes, 503, delayed response, long text and large list. The report labels these as injected UI resilience cases.

Network evidence stores method, origin-relative sanitized path, status and timing only. Header evidence allowlists CSP, content-type and other non-secret security/cache headers. Success screenshots and a JSON/Markdown manifest are uploaded as `frontend-staging-audit-<run-id>` with 30-day retention; traces and raw response bodies are disabled for staging to avoid secret leakage. Artifact expiry does not erase the decision record because the task/parent permanently store the run URL/id、artifact name、expected/observed version、counters and conclusion.

If environment or credentials are absent, implementation/CI commits may merge, but staging AC, Task 10 archive and parent Gate C remain open. After a successful release-bound run, its run URL, artifact name, expected/observed version and result are written to this task and the parent in a small evidence PR before archive.

## 10. Compatibility And Migration

- Existing `Modal`, Dashboard, API business functions and route URLs keep their public interfaces.
- Existing Python `visual_evidence.py` remains available for local/real-data investigation; docs stop calling it the only browser route.
- No backend schema or JSON wire change is required.
- No tracked raster baselines are introduced; existing repository image-ignore policy stays intact.
- Task 9 CSS paths/budgets are consumed exactly as merged; Task 10 does not predict their final filenames.

## 11. Rollback Strategy

- Coverage dependency/config/baseline, API transport seam, type ratchets, bundle budget, browser harness, CI wiring and specs are separate commits.
- If a gate exposes a real regression, fix the regression. If the gate itself is wrong, revert only its owning commit and preserve already-valid gates.
- CI duration is addressed with cache, worker bounds or spec sharding; never by deleting CSP/Modal/Dashboard/a11y assertions.
- Budget increases require explicit baseline commit rollback/review; default scripts cannot mutate budgets.
- Staging settings mutation restores original state in `finally`; restore failure is an operational blocker, not a reason to mark smoke passed.

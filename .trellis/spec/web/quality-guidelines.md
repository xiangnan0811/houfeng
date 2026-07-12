# 质量规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风前端的质量门是一条命令：`make verify-web`。**CI 与本地跑同一套 Makefile target**，`.github/workflows/ci.yml:29` 直接 `run: make verify-web`，没有任何 CI-only lint 偏方。

跨前后端改动（同时动了 `internal/` / `agent/` 与 `web/`）使用 `./scripts/verify.sh` 一把跑完，等价于 `make verify-go && make verify-web`（参考 `.trellis/spec/backend/quality-guidelines.md` 的"命令门户"段）。

不存在以下东西，**不要新增**（除非通过设计基线评审）：

- ❌ Prettier（仓库 `find . -name '.prettierrc*' -maxdepth 3` 为空）
- ❌ husky / lint-staged / lefthook 等 git hook 工具（同样为空）
- ❌ stylelint / PostCSS transform 配置（纯 CSS 不做预处理；direct `postcss` 只服务 AST inventory/contract）
- ❌ Cypress / WebDriverIO、跨浏览器矩阵和像素 golden；仓库固定使用 lockfile 内的 Playwright Chromium 做合同测试，不维护截图 diff 基线
- ❌ 任何 codegen（前端类型与 Go contract 手对齐，详见 `.trellis/spec/web/state-and-data.md`）

---

## 命令门户

实读 `web/package.json:9-15`：

| 命令 | 实际行为 | 何时用 |
|------|----------|--------|
| `npm run dev` | `vite` —— 起 dev server，`/api/*` 反代到 `VITE_API_TARGET`（默认 `http://127.0.0.1:8080`，见 `web/vite.config.ts:11-23`） | 本地开发 |
| `npm run build` | `tsc -b && vite build` —— 严格 TS 多项目 build + Vite 产物到 `web/dist/` | 提交前必跑（CI 也跑），产物由 center 通过 `HOUFENG_WEB_DIST_DIR` 吐给浏览器 |
| `npm run test:coverage` | Vitest V8 覆盖全部 production TS/TSX，并读取 `coverage-budget.json` 阻断全局或关键文件回退 | `make verify-web` 与 CI；本地改关键模块时先 focused test 再跑 |
| `npm run css:analyze` | PostCSS AST owner/debt inventory + source/production budget ratchet | production build 后运行；已由 `make verify-web` 接入 CI |
| `npm run bundle:check` | 校验入口 JS/CSS gzip、最大 async JS gzip 与全部 WOFF2 raw budget | production build 后运行；默认只读 `bundle-budget.json` |
| `npm run lint` | `eslint .` —— 跑 `web/eslint.config.js` 的 flat config | 提交前必跑 |
| `npm run test` | `vitest` —— 默认进 watch（CI 用 `--run` 一次性跑完） | 本地反复跑测；CI 用 `npm run test -- --run` |
| `npm run test:e2e` | 先 production build，再用固定 Chromium 跑 fail-closed fixture browser gate | 本地完整浏览器验证与独立 `web-browser` CI job |
| `npm run test:e2e:staging` | 使用 `playwright.staging.config.ts` 对已部署 staging 做真实认证审计 | 只由受保护的 manual staging workflow 调用 |
| `npm run preview` | `vite preview` —— 看本地 build 产物效果 | 偶尔做产物 sanity check |

**Makefile 端**（`Makefile:92-104`）：

```make
test-web-toolchain:
	@scripts/check-web-toolchain.test.sh
	@scripts/check-web-quality-gates.test.sh

verify-web: test-web-toolchain
	@scripts/check-web-toolchain.sh
	@if [ -f web/package.json ]; then \
		env -u NODE_ENV $(NPM) --prefix web ci --include=dev && \
		NODE_ENV=test $(NPM) --prefix web run lint && \
		NODE_ENV=test $(NPM) --prefix web run test:coverage && \
		NODE_ENV=production $(NPM) --prefix web run build && \
		$(NPM) --prefix web run bundle:check && \
		$(NPM) --prefix web run css:analyze; \
	else \
		echo 'web workspace not initialized yet'; \
	fi
```

注意：

- **`make verify-web` 会跑 source/toolchain contract、lint、coverage、strict TS+Vite build、bundle/font 与 CSS AST budget**。正式 Chromium gate 作为独立 `web-browser` job 运行，避免浏览器下载与普通 source gate 混在一份日志里。
- `npm ci` 每次清空 `node_modules` 重装，本地反复跑会比较慢；本地速度优先时直接 `cd web && npm run test -- --run` / `npm run build` 即可，CI 仍走完整 `verify-web`。
- `.node-version` 是本地与 CI 的唯一精确 runtime pin；`actions/setup-node@v6` 必须通过 `node-version-file: .node-version` 读取它。`web/package.json` 的 `engines.node = "22.x"` 只声明兼容范围。

## Scenario: 可重复前端质量门

### 1. Scope / Trigger

- 修改 Node runtime、npm lockfile、TypeScript compiler options、`verify-web` 或 Web CI 时，必须同时遵守本节。

### 2. Signatures

- `make test-web-toolchain`：验证 runtime pin、CI 引用、tsconfig strict、Node types、quality gate wiring 与 preflight 行为。
- `make verify-web`：source Web gate；先执行 toolchain/source contract，再 install、lint、coverage、build、bundle/font 和 CSS AST budget。
- `scripts/check-web-toolchain.sh`：无参数；Node major 非 22 时返回 1。

### 3. Contracts

- `.node-version` 固定当前批准的 Node 22 LTS patch；CI 不再另外写一份 Node 版本。
- install 必须清除调用者 `NODE_ENV` 并显式包含 devDependencies。
- lint/test 固定 `NODE_ENV=test`，build 固定 `NODE_ENV=production`。
- `tsconfig.app.json` 同时启用 `strict`、`noUncheckedIndexedAccess`、`exactOptionalPropertyTypes`；`tsconfig.node.json` 也显式 `strict: true`，`@types/node` 保持 `^22`。
- Bash 脚本从 Make 直接执行以遵守 shebang；不要用 `sh script` 覆盖 Bash interpreter。

### 4. Validation & Error Matrix

| 条件 | 结果 |
|------|------|
| Node major 不是 22 | install 前失败，输出 `web requires Node 22.x; found vX.Y.Z` |
| 调用者设置 `NODE_ENV=production` | 仍安装 devDependencies，并完成 lint/test/production build |
| `.node-version`、CI、tsconfig 或 Node types 漂移 | `test-web-toolchain` 在 npm install 前失败 |
| lint、coverage、build 或预算任一失败 | `verify-web` 原样返回非零，不继续掩盖失败 |

### 5. Good / Base / Bad Cases

- Good：Node 22.23.1 下 `NODE_ENV=production make verify-web` 与无 `NODE_ENV` 调用结果一致。
- Base：开发者可单独运行 `npm --prefix web run test -- --run` 做 focused feedback，提交前仍跑完整 coverage/source gate 与相关 browser suite。
- Bad：依赖调用 shell 恰好未设置 `NODE_ENV`，或在 workflow 里维护另一份 `node-version: 22`。

### 6. Tests Required

- fake Node 22 通过、fake/real Node 24 被拒绝，并断言完整错误文本。
- 断言 `.node-version`、setup-node `node-version-file`、两个 strict 配置和 `@types/node` 一致。
- CI 以 `NODE_ENV=production make verify-web` 执行，断言 coverage、production build 与静态预算均通过；`scripts/check-web-quality-gates.test.sh` 还必须证明独立 browser job 的 Node pin/install/test 位于同一 job block。

### 7. Wrong vs Correct

```make
# Wrong: 继承调用环境，并用 sh 覆盖 Bash shebang
	@sh scripts/check-web-toolchain.sh
	@cd web && npm ci && npm run test -- --run

# Correct: 脚本直接执行，每个阶段拥有明确环境
	@scripts/check-web-toolchain.sh
	@env -u NODE_ENV npm --prefix web ci --include=dev
	@NODE_ENV=test npm --prefix web run test -- --run
```

### Scenario: coverage、Chromium 与 staging 审计门

#### 1. Scope / Trigger

- 修改 production TS/TSX、coverage/bundle/CSS budget、十条核心路由、CSP、Modal/Tabs/Menu、响应式布局、Playwright fixture、CI browser job 或 staging workflow 时，必须使用本合同。

#### 2. Signatures

- `npm --prefix web run test:coverage`：V8 provider；production include 固定为 `src/**/*.{ts,tsx}`，阈值来自 `web/coverage-budget.json`。
- `npm --prefix web run test:e2e`：`web/playwright.config.ts`；Chromium、production preview `127.0.0.1:4175`、十 route × 三 viewport。
- `npm --prefix web run test:e2e:staging`：`web/playwright.staging.config.ts`；不启动本地 server，只访问 `HOUFENG_STAGING_BASE_URL`。
- `.github/workflows/frontend-staging-smoke.yml`：仅 `workflow_dispatch(expected_version)`，`environment: staging`。
- staging env：variable `HOUFENG_STAGING_BASE_URL`；secrets `HOUFENG_STAGING_USERNAME`、`HOUFENG_STAGING_PASSWORD`；input 映射为 `HOUFENG_EXPECTED_VERSION`。
- `StagingAudit.waitForRequestsToSettle(): Promise<void>`：跟踪 page 的 `request` / `requestfinished` / `requestfailed` 生命周期；默认要求 500ms 持续空闲，30s 内不能 settle 时以脱敏 method/path 失败。

#### 3. Contracts

- coverage 全局下限是 statements `79.14`、branches `70.27`、functions `78.88`、lines `82.95`；`modalStack`、`useModalFocus`、`Modal`、Dashboard RemoteState/model、`apiRequest`、auth client/context 与七个 Asset controller 的 branch threshold 均不低于 90%，路径不存在也必须失败。
- fixture router 以 method + canonical path/query 精确匹配；未知 API 返回 501 并在 teardown 失败，mutation fixture 必须声明 body key 集合。loading 使用受控 Promise，不用固定 sleep。
- broad browser matrix 固定 `/`、`/vps`、`/asset-decisions`、`/monitoring`、`/targets`、`/events`、`/command-audit`、`/providers`、`/subscriptions`、`/settings` × `1440x1000`、`1024x768`、`390x900`。每项断言 main/workflow、document overflow、关键命令裁切与统一 diagnostics。
- `/command-audit` fixture 只声明 exact default `/api/command-audits`，并故意附加 hostile stdout/stderr/details 字段；浏览器必须证明这些字段不进入 DOM，undeclared cursor query 仍返回 501。390px 合同还要证明 named focusable table wrapper 独占横向滚动、event expand 可用、advanced Modal Tab/Escape/focus restore 正常。
- CSP 只读取 `internal/center/http/csp-policy.txt`，main document header 必须精确相等；console/page/request/HTTP/CSP/unhandled rejection 默认全部阻断，只有测试显式声明的 method/path/status 可放行。
- staging real lane 与 deployed-frontend injection lane 必须在 manifest 中分开。真实 lane验证版本、UI 登录、十路由、自定义模板 cancel-only、设置保存/恢复、主题 reload；`/command-audit` 只断言 metadata-only 声明/无 output surface，不要求环境已有审计数据。injection lane验证 Dashboard 五态、503、受控慢响应与长列表三视口，不能冒充后端/生产数据通过。
- staging 设置 mutation 必须串行、先快照、临时 `+1`、readback，并在 `finally` 恢复/readback。workflow 固定 concurrency 且 `cancel-in-progress: false`；非 `main` ref 在读取 environment secrets 前失败。
- staging 的 audited route navigation 在旧文档不是 `about:blank` 时，必须先等待 tracked requests 持续空闲，再等待 `document.fonts.ready`、检查 diagnostics，最后才调用 `page.goto`；新 route 的 heading 可见后还必须再次等待 requests/fonts settle，才检查 layout/diagnostics 和截图。heading 可见只证明 route shell 已渲染，不能证明真实业务数据已完成。否则主动导航会取消旧文档仍在加载的 API/WOFF2，并把 harness 自造的 `net::ERR_ABORTED` 误报成部署失败；不得用宽泛 aborted-request allowlist 掩盖这个时序错误。
- staging 禁止 trace/video/自动截图，并以 `preserveOutput: 'never'` 丢弃 Playwright `error-context` 等内部输出；显式截图必须 mask 登录字段和用户 chip。artifact 只含 allowlisted headers、origin-relative 脱敏 path/status/timing、计数、步骤与截图，不含 cookie、Authorization、密码、token、request/response body。

#### 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| coverage 指标下降、关键路径改名或零匹配 | `test:coverage` 返回非零 |
| fixture 未声明 API / mutation body keys | 浏览器返回测试 501/422，teardown 列出 exact request 并失败 |
| main CSP 不等于 policy source、console/page/network 出现非预期错误 | 当前 browser test 失败 |
| page 产生横向 overflow、关键命令裁切或宽表无具名键盘 scroll region | route/visual contract 失败 |
| staging expected/observed version 不同、凭据/URL缺失 | smoke 在登录/业务验证前失败，只报告缺失变量名 |
| audited route heading 可见但业务 API 仍在加载 | 等待 tracked requests 连续 500ms 为空；30s 未 settle 时列出脱敏 method/path 并失败，不能截图或继续导航 |
| audited route navigation 时旧文档字体仍在加载 | requests settle 后再等待旧文档 `document.fonts.ready`；真实字体加载失败仍由 `requestfailed` 阻断，不忽略 `ERR_ABORTED` |
| staging 设置恢复失败 | run 阻断并标记 `settings-restore`，不得继续归档 |
| 非 `refs/heads/main` 手工 dispatch | secret-free `ref-guard` 失败，environment job 不启动 |
| 无真实 staging environment/凭据 | 仓库 gate 可合并，但不得把 Task/Gate C 标记完成 |

#### 5. Good / Base / Bad Cases

- Good：PR 的 source gate 与 30-case core route matrix/相关 Chromium contracts 都绿；release 后由 main ref dispatch staging，每次 route transition 前后都 settle tracked requests/fonts，成功截图不保留 PageState loading surface，artifact 明确区分 real-data 与 injection。
- Base：普通纯逻辑改动跑 focused Vitest + `make verify-web`；未触及 UI 时无需凭空新增 browser case，但现有 browser job仍必须绿。
- Bad：把失败 API兜底为 `[]`、允许所有 4xx/5xx、增加 retry 掩盖 flake、抬预算让 CI 变绿，或用 mock/injection 声称真实 staging 通过。

#### 6. Tests Required

- `scripts/check-web-quality-gates.test.sh`：package scripts、Make 调用链、browser job 作用域、staging dispatch/ref/environment/concurrency/permissions/artifact 合同。
- `web/src/security/stagingAuditContract.test.ts`：用 fake timers 证明 request idle window 会被级联请求重置且 timeout 失败；用 TypeScript AST 断言 `gotoAuditedRoute` 在首个 `page.goto` 前后都等待 tracked requests，并保持旧文档 font wait 位于 navigation 前；同时覆盖 locationless expected HTTP console correlation 的正反例。
- `web/e2e/{auth-router,core-routes,page-states,fixture-router,security,accessibility,visual-contracts}.spec.ts`：认证、30 route matrix、五态/四态、fail-closed、diagnostics、axe/键盘、390px/局部滚动。
- 修改 audit/staging harness 后至少运行 `tsc -b`、ESLint 与 source contract；真实 lane 只能在配置完成的 GitHub staging environment 验收。

#### 7. Wrong vs Correct

```yaml
# Wrong: PR job 直接读取 staging secrets，且新 run 可打断设置恢复。
on: [pull_request]
concurrency: { cancel-in-progress: true }

# Correct: main ref guard 先运行，staging environment job 串行且不可取消。
on:
  workflow_dispatch:
concurrency:
  group: frontend-staging-smoke
  cancel-in-progress: false
```

```ts
// Wrong: heading 可见时 API/font 仍可能在加载，page.goto 会主动取消它们。
await expect(page.getByRole('heading', { name: route.heading })).toBeVisible()
await page.goto(route.path)

// Correct: 导航前后都等待真实请求与字体 settle，并保留严格 diagnostics。
if (page.url() !== 'about:blank') {
  await audit.waitForRequestsToSettle()
  await page.evaluate(() => document.fonts.ready)
  await audit.assertClean(page)
}
await page.goto(route.path)
await expect(page.getByRole('heading', { name: route.heading })).toBeVisible()
await audit.waitForRequestsToSettle()
await page.evaluate(() => document.fonts.ready)
await audit.assertClean(page)
```

---

## TypeScript

- **strict 模式**：`web/tsconfig.app.json` 显式启用 `strict`、`noUncheckedIndexedAccess`、`exactOptionalPropertyTypes`，并叠加 `noUnusedLocals` / `noUnusedParameters` / `noFallthroughCasesInSwitch` / `erasableSyntaxOnly` / `verbatimModuleSyntax`；`tsconfig.node.json` 也保持 `strict`。
- **type-aware ESLint**：`src/lib/**/*` 与 `src/pages/asset-decisions/hooks/**/*` 使用 project service，阻断 floating/misused promises、await 非 Promise、不必要断言与非穷尽 switch；不要用全局 disable 消音。
- **`tsc -b` 是 build 第一步**：`web/package.json:11` 的 `build` = `tsc -b && vite build`。**类型错误会让 build 直接挂**，CI 红。
- **类型断言**：尽量用具体类型，**禁止 `any`**（`web/eslint.config.js:14` 启用 `tseslint.configs.recommended`）。如必须用未知输入，用 `unknown` + 收口判别。
- **类型导入**：`verbatimModuleSyntax: true` 强制类型 import 必须显式 `import type { Foo } from '...'`，不要省略 `type`。
- **JSX**：`jsx: react-jsx`，**不需要** `import React from 'react'`，按需 import 具体 hook / 类型即可；同一 import 中的类型必须显式写 `type`（参考 `web/src/components/atoms/Sparkline.tsx:1` 的 `import { type MouseEvent, ... } from 'react'`）。

---

## 测试约定

### 测试栈

- **Vitest 4 + jsdom**（`web/vitest.config.ts:6-11`：`environment: 'jsdom'`、`globals: true`、`unstubGlobals: true`、`setupFiles: './src/test/setup.ts'`）。
- **`@testing-library/react` + `@testing-library/jest-dom`** —— 后者在 `web/src/test/setup.ts:1` 一次性 import，所有测试文件无需重复 import。
- **`globals: true`** 意味着 `describe` / `it` / `expect` / `vi` 都是全局的；当前代码风格仍**显式 import 一份**（参考 `web/src/components/atoms/Button.test.tsx:2` `import { describe, it, expect } from 'vitest'`），保持一致。

### 测试文件位置 / 命名

- 测试与被测**同目录、同名 + `.test.tsx` / `.test.ts`**（如 `Button.tsx` ↔ `Button.test.tsx`）。详见 `.trellis/spec/web/directory-structure.md`。
- **不要**建集中 `__tests__/` 目录。
- 跨 page 的工具测试（`api.test.ts` / `theme.test.ts` / `auth-client.test.ts`）放 `web/src/lib/`。

### 测试覆盖目标

coverage 是 source gate 的强制 ratchet：全局阈值与关键文件清单都由 `web/coverage-budget.json` 读取，provider 必须显式 inventory 未被测试 import 的 production file；不得用 ignore/exclude、测试专用生产分支或重命名绕过。除此之外仍保持以下结构事实：

- **每个路由页有至少 1 份 `<Page>.test.tsx`**：实读 `web/src/pages/` 下 9 个 page 全部配套（`DashboardPage` / `EventsPage` / `LoginPage` / `MonitoringDetailPage` / `MonitoringDetailPage` / `MonitoringPage` / `SettingsPage` / `TargetDetailPage` / `TargetsPage`）。**新增 page 必须保持这条线**——至少 1 个 happy-path test 覆盖渲染 + 拉数据 + 默认交互。
- **每个 atom 有同名测试**（`atoms/Button.test.tsx` / `Card.test.tsx` / `Sparkline.test.tsx` / `Input.test.tsx` / `Badge.test.tsx` / `DataTable.test.tsx` / `Mono.test.tsx` / `StatusGlyph.test.tsx` / `Tabs.test.tsx` / `Toggle.test.tsx`）。新增 atom 同样补一份。
- **跨页业务组合组件按需测**（`IncidentList.test.tsx` / `EventList.test.tsx` / `ActionConfirmationCard.test.tsx` 已有；`DetailSection` / `StatusBadge` 当前未测——**不强制**，但如果改动到行为分支，请补）。
- **`lib/` 工具函数**——纯逻辑（`format.ts` / `theme.ts`）应有单测，I/O 边界（`api.ts` / `observabilityApi.ts` / `auth-client.ts`）按现状 mock `fetch` 跑表驱动用例。
- **复杂路由的结构合同必须扫描真实 production glob**：`assetDecisionArchitectureContract.test.ts` 使用 TypeScript compiler AST + synthetic fixtures，固定七个 controller entry、API symbol owner、唯一 router owner、禁止依赖边、无 `*PageContent` 替身，以及 page ≤400、controller ≤600、任一 route-private production file ≤800、controller `useEffect` ≤3。不要用 wrapper 文件、正则 import parser、路径/行号白名单或仅测试 happy fixture 代替 repository inventory。

### 测试模式（实读）

#### Page 测试：mock `fetch` + 渲染 + 交互

模板（参考 `web/src/pages/EventsPage.test.tsx:6-84`）：

```tsx
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EventsPage } from './EventsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('EventsPage', () => {
  afterEach(() => { vi.restoreAllMocks() })

  it('...', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([...]))
    vi.stubGlobal('fetch', fetchMock)
    render(<EventsPage />)
    await waitFor(() => expect(...).toBeInTheDocument())
    // fireEvent.change / click 触发交互；用 toHaveBeenLastCalledWith 校验请求形态
  })
})
```

要点：

- **`vi.stubGlobal('fetch', fetchMock)`** 是当前主流——配合 `vitest.config.ts` 的 `unstubGlobals: true`，每个测试结束自动还原。也可以在 `afterEach` 显式 `vi.restoreAllMocks()` 兜底。
- **不要起真 server**：jsdom 环境，全部走 `fetch` 桩。
- **断言用户可见行为优先**：`screen.getByRole('heading', { name: '事件' })` / `screen.getByText('正在加载事件…')`，少用 `getByTestId`。
- **`toHaveBeenLastCalledWith` 校验请求 URL + headers**：当前所有 page 测试都把请求 URL 完整字面写出来（含 URL-encoded query），这是发现 query 拼装回归的关键手段。
- **时间文本不要硬编码本机时区结果**：CI runner 默认 UTC，本地常见 Asia/Shanghai。断言 `Z` 时间戳渲染时，用产品同款 `formatDateTime('...Z')` 生成期望，或在测试环境显式固定 `TZ`；不要写死 `2026/04/26 17:15` 这类只在当前机器时区成立的字符串。若页面必须通过 `Timestamp` atom 渲染，除了文本外还要断言 `timestamp` / `mono` 等 class，避免测试退化成纯 formatter 自测。

#### Hook / Context 测试：`vi.spyOn` 替换 hook 返回值

参考 `web/src/app/RequireAuth.test.tsx:14-25`：

```tsx
import * as authCtx from '../lib/auth-context'
vi.spyOn(authCtx, 'useAuth').mockReturnValue({
  user: { username: 'admin' },
  loading: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
})
```

要点：

- **`vi.spyOn(module, 'useX').mockReturnValue(...)`** 比 `vi.mock(...)` 整文件 mock 更精准、不影响其他测试。
- **`vi.fn()`** 给 callback / handler 占位，按需 `expect(spy).toHaveBeenCalledWith(...)`。
- 跨业务模块的具体函数 mock 用 `vi.spyOn(api, 'listMonitoringInstances').mockResolvedValue(mockMonitoringInstances)`（参考 `web/src/app/layout/GlobalSearch.test.tsx:53`）。

#### Atom 测试：渲染 + 类名 / 行为断言

参考 `web/src/components/atoms/Button.test.tsx:5-37`：

```tsx
it('applies variant class', () => {
  render(<Button variant="danger">清空</Button>)
  expect(screen.getByRole('button')).toHaveClass('btn--danger')
})
```

原子测试通常 < 50 行，覆盖：默认 props、每个 variant 的 class、disabled / aria 等可见行为。**不要测样式像素**（jsdom 不跑 layout）。

### 不在 verify 链路里的东西

- **像素 visual regression / tracked screenshot baseline**不在 `make verify-web` 或 Playwright gate。正式 browser suite 使用语义、焦点、几何、overflow、axe 与 diagnostics；失败截图/trace 是短期 CI artifact，不是产品视觉验收。
- **本地 Python helper** `scripts/visual_evidence.py browser-sanity` 继续用于 running dev/local center 的增量检查，但属于 local-only 辅助。仓库正式 gate 是 lockfile 固定的 Node Playwright；缺少 Python Playwright 不影响 `npm run test:e2e` 的 CI 合同。
- 响应式修复的 local-only CDP 证据必须记录 route × viewport 矩阵、`documentWidth/innerWidth`、目标 `client/scroll` 尺寸、computed `white-space/overflow/text-overflow`、局部 scroll region 的 role/name/tabIndex/scrollLeft、关键命令 hit-test 和完整 diagnostics counters。只写“肉眼看起来正常”或只保留截图不算通过。
- URL-owned Modal 的浏览器证据还必须覆盖至少一个真实 filtered revalidation：聚焦实体入口，打开详情并确认请求 inventory，Escape 关闭后断言焦点回同一 group/manual/record 入口、body unlock、无 console/page/network/CSP error。测试不能只等新按钮出现；必须检查真实 `document.activeElement`，否则 restore target 被卸载后落到 `body` 会漏检。
- **真实 center 烟囱**由 `docs/operations/fresh-install-smoke-run.md` 承担，前端只在浏览器里 sanity check。

### Scenario: 非语义点击 AST contract 与可访问性证据层级

#### 1. Scope / Trigger

- Trigger: 新增/修改 production TSX 的 `onClick`、Tabs/Menu/skip-link/row keyboard 行为，或声称 axe/browser 可访问性通过时，必须使用本合同。
- 目标：默认阻止鼠标专属容器回流，并区分 source/RTL、正式 fixture Chromium、本地 helper 与认证 staging 的证明能力。

#### 2. Signatures

- Source contract：`web/src/security/semanticInteractionContract.test.ts`。
- `auditSource(path, source)` 使用 TypeScript compiler AST 找 intrinsic JSX element 与 `onClick` attribute；不得用正则扫描 JSX。
- 当前有限原因集合：`modal-backdrop`、`event-propagation`、`keyboard-complete-row`、`primary-link-row-enhancement`。
- 相邻 marker 形状：`{/* a11y-allow-nonsemantic-click: <reason> */}`；marker 必须是目标 JSX node 的立即前一个有效 sibling。

#### 3. Contracts

- `div` / `span` / `tr` / `td` / `li` / `article` / `section` / `p` / `label` 带 `onClick` 默认失败；native button/link 等不需要 marker。
- 未知 reason、非相邻 comment、按路径/行号静态白名单、目录 wildcard 或把测试文件当 production 例外都不能放行。
- marker 只解释已经具备其它完整语义路径的结构：backdrop、事件隔离、已有键盘合同的复合 row、有主 Link 的 pointer enhancement。真实命令必须改为 native element。
- RTL/Vitest 证明 DOM attributes 与事件合同；`web/e2e/accessibility.spec.ts` 用 lockfile 固定 Chromium/axe 证明真实 Tab/default action、focus return 与 settled serious/critical=0；`security.spec.ts` 和统一 fixture再证明 console/network/CSP。真实认证与部署 header 仍只由 staging lane 证明。
- axe 必须等待 CSS/font/transition settled 后扫描，serious/critical 不得禁用或降级；发生失败要记录 rule/target/failureSummary 并回到 owner token/组件修复。
- 折叠/移动 AppShell 运行 axe 时，Sidebar Link 的 accessible name 必须来自稳定 `aria-label`，不能依赖会被 media query `display:none` 的 `.nav-text` 或只剩数字的 badge。主题切换后必须等待 animations settled 再扫描，避免把过渡中的中间颜色误记成最终 contrast。

#### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| unmarked `<div onClick>` | source contract 列出 path/line/tag 并失败 |
| approved marker 后插入另一个 JSX sibling | 原节点 marker 不再相邻，失败 |
| VPS row 有主 Link + background enhancement marker | 允许；Link click 必须被 row guard 忽略 |
| local axe serious/critical > 0 | 本任务浏览器门失败；不得写入“通过”证据 |
| Python Playwright 缺失但本机 Chromium/CDP 可用 | 使用 CDP 等价证据并注明 local-only，不增加 repo dependency |
| tab focus 后受控 panel 让父级滚动条出现 | 检查最终 commit 后的 tab/list rect 与 scrollLeft；不能只断言 `document.activeElement` |
| collapsed Sidebar `.nav-text` 被隐藏 | 每个 nav Link 仍有 label/count 派生的可辨识名称 |
| 只有 RTL 通过 | 不能宣称真实 Tab/default focus 或 axe 已通过 |

#### 5. Good / Base / Bad Cases

- Good：AST inventory 为 7 个受控 entry、0 unexplained；Chromium 用真实 `Input.dispatchKeyEvent` 验证菜单 Tab 前移，axe 4.10.3 在 settled Dashboard/Settings/VPS 上 serious/critical 为 0。
- Base：组件单测先 RED→GREEN，再由 repository Chromium suite 覆盖真实 default action；额外 Python/CDP 证据仍明确写“local-only”。
- Bad：给 clickable div 加一个 broad comment；只跑 jsdom 就称 axe/browser 通过；通过禁用 `color-contrast` 规则让报告变绿。

#### 6. Tests Required

- synthetic AST fixtures：native pass、unmarked fail、unknown/non-adjacent fail、四个有限 reason pass。
- repository inventory：扫描全部 non-test production TSX，unexplained=0，并对当前 allowed 数量做有意识的 bounded assertion。
- 行为 focused tests：UserChip/TopBar/AppShell/VPS/Tabs/SegmentedControl；marker 涉及的 Modal/DataTable/Targets/Monitoring 回归一并运行。
- 本地 Chromium：至少记录 browser/axe 版本、数据源、routes/viewports、focus sequence、page overflow、关键文本 client/scroll/computed style、局部滚动区语义/键盘 scrollLeft、console/exception/CSP/network counters；截图不是默认 tracked evidence。

#### 7. Wrong vs Correct

```tsx
// Wrong: comment 不能把真实命令变成可访问控件。
{/* a11y-allow-nonsemantic-click: convenience */}
<div onClick={save}>保存</div>

// Correct: 命令使用原生 button；只有 pointer enhancement 进入有限例外。
<button type="button" onClick={save}>保存</button>
{/* a11y-allow-nonsemantic-click: primary-link-row-enhancement */}
<tr onClick={handleBackgroundClick}>...</tr>
```

---

## ESLint

实读 `web/eslint.config.js`：

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: { ecmaVersion: 2020, globals: globals.browser },
  },
])
```

启用的规则集：

- `@eslint/js` recommended
- `typescript-eslint` recommended（含 `no-explicit-any` / `no-unused-vars` / `consistent-type-imports` 等）
- `eslint-plugin-react-hooks` flat recommended（含 `react-hooks/rules-of-hooks` / `exhaustive-deps`）
- `eslint-plugin-react-refresh` vite preset

**政策**：

- **warning vs error 一律视作必修**——`exhaustive-deps` 是 warning，但当前代码库没有未修的 warning，新增不要破坏这条惯例。
- **不允许 `// eslint-disable-next-line` 静默问题**——只有可搜索、紧邻并写出具体框架原因的有限例外（当前 Provider+hook colocation 的 React Refresh 说明）；不得全局关闭 type-aware 或 hooks rules。
- **不要**为了通过 lint 把类型改成 `any` / 把 hook 依赖项硬塞 `[]`：先确认根因。

---

## 提交前清单

下面这条清单是 happy-path，按顺序勾：

1. [ ] **focused lint/test** —— 修改时先跑相关 Vitest/ESLint，保留 RED→GREEN 证据。
2. [ ] **`make verify-web`** —— coverage、strict build、bundle/font 与 CSS AST source gate 全绿。
3. [ ] **改了 user-visible UI / browser contract → `npm --prefix web run test:e2e`** —— 运行固定 Chromium；十 route broad change 必须保持 30/30。
4. [ ] **同时改了前后端 → `./scripts/verify.sh`** 一把跑完（前后端都过）。
5. [ ] **需要人工视觉判断** → 对照 `docs/design/current/{interface-language.md,component-patterns.md}`，并按 `docs/operations/ui-preview-and-browser-sanity.md` 记录 preview、routes/viewports、正式 browser result 与人工判断。额外本地 helper 证据必须标注 local-only；如果改变可复用 UI 方向，同步更新当前 design/spec。
   - 不要提交 screenshot manifest 或 bulk raster screenshots；只有用户明确批准的 public README/docs asset 可放入 allowlisted docs asset path。
6. [ ] **改了 API 形状（增减字段 / 改命名 / 改可选性）** → 同 PR 把 `web/src/lib/types.ts` + owning `web/src/lib/*Api.ts` façade 改完，并补 page / 测试断言。

---

## 跨层一致性（PR review checklist）

当前没有 codegen，所以"改一处必带的另一处"必须人工保证。reviewer 必看：

| 改动 | 必须连带的修改 |
|------|----------------|
| 新增 / 修改 center HTTP 端点的请求 / 响应字段 | 1) 后端按 `.trellis/spec/backend/` 改完；2) `web/src/lib/types.ts` 加 / 改 `*Record` `*Input`，**保持 snake_case 与 Go JSON tag 一致**；3) owning `web/src/lib/*Api.ts` façade 加 / 改函数；4) page / component 调用方更新；5) 必要时 page 测试的 `toHaveBeenLastCalledWith` 断言一起更新 |
| 新增 / 修改业务 API 调用 | 必须落到 `web/src/lib/` façade，**不要**在 page / component 里直接 `fetch()`；默认使用 `api.ts`，只有全部 consumer 都是 lazy route 且 fresh bundle 证据要求隔离时才使用 domain façade，详见 `state-and-data.md` |
| 移动 API helper 到 route-lazy domain façade | wire shape/API tests 不变；fresh production build 后同时检查 entry 与 max async budget，不得让入口下降以换取 async 超限，也不得抬预算 |
| 修改 Asset Decisions controller / route composition | 运行 `AssetDecisionsPage.test.tsx`、全部 `asset-decisions/` domain workflow/controller tests 与 `assetDecisionArchitectureContract.test.ts`；核对四个 filtered GET、11 GET renewal inventory、group/manual/record focus restore 和结构预算 |
| 新增 page | `web/src/app/router.tsx` 注册路由 + colocate `<Page>.test.tsx`（至少 1 个 happy-path test） |
| 新增 atom | `web/src/components/atoms/<Name>.tsx` + 同名 `.test.tsx` + `atoms/index.ts` 加 barrel export + `web/src/styles/partials/atoms.css` 加样式（用令牌） |
| 新增 / 改 CSS 令牌 | `web/src/styles/tokens.css` 同步检查 3 套运行时主题（`:root` / `theme-houfeng-light` / `theme-classic-dark`）；`classic-light` 复用 `houfeng-light`，见 `.trellis/spec/web/styling-guidelines.md` |
| 改首屏防闪烁脚本 | `web/public/theme-bootstrap.js` 与 `web/src/lib/theme.ts` 的 preset/mode allowlist、system scheme 和 `classic-light` 回退必须保持一致；`web/index.html` 只同步加载同源脚本，不得恢复 inline script |
| 改路由注册 / 页面加载边界 | 保持 `appRoutes` 可被 `matchRoutes` 测试；路由页用 `React.lazy` + `RouteModuleFallback`；fresh build 后运行 `bundle:check`，确认 entry/max async 均在 ratchet 内且没有 Vite large chunk warning |

---

## 反模式 / Common Mistakes

- ❌ **跳测试 / 跳 lint 提交**：哪怕"只改一行 className"也跑 `npm run lint && npm run test -- --run`，几秒的事。
- ❌ **`git commit --no-verify`**：仓库 `.githooks/` 已保护本地/远端 main/master；先运行 `scripts/setup-git-hooks.sh`，不得绕过。
- ❌ **`any` / 不带类型的 `useState()`**：`useState<T>(initial)` 必须给出 T 或让 initial 推导出 T；类型断言用 `unknown` + 收口判别替代 `any`。
- ❌ **把 `exhaustive-deps` warning 当噪音**：要么补依赖，要么用 `useCallback` / `useRef` 重组结构；不要 `// eslint-disable`。
- ❌ **CI 红了 force-push 改一行试**：先在本地复现 `cd web && npm run lint && npm run test -- --run && npm run build`，找到根因再提 commit。
- ❌ **PR 内只改后端 contract、不改前端类型**：会让 page 在运行期 `unknown` 解析报错（参考 `lib/api.ts` 的错处理）。同 PR 内必须前后端一起改完。
- ❌ **测试用 `setTimeout(..., 1000)` 等异步**：用 `await waitFor(() => expect(...).toBeInTheDocument())` 让 RTL 自己轮询。
- ❌ **直接 `screen.debug()` 留在测试里**：debug 完删掉。
- ❌ **page 测试 hardcode 后端响应字段拼写错误**（少 `_` / 多 `_`）：复制 `lib/types.ts` 的字段名而不是手敲。
- ❌ **新增第三方依赖不评估**：每加一个 dep 要权衡 bundle size + 维护成本——当前 `dependencies` 只有 3 个（react / react-dom / react-router-dom），保持极简。

---

## 已知 gap

> 用于后续任务评审；若形成可复用规则，更新 `.trellis/spec/` 或当前 active docs。

1. **认证 staging 依赖外部配置**：没有 GitHub `staging` environment、main-only deployment policy、URL 与账号 secrets 时，workflow 实现可以通过 source/CI，但真实 staging AC 与 Gate C 必须保持未完成。
2. **`web/src/lib/types.ts` 与 Go contract 全靠人工同步**：没有 codegen。reviewer 在 contract 改动 PR 里必须同时检查 `lib/types.ts`；只有后端正式拥有 schema 后另立生成任务。
3. **正式 browser gate 只跑 Chromium**：Firefox/WebKit、Lighthouse 与像素视觉回归不在当前合同；需要时必须独立评审成本与 owner。

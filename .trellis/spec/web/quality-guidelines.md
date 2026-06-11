# 质量规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风前端的质量门是一条命令：`make verify-web`。**CI 与本地跑同一套 Makefile target**，`.github/workflows/ci.yml:29` 直接 `run: make verify-web`，没有任何 CI-only lint 偏方。

跨前后端改动（同时动了 `internal/` / `agent/` 与 `web/`）使用 `./scripts/verify.sh` 一把跑完，等价于 `make verify-go && make verify-web`（参考 `.trellis/spec/backend/quality-guidelines.md` 的"命令门户"段）。

不存在以下东西，**不要新增**（除非通过设计基线评审）：

- ❌ Prettier（仓库 `find . -name '.prettierrc*' -maxdepth 3` 为空）
- ❌ husky / lint-staged / lefthook 等 git hook 工具（同样为空）
- ❌ stylelint / postcss 配置（纯 CSS 不需要预处理）
- ❌ Playwright / Cypress / WebDriverIO 等 e2e 框架（`web/package.json` 无依赖）
- ❌ 任何 codegen（前端类型与 Go contract 手对齐，详见 `.trellis/spec/web/state-and-data.md`）

---

## 命令门户

实读 `web/package.json:9-15`：

| 命令 | 实际行为 | 何时用 |
|------|----------|--------|
| `npm run dev` | `vite` —— 起 dev server，`/api/*` 反代到 `VITE_API_TARGET`（默认 `http://127.0.0.1:8080`，见 `web/vite.config.ts:11-23`） | 本地开发 |
| `npm run build` | `tsc -b && vite build` —— 严格 TS 多项目 build + Vite 产物到 `web/dist/` | 提交前必跑（CI 也跑），产物由 center 通过 `HOUFENG_WEB_DIST_DIR` 吐给浏览器 |
| `npm run lint` | `eslint .` —— 跑 `web/eslint.config.js` 的 flat config | 提交前必跑 |
| `npm run test` | `vitest` —— 默认进 watch（CI 用 `--run` 一次性跑完） | 本地反复跑测；CI 用 `npm run test -- --run` |
| `npm run preview` | `vite preview` —— 看本地 build 产物效果 | 偶尔做产物 sanity check |

**Makefile 端**（`Makefile:65-70`）：

```make
verify-web:
	@if [ -f web/package.json ]; then \
		cd web && $(NPM) ci && $(NPM) run lint && $(NPM) run test -- --run && $(NPM) run build; \
	else \
		echo 'web workspace not initialized yet'; \
	fi
```

注意：

- **`make verify-web` 会跑 `npm run lint`**，然后跑 `npm run test -- --run` 与 `npm run build`。CI 与本地通过同一个 target 覆盖 lint / Vitest / TS+Vite build。
- `npm ci` 每次清空 `node_modules` 重装，本地反复跑会比较慢；本地速度优先时直接 `cd web && npm run test -- --run` / `npm run build` 即可，CI 仍走完整 `verify-web`。
- CI 用 `actions/setup-node@v6 with node-version: 22 cache: npm`（`.github/workflows/ci.yml:24-28`）锁 Node 22.x；本地 Node 必须 ≥ 22（`web/package.json:6-8` 的 `engines.node = "22.x"`）。

---

## TypeScript

- **strict 模式**：`web/tsconfig.app.json` 启用 `noUnusedLocals` / `noUnusedParameters` / `noFallthroughCasesInSwitch` / `erasableSyntaxOnly` / `verbatimModuleSyntax`，配合 ESLint 的 `typescript-eslint` 推荐集相当于 strict。
- **`tsc -b` 是 build 第一步**：`web/package.json:11` 的 `build` = `tsc -b && vite build`。**类型错误会让 build 直接挂**，CI 红。
- **类型断言**：尽量用具体类型，**禁止 `any`**（`web/eslint.config.js:14` 启用 `tseslint.configs.recommended`）。如必须用未知输入，用 `unknown` + 收口判别。
- **类型导入**：`verbatimModuleSyntax: true` 强制类型 import 必须显式 `import type { Foo } from '...'`，不要省略 `type`。
- **JSX**：`jsx: react-jsx`，**不需要** `import React from 'react'`，按需 import 具体 hook / 类型即可（参考 `web/src/components/atoms/Sparkline.tsx:1` `import type { CSSProperties } from 'react'`）。

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

**当前阶段不强制覆盖率阈值**——CI 不跑 coverage，`vitest` 也未启用 `--coverage`。但仓库当前已经做到的"事实约束"是：

- **每个路由页有至少 1 份 `<Page>.test.tsx`**：实读 `web/src/pages/` 下 9 个 page 全部配套（`DashboardPage` / `EventsPage` / `LoginPage` / `MonitoringDetailPage` / `MonitoringDetailPage` / `MonitoringPage` / `SettingsPage` / `TargetDetailPage` / `TargetsPage`）。**新增 page 必须保持这条线**——至少 1 个 happy-path test 覆盖渲染 + 拉数据 + 默认交互。
- **每个 atom 有同名测试**（`atoms/Button.test.tsx` / `Card.test.tsx` / `Sparkline.test.tsx` / `Input.test.tsx` / `Badge.test.tsx` / `DataTable.test.tsx` / `Mono.test.tsx` / `StatusGlyph.test.tsx` / `Tabs.test.tsx` / `Toggle.test.tsx`）。新增 atom 同样补一份。
- **跨页业务组合组件按需测**（`IncidentList.test.tsx` / `EventList.test.tsx` / `ActionConfirmationCard.test.tsx` 已有；`DetailSection` / `StatusBadge` 当前未测——**不强制**，但如果改动到行为分支，请补）。
- **`lib/` 工具函数**——纯逻辑（`format.ts` / `theme.ts`）应有单测，I/O 边界（`api.ts` / `auth-client.ts`）按现状 mock `fetch` 跑表驱动用例。

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

- **可视化回归 / 截图对比**不在 `make verify-web`。当前 UI 指导见 `docs/design/current/{interface-language.md,component-patterns.md}`；预览、浏览器 sanity 与本地截图政策见 `docs/operations/ui-preview-and-browser-sanity.md`。bulk screenshot evidence 与 manifest 不再 tracked；新 raster 图片只有在用户明确批准为 public README/docs asset 时才可提交到 allowlisted docs asset path。旧截图流程与一次性历史截图不是当前 workflow。
- **本地 browser sanity**可用 `python3 scripts/visual_evidence.py browser-sanity --base-url <url> --route <route> ...` 复用标准几何检查；它依赖本机 Python Playwright 时必须在 PR / final report 里标注为 local-only evidence。缺少本机 Playwright 是证据阻塞项，不要把 Playwright/Cypress/WebDriverIO 加进 `web/package.json` 来绕过。
- **真实 center 烟囱**由 `docs/operations/fresh-install-smoke-run.md` 承担，前端只在浏览器里 sanity check。

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
- **不允许 `// eslint-disable-next-line` 静默问题**——当前 `grep -rn "eslint-disable" web/src/` 为空，保持纪录干净。如必须 disable（如 React Hooks rule 误报），写完整中文理由 + 跟踪 issue。
- **不要**为了通过 lint 把类型改成 `any` / 把 hook 依赖项硬塞 `[]`：先确认根因。

---

## 提交前清单

下面这条清单是 happy-path，按顺序勾：

1. [ ] **`cd web && npm run lint`** —— 快速本地 lint；完整 `make verify-web` 也会跑这一项。
2. [ ] **`cd web && npm run test -- --run`** —— 跑 vitest 一遍。
3. [ ] **`cd web && npm run build`** —— 跑 `tsc -b && vite build`，确保 TS strict + Vite 产物都干净。
4. [ ] **同时改了前后端 → `./scripts/verify.sh`** 一把跑完（前后端都过）。
5. [ ] **改了 user-visible 的 UI** → 对照 `docs/design/current/{interface-language.md,component-patterns.md}`，并按 `docs/operations/ui-preview-and-browser-sanity.md` 给出 preview URL、已检查 routes / viewports、browser sanity、local screenshot notes（如有，默认不提交）。如果任务改变了可复用 UI 方向，同步更新 `docs/design/current/` 或相关 `.trellis/spec/`，不要把新决策写回历史版本目录。
   - 若只做本地 browser sanity，记录 `scripts/visual_evidence.py browser-sanity` 的 routes / viewports / 结果和 local-only 限制即可。
   - 不要提交 screenshot manifest 或 bulk raster screenshots；只有用户明确批准的 public README/docs asset 可放入 allowlisted docs asset path。
6. [ ] **改了 API 形状（增减字段 / 改命名 / 改可选性）** → 同 PR 把 `web/src/lib/types.ts` + `web/src/lib/api.ts` 改完，并补 page / 测试断言。

---

## 跨层一致性（PR review checklist）

当前没有 codegen，所以"改一处必带的另一处"必须人工保证。reviewer 必看：

| 改动 | 必须连带的修改 |
|------|----------------|
| 新增 / 修改 center HTTP 端点的请求 / 响应字段 | 1) 后端按 `.trellis/spec/backend/` 改完；2) `web/src/lib/types.ts` 加 / 改 `*Record` `*Input`，**保持 snake_case 与 Go JSON tag 一致**；3) `web/src/lib/api.ts` 加 / 改函数；4) page / component 调用方更新；5) 必要时 page 测试的 `toHaveBeenLastCalledWith` 断言一起更新 |
| 新增 / 修改业务 API 调用 | 必须落到 `web/src/lib/api.ts`，**不要**在 page / component 里直接 `fetch()`；历史直连创建监控实例 API 已偿还，reviewer 不要让这类请求回流到 page |
| 新增 page | `web/src/app/router.tsx` 注册路由 + colocate `<Page>.test.tsx`（至少 1 个 happy-path test） |
| 新增 atom | `web/src/components/atoms/<Name>.tsx` + 同名 `.test.tsx` + `atoms/index.ts` 加 barrel export + `web/src/styles/atoms.css` 加样式（用令牌） |
| 新增 / 改 CSS 令牌 | `web/src/styles/tokens.css` 同步改 4 个主题块（`:root` / `theme-houfeng-light` / `theme-classic-dark` / `theme-classic-light`），见 `.trellis/spec/web/styling-guidelines.md` |
| 改首屏防闪烁脚本 | `web/index.html:8-19` 与 `web/src/lib/theme.ts` 的逻辑必须保持一致——它们之间没有共享代码，靠人工对齐 |
| 改路由注册 / 页面加载边界 | 保持 `appRoutes` 可被 `matchRoutes` 测试；路由页用 `React.lazy` + `RouteModuleFallback`；运行 `npm run build` 并确认没有 Vite large chunk warning，入口 chunk 不应回退到单个 500 kB+ app bundle |

---

## 反模式 / Common Mistakes

- ❌ **跳测试 / 跳 lint 提交**：哪怕"只改一行 className"也跑 `npm run lint && npm run test -- --run`，几秒的事。
- ❌ **`git commit --no-verify`**：当前仓库**没有** pre-commit hook，但如果哪天加了，禁止用 `--no-verify` 绕过。
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

1. **没有 coverage 阈值 / coverage 上传**：当前不强制；如未来引入 `vitest --coverage` 与阈值，需同步更新 `.github/workflows/ci.yml` + 本文件。
2. **没有 e2e 框架**（Playwright / Cypress）：当前 CI 不跑浏览器自动化；`docs/operations/ui-preview-and-browser-sanity.md` 只定义本地预览、browser sanity 和本地/外部截图说明，不定义 tracked screenshot evidence。如未来引入正式浏览器自动化，需独立技术决策。
3. **`web/src/lib/types.ts` 与 Go contract 全靠人工同步**：没有 codegen。reviewer 在 contract 改动 PR 里必须同时检查 `lib/types.ts`。

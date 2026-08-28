# 目录结构

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

`web/` 是候风 / Houfeng Fleet Control Plane 当前的**单页应用**，技术栈实读自 `web/package.json`：

- **React 19**（`react@^19.2.5`、`react-dom@^19.2.5`）
- **TypeScript ~6.0**（`tsc -b` 加 `vite build` 双步构建）
- **Vite 8**（`web/vite.config.ts`：dev 模式把 `/api/*` 反代到 `VITE_API_TARGET`，默认 `http://127.0.0.1:8080`）
- **react-router-dom 7**（`createBrowserRouter`，详见 `web/src/app/router.tsx`）
- **Vitest 4 + jsdom**（测试环境在 `web/src/test/setup.ts`）
- **Playwright 1.61 + Chromium / axe**（正式 browser contracts 在 `web/e2e/`，staging 使用独立 config/workflow）
- **ESLint flat config**（`web/eslint.config.js`，含 `typescript-eslint`、`react-hooks`、`react-refresh`）

顶层职责划分（与项目根 `CLAUDE.md` "Frontend (`web/`)" 段一致）：

- **入口**：`web/index.html` → `web/src/main.tsx`，`main.tsx` 串起 `AppErrorBoundary` → `ThemeProvider` → `AuthProvider` → `RouterProvider`。
- **路由 / 布局壳 / 全局 provider**：`web/src/app/`。
- **路由页**：`web/src/pages/`，每条业务路由一个 `<Name>Page.tsx` + 同名 `<Name>Page.test.tsx`。
- **跨页复用展示原子与组合**：`web/src/components/`（其中 `atoms/` 是最底层视觉 / 行为原子）。
- **API client、共享类型、formatter、客户端 context**：`web/src/lib/`。
- **样式资源**：`main.tsx` 按 `reset.css` → `tokens.css` → `index.css` owner manifest → `modernize.css` 导入；真实规则位于 `web/src/styles/partials/`。
- **质量配置与浏览器合同**：`coverage-budget.json` / `bundle-budget.json` / `css-{budget,owners}.json`、`playwright*.config.ts`、`e2e/`。
- **静态资源**：`web/public/`、`web/index.html` 中的 `/favicon.svg` 等。

**生产部署**：`npm run build` 产物在 `web/dist/`，由 center 通过 `HOUFENG_WEB_DIST_DIR` + `internal/center/http/handlers/spa.go` 兜底吐给浏览器（详见仓库根 `CLAUDE.md` "Runtime layout"）。

---

## Directory Layout

```
web/
├── index.html                  # 单页入口；同步加载同源 theme-bootstrap.js
├── package.json                # node 22.x；source、coverage、browser 与 budget 命令门户
├── coverage-budget.json        # V8 全局与关键文件 branch ratchet
├── bundle-budget.json          # entry/async/font 静态预算
├── css-budget.json             # CSS AST/source/production 预算（Task 9 owner）
├── css-owners.json             # 七个 CSS owner 的穷尽映射
├── vite.config.ts              # /api 反代到 VITE_API_TARGET
├── vitest.config.ts
├── playwright.config.ts        # fixture Chromium gate + production preview
├── playwright.staging.config.ts # 已部署 staging；无本地 webServer/trace/video
├── eslint.config.js
├── tsconfig.json / .app.json / .e2e.json / .node.json
├── e2e/
│   ├── fixtures/               # fail-closed method/path/query router 与 typed profiles
│   ├── support/                # CSP/console/network diagnostics 与 geometry
│   ├── staging/                # 脱敏 audit collector + authenticated smoke
│   └── *.spec.ts               # route/state/a11y/security/responsive contracts
├── public/                     # 同源静态资源（主题预热、字体、caret、图标等）
└── src/
    ├── main.tsx                # createRoot + Provider 嵌套
    ├── app/                    # 路由 / 布局壳 / 路由级 metadata
    │   ├── router.tsx          # createBrowserRouter；所有路由集中注册；页面模块用 React.lazy 按路由拆包
    │   ├── RequireAuth.tsx     # 受保护路由壳；未登录跳 /login
    │   ├── RequireAuth.test.tsx
    │   ├── metadata.ts         # PRODUCT_FULL_NAME_ZH 等路由级常量
    │   ├── AppErrorBoundary.tsx # Provider/Router 外层 render error 恢复面
    │   ├── RouteErrorPage.tsx   # route render / lazy chunk error 恢复面
    │   ├── RouteModuleFallback.tsx # 路由模块加载中的 current surface
    │   └── layout/             # 应用骨架（Sidebar、TopBar、Breadcrumb 等）
    │       ├── AppShell.tsx    # 业务路由统一外壳；含 <Outlet />
    │       ├── Sidebar.tsx
    │       ├── TopBar.tsx
    │       ├── Breadcrumb.tsx
    │       ├── GlobalSearch.tsx
    │       ├── SyncStatus.tsx
    │       ├── UserChip.tsx
    │       └── ChangePasswordModal.tsx
    ├── pages/                  # 一文件一路由页
    │   ├── DashboardPage.tsx + DashboardPage.test.tsx
    │   ├── AssetDecisionsPage.tsx + AssetDecisionsPage.test.tsx
    │   ├── asset-decisions/    # Asset Decisions 路由私有领域模块
    │   │   ├── hooks/          # 七个 {state, commands} controller + invalidation
    │   │   ├── components/     # 受控 workbench / record presentation
    │   │   ├── modals/         # 五个受控 modal 边界
    │   │   └── *.test.tsx      # 按业务域拆分的 workflow tests
    │   ├── MonitoringPage.tsx + MonitoringPage.test.tsx
    │   ├── MonitoringDetailPage.tsx + MonitoringDetailPage.test.tsx
    │   ├── CommandAuditPage.tsx + CommandAuditPage.test.tsx
    │   ├── command-audit/      # 审计页私有 filter model / filters / table / event timeline
    │   ├── TargetsPage.tsx + TargetsPage.test.tsx
    │   ├── TargetDetailPage.tsx + TargetDetailPage.test.tsx
    │   ├── EventsPage.tsx + EventsPage.test.tsx
    │   ├── SettingsPage.tsx + SettingsPage.test.tsx
    │   ├── LoginPage.tsx + LoginPage.test.tsx
    │   ├── LoginPage.css       # 页面特有样式（极少例外）
    │   ├── RecordComparisonPage.tsx + test
    │   └── records/compare/    # /records/compare 私有 URL codec / controller / panels
    ├── components/             # 跨页复用展示组件
    │   ├── atoms/              # 设计系统原子（Button / Card / Badge / ...）
    │   │   ├── index.ts        # barrel export，pages 通常 from './atoms'
    │   │   └── Sparkline / Tabs / SegmentedControl 等 + 同名 *.test.tsx
    │   ├── ActionConfirmationModal.tsx
    │   ├── DetailSection.tsx
    │   ├── EventList.tsx
    │   ├── IncidentList.tsx
    │   └── StatusBadge.tsx
    ├── lib/                    # 数据层与无 UI 工具
    │   ├── api.ts              # eager/shared endpoint façade + withQuery/re-exports
    │   ├── observabilityApi.ts # route-lazy events/incidents/command-audit façade
    │   ├── apiRequest.ts       # fetch/401/error/JSON transport primitives
    │   ├── auth-client.ts      # /api/auth/* 薄封装，复用 apiRequest transport
    │   ├── auth-context.tsx    # AuthProvider + useAuth
    │   ├── vpsWriteRegistry.ts + vpsWriteRegistry-context.tsx # user-scoped VPS write authority
    │   ├── modalStack.ts + useModalFocus.ts # portal modal 栈、焦点与滚动锁
    │   ├── theme.ts + theme-context.tsx
    │   ├── format.ts           # 时间 / 字节 / 百分比等展示格式化
    │   └── types.ts            # 与 center JSON 响应对齐的 TS 类型
    ├── security/               # production source contracts（CSP / semantic interaction）
    │   ├── cspContract.test.ts
    │   └── semanticInteractionContract.test.ts
    ├── index.css               # 七 owner 的唯一 import manifest；本身不承载规则
    ├── styles/
    │   ├── reset.css
    │   ├── tokens.css          # 设计令牌（颜色、间距、字体）
    │   ├── modernize.css       # owner 映射内的现代化补充层
    │   └── partials/           # atoms/page/layout + 各业务 owner 的 production 规则
    └── test/
        └── setup.ts            # vitest setup（jest-dom 等）
```

> 上面的目录树以 `ls web/src/...` 实际结果为准。**与 `CLAUDE.md` "Frontend (`web/`)" 段的差异**：根 `CLAUDE.md` 描述共享原子在 `src/components/`，但实际仓库已经在 `components/` 下进一步拆出 `atoms/`（设计系统原子）与同级组合组件（`IncidentList.tsx`、`EventList.tsx` 等）。详见后文 *与 CLAUDE.md 的差异*。

---

## Module Organization

### `web/src/app/`

应用骨架。**不放具体业务**，只放路由表、受保护壳、应用级 metadata 与布局组件。

- `router.tsx` 是路由唯一入口：所有 `path / element` 在这里集中声明，绝大多数业务路由都嵌在 `<RequireAuth />` → `<AppShell />` 之下。`/login` 是唯一的免登录路由。
- 业务页面模块在 `router.tsx` 里用 `React.lazy` 按路由拆包，并通过 `RouteModuleFallback` 包进 `Suspense`。这样 `npm run build` 不会把所有页面塞进首个 app chunk；`RouteModuleFallback` 负责加载中文文案与 `page-panel` surface。
- `router.tsx` 仍需要导出 `appRoutes` 供 `matchRoutes(appRoutes, ...)` 测试。为满足 `react-refresh/only-export-components`，`router.tsx` 中的 lazy 变量使用 lower camelCase（如 `monitoringPage`），不要定义 PascalCase 组件常量；需要渲染时用小 helper 接收 `ComponentType` 并 `createElement(Component)`。
- `RequireAuth.tsx` 承担 401 / 未登录跳转；新增需要登录的页面**只需挂到 RequireAuth 子节点**，不要重写认证逻辑。
- `layout/` 下是 AppShell 的可视组成（侧边栏、顶部栏、面包屑、全局搜索、同步状态等）；它们仅被 `AppShell.tsx` 组合，**不应被 `pages/` 直接导入**。
- `metadata.ts` 放路由 / 应用级常量（如 `PRODUCT_FULL_NAME_ZH`），供 AppShell 与 LoginPage 共享。

### `web/src/pages/`

**一条业务路由一个 `<Name>Page.tsx`**，并 colocate 同名测试 `<Name>Page.test.tsx`（实测：`pages/` 下每个页面都有同名 `.test.tsx`）。

- 页面是**装配点**：调 `lib/` 下 owning API façade 拉数据，编排 `components/` 的展示原子，处理本地 UI 状态与表单。
- 复杂单路由可建立 `<route-name>/` 私有目录；`asset-decisions/` 是当前参考：route page 只组合七个 `{state, commands}` controller、纯 model 与受控展示，controller / component / modal / workflow test 均留在路由私有边界内，不提升为跨页共享模块。
- `CommandAuditPage.tsx` 是 `/command-audit` 唯一 controller/composition point，拥有 URL canonicalization、request generation、cursor append 与 expanded state；`pages/command-audit/` 只放 route-private filter model、筛选 UI、DataTable 和 allowlisted event timeline。共享 command display metadata 位于 `config/commands.ts`，不得把审计页组件提升到跨页 `components/` 或复制 Monitoring detail command list。
- `RecordComparisonPage.tsx` 是 `/records/compare` 唯一装配点，必须挂在 `/records/:recordId` 之前。`pages/records/compare/` 只放 `comparison-url/v1` codec、`useComparisonWorkbench` 与受控面板。证据类型切换用 SegmentedControl，不把无 panel 的 Tabs 当值选择器。另存走 `recordsApi` 的 comparison helpers，不得调用 `createRecord` / `useRecordDraft.publish()`。
- 页面之间**不要相互 import**；要复用就抽到 `components/`。
- 页面仅由 `app/router.tsx` 引用。

### `web/src/components/`

跨页可复用的展示 / 行为组件。**不持有业务路由概念**，不直接调 API client（数据通过 props 进来）。

- `components/atoms/`：设计系统原子（Button、Input、Select、Badge、Card、Modal、Tabs、SegmentedControl、DataTable 等）。组件规则由 `styles/partials/atoms.css` / `forms.css` / `tabs.css` 等 shared owner 持有，不依赖业务类型。`atoms/index.ts` 是 barrel export。
- `components/`（atoms 上一层）：领域感知的组合组件（如 `IncidentList`、`EventList`、`StatusBadge`、`DetailSection`、`ActionConfirmationModal`），它们可以引用 `lib/types.ts` 的类型，但仍然是**纯展示 / 受控**——不发请求、不依赖路由。

### `web/src/lib/`

无 UI 的数据 / 工具层：

- `api.ts`：eager/shared API façade，导出 `withQuery`、兼容 transport re-export 与启动/多域共享函数（`listMonitoringInstances()`、`getDashboard()` 等）。
- `observabilityApi.ts`：bundle-evidenced route-lazy façade，拥有 events/incidents/command-audit 读取。它只能复用 `api.ts` / `apiRequest.ts` primitives，不拥有第二套 fetch/401/error 逻辑；新增 domain façade 的门槛见 `state-and-data.md`。
- `auth-client.ts`：`/api/auth/*` 的薄封装（`login`、`logout`、`me`、`changePassword`），复用 `api.ts` 的 `requestJSON` / `postJSONBody` / `requestEmpty` 与同一套 401 hook。
- `auth-context.tsx` / `theme-context.tsx` 在 `main.tsx` 顶层挂载；`vpsWriteRegistry-context.tsx` 在 authenticated AppShell 内按用户挂载并包住 Outlet。
- `format.ts`：所有面向用户的格式化（时间、字节、百分比、标签拼接）；**新格式化函数都加到这里**，不要散落到组件文件。
- `types.ts`：与 center HTTP 响应对齐的 TypeScript 类型；**不要在 page / component 里手抄一遍**。

### `web/src/security/`

生产源码的 fail-closed contract tests。它们与被扫描代码分离，但仍由普通 Vitest 全量门执行：

- `cspContract.test.ts`：禁止 production TSX inline `style=`，并校验同源资源/policy 合同。
- `semanticInteractionContract.test.ts`：用 TypeScript compiler AST 扫描 production TSX 的 non-semantic `onClick`；有限 marker reason 只能解释 backdrop、事件隔离、已有键盘 row 和主 Link 的 pointer enhancement。

不要把普通组件测试搬进 `security/`，也不要在这里维护按路径/行号放行的静态白名单。

### `web/src/styles/`

`main.tsx` 只直接 import reset、tokens、`index.css` 与 modernize；`index.css` 以七个 owner section 导入 `styles/partials/`。**不要在组件文件里 `import './foo.css'`**——当前唯一 route-level 例外是 `pages/LoginPage.css`，且仍必须出现在 `css-owners.json`。

### `web/src/test/`

仅放 vitest setup（注册 `@testing-library/jest-dom` 等）。所有测试文件 colocate 在被测代码旁边，**不要建集中 `__tests__` 目录**。

### `web/e2e/`

正式 Chromium 合同，不与 `src/` 的 Vitest/RTL 混放：

- `fixtures/contracts.ts` 定义 method/path/canonical-query 与 response 形状；`router.ts` 对未知请求 fail closed；`profiles.ts` 持有 typed route/state fixture。
- `support/diagnostics.ts` 从后端 policy source 读取 CSP，并统一收集 console/page/request/HTTP/CSP/unhandled rejection；`geometry.ts` 持有 document overflow 与裁切断言。
- 顶层 `*.spec.ts` 只消费 fixture/support，不在每个文件复制 auth、catch-all API 或 diagnostics。
- `staging/` 不使用 fixture router：它在真实 origin/UI auth 上区分 real-environment 与 deployed-frontend injection，并只向 `test-results/staging-audit` 写脱敏 artifact。

---

## Naming Conventions

| 对象 | 规则 | 例子 |
|------|------|------|
| 路由页文件 | `<Name>Page.tsx`（PascalCase + `Page` 后缀） | `MonitoringPage.tsx`、`MonitoringDetailPage.tsx` |
| 路由页组件 | 与文件同名命名导出 `export function <Name>Page()` | `export function MonitoringPage()` |
| 共享组件文件 | `<ComponentName>.tsx`（PascalCase，与组件同名） | `IncidentList.tsx`、`atoms/Sparkline.tsx` |
| 测试文件 | 与被测同目录、同名 + `.test.tsx` / `.test.ts` | `MonitoringPage.test.tsx`、`api.test.ts` |
| Hook 文件 | 跨页 hook 放 `lib/`；经设计评审的复杂路由 controller 可放 `pages/<route>/hooks/`，并统一返回 `{state, commands}` | `pages/asset-decisions/hooks/useAssetDecisionGroups.ts` |
| Context Provider | `<Name>Provider`，对应 hook `use<Name>` | `AuthProvider` + `useAuth` |
| API client 函数 | 动词 + 资源驼峰；GET 用 `list/get`、POST 用 `create/issue/...` | `listMonitoringInstances`、`getMonitoringInstance`、`createTarget`、`issueMonitoringInstanceEnrollmentToken` |
| 类型 | 与 center 响应同名的领域记录用 `<Aggregate>Record` / `<Aggregate>Input` 后缀 | `MonitoringInstanceRecord`、`UpdateMonitoringInstanceMetadataInput` |
| CSS class | BEM 风（`block__element--modifier`），见 `styles/partials/page.css` 与 `atoms.css` | `page-panel__title`、`btn--primary` |

---

## 哪里放新代码

| 变更类型 | 落点 |
|----------|------|
| 新增业务路由 / 整页 | 1) 新建 `web/src/pages/<Name>Page.tsx` + 同名 `*.test.tsx`；2) 在 `web/src/app/router.tsx` 用 `React.lazy` 建 lower camelCase 页面模块变量；3) 在 `appRoutes` 内用 `routeElement(<module>, '<中文加载文案>')` 挂到 `<RequireAuth />` 下；4) 如需新数据，在 `lib/types.ts` 加类型并选择 owning `lib/*Api.ts` façade；5) fresh build + bundle gate 验证 lazy 边界 |
| 新跨页展示原子 | `web/src/components/atoms/<Name>.tsx` + 同名 `*.test.tsx`；在 `atoms/index.ts` 导出；样式放 `styles/partials/atoms.css` / `forms.css` / `tabs.css` 的既有 shared owner |
| 新跨页业务组合组件 | `web/src/components/<Name>.tsx`（与 IncidentList / EventList 同级），保持纯展示 / 受控 |
| 新复杂路由私有 controller / presentation | `web/src/pages/<route>/hooks/use<Name>.ts` 与同级 `components/` / `modals/`；route page 是唯一 composition point，controller 不互相 import，展示层不 import controller/API |
| 新 API 调用 | 默认在 `web/src/lib/api.ts` 加函数；若全部 consumer 都是 lazy route 且 bundle 证据要求隔离，放入已有 domain façade；同步 `lib/types.ts`，不要在 page/component 直接 `fetch()` |
| 新数据格式化 | `web/src/lib/format.ts` 加函数；同时在 `lib/format.test.ts` 增用例 |
| 新跨树 / 跨组件状态 | 当前有 Auth、Theme、authenticated VPS write registry 三个 Provider；新增时放 `web/src/lib/<name>-context.tsx`，并按真实生命周期挂到根链或 AppShell，禁止 page-private provider |
| 新布局壳元素（侧边栏 / 顶栏内增项） | 改 `web/src/app/layout/`；不要把布局碎片散到 `pages/` |
| 新 SPA 全局样式 | 改 `web/src/styles/` 下既有文件；非必要不新增 CSS 文件 |
| 新 production source contract | `web/src/security/<Contract>.test.ts`；使用 AST/结构解析并配 synthetic fixture，不用正则扫描 JSX或路径级白名单 |
| 新 repository browser contract | `web/e2e/<domain>.spec.ts`；复用 `fixtures/` 的 fail-closed router 与 `support/diagnostics.ts`，不在 spec 自建 catch-all API mock |

---

## 反模式

> 这些是当前代码库已经回避（或正在偿还）的写法，**新代码不要做**。

- ❌ 在 `pages/` 或 `components/` 里直接 `fetch()`：必须走 `lib/` 下 owning API façade。
- ❌ `components/` 反向 import `pages/`：组件层不该感知具体路由页。
- ❌ 在 `lib/` 里写 React 组件 / JSX（context Provider 例外）：`lib/` 是无 UI 数据/工具层。
- ❌ 绕过 `app/router.tsx` 私自加路由（如手写 `<BrowserRouter>`）：路由唯一入口是 `createBrowserRouter(appRoutes)`。
- ❌ 在 `router.tsx` 里 eagerly import 业务页面或定义 PascalCase lazy 常量：前者会恢复大入口 chunk，后者会触发 React Refresh 规则，因为 `router.tsx` 同时导出 `appRoutes` / `router`。
- ❌ 在组件文件里 `import './foo.css'`：规则由 `main.tsx` import chain + `index.css` owner manifest 管理；只有 Login route 例外。
- ❌ 新建 `__tests__/` 集中目录：测试一律 colocate（与被测同目录、同名 `.test.*`）。
- ❌ 在 `pages/` 之间相互 import：要复用就提到 `components/`。
- ❌ 从 `app/layout/` 直接被 `pages/` 引用：layout 仅服务 AppShell。

---

## 与 CLAUDE.md 的差异 / 已知 gap

> 用于后续任务评审；若形成可复用规则，更新 `.trellis/spec/` 或当前 active docs。

1. **`components/atoms/` 子目录未被 `CLAUDE.md` "Frontend (`web/`)" 段提及**，但实际已是设计系统原子的稳定落点（`Button` / `Card` / `Sparkline` / `Mono` / `Hostname` / `Timestamp` 等都在此）。本 spec 把它写进官方目录布局。
2. **`app/layout/`、`app/RequireAuth.tsx`、`app/metadata.ts` 也在 `CLAUDE.md` 简述之外**，是当前实际的应用壳组织方式。
3. **`auth-client.ts` 当前复用 `api.ts` 的 request helpers**，所以 401 hook 只有 `setUnauthorizedHandler` 一套，由 `auth-context.tsx` 绑定。新代码不要再新增第二套 fetch 包装。
4. **当前未使用 React Query / SWR / Redux / Zustand 等状态库**（`web/package.json` 无依赖）；详见 `state-and-data.md`。
5. **route-lazy domain façade 是受 bundle ratchet 约束的窄例外**：当前为 `observabilityApi.ts` 与 lazy-only `recordsApi.ts`（含 comparison helpers）；不能无证据把单体 API 拆成文件风暴，也不能复制 transport。`/records/compare` 可静态导入 `recordsApi.ts`，仍不得进入 AppShell。

---

## Examples

仓库内"组织到位"的真实参考点：

- **完整一条路由线**：`web/src/app/router.tsx` lazy 注册 `/monitoring` → `web/src/pages/MonitoringPage.tsx`（页面装配） + `web/src/pages/MonitoringPage.test.tsx`（colocated 测试） + `web/src/lib/api.ts`（业务 API client） + `web/src/lib/types.ts`（`MonitoringInstanceRecord` 类型）。
- **设计系统原子使用**：`web/src/components/IncidentList.tsx:1` `import { Hostname, StatusGlyph, Timestamp } from './atoms'`，体现"组合组件按需引用 atoms barrel"的范式。
- **应用壳分层**：`web/src/app/layout/AppShell.tsx` 引用 `Sidebar`、`TopBar`、`ChangePasswordModal`，并通过 `<Outlet />` 渲染当前路由页（`web/src/app/router.tsx:23` 处的子路由）。
- **数据获取分层**：`web/src/pages/EventsPage.tsx` 调 `lib/observabilityApi.ts` → `loading/error/data` 三态 → 渲染事件展示；transport/401 仍由 `apiRequest.ts` 唯一拥有。

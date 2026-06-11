# 目录结构

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

`web/` 是候风 / Houfeng Fleet Control Plane V1 的**单页应用**，技术栈实读自 `web/package.json`：

- **React 19**（`react@^19.2.5`、`react-dom@^19.2.5`）
- **TypeScript ~6.0**（`tsc -b` 加 `vite build` 双步构建）
- **Vite 8**（`web/vite.config.ts`：dev 模式把 `/api/*` 反代到 `VITE_API_TARGET`，默认 `http://127.0.0.1:8080`）
- **react-router-dom 7**（`createBrowserRouter`，详见 `web/src/app/router.tsx`）
- **Vitest 4 + jsdom**（测试环境在 `web/src/test/setup.ts`）
- **ESLint flat config**（`web/eslint.config.js`，含 `typescript-eslint`、`react-hooks`、`react-refresh`）

顶层职责划分（与项目根 `CLAUDE.md` "Frontend (`web/`)" 段一致）：

- **入口**：`web/index.html` → `web/src/main.tsx`，`main.tsx` 串起 `ThemeProvider` → `AuthProvider` → `RouterProvider`。
- **路由 / 布局壳 / 全局 provider**：`web/src/app/`。
- **路由页**：`web/src/pages/`，每条业务路由一个 `<Name>Page.tsx` + 同名 `<Name>Page.test.tsx`。
- **跨页复用展示原子与组合**：`web/src/components/`（其中 `atoms/` 是最底层视觉 / 行为原子）。
- **API client、共享类型、formatter、客户端 context**：`web/src/lib/`。
- **样式资源**：`web/src/styles/`（`reset.css` / `tokens.css` / `atoms.css` / `pages.css`），全部在 `main.tsx` 顶部一次性导入。
- **静态资源**：`web/public/`、`web/index.html` 中的 `/favicon.svg` 等。

**生产部署**：`npm run build` 产物在 `web/dist/`，由 center 通过 `HOUFENG_WEB_DIST_DIR` + `internal/center/http/handlers/spa.go` 兜底吐给浏览器（详见仓库根 `CLAUDE.md` "Runtime layout"）。

---

## Directory Layout

```
web/
├── index.html                  # 单页入口；含 inline 主题预热脚本
├── package.json                # node 22.x；脚本 dev / build / lint / test
├── vite.config.ts              # /api 反代到 VITE_API_TARGET
├── vitest.config.ts
├── eslint.config.js
├── tsconfig.json / .app.json / .node.json
├── public/                     # 静态资源（图标等）
└── src/
    ├── main.tsx                # createRoot + Provider 嵌套
    ├── app/                    # 路由 / 布局壳 / 路由级 metadata
    │   ├── router.tsx          # createBrowserRouter；所有路由集中注册；页面模块用 React.lazy 按路由拆包
    │   ├── RequireAuth.tsx     # 受保护路由壳；未登录跳 /login
    │   ├── RequireAuth.test.tsx
    │   ├── metadata.ts         # PRODUCT_FULL_NAME_ZH 等路由级常量
    │   ├── RouteModuleFallback.tsx # 路由模块加载中的 current surface
    │   └── layout/             # 应用骨架（Sidebar、TopBar、Breadcrumb 等）
    │       ├── AppShell.tsx    # 业务路由统一外壳；含 <Outlet />
    │       ├── Sidebar.tsx
    │       ├── TopBar.tsx
    │       ├── Breadcrumb.tsx
    │       ├── GlobalSearch.tsx
    │       ├── SyncStatus.tsx
    │       ├── UserChip.tsx
    │       ├── ChangePasswordModal.tsx
    │       └── layout.css      # 仅服务于本目录组件
    ├── pages/                  # 一文件一路由页
    │   ├── DashboardPage.tsx + DashboardPage.test.tsx
    │   ├── MonitoringPage.tsx + MonitoringPage.test.tsx
    │   ├── MonitoringDetailPage.tsx + MonitoringDetailPage.test.tsx
    │   ├── MonitoringDetailPage.tsx + MonitoringDetailPage.test.tsx
    │   ├── TargetsPage.tsx + TargetsPage.test.tsx
    │   ├── TargetDetailPage.tsx + TargetDetailPage.test.tsx
    │   ├── EventsPage.tsx + EventsPage.test.tsx
    │   ├── SettingsPage.tsx + SettingsPage.test.tsx
    │   ├── LoginPage.tsx + LoginPage.test.tsx
    │   └── LoginPage.css       # 页面特有样式（极少例外）
    ├── components/             # 跨页复用展示组件
    │   ├── atoms/              # 设计系统原子（Button / Card / Badge / ...）
    │   │   ├── index.ts        # barrel export，pages 通常 from './atoms'
    │   │   └── Sparkline.tsx 等 + 同名 *.test.tsx
    │   ├── ActionConfirmationCard.tsx
    │   ├── DetailSection.tsx
    │   ├── EventList.tsx
    │   ├── IncidentList.tsx
    │   └── StatusBadge.tsx
    ├── lib/                    # 数据层与无 UI 工具
    │   ├── api.ts              # 统一 API client（封装 /api/* 调用）
    │   ├── auth-client.ts      # /api/auth/* 薄封装，复用 api.ts request helpers
    │   ├── auth-context.tsx    # AuthProvider + useAuth
    │   ├── theme.ts + theme-context.tsx
    │   ├── format.ts           # 时间 / 字节 / 百分比等展示格式化
    │   └── types.ts            # 与 center JSON 响应对齐的 TS 类型
    ├── styles/                 # 全局 CSS，main.tsx 一次性导入
    │   ├── reset.css
    │   ├── tokens.css          # 设计令牌（颜色、间距、字体）
    │   ├── atoms.css           # atoms 组件样式
    │   └── pages.css
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

- 页面是**装配点**：调 `lib/api.ts` 拉数据，编排 `components/` 的展示原子，处理本地 UI 状态与表单。
- 页面之间**不要相互 import**；要复用就抽到 `components/`。
- 页面仅由 `app/router.tsx` 引用。

### `web/src/components/`

跨页可复用的展示 / 行为组件。**不持有业务路由概念**，不直接调 API client（数据通过 props 进来）。

- `components/atoms/`：设计系统原子（Button、Input、Badge、Card、Tabs、Toggle、Sparkline、TrendArrow、StatusGlyph、Mono / Hostname / Timestamp、DataTable）。这些组件**只与 `styles/atoms.css` 与 `tokens.css` 耦合**，不依赖任何业务类型。`atoms/index.ts` 是 barrel export，调用方统一 `from '../components/atoms'`。
- `components/`（atoms 上一层）：领域感知的组合组件（如 `IncidentList`、`EventList`、`StatusBadge`、`DetailSection`、`ActionConfirmationCard`），它们可以引用 `lib/types.ts` 的类型，但仍然是**纯展示 / 受控**——不发请求、不依赖路由。

### `web/src/lib/`

无 UI 的数据 / 工具层：

- `api.ts`：统一 API client。**所有 `/api/*` 请求必须经此处的 request helpers 或导出业务函数**。导出函数式 API（`listMonitoringInstances()`、`getDashboard()` 等），返回 `Promise<T>`，T 直接来自 `types.ts`。
- `auth-client.ts`：`/api/auth/*` 的薄封装（`login`、`logout`、`me`、`changePassword`），复用 `api.ts` 的 `requestJSON` / `postJSONBody` / `requestEmpty` 与同一套 401 hook。
- `auth-context.tsx` / `theme-context.tsx`：唯二的 React Context Provider，分别在 `main.tsx` 顶层一次性挂载。
- `format.ts`：所有面向用户的格式化（时间、字节、百分比、标签拼接）；**新格式化函数都加到这里**，不要散落到组件文件。
- `types.ts`：与 center HTTP 响应对齐的 TypeScript 类型；**不要在 page / component 里手抄一遍**。

### `web/src/styles/`

全局 CSS，仅在 `main.tsx` 顶部 import 一次（参见 `web/src/main.tsx:5-9`）。**不要在组件文件里 `import './foo.css'`**——例外是 `app/layout/layout.css`（仅供 layout 子树）和 `pages/LoginPage.css`（首屏前缺少应用壳的特例）。

### `web/src/test/`

仅放 vitest setup（注册 `@testing-library/jest-dom` 等）。所有测试文件 colocate 在被测代码旁边，**不要建集中 `__tests__` 目录**。

---

## Naming Conventions

| 对象 | 规则 | 例子 |
|------|------|------|
| 路由页文件 | `<Name>Page.tsx`（PascalCase + `Page` 后缀） | `MonitoringPage.tsx`、`MonitoringDetailPage.tsx` |
| 路由页组件 | 与文件同名命名导出 `export function <Name>Page()` | `export function MonitoringPage()` |
| 共享组件文件 | `<ComponentName>.tsx`（PascalCase，与组件同名） | `IncidentList.tsx`、`atoms/Sparkline.tsx` |
| 测试文件 | 与被测同目录、同名 + `.test.tsx` / `.test.ts` | `MonitoringPage.test.tsx`、`api.test.ts` |
| Hook 文件 | 当前未单独建 `hooks/` 目录；本地 hook 就近放在使用文件内 | — |
| Context Provider | `<Name>Provider`，对应 hook `use<Name>` | `AuthProvider` + `useAuth` |
| API client 函数 | 动词 + 资源驼峰；GET 用 `list/get`、POST 用 `create/issue/...` | `listMonitoringInstances`、`getMonitoringInstance`、`createTarget`、`issueMonitoringInstanceEnrollmentToken` |
| 类型 | 与 center 响应同名的领域记录用 `<Aggregate>Record` / `<Aggregate>Input` 后缀 | `MonitoringInstanceRecord`、`UpdateMonitoringInstanceMetadataInput` |
| CSS class | BEM 风（`block__element--modifier`），见 `styles/pages.css` 与 `atoms.css` | `page-panel__title`、`btn--primary` |

---

## 哪里放新代码

| 变更类型 | 落点 |
|----------|------|
| 新增业务路由 / 整页 | 1) 新建 `web/src/pages/<Name>Page.tsx` + 同名 `*.test.tsx`；2) 在 `web/src/app/router.tsx` 用 `React.lazy` 建 lower camelCase 页面模块变量；3) 在 `appRoutes` 内用 `routeElement(<module>, '<中文加载文案>')` 挂到 `<RequireAuth />` 下；4) 如需新数据，先在 `lib/api.ts` 加函数 + `lib/types.ts` 加类型 |
| 新跨页展示原子 | `web/src/components/atoms/<Name>.tsx` + 同名 `*.test.tsx`；在 `atoms/index.ts` 导出；如需新样式加到 `styles/atoms.css` |
| 新跨页业务组合组件 | `web/src/components/<Name>.tsx`（与 IncidentList / EventList 同级），保持纯展示 / 受控 |
| 新 API 调用 | `web/src/lib/api.ts` 加函数；如响应/请求体新颖，同步在 `lib/types.ts` 加类型；不要在 page / component 里直接 `fetch()` |
| 新数据格式化 | `web/src/lib/format.ts` 加函数；同时在 `lib/format.test.ts` 增用例 |
| 新跨树 / 跨组件状态 | 当前只有 `auth-context` / `theme-context` 两个 Provider；如确需第三个，放 `web/src/lib/<name>-context.tsx`，并在 `main.tsx` 挂到 Provider 链 |
| 新布局壳元素（侧边栏 / 顶栏内增项） | 改 `web/src/app/layout/`；不要把布局碎片散到 `pages/` |
| 新 SPA 全局样式 | 改 `web/src/styles/` 下既有文件；非必要不新增 CSS 文件 |

---

## 反模式

> 这些是当前代码库已经回避（或正在偿还）的写法，**新代码不要做**。

- ❌ 在 `pages/` 或 `components/` 里直接 `fetch()`：必须走 `lib/api.ts`。
- ❌ `components/` 反向 import `pages/`：组件层不该感知具体路由页。
- ❌ 在 `lib/` 里写 React 组件 / JSX（context Provider 例外）：`lib/` 是无 UI 数据/工具层。
- ❌ 绕过 `app/router.tsx` 私自加路由（如手写 `<BrowserRouter>`）：路由唯一入口是 `createBrowserRouter(appRoutes)`。
- ❌ 在 `router.tsx` 里 eagerly import 业务页面或定义 PascalCase lazy 常量：前者会恢复大入口 chunk，后者会触发 React Refresh 规则，因为 `router.tsx` 同时导出 `appRoutes` / `router`。
- ❌ 在组件文件里 `import './foo.css'`：全局样式由 `main.tsx` 顶部 + `app/layout/layout.css` + `pages/LoginPage.css` 三处统一管理。
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

---

## Examples

仓库内"组织到位"的真实参考点：

- **完整一条路由线**：`web/src/app/router.tsx` lazy 注册 `/monitoring` → `web/src/pages/MonitoringPage.tsx`（页面装配） + `web/src/pages/MonitoringPage.test.tsx`（colocated 测试） + `web/src/lib/api.ts`（业务 API client） + `web/src/lib/types.ts`（`MonitoringInstanceRecord` 类型）。
- **设计系统原子使用**：`web/src/components/IncidentList.tsx:1` `import { Hostname, StatusGlyph, Timestamp } from './atoms'`，体现"组合组件按需引用 atoms barrel"的范式。
- **应用壳分层**：`web/src/app/layout/AppShell.tsx` 引用 `Sidebar`、`TopBar`、`ChangePasswordModal`，并通过 `<Outlet />` 渲染当前路由页（`web/src/app/router.tsx:23` 处的子路由）。
- **数据获取分层**：`web/src/pages/EventsPage.tsx:63-94` 完整体现 "page 调 `lib/api.ts` → `loading/error/data` 三态 → 渲染 `components/EventList`" 的标准结构。

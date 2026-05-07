# 状态与数据

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风前端**当前没有引入任何第三方状态管理库**（`web/package.json` `dependencies` 仅 `react`、`react-dom`、`react-router-dom`，**无** Redux / Zustand / Jotai / Recoil / React Query / SWR）。整体策略：

- **数据获取**集中在 `web/src/lib/`，通过函数式 API client (`api.ts` / `auth-client.ts`) 调用 center 的 `/api/*` 端点；返回类型从 `web/src/lib/types.ts` 引用。
- **本地组件状态**使用 React 内建 hooks（主用 `useState` + `useEffect`，少量 `useRef`；当前未发现 `useReducer`）。
- **跨组件 / 跨页状态**仅由两个 React Context 承担：`AuthProvider` (`web/src/lib/auth-context.tsx`) 与 `ThemeProvider` (`web/src/lib/theme-context.tsx`)；两者都在 `web/src/main.tsx:15-23` 一次性挂载。
- **URL 状态**走 `react-router-dom@7` 的路由参数（`useParams`、`useNavigate`），不另起 store。

> **未来留余地**：如果出现需要全局缓存的服务端状态（请求去重、stale-while-revalidate、跨页共享列表）或复杂客户端状态（多步表单、协作 / 撤销），可以考虑引入 React Query / Zustand。**当前不引入**——任何引入需要独立技术决策。

---

## 数据获取（`web/src/lib/`）

### API client

- **业务 `/api/*` 调用一律走 `web/src/lib/api.ts`**。该文件实现：
  - 一个 `request(path, init)` 内部函数（`web/src/lib/api.ts:39-68`）封装通用 fetch 行为：默认 `credentials: 'include'`、`Accept: application/json`、`cache: 'no-store`，对 401 调用 `onUnauthorized()` 钩子并抛 `ApiError(401)`，对非 2xx 解析 `error` / `message` 字段后包成 `ApiError(status, message)`。
  - 业务函数使用动词 + 资源命名（`listNodes` / `getNode` / `createTarget` / `updateNodeMetadata` / `enterNodeMaintenance` / `getDashboard` 等），返回 `Promise<T>`，T 来自 `lib/types.ts`。
  - `If-Match` 乐观锁通过 `patchJSONBody(path, body, { ifMatch })` 表达（`web/src/lib/api.ts:98-112`），传入的是上一次拿到的 `updated_at`。
- **`/api/auth/*` 走 `web/src/lib/auth-client.ts` + `web/src/lib/fetcher.ts`**。这是历史遗留的第二条 fetch 包装，仅服务认证；**不要把新业务请求加到 `fetcher`**。
- **不要在 page / component 里直接 `fetch()`**。业务请求必须加到 `web/src/lib/api.ts` 再由 page / component 调用；`NodesPage` 的历史直连 `fetch('/api/nodes')` 已偿还为 `createNode` API helper，新代码不要恢复这条路径。

### 类型对齐

- 与 center 响应 / 请求体一一对应的类型集中在 `web/src/lib/types.ts`：`NodeRecord`、`TargetRecord`、`ProbeItemRecord`、`StateChangeEventRecord`、`DashboardOverview`、`SettingsRecord` 等；命名遵循 `<Aggregate>Record`（响应行）/ `<Aggregate>Input` / `<Aggregate>Override` 后缀。
- 字段名**完全镜像 center JSON**（snake_case，如 `node_id` / `current_health_status` / `last_heartbeat_at`）。**不要在前端再驼峰化一遍**——保持 grep 友好，便于和 Go 侧 `internal/center/http/handlers/*` 对齐。
- 中文枚举（如 `IncidentSeverity = '正常' | '关注' | '告警' | '严重'`、`OnboardingPhase` 等）来自 center，前端原样保留中文字面量；展示标签通过 `STATE_CHANGE_EVENT_TYPE_LABELS` (`web/src/lib/types.ts:202-221`) 这种 const map 二次映射，**不要散落到组件文件**。
- **当前类型是手写**，与 Go contract 没有自动生成机制。新增字段时按以下顺序：1) center handler / contract 改完；2) 在 `lib/types.ts` 加字段（保持 snake_case、保持可选性与后端一致）；3) 在 `lib/api.ts` 引用；4) page / component 消费。

### 数据格式化

- **所有面向用户的展示格式化都集中在 `web/src/lib/format.ts`**：时间 (`formatDateTime`)、百分比 (`formatPercent`)、数值 (`formatNumber`)、字节 (`formatBytes` / `formatBytesPerSecond`)、延迟 (`formatLatency`)、运行时长 (`formatUptime`)、标签拼接 (`formatLabelList`)。
- 缺失值统一返回 `'—'`（`format.ts` 内多处约定），日期缺失返回 `'尚无'`。
- **不要在组件文件里手写 `Intl.DateTimeFormat` / `toFixed`**；如果需要新格式化，加到 `format.ts` + 在 `format.test.ts` 增用例。

---

### Dashboard 数据可信度

- `DashboardPage` 是全局工作台，但只能展示 `getDashboard()` / `/api/dashboard` 已明确返回的事实。当前可用事实来自 `DashboardOverview`：dashboard 生成时间、总节点/目标数、异常/严重/维护计数、库存完整度计数、24h 新异常/恢复趋势、真实全量 `group_summaries`、通知配置布尔摘要、异常节点/目标摘要、最近事件。
- `snapshot_generated_at` 只能写成 `生成时间`、`Dashboard 摘要` 这类接口生成时间提示。它不是 Center health、agent heartbeat、sync freshness 或全链路实时性证明，不要写 `中心运行正常` / `同步于` / `健康检查通过` 之类文案。
- `abnormal_nodes` / `abnormal_targets` 只能代表当前异常对象队列，**不能**推导全量 group / region / provider 分布。Dashboard 的 `按 Group 分布` 必须来自后端 `group_summaries`；如果该数组为空，显示空态，不在前端制造 `未分组 0` 行。
- `notification_status` 只能展示配置布尔摘要，例如 Telegram / Feishu 是否已配置、Telegram runtime apply 是否生效。前端不得要求或展示 `telegram_bot_token`、`telegram_chat_id`、`feishu_webhook_url` 等敏感配置值；需要编辑真实配置时跳转 SettingsPage。
- 系统入口可以展示 dashboard contract 支撑的库存完整度事实，例如待接入节点、暂停节点、退役节点、暂停目标、归档目标。PR4 后，Dashboard 深链是受支持 contract：`/nodes?onboarding=pending` 表示待接入或绑定待处理节点，`/nodes?abnormal=1` 表示异常节点，`/targets?abnormal=1`、`/targets?run_status=暂停`、`/targets?run_status=已归档` 表示对应目标列表筛选，`/events?severity=严重`、`/events?time_range=24h`、`/events?maintenance_only=1` 表示事件页筛选；新增深链必须先在目标页面用 URL-state 和可见 chip/toggle 承接。
- AppShell 可以复用 `getDashboard()` 做轻量 shell summary，但只能把它标成 dashboard 摘要来源。加载中显示“正在读取系统摘要”，失败显示“摘要不可用”；不要写死 `center ok`、`中心运行正常`、`sync HH:mm:ss` 或用浏览器当前时间伪装后端同步时间。Sidebar 的节点/目标 count 可以来自 `abnormal_node_count` / `abnormal_target_count`，但加载中/失败时必须由 Shell 状态说明 0 count 不代表无异常。

```tsx
// 错误：从异常摘要伪装成全量分布
const groupSummaries = overview.abnormal_nodes.reduce(...)
<DetailSection title="按 Group 分布">...</DetailSection>

// 错误：把 dashboard 生成时间写成同步/健康状态
<Timestamp value={overview.snapshot_generated_at} /> 同步完成

// 错误：要求 dashboard 暴露敏感通知配置
overview.notification_status.telegram_bot_token

// 错误：没有真实 health/sync contract 时伪造 Shell 健康状态
<SyncStatus state="ok" label="中心运行正常" meta={`v1.0 · sync ${new Date().toISOString()}`} />

// 正确：只展示 dashboard contract 支撑的事实
<KpiLink label="节点" value={overview.total_node_count} description={`${overview.abnormal_node_count} 个异常`} />
<DetailSection title="当前需要处理">...</DetailSection>
<DetailSection title="按 Group 分布">...</DetailSection> // rows = overview.group_summaries
<span>Dashboard 摘要 <Timestamp value={overview.snapshot_generated_at} /></span>
<SyncStatus state="degraded" label="正在读取系统摘要" meta="v1.0 · dashboard loading" />
```

### Scenario: Dashboard Overview Contract

#### 1. Scope / Trigger

- Trigger: `/api/dashboard` 是 DashboardPage 与 AppShell 共享的全局摘要接口；任何新增字段都会跨越 PostgreSQL read model、Go JSON、`web/src/lib/types.ts` 和页面展示。
- 修改触发：新增/改名/改语义任一 `DashboardOverview` 字段，或让 Dashboard/AppShell 展示新的系统事实。

#### 2. Signatures

- Backend API: `GET /api/dashboard?limit=<positive-int>`。
- Backend method: `PostgresDashboardRepository.GetDashboardOverview(ctx, limit)`。
- Frontend API: `getDashboard(): Promise<DashboardOverview>`。
- Frontend type: `web/src/lib/types.ts` 的 `DashboardOverview`，字段保持 center JSON snake_case。

#### 3. Contracts

- `limit` 只限制 `abnormal_nodes`、`abnormal_targets` 和 `recent_events`；不得限制全局计数、`group_summaries` 或 `notification_status`。
- `snapshot_generated_at` 是 Center 生成 overview 的时间，只能被展示为 dashboard 生成时间。
- `group_summaries` 必须由后端基于全量 `nodes` + `targets` 计算，空白 group 归一为 `未分组`，前端不得从异常队列 reduce。
- `notification_status` 只能包含配置布尔摘要，不包含 Telegram token/chat id 或 Feishu webhook URL。
- 库存完整度计数必须来自后端 contract：待接入节点、暂停节点、退役节点、暂停目标、归档目标。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `limit` 缺失 | handler 使用默认 limit |
| `limit <= 0` 或非数字 | handler 返回 400 |
| dashboard store 查询失败 | handler 返回 500，store error 用 `%w` 包装上下文 |
| `center_settings` singleton 缺失 | `notification_status` 全 false，不返回错误 |
| `group_summaries` 为空 | Dashboard 显示空态，不制造 `未分组 0` |

#### 5. Good/Base/Bad Cases

- Good: group 只存在于 targets 时仍出现在 `group_summaries`，节点计数为 0、目标计数为真实值。
- Base: 无节点无目标时 dashboard 仍返回 200，计数为 0，首次接入工作台显示，Group 区为空态。
- Bad: 从 `abnormal_nodes` 推导 `按 Group 分布`；把 `snapshot_generated_at` 写成同步完成；把通知 token 暴露给 Dashboard。

#### 6. Tests Required

- Go store test: 新计数字段、全量 group SQL、settings 缺失时通知 false、`limit` 不影响 group summary。
- Go handler test: 新字段 JSON snake_case，且不泄露敏感通知字段。
- Frontend type/API fixture: `DashboardOverview` fixture 覆盖新增字段。
- DashboardPage test: 生成时间、Group 全量分布、库存完整度、通知配置摘要。
- AppShell test: 共享 dashboard fixture 与新增 contract 保持兼容。

#### 7. Wrong vs Correct

```tsx
// 错误：PR4 前从 Dashboard 拼筛选深链，并且筛选语义不由 API contract 支撑
<Link to={`/nodes?monitoring_status=${overview.paused_node_count > 0 ? '暂停' : ''}`}>暂停节点</Link>

// 正确：PR3 只展示 contract 支撑的状态摘要，入口仍去列表页
<Link to="/nodes">暂停 <MonoDigits>{overview.paused_node_count}</MonoDigits></Link>

// 错误：要求 settings secret 出现在 dashboard contract
overview.notification_status.feishu_webhook_url

// 正确：只展示配置布尔摘要，并把编辑动作交给 SettingsPage
<Link to="/settings">{notificationSummary(overview)}</Link>
```

---

## Page 内数据流：loading / error / empty 三态

标准模式（参考 `web/src/pages/EventsPage.tsx:63-108`）：

1. 页面用 `useState` 维护一个 state object，至少含 `loading: boolean`、`error: string | null`、`data` 字段。

   ```ts
   type State = {
     loading: boolean
     error: string | null
     events: Awaited<ReturnType<typeof listEvents>>
   }
   ```

2. `useEffect` 内拉取，**必须用 `cancelled` 旗标防止 unmount 后写 state**：

   ```ts
   useEffect(() => {
     let cancelled = false
     listEvents(query)
       .then((events) => { if (!cancelled) setState({ loading: false, error: null, events }) })
       .catch((error: unknown) => {
         if (cancelled) return
         const message = error instanceof ApiError ? error.message : '加载事件失败'
         setState({ loading: false, error: message, events: [] })
       })
     return () => { cancelled = true }
   }, [appliedFilters])
   ```

3. **错误判定优先 `instanceof ApiError`**，能拿到带 `status` 的结构化错误；其他错误降级为中文兜底文案。
4. 渲染分支：先 `state.loading` → loading 文案；再 `state.error` → 错误面板（用 `page-panel` 样式）；最后正常渲染。
5. **空数据渲染由展示组件兜底**（如 `IncidentList` / `EventList` 内部 `if (items.length === 0) return <div className="empty-state">…`），不要在 page 内重复写空态。

> 该模式**当前由各 page 手抄**，未抽公共 hook。等到 ≥ 5 个 page 重复且能稳定下来时，再考虑提取 `useResource(fetcher, deps)`——目前不抽。

---

## 本地组件状态

- **`useState` 是默认选择**。同一 page 内多块独立 state 倾向于多个 `useState`，不强行合并（参考 `web/src/pages/NodesPage.tsx:144-163` 内 ~14 个 `useState`，各自语义清晰）。
- **`useRef` 用于不触发 render 的可变值**（DOM 引用、focus 还原 token、回调最新值）。典型例子：`web/src/pages/NodesPage.tsx:162-163` 的 `actionButtonRefs` / `pendingFocusRestoreRef`。
- **`useReducer` 当前未使用**。如某个 page 状态机分支 ≥ 4 个动作 + state 之间相互依赖，可以考虑引入；目前的页面用多个 `useState` + 描述性的 update 函数已经够。
- **派生状态不要存 state**：能用 `nodes.filter(isBindingConflictNode)` 现算的就别 `useEffect` 同步进 state（参考 `NodesPage.tsx:369`）。

---

## 跨组件 / 跨页状态

仅有两条 Context：

| Context | 文件 | 提供值 | 消费方式 |
|---------|------|--------|----------|
| Auth | `web/src/lib/auth-context.tsx` | `{ user, loading, login, logout, refresh }` | `useAuth()`，必须在 `<AuthProvider>` 内调用，否则抛错 |
| Theme | `web/src/lib/theme-context.tsx` | `{ preset, mode, setPreset, setMode }` | `useTheme()`（必须在 Provider 内）/ `useThemeOptional()`（测试便利） |

两者都在 `web/src/main.tsx:15-23` 一次性挂在根：`ThemeProvider` → `AuthProvider` → `RouterProvider`。

**新增第三个 Context 的判断标准**：

1. 数据真的需要被树形多个节点消费（如多 page、多 layout 子树）。
2. 不是纯服务端数据缓存（那种应在引入 React Query 之类时再统一）。
3. 写入路径有限且语义清晰（如全局开关、当前组织 ID）。

满足后落到 `web/src/lib/<name>-context.tsx`，导出 `<Name>Provider` + `use<Name>`，并在 `main.tsx` Provider 链显式挂载（不要在某个 page 内偷偷挂）。

**还有一处准全局可变状态例外**：`web/src/lib/onboardingTokenCache.ts` 是模块级 Map，专门给"创建节点 → 跳转接入页"场景做一次性传值。**不要扩展它做通用 store**——其语义是"短生命、单消费、即用即清"。

---

## 数据拉取时机（实读约束）

- 当前所有数据拉取都是 **mount 时拉一次** + **用户操作触发重拉**。`useEffect` 触发条件主要是 page 入参（`useParams` 拿到的 `nodeId` / `targetId`）或筛选条件（如 `EventsPage` 的 `appliedFilters`）。
- **没有 SSE / WebSocket / 轮询**。center 也不主动推；交互式重刷由用户点击 / 提交触发。
- **没有跨 page 的请求缓存**：从 `/nodes` 进 `/nodes/:id` 会再发一次 `getNode`。当前体量可以接受；如果未来要去抖 / 缓存，再考虑 React Query。

---

## 反模式

> 这些是当前代码已经回避（或承认偿还）的写法，**新代码不要做**。

- ❌ **page / component 里直接 `fetch()`**：业务请求必须走 `web/src/lib/api.ts`，认证请求走 `web/src/lib/auth-client.ts`。
- ❌ **手抄后端字段名 / 自己拼 URL 格式化**：从 `lib/types.ts` import 类型 + 用 `lib/api.ts` 里的 `withQuery` 模式构造查询参数。
- ❌ **驼峰化后端字段**：保持 snake_case（`node_id`、`current_health_status`），便于和 center 端 grep 对齐。
- ❌ **跨 page 共享 mutable 全局变量**：除 `onboardingTokenCache` 这种受控、单用途、即用即清的例外，不要建模块级 `let`。
- ❌ **绕过 ApiError 直接 throw 字符串**：`lib/api.ts` 已统一错 `ApiError(status, message)`，page 用 `instanceof ApiError` 判别后挑 `.message` 展示。
- ❌ **在 `useEffect` 里写 state 不带 `cancelled` 旗标**：StrictMode 下 effect 会触发两次，不防护会出现 setState on unmounted。
- ❌ **把派生数据存 `useState`**：能现算就不要二次同步。
- ❌ **预先引入 React Query / Zustand / Redux**：当前没有任何 page 真的需要它们；引入新依赖需独立技术决策。
- ❌ **在 component / page 写 inline `Intl.*` / `toFixed`**：交给 `lib/format.ts`。

---

## 与 CLAUDE.md 的差异 / 已知 gap

> 用于喂 `docs/release/v1-gap-checklist.md`。

1. **`fetcher.ts` 与 `api.ts` 双 fetch 包装并存**（`web/src/lib/fetcher.ts` + `web/src/lib/api.ts`），分别注册 `setUnauthorizedHandler` 与 `setApiUnauthorizedHandler`，由 `auth-context.tsx` 同时挂钩。这是历史遗留，新代码不要再加第三套。
2. **类型与 Go contract 全靠手维护**——没有 codegen。前后端字段如有漂移，依赖测试 + 运行期 `unknown` 解析报错暴露。
3. **当前没有任何状态库 / 数据缓存层**：CLAUDE.md 也没要求引入。本 spec 把"暂不引入"作为现行约束写明。

---

## Examples

仓库内"数据获取写得好"的真实参考点：

- **标准 page 数据流（loading / error / data 三态 + cancelled 旗标）**：`web/src/pages/EventsPage.tsx:63-108`，配合 `web/src/lib/api.ts:304-325` 的 `listEvents(filter)`。
- **乐观锁更新**：`web/src/pages/NodesPage.tsx:283-325` 的 `handleSaveLabels` → `updateNodeMetadata(nodeId, input, { expectedUpdatedAt })`，其内部由 `web/src/lib/api.ts:145-153` 走 `If-Match` 头实现。
- **Provider + Hook 配对**：`web/src/lib/auth-context.tsx`（`AuthProvider` 内 `useEffect` 挂 401 钩子 → 用 `useAuth()` 暴露 `{ user, loading, login, logout, refresh }`）。
- **类型驱动的 API 函数集**：`web/src/lib/api.ts:1-21` 顶部从 `./types` 一次性 import 所有领域类型，下方所有 `requestJSON<T>` 都直接复用。

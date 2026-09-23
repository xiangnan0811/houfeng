# 状态与数据

> **项目依据**：通用工程约定见 [规范入口](/docs/spec/README.md)，领域行为见 [领域合同](/docs/spec/contracts/README.md)，产品理由见 [设计](/docs/design/README.md)。

---

## Overview

候风前端**当前没有引入任何第三方状态管理库**（无 Redux / Zustand / Jotai / Recoil / React Query / SWR）。`web/package.json` `dependencies` 现为 React/router 加上 Records 的 route-lazy Markdown 栈（`react-markdown`/`remark-gfm`/`rehype-sanitize`、`diff`）。源文编辑器是 textarea；CodeMirror 因 CSP `style-src 'self'` 不可用。整体策略：

- **数据获取**集中在 `web/src/lib/`：`apiRequest.ts` 拥有 transport/error/401/JSON/query primitives，`api.ts` 拥有启动路径与通用业务 endpoint façade；只有在 production bundle 证据要求保持 route-lazy 边界时，才使用同目录的 domain façade（当前为 `observabilityApi.ts`，以及只被 lazy record routes 消费的 `recordsApi.ts`）。`auth-client.ts` 复用同一 transport，返回类型从 `types.ts` 引用。
- **本地组件状态**使用 React 内建 hooks（主用 `useState` + `useEffect`，少量 `useRef`；当前未发现 `useReducer`）。
- **跨组件 / 跨页状态**由三个 React Context 承担：根级 `AuthProvider`、`ThemeProvider`，以及 authenticated `AppShell` 内按 `user.user_id` 隔离的 `VPSWriteRegistryProvider`。后者只保存 VPS write owner/attempt metadata，不缓存服务端业务列表或原始表单正文。
- **URL 状态**走 `react-router-dom@7` 的路由参数（`useParams`、`useNavigate`），不另起 store。

> **未来留余地**：如果出现需要全局缓存的服务端状态（请求去重、stale-while-revalidate、跨页共享列表）或复杂客户端状态（多步表单、协作 / 撤销），可以考虑引入 React Query / Zustand。**当前不引入**——任何引入需要独立技术决策。

---

## 数据获取（`web/src/lib/`）

### API client

- **业务 `/api/*` 调用一律由 `web/src/lib/` 下的 façade 暴露**。默认 owner 是 `api.ts`；只有全部 production consumer 都位于 lazy route、且 fresh build 证明入口预算需要隔离时，才按领域拆出 façade。当前 `observabilityApi.ts` 拥有 observability helpers；`recordsApi.ts` 拥有 Records/draft/permanent-deletion transport，只允许被 `/records/new`、`/records/:recordId` 及其 edit/revision lazy pages 和同目录 draft controller 消费，不得进入 AppShell / `api.ts` / eager router graph。业务函数使用动词 + 资源命名并返回 `Promise<T>`，T 来自 `lib/types.ts`。
- **transport 唯一 owner 是 `web/src/lib/apiRequest.ts`**：默认 `credentials: 'include'`、`Accept: application/json`、`cache: 'no-store'`；401 触发共享 unauthorized handler 并抛 `ApiError(401)`；非 2xx 默认只从 `error/message` 生成 legacy `status/message`，领域 façade 可通过显式 `ApiErrorDecoder` seam追加 allowlisted metadata。当前只有 lazy-only `recordsApi.ts` 静态组合 `apiError.ts` decoder，生成 `code/field_errors/recovery` 且不把 decoder 带入 eager graph。`apiRequest.ts` 同时拥有 `withQuery`；`api.ts` 只为兼容现有调用 re-export transport/query primitives。domain façade 复用这些 primitives，不得复制 fetch wrapper。
- **`/api/auth/*` 走 `web/src/lib/auth-client.ts`**，并复用 `apiRequest.ts` 的 primitives 与 401 hook。不要新增第二套 fetch 包装。
- `If-Match` 乐观锁仍由业务 façade 的 `patchJSONBody(path, body, { ifMatch })` 表达，传入上一次拿到的 `updated_at`；transport seam 不改变 method/header/body/wire shape。
- **不要在 page / component 里直接 `fetch()`**。业务请求必须进入 `web/src/lib/` façade 再由 page / component 调用；`MonitoringPage` 的历史直连 `fetch('/api/monitoring-instances')` 已偿还为 `createMonitoringInstance` API helper，新代码不要恢复这条路径。

### 类型对齐

- 与 center 响应 / 请求体一一对应的类型集中在 `web/src/lib/types.ts`：`MonitoringInstanceRecord`、`TargetRecord`、`ProbeItemRecord`、`StateChangeEventRecord`、`DashboardOverview`、`SettingsRecord` 等；命名遵循 `<Aggregate>Record`（响应行）/ `<Aggregate>Input` / `<Aggregate>Override` 后缀。
- 字段名**完全镜像 center JSON**（snake_case，如 `monitoring_instance_id` / `current_health_status` / `last_heartbeat_at`）。**不要在前端再驼峰化一遍**——保持 grep 友好，便于和 Go 侧 `internal/center/http/handlers/*` 对齐。
- 中文枚举（如 `IncidentSeverity = '正常' | '关注' | '告警' | '严重'`、`OnboardingPhase` 等）来自 center，前端原样保留中文字面量；展示标签通过 `STATE_CHANGE_EVENT_TYPE_LABELS` (`web/src/lib/types.ts:202-221`) 这种 const map 二次映射，**不要散落到组件文件**。
- **当前类型是手写**，与 Go contract 没有自动生成机制。新增字段时按以下顺序：1) center handler / contract 改完；2) 在 `lib/types.ts` 加字段（保持 snake_case、保持可选性与后端一致）；3) 在 owning `lib/*Api.ts` façade 引用；4) page / component 消费。

### Scenario: route-lazy API façade 与 bundle 边界

#### 1. Scope / Trigger

- Trigger: 新增只被 lazy route 使用的 API helper、`bundle:check` 报告 entry JS 回退，或准备把 endpoint 从 `api.ts` 移到 domain façade。
- 目标：保持业务请求、认证和错误边界统一，同时避免单体 `api.ts` 把 route-private endpoint 实现强制打进入口 chunk。

#### 2. Signatures

- Transport: `apiRequest.ts` 的 `requestJSON/requestEmpty/postJSON/postJSONBody`。
- Query/error: `apiRequest.ts` 的 `withQuery(path, filter)`、`ApiError<TRecovery = unknown>` 与可选 `ApiErrorDecoder` seam；`apiError.ts` 是 Records allowlist decoder；`api.ts` 保留兼容 re-export。
- Shared/eager façade: `api.ts`；lazy observability façade: `observabilityApi.ts`；lazy-only Records transport: `recordsApi.ts`。
- `observabilityApi.ts` exports: `listEvents`、`listIncidents`、`listHistoricalIncidents`、`listCommandAudits`。
- `recordsApi.ts` exports: record list/read/revision/lifecycle、draft CRUD（含 cursor 分页）、`searchRecords`、permanent-delete preview/execute/status helpers、`resolveComparisonCandidates` / `evaluateFixedComparison` / `saveComparisonRecord` / `saveComparisonRevision`；canonical DTO 位于 `types.ts`。`/records/compare` 与三个入口可静态导入该 façade，仍不得进入 AppShell。

#### 3. Contracts

- domain façade 必须位于 `web/src/lib/`，只组合 `apiRequest.ts` primitives/query helper 和 `types.ts` type-only imports；需要领域 recovery metadata 时可静态组合一个纯 allowlist decoder，并通过 transport 的 `ApiErrorDecoder` seam 注入。既有 façade 可经 `api.ts` compatibility re-export 逐步迁移，但新 façade 不得反向依赖 eager `api.ts`。不得拥有第二套 `fetch`、401 hook、response reader、credentials 默认值或绕过 decoder seam 的错误路径。
- **设计决策**：domain-only decoder 不得从 shared eager transport 动态导入；即使 decoder 实现位于 async chunk，dynamic-import loader 与 hashed chunk name 仍会进入 entry。由 lazy façade 静态拥有 decoder、再显式注入 shared transport，才能让当前无 consumer 的整个领域 transport 从 production graph 消失，并避免依赖 hash 压缩波动碰 bundle ratchet。
- `withQuery` 按对象插入顺序编码；string 先 trim，省略空 string、`null`、`undefined` 和 `false`，保留 `true`、数字与 `0`。移动 owner 或新增 façade 不得改变该规则。
- `ApiError` 始终保留 `status/message`；默认 transport 不把业务 metadata带入 eager path。显式 decoder 中 `code` 只接受 string，`field_errors` 只复制 string `field/message` 项，`recovery` 默认类型为 `unknown`，领域消费者需要时使用对应 DTO 泛型/收窄。未知 JSON 顶层字段不得通过对象 spread 进入错误实例。
- 只有所有 production consumer 都位于 lazy route 时才允许拆分。AppShell、auth、router bootstrap 等启动路径使用的 helper 留在 `api.ts`，不能为了数字好看制造首屏 waterfall。
- helper 移动不得改变 method、path、query 顺序/省略规则、body 或 response 解包；原有 API/page tests 必须继续覆盖 wire shape。
- 每次拆分先 fresh production build，再运行 `bundle:check`。数值预算依 [质量规范](quality-guidelines.md) 的 advisory 策略报告；不得以 warning 放宽 lazy ownership、CSP 或错误隔离合同。Records MarkdownPreview 使用 route-lazy Markdown 栈；编辑器保持 textarea，CodeMirror 的 inline style 与 CSP 不兼容。
- page/component 只能 import façade，禁止直接 import `apiRequest.ts` 或调用 `fetch`。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| route-private helper 加入 eager façade | 检查 lazy ownership 和构建图；数值超限按 advisory 报告，不能掩盖架构回退 |
| helper 仍被 AppShell/启动路径使用 | 保留在 `api.ts`，不引入启动期动态请求 |
| domain façade 复制 fetch/response reader 或绕过 decoder seam | review/source gate 阻断，改为复用 transport primitive与显式 decoder |
| `api.ts`/AppShell/TopBar/Sidebar/eager router graph **静态**导入 `recordsApi.ts` | AST contract 阻断；production consumer 必须停在 lazy record route / shared record workspace chunk，不得进入 entry |
| AppShell 需要 records 数据（global search） | 只允许 `import()` 动态到达，且必须经 `pages/records/globalRecordSearch.ts` 这类领域模块，不得在 shell 里静态 import transport |
| entry 降低但 max async 超数值预算 | 报告 advisory warning 并评估原因；结构、lazy/CSP 合同仍必须通过 |

#### 5. Good/Base/Bad Cases

- Good: command audit、events 与 incident helper 进入 `observabilityApi.ts`，三个既有 lazy detail/events route 和新 audit route 共享一个小 async chunk，entry 与 max async 均通过。
- Good: `recordsApi.ts` 直接 type-import canonical DTO、复用 `apiRequest.ts` 并显式组合 `apiError.ts`；Records lazy record routes 可把它放进 shared async `RecordWorkspace` chunk，但 entry 仍不得包含该模块。synthetic lazy consumer 继续证明 dynamic-only 边界。
- Base: 同时被 AppShell 和 DashboardPage 使用的 `getDashboard` 留在 `api.ts`。
- Bad: 页面为了懒加载直接 `fetch('/api/command-audits')`，复制 credentials/error 处理。
- Bad: 从 `api.ts` re-export `recordsApi.ts` helpers，导致无页面的 transport 先进入 eager graph。
- Bad: 把所有 endpoint 机械拆成几十个文件，却没有 consumer/bundle 证据。

#### 6. Tests Required

- API wire tests继续断言 default、filters、cursor、incident/event query 与错误 transport 行为。
- `apiRequest.test.ts` 断言 query normalization、JSON body init、legacy status/message 默认路径，以及显式 decoder 的 allowlisted structured error / unknown recovery。
- `recordsApi.test.ts` 固定所有 Records URLs/methods、cursor/query、新/已有记录草稿 routing body、exact `If-Match`、普通/deletion idempotency header、body allowlist、comparison-candidates / comparisons / comparison-save 路径，以及 404/409/503 shape。
- `recordsTransportArchitectureContract.test.ts` 使用 TypeScript AST 和静态依赖图固定唯一 runtime dependencies `apiRequest.ts|apiError.ts`，禁止 raw fetch/UI/import drift 与 eager Records edge；negative fixture 必须证明可捕获 direct re-export 和 transitive import。
- `bundleBudgetContract.test.ts` 必须 fresh-build当前真实 app，证明 `recordsApi.ts` 已被 lazy record routes 消费且 `isEntry=false`；`apiError.ts` 同样不得进入 entry。另用 synthetic dynamic import 证明单独 lazy consumer 仍把两者放在同一个 dynamic chunk。
- 受影响 page tests继续覆盖 loading/error/data 与请求 inventory。
- `NODE_ENV=production npm --prefix web run build` 后运行 `npm --prefix web run bundle:check`，记录 entry/max async 实测值。

#### 7. Wrong vs Correct

```ts
// 错误：route-private helper 进入 eager façade，且通过抬预算掩盖入口增长。
// api.ts
export function listCommandAudits(...) { ... }
```

```ts
// 正确：新的 lazy domain façade直接复用唯一 transport/query owner并显式注入领域 decoder。
// recordsApi.ts
import allowlistedApiError from './apiError'
import { requestJSON as transportRequestJSON, withQuery } from './apiRequest'
import type { RecordListFilter, RecordListResponse } from './types'
function requestJSON<T>(path: string, init?: RequestInit) {
  return transportRequestJSON<T>(path, init, allowlistedApiError)
}
export function listRecords(filter?: RecordListFilter) {
  return requestJSON<RecordListResponse>(withQuery('/api/records', filter ? {
    q: filter.q,
    lifecycle: filter.lifecycle,
    cursor: filter.cursor,
  } : undefined))
}
```


### 数据格式化

- **所有面向用户的展示格式化都集中在 `web/src/lib/format.ts`**：时间 (`formatDateTime`)、百分比 (`formatPercent`)、数值 (`formatNumber`)、字节 (`formatBytes` / `formatBytesPerSecond`)、延迟 (`formatLatency`)、运行时长 (`formatUptime`)、标签拼接 (`formatLabelList`)。
- 缺失值统一返回 `'—'`（`format.ts` 内多处约定），日期缺失返回 `'尚无'`。
- **不要在组件文件里手写 `Intl.DateTimeFormat` / `toFixed`**；如果需要新格式化，加到 `format.ts` + 在 `format.test.ts` 增用例。

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

> 该模式**当前由各 page 手抄**，未抽公共 hook。等到 ≥ 5 个 page 重复且能稳定下来时，再考虑提取 `useResource(loader, deps)`——目前不抽。

---

## 本地组件状态

- **`useState` 是默认选择**。同一 page 内多块独立 state 倾向于多个 `useState`，不强行合并（参考 `web/src/pages/MonitoringPage.tsx:144-163` 内 ~14 个 `useState`，各自语义清晰）。
- **`useRef` 用于不触发 render 的可变值**（DOM 引用、focus 还原 token、回调最新值）。典型例子：`web/src/pages/MonitoringPage.tsx:162-163` 的 `actionButtonRefs` / `pendingFocusRestoreRef`。
- **`useReducer` 当前未使用**。如某个 page 状态机分支 ≥ 4 个动作 + state 之间相互依赖，可以考虑引入；目前的页面用多个 `useState` + 描述性的 update 函数已经够。
- **派生状态不要存 state**：能用 `monitoringInstances.filter(isBindingConflictMonitoringInstance)` 现算的就别 `useEffect` 同步进 state（参考 `MonitoringPage.tsx`）。
- **丢弃 draft 必须同步重置依赖该 draft 的派生 UI state**。如果表单草稿会驱动可见集合、展开项、选中项或 pending target，`不保存` / 取消 / Esc / overlay 关闭不能只把 `form` 重置为已保存值，还要把这些派生 state 重建或裁剪到已保存数据范围。否则用户选择丢弃后，页面会继续显示未保存的通道、分组或展开面板。

```tsx
// 错误：只重置 form，activeChannels 仍保留未保存新增的 Telegram。
setState((current) => ({ ...current, form: buildFormState(settings) }))

// 正确：用已保存 settings 重建 form，并同步重置由 form/settings 派生的可见集合。
const resetForm = buildFormState(settings)
const resetChannels = deriveActiveChannels(settings, resetForm)
setState((current) => ({ ...current, form: resetForm }))
setActiveChannels(resetChannels)
setExpandedChannels((prev) => new Set(Array.from(prev).filter((channel) => resetChannels.has(channel))))
```

---

## 跨组件 / 跨页状态

当前有三条 Context：

| Context | 文件 | 提供值 | 消费方式 |
|---------|------|--------|----------|
| Auth | `web/src/lib/auth-context.tsx` | `{ user, loading, login, logout, refresh }` | `useAuth()`，必须在 `<AuthProvider>` 内调用，否则抛错 |
| Theme | `web/src/lib/theme-context.tsx` | `{ preset, mode, setPreset, setMode }` | `useTheme()`（必须在 Provider 内）/ `useThemeOptional()`（测试便利） |
| VPS write registry | `web/src/lib/vpsWriteRegistry-context.tsx` | user-scoped `VPSWriteOwnerStore` | `useVPSWriteRegistry()`；direct page/component tests 可使用 optional hook + injected/local fallback |

Auth/Theme 在 `web/src/main.tsx` 根链挂载；VPS registry 在 `AuthenticatedAppShell key={user.user_id}` 内包住 `<Outlet />`，保证跨受保护路由延续、用户切换销毁。

**新增第四个 Context 的判断标准**：

1. 数据真的需要被树形多个子树消费（如多 page、多 layout 子树）。
2. 不是纯服务端数据缓存（那种应在引入 React Query 之类时再统一）。
3. 写入路径有限且语义清晰（如全局开关、当前组织 ID）。

满足后落到 `web/src/lib/<name>-context.tsx`，导出 `<Name>Provider` + `use<Name>`；按真实生命周期显式挂在根 Provider 链或 authenticated AppShell，禁止在会被业务 route/gate 重挂载的 page 内偷偷挂。

---

## 数据拉取时机（实读约束）

- 当前 page 数据拉取以 **mount 时拉一次** + **用户操作触发重拉** 为主。`useEffect` 触发条件主要是 page 入参（`useParams` 拿到的 `monitoringInstanceId` / `targetId`）或筛选条件（如 `EventsPage` 的 `appliedFilters`）。AppShell 的 Dashboard 摘要是明确例外：mount 后还会在 document 重新可见 / window focus 时保守刷新，并做 in-flight 去重；仍不启用常驻轮询。

- **没有跨 page 的请求缓存**：从 `/monitoring` 进 `/monitoring/:id` 会再发一次 `getMonitoringInstance`。当前体量可以接受；如果未来要去抖 / 缓存，再考虑 React Query。

---

## 反模式

> 这些是当前代码已经回避（或承认偿还）的写法，**新代码不要做**。

- ❌ **page / component 里直接 `fetch()`**：业务请求必须走 `web/src/lib/` 的 owning façade，认证请求走 `web/src/lib/auth-client.ts`。
- ❌ **手抄后端字段名 / 自己拼 URL 格式化**：从 `lib/types.ts` import 类型 + 在 owning façade 复用 `api.ts` 的 `withQuery` 模式构造查询参数。
- ❌ **驼峰化后端字段**：保持 snake_case（`monitoring_instance_id`、`current_health_status`），便于和 center 端 grep 对齐。
- ❌ **跨 page 共享 mutable 全局变量**：不要用模块级 `let` / Map 缓存业务数据或 plaintext token；监控实例安装命令只从 center 生成并在当前 onboarding page 状态中展示。
- ❌ **绕过 ApiError 直接 throw 字符串**：`lib/api.ts` 已统一错 `ApiError(status, message)`，page 用 `instanceof ApiError` 判别后挑 `.message` 展示。
- ❌ **在 `useEffect` 里写 state 不带 `cancelled` 旗标**：StrictMode 下 effect 会触发两次，不防护会出现 setState on unmounted。
- ❌ **把派生数据存 `useState`**：能现算就不要二次同步。
- ❌ **预先引入 React Query / Zustand / Redux**：当前没有任何 page 真的需要它们；引入新依赖需独立技术决策。
- ❌ **在 component / page 写 inline `Intl.*` / `toFixed`**：交给 `lib/format.ts`。

---

## 当前约定与已知 gap

> 用于后续代码评审；若形成可复用规则，更新 `docs/spec/` 或 `docs/design/`。

1. **认证请求与业务请求共享 `apiRequest.ts` 的 transport primitives、`withQuery` 和 401 hook**；`api.ts` 拥有 eager/shared endpoint façade与兼容 re-export，bundle-evidenced domain façade只做 endpoint/显式 decoder 组合，新代码不要再加第二套 fetch 包装。
2. **类型与 Go contract 全靠手维护**——没有 codegen。前后端字段如有漂移，依赖测试 + 运行期 `unknown` 解析报错暴露。
3. **当前没有任何状态库 / 数据缓存层**：本 spec 把"暂不引入"作为现行约束；引入新依赖需有实际需求和独立技术决策。

---

## Examples

仓库内"数据获取写得好"的真实参考点：

- **标准 page 数据流（loading / error / data 三态 + cancelled 旗标）**：`web/src/pages/EventsPage.tsx`，配合 `web/src/lib/observabilityApi.ts` 的 `listEvents(filter)`。
- **乐观锁更新**：`web/src/pages/MonitoringPage.tsx:283-325` 的 `handleSaveLabels` → `updateMonitoringInstanceMetadata(monitoringInstanceId, input, { expectedUpdatedAt })`，其内部由 `web/src/lib/api.ts:145-153` 走 `If-Match` 头实现。
- **Provider + Hook 配对**：`web/src/lib/auth-context.tsx`（`AuthProvider` 内 `useEffect` 挂 401 钩子 → 用 `useAuth()` 暴露 `{ user, loading, login, logout, refresh }`）。
- **类型驱动的 API 函数集**：`web/src/lib/api.ts` / `observabilityApi.ts` 从 `./types` 引入领域类型并复用唯一 transport/query primitives；transport 分支由 `apiRequest.test.ts` 的 >=90% branch ratchet 保护。


## 领域状态合同

页面行为请按 [领域合同索引](../contracts/README.md) 选择；VPS 写入归属见 [异步所有权](../contracts/assets-async-ownership.md)，订阅请求协议见 [订阅合同](../contracts/subscriptions.md)。

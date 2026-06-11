# 状态与数据

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

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
  - 业务函数使用动词 + 资源命名（`listMonitoringInstances` / `getMonitoringInstance` / `createTarget` / `updateMonitoringInstanceMetadata` / `enterMonitoringInstanceMaintenance` / `getDashboard` 等），返回 `Promise<T>`，T 来自 `lib/types.ts`。
  - `If-Match` 乐观锁通过 `patchJSONBody(path, body, { ifMatch })` 表达（`web/src/lib/api.ts:98-112`），传入的是上一次拿到的 `updated_at`。
- **`/api/auth/*` 走 `web/src/lib/auth-client.ts`**，并复用 `web/src/lib/api.ts` 的 request helpers 与 401 hook。不要新增第二套 fetch 包装。
- **不要在 page / component 里直接 `fetch()`**。业务请求必须加到 `web/src/lib/api.ts` 再由 page / component 调用；`MonitoringPage` 的历史直连 `fetch('/api/monitoring-instances')` 已偿还为 `createMonitoringInstance` API helper，新代码不要恢复这条路径。

### 类型对齐

- 与 center 响应 / 请求体一一对应的类型集中在 `web/src/lib/types.ts`：`MonitoringInstanceRecord`、`TargetRecord`、`ProbeItemRecord`、`StateChangeEventRecord`、`DashboardOverview`、`SettingsRecord` 等；命名遵循 `<Aggregate>Record`（响应行）/ `<Aggregate>Input` / `<Aggregate>Override` 后缀。
- 字段名**完全镜像 center JSON**（snake_case，如 `monitoring_instance_id` / `current_health_status` / `last_heartbeat_at`）。**不要在前端再驼峰化一遍**——保持 grep 友好，便于和 Go 侧 `internal/center/http/handlers/*` 对齐。
- 中文枚举（如 `IncidentSeverity = '正常' | '关注' | '告警' | '严重'`、`OnboardingPhase` 等）来自 center，前端原样保留中文字面量；展示标签通过 `STATE_CHANGE_EVENT_TYPE_LABELS` (`web/src/lib/types.ts:202-221`) 这种 const map 二次映射，**不要散落到组件文件**。
- **当前类型是手写**，与 Go contract 没有自动生成机制。新增字段时按以下顺序：1) center handler / contract 改完；2) 在 `lib/types.ts` 加字段（保持 snake_case、保持可选性与后端一致）；3) 在 `lib/api.ts` 引用；4) page / component 消费。

### MonitoringInstance onboarding 一键安装数据流

#### 1. Scope / Trigger

- Trigger: 修改 `MonitoringDetailPage`、`MonitoringInstanceInstallCommandIssue`、`issueMonitoringInstanceInstallCommand`、MonitoringInstance 创建后跳转 onboarding 的流程，或任何安装命令展示/复制行为。

#### 2. Signatures

- Frontend type: `MonitoringInstanceInstallCommandIssue` fields mirror center JSON snake_case: `command`, `issued_at`, `expires_at`, `installer_url`, `public_base_url`, `agent_version`, `release_repo`。
- Frontend API: `issueMonitoringInstanceInstallCommand(monitoringInstanceId)` -> `POST /api/monitoring-instances/{monitoring_instance_id}/install-command`。
- Page flow: `MonitoringPage` 创建 MonitoringInstance 后跳转 onboarding；`MonitoringDetailPage` 按用户操作生成/重新生成 center command，不再依赖 create flow 预发 plaintext token。

#### 3. Contracts

- Browser must never construct the production install command from `window.location.origin`, route params, or request metadata. The center-generated `issue.command` is the only command shown for copy.
- The command contains a one-time enrollment token; UI should hide/reveal/copy deliberately, show expiry metadata, and avoid rendering full token in incidental notices or conflict-resolution copy.
- Config errors from backend 409 (`public base URL is not configured`, `agent release version is not configured`) are actionable deployment errors and should be displayed as-is.
- Binding conflict UI must not say the enrollment token is unchanged; a pending fingerprint attempt may have consumed it, so operators may need to regenerate after confirm/reject.
- Manual fallback can remain for troubleshooting, but it must be secondary to center-generated one-command install.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Generate command succeeds | page shows command, expiry, installer URL, version/repo metadata, and copy controls |
| User regenerates command | old visible command is replaced by new center response |
| 409 public URL/version config error | page shows backend message and does not synthesize fallback command |
| Binding conflict displayed | copy explains one-time token may be consumed and regeneration may be required |
| Create MonitoringInstance succeeds | navigate to onboarding page instead of caching a plaintext token in module global state |

#### 5. Good/Base/Bad Cases

- Good: user clicks generate, reviews masked/revealed command, copies the exact backend command, and runs it on a VPS.
- Base: command expired; user clicks regenerate and copies the replacement.
- Bad: page builds `curl ${window.location.origin}/api/agent/install.sh ...` and silently ignores missing `HOUFENG_PUBLIC_BASE_URL`.
- Bad: create flow stores plaintext enrollment token in module/global state for cross-page transfer.

#### 6. Tests Required

- `web/src/lib/api.test.ts`: `issueMonitoringInstanceInstallCommand` posts to `/api/monitoring-instances/{id}/install-command`.
- `MonitoringDetailPage.test.tsx`: generate success, regenerate, reveal/hide/copy, config errors, metadata display, and conflict copy warning about consumed one-time tokens.
- `MonitoringPage.test.tsx`: create flow navigates to onboarding without pre-issuing or caching an enrollment token.

#### 7. Wrong vs Correct

```tsx
// 错误：浏览器自己拼安装命令，绕过 center 的 URL/version/token contract。
const command = `curl -fsSL ${window.location.origin}/api/agent/install.sh | sudo sh -s -- --server-url ${window.location.origin}`
```

```tsx
// 正确：只展示后端返回的命令。
const issue = await issueMonitoringInstanceInstallCommand(monitoringInstanceId)
setInstallIssue(issue)
```

### VPS 详情 agent 接入/升级复用已有 MonitoringInstance

#### 1. Scope / Trigger

- Trigger: 修改 VPS 详情页普通 agent 接入入口、`workbench=monitoring` / `workbench=monitoring-instance-create` 深链、VPS ↔ MonitoringInstance link 写路径、或 MonitoringInstance onboarding 文案。
- 目标：VPS 已有 active MonitoringInstance link 时，普通 agent 接入入口必须复用现有监控实例进入升级/重新接入流程，避免误创建第二个 active 监控实例。

#### 2. Signatures

- Frontend deep link: `/vps/{vps_id}?workbench=monitoring` and `/vps/{vps_id}?workbench=monitoring-instance-create`。
- Frontend upgrade target: `/monitoring/{monitoring_instance_id}?onboarding=1&return_vps={vps_id}`。
- Backend create API: `POST /api/vps/{vps_id}/monitoring-instances`。
- Backend link API: `POST /api/vps/{vps_id}/link-monitoring-instance`。
- Backend domain error: `assetlinks.ErrVPSActiveMonitoringInstanceExists` maps to HTTP 409.

#### 3. Contracts

- 0 active links: VPS detail may show `创建并接入 agent`; submit may call `createVPSMonitoringInstance`, then navigate to MonitoringInstance onboarding.
- 1 active link: VPS detail must show `升级/重新接入 agent`; clicking or opening either monitoring workbench deep link must navigate to the existing MonitoringInstance onboarding and must not call the create API.
- More than 1 active link: do not auto-clean historical data; hide create/link entry, show a duplicate-active-link warning, and keep per-row `升级/重新接入 agent` plus `解除关联`.
- Backend create/link write paths must lock/check active links before inserting and return 409 on existing active links. The create path must not insert an orphan `monitoring_instances` row on conflict.
- Monitoring detail onboarding keeps using `issueMonitoringInstanceInstallCommand`; only copy/title changes between `接入 agent` and `升级/重新接入 agent` based on bound/observed state.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| VPS has 0 active links and user opens monitoring workbench | Open create drawer; create API allowed |
| VPS has 1 active link and user opens monitoring workbench | Navigate to existing `/monitoring/{id}?onboarding=1&return_vps={vps_id}` |
| VPS has >1 active links | Show manual duplicate review warning; no create/link CTA |
| Create/link API sees existing active link | 409 `vps active monitoring instance exists` |
| Create API sees existing active link | No `monitoring_instances` insert before returning error |

#### 5. Good/Base/Bad Cases

- Good: already-connected VPS upgrades agent by reusing its existing MonitoringInstance and generating a fresh install command there.
- Base: brand-new VPS without active link creates one MonitoringInstance and enters onboarding.
- Bad: a button labeled `创建并接入 agent` on an already-linked VPS calls `POST /api/vps/{id}/monitoring-instances` and creates a second active monitoring instance.
- Bad: only the frontend blocks duplicate creation while backend link/create endpoints still allow another active link.

#### 6. Tests Required

- `VPSDetailPage.test.tsx`: 0/1/multiple active-link behavior, monitoring workbench deep-link branching, and no create API call when reusing an active link.
- `MonitoringDetailPage.test.tsx`: bound or already-observed instances use `升级/重新接入 agent` wording while still issuing install commands through the existing API.
- Handler/store Go tests: create/link return 409 for existing active links; store create checks active links before inserting MonitoringInstance.

#### 7. Wrong vs Correct

```tsx
// 错误：深链和按钮都无条件打开创建流程。
if (workbench === 'monitoring') {
  setActiveDrawer('monitoring-instance-create')
}
await createVPSMonitoringInstance(vpsId, input)
```

```tsx
// 正确：先按 active link 数量分流。
if (activeLinks.length === 1) {
  navigate(`/monitoring/${activeLinks[0].monitoring_instance_id}?onboarding=1&return_vps=${vpsId}`)
} else if (activeLinks.length === 0) {
  setActiveDrawer('monitoring-instance-create')
} else {
  setActiveDrawer('monitoring-instance-evidence')
}
```

### MonitoringInstance 详情 metadata 与运行态合并

#### Contracts

- `MonitoringDetailPage` 把 `group`、`labels`、`note` 视为资料维护字段；保存必须走 `updateMonitoringInstanceMetadata(monitoringInstanceId, input, { expectedUpdatedAt })`，并通过 `If-Match` 使用当前 `updated_at`。
- 运行控制、绑定接入确认、命令轮询、实时样本等非资料更新即使返回整条 `MonitoringInstanceRecord`，前端合并到当前 state 时也必须保留当前 `group`、`labels`、`note`。这些响应可能来自资料保存之前发出的请求，不能覆盖用户刚保存的资料。
- 资料保存成功后只把 `group`、`labels`、`note`、`updated_at` 合并回当前监控实例，不能把保存响应里的运行态字段反向覆盖掉更新后的 `monitoring_status`、`binding_status`、`last_action` 或心跳事实。
- 切换 `monitoringInstanceId` 时必须重建 metadata draft、清理提交中状态和错误，避免旧实例的资料草稿或错误泄漏到新实例。

#### Tests Required

- `MonitoringDetailPage.test.tsx` 覆盖：编辑 Group / labels / note、取消后 draft 重置、PATCH payload 含 `If-Match`、保存后页面显示新资料。
- `MonitoringDetailPage.test.tsx` 覆盖：非资料运行态响应返回旧 `group` / `labels` / `note` 时，详情页仍保留当前资料字段。

#### Wrong vs Correct

```tsx
// 错误：运行态响应整条覆盖，可能把刚保存的 Group/标签/备注打回旧值。
setMonitoringInstance(updated)
```

```tsx
// 正确：非资料响应只更新运行态字段，保留当前资料字段。
setMonitoringInstance({
  ...updated,
  group: current.group,
  labels: current.labels,
  note: current.note,
})
```

### MonitoringInstance 管理入口与归档工作集

#### 1. Scope / Trigger

- Trigger: 修改 `MonitoringPage` 列表范围、监控实例批量操作、`MonitoringDetailPage` 详情管理入口、归档实例详情行为、或 `lib/api.ts` 中 MonitoringInstance 管理接口。
- 目标：前端必须把 MonitoringInstance 作为可管理对象展示，不再只有“创建并接入 agent”路径；归档和永久清理这类危险操作必须通过统一管理审查入口承载。

#### 2. Signatures

- Frontend list API: `listMonitoringInstances(scope?: 'active'|'archived'|'all')`；`active` 不拼 query，`archived/all` 使用 `scope` query。
- Frontend review API: `getMonitoringInstanceManagementReview(monitoringInstanceId)`。
- Frontend action APIs: `retireMonitoringInstance`、`restoreMonitoringInstanceLifecycle`、`archiveMonitoringInstance`、`restoreMonitoringInstanceFromArchive`、`permanentCleanupMonitoringInstance`。
- Types: `MonitoringInstanceRecord.archived_at?`、`archived_reason?`、`MonitoringInstanceManagementReview`、`MonitoringInstanceManagementCounts`、`MonitoringInstanceManagementActions`、`MonitoringInstancePermanentCleanupResult`。
- URL-state: `/monitoring?scope=archived|all`；省略 `scope` 表示当前 active 工作集。

#### 3. Contracts

- `MonitoringPage` 默认请求 `/api/monitoring-instances`，不带 `scope=active`；切换 `已归档` / `全部` 才写 URL query。清空筛选必须保留当前 scope。
- 列表 scope switch 只改变工作集范围，不应清掉用户其他筛选；scope 切换时必须清理批量选择、批量面板和批量错误，避免把旧工作集选择带到新工作集。
- 批量运行控制只作用于未归档实例：`batchEligibleMonitoringInstances = sortedFilteredMonitoringInstances.filter(!archived_at)`。`scope=all` 下用户全选时，也只把 eligible IDs 发给 batch/action API。
- 如果筛选变化导致 eligible 数量变成 0，批量动作不得发送空请求，也不得让 `batchSubmitting` 停留为 true；应关闭/重置批量面板或保持可恢复状态。
- 详情页的管理审查必须懒加载：用户打开“管理实例”入口时再请求 review，避免破坏详情页既有轮询 / runtime / onboarding 请求顺序。
- 管理动作成功后必须刷新当前 record 和 review；永久清理成功后导航回 `/monitoring`。
- 归档实例详情仍可浏览历史和管理入口，但必须隐藏或禁用 onboarding、runtime action、command action 和 metadata edit。metadata section 应显示只读原因。
- 管理危险操作必须复用 `ActionConfirmationModal` 风格；退役 / 恢复需要 reason，归档 / 永久清理需要 reason + 实例显示名确认。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `/monitoring` without scope | Fetch `/api/monitoring-instances` |
| switch to archived | Fetch `/api/monitoring-instances?scope=archived` and hide active rows |
| switch to all | Fetch `/api/monitoring-instances?scope=all` and show active + archived |
| clear filters while scope=archived/all | Preserve `scope` query |
| all selected rows are archived | No batch/action request; no stuck `批量操作中…` state |
| archived detail page | Management visible; runtime/onboarding/metadata edit hidden |
| management review load failure | Show management error in the management section only |
| permanent cleanup success | Navigate to `/monitoring` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 `全部` 范围看到 active 与 archived 实例，打开批量操作时只显示并提交 active eligible 数量。
- Good: 用户打开归档实例详情，只能查看历史和进入管理，不会看到生成安装命令、恢复监控或编辑资料入口。
- Base: 用户从详情打开管理入口，review 加载失败；页面保留详情主体，只在管理区域显示错误。
- Bad: 默认列表请求 `scope=all`，把归档实例重新混入日常工作集。
- Bad: 列表里只按 UI 隐藏归档行，但批量 API payload 仍包含 archived IDs。
- Bad: 管理动作成功后只更新详情 record 不更新 review，导致 blockers/actions 仍显示旧状态。

#### 6. Tests Required

- `api.test.ts`: list scope query、review endpoint、retire/restore/archive/restore archive/permanent cleanup body。
- `MonitoringPage.test.tsx`: 默认 active 请求、scope 切换、清空筛选保留 scope、archived 从批量操作中排除、eligible 为空不提交且不保留 submitting。
- `MonitoringDetailPage.test.tsx`: 管理 review 展示、阻塞项/计数/VPS link、确认名、每个管理动作后刷新、归档详情隐藏 runtime/onboarding/metadata edit、cleanup 后导航。
- `monitoringDetailHelpers` tests or page assertions: archived / retired runtime actions 返回空。

#### 7. Wrong vs Correct

```tsx
// 错误：全量视图下把归档实例也提交给批量运行控制。
const ids = sortedFilteredMonitoringInstances.map((record) => record.monitoring_instance_id)
await postMonitoringInstanceBatch(ids, action)
```

```tsx
// 正确：批量动作只提交未归档实例，并在空目标时提前恢复 UI 状态。
const ids = batchEligibleMonitoringInstances.map((record) => record.monitoring_instance_id)
if (ids.length === 0) {
  setSelectAll(false)
  setBatchPanelOpen(false)
  return
}
await postMonitoringInstanceBatch(ids, action)
```

### 数据格式化

- **所有面向用户的展示格式化都集中在 `web/src/lib/format.ts`**：时间 (`formatDateTime`)、百分比 (`formatPercent`)、数值 (`formatNumber`)、字节 (`formatBytes` / `formatBytesPerSecond`)、延迟 (`formatLatency`)、运行时长 (`formatUptime`)、标签拼接 (`formatLabelList`)。
- 缺失值统一返回 `'—'`（`format.ts` 内多处约定），日期缺失返回 `'尚无'`。
- **不要在组件文件里手写 `Intl.DateTimeFormat` / `toFixed`**；如果需要新格式化，加到 `format.ts` + 在 `format.test.ts` 增用例。

---

### Dashboard 数据可信度

- `DashboardPage` 是全局工作台，但只能展示 `getDashboard()` / `/api/dashboard` 已明确返回的事实，并且**不得默认展示所有 contract 字段**。当前可用事实来自 `DashboardOverview`：dashboard 生成时间、总监控实例/目标数、异常/严重/维护计数、库存完整度计数、24h 新异常/恢复趋势、真实全量 `group_summaries`、通知配置布尔摘要、异常监控实例/目标摘要、最近事件；这些字段是可用事实池，不是首页全部展示清单。
- Dashboard 首屏按 asset-decision-first command surface 做渐进披露：顶部只展示一个 `工作台 command surface`，并把现有优先级决策收敛成一个主行动块 `今日第一步`。资产决策与观测异常事实可以作为同一 surface 内的证据 lane 支撑主行动；刷新、自动刷新、管理入口和非主行动链接必须降为低权重控制或 `次级动作`，不得恢复成与主 CTA 同权的第三 lane。异常态下方继续展示统一异常处理队列、工作台内 `运行上下文` 和紧凑 `管理入口`；其中处理队列仍是主任务，运行上下文与管理入口只能作为队列下方的辅助跳转，不得拆成独立 page section。正常 / 维护态下方展示运行概览、运行上下文与紧凑管理入口；首次接入态下方只展示 onboarding。不要为了“contract 已返回”而渲染 API loaded facts、独立 KPI/summary strip、`系统快捷入口` 详情列表、`Group 摘要` 列表或 `最近事件摘要` 列表。
- Command surface 的顶部可以展示一个低高度 `今日判断摘要` 轨道，但它只能汇总当前最高优先级判断：资产压力 / 资产主线、严重异常 / 观测异常 / 维护观察 / 观测稳定、以及第一条下一步动作。它不是 KPI strip，不得扩展为全量 dashboard metric 列表；每项必须链接到已有 Dashboard 深链承接页，且 390px 视口下折叠为单列。
- `snapshot_generated_at` 只能写成 `生成时间`、`摘要生成` 这类接口生成时间提示。它不是 Center health、agent heartbeat、sync freshness 或全链路实时性证明，不要写 `中心运行正常` / `同步于` / `健康检查通过` 之类文案。
- `abnormal_monitoring_instances` / `abnormal_targets` 只能代表当前异常对象队列，**不能**推导全量 group / region / provider 分布。`group_summaries` 必须来自后端全量聚合，但它默认不在 Dashboard 首屏展开；如果未来重新展示 Group 上下文，必须保持轻量、服务当前状态决策，数组为空时只显示轻量说明，不在前端制造 `未分组 0` 行。
- `recent_events` 默认不在 Dashboard 首屏展开成事件列表。Dashboard 只保留 `查看事件流` / `/events?time_range=24h` 这类入口；复杂历史筛选、事件列表和上下文展开交给 EventsPage。
- Dashboard 可以在主工作台内部展示一个低权重 `运行上下文` strip，用于补充同类服务器管理系统常见的影响范围、库存状态、最近活动。该 strip 最多 3 个 link item：不得恢复独立 KPI/summary strip，不得使用 `Group 摘要` / `最近事件摘要` heading，不得展示完整 group list 或 recent event summary 列表。最近活动只展示事件类型、严重度、对象和时间语义入口，具体事件摘要交给 EventsPage。视觉上它应是工作台内的 compact context rail，而不是三个同权摘要卡片。
- `notification_status` 只能展示配置布尔摘要，例如 Telegram / Feishu 是否已配置、Telegram runtime apply 是否生效。前端不得要求或展示 `telegram_bot_token`、`telegram_chat_id`、`feishu_webhook_url` 等敏感配置值；需要编辑真实配置时跳转 SettingsPage。
- `asset_summary` 只能展示 VPS Asset Ledger 的少量组合判断入口：30 天续费、待决策、待取消/迁移、未关联监控实例、关联异常 VPS、按币种月付成本。它应集中出现在 Dashboard 首屏 `资产组合决策` lane 中，避免在下方工作台重复出现同权资产卡片。它不能展开资产明细，不能替代 VPS / 订阅页面，也不能把 Dashboard 变成资产字段总表。入口必须深链到 `/asset-decisions?view=...&renew_within_days=30` 或 VPS/观测列表的明确筛选，而不是裸 `/asset-decisions` 或旧 `single_queue` 主入口。
- 系统入口可以展示 dashboard contract 支撑的库存完整度事实，例如待接入监控实例、暂停监控实例、退役监控实例、暂停目标、归档目标。Dashboard 深链是受支持 contract：`/monitoring?onboarding=pending` 表示待接入或绑定待处理监控实例，`/monitoring?abnormal=1` 表示异常监控实例，`/targets?abnormal=1`、`/targets?run_status=暂停`、`/targets?run_status=已归档` 表示对应目标列表筛选，`/events?severity=严重`、`/events?time_range=24h`、`/events?maintenance_only=1` 表示事件页筛选；新增深链必须先在目标页面用 URL-state 和可见 chip/toggle 承接。
- AppShell 可以复用 `getDashboard()` 做轻量 shell summary，但只能把它标成 dashboard 摘要来源。加载中显示“正在读取系统摘要”，失败显示“摘要不可用”；不要写死 `center ok`、`中心运行正常`、`sync HH:mm:ss` 或用浏览器当前时间伪装后端同步时间。Sidebar 的监控实例/目标 count 可以来自 `abnormal_monitoring_instance_count` / `abnormal_target_count`，但加载中/失败时必须由 Shell 状态说明 0 count 不代表无异常。

```tsx
// 错误：从异常摘要伪装成全量分布
const groupSummaries = overview.abnormal_monitoring_instances.reduce(...)
<GroupContextSummary groups={groupSummaries} />

// 错误：把 dashboard 生成时间写成同步/健康状态
<Timestamp value={overview.snapshot_generated_at} /> 同步完成

// 错误：要求 dashboard 暴露敏感通知配置
overview.notification_status.telegram_bot_token

// 错误：把资产摘要扩成资产明细 dump
overview.asset_summary.vps_assets.map(...)

// 错误：没有真实 health/sync contract 时伪造 Shell 健康状态
<SyncStatus state="ok" label="中心运行正常" meta={`v1.0 · sync ${new Date().toISOString()}`} />

// 错误：把 dashboard contract 当作首屏展示清单
<GlobalKpiStrip items={['监控实例', '目标', '严重', '维护', '24h 变化']} />
<ShortcutRail title="系统快捷入口" entries={allEntryDescriptions} />
<GroupContextSummary groups={overview.group_summaries} />
<RecentEventsContext events={overview.recent_events} />

// 正确：只展示支撑当前决策路径的 dashboard contract 事实
<DashboardCommandSurface overview={overview} lanes={['资产组合决策', '观测异常队列', '下一步动作']} />
<DashboardWorkbench title="当前需要处理" attentionItems={attentionItems} />
<DashboardContextStrip items={['影响范围', '库存状态', '最近活动']} />
<span>摘要生成 <Timestamp value={overview.snapshot_generated_at} /></span>
<SyncStatus state="degraded" label="正在读取系统摘要" meta="v1.0 · dashboard loading" />
```

### Monitoring 列表工作台状态

MonitoringPage 是运行证据扫描页，不应把筛选、批量操作、趋势开关和刷新控制全部平铺成首屏同权入口。主路径是 quick view → 监控实例列表 → 行级处理；高级字段筛选和批量操作是次级控制。

#### Contracts

- Quick view 负责表达当前扫描主线，至少覆盖全部、异常、待接入、维护/暂停、绑定异常；维护/暂停视图必须同时包含 `monitoring_status === '维护中'` 与 `monitoring_status === '暂停'`，不要用单个 `run_status` 推断。
- 高级筛选使用 Drawer 的 applied/draft 分离：打开时从当前已应用筛选初始化草稿；只有点击完成/应用才提交到列表状态；取消、Esc、overlay 和头部关闭必须丢弃草稿，不能改变列表或触发隐式请求。
- 高级筛选计数只统计已应用字段筛选，必须覆盖 lifecycle、health、monitoring/run status、group、region、labels、search 等会改变列表的维度；quick view 本身不混入字段筛选计数。
- 批量操作区默认隐藏；只有用户显式打开批量操作、已经选择全量/部分监控实例、存在待确认批量动作、提交中或错误需要展示时才出现。批量动作按钮仍必须以明确选择为前提，不因列表有数据而默认高亮。
- MonitoringPage 不承载资产判断支撑面，也不展示 MonitoringInstance 资产上下文列；Hero 之后应直接进入 toolbar/filter/batch/table。资产侧判断导向资产决策页、VPS 库存 / 详情和取消退役工作台，Monitoring 列表只保留运行观测扫描职责。

#### Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Drawer 内修改筛选后取消 / Esc / overlay 关闭 | 列表不变；重新打开 Drawer 恢复当前 applied filters |
| 点击完成 / 应用筛选 | Drawer 关闭，列表和 visible chips 使用新筛选 |
| runtime attention quick view | 同时显示维护中与暂停监控实例；空态不得误报为全局无监控实例 |
| 无选择且未打开批量操作 | 批量 bar 不渲染 |
| 打开批量操作但未全选 | 显示范围/选择入口，不显示实际批量动作按钮 |

### Asset Ledger 组合决策与列表数据流

Asset Ledger 的列表页可以把现有 VPS 与 Subscription contract 在前端做轻量 join，用于人工核对和资料质量提示；这不是后端字段扩展，也不能创造未存在的健康语义。`/asset-decisions` 例外地使用后端组合决策 read model 作为主语义来源：它把 VPS、订阅、服务、域名、Target 和监控关联聚成自动决策组；同时通过手工组合 scenario layer 保存用户正在比较的真实问题篮子，通过决策记录 memory layer 保存一次判断和证据快照。手工组合和记录层都不拥有 VPS / Subscription / MonitoringInstance / Target 的执行状态机。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/pages/AssetDecisionsPage.tsx`、`web/src/pages/VPSPage.tsx`、`web/src/pages/assetPageUtils.ts`、`AssetDecisionWorkPanel`、或改变 VPS/Subscription 列表页的筛选 URL-state。

#### 2. Signatures

- Frontend API: `getAssetDecisionOverview(filter?)`, `listAssetDecisionGroups(filter?)`, `getAssetDecisionGroup(groupId, filter?)`, `listAssetDecisionManualGroups(filter?)`, `createAssetDecisionManualGroup(input)`, `getAssetDecisionManualGroup(manualGroupId)`, `patchAssetDecisionManualGroup(manualGroupId, input)`, `addAssetDecisionManualGroupMember(manualGroupId, input)`, `patchAssetDecisionManualGroupMember(manualGroupId, vpsId, input)`, `deleteAssetDecisionManualGroupMember(manualGroupId, vpsId)`, `listAssetDecisionScenarioTemplates()`, `createAssetDecisionScenarioTemplate(input)`, `getAssetDecisionScenarioTemplate(templateId)`, `patchAssetDecisionScenarioTemplate(templateId, input)`, `createManualGroupFromScenarioTemplate(templateId, input)`, `listAssetDecisionRecords(filter?)`, `createAssetDecisionRecord(input)`, `getAssetDecisionRecord(recordId)`, `patchAssetDecisionRecord(recordId, input)`, `listVPSAssets(filter?)`, `listSubscriptions(filter?)`, `listProviders()`, `updateVPSAsset(vpsId, input)`。`listVPSAssets` / `listSubscriptions` 支持 `asset_scope='current'|'archived'|'all'`；普通页面不传 scope，使用后端默认 current，归档列表页显式传 archived，归档详情订阅历史使用 all。`updateVPSAsset` 仍返回 VPS record 字段，并可在取消类续费决策响应中附带 `renewal_subscription_linkage` 状态摘要。取消 / 退役协同使用 `getVPSCancellationPreview(vpsId)`、`applyVPSCancellation(vpsId, input)`、`listTargetAssetContexts()`；归档 / 恢复使用 `getVPSArchiveReview(vpsId)`、`archiveVPS(vpsId, input)`、`restoreVPSFromArchive(vpsId)`。页面不得直接 `fetch()`。
- Asset portfolio data: `AssetDecisionsPage` 首屏主 surface 从 `/api/asset-decisions/overview` 与 `/api/asset-decisions/groups?view=&renew_within_days=` 读取自动组；点击组时读取 `/api/asset-decisions/groups/{group_id}?renew_within_days=`。自动组是只读派生视图，不在前端保存或补写 group state；保存决策必须调用 `/api/asset-decisions/records`，由后端重新计算组详情并生成快照。
- Manual scenario data: 页面读取 `/api/asset-decisions/manual-groups` 展示“自定义组合” surface；点击组合读取 `/api/asset-decisions/manual-groups/{manual_group_id}`。从自动组创建手工组合 POST `source_type=auto_group`、`source_group_id`、`renew_within_days`、`scenario`、`title`、`goal`、`note`；成员增删改只调用 manual group member endpoints。新增成员必须用 VPS selector，从 `listVPSAssets()` 候选选择，不要求用户复制内部 ID。
- Scenario template data: 页面读取 `/api/asset-decisions/scenario-templates` 展示“场景模板” surface。内置模板只能用于创建自定义组合，不能 PATCH；自定义模板可从手工组合另存并可归档/启用。模板详情 `template_id` 深链只读取模板；从模板创建组合必须调用 `createManualGroupFromScenarioTemplate`，成功后打开新 manual group。模板不能直接创建 record，不能写 VPS / Subscription / MonitoringInstance / Target。
- Asset decision records data: 页面读取 `/api/asset-decisions/records` 展示“已保存组合决策”辅助 surface；点击记录读取 `/api/asset-decisions/records/{record_id}`；推进记录状态 PATCH `/api/asset-decisions/records/{record_id}` 的 `title` / `goal` / `status` 字段，成员跟进 PATCH 同 endpoint 的 `members:[{vps_id, followup_status?, followup_note?}]`，但不得修改 VPS/订阅/监控/Target。
- Asset decision execution plan data: `RecordSummary`、`RecordDetail` 和 `RecordMember` 可以包含只读 `execution_plan`，字段与后端 snake_case 对齐。记录级展示 `summary`、`lane_counts`、`actionable_count`、`blocked_count`；成员级展示 `lane`、`step_kind`、`tone`、`summary`、`step_label`、`issue_count`、`blocked`、`actionable`。前端只根据 `step_kind` 生成本地深链，后端不得返回 URL。
- Evidence assessment data: `AssetDecisionGroupSummary` 与 `AssetDecisionGroupMember` 必须包含 `evidence_assessment`；字段为 `confidence_score`、`pressure_score`、`readiness_score`、`quality_tier`、`decision_bias`、`support_signal_count`、`risk_signal_count`、`gap_signal_count`、`summary`。记录详情从 `evidence_snapshot.evidence_assessment` 读取保存时快照；历史记录缺失该字段时只显示降级文案。
- Decision recommendation data: `AssetDecisionGroupSummary`、`AssetDecisionGroupMember`、`AssetDecisionManualGroupSummary` 和 manual members 可以包含 `decision_recommendation`；UI 只展示短摘要、下一步、理由/阻塞 chips 和优先 VPS，不在前端重新评分，不把 recommendation 当作自动执行承诺。
- Comparison insight data: `AssetDecisionGroupSummary`、`AssetDecisionGroupMember`、`AssetDecisionManualGroupSummary` 和 manual members 可以包含只读 `comparison_insight`。组级字段为 `summary`、`primary_axis`、`lane_counts[]`、`priority_vps_ids[]`、`tradeoffs[]`；成员级字段为 `rank`、`lane`、`summary`、`strengths[]`、`risks[]`、`gaps[]`、`tradeoffs[]`。前端只展示后端 lane / rank / signals，不在浏览器重新分类或评分。记录详情从 `evidence_snapshot.comparison_insight` 读取保存时快照；历史记录缺失时显示“保存时未记录对比洞察”降级，不影响 readback / execution plan / followup。
- Single queue data: `AssetDecisionsPage` 底部辅助队列继续拉取续费窗口 subscriptions、全量 subscriptions（按 `renew_at asc`）、以及 `renewal_decision=unreviewed|migrate|cancel` 三个 VPS 切片。
- VPS inventory data: `VPSPage` 拉取 current `listVPSAssets()`、`listProviders()` 和 current `listSubscriptions({ sort: 'renew_at', order: 'asc' })`，在前端按 URL-state 做 derived quick views；已 `cancelled` / `archived` VPS 不在主库存页展示，只通过 `/archive` 只读入口查看。
- Archive data: `/archive` 是列表页，只显式请求 `listVPSAssets({asset_scope:'archived'})` 和 `listSubscriptions({asset_scope:'archived', sort:'renew_at', order:'asc'})` 形成摘要，不自动拉单台 detail、services、domains 或 timeline。点击行 / 操作进入 `/archive/:vpsId`。`/archive/:vpsId` 先读取 `getVPSArchiveReview(vpsId)`，只有 review 返回 `cancelled` / `archived` 才继续读取 `getVPSTimeline(vpsId)` 与 `listSubscriptions({vps_id, asset_scope:'all', sort:'renew_at', order:'asc'})`；其他 lifecycle 必须 `replace` 跳回 `/vps/:vpsId`。详情页从 archive review 读取 services、domains、monitoring links 和 target links，不依赖普通 `/api/targets` 列表。详情页只读展示身份说明、上方摘要卡、用户记录、续费/价格/规格/IP 历史、订阅/服务/域名明细、底部全宽监控与 Target 历史；用户记录必须排在订阅/服务/域名明细之前。`archived` 可显示受控恢复入口，`cancelled` 不显示恢复入口；不得出现编辑、取消、添加、创建或关联按钮。多币种订阅摘要必须按币种分开显示（例如 `USD 24.00/月 + EUR 9.00/月`），不得把不同币种相加后套用单一币种。
- Asset Decisions URL-state: `view=needs_decision|renewal|region|provider|cost|evidence|single_queue`，`renew_within_days=30|60|90`，上下文筛选 `provider_id`、`vps_id`、`country`、`region`、`city`、`scenario`，打开对象 `group_id`、`manual_group_id`、`record_id`、`template_id`。非法 view 在前端降级为 `needs_decision`，后端 API 对非法 view/window/scenario 返回 400。筛选 chips 必须首屏可见并可单个移除/全部清空；打开对象只触发读取和展示，不触发创建或 PATCH。
- URL-state: VPS inventory 支持 `view=all|renewal|unreviewed|unlinked|missing_subscription|missing_facts|cancellation_attention`，并继续支持 `provider_id`、`lifecycle_status`、`usage_status`、`renewal_decision`；lifecycle filter 只提供 current 生命周期，不提供 `cancelled` / `archived` 选项。Target inventory 支持 `coverage_gap=1` 表达执行监控实例覆盖缺口，供 TargetsSupportSurface 快捷入口和 Dashboard/资产证据支撑场景承接。

#### 3. Contracts

- Asset Decisions 首屏主 surface 必须是 `资产组合决策` 的决策组列表，不得恢复三张同权 VPS queue table，也不得把单台续费队列重新提升为主视觉主体。
- Asset Decisions 顶部必须先展示 portfolio command summary：从当前已加载的记录回读、自动组、自定义组合和模板中派生第一行动，并展示组合范围、续费窗口、执行闭环风险、evidence source 状态和 context filter 摘要。该 summary 是处理顺序导览，不是 KPI 卡片墙；不得把单台队列数量提升为主指标，也不得因为某来源失败而伪造无问题。
- Asset Decisions 可以在 command summary 后展示轻量 `决策路径 / decision path` rail，把当前已加载事实派生为 `发现组合压力 -> 形成真实场景 -> 保存一次判断 -> 回读执行闭环` 四个阶段。该 rail 只读、不可持久化、不新增 API；点击只能打开已有 `group_id` / `manual_group_id` / `record_id` / `template_id` detail 或切换已有 view。任一来源加载失败时该阶段必须标记不可用，不得把失败解释成健康、已闭环或真实资料缺口。
- Asset Decisions 可以在 `决策组列表` 右侧展示 `下一步导览 / closed-loop` surface，用于把当前已加载的自动组、已保存记录 execution readback、自定义组合和场景模板收敛成 3-6 个只读工作项。导览排序优先级为事实漂移记录、阻塞记录、需补证据记录、当前自动组、进行中自定义组合、可用模板；点击只复用 `group_id`、`manual_group_id`、`record_id`、`template_id` 打开已有详情，不自动创建、不 PATCH record status、不写 VPS / Subscription / MonitoringInstance / Target。
- `下一步导览 / closed-loop` 的指标只能从当前已加载 rows 派生，例如自动组数量、进行中自定义组合、未关闭记录、readback drift/blocked/needs_evidence/open、预算压力和资料缺口。任一来源加载失败时只显示局部不可用提示并跳过该来源的工作项，不得把失败解释成无问题、已对齐或真实资料缺口。
- 已保存组合决策必须作为主工作台下方的辅助 surface 展示，承接“保存本次判断、回看当时证据、推进记录状态”的用户任务，但不得取代自动组发现入口。
- 自定义组合必须作为自动组发现和已保存记录之间的 scenario surface：自动组回答“系统发现哪些组合问题”，自定义组合回答“用户正在比较哪些真实场景”，记录回答“某一次判断和后续跟进是什么”。自定义组合可编辑 title/goal/note/scenario/status 与成员 intended role/action/reason/note/sort，不得修改 VPS / Subscription / MonitoringInstance / Target。
- 场景模板是 scenario surface 的入口层，位于自动组和自定义组合附近，视觉权重低于自动组列表。模板只启动场景或从手工组合保存 blueprint；内置模板不允许编辑，自定义模板只能改模板元数据/归档状态。模板失败只影响模板 surface 或模板 modal，不影响自动组、手工组合、记录、续费 evidence 和单台队列。
- 自动组至少覆盖 `renewal_attention`、`cancellation_attention`、`region_portfolio`、`provider_portfolio`、`cost_pressure`、`evidence_gap`。组卡展示顺序应优先服务扫描：组名/scope、主问题、当前压力、推荐下一步、证据评估、成员数量、用途分布、成本、服务 / 域名 / Target、监控关联、异常和 evidence chips；避免把五个以上指标做成同权小格，让用户先读表再理解问题。
- 组详情必须展示成员 VPS 基础事实、主订阅、服务 / 域名 / Target / 监控摘要、`suggested_role`、`suggested_action` 和 evidence chips。建议只能帮助扫描和排序，不得自动提交 keep / migrate / cancel。
- `evidence_assessment` 的视觉层级高于零散 evidence chips、低于组合事实本身：组列表展示判断尺度，组详情展示组级和成员级评估，记录详情展示保存时证据快照。UI 文案必须表达“证据质量 / 决策压力 / 准备度”，不得把 `decision_bias` 写成自动执行承诺。
- `decision_recommendation` 的视觉层级与 evidence assessment 相邻但更偏“下一步提示”：列表里保持短摘要，详情里展示下一步和理由/阻塞 chips。不得在前端根据 recommendation 自动调用 `PATCH /api/vps/*`、`PATCH /api/asset-decisions/records/*` 或其他业务对象写接口。
- `comparison_insight` 的视觉层级用于“为什么这组资产要这么取舍”：组卡展示短比较结论、lane counts 和 priority VPS；自动组详情在 `GROUP TO SCENARIO` 后、宽表前展示 `EVIDENCE MATRIX / 证据矩阵`，按成员 rank/lane 展示主力、备用、观察、退役、补证据、复核分层，以及成本/产品、承载/监控、状态/续费、证据源、strength/risk/gap chips。矩阵是扫描层，宽表仍是事实兜底；不得让矩阵按钮直接写业务对象。
- 组详情可以把当前自动组创建为自定义组合，也可以直接保存当前自动组为决策记录。保存表单允许编辑标题、组合目标、状态，以及每个成员的决定角色、决定动作和理由。保存成功后展示记录详情，而不是继续停留在只读组详情中。
- 组详情必须展示场景推进分岔：`直接保存记录` 适用于当前自动组已经就是本次判断范围，`先创建自定义组合` 适用于还需要补成员、目标或人工语境。该分岔只解释现有按钮，不新增执行路径，不写业务对象。
- 自定义组合详情必须展示当前 facts 回读后的成员对比、组合属性表单、VPS 选择器新增成员、成员意图编辑和保存为决策记录入口；同时展示只读 `组合推进状态`，从目标/标题、成员数量、成员 intended role/action、evidence gap、current fact missing 派生保存记录准备度。自定义组合的证据矩阵必须额外展示 intended role/action 与当前 comparison lane 的对照，标出“意图匹配 / 需复核意图”，但不得自动修改成员意图或业务对象。从自定义组合保存记录必须发送 `source_type=manual_group`，并使用当前成员 intended role/action/reason 作为默认决定值。
- 记录详情必须展示记录状态、来源、成员判断和证据快照，并允许推进记录状态；同时展示低权重 `SAVED EVIDENCE / 保存时依据`，从 evidence snapshot 回看保存时的 comparison insight；再展示只读 `来源与当前闭环`，说明 record 来自 auto group 还是 manual group、保存时 scope 与当前 readback/plan 状态。复核来源只能打开已有来源 detail，不能自动恢复缺失来源、创建组合或执行业务写入。成员动作里的 `cancel` / `open_cancellation_workbench` 只能渲染到 `/vps/{id}?workbench=cancellation` 的跳转入口。
- 记录详情必须展示成员级跟进状态、备注与最后更新时间；单个成员保存跟进时只 PATCH 该成员 `vps_id`、`followup_status`、`followup_note`，成功后刷新当前记录详情与已保存记录列表的跟进计数。成员跟进状态只表达“组合判断后的执行记忆”，不能隐式修改记录级状态，也不能触发 VPS、Subscription、MonitoringInstance 或 Target 写操作。
- 已保存记录列表可以低权重展示 `execution_plan` 摘要、lane 计数、actionable / blocked 计数；点击仍只打开记录详情，不直接跳业务页。
- 记录详情必须在成员明细表上方展示执行编排 board，按 `cancel_retire / migration / keep_observe / evidence / review` lane 分组展示成员、lane summary、readback badge、issue chips、当前事实块、下一步 CTA 和快速跟进按钮。board 是记录详情内的执行导览，不取代自动组主 surface，也不批量执行。
- 执行编排 CTA 的 URL 映射只能在前端本地完成：`open_cancellation_workbench -> /vps/{id}?workbench=cancellation`，`open_subscription_context -> /subscriptions?vps_id={id}`，`open_vps_detail -> /vps/{id}`，`review_record` 留在当前记录详情复核或提供普通 VPS 详情入口。
- 快速跟进按钮只能调用 `PATCH /api/asset-decisions/records/{record_id}` 更新成员 followup；不得自动 PATCH record status，也不得调用 VPS、Subscription、MonitoringInstance、Target 写接口。`completed` 记录若当前 facts drift，仍必须展示 drift/readback/plan，不能因为人工状态完成而隐藏问题。
- 单台决策编辑必须在 group detail drawer 或底部单台辅助队列中完成，仍使用 `AssetDecisionWorkPanel` 与 `PATCH /api/vps/{id}`。保存成功 notice 应留在页面可见 surface 内。取消类续费决策保存后，若 API 返回 `renewal_subscription_linkage`，页面必须展示联动结果；`no_active_subscription` 提供创建/跳转订阅入口，`multiple_active_subscriptions` 提供到订阅页筛选当前 VPS 的处理入口，不静默吞掉。
- 取消 / 退役不是 Phase 1 的组合页写动作；任何取消 / 退役执行入口必须跳到 `/vps/{id}?workbench=cancellation`，由 VPS 详情生命周期工作台加载 preview 并提交用户确认步骤。
- Asset Decisions、Dashboard 和 VPS 列表只能把 Subscription / MonitoringInstance / Target / Service / Domain 作为 VPS 的证据和缺口展示；主处理入口必须回到 `/vps/{id}`、VPS 筛选视图或组合决策组。不要把订阅或监控实例作为与 VPS 同级的“待处理主体”。
- 已取消 / 已归档 VPS 是归档资产：普通 VPS、订阅、Dashboard、Monitoring/Target 资产上下文和 Asset Decisions 都不应默认展示或统计这些资产；VPS 页和侧边栏可以提供 `/archive` 入口。归档页是 read-only readback，不复用 VPS 详情里的写操作菜单或 lifecycle workbench。
- Dashboard 资产 lane 应深链到 `/asset-decisions?view=...` 承接组合判断。VPS 页可以提供 `进入组合决策` 入口但不改变库存主路径，并应携带当前 `provider_id` 或证据缺口 scenario；VPS 详情入口应携带 `vps_id`；订阅页只展示 `需要资产判断` 链接并携带行内 `vps_id`，不在订阅页修改 VPS 决策；服务商页入口指向 `view=provider&provider_id=<id>`；Target 资产上下文入口应带 `vps_id` 和适当 scenario。Monitoring 列表不提供资产上下文入口，监控详情只保留所属 VPS 返回路径。
- 当订阅为 `expired` / `cancelled` / `paused` 而 VPS、MonitoringInstance 或 Target 仍表现为 active/running，页面必须把它归入 `cancellation_attention` 或等价的联动处理入口；入口应打开 `/vps/{id}?workbench=cancellation`，由统一工作台提交用户确认的步骤。
- 统一取消 / 退役工作台必须展示 preview 返回的 subscription、VPS、MonitoringInstance、Target 影响范围；MonitoringInstance/Target 默认只展示为待确认项，只有用户在工作台勾选并提交的 `monitoring_instance_actions` / `target_actions` 才能修改运行状态。
- Target 列表 / 详情必须消费批量 asset-context API 显示关联 VPS 的取消 / 过期 / 状态割裂上下文；Monitoring 列表不消费批量 asset-context API，Monitoring 详情通过 `/api/monitoring-instances/{id}/vps` 展示所属 VPS 并提供回到 VPS 详情 / 取消退役工作台的路径。
- `VPSAssetRecord.active_monitoring_instance_link_count` 只能展示 MonitoringInstance 关联数量或未关联状态，**不得**展示 linked monitoring instance health、最近心跳或异常，除非后端 contract 新增并同步类型/测试。
- 资料质量提示只能来自已有字段：缺订阅、`active_monitoring_instance_link_count <= 0`、缺 provider、缺 location、缺 SSH/IP access。不要从 provider 名称、region 文案或标签推断风险。
- VPS inventory quick views 中 derived filters 在前端执行即可；40+ VPS 量级不引入新缓存/状态库，不新增 API 字段。
- Dashboard 深链进入 VPS 页时，query 必须被页面首屏可见的 tab/chip/drawer 状态承接；不能静默丢弃。
- 常规业务对象关联输入不得要求用户复制内部 ID：VPS facts 的 Provider、VPS↔MonitoringInstance link 的 MonitoringInstance、VPS service/domain 的 Target、domain 的 Service 都应使用页面加载的数据选择器，并保留“未关联/不关联”选项。选择器为空或加载失败时必须给出明确说明和到对应列表/创建流程的入口；选择监控实例/Target 只创建资产引用或链接，不隐式修改 MonitoringInstance/Target/Agent/ProbeItem 语义。
- `/subscriptions?vps_id=<id>&create=1` 只作为次级账单事实入口保留；普通补录从 VPS 详情页的 `createVPSSubscription(vpsId, input)` 发起，且不要求用户选择订阅状态。
- 订阅表单和 API contract 以 `billing_period_unit` + `billing_period_length` + `renewal_mode` 为用户可见主字段；`billing_cycle`、`billing_months`、`auto_renew`、`auto_renew_cancelled` 仅作为兼容旧数据和下游月化成本计算的辅助字段。币种和支付方式继续保存字符串，但 UI 必须通过共享常用选项 + 自定义入口标准化。
- VPS 有效期延长必须走 `extendVPSValidity(vpsId, input)`，由后端更新当前 active subscription 的 `renew_at` 并写生命周期/价格历史；前端成功后刷新 detail、timeline、subscriptions 并关闭弹层。不要在浏览器里只改本地订阅日期，也不要在无 active subscription 时伪造延长成功。
- 从 VPS 补齐监控接入时，主路径是 VPS 详情内的“创建并接入监控实例”：表单按 VPS 资料预填并允许微调，成功后导航到 `/monitoring/{id}?onboarding=1&return_vps={vps_id}`。不要再增加“继承字段确认”前置弹窗；Monitoring detail 消费 onboarding 参数后必须清理 URL，生成命令后自动复制，复制失败时保留手动复制。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Asset Decisions evidence-boundary explanation grows long | 保持优先级队列为主 surface，证据边界用 `<details>` / 低权重说明承载，不抢占主视觉 |
| `/api/asset-decisions/overview` 或 groups list failed | 组合工作台显示局部错误；底部单台队列和 renewal evidence 可按各自 API 独立加载 |
| group detail missing / 404 | Drawer 显示决策组不存在或已变化，允许返回列表；不得制造空 group |
| create manual group from auto group failed | 错误留在自动组详情内；不关闭当前组、不伪造手工组合 |
| manual group list/detail failed | 自定义组合 surface 或 modal 显示局部错误；自动组、记录、单台队列继续可用 |
| manual member selector candidate list failed | 成员新增表单显示局部错误并禁用 selector；不得让用户手输内部 ID 作为普通路径 |
| manual group member patch/delete failed | 错误留在自定义组合详情内；不修改本地成员意图，不触发业务对象写接口 |
| scenario template list/detail failed | 场景模板 surface/modal 显示局部错误；其他资产决策 surfaces 继续可用 |
| create manual group from scenario template failed | 错误留在模板详情；不关闭模板 modal，不伪造自定义组合 |
| patch builtin scenario template | 前端不展示编辑按钮；后端返回错误时显示模板错误，不影响其他 surface |
| URL has `template_id` | 只读取并打开模板详情，不自动创建组合或记录 |
| save record from manual group | POST records 带 `source_type=manual_group`；成功后关闭自定义组合详情并打开记录详情 |
| create decision record 404 | 显示自动组已变化 / 不存在的保存错误，要求用户刷新组列表，不在前端补造记录 |
| saved decision records failed | 已保存组合决策 surface 显示局部错误；自动组、续费 evidence 和单台队列继续独立可用 |
| record detail missing / 404 | 记录详情 modal 显示记录不存在，不制造空记录 |
| patch record status failed | 错误留在记录详情 modal 内，不改变本地状态，也不触发 VPS/订阅状态修改 |
| patch record member follow-up failed | 错误留在记录详情 modal 内，不改变成员本地状态，不触发 VPS/订阅/监控/Target 写操作 |
| record member follow-up note cleared | 前端发送空字符串，后端保存为空备注；不得因 falsy 值省略该字段 |
| subscriptions list failed in Asset Decisions renewal window | 续费候选 evidence 显示错误，单台辅助队列仍可显示已加载 VPS |
| all subscriptions failed while building single queue | 单台辅助队列显示加载错误，避免把全量缺订阅误报为真实数据质量 |
| backend group source availability says subscriptions unavailable | 组列表 / 组详情显示证据不可用，不渲染真实 `缺订阅` chip |
| decision record snapshot lacks `evidence_assessment` | 记录详情显示“未记录”或“无证据评估”，成员表继续展示其他 snapshot 字段 |
| decision record snapshot lacks `comparison_insight` | 记录详情 `SAVED EVIDENCE` 显示“保存时未记录对比洞察”降级；不得阻断 evidence assessment、execution readback、execution plan 或成员跟进 |
| decision record API returns `execution_readback` | 已保存记录列表显示 readback status badge、summary 和 drift / blocked / needs_evidence / open 计数；记录详情 summary 增加执行回读指标，成员表显示当前事实摘要与 issue chips |
| decision record API returns `execution_plan` | 已保存记录列表显示 plan summary / lane counts / actionable count；记录详情显示 execution board 和成员下一步列；CTA 按 step kind 本地映射 URL |
| readback status is `drift` | 只显示事实漂移证据和跳转入口，不自动 PATCH record status，也不调用 VPS / Subscription / MonitoringInstance / Target 写接口 |
| readback current facts missing for member | 成员表显示“当前事实缺失”与 issue chip，不把该成员渲染成已对齐 |
| VPS inventory subscriptions empty | 行级展示 `缺订阅`，quick view `缺订阅` 可筛出对应 VPS |
| VPS inventory URL has unsupported `view` | 降级为 `all`，下次用户操作时写回合法 query |
| user removes a chip or clears all filters | URL-state 与 visible rows 同步更新，不重新请求 `/api/vps?...` derived query |
| `/archive` loads archived subscriptions in multiple currencies | 订阅历史行逐条展示原币种价格；摘要按币种分组，不跨币种求和 |
| `/archive` renders archive list | 只读展示归档 VPS 摘要，不自动请求单台 services/domains/timeline；行级入口进入 `/archive/:vpsId` |
| `/archive/:vpsId` renders detail context | 只读展示 archive review、timeline、全量订阅历史、services、domains、monitoring links 和 Target links；用户记录排在订阅/服务/域名明细之前；archived 才显示受控恢复，cancelled 不显示恢复；不得出现编辑、取消、添加、创建或关联按钮 |
| `/subscriptions?vps_id=<id>&create=1` | 显示当前 VPS context panel，创建表单打开并预填该 VPS；关闭创建表单时移除 `create=1` 但保留 `vps_id` |
| selector candidate list is empty | 表单保留空值能力，显示去对应列表/创建流程的 Link/action，不要求手输内部 ID |
| selector list request fails | 表单显示局部错误/提示，已保存主页面数据仍可查看；不得把加载失败当成真实“无候选” |

#### 5. Good/Base/Bad Cases

- Good: `/vps?view=unlinked&renewal_decision=unreviewed` 首屏显示 `视图: 未关联` 和 `续费: 未评估` chips，列表只显示同时满足条件的 rows。
- Good: `/asset-decisions?view=provider&renew_within_days=60` 首屏请求 provider 组合组，列表展示同服务商 VPS 成本、服务/域名、监控和建议动作。
- Good: `/asset-decisions?view=provider&provider_id=pv_001&template_id=adt_builtin_provider_review` 首屏展示 provider 上下文 chip，并自动打开服务商评估模板；关闭模板后仍保留 provider 筛选。
- Good: 打开决策组后，同组 VPS 可以比较主订阅、服务 / 域名 / Target / 监控数量、建议角色和 evidence chips；点击单台 `处理` 仍提交原有 VPS renewal decision PATCH。
- Good: 打开自动组后创建自定义组合，页面刷新自定义组合列表并打开新组合详情；用户能继续编辑组合目标、场景和成员意图。
- Good: 自定义组合详情通过 VPS selector 新增成员，保存成员 intended action/reason 只调用 `/api/asset-decisions/manual-groups/{id}/members`，不写 VPS / Subscription / MonitoringInstance / Target。
- Good: 从内置模板创建自定义组合，成功后 URL 切换到 `manual_group_id=<id>` 并打开组合详情；从自定义组合另存模板，成功后刷新模板列表并打开 `template_id=<id>`。
- Good: 从自定义组合保存记录时，payload 带 `source_type=manual_group` 和成员决定值，记录详情继续使用 execution readback。
- Good: 打开决策组后保存为组合决策记录，记录详情能回看成员决定角色/动作/理由和保存时证据快照，并能把状态从 `draft` 推进到 `in_progress`。
- Good: 打开已保存记录后，把单台 VPS 成员跟进从 `todo` 改为 `blocked` 并保存备注；记录详情显示更新时间，记录列表的阻塞 / 未关闭计数同步更新，业务动作入口仍只是跳到 VPS 详情或取消工作台。
- Good: 已保存记录列表低权重展示执行回读；`drift` / `blocked` / `needs_evidence` 只作为复核证据，不抢走组合工作台主 surface。
- Good: 记录详情成员表展示“当前回读”，包括 lifecycle / usage / renewal decision、active subscription、服务 / 域名 / Target / 监控计数和 issue chips；成员跟进 PATCH 成功后刷新记录详情和记录列表，readback 随 API 响应更新。
- Good: 记录详情 execution board 将取消退役成员导向 `/vps/{id}?workbench=cancellation`，将缺订阅证据导向 `/subscriptions?vps_id={id}`，同时快速跟进只 PATCH 当前 record member。
- Good: 组列表和记录详情展示 `evidence_assessment` 的 tier、bias、可信 / 压力 / 准备刻度；旧记录没有该字段时不崩溃。
- Good: 资产决策保存 `migrate` 后，VPS 从 `待评估` tab 消失并出现在 `迁移` tab，notice 留在队列 surface。
- Good: 资产决策保存 `cancel` 后，notice 继续展示 `VPS -> 取消`，并追加 API 返回的订阅联动消息 / 订阅页 action。
- Good: VPS 详情打开 MonitoringInstance link Drawer 时懒加载 `listMonitoringInstances()`，用 `选择监控实例` selector 展示名称、ID、provider、生命周期和健康状态。
- Good: VPS 详情无订阅时显示“快速创建订阅”，调用 `/api/vps/{vps_id}/subscriptions`，表单只收账单事实，不出现订阅状态。
- Good: VPS 详情无监控实例时显示“创建并接入 agent”，调用 `/api/vps/{vps_id}/monitoring-instances`，后端从 VPS 派生身份字段，成功后跳转 MonitoringInstance onboarding。
- Base: 订阅为空、Provider 为空时，页面仍能展示 VPS identity、状态、缺订阅、未关联/缺字段提示。
- Bad: 订阅 evidence 请求失败后，前端把所有 VPS 标成 `缺订阅`，导致用户做出错误取消判断。
- Bad: 在前端只保存自动组 ID 当作长期决策状态，或从记录详情批量取消 VPS / 直接修改 Subscription / MonitoringInstance / Target。
- Bad: 自定义组合新增成员让用户复制 `vps_...` 内部 ID，或者保存成员意图时顺手 PATCH `/api/vps/{id}`。
- Bad: 模板详情点击打开后自动创建手工组合，或从模板直接保存为决策记录。
- Bad: 前端用 IP/路由/性能趋势填充 recommendation 文案；这些语义在 agent 未成熟前不属于资产决策推荐。
- Bad: 前端看到 readback `drift` 后自动把记录状态改成 `in_progress` / `completed`，或自动调用 `/api/vps/*`、`/api/subscriptions/*`、`/api/monitoring-instances/*`、`/api/targets/*` 写接口。
- Bad: 前端把 `execution_plan` 当作真实执行系统，点击 CTA 后直接 PATCH VPS / Subscription / MonitoringInstance / Target，或根据 `actionable_count=0` 自动把 record status 改成 completed。
- Bad: Dashboard 或 VPSPage 从 `abnormal_linked_vps_count` 反推单台 VPS linked monitoring instance health。
- Bad: Page 直接 `fetch('/api/vps')` 或在组件层调 API；业务请求必须走 `lib/api.ts`。
- Bad: 在 VPS 详情表单里让用户输入 `mi_...`、`tg_...`、`svc_...` 作为常规路径，且不给候选列表或落地入口。
- Bad: 让用户先去 Monitoring 列表创建监控实例、重复填写名称 / 地区 / 服务商，再回 VPS 详情关联，作为普通接入路径。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: `getAssetDecisionOverview`、`listAssetDecisionGroups`、`getAssetDecisionGroup` 路径和 query string；manual group helper list/create/get/patch/member add/patch/delete；scenario template helpers list/create/get/patch/create-manual-group；记录 fixture 覆盖 `execution_readback` 和 `execution_plan`。
- `AssetDecisionsPage.test.tsx`: `资产组合决策` 主 surface、tabs query、上下文筛选 chips、深链打开 group/manual group/record/template、场景模板列表/详情/创建组合、自定义组合另存模板、组详情 drawer、创建自定义组合、自定义组合详情/成员表单/保存 manual record、组/成员/记录 `evidence_assessment` 与 `decision_recommendation` 展示、组卡 comparison summary、自动组详情 evidence matrix、自定义组合 evidence matrix、记录详情 saved evidence snapshot、旧 snapshot 缺 `comparison_insight` 降级、保存记录、记录详情状态推进、执行回读状态 / 计数 / 当前事实 / issue chips、execution plan 列表片段 / lane board / CTA URL 映射 / 快速跟进、成员跟进 PATCH payload 与计数刷新、readback drift 不触发业务对象写请求、plan CTA 不触发业务对象写请求、模板打开不触发业务对象写请求、单台 `AssetDecisionWorkPanel` PATCH payload、renewal evidence 失败不误报缺订阅、错误/空态。
- `VPSPage.test.tsx`: initial fetch、quick view、active chips、高级筛选 drawer、client-side filtering、订阅/监控实例/资料质量展示、创建 VPS 流程和 provider selector 可访问标签。
- `SubscriptionsPage.test.tsx`: `vps_id` URL context、`create=1` 自动打开/预填、关闭创建表单保留 `vps_id` 并移除 `create=1`。
- `VPSDetailPage.test.tsx`: Provider/MonitoringInstance/Target/Service selectors 的候选加载、空态/错误提示、提交 payload 仍只发送被选 ID 或空值。

#### 7. Wrong vs Correct

```tsx
// 错误：常规关联让用户复制内部 ID，且加载失败时只能猜。
<Input label="MonitoringInstance ID" value={draft.monitoringInstanceId} onChange={...} placeholder="mi_..." />
```

```tsx
// 正确：页面加载候选，选择器展示可辨识信息；无候选时给出落地入口。
<select aria-label="选择监控实例" value={draft.monitoringInstanceId} onChange={...}>
  <option value="">选择现有监控实例</option>
  {monitoringInstances.map((monitoringInstance) => (
    <option value={monitoringInstance.monitoring_instance_id}>
      {monitoringInstance.display_name} · {monitoringInstance.monitoring_instance_id}
    </option>
  ))}
</select>
<Link to="/monitoring">监控实例列表</Link>
```

```tsx
// 错误：接到 create=1 后用 effect 同步 setState 打开表单，触发 react-hooks/set-state-in-effect。
useEffect(() => {
  if (searchParams.get('create') === '1') setCreateOpen(true)
}, [searchParams])
```

```tsx
// 正确：把 URL 作为可见状态来源，必要的本地开关只处理用户交互。
const createRequested = searchParams.get('create') === '1'
const createPanelOpen = createOpen || createRequested
```

### Asset service 数据流

VPS 服务资产是 VPS 详情页内的独立手工记录区块，前端必须把它当作 `asset_services` contract 消费，而不是从 timeline、Dashboard 或 Target probe 状态推导。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/lib/types.ts` 中 `AssetService*` 类型、`web/src/lib/api.ts` 中 service API helper，或 `web/src/pages/VPSDetailPage.tsx` 的服务资产区块。

#### 2. Signatures

- Frontend type: `AssetServiceRecord` 字段保持 center JSON snake_case：`service_id`、`vps_id`、`target_id`、`name`、`service_type`、`status`、`url`、`port`、`labels`、`note`、`created_at`、`updated_at`。
- Frontend input: `CreateAssetServiceInput` 允许 collection create 带 `vps_id`，也允许 VPS scoped create 不带 `vps_id`。
- Frontend API: `listAssetServices(filter)`, `createAssetService(input)`, `listVPSServices(vpsId)`, `createVPSService(vpsId, input)`。
- Page data: `VPSDetailPage` 初始加载 `getVPSAsset(vpsId)`、`getVPSTimeline(vpsId)`、`listVPSServices(vpsId)`、`listVPSDomains(vpsId)` 和 `listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc' })`；创建服务后只刷新 `listVPSServices(vpsId)`。

#### 3. Contracts

- 机器值标签集中在 `ASSET_SERVICE_TYPE_LABELS` 与 `ASSET_SERVICE_STATUS_LABELS`，组件内不得散落中文枚举文案。
- `createVPSService(vpsId, input)` 必须去掉 `input.vps_id`，保证 path 是唯一 VPS 来源。
- VPS 服务区块展示 name、type、status、url/port、optional Target link、labels、note；Target link 只跳转，不触发 Target 创建或修改。
- 服务创建表单负责本地校验 blank name 和 port `1..65535`，但最终校验仍以后端为准。
- 服务资产不是 timeline item；创建服务不得刷新或插入 `VPSTimeline.experience_logs`、续费历史、价格历史、IP 历史或规格快照。
- 服务创建表单应在 Drawer 或同等次级 surface 中打开。主扫描路径展示服务表格和保存后的 notice，不常驻创建表单。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank service name in form | 页面本地显示 `服务名称不能为空。`，不发 POST |
| invalid port in form | 页面本地显示 `服务端口必须为 1 到 65535。`，不发 POST |
| API returns `vps asset not found` | `ApiError.message` 原样展示在当前服务表单错误区 |
| API returns `target not found` | `ApiError.message` 原样展示在当前服务表单错误区 |
| services list is empty | `DataTable` emptyContent 显示 `尚未记录服务` |

#### 5. Good/Base/Bad Cases

- Good: 创建服务成功后显示 `服务记录已创建`，表格出现新服务，并且只追加一次 `/api/vps/{id}/services` refresh。
- Base: 初始 services 为空时，页面仍正常展示 VPS facts、MonitoringInstance links、timeline 和连接摘要。
- Bad: 页面直接 `fetch('/api/vps/.../services')`，绕过 `api.ts` 和 `ApiError`。
- Bad: 把服务数量写进 Dashboard asset summary，或从 Target 列表自动反推 services。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: collection list/create、VPS scoped list/create，并断言 scoped create body 不含 `vps_id`。
- `web/src/pages/VPSDetailPage.test.tsx`: 初始 services 请求、空态、服务创建 happy path、本地校验失败。
- 改 `AssetService*` 类型或标签时同步测试 fixture 和页面断言。

#### 7. Wrong vs Correct

```tsx
// 错误：业务 page 直接 fetch，错误处理和认证钩子会漂移。
await fetch(`/api/vps/${vpsId}/services`)

// 正确：统一通过 API client。
await listVPSServices(vpsId)
```

```tsx
// 错误：VPS scoped create 把 body 里的 vps_id 带过去。
createVPSService(vpsId, { ...form, vps_id: otherVpsId })

// 正确：helper 丢弃 vps_id，path 是唯一 VPS 来源。
const { vps_id: _ignored, ...body } = input
postJSONBody(`/api/vps/${vpsId}/services`, body)
```

### Asset domain 数据流

VPS 域名资产是 VPS 详情页内的独立手工记录区块，前端必须把它当作 `asset_domains` contract 消费，而不是从 services、timeline、Dashboard、Target probe 或 DNS provider 自动推导。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/lib/types.ts` 中 `AssetDomain*` 类型、`web/src/lib/api.ts` 中 domain API helper，或 `web/src/pages/VPSDetailPage.tsx` 的域名资产区块。

#### 2. Signatures

- Frontend type: `AssetDomainRecord` 字段保持 center JSON snake_case：`domain_id`、`vps_id`、`service_id`、`target_id`、`domain_name`、`purpose`、`status`、`registrar`、`expires_at`、`auto_renew`、`https_enabled`、`labels`、`note`、`created_at`、`updated_at`。
- Frontend input: `CreateAssetDomainInput` 允许 collection create 带 `vps_id`，也允许 VPS scoped create 不带 `vps_id`。
- Frontend API: `listAssetDomains(filter)`, `createAssetDomain(input)`, `listVPSDomains(vpsId)`, `createVPSDomain(vpsId, input)`。
- Page data: `VPSDetailPage` 初始加载 `getVPSAsset(vpsId)`、`getVPSTimeline(vpsId)`、`listVPSServices(vpsId)`、`listVPSDomains(vpsId)` 和 VPS scoped `listSubscriptions`；创建域名后只刷新 `listVPSDomains(vpsId)`；刷新 detail + timeline 的动作必须保留 services/domains 两个独立列表，并重新读取 subscription evidence。

#### 3. Contracts

- 机器值标签集中在 `ASSET_DOMAIN_STATUS_LABELS`，组件内不得散落中文枚举文案。
- `createVPSDomain(vpsId, input)` 必须去掉 `input.vps_id`，保证 path 是唯一 VPS 来源。
- VPS 域名区块展示 domain name、status、HTTPS、purpose、registrar、expires_at、auto_renew、optional Service / Target link、labels、note；Target link 只跳转，不触发 Target 创建或修改。
- 域名创建表单负责本地校验 blank domain、URL/path/space 和裸主机名，但最终校验仍以后端为准。
- 域名资产不是 timeline item；创建域名不得刷新或插入 `VPSTimeline.experience_logs`、续费历史、价格历史、IP 历史或规格快照。
- 域名创建表单应在 Drawer 或同等次级 surface 中打开。主扫描路径展示域名表格和保存后的 notice，不常驻创建表单。

### VPS detail 判断工作台数据流

VPS 详情页可以把 VPS detail、timeline、VPS scoped subscriptions、VPS scoped services/domains 组合成单台资产判断 workbench。它只使用现有 contract，不能发明不存在的资产风险或 provider facts。

#### Contracts

- `VPSDetailPage` 初始加载必须包括 `getVPSAsset(vpsId)`、`getVPSTimeline(vpsId)`、`listVPSServices(vpsId)`、`listVPSDomains(vpsId)` 和 `listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc' })`。Provider/MonitoringInstance/Target selector 数据可在对应 Drawer 打开时懒加载，避免主详情首屏为选择器阻塞。
- VPS scoped subscription 只作为续费/成本 evidence。订阅请求失败时显示请求错误和未知状态，不得把 failure 当成真实 `缺订阅`。
- VPS Detail 是普通补录的主入口：`createVPSSubscription(vpsId, input)` 只提交账单事实且不含 status；`createVPSMonitoringInstance(vpsId, input?)` 默认空 body，让后端从 VPS 派生 MonitoringInstance 身份并自动 link。
- VPS 详情页必须可加载 `getVPSCancellationPreview(vpsId)` 并在资产判断 workbench 显示取消 / 过期影响范围；URL `?workbench=cancellation` 应直接打开统一取消 / 退役工作台。
- 如果 preview 显示 subscription 已非活跃但 VPS 仍未取消，页面不得引导“创建订阅”作为主路径，而应引导用户处理 VPS、MonitoringInstance 与 Target/实例的 lifecycle action。
- `VPSAssetDetail.monitoring_instance_links` 可以在 Detail 页展示 health、heartbeat、active incident count 和 issue summary，因为后端 detail contract 已返回这些字段；这不改变 `VPSAssetRecord.active_monitoring_instance_link_count` 在列表页只能代表数量的限制。
- 决策、facts、MonitoringInstance link、experience log、service create、domain create、取消 / 退役 action 的复杂输入使用 Drawer。关闭 Drawer 后，保存成功 notice 必须留在主页面可见 surface 内。
- Facts Drawer 只编辑基础事实和用途状态；不得包含 lifecycle status，也不得在 facts PATCH payload 中发送 `lifecycle_status`。
- Archive/restore 仍是 lifecycle 危险操作，使用独立 confirmation，不放入 routine edit Drawer。

#### Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| scoped subscriptions request fails | Workbench 显示订阅读取失败和错误提示，不显示真实 `缺订阅` 质量缺口 |
| scoped subscriptions empty | Workbench 显示 `缺订阅`，资料质量 badge 标记缺订阅 |
| decision/facts/experience save succeeds | Drawer 关闭，主页面出现成功 notice，detail/timeline/services/domains/subscriptions 刷新 |
| service/domain create succeeds | Drawer 关闭，只刷新对应 service/domain list，主页面表格出现新记录 |
| local service/domain validation fails | Drawer 内显示本地错误，不发 POST；主页面不重复渲染同一错误 |

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank domain in form | 页面本地显示 `域名不能为空。`，不发 POST |
| URL/path/space/bare host in form | 页面本地显示 `域名必须是不带协议、路径和空格的完整域名。`，不发 POST |
| API returns `vps asset not found` | `ApiError.message` 原样展示在当前域名表单错误区 |
| API returns `asset service not found` | `ApiError.message` 原样展示在当前域名表单错误区 |
| API returns `target not found` | `ApiError.message` 原样展示在当前域名表单错误区 |
| API returns `asset domain conflict` | `ApiError.message` 原样展示在当前域名表单错误区 |
| domains list is empty | `DataTable` emptyContent 显示 `尚未记录域名` |

#### 5. Good/Base/Bad Cases

- Good: 创建域名成功后显示 `域名记录已创建`，表格出现新域名，并且只追加一次 `/api/vps/{id}/domains` refresh。
- Base: 初始 domains 为空时，页面仍正常展示 VPS facts、MonitoringInstance links、services、timeline 和连接摘要。
- Bad: 页面直接 `fetch('/api/vps/.../domains')`，绕过 `api.ts` 和 `ApiError`。
- Bad: 把域名数量写进 Dashboard asset summary，或从 Target / service 列表自动反推 domains。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: collection list/create、VPS scoped list/create，并断言 scoped create body 不含 `vps_id`。
- `web/src/pages/VPSDetailPage.test.tsx`: 初始 domains 请求、空态、域名创建 happy path、本地校验失败。
- 改 `AssetDomain*` 类型或标签时同步测试 fixture 和页面断言。

#### 7. Wrong vs Correct

```tsx
// 错误：业务 page 直接 fetch，错误处理和认证钩子会漂移。
await fetch(`/api/vps/${vpsId}/domains`)

// 正确：统一通过 API client。
await listVPSDomains(vpsId)
```

```tsx
// 错误：VPS scoped create 把 body 里的 vps_id 带过去。
createVPSDomain(vpsId, { ...form, vps_id: otherVpsId })

// 正确：helper 丢弃 vps_id，path 是唯一 VPS 来源。
const { vps_id: _ignored, ...body } = input
postJSONBody(`/api/vps/${vpsId}/domains`, body)
```

### Subscription Cost Workbench 数据流

订阅成本工作台是 VPS-first 成本中枢的主操作入口。它可以聚合订阅、预算、汇率、续费提醒和 VPS 证据，但不能替代 VPS 业务状态机。

#### 1. Scope / Trigger

- Trigger: 修改 `SubscriptionsPage.tsx`、`VPSDetailPage.tsx` 成本卡、`AssetDecisionsPage.tsx` 成本信号、`DashboardPage.tsx` 订阅摘要、`web/src/lib/api.ts` 订阅成本 API，或 `web/src/lib/types.ts` 订阅成本类型。

#### 2. Signatures

- Frontend APIs: `getSubscriptionOverview()`、`getSubscriptionStatistics(window)`、`getSubscriptionSettings()`、`updateSubscriptionSettings(input)`、`refreshSubscriptionExchangeRates()`、`listSubscriptionBudgets(filter?)`、`createSubscriptionBudget(input)`、`patchSubscriptionBudget(input)`、`listSubscriptions(filter?)`。
- Frontend types mirror center JSON snake_case: `monthly_price_base`、`yearly_price_base`、`base_currency`、`exchange_rate`、`exchange_rate_date`、`exchange_rate_stale`、`budget_status`、`next_reminder_at`。
- Routes: `/subscriptions` 是完整工作台；`/vps/:id` 只展示单台成本卡；`/asset-decisions` 只展示成本信号；Dashboard 只展示高信号摘要。

#### 3. Contracts

- `/subscriptions` 初始加载 overview、statistics、budgets、settings、subscriptions、VPS 列表。局部请求失败必须显示局部错误，不得把失败当成真实空态。
- 订阅列表的 derived cost 字段只读展示。创建/编辑订阅仍只提交账单事实，不提交 `monthly_price_base`、`budget_status`、`exchange_rate_stale` 或 `next_reminder_at`。
- Settings 表单不显示 Fixer key 明文；空 key 输入在 UI 中默认表示不修改，显式清除需要单独确认或后续专用动作。
- 预算 UI 可先支持创建和总览；如果新增编辑/禁用交互，必须使用 PATCH，并保持 omitted 和 `null` limit 语义。
- VPS 成本卡只展示当前订阅证据：原币种价格、base 月/年成本、续费日、预算状态、提醒状态、汇率状态。订阅读取失败时显示未知/错误，不得标成真实缺订阅。
- Asset Decisions 成本信号只能作为 VPS 决策证据：临近续费、超预算、缺订阅、取消/迁移但仍可能续费。主操作仍回到 VPS 详情或 VPS 决策 drawer。
- Dashboard 只显示总成本、未来续费、预算风险、汇率异常等高信号入口；不得加入预算编辑、汇率设置、订阅明细表或完整图表。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| overview failed, subscriptions loaded | 工作台显示 overview 局部错误，明细仍可操作 |
| settings failed | 汇率与提醒设置区显示错误，订阅明细不被清空 |
| exchange refresh failed | 显示失败结果或错误，不泄露 provider secret |
| budget list empty | 显示可创建预算的空态，不标记所有订阅超预算 |
| subscription has stale exchange | 行内和摘要显示汇率异常，成本字段可为 stale/null |
| VPS scoped subscriptions failed | VPS 成本卡显示读取失败，不显示真实缺订阅 |
| Dashboard subscription summary failed | Dashboard 降级为摘要不可用，完整操作仍在 `/subscriptions` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 `/subscriptions` 查看 CNY 总月成本、续费队列、供应商拆分、预算风险，并刷新汇率。
- Good: VPS 详情成本卡显示 USD 原价、CNY 月/年成本、下一次续费和预算状态，并链接回订阅工作台。
- Good: Asset Decisions 在取消/迁移队列里显示“仍有临近续费风险”，但保存决策仍调用 VPS 决策 API。
- Base: CNY 订阅显示汇率 `1` 且不提示 stale。
- Bad: 前端把 `budget_status='over'` 自动改写 VPS `renewal_decision='cancel'`。
- Bad: Dashboard 放入完整预算 CRUD 或 Fixer key 配置表单。
- Bad: 页面直接 `fetch('/api/subscriptions/overview')`；必须走 `web/src/lib/api.ts`。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: 新订阅成本 API 路径、方法、body 和 query。
- `SubscriptionsPage.test.tsx`: 工作台主加载、空态、多币种、预算风险、汇率异常、设置保存、刷新汇率、创建预算。
- `VPSDetailPage.test.tsx`: scoped 订阅成本卡、缺订阅、读取失败。
- `AssetDecisionsPage.test.tsx`: 临近续费、超预算、缺订阅、取消/迁移续费风险信号。
- `DashboardPage.test.tsx`: 高信号订阅摘要存在，且不展开完整订阅工作台。

#### 7. Wrong vs Correct

```tsx
// 错误：把成本风险直接升级成 VPS 业务决策。
if (subscription.budget_status === 'over') {
  await updateVPSAsset(subscription.vps_id, { renewal_decision: 'cancel' })
}
```

```tsx
// 正确：只展示成本信号，让用户在 VPS 决策入口确认。
<Link to={`/vps/${subscription.vps_id}`}>查看成本风险</Link>
```

```tsx
// 错误：组件内绕过统一 API client。
await fetch('/api/subscriptions/overview')
```

```tsx
// 正确：通过 `lib/api.ts` 保持 credentials、错误处理和类型统一。
const overview = await getSubscriptionOverview()
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
- Asset summary field: `DashboardOverview.asset_summary`，类型为 `DashboardAssetSummary`。

#### 3. Contracts

- `limit` 只限制 `abnormal_monitoring_instances`、`abnormal_targets` 和 `recent_events`；不得限制全局计数、`group_summaries` 或 `notification_status`。
- `snapshot_generated_at` 是 Center 生成 overview 的时间，只能被展示为 dashboard 生成时间。
- `group_summaries` 必须由后端基于全量 `monitoring_instances` + `targets` 计算，空白 group 归一为 `未分组`，前端不得从异常队列 reduce。
- `notification_status` 只能包含配置布尔摘要，不包含 Telegram token/chat id 或 Feishu webhook URL。
- `asset_summary` 只能包含聚合摘要：`renewal_due_30d_subscription_count`、`renewal_due_30d_vps_count`、`unreviewed_vps_count`、`to_cancel_vps_count`、`to_migrate_vps_count`、`unlinked_vps_count`、`abnormal_linked_vps_count`、`cost_by_currency[]`。`cost_by_currency[]` 只包含 `currency`、`monthly_total`、`yearly_total`。
- 库存完整度计数必须来自后端 contract：待接入监控实例、暂停监控实例、退役监控实例、暂停目标、归档目标。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `limit` 缺失 | handler 使用默认 limit |
| `limit <= 0` 或非数字 | handler 返回 400 |
| dashboard store 查询失败 | handler 返回 500，store error 用 `%w` 包装上下文 |
| `center_settings` singleton 缺失 | `notification_status` 全 false，不返回错误 |
| `group_summaries` 为空 | Dashboard 显示空态，不制造 `未分组 0` |
| Asset Ledger 表为空 | `asset_summary` 返回 0 计数与空 `cost_by_currency`，Dashboard 显示低权重空态 |

#### 5. Good/Base/Bad Cases

- Good: group 只存在于 targets 时仍出现在 `group_summaries`，监控实例计数为 0、目标计数为真实值。
- Base: 无监控实例无目标时 dashboard 仍返回 200，计数为 0，首次接入工作台显示，Group 区为空态。
- Good: 资产摘要显示为工作台内低权重入口，链接到 VPS / 订阅 / 监控筛选页，不新增资产明细表。
- Bad: 从 `abnormal_monitoring_instances` 推导 `按 Group 分布`；把 `snapshot_generated_at` 写成同步完成；把通知 token 暴露给 Dashboard。
- Bad: 把 `asset_summary` 扩展成 `vps_assets` / `subscriptions` 明细数组，或在 Dashboard 首屏展示所有资产字段。

#### 6. Tests Required

- Go store test: 新计数字段、全量 group SQL、settings 缺失时通知 false、`limit` 不影响 group summary。
- Go handler test: 新字段 JSON snake_case，且不泄露敏感通知字段。
- Frontend type/API fixture: `DashboardOverview` fixture 覆盖新增字段；新增 `asset_summary` 时必须同步 AppShell、DashboardPage、api test fixtures。
- DashboardPage test: 生成时间、PR4 深链、异常处理队列、运行上下文、库存完整度 / 通知配置在内联关键指标和紧凑入口中的呈现，资产摘要低权重入口，以及异常态 / 首次接入态不展开独立 summary/KPI strip、Group、最近事件、API facts、资产明细 dump。
- AppShell test: 共享 dashboard fixture 与新增 contract 保持兼容。

#### 7. Wrong vs Correct

```tsx
// 错误：PR4 前从 Dashboard 拼筛选深链，并且筛选语义不由 API contract 支撑
<Link to={`/monitoring?monitoring_status=${overview.paused_monitoring_instance_count > 0 ? '暂停' : ''}`}>暂停监控实例</Link>

// 正确：PR3 只展示 contract 支撑的状态摘要，入口仍去列表页
<Link to="/monitoring">暂停 <MonoDigits>{overview.paused_monitoring_instance_count}</MonoDigits></Link>

// 错误：要求 settings secret 出现在 dashboard contract
overview.notification_status.feishu_webhook_url

// 错误：资产摘要变成 Dashboard 数据仓库
overview.asset_summary.subscriptions.map((item) => item.renew_at)

// 正确：只展示配置布尔摘要，并把编辑动作交给 SettingsPage
<Link to="/settings">{notificationSummary(overview)}</Link>

// 正确：资产摘要只做少量决策入口，明细交给资产页面
<Link to="/subscriptions?renew_within_days=30">
  30 天续费 <MonoDigits>{overview.asset_summary.renewal_due_30d_vps_count}</MonoDigits>
</Link>
```

---

## Page 内数据流：loading / error / empty 三态

### Scenario: Events include backfilled filter

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/pages/EventsPage.tsx` 的筛选状态、`web/src/lib/types.ts` 的 `EventListFilter`、或 `web/src/lib/api.ts` 的 `listEvents` query 序列化。

#### 2. Signatures

- URL-state: `/events?include_backfilled=1`。
- Frontend filter type: `EventListFilter.include_backfilled?: boolean`。
- API client: `listEvents({ include_backfilled: true })` -> `/api/events?...&include_backfilled=true`，读取 `{"items":[]}` 并向 page 返回 `StateChangeEventRecord[]`。
- Backend contract: `/api/events?include_backfilled=true` 解除默认 backfilled event exclusion，成功响应为 `{"items":[...]}`。

#### 3. Contracts

- `include_backfilled` 是显式 opt-in；默认 `false` 时不写 URL，也不写 API query。
- URL 只接受 `include_backfilled=1` 为 active；`yes`、`true`、`0` 等 URL 值在 EventsPage canonicalize 时被移除。
- API query 用 `include_backfilled=true`，复用 `withQuery` 的 boolean 序列化和 false omission。
- `listEvents(...)` 是唯一解包点：page / component 继续消费数组，不在 UI 层重复判断 `{items}` envelope。
- EventsPage 必须把该维度纳入 parse -> normalize -> URL serialize -> `buildFilterQuery` -> chip -> reset/remove 全链路。
- “包含补传事件” toggle 不得禁用或显示“待后端支持”；如果后端 contract 未来撤销，必须同时更新 handler/store/spec/tests，而不是只改 UI 文案。
- EventsPage 的高级筛选采用 applied/draft 分离：URL 与 `appliedFilters` 是请求真相；Drawer 打开时 draft 必须从当前 applied filters 初始化；只有点击 `应用筛选` 或 `重置筛选` 才能改 URL 和触发 `/api/events` 请求。Esc、overlay、头部关闭与 Drawer 内 `关闭` 必须丢弃 draft，不能提交筛选或发请求。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| 首次进入 `/events?include_backfilled=1` | 初始请求包含 `include_backfilled=true`，页面显示“包含补传事件” chip |
| 点击 toggle 后应用 | URL 写 `include_backfilled=1`，API 请求写 `include_backfilled=true` |
| 移除 chip | URL 和 API query 都清除 backfill 维度 |
| 重置筛选 | 回到 `/events`，请求 `/api/events?limit=50` |
| URL `include_backfilled=yes` | canonicalize 掉，不请求 backfill 维度 |
| Drawer 内修改草稿后关闭 / Esc | URL 不变，不发新请求；下次打开恢复当前 applied filters |

#### 5. Good/Base/Bad Cases

- Good: 用户从 Dashboard 或手写 URL 进入 `/events?include_backfilled=1`，页面首个请求已经包含该维度。
- Base: 默认 `/events` 不显示 backfill chip，仍请求 `/api/events?limit=50`。
- Bad: toggle 改变本地 UI 但 `listEvents` 未带 query，导致 inert filter。
- Bad: 只在 URL 写 `include_backfilled=1`，但 active chip/remove/reset 没有纳入同一状态机。
- Bad: Drawer 草稿通过 `onChange` 直接写 URL 或请求 API，导致关闭抽屉也会提交用户尚未确认的筛选。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: `listEvents({ include_backfilled: true })` 序列化为 `include_backfilled=true`，false 被省略，并从 `{"items":[...]}` envelope 解包为数组。
- `web/src/pages/EventsPage.test.tsx`: 初始 URL、Drawer apply、Drawer close / Esc discard、toggle apply、chip remove、reset、invalid URL canonicalization 全部覆盖。

#### 7. Wrong vs Correct

```tsx
// 错误：UI 状态有字段，但 normalize 永远清成 false。
include_backfilled: false

// 正确：parse / normalize / build query 都保留用户选择。
include_backfilled: filters.include_backfilled
```

```tsx
// 错误：后端已支持后仍禁用控件。
<FilterToggle label="包含补传事件" checked={false} disabled onChange={() => {}} />

// 正确：toggle 进入同一套筛选状态机。
<FilterToggle
  label="包含补传事件"
  checked={filters.include_backfilled}
  onChange={(checked) => updateDraftFilter('include_backfilled', checked)}
/>
```

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

---

## 跨组件 / 跨页状态

仅有两条 Context：

| Context | 文件 | 提供值 | 消费方式 |
|---------|------|--------|----------|
| Auth | `web/src/lib/auth-context.tsx` | `{ user, loading, login, logout, refresh }` | `useAuth()`，必须在 `<AuthProvider>` 内调用，否则抛错 |
| Theme | `web/src/lib/theme-context.tsx` | `{ preset, mode, setPreset, setMode }` | `useTheme()`（必须在 Provider 内）/ `useThemeOptional()`（测试便利） |

两者都在 `web/src/main.tsx:15-23` 一次性挂在根：`ThemeProvider` → `AuthProvider` → `RouterProvider`。

**新增第三个 Context 的判断标准**：

1. 数据真的需要被树形多个子树消费（如多 page、多 layout 子树）。
2. 不是纯服务端数据缓存（那种应在引入 React Query 之类时再统一）。
3. 写入路径有限且语义清晰（如全局开关、当前组织 ID）。

满足后落到 `web/src/lib/<name>-context.tsx`，导出 `<Name>Provider` + `use<Name>`，并在 `main.tsx` Provider 链显式挂载（不要在某个 page 内偷偷挂）。

---

## 数据拉取时机（实读约束）

- 当前所有数据拉取都是 **mount 时拉一次** + **用户操作触发重拉**。`useEffect` 触发条件主要是 page 入参（`useParams` 拿到的 `monitoringInstanceId` / `targetId`）或筛选条件（如 `EventsPage` 的 `appliedFilters`）。
- **没有 SSE / WebSocket / 轮询**。center 也不主动推；交互式重刷由用户点击 / 提交触发。
- **没有跨 page 的请求缓存**：从 `/monitoring` 进 `/monitoring/:id` 会再发一次 `getMonitoringInstance`。当前体量可以接受；如果未来要去抖 / 缓存，再考虑 React Query。

---

## 反模式

> 这些是当前代码已经回避（或承认偿还）的写法，**新代码不要做**。

- ❌ **page / component 里直接 `fetch()`**：业务请求必须走 `web/src/lib/api.ts`，认证请求走 `web/src/lib/auth-client.ts`。
- ❌ **手抄后端字段名 / 自己拼 URL 格式化**：从 `lib/types.ts` import 类型 + 用 `lib/api.ts` 里的 `withQuery` 模式构造查询参数。
- ❌ **驼峰化后端字段**：保持 snake_case（`monitoring_instance_id`、`current_health_status`），便于和 center 端 grep 对齐。
- ❌ **跨 page 共享 mutable 全局变量**：不要用模块级 `let` / Map 缓存业务数据或 plaintext token；监控实例安装命令只从 center 生成并在当前 onboarding page 状态中展示。
- ❌ **绕过 ApiError 直接 throw 字符串**：`lib/api.ts` 已统一错 `ApiError(status, message)`，page 用 `instanceof ApiError` 判别后挑 `.message` 展示。
- ❌ **在 `useEffect` 里写 state 不带 `cancelled` 旗标**：StrictMode 下 effect 会触发两次，不防护会出现 setState on unmounted。
- ❌ **把派生数据存 `useState`**：能现算就不要二次同步。
- ❌ **预先引入 React Query / Zustand / Redux**：当前没有任何 page 真的需要它们；引入新依赖需独立技术决策。
- ❌ **在 component / page 写 inline `Intl.*` / `toFixed`**：交给 `lib/format.ts`。

---

## 与 CLAUDE.md 的差异 / 已知 gap

> 用于后续任务评审；若形成可复用规则，更新 `.trellis/spec/` 或当前 active docs。

1. **认证请求与业务请求现在共享 `api.ts` 的 request helpers 和 401 hook**；新代码不要再加第二套 fetch 包装。
2. **类型与 Go contract 全靠手维护**——没有 codegen。前后端字段如有漂移，依赖测试 + 运行期 `unknown` 解析报错暴露。
3. **当前没有任何状态库 / 数据缓存层**：CLAUDE.md 也没要求引入。本 spec 把"暂不引入"作为现行约束写明。

---

## Examples

仓库内"数据获取写得好"的真实参考点：

- **标准 page 数据流（loading / error / data 三态 + cancelled 旗标）**：`web/src/pages/EventsPage.tsx:63-108`，配合 `web/src/lib/api.ts:304-325` 的 `listEvents(filter)`。
- **乐观锁更新**：`web/src/pages/MonitoringPage.tsx:283-325` 的 `handleSaveLabels` → `updateMonitoringInstanceMetadata(monitoringInstanceId, input, { expectedUpdatedAt })`，其内部由 `web/src/lib/api.ts:145-153` 走 `If-Match` 头实现。
- **Provider + Hook 配对**：`web/src/lib/auth-context.tsx`（`AuthProvider` 内 `useEffect` 挂 401 钩子 → 用 `useAuth()` 暴露 `{ user, loading, login, logout, refresh }`）。
- **类型驱动的 API 函数集**：`web/src/lib/api.ts:1-21` 顶部从 `./types` 一次性 import 所有领域类型，下方所有 `requestJSON<T>` 都直接复用。

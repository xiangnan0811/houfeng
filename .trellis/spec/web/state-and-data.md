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
- `asset_summary` 只能展示 VPS Asset Ledger 的少量决策入口：30 天续费、待决策、待取消/迁移、未关联监控实例、关联异常 VPS、按币种月付成本。它应集中出现在 Dashboard 首屏 `资产决策队列` lane 中，避免在下方工作台重复出现同权资产卡片。它不能展开资产明细，不能替代 VPS / 订阅页面，也不能把 Dashboard 变成资产字段总表。第一版没有未关联 VPS 专用筛选时，可以链接 `/vps` 作为人工核对入口。
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
<DashboardCommandSurface overview={overview} lanes={['资产决策队列', '观测异常队列', '下一步动作']} />
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
- MonitoringSupportSurface 只作为资产判断支撑，不作为第二个主工作台；文案要压缩，support lane 数量保持克制，资产侧问题应导向 VPS 库存或资产决策队列。

#### Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Drawer 内修改筛选后取消 / Esc / overlay 关闭 | 列表不变；重新打开 Drawer 恢复当前 applied filters |
| 点击完成 / 应用筛选 | Drawer 关闭，列表和 visible chips 使用新筛选 |
| runtime attention quick view | 同时显示维护中与暂停监控实例；空态不得误报为全局无监控实例 |
| 无选择且未打开批量操作 | 批量 bar 不渲染 |
| 打开批量操作但未全选 | 显示范围/选择入口，不显示实际批量动作按钮 |

### Asset Ledger 列表与决策队列数据流

Asset Ledger 的列表页可以把现有 VPS 与 Subscription contract 在前端做轻量 join，用于人工核对和资料质量提示；这不是后端字段扩展，也不能创造未存在的健康语义。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/pages/AssetDecisionsPage.tsx`、`web/src/pages/VPSPage.tsx`、`web/src/pages/assetPageUtils.ts`、`AssetDecisionWorkPanel`、或改变 VPS/Subscription 列表页的筛选 URL-state。

#### 2. Signatures

- Frontend API: `listVPSAssets(filter?)`, `listSubscriptions(filter?)`, `listProviders()`, `updateVPSAsset(vpsId, input)`。`updateVPSAsset` 仍返回 VPS record 字段，并可在取消类续费决策响应中附带 `renewal_subscription_linkage` 状态摘要。取消 / 退役协同使用 `getVPSCancellationPreview(vpsId)`、`applyVPSCancellation(vpsId, input)`、`listMonitoringInstanceAssetContexts()`、`listTargetAssetContexts()`，不得在页面里直接 `fetch()`。
- Decision queue data: `AssetDecisionsPage` 拉取续费窗口 subscriptions、全量 subscriptions（按 `renew_at asc`）、以及 `renewal_decision=unreviewed|migrate|cancel` 三个 VPS 切片。
- VPS inventory data: `VPSPage` 拉取全量 `listVPSAssets()`、`listProviders()` 和 `listSubscriptions({ sort: 'renew_at', order: 'asc' })`，在前端按 URL-state 做 derived quick views。
- URL-state: VPS inventory 支持 `view=all|renewal|unreviewed|unlinked|missing_subscription|missing_facts|archived|cancellation_attention`，并继续支持 `provider_id`、`lifecycle_status`、`usage_status`、`renewal_decision`。

#### 3. Contracts

- Asset Decisions 首屏主 surface 必须是一个统一工作队列；不得恢复三张同权 VPS queue table。
- 决策编辑必须在 drawer 或同等次级 surface 中完成；保存成功 notice 应在队列 surface 可见。取消类续费决策保存后，若 API 返回 `renewal_subscription_linkage`，页面必须展示联动结果；`no_active_subscription` 提供创建/跳转订阅入口，`multiple_active_subscriptions` 提供到订阅页筛选当前 VPS 的处理入口，不静默吞掉。
- 当订阅为 `expired` / `cancelled` / `paused` 而 VPS、MonitoringInstance 或 Target 仍表现为 active/running，页面必须把它归入 `cancellation_attention` 或等价的联动处理入口；入口应打开 `/vps/{id}?workbench=cancellation`，由统一工作台提交用户确认的步骤。
- 统一取消 / 退役工作台必须展示 preview 返回的 subscription、VPS、MonitoringInstance、Target 影响范围；MonitoringInstance/Target 默认只展示为待确认项，只有用户在工作台勾选并提交的 `monitoring_instance_actions` / `target_actions` 才能修改运行状态。
- MonitoringInstance 列表 / 详情、Target 列表 / 详情必须消费批量 asset-context API 显示关联 VPS 的取消 / 过期 / 状态割裂上下文；不得只显示自身运行状态而隐藏宿主 VPS 已取消或待取消的事实。
- `VPSAssetRecord.active_monitoring_instance_link_count` 只能展示 MonitoringInstance 关联数量或未关联状态，**不得**展示 linked monitoring instance health、最近心跳或异常，除非后端 contract 新增并同步类型/测试。
- 资料质量提示只能来自已有字段：缺订阅、`active_monitoring_instance_link_count <= 0`、缺 provider、缺 location、缺 SSH/IP access。不要从 provider 名称、region 文案或标签推断风险。
- VPS inventory quick views 中 derived filters 在前端执行即可；40+ VPS 量级不引入新缓存/状态库，不新增 API 字段。
- Dashboard 深链进入 VPS 页时，query 必须被页面首屏可见的 tab/chip/drawer 状态承接；不能静默丢弃。
- 常规业务对象关联输入不得要求用户复制内部 ID：VPS facts 的 Provider、VPS↔MonitoringInstance link 的 MonitoringInstance、VPS service/domain 的 Target、domain 的 Service 都应使用页面加载的数据选择器，并保留“未关联/不关联”选项。选择器为空或加载失败时必须给出明确说明和到对应列表/创建流程的入口；选择监控实例/Target 只创建资产引用或链接，不隐式修改 MonitoringInstance/Target/Agent/ProbeItem 语义。
- `/subscriptions?vps_id=<id>&create=1` 是可落地上下文：订阅页必须显示当前 VPS 筛选/上下文，创建表单预填该 VPS，用户仍可切换或清除筛选。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Asset Decisions evidence-boundary explanation grows long | 保持优先级队列为主 surface，证据边界用 `<details>` / 低权重说明承载，不抢占主视觉 |
| subscriptions list failed in Asset Decisions renewal window | 续费候选 evidence 显示错误，VPS 决策队列仍可显示已加载 VPS |
| all subscriptions failed while building decision queue | VPS 队列显示加载错误，避免把全量缺订阅误报为真实数据质量 |
| VPS inventory subscriptions empty | 行级展示 `缺订阅`，quick view `缺订阅` 可筛出对应 VPS |
| VPS inventory URL has unsupported `view` | 降级为 `all`，下次用户操作时写回合法 query |
| user removes a chip or clears all filters | URL-state 与 visible rows 同步更新，不重新请求 `/api/vps?...` derived query |
| `/subscriptions?vps_id=<id>&create=1` | 显示当前 VPS context panel，创建表单打开并预填该 VPS；关闭创建表单时移除 `create=1` 但保留 `vps_id` |
| selector candidate list is empty | 表单保留空值能力，显示去对应列表/创建流程的 Link/action，不要求手输内部 ID |
| selector list request fails | 表单显示局部错误/提示，已保存主页面数据仍可查看；不得把加载失败当成真实“无候选” |

#### 5. Good/Base/Bad Cases

- Good: `/vps?view=unlinked&renewal_decision=unreviewed` 首屏显示 `视图: 未关联` 和 `续费: 未评估` chips，列表只显示同时满足条件的 rows。
- Good: 资产决策保存 `migrate` 后，VPS 从 `待评估` tab 消失并出现在 `迁移` tab，notice 留在队列 surface。
- Good: 资产决策保存 `cancel` 后，notice 继续展示 `VPS -> 取消`，并追加 API 返回的订阅联动消息 / 订阅页 action。
- Good: VPS 详情打开 MonitoringInstance link Drawer 时懒加载 `listMonitoringInstances()`，用 `选择监控实例` selector 展示名称、ID、provider、生命周期和健康状态。
- Base: 订阅为空、Provider 为空时，页面仍能展示 VPS identity、状态、缺订阅、未关联/缺字段提示。
- Bad: Dashboard 或 VPSPage 从 `abnormal_linked_vps_count` 反推单台 VPS linked monitoring instance health。
- Bad: Page 直接 `fetch('/api/vps')` 或在组件层调 API；业务请求必须走 `lib/api.ts`。
- Bad: 在 VPS 详情表单里让用户输入 `mi_...`、`tg_...`、`svc_...` 作为常规路径，且不给候选列表或落地入口。

#### 6. Tests Required

- `AssetDecisionsPage.test.tsx`: 续费窗口请求、统一工作队列渲染、drawer 更新决策、保存后队列移动/移除、取消类联动 message/action、错误/空态。
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

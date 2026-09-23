# Dashboard Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

### Scenario: Dashboard 五状态与显式 RemoteState

#### 1. Scope / Trigger

- Trigger: 修改 `DashboardPage`、`dashboardModel.ts`、`DashboardCommandSurface`、Dashboard 深链、三项数据加载或首屏状态文案时，必须遵守本合同。
- 本节约束现有 wire shape 的消费方式；如果增删/改名 `/api/dashboard` 字段，还必须同步下方 `Dashboard Overview Contract`。

#### 2. Signatures

```ts
type RemoteState<T> =
  | { status: 'loading' }
  | { status: 'success'; value: T; loadedAt: string }
  | { status: 'error'; error: string }

type DashboardMode =
  | 'critical'
  | 'abnormal'
  | 'maintenance'
  | 'onboarding'
  | 'stable'

buildDashboardModel(input: {
  overview: RemoteState<DashboardOverview>
  vps: RemoteState<VPSAssetRecord[]>
  subscription: RemoteState<SubscriptionOverview>
}): DashboardModel
```

- 数据入口：`getDashboard()`、`listVPSAssets()`、`getSubscriptionOverview()`；三者必须独立保存状态，业务请求仍统一经过 `web/src/lib/api.ts`。

#### 3. Contracts

- 失败不得再用 `[]` / `null` 表示。`overview` 失败是可重试整页 error；VPS / subscription 失败是局部 degradation，并保留已成功的 Dashboard 摘要。
- mode 优先级固定为 `critical -> abnormal -> maintenance -> onboarding -> stable`。`onboarding` 只在 VPS 请求 `success([])` 且 `total_monitoring_instance_count === 0`、`total_target_count === 0` 时成立；VPS loading/error 永远不能触发首次接入。
- `abnormal_*_count` 已包含 `severe_*_count`。总异常只允许 `abnormal_monitoring_instance_count + abnormal_target_count`；严重只做优先级分层，禁止把 severe 再加进 abnormal。
- 每个 ready model 恰好一个 `primaryAction`。固定深链：critical → `/events?severity=严重`；abnormal → 有监控实例时 `/monitoring?abnormal=1`，否则 `/targets?abnormal=1`；maintenance → `/events?maintenance_only=1`；onboarding → `/vps`；stable → 真实资产 signal 的 `/asset-decisions?...`，无 signal 时 `/vps`。
- 判断摘要固定三项（观测、资产、订阅），每项必须链接到其文案所指的承接工作流；不得出现“资产待核对”却固定跳 `/vps` 的链接漂移。
- stable 只表示没有观测异常/维护/首次接入条件，不等于所有来源健康。stable + 资产待办显示 `资产判断等待核对`；stable + 局部请求失败使用 notice tone、标题 `部分事实待确认` 和信号 `局部数据不可用`，不得显示 `摘要无异常` 或 `当前没有紧急处理项`。
- subscription 请求失败时可使用 `DashboardOverview.asset_summary.cost_by_currency` 作为较低精度 fallback，但必须同时展示来源、失败信息和 `snapshot_generated_at`；不能伪装成 subscription overview 同精度结果。
- `snapshot_generated_at` 只能表达 `摘要生成`。VPS `loadedAt` 只能表达客户端完成读取的时间；两者都不是 Center health、agent heartbeat 或全链路同步证明。
- 首屏只保留一个 command surface、一个 `今日第一步`、三项判断摘要和两条证据 lane。异常对象最多展示前三项；完整事件、资产、订阅明细交给对应路由。不得恢复独立 KPI strip、Group 摘要、最近事件列表、系统快捷入口或第二套 Dashboard workbench。
- `abnormal_monitoring_instances` / `abnormal_targets` 只用于异常对象预览，不能推导全量 group/provider/region。`notification_status` 仍只能包含布尔配置摘要，不得暴露 token/chat id/webhook。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| overview loading | 整页显示 `正在加载工作台…` |
| overview error | 整页显示 `工作台不可用` + retry，不渲染 command surface |
| VPS `success([])` 且观测库存为 0 | onboarding，唯一主行动 `创建第一台 VPS` |
| VPS 503 且观测库存为 0 | 非 onboarding；显示 `部分事实待确认`、VPS 局部错误和 retry |
| abnormal=2、severe=1 | critical；UI 异常总数仍为 2，严重为 1 |
| abnormal>0、severe=0 | abnormal；按异常主体跳 monitoring 或 targets |
| abnormal=0、maintenance>0 | maintenance；跳维护事件 |
| subscription 503 | 页面保留；标明较低精度 Dashboard fallback，不制造 0 成本事实 |
| stable + asset signal | mode 仍为 stable，但标题/信号和 primary action 表达真实资产待办 |

#### 5. Good/Base/Bad Cases

- Good: Dashboard 返回 abnormal=2/severe=1，页面展示“异常总数 2（严重已包含）”，主行动只有“处理严重异常”。
- Good: VPS 清单 503，Dashboard 仍展示已加载观测事实，但明确写“部分事实待确认”，重试只触发 supporting resources。
- Base: subscription 仍在 loading；账单判断显示读取中并暂用 Dashboard 聚合来源，不把 loading 写成真实空数据。
- Bad: `listVPSAssets().catch(() => [])` 导致请求失败时出现“创建第一台 VPS”。
- Bad: stable 模式无条件显示“摘要无异常”，同时主行动却是“进入资产组合决策”或页面存在局部失败。
- Bad: 恢复被删除的第二套 command surface、全量 KPI/Group/recent-events dump，或让多个同权 CTA 竞争 `今日第一步`。

#### 6. Tests Required

- `dashboardModel.test.ts`: subset 计数、五 mode 优先级、VPS failure-not-onboarding、stable asset signal、fallback 来源、loading/error。
- `DashboardPage.test.tsx`: 五 mode 唯一主行动及 deep link、禁止旧 surface、VPS 503、supporting retry、订阅 fallback、异常详情链接和可信标题。
- `internal/center/store/dashboard_test.go` 与 `internal/center/http/handlers/dashboard_test.go`: abnormal=2/severe=1，并断言 severe 不大于 abnormal。
- 用户可见结构变化必须更新/运行 repository Playwright：`page-states.spec.ts` 固定五种 Dashboard fixture，`core-routes.spec.ts` 覆盖 `1440x1000`、`1024x768`、`390x900`，统一断言主行动、横向溢出、裁切和 console/page/CSP/network。该证据仍是 fixture frontend rendering，不代表后端或真实资产通过；真实数据只由 staging real lane 证明。

#### 7. Wrong vs Correct

```tsx
// Wrong: error 被伪装成 empty，且 severe 被重复加总。
const vps = await listVPSAssets().catch(() => [])
const abnormalTotal = overview.abnormal_monitoring_instance_count
  + overview.severe_monitoring_instance_count

// Correct: 保留显式来源状态；severe 只做分层。
const vps: RemoteState<VPSAssetRecord[]> = await listVPSAssets()
  .then((value) => remoteSuccess(value, new Date().toISOString()))
  .catch((error) => remoteError(errorMessage(error, '加载 VPS 清单失败')))
const abnormalTotal = overview.abnormal_monitoring_instance_count
```

### Scenario: AppShell 摘要 freshness 与保守刷新

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/app/layout/AppShell.tsx`、`SyncStatus.tsx`、`shellSummaryModel.ts`，或改变 AppShell 对 `/api/dashboard` 的刷新、状态文案、生成时间与导航异常计数时，必须遵守本合同。
- 目标：Shell 只陈述 Dashboard snapshot 事实，不把一次快照扩大解释为 Center、agent、通知链路或整个平台健康。

#### 2. Signatures

```ts
const SHELL_SUMMARY_FRESHNESS_MS = 5 * 60_000

type DashboardSummaryState =
  | { status: 'loading'; overview: null; error: null }
  | { status: 'success'; overview: DashboardOverview; error: null }
  | { status: 'error'; overview: DashboardOverview | null; error: string }

type ShellSummaryStatus =
  | 'loading'
  | 'clear'
  | 'anomaly'
  | 'stale'
  | 'unavailable'

buildShellSummaryModel(summary: DashboardSummaryState, now: number): ShellSummaryModel
```

- 数据源保持 `getDashboard()` → `GET /api/dashboard`；本场景不改变后端 JSON wire shape。

#### 3. Contracts

- freshness 只以 `DashboardOverview.snapshot_generated_at` 与当前时刻比较；客户端请求完成时间不得冒充摘要生成时间。
- fresh success 无异常为 `clear / 摘要无异常`，有异常为 `anomaly / 摘要有异常`；禁止使用“系统正常”“同步完成”或等价全链路健康文案。
- snapshot 生成时间无效或达到 5 分钟窗口时为 `stale / 摘要已过期`。使用一次性 timeout 触发到期重算，不引入常驻 interval。
- 初次请求失败且没有成功快照时为 `unavailable / 摘要不可用`；已有成功快照后的刷新失败保留该 overview 与原 `snapshot_generated_at`，但状态必须转为 `stale`。
- 只有 `clear` / `anomaly` 可以把异常计数传给 Sidebar；`loading` / `stale` / `unavailable` 必须隐藏 nav badge，不能用 0 暗示无异常。
- mount 请求一次；document 从 hidden 变为 visible 或 window focus 时刷新。visibility 与 focus 连续到达时共享同一 in-flight Promise，禁止重复发请求；本合同不启用轮询。
- 顶栏必须同时提供状态形状/颜色、可见状态文案、服务端生成时间和可访问名称；窄视口隐藏可见副文案时，`role="status"` 的 `aria-label` 仍必须保留状态与生成时间。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 首次请求 pending | `loading`；无 nav badge |
| fresh snapshot，abnormal 总数为 0 | `clear`，显示“摘要无异常”与服务端生成时间 |
| fresh snapshot，abnormal 总数大于 0 | `anomaly`，显示“摘要有异常”与真实 nav badge |
| snapshot 超过 5 分钟或时间无效 | `stale`；保留生成时间；隐藏 nav badge |
| 首次请求 503 | `unavailable`；不制造 0 计数 |
| 成功后 focus refresh 503 | 保留上次 overview，转 `stale`，生成时间不改，隐藏 nav badge |
| visible 与 focus 在请求未完成时连续触发 | 只新增一个 `/api/dashboard` 请求 |

#### 5. Good / Base / Bad Cases

- Good：摘要生成于 1 分钟前且有 2 个异常，顶栏显示“摘要有异常 · 摘要生成 …”，Sidebar 展示真实 2；focus 刷新成功后以新服务端时间更新。
- Base：页面隐藏期间摘要超过 5 分钟；一次性 timeout 将状态转 stale，恢复可见后再发一次刷新。
- Bad：`getDashboard().catch(() => null)` 后显示“系统正常”或两个 0 badge。
- Bad：刷新失败时清空 last success，或用 `new Date().toISOString()` 替换服务端 `snapshot_generated_at`。
- Bad：同时监听 visibility/focus 却各自直接请求，或为了 freshness 引入常驻 interval。

#### 6. Tests Required

- `AppShell.test.tsx`：loading / clear / anomaly / stale / unavailable 五态；无“系统正常”；生成时间来自 fixture；stale/failure 隐藏 nav badge。
- `AppShell.test.tsx`：fake timer 越过 5 分钟；hidden 不刷新；visible/focus 刷新；同一 in-flight 请求去重；刷新失败保留 last success。
- 浏览器 sanity：核心路由在 `1440x1000`、`1024x768`、`390x900` 下状态/生成时间可访问、无 document 横向溢出、无 page/console error；用 mock 只能证明代表性前端渲染。

#### 7. Wrong vs Correct

```tsx
// Wrong: 客户端完成时间冒充同步时间；失败后丢弃已知快照。
getDashboard()
  .then((overview) => setSummary({ overview, loadedAt: new Date().toISOString() }))
  .catch(() => setSummary({ overview: null, loadedAt: null }))

// Correct: 保留服务端时间与 last success；纯 model 决定五态和计数可见性。
setDashboardSummary((current) => ({
  status: 'error',
  error: message,
  overview: current.overview,
}))
const shell = buildShellSummaryModel(dashboardSummary, now)
```

## Dashboard wire contract

字段、聚合范围、敏感数据 allowlist 和错误协议见 [Dashboard 合同](dashboard.md)。

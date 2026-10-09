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

- 失败不得再用 `[]` / `null` 表示。首次 `overview` 失败，以及之后的 401、403、404，是可重试整页 error，并清除已显示的摘要。已有成功摘要后的普通网络失败保留该摘要和 `snapshot_generated_at`，显示“更新失败，显示上次结果”，不把计数改成 0，也不称为最新。VPS / subscription 失败仍是局部 degradation，并保留已成功的 Dashboard 摘要。
- mode 优先级固定为 `critical -> abnormal -> maintenance -> onboarding -> stable`。`onboarding` 只在 VPS 请求 `success([])` 且 `total_monitoring_instance_count === 0`、`total_target_count === 0` 时成立；VPS loading/error 永远不能触发首次接入。观测过期不改变该优先级，也不创建或恢复 incident。
- `abnormal_*_count` 已包含 `severe_*_count`。总异常只允许 `abnormal_monitoring_instance_count + abnormal_target_count`；严重只做优先级分层，禁止把 severe 再加进 abnormal。`stale_target_count` 与异常、无观测分别计数，重叠目标不得相加。
- `unobserved_target_count` 使用后端全量计数并链接 `/targets?view=unobserved`。`stale_target_count` 使用后端全量计数并链接 `/targets?view=stale`。已知异常、无观测、观测过期都为零时，才使用“当前运行异常计数为 0”；任一非零都单独列出。资产待核对仍是独立事实。
- `group_summaries` 中 `stale_target_count > 0` 的分组链接 `/targets?view=stale&group=<group>`，保留服务端分组名，空白分组的服务端名称 `未分组` 也原样进入 query。计数为零的分组不渲染，不得恢复按 Group 分布、第四张 KPI 或其它已删除的摘要 dump。
- 下层证据 lane 保留来源、摘要生成/读取时间与金额完整性，不重复上层同组判断数字。未知目标使用中性状态，不由历史观测或摘要计数保证当前健康。观测证据 lane 另写监控覆盖事实“监控覆盖：在用 VPS 中 N/M 台已关联监控实例”：只取 VPS 清单中 `lifecycle_status=active` 的条目与其 `active_monitoring_instance_link_count`，单独标注“来源：VPS 清单 · 读取于”时间，只表达关联、不代表在线或健康（见 assets-web）；未全部关联时链接 `/vps?workspace=workbench&view=unlinked`；VPS 清单读取中或失败时如实写读取中/暂不可用；onboarding 或没有在用 VPS 时不显示。它不是第四张 KPI。
- 每个 ready model 恰好一个 `primaryAction`。固定深链：critical → `/events?severity=严重`；abnormal → 有监控实例时 `/monitoring?abnormal=1`，否则 `/targets?abnormal=1`；maintenance → `/events?maintenance_only=1`；onboarding → `/vps`；stable → 真实资产 signal 的 `/asset-decisions?...`，无 signal 时 `/vps`。
- 判断摘要固定三项（观测、资产、订阅），每项必须链接到其文案所指的承接工作流；不得出现“资产待核对”却固定跳 `/vps` 的链接漂移。资产卡大字与标题指同一件事：跟进待核对取自动续费待核对与跟进事项之和，决策待核对取待决定与决定不续费之和，续费窗口取 30 天内待续费 VPS 台数，证据待补齐取未关联与关联异常之和；没有资产信号时标题为“已登记 VPS”、大字为 VPS 台数；VPS 清单读取中或不可用时沿用台数或“待确认”。
- stable 只表示没有观测异常/维护/首次接入条件，不等于所有来源健康。stable + 资产待办显示 `资产判断等待核对`；stable + 局部请求失败使用 notice tone、标题 `部分事实待确认` 和信号 `局部数据不可用`，不得显示 `摘要无异常` 或 `当前没有紧急处理项`。
- subscription 请求失败时可使用 `DashboardOverview.asset_summary.cost_by_currency` 作为较低精度 fallback，但必须同时展示来源、失败信息和 `snapshot_generated_at`；不能伪装成 subscription overview 同精度结果。
- `snapshot_generated_at` 只能表达 `摘要生成`。VPS `loadedAt` 只能表达客户端完成读取的时间；两者都不是 Center health、agent heartbeat 或全链路同步证明。
- 首屏保留一个 command surface、一个 `今日第一步`、三项判断摘要（以指标卡呈现，观测卡可在后端返回 24 个逐小时桶时附 `new_incident_trend_24h` 趋势，否则不画趋势）和两条证据 lane；桌面两栏布局中，右栏另有两块**有界预览**：`即将续费`（来自 subscription overview 的 `upcoming_renewals`，该队列由后端按 UTC 日窗口 `[当天, 当天+90]` 筛选且最多返回 12 条：按续费日升序最多预览 5 项，计数标注“未来 90 天（UTC）· N 项”，达到 12 条上限时写“至少 12 项”而不是总数；剩余天数与窗口同源，按续费日期与订阅摘要 `snapshot_generated_at` 所在 UTC 日期的日历差计算（后端 `subscriptioncosts` 以同一时刻确定窗口与生成时间，不用浏览器接收时间，避免跨 UTC 午夜或客户端时钟偏差；该字段无效时才退回接收时间，并在计数旁标注“天数按接收时间估算”），当天为“今天”，早于当天的兜底显示“已过”；空队列写“未来 90 天（UTC）内没有待续费的订阅”；缺折算金额时显示“金额待核对”，已有过期汇率数值可显示但必须标注“汇率过期”；链接订阅明细；续费日早于 UTC 当天的订阅不属于该窗口，改由 overview 的 `overdue_renewals`/`overdue_renewal_count`（已决定不续费的不计入）在列表之前单独显示“已逾期 N 项”，最多预览 3 项（按续费日升序，天数写“逾期 N 天”）并链接订阅页续费队列，90 天窗口的计数与空状态不变；旧版 center 缺这两个字段时视为没有逾期；subscription loading/error 时如实显示读取中/不可用，不得显示为“没有待续费”）和 `最近动态`（来自 `recent_events`，按时间倒序最多 5 项；`recent_events` 无时间下界，入口链接不带 `time_range`，避免旧事件点进后为空）。DOM 与阅读顺序为：判断摘要 → 即将续费 → 证据 lane → 最近动态；两栏布局由 grid 区域把即将续费与最近动态放回右栏（即将续费在顶部），单栏（≤1024px）时按 DOM 顺序排列，可行动的续费预览不再落在证据之后。异常对象最多展示前三项；完整事件、资产、订阅明细仍交给对应路由。不得恢复独立的第四张 KPI、Group 摘要、系统快捷入口、无上限的事件/订阅列表或第二套 Dashboard workbench。
- `abnormal_monitoring_instances` / `abnormal_targets` 只用于异常对象预览，不能推导全量 group/provider/region。`notification_status` 仍只能包含布尔配置摘要，不得暴露 token/chat id/webhook。
- `total_monitoring_instance_count` 或 `total_target_count` 大于 0，且 `notification_status` 的 `telegram_configured`、`telegram_runtime_managed`、`feishu_configured` 全为 false 时，判断摘要之前显示一条 `role=note` 的「未配置通知渠道」：说明失联和告警只会显示在页面里、不会推送，并链接 `/settings?tab=notification`。这只是配置摘要，不宣称已配置渠道一定送达；尚无观测对象时不显示。
- 账单判断和成本证据消费 `current_unknown_amount_count`、`current_missing_rate_count`、`current_stale_rate_count`。金额不完整时称已知金额小计，全部未知时称金额待核对；预算风险零不能替代完整性事实。金额原价为零且换算可用仍为已知零。
- Dashboard overview 的初次读取和之后的可见刷新共用 `useVisibleRefresh`。可见时每 30 秒只刷新 overview；hidden 不请求；回到可见或 focus 立即刷新，并与在途请求去重。卸载后的迟到响应不得应用。VPS 与订阅摘要仍只在挂载和显式重试时读取。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| overview loading | 整页显示 `正在加载工作台…` |
| overview 首次失败或 401/403/404 | 整页显示 `工作台不可用` + retry，不保留上次摘要 |
| VPS `success([])` 且观测库存为 0 | onboarding，唯一主行动 `创建第一台 VPS`，链接 `/vps?create=1`：VPS 页据此直接打开创建对话框（已在 VPS 页时经客户端导航带上该参数同样打开），并用 replace 移除这一次性参数 |
| VPS 503 且观测库存为 0 | 非 onboarding；显示 `部分事实待确认`、VPS 局部错误和 retry |
| abnormal=2、severe=1 | critical；UI 异常总数仍为 2，严重为 1 |
| abnormal>0、severe=0 | abnormal；按异常主体跳 monitoring 或 targets |
| abnormal=0、maintenance>0 | maintenance；跳维护事件 |
| subscription 503 | 页面保留；标明较低精度 Dashboard fallback，不制造 0 成本事实 |
| stable + asset signal | mode 仍为 stable，但标题/信号和 primary action 表达真实资产待办 |
| 成功后 overview 网络失败 | 保留 command surface、原计数和摘要生成时间，显示“更新失败，显示上次结果” |
| stale 与 abnormal 重叠 | 异常总数不包含 stale；两条事实分别链接 |
| 分组 `stale_target_count > 0` | 链接保留 group；零计数分组不出现 |
| 可见 30 秒 | 再读一次 overview；hidden 不读 |

#### 5. Good/Base/Bad Cases

- Good: Dashboard 返回 abnormal=2/severe=1，页面展示“异常总数 2（严重已包含）”，主行动只有“处理严重异常”。
- Good: VPS 清单 503，Dashboard 仍展示已加载观测事实，但明确写“部分事实待确认”，重试只触发 supporting resources。
- Base: subscription 仍在 loading；账单判断显示读取中并暂用 Dashboard 聚合来源，不把 loading 写成真实空数据。
- Bad: `listVPSAssets().catch(() => [])` 导致请求失败时出现“创建第一台 VPS”。
- Bad: stable 模式无条件显示“摘要无异常”，同时主行动却是“进入资产组合决策”或页面存在局部失败。
- Good: 订阅摘要返回 7 条续费，`即将续费` 只列最早 5 条并显示“未来 90 天（UTC）· 7 项”；返回 12 条（后端上限）时显示“至少 12 项”。东八区凌晨与美西傍晚跨日时，剩余天数仍落在 0–90 天内。`最近动态` 只列最新 5 条事件，入口打开不限时间的事件流。
- Bad: 恢复被删除的第二套 command surface、全量 KPI/Group/recent-events dump，或让多个同权 CTA 竞争 `今日第一步`。
- Bad: subscription 503 时 `即将续费` 显示“近期没有待续费”，或后端未提供趋势时把观测卡趋势画成一条 0 线。

#### 6. Tests Required

- `dashboardModel.test.ts`: subset 计数、五 mode 优先级、VPS failure-not-onboarding、stable asset signal、fallback 来源、loading/error，以及 stale 与 abnormal/unobserved 分开计数、分组链接保留 group。
- `DashboardPage.test.tsx`: 五 mode 唯一主行动及 deep link、禁止旧 surface、VPS 503、supporting retry、订阅 fallback、异常详情链接和可信标题；判断摘要固定 3 项，续费/动态预览各最多 5 项，subscription 503 时续费预览显示不可用。另覆盖 stale 链接与重叠不加总、分组过期链接、网络失败保留上次摘要、401/404 清除、可见 30 秒刷新和隐藏不请求。
- `dashboardPanels.test.ts`: 动态倒序与上限、状态色、续费 loading/error/ready、升序、窗口返回数与 12 条上限提示、以 `snapshot_generated_at` 为准的 UTC 日历剩余天数（用例内切换 `TZ` 覆盖东八区凌晨、美西傍晚与接收时间跨日）与日期截取、汇率过期金额、趋势长度校验。
- `internal/center/subscriptioncosts/service_test.go`: 时钟在 UTC 午夜两侧交替时，续费窗口“今天”与 `snapshot_generated_at` 仍是同一天。
- `internal/center/store/dashboard_test.go` 与 `internal/center/http/handlers/dashboard_test.go`: abnormal=2/severe=1，并断言 severe 不大于 abnormal。
- 用户可见结构变化必须更新/运行 repository Playwright：`page-states.spec.ts` 固定五种 Dashboard fixture，`core-routes.spec.ts` 覆盖 `1440x1000`、`1024x768`、`390x900`，统一断言主行动、横向溢出、裁切和 console/page/CSP/network；带数据的 `dashboardPopulatedProfile` 在 `1440x1000`、`1100x800`（两栏布局的最窄视口；≤1100px 默认收起为图标栏，右栏随之变宽）与 `1024x768`（单栏）下断言有界预览、唯一主行动、面板内无横向溢出，超长续费名称与服务商不被裁切，以及两栏时即将续费位于右栏顶部、单栏时排在证据 lane 之前；`390x900` 另断言即将续费紧随判断摘要。该证据仍是 fixture frontend rendering，不代表后端或真实资产通过；真实数据只由 staging real lane 证明。

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
  | 'unobserved'
  | 'notice'
  | 'stale'
  | 'unavailable'

buildShellSummaryModel(summary: DashboardSummaryState, now: number): ShellSummaryModel
```

- 数据源保持 `getDashboard()` → `GET /api/dashboard`；本场景不改变后端 JSON wire shape。

#### 3. Contracts

- freshness 只以 `DashboardOverview.snapshot_generated_at` 与当前时刻比较；客户端请求完成时间不得冒充摘要生成时间。
- fresh success 在异常、无观测、观测过期都为零时为 `clear / 当前运行异常计数为 0`。非零项分别写成“运行异常 N”“尚有目标无观测”和“观测过期 N”，不得把 stale 加进异常。只有无观测时仍显示“尚有目标无观测”。只有观测过期时为 `notice`，不得使用表示系统摘要过期的 `stale`。禁止使用“系统正常”“系统摘要无异常”“同步完成”或等价全链路健康文案。
- 系统摘要 freshness 只以 `snapshot_generated_at` 与当前时刻比较。生成时间无效或达到 5 分钟窗口时为 `stale / 系统摘要已过期`，并用一次性 timeout 触发到期重算。这与目标观测过期无关。
- 初次请求失败且没有成功快照时为 `unavailable / 系统摘要不可用`。401、403、404 清除 overview，同样变为 unavailable，不得继续展示上次计数。已有成功快照后的普通网络失败保留 overview 与原 `snapshot_generated_at`，状态为 `stale`，文案为“更新失败，显示上次结果”。
- 只有 `clear` / `anomaly` / `unobserved` / `notice` 可以把异常、无观测和观测过期计数传给 Sidebar。`loading` / `stale` / `unavailable` 必须隐藏 nav badge（含“入口探测”旁的尚无观测/观测过期分类徽标），不能用 0 暗示无异常或无过期。无观测与观测过期计数以分类徽标链接挂在“入口探测”上，不再作为随数据出现的独立导航项（见 component-patterns）。
- 初次读取由调用方通过共享 `useVisibleRefresh().refresh()` 发起，并与后续读取共用同一 in-flight。页面可见时每 30 秒刷新一次；hidden 期间的 interval、visibility 与 focus 不发请求；回到 visible 或 focus 时立即刷新。连续唤醒共享同一个 in-flight Promise。5 分钟系统摘要到期仍是一次性 timeout，不另建第二条轮询。`refreshKey`、invalidate 或卸载使迟到回调的 `isCurrent()` 为 false，调用方不得应用该结果。
- 顶栏必须同时提供状态形状/颜色、可见状态文案、服务端生成时间和可访问名称；窄视口隐藏可见副文案时，`role="status"` 的 `aria-label` 仍必须保留状态与生成时间。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 首次请求 pending | `loading`；无 nav badge |
| fresh snapshot，异常、无观测、观测过期均为 0 | `clear`，显示“当前运行异常计数为 0”与服务端生成时间 |
| fresh snapshot，只有无观测 | 显示“尚有目标无观测”，独立链接无观测筛选 |
| fresh snapshot，只有观测过期 | `notice`，显示“观测过期 N”，链接 `/targets?view=stale` |
| fresh snapshot，异常、无观测、观测过期同时非零 | `anomaly`，三项分开列出，nav badge 不把 stale 加进异常 |
| snapshot 超过 5 分钟或时间无效 | `stale / 系统摘要已过期`；保留生成时间；隐藏 nav badge，包括观测过期 |
| 首次请求 503 | `unavailable`；不制造 0 计数 |
| 成功后 focus refresh 503 | 保留上次 overview 与生成时间，文案“更新失败，显示上次结果”，隐藏 nav badge |
| 成功后 refresh 401/403/404 | 清除 overview，`unavailable`，隐藏 nav badge |
| 可见满 30 秒 | 再请求一次 `/api/dashboard` |
| hidden 期间 interval 或 focus | 不新增请求 |
| visible 与 focus 在请求未完成时连续触发 | 只新增一个 `/api/dashboard` 请求 |
| 用户切换后旧请求才返回 | 不应用旧用户的计数 |

#### 5. Good / Base / Bad Cases

- Good：摘要生成于 1 分钟前且有 2 个异常，顶栏显示“摘要有异常 · 摘要生成 …”，Sidebar 展示真实 2；focus 刷新成功后以新服务端时间更新。
- Base：页面隐藏期间摘要超过 5 分钟；一次性 timeout 将状态转 stale，恢复可见后再发一次刷新。
- Bad：`getDashboard().catch(() => null)` 后显示“系统正常”或两个 0 badge。
- Bad：刷新失败时清空 last success，或用 `new Date().toISOString()` 替换服务端 `snapshot_generated_at`。
- Bad：同时监听 visibility/focus 却各自直接请求，或在隐藏页面继续 30 秒轮询；把目标观测过期写成“系统摘要已过期”。

#### 6. Tests Required

- `AppShell.test.tsx`：loading / clear / anomaly / notice / stale / unavailable；无“系统正常”；生成时间来自 fixture；系统摘要过期和刷新失败隐藏 nav badge；观测过期与异常分开。
- `AppShell.test.tsx`：fake timer 越过 5 分钟；可见 30 秒刷新；hidden 的 interval/focus 不刷新；visible/focus 刷新；同一 in-flight 请求去重；网络失败保留 last success；401/404 清除；用户切换后的迟到响应不覆盖新用户。
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

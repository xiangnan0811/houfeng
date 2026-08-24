# VPS 概览来源隔离与 freshness 技术设计

## 1. 根因与目标边界

当前 service 给 bundled `LoadSources` 一个 800ms context，而 store 在其中顺序读取
monitoring、IP、renewal、relations；前源变慢会剥夺后源执行机会。relation 没有
`SectionState`，失败被压成可信零；renewal 又把未来 `RenewAt` 当作 observation。本 child 将
identity 保持 fatal，把其余六个 authority 拆为独立 bounded result，并把 freshness 从 Go
source 一路送到本地 Web surface。

不新增 source、cache、schema 或 source-specific retry endpoint。I-01 决定 relation route/
interactivity；I-02 决定 success decoder 与 gate error。本 child 冻结两者要消费的最终
`relation.section` wire shape。

## 2. Backend reader 与 budget 模型

将 `SourceReader.LoadSources` 拆成 granular methods：identity、monitoring、IP quality、renewal、
service relation、domain relation；activity 保持独立 reader。identity 返回 Identity/Facts，其他
method 返回自己的 typed source result 或 error。monitoring/subscription relation 直接复用
monitoring/renewal result，避免第二次查询；services/domains 各一个 bounded query。

引入 validated `SourceBudgets`：Total、Identity、Monitoring、IPQuality、Renewal、Services、
Domains、Activity 都必须大于零。生产默认先全部保持现有 800ms；每个 child context 同时受自己
budget 与 total remaining deadline 约束，不能为了让测试通过放大 endpoint wall clock。真实
PostgreSQL p95/query-count gate 决定是否需要后续调优。

执行顺序：创建 total context → identity child read；identity 失败立即返回且不启动其他 source；
成功后并发启动六个 degradable readers。每个 goroutine 向容量足够的 typed result channel 只写
一个 immutable result；collector 等到全部结果或 total deadline。caller cancellation 仍 fatal；
service total deadline 保留已完成值，只把 pending source 标为 timeout。迟到 goroutine 不得修改
已组装 Overview，并必须观察 context cancellation。

完成顺序不影响 response：relations 固定 monitoring_instances、subscriptions、services、domains。
任何 degradable error 都由 service 映射为 closed safe reason，禁止 `err.Error()`、SQL、endpoint、
cursor/checkpoint 或 worker time 出现在 wire。

## 3. Freshness authority

`SectionState` 的 state 是 `ready|stale|unavailable`，时间 nullable，reason 为 allowlisted code。
成功但空的 source 是 ready + null timestamps；不能用请求时间伪造 observation。

| Surface | observed/last-success authority |
| --- | --- |
| overall | derived `generated_at` |
| monitoring summary/relation | 选中实例 `LastHeartbeatAt` |
| IP summary | `ipquality.Summary.ObservedAt` |
| renewal summary/subscription relation | 所有返回 subscription 的最大非零 `UpdatedAt` |
| services relation | 返回 service rows 的最大非零 `UpdatedAt` |
| domains relation | 返回 domain rows 的最大非零 `UpdatedAt` |
| recent activity | 当前授权 scope 的 `Freshness.VisibleObservedAt` |

renewal 的 `next_renew_at` 仍取 active subscriptions 最早非零 RenewAt；它与 freshness 独立。
activity 不读 global projector checkpoint。失败没有 persisted last-success authority 时返回 null，
不根据 client retained data 回填 backend 字段。

聚合完成后才捕获 `generated_at`，随后统一规范 UTC 并检查 summary/activity/every relation：任何
freshness timestamp 晚于 generated_at 时删除该时间、不 clamp，ready 降为 stale，reason 使用
`source_timestamp_invalid`；unavailable 保持原更具体 safe reason。业务 deadline 不参加该校验。

## 4. Relation wire contract

Go `RelationSummary` 保留 kind/count/status/route/label，新增 required `Section SectionState`，
JSON tag 为 `json:"section"`。失败时 count 为兼容保留的零、status 可为 unavailable，
但新 Web 必须先看 section，不能把它显示为可信零。TypeScript 将 section state 收窄成三值 union，
并把 relation.section 设为 required；route 可选性由 I-01 在此基础上修改。

这是 additive server response。旧 Web 会忽略 section；新 Web 遇到缺失 section 的旧/漂移后端由
I-02 typed decoder 显示错误，不得合成 ready 或选择 legacy。canonical E2E fixtures 同步增加
section。

## 5. Web local state 与 retry ownership

新增 route-private `VPSOverviewFreshness`，接收 section/source label/onRetry/retrying。它显示三态
文本、Timestamp 观测/最近成功、known reason 的 bounded 中文说明；unknown reason 只显示泛化
文案，绝不显示 raw code。ready 不显示 retry，stale/unavailable 显示带 source accessible name
的 retry。

- SummaryGrid：monitoring/IP/renewal cell 各自显示；overall 无 local retry。
- RecentActivity：unavailable empty 不再说“暂无最近活动”；stale rows 保留。
- Relations：unavailable count 显示“—”；retry 与 relation Link 为 siblings，不嵌套 interactive。
- PageView：把现有一次完整 refresh command 和 loading 状态传下去，删除重复的底部共享 note。

所有 local retry 仍调用 `useVPSOverview.commands.refresh` 的单一 overview GET。refresh 时保留旧
overview、禁用全部 retry 防重复；refresh transport failure 的 page classification 归 I-02。

## 6. 文件所有权、兼容与回滚

- Backend：`internal/center/vpsoverview/{types,service}*.go`、
  `internal/center/store/vps_overview*.go`、handler wire tests。
- Web：`web/src/lib/types.ts`、freshness component/tests、SummaryGrid、RecentActivity、Relations、
  PageView、hook tests、overview fixtures/state E2E 与已有 CSS owner。
- 不修改 anomaly route、React Router dependency 或 gate classifier。

无 migration。若 concurrency 造成未满足的真实 PostgreSQL p95/query-count regression，停止并以
测量结果评审 budget/query strategy，不退回顺序 shared-budget。UI 可独立 revert，但 backend
truthful section 不应回退；release rollback 可关闭现有 capability 到 legacy。

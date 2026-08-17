# 证据注册表与首批适配器设计

## 0. Development rebaseline

`0054` 与其 current APP ACL fragment 是一个原子交付。保留 schema registry、可信来源、敏感分类和 bounded capture；不承担旧数据转换或 staging/release。

## 1. 存储与registry

`0054_create_record_evidence.sql` 创建 `evidence_snapshots`、`evidence_payloads`、`record_revision_evidence`、`evidence_capture_intents`、`evidence_copy_lineage` 与 receipts。payload是确定性canonical bytes的gzip/bytea，digest唯一；logical snapshot保存owner/auth/source/provenance，不共享。

```go
type Kind interface {
	Descriptor() Descriptor
	ValidateSelection(context.Context, ActorScope, Selection) error
	PreviewCapture(context.Context, ActorScope, Selection) (Preview, error)
	Capture(context.Context, ActorScope, Intent) (CanonicalSnapshot, error)
	Authorize(context.Context, ActorScope, Selection) (AuthorizationScope, error)
	Summarize(CanonicalSnapshot) Summary
	Compare(CanonicalSnapshot, CanonicalSnapshot, Alignment) Comparison
	Export(CanonicalSnapshot, ExportMode) ExportMaterial
}
```

registry拒绝重复kind/version和无conformance metadata。server response 只由kind renderer DTO构建。

## 2. 首批adapter与权威来源

- `ip_quality.report/v1`：`store/ip_quality.go`的report revision/raw allowlist；固化provider/services、observed_at、stale policy与mask。
- `monitoring.host/v1`：直接查询host raw+aggregate，绝对窗口/覆盖/桶；不复用0-fill sparkline。
- `monitoring.probe/v2`：直接查询probe raw+aggregate，绝对窗口/覆盖/桶；不复用0-fill sparkline。
- `monitoring.event/v2`：state_change_events/incident metadata，event_at/recorded_at/backfilled/correction。
- `subscription.cost/v1`：subscription current + monthly budget + rate/date/base currency + actual coverage。
- 资产历史（renewal/price/IP/spec）是权威 source/activity adapter，不新增未在父设计注册表中声明的 `asset.history/*` kind。
- `command.audit/v1`：只允许command/sensitivity/event/source/actor/exit/occurred元数据；output/details永久禁止。

### 2.1 IP/监控静态阅读与比较合同

Task 3 的 `Summarize` 输出显式版本化 allowlisted read model，而不是透传任意 JSON：IP 使用 `ip_quality_report_read_model/v1`，只含 report/observed/received/IP version、status/stale/risk、coverage、provider/service 正常字段和 envelope quality，默认不含敏感拓扑；host/probe 分别使用 `monitoring_host_read_model/v1`、`monitoring_probe_read_model/v1`，含 requested/coverage window、实际精度、bounded buckets/gaps/peaks 和 envelope quality。Task 6 renderer 只按这些版本键消费。

`AlignmentExact` 的 IP 比较版本为 `ip_quality_report_comparison/v1`，要求相同 kind/schema、calculation version、不可用单位语义、requested window 时长和 point precision 语义，返回 status/stale/risk 变化与 coverage 计数差。host/probe 比较版本分别为 `monitoring_host_comparison/v1`、`monitoring_probe_comparison/v1`，要求相同 kind/schema、calculation version、指标单位集合、requested window 时长、实际精度和桶宽；允许绝对窗口不同以支持同长度纵向窗口。比较结果按 `series_id + metric` 汇总已存在 bucket：average 按 bucket sample count 加权，min/max 取实际极值，p95 只报告 `mean_bucket_p95` 及其差值，不把 bucket p95 伪装成全窗口分位数；同时返回 quality 计数差。缺失侧只报告实际 count，不补零、不外推。全部 compare/read model 继续受 capture 的指标、桶、点数和峰值上限约束。

adapter 必须在 custom source 边界先复制并确定性排序 IP provider/service 与 monitoring bucket/metric 数组，再构造 canonical payload；不能依赖某个 PostgreSQL query 当前恰好有序。IP source 的地址/version、report/provider/probe/service/assignment 闭合枚举、coverage 分类计数与实际 rows、全部时间和 future receipt 必须失败关闭。monitoring source 的 coverage/observed/bucket/watermark 必须是 PostgreSQL 微秒可表示的真实时序，watermark 使用 canonical UTC RFC3339Nano 且满足 `coverage_start <= observed_at < coverage_end`、`observed_at <= watermark <= capture clock`；per-series gap 展开在分配期间受 `MaxSnapshotDataPoints` 硬上限保护，不能等到 JSON canonicalization 才拒绝。

### 2.2 事件、成本与命令静态合同

`monitoring.event/v2` 只消费来源显式保存的 event/recorded time、backfill、correction、provenance、producer/rule version、prior/resulting state 和有界 metric context；缺任一必要元数据时 source 失败关闭，不从 legacy payload 或当前 incident 投影推断。`Summarize` 使用 `monitoring_event_read_model/v2`，compare 使用 `monitoring_event_comparison/v2`，要求 exact alignment、相同 kind/schema、calculation version、units 与 requested window 时长，返回 event/backfill/correction count delta。

所有可进入该 kind 的 production `state_change_events` writer——incident、monitoring-instance binding/lifecycle/runtime 与 target runtime——必须通过共享 typed payload builder，并与 adapter 共用闭合的 event/rule/object/state/provenance/backfill/correction validator。`payload.event_at` 是 occurrence time，row `created_at` 是 recorded time；dashboard 优先使用显式 occurrence/backfill，仅对字段缺失的真正 legacy row 回退。动态 prior state 必须在同一 statement 中锁定并成为 update dependency。requested occurrence window 内的 incomplete/noncanonical row 必须被 source 暴露并整体拒绝，不能被 JSON filter 静默隐藏；PostgreSQL JSON timestamp 按原始文本验证 exact UTC RFC3339Nano 与微秒 round-trip，不能借驱动规范化掩盖 offset/亚微秒输入。

`subscription.cost/v1` 使用 `subscription_cost_read_model/v1` 和 `subscription_cost_comparison/v1`。快照固化原币金额与周期、汇率 provider/date/fetched-at/stale、基准币金额、月预算 source/month/currency/limit/warning/status/spend 及实际覆盖；identity conversion 只允许同币种且 rate=1，预算币种必须等于基准币种，任一 missing rate 都强制 budget status=`unknown`。比较除通用 exact compatibility 外还要求原币、基准币和 billing period 语义相同，只返回 allowlisted amount/spend/status/stale/missing-rate delta。

`command.audit/v1` 使用 `command_audit_read_model/v1` 和 `command_audit_comparison/v1`。canonical payload 只含 audit/action/instance/actor identity snapshot、command/sensitivity/event/outcome/source/exit/time，以及 `command_result_retention_seconds=86400`、`command_result_payload_allowed=false`；details、stdout/stderr、output、raw URL、secret assignment 与任意 JSON 永久禁止。比较返回 audit/event/outcome count，不读取或恢复命令输出。

资产历史 adapter 只输出 `asset_history_source/v1` authoritative activity facts，并分别按 event time + stable ID 规范化 renewal decision、price history、IP history 与 VPS spec snapshot；它不实现 `evidence.Kind`，也不注册 `asset.history/*`。

## 3. 时间/精度与敏感分类

默认精度：≤6h 1m、≤48h 5m、≤30d 1h、更长日聚合；单指标≤2000桶、单快照≤50000点/5MiB、峰值≤20。来源只有日聚合时不伪装分钟。

schema字段携带 `normal|sensitive_topology|forbidden`；preview展示选择/剥离/mask，capture二次执行。URL query/userinfo、token/password/key/cookie/env/mount/container ID/fingerprint/raw JSON/stdout/stderr永不进入canonical bytes。

## 4. capture transaction

capture使用显式的两阶段协议，外部source读取和PostgreSQL revision事务不能混在一起：

1. Preview由server重新授权source、规范化selection并调用kind adapter，随后持久化完整preview、preview digest、source digest/watermark和15分钟有效期。intent identity统一使用`evi_`加24位小写hex；Go与SQL必须共享同一闭合grammar并由跨层测试锁定。
2. Revision事务外处理新增capture：重新授权source并重捕获，按规范化后的所有preview-bound字段逐项精确比较persisted preview，包括kind/schema、selection、subject/source identity、requested/actual window、observed time、source revision/watermark/digest、producer/calculation version、units、quality、sensitivity/redaction、precision/bucket、quota/retention和renderer contract。任一漂移、intent过期或source不可用都失败关闭，不能进入revision事务。
3. 重捕获成功后先写content-addressed payload，并校验digest、canonical size和compressed bytes一致；然后只在server内构造immutable prepared capture。prepared capture显式随revision commit传递，不能放入`context.Context`，也不能存入进程singleton/map供participant旁路读取。
4. 已存在的logical snapshot reference在事务外重新授权后构造prepared reference，直接复用snapshot，不重捕获source或复制payload。新capture与existing reference合并为最终有序snapshot ID列表；该列表进入revision canonical content/hash和idempotency fingerprint，客户端不能在prepare后改变顺序或identity。
5. Revision participant只使用传入的`pgx.Tx`。对新capture以`DELETE ... RETURNING`原子消费未过期intent，核对prepared capture的record/kind/schema/preview digest/source digest/selection/snapshot identity后插入logical snapshot；对existing reference核对持久化snapshot identity和授权准备结果；最后按ordinal插入`record_revision_evidence`。任何一步失败都回滚revision、logical snapshot、reference和intent消费，intent row随事务回滚恢复。
6. payload写入发生在revision事务外，所以revision失败或intent过期后payload可以暂时成为orphan。Task 2B提供按`created_at`、全局引用检查和24小时grace period执行的intent expiry/orphan-GC repository primitive与真实PostgreSQL行为测试；Task 7才拥有worker调度、metrics、capacity和alerts。

生产capture/save必须继续经过真实`AdmissionGate`并在gate未接线、typed nil或依赖不可用时失败关闭。Child 10接线前不得提供allow-all production fallback；测试可以使用显式test gate。

事务内participant禁止network、adapter capture或其他外部调用。没有采用durable staging table，也不在revision事务内读取source：前者引入第二套可恢复状态机，后者会让长事务和外部故障进入record authority边界。

## 5. Web renderer

`EvidenceRendererRegistry`以kind/schema显式映射组件；权威库中存在未注册kind/schema会使evidence readiness和普通读取失败关闭，不能由此registry显示“安全fallback”或继续copy/compare/export。外部never-supported schema的allowlisted envelope metadata只由task10在manifest/entry integrity-valid的隔离quarantine/dry-run界面展示，绝不进入本registry。选择器按kind→source→绝对窗口→指标→精度→敏感字段→preview→confirm，不允许客户端提交payload。

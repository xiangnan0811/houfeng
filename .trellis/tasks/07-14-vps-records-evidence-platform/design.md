# 证据注册表与首批适配器设计

## 0. Development rebaseline

`0054` 与其 current APP ACL fragment 是一个原子交付。保留 schema registry、可信来源、敏感分类和 bounded capture；不承担旧数据转换或 staging/release。

## 1. 存储与registry

`0054_create_record_evidence.sql` 创建 `evidence_snapshots`、`evidence_payloads`、`record_revision_evidence`、`evidence_capture_intents`、`evidence_copy_lineage` 与 receipts。payload是确定性canonical bytes的gzip/bytea，digest唯一；logical snapshot保存owner/auth/source/provenance，不共享。

```go
type Kind interface {
	Descriptor() Descriptor
	ValidateSelection(context.Context, ActorScope, Selection) error
	Preview(context.Context, ActorScope, Selection) (Preview, error)
	Capture(context.Context, ActorScope, Intent) (CanonicalSnapshot, error)
	Summarize(CanonicalSnapshot) Summary
	Compare(CanonicalSnapshot, CanonicalSnapshot, Alignment) Comparison
	Export(CanonicalSnapshot, ExportMode) ExportMaterial
}
```

registry拒绝重复kind/version和无conformance metadata。server response 只由kind renderer DTO构建。

## 2. 首批adapter与权威来源

- `ip_quality/v1`：`store/ip_quality.go`的report revision/raw allowlist；固化provider/services、observed_at、stale policy与mask。
- `monitoring_timeseries/v1`：直接查询host/probe raw+aggregate，绝对窗口/覆盖/桶；不复用0-fill sparkline。
- `monitoring_event/v1`：state_change_events/incident metadata，event_at/recorded_at/backfilled/correction。
- `subscription_budget/v1`：subscription current + monthly budget + rate/date/base currency + actual coverage。
- `asset_history/v1`：renewal/price/IP/spec权威历史，保持system activity语义。
- `command_audit/v1`：只允许command/sensitivity/event/source/actor/exit/occurred元数据；output/details永久禁止。

## 3. 时间/精度与敏感分类

默认精度：≤6h 1m、≤48h 5m、≤30d 1h、更长日聚合；单指标≤2000桶、单快照≤50000点/5MiB、峰值≤20。来源只有日聚合时不伪装分钟。

schema字段携带 `normal|sensitive_topology|forbidden`；preview展示选择/剥离/mask，capture二次执行。URL query/userinfo、token/password/key/cookie/env/mount/container ID/fingerprint/raw JSON/stdout/stderr永不进入canonical bytes。

## 4. capture transaction

preview保存source digest/watermark与normalized selection。revision participant在业务事务前重捕获并比较preview；payload先写content-addressed表，事务写logical snapshot+revision ref。事务失败的payload由orphan janitor 24h回收。

## 5. Web renderer

`EvidenceRendererRegistry`以kind/schema显式映射组件；权威库中存在未注册kind/schema会使evidence readiness和普通读取失败关闭，不能由此registry显示“安全fallback”或继续copy/compare/export。外部never-supported schema的allowlisted envelope metadata只由task10在manifest/entry integrity-valid的隔离quarantine/dry-run界面展示，绝不进入本registry。选择器按kind→source→绝对窗口→指标→精度→敏感字段→preview→confirm，不允许客户端提交payload。

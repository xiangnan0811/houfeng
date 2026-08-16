# Evidence Snapshot Contract

> 本规范记录 Child 4 Task 1-4 已落地的 evidence canonical snapshot、authoritative source 与 monitoring-event producer 合同。Task 5 的 API/bootstrap、Task 6 的 Web renderer 和 Task 7 的 worker/capacity 不在本文实现范围内，但接入时必须保持这些边界。

## 1. Scope / Trigger

以下改动必须加载并遵守本规范：

- 新增或修改 `internal/center/evidence/adapters/` 下的 kind adapter、read model、comparison 或 export；
- 修改 `internal/center/store/evidence_task*_sources.go` 的 authoritative source query/scan；
- 修改会被 `monitoring.event/v2` 消费的 `state_change_events` producer；
- 修改 dashboard 对 monitoring event 的发生时间、补传状态或纠错状态解释；
- 新增 kind/schema、producer/rule version、event/state/provenance 或敏感字段语义。

已注册的 evidence kind 只允许：`ip_quality.report/v1`、`monitoring.host/v1`、`monitoring.probe/v2`、`monitoring.event/v2`、`subscription.cost/v1`、`command.audit/v1`。`asset_history_source/v1` 是 source/activity adapter，不是 registry kind，禁止注册 `asset.history/*`。

## 2. Signatures / Data Flow

核心调用链必须保持：

```text
authoritative PostgreSQL rows
  -> typed store source / typed producer payload
  -> adapter ValidateSelection + PreviewCapture/Capture
  -> CanonicalSnapshot
  -> versioned Summarize/Compare/Export
```

关键签名与职责：

- `incidents.ValidMonitoringEventMetadata(...) bool`：writer 与 adapter 共用的闭合事件语义校验器；
- `store.marshalTask4MonitoringEventPayload(task4MonitoringEventPayload) ([]byte, error)`：所有可进入 `monitoring.event/v2` 的 producer 唯一 JSON builder；
- `MonitoringEventSource.LoadMonitoringEventEvidence(...)`、`SubscriptionCostSource.LoadSubscriptionCostEvidence(...)`、`CommandAuditSource.LoadCommandAuditEvidence(...)`：kind adapter 的 typed source 边界；
- `AssetHistorySource.LoadAssetHistory(...)`：只返回 versioned authoritative activity facts；
- 每个 kind adapter 必须实现 `ValidateSelection`、`Authorize`、`PreviewCapture`、`Capture`、`Summarize`、`Compare`、`Export`，且 summary/comparison 只能返回 allowlisted、显式版本化 DTO。

## 3. Contracts / Invariants

### 3.1 Monitoring event producer 与读取

- `payload.event_at` 是 occurrence time；`state_change_events.created_at` 是 recorded time。`recorded_at >= event_at`，两者都必须为 exact UTC、无 monotonic、PostgreSQL 微秒可表示时间。
- incident、monitoring-instance binding、lifecycle、runtime 和 target runtime 五类正常 producer 必须调用共享 typed builder；禁止手写 enriched JSON 或仅靠测试直接插 SQL 构造可读取事件。
- producer 必须保存：`event_at`、`is_backfilled`、`provenance`、`producer_version`、`rule_version`、`prior_state`、`resulting_state`；纠错事件还必须保存合法 `correction_of_event_id`。
- event/rule/object/state/provenance/backfill/correction/legacy-family 的组合由 `ValidMonitoringEventMetadata` 与 builder 失败关闭。新枚举不能靠任意字符串透传。
- 涉及动态 prior state 的状态转换必须在同一 SQL statement 中锁定 prior row，并让 `UPDATE` 依赖该锁定值；不能先读后写或让未被引用的 CTE 假装提供原子性。
- requested occurrence window 内存在 legacy/incomplete/noncanonical row 时，source 必须把它暴露为 rejection candidate 并整体失败关闭，不能通过 JSON filter 静默丢弃。PostgreSQL JSON timestamp 必须按原始文本验证 exact canonical UTC RFC3339Nano 与微秒 round-trip，不能先由驱动规范化后再验。
- dashboard 的过滤、排序、24h count/trend 使用显式 `payload.event_at`；仅对真正 legacy row 回退 `created_at`。显式 `payload.is_backfilled` 是权威值，仅在字段缺失时保留 legacy inference。
- `monitoring_event_read_model/v2` 与 `monitoring_event_comparison/v2` 只做 exact-compatible 比较，并完整报告 event、backfill、correction count 及 delta；metric name 对应的 unit 在同一 capture 内不得漂移。

### 3.2 Subscription cost

- `subscription_cost_read_model/v1` 固化原币、billing period、rate provider/date/fetched-at/stale、base currency/amount、budget source/month/currency/limit/warning/status/spend 与 coverage。
- budget 是全局月份 source，不按当前 VPS 错误缩小 spend；当月无配置时继承最近的先前月份。budget currency 必须等于 base currency。
- 同币种只允许 identity conversion `rate=1`；missing rate 强制 budget status=`unknown`。`spend == limit` 是 over-budget；zero limit 只有在语义不确定时保留 `unknown`，不能伪造健康状态。
- rate date 必须不晚于 fetch time；全部 persisted/custom-source 时间都服从 exact UTC + PostgreSQL 微秒合同。

### 3.3 Command audit

- `command_audit_read_model/v1` 与 comparison 只含 metadata：audit/action/instance/actor identity snapshot、command、sensitivity、event、outcome、source、exit 和时间。
- `details`、stdout/stderr/output、raw JSON、URL query/userinfo、scheme-relative URL、password/token/key/cookie/secret assignment 永久禁止进入 canonical/read model/export。
- event/source tuple 与同一 action 的 actor identity 必须一致；安全校验不能误拒普通 bracketed identity text 或 email username。
- `command_result_retention_seconds=86400`、`command_result_payload_allowed=false` 是闭合合同，不能从 retained command result 恢复输出。

### 3.4 Asset history

- 只输出 `asset_history_source/v1`，按 event time + stable ID 确定性排序 renewal decision、price history、IP history 与 VPS spec snapshot。
- 全部 family 共用一个 `evidence.MaxSnapshotDataPoints` 全局上限；一旦已超过上限立即停止后续 query，不能每个 family 各自获得完整额度。
- custom source 的 slice count 在复制/分配前先做硬上限检查，避免 hostile source 造成无界内存工作。

## 4. Validation Matrix

| Boundary | Accept | Reject / Fail closed |
|---|---|---|
| Event chronology | exact UTC、微秒对齐、`recorded_at >= event_at` | offset、亚微秒、future recorded、倒序时间 |
| Event metadata | 闭合 producer/rule/event/state/provenance 组合 | unknown enum、cross-domain lifecycle、ordinary event 带 correction、manual correction 无 link |
| Event source | 五类正常 writer 可经 reader 到达 | window 内 incomplete/legacy 元数据静默消失、partial metric tuple |
| Cost | base/budget currency 一致，rate chronology 完整 | missing rate 却给确定 budget status、局部 VPS spend 冒充全局 spend |
| Command | allowlisted metadata、稳定 identity | URL/userinfo/query、raw JSON、secret/output/details、非法 event/source tuple |
| Asset history | 四类 facts 合计在全局 cap 内 | 超 cap 后继续 query、复制 hostile oversized slice |
| Canonical ordering | source facts clone 后按稳定键排序 | 依赖数据库或 custom source 当前顺序产生 hash |

## 5. Good / Baseline / Bad

Good：正常业务 writer 通过 typed builder 保存闭合 metadata，真实 PostgreSQL 测试调用公开 writer，再经 source 与 adapter 读取；custom source hostile corpus、summary/compare/export 与 canonical bytes 都有失败关闭断言。

Baseline：legacy dashboard row 可用 `created_at`/旧 backfill inference 展示，但只限字段确实缺失；legacy/incomplete row 不能被 `monitoring.event/v2` 捕获为权威 snapshot。

Bad：测试直接插一行 enriched JSON 证明 reader 绿色，却没有证明生产 writer 可达；SQL 先按 `payload.event_at` 过滤掉缺字段行；先将 offset timestamp 转成 UTC 再验证；command adapter 把 URL 或 JSON 当普通 identity text 放行；asset family 分别应用上限。

## 6. Tests Required

任何相关变更至少运行：

```sh
go test ./internal/center/incidents ./internal/center/evidence/adapters ./internal/center/store -count=1
go test -race ./internal/center/incidents ./internal/center/evidence ./internal/center/evidence/adapters ./internal/center/records ./internal/center/store -count=1
go vet ./...
sh scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationEvidenceSources$' -count=1
```

事件 writer 变更必须保留 `TestMonitoringEventEvidenceIsReachableFromIncidentWriterPath`、`TestMonitoringEventEvidenceIsReachableFromStateControlWriterPaths`、builder fail-closed 矩阵和真实 PostgreSQL 五类 writer-through-reader 覆盖。Task 完成前还需 `make verify-go`、`go test ./... -count=1`、`go mod verify`、`gofmt -d`、`git diff --check HEAD` 与 Trellis task validation。

## 7. Wrong vs Correct

错误：producer 手写 payload，recorded time 被误当 occurrence time，测试绕过生产路径。

```go
payload, _ := json.Marshal(map[string]any{"status": next})
// INSERT state_change_events(..., payload, created_at)
```

正确：producer 从原子 transition 得到 authoritative prior/resulting state 与 DB timestamp，通过共享 builder 生成闭合 payload，`created_at` 单独保存 recorded time。

```go
payload, err := marshalTask4MonitoringEventPayload(task4MonitoringEventPayload{
    ObjectType: incidents.ObjectTypeMonitoringInstance,
    EventType: incidents.EventIncidentStarted,
    Severity: incidents.SeverityAlert,
    EventAt: eventAt, RecordedAt: recordedAt,
    Provenance: monitoringEventProvenanceCenter,
    ProducerVersion: monitoringEventProducerVersion,
    RuleVersion: monitoringEventIncidentRuleVersion,
    PriorState: "normal", ResultingState: "alert",
    IncidentID: "inc_0123456789abcdef",
    IncidentClass: string(incidents.IncidentMonitoringInstanceDiskPressure),
})
if err != nil {
    return err
}
```

错误：只查询 JSON 字段完整的 rows，使窗口内坏数据消失。

```sql
WHERE payload ? 'event_at'
  AND (payload->>'event_at')::timestamptz >= $1
```

正确：查询必须让窗口内 incomplete row 成为显式 rejection candidate；scan 后按原始 timestamp text、metadata completeness 和闭合 domain contract 验证，任一坏 row 使 source 整体失败关闭。

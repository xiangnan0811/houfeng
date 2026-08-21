# Evidence Snapshot Contract

> 本规范记录 Child 4 Task 1-7 已落地的 evidence canonical snapshot、authoritative source、HTTP/save、删除/导出/恢复、Web renderer、logical capacity 与 lifecycle maintenance 合同。

## 1. Scope / Trigger

以下改动必须加载并遵守本规范：

- 新增或修改 `internal/center/evidence/adapters/` 下的 kind adapter、read model、comparison 或 export；
- 修改 `internal/center/store/evidence_task*_sources.go` 的 authoritative source query/scan；
- 修改会被 `monitoring.event/v2` 消费的 `state_change_events` producer；
- 修改 dashboard 对 monitoring event 的发生时间、补传状态或纠错状态解释；
- 修改 `internal/center/http/handlers/evidence.go`、Records 的 `evidence_items` save hook 或 revision response；
- 修改 `internal/center/evidence/{service,deletion_adapter,export_adapter,recovery_adapter}.go`；
- 修改 `internal/center/store/evidence_task5.go`、`evidence_recovery.go` 或 `record_evidence` deletion surfaces；
- 修改 `internal/center/evidence/{capacity,maintenance_worker}.go`、`internal/center/store/evidence.go` 的capacity/backlog query或`record_evidence_participant.go`的final quota check；
- 新增 kind/schema、producer/rule version、event/state/provenance 或敏感字段语义。

已注册的 evidence kind 只允许：`ip_quality.report/v1`、`monitoring.host/v1`、`monitoring.probe/v2`、`monitoring.event/v2`、`subscription.cost/v1`、`command.audit/v1`、`comparison.result/v1`。`asset_history_source/v1` 是 source/activity adapter，不是 registry kind，禁止注册 `asset.history/*`。`comparison.result/v1` 没有 live capture；只由比较另存写入。

## 2. Signatures / Data Flow

核心调用链必须保持：

```text
authoritative PostgreSQL rows
  -> typed store source / typed producer payload
  -> adapter ValidateSelection + PreviewCapture/Capture
  -> CanonicalSnapshot
  -> versioned Summarize/Compare/Export

POST /api/evidence/capture-previews
  -> registry exact kind/schema
  -> project-scoped logical capacity preview
  -> server-owned record/snapshot/intent binding
  -> RevisionPreparer outside revision transaction rechecks capacity
  -> RecordEvidenceRevisionParticipant inside caller pgx.Tx takes project lock and rechecks capacity

GET /api/evidence/{snapshot_id}
  -> opaque snapshot-to-record binding
  -> current record/revision AND current-or-final-source-floor authorization
  -> exact registered kind + canonical reconstruction + authorized payload read
  -> versioned allowlisted Summary DTO
```

关键签名与职责：

- `incidents.ValidMonitoringEventMetadata(...) bool`：writer 与 adapter 共用的闭合事件语义校验器；
- `store.marshalTask4MonitoringEventPayload(task4MonitoringEventPayload) ([]byte, error)`：所有可进入 `monitoring.event/v2` 的 producer 唯一 JSON builder；
- `MonitoringEventSource.LoadMonitoringEventEvidence(...)`、`SubscriptionCostSource.LoadSubscriptionCostEvidence(...)`、`CommandAuditSource.LoadCommandAuditEvidence(...)`：kind adapter 的 typed source 边界；
- `AssetHistorySource.LoadAssetHistory(...)`：只返回 versioned authoritative activity facts；
- `evidence.Service.CapturePreview(...)` / `ReadSnapshot(...)`：transport-neutral preview persistence与授权交集读取；
- `evidence.RevisionPreparer.Prepare(...)`：revision事务外重授权、重捕获、preview drift检查与existing snapshot reuse；
- `evidence.CapacityPolicy` / `CapacityEnforcer`：evidence独立的project logical quota、warning与preview/final evaluation；
- `evidence.MaintenanceWorker`：有界调度existing expired-intent/orphan-GC primitive并发布aggregate observer state；
- `evidence.NewDeletionAdapter(...)`、`NewExportAdapter(...)`、`NewRecoveryAdapter(...)`：删除、kind-owned export和闭合恢复边界；
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

### 3.5 HTTP preview/read 与 Records save

- `POST /api/evidence/capture-previews` 只接受selection，不接受payload、digest、authorization或客户端指定的新snapshot identity。新record的`record_id`由server预分配；Records create只在非空`evidence_items`经`RevisionPreparer`证明intent与该record绑定时接受该ID。
- create/revise请求的`evidence_items`是严格有序tagged union：每项恰有一个`capture_intent_id`或`existing_snapshot_id`。prepared snapshot数量必须等于请求数量；existing identity必须逐位置相等，不能静默丢弃或重排。
- restore从历史revision重建同序existing snapshot items，并在新revision提交前重新授权。空evidence仍构造合法empty preparation，不能绕过participant合同。
- `GET /api/evidence/{snapshot_id}`先取得opaque snapshot-to-record binding，再执行current record/revision与current live source或tombstone final floor权限；只有授权成功后才允许registry解析kind/schema、读取/解压payload并重建canonical snapshot。denied与not-found对外保持opaque，坏payload不能成为权限oracle。
- existing-reference路径必须用`evidence_snapshots` inner join `evidence_payloads`读取encoding/digest/canonical/compressed size等完整metadata并验证split-column绑定，但不得select、解压或复制`compressed_payload`。完整read/export在授权后重新读取payload时必须把两次metadata逐字段精确绑定。
- JSONB round-trip只允许`SourceAuthorization`私有canonical-byte cache被`RestoreSnapshotEnvelopeMetadata`重建；offset time、非canonical public authorization slice顺序或其他可被normalize改变的持久化metadata一律视为corruption，禁止静默修正。
- HTTP response由transport-owned显式DTO构建，只含allowlisted envelope、preview-bound precision/bucket/quota/retention/redaction、`renderer_version`和显式版本化`read_model`；禁止canonical payload、authorization digest、任意metadata或generic JSON fallback。
- production bootstrap已具备closed source resolver与read/reference composition，但真实deployment-membership `AdmissionGate`和witnessed source-deletion authority仍是外部依赖；gate为nil/typed-nil时必须稳定503、零worker/零写，禁止allow-all fallback或仅为演示打开feature。

### 3.6 Deletion、export 与 recovery

- `record_evidence` closed surfaces固定为`evidence_capture_intents`、`evidence_copy_lineage`、`evidence_payload_gc_receipts`、`evidence_payloads`、`evidence_purge_receipts`、`evidence_snapshots`、`record_revision_evidence`。descriptor不能接受占位或额外surface。
- 删除record时只清其revision refs、intents和owned logical snapshots；其他record拥有的显式copy及其lineage必须存活。payload只在全局无snapshot引用时删除。
- deletion receipt和recovery replay必须对相同输入幂等；已提交后响应丢失不能让重试永久冲突。分歧receipt/inventory必须失败关闭，不能`ON CONFLICT DO NOTHING`后假装成功。
- export只能在exact registry lookup、canonical reconstruction和授权成功后调用`kind.Export`；返回bytes仍需runtime forbidden-corpus检查，禁止raw canonical JSON fallback。
- recovery inventory必须深拷贝并完整重放payload、logical snapshot、intent、revision ref、source authorization floor和copy lineage。所有snapshot时间必须exact UTC、无monotonic且PostgreSQL微秒可表示。
- inventory中的每个payload必须被某个待恢复logical snapshot引用；恢复后GC按全局引用判断。`comparison.result/*`仅在具体kind/version已显式注册时可恢复，prefix本身不构成许可。

### 3.7 Capacity、janitor 与 aggregate state

- evidence project capacity默认`10 GiB`、warning从`80%`开始；limit和warning percent是显式、可校验的composition input，不读取或复用attachment quota。
- quota只累计`evidence_snapshots.logical_size_bytes`，project ownership通过`snapshot.record_id -> records.project_id`解析。payload dedup/压缩不能减少logical usage；existing snapshot reuse、text-only revision、reference removal不增加capacity。
- preview在intent持久化前读取project logical usage：`allowed|warning`可持久化，`warning`仍可确认；`exceeded|unavailable`返回server-owned preview但不得写intent。reason使用闭合、无identity的固定文本。
- `RevisionPreparer`在每次recapture后、payload写前重新累计本请求的新capture；quota outcome与持久化preview不精确一致时视为stale。existing reference完全豁免capacity read。
- revision participant只对new captures执行final check：先解析record project，取得project-scoped PostgreSQL transaction advisory lock，再重新读取logical sum；任何stale/exceeded/unavailable在消费intent前失败并使整个revision回滚。并发revision不能共同越过limit，loser intent保留。
- exact limit可接受且状态为`warning`；`limit+1`拒绝。所有加法、数据库signed值和policy边界必须防overflow/negative并失败关闭。
- janitor只按固定stage调度既有`DeleteExpiredCaptureIntents(limit)`与`CollectUnreferencedPayloads(limit)`，batch/probe最大100；orphan eligibility使用PostgreSQL transaction time与24h grace，删除前仍做global snapshot reference check。repository返回数量不得超过请求limit，GC receipt必须是非零digest、exact UTC微秒时间与有界非零bytes且同一批内唯一；不一致结果失败关闭。中途context取消立即停止后续stage，不制造failure alert，但已完成cleanup仍进入aggregate counters。
- metrics/alerts只暴露bounded aggregate count/bytes/status/timestamps/backlog，不带project ID、record/snapshot/intent ID、digest、payload或dependency error文本；observer copy和并发访问必须`-race`安全，相同alert state不增长cardinality，恢复到normal必须可见。
- production nil/typed-nil `AdmissionGate`时bootstrap不注册maintenance run/log loop；Child 10提供真实gate后才组合worker。不得用allow-all/no-op gate制造假GREEN，Evidence capture/save feature仍保持关闭。

### 3.8 横向比较工作台

- 比较编排放在 `internal/center/evidence/`。`evidence` 不得 import `records`；比较路径不得 import `activity` / `recordsearch`。HTTP 只解析 candidate/compare；另存复用 records 事务与 `evidence_copy_lineage`。
- `HOUFENG_COMPARISON_ENABLED` 默认 false，叠在 `HOUFENG_RECORDS_ENABLED` 上。capability 关闭时 route 与 handler 同时关闭。已保存的 `comparison.result/v1` 一旦注册必须仍可被 registry 读取。
- `POST /api/evidence/comparison-candidates` 只接受 2–6 个 `vps|monitoring_instance|target` 与绝对 UTC 窗口；返回授权安全的 snapshot/revision 候选。不 capture、不签发 intent。缺失 subject 对外统一 404，不泄露计数。
- `POST /api/evidence/comparisons` 只接受 2–6 个 snapshot XOR `record_id+revision_id`。客户端不得提交 payload、单位转换或已计算差值。无 `Detail` 返回 review；有 `Detail` 只追加一个 kind/metric。
- 非时序 kind 复用 pairwise `Kind.Compare(..., AlignmentExact)`，`item_index` 是真实 compared item。`actual_coverage` series / trend / matrix 只服务 `monitoring.host/v1` 与 `monitoring.probe/v2`：`AlignActualCoverage` 必须把 nearest 一对一匹配接到 series 与逐桶差，尊重 `BaselineIndex`，对每个非基准项配对；测试必须断言配对改变输出。空 metric 记 `metric_missing`，不得默默返回空图。当前 kind 的 `common_overlap` 只返回 blocked reason，不实现重聚合。
- 任一 `metadata_only` 只跳过该 item 的 payload，不得短路整次数值计算。授权失败省略 snapshot（对外 404）；授权后 payload/hash 失败标 `Unreadable` 且不签发 intent。
- `comparison.result/v1` 只固化系统可验证选择、条件、差异和告警，使用派生 envelope，不整份拷贝基准机器 Source/Authorization。无快照拷贝时仍写 result（只存条件与告警）。人工标题与 conclusion 留在 revision Markdown。kind 实现 Summarize / Compare(self/exact) / Export；无 live capture。
- 另存走现有 `POST /api/records` / revisions + `Idempotency-Key` + `comparison_intent`。客户端不得发送 `evidence_items` 或自造 copied IDs。participant `Name() == "comparison"` 排在 `collaboration` 与 `evidence` 之间；copy 使用新 logical ID + lineage，source mutation = 0。HMAC claims 必须带上修订 `record_id`/`revision_id`、chosen snapshot 和 compare 时的 Detail；另存重算不得把修订项降级成 snapshot-only。HMAC 仍有效但过期的 intent 必须能重建同一 copy/result identity，以便同 key 重试到达 idempotent replay。
- HMAC intent purpose 为 `comparison-save/v1`，独立 0400 regular file、`O_NOFOLLOW`，不复用 deletion/backup/session key。`HOUFENG_COMPARISON_ENABLED=true` 时 keyring 与 key id 必填。unreadable / hash / copy blocker 不签发 intent；签发失败不得保持 eligible 且无 token。
- 进程内 weighted admission：满额时 `Wait` 直到有空位或 `ctx.Done()` 后 429 `comparison_capacity_exhausted`，越界 422 `comparison_request_memory_limit`。4 GiB cgroup / mixed-load harness 不在本 child。

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
| Preview/save | server-owned ID、ordered tagged union、prepared count/identity精确一致 | 客户端payload、空preparer吞掉非空items、重排snapshot |
| Read | record+source授权先于kind/payload、exact registry、strict metadata/payload binding、versioned allowlist | denied actor区分unknown/corrupt payload、metadata-only读取payload bytes、静默normalize持久化envelope、raw payload/authorization/generic JSON |
| Deletion/export | owned logical rows删除、global-ref payload GC、`kind.Export` | 删除其他copy、raw JSON export、不可重试receipt |
| Recovery | deep-cloned reachable inventory、exact timestamp、同输入幂等 | orphan payload、浅拷贝TOCTOU、prefix kind放行、分歧replay |
| Capacity | project logical bytes、warning可确认、exact boundary、existing ref豁免 | attachment quota、physical dedup折扣、stale/exceeded/unavailable写intent或snapshot |
| Janitor/metrics | bounded DB-time cleanup、global-ref GC、aggregate defensive snapshot | unbounded scan、local-time age、identity/digest/payload label、nil gate log loop |
| Comparison workbench | 2–6 authorized items、host/probe-only series、HMAC intent、copy-lineage save、capability default-off | generic JSON/Markdown 抽指标、source mutation、human conclusion 进 derived evidence、4 GiB harness 冒充本 child 退出门 |

## 5. Good / Baseline / Bad

Good：正常业务 writer 通过 typed builder 保存闭合 metadata，真实 PostgreSQL 测试调用公开 writer/revision participant/RecoveryAdapter，再经 source 与 adapter 读取；custom source hostile corpus、summary/compare/export 与 canonical bytes 都有失败关闭断言。删除与恢复的相同请求可在ambiguous commit后安全重试；并发new capture经project lock最多一个越过剩余容量，loser完整回滚并保留intent。

Baseline：legacy dashboard row 可用 `created_at`/旧 backfill inference 展示，但只限字段确实缺失；legacy/incomplete row不能被`monitoring.event/v2`捕获为权威snapshot。Child 10接线前production Evidence/带证据save稳定503，但注入依赖的domain/handler合同与Task 6 DTO可独立测试。

Bad：测试直接插一行 enriched JSON 或logical snapshot证明reader绿色，却没有证明生产writer/participant可达；SQL先按`payload.event_at`过滤掉缺字段行；先将offset timestamp转成UTC再验证；preparer返回空结果却提交非空evidence请求；删除/恢复首次commit后无法重试；export或renderer回退任意JSON；按compressed payload或attachment余量批准capture，或nil gate下启动反复报错的janitor。

## 6. Tests Required

任何相关变更至少运行：

```sh
go test ./internal/center/incidents ./internal/center/evidence/adapters ./internal/center/store -count=1
go test -race ./internal/center/incidents ./internal/center/evidence ./internal/center/evidence/adapters ./internal/center/records ./internal/center/store -count=1
go vet ./...
sh scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationEvidenceSources$' -count=1
sh scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^(TestPostgresIntegrationEvidenceTask5|TestPostgresIntegrationRecordEvidenceRevisionParticipant)' -count=1
sh scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationEvidenceCapacity' -count=1
go test ./internal/center/evidence -run '^TestEvidenceMaintenance' -count=10
go test -race ./internal/center/evidence ./internal/center/store ./cmd/houfeng-center -count=1
go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store ./internal/center/http/handlers -run 'Comparison|Comparability|Alignment' -count=10
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

错误：收到非空`evidence_items`后只检查preparation本身合法，允许错误preparer返回valid-empty并继续提交；删除或恢复重试直接再次INSERT immutable row。

```go
prepared, _ := preparer.Prepare(ctx, actor, request)
return application.CreateRevision(ctx, commandWith(prepared))
```

正确：transport核对prepared snapshot数量和existing identity的逐位置一致性；持久化适配器先读取并语义验证既有receipt/replayed rows，相同输入返回同一结果，分歧输入失败关闭。

```go
if len(prepared.SnapshotIDs()) != len(request.Items) {
    return ErrEvidenceServiceUnavailable
}
for i, item := range request.Items {
    if item.ExistingSnapshotID != "" && prepared.SnapshotIDs()[i] != item.ExistingSnapshotID {
        return ErrEvidenceServiceUnavailable
    }
}
```

# 证据快照合同

## VPS 生命周期投影

- 新捕获的资产事实只接受 active/archived 生命周期和 unreviewed/keep/cancel 续费意向；用途为自由 `usage_tags[]`。历史证据是当时快照，不随当前对象或关联变化重写。
- 当前成本证据只计管理中资产；归档潜在扣费独立展示，缺少订阅或汇率的金额保持未知，不按零计。
- 服务/域名归属由带起止时间的关联记录读取；结束一台 VPS 的关联不会改变共享对象身份或其他 VPS 的当前事实。

## Scenario: Evidence production read/reference authorization 与 metadata binding

- `PostgresEvidenceRepository`的ordinary read/export/existing-reference必须先经真实`AdmissionGate`读取opaque record binding并完成current record/source授权，随后才解析registry kind/schema或读取payload bytes；denied actor不能用unknown kind、坏envelope或坏gzip区分资源存在性。
- existing-reference SQL必须inner join `evidence_payloads`并校验payload encoding、digest、canonical size和compressed size，但query不得选择`compressed_payload`。ordinary read/export仅在授权后读取/解压payload，并把授权前metadata与payload read返回的全部split-column metadata精确比较。
- JSONB解码后可重建的唯一非持久化状态是`SourceAuthorization`内部canonical-byte cache；所有公开authorization字段、时间、envelope字段和split digest/size必须保持canonical exact，不能靠normalize修复corruption。
- 真实PostgreSQL测试必须从participant写入logical snapshot，再经public read/service/export/existing-reference路径读回；missing payload、unknown schema、permission intersection与corrupt metadata/payload都必须失败关闭，不允许仅插fixture绕过writer。
- C81 的 `record_evidence_payload_insert` 只接受经受信 Go 校验的 canonical bytes 与确定性 gzip；SQL 独立计算 canonical digest，重核当前 actor、record fence 和 native intent/allocation/source，或 exact import job/plan/record/evidence owner mapping 与当前 birth。runtime 不得直接 INSERT payload。SQL 不解压 gzip，也不校验 comparison token 的 HMAC；受信 Go 必须在同一效果事务内完成相应授权。
- 完整 canonical read、comparison dependency 证明、evidence 删除 preview/receipt 与 orphan GC receipt 在产生正向结果前必须解压实际保存的 gzip，按完整 canonical bytes 重算 digest；缺失、损坏或内容不匹配一律失败关闭。metadata-only existing-reference 只证明其 metadata 绑定，不构成 canonical 内容证明。

## Scenario: Evidence project logical capacity 与 lifecycle maintenance

### 1. Scope / Trigger

- Trigger：修改`evidence_snapshots.logical_size_bytes`的核算、`PostgresEvidenceRepository` capacity/backlog query、`RecordEvidenceRevisionParticipant` final quota check、`EvidenceMaintenanceWorker` composition或lifecycle primitive scheduling时。
- capacity 核算继续使用已应用的`0054_create_record_evidence.sql`，不新增counter/settings migration、不修改0054、不读取attachment quota/table。comparison dependency 派生索引及其维护使用forward `0081_record_deletion_successor.sql`，与容量事实分开。

### 2. Signatures

```go
func (r *PostgresEvidenceRepository) ReadProjectEvidenceCapacity(context.Context, string) (evidence.ProjectCapacityUsage, error)
func (r *PostgresEvidenceRepository) ReadEvidenceCapacityAggregate(context.Context) (evidence.EvidenceCapacityAggregate, error)
func (r *PostgresEvidenceRepository) ReadEvidenceLifecycleBacklog(context.Context, uint64) (evidence.EvidenceLifecycleBacklog, error)
func NewRecordEvidenceRevisionParticipantWithCapacityPolicy(evidence.CapacityPolicy) (records.RevisionParticipant, error)
```

### 3. Contracts

- project logical usage是`sum(evidence_snapshots.logical_size_bytes)`，必须通过`evidence_snapshots.record_id = records.record_id`并按`records.project_id`过滤。physical stats只对该project引用的distinct payload计一次；global orphan stats独立报告，不能抵扣logical usage。
- project ID服从`recordauth`闭合registry；unknown project在SQL前拒绝。全部capacity/backlog read仍需真实`AdmissionGate`，nil/error/typed-nil gate在query/exec前失败关闭。
- final new-capture check与revision write共用caller transaction：解析record project后取得`pg_advisory_xact_lock(hashtextextended('houfeng.evidence.capacity.project.v1:' || project_id, 0))`等价的稳定project lock，再执行fresh logical sum。lock必须早于intent `DELETE ... RETURNING`和snapshot insert。
- transaction采用PostgreSQL READ COMMITTED statement snapshot；等待project lock后执行的sum必须看到先前winner commit。preview-boundquota发生任何status/reason漂移、limit超出或read失败都回滚revision、snapshot、reference与intent消费。
- existing snapshot reference不创建logical row，因此不得取得capacity lock或查询usage。text-only、reference removal和继续reuse在warning/exceeded状态仍可用。
- backlog query只探测`limit+1`并返回capped count+more bit；limit为`1..100`。expired intent以`valid_until <= transaction_timestamp()`判定；eligible orphan以`created_at <= transaction_timestamp()-24h`且global `NOT EXISTS evidence_snapshots`判定。
- GC candidate与最终DELETE都必须做global reference check；返回给worker的receipt只被聚合为count/canonical/compressed bytes，不能成为高cardinality metric label。每个bounded primitive返回行数不得超过传入limit；receipt的version/receipt digest、deleted-at UTC微秒时间及bytes必须闭合有效且批内唯一，否则worker按janitor failure失败关闭。

### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| projected bytes `< warning` | `allowed`；可persist intent/snapshot |
| projected bytes `>= warning`且`<= limit` | canonical `warning`；仍可确认并persist |
| projected bytes `> limit` | stale/conflict；revision回滚，intent保留 |
| 两个同project transaction各自单独可fit但合计超限 | project lock序列化；恰好一个成功 |
| existing ref / empty capture list | 0 capacity lock/query，正常保存 |
| capacity row负数/计数字节不一致/sum cast失败 | persistence conflict/capacity unavailable，不提交 |
| nil/typed-nil production gate | 不注册maintenance worker；0 cleanup pass、0 log loop |

### 5. Tests Required

```bash
go test ./internal/center/evidence ./internal/center/store ./cmd/houfeng-center -count=1
go test -race ./internal/center/evidence ./internal/center/store ./cmd/houfeng-center -count=1
sh scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store -run '^TestPostgresIntegrationEvidenceCapacity' -count=1
```

Strict PostgreSQL输出必须实际运行exact-boundary、over-boundary rollback/intent preservation和concurrent oversubscription tests；任何`SKIP`不构成evidence。既有24h orphan/global-reference lifecycle integration test必须同样通过。

---

> 本规范维护 evidence canonical snapshot、authoritative source、HTTP/save、删除/导出/恢复、Web renderer、logical capacity 与 lifecycle maintenance 合同。

## 1. Scope / Trigger

以下改动必须加载并遵守本规范：

- 新增或修改 `internal/center/evidence/adapters/` 下的 kind adapter、read model、comparison 或 export；
- 修改 `internal/center/store/evidence_task*_sources.go` 的 authoritative source query/scan；
- 修改会被 `monitoring.event/v2` 消费的 `state_change_events` producer；
- 修改 dashboard 对 monitoring event 的发生时间、补传状态或纠错状态解释；
- 修改 `internal/center/http/handlers/evidence.go`、Records 的 `evidence_items` save hook 或 revision response；
- 修改 `internal/center/evidence/{service,deletion_adapter,export_adapter,recovery_adapter}.go`；
- 修改 `internal/center/store/evidence_task5.go` 或 `record_evidence` deletion surfaces；
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
- `evidence.NewDeletionAdapter(...)`、`NewExportAdapter(...)`：删除和 kind-owned export；当前没有获认证的 evidence 恢复 writer 或 production recovery adapter；
- 每个 kind adapter 必须实现 `ValidateSelection`、`Authorize`、`PreviewCapture`、`Capture`、`Summarize`、`Compare`、`Export`，且 summary/comparison 只能返回 allowlisted、显式版本化 DTO。

## 3. Contracts / Invariants

### Monitoring host CPU 有效覆盖（calculation v2）

- 仅 `monitoring.host/v1` 的新捕获采用 `monitoring-evidence/v2`；KindKey/read-model version 不变，host read model 增加 envelope 的 `calculation_version`。probe 继续 `monitoring-evidence/v1`、原 series gaps、payload、quality 和整日来源选择，不扩展 probe 合同。
- raw CPU 三指标只从整组可用行取值和计数/时间/版本；daily CPU 使用 nullable CPU 有效计数（legacy 各自回退 host 计数），计数零、average/max 缺失或不在 `[0,100]` 不出该指标行。禁止零填充，所选指标全无可接受值时不创建假空快照。
- host 来源选择按 `(series, UTC day, metric)`，仅 daily 的该指标计数大于剩余 raw 时替换该指标；小于 24h precision 不选 daily。内存触发 daily 不得删除仍合法的 raw CPU。每个 series/time 仍只有一个 bucket。
- 每个 metric 保存 `sample_count`、`maintenance_count`、`backfilled_count`、`source_layer`、`source_granularity_seconds`；计数必须正，子计数不超过总数，来源只能 raw/daily_aggregate，granularity 是 `(0,ActualPrecision]` 的整数秒。peak 使用所属 metric 来源。
- bucket 的共享三计数是 SampleCount 最大 metric 的整组三计数，平手取 name 字典序最小者；这是“覆盖参考样本”，不宣称异源样本的精确并集或 CPU 有效数。quality 累计参考数。bucket 来源相同则原来源，不同为 mixed，granularity 取指标最大值；新 capture 严格重算校验该关系。
- host gap 包含 `metric`，按实际 series 与 selection.Metrics 检查整窗，包含从未出现的指标；按 series/metric/start 稳定排序。任何 metric gap 令 quality.partial=true，gap_count 计指标缺口，不能用正常内存掩盖 CPU 缺测。
- v2 比较按 metric.sample_count 累计 count 和加权 average，不允许缺字段回退 bucket count；min/max 与 mean-bucket-p95 算法不变。v1-v2 比较经 `calculation_version_incompatible` 明确拒绝；旧 v1-v1 仍可比较、读取及导出，已存快照不重写，新增字段由 descriptor allowlist 支持导出。

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
- 预览错误必须可分类，不得把正常业务情况落成 `500 internal_error`：来源不存在、已删除、已退役或无权采集（含 `Authorize` 拒绝，统一包成 `evidence.ErrSourceNotFound`，resolver 用 `%w` 保留内层链）对外为 opaque `404 resource_not_found`；来源类型未知或来源 ID 不合法（`records.ErrInvalidSubjectReference`）是选择错误 `422 evidence_invalid`；解析结果与请求不一致（摘要、种类、ID、身份或项目不符）是完整性问题，不归入以上两类；内层是主体服务暂不可用（`store.ErrRecordSubjectUnavailable`）时为 `503 evidence_service_unavailable`；所选窗口内没有数据（监控零桶、事件/审计零条、无 IP 质量报告、无订阅账单或预算）为 `422 evidence_source_empty`；桶数或数据点超过单份上限（含 PostgreSQL loader 读到第 `MaxSnapshotDataPoints+1` 条时的提前终止、事件“事件数 + 指标数”超限）为 `422 evidence_window_too_large`。计数与条目不一致始终是完整性错误，先于“无数据 / 超上限”判定。选择参数本身非法仍是 `422 evidence_invalid`。
- 带证据的 Records save 在发布时重新采集；来源已不可访问、窗口已无数据、超出上限或证据持久化冲突都说明预览不再成立，统一为 `409 evidence_preview_stale`，且必须先于 `ErrDenied -> 404` 判定（来源错误链带着内层拒绝，误判 404 会让客户端以为记录被撤销）；主体服务或证据容量暂不可用为 `503 record_service_unavailable`；`existing_snapshot_id` 找不到为 `422 record_invalid`。
- ranked resource discovery 与最终 intent 消费保持相同的持久化冲突分类：intent 缺失、已消费或其持久绑定漂移均为 `409 evidence_preview_stale`；真实数据库故障保留原错误链，不伪装成预览冲突。
- production bootstrap已具备closed source resolver与read/reference composition，但真实deployment-membership `AdmissionGate`和witnessed source-deletion authority仍是外部依赖；gate为nil/typed-nil时必须稳定503、零worker/零写，禁止allow-all fallback或仅为演示打开feature。

### 3.6 Deletion、export 与 recovery

- `record_evidence` closed surfaces固定为`evidence_capture_intents`、`evidence_copy_lineage`、`evidence_payload_gc_receipts`、`evidence_payloads`、`evidence_purge_receipts`、`evidence_snapshots`、`record_revision_evidence`。descriptor不能接受占位或额外surface。
- 删除record时只清其revision refs、intents和owned logical snapshots；其他record拥有的显式copy及其lineage必须存活。payload只在全局无snapshot引用时删除。
- deletion receipt必须对相同输入幂等；已提交后响应丢失不能让重试永久冲突。分歧receipt必须失败关闭，不能`ON CONFLICT DO NOTHING`后假装成功。
- export只能在exact registry lookup、canonical reconstruction和授权成功后调用`kind.Export`；返回bytes仍需runtime forbidden-corpus检查，禁止raw canonical JSON fallback。
- inventory 的纯校验要求每个payload被待恢复logical snapshot引用，source authorization、copy lineage和所有snapshot时间保持一致且时间为exact UTC、无monotonic、PostgreSQL微秒可表示。`comparison.result/*`仅在具体kind/version已显式注册时通过校验，prefix本身不构成许可。
- 完整 authenticated maintenance recovery 仍未交付，runtime 不接受任意 inventory 的重放，也不将纯校验或无保护的 orphan sweep 作为恢复能力。将来实现必须闭合 source/birth 准入、writer fencing、copy lineage、恢复后全局引用检查与幂等重放，再声明 readiness。

### 3.7 Capacity、janitor 与 aggregate state

- evidence project capacity默认`10 GiB`、warning从`80%`开始；limit和warning percent是显式、可校验的composition input，不读取或复用attachment quota。
- quota只累计`evidence_snapshots.logical_size_bytes`，project ownership通过`snapshot.record_id -> records.project_id`解析。payload dedup/压缩不能减少logical usage；existing snapshot reuse、text-only revision、reference removal不增加capacity。
- preview在intent持久化前读取project logical usage：`allowed|warning`可持久化，`warning`仍可确认；`exceeded|unavailable`返回server-owned preview但不得写intent。reason使用闭合、无identity的固定文本。
- `RevisionPreparer`在每次recapture后、payload写前重新累计本请求的新capture；quota outcome与持久化preview不精确一致时视为stale。existing reference完全豁免capacity read。
- revision participant只对new captures执行final check：先解析record project，取得project-scoped PostgreSQL transaction advisory lock，再重新读取logical sum；任何stale/exceeded/unavailable在消费intent前失败并使整个revision回滚。并发revision不能共同越过limit，loser intent保留。
- exact limit可接受且状态为`warning`；`limit+1`拒绝。所有加法、数据库signed值和policy边界必须防overflow/negative并失败关闭。
- janitor只按固定stage调度既有`DeleteExpiredCaptureIntents(limit)`与`CollectUnreferencedPayloads(limit)`，batch/probe最大100；orphan eligibility使用PostgreSQL transaction time与24h grace，删除前仍做global snapshot reference check。repository返回数量不得超过请求limit，GC receipt必须是非零digest、exact UTC微秒时间与有界非零bytes且同一批内唯一；不一致结果失败关闭。中途context取消立即停止后续stage，不制造failure alert，但已完成cleanup仍进入aggregate counters。
- metrics/alerts只暴露bounded aggregate count/bytes/status/timestamps/backlog，不带project ID、record/snapshot/intent ID、digest、payload或dependency error文本；observer copy和并发访问必须`-race`安全，相同alert state不增长cardinality，恢复到normal必须可见。
- production nil/typed-nil `AdmissionGate` 时 bootstrap 不注册 maintenance run/log loop。生产已通过 `cmd/houfeng-center/bootstrap.go` 的 membership admission gate 装配 Evidence capture/save 与 maintenance；是否可用仍由 Records 开关、真实 membership 和当前准入决定。不得用 allow-all/no-op gate 制造假 GREEN。

### 3.8 横向比较工作台

- 比较编排放在 `internal/center/evidence/`。`evidence` 不得 import `records`；比较路径不得 import `activity` / `recordsearch`。HTTP 只解析 candidate/compare；另存复用 records 事务与 `evidence_copy_lineage`。
- `HOUFENG_COMPARISON_ENABLED` 默认 false，叠在 `HOUFENG_RECORDS_ENABLED` 上。capability 关闭时 route 与 handler 同时关闭。已保存的 `comparison.result/v1` 一旦注册必须仍可被 registry 读取。
- `POST /api/evidence/comparison-candidates` 只接受 2–6 个 `vps|monitoring_instance|target` 与绝对 UTC 窗口；返回授权安全的 snapshot/revision 候选。不 capture、不签发 intent。缺失 subject 对外统一 404，不泄露计数。
- `POST /api/evidence/comparisons` 只接受 2–6 个 snapshot XOR `record_id+revision_id`。客户端不得提交 payload、单位转换或已计算差值。无 `Detail` 返回 review；有 `Detail` 只追加一个 kind/metric。
- 非时序 kind 复用 pairwise `Kind.Compare(..., AlignmentExact)`，`item_index` 是真实 compared item。`actual_coverage` series / trend / matrix 只服务 `monitoring.host/v1` 与 `monitoring.probe/v2`：`AlignActualCoverage` 必须把 nearest 一对一匹配接到 series 与逐桶差，尊重 `BaselineIndex`，对每个非基准项配对；测试必须断言配对改变输出。空 metric 记 `metric_missing`，不得默默返回空图。当前 kind 的 `common_overlap` 只返回 blocked reason，不实现重聚合。
- 任一 `metadata_only` 只跳过该 item 的 payload，不得短路整次数值计算。授权失败省略 snapshot（对外 404）；授权后 payload/hash 失败标 `Unreadable` 且不签发 intent。
- 每项另有授权来源 sidecar `source_record_id`：snapshot-only 从本次授权后加载的 snapshot owner 取得；metadata-only 从已授权修订取得。它不是基础设施 `subject_id`，也不把 snapshot-only 改成 revision-bound；有修订上下文时仍保留独立 `record_id`/`revision_id`。HTTP evaluation 与 HMAC claims 携带该 sidecar，另存重读并逐项核对；缺失来源的旧 intent 或 owner 不匹配返回 `comparison_intent_stale`，不猜测或补造授权。
- 此 sidecar 不进入历史 canonical comparison payload / digest，不改已有 B 的正文、payload 或 lineage。它是 G2 派生 dependency index 与浏览器失效所需的来源输入；新增来源字段本身不证明历史索引补齐或 comparison adapter readiness。
- C81 native save 的 dependency index 在同一 revision 事务写入：comparison participant 先验证签名 claims 与实际 canonical items 的逐项绑定，并拒绝准备对象篡改 owner；evidence participant 插入 result 后写完整 source identity 集合及绑定 payload digest 的 completion。metadata-only 也产生 `(source_record_id, source_revision_id, NULL)`，不能把无 payload 当无依赖。源身份没有 live-source FK，删除源不改写这些历史字符串。
- comparison result 的真实本地 copy 可继承其源 result 的认证索引，但必须重新验证 canonical bytes、源 completion 的 result/digest 绑定与每项完整覆盖；缺行、多行、归属歧义或 digest 不符均不完成索引。destination record 不是原始 source owner。导入只验证 canonical integrity 不足以证明嵌入 ID 属于本地，禁止按 ID 碰撞或“没有 import mapping”猜来源；无法证明时保留未完成状态，不能发零依赖 completion。历史/recovery 批处理与 deletion readiness 必须同样遵守这一边界。
- maintenance 按 snapshot ID 的稳定 lexical cursor 每批最多100个实际 comparison result 做可重放补索引，轮末从头重新扫描；没有全局 done 位。canonical 不合法、版本不支持或来源无法认证的结果不写 completion，也不阻塞同一批其他可证明结果的检查。恢复须在 references/lineage 全部写入后做相同判定；unkeyed 派生 ID 和 recovery inventory 自报 lineage 不是 native-origin 认证。
- 可认证的历史 copy 必须由已有完整索引的 parent result 在同一 owner revision 中绑定其真实 copied ID/hash/kind，再核对 lineage、source owner 及 source result 的认证依赖集合。只修复派生 index/completion，不改 canonical bytes、正文或 lineage。独立 readiness probe 使用一个 REPEATABLE READ READ ONLY admitted snapshot，逐结果验证真实 canonical 和完整依赖覆盖；任何未完成结果返回`comparison_dependency_index_incomplete`，不能凭维护 cursor 或总行数宣布完整。
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
| Preview/save 错误 | 来源不可访问 404、主体服务不可用 503、窗口无数据 422 `evidence_source_empty`、超上限 422 `evidence_window_too_large`；发布时重新采集失败 409 `evidence_preview_stale` | 正常业务情况落成 500、`%v` 丢掉内层错误链、发布时来源拒绝被映射成 404 |
| Read | record+source授权先于kind/payload、exact registry、strict metadata/payload binding、versioned allowlist | denied actor区分unknown/corrupt payload、metadata-only读取payload bytes、静默normalize持久化envelope、raw payload/authorization/generic JSON |
| Deletion/export | owned logical rows删除、global-ref payload GC、`kind.Export` | 删除其他copy、raw JSON export、不可重试receipt |
| Recovery | inventory 纯校验、exact timestamp、显式 kind/version | 普通 runtime 任意写入恢复 inventory、将纯校验当作恢复 readiness |
| Capacity | project logical bytes、warning可确认、exact boundary、existing ref豁免 | attachment quota、physical dedup折扣、stale/exceeded/unavailable写intent或snapshot |
| Janitor/metrics | bounded DB-time cleanup、global-ref GC、aggregate defensive snapshot | unbounded scan、local-time age、identity/digest/payload label、nil gate log loop |
| Comparison workbench | 2–6 authorized items、host/probe-only series、HMAC intent、copy-lineage save、capability default-off | generic JSON/Markdown 抽指标、source mutation、human conclusion 进 derived evidence、4 GiB harness 冒充本 child 退出门 |

## 5. Good / Baseline / Bad

Good：正常业务 writer 通过 typed builder 保存闭合 metadata，真实 PostgreSQL 测试调用公开 writer/revision participant，再经 source 与 adapter 读取；custom source hostile corpus、summary/compare/export 与 canonical bytes 都有失败关闭断言。删除相同请求可在ambiguous commit后安全重试；并发new capture经project lock最多一个越过剩余容量，loser完整回滚并保留intent。

Baseline：legacy dashboard row 可用 `created_at`/旧 backfill inference 展示，但只限字段确实缺失；legacy/incomplete row 不能被 `monitoring.event/v2` 捕获为权威 snapshot。生产 gate 已接线；gate 不可用时 Evidence/带证据 save 必须失败关闭为 503，不能用注入依赖的 domain/handler 测试代替真实准入证据。

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

事件 writer 变更必须保留 `TestMonitoringEventEvidenceIsReachableFromIncidentWriterPath`、`TestMonitoringEventEvidenceIsReachableFromStateControlWriterPaths`、builder fail-closed 矩阵和真实 PostgreSQL 五类 writer-through-reader 覆盖。交付前还需 `make verify-go`、`go test ./... -count=1`、`go mod verify`、`gofmt -d` 与 `git diff --check HEAD`。

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

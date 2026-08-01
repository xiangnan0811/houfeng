# 横向比较工作台设计

## 1. 模块边界与持久化决策

comparison 属于 `internal/center/evidence/`，因为只有 evidence kind registry 能判断 schema、单位、覆盖与重聚合是否兼容；不新建平行 backend package，也不读取 raw source tables。HTTP 只解析 candidate/compare 请求，store 只批量解析 immutable revision/snapshot refs，Web 只渲染服务端 allowlisted result。

本任务没有 migration。交互运行不持久化；登录内分享通过 versioned URL state 重放。另存记录复用 task 2 的 `CreateRevisionCommand`/material-intent hook 和 task 4 的 `evidence_snapshots`、payload、copy lineage。唯一新持久语义是 registry kind `comparison.result/v1`，使用已有 evidence schema。

直接依赖为 2、4、5、7；task 6 由 task 7 传递。task 7 提供 subject evidence 精确 snapshot 入口与相同授权/identity 语义，但 comparison backend 仍只依赖 records/evidence 接口，避免 search/activity 反向进入 evidence。

## 2. 两阶段选择合同

### 2.1 Subject candidates

`POST /api/evidence/comparison-candidates`：

```go
type CandidateRequest struct {
	Subjects        []SubjectRef // 2..6, registry allowlist only
	RequestedWindow TimeRange    // absolute UTC
	Kinds           []KindRef    // empty means all readable kinds
}
```

service 一次批量查询每个 subject 在窗口附近的 authorized snapshots/revision refs，按 kind/schema compatibility、actual-window distance、quality、captured time 和 stable ID 确定排序。响应逐 subject/kind 返回候选 ID、fixed revision、schema/hash、窗口、质量和推荐原因；不会创建 capture intent/snapshot，也不会把推荐直接送进 compare。缺失 subject 对外统一 404，不返回其他 selection 的计数推断。

### 2.2 Fixed comparison

`POST /api/evidence/comparisons` 只接受 fixed selection：

```go
type CompareRequest struct {
	Items           []FixedItem // 2..6, ordered
	BaselineIndex   int
	Alignment       AlignmentMode // actual_coverage|common_overlap
	RequestedWindow TimeRange
	Tolerance       time.Duration
	BucketWidth     *time.Duration
	Detail          *DetailRequest // one kind + optional metric
}

type FixedItem struct {
	SnapshotID *string
	Revision   *FixedRevision
}

type FixedRevision struct {
	RecordID           string
	RevisionID         string
	ChosenSnapshotIDs  []string
}
```

每个 item 必须恰好是 snapshot 或 revision。revision ID 必须属于 record；service 只展开该 immutable revision 的 refs，并读取该revision权威保存的 `record_type/business_status/status_group/impact/occurred_at`，形成 `RevisionMetadataSnapshot`。多个同 kind snapshot 若未在 `ChosenSnapshotIDs` 唯一选择，返回 `comparison_selection_incomplete` 和候选，不自动取最新。无 evidence revision 解析为 `metadata_only` item并继续参与identity/metadata对照。纯snapshot item没有record/revision语义，resolved item固定返回`RevisionContext=not_applicable`与`RevisionMetadata=nil`；即使snapshot被一个或多个records引用，也不得选择current、first或latest引用来伪造revision metadata。两种模式都绝不从mutable root/current projection补revision元数据。

响应分两层：无 `Detail` 时返回 normalized conditions、resolved IDs/hashes、available kinds 和完整 comparability review；有 `Detail` 时只增加一个 active kind/metric 的 bounded series/matrix。这样 UI 必须先展示 review，且不会一次把所有 5 MiB payload 全送浏览器。两层共享 `comparison_digest`；detail digest 另绑定 kind/metric。

## 3. 对齐与可比性算法

registry 的 `Kind.Compare` 扩展为显式 descriptor：compatible schema pairs、metric semantic IDs、canonical unit、allowed lossless conversions、可接受 source bucket、是否支持 common-overlap reaggregation、输出 reason/version。通用 orchestration 不猜 payload 字段。

固定步骤：

1. 重新授权并取得每个 record/snapshot 的 content lease、reservation epoch 和 immutable hash；
2. 校验 2–6 items、baseline 和 requested absolute window；
3. 按 kind 分组并生成 item × kind compatibility matrix；
4. `actual_coverage` 保留各自 actual window/buckets且不重采样。interval bucket只在语义/宽度一致且起止偏移都在显式tolerance内时匹配；point/bucket使用按UTC升序的单调一对一nearest匹配，同一candidate不可复用。最小绝对偏移并列时依次选择较早UTC、canonical decode后由time/metric/sample identity排序产生的source ordinal、较小stable content hash；caller map/decoder迭代顺序不参与，ordered comparison item顺序仍作为显式业务条件进入digest；kind summary只有在descriptor声明window-equivalent时才算差；
5. `common_overlap` 求所有参与 snapshot actual windows 的真实交集，以UTC epoch为anchor生成canonical grid；bucket不细于任何source且必须被contract接受，只纳入完整落在交集内的bucket，partial edge丢弃并显式计数。禁止upsample/interpolate；tolerance只服务第4步配对，不能扩大交集、跨gap或生成观测点；
6. 对每 metric 只执行 descriptor 的 unit conversion/delta/rate/risk calculation；
7. 生成 warnings、calculation version、canonical digest 和 bounded presentation DTO。

reason enum：

| Code | 语义 / 数值行为 |
|---|---|
| `metadata_only` / `kind_missing` / `metric_missing` | 保留 item identity；无数值、无空图 |
| `coverage_partial` / `coverage_truncated` | 可按 contract 展示已有 segment；warning 常驻，不外推 |
| `common_overlap_empty` | common mode 阻止该 kind 全部数值 |
| `schema_incompatible` / `unit_incompatible` / `precision_incompatible` | 展示版本/单位/桶宽和原因；不转换/重采样 |
| `source_tombstoned` / `source_unavailable` | snapshot 若完整仍可比较，但 current source 状态单列；不把它改成数值缺失 |
| `snapshot_unreadable` | 该 snapshot 不参与数值；其他 kind 可继续并在 review 标 partial result，但整个comparison不签发save intent |

freshness 拆成 `freshness_at_capture`（observed end→captured time、capture policy state）、`snapshot_age`（observed end→request time）和 `source_status`，不能用一个 stale boolean 混淆。已注册、可读但没有对应Compare descriptor/schema-pair的snapshot只返回kind/schema/hash/time/size与`schema_incompatible` reason，不交给通用JSON renderer；权威库中的真正unregistered schema由task4 readiness/read路径失败关闭，不能进入comparison。外部unsupported schema只停留在task10 quarantine/dry-run，不能成为selection。

series 由 gap-aware segments 表示：

```go
type Series struct {
	ItemIndex int
	MetricID  string
	Segments  [][]Point // gap/maintenance boundary already split
	Unit      string
}
```

每 series 最多 2,000 aligned buckets；每个 point 有 observed window、value、sample/quality flags。0 是合法 value；missing 不创建 point。Web 为每个 segment 单独画 polyline，避免跨 gap 连线。

## 4. URL 与内部分享

`/records/compare?state=<base64url>` 使用 canonical JSON `comparison-url/v1`：

```json
{
  "mode":"fixed",
  "items":[{"snapshot_id":"ev_a"},{"record_id":"rec_b","revision_id":"rev_2","snapshot_ids":["ev_b"]}],
  "baseline":0,
  "alignment":"actual_coverage",
  "requested_from":"2026-07-01T00:00:00Z",
  "requested_to":"2026-07-02T00:00:00Z",
  "tolerance_seconds":60,
  "bucket_seconds":300,
  "kind":"monitoring_timeseries/v1",
  "metric":"latency_ms"
}
```

codec 固定 key order、UTC、整数秒、ordered items 和 deduplicated snapshot IDs；默认值省略后再编码。state 只含 IDs/条件，不含 payload、title、identity snapshot、token 或 authorization。它不签名也不授予权限；服务端严格解析、限制 256 KiB 并每次重鉴权。candidate mode state 只含 subject refs/window；用户确认后 replace 为 fixed state，浏览器 back 仍可回候选选择。

损坏/未知 version/超限显示可恢复选择 shell；不会尝试部分猜测。selection 因撤权/删除失效时 API 返回统一 404；UI 清旧 result 并保留不含受限 identity 的剩余本地条件。

## 5. Save-as-record transaction 与 traceability

compare response始终返回 `save_eligibility=eligible|blocked` 与稳定blocker reason。`metadata_only`、schema/precision不兼容或无共同覆盖仍可把选择与告警保存；只要所有实际选中snapshot的logical identity与payload hash可读、可复制即可。任一`snapshot_unreadable`、hash mismatch、权限/fence lease失败或copy contract缺失时不签发intent，Web移除保存动作并显示真实blocker，不能跳过坏项生成不完整结果。

eligible compare才返回15分钟 `comparison_intent`：versioned HMAC token 绑定purpose=`comparison-save/v1`、key ID、actor/project、ordered original IDs/hashes、baseline/conditions、comparison digest、registry/calculation versions、auth/fence heads、warnings digest 和 expiry；不嵌 payload/Markdown。records create command 携 intent、完整用户 revision input 和 Idempotency-Key。

`ComparisonIntentSigner`使用dirfd-relative `openat(..., O_NOFOLLOW)`读取独立0400 regular keyring file，并在读取前后核对owner/mode/device+inode/link-count，拒绝symlink、目录/device、非预期hard link或替换竞态；current signing key与retired verify keys都有stable key ID、active/retired时间和canonical config digest。所有center replica使用同一verify set；轮换先把新key作为verify-only分发并由deployment membership确认，再切current，旧verify key保留到`last_signed_at + 15m TTL + 2m clock skew`之后。readiness在capability开启时验证文件类型/owner/mode、current唯一、verify window、member config digest；缺失实例不接流量。key material不入DB/log，且与deletion idempotency、backup Ed25519和session secret完全分离。

`ComparisonRevisionParticipant` 在正式 transaction 中：

1. 验 token/expiry/actor/project/idempotency；
2. 重新授权、取得 fresh fence、解析同一 immutable IDs/hashes并重跑 comparability；漂移返回 `comparison_intent_stale`；
3. 为目标 record 创建每个 selected snapshot 的新 logical identity，保存 `copied_from_snapshot_id`，复用 immutable payload bytes但不共享 auth/audit/quota；
4. 生成 `comparison.result/v1` canonical payload，固化 original→copied mapping、每个revision-bound item的 `RevisionMetadataSnapshot`、每个snapshot-only item的`revision_context=not_applicable`、baseline、全部 requested/actual/common windows、alignment/tolerance/bucket、schema/registry/calculation version、quality/warnings和系统差异；
5. 将 copied snapshots 与 result snapshot 按顺序加入新 revision refs；
6. 由 records transaction 原子写 root/revision/domain activity/outbox并 commit。

用户 title/type/primary subject/visibility/occurred time 和 conclusion Markdown 位于 normal revision。`comparison.result/v1` 不保存人工结论，也不把 risk comment 变系统事实。数据库、quota、copy、participant、CAS 或 auth/fence 任一步失败全部 rollback；content-addressed orphan 遵循 task 4 janitor。同 intent/Idempotency-Key retry 返回原 record/revision。

`comparison.result/v1` 实现 renderer/Summarize/Export。Export material 含可读条件、系统差异、warning、source/copy provenance 与 snapshot exporter refs；task 10 的 Markdown/PDF/archive job调用它。工作台本身没有 Blob/下载/CSV endpoint，避免绕过 task 10 的 safe/sensitive preview、再次授权、TTL 和审计。

## 6. Web 工作台与入口

`RecordComparisonPage` 是 `/records/compare` 唯一 controller/composition point。route-private `useComparisonWorkbench` 拥有 URL state、candidate/summary/detail AbortController、stage、basket、baseline、active kind/metric、human conclusion 和 formal-save state；presentation components 不直接调用 API。

页面顺序固定：

1. subject recommendation / exact revision·snapshot mode 与 2–6 basket；
2. requested window、baseline、alignment、tolerance、bucket 条件；
3. comparability review；
4. kind tabs 和一个 active metric；
5. segmented trend 与 named-scroll matrix；
6. system-verifiable differences；
7. human conclusion + save record form。

record center、fixed revision page 和 subject evidence page只构造 canonical fixed state并导航，不在各页实现 comparison。记录无 evidence 仍可入篮并显示 metadata-only。static `/records/compare` route 必须注册在 `/records/:recordId` 之前。旧 `/monitoring/compare` 保持 live 2-way tool，不复用或重定向。

desktop 展示最多 6 columns；390px conditions 折叠为可回看摘要、review 保持首屏指标前、一次一个 kind/metric。matrix wrapper 有 visible heading/hint、`role=region`、`aria-labelledby/aria-describedby`、`tabIndex=0` 和 sticky row header；document 不拥有横向滚动。chart 同时提供可访问 data table/summary，颜色之外使用 series label/line pattern/marker。

状态机区分：少于 2 项、candidate empty、metadata-only、无兼容 kind、单 snapshot unreadable、summary/detail loading、cancelled、stale intent、save conflict/storage error、revoke。请求条件改变立即 abort old detail并保留旧结果为明确 stale shell，绝不冒充新条件结果。

## 7. 授权、失败与兼容

- candidates、summary、detail、save 和 renderer/export 都调用 `recordauth.Policy`；record revision visibility 与全部 source capture/final floor 取交集。
- handler 在 cache/decompress/response byte 前取得/续租 object content lease；`Cache-Control: private, no-store`。reservation/revoke 取消 server context 和 Web fetch。
- 一项撤权/永久删除使 fixed comparison 条件失效并统一 404；不得返回其 name、kind count 或“第 N 项无权”等可枚举信息。
- source tombstone 不自动使 immutable snapshot 失效；payload/hash损坏才是 unreadable。kind 单项失败可返回partial review；只有所有selected snapshot均可读/可复制时intent才固化warning，`snapshot_unreadable`时`save_eligibility=blocked`且不签发intent。
- stable errors：`comparison_selection_incomplete`、`comparison_selection_invalid`、`comparison_selection_stale`、`comparison_incompatible`、`comparison_intent_expired`、`comparison_intent_stale`、`comparison_result_too_large`、`resource_not_found`。
- capacity errors：admission等待超过2秒或16-request queue已满返回429 `comparison_capacity_exhausted`并给bounded `Retry-After`；单请求实际working-set越过已取得weight返回422 `comparison_request_memory_limit`。两者都不返回partial result、不签发intent，也不从旧response cache降级。
- rollout/rollback：comparison capability 默认 off。关闭 route/handler即回滚交互；没有 schema/down migration。已保存 records、logical copies 和 `comparison.result/v1` 继续由 retained version renderer/exporter读取。禁止 fallback 到 live runtime facts 或 arbitrary JSON。

## 8. Scale and performance

候选与 revision expansion 使用 batch SQL，query count不随 item×kind线性增长；payload 只读取 active compare kind，detail只返回一个 metric series。硬上限：6 items、每 snapshot既有50k points/5 MiB、每 response series 2k buckets、response 2 MiB、fresh隔离container单request cgroup peak-memory相对GC后idle增量96 MiB。超过结果边界返回 `comparison_result_too_large` 并要求缩小窗口/metric，不静默截断。

`ComparisonAdmission` 在任何payload读取/解压前，以snapshot bytes、point上限、item/series/alignment mode和response上界计算保守working-set，向8 MiB粒度weighted semaphore申请token。capability开启要求显式 `HOUFENG_COMPARISON_MEMORY_BUDGET_BYTES`、可读cgroup v2 `memory.max`和启动后idle基线；有效budget不得超过配置值或`(memory.max - 1 GiB non-comparison reserve) / 2`。父4 GiB参考容器固定配置512 MiB，可同时接纳5个96 MiB最大请求并保留32 MiB comparison余量；队列最多16，等待上限2秒。实际阶段计数若将越过已准入weight则取消并返回`comparison_request_memory_limit`，不能临时借用其他服务headroom。

request context取消、client断连或handler write失败只触发停止信号；token必须等decode/alignment goroutine与response writer全部join后释放，门禁要求5秒内归零。超时产生`comparison_drain_timeout`、把comparison readiness/capability置为unavailable并等待operator确认无残留worker后恢复，不能提前释放造成叠加峰值。task11在相同4 GiB容器的完整mixed profile中同时验证tracked active weight≤512 MiB、cgroup peak与1 GiB保留、无OOM/major throttle，并把budget/config/runner digest写入receipt。

父参考资源上工程门：candidate/summary warm p95≤1s；6×2,000 detail warm p95≤2s。context cancellation 在每次 payload decode、segment、metric loop检查，客户端 abort 后不得继续写 response。报告记录三轮 p50/p95/p99、allocs、response bytes、query count、admission wait/reject/drain、active weight、cgroup peak和错误率；task 11必须把comparison summary/detail与并发饱和场景纳入完整混合负载。

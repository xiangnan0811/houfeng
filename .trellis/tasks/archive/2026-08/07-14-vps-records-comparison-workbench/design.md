# 横向比较工作台设计

## 0. Authority

无 root migration。范围以 2026-08-20 用户批准的 **A** 为准：`research/current-main-reconciliation-2026-08-20.md` + `prd.md`。关闭 comparison capability 回滚交互，不删除已保存的 versioned `comparison.result/v1`。

锁定决策：

- 复用 pairwise `Kind.Compare(..., AlignmentExact)`；不为 registry 发明 Compare descriptor 语言。
- series / trend / matrix 只服务 `monitoring.host/v1` 与 `monitoring.probe/v2`。
- `POST /api/evidence/comparison-candidates` 与 `POST /api/evidence/comparisons` 分开。前者只做 subject 候选，后者只接受 fixed IDs。
- 另存走 `RecordCreateRequest` / `RecordRevisionCreateRequest` + `EvidencePreparation` + 新 comparison intent 字段；participant `Name() == "comparison"`，按字母序排在 `collaboration` 与 `evidence` 之间。
- 修订元数据字段是 `impact_level`。source 状态是 `live|tombstoned` + `source_available`。
- 进程内 admission 留在本 child；4 GiB / mixed-load harness 归 Child 11。

## 1. 模块边界与持久化

comparison 放在 `internal/center/evidence/`。HTTP 只解析 candidate/compare；store 批量解析 immutable revision/snapshot refs 与 copy-lineage 写入；Web 只渲染服务端 allowlisted DTO。不新建 backend package，不读 raw source tables，不 import activity/search。

交互运行不持久化。登录内分享靠 `comparison-url/v1`。另存复用 records 事务、`evidence_snapshots`、payload 去重和已有 `evidence_copy_lineage` 表。唯一新持久语义是注册 kind `comparison.result/v1`。

现有 participant：`attachments`、`collaboration`、`evidence`、`search`。比较另存扩展 evidence 写路径（logical copy）并增加 `comparison` participant 校验 intent / 写 result snapshot。

## 2. 两阶段选择合同

### 2.1 Subject candidates

`POST /api/evidence/comparison-candidates`：

```go
type CandidateRequest struct {
	Subjects        []SubjectRef // 2..6, vps|monitoring_instance|target
	RequestedWindow TimeRange    // absolute UTC
	Kinds           []KindKey    // empty = all registered readable kinds
}
```

一次批量查询每个 subject 在窗口附近的已授权 snapshot / revision refs，按 kind/schema 兼容、actual-window 距离、quality、captured time、stable ID 排序。响应只含 authorized IDs、schema/hash、窗口、质量、推荐原因。不创建 capture intent / snapshot，不把推荐送进 compare。缺失 subject 对外统一 404，不返回其他 selection 的计数。

候选 SQL 只打 records/evidence 表与 subject identity，不走 activity 投影。

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
	Detail          *DetailRequest // one registered kind + optional metric
}

type FixedItem struct {
	SnapshotID *string
	Revision   *FixedRevision // record_id + revision_id + optional chosen snapshot IDs
}
```

每个 item 必须恰好是 snapshot 或 revision。revision 只展开该不可变修订的 refs，并读取当时的 `record_type` / `business_status` / `status_group` / `impact_level` / `occurred_at`。同 kind 多 snapshot 未在 `ChosenSnapshotIDs` 唯一选择时返回 `comparison_selection_incomplete`。无证据修订为 `metadata_only`。纯 snapshot 项 `RevisionContext=not_applicable`、`RevisionMetadata=nil`。

无 `Detail`：normalized conditions、resolved IDs/hashes、available kinds、完整 comparability review。有 `Detail`：只追加一个 kind/metric。host/probe detail 才解码 series；其他 kind 返回 pairwise Compare DTO。两层共享 `comparison_digest`。

## 3. 可比性与对齐

编排层不猜 payload map key，不读 Markdown。

1. 用 `recordauth.Policy` + `CapabilityComparisonRead` / `CapabilityEvidenceRead` 重鉴权每个 record/snapshot；denied 统一 404。
2. 校验 2–6 items、baseline、absolute window。
3. 按已注册 kind 生成 item × kind 兼容矩阵。
4. 非时序 kind：对 baseline 与每个其他 item 调用现有 `Kind.Compare(..., AlignmentExact)`，把 allowlisted `Comparison.Values` 放进 system-differences；alignment 不是 exact-compatible 时标 `schema_incompatible` 或 `common_overlap_unsupported`。
5. host/probe + `actual_coverage`：在相同 bucket 语义/宽度且起止偏移落在 tolerance 内时做单调一对一 nearest 匹配；并列按较早 UTC、canonical source ordinal、较小 content hash 决胜；生成 gap-aware `[][]Point`，每 series ≤2,000 buckets。
6. 任何 kind 的 `common_overlap`：返回 `common_overlap_unsupported`（当前无 kind 声明重聚合），数值 cell / series 为 0。
7. 生成 warnings、calculation version、canonical digest、bounded DTO。

reason 见 `prd.md`。freshness 拆成 `freshness_at_capture`、`snapshot_age`、`source_state`（`live|tombstoned`）和 `source_available`。tombstone 不把完整 snapshot 改成数值缺失。

```go
type Series struct {
	ItemIndex int
	MetricID  string
	Segments  [][]Point
	Unit      string
}
```

0 是合法 value；missing 不创建 point。Web 每 segment 一条 polyline。

## 4. URL

`/records/compare?state=<base64url>`，canonical JSON `comparison-url/v1`：

```json
{
  "mode": "fixed",
  "items": [
    {"snapshot_id": "evs_a"},
    {"record_id": "rec_b", "revision_id": "rrv_2", "snapshot_ids": ["evs_b"]}
  ],
  "baseline": 0,
  "alignment": "actual_coverage",
  "requested_from": "2026-07-01T00:00:00Z",
  "requested_to": "2026-07-02T00:00:00Z",
  "tolerance_seconds": 60,
  "bucket_seconds": 300,
  "kind": "monitoring.probe/v2",
  "metric": "latency_ms"
}
```

固定 key order、UTC、整数秒、ordered items。state 不含 payload、title、token。candidate mode 只含 subject refs/window；确认后 replace 为 fixed。损坏/未知 version 显示可恢复选择 shell。撤权/删除 → API 404，UI 清结果且不保留受限 identity。

静态 `/records/compare` 必须注册在 `/records/:recordId` 前。`/monitoring/compare` 不复用、不重定向。

## 5. Save-as-record

compare 返回 `save_eligibility`。可读的 metadata-only / 不兼容选择仍可保存告警。任一 unreadable / hash / copy / auth blocker 不签发 intent。

eligible 时签发 15 分钟 HMAC intent：purpose `comparison-save/v1`、key ID、actor/project、ordered IDs/hashes、conditions、digest、registry/calculation versions、warnings digest、expiry。不含 payload / Markdown。

`ComparisonIntentSigner`：dirfd + `O_NOFOLLOW`、0400 regular file、读取前后核对 owner/mode/inode/nlink。不复用 deletion / backup / session key。key 不入 DB/log。

另存 HTTP 在现有 `POST /api/records` / `POST /api/records/{id}/revisions` 上增加 comparison intent 字段，并由服务端根据 intent 构造 copied `evidence_items` + `comparison.result/v1` 引用。不能走 `useRecordDraft.publish()`。`Idempotency-Key` 仍是 records 正式保存合同。

`comparison` participant 在同一 `pgx.Tx`：

1. 验 token / actor / project / expiry / digest；
2. 重鉴权、重跑 comparability；漂移 → `comparison_intent_stale`；
3. 为每个 selected snapshot 插入新 logical identity + `evidence_copy_lineage` 行，复用 payload bytes；
4. 写入 `comparison.result/v1` canonical payload（original→copied、revision metadata 或 `not_applicable`、windows/conditions/versions/warnings/system differences；无 human conclusion）；
5. 把 copied snapshots 与 result 按序加入新 revision refs。

任一步失败整单 rollback。同 key retry 返回原 record/revision。`comparison.result/v1` 实现 Summarize / Compare（self/exact） / Export。工作台无下载 endpoint。

## 6. Web

`RecordComparisonPage` 是 `/records/compare` 唯一 composition point。`useComparisonWorkbench` 拥有 URL state、AbortController、basket、baseline、active kind/metric、conclusion、save state。展示组件不直接 fetch。

顺序：选择篮 → 条件 → comparability review → kind tabs → host/probe trend+matrix 或非时序 Compare DTO → system differences → conclusion + save。

入口只做 URL builder：

- `RecordSearchPage`（记录中心）
- `RecordRevisionPage`
- `SubjectEvidencePage`（timeline 已有 `evidence_snapshot_id`；无篮、无 `/evidence/:id` SPA 详情页）

390px：条件可折叠回看；review 在首指标前；一次一个 kind/metric；matrix 用 named scroll region。Artifact v1 用 Playwright 语义/几何/overflow/Axe/focus，不提交 pixel golden。

`recordsApi.ts` 继续 lazy-only；`/records/compare` 与三个入口可静态导入它，不得进入 AppShell。

## 7. 授权、失败、rollout

- 一律 `recordauth.Policy`。record visibility ∩ source capture ∩ live current / tombstone final floor。
- 比较重鉴权，不读 activity digest allowlist。group-granted viewer 只要 `recordauth` 允许 snapshot，精确 ID / 分享 URL 必须可读。
- `Cache-Control: private, no-store`。payload 前取得/续租 object content lease（父设计仍要求；evidence 现读路径若未租，比较路径必须租）。
- 稳定错误：`comparison_selection_incomplete`、`comparison_selection_invalid`、`comparison_selection_stale`、`comparison_incompatible`、`comparison_intent_expired`、`comparison_intent_stale`、`comparison_result_too_large`、`resource_not_found`。
- 容量：429 `comparison_capacity_exhausted`、422 `comparison_request_memory_limit`。不截断、不签 intent。
- `HOUFENG_COMPARISON_ENABLED` 默认 false。关 route 即回滚。已保存 result 继续由 registry 读取。

## 8. Scale

candidate / revision expansion 用 batch SQL。detail 只解码一个 host/probe metric，或返回一个 pairwise Compare DTO。

本 child 的 admission 是进程内 weighted semaphore（8 MiB token，可配 budget）。等待/队列上限与 429/422 合同保留。不要求 cgroup v2 `memory.max` 作为 Child 8 readiness。

工程门：candidate/summary 与 6×2,000 host/probe detail 用 Go test + 可选 PostgreSQL integration 记录 p50/p95、query count、response bytes。4 GiB 容器 peak、512 MiB 五路并发饱和、mixed-load receipt 由 Child 11 在同一 profile 验证。

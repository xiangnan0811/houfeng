# 记录、修订、草稿与状态核心设计

## 0. Development rebaseline

`0052` 是 fresh/current development baseline 的下一条 root migration，并由本任务同时扩展 current APP ACL fragment。没有旧库 upgrader、`experience_logs` mapping、双写或 cutover。本任务关闭 feature 仅作为开发回滚，不承诺旧数据兼容。

## 1. 文件与模块边界

- Domain：`internal/center/records/`（types/validation/templates/service/drafts/revisions）。
- Deletion orchestration：`internal/center/recorddeletion/`，只编排 registry adapter，不直接知道 Blob/import 实现。
- PostgreSQL：`internal/center/store/records.go`、`record_drafts.go`、`record_deletions.go`。
- HTTP：`internal/center/http/handlers/records.go`、`record_drafts.go`、`record_deletions.go`。
- Web contract遵守当前spec：canonical DTO追加到 `web/src/lib/types.ts`；仅被lazy records routes消费的domain transport为 `web/src/lib/recordsApi.ts`。它只type-import `types.ts`并直接复用`apiRequest`，不得被AppShell/TopBar/Sidebar等eager模块导入；bundle test证明它只在records lazy chunks。通知等eager helper继续由`web/src/lib/api.ts`拥有，避免一个monolithic façade把全部records代码拉入入口包。

## 2. 数据模型

`0052_create_records_core.sql` 创建：

- `records(record_id, project_id, lifecycle, current_revision_id, lock_version, current_* projection, archived_at)`；
- `record_revisions(revision_id, record_id, revision_no, title, markdown_source, record_type, business_status, status_group, impact, occurred_at, visibility, owner_id, participant_ids, next_followup_at, template_id/version, authored_by/at, base_revision_id, canonical_hash)`；
- `record_revision_subjects`、`record_revision_tags`，含 role/order/identity snapshot/live ref/tombstone；
- `record_drafts`、`record_draft_recovery_points`，按 author 隔离；
- `record_domain_activities` 作为业务事务 append-only source；
- `record_core_purge_receipts`，不保存正文。

current projection 由 revision participant 在同事务更新；database constraints保证 current pointer 属于同 record，revision_no 唯一递增，root 内容列不能成为历史权威。

## 3. 保存事务

```go
type RevisionParticipant interface {
	ApplyRevision(context.Context, pgx.Tx, RevisionCommitted) error
}

type CreateRevisionCommand struct {
	RecordID       string
	BaseRevisionID string
	IdempotencyKey string
	DraftID        string
	Input          CompleteRevisionInput
}
```

service 顺序固定：授权 → 输入/模板/关系校验 → source ACL → idempotency → lock record → base CAS → material intent validation hook → insert full revision/relations → current projection → domain activity → registered participants（search/outbox）→ commit。任何一步失败不生成半份 revision。

## 4. 草稿与冲突

draft ETag 独立于 record lock version。两秒 idle autosave属于 Web；服务端 PATCH 使用 `If-Match`。草稿保存 `base_revision_id` 与完整当前输入；服务器 current 已推进时返回 `409 record_revision_conflict`，包含 server revision、draft/local snapshot 与字段/Markdown diff inputs，不自动合并。

## 5. 类型/模板

builtin registry 固定 `troubleshooting|maintenance|migration|provider_communication|billing|important_finding|note`，每个类型声明业务状态及统一 group。模板 ID/version 为代码注册表；新草稿可选插入，切类型只返回建议/diff，绝不覆盖正文。

## 6. 删除适配

core adapter 删除 revisions/drafts/core relations/domain activities/current projection；逻辑 evidence/attachment/collaboration/search/import adapter 由后续任务注册。preview 列出未注册/不健康 adapter 并阻止 token。reservation 后所有 handler/store query 先查 fence；`not_committed` outcome durable 才恢复。

## 7. 兼容

现有 experience 代码可在增量开发期间保持未触碰，但新 Records 不读取或迁移其数据。feature flag off 可用于开发回滚；返回不含 `0052` 的代码版本时重建开发数据库。Child 10 只实现 Records 导入导出，Child 11 只做集成/备份恢复终验。

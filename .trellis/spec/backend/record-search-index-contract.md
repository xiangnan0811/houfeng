# 记录搜索索引合同（Record Search Index Contract）

> **项目依据**：以当前代码为准。本文档记录 `0056_create_record_search.sql` 落地后 `internal/center/recordsearch/`、`internal/center/store/record_search*.go` 与 `GET /api/records/search` 的实际合同。

搜索索引是**派生投影**，不是权威数据。权威记录始终是 `public.records` 与 `public.record_revisions`。索引可以被整代丢弃并重建，任何"只有索引里才有"的事实都是 bug。

---

## 1. 三层职责边界

| 层 | 位置 | 只负责 |
|---|---|---|
| 域 | `internal/center/recordsearch/` | 查询归一化、游标编解码、候选→授权水合的编排、重建编排、删除适配器 |
| 存储 | `internal/center/store/record_search*.go` | SQL 下推、键集分页、代次校验、租约/断点、发布 CAS、purge |
| HTTP | `internal/center/http/handlers/record_search.go` | 参数解析、错误码映射、响应装配 |

`recordsearch` 只依赖标准库、`records` registry 和 `recordauth`，这样 HTTP 层、store 层和投影器**共用同一套归一化和同一套游标合同**。不要在 handler 或 store 里重新实现归一化。

---

## 2. 索引不复制正文

`record_search_documents` 存的是：

- `title`：标题原文
- `plain_text`：由 `DeriveDocumentTextFromMarkdown` 从 Markdown 派生的可搜索纯文本，上限 `MaxDocumentTextBytes`（64 KiB）
- `search_text`：生成列 `lower(title || ' ' || plain_text)`，GIN trigram 索引建在它上面
- `document_digest`：32 字节 SHA-256 内容指纹，用于重建覆盖率审计

**原始 `body_markdown` 不进索引。** `document_digest` 只覆盖投影内容字段，不含 `generation` / `record_lock_version` / `record_fence_epoch` 等控制列，否则同一内容在不同代次会得出不同指纹，覆盖率对比就失去意义（见 `projection.go` 的说明注释）。

中文检索依赖 `pg_trgm`，扩展装在 `record_platform_internal` schema，操作符类必须写全限定名 `record_platform_internal.gin_trgm_ops`。

---

## 3. 代次（generation）与发布 CAS

`record_search_generations.generation_state` 只有四个值：`building` / `published` / `superseded` / `failed`。

两个部分唯一索引是并发正确性的地基，不要删：

- `uq_record_search_generations_published`：全局最多一个 `published`
- `uq_record_search_generations_building`：全局最多一个 `building`

读路径的代次校验是**两段式**，两段都必须保留：

1. `PublishedGeneration` 取当前已发布代次；返回 `0` 表示索引尚未就绪 → `ErrIndexUnavailable`
2. `ListSearchCandidates` 在读事务内**重新确认**该代次仍是 `published`；否则 → `ErrGenerationSuperseded`

只做第 1 步会让一次翻页横跨两个代次，得到"既不是旧代次也不是新代次"的混合结果集。

首装引导用 `EnsurePublishedRecordSearchGeneration`：表为空时写入一个空的 `published` 代次，让搜索在还没有任何记录时就返回空结果而不是不可用。

---

## 4. 实时投影走 RevisionParticipant

`NewRecordSearchRevisionParticipant()` 注册名为 `"search"`，在记录提交事务内投影，因此**索引与权威记录同事务成败**。参与者顺序、回滚、归档/恢复/删除、评论脱敏、可见性变更、导入都必须走这条路径。

`record_search_documents` 的 upsert 带围栏条件：

```sql
on conflict … do update … where excluded.record_lock_version >= …
                             and excluded.record_fence_epoch >= …
```

这让**慢重建的旧快照无法覆盖已经落地的新提交**。重建批次遇到已被实时提交抢先的记录时，`projectSearchRebuildRecord` 返回 `(false, nil)`——这是正常跳过，不是错误。

`record_search_subjects` 用 DELETE + INSERT 整体替换，所以 runtime 角色对它有 DELETE 权限而没有 UPDATE 权限。

---

## 5. 重建：租约 + 断点 + CAS 发布

重建写**影子代次**（`building`），全量投影完成后才原子切换，读路径全程不受影响。

| 阶段 | 存储方法 | 机制 |
|---|---|---|
| 判定 | `RecordSearchRebuildNeeded` | 存在 `building` 代次，或已发布文档数 < 合格记录数 |
| 抢占 | `ClaimRecordSearchRebuild` | `FOR UPDATE` 锁定，铸造 `rsj_*` job，`lease_expires_at = transaction_timestamp() + lease` |
| 推进 | `ProjectRecordSearchRebuildBatch` | 校验自己仍持有活跃租约，按 `record_id` 顺序取批，推进断点 |
| 发布 | `PublishRecordSearchRebuild` | 同事务内 supersede 旧 published + publish building |
| 失败 | `FailRecordSearchRebuild` | job 与代次落 `failed`，写入 `failure_reason` |

- **租约**：`owner_id` + `lease_expires_at`，每批续租。别的 owner 持活跃租约时返回 `ErrRebuildLeaseHeld`（store 层 sentinel，不要当成域错误）。
- **断点**：`resume_after_record_id` + `processed_count`，进程崩溃后从断点续传，不重头再来。
- **覆盖率**：`measureSearchGenerationCoverage` 用 `count(*)` 加按 `record_id` 排序的 `record_id:digest` 聚合，域前缀 `"houfeng.record-search.rebuild-coverage.v1"`。改动摘要口径必须同时改域前缀。
- `failure_reason` 是受约束的 `^[a-z0-9_]{1,64}$`，当前只有 `rebuild_stalled` / `rebuild_interrupted` / `rebuild_failed`。

---

## 6. 游标是"上下文绑定"，不是签名

游标是 `base64.RawURLEncoding` 包裹的紧凑 JSON，字段为 `v` / `q`（查询摘要）/ `a`（actor scope 摘要）/ `g`（代次）/ `e`（过期，Unix 微秒）/ `u` + `i`（键集位置）。

**它不带服务端签名。** 校验靠解码时比对摘要：查询摘要、actor scope 摘要、代次任一不匹配即失败。所有失败路径统一返回裸 `ErrInvalidCursor`，不透露是过期、篡改还是代次变化——差异化信息会变成探测口。

因此不能把游标当作能力凭证：它只保证"这一页属于同一个查询 + 同一个授权命名空间 + 同一个代次"。**每条候选记录仍然要经过 `RecordReader.GetRecord` 的完整授权水合**，索引本身不做任何授权判定，只产出候选 ID。

`DisallowUnknownFields` + 拒绝尾随 JSON 是有意的：不接受任何"多带一点字段"的游标。过期判定用 `!now.Before(expiresAt)`，边界时刻算过期。TTL 默认 `DefaultCursorTTL`（30 分钟）。

键集排序是 `(record_updated_at, record_id)` 元组比较，两列同向。**不要只按时间排序**——同一微秒的多条记录会在翻页时丢失或重复。

---

## 7. 只有一条搜索路径

`records/read_service.go` 的内存 `q` / `lifecycle` / `record_type` 过滤**已经删除**，`RecordListRequest` 只剩 `Actor` / `Sort` / `After` / `Limit`。旧过滤参数在 HTTP 层被显式拒绝：`retiredRecordListFilters` 命中即 400 `filter_retired`。

保留两条搜索路径的代价是两套匹配规则和两套权限证明，任何"顺手在 list 里加个过滤"的改动都要退回搜索索引。

---

## 8. 错误码映射（对外合同）

| 域错误 | HTTP | `code` |
|---|---|---|
| `ErrInvalidQuery` / `ErrInvalidSearchRequest` | 400 | `query_invalid` |
| `ErrInvalidCursor` | 400 | `cursor_invalid` |
| `ErrGenerationSuperseded` | **409** | `search_generation_superseded` |
| `ErrIndexUnavailable` | 503 | `search_unavailable` |
| 无 session actor | 503 | `authorization_unavailable` |

409 与 503 是**可恢复**语义，前端据此分别"从第一页重放"和"提示索引未就绪 + 重试"，不要合并成通用失败。

`/api/records/search` 的查询参数是闭集（`recordSearchQueryKeys`），未知参数返回 400 而不是静默忽略——静默忽略会让拼错的筛选看起来"生效了"。

---

## 9. 删除必须穿透索引

记录删除时索引副本要一起消失，否则派生投影会变成删除数据的影子副本。

- SQL：`public.record_search_purge(bytea)` → `record_platform_internal.purge_record_search(...)`（security definer）
- Go：`PurgeRecordSearch` / `VerifyRecordSearchPurge`，域侧 `recordsearch.DeletionAdapter`
- purge 删除该 `record_id` 在**所有代次**的 `record_search_documents` 与 `record_search_subjects` 行
- 回执写 `record_search_purge_receipts`，该表由 immutable 触发器拒绝 UPDATE
- `assertRecordSearchSurfacesAbsent` 做缺席验证：purge 之后必须证明"确实没有了"，而不是相信删除语句的返回值

center runtime 只拿到 `public` 前缀的 bytea 包装函数的 EXECUTE 权限，**不直接授予 internal schema 函数的 EXECUTE**。

---

## 10. 常见错误

1. 在 handler 或 store 里重写查询归一化，导致游标摘要与实际查询不一致，翻页整体失效。
2. 只查一次已发布代次就开始翻页，让结果集横跨代次切换。
3. 给游标失败返回细分错误信息，把它变成探测口。
4. 认为游标绑定等于授权，跳过逐条授权水合。
5. 把 `document_digest` 算到控制列上，让重建覆盖率永远对不上。
6. 重建时不校验租约或不推进断点，崩溃后重头再来并与实时提交互相覆盖。
7. 往索引里塞 `body_markdown`，把派生投影变成正文的第二份权威副本。
8. 新增索引对象却不同步 `app_acl_current_contract.go` 的 managed objects 与权限，导致 ACL 收敛校验失败。

# Record Portability Contract

> **项目依据**：以 `internal/center/portability/`、`db/migrations/0058_create_record_portability.sql`、`internal/center/store/record_admission_gate.go` 和当前 HTTP/Web 接线为准。

---

## 1. Scope / Trigger

Child 10 落地记录可移植性：具名 `AdmissionGate`、witnessed tombstone reader、`0058` 整表、人读 Markdown / 机器 ZIP64 archive / 派生 PDF、导入 dry-run/apply、origin tombstone、`record_portability` 删除 adapter。比较工作台 `/records/compare` 不是下载面。

---

## 2. Signatures

### HTTP（`HOUFENG_PORTABILITY_ENABLED=true` 且 `HOUFENG_RECORDS_ENABLED=true` 才注册）

| Method | Path |
|---|---|
| `POST` | `/api/record-export-previews` |
| `POST` | `/api/record-exports` |
| `GET` | `/api/record-exports/{rej_…}` |
| `GET` | `/api/record-exports/{rej_…}/content` |
| `POST` | `/api/record-imports/dry-run` |
| `POST` | `/api/record-imports/{rip_…}/apply` |

### Domain

- `records.Application.ExportDocument` / `ImportDocument`
- `evidence.ComparisonResultKind.Export` / `Summarize` — 比较导出唯一权威
- `recordmarkdown.SafeDocumentHTML` / `WriteDerivedPDF` / `ExtractDerivedHTML`
- `portability.WriteArchiveV1` / `ReadArchiveV1`
- `store.NewDeploymentMembershipAdmissionGate`

### 表（仅 `0058`，无 `0059`）

`record_export_jobs`、`record_export_artifacts`、`record_import_jobs`、`record_import_plans`、`record_import_artifacts`、`record_import_entity_mappings`、`record_origins`、`record_origin_tombstones`、`record_portability_purge_receipts`

---

## 3. Contracts

- 导出 kind：`markdown` / `comparison_json` / `evidence_json` / `archive` / `pdf`
- Archive format：`houfeng-record-archive/v1`；manifest 路径固定 `manifest.json`；ZIP Store；成员上限 256、单文件 8MiB、整包 64MiB、压缩比 ≤100、深度 1（拒绝嵌套 archive）
- Comparison JSON 字节必须等于 `ComparisonResultKind.Export` 且等于 canonical snapshot；禁止 `conclusion` / `markdown` / `body_markdown`
- PDF 是同一 RenderModel 的派生展示（`houfeng-derived-presentation/v1`），不是机器权威，不能当 archive 读入
- 导入：dry-run 写 0 条 records/evidence/search/activity 行；apply 经 `ImportDocumentsFinishing` 把文档、`record_origins` 与 job 终态放进同一笔 `RunRecordPlatformTransaction`（origin 冲突在写记录前判定；失败可按原计划重试）；`LoadImportJob` / `ClaimImportJob` 现有行必须读出 `actor_id`，Apply 在 actor 为空或与计划不符时 fail-closed（含已 applied 回放）；官方 evidence 成员按 `kind` + `schema_version` 识别；markdown 正文允许普通 URL/英文动词，只拒 `javascript:` / `file://` / `data:` URI；JSON 只拒顶层 authorization/role/renderer/sql/password/token/path/url；checkpoint 成员拒绝；required vs optional 由本机 registry 决定，archive 的 `optional:true` 不可信；未知 schema fail-closed；已知 comparison/evidence remap 后走 `EvidenceImporter`
- Origin tombstone 用 archive SHA-256；dry-run 与 apply 都查 tombstone 和已有 `record_origins`；purge 写入墓碑
- `sensitive_topology` 需要 `record.export_sensitive_topology` 与 preview 签发的 confirm token
- 生产 bootstrap 禁止 `store.AdmissionGateFunc(`

### Env

| Key | Default | Notes |
|---|---|---|
| `HOUFENG_PORTABILITY_ENABLED` | false | 叠在 `HOUFENG_RECORDS_ENABLED` 上 |
| `HOUFENG_RECORD_INSTANCE_ID` / `HOUFENG_RECORD_DEPLOYMENT_ID` / `HOUFENG_RECORD_INSTANCE_KIND` / `HOUFENG_RECORD_INSTANCE_CAPABILITY` | 全空或全有 | 未配齐时 gate 为 nil，Admit fail-closed |

---

## 4. Validation & Error Matrix

| Condition | Error |
|---|---|
| flag off | `ErrPortabilityDisabled` → 404 |
| inventory drift | `ErrExportInventoryDrift` → 409 |
| lease revoked | `ErrExportLeaseRevoked` → 409 |
| hostile/untrusted archive | `ErrInvalidArchive` / `ErrUntrustedImportContent` → 400 |
| unknown / archive-declared optional evidence | `ErrImportSchemaBlocked` → 400 |
| missing confirm token / missing sensitive capability | `ErrExportUnauthorized` → 404 |
| apply lock mismatch | `ErrImportCASConflict` → 409 |
| origin tombstone | `ErrOriginTombstoned` → 409 |
| existing origin / 二次导入 | `ErrImportOriginConflict` → 409；dry-run 与 apply 都查 `LoadOrigin` |

---

## 5. Good / Base / Bad

- Good：Markdown 预览点名未授权证据；archive 含 document.md + 已授权 evidence + `comparison.result_v1.json`；PDF 抽出的 HTML 等于 `SafeDocumentHTML`；dry-run 后 apply 幂等
- Base：Portability 默认关；身份 env 未配时 export Admit 仍 fail-closed
- Bad：`/records/compare` 下载面；第二套 comparison exporter；comparison CSV；把 `0051.source_deletion_tombstones` 当 witnessed authority；PDF 当 archive 权威

---

## 6. Tests Required

- `go test -race ./internal/center/portability -run 'Archive|Import|Origin|PDF|Portability' -count=10`
- `web`：`RecordExportPanel` / `RecordImportPanel` / `compare/noDownloadChrome`
- Bootstrap 源码 ratchet：`portability.NewService(`、`NewIsolatedDocumentPDFRenderer(`、`NewDeletionAdapter(`、`NewAuthoritativeProjectionRebuilder(`、禁止 `AdmissionGateFunc(`

---

## 7. Wrong vs Correct

- Wrong：center 进程内再写一套 comparison JSON 或从 Markdown 抽指标
- Correct：只消费 `comparison.result/v1` 的 `Export` / `Summarize`
- Wrong：导入信任 archive 内的 role/authorization
- Correct：本地 operator/ownership 只来自当前 `ActorScope`；导入身份字段必须为空

### Known residuals

- Export preview 的完整请求仍缓存在进程内存；center 重启后未发布 preview 不能 create（fail-closed）
- `0058` **export** artifact 仍无独立 blob version 列；S3 导出 Open 依赖 lease map。**import** artifact 已持久化 `object_version_id`，apply 可在清掉进程缓存后从 Local/S3 重读 archive
- Create 若 staging 成功但 Publish 失败，export job 可能停在 `staging`
- 生产 PDF 走 `contentProcessorPDFBinary()` → `houfeng-content-processor`（`ValidateIsolation` + 禁网）；`NewIsolatedDocumentPDFRenderer("")` / 进程内 `WriteDerivedPDF` 只留测试
- 官方 archive `records/{id}/evidence/{evs}.json` 必须是 restore wrapper；Export 无法 wrap 时点名为 `unavailable` 且不写入该成员。Apply 对非 wrapper 的 `evidence_json` fail-closed。`knownKindEvidenceImporter` 仍只做 schema 门。`comparison.result_v1.json` 保持原始 `Kind.Export` 字节，不当第二份 snapshot
- 官方 archive 只纳入 `AdmitContent` 可恢复的附件；不支持的类型 preview 点名为 `unsupported` 且不进 ZIP。超限点名为 `over_archive_limit`。Apply 经 `AdmitContent` + `NewBlobTemporaryKey` + BlobStore + `ImportedAttachments` 在同一笔 finishing 事务插入 available 行再绑定。不信任 archive MIME/path。Activity 页不进 ZIP（已放弃）
- 生产 `NewAuthoritativeProjectionRebuilder` 不另起 rebuild worker：search/activity 已在 `ImportDocuments` → `SaveRevisions` 同一事务内投影；导入 checkpoint 仍被拒绝
- `HOUFENG_MINIO_INTEGRATION=1` / `HOUFENG_POSTGRES_INTEGRATION=1` 集成套件由 Child 11 在有环境时跑；本 child 只保证测试存在且无环境 skip
- Witness 池：bootstrap 在 `HOUFENG_DELETION_WITNESS_DATABASE_URL` 有值时打开；未配则 reader 对 nil witness fail-closed

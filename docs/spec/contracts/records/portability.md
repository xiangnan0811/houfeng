# Record Portability Contract

> 项目权威入口为根 `AGENTS.md`；领域行为见 [合同索引](../README.md)。

---

## 1. Scope / Trigger

相关集成 落地记录可移植性：具名 `AdmissionGate`、witnessed tombstone reader、`0058` 整表、人读 Markdown / 机器 ZIP64 archive / 派生 PDF、导入 dry-run/apply、origin tombstone、`record_portability` 删除 adapter。比较工作台 `/records/compare` 不是下载面。

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

`dry-run` 保持 `application/zip` 原始请求体、64 MiB 上限与 `Idempotency-Key`，
必须且仅能携带各一个非空 query：`destination_subject_kind`（`vps` /
`monitoring_instance` / `target`）和 `destination_subject_id`（合法稳定 ID）。
缺失、重复、未知参数或非法引用返回 400 `invalid_request`。Apply 仍只接收
`{lock_version}`，不能覆盖预检主体。计划必需返回
`destination_subject: {subject_kind, subject_id}`，不返回客户端拥有的身份快照或授权。

### Domain

- `records.Application.ExportDocument` / `ImportDocument`
- `evidence.ComparisonResultKind.Export` / `Summarize` — 比较导出唯一权威
- `recordmarkdown.SafeDocumentHTML` / `WriteDerivedPDF` / `ExtractDerivedHTML`
- `portability.WriteArchiveV1` / `ReadArchiveV1`
- `store.NewDeploymentMembershipAdmissionGate`

### 表（`0058` 建表；`0059` 只改 musl 安全的 `blob_key` CHECK）

`record_export_jobs`、`record_export_artifacts`、`record_import_jobs`、`record_import_plans`、`record_import_artifacts`、`record_import_entity_mappings`、`record_origins`、`record_origin_tombstones`、`record_portability_purge_receipts`

---

## 3. Contracts

- 导出 kind：`markdown` / `comparison_json` / `evidence_json` / `archive` / `pdf`
- Archive format：`houfeng-record-archive/v1`；manifest 路径固定 `manifest.json`；ZIP Store；成员上限 256、单文件 8MiB、整包 64MiB、压缩比 ≤100、深度 1（拒绝嵌套 archive）
- Comparison JSON 字节必须等于 `ComparisonResultKind.Export` 且等于 canonical snapshot；禁止 `conclusion` / `markdown` / `body_markdown`
- PDF 是同一 RenderModel 的派生展示（`houfeng-derived-presentation/v1`），不是机器权威，不能当 archive 读入
- 导入：dry-run 写 0 条 records/evidence/search/activity 行；apply 经 `ImportDocumentsFinishing` 把文档、`record_origins` 与 job 终态放进同一笔 `RunRecordPlatformTransaction`（origin 冲突在写记录前判定；失败可按原计划重试）；`LoadImportJob` / `ClaimImportJob` 现有行必须读出 `actor_id`，Apply 在 actor 为空或与计划不符时 fail-closed（含已 applied 回放）；官方 evidence 成员按 `kind` + `schema_version` 识别；markdown 正文允许普通 URL/英文动词，只拒 `javascript:` / `file://` / `data:` URI；JSON 只拒顶层 authorization/role/renderer/sql/password/token/path/url；checkpoint 成员拒绝；required vs optional 由本机 registry 决定，archive 的 `optional:true` 不可信；未知 schema fail-closed；已知 comparison/evidence remap 后走 `EvidenceImporter`
- 导入 Markdown 中的自定义 evidence/attachment 引用：归档内有对应成员时改写为本次计划的目标 ID；不在归档或不支持的材料仅保留可读标签与不可用材料提示，不保留可激活的来源 ID 绑定。
- 导入目标由用户显式选择，归档内所有记录统一关联该主体；不能从页面上下文、来源 ID 或首项推断。每条文档恰好传入一个 `registry_version=1`、`role=affected`、`primary=true` 的引用，由 RevisionService 重新解析并生成服务器身份快照，归档中的身份与授权不可信。
- Dry-run 在 claim/stage 前校验当前 import 能力、解析目标并按 project-wide 可见性授权；Apply 在恢复证据/附件前复核，提交前 RevisionService 仍执行 RecordCreate 授权。已 applied 回放也须先验证 actor 与当前 import/source 访问权，不能靠旧预检授权披露结果。缺失/无权访问保持 opaque 404；依赖不可用返回 503。最终 RevisionService 主体重解析错误沿用同一分类：无效引用为 400，缺失或拒绝为 opaque 404，适配器/依赖不可用或无效解析结果为 503；该最终复核失败不得写入 record、origin 或推进 import job。
- 已 applied 的 Apply / DryRun 重放在 admitted READ COMMITTED 事务中取得 comparison fence，再锁定并复核当前 actor、membership、job 与目标路由；等待期间发生撤权时不得披露结果或继续确认。随后从已提交的 birth 结果恢复精确确认，不重新分配 ID、读取归档或写入内容；APP commit 回应丢失及 W2 确认失败/丢响应都可按原身份重试，仍未知时保留 certification pending。当前授权使用当前成员关系，原始出生确认身份保持不变。
- 已安装 lineage 的导入分配、pending 重放及新增 W2 确认，在产生效果前通过独立 reader 取得新鲜 continuity，再在实际 APP 命令中核 canonical ancestry/source/binding。reader 故障不能将原 certified 请求改为 ordinary；已完成且仅返回授权结果的路径不借此补发 I/O。出生 producer 的运行时不可用与历史连续性未知是不同状态，见 [核心出生协议](core-schema.md#出生请求与外部连续性)。
- 导入和其他主体消费者复用 [授权合同](authorization.md) 的 exact W2 terminal、APP barrier 与当前 generation pin 验证及当前 actor 的 capture ∩ last-live ∩ final-floor 交集；不能把 archive 中的来源声明或旧 tombstone 当当前授权，也不能因 witness 可用而跳过本地权限复核。
- `0070` 仅为 `record_import_jobs` 追加可空目标 kind/source ID 列：两者同时 NULL 或同时合法，旧行不猜测回填。plan 经 job 关联读取唯一主体来源。Claim 的同 key 重放同时比较 raw ZIP SHA 和 canonical 主体 tuple；并发首次 claim 同样比较，SavePlan 在事务内核对主体，差异返回 409 `import_cas_conflict`。
- 计划摘要采用 `houfeng.record-import-plan.v2` 固定编码域，包含完整 canonical 主体 tuple 和 documents/evidence/attachments/remaps/quarantine，不包含可变名称或授权快照。raw ZIP SHA 仍单独用于 artifact 校验与 origin/tombstone；零 raw 摘要直接拒绝，不能用 plan digest 代替。
- 无目标主体的旧计划或摘要不符的计划在重绑、staging、Apply、已应用快捷返回前 fail-closed，返回 409 并要求新 key 重新预检。重启从 job 恢复主体，读取 Local/S3 object-version artifact、核验 raw SHA 后重绑并核验 v2 digest；改变目标主体不能绕过原始 ZIP 的重复导入或墓碑限制。
- 缓存 Apply、重启恢复和 DryRun 重放对持久计划摘要不符统一返回 409 `import_cas_conflict`。job 的主体或 raw SHA 与既有出生分配绑定不符时，在额外 artifact I/O 前拒绝；实际归档字节校验或解析失败仍为 400 `invalid_archive`，不能混为计划冲突。
- Web 文件及主体默认均为空；三类资产目录分别呈现加载、空目录和可重试失败。文件/主体变化清空旧计划并更换 key，同一选择的网络重试保留 key，显式重新预检及旧计划 409 使用新 key。过期请求不可发布到新选择，预检/应用在途禁用选择控件；计划展示服务端 echo，确认仅发送 plan ID/lock version，失败保留选择，成功链接已导入记录。
- Origin tombstone 用 archive SHA-256；dry-run 与 apply 都查 tombstone 和已有 `record_origins`；purge 写入墓碑
- `sensitive_topology` 需要 `record.export_sensitive_topology` 与 preview 签发的 confirm token
- 生产 bootstrap 禁止 `store.AdmissionGateFunc(`
- `LeasedBlobStore` Stage/StageImport 接受已认证 origin/job CAS 的 `WriteRequest`，通过 `ManagedBlobWriter` 写入；S3 临时 key 使用 `attachments.NewBlobTemporaryKey()`，禁止 `import/{job}` / `export/{job}`。附件与 staging 共用实际 namespace，不能通过用途标签绕过共享字节保护。
- `0059` 把 `blob_key` CHECK 改成 `char_length between 1 and 512` + `^[a-z0-9/._-]+$` + `not like '%..%'`，避免 musl `RE_DUP_MAX`；不要改 `0058` 原文
- `0081` 为 export/import artifact 持久保存 returned exact `object_version_id`、local namespace 和 obligation；发布在共同 SQL fence 内消费真实责任，重启读取不依赖进程 lease map。旧 export 缺失身份不回填猜测值，也不能作为物理清理证据。
- Export job claim 与 advance 通过受限命令参与统一 writer admission、comparison fence 和资源锁序，锁后复核持久 actor/record/revision 路由及 CAS。新导出效果与合法失败终态分开授权；预先读出的 material 或迟到 staging 结果不能绕过当前门禁。job 状态完成不替代物理 obligation 的独立完成证据。
- V2 local purge 以真实 operation owner tuple 准备并持久移交责任；共享 archive 的首个 record 只移交自身 target，最后合法 owner 移除后才封口整体责任。owner/job 删除后 pending target 仍可恢复，retained receipt 不代表 absence。PDF workspace 仍有独立生命周期，不能用 Blob receipt 代证。

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
| destination missing/duplicate/unknown query or invalid reference | 400 `invalid_request` |
| destination missing or inaccessible | `ErrExportUnauthorized` → opaque 404 |
| destination adapter/dependency unavailable | `ErrExportUnavailable` → 503 |
| legacy destination-free plan / v2 digest mismatch / same key different destination or ZIP | `ErrImportCASConflict` → 409；新 key 重新预检 |
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
- 三类目标唯一 primary affected、预检授权前无 claim、Apply 权限撤销无记录/origin/applied、同 key 主体/ZIP 并发冲突、旧 NULL 计划拒绝、Local/S3 重启 v2 摘要一致、真实 RevisionService/PostgreSQL finishing 原子提交与回滚。
- Web 选择前不可预检、迟响应不串选择、改选后旧 plan 不可应用、网络重试 key 稳定、Apply 不允许覆盖目标主体。
- Bootstrap 源码 ratchet：`portability.NewService(`、`NewIsolatedDocumentPDFRenderer(`、`NewDeletionAdapter(`、`NewAuthoritativeProjectionRebuilder(`、禁止 `AdmissionGateFunc(`

---

## 7. Wrong vs Correct

- Wrong：center 进程内再写一套 comparison JSON 或从 Markdown 抽指标
- Correct：只消费 `comparison.result/v1` 的 `Export` / `Summarize`
- Wrong：导入信任 archive 内的 role/authorization
- Correct：本地 operator/ownership 只来自当前 `ActorScope`；导入身份字段必须为空

### Known residuals

- Export preview 的完整请求仍缓存在进程内存；center 重启后未发布 preview 不能 create（fail-closed）
- 旧 export artifact 缺失 exact version/provenance 时 fail closed；只有新受管 artifact 支持持久身份重开。
- Create 若 staging 成功但 Publish 失败，export job 可能停在 `staging`；其真实物理 attempt 由 durable obligation 保留，不能将 job 失败或删除当作字节已清除。
- 生产 PDF 走 `contentProcessorPDFBinary()` → `houfeng-content-processor`（`ValidateIsolation` + 禁网）；`NewIsolatedDocumentPDFRenderer("")` / 进程内 `WriteDerivedPDF` 只留测试
- 官方 archive `records/{id}/evidence/{evs}.json` 必须是 restore wrapper；Export 无法 wrap 时点名为 `unavailable` 且不写入该成员。Apply 对非 wrapper 的 `evidence_json` fail-closed。`knownKindEvidenceImporter` 仍只做 schema 门。`comparison.result_v1.json` 保持原始 `Kind.Export` 字节，不当第二份 snapshot
- 官方 archive 只纳入 `AdmitContent` 可恢复的附件；不支持的类型 preview 点名为 `unsupported` 且不进 ZIP，超限点名为 `over_archive_limit`。Apply 经 `AdmitContent`、managed writer 与 `ImportedAttachments`，在同一 finishing 事务发布 available 行、绑定真实 record/epoch 并结算 quota；已 applied replay 不再 Put。不信任 archive MIME/path。Activity 页不进 ZIP（已放弃）。
- 生产 `NewAuthoritativeProjectionRebuilder` 不另起 rebuild worker：search/activity 已在 `ImportDocuments` → `SaveRevisions` 同一事务内投影；导入 checkpoint 仍被拒绝
- `HOUFENG_MINIO_INTEGRATION=1` / `HOUFENG_POSTGRES_INTEGRATION=1` 集成套件由 相关集成 在有环境时跑；本 child 只保证测试存在且无环境 skip
- Witness 池：bootstrap 在 `HOUFENG_DELETION_WITNESS_DATABASE_URL` 有值时打开。未 enroll 的 APP 在没有冲突历史时可返回 tombstone-not-found；已 enroll 时缺失或不可用 witness、未完成 handoff、unknown 或不匹配的 barrier/pin/terminal 均 fail-closed，不能当作无退休历史。

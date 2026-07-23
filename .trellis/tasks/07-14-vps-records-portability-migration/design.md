# 导入导出与 legacy 迁移设计

## 1. 边界与现状

当前仓库只有 `cmd/houfeng-import-vps-json` + `internal/center/importing/`，它处理provider/VPS/subscription严格JSON dry-run/import，不理解records revision、evidence、attachment、authorization、tombstone或删除ledger。现有 `experience_logs` 来自 `0022_create_experience_logs.sql`，字段为ID、VPS、category、severity、summary、details、occurred/created time，并对VPS使用`ON DELETE CASCADE`。仓库没有业务导出artifact store、PDF worker、通用archive manifest或record import API。

本任务新建 `internal/center/portability/`，不扩展现有资产JSON importer。功能直接依赖是子任务 2–9：records core、attachments、evidence、Markdown、search、activity、comparison与collaboration；它们不仅决定父任务顺序，还分别提供本任务实际调用的transaction/provider/schema合同。本任务拥有0058/0059，并向recorddeletion/recovery注册export、import和legacy adapter。

## 2. 包与数据流

```text
recordauth ─────────────────────────┐
records/render/collab ──────────────┼─> portability preview/export ─> ArtifactStore/processor
evidence registry + comparison v1 ─┤
attachments/blob ───────────────────┘

untrusted archive ─> registered quarantine ─> validate/scan/dry-run plan
                  ─> identity guard + fresh ledger/auth ─> records transaction
                                                          ├─> search revision participant
                                                          └─> canonical record_imported activity ─> activity adapter

experience_logs ─> fresh ledger/origin check ─> records transaction + legacy mapping
                                                   ├─> search revision participant
                                                   └─> canonical backfilled activity ─> activity adapter
```

- `portability`只通过versioned provider读取records/evidence/attachments/collaboration/activity，不直接猜schema JSON；`comparison.result/v1`通过comparison dependency注册的renderer/Summarize/Export与typed decoder参与同一registry，canonical activity通过activity service在其返回的完整`ActivitySnapshot{ProjectionGeneration, PublishedIngestSequence, ReadinessDigest}`和record authorization下读取，consumer不得零值/重算digest。
- human renderer复用dependency-created `records.RenderModel`；PDF和archive unpack复用`ContentProcessorWorkspace`。
- `ArtifactStore`使用与BlobStore相同的local/S3字节语义但独立root/prefix/bucket policy，避免临时导出/导入自动进入正式Blob manifest。
- 所有job先写应用DB identity/ref；所有workspace先登记recovery inventory exclusion/managed-source结果，再写第一字节。
- import与legacy最终写入都调用records service participant chain，不使用store直插revision：search projector在revision transaction内更新current document；records domain activity分别写唯一`record_imported`或保留event time的backfilled legacy event，activity dependency的record-domain adapter按source identity幂等投影。`HistoricalImport=true|HistoricalMigration=true`禁止业务通知重放，但不抑制search/activity事实。
- HTTP遵循一文件一资源：record exports与record imports各自拥有handler/test/RouterOptions，job/plan只是imports资源的状态子路径，不用单个portability handler混合两类资源。

portability直接依赖activity child冻结的export-readiness接口，不读取checkpoint表猜状态：

```go
type ExportReadySourceAdapter interface {
	SourceAdapter
	AuthoritativeHead(context.Context, ExportScope) (SourceHead, error)
	Readiness(context.Context, ExportScope, SourceHead) (SourceReadiness, error)
}

type ActivityExportReader interface {
	Readiness(context.Context, ActorScope, RecordSelection) (ReadinessVector, error)
	ScanRecordPage(context.Context, ActorScope, RecordSelection, ActivitySnapshot, PageCursor) (ActivityPage, error)
}
```

`SourceHead`含source kind、versioned head/cursor+hash和observed time；adapter `Readiness`证明该head可连续读取且未被retention截断。`ReadinessVector`逐required source含authoritative head、adapter readiness、projected checkpoint/hash、`caught_up|lagging|failed|unknown`、last success、record-scoped readiness digest，并内含activity child计算的统一`ActivitySnapshot{projection_generation,published_ingest_sequence,readiness_digest}`。所有注册source都必须实现`AuthoritativeHead`与`Readiness`；missing capability、head不可线性比较、lag/error、generation/digest变化或vector漂移均返回`portability_safety_unavailable`，不得降级为当前projection快照或partial export。

## 3. `0058_create_record_portability.sql`

| 表 | 合同 |
|---|---|
| `record_export_previews` | actor/project、formats/record+revision/material IDs、mode、policy/content/inventory digest、file/byte/sensitivity summary、10m expiry；不保存正文 |
| `record_export_jobs` | preview、state/attempt/lease、format/mode、created by/time、failure code、published artifact、24h expiry、reservation epoch |
| `record_export_artifacts` | random storage locator、size/hash/manifest/signature metadata、published/purged time/receipt；purge后locator置空 |
| `record_export_record_refs` | job/artifact到record/revision/evidence/attachment/action/comment的反向引用，用于删除阻塞/清理 |
| `record_export_audits` | actor、mode、scope/category counts、confirmed/generated/downloaded time、external-copy disclosed、digest；无内容 |
| `record_import_jobs` | target deployment/project、uploader、`identity_classification=unknown|complete`、classification digest、state/lease/expiry/trust结果 |
| `record_import_artifacts` | upload part/original/unpacked/scanned对象hash/size/storage locator/workspace/expiry/purge receipt；未purge前均是受管副本 |
| `record_import_plans` | archive digest/version、target visibility、normalized identity/permission/capacity/trust-policy digest、witness sequence/hash、`normal_import|reimport_deleted`、version、1h expiry/state |
| `record_import_plan_identity_refs` | complete object/origin set、entity kind/source ID/proposed target ID/mapping state；classification事务一次写全 |
| `record_import_entity_mappings` | completed plan中每个source entity→target entity、可空content hash、generation；支持响应丢失幂等重建，target永久删除时清content hash并只保留origin/tombstone所需identity |
| `record_import_activity_sources` | imported historical activity的权威source seam：target record/origin generation、versioned typed source tuple及length-prefixed canonical identity digest、event kind/time/recorded/backfilled、mapped typed refs/correction、allowlisted actor/presentation/provenance、auth scope/canonical hash、reservation epoch；唯一tuple/digest且无raw payload/拼接identity |
| `record_origins` | target record与source instance/kind/record ID、可空archive digest、generation、import operator/time；源ACL/actor仅provenance，target永久删除时清archive digest/operator与其他content-derived metadata |
| `record_origin_tombstones` | app projection的canonical object/origin删除事实、ledger sequence/hash；无标题/正文/hash等内容字段 |
| `legacy_record_mappings` | `experience_log` ID→record/revision、migration state、legacy category/severity provenance；永久删除后只保留origin/status/ledger sequence并清业务metadata |
| `record_legacy_migration_runs` | run/watermark/count/digest、ledger head、started/completed、safe failure code；不复制summary/details |
| `record_portability_purge_receipts` | deletion/job、DB/artifact/workspace/legacy adapter版本、计数与digest |

0058还为machine import提供revision author provenance：current user revision保持`actor_kind=user`；legacy使用`system_migration`；archive作者使用`imported_provenance`和allowlisted identity snapshot，`import_operator_user_id`单独审计。foreign author不映射时不得把import operator伪装成作者。若core已提供等价列，0058只加约束/索引；若尚未提供，0058以additive列/表实现并把现有revision回填为`user`。

全部content-bearing artifact/job行在purge receipt后清locator/record refs；详细job refs最多30天。target永久删除还必须删除`record_import_activity_sources` rows，并清`record_import_entity_mappings.content_hash`、`record_origins.archive_digest/import_operator`及其他content-derived lineage；只长期保留父设计allowlist内的source instance/kind/stable ID、generation、target tombstone/ledger ref，不能保留archive hash、title、filename、evidence summary或自由文本错误。

## 4. 归档格式与完整性

### 4.1 `houfeng-record-archive/v1`

容器为ZIP64，entry name必须UTF-8、NFC规范化、`/`分隔、相对路径、无空段/`.`/`..`/控制符；normalize后路径唯一。symlink/hardlink/device和加密entry拒绝。生成器固定mtime为export `generated_at`、mode为0640/0750、路径按byte sort，确保同输入得到同manifest/file bytes。

```text
manifest.json
manifest.sig.json                  # 可选，不进入manifest file list
records/<origin-record-id>/record.json
records/<origin-record-id>/revisions/<revision-no>.json
records/<origin-record-id>/revisions/<revision-no>.md
records/<origin-record-id>/evidence/<snapshot-id>/envelope.json
records/<origin-record-id>/evidence/<snapshot-id>/payload.bin
records/<origin-record-id>/attachments/<attachment-id>/metadata.json
records/<origin-record-id>/attachments/<attachment-id>/content
records/<origin-record-id>/collaboration/actions.json
records/<origin-record-id>/collaboration/comments.json
records/<origin-record-id>/activity/events.json
```

manifest是typed struct的UTF-8 canonical JSON：固定字段顺序、无map、整数/UTC RFC3339Nano/NFC string、末尾单换行。它包含format/producer/min-reader version、instance/key metadata、record roots、schema registry、sorted file entries `{path,media_type,bytes,sha256,classification}`、activity source readiness vector与完整`projection_generation/published_ingest_sequence/readiness_digest` snapshot、export mode、generated time和root content digest。generator与parser都stream file entries并计算digest，不构造全量通用map；manifest hard cap 128MiB、parser working set 16MiB，每个非manifest metadata entity 1MiB，附件原字节不改写。200,000 entries是独立上限，任一byte/count上限先到即明确拒绝。

`manifest.sig.json`固定`algorithm=Ed25519`、signer instance、key ID、manifest SHA-256和signature。signature覆盖exact manifest bytes；验证结果`unverified|signature_verified`、`verified_at`和当时policy revision冻结为历史事实，当前`trusted|revoked|unknown`在每次GET/plan update/apply时从最新policy重算。checksum失败阻止；未签名/未知/revoked signer是明确warning并要求管理员确认，但仍不赋予包内声明任何本地权威。trust-policy digest变化使旧确认plan stale并要求重新审阅。

archive可含一个或多个显式选定record；human格式只允许单record。machine archive总数由预览文件/字节/target capacity限制，不靠静默截断。跨record copied-from lineage先列入manifest，再由import预分配target IDs保持引用。

### 4.2 版本兼容

v1 importer只接受format major 1且`min_reader_version<=current`；未知required entity/comment schema阻止整份plan。evidence先查registry support history：本实例曾支持/生成、manifest required、含typed refs或本次已成功decode的schema缺decoder时fail closed；integrity-valid且从未支持的external evidence也不能信archive自报classification，只保留在`record_import_artifacts` quarantine，dry-run/job读取allowlisted envelope的kind/schema/captured time/bytes/hash/unsupported reason，不创建evidence snapshot/record、不调用renderer/Summarize/Compare、不允许machine re-export。部署支持该decoder后重新dry-run生成新plan并走完整classification/forbidden conformance；取消/过期先purge opaque bytes。`comparison.result/v1`始终是required evidence schema：manifest列出其schema/version和typed refs，export调用kind Export，import在预分配target IDs后重映射original/copied snapshot refs并以kind decoder/conformance重新验证；缺ref、unknown version或无法无损重建的字段阻止整个plan。目标再导出的renderer/Summarize/Export material必须与source语义等价，允许变化仅限manifest声明的新target IDs和origin lineage。可选producer metadata可作为opaque provenance保留，但不进入renderer/权限。v1 decoder/conformance fixture长期保留；升级新增v2而不重新解释已生成v1。

`activity/events.json`保存固定watermark内按canonical sort排列的versioned envelopes和typed refs，不导出query cursor、checkpoint、worker lease或mutable projection state。import在target IDs预分配后把record/revision/evidence/subject/correction refs全部映射；source identity是含schema version、source instance、source kind、source event ID、source version和event kind的typed tuple，各字段按类型/长度前缀canonical编码后SHA-256，不使用分隔符拼接或locale folding。完整tuple、identity digest和canonical hash写`record_import_activity_sources`；数据库逐字段唯一约束与digest交叉核验，Unicode normalization/delimiter/空段碰撞或同tuple不同hash阻止plan。activity projector通过portability source adapter从该权威表幂等重建，并额外消费一个本地`record_imported` source event；rebuild不得重复archive event或把foreign event改称目标实例live fact。

## 5. 人类导出与 export pipeline

Markdown bundle使用：

- `record.md`（current revision或索引）；
- `revisions/NNNN.md`与差异索引（选择full history时）；
- `evidence/`中的allowlisted可读summary/static assets；
- `attachments/`中的有权原文件，文件路径为attachment ID +安全展示名；
- `collaboration/actions.md`与`comments.md`，删除评论只输出tombstone/metadata；
- `manifest.json`列出所有文件/hash/source unavailable状态。

PDF使用同一RenderModel/field classification，只渲染可读正文、evidence summary、action/comment timeline和attachment清单；附件bytes不隐式嵌入。Chromium通过固定binary/args、无shell、无network、私有profile/tmpfs生成。Markdown与PDF的semantic golden fixture必须一致。

pipeline：

1. preview重新授权record +所有source/material，解析revision scope与`safe|sensitive_topology`，计算exact IDs/hash/files/bytes/inventory，签10m token。
2. create在事务内重验token/permission/content/fence，写job/outbox/idempotency；向用户再次提示external copy。
3. worker取得object content lease，逐provider生成staging objects；activity provider先读取每个required source的typed `{source_kind, authoritative_head, projected_checkpoint, status, last_success, record_readiness_digest}`，只有全部caught-up且hash一致才消费reader返回的完整`ActivitySnapshot{projection_generation,published_ingest_sequence,readiness_digest}`并逐页读取选定record有权canonical events，禁止consumer自行构造或省略digest。outbox/head领先、failed/unknown adapter、generation/digest变化或readiness不可证明返回`portability_safety_unavailable`，没有partial mode。
4. 生成canonical manifest/hash/signature，publish前重新读取并逐字比较activity readiness vector与ActivitySnapshot，同时重验授权/fence；head/checkpoint/status/generation/content漂移使job stale/安全失败，只有稳定vector/snapshot才原子publish，partial不可下载。
5. 每次download重验权限、sensitive capability、artifact hash、record reservation并取得stream lease；token≤15m。

published artifact 24h后janitor purge；record permanent deletion立即取消job、撤销token、purge所有含目标refs的server artifacts。下载成功/结果未知设置`external_copy_disclosed`，删除不能声称召回。artifact prefix若无法证明排除backup/version/snapshot，注册为受管恢复源并进入最长窗口/official replay。

## 6. Import quarantine、dry-run与资源限制

`POST /api/record-imports/dry-run`在读multipart content前创建job/workspace，初始classification unknown；stream同时计算archive hash并写随机quarantine key。默认硬限制：compressed archive≤24GiB、expanded bytes≤24GiB、entries≤200,000、path≤240 UTF-8 bytes、nesting≤8、streamed manifest≤128MiB、manifest parser working set≤16MiB、每个非manifest metadata entity≤1MiB；attachment/evidence仍受各自50MiB/5MiB和target quota。部署可降低总量但不能超过编译hard cap；全部读取使用bounded reader/stream，不把archive或manifest载入内存。

验证顺序固定：container/path→manifest canonical/schema→file count/size/hash→signature/trust→Markdown parser→evidence registry support-history/decode/forbidden corpus→activity envelope/source identity/ref graph→attachment admission/scanner→identity/reference graph→auth/capacity/tombstone。先验证结构再解包，entry写入前再次检查normalized path与声明hash；实际总量超过manifest/limit立即取消并purge。任何unsupported evidence都阻止apply；integrity-valid且从未支持的external evidence只允许quarantine dry-run读取allowlisted envelope metadata，不能信其classification、创建snapshot或导出opaque payload。

dry-run输出：

- archive/producer/schema与每个file integrity；
- signature historical result/current trust；
- record/revision/evidence/attachment/collaboration counts；
- exact duplicate lineage/content conflict/tombstone；
- source actor/user/subject候选和unresolved状态；
- target visibility/group、mapped source auth intersection；
- logical/physical storage delta与dedupe；
- blocking errors、warnings与normalized object/origin set。

完整解析后单事务写全部identity refs + classification digest并转complete。unknown job存在时同target scope删除必须cancel/drain/purge或等待complete后重preview。plan 1h到期立即撤销apply lease并进入janitor；只有每个original/part/unpacked/scanned object和processor workspace receipt完成才算清理。

## 7. Identity/reference remapping 与 atomic apply

### 7.1 映射规则

- record/revision/evidence/logical attachment/action/comment/event全部预分配新target ID；物理payload/blob可按验证hash复用，但逻辑auth/audit不合并。
- actor/owner/participant/action assignee/comment author键为`source_instance_id + source_user_id`。管理员可显式映射target user；未映射则保留allowlisted historical identity snapshot并把live user ID留空。display name/username不触发候选自动选择。
- subject键为kind + source stable ID；只接受relation registry验证的显式target object。未解析保留identity snapshot+tombstone，无live route，不按名称重连。
- target visibility/group由当前管理员选择；foreign ACL仅provenance。mapped live source参与当前recordauth交集；unresolved source不会扩大scope。
- evidence保存原kind/schema/payload/time/source provenance并标`imported_historical`，不调用source writer。附件重新创建logical identity并经过scanner；copied-from只在同archive可验证映射内重建。
- archive canonical activity按manifest ref graph预分配event与全部typed target IDs，作为`record_import_activity_sources`中的`imported_historical`权威source rows保存；foreign auth/producer只是provenance，不授予权限或触发业务通知。portability activity source adapter按typed tuple/digest幂等投影，另追加一个且仅一个local `record_imported` event。
- followers/unread/notifications/deliveries不导入。mapped owner/participants由`CollaborationRevisionParticipant`在同一records revision transaction写本地自动follow source；follower/projection失败与revision/search/activity/idempotency一起rollback，禁止commit后reconciler补洞。`HistoricalImport=true`只抑制assignment/revision外投，不跳过本地follow事实，也不从foreign activity重放通知。

### 7.2 幂等与并发

plan绑定archive digest、target project/visibility、complete identity set、mapping/capacity/authorization/trust-policy digest和observed witness head。apply要求`Idempotency-Key`和plan version，按canonical identity排序锁`identity_mutation_guards`，再fresh authorize、linearized ledger/tombstone lookup、capacity、current signer trust和source resolution。

- 已完成同origin+digest normal import返回原mapping/target IDs，`created=false`。
- 同origin不同digest返回`import_origin_content_conflict`，不覆盖现有记录。
- 任一origin tombstoned时normal plan stale/blocked；`reimport_deleted`必须是新的plan/preview/确认，生成新target IDs和origin generation。
- apply先commit会原子推进identity epoch，使旧deletion preview stale；reservation先commit使apply 409。head前进但无相关tombstone不使plan无意义失效；head不连续/不可证明一律503 fail closed。

bytes先写final immutable key并登记pin，随后单个PostgreSQL事务写entity mappings、origins、records/revisions/refs/material/collaboration/current projection、search participant state、`record_import_activity_sources`、local `record_imported` source event与idempotency。activity read projection可由dependency worker在commit后消费，但全部权威source rows/events必须与revision同事务落库；任一步失败全部DB rollback，不能出现可搜索却无revision、已有revision却无activity source或相反状态。已写bytes转registered orphan并由janitor purge。commit成功响应丢失从idempotency/entity mappings重建同一结果。archive内任一record失败则整plan失败，不支持“跳过失败项”，避免破坏跨记录引用和悄然丢历史。

## 8. API 与授权

| Method / path | 合同 |
|---|---|
| `POST /api/record-export-previews` | record export / sensitive capability；format/mode/revision scope/record IDs，返回10m token/files/bytes/warnings |
| `POST /api/record-exports` | Idempotency-Key + preview token；202 async job，digest漂移409 |
| `GET /api/record-exports/:id` | 当前权限下状态/hash/expiry/safe failure；无权404 |
| `GET /api/record-exports/:id/content` | current auth + stream lease + external-copy确认；敏感模式重验独立capability |
| `POST /api/record-imports/dry-run` | project admin；multipart stream→registered job，返回202 |
| `GET /api/record-imports/:job_or_plan_id` | uploader或project admin；状态/dry-run plan，不返回quarantine path |
| `PATCH /api/record-imports/:plan_id` | project admin设置target visibility与显式identity/subject mappings，递增plan version/recompute digest |
| `POST /api/record-imports/:plan_id/apply` | project admin + Idempotency-Key；fresh auth/fence/ledger，全或无 |
| `DELETE /api/record-imports/:job_or_plan_id` | 取消、drain、进入purge；幂等 |

export job、import job和import plan分别使用`rex_`、`rim_`、`rip_`不透明ID前缀并永不复用；GET/DELETE只按已解析类型进入对应store，未知前缀统一404，不能通过类型探测泄露另一项目对象。

关键错误码：`export_preview_stale`、`export_renderer_unavailable`、`export_artifact_unavailable`、`import_archive_invalid`、`import_integrity_failed`、`import_schema_unsupported`、`import_scan_pending|rejected`、`import_identity_unresolved`、`import_plan_stale`、`import_origin_content_conflict`、`import_deleted_origin_requires_approval`、`portability_safety_unavailable`。外部无权统一404；冲突409；大小413；字段/容量422；安全状态不可证明503。

## 9. 删除、备份、外部副本与 official restore

recorddeletion registry只编排owner-package adapters，不让一个adapter跨领域直删。`PortabilityDeletionAdapter`在provisional reservation后阻止新preview/upload/worker/stream，按refs取消并drain active export/import；identity unknown job按scope阻塞，complete job按object/origin阻塞。delete commit durable后它只删除portability-owned `record_import_activity_sources`、0058 plan/job/content refs、server artifacts、original/parts/unpacked/scanned objects、PDF/browser profile，并清mapping content hash与origin archive digest/operator；activity owner adapter负责derived projection，records/evidence/attachments/collaboration/search各自负责本领域行。所有owner receipts齐全才`online_purged`，旧reservation epoch的source/projector只能丢弃/清理不能重建。

`LegacyRecoveryAdapter`同样位于`internal/center/portability/legacy_recovery.go`，但只拥有legacy origin边界：删除/恢复时清`experience_logs`正文/整行、约束`legacy_record_mappings`为最小tombstone状态、阻止旧handler/cache和迟到migrator复活；它不实现legacy内容导入、类型映射或CLI，这些只属于§10 `LegacyMigrator`。

已经下载export或用户自行保留的source archive是外部副本，只在preview/audit披露；受管server bytes不能被误称外部。S3 noncurrent/Object Lock、volume snapshot、backup中的artifact prefix属于有界恢复源，直到`recoverable_until`到期并出介质销毁证明。

`PortabilityReplayAdapter`在official restore步骤4只处理portability-owned export/import状态：

1. 清恢复出的export artifact/job/token；
2. 清import original/unpack/plan、`record_import_activity_sources`与迟到mapping，重建origin tombstone；activity projection由activity replay adapter清理；
3. 取消旧worker lease并核验任何source version/reservation epoch迟到commit失败；
4. 返回DB/artifact/processor receipts。

随后`LegacyRecoveryAdapter`按同一连续ledger区间清恢复出的legacy row/mapping/cache并返回独立receipt；其他records/evidence/attachment/collaboration/search/activity adapters各清owner范围。recovery coordinator只有收齐全部required owner receipts才推进applied watermark。

恢复到0058/0059之前的backup时，PortabilityReplayAdapter与LegacyRecoveryAdapter必须在应用普通migration、旧handler或worker启动前运行；未知adapter/contract version或任一owner receipt缺失阻止启动。应用migration runner不能在replay gate之前无条件执行legacy content conversion。

## 10. `0059` 与 legacy conversion

0059创建/约束ledger-gated candidate state和migration provenance，不在普通SQL apply阶段无条件复制summary/details。bootstrap/CLI顺序为：应用additive schema→从fresh primary+witness取得连续head→把origin tombstone/replay投影追平→运行`LegacyMigrator`。ledger不可用/断链/旧projection时records migration capability unavailable，旧experience读取可继续但新records route不开启。

每个eligible row在一个事务中：

1. 锁`experience_logs` row与canonical origin guard；查询mapping、reservation、app tombstone和fresh independent ledger。
2. 命中delete commit/tombstone：不读/复制正文，删除仍存在的legacy row并写`skipped_permanently_deleted`最小mapping/receipt。
3. 未命中：读取原字段，分配新record/revision ID，以origin mapping唯一约束防重。
4. 通过records service写revision 1、VPS primary relation identity snapshot、current projection、backfilled domain activity和mapping；同一commit调用search revision participant，activity record-domain adapter按稳定source identity接收保留原event time的backfilled event；设置`HistoricalMigration=true`只抑制collaboration通知，不跳过search/activity。
5. 提交后对原字段和target projection计算逐字段对账digest；失败run可重试。

映射固定：

| legacy category | record type | business status |
|---|---|---|
| `note` | `note` | 无 |
| `stability` / `network` | `troubleshooting` | 未知历史状态，保留空值并要求首次正式编辑时显式选择 |
| `support` | `provider_communication` | 未知历史状态 |
| `billing` | `billing` | 未知历史状态 |
| `migration` | `migration` | 未知历史状态 |
| `cancellation` | `note` | 无；原category保存provenance，不猜为已取消/维护完成 |

severity映射`info→low`、`warning→medium`、`critical→critical`，原severity同时保留migration provenance；summary原样为title，details原样为Markdown，occurred/created time不改。author kind为`system_migration`且user ID空，不能选择当前admin。type需要status但legacy未知的例外只允许`HistoricalMigration` revision；用户下一次正式保存必须选择当前合法状态，旧revision继续显示“历史状态未知”。

`cmd/houfeng-migrate-experience-logs`支持`-dry-run|-apply -format text|json`并明确分开累计与本次计数。固定watermark累计字段为`source_total/source_eligible_total/ineligible_total/mapped_total/tombstoned_skipped_total/difference_count`，恒等式是`source_total = source_eligible_total + ineligible_total`与`source_eligible_total = mapped_total + tombstoned_skipped_total`；`difference_count`统计缺mapping、unexpected mapping和逐字段digest mismatch，交付必须为0。本次字段为`scanned_count/created_count/already_mapped_count/tombstoned_skipped_count/mismatch_count/error_count`，其和必须等于`scanned_count`。重复apply只创建新增eligible rows，第二次`created_count=0`且已有映射进入`already_mapped_count`；final cutover由integration child先停止旧POST，再运行dry-run+apply+dry-run直到累计difference 0，不建立双写。

compatibility GET/timeline在缓存前用mapping/reservation/tombstone过滤；record deletion reservation后立即404，read_fenced后purge原行。恢复到迁移前旧库时先ledger replay清原行，再允许0059/migrator，防止原ID或新record ID复活。

## 11. Worker、配置、部署与 Web

worker：`RecordExportWorker`、`RecordImportWorker`、`PortabilityJanitor`、`LegacyMigrationReconciler`。全部使用recordplatform lease/attempt/next-attempt、deployment admission和reservation epoch。管理健康只显示队列/年龄/安全code/bytes bucket，不显示record ID、filename或path。

配置新增：artifact local root或S3 endpoint/bucket/prefix/credential source、24GiB import limits、Chromium binary、archive signer instance/key file、trusted signer policy file、job concurrency/TTL。key file要求0400，secret/path不进log。local root必须持久且与formal Blob root分离；processor tmpfs/no-core/no-network；无法证明backup exclusion时注册managed source。

Web canonical portability DTO追加到`web/src/lib/types.ts`，所有export/import transport扩展仅供lazy records routes消费的`web/src/lib/recordsApi.ts`；不得创建平行records type/API façade或让AppShell eager import该domain façade。新增`/records/import` static lazy route（必须在`:recordId`前），record detail加入export dialog。Import页面分为upload→scan/dry-run→mapping/visibility→confirm/apply→result；正常态只显示计划内容，不显示空warning/conflict区域。真实warning/error按plan动态插入，解决后DOM移除。390px使用单列stepper和modal/drawer，文件/映射表有具名局部滚动；Axe、keyboard、focus restore和44px通过。

## 12. Compatibility and rollback

0058/0059 additive；records/import/export feature默认关闭，旧experience write仍是唯一生产入口直到integration切换。rollback关闭worker/routes和legacy reconciler，保留已生成records/lineage/mapping只读，不执行down migration、不删除artifact inventory或tombstone。已发布archive保持v1可读；签名key轮换不改写旧manifest。旧UI只能通过fence-aware experience adapter回退，minimum fence contract不允许回退到会绕过tombstone的backend。

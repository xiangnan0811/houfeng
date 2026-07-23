# 导入导出与 legacy 迁移 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`. Codex inline execution only; do not dispatch implement/check sub-agents. Every production behavior follows RED → verify RED → minimal GREEN → verify GREEN.

**Goal:** 交付具有canonical完整性、安全重映射、幂等/原子apply、删除不复活和可重复legacy转换的记录导入导出平台。

**Architecture:** portability通过versioned provider读取records/material/collaboration与`comparison.result/v1`，使用独立ArtifactStore/processor生成human或machine artifacts；import先登记隔离副本并生成1小时plan，再以identity guard + fresh ledger通过records participant chain单事务写入search state和canonical activity source；legacy migrator只在official replay/fresh ledger gate后复制eligible rows并复用同一participant合同。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、stdlib archive/zip/json/SHA-256/Ed25519、local/S3 ArtifactStore、Chromium、React 19/TypeScript 6、Vitest/Playwright/axe。

---

## Preconditions and dependency review

- [ ] 确认功能直接依赖子任务 2–9（core、attachments、evidence、Markdown、search、activity、comparison、collaboration）全部已合入受保护主线且required CI/post-merge CI通过，并确认它们所基于的platform foundation已在所选主线；从最新主线创建非main分支并运行`sh scripts/setup-git-hooks.sh`。
- [ ] 确认已合入依赖拥有的0051–0057已冻结且`0058_create_record_portability.sql`、`0059_migrate_experience_logs_to_records.sql`可用；若主线占用未发布编号，只整体顺延仍未实施的0058–0060并同步portability/integration全部引用，绝不改号或改写0051–0057。
- [ ] 用`trellis-before-dev`读取backend database/error/logging/quality、web component/state/styling/quality、branch/cross-layer/reuse规范；逐一核对dependency提供的RenderModel、RevisionParticipant、BlobStore/processor、evidence registry、collaboration export、search projector/store、activity `ExportReadySourceAdapter`/`ActivityExportReader`/store、`comparison.result/v1` renderer/Summarize/Export、identity guard与deletion/recovery adapter签名。若已合入activity child没有authoritative-head/readiness接口，保持本任务planning并先由其owner补合同，禁止在portability猜checkpoint或实现旁路。
- [ ] 记录baseline：`make verify-go`、Node22 `make verify-web`、`go test ./internal/center/importing ./cmd/houfeng-import-vps-json ./internal/center/renewals ./internal/center/store ./internal/center/http/handlers -run 'Import|Experience' -count=1`；预期PASS且旧资产JSON importer行为不变。

## Task 1: Archive v1 types、canonical bytes与conformance corpus

**Files:**

- Create: `internal/center/portability/types.go`
- Create: `internal/center/portability/manifest.go`
- Create: `internal/center/portability/canonical.go`
- Create: `internal/center/portability/archive.go`
- Create: `internal/center/portability/signature.go`
- Create: `internal/center/portability/conformance.go`
- Create: `internal/center/portability/manifest_test.go`
- Create: `internal/center/portability/archive_test.go`
- Create: `internal/center/portability/signature_test.go`
- Create: `internal/center/portability/testdata/archive-v1/manifest.json`
- Create: `internal/center/portability/testdata/archive-v1/manifest.sig.json`

- [ ] 写小型RED golden tests固定design §4 file layout、typed field order、NFC/UTC/整数编码、sorted paths、manifest/root digests与Ed25519 bytes，同输入可重复100次。把1MiB metadata、128MiB streamed manifest、200,000-entry和16MiB working-set命名为独立`*LargeBoundary` tests，只构造一次，各+1 case稳定拒绝且不发布partial。
- [ ] 写hostile RED corpus覆盖绝对/`..`/空段/控制符/Unicode normalization collision、duplicate、symlink/hardlink/device/encrypted、truncated ZIP64、entry/size/path/depth上限和manifest/file mismatch。
- [ ] 实现typed canonical encoder、streaming archive reader/writer和signature envelope；不对业务map直接`json.Marshal`，不信ZIP metadata的声明大小。
- [ ] 运行小型确定性suite：`go test ./internal/center/portability -run '^(TestCanonicalDeterminism|TestManifestSmallGolden|TestArchiveSmallGolden|TestSignatureGolden)$' -count=100`；预期每轮hash相同。
- [ ] 单独运行大边界suite：`go test ./internal/center/portability -run '^(TestMetadataEntityLargeBoundary|TestManifest128MiBLargeBoundary|TestArchive200KEntriesLargeBoundary|TestManifestWorkingSetLargeBoundary)$' -count=1`；记录time/peak allocation，禁止放入任何`-count=100`或fuzz循环。
- [ ] 运行`go test ./internal/center/portability -run Fuzz -fuzz=FuzzArchivePath -fuzztime=60s`和`-fuzz=FuzzManifest -fuzztime=60s`；预期无panic、越界、unsafe accept。

## Task 2: 0058 schema、store、author provenance与ArtifactStore

**Files:**

- Create: `db/migrations/0058_create_record_portability.sql`
- Modify (existing): `internal/center/store/migrate/migrate_test.go`
- Modify (existing): `internal/center/store/migrate/postgres_integration_test.go`
- Create: `internal/center/store/record_portability.go`
- Create: `internal/center/store/record_portability_test.go`
- Create: `internal/center/portability/artifact_store.go`
- Create: `internal/center/portability/artifact_local.go`
- Create: `internal/center/portability/artifact_s3.go`
- Create: `internal/center/portability/artifact_conformance_test.go`
- Modify (created by core dependency): `internal/center/records/types.go`
- Modify (created by core dependency): `internal/center/store/records.go`

- [ ] 写migration RED tests逐表断言design §3 unique/check/index/TTL、classification原子性、origin non-unique lookup、mapping generation、内容列/长期allowlist、无source cascade和author kind/user nullable约束；`record_import_activity_sources`固定typed tuple/digest/target refs/canonical hash/reservation epoch、tuple与digest交叉唯一且无raw payload；mapping content hash与origin archive digest/operator可在target删除后清空，最小origin/tombstone identity仍可唯一阻止normal re-import。
- [ ] 实现0058 additive schema；现有user-authored revision回填`actor_kind=user`，import/system provenance不伪造user。真实PG fresh/0057-upgrade/repeat apply均GREEN。
- [ ] 写local/S3同一ArtifactStore RED conformance：conditional staging put、stream open、hash/size、atomic publish、token revoke、idempotent purge、version mismatch、partial/multipart cleanup。
- [ ] 实现local temp+fsync+atomic rename和S3 random staging→verified immutable publish；prefix/key不含record title/filename。
- [ ] 运行`HOUFENG_POSTGRES_INTEGRATION=1 HOUFENG_DATABASE_URL="$HOUFENG_DATABASE_URL" go test ./internal/center/store/migrate ./internal/center/store -run 'PostgresIntegration.*Portability|RecordPortability' -count=1`以及local/真实MinIO conformance；预期PASS，不接受SKIP作为证据。

## Task 3: Export provider registry、preview与human renderers

**Files:**

- Create: `internal/center/portability/providers.go`
- Create: `internal/center/portability/providers_test.go`
- Create: `internal/center/portability/export_preview.go`
- Create: `internal/center/portability/export_preview_test.go`
- Create: `internal/center/portability/export_markdown.go`
- Create: `internal/center/portability/export_markdown_test.go`
- Create: `internal/center/portability/export_pdf.go`
- Create: `internal/center/portability/export_pdf_test.go`
- Create: `internal/center/portability/activity_provider.go`
- Create: `internal/center/portability/activity_provider_test.go`
- Modify (created by Markdown dependency): `internal/center/records/render.go`
- Modify (created by Markdown dependency): `internal/center/records/references.go`
- Modify (created by evidence dependency): `internal/center/evidence/registry.go`
- Modify (created by comparison dependency): `internal/center/evidence/comparison_result.go`
- Modify (created by comparison dependency): `internal/center/evidence/comparison_result_kind.go`
- Modify (created by attachments dependency): `internal/center/attachments/backup_adapter.go`
- Modify (created by collaboration dependency): `internal/center/records/collaboration_export.go`

- [ ] 写provider RED conformance：records/revisions、evidence schema export、attachment bytes、collaboration history与fixed-watermark canonical activity均required；`comparison.result/v1`必须经其versioned renderer/Summarize/Export生成human和machine material。逐个activity `ExportReadySourceAdapter`验证`AuthoritativeHead`与指定head的`Readiness`，service只消费`ActivityExportReader.Readiness`返回的完整`ActivitySnapshot{projection_generation,published_ingest_sequence,readiness_digest}`比较head/checkpoint/status，再只返回与选定record有权关联的versioned envelopes；missing/zero/recomputed digest、missing capability、lag/failed/unknown、generation漂移、unauthorized provider必须阻止，不返回残缺artifact。
- [ ] 写preview RED matrix固定current/all history、Markdown/PDF/machine、safe/sensitive capability、exact IDs/hash/files/bytes、10m token、content/policy/inventory/fence漂移和external-copy确认；activity覆盖outbox/source head领先、failed/unknown adapter、checkpoint/hash/generation drift与caught-up vector/ActivitySnapshot绑定，任何不完整状态fail closed且无partial选项。
- [ ] 实现provider registry与preview service；默认safe且不沿用用户上次敏感选择，command output/forbidden corpus在所有provider输出命中0。
- [ ] 实现Markdown bundle与PDF RenderModel；Chromium固定binary/args、network off、无shell、workspace receipt。共享semantic fixture逐字段比较两种格式，并固定comparison conditions/system differences/warnings/provenance不丢失。
- [ ] `go test -race ./internal/center/portability ./internal/center/records ./internal/center/evidence ./internal/center/attachments ./internal/center/activity ./internal/center/store -run 'ExportProvider|ActivityExportProvider|ExportPreview|MarkdownExport|PDFExport' -count=10`；预期PASS。

## Task 4: Machine archive export、签名、worker与download lease

**Files:**

- Create: `internal/center/portability/export_machine.go`
- Create: `internal/center/portability/export_machine_test.go`
- Create: `internal/center/portability/export_service.go`
- Create: `internal/center/portability/export_service_test.go`
- Create: `internal/center/portability/export_worker.go`
- Create: `internal/center/portability/export_worker_test.go`
- Modify (created in Task 2): `internal/center/store/record_portability.go`
- Modify (created by platform dependency): `internal/center/recordplatform/leases.go`

- [ ] 写RED tests覆盖multi-record preallocation/cross refs、含`comparison.result/v1` typed refs和`activity/events.json`固定readiness vector/watermark/sort的canonical package、optional signer trust metadata、job idempotency、provider/render/write/sign/publish每个cutpoint、24h TTL和15m download token；在activity pagination后与publish前推进source/outbox head或标failed必须拒绝publish。
- [ ] 实现machine exporter与job service；staging files全部hash验证后才生成manifest/signature并publish，published前GET content稳定不可用。
- [ ] download在headers/first/middle/last chunk注入permission/reservation/content lease；撤权/fence后新header/byte为0，结果未知推进external-copy disclosure。
- [ ] 强杀worker并运行janitor，断言staging/partial/multipart/pin为0且每job有published或purge receipt。
- [ ] `go test -race ./internal/center/portability ./internal/center/recordplatform -run 'MachineExport|ExportWorker|ExportDownload' -count=10`；预期PASS。

## Task 5: Import registration、quarantine、安全验证与dry-run

**Files:**

- Create: `internal/center/portability/import_upload.go`
- Create: `internal/center/portability/import_upload_test.go`
- Create: `internal/center/portability/import_validate.go`
- Create: `internal/center/portability/import_validate_test.go`
- Create: `internal/center/portability/import_plan.go`
- Create: `internal/center/portability/import_plan_test.go`
- Create: `internal/center/portability/import_worker.go`
- Create: `internal/center/portability/import_worker_test.go`
- Modify (created by attachments dependency): `internal/center/attachments/admission.go`
- Modify (created by attachments dependency): `internal/center/attachments/workspace.go`
- Modify (created by evidence dependency): `internal/center/evidence/conformance.go`
- Modify (created by Markdown dependency): `internal/center/records/markdown.go`

- [ ] 写RED tests证明job在首byte前存在、unknown classification覆盖零/首/中/尾chunk和parse failure；小型case统一命名`TestImportSmall*`。24GiB/200k/240-byte/depth、128MiB streamed manifest、16MiB parser working set和1MiB per-entity metadata的真实大fixture统一命名`TestImportLargeBoundary*`，只单次运行；各+1 case清理quarantine且无业务写入。
- [ ] 将Task1 hostile corpus贯穿upload/unpack；附件重新调用admission/scanner，evidence按registry support history执行decode/forbidden conformance，Markdown走同一v1 parser。为`comparison.result/v1`覆盖unknown version、missing original/copied ref、hash mismatch和不可无损重建字段；为external never-supported evidence覆盖integrity-valid quarantine metadata fallback、apply/export阻止与cancel/expiry purge，以及曾支持/required/typed schema缺decoder时整plan阻止。signature verified不改变任何检查数量。
- [ ] 验证`activity/events.json` schema/typed source tuple/sort/watermark、record/revision/evidence/subject/correction refs、allowlisted presentation和foreign provenance；加入delimiter、空段、NFC/NFD、大小写/locale、长度前缀和digest collision corpus，断裂ref、tuple/digest不一致或同tuple不同hash阻止plan，不能把foreign event当local source fact。
- [ ] 实现validation pipeline和dry-run DTO，逐项输出integrity/trust/schema/support-history/fallback/mapping/auth/capacity/tombstone；任何unsupported schema都阻止apply，external never-supported fallback只从quarantine暴露kind/schema/time/bytes/hash/reason且不得创建snapshot/record/export。decoder可用后必须新建plan；取消/过期purge exact opaque bytes，不静默跳项或generic render。
- [ ] classification complete只在normalized object/origin全量+digest同事务落库后出现；取消/过期/崩溃drain worker并逐artifact出receipt。
- [ ] 运行小型重复suite：`go test -race ./internal/center/portability -run '^TestImportSmall' -count=10`及`go test -race ./internal/center/attachments ./internal/center/evidence ./internal/center/records -run 'ImportAdmission|ImportEvidence|ImportMarkdown' -count=10`。再单独运行`go test ./internal/center/portability -run '^TestImportLargeBoundary' -count=1`；预期PASS、受管残留为0，大fixture不得进入`-count=10|100`。

## Task 6: Identity mapping、fresh ledger与atomic/idempotent apply

**Files:**

- Create: `internal/center/portability/identity.go`
- Create: `internal/center/portability/identity_test.go`
- Create: `internal/center/portability/import_apply.go`
- Create: `internal/center/portability/import_apply_test.go`
- Create: `internal/center/portability/comparison_roundtrip_test.go`
- Create: `internal/center/portability/activity_roundtrip_test.go`
- Create: `internal/center/portability/activity_source.go`
- Create: `internal/center/portability/activity_source_test.go`
- Modify (created in Task 2): `internal/center/store/record_portability.go`
- Modify (created by core dependency): `internal/center/records/service.go`
- Modify (created by platform dependency): `internal/center/recordplatform/guards.go`
- Modify (created by collaboration dependency): `internal/center/records/collaboration_revision.go`
- Modify (created by search dependency): `internal/center/recordsearch/projector.go`
- Modify (created by search dependency): `internal/center/recordsearch/projector_test.go`
- Modify (created by search dependency): `internal/center/store/record_search.go`
- Modify (created by search dependency): `internal/center/store/record_search_test.go`
- Modify (created by activity dependency): `internal/center/activity/adapters/record_domain.go`
- Modify (created by activity dependency): `internal/center/activity/adapters/record_domain_test.go`
- Modify (created by activity dependency): `internal/center/activity/projector.go`
- Modify (created by activity dependency): `internal/center/activity/projector_test.go`
- Modify (created by activity dependency): `internal/center/activity/service.go`
- Modify (created by activity dependency): `internal/center/activity/service_test.go`
- Modify (created by activity dependency): `internal/center/store/record_activity.go`
- Modify (created by activity dependency): `internal/center/store/record_activity_test.go`
- Modify (created by comparison dependency): `internal/center/evidence/comparison_result.go`
- Modify (created by comparison dependency): `internal/center/evidence/comparison_result_kind.go`

- [ ] 写映射RED matrix：fresh IDs、explicit stable-ID user/subject mapping、unresolved provenance、foreign ACL inert、logical payload dedupe、foreign followers/notifications suppressed、mapped owner/participant local auto-follow和cross-record refs；在collaboration follower write注入失败，断言整个import transaction可见写入为0。
- [ ] 写幂等RED matrix：同origin+digest existing、同origin不同digest conflict、normal tombstone block、new reimport approval/generation、response loss、same key/different fingerprint。
- [ ] 写participant RED matrix：import revision commit与rollback、collaboration follower failure、multi-record中途失败、重复apply和response-loss retry；断言revision/current/search/local auto-follow/`record_import_activity_sources`/idempotency同事务全变或全不变，portability activity source adapter对每个typed tuple/digest只投影一次并另收到唯一local `record_imported`，历史导入notification为0。
- [ ] 写`comparison.result/v1` archive export→dry-run→apply→re-export roundtrip RED test；original/copied refs按manifest预分配映射，renderer/Summarize/Export material、conditions、system differences、warnings与provenance除声明的新target IDs/origin lineage外语义等价，unknown version或断裂ref不能产生可见record。
- [ ] 写canonical activity export→dry-run→apply→projector rebuild→re-export RED test；fixed watermark/sort、event/source identity、event/recorded time、backfilled、correction、typed refs、allowlisted presentation/provenance除target ID映射外语义等价，retry/rebuild重复projection与foreign notification均为0。
- [ ] 写delete/import interleaving RED tests：apply先commit推进identity epoch；reservation先commit使apply stale；head unrelated前进允许、相关tombstone/head gap fail closed；旧epoch worker不能commit。
- [ ] 实现sorted guard locking、fresh policy/witness/ledger/capacity recheck、bytes pin和单PostgreSQL transaction；正式revision只通过records service participant chain提交，不直插search表或activity projection。search participant在事务内更新current document，typed/canonical archive events写`record_import_activity_sources`，canonical local `record_imported` event与revision同事务保存，再由portability activity source adapter/projector按tuple/digest幂等消费。任一record失败整体rollback，orphan bytes登记janitor。
- [ ] 运行`go test -race ./internal/center/portability ./internal/center/records ./internal/center/recordsearch ./internal/center/activity/... ./internal/center/evidence ./internal/center/store ./internal/center/recordplatform -run 'IdentityMapping|ImportApply|ImportParticipant|ComparisonArchiveRoundTrip|ActivityArchiveRoundTrip|ImportDeleteRace' -count=20`；预期20轮PASS、double commit/visible partial/search或activity漏投/重复投影/comparison或activity字段丢失/notification replay均为0。

## Task 7: HTTP/router/bootstrap contract

**Files:**

- Create: `internal/center/http/handlers/record_exports.go`
- Create: `internal/center/http/handlers/record_exports_test.go`
- Create: `internal/center/http/handlers/record_imports.go`
- Create: `internal/center/http/handlers/record_imports_test.go`
- Modify (existing): `internal/center/http/router.go`
- Modify (existing): `internal/center/http/router_test.go`
- Modify (existing): `internal/center/http/router_api_test.go`
- Modify (existing): `cmd/houfeng-center/bootstrap.go`
- Modify (existing): `cmd/houfeng-center/bootstrap_test.go`
- Modify (existing): `internal/center/app/app.go`
- Modify (existing): `internal/center/app/app_test.go`

- [ ] 写handler RED matrix固定design §8 method/path、multipart streaming、Idempotency-Key、plan If-Match/version、strict body、202/200/400/404/409/413/422/503、response allowlist和static/dynamic路由优先级。
- [ ] 按backend“一文件一资源”分别实现record exports与record imports handlers/RouterOptions；任何client actor/project/trust声明被忽略，全部从session/config/server verification取得。
- [ ] bootstrap显式构造ArtifactStore/providers/services/workers/janitor并把portability activity source adapter注册到activity projector；feature off不要求artifact/signer配置且旧routes不变，feature on缺任一安全前置则capability fail closed。
- [ ] `go test ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center ./internal/center/app -run 'RecordPortability|RecordExport|RecordImport|Bootstrap' -count=1`；预期PASS。

## Task 8: Web export/import workflow与normal-state合同

**Files:**

- Modify (existing, extended by core dependency): `web/src/lib/types.ts`
- Modify (created by core dependency): `web/src/lib/recordsApi.ts`
- Modify (created by core dependency): `web/src/lib/recordsApi.test.ts`
- Create: `web/src/pages/records/portability/RecordExportDialog.tsx`
- Create: `web/src/pages/records/portability/RecordExportDialog.test.tsx`
- Create: `web/src/pages/RecordImportPage.tsx`
- Create: `web/src/pages/RecordImportPage.test.tsx`
- Create: `web/src/pages/records/portability/ImportPlanReview.tsx`
- Create: `web/src/pages/records/portability/ImportPlanReview.test.tsx`
- Create: `web/src/pages/records/portability/IdentityMappingEditor.tsx`
- Create: `web/src/pages/records/portability/IdentityMappingEditor.test.tsx`
- Modify (created by Markdown dependency): `web/src/pages/records/RecordDetailPage.tsx`
- Modify (created by Markdown dependency): `web/src/pages/records/RecordDetailPage.test.tsx`
- Modify (existing): `web/src/app/router.tsx`
- Modify (existing): `web/src/app/router.test.tsx`
- Modify (existing): `web/src/styles/partials/page.css`
- Modify (existing): `web/src/styles/partials/legacy-assets.css`

- [ ] 写API/UI RED tests覆盖preview modes/history、sensitive二次确认、external-copy warning、upload/scan/dry-run/mapping/apply/result、plan stale、tombstone approval、retry/idempotent existing和revoke。
- [ ] 实现`/records/import` static lazy route并置于`:recordId`前；RecordDetail只集成export trigger，所有network由route/dialog controller经lazy `recordsApi.ts`发起，AppShell/eager `api.ts`不增加portability helper。
- [ ] 正常plan不渲染warning/conflict/修复button/预留高度；只有真实项存在才插入对应section，resolve后DOM移除。empty/query-empty/error不是同一组件文案。
- [ ] 390px单列stepper、具名mapping/file scroll region、keyboard/focus/44px/no-overflow通过；运行`NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts src/pages/records/portability src/pages/RecordImportPage.test.tsx src/pages/records/RecordDetailPage.test.tsx src/app/router.test.tsx`，预期PASS。

## Task 9: Deletion/recovery adapters、janitor与备份边界

**Files:**

- Create: `internal/center/portability/deletion.go`
- Create: `internal/center/portability/deletion_test.go`
- Create: `internal/center/portability/recovery.go`
- Create: `internal/center/portability/recovery_test.go`
- Create: `internal/center/portability/legacy_recovery.go`
- Create: `internal/center/portability/legacy_recovery_test.go`
- Create: `internal/center/portability/janitor.go`
- Create: `internal/center/portability/janitor_test.go`
- Modify (created by core dependency): `internal/center/recorddeletion/service.go`
- Modify (created by platform dependency): `internal/center/recovery/replay.go`
- Modify (created by platform dependency): `internal/center/recovery/inventory.go`

- [ ] 写RED tests覆盖export/import每个state/classification、unknown scope blocker、download result unknown、server purge、external disclosure、retention和`not_committed` no-op；delete commit后断言portability-owned activity source为0、mapping/origin content-derived字段为NULL，最小tombstone仍阻止normal import。activity/search/records等非owner表若被本adapter直接DELETE则测试失败。
- [ ] 实现并注册`PortabilityDeletionAdapter`；receipt前portability-owned source/artifact/original/parts/unpack/scan/profile/cache残留或lineage敏感字段未清都阻止本adapter完成。derived activity/search及其他领域清理由各owner adapter返回独立receipt，recorddeletion收齐前不进入`online_purged`。
- [ ] 实现`PortabilityReplayAdapter`只覆盖0058 export/import tables、source rows、origin tombstone、worker leases和artifact backend；实现`LegacyRecoveryAdapter`只清legacy row/mapping/cache并拒绝late migrator。恢复出的非owner projection交给对应owner replay adapter；缺adapter/version、ledger gap或二次head前进失败阻止startup。
- [ ] 用备份cutpoint fixture恢复export published前后、import unknown/complete/apply前后及pre/post legacy conversion；逐owner验证receipt，重放后deleted bytes/rows/late commit为0且cross-owner direct delete为0。
- [ ] `go test -race ./internal/center/portability ./internal/center/recorddeletion ./internal/center/recovery -run 'PortabilityDeletion|PortabilityReplay|LegacyRecovery|PortabilityJanitor' -count=10`；预期PASS。

## Task 10: 0059 ledger-gated legacy import、CLI与旧API fence

**Files:**

- Create: `db/migrations/0059_migrate_experience_logs_to_records.sql`
- Create: `internal/center/portability/legacy.go`
- Create: `internal/center/portability/legacy_test.go`
- Create: `internal/center/store/record_legacy_migration.go`
- Create: `internal/center/store/record_legacy_migration_test.go`
- Create: `cmd/houfeng-migrate-experience-logs/main.go`
- Create: `cmd/houfeng-migrate-experience-logs/main_test.go`
- Modify (existing): `internal/center/store/renewal_decisions.go`
- Modify (existing): `internal/center/store/renewal_decisions_test.go`
- Modify (existing): `internal/center/http/handlers/vps.go`
- Modify (existing): `internal/center/http/handlers/vps_test.go`
- Modify (existing): `cmd/houfeng-center/bootstrap.go`
- Modify (existing): `cmd/houfeng-center/bootstrap_test.go`
- Modify (created by search dependency): `internal/center/recordsearch/projector.go`
- Modify (created by search dependency): `internal/center/recordsearch/projector_test.go`
- Modify (created by search dependency): `internal/center/store/record_search.go`
- Modify (created by search dependency): `internal/center/store/record_search_test.go`
- Modify (created by activity dependency): `internal/center/activity/adapters/record_domain.go`
- Modify (created by activity dependency): `internal/center/activity/adapters/record_domain_test.go`
- Modify (created by activity dependency): `internal/center/store/record_activity.go`
- Modify (created by activity dependency): `internal/center/store/record_activity_test.go`

- [ ] 写0059 source/PG RED tests证明普通migration apply不复制正文、只安装candidate/provenance/constraints；ledger/witness gate后migrator才读summary/details。fresh、0058 upgrade、repeat apply全部GREEN。
- [ ] 写table RED tests固定全部category/severity映射、原文本/时间/VPS snapshot、system actor、unknown business status、backfilled event、search/activity participants和notification suppression；search document必须随revision commit可见，activity source保留legacy event time并标`backfilled=true`，重复run的稳定source identity不得产生第二条projection。
- [ ] 写tombstone RED tests覆盖mapping已删、independent ledger origin命中、projection缺失重建、reservation、commit response loss、malformed legacy ID、增量row和重复run。
- [ ] 实现LegacyMigrator与store transaction；每row在分配ID前fresh ledger lookup，同一mapping唯一；mismatch进入report不覆盖既有record。
- [ ] 实现CLI strict flags/report；dry-run JSON同时校验累计恒等式`source_total=source_eligible_total+ineligible_total`、`source_eligible_total=mapped_total+tombstoned_skipped_total`和`difference_count=0`，以及本次恒等式`scanned_count=created_count+already_mapped_count+tombstoned_skipped_count+mismatch_count+error_count`。测试库运行`-apply`两次，第二次`created_count=0`且既有映射进入`already_mapped_count`。
- [ ] 让old GET/timeline在缓存前检查mapping reservation/tombstone，record reservation后404；只调用Task9已注册的`LegacyRecoveryAdapter`合同，不在Task10实现删除/replay清理。旧POST最终禁用仍留给integration child。
- [ ] 真实PG执行“migrate→delete→old GET/timeline/SQL→rerun”和“pre-migration backup→replay→0059/migrator”；summary/details命中与record复活均为0。
- [ ] 运行`go test -race ./internal/center/portability ./internal/center/recordsearch ./internal/center/activity/... ./internal/center/store -run 'Legacy|LegacyParticipant|BackfilledActivity|RecordSearch' -count=10`；预期search/activity漏投、重复投影和业务notification均为0。

## Task 11: Config/deploy/processor、浏览器与完整质量门

**Files:**

- Modify (existing): `internal/center/config/config.go`
- Modify (existing): `internal/center/config/config_test.go`
- Modify (existing): `compose.yaml`
- Modify (existing): `Dockerfile`
- Modify (existing): `.env.example`
- Modify (existing): `docs/deploy/compose.env.example`
- Modify (existing): `docs/deploy/local-and-systemd.md`
- Modify (existing): `docs/deploy/systemd/houfeng-center.service`
- Modify (existing): `scripts/docker-entrypoint.sh`
- Modify (existing): `internal/center/deploy/docker_static_test.go`
- Modify (existing): `web/e2e/fixtures/contracts.ts`
- Modify (existing): `web/e2e/fixtures/profiles.ts`
- Modify (existing): `web/e2e/fixtures/router.ts`
- Modify (existing): `web/e2e/accessibility.spec.ts`
- Modify (existing): `web/e2e/page-states.spec.ts`
- Create: `web/e2e/record-portability.spec.ts`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/directory-structure.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/database-guidelines.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/error-handling.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/logging-guidelines.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/web/directory-structure.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/web/component-conventions.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/web/state-and-data.md`

- [ ] 写config/static RED matrix：feature off、local/S3 separation、24GiB limits、Chromium固定path/no-shell、0400 signer key、trust policy、tmpfs/core/network、backup exclusion/managed-source fallback和unknown policy。
- [ ] 实现typed config/deploy wiring；feature on缺安全前置明确503，不能回退到container temp、formal Blob prefix或unsigned自称verified。
- [ ] 为export/import/legacy加入fail-closed browser fixtures；desktop/390、Axe、keyboard/focus/44px、normal-state no-anomaly、unknown API 501和revoke无旧DOM全部通过。
- [ ] 运行local+MinIO ArtifactStore、Chromium processor、真实PG migrations/legacy/replay、archive fuzz/race、`make verify-go`、Node22 `make verify-web`、`npm --prefix web run test:e2e -- --grep 'record portability|record import|record export'`、完整`npm --prefix web run test:e2e`和`git diff --check`；每项fresh PASS。
- [ ] 使用`trellis-check`审查父PRD全部portability/delete/restore/legacy acceptance、跨层auth、provider完整性、migration顺序、未决项和context drift；失败必须在本任务修复，不推迟到integration child。

## Review and rollback points

- Format review：v1 canonical bytes/path/schema/signature由golden fixture冻结；任何字段/排序变化需要format version决策，不能静默改写v1。
- Dependency review：子任务 2–9 均作为功能直接依赖核对；import与legacy不能绕过search/activity participant，canonical activity必须进入archive/import/rebuild，`comparison.result/v1`不能退化为generic JSON、unknown optional item或静默丢弃。
- Authorization review：preview/create/worker/download/dry-run/mapping/apply每阶段都执行同一recordauth与source intersection，foreign ACL永不生效。
- Import safety review：signed archive仍走全部scanner/parser/registry；任何unsupported schema、partial apply和display-name mapping均拒绝apply；external never-supported evidence只在quarantine走safe envelope metadata fallback，不创建record/snapshot/export且不可generic render。
- Deletion/recovery review：unknown classification、content-derived mapping/origin字段残留、artifact backup inclusion、ledger gap、adapter缺失和late worker任一存在时fail closed；外部副本与受管副本在文案/receipt中严格区分。
- Legacy review：0059不在fresh ledger gate前读/复制正文，system actor/unknown status不伪造，旧API从reservation开始fence，restore replay先于migrator。
- 回滚只关闭portability routes/workers/reconciler并保留0058/0059、lineage、tombstone和已导入records只读；不执行down migration、不删除archive兼容decoder、不让旧backend绕过minimum fence contract。

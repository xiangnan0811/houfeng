# VPS 详情与项目级记录中心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. This repository uses Codex inline execution; do not dispatch implement/check sub-agents. Every production behavior follows RED → verify RED → minimal GREEN → verify GREEN → refactor.

**Goal:** 在不牺牲信息密度、证据真实性与永久删除边界的前提下，交付任务导向 VPS 概览、项目级记录中心、不可变证据快照、Markdown 运维知识工作区、纵向时间线与横向比较工作台。

**Architecture:** 保持 Go center + PostgreSQL + React SPA 单体，通过 11 个可独立验收的子任务逐步建立 recordauth、records、attachments、evidence、search/activity/notify、portability、deletion ledger 与 recovery control 边界。所有用户入口在最终集成前由服务端 capability/feature gate 关闭；每个子任务经独立 PR 合入受保护主线，后续任务从最新主线继续，最终任务才切换默认路由并执行跨存储恢复与 staging 验收。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、MinIO S3 SDK、Goldmark/Bluemonday、React 19、TypeScript 6、React Router 7、CodeMirror 6、react-markdown、Vitest/Testing Library、Playwright/axe、Docker Compose/systemd。

---

## 0. 本计划的授权边界

- [x] `prd.md`、`design.md` 与 `research/visual-design-contract.md` 已于 2026-07-14 获用户整文批准。
- [x] 11 个子任务已创建且保持 `planning`。
- [x] 用户曾授权并启动平台基础旧合同，隔离分支已有未提交实现；方案 A 修订后该分支与产品代码已冻结。
- [ ] 旧启动授权不覆盖本轮六份规划工件；最终审阅再次明确批准后只恢复同一平台子任务，不重复 `task.py start`，也不启动父任务或其他 child。
- [ ] 父任务只负责任务图、跨子任务合同和最终整体验收，不作为实现目标启动。
- [ ] 每次开始子任务前重新运行 `trellis-continue`，加载该子任务的 PRD/design/implement，再用 `trellis-before-dev` 读取适用规范。

### 0.1 会话与交接策略

- [x] 用户已确认规划与设计，并授权由 Codex 按最合理方式选择新会话或辅助 agent 组织后续工作；方案 A 修订仍需新的最终执行批准。
- 当前长会话作为控制会话，维护子任务依赖、主线/迁移漂移、PR/CI/release/staging receipt 和最终父任务门禁，不在多个共享 checkout 中并发修改产品代码。
- 每个子任务默认使用一个独立 Codex 新会话及隔离 worktree。新会话从当时最新、已验证的受保护主线和所需规划工件开始；该会话的主代理按 Codex inline 模式直接实施并直接运行正式质量检查，不把实现或 `trellis-check` 派给 subagent。
- 只有当前子任务完成受保护 PR、合并及要求的 post-merge 验证后，控制会话才创建或启动下一个依赖子任务；不得预先并发启动 11 个会话，也不得让后序 worktree 基于未合入的前序分支。
- 每次交接提示必须包含：精确子任务目录、依赖 merge receipts、起始 commit/branch/worktree、适用 `prd.md`/`design.md`/`implement.md`、禁止修改范围、迁移冻结点、验证命令与停止条件。新会话先运行 `trellis-continue`；从未启动的 child 在取得专属授权后执行唯一一次 `task.py start`，已冻结的平台 child 在新批准后恢复现有 branch/task 状态，绝不重复 start。
- 控制会话通过线程状态和交付报告收集结果，但不信任摘要代替证据；进入下一子任务前独立核对 git diff、测试输出、PR/check-run、merge ancestry、发布或部署 receipt。需要额外分析时，subagent 只承担有界、只读、可独立复核的代码库调查或设计审计，不写产品代码、不运行正式实现/检查流程。

## 1. 子任务与显式依赖

| 顺序 | 子任务目录 | 直接依赖 | 合并后提供的稳定边界 |
|---|---|---|---|
| 1 | `07-14-vps-records-platform-foundation` | 无 | recordauth policy、outbox/worker、删除账本/full witness、RecoveryTrustStore、首次激活/密钥治理、domain identity rotation/灾难恢复/transfer/typed receipts、八个 retention roots（双分类源、21 lifecycle、24 participant与 executable bindings）、managed filesystem/core-dump 证明、恢复清单、独立 `houfeng-record-platform-admin` 与 fail-closed gate |
| 2 | `07-14-vps-records-core` | 1 | record root、完整修订、私有草稿、类型/状态、引用关系、记录删除 reservation/read fence |
| 3 | `07-14-vps-records-attachments-storage` | 1, 2 | local/S3 Blob、附件上传/隔离/扫描/配额/下载、GC pin 与 backup/restore adapter |
| 4 | `07-14-vps-records-evidence-platform` | 1, 2 | evidence kind registry、capture intent、不可变快照与首批来源适配器 |
| 9 | `07-14-vps-records-collaboration` | 1, 2 | 负责人/参与者、行动项、评论、关注、站内/外部通知与安全重试 |
| 5 | `07-14-vps-records-markdown-workspace` | 2, 3, 4, 9 | Markdown/模板/引用块、安全阅读、协作组件集成、差异/冲突与材料工作区 |
| 6 | `07-14-vps-records-search-center` | 1, 2, 5, 9 | 服务端 Markdown 全文与协作结构化检索、记录中心、URL/游标、顶部搜索摘要 |
| 7 | `07-14-vps-records-activity-overview` | 2, 4, 6, 9 | canonical activity、评论/行动项合流、单主体时间线、VPS overview 两态读模型与路由 |
| 8 | `07-14-vps-records-comparison-workbench` | 2, 4, 5, 7 | 精确 revision/snapshot 选择、主体证据入口、可比性、同类证据对齐与另存记录 |
| 10 | `07-14-vps-records-portability-migration` | 2, 3, 4, 5, 6, 7, 8, 9 | 安全/敏感导出、机器归档、隔离导入、全部投影 participant、比较证据、identity guard 与 legacy 转换 |
| 11 | `07-14-vps-records-integration-rollout` | 1–10 | 跨存储 backup/restore/replay、删除不复活、性能/安全/视觉/staging 门禁与最终切换 |

执行顺序使用上表顺序；数字 9 提前到 5 之前，使 Markdown 工作区能直接集成协作组件，也让后续搜索、活动与任务 10 的协作导出合同拥有稳定来源。任务 3、4、9 在任务 2 合入后技术上可以分别规划；任务 5 必须等待 3、4、9，任务 6 再等待 5、9。Codex inline 模式一次只执行一个子任务；不得用并行分支绕过依赖合并与主线漂移检查。

### 1.1 `ChildSurfaceOwnerEntryV1` 规范 owner matrix

下表是 `security/record-retention/child-surface-owner-matrix.v1` 的唯一规范输入；实现只能按 `MergeOrdinal` 数值升序 canonical encode 这 11 行并计算 `OwnerMatrixDigest`，不能从 migration 编号、目录扫描或后续 registry 反推 owner。`DirectDependencies` 按 `uint16` 数值升序、去重；其余字符串列表按未经 Unicode/locale/case folding 的 canonical UTF-8 raw bytes 升序、去重；空列表编码为零项。`Kinds` 的闭合缩写映射为 `C=canonical_leaf`、`CL=managed_client_leaf`、`F=managed_file`、`PG=postgresql_column`、`S3=s3_control_property+s3_key_segment+s3_metadata`；编码前必须展开成完整 enum token，再按 raw bytes 升序，表中缩写顺序也按展开后首 token 显示，缩写文本本身不进入 digest。`*Families` 是唯一 child ownership；`LifecyclePolicyIDs` 和 `PurgeParticipantIDs` 是该 child 初始 delta 可引用的闭合 ID 集，不因跨 child 复用而转移 family ownership。每行 `InitialDeltaRequirement` 都固定为 `required_nonempty`。

| Child / slug | Ordinal | Direct deps | MigrationOwners | PostgreSQLFamilies | CanonicalSchemaFamilies | S3Families | ManagedFilesystemFamilies | ManagedClientFamilies | LifecyclePolicyIDs | PurgeParticipantIDs | Kinds | InitialDeltaRequirement |
|---|---:|---|---|---|---|---|---|---|---|---|---|---|
| `1 / vps-records-platform-foundation` | `1` | `[]` | `db/deletionledger/migrations/0001_create_deletion_ledger.sql`<br>`db/deletionwitness/migrations/0001_create_full_witness.sql`<br>`db/migrations/0051_create_record_platform_foundation.sql`<br>`db/recoverycontrol/migrations/0001_create_recovery_control.sql` | `app.record-platform-foundation/v1`<br>`deletion-ledger/v1`<br>`deletion-witness/v1`<br>`recovery-control/v1` | `child-retention-attestation/v1`<br>`deletion-ledger/v1`<br>`domain-identity/v1`<br>`managed-storage-control/v1`<br>`record-platform/v1`<br>`recordauth/v1`<br>`recovery-governance/v1`<br>`retention-control/v1` | `candidate-control/v1`<br>`record-platform-witness/v1` | `platform-admission/v1`<br>`platform-approvals/v1`<br>`platform-backup-control/v1`<br>`platform-candidate/v1`<br>`platform-plans/v1`<br>`platform-restore-control/v1`<br>`platform-telemetry/v1`<br>`platform-transfer/v1` | `[]` | `lc_derived_owner_bound_v1`<br>`lc_ephemeral_absolute_expiry_v1`<br>`lc_permanent_immutable_governance_v1`<br>`lc_permanent_immutable_ledger_v1`<br>`lc_permanent_minimal_audit_v1`<br>`lc_platform_mutation_complete_30d_v1`<br>`lc_recoverability_bound_v1`<br>`lc_storage_control_permanent_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_deletion_ledger_v1`<br>`pp_record_platform_v1`<br>`pp_recovery_control_v1`<br>`pp_retain_same_v1`<br>`pp_retention_v1` | `C,F,PG,S3` | `required_nonempty` |
| `2 / vps-records-core` | `2` | `[1]` | `db/migrations/0052_create_records_core.sql` | `app.records-core/v1` | `record-core-purge-receipt/v1`<br>`record-core/v1`<br>`record-draft/v1`<br>`record-revision/v1`<br>`record-subject-reference/v1` | `[]` | `[]` | `[]` | `lc_derived_owner_bound_v1`<br>`lc_draft_last_activity_90d_v1`<br>`lc_owner_bound_authority_v1`<br>`lc_permanent_minimal_audit_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_records_core_v1`<br>`pp_retain_same_v1` | `C,PG` | `required_nonempty` |
| `3 / vps-records-attachments-storage` | `3` | `[1,2]` | `db/migrations/0053_create_record_attachments.sql` | `app.record-attachments/v1` | `attachment-processor/v1`<br>`attachment-purge-receipt/v1`<br>`attachment-upload/v1`<br>`record-attachment/v1`<br>`record-blob/v1` | `record-attachment-preview/v1`<br>`record-blob-final/v1`<br>`record-blob-upload/v1` | `attachment-backup/v1`<br>`attachment-processor/v1`<br>`blob-local/v1` | `[]` | `lc_ephemeral_absolute_expiry_v1`<br>`lc_owner_bound_authority_v1`<br>`lc_owner_reference_zero_24h_v1`<br>`lc_recoverability_bound_v1`<br>`lc_storage_control_permanent_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_attachments_v1`<br>`pp_blob_gc_v1`<br>`pp_content_processor_v1` | `C,F,PG,S3` | `required_nonempty` |
| `4 / vps-records-evidence-platform` | `4` | `[1,2]` | `db/migrations/0054_create_record_evidence.sql` | `app.record-evidence/v1` | `asset_history/v1`<br>`command_audit/v1`<br>`evidence-envelope/v1`<br>`evidence-purge-receipt/v1`<br>`ip_quality/v1`<br>`monitoring_event/v1`<br>`monitoring_timeseries/v1`<br>`subscription_budget/v1` | `[]` | `[]` | `[]` | `lc_absolute_15m_v1`<br>`lc_owner_bound_authority_v1`<br>`lc_owner_reference_zero_24h_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_evidence_payload_gc_v1`<br>`pp_evidence_v1` | `C,PG` | `required_nonempty` |
| `9 / vps-records-collaboration` | `5` | `[1,2]` | `db/migrations/0056_create_record_collaboration.sql` | `app.record-collaboration/v1` | `houfeng-comment-markdown/v1`<br>`record-action-event/v1`<br>`record-action/v1`<br>`record-collaboration-export/v1`<br>`record-collaboration-purge-receipt/v1`<br>`record-comment/v1`<br>`record-delivery/v1`<br>`record-follow/v1`<br>`record-notification/v1` | `[]` | `[]` | `[]` | `lc_comment_redaction_v1`<br>`lc_derived_owner_bound_v1`<br>`lc_notification_product_180d_v1`<br>`lc_owner_bound_authority_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_collaboration_v1`<br>`pp_record_notify_v1` | `C,PG` | `required_nonempty` |
| `5 / vps-records-markdown-workspace` | `6` | `[2,3,4,9]` | `[]` | `[]` | `IndexedDBDraftBufferV1` | `[]` | `[]` | `indexeddb-record-draft-buffer/v1` | `lc_managed_client_unsynced_24h_v1` | `pp_markdown_client_buffer_v1` | `C,CL` | `required_nonempty` |
| `6 / vps-records-search-center` | `7` | `[1,2,5,9]` | `db/migrations/0055_create_record_search.sql` | `app.record-search/v1` | `record-cursor/v1`<br>`record-search-document/v1`<br>`record-search-rebuild-receipt/v1` | `[]` | `[]` | `[]` | `lc_derived_owner_bound_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_record_search_v1` | `C,PG` | `required_nonempty` |
| `7 / vps-records-activity-overview` | `8` | `[2,4,6,9]` | `db/migrations/0057_create_record_activity.sql` | `app.record-activity/v1` | `activity-export-snapshot/v1`<br>`activity-purge-receipt/v1`<br>`record-activity-event/v1`<br>`record-activity-readiness/v1`<br>`record-activity-source-head/v1`<br>`record-activity-subject/v1`<br>`vps-overview/v1` | `[]` | `[]` | `[]` | `lc_derived_owner_bound_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_activity_v1`<br>`pp_vps_overview_v1` | `C,PG` | `required_nonempty` |
| `8 / vps-records-comparison-workbench` | `9` | `[2,4,5,7]` | `[]` | `[]` | `comparison.result/v1` | `[]` | `[]` | `[]` | `lc_owner_bound_authority_v1` | `pp_evidence_payload_gc_v1`<br>`pp_evidence_v1` | `C` | `required_nonempty` |
| `10 / vps-records-portability-migration` | `10` | `[2,3,4,5,6,7,8,9]` | `db/migrations/0058_create_record_portability.sql`<br>`db/migrations/0059_migrate_experience_logs_to_records.sql` | `app.legacy-record-migration/v1`<br>`app.record-portability/v1` | `houfeng-record-archive/v1`<br>`legacy-record-migration/v1`<br>`record-import-activity-source/v1`<br>`record-origin-tombstone/v1`<br>`record-portability-purge-receipt/v1`<br>`record-portability/v1` | `record-import-quarantine/v1`<br>`record-portability-artifact/v1` | `portability-archive/v1`<br>`portability-import/v1`<br>`portability-processor/v1` | `[]` | `lc_absolute_10m_v1`<br>`lc_absolute_1h_v1`<br>`lc_absolute_24h_v1`<br>`lc_ephemeral_absolute_expiry_v1`<br>`lc_permanent_origin_tombstone_v1`<br>`lc_recoverability_bound_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_legacy_v1`<br>`pp_portability_v1`<br>`pp_retain_same_v1` | `C,F,PG,S3` | `required_nonempty` |
| `11 / vps-records-integration-rollout` | `11` | `[1,2,3,4,5,6,7,8,9,10]` | `db/migrations/0060_record_platform_cutover.sql` | `app.record-platform-cutover/v1` | `integration-purge-receipt/v1`<br>`record-backup/v1`<br>`record-platform-cutover/v1`<br>`record-platform-gate-receipt/v1`<br>`record-restore/v1`<br>`records-child-delivery-manifest/v1`<br>`recovery-source-binding/v1`<br>`telemetry-inventory/v1` | `record-backup-artifact/v1`<br>`record-restore-derived-source/v1`<br>`record-telemetry-archive/v1` | `integration-backup/v1`<br>`integration-processor/v1`<br>`integration-restore/v1`<br>`integration-telemetry/v1` | `[]` | `lc_ephemeral_absolute_expiry_v1`<br>`lc_permanent_immutable_governance_v1`<br>`lc_recoverability_bound_v1`<br>`lc_storage_control_permanent_v1`<br>`lc_telemetry_max_30d_v1`<br>`lc_verified_purge_24h_30d_v1` | `pp_backup_v1`<br>`pp_integration_janitor_v1`<br>`pp_record_rollout_v1`<br>`pp_record_security_v1`<br>`pp_restore_v1`<br>`pp_retain_same_v1` | `C,F,PG,S3` | `required_nonempty` |

Child 5 是 `IndexedDBDraftBufferV1` 与 `indexeddb-record-draft-buffer/v1` 的唯一生产 owner；foundation 只提供 grammar/generator/conformance harness。Child 8 是 `comparison.result/v1` 的唯一 schema owner，但其物理证据清理由 Child 4 的 `pp_evidence_v1|pp_evidence_payload_gc_v1` 执行。Child 4 的已批准设计只生产 PostgreSQL/canonical gzip-bytea evidence，不拥有 managed-filesystem family；foundation 的 reserved render/capture grammar 不是 inventory，除非将来由实际 producer child 通过新的受审 delta 认领。

## 2. 迁移与外部存储编号

应用数据库迁移从当前最大 `0050` 后顺序分配。首个子任务启动前若主线占用计划编号，只整体顺延尚未合入的记录平台 migration并同步测试引用；任一子任务 migration 合入后即视为已发布、永不改名或改写，后续冲突只顺延当时尚未合入的 migration：

| 子任务 | 计划迁移 |
|---|---|
| 1 | `0051_create_record_platform_foundation.sql` |
| 2 | `0052_create_records_core.sql` |
| 3 | `0053_create_record_attachments.sql` |
| 4 | `0054_create_record_evidence.sql` |
| 6 | `0055_create_record_search.sql` |
| 9 | `0056_create_record_collaboration.sql` |
| 7 | `0057_create_record_activity.sql` |
| 10 | `0058_create_record_portability.sql`、`0059_migrate_experience_logs_to_records.sql` |
| 11 | `0060_record_platform_cutover.sql`（只收敛 feature/default 与兼容门禁，不删除 legacy） |

任务9按产品依赖先合入`0056`：它只用真实PostgreSQL验证0051–0054后、无0055时应用0056与repeat apply，并用独立migrator fixture证明“ledger已有较大文件名后新增较小文件名仍会被发现”，不得创建或应用真实0055。任务6随后才拥有真实`0055`，由它在ledger已记录0056的数据库上验证补应用0055与repeat apply。当前migrator以完整文件名逐项记账，因此这不是改写历史。每个阶段只顺延尚未发布编号：task9启动时冻结0051–0054；task7启动时冻结0051–0056；task10启动时冻结0051–0057；task11永远只顺延自身尚未合入的0060。若实施前migrator合同发生变化，则先重排当时所有未发布编号和文档，不能改写已合入migration或留下不可升级路径。

独立故障域不复用应用迁移账本：

- `db/deletionledger/migrations/0001_create_deletion_ledger.sql`
- `db/recoverycontrol/migrations/0001_create_recovery_control.sql`
- `db/deletionwitness/migrations/0001_create_full_witness.sql`

## 3. 固定依赖与准入

依赖只在拥有该能力的子任务中加入并由 lockfile/go.sum 固定：

- S3/Object Lock：`github.com/minio/minio-go/v7@v7.2.1`；local backend 不依赖 S3 配置。
- 服务端 Markdown：`github.com/yuin/goldmark@v1.8.4` + `github.com/microcosm-cc/bluemonday@v1.0.27`；原始 HTML始终关闭，sanitizer 作为第二道防线。
- Web Markdown：`@uiw/react-codemirror@4.25.11`、`@codemirror/lang-markdown@6.5.0`、`react-markdown@10.1.0`、`remark-gfm@4.0.1`、`rehype-sanitize@6.0.0`、`diff@9.0.0`；全部仅由记录 lazy routes 消费。
- PDF/复杂内容处理使用固定二进制路径与无 shell 参数调用；Docker 安装 Chromium/Poppler，systemd 文档要求显式配置。处理器缺失时对应能力 fail closed，不降级成不安全内联解析。
- ClamAV 使用窄 `INSTREAM` client 接口；需要压缩包/复杂附件时必须配置健康 scanner，否则上传保持不可引用隔离并最终过期清理。

## 4. 每个子任务的统一交付循环

- [ ] 从受保护主线最新提交创建 `codex/<child-slug>` 非主线分支；运行 `sh scripts/setup-git-hooks.sh`。
- [ ] 确认所有直接依赖已合入主线；若未合入，不启动子任务。
- [ ] 对从未启动的 child 运行唯一一次 `python3 ./.trellis/scripts/task.py start <child-dir>`；平台基础方案 A 只恢复既有 child/branch，不重复 start；任何情况都不启动父任务。
- [ ] 按子任务 `implement.md` 的 RED/GREEN 顺序实施，迁移、store、domain、handler、router/bootstrap、Web route、E2E 不跨步骤偷跑。
- [ ] 运行子任务 focused tests、`make verify-go`、`make verify-web` 和适用的 Playwright/PostgreSQL/MinIO 测试。
- [ ] 使用 `trellis-check` 完成 spec、复用、跨层和上下文漂移检查；修复后再次 fresh 验证。
- [ ] 更新 `.trellis/spec/` 中产生的新可执行合同；feature PR required check 使用 policy-pinned scanner 对 exact base/source tree 生成确定性 `ChildRetentionSourceClaimV1`，初始 11 个 child 必须各自提交 owner-matrix 内的 nonempty add-only `RetentionRegistryDeltaV1`，不得用 `no_new_surface`。
- [ ] 只允许以双亲 merge commit 合入 feature：first parent 必须是 claim base、second parent 必须是 claim source。受保护 main workflow 在不执行 merge-tree 代码的前提下验证最高 attempt required-check observation、重扫 merge tree、证明 production-input Merkle 与 `before+delta=after`，再由隔离 no-network/no-checkout signer 产生 `SignedChildRetentionMergeAcceptanceV1`。
- [ ] policy-bound metadata writer 只创建或 exact-match 一个 exact-one-file PR：`attestations/record-retention/v1/<ordinal>-<slug>/<feature-merge-sha>.acceptance.v1`。trusted-base metadata verifier 不执行 PR branch 代码，验证路径/body/signature/policy/previous digest/feature ancestry，metadata PR 自身不生成 source claim。
- [ ] feature merge、main CI 和对应 metadata-only PR 均合入后才归档当前 child、记录 commit/PR/check/claim/acceptance digests，并允许下一 child 从包含前一 acceptance 的最新 main 创建 base；缺失前一 acceptance 时下一 claim 必须失败。
- [ ] Task 11 PR 只验证已入库的 1–10 acceptance chain 和自己的 source claim/nonempty delta；它不能预签第 11 份。Task 11 feature merge 后按同一 protected workflow 生成并合入 ordinal 11 metadata PR，父任务再先按 merge ordinal 验证 1–11 连续 chain，最后才按 child ID 计算唯一 sorted registry union。

## 5. 跨子任务不可破坏合同

- `records` 当前投影必须可由 current immutable revision 重建；任何 root 同名字段都不能反向改写历史。
- `recordauth.Policy` 是列表、搜索、时间线、比较、通知、下载、导入导出与删除的唯一授权入口；前端 capability 只做发现性。
- evidence/attachment logical identity 不因 payload/blob 内容寻址去重而合并权限或审计。
- 任何读取正文、修订、摘要、证据、附件、比较或导出字节的入口都在缓存前取得 object content lease，并受 reservation/ledger fence 约束。
- 系统活动与人工记录只在 canonical activity projection 合流；自动事实不可由记录编辑器改写。
- 稳定态不渲染异常动作、容器或高度；异常事实存在时才动态插入，恢复后从 DOM 移除。
- 比较只在同 kind/schema/单位/语义允许时计算，不补零、不外推、不生成跨证据总分。
- 永久删除只在在线 purge receipt 完整后显示 `online_purged`；备份窗口、外部副本和 `attempt_not_committed` 文案不得夸大。
- 所有子任务新增的 PostgreSQL 列、canonical schema leaf、S3 property 或本地/远端受管 surface 必须同时登记到独立 canonical-schema/人工 retention-policy registries；任何未分类 surface 在合入前保持永久删除关闭，不能等任务 11 补登记。
- domain rotation 的完整 sealed intent、三类 identity-set V1 artifact、`DrainContinuation* → CandidateImportApplied → CandidateImportRevoked → Cutover` receipt chain、transfer/cutover/recovery-request exporter 的显式 recovery signing key、strict 0400 loader、zero-output failure 与 24/20/8 MiB chunk protocol 是跨任务恢复合同；后序 adapter 不得降级为 digest-only、单进程双凭据或 candidate 直写 current control plane。

## 6. 集成切换与回滚点

1. 子任务 1–4 合入后，所有记录 API capability 默认关闭；回滚只禁用 capability，不执行 down migration。
2. 子任务5–10合入后，所有新能力仍默认off；允许在本地/CI fixture做shadow验证，但认证staging不得使用branch image或在正式release前产生cutover receipt，旧`/experience-logs`写路径仍是唯一权威且不得双写。
3. 任务11先完成PR required checks（验证 1–10 acceptance + 自身 claim/delta）→双亲feature merge→main CI→protected acceptance→ordinal 11 exact-one-file metadata PR 合入→父任务按 merge ordinal 验证1–11连续chain并按child ID计算最终union；在这条链完整前不得批准/合并 Release Please、发布记录平台镜像或部署 staging。随后才执行 release image/五binary smoke，并以全部records flags=off部署精确digest、核对commit/version/digest和运行staging preflight/shadow。
4. 任务10的legacy migration在该staging dry-run对账为零差异后，任务11先切新写、再只读保留旧路径；命中deletion ledger/tombstone的旧行永不迁移。backup/restore不复活、性能/容量、视觉/人类理解、权限与遥测门全部通过后才把新概览/记录中心设为默认。
5. 回退 UI/路由只能回到 fence-aware compatibility API；账本 activation 后禁止低于 minimum fence-contract version 的 backend/worker 获得流量或队列。
6. `experience_logs` 只在旧备份窗口到期、恢复 replay adapter 仍可验证且用户另行批准数据清理后删除；本计划默认保留 tombstone/mapping 与只读兼容。

## 7. 父任务最终质量门

- [ ] `python3 ./.trellis/scripts/task.py list` 显示 11 个 child 均已完成/归档，父任务仍持有完整映射。
- [ ] 每个child都有merged PR/check-run/digest receipt，Git host与`git merge-base --is-ancestor <merge_commit> HEAD`证明其merge commit位于最终主线；任务11的release/staging receipts绑定同一发布commit/version/image/config。
- [ ] `attestations/record-retention/v1/` 恰有 11 份按 owner matrix 路径命名的 acceptance；先按 `MergeOrdinal=1..11` 验证 previous digest/registry before-after/feature ancestry 的连续 chain，对应 ChildID 序列必须是 `1,2,3,4,9,5,6,7,8,10,11`，再按 ChildID `1..11` 计算唯一 final union。metadata path 的 ordinal 前缀固定为两位十进制 `01..11`。第 11 份 metadata PR、最终 chain/union receipt 都早于 release approval、image publish 与任何 staging preflight/cutover receipt。
- [ ] `make verify-go`、`make verify-web`、`npm --prefix web run test:e2e` fresh 通过。
- [ ] PostgreSQL fresh install、0050 upgrade、legacy migration repeated apply 与真实 EXPLAIN 通过。
- [ ] local + MinIO/S3 backend contract、scanner/renderer processor、backup/restore cutpoint 与 deletion replay 全部通过。
- [ ] 10,000 records / 200,000 revisions / 1,000,000 activities 基准达到design §25全部门槛，包含comparison summary/detail到达率、96MiB单请求/512MiB aggregate admission、2秒等待、5秒drain与4GiB cgroup容量表。
- [ ] Artifact `vps-records-visual-contract/v1` 对闭合 `PageGroupV1={comparison_workbench,evidence_selector,markdown_editor,records_center_and_subject_timeline,vps_overview}` 与闭合 `VisualStateV1={authorization_revoked_or_permanently_deleted,first_empty,initial_loading,local_failure,query_no_results,submitting_or_background}` 的完整笛卡尔积生成 `5×6=30` 个语义 fixture；ID严格为`<PageGroupV1>__<VisualStateV1>`。`records_center_and_subject_timeline`每态同时含`records_center`与`subject_timeline`两个独立route subfixture，缺任一即失败且不增加顶层计数。每项同时覆盖桌面/390px、Axe、键盘/focus/44px，并与 30 秒理解测试使用同一 commit 报告。
- [ ] staging 0.59.0 以来的 VPS/监控/IP/订阅数据路径回归通过，新记录可固化来源快照且源归档/删除后仍按授权可读。
- [ ] 永久删除专项证明在线命中为 0、旧 API/导入/processor/export 不复活、恢复 fail closed、telemetry corpus 泄露命中为 0。
- [ ] Retention 八个 registry roots 与 1–11 所有 schema/surface 双向全等；最终 lifecycle/participant set 恰为规范 21/24 行且 participant live set 与 Go/Web executable binding set 全等。初始每个 child 的 nonempty add-only registry delta 都经 source claim、feature merge acceptance 和 metadata PR 绑定其 merge commit，任务 11 后的父门先验证 ordinal chain 再验证 child-ID-sorted union；24h/30d、notification 180d 上限、forensic recoverability window、managed filesystem/core-dump exclusion chain 和 forbidden corpus 在 PostgreSQL/S3/文件面残留均为 0。
- [ ] Recovery planned/disaster 全域矩阵证明完整 intent wrapper、三 identity artifact、15-kind receipt chain、显式 signing key、24/20/8 MiB chunking、import revoke 前置与 final-chain-bound completion 在 primary/PostgreSQL witness/S3 三后端语义一致。
- [ ] `git diff --check`、工作树审查、required CI、主线CI、Release Please、五binary发布镜像smoke、all-flags-off精确digest部署、preflight/cutover/rollback/soak均完整记录；任何staging修复重新走受保护PR/release链。

## 8. 121 条验收证据矩阵

任务 11 创建 `scripts/verify-vps-records-acceptance.sh` 和版本化 registry。以下每一行都必须能独立执行：

```bash
scripts/verify-vps-records-acceptance.sh \
  --criterion P-AC-NNN \
  --output-root artifacts/acceptance
```

runner 固定使用 `github.com/yuin/goldmark v1.8.4` 与 `extension.GFM`，并对 CRLF→LF 后、拒绝bare CR的完整UTF-8源文件建立AST；不得依赖不存在的“section node”或拼接parser-specific descendant spans。输入只能有一个inline plain-text恰为`Acceptance Criteria`的top-level H2；验收区间是该H2标题行末字节之后到下一top-level H2起始字节之前。该区间除空白外必须恰有一个top-level unordered list，criterion只能来自该list的direct `ListItem`。每个item必须在column 0以exact `- [ ] `开头，checkbox后的首个非空inline必须是唯一匹配`^P-AC-[0-9]{3}$`的code span；continuation中的criterion-looking文本不声明新ID，nested list/fenced block/第二个top-level block均拒绝。

每条requirement使用一个连续源码范围：从该direct `ListItem`的`-`起始字节到下一direct item的`-`前一字节，末项到top-level list结束字节；因此item内部paragraph、softbreak、空白行和两空格continuation都原位进入hash。规范化只执行：全文件CRLF→LF；删除item首行exact `- [ ] `；后续每个非空源码行必须至少有exact两个ASCII空格并只移除这两个，空白行保留为空行；逐行移除尾随ASCII space/tab；保留其余Markdown bytes与内部空白行；末尾折叠为恰一个LF。registry拒绝unknown/duplicate/non-contiguous ID、范围参数和AST/source-range不一致。golden固定`P-AC-035`为一个direct item、两个paragraph且中间恰一空白行，literal SHA-256为`e7ed74c8e00fecb908b0cfb847de6bf27628dd67718b53b2e368a67cb70d900b`；`P-AC-053`为一个direct item加两个continuation源码行，literal SHA-256为`f1fe4afb0b6d5fa837c5a63aae6cd14fe1e9c42ffe4e7369883e6ebcefdbe907`。两者的完整normalized bytes与hash均进入testdata，删除、拆分、折叠空白或遗漏任一字节都失败。

receipt 固定包含 `criterion_id`、`normalized_requirement_sha256`、实际命令与 exit code、typed evidence receipt、artifact digest、验证时间，以及按 `(participant_kind_order,parent=0|child=1,child_id)` 升序、唯一且至少一项的 `participants[]`。`ParticipantKindV1=parent|child`、`EvidenceKindV1=planning_artifact|production_call` 是闭合判别联合；每项先固定包含 `participant_kind,evidence_kind,evidence_digest`，再按 arm 校验：`parent + planning_artifact` 必须有 `artifact_path,source_section` 且禁止 `participant_id,child_id,merge_commit,production_symbol,caller`；`child + planning_artifact` 必须有 `child_id,artifact_path,source_section` 且禁止 `participant_id` 与生产调用字段；`child + production_call` 必须有 `child_id,merge_commit,production_symbol,caller` 且禁止 `participant_id` 与 planning artifact 字段；`parent + production_call` 非法。selector 以 `planning.` 开头的 criterion 可由 planning artifact 证明；其他 behavioral criterion 必须至少有一个 non-mock integration/staging receipt 与一个 `production_call` participant。Owner 列展开为闭合 owner set：`Parent` 要求至少一个 `participant_kind=parent`，每个数字/范围要求对应每个 `child_id` 至少一项；不得只覆盖 child 而漏 Parent。即使 criterion 只有一个 owner 也使用同一数组，禁止用一个 `owner_child` 覆盖多 owner 或用 task 11 冒充其他 child。`scripts/verify-vps-records-acceptance.sh --verify-registry` 必须证明 registry 与 PRD 恰好都是 121 个连续唯一声明、规范化文本 hash 全相等、participant union/XOR/Owner coverage 合法且没有 skipped/mock-only 证据；最终父门对 121 个 JSON 做排序 Merkle digest并绑定任务11的release image/config/staging receipt。表内 selector 是 registry 的稳定 key，不能由实现阶段改成更宽的编号范围。

| 验收标准 | Owner | 可执行 evidence selector | 最终 receipt |
|---|---|---|---|
| `P-AC-001` | Parent + 11 | `planning.staging-walkthrough`：staging 交互、截图/trace、严重度与触发场景逐项存在 | `artifacts/acceptance/P-AC-001.json` |
| `P-AC-002` | Parent + 11 | `planning.implementation-audit`：前后端/模型/API/权限/响应式/测试事实与假设标记校验 | `artifacts/acceptance/P-AC-002.json` |
| `P-AC-003` | Parent + 11 | `planning.audit-dimensions`：审查维度清单与 staging/code evidence 双向覆盖 | `artifacts/acceptance/P-AC-003.json` |
| `P-AC-004` | Parent + 11 | `planning.reported-problems`：四类用户问题到去重 findings/severity 的映射 | `artifacts/acceptance/P-AC-004.json` |
| `P-AC-005` | Parent + 11 | `planning.external-patterns`：产品样本、可迁移模式、适用条件与拒绝理由 | `artifacts/acceptance/P-AC-005.json` |
| `P-AC-006` | Parent | `planning.approved-directions`：A/B/C 方向、取舍和逐节用户批准记录 | `artifacts/acceptance/P-AC-006.json` |
| `P-AC-007` | Parent + 11 | `planning.design-coverage`：PRD 要求到 design §1–28/child owner 的全量映射 | `artifacts/acceptance/P-AC-007.json` |
| `P-AC-008` | 2, 7, 9 | `activity.manual-system-contract`：修订/系统事件字段、生命周期、权限和排序集成测试 | `artifacts/acceptance/P-AC-008.json` |
| `P-AC-009` | 7, 11 | `overview.exception-dom`：stable/exception/recovered Playwright DOM 与 screenshot，空占位计数为 0 | `artifacts/acceptance/P-AC-009.json` |
| `P-AC-010` | 7, 11 | `overview.visual-hierarchy`：桌面/390px identity/actions/status/facts/timeline/links 几何与非颜色信号 | `artifacts/acceptance/P-AC-010.json` |
| `P-AC-011` | 7, 11 | `overview.task-order`：桌面、390px、纯键盘路径与内容顺序断言 | `artifacts/acceptance/P-AC-011.json` |
| `P-AC-012` | 6, 7, 8 | `navigation.deep-links`：四类 route、return state、URL filters 与 canonical identity E2E | `artifacts/acceptance/P-AC-012.json` |
| `P-AC-013` | 6, 7 | `records.global-subject-views`：sidebar 与 VPS scoped query 使用同一 DTO/query contract | `artifacts/acceptance/P-AC-013.json` |
| `P-AC-014` | Parent + 1–11 | `planning.complete-record-product`：能力矩阵 owner/evidence 无缺项，包含Child 1授权/删除/恢复治理 | `artifacts/acceptance/P-AC-014.json` |
| `P-AC-015` | 2, 6, 7 | `relations.bidirectional-navigation`：VPS/monitor/target 与 records 双向授权深链 | `artifacts/acceptance/P-AC-015.json` |
| `P-AC-016` | 2, 4, 10 | `relations.kind-conformance`：role/primary/snapshot/route/tombstone/search/export/delete suite | `artifacts/acceptance/P-AC-016.json` |
| `P-AC-017` | 4, 5 | `evidence.snapshot-survival`：capture/preview/persist 后 source archive/delete 的读取与状态 E2E | `artifacts/acceptance/P-AC-017.json` |
| `P-AC-018` | 4, 8 | `evidence.comparison-extensibility`：time/subject compare 与 route/performance test registry | `artifacts/acceptance/P-AC-018.json` |
| `P-AC-019` | 4 | `evidence.envelope-security`：allowlist/version/size/integrity/redaction/retention golden corpus | `artifacts/acceptance/P-AC-019.json` |
| `P-AC-020` | 4, 6, 8, 9, 10 | `evidence.classification-propagation`：save/list/compare/notify/export masking parity | `artifacts/acceptance/P-AC-020.json` |
| `P-AC-021` | 4 | `evidence.url-topology-bounds`：URL canonicalization、topology forbidden fields 与 source tombstone | `artifacts/acceptance/P-AC-021.json` |
| `P-AC-022` | 4 | `evidence.monitoring-window`：绝对窗口/覆盖/聚合/缺口/阈值/partial schema golden | `artifacts/acceptance/P-AC-022.json` |
| `P-AC-023` | 4 | `evidence.timeseries-limits`：720/四档/2000/50000/5MiB server boundary tests | `artifacts/acceptance/P-AC-023.json` |
| `P-AC-024` | 4 | `evidence.bucket-dedupe-history`：峰值语义、无 raw 5s、logical ACL 与历史引用保持 | `artifacts/acceptance/P-AC-024.json` |
| `P-AC-025` | 4, 5 | `evidence.quota-degradation`：10GiB/80% 与 text-only/remove/reuse 已有 snapshot 路径 | `artifacts/acceptance/P-AC-025.json` |
| `P-AC-026` | 4 | `evidence.server-generated-only`：任意 JSON/command-output spoof 请求全部拒绝 | `artifacts/acceptance/P-AC-026.json` |
| `P-AC-027` | 4 | `evidence.capture-consistency`：watermark/transaction 与 concurrent backfill/retention failure | `artifacts/acceptance/P-AC-027.json` |
| `P-AC-028` | 4, 8, 10 | `evidence.kind-conformance`：canonical/unknown/import-export/copy-delete/auth/compare corpus | `artifacts/acceptance/P-AC-028.json` |
| `P-AC-029` | 4, 10 | `evidence.schema-reader-retention`：referenced old schema read/export 与 renderer removal gate | `artifacts/acceptance/P-AC-029.json` |
| `P-AC-030` | 4, 5, 11 | `evidence.selector-confirmation`：参数、coverage/gap/redaction/size preview 与 final confirm E2E | `artifacts/acceptance/P-AC-030.json` |
| `P-AC-031` | 2, 4 | `evidence.source-lifecycle`：archive/purge/TTL 后 no-cascade snapshot semantics | `artifacts/acceptance/P-AC-031.json` |
| `P-AC-032` | 1, 2 | `deletion.archive-reservation-fence`：archive restore、preview/confirm、≤1s drain 与 post-202 zero-read | `artifacts/acceptance/P-AC-032.json` |
| `P-AC-033` | 1, 2 | `deletion.authorization-status`：revoke race、initiator/admin polling 与 cross-project 404 corpus | `artifacts/acceptance/P-AC-033.json` |
| `P-AC-034` | 2, 3, 6, 10, 11 | `deletion.online-purge-receipts`：DB/search/cache/export/material/blob participants 与 retry | `artifacts/acceptance/P-AC-034.json` |
| `P-AC-035` | 1–11 | `retention.field-level-residue`：11个 required-nonempty add-only delta 的 deterministic source claim→双亲 feature merge→protected-main rescan/sign→exact-one-file metadata PR→next-child base，Task 11后先验证`MergeOrdinal=1..11` chain及对应ChildID序列`1,2,3,4,9,5,6,7,8,10,11`，再按ChildID计算final union；owner matrix/八registry-root exact join、21 lifecycle+24 participant及live executable-binding全等、`RequiredForbiddenAfterTransitionV1`逐row生成集合全等与每enum omission-negative、逐PostgreSQL column/canonical leaf/S3 property/managed filesystem/client surface、bootstrap/current-policy exclusion-proof chain、逐family 24h/30d/notification-180d/forensic-window matrix与全类别forbidden-residue corpus scan | `artifacts/acceptance/P-AC-035.json` |
| `P-AC-036` | 1–11 | `deletion.concurrent-pipelines`：每条写/worker/notify/projector 与 reservation epoch race | `artifacts/acceptance/P-AC-036.json` |
| `P-AC-037` | 1, 3, 4, 10 | `deletion.streaming-content-leases`：headers/first/last chunk 与 disconnect/renew failure matrix | `artifacts/acceptance/P-AC-037.json` |
| `P-AC-038` | 1, 3, 11 | `backup.epoch-scope-cutpoints`：full-db lease/reservation exclusion 与 marker/pin publish | `artifacts/acceptance/P-AC-038.json` |
| `P-AC-039` | 1, 3, 11 | `backup.partial-inventory-janitor`：dump/blob/multipart/manifest/signature cutpoints and zero residue | `artifacts/acceptance/P-AC-039.json` |
| `P-AC-040` | 1, 2 | `deletion.append-ack-idempotency`：primary/witness/fence/purge crash matrix、server-issued token commitment、raw-token zero-persistence 与跨 scope/低熵拒绝 | `artifacts/acceptance/P-AC-040.json` |
| `P-AC-041` | 1 | `deletion.not-committed-release`：unknown→fenced→proof→outcome→release 全 cutpoints | `artifacts/acceptance/P-AC-041.json` |
| `P-AC-042` | 1, 11 | `recovery.four-entry-watermark`：activation/outcome/delete/domain-rotation receipts 逐序重放 | `artifacts/acceptance/P-AC-042.json` |
| `P-AC-043` | 1, 11 | `admission.lease-head-epoch-version`：failover/stale projection/LB/queue old-member denial | `artifacts/acceptance/P-AC-043.json` |
| `P-AC-044` | 2, 11 | `deletion.preview-disclosure`：references/exports/notices/offline/backup window copy snapshot | `artifacts/acceptance/P-AC-044.json` |
| `P-AC-045` | 1, 2 | `deletion.preview-token-staleness`：version/dependency/inventory/head scoped conflict matrix | `artifacts/acceptance/P-AC-045.json` |
| `P-AC-046` | 1, 11 | `recovery.delete-after-backup`：signed manifest、continuous witness tail 与 tamper/gap denial | `artifacts/acceptance/P-AC-046.json` |
| `P-AC-047` | 1, 11 | `recovery.workspace-cutpoints`：steps 2–6、expiry/forensic/purge receipt 与 zero residue | `artifacts/acceptance/P-AC-047.json` |
| `P-AC-048` | 1, 11 | `recovery.workspace-backup-exclusion`：exclusion proof 或 derived recovery point/expiry gate | `artifacts/acceptance/P-AC-048.json` |
| `P-AC-049` | 1, 11 | `recovery.source-manifests`：full/PITR/WAL/snapshot/blob-S3 binding and corruption matrix | `artifacts/acceptance/P-AC-049.json` |
| `P-AC-050` | 1 | `trust.full-witness-lifecycle`：postgres_sync/s3_worm genesis rebuild、rotation/compromise/tail denial | `artifacts/acceptance/P-AC-050.json` |
| `P-AC-051` | 1 | `activation.durable-dag-resume`：self-reference corpus、artifact/control loss、mutation resume/completion | `artifacts/acceptance/P-AC-051.json` |
| `P-AC-052` | 1 | `governance.authorization-action-matrix`：每动作 TTY/detached/scope/possession/inventory 真值表 | `artifacts/acceptance/P-AC-052.json` |
| `P-AC-053` | 1, 11 | `governance.activation-and-domain-rotation`：legacy inventory、`ValidateDomainRotationIntentV1` 在 plan/apply/import/resume/primary/PostgreSQL/S3 的完整 crypto parity corpus、signed admission/drain snapshot artifact-loss rebuild、identity primary-2+witness-3 readback及 witness primary-receipt copy 缺失/篡改、闭合 delete/trust route-reason enums、15-kind `DomainRotationReceiptV1` Go/primary/PostgreSQL-witness/S3 parity、unreachable draft/sign/seal/resume、显式 signing key、24/20/8 MiB `ObjectStart/ObjectChunk/ObjectEnd`、import-applied→revoked→cutover、current-side receipt-FD ingestion、generation-1 nonce/cleanup descriptor+deadline必填、sign/seal/2+3 readback、previous-policy逐字继承与reinjection stat前拒绝、policy→preparation→intent→plan descriptor parity、typed recovery-request artifact-loss resume、abandon-vs-intent CAS及2+3 completion、import/cutover revoke ack-before-key/bundle destruction、六scope local/KMS parser、two-kind nonce reservation与三类signer isolation、unsigned AEAD/wrapped-DEK/nonce destruction evidence、cleanup verifier exact-two purposes、zero key counts、purge/workspace-zero no-self-storage FD、Final canonical policy-head+verifier descriptor、completion schema显式policy-head generation/digest+publication receipts | `artifacts/acceptance/P-AC-053.json` |
| `P-AC-054` | 1, 2, 4, 10, 11 | `recovery.source-delete-no-revival`：每个 source kind replay/tombstone/snapshot authorization | `artifacts/acceptance/P-AC-054.json` |
| `P-AC-055` | 1, 11 | `backup.expiry-proof-and-copy`：media destruction receipt 与 UI/help/audit disclosure snapshot | `artifacts/acceptance/P-AC-055.json` |
| `P-AC-056` | 2, 3, 10, 11 | `storage.pin-watermark-races`：backup/restore/import/write/delete/GC concurrent manifest integrity | `artifacts/acceptance/P-AC-056.json` |
| `P-AC-057` | 1, 11 | `telemetry.secret-content-corpus`：all sinks/backups、30d identifier expiry、unknown sink fail-close | `artifacts/acceptance/P-AC-057.json` |
| `P-AC-058` | 2 | `records.explicit-revision-only`：draft autosave/no-op save/restore-old revision and activity counts | `artifacts/acceptance/P-AC-058.json` |
| `P-AC-059` | 2 | `records.complete-revision-rehydration`：全部字段/material/relations/template provenance restore | `artifacts/acceptance/P-AC-059.json` |
| `P-AC-060` | 2, 5 | `records.template-versioning`：create/insert/type-change/no overwrite and historical stability | `artifacts/acceptance/P-AC-060.json` |
| `P-AC-061` | 2, 5 | `draft.cross-device-retention`：remote resume、save state、90d/7d/config/save/discard boundaries | `artifacts/acceptance/P-AC-061.json` |
| `P-AC-062` | 2, 3, 5 | `draft.restore-points-and-attachments`：count/time cap、base expiry merge、shared blob retention | `artifacts/acceptance/P-AC-062.json` |
| `P-AC-063` | 2, 5 | `records.cas-conflict-merge`：local draft preservation、field/Markdown diff 与 retry E2E | `artifacts/acceptance/P-AC-063.json` |
| `P-AC-064` | 2, 4 | `records.authoritative-rebuild`：root/current/history/material rehydrate 与 projection equality | `artifacts/acceptance/P-AC-064.json` |
| `P-AC-065` | 2, 3, 9 | `records.separate-collaboration-and-logical-acl`：draft/action/comment/follow 无 revision noise 与 safe GC | `artifacts/acceptance/P-AC-065.json` |
| `P-AC-066` | 1–4 | `records.atomic-save-cutpoints`：base/idempotency/capture/reread/attachment/blob/transaction all-or-none | `artifacts/acceptance/P-AC-066.json` |
| `P-AC-067` | 3–5, 9 | `workspace.failure-ux`：draft/conflict/evidence/attachment/blob/db/notification 状态与 input preservation | `artifacts/acceptance/P-AC-067.json` |
| `P-AC-068` | 2, 5, 11 | `draft.browser-buffer-revocation`：user isolation、24h、sync/logout/switch/offline reconnect cleanup | `artifacts/acceptance/P-AC-068.json` |
| `P-AC-069` | 1, 2, 5 | `security.multi-client-revoke`：SSE/poll/focus/pageshow/reconnect two-tab/device zero stale render | `artifacts/acceptance/P-AC-069.json` |
| `P-AC-070` | 1–11 | `architecture.boundaries-and-adapters`：依赖图、interface fakes、no direct source/blob access static+integration | `artifacts/acceptance/P-AC-070.json` |
| `P-AC-071` | 7, 11 | `overview.partial-read-model`：per-section generated/error states 与 route-local loading/submission | `artifacts/acceptance/P-AC-071.json` |
| `P-AC-072` | 2, 7, 10 | `migration.legacy-experience-reconciliation`：repeat/count/ID/time/text/category/unknown provenance | `artifacts/acceptance/P-AC-072.json` |
| `P-AC-073` | 1, 11 | `rollout.flags-profile-scope-acl`：exact parser/open spies、scoped migrator、ACL revision、22/52/4/2 exact catalog、old instance gates | `artifacts/acceptance/P-AC-073.json` |
| `P-AC-074` | 11 | `performance.confirmed-scale`：overview/search/timeline/draft/revision/comparison/evidence p95 与 capacity table | `artifacts/acceptance/P-AC-074.json` |
| `P-AC-075` | Parent + 11 | `planning.child-graph-completeness`：11 child status/dependencies/acceptance/merge receipts exact set | `artifacts/acceptance/P-AC-075.json` |
| `P-AC-076` | 2, 5 | `records.type-state-contract`：per-type states/transitions/groups and no empty state DOM | `artifacts/acceptance/P-AC-076.json` |
| `P-AC-077` | 2, 5 | `records.transition-policy`：recommended/jump/back/reopen/type-change/terminal reason server+UI parity | `artifacts/acceptance/P-AC-077.json` |
| `P-AC-078` | 3 | `attachments.admission-and-download`：MIME/signature/name/hash/immutable/ref/preview/force-download/URL denial | `artifacts/acceptance/P-AC-078.json` |
| `P-AC-079` | 3, 5 | `attachments.quarantine-sandbox`：mismatch/complexity/zip bomb/encrypted/active content and unavailable states | `artifacts/acceptance/P-AC-079.json` |
| `P-AC-080` | 3, 4, 5, 10 | `processor.workspace-crash-receipts`：scanner/unpack/renderer/Chromium kill and tmpfs/profile/cache zero residue | `artifacts/acceptance/P-AC-080.json` |
| `P-AC-081` | 3 | `attachments.quota-boundaries`：50MiB/500MiB/10GiB/80%/admin config and no-new-byte save | `artifacts/acceptance/P-AC-081.json` |
| `P-AC-082` | 3, 11 | `attachments.local-s3-parity`：identity/auth/hash/backup/restore/migration and container rebuild | `artifacts/acceptance/P-AC-082.json` |
| `P-AC-083` | 2, 3 | `attachments.draft-xor-transfer`：record XOR draft、atomic save quota and failed-save ownership | `artifacts/acceptance/P-AC-083.json` |
| `P-AC-084` | 3–5, 10 | `materials.attachment-evidence-separation`：detail/diff/export labels and trust semantics | `artifacts/acceptance/P-AC-084.json` |
| `P-AC-085` | 5 | `markdown.dialect-safety-render-parity`：storage/parser/GFM/footnote/highlight/no-HTML/ref/history/export | `artifacts/acceptance/P-AC-085.json` |
| `P-AC-086` | 5, 11 | `markdown.editor-state-responsive`：draft/formal/save/material drawer desktop/390px/keyboard | `artifacts/acceptance/P-AC-086.json` |
| `P-AC-087` | 4, 5 | `evidence.preview-regeneration`：source params→new server preview with requested/actual/observed metadata | `artifacts/acceptance/P-AC-087.json` |
| `P-AC-088` | 3–5 | `materials.structured-references`：no inline JSON/binary/data URL、history keep、missing/purged states | `artifacts/acceptance/P-AC-088.json` |
| `P-AC-089` | 6 | `search.server-query-contract`：full text/filters/stable cursor/URL/highlight without all-record fetch | `artifacts/acceptance/P-AC-089.json` |
| `P-AC-090` | 2, 6 | `search.relation-index-routing`：primary+related identity tokens、authorized reverse routes、tombstones | `artifacts/acceptance/P-AC-090.json` |
| `P-AC-091` | 6, 9, 11 | `records-center.dynamic-signals`：follow-up/blocked/due conditional DOM and scannable list fields | `artifacts/acceptance/P-AC-091.json` |
| `P-AC-092` | 2, 6 | `search.scope-separation`：default/archive/history/draft queries and global no-draft assertion | `artifacts/acceptance/P-AC-092.json` |
| `P-AC-093` | 7, 8 | `activity.compare-shared-facts`：same revision/activity/evidence IDs across vertical/horizontal routes | `artifacts/acceptance/P-AC-093.json` |
| `P-AC-094` | 7 | `activity.subject-projections`：activity/records filters、non-color source marks、evidence coverage disclosure | `artifacts/acceptance/P-AC-094.json` |
| `P-AC-095` | 2, 7, 9 | `activity.event-time-ordering`：revision1/update/legacy/system/evidence/comment/action mapping and tie-break | `artifacts/acceptance/P-AC-095.json` |
| `P-AC-096` | 4, 8 | `comparison.selection-alignment`：recommended/exact snapshots、time/coverage/freshness/schema/normalized metrics | `artifacts/acceptance/P-AC-096.json` |
| `P-AC-097` | 2, 4, 8 | `comparison.immutable-revision-selection`：multi/none evidence、refresh/source-change stable selection | `artifacts/acceptance/P-AC-097.json` |
| `P-AC-098` | 8, 11 | `comparison.visual-honesty`：criteria before metrics、gap-preserving charts、partial/not-computable matrix | `artifacts/acceptance/P-AC-098.json` |
| `P-AC-099` | 2, 4, 8 | `comparison.allowed-differences-save`：same-kind calculations only、no score、save new revision/snapshots | `artifacts/acceptance/P-AC-099.json` |
| `P-AC-100` | 1, 2, 6–10 | `authorization.surface-parity`：draft/project/restricted across API/list/search/stats/activity/compare/download/export | `artifacts/acceptance/P-AC-100.json` |
| `P-AC-101` | 1, 3–5, 8, 10 | `authorization.multi-source-intersection`：snapshot/ref/URL/compare/export bypass corpus and audited revisions | `artifacts/acceptance/P-AC-101.json` |
| `P-AC-102` | 1–11 | `authorization.floor-kind-replay`：project→restricted A→delete→old-backup replay across all read surfaces | `artifacts/acceptance/P-AC-102.json` |
| `P-AC-103` | 5, 10 | `portability.human-export`：Markdown directory/PDF current+history/material/source states and authorization | `artifacts/acceptance/P-AC-103.json` |
| `P-AC-104` | 1, 10 | `portability.export-plan-reauthorize`：revision/material hashes、generate/download checks、external-copy disclosure | `artifacts/acceptance/P-AC-104.json` |
| `P-AC-105` | 10 | `portability.machine-archive-integrity`：manifest/schema/hashes/signature and signer trust-state UI | `artifacts/acceptance/P-AC-105.json` |
| `P-AC-106` | 1, 10 | `portability.import-authorization`：package ACL never grants、target scope selection/source intersection | `artifacts/acceptance/P-AC-106.json` |
| `P-AC-107` | 3, 10 | `import.dry-run-security`：compat/hash/signature/path/link/bomb/source/auth/duplicate/size/material matrix | `artifacts/acceptance/P-AC-107.json` |
| `P-AC-108` | 1, 2, 10 | `import.origin-tombstone-reimport`：default denial、approved new ID、second-delete lineage sequence | `artifacts/acceptance/P-AC-108.json` |
| `P-AC-109` | 1, 10 | `import.plan-witness-staleness`：object/origin/head/approval binding and post-dry-run delete conflict | `artifacts/acceptance/P-AC-109.json` |
| `P-AC-110` | 1, 10 | `import.identity-guard-races`：apply-first/reservation-first/commit-unknown owner fencing outcomes | `artifacts/acceptance/P-AC-110.json` |
| `P-AC-111` | 3, 10 | `import.material-inventory-janitor`：raw/unpack/scan/partial/plan refs/TTL/exclusion/purge receipts | `artifacts/acceptance/P-AC-111.json` |
| `P-AC-112` | 1, 10 | `import.unknown-identity-project-fence`：first/mid/partial/parse-fail stream versus deletion reservation | `artifacts/acceptance/P-AC-112.json` |
| `P-AC-113` | 6, 9 | `collaboration.conditional-fields-filters`：owner/participants/follow-up/actions and no empty overdue/blocked DOM | `artifacts/acceptance/P-AC-113.json` |
| `P-AC-114` | 7, 9 | `collaboration.action-item-events`：independent changes/auth/export with no Markdown revision/implicit status | `artifacts/acceptance/P-AC-114.json` |
| `P-AC-115` | 5, 9, 10 | `collaboration.comment-history-export`：safe Markdown/edit/delete tombstone/flat reply context/export | `artifacts/acceptance/P-AC-115.json` |
| `P-AC-116` | 9 | `collaboration.follow-mention-notify-rules`：defaults/unfollow/mandatory/self/draft/aggregation truth table | `artifacts/acceptance/P-AC-116.json` |
| `P-AC-117` | 1, 9 | `notifications.reauthorize-redact-deeplink`：send/retry/history/open across inbox/Telegram/Feishu | `artifacts/acceptance/P-AC-117.json` |
| `P-AC-118` | 1, 9, 11 | `notifications.post-delete-residue`：delivery/outbox/inbox/provider/buffer field scan and minimal external audit | `artifacts/acceptance/P-AC-118.json` |
| `P-AC-119` | Parent + 11 | `visual.user-reviewed-artifact`：approved visual companion snapshots tied to final route fixtures | `artifacts/acceptance/P-AC-119.json` |
| `P-AC-120` | 5–8, 11 | `visual.artifact-v1-contract`：closed five `PageGroupV1` × closed six `VisualStateV1` = 30 exact `<PageGroupV1>__<VisualStateV1>` fixtures；combined group双route subfixture；desktop/390px semantics/DOM/geometry/overflow/focus/Axe | `artifacts/acceptance/P-AC-120.json` |
| `P-AC-121` | Parent + 1 + 11 | `planning.final-self-review`：父`prd.md`、`design.md`、`implement.md`与platform-foundation`prd.md`、`design.md`、`implement.md`的placeholder/contradiction/ambiguity/scope scans及exact six-file hash manifest | `artifacts/acceptance/P-AC-121.json` |

## 9. Plan self-review

- Requirement coverage：父设计的 28 个章节分别由子任务 1–11 承接；安全、视觉、性能和恢复没有留在无 owner 的尾部。
- Dependency consistency：records core 只依赖 platform；附件/证据/搜索/协作依赖 core；活动直接等待协作以完整投影评论/行动项，比较直接等待活动以取得主体证据入口；编辑器、可移植性和集成依赖与 design §26 一致。
- Migration consistency：应用迁移、独立 ledger/recovery/witness 迁移分账；编号冲突有明确整体顺延规则。
- Scope control：不新增通用项目管理、聊天、匿名公开、任意命令归档、跨证据总分或逐记录 KMS/Vault。
- Authorization gate：本计划与 11 个子任务完成仍不等于执行授权；`task.py start` 继续等待用户明确确认。

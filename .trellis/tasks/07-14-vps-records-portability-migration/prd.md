# 导入导出与 legacy 迁移

## Goal

交付可验证、可恢复且不绕过授权/永久删除边界的记录可移植层：面向人的 Markdown/PDF 导出、面向迁移的版本化机器归档、隔离 dry-run 导入、安全身份/引用重映射，以及 `experience_logs` 到 records 的幂等无复活转换。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §9.2、§10、§11.3–11.4、§12–§15、§17、§19.3、§21–§23、§25–§27。
- 功能直接依赖（子任务 2–9）：`07-14-vps-records-core`、`07-14-vps-records-attachments-storage`、`07-14-vps-records-evidence-platform`、`07-14-vps-records-markdown-workspace`、`07-14-vps-records-search-center`、`07-14-vps-records-activity-overview`、`07-14-vps-records-comparison-workbench`、`07-14-vps-records-collaboration` 必须合入受保护主线并通过 post-merge CI。search revision participant、activity record-domain adapter和`comparison.result/v1`都是本任务直接消费的功能合同，不得只把子任务 6–8 记作线性顺序前置，也不能从缺少 0055/0057 的旧主线绕行。
- 本任务拥有保留迁移`0058_create_record_portability.sql`与`0059_migrate_experience_logs_to_records.sql`；依赖已合入的0051–0057永久冻结，占号时只能整体顺延仍未发布的0058–0060并同步integration引用。不得把导出/导入表偷塞到其他migration，也不得让0059在未验证deletion ledger/origin tombstone前复制legacy内容。
- 本任务交付完整范围，不以 MVP 理由省略人类可读导出、机器归档、签名/校验、附件、不可变证据、协作历史、canonical activity、dry-run、身份映射、幂等 apply、部分失败收敛、legacy 重复转换、删除/恢复适配和可访问 Web 工作流。
- 人类可读导出支持 Markdown 目录包与 PDF；默认只导出当前正式修订，用户可显式选择完整修订/协作历史。两种格式必须使用同一安全服务端阅读模型、来源状态、证据摘要和附件清单，不能出现格式间语义漂移。
- 全部导出默认 `safe`。`sensitive_topology` 只包含 evidence schema 已准入且已存在于快照的敏感拓扑，要求 `record.export_sensitive_topology`、影响预览、二次确认和审计；永久禁止字段、命令 stdout/stderr、凭据和任意 raw JSON 在任何模式均不得进入。
- 机器归档使用版本化 canonical manifest、逐文件 SHA-256、确定性路径/编码、producer/schema version与可选实例 Ed25519 签名。checksum只证明包内完整性；签名只证明配置的 signer，不能让包内 actor/source/producer/ACL 成为目标实例事实。
- archive hard caps独立区分：entries≤200,000、streamed `manifest.json`≤128MiB、每个非manifest metadata entity≤1MiB、manifest parser working set≤16MiB；generator与importer执行同一上限。达到任一上限明确拒绝，不把200k file list塞进1MiB metadata规则，也不把manifest整体载入内存。
- 机器归档保存选定记录的稳定关系、全部正式修订、类型化不可变 evidence envelope/payload、revision引用、允许的attachment metadata/blob、主体身份快照、action/comment/tombstone历史，以及activity child返回的完整`ActivitySnapshot{projection_generation, committed-contiguous published_ingest_sequence, readiness_digest}`内与记录关联的versioned canonical activity envelope；schema/provenance和内容哈希必须完整。不能把通知收件箱、未读状态、外投重试、channel secret或已投递message ID当成可恢复业务内容。
- 导出 preview 必须列出 revision/material IDs 与哈希、文件、字节、权限/敏感范围、外部副本不可召回提示和计划到期；生成前和每次下载都重新授权并检查 reservation/deletion fence。完成 artifact默认24小时清理，授权下载最长15分钟。
- 归档导入始终是不可信输入：接收第一字节前登记 job/workspace，隔离处理规范化/重复路径、绝对/父目录路径、Unicode路径、symlink/hardlink、条目/字节上限、压缩炸弹、manifest/hash/signature、Markdown、附件扫描和 evidence schema。签名通过不能跳过任何内容安全检查。
- dry-run 必须展示 archive/schema兼容性、完整性与签名历史/当前 trust、重复/冲突、缺失主体、actor/owner/participant/action assignee映射、权限变化、未解析引用、附件/evidence去重、预计逻辑/物理容量、删除 tombstone、阻塞错误和实际会写入的对象集合；未经确认不写业务数据。
- 包内 entity ID 一律映射为新目标 ID并保留 origin lineage；同 archive digest/target/origin 的响应丢失与重试返回同一结果。外部 actor/user/subject 只能按 source instance + stable ID 由管理员显式映射，不能按显示名、用户名、域名或内容相似度自动绑定。
- 外部 ACL、role、group、capability 永不在目标生效；导入管理员显式选择目标 visibility/group，最终权限继续与显式映射 live source 的当前授权取交集。原授权、作者和 producer 只保留为有来源的历史 provenance。
- 导入evidence永远是历史不可变快照，不写监控、IP质量、订阅/成本、资产历史或命令源表。本实例曾支持/生成、archive声明为required、含必须重映射typed refs或在本次dry-run成功解码的schema缺decoder/不兼容时阻止整份apply。integrity-valid但从未被本实例支持的外部较新/不可解释evidence只可留在受管quarantine，并在dry-run/job中展示kind、schema、capture time、size、hash和“当前版本无法解释”的安全envelope metadata；没有受支持decoder就不能验证classification/永久禁止字段，因此不得写evidence snapshot、apply record、machine re-export、Summarize/Compare或render任意JSON。decoder以后可用时必须重新生成plan并全量验证。
- `comparison.result/v1` 是 machine archive 必须支持的required evidence schema；导出必须调用其versioned renderer/Summarize/Export合同，导入必须校验并重映射typed refs。导出→导入→再导出的语义除明确的新target IDs/origin lineage外必须等价，未知版本、缺失ref或不可重建字段阻止整份apply，不能静默丢失result、warning、condition或provenance。
- canonical activity导出通过activity dependency的授权provider固定水位读取；导入将archive事件的record/revision/evidence/subject refs映射到新ID，以versioned typed source tuple的length-prefixed canonical bytes+hash形成稳定import source并保留event time、recorded time、backfilled、correction和allowlisted provenance，不能拼接不可信字段构造identity。activity adapter必须可幂等投影/重建这些历史事件；另追加唯一local `record_imported` event。foreign activity不能成为目标实例live source fact、触发旧通知或在重试中重复。
- activity archive必须完整而非“当前已投影部分”：preview绑定每个required source的authoritative head、projected checkpoint/status、record-scoped readiness digest和activity child返回的完整`ActivitySnapshot{projection_generation,published_ingest_sequence,readiness_digest}`；任一outbox/source head领先、adapter failed/unknown、generation重建或无法证明caught-up时失败关闭。worker不得自行构造/省略snapshot字段，生成前与publish前重验同一vector/snapshot，drift使preview/job stale或`portability_safety_unavailable`，不能静默导出partial history。
- 导入不重放旧notification/outbox、不自动启用外部channel、不恢复foreign follower/unread状态；目标实例只按映射后的owner/participants和当前规则，由`CollaborationRevisionParticipant`在同一revision transaction写新的自动关注来源。follower写入失败必须使record/revision/search/activity/idempotency整体rollback，不能commit后补建。
- apply 对一个 plan 是全有或全无：预写的 immutable payload/blob在数据库失败时保持登记为待清理孤立对象，数据库事务原子写全部新ID/lineage/records/revisions/material/collaboration/current projections、search participant state与canonical domain activity；不能留下对用户可见的部分记录、缺失评论或断裂引用。import revision commit必须调用search participant，并让activity dependency接收唯一、可幂等投影的`record_imported` canonical event；legacy revision同样产生保留原event time且标记backfilled的canonical activity，不能绕过participant后离线补洞。
- import plan 默认1小时有效并绑定 normalized object/origin集合、archive/permission/capacity/classification digest、observed witness sequence/hash和不可变 `normal_import|reimport_deleted` 审批类型。apply前 fresh auth/fence/ledger/tombstone检查；相关变化返回 `409 import_plan_stale`，普通计划不能在重试中升级为重新导入已删除内容。
- normal import重复命中已完成 lineage时返回原 target IDs而不复制；同 origin内容digest变化返回冲突。命中永久删除 tombstone默认阻止；只有新的显式 `reimport_deleted` 审批可分配新ID，且后续再次删除可产生新的 ledger sequence。
- import apply与永久删除 reservation对 canonical object/origin使用同一 identity mutation guard和全局锁顺序。未完成 identity classification 的包按 project/deployment scope阻塞删除；classification complete后才收窄为精确 object/origin refs。
- 导出 artifact、导入原包/parts/解包树/扫描副本/plan/processor workspace都是候风控制面的受管副本，必须反向登记 record/object/origin、TTL、备份排除或 recovery inventory和purge receipt。plan过期不等于字节已清除。
- 永久删除由recorddeletion只编排owner-package adapters：portability adapter只清0058 import/export/lineage/activity-source与artifact/workspace，records/evidence/attachments/collaboration/search/activity各自清本领域权威/投影，`LegacyRecoveryAdapter`清`experience_logs`与legacy mapping；不得由portability直接跨包删derived projection。在线完成前必须撤销/清除含目标对象的服务端导出与导入副本，并从`record_import_entity_mappings`清content hash、从`record_origins`清archive digest及其他content-derived provenance，只保留阻止复活所需的canonical object/origin/generation/tombstone/ledger最小字段。已下载文件和用户保留的原归档是无法召回的外部副本；备份窗口内由official restore先重放ledger，不能声称即时物理/密码学擦除。
- `experience_logs` 迁移不双写、不猜作者/状态/类型。summary/details/category/severity/occurred_at/created_at和VPS身份原样可对账；可靠类别映射到record type，无法可靠映射使用 note并保留原类别；缺作者标为system migration provenance。
- legacy转换以 `(legacy_source_type,legacy_source_id)` 唯一且可重复执行；每次分配新 record ID 前同时检查 mapping tombstone、reservation、fresh independent ledger/origin tombstone。永久删除后旧 `/experience-logs` API/UI/SQL、migration rerun和恢复到迁移前备份均不得复活正文。
- 0059只安装受 origin/tombstone 约束的 migration contract/candidate状态；实际内容转换由同版本 migrator在 fresh ledger/witness gate 后事务执行。初始、增量和最终cutover前重复运行同一算法；final write switch与旧UI移除由 integration child负责。
- 正常导出/导入页面不渲染“0 个异常”“无冲突”异常卡、禁用修复动作或预留警告高度；warning/error块只在事实存在时插入，恢复后从DOM移除。

## Acceptance Criteria

- [ ] Markdown bundle/PDF在当前/完整历史和safe/sensitive四种有效组合下使用同一revision、evidence、attachment、action/comment语义；未授权或永久禁止字段命中为0。
- [ ] export preview绑定record/revision/material IDs、hash、policy、mode和文件清单；preview后内容/权限/fence变化稳定409，生成/下载不输出旧字节。
- [ ] artifact只在全部文件hash、canonical manifest和可选signature成功后原子发布；任一render/write/sign/publish强杀后无可下载半包，janitor最终partial残留为0且有receipt。
- [ ] machine archive manifest/file顺序、canonical bytes和SHA-256在同输入下完全确定；单bit篡改、缺文件、重复路径、manifest交换、signature错误均被识别。
- [ ] archive边界测试覆盖1MiB non-manifest metadata、128MiB streamed manifest、200,000 entries和16MiB parser working-set；各上限内最小/最大fixture通过，任一+1稳定拒绝且无unbounded allocation/partial artifact。
- [ ] signature_verified/unverified与当前trusted/revoked/unknown分开显示；任一trust状态都不会让foreign actor/source/ACL变成本地事实或跳过扫描。
- [ ] import hostile corpus覆盖绝对/父目录/Unicode重复路径、symlink/hardlink、条目/体积上限、截断zip、压缩炸弹、Markdown XSS、MIME欺骗、恶意附件和unknown evidence schema。全部unknown schema在无supported decoder时阻止apply；integrity-valid且从未支持的external schema只在quarantine dry-run显示安全envelope metadata，evidence snapshot/record/export/generic render/source fact写入数为0。
- [ ] dry-run逐项报告兼容、完整性、trust、identity/reference mapping、权限、重复、容量和tombstone；normal-state UI不存在异常占位，真实warning/error出现并解决后从DOM移除。
- [ ] actor/owner/participant/assignee/comment author与subject映射只接受管理员选择的source instance + stable ID对应关系；按显示名误绑定数为0，未解析身份以provenance/tombstone安全显示。
- [ ] 包内ACL/capability授予目标权限数为0；target visibility由当前管理员确认，mapped source收窄后列表/详情/material/export继续统一404。
- [ ] imported evidence不写任何source fact表，附件全部重新准入；external unsupported evidence只在quarantine dry-run显示allowlisted envelope metadata，apply与machine re-export均阻止，取消/过期后opaque bytes经receipt清零；任意JSON渲染数为0，foreign follower/inbox/outbox/external delivery replay数为0。
- [ ] 含`comparison.result/v1`的archive完成export→dry-run→apply→re-export后，renderer/summary/export material、conditions、system differences、warnings与typed provenance语义等价；除manifest声明的新target IDs/origin lineage外字段丢失数为0，unknown version或断裂ref的可见半份导入数为0。
- [ ] archive import和legacy migration的revision commit都经过search/collaboration participants；目标current search document与canonical revision一致，mapped owner/participant自动follower与revision同事务提交。注入follower失败后record/revision/search/activity/idempotency可见数均为0；activity adapter分别收到唯一`record_imported`与保留event time的backfilled legacy event，重复apply/response-loss retry后的漏投、重复投影与业务notification数均为0。
- [ ] 选定record的canonical activity在固定水位export→apply→rebuild→re-export后，event/source identity、event/recorded time、backfilled、correction、subject/provenance语义除声明的target ID映射外等价；每个archive event投影恰一次，另有且仅有一个local `record_imported`，foreign event触发notification数为0。
- [ ] activity export在outbox/source head领先projector、adapter failure/unknown、checkpoint/generation/readiness-digest drift和publish前新event场景均不发布artifact；caught-up vector稳定时manifest保存activity child返回的完整`ActivitySnapshot{projection_generation,published_ingest_sequence,readiness_digest}`，重复分页漏项/重复为0且不存在partial-mode标记。
- [ ] apply transaction任一点失败后target record/revision/ref/action/comment可见数全部为0；孤立bytes被登记并清理。commit响应丢失后同Idempotency-Key返回相同target IDs且不重复。
- [ ] 相同lineage+digest normal import为幂等existing结果；相同lineage不同digest稳定冲突；删除后normal import阻止，新的reimport_deleted审批分配新ID且origin查询仍命中全部历史tombstone。
- [ ] dry-run后并发import/delete覆盖apply先提交与reservation先提交；双提交、漏报副本、旧epoch迟到apply和tombstone后复活数均为0。
- [ ] identity unknown半包在删除preview/execute中按scope成为blocker；cancel/drain/purge receipt前`online_purged`数为0，complete digest后只阻塞相关object/origin。
- [ ] server export 24h、download token 15m、import plan 1h、job/receipt refs 30d合同可测试；过期只撤销能力，字节必须经janitor receipt后才算清除。
- [ ] permanent deletion清除服务端export/import副本并披露downloaded/original archive外部副本；`record_import_entity_mappings.content_hash`、`record_origins.archive_digest`及其他content-derived lineage残留数为0，最小origin tombstone仍阻止normal re-import；backup窗口内事实与official restore replay文案不作绝对擦除承诺。
- [ ] official restore对0058表和legacy origin执行连续ledger replay；恢复到export/import/migration任一cutpoint后，被删record、legacy正文、artifact、plan和迟到worker复活数为0，ledger缺口/未知adapter时启动数为0。
- [ ] 0058/0059 fresh、0057 upgrade、repeat apply和恢复前schema路径全部通过；0059在ledger/witness不可证明时不复制任何legacy正文。
- [ ] legacy category mapping、severity、summary、details、occurred/created time和VPS身份逐字段对账。固定watermark累计恒等式为`source_total = source_eligible_total + ineligible_total`、`source_eligible_total = mapped_total + tombstoned_skipped_total`且`difference_count=0`；单次恒等式为`scanned_count = created_count + already_mapped_count + tombstoned_skipped_count + mismatch_count + error_count`，第二次apply的`created_count=0`。未知作者/状态不被伪造。
- [ ] “migration→permanent delete→old GET/timeline/SQL→migration rerun”和“pre-migration backup→restore replay→migration”场景的summary/details命中与新record复活均为0。
- [ ] focused Go/Web、真实PostgreSQL、local/S3 artifact store、Chromium/processor、fuzz/race、CLI dry-run、`make verify-go`、Node22 `make verify-web`、Playwright和`git diff --check`全部通过。

## Out of Scope

- 不把record archive当作整套应用数据库/监控原始事实备份；系统backup/restore由platform/integration contracts负责。
- 不提供持续双向同步、跨实例实时复制、按名称自动合并或导入时回写外部系统。
- 不导入foreign notification收件箱、未读状态、channel secret、delivery retry/message ID，也不重发历史消息。
- 不删除`experience_logs`表；旧表/route的最终停写、默认切换和后续物理移除由integration child在对账及备份窗口条件通过后处理。
- 不以签名、管理员身份或“来自候风”声明绕过不可信输入检查。

## Execution Gate

- 状态保持`planning`；所有依赖与父任务排序合入、迁移号复核及用户再次明确批准后才可执行`task.py start`。

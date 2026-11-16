# APP 当前迁移与准入

## Scenario: APP current-development scoped migrator and one-snapshot runtime admission

### 1. Scope / Trigger

- 触发：修改 `HOUFENG_RECORDS_ENABLED` / `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` 模式选择、`houfeng-record-platform-admin migrate --scope app`、root migration、current APP fragment/compiler、manifest/catalog verifier、`ConvergeAppACLCurrent`、`AdmitAppACLCurrentRuntime`、center bootstrap、VPS importer，或其 PostgreSQL regression 时。
- current contract 的前 52 个 source 必须 byte-for-byte 等于冻结 `0001…0051` r1 inventory（包含两个按文件名字典序排列的 `0004_*`）；冻结 R1 仍为 52 sources、3 views。后来 embedded migration 必须注册 exact `AppACLCurrentMigrationFragment`，无 APP object 时注册 explicit empty fragment。当前 C81 有 82 个 source，止于 `0081_record_deletion_successor.sql`；post-R1 共 30 个 fragments。历史 C77/C78/C79/C80 分别为 78/79/80/81 sources、26/27/28/29 fragments，原字节不变。`0063`、`0064`、`0066`、`0069`、`0070`、`0072`、`0073`、`0074`、`0075`、`0078`、`0079` 为 explicit empty ACL fragment；`0071` 注册 access-management exact runtime grants，`0065` 的两个 runtime 表级 UPDATE 保留。`0067` 注册生命周期表、最小权限与 trigger hardening；`0068` 注册 host-address parser 的 runtime EXECUTE 与 invoker hardening；`0075` 只增加 settings ownership 列。`0076` 仅注册 family-state view 与 runtime SELECT，`0077` 注册 product 两表与七个 runtime grants；`0078` 只替换 view，`0079` 只增加 cache expiry 普通 B-tree 索引。`0080` 精确退休两个旧 replay 表，新增两个 bounded replay 表和两个 hardened invoker functions。`0081` 增加 manifest-bound deletion worker 与删除授权/客户端/comparison 存储合同。新增 SQL 与 fragment 只满足 source/catalog 覆盖；已发布数据库升级仍须更新全部 exact suffix 并注册独立 predecessor golden。
- 两个 record flag 都关闭时保留 legacy owner `migrate.Apply`。`records-on/delete-off` 必须先运行 current scoped migrator，随后 center/importer 只能以 runtime 身份执行 current admission。`false/true` 和 `true/true` 在读取 URL、`_FILE` secret、DNS、数据库、输入文件或外部域配置前失败。
- `ConvergeAppACLR1`、`AdmitAppACLRuntime` 与 isolated APP R2 bootstrap/finalize/runtime API 是冻结历史合同；保留其导出签名和 regression，但 product migration/startup 不再默认调用它们。
- frozen `AdmitAppACLRuntime` 必须在开启 transaction 前通过 `snapshotAppACLR1MigrationSources(migrations.FS)` 取得并验证 exact R1 prefix，再把已 canonicalize 的 frozen set 交给 manifest verifier。它不得把 R1 manifest/ledger 与会随 `0052+` 增长的完整 `CanonicalMigrationSetFromFS(migrations.FS)` 比较；后者会让新增 current migration 反向破坏冻结 R1 admission。

### 2. Signatures

- 模式入口：`config.LoadRecordPlatformMode() (config.RecordPlatformMode, error)`，只返回 `RecordPlatformModeLegacy` 或 `RecordPlatformModeRuntimeAdmission`。
- Writer：`ConvergeAppACLCurrent(ctx context.Context, db *pgxpool.Pool, runtimeRole, adminRole, deletionWorkerRole string) (AppACLManifestPersistedV1, error)`；`houfeng-record-platform-admin migrate --scope app` 是 records-on 的唯一 APP schema/ACL writer。current 配置必须显式提供 `HOUFENG_RECORD_PLATFORM_APP_DELETION_WORKER_ROLE`，不从缺省 auxiliary role 补齐。
- Runtime gate：`AdmitAppACLCurrentRuntime(ctx context.Context, db *pgxpool.Pool) error`；center/importer 在构造任何 repository 前调用。
- Worker gate：`AdmitAppACLCurrentDeletionWorker(ctx context.Context, db *pgxpool.Pool) error`；与 runtime 共用同一单 snapshot 验证流程，但 expected subject 为 `record_deletion_worker`，两个直接登录身份不可互换。
- Extension contract：`AppACLCurrentMigrationFragment{Migration, Objects, RetiredObjects []AppACLManagedObjectR1, Privileges, RevokedPrivileges []AppACLPrivilege, AuxiliaryPrivileges, Functions}`；fragment registry 与 `migrations.FS` 在 transaction 前 closed-world compile，later migration 与 fragment 必须一一对应。`RetiredObjects`、`RevokedPrivileges` 与其他 slice 一样在 input/compiled fragment/transition clone 中深拷贝，不允许调用者事后改变合同。撤权必须恰好命中有效旧 grant；不存在、重复、同 fragment 重授或超出已注册 transition delta 均失败。
- Typed cause：`migrate.ErrDevelopmentDatabaseRebuildRequired`。admin CLI 只允许该 safe sentinel 穿过 redaction boundary；任意数据库 error、raw SQL、role password 或 DSN 仍统一屏蔽。
- 持久化合同：`AppACLManifestPersistedV1.MigratorCatalogRole` 不可变且 digest-bound；runtime 从 persisted binding 构造 current catalog verifier，不读取 migrator credential/configuration。

### 3. Contracts

- `center_runtime`、`platform_admin`、`record_deletion_worker` 与 migrator 是四个预创建、两两不同、直接认证的 `LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS` role。四者的直接与递归 membership 均为空；migration、runtime/worker admission 都证明 `session_user == current_user`。`SET ROLE`、复用 owner、role membership、default ACL 或共享 login 都不能满足该合同。worker 不得拥有任何 catalog 对象；同一 admission snapshot 通过 `pg_shdepend` 检查，包括非 managed schema 的 type/extension 和其他数据库的 owner dependency，不能只检查 managed tables/functions。
- C81 privilege body 使用 magic `HOUFENG-APP-PRIVILEGE-SET-V2`，精确三个绑定、角色互异、grant option 为 0，沿用长度前缀与规范排序并拒绝尾字节。V1 保留真实历史两个 subject；manifest envelope/digest/bytea 列不变。允许 V1→V2，禁止 V2→V1；历史 source 使用明确 V1 projection，不把 current 的 worker binding 写回旧字节。worker 不是可缺省的 `AuxiliaryPrivileges`；catalog/DCL 必须覆盖全部三个 application binding。
- 同一未交付 `0081`/C81 还登记 local namespace/incarnation/anchor、root-independent write obligations/targets、local cleanup 与 adapter preparation/targets。namespace provision/admit 仅 direct platform-admin 可执行；runtime 执行窄 register/dispatch/return、正常发布及普通 cleanup，deletion-worker 使用独立 operation-bound purge 命令。新表不给 application role 直接 DML；受限函数撤 PUBLIC，固定 SECURITY DEFINER 与 `search_path=pg_catalog`。
- C81 撤销 runtime 对 Blob metadata、publication intent、upload part、pin、attachment、revision ref、import mapping 和 portability artifact 的原始发布 INSERT，以及 export artifact UPDATE；生产调用迁到受限命令，历史 C80 字节/权限投影不变。current 独立 catalog oracle 必须枚举 source/local 新对象和撤权，不能由生产 fragment 生成期望清单。
- C81 的 `source_legacy_retirement_install(bytea)` / `source_legacy_retirement_verify(bytea)` 仅授予 platform-admin EXECUTE；`source_legacy_retirement_blocked(text, text)` 向 runtime、platform-admin、deletion-worker 提供只读 mutation guard 查询。内部 command/binding 函数和 binding 表不授予 application role 原始写入权限，PUBLIC 无执行权。正式 handoff 安装/重验的是原 source 授权身份的永久退休绑定，不代表物理资产或内容已删除。
- C81 对 `comparison_result_dependencies`、`comparison_result_dependency_index_completions`、`comparison_client_workbenches` 和 `comparison_client_save_attempts` 仅授予 runtime SELECT；索引发布与认证副本修复通过 `record_comparison_index_write(bytea, bytea)` 复核持久路由、依赖覆盖并取得 comparison fence。相关 snapshot、lineage、reference、payload、record 与 epoch 的八个 guard trigger 纳入 current catalog 准入，缺失、禁用或定义漂移均拒绝。export job 的创建与推进分别经 `record_export_job_claim(bytea)` / `record_export_job_advance(bytea)`，不得绕过统一锁序与当前路由检查。
- source/fragment compiler 必须在 `BeginTx` 前拒绝 missing/extra/duplicate fragment、duplicate object/privilege、unknown subject、unmanaged privilege/function hardening，以及新 function 缺少 exact hardening。每个 fragment 的 `Privileges(databaseName)` callback 只在 source compile 时用固定验证数据库占位符求值一次；结果、fragment input、`RetiredObjects` 和 nested function config 都必须 defensive-copy。effective object/privilege/function inventory 从冻结 R1 按 migration 顺序统一物化：退休只移除当时存在的对象及其关联权限/函数合同，拒绝不存在或重复退休、同 fragment 重新声明、退休后悬空权限。catalog、manifest 与 DCL 共用物化结果，不得在各 reader 中分别硬编码过滤。后续 catalog compile 只替换 database tuple 占位符，不再次调用 callback。
- `FunctionReplacements` 是显式 ordered hardening 变更，每项为同一 function identity 的 `{Before,After AppACLCurrentFunctionContract}`，嵌入 input/compiled fragment、canonical source digest 与所有 prefix 的 defensive copy。materialize 必须在已有 managed/hardened predecessor 恰等 Before 时原位替换；缺 predecessor、wrong Before、重复 replacement、同 fragment add/retire/redeclare 混用以及非法 kind/config 在事务前拒绝，不放宽现有 duplicate 拒绝。C81 仅把 `validate_record_revision_primary_subject()` 从 invoker 改为 definer；0052 与冻结 C80 仍 invoker，独立 oracle 把它视为 changed-existing。准入另核该 validator、两个 exact deferred triggers 和查询表 owner/RLS，篡改后拒绝，事务 rollback 恢复。
- C81 的六个 `record_attachment_{purge_prepare,purge_verify,gc_claim,gc_complete,gc_retry,gc_resolve}_v1(bytea)` 仅授 deletion-worker EXEC；`record_serving_deletion_fence_lock(bytea)` 仅授 runtime EXEC。private helpers 无 application EXEC，内容及 GC/receipt 表不恢复裸 DML。具体 owner/witness、durable dispatch、exact-version receipt 与通用行锁合同见 [删除与恢复](../records/deletion-recovery.md)。
- C81 的 `public.record_deletion_permanent_fence_applied_v1(bytea)` 仅授 direct deletion-worker EXECUTE；runtime、platform-admin 与 PUBLIC 无执行权，worker 不因此取得 `record_purge_operations` 或其他业务原表 SELECT。该 wrapper 为 migrator-owned SQL SECURITY DEFINER、固定 `search_path=pg_catalog`，复用现有 private V1 continuation dispatcher 的十字段 envelope、source admission、锁序、exact owner 与独立 verified entry 检查；不新增 private helper。合法 live owner 的传播检查只读返回 bool，live deletion fence/content lease 未排空时为 `false`，排空为 `true`；身份、状态、ledger 或过期 owner 错误仍以 SQLSTATE `42501` 拒绝，不把 pending 转为 lost-owner/retry-required。
- C81 的历史 V1 markdown/comparison/collaboration purge/verify、`record_outbox_drain_v1(bytea)`、portability prepare/purge/verify、五个 local 和五个独立 S3 GC wrapper 均只授 direct deletion-worker EXECUTE；runtime、platform-admin、PUBLIC 无新增执行权，worker 无原表 SELECT/DML。private metadata/outbox helper 显式选择 version 1/2；local GC common helper 显式传 legacy authority 或 NULL，不保留旧 overload 或隐式 DEFAULT，坏 V1 envelope 不回退 V2。全部 exact identities、definer/search_path/revoke 与 trigger attachment 同步纳入 compiler 和独立 catalog oracle。
- C81 的 `record_portability_v1_preparations`、`record_portability_v1_targets`、`record_portability_v1_artifacts`、`record_portability_v1_obligations` 为 managed 控制表；application roles 无原始读写权。immutable inventory、target state、reference guard 和 GC barrier 只能由 owner 命令约束执行。inventory 与 logical handoff 在同一 COMMIT 持久化，未解决的完整 manifest 阻止匹配对象经 V1、普通或 V2 GC 取得物理 dispatch/receipt；不阻塞无关对象。具体资格、共享责任和 exact storage binding 见 [删除与恢复](../records/deletion-recovery.md)。
- `record_portability_purge_prepare_resolve_v1(bytea)` 同样仅授 direct deletion-worker EXECUTE；它以 prepare 的精确两字段输入只重验既存 immutable preparation/projection，保留 legacy authority 与锁序，不补 prepare/task/claim/dispatch/receipt。route-aware barrier implementation 为 owner-only private helper，六参数 legacy 入口以 unknown route 调用，不新增 application grant 或旧 wire 字段。
- C81 在 `record_write_obligations` 增加 S3-only、nullable、32-byte immutable `storage_binding`；真实 ManagedBlobWriter 从同一个 configured store 捕获，既有受限 register/dispatch/return/publication 命令核关联，角色权限不扩张。registration 使用单一新增精确字段 shape，无旧 C81 shim；local 为 NULL，历史未知不回填。完整 obligation discovery 与 snapshot 保留该值，readback 显式投影 hex。该来源证明不代替 remote capability/D3 closure，见 [删除与恢复](../records/deletion-recovery.md)。
- private `local_blob_gc_authority(jsonb,boolean,bytea,boolean)` 显式区分 pending-task preparation 与 strict dispatch authority，旧三参数 identity/hardening 删除，不保留 DEFAULT/overload；公共 GC wire/hash 不变。13 source-table guard、三个 parent-route dependency guard、GC task/link continuity check 逐项登记事件、attachment、args 和函数 hardening；deferred constraint check 同时核 `tgconstraint`、deferrable 与 initially-deferred，不能仅凭函数存在视为通过。新 private helper/trigger 不授 application EXECUTE 或表权限。
- current convergence 只接受 fresh C81 genesis、196607 个明确注册的 predecessor profile chain，以及 exact C81 target repeat。历史最初 11 条为 `[P62]`、`[P63]`、`[P64]`、`[P66]`、`[P62,P63]`、`[P62,P64]`、`[P62,P66]`、`[P64,P66]`、`[P63,P66]`、`[P62,P64,P66]`、`[P62,P63,P66]`；每阶段保留全部旧链，增加独立下一 predecessor genesis，以及全部旧链追加该 profile。P67/P68 得到23/47条，P69…P74 得到95/191/383/767/1535/3071条；P75…P79 得到6143/12287/24575/49151/98303条。C81 再加 `[P80]` 与全部98303条追加P80，共196607条；不能生成任意子集闭包或采样验收。升级只追加一个 C81 successor（revision 为原链长度+1），历史 manifest 保持原字节、按独立 profile/绑定验证，不以 persisted 权限作为授权来源。
- 十七个 profile 的 registry 顺序为 P62/P64/P63/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79，既有 ID 不变，P77 ID=15、P78 ID=16、P79 ID=17。P62 绑定独立 v0.79.4 golden；P63 绑定 `2cda12e874b9f9cd4431f477566b72c2d2a5e414`（v0.79.6）发布 compiler golden；P64 绑定 `5422f939`（v0.80.2）golden；P66 绑定 v0.80.3/v0.80.4 共用的独立 v0.80.4 compiler golden；P67 绑定 v1.15.3 基线 `2f9de0f88cb0fdac2924309c9d81c1c0d33ec751` 独立 compiler golden，止于 `0067`。P68 绑定 v1.15.7 基线 `8473dc306c47a2f5ad939508ab697b6528889fba` 独立 compiler golden，69 sources 止于 `0068`。P69 使用 C68 golden 加固定 `0069` checksum，权限不变；P70 使用 C71 compiler 扩展前独立冻结的 C70 71-source/19-fragment goldens；P71 使用 pristine C71 PostgreSQL 独立捕获的 72-source/20-fragment goldens；P72 使用 C71 golden 加固定 `0072` checksum，权限不变；P73 使用 C72 golden 加固定 `0073` checksum，权限不变；P74 使用 C73 golden 加固定 `0074` checksum，权限不变。P75 使用独立 C74 literal 加 `0075_add_settings_deployment_overrides.sql` 记录，SHA256 固定为 `a00bd66a3687e3278c0e9444538e6ba572f2452f57982dd9b082eecc1e90911a`；独立 C75 privilege golden 与 C74 字面内容相同，不从 C76 output 动态切片。P76 使用注册 C77 fragment 前从 C76 compiler 捕获的独立 `app_acl_current_c76_migrations.v1.bin` / `app_acl_current_c76_privileges.v1.bin`（77 sources、25 fragments）；C76 privilege body 是 C75 加一个 family-state SELECT，不从 C77 output 动态截取。P77 使用注册 C78 fragment 前从 C77 compiler 捕获的独立 C77 goldens（78 sources、26 fragments，止于 `0077`）。P78 使用注册 C79 fragment 前从原 C78 compiler 捕获的独立 C78 goldens（79 sources、27 fragments，止于 `0078`），migration SHA-256 为 `af2497ecde095a8f33e847dd21fd2f863764c86ff1a141aa40fac6ae6cb34ad3`、privilege SHA-256 为 `3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c`；C78 的 C77 source prefix 与 privilege body 保持原字节。P79 使用修改 compiler/注册 retirement 之前由原 C79 compiler 冻结的 `app_acl_current_c79_migrations.v1.bin`（5832 bytes、80 sources、28 fragments，止于 `0079`，SHA-256 `3879119c952bd0a59dee938a0468ce848070985bd0b6bd55a7da26d40263c7db`）与 `app_acl_current_c79_privileges.v1.bin`（35745 bytes，SHA-256 `3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c`）。C79 保持 C78 source prefix 与 privilege body 原字节；旧 goldens 与 P62 digest 不变。P63/P64/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79 genesis 允许合法自定义角色/数据库；禁止任意 prefix、未知 chain 或修改后 current compiler 动态截取作为 predecessor oracle。
- C81 在上述历史十七个 profile 后追加 P80（ID=18），共十八个。P80 使用修改 C80 compiler 前捕获、独立 C79+实际0080 delta 校验的 C80 golden：migrations 5900 bytes/SHA256 `496846a4343d7c1f5a09224504fa84cfe168f674d7d49228a3794e1de077cb3e`，privileges 35976 bytes/SHA256 `6df43d88036b64162256b8d76c467a872bdfb82c314021523ac8c0576d55078c`。原25个golden及0001–0080不变，禁止从修改后的C81 compiler截断制造前驱oracle。P80同样允许合法自定义数据库与角色。
- 所有历史 current suffix 末尾追加0081。P62/P64/P63/P66/P67/P68/P69 分别为 `0063…0081`、`0065…0081`、`0064…0081`、`0067…0081`、`0068…0081`、`0069…0081`、`0070…0081`；P70…P78 分别从0071…0079开始并结束于0081，P79为 `[0080,0081]`，P80为 `[0081]`。0080 replay delta仅适用于P79及更早：移除 `agent_sync_batches` INSERT，已存在0067时另移除 `agent_live_signals` SELECT/INSERT，新增 `agent_replay_states` SELECT/INSERT/UPDATE与 `agent_batch_receipts` SELECT/INSERT/DELETE；更早前驱不得先新增随后退休的grant。P80不重放该delta。各前驱已有lifecycle/parser/access-management/family-state/product精确增量仍保持，0081新增/撤销权限独立按固定合同核对；不得以旧空delta、子集匹配或角色变更掩盖current差异。
- Writer 保持单个 `SERIALIZABLE` transaction、advisory/ledger lock 与整体 serialization retry：先验旧 source/manifest/ledger/catalog，再业务 preflight、pending SQL、复读 ledger、业务后置验证、current revoke-first DCL、current catalog、追加 revision/CAS head、完整复读后 commit。失败整体回滚 schema、索引、ACL、ledger、manifest/head，包含恢复0080旧表以及撤销0081新对象/权限；漂移不能由DCL修复。exact C81 repeat 不执行SQL/DCL/manifest写入。heartbeat分类精确比较该profile注册的完整suffix，拒绝未知、缺漏、重复或错序，不再维护第二套逐个剥terminal语法。仅含0063的P62执行heartbeat 3→12/custom保持；按是否含0067判断lifecycle pending，0067仍拒绝需转换的旧业务数据。P67…P80升级不重跑已完成转换，0080只替换重放存储，不重建凭据、实例或业务/raw/aggregate历史。
- 维护观测仅接受 exact C80 或 compiled current APP。pool 入口先编译 source/transition，再开启只读 REPEATABLE READ；复用调用者事务的入口在读取 APP 前完成同一编译，并核实事务确为只读 REPEATABLE READ。manifest、migration、角色事实、managed catalog 与调用者的库存扫描共用同一事务；提交失败时丢弃观察，观察本身不授予写入或恢复权限。
- `0067` 的 settings 白名单变换为：retention 删除 event/notification days，设置 raw=30、aggregate=365；IPQuality 删除 raw/history days；其余字段保持原逻辑值。不得扩大为任意 settings 修复或修改冻结历史。
- 十八种注册 suffix 都把 settings singleton 无行视为合法快照：升级保持行存在性，不插默认行绕过缺行；有行仍验证heartbeat JSON/正阈值并只执行注册转换。P67…P80保持settings存在性、完整值及updated_at。0075只以add-column临时true default物化ownership列，再固定default=false；完整snapshot仅接受此新增字段。exact C81 repeat不写SQL/DCL/manifest、不把显式false回填true；P75及之后不重放0075，0080/0081不改变settings。
- C75 ownership 列终态验证同样适用于 fresh genesis 发布前和 genesis exact repeat；不能只检查 registered successor。缺列、错误类型、可空或 default 非 `false` 均拒绝，fresh 失败整体回滚，repeat 保持只读且不修复漂移。独立历史 genesis oracle 按其冻结 source 构造，不反向要求尚未存在的 `0075` 列。
- current catalog 从冻结 R1 base 加 ordered fragments 及精确退休统一物化，而非只做 additions 或按新旧 privilege 长度差预分配。`public.record_platform_cas_contract_activation_projection(bytea)` 与 `public.record_platform_cas_domain_rotation_projection(bytea)` 仍是 migrator-owned、`SECURITY DEFINER`、唯一 `bytea` overload、`search_path=pg_catalog` 且显式 revoke `PUBLIC`。
- production 0052 的九张 Records core table、hardened function 与 29 个 privilege tuple 不变；0065 两个 runtime UPDATE 保留，0066 为空。历史 0067 为 monitoring_agent_sessions、receiver_health、vps_followups、asset_service_associations、asset_domain_associations、vps_maintenance_actions 注册 runtime SELECT/INSERT/UPDATE；agent_live_signals、vps_archive_requests、monitoring_instance_lifecycle_receipts、vps_maintenance_effects 只注册 SELECT/INSERT。C80 仅移除其中 agent_live_signals 对象/权限，其他生命周期表及 ownership/session 两个 invoker hardening 不变；不得授任意 DELETE、Records content 读取或 immutable history UPDATE。
- `0068` 的 `public.houfeng_parse_host_address(text)` 为 `IMMUTABLE STRICT PARALLEL SAFE SECURITY INVOKER`，function kind `f`、`search_path=pg_catalog`。只解析 host identity，不读写表、不回填数据；scoped converger 同事务 revoke-first，仅新增 runtime EXECUTE，PUBLIC/admin 不获 EXECUTE。legacy raw migration 保留 PG 默认函数执行权限，不硬编码角色名。assigned view 使用 `CREATE OR REPLACE VIEW` 保留列合同、依赖与 ACL；latest view 不重建。
- `0069` 只新增 nullable CPU validity/count 列并允许六个 CPU 日聚合 avg/max 为 NULL，无默认、无回填、不重算 finalized 日；旧 host CPU marker 和日聚合有效计数仍为 NULL，历史 CPU 数值不变。计数 CHECK 拒绝负数、子计数超过 CPU 有效数及 CPU 有效数超过 host 样本数，允许 legacy NULL。无需新增 APP object、角色或 ACL tuple；既有自定义绑定和有效权限不变，PUBLIC/admin 不获新 grant。CPU 语义见 [监控合同](../monitoring.md) 与 [保留合同](../retention.md)。
- `0070` 只为 `record_import_jobs` 增加 nullable 目标主体 kind/source ID 与成对合法值 CHECK；旧行保持 NULL，不猜测回填、不加索引。没有新增 managed object 或权限 tuple，注册显式空 fragment；既有角色与权限不变。导入旧计划的拒绝和目标主体授权见 [可移植性合同](../records/portability.md)。
- `0071` 为 `users` 增加持久化 `is_supervisor`/`disabled_at`、单 supervisor partial unique index 与 supervisor contract；空库不回填，恰好一个已有 admin 才标记 supervisor，多用户或唯一非 admin 明确回滚。为 `record_access_groups` 回填稳定 `group_id` display name、施加 trim/1–100 CHECK 和 `(project_id,display_name)` UNIQUE；不删除组、不加可能破坏旧库的 membership user FK。新增 fragment 无 managed object/function，且只增加 runtime 对 `record_access_groups` 的 INSERT/UPDATE、`record_access_group_members` 的 INSERT/DELETE 四个精确 tuple。
- `0077` 为 reports 增加 nullable `product_json` / `product_summary_json`、providers 增加 nullable `details_json`、services 增加 nullable `checks_json`，不回填历史；新表为 `ip_quality_intelligence` 与带 `blocked_until` 的 `ip_quality_source_cache`。current fragment 只把两表加入 managed surface，并仅授予上述七个 center_runtime tuple；intelligence 不授 DELETE，cache 的 DELETE 仅用于缓存清理，platform_admin、PUBLIC、Records role 均不获新 grant，旧表 ACL 不变。SQL 本身不硬编码角色或 GRANT，由 scoped convergence 同事务发布 manifest 与精确 catalog。冻结 R1/R2 source/manifest/privilege contract 不变。
- `0078` 以 `CREATE OR REPLACE VIEW` 更新 `ip_quality_vps_family_state` 的 collection metadata 判定：revision 4 仍要求双族 `[4,6]`，revision 5 接受规范 `[4]`、`[6]`、`[4,6]`，当前行地址族必须在声明集合内；既有 view shape、grants 与历史报告事实不变，不新增 managed object/function 或 ACL。
- `0079` 只创建 `idx_ip_quality_source_cache_expires_at`，定义为 `create index idx_ip_quality_source_cache_expires_at on ip_quality_source_cache(expires_at);`，是普通非部分、非concurrent B-tree索引；fragment无Objects/Functions/Privileges增量，不改cache/settings/历史业务。历史C78→C79索引DDL、ledger、manifest同事务提交；current C81把pending索引DDL与0080/0081及successor同事务提交或回滚，repeat不重跑。不得用current-time partial index、事务外concurrent DDL或运维SQL替代。
- `0080` 的 managed catalog delta 恰为 DROP `agent_live_signals` / `agent_sync_batches`、CREATE `agent_replay_states` / `agent_batch_receipts` 与两个普通 invoker trigger functions；DROP 不带 CASCADE，未知依赖使整个 scoped transaction 回滚。state 为 `session_id text PRIMARY KEY REFERENCES monitoring_agent_sessions`、`producer_id uuid NOT NULL`，另有非负 bigint `retired_through`、`allocated_through`、`live_sequence_high`（默认 0，allocated>=retired）；receipt 为 `(session_id,batch_sequence)` 主键、session FK 指向 state、sequence>0、32-byte digest。无 received_at 列/索引、逐 signal 行或永久逐 ack 历史；只需两主键索引。runtime 仅获上述六 grants，state 无 DELETE、receipt 无 UPDATE；admin/PUBLIC/Records 无新增授权，两函数无直接 runtime/PUBLIC EXECUTE。
- `enforce_agent_replay_state()` 与 `enforce_agent_batch_receipt()` 固定 `search_path=pg_catalog`、全限定表名、kind=f、SECURITY INVOKER。state BEFORE UPDATE 禁止改 session/producer 或降低任一水位；同函数的 AFTER UPDATE OF retired_through 仅在 floor 提高时原子删除本 session seq<=floor 的 receipts，repository 不重复清理。receipt BEFORE INSERT/UPDATE/DELETE guard 在 INSERT/DELETE 路径以 FOR SHARE 读取 state，使 receipt 插入/删除与非键 floor 更新互斥；INSERT 需持久 floor<seq<=allocated 且 seq-floor<=131072，DELETE 只允许已退休 seq，UPDATE 始终拒绝。直接越界 SQL 返回 23514，runtime receipt UPDATE/state DELETE 因无权限为 42501；不能以 KEY SHARE 放过竞态或借清理权限擦除未退休收据。锁序为 graph→instance/session→replay state→receipt，死锁整笔回滚。
- 两新表 autovacuum_enabled=true、vacuum threshold=1000、scale factor=0.05，state fillfactor=80；不修改全局 Postgres 参数。每 session 一行 state、至多 131072 行未退休 receipts，正常 durable ack/floor 后仅少量在途记录；live 由独立持久高水位取代逐 signal 行。session/业务历史仍增长，dead tuples/文件高水位依赖 autovacuum 重用，逻辑有界不承诺立即缩小文件。
- 0080 切换前必须逐台停所有旧 Agent（含 evidence-only）并验证实际 buffer 为合法空数组；非空、离线、不可读均阻止升级，不能清空队列。协调备份数据库/附件/Records authority/凭据/queue 后，先升级 Center，再升级全部 Agent；迁移不导入旧随机身份，也不能自行证明跨主机排空。C80 后禁止旧协议 writer、单侧数据库或 queue 快照回滚及自动重新编号恢复；恢复须另行授权、停全部 writer 并恢复同一协调点。本合同不授权部署或恢复。
- canonical function privilege identity grammar 除既有 `public.<name>(bytea)` 外，只接受精确登记的 parser/text-set helper 与 source/local 零参数函数（包括 `local_blob_recovery_inventory()`、`record_revision_blob_fence()`）；不接受任意 overload 或近似拼写。语法识别不授予权限：历史仍按独立 golden 比较，current 按注册 fragment 的 exact privilege set 和 hardening 准入。纯 text-set helper 保持 IMMUTABLE STRICT SECURITY INVOKER，不替代持久 mutation authority。
- mandatory-worker successor 的函数身份白名单另精确包含 `public.record_comparison_index_write(bytea, bytea)` 和 `public.source_legacy_retirement_blocked(text, text)`；不扩展为任意双参数 overload，也不改变冻结历史 V1 的函数身份语法。
- parser 的新 SQL 参数声明为匿名 `text`，函数内部以 `p_raw ALIAS FOR $1` 读取，确保既有 `pg_get_function_identity_arguments` catalog 读取与 `(text)` 合同一致；不提供 `p_raw => ...` 命名调用接口。不得全局去掉已发布函数身份中的参数名，也不为 parser 绕过 owner/ACL/hardening 检查。
- managed catalog准入验证compiled database/schema/relation/view/sequence/function、ledger/manifest、role attributes/membership、owner、ACL与function hardening，另执行worker全对象ownership禁止。placement、fresh-state与legacy-ledger companion-object preflight按完整 `(schema, object identity)` 检查，同名不同schema互不混淆。冻结R1裸名称shadow rejection保持；managed private schema内unknown object仍是drift，无关schema/object与第三方owner default ACL应接受，但不能借此让worker拥有对象。
- PostgreSQL 16 `pgcrypto` 必须位于 `record_platform_internal`，否则fail closed。extension-member procedures按OID识别并保持opaque，因为受限migrator不能可靠改写bootstrap-owned member ACL；opacity不产生reachability。PUBLIC/runtime/admin/worker均无private schema USAGE/CREATE；同一snapshot拒绝同时具有schema USAGE和function EXECUTE的reachable opaque member。migrator-owned helper/projector仍显式revoke PUBLIC。
- `AdmitAppACLCurrentRuntime` 与 `AdmitAppACLCurrentDeletionWorker` 各精确开启一个 `REPEATABLE READ READ ONLY` transaction，共享验证实现。先验证完整注册链；P62…P80旧终点返回需要successor convergence的typed拒绝，仅C81终点比较current privileges并验证catalog及expected subject。没有DDL/DCL、writer调用或migrator凭据读取；失败关闭pool，不得owner-migrate或warning-only fallback。升级必须先停止旧Center/processor及相关Agent writer，不能由schema准入推断旧I/O已排空。

### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| 两个 flag 都为 false | 选择 legacy path；现有 owner `migrate.Apply` 行为继续允许。 |
| `false/true` 或 `true/true` flag | 在读取 URL/secret/file/network/external-domain 前 fail；不连接 database，也不执行 migration。 |
| records-on/delete-off | admin 默认调用 `ConvergeAppACLCurrent`；center/importer 默认调用 `AdmitAppACLCurrentRuntime`；禁止 product fallback 到 frozen R1、R2 或 `migrate.Apply`。 |
| embedded root set 追加 `0052+`，数据库仍为 exact R1 manifest/ledger | frozen `AdmitAppACLRuntime` 只验证已固定的 `0001...0051` prefix 并成功；新增 current source 不得改变 R1 admission 结果。 |
| frozen R1 prefix 缺失、顺序变化或 SQL bytes 漂移 | `AdmitAppACLRuntime` 在 `BeginTx` 前 fail closed；不得退化为完整 embedded set 或跳过 checksum contract。 |
| embedded post-`0051` migration 缺 fragment，或 fragment extra/duplicate/invalid | transaction 前拒绝；`BeginTx` 调用次数为 0。 |
| fragment privilege callback 有状态，或 callback 返回的 captured slice 在 source compile 后被修改 | callback 调用次数固定为 1；catalog/manifest 使用 source compile 时深拷贝的 template，后续状态不能改变合同。 |
| fresh：无 ledger/manifest/managed object | apply exact current set、DCL/catalog verify、一个 genesis，全 transaction atomic。 |
| exact current：source/manifest/catalog 全匹配 | migrate 与 runtime 都成功；repeat 前后 durable snapshot 深相等。 |
| applied/manifest source 数量、filename 或 raw-byte checksum 不同 | `errors.Is(err, ErrDevelopmentDatabaseRebuildRequired)`；catalog read 与所有 durable write 为 0。 |
| nullable historical head、未注册 predecessor 或未知 successor revision | rebuild-required；不得 generic-adopt、repair 或读取 catalog。 |
| 注册的 P62/P64/P63/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79/P80 起点或 chain，满足业务前置条件 | 旧 catalog 先验，exact suffix 升级、事务 DCL、追加 C81 revision/head CAS；失败整体回滚。runtime/worker 只拒绝旧终点，不自行升级。 |
| suffix 含 `0067` 且存在需转换的旧 VPS/监控/服务/域名/Target/订阅数据 | `0067` 因 `requires a fresh installation` 明确拒绝；不猜测状态、不删数据、不改变原 ledger/manifest/catalog。 |
| P67 genesis 或原11 chains追加P67，含既有业务记录 | 执行 `0068…0081`，保留业务/settings/manifest；parser/access-management/family-state/product、0080 replay与0081权限差量分别精确验证，C81后准入。 |
| P68 genesis 或原23 chains追加P68，含旧host/finalized日记录 | 执行 `0069…0081`；保留CPU数值、NULL legacy marker/count、settings/history/ledger前缀，revision+1。既有增量、0080和0081独立验证，失败整体回滚。 |
| P69/P70 genesis或历史chain | 分别执行 `0070…0081` / `0071…0081`；旧导入主体列保持NULL，保留业务/settings/history，runtime reject-before/admit-after，repeat无durable drift。 |
| P71/P72/P73/P74/P75 genesis及历史chain | 分别从0072/0073/0074/0075/0076执行至0081。0073 metadata回填、0074实时汇率、0075 ownership/default/snapshot合同不变；旧报告事实保持。额外view UPDATE、admin/PUBLIC SELECT或grant option拒绝且不修复。 |
| P76 genesis或C76历史chain | 执行 `[0077,0078,0079,0080,0081]`；产品七grants、replay、C81差量独立验证。保留报告/children/settings/历史manifest，新历史列NULL，无合成job/cache；intelligence DELETE与额外权限仍拒绝。 |
| P77/P78 genesis或历史chain | 分别执行 `[0078,0079,0080,0081]` / `[0079,0080,0081]`；0078/0079自身zero-ACL且保留view shape/业务/cache/history。验证实际B-tree与精确replay/C81差量；失败恢复旧表、schema、ACL和历史。 |
| P79 genesis或独立C79历史chain | 执行 `[0080,0081]`；0080两drop/两table/两invoker与三旧grant移除/六新grant独立验证，再验证0081差量。业务/raw/aggregate/online/session/settings/history保持，不导入随机重放身份。 |
| P80 genesis或独立C80历史chain | 只执行0081；原C80 source/privilege golden及全部旧manifest保持，V1→V2，replay catalog与数据不重建。DCL后失败撤销C81 schema/权限/ledger，成功repeat不写。 |
| current suffix缺漏、重复、错序或不以0081结束；P80含0080 | exact registry分类拒绝；历史zero-ACL fragment不放宽顺序、绑定、权限差量或退休白名单。 |
| RetiredObjects 不存在/重复、同 fragment 重新声明、悬空权限，或注册 transition 退休非指定对象 | source compile / transition delta 在事务前拒绝；不能任意删除其他 managed object 或降低权限合同。 |
| 0080 DROP 遇未知依赖，或 DDL/DCL/manifest seam 失败 | 单事务回滚；旧 replay 表/记录、ACL、ledger、manifest/head 与业务事实全部恢复，runtime 仍拒绝旧终点。 |
| runtime 降低任一 replay 水位、改变 producer/session、插入退休/超窗口 receipt、删除未退休 receipt | SQLSTATE 23514；state DELETE/receipt UPDATE 因无权限为 42501。floor 合法推进与 receipt 清理原子提交，双连接不能留已退休 receipt。 |
| malformed manifest chain、exact-source catalog/owner/ACL/function drift | 返回具体 fail-closed corruption/catalog error；不得误标为 rebuild-required。 |
| 任一 role 不是不同的直接 constrained `LOGIN NOINHERIT` role，具有 direct/recursive membership，或 `session_user != current_user` | 在 scoped migration/admission 前 fail closed。`SET ROLE` runtime snapshot 精确拒绝为 `session user %q does not match current user %q`。 |
| worker缺失、与runtime/admin/migrator复用、非direct login或拥有非managed type/extension等对象 | convergence及current runtime/worker admission拒绝；无缺省角色、SET ROLE或辅助角色旁路。 |
| V2 body缺worker、重复/未知subject、grant flag非0、错序/截断/尾字节，或manifest链V2→V1 | 编码/解析或链验证拒绝，不以历史V1解释新authority。 |
| current compiler output与persisted privileges不同，或runtime/admin/worker取得未编译privilege | catalog/manifest drift，拒绝。worker对content/gate/receipt无直接DML，不能凭schema准入签发删除授权。 |
| managed object/grant/column ACL/default ACL/owner 或 projector definition drift | 拒绝；projector 必须 owner-only、唯一 `bytea`、`SECURITY DEFINER`、显式 revoke `PUBLIC`，并使用 `search_path=pg_catalog`。 |
| 不同 compiled managed schema 声明同名 relation/function tuple | 独立编译并按 exact tuple 检查；fresh/legacy preflight 不得因裸名称冲突拒绝。 |
| unrelated schema 中存在 managed relation/function 的同名对象或其他 overload，或存在 unrelated-owner default ACL | 只要完整 tuple 不能影响 compiled managed surface 就接受；冻结 R1 regression 继续保留历史 shadow rejection。 |
| `pgcrypto` 位于 `record_platform_internal` 之外 | fail closed（migration/convergence precondition 是 SQLSTATE `55000`）。 |
| runtime/admin/worker/PUBLIC取得 `record_platform_internal` 的USAGE或CREATE，或opaque extension member变为reachable | 拒绝catalog admission；不得以extension opacity隐藏可执行权限。 |
| scoped migrator 收到 serialization failure | rollback 后重试整个 `SERIALIZABLE` closure；任一不可重试错误都不留下 partial ledger、ACL、revision 或 head state。 |
| `BeginTx` 异常返回 `(nil, nil)` | 返回 defensive error；禁止注册 nil transaction rollback 导致 panic。 |

### 5. Good / Base / Bad Cases

- Good：两个 flag 都关闭时保留 legacy migration；records-on/delete-off 时 direct migrator fresh converge exact current，direct runtime 在 repository 打开前通过 current one-snapshot admission。
- Base：当前 embedded set 是冻结52-source R1 prefix加 `0052…0081` 共30个exact fragments；fresh发布C81 revision-1 genesis，exact repeat/direct runtime/worker admission不改durable state。
- Good：十八个独立profile沿196607个起点/链升级至C81；全部历史C80及更早oracle保留。所有旧suffix追加0081、P80只含0081；历史family-state/product/zero-ACL/replay差量与C81差量分别验证，不能把历史空delta当current空delta。
- Good：未来 child 同 PR 添加 SQL 与 exact fragment，compiler 在 transaction 前证明一一覆盖；同时更新 current exact suffix，并用独立发布 golden 明确注册支持的 predecessor/历史链，才能支持升级，不能只靠登记 fragment。
- Good：binary 已嵌入 `0052+`，strict R2 PostgreSQL anchor 中的 R1 fixture 仍可调用 frozen `AdmitAppACLRuntime`；admission 只消费 validated R1 prefix，而 current admission 独立消费完整 current set。
- Good：fragment callback 在 source compile 返回 privilege slice 后，调用方修改 captured slice 或 callback 自身状态；current catalog 仍使用首次物化的深拷贝结果，callback 不会再次执行。
- Good：第三方 schema 及其第三方 owner 的 default ACL 可以保留而不扩张 APP role，所以 scoped admission 接受它们。
- Good：第三方 schema 可以拥有 `monitoring_instances` 或 `record_platform_cas_contract_activation_projection(bytea)` 同名对象；current path 只检查 compiled schema/identity tuple，fresh convergence 与 runtime admission 仍成功。
- Bad：把任意合法 migration prefix当作可升级 predecessor，或从current compiler动态截取prefix同时充当产品matcher与测试oracle；这会把未知旧状态错误发布为受支持successor。
- Bad：把 unknown checksum、null head 或 unknown successor revision 当作 generic error，CLI 会丢失唯一安全可操作的 rebuild cause；只测 migration 数量变化不能覆盖该状态矩阵。
- Bad：对任一 projector 给 runtime/admin grant、把通用 `REVOKE EXECUTE ON ALL FUNCTIONS` 当作 PG16 `pgcrypto` hardening evidence，或按 extension-member name 过滤，都会创建 callable privilege 或隐藏 non-extension drift。
- Bad：分开开启 manifest/catalog transaction、以 member login 后 `SET ROLE`、product route 调用 frozen R1/R2 或 `migrate.Apply`、admission failure warning-only，都会破坏 exact-current boundary。
- Bad：frozen `AdmitAppACLRuntime` 的 verifier closure 直接捕获 `migrations.FS` 并调用 full-set verifier；第一次追加 current migration 后，exact R1 manifest 会被误报为 `latest app ACL manifest migration set does not match embedded migrations`。
- Bad：source preflight 调用一次 `Privileges(validationDatabase)`，catalog compile 又调用一次 `Privileges(actualDatabase)`；stateful callback 或 captured slice 可以让事务前验证与实际 privilege contract 不一致。
- Bad：只 DROP 旧表却不从有效 managed/privilege inventory 退休、在各 reader 中各自过滤、对任意 grant removal 放行，或给 receipt UPDATE/state DELETE；这些做法会让 manifest/catalog 脱节或打开擦除重放证据通道。

### 6. Tests Required

- Current compiler/convergence/runtime 与 caller selector：

  ```bash
  go test ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin \
    ./cmd/houfeng-center ./cmd/houfeng-import-vps-json -count=1
  ```

  必须覆盖 missing/registered fragment、transition missing/duplicate/unknown/out-of-order/overlap/privilege drift、独立历史 goldens 逐 entry/byte 匹配、callback 单次求值与所有 slice（含 RetiredObjects）defensive copy、ordered retirement/不存在或重复退休/同 fragment 重声明/悬空权限拒绝，以及只允许 0080 两旧表退休的 exact whitelist。保留跨 managed schema 同名 tuple、unrelated ledger companion、future private schema、fresh/exact/registered cutpoints、serialization retry、count/name/checksum mismatch、null head、unknown successor、SET ROLE、catalog drift、nil transaction、safe sentinel、legacy Apply 与默认 binding 回归。事务前 mismatch 断言 BeginTx=0，事务内 cutpoint 断言 rollback 且后续 seam 未调用。
- Real PostgreSQL current suite 必须经 strict wrapper 运行；locally skipped test 不构成 evidence：

  ```bash
  scripts/test-record-platform-integration.sh postgres -- \
    go test ./internal/center/store/migrate \
    -run '^(TestPostgresIntegrationAppACLCurrent.*|TestPostgresIntegrationAgentReplayMigration|TestPostgresIntegrationIPQualityProductMigration|TestPostgresIntegrationIPQualityCollectionScopeMigration|TestPostgresIntegrationIPQualityCacheExpiryMigration)$' -count=1
  ```

  完成下列聚焦 package 检查：

  ```bash
  go test ./internal/center/store/migrate -count=1
  go vet ./internal/center/store/migrate
  ```

- 断言fresh/direct runtime及worker、全部predecessor genesis/chains、repeat、自定义绑定与历史manifest字节保持；P66及 `[P62,P63,P66]` 历史基线不变。`TestClassifyAppACLCurrentManifestShapeRegisteredChains` 独立枚举全部196607条C81 chains/successor，不从生产accepted-chain表派生；全部C80及更早oracle保留。固定数据库/角色可复用已与独立golden核对的compiled predecessor body，自定义绑定仍完整重编译；不得跳过manifest解析、digest链、source或privilege字节比较。既有heartbeat、0067 fail-closed、索引/约束/ownership、CPU nullable/count、settings、family-state/product与zero-ACL合同保持；0080 replay及0081 delta分别精确验证。
  生命周期与 retention 集成用当前 `ConvergeAppACLCurrent` 和 `AdmitAppACLCurrentRuntime`，不能以冻结 R1 fixture 代替。覆盖原子归档、重复退役拒绝、关联历史、健康观察、可信在线、保留边界及 runtime 越权拒绝。
- `TestPostgresIntegrationAppACLCurrent` umbrella 保留原 P67、P68、settings、业务、repeat、parser/PUBLIC revoke、drift/rollback 覆盖，并包含 `agent_replay_migration`。`scripts/test-pg16-catalog.sh postgres:16.0`、`:16.6`、`:16.12` 分别经 `pg16-catalog` strict lane 要求精确三个顶级 anchors `TestPostgresIntegrationAppACLR2`、`TestPostgresIntegrationAppACLCurrent`、`TestPostgresIntegrationAgentReplayMigration` 的 RUN/PASS，任意 skip/fail 或缺失事件都失败。独立 product/collection-scope/cache-expiry suites 仍须按上面的完整 selector 运行，不能以 replay 专项替代 current umbrella、历史迁移或 R2 准入门禁。
- fresh C81 与 registered-successor 的原数据库分别验证 runtime 原始 root INSERT/DELETE 拒绝、孤立 revision/subject 的真实外键拒绝，以及 worker/admin 无正文读取权。需要既存 Records 的附件与协作 ACL 检查使用另一个明确标注的 C80 seed→C81 fixture；不把该夹具当作 fresh C81 birth、已授权 purge 或 same-APP source ceremony 的证据。
- umbrella内 `p68_upgrade` 保留24条C68-ended历史（P68-only与原23 chains追加P68），P62固定角色、其他自定义绑定。验证reject-before/admit-after、revision+1、digest/history/ledger前缀、0069及repeat，旧host/finalized日不回填、CPU nullable/count CHECK。current终点0081，P70/P71/P72/P73分别执行 `0071…0081` / `0072…0081` / `0073…0081` / `0074…0081`。先独立验证并投影精确C81 delta，再验证/投影C80 replay delta后复用历史catalog断言，不隐藏额外ACL/owner/function/role漂移。applyPending/applyDCL后故障证明schema/constraint/CPU facts回滚；P73汇率共存/唯一/CHECK保持。
- settings回归保留P67三态与早期前驱缺行升级；P74三态验证0075仅增ownership=true、完整JSON/时间保护、缺行保持、新行default=false、显式false repeat不变、boolean NOT NULL default false。保留post-apply tamper、列结构corruption、rollback、runtime实际操作/DDL拒绝、历史linkage与双连接初始化，不补默认行或放宽验证。current验证family-state/product、replay及C81差量；0080/0081不改settings。历史C75→C76→C77 ledger/history/revision/repeat仍受保护。
- `TestPostgresIntegrationIPQualityProductMigration` 保留历史C75→C76→C77七tuple及独立C76三种settings状态；current升级至C81，产品、replay、C81差量分别验证。保留product DCL后失败回滚、五类额外权限拒绝、runtime intelligence SELECT/INSERT/UPDATE与cache CRUD、intelligence DELETE=42501。旧报告/来源/服务、非退休catalog与history保持，新历史字段NULL，无合成job/cache，repeat深相等。
- `TestPostgresIntegrationIPQualitySyncMetadataRoundTrip/0076_preserves_C75_reports_and_children` 继续用test-only C75列清单构造真实前驱，先独立应用0076逐列比对，再应用current检查新字段NULL与族读模型，不增加backward shim。C76 golden、product七tuple与suffix回归保留；current分类精确比较注册完整suffix，缺漏/重复/错序均拒绝。
- `TestPostgresIntegrationIPQualityCollectionScopeMigration` 保留独立C77 genesis/history与revision4/5、view shape、facts/settings/cache/history证据；current至C81时额外验证replay与C81 delta。cache-expiry从独立C78升级C81，验证实际B-tree、repeat、DDL/manifest故障回滚、admission/ACL drift；历史C78→C79冻结source/空权限不变，oracle不从current截断生成。
- `TestPostgresIntegrationAgentReplayMigration` 覆盖fresh current C81、独立C79 genesis与C76→C77→C78→C79 history，C80两drop/两table/两invoker与C81差量分别验证。保存session/凭据/trusted-online/业务/raw/aggregate/settings/history全行，runtime真实INSERT/SELECT/UPDATE/retirement cleanup、无权限/23514拒绝、reject-before/admit-after、repeat、无关权限/伪retirement、DDL/DCL/manifest故障及unknown DROP dependency回滚。保留C79 golden/hash、clone、invalid retirement；current terminal为0081。业务strict gate保留AgentReplayWindow、RetentionConcurrentSync、RetentionPreservesHistoryAndFinalizedBuckets，receipt guard/floor互斥用真实PG阻塞/提交结果验证。
- `TestPostgresIntegrationRecordDeletionSuccessorAuthority` 验证独立C80→C81、DCL后失败回滚、历史V1 bytes与current V2、worker direct admission、额外content grant及非managed type ownership拒绝。V2编码回归覆盖逐byte截断、错序/重复/未知subject、缺worker、grant option、尾字节与V2→V1降级。
- Frozen regression：完整 migrate package run 必须保留 `ConvergeAppACLR1` null-head adoption、`AdmitAppACLRuntime` one-snapshot，以及 isolated R2 bootstrap/finalize/runtime suites；current product caller 不得路由到它们。strict `TestPostgresIntegrationAppACLR2` 的 R1 reader/runtime subtest 必须在 binary 已嵌入 `0052+` 时实际调用 `AdmitAppACLRuntime` 并通过，不能只测 injected verifier 或 zero-test compile。
- Full gate 与 static writer audit：

  ```bash
  make verify-go
  rg -n 'ConvergeAppACLR1|ConvergeAppACLCurrent|AdmitAppACLRuntime|AdmitAppACLCurrentRuntime|migrate\.Apply' \
    cmd/houfeng-record-platform-admin cmd/houfeng-center cmd/houfeng-import-vps-json
  ```

  审查 `migrate.Apply` 仅位于 legacy defaults，current writer/runtime 是 records-enabled product defaults，R2 commands 仍独立。

### 7. Wrong vs Correct

```go
// 错误：records-on startup 复用 owner migration writer。
if err := migrate.Apply(ctx, db.Pool()); err != nil {
    return err
}

// 正确：mode 选择互斥的 legacy migration 或 current runtime admission。
switch cfg.RecordPlatformMode {
case config.RecordPlatformModeLegacy:
    err = applyMigrations(ctx, db)
case config.RecordPlatformModeRuntimeAdmission:
    err = migrate.AdmitAppACLCurrentRuntime(ctx, db.Pool())
}
```

```sql
-- 错误：把每个 persistent schema/default-ACL owner 都视作 APP-managed。
where namespace.nspname !~ '^pg_'
  and namespace.nspname <> 'information_schema'

-- 正确：object reader 接收 compiled current inventory；default-ACL reader 是单独查询，
-- 并只按 persisted migrator role 限定范围。
where namespace.nspname = any($1::name[]) -- compiled managed schema inventory

-- 单独的 default-ACL query：
where default_acl.defaclrole = $2
  and (
    default_acl.defaclnamespace = 0
    or namespace.nspname = any($3::name[])
  )
```

```go
// 错误：每个 reader 各自开启 snapshot，随后 startup 修复 drift。
manifest := NewPostgresAppACLManifestRuntimeReader(db).ReadAppACLManifestRuntimeSnapshotV1(ctx)
catalog := VerifyPostgresAppACLEffectiveCatalogR1(ctx, db, input)
_ = migrate.Apply(ctx, db)

// 正确：一个 direct-runtime REPEATABLE READ READ ONLY transaction 检查
// identity + manifest + ledger + scoped catalog，然后只会 admit 或 stop。
if err := migrate.AdmitAppACLCurrentRuntime(ctx, db); err != nil {
	return fmt.Errorf("admit app runtime: %w", err)
}
```

```go
// 错误：冻结 R1 admission 读取会增长的完整 embedded set。
return verifyAppACLManifestRuntimeSnapshotV1(snapshot, migrations.FS)

// 正确：先验证并截取 exact frozen prefix，再复用已 canonicalize 的 set。
frozenSources, err := snapshotAppACLR1MigrationSources(migrations.FS)
if err != nil {
	return err
}
return verifyAppACLManifestRuntimeSnapshotWithMigrationSetV1(snapshot, frozenSources.canonicalSet)
```

```go
// 错误：不同 source 只给 generic error，caller 无法安全提示重建。
return fmt.Errorf("checksum mismatch for %q", filename)

// 正确：count/name/checksum、null head、valid successor 都保留 typed cause。
return fmt.Errorf("%w: checksum mismatch for %q", ErrDevelopmentDatabaseRebuildRequired, filename)
```

```go
// 错误：transaction-specific catalog compile 再次调用 public callback。
privileges := fragment.Privileges(databaseName)

// 正确：source compile 已物化并深拷贝 template；这里只替换 database tuple 占位符。
privileges, err := appACLCurrentPrivilegesForDatabase(fragment.Privileges, databaseName)
```

---

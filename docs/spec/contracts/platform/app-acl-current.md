# APP 当前迁移与准入

## Scenario: APP current-development scoped migrator and one-snapshot runtime admission

### 1. Scope / Trigger

- 触发：修改 `HOUFENG_RECORDS_ENABLED` / `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` 模式选择、`houfeng-record-platform-admin migrate --scope app`、root migration、current APP fragment/compiler、manifest/catalog verifier、`ConvergeAppACLCurrent`、`AdmitAppACLCurrentRuntime`、center bootstrap、VPS importer，或其 PostgreSQL regression 时。
- current contract 的前 52 个 source 必须 byte-for-byte 等于冻结 `0001…0051` r1 inventory（包含两个按文件名字典序排列的 `0004_*`）；冻结 R1 仍为 52 sources、3 views。后来 embedded migration 必须注册 exact `AppACLCurrentMigrationFragment`，无 APP object 时注册 explicit empty fragment。当前 C80 有 81 个 source，止于 `0080_bound_agent_sync_replay.sql`；post-R1 共 29 个 fragments。历史 C77/C78/C79 分别为 78/79/80 sources、26/27/28 fragments，原字节不变。`0063`、`0064`、`0066`、`0069`、`0070`、`0072`、`0073`、`0074`、`0075`、`0078`、`0079` 为 explicit empty ACL fragment；`0071` 注册 access-management exact runtime grants，`0065` 的两个 runtime 表级 UPDATE 保留。`0067` 注册生命周期表、最小权限与 trigger hardening；`0068` 注册 host-address parser 的 runtime EXECUTE 与 invoker hardening；`0075` 只增加 settings ownership 列。`0076` 仅注册 family-state view 与 runtime SELECT，`0077` 注册 product 两表与七个 runtime grants；`0078` 只替换 view，`0079` 只增加 cache expiry 普通 B-tree 索引。`0080` 精确退休两个旧 replay 表，新增两个 bounded replay 表和两个 hardened invoker functions。新增 SQL 与 fragment 只满足 source/catalog 覆盖；已发布数据库升级仍须更新全部 exact suffix 并注册独立 predecessor golden。
- 两个 record flag 都关闭时保留 legacy owner `migrate.Apply`。`records-on/delete-off` 必须先运行 current scoped migrator，随后 center/importer 只能以 runtime 身份执行 current admission。`false/true` 和 `true/true` 在读取 URL、`_FILE` secret、DNS、数据库、输入文件或外部域配置前失败。
- `ConvergeAppACLR1`、`AdmitAppACLRuntime` 与 isolated APP R2 bootstrap/finalize/runtime API 是冻结历史合同；保留其导出签名和 regression，但 product migration/startup 不再默认调用它们。
- frozen `AdmitAppACLRuntime` 必须在开启 transaction 前通过 `snapshotAppACLR1MigrationSources(migrations.FS)` 取得并验证 exact R1 prefix，再把已 canonicalize 的 frozen set 交给 manifest verifier。它不得把 R1 manifest/ledger 与会随 `0052+` 增长的完整 `CanonicalMigrationSetFromFS(migrations.FS)` 比较；后者会让新增 current migration 反向破坏冻结 R1 admission。

### 2. Signatures

- 模式入口：`config.LoadRecordPlatformMode() (config.RecordPlatformMode, error)`，只返回 `RecordPlatformModeLegacy` 或 `RecordPlatformModeRuntimeAdmission`。
- Writer：`ConvergeAppACLCurrent(ctx context.Context, db *pgxpool.Pool, runtimeRole, adminRole string) (AppACLManifestPersistedV1, error)`；`houfeng-record-platform-admin migrate --scope app` 是 records-on 的唯一 APP schema/ACL writer。
- Runtime gate：`AdmitAppACLCurrentRuntime(ctx context.Context, db *pgxpool.Pool) error`；center/importer 在构造任何 repository 前调用。
- Extension contract：`AppACLCurrentMigrationFragment{Migration, Objects, RetiredObjects []AppACLManagedObjectR1, Privileges, AuxiliaryPrivileges, Functions}`；fragment registry 与 `migrations.FS` 在 transaction 前 closed-world compile，later migration 与 fragment 必须一一对应。`RetiredObjects` 与其他 slice 一样在 input/compiled fragment/transition clone 中深拷贝，不允许调用者事后改变合同。
- Typed cause：`migrate.ErrDevelopmentDatabaseRebuildRequired`。admin CLI 只允许该 safe sentinel 穿过 redaction boundary；任意数据库 error、raw SQL、role password 或 DSN 仍统一屏蔽。
- 持久化合同：`AppACLManifestPersistedV1.MigratorCatalogRole` 不可变且 digest-bound；runtime 从 persisted binding 构造 current catalog verifier，不读取 migrator credential/configuration。

### 3. Contracts

- `center_runtime`、`platform_admin` 与 migrator 是三个预创建、两两不同、直接认证的 `LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS` role。三者的直接与递归 membership 均为空；migration 与 runtime admission 都证明 `session_user == current_user`。`SET ROLE`、复用 owner、role membership、default ACL 或共享 login 都不能满足该合同。
- source/fragment compiler 必须在 `BeginTx` 前拒绝 missing/extra/duplicate fragment、duplicate object/privilege、unknown subject、unmanaged privilege/function hardening，以及新 function 缺少 exact hardening。每个 fragment 的 `Privileges(databaseName)` callback 只在 source compile 时用固定验证数据库占位符求值一次；结果、fragment input、`RetiredObjects` 和 nested function config 都必须 defensive-copy。effective object/privilege/function inventory 从冻结 R1 按 migration 顺序统一物化：退休只移除当时存在的对象及其关联权限/函数合同，拒绝不存在或重复退休、同 fragment 重新声明、退休后悬空权限。catalog、manifest 与 DCL 共用物化结果，不得在各 reader 中分别硬编码过滤。后续 catalog compile 只替换 database tuple 占位符，不再次调用 callback。
- current convergence 只接受 fresh C80 genesis、98303 个明确注册的 predecessor profile chain，以及 exact C80 target repeat。保留原 47 条 P62/P63/P64/P66/P67/P68 历史：原 11 个 `[P62]`、`[P63]`、`[P64]`、`[P66]`、`[P62,P63]`、`[P62,P64]`、`[P62,P66]`、`[P64,P66]`、`[P63,P66]`、`[P62,P64,P66]`、`[P62,P63,P66]`，加 `[P67]` 与这 11 条追加 P67 的版本，再加 `[P68]` 与前述 23 条追加 P68 的版本。在每阶段保留所有旧链、增加独立下一 predecessor genesis、以及全部旧链末尾追加下一 profile：依次 P69、P70、P71、P72、P73、P74，形成 95/191/383/767/1535/3071 条；历史 C76 保留全部 3071 条 C75 chains，增加 `[P75]` 与全部旧链追加 P75，合计 6143。C77 增加 `[P76]` 与全部旧链追加 P76，合计 12287；C78 增加 `[P77]` 与全部旧链追加 P77，合计 24575；C79 增加 `[P78]` 与全部旧链追加 P78，合计 49151。C80 保留全部 49151 条，增加 `[P79]` 与全部旧链追加 P79，合计 98303；不能生成任意子集闭包。升级只追加一个 C80 successor（revision 为原链长度 + 1），历史 manifest 保持原字节、按原 profile/绑定验证，不以 persisted 权限作为授权来源。
- 十七个 profile 的 registry 顺序为 P62/P64/P63/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79，既有 ID 不变，P77 ID=15、P78 ID=16、P79 ID=17。P62 绑定独立 v0.79.4 golden；P63 绑定 `2cda12e874b9f9cd4431f477566b72c2d2a5e414`（v0.79.6）发布 compiler golden；P64 绑定 `5422f939`（v0.80.2）golden；P66 绑定 v0.80.3/v0.80.4 共用的独立 v0.80.4 compiler golden；P67 绑定 v1.15.3 基线 `2f9de0f88cb0fdac2924309c9d81c1c0d33ec751` 独立 compiler golden，止于 `0067`。P68 绑定 v1.15.7 基线 `8473dc306c47a2f5ad939508ab697b6528889fba` 独立 compiler golden，69 sources 止于 `0068`。P69 使用 C68 golden 加固定 `0069` checksum，权限不变；P70 使用 C71 compiler 扩展前独立冻结的 C70 71-source/19-fragment goldens；P71 使用 pristine C71 PostgreSQL 独立捕获的 72-source/20-fragment goldens；P72 使用 C71 golden 加固定 `0072` checksum，权限不变；P73 使用 C72 golden 加固定 `0073` checksum，权限不变；P74 使用 C73 golden 加固定 `0074` checksum，权限不变。P75 使用独立 C74 literal 加 `0075_add_settings_deployment_overrides.sql` 记录，SHA256 固定为 `a00bd66a3687e3278c0e9444538e6ba572f2452f57982dd9b082eecc1e90911a`；独立 C75 privilege golden 与 C74 字面内容相同，不从 C76 output 动态切片。P76 使用注册 C77 fragment 前从 C76 compiler 捕获的独立 `app_acl_current_c76_migrations.v1.bin` / `app_acl_current_c76_privileges.v1.bin`（77 sources、25 fragments）；C76 privilege body 是 C75 加一个 family-state SELECT，不从 C77 output 动态截取。P77 使用注册 C78 fragment 前从 C77 compiler 捕获的独立 C77 goldens（78 sources、26 fragments，止于 `0077`）。P78 使用注册 C79 fragment 前从原 C78 compiler 捕获的独立 C78 goldens（79 sources、27 fragments，止于 `0078`），migration SHA-256 为 `af2497ecde095a8f33e847dd21fd2f863764c86ff1a141aa40fac6ae6cb34ad3`、privilege SHA-256 为 `3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c`；C78 的 C77 source prefix 与 privilege body 保持原字节。P79 使用修改 compiler/注册 retirement 之前由原 C79 compiler 冻结的 `app_acl_current_c79_migrations.v1.bin`（5832 bytes、80 sources、28 fragments，止于 `0079`，SHA-256 `3879119c952bd0a59dee938a0468ce848070985bd0b6bd55a7da26d40263c7db`）与 `app_acl_current_c79_privileges.v1.bin`（35745 bytes，SHA-256 `3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c`）。C79 保持 C78 source prefix 与 privilege body 原字节；旧 goldens 与 P62 digest 不变。P63/P64/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79 genesis 允许合法自定义角色/数据库；禁止任意 prefix、未知 chain 或修改后 current compiler 动态截取作为 predecessor oracle。
- P62/P64/P63/P66/P67/P68/P69 的 exact current suffix 分别为 `0063…0080`、`0065…0080`、`0064…0080`、`0067…0080`、`0068…0080`、`0069…0080`、`0070…0080`；P70 为 `0071…0080`，P71 为 `0072…0080`，P72 为 `0073…0080`，P73 为 `0074…0080`，P74 为 `[0075,0076,0077,0078,0079,0080]`，P75 为 `[0076,0077,0078,0079,0080]`，P76 为 `[0077,0078,0079,0080]`，P77 为 `[0078,0079,0080]`，P78 为 `[0079,0080]`，P79 仅为 `[0080]`。历史独立前驱自身的 privilege body/角色绑定不变，但所有 current successor 都执行 0080：精确移除 runtime 对 `agent_sync_batches` 的 INSERT，另对已有 0067 的 P67…P79 移除 `agent_live_signals` 的 SELECT/INSERT；更早前驱不得先新增随后退休的 live-signal grants。精确新增 runtime 对 `agent_replay_states` 的 SELECT/INSERT/UPDATE、对 `agent_batch_receipts` 的 SELECT/INSERT/DELETE，共六 tuple。除此之外保留各前驱原有的精确增量：P62…P76 增加 product 七 tuple，P62…P75 增加 family-state SELECT；P62/P64/P63 增加两个 state UPDATE、其后适用的 lifecycle/parser/0071 grants；P66 增加 lifecycle/parser/0071 grants，P67 增加 parser/0071 grants，P68/P69/P70 增加 0071 四 tuple。0072/0073/0074/0075/0078/0079 自身为空 ACL fragment；这不代表 P77/P78/P79→C80 的权限 delta 为空。transition 仅允许 0080 退休上述两个表；任何其他退休、无关权限增删、grant option 或角色绑定变化均拒绝，不能用子集判断。
- Writer 保持单个 `SERIALIZABLE` transaction、advisory/ledger lock 与整体 serialization retry：先验旧 source/manifest/ledger/catalog，再业务 preflight、pending SQL、复读 ledger、业务后置验证、current revoke-first DCL、current catalog、追加 revision/CAS head、完整复读后 commit。失败整体回滚 schema、索引、ACL、ledger、manifest/head，包含恢复 0080 已 DROP 的两个旧表及其数据；漂移不能由 DCL 自动修复。exact C80 repeat 不执行 SQL/DCL/manifest 写入。P62 保留 heartbeat 3→12/custom 不变语义；其他 profile 不重跑 `0063`。heartbeat 分类要求并只剥离一次 terminal `0080`，再依次剥 terminal `0079`、`0078`、`0077`、`0076`、`0075`、`0074`、`0073`、`0072`、`0071`、`0070` 及历史 `0069`/`0068`/`0067`；未知、缺漏、重复或错序仍拒绝。P79/P78/P77 分别仅接受 `[0080]` / `[0079,0080]` / `[0078,0079,0080]`。按 suffix 是否含 `0067` 判断 lifecycle pending，而非看末项；0067 仍明确拒绝有待转换旧 VPS/监控/服务/域名/Target/订阅业务数据的部署。P67…P79→C80 不清空业务数据、不重跑已完成转换；0078/0079 不回填旧报告或修改 settings，0080 只替换重放存储，不重建凭据、实例或业务/raw/aggregate 历史。
- `0067` 的 settings 白名单变换为：retention 删除 event/notification days，设置 raw=30、aggregate=365；IPQuality 删除 raw/history days；其余字段保持原逻辑值。不得扩大为任意 settings 修复或修改冻结历史。
- 十七种注册 suffix 都把 settings singleton 无行视为合法快照：升级保持行存在性，不能插默认行绕过缺行；无行仍完成 suffix、column default、index 与 catalog 验证。有行仍验证 heartbeat JSON/正阈值并只执行注册转换；P67…P79 的 missing/default/custom 三态都保持 settings 存在性、完整值及 `updated_at`。0075 对既有行只允许以 add-column 临时 true default 物化 `incident_sweep_runtime_managed=true`，再固定 default=false；完整行 snapshot 仅接受这一新增字段，既有字段及时间仍按已注册 heartbeat 合同保护。缺行保持缺失，新行 default=false，终态 boolean NOT NULL default false。exact C80 repeat 不写 SQL/DCL/manifest，不把显式 false 回填 true；P75…P79 不重放 0075，0080 不改变 settings。
- C75 ownership 列终态验证同样适用于 fresh genesis 发布前和 genesis exact repeat；不能只检查 registered successor。缺列、错误类型、可空或 default 非 `false` 均拒绝，fresh 失败整体回滚，repeat 保持只读且不修复漂移。独立历史 genesis oracle 按其冻结 source 构造，不反向要求尚未存在的 `0075` 列。
- current catalog 从冻结 R1 base 加 ordered fragments 及精确退休统一物化，而非只做 additions 或按新旧 privilege 长度差预分配。`public.record_platform_cas_contract_activation_projection(bytea)` 与 `public.record_platform_cas_domain_rotation_projection(bytea)` 仍是 migrator-owned、`SECURITY DEFINER`、唯一 `bytea` overload、`search_path=pg_catalog` 且显式 revoke `PUBLIC`。
- production 0052 的九张 Records core table、hardened function 与 29 个 privilege tuple 不变；0065 两个 runtime UPDATE 保留，0066 为空。历史 0067 为 monitoring_agent_sessions、receiver_health、vps_followups、asset_service_associations、asset_domain_associations、vps_maintenance_actions 注册 runtime SELECT/INSERT/UPDATE；agent_live_signals、vps_archive_requests、monitoring_instance_lifecycle_receipts、vps_maintenance_effects 只注册 SELECT/INSERT。C80 仅移除其中 agent_live_signals 对象/权限，其他生命周期表及 ownership/session 两个 invoker hardening 不变；不得授任意 DELETE、Records content 读取或 immutable history UPDATE。
- `0068` 的 `public.houfeng_parse_host_address(text)` 为 `IMMUTABLE STRICT PARALLEL SAFE SECURITY INVOKER`，function kind `f`、`search_path=pg_catalog`。只解析 host identity，不读写表、不回填数据；scoped converger 同事务 revoke-first，仅新增 runtime EXECUTE，PUBLIC/admin 不获 EXECUTE。legacy raw migration 保留 PG 默认函数执行权限，不硬编码角色名。assigned view 使用 `CREATE OR REPLACE VIEW` 保留列合同、依赖与 ACL；latest view 不重建。
- `0069` 只新增 nullable CPU validity/count 列并允许六个 CPU 日聚合 avg/max 为 NULL，无默认、无回填、不重算 finalized 日；旧 host CPU marker 和日聚合有效计数仍为 NULL，历史 CPU 数值不变。计数 CHECK 拒绝负数、子计数超过 CPU 有效数及 CPU 有效数超过 host 样本数，允许 legacy NULL。无需新增 APP object、角色或 ACL tuple；既有自定义绑定和有效权限不变，PUBLIC/admin 不获新 grant。CPU 语义见 [监控合同](../monitoring.md) 与 [保留合同](../retention.md)。
- `0070` 只为 `record_import_jobs` 增加 nullable 目标主体 kind/source ID 与成对合法值 CHECK；旧行保持 NULL，不猜测回填、不加索引。没有新增 managed object 或权限 tuple，注册显式空 fragment；既有角色与权限不变。导入旧计划的拒绝和目标主体授权见 [可移植性合同](../records/portability.md)。
- `0071` 为 `users` 增加持久化 `is_supervisor`/`disabled_at`、单 supervisor partial unique index 与 supervisor contract；空库不回填，恰好一个已有 admin 才标记 supervisor，多用户或唯一非 admin 明确回滚。为 `record_access_groups` 回填稳定 `group_id` display name、施加 trim/1–100 CHECK 和 `(project_id,display_name)` UNIQUE；不删除组、不加可能破坏旧库的 membership user FK。新增 fragment 无 managed object/function，且只增加 runtime 对 `record_access_groups` 的 INSERT/UPDATE、`record_access_group_members` 的 INSERT/DELETE 四个精确 tuple。
- `0077` 为 reports 增加 nullable `product_json` / `product_summary_json`、providers 增加 nullable `details_json`、services 增加 nullable `checks_json`，不回填历史；新表为 `ip_quality_intelligence` 与带 `blocked_until` 的 `ip_quality_source_cache`。current fragment 只把两表加入 managed surface，并仅授予上述七个 center_runtime tuple；intelligence 不授 DELETE，cache 的 DELETE 仅用于缓存清理，platform_admin、PUBLIC、Records role 均不获新 grant，旧表 ACL 不变。SQL 本身不硬编码角色或 GRANT，由 scoped convergence 同事务发布 manifest 与精确 catalog。冻结 R1/R2 source/manifest/privilege contract 不变。
- `0078` 以 `CREATE OR REPLACE VIEW` 更新 `ip_quality_vps_family_state` 的 collection metadata 判定：revision 4 仍要求双族 `[4,6]`，revision 5 接受规范 `[4]`、`[6]`、`[4,6]`，当前行地址族必须在声明集合内；既有 view shape、grants 与历史报告事实不变，不新增 managed object/function 或 ACL。
- `0079` 只创建 `idx_ip_quality_source_cache_expires_at`，定义为 `create index idx_ip_quality_source_cache_expires_at on ip_quality_source_cache(expires_at);`，是普通非部分、非 concurrent B-tree 索引。`ipQualityCacheExpiryAppACLCurrentMigrationFragment()` 的 Objects/Functions/Privileges 增量均为空；不注册新表、函数、ACL object 或角色，不授新权限，不改 cache/settings/历史业务数据。历史 C78→C79 的索引 DDL、ledger 与 C79 manifest 同事务提交；升级 current C80 时，该 DDL（若 pending）与 0080 及 C80 successor 同事务提交或整体回滚，exact repeat 不重跑索引 DDL。不得用 current-time partial index、事务外 concurrent DDL 或运维 SQL 替代。
- `0080` 的 managed catalog delta 恰为 DROP `agent_live_signals` / `agent_sync_batches`、CREATE `agent_replay_states` / `agent_batch_receipts` 与两个普通 invoker trigger functions；DROP 不带 CASCADE，未知依赖使整个 scoped transaction 回滚。state 为 `session_id text PRIMARY KEY REFERENCES monitoring_agent_sessions`、`producer_id uuid NOT NULL`，另有非负 bigint `retired_through`、`allocated_through`、`live_sequence_high`（默认 0，allocated>=retired）；receipt 为 `(session_id,batch_sequence)` 主键、session FK 指向 state、sequence>0、32-byte digest。无 received_at 列/索引、逐 signal 行或永久逐 ack 历史；只需两主键索引。runtime 仅获上述六 grants，state 无 DELETE、receipt 无 UPDATE；admin/PUBLIC/Records 无新增授权，两函数无直接 runtime/PUBLIC EXECUTE。
- `enforce_agent_replay_state()` 与 `enforce_agent_batch_receipt()` 固定 `search_path=pg_catalog`、全限定表名、kind=f、SECURITY INVOKER。state BEFORE UPDATE 禁止改 session/producer 或降低任一水位；同函数的 AFTER UPDATE OF retired_through 仅在 floor 提高时原子删除本 session seq<=floor 的 receipts，repository 不重复清理。receipt BEFORE INSERT/UPDATE/DELETE guard 在 INSERT/DELETE 路径以 FOR SHARE 读取 state，使 receipt 插入/删除与非键 floor 更新互斥；INSERT 需持久 floor<seq<=allocated 且 seq-floor<=131072，DELETE 只允许已退休 seq，UPDATE 始终拒绝。直接越界 SQL 返回 23514，runtime receipt UPDATE/state DELETE 因无权限为 42501；不能以 KEY SHARE 放过竞态或借清理权限擦除未退休收据。锁序为 graph→instance/session→replay state→receipt，死锁整笔回滚。
- 两新表 autovacuum_enabled=true、vacuum threshold=1000、scale factor=0.05，state fillfactor=80；不修改全局 Postgres 参数。每 session 一行 state、至多 131072 行未退休 receipts，正常 durable ack/floor 后仅少量在途记录；live 由独立持久高水位取代逐 signal 行。session/业务历史仍增长，dead tuples/文件高水位依赖 autovacuum 重用，逻辑有界不承诺立即缩小文件。
- 0080 切换前必须逐台停所有旧 Agent（含 evidence-only）并验证实际 buffer 为合法空数组；非空、离线、不可读均阻止升级，不能清空队列。协调备份数据库/附件/Records authority/凭据/queue 后，先升级 Center，再升级全部 Agent；迁移不导入旧随机身份，也不能自行证明跨主机排空。C80 后禁止旧协议 writer、单侧数据库或 queue 快照回滚及自动重新编号恢复；恢复须另行授权、停全部 writer 并恢复同一协调点。本合同不授权部署或恢复。
- canonical function privilege identity grammar 在既有 `public.<name>(bytea)` 之外只额外识别精确的 `public.houfeng_parse_host_address(text)`；不接受任意 text overload 或近似拼写。语法识别不授予权限：旧合同仍按独立 golden 精确比较，新 current 仍按注册 fragment 的 exact privilege set 准入；schema/column 空值、EXECUTE 与无 grant option 的原限制不变。
- parser 的新 SQL 参数声明为匿名 `text`，函数内部以 `p_raw ALIAS FOR $1` 读取，确保既有 `pg_get_function_identity_arguments` catalog 读取与 `(text)` 合同一致；不提供 `p_raw => ...` 命名调用接口。不得全局去掉已发布函数身份中的参数名，也不为 parser 绕过 owner/ACL/hardening 检查。
- admission 只验证 compiled migration-owned surface：database、managed schema、relation/view/sequence/function、ledger/manifest、role attributes/membership、owner、direct/effective/column/default ACL 和 function hardening。current convergence 的 placement、fresh-state 与 legacy-ledger companion-object preflight 均以完整 `(schema, object identity)` tuple 检查 relation/function；不同 managed schema 可声明同名对象，无关 schema 中的同名 relation、同名 function 或其他 overload 也不属于 managed tuple。冻结 R1 的历史裸名称 shadow rejection 保持不变。managed private schema 内 unknown object 仍是 drift；无关 schema/object 与 unrelated-owner default ACL 必须接受。
- PostgreSQL 16 `pgcrypto` 必须安装在 `record_platform_internal`；若 extension 已在其他 schema 则 fail closed。extension-member procedure 按 OID 识别，并对普通 managed owner/direct/effective/function reader 保持 opaque，因为受限 migrator 不能可靠改写 bootstrap-owned member ACL。opacity 绝不产生 reachability：`PUBLIC`、runtime、admin 对 `record_platform_internal` 都没有 `USAGE` 或 `CREATE`；同一 admission snapshot 还会拒绝同时具有 schema `USAGE` 与 function `EXECUTE` 的 reachable opaque member。migrator-owned helper/projector 仍必须显式 revoke `PUBLIC`。
- `AdmitAppACLCurrentRuntime` 精确开启一个 `REPEATABLE READ READ ONLY` transaction。先识别并验证完整注册链；P62/P64/P63/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79 旧终点（包括 C69…C79 genesis）返回需要 successor convergence 的 typed 拒绝，仅 C80 终点比较 current privileges 并验证 catalog。不执行 DDL/DCL、不调用 writer、不读 migrator 凭据；升级时停止旧 Center/processor 与全部 Agent writer，失败关闭 pool，不得 owner-migrate 或 warning-only fallback。

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
| 注册的 P62/P64/P63/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79 起点或 chain，满足业务前置条件 | 旧 catalog 先验，exact suffix 升级、事务 DCL、追加 C80 revision/head CAS；失败整体回滚。runtime 只拒绝旧终点，不自行升级。 |
| suffix 含 `0067` 且存在需转换的旧 VPS/监控/服务/域名/Target/订阅数据 | `0067` 因 `requires a fresh installation` 明确拒绝；不猜测状态、不删数据、不改变原 ledger/manifest/catalog。 |
| P67 genesis 或原 11 chains 追加 P67，含既有业务记录 | 只执行 `0068…0080`，保留业务/settings/manifest；权限为既有 parser/access-management/family-state/product 增量加精确 replay delta，runtime C80 后准入。 |
| P68 genesis 或原 23 chains 追加 P68，含旧 host/finalized 日记录 | 只执行 `0069…0080`；保留 CPU 数值、NULL legacy marker/count、settings/history/ledger 前缀，revision+1。0071 四 tuple、0076 SELECT、0077 七 tuple之外，仅允许 0080 的三旧 grants 移除/六新 grants 增加；失败整体回滚。 |
| P69 genesis 或原 47 chains 追加 P69，含 Records 导入 job | 只执行 `0070…0080`，旧主体列 NULL；权限为 0071 四 tuple、family-state SELECT、product 七 tuple 加精确 replay delta；保留业务/settings/history，runtime reject-before/admit-after。 |
| P70 genesis 或 C70 chain exact successor | 独立 C70 goldens；只应用 `0071…0080`，仅增加原 access-management/family-state/product 增量与六 replay grants，并移除三旧 replay grants；admission/repeat 无 durable drift。 |
| P71/P72/P73/P74/P75 genesis 及 chain exact successor | 分别应用 `0072…0080`、`0073…0080`、`0074…0080`、`0075…0080`、`0076…0080`；原 family-state SELECT/product 七 tuple 加精确 replay delta。0073 metadata 回填、0074 旧实时汇率、0075 ownership/default/snapshot 合同不变；0076…0080 不改旧报告事实。额外 view UPDATE、admin/PUBLIC SELECT 或 grant option 均拒绝且不修复。 |
| P76 genesis 或 C76 chain exact successor | 独立 C76 goldens；仅应用 `[0077,0078,0079,0080]`，product 七 grants 加精确 replay delta；保留报告/children/settings/非退休旧表权限及历史 manifest，新历史列 NULL，无合成旧报告 job/cache。额外 intelligence DELETE、admin/PUBLIC/Records 权限或 grant option 仍拒绝。 |
| P77 genesis 或 C77 chain exact successor | 独立 C77 goldens（78 sources）；仅应用 `[0078,0079,0080]`，view shape/业务/settings/cache/history 保持。0078/0079 自身 zero-ACL，但 current successor 必须精确移除三旧 replay grants、增加六新 grants；runtime reject-before/admit-after、repeat 深相等。 |
| P78 genesis 或 C78 chain exact successor | 独立原 C78 goldens（79 sources）；仅应用 `[0079,0080]`，验证实际 B-tree index definition 及精确 replay delta，旧业务/settings/cache/manifest 不变。DDL/DCL/manifest 失败回滚新索引/新表/ledger/manifest，并恢复旧 replay 表；runtime reject-before/admit-after。 |
| P79 genesis 或独立 C79 history exact successor | 独立原 C79 goldens（80 sources）；仅应用 `[0080]`。恰好两个旧表移除、两个新表和两个 invoker funcs增加；三旧 replay grants 移除、六新 grants增加，其他 ACL/owner/function/业务/raw/aggregate/在线/session/settings/history 不变，不导入随机重放身份。repeat 深相等。 |
| 0080 缺漏、重复或非 terminal；剥除它后该 profile 应有的 0079/0078 缺漏、重复、错序；P77/P78/P79 suffix 非 exact | heartbeat/suffix 分类拒绝；历史 zero-ACL fragment 不放宽注册顺序、角色绑定、权限增删或退休白名单。 |
| RetiredObjects 不存在/重复、同 fragment 重新声明、悬空权限，或注册 transition 退休非指定对象 | source compile / transition delta 在事务前拒绝；不能任意删除其他 managed object 或降低权限合同。 |
| 0080 DROP 遇未知依赖，或 DDL/DCL/manifest seam 失败 | 单事务回滚；旧 replay 表/记录、ACL、ledger、manifest/head 与业务事实全部恢复，runtime 仍拒绝旧终点。 |
| runtime 降低任一 replay 水位、改变 producer/session、插入退休/超窗口 receipt、删除未退休 receipt | SQLSTATE 23514；state DELETE/receipt UPDATE 因无权限为 42501。floor 合法推进与 receipt 清理原子提交，双连接不能留已退休 receipt。 |
| malformed manifest chain、exact-source catalog/owner/ACL/function drift | 返回具体 fail-closed corruption/catalog error；不得误标为 rebuild-required。 |
| 任一 role 不是不同的直接 constrained `LOGIN NOINHERIT` role，具有 direct/recursive membership，或 `session_user != current_user` | 在 scoped migration/admission 前 fail closed。`SET ROLE` runtime snapshot 精确拒绝为 `session user %q does not match current user %q`。 |
| current compiler output 与 persisted privileges 不同，或 runtime/admin 取得未编译 privilege | catalog/manifest drift，拒绝。runtime/admin 对 base projector 的 direct call 返回 SQLSTATE `42501`。 |
| managed object/grant/column ACL/default ACL/owner 或 projector definition drift | 拒绝；projector 必须 owner-only、唯一 `bytea`、`SECURITY DEFINER`、显式 revoke `PUBLIC`，并使用 `search_path=pg_catalog`。 |
| 不同 compiled managed schema 声明同名 relation/function tuple | 独立编译并按 exact tuple 检查；fresh/legacy preflight 不得因裸名称冲突拒绝。 |
| unrelated schema 中存在 managed relation/function 的同名对象或其他 overload，或存在 unrelated-owner default ACL | 只要完整 tuple 不能影响 compiled managed surface 就接受；冻结 R1 regression 继续保留历史 shadow rejection。 |
| `pgcrypto` 位于 `record_platform_internal` 之外 | fail closed（migration/convergence precondition 是 SQLSTATE `55000`）。 |
| runtime/admin/PUBLIC 取得 `record_platform_internal` 的 `USAGE` 或 `CREATE`，或 opaque extension member 变为 reachable | 拒绝 catalog admission。runtime 对 `record_platform_internal.digest` 的 direct call 返回 SQLSTATE `42501`。 |
| scoped migrator 收到 serialization failure | rollback 后重试整个 `SERIALIZABLE` closure；任一不可重试错误都不留下 partial ledger、ACL、revision 或 head state。 |
| `BeginTx` 异常返回 `(nil, nil)` | 返回 defensive error；禁止注册 nil transaction rollback 导致 panic。 |

### 5. Good / Base / Bad Cases

- Good：两个 flag 都关闭时保留 legacy migration；records-on/delete-off 时 direct migrator fresh converge exact current，direct runtime 在 repository 打开前通过 current one-snapshot admission。
- Base：当前 embedded set 是冻结 52-source R1 prefix 加 `0052…0080` 共 29 个 exact fragments；fresh convergence 发布 C80 revision-1 genesis，exact repeat/direct runtime admission 均不改 durable state。
- Good：十七个独立 profile 验证后沿 98303 个起点/链升级至 C80；历史 C79 49151-chain/C78 24575-chain/C77 12287-chain/C76 6143-chain/C75 3071-chain/C74 1535-chain 基线保留。全部旧 suffix 末尾追加 0080，P77/P78/P79 pending 分别为 `[0078,0079,0080]` / `[0079,0080]` / `[0080]`。历史 C75→C76 单 view SELECT、C76→C77 七 tuple、C77→C78 与 C78→C79 空权限差量分别验证，当前升级另独立固定精确 replay retirement/additions，不能把历史空 delta 当 current 空 delta。
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

- 断言 fresh/direct runtime、全部 predecessor genesis/chains、repeat、自定义绑定与历史 manifest 字节保持；P66 genesis 及 `[P62,P63,P66]` 的历史 C74 基线不变。C80 全部 98303 个 predecessor chains/successor 分类由 `TestClassifyAppACLCurrentManifestShapeRegisteredChains` 独立枚举，不从生产 accepted-chain 表派生；C79 49151-chain/C78 24575-chain/C77 12287-chain/C76 6143-chain/C75 3071-chain/C74 1535-chain oracle 保留。C69…C79 终点仅追加各自 exact suffix，验证 reject-before/admit-after、history/ledger 前缀与 repeat。既有 heartbeat、0067 白名单/旧业务 fail-closed、索引/约束/ownership、nullable CPU/count、0072…0075 settings/zero-ACL、0076 SELECT、0077 product 七 tuple、0078/0079 本身 zero-ACL 合同不变；所有 current suffix 另验证 0080 精确退休/六新 grants，P77/P78/P79 的历史 privilege body 保持不等于 current privilege body 不变。
  生命周期与 retention 集成用当前 `ConvergeAppACLCurrent` 和 `AdmitAppACLCurrentRuntime`，不能以冻结 R1 fixture 代替。覆盖原子归档、重复退役拒绝、关联历史、健康观察、可信在线、保留边界及 runtime 越权拒绝。
- `TestPostgresIntegrationAppACLCurrent` umbrella 保留原 P67、P68、settings、业务、repeat、parser/PUBLIC revoke、drift/rollback 覆盖，并包含 `agent_replay_migration`。`scripts/test-pg16-catalog.sh postgres:16.0`、`:16.6`、`:16.12` 分别经 `pg16-catalog` strict lane 要求精确三个顶级 anchors `TestPostgresIntegrationAppACLR2`、`TestPostgresIntegrationAppACLCurrent`、`TestPostgresIntegrationAgentReplayMigration` 的 RUN/PASS，任意 skip/fail 或缺失事件都失败。独立 product/collection-scope/cache-expiry suites 仍须按上面的完整 selector 运行，不能以 replay 专项替代 current umbrella、历史迁移或 R2 准入门禁。
- umbrella 内 `p68_upgrade` 独立保留 24 条 C68-ended 历史（P68-only 与原 23 chains 追加 P68），P62 开头固定角色，其他自定义绑定。验证 reject-before/admit-after、revision+1、digest 链、history/ledger 前缀、0069 一项与 repeat，旧 host/finalized 日不回填、CPU nullable/count CHECK。current ledger 终点为 0080；P70 验证 `0071…0080`，P71/P72/P73 分别验证 `0072…0080` / `0073…0080` / `0074…0080`。既有 0071/family-state/product 差量之外，显式验证并投影精确 C80 replay delta 后再复用旧 catalog 断言，不隐藏其他 ACL/owner/function 漂移。真实 applyPending/applyDCL 后故障证明 schema/constraint/CPU facts 回滚，再 retry/repeat；P73 实时/月末汇率共存/唯一/CHECK 仍保留。
- settings 回归保留 P67 三态及早期前驱缺行升级；P74 三态仍验证 0075 既有行仅增 ownership=true、完整 JSON/时间保护、缺行保持缺失、新行 default=false、显式 false repeat 不变，以及 boolean NOT NULL default false。保留 post-apply tamper、列结构 corruption、rollback、runtime 实际操作/DDL 拒绝、历史 linkage 与双连接初始化，不补插 settings 或放宽验证。0075 自身 zero-ACL；current suffix 在 family-state/product 增量外还执行精确 replay delta，0080 不改 settings。历史 C75→C76→C77 ledger/history/revision/repeat 仍受保护。
- `TestPostgresIntegrationIPQualityProductMigration` 保留真实历史 C75→C76→C77 七 tuple 证据及独立 C76 三种 settings 状态；current 升级追加至 C80，并分别验证精确 replay delta 与产品七 grants。保留 product DCL 后失败回滚、五类额外权限拒绝、runtime intelligence SELECT/INSERT/UPDATE 与 cache SELECT/INSERT/UPDATE/DELETE、intelligence DELETE=42501。旧报告/来源/服务、非退休 ACL/owners/default/functions/history 保持，新历史字段 NULL、无合成旧报告 job/cache，repeat 深相等。
- `TestPostgresIntegrationIPQualitySyncMetadataRoundTrip/0076_preserves_C75_reports_and_children` 继续用 test-only C75 SQL 列清单构造真实前驱，先独立应用 0076 逐列比对，再应用 current 检查新字段 NULL 与族读模型；不调用当前 writer 或增加 backward shim。C76 golden、product 七 tuple 与 suffix 分类回归仍保留；current terminal 分类先剥 0080 再剥 0079/0078，缺漏/重复/错序拒绝。
- `TestPostgresIntegrationIPQualityCollectionScopeMigration` 保留独立 C77 genesis/历史 chain 与 revision 4/5、view shape、facts/settings/cache/history 证据；current 升级至 C80 时额外单独验证 replay delta。cache-expiry 专项从独立 C78 genesis/历史 chain 升级 current C80，验证实际 B-tree 定义、repeat、DDL/manifest 故障回滚、admission/ACL drift；C78→C79 的冻结 source/空权限事实不变，C78 oracle 仍来自注册 0079 前的原 compiler，不用 current 截断替代。
- `TestPostgresIntegrationAgentReplayMigration` 覆盖 fresh C80、独立 C79 genesis 与 C76→C77→C78→C79 history、exact C80 两 drop/两 table/两 invoker delta；保存 session/凭据/trusted-online/业务/raw/aggregate/settings/history 全行，runtime 真实 INSERT/SELECT/UPDATE/retirement cleanup 及无权限/23514 拒绝，reject-before/admit-after、repeat、无关权限增删/伪 retirement、DDL/DCL/manifest 故障与 unknown DROP dependency 回滚。compiler 回归固定 C79 golden 长度/hash、clone、invalid retirement 与 terminal0080。业务 strict gate 保留旧 anchors 并增加 AgentReplayWindow、RetentionConcurrentSync、RetentionPreservesHistoryAndFinalizedBuckets；双连接 receipt guard/floor 互斥必须以真实 PG 阻塞/提交结果验证，不能以源码字符串断言替代。
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

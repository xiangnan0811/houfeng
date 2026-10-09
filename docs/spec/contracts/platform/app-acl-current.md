# APP 当前迁移与准入

## Scenario: APP current-development scoped migrator and one-snapshot runtime admission

### 1. Scope / Trigger

- 触发：修改 `HOUFENG_RECORDS_ENABLED` / `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` 模式选择、`houfeng-record-platform-admin migrate --scope app`、root migration、current APP fragment/compiler、manifest/catalog verifier、`ConvergeAppACLCurrent`、`AdmitAppACLCurrentRuntime`、center bootstrap、VPS importer，或其 PostgreSQL regression 时。
- current contract 的前 52 个 source 必须 byte-for-byte 等于冻结 `0001…0051` r1 inventory（包含两个按文件名字典序排列的 `0004_*`）。后来 embedded migration 必须注册 exact `AppACLCurrentMigrationFragment`，无 APP object 时注册 explicit empty fragment。当前有 73 个 source，止于 `0072_add_target_observation_freshness.sql`；post-R1 共 21 个 fragments。`0063`、`0064`、`0066`、`0069`、`0070`、`0072` 为 empty fragment；`0071` 注册 access-management schema changes 的 exact runtime table grants；`0065` 的两个 runtime 表级 UPDATE 保留；`0067` 注册生命周期新表、最小权限与 trigger hardening；`0068` 注册 host-address parser 的 runtime EXECUTE 与 invoker hardening。新增 SQL 与 fragment 只满足 source/catalog 覆盖；支持已发布数据库升级还必须更新现行 exact suffix 并明确注册 predecessor 及其独立 golden。
- 两个 record flag 都关闭时保留 legacy owner `migrate.Apply`。`records-on/delete-off` 必须先运行 current scoped migrator，随后 center/importer 只能以 runtime 身份执行 current admission。`false/true` 和 `true/true` 在读取 URL、`_FILE` secret、DNS、数据库、输入文件或外部域配置前失败。
- `ConvergeAppACLR1`、`AdmitAppACLRuntime` 与 isolated APP R2 bootstrap/finalize/runtime API 是冻结历史合同；保留其导出签名和 regression，但 product migration/startup 不再默认调用它们。
- frozen `AdmitAppACLRuntime` 必须在开启 transaction 前通过 `snapshotAppACLR1MigrationSources(migrations.FS)` 取得并验证 exact R1 prefix，再把已 canonicalize 的 frozen set 交给 manifest verifier。它不得把 R1 manifest/ledger 与会随 `0052+` 增长的完整 `CanonicalMigrationSetFromFS(migrations.FS)` 比较；后者会让新增 current migration 反向破坏冻结 R1 admission。

### 2. Signatures

- 模式入口：`config.LoadRecordPlatformMode() (config.RecordPlatformMode, error)`，只返回 `RecordPlatformModeLegacy` 或 `RecordPlatformModeRuntimeAdmission`。
- Writer：`ConvergeAppACLCurrent(ctx context.Context, db *pgxpool.Pool, runtimeRole, adminRole string) (AppACLManifestPersistedV1, error)`；`houfeng-record-platform-admin migrate --scope app` 是 records-on 的唯一 APP schema/ACL writer。
- Runtime gate：`AdmitAppACLCurrentRuntime(ctx context.Context, db *pgxpool.Pool) error`；center/importer 在构造任何 repository 前调用。
- Extension contract：`AppACLCurrentMigrationFragment{Migration, Objects, Privileges, Functions}`；fragment registry 与 `migrations.FS` 在 transaction 前 closed-world compile，later migration 与 fragment 必须一一对应。
- Typed cause：`migrate.ErrDevelopmentDatabaseRebuildRequired`。admin CLI 只允许该 safe sentinel 穿过 redaction boundary；任意数据库 error、raw SQL、role password 或 DSN 仍统一屏蔽。
- 持久化合同：`AppACLManifestPersistedV1.MigratorCatalogRole` 不可变且 digest-bound；runtime 从 persisted binding 构造 current catalog verifier，不读取 migrator credential/configuration。

### 3. Contracts

- `center_runtime`、`platform_admin` 与 migrator 是三个预创建、两两不同、直接认证的 `LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS` role。三者的直接与递归 membership 均为空；migration 与 runtime admission 都证明 `session_user == current_user`。`SET ROLE`、复用 owner、role membership、default ACL 或共享 login 都不能满足该合同。
- source/fragment compiler 必须在 `BeginTx` 前拒绝 missing/extra/duplicate fragment、duplicate object/privilege、unknown subject、unmanaged privilege/function hardening，以及新 function 缺少 exact hardening。每个 fragment 的 `Privileges(databaseName)` callback 只在 source compile 时用固定验证数据库占位符求值一次；结果、fragment input 和 nested function config 都必须 defensive-copy。后续 catalog compile 只能复制已物化 privilege template，并替换 `database` tuple 的占位符，不能再次调用 callback。
- current convergence 只接受 fresh C72 genesis、383 个明确注册的 predecessor profile chain，以及 exact C72 target repeat。保留原 47 条 P62/P63/P64/P66/P67/P68 历史：原 11 个 `[P62]`、`[P63]`、`[P64]`、`[P66]`、`[P62,P63]`、`[P62,P64]`、`[P62,P66]`、`[P64,P66]`、`[P63,P66]`、`[P62,P64,P66]`、`[P62,P63,P66]`，加 `[P67]` 与这 11 条追加 P67 的版本，再加 `[P68]` 与前述 23 条追加 P68 的版本。C69 predecessor 包括独立 `[P69]` genesis 和这 47 条分别追加 P69 的历史；C70 predecessor 再包括独立 `[P70]` genesis 和上述 95 条分别追加 P70 的历史；C71 predecessor 再包括独立 `[P71]` genesis 和上述 191 条分别追加 P71 的历史；不能生成任意子集闭包。升级只追加一个 C72 successor（revision 为原链长度 + 1），每项历史 manifest 保持原字节、按原 profile/绑定验证，不以 persisted 权限作为授权来源。
- 九个 profile 的 registry 顺序为 P62/P64/P63/P66/P67/P68/P69/P70/P71，既有 ID 不变。P62 绑定独立 v0.79.4 golden；P63 绑定 `2cda12e874b9f9cd4431f477566b72c2d2a5e414`（v0.79.6）发布 compiler 导出的 golden；P64 绑定 `5422f939`（v0.80.2）golden；P66 绑定 v0.80.3/v0.80.4 共用、由 v0.80.4 compiler 独立导出的 golden。P67 绑定 v1.15.3 基线 `2f9de0f88cb0fdac2924309c9d81c1c0d33ec751` 独立 compiler 导出的 source/privilege golden，止于 `0067`。P68 绑定 v1.15.7 基线 `8473dc306c47a2f5ad939508ab697b6528889fba` 独立归档 compiler 导出的 source/privilege golden，69 个 source 止于 `0068`。P69 使用该独立 C68 source/privilege 基线加固定 `0069` source/checksum，权限不变；P70 使用在当前 compiler 扩展前独立冻结的 C70 71-source/19-fragment source 与 privilege goldens，不能从 C71 output 动态截取 oracle；P71 使用从 pristine C71 PostgreSQL 独立捕获的 72-source/20-fragment source 与 privilege goldens，不能从 C72 registry 动态截取 oracle。旧 goldens 和 P62 digest 不变。P63/P64/P66/P67/P68/P69/P70/P71 genesis 允许合法的自定义角色和数据库。禁止任意 prefix、未知 chain 或动态截取 current output 作为 predecessor oracle。
- P62/P64/P63/P66/P67/P68/P69 的 exact suffix 分别为 `0063…0071,0072`、`0064…0071,0072`、`0065…0071,0072`、`0067…0071,0072`、`0068…0071,0072`、`0069,0070,0071,0072`、`0070,0071,0072`；P70 为 `0071,0072`；P71 仅 `0072`。精确权限 delta：P62/P64/P63 为两个 state UPDATE + lifecycle fragment 权限 + parser EXECUTE + 0071 的四个 access-management table grant；P66 为 lifecycle + parser EXECUTE + 四个 0071 grant；P67 为 parser EXECUTE + 四个 0071 grant；P68/P69/P70 在其独立 predecessor body 上各新增且仅新增 0071 的四个 tuple；P71 的 freshness fragment 不新增 ACL tuple。额外 grant、删除 grant 或角色绑定变化均拒绝，不得放宽为子集判断。
- Writer 保持单个 `SERIALIZABLE` transaction、advisory/ledger lock 与整体 serialization retry：先验旧 source/manifest/ledger/catalog，再业务 preflight、pending SQL、复读 ledger、业务后置验证、current revoke-first DCL、current catalog、追加 revision/CAS head、完整复读后 commit。失败整体回滚 schema、ACL、ledger、manifest/head；漂移不能由 DCL 自动修复。target exact repeat 不执行 SQL/DCL/manifest 写入。P62 保留 heartbeat 3→12/custom 不变语义；其余 profile 不重跑 `0063`。按 suffix 是否包含 `0067` 判断 lifecycle pending，不依赖末项；heartbeat 分类先剥离 terminal `0072` freshness、再剥离 terminal `0071` access-management、再识别 `0070` target subject、历史 `0069`/`0068`/`0067`，仍拒绝空、未知、缺漏、重复或错序 suffix。`0067` 仍只接受没有旧 VPS/监控/服务/域名/Target/订阅业务数据的部署；P67/P68/P69/P70/P71→C72 不要求清空业务数据，不执行 heartbeat/lifecycle/settings 转换；P69 执行目标主体列追加及 access-management schema/ACL，P70 只执行 access-management schema/ACL，P71 只执行 freshness schema 变更。
- `0067` 的 settings 白名单变换为：retention 删除 event/notification days，设置 raw=30、aggregate=365；IPQuality 删除 raw/history days；其余字段保持原逻辑值。不得扩大为任意 settings 修复或修改冻结历史。
- 九种注册 suffix 都把 settings singleton 无行视为合法快照：升级必须保持行存在性，不能插入默认行绕过缺行。无行仍须完成 suffix、column default、index 和 catalog 验证；有行仍验证 heartbeat JSON 与正阈值并只执行已注册转换。P67/P68/P69/P70/P71 的 missing/default/custom 三态均保持 settings 存在性、完整值和 `updated_at`；最终验证与 successor repeat 采用同一规则。
- current catalog 以冻结 r1 base（当前为 **208** ACL tuple）加 ordered fragment object/privilege/function hardening 编译。`public.record_platform_cas_contract_activation_projection(bytea)` 与 `public.record_platform_cas_domain_rotation_projection(bytea)` 仍是 migrator-owned、`SECURITY DEFINER`、唯一 `bytea` overload、`search_path=pg_catalog` 且显式 revoke `PUBLIC`。
- production `0052` fragment 的九张 Records core table、hardened function 与 29 个精确 privilege tuple 保持不变；`0065` 的两个 runtime 表级 UPDATE 保留，`0066` 为空。`0067` 为 monitoring_agent_sessions、receiver_health、vps_followups、asset_service_associations、asset_domain_associations、vps_maintenance_actions 注册 runtime SELECT/INSERT/UPDATE；agent_live_signals、vps_archive_requests、monitoring_instance_lifecycle_receipts、vps_maintenance_effects 只注册 SELECT/INSERT。两个 ownership/session invoker trigger 注册 exact hardening，不给 runtime 直接 EXECUTE。不能授予任意 DELETE、Records content 读取或 immutable history UPDATE。
- `0068` 的 `public.houfeng_parse_host_address(text)` 为 `IMMUTABLE STRICT PARALLEL SAFE SECURITY INVOKER`，function kind `f`、`search_path=pg_catalog`。只解析 host identity，不读写表、不回填数据；scoped converger 同事务 revoke-first，仅新增 runtime EXECUTE，PUBLIC/admin 不获 EXECUTE。legacy raw migration 保留 PG 默认函数执行权限，不硬编码角色名。assigned view 使用 `CREATE OR REPLACE VIEW` 保留列合同、依赖与 ACL；latest view 不重建。
- `0069` 只新增 nullable CPU validity/count 列并允许六个 CPU 日聚合 avg/max 为 NULL，无默认、无回填、不重算 finalized 日；旧 host CPU marker 和日聚合有效计数仍为 NULL，历史 CPU 数值不变。计数 CHECK 拒绝负数、子计数超过 CPU 有效数及 CPU 有效数超过 host 样本数，允许 legacy NULL。无需新增 APP object、角色或 ACL tuple；既有自定义绑定和有效权限不变，PUBLIC/admin 不获新 grant。CPU 语义见 [监控合同](../monitoring.md) 与 [保留合同](../retention.md)。
- `0070` 只为 `record_import_jobs` 增加 nullable 目标主体 kind/source ID 与成对合法值 CHECK；旧行保持 NULL，不猜测回填、不加索引。没有新增 managed object 或权限 tuple，注册显式空 fragment；既有角色与权限不变。导入旧计划的拒绝和目标主体授权见 [可移植性合同](../records/portability.md)。
- `0071` 为 `users` 增加持久化 `is_supervisor`/`disabled_at`、单 supervisor partial unique index 与 supervisor contract；空库不回填，恰好一个已有 admin 才标记 supervisor，多用户或唯一非 admin 明确回滚。为 `record_access_groups` 回填稳定 `group_id` display name、施加 trim/1–100 CHECK 和 `(project_id,display_name)` UNIQUE；不删除组、不加可能破坏旧库的 membership user FK。新增 fragment 无 managed object/function，且只增加 runtime 对 `record_access_groups` 的 INSERT/UPDATE、`record_access_group_members` 的 INSERT/DELETE 四个精确 tuple。
- canonical function privilege identity grammar 在既有 `public.<name>(bytea)` 之外只额外识别精确的 `public.houfeng_parse_host_address(text)`；不接受任意 text overload 或近似拼写。语法识别不授予权限：旧合同仍按独立 golden 精确比较，新 current 仍按注册 fragment 的 exact privilege set 准入；schema/column 空值、EXECUTE 与无 grant option 的原限制不变。
- parser 的新 SQL 参数声明为匿名 `text`，函数内部以 `p_raw ALIAS FOR $1` 读取，确保既有 `pg_get_function_identity_arguments` catalog 读取与 `(text)` 合同一致；不提供 `p_raw => ...` 命名调用接口。不得全局去掉已发布函数身份中的参数名，也不为 parser 绕过 owner/ACL/hardening 检查。
- admission 只验证 compiled migration-owned surface：database、managed schema、relation/view/sequence/function、ledger/manifest、role attributes/membership、owner、direct/effective/column/default ACL 和 function hardening。current convergence 的 placement、fresh-state 与 legacy-ledger companion-object preflight 均以完整 `(schema, object identity)` tuple 检查 relation/function；不同 managed schema 可声明同名对象，无关 schema 中的同名 relation、同名 function 或其他 overload 也不属于 managed tuple。冻结 R1 的历史裸名称 shadow rejection 保持不变。managed private schema 内 unknown object 仍是 drift；无关 schema/object 与 unrelated-owner default ACL 必须接受。
- PostgreSQL 16 `pgcrypto` 必须安装在 `record_platform_internal`；若 extension 已在其他 schema 则 fail closed。extension-member procedure 按 OID 识别，并对普通 managed owner/direct/effective/function reader 保持 opaque，因为受限 migrator 不能可靠改写 bootstrap-owned member ACL。opacity 绝不产生 reachability：`PUBLIC`、runtime、admin 对 `record_platform_internal` 都没有 `USAGE` 或 `CREATE`；同一 admission snapshot 还会拒绝同时具有 schema `USAGE` 与 function `EXECUTE` 的 reachable opaque member。migrator-owned helper/projector 仍必须显式 revoke `PUBLIC`。
- `AdmitAppACLCurrentRuntime` 精确开启一个 `REPEATABLE READ READ ONLY` transaction。先识别并验证完整已注册链；旧 P62/P63/P64/P66/P67/P68/P69/P70/P71 终点（包括 C69/C70/C71 genesis）返回需要 successor convergence 的 typed 拒绝，仅 C72 终点再比较 current privileges 并验证 catalog。它不执行 DDL/DCL、不调用 writer、不读取 migrator 凭据；失败时 center/importer 关闭 pool，不得回退到 owner migration 或 warning-only dry-run。

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
| 注册的 P62/P63/P64/P66 起点或已注册 chain，且无旧业务数据 | 旧 catalog 先验，按 exact suffix 升级、事务 DCL、追加 C72 revision 与 head CAS；失败整体回滚。runtime 只拒绝，不自行升级。 |
| suffix 含 `0067` 且存在需转换的旧 VPS/监控/服务/域名/Target/订阅数据 | `0067` 因 `requires a fresh installation` 明确拒绝；不猜测状态、不删数据、不改变原 ledger/manifest/catalog。 |
| P67 genesis 或原 11 chains 追加 P67，包含既有业务记录 | 只执行 `0068…0072`，保留业务/settings/历史 manifest；runtime 升级前拒绝、C72 升级后准入。 |
| P68 genesis 或原 23 chains 追加 P68，包含旧 host/finalized 日记录 | 只执行 `0069,0070,0071,0072`，新增且仅新增 0071 四个 access-management ACL tuple；保留历史 CPU 数值、NULL legacy marker/count、settings 和 manifest/ledger 前缀，revision + 1；失败 schema/data/ACL 全部回滚。 |
| P69 genesis 或原 47 chains 追加 P69，包含既有 Records 导入 job | 只执行 `0070,0071,0072`，旧主体列均 NULL；新增且仅新增 0071 四个 access-management ACL tuple；保留业务、settings 与历史 manifest/ledger，revision + 1；runtime 升级前拒绝，升级后准入。 |
| P70 genesis 或 C70 chain exact successor | C70 source/privilege goldens 在 C71 compiler 编辑前独立冻结；convergence 只应用 `0071,0072`，仅新增 access-management runtime table 四个 tuple，0072 不新增 ACL；runtime admission 与 repeat 均成功且无 durable drift。 |
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
- Base：当前 embedded set 是冻结 52-source r1 prefix 加 `0052…0072` 共 21 个 exact fragments；fresh convergence 写入 current C72 revision-1 genesis，exact repeat 和 direct runtime admission 均不改 durable state。
- Good：独立 P62/P63/P64/P66/P67/P68/P69/P70/P71 profile 验证后沿 383 个明确起点/链升级至 C72；P69 pending 为 `0070,0071,0072`，P70 pending 为 `0071,0072`，P71 pending 仅为 `0072`，含业务数据也保留并升级；旧 catalog、迁移与新 catalog 全部成功才发布 successor，repeat 只读。
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

### 6. Tests Required

- Current compiler/convergence/runtime 与 caller selector：

  ```bash
  go test ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin \
    ./cmd/houfeng-center ./cmd/houfeng-import-vps-json -count=1
  ```

  必须覆盖 missing/registered fragment、transition missing/duplicate/unknown/out-of-order/overlap/privilege drift、独立v0.79.4 golden逐entry/逐byte匹配、privilege callback单次求值与materialized slice defensive copy、跨managed schema同名tuple、unrelated ledger companion、future managed private schema、fresh/exact/registered predecessor/successor transaction cutpoint、serialization retry、count/name/checksum mismatch、null head、unknown successor、SET ROLE、catalog drift、nil transaction、admin safe sentinel、legacy `Apply`保留和三个current默认binding。每个transaction前 mismatch必须断言 `BeginTx=0`；每个transaction内cutpoint必须断言rollback且后续seam未调用。
- Real PostgreSQL current suite 必须经 strict wrapper 运行；locally skipped test 不构成 evidence：

  ```bash
  scripts/test-record-platform-integration.sh postgres -- \
    go test ./internal/center/store/migrate \
    -run '^TestPostgresIntegrationAppACLCurrent' -count=1
  ```

  完成下列聚焦 package 检查：

  ```bash
  go test ./internal/center/store/migrate -count=1
  go vet ./internal/center/store/migrate
  ```

- 断言 fresh + direct runtime、已声明 predecessor genesis/chains、target repeat、自定义数据库/角色与历史 manifest 字节不变；P66 genesis 升级为 C72 revision 2 并链接原 P66 digest。真实发布链 `[P62,P63,P66]` 从 P66 revision 3 升至 C72 revision 4。全部 383 个 predecessor chains 与 C72 successor 分类由 `TestClassifyAppACLCurrentManifestShapeRegisteredChains` 以独立显式枚举的 manifest 历史覆盖，不从生产 accepted-chain 表派生。C69 genesis 和已注册的 C69-ended 历史须只追加 `0070,0071,0072`，C70 genesis 和已注册的 C70-ended 历史须只追加 `0071,0072`，C71 genesis 和已注册的 C71-ended 历史须只追加 `0072`，验证 runtime reject-before/admit-after、历史前缀保持与 exact repeat 深相等。P62 heartbeat 默认变换与 custom/override 保留，涉及 `0067` 的 profile 仅允许已声明 retention/IPQuality settings 变换，P67/P68/P69/P70/P71 不变换 settings。含旧业务数据的测试独立断言 `0067` 因 requires a fresh installation 拒绝且零业务删除。保留 0063 exact index、0065/0066 历史约束、0067 所有权与 0069 nullable CPU/count 合同，…
  生命周期与 retention 集成用当前 `ConvergeAppACLCurrent` 和 `AdmitAppACLCurrentRuntime`，不能以冻结 R1 fixture 代替。覆盖原子归档、重复退役拒绝、关联历史、健康观察、可信在线、保留边界及 runtime 越权拒绝。
- `TestPostgresIntegrationAppACLCurrent` umbrella 必须包含 P67 升级子场景：C67 genesis 与原 11 种链历史、已有业务记录、repeat、parser runtime EXECUTE/PUBLIC revoked、权限漂移拒绝、失败 rollback。PG16 16.0/16.6/16.12 的既有 CI selector `^(TestPostgresIntegrationAppACLR2|TestPostgresIntegrationAppACLCurrent)$` 必须实际运行这些场景，任何 skip 不算通过。原 predecessor 及 manifest/golden/role 漂移拒绝仍保留。
- `TestPostgresIntegrationAppACLCurrent` umbrella 内 `p68_upgrade` 必须独立列出 24 条 C68-ended 历史（P68-only 与原 23 链追加 P68）；P62 开头使用固定角色，其余使用自定义角色。验证 reject-before/admit-after、revision + 1、digest 链接、历史 manifest/ledger 前缀不变、0069 ledger 恰好一项、repeat 深相等。P68-only 另覆盖旧 host/finalized 日不回填、六个 CPU avg/max 可 NULL、计数 CHECK 与零 ACL 增量。真实 `applyPending` 完成及原 `applyDCL` 完成后分别注入失败，独立查询 pg_catalog 和 CPU 两表验证 schema/constraint/业务行回滚，再成功 retry/repeat；不得仅用不含 CPU schema/data 的旧 durable snapshot 代替这些断言。C70→C72/P70 另须从独立 C70 goldens 生成真实 PostgreSQL predecessor，验证 0071/0072 ledger append、exact four tuple ACL delta、0072 无 ACL expansion、runtime reject-before/admit-after、repeat 深相等。
- settings 回归必须从 released genesis 显式构造 P67 missing/default/custom，分别验证 runtime reject-before/admit-after、完整 settings 与历史 manifest 保持、repeat durable snapshot 相等；P62/P63/P64/P66 另覆盖缺行升级。P67 缺行还覆盖 rollback、权限漂移拒绝及真实双连接并发初始化，不能以补插 settings 或放宽结构验证通过。
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

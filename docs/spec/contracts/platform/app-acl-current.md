# APP 当前迁移与准入

## Scenario: APP current-development scoped migrator and one-snapshot runtime admission

### 1. Scope / Trigger

- 触发：修改 `HOUFENG_RECORDS_ENABLED` / `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` 模式选择、`houfeng-record-platform-admin migrate --scope app`、root migration、current APP fragment/compiler、manifest/catalog verifier、`ConvergeAppACLCurrent`、`AdmitAppACLCurrentRuntime`、center bootstrap、VPS importer，或其 PostgreSQL regression 时。
- current contract 的前 52 个 source 必须 byte-for-byte 等于冻结 `0001…0051` r1 inventory（包含两个按文件名字典序排列的 `0004_*`）。每个后来 embedded migration 必须注册 exact `AppACLCurrentMigrationFragment`；无 APP object 也必须注册 explicit empty fragment。当前 root set 有 67 个 source 并止于 `0066_constrain_monitoring_and_target_state_values.sql`，`0063`、`0064`、`0066` 为 empty fragment；`0065` 仅增加 runtime 对 `public.asset_services`、`public.asset_domains` 的表级 `UPDATE`，无 grant option。
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
- current convergence 只接受 fresh current genesis、五个发布起点与各自 exact target repeat：`[P62]`、`[P63]`、`[P64]` 追加 C66 为 revision 2；`[P62,P63]`、`[P62,P64]` 追加 C66 为 revision 3。P62 是独立 v0.79.4 golden 绑定的 `0062`；P63 是 `2cda12e874b9f9cd4431f477566b72c2d2a5e414`（v0.79.6）发布源码的 compiler 独立导出的 source/privilege golden，止于 `0063`；P64 是基线 `5422f939`（v0.80.2）独立 source/privilege golden 的 `0064`。P63/P64 genesis 允许发布版合法的自定义角色和数据库。P62 待执行 `0063…0066`，P63 待执行 `0064…0066`，P64 待执行 `0065/0066`。每项历史 manifest 按其 profile 与原绑定验证，不以 persisted 权限作为授权来源。禁止任意 prefix、未知 checksum、null-head adoption、额外 revision、未发布的 `[P63,P64]` 链或旧未发布 `0066` 候选接管。
- Writer 保持单个 `SERIALIZABLE` transaction、advisory/ledger lock 与整体 serialization retry：先验旧 source/manifest/ledger/catalog，再业务 preflight、pending SQL、复读 ledger、业务后置验证、current revoke-first DCL、current catalog、追加 latest revision+1/CAS head、完整复读后 commit。任何失败整体回滚 schema、ACL、ledger、manifest/head；catalog 漂移不能通过 DCL 自动修复。fresh 保持现有全量路径；target exact repeat 不执行 SQL/DCL/manifest 写入。P62 保留 heartbeat 3→12/custom 不变语义；P63/P64 验证现有默认值/索引并要求完整 settings 逻辑快照不变，不重跑 `0063`。
- current catalog 以冻结 r1 base（当前为 **204** ACL tuple）加 ordered fragment object/privilege/function hardening 编译。`public.record_platform_cas_contract_activation_projection(bytea)` 与 `public.record_platform_cas_domain_rotation_projection(bytea)` 仍是 migrator-owned、`SECURITY DEFINER`、唯一 `bytea` overload、`search_path=pg_catalog` 且显式 revoke `PUBLIC`。
- production `0052` fragment 增加九张 Records core table、一个 `record_platform_internal.validate_record_revision_primary_subject()` hardened function 与 29 个精确 APP privilege tuple；后续 fragments 累加 current catalog。发布 predecessor 到 current 的权限差必须恰为 `0065` 的两个 runtime 表级 UPDATE，无权限删除或第三项授权；`0066` 仍为空。表级 UPDATE 在数据库层覆盖整表列，应用状态纠正入口仍只修改 `status`、`updated_at`，不得描述为数据库两列权限约束。platform admin 不获得这两项 UPDATE，也不得取得 Records content 读取权或 immutable history UPDATE。
- admission 只验证 compiled migration-owned surface：database、managed schema、relation/view/sequence/function、ledger/manifest、role attributes/membership、owner、direct/effective/column/default ACL 和 function hardening。current convergence 的 placement、fresh-state 与 legacy-ledger companion-object preflight 均以完整 `(schema, object identity)` tuple 检查 relation/function；不同 managed schema 可声明同名对象，无关 schema 中的同名 relation、同名 function 或其他 overload 也不属于 managed tuple。冻结 R1 的历史裸名称 shadow rejection 保持不变。managed private schema 内 unknown object 仍是 drift；无关 schema/object 与 unrelated-owner default ACL 必须接受。
- PostgreSQL 16 `pgcrypto` 必须安装在 `record_platform_internal`；若 extension 已在其他 schema 则 fail closed。extension-member procedure 按 OID 识别，并对普通 managed owner/direct/effective/function reader 保持 opaque，因为受限 migrator 不能可靠改写 bootstrap-owned member ACL。opacity 绝不产生 reachability：`PUBLIC`、runtime、admin 对 `record_platform_internal` 都没有 `USAGE` 或 `CREATE`；同一 admission snapshot 还会拒绝同时具有 schema `USAGE` 与 function `EXECUTE` 的 reachable opaque member。migrator-owned helper/projector 仍必须显式 revoke `PUBLIC`。
- `AdmitAppACLCurrentRuntime` 精确开启一个 `REPEATABLE READ READ ONLY` transaction。先识别并验证完整已注册链；旧 P62/P63/P64 起点返回需要 successor convergence 的 typed 拒绝，仅 C66 终点再比较 current privileges 并验证 catalog。它不执行 DDL/DCL、不调用 writer、不读取 migrator 凭据；失败时 center/importer 关闭 pool，不得回退到 owner migration 或 warning-only dry-run。

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
| 注册 P62/P63/P64 发布起点 | 旧 catalog 先验，按 exact suffix 升级、事务 DCL、revision 2/3 追加与 head CAS；任一阶段失败整体回滚。runtime 只拒绝，不自行升级。 |
| 注册 revision 2/3 exact C66 successor | convergence/runtime 均只读验证成功；repeat 前后 durable snapshot 深相等。 |
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
- Base：当前 embedded set 是冻结 52-source r1 prefix 加 `0052…0066` 共 15 个 exact fragments；fresh convergence 写入 current revision-1 genesis，exact repeat 和 direct runtime admission 均不改 durable state。
- Good：独立 P62/P63/P64 profile 验证后沿五个明确起点升级至 C66，旧 catalog、业务变更与新 catalog 全部成功才发布后继；repeat 只读。
- Good：未来 child 同 PR 添加 `0053+` SQL 与 exact fragment；compiler 在 transaction 前证明一一覆盖，fresh database 自动消费新 source 与 catalog contract。
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

  断言 fresh + direct runtime、五个发布起点升级至 C66 revision 2/3、所有 target repeat、P64 自定义数据库/角色、历史 manifest 字节不变；P62 settings global `3→12`/custom `20`/override 保留、除 `incident_defaults`/`updated_at` 外整行逻辑等价，P63/P64 完整 settings 不变；heartbeat rows、0063 exact index、0065 snapshot/0066 约束、Records readback 与 runtime admission。错误同名 index、released default 漂移、非允许 settings 漂移、旧 catalog 漂移必须零写；至少两个 DCL 切点及 manifest/head 故障证明 schema/ACL/ledger/manifest 完整回滚，重试无残留。repeat 前后 durable snapshot 深相等；wrapper 输出不得含 `SKIP`。
  `TestVPSStateRepairRuntimeACLDependencyCorrectionAndCancellation` 使用直接受限 runtime 登录覆盖服务 unknown→retired、域名 unknown→paused、空依赖 VPS preview/cancellation、元数据不变、审计原因与前后状态、同态不重复、audit 失败回滚、runtime DELETE 与 admin UPDATE 的 42501 拒绝。
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

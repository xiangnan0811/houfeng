# APP 迁移与运行时 ACL 交接

## Goal

交付 `records-on/delete-off` 的 APP-only 生产交接：一次性、直接认证的受限 migrator 在单个事务中完成 r1 schema、ACL 与 manifest 收敛；center 和 VPS 导入器只以直接认证的 runtime 身份读取、验证并使用该状态，绝不再自动迁移。

## 已确认事实

- 父任务 `07-14-vps-records-platform-foundation` 的 `PF-AC-005` 要求 runtime、platform admin 和一次性 migrator 使用不同数据库身份；任一 record flag 打开后只有显式 `migrate` 命令可以写 APP schema/ACL。
- `0001…0051` 是 **52** 个嵌入 SQL 文件（有两个按字典序应用的 `0004_*` 文件）。静态审计未发现顶层 transaction-control、`CONCURRENTLY` 或其他阻止 PostgreSQL 16 单事务执行的语句；现有 `migrate.Apply` 仍是逐迁移提交，不能复用。
- 现有 r1 manifest、ACL compiler、catalog verifier、角色预检和两个 projector 都只有库/fixture 合同，尚无生产 writer 或 runtime-admission 调用方。
- `0051`、r1 manifest 与相关提交均未进入 `origin/main` 或远端 feature 分支；若在实施期间发现任何已外部应用该 checksum 的证据，必须停止修改 `0051` 并回到控制会话设计 forward migration。
- PostgreSQL 16 的 trusted `pgcrypto` 在受限非 superuser 安装时可把 extension member functions 保留为 bootstrap owner；因此 0051 当前的 `REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA record_platform_internal FROM PUBLIC` 不能作为 hardened-state 证据。
- 当前 catalog reader 错误地把所有非系统 schema、所有 owner 的 default ACL 和 extension function 都纳入 APP contract；本任务改为验证 migration-owned APP surface，而不是拒绝无关 schema。
- 两个 `SECURITY DEFINER` projector 接受 caller-supplied projection bytes，但本切片不实现 ledger/witness/trust caller。故 r1 runtime 与 admin 的 persistent-function `EXECUTE` 集必须为空；projector 仍是 migrator-owned、受 admission 验证的 DDL primitive，留给未来单独准入的 caller。

## Requirements

1. 中心配置必须支持 `HOUFENG_RECORDS_ENABLED` 与 `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED`：
   - 两者均为 false 时保留当前 0.59 兼容的 owner/自动迁移路径；
   - `true/false` 时仅使用 `HOUFENG_DATABASE_URL` 的 APP runtime 身份；
   - `false/true` 必须拒绝；`true/true` 在本切片中必须在读取任何外部域输入前 fail closed。
2. 新增唯一的 `houfeng-record-platform-admin migrate --scope app` 入口。它只能打开 `HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL`，读取精确的 APP runtime/admin role 名，并在退出前关闭高权限连接；它不得解析、stat、连接或输出 ledger、witness、recovery、S3、admin/runtime DSN 或其他外部域输入。
3. migrator、runtime 和 admin 都必须是预创建、彼此不同、直接认证的 `LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS` 角色，且三者与其递归成员关系为空。migrator 的 `session_user == current_user`；不得以 role membership、`SET ROLE`、owner reuse 或默认 ACL 规避最小权限。
4. scoped migrator 只支持两种可证明状态：fresh install，或 head 为空、ledger filename/checksum 精确匹配且 migration-owned APP objects 当前均由同一直接 migrator 所有的 legacy adoption。它不得创建/删除/重新归属角色、修复未知 owner、修复无关 schema，或声称能证明历史执行者；不满足条件即 fail closed。
5. APP r1 convergence 必须在一个 PostgreSQL `SERIALIZABLE` 事务中完成。它先锁 advisory key，`SET LOCAL search_path = pg_catalog, public`，创建/接管/锁 `schema_migrations`，执行精确 pending `0001…0051` 并写 checksum；0051 创建 manifest 表后才锁 revisions/head，再收敛 ACL、重读 catalog、写 immutable r1 revision，并以 null-head CAS 提交。必须重试完整 closure 的 serialization failure，绝不调用现有 `migrate.Apply` 或会另开事务的 genesis API。
6. r1 ACL compiler 的 tuple 数固定为 **204**。runtime/admin 在任何 persistent function 上都没有 `EXECUTE`；对两个 public projector 的任何 runtime/admin grant 都是 drift。projector 必须存在且精确满足 migrator owner、`SECURITY DEFINER`、唯一 `bytea` identity 与 `search_path=pg_catalog`，但不因其 owner-only 状态被从 verifier 排除。
7. `0051` 仍要求 `pgcrypto` 在 `record_platform_internal`，已有其他 schema 的 extension 必须失败。移除对 extension-member functions 的 blanket revoke；保留每个 migrator-owned helper/projector 的显式 PUBLIC revoke。verifier 必须把 `pgcrypto` extension members 作为 opaque exception，同时证明 `PUBLIC`、runtime、admin 对 `record_platform_internal` 都没有 `USAGE` 或 `CREATE`，并以实际直接登录调用证明 `digest` 被 SQLSTATE `42501` 拒绝。
8. catalog contract 只覆盖当前数据库 ACL、`public` 与 `record_platform_internal` schema ACL、精确 migration-owned relation/view/sequence/function surface、`schema_migrations`、manifest tables、projector definitions、三角色属性/递归 membership，以及 migrator 的 global/`public`/`record_platform_internal` default ACL。无关 schema、无关 owner 的 default ACL 和无关对象不得影响 APP admission；受管 surface 中的未知 object/grant/column ACL/default ACL/owner 仍 fail closed。
9. 将经过验证的 migrator catalog role 作为 r1 `AppACLManifestPersistedV1` 的不可变、摘要绑定字段，供 runtime 在不读取 migrator 配置或凭据时验证 projector owner。该字段与 Go codec、SQL CHECK、数据库表、runtime reader、catalog verifier 和父 Task 11 field-order 说明必须同步。
10. records-on center 及任何会打开数据库的 VPS 导入路径必须在构造 repository 前，以同一直接 runtime 会话执行一个 `REPEATABLE READ READ ONLY` APP admission：验证 `session_user == current_user`、manifest chain、完整 migration filename/checksum set、stored runtime/admin/migrator identities 和 scoped effective catalog。它们不得调用 migration writer；任一失败不得回退为 owner 自动迁移。
11. permanent delete、外部 ledger/witness/recovery、activation/trust、workers、S3、0052+、Child 2–11 均不在范围。本切片结束后 Child 1 仍为 `in_progress`，不能放行其他 child。

## Acceptance Criteria

- [x] AC-01：flags-off 不改变现有 center/importer 自动迁移行为；`false/true` 与 `true/true` 在读取外部域输入前明确拒绝；`true/false` 不调用 `migrate.Apply`。
- [x] AC-02：`migrate --scope app` 的环境和参数 allowlist 仅接受 APP migrator URL、runtime role、admin role；禁止输入不被读取，命令的成功和失败路径均关闭连接且不泄漏 DSN。
- [x] AC-03：真实 PostgreSQL 覆盖三个独立 direct-login role，证明所有属性、`session_user == current_user`、无递归 membership、runtime/admin 不拥有受管对象，且错误 schema/owner 的 legacy state 被拒绝。
- [x] AC-04：受限 direct migrator 的 fresh install 与 eligible null-head adoption 在单个 transaction 中产生同一 r1、52 个 filename/checksum、204 tuple compiler ACL、migrator role binding 和 repeat read-only 结果；任一 late cutpoint、ACL/catalog drift、head drift 或 serialization retry 不留下部分 SQL、ledger、ACL、revision 或 head。
- [x] AC-05：r1 migrator identity 被 canonical codec、SQL schema/CHECK、persisted reader 与 runtime admission 逐字验证；篡改 role、manifest、ledger、受管 catalog、migrator-scoped default ACL、membership、`PUBLIC`、column/sequence/function privilege 或 owner 都 fail closed；无关 schema/default ACL 不导致失败。
- [x] AC-06：真实 schema 上 runtime/admin 没有 persistent-function `EXECUTE`，调用两个 projector 和 `record_platform_internal.digest` 均返回 SQLSTATE `42501`；catalog verifier 仍验证两 projector 的 hardened definition。
- [x] AC-07：records-on/delete-off 的 center 与 importer 只允许直接 runtime 登录通过 one-snapshot admission 后使用 repository；pending/unknown migration 和 `SET ROLE` 均被拒绝，且不发生 writer fallback。
- [x] AC-08：unit、真实 PostgreSQL、业务 smoke 和完整 Go gate 都有可复核 PASS 证据；不得把 locally skipped integration 当作验收。
- [x] AC-09：Child 1 保持 `in_progress`，Child 2–11 保持不准入。
- [x] AC-10：`.trellis/spec/backend/database-guidelines.md` 的 APP ACL 场景改为本任务的 managed-surface、one-snapshot 与 PG16 `pgcrypto` 合同；不得保留“扫描所有非系统 schema / 所有 owner default ACL”的相互矛盾规则。

## 非目标

- 不实现 permanent delete、ledger/full witness/recovery-control、activation/trust、domain rotation、S3 或 worker admission。
- 不支持 shared-database 的全库 ACL 清扫、无关 schema 的 owner/default ACL repair，或对不具备 migrator ownership 的任意 legacy database “自动接管”。
- 不创建 `0052+`、r2 manifest 或下游 Child 所有权的迁移。
- 不设计 projector 的未来 trusted caller；本 r1 只冻结 deny-by-default boundary。

## 兼容与回滚

- 两个 flags 都关闭时，原有 `HOUFENG_DATABASE_URL` owner 流程不读取任何 record-platform 外部输入。
- records-on 只允许前置运行一次 scoped migrator；runtime admission 失败仅阻止启动/写入，不执行 DDL 或 ACL 修复。
- 对已有外部 checksum 的 0051，立即停止，保留旧 migration 不变并回到控制会话设计受控 forward migration。

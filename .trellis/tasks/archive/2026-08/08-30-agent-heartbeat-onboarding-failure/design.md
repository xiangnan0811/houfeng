# Agent 同步批次 INSERT-only 权限修复设计

## 1. 结论与推荐方案

推荐只修改 `recordAgentSyncBatch` 的冲突处理：

```sql
insert into agent_sync_batches (monitoring_instance_id, sync_batch_id)
values ($1, $2)
on conflict do nothing
```

不指定 conflict target，使 PostgreSQL 不再要求 runtime 对复合主键列拥有 `SELECT`。这保留 `agent_sync_batches` 的 INSERT-only 最小权限，也不需要 schema、migration、ACL manifest、agent 或协议变更。

当前表只有 `(monitoring_instance_id, sync_batch_id)` 复合主键这一项唯一性约束，所以首次插入与重复批次的业务语义不变。若未来新增其他唯一约束，必须重新审查 targetless `ON CONFLICT` 会忽略所有唯一冲突这一行为；不能在没有真实 runtime-role PostgreSQL 证据时恢复显式 target。

## 2. 现场证据链

1. 两台不同架构 agent 均为 v0.79.2 官方产物，systemd active、无重启循环，并按固定节奏收到 Center `500 internal_error`。
2. Center 与 PostgreSQL 日志在相同节奏出现 `permission denied for table agent_sync_batches`，并打印出生产 insert 的显式复合冲突目标。
3. 请求已到达 Center，因此 DNS、TLS、proxy path、Bearer 路由和 agent 进程存活不是当前断点。
4. `db/migrations/0045_create_agent_sync_batches.sql` 只定义复合主键和普通时间索引。
5. `acl_manifest_allowlist.go` 明确只给 runtime `INSERT`；current convergence 与 runtime admission 均按此合同成功，说明 init/Center startup 的绿色结果没有遗漏静态 grant。
6. PostgreSQL 16.12 隔离复现：INSERT-only + 显式 target 返回 42501；增加 conflict-key SELECT 后成功；撤回 SELECT 并使用 targetless form 后首次插入 `INSERT 0 1`、重复插入 `INSERT 0 0`。
7. 现有 `fakeSyncBatchTx` 根据 SQL substring 直接返回 command tag，不执行 PostgreSQL 权限检查；现有 real-PG current ACL 测试检查 catalog 和其他 Records DML，但没有调用 production agent sync repository。

PostgreSQL 16 官方 `INSERT` 文档明确规定：所有 `ON CONFLICT` 形式都要求对被读取列拥有 `SELECT`，其中包括 `conflict_target` 或 arbiter constraint 提到的列；同时，无 target 的 `DO NOTHING` 会处理所有可用唯一约束/索引冲突。参考：https://www.postgresql.org/docs/16/sql-insert.html

因此根因不是 v0.79.2 agent 二进制、代理或容器退出，而是生产 SQL 与既定最小权限合同之间的隐式 PostgreSQL 权限不兼容。v0.79.2 的 5xx retry 只是保证批次没有被误删。

## 3. 数据流与不变量

```text
agent durable queue
  -> POST agent sync
  -> handler validates request shape
  -> PostgresSyncRepository.ApplyBatch
     -> lock/read MonitoringInstance and validate token + fingerprint
     -> insert agent_sync_batches idempotency fact
     -> first batch: heartbeat / observations / instance sync state / plan
     -> duplicate batch: commit and return empty plan without rewriting facts
```

必须保持：

- token/fingerprint/binding 验证先于任何同步事实写入；
- paused、retired、archived 的 suppression 仍先于 batch-id insert；
- batch id 仍由第一条 heartbeat carrier 提供；
- 首次批次与重复批次共用同一事务原子边界；
- 只有真实 heartbeat 写入才推进 `last_heartbeat_at` / `last_sync_at`；
- agent 收到 5xx 时继续保留并重试，Center 不泄露数据库细节。

## 4. 方案比较

### A. 扩大表级 SELECT

拒绝。它会扩大 runtime 对幂等事实表的读取能力，要求修改 current ACL contract，并把一个 SQL 形状缺陷转化为长期权限扩张。

### B. 只授予复合主键列 SELECT

拒绝。虽然 PostgreSQL 能执行，但项目 current ACL 明确不接受 column ACL，catalog verifier 也要求 column ACL 为空；这会引入更复杂且不必要的授权面。

### C. targetless `ON CONFLICT DO NOTHING`

采用。它在当前单一唯一约束下保持完全相同的首次/重复语义，不读取业务列，不变更 ACL 或 schema，升级 Center 即可生效。

### D. 先 SELECT 再 INSERT 或捕获 unique violation

拒绝。前者直接需要 SELECT 且引入竞态；后者需要依赖错误分类/回滚或 savepoint，明显大于一行 SQL 修复并增加事务复杂度。

## 5. 文件与范围

计划中的产品改动：

- `internal/center/store/sync_batches.go`：将显式 conflict target 改为 targetless form，并用短注释记录 INSERT-only 原因。
- `internal/center/store/sync_batches_test.go`：冻结 SQL 形状，防止 fake transaction 再次掩盖隐式权限。
- `internal/center/store/sync_batches_postgres_integration_test.go`：通过 current ACL convergence/admission 和真实 runtime role 执行 production repository 的首次/重复 heartbeat-only 批次。
- `.trellis/spec/backend/database-guidelines.md`：补充 agent sync batch 的 INSERT-only、targetless conflict 与未来新增唯一约束的重审合同。

不修改：

- `db/migrations/**`；
- `internal/center/store/migrate/acl_manifest_allowlist.go` 与 managed surface；
- agent、installer、proxy、Compose、handler、DTO、Web 或 error mapping。

## 6. TDD 与 PostgreSQL 证据

真实 PostgreSQL test 使用现有 `newRecordsPostgresFixture`：它创建独立临时数据库和 direct login roles，应用完整迁移，执行 `ConvergeAppACLCurrent`，再以 runtime 角色通过 `AdmitAppACLCurrentRuntime`。测试随后：

1. 用 fixture owner seed 一个已绑定 MonitoringInstance，只保存 token hash；
2. 确认 runtime 对 `agent_sync_batches` 有 INSERT、无 SELECT/UPDATE/DELETE；
3. 用 `NewPostgresSyncRepository(runtimePool)` 提交 heartbeat-only batch；
4. 在旧代码上确认 wrapped cause 是 SQLSTATE 42501，形成真实 RED；
5. 应用 targetless 修复后确认首次提交成功；
6. 原样重复提交并确认成功、批次与 heartbeat 各恰好一行；
7. 确认实例心跳/同步时间已推进，ACL 仍未扩大。

strict runner 必须实际启动 PostgreSQL 16；任何 `SKIP` 都不是本任务验收证据。普通 `make verify-go` 可以保留现有无 fixture 时 skip 的仓库惯例，但不能替代 strict lane。

## 7. 发布、恢复与回滚

- 无数据库 migration 或手工 SQL。发布新的 Center 镜像并重建/重启 Center 服务即可。
- v0.79.2 agent 已把 5xx 批次保留在 durable queue；Center 恢复后，现有两个 agent 会在后续 tick 自动重试，无需重新安装或重新生成 token。
- 现场验证同时检查 Center/PostgreSQL 不再出现该表权限错误、同步 HTTP 成功、`agent_sync_batches`/heartbeat/last-sync 事实推进。只看 systemd active 不构成恢复证据。
- 如需回滚，仅回滚 Center 代码/镜像；没有数据库状态迁移需要撤销。回滚会重新引入已知 500，但不会要求数据回滚。

## 8. 风险

- **未来新增另一项唯一约束：** targetless form 会忽略该冲突。通过 spec 和真实重复批次测试冻结当前前提；schema 变更时强制重审。
- **测试假绿：** fake tx 不证明权限。验收必须包含 direct runtime role 的 PostgreSQL 16 production repository 调用。
- **范围扩张：** 不以“修复权限”为由修改 ACL、migration 或 agent。
- **现场误判：** 一次性 init 容器 `Exited (0)` 与本缺陷无关；只追踪非零退出、Center sync 500 和数据库 42501。

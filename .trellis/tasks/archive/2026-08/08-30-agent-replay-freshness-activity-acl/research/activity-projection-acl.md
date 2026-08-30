# Activity Projection 与 current APP ACL 研究

## 1. 结论

Field audit 问题 4 的推荐答案是：**调整 production SQL，使候选投影事实的分类查询不再使用 `FOR UPDATE`；不要给 `record_activity_projection` 增加 `UPDATE` 权限。**

问题不是业务上缺少“修改已发布事实”的能力，而是一个只读分类查询选择了 PostgreSQL 会按更新能力鉴权的行锁语法。当前生产协议已经先锁定唯一 active generation 的 head 行；同一 generation 的发布者和 rebuild 都在这把锁下串行化。之后对候选事实做普通 `SELECT`，再以 strict `INSERT` 写入缺失事实，仍能维持：

- 相同 `activity_id` 的已发布事实只做 canonical hash 分类，不原地修改；
- 同一批次只给真正缺失的事实分配连续 sequence；
- 意外唯一键冲突使整个事务失败，不会静默吞掉已分配 sequence；
- head watermark 与投影事实在同一事务提交；
- rebuild 与 publisher 继续由 active head 行锁互斥。

因此，现象应归类为 **SQL 与最小权限契约不匹配**，而不是 current APP ACL 漏授业务必需的 `UPDATE`。

本阶段没有运行测试，也没有访问远端或数据库；下面的 RED/GREEN 是后续实现阶段的可执行设计。

## 2. 生产 SQL、并发与事务语义

### 2.1 当前写入路径

`PublishActivityBatch` 的关键顺序如下：

1. 在事务内以 `FOR UPDATE` 锁定唯一 active `record_activity_projection_heads` 行，读取 generation 与 watermark（`internal/center/store/record_activity.go:97-105`）。
2. 读取候选 `activity_id` 的既有 canonical hash，当前查询对 `record_activity_projection` 使用 `FOR UPDATE`（同文件 `279-305`，尤其 `295-300`）。
3. 将候选分类为 already-published、missing 或 hash mismatch；hash mismatch 返回稳定 sentinel（同文件 `115-145`）。
4. 对 missing 集合确定性排序，并从 head watermark 后分配连续 sequence（同文件 `147-181`）。
5. 更新 head watermark，写投影事实、subjects、revision intervals，全部在同一事务完成（同文件 `183-276`、`328-416`）。投影事实本身使用 strict `INSERT`，没有 `ON CONFLICT DO NOTHING`。

这里真正的 publisher generation/sequence 串行化点是第 1 步的 active head 行锁。候选投影事实行锁不是 sequence 分配的互斥根。

### 2.2 与其他生产路径的关系

- rebuild 也先对 active head 使用 `FOR UPDATE`，随后 retire 旧 generation、建立新 generation 并清除旧投影（`internal/center/store/record_activity_recovery.go:28-63`）。因此 publisher 与 rebuild 已共享同一事务锁边界。
- revision interval 的 open-row 查询使用 `FOR UPDATE`，之后确实会关闭该 interval，即执行 `UPDATE`（`internal/center/store/record_activity.go:202-276`）。该表的 runtime `UPDATE` 是业务必需权限，不能与不可变投影事实混为一谈。
- purge 通过受约束函数执行，先校验 reservation/fence，再删除投影与相关派生数据；它不是用 `UPDATE` 修订既有投影事实（`internal/center/store/record_activity_deletion.go:89-203`，`db/migrations/0057_create_record_activity.sql:234-347`）。

### 2.3 删除候选行锁后仍成立的并发契约

推荐 SQL 只把候选哈希查询从：

```sql
select activity_id, canonical_hash
from record_activity_projection
where activity_id = any($1::text[])
order by activity_id
for update
```

改为普通、确定性排序的 `SELECT`。active head 的 `FOR UPDATE` 保留。

理由：

- 其他 publisher 必须先获得同一 active head 行锁，无法在本事务分类与提交之间发布同一 generation 的事实。
- rebuild 同样受 head 行锁阻挡。
- 普通 `SELECT` 在 PostgreSQL 默认 `READ COMMITTED` 下取得该语句开始前已经提交的事实；持有 head 锁后，另一个遵守协议的 publisher 不会再并发提交新事实。
- strict `INSERT` 是最终完整性防线：若存在不遵守仓储锁协议的写入者导致唯一键冲突，事务整体失败并回滚 watermark，而不是产生 sequence 空洞。
- 对 missing 行无法取得行锁；因此现有 `FOR UPDATE` 本来就不能为“尚不存在的候选事实”提供锁保护。删除/清理互斥仍应由已有 reservation/fence、受约束 purge 与 head/rebuild 协议承担。

需要在实现审查中明确一个边界：current ACL 仍允许 runtime 删除 rebuildable projection，因为受约束 purge/rebuild 是现有业务能力；SQL 改动不能被解释为允许任意调用方绕过仓储协议。若产品另有尚未记录的要求——必须用候选事实行锁去阻挡任意 raw `DELETE`——才需要把“保留行锁并扩权”升级为用户选择。按当前源码、迁移与规范，没有这样的业务契约，而且该行锁对 missing 行也无效。

## 3. PostgreSQL 16 权限事实

PostgreSQL 16 官方 `SELECT` 文档规定，`FOR UPDATE`、`FOR NO KEY UPDATE`、`FOR SHARE`、`FOR KEY SHARE` 都要求对被锁表至少一个列具有 `UPDATE` 权限，并且仍要求所读取列的 `SELECT` 权限：

- [PostgreSQL 16 SELECT](https://www.postgresql.org/docs/16/sql-select.html)
- [PostgreSQL 16 Privileges](https://www.postgresql.org/docs/16/ddl-priv.html)

所以把 `FOR UPDATE` 换成 `FOR NO KEY UPDATE`、`FOR SHARE` 或 `FOR KEY SHARE` **不能**解决 SQLSTATE `42501`，也不能维持 current ACL 的 `UPDATE=false`。

## 4. 方案比较

| 方案 | 做法 | 并发与权限结果 | 迁移影响 | 结论 |
|---|---|---|---|---|
| A. 适配 SQL | 去掉候选投影查询的 `FOR UPDATE`；保留 active head `FOR UPDATE`、strict `INSERT` 和同事务 watermark | 复用现有串行化根；投影事实仍不可变；runtime 无新增能力 | 无 migration、无 ACL fragment 变化 | **推荐** |
| B. 扩表级 ACL | 保留 SQL，给 runtime `record_activity_projection UPDATE` | 机械消除 `42501`，但同时允许 runtime 原地修改任意投影列，扩大攻击/误操作面，并违反明确的不可变事实契约 | 不能只改 fragment；既有 current DB 无原地 DCL 升级路径 | 不推荐 |
| C. 特权绕行 | 以 column-level `UPDATE`、trigger，或 `SECURITY DEFINER` classifier 函数保留行锁 | 仍引入更新能力或新的高权限函数边界；复杂度高，而 publisher/rebuild 已由 head 锁串行化 | 需要 schema/DCL 迁移、fragment、catalog 与升级协议共同变化 | 不推荐 |

方案 C 中，column-level `UPDATE` 还会直接违反 current ACL 测试要求的“零 column ACL”；`SECURITY DEFINER` 则会把普通分类读取升级成需单独审计 search path、owner、EXECUTE 与注入面的高权限入口。两者都没有与风险相称的收益。

## 5. 推荐的最小权限向量

推荐方案不改变 current contract。关键对象的 runtime 最小权限应继续是：

| 对象 | SELECT | INSERT | UPDATE | DELETE | 说明 |
|---|---:|---:|---:|---:|---|
| `record_activity_projection_heads` | 是 | 是 | 是 | 否 | generation 建立、head 锁与 watermark 更新 |
| `record_activity_projection` | 是 | 是 | **否** | 是 | 分类/发布；删除仅服务既有 rebuildable projection 清理能力 |
| `record_activity_subjects` | 是 | 是 | 否 | 是 | 派生索引，不原地修改 |
| `record_activity_checkpoints` | 是 | 是 | 是 | 是 | projector checkpoint 可推进/重建 |
| `record_activity_revision_intervals` | 是 | 是 | 是 | 是 | open interval 会被锁定并关闭 |
| `record_activity_receipts` | 是 | 是 | 否 | 否 | durable receipt append-only |

另外：

- `record_activity_projection` 的 runtime **column ACL 条目必须为 0**；不能借列级 `UPDATE` 绕过表级向量。
- runtime 保留对受约束 purge 函数的 `EXECUTE`；admin 仅保留 receipt `SELECT`，不能读投影内容。
- 当前 compiler 正是上述向量：`internal/center/store/migrate/app_acl_current_contract.go:205-252`；其中投影事实不可变的设计说明见 `197-204`。
- ACL snapshot 测试明确要求 projection `UPDATE=false` 且无 column ACL：`internal/center/store/migrate/record_activity_app_acl_test.go:73-113`、`147-185`。

## 6. Direct-runtime PostgreSQL 16 RED/GREEN 设计

### 6.1 新测试定位

增加一个严格 direct-runtime 集成测试，例如：

```text
TestPostgresIntegrationRecordActivityRuntimeACL
```

测试应复用 `newRecordsPostgresFixture`：它先用 owner/migrator 收敛 `app_acl_current`，再通过 direct runtime admission 打开 runtime DSN（`internal/center/store/records_postgres_integration_test.go:743-755`）。测试结构可对齐已经验证过 current ACL production path 的 `TestPostgresIntegrationSyncBatchRuntimeACL`（`internal/center/store/sync_batches_postgres_integration_test.go:19-138`）。

不要复用目前只用 owner pool 的 `record_activity_postgres_integration_test.go` fixture 来证明 ACL；该文件可以继续覆盖事务/并发语义，但不能证明 direct runtime 权限充分。

### 6.2 RED

在实现 SQL 改动之前，新 acceptance 测试直接以 runtime pool 调用生产仓储：

1. 断言 server major 是 PostgreSQL 16。
2. 用 catalog helper 断言 projection runtime 权限为 `SELECT=true, INSERT=true, DELETE=true, UPDATE=false`，column ACL 数量为 0；同时检查 heads 与 revision intervals 的必要 `UPDATE=true`。
3. 通过生产 `EnsureActiveActivityProjectionGeneration` 建立 active generation。
4. 通过生产 `PublishActivityBatch` 发布一个合法候选。
5. 当前代码应在候选分类的 `SELECT ... FOR UPDATE` 处返回 typed `*pgconn.PgError`，SQLSTATE 为 `42501`。测试失败报告只打印稳定 code/分类，不泄露 raw database message。
6. 由 owner fixture 验证事务原子回滚：projection/subjects 为 0，head watermark 仍为 0。

永久 acceptance 断言应写成“发布必须成功”；这样未经修改的 production SQL 自然 RED，修改后同一测试自然 GREEN，而不是保留一个期待生产失败的测试。

### 6.3 GREEN 与回归断言

去掉候选查询行锁后，同一 current ACL 下验证：

1. 首次发布成功，得到 generation 内 sequence 1，head watermark 为 1。
2. 完全相同候选重试：`inserted=0`、`already_published=1`，watermark 不变。
3. 相同 `activity_id` 但 canonical hash 不同：返回 `ErrActivitySourceHashMismatch`，不改事实与 watermark。
4. 两个 direct-runtime 连接并发发布不同候选，最终 sequence 唯一且连续；也可加入 exact duplicate 并发，验证一方发布、另一方分类为 already-published。
5. 现有 head-lock rollback/gap-free、retry/idempotency、canonical mismatch 测试继续通过（`internal/center/store/record_activity_postgres_integration_test.go:64-233`）。
6. 若实现审查仍担心 purge 交错，再加一个有界并发用例：publisher 持有 head 锁并完成分类时触发受约束 purge，断言不存在 watermark 空洞、hash 漂移或部分 projection/subjects；测试必须只走生产 purge API，不能以 owner raw SQL 伪造 runtime 语义。

### 6.4 严格命令草案

后续实现阶段应使用现有 PostgreSQL integration runner，要求 RUN/PASS，不能把 DSN 缺失当成 SKIP：

```bash
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store \
  -run '^TestPostgresIntegrationRecordActivityRuntimeACL$' -count=1

GOTOOLCHAIN=go1.26.2 go test ./internal/center/store/migrate \
  -run 'RecordActivityAppACL|AppACLCurrent' -count=1

GOTOOLCHAIN=go1.26.2 go test ./internal/center/store \
  -run 'Activity' -count=1
```

并发用例应另做重复或 race gate，而不把慢速、环境依赖的 PostgreSQL 测试混入普通 unit gate。

## 7. Migration / fragment 边界

### 7.1 推荐方案 A

SQL-only 方案的边界最小：

- 不修改已发布的 `db/migrations/0057_create_record_activity.sql`。
- 不新增 migration。
- 不修改 `recordActivityProjectionPrivilegeFragment` 或 expected ACL snapshot。
- 既有 current database 的 source count/checksum、fragment checksum、privilege body 与 catalog contract 全部保持 exact。
- 后续实现只需修改生产 store 查询，并增加/收紧 direct-runtime 与并发测试。

### 7.2 如果坚持方案 B/C

不能把 ACL 扩权理解为“只改一行 fragment 即可上线”：

- current contract 要求 migration source 与 privilege fragment 闭世界一一对应（`internal/center/store/migrate/app_acl_current_contract.go:49-63`、`785-944`）。
- 对已经 exact 的 current DB，convergence 只核验 manifest、compiled privilege body 与 catalog，不会重新执行 DCL（`internal/center/store/migrate/app_acl_current_convergence.go:204-270`）。因此只改 fragment 会让旧 catalog 与新 compiled body 不一致，并不能授予权限。
- fresh DB 才会 apply migrations、执行 compiled DCL 并写 current genesis（同文件 `272-346`）。
- 新增 post-0057 migration/fragment 会改变 migration source set；现有协议把旧 source count/checksum 与新源码不一致判为 `ErrDevelopmentDatabaseRebuildRequired`（同文件 `349-369`），不是原地升级。

所以若产品最终选择 ACL 扩权，必须另行设计并评审 successor current-contract / in-place upgrade 协议，或明确接受 rebuild；严禁修改 0057，也不能用 field/manual `GRANT` 绕过 manifest。该成本进一步支持优先采用 SQL-only 方案。

## 8. 规划级验收与独立审查关注点

实施计划应至少设置以下可核验检查点：

1. RED 必须由 direct runtime、生产仓储与 PG16 共同复现，而不是 owner pool、catalog 模拟或手写替代 SQL。
2. GREEN 必须在 projection `UPDATE=false`、column ACL 为 0 的 exact current contract 下完成。
3. 审查 publisher、rebuild、purge 三条生产路径是否都遵守已声明的 head/fence 边界，尤其确认没有第二条绕过 head 的 projection INSERT 路径。
4. 现有 gap-free、retry/idempotency、hash mismatch 与 rollback 用例全部回归；新增并发用例不能只断言“无错误”，还要断言 sequence、watermark、事实数量与 canonical hash。
5. 错误面只暴露稳定 sentinel 或 SQLSTATE 分类；不把 raw PG message、DSN 或 credential 写入 API、日志或测试输出。
6. 独立审查必须单独回答：去掉候选行锁后是否存在一个生产调用方能在持有/等待 head 锁之外 raw INSERT；如有，先收敛该协议，不应以扩大 projection `UPDATE` 掩盖。

## 9. 对 field-audit 问题 4 的简答

**应改 SQL 适配现有 ACL。** `record_activity_projection` 是已发布不可变事实，业务不需要 runtime 原地更新；`42501` 来自 PostgreSQL 把 `SELECT ... FOR UPDATE` 视为需要更新能力。现有 active head 行锁已经承担 publisher/rebuild 串行化，strict INSERT 与同事务 watermark 保持最终完整性。推荐删除候选投影查询的 `FOR UPDATE`，维持 projection `SELECT/INSERT/DELETE=true, UPDATE=false, column ACL=0`。该方案无需 migration/fragment 变化，并且可以由 direct-runtime PG16 acceptance test 从自然 `42501` RED 推进到最小权限 GREEN。

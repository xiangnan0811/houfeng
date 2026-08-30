# Bug Analysis: INSERT-only ACL 与 `ON CONFLICT` 隐式读权限不兼容

## 1. Root Cause Category

- **Category**: D — Test Coverage Gap；同时包含 E — Implicit Assumption。
- **Specific Cause**: current APP ACL 的 catalog convergence 正确证明 runtime 只拥有 `agent_sync_batches.INSERT`，但生产 SQL 显式指定 conflict target。PostgreSQL 16 会读取 conflict target columns，因此静态授权正确并不代表生产 DML 可执行。原 fake transaction 只匹配 SQL 并返回 command tag，无法执行数据库 privilege check。

## 2. Why Fixes Failed

1. **Catalog-only verification**: 只能证明 grant 集合与 manifest 一致，不能证明 SQL 的隐式权限需求。
2. **Fake transaction coverage**: 能证明调用顺序、参数、rollback 与 `RowsAffected` 分支，但不能证明 PostgreSQL parser/planner/executor 的权限语义。
3. **Process signal misclassification**: agent systemd active、init container `Exited(0)` 与请求穿过 proxy 都是局部健康信号，不能替代 Center 持久化事实或数据库错误证据。

## 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Test Coverage | 用 production repository + direct runtime role 在 PostgreSQL 16 执行首次、重复和旧显式 target 负向探针 | DONE |
| P0 | Least privilege | 保持 runtime INSERT-only，禁止通过 table/column SELECT 绕过 SQL 形状缺陷 | DONE |
| P0 | Regression semantics | 冻结唯一索引集合、`RowsAffected`、exact empty plan、重复批次不重写 heartbeat/sync 时间 | DONE |
| P1 | Privacy review | 对未知 store error 冻结 exact `500 internal_error` envelope，并扫描 body/headers 中的 DB/token/fingerprint 细节 | DONE |
| P1 | Spec | 在 database/quality specs 记录 strict RUN/PASS 与未来新增 unique constraint 的重审触发器 | DONE |

## 4. Systematic Expansion

- **Similar Issues**: 任何依赖 `ON CONFLICT`、`RETURNING`、row locking、policy、trigger 或函数调用的最小权限 SQL，都可能拥有 catalog grant 之外的隐式权限需求。
- **Design Improvement**: 静态 ACL verifier 与真实 runtime-role DML 各自负责不同证据；两者不能互相替代。
- **Process Improvement**: 对权限类 bug，诊断必须同时关联 client retry、Center handler、production repository 和 PostgreSQL direct-role execution，不以单层健康状态收尾。

## 5. Knowledge Capture

- [x] 更新 `.trellis/spec/backend/database-guidelines.md` 的可执行 DB/ACL 合同。
- [x] 更新 `.trellis/spec/backend/quality-guidelines.md` 的 real-PostgreSQL strict runner 合同。
- [x] 更新 `.trellis/spec/guides/cross-layer-thinking-guide.md`，加入 least-privilege SQL 的真实 runtime-role 检查触发器。
- [x] 当前仓库不存在 `src/templates/markdown/spec/`，无可同步模板；不得凭空创建另一套 spec source。

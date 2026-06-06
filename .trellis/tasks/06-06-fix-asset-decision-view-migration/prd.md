# 修复资产决策记录视图迁移启动失败

## Goal

修复 v0.44.0 启动时应用 `0036_add_asset_decision_member_followups.sql` 失败的问题。现有数据库从 `0035` 升级到 `0036` 时，`asset_decision_records_with_counts` 视图列结构发生变化，PostgreSQL 拒绝 `CREATE OR REPLACE VIEW` 在中间插入 followup 统计列，导致 center bootstrap 失败。

## Requirements

- 修复 `0036_add_asset_decision_member_followups.sql`，让它在已有 `asset_decision_records_with_counts` 视图存在时可以安全替换为新结构。
- 修复必须兼容失败后重试：如果列、约束或索引已部分存在，重新执行迁移仍应成功。
- 不新增业务 schema 语义，不改变 records API，不改变资产决策执行编排逻辑。
- 增加测试或静态校验，覆盖 PostgreSQL 视图结构变更不得直接依赖 `CREATE OR REPLACE VIEW` 改列顺序/列名这一约束。
- 更新后端数据库规范，记录 view shape 变更的迁移写法。

## Acceptance Criteria

- [ ] `go test ./internal/center/store/migrate` 通过。
- [ ] `make verify-go` 通过。
- [ ] 如果本地可用 PostgreSQL，运行 `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store/migrate -run TestPostgresIntegrationAppliesFreshMigrations` 并确认 fresh migration 通过。
- [ ] 修复后从 `0035` 到 `0036` 的迁移不会再出现 `cannot change name of view column "evidence_snapshot" to "followup_todo_count"`。
- [ ] 不修改 VPS / Subscription / MonitoringInstance / Target 业务写路径。

## Notes

- 这是发布后启动阻断热修。通常不修改已发布迁移；但本次失败发生在该迁移记录进 `schema_migrations` 之前，后续新迁移无法被执行，因此必须修正失败迁移本身。

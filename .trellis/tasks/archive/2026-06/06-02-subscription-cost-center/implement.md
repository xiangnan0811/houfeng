# 订阅模块 VPS 成本中枢实施清单

## Order

1. 分支与任务
   - 使用 worktree 分支 `worktree/subscription-cost-center`。
   - 启用 git hooks。
   - `task.py start` 后再改代码。

2. 后端模型与迁移
   - 增加 settings JSON、汇率缓存、预算、提醒投递、订阅事实轻量字段迁移。
   - 更新 settings 类型、仓库 scan/upsert、presentation 脱敏。
   - 更新 subscription 类型、store select/insert/update/filter。

3. 后端服务与 API
   - 实现汇率 Provider、成本折算、预算计算、overview/statistics。
   - 实现预算仓库与 handler。
   - 实现订阅设置、手动刷新、overview/statistics handler。
   - 更新 router/bootstrap/bootstrap_test。

4. Workers
   - 实现 exchange refresh worker。
   - 实现 subscription reminder worker 与 settings-aware notifier facade。
   - 添加 fake clock/fake notifier 测试。

5. 前端
   - 更新 `types.ts` 与 `api.ts`。
   - 重构 `/subscriptions` 为 workbench，并保留 create/edit。
   - VPS 详情加成本卡，Asset Decisions 加成本信号，Dashboard 加紧凑摘要。
   - 补页面和 API 测试。

6. 验证
   - 定向 Go 测试：settings/subscriptions/costs/budgets/reminders/handlers/bootstrap。
   - 定向 Web 测试：api、SubscriptionsPage、VPSDetailPage、AssetDecisionsPage、DashboardPage。
   - `make verify-go`、`make verify-web`，最终 `./scripts/verify.sh`。
   - 视觉：启动 web dev server，按 `/subscriptions`、`/vps/:id`、`/asset-decisions`、Dashboard 做 desktop/mobile sanity；若工具不可用则记录限制。

## Risk Gates

- 完成后端 settings/secret 脱敏测试前，不接前端设置页。
- 完成提醒去重测试前，不启用 reminder worker wiring。
- 完成 Dashboard contract 测试前，不扩 Dashboard UI。
- 若范围超过当前回合可稳定完成，优先交付后端 contracts + `/subscriptions` 工作台核心路径，再把更细的未来性价比/趋势快照作为后续任务。

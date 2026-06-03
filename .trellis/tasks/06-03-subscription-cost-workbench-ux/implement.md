# Implementation Plan

## Steps

1. 启动 Trellis 任务并确认 worktree 分支与 hooks。
2. 后端扩展 statistics 合同：
   - 类型增加 `cost_month_buckets`。
   - repository 增加历史月成本查询。
   - service 填充该字段。
   - 更新 service/handler 相关测试。
3. 前端类型与 API 测试更新：
   - `SubscriptionStatistics` 类型加入 `cost_month_buckets`。
   - statistics 年度请求与预算 create/update payload 覆盖。
4. 设置页重构：
   - `SettingsPage` 使用 `useSearchParams` 管理 tab。
   - 根容器从 form 改成非 form。
   - 非订阅 tab 使用系统设置保存 form。
   - 新增 `SubscriptionSettingsSection` 管理订阅设置与预算。
5. 订阅页重构：
   - 移除底部配置表单和主页面 settings/budgets 强依赖。
   - 分层加载核心数据与 statistics。
   - 增加订阅配置入口。
   - 增加成本洞察区域。
   - 将筛选、chips、结果数量和订阅明细合入同一列表 panel。
6. 样式与响应式：
   - 四指标桌面一行。
   - 新增洞察、donut、breakdown、订阅设置 tab 样式。
   - 移动端溢出控制。
7. 验证：
   - Go 相关测试。
   - `cd web && npm run lint`。
   - `cd web && npm run test -- --run SubscriptionsPage SettingsPage MetricChart Sparkline`。
   - `cd web && npm run build`。
   - 若时间允许，运行 `make verify-go && make verify-web` 或 `./scripts/verify.sh`。

## Notes

- 不提交到 main。
- 不新增图表依赖。
- 保留现有订阅创建/编辑和 URL 行为。
- 若发现已有数据无法证明某段历史成本，前端以空态或不足数据说明展示。

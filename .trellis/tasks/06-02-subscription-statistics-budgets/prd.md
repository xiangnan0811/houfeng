# 统计与预算

## Goal

Cost facts, CNY cost calculation, overview/statistics APIs, budget scopes and budget status.

## Requirements

- 订阅事实只保守扩展展示名、成本分类、标签、试用/固定期结束等账单事实字段。
- 后端统一计算基准货币月/年成本、汇率来源、汇率日期、stale 状态。
- 预算支持 global/provider/label/category/vps scope，比较使用基准货币。
- Overview/statistics API 需要提供总成本、未来续费、预算风险、供应商/币种/分类/VPS 拆分和缺失订阅资产。

## Acceptance Criteria

- [ ] `GET /api/subscriptions` 保持旧字段兼容并追加成本/汇率/预算/提醒派生字段。
- [ ] `GET /api/subscriptions/overview` 与 `GET /api/subscriptions/statistics` 返回多角度成本统计。
- [ ] `GET/POST/PATCH /api/subscription-budgets` 可管理预算并影响预算状态。
- [ ] VPS 的 `renewal_decision` 仍是用户侧业务决策来源，订阅状态不变成第二套决策状态。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.

# 前端工作台与跨模块集成

## Goal

Subscriptions workbench, VPS detail cost card, Asset Decisions signals, Dashboard summary and frontend tests.

## Requirements

- `/subscriptions` 升级为成本工作台，包含总览、续费队列、订阅明细、预算、汇率与提醒设置，同时保留创建/编辑订阅流程。
- VPS 详情展示成本卡片：原币种价格、基准货币月/年成本、下次续费、预算状态、提醒状态、汇率状态。
- Asset Decisions 增加成本信号：临近续费、超预算、缺订阅、取消/迁移但仍可能续费。
- Dashboard 只展示高信号摘要：总成本、未来 14 天续费、预算风险、汇率异常，不承载完整订阅分析。

## Acceptance Criteria

- [ ] 前端类型与 API helper 覆盖新增订阅成本接口和字段。
- [ ] `/subscriptions` 页面支持多币种展示、预算风险、汇率异常、提醒设置和订阅创建/编辑。
- [ ] `/vps/:id`、`/asset-decisions`、Dashboard 均能显示成本信号且不改变 VPS-first 主线。
- [ ] 前端 lint、Vitest、build 通过；视觉 sanity 可执行时覆盖桌面和移动核心路由。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.

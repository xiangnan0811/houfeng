# 前端视觉重构与统计卡片统一

## Goal

覆盖当前 visual-redesign 分支的前端改动：统一 StatCard/SectionTitle 原子、重构 Dashboard 工作台概览、迁移 Events/Targets/MonitoringHero 统计卡片、补充测试与浏览器 sanity 证据。

## Background

当前 `visual-redesign` 分支已经完成一轮前端视觉重构和多轮代码审查修复。Trellis 任务需要覆盖这些实际业务改动，而不是只覆盖小型项目记忆更新。

## Requirements

- 新增可复用的 `StatCard` atom，用于跨 Dashboard、Events、Targets、MonitoringHero 的统计卡片展示。
- 新增可复用的 `SectionTitle` atom，用于 Dashboard 面板标题、count chip 和右侧 action slot。
- Dashboard 从旧 `.wb-*` 工作台布局迁移到 atom-based 响应式概览，保留核心数据：异常监控、续费、成本、预算风险、关注队列、观测事实、动态、账单事实、经验记录和资产总览。
- Dashboard 关注队列中只有实际可导航的行携带 `dash-att--clickable`；无点击行为的空态和异常摘要行不得伪装为可点击项。
- 交互型 `StatCard` 必须渲染原生 `<button type="button">`，不能使用裸 `div onClick`。
- Dashboard 健康状态 badge 必须复用 `dashboardHelpers.statusTone`，保持 “关注/告警/严重/维护中/正常” 的语义色一致。
- 抑制客户端路由切换时的非必要入场动画重放，但不引入新的前端依赖。
- 为新增 atom 和 Dashboard 行为调整补充/更新测试。
- 记录浏览器 sanity 证据：Dashboard 正常渲染、无错误态/异常，`dashAtt:5`、`dashAttClickable:2`。

## Constraints

- 不引入 Playwright/Cypress/WebDriverIO 到 `web/package.json`。
- 不回滚用户已有的 `visual-redesign` 工作树改动。
- 不直接提交、合并或改写 `main` / `master`。
- 项目记忆/规范更新可以随本分支保留，但不是独立 Trellis 任务范围。

## Acceptance Criteria

- [x] `StatCard` / `SectionTitle` atom 已新增并从 atoms barrel 导出。
- [x] Dashboard、Events、Targets、MonitoringHero 已迁移到共享 `StatCard`。
- [x] Dashboard 可点击关注行和非交互行的 class 语义区分正确。
- [x] `StatCard` 交互态使用原生 button，并有单测覆盖。
- [x] `SectionTitle` title/count/action/className 行为有单测覆盖。
- [x] `DashboardPage.test.tsx` 更新后仍覆盖预算风险关注项跳转。
- [x] 前端质量门已通过：`make verify-web`。
- [x] 浏览器 sanity 证据已确认：`dashAtt:5`、`dashAttClickable:2`。
- [x] 最终提交/归档前再次确认工作树范围，避免把无关改动混入任务提交。

## Out of Scope

- 不做新的后端/API contract 改动。
- 不引入正式 e2e/视觉回归框架。
- 不提交截图或 screenshot manifest。

## Notes

- 该任务是对当前分支已完成实现的 Trellis 补录和后续收尾载体。
- 小型 project memory 更新已写入规范，但不再单独建任务。

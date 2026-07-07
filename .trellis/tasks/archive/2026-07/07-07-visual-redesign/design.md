# 前端视觉重构与统计卡片统一 - Design

## Scope

本任务覆盖 `web/` 前端展示层重构：新增共享 atom，重构 Dashboard 概览布局，并将多个页面的统计卡片统一到同一组件模式。没有后端数据契约变更。

## Component Boundaries

- `web/src/components/atoms/StatCard.tsx`
  - 负责统计值、label、sub slot 和 tone 展示。
  - 非交互态复用 `Card`。
  - 交互态直接渲染 `<button type="button">` 并保留 `card card--default` 视觉 class，保证键盘和屏幕阅读器语义。
- `web/src/components/atoms/SectionTitle.tsx`
  - 负责 section title、可选 count chip、可选右侧 action slot。
  - 只做结构封装，不承载业务导航逻辑。
- `DashboardPage.tsx`
  - 消费 shared atoms，保留本页的数据装配、导航和展示策略。
  - 健康状态 tone 复用 `dashboardHelpers.statusTone`。
- `AppBoot.tsx`
  - 首次 paint 后给 `body` 添加 `app-booted`，配合 CSS 抑制后续路由切换中的 `.animate-in` 重放。

## Styling Boundaries

- 当前分支沿用现有 `web/src/index.css` 集中样式现状，新增 `.stat-*`、`.dash-*`、`.section-*` 规则。
- `.dash-att` 是通用行容器，不表达点击能力。
- `.dash-att--clickable` 是唯一点击语义 modifier，负责 cursor 和 hover。
- `.stat-grid` 提供跨页响应式统计卡片布局。

## Behavioral Contracts

- 只有带 `onClick` 的 `StatCard` 才暴露 button role。
- Dashboard 中没有导航行为的关注项不能有 `dash-att--clickable`。
- Dashboard retry 只在用户点击重试时设置 loading，不在 `loadData()` 内同步 setState，避免 React Hooks lint 对 effect 内同步 setState 的告警。
- Browser sanity 对 Dashboard 点击语义的关键计数是 `dashAtt:5`、`dashAttClickable:2`；实际渲染中只有两个 onClick 行可点击。

## Compatibility

- 不新增 npm dependency。
- 不改变 API 请求形状。
- 原有 page tests 继续使用 jsdom + fetch mock，不引入真实 browser test 到 CI。


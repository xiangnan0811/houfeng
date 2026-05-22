# 前端第一阶段 UI 视觉优化

## Goal

在不改变功能、路由、API 调用和组件结构的前提下，对 `web/src` 内现有候风 v2 前端视觉做第一阶段收敛：保留“玄夜青 + 晨晖金”的候风气质，去除明显 AI dashboard 感的大面积渐变、aurora、玻璃拟态和强 glow，让界面更像沉稳的服务器舰队工作台。

## Requirements

* 仅修改 `web/src` 范围内前端代码，优先修改 `tokens.css`、`atoms.css`、`pages.css`、`layout.css`、`filters.css`、`LoginPage.css`。
* 不修改后端代码，不修改 API 调用逻辑，不改变路由结构。
* 不引入新的 UI 框架或大型依赖。
* 不推翻现有主题变量，不删除 `houfeng-dark` / `houfeng-light` / `classic-dark` / `classic-light`。
* 保留现有 `className`，避免破坏测试和组件结构。
* 主要通过 CSS 变量和现有样式层级弱化装饰，不硬编码大量新颜色。
* 降低阴影、hover 上浮、发光边缘、卡片 glow、玻璃感和背景雾气。
* 保留 Sidebar、TopBar、GlobalCriticalAlert、GlobalSearch、按钮、筛选、Drawer、表格等现有交互能力。

## Acceptance Criteria

* [ ] 页面不再出现明显 AI 生成感的渐变雾气或玻璃拟态。
* [ ] 深色主题仍保持候风 v2 的“玄夜青 + 晨晖金”气质。
* [ ] Dashboard、Nodes、VPS、Asset Decisions、Login 页面视觉风格统一。
* [ ] 交互能力不丢失，按钮、筛选、Drawer、表格、GlobalSearch 仍正常工作。
* [ ] 移动端断点不被破坏。
* [ ] 前端 lint、测试、构建通过；优先运行 `make verify-web`。

## Definition of Done

* CSS 修改限制在用户指定的前端范围内。
* 使用现有 CSS variables 表达新视觉方向。
* 验证命令完成并记录结果。

## Out of Scope

* 后端、数据库、API 合同、路由和状态管理改动。
* 新 UI 框架、设计系统重写、组件重构或 className 重命名。
* 删除现有主题或大规模替换候风 v2 品牌色。
* 将登录页或 Dashboard 重新设计成全新信息架构。

## Technical Notes

* 当前任务明确，默认不向用户追加问题。
* 需要先读取前端视觉规范与现有 CSS，再做局部视觉收敛。

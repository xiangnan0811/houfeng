# 前端质量 ratchet 与浏览器门

## Goal

把本轮发现转为持续执行的 coverage、浏览器、CSP、可访问性、bundle/CSS 和规范门，防止相同问题在后续视觉或结构调整中复发。

## Confirmed Facts

- 74 files / 578 tests 全绿仍未发现错误计数、嵌套 Modal、CSP、移动裁切、Tabs 键盘和 required 丢失。
- 当前无 coverage、Playwright、axe、bundle budget 或 browser console gate。
- Trellis web specs 仍引用已不存在的 CSS/layout 路径。
- mock 浏览器审查不能替代真实认证 Center/PostgreSQL 与慢请求/大数据验证。

## Requirements

- coverage 先记录真实基线再 ratchet，不设置任意全局 80%；高风险模块 branch 至少 90%。
- Playwright 覆盖 core routes、PageState、a11y/keyboard、responsive visual、CSP/console。
- CI 固化入口 JS/CSS gzip、最大 route chunk、字体量和 CSS AST budget。
- 类型安全按 lib → atoms → dashboard → routes 扩展，禁止全局忽略。
- 更新真实 Trellis web specs，并在真实 staging 留存可审计证据。

## Dependency And Scope

- 依赖 CSP、accessibility、responsive、CSS owner tasks；复用其稳定 contracts。
- API 生成不在范围内；高风险 response 继续使用共享 fixture/contract tests。

## Acceptance Criteria

- [ ] coverage、Playwright/axe、CSP/console、bundle/CSS gates 在 CI 执行。
- [ ] 修改或新增的高风险模块 branch coverage 不低于 90%，全局不低于记录基线。
- [ ] Node/CSP/Modal/Dashboard/CSS/browser 规范与真实目录和命令一致。
- [ ] 真实认证 staging smoke 覆盖登录、五状态、嵌套确认、设置保存、503/慢请求、长文本/大列表与主题。

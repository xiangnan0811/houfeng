# 复审 UI 设置修复并补 Trellis 流程

## Goal

对当前分支的大量前端/UI 设置相关修改做一次补充复审，确认上一轮发现的问题已被修复，继续查找可能遗漏的新问题；若发现真实问题，直接在同一分支修复并补回归测试。由于此前修复未按 Trellis 任务流程启动，本任务同时补齐 PRD、设计、实施计划、执行记录与最终质量门证据。

## Requirements

- 保持当前分支 `fix/review-findings-ui-settings` 上的工作，不直接修改 `main` / `master`。
- 复审 staged 变更，重点覆盖：
  - CSS 现代化与登录页布局；
  - Settings 订阅保存与预算保存的交互一致性；
  - watchtower 标题 pseudo element 和 section title 装饰定位；
  - 新增样式合同测试、设置页回归测试、构建配置与依赖变化。
- 若复审发现问题，必须先确认根因，再最小范围修复，并补能复现问题的自动化测试或 CSS 合同测试。
- 对用户可见 UI 修改进行本地浏览器 sanity，至少覆盖 `/login` 的桌面与窄屏视口，记录证据边界。
- 执行前端完整质量门 `make verify-web`，并报告 Node engine 警告等环境限制。
- 不提交 commit，除非用户单独授权。

## Acceptance Criteria

- [ ] Trellis 任务工件齐备：`prd.md`、`design.md`、`implement.md`。
- [ ] 当前分支复审完成，发现的问题均已修复，或明确说明未发现阻塞问题。
- [ ] 相关回归测试覆盖本轮修复点。
- [ ] `make verify-web` 通过。
- [ ] `/login` 浏览器 sanity 覆盖 `375x667` 与 `1440x900`，无横向溢出，登录卡片与页脚布局正确。
- [ ] 最终报告列明：分支、主要修复、验证命令、浏览器 sanity、环境警告、是否提交。

## Notes

- 本任务是对已发生修复的补流程和补复审；规划工件用于固化范围和验收，不扩大到 PR 创建、提交或发布。

# 前端窄视口核心流程

## Goal

确保 390px 及窄容器下关键命令完整可见、可读、可操作，并把横向 overflow 限制在具有明确 affordance 的局部区域。

## Confirmed Facts

- Settings Tabs 在 390px 逐字折行。
- Asset Decisions 辅助入口与 Provider “组合决策”文字被主动裁切。
- 同一 Asset nav selector 在多个 breakpoint 存在矛盾覆盖。
- Dashboard 四卡问题由 Task 3 负责，表格与命令 overflow 仍需统一策略。

## Requirements

- tabs 使用单行横向滚动且 tab 文本不折行。
- 命令文字不得用 ellipsis/hidden 代替可见信息；badge 可换行但目标高度不少于 40px。
- 数据表只能在带可聚焦 label 的局部 scroll region 横向滚动，heading/toolbar 保持固定。
- 核心路由无 document 横向溢出，sticky/fixed 元素不遮挡最后字段。

## Dependency And Scope

- 依赖 `frontend-dashboard-trust` 与 `frontend-accessibility-contracts`。
- 不在本任务进行 CSS owner 大规模重构，仅修当前规则所有者。

## Acceptance Criteria

- [ ] 390x900 下“监控策略”“场景与组合”“组合决策”完整可见可操作。
- [ ] Settings/Asset/Providers/Dashboard 无 document 横向溢出。
- [ ] 表格局部滚动区域有可访问名称和键盘入口。
- [ ] Dashboard 主行动在 390x900 首屏内。

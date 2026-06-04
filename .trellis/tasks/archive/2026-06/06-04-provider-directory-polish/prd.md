# 服务商页二次信息架构修正

## Goal

Refine the provider directory table and edit modal after user review: simplify display, move actions into entry column, click provider name to edit, and align the modal with v2 UI.

## Requirements

- 表格区标题改为“服务商与入口”。
- 账号、评分、备注、标签等可选资料只在用户填写后展示；未填写时不展示“未记录 / 未评分 / 未标记”等占位文案。
- 移除操作列；点击服务商名称打开编辑弹窗。
- 服务入口列只展示真实入口动作：官网、面板、VPS、订阅；不展示面板是否填写、网站是否填写等状态解释。
- 外部口碑列只保留口碑入口，不展示“入口，不代表我的评分”等解释文案。
- 资产上下文列只展示 `M VPS · N 订阅`。
- 标签和备注从服务商名称列拆出为独立列，放在更新时间列之前；标签在上，备注换行并截断超长文本。
- 编辑服务商弹窗需要更紧凑、更统一，避免大块嵌套卡片和过宽输入布局。

## Acceptance Criteria

- [ ] `/providers` 表格包含“服务商与入口”标题，不再出现“目录与入口”。
- [ ] 无可选资料时，列表不出现“账号未记录”“未评分”“未标记”“缺面板入口”“入口，不代表我的评分”等解释性占位。
- [ ] 表格不再有“操作”列；服务商名称可点击并打开编辑弹窗。
- [ ] 官网、面板、VPS、订阅入口在服务入口列展示，并保持正确链接。
- [ ] 资产上下文仅显示 VPS 和订阅数量。
- [ ] 标签 / 备注独立列能展示已填写内容，备注超长时视觉截断。
- [ ] 创建 / 编辑 payload、评分校验、取消重置行为不回退。
- [ ] ProvidersPage 相关测试、lint、build 通过；本地视觉核查桌面和窄屏无明显拥挤/空白失衡。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.

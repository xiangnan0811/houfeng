# Asset Decision Modal Simplification Design

## Root Cause

上一轮修复只替换了部分旧标题和区块名称，但默认详情流仍然把多个不同目的的内容直接铺开：

- 组级判断；
- 成员全量取舍卡；
- 保存记录表单；
- 原始宽表格；
- 单台处理面板；
- 自定义组合编辑与成员维护表单；
- 保存记录执行编排和成员跟进；
- 模板创建表单和成员蓝图。

这些内容都可能有价值，但不应同时出现在默认 modal。当前问题的根因是“默认状态没有信息层级和披露边界”，而不是某个标题或某段文案。

## Target Interaction Model

每个详情 modal 使用同一模式：

1. **Overview**：默认渲染，回答“当前判断是什么、我现在能做什么”。
2. **Secondary actions**：一排低权重按钮或 segmented controls，进入二级内容。
3. **Panel content**：只有用户选择某个二级入口后才渲染宽表、表单、执行编排或成员维护。

二级状态仅为前端本地 UI 状态，不进入 URL；现有 URL deep link 继续只决定打开哪个 modal。

## State Model

在 `AssetDecisionsPage` 中新增四个本地 state：

- `groupDetailPanel: 'overview' | 'members' | 'save' | 'raw' | 'vps'`
- `manualDetailPanel: 'overview' | 'edit' | 'members' | 'add' | 'save' | 'raw'`
- `recordDetailPanel: 'overview' | 'execution' | 'members' | 'source' | 'raw'`
- `templateDetailPanel: 'overview' | 'create' | 'members' | 'status'`

打开/关闭不同 modal 时重置到 `overview`。触发原有动作时同步切换：

- 点击自动组“保存为决策记录”先创建 `recordDraft`，再切到 `save`。
- 点击自动组某成员“处理”设置 `selectedVPS`，再切到 `vps`。
- 点击自定义组“保存为决策记录”切到 `save`。
- 点击记录“复核来源”继续沿用现有打开来源逻辑。
- 模板归档/启用确认只在 `status` 面板显示。

## Rendering Changes

### Automatic Group Modal

默认 overview：

- summary strip；
- `renderDetailCommand`，但 body 降噪：只保留主建议、证据 tier、最多 3 个 chips / tradeoffs；
- `renderMemberDecisionPreview`：最多 3 条成员摘要行，不渲染全量成员大卡；
- secondary panel launcher：成员明细、保存记录、原始明细。

二级 panels：

- `members`：渲染完整成员取舍卡；
- `save`：渲染保存记录表单；
- `raw`：渲染 DataTable；
- `vps`：渲染 `AssetDecisionWorkPanel`。

### Manual Group Modal

默认 overview：

- summary strip；
- current judgement / readiness；
- 最多 3 条成员摘要；
- secondary panel launcher：编辑组合、成员维护、添加成员、保存记录、原始明细。

二级 panels：

- `edit`：组合场景表单；
- `members`：完整成员卡和每个成员的编辑/移除控件；
- `add`：新增成员表单；
- `save`：保存记录表单；
- `raw`：DataTable。

### Record Modal

默认 overview：

- summary strip；
- saved evidence compact；
- goal / status summary；
- secondary panel launcher：执行跟进、成员跟进、来源复核、原始成员。

二级 panels：

- `execution`：状态更新、执行编排；
- `members`：成员跟进表单；
- `source`：来源连续性和复核来源按钮；
- `raw`：成员 DataTable。

### Template Modal

默认 overview：

- summary strip；
- template goal；
- status badge and high-level controls；
- secondary panel launcher：创建组合、成员蓝图、状态维护。

二级 panels：

- `create`：从模板创建组合表单；
- `members`：成员蓝图；
- `status`：归档/重新启用确认。

## Component / Helper Strategy

This remains a scoped page refactor to avoid broad route/component churn:

- Keep API calls and mutation handlers in `AssetDecisionsPage`.
- Add small local render helpers for:
  - secondary panel launcher buttons;
  - compact member preview rows;
  - reusable detail section wrapper.
- Do not introduce new dependencies.
- CSS updates stay in `web/src/index.css` and reuse existing tokens.

## Testing Strategy

Tests must prove the default state and the expanded state separately:

- Default modal assertions forbid tables/forms/execution blocks and explanation text.
- Expansion assertions click panel launchers and verify the old capabilities still appear.
- Mutation tests keep existing payload checks.
- Browser checks verify the actual rendered modal in desktop and mobile viewports, including no document/body horizontal overflow.

## Compatibility

- No API, schema, or route changes.
- Existing deep links still open the same modal.
- Existing write payloads remain unchanged.
- Existing labels for domain facts remain honest; only default disclosure changes.

## Risks

- `AssetDecisionsPage.tsx` remains large; this task intentionally avoids a full component extraction to reduce delivery risk.
- Test updates are extensive because previous tests assumed default visibility of too many details.
- Browser validation must cover all modal types, not just one screenshot case.

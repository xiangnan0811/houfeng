# 资产决策详情弹窗重构实施计划

## Phase 1: Failing Tests

- 修改 `web/src/pages/AssetDecisionsPage.test.tsx` 中自动组详情用例：
  - 断言 `场景推进建议` 不存在。
  - 断言 `证据矩阵 / 取舍对比` 不存在。
  - 断言关键事实仍存在：成员名、角色/动作、成本/承载/监控、`创建自定义组合`、`保存为决策记录`、`处理`。
- 修改自定义组详情相关用例：
  - 断言 `组合推进状态` 不存在。
  - 断言 `自定义组合证据矩阵` 不存在。
  - 断言 readiness/意图/保存动作仍存在。
- 修改记录来源复核用例：
  - 打开来源组时同样禁止旧自动组标题。
- 修改记录详情用例：
  - 若存在快照矩阵断言，改为紧凑保存时判断摘要断言。
- 运行：
  - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
  - 预期失败，失败点指向旧标题仍在 DOM 或新结构尚未实现。

## Phase 2: Automatic Group Detail

- 在 `web/src/pages/AssetDecisionsPage.tsx` 新增 detail helper：
  - `renderDetailCommand`：统一渲染组级结论、badge/chips 和 action slot。
  - `renderMemberDecisionCards`：从 `ComparisonMatrixMember[]` 渲染紧凑成员卡。
- 替换自动组 Modal 内旧顺序：
  - 保留 summary。
  - 删除 `asset-decision-progression-branch` 默认区。
  - 删除默认 `renderComparisonMatrix(...)`。
  - 插入 detail command 和 member decision cards。
  - 保留保存表单、原始表格、单台处理面板。
- 跑专项测试并修到通过。

## Phase 3: Manual Group Detail

- 复用 `renderDetailCommand` 和 `renderMemberDecisionCards`。
- 删除默认 `asset-decision-progress-panel` 与 `renderComparisonMatrix(...)`。
- 在 command 区保留 readiness 信息和主要动作。
- 在成员卡显示 intended role/action 和 intent mismatch。
- 跑专项测试并修到通过。

## Phase 4: Record Detail

- 改 `renderRecordSavedEvidence`：
  - 不默认调用完整 `renderComparisonMatrix`。
  - 渲染紧凑保存时判断摘要、证据 tier、lane counts、tradeoff chips。
  - 无 insight 时保留 fallback，但减少解释文案。
- 保持执行编排和成员表格。
- 跑专项测试并修到通过。

## Phase 5: CSS

- 在 `web/src/index.css` 中追加：
  - `.asset-decision-detail-command`
  - `.asset-decision-detail-command__main`
  - `.asset-decision-detail-command__actions`
  - `.asset-decision-member-decisions`
  - `.asset-decision-member-card`
  - `.asset-decision-member-card__head`
  - `.asset-decision-member-card__facts`
  - `.asset-decision-member-card__signals`
  - `.asset-decision-member-card__actions`
- 调整 modal body overflow：
  - 避免整段内容因为宽表格导致主要弹窗横向滚动。
  - 横向滚动只保留在 `.asset-table-scroll`。
- 添加 920px/640px 响应式规则，成员卡单列显示。

## Phase 6: Verification

- `git diff --check`
- `cd web && npm run lint`
- `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- 启动本地 web preview/dev server，用 mock API 跑 `/asset-decisions` 浏览器 sanity：
  - 桌面 1440x1000；
  - 移动 390x900；
  - 打开自动组详情；
  - 检查无页面级横向溢出；
  - 检查旧标题不在默认弹窗中；
  - 截图或 DOM evidence 记录到 check notes。

## Rollback

- 若新成员卡无法覆盖必要事实，回滚到保留原表格但仍禁止默认解释矩阵；不要恢复旧“场景推进建议/证据矩阵”首屏结构。

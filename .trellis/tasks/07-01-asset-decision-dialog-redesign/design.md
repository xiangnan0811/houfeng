# 资产决策详情弹窗重构设计

## Root Cause

上一轮只重构了 `/asset-decisions` 默认页的信息架构，深层详情弹窗仍保留旧结构。当前自动组详情在首屏内顺序渲染：

- 四个摘要块；
- “场景推进建议”说明区；
- “证据矩阵 / 取舍对比”大矩阵；
- 推荐、评分条、证据 chips、动作按钮；
- 保存表单；
- 宽表格；
- 单台处理抽屉。

自定义组详情也有同类问题：“组合推进状态”说明区、“自定义组合证据矩阵”、表单、宽表格混在同一滚动流。记录详情相对更有执行语义，但“保存时判断依据”仍嵌套快照矩阵，存在同类文字密度风险。

现有测试还显式断言这些旧标题存在，因此测试本身也在保护混乱结构。

## Design Principles

- 详情弹窗是“决策工作台”，不是说明文档。
- 首屏只回答三件事：当前结论、关键事实、下一步行动。
- 成员比较用可扫描的行/卡表达，不默认展开成大矩阵。
- 解释性文字改为标签、短句、可折叠 secondary 区或直接删除。
- 宽表格只作为“原始明细”，必须放在首屏之后，并且不能成为主要阅读路径。
- 自动组、自定义组、记录详情使用相同视觉语法：summary strip + decision lead + member decision list + optional raw details.

## Target IA

### 自动组详情

默认内容顺序：

1. `asset-decision-detail__summary`
   - 保留紧凑四项事实：VPS、成本、业务上下文、证据质量。
   - 文案必须短，不解释指标背景。
2. 新增 `asset-decision-detail-command`
   - 左侧：组级主判断，使用 `comparison_insight.summary` 或 `decision_recommendation.next_step`。
   - 右侧：主要行动按钮：`创建自定义组合`、`保存为决策记录`。
   - 同区显示证据质量 badge 和少量 evidence chips，limit 默认 4。
3. 新增 `asset-decision-member-decisions`
   - 每个成员一张横向 decision row/card。
   - 展示：成员名、建议角色/动作、成本/产品、状态/续费、承载/监控、风险/缺口 chips、处理按钮。
   - 复用 `ComparisonMatrixMember` 派生函数，避免重新拼接业务事实。
4. 保存表单按用户点击后展开。
5. 原始成员表格保留，但标题/aria 明确为“原始明细”，放在成员卡之后。
6. 删除默认渲染的“场景推进建议”和“证据矩阵 / 取舍对比”两大区块。

### 自定义组详情

默认内容顺序：

1. 紧凑 summary。
2. `asset-decision-detail-command`
   - 展示 readiness badge、目标/来源短句和主要动作：保存组合、保存为决策记录、另存为模板。
3. `asset-decision-member-decisions`
   - 使用 `manualMemberComparisonMatrixMember`。
   - 额外展示“意图匹配/需复核意图”和成员意图。
4. 组合场景表单和成员编辑表单保留，但放在首屏行动区之后。
5. 删除默认“组合推进状态”和“自定义组合证据矩阵”大区块；必要状态以 readiness badge 和每个成员的意图状态表达。

### 记录详情

记录详情保留执行导向结构，但降低快照矩阵默认权重：

1. 保留 summary 和 goal/status。
2. `renderRecordSavedEvidence` 不再默认渲染完整 `renderComparisonMatrix`。
3. 改为紧凑快照摘要：证据 tier、保存时主判断、lane counts、少量 tradeoff chips。
4. 成员执行编排仍是记录详情主要工作区。
5. 原始成员宽表保留在执行编排之后。

## Component Boundaries

当前文件 `web/src/pages/AssetDecisionsPage.tsx` 已过大，但本次先做局部提取，避免大范围重构引入新风险：

- 在同文件新增小型 render helpers：
  - `renderDetailCommand(...)`
  - `renderMemberDecisionCards(...)`
  - `renderCompactSavedEvidence(...)`
- 继续复用既有 `ComparisonMatrixMember`、`groupMemberComparisonMatrixMember`、`manualMemberComparisonMatrixMember`、`recordMemberComparisonMatrixMember`。
- CSS 只追加/调整 `.asset-decision-detail-command`、`.asset-decision-member-decisions`、`.asset-decision-member-card` 等类。
- 不改 API，不改类型，不引入依赖。

## Compatibility

- URL deep links 保持不变：`group_id`、`manual_group_id`、`record_id`、`template_id`。
- 保存记录、创建自定义组合、保存组合、另存模板、成员处理入口保持可用。
- 宽表格仍存在，避免丢失审计型明细。
- 旧测试中断言旧标题的部分必须反向改为 forbidden assertions。

## Risks

- `AssetDecisionsPage.tsx` 仍很大，局部 helper 会继续增加文件复杂度。此次只处理用户可见混乱，后续可另开任务拆分详情组件。
- 浏览器 visual sanity 只能证明特定 mock 数据下的布局；因此测试要覆盖不同 detail 类型的旧标题缺失和关键事实保留。

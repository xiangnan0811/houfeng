# Asset Decision Page Deep Declutter Design

## Root Cause

上一轮修复只建立了“重内容放二级面板”的结构，但默认层本身仍然没有预算。默认层仍然展示摘要卡、判断、评分条、风险标签、成员摘要、二级入口说明和多个行动按钮。结果是宽表和表单虽然不再默认出现，但首屏仍像报告页。

本次设计把默认层定义成“决策控制面”，而不是“证据报告”。

## Alternatives Considered

### A. Continue moving more content into existing panels

优点：改动较小。

缺点：不能解决默认层自身的文字、badge、score bar 过载。上一轮已经证明这个方向不足。

### B. Replace default layer with a compact decision contract

优点：直接解决默认体验；可复用到主页面列表和所有详情弹窗；不改 API。

缺点：需要重写一部分 render helper 和测试断言。

### C. Full page redesign with new component extraction

优点：长期结构最好。

缺点：当前 `AssetDecisionsPage.tsx` 很大，整页拆组件会扩大风险，容易混入非本任务重构。

## Decision

采用 B：在当前页面内建立 compact decision contract，并只做必要的 helper / CSS 调整。不做全量组件拆分，不改 API。

## Information Architecture

### Main Page

主页面变成三层：

1. **Current Priority Strip**：当前最重要事项 + 3-4 个核心数字 + 主动作。
2. **Surface Switcher**：记录、场景、续费、单台队列的轻量入口；每个入口只显示名称、数量、状态。
3. **Decision Queue**：自动组列表是队列行，不是证据报告。

自动组行默认保留：

- `P1` / `P2`；
- 标题；
- 1 行短原因；
- 最多 2 个状态/风险 chips；
- 3 个事实字段：成员数、成本、证据/风险；
- 主按钮 `查看组`。

默认移除或降级：

- `NEXT STEP` 块；
- `COMPARISON` 块；
- score bars；
- 全量 evidence chips；
- 多行承载/成本/监控说明。

### Detail Modal Default

所有详情 modal 默认使用同一骨架：

1. **Header**：标题 + 最多 3 个状态 chips + close。
2. **Fact Rail**：4 个短事实卡，不含完整说明句。
3. **Decision Lead**：一句主判断 + 最多 2 个风险 chips + 主动作。
4. **Key Objects**：最多 2 条成员/记录/模板对象短行。
5. **Icon-like Panel Nav**：二级入口只有短 label 和 count，不显示 summary 小字。

默认不显示：

- score bars；
- 英文 eyebrow；
- 长段说明；
- 完整成员卡；
- 表格；
- 表单；
- 执行编排；
- source continuity 大块。

### Secondary Panels

保留已有二级能力：

- 自动组：成员明细、保存记录、数据底稿、VPS 处理。
- 自定义组合：编辑组合、成员维护、添加成员、保存记录、数据底稿。
- 保存记录：执行跟进、成员跟进、来源复核、成员底稿。
- 模板：创建组合、成员蓝图、状态维护。

## Copy Budget

默认层文案预算：

- 主判断：最多约 40 个中文字符。
- 风险 chips：最多 2 个。
- 成员短行：最多 2 条，每条最多“名称 / 角色动作 / 一句原因”。
- 面板入口：只允许 label + count，不允许说明小字。
- 默认层不展示 `GROUP DECISION`、`MEMBERS`、`NEXT STEP`、`COMPARISON` 这类英文 eyebrow。

## Visual Direction

资产决策是运维工作台，不是营销页。视觉方向应是安静、紧凑、可扫视：

- 少卡片嵌套；
- 少描边和低价值标签；
- 关键风险用色明确，但不堆满整屏；
- 主动作突出，次动作收敛；
- 移动端优先纵向扫描，不让评分条占据首屏。

## Compatibility

- 不改后端 API。
- 不改 URL deep link。
- 不新增依赖。
- 不删除能力，只改变默认披露层级。

## Testing Strategy

- 扩展 `AssetDecisionsPage.test.tsx`：
  - 默认自动组详情不显示 score bars、英文 eyebrow、支撑/风险/缺口说明、完整成员卡、panel summary。
  - 默认主页面组列表不显示 `NEXT STEP`、`COMPARISON` 报告块。
  - 二级入口仍展开原能力并保持写 API payload。
- 浏览器验证：
  - `1440x1000` 和 `390x900`；
  - 自动组至少覆盖 cancel、renewal、cost、evidence 四类；
  - 记录、自定义组合、模板、续费、单台队列默认展示；
  - 检查 document/body 无横向溢出；
  - 记录默认层 badge/button/text 数量明显低于当前审查基线。

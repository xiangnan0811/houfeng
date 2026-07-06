# 技术设计：资产决策页面设计重置与弹窗组件化

## 边界

本设计只涉及 `web/` 前端，不改后端 API / DB schema / 写入合同。关联文件：

- `web/src/pages/AssetDecisionsPage.tsx`（3585 行，主重构目标）
- `web/src/pages/asset-decisions/components/`（PortfolioWorkbench / SecondaryWorkbenches，已有）
- `web/src/pages/asset-decisions/{types,constants,utils,formatters,businessLogic,renderHelpers,tableColumns}.{ts,tsx}`（已有模块）
- `web/src/components/AssetDecisionWorkPanel.tsx` / `AssetDecisionRenewalTable.tsx`
- `web/src/components/VPSCancellationWorkbench.tsx`（关联文案精简）
- `web/src/styles/pages.css`（CSS 类名收敛）
- `.trellis/spec/web/component-conventions.md`（spec 清理）
- `docs/design/current/component-patterns.md`（设计契约补充）

## P0: spec 清理

### 目标文件

`.trellis/spec/web/component-conventions.md` 的"详情页 IA 合同"段落。

### 改动

将以下 8 条资产决策专属补丁规则合并删除：

1. "资产组合决策页面主体必须决策优先、稳定静默"
2. "资产决策详情默认层必须是决策封面，不是压缩报告"
3. "资产决策弹窗必须使用不透底内容面"
4. "资产决策二级面板必须是单任务短面板"
5. "资产决策成员数组默认必须预览限量"
6. （及 sub-bullet 中关于 comparison_insight.summary / 英文 eyebrow / marker 的禁令）

替换为 1 条通用契约：

```markdown
### 决策类页面信息层级契约（适用资产决策、VPS/Target 详情等）

1. 默认层只回答一个问题："现在最该处理什么？"——一个主判断 + 一个主动作。
2. 次级层是扫描列表，每项一行：身份 + 状态 + 单一入口，无解释句。
3. 详情层是弹窗，弹窗内 ≤3 个 Tab，每个 Tab 单一任务。
4. 底稿层是原始数据，默认折叠，显式进入。
5. 稳定态静默：无待办时一行稳定提示，不渲染 CTA / 警示色 / 统计卡。
6. 文案零解释：无说明性段落；eyebrow 全中文或去除；字段含义靠标签自解释。
7. 弹窗内容面不透明（var(--surface-elevated)），overlay 半透明。
8. 成员数组默认预览 ≤3 行 + "查看全部"跳底稿；写入 payload 仍用全量。
```

保留"迁移意向""危险联动""低频报告独立页"等与具体业务安全边界相关的规则（这些不是 UI 补丁）。

## P1: 弹窗组件化提取

### 新增目录结构

```
web/src/pages/asset-decisions/modals/
├── GroupDetailModal.tsx        ← 自动组详情（原行 2796-2957）
├── ManualGroupDetailModal.tsx  ← 自定义组合（原行 2959-3290）
├── TemplateDetailModal.tsx     ← 场景模板（原行 3292-3464）
├── RecordDetailModal.tsx       ← 保存记录（原行 3485-3582）
└── RenewalDecisionModal.tsx    ← 单台续费（原行 3466-3483）
```

### 组件接口契约

页面只持有 `openXxxID: string | null` 状态，弹窗组件自管数据拉取和内部 Tab/面板状态：

```tsx
type DetailModalProps = {
  open: boolean
  groupID: string | null        // 或 manualGroupID / templateID / recordID
  onClose: () => void
  onCreateManualGroup?: (sourceGroupID: string) => void  // 跨弹窗导航回调
  onOpenRecord?: (recordID: string) => void
  onNavigateToVPS?: (vpsID: string) => void
}
```

### 数据拉取策略

当前页面预拉全部数据（groups / manualGroups / templates / records / queue / renewals）再 props 传入。P1 阶段弹窗组件**自行调用 `lib/api.ts`** 拉取自身详情数据（group detail / manual group detail 等），页面只传 ID。

理由：
- 降低 `SecondaryWorkbenches` 的 26 props 耦合。
- 弹窗关闭时数据不占用页面状态。
- 与 `VPSCreateModal` / `MonitoringDetailPage` 的弹窗数据自管模式一致。

### 风险

- 弹窗打开时多一次请求（当前是预拉）。早期阶段可接受，且详情数据量小。
- 跨弹窗导航（自动组→创建自定义组合→保存记录）需页面协调。用回调链：`onCreateManualGroup` → 页面关闭当前弹窗 + 打开目标弹窗。

## P2: 页面 IA 三段式重排

### 目标 JSX 结构

```tsx
<div className="asset-decision-page">
  {/* ① 当前判断板 */}
  <HeroPanel variant="decision" tone={portfolioLead.tone}>
    <HeroPanel.Lead>
      <h1>{portfolioLead.title}</h1>
      <p>{portfolioLead.summary}</p>
      {portfolioLead.actionLabel && <Button onClick={onOpenLead}>{portfolioLead.actionLabel}</Button>}
    </HeroPanel.Lead>
    {portfolioLead.kind === 'stable' && <StableHint />}
  </HeroPanel>

  {/* ② 决策组扫描 */}
  <PagePanel variant="scan">
    <PagePanel.Header title="决策组扫描" tools={<RenewalWindowSelect />} />
    <Tabs items={workbenchTabs} />
    <DecisionGroupList groups={portfolioState.groups} onOpen={onOpenGroup} />
  </PagePanel>

  {/* ③ 辅助入口工具条 */}
  <AuxEntryBar
    items={secondaryNavItems}
    active={secondaryWorkbench}
    onOpen={onSetSelectedSecondaryWorkbench}
  />
  {secondaryWorkbench && (
    <PagePanel variant="aux">
      {renderSecondaryWorkbench(secondaryWorkbench)}
    </PagePanel>
  )}

  {/* 弹窗 */}
  <GroupDetailModal ... />
  <ManualGroupDetailModal ... />
  <TemplateDetailModal ... />
  <RecordDetailModal ... />
  <RenewalDecisionModal ... />
</div>
```

### 辅助入口工具条

桌面：一行紧凑按钮（图标 + 中文短标题 + 数量/状态），默认全收起。
移动：2×2 网格。
点击某项 → 展开对应单一面板（场景模板 / 自定义组合 / 保存记录 / 续费事实 / 单台辅助），其余收起。深链命中时自动展开对应项。

### 删除项

- 页面副标题 "从 VPS、订阅、服务、域名和监控证据中派生组合取舍。"
- 深链提示 inline-alert。
- `closedLoopPartialErrors` 的 inline-alert warn 移入判断板的事实条。

## P3: 文案精简

### 删除清单

| 位置 | 文字 | 处理 |
|---|---|---|
| 页面副标题 | "从 VPS、订阅…派生组合取舍。" | 删除 |
| 深链提示 | "旧链接已承接到单台辅助队列…" | 删除 |
| 处理面板空态 | "从左侧队列进入处理；已确认…" | 改"选择一台 VPS 开始" |
| VPSCancellationWorkbench 8 处 | "普通 CRUD 不会联动…""active 订阅不会自动勾选…"等 | 移入 title 属性 tooltip |
| 所有弹窗 eyebrow | DECISION/PORTFOLIO/RENEWAL/SCENARIO/WORKBENCH | 删除或改中文 |
| 弹窗确认对话框 | 多行"当前：…/操作后：…/不会修改…" | 压缩 1 句 + ActionConfirmationModal |

### ActionConfirmationModal 统一

`web/src/components/ActionConfirmationModal.tsx` 已存在（2.6KB）。替换弹窗内嵌的 `role=alertdialog` section：

```tsx
<ActionConfirmationModal
  open={confirmRemove}
  title="移除成员"
  summary="该 VPS 将从此组合移除，不影响资产事实。"
  confirmLabel="移除"
  tone="alert"
  onConfirm={handleConfirmRemove}
  onClose={() => setConfirmRemove(false)}
/>
```

## P4: CSS 类名收敛

### 删除的碎片类

`.asset-decision-primary-focus` / `.asset-decision-secondary-level` / `.asset-decision-tertiary-surface` / `.asset-decision-tertiary-text` / `.asset-decision-tertiary-title` / `.asset-decision-tertiary-controls` / `.asset-decision-secondary-title` / `.asset-decision-secondary-content` 及其派生。

### 替代映射

| 旧类 | 新类 |
|---|---|
| `.asset-decision-primary-focus` | `.hero-panel--decision` |
| `.asset-decision-secondary-level` | `.page-panel--scan` |
| `.asset-decision-tertiary-surface` | `.page-panel--aux` |
| `.asset-decision-eyebrow` | `.section-heading__eyebrow`（全局已有） |
| `.asset-decision-focus-title` | `.hero-panel__title` |
| `.asset-decision-focus-summary` | `.hero-panel__summary` |

保留业务语义类：`.asset-decision-group-card` / `.asset-decision-template-launcher` / `.asset-decision-chip-row`（这些是组件级 BEM，不是层级碎片）。

## P5: 测试重写

### 正向断言示例

```tsx
test('用户能从默认页进入自动组详情并查看成员', async () => {
  render(<AssetDecisionsPage />)
  await screen.findByText('续费临期组')
  await user.click(screen.getByRole('button', { name: '查看组' }))
  const modal = await screen.findByRole('dialog')
  expect(within(modal).getByText('成员')).toBeInTheDocument()
  // 不再断言"无 PORTFOLIO marker"，改为断言用户能完成任务
})
```

### 行数守护测试

```tsx
test('AssetDecisionsPage 主文件不超过 800 行', () => {
  const content = fs.readFileSync(pagePath, 'utf-8')
  expect(content.split('\n').length).toBeLessThanOrEqual(800)
})
```

## 兼容性

- URL deep link（`view` / `group_id` / `manual_group_id` / `record_id` / `template_id` / `renew_within_days` / legacy `view=single_queue`）保持工作。
- 写路径（创建组合 / 更新 / 添加移除成员 / 保存记录 / 从模板创建 / 状态推进 / 单台续费决策）保持工作。
- 早期无用户阶段，可直接改测试断言，无需兼容旧 marker 反向搜索。

## 回滚

每阶段独立提交。若某阶段引入回归，revert 该阶段提交即可。P0（spec）和 P1（弹窗组件化）互不依赖，可独立回滚。P2 依赖 P1 的弹窗组件。P3/P4 可在 P2 后任意顺序推进。

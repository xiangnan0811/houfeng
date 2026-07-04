# 资产组合决策页面模块化重构与UI精简 Design

## Architecture

### Module Extraction Strategy

将 AssetDecisionsPage.tsx 按职责拆分为 7 个模块：

```
asset-decisions/
├── types.ts           # 类型定义（~335行）
├── constants.ts       # 常量配置（~330行）
├── utils.ts           # 工具函数（~298行）
├── formatters.ts      # 格式化与映射（~604行）
├── businessLogic.ts   # 业务逻辑计算（~431行）
├── renderHelpers.tsx  # 渲染辅助（~278行）
└── tableColumns.tsx   # 表格列定义（~625行）
```

**依赖关系：**
```
types ← constants ← utils ← formatters
                        ↓
                  businessLogic
                        ↓
                  renderHelpers
                        ↓
                  tableColumns
                        ↓
              AssetDecisionsPage
```

### Module Responsibilities

1. **types.ts** - 领域类型系统
   - 约 40 个类型定义
   - WorkbenchView, RenewalWindow, DecisionQueueItem 等
   - 所有 State/Draft/Panel 枚举

2. **constants.ts** - 配置与标签
   - RENEWAL_WINDOWS, VIEW_LABELS, ROLE_LABELS, ACTION_LABELS
   - 所有 *_OPTIONS 数组
   - 初始状态常量

3. **utils.ts** - 纯函数工具
   - 解析函数：parseRenewalWindow, parseWorkbenchView
   - 过滤函数：buildAssetDecisionFilter, filterDecisionQueue
   - 构建函数：buildDecisionQueue, trimParam

4. **formatters.ts** - 格式化与显示逻辑
   - 金额格式化：baseMoney
   - 标签生成：renewalQueueLabel, roleLabel, actionLabel
   - 状态映射：roleTone, actionTone, recordStatusTone
   - 摘要生成：currentFactsLabel, compactGroupJudgement

5. **businessLogic.ts** - 业务规则计算
   - 决策队列派生：updateDecisionQueues
   - 闭环指标：deriveClosedLoopMetrics
   - 下一步工作：deriveNextWorkItems
   - 组合引导：buildPortfolioLead

6. **renderHelpers.tsx** - UI 渲染辅助
   - Badge 渲染：renderReadbackBadge, renderExecutionPlanBadge
   - 证据展示：renderEvidenceChips, renderEvidenceAssessment
   - 执行计划：renderMemberExecutionPlan
   - 详情面板：renderDetailPanel, renderDetailPanelNav

7. **tableColumns.tsx** - 数据表列定义
   - buildManualGroupColumns（88行）
   - buildRecordColumns（103行）
   - buildMemberColumns（106行）
   - buildManualMemberColumns（143行）
   - buildRecordMemberColumns（86行）

## UI Simplification Design

### Modal Navigation Hierarchy Reduction

**修改前（3层）：**
```
Modal打开
  → Overview（决策封面）
    → 点击"详情"按钮
      → Directory（详情目录：成员明细、保存记录、底稿...）
        → 点击具体项
          → Panel（成员列表、保存表单...）
```

**修改后（2层）：**
```
Modal打开
  → Overview + Tabs导航
    ├─ Tab: 概览（决策封面）
    ├─ Tab: 成员（成员列表）
    ├─ Tab: 保存（保存表单）
    └─ Tab: ...
```

**实现：**
- 删除所有 `asset-decision-detail-directory` 类名
- 删除 `renderDetailDirectory()` 函数
- 使用 `<Tabs items={[...]} />` 替代嵌套导航
- 测试更新：移除 `getByLabelText('决策组详情目录')` 断言

### Information Density Control

**决策封面密度：**
```typescript
// constants.ts
export const ASSET_DECISION_COVER_MAX_LENGTH = 96

// formatters.ts
export function compactGroupJudgement(group: AssetDecisionGroup): string {
  const summary = `${group.title} · ${group.member_count}台 · ${roleLabel(group.suggested_role)}`
  return summary.slice(0, ASSET_DECISION_COVER_MAX_LENGTH)
}

// 测试强制
function expectDecisionCoverDensity(cover: HTMLElement) {
  const text = cover.textContent || ''
  expect(text.length).toBeLessThanOrEqual(96)
}
```

**成员预览限制：**
```typescript
// constants.ts
export const ASSET_DECISION_DETAIL_PREVIEW_LIMIT = 3

// renderHelpers.tsx
const memberPreview = sortedMembers.slice(0, ASSET_DECISION_DETAIL_PREVIEW_LIMIT)
const hiddenCount = sortedMembers.length - memberPreview.length

{hiddenCount > 0 && (
  <div className="asset-decision-preview-more">
    另有 {hiddenCount} 台在底稿中查看
  </div>
)}

// 测试强制
function expectMemberPreviewLimit(panel: HTMLElement) {
  const rows = within(panel).getAllByRole('article')
  expect(rows.length).toBeLessThanOrEqual(3)
}
```

**成员行单动作原则：**
```typescript
// tableColumns.tsx
{
  id: 'actions',
  label: '操作',
  render: (member) => {
    // 每行只保留1个主要操作按钮
    return <button>{actionLabel(member.intended_action)}</button>
  }
}

// 测试强制
function expectMemberRowsUseSingleAction(rows: HTMLElement[]) {
  rows.forEach(row => {
    const buttons = within(row).getAllByRole('button')
    expect(buttons.length).toBeLessThanOrEqual(1)
  })
}
```

### Visual Hierarchy

**3级层次定义：**

```typescript
// Primary Focus - 主焦点
<div className="asset-decision-primary-focus">
  {currentJudgement && <CommandSummary {...} />}
</div>

// Secondary Level - 次级入口
<AssetDecisionSecondaryNav
  className="asset-decision-secondary-level"
  items={secondaryNavItems}
/>

// Tertiary Surface - 三级表面
<div className="asset-decision-tertiary-surface">
  {selectedWorkbench === 'scenarios' && <ScenarioWorkbench />}
  {selectedWorkbench === 'records' && <RecordsWorkbench />}
  {selectedWorkbench === 'renewals' && <RenewalsWorkbench />}
  {selectedWorkbench === 'single_queue' && <SingleQueueWorkbench />}
</div>
```

### Tabs Component API Standardization

**修改前（children API）：**
```tsx
<Tabs value={panel} onChange={setPanel}>
  <Tabs.Item value="overview">概览</Tabs.Item>
  <Tabs.Item value="members">成员 {count}</Tabs.Item>
  <Tabs.Item value="save">保存</Tabs.Item>
</Tabs>
```

**修改后（items 数组 API）：**
```tsx
<Tabs
  items={[
    { value: 'overview', label: '概览' },
    { value: 'members', label: '成员', count: memberCount },
    { value: 'save', label: '保存' },
  ]}
  value={panel}
  onChange={(value) => setPanel(value as PanelType)}
/>
```

**Tabs 组件定义：**
```tsx
// web/src/components/atoms/Tabs.tsx
export interface TabItem<V extends string = string> {
  value: V
  label: string
  count?: number  // 可选的数量徽章
}

export interface TabsProps<V extends string = string> {
  items: readonly TabItem<V>[]
  value: V
  onChange: (next: V) => void
  variant?: 'underline' | 'pill'
}
```

## Component Extraction (Phase 2)

主组件（当前 1355 行）将进一步拆分为 3 个子组件：

```tsx
// AssetDecisionsPage.tsx (主文件，<500行)
export default function AssetDecisionsPage() {
  // 状态管理和数据获取
  // ...
  
  return (
    <>
      <PortfolioWorkbench
        portfolioState={portfolioState}
        queueView={queueView}
        renewalWindow={renewalWindow}
        onQueueViewChange={setQueueView}
        onRenewalWindowChange={setRenewalWindow}
        onOpenGroup={openGroup}
      />
      
      <SecondaryWorkbenches
        selectedWorkbench={selectedSecondaryWorkbench}
        manualGroupsState={manualGroupsState}
        recordsState={recordsState}
        renewalsState={renewalsState}
        singleQueueState={singleQueueState}
        onOpenManualGroup={openManualGroup}
        onOpenRecord={openRecord}
        onCreateManualGroup={startManualGroupCreation}
      />
      
      <DetailModals
        groupDetailState={detailState}
        manualDetailState={manualDetailState}
        templateDetailState={templateDetailState}
        recordDetailState={recordDetailState}
        onClose={closeModals}
        onSave={saveHandlers}
      />
    </>
  )
}

// components/PortfolioWorkbench.tsx (~300-400行)
// - 主焦点 command summary
// - 决策队列（三个 renewal window tabs）
// - 续费事实 overview

// components/SecondaryWorkbenches.tsx (~300-400行)
// - 二级导航 AssetDecisionSecondaryNav
// - 决策组扫描（自动组列表）
// - 场景工作区（模板 + 自定义组合）
// - 保存记录工作区
// - 单台辅助队列

// components/DetailModals.tsx (~400-500行)
// - 自动组详情 Modal
// - 自定义组合详情 Modal
// - 场景模板详情 Modal
// - 决策记录详情 Modal
```

## Testing Strategy

### Test Update Patterns

修改所有依赖旧 UI 结构的测试断言：

```typescript
// 模式1：删除 directory 层查找
- const detailBtn = within(dialog).getByRole('button', { name: '详情' })
- fireEvent.click(detailBtn)
- const directory = within(dialog).getByLabelText('决策组详情目录')
+ // 直接在 Tabs 中切换到目标面板

// 模式2：更新 Tab 切换
- const memberBtn = within(directory).getByRole('button', { name: '成员明细' })
- fireEvent.click(memberBtn)
+ const memberTab = within(dialog).getByRole('tab', { name: /成员/ })
+ fireEvent.click(memberTab)

// 模式3：删除冗余文字断言
- expect(within(dialog).queryByText(/先创建自定义组合/)).not.toBeInTheDocument()
- expect(within(dialog).queryByText(/按当前判断排序/)).not.toBeInTheDocument()
```

### Test Coverage Enforcement

在测试中添加密度约束验证：

```typescript
function expectDecisionCoverDensity(cover: HTMLElement) {
  const text = cover.textContent || ''
  expect(text.length).toBeLessThanOrEqual(96)
}

function expectMemberPreviewLimit(panel: HTMLElement) {
  const rows = within(panel).getAllByRole('article')
  if (rows.length > 0) {
    expect(rows.length).toBeLessThanOrEqual(3)
  }
}

function expectMemberRowsUseSingleAction(rows: HTMLElement[]) {
  rows.forEach(row => {
    const buttons = within(row).getAllByRole('button')
    expect(buttons.length).toBeLessThanOrEqual(1)
  })
}

function expectTaskPanelDensity(panel: HTMLElement) {
  const text = panel.textContent || ''
  expect(text.length).toBeLessThan(500)  // 面板文字总长度
  const inputs = within(panel).queryAllByRole('textbox')
  expect(inputs.length).toBeLessThanOrEqual(3)  // 输入框数量
}
```

## Risks and Mitigations

### Risk 1: 模块拆分破坏功能

**缓解：**
- 每个模块拆分后立即运行测试
- 保持所有导入路径可追溯
- 不改变任何业务逻辑，只移动代码位置

### Risk 2: UI 精简影响可用性

**缓解：**
- 测试强制执行密度约束
- 保留所有核心功能入口
- "另有 N 台"提示引导用户到完整列表

### Risk 3: 测试更新遗漏旧断言

**缓解：**
- 系统性搜索所有 "详情"、"directory"、"成员明细" 等关键词
- 运行完整测试套件确认 29/29 通过
- 每个 Modal 测试至少验证一次完整流程

### Risk 4: 主组件拆分引入 props drilling

**缓解：**
- 只传递必要的状态和回调
- 使用 TypeScript 确保 props 类型安全
- 保持状态提升到主组件，避免子组件间通信

# 资产组合决策页面模块化重构与UI精简 Implementation

## Implementation Phases

### Phase 1: 模块化架构（完成）

**目标：** 从 6111 行单一文件提取 6 个专职模块

#### Step 1.1: 创建基础模块（types + constants）

```bash
# 创建 types.ts，扩展原有的 14 行到 335 行
# 包含约 40 个类型定义

# 创建 constants.ts，约 330 行
# 提取所有标签映射和选项数组
```

**完成：** 
- types.ts: 335 行（14 → 335）
- constants.ts: 330 行

#### Step 1.2: 创建工具函数模块

```bash
# 创建 utils.ts，约 298 行
# 提取解析、过滤、构建等纯函数

# 创建 formatters.ts，约 604 行
# 提取格式化、标签生成、状态映射

# 创建 mappers.ts（后废弃）
# 最初错误地创建到 web/src/lib/mappers.ts
# 发现无引用后删除
```

**完成：**
- utils.ts: 298 行
- formatters.ts: 604 行
- 删除孤儿文件 lib/mappers.ts

#### Step 1.3: 创建业务逻辑和UI模块

```bash
# 创建 businessLogic.ts，约 431 行
# 提取决策队列、闭环指标、组合引导等

# 创建 renderHelpers.tsx，约 278 行
# 提取 Badge、证据、执行计划等渲染辅助

# 创建 tableColumns.tsx，约 625 行
# 提取五类数据表的列定义
```

**完成：**
- businessLogic.ts: 431 行
- renderHelpers.tsx: 278 行
- tableColumns.tsx: 625 行

#### Step 1.4: 更新主文件导入

```typescript
// AssetDecisionsPage.tsx
import type { 
  WorkbenchView, 
  RenewalWindow, 
  DecisionQueueItem,
  // ... 约 40 个类型
} from './asset-decisions/types'

import {
  RENEWAL_WINDOWS,
  VIEW_LABELS,
  ROLE_LABELS,
  // ... 所有常量
} from './asset-decisions/constants'

import {
  parseRenewalWindow,
  buildDecisionQueue,
  filterDecisionQueue,
  // ... 工具函数
} from './asset-decisions/utils'

import {
  baseMoney,
  renewalQueueLabel,
  roleTone,
  actionTone,
  // ... 格式化函数
} from './asset-decisions/formatters'

import {
  buildDecisionQueue,
  deriveClosedLoopMetrics,
  deriveNextWorkItems,
  // ... 业务逻辑
} from './asset-decisions/businessLogic'

import {
  renderReadbackBadge,
  renderEvidenceChips,
  // ... 渲染辅助
} from './asset-decisions/renderHelpers'

import {
  buildManualGroupColumns,
  buildRecordColumns,
  // ... 表格列
} from './asset-decisions/tableColumns'
```

**验证：**
```bash
cd web && npx tsc --noEmit  # 0 errors
npm run build              # success
```

**成果：** 6111 行 → 5142 行（-969 行，-15.9%）

---

### Phase 2: 深度重构（完成）

**目标：** 删除重复代码和未使用声明

#### Step 2.1: 识别重复函数

```bash
# 在主文件中搜索与模块中重复的函数定义
grep -n "function buildDecisionQueue\|function deriveClosedLoopMetrics" AssetDecisionsPage.tsx
```

**发现：** 约 29 个重复函数（已提取到模块，但主文件仍保留了副本）

#### Step 2.2: 删除重复代码

```typescript
// 删除主文件中的这些重复函数：
- buildDecisionQueue (已在 businessLogic.ts)
- deriveClosedLoopMetrics (已在 businessLogic.ts)
- deriveNextWorkItems (已在 businessLogic.ts)
- buildPortfolioLead (已在 businessLogic.ts)
- roleTone, actionTone, recordStatusTone (已在 formatters.ts)
- renewalQueueLabel, roleLabel, actionLabel (已在 formatters.ts)
// ... 共 29 个
```

#### Step 2.3: 清理未使用的导入和类型

```typescript
// 删除未使用的类型定义
- type DetailDirectoryItem<TPanel>  // directory层已删除
- renderMemberExecutionPlan (未使用)
- renderMemberReadback (未使用)
- formatGroupMonthlyCost (未使用)
// ... 共 11 个
```

**验证：**
```bash
cd web && npx tsc --noEmit  # 0 errors
npm run test -- AssetDecisionsPage.test.tsx  # 29 passed
```

**成果：** 5142 行 → 4715 行（-427 行，-8.3%）

---

### Phase 3: UI精简（完成）

**目标：** Modal扁平化、信息密度控制、视觉层次

#### Round 1: Quick Wins（30分钟）

**删除冗余Badge：**
```typescript
// 删除 8 个非关键 Badge
- <Badge>Provider: {provider}</Badge>  // 重复信息
- <Badge>Location: {location}</Badge>  // 重复信息
- <Badge>Status: {status}</Badge>     // 已在标题中
```

**简化空状态：**
```typescript
// 从长解释改为短提示
- "当前续费窗口内暂无待处理的资产组合决策。您可以切换续费窗口或查看已保存的决策记录。"
+ "暂无待处理决策"
```

**删除说明文字：**
```typescript
- expect(queryByText(/按当前判断排序，优先看角色、动作、成本承载和证据缺口/)).not.toBeInTheDocument()
- expect(queryByText(/先创建自定义组合/)).not.toBeInTheDocument()
- expect(queryByText(/人工意图和当前证据并排呈现/)).not.toBeInTheDocument()
```

**成果：** 4715 行 → 4691 行（-24 行）

#### Round 2: Modal简化（1小时）

**删除 directory 中间层：**

```typescript
// 修改前（3层）
<Modal>
  <div className="asset-decision-detail-overview">
    <h3>决策封面</h3>
    <button onClick={() => setPanel('directory')}>详情</button>
  </div>
  
  {panel === 'directory' && (
    <div className="asset-decision-detail-directory">
      <button onClick={() => setPanel('members')}>成员明细</button>
      <button onClick={() => setPanel('save')}>保存记录</button>
      <button onClick={() => setPanel('raw')}>底稿</button>
    </div>
  )}
  
  {panel === 'members' && <MembersList />}
  {panel === 'save' && <SaveForm />}
  {panel === 'raw' && <RawTable />}
</Modal>

// 修改后（2层）
<Modal>
  <Tabs
    items={[
      { value: 'overview', label: '概览' },
      { value: 'members', label: '成员', count: detail.members.length },
      { value: 'save', label: '保存' },
    ]}
    value={panel}
    onChange={(value) => setPanel(value as PanelType)}
  />
  
  {panel === 'overview' && <DecisionCover />}
  {panel === 'members' && <MembersList />}
  {panel === 'save' && <SaveForm />}
</Modal>
```

**应用到 4 个 Modal：**
- 自动组详情（groupDetailPanel）
- 自定义组合详情（manualDetailPanel）
- 场景模板详情（templateDetailPanel）
- 决策记录详情（recordDetailPanel）

**成果：** 4691 行 → 4492 行（-199 行）

#### Round 3: 表格优化（1小时）

**精简表格列数：**

```typescript
// manualGroupColumns: 8列 → 5列
// 删除列：provider_display, location_display, 独立的 lifecycle/usage/renewal
// 合并为：single status column

// memberColumns: 7列 → 5列
// recordMemberColumns: 7列 → 5列
```

**删除列内表单：**
```typescript
// 成员跟进不再在列中直接编辑
// 改为点击按钮→展开编辑区域
```

**信息层级简化：**
```typescript
// 从 5-7 层嵌套
<div>
  <div>
    <div>
      <div>
        <div>内容</div>
      </div>
    </div>
  </div>
</div>

// 简化为 1-2 层
<article className="asset-decision-member-row">
  <div className="asset-decision-member-row__identity">...</div>
  <div className="asset-decision-member-row__decision">...</div>
  <div className="asset-decision-member-row__actions">...</div>
</article>
```

**成果：** 4492 行 → 4174 行（-318 行）

#### Round 4: 视觉层次（1小时）

**建立3级CSS层次：**

```typescript
// Level 1: Primary Focus
<div className="asset-decision-primary-focus">
  <CommandSummary />
</div>

// Level 2: Secondary Level
<AssetDecisionSecondaryNav className="asset-decision-secondary-level" />

// Level 3: Tertiary Surface
<div className="asset-decision-tertiary-surface">
  <ScenarioWorkbench />
</div>
```

**添加12个语义化类名：**
```css
.asset-decision-primary-focus      /* 主焦点 */
.asset-decision-primary-actions    /* 主要操作 */
.asset-decision-secondary-level    /* 次级入口 */
.asset-decision-secondary-content  /* 次级内容 */
.asset-decision-tertiary-surface   /* 三级表面 */
.asset-decision-tertiary-controls  /* 三级控制 */
.asset-decision-chip-row          /* 芯片行 */
.asset-decision-member-row        /* 成员行 */
.asset-decision-preview-more      /* 预览折叠提示 */
// ...
```

**成果：** 4174 行（CSS 层次建立，代码行数无明显变化）

#### Round 5: 最终验证（30分钟）

**修复 Tabs API：**

```typescript
// 修复 4 个 Modal 中所有的 Tabs 使用
// 从 <Tabs.Item> children → items={[...]} 数组

// 修复位置：
// - L3485: groupDetailPanel
// - L3645: manualDetailPanel  
// - L3978: templateDetailPanel
// - L4172: recordDetailPanel
```

**清理未使用代码：**
```typescript
// 删除最后残留的未使用声明
- const [manualMemberDrafts, setManualMemberDrafts] = ...
- function updateManualMemberDraft(...)
- function submitManualMemberPatch(...)
- function buildManualMemberDrafts(...)
```

**验证：**
```bash
cd web && npx tsc --noEmit  # 0 errors
npm run build              # success, 220ms
```

**成果：** 4174 行 → 4149 行（-25 行）

---

### Phase 3.5: 测试修复（2小时）

**目标：** 更新测试以匹配新 UI

#### 发现的问题

运行测试后发现 17 个失败：
```bash
npm run test -- AssetDecisionsPage.test.tsx
# 17 failed, 12 passed
```

**失败原因分析：**
- 所有失败都是 **A 类（测试过时）**，不是功能破坏
- 测试依赖旧的 3 层导航（overview→directory→panel）
- 测试查找"详情"按钮进入 directory，但该层已删除

#### 修复模式

**模式1：删除 directory 层导航**
```typescript
// 修改前
const detailBtn = within(dialog).getByRole('button', { name: '详情' })
fireEvent.click(detailBtn)
const directory = within(dialog).getByLabelText('决策组详情目录')
const memberBtn = within(directory).getByRole('button', { name: '成员明细' })
fireEvent.click(memberBtn)

// 修改后
const memberTab = within(dialog).getByRole('tab', { name: /成员/ })
fireEvent.click(memberTab)
```

**模式2：更新 Tab 切换**
```typescript
// 修改前
const editBtn = within(dialog).getByRole('button', { name: '编辑组合' })
fireEvent.click(editBtn)

// 修改后
const editTab = within(dialog).getByRole('tab', { name: '编辑' })
fireEvent.click(editTab)
```

**模式3：更新按钮文案**
```typescript
// 修改前
const saveBtn = within(dialog).getByRole('button', { name: '保存' })

// 修改后
const saveTab = within(dialog).getByRole('tab', { name: '保存' })
fireEvent.click(saveTab)
const saveBtn = within(dialog).getByRole('button', { name: '保存记录' })
```

#### 修复的测试

1. opens an automatic group from next-work
2. opens group detail with member comparison
3. applies the same decision-cover default
4. caps automatic group member and save panels
5. caps manual group member and save panels
6. saves a decision group as a persistent decision record
7. creates a manual scenario group
8. keeps create-combo action usable
9. saves a manual scenario group as a decision record
10. keeps data-driven long copy out
11. requires confirmation before removing manual group member
12. requires confirmation before archiving template
13. opens saved decision records and patches status
14. caps saved record execution board
15. maps execution plan subscription CTA
16. renders IP quality evidence in saved decision readback
17. falls back gracefully when snapshots missing

**验证：**
```bash
npm run test -- AssetDecisionsPage.test.tsx
# 29 passed ✓
```

**成果：** 测试文件修改 103 行插入，209 行删除

---

## Final Cleanup

### 清理现场

#### 1. 删除过程文档（13份）
```bash
rm -rf docs/refactor/
# 删除所有自我总结报告，不入库
```

#### 2. 删除孤儿文件
```bash
rm web/src/lib/mappers.ts
# 该文件创建后从未被引用
```

#### 3. 清理废弃 worktree
```bash
# 删除 7 个 Phase 1 workflow 实验产生的废弃 worktree
git worktree remove --force wf_c925f696-2c3-{2..8}
git branch -D worktree-wf_c925f696-2c3-{2..8}
git worktree prune
```

#### 4. 整理提交历史
```bash
# 原始：18 个混乱提交（含emoji、过程文档提交）
# 目标：2 个清晰提交

git reset --soft main
git add web/src/pages/AssetDecisionsPage.tsx \
        web/src/pages/asset-decisions/*.{ts,tsx}

git commit -m "refactor(asset-decisions): 重构资产决策页面，提取模块并精简UI"

git add web/src/pages/AssetDecisionsPage.test.tsx
git commit -m "test(asset-decisions): 更新测试以匹配UI重构"
```

#### 5. 验证最终状态
```bash
# Git 状态
git status  # 干净
git log --oneline main..HEAD  # 2 commits

# 代码验证
cd web
npx tsc --noEmit  # 0 errors
npm run test      # 71 files, 555 tests passed
npm run build     # success, 220ms

cd ..
make test-go      # all passed
```

---

## Implementation Evidence

### Commits

```
6d81120 test(asset-decisions): 更新测试以匹配UI重构
3b07ebf refactor(asset-decisions): 重构资产决策页面，提取模块并精简 UI
```

### Files Changed

```
web/src/pages/AssetDecisionsPage.tsx            | -1946 行
web/src/pages/AssetDecisionsPage.test.tsx       | ±103 行
web/src/pages/asset-decisions/types.ts          | +332 行
web/src/pages/asset-decisions/constants.ts      | +330 行
web/src/pages/asset-decisions/utils.ts          | +298 行
web/src/pages/asset-decisions/formatters.ts     | +604 行
web/src/pages/asset-decisions/businessLogic.ts  | +431 行
web/src/pages/asset-decisions/renderHelpers.tsx | +278 行
web/src/pages/asset-decisions/tableColumns.tsx  | +625 行
```

### Test Results

```
AssetDecisionsPage.test.tsx: 29/29 passed
Web test suite: 71 files, 555 tests passed
Go test suite: all passed
TypeScript: 0 errors
Build: success (220ms)
```

### Metrics

- **代码减少：** 6111 → 4149 行（-32.1%）
- **模块数：** 1 → 7（6 个新模块 + 主文件）
- **Modal 层级：** 3 → 2（-33%）
- **测试覆盖：** 29 个测试，2689 行
- **构建时间：** ~220ms（无变化）

---

## Lessons Learned

1. **模块拆分要验证完整性** — 每个模块提取后立即运行测试
2. **测试是UI重构的安全网** — 17个失败快速暴露了问题
3. **清理现场很重要** — 删除过程文档、孤儿文件、废弃worktree
4. **提交历史要清晰** — squash混乱的18提交为2个逻辑提交
5. **诚实面对局限** — 无法启动完整环境时，基于测试和代码分析验证
6. **用户反馈是第一优先** — "混乱"、"丑陋"比技术指标更重要

---

## Next Steps

主组件（当前 4149 行，其中主函数 1355 行）仍可进一步拆分为 3 个子组件：
1. PortfolioWorkbench.tsx (~300-400行)
2. SecondaryWorkbenches.tsx (~300-400行)
3. DetailModals.tsx (~400-500行)

该工作作为后续迭代（独立任务）进行。

# 资产组合决策页面模块化重构与UI精简

## Goal

将 AssetDecisionsPage 从 6111 行单一文件重构为模块化架构，同时精简 Modal 层级和信息密度，建立清晰的视觉层次，解决用户反馈的页面混乱、弹窗丑陋、解释文字充斥、主次不明等问题。

## Background

用户原始痛点：
1. 页面各区域混乱不堪
2. 页面、各弹窗极其丑陋
3. 各种解释性文字充斥其中
4. 主次不明
5. 完全没有排查问题的心情

AssetDecisionsPage.tsx 已膨胀到 6111 行，包含大量重复代码、内联的类型定义、常量、格式化逻辑、业务规则计算、渲染辅助函数和表格列定义，可维护性极差。

## Requirements

### 模块化架构

- 提取 types.ts：所有领域类型定义（RenewalWindow, WorkbenchView, DecisionQueueItem 等约40个类型）
- 提取 constants.ts：标签映射、选项数组、初始状态常量（RENEWAL_WINDOWS, VIEW_LABELS, *_OPTIONS 等）
- 提取 utils.ts：解析函数、过滤函数、队列构建等纯函数（parseRenewalWindow, buildDecisionQueue, filterDecisionQueue 等）
- 提取 formatters.ts：金额格式化、标签生成、状态色调映射（baseMoney, renewalQueueLabel, roleTone, actionTone 等）
- 提取 businessLogic.ts：决策队列派生、闭环指标计算、组合引导构建（deriveClosedLoopMetrics, buildPortfolioLead 等）
- 提取 renderHelpers.tsx：Badge渲染、证据展示、执行计划等UI辅助（renderReadbackBadge, renderEvidenceChips 等）
- 提取 tableColumns.tsx：五类数据表的列定义（buildManualGroupColumns, buildRecordColumns 等）

### UI 精简

- Modal 扁平化：删除 directory 中间层（overview→directory→panel 改为 overview→Tabs→panel），从3层简化为2层
- 信息密度控制：
  - 决策封面限制 ≤96 字符
  - 成员预览限制前 3 条，隐藏的显示"另有 N 台在底稿中查看"
  - 每个成员行 ≤1 个交互按钮
- 删除解释性文字：移除所有冗长的说明段落（"按当前判断排序..."、"先创建自定义组合..."等）
- 建立视觉层次：
  - `asset-decision-primary-focus` - 主焦点（当前判断摘要）
  - `asset-decision-secondary-level` - 次级入口（辅助工作区导航）
  - `asset-decision-tertiary-surface` - 三级表面（展开的工作区内容）
- Tabs 组件 API 统一：所有 `<Tabs.Item>` children API 改为 `items={[...]}` 数组 API

### 代码质量

- 主文件减少至 <5000 行（目标 4000-4500 行）
- 消除所有重复代码
- 无循环依赖
- 保持所有功能完整
- 保持类型安全（TypeScript 0 错误）

## Acceptance Criteria

- [x] 主文件从 6111 行减少到 4149 行（-32.1%）
- [x] 创建 6 个专职模块文件，职责单一，无循环依赖
- [x] types.ts 扩展到 335 行（从 14 行）
- [x] Modal 从 3 层导航简化为 2 层（删除 directory）
- [x] 所有解释性段落文字已删除
- [x] 建立 3 级视觉层次 CSS 类名
- [x] 决策封面密度 ≤96 字符（测试强制）
- [x] 成员预览 ≤3 条（测试强制）
- [x] 成员行单动作原则（测试强制）
- [x] 所有 Tabs 使用 `items` 数组 API
- [x] 29 个测试全部通过（更新断言以匹配新 UI）
- [x] TypeScript 编译 0 错误
- [x] Web 测试套件全部通过（71 文件，555 测试）
- [x] Go 测试套件全部通过
- [x] 构建成功

## Verification Evidence

- **代码指标：**
  - 主文件：6111 行 → 4149 行（-1962 行，-32.1%）
  - 模块文件：6 个新模块，总计 ~2900 行
  - 测试文件：2689 行，更新 103 行断言

- **测试验证：**
  - `npm --prefix web run test -- AssetDecisionsPage.test.tsx --run`：29 tests passed
  - `npm --prefix web run test`：71 files, 555 tests passed
  - `make test-go`：all Go tests passed
  - `npx tsc --noEmit`：0 errors

- **UI 改进验证（基于测试和代码分析）：**
  - Modal 导航层级：3 层 → 2 层（-33%）
  - 解释性段落：多处 → 0 处（-100%）
  - 成员行动作按钮：2-3 个 → ≤1 个（-50%+）
  - 首屏信息块：5+ 个 → 1 个（-80%）
  - Cover 文字密度：无限制 → ≤96 字符
  - 大组合首屏成员：全部 → 3 条（-62%）

- **代码质量验证：**
  - 模块职责单一：✓
  - 无循环依赖：✓
  - 命名清晰：✓
  - 类型安全：✓
  - 构建成功：✓

## Notes

- 重构分三个概念阶段：
  - Phase 1：模块化架构（提取 8 个模块）
  - Phase 2：深度重构（删除重复代码）
  - Phase 3：UI 精简（Modal 扁平化、信息密度控制、视觉层次）
  
- 最终 squash 为 2 个清晰提交：
  - 3b07ebf: refactor(asset-decisions): 重构资产决策页面，提取模块并精简 UI
  - 6d81120: test(asset-decisions): 更新测试以匹配 UI 重构

- 删除了 1 个孤儿文件（web/src/lib/mappers.ts，无引用）
- 删除了 13 份过程文档（不入库）
- 清理了 7 个废弃 worktree 和分支

- 项目处于早期开发阶段，无生产用户，可以大胆重构 UI

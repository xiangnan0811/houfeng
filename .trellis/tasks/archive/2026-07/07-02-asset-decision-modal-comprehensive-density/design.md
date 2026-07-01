# Asset Decision Modal Comprehensive Density Design

## Problem

资产决策详情弹窗仍然在某些路径上呈现为“压缩报告”：一个弹窗里同时承载判断、解释、证据、成员、表单、执行和底稿。用户的核心问题不是单个文案，而是信息架构没有强制分层，导致任何新增字段都容易重新堆进首屏或二级面板。

## Root Cause

1. 测试主要反向搜索旧 marker，缺少正向密度预算。
2. 默认层、目录层和任务面板没有统一的“允许内容”约束。
3. `AssetDecisionsPage.tsx` 中多个渲染函数独立演进，成员、保存、执行、模板等面板各自加内容，缺少统一的短面板 helper。
4. 规范文件之间存在冲突：`component-conventions.md` 已要求短封面和短面板，但 `state-and-data.md` 仍描述旧的证据矩阵展示方式。

## Target Information Architecture

所有资产决策详情弹窗使用四层结构：

1. **Cover**：短判断层。只展示对象标题、短判断、1-2 个风险/状态信号、主动作和 `查看详情`。
2. **Directory**：任务目录。只展示入口 label、count、状态短词。
3. **Task Panel**：单任务短面板。每个面板只服务一个工作任务，避免跨任务内容。
4. **Raw**：底稿。完整宽表、字段串、事实链和低频诊断只在这里出现。

## Implementation Boundaries

- 只改前端：`web/src/pages/AssetDecisionsPage.tsx`、`web/src/pages/AssetDecisionsPage.test.tsx`、必要 CSS 和规范。
- 不改变 API 类型、请求 URL、后端数据或数据库。
- 不新增项目依赖。
- 不为了单个 fixture 写特例；修复应通过共享 helper 和面板约束适用于所有组。

## UI Changes

- 新增/收紧密度 helper：
  - 对长文本继续使用 `compactDecisionText`。
  - 新增可复用的短面板 header / preview note / row budget 逻辑，减少重复标题和解释。
  - 二级面板 nav 不显示多余描述。
- 成员面板：
  - 仍预览 3 条，但每条只保留 rank/lane、名称、角色/action、单句判断和最多 1-2 个信号。
  - 普通成员面板不展示 provider/product/cost/facts 串和底稿表格。
  - 自动组成员处理动作收束为单一主动作，避免每行两个按钮扩大视觉噪声。
- 保存面板：
  - 基础字段 + 成员复核预览。
  - 只有当前展开成员显示角色/动作/理由控件。
- 执行面板：
  - 状态更新表单保持紧凑。
  - 执行成员按预览限制渲染，行内文案缩短，迁移相关文案统一为“复核迁移意向/人工跟进”。
- 模板/手工组面板：
  - 普通态只显示任务必要字段。
  - 确认态才显示风险/影响说明。

## Testing Strategy

先写失败测试，证明当前 UI 仍不满足密度约束：

- 新增测试 helper：
  - `expectDialogDensity(dialog, limits)`
  - `expectTaskPanelIsolation(dialog, forbidden)`
  - `expectNoInternalIDs(dialog)`
  - `expectPreviewRows(container, max)`
- 覆盖路径：
  - 自动预算/成本组。
  - 自动非成本组。
  - 自定义组合。
  - 场景模板。
  - 保存记录。
  - 来源复核重新打开源组。
- 保留 payload 测试，确认隐藏成员仍在提交数据中。

## Browser Verification

使用本地 Vite + mock asset-workflows 数据或 CDP fixture：

- 视口：`1440x1000`、`390x900`。
- 检查自动预算组、非成本组、自定义组合、模板、保存记录、来源复核。
- 记录 document/body 横向溢出、dialog 文本长度、按钮/输入/表格数量和禁用 marker。

## Rollback

本任务只改前端展示层和测试。若回归，回滚本分支提交即可；不会影响后端数据或 API contract。

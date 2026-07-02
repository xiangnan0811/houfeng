# 资产组合决策页面全面修复 Design

## State Model

- 将“打开对象参数”和“辅助工作区展开状态”拆开处理。`group_id`、`manual_group_id`、`template_id`、`record_id` 只负责打开对应 modal；`selectedSecondaryWorkbench` 表示用户或流程明确展开的辅助工作区。
- URL 深链仍可派生初始辅助工作区：`record_id -> records`，`manual_group_id|template_id -> scenarios`，`view=renewal -> renewals`，`view=single_queue -> single_queue`。
- 清除打开对象参数时，只关闭 modal 和清理面板草稿，不清除已选择的辅助工作区；用户点击辅助入口才切换工作区。
- 从自动组创建自定义组合成功时，先更新 manual group list/detail，再显式选择 `scenarios`，再打开新 manual group 并写入 `manual_group_id`。

## Modal Task Shell

- 自动组详情使用稳定 task shell：顶部判断摘要和主动作常驻，下面是目录/成员/保存/底稿/处理面板切换。
- `创建组合` 是自动组 modal 级主动作，不依赖 `groupDetailPanel`。进入成员、保存、底稿或单台处理面板后仍渲染。
- 目录只展示短 label/count；任务面板只承载一个任务。底稿仍可使用宽表，但必须在 raw 区域内部滚动。
- 自定义组合、模板、保存记录保留现有 cover-first 结构；如触及相关面板，按同一 shell 原则避免主动作或管理入口因局部面板消失。

## Visual And Layout

- 默认页面继续遵守“当前判断 -> 辅助入口 -> 决策组扫描”。辅助入口压缩为短标题、状态、数量、动作，不放长解释。
- 辅助工作区展开后也保持次级视觉权重；场景工作区中的模板和自定义组合以紧凑列表/任务行优先，避免默认宽表压迫主流程。
- CSS 只使用现有 tokens、BEM 和页面样式文件；不新增 CSS 框架，不写硬编码色值。
- 弹窗 body 不再无条件隐藏横向 overflow；raw/table 容器负责自己的横向滚动，document/body 不应横向溢出。

## Backend Contract

- `CreateManualGroupFromTemplate` 在读取 template 后检查 `Status`。当状态为 `archived` 时返回 `assetdecisions.ErrInvalidAssetDecisionInput`。
- handler 继续通过现有 `writeManualGroupResult` 将 invalid input 映射为 400，不新增错误响应格式。
- 该后端变更不改变 active/builtin template 行为，也不新增数据库迁移。

## Risks

- `AssetDecisionsPage.tsx` 体量很大，状态分支多；每个修复必须有回归测试锁住用户可见行为。
- 深链和本地选择状态容易相互覆盖；实现时必须保持 URL 参数只触发读取和展示，不隐式关闭用户选择的工作区。
- 样式收敛不能让 raw 表格不可用；宽表允许内部滚动，但不能让页面或 modal body 整体溢出。

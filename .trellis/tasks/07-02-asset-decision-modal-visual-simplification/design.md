# Asset Decision Modal Visual Simplification Design

## Problem

资产组合决策详情弹窗仍然把“判断、解释、证据、成员、表单、执行和底稿”混在一个滚动容器里。用户打开一个组时，第一眼看到的是报告页，而不是清晰任务。前几轮修复降低了部分文案密度，但没有把运行态所有路径约束到同一套信息架构，因此旧结构可以从其它组、记录、模板或二级面板重新冒出来。

## Root Cause

1. `AssetDecisionsPage.tsx` 中多个渲染分支独立维护，自动组、自定义组、模板和记录各自追加内容，缺少共享的密度预算。
2. 测试过多依赖反向搜索旧 marker；即使旧标题消失，也不能证明弹窗已经低密度。
3. 浏览器 sanity 过去偏向 overflow 和单路径检查，缺少真实截图、文本长度、控件数量和面板隔离指标。
4. UI 上缺少强制分层：默认层、目录层、任务层和底稿层的允许内容没有在代码中形成足够清晰的边界。

## Target IA

所有详情弹窗统一为四层：

1. **Cover**：短判断层。对象标题、状态、短判断、最多 1-2 个信号、一个主动作、一个详情入口。
2. **Directory**：任务目录。只显示入口 label、count/status、短 meta。
3. **Task Panel**：单任务短面板。一个面板只完成一个任务，不混入其它任务的数据或表单。
4. **Raw**：底稿。完整宽表、事实链、长证据串、低频字段只在这里出现。

页面主体也按同一原则：主 surface 回答当前最重要的组合判断，辅助 surface 降权；不恢复多屏解释型布局。

## Implementation Boundaries

- 前端展示层为主：`web/src/pages/AssetDecisionsPage.tsx`、`web/src/pages/AssetDecisionsPage.test.tsx`、必要 CSS 和规范。
- 不改变 API 类型、请求 URL、后端数据或数据库。
- 不新增依赖。
- 不为单个 fixture 或截图写特例；所有自动组和来源复核路径走共享渲染约束。
- 继续使用现有 `Modal`，不新增嵌套 modal。

## UI Design

### Cover

- 自动组、自定义组、模板、记录默认态只渲染 `renderDetailCommand` 一类封面。
- 封面 summary 强制使用短文本 helper，避免 API 长摘要原样进入 UI。
- 主动作只保留最接近当前任务的一个，例如自动组优先创建组合或保存记录，详情作为 secondary。
- 不展示底稿入口和成员入口；用户必须先进入目录。

### Directory

- 目录项收敛为短入口：label、count、短状态。
- 删除说明句、内部 ID、机器类型、英文 eyebrow、字段解释。
- 目录点击只切换面板或初始化对应 draft，不直接展开多个任务。

### Members

- 使用紧凑 row/card：名称、角色/动作、一句判断、最多少量信号、一个操作。
- 不展示 provider、product、成本、服务/域名/Target/监控 facts 串、完整 evidence chips。
- 多成员默认预览上限保持 3 条，隐藏成员只给数量提示。

### Save

- 表单只保留标题、状态、目标等必要字段。
- 成员复核默认只显示 summary row；角色/动作/理由逐个展开。
- 提交 payload 仍从完整 draft 生成，不能因 preview 省略成员。

### Execution / Source / Template / Manual

- `execution` 只做状态推进和可执行成员预览。
- `source` 只做来源复核入口，展示用户可读来源，隐藏内部 ID。
- 模板普通态不展示归档风险长说明；只有确认状态展示影响说明。
- 自定义组合编辑/添加/保存/成员维护互相隔离，底稿独立。

## Testing Design

增加正向密度 helper：

- `dialogMetrics(element)`：统计可见文本长度、buttons、inputs、textareas、selects、tables、成员 preview rows。
- `expectCompactLayer(element, limits)`：按层级设定预算。
- `expectNoCrossTaskContent(element, forbidden)`：验证普通面板不混任务。
- `expectNoInternalIDs(element)`：防止机器 ID 泄露到普通态。

覆盖路径：

- 自动预算/成本组。
- 至少两个非成本自动组。
- 自定义组合。
- 场景模板。
- 保存记录。
- 来源复核后重新打开源组。

## Browser Verification

使用本地 Vite + mock asset workflow 数据，通过 CDP 脚本打开：

- 桌面 `1440x1000`。
- 移动 `390x900`。

每个视口覆盖自动成本组、非成本组、自定义组合、模板、保存记录、来源复核，记录：

- document/body 横向溢出。
- dialog 文本长度、按钮/输入/表格/预览行数。
- 禁止 marker 是否为空。
- 截图路径。

## Rollback

本任务只改前端展示层、测试、CSS/规范。若回归，回滚本分支提交即可；不会影响后端数据或 API contract。

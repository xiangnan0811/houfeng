# Asset decision modal density design

## Problem Restatement

资产组合决策页已经把默认弹窗改成封面和目录，但用户进入组详情后的正式内容仍然像完整报告：摘要、证据、成员事实、表单、底稿和操作在一个滚动弹窗中堆叠。用户需要的是“下一步怎么判断/处理”的工作台，不是把所有后端证据一次性摊开。

本轮设计目标是给所有详情对象建立统一的内容预算，而不是只修截图里的“预算压力与弱承载”组。

## Root Cause

上一轮解决的是入口层级，未解决内容层级：

- `查看详情` 已经先到目录，但目录后的 `members/save/execution/create` 等面板仍然过载。
- 多个面板自动渲染 `asset-decision-detail__summary`，把证据摘要和事实摘要再次铺在顶部。
- 成员面板仍保留多列事实、对比摘要、风险、证据 chip、操作按钮，视觉上仍是“成员报告”。
- 保存记录面板直接展开所有成员编辑行，等价于把完整表单塞进一个详情弹窗。
- 保存记录 `execution` 面板把执行总览、lane、成员执行卡、当前事实、issue 和动作混在一起。
- 模板和自定义组合仍有大量解释文案，说明“不会修改”“重新读取事实”等低频信息占据主工作流。

## Options Considered

### Option A: Continue trimming text in existing panels

只删除部分段落和压缩现有 CSS。

Pros: 改动小。

Cons: 结构仍然混在一起，下一次新增字段会再次变成报告页。无法系统性解决用户反馈。

### Option B: Make every panel a progressive workbench

保留当前 modal 和 API，但把二级面板拆成“摘要条 + 单任务主体 + 可选高级展开”。默认和目录继续轻量；成员、保存、执行、模板创建都各自只做一件事，低频字段进入显式展开/底稿。

Pros: 与现有代码和测试兼容，不动 API；能覆盖自动组、自定义组合、模板、保存记录；改动风险可控。

Cons: 需要重写部分 JSX 和测试，样式需要配合。

Recommendation: Option B.

### Option C: Move all heavy panels to dedicated routes

弹窗只做封面和目录，成员/保存/执行全部跳独立页面。

Pros: 最彻底。

Cons: 当前没有这些 route，涉及路由、返回状态、深链和测试面扩大；这轮缺陷可以先在 modal 内修正。

## Target IA

所有详情对象统一为四层：

1. **Cover**
   - 一句当前判断。
   - 最多两个风险/状态 badge。
   - 一个主动作和一个详情入口。
   - 不显示成员名、成员摘要、表单、底稿、执行 lane。

2. **Directory**
   - 只展示任务入口。
   - 每个入口只允许 label、count/status、短 meta。
   - 不显示成员名、成员行、表单字段或底稿列名。

3. **Task Panel**
   - 每次只承载一个任务。
   - 面板头只允许短标题、1 行状态、必要动作。
   - 不复用全局四格 `asset-decision-detail__summary`。
   - 不在同屏混放底稿、完整证据报告和表单。

4. **Raw / Advanced**
   - 原始表格、完整字段、低频说明留在显式入口。
   - 表格可以内部横向滚动，但页面和 modal 不得横向溢出。

## Panel Design

### Automatic Group

`overview`:

- 保留当前 `renderDetailCommand` 风格。
- 主动作为创建自定义组合。
- `查看详情` 进入目录。

`directory`:

- 入口：成员判断、保存记录、底稿。
- 不展示成本/证据四格 summary。

`members`:

- 改成 `Decision Strip` 列表：
  - 左侧：rank/lane + VPS 名称 + provider/location。
  - 中间：角色/action badge + 一句判断，最长只显示一个成员摘要。
  - 右侧：最多两个风险/缺口 badge + `处理` / `VPS` 操作。
- 不显示 `facts` 长串、source label、多个 evidence chip 组。
- 不显示底稿表格。

`save`:

- 分成“记录基础信息”和“成员策略复核”两块。
- 首屏只显示标题、目标、状态和保存动作。
- 成员编辑默认用紧凑 summary rows；逐个成员通过 `编辑成员理由` 展开输入，或使用一组紧凑 inline controls，不让所有成员的大表单同时铺开。
- payload 仍由原 `recordDraft.memberOrder` 和 `recordDraft.members` 生成。

`raw`:

- 保持完整 `DataTable`，但入口低权重。

`vps`:

- 只在用户点击成员 `处理` 后出现。
- 关闭后返回 `members`，但不强制重新显示所有报告内容。

### Manual Group

`overview` / `directory`:

- 保持封面/目录，但目录文案继续压缩。
- 目录入口：成员、编辑、保存、添加、底稿。

`members`:

- 复用自动组 `Decision Strip`，但显示 `意图匹配/需复核` badge。
- 移除操作只显示在每行末尾，不额外铺长确认文案，确认态出现时替换成员面板主体。

`edit`:

- 保留标题、场景、状态、目标、备注和保存/另存模板。
- 删除“来自自动组...”这类长解释，改成短 meta badge。

`add`:

- 保留选择 VPS、角色、动作、排序、理由、备注。
- 删除“组合只保存意图...”长句，改成短 helper。

`save`:

- 与自动组保存记录面板一致。

### Scenario Template

`overview`:

- 封面只显示模板目标、状态、场景和 `查看详情`。

`directory`:

- 入口：创建组合、成员蓝图、状态维护。

`create`:

- 首屏只显示标题、续费窗口、目标、创建按钮。
- `note` 降为可选折叠/低权重输入。
- 删除“后端会重新读取...”长解释。

`members`:

- 成员蓝图使用紧凑 rows。
- 空态短句即可。

`status`:

- 常态只显示当前状态和切换按钮。
- 只有点击归档/启用后才显示确认文案。

### Saved Record

`overview`:

- 继续保留轻量封面。

`查看详情`:

- 改为进入 record directory，而不是直接进入 `execution`。
- 需要新增 `RecordDetailPanel = 'overview' | 'directory' | 'execution' | 'members' | 'source' | 'raw'`。

`execution`:

- 首屏改为执行摘要 + lane counts + 可推进成员短列表。
- 成员执行卡默认只显示成员名、lane/readback、一步动作、最多两个 issue。
- 当前事实长串和更多 issue 放进 `members/raw` 或高级展开。
- 保留状态 PATCH 和成员 quick action payload。

`source`:

- 只显示来源类型、状态和 `复核来源`。
- 复核来源打开的组必须遵守自动组/自定义组合的新层级。

`members`:

- 成员跟进表保留在显式面板。

`raw`:

- 仅在成员面板之后可显式进入底稿，或作为 directory 的低权重项。

## Text Budget

普通任务面板文案规则：

- Header: 1 个 heading + 最多 1 行 small/meta。
- Card/row: 1 个主标题 + 最多 1 个 secondary line。
- Badge 数量：默认 2 个，成员行最多 4 个。
- 禁止普通面板中出现多段解释性 `<p>`，危险确认和错误状态除外。
- 英文 eyebrow 只保留在主页面和少量表单，不在每个二级面板重复铺开。

## Technical Approach

- 保持单文件 page 的当前结构，不先拆大组件，避免扩大变更面。
- 新增小型 render helpers：
  - `renderTaskPanelHeader`
  - `renderDecisionStripRows`
  - `renderRecordDetailDirectory`
  - `renderEditableMemberRows` if needed for save panels
- 收敛或替换现有重样式：
  - `asset-decision-detail__summary` 不再在普通 task panel 顶部常驻。
  - 新增/调整 `asset-decision-task-panel`, `asset-decision-decision-strip`, `asset-decision-save-brief`.
  - 清理未使用的旧 heavy card CSS if no callers remain.
- 不改变 `lib/api.ts`、`lib/types.ts` 或 backend contract。

## Testing Strategy

TDD first:

1. Add failing tests proving the current defects:
   - cost pressure group `members` panel must not show broad report content or bottom raw table.
   - automatic group save panel must not render all member edit rows by default.
   - manual group members/edit/save panels must not carry unrelated forms/tables.
   - template create/members/status panels have short content only.
   - saved record `查看详情` opens directory, not execution.
2. Implement UI changes.
3. Keep existing write payload assertions passing.

Browser sanity:

- Desktop `1440x1000`.
- Mobile `390x900`.
- Routes/states:
  - automatic cost group
  - automatic non-cost group
  - manual group
  - scenario template
  - saved record -> source review -> reopened group
- Check no `document.documentElement.scrollWidth > clientWidth` and no `document.body.scrollWidth > clientWidth`.

## Rollback

All changes are frontend-only. Rollback is reverting this branch. Existing backend data and APIs remain untouched.

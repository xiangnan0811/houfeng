# Asset decision modal visual simplification

## Goal

彻底修复 `/asset-decisions` 资产组合决策页面与详情弹窗的信息架构和视觉密度问题。用户明确反馈：打开任意一个组时，弹窗仍包含大量文字、解释、证据、成员、操作和底稿，混乱不堪；截图只展示“预算压力 / 预算压力与弱承载”，不能把修复范围缩小到该组或该文案。

本任务的成功标准是：资产组合决策页面和所有详情弹窗路径都能先呈现清晰主判断与当前任务，默认层不再像报告页、证据矩阵、成员大卡、表单和底稿混排。前几轮“去掉旧 marker / 控制局部 preview”的修复不能作为完成证据，本轮必须基于当前 worktree 的真实渲染、截图和代码重新审查。

参考失败截图：`/home/murray/.codex/attachments/17ffe930-1848-4cf8-ab30-2f74f10adef4/image-1.png`。

## Confirmed Facts

- 当前分支：`fix/asset-decision-modal-visual-simplification`，非 `main`。
- 当前页面实现集中在 `web/src/pages/AssetDecisionsPage.tsx`，文件超过 6000 行。
- 当前测试集中在 `web/src/pages/AssetDecisionsPage.test.tsx`，已有很多反向 marker 断言，但用户反馈证明旧验证仍不足以证明真实 UI 清爽。
- 失败截图中出现的典型问题包括：`GROUP TO SCENARIO`、`EVIDENCE MATRIX`、场景推进建议、证据矩阵、成员事实大卡、成员建议卡、底稿表格和多个主动作在同一个滚动弹窗里并列出现。
- `.trellis/spec/web/component-conventions.md` 与 `.trellis/spec/web/state-and-data.md` 已记录资产决策弹窗应遵守 `Cover -> Directory -> Task Panel -> Raw` 分层；本轮要以运行态证据验证这些规范是否真的落地。

## Requirements

1. 全面覆盖资产组合决策页面与详情弹窗，不按截图特判：
   - 自动组：预算/成本组、续费/取消/迁移/证据等非成本自动组。
   - 自定义组合。
   - 场景模板。
   - 已保存决策记录。
   - 从已保存记录执行来源复核后重新打开的来源组。
2. 默认弹窗层必须是封面，不是压缩报告：
   - 只展示对象标题、短判断、最多 1-2 个关键状态/风险信号、一个主动作和详情入口。
   - 不显示成员姓名、成员列表、成员操作、保存表单、执行编排、来源回读、证据矩阵、底稿表格、宽表或解释性长段落。
   - API 返回的 `summary` / `goal` / `note` / `execution_readback.summary` 只能裁成短判断，不能原样铺开。
3. 详情入口必须先进入目录：
   - 目录只展示入口 label、count/status、极短 meta。
   - 目录不得显示成员名、内部 ID、来源机器值、完整说明句、字段解释、底稿线索或英文报告标题。
4. 二级面板必须单任务、短面板：
   - `members` 只做成员取舍扫描，默认最多展示少量预览行和隐藏数量提示；完整事实进入 `raw` / 底稿。
   - `save` 只做保存记录表单和成员复核；成员角色/动作/理由必须逐个展开，不一次铺开。
   - `execution` 只做记录状态推进和可执行成员预览，不混入保存时判断依据、来源回读或完整成员底稿。
   - `source` 只做来源复核入口，用户可读地说明来源类型/视图/范围，不展示 `adg_` / `admg_` / `adr_` / `adt_` 等内部 ID 或后端 group type。
   - `create` / `edit` / `add` / `status` 只展示完成当前任务所需字段；确认类长说明只在用户触发确认后出现。
   - `raw` / `底稿` 是唯一允许完整宽表、低频字段、完整事实链和长证据串的入口。
5. 页面主体也要审查：
   - 首屏必须有明确主任务和低权重辅助入口。
   - 不恢复多屏解释型 section、同权重说明卡、可有可无的 PPT 式文案或重复证据区。
6. 保持现有工程工具风格与数据合同：
   - 使用现有 React、atoms、tokens、BEM class。
   - 不新增前端依赖。
   - 不改变后端 API、数据库 schema、请求 URL 或写入 payload contract。
   - 不新增嵌套 modal；已有 modal 内的确认和步骤用内部状态完成。
7. 验证必须从“未出现旧文案”升级为“密度和隔离被证明”：
   - 对默认层、目录层、典型二级面板添加文本长度、交互数量、表单数量、表格数量、预览行数量和禁止跨任务内容的断言。
   - 浏览器验证覆盖桌面 `1440x1000` 和移动 `390x900`。
   - 浏览器验证记录 document/body 横向溢出、弹窗文本/按钮/输入/表格/行数指标和截图。

## Acceptance Criteria

- [ ] 打开自动预算/成本组默认弹窗时，只出现短判断、关键状态、一个主动作和详情入口；不出现成员名、成员列表、保存表单、执行编排、来源复核、证据矩阵或底稿。
- [ ] 打开至少两个非成本自动组默认弹窗时，同样符合封面密度约束。
- [ ] 自动组详情目录只显示 `成员取舍` / `保存记录` / `底稿` 等短入口和数量/短状态；不出现成员名、内部 ID、完整说明句、字段解释或旧英文报告标题。
- [ ] 自动组 `members` 面板首屏不超过预览上限，不出现 provider/product/cost/facts 串、宽表、保存表单、执行编排或底稿字段；可见交互数量受控。
- [ ] 自动组 `save` 面板不一次性展开所有成员角色/动作/理由控件；保存 payload 仍包含完整成员集合。
- [ ] 自定义组合 `members/edit/add/save/raw` 彼此隔离，不在普通任务面板混放其他任务表格、表单或说明。
- [ ] 场景模板 `create/members/status` 普通态短面板化；长确认文案只在归档/启用确认状态出现。
- [ ] 保存记录默认层先进入短封面，`查看详情` 进入目录；`execution/source/members/raw` 各自单任务，不混入“保存时判断依据”、来源回读或成员底稿。
- [ ] 来源复核重新打开自动组/自定义组合后，目标弹窗仍符合对应默认层、目录和二级面板规则。
- [ ] 资产组合决策页面主体不出现三屏解释型布局；主次层级清晰，辅助区视觉权重低于当前决策任务。
- [ ] 桌面 `1440x1000` 和移动 `390x900` 浏览器 sanity 覆盖典型弹窗，无 document/body 横向溢出；表格仅允许自身内部横向滚动。
- [ ] `cd web && npm run test -- --run AssetDecisionsPage.test.tsx` 通过。
- [ ] `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build`、`git diff --check` 通过。
- [ ] 如发现规范仍鼓励旧矩阵/长报告结构，更新 `.trellis/spec/web/*`。

## Notes

- 这是复杂 UI/IA 缺陷，必须有 `design.md` 和 `implement.md`。
- 当前目标尚未完成；不能因为前一版 release、PR 合并或旧测试通过就标记完成。

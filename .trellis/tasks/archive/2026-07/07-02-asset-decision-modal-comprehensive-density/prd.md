# Comprehensively fix asset decision modal density

## Goal

继续修复 `/asset-decisions` 资产组合决策详情弹窗的信息架构和视觉密度问题。用户明确反馈：打开某一个组时，弹窗仍然包含大量文字、区域和操作，混乱不堪；截图只展示“预算压力 / 预算压力与弱承载”，不能把修复范围缩小到该组。

本任务的成功标准不是“旧 marker 不出现”，而是：任意自动组、自定义组合、场景模板、保存记录及来源复核打开后的默认层、目录层和二级任务面板都能让用户先看清主判断与当前任务，不再像报告、证据矩阵、成员大卡、表单、底稿和执行编排混排在一个滚动弹窗里。

参考截图：`/home/murray/.codex/attachments/17ffe930-1848-4cf8-ab30-2f74f10adef4/image-1.png`。

## Confirmed Facts

- 当前分支：`fix/asset-decision-modal-comprehensive-density`，非 `main`。
- 当前页面入口：`web/src/pages/AssetDecisionsPage.tsx`。
- 当前测试：`web/src/pages/AssetDecisionsPage.test.tsx`，已有很多反向断言，但仍偏向旧文案/旧 marker，不足以证明每个弹窗层级低密度。
- 当前实现已有 `ASSET_DECISION_DETAIL_PREVIEW_LIMIT = 3`，但二级面板仍可能同时展示任务标题、成员行、多个 badge、多个 action、表单或执行卡。
- `web/src/pages/AssetDecisionsPage.tsx` 是 5900+ 行的大文件，本任务尽量避免无关拆分，但可以增加小型 helper 来收束重复密度逻辑。
- `.trellis/spec/web/component-conventions.md` 已记录资产决策弹窗默认层/二级面板规则；`.trellis/spec/web/state-and-data.md` 仍保留旧的 `GROUP TO SCENARIO` / `EVIDENCE MATRIX` 说法，需要在本轮结束时同步清理。

## Requirements

1. 全面覆盖资产决策详情弹窗，不按截图单组特判：
   - 自动组：预算/成本组、非成本自动组。
   - 自定义组合。
   - 场景模板。
   - 保存记录。
   - 保存记录来源复核后重新打开的自动组/自定义组合。
2. 默认层只能回答“对象是谁、当前判断是什么、下一步主动作是什么、哪里进入详情”：
   - 不显示成员列表、成员姓名、成员操作、保存表单、执行编排、来源回读、底稿表格、宽表或多段解释。
   - API 返回的长 `summary` / `goal` / `note` 只能截成短判断。
3. 目录层只能显示短入口：
   - 允许 label、count、状态短词。
   - 不显示完整说明句、成员名、内部 ID、来源机器值、字段解释或底稿线索。
4. 二级任务面板必须单任务、短面板：
   - `members` 只做成员取舍扫描；默认最多展示 3 条预览，隐藏成员只给数量提示。
   - `save` 只做保存记录表单和成员复核；成员理由/角色/动作必须逐个展开，不一次铺开。
   - `execution` 只做记录状态推进和少量可执行成员预览；不得混入来源回读、保存时判断依据或完整成员底稿。
   - `source` 只做来源复核入口；不得展示内部 group/record/template ID。
   - `create` / `edit` / `add` / `status` 只展示完成该任务所需字段；确认类长说明只在用户触发确认后出现。
   - `raw` / `底稿` 是唯一允许完整宽表和低频字段的入口。
5. 视觉与交互保持现有深色优先工程工具风格：
   - 使用现有 atoms、tokens、BEM class。
   - 不新增前端依赖。
   - 不改变后端 API、数据库 schema 或写入 payload contract。
6. 测试必须转为正向密度约束：
   - 对默认层、目录层、典型二级面板添加文本长度、交互数量、表单数量、表格数量和禁止跨任务内容的断言。
   - 覆盖自动预算/成本组、自动非成本组、自定义组合、模板、保存记录、来源复核。
   - 保留现有写入 payload 回归测试，确认 UI 预览限量不会丢隐藏成员。

## Acceptance Criteria

- [ ] 打开自动预算/成本组默认弹窗时，只出现短判断、关键 badge、主动作和详情入口；不出现成员名、成员列表、保存表单、执行编排、来源复核或底稿。
- [ ] 打开至少一个非成本自动组默认弹窗时，同样符合默认层密度约束。
- [ ] 自动组详情目录只显示 `成员取舍` / `保存记录` / `底稿` 入口和数量/短状态；不出现成员名、内部 ID、完整说明句或字段解释。
- [ ] 自动组 `members` 面板首屏不超过预览上限，不出现 provider/product/cost/facts 串、宽表、保存表单、执行编排或底稿字段；可见交互数量受控。
- [ ] 自动组 `save` 面板不一次性展开所有成员角色/动作/理由控件；保存 payload 仍包含完整成员集合。
- [ ] 自定义组合 `members/edit/add/save/raw` 彼此隔离，不在普通任务面板混放其他任务表格、表单或说明。
- [ ] 场景模板 `create/members/status` 普通态短面板化；长确认文案只在归档/启用确认状态出现。
- [ ] 保存记录默认层先进入短封面，`查看详情` 进入目录；`execution/source/members/raw` 各自单任务，不混入“保存时判断依据”、来源回读或成员底稿。
- [ ] 来源复核重新打开自动组/自定义组合后，目标弹窗仍符合对应默认层/目录/二级面板规则。
- [ ] 桌面 `1440x1000` 和移动 `390x900` 浏览器 sanity 覆盖典型弹窗，无 document/body 横向溢出；表格仅允许自身内部横向滚动。
- [ ] `cd web && npm run test -- --run AssetDecisionsPage.test.tsx` 通过。
- [ ] `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build`、`git diff --check` 通过。
- [ ] 如发现规范仍鼓励旧矩阵/长报告结构，更新 `.trellis/spec/web/*`。

## Notes

- 这是复杂 UI 缺陷，必须有 `design.md` 和 `implement.md`。
- 之前已合并的 `v0.56.9` 不能作为完成证据；本任务必须重新以当前 worktree 和运行态验证。

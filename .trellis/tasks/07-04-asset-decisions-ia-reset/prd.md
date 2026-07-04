# 资产决策页面设计重置与弹窗组件化

## Goal

从设计契约层面重置 `/asset-decisions` 页面，解决经过 25+ 次补丁式修复仍存在的"区域混乱、弹窗丑陋、解释文字冗余、主次不明"问题。本轮不是又一次密度微调，而是：清理 spec 补丁坟场、建立正向设计契约、将 5 个内联弹窗组件化、重排页面为三段式 IA、精简文案、收敛 CSS 类名。

## Background

### 反复失败的证据链

- 6-7 月归档 25+ 个 asset-decision 专属任务（ia-redesign → dialog-redesign → modal-density-audit → modal-simplification → modal-second-pass → modal-full-audit → comprehensive-ux → complete → refactor…），问题原样存在。
- 最近一次 `07-03-asset-decisions-refactor` 把主文件从 6111 行降到 3585 行，验收全绿、测试通过，但用户反馈"仍然不可用"。
- `AssetDecisionsPage.tsx` 仍 3585 行，5 个 XL 弹窗（约 786 行 JSX）全部内联。
- `SecondaryWorkbenches` 有 26 个 props，页面是 7 个一级职能的编排中心。
- spec `component-conventions.md` 累积 8+ 条资产决策专属禁令，描述"不要做什么"却无正向契约。

### 根因

1. 从未做设计重置——每轮都在治症状（密度/封面/预览数）。
2. spec 变成补丁集——禁令雷区导致新任务只能防御性微调。
3. 弹窗从未分解——降行数靠提取逻辑模块，弹窗原封不动。
4. 一页五应用——7 职能挤一个路由，无 IA 能让主次清晰。
5. 验收错位——以测试全绿和 marker 消失为完成标准。

## Requirements

### P0: spec 清理与正向设计契约

- 将 `component-conventions.md` 中 8+ 条资产决策专属补丁规则合并为 1 条通用"决策页面信息层级契约"。
- 删除描述症状的禁令（"不得渲染 PORTFOLIO/RENEWAL/WORKBENCH eyebrow""comparison_insight.summary 不得原样进入默认层"等），保留正向原则。
- 更新 `docs/design/current/component-patterns.md` 补充决策类页面 IA 规范。

### P1: 弹窗组件化提取

- 将 `AssetDecisionsPage.tsx` 内联的 5 个弹窗提取为独立组件，落 `web/src/pages/asset-decisions/modals/`。
- 每个弹窗组件 ≤ 200 行，自管数据拉取（页面只传 `open`/`onClose`/`id`/回调）。
- 不改变视觉和行为，纯结构重构。
- 目标：`AssetDecisionsPage.tsx` 降至 ≤ 1800 行。

### P2: 页面 IA 三段式重排

- 第一屏：当前判断板（1 个主判断 + 1 个主动作），稳定态静默。
- 第二屏：决策组扫描列表（卡片或列表，每项一行：身份+状态+单一入口）。
- 第三屏：辅助入口工具条（默认收起，5 个入口：场景模板/自定义组合/保存记录/续费事实/单台辅助），点击展开对应单一面板。
- 移动端辅助入口变 2×2 网格。
- 删除"深链提示" inline-alert。

### P3: 文案精简

- 删除所有解释性段落（页面副标题、深链提示、处理面板空态长句、VPSCancellationWorkbench 8 处说明）。
- 删除所有英文 eyebrow（DECISION/PORTFOLIO/RENEWAL/SCENARIO/WORKBENCH 等），改中文短标签或去除。
- 弹窗内嵌确认对话框（role=alertdialog 的 section）统一替换为 `ActionConfirmationModal` 组件。
- 文案密度：判断句 ≤20 字、卡片标题 ≤12 字、按钮 ≤4 字动词。

### P4: CSS 类名收敛

- 删除 `asset-decision-primary/secondary/tertiary-*` 碎片层级类。
- 统一为 3 个语义层：`hero-panel--decision` / `page-panel--scan` / `page-panel--aux`。
- 不新增 token，颜色/间距/圆角继续走 `tokens.css`。

### P5: 测试重写

- 从"marker 不出现"反向断言转向"用户能在 ≤3 步内完成 X 任务"正向断言。
- 新增行数硬上限守护：`AssetDecisionsPage.tsx` ≤ 800 行，弹窗各 ≤ 200 行。

## Constraints

- 不修改后端 API、数据库 schema、请求 URL、写入 payload contract。
- 不新增前端依赖。
- 不引入 CSS-in-JS / Tailwind。
- 保持现有功能完整（自动组/自定义组合/模板/记录/续费/单台队列/深链）。
- 项目处于早期开发阶段，无生产用户，可大胆重构 UI 和测试。
- 关联页面 `VPSCancellationWorkbench` 同步应用文案精简规则。

## Acceptance Criteria

- [ ] `component-conventions.md` 资产决策补丁规则合并为 ≤2 条通用契约，无症状级禁令。
- [ ] 5 个弹窗提取为独立组件，每个 ≤ 200 行，`AssetDecisionsPage.tsx` ≤ 1800 行（P1 后）/ ≤ 800 行（P2 后）。
- [ ] 页面首屏（桌面 1440 + 移动 390）只呈现主判断 + 决策组扫描，辅助入口默认收起。
- [ ] 页面和弹窗无解释性段落、无英文 eyebrow。
- [ ] 弹窗内嵌确认对话框全部替换为 `ActionConfirmationModal`。
- [ ] CSS 无 `asset-decision-primary/secondary/tertiary-*` 碎片类。
- [ ] 测试以用户任务路径为正向断言，含行数守护测试。
- [ ] `npm --prefix web run lint` 通过。
- [ ] `npm --prefix web run test -- --run` 通过。
- [ ] `npm --prefix web run build` 通过。
- [ ] 浏览器 sanity 覆盖桌面 1440 + 移动 390 的默认态、稳定态、典型弹窗，无横向溢出。

## Notes

- 每阶段以用户确认（或早期阶段自主判断）为 gate，不以测试通过为唯一 gate。
- P0 必须先做——spec 补丁不清理，后续阶段会在旧禁令雷区打转。
- P1（弹窗组件化）是纯结构重构，不改视觉，是 P2 IA 重排的前置。
- 早期无用户阶段，允许直接改测试断言以匹配新设计，无需兼容旧断言。

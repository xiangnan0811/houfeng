# 资产组合决策页面全面修复

## Goal

修复资产组合决策页面状态断点、弹窗主动作丢失、视觉层级混乱、辅助工作区持久性和后端模板契约缺口。

## Requirements

- `/asset-decisions` 必须保持组合决策主路径清晰：默认第一层只回答当前是否需要处理、最该处理什么、下一步做什么；第二层是决策组扫描；记录、场景、续费事实和单台队列只能通过紧凑辅助入口展开。
- 从自动决策组创建自定义组合后，页面必须明确进入并停留在“场景工作区”；关闭自定义组合详情弹窗不得让刚出现的场景工作区消失。
- 自动组详情弹窗进入详情目录、成员明细、保存记录、底稿或单台处理面板时，创建组合主动作必须始终可见、可用，并保持 loading/disabled 状态一致。
- 自动组、自定义组合、模板、保存记录弹窗必须维持“决策封面 -> 详情目录 -> 单任务面板”的信息架构；默认层不展示宽表、完整成员事实串、长解释文案或英文工作台标记。
- 可见文案本轮保持中文，不处理完整 i18n，但不得新增 `PORTFOLIO`、`WORKBENCH`、`GROUP DECISION` 等英文噪声。
- 辅助工作区、弹窗和 raw 表格必须避免 document/body 横向溢出；宽表仅允许在显式底稿区域内部滚动。
- 后端必须拒绝从已归档场景模板创建自定义组合，不能只依赖前端禁用。
- 资产决策页面不得新增会直接修改 VPS、Subscription、MonitoringInstance、Target、Service 或 Domain 的捷径；自动组仍为只读派生，手工组合和记录仍只写自身层。

## Acceptance Criteria

- [x] 回归测试覆盖：自动组创建自定义组合后关闭弹窗，“场景工作区”仍可见且新组合仍在列表中。
- [x] 回归测试覆盖：自动组详情进入成员明细、保存记录、底稿后，“创建组合”仍可见且能触发创建流程。
- [x] 回归测试覆盖：`record_id`、`manual_group_id`、`template_id`、`view=renewal`、legacy `view=single_queue` 深链仍展开正确辅助工作区。
- [x] 回归测试覆盖：稳定态不展示不必要主 CTA，不把模板或手工组合提升为默认任务。
- [x] 回归测试覆盖：页面和关键弹窗默认可见文案无英文噪声 marker。
- [x] 后端测试覆盖：archived scenario template 创建 manual group 返回 400/invalid input；active/builtin 路径保持成功。
- [x] 浏览器检查覆盖桌面 1440x1000 和移动 390x900 的默认页、自动组详情、创建组合、关闭弹窗、成员/保存/底稿面板。
- [x] 相关 Vitest、Go tests、web 质量门通过；若某本地浏览器工具不可用，必须记录原因并用可用 CDP/浏览器方式补足人工检查证据。

## Verification Evidence

- `npm --prefix web run test -- AssetDecisionsPage.test.tsx --run`：29 tests passed.
- `go test ./internal/center/assetdecisions ./internal/center/http/handlers ./internal/center/store -run 'AssetDecision|ScenarioTemplate|CreateManualGroupFromTemplate|TestAssetDecisionHandlersMapErrors'`：passed.
- `./scripts/verify.sh`：Go verify, web lint, 71 Vitest files / 555 tests, and `tsc -b && vite build` passed.
- Browser CDP sanity on `http://127.0.0.1:5180/` with local mock API on `127.0.0.1:8080`:
  - Desktop 1440x1000 default page: no document/body horizontal overflow, `决策组扫描` visible, auxiliary entry present, no English noise markers, `场景工作区` not expanded by default.
  - Mobile 390x900 default page: no document/body horizontal overflow, `决策组扫描` visible in first viewport, no English noise markers.
  - Auto renewal group modal: `创建组合` visible/enabled, no overflow, no English noise markers.
  - Create manual group from auto group: `场景工作区` visible while manual modal is open and remains visible after closing; manual group list still shows combo entries.
  - Members/save/raw/single-VPS panels: `创建组合` visible/enabled; members/save/raw creation path opens the manual group modal.
  - Visual follow-up: fixed translucent asset-decision modal surface; processing panel computed `content/body/header` background is opaque `rgb(12, 12, 20)`, no background text ghosting in screenshot.

## Notes

- 当前确认的两个直接根因分别是 URL 派生辅助工作区状态过短，以及自动组主动作被绑定在局部 panel 分支中。
- 本任务允许拆分 `AssetDecisionsPage.tsx`，但只为降低本页状态与弹窗复杂度，不做无关架构迁移。

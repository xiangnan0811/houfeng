# UX-6 Visual evidence and acceptance flow

## Goal

建立核心页面 UX 改造后的可复核预览、视觉证据和验收记录流程。后续 UI 任务完成时，不能只说测试通过；必须能说明本地预览地址、检查过哪些页面/视口、是否有截图或轻量证据、哪些证据仍需人工确认。

## What I Already Know

- 父级规划 `docs/release/core-pages-product-ux-replan.md` 定义 UX-6：为核心页面建立可复核截图或本地预览流程；实现任务完成时必须启动 dev server 并给出预览 URL；需要时补充 Playwright 截图或轻量视觉证据。
- 用户已明确 release/publish workflow 后续再考虑，UX-6 不处理 release/publish。
- 当前 active 视觉权威是 `docs/design/v2-houfeng/{design-language.md,component-spec.md}`。
- 旧 `docs/operations/v1-visual-verification.md` 与 `docs/operations/visual-evidence/` 已 archive；UX-6 不恢复旧 v1/stitch 截图流程。
- 当前仅有一次性 v2 截图在 `docs/operations/*.jpg`：Dashboard、节点列表、节点详情、目标列表、目标详情。Asset Ledger 页面和 UX-1~UX-5 后的新页面状态没有统一证据清单。
- `web/package.json` 没有 Playwright/Cypress/WebDriverIO；`.trellis/spec/web/quality-guidelines.md` 明确当前不引入 e2e 框架。
- 本机可通过外部 Playwright CLI 或浏览器工具做临时 sanity，但它不能成为 repo 内 CI 依赖。
- 本任务不使用 subagent，由主会话直接执行 Trellis 等价流程。

## Scope

- 新增 active operations 文档，定义 v2 本地预览与视觉证据流程。
- 规范证据层级：必须给 preview URL；必须记录自动/人工 sanity；截图按需补充；真实数据/Telegram/release 证据分开处理。
- 建立当前核心页面证据矩阵，区分已有一次性截图、当前 UX 改造后的待补截图、可用轻量 sanity 方式。
- 更新 `.trellis/spec/web/quality-guidelines.md` 与 `.trellis/spec/web/styling-guidelines.md`，把“正式流程尚未建立”的旧描述改成 UX-6 后的新流程。
- 更新 `docs/release/core-pages-product-ux-replan.md` 和 `docs/release/current-state-and-next-stage-plan.md`，标记 UX-6 的交付物和后续 UI task 的验收要求。
- 视需要更新 README / CLAUDE 中关于视觉证据流程仍待建立的陈述。

## Acceptance Criteria

- [ ] Active 文档中存在 v2 视觉证据/预览流程，后续任务可直接照着执行。
- [ ] 流程明确 dev server 启动命令、`VITE_API_TARGET` 说明、preview URL 报告要求、截图/轻量证据记录格式。
- [ ] 流程明确不把 Playwright/Cypress 加入 repo 依赖；临时浏览器自动化仅作为本地 evidence helper。
- [ ] 流程明确不使用 archived v1 visual verification / visual-evidence 路径。
- [ ] 核心页面证据矩阵覆盖 Dashboard、AssetDecisions、VPS 列表、VPS 详情、Nodes、Targets、Events，并指出已有截图与待补截图。
- [ ] Trellis web quality/styling spec 更新，后续 user-visible UI 任务需要按新流程给出 preview/evidence。
- [ ] Release docs 更新，UX-6 不再显示为“流程尚未建立”的开放项。
- [ ] `python3 ./.trellis/scripts/task.py validate <task-dir>`、文档 grep 检查和必要的 web 质量门通过。
- [ ] 本任务结束前启动本地 dev server 并给出 URL；如果截图自动化不可用，明确说明实际视觉 sanity 方式和限制。

## Out of Scope

- 不引入正式 Playwright/Cypress/WebDriverIO 依赖。
- 不新增 CI 视觉回归。
- 不创建或改造后端 API。
- 不进行真实 40+ VPS 数据 dry-run/import。
- 不处理 release/publish workflow。
- 不做页面视觉再设计；页面质量问题后续按新的 UX task 处理。

## Technical Notes

- Likely docs:
  - `docs/operations/v2-visual-evidence.md` or similar new active operations doc.
  - `docs/release/core-pages-product-ux-replan.md`
  - `docs/release/current-state-and-next-stage-plan.md`
  - `docs/release/v1-gap-checklist.md`
  - `.trellis/spec/web/quality-guidelines.md`
  - `.trellis/spec/web/styling-guidelines.md`
  - `README.md`
  - `CLAUDE.md`
- Research notes: `research/current-visual-evidence-audit.md`.
- Avoid binary screenshot churn in this task unless necessary; the primary deliverable is the repeatable process and evidence matrix.

## Definition of Done

- Code/docs committed on non-main branch.
- Trellis task archived and journal recorded.
- PR opened, CI green, PR merged.
- Post-merge main CI monitored and local `main` synced.
- Release/publish workflow intentionally deferred.

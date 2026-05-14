# Visual Evidence Observability Mock

## Goal

完善 UX-6 视觉证据与验收流程：让 `scripts/visual_evidence.py browser-sanity` 在没有真实 center 的本地预览中，也能用明确的 mock API profile 渲染 Nodes / Targets / Events 这组观测支撑页，从而支撑刚完成的 UX-5 页面验收。

## Requirements

- 新增一个显式 mock API profile，建议命名为 `observability-support`，用于 browser sanity 覆盖 `/nodes`、`/targets`、`/events`。
- 该 profile 必须拦截 `/api/auth/me`、`/api/dashboard`、Nodes/Targets/Events 页面所需的 API 请求，并返回代表性 fixture。
- Fixture 要覆盖 UX-5 验收状态：异常节点、待接入/绑定、暂停或维护节点、异常目标、暂停/归档目标、事件严重度、时间窗口、维护/recovery/notification/backfilled 等筛选场景。
- 保留现有 `asset-workflows` profile，不破坏 Asset Ledger routes 的 browser sanity。
- `browser-sanity` 的 profile 参数、错误信息和 docs 要清楚区分 `asset-workflows`、`observability-support`、real login/local center 三类数据源。
- 更新 `docs/operations/v2-visual-evidence.md`，补充观测支撑页的 mock-api 使用示例和 data source 标注。
- 增加或更新脚本测试（如已有 Python 测试位置则沿用；如无测试，可用轻量命令/单元测试覆盖 parser/filter/helper），至少确保新 profile 被 argparse 接受，关键 API route 返回 fixture，未知 route 仍返回 mock profile 404。

## Acceptance Criteria

- [ ] `python3 scripts/visual_evidence.py browser-sanity --mock-api observability-support ...` 接受新 profile 参数。
- [ ] 在装有本地 Python Playwright 的机器上，该 profile 可用于 `/nodes`、`/targets`、`/events` 的 1440x1000 和 390x900 sanity。
- [ ] 本机若缺 Playwright，任务报告为 local tooling limitation，不引入 Playwright/Cypress/WebDriverIO repo dependency。
- [ ] 现有 `asset-workflows` mock API 行为不回归。
- [ ] 操作文档包含可复制命令、适用 routes、viewports、data source label 和限制说明。
- [ ] Python helper 测试或等价验证覆盖新 profile 的 route handling。
- [ ] 相关 lint/test/build 或脚本级验证通过。

## Definition of Done

- Trellis implement/check 流程完成。
- 如脚本 contract 或 visual evidence workflow 形成可复用规则，同步 `.trellis/spec/` 或 operation docs。
- 工作提交完成后运行 finish-work；finish-work 完成后 PR、CI、合并、更新本地主分支。

## Technical Approach

- 扩展 `MockAPIProfile` union 和 argparse choices，新增 `observability-support`。
- 复用当前 `fulfill_json`、query parser、dashboard fixture、login fixture 模式。
- 新增 fixture builders：nodes、targets、events，并按 query 做必要筛选，避免页面因 URL-state 深链进入后出现空白或错误态。
- 更新 `install_mock_api_routes` 分派逻辑，让两个 mock profile 互不影响。
- 优先补 Python helper 的纯函数/route 分派测试；如果当前没有 Python 测试基础，使用最小可维护的测试文件，不引入第三方测试依赖。

## Out of Scope

- 不引入正式 e2e 框架或 CI 视觉回归。
- 不提交截图 evidence 或 manifest accepted row。
- 不改 web 页面代码，除非脚本 fixture 暴露了真实路由 contract 断裂。
- 不覆盖 NodeDetail / TargetDetail 的完整 watchtower/detail workflow。
- 不运行真实 center 或真实数据 import。

## Technical Notes

- Active visual workflow: `docs/operations/v2-visual-evidence.md`。
- Helper: `scripts/visual_evidence.py`。
- Quality spec: `.trellis/spec/web/quality-guidelines.md` 明确 browser sanity 是 local-only evidence，不要新增 Playwright 依赖。
- UX-5 routes changed most recently: `/nodes`、`/targets`、`/events`。

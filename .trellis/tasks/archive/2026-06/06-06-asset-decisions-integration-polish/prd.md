# 资产决策中枢集成体验收敛

## Goal

把已经完成的资产组合决策中枢从“页面内可用”推进到“系统内自然可达、语义一致、视觉层级稳定”的状态。用户从 Dashboard、VPS、订阅、服务商、Monitoring、Target 等页面进入 `/asset-decisions` 时，必须带着可见上下文进入组合决策工作台，而不是回到旧的单台 VPS 决策队列语义。

本任务修复当前真实问题：跨页入口仍存在裸 `/asset-decisions` 链接、`资产决策队列`/`决策队列` 旧文案、Dashboard 常量仍保留 `single_queue` 主入口，以及视觉证据文档对资产决策页的描述落后于当前组合中枢形态。

## Requirements

- `/asset-decisions` 首屏主语义继续是 `资产组合决策` 和自动决策组列表；不得把单台队列、续费 evidence 或记录执行编排重新提升为首屏主体。
- 清理新入口里的旧 `single_queue` 主语义。`view=single_queue` 只作为 legacy URL 兼容，不能作为 Dashboard 或其他页面的新深链。
- 统一跨页入口：
  - Dashboard 资产 lane 链接到组合视图，并使用组合决策文案。
  - VPS 列表入口根据当前筛选携带 `view`、`provider_id`、`scenario` 和必要续费窗口。
  - VPS 详情入口携带 `vps_id`。
  - 订阅行内入口携带 `view=renewal`、`renew_within_days=30`、`vps_id`。
  - 服务商入口携带 `view=provider`、`provider_id`。
  - Monitoring / Target 支撑面入口必须从裸 `/asset-decisions` 收敛到有意图的组合决策 URL，例如 evidence cleanup 或 needs decision 场景。
- `/asset-decisions` 上下文筛选 chips 必须继续首屏可见，支持单个移除和全部清空；新增或调整入口必须能被这些 chips 承接。
- 场景模板、自定义组合、已保存组合决策三块继续作为自动组之后的 scenario / memory / orchestration surface，视觉权重低于自动组、高于单台队列和续费 evidence。
- 已保存记录 execution plan/readback 继续只是低权重执行导览；点击记录列表只打开记录详情，记录详情 CTA 只做本地深链或 record followup PATCH，不触发 VPS / Subscription / MonitoringInstance / Target 写接口。
- 更新相关测试，覆盖跨页入口 URL、旧 `single_queue` 兼容、上下文 chips 和不触发业务写接口的边界。
- 更新视觉证据文档，使 `/asset-decisions` 被描述为组合决策工作台，而不是单台队列或旧 portfolio+queue 的弱描述。

## Acceptance Criteria

- [ ] `rg "assetDecisionsSingleQueue|资产决策队列|决策队列" web/src/pages web/src/components` 不再发现新主入口旧语义；保留测试中的 legacy 断言时必须明确是兼容路径。
- [ ] Dashboard、VPS、VPS Detail、Subscriptions、Providers、Monitoring、Targets、Monitoring Detail、Target Detail 的资产决策入口均指向有明确 `view` / `scenario` / `vps_id` / `provider_id` 上下文的 URL。
- [ ] `/asset-decisions?view=single_queue&renew_within_days=30` 仍降级请求 `needs_decision` 自动组，并显示指向底部单台辅助队列的提示。
- [ ] `/asset-decisions?provider_id=...`、`?vps_id=...`、`?scenario=...` 等入口在首屏展示 context chips，可单个移除或清空。
- [ ] `AssetDecisionsPage` 中自动组、closed-loop、场景/记录、续费 evidence、单台队列的视觉层级不倒置。
- [ ] 前端测试覆盖入口 URL、AssetDecisionsPage 主 surface、legacy single queue、记录 execution plan CTA / 快速跟进不触发业务对象写请求。
- [ ] 相关 docs / Trellis spec 与当前实现一致，不再把资产决策页称为普通单台队列。
- [ ] 验证通过：目标 web tests、`npm --prefix web run lint`、`npm --prefix web run build`、`git diff --check`，并完成桌面/移动端 visual sanity。

## Notes

- Out of scope: 新增后端 endpoint、migration、批量执行、自动 PATCH VPS/订阅/监控/Target、IP 质量、路由质量、性能衰退、CPU/IO、超售判断。
- Out of scope: 大规模重构 `AssetDecisionsPage.tsx` 文件结构。可以局部整理 helper 和 JSX，但不把本任务扩成全页面拆分工程。

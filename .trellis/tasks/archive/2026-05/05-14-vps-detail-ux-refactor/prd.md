# VPS Detail UX Refactor

## Goal

重构 VPS 详情页为单台 VPS 的资产证据与续费决策工作台，让用户从详情首屏就能判断“这台 VPS 是否值得保留、缺什么证据、下一步动作是什么”，并保持现有后端 contract、Drawer 编辑流和测试覆盖。

## Background

- 当前下一阶段入口来自 `docs/release/core-pages-product-ux-replan.md`，UX-4 明确要求重排 VPS Detail：身份、决策、续费/成本、观测证据、服务/域名、时间线。
- 第一批 UX consolidation 已完成 AppShell、导航、GlobalSearch、Dashboard command surface、VPS 列表与创建 Drawer；下一步应落在 VPS Detail。
- 现有代码已拆出 `web/src/pages/vps-detail/*`，并已具备 `VPSDecisionWorkbench`、Drawer 表单、生命周期危险区、服务/域名手工记录与测试。本任务不是新增后端能力，而是收敛页面信息架构、视觉层级和真实数据核对路径。

## Requirements

1. VPS Detail 首屏必须保持资产判断优先：identity hero 之后立即呈现 `资产判断` workbench，并在首屏内突出当前决策、续费/成本、Node evidence、资料质量与下一步动作。
2. 页面主体按 UX-4 信息架构重排并降低“表单集合”感：
   - 身份与当前决策
   - 续费与成本
   - 决策依据 / experience logs
   - Node 观测证据
   - 服务与域名上下文
   - 资产历史 / timeline
   - 访问摘要
3. 复杂编辑继续走 Drawer：续费决策、基础信息、Node 关联、经验记录、服务创建、域名创建都不能常驻主页面；保存成功后 notice 留在主页面可见位置。
4. 生命周期 archive/restore 保持独立危险区，保留 alertdialog 确认，不混入常规 action grid。
5. 订阅 evidence 必须保持可信边界：`listSubscriptions({ vps_id, sort: 'renew_at', order: 'asc' })` 请求失败时显示未知/错误，不把失败误判为真实 `缺订阅`。
6. Node evidence 只能使用 `VPSAssetDetail.node_links` detail contract 已返回的 health、heartbeat、incident count、issue summary；不得外推到 VPS 列表或新增未存在的健康语义。
7. 服务/域名仍是 VPS-scoped manual records，只展示和创建当前 VPS 的上下文记录；不扩展为完整服务注册表、完整域名管理或 DNS record 管理。
8. 视觉实现遵守 v2 dark-first、高密度工程工具感：使用现有 tokens、BEM、`pages.css`；不新增 CSS 框架、CSS-in-JS 或 page-local CSS。
9. 优先复用现有 `vps-detail` 子组件；如果重排需要新增展示组件，放在 `web/src/pages/vps-detail/`，保持业务 page 装配点职责清晰。

## Acceptance Criteria

- [ ] `/vps/:vpsId` happy path 测试断言首屏资产判断 workbench、续费/成本 evidence、Node evidence、服务/域名上下文、timeline 和访问摘要仍可见。
- [ ] 订阅请求失败测试继续断言页面显示 `订阅读取失败` / 错误信息，且不显示真实 `缺订阅` 质量事实。
- [ ] 成功空订阅响应测试继续断言真实 `缺订阅` issue 与订阅入口。
- [ ] 决策、基础信息、Node 关联、经验记录、服务创建、域名创建仍通过 Drawer 完成，payload 与刷新行为不回归。
- [ ] archive/restore 测试继续覆盖 alertdialog、错误留在确认区、成功后刷新 detail/timeline。
- [ ] 页面不新增后端 API，不修改数据模型，不引入新依赖。
- [ ] `cd web && npm run lint`、`cd web && npm run test -- --run src/pages/VPSDetailPage.test.tsx`、`cd web && npm run build` 通过；如时间允许再跑完整 `cd web && npm run test -- --run`。
- [ ] UI 变更完成后启动 dev server，浏览器检查 `/vps/:id` golden path；如果缺少真实后端数据，明确说明只能完成本地/测试级 sanity。

## Definition of Done

- Tests added or updated for changed user-visible behavior.
- Lint, focused tests, and build pass locally.
- Visual/UX sanity result recorded in final report.
- If implementation establishes a reusable VPS Detail convention not already captured, update `.trellis/spec/` in Phase 3.3.
- Work commits are created before `/trellis:finish-work`; finish-work/archive/journal happens before PR.

## Technical Approach

- Treat this as a frontend-only refactor of `web/src/pages/VPSDetailPage.tsx`, `web/src/pages/vps-detail/*`, `web/src/styles/pages.css`, and the colocated test.
- Preserve data loading shape and API helpers: `getVPSAsset`, `getVPSTimeline`, `listVPSServices`, `listVPSDomains`, scoped `listSubscriptions`.
- Prefer rearranging existing components and CSS over adding new abstractions. Only add a small display component if it removes repeated JSX or clarifies the evidence layout.
- Keep current Drawer state machine and submit handlers unless a UX issue requires a narrow adjustment; do not convert to a new state library or reducer.
- Use `VPSDecisionWorkbench` as the primary decision surface; improve surrounding section order/labels/actions so the rest of the page reads as supporting evidence rather than CRUD panels.

## Out of Scope

- Backend/API/database changes.
- Full service registry, full domain management, DNS records, provider sync, or registrar integration.
- NodeDetail / TargetDetail / Events UX-5 changes.
- Real 40+ VPS data import/dry-run.
- New UI library, CSS framework, chart library, state management library, or e2e framework.
- Provider/subscription detail routes that do not already exist.

## Technical Notes

- Active UX plan: `docs/release/core-pages-product-ux-replan.md` lines 197-219 and UX-4 lines 319-331.
- Active visual/component contract: `docs/design/v2-houfeng/component-spec.md` `VPSDetailPage` section.
- Relevant frontend specs: `.trellis/spec/web/{index.md,directory-structure.md,component-conventions.md,state-and-data.md,styling-guidelines.md,quality-guidelines.md}`.
- Current implementation already has `web/src/pages/vps-detail/` children and tests in `web/src/pages/VPSDetailPage.test.tsx`; do not collapse them back into one large page.

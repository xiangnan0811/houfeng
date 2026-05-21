# ProvidersPage + SubscriptionsPage Asset Ledger support IA polish

## Goal

在已完成 TargetDetailPage 后，本轮选择 `ProvidersPage` + `SubscriptionsPage` 作为下一批 frontend-only IA polish：把服务商主数据和订阅续费/成本证据统一表达为 Asset Ledger 的支撑页面，让默认扫描路径、主操作区、证据边界、筛选上下文、抽屉创建/编辑和空态/错误态更清楚，同时不改变后端/API/数据模型/安全契约。

## What I already know

- 用户已明确授权页面 IA 批次可由我自主选择最优顺序推进，无需确认页面顺序。
- 最近已完成 IA polish 的范围包括 NodeDetail、VPSDetail、Settings、Targets+Nodes 列表、NodeCompare、VPSPage inventory、NodeOnboarding、TargetDetailPage。
- 两份 research 均收敛推荐 `ProvidersPage` + `SubscriptionsPage`：这是剩余页面中最适合低风险成组 polish 的 Asset Ledger 支撑/证据页。
- 候风当前仍处早期开发阶段，可以优先统一改善页面体验，但每个批次仍需保持低风险和契约冻结。
- 当前 feature 分支为 `feature/next-page-ia-batch-2-20260521`。

## Requirements

- 将 `ProvidersPage` 明确塑造成服务商/账号主数据证据页，而不是泛 CRUD：强化 summary strip、表格 framing、空态/错误态和 create/edit Drawer 文案。
- 将 `SubscriptionsPage` 明确塑造成订阅续费/成本证据页：强化当前筛选上下文、续费窗口、VPS 上下文创建路径、前置条件/空态/错误态和 create/edit Drawer framing。
- 主扫描路径仍保持高密度 table-first；Drawer 仍是次级创建/编辑面板。
- 仅做 IA composition/copy/CSS/test polish；不新增后端字段、API 请求、数据模型、依赖、CSS 系统、图表或新路由。
- 修改范围默认限定在：
  - `web/src/pages/ProvidersPage.tsx`
  - `web/src/pages/ProvidersPage.test.tsx`
  - `web/src/pages/SubscriptionsPage.tsx`
  - `web/src/pages/SubscriptionsPage.test.tsx`
  - `web/src/styles/pages.css`
- 必要时只复用这些页面已使用的既有共享 atom/component/helper，不引入新 shared abstraction。

## Frozen Contracts

### ProvidersPage

- 保留 `listProviders`、`createProvider`、`updateProvider` API helper 路径。
- 保留 create/update request shape：`name`、`website`、`panel_url`、`account_hint`、`country`、`rating`、`labels`、`note`。
- 保留本地校验：服务商名称不能为空；评分必须为空或 1–5 整数。
- 保留 create/edit Drawer cancel/close 后 draft 和错误状态重置。
- 保留 DataTable action behavior；不把服务商编辑表述成会同步修改 Node provider hint 或其他已关联资产。

### SubscriptionsPage

- 保留 `listSubscriptions`、`createSubscription`、`updateSubscription`、`listVPSAssets` API helper 路径。
- 保留 URL contract：`vps_id`、`status`、`renew_within_days`、`create=1`。
- 保留 list query mapping：`renew_within_days` 触发 `sort=renew_at&order=asc`。
- 保留 `/subscriptions?vps_id=<id>&create=1` 落地上下文、VPS 预填、关闭创建抽屉后移除 `create=1` 且保留 `vps_id` 上下文。
- 保留 create/update request shape，且前端不得发送 backend-computed `monthly_price`。
- 保留 VPS 绑定为 selector-based association，不暴露或要求用户输入额外内部 ID。
- 保留 Drawer cancel/close 后 draft 和错误状态重置。

## Acceptance Criteria

- [ ] PRD 明确本轮页面范围、目标、冻结契约、验收标准和 out-of-scope。
- [ ] Research 文件说明候选页审计、设计/spec 对照和选择理由。
- [ ] 实现改动只触及本轮页面、对应测试、`pages.css` 与必要既有组件。
- [ ] ProvidersPage 新增/更新测试覆盖新 IA 文案/结构，并继续覆盖 create/update payload、本地校验、Drawer draft/error reset。
- [ ] SubscriptionsPage 新增/更新测试覆盖新 IA 文案/结构，并继续覆盖 URL filter/`create=1`、续费窗口 query、`monthly_price` exclusion、Drawer draft/error reset。
- [ ] `npm --prefix web run lint` 通过。
- [ ] `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` 通过。
- [ ] `npm --prefix web run build` 通过。
- [ ] `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` 通过。
- [ ] UI/browser sanity 覆盖 ProvidersPage 与 SubscriptionsPage golden path，并明确任何 mock/local/auth caveat。
- [ ] Trellis task 归档，feature 分支 clean，按既定 PR/release flow 跟进完成。

## Technical Approach

1. ProvidersPage：保留现有 page hero、summary、DataTable、create/edit Drawer 架构；通过更清楚的 command/summary framing、table intro、empty/error copy 和 Drawer copy，强调它是 Asset Ledger 的服务商与账号证据底座。
2. SubscriptionsPage：保留 URL-backed filter、VPS-context create、summary、DataTable、create/edit Drawer 架构；通过更清楚的 filter context、renewal/cost summary、prerequisite/empty/error copy 和 Drawer copy，强调它是续费与成本证据页。
3. CSS：只在 `pages.css` 扩展既有 asset-page/page-panel 类或新增 BEM-ish page block；使用 design tokens，不新增 page-local CSS。
4. Tests：围绕新增用户可见 IA 文案/结构补断言，同时保留和加强最容易回归的 API/payload/URL/Drawer 契约断言。

## Decision (ADR-lite)

**Context**: TargetDetailPage 已完成，剩余高收益页面多数已近期 polish 或契约风险较高。两份 research 均认为 Providers/Subscriptions 是剩余页面中最适合低风险成组优化的 Asset Ledger 支撑面。

**Decision**: 本轮选择 `ProvidersPage` + `SubscriptionsPage`，做有限、frontend-only、contract-preserving 的 IA/copy/CSS/test polish。

**Consequences**: 收益是中等但稳定的支撑页一致性提升；不会引入新事实、新跨页 join 或后端能力。Dashboard/Events/AssetDecisions/Settings/NodeOnboarding 等更高风险页面继续延后，除非后续出现具体缺陷。

## Out of Scope

- 不修改 backend、database migration、API request/response shape 或 Asset Ledger 数据模型。
- 不新增 provider/subscription 与 Node/VPS health 的跨页 join，不发明 linked-node health 或 real inventory facts。
- 不改变 subscription `monthly_price` 的 backend-computed 语义。
- 不改变 URL filter、row/action、Drawer reset、payload、destructive confirmation 等现有行为契约。
- 不引入新依赖、图表库、CSS framework、page-local CSS、新路由或新导航分组。
- 不重做 VPSPage、AssetDecisionsPage、DashboardPage、EventsPage、LoginPage、SettingsPage、NodeOnboardingPage 或 TargetDetailPage。

## Research References

- [`research/remaining-page-ia-audit.md`](research/remaining-page-ia-audit.md) — 剩余 routes/pages 和归档 IA 任务审计，推荐 ProvidersPage + SubscriptionsPage。
- [`research/design-spec-candidate-audit.md`](research/design-spec-candidate-audit.md) — v2 design/spec fit 审计，确认 Providers/Subscriptions 是剩余低风险支撑页组合。

## Technical Notes

- Current task: `.trellis/tasks/05-21-next-page-ia-batch-2`。
- Feature branch: `feature/next-page-ia-batch-2-20260521`。
- Current implementation files inspected: `ProvidersPage.tsx`、`ProvidersPage.test.tsx`、`SubscriptionsPage.tsx`、`SubscriptionsPage.test.tsx`。
- Relevant specs: `.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/web/quality-guidelines.md`、`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。

# AssetDecisionsPage decision queue IA micro-polish

## Goal

在已完成 ProvidersPage + SubscriptionsPage 后，本轮选择 `AssetDecisionsPage` 做一轮有限、低风险的 frontend-only IA micro-polish：保持它作为 Asset Ledger 主工作队列的定位，进一步澄清统一决策队列、续费窗口证据、队列筛选上下文、行级处理动作和证据失败边界，同时不改变后端/API/数据模型/安全契约。

## What I already know

- 用户已明确授权页面 IA 批次可由我自主选择最优顺序推进，无需确认页面顺序。
- 最近已完成 IA polish 的范围包括 NodeDetail、VPSDetail、Settings、Targets+Nodes 列表、NodeCompare、VPSPage inventory、NodeOnboarding、TargetDetailPage、ProvidersPage + SubscriptionsPage。
- 两份 research 均确认剩余大页面大多已对齐 v2；`LoginPage` 更安全但价值很小，`AssetDecisionsPage` 是剩余主要操作页里最高收益但必须保持小范围的候选。
- 候风当前仍处早期开发阶段，可以优先统一改善页面体验，但每个批次仍需保持低风险和契约冻结。
- Houfeng 开发必须在 feature 分支进行，当前分支为 `feature/next-page-ia-batch-3-20260521`。

## Requirements

- 将 `AssetDecisionsPage` 的本轮范围限定为微优化，不做队列模型或页面结构重写。
- 保持 `资产决策工作队列` 为主扫描路径，`RENEWAL EVIDENCE / 续费候选证据` 为次级证据区。
- 澄清队列 header/summary/focus/empty/error/notice 文案，让用户更明确：
  - 当前队列排序依据来自续费窗口、未评估、迁移/取消、Node 关联数量和订阅证据缺口；
  - subscription evidence failure 不是“缺订阅”的事实；
  - Node 信号只展示关联数量，不展示或暗示 linked node health；
  - Drawer 是处理单台 VPS 续费决策的次级面板。
- 保持高密度队列行和 row navigation/action isolation；不要把队列替换成卡片墙或完整表单。
- 只做 IA composition/copy/CSS/test polish，不新增 API 请求、字段、路由、依赖、图表或 CSS 系统。
- 修改范围默认限定在：
  - `web/src/pages/AssetDecisionsPage.tsx`
  - `web/src/pages/AssetDecisionsPage.test.tsx`
  - `web/src/styles/pages.css`
- 可选仅在必要且行为中性时修改已存在的 `AssetDecisionWorkPanel` / `AssetDecisionRenewalTable` copy/class hook；不改变它们的 API 或 shared semantics。

## Frozen Contracts

- 保留 API helper usage：`listSubscriptions`、`listVPSAssets`、`updateVPSAsset`。
- 保留初始请求形状和顺序：
  - `listSubscriptions({ renew_within_days: 30|60|90, sort: 'renew_at', order: 'asc' })`
  - `listSubscriptions({ sort: 'renew_at', order: 'asc' })`
  - `listVPSAssets({ renewal_decision: 'unreviewed' | 'migrate' | 'cancel' })`
- 保留 renewal-window query mapping：`renew_within_days=<30|60|90>&sort=renew_at&order=asc`。
- 保留 queue tabs 和派生语义：`all`、`unreviewed`、`renewal`、`migrate`、`cancel`、`unlinked`、`missing_subscription`。
- 保留 `updateVPSAsset` PATCH body：只发送 `renewal_decision` 和可选非空 `renewal_reason`。
- 保留 subscription evidence failure boundary：如果全量订阅证据读取失败，显示队列错误，不把所有 VPS 渲染成 `缺订阅`。
- 保留 `monthly_price` 为 backend-computed display-only subscription evidence；前端不计算、不提交。
- 保留 `active_node_link_count` 为 count-only；不发明 linked-node health、heartbeat、incident、freshness 事实。
- 保留 row navigation 到 `/vps/:vpsId`，并保留内部 `详情` / `处理` actions 的 `stopPropagation` 隔离。
- 保留 Drawer cancel/Escape/overlay close 丢弃 draft/error 且不提交。
- 保留保存成功 notice 出现在队列 surface，而不是只在已关闭 Drawer 内。

## Acceptance Criteria

- [ ] PRD 明确本轮页面范围、目标、冻结契约、验收标准和 out-of-scope。
- [ ] Research 文件说明候选页审计、设计/spec 对照和选择理由。
- [ ] 实现改动只触及本轮页面、对应测试、`pages.css` 与必要既有组件。
- [ ] 新增/更新测试覆盖新增 IA 文案/结构，并继续覆盖 request shapes、renewal-window reload、PATCH payload、queue movement、empty action、row/action isolation、Drawer discard、subscription evidence failure truthfulness。
- [ ] `npm --prefix web run lint` 通过。
- [ ] `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` 通过。
- [ ] `npm --prefix web run build` 通过。
- [ ] `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` 通过。
- [ ] UI/browser sanity 覆盖 `/asset-decisions` golden path，并明确任何 mock/local/auth caveat。
- [ ] Trellis task 归档，feature 分支 clean，按既定 PR/release flow 跟进完成。

## Technical Approach

1. 在现有 `AssetDecisionsPage` 架构内微调，不改变数据流、queue builder、tabs、Drawer 或 row action semantics。
2. 增强队列顶部 framing：让 `资产决策工作队列` 明确是 Asset Ledger 的处理入口，并说明当前排序和证据来源。
3. 适度强化 summary/focus/queue row/empty/error copy：突出“证据边界”和“下一步动作”，避免把读取失败或缺字段伪装成真实业务判断。
4. 必要时在 `pages.css` 补少量 token/BEM-ish class，保持和现有 Asset Ledger 支撑页一致。
5. 更新 `AssetDecisionsPage.test.tsx`：为新增 IA copy/structure 加断言，同时保留既有冻结契约断言。

## Decision (ADR-lite)

**Context**: ProvidersPage + SubscriptionsPage 已完成后，两份 research 都没有发现新的广泛高收益且低风险页面组。LoginPage 是最安全的残余候选但价值很小；Dashboard/Events 已高度对齐且契约风险高；NodeOnboarding/Settings 等安全敏感或近期完成页面不适合 speculative polish。

**Decision**: 本轮选择 `AssetDecisionsPage`，但明确限定为 targeted IA micro-polish，不做重写。

**Consequences**: 收益集中在 Asset Ledger 主工作队列的证据边界和处理路径清晰度；风险通过冻结 API/URL/payload/Drawer/row semantics 和测试覆盖控制。若后续没有具体缺陷，剩余页面 IA 批次应逐渐转向 real-use findings，而不是继续广泛 speculative polish。

## Out of Scope

- 不修改 backend、database migration、API request/response shape 或 Asset Ledger 数据模型。
- 不新增 VPS/Subscription/Node join，不展示 linked-node health、heartbeat、incident 或 freshness。
- 不改变 queue builder、priority formula、tab semantics、renewal-window options 或 PATCH payload。
- 不改变 row navigation/action isolation、Drawer reset/discard、success notice placement。
- 不引入新依赖、图表库、CSS framework、page-local CSS、新路由或导航分组。
- 不重做 DashboardPage、EventsPage、LoginPage、NodeOnboardingPage、SettingsPage 或任何近期完成页面。

## Research References

- [`research/remaining-page-ia-audit.md`](research/remaining-page-ia-audit.md) — 剩余 routes/pages 和近期 IA 归档审计，推荐 AssetDecisionsPage targeted micro-polish 作为剩余主要操作页候选。
- [`research/design-spec-candidate-audit.md`](research/design-spec-candidate-audit.md) — v2 design/spec fit 审计，指出 LoginPage 是最安全小候选但价值低，广泛页面 polish 应收敛到具体问题。

## Technical Notes

- Current task: `.trellis/tasks/05-21-next-page-ia-batch-3`。
- Feature branch: `feature/next-page-ia-batch-3-20260521`。
- Current implementation files inspected: `AssetDecisionsPage.tsx`、`AssetDecisionsPage.test.tsx`、`pages.css`。
- Relevant specs: `.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/web/quality-guidelines.md`、`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。

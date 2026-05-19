# 优化 VPS 详情页信息结构

## Goal

让 VPS 详情页从“字段与区块平铺”收敛为资产运营判断页：首屏优先回答这台 VPS 是否值得续费/保留，突出续费/成本、生命周期、关联 Node/服务/域名数量和主要风险；把低价值资料、重复证据和危险/编辑动作降噪或收口，减少滚屏成本和“信息很多但判断很少”的假象。

## What I already know

- 用户选择 VPS Detail 的主任务为“资产运营判断”：它应优先服务“是否值得续费/保留”，而不是运行态监控或资料档案。
- 参考刚完成的 Node Detail 信息架构优化：操作收口、事实按价值分层、重复/低价值下方区域移除。
- 当前 `web/src/pages/VPSDetailPage.tsx` 已经存在 `VPSDecisionWorkbench`，但页面下方仍顺序渲染续费证据、决策证据、Node 表、基础信息、服务表、域名表、Timeline、访问摘要、生命周期危险区，形成长滚动。
- 当前 `VPSDetailHero` 的主操作是 `处理决策`、`编辑资料`、`返回`、`VPS 列表`，动作没有像 Node Detail 一样收口到右上角菜单。
- 当前 `VPSFactsSection` 平铺 VPS ID、Provider ID、产品名、订单号、数据中心、重要性、IP/SSH、OS、虚拟化、归档时间、备注等大量资料字段，会放大“资料很多”的错觉。
- 当前 `VPSAccessSummarySection` 只展示 SSH/IP 入口，价值接近基础资料摘要，独立占一整块页面空间。
- 当前 `VPSLifecycleCard` 独立占据底部危险区；归档/恢复是操作，不应作为常规信息区平铺，但确认流程需要保留。
- 当前服务/域名/Node section 都是可用数据区，但全量表格都直接展开，可能应改为“概览优先、详情后置”。

## Assumptions (temporary)

- 不改后端数据模型和 API，本任务主要是前端信息架构、组件重排和测试更新。
- 不删除现有业务能力：调整续费决策、编辑资料、关联/解除 Node、记录经验、新增服务、新增域名、归档/恢复仍可用。
- 不把 VPS Detail 改成 Node Detail 的运行面板；Node 运行健康只作为资产判断证据。
- 低价值字段可以降级为折叠/摘要/Drawer 内编辑资料，而不是全部首屏平铺。

## Open Questions

- None.

## Requirements (evolving)

- 首屏主线必须围绕资产运营判断：续费/成本、当前决策、生命周期、关联 Node/服务/域名数量、主要风险或下一步动作。
- 采用“摘要卡 + Drawer/详情表后置”策略：主页面只保留支持判断的 Node、服务、域名、最近历史摘要，全量表格和低价值资料后置。
- 采用“主 CTA + 右上角 actions menu”策略：Hero/工作台只保留最关键的处理/调整决策主按钮；编辑资料、记录经验、关联 Node、新增服务、新增域名、归档/恢复等操作进入右上角 actions menu。
- 减少页面纵向长度，避免用户需要连续滚动三四屏才能看完关键内容。
- 动作入口不应散落在多个平铺 section 中；摘要卡可表达缺口和跳转意图，但不重复渲染一堆操作按钮。
- 保留现有 Drawer 表单与提交/取消状态清理合同。
- 保留归档危险操作确认，不允许从菜单直接无确认归档。
- 使用现有 v2 设计令牌、BEM、`pages.css`，不新增 CSS 框架或组件局部 CSS。

## Acceptance Criteria (evolving)

- [ ] VPS Detail 首屏能直接看到资产判断主结论/下一步动作、续费成本/窗口、Node 证据、服务/域名上下文数量和生命周期状态。
- [ ] 页面不再把基础资料、访问摘要、生命周期危险区作为同级大块连续平铺。
- [ ] 右上角 actions menu 包含编辑资料、记录经验、关联 Node、新增服务、新增域名、归档/恢复等操作；主页面只保留处理/调整决策主 CTA。
- [ ] 低价值或缺失信息不制造“信息很多”的假象；缺失时以明确空态/缺口表达。
- [ ] Node/服务/域名/Timeline 仍可查看，但主页面默认只展示判断所需摘要，全量内容后置到 Drawer/详情入口。
- [ ] 更新 `VPSDetailPage.test.tsx` 覆盖新布局、操作入口和关键能力保留。
- [ ] Web lint/test/build 通过，最终完整验证通过。

## Definition of Done

- 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- 跨前后端最终优先跑 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- 对照 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`；如果无法浏览器 sanity，最终报告明确说明。
- Trellis implement/check jsonl 在 `task.py start` 前完成。

## Out of Scope (explicit)

- 不改 VPS / Subscription / Node / Service / Domain 后端模型。
- 不新增真实成本算法、自动续费策略或智能推荐规则。
- 不新增图表库、CSS 框架或单页局部 CSS。
- 不删除已有业务操作能力。
- 不把服务/域名扩展成完整 DNS、Registrar 或服务编排管理。

## Technical Approach

- Keep the VPS Detail data-fetching and mutation contract intact; refactor the presentation layer and tests only unless implementation discovers a strict need otherwise.
- Introduce/reshape a compact VPS asset-operations header/workbench: first-screen evidence should expose decision, renewal/cost, lifecycle, Node health, service/domain counts, and latest history/risk.
- Add a VPS Detail top-right actions menu similar in spirit to Node Detail’s action consolidation. Keep the primary decision CTA visible; move secondary operations into the menu.
- Replace full-width lower evidence/table sections with compact summary cards and explicit detail entry points. Reuse existing Drawer infrastructure where practical; do not create a new page-level navigation system unless unavoidable.
- Remove or demote low-value standalone sections: baseline facts grid, access summary, lifecycle danger card, duplicated renewal/decision evidence cards, and always-expanded service/domain/Node tables.
- Preserve all existing forms, API calls, feedback messages, cancellation reset behavior, archive confirmation, and links to subscriptions/nodes/targets.

## Decision (ADR-lite)

**Context**: VPS Detail currently gives an impression of rich information by vertically stacking many flat sections, but the page’s actual product job is to help decide whether a VPS should be renewed, kept, watched, migrated, cancelled, or archived.

**Decision**: Optimize the page around asset-operations judgment. Use summary cards plus detail/drawer entry points, and consolidate secondary operations into a top-right actions menu while leaving decision handling as the primary CTA.

**Consequences**: The page becomes shorter and more opinionated. Some data moves one click deeper, but the default view becomes more truthful: it shows what supports the renewal/retention decision instead of presenting every sparse field as equal.

## Research References

- [`research/browser-sanity.md`](research/browser-sanity.md) — Fixture-backed browser sanity confirmed the compact default VPS Detail IA, actions menu, and Drawer-backed detail/form entries in the real Vite/React UI.

## Verification Evidence

- `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` passed on 2026-05-19: Go tests, web lint, 63 web test files / 481 tests, and web production build all succeeded.
- Browser sanity passed with injected fetch fixtures because no local center was listening on `127.0.0.1:8080` or `127.0.0.1:16001`; this verifies real frontend rendering/interactions but not live backend auth/data.

## Technical Notes

- Likely files: `web/src/pages/VPSDetailPage.tsx`、`web/src/pages/vps-detail/VPSDetailHero.tsx`、`VPSDecisionWorkbench.tsx`、`VPSFactsSection.tsx`、`VPSLifecycleCard.tsx`、`VPSAccessSummarySection.tsx`、`VPSNodeLinksSection.tsx`、`VPSServicesSection.tsx`、`VPSDomainsSection.tsx`、`VPSDecisionEvidenceSection.tsx`、`VPSRenewalEvidenceSection.tsx`、`web/src/styles/pages.css`、`web/src/pages/VPSDetailPage.test.tsx`。
- Relevant specs: `.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/web/quality-guidelines.md`、`.trellis/spec/guides/code-reuse-thinking-guide.md`。
- Current branch: `feature/vps-detail-info-architecture`.

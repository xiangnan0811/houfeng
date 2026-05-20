# VPSPage inventory information architecture polish

## Goal

对 `VPSPage` 做一轮有限、低风险的信息架构 polish，让资产库存默认视图更清楚地区分“库存判断入口”“筛选/证据状态”“列表行动区”和“异常/缺失证据边界”，同时保留当前 URL-state filters、advanced filter Drawer、DataTable row behavior、provider/subscription join 和后端契约。

## Requirements

- 仅纳入 `web/src/pages/VPSPage.tsx`、`web/src/pages/VPSPage.test.tsx`、`web/src/styles/pages.css` 与必要的现有 Asset/VPS 子组件；不扩展到其他页面。
- 保留当前 `/vps` 页面模型：page identity、quick views、filter chips、advanced filter Drawer、VPS inventory DataTable、create Drawer。
- 默认扫描路径要更清楚地区分：当前 quick view/lens、subscription evidence readiness、active field filters、table work area 和缺失证据边界。
- 使用现有 VPS/provider/subscription 数据改善层级、文案和密度，不新增后端字段、图表、API 请求或真实库存验证能力。
- 使 subscription evidence state 更显式；当 evidence loading/error 时，不得把 VPS 错判为 `缺订阅`。
- 强化 `VPS 库存表` 作为主工作区的 framing，可展示当前 view/filter/evidence/count 上下文，但不改 columns、row key、row click 或 cell actions。
- 样式改动进入 `pages.css`，使用 BEM-ish 命名和 design tokens；不新增 CSS 系统、page-local CSS 或依赖。

## Frozen Contracts

- URL query/filter contract 保持不变：`view`、`provider_id`、`lifecycle_status`、`usage_status`、`renewal_decision`。
- Quick view values 保持不变：`all`、`renewal`、`unreviewed`、`unlinked`、`missing_subscription`、`missing_facts`、`archived`。
- `parseFilters`/`filterToQuery` fallback/serialization 语义保持不变；filter 写入继续使用 replace 语义，不因 client-side filter 变化触发额外 refetch。
- Advanced filter Drawer 保持 draft/apply/discard：打开复制当前 applied filters，`应用筛选` 才写 URL；`关闭`、Escape、overlay 关闭丢弃 draft；`重置` 只重置 drawer draft 直到 apply。
- `DataTable` row click 继续导航到 `/vps/:vpsId`；行内 button/link/select/input 等交互继续由 DataTable guard 隔离，不触发行导航。
- Subscription evidence failure truthfulness 保持不变：loading/error 时显示 unknown/unavailable state，不显示 table `缺订阅` / `无法核算续费` 事实，不把 missing subscription count 置为非 0。
- `view=missing_subscription` 只有在 subscription evidence ready 且 row 无 subscription 时才匹配。
- Provider master data 只用于 filter options/chip labels/create selector；table provider display 继续来自 VPS row snapshot `vps.provider_name`。
- Subscription join 继续通过 `groupSubscriptionsByVPS` + `selectPrimarySubscription`，不改 active/renew date 排序语义。
- API request shapes 保持不变：initial load 仍是 `/api/vps`、`/api/providers`、`/api/subscriptions?sort=renew_at&order=asc`；create payload shape 不变。
- 不修改 `web/src/lib/api.ts`、`web/src/lib/types.ts`、后端/API/data model/import flow。

## Acceptance Criteria

- [x] `VPSPage` 默认扫描路径更清楚：用户能先看到当前库存 lens、证据状态、filter 状态，再进入 `VPS 库存表` 主工作区。
- [x] Subscription evidence readiness 有清晰展示；evidence loading/error 时不错误表达 `缺订阅` 事实。
- [x] `VPS 库存表` header/framing 使用现有 count/view/evidence/filter 信息增强上下文，但 DataTable columns、row navigation、row actions 不变。
- [x] URL-state filters、quick view semantics、advanced drawer draft/apply/discard、subscription evidence failure behavior、provider/subscription join、API request shapes 保持不变并有测试覆盖。
- [x] Create Drawer 与 create POST payload 行为保持不变。
- [x] 样式改动只进入 `pages.css`，遵循 BEM/tokens，不新增 CSS 系统、page-local CSS 或依赖。
- [x] `VPSPage.test.tsx` 更新覆盖新增 IA 文案/结构，同时保留 frozen contract 断言。
- [x] 前端验证通过：`npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- [x] 最终完整验证优先通过：`TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [x] UI/browser sanity 覆盖 `/vps` golden path；使用 mock-backed `asset-workflows` 等效数据，已明确 caveat。

## Technical Approach

- 保持 `VPSPage` 单页面数据流和当前 URL-backed filter 架构，只调整 command surface/table header 的 IA composition 与 CSS。
- 在 `库存核对` command surface 内拆清楚三层：quick view/lens、subscription evidence readiness/current scope、field filter controls/chips。
- 对 evidence state 使用已有 `subscriptionEvidenceLabel(...)`、`subscriptionEvidence`、`state.subscriptionsError`、`filteredRows.length`、`inventoryRows.length` 等现有数据，不新增 derived truth。
- 强化 table panel title/description/summary，让它成为 quick views/filter 之后的主工作区；保留 `DataTable` 定义和 rows/cells 行为。
- CSS 复用并扩展现有 `.vps-inventory-command*`、`.vps-filter-bar*`、`.vps-inventory-table-panel*`、`.asset-*` hooks。
- 测试优先保留现有行为契约；新增断言只覆盖对用户有意义的新 IA 结构/文案。

## Decision (ADR-lite)

**Context**: `VPSPage` 已经是较成熟的高密度库存页，具备 quick views、URL filters、advanced Drawer 和 evidence-aware subscription join。当前风险不在缺功能，而在 command surface 同时承载 lens、evidence、filters、table framing，默认扫描路径仍可更清楚。

**Decision**: 采用“composition/copy/CSS polish only”的有限方案。只强化库存核对层级、subscription evidence 状态和表格主工作区 framing，不改变 URL/API/data/join/filter/create 行为。

**Consequences**: 本轮能提升库存页可读性和证据边界表达，风险低；但不会重构 Asset Ledger 数据模型、真实库存验证、导入流程或 table column semantics。后续若真实使用暴露具体库存判断问题，应另开功能/数据任务。

## Research References

- [`research/vps-page-ia-audit.md`](research/vps-page-ia-audit.md) — 静态审计确认 VPSPage 当前结构、冻结 URL/filter/evidence/join 契约、安全 polish seams、验收标准与验证重点。
- [`research/browser-sanity.md`](research/browser-sanity.md) — DevTools/mock-backed 浏览器 sanity 覆盖 `/vps` desktop/mobile、quick view、filter drawer 与 create drawer 路径。

## Definition of Done

- [x] PRD、research、implement/check JSONL 完成并归档。
- [x] 任务通过 `trellis-implement` 实现与 `trellis-check` 独立检查。
- [x] 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- [x] 最终完整验证通过 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [x] UI/浏览器 sanity 覆盖 VPSPage golden path；使用 mock-backed 数据，已明确非真实 authenticated center/PostgreSQL caveat。
- [ ] 按分支/PR/release 约定完成后续流程。

## Out of Scope

- 不改后端模型/API、VPS/provider/subscription repository、导入器或 migrations。
- 不改 `web/src/lib/api.ts`、`web/src/lib/types.ts`、filter query param names、quick view values 或 API request shapes。
- 不改 provider/subscription join 规则、missing subscription truthfulness、provider row snapshot display 或 real-data validation boundary。
- 不移动 field filters 到常驻控件，不移除/禁用/重命名 `缺订阅` tab。
- 不新增 linked-node health、provider risk、billing risk、import validation 或真实库存状态等当前 contract 不存在的事实。
- 不纳入 VPSDetailPage、ProvidersPage、SubscriptionsPage、AssetDecisionsPage、DashboardPage、NodeOnboardingPage、EventsPage 或 docs/specs 更新。
- 不引入新依赖、图表库、设计系统、CSS 框架、CSS Modules、CSS-in-JS 或 page-local CSS。

## Technical Notes

- Current task: `.trellis/tasks/05-20-vps-page-inventory-ia-polish`。
- Changed files: `web/src/pages/VPSPage.tsx`, `web/src/pages/VPSPage.test.tsx`, `web/src/styles/pages.css`。
- `trellis-implement` completed the IA composition/CSS/test changes and verified focused VPSPage tests, lint, full web tests, build, and full repo verify.
- `trellis-check` found one evidence-truthfulness issue in the inventory focus meta while subscription evidence was loading; it fixed the copy to avoid implying `缺订阅 0` before evidence is ready and added jsdom coverage for `view=missing_subscription` loading-to-ready behavior.
- Main-session verification passed:
  - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run src/pages/VPSPage.test.tsx` — PASS, 1 file / 5 tests.
  - `npm --prefix web run lint` — PASS.
  - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` — PASS, 63 files / 488 tests.
  - `npm --prefix web run build` — PASS.
  - `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` — PASS.
- Local verification caveats: npm emitted the existing engine warning because local Node is `v24.14.1` while `web/package.json` requires `22.x`; npm audit still reports 1 moderate vulnerability.
- Browser sanity evidence: `research/browser-sanity.md` PASS via DevTools-based local Vite run with mock-backed `asset-workflows` equivalent data at `1440x1000` and `390x900`; Python Playwright was unavailable, so this was not a direct `scripts/visual_evidence.py` run and did not exercise real authenticated center/PostgreSQL.

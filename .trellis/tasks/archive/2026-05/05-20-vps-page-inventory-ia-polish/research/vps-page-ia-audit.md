# Research: VPSPage inventory IA audit

- **Query**: Research VPSPage inventory information architecture polish scope. Goal: frontend-only, limited IA/UI polish for `VPSPage`, preserving URL filters, advanced drawer semantics, DataTable row guards, evidence-aware subscription behavior, provider/subscription joins, API/data/import boundaries, and no new deps/CSS systems.
- **Scope**: internal
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/VPSPage.tsx` | Route/page container for VPS inventory. Owns URL-backed filter parsing/serialization, quick views, provider/subscription fetches, client-side join/filter/rank, inventory command surface, DataTable, create drawer, and advanced filter drawer. |
| `web/src/pages/VPSPage.test.tsx` | Regression tests for quick views, filter chips, drawer apply/discard, subscription evidence failure, missing subscription classification, row navigation, create drawer, and exact API request/payload shapes. |
| `web/src/pages/assetPageUtils.ts` | Shared Asset Ledger helpers for label parsing, subscription grouping/primary selection, renewal timing/window, VPS location/access labels, and quality issue derivation. |
| `web/src/pages/assetPageBadges.tsx` | Shared VPS lifecycle/usage/renewal badge wrappers and label rendering. |
| `web/src/lib/api.ts` | API helpers used by the page: `listVPSAssets`, `createVPSAsset`, `listProviders`, `listSubscriptions`; request helper sets `Accept`, `cache: no-store`, and `credentials: include`. |
| `web/src/lib/types.ts` | Frontend contract types and label maps for `VPSAssetRecord`, VPS status enums, `CreateVPSAssetInput`, `VPSAssetListFilter`, `ProviderRecord`, `SubscriptionRecord`, and `SubscriptionListFilter`. |
| `web/src/components/atoms/DataTable.tsx` | Shared semantic table atom with clickable-row interactive-descendant guard for links/buttons/inputs/selects/textareas/roles. |
| `web/src/components/atoms/DataTable.test.tsx` | Atom tests for row click, interactive cell action guard, keyboard navigation, and empty content. |
| `web/src/components/atoms/Drawer.tsx` | Shared portal drawer used by VPS create and advanced filter drawers; uses modal focus behavior, overlay/Escape close, and dialog semantics. |
| `web/src/components/filters/FilterBar.tsx` | Shared filter controls/chips wrapper with `清空所有` behavior. |
| `web/src/components/filters/FilterChip.tsx` | Shared removable chip; remove button accessible name is `移除筛选 ${label}`. |
| `web/src/components/filters/FilterSelect.tsx` | Shared controlled select used in the advanced drawer. |
| `web/src/styles/pages.css` | Existing VPS/asset page styles: inventory command layout, focus items, table row treatment, subscription/quality cells, create/filter drawers, and responsive table behavior. |
| `.trellis/spec/web/state-and-data.md` | Asset Ledger/VPS inventory data-flow contracts, URL-state quick view/filter names, evidence failure matrix, and required tests. |
| `.trellis/spec/web/component-conventions.md` | DataTable row-click contract, Drawer discard/reset contract, Drawer-first list workflow guidance, and selector/association guidance. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS, BEM, design-token, dark-first, no page-local CSS/CSS-in-JS/Tailwind constraints. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification commands, page-test patterns, and browser-sanity guidance. |
| `docs/design/v2-houfeng/component-spec.md` | Active visual/IA contract for `VPSPage`: high-density inventory, quick views, Drawer filters, DataTable signals, subscription join, and no invented linked-node health. |
| `docs/design/v2-houfeng/design-language.md` | Active visual language: high-density engineering-tool UI, compact spacing, DataTable density, and page hierarchy. |
| `docs/operations/v2-visual-evidence.md` | Local browser-sanity workflow and `asset-workflows` mock API profile covering `/vps`. |
| `.trellis/tasks/05-20-vps-page-inventory-ia-polish/prd.md` | Current task PRD: frontend-only IA polish, explicit out-of-scope backend/API/model/import changes, frozen filter/evidence contracts. |
| `.trellis/tasks/archive/2026-05/05-20-continue-remaining-page-information-architecture-optimization/research/remaining-pages-ia-audit.md` | Prior page ranking: `VPSPage` already high-density and only suitable for minor evidence/IA polish; must preserve URL filters and subscription evidence semantics. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-remaining-page-information-architecture/research/overview-list-pages-audit.md` | Prior overview/list audit: `VPSPage` already a strong inventory-command reference; main risk is weakening subscription-evidence semantics. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-remaining-page-information-architecture/research/design-patterns-for-page-ia.md` | Reusable IA patterns: page job first, summary/focus signals, Drawer for interrupting workflows, DataTable for dense inventories, no invented facts. |
| `.trellis/tasks/archive/2026-05/05-11-ux-asset-decision-vps-list/research/current-asset-pages-audit.md` | Historical Asset Ledger audit that recommended quick views + chips + advanced drawer and client-side subscription enrichment; now partially superseded by current `VPSPage`. |

### Current IA Structure

#### Visible page order

1. **Page identity / create action** — `<section className="page-panel page-panel--inline">` renders eyebrow `ASSET LEDGER`, title `VPS`, explanatory copy, and primary create button (`web/src/pages/VPSPage.tsx:678-691`).
2. **Inventory command surface** — `<section className="page-panel vps-inventory-command">` renders heading `库存核对`, total/current count, quick view `Tabs`, a three-item focus strip, `FilterBar`, active chips, and `高级筛选` (`web/src/pages/VPSPage.tsx:693-747`).
3. **Subscription evidence failure notice** — only when subscription evidence is `error`, the page renders `订阅证据不可用，缺订阅视图暂不作为事实。...` with `role="status"` (`web/src/pages/VPSPage.tsx:742-746`).
4. **Inventory table panel** — `<section className="page-panel page-panel--scroll-x vps-inventory-table-panel">` renders `VPS 库存表`, current row count, `PageState` loading/error, or the `DataTable` (`web/src/pages/VPSPage.tsx:749-786`).
5. **Create drawer** — `Drawer` with `ariaLabel="VPS 创建表单"`, grouped create form sections (`基础识别`, `访问入口`, `运行与决策`, `备注标签`), and submit/cancel actions (`web/src/pages/VPSPage.tsx:788-881`).
6. **Advanced filter drawer** — `Drawer` with `ariaLabel="VPS 高级筛选"`, `FilterSelect` controls for provider/lifecycle/usage/renewal, plus `重置` and `应用筛选` (`web/src/pages/VPSPage.tsx:883-926`).

#### Data flow and derived inventory model

- The page loads VPS assets and providers together on mount with `Promise.all([listVPSAssets(), listProviders()])` (`web/src/pages/VPSPage.tsx:422-442`).
- It loads subscription evidence separately via `listSubscriptions({ sort: 'renew_at', order: 'asc' })` (`web/src/pages/VPSPage.tsx:452-475`).
- `subscriptionEvidence` is derived as `loading | error | ready` from `subscriptionsLoading` and `subscriptionsError` (`web/src/pages/VPSPage.tsx:481-485`).
- `groupSubscriptionsByVPS` groups records by `vps_id` and sorts each group by active status first, then renewal date (`web/src/pages/assetPageUtils.ts:78-98`). `selectPrimarySubscription` returns the first grouped subscription (`web/src/pages/assetPageUtils.ts:100-105`).
- `buildInventoryRows` joins each VPS to a primary subscription **only when evidence is ready**; otherwise `subscription` stays `null`, and `buildVPSQualityIssues` receives `includeMissingSubscription: subscriptionEvidence === 'ready'` (`web/src/pages/VPSPage.tsx:264-285`).
- `buildVPSQualityIssues` can emit only contract-backed quality labels: `缺订阅`, `未关联 Node`, `缺服务商`, `缺位置`, `缺访问入口` (`web/src/pages/assetPageUtils.ts:123-147`).
- `applyInventoryFilters` applies URL-derived field filters, then quick view matching, then ranks rows by unreviewed, renewal due, missing subscription, unlinked, and missing facts (`web/src/pages/VPSPage.tsx:288-306`, `web/src/pages/VPSPage.tsx:319-327`).
- Quick views are derived client-side with values `all`, `renewal`, `unreviewed`, `unlinked`, `missing_subscription`, `missing_facts`, `archived` (`web/src/pages/VPSPage.tsx:54-62`, `web/src/pages/VPSPage.tsx:171-179`, `web/src/pages/VPSPage.tsx:308-317`).
- Counts are calculated from `inventoryRows`: missing subscriptions count is `0` unless subscription evidence is ready (`web/src/pages/VPSPage.tsx:496-511`).

#### URL-state filter contract

- `FilterState` keys are `view`, `provider_id`, `lifecycle_status`, `usage_status`, and `renewal_decision` (`web/src/pages/VPSPage.tsx:108-114`).
- `parseFilters` reads exactly those query params, validates enum values, and falls back invalid/unsupported values to `all`/`null` (`web/src/pages/VPSPage.tsx:187-199`).
- `filterToQuery` writes exactly those query params and omits defaults/empty values (`web/src/pages/VPSPage.tsx:201-209`).
- `setFilter` and `clearFilters` write URL state using `setSearchParams(..., { replace: true })` (`web/src/pages/VPSPage.tsx:559-566`).
- The advanced drawer opens from current applied filters (`openFilterDrawer` sets `draftFilters(filters)`), and only `applyDrawerFilters` writes URL state (`web/src/pages/VPSPage.tsx:568-575`).

#### Table column structure and display truthfulness

Current columns are defined inline (`web/src/pages/VPSPage.tsx:611-674`):

| Column | Current content |
|---|---|
| `VPS` | Display name, `vps_id` through `Hostname`, and access label from `ssh_host || ipv4 || ipv6 || 接入信息缺失`. |
| `服务商 / 区域` | `vps.provider_name`, location label, and product name. This displays the VPS row snapshot, not a synthetic provider master replacement. |
| `订阅 / 续费` | Loading/unknown/missing/ready subscription cell from `renderSubscriptionCell`. |
| `决策` | Lifecycle, usage, and renewal badges. |
| `关联 / 质量` | Active Node link count or `未关联 Node`, plus quality badges. |
| `标签` | `AssetLabels`. |

`renderSubscriptionCell` is the main evidence boundary (`web/src/pages/VPSPage.tsx:348-390`):

- `loading` → `订阅读取中` / `暂不判定缺订阅`.
- `error` → `订阅未知` / `证据不可用`.
- `ready` + no subscription → `缺订阅` / `无法核算续费`.
- `ready` + subscription → monthly price, renewal date/timing, and auto-renew label.

### Current Styles / Layout Hooks

Existing `pages.css` already has page-specific VPS hooks; no new CSS file is needed:

- `.vps-inventory-command` and `.vps-inventory-command__body` define the command surface rhythm and two-column command body (`web/src/styles/pages.css:4505-4513`).
- `.vps-inventory-table-panel` gives the table panel accent border and soft shadow (`web/src/styles/pages.css:4516-4519`).
- `.vps-filter-bar .filter-bar__controls-row` and `.vps-filter-bar__summary` handle filter-control alignment and summary copy (`web/src/styles/pages.css:4521-4529`).
- `.vps-inventory-table .data-table__row` and hover treatment style the inventory rows (`web/src/styles/pages.css:4531-4541`).
- `.asset-table-empty-state`, `.asset-subscription-cell`, `.asset-quality-list`, and `.asset-quality-pill` cover table empty/subscription/quality cells (`web/src/styles/pages.css:4543-4622`).
- `.asset-create-drawer`, `.asset-filter-drawer`, and `.asset-filter-drawer__actions` cover Drawer bodies/actions (`web/src/styles/pages.css:4624-4632`, `web/src/styles/pages.css:4836-4846`).
- Responsive rules collapse the command body to one column at `max-width: 920px`, collapse focus cards to one column at `max-width: 620px`, and keep `.vps-inventory-table` horizontally scrollable with `min-width: 760px` (`web/src/styles/pages.css:5068-5070`, `web/src/styles/pages.css:5291-5357`).

## Pain Points and Safe Improvement Seams

The page is already close to the current v2 contract, so the safe seams are **composition/copy/hierarchy only**. Avoid changing data sources, query names, quick view values, row classification logic, or backend contracts.

### Pain point 1: command surface mixes four jobs in one area

The `库存核对` panel currently contains quick views, count, focus signals, filter explanation, active chips, advanced filter entry, and evidence-error notice (`web/src/pages/VPSPage.tsx:693-747`). This is functionally correct, but the default scan path can read as one dense control band instead of a hierarchy of:

1. inventory lens (quick views),
2. evidence/status summary,
3. filter editing state,
4. table work area.

**Safe seam**: split/relabel within the existing `vps-inventory-command` panel using current data only. For example, make evidence readiness/current scope visually distinct from filter controls while leaving `Tabs`, `FilterBar`, chip names, and URL writes unchanged.

### Pain point 2: subscription evidence status is partly hidden in metric metadata

The focus strip has `质量 / Node` meta that either says `缺订阅 N` or embeds the subscription evidence error label (`web/src/pages/VPSPage.tsx:526-535`). There is also a separate error notice below the command body (`web/src/pages/VPSPage.tsx:742-746`). This can make evidence readiness and quality counts feel like the same concept.

**Safe seam**: add or clarify a small evidence-status line/card inside the command surface using `subscriptionEvidenceLabel(...)` (`web/src/pages/VPSPage.tsx:392-396`). Keep the existing `missingSubscriptionCount === 0 unless ready` rule and keep the error notice/copy semantics.

### Pain point 3: missing-subscription quick view remains selectable while evidence is unavailable

The `缺订阅` tab remains present because quick view values are frozen, but when subscription evidence fails, its count is `0`, `matchesQuickView` returns true only for `ready && !row.subscription`, and selecting it hides rows rather than asserting absence (`web/src/pages/VPSPage.tsx:308-317`, `web/src/pages/VPSPage.tsx:496-511`). Tests already assert this behavior.

**Safe seam**: clarify copy around `缺订阅` when evidence is unavailable; do **not** disable/remove/rename the tab value, do **not** make rows appear as missing subscriptions, and do **not** change URL `view=missing_subscription` behavior.

### Pain point 4: provider display truthfulness is easy to accidentally change

The page fetches provider master data for filter options, chip labels, and create form provider selection (`web/src/pages/VPSPage.tsx:494`, `web/src/pages/VPSPage.tsx:398-401`, `web/src/pages/VPSPage.tsx:221-255`). The table itself renders `vps.provider_name` from the VPS row snapshot (`web/src/pages/VPSPage.tsx:624-633`).

**Safe seam**: if adding hierarchy/copy, describe row provider data as the asset snapshot and provider filter as master-data-backed. Do not silently replace table display with master provider names, because that would hide imported/stale/empty row snapshots and weaken display truthfulness.

### Pain point 5: the table work area can be visually subordinate to controls

The v2 component spec defines the first-screen structure as `页面标题 → quick views / chips / 高级筛选入口 → VPS 库存表` (`docs/design/v2-houfeng/component-spec.md:228-233`). Current code follows this, but the command panel’s focus strip and filter bar can compete with the DataTable as the actual inventory work area.

**Safe seam**: strengthen the table panel header/context using existing counts/current view/evidence status; keep the `DataTable` rows, columns, `rowKey`, and `onRowClick` behavior intact (`web/src/pages/VPSPage.tsx:777-784`).

### Pain point 6: empty/filter-empty copy is already useful but coupled to `active`

`inventoryEmptyContent` distinguishes no assets vs no matches and offers `清空筛选` plus create actions (`web/src/pages/VPSPage.tsx:538-557`). If IA polish changes labels or positioning, these CTA labels are likely to affect tests around `创建第一台 VPS` / `录入第一台 VPS` / `还没有录入 VPS 资产`.

**Safe seam**: keep the same empty-state actions and create flow; if copy changes, update tests intentionally without changing create API payload or drawer reset behavior.

## Frozen Contracts and Regression-Sensitive Tests

### 1. URL-backed filter/query contract

**Contract**

- Query params: `view`, `provider_id`, `lifecycle_status`, `usage_status`, `renewal_decision` (`web/src/pages/VPSPage.tsx:187-209`).
- Quick view values: `all`, `renewal`, `unreviewed`, `unlinked`, `missing_subscription`, `missing_facts`, `archived` (`web/src/pages/VPSPage.tsx:54-62`, `web/src/pages/VPSPage.tsx:171-179`).
- Defaults/invalid values fall back to `all`/`null` (`web/src/pages/VPSPage.tsx:187-199`).
- `setSearchParams(..., { replace: true })` is used for filter writes/clear/apply (`web/src/pages/VPSPage.tsx:559-575`).

**Tests/labels likely to fail if regressed**

- `VPSPage.test.tsx:105-164` — quick view tab `未关联`, chip `视图: 未关联`, applying drawer filter `生命周期: 测试中`, removing chips, and row navigation.
- `VPSPage.test.tsx:197-252` — `LocationProbe` expects the URL to remain `/vps?view=unlinked` after closing draft drawer by button/Escape/overlay.
- Query-sensitive chips/buttons: `视图: 未关联`, `生命周期: 测试中`, `用途: 承载业务`, `续费: 保留`, remove button accessible names like `移除筛选 生命周期...` (`web/src/components/filters/FilterChip.tsx:13-18`).

### 2. Advanced filter drawer draft/apply/reset/close-discard semantics

**Contract**

- `openFilterDrawer()` copies applied `filters` into `draftFilters` (`web/src/pages/VPSPage.tsx:568-571`).
- `applyDrawerFilters()` serializes `draftFilters` to URL and closes the drawer (`web/src/pages/VPSPage.tsx:573-576`).
- Drawer `onClose={() => setFilterDrawerOpen(false)}` closes without applying draft (`web/src/pages/VPSPage.tsx:883-887`).
- `重置` only resets `draftFilters` inside the drawer until `应用筛选` is clicked (`web/src/pages/VPSPage.tsx:914-923`).
- Shared `Drawer` closes on overlay mouse down, close button, and Escape via `useModalFocus` (`web/src/components/atoms/Drawer.tsx:4`, `web/src/components/atoms/Drawer.tsx:37-61`).

**Tests/labels likely to fail if regressed**

- Dialog accessible name: `VPS 高级筛选` (`web/src/pages/VPSPage.test.tsx:152`, `web/src/pages/VPSPage.test.tsx:226`).
- Controls/labels: `服务商`, `生命周期`, `用途状态`, `续费决策`, `重置`, `应用筛选`, close button `关闭` (`web/src/pages/VPSPage.tsx:890-923`, `web/src/components/atoms/Drawer.tsx:51-56`).
- `VPSPage.test.tsx:197-252` asserts button close, Escape, and `.drawer-overlay` do not change URL/chips and do not refetch (`expect(fetchMock).toHaveBeenCalledTimes(3)`).

### 3. DataTable row navigation vs row action propagation guards

**Contract**

- `DataTable` ignores row click/keyboard activation when the event target is inside `a[href]`, `button`, `input`, `select`, `textarea`, `[role="button"]`, or `[role="link"]` (`web/src/components/atoms/DataTable.tsx:3-15`, `web/src/components/atoms/DataTable.tsx:139-158`).
- `VPSPage` row click navigates to `/vps/{vps_id}` (`web/src/pages/VPSPage.tsx:777-784`).

**Tests/labels likely to fail if regressed**

- `VPSPage.test.tsx:105-164` clicks `Tokyo Edge` and expects `vps detail route`.
- `DataTable.test.tsx:49-80` asserts interactive button actions do not emit row click and keyboard activation ignores child controls.
- If IA polish adds any row-level buttons/links, they must remain interactive descendants so the existing DataTable guard applies.

### 4. Subscription evidence failure behavior

**Contract**

- Subscription evidence states: `loading | ready | error` (`web/src/pages/VPSPage.tsx:63`, `web/src/pages/VPSPage.tsx:481-485`).
- Missing subscription classification only occurs when evidence is `ready`: `buildVPSQualityIssues(... includeMissingSubscription: subscriptionEvidence === 'ready')`, `renewalDue: subscriptionEvidence === 'ready' && ...`, `matchesQuickView('missing_subscription')` requires `row.subscriptionEvidence === 'ready' && !row.subscription`, and `missingSubscriptionCount` is `0` when not ready (`web/src/pages/VPSPage.tsx:264-285`, `web/src/pages/VPSPage.tsx:308-317`, `web/src/pages/VPSPage.tsx:496-511`).
- Error cell displays `订阅未知` / `证据不可用`, not `缺订阅` (`web/src/pages/VPSPage.tsx:358-365`).
- Error notice says `订阅证据不可用，缺订阅视图暂不作为事实。...` (`web/src/pages/VPSPage.tsx:742-746`).

**Tests/labels likely to fail if regressed**

- `VPSPage.test.tsx:166-195` asserts row remains visible under `/vps?view=unknown&renewal_decision=unreviewed`, shows `订阅未知`, `证据不可用`, and the evidence notice, and does **not** show table `缺订阅` or `无法核算续费`.
- Same test clicks tab `缺订阅` and expects `Osaka Missing` to disappear, then removing `视图` returns it.
- `VPSPage.test.tsx:254-273` asserts missing subscriptions appear only after ready evidence with empty subscriptions, including `缺订阅` and `无法核算续费`.

### 5. Provider/subscription join behavior and display truthfulness

**Contract**

- `listProviders()` is used for provider options and chip label lookup (`web/src/lib/api.ts:420-422`, `web/src/pages/VPSPage.tsx:257-262`, `web/src/pages/VPSPage.tsx:398-401`).
- Table provider display comes from `vps.provider_name`, with `formatOptional` fallback, and should not be silently overridden with provider master data (`web/src/pages/VPSPage.tsx:624-633`).
- `buildCreateInput` can set `provider_name` from the selected provider name when creating, but this is a create payload behavior, not a row display join (`web/src/pages/VPSPage.tsx:221-255`).
- `groupSubscriptionsByVPS` and `selectPrimarySubscription` define the subscription join; active subscriptions sort before inactive, then earlier renewal date (`web/src/pages/assetPageUtils.ts:78-105`).

**Tests/labels likely to fail if regressed**

- Initial fetch order/shape is asserted exactly: `/api/vps`, `/api/providers`, `/api/subscriptions?sort=renew_at&order=asc` (`web/src/pages/VPSPage.test.tsx:130-144`).
- Row display expects provider and subscription facts like `Hetzner`, `USD 12.00/月`, `保留`, `承载业务` (`web/src/pages/VPSPage.test.tsx:122-129`).
- Create test expects selected provider `pv_001` to produce payload `provider_id: 'pv_001'` and `provider_name: 'Hetzner'` (`web/src/pages/VPSPage.test.tsx:322-354`).

### 6. API request shapes, backend/data model, import flow, and real-data boundaries

**Contract**

- `listVPSAssets(filter?)` serializes only backend-supported field filters (`provider_id`, `lifecycle_status`, `usage_status`, `renewal_decision`) (`web/src/lib/api.ts:466-475`). `VPSPage` currently calls it without filter, because quick views are derived client-side (`web/src/pages/VPSPage.tsx:422-423`).
- `listSubscriptions(filter?)` supports `vps_id`, status/renew range/window, and sort/order; `VPSPage` uses only `sort=renew_at&order=asc` (`web/src/lib/api.ts:556-568`, `web/src/pages/VPSPage.tsx:452`).
- `createVPSAsset` posts to `/api/vps` with current `CreateVPSAssetInput` fields (`web/src/lib/api.ts:477-479`, `web/src/lib/types.ts:745-768`).
- No backend/API/model/import changes are needed or safe for this IA pass.

**Tests/labels likely to fail if regressed**

- `VPSPage.test.tsx:130-144` exact initial GET calls.
- `VPSPage.test.tsx:157` and `VPSPage.test.tsx:251` assert client-side filter changes and drawer closes do not trigger extra GETs.
- `VPSPage.test.tsx:322-354` exact POST body for create flow.

### 7. CSS/dependency/styling constraints

**Contract**

- No Tailwind/CSS-in-JS/chart libs or new deps; styling remains pure CSS + tokens + BEM (`.trellis/spec/web/styling-guidelines.md:9-17`, `.trellis/spec/web/styling-guidelines.md:138-151`).
- No new page-local CSS file; page-specific styling should land in `web/src/styles/pages.css` (`.trellis/spec/web/styling-guidelines.md:95-112`, `.trellis/spec/web/styling-guidelines.md:146-150`).
- High-density engineering UI should use compact spacing and existing typography/mono wrappers (`docs/design/v2-houfeng/design-language.md:147-170`, `.trellis/spec/web/styling-guidelines.md:129-135`).

## Recommended Narrow Implementation Scope

Recommended safe scope: **one frontend-only VPSPage IA polish pass that clarifies default scan hierarchy without changing data contracts**.

### In scope

1. **Clarify the `库存核对` command surface hierarchy**
   - Keep the existing quick view `Tabs` and URL `view` values.
   - Separate “current inventory lens”, “evidence readiness”, and “field filters” visually/copy-wise inside the existing command surface.
   - Keep `FilterBar`, chip labels, `高级筛选`, `清空所有`, and drawer controls behaviorally unchanged.

2. **Make subscription evidence state more explicit**
   - Use existing `subscriptionEvidenceLabel(...)` / `state.subscriptionsError` data.
   - Preserve the existing error notice copy or update tests intentionally if copy changes.
   - Do not classify `缺订阅` unless evidence is ready.

3. **Strengthen the table work-area framing**
   - Make `VPS 库存表` read as the primary scan/work area after command controls.
   - Use existing `filteredRows.length`, `inventoryRows.length`, active view label, and evidence state.
   - Do not change table columns, row key, row navigation, or create flow unless strictly necessary.

4. **Small CSS additions/adjustments only in `pages.css`**
   - Reuse existing blocks (`vps-inventory-command`, `vps-inventory-focus`, `vps-filter-bar`, `vps-inventory-table-panel`, `asset-subscription-cell`).
   - Use BEM-ish classes and design tokens.
   - Keep responsive behavior: command body one-column at narrow width and table horizontal scroll.

5. **Update `VPSPage.test.tsx` only for intentional visible IA/copy changes**
   - Keep existing tests for query/fetch/evidence/drawer/create behavior green.
   - Add assertions for any new evidence-status or hierarchy labels that are product-significant.

### Out of scope

- Backend/API/data-model/migration/import changes.
- Changing `web/src/lib/api.ts` request shapes or `web/src/lib/types.ts` contracts.
- Adding new quick view values or renaming query params.
- Moving field filters out of the drawer into always-visible controls.
- Disabling/removing the `缺订阅` tab when evidence fails.
- Replacing provider row snapshot display with provider master data.
- Inventing linked-node health, provider risk, billing risk, import validation, or real-inventory status not present in current contracts.
- Adding charts, dependencies, page-local CSS files, CSS Modules, Tailwind, or CSS-in-JS.
- Touching `VPSDetailPage`, Providers, Subscriptions, Asset Decisions, Dashboard, import flow, or docs/specs from this implementation task.

## Recommended Acceptance Criteria

1. **Default scan path is clearer**
   - First screen still follows `VPS` page identity → quick views/filter status → `VPS 库存表`.
   - The user can distinguish current quick view, active field filters, subscription evidence readiness, and table row count without changing interactions.

2. **Frozen filter contracts remain intact**
   - URLs using `view`, `provider_id`, `lifecycle_status`, `usage_status`, and `renewal_decision` still parse/serialize the same way.
   - Quick view tabs keep the same values and semantics.
   - Removing chips and `清空所有` update URL state without refetching VPS/subscription/provider lists.

3. **Advanced drawer semantics remain intact**
   - Drawer opens from applied filters.
   - `应用筛选` is the only action that writes draft filters to URL.
   - `关闭`, Escape, and overlay discard drafts.
   - `重置` resets draft only until applied.

4. **Evidence truthfulness remains intact**
   - Subscription fetch failure shows unknown/unavailable evidence state.
   - The page does not mark rows or counts as `缺订阅` while subscription evidence is loading/error.
   - `view=missing_subscription` filters rows only when subscription evidence is ready and missing.

5. **Provider/subscription joins remain truthful**
   - Provider master data is used for filter options/chip labels and create selector only.
   - Row provider display remains based on the VPS row snapshot.
   - Subscription row display remains based on `groupSubscriptionsByVPS` + `selectPrimarySubscription`.

6. **Table navigation and actions remain intact**
   - Clicking a row still navigates to `/vps/:vpsId`.
   - Any newly added row action/link remains protected by DataTable interactive-descendant guard.

7. **Create flow remains intact**
   - `VPS 创建表单` still opens in a Drawer, cancel/close resets draft/error, and successful create posts the same payload shape then navigates to `/vps/:vpsId`.

8. **Style and dependency boundaries remain intact**
   - CSS changes are limited to `web/src/styles/pages.css` and use existing tokens/BEM rhythm.
   - No new dependencies, CSS framework, page-local CSS file, backend code, API helper, or type changes.

## Verification Commands

Focused verification for this task:

```bash
cd /Users/weibo/Code/houfeng/web && npx vitest run src/pages/VPSPage.test.tsx
cd /Users/weibo/Code/houfeng/web && npm run lint
cd /Users/weibo/Code/houfeng/web && npm run build
```

Broader web verification before PR/commit:

```bash
cd /Users/weibo/Code/houfeng/web && npm run test -- --run
make -C /Users/weibo/Code/houfeng verify-web
```

Full repo verification if the final implementation wants the strongest gate, even though this task should be frontend-only:

```bash
TMPDIR="/Users/weibo/Code/houfeng/.tmp/verify-tmp" GOCACHE="/Users/weibo/Code/houfeng/.tmp/go-cache" /Users/weibo/Code/houfeng/scripts/verify.sh
```

## Browser Sanity Notes

Because this is a user-visible IA/layout change, run local browser sanity after implementation when possible.

Recommended preview:

```bash
cd /Users/weibo/Code/houfeng/web
npm run dev -- --host 127.0.0.1 --port 5178
```

Recommended protected-route mock check for `/vps`:

```bash
mkdir -p /Users/weibo/Code/houfeng/.tmp/playwright
TMPDIR="/Users/weibo/Code/houfeng/.tmp/playwright" python3 /Users/weibo/Code/houfeng/scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route /vps \
  --viewport 1440x1000 \
  --viewport 390x900
```

Use the Python interpreter with local Playwright installed if needed, as documented in `docs/operations/v2-visual-evidence.md`.

Browser sanity should specifically check:

- First viewport shows the VPS inventory workflow, not only navigation chrome.
- Quick views, evidence/status copy, active chips, and `高级筛选` remain reachable.
- `VPS 高级筛选` Drawer opens/closes and remains keyboard/mouse reachable.
- No text overlap/overflow in focus cards, filter bar, evidence notice, table cells, or buttons.
- Table-heavy narrow viewport keeps horizontal scroll behavior rather than squeezing columns.
- Data source is reported as `mock-api asset-workflows` unless a local center/real data run is used. Mock API proves representative frontend rendering, not backend correctness, import fidelity, or real inventory completeness (`docs/operations/v2-visual-evidence.md:84-118`, `docs/operations/v2-visual-evidence.md:214-237`).

## Related Specs

- `.trellis/spec/web/state-and-data.md` — Asset Ledger list/VPS inventory data flow, URL-state quick views, evidence failure matrix, and required tests (`.trellis/spec/web/state-and-data.md:147-202`).
- `.trellis/spec/web/component-conventions.md` — DataTable guard, Drawer discard/reset, and Drawer-first list workflow guidance (`.trellis/spec/web/component-conventions.md:45-57`).
- `.trellis/spec/web/styling-guidelines.md` — pure CSS/token/BEM/no page-local CSS/no new dependency styling contract (`.trellis/spec/web/styling-guidelines.md:9-17`, `.trellis/spec/web/styling-guidelines.md:95-112`, `.trellis/spec/web/styling-guidelines.md:138-151`).
- `.trellis/spec/web/quality-guidelines.md` — `make verify-web`, page-test patterns, and browser-sanity expectations (`.trellis/spec/web/quality-guidelines.md:9-50`, `.trellis/spec/web/quality-guidelines.md:89-126`, `.trellis/spec/web/quality-guidelines.md:164-165`).
- `docs/design/v2-houfeng/component-spec.md` — `VPSPage` visual/IA contract (`docs/design/v2-houfeng/component-spec.md:228-233`).
- `docs/design/v2-houfeng/design-language.md` — high-density rhythm and five-level hierarchy (`docs/design/v2-houfeng/design-language.md:147-170`).
- `docs/operations/v2-visual-evidence.md` — local preview/browser-sanity workflow, asset-workflows mock API, and browser-sanity checklist (`docs/operations/v2-visual-evidence.md:84-118`, `docs/operations/v2-visual-evidence.md:214-237`).

### External References

No external search was used. The task is repository-specific and satisfied by source, specs, active design docs, and archived Trellis research.

## Caveats / Not Found

- Static code/spec inspection only; this research did not run Vitest, lint, build, or browser sanity.
- No backend/import/model files were inspected because the requested scope is frontend-only and explicitly excludes backend/API/import changes.
- Archived `current-asset-pages-audit.md` contains older VPSPage observations saying the page did not fetch subscriptions; that is now obsolete. Current code does fetch subscriptions and performs evidence-aware joins.
- Visual hierarchy concerns should be validated in a browser after implementation; jsdom tests can protect behavior/copy but cannot prove density, overlap, or scan-path quality.
- There is no need for external UX/library references or new dependencies for this limited IA polish.

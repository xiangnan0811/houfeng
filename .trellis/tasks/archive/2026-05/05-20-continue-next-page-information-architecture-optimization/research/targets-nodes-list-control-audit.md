# Research: TargetsPage + NodesPage list control IA audit

- **Query**: Research TargetsPage + NodesPage list control information architecture for the active Trellis task. Inspect `TargetsPage`, `NodesPage`, page-local components, web specs, and v2 design docs. Report files/current IA, control-band differences, IA gaps fixable without frozen-contract changes, recommended joint-pass boundary, test impact, and caveats.
- **Scope**: internal
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/TargetsPage.tsx` | Target list route assembly: fetches target list/sparklines, derives URL filters, renders header, create drawer, support surface, filter panel, batch panel, table, and runtime overlays. |
| `web/src/pages/TargetsPage.test.tsx` | TargetsPage page tests for create flow, support surface quick filters, URL deep links, row guards, runtime confirmations, metadata editing, and sparklines. |
| `web/src/pages/targets/CreateTargetPanel.tsx` | Target create form rendered inside the page-level `Drawer`. |
| `web/src/pages/targets/TargetsSupportSurface.tsx` | Target support surface: evidence lead, scope count, four support lanes, top evidence focus. |
| `web/src/pages/targets/TargetsFilterPanel.tsx` | Target `FilterBar` controls/chips for URL-backed filters. |
| `web/src/pages/targets/TargetsBatchPanel.tsx` | Target batch bar and batch pause confirmation. |
| `web/src/pages/targets/TargetsRuntimeOverlays.tsx` | Target row-level runtime confirmation/error overlays rendered below the table. |
| `web/src/pages/targets/TargetsTableColumns.tsx` | Target DataTable column definitions and row action/metadata cell wiring. |
| `web/src/pages/targets/targetHelpers.ts` | Target filter parsing, counts, evidence lead ranking, create payload validation, runtime action list, and merge helpers. |
| `web/src/pages/targets/types.ts` | Target filter state, create form state, runtime action, confirmation, focus restore, and evidence lead types. |
| `web/src/pages/NodesPage.tsx` | Node list route assembly: fetches node list/sparklines, derives URL filters, renders hero, support surface, create drawer, toolbar, list section, table, batch/runtime controls. |
| `web/src/pages/NodesPage.test.tsx` | NodesPage page tests for create/onboarding, support surface, URL deep links, toolbar tabs, row guards, runtime confirmations, batch bar, sparklines, and metadata editing. |
| `web/src/pages/nodes/NodesHero.tsx` | Node page heading with inventory stat buttons and primary create action. |
| `web/src/pages/nodes/NodesSupportSurface.tsx` | Node support surface: evidence lead, scope count, four support lanes, top evidence focus. |
| `web/src/pages/nodes/NodesToolbar.tsx` | Node list toolbar: view tabs, result count, trends toggle, compare link/hint, auto-refresh select. |
| `web/src/pages/nodes/NodesListSection.tsx` | Node list frame body: empty state, filter panel, batch panel, table, runtime overlays. |
| `web/src/pages/nodes/NodesFilterPanel.tsx` | Node `FilterBar` controls/chips for URL-backed filters. |
| `web/src/pages/nodes/NodesBatchPanel.tsx` | Node batch bar, batch command panel, and batch pause confirmation. |
| `web/src/pages/nodes/CreateNodeDrawer.tsx` | Node create drawer with onboarding/security copy and fixed initial lifecycle context. |
| `web/src/pages/nodes/NodesRuntimeOverlays.tsx` | Node row-level runtime confirmation/error overlays rendered below the table. |
| `web/src/pages/nodes/NodesTableColumns.tsx` | Node DataTable column definitions, compare checkbox, row identity, sort columns, action/metadata cell wiring. |
| `web/src/pages/nodes/nodeHelpers.ts` | Node filter parsing, counts, onboarding/binding predicates, evidence lead ranking, runtime action list, merge helpers. |
| `web/src/pages/nodes/types.ts` | Node filter state, runtime action, list view, confirmation, focus restore, and evidence lead types. |
| `web/src/components/atoms/DataTable.tsx` | Shared DataTable with clickable-row interactive-target guard and optional sorting. |
| `web/src/components/filters/FilterBar.tsx` | Shared filter controls/chips wrapper and clear-all affordance. |
| `web/src/components/ObservabilityEvidenceLead.tsx` | Shared evidence lead skeleton used by Node/Target support surfaces. |
| `web/src/components/ObservabilityEvidenceFocus.tsx` | Shared top-evidence focus skeleton used by Node/Target support surfaces. |
| `web/src/components/ActionConfirmationCard.tsx` | Shared confirmation card used by row and batch runtime confirmations. |
| `web/src/styles/pages.css` | Current page/list-frame/support/toolbar/batch/table styles for NodesPage and TargetsPage. |
| `.trellis/spec/web/component-conventions.md` | Component contracts: page composition, Drawer reset, DataTable row guard, route-agnostic shared components. |
| `.trellis/spec/web/styling-guidelines.md` | Styling contracts: tokens, BEM, dark-first, no new page-local CSS. |
| `.trellis/spec/web/state-and-data.md` | Data/API contracts, especially API client usage and Node onboarding install-command security. |
| `.trellis/spec/web/quality-guidelines.md` | Web test/lint/build command and page-test expectations. |
| `docs/design/v2-houfeng/design-language.md` | v2 visual language: density, dark-first, DataTable/empty state expectations. |
| `docs/design/v2-houfeng/component-spec.md` | v2 page/component visual contracts for NodesPage and TargetsPage. |

## 1. Files found and current IA composition for each page

### TargetsPage current composition

`TargetsPage` is a route-level assembly component with data fetch, URL filter state, create state, metadata inline edit state, runtime state, batch state, and render composition in one file (`web/src/pages/TargetsPage.tsx:54-715`). The current visible IA order is:

1. **Page identity / primary create** — `<header className="page-panel page-panel--inline">` with eyebrow `观测 · 目标`, title `入口观测`, explanatory copy, and primary `新建目标` button (`TargetsPage.tsx:570-588`).
2. **Create drawer** — `Drawer` with `title="创建目标"`, `ariaLabel="创建目标"`, and `CreateTargetPanel` (`TargetsPage.tsx:590-604`). The drawer portals, so this JSX position is not a visible in-flow panel.
3. **Support surface** — `TargetsSupportSurface` receives inventory/display counts, evidence lead, top evidence, filter context, and quick actions (`TargetsPage.tsx:606-627`). Internally it renders:
   - Header/scope count (`TargetsSupportSurface.tsx:69-86`).
   - `ObservabilityEvidenceLead` with action and secondary VPS link (`TargetsSupportSurface.tsx:88-112`).
   - Four lanes: `异常入口`, `暂停 / 归档`, `执行覆盖`, `资产服务上下文` (`TargetsSupportSurface.tsx:114-203`).
   - Top evidence focus or stable focus (`TargetsSupportSurface.tsx:205-245`).
4. **First-run empty state OR list frame** — If `targets.length === 0`, the page renders a `PageState` directly after support surface (`TargetsPage.tsx:629-640`). Otherwise it renders `.observability-list-frame--targets` (`TargetsPage.tsx:642-711`).
5. **Target list frame controls and content** — Inside the list frame:
   - `TargetsFilterPanel` first (`TargetsPage.tsx:643-653`).
   - `TargetsBatchPanel` second (`TargetsPage.tsx:654-665`).
   - Filter-empty `PageState` or `DataTable` (`TargetsPage.tsx:666-691`).
   - `TargetsRuntimeOverlays` after the table (`TargetsPage.tsx:694-710`).

Current target filter state is entirely URL-derived from `group`, `type`, `run_status`, `health`, `labels`, `execution_labels`, and `abnormal=1` (`TargetsPage.tsx:312-323`). Filtering is client-side against loaded targets (`TargetsPage.tsx:351-370`). `clearAllFilters()` replaces the search params with an empty `URLSearchParams` (`TargetsPage.tsx:526-528`).

Current target table columns are `glyph`, `identity`, `type`, `host`, `labels`, `status`, `trends`, `issue`, `actions` (`TargetsTableColumns.tsx:50-169`). The implementation already places recent success/failure freshness inside the identity column (`TargetsTableColumns.tsx:65-79`) and includes a `近 24h` sparkline column (`TargetsTableColumns.tsx:132-137`).

### NodesPage current composition

`NodesPage` is also a route-level assembly component with data fetch, URL filter state, create state, metadata inline edit state, runtime state, batch/command state, toolbar state, sort state, compare state, and auto-refresh state in one file (`web/src/pages/NodesPage.tsx:58-774`). The current visible IA order is:

1. **Node hero / primary create / inventory stat shortcuts** — `NodesHero` renders section heading `节点观测`, copy, four stat controls (`全部`, `异常`, `待接入`, `维护/暂停`), and `新建节点` (`NodesPage.tsx:661-672`; `NodesHero.tsx:27-85`).
2. **Support surface** — `NodesSupportSurface` receives inventory/display counts, evidence lead, top evidence, filter context, and quick actions (`NodesPage.tsx:674-691`). Internally it renders:
   - Header/scope count (`NodesSupportSurface.tsx:57-74`).
   - `ObservabilityEvidenceLead` with action and secondary asset-decision link (`NodesSupportSurface.tsx:76-100`).
   - Four lanes: `异常证据`, `接入 / 绑定`, `维护 / 暂停`, `VPS 关联` (`NodesSupportSurface.tsx:102-190`).
   - Top evidence focus or stable focus (`NodesSupportSurface.tsx:192-232`).
3. **Create drawer** — `CreateNodeDrawer` renders a `Drawer` with `title="节点创建"` and `ariaLabel="创建节点表单"` (`NodesPage.tsx:693-706`; `CreateNodeDrawer.tsx:29-128`). The drawer copy explicitly says creation jumps to the onboarding workspace, where one-command install generation happens (`CreateNodeDrawer.tsx:33-35`).
4. **Node list frame** — `.observability-list-frame--nodes` always wraps the toolbar/list section (`NodesPage.tsx:708-771`).
5. **Node toolbar** — `NodesToolbar` is first inside the frame and contains tabs, displayed/base count, trends toggle, compare link/hint, and auto-refresh select (`NodesPage.tsx:709-720`; `NodesToolbar.tsx:33-82`).
6. **Node list section controls and content** — `NodesListSection` receives all list state and callbacks (`NodesPage.tsx:722-770`). Internally:
   - If `baseNodes.length === 0`, it returns a first-run/binding-conflict empty `PageState` immediately (`NodesListSection.tsx:100-120`). This means the toolbar is still visible before this empty state.
   - Otherwise it renders `NodesFilterPanel` (`NodesListSection.tsx:127-141`), `NodesBatchPanel` (`NodesListSection.tsx:143-159`), filter-empty state or `DataTable` (`NodesListSection.tsx:161-185`), then `NodesRuntimeOverlays` (`NodesListSection.tsx:188-195`).

Current node filter state is entirely URL-derived from `group`, `region`, `city`, `provider`, `lifecycle`, `run_status`, `health`, `labels`, `abnormal=1`, and `onboarding=pending` (`NodesPage.tsx:300-314`). It is applied after the local `nodeListView` tab scope (`NodesPage.tsx:298-378`). Sorting is local state and only covers `identity`, `issue`, and `location` (`NodesPage.tsx:380-407`).

Current node table columns are `compare`, `glyph`, `identity`, `location`, `labels`, `issue`, `trends`, `actions` (`NodesTableColumns.tsx:62-192`). The identity column includes node ID, display-name link, and `心跳` / `同步` freshness line (`NodesTableColumns.tsx:97-118`).

## 2. Current control-band/support/filter/batch/create/onboarding/runtime surfaces and how they differ

### Page identity and support surfaces

- **TargetsPage** has a simpler page identity header: title/copy + `新建目标` only (`TargetsPage.tsx:570-588`). It does not have a separate stat shortcut band. Its support surface is therefore the first place where counts and next action appear (`TargetsPage.tsx:606-627`).
- **NodesPage** has a stronger hero with four stat shortcuts plus create (`NodesHero.tsx:35-83`) before a second support surface that also contains counts/quick filters (`NodesSupportSurface.tsx:102-190`). The result is two prominent node-level decision/control bands before the actual list controls.
- Both support surfaces use the same shared evidence components (`ObservabilityEvidenceLead.tsx:37-52`, `ObservabilityEvidenceFocus.tsx:30-39`) and the same pattern of derived counts + top evidence. Domain wording differs appropriately: Targets emphasizes service entry observability; Nodes emphasizes VPS asset judgment support.

### Filter contracts

- **TargetsPage URL filter contract**: `group`, `type`, `run_status`, `health`, `labels`, `execution_labels`, `abnormal=1` (`TargetsPage.tsx:312-323`, `TargetsFilterPanel.tsx:47-97`, `TargetsFilterPanel.tsx:101-141`). Dashboard deep links currently tested include `/targets?abnormal=1`, `/targets?run_status=暂停`, and `/targets?run_status=已归档` (`TargetsPage.test.tsx:1115-1216`).
- **NodesPage URL filter contract**: `group`, `region`, `city`, `provider`, `lifecycle`, `run_status`, `health`, `labels`, `abnormal=1`, `onboarding=pending` (`NodesPage.tsx:300-314`, `NodesFilterPanel.tsx:53-115`, `NodesFilterPanel.tsx:119-176`). Dashboard/onboarding deep-link behavior is tested for `/nodes?onboarding=pending` (`NodesPage.test.tsx:959-1032`) and abnormal filter (`NodesPage.test.tsx:1034-1081`).
- Both pages use the same `FilterBar` contract: controls row, optional `清空所有`, and active chips (`FilterBar.tsx:23-40`).
- Both pages clear filters by replacing search params with an empty `URLSearchParams` (`TargetsPage.tsx:526-528`, `NodesPage.tsx:571-573`).

### List toolbar/control band

- **TargetsPage** has no explicit toolbar between support surface and filter bar. The list frame begins directly with filters (`TargetsPage.tsx:642-653`). Result count is visible only in support scope and evidence lead, not near the list table.
- **NodesPage** has a separate toolbar inside the list frame before filters (`NodesPage.tsx:708-720`). It controls:
  - local view tabs `全部节点` / `绑定异常` (`NodesPage.tsx:575-582`; `NodesToolbar.tsx:35-40`), not URL state;
  - displayed/base result count (`NodesToolbar.tsx:41-43`);
  - trends column visibility (`NodesToolbar.tsx:46-52`);
  - compare flow (`NodesToolbar.tsx:53-62`);
  - auto-refresh (`NodesToolbar.tsx:63-80`).
- Nodes has local sort and table-level sort controls (`NodesPage.tsx:380-407`, `NodesListSection.tsx:174-184`); Targets does not expose local sort.

### Batch/selection surfaces

- **TargetsPage batch eligibility** is `groupFilterActive && filteredTargets.length > 0` (`TargetsPage.tsx:381`, `TargetsPage.tsx:654-655`). This means target batch controls are scoped to an active `group` filter only. The batch target IDs are all current `filteredTargets` (`TargetsPage.tsx:440`, `TargetsPage.tsx:477`).
- **NodesPage batch eligibility** is `hasActiveFilters && filteredNodeCount > 0` (`NodesBatchPanel.tsx:40-91` via `NodesListSection.tsx:143-159`). This means any active node filter can expose the batch bar. Node batch IDs are all current `filteredNodes` for runtime batch (`NodesPage.tsx:465-467`, `NodesPage.tsx:484-487`) and command dispatch (`NodesPage.tsx:504-512`).
- Both batch panels use a `selectAll` boolean as a gate for showing actions, not per-row selection. The visible copy is `全选 (N)` (`TargetsBatchPanel.tsx:31-39`, `NodesBatchPanel.tsx:41-49`).
- **Target batch actions**: enter maintenance, exit maintenance, pause, resume (`TargetsBatchPanel.tsx:41-70`). Pause has confirmation (`TargetsBatchPanel.tsx:76-87`). `TargetsPage` currently has code paths for `archive` in `executeBatchTargetAction`, but the panel does not render an archive button (`TargetsPage.tsx:433-471`; `TargetsBatchPanel.tsx:41-70`). Target batch mutation loops individual runtime endpoints and refreshes `listTargets()` afterward (`TargetsPage.tsx:442-470`, `TargetsPage.tsx:479-493`).
- **Node batch actions**: enter maintenance, exit maintenance, pause, resume, execute command (`NodesBatchPanel.tsx:51-87`). Pause has confirmation (`NodesBatchPanel.tsx:132-143`). Runtime batch uses `postNodeBatch(nodeIDs, action)` (`NodesPage.tsx:458-478`, `NodesPage.tsx:480-497`). Command dispatch loops `postNodeAction(nodeID, commandID.trim())` (`NodesPage.tsx:499-519`).

### Create / onboarding surfaces

- **Target create** opens a target create drawer from the header, support lead, or empty state. Submit creates a target via `createTarget(payload)`, inserts it locally, and navigates to `/targets/{target_id}` (`TargetsPage.tsx:127-174`). The form requires execution node labels and validates base port as a positive integer before API submission (`targetHelpers.ts:309-327`). Drawer close/cancel resets draft/error/submitting state (`TargetsPage.tsx:120-134`).
- **Node create** opens a node create drawer from hero, support lead, or empty state. Submit calls `createNode(payload)`, inserts the node locally, closes/resets the drawer, and navigates to `/nodes/{node_id}/onboarding` (`NodesPage.tsx:259-289`). The drawer does not expose lifecycle as an editable field; it displays `生命周期状态` fixed to `待接入` and explains that short-lived one-time enrollment tokens are issued later in the onboarding workspace (`CreateNodeDrawer.tsx:87-93`).
- Node create tests explicitly assert that creation does **not** pre-issue an enrollment token and that the only fetches are list + create (`NodesPage.test.tsx:71-159`). This is a security contract boundary for the joint pass.

### Runtime row surfaces

- Both pages have row actions rendered in DataTable action columns with hover/focus reveal CSS (`pages.css:5536-5557` for Nodes, `pages.css:5878-5900` for Targets).
- **Target row runtime actions** include maintenance, pause/resume, archive, restore-to-paused (`targetHelpers.ts:329-360`). Pause and archive require page-level confirmation before mutation (`TargetsPage.tsx:197-245`; `TargetsRuntimeOverlays.tsx:33-65`).
- **Node row runtime actions** include maintenance, pause/resume only (`nodeHelpers.ts:103-125`). Pause requires confirmation (`NodesPage.tsx:166-210`; `NodesRuntimeOverlays.tsx:31-45`).
- Runtime overlays are rendered after the table by mapping filtered rows; tests already document this as a row-overlay sibling below the DataTable rather than inside `<tr>` (`TargetsPage.test.tsx:606-610`).
- Row navigation has two layers of protection:
  - `DataTable` itself ignores clicks/Enter/Space from interactive descendants (`DataTable.tsx:3-15`, `DataTable.tsx:139-158`).
  - Each page blocks row navigation while a row is in inline edit or pending confirmation (`TargetsPage.tsx:563-567`, `NodesPage.tsx:645-650`).

## 3. Concrete IA gaps fixable without changing frozen contracts

These gaps are constrained to presentation/composition/copy/layout. They do not require backend/API changes, URL filter changes, DataTable row guard changes, onboarding command/token changes, or runtime/batch payload changes.

1. **Node top-level controls are split across two strong bands; target top-level controls are not.**  
   Nodes has `NodesHero` stats plus `NodesSupportSurface` lanes before list controls (`NodesPage.tsx:661-691`). Targets has only a page header, then support surface (`TargetsPage.tsx:570-627`). A joint IA pass can standardize the hierarchy as: page identity/primary create → support/evidence surface → list control band → filters/batch/table. This can keep node-only stats, but should make their relationship to support lanes explicit to avoid two competing “next action” areas.

2. **List-scope controls are not mapped consistently.**  
   Nodes has a toolbar for list view/count/display/auto-refresh (`NodesToolbar.tsx:33-82`) before the filter bar; Targets starts directly with filters (`TargetsPage.tsx:642-653`). If implementation wants a shared list-control rhythm, it can introduce a comparable list-scope header for Targets (even if it only contains result count and possibly “全部目标”) or visually fold NodesToolbar into the same list-frame command band style. This does not require changing filters or rows.

3. **Batch scope is implicit and differs by page.**  
   Target batch appears only for active `group` filters (`TargetsPage.tsx:654-655`); node batch appears for any active filters (`NodesBatchPanel.tsx:40-91`). Both mutate all currently filtered rows, gated by a single `selectAll` boolean. The safe IA fix is to label the batch scope explicitly as “current filtered set” / “当前筛选范围” and keep existing eligibility conditions and payload sources unchanged. Avoid broadening target batch eligibility or changing node batch eligibility in this pass unless explicitly accepted as a semantics change.

4. **First-run empty-state placement differs.**  
   Targets renders the no-target empty state outside the list frame after the support surface (`TargetsPage.tsx:629-640`). Nodes always renders the list frame and toolbar first, then `NodesListSection` returns the no-node empty state inside the frame (`NodesPage.tsx:708-771`; `NodesListSection.tsx:100-120`). A joint pass can standardize whether first-run empty belongs inside the list frame, while preserving the same CTA text/action and create/onboarding semantics.

5. **Create drawer triggers are consistent in behavior but not in IA naming/placement.**  
   Target create is triggered from a generic page-panel header (`TargetsPage.tsx:579-586`), support lead/action (`TargetsSupportSurface.tsx:52-65`), and empty state (`TargetsPage.tsx:635-638`). Node create is triggered from `NodesHero` (`NodesHero.tsx:81-83`), support lead (`NodesSupportSurface.tsx:42-53`), and empty state (`NodesListSection.tsx:112-116`). A joint pass can align trigger copy and location while preserving target submit → detail and node submit → onboarding.

6. **Filter and support surfaces duplicate “active filter” context in different places.**  
   Support leads show current filter chips/context via `filterContext` (`TargetsSupportSurface.tsx:88-112`, `NodesSupportSurface.tsx:76-100`), while `FilterBar` also shows active chips (`TargetsFilterPanel.tsx:45-99`, `NodesFilterPanel.tsx:51-117`). This is a valid pattern, but the current IA does not distinguish “judgment context” from “filter editing controls.” Copy/layout can make support context read-only and FilterBar the editing surface without changing URL state.

7. **Targets table implementation already diverges from the older component-spec column description.**  
   The v2 component spec says TargetsPage DataTable includes separate `最近成功/失败` (`docs/design/v2-houfeng/component-spec.md:305-307`), but current code has freshness inside the identity column and a `近 24h` trends column (`TargetsTableColumns.tsx:65-79`, `TargetsTableColumns.tsx:132-137`). This is not necessarily a bug, but it means the joint pass should avoid relying on the older table-column text as an implementation target unless the spec is intentionally updated later.

8. **Node-only controls need a clear “domain-specific” boundary.**  
   Binding-conflict tabs, compare, show/hide trends, auto-refresh, sorting, and command batch are node-specific (`NodesToolbar.tsx:35-80`, `NodesBatchPanel.tsx:80-129`, `NodesTableColumns.tsx:63-83`). A joint pass should align container hierarchy and language, not force TargetsPage to acquire node-only controls.

## 4. Recommended implementation boundary for a joint pass, with must-preserve behaviors

### Recommended joint-pass boundary

Implement a **presentation-level list control IA pass** only:

1. **Define a shared control hierarchy for both pages**:  
   `page identity + primary create` → `observability support/evidence` → `list frame command band` → `filter controls` → `batch scope controls` → `table/runtime overlays`.

2. **Keep domain-specific capabilities inside that hierarchy**:
   - Nodes may keep view tabs, compare, trend visibility, auto-refresh, sort, and command dispatch.
   - Targets may keep simpler list controls and no node-only controls.
   - Both should visually communicate displayed count / current scope near the table, not only in the support surface.

3. **Treat support surfaces as “current judgment / next action,” not as filter editors.**  
   Support quick actions may still apply URL filters or navigate, but the filter bar remains the explicit edit surface. Keep `ObservabilityEvidenceLead` / `ObservabilityEvidenceFocus` route-agnostic shared components; pass Link/Button/glyph/meta from page-specific support surfaces as today.

4. **Make batch scope explicit without changing batch behavior.**  
   Preserve existing batch eligibility and payload semantics:
   - Targets: show batch only when `group` filter is active and rows exist (`TargetsPage.tsx:654-655`); apply to `filteredTargets`.
   - Nodes: show batch when any active filter exists and rows exist (`NodesBatchPanel.tsx:40-91`); apply to `filteredNodes`.
   - Keep single `selectAll` boolean behavior unless a separate task introduces real row selection.

5. **Standardize empty-state placement/copy only if tests are updated accordingly.**  
   Preserve CTA actions: target empty CTA opens target create drawer; node empty CTA opens node create drawer and later submit navigates onboarding.

### Must-preserve behaviors / frozen contracts

- **No backend/API changes**: continue using `listTargets`, `listTargetSparklines`, target runtime APIs, `createTarget`, `updateTargetMetadata`, `listNodes`, `listNodeSparklines`, `createNode`, node runtime APIs, `postNodeBatch`, and `postNodeAction` through `web/src/lib/api.ts` (`TargetsPage.tsx:7-18`, `NodesPage.tsx:6-18`).
- **Preserve URL filter contracts**:
  - Targets: `group`, `type`, `run_status`, `health`, `labels`, `execution_labels`, `abnormal=1`.
  - Nodes: `group`, `region`, `city`, `provider`, `lifecycle`, `run_status`, `health`, `labels`, `abnormal=1`, `onboarding=pending`.
  - Preserve chip removal and `清空所有` URL clearing.
- **Preserve DataTable row navigation guards**: do not replace `DataTable` guard behavior (`DataTable.tsx:3-15`, `DataTable.tsx:139-158`) or page-level edit/confirmation navigation blocks (`TargetsPage.tsx:563-567`, `NodesPage.tsx:645-650`).
- **Preserve Node onboarding security**: Node creation must navigate to `/nodes/{node_id}/onboarding` without issuing/caching plaintext tokens (`NodesPage.tsx:275-284`; `.trellis/spec/web/state-and-data.md:38-80`). Do not synthesize install commands or touch `NodeOnboardingPage` command reveal/copy contracts in this task.
- **Preserve runtime mutation semantics**:
  - Target row pause/archive confirmations and focus restoration (`TargetsPage.tsx:197-245`, `TargetsRuntimeOverlays.tsx:33-65`).
  - Node row pause confirmation and runtime focus restoration (`NodesPage.tsx:166-210`, `NodesRuntimeOverlays.tsx:31-45`).
  - Target batch loop + refresh semantics (`TargetsPage.tsx:433-497`).
  - Node `postNodeBatch` and command dispatch semantics (`NodesPage.tsx:458-519`).
- **Preserve Drawer state reset** for create forms (`TargetsPage.tsx:120-134`, `CreateNodeDrawer.tsx:117-123`) and keep close/cancel/Escape/overlay behavior aligned with the Drawer focus/reset spec (`.trellis/spec/web/component-conventions.md:48-57`).
- **Preserve no-new-dependency/no-new-CSS-system constraints**: use existing atoms/shared components, BEM classes in `pages.css`, and design tokens (`.trellis/spec/web/styling-guidelines.md:93-112`, `docs/design/v2-houfeng/design-language.md:312-325`).

## 5. Test impact and exact tests likely to update

### `web/src/pages/TargetsPage.test.tsx`

Likely updates if headings, copy, empty placement, support/list-control composition, or batch copy changes:

- **Create/heading/support assertions**:
  - `creates the first target and navigates to its detail page` asserts page eyebrow and description (`TargetsPage.test.tsx:102-110`) and empty CTA (`TargetsPage.test.tsx:102-103`).
  - `keeps failed target creation API errors local while preserving the loaded list` asserts heading `入口观测`, support heading `服务入口支撑`, and support description (`TargetsPage.test.tsx:249-265`).
  - `toggles the create target drawer via the section heading button and restores focus` asserts `新建目标` button, dialog open/close, Escape focus restore (`TargetsPage.test.tsx:1540-1574`).
- **Filter/deep-link assertions**:
  - Target type filter and chip (`TargetsPage.test.tsx:1082-1113`).
  - `abnormal=1` deep link support lead/filter context (`TargetsPage.test.tsx:1115-1154`).
  - `run_status=暂停` and `run_status=已归档` deep links (`TargetsPage.test.tsx:1156-1216`).
  - Toggle `仅看异常` and clear all (`TargetsPage.test.tsx:1218-1261`).
- **Support surface assertions**:
  - `surfaces entry support lanes and applies support quick filters` checks support heading, evidence lead, lanes, links, quick filters, and chip (`TargetsPage.test.tsx:1263-1337`).
  - Empty filter lead clear (`TargetsPage.test.tsx:1339-1371`).
  - Coverage gap support and `/nodes` navigation (`TargetsPage.test.tsx:1373-1407`).
  - Stable lead (`TargetsPage.test.tsx:1409-1442`).
- **Must remain green / should not need behavior changes**:
  - Row navigation guard tests (`TargetsPage.test.tsx:1482-1538`).
  - Runtime confirmation/action tests (`TargetsPage.test.tsx:404-732`).
  - Metadata/race tests (`TargetsPage.test.tsx:735-1080`).
  - Sparkline tests (`TargetsPage.test.tsx:1578-1648`).

No current TargetsPage test directly covers the batch bar. If the joint pass changes target batch copy/placement, add focused coverage for `/targets?group=<value>` showing the batch scope bar, select-all revealing the same target actions, and preserving the existing group-only eligibility.

### `web/src/pages/NodesPage.test.tsx`

Likely updates if hero/support/list-toolbar copy, empty placement, or batch copy changes:

- **Create/hero/support assertions**:
  - `creates a node and navigates to onboarding without pre-issuing a token` asserts heading `节点观测`, eyebrow, hero description, support description, support heading, and tab presence (`NodesPage.test.tsx:105-119`), then verifies create payload and no token issuance (`NodesPage.test.tsx:133-159`).
  - `keeps create errors local to the page` asserts heading remains and no onboarding route (`NodesPage.test.tsx:161-196`).
  - `surfaces the shared API fallback message...` may be unaffected unless create drawer copy/structure changes (`NodesPage.test.tsx:198-231`).
  - `toggles the create node form panel via the section heading button` asserts `新建节点` toggles drawer content (`NodesPage.test.tsx:1353-1378`).
- **Onboarding/binding scope assertions**:
  - Existing pending onboarding workspace link (`NodesPage.test.tsx:233-275`).
  - Binding-conflict tabs and empty binding-conflict state (`NodesPage.test.tsx:277-360`).
  - Segmented control tabs (`NodesPage.test.tsx:1307-1351`).
- **Filter/deep-link/support assertions**:
  - Lifecycle filter and chip (`NodesPage.test.tsx:918-957`).
  - `/nodes?onboarding=pending` deep link and clear (`NodesPage.test.tsx:959-1032`).
  - Toggle `仅看异常` and clear all (`NodesPage.test.tsx:1034-1081`).
  - Support lanes and quick filters (`NodesPage.test.tsx:1083-1164`).
  - Empty-filter lead and clear (`NodesPage.test.tsx:1166-1206`).
  - Stable evidence lead (`NodesPage.test.tsx:1208-1238`).
- **Toolbar/display assertions**:
  - Toolbar tabs (`NodesPage.test.tsx:1307-1351`).
  - Sparklines/trends column tests (`NodesPage.test.tsx:1380-1473`) should stay green if trends toggle defaults remain `showTrends=true`.
  - If toolbar is visually reorganized, add/update assertions for `aria-label="节点列表工具栏"`, displayed/base count, `隐藏趋势` / `显示趋势`, compare hint/link, and `自动刷新间隔` select.
- **Batch assertions**:
  - `shows the batch bar with select-all toggle when a group filter is active` checks `.batch-bar`, `全选 (2)`, and five action buttons (`NodesPage.test.tsx:1475-1528`).
  - `does not show batch bar when group filter is not active` currently verifies no batch bar when no filters are active (`NodesPage.test.tsx:1530-1556`). Note the implementation condition is any active filter, not specifically group-only (`NodesBatchPanel.tsx:40-91`). If copy or placement changes, update the test name/expectations to match implementation without changing mutation semantics.
- **Must remain green / should not need behavior changes**:
  - Runtime confirmation/action tests (`NodesPage.test.tsx:362-634`).
  - Metadata/race tests (`NodesPage.test.tsx:637-916`).
  - Row navigation guard tests (`NodesPage.test.tsx:1240-1305`).
  - Heartbeat/sync freshness identity test (`NodesPage.test.tsx:1558-1589`).

### Specs/tests to keep in mind during implementation

- Component conventions require route pages to assemble data/sections, shared components to stay controlled/pure, and DataTable row clicks to be guarded from interactive descendants (`.trellis/spec/web/component-conventions.md:21-49`).
- Component conventions prefer Drawer for list primary scan paths and require Drawer close/cancel reset (`.trellis/spec/web/component-conventions.md:48-57`).
- `make verify-web` runs lint, Vitest, and build (`.trellis/spec/web/quality-guidelines.md:35-50`). For this pass, page-level Vitest updates are the primary safety net.

## Related Specs

- `.trellis/spec/web/component-conventions.md` — Route page/component boundaries, `PageState`, DataTable row guard, Drawer focus/reset/list-scan guidance (`component-conventions.md:21-57`).
- `.trellis/spec/web/styling-guidelines.md` — BEM/token/global CSS placement, no page-local CSS, no CSS framework (`styling-guidelines.md:93-112`, `styling-guidelines.md:138-151`).
- `.trellis/spec/web/state-and-data.md` — API client usage, URL/deep-link contracts, Node onboarding install-command security (`state-and-data.md:20-37`, `state-and-data.md:38-80`, `state-and-data.md:102-145`).
- `.trellis/spec/web/quality-guidelines.md` — Page tests, `make verify-web`, and page-test patterns (`quality-guidelines.md:23-50`, `quality-guidelines.md:64-127`).
- `docs/design/v2-houfeng/design-language.md` — Dark-first, density, DataTable, empty state, no new dependencies (`design-language.md:147-170`, `design-language.md:232-262`, `design-language.md:263-325`).
- `docs/design/v2-houfeng/component-spec.md` — NodesPage and TargetsPage intended page contracts (`component-spec.md:243-256`, `component-spec.md:301-309`).

## External References

None. This was an internal code/spec audit only.

## Caveats / Not Found

- No external documentation was searched; the task is entirely local IA/code/spec research.
- The requested file `web/src/pages/TargetsPage.test.tsx` exists and was inspected. The requested research output file did not exist before this audit.
- I did not find page-local tests for `TargetsBatchPanel` behavior. Targets batch UI is currently covered indirectly, if at all; add coverage if the joint pass changes its presentation.
- `docs/design/v2-houfeng/component-spec.md` still says TargetsPage may use an optional create page-panel and lists a separate recent success/failure column (`component-spec.md:301-307`), while current code uses a Drawer and identity-column freshness. Treat current code/tests plus component conventions as the implementation source unless the spec is intentionally updated in a separate step.
- Node batch test names mention group filtering, but implementation exposes batch for any active filter (`NodesBatchPanel.tsx:40-91`). This audit treats the implementation as the factual current behavior and recommends preserving it unless explicitly changed.
- The audit did not inspect backend handlers because the task explicitly freezes backend/API changes and the relevant frontend API usage is visible through `lib/api` calls in the pages.

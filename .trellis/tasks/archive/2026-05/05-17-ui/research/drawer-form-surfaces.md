# Research: Houfeng frontend Drawer and form UI surfaces

- **Query**: Research the Houfeng frontend Drawer and form UI surfaces for task `.trellis/tasks/05-17-ui`. Inspect repo files only. Identify every Drawer usage, its child component/content, likely form/table/modal interactions, and any layout/theme risks (hardcoded dark colors, naked forms, missing structured classes).
- **Scope**: internal
- **Date**: 2026-05-17

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/components/atoms/Drawer.tsx` | Drawer atom implementation; portal-rendered dialog with overlay click close, Escape/focus behavior via `useModalFocus`, title/header/body structure. |
| `web/src/lib/useModalFocus.ts` | Shared modal/drawer focus hook; stores opener, focuses first focusable element/container, traps Tab/Shift+Tab, closes on Escape, restores focus on cleanup. |
| `web/src/styles/atoms.css` | Drawer atom styles and direction modifiers; also Input/Button atom styles used by some Drawer forms. |
| `web/src/styles/pages.css` | Page-level Drawer child styles for event filters, VPS detail forms, asset decisions, VPS/Target create drawers, command picker, and generic page-stack/page-panel form rules. |
| `web/src/pages/nodes/CreateNodeDrawer.tsx` | Node create Drawer; direct child content is a paragraph plus a raw HTML form for node fields. |
| `web/src/pages/NodesPage.tsx` | Opens/closes `CreateNodeDrawer`, resets create state on close, submits `createNode`, navigates to onboarding after create. |
| `web/src/pages/targets/CreateTargetPanel.tsx` | Target create form component rendered inside the TargetsPage Drawer. |
| `web/src/pages/TargetsPage.tsx` | Target create Drawer; direct child is `CreateTargetPanel`. |
| `web/src/pages/VPSPage.tsx` | Two Drawers: VPS create form Drawer and VPS advanced filter Drawer. |
| `web/src/pages/events/EventsFilterDrawer.tsx` | Event advanced filter Drawer; direct child is a structured form with tabs, filter controls, raw time/label inputs, and apply/reset/close actions. |
| `web/src/pages/EventsPage.tsx` | Owns applied/draft event filter state and opens/closes/applies/resets `EventsFilterDrawer`. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset decision Drawer; direct child is `AssetDecisionWorkPanel` in drawer mode. |
| `web/src/components/AssetDecisionWorkPanel.tsx` | Drawer child for renewal decisions; selected VPS form with renewal decision select, reason textarea, error, cancel/save actions. |
| `web/src/pages/VPSDetailPage.tsx` | One multiplexed VPS detail Drawer; direct child wrapper renders one of six VPS operation forms by `activeDrawer`. |
| `web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx` | VPS detail Drawer child for renewal decisions. |
| `web/src/pages/vps-detail/VPSFactsEditForm.tsx` | VPS detail Drawer child for basic facts editing. |
| `web/src/pages/vps-detail/VPSNodeLinkForm.tsx` | VPS detail Drawer child for linking a Node to a VPS. |
| `web/src/pages/vps-detail/VPSExperienceLogForm.tsx` | VPS detail Drawer child for adding experience log entries. |
| `web/src/pages/vps-detail/VPSServicesForm.tsx` | VPS detail Drawer child for creating service records. |
| `web/src/pages/vps-detail/VPSDomainsForm.tsx` | VPS detail Drawer child for creating domain records. |
| `web/src/pages/node-detail/NodeHistoryDrawer.tsx` | Node history Drawer; direct child is tabs plus event/incident list, empty, loading, or retry content. |
| `web/src/pages/node-detail/NodeCommandDrawer.tsx` | Node command Drawer; direct child is command picker buttons, optional error, and optional command result. |
| `web/src/pages/node-detail/NodeDetailPageBody.tsx` | Composes `NodeHistoryDrawer` and `NodeCommandDrawer` into the node detail page. |
| `web/src/pages/NodeDetailPage.tsx` | Owns node history/command Drawer open state, submit handlers, close/error reset behavior. |
| `web/src/pages/target-detail/TargetHistoryDrawer.tsx` | Target history Drawer; direct child is tabs plus event/incident list, empty, loading, or retry content. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Composes `TargetHistoryDrawer` into the target detail page. |
| `web/src/pages/TargetDetailPage.tsx` | Owns target history Drawer open state and history tab handling. |
| `web/src/components/atoms/Drawer.test.tsx` | Drawer atom behavior tests. |
| `web/src/pages/NodesPage.test.tsx` | Node create Drawer page tests. |
| `web/src/pages/TargetsPage.test.tsx` | Target create Drawer page tests. |
| `web/src/pages/VPSPage.test.tsx` | VPS create/filter Drawer tests. |
| `web/src/pages/EventsPage.test.tsx` | Events filter Drawer tests, including close/Escape discard behavior. |
| `web/src/pages/AssetDecisionsPage.test.tsx` | Asset decision Drawer tests. |
| `web/src/pages/VPSDetailPage.test.tsx` | VPS detail Drawer tests across decision/facts/link/experience/service/domain flows. |
| `web/src/pages/NodeDetailPage.test.tsx` | Node detail history/command Drawer tests. |

### Drawer Atom Pattern

- `Drawer` props are `open`, `onClose`, `title`, optional `side`, `children`, and optional `ariaLabel` in `web/src/components/atoms/Drawer.tsx:6-13`.
- When closed, the Drawer returns `null`, so closed Drawer children are unmounted (`web/src/components/atoms/Drawer.tsx:30-33`).
- Open Drawers render through `createPortal(..., document.body)` (`web/src/components/atoms/Drawer.tsx:35-64`).
- The overlay is a sibling div with `onMouseDown={onClose}` (`web/src/components/atoms/Drawer.tsx:37-40`). The dialog surface is an `<aside role="dialog" aria-modal="true">` with title header, close button, and `.drawer__body` wrapper (`web/src/components/atoms/Drawer.tsx:41-60`).
- Focus behavior is centralized in `useModalFocus`: it stores the previously focused element, focuses the first focusable element or the container, closes on Escape, traps Tab/Shift+Tab, and restores focus on cleanup (`web/src/lib/useModalFocus.ts:21-83`).

### Every Production Drawer Usage

| Drawer surface | Trigger/owner | Direct child/content | Form/table/modal interactions |
|---|---|---|---|
| Node create | `NodesPage` renders `CreateNodeDrawer` at `web/src/pages/NodesPage.tsx:693-706`; toggle/reset behavior at `web/src/pages/NodesPage.tsx:652-658` and close callback at `web/src/pages/NodesPage.tsx:699-702`. | `CreateNodeDrawer` renders description plus raw `<form>` (`web/src/pages/nodes/CreateNodeDrawer.tsx:30-118`). Fields: display name, group, region, city, provider, fixed lifecycle text, labels, note (`web/src/pages/nodes/CreateNodeDrawer.tsx:31-110`). | Submit handled by `NodesPage.handleCreate`, trims payload, calls `createNode`, prepends new node, closes/resets form, and navigates to `/nodes/{id}/onboarding` (`web/src/pages/NodesPage.tsx:259-288`). No in-Drawer table. Modal behavior comes from Drawer overlay/Escape/close button. |
| Target create | `TargetsPage` Drawer at `web/src/pages/TargetsPage.tsx:590-604`; open/close/reset at `web/src/pages/TargetsPage.tsx:120-134`. | Direct child `CreateTargetPanel` (`web/src/pages/TargetsPage.tsx:596-603`). It renders `.target-create-drawer` with a `.target-create-drawer__form` form (`web/src/pages/targets/CreateTargetPanel.tsx:30-172`). Fields: name, target type, host, base port, execution node labels, run status, group, labels, note (`web/src/pages/targets/CreateTargetPanel.tsx:37-151`). | Submit validates/builds payload via `buildCreateTargetInput`, requires execution node labels, calls `createTarget`, updates target list, then navigates to target detail (`web/src/pages/TargetsPage.tsx:143-174`; builder at `web/src/pages/targets/targetHelpers.ts:309-327`). Targets table remains outside the Drawer (`web/src/pages/TargetsPage.tsx:629` onward). |
| VPS create | `VPSPage` Drawer at `web/src/pages/VPSPage.tsx:784-870`; open/close/reset at `web/src/pages/VPSPage.tsx:574-582`. | `.asset-create-drawer` with description and `.asset-create-form` (`web/src/pages/VPSPage.tsx:790-868`). Form uses fieldsets for base identity, access, run/decision, and notes/labels (`web/src/pages/VPSPage.tsx:795-856`). Many controls use the `Input` atom; selects are wrapped in `.input-field` with `.input` class (`web/src/pages/VPSPage.tsx:797-855`). | Submit builds `CreateVPSAssetInput`, calls `createVPSAsset`, and navigates to `/vps/{id}` (`web/src/pages/VPSPage.tsx:584-604`). The VPS inventory `DataTable` remains visible outside the Drawer (`web/src/pages/VPSPage.tsx:745-781`). |
| VPS advanced filter | `VPSPage` Drawer at `web/src/pages/VPSPage.tsx:872-915`; `openFilterDrawer` copies applied filters to draft state (`web/src/pages/VPSPage.tsx:564-567`), `applyDrawerFilters` writes URL and closes (`web/src/pages/VPSPage.tsx:569-572`). | `.asset-filter-drawer` containing four `FilterSelect` controls for provider, lifecycle, usage status, renewal decision, plus reset/apply actions (`web/src/pages/VPSPage.tsx:878-913`). | Applying writes search params used by the inventory table/chips; closing through Drawer `onClose` only sets `filterDrawerOpen` false (`web/src/pages/VPSPage.tsx:872-876`). Next open re-initializes draft from applied filters (`web/src/pages/VPSPage.tsx:564-567`). |
| Events advanced filter | `EventsPage` renders `EventsFilterDrawer` at `web/src/pages/EventsPage.tsx:402-410`; draft/apply/reset/close handlers at `web/src/pages/EventsPage.tsx:308-348`. | `EventsFilterDrawer` direct child is a `.events-filter-drawer` form (`web/src/pages/events/EventsFilterDrawer.tsx:49-175`). It contains time range tabs, `FilterSelect` controls, `FilterToggle` controls, raw input fields for custom start/end and label, an include-backfilled summary block, and apply/reset/close buttons (`web/src/pages/events/EventsFilterDrawer.tsx:50-174`). | Submit calls `onApply` (`web/src/pages/events/EventsFilterDrawer.tsx:43-46`). `EventsPage` keeps URL/applied filters separate from draft; close resets draft to applied without committing (`web/src/pages/EventsPage.tsx:330-338`), apply commits filters and closes (`web/src/pages/EventsPage.tsx:340-343`), reset commits defaults and closes (`web/src/pages/EventsPage.tsx:345-348`). Event stream/list stays outside the Drawer (`web/src/pages/EventsPage.tsx:412` onward). |
| Asset decision work | `AssetDecisionsPage` Drawer at `web/src/pages/AssetDecisionsPage.tsx:640-657`; selected VPS controls open state. | Direct child `AssetDecisionWorkPanel surface="drawer"` (`web/src/pages/AssetDecisionsPage.tsx:646-656`). The child renders `.asset-decision-panel--drawer` and, when a VPS is selected, a `.asset-operation-form` with header, renewal decision select, reason textarea, error, cancel/save actions (`web/src/components/AssetDecisionWorkPanel.tsx:45-103`). | Submit updates `renewal_decision`, updates decision queues, closes Drawer, and leaves success notice in the page surface (`web/src/pages/AssetDecisionsPage.tsx:472-500`). The queue and renewal evidence table remain outside the Drawer (`web/src/pages/AssetDecisionsPage.tsx:609-638`). |
| VPS detail multiplexed operations | Single `VPSDetailPage` Drawer at `web/src/pages/VPSDetailPage.tsx:838-847`; title from `activeDrawer` (`web/src/pages/VPSDetailPage.tsx:641-649`). | Direct child wrapper `.vps-detail-drawer` renders `renderDrawerContent()` (`web/src/pages/VPSDetailPage.tsx:844-846`). Possible children: `VPSRenewalDecisionForm`, `VPSFactsEditForm`, `VPSNodeLinkForm`, `VPSExperienceLogForm`, `VPSServicesForm`, `VPSDomainsForm` (`web/src/pages/VPSDetailPage.tsx:651-739`). | Close resets the active draft/errors/notices for every mode and sets `activeDrawer` null (`web/src/pages/VPSDetailPage.tsx:333-364`). Successful decision/facts/experience refresh detail/timeline; service/domain creates refresh only their lists; node link refreshes detail (`web/src/pages/VPSDetailPage.tsx:366-615`). VPS detail tables/sections remain outside Drawer (`web/src/pages/VPSDetailPage.tsx:805-823`). Archive/restore is a separate confirmation surface, not this routine Drawer (`web/src/pages/vps-detail/VPSLifecycleCard.tsx:38`). |
| Node history | `NodeDetailPageBody` renders `NodeHistoryDrawer` at `web/src/pages/node-detail/NodeDetailPageBody.tsx:286-298`; `NodeDetailPage` opens it with selected tab (`web/src/pages/NodeDetailPage.tsx:685-688`) and closes it (`web/src/pages/NodeDetailPage.tsx:746-748`). | `NodeHistoryDrawer` contains `Tabs`, then either empty/error state, `EventList`, loading text, warning `Card` with retry, or `IncidentList` (`web/src/pages/node-detail/NodeHistoryDrawer.tsx:38-87`). | No form. It is a list/history modal surface over the detail page. Incident tab may trigger history incident loading in the page state. |
| Node command | `NodeDetailPageBody` renders `NodeCommandDrawer` at `web/src/pages/node-detail/NodeDetailPageBody.tsx:300-309`; `NodeDetailPage` opens/resets error (`web/src/pages/NodeDetailPage.tsx:736-739`) and closes/resets error (`web/src/pages/NodeDetailPage.tsx:741-744`). | `NodeCommandDrawer` contains `.command-picker` command buttons, optional `.watchtower-runtime-error`, and optional `NodeCommandResult` (`web/src/pages/node-detail/NodeCommandDrawer.tsx:28-57`). | No form element; command buttons call `onExecute(command.id)` (`web/src/pages/node-detail/NodeCommandDrawer.tsx:35-44`). `NodeDetailPage.handleCommandExecute` posts node action and writes `last_action` into node state (`web/src/pages/NodeDetailPage.tsx:811-856`). |
| Target history | `TargetDetailPageBody` renders `TargetHistoryDrawer` at `web/src/pages/target-detail/TargetDetailPageBody.tsx:278-290`; `TargetDetailPage` opens it with selected tab (`web/src/pages/TargetDetailPage.tsx:389-392`) and closes via `setHistoryOpen(false)` (`web/src/pages/TargetDetailPage.tsx:912-918`). | `TargetHistoryDrawer` contains `Tabs`, then either empty/error state, `EventList`, loading text, warning `Card` with retry, or `IncidentList` (`web/src/pages/target-detail/TargetHistoryDrawer.tsx:38-87`). | No form. It is a list/history modal surface over target detail. |

### VPS Detail Drawer Child Forms

| Mode/title | Child component | Fields/content | Submit/close behavior |
|---|---|---|---|
| `续费决策` | `VPSRenewalDecisionForm` | `.asset-operation-form`; renewal decision `<select>`, reason `<textarea>`, error/notice, cancel/save (`web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx:35-86`). | Validates decision change, updates VPS asset, refreshes detail/timeline, closes Drawer, and sets notice (`web/src/pages/VPSDetailPage.tsx:366-393`). Close/cancel resets decision draft to saved value and clears feedback (`web/src/pages/VPSDetailPage.tsx:333-339`). |
| `编辑基础信息` | `VPSFactsEditForm` | `.asset-facts-edit-form`; many `Input` atom fields, usage status `select.input`, error/notice, cancel/save (`web/src/pages/vps-detail/VPSFactsEditForm.tsx:28-78`). | Builds update input, updates VPS asset, refreshes detail/timeline, updates fact draft, closes Drawer, and sets notice (`web/src/pages/VPSDetailPage.tsx:403-430`). Close/cancel restores draft from current detail and clears feedback (`web/src/pages/VPSDetailPage.tsx:340-346`). |
| `关联 Node` | `VPSNodeLinkForm` | `.asset-operation-form`; Node ID `Input`, note `<textarea>`, error/notice, cancel/link (`web/src/pages/vps-detail/VPSNodeLinkForm.tsx:33-78`). | Validates Node ID, calls `linkVPSNode`, refreshes detail, resets draft, closes Drawer, and sets notice (`web/src/pages/VPSDetailPage.tsx:433-463`). Close/cancel clears link draft and feedback (`web/src/pages/VPSDetailPage.tsx:347-350`). |
| `经验记录` | `VPSExperienceLogForm` | `.asset-operation-form`; category/severity selects, summary/occurred-at `Input`s, details `<textarea>`, error/notice, cancel/save (`web/src/pages/vps-detail/VPSExperienceLogForm.tsx:32-118`). | Builds experience input, calls `createVPSExperienceLog`, refreshes detail/timeline, resets draft, closes Drawer, and sets notice (`web/src/pages/VPSDetailPage.tsx:530-557`). Close/cancel resets draft and clears feedback (`web/src/pages/VPSDetailPage.tsx:351-354`). |
| `新增服务` | `VPSServicesForm` | `.asset-operation-form asset-service-form`; service name `Input`, type/status selects, URL/port/Target/labels `Input`s, note `<textarea>`, error/notice, cancel/create (`web/src/pages/vps-detail/VPSServicesForm.tsx:30-148`). | Builds service input, calls `createVPSService`, refreshes services only, resets draft, closes Drawer, and sets notice (`web/src/pages/VPSDetailPage.tsx:559-586`). Close/cancel resets draft and clears feedback (`web/src/pages/VPSDetailPage.tsx:355-358`). |
| `新增域名` | `VPSDomainsForm` | `.asset-operation-form asset-service-form`; domain `Input`, status select, purpose/Service ID/Target ID/registrar/expires/labels `Input`s, auto-renew and HTTPS checkboxes, note `<textarea>`, error/notice, cancel/create (`web/src/pages/vps-detail/VPSDomainsForm.tsx:30-167`). | Builds domain input, calls `createVPSDomain`, refreshes domains only, resets draft, closes Drawer, and sets notice (`web/src/pages/VPSDetailPage.tsx:588-615`). Close/cancel resets draft and clears feedback (`web/src/pages/VPSDetailPage.tsx:359-362`). |

### Code Patterns

#### 1. Drawer state usually lives in the owning page; child components receive controlled draft + callbacks

- `EventsPage` keeps applied filters and draft filters separate; `openFiltersDrawer` initializes draft, `closeFiltersDrawer` restores draft from applied state, and `applyDraftFilters` commits (`web/src/pages/EventsPage.tsx:330-343`).
- `VPSDetailPage` uses a single `activeDrawer` union and one `closeDrawer()` that switches on active mode to reset drafts/errors/notices (`web/src/pages/VPSDetailPage.tsx:333-364`).
- `AssetDecisionsPage` uses `selectedVPS` as open state and resets selected/draft/error on close (`web/src/pages/AssetDecisionsPage.tsx:461-465`).
- `NodesPage` and `TargetsPage` close handlers reset create draft/error (`web/src/pages/NodesPage.tsx:699-702`; `web/src/pages/TargetsPage.tsx:131-134`).

#### 2. Drawer children are mounted in `document.body`, outside page wrappers

- `Drawer` portals to `document.body` (`web/src/components/atoms/Drawer.tsx:35-64`). Therefore descendant CSS that depends on page ancestors like `.page-stack` only applies if that class is inside the Drawer subtree.
- The generic pure HTML form styling is scoped to `.page-stack` (`web/src/styles/pages.css:2210-2255`) and `.page-panel form` (`web/src/styles/pages.css:2519-2537`), not globally.
- Drawer-specific form styling exists for Events filters (`web/src/styles/pages.css:2989-3079`), target create (`web/src/styles/pages.css:4395-4538`), VPS create/filter (`web/src/styles/pages.css:4470-4551`), VPS detail operation wrappers (`web/src/styles/pages.css:3196-3279`, `web/src/styles/pages.css:3879-3890`), and command picker (`web/src/styles/pages.css:6386-6407`).

#### 3. Child form class coverage varies by surface

- `CreateNodeDrawer` has `<form onSubmit={onSubmit}>` with no `className` and raw `<label><input>` / `<textarea>` fields (`web/src/pages/nodes/CreateNodeDrawer.tsx:31-117`). Its submit button uses `btn` classes, but the form/fields are not wrapped in `.page-stack`, `.input-field`, `.input`, or a Drawer-specific form class (`web/src/pages/nodes/CreateNodeDrawer.tsx:32-116`).
- `CreateTargetPanel` uses `.target-create-drawer__form`, and CSS styles its labels, inputs, selects, and textarea (`web/src/pages/targets/CreateTargetPanel.tsx:36-171`; `web/src/styles/pages.css:4428-4468`).
- `VPSPage` create uses `.asset-create-form` with fieldsets and mostly `Input` atoms / `.input-field` + `.input` selects (`web/src/pages/VPSPage.tsx:794-868`), with layout CSS in `web/src/styles/pages.css:4470-4528`.
- `EventsFilterDrawer` uses `.events-filter-drawer`, `FilterSelect`, `FilterToggle`, and `.events-filter-drawer__field` wrappers for raw inputs (`web/src/pages/events/EventsFilterDrawer.tsx:49-175`; `web/src/styles/pages.css:2989-3079`).
- `AssetDecisionWorkPanel` uses `.asset-operation-form` and gives its select/textarea className `input` (`web/src/components/AssetDecisionWorkPanel.tsx:61-103`).
- Several VPS detail child forms use `.asset-operation-field` wrappers, but raw selects/textareas do not consistently carry `.input`: `VPSRenewalDecisionForm` select/textarea have no `.input` class (`web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx:43-70`); `VPSExperienceLogForm` selects/textarea have no `.input` class (`web/src/pages/vps-detail/VPSExperienceLogForm.tsx:40-102`); `VPSServicesForm` selects/textarea have no `.input` class (`web/src/pages/vps-detail/VPSServicesForm.tsx:47-132`); `VPSDomainsForm` select/textarea/checkboxes have no `.input` class (`web/src/pages/vps-detail/VPSDomainsForm.tsx:47-151`). The CSS found for `.asset-operation-field` sets layout and textarea height, but not general select/input/textarea color/background/border (`web/src/styles/pages.css:3229-3243`).

#### 4. Table/list interactions stay outside the Drawer except history/list children

- VPS inventory `DataTable` sits outside the create/filter Drawers (`web/src/pages/VPSPage.tsx:745-781`); filter Drawer apply updates URL/search params, which then affects the table.
- Targets table/list sits after the create Drawer (`web/src/pages/TargetsPage.tsx:590-629`); create success navigates to detail.
- Asset decision queue and renewal evidence table sit outside the decision Drawer (`web/src/pages/AssetDecisionsPage.tsx:609-638`); decision success updates queue state and closes Drawer (`web/src/pages/AssetDecisionsPage.tsx:489-500`).
- Node/Target history Drawers place `EventList`/`IncidentList` inside the Drawer (`web/src/pages/node-detail/NodeHistoryDrawer.tsx:50-87`; `web/src/pages/target-detail/TargetHistoryDrawer.tsx:50-87`).

#### 5. Tests cover many Drawer contracts

- Drawer atom tests exist in `web/src/components/atoms/Drawer.test.tsx`; production component uses the shared focus hook and portal (`web/src/components/atoms/Drawer.tsx:28-64`).
- EventsPage tests cover opening the events filter dialog, applying/resetting, closing, and Escape discard (`web/src/pages/EventsPage.test.tsx:41-43`, `web/src/pages/EventsPage.test.tsx:492-509`).
- VPSPage tests cover advanced filter Drawer and create Drawer reset/reopen behavior (`web/src/pages/VPSPage.test.tsx:151-152`, `web/src/pages/VPSPage.test.tsx:225-248`, `web/src/pages/VPSPage.test.tsx:295-319`).
- AssetDecisionsPage tests cover renewal decision Drawer update and close/reopen reset behavior (`web/src/pages/AssetDecisionsPage.test.tsx:183-188`, `web/src/pages/AssetDecisionsPage.test.tsx:287-309`).
- TargetsPage tests cover create target Drawer submit and close/reopen behavior (`web/src/pages/TargetsPage.test.tsx:112-122`, `web/src/pages/TargetsPage.test.tsx:354-401`, `web/src/pages/TargetsPage.test.tsx:1560-1573`).
- NodesPage tests cover create node Drawer toggling (`web/src/pages/NodesPage.test.tsx:121-137`, `web/src/pages/NodesPage.test.tsx:1353-1377`).
- VPSDetailPage tests cover each multiplexed Drawer mode and cancel/reset behavior (`web/src/pages/VPSDetailPage.test.tsx:624-691`) plus submit flows for decision/facts/node link/experience/service/domain (`web/src/pages/VPSDetailPage.test.tsx:530-535`, `web/src/pages/VPSDetailPage.test.tsx:803-815`, `web/src/pages/VPSDetailPage.test.tsx:966-969`, `web/src/pages/VPSDetailPage.test.tsx:1074-1082`, `web/src/pages/VPSDetailPage.test.tsx:1440-1449`, `web/src/pages/VPSDetailPage.test.tsx:1601-1613`).

### Layout / Theme Risk Inventory

| Surface | Observed risk | Evidence |
|---|---|---|
| Drawer atom theme | Drawer overlay/surface/header use hardcoded dark RGBA values instead of theme tokens for overlay, surface, and white borders. This can stay visually dark under light theme tokens. | `.drawer-overlay` uses `rgba(0,0,0,0.6)`; `.drawer` uses `rgba(10, 10, 15, 0.85)` and `rgba(255,255,255,0.1)`; header border uses `rgba(255,255,255,0.05)` (`web/src/styles/atoms.css:480-484`). Light theme tokens exist separately in `web/src/styles/tokens.css:115-162`. |
| Drawer atom layout | `.drawer__body` has `flex: 1`, but the base `.drawer` rule is a fixed panel without `display: flex` / column layout in the found rule. | `.drawer` rule at `web/src/styles/atoms.css:482`; `.drawer__body { padding: 24px; overflow-y: auto; flex: 1; }` at `web/src/styles/atoms.css:488`. |
| Drawer atom side borders | Base `.drawer` defines a left border with hardcoded white alpha, while direction modifiers add token borders. A left-side Drawer would retain the base left border and add right border unless later CSS clears it. | Base border-left at `web/src/styles/atoms.css:482`; `.drawer--right` border-left token and `.drawer--left` border-right token at `web/src/styles/atoms.css:871-882`. |
| Drawer atom width vs design doc | Design doc says desktop width 440px and max 40vw, narrow max 92vw. Current CSS uses width 440px and max-width 90vw. | Design doc `docs/design/v2-houfeng/component-spec.md:105-109`; CSS `web/src/styles/atoms.css:482`. |
| Node create Drawer naked form | Form has no form-level class, raw labels/inputs/textarea, error paragraph without role/class, and no cancel button inside the form. Because Drawer portals to body, `.page-stack` scoped form CSS is not inherited unless included inside the Drawer subtree. | `web/src/pages/nodes/CreateNodeDrawer.tsx:31-117`; portal at `web/src/components/atoms/Drawer.tsx:35-64`; `.page-stack` form styling at `web/src/styles/pages.css:2210-2255`. |
| VPS detail operation forms raw selects/textareas | VPS detail forms have `.asset-operation-field` wrappers, but several raw selects/textareas lack `.input`. Found CSS for `.asset-operation-field` covers layout and textarea height only, not general control chrome. | `VPSRenewalDecisionForm` raw select/textarea (`web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx:43-70`); `VPSExperienceLogForm` raw selects/textarea (`web/src/pages/vps-detail/VPSExperienceLogForm.tsx:40-102`); `VPSServicesForm` raw selects/textarea (`web/src/pages/vps-detail/VPSServicesForm.tsx:47-132`); `VPSDomainsForm` raw select/textarea/check boxes (`web/src/pages/vps-detail/VPSDomainsForm.tsx:47-151`); CSS at `web/src/styles/pages.css:3229-3243`. |
| VPS detail drawer removes operation-card chrome | Inside `.vps-detail-drawer`, `.asset-operation-form` and `.asset-facts-edit-form` have padding/border/background removed, making the Drawer body itself the only containing surface. | `web/src/styles/pages.css:3879-3890`. |
| Asset decision drawer removes operation-card chrome | `.asset-decision-panel--drawer .asset-operation-form` removes padding, border, and background. | `web/src/styles/pages.css:3985-3992`. |
| Generic raw form styling is page-scoped | Raw form controls in Drawer content do not automatically receive `.page-stack` styles because Drawer renders under `document.body`. Drawer children need their own structured classes or atom classes to receive styling. | Portal at `web/src/components/atoms/Drawer.tsx:35-64`; `.page-stack` raw form selectors at `web/src/styles/pages.css:2210-2255`; reset only inherits font/color for raw controls at `web/src/styles/reset.css:45-48`. |

### Related Specs

- `.trellis/spec/web/component-conventions.md:48` — modal/drawer focus behavior must use `web/src/lib/useModalFocus.ts`, portal to body, declare dialog semantics, trap focus, Escape close, restore focus.
- `.trellis/spec/web/component-conventions.md:49` — Drawer create/edit form close/cancel/Escape/overlay must discard draft, form errors, and save feedback; reopen must rebuild from saved data/empty form.
- `.trellis/spec/web/component-conventions.md:56` — list main scan-path create/edit forms should prefer `Drawer` so the primary table/queue remains visible.
- `.trellis/spec/web/state-and-data.md:216` — service create form should be in Drawer or equivalent secondary surface; main scan path shows service table and notice.
- `.trellis/spec/web/state-and-data.md:282` — domain create form should be in Drawer or equivalent secondary surface; main scan path shows domain table and notice.
- `.trellis/spec/web/state-and-data.md:293-295` — VPS detail decision/facts/Node link/experience/service/domain complex inputs use Drawer; facts Drawer excludes lifecycle status; archive/restore stays a separate confirmation.
- `.trellis/spec/web/state-and-data.md:451-462` — EventsPage advanced filters use applied/draft separation; Drawer close/Escape/overlay discards draft without URL/API changes.
- `docs/design/v2-houfeng/component-spec.md:105-109` — Drawer design contract: side slide-in panel, fixed portal, ESC/overlay close, focus containment, header/body, classes.
- `docs/design/v2-houfeng/component-spec.md:225` — AssetDecisionsPage decision editing uses Drawer with `AssetDecisionWorkPanel`; save notice stays in queue surface.
- `docs/design/v2-houfeng/component-spec.md:231` — VPSPage field filters use right-side Drawer and applied filters show as chips/URL state.

### External References

- None. Repo files only were inspected.

## Caveats / Not Found

- No external docs or web searches were used.
- This is an inventory of existing Drawer/form surfaces and observed layout/theme risk points only; it does not include implementation changes.
- The grep scope focused on production TSX/CSS under `web/src` and related repo specs/docs. Test files were used only as supporting evidence, not counted as production Drawer surfaces.

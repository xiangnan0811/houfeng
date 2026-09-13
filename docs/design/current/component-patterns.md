# Current component and page patterns

## Component defaults

The current frontend is a React/Vite SPA with plain CSS, design tokens, and shared atoms under `web/src/components/`. Prefer existing atoms and page primitives before adding a new abstraction.

Current reusable patterns include:

- `Button`, `Badge`, `Card`, `Input`, `Toggle`, and `Tabs` for ordinary controls;
- `Sparkline`, `MetricChart`, `TrendArrow`, and `StatusGlyph` for compact runtime evidence;
- `MonoDigits`, `Hostname`, and `Timestamp` for technical facts;
- `DataTable` for dense list scanning;
- `Drawer` for advanced filters and scoped edit flows;
- `DetailSection` for titled surfaces;
- `PageState` for route/list loading, error, and empty states;
- `ActionConfirmationCard` for explicit state transitions.

Do not add a new atom because an old design document named one. Add one only when current code has repeated behavior, clear ownership, and tests or usage that justify the abstraction.

## Page composition

The current product prefers workbench-first pages:

- show the primary workflow in the first viewport;
- keep hero/header areas compact;
- put filters and advanced controls in drawers when they would crowd the scanning path;
- keep tables and queues dense enough for real inventory sizes;
- show current evidence and next actions before historical details;
- keep dangerous lifecycle actions isolated with explicit review and confirmation.

This is guidance, not a page freeze. A page may change structure when the current task has a clearer workflow and updates tests/specs accordingly.

### VPS inventory dual-view contract

`/vps` owns one data and action flow with two presentations: a grouped scanning table with a compact quick inspector, and a directory with a richer reading inspector. Both reuse existing create/filter dialogs, fact formatters, and canonical management destinations; neither is a replacement for the independent VPS detail workspace. Inspectors summarize available list and subscription evidence, not a second detail API or an independent business-state source.

The switch is labeled 表格视图 / 目录视图; persisted `workbench` / `ledger` values remain unchanged. Both layouts consume the existing common theme and component language rather than maintaining page-local palettes, badge skins, or a separate editorial type scale. Shared-control contrast fixes belong to their shared semantic styles, not another VPS override.

- `workspace=ledger|workbench` selects presentation. A valid explicit URL wins over the browser preference `houfeng.vps.workspace`; absent a preference, use `ledger`. Storage denial must not prevent switching.
- Existing `view` is a business quick filter. `q` is local inventory search and `selected` identifies the inspected asset. Workspace changes and filter edits preserve these fields and unrelated URL parameters. Only the non-sensitive workspace preference is stored in localStorage.
- Clicking a table row or its native asset-name button toggles that row's accordion, without navigation. Selecting another row moves the single open accordion. Collapsing keeps the selected URL value. Use the explicit detail link inside the accordion to open management; the unlinked quick view retains the existing `workbench=monitoring` onboarding destination.
- If the selected asset is outside the filtered results, ledger shows an empty inspector and workbench shows no accordion rather than silently inspecting another asset. Clearing filters can restore the selection.
- Subscription loading and failure are not missing-subscription evidence. Do not infer runtime health, successful sync, or trends from the asset ledger.
- Browser verification must cover continuous text entry, view switching, selection, history, and narrow screens; a single synthetic change event does not prove typing works.

### List, quick inspection, and independent detail

- The table view prioritizes scanning an inventory of tens of machines. Its quick inspector starts collapsed and expands directly after the selected row, inside the list scroller. There is no bottom inspector strip. Expanded inspection is limited to identity, business/renewal facts, monitoring association, and available observation evidence; it must not become a second full detail page. On narrow screens, the list remains independently scrollable.
- Both inventory views enter the same canonical `/vps/:id` workspace. Both capability modes use a compact identity/action header beneath the sole validated top-bar inventory return. Organize by content rather than a permanent main/aside split: flexible asset/connection facts and a bounded billing summary form the first zone; observations, named service/domain lists and recent activity remain independent. The billing area is approximately 320–420px only while beside sufficiently wide facts, and stacks when needed. The route uses a comfortable 1600px ceiling, 16px padding, 14px gaps and natural heights. Keep shared palette and fine borders without decorative card shadows. The closed 页面目录 remains subordinate to real business tabs and preserves hash positioning/focus.
- Use the true subscription name and put its weak record ID on a separate subordinate line, distinct from asset identity. Combine amount and normalized period, then keep absolute renewal/expiry date and the VPS decision close; do not interpret 保留 as an automated resource action. Labeled record updates do not imply staleness. Existing scoped APIs or loaded legacy collections remain the source; loading/error are not empty collections. Resource rows have two content layers: name/status/explicit details action, then address/type/port/purpose. The row itself is not a button; full values and independent copy/links must remain usable. Copy stays adjacent to full wrapping IP/SSH values and announces success/failure; missing values are not copyable. Compact desktop controls retain adequate touch targets on narrow/coarse-pointer surfaces.
- Runtime observation rows keep scope, known result, source time and real actions nearby. Remove exact same-object status repetition without erasing distinct results or interpreting lifecycle as health. Missing/stale/unavailable evidence cannot expand a healthy claim; IP query failures do not erase valid monitoring or known historical IP findings. Keep not-configured, failed, absent and historical facts distinct according to the contract. Do not invent scores, denominators, configuration routes or incidents. Activity highlights action, event time and type; shorten only an exact current-asset prefix when distinguishing other-object information is retained, keeping the full accessible title. The top-bar 系统摘要 belongs to the fleet snapshot and is not asset health.
- Use one shared observation-row layout for project, primary conclusion, explanation, data time, and actions across capability modes. Desktop columns share stable tracks; narrow layouts reflow into consistent labelled slots. Keep long source errors in a subordinate row under their object, not interleaved with the main conclusion or action track. Existing result, latest read outcome, and source age are independent: name retained content only when it exists, and never describe visible retained evidence as wholly unavailable. Activity data time and event time are different fields.
- Shared fact rendering preserves all facts supplied by each existing source, including importance and labels already present in overview identity. Short fields may share columns; SSH and notes retain continuous width. Do not fabricate fields absent from a source or add requests to make capability modes identical. Successful empty resource groups use lightweight summaries; unavailable is not empty, and an empty report does not disable a real authorized report destination.
- The six bounded VPS detail dialogs share three content templates inside the existing Modal: short renewal decision, structured VPS/subscription forms, and associated-object reading. Keep the accepted main workspace unchanged. Use one task title and light asset context; renewal decisions describe saved intent, never supplier execution. Show current versus draft decision only when they differ.
- Service, domain, validity, monitoring-link, monitoring-create, and legacy experience forms reuse `.vps-form` / `VPSFormSection` and Modal title+fixed footer; do not repeat the task title in an inner `asset-operation-form__header`. Keep current subscription facts and prerequisite warnings. Do not delete shared `.vps-create-form*` or decision-work `asset-operation-form__header` styles still used outside VPS dialogs. Inventory list and inspector status use `LifecycleBadge` / `UsageBadge` / `RenewalBadge`; ledger directory may omit usage for density.
- Match width to content: decision 380px, facts 680px, services 520px, domains 560px, and subscription/monitoring reading 780px. Facts and service/domain reading become full-viewport sheets on narrow screens; other dialogs retain their existing gutters. Keep a fixed header, an independently scrolling body, and fixed actions only for actual forms. Subscription facts remain a new record, not an edit; preserve their existing fields and behavior.
- Service/domain reading keeps complete collections, neutral record status (not observed health), names, notes, labels and subordinate IDs. Show service URLs as full monospaced text with adjacent copy and HTTP(S)-only open actions on the following row; neither read panel has a save/cancel footer or a pretend edit action. Domains may enrich service IDs from the scoped service API; missing optional metadata must not block reading or reuse another VPS’s data. Existing legacy create actions remain separate. Monitoring reading and all ownership/focus/scroll safeguards are unchanged.
- Freshness belongs beside each object's own source timestamp, not inside its conclusion. Adjacent view and refresh actions share a wide-screen row without changing ordinary single-action columns. Overview refresh/retry rereads the overview; it does not request agent sync or a probe. Activity source time denotes latest visible recorded intake, independently of event time.
- Empty navigation retains the current VPS: a subscription collection is not an existing subscription object, zero monitoring opens association status rather than a fabricated instance, and a report route remains available when its real capability permits reading empty/history/error states. Label historical report entry only from known historical evidence. A lone unlinked-monitoring notice states its reason once, its observation impact, and the existing action or read-only explanation.
- Cancellation plans are not supplier cancellation confirmations. Keep registered renewal/billing dates separate from the VPS plan. Preserve the center's overall conclusion and name a pending cancellation plan as an additional attention basis when applicable, rather than attributing the difference from incomplete observations solely to missing IP evidence.
- Pending cancellation labels registered renewal and billing-period dates consistently for primary and extra subscriptions; trial expiry remains trial expiry. Report entry names distinguish actual report/history evidence from the generic results destination; successful unconfigured/empty responses are not read failures.
- An explicitly disabled optional IP-quality source with a ready section is outside the enabled-observation assessment, not a failed source. Keep its local 未启用 label and state the limited scope beside an otherwise healthy judgment. Stale or unavailable source metadata still limits the judgment, and known adverse findings must not be downgraded.
- Full-context reading uses the independent page. Management entries are grouped by business/billing, runtime, associations, and applicable lifecycle operations; grouping must not remove a destination or callback. Existing bounded edits, onboarding panels, and dangerous-action confirmations remain shared management operations, with their capability, freshness, read-only, and write-ownership checks preserved. The overview-enabled and capability-off presentations do not define separate business workflows.
- Inventory entries carry the current `/vps` query in React Router history state `vpsInventoryHref`. The top-bar return accepts only `/vps` with optional search parameters; absent or invalid context falls back to `/vps`. Onboarding query consumption, in-page sections, VPS activity/records/evidence navigation, and activity filter/retry replacements preserve that state. This is navigation context, not another persisted preference.
- Browser acceptance covers both layouts in global dark/light themes, selecting from a 30-asset inventory, collapsing/reopening inspection, opening actual independent detail, accessing and closing management, and returning with view/search/filter/selection intact. Entering the base detail page resets the shared main scroller; explicit hash navigation retains its section target. Fixed top bars must not cover return controls or in-page section targets.

## VPS facts editor

The shared editor is the create (添加 VPS) and edit facts form. It shows common identity/provider, country, city/IPv4, usage status, importance, labels and notes first. Put all lower-frequency facts in one default-collapsed 可选设置 without nested categories. IPv6 and custom SSH host/port are hidden until shown; existing configuration initializes them visible, and hiding or collapsing never erases values. Editing IPv4 still derives an automatic SSH host but must preserve a hidden custom host. The country field is one editable in-flow combo: 17 common locations grouped by geography, 232 remaining ISO locations behind one expansion, full Chinese/English/code search and custom values. Reopening a committed value returns to browsing. Usage status remains the existing five-value business enum, not free-text purpose; importance remains a native select. Keep the header and feedback/save/cancel footer fixed while the body scrolls. Create keeps inline provider creation, reset/error/pending/submit and navigation, and must not duplicate these fields. Facts dialog chrome (680px desktop, fullscreen on narrow screens) lives only in `VPSFactsEditForm.css`. Both overview and legacy retain the same real PATCH, provider snapshots, conflict recovery and write ownership. Service/domain update APIs are not part of this frontend change.

## Decision and workbench page IA

Decision-heavy pages (asset decisions, detail pages with decision boards) follow a three-tier information architecture instead of stacking every API field on one screen:

1. **Primary tier — one question**: "What should I act on now?" A single judgement plus a single primary action. When there is nothing to decide, show a quiet stable hint — no CTAs, warning colors, or stat cards.
2. **Scan tier — list**: one row per item: identity + status + single entry point, no explanatory sentences. Auxiliary entries (history, templates, renewal facts, single-VPS queue) collapse into a toolbar by default (one row on desktop, 2×2 on mobile) and expand a single panel on click.
3. **Bounded edit tier — modal**: at most 3 tabs, each tab one task. Default tab shows object name + one-line judgement + primary action. Long API text is trimmed to a short judgement, never shown verbatim. Raw data (full member lists, wide tables, execution details) lives behind an explicit "view all" entry; write payloads still use full data. This does not replace the independent VPS full-context workspace described above.
4. **Copy**: no explanatory paragraphs; eyebrows are Chinese or removed; field meaning is conveyed by labels and placeholders.

This replaces prior patch-style rules about specific markers, density caps, and preview limits with a positive contract. Tests should assert user task completion in ≤3 steps, not just absence of old markers.

## Current surface responsibilities

- Dashboard / workbench: daily entry point and highest-priority next actions, not a dump of every API field.
- Asset decisions: portfolio and scenario decisions, saved records, readback, and renewal/cost evidence.
- VPS inventory/detail: asset facts, subscriptions, lifecycle decisions, monitoring linkage, and local asset workbenches.
- Monitoring list/detail: runtime observation objects, health, sync/heartbeat evidence, trends, incidents, and controlled agent actions.
- Targets/detail: service entrypoint probing, ProbeItems, coverage, recent observations, and target events.
- Events: diagnostic and audit timeline with explicit filters.
- Settings: runtime configuration, notification settings, frequency defaults, overrides, retention, and theme controls.

Do not use these responsibilities to block future product exploration. Use them to avoid accidental duplication and to keep each page's current job legible.

## Associated operator workspaces

Monitoring and entrypoint detail pages lead with compact identity and current observations. Keep management and history subordinate, incident/event failures independently retryable, and missing or disabled probes distinct from failed observations. An empty incident list is not evidence that every runtime source is healthy. Onboarding names its monitoring instance, obtains installation commands only after an explicit request to the center, and keeps sensitive copy/reveal controls inside the authenticated dialog. Pending issuance or copying prevents dismissal; a closed, replaced, or unmounted dialog must not start copying a late response or restore its command. An already-started clipboard write cannot be cancelled. Probe and onboarding dialogs keep their action footer outside the scrolling body.

Activity, records, and evidence for VPS, monitoring instances, and entrypoints remain views of the shared subject workspace. Preserve the existing URL filter codec and location.state return context through record publication, restoration, and evidence links. Interactive filters match each view's server predicate; retained incompatible URL filters stay visible and removable. A scoped new record consumes the same canonical subject reference emitted by its entry link and isolates its unsynced buffer from other subjects and unscoped creation. Reopening the same entry restores its buffer without overwriting newer edits; unscoped draft recovery remains available from /records/new. A valid canonical return_to provides an explicit subject-return link, without changing the post-publication record destination. Record search prioritizes results; import/export remain secondary tools. Evidence links open the protected /evidence/:evidenceId reader using the existing exact renderer tuple and validated read model. Unsupported or invalid evidence fails closed without exposing raw payloads; source deletion does not erase an authorized retained snapshot.

IP quality remains a read-only report, not another asset overview. Keep report identity/time, provider and service results, and every returned historical report reachable; put collection diagnostics behind an explicit disclosure. Reuse existing risk calculations without introducing another score or treating unknown results as failures. Wide observation/provider tables have named, keyboard-focusable local scroll regions. These related workspaces use the VPS title/body density and stack title/actions and fact groups when narrow.

## Contracts and tests

Component and page changes should update the closest useful tests:

- API client/type changes need tests at the API boundary.
- Page workflow changes need page tests that assert visible behavior, URL state, and request shape where applicable.
- Styling-only changes should still use browser sanity for user-visible work when layout, density, route structure, or responsive behavior can regress.
- Historical design files are not tests. Current behavior is proven by code, tests, docs, and explicit evidence.

## Historical references

The old component/page bundle was replaced by a short stub at `../v2-houfeng/component-spec.md`; use git history if you need to inspect it. Treat any old page notes as source material, not an instruction to preserve every surface exactly.

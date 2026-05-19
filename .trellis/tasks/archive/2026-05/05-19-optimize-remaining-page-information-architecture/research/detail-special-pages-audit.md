# Research: detail and specialized page information architecture audit

- **Query**: Research current information architecture issues for detail/specialized pages. Focus on TargetDetailPage, NodeDetailPage, VPSDetailPage, NodeOnboardingPage, NodeComparePage, SettingsPage, LoginPage. Report page purpose, current structure, likely IA problems, whether Node/VPS need further fixes, quick wins, risk, and recommended MVP priority. Use concrete file paths.
- **Scope**: internal
- **Date**: 2026-05-19

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Route registration for login, Node/VPS/Target detail, onboarding, compare, settings. |
| `web/src/pages/TargetDetailPage.tsx` | Target detail container: data loading, runtime/probe/history/metadata state, mutation handlers. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Target detail visible section order and composition. |
| `web/src/components/target-detail/TargetWatchtowerHeader.tsx` | Target identity header and runtime menu. |
| `web/src/components/target-detail/TargetLatencyTrends.tsx` | Target latency trend cards; watchtower variant used by detail page. |
| `web/src/pages/target-detail/TargetProbeListSection.tsx` | Collapsed ProbeItem list wrapper. |
| `web/src/components/target-detail/TargetProbeList.tsx` | Probe cards and nested latest observations table. |
| `web/src/pages/target-detail/TargetProbeManagementSection.tsx` | Probe add/edit entry and inline form surface. |
| `web/src/pages/target-detail/TargetMetadataSection.tsx` | Collapsed metadata section wrapper. |
| `web/src/components/target-detail/TargetLabelsAndNote.tsx` | Target group/labels/note view/edit content. |
| `web/src/pages/target-detail/TargetLifecycleSection.tsx` | Target archive/restore lifecycle surface. |
| `web/src/pages/target-detail/TargetHistoryDrawer.tsx` | Target event/history incident drawer. |
| `web/src/pages/NodeDetailPage.tsx` | Node detail container: data loading, runtime/history/command/binding/linked-VPS state. |
| `web/src/pages/node-detail/NodeDetailPageBody.tsx` | Node detail visible section order and composition. |
| `web/src/components/node-detail/NodeWatchtowerHeader.tsx` | Node identity header, history link, runtime/command/onboarding/lifecycle menu. |
| `web/src/components/node-detail/NodeWatchtowerMetrics.tsx` | Node host metrics chart grid, sorted by threshold priority. |
| `web/src/pages/node-detail/NodeBindingConflictSection.tsx` | Node detail binding conflict action section. |
| `web/src/pages/node-detail/NodeLinkedVPSSection.tsx` | Node detail linked VPS evidence section. |
| `web/src/pages/node-detail/NodeDangerCard.tsx` | Node current primary issue card. |
| `web/src/pages/node-detail/NodeHistoryDrawer.tsx` | Node event/history incident drawer. |
| `web/src/pages/VPSDetailPage.tsx` | VPS detail container: asset detail/timeline/services/domains/subscriptions loading, drawer state, mutation handlers. |
| `web/src/pages/vps-detail/VPSDetailHero.tsx` | VPS identity hero and main action/menu surface. |
| `web/src/pages/vps-detail/VPSDecisionWorkbench.tsx` | VPS primary asset decision workbench. |
| `web/src/pages/vps-detail/VPSOperationsSummary.tsx` | VPS evidence summary cards and detail-entry buttons. |
| `web/src/pages/NodeOnboardingPage.tsx` | Node onboarding/install/binding workflow page. |
| `web/src/pages/NodeComparePage.tsx` | Node A/B comparison page. |
| `web/src/pages/SettingsPage.tsx` | Settings container: tabs, channel modal, global save, payload assembly. |
| `web/src/pages/settings/ThemeSettingsSection.tsx` | Settings theme section. |
| `web/src/pages/settings/FrequencyDefaultsSection.tsx` | Settings default frequency section. |
| `web/src/pages/settings/TelegramSettingsSection.tsx` | Settings Telegram notification section. |
| `web/src/pages/settings/FeishuSettingsSection.tsx` | Settings Feishu notification section. |
| `web/src/pages/settings/IncidentDefaultsSection.tsx` | Settings incident defaults and thresholds section. |
| `web/src/pages/LoginPage.tsx` | Login page and redirect handling. |
| `web/src/pages/*.test.tsx` | Page-level tests covering current behavior for the audited pages. |
| `.trellis/spec/web/component-conventions.md` | Web component/page organization conventions and known gaps. |
| `.trellis/spec/web/styling-guidelines.md` | Styling/IA-adjacent constraints, including PageState and known Settings inline-style gaps. |
| `.trellis/spec/web/directory-structure.md` | Page/component/lib/style placement rules. |
| `docs/design/v2-houfeng/component-spec.md` | Current visual/page IA authority for detail/specialized pages. |
| `docs/design/v2-houfeng/design-language.md` | Current design-language authority for density, hierarchy, states, and dark-first UI. |

### Current Route Purposes

`web/src/app/router.tsx` registers the audited pages as protected app routes except `/login`: `/vps/:vpsId`, `/nodes/compare`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets/:targetId`, `/settings`, plus public `/login` (`web/src/app/router.tsx:62-94`). This makes the pages specialized decision/workbench routes rather than generic CRUD screens.

## Page-by-page audit

### 1. TargetDetailPage

#### Page purpose

Target detail is the specialized observability page for one `Target = an observable entrypoint`. It combines target identity, runtime state, latency observations, ProbeItem management, metadata, lifecycle, and history.

#### Current structure

- Container loads target identity, ProbeItems, and runtime facts together (`web/src/pages/TargetDetailPage.tsx:225-272`).
- Activity data is loaded separately via `listIncidents({ object_type: 'target' })` and `listEvents({ object_type: 'target' })` (`web/src/pages/TargetDetailPage.tsx:274-303`).
- Historical incidents are lazy-loaded only when the history drawer is open on the incidents tab (`web/src/pages/TargetDetailPage.tsx:305-364`).
- Render passes everything into `TargetDetailPageBody` (`web/src/pages/TargetDetailPage.tsx:816-921`).
- Visible body order is:
  1. `TargetWatchtowerHeader` (`web/src/pages/target-detail/TargetDetailPageBody.tsx:172-181`)
  2. runtime pause confirmation/error (`web/src/pages/target-detail/TargetDetailPageBody.tsx:183-195`)
  3. conditional current-problem danger card (`web/src/pages/target-detail/TargetDetailPageBody.tsx:197-203`)
  4. time-window tabs (`web/src/pages/target-detail/TargetDetailPageBody.tsx:205`)
  5. latency trends (`web/src/pages/target-detail/TargetDetailPageBody.tsx:207-212`)
  6. collapsed ProbeItem list (`web/src/pages/target-detail/TargetDetailPageBody.tsx:214-228`)
  7. property-list region with Probe management, metadata, lifecycle (`web/src/pages/target-detail/TargetDetailPageBody.tsx:230-274`)
  8. snapshot meta and history drawer (`web/src/pages/target-detail/TargetDetailPageBody.tsx:276-290`)
- Header shows target name, run/health/type badges, freshness, history button, and conditional runtime menu (`web/src/components/target-detail/TargetWatchtowerHeader.tsx:58-140`).
- Latency trends in watchtower mode render a bare `<section aria-label="近期延迟趋势">` with cards, not a `DetailSection` header (`web/src/components/target-detail/TargetLatencyTrends.tsx:117-194`).
- ProbeItem list is inside `<details className="watchtower-secondary">` (`web/src/pages/target-detail/TargetProbeListSection.tsx:40-61`). Each ProbeItem card can contain a nested compact `DataTable` of observations (`web/src/components/target-detail/TargetProbeList.tsx:142-245`).
- Metadata is also collapsed under `<details>` (`web/src/pages/target-detail/TargetMetadataSection.tsx:38-57`). Lifecycle is a non-details `watchtower-secondary` surface (`web/src/pages/target-detail/TargetLifecycleSection.tsx:34-84`).

#### Likely IA problems

- **Core Target evidence is lower-visibility than Node/VPS after their refactors.** The current primary scan path is identity → optional danger → time tabs → latency. ProbeItem configuration and observation rows, which are central to understanding why the Target behaves as it does, are collapsed under `ProbeItem 列表` (`web/src/pages/target-detail/TargetProbeListSection.tsx:40-61`). By contrast, the current component spec describes Target detail as explicit hero/summary/DetailSection blocks, including `DetailSection ProbeItem 列表`, current incidents, and events (`docs/design/v2-houfeng/component-spec.md:310-325`).
- **Spec and implementation currently express different page hierarchy.** Spec says TargetDetailPage starts with hero meta cards and summary KPI cards (`docs/design/v2-houfeng/component-spec.md:310-314`), while implementation uses the watchtower header pattern and no target summary grid (`web/src/pages/target-detail/TargetDetailPageBody.tsx:172-212`). This may be intentional borrowing from Node detail, but it is not yet documented as the new Target authority.
- **Events/current incidents are drawer-first rather than main evidence sections.** Active incidents only surface as a danger card when count > 0 (`web/src/pages/target-detail/TargetDetailPageBody.tsx:197-203`); events are available through the history drawer (`web/src/pages/target-detail/TargetHistoryDrawer.tsx:37-88`). Spec still lists `DetailSection 当前异常` and `DetailSection 事件` on the page (`docs/design/v2-houfeng/component-spec.md:323-324`).
- **Probe creation/editing and Probe list are split into two separate surfaces.** The Probe list is one collapsed `<details>` (`TargetProbeListSection.tsx:40-61`), while add/edit lives later in the property-list (`TargetProbeManagementSection.tsx:43-75`). For a Target page, this separates the object being configured from the create/edit affordance.
- **Latency section lacks an explicit title in watchtower mode.** It uses only meta text plus cards (`TargetLatencyTrends.tsx:117-194`), so after the time-window tabs the user sees charts without the stronger section title/aside contract used by `DetailSection` elsewhere.

#### Quick wins

1. Make Target detail the highest MVP IA priority among the audited pages.
2. Decide whether Target detail should follow the newer Node watchtower pattern or the older explicit TargetDetail spec, then update page structure/spec accordingly.
3. Move or expose ProbeItem list closer to latency trends so "probe definition → latest observations → latency" scans as one evidence chain.
4. Add a visible section heading for latency trends in watchtower mode.
5. Surface current incidents/events in a lightweight summary on the main page if the drawer remains the full-history surface.

#### Risk

- **Implementation risk: medium-high.** `TargetDetailPage.tsx` has many intertwined local states for runtime confirmations, probe creation/editing/deletion, metadata optimistic update, history drawer, focus restore, and request guards (`web/src/pages/TargetDetailPage.tsx:70-118`, `web/src/pages/TargetDetailPage.tsx:419-806`). IA changes that move Probe forms or confirmations can easily affect focus restoration and mutation disable rules.
- **Product risk: medium.** Target is a core observability object; hiding or scattering Probe evidence can slow diagnosis even if the underlying data is present.

#### Recommended MVP priority

**P0 / highest among remaining pages.** Target detail is the clearest remaining mismatch against the current Node/VPS IA direction and against the active component spec.

---

### 2. NodeDetailPage

#### Page purpose

Node detail is a watchtower-style operational page for one server/agent runtime instance. It prioritizes current problem, host metrics, runtime controls, binding conflicts, linked VPS context, containers, and history/command drawers.

#### Current structure

- Container fetches node + runtime facts in the main load (`web/src/pages/NodeDetailPage.tsx:169-202`).
- Linked VPS summaries are lazy-loaded when the section enters the viewport using `IntersectionObserver` (`web/src/pages/NodeDetailPage.tsx:221-316`).
- Binding conflict details are loaded only when `binding_status === '指纹变更待确认'` (`web/src/pages/NodeDetailPage.tsx:318-374`).
- Active incidents/events are loaded separately (`web/src/pages/NodeDetailPage.tsx:376-405`).
- History incidents are lazy-loaded by drawer tab (`web/src/pages/NodeDetailPage.tsx:415-464`).
- Command drawer polls node data while an action is pending (`web/src/pages/NodeDetailPage.tsx:466-493`).
- Body renders:
  1. `NodeWatchtowerHeader` (`web/src/pages/node-detail/NodeDetailPageBody.tsx:157-171`)
  2. pause confirmation/runtime error (`web/src/pages/node-detail/NodeDetailPageBody.tsx:173-181`)
  3. conditional current-problem danger card (`web/src/pages/node-detail/NodeDetailPageBody.tsx:183-189`)
  4. conditional binding conflict section (`web/src/pages/node-detail/NodeDetailPageBody.tsx:191-202`)
  5. time-window tabs + host metrics (`web/src/pages/node-detail/NodeDetailPageBody.tsx:204-210`)
  6. linked VPS (`web/src/pages/node-detail/NodeDetailPageBody.tsx:212-218`)
  7. retire confirmation/lifecycle error (`web/src/pages/node-detail/NodeDetailPageBody.tsx:220-237`)
  8. containers, snapshot, history drawer, command drawer (`web/src/pages/node-detail/NodeDetailPageBody.tsx:239-272`)
- Header includes identity, four statuses, freshness, history button, runtime menu, onboarding link, command action, retire/restore action (`web/src/components/node-detail/NodeWatchtowerHeader.tsx:50-157`).
- Host metrics sort cards by threshold-derived priority (`web/src/components/node-detail/NodeWatchtowerMetrics.tsx:86-118`, `web/src/components/node-detail/NodeWatchtowerMetrics.tsx:394-418`).
- Linked VPS section explicitly explains Node vs VPS semantics (`web/src/pages/node-detail/NodeLinkedVPSSection.tsx:76-109`).

#### Likely IA problems

- **Binding conflict order differs from the active spec.** The spec says binding conflict is highest priority and appears above the danger card (`docs/design/v2-houfeng/component-spec.md:265-268`). The current body renders `NodeDangerCard` first, then `NodeBindingConflictSection` (`web/src/pages/node-detail/NodeDetailPageBody.tsx:183-202`). If a node has both active incidents and binding conflict, the binding action can be visually demoted.
- **Some secondary sections are not actually collapsed.** The Node watchtower spec describes secondary information as default-collapsed `<details>` for labels/notes, lifecycle, and onboarding credential status (`docs/design/v2-houfeng/component-spec.md:272-276`). Current implementation keeps linked VPS and containers as full sections on the main page (`web/src/pages/node-detail/NodeDetailPageBody.tsx:212-218`, `web/src/pages/node-detail/NodeDetailPageBody.tsx:239-245`). This may be a deliberate post-refactor choice, but it differs from the spec.
- **No explicit labels/notes edit surface is present in the current Node detail body.** The body has header metadata, metrics, linked VPS, containers, lifecycle confirmation, history/commands, but no `NodeLabelsAndNote` equivalent in the visible order (`web/src/pages/node-detail/NodeDetailPageBody.tsx:157-273`).
- **Runtime command and onboarding are hidden behind the header ellipsis.** `NodeWatchtowerHeader` puts onboarding and command actions inside `<details>` (`web/src/components/node-detail/NodeWatchtowerHeader.tsx:71-115`). This keeps the scan clean, but it can make operational actions less discoverable.

#### Whether Node needs further fixes

**Yes, but surgical rather than another full IA refactor.** Recent Node detail architecture is close to the watchtower direction: identity, current problem, priority-sorted metrics, history drawer, command drawer, linked VPS evidence, and request guards are in place. The remaining fixes are mainly order/alignment and secondary-section decisions.

#### Quick wins

1. Move binding conflict above current-problem danger card to match the documented highest-priority behavior.
2. Decide whether linked VPS and containers are primary evidence or secondary details; if secondary, collapse or summarize them.
3. Add/restore a small labels/notes surface if editable Node metadata remains a detail-page requirement.
4. Consider a more visible "接入工作台" affordance when `binding_status` is not healthy, while keeping the ellipsis for normal nodes.

#### Risk

- **Implementation risk: medium.** The page has race guards, focus restoration, lazy linked-VPS loading, drawer state, polling, and multiple mutation pathways (`web/src/pages/NodeDetailPage.tsx:90-107`, `web/src/pages/NodeDetailPage.tsx:513-786`). Reordering simple sections is low risk; moving actions/forms is higher risk.
- **Product risk: low-medium.** Node page is already significantly refactored and useful. The main risk is a conflict state being visually lower than ordinary active incidents.

#### Recommended MVP priority

**P1.** Do the binding-conflict ordering and any obvious secondary-section trim after Target detail, not before it.

---

### 3. VPSDetailPage

#### Page purpose

VPS detail is the single-asset decision workbench for one VPS: identity, renewal decision, subscription/cost evidence, linked Node health evidence, service/domain context, facts, timeline, and lifecycle operations.

#### Current structure

- Initial load fetches VPS detail, timeline, services, domains, and scoped subscriptions in parallel (`web/src/pages/VPSDetailPage.tsx:179-185`).
- Page normalizes `node_links` and stores detail/timeline/services/domains/subscriptions in one state object (`web/src/pages/VPSDetailPage.tsx:186-223`).
- Render order is:
  1. `VPSDetailHero` (`web/src/pages/VPSDetailPage.tsx:879-894`)
  2. `VPSDecisionWorkbench` (`web/src/pages/VPSDetailPage.tsx:896-908`)
  3. subscription/decision feedback (`web/src/pages/VPSDetailPage.tsx:910-925`)
  4. `VPSOperationsSummary` (`web/src/pages/VPSDetailPage.tsx:927-951`)
  5. conditional lifecycle confirmation (`web/src/pages/VPSDetailPage.tsx:953-964`)
  6. Drawer for decision/facts/node-link/experience/service/domain/detail/timeline surfaces (`web/src/pages/VPSDetailPage.tsx:966-975`).
- Hero carries identity, lifecycle/usage/renewal badges, node count, primary decision action, ellipsis menu, back/list links (`web/src/pages/vps-detail/VPSDetailHero.tsx:38-80`).
- Decision workbench displays next action, evidence statuses, current decision, subscription/cost, Node evidence, context quality, key metrics, and access summary (`web/src/pages/vps-detail/VPSDecisionWorkbench.tsx:86-275`).
- Operations summary repeats/condenses subscription, Node, service/domain, recent history, and facts with buttons opening drawers (`web/src/pages/vps-detail/VPSOperationsSummary.tsx:103-228`).

#### Likely IA problems

- **Workbench and operations summary overlap.** Both `VPSDecisionWorkbench` and `VPSOperationsSummary` show subscription/renewal, Node evidence, services/domains, and timeline/history concepts (`VPSDecisionWorkbench.tsx:134-268`; `VPSOperationsSummary.tsx:131-227`). This gives the page a strong decision-first posture, but there is some repeated evidence density.
- **Lifecycle danger zone is not a persistent bottom section.** Spec calls lifecycle archive/restore an independent danger zone (`docs/design/v2-houfeng/component-spec.md:240`). Current implementation exposes archive/restore inside the hero ellipsis (`web/src/pages/vps-detail/VPSDetailHero.tsx:54-77`) and only renders `VPSLifecycleCard` after the user starts the action (`web/src/pages/VPSDetailPage.tsx:953-964`). This is clean, but the danger-zone affordance is less discoverable than the spec wording suggests.
- **Spec sequence is more explicit than implementation.** The active spec lists identity hero → asset judgment → renewal/cost evidence → decision/experience → Node evidence → facts → service/domain → timeline → access → lifecycle (`docs/design/v2-houfeng/component-spec.md:235-241`). Current implementation intentionally compresses these into workbench + operations summary + drawers. That seems like the recent refactor direction, but the spec should be considered stale or needs an update if this is accepted.

#### Whether VPS needs further fixes

**Minor only.** The current VPS detail page already implements the decision-workbench IA requested by the spec: asset judgment, subscription error handling, Node health evidence, services/domains, quality gaps, and drawer-based editing are present (`web/src/pages/vps-detail/VPSDecisionWorkbench.tsx:64-83`, `web/src/pages/VPSDetailPage.tsx:729-877`). The main remaining issue is deduplication/weighting, not structural absence.

#### Quick wins

1. Reduce duplicate subscription/Node/service-domain language between the workbench and operations summary.
2. Clarify lifecycle archive/restore visibility: either keep it menu-only as a deliberate pattern or add a low-weight danger entry near the bottom.
3. If the compressed workbench+summary pattern is accepted, update the active component spec so future work does not re-expand it into many same-weight sections.

#### Risk

- **Implementation risk: medium.** The page has many drawer modes and state-reset requirements (`web/src/pages/VPSDetailPage.tsx:371-428`, `web/src/pages/VPSDetailPage.tsx:729-877`). Small copy/order changes are low risk; moving forms out of drawers is high risk and should be avoided.
- **Product risk: low.** The recent VPS refactor appears mostly aligned with the decision-first objective.

#### Recommended MVP priority

**P2.** Only dedupe and spec-align after Target and Node surgical fixes.

---

### 4. NodeOnboardingPage

#### Page purpose

Node onboarding is the specialized agent installation and binding workbench for one Node. It prioritizes one-command install, binding state, conflict handling, and safe fallback manual instructions.

#### Current structure

- Derives a four-step phase model from binding status and accepted observation (`web/src/pages/NodeOnboardingPage.tsx:141-181`).
- Install command panel warns about token secrecy, generates/re-generates backend-issued command, supports reveal/hide/copy, and displays issue metadata (`web/src/pages/NodeOnboardingPage.tsx:210-339`).
- Main render order:
  1. hero identity/status (`web/src/pages/NodeOnboardingPage.tsx:519-569`)
  2. stepper progress (`web/src/pages/NodeOnboardingPage.tsx:571-580`)
  3. two summary cards for host sample / accepted observation (`web/src/pages/NodeOnboardingPage.tsx:582-593`)
  4. conditional binding conflict handling (`web/src/pages/NodeOnboardingPage.tsx:595-692`)
  5. one-command install (`web/src/pages/NodeOnboardingPage.tsx:694-715`)
  6. installer behavior checklist (`web/src/pages/NodeOnboardingPage.tsx:717-725`)
  7. manual fallback (`web/src/pages/NodeOnboardingPage.tsx:727-748`)
  8. snapshot meta (`web/src/pages/NodeOnboardingPage.tsx:750-754`).
- Binding conflict uses two-step confirmation cards and copy that explains token consumption (`web/src/pages/NodeOnboardingPage.tsx:74-102`, `web/src/pages/NodeOnboardingPage.tsx:670-690`).
- Tests assert one-command install is primary and does not synthesize from browser origin (`web/src/pages/NodeOnboardingPage.test.tsx:62-90`, `web/src/pages/NodeOnboardingPage.test.tsx:151-193`, `web/src/pages/NodeOnboardingPage.test.tsx:195-218`).

#### Likely IA problems

- **Manual fallback has high page weight.** The manual fallback is a full `DetailSection` always present after the installer checklist (`web/src/pages/NodeOnboardingPage.tsx:727-748`). It is correct as troubleshooting content, but it can visually compete with the primary one-command path.
- **Summary cards duplicate stepper semantics.** The "首批样本 / 已接收观测" cards (`web/src/pages/NodeOnboardingPage.tsx:582-593`) repeat parts of the phase state. They help diagnostics, but are lower value than the install/conflict work.
- **Binding conflict appears after summary cards.** The component spec says binding conflict is conditional highest priority (`docs/design/v2-houfeng/component-spec.md:327-330`). Current render puts progress + summary before conflict (`web/src/pages/NodeOnboardingPage.tsx:571-596`). The conflict still appears above install, but not directly after hero.

#### Quick wins

1. If `binding_status === '指纹变更待确认'`, move conflict immediately after hero or progress.
2. Collapse manual fallback under a lower-weight `<details>` or make it visually secondary while preserving copy and snippets.
3. Keep install command generation exactly backend-issued; tests already guard this behavior.

#### Risk

- **Implementation risk: low-medium.** The page is self-contained and well-covered by tests for command generation, hiding/reveal, missing config, and secret handling. Reordering sections is low risk; changing command/security behavior is high risk.
- **Product risk: low.** The page is already close to the documented onboarding IA.

#### Recommended MVP priority

**P1/P2.** Do conflict ordering or fallback de-emphasis when touching onboarding, but it is not the biggest remaining IA issue.

---

### 5. NodeComparePage

#### Page purpose

Node compare is a specialized A/B page for comparing two selected Nodes’ identity and host metric charts.

#### Current structure

- Reads selected IDs from repeated `?id=` query parameters (`web/src/pages/NodeComparePage.tsx:65-70`).
- Each side uses `useNodeData` to fetch `getNode` and `getNodeRuntimeFacts` (`web/src/pages/NodeComparePage.tsx:23-63`).
- If fewer than two IDs are present, it renders a `PageState` empty state and link back to nodes (`web/src/pages/NodeComparePage.tsx:74-83`).
- Main page shows heading, return link, two identity cards, then two metric columns using `NodeWatchtowerMetrics` (`web/src/pages/NodeComparePage.tsx:87-125`).
- Tests cover empty state, A/B identity composition, metric placeholder, and one-side load error (`web/src/pages/NodeComparePage.test.tsx:95-147`).

#### Likely IA problems

- **Metric card order may differ between the two columns.** `NodeWatchtowerMetrics` sorts cards by each node’s threshold priority (`web/src/components/node-detail/NodeWatchtowerMetrics.tsx:394-418`). In an A/B comparison, this can put different metrics in the same vertical position on each side, making visual comparison harder.
- **No comparison-specific deltas or common axis explanation.** The page reuses full Node metric panels twice (`web/src/pages/NodeComparePage.tsx:101-124`). This is simple and consistent, but it is not yet an A/B comparison model beyond side-by-side display.
- **No time-window control on compare page.** It fetches runtime facts using default `getNodeRuntimeFacts(nodeId)` via `useNodeData` (`web/src/pages/NodeComparePage.tsx:36-39`), unlike Node/Target detail pages that expose time-window tabs.
- **Narrow route entry conditions.** The page only explains the less-than-two case (`web/src/pages/NodeComparePage.tsx:74-83`). Extra IDs are silently ignored (`web/src/pages/NodeComparePage.tsx:67-70`).

#### Quick wins

1. For compare mode, keep metric order fixed rather than priority-sorted per node.
2. Add a small explanatory line that the charts are independent latest 24h host metrics if no shared comparison math is added.
3. Consider exposing the same time-window tabs used by Node detail if comparison is an MVP workflow.

#### Risk

- **Implementation risk: low.** The page is small and reuses existing components.
- **Product risk: low-medium.** Compare is specialized; if used for real diagnosis, misaligned metric order is the main issue.

#### Recommended MVP priority

**P2/P3.** Useful quick improvement, but less important than Target detail.

---

### 6. SettingsPage

#### Page purpose

Settings is the global control page for theme, notification channels, host/probe frequencies, incident defaults, override rules, and retention policy.

#### Current structure

- Builds a large form state from server settings (`web/src/pages/SettingsPage.tsx:47-94`).
- Builds update payload with validation for Telegram, Feishu, frequencies, incident defaults, override JSON arrays, and retention policy (`web/src/pages/SettingsPage.tsx:134-303`).
- Uses top-level tabs: `general`, `notifications`, `advanced` (`web/src/pages/SettingsPage.tsx:305-306`).
- Tracks notification channel modal state, channel draft, active channels, expanded channels (`web/src/pages/SettingsPage.tsx:317-341`).
- Main render order is hero + tabs (`web/src/pages/SettingsPage.tsx:473-493`), tab panes (`web/src/pages/SettingsPage.tsx:495-579`), save error/success (`web/src/pages/SettingsPage.tsx:581-582`), global save button (`web/src/pages/SettingsPage.tsx:584-588`), and channel modal (`web/src/pages/SettingsPage.tsx:590-687`).
- General tab contains theme and frequency defaults (`web/src/pages/SettingsPage.tsx:495-510`).
- Notifications tab conditionally shows active Telegram/Feishu sections, channel manager, and incident defaults (`web/src/pages/SettingsPage.tsx:512-562`).
- Advanced tab shows override rules and retention policy (`web/src/pages/SettingsPage.tsx:564-579`).
- Tests cover truthful settings copy and tab content (`web/src/pages/SettingsPage.test.tsx:106-167`).

#### Likely IA problems

- **Settings spec and implementation differ materially.** The active component spec lists a linear sequence of sections: hero, theme, Telegram, frequency, global defaults, override rules, retention, bottom save (`docs/design/v2-houfeng/component-spec.md:291-299`). Current implementation adds top-level tabs and a notification-channel manager modal (`web/src/pages/SettingsPage.tsx:473-687`). This may be a valid density improvement, but the spec no longer describes the page accurately.
- **Global save feedback is far from tab-local edits.** Save error/success appears near the bottom after all tab panes (`web/src/pages/SettingsPage.tsx:581-588`). On long tabs, feedback may be separated from the changed controls.
- **Notification channels can be hidden by active-channel state.** Telegram/Feishu sections render only if `activeChannels.has(...)` (`web/src/pages/SettingsPage.tsx:513-540`). The initialization intentionally preserves prior `activeChannels` once non-empty (`web/src/pages/SettingsPage.tsx:323-341`). This keeps the UI clean but can make the page feel like channels are separate "objects" rather than normal settings.
- **Inline styles are a documented styling/maintainability gap.** Several settings sections use `style={{ ... }}` for layout (`web/src/pages/SettingsPage.tsx:481`, `web/src/pages/SettingsPage.tsx:495-512`, `web/src/pages/settings/TelegramSettingsSection.tsx:23`, `web/src/pages/settings/TelegramSettingsSection.tsx:39`, `web/src/pages/settings/FeishuSettingsSection.tsx:18`). The styling spec explicitly says inline layout/spacing is a known SettingsPage gap (`.trellis/spec/web/styling-guidelines.md:115-125`, `.trellis/spec/web/styling-guidelines.md:155-160`). This is style rather than pure IA, but it affects future IA work.

#### Quick wins

1. Decide whether the tabbed Settings IA is the new authority; if yes, update the component spec.
2. Keep save feedback visible near the save button and/or current tab context.
3. Clarify channel manager states: configured, enabled, disabled, and not yet added.
4. Avoid expanding Settings into more same-weight sections; the page already covers many global controls.

#### Risk

- **Implementation risk: medium.** Settings payload validation touches notification delivery, incident defaults, override rules, and retention (`web/src/pages/SettingsPage.tsx:134-303`). IA changes that preserve section components are safer than payload/state rewrites.
- **Product risk: medium.** Misunderstanding settings scope can affect notifications and incident thresholds.

#### Recommended MVP priority

**P1/P2.** Worth aligning after Target detail. Do not let Settings absorb more controls without clearer grouping.

---

### 7. LoginPage

#### Page purpose

Login is the public auth gate. It collects username/password, calls `useAuth().login`, then redirects to `next` or `/`.

#### Current structure

- Local state: username, password, submitting, error (`web/src/pages/LoginPage.tsx:7-14`).
- Submit clears error, calls login, navigates to `next` query or `/`, and shows generic credential error on failure (`web/src/pages/LoginPage.tsx:16-29`).
- Render: full-screen login page, seal, brand card, error alert, username/password inputs, primary login button, hard-coded footer version (`web/src/pages/LoginPage.tsx:31-65`).
- CSS is a documented page-local exception (`.trellis/spec/web/styling-guidelines.md:13-16`, `.trellis/spec/web/styling-guidelines.md:155-160`).
- Spec describes the same simple structure: full-screen centered, seal/aurora, brand/motto, username/password/login, role alert error (`docs/design/v2-houfeng/component-spec.md:339-344`).

#### Likely IA problems

- **Hard-coded footer version may be stale.** The page renders `v1.0` (`web/src/pages/LoginPage.tsx:63`) while the repository recent commit history indicates current release work around `0.4.3`. This is more truthfulness/metadata than IA, but it appears on a high-visibility public screen.
- **No recovery/help path.** For a single-operator product, this may be acceptable, but the page has only generic invalid-credentials feedback (`web/src/pages/LoginPage.tsx:24-25`).

#### Quick wins

1. Verify whether the login footer version should be shown at all or derived from actual app/build metadata.
2. Keep the page minimal; it matches the spec and should not become a settings/help surface.

#### Risk

- **Implementation risk: low.** Small page with tests.
- **Product risk: low.** Login IA is adequate for MVP.

#### Recommended MVP priority

**P3 / no IA refactor needed.** Only fix version truthfulness if in scope.

## Cross-page code patterns

### Recent Node/VPS refactor pattern

Node and VPS detail pages now both follow a higher-level "workbench" posture rather than old linear CRUD details:

- Node: watchtower header + current problem + metrics + drawers/history/actions (`web/src/pages/node-detail/NodeDetailPageBody.tsx:157-273`).
- VPS: identity hero + decision workbench + evidence summary + drawers (`web/src/pages/VPSDetailPage.tsx:879-975`).

Target detail partially follows this through `TargetWatchtowerHeader` and watchtower latency cards, but still has a split/collapsed ProbeItem management model that does not yet feel as settled (`web/src/pages/target-detail/TargetDetailPageBody.tsx:172-291`).

### Drawer / modal pattern

- VPS detail uses a single `Drawer` with many mode-specific contents (`web/src/pages/VPSDetailPage.tsx:714-877`, `web/src/pages/VPSDetailPage.tsx:966-975`).
- Node/Target detail use drawers for history, and Node also uses command drawer (`web/src/pages/node-detail/NodeDetailPageBody.tsx:249-272`; `web/src/pages/target-detail/TargetDetailPageBody.tsx:278-290`).
- Settings uses `Modal` for adding/configuring notification channels (`web/src/pages/SettingsPage.tsx:590-687`).
- Component conventions require drawer close/cancel to clear draft/error state (`.trellis/spec/web/component-conventions.md:48-50`). VPS appears designed around this; Settings channel modal has separate `channelDraft` and close handlers (`web/src/pages/SettingsPage.tsx:404-435`).

### PageState / loading/error consistency

- NodeCompare uses `PageState` for empty/error cases (`web/src/pages/NodeComparePage.tsx:74-83`, `web/src/pages/NodeComparePage.tsx:129-154`, `web/src/pages/NodeComparePage.tsx:199-222`).
- Settings uses `PageState` for loading/error (`web/src/pages/SettingsPage.tsx:377-390`).
- Target and Node detail have private loading/unavailable components (`web/src/pages/TargetDetailPage.tsx:808-814`, `web/src/pages/NodeDetailPage.tsx:678-684`). This is acceptable, but the component convention says route/detail/list loading/error/empty should prefer `PageState` (`.trellis/spec/web/component-conventions.md:44`).

## Recommended MVP priority order

| Priority | Page | Rationale |
|---|---|---|
| P0 | `TargetDetailPage` | Biggest remaining mismatch: Target’s core probe/evidence IA is less settled than Node/VPS and diverges from active spec. |
| P1 | `NodeDetailPage` | Recent refactor mostly works; fix binding conflict priority/order and decide secondary sections. |
| P1/P2 | `SettingsPage` | Important global controls; implementation has a new tab/channel IA not reflected in spec. |
| P1/P2 | `NodeOnboardingPage` | Mostly aligned; conflict order and manual fallback weight are the main IA issues. |
| P2 | `VPSDetailPage` | Recent refactor is strong; minor duplication and lifecycle visibility decisions remain. |
| P2/P3 | `NodeComparePage` | Small specialized page; fixed metric order is the main IA quick win. |
| P3 | `LoginPage` | Matches spec; only version/help truthfulness checks. |

## Related Specs

- `.trellis/spec/web/component-conventions.md` — page/component layering, PageState, drawer state reset, selector patterns, known oversized page gaps.
- `.trellis/spec/web/styling-guidelines.md` — dark-first styling, `DetailSection`/PageState expectations, Settings inline-style known gap.
- `.trellis/spec/web/directory-structure.md` — page/component/lib style placement.
- `docs/design/v2-houfeng/component-spec.md` — active page-level visual/IA contracts for VPSDetail, NodeDetail, TargetDetail, NodeOnboarding, Settings, Login.
- `docs/design/v2-houfeng/design-language.md` — hierarchy, density, states, empty/loading/error rules.

## External References

None. This audit is based only on current repository code and active in-repo specs/docs.

## Caveats / Not Found

- No external UI/IA references were searched because the task is repository-specific and asked to use recent in-repo Node/VPS refactors as reference.
- This is a static code/spec audit. It did not run the app or inspect screenshots, so visual weight observations are inferred from component structure and documented CSS classes.
- The active `docs/design/v2-houfeng/component-spec.md` appears stale relative to the recent Node/VPS detail refactors in a few places. Where code and spec differ, this report calls out the mismatch rather than assuming either one is authoritative.

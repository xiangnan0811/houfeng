# Research: Frontend linkage UX audit

- **Query**: Research frontend integration/linkage UX gaps in this Houfeng repo for task `.trellis/tasks/05-18-fix-integration-linkage-ux`. Focus on React pages/forms around VPS assets, subscriptions, providers, nodes, node linking, renewal decisions, and empty-state/create flows. Identify where users must copy IDs, leave the flow to create prerequisites, or where cross-page business facts are displayed/edited independently without guidance.
- **Scope**: internal
- **Date**: 2026-05-18

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/VPSPage.tsx` | VPS inventory page, provider-backed create drawer, derived subscription/Node quality signals, empty state. |
| `web/src/pages/VPSDetailPage.tsx` | VPS detail coordinator for renewal decision, facts edit, Node link/unlink, service/domain create, lifecycle archive/restore. |
| `web/src/pages/vps-detail/VPSDecisionWorkbench.tsx` | Single-VPS decision workbench with next-action links/actions for subscription, Node, facts, and experience evidence. |
| `web/src/pages/vps-detail/vpsDecisionModel.ts` | Decision model that chooses next action; missing subscription routes to `/subscriptions`, missing Node opens Node-link form. |
| `web/src/pages/vps-detail/VPSNodeLinkForm.tsx` | VPS-to-Node link drawer form; raw `Node ID` input. |
| `web/src/pages/vps-detail/VPSNodeLinksSection.tsx` | VPS detail linked Node evidence table and unlink action. |
| `web/src/pages/vps-detail/VPSFactsEditForm.tsx` | VPS facts edit drawer; raw `Provider ID` input plus provider name snapshot. |
| `web/src/pages/vps-detail/VPSServicesForm.tsx` | VPS service create drawer; raw `Target ID` input. |
| `web/src/pages/vps-detail/VPSDomainsForm.tsx` | VPS domain create drawer; raw `Service ID` and `Target ID` inputs. |
| `web/src/pages/vps-detail/VPSServicesSection.tsx` | VPS service table; shows `service_id`, optional Target link, and empty state. |
| `web/src/pages/vps-detail/VPSDomainsSection.tsx` | VPS domain table; shows optional Service ID text and Target link. |
| `web/src/pages/vps-detail/VPSRenewalEvidenceSection.tsx` | VPS scoped subscription evidence section; links to subscription list. |
| `web/src/pages/vps-detail/VPSLifecycleCard.tsx` | VPS archive/restore card; lifecycle operations preserve subscriptions and Node links. |
| `web/src/pages/SubscriptionsPage.tsx` | Subscriptions list/create/edit page; create/edit selects existing VPS and stores `vps_id`. |
| `web/src/pages/ProvidersPage.tsx` | Provider master list/create/edit page; explicit note that it does not sync Node provider hint. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset decision queue and renewal evidence table; drawer edits only VPS renewal decision/reason. |
| `web/src/components/AssetDecisionRenewalTable.tsx` | Renewal candidate table; displays raw `vps_id` and `subscription_id`. |
| `web/src/components/AssetDecisionWorkPanel.tsx` | Asset decision drawer/panel; edits only renewal decision/reason for selected VPS. |
| `web/src/pages/NodesPage.tsx` | Node list/create page; create flow jumps to onboarding, no VPS association step. |
| `web/src/pages/nodes/CreateNodeDrawer.tsx` | Node create drawer; creates observability Node before onboarding. |
| `web/src/pages/node-detail/NodeLinkedVPSSection.tsx` | Node detail linked VPS table; one-way links to VPS detail but no link/create association action. |
| `web/src/pages/NodeOnboardingPage.tsx` | Node onboarding command page after Node creation. |
| `web/src/pages/TargetsPage.tsx` | Target list/create page; Target is prerequisite for service/domain `target_id` links. |
| `web/src/pages/targets/CreateTargetPanel.tsx` | Target create form; creates target separately from VPS service/domain forms. |
| `web/src/app/router.tsx` | Registered routes; asset pages have list/detail routes only for VPS, list-only routes for providers/subscriptions. |
| `web/src/lib/api.ts` | API helper surface for providers, VPS, links, services/domains, subscriptions. |
| `web/src/lib/types.ts` | Frontend types for Provider, VPS, VPSNodeLink, AssetService/Domain, Subscription contracts. |
| `.trellis/spec/web/state-and-data.md` | Relevant data-flow contracts for Asset Ledger list/decision, service/domain, VPS detail workbench. |
| `.trellis/spec/web/component-conventions.md` | Relevant component/routing constraints, including drawer state reset and route availability. |

### Observed linkage UX gaps

#### 1. Subscription creation depends on an existing VPS and leaves the flow when the prerequisite is missing

- `SubscriptionsPage` loads subscriptions and VPS assets together via `Promise.all([listSubscriptions(...), listVPSAssets()])` at `web/src/pages/SubscriptionsPage.tsx:197-204`.
- Create/edit payloads require `vps_id`; local validation throws `VPS 不能为空。` when `form.vpsID` is empty at `web/src/pages/SubscriptionsPage.tsx:120-142`.
- The create and edit forms expose a `<select>` whose only creation-side option is `选择 VPS`; options come from existing VPS rows at `web/src/pages/SubscriptionsPage.tsx:414-421` and `web/src/pages/SubscriptionsPage.tsx:460-467`.
- When the subscriptions table is empty, the in-table empty content is only `暂无订阅` at `web/src/pages/SubscriptionsPage.tsx:550-556`; the page-level related action sends users to `/vps` for asset context at `web/src/pages/SubscriptionsPage.tsx:560-568`.
- On VPS detail, missing subscription next action links to `/subscriptions` without carrying the current `vps_id` (`web/src/pages/vps-detail/vpsDecisionModel.ts:225-233`), and the renewal evidence section has a generic `订阅列表` link (`web/src/pages/vps-detail/VPSRenewalEvidenceSection.tsx:41-43`). Users then choose the VPS again in `SubscriptionsPage`.

#### 2. Provider master data, VPS provider fields, and Node provider hints are edited independently

- `ProvidersPage` states the boundary directly: provider master data records panel/account/country/tags and `不会同步修改 Node 的 provider hint` at `web/src/pages/ProvidersPage.tsx:269-275`.
- Provider rows display `provider.provider_id` under the provider name (`web/src/pages/ProvidersPage.tsx:209-218`), but `app/router.tsx` registers only `/providers`, not `/providers/:id` (`web/src/app/router.tsx:72-79`).
- `VPSPage` loads provider rows for the create drawer (`web/src/pages/VPSPage.tsx:415-427`). The drawer lets users select an existing `provider_id` or leave `未关联服务商`, and separately edit a `服务商名称快照` string (`web/src/pages/VPSPage.tsx:798-810`).
- The VPS detail facts drawer uses a raw `Provider ID` input and separate provider-name snapshot input at `web/src/pages/vps-detail/VPSFactsEditForm.tsx:28-32`; the update helper sends `provider_id: form.providerID.trim() || null` and `provider_name: form.providerName.trim()` independently (`web/src/pages/vps-detail/vpsDetailHelpers.ts:95-100`).
- Node create uses a required free-text `供应商` field (`web/src/pages/nodes/CreateNodeDrawer.tsx:77-84`); Node detail linked VPS rows show VPS provider facts separately (`web/src/pages/node-detail/NodeLinkedVPSSection.tsx:39-45`).

#### 3. VPS-to-Node linking requires a raw Node ID and is editable only from the VPS detail side

- `VPSNodeLinkForm` exposes a raw `Node ID` input with placeholder `nd_...` (`web/src/pages/vps-detail/VPSNodeLinkForm.tsx:41-50`).
- `VPSDetailPage` validates only non-empty Node ID (`Node ID 不能为空`) before calling `linkVPSNode(detail.vps_id, { node_id: nodeId, note })` at `web/src/pages/VPSDetailPage.tsx:433-454`.
- The VPS linked-Node section displays linked node name and `node_id` as plain text, plus an unlink button, but no `Link` to `/nodes/:node_id` in the Node column (`web/src/pages/vps-detail/VPSNodeLinksSection.tsx:23-33`, `web/src/pages/vps-detail/VPSNodeLinksSection.tsx:70-82`).
- Node detail has a reciprocal linked-VPS table that links to `/vps/:vps_id` (`web/src/pages/node-detail/NodeLinkedVPSSection.tsx:25-36`) but its empty state is only `尚未关联 VPS` / `关联 VPS 待同步` text (`web/src/pages/node-detail/NodeLinkedVPSSection.tsx:87-100`); no link action to open a VPS-linking flow exists in that section.
- Node creation (`NodesPage`) navigates to onboarding after creating the Node (`web/src/pages/NodesPage.tsx:275-284`), and the create drawer copy describes creating a server then generating install command (`web/src/pages/nodes/CreateNodeDrawer.tsx:31-36`); there is no VPS association field in the Node create form.

#### 4. VPS service/domain linkage uses raw Target ID and Service ID values

- `VPSServicesForm` exposes a raw `Target ID` input with placeholder `tg_...` (`web/src/pages/vps-detail/VPSServicesForm.tsx:103-111`). The helper maps `target_id: form.targetID.trim() || null` into the create payload (`web/src/pages/vps-detail/vpsDetailHelpers.ts:151-158`).
- `VPSServicesSection` shows the generated `service.service_id` under the service name (`web/src/pages/vps-detail/VPSServicesSection.tsx:25-33`) and links to Target detail only when `service.target_id` is already present (`web/src/pages/vps-detail/VPSServicesSection.tsx:61-70`).
- `VPSDomainsForm` exposes raw `Service ID` and `Target ID` inputs (`web/src/pages/vps-detail/VPSDomainsForm.tsx:73-90`). The helper maps them directly as `service_id` and `target_id` after trimming (`web/src/pages/vps-detail/vpsDetailHelpers.ts:180-186`).
- `VPSDomainsSection` displays `服务 ${domain.service_id}` as text when a domain is linked to a service, and links only Target IDs to `/targets/:target_id` (`web/src/pages/vps-detail/VPSDomainsSection.tsx:70-83`).
- Target creation is a separate `/targets` flow; `CreateTargetPanel` creates a Target and then enters Target detail for ProbeItem configuration (`web/src/pages/targets/CreateTargetPanel.tsx:30-35`, `web/src/pages/TargetsPage.tsx:159-166`).

#### 5. Renewal decisions, subscriptions, and VPS lifecycle are edited on separate surfaces

- `AssetDecisionsPage` decision drawer patches only `renewal_decision` and optional `renewal_reason` through `updateVPSAsset` (`web/src/pages/AssetDecisionsPage.tsx:472-489`).
- `VPSDetailPage` uses the same renewal-decision-only patch in the VPS detail drawer (`web/src/pages/VPSDetailPage.tsx:366-388`).
- `AssetDecisionWorkPanel` / `VPSRenewalDecisionForm` forms contain only renewal decision and reason inputs (`web/src/components/AssetDecisionWorkPanel.tsx:69-93`, `web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx:43-70`).
- Subscription auto-renew, cancellation flag, status, price, and renewal date are edited separately in `SubscriptionsPage` (`web/src/pages/SubscriptionsPage.tsx:140-153`, `web/src/pages/SubscriptionsPage.tsx:429-444`, `web/src/pages/SubscriptionsPage.tsx:475-490`).
- VPS lifecycle archive/restore is a separate danger-zone card. Its confirmation states that archive does not delete VPS, subscriptions, Node links, or asset history (`web/src/pages/vps-detail/VPSLifecycleCard.tsx:53-64`).
- VPS facts editing excludes lifecycle and renewal decision fields; `VPSFactsEditForm` edits provider/access/spec/usage/importance/labels/note (`web/src/pages/vps-detail/VPSFactsEditForm.tsx:28-62`), and `buildFactEditInput` sends no `lifecycle_status` or `renewal_decision` (`web/src/pages/vps-detail/vpsDetailHelpers.ts:95-116`).

#### 6. Renewal queue evidence includes raw IDs and indirect subscription navigation

- `AssetDecisionRenewalTable` renders the subscription row identity as raw `subscription.vps_id` and `subscription.subscription_id` (`web/src/components/AssetDecisionRenewalTable.tsx:22-31`).
- In `AssetDecisionsPage`, renewal-row actions link to the VPS detail and to `/subscriptions?renew_within_days=${renewalWindow}` (`web/src/pages/AssetDecisionsPage.tsx:627-636`). The subscription link preserves the window but does not include the row's `vps_id`.
- The main decision queue does join VPS rows with subscriptions by VPS ID in memory (`web/src/pages/AssetDecisionsPage.tsx:112-130`) and shows missing-subscription / linked-node counts, but the renewal evidence table itself does not join VPS display names.

#### 7. Empty-state and create-flow coverage is uneven across asset linkage prerequisites

| Surface | Empty/create behavior observed | Linkage implication |
|---|---|---|
| VPS inventory | Empty table content includes first-VPS CTA and copy stating users later add subscription and Node association (`web/src/pages/VPSPage.tsx:534-552`, `web/src/pages/VPSPage.tsx:790-793`, `web/src/pages/VPSPage.tsx:858-866`). | First asset creation is handled in-flow; prerequisites after creation move to detail/subscription/link flows. |
| Subscriptions | Top button opens create panel; table empty content is `暂无订阅` (`web/src/pages/SubscriptionsPage.tsx:403-405`, `web/src/pages/SubscriptionsPage.tsx:550-556`). | Creating requires selecting an existing VPS; no inline VPS creation in the subscription form. |
| Providers | Top button opens create panel; table empty content is `暂无服务商` (`web/src/pages/ProvidersPage.tsx:277-280`, `web/src/pages/ProvidersPage.tsx:369-375`). | VPS create can proceed with no provider association and a provider-name snapshot; provider master creation is separate. |
| VPS Node evidence | Empty table content is `尚未关联 Node`, with a section action `关联 Node` (`web/src/pages/vps-detail/VPSNodeLinksSection.tsx:87-100`, `web/src/pages/vps-detail/VPSNodeLinksSection.tsx:127-133`). | Link action exists, but requires a raw Node ID. |
| Node linked VPS | Empty text is `尚未关联 VPS` / `关联 VPS 待同步` (`web/src/pages/node-detail/NodeLinkedVPSSection.tsx:87-100`). | Reciprocal link management is not surfaced from Node detail. |
| VPS services | Empty content is `尚未记录服务`; create action opens service drawer (`web/src/pages/vps-detail/VPSServicesSection.tsx:84-110`). | Creating a service can optionally reference a Target only by raw Target ID. |
| VPS domains | Empty content is `尚未记录域名`; create action opens domain drawer (`web/src/pages/vps-detail/VPSDomainsSection.tsx:97-123`). | Creating a domain can optionally reference Service/Target only by raw IDs. |
| Targets | No-target page has first-target CTA (`web/src/pages/TargetsPage.tsx:629-640`). | Target creation is available as a separate page flow, not inside VPS service/domain link forms. |
| Nodes | Node create drawer creates Node and navigates to onboarding (`web/src/pages/NodesPage.tsx:275-284`, `web/src/pages/nodes/CreateNodeDrawer.tsx:31-36`). | Node creation/onboarding is separate from VPS linking. |

### Code Patterns

#### Raw ID entry points

| Raw ID | Entry point | Current behavior |
|---|---|---|
| `provider_id` | `web/src/pages/vps-detail/VPSFactsEditForm.tsx:29-31` | User edits Provider ID as text; helper stores it independently from provider-name snapshot (`web/src/pages/vps-detail/vpsDetailHelpers.ts:95-100`). |
| `node_id` | `web/src/pages/vps-detail/VPSNodeLinkForm.tsx:41-50` | User types Node ID; page checks only non-empty then posts link request (`web/src/pages/VPSDetailPage.tsx:433-454`). |
| `target_id` | `web/src/pages/vps-detail/VPSServicesForm.tsx:103-111` | User types Target ID; helper trims or sends null (`web/src/pages/vps-detail/vpsDetailHelpers.ts:151-158`). |
| `service_id` | `web/src/pages/vps-detail/VPSDomainsForm.tsx:73-81` | User types Service ID; helper trims or sends null (`web/src/pages/vps-detail/vpsDetailHelpers.ts:180-185`). |
| `target_id` | `web/src/pages/vps-detail/VPSDomainsForm.tsx:82-90` | User types Target ID; helper trims or sends null (`web/src/pages/vps-detail/vpsDetailHelpers.ts:180-186`). |

#### Cross-page fetch/join pattern

- Asset inventory performs a client-side join between VPS rows and subscriptions: `listVPSAssets()`, `listProviders()`, and `listSubscriptions({ sort: 'renew_at', order: 'asc' })` are fetched separately (`web/src/pages/VPSPage.tsx:415-471`), then joined in `buildInventoryRows` using `selectPrimarySubscription` (`web/src/pages/VPSPage.tsx:264-286`).
- Asset decisions fetch renewal-window subscriptions and three VPS slices separately (`unreviewed`, `migrate`, `cancel`), then join all subscriptions by VPS in memory (`web/src/pages/AssetDecisionsPage.tsx:324-392`, `web/src/pages/AssetDecisionsPage.tsx:394-405`).
- VPS detail fetches `getVPSAsset`, timeline, services, domains, and scoped subscriptions in parallel (`web/src/pages/VPSDetailPage.tsx:146-164`) and refreshes different subsets depending on the operation (`web/src/pages/VPSDetailPage.tsx:217-235`, `web/src/pages/VPSDetailPage.tsx:238-253`).

#### Route availability pattern

- `app/router.tsx` registers `/vps` and `/vps/:vpsId`, but providers and subscriptions are list-only routes (`/providers`, `/subscriptions`) with no detail route (`web/src/app/router.tsx:72-79`).
- Node and Target detail routes exist (`/nodes/:nodeId`, `/targets/:targetId`) and are used by some cross-links (`web/src/app/router.tsx:80-90`).

### External References

- None. This was an internal code/spec audit only.

### Related Specs

- `.trellis/spec/web/state-and-data.md` — Asset Ledger list/decision contracts define front-end joins, URL-state, and that `VPSAssetRecord.active_node_link_count` only represents count on list pages (`.trellis/spec/web/state-and-data.md:147-193`).
- `.trellis/spec/web/state-and-data.md` — Asset service/domain contracts define scoped VPS create/list behavior and Target links as jump-only links (`.trellis/spec/web/state-and-data.md:194-282`).
- `.trellis/spec/web/state-and-data.md` — VPS detail workbench contract permits detail Node health fields, requires drawer-based edits, and separates facts drawer from lifecycle status (`.trellis/spec/web/state-and-data.md:284-305`).
- `.trellis/spec/web/component-conventions.md` — App route/search guidance says search results only link to registered routes; providers/subscriptions have list/filter destinations rather than nonexistent detail routes (`.trellis/spec/web/component-conventions.md:51-56`).
- `.trellis/spec/web/component-conventions.md` — Drawer close/reset contract for create/edit forms (`.trellis/spec/web/component-conventions.md:48-50`).
- `.trellis/spec/guides/cross-layer-thinking-guide.md` — General guide for mapping data across source, transform, store, retrieve, transform, display boundaries (`.trellis/spec/guides/cross-layer-thinking-guide.md:20-31`).

## Caveats / Not Found

- No external/web documentation search was needed or performed.
- This audit did not run the SPA in a browser; findings are from static code/spec inspection.
- Backend handlers/store behavior was not audited beyond frontend API/type contracts.
- I did not modify production code or tests; only this research file was written.

# Research: Backend linkage audit

- **Query**: Research backend/data-model integration and linkage gaps in this Houfeng repo for task `.trellis/tasks/05-18-fix-integration-linkage-ux`. Focus on Go handlers/stores/models for VPS assets, subscriptions, providers, renewals/experiences, asset-node links, node onboarding, and whether renewal/autorenew decisions propagate across aggregates. Identify current invariants, APIs available to frontend, missing endpoints or missing transaction-level synchronization.
- **Scope**: internal
- **Date**: 2026-05-18

## Findings

### Files Found

| File Path | Description |
|---|---|
| `internal/center/providers/types.go` | Provider domain model, create/patch input, validation, repository interface. |
| `internal/center/store/providers.go` | Provider Postgres repository: list/get/create/patch only. |
| `internal/center/http/handlers/providers.go` | Provider HTTP handlers for `/api/providers` and `/api/providers/{provider_id}`. |
| `internal/center/vpsassets/types.go` | VPS asset domain model, lifecycle/usage/renewal enums, patch validation, archived-at derivation. |
| `internal/center/store/vps_assets.go` | VPS asset repository and transaction-backed history creation for renewal, IP, and spec changes. |
| `internal/center/http/handlers/vps.go` | VPS asset collection/item handlers plus timeline and experience-log handlers. |
| `internal/center/subscriptions/types.go` | Subscription domain model, date wrapper, list filters, auto-renew fields, validation. |
| `internal/center/store/subscriptions.go` | Subscription repository and transaction-backed price-history creation on billing/renewal/status changes. |
| `internal/center/http/handlers/subscriptions.go` | Subscription collection/item handlers and query-param filters. |
| `internal/center/renewals/types.go` | Renewal decision, price/IP/spec history, experience-log, and timeline DTOs. |
| `internal/center/store/renewal_decisions.go` | Timeline/history repository: renewal decisions, price histories, IP histories, spec snapshots, experience logs. |
| `internal/center/assetlinks/types.go` | VPS-node link domain model and read summaries for VPS detail and node detail. |
| `internal/center/store/vps_node_links.go` | VPS-node link repository: link/unlink/list/count. |
| `internal/center/http/handlers/asset_links.go` | Link/list handlers for `/api/vps/{id}/nodes`, `/api/vps/{id}/link-node`, `/api/vps/{id}/unlink-node`, `/api/nodes/{id}/vps`. |
| `internal/center/nodes/types.go` | Node lifecycle/binding/onboarding model and onboarding repository interface. |
| `internal/center/store/nodes.go` | Node repository: create/update, token issue/consume, onboarding state, binding transactions, runtime/lifecycle controls. |
| `internal/center/http/handlers/node_onboarding.go` | Onboarding, enrollment-token, install-command, and binding action handlers. |
| `internal/center/assetservices/types.go` | VPS-scoped service asset domain model. |
| `internal/center/store/asset_services.go` | Service asset repository: list/list-for-VPS/create. |
| `internal/center/http/handlers/asset_services.go` | Service asset handlers for `/api/services` and `/api/vps/{id}/services`. |
| `internal/center/assetdomains/types.go` | VPS-scoped domain asset domain model. |
| `internal/center/store/asset_domains.go` | Domain asset repository: list/list-for-VPS/create, including same-VPS service check. |
| `internal/center/http/handlers/asset_domains.go` | Domain asset handlers for `/api/domains` and `/api/vps/{id}/domains`. |
| `internal/center/http/router.go` | Router surface and subtree dispatch for all asset/node APIs. |
| `cmd/houfeng-center/bootstrap.go` | Production wiring for repositories and handlers. |
| `cmd/houfeng-import-vps-json/main.go` | CLI import transaction boundary and tx-backed asset-ledger repositories. |
| `internal/center/importing/importing.go` | Import dry-run/import analysis and write order. |
| `internal/center/importing/types.go` | Import input/report schema including node-association, renewal, and idle-paid candidates. |
| `internal/center/store/dashboard.go` | Dashboard read model that joins asset tables, subscriptions, links, and node health. |
| `internal/center/incidents/types.go` | Dashboard asset summary JSON contract. |
| `web/src/lib/api.ts` | Frontend API helpers currently available to UI. |
| `web/src/lib/types.ts` | Frontend DTOs and enum/label contracts for asset, subscription, timeline, link, and onboarding APIs. |
| `db/migrations/0016_create_asset_ledger.sql` | Providers table. |
| `db/migrations/0017_add_vps_assets.sql` | VPS assets table and enum/check constraints. |
| `db/migrations/0018_add_subscriptions.sql` | Subscriptions table and billing/status constraints. |
| `db/migrations/0019_create_vps_node_links.sql` | VPS-node link history table and active-pair uniqueness. |
| `db/migrations/0020_create_renewal_decisions.sql` | Renewal decision history table. |
| `db/migrations/0021_create_asset_histories.sql` | Price histories, IP histories, and VPS spec snapshots. |
| `db/migrations/0022_create_experience_logs.sql` | Experience logs table. |
| `db/migrations/0023_create_asset_services.sql` | VPS service assets table. |
| `db/migrations/0024_create_asset_domains.sql` | VPS domain assets table. |
| `.trellis/spec/backend/database-guidelines.md` | Current backend/data-model contracts for Asset Ledger, timeline histories, import, dashboard summary, and node onboarding token semantics. |
| `.trellis/spec/guides/cross-layer-thinking-guide.md` | Cross-layer data-flow checklist relevant to API/backend/frontend linkage. |
| `docs/operations/asset-ledger-real-data-validation-readiness.md` | Public workflow and boundaries for Asset Ledger sample/real-data validation. |

### Code Patterns

#### 1. Provider aggregate is standalone master data

- Provider domain fields are `provider_id`, `name`, URLs/account hints, country, note, optional rating, labels, timestamps (`internal/center/providers/types.go:16-28`). The repository interface exposes only list/get/create/patch (`internal/center/providers/types.go:67-72`).
- Store implementation writes only the `providers` table: `ListProviders` orders by lower(name), `GetProvider` reads one provider, `CreateProvider` inserts one provider, and `PatchProvider` updates provider fields (`internal/center/store/providers.go:67-202`).
- Handler surface is collection GET/POST and item GET/PATCH (`internal/center/http/handlers/providers.go:11-102`), wired to `/api/providers` and `/api/providers/` (`internal/center/http/router.go:111-116`).
- Data-model contract explicitly keeps Asset Ledger providers separate from Fleet Observability node metadata: provider CRUD must not backfill or normalize `nodes.provider` (`.trellis/spec/backend/database-guidelines.md:238-247`).

Observed linkage state:

- `vps_assets.provider_id` can reference `providers(provider_id)` with `on delete set null`, while `provider_name` remains a compatibility/display string (`db/migrations/0017_add_vps_assets.sql:1-6`).
- No code path found that propagates `providers.name` changes into existing `vps_assets.provider_name` or `nodes.provider`; this matches the spec boundary above.
- No DELETE endpoint for providers was found in router/handler/store; available operations are list/get/create/patch only.

#### 2. VPS asset current state and history are split deliberately

- VPS domain model defines lifecycle machine values `active`, `idle`, `testing`, `to_migrate`, `to_cancel`, `cancelled`, `archived`; usage machine values; and renewal decisions `unreviewed`, `keep`, `observe`, `migrate`, `cancel`, `auto_renew_cancelled`, `replaced` (`internal/center/vpsassets/types.go:16-48`).
- A VPS record includes provider identity/name, location/access facts, lifecycle/usage/renewal, importance, labels, note, derived `active_node_link_count`, timestamps, and `archived_at` (`internal/center/vpsassets/types.go:56-84`).
- Create input defaults `renewal_decision` to `unreviewed`, `importance` to `normal`, and `ssh_port` to 22 (`internal/center/vpsassets/types.go:313-345`). Create validation requires `display_name`, valid lifecycle/usage/renewal, and SSH port range (`internal/center/vpsassets/types.go:347-363`).
- Patch validation accepts optional fields and enforces that `renewal_reason` is only valid together with `renewal_decision` (`internal/center/vpsassets/types.go:366-420`).
- `archived_at` is derived from lifecycle state: when lifecycle is archived, keep existing archived time or set now; otherwise clear it (`internal/center/vpsassets/types.go:516-524`, `internal/center/store/vps_assets.go:431-435`).
- VPS create writes only `vps_assets`; initial `renewal_decision`, IP, and spec facts do not create history rows (`internal/center/store/vps_assets.go:175-270`).

Transaction/history behavior:

- `PatchVPSAsset` uses a transaction only when history-relevant fields are present: renewal decision, IP, product/access/spec fields (`internal/center/store/vps_assets.go:272-283`, `internal/center/store/vps_assets.go:385-395`).
- The history path locks the VPS row with `select ... for update`, patches the row, then inserts history rows in the same transaction as needed (`internal/center/store/vps_assets.go:298-383`).
- If `renewal_decision` changes, it inserts a `renewal_decisions` row with old and new decisions plus optional reason (`internal/center/store/vps_assets.go:332-345`).
- If IPv4/IPv6 changes, it inserts an IP history row (`internal/center/store/vps_assets.go:347-360`).
- If product/access/spec fields change, it inserts a spec snapshot row with the new values (`internal/center/store/vps_assets.go:362-377`).
- Non-history fields such as display name, provider, country/region/city, lifecycle, usage, importance, labels, and note are patched as one row update when no history trigger is present (`internal/center/store/vps_assets.go:406-484`). Lifecycle transitions do not have a dedicated VPS lifecycle history table in the current model.

HTTP/API behavior:

- `/api/vps` supports GET with provider/lifecycle/usage/renewal filters and POST create (`internal/center/http/handlers/vps.go:13-88`).
- `/api/vps/{vps_id}` supports GET and PATCH (`internal/center/http/handlers/vps.go:90-165`). If link repository is wired, GET returns an object embedding the VPS record plus `node_links`; PATCH returns the VPS record with refreshed `active_node_link_count` (`internal/center/http/handlers/vps.go:115-123`, `internal/center/http/handlers/vps.go:152-160`).
- Router dispatch includes VPS item, nodes, link-node, unlink-node, timeline, experience-logs, domains, and services subtrees (`internal/center/http/router.go:117-180`, `internal/center/http/router.go:348-396`).
- Frontend helpers expose `listVPSAssets`, `createVPSAsset`, `getVPSAsset`, and `updateVPSAsset` (`web/src/lib/api.ts:465-486`), with DTOs and enum labels in `web/src/lib/types.ts:580-780`.

Observed linkage state:

- Active link counts are not stored in `vps_assets`; handlers compute them through `assetlinks.Repository` (`internal/center/http/handlers/vps.go:42-51`, `internal/center/http/handlers/vps.go:75-82`, `internal/center/http/handlers/vps.go:152-159`).
- The spec says active node link count/node summary should be presentation-layer enrichment, not direct coupling inside `store/vps_assets.go` (`.trellis/spec/backend/database-guidelines.md:248-260`). The current code follows that boundary.
- No API found that atomically updates VPS current state together with a subscription row or node link row. VPS PATCH owns only `vps_assets` plus VPS history tables.

#### 3. Subscription aggregate records billing/autorenew facts independently from VPS decisions

- Subscription record includes `vps_id`, price/currency/billing, derived `monthly_price`, nullable dates, `auto_renew`, `auto_renew_cancelled`, status, payment method, note, timestamps (`internal/center/subscriptions/types.go:39-56`).
- Create and patch inputs include `auto_renew` and `auto_renew_cancelled` (`internal/center/subscriptions/types.go:58-86`).
- List filters include `vps_id`, status, renew-before/after/window, sort/order (`internal/center/subscriptions/types.go:88-96`).
- Dates serialize as `YYYY-MM-DD` through the `subscriptions.Date` wrapper (`internal/center/subscriptions/types.go:35-40`, `internal/center/subscriptions/types.go:135-167`).
- Create normalization defaults blank status to `active`; validation enforces VPS ID, non-negative 2-decimal-safe price, positive billing months, 3-letter uppercase currency, and valid status (`internal/center/subscriptions/types.go:278-308`).
- `monthly_price` is backend-derived via `CalculateMonthlyPrice(price, billingMonths)` rounded to four decimals (`internal/center/subscriptions/types.go:419-424`).

Transaction/history behavior:

- `CreateSubscription` inserts one row and calculates `monthly_price` before insert; it does not create an initial price history row (`internal/center/store/subscriptions.go:168-234`).
- `PatchSubscription` uses the history transaction path only when price, currency, billing cycle, billing months, renew date, auto-renew flags, or status are present in the patch (`internal/center/store/subscriptions.go:236-260`, `internal/center/store/subscriptions.go:367-376`).
- The history path locks the subscription row with `for update`, patches it, and inserts `price_histories` in the same transaction only if final tracked values changed (`internal/center/store/subscriptions.go:262-312`, `internal/center/store/subscriptions.go:378-388`).
- The price history record includes from/to values for price, currency, cycle, months, monthly price, renew date, `auto_renew`, `auto_renew_cancelled`, and status (`internal/center/renewals/types.go:38-62`, `internal/center/store/renewal_decisions.go:521-589`).

HTTP/API behavior:

- `/api/subscriptions` supports GET with `vps_id`, `status`, `renew_before`, `renew_after`, `renew_within_days`, `sort`, and `order`; POST creates (`internal/center/http/handlers/subscriptions.go:12-59`, `internal/center/http/handlers/subscriptions.go:115-150`).
- `/api/subscriptions/{subscription_id}` supports GET and PATCH (`internal/center/http/handlers/subscriptions.go:61-113`).
- Frontend exposes `listSubscriptions`, `createSubscription`, `getSubscription`, and `updateSubscription` (`web/src/lib/api.ts:555-579`). DTOs are in `web/src/lib/types.ts:1004-1048`.
- The subscriptions page loads subscriptions plus VPS assets and allows create/edit through the subscription endpoints (`web/src/pages/SubscriptionsPage.tsx:197-218`, `web/src/pages/SubscriptionsPage.tsx:242-319`).
- VPS detail loads subscriptions for its VPS for evidence display (`web/src/pages/VPSDetailPage.tsx:77-94`, `web/src/pages/VPSDetailPage.tsx:146-152`), but subscription editing is not implemented in VPS detail in the current frontend code.

Observed linkage state:

- No code path found where `subscriptions.auto_renew` or `subscriptions.auto_renew_cancelled` updates `vps_assets.renewal_decision`.
- No code path found where `vps_assets.renewal_decision = 'auto_renew_cancelled'` updates `subscriptions.auto_renew_cancelled`.
- The backend spec states subscription CRUD must not reverse-write VPS asset, provider, node, target, or agent state (`.trellis/spec/backend/database-guidelines.md:262-274`). It also states renewal decision history must not rewrite subscription (`.trellis/spec/backend/database-guidelines.md:287-300`). Current code matches this no-propagation boundary.
- Current synchronization exists only as independent history rows: VPS renewal decision changes create `renewal_decisions`; subscription auto-renew/status/renew-date changes create `price_histories`.
- No cross-aggregate transaction found that updates a VPS renewal decision and subscription auto-renew flags together.

#### 4. Renewal/timeline and experience-log APIs expose history read/write boundaries

- Renewal decision DTO includes `from_decision`, `to_decision`, reason, decision time, and created time (`internal/center/renewals/types.go:20-28`).
- Price history, IP history, spec snapshot, and experience log records are separate DTOs (`internal/center/renewals/types.go:38-160`).
- `VPSTimeline` returns `renewal_decisions`, `price_histories`, `ip_histories`, `spec_snapshots`, and `experience_logs` (`internal/center/renewals/types.go:154-160`).
- Timeline repository interface includes create/list for each history type and `GetVPSTimeline` (`internal/center/renewals/types.go:163-175`).
- `GetVPSTimeline` first checks that the VPS exists, then aggregates all five history arrays (`internal/center/store/renewal_decisions.go:431-478`).
- History create methods insert directly into their tables and map FK/check violations to timeline/asset-history sentinel errors (`internal/center/store/renewal_decisions.go:480-751`).

HTTP/API behavior:

- `GET /api/vps/{vps_id}/timeline` returns full `VPSTimeline` (`internal/center/http/handlers/vps.go:172-199`).
- `GET /api/vps/{vps_id}/experience-logs` lists experience logs and `POST /api/vps/{vps_id}/experience-logs` creates one (`internal/center/http/handlers/vps.go:201-257`).
- There is no direct HTTP handler found for `POST /api/vps/{vps_id}/renewal-decisions`, `POST /api/vps/{vps_id}/price-histories`, `POST /api/vps/{vps_id}/ip-histories`, or `POST /api/vps/{vps_id}/spec-snapshots`. Renewal/price/IP/spec histories are created indirectly by VPS/subscription patch flows.
- Frontend exposes `getVPSTimeline`, `listVPSExperienceLogs`, and `createVPSExperienceLog` (`web/src/lib/api.ts:488-498`), and types for all timeline arrays (`web/src/lib/types.ts:824-984`).
- VPS detail updates renewal decision by calling `updateVPSAsset` and then refreshes detail/timeline (`web/src/pages/VPSDetailPage.tsx:366-394`). It creates experience logs through `createVPSExperienceLog` and refreshes timeline (`web/src/pages/VPSDetailPage.tsx:530-557`).

Observed linkage state:

- Renewal decision history supplements `vps_assets.renewal_decision`; it is not the source of current decision. The spec states exactly this and requires PATCH-driven history inserts in the same transaction (`.trellis/spec/backend/database-guidelines.md:287-300`).
- Price histories supplement `subscriptions` current state; they are inserted by subscription PATCH in the same transaction (`.trellis/spec/backend/database-guidelines.md:302-310`).
- Experience logs are manual history notes and must not rewrite VPS current fields, subscription, node, target, agent, provider, or links (`.trellis/spec/backend/database-guidelines.md:312-322`). Current handler sets path `vps_id` as the write target before validation (`internal/center/http/handlers/vps.go:225-239`).
- No code path found that converts an experience log category/severity into a renewal decision, lifecycle state, subscription state, or node state.

#### 5. VPS-node links are a separate soft-link history table, not a Node state machine

- Link record has `link_id`, `vps_id`, `node_id`, `linked_at`, optional `unlinked_at`, and note (`internal/center/assetlinks/types.go:15-22`).
- VPS-side node summaries include node metadata/health and link timestamp/note (`internal/center/assetlinks/types.go:24-41`). Node-side VPS summaries include VPS asset metadata/renewal and link timestamp/note (`internal/center/assetlinks/types.go:43-59`).
- Repository interface exposes `LinkNode`, `UnlinkNode`, `ListNodesForVPS`, `ListVPSForNode`, and `CountActiveLinksForVPS` (`internal/center/assetlinks/types.go:71-77`).
- Migration creates `vps_node_links` with FKs to `vps_assets` and `nodes`; active links are `unlinked_at is null`; a partial unique index prevents duplicate active `(vps_id, node_id)` links (`db/migrations/0019_create_vps_node_links.sql:1-33`).
- `LinkNode` inserts one active row; FK/unique/check errors map to link sentinel errors (`internal/center/store/vps_node_links.go:58-90`, `internal/center/store/vps_node_links.go:250-263`).
- `UnlinkNode` sets `unlinked_at = now()` and optionally updates note; it does not delete the row (`internal/center/store/vps_node_links.go:93-118`).
- `ListNodesForVPS` joins active links to `nodes` (`internal/center/store/vps_node_links.go:120-178`). `ListVPSForNode` joins active links to `vps_assets` (`internal/center/store/vps_node_links.go:180-236`).
- `CountActiveLinksForVPS` derives active count from `vps_node_links`; no stored counter exists (`internal/center/store/vps_node_links.go:238-248`).

HTTP/API behavior:

- `GET /api/vps/{vps_id}/nodes` lists active linked nodes (`internal/center/http/handlers/asset_links.go:11-30`).
- `POST /api/vps/{vps_id}/link-node` creates an active link (`internal/center/http/handlers/asset_links.go:32-74`).
- `POST /api/vps/{vps_id}/unlink-node` soft-unlinks (`internal/center/http/handlers/asset_links.go:76-114`).
- `GET /api/nodes/{node_id}/vps` lists active linked VPS assets (`internal/center/http/handlers/asset_links.go:116-135`).
- Frontend exposes `listVPSNodes`, `linkVPSNode`, `unlinkVPSNode`, and `listVPSForNode` (`web/src/lib/api.ts:539-553`) plus link DTOs (`web/src/lib/types.ts:782-822`, `web/src/lib/types.ts:986-1002`).

Observed linkage state:

- Link/unlink does not update `nodes.lifecycle_status`, `nodes.monitoring_status`, `nodes.provider`, `nodes.current_health_status`, VPS lifecycle/usage/renewal, target rows, or subscriptions. This matches the spec's statement that links are association history and not Node state machine operations (`.trellis/spec/backend/database-guidelines.md:275-285`).
- No transaction was found that couples link/unlink with node onboarding, node lifecycle, VPS lifecycle, or subscription changes.
- No global link-list endpoint, link PATCH endpoint, or physical-delete endpoint was found. Available link writes are link and unlink only.
- Import can report node association candidates, but it does not create `vps_node_links`; see the import section below.

#### 6. Node onboarding is token/binding-state focused and currently separate from Asset Ledger links

- Node domain constants define lifecycle values (`待接入`, `在用`, `观察中`, `不续费`, `已退役`), monitoring values, binding values, and onboarding phases (`internal/center/nodes/types.go:10-31`).
- `Node = a specific server` and lifecycle/health semantics are core backend invariants (`.trellis/spec/backend/database-guidelines.md:657-668`).
- Onboarding state embeds `nodes.Record` and adds phase, host-sample/accepted-observation booleans, enrollment-token issue time, current fingerprint summary, and pending-binding metadata (`internal/center/nodes/types.go:140-148`).
- Onboarding repository interface exposes token issue, onboarding read, confirm rebind, reject pending fingerprint, and reset binding (`internal/center/nodes/types.go:160-166`).
- `DeriveOnboardingPhase` maps binding and observation state to one of the phase strings (`internal/center/nodes/types.go:173-185`).

Token and binding behavior:

- `IssueNodeEnrollmentToken` generates an `enroll_*` token, stores only a hash, sets `enrollment_token_issued_at`, clears `enrollment_token_consumed_at`, and returns issued/expires times (`internal/center/store/nodes.go:461-489`).
- `GetNodeOnboarding` reads node state plus existence checks for host samples/probe observations matching the current binding fingerprint and binding epoch (`internal/center/store/nodes.go:491-523`).
- `ApplyEnrollment` runs in one transaction: selects active unconsumed token `for update`, resolves binding transition, issues sync token if bound, updates binding fields, and marks `enrollment_token_consumed_at = now()` (`internal/center/store/nodes.go:1139-1242`).
- Confirm/reject/reset binding actions each run in a transaction and insert a state-change event before commit (`internal/center/store/nodes.go:585-743`).
- Spec requires active token lookup to check token hash, unconsumed state, and TTL, and requires successful validation to consume the token in the same transaction as binding changes (`.trellis/spec/backend/database-guidelines.md:108-152`).

HTTP/API behavior:

- `GET /api/nodes/{node_id}/onboarding` returns onboarding state (`internal/center/http/handlers/node_onboarding.go:13-38`).
- `POST /api/nodes/{node_id}/enrollment-token` returns a bare enrollment token issue (`internal/center/http/handlers/node_onboarding.go:40-64`). Router registers this subtree (`internal/center/http/router.go:239-244`).
- `POST /api/nodes/{node_id}/install-command` issues an enrollment token and returns a center-generated curl command using configured public base URL, agent version, installer URL, and release repo (`internal/center/http/handlers/node_onboarding.go:79-137`).
- Binding actions `confirm-rebind`, `reject-pending`, and `reset` return refreshed onboarding state (`internal/center/http/handlers/node_onboarding.go:139-189`).
- Frontend helpers expose onboarding read, install-command issue, and the three binding actions (`web/src/lib/api.ts:229-247`). No frontend helper was found for the backend-only `/api/nodes/{node_id}/enrollment-token` endpoint.
- Node onboarding page uses `issueNodeInstallCommand`, not `NodeEnrollmentToken`, and warns that install commands contain a 30-minute one-time token (`web/src/pages/NodeOnboardingPage.tsx:229-261`, `web/src/pages/NodeOnboardingPage.tsx:481-517`).

Observed linkage state:

- No code path found that auto-creates `vps_node_links` during node create, install-command generation, agent enrollment, binding confirmation, or reset.
- No code path found that uses VPS asset fields (`vps_assets.ssh_host`, IP, provider, renewal decision) to drive node onboarding.
- Node lifecycle controls (`/api/nodes/{id}/lifecycle/...`) are separate handlers and transactions (`internal/center/http/handlers/runtime_controls.go:78-119`, `internal/center/store/nodes.go:835-909`). They do not update Asset Ledger VPS lifecycle or subscriptions.

#### 7. VPS service/domain assets have create/list only and do not affect observability state

- Service asset model is VPS-scoped, with optional `target_id`, name, type, status, URL, port, labels, note (`internal/center/assetservices/types.go:36-61`). Repository interface exposes list, list-for-VPS, create (`internal/center/assetservices/types.go:70-74`).
- Service store validates missing VPS and missing Target through FK/error mapping; list-for-VPS first checks that the VPS exists (`internal/center/store/asset_services.go:124-137`, `internal/center/store/asset_services.go:139-235`).
- Service handlers expose collection GET/POST and `/api/vps/{vps_id}/services` GET/POST (`internal/center/http/handlers/asset_services.go:10-104`).
- Domain asset model is VPS-scoped, with optional `service_id` and `target_id`, domain facts, expiration, auto-renew, HTTPS flag, labels, note (`internal/center/assetdomains/types.go:31-62`). Repository interface exposes list, list-for-VPS, create (`internal/center/assetdomains/types.go:71-75`).
- Domain store checks that an optional `service_id` belongs to the same VPS before insert, maps FK/unique/check violations, and list-for-VPS checks VPS existence (`internal/center/store/asset_domains.go:133-220`, `internal/center/store/asset_domains.go:222-280`).
- Domain handlers expose collection GET/POST and `/api/vps/{vps_id}/domains` GET/POST (`internal/center/http/handlers/asset_domains.go:10-127`).
- Frontend exposes `listAssetServices`, `createAssetService`, `listVPSServices`, `createVPSService`, `listAssetDomains`, `createAssetDomain`, `listVPSDomains`, and `createVPSDomain` (`web/src/lib/api.ts:423-451`, `web/src/lib/api.ts:500-537`).
- Frontend types include service/domain records and create/list filters (`web/src/lib/types.ts:895-967`).

Observed linkage state:

- No PATCH/DELETE handlers or frontend helpers found for service assets or domain assets; current operations are list/create only.
- Optional `target_id` is a reference for association and display; current service/domain create paths do not create targets, update targets, update probe items, or alter agent behavior. This matches the spec contracts (`.trellis/spec/backend/database-guidelines.md:390-473`, `.trellis/spec/backend/database-guidelines.md:475-557`).
- Domain `auto_renew` is independent from subscription auto-renew and VPS renewal decision. Spec states domain `auto_renew` is a manual fact and does not trigger renewal decisions, certificate checks, or target probe changes (`.trellis/spec/backend/database-guidelines.md:475-485`).

#### 8. Import has a transaction boundary for provider → VPS → subscription, but not links/history propagation

- Import input accepts provider identity/name, VPS facts, lifecycle/usage/renewal decision, subscription facts including auto-renew flags, and optional node/target hints (`internal/center/importing/types.go:47-89`).
- Dry-run/import report includes provider candidates, VPS candidates, subscription candidates, missing provider/renew date rows, validation errors, duplicate candidates, node association candidates, renewal candidates, idle-paid candidates, and import results (`internal/center/importing/types.go:91-216`).
- Import analysis builds renewal-window candidates and idle-paid candidates from subscription and usage facts (`internal/center/importing/importing.go:442-475`).
- Node hints are only reported as manual confirmation candidates; matching is by node ID or node display name when database state is available (`internal/center/importing/importing.go:477-509`).
- `Import` creates missing providers, then VPS assets, then subscriptions (`internal/center/importing/importing.go:76-147`).
- CLI `runImport` opens Postgres, applies migrations, begins a transaction, constructs tx-backed provider/VPS/subscription repositories, calls importing.Import, then commits (`cmd/houfeng-import-vps-json/main.go:111-152`).
- The tx-backed repository constructor exists specifically for provider/VPS/subscription repository creation (`internal/center/store/asset_ledger.go:14-18`).

Observed linkage state:

- Import does not create `vps_node_links`; node inputs remain report candidates. The spec states import must not create links, alter `nodes.provider`, or change Node/Target/Agent semantics (`.trellis/spec/backend/database-guidelines.md:559-568`).
- Import creates initial VPS/subscription current rows; it does not create initial renewal decision history, price history, IP history, spec snapshot, or experience log rows. Histories are currently change histories created by later PATCH flows.
- Import transaction covers provider/VPS/subscription writes, but it does not involve node/link/service/domain/timeline repositories.

#### 9. Dashboard is read-only linkage over asset, subscription, link, and Node health

- Dashboard asset summary JSON fields are renewal-due counts, unreviewed/to-cancel/to-migrate counts, unlinked and abnormal-linked VPS counts, and costs by currency (`internal/center/incidents/types.go:101-109`).
- `loadDashboardAssetSummary` defines active VPS as lifecycle not `cancelled` or `archived`, active links as `unlinked_at is null`, and renewal due as active subscriptions due within 30 days joined to active VPS (`internal/center/store/dashboard.go:411-431`).
- It counts unreviewed, to-cancel, to-migrate, unlinked, and abnormal-linked VPS via read-only queries; abnormal-linked joins active links to `nodes.current_health_status <> '正常'` (`internal/center/store/dashboard.go:432-463`).
- Cost by currency sums active subscription monthly prices and multiplies by 12 (`internal/center/store/dashboard.go:466-497`).
- Spec confirms this is a read model and must not modify Node, Target, Agent, VPS, subscription, or link records (`.trellis/spec/backend/database-guidelines.md:570-581`).

Observed linkage state:

- Dashboard is the only backend read model found that combines subscription due dates, VPS current state, active links, and Node health into a single response.
- No dashboard write-back or decision propagation code was found.

### APIs available to frontend

The frontend API client currently exposes these relevant backend APIs:

| Frontend helper | Backend endpoint | Notes |
|---|---|---|
| `listProviders` / `createProvider` | `GET/POST /api/providers` | Collection operations (`web/src/lib/api.ts:419-455`). |
| `getProvider` / `updateProvider` | `GET/PATCH /api/providers/{provider_id}` | No delete helper found (`web/src/lib/api.ts:457-463`). |
| `listVPSAssets` / `createVPSAsset` | `GET/POST /api/vps` | Filters include provider/lifecycle/usage/renewal (`web/src/lib/api.ts:465-478`). |
| `getVPSAsset` / `updateVPSAsset` | `GET/PATCH /api/vps/{vps_id}` | GET type includes `node_links`; PATCH returns `VPSAssetRecord` (`web/src/lib/api.ts:480-486`). |
| `getVPSTimeline` | `GET /api/vps/{vps_id}/timeline` | Reads all history arrays (`web/src/lib/api.ts:488-490`). |
| `listVPSExperienceLogs` / `createVPSExperienceLog` | `GET/POST /api/vps/{vps_id}/experience-logs` | Experience logs also appear in timeline (`web/src/lib/api.ts:492-498`). |
| `listVPSServices` / `createVPSService` | `GET/POST /api/vps/{vps_id}/services` | Path-scoped create strips `vps_id` from body (`web/src/lib/api.ts:500-516`). |
| `listVPSDomains` / `createVPSDomain` | `GET/POST /api/vps/{vps_id}/domains` | Path-scoped create strips `vps_id` from body (`web/src/lib/api.ts:518-537`). |
| `listVPSNodes` / `linkVPSNode` / `unlinkVPSNode` | `GET /api/vps/{vps_id}/nodes`, `POST /link-node`, `POST /unlink-node` | Active link operations only (`web/src/lib/api.ts:539-549`). |
| `listVPSForNode` | `GET /api/nodes/{node_id}/vps` | Node detail side read (`web/src/lib/api.ts:551-553`). |
| `listSubscriptions` / `createSubscription` | `GET/POST /api/subscriptions` | Filters include VPS/status/renew windows (`web/src/lib/api.ts:555-571`). |
| `getSubscription` / `updateSubscription` | `GET/PATCH /api/subscriptions/{subscription_id}` | No delete helper found (`web/src/lib/api.ts:573-579`). |
| `getNodeOnboarding` | `GET /api/nodes/{node_id}/onboarding` | Onboarding state (`web/src/lib/api.ts:229-231`). |
| `issueNodeInstallCommand` | `POST /api/nodes/{node_id}/install-command` | Center-generated command; frontend does not synthesize production install URL (`web/src/lib/api.ts:233-235`). |
| `confirmNodeRebind` / `rejectPendingNodeBinding` / `resetNodeBinding` | `POST /api/nodes/{node_id}/binding/...` | Binding conflict actions (`web/src/lib/api.ts:237-247`). |
| `listAssetServices` / `createAssetService` | `GET/POST /api/services` | Global service list/create (`web/src/lib/api.ts:423-436`). |
| `listAssetDomains` / `createAssetDomain` | `GET/POST /api/domains` | Global domain list/create (`web/src/lib/api.ts:438-451`). |

Backend endpoint present but no frontend helper found:

- `POST /api/nodes/{node_id}/enrollment-token` is implemented and routed (`internal/center/http/handlers/node_onboarding.go:40-64`, `internal/center/http/router.go:239-244`), but `web/src/lib/api.ts` exposes install-command generation rather than a bare enrollment-token helper (`web/src/lib/api.ts:229-247`).

### Linkage gaps / not-found results

These are observed absences in the current codebase, stated as current state rather than recommendations.

1. **No renewal/autorenew propagation across VPS and subscriptions**
   - VPS renewal decision changes create `renewal_decisions` history in the VPS patch transaction (`internal/center/store/vps_assets.go:332-345`).
   - Subscription auto-renew/status/renew-date changes create `price_histories` in the subscription patch transaction (`internal/center/store/subscriptions.go:262-312`).
   - No code path found that synchronizes `vps_assets.renewal_decision` with `subscriptions.auto_renew` or `subscriptions.auto_renew_cancelled` in either direction.
   - The spec currently defines this separation: subscription CRUD must not rewrite VPS state, and renewal history must not rewrite subscription (`.trellis/spec/backend/database-guidelines.md:262-274`, `.trellis/spec/backend/database-guidelines.md:287-300`).

2. **No cross-aggregate transaction for “asset decision + subscription change”**
   - VPS PATCH and subscription PATCH use separate repositories/endpoints/transactions (`internal/center/store/vps_assets.go:272-383`, `internal/center/store/subscriptions.go:236-312`).
   - No handler found that accepts both a VPS renewal decision and subscription auto-renew/status changes and commits them together.

3. **No automatic VPS-node link creation from onboarding or import**
   - Link/unlink are explicit `/api/vps/{id}/link-node` and `/unlink-node` operations (`internal/center/http/handlers/asset_links.go:32-114`).
   - Node create/onboarding/enrollment/binding code has no call into the link repository (`internal/center/store/nodes.go:461-523`, `internal/center/store/nodes.go:1139-1242`).
   - Import reports node association candidates but does not write links (`internal/center/importing/importing.go:477-509`, `.trellis/spec/backend/database-guidelines.md:559-568`).

4. **No provider propagation into VPS provider_name or node provider**
   - Provider patch updates only `providers` (`internal/center/store/providers.go:155-202`).
   - `vps_assets.provider_id` and `provider_name` are fields on VPS assets, but no provider patch path updates them.
   - The spec states provider CRUD must not backfill `nodes.provider`; VPS asset spec also separates Asset Ledger provider from Fleet Observability node provider (`.trellis/spec/backend/database-guidelines.md:238-260`).

5. **No direct history creation endpoints except experience logs**
   - Direct HTTP create exists for experience logs (`internal/center/http/handlers/vps.go:201-257`).
   - Renewal decision history is created only via VPS PATCH; price history only via subscription PATCH; IP/spec histories only via VPS PATCH (`internal/center/store/vps_assets.go:332-377`, `internal/center/store/subscriptions.go:296-305`).
   - No direct HTTP endpoints found for creating renewal decision, price history, IP history, or spec snapshot records.

6. **No update/delete endpoints for service/domain assets**
   - Service/domain repositories expose only list/list-for-VPS/create (`internal/center/assetservices/types.go:70-74`, `internal/center/assetdomains/types.go:71-75`).
   - Handlers expose only GET/POST for collection and path-scoped routes (`internal/center/http/handlers/asset_services.go:10-104`, `internal/center/http/handlers/asset_domains.go:10-127`).
   - Frontend helpers mirror create/list only (`web/src/lib/api.ts:423-451`, `web/src/lib/api.ts:500-537`).

7. **No physical delete endpoints for primary asset aggregates**
   - Providers, VPS assets, subscriptions, links, services, and domains expose create/read/update subsets as described above; no DELETE routes found in relevant handlers/router.
   - VPS-node unlink is a soft unlink (`unlinked_at`) rather than a delete (`internal/center/store/vps_node_links.go:93-118`).

8. **No lifecycle synchronization between VPS lifecycle and Node lifecycle**
   - VPS lifecycle is an Asset Ledger machine value (`internal/center/vpsassets/types.go:16-26`). Node lifecycle is a separate Chinese machine value set (`internal/center/nodes/types.go:10-18`).
   - VPS lifecycle PATCH updates `vps_assets.lifecycle_status`; Node lifecycle controls call separate node repository methods and insert Node state-change events (`internal/center/store/vps_assets.go:425-435`, `internal/center/store/nodes.go:835-909`).
   - No code path found that maps VPS `to_cancel`/`cancelled`/`archived` to Node `不续费`/`已退役`, or vice versa.

9. **No dashboard write-back from read-model findings**
   - Dashboard computes renewal due, unreviewed, to-cancel, to-migrate, unlinked, abnormal-linked, and cost read models (`internal/center/store/dashboard.go:411-497`).
   - No code path found where dashboard findings create renewal decisions, update subscriptions, or create/remove links.

### Related Specs

- `.trellis/spec/backend/database-guidelines.md` — authoritative backend contracts for Asset Ledger providers, VPS assets, subscriptions, links, histories, experience logs, services/domains, import, dashboard summary, and node onboarding token semantics. Especially relevant lines: provider/VPS/subscription/link boundaries (`.trellis/spec/backend/database-guidelines.md:238-285`), timeline/history transaction contracts (`.trellis/spec/backend/database-guidelines.md:287-310`), experience logs (`.trellis/spec/backend/database-guidelines.md:312-388`), services/domains (`.trellis/spec/backend/database-guidelines.md:390-557`), import (`.trellis/spec/backend/database-guidelines.md:559-568`), dashboard read model (`.trellis/spec/backend/database-guidelines.md:570-581`), core model invariants (`.trellis/spec/backend/database-guidelines.md:657-668`).
- `.trellis/spec/guides/cross-layer-thinking-guide.md` — cross-layer checklist for mapping source → transform → store → retrieve → display, useful because this task spans database/store/handler/frontend (`.trellis/spec/guides/cross-layer-thinking-guide.md:20-49`).
- `docs/operations/asset-ledger-real-data-validation-readiness.md` — states current public validation scope and boundaries for `/asset-decisions`, `/vps`, `/providers`, `/subscriptions`; warns not to claim real provider truth, billing accuracy, or real linked-node health without separate verification (`docs/operations/asset-ledger-real-data-validation-readiness.md:1-21`, `docs/operations/asset-ledger-real-data-validation-readiness.md:166-190`).

### External References

- None. This audit used internal repository code, migrations, frontend contracts, and Trellis/project docs only.

## Caveats / Not Found

- This audit did not execute tests or run the server; it is source-level research.
- The repo status snapshot at session start was clean, but this research did not inspect untracked runtime data or a live database.
- The search did not find a product requirement saying renewal/autorenew decisions should propagate. In fact, current backend spec records no-propagation boundaries for Asset Ledger aggregates.
- The search did not find delete endpoints for the audited asset aggregates, update/delete for service/domain assets, direct renewal/price/IP/spec history create endpoints, automatic node-link creation from onboarding/import, or cross-aggregate transactions that synchronize VPS renewal decisions with subscription auto-renew flags.

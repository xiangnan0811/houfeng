# Research: VPS overview action / route contract and React Router remediation

- Query: Research I-01 and M-01 only. Identify the smallest existing-code-consistent action/route contract, current owners and tests, safe destinations for every anomaly/relation kind, and the React Router upgrade boundary.
- Scope: mixed
- Date: 2026-08-24

## Findings

### 1. Decision

The smallest robust contract is not another set of unchecked server-provided URLs. The wire already has the two semantic discriminators needed to keep navigation closed:

- anomaly primary/secondary actions have action.id;
- relation summaries have relation.kind.

Use those stable values as a closed dispatch contract. Keep route only for a destination that really is a registered React Router route, make it optional on both Go and TypeScript relation DTOs, and require command destinations to omit route. The page composition resolves a known ID/kind to one of:

    { kind: "route", to: knownInternalRoute }
    { kind: "command", run: pageOwnedCallback }

Unknown IDs/kinds, a route on a command-only destination, or a route that does not exactly equal the allowlisted destination must fail closed: no Link and no navigation. A disabled/non-interactive label is acceptable; silently falling back to /, /vps/<id>, or the dashboard is not.

Do not add another free-form command string to the API. It would duplicate action.id/relation.kind and create a second untyped cross-layer control surface. Do not accept any API string merely because it starts with "/" or appears same-origin; exact token-to-destination matching is both smaller and stronger.

The renderer should use Link only for a resolved route and Button for a resolved command. This follows the existing product rule that links navigate and buttons perform same-page/state actions, and the Web spec rule that route ownership belongs in the page/controller rather than a presentation component.

### 2. Exact anomaly destination matrix

The backend owns which rule fired and its semantic action ID. The canonical VPS page owns navigation and commands.

| Rule ID | Recommended action ID | Destination type | Safe destination / owner | Reason |
| --- | --- | --- | --- | --- |
| monitoring.health.abnormal.v1 | open_monitoring | route | /monitoring?abnormal=1 | /monitoring is registered and MonitoringPage consumes abnormal=1. This is a real abnormal-monitoring work surface. |
| monitoring.incidents.open.v1 | open_incidents | route | /events?object_type=monitoring_instance | /events is registered and EventsPage consumes object_type=monitoring_instance. There is no /incidents page. |
| ip_quality.risk.elevated.v1 | open_ip_quality | route | /vps/<encoded-vps-id>/ip-quality | Exact registered VPS-scoped evidence page. |
| ip_quality.stale.v1 | open_ip_quality | route | /vps/<encoded-vps-id>/ip-quality | Same exact registered evidence page. |
| ip_quality.partial.v1 | open_ip_quality | route | /vps/<encoded-vps-id>/ip-quality | Same exact registered evidence page. |
| renewal.subscription.missing.v1 | open_subscription | command | management.openPanel("subscription") | Existing canonical owner opens the subscription fact form. A link to the current page cannot create/manage the missing subscription. |
| renewal.due.soon.v1 | open_renewal_decision | command | management.openPanel("decision") | Existing legacy and canonical management semantics use the renewal-decision panel for a due-soon decision. The present shared open_subscriptions ID is ambiguous and should be split. |
| lifecycle.blocker.v1 | open_management | command | management.openMenu() | The label is generic (“打开管理”), and the menu is the existing safe owner for both to_cancel and to_migrate without pretending that migration is a cancellation workflow. |
| source.unavailable.v1 | retry_overview | command | commands.refresh(), passed as onRefresh | This is a data reload command, not navigation. |

The current comment makes rule IDs stable (internal/center/vpsoverview/anomalies.go:9-20), but does not declare action IDs stable. Splitting the ambiguous open_subscriptions action into open_subscription and open_renewal_decision is therefore a bounded full-stack contract correction. If compatibility requires preserving the old ID temporarily, dispatch must key on the exact pair (rule_id, action.id); it must never infer behavior from label or detail.

The monitoring and events routes above are valid and useful but not VPS-scoped because the present list pages do not accept vps_id. MonitoringPage recognizes group/region/city/provider/lifecycle/run_status/health/labels/abnormal/onboarding only (web/src/pages/MonitoringPage.tsx:136-188); EventsPage recognizes object_type and other event filters but no VPS ID (web/src/pages/EventsPage.tsx:32-33,64-90,118-138). Adding a VPS-scoped monitoring/events query would be a separate product/API expansion, not required to stop invalid/inert navigation.

### 3. Exact relation destination matrix and missing-owner wire shape

| relation.kind | Final destination type | Safe destination / owner | Current state |
| --- | --- | --- | --- |
| monitoring_instances | command | open a canonical monitoring-instance-evidence panel for this VPS | The generic /monitoring route is registered but is not VPS-scoped. The exact existing implementation is the legacy monitoring-instance-evidence panel and VPSMonitoringInstanceLinksSection. Canonical overview has no owner yet. |
| subscriptions | route | /subscriptions?vps_id=<encoded-vps-id> | Registered and SubscriptionsPage reads/canonicalizes vps_id (web/src/pages/SubscriptionsPage.tsx:101-120,393-458). |
| services | command | open a canonical services-detail panel for this VPS | No services page/route exists. The existing exact implementation is the legacy services-detail panel using listVPSServices and VPSServicesSection. Canonical overview has no owner yet. |
| domains | command | open a canonical domains-detail panel for this VPS | No domains page/route exists. The existing exact implementation is the legacy domains-detail panel using listVPSDomains and VPSDomainsSection. Canonical overview has no owner yet. |

Required wire shape for relation kinds without a route owner:

    Go:
      type RelationSummary struct {
          Kind   string `json:"kind"`
          Count  int    `json:"count"`
          Status string `json:"status,omitempty"`
          Route  string `json:"route,omitempty"`
          Label  string `json:"label"`
      }

    TypeScript:
      type VPSOverviewRelationKind =
        | "monitoring_instances"
        | "subscriptions"
        | "services"
        | "domains"

      type VPSOverviewRelation = {
        kind: VPSOverviewRelationKind
        count: number
        status?: string
        route?: string
        label: string
      }

For monitoring_instances, services, and domains the server sends no route; the known kind resolves to a page-owned command. For subscriptions the server sends only the exact allowlisted filtered route. An unknown kind or malformed/mismatched route is informational only and never becomes a Link.

The canonical owner can be added with the least conceptual drift by extending VPSManagementPanel with the already established names monitoring-instance-evidence, services-detail, and domains-detail, then rendering those modal bodies from VPSOverviewManagementActions (or a colocated relation-panel owner sharing the same controller). Reuse the existing focused components/API helpers; do not import LegacyVPSDetail itself into the canonical chunk.

Evidence that these are established behaviors rather than new IA:

- vpsDetailOverviewModel.ts:226-303 maps a single monitoring instance to its detail route, multiple/zero monitoring relations to monitoring-instance-evidence, subscriptions to /subscriptions?vps_id=..., services to services-detail, and domains to domains-detail.
- LegacyVPSDetail.tsx:1387-1422 renders VPSMonitoringInstanceLinksSection, VPSServicesSection, and VPSDomainsSection for those modes.
- LegacyVPSDetail.tsx:1590-1602 owns the modal boundary.
- vpsDetailOverviewModel.test.ts:255-261 already locks services-detail/domains-detail; LegacyVPSDetail.test.tsx:690-695 locks the registered subscription, monitoring-detail, and IP-quality destinations.
- listVPSServices and listVPSDomains already exist in web/src/lib/api.ts:662-681 and are tested in api.test.ts:1118-1124,1236-1242.

This relation-panel work is mandatory if the requirement is “the relation entry opens the related resources for this VPS.” Mapping services/domains back to /vps/<id>, /vps/<id>/activity, /records, or /asset-decisions would be registered but semantically false. Mapping monitoring_instances to /monitoring is a safe temporary global-list fallback, but it is not an exact VPS relation owner.

### 4. Current ownership and why present tests missed I-01

#### Backend/wire owners

- internal/center/vpsoverview/anomalies.go:55-206 owns rule evaluation and emits every anomaly action. Current bad values are /vps/<id>/monitoring at 69-71, /incidents at 82-84, and same-page /vps/<id> at 97-126,140-187.
- internal/center/store/vps_overview.go:258-296 owns relation summaries and currently assigns one /vps/<id> route to all four kinds.
- internal/center/vpsoverview/types.go:123-160 owns RelationSummary and AnomalyAction wire fields. Relation route is currently required; anomaly route is already optional.
- web/src/lib/types.ts:3337-3366 mirrors those fields but leaves IDs/kinds as unrestricted strings and relation.route required.
- web/src/lib/recordsApi.ts:512-580 normalizes arrays but performs no action/relation destination validation.

#### Frontend route/command owners

- web/src/app/router.tsx:105-190 is the route registry. Relevant registered routes are /vps/:vpsId/ip-quality, /vps/:vpsId, /subscriptions, /monitoring, /monitoring/:monitoringInstanceId, and /events; wildcard routes redirect to /.
- web/src/pages/VPSDetailPage.tsx:119-170 is the canonical overview composition. It creates useVPSOverview and useVPSManagementController, passes commands.refresh to the view, and mounts VPSOverviewManagementActions.
- web/src/pages/vps-detail/hooks/useVPSOverview.ts:16-18,64-125 owns the real retry command.
- web/src/pages/vps-detail/hooks/useVPSManagementController.ts:3-44 owns menu/panel state. Current canonical panels are facts, decision, subscription, cancellation, archive; monitoring/services/domains detail are absent.
- web/src/pages/vps-detail/VPSOverviewManagementActions.tsx:91-215,456-630 owns the five current canonical modal workflows.
- web/src/pages/vps-detail/VPSOverviewPageView.tsx:24-94 is the correct integration point for resolving an API semantic action to a route or page-owned callback.
- web/src/pages/vps-detail/VPSOverviewAnomalies.tsx:35-55 and VPSOverviewRelations.tsx:17-25 currently turn non-empty API strings directly into Link.

#### Existing tests and gaps

- internal/center/vpsoverview/anomalies_test.go:30-134 covers all nine rule families but asserts only that a rule exists and secondary_actions is non-nil. It never asserts action ID, route, or command shape.
- internal/center/store/vps_overview_test.go:75-113 asserts only four relations, not kind/order/route semantics.
- web/src/app/router.test.tsx:8-95 tests selected registrations but not overview-produced destinations or wildcard avoidance.
- web/src/pages/vps-detail/VPSOverviewAnomalies.test.tsx:7-32 tests only healthy zero-DOM behavior.
- web/src/pages/vps-detail/VPSOverviewPageView.test.tsx:137-157 asserts only that an action Link exists; its same-page fixture would normalize the bug.
- web/src/pages/vps-detail/hooks/useVPSManagementController.test.tsx:6-21 covers only facts.
- web/src/pages/vps-detail/VPSOverviewManagementActions.test.tsx:73-137 covers stale/duplicate fact mutations, not destination panel ownership.
- web/e2e/page-states.spec.ts:292-309 asserts only that “处理续费” is visible as a Link and uses the broken same-page fixture.
- web/e2e/fixtures/profiles.ts:653-660 uses singular monitoring_instance, while production emits monitoring_instances. A closed union will expose this fixture drift and the fixture must be corrected.
- There is no VPSOverviewRelations.test.tsx today.

### 5. Exact RED files and commands

Write failing tests before implementation.

Backend RED files:

1. internal/center/vpsoverview/anomalies_test.go
   - Add a table that asserts every stable rule ID, exact action ID, label, and route-vs-command shape.
   - Assert command actions have empty route.
   - Assert route actions match the table in section 2.

       go test ./internal/center/vpsoverview -run 'TestEvaluateAnomalies(ActionDestinations|Table)' -count=1

2. internal/center/store/vps_overview_test.go
   - Assert the exact ordered relation kinds.
   - Assert subscriptions has the filtered route.
   - Assert monitoring_instances/services/domains omit route when they dispatch to local panels.

       go test ./internal/center/store -run TestVPSOverviewRepositoryLoadSourcesHealthy -count=1

Frontend RED files:

3. Add web/src/pages/vps-detail/vpsOverviewDestination.test.ts beside a pure resolver.
   - Table every anomaly rule/action pair and every relation kind.
   - Assert route destinations are exact.
   - Assert command destinations are exact.
   - Assert unknown ID/kind, https://evil.example, //evil.example, and backslash-based targets return no destination.
   - For every resolved route, assert matchRoutes(appRoutes, to) reaches the intended route and not wildcard.

4. Update/add:
   - web/src/pages/vps-detail/VPSOverviewAnomalies.test.tsx
   - web/src/pages/vps-detail/VPSOverviewRelations.test.tsx (new)
   - web/src/pages/vps-detail/VPSOverviewPageView.test.tsx
   - web/src/pages/vps-detail/hooks/useVPSManagementController.test.tsx
   - web/src/pages/vps-detail/VPSOverviewManagementActions.test.tsx
   - web/src/app/router.test.tsx

   Required assertions: Link versus Button semantics, exact href, callbacks for subscription/decision/menu/retry, exact monitoring/services/domains panels, unknown destination fail-closed, and focus return after a relation panel closes.

       npm --prefix web run test -- --run src/pages/vps-detail/vpsOverviewDestination.test.ts src/pages/vps-detail/VPSOverviewAnomalies.test.tsx src/pages/vps-detail/VPSOverviewRelations.test.tsx src/pages/vps-detail/VPSOverviewPageView.test.tsx src/pages/vps-detail/hooks/useVPSManagementController.test.tsx src/pages/vps-detail/VPSOverviewManagementActions.test.tsx src/app/router.test.tsx

Browser RED files:

5. Update web/e2e/fixtures/profiles.ts and add web/e2e/vps-overview-destinations.spec.ts (or place the cases in page-states.spec.ts).
   - Correct monitoring_instance to monitoring_instances.
   - Click every route action and assert the final pathname/search.
   - Click every command action and assert the right dialog/menu or a new overview request for retry.
   - Click all four relation entries and assert the filtered route or exact dialog.
   - Inject external/protocol-relative/backslash routes and assert no external navigation and no clickable Link.

       npm --prefix web run test:e2e -- e2e/vps-overview-destinations.spec.ts

React Router audit RED:

6. Under the repository-pinned Node version:

       /home/murray/.nvm/versions/node/v22.23.1/bin/node /home/murray/.nvm/versions/node/v22.23.1/lib/node_modules/npm/bin/npm-cli.js audit --omit=dev --json

   Fresh result on 2026-08-24: exit 1; 2 vulnerable production packages, 1 moderate and 1 high aggregate, with 5 react-router advisories.

### 6. GREEN implementation/gate sequence

Likely implementation owners:

- internal/center/vpsoverview/anomalies.go
- internal/center/vpsoverview/types.go
- internal/center/store/vps_overview.go
- web/src/lib/types.ts
- web/src/pages/vps-detail/vpsOverviewDestination.ts (new closed resolver)
- web/src/pages/vps-detail/VPSOverviewAnomalies.tsx
- web/src/pages/vps-detail/VPSOverviewRelations.tsx
- web/src/pages/vps-detail/VPSOverviewPageView.tsx
- web/src/pages/vps-detail/hooks/useVPSManagementController.ts
- web/src/pages/vps-detail/VPSOverviewManagementActions.tsx or a colocated relation-panel owner
- web/package.json and web/package-lock.json
- tests/fixtures listed in section 5

Focused GREEN:

    go test ./internal/center/vpsoverview ./internal/center/store -count=1

    npm --prefix web run test -- --run src/pages/vps-detail/vpsOverviewDestination.test.ts src/pages/vps-detail/VPSOverviewAnomalies.test.tsx src/pages/vps-detail/VPSOverviewRelations.test.tsx src/pages/vps-detail/VPSOverviewPageView.test.tsx src/pages/vps-detail/hooks/useVPSManagementController.test.tsx src/pages/vps-detail/VPSOverviewManagementActions.test.tsx src/app/router.test.tsx

    npm --prefix web run test:e2e -- e2e/vps-overview-destinations.spec.ts

Full GREEN with Node 22.23.1:

    make verify-web
    npm --prefix web run test:e2e
    npm --prefix web run build
    npm --prefix web run bundle:check
    npm --prefix web audit --omit=dev

The production-browser assertion must be behavior-based: final pathname/search, opened dialog/menu, or observed refresh request. “A link exists” is not sufficient.

### 7. React Router upgrade implications (M-01)

Current repository state:

- web/package.json:23-30 declares react-router-dom ^7.17.0.
- web/package-lock.json:5073-5109 locks react-router and react-router-dom at 7.17.0.
- .node-version pins 22.23.1.
- web/src/app/router.tsx:190 uses createBrowserRouter and web/src/main.tsx:3,22 uses RouterProvider: this is client Data Mode.
- A workspace search found no unstable_RSC, ServerRouter, HydratedRouter, createRequestHandler, @react-router/dev, or react-router.config usage.

Upgrade target:

- Set the direct minimum to react-router-dom ^7.18.2 and regenerate the lock so react-router-dom and its exact react-router dependency are both 7.18.2.
- Do not stop at 7.18.0 or 7.18.1. The newly updated GHSA-qwww-vcr4-c8h2 range includes 7.12.0 through 7.18.1 and lists 7.18.2 as the v7 patched version.
- Do not jump to v8 for this remediation. The maintained v7 backport exists and keeps the current react-router-dom import/API surface.

Compatibility:

- npm registry metadata for 7.18.2 says Node >=20 and React/React DOM >=18. The project uses Node 22.23.1 and React/React DOM ^19.2.5, so engine/peer ranges are satisfied.
- 7.18.2 itself is a patch that hardens RSC CSRF paths. The application does not use RSC, so no code migration is expected from that patch.
- The effective 7.17.0 to 7.18.2 update also includes 7.18.0 route matching and URL-normalization changes plus 7.18.1's CommonJS package-main correction. The 7.18.0 reverse-proxy/allowedActionOrigins warning concerns Framework/server adapters; this client Data Mode app has no such adapter. Still run router match tests, full Vitest, production build/bundle budget, and Chromium E2E because route matching/navigation internals changed.
- 7.18.2 fixes the external-navigation backslash bypass, but it does not replace the application-level allowlist. API-provided action/relation strings must still be constrained to known internal destinations.

Current applicability:

- The external navigation advisory is directly relevant to the current sink shape because VPSOverviewAnomalies and VPSOverviewRelations pass API route strings to Link. The current backend generates fixed internal strings and the handler rejects a VPS ID containing "/" (internal/center/http/handlers/vps_overview.go:48-58), so this research did not establish a current attacker-controlled external URL exploit chain.
- The __manifest DoS advisory explicitly affects Framework Mode, not Data Mode; it is not reachable through this createBrowserRouter SPA.
- The RSC XSS/constructor/CSRF advisories require SSR/RSC paths absent from this app. They still keep npm audit red at 7.17.0, and the compatible 7.18.2 patch is available.

External references:

- [React Router 7.18.2 release](https://github.com/remix-run/react-router/releases/tag/react-router%407.18.2)
- [Official v7 changelog, 7.18.2 through 7.18.0](https://github.com/remix-run/react-router/blob/v7/CHANGELOG.md#v7182)
- [External navigation advisory GHSA-wrjc-x8rr-h8h6](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6)
- [RSC CSRF advisory GHSA-qwww-vcr4-c8h2](https://github.com/advisories/GHSA-qwww-vcr4-c8h2)
- [Framework-mode-only DoS advisory GHSA-chx6-hx7r-mcp5](https://github.com/remix-run/react-router/security/advisories/GHSA-chx6-hx7r-mcp5)
- npm registry metadata queried on 2026-08-24 for react-router-dom@7.18.2 and react-router@7.18.2.

## Files found

- internal/center/vpsoverview/anomalies.go — rule/action producer.
- internal/center/vpsoverview/anomalies_test.go — rule tests with no destination assertions.
- internal/center/vpsoverview/types.go — Go action/relation wire contract.
- internal/center/store/vps_overview.go — relation summary producer.
- internal/center/store/vps_overview_test.go — relation-count-only test.
- internal/center/http/handlers/vps_overview.go — overview path parsing and VPS ID slash rejection.
- web/src/app/router.tsx — authoritative registered route table.
- web/src/app/router.test.tsx — matchRoutes coverage, currently not overview-destination-complete.
- web/src/lib/types.ts — handwritten TypeScript overview DTO.
- web/src/lib/recordsApi.ts — overview response normalization boundary.
- web/src/pages/VPSDetailPage.tsx — canonical route composition.
- web/src/pages/MonitoringPage.tsx — supports abnormal filter, not vps_id.
- web/src/pages/EventsPage.tsx — supports monitoring_instance object type, not vps_id.
- web/src/pages/SubscriptionsPage.tsx — supports vps_id filter.
- web/src/pages/vps-detail/VPSOverviewPageView.tsx — correct page-level route/command integration owner.
- web/src/pages/vps-detail/VPSOverviewAnomalies.tsx — current unchecked Link sink.
- web/src/pages/vps-detail/VPSOverviewRelations.tsx — current unchecked Link sink.
- web/src/pages/vps-detail/hooks/useVPSOverview.ts — refresh owner.
- web/src/pages/vps-detail/hooks/useVPSManagementController.ts — canonical panel/menu owner.
- web/src/pages/vps-detail/VPSOverviewManagementActions.tsx — canonical modal workflow owner.
- web/src/pages/vps-detail/vpsDetailOverviewModel.ts — pre-existing semantic route/modal model.
- web/src/pages/vps-detail/LegacyVPSDetail.tsx — existing monitoring/services/domains exact modal owners.
- web/src/pages/vps-detail/VPSMonitoringInstanceLinksSection.tsx — VPS-scoped monitoring relation UI.
- web/src/pages/vps-detail/VPSServicesSection.tsx — VPS-scoped services relation UI.
- web/src/pages/vps-detail/VPSDomainsSection.tsx — VPS-scoped domains relation UI.
- web/e2e/fixtures/profiles.ts — current overview fixture and singular relation-kind drift.
- web/e2e/page-states.spec.ts — overview presence/state tests with no destination behavior.
- web/package.json — direct router range.
- web/package-lock.json — vulnerable 7.17.0 lock.
- .node-version — Node 22.23.1 verification owner.

## Code patterns

- Rule IDs are explicitly stable contracts: internal/center/vpsoverview/anomalies.go:9-20.
- All present route bugs originate in one rule producer: internal/center/vpsoverview/anomalies.go:55-187.
- All relation route bugs originate in one repository method: internal/center/store/vps_overview.go:258-296.
- Direct API-to-Link sinks: web/src/pages/vps-detail/VPSOverviewAnomalies.tsx:35-55 and VPSOverviewRelations.tsx:17-25.
- Canonical command wiring already converges in one place: web/src/pages/VPSDetailPage.tsx:119-170.
- Retry is already a first-class command: web/src/pages/vps-detail/hooks/useVPSOverview.ts:16-18,64-125.
- Current canonical management commands are already modal-owned: web/src/pages/vps-detail/hooks/useVPSManagementController.ts:3-44 and VPSOverviewManagementActions.tsx:91-215,456-630.
- Existing legacy model already distinguishes route versus modal instead of forcing every task through Link: web/src/pages/vps-detail/vpsDetailOverviewModel.ts:33-56,226-303.

## Related specs

- .trellis/spec/web/component-conventions.md:40-44 — route-agnostic presentation, page-provided callback/Link slots.
- .trellis/spec/web/component-conventions.md:58-61 — search/navigation targets must be registered and subscriptions use the vps_id filtered list.
- .trellis/spec/web/state-and-data.md:14 — URL state belongs to React Router.
- .trellis/spec/web/state-and-data.md:311-340 — established monitoring evidence/deep-link semantics.
- .trellis/spec/web/quality-guidelines.md:442-456 — cross-layer DTO synchronization, route tests, lazy loading, and bundle checks.
- .trellis/spec/guides/cross-layer-thinking-guide.md:18-50,75-87 — map boundaries, define exact contracts, and test invalid/edge cases.
- .trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/design.md:119-155 — overview relations require explicit destinations; anomalies carry one primary/two secondary actions; management state belongs to a controller/modal owner.
- .trellis/tasks/archive/2026-08/07-13-vps-detail-experience-design/research/visual-design-contract.md:18-44 — relations are compact entries; anomaly actions appear in focus order.
- .trellis/tasks/archive/2026-08/07-13-vps-detail-experience-design/research/external-product-patterns.md:82-92 — Link means navigation; Button means same-page action.

## Caveats / Not Found

- There is no currently registered VPS-scoped monitoring list route and no EventsPage vps_id filter. The selected anomaly routes are valid filtered work surfaces but not exact VPS filters.
- There are no registered services or domains pages. Any route-only fix for those relations would be dishonest; the exact existing behavior is a local panel.
- Canonical overview currently lacks monitoring-instance-evidence, services-detail, and domains-detail panel owners. They exist only in the lazy legacy implementation and must be reused/factored without importing the entire LegacyVPSDetail graph into the canonical entry.
- The archived design says relations expose an explicit route, while the current codebase also establishes modal destinations for services/domains. Making route optional for command-owned relations is a narrow contract clarification; if Trellis treats the archived wording as requiring a URL for every relation, update the governing spec/design in the implementation task rather than inventing false routes.
- The current package manifest has moved to React 19.2.5 since the prior audit, but React Router remains locked at 7.17.0. Research conclusions use the current worktree, not the older report’s dependency snapshot.
- npm audit is time-sensitive and network-backed. The reported result is fresh on 2026-08-24 under Node 22.23.1; rerun it after lock regeneration and again in CI.
- No product, test, task PRD, spec, package, lock, Git ref, or external state was modified by this research.

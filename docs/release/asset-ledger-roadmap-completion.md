# Asset Ledger Roadmap Completion Audit

> Date: 2026-05-10
>
> Scope: `houfeng_codex_下一步开发计划.md` Task 1-8 and section 11 first-stage completion standards.
>
> Decision: the Asset Ledger implementation track is functionally closed for the current plan, except the real 40+ VPS data run, which remains user-data-dependent and intentionally deferred.

## Conclusion

The repository now has a working VPS Asset Ledger next to the original Fleet Observability system:

- Providers, VPS assets, subscriptions, VPS-to-Node links, renewal decisions, asset histories, timeline/experience logs, service assets, domain assets, asset pages, decision pages, and Dashboard asset summary are implemented.
- Task 4's dry-run/import tooling is implemented, but the actual real 40+ VPS dataset has not been executed in this repository because the real-data problem was explicitly deferred.
- Services and domains were implemented as VPS-scoped manual records. They do not implement full service discovery, service registry, DNS provider sync, registrar sync, or DNS record management.
- No further immediate feature task remains from `houfeng_codex_下一步开发计划.md`. The next development item should only be opened after a real dataset run exposes a concrete model gap, or after a new product plan expands the scope.

## Plan Task Matrix

| Plan item | Status | Implementation evidence | Verification evidence | Notes |
|---|---|---|---|---|
| Task 1: Asset Ledger schema + providers vertical slice | Complete | `db/migrations/0016_create_asset_ledger.sql`, `internal/center/providers/`, `internal/center/store/providers.go`, `internal/center/http/handlers/providers.go`, `internal/center/http/router.go`, `cmd/houfeng-center/bootstrap.go` | `internal/center/providers/types_test.go`, `internal/center/store/providers_test.go`, `internal/center/http/handlers/providers_test.go`, `internal/center/http/router_api_test.go`; commit `7bfc7c2` / PR #4 | The first slice delivered usable API, not just skeleton schema. |
| Task 2: VPS assets backend | Complete | `db/migrations/0017_add_vps_assets.sql`, `internal/center/vpsassets/`, `internal/center/store/vps_assets.go`, `internal/center/http/handlers/vps.go`, router/bootstrap wiring | `internal/center/vpsassets/types_test.go`, `internal/center/store/vps_assets_test.go`, `internal/center/http/handlers/vps_test.go`, `internal/center/http/router_api_test.go`; commit `de9bd5a` / PR #5 | Stable machine statuses stay in API/DB; Chinese labels remain frontend mapping. |
| Task 3: Subscriptions backend | Complete | `db/migrations/0018_add_subscriptions.sql`, `internal/center/subscriptions/`, `internal/center/store/subscriptions.go`, `internal/center/http/handlers/subscriptions.go` | `internal/center/subscriptions/types_test.go`, `internal/center/store/subscriptions_test.go`, `internal/center/http/handlers/subscriptions_test.go`, `internal/center/http/router_api_test.go`; commit `20f08ed` / PR #7 | Backend computes `monthly_price` and supports renewal-window filtering. |
| Task 4: Real VPS JSON dry-run/import model validation | Tooling complete; real-data execution deferred | `internal/center/importing/`, `cmd/houfeng-import-vps-json/`, `internal/center/importing/report.go` | `internal/center/importing/importing_test.go`, `cmd/houfeng-import-vps-json/main_test.go`; commit `946ecb8` / PR #8 | The importer can dry-run, report validation/model gaps, detect duplicates, and import after validation. The actual 40+ VPS file is outside this task until the user authorizes/provides it. |
| Task 5: VPS and Node association | Complete | `db/migrations/0019_create_vps_node_links.sql`, `internal/center/assetlinks/`, `internal/center/store/vps_node_links.go`, `internal/center/http/handlers/asset_links.go`, router/bootstrap wiring | `internal/center/assetlinks/types_test.go`, `internal/center/store/vps_node_links_test.go`, `internal/center/http/handlers/asset_links_test.go`, `internal/center/http/router_api_test.go`; commit `9913a1c` | Link/unlink preserves history and does not rewrite Node state or `nodes.provider`. |
| Task 6: Asset frontend pages | Complete | `web/src/lib/api.ts`, `web/src/lib/types.ts`, `web/src/pages/ProvidersPage.tsx`, `web/src/pages/VPSPage.tsx`, `web/src/pages/VPSDetailPage.tsx`, `web/src/pages/SubscriptionsPage.tsx`, `web/src/app/router.tsx` | `web/src/lib/api.test.ts`, `web/src/pages/ProvidersPage.test.tsx`, `web/src/pages/VPSPage.test.tsx`, `web/src/pages/VPSDetailPage.test.tsx`, `web/src/pages/SubscriptionsPage.test.tsx`; commit `d30bda7` | Pages use API helpers and Chinese display labels while preserving machine values on the wire. |
| Task 7: Dashboard asset summary | Complete | Asset summary queries in `internal/center/store/dashboard.go`, response fields in `internal/center/incidents/types.go`, handler response, `web/src/pages/DashboardPage.tsx` | `internal/center/store/dashboard_test.go`, `internal/center/http/handlers/dashboard_test.go`, `web/src/pages/DashboardPage.test.tsx`; commit `1855bf1` / PR #11 | Dashboard remains an overview workbench, not an asset field dump. |
| Task 8: History and decision enhancements | Complete | `db/migrations/0020_create_renewal_decisions.sql`, `0021_create_asset_histories.sql`, `0022_create_experience_logs.sql`, `internal/center/renewals/`, `internal/center/store/renewal_decisions.go`, `GET /api/vps/{id}/timeline`, `web/src/pages/AssetDecisionsPage.tsx`, VPS current-state editing and archive workflow | `internal/center/renewals/types_test.go`, `internal/center/store/renewal_decisions_test.go`, `internal/center/http/handlers/vps_test.go`, `web/src/pages/AssetDecisionsPage.test.tsx`, `web/src/pages/VPSDetailPage.test.tsx`; commits `9ebdfd9`, `ee60865`, `562b4b1`, `5210d33`, `ade57fd`, `dcba7b2`, `8432994` | Renewal decisions, price history, IP history, spec snapshots, timeline, experience logs, current-state edits, and archive/restore are present. |

## Additional Lightweight Asset Extensions

The plan's section 10 deferred a full service registry and full domain management. The current implementation stays within the allowed lightweight boundary:

| Extension | Status | Evidence | Boundary |
|---|---|---|---|
| VPS-scoped service assets | Complete | `db/migrations/0023_create_asset_services.sql`, `internal/center/assetservices/`, `internal/center/store/asset_services.go`, `internal/center/http/handlers/asset_services.go`, `web/src/pages/VPSDetailPage.tsx`, `web/src/lib/api.ts` | Manual VPS-scoped records with optional Target references only. No service discovery, registry, orchestration, or Agent collection. |
| VPS-scoped domain assets | Complete | `db/migrations/0024_create_asset_domains.sql`, `internal/center/assetdomains/`, `internal/center/store/asset_domains.go`, `internal/center/http/handlers/asset_domains.go`, `web/src/pages/VPSDetailPage.tsx`, `web/src/lib/api.ts` | Manual VPS-scoped records only. No DNS provider sync, registrar sync, DNS record management, or certificate probing side effects. |

Both extensions have domain/store/handler/router/bootstrap wiring and tests:

- `internal/center/assetservices/types_test.go`
- `internal/center/store/asset_services_test.go`
- `internal/center/http/handlers/asset_services_test.go`
- `internal/center/assetdomains/types_test.go`
- `internal/center/store/asset_domains_test.go`
- `internal/center/http/handlers/asset_domains_test.go`
- `internal/center/http/router_api_test.go`
- `web/src/pages/VPSDetailPage.test.tsx`

## First-Stage Completion Standards

| Standard from section 11 | Status | Evidence | Notes |
|---|---|---|---|
| 1. Can enter real VPS assets | Complete | `/api/vps`, `web/src/pages/VPSPage.tsx`, `web/src/pages/VPSDetailPage.tsx` | CRUD and current-state edits are implemented. |
| 2. Can enter providers | Complete | `/api/providers`, `web/src/pages/ProvidersPage.tsx` | Create/list/read/patch are implemented. |
| 3. Can record price, billing cycle, renewal date, and auto-renew state | Complete | `subscriptions` table/domain/store/API/UI | Subscription create/patch supports these fields. |
| 4. Backend computes monthly price | Complete | `internal/center/subscriptions/`, `internal/center/store/subscriptions.go` | Tests cover recalculation on patch. |
| 5. Can associate VPS with existing Node | Complete | `vps_node_links`, `/api/vps/{id}/link-node`, `/api/vps/{id}/unlink-node` | Association is independent from Node lifecycle/provider fields. |
| 6. VPS detail shows linked Node monitoring summary | Complete | `GET /api/vps/{id}` detail response and `VPSDetailPage` linked Node section | The detail page loads health and summary fields for linked Nodes. |
| 7. Node side shows linked VPS summary | Complete | `GET /api/nodes/{node_id}/vps`, `web/src/pages/NodeDetailPage.tsx` | Node detail has the asset ledger summary section. |
| 8. Can validate real 40+ VPS data through dry-run/import | Tooling complete; real-data execution deferred | `cmd/houfeng-import-vps-json`, `internal/center/importing/` | The actual real dataset was not run by policy/user decision, so this is not marked fully complete. |
| 9. Can view next 30-day renewal candidates | Complete | `GET /api/subscriptions?renew_within_days=30`, `AssetDecisionsPage`, Dashboard asset summary | Backend filters and frontend queues are implemented. |
| 10. Can mark keep/observe/migrate/cancel decisions | Complete | `renewal_decision` fields, renewal decision history, `AssetDecisionsPage`, `VPSDetailPage` current-state edit | Current state and history are both present. |
| 11. Dashboard can show a small asset decision summary | Complete | `DashboardAssetSummary`, `DashboardPage` asset summary tests | Summary remains compact and decision-oriented. |
| 12. Existing Node / Target / Agent / Dashboard monitoring does not regress | Complete subject to current local/CI verification | Asset work is routed through new asset tables and APIs; link APIs explicitly avoid Node/Target mutation | This audit task runs the full local verification gate before PR. PR CI and main CI remain the merge gate. |

## Real Data Boundary

The repository has the importer model and command:

```bash
go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -dry-run
go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -import
```

The importer validates provider/VPS/subscription inputs, reports missing or invalid fields, identifies duplicate candidates, reports renewal candidates, and only writes providers, VPS assets, and subscriptions when validation passes.

What is not complete yet: an actual run against the user's real 40+ VPS dataset. That is intentionally not counted as finished because the data file and authorization are outside the repository and the user previously deferred real-data work.

## Remaining Work

No immediate implementation task remains in this plan. The remaining work is conditional:

| Follow-up | Trigger | Owner/action |
|---|---|---|
| Run real 40+ VPS dry-run | User provides or authorizes the real JSON dataset | Execute the dry-run command, review gaps, then decide whether to import. |
| Model correction after real-data dry-run | Dry-run report proves fields or validation are insufficient | Create a new Trellis task with the exact report evidence. |
| Import UI/API | The CLI flow becomes insufficient for repeated operator use | Plan separately; current plan only required dry-run/import tooling. |
| Full service registry or domain management | Product plan explicitly expands beyond VPS-scoped manual records | New plan required; do not infer this from the lightweight service/domain MVP. |

Of the follow-up items called out here, the event envelope design has since been closed in `docs/release/v1-gap-checklist.md` by the 2026-05-11 `05-11-events-api-envelope-migration` task. Formal notification channel modeling and Docker/exec boundary hardening were handled after this Asset Ledger audit and remain separate from the Asset Ledger plan itself.

## Verification

This audit task uses the project quality gate:

```bash
git diff --check
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build ./scripts/verify.sh
```

The PR workflow remains mandatory: feature branch, PR, CI green before merge, local `main` sync, and post-merge main CI monitoring.

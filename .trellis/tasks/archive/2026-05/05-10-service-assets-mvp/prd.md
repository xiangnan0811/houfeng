# Service assets MVP

## Goal

Add the first small service-asset slice on top of the existing VPS Asset Ledger so operators can record which user-facing or internal services run on a VPS. This continues the root plan's later `services` object direction, but deliberately stays below a complete service registry, service discovery system, domain manager, or Agent-driven automation.

## What I Already Know

* `houfeng_codex_下一步开发计划.md` positions Houfeng as `VPS Asset Ledger + Fleet Observability`.
* The root plan lists `services` and `domains` as later objects, while explicitly deferring complete service registry, full domain management, service discovery, Web SSH, plugins, and provider sync.
* The first-phase Asset Ledger backbone is already implemented and merged: providers, VPS assets, subscriptions, VPS-Node links, JSON dry-run/import command, Dashboard asset summary, renewal/history timeline, current-state edit workflows, decision workbench, archive workflow, and experience logs.
* Real 40+ VPS data execution remains deferred by user instruction; this task must not run or require real-data import.
* Existing backend patterns use one domain package under `internal/center/<domain>/`, one Postgres store file, explicit handlers/router/bootstrap wiring, and focused tests.
* Existing frontend patterns require API calls in `web/src/lib/api.ts`, shared types in `web/src/lib/types.ts`, page state with loading/error/data and cancellation flags, and no direct page-level `fetch`.
* This session must not use subagents. Implementation, checking, PR creation, CI monitoring, merge, and local main sync are performed directly in the main session.

## Scope

### In Scope

* Add a new `asset_services` table in the next migration after `0022_create_experience_logs.sql`.
* Add `internal/center/assetservices/` with domain types, stable machine-value enums, normalization, validation, and repository interface.
* Add `internal/center/store/asset_services.go` with Postgres-backed create, list-by-VPS, and list-all behavior.
* Add backend endpoints:
  * `GET /api/services`
  * `POST /api/services`
  * `GET /api/vps/{vps_id}/services`
  * `POST /api/vps/{vps_id}/services`
* Path-scoped `POST /api/vps/{vps_id}/services` must use the path as the only VPS source; request body cannot override `vps_id`.
* Service MVP fields:
  * `service_id`
  * `vps_id`
  * `target_id`
  * `name`
  * `service_type`
  * `status`
  * `url`
  * `port`
  * `labels`
  * `note`
  * `created_at`
  * `updated_at`
* Stable machine values:
  * `service_type`: `web`, `api`, `database`, `worker`, `proxy`, `other`
  * `status`: `active`, `paused`, `retired`, `unknown`
* `vps_id` is required and must reference an existing VPS asset.
* `target_id` is optional; if supplied, it must reference an existing Target. Linking a service to a Target must not modify the Target.
* Add TypeScript types and API helpers for service list/create and VPS service list/create.
* Add a compact service section to `VPSDetailPage`:
  * load services for the current VPS
  * show name, type, status, URL/port, optional Target link, labels, and note
  * provide a lightweight form to create a service for that VPS
  * refresh the service list after create
* Add focused backend and frontend tests.
* Update Trellis specs if the new service-asset contract is worth preserving.

## Requirements

* This is not a complete service registry. It is a manually maintained VPS-scoped asset note with optional Target correlation.
* DB/API values must use stable English machine values; UI labels may map to Chinese in `web/src/lib/types.ts`.
* Backend entry points must trim strings, normalize labels, validate enum values, validate `port` if present, and reject blank service names.
* `port` is optional. If present, it must be in `1..65535`.
* `target_id` may be blank/null; when non-empty, DB foreign key validation and store error mapping must expose invalid input or not-found semantics through HTTP.
* Service writes must not change VPS current state, subscriptions, experience logs, Node state, Target state, ProbeItem state, Agent state, or `nodes.provider`.
* All new routes must go through existing auth middleware; agent routes must remain unaffected.
* Frontend page code must not call `fetch` directly.
* Existing VPS detail behavior, timeline, experience logs, Node links, lifecycle/archive, and current-state edit flows must not regress.

## Acceptance Criteria

* [ ] Migration creates `asset_services` with foreign keys, enum checks, not-blank name check, optional valid port check, and useful indexes.
* [ ] Domain tests cover normalize/validate for blank names, enums, labels, optional Target, and port boundaries.
* [ ] Store tests cover create, list all, list by VPS, ordering, missing VPS, invalid Target, and no Node/Target mutation behavior.
* [ ] Handler tests cover collection GET/POST, VPS-subresource GET/POST, invalid JSON, invalid input, not found, method not allowed, and repository failure.
* [ ] Router tests prove `/api/services` and `/api/vps/{vps_id}/services` do not fall through to SPA or the VPS item handler.
* [ ] Bootstrap tests prove service handlers are explicitly wired.
* [ ] Frontend API tests cover service URLs and request bodies, especially path-scoped VPS create without `vps_id` override.
* [ ] `VPSDetailPage` test covers loading services, empty state, create happy path, and local validation failure.
* [ ] `./scripts/verify.sh` passes locally.
* [ ] Feature branch is pushed, PR is created, PR CI is green, PR is merged, post-merge main CI is green, and local `main` is synced.

## Out of Scope

* No real 40+ VPS JSON dry-run/import execution or production data import.
* No full service registry page or global service workbench.
* No domain management, DNS sync, provider sync, service discovery, Web SSH, plugins, RBAC, scoring, uptime SLA calculation, or Agent-side service logic.
* No changes to Target probe semantics or automatic Target creation.
* No changes to Dashboard asset summary in this task.
* No delete/archive/edit workflow for services in this MVP; create/list only.

## Technical Notes

* Current latest migration is `0022_create_experience_logs.sql`; this task should use `0023_create_asset_services.sql` unless another migration lands first.
* Backend pattern references:
  * `internal/center/providers/types.go`
  * `internal/center/store/providers.go`
  * `internal/center/http/handlers/providers.go`
  * `internal/center/renewals/types.go`
  * `internal/center/http/handlers/vps.go`
  * `internal/center/store/renewal_decisions.go`
* Frontend pattern references:
  * `web/src/lib/api.ts`
  * `web/src/lib/types.ts`
  * `web/src/pages/VPSDetailPage.tsx`
  * `web/src/pages/VPSDetailPage.test.tsx`
* Relevant specs are curated in `implement.jsonl` and `check.jsonl`.

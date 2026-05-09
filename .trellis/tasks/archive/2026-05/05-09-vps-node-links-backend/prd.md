# VPS Node links backend

## Goal

Implement the backend slice that connects VPS Asset Ledger records to Fleet Observability nodes through `vps_node_links`, while preserving the existing Node / Target / Agent semantics.

## What I already know

- The root plan defines Task 5 as `VPS 与 Node 关联`.
- Tasks 1-4 already delivered providers, VPS assets, subscriptions, and the JSON dry-run/import command.
- Current migrations end at `0018_add_subscriptions.sql`; the next schema migration is `0019_create_vps_node_links.sql`.
- Asset Ledger imports must not create node links. Node candidates from import remain human-confirmation signals.
- `nodes.provider` is still observability metadata and must not be synchronized from asset providers or VPS assets.
- Existing backend endpoints use domain types under `internal/center/<domain>/`, stores under `internal/center/store/`, handlers under `internal/center/http/handlers/`, explicit router fields, and bootstrap wiring.

## Scope

- Create the `vps_node_links` table and active-link indexes if missing.
- Add `internal/center/assetlinks/` for link domain types, repository interface, normalization, and sentinel errors.
- Add `internal/center/store/vps_node_links.go` with Postgres-backed link, unlink, and query behavior.
- Add link/query HTTP handlers:
  - `GET /api/vps/{vps_id}/nodes`
  - `POST /api/vps/{vps_id}/link-node`
  - `POST /api/vps/{vps_id}/unlink-node`
  - `GET /api/nodes/{node_id}/vps`
- Extend VPS item responses with active Node link summaries when the asset link repository is wired.
- Expose active Node link count on VPS asset list/item records.
- Wire handlers through `internal/center/http/router.go` and `cmd/houfeng-center/bootstrap.go`.
- Add focused domain, store, handler, router, bootstrap, and migration tests.
- Update backend Trellis specs with the new asset-link contract.

## Requirements

- Link writes must only insert into `vps_node_links`.
- Unlink must set `unlinked_at` and retain the historical row.
- A VPS/Node pair must not have more than one active link at the same time.
- Link and unlink must not modify:
  - `nodes.provider`
  - Node lifecycle status
  - Node monitoring status
  - Node health fields
  - Target or Agent state
- Link must reject missing/blank `node_id`.
- Link must report missing VPS and missing Node as not found.
- Duplicate active link must be a conflict.
- VPS-side query must return active Node health/monitoring summaries.
- Node-side query must return active VPS asset summaries.
- Route registration must remain protected by the existing auth middleware.

## Acceptance Criteria

- [ ] `POST /api/vps/{vps_id}/link-node` creates an active link and returns the link record.
- [ ] Repeating the same active link returns a conflict instead of creating a duplicate.
- [ ] `POST /api/vps/{vps_id}/unlink-node` sets `unlinked_at` and preserves history.
- [ ] `GET /api/vps/{vps_id}/nodes` returns active Node summaries including health and incident summary fields.
- [ ] `GET /api/nodes/{node_id}/vps` returns active VPS summaries.
- [ ] `GET /api/vps/{vps_id}` includes `node_links` when link wiring is present.
- [ ] VPS asset records include `active_node_link_count`.
- [ ] Store tests prove the repository does not update `nodes`.
- [ ] Router and bootstrap tests prove the new handlers are registered and protected.
- [ ] `git diff --check`, focused Go tests, and `make verify-go` pass.

## Out of Scope

- Frontend Providers/VPS/Subscriptions pages.
- Dashboard asset summary cards.
- JSON import creating links.
- Provider API auto-sync or Node provider backfill.
- Agent changes.
- Node lifecycle/monitoring/health transitions.
- Historical link browsing UI; history is retained in the database and store unlink behavior, but first API queries active links.

## Technical Notes

- Relevant specs:
  - `.trellis/spec/backend/directory-structure.md`
  - `.trellis/spec/backend/database-guidelines.md`
  - `.trellis/spec/backend/error-handling.md`
  - `.trellis/spec/backend/quality-guidelines.md`
  - `.trellis/spec/guides/cross-layer-thinking-guide.md`
  - `.trellis/spec/guides/branch-workflow-governance.md`
- Existing patterns inspected:
  - `internal/center/vpsassets/types.go`
  - `internal/center/store/vps_assets.go`
  - `internal/center/http/handlers/vps.go`
  - `internal/center/http/router.go`
  - `cmd/houfeng-center/bootstrap.go`
  - `internal/center/nodes/types.go`
  - `internal/center/store/nodes.go`
- The implementation will be done directly in the main session per the user instruction: no subagents.

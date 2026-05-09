# Asset Ledger schema and providers vertical slice

## Goal

Deliver the first post-V1 VPS Asset Ledger backend vertical slice: create the asset-ledger schema entry point and make providers usable through domain validation, PostgreSQL store, HTTP handlers, router registration, bootstrap wiring, and tests.

This task implements Task 1 from `houfeng_codex_下一步开发计划.md`. It must be a runnable vertical slice, not a directory skeleton.

## What I already know

* Current branch: `feat/asset-ledger-providers`.
* Current max migration before implementation is `db/migrations/0015_add_host_containers.sql`; use `db/migrations/0016_create_asset_ledger.sql` unless a newer migration appears before writing.
* Houfeng keeps the existing Fleet Observability model. `nodes`, `targets`, and agent behavior must not be repurposed for asset management.
* Asset Ledger is additive. This task only delivers providers plus the schema foundation needed for providers.
* `providers` is asset-layer service-provider master data. It must not automatically rewrite `nodes.provider`.
* Existing backend wiring goes through domain packages under `internal/center/*`, stores under `internal/center/store`, handlers under `internal/center/http/handlers`, `internal/center/http/router.go`, and `cmd/houfeng-center/bootstrap.go`.
* Existing protected `/api/*` routes are wrapped by the router's `AuthMiddleware`; `/api/agent/*`, `/api/auth/*`, and `/api/healthz` are not.

## Requirements

### Database

* Add a new migration using the next unused migration number. With the current repository state this should be:

  ```text
  db/migrations/0016_create_asset_ledger.sql
  ```

* Create `providers`:

  ```sql
  create table if not exists providers (
    provider_id text primary key,
    name text not null,
    website text not null default '',
    panel_url text not null default '',
    account_hint text not null default '',
    country text not null default '',
    note text not null default '',
    rating integer,
    labels text[] not null default '{}',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
  );
  ```

* Add database constraints for `name` and `rating`:
  * `name` must be non-empty after trimming.
  * `rating`, when present, must be between 1 and 5.
* Add provider-oriented indexes only when they serve first-stage list/detail usage.
* Do not create, modify, or backfill `nodes.provider`.
* Do not add delete behavior for providers in this task.

### Domain

* Add `internal/center/providers/` with provider types, repository interface, validation, request/update normalization helpers, and sentinel errors.
* Stable fields:
  * `provider_id`
  * `name`
  * `website`
  * `panel_url`
  * `account_hint`
  * `country`
  * `note`
  * `rating`
  * `labels`
  * `created_at`
  * `updated_at`
* Validation:
  * `name` required on create and cannot be blank on update.
  * `rating` must be nil or `1..5`.
  * string fields trim surrounding whitespace.
  * `labels` trim values and avoid storing empty label values.

### Store

* Add `internal/center/store/providers.go`.
* Implement create, list, get, and patch/update behavior for providers.
* Generate stable provider IDs in backend code, using existing ID conventions where possible.
* Map no-row cases to provider-domain not-found sentinel errors.
* Return stored `created_at` and `updated_at` values.
* Preserve unmentioned fields on PATCH.

### HTTP API

* Add provider handlers in `internal/center/http/handlers/`.
* Register handlers through `internal/center/http/router.go` and `cmd/houfeng-center/bootstrap.go`.
* Endpoints:

  ```text
  GET    /api/providers
  POST   /api/providers
  GET    /api/providers/{provider_id}
  PATCH  /api/providers/{provider_id}
  ```

* Behavior:
  * `GET /api/providers` lists providers.
  * `POST /api/providers` creates a provider and returns stable `provider_id`.
  * `GET /api/providers/{provider_id}` returns one provider.
  * `PATCH /api/providers/{provider_id}` updates supported fields and labels.
  * Empty `name` and invalid `rating` return `400`.
  * Unknown JSON fields return `400`.
  * Missing provider item returns `404`.
  * Unsupported methods return `405`.
  * Routes are protected by existing auth middleware when mounted in the router.
  * Agent routes remain unaffected.

### CI unblock allowance

If full `make verify-go` exposes an existing unrelated flaky Go test that would block PR merge, this task may include a minimal test-only stabilization fix when all of the following are true:

* the failure is reproducible under the full gate or reported by the check agent;
* focused provider checks pass;
* the fix does not change production runtime behavior;
* the fix is limited to deterministic test waiting/cancellation mechanics and is verified with focused reruns plus `make verify-go`.

## Acceptance Criteria

* [ ] Migration applies under the existing migration runner.
* [ ] `providers` table exists with non-empty-name and rating constraints.
* [ ] `POST /api/providers` can create a provider.
* [ ] `GET /api/providers` can list providers.
* [ ] `GET /api/providers/{provider_id}` can read a provider.
* [ ] `PATCH /api/providers/{provider_id}` can update provider fields and labels.
* [ ] Invalid input returns `400`.
* [ ] Missing provider item returns `404`.
* [ ] Provider API routes are auth-protected by the router.
* [ ] `/api/agent/*` behavior remains outside auth wrapping.
* [ ] Existing Node / Target / Agent behavior is not changed.
* [ ] Full Go CI gate is either green or any remaining failure is explicitly proven unrelated and non-blocking.

## Tests Required

* Migration test coverage for the new migration.
* Store tests for provider create/list/get/patch, invalid values, and not-found behavior.
* Handler tests for list/create/get/patch, invalid JSON/input, not-found, and method handling.
* Router tests proving provider collection/item routes are mounted and auth protected.
* Bootstrap/app wiring tests updated if compile-time or existing tests require it.

## Suggested Verification

```bash
git diff --check
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresProvider' -v
go test ./internal/center/http/handlers -run 'TestProviders' -v
go test ./internal/center/http -run 'TestRouter.*Provider|TestAuth' -v
make verify-go
```

## Out of Scope

* VPS asset CRUD.
* Subscriptions.
* VPS-to-Node links.
* Provider API auto-sync.
* DNS provider sync.
* Frontend pages and navigation.
* Dashboard asset summaries.
* Bulk JSON dry-run/import.
* Rewriting or normalizing existing `nodes.provider`.
* Production changes to agent runtime behavior. Test-only stabilization is allowed solely as a CI unblock per the section above.

## Technical Notes

* Source plan: `houfeng_codex_下一步开发计划.md`, Task 1.
* Keep API/DB machine values stable and English where new state-like values are introduced. This task has no lifecycle status enums.
* The first asset-ledger slice is intentionally small to keep the branch reviewable and CI feedback tight.
* Follow the repository branch flow: commit on feature branch, push branch, open PR, monitor CI, merge only when green, then update local `main`.

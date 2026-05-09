# VPS assets backend

## Goal

Deliver Task 2 from `houfeng_codex_下一步开发计划.md`: the backend VPS asset CRUD slice for the post-V1 Asset Ledger. This task should make real VPS assets creatable, readable, patchable, and list-filterable through the backend while preserving existing Node / Target / Agent semantics.

This task builds directly on PR #4 / migration `0016_create_asset_ledger.sql`, which added the `providers` table and provider API.

## What I already know

* Current branch: `feat/vps-assets-backend`.
* Current main is clean and synced to `origin/main` at `7bfc7c2 feat: add asset ledger providers api (#4)`.
* No active Trellis task remained after Task 1; PR #4 was merged with `GitGuardian Security Checks`, `go`, and `web` passing.
* Current max migration is `db/migrations/0016_create_asset_ledger.sql`; use `db/migrations/0017_add_vps_assets.sql` unless another migration lands first.
* `providers` already exists and is asset-layer service-provider master data.
* `nodes.provider` remains Fleet Observability node metadata and must not be rewritten by VPS asset writes.
* Existing provider code gives the local pattern for domain package, PostgreSQL store, HTTP handlers, router/bootstrap wiring, and tests.

## Scope

### Database

Add `vps_assets` in a new migration:

```sql
create table if not exists vps_assets (
  vps_id text primary key,
  display_name text not null,
  provider_id text references providers(provider_id) on delete set null,
  provider_name text not null default '',
  product_name text not null default '',
  order_ref text not null default '',
  country text not null default '',
  region text not null default '',
  city text not null default '',
  datacenter text not null default '',
  ipv4 text not null default '',
  ipv6 text not null default '',
  ssh_host text not null default '',
  ssh_port integer not null default 22,
  ssh_user text not null default '',
  os_name text not null default '',
  virtualization text not null default '',
  lifecycle_status text not null,
  usage_status text not null,
  renewal_decision text not null default 'unreviewed',
  importance text not null default 'normal',
  labels text[] not null default '{}',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  archived_at timestamptz
);
```

Required constraints:

* `display_name` must be non-empty after trimming.
* `lifecycle_status` must be one of:
  * `active`
  * `idle`
  * `testing`
  * `to_migrate`
  * `to_cancel`
  * `cancelled`
  * `archived`
* `usage_status` must be one of:
  * `in_use`
  * `idle`
  * `standby`
  * `testing`
  * `unknown`
* `renewal_decision` must be one of:
  * `unreviewed`
  * `keep`
  * `observe`
  * `migrate`
  * `cancel`
  * `auto_renew_cancelled`
  * `replaced`
* `ssh_port` must be between 1 and 65535.
* `provider_id`, when present, references an existing provider.

Required indexes:

* `idx_vps_assets_provider` on `provider_id`.
* `idx_vps_assets_status` on `(lifecycle_status, usage_status, renewal_decision)`.
* `idx_vps_assets_location` on `(country, region, city)`.

### Domain

Add `internal/center/vpsassets/` with:

* stable API/domain record type;
* `Repository` interface;
* create input;
* PATCH input with field-presence semantics;
* list filters;
* sentinel errors for not found and invalid input;
* enum constants and validation helpers.

Validation rules:

* `display_name` required on create and cannot be blank on patch.
* `lifecycle_status`, `usage_status`, and `renewal_decision` must be stable English machine values from the lists above.
* `provider_id` may be empty/nil, but if supplied must refer to an existing provider.
* `provider_name` is an import/display compatibility string; it does not create or mutate providers.
* string fields trim surrounding whitespace.
* labels trim values and drop empty duplicate labels, matching provider behavior.
* `ssh_port` defaults to 22 when omitted/zero on create, and must be `1..65535` when explicitly patched.
* `archived_at` is derived by lifecycle semantics in this task: when setting lifecycle to `archived`, set it if empty; when moving away from `archived`, clear it. Do not expose arbitrary `archived_at` writes from API input.

### Store

Add `internal/center/store/vps_assets.go` with PostgreSQL implementation:

* generate IDs with prefix `vps`;
* create VPS asset;
* list VPS assets with optional filters:
  * `provider_id`
  * `lifecycle_status`
  * `usage_status`
  * `renewal_decision`
* get VPS asset by `vps_id`;
* patch supported fields while preserving unmentioned fields;
* map no rows to `vpsassets.ErrVPSAssetNotFound`;
* map provider foreign-key violations to `vpsassets.ErrInvalidVPSAssetInput`;
* return stored `created_at`, `updated_at`, and `archived_at`.

### HTTP API

Add handlers and route/bootstrap wiring for:

```text
GET    /api/vps
POST   /api/vps
GET    /api/vps/{vps_id}
PATCH  /api/vps/{vps_id}
```

Behavior:

* `GET /api/vps` lists assets and supports query filters:
  * `provider_id`
  * `lifecycle_status`
  * `usage_status`
  * `renewal_decision`
* `POST /api/vps` creates an asset.
* `GET /api/vps/{vps_id}` returns one asset.
* `PATCH /api/vps/{vps_id}` updates supported fields and labels.
* Invalid enum, blank display name, invalid SSH port, or missing provider returns `400`.
* Missing VPS item returns `404`.
* Unknown JSON fields return `400`.
* Unsupported methods return `405`.
* Routes are protected by the existing auth middleware.
* Agent routes remain unaffected.

### DTO boundary for later subscriptions / links

The plan asks the first VPS list to return subscription summary and active Node link count. This task does not implement `subscriptions` or `vps_node_links`, so it must not fake those aggregates.

Acceptable behavior for this task:

* return core VPS asset fields only; or
* include clearly nullable/zero-value optional summary fields that are not populated until Task 3 / Task 5.

Do not silently query non-existent tables, hard-code fake counts, or create placeholder subscriptions/link tables in this task.

## Acceptance Criteria

* [ ] `db/migrations/0017_add_vps_assets.sql` or the next unused migration creates `vps_assets`.
* [ ] Migration runner sees the new migration after `0016_create_asset_ledger.sql`.
* [ ] `POST /api/vps` can create a VPS asset.
* [ ] `GET /api/vps` can list VPS assets.
* [ ] `GET /api/vps/{vps_id}` can read a VPS asset.
* [ ] `PATCH /api/vps/{vps_id}` can update supported fields while preserving unmentioned values.
* [ ] List filters work for provider and the three status fields.
* [ ] Invalid status values, blank display names, invalid SSH ports, and missing provider references return `400`.
* [ ] Missing VPS item returns `404`.
* [ ] API returns stable English machine status values; UI Chinese labels are not introduced in backend code.
* [ ] Creating/updating VPS assets does not rewrite `nodes.provider` and does not change Node / Target / Agent behavior.
* [ ] Routes are protected by existing auth middleware.

## Tests Required

* Domain tests for enum validation, normalization, labels, SSH port default/range, PATCH presence semantics, and archived lifecycle semantics.
* Migration test coverage for `vps_assets` table, constraints, indexes, and migration ordering.
* Store tests for create/list/get/patch, filters, not-found, invalid enum, invalid provider FK mapping, and archived lifecycle persistence.
* Handler tests for collection/item methods, invalid JSON/unknown fields, invalid filters, invalid create/patch, not-found, and method not allowed.
* Router tests proving `/api/vps` and `/api/vps/{vps_id}` are mounted, auth-protected, and do not fall through to SPA.
* Bootstrap test update ensuring VPS handlers are wired.

## Suggested Verification

```bash
git diff --check
go test ./internal/center/vpsassets -v
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresVPSAsset' -v
go test ./internal/center/http/handlers -run 'TestVPS' -v
go test ./internal/center/http -run 'TestRouter.*VPS|TestAuth' -v
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go
```

## Out of Scope

* Subscriptions table or `/api/subscriptions`.
* VPS-to-Node link table or link/unlink APIs.
* Real subscription summaries or active Node link count aggregation.
* Frontend pages, navigation, or Chinese label mapping.
* Dashboard asset summaries.
* JSON dry-run/import.
* Provider API auto-sync.
* Rewriting or normalizing `nodes.provider`.

## Technical Notes

* Source plan: `houfeng_codex_下一步开发计划.md`, Task 2.
* Reuse the provider slice architecture from PR #4.
* Keep Asset Ledger fields additive and separate from Fleet Observability.
* Complete this branch with the established workflow: commit on feature branch, archive/journal, push PR, monitor CI, merge only when green, then sync local `main`.

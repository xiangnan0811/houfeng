# Subscriptions backend

## Goal

Deliver Task 3 from `houfeng_codex_下一步开发计划.md`: the backend subscription slice for the post-V1 VPS Asset Ledger. This task should make VPS subscriptions creatable, readable, patchable, list-filterable, and renewal-queryable through the backend while preserving existing VPS asset, Provider, Node, Target, and Agent semantics.

Task 3 builds directly on:

- PR #4 / `db/migrations/0016_create_asset_ledger.sql`, which added `providers`.
- PR #5 / `db/migrations/0017_add_vps_assets.sql`, which added `vps_assets`.
- PR #6, which stabilized an unrelated runtime test flake and left `main` green.

## What I Already Know

- Current branch: `feat/subscriptions-backend`.
- Current `main` is clean and synced to `origin/main` at `ed7d5f1 fix: stabilize runtime action sync tests (#6)`.
- Current max migration is `db/migrations/0017_add_vps_assets.sql`; use `db/migrations/0018_add_subscriptions.sql` unless another migration lands first.
- `providers` and `vps_assets` already exist and are independent Asset Ledger aggregates.
- `nodes.provider` remains Fleet Observability node metadata and must not be rewritten by subscription writes.
- Task 3 is backend-only. Frontend pages, import, Dashboard summaries, and VPS-to-Node links remain later tasks.
- Existing provider and VPS slices provide the implementation pattern for domain, store, handler, router/bootstrap wiring, and tests.

## Scope

### Database

Add `subscriptions` in a new migration:

```sql
create table if not exists subscriptions (
  subscription_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  price numeric(12, 2) not null,
  currency text not null,
  billing_cycle text not null default '',
  billing_months integer not null,
  monthly_price numeric(12, 4) not null,
  started_at date,
  renew_at date,
  auto_renew boolean not null default false,
  auto_renew_cancelled boolean not null default false,
  status text not null default 'active',
  payment_method text not null default '',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
```

Required constraints:

- `price >= 0`.
- `billing_months > 0`.
- `currency` must be non-empty after trimming and should be normalized to an uppercase 3-character code in the domain layer.
- `status` must be one of:
  - `active`
  - `paused`
  - `cancelled`
  - `expired`
  - `unknown`
- `monthly_price` must be produced by backend calculation: `price / billing_months`. API input must not be able to set it directly.
- `vps_id` must reference an existing VPS asset.

Required indexes:

- `idx_subscriptions_vps` on `vps_id`.
- `idx_subscriptions_renew_at` on `renew_at`.
- `idx_subscriptions_status` on `status`.

### Domain

Add `internal/center/subscriptions/` with:

- stable API/domain record type;
- `Repository` interface;
- create input;
- PATCH input with field-presence semantics;
- list filters / options;
- sentinel errors for not found and invalid input;
- status constants and validation helpers;
- currency normalization and monthly-price calculation helpers.

Validation rules:

- `vps_id` is required on create.
- `status` defaults to `active` on create when omitted and must be a stable English machine value.
- `price` must be non-negative.
- `billing_months` must be greater than zero.
- `currency` must be trimmed, uppercase, and a 3-letter code.
- `billing_cycle`, `payment_method`, and `note` trim surrounding whitespace.
- `monthly_price` is derived by backend code and is not accepted in create or patch JSON.
- PATCH must recompute `monthly_price` whenever `price` or `billing_months` changes.
- Date fields use nullable `date` semantics; unknown date is null, not a fake date.

### Store

Add `internal/center/store/subscriptions.go` with PostgreSQL implementation:

- generate IDs with prefix `sub`;
- create subscription;
- list subscriptions with optional filters:
  - `vps_id`
  - `status`
  - `renew_before`
  - `renew_after`
  - `renew_within_days`
- support `renew_at` ordering, at minimum ascending;
- get subscription by `subscription_id`;
- patch supported fields while preserving unmentioned fields;
- recalculate `monthly_price` on create and on price/month patch;
- map no rows to `subscriptions.ErrSubscriptionNotFound`;
- map VPS foreign-key violations to `subscriptions.ErrInvalidSubscriptionInput`;
- return stored `created_at` and `updated_at` values.

### HTTP API

Add handlers and route/bootstrap wiring for:

```text
GET    /api/subscriptions
POST   /api/subscriptions
GET    /api/subscriptions/{subscription_id}
PATCH  /api/subscriptions/{subscription_id}
```

Behavior:

- `GET /api/subscriptions` lists subscriptions.
- Query filters:
  - `vps_id`
  - `status`
  - `renew_before`
  - `renew_after`
  - `renew_within_days`
  - `sort=renew_at`
  - `order=asc|desc` if practical; otherwise document and test ascending behavior.
- `POST /api/subscriptions` creates a subscription.
- `GET /api/subscriptions/{subscription_id}` returns one subscription.
- `PATCH /api/subscriptions/{subscription_id}` updates supported fields.
- Invalid amount, period, currency, status, dates, or missing VPS reference returns `400`.
- Missing subscription returns `404`.
- Unknown JSON fields return `400`.
- Unsupported methods return `405`.
- Routes are protected by the existing auth middleware.
- Agent routes remain unaffected.

### DTO boundary for later tasks

This task does not implement frontend or dashboard surfaces, but it must return enough stable backend data for later Task 4 import, Task 5 links, and later UI work.

Acceptable optional behavior:

- `GET /api/vps/{vps_id}` may remain unchanged.
- `GET /api/vps/{vps_id}` may include a real subscription summary only if it can be implemented without widening scope or faking data.

Do not:

- fake subscription summaries in VPS responses;
- create `vps_node_links`;
- implement Node-link summaries;
- introduce currency exchange or cross-currency totals.

## Acceptance Criteria

- [ ] `db/migrations/0018_add_subscriptions.sql` or the next unused migration creates `subscriptions`.
- [ ] Migration runner sees the new migration after `0017_add_vps_assets.sql`.
- [ ] `POST /api/subscriptions` can create a subscription for an existing VPS asset.
- [ ] `GET /api/subscriptions` can list subscriptions.
- [ ] `GET /api/subscriptions/{subscription_id}` can read a subscription.
- [ ] `PATCH /api/subscriptions/{subscription_id}` can update supported fields while preserving unmentioned values.
- [ ] `monthly_price` is calculated by the backend on create and recalculated after price or billing-month changes.
- [ ] Invalid status, negative price, zero/negative billing months, invalid currency, invalid date filters, and missing VPS references return `400`.
- [ ] Missing subscription item returns `404`.
- [ ] List filters work for `vps_id`, status, and renewal windows.
- [ ] API can query future 30-day renewal candidates.
- [ ] API returns stable English machine status values; UI Chinese labels are not introduced in backend code.
- [ ] Creating/updating subscriptions does not rewrite `nodes.provider` and does not change Node / Target / Agent behavior.
- [ ] Routes are protected by existing auth middleware.

## Tests Required

- Domain tests for status validation, currency normalization, amount/month validation, monthly-price calculation, string trimming, nullable dates, and PATCH presence semantics.
- Migration test coverage for `subscriptions` table, constraints, indexes, and migration ordering.
- Store tests for create/list/get/patch, filters, renewal ordering/windows, not-found, invalid status, invalid VPS FK mapping, and monthly-price recalculation.
- Handler tests for collection/item methods, invalid JSON/unknown fields, invalid filters, invalid create/patch, not-found, and method not allowed.
- Router tests proving `/api/subscriptions` and `/api/subscriptions/{subscription_id}` are mounted, auth-protected, and do not fall through to SPA.
- Bootstrap test update ensuring subscription handlers are wired.

## Suggested Verification

```bash
git diff --check
go test ./internal/center/subscriptions -v
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresSubscription' -v
go test ./internal/center/http/handlers -run 'TestSubscriptions' -v
go test ./internal/center/http -run 'TestRouter.*Subscription|TestAuth' -v
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go
```

## Out of Scope

- VPS-to-Node link table or link/unlink APIs.
- Node-side VPS summary.
- Frontend pages, navigation, or Chinese label mapping.
- Dashboard asset summaries.
- JSON dry-run/import.
- Provider API auto-sync.
- Rewriting or normalizing `nodes.provider`.
- Price history, renewal decision history, or experience logs.
- Currency exchange or unified multi-currency cost totals.
- Agent runtime behavior changes.

## Technical Notes

- Source plan: `houfeng_codex_下一步开发计划.md`, Task 3.
- Reuse the provider and VPS asset slice architecture from PR #4 and PR #5.
- Keep Asset Ledger fields additive and separate from Fleet Observability.
- Complete this branch with the established workflow: commit on feature branch, archive/journal, push PR, monitor CI, merge only when green, then sync local `main` and monitor post-merge `main` CI.

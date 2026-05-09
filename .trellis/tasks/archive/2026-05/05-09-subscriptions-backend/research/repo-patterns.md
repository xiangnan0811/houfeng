# Subscriptions backend repo patterns

## Relevant existing slices

- Providers slice:
  - `db/migrations/0016_create_asset_ledger.sql`
  - `internal/center/providers/`
  - `internal/center/store/providers.go`
  - `internal/center/http/handlers/providers.go`
  - router/bootstrap tests
- VPS assets slice:
  - `db/migrations/0017_add_vps_assets.sql`
  - `internal/center/vpsassets/`
  - `internal/center/store/vps_assets.go`
  - `internal/center/http/handlers/vps.go`
  - router/bootstrap tests

Task 3 should copy the architecture, not the code blindly:

1. Domain package owns validation, normalization, patch presence, and sentinel errors.
2. Store package owns handwritten SQL and maps pgx/postgres errors to domain sentinels.
3. Handler parses/validates JSON and query strings, translates domain errors to HTTP status, and never exposes raw DB errors.
4. Router and bootstrap wiring remain explicit.
5. Tests cover domain, migration, store, handler, router, and bootstrap.

## Migration

Current max migration after Task 2 is `0017_add_vps_assets.sql`; Task 3 should use `0018_add_subscriptions.sql` unless another migration lands first.

Use idempotent DDL:

- `create table if not exists subscriptions (...)`
- `create index if not exists ...`

Do not edit existing migrations.

## Product boundaries

- Subscription belongs to a VPS asset via `vps_id`.
- It does not rewrite providers, `nodes.provider`, Node lifecycle, Target state, or Agent behavior.
- It does not create VPS-to-Node links.
- `monthly_price` is a backend-derived field.
- Keep currency handling limited to code normalization and original-currency monthly price; no exchange rates in this task.

## Verification

Use repo-local Go temp dirs:

```bash
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go
```

Focused checks:

```bash
go test ./internal/center/subscriptions -v
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresSubscription' -v
go test ./internal/center/http/handlers -run 'TestSubscriptions' -v
go test ./internal/center/http -run 'TestRouter.*Subscription|TestAuth' -v
```

# Repo patterns for VPS assets backend

## Prior slice

PR #4 added the Asset Ledger provider slice and should be the implementation template:

* domain package: `internal/center/providers`
* store file: `internal/center/store/providers.go`
* handlers: `internal/center/http/handlers/providers.go`
* router fields and registrations: `internal/center/http/router.go`
* bootstrap wiring: `cmd/houfeng-center/bootstrap.go`
* migration: `db/migrations/0016_create_asset_ledger.sql`

## Task 2 mapping

The matching VPS asset slice should add:

* `internal/center/vpsassets`
* `internal/center/store/vps_assets.go`
* `internal/center/http/handlers/vps_assets.go` or `vps.go` following existing handler naming conventions
* `RouterOptions` fields for VPS collection and item handlers
* bootstrap repository construction and handler wiring
* `db/migrations/0017_add_vps_assets.sql` unless a newer migration appears first

## Boundary decisions

Subscriptions and Node links do not exist yet. This task must not fake subscription summary or active Node link counts. Those belong to later Task 3 and Task 5. The VPS API should return core VPS asset fields and can leave room for future summary DTOs only if the absence is explicit.

`nodes.provider` remains independent monitoring metadata. VPS asset create/patch must not update it.

## Verification notes

Use repo-local Go temp dirs for full Go verification:

```bash
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go
```

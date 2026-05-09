# Repo patterns for provider vertical slice

## Scope

This file records local code patterns discovered before implementing Asset Ledger providers. It is intentionally repo-local research, not external market research.

## Existing backend structure

* Domain packages live under `internal/center/<domain>/`, for example `settings`, `nodes`, `targets`, `auth`.
* PostgreSQL repositories live in `internal/center/store/` and are constructed from `*pgxpool.Pool` in `cmd/houfeng-center/bootstrap.go`.
* HTTP handlers live under `internal/center/http/handlers/`.
* Router registration is centralized in `internal/center/http/router.go` using `RouterOptions`.
* Protected browser/API routes are wrapped with `AuthMiddleware`; agent sync/enroll routes and auth routes are intentionally not wrapped.

## Implementation implications

* Providers should use a dedicated domain package: `internal/center/providers`.
* The store should be `internal/center/store/providers.go` and expose `NewPostgresProviderRepository`.
* The handler constructor should live in `internal/center/http/handlers/providers.go`.
* Router options need collection and item handlers for `/api/providers` and `/api/providers/`.
* Bootstrap should instantiate the provider repository and pass both handlers into `RouterOptions`.

## Verification implications

* Store tests should follow existing PostgreSQL integration-test patterns in `internal/center/store/*_test.go`.
* Handler tests should use local fake repositories, mirroring existing handler package tests.
* Router tests should verify route mounting and auth wrapping at the router layer, not only handler behavior.

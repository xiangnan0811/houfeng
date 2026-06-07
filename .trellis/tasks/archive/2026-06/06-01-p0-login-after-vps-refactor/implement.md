# Implementation Plan

## Steps

1. Add focused auth service tests:
   - unknown user remains `ErrInvalidCredentials`;
   - wrong password remains `ErrInvalidCredentials`;
   - non-not-found repository error propagates as an internal error.
2. Change `auth.Service.Login` to only collapse `ErrUserNotFound` into invalid credentials.
3. Add/adjust handler tests proving internal login failures return 500 `login failed`, while wrong credentials remain 401.
4. Add an upgrade-preservation test around a persisted bcrypt user row if feasible in existing store test patterns.
5. Run targeted tests:
   - `go test ./internal/center/auth ./internal/center/http/handlers -run 'TestServiceLogin|TestLoginHandler'`
   - store/bootstrap targeted auth tests if added.
6. Run `make verify-go`.
7. Commit, push, open PR, monitor CI, merge, and monitor Release Please plus Docker image publication for a patch release.

## Rollback Points

- If login propagation changes expose too much detail to users, keep the handler response generic 500 while retaining server-side wrapped errors.
- If store integration coverage requires a real PostgreSQL fixture that is too heavy for the patch, keep repository-level tests and add a separate gated Postgres test only when reliable.

## Verification Notes

The real deployment symptom should change in one of two ways:

- Existing password works again if the prior 401 was caused by a masked repository problem now fixed or avoided.
- If a database/schema problem remains, the UI/server response becomes an internal login failure instead of misleading wrong-password semantics, and logs preserve the wrapped cause.


## Implementation Notes

- `auth.Service.Login` now only maps `ErrUserNotFound` to `ErrInvalidCredentials`; repository/query/scan errors propagate with `find user by username` context.
- The login HTTP handler already maps non-credential failures to generic 500 `login failed`; a regression test now locks that behavior.
- `LoginPage` now shows `用户名或密码不正确` only for `ApiError(401)` and shows a service-failure message for non-401 errors, so backend/DB failures are no longer presented as bad credentials.
- Seed coverage now asserts an existing admin bcrypt hash is not overwritten by bootstrap initialization.
- PostgreSQL integration coverage now simulates an upgraded deployment with an existing `admin` row and bcrypt hash, runs current migration/bootstrap behavior, and verifies the old password still logs in while the seed password does not replace it.

## Verification Results

- `go test ./internal/center/auth ./internal/center/http/handlers -run 'TestServiceLogin|TestSeedInitialUser|TestLoginHandler'`: passed.
- `npm run test -- --run LoginPage auth-client`: passed.
- `go test ./internal/center/store/migrate -run 'TestNames|TestPostgresIntegration'`: passed with integration tests skipped by default when `HOUFENG_POSTGRES_INTEGRATION` is unset.
- `HOUFENG_POSTGRES_INTEGRATION=1 HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@192.168.100.192:5432/houfeng?sslmode=disable' go test ./internal/center/store/migrate -run 'TestPostgresIntegrationUpgradePreservesExistingLogin|TestPostgresIntegrationVPSFirstUpgradeNormalizesLegacyState' -count=1`: passed.
- `make verify-go`: passed.
- `cd web && npm run lint`: passed.
- `cd web && npm run build`: passed.
- `git diff --check`: passed.

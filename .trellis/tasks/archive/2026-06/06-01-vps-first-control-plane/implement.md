# Implementation Plan

## Order

1. Read backend/web specs and inspect existing contracts.
2. Add migration/comments and backend domain types for VPS-scoped monitoring/subscription creation.
3. Implement handlers, router wiring, bootstrap wiring, API client functions, and backend tests.
4. Refactor `VPSCreateModal`, `VPSDetailPage`, subscription forms, monitoring creation entrypoints, and related types/tests.
5. Update `CLAUDE.md` and active specs/docs for VPS-first semantics.
6. Run targeted tests, fix failures, then run full verification and browser sanity.

## Validation

- `go test ./internal/center/http/handlers -run 'TestVPS|TestMonitoring|TestSubscription|TestAsset'`
- `go test ./internal/center/store -run 'TestPostgresVPS|TestPostgresMonitoring|TestPostgresSubscription|TestPostgresAsset'`
- `cd web && npm run test -- --run VPSPage VPSDetailPage MonitoringPage SubscriptionsPage VPSCreateModal`
- `make verify-go`
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- `git diff --check`

## Risk Points

- Router path ordering under `/api/vps/{id}` must not shadow existing subresources.
- MonitoringInstance creation still needs required fields for current store validation; derived VPS fields must satisfy those requirements.
- Subscription status can remain in backend for compatibility, but new UI must not ask users to manage it.
- Existing cancellation workbench tests expect status-split language and will need intentional updates, not blind snapshots.

## Final Review 2026-06-01

- Removed the remaining ordinary MonitoringInstance lifecycle control surface after review:
  - public `/api/monitoring-instances/{id}/lifecycle/*` routing and bootstrap wiring are gone;
  - frontend Monitoring list/detail menus no longer expose `退役监控实例` / `恢复到观察中`;
  - store-level standalone retire/restore repository methods and tests were removed so the only lifecycle write path is the VPS `asset_lifecycle` coordination path.
- Added real PostgreSQL integration coverage for the VPS-first migration:
  - fresh isolated database migration test is gated and skips when the supplied PostgreSQL user lacks `CREATEDB`;
  - temp-schema upgrade test seeds legacy subscription / monitoring lifecycle rows, runs `0030_vps_first_status_semantics.sql` twice, and verifies normalized VPS business state plus audit events.
- Real test PostgreSQL evidence with `HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@192.168.100.192:5432/houfeng?sslmode=disable`:
  - `TestPostgresIntegrationVPSFirstUpgradeNormalizesLegacyState` passed;
  - `TestPostgresIntegrationAppliesFreshMigrations` skipped because the account cannot create databases.
- Browser sanity used a local center against the supplied PostgreSQL on `127.0.0.1:18080`:
  - login, `/vps`, `/subscriptions`, `/monitoring`, existing monitoring detail, freshly created VPS detail, VPS-scoped subscription create, VPS-scoped monitoring instance create, and created monitoring detail passed;
  - freshly created VPS detail showed quick subscription / agent entry points and did not surface the cancel / retire workbench as a primary danger CTA;
  - temporary provider / VPS / subscription / monitoring instance / session / test user rows were cleaned from PostgreSQL after the run.
- Verification passed:
  - `go test ./internal/center/store -run 'TestPostgresVPS|TestPostgresMonitoring|TestPostgresSubscription|TestPostgresAsset|TestMonitoringInstanceLifecycle|TestAssetLifecycle|TestMonitoringInstanceRuntime'`
  - `go test ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center -run 'TestMonitoring|TestRouter|TestBootstrap|TestRuntime|TestVPS|TestSubscription|TestAsset'`
  - `HOUFENG_POSTGRES_INTEGRATION=1 ... go test ./internal/center/store/migrate -run 'TestPostgresIntegration' -count=1 -v`
  - `make verify-go`
  - `cd web && npm run lint`
  - `cd web && npm run test -- --run VPSPage VPSDetailPage MonitoringPage MonitoringDetailPage MonitoringComparePage SubscriptionsPage VPSCreateModal api`
  - `cd web && npm run test -- --run`
  - `cd web && npm run build`
  - `git diff --check`

## Monitoring Page Follow-up 2026-06-01

- User browser check found the ordinary `/monitoring` page still exposed an independent MonitoringInstance creation drawer with repeated identity/location fields.
- Fixed by removing the `MonitoringPage` standalone create state, submit path, and drawer component; the page header now links to `/vps` with `从 VPS 接入 agent`, and the empty state routes to VPS inventory with `创建第一台 VPS`.
- Added regression tests that `/monitoring` no longer shows `高级创建`, no longer opens `高级创建监控实例表单`, and only reads `/api/monitoring-instances` instead of posting a standalone create request from the monitoring page.

## Pre-Delivery Hardening 2026-06-01

- A second residual-flow review found two more VPS-first UX drifts:
  - the VPS monitoring evidence drawer used a misleading `创建并接入 agent` action for the existing-instance link flow;
  - fresh active VPS records could still expose `取消/退役工作台` inside the hero overflow menu.
- Fixed by splitting the monitoring evidence drawer into a primary VPS-scoped create action and secondary existing-instance link action, and by only showing the cancellation workbench menu item for migrate/cancel/cancelled/archived/conflict states.
- Added regression assertions for both fixes.
- Re-ran final checks:
  - `cd web && npm run lint`
  - `cd web && npm run test -- --run VPSPage VPSDetailPage MonitoringPage MonitoringDetailPage SubscriptionsPage AssetDecisionsPage DashboardPage api`
  - `cd web && npm run test -- --run`
  - `cd web && npm run build`
  - `go test ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center -run 'TestMonitoring|TestRouter|TestBootstrap|TestRuntime|TestVPS|TestSubscription|TestAsset'`
  - `go test ./internal/center/store -run 'TestPostgresVPS|TestPostgresMonitoring|TestPostgresSubscription|TestPostgresAsset|TestMonitoringInstanceLifecycle|TestAssetLifecycle|TestMonitoringInstanceRuntime'`
  - `make verify-go`
  - `git diff --check`

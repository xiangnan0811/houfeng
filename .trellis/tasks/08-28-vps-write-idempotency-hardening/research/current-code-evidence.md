# v0.77.6 current-code evidence

## Review anchor

- Release tag/commit: `v0.77.6` / `da83a96769b618c6e223f71a1d2c6645d54c853b`.
- Source review supplied by the user: `/home/murray/.codex/attachments/dd4d79cb-4d44-402f-a882-5cd944372b6e/pasted-text.txt`.
- Scope: P2-01 route/view owner lifetime; P2-02 unknown-result retry for four VPS scoped creates; P3-01 nullable string/date mirror inference.

## P2-01 evidence

- `web/src/app/layout/AppShell.tsx:17-34,120-138` is the authenticated route shell and owns the persistent `<Outlet />`; it currently has no VPS write provider.
- `web/src/pages/VPSDetailPage.tsx:50-58` creates `legacyWriteOwnerStore` inside the route instance.
- `web/src/pages/VPSDetailPage.tsx:210-221` passes that store only to Legacy; the Overview route receives neither store nor common authority.
- `web/src/pages/VPSDetailPage.tsx:274-291` mounts `VPSOverviewManagementActions` without owner props/context.
- `web/src/pages/vps-detail/VPSOverviewManagementActions.tsx:100-109,135-146,282-297` uses component-local `submitting`/`submissionLockRef`, resets it on route identity/unmount, and therefore cannot observe a pending Legacy request.
- `web/src/pages/vps-detail/vpsWriteOwnerStore.ts:15-29` stores only view-local owner metadata; it has no `startedAt`, `viewToken`, request digest or idempotency attempt lifecycle.
- `web/src/pages/vps-detail/LegacyVPSDetail.tsx:262-266,340-360` already subscribes to the store and uses exact token release; this is the mechanism to preserve while lifting ownership.

## P2-02 evidence

- `web/src/lib/api.ts:664-715` sends ordinary POST for experience/service/domain/monitoring creates and exposes no caller-owned key parameter.
- `web/src/pages/vps-detail/LegacyVPSDetail.tsx:1461-1553` releases each create owner in `finally`; after a rejected fetch the user may retry, but these operations have no durable key.
- `internal/center/http/handlers/vps.go:380-435`, `asset_services.go:60-103`, `asset_domains.go:60-103`, and `asset_links.go:34-106` decode/validate and call direct create methods without key validation or replay response.
- `internal/center/store/subscriptions.go:236-309` is the current authoritative pattern: normalized input/key/digest, advisory transaction lock, receipt lookup, same-digest replay, different-digest sentinel, result+receipt in one transaction.
- `db/migrations/0061_create_subscription_create_idempotency.sql:1-17` is released and immutable. New receipt schema must be an additive `0062` successor.
- Monitoring’s `PostgresMonitoringInstanceRepository.CreateLinkedMonitoringInstance` already owns the instance+link transaction; its idempotent path must add receipt to that same transaction rather than wrapping it outside.

## P3-01 evidence

- `web/src/lib/vpsSubscriptionCreateContract.test.ts:337-381` reduces unions to a primitive and returns `date` whenever the primitive is string and nullable.
- `internal/center/http/handlers/vps_subscription_create_contract_test.go:1194-1231` repeats the same inference in the Go-side TypeScript source mirror.
- `internal/center/http/handlers/vps_subscription_create_fields.json:8-9` currently models dates as a distinct base type instead of string + format.
- `web/src/lib/types.ts:2210-2223` declares scoped dates as raw `string | null`; there is no explicit date alias for the mirror to verify.
- Both mirrors already contain extensive fail-closed parsing negatives; the change must extend those parsers without weakening missing tag, anonymous embedding, alias widening, continuation or manifest-presence checks.

## Verification boundary

- Project gates: `make verify-go`, Node 22 `make verify-web`, Trellis check, and relevant Chromium route tests.
- The supplied review notes a pre-existing local attachment PNG golden digest mismatch despite green release CI. If reproduced, record exact output and separate it from task-caused failures.
- Missing required PostgreSQL DSN/fixture is a blocked integration gate, not a skipped pass and not authorization to create/replace infrastructure.
- Final handoff is deliberately uncommitted: no stage, commit, push, PR, merge, release or archive.

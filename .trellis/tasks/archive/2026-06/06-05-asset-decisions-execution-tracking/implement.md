# Implementation Plan

## Steps

1. Read relevant Trellis specs for backend, web, and shared guides.
2. Add migration `0036_add_asset_decision_member_followups.sql`.
3. Extend backend domain types, normalization, validation, and repository contract for follow-up status.
4. Update Postgres store queries, scans, create defaults, patch transaction behavior, and tests.
5. Update HTTP handler patch request decoding and handler tests.
6. Update frontend API/types/tests.
7. Update `AssetDecisionsPage` UI, local state, member follow-up patch action, CSS, tests, and visual fixture.
8. Run backend and frontend verification.
9. Update Trellis spec only if a durable convention or contract changed.
10. Commit, push branch, open PR, monitor CI, merge when green, then monitor post-merge release/automation if triggered.

## Validation Commands

Backend:

```bash
go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http/handlers ./internal/center/http/router ./internal/center/bootstrap
go test ./...
```

Frontend:

```bash
cd web && npm ci
cd web && npm run test -- AssetDecisionsPage api
cd web && npm run test
cd web && npm run build
```

Visual sanity:

```bash
cd web && npm run dev -- --host 127.0.0.1
```

Then inspect `/asset-decisions?view=needs_decision&renew_within_days=30` in the in-app browser on desktop and mobile widths if the page is reachable locally.

## Rollback Points

- Migration is additive and can be reverted before release by dropping the new columns, check constraint, index, and restoring the view.
- API extension is backwards compatible; if frontend work is risky, backend fields can still ship harmlessly.
- UI changes are isolated to `/asset-decisions` saved records/detail surface.

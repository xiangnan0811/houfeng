# Implementation Plan

## Ordered Steps

1. Read Trellis specs for backend, web, database, shared guides, and frontend design guidelines before code edits.
2. Add backend domain types, validation, snapshot helpers, and pure functions for manual group summary/detail derivation.
3. Add migration `0037_create_asset_decision_manual_groups.sql` and update migration tests/tail ordering.
4. Extend `PostgresAssetDecisionRepository`:
   - list/create/get/patch manual groups.
   - add/patch/delete manual group members.
   - create records from `manual_group`.
   - keep existing auto group record behavior backward-compatible.
5. Extend handlers/router/bootstrap interfaces and tests.
6. Extend web types/API helpers/tests.
7. Refactor `AssetDecisionsPage`:
   - load manual group list alongside overview/groups/records/queues.
   - add manual group list surface.
   - add create-from-auto-group action.
   - add manual group detail modal with member intent editing and save-as-record.
8. Update specs/docs for asset decision model boundaries.
9. Run focused Go tests.
10. Run focused web tests and type/build checks as feasible.
11. Run visual sanity with the in-app browser for `/asset-decisions?view=needs_decision&renew_within_days=30`.
12. Run Trellis check / finish-work, commit, push, PR, CI, and release monitoring flow.

## Validation Commands

```bash
go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http ./internal/center/http/handlers ./cmd/houfeng-center ./internal/center/store/migrate
cd web && npm run test -- --run AssetDecisionsPage api
cd web && npm run typecheck
cd web && npm run build
python3 .trellis/scripts/task.py validate .trellis/tasks/06-06-asset-decisions-scenario-workbench
```

If full web commands are too slow, run focused Vitest first and record any skipped broader command with the reason.

## Risky Files

- `db/migrations/0035_create_asset_decision_records.sql` is historical and must not be edited directly.
- New migration must be additive and must safely alter the existing source type constraint.
- `web/src/pages/AssetDecisionsPage.tsx` is large; keep edits scoped and avoid unrelated visual rewrites.
- `internal/center/store/asset_decisions.go` already contains record/readback logic; avoid duplicating readback or facts queries.

## Rollback Points

- Migration changes are isolated in `0037_create_asset_decision_manual_groups.sql`.
- Manual group API can be disabled by removing router wiring without affecting existing auto groups and records.
- Frontend manual group surface can be hidden while keeping backend record compatibility intact.

## Pre-Start Checklist

- [ ] PRD contains testable acceptance criteria.
- [ ] Design records persistence/API boundaries and non-goals.
- [ ] Implementation plan includes validation commands.
- [ ] Relevant Trellis specs read before writing production code.
- [ ] `task.py start` has been run in the worktree.

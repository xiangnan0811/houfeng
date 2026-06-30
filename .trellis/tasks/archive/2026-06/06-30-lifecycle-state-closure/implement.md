# Complete lifecycle state closure implementation plan

## 1. Start task

- [x] Run `python3 ./.trellis/scripts/task.py start .trellis/tasks/06-30-lifecycle-state-closure` after planning artifacts are complete.

## 2. VPS state matrix

- [x] Add RED tests in `internal/center/vpsassets/types_test.go` for create lifecycle boundary and multi-axis forbidden combinations.
- [x] Implement domain helpers in `internal/center/vpsassets/types.go` and call them from create/patch validation without weakening ordinary PATCH restrictions.
- [x] Update import/create tests or fixtures that intentionally need historical terminal states.
- [x] Run `go test ./internal/center/vpsassets ./internal/center/http/handlers ./internal/center/store`.

## 3. Asset scope historical alias

- [x] Add RED domain tests that `historical` is valid and normalizes/trims like other scopes.
- [x] Update VPS/subscription handler/store tests to accept `asset_scope=historical` and query cancelled+archived.
- [x] Update `web/src/lib/types.ts`, API tests, Archive page requests/tests.
- [x] Update specs documenting `archived` as compatibility alias.

## 4. Subscription gift renewal mode

- [x] Add RED tests in `internal/center/subscriptions/types_test.go` for `gift` validation/normalization and legacy flags.
- [x] Add migration `0048_subscription_gift_renewal_mode.sql` to relax constraints for `subscriptions` and `price_histories`.
- [x] Update Go renewal/history validation tests.
- [x] Update frontend `RenewalMode`, `RENEWAL_MODE_OPTIONS`, labels and page/API tests.
- [x] Run targeted Go and web tests.

## 5. Incident inactive convergence

- [x] Add RED service tests: paused/maintenance/retired MonitoringInstance with existing active incidents yields mutation with no active incidents and recovered events, no notification records.
- [x] Add RED service tests: paused/archived Target in periodic sweep closes prior active target incidents and skips new evaluations.
- [x] Implement convergence helper in `internal/center/incidents/service.go`.
- [x] Ensure `AfterSuccessfulSync` also applies MI non-running convergence before sample evaluation.
- [x] Run `go test ./internal/center/incidents ./internal/center/store`.

## 6. Migration wording and state UX audit

- [x] Search production code for forbidden migration-workflow phrases and ensure only “迁移意向/人工跟进” remains.
- [x] Update visual evidence mock fixtures if needed to cover `gift` and `historical` paths.
- [x] Update task `research/post-fix-review.md` with status closure and remaining out-of-scope rationale.

## 7. Full verification

- [x] `make verify-go`
- [x] `python3 -m unittest scripts/test_visual_evidence.py`
- [x] `git diff --check`
- [x] `cd web && npm run lint`
- [x] `cd web && npm run test -- --run`
- [x] `cd web && npm run build`
- [x] Start Vite and run browser sanity for asset and observability mock routes at `1440x1000` and `390x900`.

## 8. Finish

- [x] Update `.trellis/spec/` for new state contracts.
- [ ] Commit task changes on non-main branch.
- [ ] Archive task and record journal if the goal is fully satisfied.
- [ ] Only mark goal complete if no known state lifecycle gap from the original request remains unhandled except explicitly out-of-scope non-goal items.

# Lifecycle state closure fix implementation plan

## 1. Planning

- [x] Write PRD/design/implement artifacts.
- [x] Start Trellis task.
- [x] Read applicable backend/web specs before edits.

## 2. TDD: VPS merged state

- [x] Add failing domain/store tests for `active/in_use + renewal_decision=replaced` via single-field patch.
- [x] Add failing tests for another omitted-current-value hard failure if coverage is practical.
- [x] Implement store merged-state validation using current row + patch preview.
- [x] Add DB migration/test for cross-column state constraint.
- [x] Re-run targeted VPS/store/migration tests.

## 3. TDD: Import renewal mode

- [x] Add failing import decode/dry-run tests for `subscription.renewal_mode=gift`.
- [x] Add invalid renewal mode test if not already covered by create validation.
- [x] Implement import DTO propagation.
- [x] Update sample docs or fixtures for explicit `gift` / `lottery`.
- [x] Re-run targeted import/subscription tests.

## 4. Low-risk closure

- [x] Add archived MonitoringInstance administrative recovery test.
- [x] Rename ArchivePage test description to `historical` scope.
- [x] Update visual evidence fixture with `gift` / `lottery` renewal modes.
- [x] Re-run targeted incident/frontend tests.

## 5. Specs and Review

- [x] Update `.trellis/spec/backend/database-guidelines.md` for merged PATCH validation, DB constraint, and import renewal_mode.
- [x] Update `.trellis/spec/web/state-and-data.md` if fixture/browser evidence or frontend contract guidance changes.
- [x] Write final review report under task research.

## 6. Verification

- [x] `go test ./internal/center/vpsassets ./internal/center/store ./internal/center/store/migrate ./internal/center/importing ./cmd/houfeng-import-vps-json ./internal/center/incidents ./internal/center/http/handlers ./internal/center/subscriptions`
- [x] `cd web && npm run test -- --run src/lib/api.test.ts src/lib/assetOptions.test.ts src/pages/ArchivePage.test.tsx`
- [x] `make verify-go`
- [x] `make verify-web`
- [x] `git diff --check`
- [x] Browser sanity with `asset-workflows` and `observability-support` mock profiles.

## 7. Finish

- [ ] Review all diffs for unintended scope.
- [ ] Commit implementation/spec/task artifacts.
- [ ] Archive task and record journal.

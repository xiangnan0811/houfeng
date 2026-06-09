# Implementation Plan

## Steps

1. Load backend specs and IP quality contract before code changes.
2. Add failing tests first:
   - agent collector parses ipapi.is nested JSON and uses JSON endpoint defaults.
   - agent collector treats HTML/non JSON as clean failure.
   - default service unlock probing is skipped.
   - agent due uses `LastAttemptedAt` to throttle failures.
   - center read model hides failure/`0.0.0.0`, keeps valid `partial`, and falls back to older valid reports.
3. Implement minimal agent changes:
   - default lookup URL.
   - optional service URL behavior.
   - nested JSON parsing helpers.
   - non JSON error classification.
   - due throttling by attempt timestamp.
4. Implement center changes:
   - add migration to redefine IP quality views with valid-report filtering.
   - adjust static migration tests.
   - add/adjust repository tests around latest/history behavior.
5. Update `.trellis/spec/backend/ip-quality-contract.md`.
6. Run targeted verification:
   - `go test ./agent/ipquality ./agent/runtime`
   - `go test ./internal/center/store ./internal/center/http/handlers ./internal/center/assetdecisions ./internal/center/store/migrate`
7. Run broader backend verification if targeted tests pass:
   - `make verify-go`
8. Self-review:
   - inspect git diff for unintended changes.
   - confirm no direct main/master work.
   - confirm failure reports still save but user reads are filtered.
   - fix any issues found.

## Rollback Points

- Agent collector changes are isolated to `agent/ipquality`.
- Center user-facing read-model change is isolated to a new migration and repository tests; raw table writes remain unchanged.
- If service unlock behavior needs restoration, add explicit provider support in a later task rather than re-enabling the broken default URL.

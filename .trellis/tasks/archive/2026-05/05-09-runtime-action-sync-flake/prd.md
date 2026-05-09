# Fix runtime action sync flake

## Goal

Stabilize the post-merge `main` CI failure in `agent/runtime` without changing runtime behavior. The failed run is `25588033256` on `main` after PR #5 merged; GitHub Actions `go` job failed in `TestRuntimeExecutesPendingActionAndReturnsCommandResult` because the test observed only one `Sync()` call before its timeout.

## What I Already Know

- PR #5 passed pull request checks (`go`, `web`, GitGuardian) before merge.
- The merge commit is `de9bd5ac2ce3bb54f2d0781c6111c87cac05e276`.
- The post-merge `main` push CI run `25588033256` failed only the `go` job; `web` passed.
- The failing assertion was `Sync() calls = 1, want at least 2` in `agent/runtime/runtime_test.go:651`.
- Local `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go` passes on macOS.
- The target test uses `context.WithTimeout(context.Background(), 35*time.Millisecond)` plus a `10*time.Millisecond` runtime interval, so it relies on scheduler timing rather than a deterministic stop condition.
- `fakeClient` already supports `cancelAfterSyncs` and `cancel`, which can stop the runtime immediately after a required number of syncs.

## Requirements

- Replace timing-dependent completion in the affected runtime action-result tests with deterministic cancellation after the expected sync call count.
- Preserve production runtime behavior: do not change command execution, sync request shape, plan handling, queue behavior, or agent API contracts.
- Keep the fix narrow to the flaky test surface unless a small shared test helper clearly reduces duplication.
- Do not add sleeps with larger arbitrary timeouts as the main fix.

## Acceptance Criteria

- [ ] `TestRuntimeExecutesPendingActionAndReturnsCommandResult` deterministically waits for two sync calls and still asserts the command result appears on the second sync request.
- [ ] The unknown pending action test retains coverage that no command result is sent for unrecognized command IDs.
- [ ] Any nearby runtime tests that rely on the same two-sync timing pattern are reviewed and adjusted if they carry the same flake risk.
- [ ] `go test ./agent/runtime -count=20` passes locally.
- [ ] `make verify-go` passes locally.
- [ ] CI passes on the repair PR and on `main` after merge.

## Out of Scope

- No changes to Task 2 VPS asset backend behavior.
- No changes to `agent/runtime` production scheduling or tick semantics unless the tests reveal an actual product bug.
- No frontend, migration, Asset Ledger, or dashboard changes.
- No direct commit or merge on local `main`.

## Technical Notes

- Branch: `fix/runtime-action-sync-flake`.
- Task is a post-merge CI repair required by the repository PR lifecycle.
- Relevant files: `agent/runtime/runtime_test.go`; possibly `.trellis/spec/backend/quality-guidelines.md` if the repository should document deterministic worker-test guidance after the repair.
- Research details: `research/ci-failure.md`.

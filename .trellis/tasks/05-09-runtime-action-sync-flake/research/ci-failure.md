# CI failure research

## Failure

- Workflow run: `25588033256`
- Branch: `main`
- Commit: `de9bd5ac2ce3bb54f2d0781c6111c87cac05e276`
- Job: `go`
- Step: `make verify-go`
- Failed package: `houfeng/agent/runtime`
- Test: `TestRuntimeExecutesPendingActionAndReturnsCommandResult`
- Assertion: `Sync() calls = 1, want at least 2`

## Root Cause Assessment

The failure is a test synchronization issue, not a VPS assets backend behavior failure. The test starts the runtime with a `10*time.Millisecond` interval and cancels the context after `35*time.Millisecond`. It then expects at least two sync ticks to have completed. On the GitHub Ubuntu runner, the process observed only one sync before cancellation.

The fake runtime client already has deterministic cancellation fields:

- `cancelAfterSyncs int`
- `cancel context.CancelFunc`

These let a test cancel as soon as the expected number of `Sync` calls has been observed. Nearby tests already use this pattern around the third sync path, so the repair should reuse the existing fake-client mechanism instead of increasing arbitrary timeouts.

## Candidate Fix

Use `context.WithCancel`, set `client.cancelAfterSyncs = 2`, and set `client.cancel = cancel` before `rt.Run(ctx)` for tests that require a second sync request. Keep a safety timeout around the whole test only if implemented as a parent deadline that fails the test instead of racing the runtime tick count.

## Verification

- `go test ./agent/runtime -count=20`
- `make verify-go`
- PR CI `go` and `web`
- post-merge `main` CI

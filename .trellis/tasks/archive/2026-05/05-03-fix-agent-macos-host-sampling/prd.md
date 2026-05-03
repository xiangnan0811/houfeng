# Fix macOS agent host sampling

## Goal

Make the local `houfeng-agent` collect and submit host samples on macOS instead of failing every sampling interval with `read /proc/loadavg: no such file or directory`. The Linux systemd deployment path must keep its existing `/proc` behavior.

## What I already know

* The user reproduced the issue with `make build-agent && ./bin/houfeng-agent` on macOS.
* The agent enrolls successfully and receives an accepted binding, so enrollment and sync credentials are not the failure point.
* The runtime logs `collect host sample failed` because `agent/hostsample.Provider.Collect` directly reads Linux `/proc/loadavg`.
* `docs/operations/v1-smoke-run.md` and `docs/release/v1-gap-checklist.md` already record the same macOS local-smoke gap.
* Existing host sampling tests cover Linux `/proc` parsing and rate derivation through injected file readers.

## Assumptions

* macOS local development should be a supported smoke-test environment for host sample collection.
* The production Linux path should remain the primary and most complete metrics source.
* It is acceptable for macOS-only fields that do not have a stable local source in this task to report conservative zero values, as long as host sample ingestion can complete and obvious metrics such as load, memory, disk, and uptime are populated.

## Requirements

* Keep the current Linux `/proc` collector behavior and tests intact.
* Add a Darwin-compatible host sample collection path selected automatically by `runtime.GOOS`.
* Ensure macOS collection does not attempt to read `/proc/*`.
* Populate enough fields for the center to accept `host_samples`: observed time, load averages, memory usage/available bytes, disk/inode usage, and uptime.
* Preserve existing first-sample/rate semantics: rate-based fields may start at zero and must not cause collection failure.
* Add focused tests covering the Darwin collector path and preventing regression to `/proc/loadavg` on macOS.
* Update release/smoke notes so the previous open macOS local-smoke gap is no longer misleading after the fix.

## Acceptance Criteria

* [x] `go test ./agent/hostsample` passes.
* [x] `go test ./agent/runtime` passes.
* [x] `make build-agent` passes.
* [x] Running the agent on macOS no longer logs `read /proc/loadavg: no such file or directory` during host sample collection.
* [x] Documentation no longer says macOS local host sampling is an unresolved gap.

## Definition of Done

* Tests added or updated for the changed behavior.
* Lint/typecheck/build gates relevant to the agent pass.
* Documentation updated if public smoke/release status changes.
* Rollback is simple: revert the Darwin collector and documentation status change.

## Out of Scope

* Windows host sample support.
* A full cross-platform metrics abstraction for every Linux `/proc` metric.
* Changing the center sync API or host sample database schema.
* Changing enrollment, heartbeat, probe collection, or incident evaluation behavior.

## Technical Notes

* Primary code: `agent/hostsample/provider.go`, `agent/hostsample/provider_test.go`.
* Runtime logs host sample failures from `agent/runtime/runtime.go`.
* Existing documentation gap: `docs/release/v1-gap-checklist.md` row 14 and `docs/operations/v1-smoke-run.md` caveats.
* Relevant spec layer: `.trellis/spec/backend/` because this is Go agent/runtime work.

## Verification

* `go test ./agent/hostsample`
* `go test ./agent/runtime`
* `make build-agent`
* `make verify-go`
* Short macOS rerun against the existing local center produced no `collect host sample failed` log and `latest_host_sample.observed_at=2026-05-03T14:50:01.228739+08:00`.

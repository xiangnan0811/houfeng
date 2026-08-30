# Independent Planning Review

Date: 2026-08-30 (Asia/Shanghai)

Scope: read-only source/spec review of `design.md` and `implement.md`. The reviewer did not edit product code, run tests, access a remote system/database, or perform Git delivery actions.

## Verdict

The first review found 1 Critical, 4 Important, and 1 Minor planning issue. A second pass found one remaining latest-ordering Important; after correcting the per-table stable row key and test inventory, the final independent pass reported **no Critical and no Important findings**. The user accepted the intentional relaxation from global FIFO to backlog-lane FIFO with `K=2` fresh interleaving on 2026-08-30. No implementation is authorized or claimed complete.

## Findings and resolutions

### Critical — committed sync must not be reversed by projection conflict

Evidence:

- `internal/center/store/sync_batches.go:105,137,148`: ApplyBatch may commit and consume a pending action before post-sync; an exact duplicate later returns no equivalent plan/action.
- `internal/center/syncing/service.go:89,95`: a post-sync error currently discards the committed Result at the service boundary.

Resolution in the drafts: one full re-read/re-evaluation remains inside the incident processor; a second CAS conflict records a safe bounded warning, returns success to the already committed sync, and yields convergence to a later sweep. The TDD plan now requires an end-to-end pending-action/plan preservation regression.

### Important — duplicate and suppressed are different outcomes

Evidence:

- `internal/center/store/sync_batches.go:85,105`: inactive-object suppression and exact duplicate currently both use zero-value Result paths.
- `.trellis/spec/backend/database-guidelines.md:2413,2415`: touched inactive monitoring instances/targets require administrative recovery.

Resolution: replace the proposed bool with a closed `recorded / exact_duplicate / suppressed` disposition. Only exact duplicate skips post-sync; suppressed preserves administrative recovery.

### Important — latest ordering file set was incomplete

Evidence:

- `internal/center/store/monitoring_instances.go:430`: latest heartbeat agent-version query.
- `internal/center/store/ip_quality.go:244`: IP-quality latest query.
- `internal/center/incidents/evaluator.go:603,614,849`: unstable Go sorts can destroy SQL tie order.

Resolution: the file/test inventory now includes these paths plus `runtime_facts_test.go` and applies one ordering contract: `(observed_at DESC, is_backfilled ASC, received_at DESC, stable_row_key DESC)`, with stable Go sorting that preserves the final SQL key order. The stable key is `id` for host/probe/heartbeat and `report_id` for IP-quality.

### Important — local queue ID must also drive provenance

Evidence:

- `agent/runtime/runtime.go:558`: current backfilled classification compares carrier batch ID.
- `agent/syncqueue/store.go:438`: collision suffixes change only the local entry ID, not the request batch ID.

Resolution: `entry.ID == currentEntryID` drives both lane selection and live/backfilled classification. A named collision RED/GREEN test is included in the focused commands.

### Important — Activity fixture named a nonexistent function

Evidence: the production entrypoint is `EnsureActiveActivityProjectionGeneration` at `internal/center/store/record_activity_generations.go:14`.

Resolution: the implementation plan now uses the real function name. The core Activity conclusion remains unchanged: remove only the facts-table `FOR UPDATE`; keep the active-head lock and immutable-fact ACL.

### Minor — choose the behavior change, not the already-satisfied logging option

Evidence: `.trellis/spec/backend/error-handling.md:326,356` currently specifies global oldest-first behavior, while the PRD already permits minimal log-based observability.

Resolution: Agent journal aggregation is now the default. The single user choice is whether to accept backlog-lane FIFO plus fresh interleaving after at most two old attempts.

## CAS conclusion and residual risk

The reviewer accepted `xmin::text` as an opaque, short-lived PostgreSQL 16 equality token for this design, provided direct-runtime tests prove permission, token change on successful projection, guard-before-side-effects, and full re-read on conflict. It avoids an unsafe successor migration against the exact-current APP ACL manifest.

This CAS does not make external notification dispatch and notification-record append atomic. Their pre-existing crash window remains explicit and must not be described as exactly-once delivery.

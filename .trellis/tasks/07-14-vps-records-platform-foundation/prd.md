# Unified Authorization and Platform Foundation

## Goal

Close the platform foundation by making the shipped APP migration/ACL path usable
for the current early-development migration set, while preserving the
authorization, idempotency, delivery, deletion, and recovery primitives already
merged on main.

## 2026-08-02 status

This child remains `in_progress`. Four bounded descendants are archived and
merged:

- `07-24-app-acl-migration-runtime-handoff`;
- `07-24-record-platform-recordauth-policy`;
- `07-24-record-platform-delivery-primitives`;
- `07-27-app-acl-r2-privileged-transition`.

The bounded current-development migration/admission slice is implemented and
locally verified on `codex/vps-records-platform-current-app-acl`. It has not been
pushed, opened as a PR, run through remote CI, merged to protected main, or
archived. The previous APP V3 owner-transfer, approval, drain, rotation, and
advanced disaster-recovery expansion is removed from this child.

## Confirmed delivered baseline

- `0051_create_record_platform_foundation.sql` and foundation stores/types;
- scoped APP migrator CLI and atomic migration/ACL convergence;
- persisted manifest and effective catalog admission;
- trusted actor and `recordauth.Policy` boundary;
- idempotency, outbox, identity mutation guard, deletion reservation/fence, and
  content-delivery primitives;
- isolated APP ACL R2 transition code and tests;
- archived descendant acceptance present on protected main.

Delivered code is still subject to the final child audit. This list does not
mean the child is complete.

## 2026-08-02 local closeout evidence

The selected worktree is
`/home/murray/code/houfeng/.worktree/vps-records-platform-current-app-acl`, based
on `origin/main@d38a8cad`. The local implementation commits are:

- `cfc5cd69` compile current migration fragments;
- `d023d651` share the APP ACL catalog contract;
- `ef3609eb` share catalog verification;
- `f2fec02e` add strict current convergence;
- `12ceaa01` add current runtime admission;
- `eccb22d6` add real PostgreSQL evidence;
- `0bf7c83b` route product callers through current entry points;
- `2e6a45a2` harden state classification and fragment/transaction preflight.

Fresh local evidence after the review fixes:

- `go test ./internal/center/store/migrate -count=1`;
- strict `TestPostgresIntegrationAppACLCurrent` through
  `scripts/test-record-platform-integration.sh postgres` with no skip;
- `go test` for record-platform admin, center, importer, and migrate packages;
- `make verify-go` (`fmt-go`, `vet-go`, and all Go tests);
- `git diff --check` and Trellis task validation for this child and its parent;
- root migrations remain 52 files ending at
  `0051_create_record_platform_foundation.sql`;
- all four archived descendants and their completed task metadata are present
  on `origin/main`.

| Local acceptance area | Code and test evidence |
| --- | --- |
| Frozen R1/R2 compatibility | existing exported R1/R2 entry points plus the complete `internal/center/store/migrate` package run |
| Source/fragment closed world | `app_acl_current_contract.go` and `app_acl_current_contract_test.go` |
| Fresh/exact/different convergence | `app_acl_current_convergence.go` and `app_acl_current_convergence_test.go` |
| Real database behavior | `app_acl_current_postgres_integration_test.go` strict PostgreSQL suite |
| One-snapshot runtime admission | `app_acl_current_runtime_admission.go` and its tests |
| Product routing and safe error chain | admin, center, and importer `main`/`bootstrap` tests |

Checkboxes below record local branch evidence only. Child 1 is not complete
until the selected commits pass PR review/CI and are integrated into protected
main; the parent therefore remains `0/11`, and Child 2 has not started.

## Requirements

- Read the parent
  `research/development-rebaseline-2026-08-02.md` before implementation.
- Preserve frozen APP R1 and isolated APP R2 exported entry points and tests as
  historical contracts. Do not route new Records migrations through APP R2.
- Add a current-development APP contract that consumes the exact embedded root
  migration set and an explicit ACL fragment for every migration after `0051`.
- The fragment contract must own managed objects, runtime/admin privileges, and
  any persistent function hardening needed by that migration. An explicit empty
  fragment is required when a migration adds no APP-managed object.
- `houfeng-record-platform-admin migrate --scope app` must create a fresh
  database, apply the complete embedded set, converge exact ACL/ownership, and
  persist an exact manifest in one serializable transaction.
- Records-enabled center startup must perform read-only admission against the
  same current contract.
- Repeating migrate/startup on the exact current database must succeed without
  changing manifest head, migration ledger, or catalog ACL.
- A database with a different applied/manifest migration set, including the
  prior exact R1 set after a future migration is embedded, must fail before
  DDL/DCL/ledger/manifest mutation with an actionable rebuild-required error.
- Existing generic `Apply` behavior may remain for non-Records legacy paths, but
  it is not a supported upgrade route for Records-enabled startup.
- No root migration is added in this slice. The next available migration remains
  `0052_create_records_core.sql` for Child 2.
- Run supported Node 22 and workspace-backed Go temp directories for full gates.

## Acceptance Criteria

- [x] The frozen `ConvergeAppACLR1` and isolated R2 suites retain their exact
  historical behavior.
- [x] A future migration injected in tests is rejected before `BeginTx` when its
  current-development ACL fragment is absent.
- [x] The same injected migration reaches the transaction boundary when an exact
  fragment is registered, and mismatched/duplicate/unknown fragments fail.
- [x] Fresh PostgreSQL convergence uses the exact embedded canonical migration
  set, creates the expected current managed surface, applies only compiled
  privileges, and persists one genesis manifest.
- [x] Exact repeat convergence is read-only with respect to ledger, manifest
  head/revisions, ownership, and ACL.
- [x] Current runtime admission verifies manifest, ledger, embedded sources,
  direct-login identity, ownership, and direct/effective privileges in one
  repeatable-read read-only snapshot.
- [x] A prior/different development database returns an
  `errors.Is(..., ErrDevelopmentDatabaseRebuildRequired)`-compatible error before
  mutation; the CLI and center startup surface a message that tells the operator
  to recreate the development database.
- [x] `cmd/houfeng-record-platform-admin` and `cmd/houfeng-center/bootstrap.go`
  use the current entry points; no product bootstrap/finalize R2 route becomes
  the path for Records migrations.
- [x] Focused unit tests, APP PostgreSQL integration, CLI/bootstrap tests, full
  Go verification, `git diff --check`, and `trellis-check` pass.
- [ ] The final audit maps all surviving Child 1 acceptance to code/tests on
  protected main, removes stale APP V3 requirements from active planning, and
  closes this child without adding Records Core functionality.

## Out of Scope

- `0052` or any Records Core table/API/UI.
- In-place upgrade or successor for any released/development database.
- Mixed-version deployment, rolling upgrade, staging cutover, or release receipt.
- APP V3 owner transfer, detached approvals, traffic drain, key rotation, or
  cross-domain disaster-recovery governance.
- Deleting frozen R1/R2 history merely to reduce file count.
- Cleaning old worktrees, branches, or the dirty primary checkout.

## Execution Gate

The 2026-08-02 rebaseline and detailed closeout plan were reviewed and execution
was explicitly approved. The bounded slice has reached its local review
checkpoint. Do not push, open a PR, merge/archive Child 1, or continue into
Child 2 until this checkpoint is reviewed.

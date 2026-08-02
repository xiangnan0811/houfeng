# Unified Authorization and Platform Foundation

## Goal

Close the platform foundation by making the shipped APP migration/ACL path usable
for the current early-development migration set, while preserving the
authorization, idempotency, delivery, deletion, and recovery primitives already
merged on main.

## 2026-08-02 status

This child is `in_progress` and materially implemented. Four bounded descendants
are archived and merged:

- `07-24-app-acl-migration-runtime-handoff`;
- `07-24-record-platform-recordauth-policy`;
- `07-24-record-platform-delivery-primitives`;
- `07-27-app-acl-r2-privileged-transition`.

The remaining work is one current-development migration/admission slice plus a
closeout audit. The previous APP V3 owner-transfer, approval, drain, rotation,
and advanced disaster-recovery expansion is removed from this child.

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

- [ ] The frozen `ConvergeAppACLR1` and isolated R2 suites retain their exact
  historical behavior.
- [ ] A future migration injected in tests is rejected before `BeginTx` when its
  current-development ACL fragment is absent.
- [ ] The same injected migration reaches the transaction boundary when an exact
  fragment is registered, and mismatched/duplicate/unknown fragments fail.
- [ ] Fresh PostgreSQL convergence uses the exact embedded canonical migration
  set, creates the expected current managed surface, applies only compiled
  privileges, and persists one genesis manifest.
- [ ] Exact repeat convergence is read-only with respect to ledger, manifest
  head/revisions, ownership, and ACL.
- [ ] Current runtime admission verifies manifest, ledger, embedded sources,
  direct-login identity, ownership, and direct/effective privileges in one
  repeatable-read read-only snapshot.
- [ ] A prior/different development database returns an
  `errors.Is(..., ErrDevelopmentDatabaseRebuildRequired)`-compatible error before
  mutation; the CLI and center startup surface a message that tells the operator
  to recreate the development database.
- [ ] `cmd/houfeng-record-platform-admin` and `cmd/houfeng-center/bootstrap.go`
  use the current entry points; no product bootstrap/finalize R2 route becomes
  the path for Records migrations.
- [ ] Focused unit tests, APP PostgreSQL integration, CLI/bootstrap tests, full
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

The child retains its existing `in_progress` state, but production implementation
does not resume until the 2026-08-02 rebaseline and detailed closeout plan are
reviewed. Execute only the bounded current-development slice, then stop for
audit; do not continue directly into Child 2.

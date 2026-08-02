# Unified Authorization and Platform Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use
> `superpowers:subagent-driven-development` or `superpowers:executing-plans`
> only after the execution approach is reviewed. This plan contains one bounded
> production slice and one closeout review.

**Goal:** Close Child 1 by routing APP migration and startup admission through an
exact current-development contract without building an APP successor.

**Architecture:** Preserve frozen R1/R2 wrappers; extract their reusable catalog
verification/convergence core into a slice-backed internal contract; add an
explicit per-migration current fragment registry; route the admin CLI and center
startup to current entry points.

**Tech Stack:** Go, pgx/v5, PostgreSQL, embedded SQL migrations, standard
`errors.Is` error chains.

---

## Delivered work (do not reimplement)

The following behavior is already merged on main and should be audited, not
started again:

- migration `0051` and platform foundation stores/contracts;
- frozen APP R1 migration, ACL, manifest, and runtime admission;
- isolated APP R2 transition;
- `recordauth.Policy`;
- idempotency/outbox/identity/deletion/delivery primitives;
- archived Child 1 descendant tasks.

## Preconditions

- [x] Read the parent rebaseline and this child's PRD/design.
- [x] Start from the reviewed `origin/main` baseline in the selected non-main
  worktree; do not use the dirty primary checkout.
- [x] Run `trellis-before-dev` for backend/database/error/quality guidance.
- [x] Confirm no new root migration exists after `0051` on the selected main.
- [x] Run the focused frozen APP migration/admission baseline and full Go
  baseline with workspace-backed `GOTMPDIR`/`TMPDIR`.
- [x] Review
  `research/current-app-migration-baseline-plan.md`.

## Task 1: Current contract compiler and shared catalog model

- [x] Follow detailed-plan Tasks 1-3 using RED -> verified RED -> minimal GREEN.
- [x] Preserve frozen exported R1 signatures and exact R1/R2 regression behavior.
- [x] Prove the registry rejects missing, extra, duplicate, or invalid future
  migration fragments.

## Task 2: Current convergence and rebuild-required boundary

- [x] Follow detailed-plan Tasks 4-5.
- [x] Support only fresh and exact-current states.
- [x] Prove different development baselines fail before transaction/catalog
  mutation and exact repeats do not update durable state.

## Task 3: Runtime, CLI, and center routing

- [x] Follow detailed-plan Tasks 6-7.
- [x] Route `migrate --scope app` and Records-enabled bootstrap to current entry
  points.
- [x] Keep R2 commands isolated and ensure actionable errors remain wrapped.

## Task 4: Verification and closeout audit

- [x] Run detailed-plan Task 8 focused tests and PostgreSQL integration.
- [x] Run full Go verification using supported temp paths.
- [x] Run `git diff --check` and `trellis-check`.
- [x] Compare Child 1 delivered behavior and surviving PRD acceptance to code and
  tests on the selected commit.
- [x] Update only executable specs established by the resulting implementation.
- [x] Confirm the slice does not touch Records Core, introduce a root `0052`, or
  require a successor database state; stop for review if any boundary changes.
- [ ] After protected-main merge and CI, archive Child 1 and update the parent
  progress table. Do not start Child 2 in the same unchecked continuation.

## Local execution record

The implementation is on
`codex/vps-records-platform-current-app-acl`, based on
`origin/main@d38a8cad`, in the dedicated selected worktree. Eight local code
commits currently implement the slice:

```text
cfc5cd69 refactor: compile current app migration fragments
d023d651 refactor: share app acl catalog contract
ef3609eb refactor: share app acl catalog verification
f2fec02e feat: converge current app migration baseline
12ceaa01 feat: admit current app runtime contract
eccb22d6 test: prove current app acl on postgres
0bf7c83b feat: route app startup through current acl
2e6a45a2 fix: harden current app acl state classification
```

The final local verification used workspace-backed `TMPDIR`/`GOTMPDIR` and
passed the complete migrate package, the strict real-PostgreSQL current suite,
the three product callers plus migrate, `make verify-go`, `git diff --check`,
and both child/parent `task.py validate` commands. Static audit confirms the
root migration set still ends at `0051`, the frozen R1/R2 exports remain, all
Records-enabled product defaults route through current entry points, and the
four archived descendants exist on `origin/main`.

This is a local review checkpoint, not task completion. The branch is not
pushed, no PR or remote CI exists, protected main does not contain these
commits, Child 1 is not archived, the parent remains `0/11`, and Child 2 has not
started.

## Required commands

Use exact focused commands from the detailed plan. The final local gate includes:

```bash
go test ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin ./cmd/houfeng-center -count=1
HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store/migrate -run 'PostgresIntegration.*AppACLCurrent' -count=1
make verify-go
git diff --check
```

The PostgreSQL command also requires the repository's normal integration
database environment. A skipped PostgreSQL suite is not passing evidence.

## Hard stops

- No APP V3 successor, owner-transfer, signing, drain, rotation, or DR work.
- No old-database upgrader or null-head adoption in the current path.
- No Records Core migration, API, domain, or UI.
- No edits to the dirty primary checkout or stale worktrees.
- The rebaseline implementation approval has been consumed. Stop at this local
  review checkpoint before push/PR; do not infer approval to merge, archive, or
  start Child 2.

## Rollback

Before later Records migrations, product routing can be reverted to the frozen
R1 entry points without schema change. Once a post-`0051` development migration
exists, rollback to an older build requires recreating the development database.
Frozen R1/R2 code remains available for regression comparison, not production
advancement.

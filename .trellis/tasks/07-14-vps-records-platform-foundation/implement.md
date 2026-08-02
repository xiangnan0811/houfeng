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

- [ ] Read the parent rebaseline and this child's PRD/design.
- [ ] Start from the reviewed `origin/main` baseline in the selected non-main
  worktree; do not use the dirty primary checkout.
- [ ] Run `trellis-before-dev` for backend/database/error/quality guidance.
- [ ] Confirm no new root migration exists after `0051` on the selected main.
- [ ] Run the focused frozen APP migration/admission baseline and full Go
  baseline with workspace-backed `GOTMPDIR`/`TMPDIR`.
- [ ] Review
  `research/current-app-migration-baseline-plan.md`.

## Task 1: Current contract compiler and shared catalog model

- [ ] Follow detailed-plan Tasks 1-3 using RED -> verified RED -> minimal GREEN.
- [ ] Preserve frozen exported R1 signatures and exact R1/R2 regression behavior.
- [ ] Prove the registry rejects missing, extra, duplicate, or invalid future
  migration fragments.

## Task 2: Current convergence and rebuild-required boundary

- [ ] Follow detailed-plan Tasks 4-5.
- [ ] Support only fresh and exact-current states.
- [ ] Prove different development baselines fail before transaction/catalog
  mutation and exact repeats do not update durable state.

## Task 3: Runtime, CLI, and center routing

- [ ] Follow detailed-plan Tasks 6-7.
- [ ] Route `migrate --scope app` and Records-enabled bootstrap to current entry
  points.
- [ ] Keep R2 commands isolated and ensure actionable errors remain wrapped.

## Task 4: Verification and closeout audit

- [ ] Run detailed-plan Task 8 focused tests and PostgreSQL integration.
- [ ] Run full Go verification using supported temp paths.
- [ ] Run `git diff --check` and `trellis-check`.
- [ ] Compare Child 1 delivered behavior and surviving PRD acceptance to code and
  tests on the selected commit.
- [ ] Update only executable specs established by the resulting implementation.
- [ ] Stop for review before commit/PR if the slice touches Records Core,
  introduces a root `0052`, or needs a successor database state.
- [ ] After protected-main merge and CI, archive Child 1 and update the parent
  progress table. Do not start Child 2 in the same unchecked continuation.

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
- No task status change, commit, push, or PR until the user reviews this
  rebaseline handoff.

## Rollback

Before later Records migrations, product routing can be reverted to the frozen
R1 entry points without schema change. Once a post-`0051` development migration
exists, rollback to an older build requires recreating the development database.
Frozen R1/R2 code remains available for regression comparison, not production
advancement.

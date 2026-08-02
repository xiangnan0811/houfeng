# VPS Records Development Rebaseline - 2026-08-02

## Status and authority

This document is the authoritative execution baseline for the VPS detail and
project records program as of 2026-08-02. It supersedes older assumptions about
upgrade compatibility, mixed-version deployment, staging cutover, release
receipts, and APP ACL successor work. The approved product design remains the
target except where this document narrows the current development scope.

This is a planning snapshot, not implementation approval. Task statuses stay
unchanged until the relevant child plan is reviewed and explicitly started.

## Confirmed product context

- The repository is public, but there are no users and no known deployments.
- The project is still in early development and has not been promoted.
- Shipping speed is not the primary constraint; implementing the intended
  functionality correctly is the priority.
- In-place database upgrades from `v0.60.1` or earlier are not supported by
  this program. Development databases may be rebuilt.
- Existing root migrations are not rewritten merely because rewriting is now
  allowed. They may be adjusted when a concrete implementation benefit
  outweighs the churn.
- Progress is measured by accepted child behavior, tests, and protected-main
  integration, not elapsed time or lines changed.

## Repository snapshot

### Selected baseline

- Remote baseline: `origin/main@84f1716cf37832054918be4ea46a152b3759d193`.
- Planning branch: `codex/vps-detail-rebaseline`.
- Planning worktree: `/home/murray/code/houfeng/.worktree/vps-detail-rebaseline`.
- The planning branch starts exactly at `origin/main`.
- Repository hooks were enabled in the planning worktree.
- GitHub had no open pull request at the time of this snapshot.

### Existing main checkout

The primary checkout remains on `codex/vps-detail-experience-design@ff126787`
and is intentionally untouched. Its dirty state is not part of this rebaseline:

- modified `.trellis/config.yaml`, selecting Trellis sub-agent dispatch;
- untracked `db/migrations/0052_add_app_extension_hardening_receipt.sql`;
- untracked `internal/center/store/migrate/app_extension_hardening_receipt.go`;
- untracked `.tmp/vps-detail-review/` research artifacts;
- untracked root `node_modules/` containing only Vite cache directories.

The untracked `0052` migration and receipt implementation belong to the
abandoned APP V3/privileged-hardening direction. They also collide with the new
Records Core migration number. They are quarantined evidence, not completed
program work, and must not be copied or deleted as part of planning.

### Local and remote branch interpretation

- Almost all VPS foundation and APP ACL worktree branches are ancestors of
  `origin/main`; their delivered behavior is already represented on main.
- A few old branches diverge only because equivalent patches were replayed
  under different commits. They are not active implementation branches.
- `codex/app-acl-r2-slice6-runtime-admission-spec` contains one patch-unique
  historical spec commit. It predates later R2 closeout work and is not an
  implementation dependency; audit it separately before eventual cleanup.
- Remote feature branches that remain visible all correspond to merged PRs
  (`#384`, `#386`, `#387`, and `#391`). No remote branch represents unmerged
  Records Core or VPS detail functionality.
- Stale worktrees are retained for now. Their presence is not counted as
  parallel progress and does not authorize resuming them.

## Baseline verification

The selected main baseline is green when run with the supported toolchain and
workspace-backed temporary directories:

- Go verification: pass.
- Web lint: pass.
- Vitest: 124 files and 865 tests pass.
- Web build, bundle budget, and CSS budget: pass.

Use Node `22.23.1`, not the ambient Node 24 installation. Go verification needs
`GOTMPDIR` and `TMPDIR` outside the quota-constrained system `/tmp`.

## What main already contains

Child 1 produced substantial reusable platform work, including:

- root migration `0051_create_record_platform_foundation.sql`;
- scoped APP migrator CLI and atomic migration/ACL convergence;
- runtime manifest and effective-catalog admission;
- `recordauth.Policy` and actor-scope boundary;
- idempotency, outbox, identity guards, deletion reservations and leases;
- delivery, deletion, recovery, and retention primitives;
- an isolated APP ACL R2 privileged-transition implementation.

Four bounded child tasks are archived and merged:

- `07-24-app-acl-migration-runtime-handoff`;
- `07-24-record-platform-recordauth-policy`;
- `07-24-record-platform-delivery-primitives`;
- `07-27-app-acl-r2-privileged-transition`.

The R1/R2 code is retained as historical, tested contract code. It is not the
successor path for new Records migrations and must not continue expanding into
APP V3 owner transfer, signature approval, drain orchestration, key rotation,
or cross-domain disaster-recovery governance.

## Honest progress snapshot

The parent remains `0/11` complete. Child 1 is materially implemented but not
closed; Children 2-11 have planning artifacts but no accepted product
implementation. Therefore the user-facing VPS detail/Records experience is
still largely unimplemented even though the foundation is substantial.

| Child | Current evidence | Remaining exit gate |
|---|---|---|
| 1. Platform foundation | Four merged/archived children, `0051`, ACL/auth/delivery primitives | Current-development migration/admission slice, focused verification, audit, close |
| 2. Records core | Planning only | Records, revisions, drafts, lifecycle, API, core deletion adapter |
| 3. Attachments | Planning only | `0053`, local/S3 blob, scan/quota/download, adapters |
| 4. Evidence | Planning only | `0054`, registry, initial source adapters, capture and rendering |
| 5. Markdown workspace | Planning only | Editor, safe render, diff/conflict, material workspace |
| 6. Search center | Planning only | `0056`, projection/query, Records pages and global search |
| 7. Activity and overview | Planning only | `0057`, activity projection, subject timeline, VPS overview |
| 8. Comparison workbench | Planning only | Candidate/fixed comparison, UI, save-as-record |
| 9. Collaboration | Planning only | `0055`, actions/comments/watch/inbox/notification |
| 10. Portability | Planning only | `0058`, import/export and deletion/recovery adapters |
| 11. Integration verification | Planning only | Full integration, backup/restore, security/performance/UI and final acceptance |

No child is credited merely because an old branch, worktree, plan, or goal
exists. A child completes only when its own acceptance criteria pass and its
selected branch is integrated into protected main.

## Scope removed from the current program

- in-place upgrades from released or development databases;
- mixed binary/database versions and rolling deployment compatibility;
- `experience_logs` backfill, conversion, dual-write, or migration ledger;
- APP V3 owner-transfer successor and privileged activation saga;
- detached signature approval, traffic-drain receipts, advanced key rotation,
  and cross-domain disaster-recovery governance;
- staging cutover phases, release-image receipts, soak periods, Release Please
  orchestration, and deployment completion as child acceptance gates;
- human-participant comprehension studies as an implementation blocker.

These may become later tasks when users, deployments, or release requirements
make them real constraints. They are not silently deferred work inside the 11
current children.

## Contracts retained

- least-privilege runtime and admin database access;
- exact migration/manifest/catalog admission for the embedded build;
- authorization and response allowlists;
- immutable revision and evidence history;
- idempotency, CAS, outbox, and deletion fencing;
- source deletion without content resurrection;
- safe attachment/content processing;
- import/export integrity and authorization;
- backup/restore that preserves supported data and deletion outcomes;
- permanent delete disabled until every content-owning adapter and the final
  backup/restore replay contract are complete.

## Dependency and migration baseline

The default execution is sequential. Parallel work is allowed only when file
ownership, database numbering, and review capacity are explicitly independent.

```text
Child 1 foundation closeout
  -> Child 2 records core (0052)
     -> Child 3 attachments (0053)
     -> Child 4 evidence (0054)
     -> Child 9 collaboration (0055)
        -> Child 5 Markdown workspace (no migration)
        -> Child 6 search center (0056)
        -> Child 7 activity and overview (0057)
        -> Child 8 comparison workbench (no migration)
        -> Child 10 portability (0058)
           -> Child 11 integration verification (no migration)
```

The default reviewed order is `1 -> 2 -> 3 -> 4 -> 9 -> 5 -> 6 -> 7 -> 8 ->
10 -> 11`. The arrows express the reviewed default integration order, not a
requirement to create multiple simultaneous branches. Direct functional
dependencies remain explicit in each child's PRD and implementation plan.
Child 5 starts after Core, Attachments, Evidence, and Collaboration because its
accepted UI includes all four domains. Child 8 requires Core and Evidence; its
final UI integration also requires the Markdown workspace. Child 10 starts only
after every content-owning domain it exports has stabilized.

| Owner | Root migration |
|---|---|
| Child 1 foundation | existing `0051` |
| Child 2 core | `0052` |
| Child 3 attachments | `0053` |
| Child 4 evidence | `0054` |
| Child 9 collaboration | `0055` |
| Child 6 search | `0056` |
| Child 7 activity | `0057` |
| Child 10 portability | `0058` |
| Children 5, 8, 11 | none planned |

Every child that adds root-database objects owns the corresponding managed
surface, role privilege, runtime admission, fresh-database, and repeat-start
tests. A migration is incomplete if the current APP ACL compiler cannot admit
its objects.

## Execution governance

- Do not restart the stopped 46-hour goal or create another goal for the whole
  parent program.
- Goals, sub-agents, and child sessions remain available for bounded work with
  an explicit exit condition and review checkpoint.
- Prefer one active implementation branch/worktree/PR. Deviate only for a
  concrete independent dependency or verification reason.
- Start one child or one reviewable slice at a time. Re-evaluate scope after
  each merge instead of granting an open-ended implementation mandate.
- Keep the parent in planning; start only the child that owns the next product
  deliverable.
- Do not change task status, commit, push, or open a PR merely because this
  rebaseline is written. Planning review comes first.

## Immediate next slice

Child 1 has one bounded closeout slice:

1. Keep frozen APP R1/R2 entry points and tests as historical contracts.
2. Add a current-development APP migration/ACL contract that consumes the
   exact embedded migration set and explicit per-migration ACL fragments.
3. Make `houfeng-record-platform-admin migrate --scope app` converge a fresh
   database to that current contract and repeat safely on the exact same build.
4. Make Records-enabled center startup admit that same current contract.
5. Reject older or otherwise different development databases before mutation
   with an actionable rebuild-required error; do not build a successor.
6. Prove an injected future migration is rejected without its ACL fragment and
   accepted when the fragment is registered.
7. Verify the focused and full Go gates, then audit and close Child 1.

No Records Core schema or user-facing feature is part of that slice.

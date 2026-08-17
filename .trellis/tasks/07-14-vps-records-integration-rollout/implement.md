# Records Integration Verification, Backup/Restore, and Final Acceptance Implementation Plan

> **For agentic workers:** Choose a reviewed bounded execution approach. Do not
> dispatch one worker to “finish integration”; each task below has an explicit
> output and checkpoint.

**Goal:** Prove the complete Records experience and same-build backup/restore
behavior across real local and S3-compatible profiles.

**Architecture:** Compose child-owned adapters through an exact registry, stage
typed backup manifests/artifacts, restore into an isolated fresh target, replay
deletions before projection rebuild/readiness, and run cross-domain/browser
acceptance on the same commit.

**Tech Stack:** Go/pgx/PostgreSQL 16, local/S3-compatible storage, content
processor/scanner, shell integration harness, React/Playwright/Axe.

---

## Preconditions

- [ ] Children 1-10 are merged and archived on protected main.
- [ ] Run `trellis-before-dev` for backend/database/deploy/security and Web
  quality/e2e guidance.
- [ ] Inventory actual adapter kinds, worker constructors, integration helpers,
  feature flags, and test commands; revise paths below before start.
- [ ] Inventory and verify Child 10's concrete deployment-membership gate,
  source-deletion witness, and quarantine constructors; production composition
  must have no test-gate or local-tombstone fallback.
- [ ] Confirm no root migration is added and the current migration maximum is
  the planned `0058` (or the globally rebaselined equivalent).
- [ ] Establish clean Go/Web/PostgreSQL/local/MinIO baselines with Node 22 and
  workspace-backed temporary directories.

## Task 1: Exact adapter/capability registry

- [ ] Write RED tests for exact expected kinds, duplicate/unknown/missing/
  incompatible/unhealthy adapters and permanent-delete readiness.
- [ ] Implement aggregate registry and content-safe status matrix.
- [ ] Wire child-owned adapters without moving their domain logic into Child 11.
- [ ] Compose membership/witness authority into bootstrap/readiness and prove
  nil/typed-nil, stale, wrong-deployment, discontinuous, and outage cases remain
  closed before any business write or protected read.
- [ ] Run focused registry/bootstrap tests; return missing contracts to owners.

## Task 2: Typed backup manifest and staging

**Files:** create focused `internal/center/recordbackup` package and
`cmd/houfeng-backup` only if equivalent current packages do not already exist.

- [ ] Write canonical manifest, tamper, unknown-version, content-allowlist, and
  deterministic digest RED tests.
- [ ] Implement plan/create/verify with staged database/external objects and
  atomic manifest publish.
- [ ] Add local/S3-compatible artifact conformance and all failure cutpoints.
- [ ] Verify cleanup receipts for partial files, multipart uploads, pins, and
  workspaces.

## Task 3: Isolated restore runtime

**Files:** create focused `internal/center/recordrestore` package and
`cmd/houfeng-restore` only where current code has no equivalent.

- [ ] Write state-machine RED tests for non-empty target, incompatible build/
  migration/adapters, missing/tampered bytes, and each cutpoint.
- [ ] Implement plan/apply/verify into an isolated fresh target.
- [ ] Restore database/objects, call adapters, replay deletions, rebuild
  projections, converge current APP ACL, and publish readiness in order.
- [ ] Prove retry/cleanup and zero serving/worker activity before readiness.

## Task 4: Integration profiles and happy-path matrix

- [ ] Build deterministic PostgreSQL 16 + local storage profile.
- [ ] Build equivalent S3-compatible Blob/ArtifactStore + processor profile.
- [ ] Exercise every user workflow from record creation through import/export
  and archive/restore using real HTTP/workers.
- [ ] Emit commit/config/manifest-bound content-safe reports and deterministic
  cleanup.

## Task 5: Failure, revoke, deletion, and recovery matrix

- [ ] Inject database/object/processor/worker/stream/export/import/backup/restore
  failures at before/after durable boundaries.
- [ ] Verify idempotent retry, no partial visibility, bounded workspaces, and
  truthful external-copy disclosure.
- [ ] Run backup -> permanent delete -> restore for records and related source
  subjects; assert zero resurrection in all authoritative/derived/artifact
  surfaces.
- [ ] Keep permanent delete disabled if any exact adapter/replay row is not green.

## Task 6: Security and leak corpus

- [ ] Run authorization/IDOR, XSS/Markdown, MIME/archive, network isolation,
  response allowlist, and permission-revoke streaming corpus end to end.
- [ ] Scan logs and generated artifacts for content, secrets, raw URLs, and
  stable protected identifiers.
- [ ] Fix only cross-domain assembly issues here; return domain defects to their
  owning child/branch and rerun after merge.

## Task 7: Performance/capacity and browser acceptance

- [ ] Generate deterministic representative data and measure reviewed operations,
  resource peaks, queues, workspaces, and SQL evidence.
- [ ] Fail on unexpected error, OOM, unbounded growth, silent truncation, or
  reviewed latency/capacity regression.
- [ ] Run six-surface desktop/390px semantic/state/geometry/overflow/keyboard/
  focus/touch/reduced-motion/Axe matrix.
- [ ] Prove test fixtures and helpers do not enter production bundles.

## Task 8: Full gate and handoff

- [ ] Run full Go, Web, browser, both integration profiles, both recovery
  profiles, `git diff --check`, and `trellis-check` on one commit.
- [ ] Generate the final capability matrix and permanent-delete enable/disable
  decision with exact reasons.
- [ ] Update executable specs for implemented backup/restore/integration
  contracts.
- [ ] Merge through protected main, verify CI, and archive Child 11.
- [ ] Run the parent final cross-child audit; do not deploy staging or begin a
  release workflow as part of this task.

## Required command shape

Final scripts should provide stable entry points comparable to:

```bash
./scripts/run-records-integration.sh --profile local
./scripts/run-records-integration.sh --profile s3
./scripts/run-records-recovery.sh --profile local --all
./scripts/run-records-recovery.sh --profile s3 --all
make verify-go
PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web
git diff --check
```

Exact script names are finalized against code before task start. A skipped real
PostgreSQL/S3/browser/recovery suite is not passing evidence.

## Rollback

The integration/backup/restore additions are feature-gated and add no root
schema. Failed restore targets are isolated and disposable. Revert assembly
wiring or keep affected capability disabled; do not claim success through a
partial adapter set or introduce a staging cutover to compensate.

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

- [x] Children 1-10 and Child 12 are on protected `origin/main` `6a37448d`
  (v0.73.1). Child 12 = `c7081519` / fix `3418e0ca`; Child 10 = `9e910d7c`.
- [x] Inventory recorded in
  `research/current-main-inventory-2026-08-21.md`. Paths below follow that
  inventory, not the original guessed `recordbackup` / script names.
- [x] Ran inventoried Postgres suites via the local profile script
  (2026-08-21). Witness/watch passed. Portability deletion seed fails on
  Alpine `blob_key ~ '{1,512}'` — recorded in
  `research/local-profile-2026-08-21.md` and returned to the portability
  owner. MinIO suite not run (no `--profile s3` yet). Do not add persist
  paths or quarantine rows.
- [x] `trellis-before-dev` loaded backend quality / directory / error /
  logging / database / portability / evidence / authorization plus branch
  governance. Backend `index.md` has no separate Pre-Development Checklist
  section; the Guidelines Index plus those files were used.
- [x] Child 10 membership gate and witnessed tombstone reader exist and
  fail-closed. Production bootstrap already forbids `AdmissionGateFunc` and
  nil witness. Do not add quarantine row storage.
- [x] Root migration maximum is `0058`. Child 11 adds none.
- [x] Local profile uses `TMPDIR=/tmp` workspaces and the existing
  PostgreSQL 16 fixture runner. Node 22 / MinIO baselines remain for
  `--profile s3` and Task 8.

## Task 1: Exact adapter/capability registry

**Files:** `internal/center/recordreadiness/` (new composition package).
Reuse `recorddeletion.RequiredAdapterNames` / `recorddeletion.Adapter` /
`recorddeletion.NewRegistry` for the deletion family. Do **not** invent
`record_markdown_client` or `record_comparison` deletion adapters, and do
**not** write missing search/collaboration/portability recovery logic.
`cmd/houfeng-center/bootstrap.go` later composes what exists; permanent
delete stays on `handlers.RecordDeletions(nil)` until the matrix is all
green.

Expected capability families (compile-owned):

- deletion: the nine `recorddeletion.RequiredAdapterNames`
- recovery: `record_core`, `record_attachments`, `record_evidence`,
  `record_search`, `record_activity_projection`, `record_collaboration`,
  `record_portability` (markdown/comparison have no recovery contract)
- authority: `deployment_membership`, `source_deletion_witness`
- orchestration: `backup.orchestration`, `restore.replay` (Child 11 Tasks 2-3)

- [x] Write RED tests for exact expected kinds, duplicate/unknown/missing/
  incompatible/unhealthy adapters and permanent-delete readiness
  (`internal/center/recordreadiness/registry_test.go`,
  `TestBootstrapWiresRecordReadinessRegistry`). Verified 2026-08-21:
  NewRegistry stub is not `ErrInvalidCapabilityRegistry`; Evaluate is
  unreached; bootstrap lacks `recordreadiness.NewRegistry(`. Do not
  implement GREEN in the same step.
- [x] Implement aggregate registry and content-safe status matrix (reason
  codes and kind names only; no Markdown/comment/evidence/attachment/
  archive/credential/`DATABASE_URL` text). Verified 2026-08-21:
  complete healthy fixture enables PD; incomplete/unhealthy/closed keep
  it disabled; Encode has no `note` and no leaked Health text.
- [x] Wrap child-owned adapters that already exist; missing kinds stay
  named `missing` and return to their owner. Production assembles core/
  attachments/evidence/search/activity/collaboration/portability
  deletion plus present recoveries; markdown/comparison stay unnamed
  here.
- [x] Compose membership/witness probes into Evaluate: nil/typed-nil are
  construction-closed; stale, wrong-deployment, discontinuous, and outage
  stay Evaluate-closed before any business write or protected read.
- [x] Add a bootstrap source ratchet for `recordreadiness.NewRegistry(`.
  Keep `RecordDeletions(nil)` until the decision is enabled.
- [x] Run focused `go test ./internal/center/recordreadiness ./cmd/houfeng-center -run 'Readiness|Registry|BootstrapWiresRecordReadiness'`
  and full `./internal/center/recordreadiness ./cmd/houfeng-center`
  packages (2026-08-21, both ok).

## Task 2: Typed backup manifest and staging

**Files:** current main has no backup package or CLI. Create focused
`internal/center/recordbackup` and `cmd/houfeng-backup` only after Task 1
registry slots exist. Do not reuse `cmd/houfeng-record-platform-admin`
as a backup binary (it is APP ACL migrate/bootstrap/finalize only).

- [x] Write canonical manifest, tamper, unknown-version, content-allowlist, and
  deterministic digest RED tests. Watched 2026-08-21: Encode leaked stub
  `note`/URL; NewService/CLI were unimplemented.
- [x] Implement plan/create/verify with staged database/external objects and
  atomic manifest publish. Manifest is published last; Plan performs no
  ArtifactStore writes.
- [x] Add local/S3-compatible artifact conformance and all failure cutpoints.
  Local and S3 share `houfeng-record-backup/v1`; profile is a typed field.
- [x] Verify cleanup receipts for partial files, multipart uploads, pins, and
  workspaces. Failed Create never publishes the manifest.
  `go test ./internal/center/recordbackup ./cmd/houfeng-backup -count=1` ok.

## Task 3: Isolated restore runtime

**Files:** current main has no restore package or CLI. Create focused
`internal/center/recordrestore` and `cmd/houfeng-restore` only where
needed. Restore must call existing domain `NewRecoveryAdapter`
implementations; replay deletions before activity/search rebuild.

- [x] Write state-machine RED tests for non-empty target, incompatible build/
  migration/adapters, missing/tampered bytes, and each cutpoint. Watched
  2026-08-21: NewService/CLI unimplemented.
- [x] Implement plan/apply/verify into an isolated fresh target.
- [x] Restore database/objects, call adapters, replay deletions, rebuild
  projections, converge current APP ACL, and publish readiness in order.
  Replay is ordered before search/activity rebuild.
- [x] Prove retry/cleanup and zero serving/worker activity before readiness.
  Failed Apply never reports ready or starts serving/workers.
  `go test ./internal/center/recordrestore ./cmd/houfeng-restore -count=1` ok.

## Task 4: Integration profiles and happy-path matrix

- [x] Build deterministic PostgreSQL 16 + local storage profile.
  `scripts/run-records-integration.sh --profile local` plus
  `recordbackup.NewLocalStore` / restore roundtrip. Docker fixture
  cleanup is `docker rm -f`; SKIP is failure.
- [x] S3 store constructor and script `--profile s3` share the same
  manifest/report contract (`NewS3Store`, MinIO env). Full MinIO lane
  not executed on 2026-08-21.
- [x] Exercise inventoried real-store workflows (witness, watch,
  local backup/restore). Official ZIP/HTTP export-import remains the
  existing portability suite; do not add a second exporter. Portability
  deletion PG seed is a returned domain defect.
- [x] Emit commit/config/manifest-bound content-safe reports
  (`houfeng-record-profile-report/v1`). Script writes the report only
  after the child tests pass; failed local run correctly omitted it.
  `go test ./internal/center/recordbackup ./internal/center/recordrestore -count=1` ok.

## Task 5: Failure, revoke, deletion, and recovery matrix

- [x] Inject backup/restore durable-boundary failures (database stage/publish
  and restore-database cutpoints). Domain processor/stream/export injectors
  stay in their owning suites; Child 11 does not reimplement them.
- [x] Verify idempotent retry on a fresh isolated target, bounded workspace
  cleanup, and content-safe external-copy disclosure
  (`EncodeExternalCopies` kind/count only).
- [x] Run backup -> replay-delete -> restore: purged `record_evidence` is
  absent at rebuild; survivor attachments remain. A no-op replay with
  `PurgedKinds` is `ErrResurrectionBlocked`.
- [x] Keep permanent delete disabled when exact rows are missing.
  `./scripts/run-records-recovery.sh --profile local --all` (2026-08-21)
  reported `permanent_delete=disabled` and missing markdown/comparison.
  S3 recovery lane not executed this turn.

## Task 6: Security and leak corpus

- [x] Run authorization/IDOR, XSS/Markdown, MIME/archive, network isolation,
  response allowlist, and permission-revoke streaming corpus end to end via
  compile-owned `RequiredSecurityCorpusTests` and
  `./scripts/run-records-security.sh` (2026-08-21, all inventoried tests ok).
- [x] Scan Child 11 manifests, profile reports, readiness Encode, external-copy
  disclosure, scripts, and backup/restore CLI sources with `ScanContentSafe`.
- [x] No new cross-domain assembly leak. Domain defects stay with their owners
  (Alpine `0058` portability-deletion seed from Task 4). The unit corpus did
  not return a new owning-child defect.

## Task 7: Performance/capacity and browser acceptance

- [x] Generate deterministic representative data via inventoried unit capacity
  tests plus `./scripts/run-records-capacity.sh --profile local` with
  `HOUFENG_ACTIVITY_PERF_SCALE=0.001` (minimum 1000 activity rows). Full 1M-row
  default stays owning-child and is not required for Child 11 hardware.
- [x] Fail on unexpected error, OOM, unbounded growth, silent truncation, or
  reviewed latency/capacity regression in the inventoried suites. No 4 GiB
  cgroup harness: Child 11 PRD does not require dedicated benchmark hardware.
- [x] Run six-surface desktop/390px semantic/state/geometry/overflow/keyboard/
  focus/touch/reduced-motion/Axe matrix via `./scripts/run-records-browser.sh`
  (2026-08-21, 64/64 passed).
- [x] Prove test fixtures and helpers do not enter production sources or
  `web/dist` (`ScanProductionBundleSafe` + post-preview dist grep).

## Task 8: Full gate and handoff

- [x] Ran verify-web (191 files / 1239 tests), security, local recovery,
  local capacity, browser (earlier 64/64), and `git diff --check`. Local and
  S3 integration fail on the inventoried Alpine `0058` portability-deletion
  seed. S3 integration also returned MinIO `invalid Blob request` to the
  portability owner. Local `make verify-go` hits a main-owned PNG golden
  mismatch on host Go 1.27 (`TestPreviewImageGoldenMetadataFreeBoundedPNG`);
  Child 11 packages pass; CI uses `go.mod`.
- [x] Final capability matrix: `permanent_delete=disabled` because markdown/
  comparison deletion, three recoveries, and the backup/restore pair are
  missing from production wiring. See
  `research/final-capability-matrix-2026-08-21.md`.
- [x] Added `.trellis/spec/backend/record-integration-contract.md` and index/
  directory/error-handling pointers.
- [x] Pushed `feat/child-11-records-integration-rollout`, opened PR #433,
  required CI green, squash-merged as `79f62aac`. Main CI run
  `32497370438` green. Did not checkout or merge local main. Release
  Please #434 left unmerged (no Child 11 release cutover).
- [x] Parent cross-child audit recorded in
  `../07-13-vps-detail-experience-design/research/final-cross-child-audit-2026-08-21.md`.
  No staging or release.

## Required command shape

`scripts/run-records-integration.sh`, `scripts/run-records-recovery.sh`,
`scripts/run-records-security.sh`, `scripts/run-records-capacity.sh`, and
`scripts/run-records-browser.sh` exist. Recovery `--all` does not rerun the
known Alpine portability-deletion seed defect; that remains inventoried and
returned to its owner.

```bash
# existing portability / witness / deletion integration (env-gated)
HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store \
  -run 'WitnessedRecordSubject|RecordPortabilityDeletion|RecordWatchVersionedDefaultAnchor' -count=1
HOUFENG_MINIO_INTEGRATION=1 go test ./internal/center/portability -run 'MinIO' -count=1

# later Task 4/5 entry points, created only if still missing
./scripts/run-records-integration.sh --profile local
./scripts/run-records-integration.sh --profile s3
./scripts/run-records-recovery.sh --profile local --all
./scripts/run-records-recovery.sh --profile s3 --all
./scripts/run-records-security.sh
./scripts/run-records-capacity.sh
./scripts/run-records-capacity.sh --profile local
./scripts/run-records-browser.sh

make verify-go
PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web
git diff --check
```

A skipped real PostgreSQL/S3/browser/recovery suite is not passing evidence.

## Rollback

The integration/backup/restore additions are feature-gated and add no root
schema. Failed restore targets are isolated and disposable. Revert assembly
wiring or keep affected capability disabled; do not claim success through a
partial adapter set or introduce a staging cutover to compensate.

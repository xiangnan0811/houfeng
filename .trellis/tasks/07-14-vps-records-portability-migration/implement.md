# Records Import, Export, and Portability Implementation Plan

> **For agentic workers:** Use `trellis-before-dev` before product edits.
> RED → verify RED → minimal GREEN → verify GREEN. Do not execute the
> 2026-08-02 Task 1–9 list. Scope B: PR1 then PR2. Do not start Child 11
> after PR1.

**Goal:** Close production admission/witness, deliver human Markdown export
that consumes `comparison.result/v1` Export, then deliver machine archive
and safe import.

**Architecture:** Named `AdmissionGate` / witness types in `store`.
Portability orchestrates existing evidence/activity/markdown/attachment
seams, stores job/origin metadata in `0058`, and stages bytes through the
existing `BlobStore` unless a reviewed cut proves otherwise.

**Tech Stack:** Go/pgx/PostgreSQL, ZIP64/JSON/SHA-256, local/S3 BlobStore,
isolated content processor (PDF in PR2), React/TypeScript.

---

## 2026-08-21 approved scope B

- Baseline: `origin/main` `a5836f33` / `v0.71.0` after #423.
- One Trellis child; two reviewable PRs; exit after both merge.
- Contrast: `research/current-main-reconciliation-2026-08-21.md`.
- No `0059`. No `/monitoring/compare` change. No workbench download.
- No 4 GiB harness, overview manage panel, activity digest expansion, or
  sticky row headers.

## Preconditions

- [x] Children 1–9 on protected main (`v0.71.0`).
- [x] Current-main reconciliation written; Alan chose B.
- [ ] Explicit approval of this planning summary, then `task.py start`.
- [ ] Non-main branch/worktree from that main; `sh scripts/setup-git-hooks.sh`.
- [ ] `trellis-before-dev` for backend database/http/security and Web
  component/state/quality.
- [ ] Reconfirm `0058` is still free and APP ACL fragments still end at `0057`.
- [ ] Baseline GREEN: `make verify-go` and `make verify-web` (Node 22;
  `TMPDIR=/tmp`; `GOCACHE`/`GOMODCACHE` under `$HOME`).

---

## PR1 — authority, 0058, human/comparison export

### Task 1: Named AdmissionGate

**Files:** create a named gate type beside `internal/center/store/record_platform.go`;
modify `cmd/houfeng-center/bootstrap.go` / `bootstrap_test.go`; tests in
`record_platform*_test.go` and a real PostgreSQL membership fixture.

- [ ] RED: nil/typed-nil, empty membership, stale heartbeat, wrong
  `deployment_id`, contract drift, typed-nil tx →
  `ErrRecordPlatformAdmissionUnavailable`; 0 writes.
- [ ] GREEN: `Admit` reads `deployment_membership` +
  `deployment_contract_state` on the caller tx. Identity bound at
  construction.
- [ ] Bootstrap wires the named type. Source still contains no
  `store.AdmissionGateFunc(`.
- [ ] `go test -race ./internal/center/store ./cmd/houfeng-center -run 'Admission|Bootstrap' -count=10`

### Task 2: Witnessed tombstone reader

**Files:** `internal/center/store/record_subjects.go` and tests;
bootstrap `NewRecordSubjectReadResolver(subjects, witness)` no longer nil.

- [ ] RED: digest-only `source_deletion_tombstones` row is rejected;
  missing/stale/unknown-version/unreachable witness fail closed.
- [ ] GREEN: successful witness populates `WitnessedRecordSubjectTombstone`
  with final floor. Evidence resolver stays live-only (no local fallback).
- [ ] `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store -run 'WitnessedRecordSubject' -count=1`

### Task 3: 0058 + ACL + store

**Files:** `db/migrations/0058_create_record_portability.sql`;
`internal/center/store/record_portability.go`;
`internal/center/store/migrate/app_acl_current_contract.go` fragment;
migration/ACL tests.

- [ ] RED schema/constraint/index/TTL/content-allowlist.
- [ ] Fragment lists exact objects and runtime/admin privileges. Prove no
  duplicate membership/contract tables.
- [ ] Store CAS/idempotency for export jobs (import tables exist, unused).
- [ ] Fresh/repeat migration + current convergence/admission.

### Task 4: Human Markdown + comparison.result/v1 consumption

**Files:** create `internal/center/portability/` preview/export service;
handlers `record_portability.go`; config flag; Web lazy export on record
center / revision / detail — **not** `RecordComparisonPage`.

- [x] RED: unauthorized material, inventory drift, capability off, body
  limits, comparison download byte-equals
  `ComparisonResultKind.Export`; Summarize allowlist; forbidden
  `conclusion`/`markdown` fields stay absent.
- [x] GREEN: `POST /api/record-export-previews`, `POST /api/record-exports`,
  `GET /api/record-exports/{id}`, `GET /api/record-exports/{id}/content`.
  Markdown via `SafeDocumentHTML`. Comparison/evidence via existing
  `Export` / `ExportAdapter`. Activity via `ActivityExportReader` when
  included.
- [x] Stage through `BlobStore` wrapper. Lease before headers and during
  stream; revoke stops new bytes.
- [x] `HOUFENG_PORTABILITY_ENABLED` default false; requires records-enabled.
- [x] Web: no download control in `web/src/pages/records/compare/*`.
- [x] `go test -race ./internal/center/portability ./internal/center/http/handlers ./internal/center/evidence -run 'Portability|RecordExport|ComparisonResult' -count=10`
- [x] `cd web && nvm use 22.23.1 && npx vitest run` on the new export tests
  plus `src/pages/records/compare` to prove no download chrome.

### Task 5: PR1 deletion adapter + quality

- [x] Register `record_portability` surfaces for the `0058` tables.
- [x] Focused Go/Web gates, `git diff --check`, `trellis-check`.
- [ ] Do **not** commit, archive, or open a PR until Child 10 (PR1 + PR2)
  is finished and Alan asks for delivery.

---

## PR2 — machine archive, import, PDF, quarantine

Same Trellis child and preferably the same branch after PR1 merges, or a
follow-on branch from the new main. Reconfirm `0058` is the latest root
migration before adding code (no new number).

### Task 6: Archive v1 conformance

**Files:** `internal/center/portability/{archive,manifest,canonical}.go`
and testdata.

- [x] RED: path normalization, links/devices, collision, size/count/ratio/
  depth/working-set, hash mismatch.
- [x] GREEN: ZIP64-capable `houfeng-record-archive/v1` writer/reader,
  deterministic bytes, canonical manifest. Not yet wired into export HTTP.
- [x] Bounded fuzz + working-set/ratio/depth corpus; wire `archive` export
  kind through the existing preview/create path.

### Task 7: PDF derived presentation

- [x] Same RenderModel as Markdown. Semantic parity tests; PDF is not
  authority.
- [x] Isolated processor **deferred to Child 12**. Child 10 keeps the
  derived RenderModel + in-process stub.

### Task 8: Import quarantine, dry-run, apply

- [x] RED: dry-run writes 0 domain rows; hostile ZIP; untrusted
  auth/role/path (top-level JSON); official markdown URLs/verbs allowed;
  remap; idempotent replay; exact lock CAS; atomic multi-document apply;
  empty/foreign actor fail-closed; origin conflict before writes;
  official `kind`+`schema_version` evidence envelope.
- [x] GREEN: `POST /api/record-imports/dry-run`,
  `POST /api/record-imports/{plan_id}/apply`. Apply uses
  `ImportDocumentsFinishing` so documents, origin, and job terminal
  state share one platform transaction. `LoadImportJob` /
  `ClaimImportJob` select `actor_id`.
- [x] Rebuild search/activity; never import checkpoints.
- [x] Unknown schemas fail-closed from the local registry. Archive
  `optional:true` is not trusted. Quarantine persist abandoned.
  Known comparison/evidence remaps and call the evidence importer.
  Snapshot-row persist is Child 12.

### Task 9: Origin tombstone, Web import, handoff

- [x] Official restore/re-import of the same archive SHA-256 fails on
  origin tombstone (dry-run and apply). Purge writes tombstones from
  import mappings and origins.
- [ ] Lazy Web import workflow: loading/progress/warning/error exist.
  Independent revoked/deleted states and 390px/keyboard contract tests
  are still open.
- [x] Local staging conformance. MinIO/Postgres integration runs are
  Child 11 (`HOUFENG_*_INTEGRATION=1`).
- [x] Sensitive topology requires `record.export_sensitive_topology` and
  a short-lived confirm token.
- [x] Search-page export targets the selected result row, not always
  `visibleRecords[0]`.
- [ ] Merge PR2; archive this child only after the **narrowed**
  `P-AC-01`–`P-AC-15` are on protected main. Child 12 owns evidence
  persist / attachments / PDF isolation. Child 11 owns integration runs.
  Then Child 11 may reconcile.

## Validation commands

```bash
# PR1 focused
go test -race ./internal/center/store ./internal/center/portability \
  ./internal/center/http/handlers ./cmd/houfeng-center \
  -run 'Admission|Witnessed|Portability|RecordExport|ComparisonResult' -count=10
HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store \
  ./internal/center/store/migrate -count=1
make verify-go
# web: nvm use 22.23.1
make verify-web

# PR2 adds
go test -race ./internal/center/portability -run 'Archive|Import|Origin|PDF' -count=10
```

Use `TMPDIR=/tmp`. Keep `GOCACHE`/`GOMODCACHE` under `$HOME`. Do not
`git add .tmp/`.

## Risky files / rollback

- `cmd/houfeng-center/bootstrap.go` — gate/witness wiring; keep the
  `AdmissionGateFunc` ratchet.
- `internal/center/store/record_subjects.go` — do not accept digest-only
  tombstones.
- `internal/center/evidence/comparison_result_kind.go` — consume, do not
  fork Export.
- `web/src/pages/records/compare/*` — no download UI.
- Feature-off hides routes/workers. `0058` is additive; rollback to
  pre-0058 code rebuilds the development database.

## Follow-up before `task.py start`

- [ ] Alan approved this summary in a later message (choosing B is not
  start approval).
- [ ] Working location is a non-main branch from current `origin/main`.
- [ ] `0058` still unoccupied.

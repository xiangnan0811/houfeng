# Records Import, Export, and Portability

## Goal

Give the single operator authorized Records export and safe import, and close
the production admission/witness hole that earlier children fail-closed toward,
without converting `experience_logs` or folding Asset JSON import into Records.

## Background

Children 2–9 are on protected main (`v0.71.0` / `a5836f33`, comparison #423).
The 2026-07-14 / 2026-08-02 task list is historical. Scope **B** (2026-08-21):
one Trellis child, two reviewable PRs, exit only after both land. Authority:
`research/current-main-reconciliation-2026-08-21.md`.

Confirmed on that baseline:

- `0058` is free. Current APP ACL fragments stop at `0057`.
- `store.AdmissionGate` exists; production injects nil. `AdmissionGateFunc` is
  a test adapter and is forbidden in bootstrap.
- `WitnessedRecordSubjectTombstoneSource` exists; production passes nil.
  `0051.source_deletion_tombstones` is digest-only and insufficient.
- `comparison.result/v1` already has `Export` / `Summarize` / renderer. The
  workbench has no download surface; Child 10 must consume those APIs.
- `evidence.ExportAdapter` and `activity.ActivityExportReader` exist.
  Collaboration `PortabilityAdapter` is Backup/Restore, not archive export.
- `recordmarkdown.SafeDocumentHTML` is the human render path. No document PDF.
- Planned `ExportProvider` / `ImportParticipant` types are not in Go.
- Asset JSON import (`internal/center/importing`) stays independent.

## Requirements

### Authority (PR1)

- `P-AUTH-01` Implement a named transaction-scoped `store.AdmissionGate` that
  reads existing `0051` `deployment_membership` and `deployment_contract_state`
  in the open `pgx.Tx`. Bind the activated deployment identity at construction.
  Reject nil, typed-nil, stale, incomplete, wrong-deployment, and unreachable
  authority before any business write. Do not add a gate table in `0058`. Do
  not use `AdmissionGateFunc` or any allow-all in production wiring.
- `P-AUTH-02` Implement `WitnessedRecordSubjectTombstoneSource` so
  `NewRecordSubjectReadResolver` is no longer constructed with nil. A local
  digest-only `source_deletion_tombstones` row cannot satisfy the contract.
- `P-AUTH-03` Wire the named constructors in center bootstrap. Empty or
  unactivated membership still fail-closes writes. Child 11 still owns
  aggregate composition proof and `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED`.

### Persistence (PR1 schema, PR2 uses remaining tables)

- `P-MIG-01` Create `0058_create_record_portability.sql` once in PR1 with
  export jobs/artifacts, import jobs/plans/artifacts/mappings, origins,
  origin tombstones, and purge receipts. No `0059`.
- `P-MIG-02` Register a current APP ACL fragment for `0058`. Privileges match
  DDL. Fresh/repeat migration and runtime admission stay read-only on exact
  repeat.

### Human export and comparison consumption (PR1)

- `P-EXP-01` Human Markdown uses `recordmarkdown` RenderModel /
  `SafeDocumentHTML`. Unavailable or unauthorized material is named, not
  silently omitted as if present.
- `P-EXP-02` Comparison material is produced only by
  `ComparisonResultKind.Export(snapshot, ExportModeSafe|ExportModeSensitiveTopology)`
  and `Summarize`. Do not add a second comparison exporter. Do not add
  download chrome on `/records/compare`. Do not invent comparison CSV;
  that kind returns `application/json`.
- `P-EXP-03` Other evidence uses `evidence.ExportAdapter`. Activity archive
  pages use `activity.ActivityExportReader`. New domain methods stay in the
  owning package; portability does not import `activity` / `recordsearch`
  internals and `evidence` does not import `records` or `portability`.
- `P-EXP-04` Export preview fixes record set, revision range, material
  inclusion, authorization/fence/readiness, expected files/bytes, and expiry.
  Publish rechecks mutable gates. Drift publishes nothing.
- `P-EXP-05` Download re-authorizes and checks the content lease before
  headers and during stream. Revoke or deletion reservation stops new bytes.
  Already delivered copies are disclosed, not recalled.

### Machine archive and import (PR2)

- `P-ARC-01` Machine export is `houfeng-record-archive/v1` ZIP64 with a typed
  canonical manifest, deterministic ordering, per-file size/hash/classification,
  and bounded hostile-ZIP parsing.
- `P-ARC-02` PDF is a derived presentation of the same RenderModel as Markdown,
  generated in the isolated content processor with network disabled. PDF is
  not the machine source of truth.
- `P-IMP-01` Import quarantines bytes, validates path/hash/size/schema, emits
  an expiring dry-run plan, remaps IDs, and applies through domain write
  paths. Archive-declared authorization, role, classification, renderer, SQL,
  path, or URL is never trusted.
- `P-IMP-02` Apply is idempotent and atomic for one plan. Partial failure
  publishes no record, attachment, evidence, collaboration, search, or
  activity state. Search/activity rebuild from imported authoritative rows;
  checkpoints are never imported.
- `P-IMP-03` Imported author/source identity is provenance. Local operator
  and ownership come only from explicit local input under normal policy.
- `P-IMP-04` Integrity-valid unsupported evidence may show locally derived
  allowlisted envelope metadata only. It cannot create a record/snapshot,
  render, compare, apply, or re-export as trusted evidence. Ordinary unknown
  authoritative contracts stay fail-closed in the registry.
- `P-IMP-05` An origin tombstone blocks official restore/re-import of a
  permanently deleted target.

### Platform (both PRs)

- `P-PLT-01` Portability routes and workers are gated by a default-off flag
  stacked on `HOUFENG_RECORDS_ENABLED` (name locked at implement time as
  `HOUFENG_PORTABILITY_ENABLED` unless config review finds a collision).
- `P-PLT-02` Stage artifacts through existing local/S3 `BlobStore` with an
  explicit classification/lease wrapper unless a reviewed cut proves a
  distinct ArtifactStore is required. One conformance suite covers the
  chosen backends.
- `P-PLT-03` Register `record_portability` deletion/backup/restore adapters
  for `0058` surfaces so Child 11 can compose them. Permanent-delete enablement
  stays Child 11.
- `P-PLT-04` Collaboration Backup/Restore stays the Child 11 backup seam.
  Do not treat it as archive export/import.

## Acceptance Criteria

### PR1 (required to open PR2, not sufficient to archive Child 10)

- [ ] `P-AC-01` `0058` fresh/repeat migration and current APP ACL tests pass;
  no duplicate membership/contract tables.
- [ ] `P-AC-02` Named production gate and witnessed tombstone reader fail
  closed on nil, typed-nil, stale, wrong-deployment, digest-only local
  tombstone, and witness outage. Bootstrap contains no `AdmissionGateFunc`.
- [ ] `P-AC-03` Markdown export contains only authorized allowlisted data and
  names unavailable material.
- [ ] `P-AC-04` A saved `comparison.result/v1` download/export byte-equals
  `Export(snapshot, mode)` and its Summarize read model; `/records/compare`
  gains no download control; `/monitoring/compare` is unchanged.
- [ ] `P-AC-05` Export preview/publish rejects auth, revision, inventory,
  fence, or readiness drift without a partial artifact.
- [ ] `P-AC-06` Download lease/revoke tests pass. Portability flag off hides
  routes and stops workers.
- [ ] `P-AC-07` `record_portability` adapter name is registered with `0058`
  surfaces. Focused Go/Web tests and `trellis-check` pass for the PR1 surface.

### PR2 (required to archive Child 10)

- [ ] `P-AC-08` Canonical archive bytes are deterministic for a fixed input;
  hostile ZIP/path/hash/size/schema cases fail closed.
- [x] `P-AC-09` **narrowed (2026-08-21):** derived PDF uses the same
  RenderModel as Markdown. Isolation/no-network processor wiring is Child 12.
- [ ] `P-AC-10` Dry-run reports exact creates/remaps/warnings/blockers/bytes
  and writes no authoritative domain row.
- [x] `P-AC-11` **narrowed (2026-08-21):** apply is atomic for documents +
  origin + job terminal state. Evidence snapshot persist, attachment bytes,
  and collaboration remap are Child 12.
- [x] `P-AC-12` **narrowed (2026-08-21):** unknown schemas fail-closed
  (`ErrImportSchemaBlocked`); archive `optional:true` is not trusted.
  Persisted quarantine / optional opaque envelope display is abandoned
  (parent note). Do not implement quarantine rows.
- [ ] `P-AC-13` Permanent-delete purge of portability content plus official
  restore/re-import cannot resurrect the target (origin tombstone). Child 11
  still owns enabling the delete flag.
- [x] `P-AC-14` **narrowed (2026-08-21):** local unit/race/Web gates exist.
  Real Postgres/MinIO integration runs are Child 11 (tests skip without env).
- [ ] `P-AC-15` Asset JSON import remains independently tested and is not
  part of the Records archive contract.

## Out of Scope

- `experience_logs` migration, backfill, or deletion.
- General cross-version database upgrade; `0059`.
- Anonymous/public links or permanent bearer downloads.
- Executing imported code, SQL, macros, remote URLs, or active content.
- A second comparison exporter or workbench/monitor-compare download UI.
- Comparison CSV; registry-wide Compare; `/monitoring/compare` changes.
- 4 GiB / 512 MiB comparison harness (`scripts/run-comparison-capacity.sh`).
- Overview manage-panel writes; activity group-granted digest expansion;
  sticky comparison row headers.
- ZIP activity pages (rebuild after import). Persisted import quarantine
  rows (unknown schema stays fail-closed).
- Folding Asset JSON import into Records archives.
- Aggregate backup/restore CLI, adapter-registry enablement, and
  `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=true` (Child 11).
- Release, staging, or cutover orchestration.

## Execution Gate

Stay `planning` until Alan explicitly approves this summary. Then
`task.py start` on a non-main branch from `origin/main`. Do not reserve
`0058` with an unmerged placeholder ahead of PR1 work.

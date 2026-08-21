# Child 10 plan reconciliation against `origin/main` `a5836f33` / `v0.71.0`

Date: 2026-08-21  
Baseline: protected main `a5836f33` (`chore(main): release 0.71.0`, #424) after Child 8 `#423`.  
This note is the current-main reconciliation required before `task.py start`. It does not authorize implementation.

The 2026-07-14 / 2026-08-02 / 2026-08-17 Child 10 artifacts stay historical. Do not execute `implement.md` Task 1–9 as written.

## 1. Dependency status

| Planned dependency | On main? | Evidence |
|---|---|---|
| Child 1 foundation (`0051`) | Yes | `deployment_membership`, `deployment_contract_state`, `source_deletion_tombstones` live |
| Child 2 records core (`0052`) | Yes | `/api/records*` + `RevisionCommitCommand` |
| Child 3 attachments (`0053`) | Yes | local/S3 `BlobStore` + upload/download |
| Child 4 evidence (`0054`) | Yes | 7 registered kinds including `comparison.result/v1` |
| Child 9 collaboration (`0055`) | Yes | `PortabilityAdapter` Backup/Restore + deletion adapter |
| Child 5 Markdown workspace | Yes | `recordmarkdown.DocumentRenderModel` + `RenderSafeHTML` / `SafeDocumentHTML` |
| Child 6 search (`0056`) | Yes | rebuild-only projection; no archive import path |
| Child 7 activity (`0057`) | Yes | `#422` → `v0.70.0`; `ActivityExportReader` published for Child 10 |
| Child 8 comparison | Yes | `#423` → `v0.71.0`; workbench has **no** download surface |
| Child 10 portability | No | only Trellis planning artifacts |
| Root migration `0058` | **Free** | latest root file is `0057_create_record_activity.sql`; no `0058` file or ACL fragment |

Children 2–9 are on protected main. The remaining execution-gate gap is this reconciliation plus Alan's scope choice — not a missing child.

`/monitoring/compare` stays a 2-way monitoring A/B tool. Do not use it as a records export or comparison-download fallback.

Asset JSON import (`cmd/houfeng-import-vps-json` + `internal/center/importing`) stays an independent VPS ledger path. Do not fold it into `houfeng-record-archive/v1`.

## 2. Real contracts Child 10 must use

### 2.1 Planned `ExportProvider` / `ImportParticipant` do not exist

`internal/center/portability` is absent. The 2026-07-14 sketches:

```go
type ExportProvider interface {
    Kind() string
    Preview(context.Context, ExportScope) (ExportContribution, error)
    Write(context.Context, FixedExport, ArchiveWriter) error
}

type ImportParticipant interface {
    Kind() string
    Plan(context.Context, ValidatedArchive, IDMap) (ImportContribution, error)
    Apply(context.Context, pgx.Tx, FixedImportPlan) error
}
```

are **not** in Go. Child 10 must define orchestration against the seams that actually shipped, not invent a second exporter family that ignores them.

| Domain | What exists | What it is | What it is not |
|---|---|---|---|
| Evidence | `Kind.Export(CanonicalSnapshot, ExportMode) ExportMaterial`; `evidence.ExportAdapter.Export(ctx, ExportRequest)` | Per-snapshot authorized material. Modes: `ExportModeSafe` / `ExportModeSensitiveTopology`. Media types allowlisted to `application/json` \| `text/csv` \| `text/plain` | No HTTP download. Composition constructs the adapter (`cmd/houfeng-center/evidence_composition.go`) and does not expose it |
| Comparison result | `ComparisonResultKind.Export` / `Summarize` / renderer `comparison_result_v1` | Canonical JSON bytes (`<kind>_v1.json`). Both export modes currently emit the same snapshot bytes. Payload forbids `conclusion` / `markdown` / `title` / secrets | Not a human Markdown/PDF/CSV exporter. Workbench has zero download chrome |
| Activity | `activity.ActivityExportReader` = `Readiness` + `ScanRecordPage`; `ExportReader` implements it | Frozen Child 7 seam “for Child 10 consumers”. Explicit `RecordSelection`, fail-closed if any source is not ready | Not wired in production bootstrap. Not an authoritative import target |
| Collaboration | `recordcollaboration.PortabilityAdapter` `Backup` / `Restore` on `PortabilitySnapshot` | Record-scoped backup/restore of actions/comments/watches/tombstones. Fence-bound | Not archive export. Do not treat Backup/Restore as `ExportProvider` |
| Markdown | `DocumentRenderModel`; `RenderSafeHTML`; `SafeDocumentHTML` (explicitly for “export and print”) | Authorization-safe human render model | No PDF generator. Attachment processor PDF tools are for uploaded files, not document export |
| Records core | create/revise/restore HTTP; deletion adapters | Authoritative write path | No export/import provider |
| Attachments | `BlobStore` local + S3-compatible; deletion adapter | Object bytes + scan/quota | No archive ArtifactStore. No attachment export provider |
| Search | rebuild store + deletion adapter | Derived index | Must rebuild after import; never import checkpoints |
| Deletion registry | required names include `record_comparison` and `record_portability` | Names reserved; production set stays incomplete | No `record_portability` adapter, no reserved portability surfaces. `record_comparison` also has no surface list yet |

Dependency direction stays: `portability -> published domain interfaces`. Domain packages must not import portability. `evidence` must not import `records`. Comparison backend must not import `activity` / `recordsearch`.

### 2.2 `comparison.result/v1` — Child 8 leftover that Child 10 owns

Child 8 locked this:

1. Comparison workbench does **not** grow a download surface.
2. Markdown / PDF / archive / CSV / export job belong to Child 10.
3. Child 10 **must reuse** `ComparisonResultKind.Export(snapshot, mode)`, `Summarize`, and the registered renderer. Do not write a second comparison exporter.

Entry: `internal/center/evidence/comparison_result_kind.go`.

Human Markdown for a saved comparison must be derived from `Summarize` / allowlisted read model plus the **revision** title/body (where human conclusion already lives). It must not invent payload fields `markdown` / `conclusion` — those are forbidden on the kind.

CSV is only legitimate if a kind's `Export()` already returns `text/csv`. `comparison.result/v1` returns `application/json`. Do not add a comparison CSV dialect in Child 10.

### 2.3 HTTP that exists vs planned

Parent `design.md` §19.3 still names these; **none exist**:

| Planned | Reality |
|---|---|
| `POST /api/record-export-previews` | Absent |
| `POST /api/record-exports` | Absent |
| `GET /api/record-exports/:id` | Absent |
| `GET /api/record-exports/:id/content` | Absent |
| `POST /api/record-imports/dry-run` | Absent |
| `POST /api/record-imports/:plan_id/apply` | Absent |

Live nearby surfaces Child 10 may consume, not replace:

- Records create/revise/restore, drafts, search, activity, comparison candidates/comparisons
- `GET /api/evidence/{id}` (allowlisted read model; `source_available`)
- Attachment upload/download
- Asset JSON import CLI (separate product)

### 2.4 Feature flags

| Flag | Default | Notes |
|---|---|---|
| `HOUFENG_RECORDS_ENABLED` | `false` | Registers records/evidence routes |
| `HOUFENG_COMPARISON_ENABLED` | `false` | Stacks on records; needs HMAC keyring |
| `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` | `false`; `true` rejected | Child 11 enablement |
| Portability flag | **Absent** | Should default off / fail-closed like comparison unless Alan chooses otherwise |

### 2.5 ACL / migration

Current APP ACL fragments (exact one per post-`0051` root migration):

- `0052_create_records_core.sql`
- `0053_create_record_attachments.sql`
- `0054_create_record_evidence.sql`
- `0055_create_record_collaboration.sql`
- `0056_create_record_search.sql`
- `0057_create_record_activity.sql`

`0058` is free. Child 8 added no root migration, so no empty fragment was required. If Child 10 adds `0058`, it must also add a current APP ACL fragment (empty only if no APP object is added — the planned job/origin tables are APP objects).

## 3. Admission, witness, quarantine — interface / stub / production

This is the clause most likely to be executed wrong if someone starts from the 2026-08-02 task list.

### 3.1 `store.AdmissionGate`

Interface is real and transaction-scoped:

```go
type AdmissionGate interface {
    Admit(context.Context, pgx.Tx) error
}
```

`AdmissionGateFunc` is documented as a **test adapter**. `allowRecordPlatformAdmissionGate` is the test allow-all (`return nil`). Bootstrap tests **forbid** `store.AdmissionGateFunc(` in production wiring.

Production: `bootstrapDeps.recordPlatformAdmissionGate` is **never assigned**. `withDefaults()` does not set it. `newRecordsHTTPHandlers` treats nil/typed-nil as nil. Nil gate → `ErrRecordPlatformAdmissionUnavailable` before business writes. Comment in bootstrap: default nil path “registers stable transports while every record and evidence operation remains closed.”

There is **no** production type that reads `deployment_membership` + `deployment_contract_state`. Child 10's job is still the **concrete implementation**, not another interface.

Wiring a real gate does **not** by itself open writes: empty/stale/wrong-deployment membership should still fail closed. It changes the *reason* from “no gate” to “membership/contract authority”. Child 11 still owns aggregate composition proof and `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED`.

### 3.2 Witnessed source-deletion tombstone

`WitnessedRecordSubjectTombstone` + `WitnessedRecordSubjectTombstoneSource` exist on `store.RecordSubjectReadResolver`. Production bootstrap calls `NewRecordSubjectReadResolver(subjects, nil)`. Nil witness → tombstoned-source reads fail closed. Evidence `RecordEvidenceSourceResolver` has **no** local tombstone fallback (live sources only).

`0051` table `source_deletion_tombstones` is a **digest-only** local projection (`authorization_floor_digest`). Code comments say that projection is intentionally insufficient to construct `WitnessedRecordSubjectTombstone`. A Child 10 reader that only `SELECT`s this table would violate the already-shipped contract.

Permanent-delete `DeletionWitnessSource` (`postgres_sync` / `s3_worm` full witness) is a related but different surface. `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=true` is still unsupported. Do not pretend the delete-worker witness is the subject-tombstone reader.

### 3.3 Unsupported-evidence quarantine

Ordinary evidence registry: unknown authoritative contract fail-closes. External unsupported kind/schema quarantine (integrity-valid envelope metadata only; no record/snapshot/render/compare/apply/re-export) is **not implemented**. Attachment `quarantined` is the upload scanner state machine, not this contract.

### 3.4 What Child 11 still owns

- Aggregate adapter registry completeness (`record_portability` must exist before permanent-delete readiness)
- Backup/restore orchestration and CLI
- Local + S3 integration profiles for the **assembled** system
- Final enablement evidence
- 4 GiB / 512 MiB comparison mixed-load harness (`scripts/run-comparison-capacity.sh`)

Child 11 must **not** silently implement a missing Child 10 adapter or the production gate.

## 4. Child 7 / 8 leftovers — not default Child 10

| Item | Owner |
|---|---|
| 4 GiB cgroup peak / 512 MiB mixed-load / `scripts/run-comparison-capacity.sh` | **Child 11**. Child 8 left in-process weighted admission + unit tests |
| 390 Artifact “sticky row headers” | Child 8 visual contract vs CSS ratchet. Only `thead th { top: 0 }`. Not Child 10/11 default |
| Overview manage-panel real writes | Independent follow-up (Child 7 leftover) |
| Activity group-granted digest expansion | Independent follow-up |
| Registry-wide Compare, `common_overlap` reaggregation, `/monitoring/compare` changes, new comparison tables, live runtime compare, Markdown-derived metrics, cross-kind scores, anonymous compare links | Child 8 abandoned; need a new kind/task |
| Expired-replay HTTP test stubs `CreateRecord`; workbench save always new draft; same `Idempotency-Key` → 409 | Accepted Child 8 residue, not a portability defect |

## 5. Outdated 2026-08-02 / 2026-08-17 clauses

Must rewrite before start:

1. “Define `ExportProvider` / `ImportParticipant` then implement domains to match” is inverted. Domains already published different seams. Portability composes those seams; new domain methods stay in the owning package.
2. Collaboration `PortabilityAdapter` is backup/restore, not archive export/import.
3. `AdmissionGate` interface + fail-closed nil injection already exist. Do not re-define the interface. Do not ship `AdmissionGateFunc` in production.
4. `source_deletion_tombstones` is not witnessed authority.
5. Evidence `ExportAdapter` already exists; Child 10 adds authorized download/archive consumption, not a parallel evidence exporter.
6. `comparison.result/v1` Export/Summarize already exist; the missing piece is the portability download/job surface.
7. Parent HTTP table omits comparison-candidates (Child 8 added it) and still lists export/import routes that are absent. Keep the UX intent; lock the wire in the revised implement plan.
8. Planned `ArtifactStore` must be reconciled with existing attachment `BlobStore` (local + S3, one conformance idea). A third object-store family needs an explicit reason.
9. `implement.jsonl` / `check.jsonl` are seed-only. Cursor inline does not need them as curated context.
10. “Children 2–9 must merge before start” is now true on `v0.71.0`. The remaining gate is scope approval, not waiting for 2–9.

## 6. Recommended keep / shrink / split

Keep one Trellis child (parent map stays 10 → 11). Do not execute the 2026-08-02 task list as written.

The original child now binds **three different risk classes**:

1. **Production authority** — concrete `AdmissionGate`, witnessed tombstone reader, unsupported-evidence quarantine. This is what earlier children fail-closed toward. Parent still assigns it to Child 10; Child 11 only composes/verifies/enables.
2. **Human export + Child 8 download leftover** — Markdown (and maybe PDF) plus portability-owned consumption of `comparison.result/v1` Export. This is the user-visible “export/下载” hole.
3. **Machine archive + safe import + origin tombstone + ArtifactStore + lazy Web import/export** — the original portability product. Largest surface. Domain providers as sketched do not exist.

Doing all three in one PR on `v0.71.0` is larger than Child 8.

### Keep in Child 10

- `0058_create_record_portability.sql` remains reserved for this child if jobs/origins/artifacts persist. Confirm still free at start.
- Current APP ACL fragment for `0058`; no duplicate `deployment_membership` / `deployment_contract_state`.
- Concrete transaction-scoped `AdmissionGate` reading existing `0051` membership + contract state. Named type, not `AdmissionGateFunc`. Nil/typed-nil/stale/wrong-deployment still fail closed.
- Witnessed source-deletion reader that can populate `WitnessedRecordSubjectTombstone`. Digest-only local rows are not enough.
- Integrity-valid unsupported-evidence quarantine (import-side). Do not change ordinary registry fail-closed behavior.
- Human Markdown export from `recordmarkdown` RenderModel / `SafeDocumentHTML`.
- Portability-owned download/export job that calls `comparison.result/v1` `Export` / `Summarize`. No workbench-local downloader. No second comparison exporter.
- Consume `evidence.ExportAdapter` and `activity.ActivityExportReader` rather than reaching into those tables.
- Portability capability default off, stacked on `HOUFENG_RECORDS_ENABLED`, same pattern as comparison.
- No `0059`. No `experience_logs` conversion. No Asset JSON merge.
- `record_portability` deletion/backup/restore adapter so Child 11 can compose it.

### Shrink

- Do not implement the 2026-07-14 `ExportProvider` / `ImportParticipant` sketches as a second evidence/activity/collaboration API.
- Do not treat collaboration Backup/Restore as archive export.
- Do not add comparison CSV unless `Kind.Export` already emits `text/csv`.
- Do not put download buttons on `/records/compare`.
- Do not create a third object store if `BlobStore` can stage export artifacts with an explicit classification/lease wrapper.
- PDF is a derived presentation of the same RenderModel. It is optional in the first reviewable slice (isolated Chromium already exists for **attachment** processing, not document export).
- Do not import search/activity checkpoints. Rebuild only.

### Split / defer

- 4 GiB / mixed-load comparison harness → **Child 11**
- Aggregate backup/restore CLI and adapter-registry enablement → **Child 11**
- Permanent-delete flag → **Child 11**
- Overview manage panel / activity digest expansion / sticky row headers → **not Child 10**
- Machine ZIP64 `houfeng-record-archive/v1` + quarantine dry-run/apply + origin tombstone + lazy Web import workflow → **same Trellis child, second reviewable PR**, unless Alan splits a new child (that would change the 11-child parent map)

Optional in-child PR split (same Trellis child):

1. **Authority + human/comparison export:** concrete gate + witness reader + `0058` job/artifact metadata + Markdown export + comparison.result/v1 download consumption + default-off flag + portability deletion adapter stub/surfaces
2. **Machine archive + import:** ZIP64 + hostile corpus + remapping + atomic apply + origin tombstone + local/S3 artifact conformance + Web import + unsupported-evidence quarantine UI

## 7. Scope options for Alan

### A — One child, one PR, original full scope

Keep admission + Markdown + PDF + ZIP64 + import + tombstone + ArtifactStore + Web + comparison Export consumption in a single implementation pass.

Trade-off: matches the 2026-08-02 page count; high chance of crossing ownership boundaries and stalling review. Closest to “just start Task 1–9.”

### B — One Trellis child, two PRs (recommended)

Parent map stays 10 → 11. PR1 = authority + human Markdown + comparison.result/v1 consumption. PR2 = machine archive + import + origin tombstone. PDF either joins PR2 or stays a later slice of the same child.

Trade-off: first ship closes the Child 8 export leftover and the fail-closed authority hole without pretending the 2026-07-14 archive product is a weekend. Import/no-resurrection waits for PR2. Child 11 still cannot start until both PRs are on main if we keep Child 10 acceptance as originally written — unless Alan also shrinks Child 10 exit criteria to PR1 and moves archive acceptance.

### C — Split Trellis children

Child 10 becomes authority + human/comparison export only. A new child owns archive/import/tombstone. Parent becomes 12 children or Child 10/11 get rewritten.

Trade-off: cleanest ownership; changes the program map and Child 11 preconditions. Do not do this unless Alan wants a durable replan.

## 8. Decision (2026-08-21)

User chose **B**: one Trellis Child 10; parent map stays 10 → 11; two reviewable PRs.

Locked with that choice:

- Child 10 **exit requires both PRs** on protected main. Child 11 does not start after PR1 alone.
- **PR1:** concrete `AdmissionGate` + witnessed tombstone reader + `0058` (full planned table set, no `0059`) + human Markdown + portability-owned `comparison.result/v1` Export/Summarize consumption + default-off portability flag + `record_portability` deletion adapter surfaces.
- **PR2:** ZIP64 `houfeng-record-archive/v1` + import quarantine/dry-run/apply + origin tombstone + PDF as derived RenderModel presentation + Web import + unsupported-evidence quarantine path.
- Do not execute the 2026-08-02 Task 1–9 order. Authority and human/comparison export come first.

This note still does not authorize `task.py start`. Implementation waits for explicit approval of the rewritten planning summary.

# Records Import, Export, and Portability Design

Scope B against `v0.71.0`. Historical 2026-07-14 `ExportProvider` /
`ImportParticipant` sketches are not the implementation contract.

## 1. Boundary

Portability orchestrates authorized artifacts in and out of Records. It does
not own mutable search/activity projections, does not convert `experience_logs`,
and does not import another package's tables.

It also supplies the concrete production authority earlier children left
fail-closed: a named `store.AdmissionGate`, a witnessed source-deletion reader,
and (PR2) integrity-valid unsupported-evidence quarantine. Child 11 composes
those constructors into the aggregate readiness/enablement story. This child
may wire the named gate/witness into bootstrap; empty membership still
fail-closes writes.

```text
portability -> published domain interfaces
domain packages must not import portability
evidence must not import records or portability
comparison backend must not import activity or recordsearch
```

Collaboration `PortabilityAdapter` Backup/Restore remains the Child 11 backup
seam. Archive export/import is a different contract.

## 2. Modules

- `internal/center/portability`: preview, jobs, workers, policy, remapping,
  archive v1 (PR2), orchestration over domain seams.
- `internal/center/store/record_portability.go`: `0058` rows.
- Named admission/witness types live next to existing store seams
  (`record_platform.go` / `record_subjects.go`), not inside portability, so
  records/evidence can keep depending on `store.AdmissionGate` without
  importing portability.
- `internal/center/http/handlers/record_portability.go`: authenticated API.
- `web/src/pages/records` lazy export (PR1) and import (PR2) workflows.

## 3. Migration 0058

One root migration in PR1, even though import tables stay unused until PR2:

- `record_export_jobs`, `record_export_artifacts`
- `record_import_jobs`, `record_import_plans`, `record_import_artifacts`
- `record_import_entity_mappings`
- `record_origins`, `record_origin_tombstones`
- `record_portability_purge_receipts`

No duplicate `deployment_membership` / `deployment_contract_state`.
Long-lived origin/tombstone rows contain no title, Markdown, filename,
evidence summary, or free-text error.

## 4. Domain seams to compose

Do not re-introduce the unused 2026-07-14 interfaces as a second evidence API.
Portability holds a closed orchestration table that calls:

| Need | Existing seam | Package |
|---|---|---|
| Evidence bytes | `Kind.Export` via `evidence.ExportAdapter.Export` | `evidence` |
| Comparison result | `ComparisonResultKind.Export` / `Summarize` | `evidence` (`comparison_result_kind.go`) |
| Activity pages | `ActivityExportReader.Readiness` + `ScanRecordPage` | `activity` |
| Human document | `SafeDocumentHTML` / `DocumentRenderModel` | `recordmarkdown` |
| Attachment bytes | authorized `BlobStore` + attachment read APIs | `attachments` |
| Records identity | existing record/revision read + `recordauth` | `records` / `store` |
| Collaboration backup | `PortabilityAdapter.Backup` / `Restore` | `recordcollaboration` — Child 11 only |

If a domain cannot preview or write its contribution, add the method **in that
domain** and keep the dependency arrow pointing at portability.

Missing required contributors block preview or apply.

## 5. Authority

### AdmissionGate

Construct a named type (not `AdmissionGateFunc`) whose `Admit(ctx, tx)` reads
`deployment_membership` and `deployment_contract_state` on the caller tx.
Identity is bound at construction. Bootstrap tests that currently forbid
`AdmissionGateFunc(` remain the ratchet.

### Witness

`RecordSubjectReadResolver` gains a real `WitnessedRecordSubjectTombstoneSource`.
The reader must bind external full-witness / contract-activation identity.
Selecting `source_deletion_tombstones.authorization_floor_digest` alone is a
spec violation.

### Quarantine (PR2)

After archive and entry integrity succeed, unsupported optional payloads may
expose allowlisted kind/schema/time/size/digest and “cannot interpret.”
Payload bytes never reach a generic JSON renderer. Ordinary registry unknown
contracts stay fail-closed.

## 6. Export flow

```text
request -> authorized preview -> fixed inventory/token
        -> staged writer (BlobStore wrapper) -> recheck auth/fence/readiness
        -> hash/manifest -> atomic publish -> expiring download
```

PR1 publish formats: Markdown (and comparison/evidence JSON from `Export`).
PR2 adds ZIP64 archive and PDF derived from the same RenderModel.

Modes `safe` and `sensitive_topology` are the evidence export modes already on
`ExportMaterial`. Sensitive mode requires the existing independent capability
and a short-lived confirm token (parent HTTP table).

`/records/compare` is not an export surface. Download entry points are record
center, revision, and record detail — the same places an operator already
holds a record/revision identity.

## 7. Import flow (PR2)

```text
quarantine bytes -> structural/integrity/schema validation
                 -> dry-run + ID preallocation + fixed plan
                 -> operator confirmation
                 -> staged blobs + one domain transaction
                 -> publish or compensate
                 -> rebuild search/activity
```

## 8. HTTP

Keep parent names unless implement-time review finds a collision:

| Method / path | PR |
|---|---|
| `POST /api/record-export-previews` | 1 |
| `POST /api/record-exports` | 1 |
| `GET /api/record-exports/:id` | 1 |
| `GET /api/record-exports/:id/content` | 1 |
| `POST /api/record-imports/dry-run` | 2 |
| `POST /api/record-imports/:plan_id/apply` | 2 |

Flag off: no routes, no workers.

## 9. Deletion and recovery

`record_portability` adapter surfaces are the `0058` tables. Permanent delete
purges content-bearing rows and locators. Origin tombstone + ledger reference
may remain. Child 11 enables the delete flag only after the aggregate registry
is healthy.

## 10. Compatibility and rollback

Archive compatibility only; the development database may be rebuilt.
Feature-off hides routes/workers; additive `0058` rows stay inert.
Unpublished staging is janitor-cleaned.
Rollback to code without `0058` requires rebuilding the development database.

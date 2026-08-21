# Archive restore fidelity implementation

> Child 10 is on protected main (`9e910d7c` / `v0.72.0`). Alan approved this
> child. Reconciled with live `ImportDocumentsFinishing` / attachment /
> processor seams.

**Goal:** Persist known evidence, round-trip attachments, isolate PDF.

## Task 1: Evidence in the apply transaction

- RED: official archive with `EvidenceSnapshotIDs` + `Kind.Export` bytes
  (comparison.result + `monitoring.probe/v2`); after apply,
  `ImportDocumentRequest.EvidencePreparation` is set and the evidence
  participant writes snapshot rows in the finishing transaction;
  origin/job failure still leaves zero records and zero snapshots.
- GREEN: restore wrapper + `RestoreCanonicalSnapshot`;
  `PreparedImportedSnapshot` on `RevisionPreparation`;
  `ImportDocumentsFinishing` copies preparation onto
  `RevisionSaveRequest`; participant persists payload + snapshot on the
  same `RunRecordPlatformTransaction`. `knownKindEvidenceImporter` stays
  a schema gate, not a write channel.
- Second apply of the same plan stays idempotent; origin conflict /
  tombstone still fail before writes.

## Task 2: Attachment bytes

- RED: authorized attachment missing from ZIP or over archive limits is
  named on preview; present bytes restore to BlobStore and bind to the
  imported record via `AttachmentIDs`.
- GREEN: export uses `DownloadService.Open` (or equivalent authorized
  read); import uses `AdmitContent` + BlobStore, not archive MIME/path.

## Task 3: Isolated PDF

- RED: bootstrap source ratchet forbids
  `NewIsolatedDocumentPDFRenderer("")` in production wiring.
- GREEN: `contentProcessorPDFBinary()` supplies a non-empty processor
  path; `ValidateIsolation` + no network; in-process `WriteDerivedPDF`
  remains test-only.

## Validation

```bash
go test -race ./internal/center/portability ./internal/center/records ./internal/center/evidence ./internal/center/store ./cmd/houfeng-center \
  -run 'Archive|Import|Evidence|PDF|Admission|Imported' -count=5
make verify-go
# nvm use 22.23.1
make verify-web
```

Postgres/MinIO integration remains Child 11.

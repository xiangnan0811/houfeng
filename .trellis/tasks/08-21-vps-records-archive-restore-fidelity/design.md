# Archive restore fidelity

## Seams (do not invent a second stack)

Reconciled against `origin/main` = `3c239fa0` (`v0.72.0`).

- Evidence write: `records.RevisionCommitCommand.EvidencePreparation` +
  `store.recordEvidenceRevisionParticipant` (`insertEvidenceSnapshotRow`).
  Child 10 `Apply` still calls `applyImportedEvidence` **before**
  `ImportDocumentsFinishing`; `knownKindEvidenceImporter` only validates
  schema and writes zero `evidence_snapshots` rows. Child 12 attaches a
  reconstructed `evidence.RevisionPreparation` to
  `records.ImportDocumentRequest` so `ImportDocumentsFinishing` →
  `SaveRevisionsFinishing` → `CommitRevisionsFinishing` persists snapshot
  rows (and payloads on the same `RunRecordPlatformTransaction`) with
  documents, origin, and job terminal state. Capture intents are not used
  (no live source). Comparison-save tokens are not forged.
- Archive envelope: official ZIP `Kind.Export` members stay byte-equal
  (`comparison.result_v1.json` and evidence JSON). Restore needs
  `SnapshotEnvelope` for `RestoreCanonicalSnapshot`. Export writes a
  portability restore wrapper (`kind` + `schema_version` + `envelope` +
  `export`) as `records/{id}/evidence/{evs}.json`. Raw
  `comparison.result_v1.json` remains `ComparisonResultKind.Export`.
- Attachments: `records.ExportDocument.AttachmentIDs` + Child 3
  `DownloadService.Open` / `AdmitContent` / `BlobStore`. Archive class
  `attachment` already exists. Members stay under `houfeng-record-archive/v1`
  limits (256 files, 8MiB each, 64MiB pack). Apply binds via
  `ImportDocumentRequest.AttachmentIDs` → existing attachment revision
  participant (`ApplyRevisionAttachments`).
- PDF: `NewIsolatedDocumentPDFRenderer` must receive the
  `houfeng-content-processor` path in `cmd/houfeng-center/bootstrap.go`.
  `processorBinary == ""` falls back to in-process `WriteDerivedPDF` and
  is test-only. Processor already implements `render-document-pdf` in
  `cmd/houfeng-content-processor/document_pdf.go`.

## Order

1. Evidence persist (unblocks comparison after import).
2. Attachment bytes in ZIP + restore.
3. Isolated PDF processor wiring.

## Forbidden

- Store must not import `portability`.
- `evidence` must not import `records` or `portability`.
- Do not fork `ComparisonResultKind.Export`.
- Do not persist quarantine or import activity checkpoints.
- Do not add `0059`, Asset JSON import, experience_logs, backup/restore CLI,
  permanent-delete flag, or a 4 GiB harness.

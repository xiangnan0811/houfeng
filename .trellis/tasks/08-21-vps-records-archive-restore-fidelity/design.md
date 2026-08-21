# Archive restore fidelity

## Seams (do not invent a second stack)

- Evidence write: `records.RevisionCommitCommand.EvidencePreparation` + existing evidence participant. Extend `ImportDocumentsFinishing` / `CommitRevisionsFinishing` so snapshot persist is inside the same platform transaction as documents, origin, and job terminal state.
- Attachments: `attachments` read/export APIs + BlobStore. Archive members stay under `houfeng-record-archive/v1` limits (256 files, 8MiB each, 64MiB pack).
- PDF: `NewIsolatedDocumentPDFRenderer` must receive the content-processor path in production bootstrap. `processorBinary == ""` is test-only.

## Order

1. Evidence persist (unblocks comparison after import).
2. Attachment bytes in ZIP + restore.
3. Isolated PDF processor wiring.

## Forbidden

- Store must not import `portability`.
- `evidence` must not import `records` or `portability`.
- Do not fork `ComparisonResultKind.Export`.
- Do not persist quarantine or import activity checkpoints.

# Archive restore fidelity implementation

> Start only after Child 10 is on protected main and Alan approves this plan.

**Goal:** Persist known evidence, round-trip attachments, isolate PDF.

## Task 1: Evidence in the apply transaction

- RED: official archive with `EvidenceSnapshotIDs` + `Kind.Export` bytes; after apply, snapshot rows exist; origin/job failure still leaves zero records and zero snapshots.
- GREEN: `EvidencePreparation` on the finishing commit; `knownKindEvidenceImporter` writes or is replaced by the records participant path.
- Include `monitoring.probe/v2` (or another non-comparison known kind) in the official fixture.

## Task 2: Attachment bytes

- RED: authorized attachment missing from ZIP is named on preview; present bytes restore to BlobStore and bind to the imported record.
- GREEN: export uses attachment read APIs; import uses existing admission, not archive MIME.

## Task 3: Isolated PDF

- RED: bootstrap source ratchet forbids `NewIsolatedDocumentPDFRenderer("")` in production wiring.
- GREEN: processor binary + `ValidateIsolation`; in-process writer remains test-only.

## Validation

```bash
go test -race ./internal/center/portability ./internal/center/records ./cmd/houfeng-center \
  -run 'Archive|Import|Evidence|PDF|Admission' -count=5
make verify-go
# nvm use 22.23.1
make verify-web
```

Postgres/MinIO integration remains Child 11.

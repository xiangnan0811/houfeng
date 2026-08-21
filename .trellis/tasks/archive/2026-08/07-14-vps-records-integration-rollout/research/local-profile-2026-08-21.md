# Local profile run 2026-08-21

`./scripts/run-records-integration.sh --profile local` used
`scripts/test-record-platform-integration.sh postgres` (PostgreSQL 16 Alpine)
and `TMPDIR=/tmp`.

Passed:

- `go test ./internal/center/recordbackup` (local ArtifactStore + report)
- `go test ./internal/center/recordrestore` (local backup→restore roundtrip)
- `go test ./internal/center/store -run 'WitnessedRecordSubject|RecordWatchVersionedDefaultAnchor'`

Failed (existing Child 10 portability seed, not Child 11 assembly):

- `TestPostgresIntegrationRecordPortabilityDeletionPurgesOwnedRowsKeepsTombstonesAndReplays`
- seed `record_export_artifacts` (`rxa_portdelete`)
- `ERROR: invalid regular expression: invalid repetition count(s)`
- likely `0058` CHECK `blob_key ~ '^[a-z0-9/._-]{1,512}$'` on Alpine/musl
  POSIX (`RE_DUP_MAX` often 255). Child 11 adds no root migration and
  does not rewrite `0058`. Return to the portability owner.

S3 profile was not executed in this run. MinIO env was unset; the script
starts MinIO only for `--profile s3`.

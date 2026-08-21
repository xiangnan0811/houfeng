# Child 11 inventory against origin/main `6a37448d` (v0.73.1)

Inventoried 2026-08-21 before Task 1. Child 11 is the integration owner.
Missing domain adapters return to their owning child; do not invent them here.

## Baseline

- `origin/main` = `6a37448d` `chore(main): release 0.73.1` (#432)
- Child 12 product fix = `3418e0ca` (#431)
- Child 12 feature = `c7081519` / v0.73.0
- Child 10 feature = `9e910d7c` / v0.72.0
- Root migrations end at `db/migrations/0058_create_record_portability.sql`
- No `0059` / `0060`

## Deletion adapters (`recorddeletion.RequiredAdapterNames`)

| Kind | Constructor | Bootstrap |
|---|---|---|
| `record_core` | `recorddeletion.NewCoreAdapter` | not wired into a production registry |
| `record_attachments` | `attachments.NewDeletionAdapter` | not wired |
| `record_evidence` | `evidence.NewDeletionAdapter` | constructed inside evidence composition, not registered |
| `record_markdown_client` | **name only** | none |
| `record_search` | `recordsearch.NewDeletionAdapter` | not wired |
| `record_activity_projection` | `activity.NewDeletionAdapter` | constructed in `newRecordsHTTPHandlers`, assigned to runtime, not registered |
| `record_comparison` | **name only** | none |
| `record_collaboration` | `recordcollaboration.NewDeletionAdapter` | not wired |
| `record_portability` | `portability.NewDeletionAdapter` | constructed, assigned to runtime, not registered |

`recorddeletion.NewRegistry` already rejects nil / typed-nil / duplicate / unknown / overlapping surfaces and `RequireReady` fails on missing or unhealthy. Production bootstrap still calls `handlers.RecordDeletions(nil)`.

Do **not** implement `record_markdown_client` or `record_comparison` deletion adapters in Child 11. Permanent delete stays disabled and the matrix must name those missing kinds.

## Recovery adapters that already exist

| Kind | Constructor | Notes |
|---|---|---|
| record core / deletion replay | `recorddeletion.NewRecoveryAdapter` | replay cursor + surfaces |
| attachments | `attachments.NewRecoveryAdapter` | inventory / pin / verify blob identity |
| evidence | `evidence.NewRecoveryAdapter` | kind registry + repository |
| activity | `activity.NewRecoveryAdapter` | rebuild generation; does not restore purged rows |

No recovery adapter files for search, collaboration, portability, markdown, or comparison. Child 11 reports those gaps; it does not write domain recovery logic.

Existing recovery types do **not** share the design.md `Backup/Restore/ReplayDeletions/Verify` interface. Task 1 owns a composition capability interface only.

## Authority already on main

- Membership: `store.NewDeploymentMembershipAdmissionGate`; Admit fail-closed on nil / typed-nil / stale heartbeat / wrong deployment / unactivated or drifted contract / stale fence / kind not admitted. Unit tests live in `internal/center/store/record_admission_gate_test.go`.
- Witness: `store.NewWitnessedRecordSubjectTombstoneReader`; fail-closed on typed-nil / unreachable witness / missing entry / unknown version / stale hash / wrong deployment. Local `source_deletion_tombstones` digest is never sufficient.
- Bootstrap: `newProductionRecordPlatformAdmissionGate` returns named `*store.DeploymentMembershipAdmissionGate` or nil; incomplete identity errors. `newProductionWitnessedRecordSubjectTombstoneSource` never passes a nil production source; constructor failure yields an empty reader that still fail-closes.
- Source ratchets already forbid `store.AdmissionGateFunc(` and nil witness resolver.

Task 1 composes these into an aggregate readiness decision. It does not reimplement gate SQL.

## Backup / restore / CLI / scripts

Absent on current main:

- `internal/center/recordbackup`
- `internal/center/recordrestore`
- `cmd/houfeng-backup`
- `cmd/houfeng-restore`
- `scripts/run-records-integration.sh`
- `scripts/run-records-recovery.sh`

`cmd/houfeng-record-platform-admin` is APP ACL migrate / bootstrap / finalize only. Reuse it for APP admission on restore; do not turn it into a Records backup CLI.

`scripts/test-record-platform-integration.sh` remains the PG16 / postgres / postgres-s3 fixture runner.

## Existing integration entry points to run, not rewrite

Postgres (`HOUFENG_POSTGRES_INTEGRATION=1`):

- `go test ./internal/center/store -run 'WitnessedRecordSubject|RecordPortabilityDeletion|RecordWatchVersionedDefaultAnchor' -count=1`
- other existing `TestPostgresIntegration*` suites when a profile is up

MinIO (`HOUFENG_MINIO_INTEGRATION=1`):

- `go test ./internal/center/portability -run 'MinIO' -count=1`
- attachment S3 suites already gated on the same flag

Do not add persist, quarantine rows, or Child 12 fidelity tests.

## Flags and closed production rules

- `HOUFENG_PORTABILITY_ENABLED` stacks on `HOUFENG_RECORDS_ENABLED`; both default off
- Production PDF: `contentProcessorPDFBinary()` → `houfeng-content-processor`
- `NewIsolatedDocumentPDFRenderer("")` tests only
- `knownKindEvidenceImporter` is a schema gate, not a write channel
- store must not import portability; evidence must not import records or portability

## Task 1 landing

New package `internal/center/recordreadiness`:

- exact capability kind set (deletion + recovery + authority + backup/restore orchestration)
- content-safe status matrix
- permanent-delete decision stays `disabled` until every required row is present and healthy
- wrap existing child adapters; do not move their purge/replay SQL here

# Research: production v0.79.4 exact-current upgrade blocker verification

- Query: Does the live `fleet.yading.de` v0.79.4 database admit the released v0.79.5 migration set, and is there a repository-supported forward path?
- Scope: internal
- Date: 2026-08-31

## Conclusion

Direct production deployment of v0.79.5 is blocked. The live database is an exact-current revision-1 state through `0062_create_vps_create_idempotency.sql`; v0.79.5 embeds the additional `0063_tune_heartbeat_incident_policy.sql`. `ConvergeAppACLCurrent` classifies the live three-table state as an exact candidate and compares the complete applied and persisted migration sets with the new build before it calls any pending-migration writer. The count mismatch returns `ErrDevelopmentDatabaseRebuildRequired`, rolls back, and prevents `houfeng-db-init` from succeeding.

The safe product outcome is a new patch release, expected to be v0.79.6, that implements and tests one explicit 0062→0063 successor transition. The immutable v0.79.5 tag must not be republished or edited. Production Center and Agents should skip directly from v0.79.4 to that fixed release after release artifacts and an isolated rehearsal are proven.

## Live read-only evidence

All probes used non-interactive SSH and PostgreSQL `default_transaction_read_only=on`. No service, file, container, image, database row, or secret changed.

- Center image: `docker.io/linnea7171/houfeng:v0.79.4`, OCI revision `1481a558b136c2e6e00e59d523fe281acd655ae8`.
- Database migration ledger: 63 rows, tail `0062_create_vps_create_idempotency.sql`; 0063 is absent.
- `public.app_acl_manifest_revisions`: one row.
- `public.app_acl_manifest_head`: revision 1.
- Current head canonical migration body: 4539 bytes.
- Center `/api/healthz`: `status=ok`, `version=v0.79.4`.
- Persisted incident policy: heartbeat `5s`, global missing threshold `20`, sweep `60s`; started/escalated/recovered notification toggles are all enabled. The override body was not printed; its pre-upgrade MD5 fingerprint is `912fb8934aac60e2f770fccd29bd4fd7` for later equality comparison.
- Active incidents: zero rows at the read-only snapshot.
- Recent live evidence: two monitoring instances reported `agent_version=v0.79.4`; the previous ten minutes contained 232 distinct non-backfilled sync batches in total. No instance IDs, display names, payloads, or raw rows were printed.

The manifest query returned exactly:

```text
1
1
4539
```

for revision-row count, head revision, and canonical-migration-set byte length. The query did not print role names, digests, settings JSON, credentials, or database URLs.

A later settings/liveness query printed only the non-secret policy body, override fingerprint, aggregated active-incident groups, and aggregated agent-version counts. One earlier read-only attempt had a shell-quoting SQL syntax error; PostgreSQL remained in `default_transaction_read_only=on` and no mutation occurred.

## Source evidence

- `db/migrations/0063_tune_heartbeat_incident_policy.sql:1-12` is new between v0.79.4 and v0.79.5.
- `internal/center/store/migrate/app_acl_current_contract.go:43-62` registers 0063 as the last explicit empty current fragment.
- `internal/center/store/migrate/app_acl_current_convergence.go:183-200` supports only fresh or an existing three-table exact candidate.
- `internal/center/store/migrate/app_acl_current_convergence.go:220-245` locks and compares the complete applied ledger and persisted manifest before any catalog read or migration write.
- `internal/center/store/migrate/app_acl_current_convergence.go:272-320` calls `applyPending` only for a fresh database.
- `internal/center/store/migrate/app_acl_current_convergence.go:349-369` converts a count/name/checksum mismatch into the typed rebuild-required error.
- `internal/center/store/migrate/acl_manifest_runtime.go:150-199` likewise admits only a single revision-1 manifest matching the current complete migration set.
- `internal/center/deploy/compose_init.go` routes the production `deploy-init --scope compose` path through `ConvergeAppACLCurrent`; there is no fallback to the legacy forward migrator.
- `cmd/houfeng-record-platform-admin/main.go:123-185` exposes current converge plus historical R2/bootstrap/finalize commands, but no current successor/upgrade command.

## Executed PostgreSQL 16 proof

The project-prescribed strict runner was executed from `origin/main@89fcf16af98e3bfcd3927309e1d16f3301195e07`:

```bash
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store/migrate \
  -run '^TestPostgresIntegrationAppACLCurrent$' -count=1
```

It completed with exit 0 and no skip. In particular:

```text
=== RUN   TestPostgresIntegrationAppACLCurrent/prior_baseline_requires_rebuild_without_mutation
--- PASS: TestPostgresIntegrationAppACLCurrent (5.59s)
```

That subtest creates a real exact-current PostgreSQL state, injects one registered future migration, requires `ErrDevelopmentDatabaseRebuildRequired`, and proves the complete durable snapshot is unchanged (`app_acl_current_postgres_integration_test.go:275-310`). This is the same state-classification edge as production 0062→0063.

## Required bridge properties

The supported bridge must remain fail closed and must not become generic prefix adoption:

1. Compile and freeze the current complete source/fragment/catalog contract before opening the transaction.
2. Accept only either exact current state or the one explicitly registered, checksum-exact 0062 predecessor with a revision-1 manifest and unchanged privilege body/catalog.
3. Under the existing advisory lock and one `SERIALIZABLE` transaction, validate the complete predecessor ledger/manifest/catalog before mutation.
4. Apply only the registered pending 0063 source, verify the complete new ledger and effective catalog, append revision 2 bound to revision 1, and CAS-advance the singleton head.
5. Teach both convergence and one-snapshot runtime admission to accept the exact current genesis shape and the exact registered revision-2 successor shape; unknown prefixes, advanced heads, changed privileges, checksum drift, partial application, or malformed chains still fail before unsafe effects.
6. Repeated convergence after the bridge must be read-only; serialization retry must re-run the complete closure.
7. A fresh v0.79.5/v0.79.6 database with one revision-1 manifest through 0063 must continue to work.

## Deployment boundary

Do not run v0.79.5 `docker compose up`, ad hoc SQL, manifest deletion/editing, Records-mode downgrade, or manual Center startup. The next executable production sequence begins only after the bridge is merged, released in a new immutable patch, strict PostgreSQL and Compose-upgrade tests pass, public artifacts are verified, and a full cold restore rehearsal succeeds.

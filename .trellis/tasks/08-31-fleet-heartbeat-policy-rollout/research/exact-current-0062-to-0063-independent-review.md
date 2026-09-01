# Research: independent review of exact-current 0062 to 0063 convergence

- Query: Prove or refute whether a v0.79.4 database migrated through `0062_create_vps_create_idempotency.sql` with an exact-current revision-1 manifest must be rejected by the current release's `ConvergeAppACLCurrent`, and exhaust repository-supported successor/finalize/admin/upgrade bridge entry points, tests, and documentation.
- Scope: internal, repository-only, read-only review
- Date: 2026-08-31

## Findings

### Conclusion

Confirmed, with one precise qualification: given the stated v0.79.4 state (all three current ledger/manifest tables exist, the applied ledger and revision-1 genesis are exact through 0062, and the direct-role/session preconditions are valid), the current v0.79.5 implementation necessarily returns an error satisfying `errors.Is(err, ErrDevelopmentDatabaseRebuildRequired)` before it can execute 0063. No repository-supported current-manifest successor, finalize, admin, Compose-upgrade, or runtime bridge exists.

If an earlier connection, role, source-compile, or catalog precondition is invalid, the invocation can fail earlier with a different error. That does not create an upgrade path; it only means the typed rebuild cause is conditional on reaching the exact-candidate comparison. The stated exact-current production shape reaches that comparison.

This is a current-manifest protocol blocker, not a SQL compatibility blocker. The 0063 SQL has a direct PostgreSQL test showing it can execute after migrations through 0062, but that test bypasses `ConvergeAppACLCurrent`, does not publish a current-manifest successor, and is not a supported deployment entry point.

### Mechanical proof of the rejection

1. The released current source set contains 64 files: the frozen 52-source R1 prefix plus 12 current extensions. The test constants fix that count, and the final two ordered extensions are 0062 then 0063:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/migrate_test.go:19-20`
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/migrate_test.go:165-177`
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/CHANGELOG.md:3-10` identifies the reviewed release pair as v0.79.4 to v0.79.5.
2. 0063 is a real root migration that changes the heartbeat policy default/current value and adds an index:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/db/migrations/0063_tune_heartbeat_incident_policy.sql:1-12`.
3. The current source compiler snapshots the entire embedded migration filesystem before beginning a transaction and requires every post-0051 migration to have exactly one fragment:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_contract.go:793-848`.
4. 0063 has the required explicit empty fragment, so compilation succeeds; “empty” means no APP ACL delta, not “ignore this migration in the canonical source set”:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_contract.go:43-62`.
5. The public writer always supplies the complete current `migrations.FS` and current fragment registry:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:62-81`.
6. An existing v0.79.4 exact-current database has the ledger, revisions, and head tables, so it is classified as `exactCandidate`, not `fresh`:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:178-200`.
7. The exact-candidate branch locks and reads the applied ledger, then compares it against the complete current source set before reading the manifest or catalog:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:204-230`.
8. The 63-row through-0062 ledger cannot equal the 64-entry through-0063 expected set. The count branch deterministically wraps `ErrDevelopmentDatabaseRebuildRequired`:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:349-369`.
9. `applyPending` is reachable only from the `fresh` branch, after requiring an empty ledger. It is absent from the exact-candidate verifier:
   - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:272-320`.
10. Convergence runs in a `SERIALIZABLE` transaction, defers rollback, and commits only after the entire closure succeeds. The count mismatch therefore commits nothing:
    - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:93-130`.

The same fail-closed result survives the obvious unsupported partial workarounds:

- Apply 0063 SQL but do not record it: the ledger still has 63 entries and fails the first count comparison.
- Record 0063 in the ledger but leave the v0.79.4 genesis unchanged: the ledger can pass, but the persisted manifest's 63-entry body then fails the second complete-set comparison at `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:230-245`.
- Append a syntactically valid revision-2 current manifest: convergence rejects every chain with more than one manifest or a head other than revision 1 at `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:234-239`; current runtime admission independently rejects it at `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/acl_manifest_runtime.go:150-180`.
- Rewrite the revision-1 genesis: the only production persistence helper inserts revision 1 and CASes only a null head (`/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/acl_manifest_genesis.go:143-190`), while the foundational migration installs an immutable update/delete/truncate trigger on revisions (`/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/db/migrations/0051_create_record_platform_foundation.sql:1003-1007`). This is not an upgrade mechanism.

### Exhaustive supported-entry-point audit

#### Current admin writer

- `houfeng-record-platform-admin migrate --scope app` is parsed as the only current APP migration invocation:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go:517-531`.
- Its production dependency calls only `ConvergeAppACLCurrent`:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go:136-147`.
- Its safe-error boundary preserves only the rebuild-required sentinel and exposes no repair/successor mode:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go:279-303`.
- The top-level route switch has only bootstrap, finalize, deploy-init, record-authority, and the default migrate route; there is no `upgrade`, `successor`, or current-manifest finalize command:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go:172-185`.

#### Compose upgrade path

- Production Compose runs `houfeng-record-platform-admin deploy-init --scope compose` in `houfeng-db-init`:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/compose.yaml:71-91`.
- Center and authority services are gated on successful completion of that initializer:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/compose.yaml:93-112`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/compose.yaml:139-150`.
- `InitializeCompose` wires its schema step directly to `ConvergeAppACLCurrent`; bootstrap provisioning explicitly does not apply application migrations:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/deploy/compose_init.go:199-214`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/platformmigrate/compose_bootstrap.go:192-214`.
- The initialization order is provision roles, open migrator, call current convergence, and stop immediately with `ErrComposeInitConvergeCurrent` on failure. Authority activation/publication and runtime admission occur only afterward:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/deploy/compose_init.go:257-329`.
- Therefore normal `docker compose up -d` cannot bridge the source-set change. Its rollback point is before the initializer succeeds: leave v0.79.4 running. If any unsupported mutation has begun, repository documentation requires restoration of the complete pre-upgrade cold recovery point, not an image-only downgrade:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/docs/deploy/local-and-systemd.md:402-426`.

#### Center/importer and legacy forward migrator

- With Records enabled, Center performs only `AdmitAppACLCurrentRuntime`; it does not write or fall back to migration:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-center/bootstrap.go:118-150`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-center/bootstrap.go:1202-1210`.
- The VPS importer has the same runtime-only boundary:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-import-vps-json/main.go:220-238`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-import-vps-json/main.go:249-268`.
- `migrate.Apply` is a genuine append-missing-migrations runner (`/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/migrate.go:131-167`), but product selection exposes it only in legacy Records-disabled Center mode. Records-enabled mode is runtime admission (`/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/config/config.go:82-112`). Using it against the production Records-enabled current database would bypass the manifest/ACL protocol and is not a supported bridge.

#### Frozen R1 and isolated R2 APIs

- `ConvergeAppACLR1` snapshots only the exact frozen 0001…0051 prefix, not 0062 or 0063:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_convergence.go:74-99`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_convergence_sources.go:67-99`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_convergence_sources.go:109-147`.
- `bootstrap --scope app-acl-r2` and `finalize --scope app-acl-r2` are distinct CLI routes to historical R2 APIs:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go:150-169`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go:534-565`.
- R2 bootstrap accepts an exact frozen R1 catalog and creates only the R2 receipt/L2 surface:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_bootstrap.go:107-150`.
- R2 finalize advances only exact `PREPARED` to exact `FINALIZED` and writes isolated R2 revision/head tables:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_finalize.go:19-90`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql:55-152`.
- The isolated R2 source contract is fixed at the 52-root R1 sources plus its private 0052 transition and explicitly excludes current migrations after 0051:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_source.go:19-27`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_source.go:80-110`.
- The strict R2 integration suite proves `FinalizeAppACLR2` rejects R1 state and that bootstrap/finalize operate only across R1 → PREPARED → FINALIZED:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_postgres_integration_test.go:59-115`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_postgres_integration_test.go:440-467`.

These historical APIs neither apply root 0063 nor append to `public.app_acl_manifest_revisions`. They are not an alternate current upgrade path.

#### In-memory successor model is not a persisted bridge

- `NewAppACLManifestPersistedV1` can construct revision-2 values in memory and `ValidateAppACLManifestChainV1` can validate a chain:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/acl_manifest.go:552-618`.
- No production current successor persistence function or admin route was found. The only current writer dependency is `insertGenesis`, bound to `insertAppACLManifestGenesisV1`:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go:20-59`.
- The tests use the constructor to prove a valid revision-2 chain is rejected, rather than accepted as an upgrade:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence_test.go:204-258`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_runtime_admission_test.go:180-223`.

### Tests: what they prove and what they do not

- Unit proof: the different-baseline test constructs an existing exact candidate, injects a different compiled source set, asserts the rebuild sentinel, and makes every migration/DCL/manifest/catalog mutation seam fatal. It also proves rollback/no commit:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence_test.go:87-168`.
- Runtime proof: a prior manifest against a different current source returns the same sentinel before catalog read and rolls back:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_runtime_admission_test.go:120-148`.
- PostgreSQL proof of the generic state-classification edge: the current suite includes `prior_baseline_requires_rebuild_without_mutation`; it creates an exact-current state, invokes convergence with a different registered source set, and deep-compares the complete durable snapshot before and after:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_postgres_integration_test.go:19-24`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_postgres_integration_test.go:275-310`.
- 0063 SQL proof: a separate PostgreSQL test applies migrations through 0062, then reads and executes 0063 directly and validates its data/default/index behavior:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/heartbeat_incident_policy_migration_test.go:71-156`
  - the helper's through-name filesystem truncation and generic `applyFS` call are at `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/postgres_integration_test.go:2950-2975`.

Critical coverage caveat: no checked-in test was found that constructs an exact-current revision-1 manifest through the real 0062 source/fragment set and then calls the actual through-0063 `ConvergeAppACLCurrent`. The generic current PostgreSQL mismatch test is direction-independent for the count comparison, but its alternate filesystem comes from `appACLCurrentTestMigrationFS` (the frozen R1 prefix) plus a fake `0052_future.sql`, not the exact 0062 predecessor plus real 0063:

- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_contract_test.go:314-331`
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence_test.go:684-691`
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_postgres_integration_test.go:275-310`.

That missing release-pair fixture does not weaken the direct code proof at the unconditional count branch, but it means the repository has no exact v0.79.4→v0.79.5 supported-upgrade integration evidence. No tests were executed in this review; all evidence above is source inspection, preserving the strict read-only boundary.

### Documentation and contract consistency

- The executable database spec explicitly says current convergence supports only fresh and exact-current, and forbids old-source upgrade, repair, successor append, and null-head adoption:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:319-326`.
- Its matrix requires source count/name/checksum mismatch and valid successor revision to return rebuild-required with no writes or catalog read, and forbids product fallback to R1, R2, or `migrate.Apply`:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:328-343`.
- A later current scenario states the exact behavior directly: previous exact-current plus a future migration returns rebuild-required before DDL and establishes no successor:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:2410-2417`
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:2432-2451`.
- The heartbeat scenario requires 0063's explicit empty fragment but does not define an upgrade exception:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:2474-2497`.
- The Compose spec says db-init calls current convergence and runtime admission:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/directory-structure.md:351-365`.
- The deployment guide's broad claim that “the database initializer applies the supported forward schema transition” is false for an existing exact-current manifest whose source set grew from 0062 to 0063:
  - `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/docs/deploy/local-and-systemd.md:402-426`.
  The same guide correctly requires a cold recovery point and complete restore for incompatible rollback, so its rollback instructions remain applicable even though its forward-upgrade sentence is overbroad.

### Safe repository-only reproduction commands

These are read-only source audits; they do not connect to PostgreSQL, Docker, SSH, or any remote:

```bash
cd /home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance

rg -n 'ConvergeAppACLCurrent|ConvergeAppACLR1|AdmitAppACLCurrentRuntime|migrate\.Apply|BootstrapAppACLR2|FinalizeAppACLR2' \
  cmd/houfeng-record-platform-admin cmd/houfeng-center cmd/houfeng-import-vps-json internal/center/deploy

rg -n 'NewAppACLManifestPersistedV1\(|insertAppACLManifest|ManifestRevision: 2|manifest_revision = 2' \
  internal/center/store/migrate cmd db docs scripts

rg -n '0062_create_vps_create_idempotency|0063_tune_heartbeat_incident_policy' \
  db internal/center/store/migrate .trellis/spec docs

rg -n 'upgrade|successor|finalize --scope|migrate --scope|deploy-init' \
  cmd internal scripts docs compose.yaml .trellis/spec
```

The exact PostgreSQL test that is missing should be added only in a product-code task: materialize 0062 exact-current via injected through-0062 FS/fragments, call actual current convergence through 0063, assert the rebuild sentinel and byte/deep-equal durable state. A future supported bridge would need a separate RED/GREEN test that instead proves checksum-exact predecessor validation, atomic 0063 application, revision-2 append/head CAS, exact-repeat read-only behavior, and matching current runtime admission.

## Files Found

- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence.go` — current writer state machine; only fresh mutates, exact-candidate only verifies, mismatch/successor rebuild.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_contract.go` — closed current source/fragment compiler and explicit empty 0063 fragment.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/acl_manifest_runtime.go` — current runtime admits only one exact revision-1 genesis.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/acl_manifest_genesis.go` — only current manifest persistence helper; genesis-only.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/cmd/houfeng-record-platform-admin/main.go` — exhaustive admin routes and safe error mapping.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/deploy/compose_init.go` and `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/compose.yaml` — production Compose ordering into current convergence, with no fallback.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_convergence.go` and `app_acl_convergence_sources.go` — frozen historical R1 writer/source boundary.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_r2_bootstrap.go`, `app_acl_r2_finalize.go`, and `app_acl_r2_source.go` — isolated historical R2 transitions, not current successors.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/app_acl_current_convergence_test.go`, `app_acl_current_runtime_admission_test.go`, and `app_acl_current_postgres_integration_test.go` — generic mismatch/successor/no-mutation regressions.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/internal/center/store/migrate/heartbeat_incident_policy_migration_test.go` — direct 0062→0063 SQL compatibility test that bypasses current convergence.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md` — executable no-upgrade/no-successor current contract.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/docs/deploy/local-and-systemd.md` — production cold-restore rollback guidance plus an overbroad forward-upgrade claim.

## External References

None. This independent review used only the supplied repository/worktree and the stated v0.79.4 production precondition. It did not access GitHub, Docker registries, hosts, PostgreSQL, or other external systems.

## Related Specs

- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:298-397` — current APP writer/runtime contract and validation matrix.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:2410-2451` — explicit previous-exact-current plus future-migration rebuild rule.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:2474-2497` — 0063 migration and empty-fragment contract.
- `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/directory-structure.md:334-414` — production Compose initialization and recovery contract.

## Caveats / Not Found

- This review did not independently query the live database or v0.79.4 artifact. It treats “63 applied rows through 0062 plus an exact-current revision-1 manifest” as the supplied precondition; the task's separate live evidence records that state.
- No supported current successor writer, revision-2 append/CAS helper, current finalize command, current upgrade command, Compose fallback, migration script, or documentation procedure was found.
- No exact real-0062-manifest to real-0063-current integration fixture was found. Existing tests prove the general mismatch/no-mutation invariant and direct SQL compatibility separately.
- The older spec prose at `/home/murray/code/houfeng/.worktree/staging-heartbeat-policy-acceptance/.trellis/spec/backend/database-guidelines.md:303` and `:357-359` still describes the then-current root set as stopping at 0052. This task explicitly requires updating that current inventory to 64 entries through 0063 while preserving frozen R1/R2 historical counts; until the implementation step lands, the stale prose is not current inventory evidence.
- No tests or mutating commands were run. No product code, specs, remote state, database, Docker resource, branch, or other task file was changed.

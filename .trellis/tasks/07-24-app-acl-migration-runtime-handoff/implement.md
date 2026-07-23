# APP 迁移与运行时 ACL 交接：执行计划

> Active task: `.trellis/tasks/07-24-app-acl-migration-runtime-handoff`
>
> 仅在 `codex/vps-records-platform-acl-migration-convergence` worktree 实施。每个生产改动均先完成 RED → 验证 RED → 最小 GREEN → 验证 GREEN。若发现 `0051` 已被外部应用的证据，停止，不修改该 SQL。

## Task 0：实施前硬门与基线

1. 在开始 DDL 改动前，记录 `git log origin/main -- db/migrations/0051_create_record_platform_foundation.sql`、`git ls-remote --heads origin 'codex/vps-*'`、本 worktree `git status --short` 和当前 0051 checksum；任何外部部署/已记录 checksum 证据都交回控制会话。
2. 运行 `sh scripts/setup-git-hooks.sh`；确认当前分支不是 main/master，且不读取或修改冻结旧 worktree。
3. 先运行当前 focused Go selector，记录已有失败与 skip；不得把它们归因于本 task。真实 PostgreSQL 测试使用 `scripts/test-record-platform-integration.sh postgres -- ...`，若 wrapper skip 则该 selector 不可作为验收。

## Task 1：冻结未发布 r1 的 manifest、projector 与 pgcrypto 访问合同

**Files:**

- Modify: `internal/center/store/migrate/acl_manifest.go`
- Modify: `internal/center/store/migrate/acl_manifest_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_persisted_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_runtime.go`
- Modify: `internal/center/store/migrate/acl_manifest_runtime_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_runtime_postgres.go`
- Modify: `internal/center/store/migrate/acl_manifest_allowlist.go`
- Modify: `internal/center/store/migrate/acl_manifest_allowlist_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier_test.go`
- Modify: `db/migrations/0051_create_record_platform_foundation.sql`
- Modify: `internal/center/store/migrate/record_platform_migration_test.go`

1. Write RED golden tests for a required `migrator_catalog_role` in persisted r1: field omission/mutation/trailing bytes, SQL row/CHECK disagreement and runtime readback must fail. Prove the digest preimage order is `revision → migrator role → previous digest → migration body/digest → privilege body/digest`.
2. Write RED compiler/verifier tests for exactly 204 ACL tuples, zero runtime/admin function tuples, and two fixed owner-only projector definitions. An added runtime/admin projector `EXECUTE` must fail admission.
3. Write a 0051 source regression: no blanket `REVOKE EXECUTE ON ALL FUNCTIONS`; pgcrypto remains exact internal-schema-only; all migrator-owned helpers/projectors still revoke PUBLIC.
5. Run:

   ```bash
   go test ./internal/center/store/migrate \
     -run 'AppACLManifest|CanonicalPrivilege|AppACLEffectiveCatalog|RecordPlatformFoundationMigration' -count=1
   ```

   Confirm each new assertion is RED for the missing field/204 tuple/opaque-extension contract, not a test harness error.
5. Implement the minimal r1 codec/SQL/readback changes; remove only the unsafe blanket revoke, never the individual helper/projector revokes. Refactor expected projectors out of ACL tuples and bind their expected owner to persisted migrator role.
6. Re-run the selector and `git diff --check`.

## Task 2：建立固定 managed surface 与 scoped PostgreSQL catalog reader

**Files:**

- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_postgres.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_postgres_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

1. Write RED tests proving the reader covers only the fixed migration-owned public/internal relation/view/sequence/function inventory plus database/schema/manifest surface; an unrelated schema and its owner's default ACL must be accepted.
2. Add RED cases for a managed wrong-schema/wrong-owner object, managed column grant, runtime/admin internal-schema USAGE/CREATE, migrator global/public/internal default ACL, and added projector grant. Each must be rejected.
3. Add a real PostgreSQL RED case for PG16 `pgcrypto`: a constrained direct migrator creates the extension; extension-member function ACLs are opaque; a direct runtime call to `record_platform_internal.digest` returns SQLSTATE `42501` due to absent schema USAGE.
4. Implement an explicit typed managed-surface inventory and use it in owners/direct/effective/column/function/default-ACL readers. Exclude procedures whose OIDs are `pg_extension` members, not arbitrary functions by name. Preserve global role-attribute and recursive membership checks.
5. Run unit selector and real selector:

   ```bash
   go test ./internal/center/store/migrate \
     -run 'AppACLEffectiveCatalog' -count=1
   scripts/test-record-platform-integration.sh postgres -- \
     go test -v ./internal/center/store/migrate \
       -run 'TestPostgresIntegration(VerifyAppACLEffectiveCatalogR1|RecordPlatform.*ACL)' -count=1
   ```

## Task 3：实现 direct-role preflight 与单事务 scoped convergence

**Files:**

- Modify: `internal/center/platformmigrate/roles.go`
- Modify: `internal/center/platformmigrate/roles_test.go`
- Modify: `internal/center/platformmigrate/postgres_integration_test.go`
- Modify: `internal/center/store/migrate/migrate.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_genesis.go`
- Create: `internal/center/store/migrate/app_acl_convergence.go`
- Create: `internal/center/store/migrate/app_acl_convergence_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

1. Write RED unit tests for tx-scoped helpers: direct `session_user/current_user`, all three role attributes and memberships, lexical 52-source snapshot, `SET LOCAL search_path = pg_catalog, public`, same-tx ledger backfill/lock, no nested pool transaction, full closure retry on `40001`, and no retry for ordinary drift.
2. Write real PostgreSQL RED tests for constrained direct-migrator fresh install; eligible null-head adoption; late SQL/ledger/ACL/catalog/head failure rollback; serialization retry; no partial state; wrong-owner/wrong-schema rejection; `pgcrypto` wrong-schema rejection; and direct runtime/admin manifest-head `UPDATE`/projector calls returning `42501`.
3. Implement transaction-aware ledger and manifest helpers accepting `pgx.Tx`. Do not wrap `migrate.Apply` or `EnsureAppACLManifestGenesisV1`; those APIs retain their legacy behavior.
4. Implement `ConvergeAppACLR1` as the exact retry closure from the design. It must construct/apply grants/revokes only on the fixed managed surface, validate catalog in the same transaction, persist the migrator role, and use a null-head CAS. A complete repeat with exact r1 is read-only.
5. Run:

   ```bash
   go test ./internal/center/store/migrate ./internal/center/platformmigrate \
     -run 'AppACL.*(Convergence|Genesis)|ProvisionRoles|Migration' -count=1
   scripts/test-record-platform-integration.sh postgres -- \
     go test -v ./internal/center/store/migrate ./internal/center/platformmigrate \
       -run 'TestPostgresIntegration(AppACL.*(Convergence|Genesis)|ProvisionRoles)' -count=1
   ```

   Capture the absence of `--- SKIP:` and prove cleanup removes temporary DB roles/databases.

## Task 4：交付受限 scoped migrator CLI

**Files:**

- Create: `cmd/houfeng-record-platform-admin/main.go`
- Create: `cmd/houfeng-record-platform-admin/main_test.go`
- Create or Modify: `internal/center/platformmigrate/app_scope_config.go`
- Create or Modify: `internal/center/platformmigrate/app_scope_config_test.go`

1. Write RED parser tests: only `migrate --scope app` is valid; only APP migrator URL and runtime/admin role names are read; positional secret, unknown flag, witness mode, external DB/S3 input, and runtime/admin DSNs are rejected or left unread.
2. Run:

   ```bash
   go test ./cmd/houfeng-record-platform-admin ./internal/center/platformmigrate \
     -run 'MigrateApp|AppScope|OnlyLoads' -count=1
   ```

3. Implement parser, restricted environment reader, pool lifecycle and `ConvergeAppACLR1` call. Close the pool on every outcome; no DSN/password may enter an error.
4. Re-run selector with fake opener/reader assertions for all forbidden inputs.

## Task 5：实现 one-snapshot runtime admission 并接入 center/importer

**Files:**

- Create or Modify: `internal/center/store/migrate/app_acl_runtime_admission.go`
- Create or Modify: `internal/center/store/migrate/app_acl_runtime_admission_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_runtime_postgres.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_postgres.go`
- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `cmd/houfeng-import-vps-json/main.go`
- Modify: `cmd/houfeng-import-vps-json/main_test.go`

1. Write RED tests for an exported shared `RecordPlatformMode` parser. It must be the first `LoadCenterConfig` operation and run immediately after importer CLI validation but before `os.Open(-file)`: invalid `false/true` and `true/true` fail before URL parsing, `_FILE` secret reads, DNS, connection or VPS JSON file access. `true/false` must select an admission seam that initially fails closed rather than silently using a placeholder writer.
2. Add `bootstrapDeps.admitRuntime` next to `applyMigrations`; write RED center tests proving flags-off retains `applyMigrations`, records-on calls admission exactly once, calls migration zero times, and closes the pool after admission failure. Add `runWithDeps` (including a file opener) or equivalent importer seam so records-on `-import` and DB-opening dry-run make failed runtime open/admission fatal instead of falling back to its legacy warning behavior.
3. Write RED tests proving the real admission uses one `REPEATABLE READ READ ONLY` snapshot for direct identity, manifest chain, 52-file ledger and scoped catalog; a real member login + `SET ROLE` must fail by identity. Extract tx-bound manifest and effective-catalog readers; the existing pool-bound readers cannot be composed because each opens its own transaction.
4. Implement admission with persisted `migrator_catalog_role`, then connect the minimal config/bootstrap/importer branches. Center/importer must open only the runtime pool for `true/false`, never inspect migrator/admin/external-domain configuration there, and must remove/redesign a generic `openRepositories(..., applyMigrations bool)` switch that could become a records-on writer bypass.
5. Run:

   ```bash
   go test ./internal/center/config ./internal/center/store/migrate \
     ./cmd/houfeng-center ./cmd/houfeng-import-vps-json \
     -run 'AppACL.*Admission|RecordPlatform|Bootstrap|Import' -count=1
   rg -n 'migrate\.Apply\(' cmd/houfeng-center cmd/houfeng-import-vps-json
   ```

   Verify every records-on production path is admission-only and every remaining writer call is explicitly flags-off.

## Task 6：完整验证、双阶段审查与交接

1. Run formatting and the complete Go gate:

   ```bash
   gofmt -w cmd/houfeng-record-platform-admin/*.go internal/center/config/*.go \
     internal/center/platformmigrate/*.go internal/center/store/migrate/*.go \
     cmd/houfeng-center/*.go cmd/houfeng-import-vps-json/*.go
   git diff --check
   GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go
   ```

2. Re-run Task 2/3 PostgreSQL selectors and the flags/runtime admission selectors; record exact commands, exit codes and skip-free output in task check evidence.
3. Invoke `trellis-update-spec` and replace the obsolete `database-guidelines.md` APP ACL scenario before requesting the final reviews. The replacement must be an executable managed-surface contract: database + `public` + `record_platform_internal` + fixed `0001…0051` inventory, migrator global/public/internal default ACL only, opaque PG16 extension members behind no internal-schema `USAGE`/`CREATE`, and one `REPEATABLE READ READ ONLY` transaction for runtime admission. It must name the direct-login identity rule, exact SQLSTATE `42501` `digest`/projector regressions, unrelated-schema acceptance, and the required unit/integration selectors. Remove the incompatible whole-database persistent-schema/default-ACL requirement rather than leaving two competing rules.
4. Request first a security/spec compliance review, resolve every P0/P1/P2 and re-review; then request an independent code-quality review and resolve every P0/P1/P2 before commit.
5. Commit only this task's code/tests/docs/spec evidence. Do not mark the parent complete or admit Child 2–11.

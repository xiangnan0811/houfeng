# Focused implementation contracts: current successor and production release

This file is a task-scoped digest of the large executable specs. Implementers and reviewers must still use `trellis-before-dev` and read the relevant source sections before editing; this digest prevents critical rules from being lost when full spec files exceed context-injection limits.

## Current APP writer and runtime admission

Source: `.trellis/spec/backend/database-guidelines.md:298-397`.

- Records-on Compose uses `ConvergeAppACLCurrent` as the sole APP schema/ACL writer and `AdmitAppACLCurrentRuntime` before repositories. It must not fall back to frozen R1/R2 or legacy owner `migrate.Apply`.
- Fragment/source compilation is closed-world and happens before `BeginTx`: every post-0051 migration has exactly one fragment, including explicit empty fragments; invalid/missing/extra/duplicate definitions fail with zero transaction.
- runtime/admin/migrator are distinct direct constrained logins with no membership; session/current identity, owner, direct/effective/column/default ACL and function hardening remain exact.
- current convergence uses hardened search path, an advisory lock and one `SERIALIZABLE` transaction; serialization failure retries the whole closure. Runtime admission uses one `REPEATABLE READ READ ONLY` snapshot.
- `ErrDevelopmentDatabaseRebuildRequired` is the only safe typed database-state cause allowed through the admin boundary; raw database errors, SQL, role data, DSN and secrets remain redacted.
- Existing spec lines 321, 341-342 and 363 currently prohibit old-source upgrade and any successor. This task intentionally replaces only that part with a registered exact predecessor/successor matrix. Null-head adoption, generic prefix acceptance, arbitrary repair and unknown successor remain prohibited.
- The registered predecessor is independently anchored by committed v0.79.4 release goldens (all migration entries/canonical body, deterministic privilege body and revision-1 digest). Product matching and integration fixtures may not co-derive the only oracle from the fixed compiler.
- Before revision 2 is published, transition-specific product verification in the same transaction proves 0063's exact column default, settings conversion/preservation and full index catalog shape. `CREATE INDEX IF NOT EXISTS` must never allow a preexisting wrong same-name index to be blessed by the manifest.
- `AppACLManifestPersistedV1.MigratorCatalogRole` and both canonical bodies are digest-bound. Latest runtime contract is derived from persisted bindings; no migrator credential is read by runtime.
- Frozen R1 and isolated R2 APIs/tests are historical contracts and must not change.

## Heartbeat migration 0063

Source: `.trellis/spec/backend/database-guidelines.md:2474-2527`.

- `0063_tune_heartbeat_incident_policy.sql` is immutable. It changes column default/global old default `3→12`, preserves global custom values and every override, and creates the exact partial covering live-heartbeat index.
- 0063's current fragment is explicit empty and must not expand APP privileges.
- Strict PostgreSQL evidence must execute the migration and validate data/index/runtime query behavior; SQL substring tests or skips are insufficient.
- This upgrade task does not change the N/2N/4N policy, recovery rules, notifications, Agent carrier contract or Settings API/UI.

## Existing CAS spec that must be corrected narrowly

Source: `.trellis/spec/backend/database-guidelines.md:2408-2451`.

- The incident CAS scenario correctly says its own fix must not add a schema revision or ad hoc grant.
- Lines 2417, 2432, 2442 and 2451 encode the old global assumption that any previous exact-current + future migration is permanently rebuild-only. Update them so the CAS task still introduced no migration, while current production now permits only an explicitly registered, exact, tested successor transition.
- Do not rewrite unrelated CAS, backfill ordering, object `xmin`, notification-after-commit or direct-runtime permission contracts.

## Test quality

Source: `.trellis/spec/backend/quality-guidelines.md:113-174` and `645+`.

- Use same-package unit tests and table-driven cases for state matrices; fake pgx transactions are appropriate for ordered calls/cutpoints/rollback, but not for PostgreSQL catalog, trigger, DDL, transaction or permission proof.
- `app_acl_current_postgres_integration_test.go` is the current strict integration anchor.
- Execute through `scripts/test-record-platform-integration.sh`; output must contain actual RUN/PASS for the intended top-level anchor and no SKIP/zero-test evidence.
- Preserve the required PG16 catalog lane across 16.0, 16.6 and 16.12, plus the general Postgres 16.12 runner used locally.
- Full completion requires exact repository Go toolchain (`GOTOOLCHAIN=go1.26.2`), full Go/Web/E2E gates, diff hygiene and independent review after fixes.

## Logging and evidence privacy

Source: `.trellis/spec/backend/logging-guidelines.md`.

- Never record DB URLs/passwords, Compose `.env` values, session/enrollment/sync tokens, Telegram bot/chat secrets, Feishu webhook, raw Settings bodies, generated Agent installer command or raw production payload/log content.
- Remote evidence uses stable version/digest/status/count/fingerprint fields and tightly bounded sanitized logs. Exact error text is private when it can contain infrastructure or business identity.
- Agent token preservation is verified by private byte comparison/digest, never by printing content.

## Branch, PR, release and cleanup

Source: `.trellis/spec/guides/branch-workflow-governance.md:7-194`.

- Local/remote main are protected; all work stays on `codex/fleet-heartbeat-policy-rollout`, hooks enabled, PR targeting main.
- PR creation is not completion. Monitor/fix required CI, merge only when green, then verify exact-main CI, Release Please, release workflow, published Center image and Agent/deployment artifacts.
- A release-worthy bugfix must continue through the release PR and `publish-images` verification.
- Release trust roots are exact commits: v0.79.4=`1481a558b136c2e6e00e59d523fe281acd655ae8`, v0.79.5=`e427f41b73b3b799f581274ebb1ad11ced56f421`, base=`89fcf16af98e3bfcd3927309e1d16f3301195e07`. Independently allowlist the existing post-release metadata commits `8f8808d4d72de7233f1181cf2f135ebf7818b216`,`1ebae26c54fea96e8e2fed1aa2e47f09ad5e3646`,`c8c1030fa09f111c6a895230393737a51ab5c193` plus their merge; then require the base-to-feature range to contain only this task and the Release Please PR to contain only expected release metadata. The only authorized release target is exact v0.79.6; any version, tag, base or range drift requires new approval.
- After release and production acceptance, inspect all worktrees/dirty paths; archive this task and remove only exact task-owned branch/worktree/temp resources. Do not reset/clean broad paths or delete backups without approval.

## Production-specific decisions

- `hostcram` is the Center at `/root/data/docker_data/houfeng`; `netcup` is arm64 Agent canary; `informaten` is amd64 Agent second.
- Direct v0.79.5 deployment is forbidden. Publish and deploy only exact immutable v0.79.6 containing the bridge.
- Center rollback after 0063 is a complete cold restore with matching old image and authority state, not image-only rollback.
- Production notification routing is global. Acceptance is passive; forced 19/20/recovery/provider-message testing remains in the separate staging task.
- Center and Agent operational scripts are host-specific, fail-fast and evidence-bound: a sanitized, host-wide-locking root wrapper invokes each exact script and publishes a separate zero-exit receipt only after true child success; Center cold, Center cutover, Agent backup and Agent supervisor marker/receipt pairs are all distinct and required by the next gate. Traps cannot mask the original failure, phase complete markers are written only after all invariants pass, and mixed Agent rollback state never starts. Cold-backup deletion, Agent-bundle deletion, and production failed-state quarantine deletion each require separate explicit authorization bound to an exact path and retention receipt.
- Before Agent rollout, audit the exact v0.79.4-to-fixed Agent/queue/installer/env/unit diff. Any semantic change requires a nonempty-queue upgrade/downgrade rehearsal; unchanged code requires recorded diff/source evidence. Three-batch acceptance uses the production 768-candidate-before-dedupe query and a strict PostgreSQL bounded-plan proof.

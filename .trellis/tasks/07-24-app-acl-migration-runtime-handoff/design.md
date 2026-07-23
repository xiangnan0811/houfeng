# APP 迁移与运行时 ACL 交接：技术设计

## 范围与边界

本任务实现一个 APP-only `records-on/delete-off` 垂直切片。它把 r1 的 schema/ACL/manifest 写能力固定在一次性 migrator，把 center/importer 固定为直接 runtime 登录后的只读 admission + 业务访问。它不接入外部恢复域，也不把 records capability 误报为已经获得 Child 2 准入。

```text
one-shot migrator (direct LOGIN NOINHERIT)
  -> SERIALIZABLE transaction / advisory lock / search_path=pg_catalog,public
  -> 0001…0051 ledger + managed-surface ACL convergence + r1 manifest/head CAS
  -> close pool

center / importer (direct runtime LOGIN NOINHERIT)
  -> one REPEATABLE READ READ ONLY snapshot
  -> identity + manifest + ledger + managed-surface catalog admission
  -> repositories; never migration/ACL writer
```

`platform admin` remains an independently bound manifest identity, but this slice gives it neither persistent-function `EXECUTE` nor a production activation/trust path.

## Credentials and deployment preconditions

The three named roles are direct `LOGIN NOINHERIT` identities with no direct or recursive membership. The migration transaction proves `session_user == current_user` before doing any DDL/DCL. Runtime/admin must not own a managed database/schema/relation/sequence/function; the migrator must own the database, the two managed schemas and each managed APP object whenever adoption rather than fresh creation is attempted. This is a capability precondition, not an in-scope repair operation.

Legacy adoption therefore proves current placement, ownership and checksums only. It cannot establish who applied historical 0.59 migrations. A non-owned object, a managed object in a non-`public` schema, an unknown ledger filename, a changed checksum, a non-null/advanced head or a missing required object aborts the transaction without repair.

## r1 DDL, projector and pgcrypto contract

The r1 privilege body continues to bind only `center_runtime` and `platform_admin`; `migrator_catalog_role` is a separate canonical persisted field. Its preimage position is fixed between revision and previous digest, and all Go/SQL/readback validation uses exactly that order. Runtime never resolves the migrator DSN or dynamically trusts an observed owner.

The privilege compiler emits 204 tuples. Both projector functions remain required public catalog objects:

```text
public.record_platform_cas_contract_activation_projection(bytea)
public.record_platform_cas_domain_rotation_projection(bytea)
```

They must have migrator owner, `prokind='f'`, `SECURITY DEFINER`, exactly `search_path=pg_catalog`, one `bytea` overload and explicit PUBLIC revoke. They are **not** ACL tuples: runtime/admin function `EXECUTE` is empty. A future trusted caller must be separately designed and admitted with its own proof boundary; this slice tests that both current APP roles receive `42501`.

`0051` is unshipped, so its blanket `REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA record_platform_internal FROM PUBLIC` is removed. PostgreSQL 16 may leave trusted `pgcrypto` extension members bootstrap-owned and PUBLIC-executable even after a non-superuser tries to revoke them. The supported r1 protection is:

1. `pgcrypto` must be installed only in `record_platform_internal`; an existing extension in another schema is `55000` fail-closed.
2. Each migrator-owned helper/projector keeps its explicit `REVOKE ALL ... FROM PUBLIC`.
3. `PUBLIC`, runtime and admin have neither `USAGE` nor `CREATE` on `record_platform_internal`; a direct runtime call to `record_platform_internal.digest` must fail with `42501`.
4. The scoped catalog reader excludes `pg_extension` member procedures from managed owner/direct/effective function observations. It still verifies the extension schema and the access-denying schema ACL, so the exception cannot hide an accessible function.

## Managed catalog surface

The verifier is intentionally not a whole-database purity checker. Its fixed r1 inventory is derived from the 52 embedded migration sources and includes:

| Included | Excluded |
|---|---|
| current database ACL; `public` and `record_platform_internal` schema ACL | unrelated schemas and their objects |
| relations/views/sequences/functions created by `0001…0051`, `schema_migrations`, manifest tables, projectors | `pgcrypto` extension-member procedures |
| owner / direct / effective / column ACL observations for that inventory | default ACLs of unrelated owners in unrelated schemas |
| role attributes and recursive memberships for runtime/admin/migrator | third-party grants that cannot affect a managed object |
| migrator global, `public`, and `record_platform_internal` default ACL rows | — |

Within the included surface, exactness remains fail-closed: unknown object, wrong owner/schema, direct or effective extra privilege, `PUBLIC`, column ACL, grant option or a migrator-scoped default ACL is drift. The default-ACL query is exactly `defaclrole = migrator AND (defaclnamespace = 0 OR namespace IN ('public', 'record_platform_internal'))`; any returned grant is invalid. This narrow scope permits a shared deployment to retain unrelated schemas without expanding the APP roles' authority.

## Scoped convergence transaction

The runner snapshots the embedded sources and lexical order before a retry loop. Every attempt follows this order on one `pgx.Tx`:

1. Begin `SERIALIZABLE`; execute `SET LOCAL search_path = pg_catalog, public`; acquire `pg_advisory_xact_lock(hashtextextended('houfeng-app-schema-acl-v1', 0))`.
2. Read/validate direct migrator identity and all three roles in the same transaction. Create/adopt/checksum-backfill `public.schema_migrations`, then lock it in `SHARE ROW EXCLUSIVE` through commit.
3. Require an exact subset/prefix of the embedded 52-source map, execute only missing SQL using the same tx, and insert their raw-byte SHA-256 checksums. Old unqualified migrations therefore resolve to `public`, never `$user`.
4. After 0051 has created manifest tables, lock revisions/head. Require null head + no orphan revisions for genesis, or exact existing r1 for an idempotent read-only repeat.
5. Revoke each runtime/admin direct privilege on the fixed managed surface, grant only compiler tuples, validate the scoped catalog from the same tx, build the manifest with migrator identity, insert immutable revision, and null-head CAS.
6. Re-read head/manifest/catalog, then commit. On `40001` (and optional `40P01`) retry the whole closure; any other error rolls back the whole closure. Do not call `migrate.Apply` or `EnsureAppACLManifestGenesisV1`, because both open their own transactions.

Only a transaction-aware ledger/genesis/catalog API may be shared with the legacy paths. The existing owner auto-migrator is retained only when both flags are off.

## Runtime admission and process boundaries

The runtime reader uses one `REPEATABLE READ READ ONLY` transaction to read `session_user/current_user`, revisions/head, `schema_migrations`, and scoped catalog. It verifies canonical chain/migration/privilege bytes, runtime/admin/migrator bindings, direct identity and the complete managed surface before center/importer constructs a repository. A `SET ROLE` mismatch, a missing/advanced head, catalog drift or a pending/unknown migration returns an error without DDL/DCL.

`bootstrapCenter` retains its flags-off `applyMigrations` seam. In records-on/delete-off it opens only the runtime pool and invokes admission; it must not parse non-APP external input. The importer follows the same boundary for `-import` and every DB-opening dry-run path.

## Rollout and rollback

- Run the scoped migrator before switching center/importer to the runtime DSN.
- If convergence fails, the transaction leaves no pending baseline/ACL/head fragment. Correct deployment preconditions, then rerun the migrator.
- If runtime admission fails after successful convergence, do not use an owner fallback; keep records-on blocked until the scoped migrator or deployment correction produces an exact state.
- If 0051 is discovered externally applied, stop before editing it and create a separate forward-migration design.

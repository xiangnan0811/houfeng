# Unified Authorization and Platform Foundation Design

## 1. Boundary

Child 1 no longer owns a future production migration governance platform. It
owns the reusable foundation already on main and one bridge from frozen APP R1
to the exact migration set embedded in the current development build.

The full repository status and scope decisions are in the parent
`research/development-rebaseline-2026-08-02.md`.

## 2. Existing contracts retained

The following are retained and tested:

- canonical migration/privilege encodings and immutable manifest chain;
- direct migrator, direct runtime, and platform admin role separation;
- effective catalog ownership/ACL verification;
- `recordauth.Policy` and actor scope;
- idempotency, outbox, identity guard, deletion and delivery leases;
- foundation deletion/recovery interfaces used by later children;
- frozen APP R1 and isolated R2 code as historical contracts.

R2 bootstrap/finalize/transition code is not deleted in this slice. No current
product entry point calls it to advance Records migrations.

## 3. Current-development contract

Introduce an unversioned current-build compiler beside the frozen compilers.

```go
type AppACLCurrentFunctionContract struct {
	SchemaName      string
	Identity        string
	Kind            string
	SecurityDefiner bool
	Config          []string
}

type AppACLCurrentMigrationFragment struct {
	Migration  string
	Objects    []AppACLManagedObjectR1
	Privileges func(databaseName string) []AppACLPrivilege
	Functions  []AppACLCurrentFunctionContract
}
```

The production registry has the frozen `0001...0051` surface as its base and one
explicit fragment for every later embedded migration. Registration is
closed-world:

- embedded post-`0051` migration without a fragment: reject;
- fragment without an embedded migration: reject;
- duplicate migration/object/privilege: reject;
- privilege for an unmanaged object or unknown subject: reject;
- function hardening for an unmanaged function, or a new managed function
  without an exact hardening contract: reject;
- migration with no APP object: explicit empty fragment.

The compiler returns a slice-backed internal contract containing database, the
two role bindings, all privileges, all managed objects, and persistent function
expectations. Frozen R1 wrappers convert their fixed arrays to the same internal
verification engine, preserving exported R1 comparability and tests.

This extraction avoids two divergent catalog verifiers. The R1 wrapper still
validates the exact frozen checksum set; the current compiler uses the exact
bytes discovered from `migrations.FS` plus the explicit fragment registry.

## 4. Supported database states

The current path recognizes only:

| State | Migrate behavior | Runtime behavior |
|---|---|---|
| Fresh: no APP ledger/manifest/managed objects | apply all sources, converge catalog, insert genesis | unavailable until migrate completes |
| Exact current: ledger + manifest + catalog equal this build | verify and return without mutation | admit read-only |
| Different development baseline | return rebuild-required before mutation | reject with rebuild-required |
| Malformed/partial/drifting current state | fail closed before repair | fail closed |

A nullable historical head, a legacy public ledger, an R1 manifest whose source
set differs from the current build, or any successor state is a different
development baseline. The current path does not adopt, upgrade, repair, or
append a successor. The operator recreates the database.

Frozen `ConvergeAppACLR1` keeps its historical adoption behavior for its tests
and isolated callers; production APP migration switches to the current path.

## 5. Convergence data flow

```text
migrations.FS
  -> snapshot exact SQL/checksums
  -> compile current fragments + base R1 catalog
  -> preflight database state
     -> fresh: serializable apply -> DCL -> catalog verify -> genesis
     -> exact: catalog/manifest verify -> no mutation
     -> other: ErrDevelopmentDatabaseRebuildRequired
```

The current convergence engine continues to use one direct-migrator
`SERIALIZABLE` transaction, advisory lock, hardened search path, checksum ledger,
DCL revocation before allowlisted grants, catalog verification, and immutable
manifest insertion.

The refactor supplies a compiled slice-backed contract/surface to the shared
engine instead of making the engine call frozen R1 globals internally.

## 6. Runtime admission

`AdmitAppACLCurrentRuntime` performs one `REPEATABLE READ READ ONLY` transaction:

1. read database/session/current identity, ledger, manifest, and head;
2. compile the expected current source and privilege contract;
3. reject a different baseline with the typed rebuild-required error;
4. read only the compiled managed catalog scope;
5. verify direct login, roles, ownership, direct/effective ACL, column/default
   ACL absence, and required function hardening;
6. commit the read-only transaction.

The existing `AdmitAppACLRuntime` remains the frozen R1 entry point for
historical tests. Center bootstrap switches to the current entry point.

## 7. Error contract

Expose a sentinel or typed error:

```go
var ErrDevelopmentDatabaseRebuildRequired = errors.New(
	"development database must be recreated for the current embedded migrations",
)
```

Internal errors wrap this value and retain the observed/expected reason without
including connection strings, credentials, raw SQL, or role passwords. The CLI
must wrap, not replace, the convergence error so the actionable message reaches
stderr. Center bootstrap keeps the same cause in its startup error chain.

Catalog corruption that could also occur on an exact current set remains a
specific fail-closed catalog error; it must not be mislabeled as a harmless
rebuild mismatch after mutation has begun.

## 8. Child-owned extension rule

Every later migration-owning child modifies the current fragment registry in
the same PR as its SQL migration and tests. Its fragment is part of that child's
acceptance. Child 1 owns the registry/compiler mechanism and base through
`0051`, not the objects introduced by Children 2-10.

This is intentionally a development-build contract, not an extensible plugin
API. Fragments are compile-time values in `internal/center/store/migrate`.

## 9. Compatibility and rollback

There is no database rollback or down migration. Before later Records migrations,
disabling the new current production routing returns the code to frozen R1
behavior on a fresh R1 database. After later migrations land, rollback means
rebuilding the development database with the selected code version.

If the extraction breaks a frozen R1/R2 test, fix the shared engine while
preserving the frozen wrapper; do not weaken the historical invariant or route
through R2.

## 10. Verification

Required evidence:

- pure compiler/registry tests, including injected future migrations;
- frozen R1/R2 regression tests;
- current convergence unit cutpoint tests;
- current runtime read-only admission tests;
- real PostgreSQL fresh/exact-repeat/rebuild-required tests;
- CLI error-chain and bootstrap routing tests;
- full Go repository verification.

No release, staging, mixed-version, or old-database upgrade evidence is required.

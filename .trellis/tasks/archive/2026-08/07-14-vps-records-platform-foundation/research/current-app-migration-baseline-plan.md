# Current APP Migration Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close VPS Records Child 1 by making the APP migrator and runtime
admission consume the exact migrations embedded in the current development
build, while retaining frozen R1/R2 behavior and requiring old development
databases to be rebuilt.

**Architecture:** Compile the embedded migration snapshot and explicit
post-`0051` ACL fragments before opening a transaction. Adapt the fixed R1
catalog contract into one internal slice-backed verifier/convergence core, then
add strict current-build convergence and read-only admission entry points. Only
fresh and exact-current databases are supported; source-set differences fail
before durable mutation with a typed rebuild-required cause.

**Tech Stack:** Go 1.26, pgx/v5, PostgreSQL 16, embedded SQL migrations,
standard-library `io/fs`, `errors.Is`, and the existing APP ACL manifest/catalog
code.

**Canonical task copy:**
`.trellis/tasks/07-14-vps-records-platform-foundation/research/current-app-migration-baseline-plan.md`.
The matching `docs/superpowers/plans/` file is the local writing-plans mirror
because the repository intentionally ignores that directory.

## Completion note

This execution recipe is complete. Its implementation was delivered through
PR #394 at head `7858b30c`, merged to protected main as `2cbeb1bb`, and passed
post-merge main CI run `30751460764`. Unchecked boxes and local-checkpoint
language below preserve the original reviewed execution gates; they are not the
current task or parent status. The current status is recorded in the child
`prd.md` and `implement.md`: Child 1 is archived and the parent is `1/11`.

---

## Execution Boundary

This document is a reviewed implementation recipe, not authorization to execute
it. During the 2026-08-02 planning pass, do not edit production Go/SQL, change
task status, commit, push, or create a PR.

Before later execution:

- read `.trellis/tasks/archive/2026-08/07-13-vps-detail-experience-design/research/development-rebaseline-2026-08-02.md`;
- read the Child 1 `prd.md`, `design.md`, and `implement.md`;
- run `trellis-before-dev` for backend, database, error handling, and quality
  guidance;
- start from reviewed `origin/main` in a clean non-main worktree with hooks;
- confirm `db/migrations/` still ends at
  `0051_create_record_platform_foundation.sql`;
- do not copy the abandoned untracked APP V3 `0052` files from the primary
  checkout.

## File Map

Create:

- `internal/center/store/migrate/app_acl_current_contract.go`: public fragment
  shape, production registry, source/fragment compiler, and current catalog
  compiler.
- `internal/center/store/migrate/app_acl_current_contract_test.go`: exact source
  coverage, registry rejection, and future-migration fixtures.
- `internal/center/store/migrate/app_acl_current_convergence.go`: typed rebuild
  error, strict state classifier, and current writer entry point.
- `internal/center/store/migrate/app_acl_current_convergence_test.go`: no-write
  cutpoints, fresh/exact/different state tests, and transaction options.
- `internal/center/store/migrate/app_acl_current_runtime_admission.go`: current
  read-only runtime entry point.
- `internal/center/store/migrate/app_acl_current_runtime_admission_test.go`:
  current manifest/catalog admission and error-chain tests.
- `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`:
  real PostgreSQL fresh, repeat, prior-baseline, runtime, and catalog tests.

Modify:

- `internal/center/store/migrate/acl_manifest_effective_catalog.go`: add the
  internal slice-backed contract and an adapter from frozen R1 arrays.
- `internal/center/store/migrate/acl_manifest_effective_catalog_verifier.go`:
  make the internal verifier consume slices; retain exported R1 wrappers.
- `internal/center/store/migrate/acl_manifest_effective_catalog_managed_scope.go`:
  derive catalog scope from the compiled contract; retain the R1 constructor.
- `internal/center/store/migrate/acl_manifest_effective_catalog_postgres.go`:
  share one PostgreSQL reader between R1 and current inputs.
- `internal/center/store/migrate/app_acl_convergence.go`: extract reusable fresh
  apply, exact verify, DCL, and catalog steps without changing R1 entry points.
- `internal/center/store/migrate/app_acl_convergence_sources.go`: add a frozen
  R1-prefix validator while preserving the exact-R1 validator.
- `internal/center/store/migrate/acl_manifest_runtime.go`: extract manifest
  verification against a supplied compiled privilege/source contract; retain
  the frozen R1 wrapper.
- the matching existing `*_test.go` files: prove all frozen R1 behavior remains
  byte- and behavior-compatible.
- `cmd/houfeng-record-platform-admin/main.go` and `main_test.go`: route APP
  migrate to current convergence and expose only the typed rebuild cause.
- `cmd/houfeng-center/bootstrap.go` and `bootstrap_test.go`: route Records-mode
  startup to current admission.
- `cmd/houfeng-import-vps-json/main.go` and `main_test.go`: route the other
  Records-mode runtime caller to current admission.

Do not create or modify any SQL migration in this slice.

### Task 1: Compile Exact Sources and Explicit Fragments

**Files:**

- Create: `internal/center/store/migrate/app_acl_current_contract.go`
- Create: `internal/center/store/migrate/app_acl_current_contract_test.go`
- Modify: `internal/center/store/migrate/app_acl_convergence_sources.go`
- Test: `internal/center/store/migrate/app_acl_convergence_test.go`

- [ ] **Step 1: Write the RED source/fragment coverage tests**

Add tests with these exact names:

```go
func TestCompileAppACLCurrentSourceContractRejectsMissingFutureFragment(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}

	_, err := compileAppACLCurrentSourceContract(fsys, nil)
	if err == nil || !strings.Contains(err.Error(), `migration "0052_future.sql" has no current APP ACL fragment`) {
		t.Fatalf("compileAppACLCurrentSourceContract() error = %v, want missing-fragment rejection", err)
	}
}

func TestCompileAppACLCurrentSourceContractAcceptsRegisteredFutureMigration(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}

	contract, err := compileAppACLCurrentSourceContract(fsys, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Privileges: func(string) []AppACLPrivilege {
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("compileAppACLCurrentSourceContract() error = %v", err)
	}
	if got := contract.sources.names[len(contract.sources.names)-1]; got != "0052_future.sql" {
		t.Fatalf("last current migration = %q, want 0052_future.sql", got)
	}
}

func appACLCurrentTestMigrationFS(t *testing.T) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	fsys := make(fstest.MapFS, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", entry.Name(), err)
		}
		fsys[entry.Name()] = &fstest.MapFile{Data: append([]byte(nil), data...)}
	}
	return fsys
}
```

Use one table-driven test to reject duplicate fragments, fragments for absent
migrations, duplicate objects, duplicate privileges, unknown subjects,
privileges for unmanaged objects, function hardening for unmanaged functions,
and a new function object without a hardening contract. Keep the existing
`TestConvergeAppACLR1WithDependenciesRejectsNonR1SourceSetBeforeBeginningTransaction`
unchanged.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./internal/center/store/migrate \
  -run '^(TestCompileAppACLCurrentSourceContract|TestConvergeAppACLR1WithDependencies)' \
  -count=1
```

Expected: the new test file fails to compile because the current fragment types
and compiler do not exist. That compiler error is the verified RED result.

- [ ] **Step 3: Add the current fragment and source-contract API**

Use this exact production shape:

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

var appACLCurrentMigrationFragments = []AppACLCurrentMigrationFragment{}

type appACLCurrentSourceContract struct {
	sources   migrationSourceSnapshot
	fragments []AppACLCurrentMigrationFragment
}
```

`compileAppACLCurrentSourceContract` must perform these checks in order:

1. snapshot the supplied `fs.FS` once;
2. prove the first 52 names and bytes equal `appACLR1MigrationSourceContract`;
3. require every later embedded filename to be lexically after
   `0051_create_record_platform_foundation.sql`;
4. build a migration-keyed fragment map and reject duplicate keys;
5. reject fragments not present in the later embedded set;
6. reject later embedded migrations with no fragment;
7. clone fragment slices and function configs so callers cannot mutate the
   compiled contract;
8. validate object, privilege, and function ownership rules without opening a
   PostgreSQL transaction.

Add `validateAppACLR1FrozenSourcePrefix` beside, not instead of,
`validateAppACLR1FrozenSourceSnapshot`. Build a 52-entry snapshot and call the
existing exact validator so there is one checksum implementation.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run the Step 2 command. Expected: PASS, including the frozen exact-R1 boundary.

- [ ] **Step 5: Record the future execution commit**

During an authorized implementation session only:

```bash
git add internal/center/store/migrate/app_acl_current_contract.go \
  internal/center/store/migrate/app_acl_current_contract_test.go \
  internal/center/store/migrate/app_acl_convergence_sources.go \
  internal/center/store/migrate/app_acl_convergence_test.go
git commit -m "refactor: compile current app migration fragments"
```

### Task 2: Introduce the Slice-Backed Catalog Contract

**Files:**

- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_managed_surface.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier_test.go`
- Modify: `internal/center/store/migrate/app_acl_current_contract.go`
- Test: `internal/center/store/migrate/app_acl_current_contract_test.go`

- [ ] **Step 1: Write RED adapter and extension tests**

Add tests proving:

- converting `CompileAppACLEffectiveCatalogContractR1` to the internal form
  produces exactly two bindings, 204 privileges, the frozen managed surface,
  and the two projector hardening expectations;
- a current fragment adds exactly its objects, privileges, and function
  hardening;
- input slices are defensively copied;
- duplicate base/fragment objects or privileges fail;
- an explicit empty fragment changes only the canonical migration set.

Use this assertion pattern for frozen compatibility:

```go
r1, err := CompileAppACLEffectiveCatalogContractR1("houfeng", bindings)
if err != nil {
	t.Fatal(err)
}
generic, err := appACLEffectiveCatalogContractFromR1(r1, "houfeng_migrator")
if err != nil {
	t.Fatal(err)
}
if len(generic.RoleBindings) != 2 || len(generic.Privileges) != appACLEffectiveCatalogR1PrivilegeCount {
	t.Fatalf("generic R1 sizes = %d/%d, want 2/%d", len(generic.RoleBindings), len(generic.Privileges), appACLEffectiveCatalogR1PrivilegeCount)
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./internal/center/store/migrate \
  -run '^(TestCompileAppACLEffectiveCatalogContractR1|TestAppACLEffectiveCatalogContractFromR1|TestCompileAppACLCurrentCatalogContract)' \
  -count=1
```

Expected: FAIL because the internal slice-backed contract and adapter are absent.

- [ ] **Step 3: Add the internal contract without changing exported R1 types**

Use this internal model:

```go
type appACLEffectiveCatalogFunctionContract struct {
	SchemaName      string
	Identity        string
	OwnerRole       string
	Kind            string
	SecurityDefiner bool
	Config          []string
}

type appACLEffectiveCatalogContract struct {
	DatabaseName     string
	RoleBindings     []AppACLRoleBinding
	Privileges       []AppACLPrivilege
	ManagedObjects   []AppACLManagedObjectR1
	ExpectedFunctions []appACLEffectiveCatalogFunctionContract
}
```

The R1 adapter copies the fixed arrays and uses `CompileAppACLManagedSurfaceR1`.
It binds the two existing projector expectations to the supplied migrator role.
Do not change `AppACLEffectiveCatalogContractR1`, its array sizes, its exported
compiler, or its comparability tests.

The current compiler starts from the R1 adapter, appends fragment values in
migration order, canonicalizes bindings/privileges through
`CanonicalPrivilegeSetBodyV1`, sorts managed objects by class/schema/identity,
and rejects duplicates before returning. Every privilege must map to a managed
object. Every post-`0051` managed function must have exactly one matching
hardening entry.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run the Step 2 command. Expected: PASS with the original 204-tuple R1 evidence
unchanged.

- [ ] **Step 5: Record the future execution commit**

```bash
git add internal/center/store/migrate/acl_manifest_effective_catalog.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_test.go \
  internal/center/store/migrate/acl_manifest_managed_surface.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_verifier_test.go \
  internal/center/store/migrate/app_acl_current_contract.go \
  internal/center/store/migrate/app_acl_current_contract_test.go
git commit -m "refactor: share app acl catalog contract"
```

### Task 3: Share Catalog Scope, Reader, Verifier, and DCL

**Files:**

- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_verifier_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_managed_scope.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_managed_scope_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_postgres.go`
- Modify: `internal/center/store/migrate/acl_manifest_effective_catalog_postgres_test.go`
- Modify: `internal/center/store/migrate/app_acl_convergence.go`
- Modify: `internal/center/store/migrate/app_acl_convergence_test.go`

- [ ] **Step 1: Write RED current-surface tests**

Create a test contract containing one future table and one hardened function.
Prove the managed scope keeps their owner/ACL/function observations, the
verifier rejects their absence or drift, and DCL revokes `PUBLIC`, runtime, and
admin before emitting only the compiled grants. Also retain tests showing an
unrelated public table remains outside APP scope.

Use these exact internal entry points in tests:

```go
scope, err := newAppACLManagedSurfaceScope(contract)
statements, err := appACLConvergenceDCLStatementsForContract(contract)
err = verifyAppACLEffectiveCatalogSnapshot(snapshot, input)
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./internal/center/store/migrate \
  -run '^(TestAppACLCurrentCatalog|TestAppACLEffectiveCatalog|TestAppACLConvergenceDCL)' \
  -count=1
```

Expected: FAIL because the existing reader/verifier/DCL still reconstruct the
fixed R1 surface.

- [ ] **Step 3: Extract the slice-backed internal core**

Make the internal scope, reader, verifier, owner checks, privilege checks,
function checks, and DCL renderer accept `appACLEffectiveCatalogContract`.
Validation must compare exact sets rather than counts alone. Preserve these
wrappers and their existing error prefixes:

```go
func NewAppACLEffectiveCatalogVerifierInputR1(
	contract AppACLEffectiveCatalogContractR1,
	migratorRole string,
) (AppACLEffectiveCatalogVerifierInputR1, error)

func VerifyAppACLEffectiveCatalogSnapshotR1(
	snapshot AppACLEffectiveCatalogSnapshotR1,
	input AppACLEffectiveCatalogVerifierInputR1,
) error

func readAppACLEffectiveCatalogSnapshotInTxR1(
	ctx context.Context,
	tx pgx.Tx,
	input AppACLEffectiveCatalogVerifierInputR1,
) (AppACLEffectiveCatalogSnapshotR1, error)
```

Each wrapper must adapt R1 to the generic core. The R1 wrapper still validates
against the frozen compiler before adaptation, so a caller cannot substitute a
dynamic contract into a historical R1 API.

- [ ] **Step 4: Verify current and frozen behavior**

Run:

```bash
go test ./internal/center/store/migrate -count=1
```

Expected: PASS. This broad package run is required because catalog helpers are
shared by convergence, runtime admission, and R2 frozen-R1 verification.

- [ ] **Step 5: Record the future execution commit**

```bash
git add internal/center/store/migrate/acl_manifest_effective_catalog_verifier.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_verifier_test.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_managed_scope.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_managed_scope_test.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_postgres.go \
  internal/center/store/migrate/acl_manifest_effective_catalog_postgres_test.go \
  internal/center/store/migrate/app_acl_convergence.go \
  internal/center/store/migrate/app_acl_convergence_test.go
git commit -m "refactor: share app acl catalog verification"
```

### Task 4: Add Strict Current Convergence

**Files:**

- Create: `internal/center/store/migrate/app_acl_current_convergence.go`
- Create: `internal/center/store/migrate/app_acl_current_convergence_test.go`
- Modify: `internal/center/store/migrate/app_acl_convergence.go`
- Modify: `internal/center/store/migrate/app_acl_convergence_postgres_test.go`

- [ ] **Step 1: Write RED typed-error and no-mutation tests**

Add tests with these exact names:

- `TestConvergeAppACLCurrentRejectsMissingFragmentBeforeBeginTx`
- `TestConvergeAppACLCurrentRegisteredFutureMigrationReachesBeginTx`
- `TestConvergeAppACLCurrentDifferentBaselineRequiresRebuildBeforeMutation`
- `TestConvergeAppACLCurrentExactRepeatOmitsMutation`
- `TestConvergeAppACLCurrentFreshUsesSerializableTransaction`
- `TestConvergeAppACLR1RetainsNullHeadAdoption`

In the different-baseline test, every dependency that can write must call
`t.Fatal`: ledger creation, pending migration apply, DCL, genesis insert, and
head-for-update. Assert `errors.Is(err,
ErrDevelopmentDatabaseRebuildRequired)` and assert the transaction rolls back.
In the registered-future test, inject `0052_future.sql` with an explicit empty
fragment, make `BeginTx` return a sentinel error, and assert exactly one begin
call plus that sentinel in the returned error chain.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./internal/center/store/migrate \
  -run '^(TestConvergeAppACLCurrent|TestConvergeAppACLR1RetainsNullHeadAdoption)' \
  -count=1
```

Expected: FAIL because current convergence and the typed error do not exist.

- [ ] **Step 3: Add the error and current entry point**

Use these exported contracts:

```go
var ErrDevelopmentDatabaseRebuildRequired = errors.New(
	"development database must be recreated for the current embedded migrations",
)

func ConvergeAppACLCurrent(
	ctx context.Context,
	db *pgxpool.Pool,
	runtimeRole string,
	adminRole string,
) (AppACLManifestPersistedV1, error)
```

`ConvergeAppACLCurrent` compiles `migrations.FS` plus
`appACLCurrentMigrationFragments` before `BeginTx`. The transaction uses
`SERIALIZABLE`, the existing advisory lock and hardened search path, and a strict
preflight with only these outcomes:

- fresh: no APP ledger/manifest/managed object, then apply every source, DCL,
  catalog verification, and one genesis manifest atomically;
- exact current: ledger, one genesis manifest/head, source bytes, privileges,
  roles, ownership, functions, and ACL all match, then commit with no durable
  write;
- source-set or prior development baseline: wrap
  `ErrDevelopmentDatabaseRebuildRequired` before calling any write dependency;
- exact-source catalog or manifest corruption: return the specific corruption
  error, not a misleading rebuild-success classification.

Extract reusable fresh-apply and exact-verify helpers from R1 convergence.
Frozen `ConvergeAppACLR1` keeps exact-R1 validation, null-head adoption, its
exported signature, retry behavior, and error text asserted by existing tests.

- [ ] **Step 4: Run focused and frozen tests**

Run:

```bash
go test ./internal/center/store/migrate \
  -run '^(TestConvergeAppACLCurrent|TestConvergeAppACLR1|TestAppACLConvergence)' \
  -count=1
```

Expected: PASS. Inspect spy traces to confirm no DDL, DCL, ledger insert/update,
manifest insert/update, or repair query occurs on the rebuild path.

- [ ] **Step 5: Record the future execution commit**

```bash
git add internal/center/store/migrate/app_acl_current_convergence.go \
  internal/center/store/migrate/app_acl_current_convergence_test.go \
  internal/center/store/migrate/app_acl_convergence.go \
  internal/center/store/migrate/app_acl_convergence_postgres_test.go
git commit -m "feat: converge current app migration baseline"
```

### Task 5: Add Current Read-Only Runtime Admission

**Files:**

- Create: `internal/center/store/migrate/app_acl_current_runtime_admission.go`
- Create: `internal/center/store/migrate/app_acl_current_runtime_admission_test.go`
- Modify: `internal/center/store/migrate/acl_manifest_runtime.go`
- Modify: `internal/center/store/migrate/acl_manifest_runtime_test.go`
- Modify: `internal/center/store/migrate/app_acl_runtime_admission.go`
- Modify: `internal/center/store/migrate/app_acl_runtime_admission_test.go`

- [ ] **Step 1: Write RED current-admission tests**

Add tests proving one `REPEATABLE READ READ ONLY` transaction reads manifest,
ledger, identity, and catalog; source mismatch yields the rebuild sentinel before
catalog read; exact source with catalog drift returns a catalog error; SET ROLE
is rejected; and the frozen `AdmitAppACLRuntime` still uses R1.

The production entry point under test is:

```go
func AdmitAppACLCurrentRuntime(ctx context.Context, db *pgxpool.Pool) error
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./internal/center/store/migrate \
  -run '^(TestAdmitAppACLCurrentRuntime|TestAppACLRuntimeAdmission|TestVerifyPersistedAppACLManifestRuntime)' \
  -count=1
```

Expected: FAIL because the current entry point and contract-aware manifest
verifier are absent.

- [ ] **Step 3: Extract contract-aware manifest verification**

Keep `VerifyPersistedAppACLManifestRuntimeV1` and
`verifyAppACLManifestRuntimeSnapshotV1` as frozen R1 wrappers. Add an internal
verifier that receives the expected canonical migration bytes and a function
that compiles privileges/catalog from the persisted role bindings. It must:

1. validate direct login identity and the manifest chain;
2. compare persisted migration bytes to expected current bytes;
3. compare the applied ledger to the same bytes;
4. compare persisted privileges to the compiled current privilege body;
5. preserve the migrator/runtime/admin role separation checks;
6. return a mismatch classification that current admission wraps with the
   rebuild sentinel, without labeling catalog corruption as rebuildable.

- [ ] **Step 4: Implement current admission and verify GREEN**

Compile the current source contract before opening the transaction. Inside one
read-only snapshot, compile the database-specific current catalog contract,
verify manifest/ledger, read only that managed catalog scope, verify identity and
catalog, and commit. Run the Step 2 command; expected: PASS.

- [ ] **Step 5: Record the future execution commit**

```bash
git add internal/center/store/migrate/app_acl_current_runtime_admission.go \
  internal/center/store/migrate/app_acl_current_runtime_admission_test.go \
  internal/center/store/migrate/acl_manifest_runtime.go \
  internal/center/store/migrate/acl_manifest_runtime_test.go \
  internal/center/store/migrate/app_acl_runtime_admission.go \
  internal/center/store/migrate/app_acl_runtime_admission_test.go
git commit -m "feat: admit current app runtime contract"
```

### Task 6: Prove Fresh, Repeat, Prior-Baseline, and Runtime on PostgreSQL

**Files:**

- Create: `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`
- Modify only if a shared fixture is needed:
  `internal/center/store/migrate/app_acl_convergence_postgres_test.go`

- [ ] **Step 1: Write the real PostgreSQL tests**

Add one anchored suite:

```go
func TestPostgresIntegrationAppACLCurrent(t *testing.T) {
	t.Run("fresh_and_runtime", testPostgresIntegrationAppACLCurrentFreshAndRuntime)
	t.Run("exact_repeat_is_read_only", testPostgresIntegrationAppACLCurrentExactRepeat)
	t.Run("prior_baseline_requires_rebuild_without_mutation", testPostgresIntegrationAppACLCurrentPriorBaseline)
}
```

The fresh case must assert exact ledger source count, one manifest revision/head,
compiled managed ownership, direct/effective privileges, no PUBLIC/column/default
ACL, expected function hardening, and successful direct-runtime admission.

The repeat case snapshots ledger rows, manifest rows/head, owners, direct/effective
ACL, functions, and relevant catalog state before and after a second convergence;
all snapshots must be deeply equal.

The prior-baseline case first converges the embedded `0051` build, then invokes a
test-only current compiler with an injected `0052_future.sql` and an explicit
empty fragment. Assert `errors.Is` on the rebuild sentinel and deep equality of
all durable snapshots. Do not add a real `0052` file.

- [ ] **Step 2: Run the suite and verify RED or a precise first failure**

Run:

```bash
scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/store/migrate \
  -run '^TestPostgresIntegrationAppACLCurrent$' -count=1
```

Expected before implementation is complete: FAIL. A skipped suite is also a
failure because the wrapper rejects skipped tests.

- [ ] **Step 3: Complete only the minimum production behavior exposed by the suite**

Fix current compiler/convergence/admission defects found by this real database
test. Do not weaken assertions, adopt the old database, add a successor state,
or add `0052`.

- [ ] **Step 4: Run PostgreSQL 16 evidence and frozen regressions**

Run the Step 2 command, then:

```bash
go test ./internal/center/store/migrate -count=1
```

Expected: PASS with no skips. CI later runs the repository's PostgreSQL 16.0,
16.6, and 16.12 matrix.

- [ ] **Step 5: Record the future execution commit**

```bash
git add internal/center/store/migrate/app_acl_current_postgres_integration_test.go \
  internal/center/store/migrate/app_acl_convergence_postgres_test.go \
  internal/center/store/migrate/app_acl_current_contract.go \
  internal/center/store/migrate/app_acl_current_convergence.go \
  internal/center/store/migrate/app_acl_current_runtime_admission.go
git commit -m "test: prove current app acl on postgres"
```

### Task 7: Route Every Current Product Caller

**Files:**

- Modify: `cmd/houfeng-record-platform-admin/main.go`
- Modify: `cmd/houfeng-record-platform-admin/main_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `cmd/houfeng-import-vps-json/main.go`
- Modify: `cmd/houfeng-import-vps-json/main_test.go`

- [ ] **Step 1: Write RED routing and error-chain tests**

Prove:

- default `migrate --scope app` calls `ConvergeAppACLCurrent`;
- a rebuild-required cause remains available through `errors.Is` and prints the
  actionable rebuild message;
- arbitrary database errors and DSNs remain redacted;
- Records-enabled center bootstrap uses current admission and retains its cause;
- Records-enabled JSON import uses current admission;
- legacy mode still uses generic `migrate.Apply` where it already did;
- R2 bootstrap/finalize commands remain separately routed and unchanged.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./cmd/houfeng-record-platform-admin ./cmd/houfeng-center \
  ./cmd/houfeng-import-vps-json \
  -run 'App|RuntimeAdmission|RecordPlatform' -count=1
```

Expected: routing assertions fail because defaults still point to frozen R1.

- [ ] **Step 3: Switch defaults and preserve safe error detail**

Set the three defaults to `ConvergeAppACLCurrent` or
`AdmitAppACLCurrentRuntime`. In the admin command, retain only the typed safe
cause:

```go
if err := deps.converge(ctx, pool, config.RuntimeRole, config.AdminRole); err != nil {
	if errors.Is(err, migrate.ErrDevelopmentDatabaseRebuildRequired) {
		return fmt.Errorf("%w: %w", errConvergeAppMigration, migrate.ErrDevelopmentDatabaseRebuildRequired)
	}
	return errConvergeAppMigration
}
```

Center and importer already wrap their dependency errors; keep that wrapping so
`errors.Is` works. Do not route product startup through R2 bootstrap/finalize.

- [ ] **Step 4: Run call-site and migrate package tests**

Run:

```bash
go test ./cmd/houfeng-record-platform-admin ./cmd/houfeng-center \
  ./cmd/houfeng-import-vps-json ./internal/center/store/migrate -count=1
```

Expected: PASS, no DSN or arbitrary database error text in command output.

- [ ] **Step 5: Record the future execution commit**

```bash
git add cmd/houfeng-record-platform-admin/main.go \
  cmd/houfeng-record-platform-admin/main_test.go \
  cmd/houfeng-center/bootstrap.go cmd/houfeng-center/bootstrap_test.go \
  cmd/houfeng-import-vps-json/main.go cmd/houfeng-import-vps-json/main_test.go
git commit -m "feat: route app startup through current acl"
```

### Task 8: Full Verification and Child 1 Closeout Audit

**Files:**

- Modify only after code establishes the facts:
  `.trellis/tasks/07-14-vps-records-platform-foundation/prd.md`
- Modify only after code establishes the facts:
  `.trellis/tasks/07-14-vps-records-platform-foundation/implement.md`
- Modify only for durable project conventions discovered during implementation:
  `.trellis/spec/`

- [ ] **Step 1: Run focused tests from a workspace-backed temp directory**

```bash
mkdir -p .tmp/go-tmp .tmp/test-tmp
GOTMPDIR="$PWD/.tmp/go-tmp" TMPDIR="$PWD/.tmp/test-tmp" \
  go test ./internal/center/store/migrate \
  ./cmd/houfeng-record-platform-admin ./cmd/houfeng-center \
  ./cmd/houfeng-import-vps-json -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the non-skipping PostgreSQL suite**

```bash
TMPDIR="$PWD/.tmp/test-tmp" \
  scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/store/migrate \
  -run '^TestPostgresIntegrationAppACLCurrent$' -count=1
```

Expected: PASS and no `--- SKIP:` output.

- [ ] **Step 3: Run the full Go gate**

```bash
GOTMPDIR="$PWD/.tmp/go-tmp" TMPDIR="$PWD/.tmp/test-tmp" make verify-go
```

Expected: `fmt-go`, `vet-go`, and `test-go` all exit 0.

- [ ] **Step 4: Inspect migration, frozen API, and scope invariants**

```bash
test ! -e db/migrations/0052_add_app_extension_hardening_receipt.sql
test "$(find db/migrations -maxdepth 1 -name '*.sql' -printf '%f\n' | sort | tail -1)" = \
  "0051_create_record_platform_foundation.sql"
rg -n 'func (ConvergeAppACLR1|AdmitAppACLRuntime|BootstrapAppACLR2|FinalizeAppACLR2)' \
  internal/center/store/migrate
git diff --check
```

Expected: both migration checks exit 0, all frozen entry points remain, and
`git diff --check` reports nothing.

- [ ] **Step 5: Run Trellis validation and quality review**

```bash
python3 ./.trellis/scripts/task.py validate \
  .trellis/tasks/07-14-vps-records-platform-foundation
python3 ./.trellis/scripts/task.py validate \
  .trellis/tasks/07-13-vps-detail-experience-design
```

Then run `trellis-check` against the complete implementation diff. Fix findings
and repeat focused/full gates. Use `trellis-update-spec` only for contracts that
the code and tests now prove.

- [ ] **Step 6: Audit Child 1 without claiming Child 2 progress**

Map every surviving Child 1 acceptance criterion to a protected-main file and
test. Confirm the four archived descendants are still represented, the current
slice is green, APP V3 requirements are absent from active gates, and no Records
Core schema/API/UI was added. Child 1 is not complete until this audit and its PR
are accepted; parent progress remains `0/11` until then.

- [ ] **Step 7: Stop for review before delivery actions**

Present the complete diff and verification evidence. Only after explicit
delivery approval should the selected branch be committed/pushed, a PR opened,
required CI monitored, and Child 1 archived after protected-main integration.
Do not continue into Child 2 in the same unchecked run.

## Hard Stops

Stop and return to planning if any step requires:

- a real root `0052` migration or any Records Core functionality;
- modifying frozen `0001`-`0051` SQL bytes;
- adopting, upgrading, or repairing a prior development database;
- APP V3, owner transfer, detached approval, traffic drain, key rotation, or
  cross-domain disaster-recovery governance;
- changing an R1/R2 exported entry point or weakening a frozen regression;
- touching the dirty primary checkout or deleting old branch/worktree evidence;
- staging, release, or deployment work as a Child 1 acceptance condition.

## Rollback

Before later post-`0051` migrations exist, reverting product call sites to the
frozen R1 entry points requires no database change. After a later Records
migration is embedded, returning to an older build requires recreating the
development database. There is no down migration or successor/adoption path in
this plan.

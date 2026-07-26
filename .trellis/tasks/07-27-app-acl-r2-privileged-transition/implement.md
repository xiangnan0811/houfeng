# APP ACL R2 Privileged Transition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` task-by-task. Track work with the checkboxes below.

**Goal:** Replace the superseded root implementation with an isolated two-command R2
transition that proves bootstrap authority before finalizing the 206-tuple
catalog.

**Architecture:** Keep all frozen R1 entry points unchanged and R1-only:
`AdmitAppACLRuntime`, R1 readers, `ConvergeAppACLR1`, and generic
`migrate --scope app`. Add a separate embedded R2 source, R2-only codecs,
immutable L2 receipt ledger, and new M2 relations
`app_acl_r2_manifest_revisions` and `app_acl_r2_manifest_head`. Bootstrap owns
only the L2 proof surface; the direct-finalizer transaction alone creates and
owns the M2 relation pair. New `ClassifyAppACLR2State`,
`AdmitAppACLR2Runtime`, and `StartAppACLR2Runtime` are the sole R2-aware
surfaces: they use frozen R1 verification for exact R1 and inspect R2 only
after FINALIZED.

**Tech Stack:** Go, pgx v5, PostgreSQL 16, embedded SQL, existing record
platform integration fixture, Trellis reviews.

---

## Mandatory Gate After Every Slice

1. Write and run the listed RED test first; retain proof of the intended fail.
2. Implement only the listed files, run GREEN selectors, `gofmt`, and
   `git diff --check`.
3. Run a specification-compliance review against this task's PRD/design and
   resolve every P0/P1/P2 finding.
4. Run an independent code-quality review and resolve every P0/P1/P2 finding
   before starting the next slice.

All work stays on a non-main branch. Do not alter parent status or direct main.

### Slice 1: Remove The Draft Atomically

**Ownership:**

- Remove the superseded root migration, receipt implementation, and associated
  tests.
- Remove only the superseded root R2 source-contract and privilege-compiler
  additions from their current frozen R1 source, manifest, runtime,
  convergence, integration, migration, configuration, and CLI locations.
  Preserve every frozen R1 symbol in those files.
- Remove the related legacy configuration, environment, and CLI paths.
  Recovery-related symbols are not owned here.

- [ ] RED: add tests that root migrations contain no 0052, R1 sees exactly 52,
  and superseded legacy routes/env lookups are absent. Run:

```bash
go test ./internal/center/store/migrate ./internal/center/platformmigrate ./cmd/houfeng-record-platform-admin -run 'NoRootR2|NoLegacyR2|FrozenR1' -count=1
```

- [ ] GREEN: remove every draft occurrence and prove it:

```bash
go test ./internal/center/store/migrate ./internal/center/platformmigrate ./cmd/houfeng-record-platform-admin -run 'NoRootR2|NoLegacyR2|FrozenR1' -count=1
assert_no_obsolete_root_match() (
  set +e
  rg -n "$@" db internal cmd
  rg_status=$?
  case "$rg_status" in
    0)
      printf '%s\n' 'obsolete APP R2 root symbol remains' >&2
      exit 1
      ;;
    1)
      exit 0
      ;;
    *)
      printf 'rg failed while checking obsolete APP R2 root symbols (status %s)\n' "$rg_status" >&2
      exit 1
      ;;
  esac
)
assert_no_obsolete_root_match -e '0052_add_app_extension_hardening_receipt|app_extension_hardening_receipt|AppExtensionHardening|AppExtensionHardener|appExtensionHardening|appExtensionHardener|appACLR2MigrationSourceContract|AppACLR2FrozenSourceSnapshot|validateAppACLR2FrozenSourceSnapshot|AppACLPrivilegeSetR2|appACLPrivilegesR2|CompileAppACLPrivilegeSetR2|HOUFENG_RECORD_PLATFORM_APP_EXTENSION_HARDENER_DATABASE_URL|HOUFENG_RECORD_PLATFORM_APP_EXTENSION_HARDENER_ROLE|APP_EXTENSION_HARDENING|APP_EXTENSION_HARDENER|app-extension-hardening'
if [ -e db/migrations/0052_add_app_extension_hardening_receipt.sql ]; then
  printf '%s\n' 'obsolete root migration filename remains' >&2
  exit 1
fi
```

### Slice 2: Isolated Source And R2 Canonical Codecs

**Ownership:** Create `db/appaclr2/migrations/{embed.go,embed_test.go,
0052_app_acl_r2_privileged_transition.sql}` and
`internal/center/store/migrate/{app_acl_r2_source.go,app_acl_r2_source_test.go,
app_acl_r2_manifest.go,app_acl_r2_manifest_test.go}`. This slice owns only
source and manifest vectors; it does not consume the `domain_body` or
`l2_acl_body` literal corpus or own their decoders, malformed cases, or nesting.

- [ ] RED: test root invisibility, frozen 52 prefix plus exact 0052, altered
  bytes, R1 magic, 2/4 bindings, 205/207 tuples, noncanonical ordering, and
  checksum substitution. `app_acl_r2_source_test.go` owns source-set valid hex,
  bad-magic, truncated-length, substituted-digest, duplicate, reorder, and
  trailing-byte vectors. `app_acl_r2_manifest_test.go` owns privilege,
  control-ACL, and M2 vectors plus count, role-map, grant-option, nested-digest,
  ordering, malformed-length, and trailing-byte vectors. The domain/L2 literal
  corpus and all of its decoder, malformed, and nesting coverage are exclusive
  to Slice 3 receipt tests; PostgreSQL tests assert live catalog equality only.
- [ ] GREEN: implement separate R2 magic/strict EOF parser, 53 source entries,
  three bindings, and the separate 206-tuple body/digest contract without
  calling or widening V1 code. Do not touch `ConvergeAppACLR1`, R1 readers,
  `AdmitAppACLRuntime`, or generic `migrate --scope app`.
- [ ] Run:

```bash
go test ./db/appaclr2/migrations ./internal/center/store/migrate -run 'AppACLR2(Source|Manifest|Privilege)' -count=1
go test ./internal/center/store/migrate -run 'FrozenR1|Canonical.*V1' -count=1
```

### Slice 3: Receipt SQL, Snapshot, And Credential-Neutral Frozen R1 State Verification

**Ownership:** Modify isolated 0052; create
`app_acl_r2_receipt.go`, `app_acl_r2_receipt_test.go`,
`app_acl_r2_receipt_postgres.go`, and
`app_acl_r2_receipt_postgres_test.go`,
`app_acl_r2_frozen_r1_verify.go`, and
`app_acl_r2_frozen_r1_verify_test.go`. The verifier file owns
`VerifyFrozenAppACLR1StateInTx(ctx, tx) (FrozenAppACLR1StateV1, error)` and
`RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, state)`; only extend
`internal/center/platformmigrate/{domain_identity.go,roles.go}` where a
transaction-bound read helper is genuinely reusable. Slice 3
`app_acl_r2_receipt_test.go` is the sole consumer of the task-local
`domain_body`/`l2_acl_body` literal corpus and owns its decoder, malformed, and
nesting coverage.

- [ ] RED: wrong allowed PG16 version/OID/direct session/superuser,
  missing/change domain identity, non-36 member count,
  `record_platform_internal` member namespace/OID/identity-argument/owner or
  dependency drift, extension version/schema/owner/ACL drift,
  application-source-hash drift, equal-cardinality member substitution, helper
  drift, and direct/effective receipt ACL drift all fail.
- [ ] RED: consume the task-local fixed `domain_body` and `l2_acl_body`
  literals only in `app_acl_r2_receipt_test.go`; compare encoder/re-encoder
  bytes and SHA-256 directly to the documented hex/digest, reject every
  documented malformed body, and own all domain/L2 nesting, swap, and digest
  tamper coverage without deriving expectations from a production encoder,
  compiler, parser, or live database.
- [ ] RED: use one exact frozen R1 catalog/source/ledger/ACL fixture with each
  direct identity from the identity matrix. The transaction-bound verifier must
  return the same verified state in every row, must not read `session_user` or
  `current_user`, and must not open a pool/second transaction or call frozen
  `AdmitAppACLRuntime`. The separate runtime predicate alone accepts the
  matching center-runtime row and rejects every other row, including the test
  distinct-pair fixture without `SET ROLE`.
- [ ] GREEN: implement immutable singleton receipt/ledger, bootstrap-owned
  helpers, direct/runtime-only receipt SELECT, and fresh exact catalog
  comparison. Bootstrap-superuser preflight reads only the locked local
  PostgreSQL catalog; it records allowed PG16/version and the exact pgcrypto
  extension/member/dependency/owner/ACL baseline, with no server-file,
  directory, path, symlink, image, package, or raw-extension-hash evidence.
  The preflight proves the pre-existing R1 baseline: direct migrator already
  owns the `record_platform_internal` pgcrypto extension and all 36 exact PG16
  v1.3 `record_platform_internal` member procedures are already owned by
  bootstrap OID 10. Finalize/runtime compare the receipt's catalog facts plus
  application source hashes to fresh catalog/application constants only.
  `app_acl_r2_receipt_test.go` solely consumes the task-local domain/L2 literal
  vectors and owns their decoder, malformed, nesting, receipt-body, swap/digest
  tamper, wrong member-schema, malformed-length, and trailing-byte coverage. It
  rejects any deviation and never transfers ownership or drops/recreates the
  extension. Before any bootstrap/finalize command exists, implement the
  credential-neutral frozen R1 verifier over only tx-bound
  catalog/source/ledger/ACL evidence. `RequireDirectFrozenAppACLR1RuntimeInTx`
  is a separate direct-runtime predicate and is the only Slice 3 API allowed to
  inspect `session_user` or `current_user`.
- [ ] Run:

```bash
go test ./internal/center/store/migrate ./internal/center/platformmigrate -run 'AppACLR2Receipt|FrozenAppACLR1State|RequireDirectFrozenAppACLR1Runtime|DomainIdentity|DirectRole' -count=1
```

The passing Slice 3 state-verifier gate is a prerequisite for the bootstrap
and finalizer slices; neither may implement a replacement verifier.

### Slice 4: Shared State Classifier And Bootstrap

**Ownership:** Create `app_acl_r2_catalog.go`, `app_acl_r2_catalog_test.go`,
`app_acl_r2_state.go`, `app_acl_r2_state_test.go`, `app_acl_r2_bootstrap.go`,
and `app_acl_r2_bootstrap_test.go`; create
`internal/center/platformmigrate/app_acl_r2_transition_config.go` and its
test; modify only `cmd/houfeng-record-platform-admin/main.go` and
`cmd/houfeng-record-platform-admin/main_test.go` to register the new,
separately named `bootstrap --scope app-acl-r2` route. Do not modify any frozen
R1 admission, reader, convergence, startup, or generic migration file. This
slice consumes the passed Slice 3 verifier/predicate gate. Its catalog files
first own and test the reusable exact L1/M1/L2/M2/control-ACL relation/head
predicates; `app_acl_r2_state.go` composes them into typed state, and bootstrap
consumes them. Do not implement the classifier or bootstrap before that catalog
predicate gate passes.

- [ ] RED: `app_acl_r2_catalog.go` and its test first establish the reusable
  exact L1/M1/L2/M2/control-ACL relation/head predicates, including absent,
  one-sided, extra, wrong-owner, wrong-link, wrong-head, and mixed shapes.
  `ClassifyAppACLR2State` in the single state file then recognizes only exact
  R1, PREPARED, FINALIZED, and CORRUPT by composing those predicates; it rejects
  unknown/mixed object shapes. The identity-invariant classifier matrix runs
  every direct identity against otherwise identical exact R1, PREPARED, and
  FINALIZED fixtures and returns the same state without reading
  `session_user`/`current_user` or turning a wrong session into `CORRUPT`.
  Bootstrap reads only
  `HOUFENG_RECORD_PLATFORM_APP_BOOTSTRAP_DATABASE_URL`, rejects unexpected
  lookup before pool open, proves lock order/pre-mutation checks, retries only
  whole `40001`/`40P01` closure, uses the prebuilt credential-neutral frozen-R1
  state verifier plus its own OID-10 actor gate, and reclassifies PREPARED ACK
  loss.
- [ ] GREEN: implement the read-only reusable catalog predicates, then the
  classifier that composes them. Execute bootstrap only in one serializable
  transaction; create only receipt/helpers/L2, and leave both
  `app_acl_r2_manifest_revisions` and `app_acl_r2_manifest_head` absent. Never
  create roles, transfer owner, recreate pgcrypto, invoke finalizer, or alter
  frozen M1.
- [ ] Run:

```bash
go test ./cmd/houfeng-record-platform-admin ./internal/center/platformmigrate ./internal/center/store/migrate -run 'AppACLR2(Catalog|State|Bootstrap)|BootstrapAppACLR2|AppACLR2Config' -count=1
```

### Slice 5: Direct-Finalizer M2 Relations, 206-Tuple Catalog, And CAS

**Ownership:** Create `app_acl_r2_finalize.go` and its test; modify isolated
0052 and admin command files only for the separately named
`finalize --scope app-acl-r2` route. This slice consumes the passed Slice 4
reusable L1/M1/L2/M2/control-ACL relation/head predicate and builds finalizer
behavior on it; it does not recreate catalog predicate or catalog-test
ownership. The only M2 persistence owned by this slice is new
`public.app_acl_r2_manifest_revisions` and
`public.app_acl_r2_manifest_head`; 0051 V1 manifest relations are read-only
predecessors and are never inserted into, altered, re-owned, or advanced.

- [ ] RED: finalize reads only
  `HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL`, rejects R1/nonexact
  PREPARED/direct-role/M1/receipt drift through the passed predicate; rejects
  pre-existing, one-sided,
  empty, extra, wrong-owner, wrong-link, or wrong-head M2 shape; rejects
  205/207 catalog; detects stale head CAS; verifies one M2 revision/one true
  singleton head; proves full transaction rollback; and reclassifies FINALIZED
  ACK loss.
- [ ] GREEN: validate receipt and the passed reusable catalog predicate first,
  then execute the finalizer section in one serializable transaction after
  credential-neutral state verification and its own direct-migrator actor gate
  (the only finalizer session-identity check),
  create the direct-migrator-owned M2 relation pair and immutable triggers with
  plain `CREATE TABLE`, insert exactly one
  `(protocol_version, manifest_revision) = (2, 2)` revision plus exactly one
  true singleton head, bind immutable M1 revision/digest/source/privilege/role
  link fields, store the separate three-binding/206-tuple body/digest, use M1
  revision/digest CAS, and re-read receipt/catalog/head before commit. Any
  error rolls back all M2 DDL/DML to exact PREPARED.
- [ ] Run:

```bash
go test ./cmd/houfeng-record-platform-admin ./internal/center/store/migrate -run 'AppACLR2Finalize|AppACLR2Manifest' -count=1
```

### Slice 6: Separate R2 Admission And Startup Route

**Ownership:** Create `app_acl_r2_runtime_admission.go` and
`app_acl_r2_runtime_admission_test.go`. The admission file owns the new
`AdmitAppACLR1OnlyRuntime`, `AdmitAppACLR2Runtime`, and
`StartAppACLR2Runtime` APIs and consumes the already-tested Slice 3 verifier
and direct-runtime predicate. `app_acl_r2_runtime_admission_test.go` owns the
mandatory adversarial `TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace` and its
paired PREPARED-classification-only assertions. No file here edits
`AdmitAppACLRuntime`, any R1 reader, `ConvergeAppACLR1`, an existing R1 startup
route, or generic `migrate --scope app`. There is no
`app_acl_r2_dispatch.go`.

- [ ] RED: frozen V1 parser/reader/converger and generic app migration receive
  no R2 bytes or dispatch dependency; `AdmitAppACLR1OnlyRuntime` rejects every
  non-R1 state; `AdmitAppACLR2Runtime` and `StartAppACLR2Runtime` accept exact
  R1 as R1, reject PREPARED/CORRUPT, and admit R2 only after one locked
  repeatable-read/read-only FINALIZED proof. Add the adversarial
  `TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace`: pause after exact-R1
  classification while the shared advisory lock is held, prove bootstrap cannot
  acquire/commit its exclusive transition, then prove the fresh post-commit
  admission may classify PREPARED to identify it, then rejects it without
  invoking `VerifyFrozenAppACLR1StateInTx`,
  `RequireDirectFrozenAppACLR1RuntimeInTx`, or frozen `AdmitAppACLRuntime` and
  without R2 payload, receipt, or manifest parsing or admission. The Slice 3
  identity-invariant verifier/predicate matrix remains its sole owner. The
  mismatch rows use a test identity fixture, never `SET ROLE`, membership,
  ownership, or credential handoff.
- [ ] GREEN: implement only the new R2 admission-wrapper/startup APIs using the
  prebuilt verifier and runtime predicate. Classification and exact R1 state
  verification occur in the same locked
  `REPEATABLE READ, READ ONLY` `pgx.Tx`; direct runtime admission then applies
  the separate predicate in that same transaction. Bootstrap/finalize use state
  verification plus their own actor gates inside locked `SERIALIZABLE` `pgx.Tx`
  closures. PREPARED may be classified to identify it but calls neither
  `VerifyFrozenAppACLR1StateInTx` nor
  `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen
  `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
  parsing or admission. Preserve all V1 serialization and entry-point behavior
  byte-for-byte.
- [ ] Run:

```bash
go test ./internal/center/store/migrate -run 'AppACLR2(State|RuntimeAdmission|Startup)|AppACLRuntimeAdmission|Canonical.*V1' -count=1
```

### Slice 7: PostgreSQL 16 Evidence And Completion Reviews

**Ownership:** Create
`internal/center/store/migrate/app_acl_r2_postgres_integration_test.go`; modify
`scripts/test-record-platform-integration.sh` to require the exact selected
PG16 image and reject every fallback; and modify `.github/workflows/ci.yml` to
add the required three-lane release gate. These are planned ownership paths
only in this planning task; do not change the Go test, script, or workflow now.

- [ ] RED/GREEN: cover R1 -> PREPARED -> FINALIZED; every wrong identity,
  application-source, member/dependency, domain, owner, ACL, and state failure;
  receipt immutability; no membership/role switch/drop/recreate; serializable
  retry/CAS/ACK loss; the full state/runtime identity matrix; the adversarial
  R1-to-PREPARED race; and R1/PREPARED/FINALIZED runtime routing. The PG16
  catalog matrix must assert allowed server/version, `pgcrypto` v1.3 catalog
  facts, each of the 36 `record_platform_internal` identity/identity-argument
  members, dependency/ACL baseline, direct-migrator extension ownership, OID-10
  member ownership, and rejection of an equal-cardinality substitution. It
  makes no raw server-file or artifact-provenance assertion. The runner accepts only
  `postgres:16.0`, `postgres:16.6`, or `postgres:16.12`; unset,
  `postgres`, `postgres:16`, `postgres:16-alpine`, and every other value fail
  before any fixture starts. `.github/workflows/ci.yml` must define a required
  `record-platform-pg16-catalog` matrix job with exactly those three literal
  image strings and no include/default fallback. Every lane invokes this same
  command with its matrix value.
- [ ] Run every real PostgreSQL 16 lane with roles created inside the fixture:

```bash
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.0 \
  scripts/test-record-platform-integration.sh postgres:16.0 -- \
  go test -v ./internal/center/store/migrate ./internal/center/platformmigrate ./cmd/houfeng-record-platform-admin \
  -run 'TestPostgresIntegrationAppACLR2|TestPostgresIntegration.*AppACLR2' -count=1
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.6 \
  scripts/test-record-platform-integration.sh postgres:16.6 -- \
  go test -v ./internal/center/store/migrate ./internal/center/platformmigrate ./cmd/houfeng-record-platform-admin \
  -run 'TestPostgresIntegrationAppACLR2|TestPostgresIntegration.*AppACLR2' -count=1
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.12 \
  scripts/test-record-platform-integration.sh postgres:16.12 -- \
  go test -v ./internal/center/store/migrate ./internal/center/platformmigrate ./cmd/houfeng-record-platform-admin \
  -run 'TestPostgresIntegrationAppACLR2|TestPostgresIntegration.*AppACLR2' -count=1
```

- [ ] Run the final focused/full quality gate:

```bash
gofmt -w db/appaclr2/migrations/*.go internal/center/store/migrate/app_acl_r2*.go internal/center/platformmigrate/app_acl_r2*.go cmd/houfeng-record-platform-admin/*.go
git diff --check
GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go
```

- [ ] Perform final spec-compliance review first, then independent code-quality
  review. Resolve all P0/P1/P2 findings, rerun the fixture/full Go gate, and
  request parent integration review only after both pass.

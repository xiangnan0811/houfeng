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
The remaining slices follow this gate prospectively. Slice 1's completed record
below is historical: it has retrospective mutation-based RED evidence, not a
chronological pre-implementation RED run, and that chronology must not be
rewritten.

### Governance Correction Gate Before Slice 3

This governance correction is a strict serial admission gate. Do not start
Slice 3 until an independent specification-compliance review of `task.json`,
`prd.md`, and `implement.md` reports zero P0/P1/P2 findings with final verdict
`SPEC_COMPLIANT`. Only then run an independent code-quality review of the same
artifacts; it must report zero P0/P1/P2 findings with final verdict `APPROVE`.
Resolve any finding and repeat both reviews in that order before admitting Slice
3.

This gate was satisfied before Slice 3 implementation began. Slice 3 has now
also passed its own ordered specification and quality reviews recorded below;
Slice 4 is closed only by the explicit reviewed governance exception recorded
in its evidence note below, and Slice 5 was then the next admitted slice. Slice
6 is closed by the evidence below; Slice 7 is the next admitted slice. This
does not claim chronological RED proof, Child 1 delivery, or delivery of any
parent task. Slice 7 remains open.

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

- [x] RETROSPECTIVE MUTATION RED ONLY: after the implementation existed,
  mutation checks proved that reintroduced obsolete root R2 evidence fails the
  Slice 1 gate. There was no chronological pre-implementation RED run, and none
  is claimed. The final-state selector is:

```bash
go test ./internal/center/store/migrate ./internal/center/platformmigrate ./cmd/houfeng-record-platform-admin -run 'NoRootR2|NoLegacyR2|FrozenR1' -count=1
```

- [x] GREEN/final gate: remove every draft occurrence and prove it:

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

> Evidence: `6e61bf8f` removed the obsolete root R2 implementation and
> `e75bde2f` restored the frozen R1 surfaces. `ac3b27f6` made the regression
> gate identifier-aware, and `328b42b4` is the final hermetic Slice 1 gate.
> There was no chronological pre-implementation RED; the RED evidence is
> retrospective mutation evidence only. Final specification-compliance review
> reported zero P0/P1/P2 findings with final verdict `SPEC_COMPLIANT`; the
> independent code-quality review reported zero P0/P1/P2 findings with final
> verdict `APPROVE`.

### Slice 2: Isolated Source And R2 Canonical Codecs

**Ownership:** Create `db/appaclr2/migrations/{embed.go,embed_test.go,
0052_app_acl_r2_privileged_transition.sql}` and
`internal/center/store/migrate/{app_acl_r2_source.go,app_acl_r2_source_test.go,
app_acl_r2_manifest.go,app_acl_r2_manifest_test.go}`. This slice owns only
source and manifest vectors; it does not consume the `domain_body` or
`l2_acl_body` literal corpus or own their decoders, malformed cases, or nesting.

- [x] RED: test root invisibility, frozen 52 prefix plus exact 0052, altered
  bytes, R1 magic, 2/4 bindings, 205/207 tuples, noncanonical ordering, and
  checksum substitution. `app_acl_r2_source_test.go` owns source-set valid hex,
  bad-magic, truncated-length, substituted-digest, duplicate, reorder, and
  trailing-byte vectors. `app_acl_r2_manifest_test.go` owns privilege,
  control-ACL, and M2 vectors plus count, role-map, grant-option, nested-digest,
  ordering, malformed-length, and trailing-byte vectors. The domain/L2 literal
  corpus and all of its decoder, malformed, and nesting coverage are exclusive
  to Slice 3 receipt tests; PostgreSQL tests assert live catalog equality only.
- [x] GREEN: implement separate R2 magic/strict EOF parser, 53 source entries,
  three bindings, and the separate 206-tuple body/digest contract without
  calling or widening V1 code. Do not touch `ConvergeAppACLR1`, R1 readers,
  `AdmitAppACLRuntime`, or generic `migrate --scope app`.
- [x] Run:

```bash
go test ./db/appaclr2/migrations ./internal/center/store/migrate -run 'AppACLR2(Source|Manifest|Privilege)' -count=1
go test ./internal/center/store/migrate -run 'FrozenR1|Canonical.*V1' -count=1
```

> Evidence: `cc8f0565` adds exactly the seven Slice 2 owned files. RED
> evidence predates implementation; both listed selectors, affected-package
> full tests, `go vet`, and `gofmt`/`git diff --check` passed. Independent
> specification and quality reviews each approved with zero P0/P1/P2 findings.

### Slice 3: Receipt SQL, Snapshot, And Identity-Blind Frozen R1 State Verification

**Ownership:** Modify isolated
`db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql` together
with `db/appaclr2/migrations/embed_test.go` and
`internal/center/store/migrate/{app_acl_r2_source.go,
app_acl_r2_source_test.go}`; create
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

Every Slice 3 edit to isolated `0052` is one atomic source-evidence change with
those three existing files. Refresh and review the complete embedded SQL bytes,
the full-file SHA-256, the `0052` entry in the fixed 53-source vector, and the
resulting full source-set digest. Piecemeal edits and stale-vector reuse are not
authorized.

- [x] RED: wrong allowed PG16 version/OID/direct session/superuser,
  missing/change domain identity, non-36 member count,
  `record_platform_internal` member namespace/OID/identity-argument/owner or
  dependency drift, extension version/schema/owner/ACL drift,
  application-source-hash drift, equal-cardinality member substitution, helper
  drift, and direct/effective receipt ACL drift all fail.
- [x] RED: consume the task-local fixed `domain_body` and `l2_acl_body`
  literals only in `app_acl_r2_receipt_test.go`; compare encoder/re-encoder
  bytes and SHA-256 directly to the documented hex/digest, reject every
  documented malformed body, and own all domain/L2 nesting, swap, and digest
  tamper coverage without deriving expectations from a production encoder,
  compiler, parser, or live database.
- [x] RED: use one exact frozen R1 catalog/source/ledger/ACL fixture. Assert
  that verification has no `session_user`/`current_user` branch, opens no pool
  or second transaction, and cannot call frozen `AdmitAppACLRuntime`. The real
  transaction-bound verifier retains documented authorized-R1-reader authority
  rather than promising all-identity PostgreSQL success. The separate runtime
  predicate alone accepts the matching center-runtime row and rejects every
  other row, including the test distinct-pair fixture without `SET ROLE`.
- [x] GREEN: implement immutable singleton receipt/ledger, bootstrap-owned
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
  identity-blind frozen R1 verifier over only tx-bound
  catalog/source/ledger/ACL evidence. `RequireDirectFrozenAppACLR1RuntimeInTx`
  is a separate direct-runtime predicate and is the only Slice 3 API allowed to
  inspect `session_user` or `current_user`.
- [x] Run:

```bash
go test ./db/appaclr2/migrations ./internal/center/store/migrate ./internal/center/platformmigrate \
  -run 'AppACLR2(Source|Receipt)|FrozenAppACLR1State|RequireDirectFrozenAppACLR1Runtime|DomainIdentity|DirectRole' -count=1
```

> Evidence: Slice 3 code commit
> `b8c057f3e074f1682e87cfb9f04ae23706f8120d` (`feat(record-platform): add app
> acl r2 bootstrap receipt`) has commit tree
> `535366c64e10e4e25ea528a4864c1a4ea0b8831e`, identical to the reviewed staged
> tree. It changes exactly the isolated `0052` SQL,
> `db/appaclr2/migrations/embed_test.go`, and
> `internal/center/store/migrate/{app_acl_r2_source.go,
> app_acl_r2_source_test.go,app_acl_r2_receipt.go,app_acl_r2_receipt_test.go,
> app_acl_r2_receipt_postgres.go,app_acl_r2_receipt_postgres_test.go,
> app_acl_r2_frozen_r1_verify.go,app_acl_r2_frozen_r1_verify_test.go}`: ten
> Slice 3 code paths. The cached patch SHA-256 is
> `2f0ee9c540de7e80016b40bf4f2579e32117d15a2de651661a92d9e547953753`; the
> isolated `0052` SQL SHA-256 is
> `7e15c579cd2055d61d1768c35556032f3ec4c17950c2a15ef7e5e22f4350fc01`; and the
> canonical 53-source digest is
> `6a2a82332c9646375434689255528565c612bd86e195aa854357b3f386e242a1`.
>
> A fixed `.git`-free snapshot of that tree passed the column-ACL/inheritance
> focused selector, the required three-package Slice 3 selector, the Frozen
> R1/Canonical V1 regression selector, full tests for
> `cmd/houfeng-record-platform-admin`, `internal/center/platformmigrate`, and
> `internal/center/store/migrate`, `go vet` for those packages, staged Go
> `gofmt -d`, and `git diff --cached --check`. The live PostgreSQL three-image
> matrix was not run in Slice 3 and remains intentionally assigned to Slice 7.
>
> Earlier P1 findings for pgcrypto inventory/helper exactness, trigger
> exactness, column ACLs, and bidirectional inheritance were fixed and
> re-reviewed. The final independent specification review reported zero
> P0/P1/P2 findings with verdict `SPEC_COMPLIANT`; the final independent quality
> review reported zero P0/P1/P2 findings with verdict `APPROVE`, re-confirmed
> the patch/SQL/source hashes and cached diff check, and ran in Codex review
> session `019fa47a-014d-7e92-87d7-bb14c0ffdbf8`. Neither review changed files
> or repository state.

The passing Slice 3 state-verifier and review gates historically admitted Slice
4. Bootstrap and finalizer must consume this verifier; neither may implement a
replacement verifier.

### Slice 4: Shared State Classifier And Bootstrap

**Ownership:** Create `app_acl_r2_catalog.go`, `app_acl_r2_catalog_test.go`,
`app_acl_r2_state.go`, `app_acl_r2_state_test.go`, `app_acl_r2_bootstrap.go`,
and `app_acl_r2_bootstrap_test.go`; for the direct-owner ACL correction only,
modify `internal/center/store/migrate/{app_acl_r2_manifest.go,
app_acl_r2_manifest_test.go}` to update the fixed M2 control-ACL value and its
independent vectors without changing the canonical wire layout; create
`internal/center/platformmigrate/app_acl_r2_transition_config.go` and its
test; modify only `cmd/houfeng-record-platform-admin/main.go` and
`cmd/houfeng-record-platform-admin/main_test.go` to register the new,
separately named `bootstrap --scope app-acl-r2` route. Do not modify any frozen
R1 admission, reader, convergence, startup, or generic migration file. This
slice consumes the passed Slice 3 verifier/predicate gate. Its catalog files
first own and test the reusable exact L1/M1/L2/M2/control-ACL relation/head
predicates; `app_acl_r2_state.go` composes them into typed state, and bootstrap
consumes the full classifier only after its ordinary-bootstrap metadata-only
rejection gate. Do not implement the classifier or bootstrap before that
catalog predicate gate passes.

- [x] RED gate closed by Slice 4-only governance exception: trustworthy
  chronological pre-implementation RED proof for this catalog/state/classifier
  batch is unavailable and is not claimed. Later executable mutation/regression
  evidence remains retrospective and is never relabeled as chronological RED.
  `app_acl_r2_catalog.go` and its test establish the reusable
  exact L1/M1/L2/M2/control-ACL relation/head predicates, including absent,
  one-sided, extra, wrong-owner, wrong-link, wrong-head, and mixed shapes.
  Exact M2 coverage must distinguish owner OID from ordinary native access:
  each table requires exact non-grantable direct-migrator owner-self and
  center-runtime `SELECT` ACL entries plus true direct/runtime
  `has_table_privilege(..., 'SELECT')`; the helper requires its exact
  non-grantable direct-migrator owner-self `EXECUTE` entry plus true
  `has_function_privilege(..., 'EXECUTE')`. It rejects a correct owner OID
  with a missing/revoked owner ordinary grant, any owner grant option, an extra
  ACL entry, or a changed fixed table/helper mask (`0x06`/`0x02`). The exact
  `aclexplode` assertions include owner rows; they must not filter the owner
  baseline away. The manifest codec/vector update retains its existing field
  layout and order while encoding the tag-2 owner self-grants.
  `ClassifyAppACLR2State` in the single state file then recognizes only exact
  R1, PREPARED, FINALIZED, and CORRUPT by composing those predicates; it rejects
  unknown/mixed object shapes. The identity-invariant classifier matrix is
  pure predicate composition only: it uses synthetic identity labels with
  otherwise identical exact R1, PREPARED, and FINALIZED evidence inputs, opens
  no PostgreSQL transaction, and returns the same state without reading
  `session_user`/`current_user`. It makes no all-identity real-reader claim;
  Slice 7 owns the native-ACL PG16 authority matrix.
  Bootstrap reads only
  `HOUFENG_RECORD_PLATFORM_APP_BOOTSTRAP_DATABASE_URL`, rejects unexpected
  lookup before pool open, proves lock order/pre-mutation checks, retries only
  whole `40001`/`40P01` closure, applies its OID-10 actor gate before the
  metadata inventory, uses the prebuilt identity-blind frozen-R1 state verifier
  only on the post-gate full-classifier path, and recovers uncertain commit
  acknowledgement only through the private
  `observeAppACLR2BootstrapACKRecoveryInTx` observer. In its fresh locked
  SERIALIZABLE recovery closure, that observer reads the frozen verifier and
  complete `app_acl_r2_*` catalog name/identity inventory; only an exact L2
  inventory permits it to read the receipt and perform the exact L2/catalog
  proof. Empty inventory plus frozen R1 is exact R1; exact L2 inventory plus
  one valid receipt/equality proof is exact PREPARED. Any unknown, incomplete,
  excessive, mixed, or M2-reserved inventory, any L2 drift, or any read error
  returns an error with no outcome. Exact PREPARED is success, exact R1 is a
  retryable prior state, and every error is failure. The observer never calls
  `ClassifyAppACLR2State` or `ReadAppACLR2CatalogPredicatesInTx`, reads neither
  M2 relation's contents, takes no M2 table lock, and reads no M2
  predicate/manifest/control-ACL or helper/trigger-definition data beyond the
  permitted name/identity inventory. It never invokes FINALIZED classification,
  which would rely on superuser bypass rather than native M2 `SELECT`. Recovery
  checks the error before its outcome and never treats a zero-value `CORRUPT`
  accompanying an error as an evidence verdict. Bootstrap coverage must prove
  this exact permitted-read/lock trace and every rejection path.
- [x] RED gate closed by Slice 4-only governance exception: trustworthy
  chronological pre-implementation RED proof for this bootstrap batch is
  unavailable and is not claimed. Later executable mutation/regression evidence
  remains retrospective and is never relabeled as chronological RED. Add
  executable
  `TestBootstrapAppACLR2OrdersActorInventoryBeforeClassifier` and
  `TestBootstrapAppACLR2RejectsM2OrUnknownInventoryWithoutClassifierOrM2Access`
  dependency-trace tests. Ordinary bootstrap must execute exactly `OID-10 actor
  gate -> reserved-object metadata-only inventory -> reject M2/unknown presence
  -> only when M2/unknown are absent, invoke the full shared classifier`. The
  absent-M2/unknown fixture must observe actor gate, then inventory, then one
  full-classifier call. Each M2-reserved and unknown-reserved fixture must fail
  closed immediately after inventory and observe zero full-classifier calls,
  zero M2 content reads, zero M2 table locks, zero M2 scans, zero M2
  aggregations, and no `FINALIZED` classification. The metadata-only inventory
  is permitted here as the ordinary-bootstrap actor-gated rejection gate in
  addition to its use by uncertain ACK recovery; it returns no state and does
  not weaken the shared classifier contract.
  After the actor gate and M2/unknown-absent inventory,
  `TestBootstrapAppACLR2ExactR1ClassifierResultContinuesToVerifierPreflightAndL2DDL`,
  `TestBootstrapAppACLR2PreparedClassifierResultIsNoMutationRepeat`,
  `TestBootstrapAppACLR2CorruptClassifierResultRejectsWithoutPostClassifierWork`,
  and `TestBootstrapAppACLR2ClassifierErrorPropagatesWithoutPostClassifierWork`
  must prove exact R1 alone invokes the frozen-R1 verifier, PG16 preflight,
  and L2 DDL; PREPARED is a target-state no-mutation repeat and CORRUPT
  rejects fail closed, each with zero verifier, preflight, or L2-DDL calls;
  and a classifier operational error returns the original error with zero of
  those calls.
- [x] GREEN: implement the read-only reusable catalog predicates, then the
  classifier that composes them. In the ordinary-bootstrap serializable
  transaction, implement exactly the actor gate, metadata-only inventory,
  direct fail-closed M2/unknown rejection, and only-when-absent full-classifier
  sequence proved by the RED dependency traces. The rejection branch must make
  none of the forbidden classifier or M2 read/lock/scan/aggregation calls and
  must not classify `FINALIZED`. After the post-inventory classifier call, only
  exact R1 may invoke the frozen-R1 verifier, PG16 preflight, and L2 DDL; exact
  PREPARED is the target-state no-mutation repeat, CORRUPT rejects fail closed,
  and a classifier operational error propagates unchanged, with those three
  paths making none of those later calls. Create only receipt/helpers/L2, and
  leave both
  `app_acl_r2_manifest_revisions` and `app_acl_r2_manifest_head` absent. Never
  create roles, transfer owner, recreate pgcrypto, invoke finalizer, or alter
  frozen M1. After an uncertain bootstrap commit acknowledgement, invoke only
  the private ACK observer defined above; do not route that recovery through the
  public full classifier.
- [x] Run:

```bash
go test ./cmd/houfeng-record-platform-admin ./internal/center/platformmigrate ./internal/center/store/migrate -run 'AppACLR2(Catalog|State|Bootstrap|Manifest)|BootstrapAppACLR2|AppACLR2Config' -count=1
```

> Evidence and governance exception: trustworthy chronological
> pre-implementation RED proof for the initial Slice 4 catalog/bootstrap work
> cannot be recovered, is unavailable, and is not claimed. The two RED gates
> above are closed only for this already-completed slice by an explicit reviewed
> governance exception. Its substitute record is executable coverage; later
> real mutation/regression RED evidence only where it genuinely exists, kept
> retrospective and never relabeled as chronological RED; complete GREEN
> verification; ordered independent read-only specification and quality
> approval; and fresh controller reruns. This exception is not prospective and
> leaves Slices 5-7 open.
>
> The exact committed Slice 4 chain is:
> `b610f591f1805ec990e36409da628c91c9062e8b` (catalog/state predicate and
> classifier batch, including the direct-owner manifest ACL correction), changing exactly
> `internal/center/store/migrate/{app_acl_r2_catalog.go,
> app_acl_r2_catalog_test.go,app_acl_r2_manifest.go,
> app_acl_r2_manifest_test.go,app_acl_r2_state.go,
> app_acl_r2_state_test.go}`;
> `485effd0d62433471cf72d777c899336f322f99c` (Trellis bootstrap-contract
> alignment/docs checkpoint), changing exactly
> `.trellis/tasks/07-27-app-acl-r2-privileged-transition/{design.md,
> implement.md,prd.md}`; then
> `732298ee6176b7a3b4a988c5b9e8d9114501653f` (bootstrap/config/admin command
> batch), changing exactly
> `cmd/houfeng-record-platform-admin/{main.go,main_test.go}`,
> `internal/center/platformmigrate/{app_acl_r2_transition_config.go,
> app_acl_r2_transition_config_test.go}`, and
> `internal/center/store/migrate/{app_acl_r2_bootstrap.go,
> app_acl_r2_bootstrap_test.go}`.
>
> Against the final Slice 4 state at `732298ee6176b7a3b4a988c5b9e8d9114501653f`,
> read-only specification task `slice4-bootstrap-cleanup-spec-review-0729`
> preceded the independent read-only quality task
> `slice4-bootstrap-cleanup-quality-review-0729`. Their final verdicts were
> respectively `SPEC_REVIEW_PASS` and `QUALITY_REVIEW_PASS`, each with
> P0=0, P1=0, and P2=0; neither review changed files or repository state.
>
> After those reviews, the controller freshly passed the two cleanup
> regressions
> `TestAppACLR2BootstrapLockedBeginDiscardsConnectionWhenSessionUnlockFails`
> and
> `TestAppACLR2BootstrapLockedBeginDiscardsPostHandoffUnlockFailureWithBoundedContext`,
> the required three-package focused selector above, full tests for
> `cmd/houfeng-record-platform-admin`, `internal/center/platformmigrate`, and
> `internal/center/store/migrate`, `go test ./db/appaclr2/migrations -count=1`,
> and `go vet` for the three affected packages. `gofmt -d` was empty, tracked
> and untracked whitespace checks were clean, and immediately before the final
> commit the staged allowlist was verified as exactly the six files listed for
> `732298ee6176b7a3b4a988c5b9e8d9114501653f` above.

### Slice 5: Direct-Finalizer M2 Relations, 206-Tuple Catalog, And CAS

> **2026-07-29 correction:** This is a pre-code-review PostgreSQL 16
> specification correction, not Slice 5 completion and not a claim of PG16
> integration evidence. A SELECT-only, non-owner direct finalizer locks a
> present bootstrap-owned L2 receipt with `ACCESS SHARE`, not
> `SHARE ROW EXCLUSIVE`.
>
> **2026-07-29 prospective P1 correction:** This changes only the unimplemented
> Slice 5-7 plan. It preserves all checked historical Slice 3/4 checkboxes and
> evidence and makes no retrospective implementation or PG16-evidence claim.
> Before implementation, planning review must approve this correction; after
> implementation, specification review, independent quality review, and full
> verification must pass in that order. The correction does not authorize a
> receipt/manifest wire, golden-vector, L2 three-object ACL, M2 SQL/ACL,
> 53-source, 206-tuple, or hash change.
>
> **2026-07-29 KEEP_DIRECT_OWNER correction:** PostgreSQL 16 owner-native
> privileges make the earlier revoked-owner expectation invalid. Slice 5 keeps
> direct M2 ownership, exact `aclexplode` owner self-rows, revoke-first DCL, the
> fixed `0x06`/`0x02` ordinary-reader body, isolated `0052`, vectors, hashes,
> one direct-migrator DSN, and no helper or ownership transfer. All seven queried
> M2 table privileges are true for the direct owner; center runtime remains
> `SELECT`-only and platform admin has none. `has_*_privilege` proves
> reachability, not grant provenance. A revoked owner self-row remains corrupt
> solely because the raw ACL shape is wrong while owner-native reachability
> remains true. Keeping ownership accepts that a hostile owner can alter/drop
> M2 objects, functions, constraints, or triggers, perform native DML, and grant
> access; exact readback detects drift when run but cannot confine that owner.
> Moving ownership is a material redesign requiring membership/`SET ROLE`, a
> helper, a second creator DSN, or bootstrap precreation and remains out of
> scope.

**Ownership:** Create `app_acl_r2_finalize.go` and its test; modify isolated
`db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql`,
`db/appaclr2/migrations/embed_test.go`,
`internal/center/store/migrate/{app_acl_r2_source.go,
app_acl_r2_source_test.go}`, and admin command files only for the separately
named `finalize --scope app-acl-r2` route. Every Slice 5 edit to isolated `0052`
is one atomic source-evidence change with those three existing files: refresh
and review the complete embedded SQL bytes, the full-file SHA-256, the `0052`
entry in the fixed 53-source vector, and the resulting full source-set digest.
Piecemeal edits and stale-vector reuse are not authorized. This slice consumes
the passed Slice 4 reusable L1/M1/L2/M2/control-ACL relation/head predicate and
builds finalizer behavior on it; it does not create a finalizer-private catalog
predicate or catalog-test fork. The only M2 persistence owned by this slice is new
`public.app_acl_r2_manifest_revisions` and
`public.app_acl_r2_manifest_head`; 0051 V1 manifest relations are read-only
predecessors and are never inserted into, altered, re-owned, or advanced.

**Temporary P1 correction ownership:** The existing eight Slice 5 paths (the
two finalizer files, isolated `0052` SQL, `embed_test.go`, the two source files,
and the two route-specific admin command files) remain owned, and this
correction additionally permits only
`internal/center/store/migrate/app_acl_r2_receipt_postgres.go`,
`internal/center/store/migrate/app_acl_r2_receipt_postgres_test.go`,
`internal/center/store/migrate/app_acl_r2_catalog.go`,
`internal/center/store/migrate/app_acl_r2_catalog_test.go`,
`internal/center/store/migrate/app_acl_r2_bootstrap.go`, and
`internal/center/store/migrate/app_acl_r2_bootstrap_test.go`. Those temporary
paths establish one shared constrained post-bootstrap classifier/predicate; they
do not authorize a finalizer-private classifier, predicate, snapshot, verifier,
privilege, helper, DSN, or ACL surface.

**2026-07-31 Slice 5 source/review evidence:** The five independent findings
below are checked only for current implementation, focused Go verification, and
the passed specification/quality reviews. They do not claim a PG16 integration
lane or any Slice 6/7, parent, Child 1, or PF-AC completion.

- [x] Enforce a strict explicit finalizer connection source. Only
  `HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL` may reach the
  finalizer-specific opener; reject ambient service/pass/TLS-file/default
  sources before pool creation and prove there is no generic-opener fallback.
- [x] Bound ACK-retry continuation within the same total attempt budget as the
  main finalizer loop. Retryable recovery errors and exact PREPARED recovery
  results consume attempts and cannot create a nested or unbounded retry loop.
- [x] Instantiate the real constrained finalizer connection/transaction wrapper
  in tests and prove the production route reserves one connection and holds its
  advisory-lock/transaction boundary; a permissive method-compatible fake alone
  is insufficient evidence.
- [x] Add the DDL retry/bounds matrix: `40001` and `40P01` during finalizer DDL
  roll back the whole attempt and retry within the cap; non-retryable DDL errors
  stop; exhaustion stops; and fixed embedded finalize-section/body cardinality
  and size bounds reject before mutation.
- [x] Correct the owner-native verifier and tests: all seven direct-owner table
  probes and owner helper `EXECUTE` are true, runtime is table-`SELECT`-only,
  admin has none, and missing owner self-ACL rows fail the raw-ACL predicate
  while owner-native probes remain true.

- [ ] P1 correction RED/TDD plan: prove the OID-10 bootstrap-only live reader
  calls `pg_control_system()` and rejects a live-system/domain mismatch; prove
  the shared constrained PREPARED/FINALIZED reader never calls it and succeeds
  when a direct call would be SQLSTATE `42501`; reject fresh database OID/name
  drift and receipt/domain disagreement; retain the bootstrap-only live verifier
  before ordinary PREPARED-repeat and bootstrap ACK-recovery success; and prove
  finalizer deletes private classifier/predicate/snapshot duplication and uses
  the shared path for preflight, post-DCL readback, normal repeat, and ACK
  recovery.

- [ ] RED: finalize reads only
  `HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL`, rejects R1/nonexact
  PREPARED/direct-role/M1/receipt drift through the one shared constrained
  predicate; rejects
  pre-existing, one-sided,
  empty, extra, wrong-owner, wrong-link, or wrong-head M2 shape; rejects
  205/207 catalog; detects stale head CAS; verifies one M2 revision/one true
  singleton head; proves full transaction rollback; and reclassifies FINALIZED
  ACK loss. Its DCL/readback tests must require the two non-grantable
  direct-migrator/center-runtime `SELECT` entries on each M2 table and the one
  non-grantable direct-migrator `EXECUTE` entry on the helper. Add RED-first
  unit cases that expect all seven direct-owner table probes and owner helper
  `EXECUTE` true, only center-runtime table `SELECT` true, and every queried
  platform-admin privilege false. The inverse fixtures remove an owner
  self-`SELECT` or self-`EXECUTE` row while keeping those owner-native probes
  true; they must fail the exact `aclexplode` predicate, not an effective-access
  predicate. Grant option and extra-row variants also reject. Finalizer ACK
  recovery may read through owner-native authority but must reject any raw ACL
  shape other than the exact self-row baseline. The direct finalizer must lock a
  present bootstrap-owned L2 receipt in `ACCESS SHARE`, never a stronger mode,
  and never grant itself receipt write privilege; all other present state/M2
  tables retain `SHARE ROW EXCLUSIVE`. The real PostgreSQL 16 lane must prove
  the receipt-`SELECT`-only direct migrator succeeds with `ACCESS SHARE` and would receive
  SQLSTATE `42501` under the superseded `SHARE ROW EXCLUSIVE` receipt-lock
  contract.
- [ ] GREEN: validate receipt through the one shared constrained
  `ReadAppACLR2CatalogPredicatesInTx`/`ClassifyAppACLR2State` path first, then
  execute the finalizer section in one serializable transaction after
  identity-blind state verification and its own direct-migrator actor gate
  (the only finalizer session-identity check). It never calls
  `pg_control_system()` or creates a private classifier, predicate, snapshot, or
  verifier. After those pre-mutation checks, the in-transaction mutation/readback
  sequence is one mandatory ordered
  contract: (1) DDL creates the direct-migrator-owned M2 relation pair with
  plain `CREATE TABLE` and creates its immutable triggers; (2) the M2
  revision/head writes insert exactly one
  `(protocol_version, manifest_revision) = (2, 2)` revision plus exactly one
  true singleton head, bind immutable M1 revision/digest/source/privilege/role
  link fields, store the separate three-binding/206-tuple body/digest, and
  complete the M1 revision/digest CAS; (3) ACL normalization runs revoke-first
  DCL, revoking the five control grantees, then granting no-grant-option table
  `SELECT` to direct migrator and center runtime and no-grant-option helper
  `EXECUTE` only to direct migrator; and (4) only after ACL normalization,
  FINALIZED readback re-reads receipt/catalog/head through the same shared
  constrained path before commit. That readback proves owner OID, explicit owner
  self-ACL, and `has_*_privilege` separately without treating reachability as
  ACL provenance. The corrected verifier expects all seven direct-owner table
  privileges and owner helper `EXECUTE`, center-runtime `SELECT` only, and no
  platform-admin table/function privilege;
  default-ACL absence never substitutes for the owner self-grants. No FINALIZED
  readback may occur before ACL normalization. Any error rolls back all M2
  DDL/DML to exact PREPARED.
- [ ] Run:

```bash
go test ./db/appaclr2/migrations ./cmd/houfeng-record-platform-admin ./internal/center/store/migrate \
  -run 'AppACLR2(Source|Finalize|Manifest|M2)|VerifyAppACLR2M2' -count=1
```

> **2026-07-31 evidence note:** Specification review
> `019fb5fd-f639-7fd0-b57c-3676d95ba659` = `SPEC RESULT PASS`, P0/P1/P2 = 0;
> independent quality review `019fb610-234f-7103-9d5d-5bbf045f39a9` =
> `QUALITY RESULT PASS`, P0/P1/P2 = 0. Its reported focused selector,
> affected-package `go vet`, `gofmt -d`, and `git diff --check` all exited 0.
> Scope is the Slice 5 isolated source/M2 DDL, direct finalizer route,
> shared continuity, retry/ACK, and owner provenance/reachability checks only.
> It deliberately leaves every Slice 6/7, PG16, R2 total-acceptance, Child 1,
> and PF-AC checkbox unclaimed.

### Slice 6: Separate R2 Admission And Startup Route

**Ownership:** Create `app_acl_r2_runtime_admission.go` and
`app_acl_r2_runtime_admission_test.go`. The admission file owns the new
`AdmitAppACLR1OnlyRuntime`, `AdmitAppACLR2Runtime`, and
`StartAppACLR2Runtime` APIs and consumes the already-tested Slice 3 verifier,
direct-runtime predicate, and shared constrained post-bootstrap classifier.
It adds no private runtime classifier/predicate fork, privilege, helper, or
DSN. `app_acl_r2_runtime_admission_test.go` owns the
mandatory adversarial `TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace` and its
paired PREPARED-classification-only assertions. No file here edits
`AdmitAppACLRuntime`, any R1 reader, `ConvergeAppACLR1`, an existing R1 startup
route, or generic `migrate --scope app`. There is no
`app_acl_r2_dispatch.go`.

- [x] RED: frozen V1 parser/reader/converger and generic app migration receive
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
  without R2 payload, receipt, or manifest parsing or admission. Slice 4 owns
  the pure predicate-composition identity-invariant matrix; real PostgreSQL
  reader authority remains exclusive to Slice 7. The mismatch rows use a test
  identity fixture, never `SET ROLE`, membership, ownership, or credential
  handoff.
- [x] GREEN: implement only the new R2 admission-wrapper/startup APIs using the
  prebuilt verifier, runtime predicate, and shared constrained classifier;
  never add a runtime-private classifier/predicate, privilege, helper, or DSN.
  Classification and exact R1 state verification occur in the same locked
  `REPEATABLE READ, READ ONLY` `pgx.Tx`; direct runtime admission then applies
  the separate predicate in that same transaction. Finalizer uses state
  verification plus its own actor gate inside a locked `SERIALIZABLE` `pgx.Tx`
  closure. Ordinary bootstrap uses its OID-10 gate and metadata-only
  M2/unknown rejection before invoking full classification/state verification
  inside its locked `SERIALIZABLE` closure. PREPARED may be classified to
  identify it but calls neither
  `VerifyFrozenAppACLR1StateInTx` nor
  `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen
  `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
  parsing or admission. Preserve all V1 serialization and entry-point behavior
  byte-for-byte.
- [x] Run:

```bash
go test ./internal/center/store/migrate -run 'AppACLR2(State|RuntimeAdmission|Startup)|AppACLRuntimeAdmission|Canonical.*V1' -count=1
```

### Slice 6 Evidence Note — 2026-07-31

- Closed local delivery-candidate chain: production
  `518b3dfe571ba9329fe4aedf51fceb0378f95249`; cleanup/fault proof
  `8ea72a30dee9f9153447af0ba0cfdbe158018151`; physical lock-identity proof
  `9a607d0807b53823b6f8c00867e8f199cc91ab27`; frozen-verifier plus public
  `StartAppACLR2Runtime` AST proof
  `a6a2fb68bf17caee289c9569536666dfba7ecedf`; and final code-spec alignment
  `cc59844fecd5ce96759983cf9255b65279b66e20`. This governance commit starts
  from the independently reviewed `cc59844f` baseline.
- Authoritative contracts are unchanged: R1
  `503d58670dc790c4b852bfb58cf93d2b816c1ce956958567dc605cb28d5cd23f`
  (52 sources / 204 tuples); R2
  `23f79c60dcede45a42aae82da5a9de0d3d650d7eef64dbfd7ce96c6dd5d95fff`
  (53 sources / 206 tuples); canonical 53-source digest
  `1d9dc20e71e9f319f8b1cef4b22f9dc92051a88dc9cb8a892b69494658c44dd3`.
- Review trail: initial quality `019fb66e...` FAIL was closed by
  `019fb68e...` PASS; specification `019fb6a4...` FAIL was adjudicated by
  `019fb6b2...` and repaired in `019fb6ba...`; final combined review
  `019fb6c8-df27-7091-8c6b-08133de76c6d` PASS reported
  Critical/Important/Minor = 0.
- Three mutation RED checks all failed and were exactly restored: lock seed
  `0 -> 1`; frozen-verifier error swallowing; and replacement of the public
  `StartAppACLR2Runtime` delegate.
- Fresh PASS gates on the reviewed code: (1) required Slice 6 focused selector
  `go test ./internal/center/store/migrate -run 'AppACLR2(State|RuntimeAdmission|Startup)|AppACLRuntimeAdmission|Canonical.*V1' -count=1`;
  (2) focused verifier-error/public-Start AST selector
  `go test ./internal/center/store/migrate -run 'TestAdmitAppACLR2RuntimePropagatesFrozenVerifierErrorsAndCleansUp|TestStartAppACLR2RuntimeDirectlyDelegatesToR2Admission' -count=1`;
  (3) race selector
  `go test ./internal/center/store/migrate -run '^TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace$' -count=1`;
  (4) full `go test ./internal/center/store/migrate -count=1`; (5)
  `go vet ./internal/center/store/migrate`; (6) empty
  `gofmt -d internal/center/store/migrate/app_acl_r2_runtime_admission.go internal/center/store/migrate/app_acl_r2_runtime_admission_test.go`; and (7)
  `git diff --check`. Immediately before this documentation edit, status was
  clean and both Slice 6 code blobs equaled their `cc59844f` blobs.
- Remote boundary: PR #384 is Draft/Open/MERGEABLE, base `main@3a7f31e`, with
  remote Slice 5 head `codex/app-acl-r2-slice5-finalizer@40c7c8c`; its existing
  green go/web/web-browser/docker-image checks are Slice 5-only and are not
  Slice 6 CI. Controller push, new Slice 6 CI, PR verification, and merge are
  still pending.
- Boundary: no PostgreSQL 16 Slice 7 reader-authority conclusion; no
  remote/CI/merge conclusion; and no R2 microtask, Child 1, PF-AC, or parent
  completion. Slice 7 and all of its checkboxes remain unclaimed.

### Slice 7: PostgreSQL 16 Evidence And Completion Reviews

#### Immutable pgcrypto receipt-contract repair (must precede Slice 7)

**Single contract:** retain the production reader's exact
`pg_get_function_identity_arguments(oid)` output. The zero-based member index
16 is exactly `record_platform_internal.pgp_armor_headers|text, OUT key text,
OUT value text`, and the sorted 36-member digest is
`57e7ac6a986705d8fa1e5b2260c1836b74dffe1b33bee00d65d1b275284e8196`.
The production member query and its static reader test may use only
`pg_catalog.pg_get_function_identity_arguments(procedure.oid)`. Do not derive
a different member string from `proargtypes`: it records the callable input
(`text`) but drops the formatter's bound record result shape.

**Rejected alternatives:** `pg_catalog.pg_get_function_arguments(procedure.oid)`
is rejected even if current PostgreSQL 16 output happens to match: it includes
default-argument semantics and is not the contract's specified formatter.
Changing the reader to `oidvectortypes(proargtypes)` and retaining the prior
input-only digest is rejected; it conflicts with the exact server formatter and
permits result shape drift. Any pgcrypto-specific stripping of formatter-emitted
`OUT` terms is rejected for the same reason. A version-specific digest allowlist
is rejected because
`postgres:16.0`, `postgres:16.6`, and `postgres:16.12` all have pgcrypto `1.3`,
the same 36 rows, and the same full-formatter digest.

**Ownership:** before the existing Slice 7 integration/runner/CI work, modify
only `internal/center/store/migrate/app_acl_r2_receipt.go`,
`internal/center/store/migrate/app_acl_r2_receipt_postgres.go`,
`internal/center/store/migrate/app_acl_r2_receipt_test.go`, and
`internal/center/store/migrate/app_acl_r2_receipt_postgres_test.go`.
The correction retains the reader and changes its fixed member-16 literal and
fixed digest, together with direct tests. It must not change `0051`, isolated
`0052`, the migration/source checksums, protocol number, L2/domain golden
vectors, SQL, ACLs, runner, workflow, or any frozen R1 surface.

**RED then GREEN:** first prove the old fixed input-only catalog expectation
rejects the real `pgp_armor_headers` formatter row in each allowed image. The
static catalog-reader test must assert
`pg_get_function_identity_arguments(procedure.oid)`, reject a
`pg_get_function_arguments(procedure.oid)` replacement, an input-only
`oidvectortypes(procedure.proargtypes)` replacement, and any pgcrypto-specific
`OUT` stripping, then use the exact full member-16 fixture. The member validator
must reject both the old input-only row/digest and equal-cardinality
substitutions, including altered `OUT` names, modes, or types. Then update only
the fixed member/digest expectations and watch those tests turn green. The real
PG16 coverage must independently assert the full 36-member list/digest and all
three negative forms in
`postgres:16.0`, `postgres:16.6`, and `postgres:16.12`.

**Receipt and compatibility boundary:** changing this literal changes the
receipt body and receipt SHA-256; because M2 serializes `receipt_digest`, it
also changes the deployment's M2 digest. These are runtime-derived values, not
new literal golden vectors. No released R2 receipt exists on `origin/main` /
`v0.59.0`, and PR #384 is Draft, so do not introduce a dual parser, receipt
rewrite, protocol bump, or migration checksum bump. An input-only pre-release
receipt fails closed. The future implementation must prove unchanged
`sha256sum` values for `db/migrations/0051_create_record_platform_foundation.sql`
and `db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql`.

**Order:** land this contract correction with its focused unit tests and local
Go verification first. Then land the existing Slice 7 integration test, strict
runner, and three-image CI matrix on the corrected contract; only afterward may
controller-owned push/CI/required-check work begin. This planning entry records
no RED/GREEN execution, Slice 7 completion, R2 completion, Child 1, PF-AC, or
parent completion.

After the future GREEN edit, run the focused receipt selectors, affected-package
tests, `go vet ./internal/center/store/migrate`, empty `gofmt -d` for the four
owned Go files, and `git diff --check`; then run
`env GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go`. Exercise each
real image through the strict later Slice 7 entry point:
`HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.0 scripts/test-record-platform-integration.sh pg16-catalog -- go test -json ./internal/center/store/migrate -run '^TestPostgresIntegrationAppACLR2$' -count=1`,
with the same command for `postgres:16.6` and `postgres:16.12`. The RED result
must be observed before changing the fixed constant; no planning-only result is
reported as GREEN.

**Ownership:** Create
`internal/center/store/migrate/app_acl_r2_postgres_integration_test.go`; modify
`scripts/test-record-platform-integration.sh` only to add the strict
`pg16-catalog` mode; and modify `.github/workflows/ci.yml` to add its three-lane
catalog job. `postgres` and parent-owned `postgres-s3` retain their existing
names, signatures, and behavior; Slice 7 does not rename, remove, or tighten
either mode. The strict mode signature is
`HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=<exact-image> scripts/test-record-platform-integration.sh pg16-catalog -- <command> [args...]`.
It accepts only `postgres:16.0`, `postgres:16.6`, or `postgres:16.12`, and
rejects missing/other values before `mktemp`, random/password or port work,
Docker, containers, or fixture exports. These three paths are exclusive to
Slice 7; Slices 1-6 do not change the Go test, script, or workflow. The new CI
job must be executable from a fresh runner: `runs-on: ubuntu-latest`, then
`actions/checkout@v6`, then `actions/setup-go@v6` with
`go-version-file: go.mod`, before its exact matrix command.

- [ ] RED/GREEN: cover R1 -> PREPARED -> FINALIZED; every wrong identity,
  application-source, member/dependency, domain, owner, ACL, and state failure;
  receipt immutability; no membership/role switch/drop/recreate; serializable
  retry/CAS/ACK loss; the adversarial R1-to-PREPARED race; and
  R1/PREPARED/FINALIZED runtime routing. Every PG16 lane must prove the OID-10
  bootstrap-only live binding through `pg_control_system()`, while direct
  migrator and runtime have no direct or effective `EXECUTE` on
  `pg_control_system()`, no `pg_monitor` membership,
  and no `pg_control_system()` query in their PREPARED/FINALIZED classifier,
  finalizer, or runtime traces. It must prove SQLSTATE `42501` for unreadable
  present L2/M2 evidence, fresh database OID/name rejection, unchanged
  206-tuple/L2/M2 contracts, and both accepted residual risks: no clone/restore
  detection claim and no confinement claim against the direct M2 owner. Run and
  reverify the already-owned
  Slice 4 pure predicate-composition and Slice 3 runtime-predicate unit matrices;
  Slice 7 neither recreates nor takes ownership of them. Its only new matrix
  ownership is the real PG16 reader-authority matrix. The real
  PG16 reader matrix must retain documented authority for existing authorized
  R1 evidence readers; allow PREPARED only with native L2 `SELECT`; allow
  FINALIZED only for direct migrator with owner-native M2 authority and center
  runtime with granted M2 `SELECT`; propagate SQLSTATE `42501` for
  platform-admin/unrelated callers
  when a present evidence relation is unreadable without translating it to
  `CORRUPT`; prove bootstrap does not invoke FINALIZED classification; and
  treat any zero-value `CORRUPT` accompanying an error as no evidence verdict.
  In every PG16 lane, begin with a normal direct-owner finalization and assert
  the exact M2 owner/self-ACL baseline separately: `aclexplode` contains the two
  non-grantable table entries and one non-grantable helper entry, including the
  owner rows; all seven queried privileges are true for direct migrator on both
  tables; owner helper `EXECUTE` is true; center runtime has table `SELECT` only
  and no helper `EXECUTE`; and platform admin has none. Then run inverse owner
  revocation regressions: remove direct migrator's self-`SELECT` or
  self-`EXECUTE` row while retaining the same owner OID and prove the relevant
  owner-native `has_*_privilege` stays true while the exact raw-ACL predicate,
  classifier, normal repeat, and finalizer ACK-loss recovery reject the shape.
  The ACK-loss success lane must prove exact self-ACL evidence but must not claim
  that the self-row, rather than ownership, caused the M2 read.
  One real PG16 commit-ACK-loss scenario must let bootstrap commit PREPARED and
  then have recovery observe that the database has already advanced to exact
  FINALIZED (or begin recovery against an already exact FINALIZED database).
  Bootstrap must fail. The fixture query trace must prove that the private
  bootstrap ACK observer first proves frozen R1 and uses only the complete
  reserved-object name/identity inventory to reject any M2 presence; it must not
  read either M2 relation's contents, take an M2 table lock, read any M2
  predicate/manifest/control-ACL or helper/trigger-definition data, or classify
  M2/FINALIZED through superuser bypass. The same suite must prove its
  empty-inventory exact-R1 and exact-L2-inventory/one-receipt exact-PREPARED
  outcomes, and error-first failure for unknown, partial, excessive, mixed, and
  every M2-reserved inventory. Observing reserved M2 presence solely to fail is
  not classification.
  Any `(CORRUPT, err)` result with `err != nil` is error-only and the zero-value
  `CORRUPT` must not be accepted as an evidence verdict.
  The PG16
  catalog matrix must assert allowed server/version, `pgcrypto` v1.3 catalog
  facts, each of the 36 exact server-formatted
  `record_platform_internal` member signatures, dependency/ACL baseline,
  direct-migrator extension ownership, OID-10 member ownership, and rejection
  of an equal-cardinality substitution. It requires the zero-based member-16
  `pgp_armor_headers` value `text, OUT key text, OUT value text` and digest
  `57e7ac6a986705d8fa1e5b2260c1836b74dffe1b33bee00d65d1b275284e8196`, and
  rejects the input-only `text` form as well as result-shape substitutions. It
  makes no raw server-file or artifact-provenance assertion. The
  `pg16-catalog` runner mode accepts only `postgres:16.0`, `postgres:16.6`, or
  `postgres:16.12` through `HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE`; unset,
  `postgres`, `postgres:16`, `postgres:16-alpine`, and every other value fail
  before any fixture starts. Its preflight must run before every fixture or
  Docker side effect and leave `postgres`/`postgres-s3` outside this allowlist.
  `.github/workflows/ci.yml` must define a
  `record-platform-pg16-catalog` matrix job whose explicit job name is
  `record-platform-pg16-catalog (${{ matrix.postgres_image }})`, with exactly
  those three literal image strings and no include/default fallback. Every lane
  invokes the same `pg16-catalog` entry point with its matrix value after the
  repository's `ubuntu-latest` / checkout v6 / setup-go v6 with
  `go-version-file: go.mod` bootstrap. Its child is only
  `go test -json ./internal/center/store/migrate -run
  '^TestPostgresIntegrationAppACLR2$' -count=1`; the required new test file
  provides that exact top-level anchor and may use subtests for its matrix.
- [ ] Run every real PostgreSQL 16 lane with roles created inside the fixture:

```bash
run_pg16_catalog() (
  set -euo pipefail
  image=$1
  events=$(mktemp)
  trap 'rm -f "$events"' EXIT
  HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE="$image" \
    scripts/test-record-platform-integration.sh pg16-catalog -- \
    go test -json ./internal/center/store/migrate \
    -run '^TestPostgresIntegrationAppACLR2$' -count=1 \
    | tee "$events"
  jq -se '
    def package_event:
      .Package == "houfeng/internal/center/store/migrate";
    def anchored_event:
      package_event and
      ((.Test // "") | test("^TestPostgresIntegrationAppACLR2($|/)"));
    [.[] | select(package_event)] as $package_events
    | [$package_events[] | select(anchored_event)] as $anchored_events
    | (($anchored_events | map(select(.Action == "run")) | length) > 0)
      and (($anchored_events | map(select(.Action == "pass")) | length) > 0)
      and (($package_events | map(select(.Action == "skip")) | length) == 0)
      and (($package_events | map(select(.Action == "fail")) | length) == 0)
  ' "$events" >/dev/null
)
run_pg16_catalog postgres:16.0
run_pg16_catalog postgres:16.6
run_pg16_catalog postgres:16.12
```

#### Slice 7 Admission And Required-Check Order

`app_acl_r2_postgres_integration_test.go` is the approved, narrow file-name
exception. It may retain ordinary direct-invocation `t.Skip` behavior when the
fixture environment is absent; once the strict runner has exported
`HOUFENG_POSTGRES_INTEGRATION=1`, a missing prerequisite is a test failure and
any emitted `--- SKIP:` causes the runner to fail. This does not change the
ordinary `_e2e_test.go` / optional-environment convention for any other test.

Implement the script dispatcher with the `postgres` and `postgres-s3` branches
unchanged, then parse the `pg16-catalog` image allowlist before every side
effect. Add runner coverage that proves each allowed literal is passed to all
four fixture databases, and that unset/invalid values invoke no fixture setup.
The local Docker Server reports `29.6.2`; execute the three commands above
locally as evidence in addition to CI rather than substituting a CI-only claim.
The JSON-event assertion is part of each lane: it requires nonzero matching
`run` and `pass` events and zero package `skip`/`fail` events, so a zero-match
selector cannot pass. Do not add `internal/center/platformmigrate` or
`cmd/houfeng-record-platform-admin` to that strict child command. Their
regressions are independent non-PG16 work and run through the existing `go`
job's `make verify-go` full-test gate (or an equivalently separate full-test
gate), never as catalog-lane proof.

The workflow layer must use an explicit job name so its matrix check contexts
are exactly `record-platform-pg16-catalog (postgres:16.0)`,
`record-platform-pg16-catalog (postgres:16.6)`, and
`record-platform-pg16-catalog (postgres:16.12)`. This workflow edit does not
itself make a GitHub required check. After a newly pushed Slice 7 head has all
three successful contexts, the controller may take exactly one create-only
additive action: POST the uniquely named repository ruleset
`app-acl-r2-pg16-catalog-required-v1`. It targets only `refs/heads/main`,
has no bypass actors, and adds exactly the three app-bound PG16 contexts.
Applicable rulesets aggregate with existing branch protection, so this creates
a merge gate without replacing existing checks.

Before that POST, the controller verifies repository admin permission, reads all
repository/parent rulesets with `includes_parents=true`, relevant detailed
ruleset records, `--paginate --slurp rules/branches/main?per_page=100`,
canonical full
`branches/main/protection`, and all check-run pages for the same new `$HEAD`.
Any active same-name, same-scope, or same-three-check ruleset is a duplicate or
concurrent state: return `NEEDS_CONTROLLER` without creating, updating,
deleting, disabling, or retrying anything. The paginated
`main_rules_before` flatten is the only input for effective-main
`ruleset_id`, uniqueness, and exact required-status-check decisions; false,
null, missing, or unreadable admin state and every pagination/shape/flatten
failure stop before POST.

```bash
set -euo pipefail
if ! gh api "repos/$OWNER/$REPO" |
  jq -e '.permissions.admin == true' >/dev/null; then
  echo "NEEDS_CONTROLLER: repository admin permission is required" >&2
  exit 1
fi
read_effective_main_rules() {
  gh api --paginate --slurp \
    "repos/$OWNER/$REPO/rules/branches/main?per_page=100" |
    jq -ce '
      if type == "array" and length > 0 and all(.[]; type == "array")
      then add
      else error("effective-main pages must be nonempty arrays")
      end
    '
}
main_rules_before=$(read_effective_main_rules) || {
  echo "NEEDS_CONTROLLER: effective-main pagination/shape/flatten failed" >&2
  exit 1
}
gh api --paginate --slurp \
  "repos/$OWNER/$REPO/rulesets?includes_parents=true"
gh api "repos/$OWNER/$REPO/branches/main/protection" | jq -cS .
gh api --paginate --slurp \
  "repos/$OWNER/$REPO/commits/$HEAD/check-runs?per_page=100"
```

For each literal context, require exactly one `completed`/`success` check-run
with `.head_sha == $HEAD` and a numeric `.app.id`; map that exact value to
the ruleset field `integration_id`. Missing, duplicate, unsuccessful,
wrong-head, or nonnumeric evidence is `NEEDS_CONTROLLER` before POST. Build
only the one active branch ruleset payload below. The example numeric value is a
shape placeholder, never a hard-coded integration ID.

```json
{
  "name": "app-acl-r2-pg16-catalog-required-v1",
  "target": "branch",
  "enforcement": "active",
  "bypass_actors": [],
  "conditions": {
    "ref_name": {
      "include": ["refs/heads/main"],
      "exclude": []
    }
  },
  "rules": [
    {
      "type": "required_status_checks",
      "parameters": {
        "do_not_enforce_on_create": false,
        "strict_required_status_checks_policy": true,
        "required_status_checks": [
          {"context": "record-platform-pg16-catalog (postgres:16.0)", "integration_id": 12345},
          {"context": "record-platform-pg16-catalog (postgres:16.6)", "integration_id": 12345},
          {"context": "record-platform-pg16-catalog (postgres:16.12)", "integration_id": 12345}
        ]
      }
    }
  ]
}
```

Send that body once to `POST repos/$OWNER/$REPO/rulesets`. Only an
unambiguous 201 permits a fresh `read_effective_main_rules` call that flattens
every `rules/branches/main?per_page=100` page into `main_rules_after` before
the GET of the returned ID and all applicable rulesets. Those readbacks must
prove exactly one active matching ruleset with the same ID/name, empty bypass
list, exact main ref scope, one required-status-check rule, and the three exact
`{context, integration_id}` pairs. The main active evaluation must identify
that created ruleset; failure to prove it is `NEEDS_CONTROLLER`.

```bash
main_rules_after=$(read_effective_main_rules) || {
  echo "NEEDS_CONTROLLER: post-201 effective-main pagination/shape/flatten failed" >&2
  exit 1
}
```

Finally re-GET full `branches/main/protection` and require its canonical body
to equal the pre-create body. Non-201/ambiguous/validation/duplicate/readback
failure or changed protection is `NEEDS_CONTROLLER`; never retry the POST or
update/delete/disable a ruleset. No Slice 7 path PATCHes
`required_status_checks`, serializes branch-protection `app_id`, uses ETags,
leases, or canaries, or changes the existing branch protection. The existing
`web-browser` `{context, app_id:null}` remains naturally unchanged. A rejected
branch-protection PATCH alternative would map GET any-app/null to request
`app_id:-1`, but it is not an executable fallback. This planning update does
not perform the POST or assert that Slice 7, R2, Child 1, PF-AC, or the parent
task is complete.

- [ ] Perform final spec-compliance review, then independent code-quality review.
  The prospective P1 planning review must have passed before any Slice 5
  implementation. Resolve all P0/P1/P2 findings before final verification.

- [ ] After the planning, specification, and quality gates pass, run the final
  focused/full verification gate and request parent integration review only when
  it passes:

```bash
gofmt -w db/appaclr2/migrations/*.go internal/center/store/migrate/app_acl_r2*.go internal/center/platformmigrate/app_acl_r2*.go cmd/houfeng-record-platform-admin/*.go
git diff --check
GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go
```

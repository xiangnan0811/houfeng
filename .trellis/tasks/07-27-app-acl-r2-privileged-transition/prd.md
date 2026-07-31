# APP ACL R2 Privileged Transition

## Goal

Deliver an auditable, forward-only R2 transition for an exact R1 APP ACL
deployment. It separates PostgreSQL bootstrap-superuser preparation from
direct-migrator finalization, records immutable local evidence, and lets
R2-aware runtime admit only a fully finalized R2 catalog.

## User Value

Operators gain a narrowly scoped mechanism to prove the local PostgreSQL
extension and receipt boundary before giving runtime the read access needed to
verify that proof. An incomplete or tampered transition cannot be misreported
as a healthy R1 or R2 deployment.

## Confirmed Constraints

- `0051`, frozen R1 APIs/parsers/runners, the R1 52-source snapshot, and the
  R1 204-tuple contract are immutable.
- The obsolete root `0052` draft, receipt grammar/tests, R2 compiler/source
  delta, and related config/environment/CLI routes were non-authoritative.
  Slice 1 removed them atomically; they must not be reintroduced or adopted.
- R2 uses one isolated source boundary that root generic handling and R1
  runners cannot enumerate or apply. Its immutable receipt/ledger retains the
  canonical R1/R2 application source-set bodies and digests, including the
  isolated `0052` source entry.
- R2 has exactly 53 canonical sources: frozen R1 52 plus isolated `0052`.
  R2 has exactly 206 canonical privilege tuples: frozen R1 204 plus direct
  migrator `SELECT` and runtime `SELECT` on the bootstrap-owned receipt.
- This correction leaves the receipt/manifest wire, golden vectors, L2
  three-object ACL contract, M2 SQL/ACL, 53-source vector, 206-tuple contract,
  and their hashes unchanged.
- R2 has three fixed bindings: `center_runtime`, `platform_admin`, and
  `direct_migrator`. R1 magic and parsers stay closed.
- Direct migrator owns database, APP schemas/R1 objects, `schema_migrations`,
  frozen R1 manifest/head, the new R2 manifest/head relations, domain identity,
  and the `pgcrypto` extension. Bootstrap superuser uses direct OID 10 and
  already owns the exact 36 PostgreSQL 16 `pgcrypto` member procedures plus the
  narrow receipt/helper surface. The accepted baseline is
  `pg_extension.extowner = direct_migrator` and every member `proowner = 10`;
  R2 performs no ownership transition.
- Direct M2 ownership is intentional and its PostgreSQL 16 authority is part of
  the accepted boundary. For each direct-migrator-owned M2 table, all seven
  queried native privileges (`SELECT`, `INSERT`, `UPDATE`, `DELETE`,
  `TRUNCATE`, `REFERENCES`, and `TRIGGER`) are true; the owned M2 helper is
  natively executable. Center runtime remains table-`SELECT`-only with no
  helper `EXECUTE`, and platform admin has none of those table/function
  privileges.
- Exact non-grantable direct-migrator self-`SELECT`/`EXECUTE` ACL rows remain
  mandatory `aclexplode` shape evidence even though ownership independently
  provides native authority. `has_table_privilege` and
  `has_function_privilege` prove reachability, not grant provenance: revoking
  an owner self-row leaves owner-native access true but makes the raw ACL shape
  corrupt. The fixed M2 `0x06`/`0x02` masks describe intended ordinary
  reader access only; they do not encode the owner's complete capability set.
- Role membership, `SET ROLE`, ownership transfer, catalog DML, and extension
  drop/recreate are unsupported. Receipt access is direct-migrator/runtime
  `SELECT` only, with no admin/PUBLIC access.
- Exact L2/M2 ACLs remain unchanged: no additional grants, `SECURITY DEFINER`
  reader, role membership, `SET ROLE`, ownership transfer, or superuser-based
  classification is permitted. R2 provides no `EXECUTE` grant on
  `pg_control_system()`, no `pg_monitor` membership, no helper, no second DSN,
  and no new ACL surface for that function.
- States are only R1, PREPARED, and FINALIZED. Bootstrap prepares in a
  serializable transaction; finalizer creates the new direct-migrator-owned M2
  revision/head relation pair and commits M2/head in one serializable
  transaction. `0051`'s V1-only relation/check is never altered or reused for
  M2. Only whole-transaction `40001`/`40P01` retries are allowed, and
  acknowledgement loss reclassifies exact state.
- Receipt data binds frozen R1/R2 application source hashes and ACL contracts,
  the domain `postgres_system_identifier`, database OID/name,
  bootstrap/direct/runtime/admin facts, and the local pgcrypto
  extension/member/dependency/owner/ACL baseline.
- The bootstrap-only live binding is performed only by the direct OID-10
  bootstrap session: before its initial PREPARED commit it calls
  `pg_control_system()` and binds that live identifier with the fresh database
  OID/name into the immutable receipt/domain evidence.
- Post-bootstrap finalizer and runtime use one shared constrained
  post-bootstrap continuity predicate: persisted receipt/domain and, when M2
  exists, M2-domain equality, plus fresh database OID/name, allowed
  server/version, role/membership, extension/member/dependency/owner/ACL, and
  source facts. They neither call nor claim a fresh physical system identifier.
- The R2 PG16/extension proof is only an in-transaction local catalog baseline:
  allowed PostgreSQL 16 version and exact pgcrypto extension/member/dependency/
  owner/ACL facts. It does not prove file bytes, paths or symlink resistance,
  image provenance, or package provenance; external supply-chain policy owns
  those claims. It also does not promise session drain, physical clone/restore
  detection, or cluster liveness/attestation.
- Only bootstrap reads a bootstrap DSN and only finalize reads a direct-
  migrator DSN. Only bootstrap queries `pg_control_system()`; direct migrator
  and runtime must succeed without an `EXECUTE` grant or `pg_monitor`
  membership and never query it. Wrong privilege, OID, allowed server/version,
  member, dependency, role, ACL, or domain state rejects before mutation.
- Keeping direct ownership explicitly accepts residual owner-bypass risk. A
  hostile or compromised `direct_migrator` can use its native DDL/DCL/DML
  authority to alter or drop M2 relations, functions, constraints, or triggers
  and can grant access. Exact unchanged-shape readback detects resulting drift
  when it runs, but it cannot confine a hostile owner or establish continuous
  enforcement between reads.
- The task-local `golden-vectors.md` is a fixed, pre-existing artifact consumed
  literally by tests. It is the authoritative, implementation-independent
  literal corpus for `domain_body` and `l2_acl_body`, fixing semantic inputs,
  body hex, SHA-256 digests, and malformed rejection cases; it is not generated
  by production encoders. Slice 3 receipt tests are its sole domain/L2 literal
  consumer and own those decoders, malformed cases, and nesting coverage; Slice
  2 source/manifest tests own only their source/manifest vectors.

## Scope

1. Replace the obsolete draft with an isolated R2 source, R2-only parser,
   receipt/ledger grammar, protocol-2 manifest, and state dispatcher.
2. Implement bootstrap and finalize commands with the authority, locking,
   retry, acknowledgement-loss, and fail-closed behavior in `design.md`.
3. Add new R2-only reader/admitter/startup APIs: identity-blind
   `ClassifyAppACLR2State` and `VerifyFrozenAppACLR1StateInTx(ctx, tx)` prove
   only catalog/source/ledger/ACL evidence for frozen L1/M1/body/chain/head/
   binding facts. They never query or branch on `session_user` or
   `current_user`. Identity-blindness does not grant evidence-reader authority:
   successful PostgreSQL classification requires the caller transaction to
   natively possess `SELECT` on every evidence relation that exists for the
   state. Pure predicate composition alone is identity-invariant across
   synthetic identities. Bootstrap and direct-migrator actor gates, plus
   `RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, state)` for R1 runtime,
   are the only session-identity checks. The R2 routes may classify PREPARED
   solely to reject it and admit R2 only after FINALIZED validation. Frozen
   `AdmitAppACLRuntime`, R1 readers, parsers, runners, and CLI routes remain
   R1-only and are never widened into R2 dispatchers.
4. Add unit and PostgreSQL 16 integration coverage for accepted states and
   security-relevant rejections.

### Slice 7 Admission Contract

Slice 7 has one additional, isolated PostgreSQL 16 catalog-fixture entry point:

```bash
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=<exact-image> \
  scripts/test-record-platform-integration.sh pg16-catalog -- <command> [args...]
```

`<exact-image>` is exactly one of `postgres:16.0`, `postgres:16.6`, or
`postgres:16.12`. For `pg16-catalog`, missing or any other value is an exit-2
configuration error before `mktemp`, port/password generation, Docker, or any
fixture work. Each accepted lane uses that one image for every fixture database.
The existing `postgres` and parent-owned `postgres-s3` modes retain their names,
signatures, and semantics; Slice 7 must not rename, delete, or make either mode
subject to this image allowlist.

The approved Slice 7 file
`internal/center/store/migrate/app_acl_r2_postgres_integration_test.go` remains
the sole name exception to the ordinary `_e2e_test.go` convention. A direct run
without the fixture environment may use the ordinary skip behavior, but the
strict runner exports `HOUFENG_POSTGRES_INTEGRATION=1` and treats any
`--- SKIP:` output as failure. Therefore no required Slice 7 lane can pass by
skipping. Its only required PG16 selector is the exact migrate-package anchor
`^TestPostgresIntegrationAppACLR2$`, executed with `go test -json`. The lane
must retain the JSON event stream and fail unless it shows at least one matching
`run` and `pass` event, and zero package `skip` and `fail` events. Thus a
zero-test selector cannot create a green catalog result. `platformmigrate` and
`cmd/houfeng-record-platform-admin` remain outside this catalog command; any
regression for them belongs to the independent non-PG16 `go`/`make verify-go`
full-test gate.

The workflow layer and branch-protection layer are separate acceptance gates.
The workflow must create the three stable check contexts
`record-platform-pg16-catalog (postgres:16.0)`,
`record-platform-pg16-catalog (postgres:16.6)`, and
`record-platform-pg16-catalog (postgres:16.12)` from one literal matrix and
the same strict entry point. Its fresh runner must use `ubuntu-latest`,
`actions/checkout@v6`, and `actions/setup-go@v6` with
`go-version-file: go.mod` before that command.

Only the controller, after querying the new head and observing all three exact
successful check-runs, may create the one additive repository ruleset
`app-acl-r2-pg16-catalog-required-v1`. It targets only `refs/heads/main`, is
`active`, has no bypass actors, and contains one `required_status_checks` rule
with `strict_required_status_checks_policy: true` and
`do_not_enforce_on_create: false`. Its three pairs are the literal check context
and the unique successful same-head check-run's numeric `app.id`, written as
the ruleset API's `integration_id`.

Before one `POST /repos/$OWNER/$REPO/rulesets`, the controller reads all
repository/parent rulesets with `includes_parents=true`, the effective
`rules/branches/main`, and canonical full `branches/main/protection`; it also
reads every check-run page for that exact `$HEAD`. It fails closed as
`NEEDS_CONTROLLER` if its token is not an admin, an active rule already uses
that name, target, or three-check tuple, or any target context lacks exactly one
`completed`/`success` run with a numeric `app.id`. On a non-201 response,
transport ambiguity, validation error, or possible concurrent/duplicate create,
it does not retry, delete, or update a ruleset.

After a 201, the controller freshly GETs the created ruleset and
`rules/branches/main`, verifies exactly one active matching rule and all three
`{context, integration_id}` pairs, then re-GETs full branch protection and
requires its canonical body to be unchanged. Applicable rulesets aggregate with
existing branch protection, so this is an additive merge gate. In particular,
the existing `web-browser` `{context, app_id:null}` remains untouched rather
than being serialized or rebound. No Slice 7 action PATCHes
`required_status_checks`, uses ETags, a mutation lease, a canary, or a
contexts-only replacement. A branch-protection PATCH alternative would require
the explicit any-app request mapping `app_id:-1` for a GET `app_id:null`, but it
is rejected for Slice 7 and is not an executable fallback. This planning
contract neither changes a ruleset or protection nor claims that any Slice 7
implementation, CI run, review, or parent acceptance is complete.

## Unsupported States And Non-Goals

- Partial receipt, non-exact source, mixed M1/M2, missing/changed domain
  identity, altered extension members, and extra/missing receipt grants fail
  closed without repair.
- PREPARED is never a runtime target. There is no rollback command that deletes
  evidence, rewrites receipt/extension state, or restores an old owner.
- No platform-admin receipt access, external witness, session drain, or legacy
  ownership repair. R2 admission deliberately does not detect a replacement or
  restored cluster that reproduces copied immutable evidence and matching fresh
  catalog identity; physical clone/liveness/attestation is a separate parent
  gate or future version, and R2 admission alone is insufficient.
- Moving M2 ownership away from `direct_migrator` is out of scope and would be
  a material architecture redesign. It would require role membership/`SET
  ROLE`, a privileged helper, a second creator DSN, or bootstrap precreation;
  each option violates the current single-DSN, no-helper, no-membership, and
  phase-ownership boundary. The accepted design therefore detects catalog
  drift but does not claim to confine a hostile direct owner.
- No change to frozen R1 codecs, APIs, parsers, runners, 52-source contract,
  or 204-tuple compiler.
- Planning completed before task activation. The Slice 6 local-gate delivery
  candidate is authorized on the non-main
  `codex/app-acl-r2-slice6-runtime-contract-fix` integration branch at
  `/home/murray/.codex/worktrees/166c/houfeng`. Remote push, a new Slice 6
  PR/CI run, and merge verification remain controller-owned. This does not
  change the parent task's status or authorize direct `main`/`master` work.

## Acceptance Criteria

- [ ] The superseded root implementation is removed in one replacement slice;
  no root `0052`, old receipt grammar, old R2 compiler/source, or old
  command/config route remains.
- [ ] Isolated R2 inventory has exactly 53 entries: frozen R1 52 plus one exact
  `0052`; root generic/R1 discovery and R1 runtime cannot see it.
- [ ] R2 parser accepts exactly three sorted bindings and exactly 206 sorted,
  duplicate-free tuples; it rejects 205, 207, R1 magic, unknown binding,
  noncanonical ordering, trailing bytes, and checksum substitution.
- [ ] Bootstrap accepts only a direct PostgreSQL 16 superuser OID 10 session,
  validates R1/domain before mutation, performs the bootstrap-only live binding
  through `pg_control_system()` before its initial PREPARED commit, records the
  immutable receipt/ledger, and grants only direct migrator/runtime receipt
  SELECT.
- [ ] The shared constrained post-bootstrap continuity predicate proves
  receipt/domain and, for FINALIZED, M2-domain equality plus fresh
  receipt-bound database OID/name, application-source, ACL, role,
  allowed-PG16/version, extension/member/dependency/owner, helper, and access
  facts. It does not read a fresh physical system identifier.
- [ ] Finalize accepts only direct constrained migrator identity, reads exact
  PREPARED receipt, creates an R2-specific direct-migrator-owned M2
  revision/head relation pair with one M2/one head and a read-only immutable M1
  link, and commits all DDL/DML in one serializable transaction.
- [ ] Finalize applies the fixed revoke-first DCL and proves exact
  `aclexplode` rows independently from native reachability. PostgreSQL 16 unit
  and regression coverage proves all seven queried table privileges true for
  the direct owner, only `SELECT` true for center runtime, none true for
  platform admin, and owner helper `EXECUTE` true. Removing an owner self-row
  must still classify the raw ACL as corrupt while leaving owner-native
  `has_*_privilege` true. The fixed `0x06`/`0x02` bodies, SQL, vectors, and
  hashes do not change.
- [ ] Full state classification, admission, and finalizer ACK loss accept only
  exact R1/PREPARED/FINALIZED catalog/source/ledger/ACL predicates. The sole
  bootstrap ACK-loss exception is a private observer that proves only exact R1
  or exact PREPARED. Its permitted reads are limited to frozen/L2 evidence,
  reserved-object metadata, and, only before reporting exact PREPARED ACK
  recovery success, the direct OID-10 bootstrap session's bootstrap-only live
  binding through `pg_control_system()` plus fresh database OID/name. The same
  bootstrap-only live binding is required before an ordinary PREPARED repeat
  reports success. It is not a shared post-bootstrap read, adds no grant, ACL
  surface, `pg_monitor` membership, helper, or second DSN, and permits no direct-
  migrator/finalizer or runtime physical-system reads. The observer fails on
  every unknown, partial, or M2-reserved shape
  without reading M2 relation contents, taking an M2 table lock, or classifying
  FINALIZED. Every other evidence shape rejects without mutation. Session
  identity is not a classifier predicate, but a
  PostgreSQL evidence-read error propagates rather than becoming `CORRUPT`; an
  accompanying zero-value `CORRUPT` when `ClassifyAppACLR2State` returns an
  error is not an evidence verdict.
- [ ] New R1-only transition route rejects any R2 state without parsing R2
  bytes; the frozen `AdmitAppACLRuntime` remains closed. New R2-aware runtime
  admission accepts exact R1 as R1 before upgrade, rejects PREPARED, and
  accepts only FINALIZED as R2. `ClassifyAppACLR2State` and
  `VerifyFrozenAppACLR1StateInTx` are identity-blind in the caller's
  transaction: they never inspect connection identity, while real PostgreSQL
  calls retain native evidence-relation authority. The pure predicate-
  composition matrix alone proves identity-invariant state results across
  synthetic identities. The PG16 authority matrix retains documented behavior
  for existing authorized R1 evidence readers; permits PREPARED classification
  only with native L2 `SELECT`; permits FINALIZED classification only through
  the authorized direct-migrator and center-runtime paths with
  native L2/M2 read reachability; propagates
  SQLSTATE `42501` to platform-admin and unrelated callers when a present
  evidence relation is unreadable; and never maps that error to `CORRUPT`.
  Platform admin retains no such privilege. OID-10 superuser bypass can
  technically reach M2 but is not ACL evidence; bootstrap must not invoke
  FINALIZED classification. Only bootstrap/direct-migrator actor gates and
  `RequireDirectFrozenAppACLR1RuntimeInTx` enforce session identity;
  the classifier and verifier do not inspect it. Ordinary bootstrap/finalizer
  work uses state verification plus its actor gate; uncertain bootstrap ACK
  recovery uses only the private observer after the bootstrap actor gate.
  PREPARED may call `ClassifyAppACLR2State` to identify itself, but calls neither
  `VerifyFrozenAppACLR1StateInTx` nor
  `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen
  `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
  parsing or admission.
- [ ] Slice 4 historically created and tested the reusable exact L1/M1/L2/M2/
  control-ACL relation/head predicate and typed state-composition foundation.
  Decision C's constrained-reader/shared-continuity correction remains
  prospective: before Slice 5 finalizer acceptance, the shared classifier is
  corrected and tested so its post-bootstrap continuity path never invokes the
  bootstrap-only live reader or `pg_control_system()`. Finalizer then consumes
  that one corrected shared path for preflight, readback, normal repeat, and ACK
  recovery, with no private classifier, predicate, or snapshot fork. This is
  not a Slice 5 or PostgreSQL 16 completion claim.
- [ ] A PostgreSQL 16 in-transaction catalog preflight proves the allowed
  server/version, pgcrypto extension/member/dependency/owner/ACL baseline, all
  36 exact member identities, and OID-10 member owners; a count-preserving
  substitution is rejected. File, path, image, and package provenance remain
  external supply-chain policy.
- [ ] PostgreSQL 16 tests cover wrong DSN privilege, identity, server/version,
  bootstrap OID, membership, extension member/dependency, ownership, receipt
  ACL, domain, application source, state, and M2 catalog failure without
  partial mutation. The pure predicate-composition matrix is identity-invariant
  only across synthetic identities. A separate real-reader PG16 authority
  matrix proves the exact R1, PREPARED, FINALIZED, SQLSTATE `42501`, and
  bootstrap-no-FINALIZED cases above, including that an error with a zero-value
  `CORRUPT` result is not an evidence verdict. Bootstrap ACK-recovery tests
  prove the private observer's exact R1/PREPARED proofs, error-first failure on
  every unknown/partial/M2-reserved inventory, and the absence of M2
  relation-content reads or FINALIZED classification. Together with the separate
  runtime-predicate matrix and adversarial R1-to-PREPARED race, the tests prove
  that classification may identify PREPARED but it calls neither
  `VerifyFrozenAppACLR1StateInTx` nor
  `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen
  `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
  parsing or admission.
- [x] Slice 5 resolves its five source-level quality findings with ordered
  specification and independent quality review: strict explicit finalizer
  connection source, bounded ACK-retry continuation, real constrained-wrapper
  instantiation, DDL retry/bounds matrix, and corrected owner-native
  verifier/tests. This checkbox records only current source, focused Go
  verification, and the two Slice 5 reviews; it is not PostgreSQL 16,
  Slice 6/7, R2 total-acceptance, Child 1, or PF-AC evidence.
- [ ] The fixed, task-local pre-existing `golden-vectors.md` is consumed
  literally by Slice 3 receipt tests for the domain/L2 input-to-hex-to-SHA-256
  vectors and malformed cases. `app_acl_r2_receipt_test.go` solely owns their
  decoder, malformed, nesting, and receipt tamper coverage; Slice 2
  source/manifest tests own only source/manifest bodies and vectors; PostgreSQL
  tests own live catalog equality only. Literal assertions compare documented
  bytes and digest directly, never a value derived from a production
  encoder/compiler or live database.
- [ ] Each implementation slice has a specification review followed by an
  independent code-quality review; no direct main change occurs.

### Slice 5 Evidence Note — 2026-07-31

- Review gates: specification thread `019fb5fd-f639-7fd0-b57c-3676d95ba659`
  returned `SPEC RESULT PASS` with P0/P1/P2 = 0; independent quality thread
  `019fb610-234f-7103-9d5d-5bbf045f39a9` returned `QUALITY RESULT PASS` with
  P0/P1/P2 = 0.
- Scope evidenced by those reviews: isolated `0052` M2 DDL/source digest,
  direct-finalizer command/opener, serializable finalizer/ACK path, shared
  catalog continuity, and owner-self-ACL versus native-privilege verifier
  tests. The quality gate reported focused selector, affected-package `go vet`,
  `gofmt -d`, and `git diff --check` exit code 0.
- Boundary: this note does not check any Slice 6/7, PG16 integration/image
  lane, parent/R2 total acceptance, Child 1, or PF-AC criterion. Persisted
  receipt identity is not a claim of fresh physical identity.

### Slice 6 Evidence Note — 2026-07-31

- Local delivery-candidate chain: `518b3dfe571ba9329fe4aedf51fceb0378f95249`
  is the production admission/startup implementation;
  `8ea72a30dee9f9153447af0ba0cfdbe158018151` adds cleanup/fault proof;
  `9a607d0807b53823b6f8c00867e8f199cc91ab27` proves the physical lock
  identity; `a6a2fb68bf17caee289c9569536666dfba7ecedf` proves frozen-verifier
  propagation and the public `StartAppACLR2Runtime` AST binding; and
  `cc59844fecd5ce96759983cf9255b65279b66e20` is the final code-spec
  alignment. The local candidate began from that reviewed `cc59844f` commit.
- Authoritative frozen contracts remain unchanged: R1 SQL digest
  `503d58670dc790c4b852bfb58cf93d2b816c1ce956958567dc605cb28d5cd23f`
  (52 sources / 204 tuples); R2 SQL digest
  `23f79c60dcede45a42aae82da5a9de0d3d650d7eef64dbfd7ce96c6dd5d95fff`
  (53 sources / 206 tuples); canonical 53-source digest
  `1d9dc20e71e9f319f8b1cef4b22f9dc92051a88dc9cb8a892b69494658c44dd3`.
- Review trail: initial quality review `019fb66e...` failed and was closed by
  `019fb68e...` PASS. Specification review `019fb6a4...` failed, was
  adjudicated by `019fb6b2...`, repaired in `019fb6ba...`, and the final
  combined review `019fb6c8-df27-7091-8c6b-08133de76c6d` returned PASS with
  Critical/Important/Minor = 0.
- Three mutation RED proofs all failed as intended and were precisely restored:
  advisory-lock seed `0 -> 1`; a swallowed frozen-verifier error; and a
  replaced public `StartAppACLR2Runtime` delegate.
- Fresh local PASS gates on the reviewed code baseline: the required Slice 6
  focused selector; the focused frozen-verifier-error plus public-Start AST
  selector; the dedicated R1-to-PREPARED race selector; full
  `./internal/center/store/migrate` tests; `go vet`; empty `gofmt -d`; and
  `git diff --check`. Immediately before this evidence edit, `git status` was
  clean and both Slice 6 code blobs equaled their `cc59844f` blobs.
- Remote boundary: PR #384 is Draft/Open/MERGEABLE against `main@3a7f31e`
  with remote Slice 5 head `codex/app-acl-r2-slice5-finalizer@40c7c8c`. Its
  green go/web/web-browser/docker-image checks apply only to that Slice 5 head,
  not to Slice 6. Slice 6 remote push, new CI, PR verification, and merge
  remain for the controller.
- This records no PostgreSQL 16 Slice 7 reader-authority conclusion, no
  remote/CI/merge conclusion, and no R2 microtask, Child 1, PF-AC, or parent
  completion.

## Parent Dependency

This child depends on `07-24-app-acl-migration-runtime-handoff` as the frozen
R1 baseline and on parent `07-14-vps-records-platform-foundation` for
integration approval. The parent separately owns any physical cluster
liveness/clone/restore attestation gate; an R2 admission result cannot satisfy
that gate. Parent task status remains unchanged.

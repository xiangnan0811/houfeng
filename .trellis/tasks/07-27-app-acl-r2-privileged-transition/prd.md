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
- The currently uncommitted root `0052` implementation, receipt grammar/tests,
  R2 compiler/source delta, and related config/environment/CLI routes are
  non-authoritative. Future implementation removes/replaces them atomically;
  it must not adopt them.
- R2 uses one isolated source boundary that root generic handling and R1
  runners cannot enumerate or apply. Its immutable receipt/ledger retains the
  canonical R1/R2 application source-set bodies and digests, including the
  isolated `0052` source entry.
- R2 has exactly 53 canonical sources: frozen R1 52 plus isolated `0052`.
  R2 has exactly 206 canonical privilege tuples: frozen R1 204 plus direct
  migrator `SELECT` and runtime `SELECT` on the bootstrap-owned receipt.
- R2 has three fixed bindings: `center_runtime`, `platform_admin`, and
  `direct_migrator`. R1 magic and parsers stay closed.
- Direct migrator owns database, APP schemas/R1 objects, `schema_migrations`,
  frozen R1 manifest/head, the new R2 manifest/head relations, domain identity,
  and the `pgcrypto` extension. Bootstrap superuser uses direct OID 10 and
  already owns the exact 36 PostgreSQL 16 `pgcrypto` member procedures plus the
  narrow receipt/helper surface. The accepted baseline is
  `pg_extension.extowner = direct_migrator` and every member `proowner = 10`;
  R2 performs no ownership transition.
- Role membership, `SET ROLE`, ownership transfer, catalog DML, and extension
  drop/recreate are unsupported. Receipt access is direct-migrator/runtime
  `SELECT` only, with no admin/PUBLIC access.
- States are only R1, PREPARED, and FINALIZED. Bootstrap prepares in a
  serializable transaction; finalizer creates the new direct-migrator-owned M2
  revision/head relation pair and commits M2/head in one serializable
  transaction. `0051`'s V1-only relation/check is never altered or reused for
  M2. Only whole-transaction `40001`/`40P01` retries are allowed, and
  acknowledgement loss reclassifies exact state.
- Receipt data binds frozen R1/R2 application source hashes and ACL contracts,
  domain system ID, database OID/name, bootstrap/direct/runtime/admin facts,
  and the local pgcrypto extension/member/dependency/owner/ACL baseline. It is
  compared to a fresh live catalog snapshot.
- The R2 PG16/extension proof is only an in-transaction local catalog baseline:
  allowed PostgreSQL 16 version and exact pgcrypto extension/member/dependency/
  owner/ACL facts. It does not prove file bytes, paths or symlink resistance,
  image provenance, or package provenance; external supply-chain policy owns
  those claims. It also does not promise session drain or rejection of a
  physically identical clone.
- Only bootstrap reads a bootstrap DSN and only finalize reads a direct-
  migrator DSN. Wrong privilege, OID, allowed server/version, member,
  dependency, role, ACL, or domain state rejects before mutation.
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
3. Add new R2-only reader/admitter/startup APIs: credential-neutral
   `ClassifyAppACLR2State` and `VerifyFrozenAppACLR1StateInTx(ctx, tx)` prove
   only catalog/source/ledger/ACL evidence for frozen L1/M1/body/chain/head/
   binding facts. Neither reads `session_user` or `current_user`, and a
   different session never changes its state result or produces `CORRUPT`.
   Bootstrap and direct-migrator actor gates, plus
   `RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, state)` for R1 runtime,
   are the only session-identity checks. The R2 routes may classify PREPARED
   solely to reject it and admit R2 only after FINALIZED validation. Frozen
   `AdmitAppACLRuntime`, R1 readers, parsers, runners, and CLI routes remain
   R1-only and are never widened into R2 dispatchers.
4. Add unit and PostgreSQL 16 integration coverage for accepted states and
   security-relevant rejections.

## Unsupported States And Non-Goals

- Partial receipt, non-exact source, mixed M1/M2, missing/changed domain
  identity, altered extension members, and extra/missing receipt grants fail
  closed without repair.
- PREPARED is never a runtime target. There is no rollback command that deletes
  evidence, rewrites receipt/extension state, or restores an old owner.
- No platform-admin receipt access, external witness, session drain, legacy
  ownership repair, or clone detection beyond local identity comparison.
- No change to frozen R1 codecs, APIs, parsers, runners, 52-source contract,
  or 204-tuple compiler.
- This task authorizes planning only. It remains `planning` until parent review
  and a later explicit implementation request.

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
  validates R1/domain before mutation, records the immutable receipt/ledger,
  and grants only direct migrator/runtime receipt SELECT.
- [ ] Fresh catalog comparison proves all receipt-bound application-source,
  ACL, domain, role, allowed PG16/version, extension/member/dependency/owner,
  helper, and access facts. Missing/mismatched data rejects without repair.
- [ ] Finalize accepts only direct constrained migrator identity, reads exact
  PREPARED receipt, creates an R2-specific direct-migrator-owned M2
  revision/head relation pair with one M2/one head and a read-only immutable M1
  link, and commits all DDL/DML in one serializable transaction.
- [ ] State classification and ACK loss accept only exact R1/PREPARED/FINALIZED
  catalog/source/ledger/ACL predicates. Every other evidence shape rejects
  without mutation; session identity is not a classifier input and cannot
  produce `CORRUPT`.
- [ ] New R1-only transition route rejects any R2 state without parsing R2
  bytes; the frozen `AdmitAppACLRuntime` remains closed. New R2-aware runtime
  admission accepts exact R1 as R1 before upgrade, rejects PREPARED, and
  accepts only FINALIZED as R2. `ClassifyAppACLR2State` and
  `VerifyFrozenAppACLR1StateInTx` are credential-neutral in the caller's
  transaction and prove L1/M1/body/chain/head/binding/catalog facts without
  inspecting connection identity. An identity-invariant matrix proves that the
  same evidence returns the same classified and verified state across direct
  identities. Only bootstrap/direct-migrator actor gates and
  `RequireDirectFrozenAppACLR1RuntimeInTx` enforce session identity;
  classifier/bootstrap/finalizer use only the state verifier plus their own
  actor gates. PREPARED may call `ClassifyAppACLR2State` to identify itself, but
  calls neither `VerifyFrozenAppACLR1StateInTx` nor
  `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen
  `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
  parsing or admission.
- [ ] The reusable exact L1/M1/L2/M2/control-ACL relation/head predicate is
  created and tested in Slice 4 before `ClassifyAppACLR2State` composes it;
  Slice 5 finalizer logic consumes that predicate rather than rebuilding it.
- [ ] A PostgreSQL 16 in-transaction catalog preflight proves the allowed
  server/version, pgcrypto extension/member/dependency/owner/ACL baseline, all
  36 exact member identities, and OID-10 member owners; a count-preserving
  substitution is rejected. File, path, image, and package provenance remain
  external supply-chain policy.
- [ ] PostgreSQL 16 tests cover wrong DSN privilege, identity, server/version,
  bootstrap OID, membership, extension member/dependency, ownership, receipt
  ACL, domain, application source, state, and M2 catalog failure without
  partial mutation. Identity-invariant classifier and state-verifier matrices,
  plus the separate runtime-predicate matrix and adversarial R1-to-PREPARED
  race, prove that classification may identify PREPARED but it calls neither
  `VerifyFrozenAppACLR1StateInTx` nor
  `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen
  `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
  parsing or admission.
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

## Parent Dependency

This child depends on `07-24-app-acl-migration-runtime-handoff` as the frozen
R1 baseline and on parent `07-14-vps-records-platform-foundation` for
integration approval. It has no implementation authorization yet.

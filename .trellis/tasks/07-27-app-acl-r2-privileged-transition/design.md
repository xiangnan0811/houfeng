# APP ACL R2 Privileged Transition Design

## Boundary And Routes

R2 is a forward-only local PostgreSQL catalog-proof transition. It is not a root
migration, R1 extension, ownership-repair tool, session-drain protocol, or
clone detector. R2 does not detect a replacement or restored cluster that
reproduces copied immutable evidence and matching fresh catalog identity;
physical clone/liveness/attestation is a separate parent gate or future version,
and R2 admission alone is insufficient. 0051, the root 52-source ledger, M1,
all V1 codecs/readers/runners, and frozen AdmitAppACLRuntime remain unchanged.

The sole R2 source is
db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql. It is
embedded only by db/appaclr2/migrations. Root db/migrations, generic migration
discovery, R1 runners, and R1 source parsers cannot enumerate it. Its full-file
SHA-256 plus fixed bootstrap/finalize section hashes are receipt-bound.
public.schema_migrations remains exactly the root 52-row R1 ledger and never
contains 0052.

`.trellis/tasks/07-27-app-acl-r2-privileged-transition/golden-vectors.md` is
the fixed, task-local pre-existing artifact that tests consume literally for the
receipt `domain_body` and `l2_acl_body`. It is the normative fixed-byte corpus,
supplying named valid and malformed literals with semantic input, complete body
hex, lowercase SHA-256, and an exact rejection reason. Acceptance is literal:
encoder bytes equal the documented hex, SHA-256 of those literal bytes equals
the documented digest, and parser/re-encoder retains the literal valid bytes;
negatives reject. Assertions must not derive expected bytes or digests from a
production compiler/encoder or a live database. Slice 3
`app_acl_r2_receipt_test.go` is the sole consumer of those domain/L2 literals
and owns their decoder, malformed, nesting, swap, and digest-tamper cases.
Slice 2 source/manifest tests own only source/manifest vectors; manifest tests
retain control-ACL/M2 vectors but do not consume the domain/L2 literal corpus.
PostgreSQL tests own only live catalog equality. This corpus is not an alternate
catalog contract: admission still accepts only the frozen R1 plus isolated 0052
source snapshot and the exact 206-tuple membership below.

There are four separate runtime surfaces. The names below are normative; no
compatibility wrapper may silently route a frozen R1 entry point into one of
the new R2 surfaces.

| Surface | Contract |
| --- | --- |
| Frozen R1 | `AdmitAppACLRuntime`, every R1 reader/parser, `ConvergeAppACLR1`, existing R1 runners, and generic `migrate --scope app` remain R1-only. They receive no R2 bytes, source inventory, manifest, state reader, or dispatch dependency, and this task does not modify them. |
| New R1-only transition guard | `AdmitAppACLR1OnlyRuntime` is a new R2-package guard. On one locked `REPEATABLE READ, READ ONLY` snapshot it classifies and, only for exact R1, calls `VerifyFrozenAppACLR1StateInTx` followed by `RequireDirectFrozenAppACLR1RuntimeInTx`. It rejects PREPARED/FINALIZED after classification and before V1 parsing, `VerifyFrozenAppACLR1StateInTx`, or `RequireDirectFrozenAppACLR1RuntimeInTx`; it performs no R2 payload, receipt, or manifest parsing or admission and never calls frozen `AdmitAppACLRuntime`. |
| New R2-aware API | `ClassifyAppACLR2State`, `PostgresAppACLR2StateReader`, `VerifyFrozenAppACLR1StateInTx`, `RequireDirectFrozenAppACLR1RuntimeInTx`, and `AdmitAppACLR2Runtime` are new R2-package APIs. `AdmitAppACLR2Runtime` may classify PREPARED/CORRUPT to identify it and, only for exact R1, state-verifies then performs direct runtime admission in its one locked `REPEATABLE READ, READ ONLY` snapshot. Evidence-read errors propagate without inferring a state. Once classification returns PREPARED/CORRUPT, it calls neither `VerifyFrozenAppACLR1StateInTx` nor `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest parsing or admission. It reads/parses R2 only after exact FINALIZED. |
| New startup route | `StartAppACLR2Runtime` is the separately named opt-in startup route. It invokes `AdmitAppACLR2Runtime`, reports exact R1 before upgrade, rejects PREPARED/CORRUPT, and reports R2 only after FINALIZED. It does not replace an existing R1 startup route. |

The new startup route must be deployed before bootstrap. The only new admin
CLI routes are `bootstrap --scope app-acl-r2` and `finalize --scope app-acl-r2`;
they are not aliases of generic `migrate --scope app`. No existing R1 caller
is modified to dispatch R2: frozen `AdmitAppACLRuntime`, R1 readers,
`ConvergeAppACLR1`, and generic app migration stay closed.

`ClassifyAppACLR2State` and `PostgresAppACLR2StateReader` are identity-blind
in the caller's transaction. They directly read only catalog, source, ledger,
and ACL evidence and never query or branch on `session_user` or `current_user`.
Identity-blindness does not confer reader authority: successful PostgreSQL
classification requires the caller transaction to natively possess `SELECT` on
every evidence relation that exists for that state. An evidence-read error
propagates; it is not a `CORRUPT` predicate. The direct-query API boundary is
unchanged: the reader accepts the caller's already-open transaction and does
not use a `SECURITY DEFINER` reader, membership, `SET ROLE`, extra grant,
ownership transfer, or superuser-based classification. Only the bootstrap and
direct-migrator actor gates, and the separate R1 runtime predicate below, may
inspect session identity.

`VerifyFrozenAppACLR1StateInTx(ctx, tx) (FrozenAppACLR1StateV1, error)` is
implemented only in the new R2 file
`internal/center/store/migrate/app_acl_r2_frozen_r1_verify.go`. In the
caller's already-open transaction it verifies the frozen L1/M1, source,
privilege, revision/head chain, role bindings, and required catalog facts and
returns the verified `FrozenAppACLR1StateV1`, including `CenterRuntimeRole`.
It is identity-blind: it never reads or branches on `session_user` or
`current_user`, opens no pool, starts no second transaction, and never calls
`AdmitAppACLRuntime`. That property does not expand native R1 evidence-reader
authority: a real transaction still must be able to read the evidence it
verifies, and a read error propagates rather than becoming a corrupt-state
result.

`RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, state)` is the separate
R2-owned R1 runtime-admission predicate. It alone enforces
`session_user == current_user == state.CenterRuntimeRole`. Finalizer uses full
classification/state verification plus its own actor gate and never uses this
runtime predicate. Ordinary bootstrap also never uses this predicate; its
state-decision sequence is exactly `OID-10 actor gate -> reserved-object
metadata-only inventory -> reject M2/unknown presence -> only when M2/unknown
are absent, invoke the full shared classifier`. M2 or unknown presence is a
direct fail-closed ordinary-bootstrap rejection. Bootstrap ACK recovery uses
only the separately documented private observer after its bootstrap actor
gate. PREPARED uses neither this predicate nor frozen `AdmitAppACLRuntime`. The
frozen function, its signature, and every existing caller remain unchanged.

The R2 entry routes reserve one connection, acquire the transition's
session-level shared advisory lock on that connection before their first
snapshot-taking query, then begin the read-only repeatable-read transaction.
They release the shared lock only after state verification/runtime admission and
the transaction finish. Bootstrap/finalize use the conflicting
transaction-level exclusive advisory lock. Thus no R1-to-PREPARED/FINALIZED
commit can occur between R2 classification and the R2-owned R1 state proof.

### Pure Predicate-Composition Identity-Invariant Matrix

The identity-invariant matrix invokes only the pure L1/M1/L2/M2/control-ACL
predicate composition with synthetic identity labels. The labels are not
PostgreSQL `session_user` or `current_user`, no PostgreSQL query is issued, and
the matrix is not a reader-authorization promise. For otherwise identical
evidence inputs, composition must return the same typed state for every label.

| Synthetic identity label | Exact R1 composition | Exact PREPARED composition | Exact FINALIZED composition |
| --- | --- | --- | --- |
| `state.CenterRuntimeRole` | `R1` | `PREPARED` | `FINALIZED` |
| direct migrator | `R1` | `PREPARED` | `FINALIZED` |
| bootstrap OID-10 role | `R1` | `PREPARED` | `FINALIZED` |
| platform admin | `R1` | `PREPARED` | `FINALIZED` |
| unrelated direct role | `R1` | `PREPARED` | `FINALIZED` |
| any distinct synthetic pair | `R1` | `PREPARED` | `FINALIZED` |

The distinct-pair row is a synthetic test fixture, not `SET ROLE`; the task
creates no role, membership, ownership, or credential handoff. The real
transaction-bound verifier retains documented R1 evidence-reader authority,
and the separate direct R1 runtime predicate alone checks its direct runtime
identity.

## Authority And PG16 Baseline

Before R1 or any APP credential is used, deployment provisioning must run
`docs/deploy/app-acl-r2-pre-r1-provisioning.sql` in each newly created target
database as the bootstrap superuser or `pg_control_system()` function owner.
Stock PostgreSQL 16.0, 16.6, and 16.12 expose `proacl = NULL` and PUBLIC
`EXECUTE`; provisioning resolves the zero-argument signature and revokes PUBLIC
`EXECUTE`, producing the explicit owner-OID-10-only ACL. R2 performs no REVOKE
or repair. Bootstrap and shared continuity both fail closed on any owner, ACL,
grant-option, effective-execute, or recursive-membership drift.

| Actor | Exact proof and authority | Forbidden |
| --- | --- | --- |
| Direct migrator | Direct session_user = current_user; constrained LOGIN/NOINHERIT/non-superuser; no recursive membership; owns DB, R1 objects, frozen M1 relations, new R2 relations, domain identity, and pgcrypto. Its exact effective M2 vector is table `SELECT` only plus helper `EXECUTE`; explicit owner self-ACL rows are mandatory shape evidence. Finalize uses shared constrained continuity, reads `pg_control_system()` catalog metadata, and never invokes the function or joins `pg_monitor`. | Contract-compliant finalizer code must not use `SET ROLE`, role/membership DDL, `pg_control_system()` invocation/grant, owner changes, receipt mutation, or extension drop/recreate. The catalog proof detects out-of-contract owner mutation when read; it does not technically prevent it. |
| Bootstrap superuser | PostgreSQL 16 direct login, role OID 10, rolsuper; owns receipt table and bootstrap helpers; the sole actor permitted to invoke `pg_control_system()` in the bootstrap-only live binding before initial PREPARED commit and before reporting PREPARED repeat/ACK success; may read its L2 evidence for that bootstrap work, and may use the reserved-object metadata-only inventory solely as an actor-gated M2/unknown rejection gate during ordinary bootstrap and uncertain ACK recovery | Direct-migrator DSN, reading, locking, scanning, or aggregating M2 contents, M2 predicate/manifest/control-ACL reads, FINALIZED classification, R2-provided `pg_control_system()` grant or `pg_monitor` membership, owner changes, extension drop/recreate |
| Center runtime | Direct constrained role matching center_runtime; read-only R2 admission through shared constrained post-bootstrap continuity; no direct or effective `EXECUTE` on `pg_control_system()` and no `pg_monitor` membership | DDL/DCL, receipt mutation, `SET ROLE`, `pg_control_system()` query/grant |
| Platform admin and PUBLIC | No R2 receipt/M2/helper privilege | Every R2 mutation/read/execute grant, `pg_control_system()` grant, `pg_monitor` membership |

### PostgreSQL 16 Authority-Aware Reader Matrix

This is the real PostgreSQL reader matrix, distinct from the synthetic pure
predicate-composition matrix above. It preserves the direct-query API and all
exact ACLs; it adds no grant, `SECURITY DEFINER` reader, membership, `SET ROLE`,
ownership change, or superuser-based classification path.

Two noninterchangeable predicates apply. The **bootstrap-only live binding** is
OID-10 bootstrap work only: after the external owner-only ACL preflight, it
calls `pg_control_system()` without an R2-provided `EXECUTE` grant, compares the
live system identifier with the
persisted domain/receipt, and initially binds it with the fresh database OID/name
before PREPARED commit. The **constrained post-bootstrap continuity** predicate
is the shared reader used by finalizer and runtime: it checks persisted
receipt/domain and, when M2 exists, M2-domain equality, plus fresh database
OID/name, allowed server/version, role/membership,
extension/member/dependency/owner/ACL, source facts, and the exact
`pg_control_system()` signature/owner/ACL/effective-execute evidence. It never
invokes the function or claims a fresh physical system identifier. R2 adds no
`pg_monitor` membership, `SECURITY DEFINER` helper, second DSN, role membership,
`SET ROLE`, or ACL surface for either predicate.

| Exact state / caller | Required real-reader result |
| --- | --- |
| Exact R1 | Existing authorized R1 evidence readers retain their documented authority behavior. This contract does not infer success for an identity without that native R1 evidence access. |
| PREPARED | Successful classification requires native L2 `SELECT` in the caller transaction, in addition to the applicable R1 evidence access. |
| FINALIZED | The only authorized post-bootstrap classifier callers are direct migrator and center runtime. Both have only effective table `SELECT`; direct migrator alone has helper `EXECUTE`, and platform admin has none. Exact no-grant-option direct-migrator self-`SELECT`/`EXECUTE` and center-runtime `SELECT` rows are mandatory `aclexplode` evidence. Removing an owner self-row makes the corresponding effective probe false and exact M2 rejects. OID-10 superuser bypass could technically read M2, so the bootstrap route is separately prohibited from invoking FINALIZED classification. |
| Direct migrator or center runtime post-bootstrap continuity | The shared reader succeeds from its exact evidence access while it has no direct or effective `EXECUTE` on `pg_control_system()`, no `pg_monitor` membership, and no function-call trace; catalog metadata reads are required. |
| Platform admin or unrelated role with an unreadable present evidence relation | The direct PostgreSQL read propagates SQLSTATE `42501`; the classifier must not translate permission denial into `CORRUPT`. |
| Bootstrap and FINALIZED | Ordinary bootstrap rejects M2/unknown reserved-object presence at its metadata-only gate before any full-classifier call. It therefore never invokes a path that classifies FINALIZED through superuser bypass instead of exact M2 `SELECT`. |
| OID-10 bootstrap PREPARED repeat or ACK recovery | A PREPARED success requires the bootstrap-only live binding in addition to its permitted R1/L2 evidence proof; this is not delegated to the shared constrained reader. |
| Any reader error | An accompanying zero-value `CORRUPT` result from `ClassifyAppACLR2State` is not an evidence verdict. |

Keeping M2 directly owned by `direct_migrator` is an explicit residual-risk
decision. A hostile or compromised owner can `ALTER` or `DROP` the M2 tables,
helper, constraints, or triggers, can exercise native DML, and can grant access.
The unchanged-shape readback and later classification detect a non-exact shape
when they run, but cannot confine the owner or guarantee that the shape stayed
unchanged between reads. Moving ownership elsewhere is a material redesign,
not a local hardening edit: it requires membership/`SET ROLE`, a privileged
helper, a second creator DSN, or bootstrap precreation. Each alternative breaks
the current no-membership, no-helper, single-direct-DSN, phase-separated
boundary and remains out of scope.

The accepted R1 baseline is exact and is not an R1-to-R2 ownership transition:

- Trusted PostgreSQL 16 pgcrypto was already created by direct migrator in
  `record_platform_internal`, so `pg_extension.extowner` is the direct-migrator
  OID and its `extnamespace` is the `record_platform_internal` namespace OID.
- Its 36 trusted script member functions are already bootstrap-owned by the
  PostgreSQL bootstrap superuser OID 10, are all in
  `record_platform_internal`, and have `pg_proc.proowner = 10`.
- R2 only proves this baseline. It never uses ALTER EXTENSION ... OWNER TO,
  ALTER FUNCTION ... OWNER TO, DROP EXTENSION, or CREATE EXTENSION.

The R2 extension claim is catalog-only. Bootstrap uses no credential beyond
`HOUFENG_RECORD_PLATFORM_APP_BOOTSTRAP_DATABASE_URL`: the opened bootstrap DSN
session must be a direct `session_user = current_user` OID-10 superuser session
and must not use `SET ROLE`. In its locked transaction, the bootstrap-only live
binding alone calls `pg_control_system()` before the initial PREPARED commit and
binds its live identifier with the fresh database OID/name. It also proves the
allowed PostgreSQL 16 server/version and exact `pgcrypto` extension, member,
dependency, owner, and ACL baseline:

| Allowed PostgreSQL `server_version_num` | Required `pgcrypto` catalog version |
| --- | --- |
| `160000` | `1.3` |
| `160006` | `1.3` |
| `160012` | `1.3` |

Bootstrap reads the required local catalog facts (`pg_extension`, `pg_depend`,
`pg_proc`, `pg_namespace`, `pg_roles`, relation/ACL catalogs, and
`pg_default_acl`) before R2 DDL. It rejects a missing/wrong allowed server
version, extension version/schema, extension/member dependency, member
identity/owner, ACL baseline, live-system/domain mismatch, or database OID/name
mismatch. It records those catalog facts, the bootstrap-only live-bound domain,
and the application R1/R2 source hashes already defined by this task.

This local catalog proof does not prove file bytes, filesystem paths, symlink
resistance, image artifacts, or package provenance. It performs no server-file
read and has no bootstrap directory configuration. External supply-chain policy
owns all of those non-catalog claims. Finalize and runtime compare the receipt
with constrained post-bootstrap continuity only: persisted receipt/domain and,
when applicable, M2-domain equality plus fresh database OID/name, allowed server
version, extension name/version/schema/OID/owner, the exact 36
`record_platform_internal` member identities/OIDs/owners/dependencies, role and
membership facts, required ACLs, and source facts. They do not reread
`postgres_system_identifier` through `pg_control_system()`. A different baseline
is a hard rejection, never a repair. No helper with an unsafe `EXECUTE` grant
exists.

### Normative PG16 pgcrypto Contract

For each allowed PostgreSQL 16 `server_version_num`, the required local catalog
baseline is `pgcrypto` `extversion = 1.3` in
`record_platform_internal`, owned by direct migrator, with the exact member
dependency/identity/owner/ACL facts below. This is intentionally a catalog
definition, not a claim about an extension file, container artifact, or package.

The sole accepted sorted `identity_set_sha256` is
57e7ac6a986705d8fa1e5b2260c1836b74dffe1b33bee00d65d1b275284e8196 over
raw-byte-sorted UTF-8 lines
`record_platform_internal.<proname>|pg_get_function_identity_arguments(oid)`
followed by LF. This is PostgreSQL's exact emitted catalog-signature text, not
an attempted reconstruction of callable input arity: every `OUT` mode, name,
and type emitted by the formatter is bound byte-for-byte. The production member
reader and its static query test may use only
`pg_catalog.pg_get_function_identity_arguments(procedure.oid)`.
`pg_catalog.pg_get_function_arguments(procedure.oid)` is rejected even if its
current PostgreSQL 16 output happens to match: it includes default-argument
semantics and is not this contract's specified formatter. The reader must also
reject `oidvectortypes(procedure.proargtypes)` and any pgcrypto-specific
stripping of emitted `OUT` terms. Unsorted enumeration digests are not receipt
values and never satisfy an acceptance rule. This list, not its cardinality, is
normative:

    record_platform_internal.armor|bytea
    record_platform_internal.armor|bytea, text[], text[]
    record_platform_internal.crypt|text, text
    record_platform_internal.dearmor|text
    record_platform_internal.decrypt|bytea, bytea, text
    record_platform_internal.decrypt_iv|bytea, bytea, bytea, text
    record_platform_internal.digest|bytea, text
    record_platform_internal.digest|text, text
    record_platform_internal.encrypt|bytea, bytea, text
    record_platform_internal.encrypt_iv|bytea, bytea, bytea, text
    record_platform_internal.gen_random_bytes|integer
    record_platform_internal.gen_random_uuid|
    record_platform_internal.gen_salt|text
    record_platform_internal.gen_salt|text, integer
    record_platform_internal.hmac|bytea, bytea, text
    record_platform_internal.hmac|text, text, text
    record_platform_internal.pgp_armor_headers|text, OUT key text, OUT value text
    record_platform_internal.pgp_key_id|bytea
    record_platform_internal.pgp_pub_decrypt|bytea, bytea
    record_platform_internal.pgp_pub_decrypt|bytea, bytea, text
    record_platform_internal.pgp_pub_decrypt|bytea, bytea, text, text
    record_platform_internal.pgp_pub_decrypt_bytea|bytea, bytea
    record_platform_internal.pgp_pub_decrypt_bytea|bytea, bytea, text
    record_platform_internal.pgp_pub_decrypt_bytea|bytea, bytea, text, text
    record_platform_internal.pgp_pub_encrypt|text, bytea
    record_platform_internal.pgp_pub_encrypt|text, bytea, text
    record_platform_internal.pgp_pub_encrypt_bytea|bytea, bytea
    record_platform_internal.pgp_pub_encrypt_bytea|bytea, bytea, text
    record_platform_internal.pgp_sym_decrypt|bytea, text
    record_platform_internal.pgp_sym_decrypt|bytea, text, text
    record_platform_internal.pgp_sym_decrypt_bytea|bytea, text
    record_platform_internal.pgp_sym_decrypt_bytea|bytea, text, text
    record_platform_internal.pgp_sym_encrypt|text, text
    record_platform_internal.pgp_sym_encrypt|text, text, text
    record_platform_internal.pgp_sym_encrypt_bytea|bytea, text
    record_platform_internal.pgp_sym_encrypt_bytea|bytea, text, text

All three accepted images, `postgres:16.0`, `postgres:16.6`, and
`postgres:16.12`, report pgcrypto `1.3`, the same 36 member rows, and the same
digest above. For the zero-based member index 16,
`pgp_armor_headers` has `proargtypes = text`, `pronargs = 1`,
`proallargtypes = {25,25,25}`, and `proargmodes = {i,o,o}`, while
`proargnames = {"",key,value}` and
`pg_get_function_identity_arguments(oid)` emits exactly
`text, OUT key text, OUT value text`. `proargtypes` is evidence of callable
input arity, but it is not the stored member-signature string.

The rejected input-only alternative replaces that row with `text`. It is
rejected because it conflicts with the production reader and drops the record
result shape. The static catalog-reader test must assert the exact
`pg_get_function_identity_arguments(procedure.oid)` call and reject a
`pg_get_function_arguments(procedure.oid)` replacement, an
`oidvectortypes(procedure.proargtypes)` replacement, or any pgcrypto-specific
`OUT` stripping. A version-specific allowlist is also rejected: all three
allowed PostgreSQL 16 images have the same exact row, so multiple accepted
digests would weaken the immutable receipt without adding compatibility.

Bootstrap persists the allowed `server_version_num`, `server_version`,
`extversion = 1.3`, extension/schema/OID/owner/dependency facts,
identity-set SHA-256, `member_count = 36`, and exact local ACL facts in the
receipt. In the bootstrap-only live binding it also binds the live
`postgres_system_identifier` and fresh database OID/name into the immutable
domain/receipt before PREPARED commit. It persists the R1/R2 application source
and section hashes already defined by this task, but no server-file, directory,
path, image, package, or server-file-hash evidence. Finalize/runtime use shared
constrained post-bootstrap continuity: persisted receipt/domain and, when
present, M2-domain equality plus fresh database OID/name, allowed server/version
and catalog facts, including `record_platform_internal`, identity set, extension
owner/dependency, and every member identity/OID/owner. They do not reread a
physical system identifier. A 36-row set with a substituted identity or identity
argument is rejected; equal cardinality is never evidence of equivalence.

### Slice 7 PG16 Catalog Fixture And Required-Check Adjudication

The PostgreSQL 16 catalog matrix is an all-lane requirement, not a generic
smoke test. The selected contract is an additional strict runner mode, rather
than a reinterpretation of the parent record-platform modes:

```bash
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=<exact-image> \
  scripts/test-record-platform-integration.sh pg16-catalog -- <command> [args...]
```

`pg16-catalog` accepts only `postgres:16.0`, `postgres:16.6`, and
`postgres:16.12`. The script validates its positional mode, `--`, child argv,
and this environment value before `mktemp`, random-password generation, port
selection, Docker invocation, container creation, or any fixture URL export.
Missing input, `postgres`, `postgres:16`, `postgres:16-alpine`, or every other
value exits 2 with no Docker or fixture side effect. An accepted value is used
for every APP/ledger/witness/recovery PostgreSQL fixture in that run. Each lane
proves only its live allowed server version, `extversion`, the exact 36-member
inventory/identity-set digest, direct-migrator extension owner/dependency,
OID-10 member owners, and ACL baseline. The fixed image names make catalog
coverage reproducible; they do not prove image-artifact or package provenance.

`postgres` remains the existing general PostgreSQL fixture interface and
`postgres-s3` remains the parent-owned S3-profile interface. Neither may be
renamed, removed, constrained by the PG16 image allowlist, or otherwise changed
as an incidental effect of implementing `pg16-catalog`; their future parent
calls continue to use `scripts/test-record-platform-integration.sh <mode> --
<command> [args...]`. The strict dispatcher must preserve those branches before
adding the new branch.

Two alternatives were rejected. Replacing `postgres` with the three-image
allowlist would invalidate the parent Task 10/14/15/16/17/18 calls and make the
general/S3 fixture contracts incompatible. A second standalone Slice 7 runner
would duplicate lifecycle, cleanup, skip detection, and port/identity behavior,
and would violate the Slice 7 three-file ownership boundary. The distinct
`pg16-catalog` mode is the smallest compatible option: it makes the PG16 input
strict without taking authority over the parent modes.

The workflow defines evidence; branch protection is an external governance
action. The Slice 7 implementation must create this one matrix job with no
`include`, no default image value, and no fallback command:

```yaml
record-platform-pg16-catalog:
  name: record-platform-pg16-catalog (${{ matrix.postgres_image }})
  runs-on: ubuntu-latest
  strategy:
    fail-fast: false
    matrix:
      postgres_image:
        - "postgres:16.0"
        - "postgres:16.6"
        - "postgres:16.12"
  env:
    HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE: ${{ matrix.postgres_image }}
  steps:
    - uses: actions/checkout@v6
    - uses: actions/setup-go@v6
      with:
        go-version-file: go.mod
    - name: Run strict PG16 catalog evidence
      shell: bash
      run: |
        set -euo pipefail
        events=$(mktemp)
        trap 'rm -f "$events"' EXIT
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
```

The new test file must expose `TestPostgresIntegrationAppACLR2` as the sole
top-level PG16 anchor; its subtests may cover the authority matrix. The runner
already fails on nonzero child exit and `--- SKIP:`; the `go test -json` proof
adds a machine-checkable nonzero `run`/`pass`, zero `skip`, and zero `fail`
requirement so an unmatched `-run` selector cannot pass. The command names only
`./internal/center/store/migrate`; neither `./internal/center/platformmigrate`
nor `./cmd/houfeng-record-platform-admin` may be folded into a PG16 catalog
result. Their regressions belong to the independent existing `go` job's
`make verify-go` full-test gate and must not be described as catalog evidence.

The explicit job name makes the actual expected check contexts stable and
queryable, rather than relying on GitHub's implicit matrix formatting:

```text
record-platform-pg16-catalog (postgres:16.0)
record-platform-pg16-catalog (postgres:16.6)
record-platform-pg16-catalog (postgres:16.12)
```

After a new Slice 7 head is pushed, the controller uses that exact `$HEAD`, not
a previous PR head. The selected governance path is one create-only, additive
repository ruleset named `app-acl-r2-pg16-catalog-required-v1`. Applicable
repository rulesets and branch protection aggregate, so it adds the three PG16
merge gates without replacing an existing required check.

The rejected alternative was patching existing branch protection. That endpoint
has no supported conditional-write CAS, so an external writer cannot prove that
it will not overwrite a concurrent required check. The independent ruleset
avoids that shared-list overwrite structurally and is therefore the sole Slice 7
path.

Before the one create request, the controller must make a read-only preflight:
the token has repository admin permission; every repository/parent ruleset is
read with `includes_parents=true`; relevant detailed ruleset records and the
`--paginate --slurp rules/branches/main?per_page=100` response are read; and
the full `branches/main/protection`
response is captured canonically. An active ruleset with the chosen name, the
same `refs/heads/main` scope, or the same three context/integration pairs is
a duplicate or concurrent state and is `NEEDS_CONTROLLER`. The controller
does not create, update, delete, disable, or otherwise alter it; the disabled
`protect_main` ruleset also remains untouched. The paginated
`main_rules_before` flatten is the only input for effective-main
`ruleset_id`, uniqueness, and exact required-status-check decisions; a false,
null, missing, or unreadable admin value, or any page/shape/flatten failure,
stops before POST.

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

The controller derives all three `integration_id` values only from those
unique successful same-head check-runs. It must not hard-code an app ID, infer an
app from a context name, or use evidence from another head. The create body has
one active branch-scoped rule, no bypass actors, and exactly these three literal
required checks:

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

The numeric values in the example are shape placeholders: each is the exact
numeric `.app.id` proved for its matching context on the new `$HEAD`. The
controller sends this body once with `POST repos/$OWNER/$REPO/rulesets`. Only an
unambiguous 201 permits readback. It then calls
`read_effective_main_rules` again, flattening every
`rules/branches/main?per_page=100` page into `main_rules_after` before it
GETs the returned ruleset by ID and all applicable rulesets. Those readbacks
must prove exactly one active matching ruleset with the same ID/name, empty
bypass list, only
`refs/heads/main`, exactly one required-status-check rule, and the three
literal `{context, integration_id}` pairs. If the active evaluation cannot
identify the created ruleset, return `NEEDS_CONTROLLER`.

```bash
main_rules_after=$(read_effective_main_rules) || {
  echo "NEEDS_CONTROLLER: post-201 effective-main pagination/shape/flatten failed" >&2
  exit 1
}
```

The controller then freshly GETs full `branches/main/protection` and requires
its canonical body to equal the pre-create body. A non-201 response,
transport ambiguity, validation error, duplicate/concurrent state, malformed
readback, or changed protection is `NEEDS_CONTROLLER`; it must not retry,
delete, update, disable, or otherwise mutate a ruleset. No Slice 7 active path
PATCHes `required_status_checks`, serializes `app_id`, uses a lease, ETag, or
canary, or rebuilds `contexts`. The existing `web-browser`
`{context, app_id:null}` remains exact because branch protection is never
touched. The rejected branch-protection PATCH alternative would use
`app_id:-1` for a GET any-app/null binding, but it is not a Slice 7 fallback.
This task does not perform the POST or claim that the contexts are currently
required. The local Docker Server remains required local evidence and is not
replaced by CI-only evidence.

## M2 Persistence: Separate R2 Relations

0051 has a fixed V1 manifest digest CHECK, field order, and foreign-key chain.
M2 must not be inserted into, alter, re-own, or advance
public.app_acl_manifest_revisions or public.app_acl_manifest_head.

Bootstrap creates only the bootstrap-owned receipt surface:

- public.app_acl_r2_bootstrap_receipt, exactly one immutable singleton row;
- record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation() and
  record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea,
  bytea), exactly those bootstrap-owned helper identities; and
  `public.app_acl_r2_bootstrap_receipt.app_acl_r2_bootstrap_receipt_immutable`,
  exactly that receipt trigger identity, whose target table and trigger
  function are both bootstrap-owned (a PostgreSQL trigger has no independent
  ACL or owner); and
- explicit receipt SELECT only for direct migrator and center runtime. Admin
  and PUBLIC have none, and direct/runtime/admin/PUBLIC have no helper EXECUTE.

Before the receipt row is inserted or PREPARED is accepted, bootstrap executes
these ACL operations in its serializable transaction, in this order: `REVOKE
ALL PRIVILEGES ON TABLE public.app_acl_r2_bootstrap_receipt FROM PUBLIC,
direct_migrator, center_runtime, platform_admin`; `GRANT SELECT` on that table
only to `direct_migrator` and `center_runtime`; and `REVOKE ALL PRIVILEGES ON
FUNCTION` for both named bootstrap helpers from `PUBLIC`, `direct_migrator`,
`center_runtime`, and `platform_admin`. It then reads catalog ACLs and proves:
the receipt table has exactly the two explicit no-grant-option SELECT entries;
direct migrator and center runtime have effective SELECT but no other table
privilege; platform admin has no effective table privilege; no PUBLIC ACL item
exists; and all four non-owner roles have no effective helper EXECUTE. Owner
authority for bootstrap is inherent, not an ACL exception. Before creation and
again before commit, `pg_default_acl` must have no row for OID 10 that applies
globally or to `public` for tables or globally or to
`record_platform_internal` for functions. Any default-ACL row, direct grant,
inherited effective access, grant option, or extra privilege is corrupt.

PREPARED has no R2 manifest relation, head, manifest row, or manifest trigger.
Only a successful direct-migrator finalizer transaction creates these new,
direct-migrator-owned relations; bootstrap cannot create, pre-create, or seed
any part of them:

    public.app_acl_r2_manifest_revisions
      at FINALIZED exactly one row; primary key
        (protocol_version = 2, manifest_revision = 2)
      immutable M1 link columns: m1_revision = 1, m1_manifest_digest,
        m1_source_set_digest, m1_privilege_set_digest,
        m1_migrator_catalog_role
      FK (m1_revision, m1_manifest_digest) -> frozen M1 revision/digest
      R2 fields: direct migrator name/OID, receipt digest, R2 source-set
        body/digest, three-binding/206-tuple privilege body/digest, domain
        body/digest, control-ACL body/digest, M2 digest, recorded_at; CHECKs
        recompute every digest.

    public.app_acl_r2_manifest_head
      at FINALIZED exactly one singleton = true row and no false/extra row
      non-null pointer (protocol_version = 2, manifest_revision = 2, M2 digest)
      FK to the R2 revision row.

Both relations have the direct-migrator-owned
`record_platform_internal.app_acl_r2_reject_manifest_mutation()` trigger
function and exactly these trigger identities:
`public.app_acl_r2_manifest_revisions.app_acl_r2_manifest_revisions_immutable`
and `public.app_acl_r2_manifest_head.app_acl_r2_manifest_head_immutable`.
They reject update/delete/truncate. Finalize uses plain CREATE TABLE, never IF
NOT EXISTS. A pre-existing one-sided, empty, extra, wrong-owner, or wrong-head
relation is corrupt and not repairable. In one serializable transaction it
follows one mandatory order: (1) DDL creates both relations/triggers; (2) M2
revision/head writes insert the one M2 row and one M2 head and complete the
phase-two M1 revision/digest CAS; (3) revoke-first ACL normalization applies
the exact ACLs; and (4) FINALIZED readback re-reads the result before commit.
PostgreSQL DDL is
transactional, so any error rolls back all R2 DDL/DML and leaves exact PREPARED.
There is no committed rollback: a successful FINALIZED state always contains
the one revision and one head, both linked to frozen M1 by the immutable fields
above. No M2 data is ever inserted into the frozen 0051 V1 relations.

After the M2 revision/head writes and phase-two M1 revision/digest CAS, but
before FINALIZED readback, finalize runs the same revoke-first proof for its
exact surfaces. It revokes all table
privileges on both M2 relations from `PUBLIC`, bootstrap, `direct_migrator`,
`center_runtime`, and `platform_admin`, then grants no-grant-option `SELECT`
to both `direct_migrator` and `center_runtime`. It revokes all function
privileges on `record_platform_internal.app_acl_r2_reject_manifest_mutation()`
from those same five grantees, then grants no-grant-option `EXECUTE` only to
`direct_migrator`. The grants use no `WITH GRANT OPTION`. PostgreSQL ownership
is verified independently through the relation/function owner OID. Raw
`aclexplode` rows and effective `has_*_privilege` probes are both exact and
neither substitutes for the other.

The post-DDL catalog proof requires, for each M2 table, exactly two explicit
no-grant-option `SELECT` ACL entries: the direct-migrator owner self-grant and
the center-runtime grant. It requires exactly one explicit no-grant-option
`EXECUTE` ACL entry on the immutable helper, the direct-migrator owner
self-grant. `aclexplode` checks include the owner entries rather than filtering
them out. The effective table probes require only `SELECT` for direct migrator
and center runtime; `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `REFERENCES`, and
`TRIGGER` are false for both, and every probe is false for platform admin. The
helper probe requires direct-migrator `EXECUTE` and rejects
runtime/admin reachability. Bootstrap's superuser bypass and PUBLIC are not
ordinary-reader grant provenance and are excluded from these native probes;
their raw ACL entries must still be absent. There is no PUBLIC ACL item and no
extra or grant-option ACL item. It also rejects any
`pg_default_acl` row for the direct-migrator OID applying globally or to
`public` tables or globally or to `record_platform_internal` functions. Those
default-ACL absence assertions prevent ambient access; they never replace the
required explicit direct-migrator self-ACL entries. The receipt L2 SELECT
exceptions remain only direct migrator and center runtime; M2 control access is
represented separately and never inflates the 206 application tuples.

Direct migrator owns both R2 relations and has an explicit no-grant-option
self-`SELECT` on both; center runtime has the corresponding explicit
no-grant-option `SELECT`; and the direct-migrator-owned immutable helper has
only the explicit direct-migrator self-`EXECUTE`. Bootstrap, platform admin,
and PUBLIC have no explicit M2 table/function ACL row; bootstrap superuser
bypass is intentionally outside the ordinary-reader contract. Owner OID,
owner self-ACL, and effective `has_*_privilege` result are checked separately.
The direct owner must report table `SELECT` only plus helper `EXECUTE`;
removing its self-row makes the corresponding raw and effective results false,
and exact M2 fails. These facts are bound by the M2 control ACL and fresh
catalog comparison. This
transition-control ACL is deliberately outside the application privilege
grammar. The application body remains exactly 206: frozen R1's 204 semantic
tuples plus only the two receipt SELECT exceptions:

    direct_migrator | table | public | app_acl_r2_bootstrap_receipt | "" | SELECT | false
    center_runtime  | table | public | app_acl_r2_bootstrap_receipt | "" | SELECT | false

## Canonical Contracts

All R2 encoders and parsers live beside the new R2 implementation; no V1 magic,
parser, reader, or byte layout changes. `u8`, `u16be`, `u32be`, and `u64be`
mean unsigned fixed-width big-endian values. `digest` is exactly 32 raw
SHA-256 bytes. `str16` is `u16be(UTF-8 byte length) || UTF-8 bytes`, with a
maximum declared below; `body32` is `u32be(byte length) || raw bytes` and has
a 4 MiB maximum. All nonempty text is valid NFC UTF-8, contains no NUL or C0/
C1 control byte, and is compared as raw UTF-8 bytes. A PostgreSQL role name is
the lower-case unquoted identifier `[a-z_][a-z0-9_]{0,62}`. A bare catalog name
uses the same rule. A function identity is the exact server spelling
`<schema>.<proname>(<pg_get_function_identity_arguments>)`; when that server
formatter emits `OUT` terms, those exact bytes are retained rather than reduced
to `proargtypes`. pgcrypto members specifically use
`record_platform_internal`, and identities are not whitespace-normalized. Every
decoder rejects an invalid magic/version, short
field, oversized field/body, invalid text, unknown enum, noncanonical order,
duplicate, inconsistent nested digest, trailing byte, or an input whose
re-encoding differs byte-for-byte.

The R2 application subject map is fixed and ordered: `1 = center_runtime`,
`2 = direct_migrator`, `3 = platform_admin`. The R2 control-role map is fixed
and ordered: `1 = bootstrap_superuser`, `2 = direct_migrator`,
`3 = center_runtime`, `4 = platform_admin`, `5 = PUBLIC`. Subject bindings
must use tags 1, 2, 3 in that order and have three distinct valid role names.
The three non-bootstrap control names must equal their subject bindings. The
object-class map is `1 = database`, `2 = schema`, `3 = table`, `4 = view`,
`5 = sequence`, `6 = function`, `7 = column`; class 7 is rejected. The
privilege map is `1 = CONNECT`, `2 = USAGE`, `3 = SELECT`, `4 = INSERT`,
`5 = UPDATE`, `6 = DELETE`, `7 = EXECUTE`. A boolean is one byte and is only
`0` or `1`.

All literal magics below are the unquoted ASCII bytes shown, with no NUL,
length prefix, or terminator. There is no field padding, optional field, or
alternate byte order. An encoder validates its complete value before emitting
its first byte; a decoder consumes the declared fields in order, requires EOF
immediately after the last field, and re-encodes the parsed value to require
byte-for-byte equality with its input. A `body32` nested value is checked for
its own exact magic, version, counts, EOF, and digest before its containing
value is accepted. No parser trims, case-folds, decomposes/recomposes after
validation, or otherwise normalizes a value on decode: input text must already
be canonical NFC. `PUBLIC` is only control-role tag 5 (catalog ACL grantee
OID 0); it is never a subject binding, owner, or receipt `role` record.

Every repeated record has the byte-sort key stated with that body. The decoder
requires strict increase of that full key, so a repeated key, an equal logical
record with a different serialization, or a record inserted out of order is a
rejection. A singleton body has no repeatable record; its fixed scalar key is
stated instead, and duplicate persisted rows or heads are rejected by the SQL
reader. In all cases, malformed length prefixes, truncation, unrecognized
tags, disallowed zero values, unexpected record count, duplicate, bad nested
digest, noncanonical text, trailing bytes, or a failed catalog equality proof
is a hard rejection with no repair, partial value, or fallback parser.

### R2 Source-Set Body

`AppACLSourceSetR2V1` is an isolated R2 value. It contains no role field,
role tag, or privilege record; adding one is malformed. Its complete byte
preimage is:

```text
"HOUFENG-APP-ACL-R2-SOURCE-SET-V1" || u16be(1) || u16be(53) || entry[53]

entry = str16(filename, 1..255) || digest(full_file_sha256)
```

`filename` is ASCII, matches `[0-9]{4}_[a-z0-9_]+[.]sql`, has no path
separator, and entries are strict raw-byte filename order. The exact set is
the frozen 52 filename/full-file-SHA-256 pairs plus exactly
`0052_app_acl_r2_privileged_transition.sql`; duplicate filenames, a substituted
checksum, missing entry, or any other 53-entry set rejects. The source-set
digest is `SHA-256` over every byte shown above, including literal magic,
version, count, all `str16` prefixes, and all 32-byte entry digests. The only
record key is the raw UTF-8 byte sequence of `filename`; no path cleaning,
case conversion, or filename normalization is permitted. A parser rejects
anything other than exactly 53 valid entries in this order, any duplicate
filename, a digest that does not match the fixed R1+0052 source snapshot, or
any byte after entry 53.

### R2 Three-Binding/206-Tuple Privilege Body

`AppACLPrivilegeSetR2V1` is the only new application privilege value. Its
complete byte preimage is:

```text
"HOUFENG-APP-ACL-R2-PRIVILEGE-SET-V1" || u16be(1) || u16be(3)
|| binding[3] || u16be(206) || tuple[206]

binding = u8(subject_tag) || str16(catalog_role, 1..63)
tuple = u8(subject_tag) || u8(object_class_tag)
      || str16(schema_name, 0..63)
      || str16(object_identity, 1..1024)
      || str16(column_name, 0..63)
      || u8(privilege_tag) || u8(grant_option)
```

Bindings are exactly the three consecutive records `(1, center_runtime)`,
`(2, direct_migrator)`, and `(3, platform_admin)`, where each name is the
validated catalog role name and tags/names are unique. The names must also
equal receipt control roles 3, 2, and 4 respectively; a different role map,
missing binding, fourth binding, reordered binding, or duplicate tag/name
rejects. Tuples are strict lexicographic order by the complete key
`(subject_tag, object_class_tag, raw schema bytes, raw object-identity bytes,
raw column bytes, privilege_tag, grant_option)`; no equal tuple is allowed.
`grant_option` is always `0`. The permitted shapes are: database (`schema =
column = ""`, bare identity, CONNECT); schema (`schema = column = ""`,
identity `public`, USAGE); table (schema `public`, empty column, bare identity,
SELECT/INSERT/UPDATE/DELETE); view (same shape, SELECT); sequence (same shape,
USAGE/SELECT); and function (`schema = column = ""`, exact server function
identity, EXECUTE). A function identity is the exact server spelling
`<schema>.<proname>(<pg_get_function_identity_arguments>)`, including any
`OUT` text PostgreSQL emits; pgcrypto members specifically use
`record_platform_internal`; it is not
whitespace-normalized. Object-class tag 7, every unlisted shape, and every
unknown subject/class/privilege tag reject. The exact tuple membership is the
frozen R1 204 semantic tuples mapped to subject tags 1/3 plus only these two
receipt tuples, shown in their required byte-sort order:

```text
1 | 3 | public | app_acl_r2_bootstrap_receipt | "" | SELECT | 0
2 | 3 | public | app_acl_r2_bootstrap_receipt | "" | SELECT | 0
```

The body digest is `SHA-256` over the entire body, including magic, version,
both counts, every role-name length prefix, every tuple field prefix, and all
tuple bytes. Counts 205/207, R1 magic, unknown roles/classes/privileges,
nonzero grant option, reordered or duplicate records, a role-map mismatch, any
tuple outside that exact membership, or any trailing byte reject.

### Nested Domain And ACL Bodies

The R2 domain body used by receipt and M2 is
`"HOUFENG-APP-ACL-R2-DOMAIN-V1" || u16be(1) || str16(domain_id,1..128) ||
str16("application") || u64be(identity_epoch=1) || str16("postgres_system")
|| str16(postgres_system_identifier,1..128) || u32be(database_oid) ||
str16(database_name,1..63)`. Its digest is SHA-256 of that complete body and
it must equal the one immutable domain row. The OID-10 bootstrap-only live
binding calls `pg_control_system()` and binds the live identifier before the
initial PREPARED commit. Thereafter constrained post-bootstrap continuity checks
the persisted receipt/domain and, when present, M2-domain equality plus fresh
database OID/name; finalizer and runtime never reread or claim a fresh physical
system identifier.

Both ACL bodies use the same record grammar. The L2 body magic is
`HOUFENG-APP-ACL-R2-L2-ACL-V1`; the M2 `control_acl_body` magic is
`HOUFENG-APP-ACL-R2-CONTROL-ACL-V1`.

```text
magic || u16be(1) || u16be(object_count) || object[object_count]
|| u16be(trigger_count) || trigger[trigger_count]
|| u16be(default_acl_assertion_count) || default_acl_assertion[count]

object = u8(kind: 1=table, 2=function) || str16(schema,1..63)
       || str16(identity,1..1024) || u8(owner_control_role)
       || u32be(owner_oid) || u16be(explicit_grant_count) || grant[count]
       || u8(effective_relevant_privilege_mask)
grant = u8(grantee_control_role) || u8(privilege: 1=SELECT, 2=EXECUTE)
      || u8(grant_option=0)
trigger = str16(table_schema,1..63) || str16(table_name,1..63)
        || str16(trigger_name,1..63) || str16(function_schema,1..63)
        || str16(function_identity,1..1024) || u32be(table_owner_oid)
        || u32be(function_owner_oid) || u8(enabled=1)
default_acl_assertion = u8(owner_control_role) || u8(kind: 1=table,2=function)
                      || u8(namespace: 1=public,2=record_platform_internal)
```

Objects are ordered by kind, schema bytes, identity bytes; grants are ordered
by grantee then privilege; triggers are ordered by target table then trigger;
default-ACL assertions are ordered by owner, kind, namespace. The effective
mask bit `tag - 1` records intended ordinary-reader access for the object's one
relevant read/use operation: table `SELECT` or function `EXECUTE`. It is a fixed
policy projection, not a complete `has_*_privilege` result set and not grant
provenance. It omits DDL/DCL authority and any bootstrap-superuser bypass; the
separate effective table vector still probes and rejects all six non-SELECT
privileges. The
`owner_control_role` and `owner_oid` fields independently prove ownership. For
M2, every set direct-migrator bit is paired with a mandatory explicit
no-grant-option owner self-ACL record and the corresponding effective probe.
Every listed default-ACL assertion means that no matching global or named-schema
`pg_default_acl` row exists; its appearance in the body proves absence, not a
grant or a substitute for that owner self-ACL.

L2 has exactly three objects: the bootstrap-owned receipt table with two
explicit SELECT grants to tags 2/3 and effective SELECT mask tags 1/2/3; the
two bootstrap-owned functions
`app_acl_r2_reject_bootstrap_receipt_mutation()` and
`app_acl_r2_assert_bootstrap_receipt_insert(bytea,bytea)` with no grant and
only tag 1 effective EXECUTE. It has exactly one enabled receipt trigger bound
to the first function and two default-ACL absence assertions for bootstrap:
table/public and function/record_platform_internal. M2 has exactly three
objects: the direct-migrator-owned revisions and head tables, each with the
strictly ordered tag-2 direct-migrator and tag-3 center-runtime no-grant-option
`SELECT` grants and ordinary-reader mask tags 2/3 (`0x06`); and the
direct-migrator-owned `app_acl_r2_reject_manifest_mutation()` function with
its tag-2 direct-migrator no-grant-option `EXECUTE` self-grant and only tag 2
intended ordinary-reader `EXECUTE` (`0x02`). These masks preserve the fixed
ordinary-reader policy and do not describe the owner's complete PostgreSQL
capability set. It has the two named immutable M2 triggers and two
direct-migrator default-ACL absence assertions. SQL catalog checks must prove
the owner fields, exact `aclexplode` entries including the owner self-entries,
no PUBLIC (`grantee = 0`) item, only direct-owner and center-runtime table
`SELECT` true, all six other table probes and all platform-admin probes false, and
the fixed ordinary-reader masks exact. The historical, completed Slice
4 change established the current fixed M2 control-ACL value and digest. The
2026-07-29 Decision C correction leaves that resulting value and digest
unchanged and does not alter its grammar, magic, version, object count, role
tags, field order, or masks. `control_acl_digest =
SHA-256(control_acl_body)`.

The literal `domain_body` and `l2_acl_body` decoder, malformed, and nesting
coverage belongs solely to Slice 3 `app_acl_r2_receipt_test.go`. Slice 2
source/manifest tests do not consume that literal corpus; their vector coverage
is limited to source and manifest bodies.

### M2 Canonical Manifest Body

The M2 body, whose digest is the stored M2 digest, is exactly:

```text
"HOUFENG-APP-ACL-MANIFEST-R2-V1" || u16be(1) || u16be(protocol_version=2)
|| u64be(manifest_revision=2) || u64be(m1_revision=1)
|| m1_manifest_digest || m1_source_set_digest || m1_privilege_set_digest
|| str16(m1_migrator_catalog_role,1..63)
|| str16(direct_migrator_name,1..63) || u32be(direct_migrator_oid)
|| body32(r2_source_set_body) || r2_source_set_digest
|| body32(r2_privilege_set_body) || r2_privilege_set_digest
|| body32(domain_body) || domain_digest || receipt_digest
|| body32(control_acl_body) || control_acl_digest
|| u64be(recorded_at_unix_microseconds)
```

`m1_manifest_digest` is the sole predecessor/M1 digest; there is no second
ambiguous predecessor field. The five immutable M1-link fields are M1 revision,
M1 manifest/source/privilege digests, and M1 migrator role. The two migrator
names are equal. Nested bodies must parse using their stated magics and their
stored digests must be SHA-256 of their exact raw bytes. `recorded_at` is
nonnegative UTC Unix microseconds. `m2_digest = SHA-256` over this full
preimage, excluding the digest column itself. Frozen M1 keeps its original V1
preimage and is never parsed by an R2 decoder as an M2 value.

Correcting the pgcrypto member signature changes the canonical L2 receipt body
at both `identity_set_sha256` and the member-16 `identity_arguments` string, so
its SHA-256 receipt digest changes. That receipt digest is an M2 preimage field
above, so a finalized M2 digest changes in the same deployment. Neither value
is a stable vector: receipt fields include local role/OID/domain facts and M2
also includes `recorded_at`. The protocol-2 field order and all fixed nested
R1/R2 source, privilege, domain, and control-ACL bytes remain unchanged.

### Immutable Receipt Canonical Body

The receipt body's SHA-256 is the stored immutable receipt digest. Its full
preimage is:

```text
"HOUFENG-APP-ACL-R2-BOOTSTRAP-RECEIPT-V1" || u16be(1) || u16be(protocol=2)
|| u16be(r1_source_count=52) || body32(r1_source_body) || r1_source_digest
|| u16be(r1_privilege_count=204) || body32(r1_privilege_body) || r1_privilege_digest
|| u16be(r2_source_count=53) || body32(r2_source_body) || r2_source_digest
|| u16be(r2_privilege_count=206) || body32(r2_privilege_body) || r2_privilege_digest
|| r2_0052_full_file_sha256 || r2_bootstrap_section_sha256 || r2_finalize_section_sha256
|| body32(domain_body) || domain_digest
|| u16be(role_count=4) || role[4]
|| u32be(server_version_num) || str16(server_version,1..32)
|| str16(extension_name,1..63) || u32be(extension_oid)
|| str16(extension_schema,1..63) || str16(extension_version,1..16)
|| str16(extension_owner_name,1..63) || u32be(extension_owner_oid)
|| identity_set_sha256
|| u16be(member_count=36) || member[36]
|| str16(receipt_schema="public") || str16(receipt_table,1..63)
|| u32be(receipt_owner_oid=10) || u8(singleton=1)
|| u16be(helper_function_count=2) || helper_function[2]
|| u16be(receipt_trigger_count=1) || trigger[1]
|| body32(l2_acl_body) || l2_acl_digest

role = u8(control_role: 1..4) || str16(name,1..63) || u32be(oid)
     || u8(flags: bit0=LOGIN, bit1=INHERIT, bit2=SUPERUSER)
     || u16be(recursive_membership_count=0)
member = u32be(oid) || str16(schema,1..63) || str16(proname,1..63)
       || str16(identity_arguments,0..512) || str16(owner_name,1..63)
       || u32be(owner_oid)
helper_function = str16(schema,1..63) || str16(function_identity,1..1024)
                || u32be(owner_oid)
```

Roles are control tags 1, 2, 3, 4 in that order and have unique names/OIDs.
Bootstrap is OID 10 with LOGIN/SUPERUSER, direct migrator and center runtime
are LOGIN/NOINHERIT/non-superuser, platform admin is non-superuser, and every
role has an empty recursive membership closure. Members are strict ascending
OID order, all have schema `record_platform_internal`, owner OID 10/name equal
bootstrap, and their `record_platform_internal.proname|identity_arguments` set
is exactly the normative 36-member list above. `identity_arguments` is the
unmodified `pg_get_function_identity_arguments(oid)` output, including the
`pgp_armor_headers` `OUT key`/`OUT value` terms; `identity_set_sha256` is the
sole accepted SHA-256 of its raw-byte-sorted UTF-8 lines with a terminal LF per
line. The extension is `pgcrypto` v1.3 in `record_platform_internal`, owned by
direct migrator. The helper list is strict `(schema, identity)` order and is
exactly the two named L2 functions; the one trigger is the L2 trigger record
above. Receipt parsing checks the nested receipt contract. The shared constrained
post-bootstrap continuity predicate checks allowed server version, fresh database
OID/name, exact extension/member/dependency/owner/ACL, role/membership, domain,
and application-source facts against the fresh local snapshot, without reading a
physical system identifier. It contains no server filesystem or artifact
provenance evidence.
The 0052 section hashes cover the raw inclusive byte range from the unique
ASCII `HOUFENG-APP-ACL-R2-BOOTSTRAP-BEGIN/END` and
`HOUFENG-APP-ACL-R2-FINALIZE-BEGIN/END` markers, respectively, through the
terminal LF of each END marker. The receipt parser validates every nested body
and digest and exact EOF; the shared constrained predicate validates the
post-bootstrap fresh catalog continuity facts. Bootstrap-only live binding is a
separate OID-10 proof. The security claim remains local catalog continuity only,
never session drain, physical-clone detection, restore detection, or liveness
attestation.

There is no input-only receipt compatibility mode. Before this R2 work is
released, the corrected constant and catalog fixture replace the unreleased
input-only expectation; after bootstrap, parser and continuity checks admit
only the full-formatter form. This is not a protocol bump and does not alter
the frozen `0051` checksum or isolated `0052` source bytes/checksum: neither
SQL source serializes a fixed pgcrypto identity digest.

## Shared State Classifier

`ClassifyAppACLR2State` is the sole full state classifier, implemented in the
one R2-only state file `app_acl_r2_state.go`. Its reader,
`PostgresAppACLR2StateReader`, and the generic
`ReadAppACLR2CatalogPredicatesInTx` use the one shared constrained
post-bootstrap continuity predicate. They accept an already-open PostgreSQL
transaction, do not start/commit one, and never call `pg_control_system()`.
The admission caller runs them in one `REPEATABLE READ, READ ONLY` transaction
after reserving the connection and holding the session-level shared transition
advisory lock, while finalize runs the same shared path inside its serializable
closure after acquiring the conflicting exclusive lock. Finalizer preflight,
post-DCL readback, normal repeat, and ACK recovery all use this same path; no
finalizer-private classifier, predicate, snapshot, or live-system verifier is
authorized. Ordinary bootstrap may invoke it in its serializable closure only
after the exact actor-gated metadata rejection sequence below has established
that M2/unknown objects are absent. After evidence reads succeed the classifier
returns exactly `R1`, `PREPARED`, `FINALIZED`, or `CORRUPT`; a reader error is
returned as an error, and any accompanying zero-value `CORRUPT` is not a state
verdict. The ordinary-bootstrap metadata gate returns no state, and the sole
bootstrap ACK-loss exception is the private observer defined below; neither
weakens the full classifier or invents a fifth state.

### Ordinary Bootstrap Rejection Gate

Ordinary bootstrap uses exactly this rejection-only ordering: `OID-10 actor
gate -> reserved-object metadata-only inventory -> reject M2/unknown presence
-> only when M2/unknown are absent, invoke the full shared classifier`. The
inventory is permitted here as an actor-gated rejection gate in addition to
its permitted use during uncertain bootstrap ACK recovery. It observes only
reserved-object metadata; it is not a partial state classifier and returns no
`AppACLR2State`.

Any M2 reserved identity or unknown reserved identity in that inventory is a
direct fail-closed rejection. On that branch ordinary bootstrap must not call
`ClassifyAppACLR2State` or `ReadAppACLR2CatalogPredicatesInTx`; read, lock,
scan, or aggregate either M2 relation's contents; read an M2 head row, manifest
body, predicate, control ACL, or helper/trigger definition; or classify
`FINALIZED`. Only the branch where both M2 and unknown presence are absent may
invoke the full shared classifier. All classifier predicates and typed-state
rules remain unchanged after that gate.

On the permitted ordinary-bootstrap branch, exact R1 runs the bootstrap-only
live binding before bootstrap DDL, and an exact PREPARED normal repeat runs that
same live binding before target-state success. The shared classifier supplies
only constrained post-bootstrap continuity and must not weaken either OID-10
bootstrap proof.

### Bootstrap ACK-Recovery Observer

`observeAppACLR2BootstrapACKRecoveryInTx` is a private, bootstrap-only,
acknowledgement-loss observer. It is not `ClassifyAppACLR2State`, does not
implement `AppACLR2StateReader`, does not return `AppACLR2State`, and is never
usable by runtime or finalizer code. Its private result has only `R1` and
`PREPARED` outcomes; every other result is an error with no outcome. It may run
only after the existing direct OID-10 bootstrap actor gate and an uncertain
bootstrap commit acknowledgement, in a fresh locked SERIALIZABLE recovery
closure. It acquires only the transition advisory lock for that recovery and
takes no M2 table lock. It is the sole exception to requiring the full
classifier for ACK recovery, and it does not classify FINALIZED. Its PREPARED
success additionally requires the bootstrap-only live binding; this is not
delegated to the shared constrained reader.

The observer may read only the frozen R1 verifier evidence; the complete
`app_acl_r2_*` name/identity inventory in `pg_class`, `pg_proc`, and
`pg_trigger` (with their namespaces); and, only when that inventory is exactly
the five L2 identities, the receipt singleton plus the existing L2 receipt,
helper, ACL, default-ACL, and bootstrap-only live binding plus fresh
bootstrap-catalog comparison. The inventory read observes metadata only: it
never reads either M2 relation's rows, head,
contents, manifest body, control ACL, helper/trigger definition, or M2
predicate. It never calls `ClassifyAppACLR2State`,
`ReadAppACLR2CatalogPredicatesInTx`, or any M2 reader.

It proves `R1` only when the frozen verifier succeeds and the complete reserved
inventory is empty. It proves `PREPARED` only when the frozen verifier
succeeds, the inventory is exactly the L2 table, primary-key index, two helpers,
and receipt trigger, and there is exactly one canonical receipt row whose
digest, parsed body, bootstrap-only live binding, and fresh L2/catalog proof all
succeed. An
unknown reserved object, an incomplete or excessive L2 inventory, any L2 row
or surface drift, or any single M2 reserved object (including a complete M2
inventory or an L2/M2 mixture) returns an error. It never chooses a nearest
outcome. Every query/scan/verifier error also returns an error with no outcome;
there is no observer `CORRUPT` verdict, and a `(CORRUPT, error)` from any
called API remains error-only. The metadata presence check does not create an
M2 reader authority or a superuser FINALIZED-classification path.

Slice 4 first creates and tests the reusable, read-only
`app_acl_r2_catalog.go` L1/M1/L2/M2/control-ACL relation/head predicates.
`app_acl_r2_state.go` then composes those passed predicates into the four typed
states; it does not duplicate their catalog checks. Ordinary bootstrap consumes
the full classifier only after its metadata-only rejection gate accepts the
absence of M2/unknown objects, and the Slice 5 finalizer consumes the same
constrained classifier/predicate, so finalizer logic never rebuilds a parallel
M2 relation/head or control-ACL check or a private continuity snapshot/verifier.

The classifier is identity-blind. It reads only constrained-continuity catalog,
source, ledger, and ACL evidence; it never invokes `pg_control_system()` but
does read its exact signature/owner/ACL/effective metadata, and it never
queries or branches on `session_user` or `current_user`; bootstrap and
direct-migrator actor gates, and
`RequireDirectFrozenAppACLR1RuntimeInTx` for R1 runtime, are the only
session-identity checks. A successful direct PostgreSQL call still requires
native `SELECT` on every evidence relation that exists for the state. Pure
predicate composition is identity-invariant only with synthetic evidence
inputs; a real evidence-read failure propagates and is never `CORRUPT`.

The classifier uses these mechanically exact terms:

- `L1` is `public.schema_migrations`: exactly the frozen 52
  filename/checksum rows, in the frozen checksum mapping, and no `0052` row.
- `M1` is the frozen V1 revision/head pair: exactly one valid V1 revision at
  revision 1, exactly one non-null singleton head pointing to it, and frozen
  source/privilege bodies, digests, role-binding facts, and identity-blind
  catalog verification.
- `L2` is the bootstrap-owned R2 receipt ledger
  `public.app_acl_r2_bootstrap_receipt`. Its one row contains the canonical
  53-source body/digest, canonical three-binding/206-tuple body/digest,
  receipt digest, allowed PG16 catalog baseline, domain/role facts, and
  helper/ACL facts. Shared constrained post-bootstrap continuity compares its
  persisted domain to the immutable domain and fresh database OID/name,
  server/version, role/membership, extension/member/dependency/owner/ACL, and
  source facts; it does not reread `postgres_system_identifier`.
- `M2` is only the new pair
  `public.app_acl_r2_manifest_revisions` and
  `public.app_acl_r2_manifest_head`, never a V1 row. Its sole revision has
  `(protocol_version, manifest_revision) = (2, 2)` and its immutable M1 links
  are `m1_revision = 1`, `m1_manifest_digest`, `m1_source_set_digest`,
  `m1_privilege_set_digest`, and `m1_migrator_catalog_role`, all equal to the
  fresh M1 values. Its sole head has `singleton = true`, no alternative head,
  and points to that same revision and M2 digest. Its persisted domain must
  equal the receipt and immutable domain under the same shared continuity
  predicate.

For receipt identity, the only accepted L2 surface is the table owned by OID
10; helpers
`record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()` and
`record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea,
bytea)` owned by OID 10; and trigger
`public.app_acl_r2_bootstrap_receipt.app_acl_r2_bootstrap_receipt_immutable`
bound to an OID-10 table and OID-10 trigger function. The only accepted M2
mutation surface is the direct-migrator-owned relation pair, function
`record_platform_internal.app_acl_r2_reject_manifest_mutation()`, and the two
named immutable triggers bound to direct-migrator-owned tables and that
function. No other `app_acl_r2_*` relation, function, or trigger is allowed.

The ACL comparison is exact rather than privilege-intent based. L2 permits
only the receipt owner's inherent authority plus explicit `SELECT` without
grant option for `direct_migrator` and `center_runtime`; it has no grant to
`platform_admin` or `PUBLIC`, and none of those four non-owner roles has
helper `EXECUTE`. M2 independently proves direct-migrator ownership and the
mandatory raw ACL shape: each M2 table has exact explicit no-grant-option
`SELECT` entries for direct migrator and center runtime, and the immutable
helper has the exact explicit no-grant-option direct-migrator `EXECUTE` entry.
An M2 owner OID cannot substitute for either mandatory direct-migrator
self-ACL entry. Direct migrator and center runtime have no table privilege
beyond `SELECT`; only direct migrator has helper `EXECUTE`; platform admin has
none. Bootstrap and
PUBLIC have no raw M2 ACL entry, while bootstrap's superuser bypass is outside
the ordinary-reader model and is never used for classification. The control-ACL
body/digest, function
revocations, exact owner self-ACL rows, effective-access masks, and default-ACL
absence assertions must match exactly. Superuser bypass is not an ACL grant and
is never used by an authorized post-bootstrap reader/classifier/runtime path.
No additional
grant, `SECURITY DEFINER` reader, membership, `SET ROLE`, or ownership transfer
may widen these facts, and no generic or post-bootstrap reader is authorized to
call `pg_control_system()`. Therefore PREPARED requires native L2 `SELECT`, while
authorized FINALIZED classification is limited to direct migrator and center
runtime. The explicit M2 self-`SELECT` and the corresponding effective probe
are both required. Bootstrap must not invoke FINALIZED
classification through superuser bypass.

| Result | Exhaustive predicate |
| --- | --- |
| `R1` | Exact `L1` and `M1`; `L2`, its two helpers, its receipt trigger, both M2 relations, the M2 function, and both M2 triggers are all absent; no other reserved `app_acl_r2_*` catalog object exists. |
| `PREPARED` | Exact `L1` and `M1`; exactly one valid `L2` row, exact OID-10 helper/trigger identities/owners, and shared constrained post-bootstrap continuity: receipt/domain equality plus fresh database OID/name, allowed PG16 server/version, roles/memberships, pgcrypto extension/member/dependency/owner/ACL baseline, exact `pg_control_system()` signature/owner-only ACL/effective denial, source facts, and exact L2 ACL exceptions. It does not invoke the function or read a live physical system identifier. Both M2 relations/function/triggers are absent; no unknown reserved object exists. |
| `FINALIZED` | Every `PREPARED` predicate; persisted receipt, immutable domain, and M2 domain are equal; both direct-migrator-owned M2 relations/function/triggers exist with their exact identities; revision cardinality is exactly one, head cardinality is exactly one true singleton and zero alternatives, the head links to that one revision, every immutable M1 link equals fresh M1, the M2 digest/body is valid, and its separate three-binding/206-tuple body and control ACL are exact. Exactness includes the direct-migrator owner self-`SELECT`/`EXECUTE` rows, fixed ordinary-reader masks, direct owner and center runtime table `SELECT`-only, all other table probes false, only owner helper `EXECUTE`, and platform admin with no queried table/function privilege. |
| `CORRUPT` | The complement of the three complete catalog/source/ledger/ACL predicates above after evidence reads succeed. A query or scan error, including SQLSTATE `42501` for an unreadable present evidence relation, propagates as an error rather than becoming a predicate result. Session identity is not a predicate. The classifier does not choose the nearest state. |

Examples of `CORRUPT` include L1 checksum/count/0052 drift; a non-original
M1/head; L2 absent/duplicate/replaced/stale; wrong receipt helper or trigger
identity/owner/ACL; receipt/domain disagreement; fresh database OID/name drift;
wrong allowed PG16 server/version or pgcrypto extension/member/dependency/owner/
ACL baseline; an equal-cardinality member substitution; receipt or M2 ACL
exceptions outside the exact set; a one-sided, empty, duplicate, mislinked,
wrong-owned, wrong-headed, or pre-existing M2 shape; a noncanonical 53/206
body; missing, revoked, grant-option, or extra M2 direct-migrator owner
self-ACL; an effective M2 `has_*_privilege` mismatch against the owner/runtime
`SELECT`-only, owner-helper-only, and admin-none matrix; unknown R2 object; wrong
role-binding, role-attribute, or membership catalog evidence; and any
R1/PREPARED/FINALIZED mixed shape. It never
normalizes, repairs, or mutates a corrupt shape. The pure predicate-composition
matrix returns the same result for synthetic identity labels; a real direct
reader follows the native-ACL authority matrix and propagates an unreadable
evidence relation instead of returning `CORRUPT`.

Revoking a direct-migrator self-`SELECT` or self-`EXECUTE` row is the important
inverse case: the corresponding effective privilege is false. The verifier
must reject both the missing `aclexplode` row and the false effective probe as
non-exact M2; an evidence-read SQLSTATE `42501` remains a propagated error, not
a `CORRUPT` predicate result.

`AdmitAppACLR1OnlyRuntime` accepts only `R1` after
`VerifyFrozenAppACLR1StateInTx` succeeds in the already-classified snapshot and
`RequireDirectFrozenAppACLR1RuntimeInTx` accepts its returned state.
`AdmitAppACLR2Runtime` uses that sequence only for `R1`, rejects
`PREPARED`/`CORRUPT` after the allowed classifier call without
`VerifyFrozenAppACLR1StateInTx`, `RequireDirectFrozenAppACLR1RuntimeInTx`, or
frozen `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest
parsing or admission for those results. It reads R2 receipt/M2/control-ACL data
only for `FINALIZED`. `StartAppACLR2Runtime` is the new startup route that uses
this API; frozen R1 startup and admission callers are not changed.

## Transactions, Locks, Retry, And ACK Loss

Bootstrap and finalize each use a fresh SERIALIZABLE closure, SET LOCAL
search_path = pg_catalog, public, and advisory lock
houfeng.app-acl-r2-privileged-transition.v1. Finalize's fixed table-lock order
is M1 head, M1 revisions, domain identity, L1 root ledger, L2 receipt when
present, M2 revisions when present, then M2 head when present. M1 head, M1
revisions, domain identity, and the L1 root ledger use `SHARE ROW EXCLUSIVE`.
The present bootstrap-owned L2 receipt and present direct-migrator-owned M2
revisions/head are read-authority-only evidence relations and use
`ACCESS SHARE`. PostgreSQL 16 permits a `SELECT`-only actor to acquire `ACCESS SHARE`,
while `SHARE ROW EXCLUSIVE` requires `UPDATE`, `DELETE`, or `TRUNCATE`; a
relation owner whose ordinary DML privileges were explicitly revoked does not
bypass that `LOCK TABLE` ACL check. This shared lock contract applies whenever
present M2 is observed before initial classification, including a normal exact
FINALIZED repeat and post-commit finalizer ACK recovery; it is not an ACK-only
exception. The exclusive transition advisory lock, SERIALIZABLE closure, four
mutable-core `SHARE ROW EXCLUSIVE` locks, and exact classifier prevent
legitimate transition races without broadening L2/M2 authority. Exact PREPARED
keeps M2 absent; mutation uses its own DDL/DML locks, M1/head CAS, and post-DCL
exact readback. `ACCESS SHARE` still excludes concurrent `ACCESS EXCLUSIVE` DDL
drift. A hostile database/direct owner that re-grants itself privileges remains
the documented residual risk; a transient stronger table lock would not create
persistent confinement. Ordinary bootstrap first
follows its exact OID-10 actor gate and metadata-only inventory sequence.
M2/unknown presence rejects before its full
classifier or any M2 read, lock, scan, or aggregation; after confirmed absence,
its applicable table-lock order ends at L2. Absence is checked under the
advisory lock before a conditional lock. An R2 admission route reserves one
connection, acquires
`pg_advisory_lock_shared` for that same key before its first snapshot-taking
query, begins `REPEATABLE READ, READ ONLY`, then runs classification and any
`VerifyFrozenAppACLR1StateInTx` and R1 runtime-predicate work using that exact
`pgx.Tx`. It commits/rolls back the read-only transaction and releases the
session lock before returning the connection. Bootstrap/finalize take the conflicting
`pg_advisory_xact_lock` before their first state predicate. They cannot commit
a transition between R2 classification and R1 state verification; a route that
starts after their commit gets a new snapshot and rejects PREPARED rather than
admitting R1. Neither the ordinary-bootstrap metadata rejection gate nor the
bootstrap ACK observer takes an M2 table lock or reads, scans, or aggregates M2
contents; the observer is not a mutation closure and uses only the metadata and
L2 reads listed in its dedicated contract. Finalizer preflight, post-DCL
FINALIZED readback, normal repeat, and ACK recovery use only the shared
`ReadAppACLR2CatalogPredicatesInTx`/`ClassifyAppACLR2State` constrained
continuity path; no finalizer-private classifier, predicate, snapshot, or
bootstrap-live verifier is permitted.

`TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace` is mandatory. It pauses an
R2 admission immediately after an exact-R1 classification while that route
holds the shared connection lock, starts bootstrap on a second connection, and
proves bootstrap cannot acquire its exclusive lock or commit PREPARED before
state verification finishes. After release and a successful PREPARED commit, a
fresh R2 admission may classify PREPARED to identify it, then must reject it
without calling `VerifyFrozenAppACLR1StateInTx`,
`RequireDirectFrozenAppACLR1RuntimeInTx`, or frozen `AdmitAppACLRuntime`; it
must perform no R2 payload, receipt, or manifest parsing or admission and
produce no R1 admission. A paired unit test injects a completed PREPARED
classification result and proves those forbidden calls and parsing/admission
paths are not invoked. This test closes the phase-boundary race without
changing or calling frozen `AdmitAppACLRuntime`.

`app_acl_r2_catalog_test.go` owns the reusable exhaustive L1/M1/L2/M2/
control-ACL relation/head predicate matrix before the classifier is introduced.
For M2 it proves owner OID, owner self-ACL, and effective reachability as
separately queried facts: each table
needs direct-migrator and center-runtime non-grantable `SELECT`, the helper
needs direct-migrator non-grantable `EXECUTE`, direct owner and center runtime
are both `SELECT`-only, all six other table probes are false, and platform admin
has none.
A correct owner OID with a missing/revoked direct-migrator self-row is not exact
M2 and the corresponding effective probe is false. It also owns shared
constrained-continuity
rows that reject fresh database OID/name drift and receipt/domain or M2-domain
disagreement without a physical-system read.
`app_acl_r2_state_test.go` owns only composition of those predicates into typed
states and the identity-invariant pure predicate-composition matrix. Its
synthetic identity labels issue no PostgreSQL queries and do not assert any
real-reader authorization result. `app_acl_r2_bootstrap_test.go` owns executable
dependency traces proving the ordinary-bootstrap call order is exactly OID-10
actor gate, metadata-only reserved-object inventory, then either direct
M2/unknown rejection or the full classifier. Its M2/unknown fixtures assert
zero full-classifier calls and zero M2 content reads, locks, scans, or
aggregations. It also owns bootstrap-only live-binding tests: its OID-10 reader
calls `pg_control_system()`, rejects live-system/domain mismatch, and is invoked
before an ordinary PREPARED repeat or ACK-recovery PREPARED success.
`app_acl_r2_frozen_r1_verify_test.go` owns
direct transaction-bound exact-R1 verification tests for the existing
authorized R1 evidence-reader authority and the separate runtime-predicate
matrix: it proves
`VerifyFrozenAppACLR1StateInTx` accepts the caller's already-open `pgx.Tx`,
never opens a pool/transaction, does not inspect `session_user` or
`current_user`, and cannot call frozen or pool-bound `AdmitAppACLRuntime`.
It separately proves `RequireDirectFrozenAppACLR1RuntimeInTx` accepts only
`session_user == current_user == state.CenterRuntimeRole`.
`app_acl_r2_finalize_test.go` proves finalizer deletes rather than duplicates
any private classifier/predicate/snapshot and uses the shared constrained path
for PREPARED preflight, post-DCL FINALIZED readback, normal repeat, and ACK
recovery. `app_acl_r2_postgres_integration_test.go` owns the real PG16
authority matrix. Every PG16 lane first reproduces the deployment-owned
pre-R1 provisioning, proves stock unprovisioned state RED, and proves bootstrap
live binding calls `pg_control_system()` and rejects live-system/domain mismatch;
direct migrator and center runtime have no direct or effective `EXECUTE` on it
and no `pg_monitor` membership; their constrained PREPARED/FINALIZED reader
traces contain no `FROM pg_catalog.pg_control_system()` call and still succeed where a direct
call would return SQLSTATE `42501`. It proves SQLSTATE `42501` propagation for
unreadable present L2/M2 evidence, rejects fresh database OID/name drift and
receipt/domain disagreement, and preserves the exact 206-tuple, L2
three-object ACL, and M2 SQL/ACL contracts. Its M2 matrix proves a normal
direct-owner finalization, exact owner self-ACL rows, owner/runtime
`SELECT`-only, all six other table probes false, owner helper `EXECUTE`, and
admin none. It separately revokes an owner self-row and proves raw and effective
rejection. It makes no clone/restore detection claim.
It also owns the bootstrap ACK-observer query trace: R1/PREPARED proofs
use only the permitted reads and the bootstrap-only live binding, while unknown,
partial, or any M2 reserved inventory fails without an M2 relation-content read
or FINALIZED classification. `app_acl_r2_bootstrap_test.go` owns the
corresponding ACK-recovery dependency-level outcome/error matrix.
`app_acl_r2_runtime_admission_test.go` owns the adversarial R1-to-PREPARED race
and PREPARED classification-only test above. Both admission paths may classify
PREPARED to identify it but, for exact R1 only, run state verification then the
runtime predicate inside the same locked `REPEATABLE READ, READ ONLY` `pgx.Tx`;
after PREPARED classification they perform none of the forbidden verifier,
predicate, frozen-admission, or R2 payload/receipt/manifest parsing/admission
paths. Finalize runs its classifier/state-verification predicates plus its own
actor gate in its locked `SERIALIZABLE` `pgx.Tx` closure. Ordinary bootstrap's
locked closure instead applies exactly `OID-10 actor gate -> reserved-object
metadata-only inventory -> reject M2/unknown presence -> only when M2/unknown
are absent, invoke the full shared classifier`.

For ordinary bootstrap, M2 or unknown presence is a direct fail-closed
rejection with no full-classifier call, M2 content read, M2 lock, M2 scan, or
M2 aggregation and no `FINALIZED` classification. After the gate proves both
are absent, bootstrap invokes the full classifier. Exact R1 then performs the
bootstrap-only live binding, catalog-only PG16 preflight, and frozen state
verification before DDL, executes bootstrap only, creates L2/helpers/inserts/
grants/re-reads, and commits; an exact PREPARED normal repeat first runs the
same bootstrap-only live binding, then keeps the target-state no-mutation
behavior. Target PREPARED must leave both M2 relations absent. Finalize applies
its direct-migrator actor gate, classifies exact PREPARED through shared
constrained continuity, verifies the receipt/catalog through that same path
before DDL, then executes finalize only in one serializable transaction with
the mandatory order: DDL creates the M2 relation
pair/function/triggers; M2 revision/head writes insert exactly one M2 revision
and one M2 head and complete the phase-two M1 revision/digest CAS; revoke-first
control ACL normalization applies the exact ACLs; FINALIZED readback re-reads
the same shared constrained proof; then it commits. Target FINALIZED.

Only SQLSTATE 40001 and 40P01 retry the entire closure. All other errors roll
back the full attempt, including every direct-finalizer M2 DDL/DML statement.
After uncertain commit acknowledgement, bootstrap invokes only
`observeAppACLR2BootstrapACKRecoveryInTx`: its exact PREPARED outcome is
success only after bootstrap-only live binding, its exact R1 outcome is a
retryable prior state, and every observer error, including any M2 or other
reserved-object presence, is bootstrap failure. It must not invoke FINALIZED
classification, because that would use
superuser bypass instead of exact granted M2 `SELECT`; the permitted metadata
presence check is solely a rejection path, not a reclassified success. Finalize
reclassifies only through the shared constrained classifier with its
direct-migrator exact M2 read authority and never invokes `pg_control_system()`:
FINALIZED is success, PREPARED is a retryable prior state, and all else is
failure. The explicit self-`SELECT` and its effective probe are both mandatory.
A missing/revoked owner self-`SELECT` makes raw and effective evidence false and
fails exact classification as `CORRUPT`, so it is never an ACK-loss success. A
normal repeat uses that same shared constrained
path and makes no mutation. The shared path still revalidates the function's
signature/owner/ACL/effective metadata.

### Slice 5 Quality Findings — 2026-07-31 Source/Review Evidence

The following five source-level findings are resolved by the current Slice 5
implementation and its two passed reviews. Their checkmarks do not assert a
PostgreSQL 16 integration run or a later Slice/parent acceptance.

- [x] The finalizer connection source is strictly explicit: only the
   canonical direct-migrator environment value may reach the finalizer-specific
   opener, with ambient service/pass/TLS-file/default sources rejected before
   pool creation and with no generic opener fallback.
- [x] ACK-recovery continuation is bounded by the same total attempt budget
   as ordinary `40001`/`40P01` retries. Retryable observer failures and an exact
   PREPARED observer result consume attempts and cannot start an unbounded or
   nested retry sequence.
- [x] Tests instantiate the real constrained finalizer connection/transaction
   wrapper used by the production route, not merely a permissive fake with the
   same method set, and must prove its single reserved-connection, lock, and
   transaction boundary.
- [x] The DDL retry/bounds matrix covers retryable and non-retryable failures
   at finalizer-section execution, rollback to exact PREPARED, attempt
   exhaustion, and the fixed embedded section/body cardinality and size bounds.
- [x] The M2 verifier and its unit coverage implement the corrected effective
   matrix: owner/runtime `SELECT`-only, all six other table probes false, owner
   helper `EXECUTE` true, admin none, and a revoked owner self-row rejected by
   both raw and effective evidence.

> Evidence boundary (2026-07-31): specification review
> `019fb5fd-f639-7fd0-b57c-3676d95ba659` and independent quality review
> `019fb610-234f-7103-9d5d-5bbf045f39a9` both returned PASS with P0/P1/P2 = 0.
> This records current source and focused Go verification only. The PG16 live
> authority matrix, Slice 6/7 work, physical-clone/attestation claims, and
> parent/Child 1/PF-AC completion remain outside this evidence.

## Commands And Atomic Supersession

The only new admin routes are:

    houfeng-record-platform-admin bootstrap --scope app-acl-r2
      -> HOUFENG_RECORD_PLATFORM_APP_BOOTSTRAP_DATABASE_URL (only credential)
    houfeng-record-platform-admin finalize --scope app-acl-r2
      -> HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL only

They reject other scopes, positional secrets, role overrides, foreign DSNs,
file indirection, and unexpected environment reads before pool open; bootstrap
permits only its listed DSN, while finalize permits only its listed DSN. Errors
redact DSNs. There is no reverse command.

Slice 1 removes the superseded root implementation atomically. It covers the
root migration and receipt implementation, the superseded root source-contract
and privilege-compiler additions, their current frozen R1 source, manifest,
runtime, convergence, integration, migration, configuration, and CLI
locations, and the related legacy command/configuration/environment paths.
Recovery-related symbols are outside this inventory and remain. The same
status-aware assertion is required by the Slice 1 implementation and test
requirements: no match (`rg` status 1) passes, a match (status 0) fails, and an
`rg` error (any other status) fails. It also separately checks the exact
obsolete root filename:

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

## Changelog

2026-07-27 review correction: M2 moved from impossible V1 tables to a new
transactional direct-migrator-owned R2 relation pair; frozen R1 dispatch stayed
closed; the PG16 catalog baseline and 36-member contract became explicit; the
release matrix was made normative; state classification became shared; and
supersession became auditable. Follow-up correction: R2 wire layouts,
SHA-256 preimages, helper/default ACL proofs, exact image lanes, same-snapshot
state verification with isolated R1 runtime identity admission, race exclusion,
and status-aware obsolete-draft checks are now executable requirements.

2026-08-01 production-contract correction: this supersedes the earlier
owner-native effective-vector claim. Direct ownership remains, but exact M2
requires owner/runtime table `SELECT` only, all six other table probes false,
only owner helper `EXECUTE`, and matching raw self-ACL rows. Removing a self-row
makes both raw and effective evidence false. The existing revoke-first DCL,
control-ACL layout, `0x06`/`0x02` masks, SQL, vectors, and hashes remain fixed.
The same correction requires external per-database pre-R1 provisioning to
revoke PUBLIC `EXECUTE` on `pg_catalog.pg_control_system()`, with R2 limited to
read-only fail-closed verification and bootstrap-only live invocation.

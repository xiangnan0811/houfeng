# APP ACL R2 Privileged Transition Design

## Boundary And Routes

R2 is a forward-only local PostgreSQL catalog-proof transition. It is not a root
migration, R1 extension, ownership-repair tool, session-drain protocol, or
clone detector. 0051, the root 52-source ledger, M1, all V1 codecs/readers/
runners, and frozen AdmitAppACLRuntime remain unchanged.

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
| New R2-aware API | `ClassifyAppACLR2State`, `PostgresAppACLR2StateReader`, `VerifyFrozenAppACLR1StateInTx`, `RequireDirectFrozenAppACLR1RuntimeInTx`, and `AdmitAppACLR2Runtime` are new R2-package APIs. `AdmitAppACLR2Runtime` may classify PREPARED/CORRUPT to identify it and, only for exact R1, state-verifies then performs direct runtime admission in its one locked `REPEATABLE READ, READ ONLY` snapshot. Once classification returns PREPARED/CORRUPT, it calls neither `VerifyFrozenAppACLR1StateInTx` nor `RequireDirectFrozenAppACLR1RuntimeInTx`, never calls frozen `AdmitAppACLRuntime`, and performs no R2 payload, receipt, or manifest parsing or admission. It reads/parses R2 only after exact FINALIZED. |
| New startup route | `StartAppACLR2Runtime` is the separately named opt-in startup route. It invokes `AdmitAppACLR2Runtime`, reports exact R1 before upgrade, rejects PREPARED/CORRUPT, and reports R2 only after FINALIZED. It does not replace an existing R1 startup route. |

The new startup route must be deployed before bootstrap. The only new admin
CLI routes are `bootstrap --scope app-acl-r2` and `finalize --scope app-acl-r2`;
they are not aliases of generic `migrate --scope app`. No existing R1 caller
is modified to dispatch R2: frozen `AdmitAppACLRuntime`, R1 readers,
`ConvergeAppACLR1`, and generic app migration stay closed.

`ClassifyAppACLR2State` and `PostgresAppACLR2StateReader` are fully
credential-neutral in the caller's transaction. They read only catalog, source,
ledger, and ACL evidence; they never read or branch on `session_user` or
`current_user`, and an actor mismatch is never a `CORRUPT` state input. Only
the bootstrap and direct-migrator actor gates, and the separate R1 runtime
predicate below, may inspect session identity.

`VerifyFrozenAppACLR1StateInTx(ctx, tx) (FrozenAppACLR1StateV1, error)` is
implemented only in the new R2 file
`internal/center/store/migrate/app_acl_r2_frozen_r1_verify.go`. In the
caller's already-open transaction it verifies the frozen L1/M1, source,
privilege, revision/head chain, role bindings, and required catalog facts and
returns the verified `FrozenAppACLR1StateV1`, including `CenterRuntimeRole`.
It is credential-neutral: it never reads or branches on `session_user` or
`current_user`, opens no pool, starts no second transaction, and never calls
`AdmitAppACLRuntime`. With unchanged catalog/source/ledger/ACL evidence, a
different direct session returns the same verified state rather than a corrupt
state result.

`RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, state)` is the separate
R2-owned R1 runtime-admission predicate. It alone enforces
`session_user == current_user == state.CenterRuntimeRole`. Classifier,
bootstrap, and finalizer use state verification plus their own actor gates;
they never use this runtime predicate. PREPARED uses neither this predicate nor
frozen `AdmitAppACLRuntime`. The frozen function, its signature, and every
existing caller remain unchanged.

The R2 entry routes reserve one connection, acquire the transition's
session-level shared advisory lock on that connection before their first
snapshot-taking query, then begin the read-only repeatable-read transaction.
They release the shared lock only after state verification/runtime admission and
the transaction finish. Bootstrap/finalize use the conflicting
transaction-level exclusive advisory lock. Thus no R1-to-PREPARED/FINALIZED
commit can occur between R2 classification and the R2-owned R1 state proof.

### Identity-Invariant State Matrix

The classifier matrix runs every row below against otherwise identical exact
R1, PREPARED, and FINALIZED catalog/source/ledger/ACL fixtures; each fixture
must return its same expected classified state. The verifier column uses the
exact R1 fixture only. Thus state evidence, rather than caller identity,
controls the result; the runtime predicate is the only place where R1 runtime
identity matters.

| `session_user` | `current_user` | `ClassifyAppACLR2State` | `VerifyFrozenAppACLR1StateInTx` for exact R1 | `RequireDirectFrozenAppACLR1RuntimeInTx` for R1 runtime |
| --- | --- | --- | --- | --- |
| `state.CenterRuntimeRole` | `state.CenterRuntimeRole` | Returns the fixture's same state; no identity branch | Returns the same verified `FrozenAppACLR1StateV1`; no identity branch | Accepts |
| direct migrator | direct migrator | Returns the fixture's same state; no identity branch | Returns the same verified state; no identity branch | Rejects |
| bootstrap OID-10 role | bootstrap OID-10 role | Returns the fixture's same state; no identity branch | Returns the same verified state; no identity branch | Rejects |
| platform admin | platform admin | Returns the fixture's same state; no identity branch | Returns the same verified state; no identity branch | Rejects |
| unrelated direct role | unrelated direct role | Returns the fixture's same state; no identity branch | Returns the same verified state; no identity branch | Rejects |
| any distinct direct pair | a different role | Returns the fixture's same state; no identity branch | Returns the same verified state; no identity branch | Rejects |

The distinct-pair row is exercised through a test identity fixture, not by
`SET ROLE`; the task creates no role, membership, ownership, or credential
handoff. Only bootstrap and direct-migrator actor gates, plus the direct R1
runtime predicate, inspect session identity. Classifier and verifier state
proof do not.

## Authority And PG16 Baseline

| Actor | Exact proof and authority | Forbidden |
| --- | --- | --- |
| Direct migrator | Direct session_user = current_user; constrained LOGIN/NOINHERIT/non-superuser; no recursive membership; owns DB, R1 objects, frozen M1 relations, new R2 relations, domain identity, and pgcrypto; finalize only | SET ROLE, role/membership DDL, owner changes, receipt mutation, extension drop/recreate |
| Bootstrap superuser | PostgreSQL 16 direct login, role OID 10, rolsuper; owns receipt table and bootstrap helpers | Direct-migrator DSN, M2 relations, owner changes, extension drop/recreate |
| Center runtime | Direct constrained role matching center_runtime; read-only R2 admission | DDL/DCL, receipt mutation, SET ROLE |
| Platform admin and PUBLIC | No R2 receipt/M2/helper privilege | Every R2 mutation/read/execute grant |

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
and must not use `SET ROLE`. In its locked transaction, it proves only the
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
identity/owner, or ACL baseline. It records only those catalog facts plus the
application R1/R2 source hashes already defined by this task.

This local catalog proof does not prove file bytes, filesystem paths, symlink
resistance, image artifacts, or package provenance. It performs no server-file
read and has no bootstrap directory configuration. External supply-chain policy
owns all of those non-catalog claims. Finalize and runtime compare the receipt
to fresh local catalog facts only: allowed server version, extension
name/version/schema/OID/owner, the exact 36 `record_platform_internal` member
identities/OIDs/owners/dependencies, and required ACL/domain facts. A different
baseline is a hard rejection, never a repair. No helper with an unsafe
`EXECUTE` grant exists.

### Normative PG16 pgcrypto Contract

For each allowed PostgreSQL 16 `server_version_num`, the required local catalog
baseline is `pgcrypto` `extversion = 1.3` in
`record_platform_internal`, owned by direct migrator, with the exact member
dependency/identity/owner/ACL facts below. This is intentionally a catalog
definition, not a claim about an extension file, container artifact, or package.

The sole accepted sorted `identity_set_sha256` is
c544baa39772e3986e5d2c9202ae74b0027815e7021ab7d891f08d878d3e87f7 over
raw-byte-sorted UTF-8 lines
`record_platform_internal.<proname>|pg_get_function_identity_arguments(oid)`
followed by LF. OUT arguments are not identity arguments. Unsorted enumeration
digests are not receipt values and never satisfy an acceptance rule. This list,
not its cardinality, is normative:

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
    record_platform_internal.pgp_armor_headers|text
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

Bootstrap persists the allowed `server_version_num`, `server_version`,
`extversion = 1.3`, extension/schema/OID/owner/dependency facts,
identity-set SHA-256, `member_count = 36`, and exact local ACL facts in the
receipt. It also persists the R1/R2 application source and section hashes
already defined by this task. It persists no server-file, directory, path,
image, package, or server-file-hash evidence. Finalize/runtime compare the
receipt's allowed server/version and catalog facts to a fresh local snapshot,
including `record_platform_internal`, identity set, extension owner/dependency,
and every member identity/OID/owner. A 36-row set with a substituted identity
or identity argument is rejected; equal cardinality is never evidence of
equivalence.

The PostgreSQL 16 catalog matrix is an all-lane requirement, not a generic
smoke test. The fixture lanes are exactly `postgres:16.0`, `postgres:16.6`, and
`postgres:16.12`, selected by
`HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=<exact-image>`. The runner rejects
unset, `postgres`, `postgres:16`, `postgres:16-alpine`, and every value outside
that three-member fixture allowlist, and uses the selected image for every
fixture database. Each lane proves only its live allowed server version,
`extversion`, the exact 36-member inventory/identity-set digest,
direct-migrator extension owner/dependency, OID-10 member owners, and ACL
baseline. The fixed fixture image names make the catalog coverage reproducible;
they do not prove image-artifact or package provenance.

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
creates both relations/triggers, inserts the one M2 row and one M2 head, sets
the exact ACLs, re-reads the result, then commits. PostgreSQL DDL is
transactional, so any error rolls back all R2 DDL/DML and leaves exact PREPARED.
There is no committed rollback: a successful FINALIZED state always contains
the one revision and one head, both linked to frozen M1 by the immutable fields
above. No M2 data is ever inserted into the frozen 0051 V1 relations.

Before the M2 row/head are inserted or FINALIZED is accepted, finalize runs
the same revoke-first proof for its exact surfaces. It revokes all table
privileges on both M2 relations from `PUBLIC`, bootstrap, `direct_migrator`,
`center_runtime`, and `platform_admin`, then grants only no-grant-option
`SELECT` to `center_runtime`. It revokes all function privileges on
`record_platform_internal.app_acl_r2_reject_manifest_mutation()` from those
same five grantees. The direct migrator remains able to execute its own
function only through immutable owner authority; there is no explicit grant
to it. The post-DDL catalog proof requires exactly one center-runtime SELECT
entry per M2 table, no PUBLIC ACL item, no bootstrap/admin effective access,
no center-runtime privilege other than SELECT, no explicit direct-migrator
grant, and no effective helper EXECUTE for bootstrap/runtime/admin/PUBLIC.
It also rejects any `pg_default_acl` row for the direct-migrator OID applying
globally or to `public` tables or globally or to
`record_platform_internal` functions. The receipt L2 SELECT exceptions remain
only direct migrator and center runtime; M2 control access is represented
separately and never inflates the 206 application tuples.

Direct migrator owns both R2 relations; center runtime gets explicit SELECT on
both; bootstrap, platform admin, and PUBLIC get no direct/effective grant.
This transition-control ACL is bound by M2 control_acl_digest and fresh catalog
comparison. It is deliberately outside the application privilege grammar. The
application body remains exactly 206: frozen R1's 204 semantic tuples plus only
the two receipt SELECT exceptions:

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
`<schema>.<proname>(<pg_get_function_identity_arguments>)`; pgcrypto members
specifically use `record_platform_internal`, and identities are not
whitespace-normalized. Every decoder rejects an invalid magic/version, short
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
`<schema>.<proname>(<pg_get_function_identity_arguments>)`, and pgcrypto
members specifically use `record_platform_internal`; it is not
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
it must equal the one immutable domain row and current local server/database.

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
mask bit `tag - 1` states whether that role has the record's relevant
privilege (SELECT for tables, EXECUTE for functions), including owner
authority. Every listed default-ACL assertion means that no matching global or
named-schema `pg_default_acl` row exists; its appearance in the body proves
absence, not a grant.

L2 has exactly three objects: the bootstrap-owned receipt table with two
explicit SELECT grants to tags 2/3 and effective SELECT mask tags 1/2/3; the
two bootstrap-owned functions
`app_acl_r2_reject_bootstrap_receipt_mutation()` and
`app_acl_r2_assert_bootstrap_receipt_insert(bytea,bytea)` with no grant and
only tag 1 effective EXECUTE. It has exactly one enabled receipt trigger bound
to the first function and two default-ACL absence assertions for bootstrap:
table/public and function/record_platform_internal. M2 has exactly three
objects: the direct-migrator-owned revisions and head tables, each with only a
tag-3 SELECT grant and effective mask tags 2/3, and the direct-migrator-owned
`app_acl_r2_reject_manifest_mutation()` function with no explicit grant and
only tag 2 effective EXECUTE. It has the two named immutable M2 triggers and
two direct-migrator default-ACL absence assertions. SQL catalog checks must
also prove all unrepresented table privileges are false, `aclexplode` has no
PUBLIC (`grantee = 0`) item, and the required effective masks are exact.
`control_acl_digest = SHA-256(control_acl_body)`.

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
is exactly the normative 36-member list above; `identity_set_sha256` is the
sole accepted SHA-256 of its raw-byte-sorted UTF-8 lines with a terminal LF per
line. The extension is `pgcrypto` v1.3 in `record_platform_internal`, owned by
direct migrator. The helper list is strict `(schema, identity)` order and is
exactly the two named L2 functions; the one trigger is the L2 trigger record
above. Receipt parsing checks the allowed server version and the exact extension,
member/dependency, owner, ACL, domain, and application-source catalog contract
against a fresh local snapshot. It contains no server filesystem or artifact
provenance evidence.
The 0052 section hashes cover the raw inclusive byte range from the unique
ASCII `HOUFENG-APP-ACL-R2-BOOTSTRAP-BEGIN/END` and
`HOUFENG-APP-ACL-R2-FINALIZE-BEGIN/END` markers, respectively, through the
terminal LF of each END marker. The receipt parser validates every nested body
and digest, exact EOF, and fresh catalog equality. Its security claim remains
local catalog equality only, never session drain or physical-clone rejection.

## Shared State Classifier

`ClassifyAppACLR2State` is the sole state classifier, implemented in the one
R2-only state file `app_acl_r2_state.go`. Its reader is
`PostgresAppACLR2StateReader`. The reader accepts an already-open PostgreSQL
transaction and does not start/commit one; the admission caller runs it in one
`REPEATABLE READ, READ ONLY` transaction after reserving the connection and
holding the session-level shared transition advisory lock, while
bootstrap/finalize run the same predicates inside their serializable closure
after acquiring the conflicting exclusive lock. It returns exactly `R1`,
`PREPARED`, `FINALIZED`, or `CORRUPT`. All commands, ACK-loss recovery, and
admission use this typed result; no caller performs a partial state check or
invents a fifth state.

Slice 4 first creates and tests the reusable, read-only
`app_acl_r2_catalog.go` L1/M1/L2/M2/control-ACL relation/head predicates.
`app_acl_r2_state.go` then composes those passed predicates into the four typed
states; it does not duplicate their catalog checks. Bootstrap and the Slice 5
finalizer consume the same predicates, so finalizer logic never rebuilds a
parallel M2 relation/head or control-ACL check.

The classifier is fully credential-neutral. It reads only catalog, source,
ledger, and ACL evidence, never `session_user` or `current_user`; bootstrap and
direct-migrator actor gates, and `RequireDirectFrozenAppACLR1RuntimeInTx` for
R1 runtime, are the only session-identity checks. An unchanged evidence fixture
therefore classifies identically for every direct identity, and a session
mismatch is never `CORRUPT`.

The classifier uses these mechanically exact terms:

- `L1` is `public.schema_migrations`: exactly the frozen 52
  filename/checksum rows, in the frozen checksum mapping, and no `0052` row.
- `M1` is the frozen V1 revision/head pair: exactly one valid V1 revision at
  revision 1, exactly one non-null singleton head pointing to it, and frozen
  source/privilege bodies, digests, role-binding facts, and credential-neutral
  catalog verification.
- `L2` is the bootstrap-owned R2 receipt ledger
  `public.app_acl_r2_bootstrap_receipt`. Its one row contains the canonical
  53-source body/digest, canonical three-binding/206-tuple body/digest,
  receipt digest, allowed PG16 catalog baseline, domain/role facts, and
  helper/ACL facts; every field must equal a fresh catalog snapshot.
- `M2` is only the new pair
  `public.app_acl_r2_manifest_revisions` and
  `public.app_acl_r2_manifest_head`, never a V1 row. Its sole revision has
  `(protocol_version, manifest_revision) = (2, 2)` and its immutable M1 links
  are `m1_revision = 1`, `m1_manifest_digest`, `m1_source_set_digest`,
  `m1_privilege_set_digest`, and `m1_migrator_catalog_role`, all equal to the
  fresh M1 values. Its sole head has `singleton = true`, no alternative head,
  and points to that same revision and M2 digest.

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
helper `EXECUTE`. M2 permits only direct-migrator ownership plus explicit
`SELECT` without grant option for `center_runtime` on both M2 relations; its
control-ACL body/digest, function revocations, effective-access masks, and
default-ACL absence assertions must match exactly. Superuser bypass is not an
ACL grant and is never used by runtime admission.

| Result | Exhaustive predicate |
| --- | --- |
| `R1` | Exact `L1` and `M1`; `L2`, its two helpers, its receipt trigger, both M2 relations, the M2 function, and both M2 triggers are all absent; no other reserved `app_acl_r2_*` catalog object exists. |
| `PREPARED` | Exact `L1` and `M1`; exactly one valid `L2` row, exact OID-10 helper/trigger identities/owners, receipt-to-live-catalog equality (including allowed PG16 server/version, pgcrypto extension/member/dependency/owner/ACL baseline, and all 36 identities), and exact L2 ACL exceptions; both M2 relations/function/triggers are absent; no unknown reserved object exists. |
| `FINALIZED` | Every `PREPARED` predicate; both direct-migrator-owned M2 relations/function/triggers exist with their exact identities; revision cardinality is exactly one, head cardinality is exactly one true singleton and zero alternatives, the head links to that one revision, every immutable M1 link equals fresh M1, the M2 digest/body is valid, and its separate three-binding/206-tuple body and control ACL are exact. |
| `CORRUPT` | The complement of the three complete catalog/source/ledger/ACL predicates above, including any catalog-read error. Session identity is not a predicate. The classifier does not choose the nearest state. |

Examples of `CORRUPT` include L1 checksum/count/0052 drift; a non-original
M1/head; L2 absent/duplicate/replaced/stale; wrong receipt helper or trigger
identity/owner/ACL; wrong allowed PG16 server/version or pgcrypto
extension/member/dependency/owner/ACL baseline; an equal-cardinality member
substitution; receipt or M2 ACL
exceptions outside the exact set; a one-sided, empty, duplicate, mislinked,
wrong-owned, wrong-headed, or pre-existing M2 shape; a noncanonical 53/206
body; unknown R2 object; wrong role-binding, role-attribute, or membership
catalog evidence; and any R1/PREPARED/FINALIZED mixed shape. It never
normalizes, repairs, or mutates a corrupt shape. A wrong direct session with
otherwise unchanged evidence returns that evidence's same result, never
`CORRUPT`.

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
houfeng.app-acl-r2-privileged-transition.v1. The fixed table-lock order is M1
head, M1 revisions, domain identity, L1 root ledger, L2 receipt when present,
M2 revisions when present, then M2 head when present; each is SHARE ROW
EXCLUSIVE. Absence is checked under the advisory lock before a conditional
lock. An R2 admission route reserves one connection, acquires
`pg_advisory_lock_shared` for that same key before its first snapshot-taking
query, begins `REPEATABLE READ, READ ONLY`, then runs classification and any
`VerifyFrozenAppACLR1StateInTx` and R1 runtime-predicate work using that exact
`pgx.Tx`. It commits/rolls back the read-only transaction and releases the
session lock before returning the connection. Bootstrap/finalize take the conflicting
`pg_advisory_xact_lock` before their first state predicate. They cannot commit
a transition between R2 classification and R1 state verification; a route that
starts after their commit gets a new snapshot and rejects PREPARED rather than
admitting R1.

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
`app_acl_r2_state_test.go` owns only composition of those predicates into typed
states and the identity-invariant `ClassifyAppACLR2State` matrix. For otherwise
identical exact R1, PREPARED, and FINALIZED fixtures, every identity row above
must return the same classified state without a `session_user`/`current_user`
read or a wrong-session `CORRUPT` result. `app_acl_r2_frozen_r1_verify_test.go` owns
direct transaction-bound state verification tests and the exact-R1 verifier/
runtime-predicate matrix above: it proves
`VerifyFrozenAppACLR1StateInTx` accepts the caller's already-open `pgx.Tx`,
never opens a pool/transaction, does not inspect `session_user` or
`current_user`, and cannot call frozen or pool-bound `AdmitAppACLRuntime`.
It separately proves `RequireDirectFrozenAppACLR1RuntimeInTx` accepts only
`session_user == current_user == state.CenterRuntimeRole`.
`app_acl_r2_runtime_admission_test.go` owns the adversarial R1-to-PREPARED race
and PREPARED classification-only test above. Both admission paths may classify
PREPARED to identify it but, for exact R1 only, run state verification then the
runtime predicate inside the same locked `REPEATABLE READ, READ ONLY` `pgx.Tx`;
after PREPARED classification they perform none of the forbidden verifier,
predicate, frozen-admission, or R2 payload/receipt/manifest parsing/admission
paths. Bootstrap and finalize run their classifier/state-verification
predicates plus their own actor gates in their respective locked `SERIALIZABLE`
`pgx.Tx` closures.

Bootstrap applies its bootstrap-superuser actor gate, classifies exact R1,
performs catalog-only PG16 preflight and frozen state verification before DDL,
executes bootstrap only, creates L2/helpers/inserts/grants/re-reads, then
commits. Target PREPARED; it must leave both M2 relations absent. Finalize
applies its direct-migrator actor gate, classifies exact PREPARED, verifies the
receipt/catalog before DDL, executes finalize only, creates the M2 relation
pair/function/triggers, inserts exactly one M2 revision and one M2 head,
applies control ACLs, re-reads FINALIZED proof, then commits. Target FINALIZED.

Only SQLSTATE 40001 and 40P01 retry the entire closure. All other errors roll
back the full attempt, including every direct-finalizer M2 DDL/DML statement.
After uncertain commit acknowledgement, the same credential reclassifies with
the sole classifier: bootstrap sees PREPARED as success, R1 as retryable prior
state, all else failure; finalize sees FINALIZED as success, PREPARED as
retryable prior state, all else failure. A normal repeat has the same
target-state behavior and makes no mutation.

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

# Blob、附件、配额与扫描设计

## 0. Design status and delivery control

本设计以父任务 `../07-13-vps-detail-experience-design/design.md` 的 §13、§15、§19.2、§20、§21、§23 和已合入 Records Core 为准。采用一个 worktree、一个 branch、一个 PR 与三个强制检查点；每个检查点有独立 focused verification 和进度报告，后续检查点不得被当前检查点顺手提前实现。

Child 3 交付 attachment platform 和下游可复用接缝，不交付 Child 5 的完整材料工作区、Child 10 的通用 portability workflow 或 Child 11 的 end-to-end recovery controller。

## 1. Cross-layer data flow

```text
Draft payload attachment_ids
  -> POST upload session and quota reservation
  -> local stream or private S3 temporary object
  -> server hash/signature/size verification
  -> quarantined processor job
  -> available logical attachment + immutable digest Blob
  -> CompleteRevisionInput canonical attachment_ids
  -> transaction-local RevisionParticipant
  -> draft-to-record transfer + immutable record_revision_attachments
  -> authorized revision read / preview / download
  -> deletion adapter / Blob inventory and restore verification seams
```

Validation ownership is explicit:

- HTTP validates transport shape and stable error mapping.
- `attachments` domain validates IDs, state transitions, declared metadata and quota inputs.
- store transactions own locking, reservation accounting, ownership transfer, refs, pins and receipts.
- BlobStore validates physical object version/hash/size.
- processor validates content safety and produces typed result + workspace purge receipt.
- `recordauth.Policy` and deletion fence own every record download/read decision; Web visibility is never an authorization boundary.

All collection fields use non-nil empty slices at HTTP boundaries. Go and TypeScript use the same snake_case JSON names.

## 2. Domain identities and state

Stable IDs use the repository `ids.New` pattern with distinct prefixes:

- `att_`: logical attachment
- `aup_`: upload session
- `apj_`: processor job
- `cpw_`: content processor workspace
- `bgp_`: GC pin

```go
type UploadState string

const (
	UploadStateCreated     UploadState = "created"
	UploadStateUploading   UploadState = "uploading"
	UploadStateQuarantined UploadState = "quarantined"
	UploadStateAvailable   UploadState = "available"
	UploadStateRejected    UploadState = "rejected"
	UploadStateExpired     UploadState = "expired"
)

type AttachmentReference struct {
	AttachmentID string
}
```

Attachment references are ordered because the later material UI and Markdown reference list must round-trip deterministically. Normalization rejects invalid or duplicate IDs and preserves caller order. The ordered IDs enter the revision canonical hash; author/save reason remain commit metadata as in Records Core.

`records.RevisionCommitted` gains the published `DraftID` needed by transaction participants. Draft identity is transaction context, not canonical record content. The attachment participant receives the normalized IDs from `CompleteRevisionInput`, validates them through the supplied `pgx.Tx`, transfers only attachments owned by that exact draft, permits existing attachments owned by the same record, and writes ordinal revision refs. It performs no BlobStore or processor call.

## 3. Migration and relational invariants

`db/migrations/0053_create_record_attachments.sql` owns these surfaces:

- `blob_objects`
- `attachment_quota_accounts`
- `attachment_uploads`
- `attachment_upload_parts`
- `record_attachments`
- `record_revision_attachments`
- `attachment_processor_jobs`
- `content_processor_workspaces`
- `blob_gc_pins`
- `blob_gc_deletions`
- `attachment_purge_receipts`
- `content_workspace_purge_receipts`

The migration uses checks/FKs/unique indexes for:

- `record_id XOR draft_id` logical ownership;
- nullable immutable `origin_draft_id` upload provenance, which matches current `draft_id` while draft-owned but does not retain a foreign key to a live draft after publication;
- closed upload/processor state values; monotonic transitions remain repository CAS behavior;
- non-negative declared/actual/logical/physical byte counters;
- digest length, digest-derived Blob key equality and immutable object version identity;
- one revision ordinal and one attachment ID per revision;
- revision ref record ownership consistency;
- unique active quota reservation, complete idempotency result and exact processor job upload/attachment pairing;
- pins with bounded owner kind/expiry and exact object version;
- one immutable purge receipt per operation/surface/object version.

The original attachment Blob and the managed preview Blob are distinct durable
identities. `record_attachments.blob_key/blob_object_version` remains the exact
authorized original. Previewable profiles may additionally persist one primary
managed preview through `preview_blob_key/preview_blob_object_version`, its
allowlisted media type and exact byte size; those fields are all-null or
all-present and reference `blob_objects`. This is the durable hand-off required
by the later closed `original|preview` download variant. In this checkpoint the
primary preview is a bounded metadata-free PNG for images, a bounded first-page
PNG for PDF, and a bounded UTF-8 prefix for text; archives have no preview.

Processor outcomes also persist a closed, content-free result code separately
from `result_digest`. Retryable outcomes can therefore remain observable without
storing scanner text, filenames, command output or content in PostgreSQL or
logs. The allowed codes distinguish clean, malware, unsafe content, scanner
unavailability, timeout and internal processing failure.

`copied_from_attachment_id` and `content_workspace_purge_receipts.workspace_id` are detached provenance rather than live-row foreign keys. A source logical attachment may be purged while an authorized cross-record copy survives on the shared Blob, and an immutable workspace purge receipt survives terminal workspace/job/upload cleanup. Reverse indexes cover Blob-to-attachment, copy-source-to-copy, attachment-to-revision and Blob-to-pin lookups used by later quota, GC and deletion transactions.

`0053` and the exact `AppACLCurrentMigrationFragment` are one atomic delivery. The exact 12 new tables are registered in the current managed surface; this migration adds no sequence or SQL function. Runtime/admin privileges, convergence, effective catalog and runtime admission tests change in the same checkpoint. There is no old-database upgrade route.

## 4. Quota model

Defaults are centralized in the Go domain and exposed through config without duplicating numeric literals:

- file: 50 MiB
- record/draft provisional record: 500 MiB
- project logical: 10 GiB
- warning: 80%
- inline text preview: 5 MiB

The upload create transaction locks the project quota row and effective record/draft owner, then records a reservation before bytes are accepted. Logical usage counts every logical attachment by original size; physical usage counts unique committed Blob versions. Cross-record copy creates a new logical attachment with `copied_from_attachment_id` while retaining the same Blob version.

Publication changes only current ownership from `draft_id` to `record_id`; the upload row continues to reference the logical attachment plus its immutable `origin_draft_id`. This lets Records delete the published draft in the same transaction without erasing the upload provenance or forcing processor/workspace cleanup into the revision transaction.

Complete/reject/expire/cancel are idempotent state transitions with exact reservation release. Revision save rechecks record and project logical quota inside the revision transaction. Removing attachment IDs or saving text-only changes remains allowed when quota is exhausted.

## 5. BlobStore and backend behavior

```go
type ObjectVersion struct {
	Key       string
	VersionID string
	SHA256    [sha256.Size]byte
	Size      int64
}

type BlobStore interface {
	Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error)
	Open(context.Context, ObjectVersion, ByteRange) (io.ReadCloser, error)
	Stat(context.Context, ObjectVersion) (ObjectInfo, error)
	Delete(context.Context, ObjectVersion) (DeletionReceipt, error)
}
```

The common contract requires conditional create, exact expected digest and size, closed byte ranges, immutable version identity, typed not-found/conflict/version errors, idempotent delete and cleanup after injected interruption.

Local writes use a private same-directory temporary file, streaming hash/size validation, file fsync, conditional atomic publish, directory fsync and mode verification. Existing digest objects are accepted only after exact size/hash verification.

S3 uses a private bucket and random session temporary keys. The upload service owns this identity: it generates the key and persists it before the first `BlobStore.Put` or other object write, then passes the persisted value through `PutRequest.TemporaryKey`; the S3 adapter never creates an in-memory-only replacement key. At the 50 MiB file limit the transport may use one presigned PUT; the contract retains part metadata so multipart can be used without changing logical state. Complete always performs a server-authorized read/verification before conditional copy/publish to the digest key and deletion of the temporary key. Runtime credentials cannot list/read noncurrent versions. No user-facing download presign is issued.

The persisted upload state permits a known temporary key whose object version is not known yet. After a process crash, retry/janitor code resolves only that exact key's current version, persists the observed `VersionID` with CAS, and deletes only that exact key/version; it never discovers cleanup work through bucket or version listing. Missing, replaced or otherwise ambiguous temporary identity fails closed and remains bounded for explicit retry/expiry rather than creating another untracked version.

The managed S3 prefixes have one application writer. Runtime policy permits conditional object writes, current/exact-version reads, and exact-version deletes, but denies object/version listing and unversioned deletes; administrative mutation is restricted to explicit maintenance. This policy closes the otherwise unavoidable gap between the server-side delete-marker preflight and the conditional digest publish without granting the runtime version-list authority.

## 6. Admission and processor isolation

Admission first classifies extension + magic + MIME, applies byte and complexity bounds, and chooses a typed processor profile:

- re-encode and metadata-strip PNG/JPEG/WebP preview;
- render PDF pages through fixed `pdfinfo`/`pdftoppm` arguments;
- validate UTF-8 and emit bounded text preview;
- structurally inspect ZIP/TAR/GZIP/Zstandard, reject unsafe path, duplicate, link, encryption, nesting and expansion limits, then require ClamAV;
- reject active/unknown content before publishing availability.

The processor result binds the source Blob identity, profile, outcome code and
optional preview Blob identity in a canonical digest. A successful image/PDF/text
job cannot mark the logical attachment available until its required primary
preview has been published and committed with that exact result. Archive success
has no preview and requires a clean scanner verdict. The original object remains
the downloadable source; a preview never silently replaces it.

`cmd/houfeng-content-processor` only contains entrypoint and wiring. Domain behavior lives under `internal/center/attachments`. External commands use `exec.CommandContext` with fixed binaries/arguments and never invoke a shell. Processor workspaces are registered before any materialization; the worker claims with lease/attempt, writes only to its private workspace, publishes typed results, and leaves cleanup to an idempotent janitor that records a receipt for every terminal path.

Compose/systemd configure non-root, read-only root, `cap_drop: ALL`, tmpfs workspace, disabled core dumps, no unnecessary network and health endpoints. A required scanner that is absent/unhealthy causes archive session creation to return stable unavailable before accepting bytes.

## 7. HTTP, authorization and streaming

`POST /api/attachment-uploads` accepts draft ID, safe display name, declared size and media hint. It authorizes the draft owner and reserves quota. Response transport is explicit:

- local: center `PUT /api/attachment-uploads/:id/content`;
- S3: short-lived private temporary-object upload instructions bound to the session.

`POST .../complete` verifies backend version, actual bytes and idempotency, then queues processing; it never blocks on scanning. `GET /api/attachments/:id` returns safe metadata/status only. `GET .../content` returns original download or managed preview based on a closed query enum; it reauthorizes every request.

Draft reads require the author. Record attachment reads resolve the owning record through existing current/historical authorization evidence and obtain a content delivery lease whose duration never exceeds one second. A delivery-owned background renewal/cancel loop renews before expiry even while the Blob reader is blocked; renewal failure, owner/epoch drift, reservation, shutdown or expiry cancels and closes the source reader before another write. Database renewal uses the same content-epoch -> deletion-fence -> object-content-lease lock order as acquisition and reservation fencing, and atomically checks the captured epoch, absence of a live deletion fence and the exact owner tuple before extending it. After each bounded read, the delivery performs one final serving assertion before authorizing that chunk. The authoritative successful database read inside that final assertion is the chunk linearization point: a chunk whose assertion observed the pre-fence state may finish even if the concurrent fence commits before `writer.Write`, while every later chunk must obtain a new assertion. No database I/O is performed while holding the delivery state mutex, and neither a database lock nor that mutex is held across arbitrary writer I/O.

Deletion execution may establish the provisional reservation/deletion fence while the old object-content lease is still live. That durable fence advances the delivery epoch and makes every later renewal/assertion fail, which actively drains the stream instead of waiting for a renewal loop to stop by accident. A final assertion that already linearized against the pre-fence state may authorize exactly that in-flight chunk; the fence forbids the next assertion and therefore the next chunk. Local `revoke`/`Close` linearizes through the delivery terminal state and prevents a chunk whose final assertion has not yet acquired write authorization, without waiting for a writer already in progress. This contract deliberately does not claim zero physical network bytes after the wall-clock fence commit, because satisfying that stronger claim would require a durable permit and cancellable transport held across arbitrary writer I/O. The deletion worker must not claim the provisional operation for ledger append until the exact object-content lease has been released or expired; with the one-second maximum this leaves a bounded crash path while preserving the rule that ledger append starts only after content delivery is drained. The handler checks cancellation before each write and never converts a partial forbidden stream into success. Headers use an ASCII/UTF-8 safe filename encoder, allowlisted content type, `Content-Disposition`, `X-Content-Type-Options: nosniff`, private no-store caching and restricted CSP.

## 8. Records Core integration

The following existing contracts change together:

- `records.CompleteRevisionValues` and immutable `CompleteRevisionInput` expose ordered `AttachmentIDs()`.
- `NormalizeCompleteRevisionInput` validates IDs and includes them in `canonicalRevisionHash`.
- request fingerprints continue to include the canonical hash, so attachment changes are idempotency-visible.
- draft transport requires non-null `attachment_ids`; Records HTTP publish maps it into domain values.
- revision response includes non-null `attachment_ids` for current, historical and restored revisions.
- store revision reads load ordered refs from `record_revision_attachments`; the transaction participant writes those refs and draft-to-record ownership.
- restore copies the historical ordered IDs into a new revision. Because historical refs prevent GC, a valid historical attachment remains resolvable.

The participant runs after the core revision/current projection writes but before transaction commit. Any attachment validation or relation failure rolls back the core revision, current pointer, activity, outbox, ownership transfer and refs together.

## 9. Deletion, GC and recovery seams

The attachment deletion adapter implements the existing closed name `record_attachments` and owns only the `0053` attachment/Blob surfaces. It provides deterministic descriptor/health proof, preview counts and surviving-copy disclosure, idempotent purge, immutable receipt and verified absence. It cancels upload/processor jobs, removes logical/revision refs owned by the target record, and deletes a Blob immediately only after the transaction proves no global refs/pins remain.

Terminal cleanup is explicit and dependency-ordered: workspace, processor job, upload parts, upload and logical attachment rows are removed before an unselected expired draft is deleted. Content-free workspace purge receipts are intentionally independent of those mutable rows and remain as cleanup evidence.

Blob GC uses the durable `blob_gc_deletions` protocol; external Blob deletion never runs while a PostgreSQL transaction holds locks:

1. The claim transaction uses database time, serializes final-metadata and upload-part publication with table locks, rejects original/preview/revision/upload-part/active-pin references, removes only expired pins, and persists the exact key/version/hash/size/backend deletion fence.
2. The same claim transaction deletes the exact `blob_objects` metadata and commits. Existing foreign keys plus the publication fence prevent new logical metadata, upload-part or pin references from crossing the claim.
3. The worker performs exact-version `BlobStore.Delete` outside PostgreSQL. Exact absence is an idempotent physical success; any other failure retains the durable fence and schedules a bounded retry. A due retry or expired lease increments attempt and owner generation, so an old owner cannot complete a replacement claim.
4. Completion compares deletion ID, owner, generation, observed lease and the complete Blob identity, then stores a content-free receipt digest and decrements physical quota in the same transaction. Completion replay does not decrement quota again, and `ResolveBlobGC` reads the terminal receipt after an uncertain commit acknowledgement.

Ordinary GC requires both the caller watermark and the database-enforced 24-hour watermark. The permanent primitive targets one exact object and bypasses only this time predicate; it never bypasses references, pins or owner fencing. Ordinary GC receipts remain in `blob_gc_deletions`; they do not fabricate a `record_purge_operations` identity or reuse `attachment_purge_receipts`. The latter remains operation-scoped evidence owned by the Task 3.3 deletion adapter.

This protocol is complete for objects whose final identity has been persisted in `blob_objects`. It does not yet discover a physical final object published before its metadata transaction crashed, nor prevent a publisher that began before claim from persisting the same identity after the deletion reached `completed`. A bounded final-object publication intent/reconciliation slice must close that window before Task 2.5 and Checkpoint 2 are marked complete; bucket/version listing remains forbidden.

Final-object publication therefore uses durable `blob_publication_intents` for
the three current publishers: local/S3 upload source and processor preview. A
publisher first persists an owner-bound exact digest key, hash, size, backend
and bounded publication expiry before external I/O. Only one nonterminal intent
may own a digest key at a time. After `BlobStore.Put` or direct S3 publication,
the publisher CASes the exact observed object version into the same intent;
`attachment_upload_parts` insertion or preview `blob_objects` insertion then
marks that exact intent consumed in the same metadata transaction. GC excludes
every nonterminal intent by digest key, and intent creation uses the same
`blob_objects` then `attachment_upload_parts` table-lock order as GC.

An expired intent is handled by the existing processor reconciliation loop. It
first serializes against metadata publication and closes the intent as consumed
when a durable Blob/upload-part reference already exists. Otherwise it claims a
generation-fenced cleanup lease, resolves at most the one persisted digest key
when the exact version was not acknowledged, CASes that version, and performs
only exact-version deletion. Delete/retry/completion are durable and replayable;
absence is terminal only after the exact-key lookup or exact-version delete says
the object is absent. The reconciler never lists bucket keys or versions. A live
publisher cannot resume once cleanup is claimed, and a completed GC cannot be
crossed by a publisher that started earlier because that publisher's intent was
already visible to the GC candidate transaction.

Child 3 defines typed seams for:

- enumerate exact Blob key/version/hash/size;
- create/release bounded pins for a manifest owner;
- verify restored object bytes against an expected version/hash;
- enumerate and purge attachment-owned processor/upload partials;
- report deletion replay contract version.

These are dependency interfaces and conformance tests, not a global restore controller. Child 11 supplies RecoveryPointManifest signing, source selection, copying, deletion ledger replay, isolated restore workspace and serving gate.

Production record deletion remains closed because the required adapter registry also names later Child 4-10 adapters. Child 3 tests its adapter independently and bootstrap wiring only where it cannot imply the full registry is ready.

## 10. Web boundary

`web/src/lib/types.ts` owns attachment DTOs and adds non-null `attachment_ids` to draft/revision payloads. A route-lazy records attachment API facade owns session creation, local/S3 transport instructions, complete, polling and authorized content fetch. Components/controllers do not call `fetch` directly.

Child 3 provides controlled, reusable primitives for upload queue state, retry/cancel/remove and authorized download/object URL cleanup. They are tested at narrow widths and remain lazy. Child 5 owns the actual editor/material drawer composition, evidence-vs-attachment presentation, Markdown insertion and page focus behavior.

## 11. Failure semantics

| Failure | Required result |
|---|---|
| quota unavailable/exceeded | reject before bytes; text-only or removal revision remains possible |
| local/S3 partial upload | no final digest object; bounded temporary cleanup |
| hash/size/version mismatch | stable rejected/conflict state; never available |
| scanner temporarily unavailable after receive | quarantined, bounded retry, no reference/preview/download |
| required archive scanner unavailable before receive | 503, no accepted bytes/reservation leak |
| processor crash/timeout | job retry or terminal expiry; janitor receipt and zero workspace residue |
| revision transaction failure | no revision ref, no ownership transfer, draft attachment retained |
| permission revoke/deletion reservation | no new stream/complete; at most the chunk whose final assertion already linearized may finish, and no later chunk may start |
| Blob backend unavailable | new material operation fails; existing text-only revision remains possible |
| unknown object/processor/replay contract | fail closed |

## 12. Verification checkpoints

- Checkpoint 1: migration/ACL/domain/quotas and local+MinIO BlobStore conformance.
- Checkpoint 2: upload HTTP, admission corpus, processor crash cleanup, download authorization and stream fencing.
- Checkpoint 3: revision/draft/read/restore round trip, deletion/recovery seams, Web primitives, deploy/static tests and full repository gates.

No checkpoint is complete from code volume or elapsed time. Completion requires its specified behavior and fresh verification evidence.

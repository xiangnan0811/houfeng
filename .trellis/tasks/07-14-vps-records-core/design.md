# 记录、修订、草稿与状态核心设计

## 0. Delivery and migration boundary

`0052_create_records_core.sql` 是 fresh/current development baseline 的下一条 root migration，并由本任务在同一 PR 扩展 current APP ACL fragment。没有旧库 upgrader、`experience_logs` mapping、双写或 cutover。

实施分三个硬检查点：schema/ACL/domain contracts；revision/draft/API；deletion/Web/full acceptance。每个检查点独立提交、验证和报告。Draft PR 在检查点 1 后创建，但 `0052` 直到三个检查点共同闭合且 PR 合并后才成为不可变历史；检查点 2/3 发现 schema 缺口时在同一分支补正，而不是追加伪修复 migration。

## 1. Ownership and module boundaries

| Owner | Files / objects | Responsibility |
|---|---|---|
| Domain | `internal/center/records/` | immutable inputs, registries, validation, source adapters, revision/draft services, transport-neutral errors |
| PostgreSQL | `internal/center/store/records*.go` | pgx transaction, CAS, current projection, draft/checkpoint persistence, fence-aware reads |
| HTTP | `internal/center/http/handlers/records*.go` | versioned DTO mapping, allowlists, status/error mapping; no policy reimplementation |
| Deletion | `internal/center/recorddeletion/` | preview/operation registry and core purge adapter over Child 1 primitives |
| Web contract | `web/src/lib/types.ts`, `web/src/lib/recordsApi.ts` | canonical DTO and lazy-only transport; no Records pages |
| Later children | evidence/material/search/activity/collaboration/import/export adapters | their tables, derived projections, purge receipts and UI |

`record_domain_activities` is the append-only business source written here. The later activity child alone owns canonical `record_activities`, projection generation/watermark, mixed-source pagination and subject activity UI.

## 2. `0052` data model

### 2.1 Authority tables

- `records`: stable `record_id`, `project_id`, `lifecycle`, `current_revision_id`, `lock_version`, query-oriented current projection, `authorization_epoch`, `archived_at`, timestamps. Root projection is rebuildable and never replaces revision authority.
- `record_revisions`: monotonic `revision_no`, full title/Markdown/dialect/type/status/status-group/impact/time/visibility/owner/follow-up/template/authorship/reason/base/hash fields. Insert-only.
- `record_revision_subjects`: ordered primary/related rows with registry version, subject kind, relation role, stable source ID, capture authorization digest/evidence, identity snapshot, nullable live route and tombstone state. One primary per revision; source deletion never cascades.
- `record_revision_tags`: ordered normalized tag values.
- `record_revision_participants`: ordered stable participant IDs and identity snapshots. No participant array exists in `record_revisions`.
- `record_drafts`: author-private mutable complete input, nullable target record/base revision, independent ETag/version, activity/expiry/warning timestamps.
- `record_draft_checkpoints`: bounded immutable draft recovery snapshots. This is the only recovery-point name.
- `record_domain_activities`: append-only, identity-only/allowlisted activity source emitted by committed business changes. It is not the cross-source read projection.
- `record_core_purge_receipts`: operation-scoped, content-free proof that this owner removed its exact surfaces.

### 2.2 Constraints

- IDs are non-empty stable strings under existing project ID conventions; record IDs never reuse.
- `records.current_revision_id` must reference a revision belonging to the same record. `record_revisions(record_id, revision_no)` and `(record_id, canonical_hash)` support monotonic history and no-change detection.
- lifecycle is `active|archived`; type, state and status-group values are checked by domain validation and constrained to known storage grammar without encoding a mutable transition graph in SQL.
- subject ordinal/participant ordinal/tag ordinal are unique per revision. Exactly one primary subject is enforced by a partial unique index plus deferred transaction validation.
- drafts are unique per `(record_id, author_id)` for existing-record editing; new-record drafts remain independently addressable.
- all record-owned rows use explicit controlled cleanup from the core purge adapter. Source-domain tables have no cascade path into records.
- timestamps use UTC database time for locks, ETags, expiry and commits; application-provided business times are normalized to UTC.

## 3. Current APP ACL extension

`internal/center/store/migrate/app_acl_current_contract.go` registers one `AppACLCurrentMigrationFragment` for `0052_create_records_core.sql`.

- Managed objects list every new table, sequence and any new function; PostgreSQL indexes are schema assertions, not ACL grant targets.
- Center runtime receives only the table/sequence privileges required by online record, draft and purge operations.
- Platform admin receives only explicit administrative/readiness/cleanup privileges required by current migration and operational verification.
- Any SECURITY DEFINER function is separately registered with exact identity, kind and hardened configuration; none is added unless SQL constraints cannot express the operation safely.
- Existing frozen R1/R2 manifests are not rewritten. Current-source, exact fragment coverage, convergence, runtime admission and PostgreSQL catalog tests prove the extension.

Every root migration after `0051` must have exactly one current fragment; an empty or partial registration is a build failure.

## 4. Domain contracts and registries

The initial registries are closed/versioned:

```go
type SubjectKind string
const (
	SubjectKindVPS                SubjectKind = "vps"
	SubjectKindMonitoringInstance SubjectKind = "monitoring_instance"
	SubjectKindTarget             SubjectKind = "target"
)

type RelationRole string
const (
	RelationRoleAffected       RelationRole = "affected"
	RelationRoleContext        RelationRole = "context"
	RelationRoleEvidenceSource RelationRole = "evidence_source"
)
```

Each revision has exactly one primary and zero or more related subjects. Registry validation rejects unknown version/kind/role, duplicate `(kind,id,relation)`, cross-project subjects and invalid primary cardinality.

Builtin record types are `troubleshooting|maintenance|migration|provider_communication|billing|important_finding|note`. The registry maps allowed business states to the canonical groups `pending|in_progress|waiting|verification|completed|cancelled`; finding/note have no business state. Completion/cancellation invariants and non-default transitions are validated in domain code. Template `(id,version)` is optional provenance; template application returns suggestions/diff and never mutates an existing body implicitly.

All input slices/maps are defensively copied, strings/IDs are normalized once, Markdown stays opaque UTF-8 at this layer, and canonical hashing covers every revision-authoritative field in deterministic order.

## 5. Source adapters and authorization

```go
type SubjectSourceAdapter interface {
	Kind() SubjectKind
	Resolve(context.Context, recordauth.ActorScope, SubjectReference) (ResolvedSubject, error)
}

type ResolvedSubject struct {
	ProjectID            recordauth.ProjectID
	StableID             string
	IdentitySnapshot     SubjectIdentitySnapshot
	LiveRoute            string
	CaptureAuthorization recordauth.SourceAuthorization
}
```

Production adapters wrap current authoritative repositories:

| Kind | Source | Snapshot / route behavior |
|---|---|---|
| `vps` | `PostgresVPSAssetRepository` | stable VPS ID, safe display name/provider/region/purpose snapshot, `/vps/:id` route |
| `monitoring_instance` | `PostgresMonitoringInstanceRepository` | stable instance ID, safe name/version snapshot, monitoring-instance route |
| `target` | `PostgresTargetRepository` | stable target ID, type/safe display name snapshot, target route |

The client sends kind/stable ID/relation intent only. The adapter loads project and current scope, creates canonical capture evidence, and returns a server-owned snapshot. Save and read authorization evaluates record visibility intersected with every source capture scope and live current scope through `recordauth.Policy`.

When a source delete commit is witnessed, the source-deletion integration nulls the live route/reference and binds the full-witness `authorization_floor_snapshot`. Reads then use the strict tombstoned `SourceAuthorization` union. Missing/unknown/widening evidence, wrong project or unverifiable witness fails closed. Historical display uses the immutable safe snapshot and never reconnects by name.

## 6. Revision transaction

```go
type RevisionParticipant interface {
	Name() string
	ApplyRevision(context.Context, pgx.Tx, RevisionCommitted) error
}

type CreateRevisionCommand struct {
	RecordID       string
	BaseRevisionID string
	LockVersion    uint64
	IdempotencyKey string
	DraftID        string
	Input          CompleteRevisionInput
}
```

The fixed order is:

1. resolve actor, record visibility and all subject source evidence;
2. validate type/status/template/relations and normalize complete input;
3. enter the Child 1 admitted transaction and claim idempotency;
4. check reservation fence, lock root, verify base revision and lock version;
5. detect canonical no-change and return the existing revision without side effects;
6. insert the full revision, subjects, tags and participants;
7. update current pointer/projection and increment lock/authorization epoch;
8. append `record_domain_activities` and invoke registered in-transaction participants, including identity-only outbox;
9. complete idempotency and commit.

No external network call occurs inside the transaction. Participant name order is deterministic and duplicate registration fails at bootstrap. A later search/activity child registers its own transaction participant without changing revision authority.

Restore loads an authorized historical revision, copies its full authoritative fields into a new normalized input and follows the same transaction. Archive/restore only changes lifecycle/current projection and appends a domain activity; it does not rewrite business status.

## 7. Draft and conflict semantics

Draft ETag is independent of record lock version. `PATCH` requires exact `If-Match`; a stale ETag returns `409 draft_conflict` with current server draft metadata and merge inputs. A draft also stores `base_revision_id`; publishing or reopening against a newer current revision returns `409 record_revision_conflict`, preserving the draft.

On actual content change, the store creates at most one `record_draft_checkpoints` row per five-minute bucket, keeps the latest 20, and deletes checkpoints older than seven days. Draft expiry defaults to 90 days of inactivity and exposes a warning boundary seven days before expiry. Publish/discard/revocation/permanent deletion cleans the server draft through explicit paths. Draft operations never write domain activities, outbox, search or notification rows.

## 8. HTTP contract

This task implements parent design §19.1 endpoints. Static paths are registered before `:id`; session middleware and actor scope are mandatory. Handlers map transport DTOs to domain inputs, never trust project/actor/snapshot fields, and return explicit allowlisted response structs.

- `GET/POST /api/records`, current/history/revision/restore/archive endpoints;
- `GET/POST/PATCH/DELETE /api/record-drafts` endpoints;
- permanent-delete preview/execute/status endpoints, but readiness controls capability exposure.

`If-Match` is mandatory for draft PATCH and `Idempotency-Key` for formal mutation. External denial/not-found is 404. Validation, conflict, size/quota and unavailable dependencies map to stable 400/409/413/422/503 codes from the parent contract. Response bodies never serialize domain/store structs wholesale.

Feature default-off prevents route capability exposure and preserves the legacy VPS experience/timeline behavior. It is a development rollout control, not a legacy data compatibility promise.

## 9. Permanent deletion

`recorddeletion.Service` composes Child 1 reservation/fence/ledger/witness primitives with a closed adapter registry. Each adapter declares stable name, owned surfaces, readiness and preview/purge/verify operations.

The core adapter owns only the tables in §2 plus current projection/cache keys it creates. It emits a content-free `record_core_purge_receipts` digest after verified absence. It does not claim evidence, attachment, search, activity read projection, collaboration, export/import, browser buffer or later processor surfaces.

Production capability readiness requires this exact stable adapter-name set: `record_core`, `record_attachments`, `record_evidence`, `record_markdown_client`, `record_search`, `record_activity_projection`, `record_comparison`, `record_collaboration`, `record_portability`. The names correspond to Child 2 through Child 10 ownership and prevent an empty yet unknown surface from being treated as safe. Until later children register and prove every adapter healthy, preview returns `deletion_safety_unavailable`, execute cannot establish a deletion token, and the UI capability remains false. Unit/integration tests may use a complete in-memory registry fixture to exercise orchestration; production bootstrap remains closed.

Preview binds actor/project/capability digest, record version/current revision, dependency graph, adapter snapshot and ledger/witness state. Execute reauthorizes and recomputes before reservation. Unknown commit/outcome keeps the provisional fence; purge begins only after witnessed delete commit. A witnessed `attempt_not_committed` releases the reservation and permanently resolves the same idempotency key without deleting content. Reservation and permanent fence checks precede cache/store reads and final writes.

## 10. Web transport boundary

Canonical versioned DTOs are appended to `web/src/lib/types.ts`. `web/src/lib/recordsApi.ts` only type-imports these DTOs and delegates to shared `apiRequest` helpers; it does not call raw `fetch` and it is not imported by `web/src/lib/api.ts` or any eager shell module. The existing `withQuery` implementation moves from `api.ts` to `apiRequest.ts` so both façades share one query encoder without importing each other. `ApiError` gains allowlisted `code`, field errors and recovery payload while preserving existing status/message behavior.

Contract tests fix paths, methods, headers, normalized query/cursor behavior and stable error recovery shapes. Because Child 2 creates no Records page, the fresh production bundle proves the unconsumed transport is absent from all current chunks; a synthetic build fixture proves a lazy-only consumer isolates it from the entry chunk. Complete Records pages, Markdown editor and user navigation remain later work.

## 11. Rollback and verification

- Before merge, rollback is branch/PR based; `0052` may be corrected in place because no deployment consumes it.
- After merge, a development environment returning to code without `0052` rebuilds its database; no down migration or legacy binary compatibility path is added.
- Each checkpoint runs focused tests, `git diff --check` and its migration/auth or Web-specific gates. The final checkpoint runs real PostgreSQL fresh/repeat/current admission, race tests, `make verify-go`, Node 22 `make verify-web`, Trellis check and required Draft PR CI.

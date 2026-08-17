# Record Collaboration Design

## 1. Boundary and dependency direction

Collaboration enriches Records but does not own the Record document. Core
publishes revision/authorization interfaces; Collaboration registers
participants and exposes action/comment/watch/inbox services. Core does not
import Collaboration.

Existing incident notification tables/transports are not reused as the
Collaboration data model. Network transports may be adapted behind the new
permission-safe delivery interface.

The implementation reuses `records.RevisionParticipant`, `recordplatform`
admission/idempotency/identity-only outbox/leases, `recordauth`, and the existing
record deletion registry. It may extend closed collaboration event enums and
typed repository methods, but it does not introduce generic SQL callbacks,
network access inside revision transactions, a second outbox, a second auth
policy, or a second deletion orchestrator.

Child 9 emits only normalized filter facts for Child 6, typed activity facts for
Child 7, typed portability providers for Child 10, and exact deletion/backup/
restore adapters for Child 11. Those consumers own their root migrations,
projections, jobs, pages, and aggregate orchestration.

### 1.1 Comment-safe Markdown boundary

Child 9 owns `comment_markdown/v1`, a deliberately smaller shared rendering
contract than the Records document dialect. The canonical source is bounded
UTF-8 Markdown; server and Web decoders share one golden/hostile corpus and emit
only a closed render model. Its exact node registry contains paragraph/text,
line break, emphasis, strong, strikethrough, inline/fenced code,
ordered/unordered list, and link. Links accept bounded canonical `http` or
`https` URLs only and reject userinfo; mentions are separate stable user-ID
relations, never parsed from link/display text. Source must contain 1–16,384
UTF-8 bytes; the render model permits at most 512 nodes, nesting depth 8, and a
2,048-byte serialized link. Raw HTML, images, active content, tables, headings,
footnotes, task-list promotion, evidence/attachment references, invalid UTF-8,
and inputs exceeding those bounds fail with 422
`invalid_comment_markdown`. Nothing falls back to arbitrary HTML or a generic
JSON/Markdown renderer. Child 5 imports and extends this contract for full
documents; it does not fork or reinterpret historical comments.

## 2. Migration 0055

`0055_create_record_collaboration.sql` creates:

- `record_actions` and `record_action_events`;
- `record_comments`, `record_comment_revisions`, and reply/mention relations;
- `record_followers` with source and explicit preference;
- `record_notifications` and per-user read state;
- `record_notification_deliveries` and scoped binding references;
- bounded delivery attempt/retry state and minimal purge receipts.

All collaboration commands use current `recordplatform` idempotency keys,
request/result fingerprints, and owner leases. Replay resolves the typed
collaboration resource identity from the content-free result fingerprint; no
response body or protected content enters foundation rows. `0055` stores only
collaboration state and typed recipient/delivery facts needed beyond the
foundation primitives and must not clone idempotency, lease, authorization, or
outbox tables under collaboration names.

The schema separates content from long-lived minimal audit. Comment deletion has
a database-enforced one-way redaction operation: content/render/cache/hash may
become null only with a versioned tombstone; it cannot be restored.

The `0055` current APP ACL fragment lists all runtime/admin objects and grants.
No Search object exists in or is referenced by this migration.

## 3. Revision participant

`CollaborationRevisionParticipant` validates owner/participants against project
membership and the post-save authorization floor, then updates the current
collaboration projection, follower sources, activity, and outbox within the
Core revision transaction.

Membership is read from a narrow injected authoritative source using the
caller-owned `pgx.Tx`. Because current `recordauth` has exactly the `default`
project and current `users` has no disabled/soft-delete state, v1 accepts only a
present local user whose persisted role is exactly `auth.RoleAdmin`. Missing
rows, malformed IDs, any other role, and read errors fail closed. This is an
assignment-existence boundary, not a second resource policy; record access-group
visibility alone is insufficient. The participant also checks the current
deletion reservation/fence epoch before any projection or recipient fact is
written.

No separate PATCH mutates owner/participant/follow-up.

## 4. Actions

Actions contain record, text, status, assignee, due/completed time, optional
revision subject identity, version, and actor/time. Commands use
`Idempotency-Key` and `If-Match`. Every transition appends an action event and
domain activity. Delete is represented by a reviewed cancel/redaction policy,
not a silent row removal.

## 5. Comments and mentions

Comments use a bounded Markdown subset and flat reply context. Each edit appends
an immutable revision. Delete appends a tombstone then atomically clears content
from current and historical revisions/caches.

Mentions resolve stable user IDs, validate project membership/read permission,
and produce recipient facts only. User display names are presentation data, not
authorization identity.

## 6. Watches, inbox, and delivery

Follower sources include author/owner/participant/comment/mention/action and
manual. Explicit preference can suppress optional automatic updates but not a
new direct assignment/mention addressed to the user.

Notification rendering happens at read/send time after policy evaluation.
External bindings are explicitly scoped by project/user/channel and default
disabled. Outbox and delivery rows contain typed event references/reason codes,
not rendered Record/comment/evidence content.

Retries are bounded and idempotent. Business transactions commit independently
of an unavailable external provider.

The production default has no external provider binding and remains fully
valid. If an existing Telegram/Feishu transport is adapted, the adapter receives
only an allowlisted minimal summary/link after fresh authorization, fence, and
binding checks; provider-specific data never enters the collaboration domain.

## 7. API and Web

Nested APIs authorize through the parent Record and return the same safe 404 for
missing/unauthorized resources. Stable 409/422/503 codes distinguish CAS,
validation, and dependency failure.

Lazy Web components provide owner/participant/follow-up editing inside the
revision workspace, action list/editor, comments, watch control, and inbox.
Eager shell code may consume only a minimal unread-count endpoint; it does not
import the Records transport facade.

Child 9 proves these components independently. Child 5 owns their final mounting
and focus/layout integration in the Records read/edit workspace.

## 8. Adapters and retention

Collaboration publishes typed Activity and Portability providers plus deletion/
backup/restore adapters. Comment tombstones and minimal delivery audit have
bounded allowlists/retention. Permanent Record deletion removes logical
collaboration identity/content and cancels pending delivery; already delivered
external copies are disclosed, not claimed recalled.

Every online mutation, inbox render, worker claim, send and adapter operation
binds the existing deletion reservation/fence epoch. Stale epochs, source or
record revocation, unknown provider outcomes, and missing adapter dependencies
fail closed without retaining or emitting protected content.

## 9. Compatibility and rollback

The database follows the current development baseline and may be rebuilt.
Disabling Collaboration stops workers/routes and leaves `0055` rows inert.
Existing incident notification behavior remains separate. No down migration or
legacy experience compatibility is required.

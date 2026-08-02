# Record Collaboration Design

## 1. Boundary and dependency direction

Collaboration enriches Records but does not own the Record document. Core
publishes revision/authorization interfaces; Collaboration registers
participants and exposes action/comment/watch/inbox services. Core does not
import Collaboration.

Existing incident notification tables/transports are not reused as the
Collaboration data model. Network transports may be adapted behind the new
permission-safe delivery interface.

## 2. Migration 0055

`0055_create_record_collaboration.sql` creates:

- `record_actions` and `record_action_events`;
- `record_comments`, `record_comment_revisions`, and reply/mention relations;
- `record_followers` with source and explicit preference;
- `record_notifications` and per-user read state;
- `record_notification_deliveries` and scoped binding references;
- command idempotency/fingerprint and minimal purge receipts.

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

## 7. API and Web

Nested APIs authorize through the parent Record and return the same safe 404 for
missing/unauthorized resources. Stable 409/422/503 codes distinguish CAS,
validation, and dependency failure.

Lazy Web components provide owner/participant/follow-up editing inside the
revision workspace, action list/editor, comments, watch control, and inbox.
Eager shell code may consume only a minimal unread-count endpoint; it does not
import the Records transport facade.

## 8. Adapters and retention

Collaboration publishes typed Activity and Portability providers plus deletion/
backup/restore adapters. Comment tombstones and minimal delivery audit have
bounded allowlists/retention. Permanent Record deletion removes logical
collaboration identity/content and cancels pending delivery; already delivered
external copies are disclosed, not claimed recalled.

## 9. Compatibility and rollback

The database follows the current development baseline and may be rebuilt.
Disabling Collaboration stops workers/routes and leaves `0055` rows inert.
Existing incident notification behavior remains separate. No down migration or
legacy experience compatibility is required.

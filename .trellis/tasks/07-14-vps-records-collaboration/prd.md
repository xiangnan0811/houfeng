# Record Ownership, Actions, Comments, Watches, and Notifications

## Goal

Deliver lightweight collaboration around authoritative Records revisions:
owners/participants/follow-up, structured actions, safe comments, watches,
inbox, and permission-safe notifications.

## 2026-08-02 Development Rebaseline

This child owns `0055_create_record_collaboration.sql`. The previous plan that
reserved `0055` for Search and applied Collaboration as `0056` out of order is
superseded. Only fresh/current development databases and exact repeat startup
are supported.

## Dependencies

Platform Foundation and Records Core are direct functional dependencies.
Attachments and Evidence should already be merged under the parent's default
sequential execution, but Collaboration does not import their storage packages.

## Requirements

- Owner, participants, follow-up time, visibility, and related subjects remain
  fields of the complete immutable Records revision. Collaboration registers a
  revision participant; it does not add a mutable bypass endpoint.
- Assignment validates current project membership plus post-save record/source
  authorization in the same transaction. Invalid assignment rolls back the
  revision.
- `0055` creates action, comment/revision/tombstone, follower, inbox/notification,
  delivery-attempt, idempotency, worker, and minimal purge receipt state.
- Register `0055` objects/privileges in the current APP ACL fragment.
- Actions have independent CAS/idempotent state/history and domain activity.
  They never automatically change record status or an external business object.
- Comments use the shared approved safe Markdown subset. Edit history is
  append-only; delete performs one-way content redaction while retaining a
  minimal reply/audit tombstone.
- Watches distinguish automatic sources from explicit user preference.
  Unwatching optional updates cannot suppress mandatory direct assignment/
  mention/security-relevant notifications.
- Recipient calculation is deterministic, deduplicated, authorization-aware,
  and rechecked before inbox rendering, every delivery attempt, and deep-link
  open.
- In-app inbox is required. External Telegram/Feishu delivery is available only
  through explicit scoped bindings and sends a safe minimal message/link; the
  feature remains valid when no external binding is configured.
- Outbox stores event/recipient facts, not pre-rendered comment/record content.
  Permission revoke/deletion cancels future delivery and purges content caches.
- Collaboration supplies activity, export/import, permanent-delete, backup, and
  restore adapters for later children.
- Draft autosave and the actor's own low-value actions do not create notification
  noise.

## Acceptance Criteria

- [ ] `0055` fresh/repeat migration and current APP ACL/admission tests pass.
- [ ] Owner/participant/follow-up changes occur only through a complete revision
  and roll back on membership, visibility, source-floor, participant, activity,
  or outbox failure.
- [ ] Action create/update/complete/cancel/reopen is CAS/idempotent, auditable,
  permission-safe, and does not mutate the record revision or business object.
- [ ] Comment create/edit/delete/reply/mention is CAS/idempotent; XSS is blocked;
  delete makes all current/history/render/cache/hash content irrecoverable from
  normal collaboration APIs while preserving a minimal tombstone.
- [ ] Recipient/watcher rules deduplicate self/automatic/manual/mandatory sources
  and re-evaluate authorization on inbox/delivery/deep link.
- [ ] Permission revoke, record delete, source delete, target unbind, retry, and
  worker crash yield no new restricted external/inbox content.
- [ ] External delivery disabled/unconfigured does not fail the business
  transaction; enabled scoped bindings use safe summaries and bounded retries.
- [ ] Records filters can query owner/participant/follow-up/action without
  duplicate records and with same-field OR/cross-field AND semantics.
- [ ] Activity/export/import/deletion/backup/restore adapters preserve allowed
  history, remove deleted content, and fail closed when missing.
- [ ] Inbox, actions, comments, owner/follow-up components cover loading/empty/
  error/revoked/deleted, keyboard, desktop/390px, and accessibility.
- [ ] Focused race/real PostgreSQL/worker/Web tests, full gates,
  `git diff --check`, and `trellis-check` pass.

## Out of Scope

- General per-user ACL editor, public sharing, chat, boards/Sprints, time
  tracking, arbitrary dependencies, unlimited nested threads, or custom fields.
- Making Markdown checklist items into actions automatically.
- Executing business commands from actions/comments.
- Requiring an external notification provider.
- Legacy `experience_logs` compatibility or old database upgrade.
- Staging/release cutover.

## Execution Gate

Keep `planning` until Foundation/Core are accepted, the parent's prior
migration-owning children are merged, and `0055` is free. Reconcile Core revision
participant APIs before start.

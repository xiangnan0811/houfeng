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

## 2026-08-17 Current-Main Rebaseline

Planning targets protected `origin/main` at `2e6aa62a` (`v0.66.0`). Children
1–4 are delivered and `0055` is free. The product scope remains owners,
participants, follow-up, actions, comments, watches, inbox, and safe
notifications, but implementation must reuse the current Core revision,
`recordplatform`, `recordauth`, and deletion contracts. Child 9 publishes typed
facts/adapters for later children and does not implement their projections,
jobs, pages, or orchestration.

## Dependencies

Platform Foundation and Records Core are direct functional dependencies.
Attachments and Evidence should already be merged under the parent's default
sequential execution, but Collaboration does not import their storage packages.
All four dependencies are now on protected main. Child 5 remains blocked until
this child is independently reviewed and merged.

## Requirements

- Owner, participants, follow-up time, visibility, and related subjects remain
  fields of the complete immutable Records revision. Collaboration registers a
  revision participant; it does not add a mutable bypass endpoint.
- Assignment validates current project membership plus post-save record/source
  authorization in the same transaction. Invalid assignment rolls back the
  revision. In the current single-project v1 model, a member is an existing
  `users` row whose role is exactly `admin`; a narrow transaction-bound reader
  performs that lookup. Missing users, other/unknown roles, malformed IDs, and
  unavailable reads fail closed. Access-group visibility is not proof of
  membership.
- `0055` creates action, comment/revision/tombstone, follower, inbox/notification,
  delivery-attempt/retry, and minimal purge receipt state.
  Commands reuse the existing `recordplatform` idempotency rows, owner fences,
  request fingerprints, and content-free result fingerprints; `0055` creates no
  second idempotency table.
- Register `0055` objects/privileges in the current APP ACL fragment.
- Actions have independent CAS/idempotent state/history and domain activity.
  They never automatically change record status or an external business object.
- Child 9 owns a minimal, versioned comment-safe Markdown contract with matching
  server/Web renderers and one shared hostile/golden corpus. It excludes raw
  HTML, active content, attachment/evidence references, document headings,
  images, footnotes, tables, and task-list promotion. The only accepted v1
  nodes are paragraphs/text, line breaks, emphasis, strong, strikethrough,
  inline/fenced code, ordered/unordered lists, and bounded `http`/`https` links
  without URL userinfo. Canonical source is 1–16,384 UTF-8 bytes; the render
  model is limited to 512 nodes and depth 8, and link serialization to 2,048
  bytes. Unsupported nodes, unsafe URLs, excessive source/node/depth/link size,
  and invalid UTF-8 receive 422 `invalid_comment_markdown`; they are never
  downgraded to arbitrary HTML or a generic renderer. Child 5 reuses this
  contract and extends the full document dialect rather than replacing comment
  rendering.
  Comment edit history is append-only; delete performs one-way content
  redaction while retaining a minimal reply/audit tombstone.
- Watches distinguish automatic sources from explicit user preference.
  Unwatching optional updates cannot suppress mandatory direct assignment/
  mention/security-relevant notifications.
- Recipient calculation is deterministic, deduplicated, authorization-aware,
  and rechecked before inbox rendering, every delivery attempt, and deep-link
  open.
- In-app inbox is required. External Telegram/Feishu delivery is available only
  through explicit scoped bindings and sends a safe minimal message/link; the
  feature remains valid when no external binding is configured.
- Collaboration extends the existing identity-only `recordplatform` outbox with
  closed event/recipient facts; it does not create a generic second outbox or
  store pre-rendered comment/record content. Permission revoke/deletion cancels
  future delivery and purges content caches.
- Comments, actions, recipient calculation, inbox reads, delivery attempts and
  adapters bind the current record deletion reservation/fence epoch. A stale or
  unavailable fence fails closed before content is rendered, persisted, or sent.
- Collaboration supplies normalized filter facts, typed activity facts, typed
  portability providers, and exact permanent-delete/backup/restore adapters for
  later children. It creates no Search/Activity/Portability root objects and no
  aggregate backup/restore runtime.
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
- [ ] `comment_markdown/v1` accepts only its exact node/link registry and rejects
  every document-only, active, over-limit, invalid-UTF-8, or unsafe-URL input
  with the same shared server/Web hostile corpus and exact 422
  `invalid_comment_markdown` mapping.
- [ ] Recipient/watcher rules deduplicate self/automatic/manual/mandatory sources
  and re-evaluate authorization on inbox/delivery/deep link.
- [ ] Permission revoke, record delete, source delete, target unbind, retry, and
  worker crash yield no new restricted external/inbox content.
- [ ] External delivery disabled/unconfigured does not fail the business
  transaction; enabled scoped bindings use safe summaries and bounded retries.
- [ ] Normalized owner/participant/follow-up/action filter facts are deterministic
  and deduplicated for Child 6; Child 9 creates no Search table or search page.
- [ ] Typed activity and portability providers plus deletion/backup/restore
  adapters preserve allowed history, remove deleted content, bind reservation
  epochs, and fail closed when missing; downstream projections/jobs/orchestration
  remain in Children 7/10/11.
- [ ] Inbox, actions, comments, owner/follow-up components cover loading/empty/
  error/revoked/deleted, keyboard, desktop/390px, and accessibility.
- [ ] Focused race/real PostgreSQL/worker/Web tests, full gates,
  `git diff --check`, and `trellis-check` pass.

## Out of Scope

- General per-user ACL editor, public sharing, chat, boards/Sprints, time
  tracking, arbitrary dependencies, unlimited nested threads, or custom fields.
- Making Markdown checklist items into actions automatically.
- Full document Markdown, evidence/attachment reference syntax, editor routes,
  revision diff, and Records workspace integration; these belong to Child 5.
- Executing business commands from actions/comments.
- Requiring an external notification provider.
- Legacy `experience_logs` compatibility or old database upgrade.
- Staging/release cutover.

## Execution Gate

Keep `planning` until the 2026-08-17 current-main artifacts and curated context
manifests pass Trellis validation and the user explicitly approves the final
planning summary. Production write paths remain disabled/fail-closed while the
real deployment-membership gate is absent; Child 9 must not fabricate it.

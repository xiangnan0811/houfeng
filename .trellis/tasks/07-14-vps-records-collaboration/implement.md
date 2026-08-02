# Record Collaboration Implementation Plan

> **For agentic workers:** Use the reviewed execution mode and bounded
> RED -> verified RED -> minimal GREEN slices.

**Goal:** Deliver revision-integrated ownership/follow-up, actions, comments,
watches, inbox, and optional permission-safe external notifications.

**Architecture:** `0055` stores collaboration state; a Core revision participant
owns revision-coupled fields; action/comment services append independent
history/activity/outbox; recipient rendering/delivery rechecks policy.

**Tech Stack:** Go/pgx/PostgreSQL, existing notification transports behind a new
adapter, React/TypeScript, Vitest/Playwright/Axe.

---

## Preconditions

- [ ] Foundation/Core and earlier planned dependencies are accepted on protected
  main.
- [ ] Run `trellis-before-dev` for backend database/auth/http and Web
  component/state/security guidance.
- [ ] Confirm `0055` is free and current APP ACL fragments are available.
- [ ] Reconcile the actual Core RevisionParticipant/activity/outbox/auth APIs.
- [ ] Baseline Go/Web/notification tests with Node 22.

## Task 1: 0055 schema, types, and ACL fragment

- [ ] Write RED migration/domain tests for all state/history/idempotency/
  retention/redaction constraints and no source cascade.
- [ ] Implement `0055`, immutable values/state machines, and exact current APP
  managed objects/privileges.
- [ ] Run fresh/repeat migration, current admission, and real PostgreSQL tests.

## Task 2: Revision collaboration participant

- [ ] Test owner/participant membership, post-save visibility/source floor,
  follow-up, restore-old-revision, follower sources, activity/outbox order, and
  rollback at each failure.
- [ ] Implement and register `CollaborationRevisionParticipant`.
- [ ] Expose normalized filter facts for Search without creating Search tables.

## Task 3: Action service

- [ ] Test command idempotency/fingerprint, `If-Match`, permissions, transitions,
  assignee/due filters, activity/outbox, and races.
- [ ] Implement store/service/handlers and response allowlists.
- [ ] Prove actions do not update Record/business status implicitly.

## Task 4: Comment, reply, mention, and redaction

- [ ] Run shared Markdown/XSS corpus for create/edit/reply/mention/render/export.
- [ ] Test idempotency/CAS, author/moderator policy, reply integrity, mention
  auth, tombstone, and database one-way redaction against stale writers.
- [ ] Implement comment service/handlers and safe rendering.

## Task 5: Watches, recipient decisions, and inbox

- [ ] Test automatic/manual/mandatory follower matrix, self-noise, grouping,
  unread state, revoke/delete, and deterministic recipient deduplication.
- [ ] Implement recipient policy, inbox projection/query/read state, and worker.
- [ ] Recheck policy on each inbox read and deep-link target.

## Task 6: Optional external delivery

- [ ] Define scoped transport binding and content-safe render contract.
- [ ] Test disabled/unconfigured/success/retry/permanent-failure/revoke/delete/
  unbind paths and no business rollback.
- [ ] Adapt existing transports without reusing incident data semantics.

## Task 7: Web components and adapters

- [ ] Build revision owner/follow-up controls, actions, comments, watch control,
  inbox, and minimal unread badge through correct lazy/eager boundaries.
- [ ] Test loading/empty/error/revoked/deleted, desktop/390px, keyboard/focus,
  touch, Axe, and bundle/CSS budgets.
- [ ] Register Activity, Portability, deletion, backup, and restore adapters with
  focused conformance tests.

## Task 8: Quality and handoff

- [ ] Run focused race/real PostgreSQL/worker/security and Web/browser tests.
- [ ] Run full Go/Web/browser gates, `git diff --check`, and `trellis-check`.
- [ ] Update implemented collaboration/notification specs.
- [ ] Merge through protected main and archive before Search/Activity/Portability
  final integration.

## Rollback

Disable collaboration routes/workers/external delivery; keep `0055` history
read-only. Never reverse comment redaction or run a down migration. Rebuild the
development database when returning to code without `0055`.

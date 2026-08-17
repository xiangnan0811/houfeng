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

- [x] Foundation/Core/Attachments/Evidence are accepted on protected main at
  the `2e6aa62a` / `v0.66.0` planning baseline.
- [x] Run `trellis-before-dev` for backend database/auth/http and Web
  component/state/security guidance.
- [x] Confirm `0055` is free and current APP ACL fragments are available.
- [x] Reconcile the actual Core `RevisionParticipant`, `recordplatform`
  admission/idempotency/outbox/lease, `recordauth`, and deletion registry APIs;
  findings are recorded in `research/current-main-rebaseline-2026-08-17.md`.
- [x] Baseline Go/Web/notification tests with Node 22.

## Task 1: 0055 schema, types, and ACL fragment

- [x] Write RED migration/domain tests for all state/history/retention/redaction
  constraints, deletion-fence binding, no source cascade, and the explicit
  absence of duplicate idempotency/outbox/lease/authorization tables.
- [x] Implement `0055`, immutable values/state machines, and exact current APP
  managed objects/privileges without cloning existing foundation idempotency,
  outbox, lease, authorization, or deletion state.
- [x] Run fresh/repeat migration, current admission, and real PostgreSQL tests.

## Task 2: Revision collaboration participant

- [x] Test the exact `default`-project membership matrix (present admin only;
  missing/malformed/other-role/unavailable fail closed), post-save visibility/
  source floor, deletion fence, follow-up, restore-old-revision, follower
  sources, typed activity/outbox order, and rollback at each failure.
- [x] Implement and register `CollaborationRevisionParticipant`.
- [x] Expose normalized filter facts for Search without creating Search tables.

## Task 3: Action service

- [x] Test command idempotency/fingerprint, `If-Match`, permissions, transitions,
  assignee/due filters, activity/outbox, and races.
- [x] Implement store/service/handlers and response allowlists.
- [x] Prove actions do not update Record/business status implicitly.

## Task 4: Comment, reply, mention, and redaction

- [x] Define the exact `comment_markdown/v1` nodes, canonical HTTP(S)-link rule,
  16,384-byte source/512-node/depth-8/2,048-byte-link bounds, and exact 422
  `invalid_comment_markdown`; run one shared server/Web
  Markdown/XSS corpus for create/edit/reply/mention/render/export and prove
  document-only/active/unsafe/invalid inputs cannot enter comments.
- [x] Test idempotency/CAS, author/moderator policy, reply integrity, mention
  auth, tombstone, and database one-way redaction against stale writers.
- [x] Implement comment service/handlers and safe rendering.

## Task 5: Watches, recipient decisions, and inbox

- [x] Test automatic/manual/mandatory follower matrix, self-noise, grouping,
  unread state, revoke/delete, and deterministic recipient deduplication.
- [x] Implement recipient policy, inbox projection/query/read state, and worker.
- [x] Recheck policy on each inbox read and deep-link target.

## Task 6: Optional external delivery

- [x] Define scoped transport binding and content-safe render contract.
- [x] Test disabled/unconfigured/success/retry/permanent-failure/revoke/delete/
  unbind paths and no business rollback.
- [x] Keep no-binding production valid and disabled by default; adapt an existing
  transport only behind the scoped interface without reusing incident data
  semantics or making provider availability a completion dependency.

## Task 7: Web components and adapters

- [x] Build revision owner/follow-up controls, actions, comments, watch control,
  inbox, and minimal unread badge through correct lazy/eager boundaries.
- [x] Test loading/empty/error/revoked/deleted, desktop/390px, keyboard/focus,
  touch, Axe, and bundle/CSS budgets.
- [x] Publish normalized filter facts, typed Activity/Portability providers, and
  exact deletion/backup/restore adapters with focused conformance tests; create
  no Child 6/7/10/11 root tables, projections, jobs, pages, or orchestration.

## Task 8: Quality and handoff

- [x] Run focused race/real PostgreSQL/worker/security and Web/browser tests.
- [x] Run full Go/Web/browser gates, `git diff --check`, and `trellis-check`.
- [x] Update implemented collaboration/notification specs.
- [ ] Merge through protected main and archive before Search/Activity/Portability
  final integration.

## Planning review gate

- [x] Current-main code/spec/task audit completed without product-code edits.
- [x] Child 9/5 Markdown ownership and downstream contract boundaries are
  explicit with no blocking open product question.
- [x] Curated `implement.jsonl` and `check.jsonl` validate without injection
  truncation warnings.
- [x] Present the final planning summary and stop. Run `task.py start` only after
  a subsequent explicit user approval of these updated artifacts.

## Rollback

Disable collaboration routes/workers/external delivery; keep `0055` history
read-only. Never reverse comment redaction or run a down migration. Rebuild the
development database when returning to code without `0055`.

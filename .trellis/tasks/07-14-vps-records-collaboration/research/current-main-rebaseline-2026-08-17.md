# Child 9 current-main rebaseline (2026-08-17)

## Baseline and delivered dependencies

- Planning baseline is protected `origin/main` at
  `2e6aa62a7b40de5835a5f9e670a8b80dbbe81ed4` (`v0.66.0`).
- Children 1–3 are archived. Child 4 Evidence was delivered through PR #408,
  protected-main CI, release PR #409, and the published `v0.66.0` artifacts.
- Root migrations on this baseline end at
  `0054_create_record_evidence.sql`; `0055_create_record_collaboration.sql` is
  free and remains owned by Child 9.
- Child 9 is the next implementation child. Child 5 Markdown starts only after
  Child 9 is independently reviewed and merged to protected main.

## Existing contracts Child 9 must reuse

- `records.CompleteRevisionInput` already owns immutable owner, participant,
  follow-up, visibility, related-subject, attachment, evidence, author, and
  Markdown fields. Child 9 must not create mutable bypass endpoints for the
  revision-owned fields.
- `records.RevisionParticipant` and its deterministic registry already provide
  the caller-owned `pgx.Tx` extension point. Collaboration registers one
  participant and performs no network call inside that transaction.
- `recordplatform` already owns transaction admission, request idempotency,
  identity-only outbox events, owner leases, and claims. Collaboration extends
  closed event/fact enums where required; it does not build a second generic
  outbox or idempotency system.
- `recordauth` is the sole resource authorization policy, with production actor
  group hydration in `store.PostgresRecordAuthorizationRepository`.
  Assignment still needs a narrow, injected project-membership reader because
  group visibility is not proof that an arbitrary assignee is a current member.
  Current v1 has only `recordauth.ProjectIDDefault`; `users` has no project,
  disabled, or soft-delete columns, and authenticated actors are admitted only
  when their persisted role is exactly `admin`. Therefore the v1 assignment
  authority is a transaction-bound lookup of a present `users` row with exact
  role `admin`; missing/malformed/other-role/unavailable cases fail closed.
- The permanent-deletion registry already reserves
  `record_collaboration`. Collaboration must implement the exact adapter,
  reservation/fence, receipt, recovery, and retry contracts instead of creating
  a parallel deletion orchestrator.

## Scope corrections from the 2026-08-02 plan

1. Child 9 owns a minimal versioned comment-safe Markdown contract, server/Web
   renderers, and shared hostile/golden corpus. It is deliberately smaller than
   the document dialect. Child 5 reuses this contract and extends the full
   document renderer/editor; it does not replace the comment renderer.
   The exact v1 accepted nodes are paragraph/text, line break, emphasis, strong,
   strikethrough, inline/fenced code, ordered/unordered list, and bounded
   canonical HTTP(S) link without userinfo. Raw HTML, images, headings, tables,
   task lists, footnotes, active/unsafe URLs, attachment/evidence refs, invalid
   UTF-8, and over-limit input fail with 422 `invalid_comment_markdown`; there is
   no fallback. Bounds are 1–16,384 source bytes, 512 render nodes, depth 8, and
   2,048 serialized link bytes.
2. Child 9 emits normalized collaboration filter facts for Child 6 and typed
   activity facts for Child 7. It creates neither `0056` search objects nor
   `0057` activity projections/pages.
3. Child 9 publishes typed portability and backup/restore/deletion adapters for
   later registries. It does not build Child 10 export/import jobs or Child 11
   backup/restore orchestration.
4. In-app inbox is required. External Telegram/Feishu delivery remains optional
   and disabled by default; no provider configuration is a valid production
   state. If a current scoped binding is adapted, every send/retry reauthorizes
   and carries only an allowlisted minimal summary/link.
5. Lazy collaboration components and an inbox surface belong to Child 9. Child 5
   owns their integration into the Records read/edit workspace. Only a bounded
   unread-count seam may enter the eager shell.
6. Comments, actions, recipient calculation, inbox reads, delivery claims, and
   adapters must bind the existing record deletion reservation/fence epoch.
   Permission/source deletion, revoke, unbind, stale workers, and unknown
   dependency state fail closed without retaining or emitting content.

## Deferred production authority

- The real deployment-membership `store.AdmissionGate`, witnessed
  source-deletion tombstone authority, and integrity-valid external evidence
  quarantine remain outside Child 9. Child 10 owns those production authority
  contracts; Child 11 owns aggregate composition/readiness and end-to-end
  enablement evidence.
- Child 10 must implement that gate against the existing `0051`
  `deployment_membership` and `deployment_contract_state` authority. Its `0058`
  migration must not create a duplicate deployment-membership authority table.
- Until that authority is injected, Child 9 write paths and workers must remain
  disabled or stably fail closed. Production code must not use an allow-all,
  `AdmissionGateFunc`, typed-nil bypass, or locally fabricated tombstone.

## Planning exit

- Reconcile `prd.md`, `design.md`, `implement.md`, `implement.jsonl`, and
  `check.jsonl` against this baseline.
- Present the final planning summary and stop before `task.py start`. A later
  user message must explicitly approve the updated artifacts before product
  implementation begins.

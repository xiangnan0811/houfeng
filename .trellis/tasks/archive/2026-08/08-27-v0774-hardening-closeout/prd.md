# v0.77.4 remaining hardening seams

## Problem

`v0.77.3` closed the previous P1 gaps (VPS-scoped subscription idempotency, modern CAS recovery, IP Quality default-off missing anomaly, cancellation deep links, coded lifecycle errors, closed anomaly severity). Three P2 seams and three P3 contract/UX tails remain. None require rollback of `v0.77.3`, but they still allow duplicate subscriptions, a Legacy CAS dead-end, disabled IP Quality being treated as unavailable, an uncoded readonly 409, internal renewal values in conflict copy, and a TypeScript DTO that is wider than the VPS-scoped request.

## Goal

Close the remaining `v0.77.3` review items so both subscription create doors, both VPS detail surfaces, disabled IP Quality judgement, and the VPS mutation error contract behave as one hardened set.

## In scope

1. Global `POST /api/subscriptions` uses the same idempotency receipt as VPS-scoped create.
2. Legacy facts edit and renewal decision recover from `vps_asset_conflict` the same way modern Overview does (load latest, three-way merge, new ETag, retry).
3. Disabled IP Quality history lookup is best-effort and cannot change Overview health.
4. Ordinary PATCH against a cancelled/archived VPS returns coded `vps_asset_readonly`; the UI refreshes identity and routes to archive when the VPS is terminal.
5. Modern CAS compare rows localize renewal decisions; if latest already matches the local decision, treat it as already done.
6. `CreateVPSSubscriptionInput` matches the VPS-scoped Go request DTO, not the full collection create input.

## Out of scope

- Expanding VPS-scoped create to accept `display_name` / `labels` / `cost_category` / trial/end dates.
- Removing the Legacy VPS detail path.
- Changing IP Quality when it is enabled.
- New subscription PATCH idempotency.
- Multi-user / SaaS authorization changes.

## Constraints

- Reuse `CreateSubscriptionIdempotent`, `subscription_create_idempotency`, and existing CAS merge helpers. Do not add a second receipt table or a second three-way merge.
- Collection POST keeps the full `subscriptions.CreateInput` body. VPS-scoped POST stays the billing-fact DTO with `DisallowUnknownFields()`.
- Disabled IP Quality must remain `not_configured` + `SectionReady`. A failed history query must not emit `source.unavailable.v1` or raise overall status.
- Chinese UI copy; machine values stay in API JSON.
- Tests live next to the code they protect.

## Acceptance criteria

- `POST /api/subscriptions` requires a valid `Idempotency-Key`, calls `CreateSubscriptionIdempotent`, replays the original record for the same key+digest (200), and returns `idempotency_key_reused` for same key+different digest.
- `SubscriptionsPage` reuses the original key after a network/error retry; it rotates the key only when the user actually changes the form or the server returns `idempotency_key_reused`.
- A handler + page test covers: insert succeeded, response lost, retry with the same key yields one subscription.
- Legacy facts 409 → load latest → keep local edits → retry succeeds with the new `updated_at`.
- Legacy renewal 409 → load latest → keep reason/decision → retry succeeds with the new `updated_at`.
- Legacy mutation responses are generation-owned: route switch, unmount, and Drawer close discard late state/navigation effects. A pending PATCH remains mutually exclusive after close/reopen; write locks are scoped by VPS ID and request token so A cannot block or unlock B.
- `disabled + latest summary query error` → IP Quality `ready` / `not_configured` / Overview `healthy`; no `ip_quality` in `JudgementSourcesUnavailable`.
- PATCH readonly races return `{"error":"vps asset readonly","code":"vps_asset_readonly"}`. Frontend shows Chinese copy and navigates to `/archive/:id` when the current identity is cancelled or archived.
- Conflict compare rows show “取消” / “保留”, not `cancel` / `keep`. If latest decision equals the local draft, the UI reports that another operation already completed it and closes the workbench.
- TypeScript cannot express VPS-scoped fields the Go DTO rejects. A contract test fails if the two DTO field sets drift.

## Non-goals / non-claims

Do not describe this as “all hardening fully boxed” until the tests above pass. `v0.77.3` remains deployable; this task is the closeout patch.

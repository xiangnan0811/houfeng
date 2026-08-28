# v0.77.5 remaining hardening seams

## Goal

Close the remaining `v0.77.4` review items so disabled IP Quality cannot change current Overview health, every Legacy write that mutates UI after await is generation-owned, the VPS-scoped subscription DTO contract checks field semantics, and subscription create receipts have an explicit permanent lifecycle. This hardening round is boxed only after implementation gates pass and an independent external review reports no blocking findings.

## Background

`v0.77.4` (`3c94a95`, current `main`) closed global subscription create idempotency, Legacy facts/decision CAS recovery, coded `vps_asset_readonly`, localized conflict compare, already-satisfied convergence, and a narrow VPS-scoped DTO. Four seams remain. None require rollback.

`LoadIPQuality` (`internal/center/store/vps_overview.go:119-141`) returns `not_configured` + `SectionReady` when disabled, but still synchronously calls `GetLatestVPSIPQualitySummary` on the Overview child context to set `ip_quality_disabled_has_history`. Immediate errors are ignored. A query that blocks until `ctx.Done()` can miss the results channel; `timeoutPending` then records `context.DeadlineExceeded` (`internal/center/vpsoverview/service.go:238-241, 292-311`). Production `Total` and `IPQuality` budgets are both `800ms`. Existing tests inject an immediate timeout error, not a deadline-blocking query. Overview UI localizes that reason to “存在历史报告（当前未启用）”. The IP Quality detail page already lists history independently (`IPQualityDashboard`, `GET /api/vps/{vps_id}/ip-quality`).

Legacy `beginVpsWrite` / `mutationIsCurrent` / per-VPS request tokens only wrap facts PATCH and renewal decision PATCH (`LegacyVPSDetail.tsx:338-350, 933, 1015`). Create monitoring, archive, link/unlink, create subscription, extend validity, experience, service, and domain handlers still `setNotice` / `collapseDrawer` / `navigate` after await with no ownership check. Cancellation already uses `cancellationPreviewGenerationRef`, which the route effect also invalidates.

`CreateVPSSubscriptionInput` and `vpsSubscriptionCreateRequest` share `vps_subscription_create_fields.json` as a string-name list. Parsers compare names only; TypeScript strips `?`. Current wire shape: required numbers/bools/strings plus optional billing/renewal fields and optional nullable dates.

`subscription_create_idempotency` (`0061`) has PK + `subscription_id` index and `on delete cascade`. There is no application delete path, no user-facing subscription DELETE, and the retention worker does not cover this table. Archive/cancel of a VPS does not remove subscription rows, so cascade almost never fires. Both create doors already write a receipt per successful create.

`POST /api/subscriptions` without `Idempotency-Key` already returns `400 invalid_idempotency_key`. In-repo callers send the header. Public docs do not yet call this a wire-contract change.

## Requirements

1. **P2-01.** When IP Quality is disabled, Overview does not query history and does not show “存在历史报告（当前未启用）”. `LoadIPQuality` returns immediately after the availability check. History remains on the IP Quality detail page. Current health copy is “未启用”.
2. **P2-02.** Every Legacy async write that sets notice/error, resets draft, closes a drawer, refreshes the current route, or navigates after await is generation-owned through the existing mutation generation + per-VPS request token. Late VPS A results must not mutate VPS B UI. Returning to A refreshes authoritative state.
3. **P3-01.** The shared VPS-scoped subscription create manifest records field semantics (`type`, `required`, `nullable`), not only names. Go and TypeScript contract tests fail on type / requiredness / nullability drift. Do not add an OpenAPI/codegen pipeline.
4. **P3-02.** Create receipts are permanent for the lifetime of the subscription row. Same key+digest always replays while that row exists. If the subscription row is deleted, cascade removes the receipt. Backup restore keeps old keys valid. No janitor, TTL, or new metrics platform. Write this contract into active docs.
5. Public operator-facing docs state that `POST /api/subscriptions` requires `Idempotency-Key`: reuse a stable UUID for retries of the same body; rotate the key when the logical operation changes.
6. Internal implementation/check completion is not an archive condition. Every external-review Critical/Important/Minor finding in the accepted review scope must be recorded, fixed, and independently re-reviewed before this task can be archived.
7. The active remediation tree rooted at `08-27-v0775-vps-detail-hardening` remains part of this task and owns the later VPS detail/subscription contract review rounds. This parent remains `in_progress` while that tree or its external review is unresolved.

## Out of scope

- Changing IP Quality behaviour when it is enabled.
- Adding Overview IP Quality cache, precomputed columns, a second Overview source, or a smaller independent history query.
- Removing the Legacy VPS detail path.
- Subscription PATCH idempotency.
- Generating TypeScript DTOs from OpenAPI / JSON Schema.
- Adding a user-facing subscription DELETE or a receipt janitor.
- Multi-user / SaaS authorization changes.
- Rolling back `v0.77.4`.

## Constraints

- Reuse the existing Legacy mutation generation + per-VPS request token. Do not add a second ownership system for facts/decision.
- Server writes that already left the client are not aborted; only UI side effects are owned.
- Same-VPS dangerous duplicate submits stay locked; different VPS writes may run in parallel.
- Chinese UI copy; machine values stay in API JSON.
- Tests live next to the code they protect.
- Receipt policy stays truthful to single-operator scale. Do not build a generic TTL platform.
- Do not work on local `main`; use a feature branch.
- Do not archive immediately after development or internal quality gates. Wait for the external review verdict and keep the task active whenever that verdict contains blocking findings.

## Acceptance criteria

- Disabled IP Quality Overview: `status = not_configured`, `section = ready`, empty history `reason_code`, `overall = healthy`, no `source.unavailable.v1`. `GetLatestVPSIPQualitySummary` is not called on the disabled path.
- A service-level test whose summary fake blocks on `ctx.Done()` then delays still yields `not_configured` + `ready` + `healthy` with no `source.unavailable.v1` when IP Quality is disabled. Existing leftover-report tests no longer require `ip_quality_disabled_has_history` on Overview.
- Overview UI for disabled IP Quality shows “未启用” and does not show “存在历史报告（当前未启用）”. The IP Quality detail page still lists history.
- Legacy tests cover at least: A create-monitoring pending → switch to B → A completes, no navigate; A archive pending → switch to B → A completes, no archive navigate; A create-service pending → switch to B and open a drawer → A completes, B drawer stays open; A create-subscription pending → switch to B → A replay completes, no A notice on B.
- Go and TypeScript subscription create contract tests fail if `price` becomes string, `auto_renew` becomes string, `renew_at` loses nullability, or a required field such as `note` becomes optional.
- Active docs state that receipts are permanent for the subscription row lifetime, and that `POST /api/subscriptions` requires `Idempotency-Key`.
- The remediation child tree is complete, the latest independent external review reports no blocking findings, and that review evidence is recorded before archive.

## Non-goals / non-claims

`v0.77.4` remains deployable. This task is the closeout patch, not a rollback. Do not describe the hardening round as boxed until the acceptance tests above pass and the independent external-review gate is clear.

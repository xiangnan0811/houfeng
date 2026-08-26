# VPS overview post-v0.77.1 hardening design

## Boundaries

This patch hardens the existing VPS Overview launch surface. It does not
redesign Overview architecture, add lifecycle states, enable IP Quality by
default, introduce message queues, or load the full settings document on every
Overview request.

## Product decision — disabled + historical IP report

Locked:

- Current judgment treats a disabled capability as `not_configured` even when
  a leftover report exists.
- Leftover `status` / `risk_level` / `stale` must not emit
  `ip_quality.missing.v1`, `ip_quality.stale.v1`, `ip_quality.partial.v1`, or
  `ip_quality.risk.elevated.v1`, and must not raise overall `notice` / `attention`
  / `critical`.
- The IP Quality summary cell may carry detail
  `historical_disabled` and section `reason_code=ip_quality_disabled_has_history`
  so the operator can see that leftover evidence exists. That note is
  non-judging. No info-severity anomaly is required for this slice.

## Backend

### P1-01 / P2-05 — IP Quality availability + summary-only source

- Add `IPQualityAvailability` with `Enabled(context.Context) (bool, error)`.
  Production implementation reads only
  `center_settings.ip_quality_settings->>'enabled'` (or first-install default
  `false` when the row is absent). It must not decode the full settings JSON.
- Change `vpsOverviewIPQualitySource` from `GetVPSIPQuality` to
  `GetLatestVPSIPQualitySummary(ctx, vpsID)` (or reuse
  `ListLatestSummariesForVPS`). Overview mapping uses `status`, `risk_level`,
  `stale`, `observed_at` only.
- `LoadIPQuality` consults availability first:
  - availability error → return error (service maps to `unavailable`)
  - disabled → `Status=not_configured`; if a summary exists, set
    `ReasonCode=ip_quality_disabled_has_history` and do not copy risk/stale
    into the judging snapshot
  - enabled + empty summary → `Status=missing`
  - enabled + summary → current mapping, including stale
- Wire the availability owner in `NewVPSOverviewRepository` and
  `cmd/houfeng-center/bootstrap.go`.
- Integration test must construct the real repository + settings/IP-quality
  stores (bootstrap-equivalent wiring), not only `EvaluateAnomalies`.

### P2-03 — Coded HTTP errors

- Extend `handlers.writeError` family with `writeCodedError(w, status, message, code)`
  emitting `{ "error": "...", "code": "..." }`. Existing uncoded `writeError`
  stays for unrelated handlers.
- Map:
  - `vpsassets.ErrVPSAssetConflict` → `vps_asset_conflict`
  - `assetlifecycle.ErrStaleCancellationPreview` → `cancellation_preview_stale`
  - `assetlifecycle.ErrRetryableLifecycleConflict` → `lifecycle_transaction_conflict`
  - `assetlifecycle.ErrLifecycleActionBlocked` → `lifecycle_action_blocked`
- Keep short English `error` strings for logs/compat. Clients must not branch
  on those strings after this change.

### P2-04 — Subscription create idempotency

- New migration `0061_create_subscription_create_idempotency.sql` plus
  `currentRootSourceCount` / current APP ACL fragment for the table
  (`select`/`insert`; no update/delete needed if retries only read).
- Table: `idempotency_key` PK, `request_digest`, `subscription_id`,
  `created_at`. Key is the header value (trimmed, length-bounded). Digest is
  SHA-256 of a canonical JSON of the normalized create fields including
  `vps_id`.
- `POST /api/vps/{id}/subscriptions` requires `Idempotency-Key`. Missing /
  empty / oversized key → 400. Same key + same digest returns the original
  row (200 or 201; prefer 201 only on first insert, 200 on replay). Same key +
  different digest → 409 `idempotency_key_reused` (coded).
- Persist the key inside the same transaction as the subscription insert so a
  committed row cannot be duplicated on retry after a lost response.
- Collection `POST /api/subscriptions` is out of this slice unless it shares
  the same create helper; modern Overview uses the VPS-scoped path.

### P3-01 — Closed severity

- Change `Anomaly.Severity` from `string` to a named type with the four
  constants. `EvaluateAnomalies` and `overallStatus` use the typed values.
- JSON remains the same four strings.

## Web

### P1-02 — Cancellation entry and deep links

- `visibleManagementPanels(lifecycle, renewalDecision)` shows `cancellation`
  when:
  - lifecycle is `active|idle|testing` and renewal is
    `cancel|auto_renew_cancelled|migrate`, or
  - lifecycle is `to_cancel` or `to_migrate`.
- Archive item stays `to_cancel` only. Terminal `cancelled|archived` expose
  no write panels.
- `open_management` opens cancellation for `to_cancel` **and** `to_migrate`.
- `VPSDetailPage` (or a helper it owns before rendering Overview) parses
  `workbench` against `{cancellation, subscription}` only. After the Overview
  gate, open the panel. Unknown values are ignored and stripped.
- `VPSOverviewManagementActions` deep-link allowlist shrinks to the same two
  values; `decision` / `archive` query values no longer auto-open.
- `subscriptionLinkageAction` for a successful cancellation-like renewal
  decision adds “继续取消 / 退役” (`panel: 'cancellation'`) even when a
  subscription exists.

### P2-01 — Classified presentation

- New mappers in `web/src/lib/vpsOverviewPresentation.ts`.
- Summary detail maps only known machine tokens (IP status, lifecycle,
  renewal). Monitoring cell detail stays verbatim.
- Anomaly detail maps by `rule_id`: IP/lifecycle/source-unavailable tokens
  localize; monitoring health/incident details stay verbatim.
- Source-unavailable detail (`ip_quality, monitoring, renewal`) maps to
  Chinese source names.

### P2-02 — CAS conflict state

- Add `MutationConflict` on the management actions controller/page.
- On `code === 'vps_asset_conflict'` (allowlist), stop using the stale
  `updated_at`, keep the draft, and require “加载最新版本” before another
  write. Show a simple field-level 3-way compare for facts/decision drafts
  when both sides are available.

### P2-03 — Code-only client branching

- Default `apiRequest` error parse copies a string `code` when present
  (eager-safe; no Records decoder).
- `describeManagementError` and cancellation recovery branch only on
  allowlisted codes.

### P3-01 — Decoder fail-closed

- `decodeOverviewAnomaly` uses `overviewEnum(severity, CLOSED_SEVERITIES)`.
  Unknown severity rejects the Overview DTO.

## Compatibility / rollback

- Uncoded 409 messages remain in the body for a transition window; new clients
  ignore them.
- Missing `Idempotency-Key` on VPS subscription create is a breaking change
  for that one modern path only. Legacy collection create is unchanged unless
  shared.
- Rollback is revert of this branch; the new table is additive.

## Delivery and release

- Commit planning follows logical ownership: Trellis task record, backend
  idempotency/IP-quality contracts, then Web management/presentation behavior.
- Push only `fix/vps-overview-post-0771-hardening`; merge to protected `main`
  with GitHub PR required checks green and immutable head SHA verification.
- After feature merge, wait for main CI and Release Please. Merge the generated
  release PR through its checks; do not synthesize tags or publish images by
  hand unless the automated workflow is genuinely blocked and separately
  diagnosed.
- Release success requires the GitHub release and `publish-images` workflow to
  finish, including signed agent binaries/checksum, Compose assets, and the
  `docker.io/linnea7171/houfeng:v<version>`, `<version>`, and `latest` manifest.
- Cleanup happens only after release evidence is complete: archive the Trellis
  task, record the journal, sync local main by fast-forward, then remove the
  dedicated worktree and local feature branch. Remote branch cleanup follows
  the merged PR's normal deletion path.

## Out of scope

- Enabling IP Quality by default.
- Changing cancellation apply / preview digest contracts.
- Records-platform idempotency reuse.

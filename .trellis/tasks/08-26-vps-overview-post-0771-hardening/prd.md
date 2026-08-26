# Harden VPS overview after v0.77.1

## Goal

Close post-v0.77.1 release-blocker and concurrency/contract gaps on the VPS
Overview so a 30-second judgment stays truthful, cancellation/retirement stays
discoverable, and mutations cannot retry stale versions or duplicate
subscriptions.

## Requirements

### P1-01 — Overview must know whether IP Quality is enabled

- Default settings keep IP Quality `Enabled=false`. Overview must consult an
  explicit cheap availability owner, not infer enablement from the presence of
  a historical report.
- Do not load the full settings document on every Overview request.
- Disabled + no historical report → section `not_configured`; do **not** emit
  `ip_quality.missing.v1`.
- Disabled + leftover historical report → still `not_configured` for **current**
  judgment. Do **not** emit missing / stale / partial / high-risk anomalies from
  leftover data. Overall notice/alert must not be driven by leftover reports.
  The section may note that historical data exists while the capability is
  disabled (info-level, non-judging).
- Enabled + no historical report → `missing`, emit `ip_quality.missing.v1`.
- Enabled + source failure → `unavailable`.

### P1-02 — Discoverable cancellation / retirement entry

- Ordinary VPS PATCH still forbids writing `to_cancel` / `cancelled` /
  `archived`. Users who set `renewal_decision` to a cancellation-like value
  must still reach the cancellation workbench from the current VPS page.
- Visibility:
  - `active` / `idle` / `testing` and
    `renewal_decision ∈ cancel | auto_renew_cancelled | migrate` → show
    “取消 / 退役” in the lifecycle management group.
  - `to_migrate` → show migrate / retire workbench.
  - `to_cancel` → anomaly primary action opens cancellation; menu still keeps
    the lifecycle entry.
  - `cancelled` / `archived` → no write actions; read-only archive page.
- Modern `VPSDetailPage` must parse a strict allowlist query
  `?workbench=cancellation|subscription` and, after the Overview capability
  gate, open the matching panel. Unknown values: ignore or canonicalize-remove.
- After a cancellation-like renewal decision succeeds, provide an explicit next
  step “继续取消 / 退役”, not only “查看关联订阅”.
- Cancellation stays low-frequency / dangerous / not first-level, but must not
  disappear.

### P2-01 — Localize summary and anomaly details (classified, not blunt)

- Add field/rule-aware mappers `overviewSummaryDetailLabel(key, value)` and
  `overviewAnomalyDetailLabel(ruleId, value)`.
- Do not blindly translate every detail. Monitoring failure summaries that the
  operator must read stay verbatim.
- Mapped DOM fields must not leak machine values such as `partial`, `high`,
  `success`, `to_cancel`, or `ip_quality, monitoring, renewal`.

### P2-02 — CAS 409 recovery

- After a VPS version conflict, the UI must enter a dedicated
  `MutationConflict` state (`kind: 'vps_version_conflict'`) that keeps the
  user draft, explains which fields will be replaced, and offers “加载最新版本”.
- The user must not infinitely retry the same stale `updated_at` / ETag.

### P2-03 — Stable lifecycle error codes

- Server returns stable `code` values in addition to display `error` text:
  `vps_asset_conflict`, `cancellation_preview_stale`,
  `lifecycle_transaction_conflict`, `lifecycle_action_blocked`.
- Shape: `{ "error": "...", "code": "lifecycle_transaction_conflict" }`.
- Frontend decides behavior only from an allowlisted `code`. Message is
  display/log only.

### P2-04 — Idempotent subscription create

- Modern Overview `POST /api/vps/{id}/subscriptions` must send
  `Idempotency-Key`.
- Server persists key + request digest + result identity:
  same key + same digest → original result; same key + different digest → 409.
- A lost HTTP response plus client retry must create only one subscription row.

### P2-05 — Overview IP source is summary-only

- Overview needs only `status`, `risk_level`, `stale`, `observed_at`.
- Full report / provider results / service unlocks / 30-history stay on
  `/ip-quality` detail. Overview must not query those tables.

### P3-01 — Close anomaly severity enum

- Backend and TypeScript close severity to `critical` / `warning` / `notice` /
  `info`. The frontend decoder fails closed on unknown severity (reject DTO,
  do not emit an unknown CSS modifier).

## Acceptance Criteria

- [x] Default settings: IP Quality disabled + no report → no
      `ip_quality.missing.v1`, proven through real bootstrap + repository
      wiring (not only the anomaly evaluator).
- [x] Disabled + leftover high-risk report → `not_configured`; no current
      judgment pollution from leftover data.
- [x] Active VPS + single active subscription → set renewal to cancel →
      cancellation workbench is discoverable and openable from the current VPS
      page.
- [x] Modern deep link `?workbench=cancellation` opens the panel when Overview
      capability is on.
- [x] CAS 409 → load latest version; cannot infinitely retry stale
      `updated_at`.
- [x] Same subscription Idempotency-Key retry after lost response → one row.
- [x] Overview IP source executes summary-only queries, not provider results /
      unlocks / history.
- [x] DOM does not show internal details `partial`, `high`, `to_cancel`,
      `ip_quality, monitoring`.
- [x] Unknown anomaly severity is rejected by the decoder, not rendered as an
      unknown CSS modifier.
- [x] Changes are split into coherent Conventional Commit batches, pushed to
      the feature branch, and merged through a required-checks-green PR.
- [x] Main CI and Release Please succeed; the generated release PR is merged
      only after its required checks pass.
- [x] The new GitHub release, signed agent assets, production Compose assets,
      and `docker.io/linnea7171/houfeng` version/latest multi-arch image are
      publicly verifiable.
- [ ] Trellis task/journal, local main, feature branch, and dedicated worktree
      are reconciled and cleaned without directly committing to main.

## Notes

- Source: post-v0.77.1 review spec provided by the operator. This is a complex
  backend+web task; `design.md` and `implement.md` are required.
- Source branch: `fix/vps-overview-post-0771-hardening` in worktree
  `/home/murray/code/houfeng/.worktree/vps-overview-post-0771-hardening`.
- Target branch: protected `main`; all integration goes through pull requests.
- Delivery authorization was granted on 2026-08-26: batch commits, PR/CI,
  protected merge, release verification, then worktree cleanup.
- Chinese remains the primary UI language. Keep single-center + PostgreSQL +
  outbound agents. Do not revive archived docs.
- Feature PR #458 merged at `0a54b50a` after CI run `32979231414`; the following
  main CI run `32979617250` and Release Please run `32979617175` passed.
- Release PR #459 merged at `efc32b0b` after CI run `32979673901`. Final main
  CI `32980352538`, Release Please `32980352452`, and publish-images
  `32980370133` passed and published `v0.77.2`.
- The public release exposes exactly six expected assets. Downloaded agent
  binaries passed `sha256sum -c`; `sha256sums.txt.minisig` verified with the
  installer-pinned public key and trusted comment `houfeng v0.77.2 checksum
  manifest`; Compose asset digests matched GitHub metadata.
- Docker tags `v0.77.2`, `0.77.2`, and `latest` resolve to index digest
  `sha256:833ed47cda1b2cdf9a64cbe00551d2ff58075236387119e884e709e2c35824a4`,
  containing `linux/amd64` and `linux/arm64` images.
- Local `main` was fast-forwarded to the release commit. The clean, merged
  feature worktree and both local and remote feature branches were removed
  before task archival; the separate closeout branch carries only Trellis
  archival and journal records through protected-main review.

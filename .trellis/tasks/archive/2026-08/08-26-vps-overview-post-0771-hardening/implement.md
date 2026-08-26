# VPS overview post-v0.77.1 hardening execution plan

TDD: write the failing regression first, watch it fail for the stated reason,
then implement the minimum code.

## Phase 1 — Release blockers

### P1-01 IP Quality availability

- [x] Failing store/service tests: disabled + no report → `not_configured`, no
      `ip_quality.missing.v1`; disabled + leftover high-risk → no judging
      anomalies; enabled + no report → missing.
- [x] Failing bootstrap-equivalent integration: real settings + IP quality +
      `VPSOverviewRepository` wiring (not stuffed evaluator input).
- [x] Implement `IPQualityAvailability`, cheap `Enabled`, `LoadIPQuality`
      gating, bootstrap wiring.

### P1-02 Cancellation entry

- [x] Failing Vitest: menu shows “取消 / 退役” for active + `renewal_decision=cancel`;
      `open_management` opens cancellation for `to_migrate`; deep-link
      allowlist; post-decision “继续取消 / 退役”.
- [x] Failing Playwright: active VPS + one active subscription + no other
      links → set renewal to cancel → workbench discoverable/openable;
      `?workbench=cancellation` opens when Overview capability is on.
- [x] Implement menu visibility, page command, query parse, next-step action.

## Phase 2 — Data / concurrency UX

### P2-03 Stable error codes (backend first so frontend can branch)

- [x] Failing handler tests for coded 409 bodies.
- [x] Implement `writeCodedError` and lifecycle/VPS conflict mapping.
- [x] Frontend allowlist branching; stop matching English messages.

### P2-04 Subscription idempotency

- [x] Failing handler + store tests; PostgreSQL integration: commit, drop
      response, retry same key → one row.
- [x] Migration `0061_*`, repository transaction, VPS POST header, frontend
      `Idempotency-Key`.

### P2-02 CAS 409 recovery

- [x] Failing Vitest: 409 conflict enters `MutationConflict`; retry without
      reload still uses stale etag is forbidden; “加载最新版本” applies new
      `updated_at`.
- [x] Implement conflict state + 3-way compare for facts/decision.

### P2-01 Localized details

- [x] Failing presentation + component tests: mapped fields never render
      `partial` / `high` / `to_cancel` / `ip_quality, monitoring`.
- [x] Implement classified mappers; monitoring details stay verbatim.

## Phase 3 — Performance and enum cleanup

### P2-05 Summary-only IP source

- [x] Failing test that Overview IP load does not query provider results /
      unlocks / 30-history (query spy or SQL fixture assertion).
- [x] Switch Overview source to `GetLatestVPSIPQualitySummary` /
      `ListLatestSummariesForVPS`.

### P3-01 Closed severity

- [x] Failing decoder test: unknown severity rejects Overview DTO.
- [x] Close Go type + TS enum; CSS only receives known modifiers.

## Phase 4 — Verification

- [x] `make verify-go` (or focused Go packages if the full gate is running).
- [x] Focused Go tests for availability, coded errors, idempotency, summary-only IP.
- [x] `cd web && npm run test -- --run` plus focused Vitest for presentation,
      management, decoder, conflict recovery.
- [x] Playwright cancellation-path spec when Chromium is available.
- [x] Do not commit.

## Validation commands

```bash
go test ./internal/center/vpsoverview ./internal/center/store ./internal/center/http/handlers ./cmd/houfeng-center
make verify-go
cd web && npx vitest run src/lib/vpsOverviewPresentation.test.ts src/lib/recordsApi.test.ts src/pages/vps-detail/
cd web && npx playwright test e2e/vps-overview-cancellation.spec.ts
```

## Postgres integration follow-up (2026-08-26)

Ran against the existing local `postgres:16-alpine` on `:5432` with the documented DSN
`postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable`.

- Added `internal/center/store/vps_overview_ip_quality_postgres_integration_test.go`
  (bootstrap-equivalent settings + overview + activity wiring). Both subtests PASS.
- Existing `TestCreateSubscriptionIdempotentReplayAfterLostResponseKeepsOneRow` PASS
  (replay keeps one subscription row; reused key + different digest → `ErrIdempotencyKeyReused`).

## Review-fix follow-up (2026-08-26, no commit)

Five commit-blocking findings:

1. VPS-scoped POST DTO now accepts/persists `billing_period_unit`,
   `billing_period_length`, `renewal_mode` (real `buildSubscriptionInput` body).
2. Overview `GetLatestVPSIPQualitySummary` uses an independent narrow query;
   it no longer reads `ip_quality_latest_vps_summaries` or provider/unlock counts.
3. CAS `mutationConflict` clears on panel close/switch and is scoped by `draftKind`.
4. Facts retry keeps a form-open base snapshot and 3-way-merges every retry field.
5. `.trellis/spec/web/state-and-data.md` documents caller-held idempotency.

## Rematch follow-up (2026-08-26, no commit)

1. Facts form remounts on `detail.updated_at` and rehydrates SSH/IPv6/country
   derived state from the merged draft when that revision changes.
2. Conflict hint is a true 3-way compare: only local edits (`draft !== base`)
   that still differ from latest. Server-only fields are not shown as draft overwrites.

## Rematch review follow-up (2026-08-26, no commit)

1. Deleted the facts-form revision `useEffect`. Parents remount with
   `key={detail.updated_at}`; derived SSH/IPv6/country state is first-mount only.
2. Load-latest freezes fact fields while `submitting` and merges from
   `factDraftRef` so mid-flight typing is not overwritten by the stale closure.
3. `mergeFactDraftWithLatest` / compare use `buildFactEditInput` semantics
   (trim, port parse, label normalize) so formatting-only edits do not clobber
   concurrent server fields.
4. Overview and Legacy rotate the subscription idempotency key only after
   `409 idempotency_key_reused`; transport failure keeps the original key.
5. Backend database spec VPS-scoped subscription section now includes period
   fields, `renewal_mode`, and Idempotency-Key / 200 replay / 400/409 codes.
6. Playwright cancellation spec starts from active + keep, PATCHes renewal
   to cancel, then asserts the workbench entry. Deep-link coverage remains.

## Rematch review follow-up 2 (2026-08-26, no commit)

1. Fact 3-way compare now normalizes each field independently (trim / port
   parse / label parse). An invalid name or port no longer falls every field
   back to raw string compare, so format-equivalent labels still take server
   `ops`.
2. Renewal decision select and reason textarea disable while `submitting`,
   matching the facts-form freeze during load-latest.

## Rematch review follow-up 3 (2026-08-26, no commit)

1. After a successful cancel-like renewal PATCH, both refresh-success and
   refresh-failure feedback keep the `updated`-derived “继续取消 / 退役”
   action. Stale Overview `keep` no longer hides the next step.

## Final comprehensive review (2026-08-26)

- Findings: no remaining Critical, Important, or Minor review issue.
- Focused Vitest: 5 files / 25 tests PASS.
- Full Go: `gofmt -l`, `git diff --check`, `go vet`, and `go test -count=1`
  across `agent/...`, `cmd/...`, `db/...`, and `internal/...` PASS.
- Full web: lint, production build, toolchain/quality/bundle/CSS gates PASS;
  coverage 202 files / 1434 tests PASS; Playwright 133 / 133 PASS.
- PostgreSQL integrations remain recorded above and are rerun as the final
  pre-commit database gate before delivery.

## Phase 5 — Commit and PR delivery

- [x] Run the final PostgreSQL and full repository pre-commit gates.
- [x] Commit the Trellis task record and implementation in coherent batches.
- [x] Push `fix/vps-overview-post-0771-hardening` and open feature PR #458.
- [x] Monitor all required PR checks; diagnose and fix failures on the same
      branch; merge only with an unchanged verified head SHA.
- [x] Verify main CI and Release Please after the feature merge.

## Phase 6 — Release and cleanup

- [x] Review and merge the generated Release Please PR after its checks pass.
- [x] Verify the GitHub release, signed agent assets, Compose assets, and
      multi-arch Docker version/latest tags.
- [ ] Archive this Trellis task and record the delivery commits in the journal.
- [x] Fast-forward local main, remove the dedicated worktree and local feature
      branch, prune stale worktree metadata, and confirm a clean next-start
      checkout.

### Final pre-commit evidence (2026-08-26)

- Go 1.26.2 targeted PostgreSQL integration PASS:
  `TestCreateSubscriptionIdempotentReplayAfterLostResponseKeepsOneRow` and both
  `TestPostgresOverviewDisabledIPQualityDoesNotJudgeLeftoverOrMissingReport`
  subtests.
- Go 1.26.2 + Node 22.23.1 official `./scripts/verify.sh` PASS. The Go format
  stage normalized two APP ACL expectation files before vet/tests; the final
  formatted tree is the reviewed input.
- Web coverage: 202 files / 1434 tests PASS; production build and bundle/CSS
  budgets PASS.
- Full Chromium Playwright: 133 / 133 PASS, including the cancellation decision
  PATCH path and cancellation workbench deep link.

### Feature PR record

- PR: https://github.com/xiangnan0811/houfeng/pull/458
- Work commits: `d153b8e2`, `e62e485a`, `2dda7437`, `1043e08c`.

### Delivery and release evidence (2026-08-26)

- Feature head `6212f66f` passed all seven checks in CI run `32979231414` and
  merged through PR #458 at `0a54b50a`. Main CI `32979617250` and Release
  Please `32979617175` passed.
- Release head `925dfc45` passed all seven checks in CI run `32979673901` and
  merged through PR #459 at `efc32b0b`. Final main CI `32980352538` and Release
  Please `32980352452` passed.
- `v0.77.2` is a public non-prerelease whose tag resolves to `efc32b0b`.
  `publish-images` run `32980370133` passed agent-assets, amd64/arm64 builds,
  manifest publication, image inspection, and public deployment-asset checks.
- The six public assets are `houfeng-agent_v0.77.2_linux_amd64`,
  `houfeng-agent_v0.77.2_linux_arm64`, `sha256sums.txt`,
  `sha256sums.txt.minisig`, `compose.yaml`, and `compose.env.example`. Local
  checksum and minisign verification passed; forbidden legacy deployment asset
  names were absent.
- Docker tags `v0.77.2`, `0.77.2`, and `latest` all resolve to
  `sha256:833ed47cda1b2cdf9a64cbe00551d2ff58075236387119e884e709e2c35824a4`,
  with `linux/amd64` and `linux/arm64` manifests.
- Primary `main` was fast-forwarded to `efc32b0b`. The original feature
  worktree was clean and its head was an ancestor of `origin/main`; it and the
  local/remote feature branches were then removed and worktree metadata pruned.

## Rollback

Revert the feature branch. The idempotency table is additive; unused after
revert.

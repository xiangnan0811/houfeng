# Implement: v0.77.5 remaining hardening seams

## Checkout

Current tree is clean `main` at `v0.77.4`. Create `feat/v0775-hardening-closeout` in this checkout (no worktree). Enable hooks with `sh scripts/setup-git-hooks.sh` before the first commit.

## Order

1. **P2-01 disabled IP Quality out of the judgement path**
   - `LoadIPQuality`: if `!enabled`, return `not_configured` + `SectionReady` without calling `GetLatestVPSIPQualitySummary`.
   - Repo test: disabled path `summaryCalls == 0`; leftover high-risk report does not set `ip_quality_disabled_has_history`.
   - Service test: summary fake blocks on `ctx.Done()` then sleeps ~10ms; disabled Overview stays `healthy` / `not_configured` / `ready`, no `source.unavailable.v1`. Use a short budget.
   - Postgres integration: disabled + existing report still `not_configured` + ready, empty reason code.
   - Overview UI tests: disabled current judgement shows “未启用”, not “存在历史报告（当前未启用）”. Keep the presentation label map.

2. **P2-02 Legacy write ownership**
   - Wrap link, create-monitoring, create-subscription, extend-validity, unlink, archive, experience, service, domain, and cancellation with `beginVpsWrite` + `mutationIsCurrent` before notice/draft/drawer/navigate.
   - Cancellation keeps preview generation; also take the per-VPS write lock.
   - Duplicate-submit error: “上一次保存仍在进行，请稍后再试”.
   - Tests in `LegacyVPSDetail.test.tsx`:
     - A create-monitoring pending → switch B → A completes, no `/monitoring/:id` navigate.
     - A archive pending → switch B → A completes, no `/archive/:id` navigate.
     - A create-service pending → switch B and open a drawer → A completes, B drawer stays open.
     - A create-subscription pending → switch B → A replay completes, no A notice on B.

3. **P3-01 semantic DTO manifest**
   - Rewrite `vps_subscription_create_fields.json` to `{name,type,required,nullable}[]` using the design table.
   - Update Go and Vitest parsers. Fail on type / required / nullable drift. Do not infer Go zero-value fields as required.
   - Add a negative assertion (or parser unit) that `price: string`, `note?: string`, and non-nullable `renew_at` would fail.

4. **P3-02 + wire-doc**
   - Update `.trellis/spec/backend/subscription-cost-center.md`: permanent receipt lifetime, cascade-on-subscription-delete, backup restore keeps keys, no janitor.
   - Update `.trellis/spec/web/state-and-data.md`: manifest is a semantic object list.
   - Update `.trellis/spec/backend/ip-quality-contract.md`: disabled Overview does not query latest summary and does not emit `ip_quality_disabled_has_history`.
   - Short `README.md` + `docs/design/current/product-and-architecture.md` note: `POST /api/subscriptions` requires `Idempotency-Key`.

## Validation

```bash
go test ./internal/center/store -run 'TestLoadIPQuality|TestOverviewServiceDisabledIPQuality|TestPostgresVPSOverview'
go test ./internal/center/http/handlers -run 'TestVPSSubscriptionCreateRequestMatchesTypeScriptDTO'
cd web && npx vitest run \
  src/lib/vpsSubscriptionCreateContract.test.ts \
  src/lib/vpsOverviewPresentation.test.ts \
  src/pages/vps-detail/VPSOverviewPageView.test.tsx \
  src/pages/vps-detail/VPSOverviewFreshness.test.tsx \
  src/pages/vps-detail/LegacyVPSDetail.test.tsx
make fmt-go
# before finish:
./scripts/verify.sh
```

PostgreSQL integration for disabled Overview with an existing report must be run where that file is already gated (same as v0.77.4). Browser Playwright is not required unless Legacy/Overview e2e fixtures fail compile; Vitest covers the new races.

## Review gates

- Disabled path must not call `GetLatestVPSIPQualitySummary`. Ignoring an immediate error is not enough.
- The blocking service test must exist; do not only assert `summaryCalls == 0`.
- Legacy ownership must cover create-monitoring, archive, service, and subscription, not only facts/decision.
- Late A callbacks must not `collapseDrawer` or `navigate`.
- Manifest tests must fail type and requiredness drift, not only renamed fields.
- No receipt janitor or new migration.
- Docs must say receipts are permanent and collection POST requires `Idempotency-Key`.

## Rollback

Feature-branch revert. No schema change.

## Follow-up before `task.py start`

- Planning summary approved by the user.
- Branch `feat/v0775-hardening-closeout` created from current `main`.
- `implement.jsonl` / `check.jsonl` contain real spec entries.

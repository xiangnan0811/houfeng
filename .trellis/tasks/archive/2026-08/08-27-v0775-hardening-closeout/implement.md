# Implement: v0.77.5 remaining hardening seams

## Checkout

- Worktree: `/home/murray/code/houfeng/.worktree/v0776-vps-hardening`
- Branch: `codex/v0776-vps-hardening`
- Base/HEAD: reviewed `6c91b128a621e0adf0b2ce2e6434ebc3ad758340` (`v0.77.5`)
- The remediation starts unstaged on this non-main branch. Hooks are enabled. External review cleared the accepted scope on 2026-08-28; batch the work commits before archiving this task tree, then continue through PR, CI, merge, release and image verification.

## Order

1. **P2-01 disabled IP Quality out of the judgement path**
   - `LoadIPQuality`: if `!enabled`, return `not_configured` + `SectionReady` without calling `GetLatestVPSIPQualitySummary`.
   - Repo test: disabled path `summaryCalls == 0`; leftover high-risk report does not set `ip_quality_disabled_has_history`.
   - Service test: summary fake blocks on `ctx.Done()` then sleeps ~10ms; disabled Overview stays `healthy` / `not_configured` / `ready`, no `source.unavailable.v1`. Use a short budget.
   - Postgres integration: disabled + existing report still `not_configured` + ready, empty reason code.
   - Overview UI tests: disabled current judgement shows “未启用”, not “存在历史报告（当前未启用）”. Keep the presentation label map.

2. **P2-02 Legacy write ownership**
   - Follow the bounded current contract in `.trellis/spec/web/vps-detail-ownership.md`; do not reintroduce the historical begin/finish signatures or component-local submitting booleans.
   - Keep the production store page-scoped across capability-gate remounts. Guard `beginVpsWrite(...) === null`, derive pending state with `useSyncExternalStore`, gate post-await UI commits with the owner generation, and finalize only the exact owner.
   - Exact stale-view settle may notify the mounted/current matching page to re-probe; current-view settle and an old A while B is current must not re-probe.
   - Preserve the full operation/refresh/preview regression inventory maintained by the active remediation task, including real-entry remount, Drawer close/reopen, same-VPS query reload, current-view no-extra-probe, and exact/stale store finish results.

3. **P3-01 semantic DTO manifest**
   - Rewrite `vps_subscription_create_fields.json` to `{name,type,required,nullable}[]` using the design table.
   - Update Go and Vitest parsers. Fail on type / required / nullable drift. Do not infer Go zero-value fields as required.
   - Add a negative assertion (or parser unit) that `price: string`, `note?: string`, and non-nullable `renew_at` would fail.

4. **P3-02 + wire-doc**
   - Update `.trellis/spec/backend/subscription-cost-center.md`: permanent receipt lifetime, cascade-on-subscription-delete, backup restore keeps keys, no janitor.
   - Update `.trellis/spec/web/state-and-data.md`: manifest is a semantic object list.
   - Update `.trellis/spec/backend/ip-quality-contract.md`: disabled Overview does not query latest summary and does not emit `ip_quality_disabled_has_history`.
   - Short `README.md` + `docs/design/current/product-and-architecture.md` note: `POST /api/subscriptions` requires `Idempotency-Key`.

## Final validation matrix

```bash
go test ./internal/center/store -run 'TestLoadIPQuality|TestOverviewServiceDisabledIPQuality|TestPostgresVPSOverview' -count=1
go test ./internal/center/http/handlers -count=1
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test -- --run \
  src/lib/vpsSubscriptionCreateContract.test.ts \
  src/lib/vpsOverviewPresentation.test.ts \
  src/pages/vps-detail/VPSOverviewPageView.test.tsx \
  src/pages/vps-detail/VPSOverviewFreshness.test.tsx \
  src/pages/vps-detail/LegacyVPSDetail.test.tsx \
  src/pages/VPSDetailPage.legacy-ownership.test.tsx \
  src/pages/vps-detail/vpsWriteOwnerStore.test.ts
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin make verify-web
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test:e2e
make verify-go
python3 .trellis/scripts/task.py validate .trellis/tasks/08-27-v0775-hardening-closeout
python3 .trellis/scripts/task.py validate .trellis/tasks/08-27-v0775-vps-detail-hardening
git diff --check
git diff --cached --check
```

PostgreSQL integration for disabled Overview with an existing report must be run where that file is already gated. Formal Chromium `npm --prefix web run test:e2e` is a mandatory independent gate because `make verify-web` does not execute Playwright. If `make verify-go` again fails only at unchanged `internal/center/attachments TestPreviewImageGoldenMetadataFreeBoundedPNG` with actual `0d749fd4…` versus expected `dac4e6f5…`, record it as the approved pre-existing baseline exception; any other failure blocks closeout. All five remediation task directories must validate without context warnings before external re-review.

## Review gates

- Disabled path must not call `GetLatestVPSIPQualitySummary`. Ignoring an immediate error is not enough.
- The blocking service test must exist; do not only assert `summaryCalls == 0`.
- Legacy ownership must cover create-monitoring, archive, service, and subscription, not only facts/decision.
- Late A callbacks must not `collapseDrawer` or `navigate`.
- Manifest tests must fail type and requiredness drift, not only renamed fields.
- No receipt janitor or new migration.
- Docs must say receipts are permanent and collection POST requires `Idempotency-Key`.
- The remediation tree rooted at `08-27-v0775-vps-detail-hardening` must remain linked and active until its latest external review has no blocking findings.
- Passing implementation tests or an internal `trellis-check` does not authorize archive. Record the independent external-review verdict, remediate every accepted finding, and wait for a clear re-review before archiving this parent.

## Rollback

Feature-branch revert. No schema change.

## Cleared execution state

- Parent status remains `in_progress` on `codex/v0776-vps-hardening` only until the reviewed work commits and task-tree archive commits are created.
- `08-27-v0775-vps-detail-hardening` retains its three implementation children; all four remediation tasks passed external review on 2026-08-28.
- `implement.jsonl` / `check.jsonl` use bounded current specs and review evidence without context-truncation warnings.

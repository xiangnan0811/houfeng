# Design: v0.77.5 remaining hardening seams

## Boundaries

| Layer | Owner | Change |
| --- | --- | --- |
| Store | `internal/center/store/vps_overview.go` | Disabled IP Quality returns `not_configured` + `SectionReady` and does not call `GetLatestVPSIPQualitySummary`. |
| Overview service tests | `internal/center/store/vps_overview_ip_quality_test.go`, postgres integration | Blocking-deadline fake; leftover-history tests no longer require `ip_quality_disabled_has_history`. |
| Web Overview | `VPSOverviewPageView` / `VPSOverviewFreshness` tests | Disabled current judgement shows “未启用” only. Keep the presentation map so a stale reason code still localizes. |
| Web Legacy | `web/src/pages/vps-detail/LegacyVPSDetail.tsx` | Every remaining async write uses `beginVpsWrite` + `mutationIsCurrent` before UI side effects. |
| HTTP contract | `vps_subscription_create_fields.json` + Go/TS parsers | Manifest becomes `{name,type,required,nullable}[]`. |
| Spec / docs | `subscription-cost-center.md`, `README.md`, `docs/design/current/product-and-architecture.md` | Permanent receipt lifecycle + `Idempotency-Key` wire note. |

No new tables, workers, routes, or caches.

## P2-01 Disabled IP Quality

```
IPQualityEnabled → false
  return {
    Status: "not_configured",
    Section: { State: SectionReady },
  }
```

Do not read latest summary. Do not set `ip_quality_disabled_has_history`. Availability-check errors still fail the section. Enabled path is unchanged.

History stays on `GET /api/vps/{vps_id}/ip-quality` / IP Quality detail. Overview copy is “未启用”.

Regression: a summary fake that does `<-ctx.Done(); time.Sleep(...); return ctx.Err()` must not change disabled Overview health even if a later change reintroduces the query. Use a short `NewServiceWithBudgets` total (tens of milliseconds), not the 800ms production budget. Repository test asserts `summaryCalls == 0` on the disabled path.

`ip_quality_disabled_has_history` may remain in the frontend label map. Overview must not emit it.

## P2-02 Legacy mutation ownership

Reuse the existing mechanism. Do not invent `MutationOwner` as a second system.

`beginVpsWrite(vpsId)` already:

- rejects a second in-flight write for the same VPS
- stores a request token in `Map<vpsId, token>`
- increments `mutationGenerationRef`
- sets `writeInFlight` only when that VPS is current

Route change already increments `mutationGenerationRef` and `cancellationPreviewGenerationRef`.

Wrap every remaining async write with the same pattern as facts/decision:

```
const write = beginVpsWrite(detail.vps_id)
if (!write) { setError('上一次保存仍在进行，请稍后再试'); return }
const { generation, token } = write
try {
  await server
  if (!mutationIsCurrent(generation)) return
  // notice, draft reset, collapseDrawer, navigate, refresh
} catch {
  if (!mutationIsCurrent(generation)) return
  setError(...)
} finally {
  finishVpsWrite(detail.vps_id, token)
  setSubmitting(false)
}
```

Handlers in this set:

- `handleLinkSubmit`
- `handleMonitoringInstanceCreateSubmit`
- `handleSubscriptionSubmit`
- `handleValidityExtensionSubmit`
- `handleUnlinkMonitoringInstance`
- `handleArchiveVPS`
- `handleExperienceSubmit`
- `handleServiceSubmit`
- `handleDomainSubmit`

`handleCancellationSubmit` already uses preview generation. Also take `beginVpsWrite` so cancel cannot overlap another same-VPS write. Keep the preview generation check. `handleRestoreVPS` is a synchronous archive redirect and stays as-is.

`refreshDetail` already ignores `setState` when `current.vpsId` differs. That is not enough: `collapseDrawer`, `navigate`, and `setNotice` still fire. Ownership checks must sit in front of those side effects.

`finishVpsWrite` stays in `finally` even when generation is stale, so returning to A after A completes can write again. Different VPS IDs stay parallel.

Duplicate-submit copy matches facts/decision: “上一次保存仍在进行，请稍后再试”.

## P3-01 Semantic DTO manifest

Replace the string array in `internal/center/http/handlers/vps_subscription_create_fields.json` with:

```json
[
  {"name": "price", "type": "number", "required": true, "nullable": false},
  {"name": "renew_at", "type": "date", "required": false, "nullable": true}
]
```

Closed `type` vocabulary: `number` | `string` | `boolean` | `date`.

Current wire contract (do not “fix” optional-with-default fields into required):

| name | type | required | nullable |
| --- | --- | --- | --- |
| price | number | true | false |
| currency | string | true | false |
| billing_cycle | string | true | false |
| billing_months | number | true | false |
| billing_period_unit | string | false | false |
| billing_period_length | number | false | false |
| started_at | date | false | true |
| renew_at | date | false | true |
| auto_renew | boolean | true | false |
| auto_renew_cancelled | boolean | true | false |
| renewal_mode | string | false | false |
| payment_method | string | true | false |
| note | string | true | false |

Checks:

- **Names:** Go json tags, TS property names, and manifest names are the same ordered list.
- **Type:** Go `float64`/`int` → `number`; `string` → `string`; `bool` → `boolean`; `*subscriptions.Date` → `date`. TS `number` / `string` (including enum unions) / `boolean` / `string \| null`.
- **Nullable:** Go pointer ↔ `nullable: true`. TS includes `null` ↔ `nullable: true`.
- **Required:** TS `?` ↔ `required: false`. Manifest is the requiredness source of truth. Go non-pointer optional-with-default fields (`billing_period_unit`, `billing_period_length`, `renewal_mode`) are allowed; do not infer requiredness from Go zero values.

Do not parse enum ranges. Do not generate types from OpenAPI.

A focused test must fail if `price` is `string`, `auto_renew` is `string`, `renew_at` drops `| null`, or `note` becomes optional.

## P3-02 Receipt lifecycle and Idempotency-Key docs

No schema or worker change.

Contract to write:

- One receipt row per successful create key.
- Same key + digest always replays while the subscription row exists.
- Receipt lifetime equals the subscription row. `on delete cascade` already implements delete-with-subscription; there is no user-facing DELETE today.
- Backup restore restores receipts; old keys remain valid.
- No TTL, janitor, `created_at` index, or metrics platform. Table size is one row per historical create and is inspectable with SQL if needed.

Places:

- `.trellis/spec/backend/subscription-cost-center.md` — authoritative (already states both create doors require `Idempotency-Key`).
- `docs/design/current/product-and-architecture.md` — durable API/safety note.
- `README.md` — short public compatibility note, not a second design treatise.

Copy for the wire change that already shipped in `v0.77.4`:

```
POST /api/subscriptions requires Idempotency-Key.
Use a stable UUID for retries of the same request body.
Generate a new key when the logical operation changes.
```

## Compatibility

- Overview no longer annotates disabled IP Quality with leftover history. This is a display change, not a stored-data change. IP Quality detail is unchanged.
- Collection POST without `Idempotency-Key` remains `400`; this task only documents it.
- Receipt replay behaviour is unchanged.

## Rollback

Feature-branch revert. No migration.

## Risks

- Re-introducing the disabled history query would restore the deadline race. The blocking service test is the guard.
- Wrapping every Legacy write increases lock coverage: a pending service create blocks facts save on the same VPS. That is intended same-VPS mutual exclusion.
- Semantic DTO tests can fail on current optional-with-default fields if someone maps Go non-pointer to `required: true`. Follow the table above.

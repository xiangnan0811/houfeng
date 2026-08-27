# Design: v0.77.4 remaining hardening seams

## Boundaries

| Layer | Owner | Change |
| --- | --- | --- |
| HTTP | `internal/center/http/handlers/subscriptions.go` | Collection POST requires `Idempotency-Key` and uses `CreateSubscriptionIdempotent`. Share the VPS-scoped replay/error mapping. |
| HTTP | `internal/center/http/handlers/vps.go` | Both pre-check and repository `ErrVPSAssetReadonly` branches use `writeCodedError(..., "vps_asset_readonly")`. |
| Store | `internal/center/store/vps_overview.go` | Disabled IP Quality returns `not_configured` + `SectionReady` before any history lookup can fail the section. History is best-effort. |
| Store | `internal/center/store/subscriptions.go` | No new receipt table. Collection POST reuses `CreateSubscriptionIdempotent`. |
| Web API | `web/src/lib/api.ts`, `web/src/lib/types.ts` | `createSubscription(input, idempotencyKey)` sends the header. `CreateVPSSubscriptionInput` is an explicit billing-fact DTO. |
| Web pages | `SubscriptionsPage`, Overview + Legacy VPS detail | Shared CAS recovery presentation; collection create key lifecycle; readonly routing. |

No new tables, workers, or routes.

## Data flow

### Subscription create

```
SubscriptionsPage form
  → createSubscription(input, key)  // POST /api/subscriptions + Idempotency-Key
  → decode CreateInput
  → CreateSubscriptionIdempotent(input, key)
      lock key → same digest: return existing row (replayed)
               → different digest: ErrIdempotencyKeyReused
               → miss: insert subscription + receipt in one tx
  → 201 first write / 200 replay / 409 reused / 400 invalid key
```

VPS-scoped create stays on `vpsSubscriptionCreateRequest` and the same repository helper.

Key lifecycle (both workbenches):

- Allocate UUID when the form opens.
- Reuse the same key while the serialized create payload is unchanged (network retry).
- Rotate when the payload changes or the server returns `idempotency_key_reused`.
- Overview/Legacy already rotate on `subscriptionDraft` change; collection page must match.

### Legacy / modern CAS

Shared pieces, not a second algorithm:

- `isVPSVersionConflict` / `isVPSAssetReadonly` / `describeManagementError` in `vpsManagementHelpers.ts`.
- Fact three-way merge already in `vpsDetailHelpers.ts` (`mergeFactDraftWithLatest`, `compareFactDraftAgainstLatest`).
- Decision compare + labels live next to those helpers.
- `VPSVersionConflictBanner` extracted from Overview and reused by Legacy.

On `vps_asset_conflict`:

1. Record conflict `{ draftKind, loaded:false, staleUpdatedAt, compare:[] }`.
2. Block another write until latest is loaded.
3. `GET /api/vps/:id`, merge draft over latest for edited fields, replace `updated_at`.
4. If renewal draft already equals latest `renewal_decision`, treat as already satisfied: Chinese notice, refresh, close panel. Do not show “请选择一个不同的续费决策”.
5. Retry uses the new ETag.

On `vps_asset_readonly`:

1. `GET` current identity.
2. If `cancelled` / `archived`, `replace` to `/archive/:id`.
3. Else show “当前状态不允许修改” and stop further writes on that panel.

Async ownership:

- Both detail surfaces capture a mutation generation; route switch/unmount, and every allowed panel close, invalidate it before a late response can write draft/error/notice or navigate.
- Overview prevents closing the current panel while a submit is pending. Its production identity gate unmounts the route when `vpsId` changes, so a synchronous single-request ref owns the mounted Overview mutation.
- Legacy's persistent Modal may close and reopen while PATCH is pending. Its write ownership is `Map<vps_id, request_token>`: closing the Drawer does not release transport ownership, and only the matching request `finally` removes its token. Same-VPS retries stay blocked until settle, while unrelated VPS writes proceed independently.
- `loadLatestVersion` keeps its own synchronous in-flight ref so repeated clicks issue one GET. Late latest responses cannot replace another VPS draft or ETag.

### Disabled IP Quality

```
IPQualityEnabled → false
  status = not_configured
  section = ready
  summary, err := GetLatestVPSIPQualitySummary(...)  // best-effort
  if err == nil && summary != nil {
    reason = ip_quality_disabled_has_history
  }
  return result, nil
```

Enabled path still fails closed on summary errors (`ip_quality_timeout` / `ip_quality_unavailable` → `JudgementSourcesUnavailable`). Availability-check errors still fail the section.

## Error contract

| Condition | Status | code |
| --- | --- | --- |
| Missing/invalid Idempotency-Key | 400 | `invalid_idempotency_key` |
| Same key, different digest | 409 | `idempotency_key_reused` |
| Stale If-Match | 409 | `vps_asset_conflict` |
| Cancelled/archived ordinary PATCH | 409 | `vps_asset_readonly` |

Messages stay short English. Frontend maps codes to Chinese.

## Type contract (P3-03)

Choose A: narrow TypeScript.

`CreateVPSSubscriptionInput` is the VPS-scoped request DTO (price, currency, billing fields, dates, auto-renew flags, renewal_mode, payment_method, note). It is not `Omit<CreateSubscriptionInput, 'vps_id' \| 'status'>`.

`CreateSubscriptionInput` remains the collection body and may include display_name, labels, category, trial/end, status.

A Go reflect test on `vpsSubscriptionCreateRequest` json tags and a TS test on the explicit type share the same frozen field list.

## Compatibility

- Collection POST without `Idempotency-Key` becomes 400. Only `SubscriptionsPage` and `createSubscription` call it; both change together.
- Replay HTTP status 200 vs first write 201 matches VPS-scoped create.
- `ip_quality_disabled_has_history` still appears when the optional query succeeds.

## Rollback

Revert the feature branch. No migration. Idempotency table `0061_create_subscription_create_idempotency.sql` already exists from v0.77.3.

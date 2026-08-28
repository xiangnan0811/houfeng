# Design: v0.77.5 VPS detail hardening

## Scope map

| Child | Layer | Contract repaired | Primary files |
| --- | --- | --- | --- |
| scoped subscription required contract | Go HTTP | distinguish missing/null/explicit zero and enforce manifest semantics | `subscriptions.go`, `subscriptions_test.go`, contract fixtures/tests |
| Legacy operation and refresh ownership | Web | scope write pending state and refresh commits to an exact owner | `LegacyVPSDetail.tsx`, `LegacyVPSDetail.test.tsx` |
| archive review request ownership | Web | scope modal review loading/data/error to an exact request | same Legacy component/test |

The backend child is independent. The two web children share the same component and test file and therefore execute sequentially: operation/refresh ownership first, archive review second.

## 1. Scoped subscription request semantics

Reuse `subscriptions.OptionalString`, `OptionalFloat`, `OptionalInt`, `OptionalBool`, and `OptionalDate`; they already distinguish absent fields from decoded values and reject null for non-nullable scalar types. Give each manifest-required request field `required:"true"` and add an explicit request-to-domain validation/mapping boundary.

Required fields are:

| Field | Type | Null | Explicit zero/empty |
| --- | --- | --- | --- |
| `price` | number | reject | `0` valid |
| `currency` | string | reject | empty rejected by existing domain validation |
| `billing_cycle` | string | reject | empty rejected by existing normalization/validation |
| `billing_months` | number | reject | mapped exactly, existing domain validation applies |
| `auto_renew` | boolean | reject | `false` valid |
| `auto_renew_cancelled` | boolean | reject | `false` valid |
| `payment_method` | string | reject | `""` valid |
| `note` | string | reject | `""` valid |

`billing_period_unit`, `billing_period_length`, and `renewal_mode` remain optional but non-nullable. `started_at` and `renew_at` remain optional and nullable. The scoped request maps wrappers into the unchanged `subscriptions.CreateInput`; the collection handler keeps its current request type.

The Go contract reflection test reads the `required` tag instead of inferring requiredness from pointer shape. The existing TypeScript source-contract test receives only the minimal wrapper type mapping needed for the new Go request representation. Production validation remains explicit and does not depend on that text parser.

## 2. Operation transport ownership

Replace network-write component booleans with one reactive external ownership store keyed by VPS ID. Each owner records:

```text
{ vpsId, token, generation, operation }
```

`VPSDetailPage` creates the store once at the page-shell lifetime and passes it to the lazy Legacy child. Parameter changes may render `probing` and unmount/remount Legacy, but do not recreate the store. Direct Legacy mounts use a local fallback store; no module-global singleton is allowed. `beginVpsWrite(vpsId, operation)` refuses an existing owner only for the same VPS and publishes one immutable snapshot. `finishVpsWrite(owner)` deletes only when the stored token still equals the finishing token. `useSyncExternalStore` derives submitting props from the current VPS owner and operation, so A and B can have independent in-flight operations and an old `finally` cannot clear a newer owner.

Route changes invalidate mutation generations but do not erase transport owners: leaving and returning to A must still display A's pending request until A itself settles. All post-await UI effects remain behind `mutationIsCurrent(generation)`.

`VPSWriteOwnerStore.finish(owner)` returns whether it released the exact `vpsId + token`. Legacy centralizes finalization: after an exact release it notifies the page shell only when the owning mutation generation is no longer current. The page callback advances probe revision only while mounted and still displaying `owner.vpsId`. This stale-view settle signal covers returned-A remounts, Drawer close/reopen, and same-VPS query reload; it cannot let an A closure re-probe current B. A current-view write continues through Legacy's own commit/refresh path and emits no page re-probe, avoiding duplicate convergence work.

## 3. Refresh commit ownership

`refreshDetail`, `refreshServices`, and `refreshDomains` accept an `ownsRefresh` predicate defaulting to true for route-load callers. Mutation callers pass `() => mutationIsCurrent(owner.generation)`. Each helper checks both the current `vpsId` and the predicate inside the functional state update immediately before commit. This prevents an old A request from committing after A→B→A.

The server request is not aborted. A stale promise may resolve normally; it simply has no authority to mutate view state.

## 4. Archive review ownership

Maintain a monotonic `archiveReviewRequestRef`. Opening review captures `requestId` and target `vpsId`. Ownership requires both the latest request ID and `currentVpsIdRef.current === targetVpsId`. Route effect start/cleanup, modal close, and any route payload that resets/closes the modal increment the counter before visible state is cleared. A later open supersedes all earlier requests, including on the same VPS, while a same-VPS reload payload supersedes a review opened during that load.

Guard each asynchronous branch independently:

- success may set review data only while owned;
- failure may set the archive error only while owned;
- finally may clear loading only while owned.

Backend archive eligibility is still revalidated by the archive endpoint; this client owner is a UI integrity boundary, not authorization.

## 5. Route cleanup authority

Define one route-effect cleanup boundary that always invalidates archive-review request ID, mutation generation, and latest-load authority. Normal route loads additionally set their local `cancelled` flag. Both `!vpsId` and `skipNextQueryDrivenReload` early-return branches return the same authority cleanup rather than an archive-only callback.

Cleanup invalidates view commits but does not delete `writeOwnersRef`: transport owners survive until the matching `vpsId + token` promise finalizer runs. A deferred archive mutation started after a query-skip effect must not navigate after the component unmounts.

## 6. Cancellation preview supersession

`cancellationPreviewGenerationRef` is the exact owner of ordinary preview GET commits. Closing the cancellation Drawer increments it before clearing Drawer-local feedback. A stale preview may settle at the transport layer, but cannot populate `state.cancellationPreview`; manually reopening always clears pending or already-loaded preview/error/result and issues a fresh preview request, for both the workbench close control and Modal `onClose`.

## 7. Fail-closed DTO source contracts

Keep the existing bounded source-text approach, but classify a TypeScript union by exact trimmed members instead of keyword containment. `undefined`, empty/unknown members, or more than one JSON primitive kind return an error. Current string aliases (`BillingPeriodUnit`, `RenewalMode`) may union with `string` only after the parser finds exactly one live, same-source alias declaration outside comments/strings and proves every alias member is a non-empty string literal; widening an alias to `number`, `undefined`, another alias, or an empty member fails closed. Alias union splitting is quote/escape aware so `|` inside a valid literal remains content. Any multiline alias `&` / `|` continuation is rejected rather than trusting the first line. `null` only supplies nullability, and `string | null` remains the date wire shape. DTO markers likewise require exactly one live declaration, preventing block-comment shadows. The object parser validates the closing-brace line and, without a terminating semicolon, skips whitespace/comment trivia to reject a later significant `&` / `|`, so multiline intersections cannot hide extra fields. The Go-source mirror removes closed line/block comments outside raw tags before anonymous-field classification, preserves comment markers inside raw struct tags, rejects unterminated/multiline block comments, and extracts exact raw struct-tag keys, never substring matches such as `notjson` / `notrequired`. Both mirrors reject exported Go fields without an explicit usable JSON name; only exact `json:"-"` is ignored. Go DTO type classification uses an exact built-in/wrapper allowlist, panics directly for unsupported named types, rejects unknown named pointers before `Elem()`, and manifest decoding requires explicit non-null `required` and `nullable` keys.

## 8. Bounded Trellis context

Move the two Legacy ownership scenarios into a dedicated `.trellis/spec/web/vps-detail-ownership.md` below the per-file injection limit and link it from the web spec index. Replace parent/web-child JSONL references to oversized state/quality guides with this bounded ownership spec and a task-local bounded verification contract. The large general guides remain available by navigation, but are not the only injected authority for this task.

## 9. Route-load and route-preview ownership

Treat every normal route load as a generation-owned view transaction. Capture its mutation generation at entry and define one exact predicate over the captured generation, target VPS, and effect cancellation. Check that predicate before terminal navigation, in payload and catch branches, and again inside the functional state setter. A same-VPS reload is therefore superseded by any later reload or mutation generation, not only by an effect cleanup.

Every normal route entry also captures a preview generation. When a cancellation deep link fetches a preview, route-owned blockers/warnings/digest may enter the payload only while both the route and preview owners remain current; an ordinary A2 refresh increments its generation before awaiting transport, so pending A2 already supersedes A1. Before a non-cancellation route payload clears cancellation fields or switches the Drawer, capture a newly incremented reset generation and clear only while it remains current. The reset removes both pending authority and already-committed preview/error/result state, preventing hidden blockers from continuing to drive page attention after the Drawer switches.

## 10. Anonymous embedding and strict unknown-field proof

The bounded Go/TS mirrors do not recursively model `encoding/json` field promotion. They therefore reject every anonymous embedded field unless its tag is exactly `json:"-"` with no options, including an unexported anonymous struct type with exported members. The real scoped handler `status` negative sends a valid idempotency key and contract-complete payload, asserts the exact `error: "invalid json"` body, and proves the idempotent repository path was never called.

## Verification model

- Go handler tests exercise the routed scoped endpoint and a repository spy, not only request struct unmarshalling.
- Vitest uses controlled deferred promises to deterministically order A/B requests and completions.
- Production owner lifetime is proved through real `VPSDetailPage` capability probing and real lazy Legacy, not only a direct-Legacy router harness. Tests require settle-driven authoritative convergence after returned-A remount, current-page Drawer close/reopen, and same-VPS query reload; companion orderings prove A settle cannot re-probe current B and current-view settle does not add a page probe.
- Same-VPS route load and route-owned cancellation preview use controlled A1/A2 deferred orderings, including A2-pending → A1-settle → A2-settle. Payload-reset coverage includes both late preview revocation and the reverse ordering where committed preview/error/result/attention exists before payload reset.
- Only the evidence-heavy real-entry remount test receives a documented scoped timeout; global Vitest timing is unchanged.
- The stale route success proof delays a second-stage request after detail has passed its first guard, then separately controls the interval between payload admission and functional updater execution; deleting either guard must make its owning regression RED.
- Archive late-success uses independent interleavings: stale A1 settles while A2 is pending to prove `finally` ownership, then a separate A2-complete → blocker-A1 settle case proves `then` data ownership without loading hiding the mutation.
- DTO parser negatives widen approved aliases on same and continuation lines, append same-line and later-line intersection suffixes, and add line/block-comment anonymous embeddings so both source mirrors fail closed for the exact reviewed bypasses.
- Parser quality negatives also cover block-comment DTO/alias shadows, exact struct-tag key identity, direct unsupported-Go-type rejection, and quoted alias literals containing escapes or embedded pipes.
- Focused RED/GREEN precedes broader package and repository gates.
- `make verify-web` runs under Node `22.23.1` as required by the project.
- Formal Chromium `npm --prefix web run test:e2e` is a separate required gate because `make verify-web` does not execute Playwright.

## Compatibility and rollback

Wire field names and successful payloads do not change. Requests that previously omitted required scoped fields or sent null to non-nullable fields change from silent defaults to `400`. No migration is introduced. Rollback is a feature-branch revert.

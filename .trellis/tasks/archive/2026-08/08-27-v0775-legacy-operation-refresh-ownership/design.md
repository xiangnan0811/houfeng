# Design: Legacy operation and refresh ownership

## Ownership dimensions

Two lifetimes must remain distinct:

1. **Transport lifetime** — a write sent for VPS A stays in flight even after leaving A. Its token lives until that promise settles.
2. **View lifetime** — route changes increment mutation generation. A response from the old view may release its own transport owner but cannot perform current-view side effects.

Represent a transport owner as:

```ts
type VPSWriteOperation =
  | 'facts'
  | 'decision'
  | 'link'
  | 'monitoring-create'
  | 'subscription'
  | 'validity-extension'
  | 'monitoring-unlink'
  | 'lifecycle'
  | 'cancellation'
  | 'experience'
  | 'service'
  | 'domain'

type VPSWriteOwner = {
  vpsId: string
  token: string
  generation: number
  operation: VPSWriteOperation
}
```

The exact union spelling may follow component conventions, but every network-write handler has a stable operation identity.

## Owner store and lifecycle

Use an external store with immutable Map snapshots, synchronous `begin`/`finish`, `subscribe`, and `getSnapshot`. It is keyed by VPS ID and contains the exact owner. `VPSDetailPage` constructs one store for its page-shell lifetime and passes it into each lazy Legacy mount across capability probing. Direct Legacy mounts create a local fallback; a module-global singleton is forbidden because it leaks across page instances/tests.

`beginVpsWrite(vpsId, operation)`:

1. rejects if the ref already has that VPS;
2. creates token and captures/increments the current mutation generation using the established mechanism;
3. writes the same owner to ref and reactive state;
4. returns the owner.

`finishVpsWrite(owner)`:

1. compares the stored owner token for `owner.vpsId`;
2. removes from ref only on exact match;
3. functional-updates reactive state and removes only the same token.

Route effects invalidate response generation and reset route-local drafts/notices as today, but do not clear the external store. `useSyncExternalStore` makes a remounted Legacy immediately observe the pending owner. This permits A and B to each have one pending write and preserves A pending when navigating back through the production gate.

At route-effect entry the page shell also snapshots an inherited owner token and subscribes to that store before issuing its capability probe. When the exact token disappears or changes, the listener advances the probe revision only if a current-route ref still equals the captured target. This creates one settle-driven authoritative reload for returned A after an old Legacy generation rejected its POST response; the subscription is cleaned up with the route effect, cannot make old A re-probe current B, and does not register when no owner existed at route entry.

Every route-effect instance, including `!vpsId` and query-skip early returns, installs cleanup that increments mutation generation and revokes latest-load authority. Normal load cleanup also marks its local fetch chain cancelled. The cleanup never deletes transport-owner maps; a late promise may release only its own `vpsId + token`.

## Rendering

Derive `currentWriteOwner` from the currently loaded detail VPS. Each form/button receives submitting state only when `currentWriteOwner.operation` matches its operation. Where the UI displays an affected monitoring instance ID, derive/store that payload with the owner rather than keep a globally clearable ID.

Delete component-level network submitting booleans after all consumers move. Pure read/loading flags such as latest-data loading remain local because they describe a different lifetime.

## Handler pattern

Every handler follows one shape:

```ts
const owner = beginVpsWrite(detail.vps_id, 'service')
if (!owner) {
  setError('上一次保存仍在进行，请稍后再试')
  return
}
try {
  await write()
  if (!mutationIsCurrent(owner.generation)) return
  await refreshServices(owner.vpsId, () => mutationIsCurrent(owner.generation))
  if (!mutationIsCurrent(owner.generation)) return
  // current-view notice/draft/drawer effects
} catch (nextError) {
  if (!mutationIsCurrent(owner.generation)) return
  setError(...)
} finally {
  finishVpsWrite(owner)
}
```

The precise ordering respects each existing handler. The invariant is that all view effects are generation-owned and finalization releases only the exact transport owner.

## Refresh commit gate

Extend each helper with an optional predicate:

```ts
refreshDetail(vpsId, ownsRefresh = () => true)
refreshServices(vpsId, ownsRefresh = () => true)
refreshDomains(vpsId, ownsRefresh = () => true)
```

Immediately before mutating state, inside the functional setter, require both:

```text
current.vpsId == requested vpsId AND ownsRefresh()
```

Mutation call sites pass their generation predicate. Route-load call sites use the default because their existing route request owner already controls that flow. Check ownership again after any awaited refresh before subsequent notice/drawer/navigation effects.

## Route transaction and cancellation preview

A normal route effect increments and captures `routeGeneration`, then defines one `routeIsCurrent` predicate over effect cancellation, current VPS identity, and the captured mutation generation. Terminal archive navigation, payload, catch, and both success/error functional state setters re-check that predicate at their own commit points. This prevents an old same-VPS load from overwriting a mutation-owned refresh without relying on effect cleanup.

The same entry captures `routePreviewGeneration`. A cancellation deep-link payload installs its preview only when that generation is still current; an ordinary preview increments its generation before awaiting the GET, so A2 owns the surface while pending and A1 cannot briefly commit. A non-cancellation payload increments a reset generation before clearing cancellation state or switching the Drawer. That reset both revokes pending preview transport commits and clears already-committed preview/error/result state so stale blockers cannot keep page attention alive behind another Drawer.

## Deterministic race tests

Use deferred promises whose resolvers are held by the test:

- service A pending → B service starts → A settles → B UI/owner unchanged → return A and observe A lifecycle;
- subscription A/B equivalent;
- for each detail/services/domains: old A refresh pending → route B → route A and resolve new refresh → resolve old refresh → new A data remains.
- query-driven reload skip → start deferred archive write → unmount → settle write → route remains at the replacement location;
- ordinary cancellation preview pending → close Drawer → settle stale preview → reopen → second preview owns rendered blockers/warnings;
- already-loaded preview → workbench close or Modal `onClose` → reopen → old preview is absent while a fresh GET owns loading/result.
- real `VPSDetailPage`/Legacy: A subscription pending → capability probe B → probe A-before-settle; return A's stale GET first, then settle POST and require one automatic authoritative reload with changed subscription/service/lifecycle. A separate B-current ordering proves A settle does not re-probe B.
- same-VPS route A1 pending → mutation/refresh or reload A2 commits → A1 success/error settles; latest detail/drawer/error/navigation remains unchanged, including the functional setter boundary.
- stale-success route A1 resolves detail while still current, then pauses one second-stage request. One ordering invalidates generation before payload admission to prove the outer guard; a separate controlled React update ordering admits the payload, invalidates generation before the queued functional updater runs, and proves the inner guard independently.
- cancellation deep-link route preview A1 pending → ordinary A2 starts and remains pending → A1 settles without committing → A2 settles and is the only digest/warnings/blockers. Separately, non-cancellation payload reset covers both a preview that settles late and preview/error/result/attention committed before the payload.

Assertions include request targets/counts, disabled state, visible drafts/drawers/notices, and final rendered data. No timers are used to create ordering. Only the evidence-heavy real-entry multi-remount case may use a documented scoped timeout; the global timeout remains unchanged.

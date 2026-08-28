# Design: archive review request ownership

## Why a separate owner

Archive review is a modal-local read performed before the archive write. It has a different lifetime from the per-VPS write owner: multiple opens can supersede one another before any mutation begins. Reusing the write lock would either over-lock reads or fail to distinguish close/reopen of the same VPS.

## Owner primitive

Add a monotonic ref:

```ts
const archiveReviewRequestRef = useRef(0)
```

Starting an archive review increments the ref and captures both the resulting `requestId` and `targetVpsId`. Define the ownership predicate at the call site or as a helper:

```ts
const ownsArchiveReview = () =>
  archiveReviewRequestRef.current === requestId &&
  currentVpsIdRef.current === targetVpsId
```

Opening a newer review increments first and thereby supersedes the old request. Closing the modal increments before clearing modal state. Route effect start and cleanup also increment, ensuring both forward navigation and effect teardown revoke authority.

## Guarded branches

The archive branch of `openLifecycleConfirmation` follows:

```ts
const requestId = ++archiveReviewRequestRef.current
const targetVpsId = detail.vps_id
setArchiveReviewLoading(true)
try {
  const review = await loadArchiveReview(targetVpsId)
  if (!ownsArchiveReview()) return
  setArchiveReview(review)
  // any derived confirm state
} catch (nextError) {
  if (!ownsArchiveReview()) return
  setArchiveReviewError(...)
} finally {
  if (ownsArchiveReview()) setArchiveReviewLoading(false)
}
```

All modal state touched by the async review follows the same predicate. Non-archive lifecycle confirmation that does not load review remains unchanged.

## Invalidation ordering

- **Close:** increment ID, then clear modal state. A late branch sees a different ID.
- **Route effect:** increment at entry before initializing the new VPS view, and increment again in cleanup if the component/effect is torn down.
- **Route payload reset:** when a completed same-VPS load closes lifecycle confirmation and clears review/error/loading, increment before the first reset so a review opened while that load was pending loses authority.
- **Reopen:** increment for the new request even when the VPS ID is unchanged; request ID, not VPS equality alone, resolves the ABA.

## Test matrix

Controlled deferred review calls cover:

1. A pending → route B → B pending → A resolves success: B stays loading with no A review and confirm disabled until B resolves.
2. A pending → B review owns dialog → A rejects: no B error/data/loading mutation.
3. A pending → close → reopen same A → first resolves/rejects/finalizes: second dialog remains owned and loading.
4. Current request success and current request error still render and finalize normally.
5. Same-VPS query reload pending → open review → payload settles/reset closes modal → late review rejects: no page-level error/loading commit.
6. Use two late-success orderings: settle stale A1 while A2 remains pending to prove only A2 can end loading, then independently settle eligible A2 before blocker A1 to prove stale data cannot replace A2 or disable confirm.

Assertions distinguish review content, error copy, confirm enabled state, and loading indicator, so a guard missing from any branch fails deterministically.

## Security boundary

This prevents stale client state from enabling or confusing a confirmation UI. The archive mutation endpoint remains responsible for current eligibility and authorization; no security decision relies on cached review data.

# Implement: archive review request ownership

## Preconditions

Complete and review `08-27-v0775-legacy-operation-refresh-ownership` first. Re-read the resulting `LegacyVPSDetail.tsx` ownership helpers before editing because both children touch the same component and test.

## TDD sequence

1. **RED — cross-route late success/finally**
   - Add deferred archive review responses for A and B.
   - Open A review, route to B, open B review, then resolve A.
   - Assert A data never appears, B confirm remains unavailable, and A finally does not end B loading.
   - Resolve B and assert the current owner completes normally.

2. **RED — late rejection**
   - Reject A only after B owns the dialog.
   - Assert no A error appears and B review/loading remain unchanged.

3. **RED — same-VPS close/reopen ABA**
   - Open review A1, close, reopen A2, then settle A1 before A2.
   - Cover both stale success and stale failure/finalization so request ID, not only VPS ID, is required.

4. **GREEN — exact modal request owner**
   - Add `archiveReviewRequestRef` and the exact ID+VPS predicate.
   - Increment on each review start, modal close, route effect entry, and effect cleanup.
   - Guard every review-derived state write in success, catch, and finally.
   - Keep current-owner behavior and archive write flow unchanged.

5. **Spec and regression pass**
   - Update bounded `.trellis/spec/web/vps-detail-ownership.md` with superseding modal-read ownership and separately guarded finalization. Keep `.trellis/spec/web/state-and-data.md` as an index pointer only; do not restore a duplicate full contract.
   - Run all Legacy tests after the focused cases to catch interaction with operation owners from the preceding child.

6. **RED/GREEN — same-VPS route payload reset**
   - Hold a same-VPS query reload, open a newer archive review, settle the route payload so it closes/resets the modal, then settle the review.
   - Increment request ID immediately before payload-driven lifecycle/review reset; use controlled late-rejection and two late-success cases. Keep A2 pending and settle A1 first for `finally` loading ownership; separately complete eligible A2 before blocker A1 for `then` data/confirm ownership.

## Focused commands

```bash
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test -- --run src/pages/vps-detail/LegacyVPSDetail.test.tsx
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run lint
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run build
```

## Review gates

- VPS ID alone is insufficient for close/reopen ABA.
- A single guard before await is insufficient; success, catch, and finally each check current ownership.
- Close and route change revoke authority before resetting visible state.
- Old finalization cannot clear a newer loading flag.
- Backend archive review/confirm remains authoritative and unchanged.
- Payload-driven modal reset revokes a review opened during the pending load before clearing visible state.
- The same-VPS payload-reset proof covers both async branches: a late rejection cannot leak error; a late success cannot restore review data; neither old finally may change loading.
- The late-success proofs are independent: A1-before-A2 is required for stale-finally ownership, while A2-before-blocker-A1 is required for visible stale-then data/confirm ownership.

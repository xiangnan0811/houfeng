# Implement: Legacy operation and refresh ownership

## TDD sequence

1. **RED — operation pending belongs to a VPS**
   - Add a deferred service-create test: start on A, route to B while A is unresolved, prove B is enabled and can issue its own request, then settle A and prove B owner/drawer remain unchanged.
   - Route back to A before or after settlement as needed to prove the A pending indicator follows A rather than the component.
   - Add the equivalent subscription case so the abstraction is exercised by a second operation and form.
   - Run only the new tests and record current global-boolean failures.

2. **GREEN — reactive per-VPS write owners**
   - Define the operation and owner types.
   - Evolve `writeLocksRef`, `beginVpsWrite`, and `finishVpsWrite` into exact-owner operations with a reactive per-VPS view.
   - Convert facts and decision first to protect existing reference behavior, then convert link, monitoring create, subscription, validity extension, unlink, lifecycle/archive, cancellation, experience, service, and domain.
   - Derive current-route submitting/disabled props from the owner; remove obsolete network submitting booleans and unconditional finalizers.
   - Run new service/subscription tests and the existing facts/decision/operation suite.

3. **RED — refresh A→B→A**
   - Add separate deferred tests for detail, services, and domains.
   - Resolve the new A generation first, then the original A request, and assert the old payload cannot overwrite current A.

4. **GREEN — generation-owned refresh commits**
   - Add the optional owner predicate to the three refresh helpers.
   - Check VPS ID and predicate inside functional state updates.
   - Pass `mutationIsCurrent(owner.generation)` from mutation-triggered refreshes and re-check before later effects.
   - Run the three ABA tests plus all Legacy component tests.

5. **Spec and cleanup review**
   - Update bounded `.trellis/spec/web/vps-detail-ownership.md` with transport-vs-view lifetime, exact-token finalization, per-VPS parallelism, and commit-time refresh ownership. Keep `.trellis/spec/web/state-and-data.md` as an index pointer only; do not restore a duplicate full contract.
   - Search the component for obsolete `set*Submitting(false)` and unowned post-await effects; classify any remaining loading flag explicitly as read-only/modal-local.

6. **RED/GREEN — early route cleanup**
   - Exercise the query-skip effect, start a deferred archive mutation, unmount the Legacy route, settle the mutation, and prove it cannot navigate.
   - Make every effect branch return cleanup that revokes mutation/archive/latest view authority without releasing transport owners.

7. **RED/GREEN — cancellation preview close/reopen**
   - Start an ordinary deferred preview, close the Drawer, settle the old response, assert before reopen that it did not alter page attention/state, then reopen and require a second GET/current result.
   - Cover an already-loaded preview through both the workbench close button and Modal `onClose`; reopen must clear cached A1 and fetch A2.
   - Increment `cancellationPreviewGenerationRef` on cancellation Drawer close before clearing feedback.

8. **RED/GREEN — production capability-gate remount**
   - Add a separate real-entry test without the existing Legacy mock: A subscription pending → B → A-before-settle through `VPSDetailPage` probing.
   - Assert A remains disabled with one POST/idempotency key, B is independently writable, and each exact token releases only on its own settle.
   - Hoist the external owner store to the persistent `VPSDetailPage` instance; keep direct Legacy fallback local and add store isolation/exact-release coverage.

9. **RED/GREEN — same-VPS route transaction ownership**
   - Capture one route generation and require it at terminal navigation, payload, catch, and the functional state commit so an old same-VPS load cannot overwrite a later mutation/refresh or reload.
   - Capture cancellation deep-link preview generation and add A1/A2 deferred coverage.
   - Invalidate cancellation preview authority before a route payload clears/switches its Drawer state; prove a preview opened while reload is pending cannot change page attention after reset.

10. **RED/GREEN — exercise both stale-success commit guards**
   - Move the stale A1 delay from the second detail GET to a detail-following request so A1 has already passed the earliest route guard.
   - First invalidate generation while that second-stage request is pending and prove the payload-admission guard blocks A1.
   - Separately queue A1's functional state updater while current, invalidate generation before React applies it, and prove the inner updater guard returns the current state. Record independent mutation REDs for each guard.

11. **RED/GREEN — settle convergence and bidirectional preview reset proof**
   - Extend the real-entry A→B→A case so returned A completes a stale GET before the old POST settles. POST settle must cause one current-route re-probe and render updated subscription/service/lifecycle, while the old Legacy generation still cannot commit directly.
   - Subscribe to an inherited exact token in the same page route effect before capability probing; on token disappearance/change bump revision only when `currentVPSIdRef` still matches. Add a B-current/A-settle probe-count regression and cleanup unsubscribe.
   - Make route preview A1 and ordinary A2 both deferred; start A2, settle A1 while A2 is pending, then settle A2. Mutation-test the await-before-generation defect and restore the begin-time increment.
   - Keep the pending-preview payload-reset case and add reverse ordering with committed blockers/warnings, error/result and page attention before route payload settle. Mutation-test preservation of visible cancellation state.
   - Apply a documented scoped timeout only to the multi-probe/remount integration test; do not raise global timeout.

## Focused commands

```bash
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test -- --run src/pages/VPSDetailPage.legacy-ownership.test.tsx src/pages/vps-detail/vpsWriteOwnerStore.test.ts src/pages/vps-detail/LegacyVPSDetail.test.tsx
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run lint
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run build
```

Use the repository's actual npm script names if they differ; Node 22 is mandatory.

## Review gates

- A single global owner or per-operation global booleans do not satisfy cross-VPS concurrency.
- Route invalidation must not release an unresolved transport owner.
- Finalization must compare exact token in both ref and reactive state.
- Every post-await current-view effect is generation-owned.
- Refresh ownership is checked at commit time and covers detail, services, and domains.
- Archive review GET ownership remains untouched for the next sequential child.
- Early-return effect cleanup invalidates mutation generation; transport owner cleanup remains exact-token and promise-owned.
- Cancellation Drawer close invalidates preview generation and cannot reuse a late stale preview.
- Production capability probing may unmount Legacy but must not recreate its transport store; the real-entry regression must not mock Legacy.
- A same-VPS route load is generation-owned even without effect cleanup; its payload/catch/functional commit and any deep-link cancellation preview cannot overwrite newer authority.
- Runtime evidence, not only source inventory, must reach the second-stage payload and separately the commit-time functional updater boundary.
- Inherited-owner settle revalidation must be page-scoped, exact-token observed, and current-route-ref gated; it must not re-probe B from an old A closure or loop after owner removal.
- Preview supersession proof must acquire A2 generation before await, and payload reset proof must clear already-committed cancellation state as well as late transports.

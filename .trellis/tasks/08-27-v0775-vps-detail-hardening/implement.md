# Implement: v0.77.5 VPS detail hardening

## Checkout

- Worktree: `/home/murray/code/houfeng/.worktree/v0776-vps-hardening`
- Branch: `codex/v0776-vps-hardening`
- Base: exact reviewed commit `6c91b128a621e0adf0b2ce2e6434ebc3ad758340`
- Versioned hooks are enabled with `sh scripts/setup-git-hooks.sh`.

## Execution order

1. Start and complete child `08-27-v0775-scoped-subscription-required-contract` using its TDD plan.
2. Start and complete child `08-27-v0775-legacy-operation-refresh-ownership`.
3. Rebase the archive-review reasoning on the resulting Legacy component, then start and complete child `08-27-v0775-archive-review-request-ownership`.
4. Run parent integration review and broad gates. Do not overlap children 2 and 3 because they own the same files.
5. Apply review remediation in two independent TDD lanes: Legacy cleanup/preview ownership and strict DTO parser classification.
6. Integrate bounded Trellis ownership/verification contracts, correct examples, and rerun parent review plus all gates.

## Parent integration checklist

1. Review the final diff against the three acceptance boundaries; reject unrelated parser/framework/schema work.
2. Confirm scoped and collection subscription handlers still use distinct request boundaries.
3. Confirm every Legacy mutation call site pairs one returned owner with token-safe finish and gates post-await effects.
4. Confirm route invalidation does not discard in-flight transport owners.
5. Confirm refresh predicates are checked at commit time, not only before awaiting.
6. Confirm archive review guards success, failure, and finalization and invalidates on route/close/reopen.
7. Update `.trellis/spec/backend/subscription-cost-center.md` and bounded `.trellis/spec/web/vps-detail-ownership.md`; keep `state-and-data.md` as a navigation pointer rather than a duplicate owner contract.
8. Confirm every route-effect branch installs mutation/archive/latest cleanup without clearing transport owners.
9. Confirm cancellation Drawer close invalidates preview generation and pending/already-loaded previews re-fetch after both close controls.
10. Confirm TS and Go mirror parsers reject mixed primitive unions, `undefined`, unknown Go DTO types, and missing/null manifest semantic keys rather than guessing.
11. Confirm parent and both web child JSONLs inject bounded ownership/verification contracts without max-byte warnings.
12. Confirm `VPSDetailPage` owns the production write-owner store across probing remounts; no module-global singleton or Legacy-instance-only production registry remains.
13. Confirm route payload reset increments archive review request ID before clearing lifecycle/review state.
14. Confirm both DTO mirrors reject exported fields without usable JSON tags and Go rejects unknown named pointers before unwrapping.

## Review-remediation TDD sequence

1. **RED/GREEN — route cleanup and preview**
   - After a query-driven effect skip, start a deferred archive write, unmount, settle it, and prove no stale navigation or view commit.
   - Start a deferred ordinary cancellation preview, close, settle the old response, reopen, and prove a second GET owns the rendered result; repeat with an already-loaded preview and both close controls.
2. **RED/GREEN — contract parser fail-closed**
   - Add direct TS and Go-mirror negatives for `number | string`, `string | undefined`, unknown, and empty union members; also reject unknown Go DTO types and missing/null manifest semantic keys.
   - Replace keyword containment with exact member classification; do not add AST/codegen dependencies.
3. **Bounded context/spec reconciliation**
   - Extract canonical Legacy ownership scenarios to a bounded web spec and link it from `web/index.md` / `state-and-data.md`.
   - Add a bounded task verification contract and replace oversized JSONL injections for the parent and two web children.
   - Correct the mutation example and `vpsId + token` release wording.
4. **RED/GREEN — production gate owner lifetime**
   - Mount real `VPSDetailPage` and real Legacy; A subscription pending → B → A-before-settle must keep A disabled with exactly one POST/idempotency key while B remains independent.
   - Move the store to the persistent page-shell lifetime, retain a direct-Legacy fallback, and unit-test page-instance isolation plus exact-token release.
5. **RED/GREEN — route payload/review supersession**
   - Interleave same-VPS query reload, a newer archive review, payload reset, then late review settle; reset must invalidate request authority first.
6. **RED/GREEN — JSON tag and pointer fail-closed**
   - Reject exported fields with missing/empty/empty-name tags in both mirrors, ignore only exact dash, and reject unknown named pointers before `Elem()`.
7. **RED/GREEN — same-VPS route load and cancellation authority**
   - Capture a route generation and check it in terminal navigation, payload, catch, and functional state commit; prove an old same-VPS route request cannot overwrite a later mutation/refresh or reload.
   - Capture route-owned cancellation preview generation; prove route A1 cannot overwrite A2 digest/warnings/blockers.
   - Before payload-driven Drawer/cancellation reset, invalidate any preview opened while the route load was pending; prove its late settle cannot change page attention/state.
   - Add the missing same-VPS query-reload archive late-success/data case alongside the existing rejection/finally proof.
8. **RED/GREEN — anonymous embedding and strict status rejection**
   - Add Go/TS mirror negatives for exported and unexported-type anonymous fields; reject every non-dash embedding and allow only exact `json:"-"`.
   - Strengthen the scoped `status` handler regression with a valid idempotency key, exact `error: "invalid json"` response, and zero repository/idempotency calls.
9. **Focused matrix reconciliation**
   - Include the real `VPSDetailPage` ownership test and owner-store unit test in the parent bounded verification contract and Legacy child focused command.
10. **Independent spec and quality reviews**, followed by fresh parent integration gates.
11. **RED/GREEN — final proof and bounded-parser hardening**
   - Replace the stale-success route fixture's deferred detail with a deferred second-stage request, then add a separate controlled payload-admitted/updater-delayed interleaving. Record mutation REDs for the outer payload guard and inner functional setter guard independently.
   - Keep archive A2 pending, settle stale A1 first, assert A2 loading/confirm authority remains, then settle A2 for a stale-finally mutation RED. Separately complete eligible A2 before settling blocker A1 and record a stale-then data/confirm mutation RED.
   - Validate `BillingPeriodUnit` / `RenewalMode` definitions as string-literal-only aliases in both TS and Go mirrors; add widened-alias negatives.
   - Reject non-semicolon object suffixes (including intersections) in both mirrors and handle/reject trailing inline comments before anonymous Go-field classification.
   - Repair `research/review-evidence.md` ownership/idempotency pointers, sync bounded specs/contracts, then run sequential spec and quality review.
   - During quality review, fail closed on commented DTO/alias shadow markers, exact-match Go struct-tag keys, unsupported named Go types, and quote/escape-aware alias literals; add direct REDs before each helper fix.
12. **RED/GREEN — multiline declaration and block-comment parser closure**
   - Add direct TS/Go-mirror negatives for a closing brace followed on a later line by `& { debug?: string }`; reject a later significant `&` / `|` after whitespace/comment trivia unless a semicolon terminated the object declaration.
   - Add `BillingPeriodUnit` and `RenewalMode` continuation-line `| number` / `| undefined` negatives in both mirrors; multiline alias continuation may be rejected wholesale but must never validate only the first line.
   - Add TS Go-source negatives for `embeddedFields /* promoted fields */` and unterminated external block comments, plus a positive proving `/* */` inside raw struct tags remains data.
13. **RED/GREEN — stale-view settle convergence and final preview proofs**
   - In the real entry test, let returned A finish its stale route GET while the original POST remains pending; settle the POST only after asserting duplicate-submit remains blocked, then require an automatic third A load with updated subscription/service/lifecycle. Exact store release plus a stale owning generation notifies the page, which bumps revision only while mounted and still displaying the target VPS.
   - Add the companion current-B ordering: settle an unmounted A write and prove B heading/data and capability-probe count remain unchanged.
   - Change the route preview proof to two deferred responses ordered A2 pending → A1 settle → A2 settle; record a mutation RED with begin-time generation acquisition removed, then restore it.
   - Preserve the late-preview payload-reset regression and add its reverse: commit blockers/warnings plus error/result/attention first, then settle the route payload and require all cancellation state to clear. Record a visible-state-clear mutation RED.
   - Give only the multi-remount real-entry evidence test a documented scoped timeout; keep the global timeout unchanged and rerun the complete relevant files under Node 22.
14. **RED/GREEN — same-route stale-view settle convergence**
   - With real `VPSDetailPage` + real Legacy, start one subscription POST, close/reopen the Drawer while pending, preserve the pre-write snapshot until settle, then require an automatic authoritative reload with exactly one POST and one idempotency key.
   - In a separate real-entry ordering, complete a same-VPS query reload while the POST remains pending; after POST settle require one further authoritative reload and the updated server subscription, again without duplicate POST/key.
   - Make `VPSWriteOwnerStore.finish(owner)` return `false` for a stale token and `true` for exact release. The Legacy central finalizer notifies Page only after exact release when its mutation generation is stale; Page ignores notifications after unmount or for another current VPS.
   - Add the negative proof: a normal current-view subscription settle remains owned by Legacy, preserves its local success path, and does not increase page overview/detail probe calls. Record a mutation RED with the stale-generation condition removed, then restore it.

## Final validation

```bash
go test ./internal/center/http/handlers -count=1
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test -- --run src/lib/vpsSubscriptionCreateContract.test.ts src/pages/VPSDetailPage.legacy-ownership.test.tsx src/pages/vps-detail/vpsWriteOwnerStore.test.ts src/pages/vps-detail/LegacyVPSDetail.test.tsx
make fmt-go
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin make verify-web
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test:e2e
make verify-go
git diff --check
```

If the unchanged attachment PNG golden test still reports the recorded digest mismatch, preserve its exact output as a pre-existing baseline exception; all task-related packages must nevertheless be green. Any new or changed failure blocks completion.

## Review and delivery boundary

After implementation, run Trellis quality review before any completion claim. Commit, push, PR, CI monitoring, merge, and release handling require explicit user authorization and are not implied by approval of this implementation plan.

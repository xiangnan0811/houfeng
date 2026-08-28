# Review evidence and baseline

## Reviewed snapshot

- Source review: attached `v0.77.5` comprehensive review.
- Commit: `6c91b128a621e0adf0b2ce2e6434ebc3ad758340`.
- Worktree and branch were created directly from that commit. All implementation/remediation remains as unstaged working-tree changes on the feature branch; HEAD is still the reviewed base.

## Confirmed P2 evidence

### Scoped subscription request

- `internal/center/http/handlers/vps_subscription_create_fields.json` marks eight fields required and only `started_at` / `renew_at` nullable.
- `internal/center/http/handlers/subscriptions.go` currently decodes required fields into plain scalars, so missing keys collapse to zero values before normalization/validation.
- `internal/center/subscriptions/types.go` already provides `OptionalString`, `OptionalFloat`, `OptionalInt`, `OptionalBool`, and `OptionalDate` with presence tracking; reuse avoids a second JSON-presence abstraction.
- Existing UI builders always send required keys and intentionally allow empty `payment_method` / `note`, so presence must not be confused with non-empty content.

### Legacy operation and refresh ownership

- `web/src/pages/vps-detail/LegacyVPSDetail.tsx` has a per-VPS write token ref, but operation-specific submitting state is component-global and several `finally` blocks clear it unconditionally.
- `refreshDetail`, `refreshServices`, and `refreshDomains` compare only the current VPS ID before state commit. A→B→A therefore passes the ID check despite belonging to an old generation.
- The existing mutation generation mechanism is the authoritative view-owner signal and should be reused rather than duplicated.

### Archive review ownership

- `openLifecycleConfirmation` awaits archive review and mutates review/error/loading without a request owner.
- Route changes and modal close clear visible state but do not prevent late success, failure, or finalization from mutating a later dialog.

## P3 follow-up boundary

The Go/TypeScript contract test parses source text and is fragile under formatting/type-shape changes. The P2 request-wrapper change requires a small parser mapping update, but AST/codegen/parser redesign is intentionally excluded and should become a separate P3 task after the runtime contract is fixed.

## Relevant active-spec constraints

- `.trellis/spec/web/vps-detail-ownership.md:5-126` is the bounded authority for generation-owned route/mutation commits, transport owners that survive Drawer and capability-gate remount lifetimes, inherited-owner settle revalidation, per-VPS isolation, and exact `vpsId + token` release in `finally`.
- `.trellis/spec/web/vps-detail-ownership.md:128-204` is the bounded authority for archive-review and cancellation-preview supersession, including same-VPS ABA, request-start preview generation, payload reset, and loading/finalizer ownership.
- `.trellis/spec/backend/subscription-cost-center.md:34-35` keeps collection and VPS-scoped request bodies distinct while both use caller-owned idempotency keys and the same idempotent create service; `.trellis/spec/backend/subscription-cost-center.md:118-252` owns the scoped Go/TypeScript/manifest alignment and presence contract. Presence enforcement must not rotate keys or change payload ownership.
- `.trellis/spec/backend/quality-guidelines.md` defines `make verify-go`; `.trellis/spec/web/quality-guidelines.md` defines `make verify-web` and pins Node through `.node-version`/Node 22. Focused tests supplement but do not replace those gates.

## Baseline verification on the exact commit

- `make verify-web` under Node `22.23.1`: 203 test files and 1457 tests passed; lint, coverage, strict build, JS/CSS/font budgets passed. `npm ci` reported five existing high-severity audit findings; no automatic dependency mutation was authorized.
- `make verify-go`: task-related packages, including `internal/center/http/handlers`, passed. The all-repository gate failed only at `internal/center/attachments TestPreviewImageGoldenMetadataFreeBoundedPNG`: got `0d749fd4e5010a847bd9b8872b56cf56049caa705d45616e4a823cc2a4768c6e`, want `dac4e6f598e26f4dcfb32ea88f81375f42a14739719a9761db54160b1267ed9d`.
- That digest mismatch exists before task edits and is outside this scope. It remains visible in the final gate rather than being silently blessed or repaired.

## Post-implementation review remediation

- Review finding I1: the query-skip route effect returned archive-only cleanup; a later mutation could retain its generation after unmount and still navigate. Required regression: deferred mutation started after the skip, then unmount before settle.
- Review finding I2: both source parsers classified TypeScript unions by substring, allowing `number | string` and `string | undefined` to match the manifest. The bounded fix is exact-member fail-closed classification plus direct TS/Go negatives, not an AST rewrite.
- Review finding I3: `state-and-data.md` is 163087 bytes while per-file injection defaults to 32768 bytes; the ownership scenarios begin after the cutoff. Extract a bounded canonical ownership spec and task verification contract, then rewire parent/web-child JSONLs.
- Review finding M1: cancellation Drawer close did not increment preview generation, so a late ordinary preview could populate state and suppress the reopen refresh.
- Review finding M2: the ownership spec example called nonexistent/mismatched helpers and overstated release identity. Runtime release is map key `vpsId` plus unique token; generation/operation govern view/UI semantics.
- Review finding M3: the parent validation matrix omitted `npm --prefix web run test:e2e`; independent review ran the formal Chromium suite 133/133 green and the command must become mandatory.

## Subsequent review remediation

- Production capability probing unmounts/remounts Legacy; the transport store therefore moved to the persistent `VPSDetailPage` shell, with a real-entry A→B→A regression and local fallback for direct Legacy tests.
- A same-VPS route payload reset must invalidate archive review and cancellation preview owners before clearing their surfaces. Archive evidence must include separate late rejection and late success/data cases, not only a generic settle.
- A normal route load needs its own captured mutation generation at every commit point; effect-local `cancelled` alone does not stop an old same-VPS payload/catch from overwriting later mutation/refresh state.
- Cancellation deep-link route previews need a captured preview generation; non-cancellation payload reset must revoke a preview opened while the load was pending.
- DTO mirrors must reject missing/empty tags, unknown named pointers, and all non-dash anonymous embedding. The real scoped `status` negative must carry a valid idempotency key and prove the exact invalid-json/no-repository path.

## Parser continuation review and remediation

- External review found that both object mirrors validated only the closing-brace line, so a later-line `& { debug?: string }` continuation could add a legal TypeScript wire field without entering the manifest comparison. Both mirrors now inspect significant tokens after the complete declaration and reject later `&` / `|` continuation; direct multiline intersection negatives prove the former behavior RED.
- Both alias mirrors previously trusted the first line of `BillingPeriodUnit` / `RenewalMode`, allowing a following `| number` or `| undefined` widening to remain classified as string. Both sides now validate or fail closed over the complete alias declaration boundary, with continuation-line negatives for both approved aliases.
- The TypeScript-side Go-source mirror removed only line comments around anonymous fields, so `embeddedFields /* promoted fields */` could be tokenized as an unexported ordinary field and skipped. It now handles closed line/block comments outside raw tags, preserves comment markers inside raw struct tags, and rejects unterminated/multiline external block comments.
- Review verification recorded focused Go handler test/vet green, four focused Web files with 126 tests green, independent `make verify-web` with 205 files / 1525 tests and all build/budget gates green, and Chromium 133/133. Full Go remained limited only by the unchanged approved attachment PNG golden mismatch. This evidence records remediation, not later external-review clearance.

## Settle convergence and final preview-proof review

- External review finding I1: returned A could finish its route GET before the old POST, then keep a pre-write snapshot because old Legacy generation correctly rejected the POST response and the page-scoped store only unlocked. RED command: `env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin npm --prefix web run test -- --run src/pages/VPSDetailPage.legacy-ownership.test.tsx`; one test failed because `Tokyo Edge A converged` was absent and the DOM still showed `Tokyo Edge A`.
- Initial remediation I1 used a route-effect subscription to an inherited exact token before capability probe. Its real-entry GREEN required a third A detail load and changed subscription/service/lifecycle, while a companion test kept current B unchanged. The later same-route review below supersedes that inherited-owner-only mechanism with exact-release stale-generation notification, because a write can start after the route effect is already established.
- External review finding I2: the prior route-preview proof made A2 resolve immediately and did not prove request-start ownership. The test now controls both A1 and A2 and orders A2 pending → A1 settle → A2 settle. Isolated mutation RED removed A2's begin-time generation increment; the test failed while A2 was pending because A1 blockers/warnings replaced the loading surface. Production begin-time increment was restored.
- External review finding I3: the prior payload-reset proof covered only a preview that settled after reset, so it could not detect retained already-committed cancellation state. A reverse-order test now first retains a successful result, recovers a stale-preview error into committed blockers/warnings, and proves page attention exists; after a pending subscription route payload settles it requires preview/error/result/attention all absent. Isolated mutation RED preserved payload cancellation state and failed because `处理取消/退役` attention remained. Production reset was restored; the older late-preview case remains.
- External review finding I4: the evidence-heavy real-entry test exceeded the default five-second timeout once under full coverage. Only that controlled multi-probe/remount test now has a documented 15-second scoped timeout; the global timeout was not changed. Root integration review owns repeated `make verify-web` proof.
- External review finding M1: this file lacked the preceding parser-continuation findings and repair evidence. The section above records them, and this section records the current findings/remediation. No external-review clearance is claimed; a fresh review is still required before closeout.
- Implementer verification after final route-effect ordering: the three complete relevant Vitest files passed 83/83 under Node 22; full Web ESLint and strict TypeScript/Vite build passed; parent and Legacy-child `task.py validate` passed without context warnings; `git diff --check` and cached diff check passed.
- Parent integration verification then passed the four focused files 140/140, two consecutive full `make verify-web` runs at 205/205 files and 1539/1539 tests with coverage/build/bundle/CSS gates green, and formal Chromium 133/133. All five task directories validated without context warnings. This clears the recorded timeout instability locally; external-review clearance is still required before closeout or archive.
- Fresh Go verification passed `go test ./internal/center/http/handlers -count=1`, full `make vet-go`, and empty `gofmt -d` for the three changed handler files. `make test-go` failed only at the unchanged approved attachment PNG golden (`0d749fd4…` actual versus `dac4e6f5…` expected); every other reported package passed, so the baseline exception remains explicit rather than being described as an all-green repository gate.

## Same-route stale-view settle review

- External review finding I1: the inherited-owner-only Page subscription did not observe a write started after the route effect was established. Closing/reopening the Drawer or completing a same-VPS query reload invalidated the Legacy generation; the POST finalizer unlocked transport without any authoritative reload, leaving the page on its pre-write snapshot.
- TDD RED used real `VPSDetailPage` plus real lazy Legacy. The close/reopen and same-VPS query cases each failed because the authoritative `Tokyo Edge A converged` response was never rendered after POST settle, while the current-view negative already passed. The store unit regression separately failed because stale/exact `finish` returned `undefined` instead of `false`/`true`.
- Remediation: `VPSWriteOwnerStore.finish(owner)` now reports exact release. Legacy's central finalizer notifies Page only after exact release when its own mutation generation is stale; Page increments probe revision only while mounted and still displaying that VPS. The old inherited-token-only subscription was removed to prevent duplicate probes.
- GREEN requires one POST and one non-empty idempotency key through close/reopen and same-VPS query interleavings, preserves the old view while the POST is pending, and renders authoritative post-write data only after stale settle. A separate current-view case requires Legacy's local success path and unchanged overview/detail probe counts.
- Mutation proof removed the stale-generation condition so every exact release notified Page. The current-view negative became RED because the redundant re-probe replaced Legacy's local success state; restoring `released && !mutationIsCurrent(owner.generation)` restored the intended boundary.
- External review finding M1: the active closeout design/implement artifacts still described obsolete begin/finish/submitting APIs and optional Playwright. They now defer to the bounded ownership spec, describe exact-release stale-view notification, and require formal Chromium. This is remediation evidence only; the task remains `in_progress` pending fresh external review and is not authorized for archive.
- Root verification after this remediation passed the four focused Web files at 143/143, two consecutive `make verify-web` runs at 205/205 files and 1542/1542 tests with coverage/build/bundle/CSS gates green, formal Chromium at 133/133, the handler package test, full Go vet, empty `gofmt -d`, all five task validators without context warnings, and both diff checks. `make test-go` again failed only at the approved unchanged attachment PNG golden (`0d749fd4…` actual versus `dac4e6f5…` expected). These results make the changes ready for fresh external review; they do not close, archive, stage, commit, or otherwise complete any task.

## External review clearance

- On 2026-08-28 the independent external review explicitly reported that the review passed and authorized batched commits plus the complete PR/CI/release workflow. No Critical, Important, or Minor finding remains open in the accepted scope.
- This clearance satisfies the parent and remediation-tree archive gate. The tasks may be archived only after their work commits exist; PR creation is not the delivery endpoint, and release/image verification remains required before worktree cleanup.

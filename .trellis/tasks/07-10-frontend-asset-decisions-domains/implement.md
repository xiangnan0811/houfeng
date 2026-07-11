# Asset Decisions 领域拆分 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:executing-plans` in the approved inline mode, plus `trellis-before-dev`, `superpowers:test-driven-development`, `trellis-check`, and `superpowers:verification-before-completion`. Do not dispatch runtime subagents for this task.

**Goal:** 删除 2,705 行 Asset Decisions 总控，以七个 `{state, commands}` controller 和不可绕过的 AST contract 取代，同时保持所有网络、URL、DOM workflow 与 mutation 语义。

**Architecture:** URL selection is the single route truth. Six business controllers own their API/read/write/UI state; the route page wires typed results and a semantic `renewal-decision-saved` invalidation event. Presentation stays controlled and API-free.

**Tech Stack:** React 19, TypeScript 6 strict, react-router-dom 7, Vitest 4/jsdom, Testing Library, TypeScript compiler AST, Vite 8, local Chromium/CDP evidence.

---

## 0. Preconditions and Invariants

- Work only in `/home/murray/code/houfeng/.worktree/frontend-asset-decisions-domains` on `codex/frontend-asset-decisions-domains`.
- Run `sh scripts/setup-git-hooks.sh`; never modify local/remote `main` directly.
- Start only after the user reviews the final `prd.md`, `design.md`, and this plan and `task.py start` succeeds.
- Baseline must remain 90 Vitest files / 673 tests or grow; any replacement must name the assertions it supersedes.
- No Go, API wire shape, package, lockfile, CI, CSS, user copy, route, or business-action changes.
- Every production function/hook begins with a failing test or failing architecture assertion, followed by minimum implementation, focused GREEN, refactor, and full regression.
- After every domain commit, `AssetDecisionsPage.test.tsx` and the new owner test must pass. Do not accumulate several red domains.

## 1. Activate and Freeze the Observable Baseline

**Files:**

- Modify `.trellis/tasks/07-10-frontend-asset-decisions-domains/task.json`
- Modify `web/src/pages/AssetDecisionsPage.test.tsx`
- Create `web/src/pages/asset-decisions/testFixtures.ts`
- Create `web/src/pages/asset-decisions/requestContracts.test.tsx`

- [ ] Run `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-10-frontend-asset-decisions-domains` and confirm status `in_progress`, branch `codex/frontend-asset-decisions-domains`, base `main`.
- [ ] Move fixture builders, `mockInitialWorkbench`, fetch lookup helpers and URL probe from the 3,069-line page test into `testFixtures.ts` without changing their values or response routing.
- [ ] Run the existing page test after the mechanical move:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/AssetDecisionsPage.test.tsx
```

Expected: all 36 current tests PASS; this move does not change production behavior.

- [ ] Add characterization assertions named exactly:
  - `issues the exact eleven-request initial inventory once`
  - `preserves overview rows when groups fail and group rows when overview fails`
  - `preserves URL context while opening and closing each entity type`
  - `preserves local mutation refreshes as returned state plus four filtered reads and the opened detail`
  - `replays the compatibility refresh inventory after a renewal decision`
  - `does not write before nested member/template confirmation`
- [ ] Count GET calls by normalized `{method,url}` and compare multisets, not incidental Promise resolution order. Renewal assertions cover queue use and a group-open use so optional detail refresh is explicit.
- [ ] Run `requestContracts.test.tsx` alone and confirm all characterization assertions PASS against the unmodified Content. If an assertion fails, correct the documented baseline before any production edit; do not alter production to satisfy an assumed contract.
- [ ] Add an architecture RED assertion using `import.meta.glob` that requires the seven controller entry paths. Run it and confirm FAIL lists all seven missing paths.
- [ ] Commit the test-only baseline:

```bash
git add web/src/pages/AssetDecisionsPage.test.tsx web/src/pages/asset-decisions/testFixtures.ts web/src/pages/asset-decisions/requestContracts.test.tsx web/src/security/assetDecisionArchitectureContract.test.ts
git commit -m "test(asset-decisions): freeze domain contracts"
```

**Rollback:** revert this commit only if a characterization assertion is proven to describe something other than current observable behavior.

## 2. Extract the Sole Route-State Owner

**Files:**

- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionRouteState.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionRouteState.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/utils.ts`
- Modify `web/src/pages/asset-decisions/types.ts`

- [ ] Satisfy only the path-existence architecture assertion with a named exported hook shell; keep its result typed, then add behavioral tests so the shell fails assertions rather than failing module resolution.
- [ ] Write route tests for default/legacy/invalid view, renewal window, all six filters, open-key priority, `openEntity` exclusivity, `closeEntity` preservation, local secondary selection, deep-link-derived secondary, and history back/forward.
- [ ] Run the route test and verify RED on returned state/URL, not a test syntax error.
- [ ] Implement `AssetDecisionOpenSelection`, `AssetDecisionRouteState`, and semantic commands from `design.md`. Move `portfolioViewForWorkbench`, context chip construction and filter-key construction to the existing pure owner (`utils.ts` where reusable).
- [ ] Replace Content's `useSearchParams`, `useNavigate`, four selected IDs, URL synchronization timer, and open/close query mutation with the route controller. Domain state remains in Content in this commit.
- [ ] Key adapters by `route.state.open` so a different entity ID cannot render prior detail while loading.
- [ ] Run focused tests:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/useAssetDecisionRouteState.test.tsx src/pages/asset-decisions/requestContracts.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

Expected: PASS; exact query/deep-link/secondary contracts unchanged.

- [ ] Commit:

```bash
git add web/src/pages/AssetDecisionsPageContent.tsx web/src/pages/asset-decisions/hooks/useAssetDecisionRouteState.ts web/src/pages/asset-decisions/hooks/useAssetDecisionRouteState.test.tsx web/src/pages/asset-decisions/utils.ts web/src/pages/asset-decisions/types.ts
git commit -m "refactor(asset-decisions): own route state in one hook"
```

**Rollback:** any query, deep-link, close, back/forward or secondary-workbench difference reverts this commit without touching later data owners.

## 3. Add Typed Invalidation and Extract Portfolio

**Files:**

- Create `web/src/pages/asset-decisions/hooks/invalidation.ts`
- Create `web/src/pages/asset-decisions/hooks/invalidation.test.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionPortfolio.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionPortfolio.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`

- [ ] Write pure RED tests that start from six zero revisions, apply `{type:'renewal-decision-saved',vpsID}`, and expect every approved domain to increment once without mutating the input. An unknown event must be impossible at compile time; do not add a default arbitrary-target API.
- [ ] Implement the event/revision reducer and initial revisions.
- [ ] Write portfolio RED tests for initial loading, filtered overview request, success, error, cancelled stale response, filter change, local retry and external revision.
- [ ] Implement `useAssetDecisionPortfolio({filter, revision})` with one read effect and `{state,commands}`. Keep existing fallback message.
- [ ] Replace only overview ownership in Content; adapt `PortfolioWorkbench`'s existing `PortfolioState` from portfolio + still-local group state.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/invalidation.test.ts src/pages/asset-decisions/hooks/useAssetDecisionPortfolio.test.tsx src/pages/asset-decisions/requestContracts.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

- [ ] Commit `refactor(asset-decisions): extract portfolio controller`.

**Rollback:** revert if overview request, partial error or reload count changes.

## 4. Extract Automatic Groups

**Files:**

- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionGroups.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionGroups.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/tableColumns.tsx`

- [ ] Write RED tests for filtered list, keyed detail, selected ID change, renewal-window detail query, list/detail independent errors, external revision, panel reset and cancelled response.
- [ ] Implement list/detail effects and semantic `selectPanel`/`resetDetailUI` commands. Do not put create-manual or save-record mutations in this controller.
- [ ] Replace local portfolio group list/detail/panel state and use the existing `createMemberColumns` factory instead of Content's duplicate member columns.
- [ ] Prove `Promise.allSettled` compatibility at the composed page: overview failure keeps groups; groups failure keeps overview.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/useAssetDecisionGroups.test.tsx src/pages/asset-decisions/requestContracts.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

- [ ] Commit `refactor(asset-decisions): extract automatic group controller`.

**Rollback:** revert this commit if list/detail requests, group modal, partial failure or group-local panel state differs.

## 5. Extract Manual Groups

**Files:**

- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.test.tsx`
- Create `web/src/pages/asset-decisions/modals/ManualGroupDetailModal.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/modal-content/ManualGroupDetailModalContent.tsx`
- Modify `web/src/pages/asset-decisions/tableColumns.tsx`
- Modify `web/src/pages/asset-decisions/types.ts`

- [ ] Write RED read tests for filtered list, preserved newly-created summary per filter key, keyed detail, candidate `/api/vps`, independent list/detail/catalog failure, revision and cancelled response.
- [ ] Write RED mutation tests for exact auto→manual and template→manual inputs, metadata patch, member add, confirmation-gated delete, error retention, saving maps, draft reset and local list/detail merge. Assert no VPS write and no broad GET refresh.
- [ ] Implement a single private `applyDetail` that derives/upserts the manual summary. Expose typed semantic commands; return created detail or `null` for cross-domain transitions.
- [ ] Change modal props from raw dispatch to `onUpdateMemberAddDraft(patch)`, `onUpdateRecordDraft(patch)`, `onSetMemberAddAdvanced(visible)` and semantic submit callbacks. Keep the same inputs, labels, roles and DOM order.
- [ ] Replace Content manual/list/detail/catalog/mutation state and use `createManualMemberColumns`.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/useAssetDecisionManualGroups.test.tsx src/pages/asset-decisions/modals/ManualGroupDetailModal.test.tsx src/pages/asset-decisions/requestContracts.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

- [ ] Commit `refactor(asset-decisions): extract manual group controller`.

**Rollback:** any payload, returned-detail merge, confirmation, catalog error, draft or modal transition difference reverts this domain only.

## 6. Extract Scenario Templates

**Files:**

- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionTemplates.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionTemplates.test.tsx`
- Create `web/src/pages/asset-decisions/modals/TemplateDetailModal.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/modal-content/TemplateDetailModalContent.tsx`

- [ ] Write RED tests for list/detail isolation, keyed draft, built-in patch guard, custom archive/restore confirmation, create-from-manual payload, local list/detail merge, opened-detail GET and error retention.
- [ ] Implement template controller with semantic draft patch commands and returned detail for manual→template transitions.
- [ ] Keep template→manual mutation in the manual controller; the template controller supplies current template/draft state only.
- [ ] Replace raw dispatch props in `TemplateDetailModalContent` with typed patch callbacks; keep nested confirmation behavior unchanged.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/useAssetDecisionTemplates.test.tsx src/pages/asset-decisions/modals/TemplateDetailModal.test.tsx src/pages/asset-decisions/requestContracts.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

- [ ] Commit `refactor(asset-decisions): extract template controller`.

**Rollback:** revert if built-in guard, payload, nested focus, local summary or manual/template route transition changes.

## 7. Extract Records and Record Presentation

**Files:**

- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionRecords.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionRecords.test.tsx`
- Create `web/src/pages/asset-decisions/components/RecordDraftMemberRows.tsx`
- Create `web/src/pages/asset-decisions/components/RecordExecutionBoard.tsx`
- Create `web/src/pages/asset-decisions/components/RecordFollowupRows.tsx`
- Create corresponding component tests
- Create `web/src/pages/asset-decisions/modals/RecordDetailModal.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/modals/RecordDetailModal.tsx`
- Modify `web/src/pages/asset-decisions/tableColumns.tsx`

- [ ] Write RED controller tests for list/detail, auto/manual draft construction and preservation, exact record POST, keyed transition, record status PATCH, member follow-up PATCH including empty note, per-member saving/error, local list/detail counters and no business-object writes.
- [ ] Implement record controller and reuse `recordDrafts.ts`/`buildRecordFollowupDrafts`; do not move JSX into the hook.
- [ ] Write RED component tests for preview limit, edit/patch callbacks, execution CTA mapping and quick follow-up callback, then move the three closure-heavy render functions into controlled components without DOM/text changes.
- [ ] Replace raw record setters in group/manual/record modals with semantic patch callbacks and use existing record/member column factories.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/useAssetDecisionRecords.test.tsx src/pages/asset-decisions/components/RecordDraftMemberRows.test.tsx src/pages/asset-decisions/components/RecordExecutionBoard.test.tsx src/pages/asset-decisions/components/RecordFollowupRows.test.tsx src/pages/asset-decisions/modals/RecordDetailModal.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

- [ ] Commit `refactor(asset-decisions): extract record controller`.

**Rollback:** revert on POST/PATCH, draft alignment, execution CTA, follow-up counters, preview density or modal differences.

## 8. Extract Renewal Queue and Wire Invalidation

**Files:**

- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.ts`
- Create `web/src/pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.test.tsx`
- Create `web/src/pages/asset-decisions/modals/RenewalDecisionModal.test.tsx`
- Modify `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/businessLogic.ts`

- [ ] Write RED tests for the two subscription reads, three decision slices, partial renewal-vs-queue failure, candidate derivation, selected VPS/draft reset, exact PATCH with omitted empty reason, linkage merge and one semantic invalidation event.
- [ ] Implement renewal controller. Keep `buildDecisionQueue`, `filterDecisionQueue`, `updateDecisionQueues` and related calculations pure; move missing pure helpers into `businessLogic.ts` instead of copying them.
- [ ] Wire invalidation revisions into all six domain inputs. Assert a renewal success reproduces the exact 11-GET multiset and optional currently-open detail GET once.
- [ ] Replace decision raw setter prop with a typed draft patch/value callback. Preserve nested group renewal behavior and standalone queue modal behavior.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/asset-decisions/hooks/useAssetDecisionRenewalQueue.test.tsx src/pages/asset-decisions/hooks/invalidation.test.ts src/pages/asset-decisions/requestContracts.test.tsx src/pages/asset-decisions/modals/RenewalDecisionModal.test.tsx src/pages/AssetDecisionsPage.test.tsx
```

- [ ] Commit `refactor(asset-decisions): extract renewal queue controller`.

**Rollback:** revert if the PATCH, linkage notice, subscription-failure semantics, queue movement or compatibility refresh multiset differs.

## 9. Collapse the Route Page and Delete the Total Controller

**Files:**

- Rewrite `web/src/pages/AssetDecisionsPage.tsx`
- Delete `web/src/pages/AssetDecisionsPageContent.tsx`
- Modify `web/src/pages/asset-decisions/components/PortfolioWorkbench.tsx`
- Modify `web/src/pages/asset-decisions/components/SecondaryWorkbenches.tsx`
- Modify five modal/content files only for final semantic-prop wiring

- [ ] Add a RED architecture assertion that route page has no API import, `useEffect`, `useSearchParams`, `React.Dispatch` or `SetStateAction`, and that no production `*PageContent` exists.
- [ ] Move the remaining page header, notice, derived model composition and controller wiring into `AssetDecisionsPage.tsx`. API response parsing, request bodies and local mutation merges must remain in controllers.
- [ ] Use controller values and semantic callbacks to render the two workbenches and five modals in exactly the existing DOM order.
- [ ] Delete Content and search for duplicated former owners:

```bash
rg -n "AssetDecisionsPageContent|setRefreshToken|useSearchParams|from ['\"].*lib/api" web/src/pages/AssetDecisionsPage.tsx web/src/pages/asset-decisions
```

Expected: no Content/refresh token; router hook only in route-state file; API imports only in approved controller files and `utils.ts`'s existing `ApiError` type/value use.

- [ ] Run the complete Asset Decisions focused set:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/AssetDecisionsPage.test.tsx src/pages/asset-decisions src/security/assetDecisionArchitectureContract.test.ts
```

- [ ] Commit `refactor(asset-decisions): compose page from domain controllers`.

**Rollback:** restore the previous composition commit if DOM or browser behavior changes; do not reintroduce cross-domain setters into controllers.

## 10. Split Test Ownership and Finish the AST Contract

**Files:**

- Modify `web/src/pages/AssetDecisionsPage.test.tsx`
- Create/move domain workflow tests under `web/src/pages/asset-decisions/`
- Create `web/src/pages/asset-decisions/businessLogic.test.ts`
- Complete `web/src/security/assetDecisionArchitectureContract.test.ts`

- [ ] Move tests by the ownership map in `research/baseline.md`; after each file move, run source and destination files and confirm the same named workflows pass before changing assertions.
- [ ] Add pure-model tests for decision queue, partial metrics, next work, portfolio lead, manual progress and invalidation mapping.
- [ ] Implement AST import/call inventory with synthetic fixtures. Assert exact controller entries, API symbol whitelist, sole router owner, forbidden dependency edges, no `*PageContent`, page/controller/global line budgets and controller effect budget.
- [ ] Ensure every AST failure reports path, line and forbidden symbol/edge/budget. No path-line whitelist or regex import parser.
- [ ] Run:

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/AssetDecisionsPage.test.tsx src/pages/asset-decisions src/security/assetDecisionArchitectureContract.test.ts
```

Expected: all moved workflows and architecture tests PASS; total repository tests ≥673.

- [ ] Commit `test(asset-decisions): enforce domain ownership`.

**Rollback:** if a moved test loses assertions, restore it to the source file and repeat the mechanical move; do not weaken architecture budgets to make an oversized owner pass.

## 11. Full Quality and Browser Gates

- [ ] Run focused lint/test/build first:

```bash
NODE_ENV=test npm --prefix web run lint
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
npm --prefix web audit --include=dev
git diff --check
```

- [ ] Run the canonical clean-install gate:

```bash
env -u NODE_ENV make verify-web
```

Expected: toolchain preflight, clean install, lint, all Vitest tests, strict TS and production build PASS; audit reports 0 vulnerabilities.

- [ ] Start the production preview on a dedicated local port and run the repo-local geometry/diagnostic helper:

```bash
npm --prefix web run preview -- --host 127.0.0.1 --port 4178
TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:4178/ --mock-api asset-workflows --route /asset-decisions --viewport 1440x1000 --viewport 1024x768 --viewport 390x900
```

- [ ] Using local Chromium/CDP (outside repository dependencies), exercise automatic group open/close, auto→manual, member-remove nested confirmation cancel/confirm, template archive nested confirmation, record status/follow-up, and single-VPS renewal. Record text evidence in task research: viewport, URL sequence, request method/path/body, focus sequence, body overflow, document width, target client/scroll metrics, and page/console/CSP/network counters.
- [ ] Require diagnostics 0, no document horizontal overflow, no critical text clipping, one visible/top modal, correct inert parent, one Escape per layer, focus restoration and final body unlock.
- [ ] Do not commit screenshots or add Playwright/axe dependencies. Label fixture evidence `mock-api asset-workflows`; it is not staging proof.

## 12. Spec, Review, Delivery, Release, and Archive

- [ ] Run `trellis-check`; inspect full diff, deleted Content, API import inventory, test inventory and spec compliance.
- [ ] Use `trellis-update-spec` to update `.trellis/spec/web/{directory-structure,component-conventions,state-and-data,quality-guidelines}.md` with the proven Asset Decisions controller/architecture contract. Do not write aspirational rules before the implementation proves them.
- [ ] Run the full quality gate again after spec/task evidence edits.
- [ ] Record verification in `.trellis/tasks/07-10-frontend-asset-decisions-domains/research/verification.md`, update task branch/PR metadata, and commit documentation separately:

```bash
git add .trellis/tasks/07-10-frontend-asset-decisions-domains .trellis/spec/web web/src
git commit -m "docs(asset-decisions): record domain ownership contracts"
```

- [ ] Use `superpowers:requesting-code-review`, push `codex/frontend-asset-decisions-domains`, open a PR to protected `main`, monitor all required checks, and fix failures on the same branch.
- [ ] Merge only after required checks pass. Monitor post-merge main CI, Release Please PR/checks, GitHub Release and `publish-images`.
- [ ] Verify the published multi-arch `docker.io/linnea7171/houfeng:<version>` digest and inspect `/app/web/dist` from that exact digest. Re-run `/asset-decisions` browser geometry/diagnostic and core workflow smoke against the released dist.
- [ ] Update the task with PR, merge, release, digest and released-dist evidence; run the gate; archive Task 8 in a separate archive PR; monitor post-archive main CI.
- [ ] Only after archive/main CI is green, prepare Task 9 from the new protected `origin/main` baseline.

## Completion Checklist

- [ ] All PRD behavior/architecture/validation criteria are checked with evidence.
- [ ] Seven controllers and semantic invalidation exist; no raw setter crosses the boundary.
- [ ] Content total controller and all replacement loopholes are absent.
- [ ] Repository tests are ≥673 and all local/CI/released-dist gates pass.
- [ ] Worktree/branch are clean; no stale temporary browser process or generated screenshot artifact remains.

# Workbench visual density audit

Date: 2026-05-22
Preview URL: http://127.0.0.1:5178/
Data sources: `mock-api asset-workflows` for `/`, `/vps`, `/asset-decisions`; `mock-api observability-support` for `/nodes`, `/targets`.
Viewports: 1440x1000 and 390x900.
Screenshots: local-only under `research/screenshots/`, not committed.
Raw metrics: `research/visual-audit-raw.json`.

## Overall findings

The phase-three layout successfully moved pages away from simple vertical `page-panel` stacking, but the rendered result still feels chaotic because the control density and visual scales are not normalized.

- Top-level headers, metric boxes, toolbars, tab rows, priority strips, tables, and asides are all visually strong; too many elements compete in the first viewport.
- Buttons do not share a stable rhythm: header actions are 44px high, toolbar actions are 33px, row actions are 33px, sidebar/user controls are larger, and tab controls sometimes collapse into unexpected shapes.
- Toolbar placement differs by page: VPS puts filter controls at the far right above a broken tab block; Nodes uses a compact toolbar plus separate tab row; Targets renders a full filter form in the main scan path; Asset Decisions has no toolbar but a large tab rail.
- Aside content repeats the same metrics already present in the header, so the right column becomes a second dashboard instead of auxiliary context.
- Several list pages still include explanatory headings/subtitles that do not help the next action and add visual noise.

## Page findings

### Dashboard `/`

- Main story is readable, but header, primary card, list rows, refresh controls, and aside links still compete as several mini-surfaces.
- `table-workbench__toolbar` is a tiny stray 29x22 area near the main/aside boundary, which makes the layout feel accidental.
- The same primary action appears in the header and in the primary card, creating duplicate visual anchors.
- Refresh control is a full-width 322x33 button in the aside; it feels heavier than a secondary utility.
- Evidence numbers in the aside repeat header facts, making the right side feel like another dashboard.

### VPS `/vps`

- Critical failure: quick-view tabs are broken into vertical 28x93 controls inside a 127px-tall tab region. The first viewport reads as malformed layout rather than a workbench.
- Header metrics repeat again in the aside, making the page show the same three numbers twice above the table.
- The toolbar has a small `字段筛选未应用` label and a 94x44 filter button floating at the right; it does not form a stable action group.
- The table is visible, but the malformed tab band dominates the first viewport and makes the table feel pushed down.
- The aside stacks three compact metric cards after evidence cards, adding duplicate visual blocks.

### Nodes `/nodes`

- The core list page shape is closer to the target, but the first viewport has too many equal-weight pieces: header metrics, quick stats card, toolbar buttons, pill tabs, priority strip, table, and a detailed aside.
- Toolbar buttons are all 33px high while header CTA is 44px; this is acceptable across hierarchy, but the button group alignment is loose.
- Row action buttons appear in captured metrics at x=1241+ while the main region is 796px wide, indicating table action overflow / horizontal scan risk.
- The aside `资产判断支撑` has multiple strong cards and repeats inventory/evidence concepts, making it compete with the table.
- The priority strip is useful, but its dashed card styling and internal button add another high-weight frame above the table.

### Targets `/targets`

- Main failure: full field filters occupy the first scan path (`filter-bar` 796x125) before the priority strip and table. This restores the old “function controls first” problem.
- There are two create actions (`新建目标` in header, `创建 Target` in toolbar), which creates inconsistent action hierarchy and bilingual copy noise.
- Row action buttons overflow far beyond the main region (x=1490+), confirming horizontal layout instability.
- Aside support card is visually strong and explanatory; it competes with the main scan rather than supporting it.
- The table begins lower than Nodes because the filter form consumes a large block.

### Asset Decisions `/asset-decisions`

- This page is the most coherent of the five: queue is primary and visible.
- The header actions and renewal window selector form a wide mixed action strip; the selector sits beside links but has different visual weight.
- Queue rows are visually heavy with nested bordered cells and multiple badges; primary `处理` buttons are small 50x33 and get lost in dense rows.
- Aside repeats queue counts and evidence stats already present in the header/tabs.
- The tab rail is stable horizontally, unlike VPS, and can be used as the target pattern.

## Correction priorities

1. Fix VPS quick-view tab collapse immediately by making workbench tab rows horizontal, wrapping, and content-sized instead of column-compressed.
2. Remove the full Targets filter form from the main first viewport; use a compact toolbar trigger + chips, with advanced fields in Drawer or lower-weight summary.
3. Normalize list-page toolbar rhythm: left side = quick view / active chips; right side = one compact action group. Avoid free-floating helper labels and duplicate create actions.
4. Reduce aside duplication: remove repeated header metrics from asides and keep evidence/help/context only.
5. Stabilize table action overflow: keep row actions compact, wrap or hide overflow safely inside table cells, and do not let row buttons extend beyond the visible workbench.
6. Reduce duplicate primary actions in Dashboard and asset pages; one primary CTA should dominate, not multiple identical anchors.

## Correction verification

Recaptured after the visual correction pass on 2026-05-22 using the same mock profiles and viewports.

- Dashboard: stray `.table-workbench__toolbar` count is gone (`count: 0`); primary queue content remains in the 796px main column.
- VPS: quick-view tabs are horizontal again (`.table-workbench__tabs` 796x58 on desktop), and the aside only carries subscription evidence plus field-filter context.
- Nodes: table width is contained to the main column (`.data-table` 796px on desktop) after consolidating location into the identity column and shortening row action labels.
- Targets: full field filter no longer appears in the first scan path (`.filter-bar count: 0`); table and row actions are contained to 796px on desktop and 366px on mobile.
- Asset Decisions: renewal evidence table is not mounted by default (`.data-table count: 0` on desktop) and only appears after expanding the details disclosure.
- No route-level horizontal document overflow was observed in the recaptured desktop pages (`scrollWidth == clientWidth == 1440`).

Validation commands completed:

- `cd web && npx tsc --noEmit`
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npx vitest run src/pages/NodesPage.test.tsx src/pages/TargetsPage.test.tsx`
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npx vitest run src/pages/AssetDecisionsPage.test.tsx`
- `cd web && npm run lint`
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm run test -- --run`
- `cd web && npm run build`
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web`

`make verify-web` passed; npm emitted the existing environment warning that this package requires Node 22.x while the local runtime is Node v24.14.1, plus the existing npm audit moderate-severity notice.

## Evidence limitations

- Screenshots are local-only and untracked.
- Data is representative mock API data, not real inventory or backend correctness proof.
- Browser geometry and screenshot review reveal layout quality issues, but final visual acceptance remains human judgment.

## External-plan Dashboard P0/P1 checkpoint

The external frontend/UI review supersedes the earlier one-primary-block Dashboard correction. The current Dashboard direction is now a real workbench first screen rather than a single adaptive card: stable identity, compact metrics, three visible command lanes, and one primary current-task strip.

Required Dashboard shape for this checkpoint:

- Top identity answers `今天先处理什么？` instead of presenting a generic dashboard title.
- Compact metrics are limited to `待决策资产`, `异常对象`, `严重事件`, and `维护观察`; they are not restored as large KPI cards.
- Command Surface keeps three stable lanes: `资产决策队列`, `观测异常队列`, and `下一步动作`; the page structure no longer disappears based on priority mode.
- `当前需要处理` remains the leading task/CTA area, while lane content provides context without becoming separate page sections.
- The already rejected regressions remain absent: automatic refresh selector, `.dashboard-command-disclosure`, `已加载 /api/dashboard`, `Dashboard 摘要指标`, `系统快捷入口`, `Group 摘要`, and `最近事件摘要`.
- `相关路径`, `上下文`, and `管理` are not re-exposed in this slice; the next treatment of context rail should be deliberate, not an inline spillover.

Validation after this checkpoint:

- `cd web && npm run build` passed.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web` passed: lint, 66 Vitest files / 500 tests, and production build.
- Existing warnings remain environmental: local Node v24 while package expects Node 22.x, plus the existing npm moderate audit notice.

Visual review still needs human acceptance. The three-lane structure better matches the external plan, but the next visual risk to inspect is whether the lane grid is sufficiently readable at 1366px and whether it stacks cleanly at 390px without becoming another dense block pile.

### Dashboard mobile rhythm follow-up

After the first external-plan P0/P1 capture, the 390px Dashboard still had an internal mobile rhythm problem: the generated-time/refresh metadata sat beside the title block, narrowing the heading and pushing the primary task area down.

A Dashboard-only mobile refinement moved the generated timestamp and manual refresh into a full-width secondary meta row and tightened the compact metric / priority spacing. Recapture results:

- Desktop `/` remains horizontally contained (`scrollWidth == clientWidth == 1440`).
- Mobile `/` remains horizontally contained (`scrollWidth == clientWidth == 390`).
- Mobile heading `今天先处理什么？ 先处理资产决策队列` is no longer squeezed by refresh metadata (heading rect changed from 193x84 to 334x35).
- Mobile `当前需要处理` priority strip moved earlier (`y≈645`, height 129), and the command content starts earlier (`y≈782` instead of `y≈887`).
- The first visible lane is `资产决策队列`; three-lane structure remains intact below the primary task strip.

Validation after the mobile refinement:

- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web` passed: lint, 66 Vitest files / 500 tests, and production build.
- Browser recapture completed with the same mock profiles and viewports; screenshots remain local-only under `research/screenshots/`.

Remaining visual judgment: the mobile AppShell still consumes substantial first-screen height before Dashboard content. That is outside this Dashboard-only slice and should not be mixed into this checkpoint unless the shell/navigation mobile IA becomes the next explicit target.

## List workbench P0/P2 checkpoint

Following the external frontend/UI review, VPS, Nodes, and Targets were rechecked as table-first workbenches rather than metric-card first pages. The accepted direction for this slice is: page identity, compact current filter/task controls, primary table/workbench, Drawer handling, then low-weight context.

Implemented list-page corrections:

- VPS, Nodes, and Targets keep create actions available from the workbench toolbar instead of large header-level CTA buttons on mobile.
- Applied filters/search state are visible in the scan path as chips; advanced filter controls remain Drawer/secondary instead of large first-scan forms.
- Mobile compact-header metric cards on the three list pages are hidden/lowered so the table workbench appears materially earlier.
- VPS mobile inventory table no longer keeps the old 760px table surface; it uses a contained field-row/card-like layout under the mobile breakpoint.
- List workbench mobile toolbars occupy a normal full-width row instead of a skinny right-side strip.

Recapture results after the P0/P2 correction:

- `/vps` desktop remains contained (`scrollWidth == clientWidth == 1440`); mobile remains contained (`390 == 390`). Mobile table moved from `y≈1368`, width 760 to `y≈961`, width 366.
- `/nodes` desktop remains contained (`1440 == 1440`); mobile remains contained (`390 == 390`). Mobile table moved from `y≈1528` to `y≈1076`, width 366.
- `/targets` desktop remains contained (`1440 == 1440`); mobile remains contained (`390 == 390`). Mobile table moved from `y≈1459` to `y≈1081`, width 366.
- `.filter-bar` remains absent from the first-scan route captures for these pages.

Validation after this checkpoint:

- Focused VPS/Nodes/Targets tests passed during implementation/check.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web` passed: lint, 66 Vitest files / 500 tests, and production build.
- Browser recapture completed with the same mock profiles and viewports; screenshots remain local-only under `research/screenshots/`.

Remaining visual risk: AppShell/navigation still consumes substantial mobile first-screen height before page content. That is outside this P0/P2 list-page slice unless mobile shell/navigation IA becomes the next explicit target.

## Asset Decisions workbench checkpoint

The external review treats `asset-decisions` as the asset handling entry, with VPS/provider/subscription pages acting as evidence pages. This checkpoint keeps the decision queue as the primary surface and pushes supporting evidence to lower weight.

Implemented Asset Decisions corrections:

- Header is identity-first; header metrics/actions no longer compete with the decision queue.
- Renewal window selector moved into the queue toolbar as a quiet auxiliary control and remains functional.
- Summary, evidence boundary, secondary links, and renewal evidence remain in low-weight aside/details.
- Renewal evidence table remains folded and unmounted until explicitly opened.
- Queue update drawer, linkage notice/action behavior, renewal-window refetch, and evidence failure behavior remain covered by tests.
- Mobile queue filter tabs no longer rely on horizontal clipping; they wrap into a compact chip grid.

Recapture results after the Asset Decisions correction:

- `/asset-decisions` desktop remains contained (`scrollWidth == clientWidth == 1440`); main queue content starts at `y≈428`, with aside at 340px width.
- `/asset-decisions` mobile remains contained (`scrollWidth == clientWidth == 390`); workbench content starts at `y≈851`, and queue tabs occupy a wrapped 366px grid instead of clipped overflow.
- Renewal evidence still does not mount as a `.data-table` in the default capture.

Validation after this checkpoint:

- Focused Asset Decisions tests passed during implementation/check.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web` passed: lint, 66 Vitest files / 500 tests, and production build.
- Browser recapture completed with the same mock profiles and viewports; screenshots remain local-only under `research/screenshots/`.

Remaining visual risk: queue rows are still dense by design and should be judged with real inventory later, but the page now follows the external plan’s hierarchy: identity → queue controls → decision queue → low-weight evidence.

## Full top-level route screenshot audit before final correction

Date: 2026-05-25
Preview URL: http://127.0.0.1:5179/
Data sources: task-local mock API on `127.0.0.1:58080` through Vite proxy; authenticated via `/api/auth/me` mock user `Visual Evidence`.
Viewports: 1440x1000 and 390x900.
Routes: `/`, `/vps`, `/nodes`, `/targets`, `/events`, `/providers`, `/subscriptions`, `/asset-decisions`.
Raw metrics: `research/visual-audit-raw-node.json`.
Screenshots: local-only under `research/screenshots/`, not committed.

Capture note: the first recapture attempt on port 5178 hit a real/protected center session and stopped at `/login?next=...`; it was discarded for visual judgment. A separate mock-proxied Vite preview on 5179 produced the route captures used below.

### Cross-route findings

- Desktop document overflow is controlled on all eight routes (`scrollWidth == clientWidth == 1440`), so the remaining problem is visual rhythm rather than hard horizontal page overflow.
- The AppShell consumes the expected left rail, leaving a 1084px work area (`x≈310`, width≈1084). This matches the prototype’s centered main column direction closely enough for this slice.
- List workbench controls are now stable horizontally, but VPS / Nodes / Targets still push the primary table too low: table starts at `y≈608`, `y≈703`, and `y≈716` respectively. The prototype direction expects controls to be compact enough that the table/queue becomes the dominant first-scan object earlier.
- The repeated large header metric cards on VPS / Nodes / Targets / Events occupy `78px` high blocks and compete with the workbench controls below; the first viewport reads like header KPI + control grid + table instead of identity → compact controls → table.
- `table-workbench__tabs` is too tall on VPS / Nodes / Targets (`123px`, `105px`, `118px`) because search and quick-view tabs are stacked in one left rail. This preserves behavior but creates unnecessary vertical latency before the primary table.
- `activeChips` is always visible as a separate 28px row with only `字段筛选：无` on VPS / Nodes / Targets, adding a non-decision row before the table when no filter is active.
- Events and Subscriptions still mount `.filter-bar` in the first scan path. Events uses it acceptably as compact current-query controls, but Subscriptions’ three selects plus evidence strip make the page feel more form-first than table-first.
- Providers is the closest to the target among evidence pages: table starts at `y≈420`, but the explanatory header/description still reads long for a master-data evidence table.
- Asset Decisions keeps the queue in the first viewport, but the queue row treatment remains heavy and card-like; this is acceptable compared with the list pages and should not be widened in this pass.

### Route-specific gaps and correction plan

- `/` Dashboard: geometry is stable (`dashboard-command-grid` starts at `y≈437`) and the command lanes match the current plan. No structural change planned beyond keeping the capture as baseline.
- `/vps`: `CompactHeader` metrics and stacked search/tabs push the table to `y≈608`; the no-filter chip row adds another 28px. Hide list-page header metric cards on desktop for this page, make quick controls a horizontal command rail, and omit the inactive chip row until filters are applied or evidence notice exists.
- `/nodes`: table starts at `y≈703` because header metrics, stacked quick controls, a no-op chip row, and a 124px priority evidence card all compete. Hide desktop header metrics, put search and quick views into one horizontal scan rail, suppress the inactive chip row, and keep the evidence focus compact enough that it supports rather than dominates the list.
- `/targets`: same pattern as Nodes, with table start at `y≈716`. Hide desktop header metrics, align search + scan buttons in one horizontal rail, suppress inactive chips, and keep priority evidence compact.
- `/events`: timeline starts at `y≈453`, acceptable; the compact filter bar remains as current-query control. No broad structural change planned, only ensure shared CSS changes do not regress Events.
- `/providers`: table starts at `y≈420`, acceptable. Tighten wording to evidence/table-first copy; no new layout component.
- `/subscriptions`: table starts at `y≈463`, but the evidence strip plus inline filter form makes the first scan path busy. Compress the evidence strip visually and tighten copy; do not change URL/filter behavior.
- `/asset-decisions`: queue starts at `y≈454`, acceptable. No broad structural change planned in this pass; avoid touching drawer/update behavior.

Planned implementation is limited to `web/src`: CSS density fixes in `pages.css` plus small JSX copy/chip rendering changes on VPS / Nodes / Targets / Providers / Subscriptions if needed. API calls, routes, drawers, URL-state, row navigation guards, batch behavior, and decision update flows remain unchanged.

### Asset Decisions screenshot follow-up

User-provided desktop/mobile captures showed the renewal decision page still had first-viewport readability problems after the previous pass:

- Desktop row actions were visually squeezed into the evidence column: `详情` text sat too close to the primary `处理` button, making the action group read as an accidental overlay.
- Queue rows were too heavy for the primary scan path: duplicate rank text, nested subscription/Node/quality boxes, and strong row borders made every row compete as a card.
- The queue tab rail was readable but still over-framed relative to the queue; the active tab and the container border drew as much attention as the first row.
- The right aside repeated queue/evidence facts in strong boxes and pushed `证据边界` / renewal evidence / evidence links into several similarly weighted surfaces.
- Mobile is lower priority for this follow-up, but the pasted mobile capture confirmed the same tab/row density carries into the narrow layout after the shell.

Focused correction made in this follow-up:

- Removed the duplicated rank label inside each queue row.
- Converted the row action cell into a fixed-width vertical action group with a quiet full-width detail link above the primary `处理` button, avoiding overlap with quality badges.
- Softened nested subscription/Node/quality evidence borders and the queue tab container so the primary table/queue reads before evidence decoration.
- Reordered and lowered the aside: current queue summary remains first, evidence boundary is a quiet disclosure, evidence page links are plain low-weight links, and renewal evidence remains folded.

Browser evidence: the existing local screenshots under `research/screenshots/`, the user-provided capture, and a fresh local Chrome headless screenshot were used for visual diagnosis/confirmation. Python Playwright remained unavailable in this environment (`ModuleNotFoundError: No module named 'playwright'`), so the follow-up recapture used a temporary repo-local mock API on `127.0.0.1:58080`, Vite on `127.0.0.1:5179`, and Chrome headless. The fresh desktop capture (`asset-decisions-focused-1440x1000.png`) confirmed the queue remains in the first viewport, row actions no longer overlap the quality/evidence cell, and the aside is lower weight. Screenshot evidence remains local-only and raster files are not committed.

Validation after this follow-up:

- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm --prefix web run test -- --run src/pages/AssetDecisionsPage.test.tsx` passed: 1 file / 6 tests.
- `npm --prefix web run lint` passed.
- `npm --prefix web run build` passed.

## Events traceability workbench checkpoint

The external review assigns `/events` the traceability role, so this checkpoint keeps Dashboard and object list pages free from recent-event summaries and makes the event timeline the primary surface.

Implemented Events corrections:

- Events now uses the shared workbench skeleton: identity header, compact current-query controls, primary timeline/list, advanced filter Drawer, and low-weight support context.
- The event stream is mounted before the support context; desktop keeps a main/aside split, while mobile stacks support after the timeline.
- The previous mobile regression where the aside squeezed `.workbench-main` to ~50px is fixed with an Events-scoped responsive override.
- Mobile Events hides header metrics and inline filter selects, leaving a compact summary plus `高级筛选`, so the first event row appears in the first viewport.
- Maintenance event types render with maintenance treatment instead of severe/offline abnormal styling.
- URL filter state, active chips/remove/reset, Drawer draft/apply/cancel, and load-more behavior remain covered by tests.

Recapture results after the Events correction:

- `/events` desktop remains contained (`scrollWidth == clientWidth == 1440`); timeline content starts at `y≈457`, first event row at `y≈558`, with aside at 300px width.
- `/events` mobile remains contained (`scrollWidth == clientWidth == 390`); `.workbench-main` is 366px wide, support aside starts after the timeline (`y≈2839`), and the first event row starts at `y≈818` instead of being pushed below the first viewport.
- Mobile header metrics are no longer visible in the route capture (`.compact-header__metrics` rect 0x0), and the compact filter area is reduced to a 366x55 summary/control band.

Validation after this checkpoint:

- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm --prefix web run test -- --run src/pages/EventsPage.test.tsx src/components/EventList.test.tsx` passed: 2 files / 24 tests.
- `npm --prefix web run lint` passed.
- `npm --prefix web run build` passed.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web` passed: lint, 66 Vitest files / 501 tests, and production build.
- Browser recapture completed with the same mock profiles and viewports; screenshots remain local-only under `research/screenshots/`.

Remaining visual risk: the mobile AppShell/global alert/search area still consumes substantial height before route content. The Events page itself now brings the timeline into the first viewport, but deeper mobile compression would require an explicit shell/navigation IA slice rather than another Events-only change.

## Open-design prototype alignment checkpoint

The follow-up prototype at `.tmp/open-design.html` is now treated as the concrete desktop reference for main and list pages. The file is reference-only and remains untracked; detail pages and deeper mobile compression remain deferred.

Implemented prototype-alignment corrections:

- Desktop AppShell rhythm moved closer to the prototype: stable left navigation, centered max-width route content, calmer topbar/search placement, and required global anomaly alert visibility preserved.
- Providers and Subscriptions were added to the audited list-page scope as evidence pages rather than handling entries.
- Providers now follows identity → provider evidence table → low-weight master-data context instead of stacked explanatory panels.
- Subscriptions now follows identity → compact renewal evidence/filter rail → primary subscription table → low-weight URL/filter truth context. The first implementation left four summary cards stacked before the table; this was corrected after browser recapture.
- The visual audit capture script now includes `/providers` and `/subscriptions` under the `asset-workflows` mock profile.

Recapture results after the prototype-alignment correction:

- Dashboard desktop remains contained (`scrollWidth == clientWidth == 1440`) and keeps the prototype direction: `今天先处理什么？`, compact metrics, current-task strip, three lanes.
- Providers desktop remains contained (`1440 == 1440`); provider table content starts at `y≈421`, with the context aside at 340px width.
- Initial Subscriptions desktop recapture showed the table too low (`.data-table y≈686`) because summary cards stacked vertically before the filters/table.
- After the Subscriptions compression pass, desktop `/subscriptions` remains contained (`1440 == 1440`) and the table moved materially earlier (`.data-table y≈462`, `.filter-bar y≈400`, workbench height `≈479`).
- Existing list pages remain horizontally contained in desktop recapture: `/vps`, `/asset-decisions`, `/nodes`, `/targets`, and `/events` all report `scrollWidth == clientWidth == 1440`.

Validation after this checkpoint:

- Prototype alignment implement/check passed focused Providers/Subscriptions/AppShell tests, lint, full Vitest, build, and `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make verify-web`.
- Subscriptions compression check passed `git diff --check`, focused Subscriptions tests, lint, full web tests, build, and `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make -C /Users/weibo/Code/houfeng verify-web`.
- Browser recapture completed after the Subscriptions compression; screenshots remain local-only under `research/screenshots/`.

Remaining visual risk: visual acceptance still needs human judgment against the prototype, especially whether Dashboard’s current compact lane treatment is close enough to the open-design HTML. Mobile remains explicitly lowest priority for this stage, and detail pages remain deferred because the prototype does not specify them.

## Asset Decisions prototype-layout rejection correction

The user rejected the previous Asset Decisions implementation because `class="workbench-layout workbench-layout--with-aside"` created a generic desktop left/right split that conflicts with the external frontend 改造计划 and `.tmp/open-design.html` prototype. The decisive correction is that `/asset-decisions` must behave like the prototype asset list page: identity header, one central decision/list panel, compact controls near the top, and folded/low-weight evidence in the same vertical flow.

Implemented correction:

- Removed `WorkbenchPage` usage from `AssetDecisionsPage`; the page no longer mounts `.workbench-layout--with-aside` or `.workbench-aside` for Asset Decisions.
- Kept `CompactHeader` and one primary `ListWorkbench` decision queue panel as the dominant first-viewport surface.
- Moved queue summary, evidence boundary, evidence links, and renewal candidate evidence into `.asset-decision-flow-context` below the queue panel instead of a right rail.
- Renewal candidate evidence remains folded and unmounted by default; expanding the disclosure is still required before the renewal table appears.
- Existing API calls, renewal-window refetch, queue filters, update drawer, linkage notice/action, and evidence failure behavior were preserved and covered by the focused test.

Visual recapture:

- Preview URL: `http://127.0.0.1:5179/asset-decisions`.
- Data source: local-only mock Asset Ledger API on `127.0.0.1:58080`, using `scripts/visual_evidence.py` asset workflow fixtures.
- Viewports: `1440x1000` and `390x900`.
- Browser: local Chrome headless with `--virtual-time-budget=5000`; Python Playwright remains unavailable in this environment (`ModuleNotFoundError: No module named 'playwright'`).
- Screenshots: local-only `research/screenshots/asset-decisions-single-column-1440x1000.png` and `research/screenshots/asset-decisions-single-column-390x900.png`; raster files are not intended for commit.

DOM marker checks after React render:

- `.asset-decisions-page .workbench-layout--with-aside`: absent.
- `.asset-decisions-page .workbench-aside`: absent.
- `.asset-decision-flow-context`: present.
- `aria-label="资产决策队列列表"`: present.
- `.asset-decision-renewals-table` in default render: absent.

Visual result: desktop now shows one central content lane and one strong decision queue panel. The renewal window selector, queue tabs, and dense rows sit inside the primary panel, while summary/evidence disclosures are below the queue rather than beside it. Mobile keeps the same single-column flow; remaining mobile height is dominated by the AppShell/global alert area, not by an Asset Decisions right-rail split.

## Remaining top-level prototype split correction

The Asset Decisions rejection exposed the same generic split risk on several other top-level routes. The follow-up correction treats the open-design prototype as table/list/timeline-first: right-side support is only valid where the prototype explicitly asks for a rail, otherwise context must follow the primary surface in the same flow.

Implemented correction:

- `WorkbenchPage` now supports `variant="centered"`, keeping the previous split aside as the default while allowing an in-flow `.workbench-context` for top-level list workbenches.
- `/events` no longer passes `aside` to `WorkbenchPage`; the diagnostic support surface now sits inside the main region after the event timeline as `.events-flow-context`. This keeps Events as a traceability timeline rather than a secondary dashboard with a narrow side rail.
- `/providers` no longer uses a WorkbenchPage complementary aside; the provider master-data note now follows the provider evidence table in `.providers-flow-context`.
- `/subscriptions` no longer uses a WorkbenchPage complementary aside; URL/filter truth, VPS context, and prerequisite guidance now follow the subscription evidence table in `.subscriptions-evidence-workbench__context`.
- `/vps`, `/nodes`, and `/targets` use `WorkbenchPage variant="centered"`, so their existing support surfaces remain available but no longer narrow the primary table lane with `.workbench-layout--with-aside`.
- Nodes support links and auto-refresh controls were regrouped into a compact in-flow footer instead of behaving like a side-rail control stack.

Structural checks added/updated:

- `WorkbenchPage` tests now cover both the default split aside and the centered in-flow variant.
- Events, Providers, Subscriptions, VPS, Nodes, and Targets page tests now assert the corrected routes do not mount route-level `.workbench-layout--with-aside` and that support context follows the main table/timeline surface.

Verification after this final follow-up:

- `cd web && TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npx vitest run src/components/WorkbenchPage.test.tsx src/pages/EventsPage.test.tsx src/pages/ProvidersPage.test.tsx src/pages/SubscriptionsPage.test.tsx src/pages/VPSPage.test.tsx src/pages/NodesPage.test.tsx src/pages/TargetsPage.test.tsx` passed: 7 files / 106 tests.
- `cd web && npm run lint` passed.
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm run test -- --run` passed: 66 files / 503 tests.
- `cd web && npm run build` passed.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make -C /Users/weibo/Code/houfeng verify-web` passed: lint, 66 Vitest files / 503 tests, and production build.
- `verify-web` emitted the existing local environment warnings: package requires Node 22.x while this machine is running Node v24.14.1, plus the existing npm moderate-severity audit notice.

Browser recapture has not yet been repeated after this last split correction; screenshots should remain local-only if captured.

## Final desktop density recapture after list/evidence compression

The final top-level recapture has now been repeated after the latest list/evidence compression and centered-flow corrections.

Evidence setup:

- Preview URL: `http://127.0.0.1:5179/`.
- Data source: task-local mock API on `127.0.0.1:58080` through the Vite proxy; authenticated by the mock `/api/auth/me` user.
- Viewports: `1440x1000` and `390x900`.
- Routes: `/`, `/vps`, `/nodes`, `/targets`, `/events`, `/providers`, `/subscriptions`, `/asset-decisions`.
- Raw metrics: `research/visual-audit-raw-node.json`.
- Screenshots: local-only under `research/screenshots/`, not intended for commit.

Desktop results:

- All eight routes remained horizontally contained (`scrollWidth == clientWidth == 1440`).
- `/vps` table moved from the pre-fix `y≈608` to `y≈465`; the header metric block is hidden (`0x0`), the tabs/control band is `h≈67`, and the inactive `字段筛选：无` chip row is no longer mounted.
- `/nodes` table moved from `y≈703` to `y≈556`; the header metric block is hidden (`0x0`), search and quick views sit in a `h≈57` rail, and the inactive chip row is not mounted.
- `/targets` table moved from `y≈716` to `y≈556`; it follows the same compressed scan path as Nodes, with the priority evidence strip retained at support weight (`h≈116`).
- `/subscriptions` table moved from `y≈463` to `y≈451`; the evidence/filter area is tighter (`.table-workbench__tabs h≈31`) while URL context chips remain visible when active.
- `/providers` remains table-first (`.data-table y≈420`) with shorter evidence-page copy.
- Dashboard, Events, and Asset Decisions were intentionally kept stable in this pass: Dashboard command grid starts at `y≈437`, Events timeline remains around `y≈453`, and Asset Decisions keeps one primary queue panel rather than a right-rail split.

Visual judgment: the main desktop gaps identified in the pre-edit audit are materially reduced without changing API requests, URL-state contracts, Drawer draft/apply behavior, row navigation guards, batch behavior, runtime actions, or create/update flows. Remaining mobile height is still dominated by the AppShell/global shell rather than these page-level workbench surfaces.

Final verification after this recapture:

- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm --prefix /Users/weibo/Code/houfeng/web run test -- --run src/pages/VPSPage.test.tsx src/pages/NodesPage.test.tsx src/pages/TargetsPage.test.tsx src/pages/ProvidersPage.test.tsx src/pages/SubscriptionsPage.test.tsx` passed: 5 files / 86 tests.
- `npm --prefix /Users/weibo/Code/houfeng/web run lint` passed.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm --prefix /Users/weibo/Code/houfeng/web run test -- --run` passed: 66 files / 503 tests.
- `npm --prefix /Users/weibo/Code/houfeng/web run build` passed.
- `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make -C /Users/weibo/Code/houfeng verify-web` passed: lint, 66 Vitest files / 503 tests, and production build.
- `verify-web` emitted the existing local environment warnings: package requires Node 22.x while this machine is running Node v24.14.1, plus one existing npm moderate-severity audit notice.

## Providers / Subscriptions / Nodes / Targets usability follow-up

The user accepted the corrected VPS page direction as a usability reference, but explicitly rejected copying the VPS layout directly. The follow-up applied the same principles per page function: table-first scan path, compact command/header treatment, visible list-determining filters, and evidence/context integrated into the primary workbench rather than mounted as competing standalone blocks.

Implemented follow-up corrections:

- `/providers`: provider master-data context is now inline inside the provider evidence list surface. The route no longer mounts a separate provider flow context block or complementary side context for this evidence page; create/edit remain Drawer-only.
- `/subscriptions`: URL/filter truth, VPS context, and no-VPS prerequisite guidance remain visible but now live inside the subscription evidence workbench after the table/error/empty state. The page preserves `vps_id`, `create=1`, renew-window request semantics, and Drawer create/edit behavior.
- `/nodes`: routine list filters are no longer hidden behind `节点高级筛选`. The existing Nodes filter controls render inline above the table as a compact filter rail, while create/onboarding/runtime/batch flows remain unchanged.
- `/targets`: routine Target field filters are no longer hidden behind `字段筛选`. The existing Targets filter controls render inline above the table, while create drawer, batch controls, runtime controls, row navigation guard, and metadata behavior remain unchanged.
- Shared page CSS now gives Nodes/Targets inline filters a compact grid rhythm and gives provider/subscription evidence context low-weight inline treatment. No new CSS files, dependencies, API routes, or backend protocol changes were introduced.

Behavior and test adjustments:

- Providers tests now assert the master-data evidence context is inside `服务商主证据列表` and that no old `providers-flow-context` / complementary support rail is mounted.
- Subscriptions tests assert routine subscription filtering is not hidden in a filter dialog and preserve evidence failure / URL-create behavior.
- Nodes tests scope create-drawer field queries with `within(createDialog)` because inline filters and create form fields intentionally share labels such as `地区`, `城市`, and `供应商`.
- Targets tests now use the inline `FilterBar` clear action label `清空所有` instead of the removed drawer-era `清空筛选` expectation.

Validation after this follow-up:

- Focused touched-page suite passed: `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm --prefix /Users/weibo/Code/houfeng/web run test -- --run src/pages/NodesPage.test.tsx src/pages/TargetsPage.test.tsx src/pages/ProvidersPage.test.tsx src/pages/SubscriptionsPage.test.tsx` passed: 4 files / 81 tests.
- `npm --prefix /Users/weibo/Code/houfeng/web run lint` passed.
- `npm --prefix /Users/weibo/Code/houfeng/web run build` passed.
- Full Vitest passed: `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp npm --prefix /Users/weibo/Code/houfeng/web run test -- --run` passed: 66 files / 503 tests.
- Full web verification passed: `TMPDIR=/Users/weibo/Code/houfeng/web/.tmp make -C /Users/weibo/Code/houfeng verify-web` passed lint, 66 Vitest files / 503 tests, and production build.
- `verify-web` again emitted the existing local environment warnings: package requires Node 22.x while this machine is running Node v24.14.1, plus one existing npm moderate-severity audit notice.

Remaining risks / limitations from that follow-up were superseded by the final recapture below.

## Final visual recapture for top-level workbenches

Date: 2026-05-25
Preview URL: `http://127.0.0.1:5179/`
Data source: task-local mock API on `127.0.0.1:58080` through Vite proxy; authenticated by the mock `/api/auth/me` user `Visual Evidence`.
Browser: local Chrome headless via `chrome-capture.mjs` and Chrome DevTools Protocol.
Viewports: desktop `1440x1000` for all target routes; mobile `390x900` was captured as lower-priority risk evidence, not as the acceptance baseline for this pass.
Routes: `/`, `/vps`, `/asset-decisions`, `/providers`, `/subscriptions`, `/nodes`, `/targets`, `/events`.
Raw metrics: `research/visual-audit-raw-node.json`.
Screenshots: local-only ignored PNG files under `research/screenshots/`; raster files are not intended for commit.

Capture command:

```bash
HOUFENG_VISUAL_MOCK_PORT=58080 node .trellis/tasks/05-22-frontend-workbench-visual-correction/research/mock-api-server.mjs
VITE_API_TARGET=http://127.0.0.1:58080 npm --prefix web run dev -- --host 127.0.0.1 --port 5179 --strictPort
HOUFENG_VISUAL_BASE_URL=http://127.0.0.1:5179 HOUFENG_CHROME_DEBUG_PORT=9224 node .trellis/tasks/05-22-frontend-workbench-visual-correction/research/chrome-capture.mjs
```

The actual run used one shell wrapper to start/stop the mock API and Vite server around the capture. Chrome emitted transient headless/GPU mailbox warnings after writing the JSON; the capture completed successfully.

### Desktop 1440x1000 route results

| Route | Horizontal overflow | Primary surface start | Split aside / rejected rail | Filter/context notes |
| --- | --- | --- | --- | --- |
| `/` | `scrollWidth == clientWidth == 1440` | Dashboard command grid `y≈437` | `.workbench-aside` absent | Keeps one centered command surface; no table/list overflow observed. |
| `/vps` | `1440 == 1440` | DataTable `y≈464`, width `1084` | `.workbench-aside` absent | Quick views and search are visible above table; routine VPS field filters remain inline in the priority strip; inactive chips are not mounted. |
| `/asset-decisions` | `1440 == 1440` | Queue content `y≈510`, width `1050` | `.workbench-layout--with-aside` absent, `.workbench-aside` absent | Rejected split aside remains absent; decision queue is the primary panel. Queue summary/evidence is kept in the single-column flow rather than a right rail; renewal evidence table is not mounted by default. |
| `/providers` | `1440 == 1440` | DataTable `y≈420`, width `1084` | `.workbench-aside` absent | Provider master-data context is integrated into the primary evidence table surface; old `.providers-flow-context` is absent. |
| `/subscriptions` | `1440 == 1440` | DataTable `y≈451`, width `1084` | `.workbench-aside` absent | Inline subscription filters are visible above the table (`.filter-bar y≈388`); URL/filter truth context follows the table in `.subscriptions-evidence-workbench__context` at `y≈729`. |
| `/nodes` | `1440 == 1440` | DataTable `y≈703`, width `1084` | `.workbench-aside` absent; in-flow `.workbench-context` present after the table | Routine Node filters are now visible inline (`.filter-bar y≈441`, height `≈121`) with chips/controls above the table; no `节点高级筛选` routine Drawer is present in this capture. |
| `/targets` | `1440 == 1440` | DataTable `y≈707`, width `1084` | `.workbench-aside` absent; in-flow `.workbench-context` present after the table | Routine Target filters are visible inline (`.filter-bar y≈441`, height `≈125`) with chips/controls above the table; no routine field-filter Drawer is present in this capture. |
| `/events` | `1440 == 1440` | Timeline content `y≈453`, support context `y≈1969` | `.workbench-aside` absent | Events keeps its separate current-query filter model: compact filter bar remains in the first scan path and support context follows the timeline. This does not change the Events Drawer applied/draft contract. |

### Required recapture checks

- Horizontal document overflow: none observed on the eight desktop routes (`scrollWidth == clientWidth == 1440`). Mobile route documents also stayed contained (`390 == 390`); some mobile tables use internal horizontal scroll within the table region rather than document-level overflow.
- Table/list/timeline starts: Providers and Subscriptions remain table-first (`y≈420` / `451`); VPS remains acceptable (`y≈464`); Events timeline starts at `y≈453`; Asset Decisions queue content starts at `y≈510`. Nodes and Targets now start lower (`y≈703` / `707`) because the accepted inline field filters are visible above the table.
- Rejected split aside: absent from Asset Decisions, Events, Providers, Subscriptions, VPS, Nodes, and Targets route-level captures. Nodes/Targets keep only in-flow support context after the primary table.
- Inline filter visibility: VPS, Nodes, Targets, and Subscriptions all expose routine list-determining filters inline above the table and sync them to visible state/URL behavior. The stale spec requirement for Nodes routine Drawer filters has been corrected in `.trellis/spec/web/state-and-data.md`.
- Asset Decisions context integration: the page remains a single primary queue panel; evidence/summary stays in the same flow and renewal candidate evidence remains folded/unmounted by default.
- Screenshots: generated PNGs are local-only and ignored by the repository raster screenshot rules; only markdown/JSON evidence is intended to remain.

### Remaining visual risks

- Nodes and Targets desktop table starts are materially lower than the previous compressed capture because routine filters are now deliberately inline. This is the accepted IA direction, but it should be judged visually with real data to ensure the filter rail does not become a new first-viewport wall.
- Mobile remains lower priority for this slice. The AppShell/global alert still consumes much of the first screen, and inline Nodes/Targets filters push the mobile table start to `y≈1685` / `1516`; solving that would require a mobile-specific shell/filter-density task rather than another desktop workbench correction.
- Providers and Subscriptions mobile table regions remain document-contained but use internal table overflow/card density; future mobile evidence-page work may need a separate compact row treatment.


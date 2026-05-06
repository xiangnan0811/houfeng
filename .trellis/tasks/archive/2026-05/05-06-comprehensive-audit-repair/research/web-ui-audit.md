# Research: web-ui-audit

- Query: Research frontend and UI implementation risks for the active Trellis task.
- Scope: internal
- Date: 2026-05-06

## Findings

### Task / Scope Context

- Active task resolution via `python3 ./.trellis/scripts/task.py current --source` returned no active task pointer (`Current task: (none)`, `Source: none`). User confirmed task directory: `.trellis/tasks/05-06-comprehensive-audit-repair`.
- Task PRD: `.trellis/tasks/05-06-comprehensive-audit-repair/prd.md` describes a repo-wide early audit whose web scope includes v2 visual/spec drift, usability/accessibility defects, stale docs-to-code claims, and verification evidence.
- Web visual authority: `.trellis/spec/web/index.md` points visual authority at `docs/design/v2-houfeng/`; `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md` are the main v2 references for this audit.
- Inspected web implementation surfaces:
  - `web/src/pages/*Page.tsx` and page tests
  - `web/src/components/**`
  - `web/src/app/layout/**`
  - `web/src/lib/api.ts`, `web/src/lib/types.ts`, formatter/theme/auth helpers where relevant
  - `web/src/styles/*.css`, `web/src/app/layout/layout.css`, `web/src/components/filters/filters.css`

### Files Found

- `.trellis/tasks/05-06-comprehensive-audit-repair/prd.md` - current task requirements and acceptance criteria.
- `.trellis/spec/web/index.md` - spec index and authority ordering.
- `.trellis/spec/web/component-conventions.md` - component layering, export, API, and known frontend gaps.
- `.trellis/spec/web/styling-guidelines.md` - token/BEM styling rules and inline-style restrictions.
- `.trellis/spec/web/quality-guidelines.md` - web verification and test expectations; contains stale `verify-web` claims.
- `docs/design/v2-houfeng/design-language.md` - v2 visual language, status, loading/error/empty, density, and typography contracts.
- `docs/design/v2-houfeng/component-spec.md` - v2 component/page visual contracts.
- `web/src/pages/EventsPage.tsx` - events filters, grouping, load-more UI.
- `web/src/pages/NodesPage.tsx` - node DataTable, filters, batch command, row actions.
- `web/src/pages/TargetsPage.tsx` - target DataTable, filters, row actions.
- `web/src/pages/NodeDetailPage.tsx` - node watchtower/detail UI and container table.
- `web/src/components/atoms/DataTable.tsx` - semantic table / clickable row primitive.
- `web/src/components/atoms/Drawer.tsx` - drawer implementation used by history views.
- `web/src/components/StatusBadge.tsx` - v2 Badge wrapper and status tone inference.
- `web/src/styles/pages.css` - page/layout BEM styles including empty states and row action reveal.
- `web/src/styles/atoms.css` - atom styles including Drawer and DataTable.
- `web/src/app/layout/ChangePasswordModal.tsx` - modal implementation.
- `Makefile`, `.github/workflows/ci.yml`, `web/package.json` - verification command truth.

### Concrete Risks / Bugs

#### P1 - EventsPage exposes an inert `include_backfilled` checkbox

Evidence:

- `web/src/pages/EventsPage.tsx:27-31` comments that `include_backfilled` is forwarded as a query param for future backend support.
- `web/src/pages/EventsPage.tsx:421-433` renders the visible checkbox labelled `包含补传事件`.
- `web/src/pages/EventsPage.tsx:81-93` builds the API filter but omits `include_backfilled`.
- `web/src/lib/types.ts:339-351` defines `EventListFilter` without `include_backfilled`.
- `web/src/lib/api.ts:332-349` serializes `listEvents()` query params without `include_backfilled`.

Impact:

- The UI gives users a filter control that cannot affect the request. This is a concrete stale docs-to-code claim and a product trust issue: the checkbox changes local state but does not change the API query.
- The issue is especially easy to miss because `EventsPage.tsx` contains an implementation comment claiming the opposite.

Suggested repair:

- Either remove/disable the checkbox with explicit copy until backend support exists, or add the field through `EventListFilter` and `listEvents()` once backend semantics are available.
- Add an `EventsPage.test.tsx` assertion that toggling `包含补传事件` changes the final `fetch` URL, or that the disabled/not-supported state is visible.

#### P1 - `NodeDetailPage` uses an undefined CSS token for container image text

Evidence:

- `web/src/pages/NodeDetailPage.tsx:1113` uses `style={{ fontSize: 'var(--fs-sm)', wordBreak: 'break-all' }}`.
- `rg -- "--fs-sm|fs-sm" web/src .trellis/spec docs` found no token definition or other usage.
- The actual type tokens in `web/src/styles/tokens.css` are `--type-small-size`, `--type-body-size`, etc.; v2 styling rules require type scale usage through these tokens.

Impact:

- `font-size: var(--fs-sm)` is invalid at computed-value time when the variable is undefined, so the browser falls back rather than applying the intended small text size.
- This is also inline styling in a page component, which conflicts with `.trellis/spec/web/styling-guidelines.md:112-122` unless it is runtime sizing/calculation.

Suggested repair:

- Move the container image text styling into a BEM class in `pages.css`, or at minimum replace the token with `var(--type-small-size)` while moving `word-break` into CSS.
- Add a focused test or DOM class assertion around the container table image cell if this area is touched.

#### P1 - Web spec verification docs are stale and contradict the actual Makefile / CI gate

Evidence:

- `Makefile:65-67` currently runs `cd web && $(NPM) ci && $(NPM) run lint && $(NPM) run test -- --run && $(NPM) run build`.
- `.github/workflows/ci.yml:19-29` runs `make verify-web` in CI with Node 22 and the web package-lock cache.
- `.trellis/spec/web/quality-guidelines.md:35-48` still shows `verify-web` without lint and states that `make verify-web` does not run `npm run lint`.
- `.trellis/spec/web/quality-guidelines.md:207` repeats that lint must be run separately because `make verify-web` does not run it.
- `.trellis/spec/web/quality-guidelines.md:250` lists this as a known gap, even though `docs/release/v1-gap-checklist.md:164` marks the gap closed.
- `.trellis/spec/backend/quality-guidelines.md:32` also lists `make verify-web` without `npm run lint`.

Impact:

- This can mislead implement/check agents and human contributors about what the CI quality gate actually covers.
- It is directly relevant to this task because the PRD says verification command truth is part of the comprehensive audit.

Suggested repair:

- Update `.trellis/spec/web/quality-guidelines.md` and `.trellis/spec/backend/quality-guidelines.md` through the Trellis spec-update flow, not as part of this research-only pass.
- Keep the happy-path command list as `make verify-web` or `cd web && npm run lint && npm run test -- --run && npm run build` depending on speed needs.

#### P2 - EventsPage still has visible v2 layout drift: advanced filters are inline card-grid, not a drawer/chip flow

Evidence:

- v2 component spec says EventsPage should use a chip row plus `高级筛选` drawer for time range, labels, and checkbox filters: `docs/design/v2-houfeng/component-spec.md:240-243`.
- Current `EventsPage.tsx:255-447` renders all filters inline inside one `DetailSection`, using a `summary-grid` of form fields.
- Current implementation does have useful improvements that older release notes said were missing: time range tabs exist at `EventsPage.tsx:68-79` and grouped events / load-more exist at `EventsPage.tsx:450-497`. The remaining drift is specifically the layout/interaction model, not total absence of filtering.

Impact:

- The events page is functionally richer than earlier gap docs imply, but still visually diverges from v2: all advanced fields are always visible, reducing scan density and making the main event stream less prominent.
- This is not necessarily a blocker, but it is a concrete v2 spec drift item to decide or repair.

Suggested repair:

- Introduce the existing `Drawer` atom for advanced filters, keep the primary chip row visible, and move time range / label / boolean controls into the drawer.
- Add tests around opening/closing the drawer and preserving existing URL/query behavior.

#### P2 - EventsPage contains several page-level inline layout styles

Evidence:

- `web/src/pages/EventsPage.tsx:262-263` uses inline margins.
- `web/src/pages/EventsPage.tsx:454-483` uses inline flex/gap/margin styles for event groups and load-more layout.
- Styling spec restricts inline `style={{ ... }}` to runtime dimensions/calculation and says spacing/layout should go through tokens + BEM classes: `.trellis/spec/web/styling-guidelines.md:112-122`.

Impact:

- This is not a runtime bug, but it is recurring drift from the web styling contract and makes v2 layout harder to maintain consistently.
- It also weakens the “no new page-local CSS/imports, use BEM in shared CSS” discipline.

Suggested repair:

- Add BEM classes such as `events-filter__range`, `event-groups`, `event-group__header`, and `events-load-more` in `pages.css`; replace inline layout styles.
- Cover only behavior in tests; jsdom should not assert visual pixels.

#### P2 - SettingsPage still has known inline spacing styles, and there are more than the spec currently names

Evidence:

- Styling spec lists `SettingsPage.tsx:853 / :862` as known inline spacing debt: `.trellis/spec/web/styling-guidelines.md:155-156`.
- Current code has inline layout spacing at `web/src/pages/SettingsPage.tsx:418`, `web/src/pages/SettingsPage.tsx:458`, `web/src/pages/SettingsPage.tsx:1125`, and `web/src/pages/SettingsPage.tsx:1134`.

Impact:

- This is small but concrete styling drift. The spec points at stale line numbers and undercounts current inline style debt.

Suggested repair:

- Replace with BEM classes in `pages.css`.
- Update the styling spec after repair so the known-gap section no longer points at stale line numbers.

#### P2 - Empty state visual treatment does not fully implement the v2 decorative/CTA contract

Evidence:

- v2 says `.empty-state` should include a small centered SVG ornament, one explanatory line, and a ghost CTA where applicable: `docs/design/v2-houfeng/design-language.md:247-260`.
- `.empty-state` CSS currently only sets background, dashed border, padding, center alignment, and muted text: `web/src/styles/pages.css:328-336`.
- `IncidentList` empty state only renders heading + paragraph: `web/src/components/IncidentList.tsx:60-65`.
- `TargetProbeList` has a CTA but it is a plain native button, not a `Button` atom / ghost button: `web/src/components/target-detail/TargetProbeList.tsx:123-133`.

Impact:

- Usability is not broken, but v2 empty states are visually incomplete and inconsistent. This is noticeable on early-account/empty-data flows.

Suggested repair:

- Add a shared empty-state ornament pattern through CSS/markup without creating a new atom unless repetition becomes heavy.
- Use existing `Button variant="ghost"` for empty-state CTAs.
- Add targeted tests only for expected CTA presence/click behavior, not decorative rendering.

#### P2 - Drawer and ChangePasswordModal are aria-modal dialogs but do not manage focus/trapping

Evidence:

- v2 Drawer spec requires React portal + ESC close + overlay close + `aria-modal="true"`: `docs/design/v2-houfeng/component-spec.md:105-109`.
- Current `Drawer.tsx:45-70` renders inline rather than through a portal, and `Drawer.tsx:27-38` only handles Escape; it does not move focus into the dialog, trap Tab, or restore focus to the trigger.
- Drawer tests cover render/open/close/Esc/side positioning, but not focus management: `web/src/components/atoms/Drawer.test.tsx:5-90`.
- `ChangePasswordModal.tsx:40-82` renders a modal backdrop/form with `autoFocus` on the first input, but no `role="dialog"`, `aria-modal`, focus trap, Escape handling, or focus restoration.

Impact:

- Keyboard and screen reader users can remain focused behind modal/drawer content despite `aria-modal`, especially for history drawers with long content.
- This is an accessibility/usability risk rather than a data-flow bug.

Suggested repair:

- Decide whether Drawer needs the full portal/focus-trap contract now. If yes, implement focus capture, Tab containment, Escape close, and trigger focus restoration; then add RTL tests for initial focus and Tab behavior.
- Update ChangePasswordModal to use dialog semantics and Escape/focus handling, or convert it to the Drawer/modal primitive once one exists.

#### P2 - StatusBadge tone inference maps maintenance/observing/unbound statuses to notice and conflict/offline states to critical

Evidence:

- `StatusBadge.tsx:18-25` maps `维护中`, `观察中`, `待接入` to yellow/notice and maps `暂停`, `已归档`, `已退役`, `不续费`, `指纹变更待确认` to red/critical.
- v2 design language has distinct semantic states for `maintenance` and `offline`: `docs/design/v2-houfeng/design-language.md:211-224`.
- v2 component spec says the legacy `StatusBadge` wrapper maps only old explicit tones (`cyan|green|yellow|red|slate`) to v2 tone vocabulary: `docs/design/v2-houfeng/component-spec.md:112-116`; it does not define the current label-inference policy for lifecycle/run/binding labels.

Impact:

- Some normal operational states may be visually over-severe. For example, `维护中` can be an intentional state but is colored as notice rather than maintenance; `暂停` and `已归档` are currently rendered as critical via red.
- This may conflict with the product’s status language and create alert fatigue.

Suggested repair:

- Revisit `inferTone(label)` with product semantics:
  - `维护中` likely maps to maintenance.
  - `暂停`, `已归档`, `已退役`, `不续费` likely map to offline/dim rather than critical.
  - `指纹变更待确认` can remain critical/alert depending on desired priority.
- Add `StatusBadge.test.tsx`; there is currently no dedicated unit test for this wrapper, only page-level presence assertions.

#### P2 - DataTable keyboard row activation exists but lacks a direct test

Evidence:

- `DataTable.tsx:84-99` makes clickable rows focusable and activates `onRowClick` on Enter/Space.
- `DataTable.test.tsx:28-35` covers mouse click only; it does not cover keyboard activation.

Impact:

- The implementation is currently reasonable, but this is an unguarded accessibility contract. Future table refactors could break keyboard row navigation without tests catching it.

Suggested repair:

- Add an atom-level test that focuses a clickable row and fires Enter/Space, asserting `onRowClick` fires and Space prevents default.

### Current Positive Evidence / Not Current Bugs

- `NodesPage` no longer directly calls `fetch` for create node. It imports `createNode` from `lib/api` (`web/src/pages/NodesPage.tsx:25-38`) and calls it at `web/src/pages/NodesPage.tsx:435`; `web/src/lib/api.ts:140` defines the API function. Older release/spec notes about `NodesPage.tsx:60` direct fetch are stale.
- `NodesPage` now has rich filters for group/region/city/provider/lifecycle/run status/health/labels/abnormal. Evidence starts at `web/src/pages/NodesPage.tsx:64-74` and UI renders through `FilterBar` beginning at `web/src/pages/NodesPage.tsx:1078`.
- `TargetsPage` now has target filters for group/type/run status/health/labels/execution labels/abnormal. Evidence: `web/src/pages/TargetsPage.tsx:65-73`, filtering logic at `web/src/pages/TargetsPage.tsx:576-595`, active-filter detection at `web/src/pages/TargetsPage.tsx:597-604`.
- Sidebar count badges comply with the v2 “neutral count, not alarm state” rule. v2 spec says nav count badge must force neutral: `docs/design/v2-houfeng/component-spec.md:165-169`; implementation does this at `web/src/app/layout/Sidebar.tsx:51-58`.
- `DataTable` already uses semantic table markup and keyboard row activation (`web/src/components/atoms/DataTable.tsx:55-120`), aligning with v2 `DataTable` semantics at `docs/design/v2-houfeng/component-spec.md:87-93`.
- Hover-only row actions in Nodes/Targets are mitigated by `:focus-within`, not purely mouse-only. Example target CSS: `web/src/styles/pages.css:1304-1319`.
- `make verify-web` truth is good in code/CI: Makefile includes lint/test/build (`Makefile:65-67`) and CI runs `make verify-web` (`.github/workflows/ci.yml:19-29`). The drift is documentation/spec, not the command itself.

### Missing / Weak Tests To Add

- `EventsPage.test.tsx`: assert `include_backfilled` behavior explicitly, either URL query propagation or a disabled/not-supported state.
- `StatusBadge.test.tsx`: cover label-to-tone inference for maintenance/offline/critical-ish labels.
- `DataTable.test.tsx`: cover keyboard activation via Enter and Space.
- `Drawer.test.tsx`: cover initial focus, focus restoration, and Tab containment if Drawer keeps `aria-modal`.
- `ChangePasswordModal.test.tsx`: cover dialog semantics/Escape/focus behavior if the modal remains custom.
- UI drift tests should stay behavior-oriented. Do not add pixel assertions in jsdom.

### Verification Commands

Recommended after repairs:

```bash
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
make verify-web
```

Full repo verification when web changes accompany backend/contract changes:

```bash
./scripts/verify.sh
```

Notes:

- I did not run these commands in this research pass because the user explicitly restricted edits to this research file. `npm run build` can update build artifacts, and `make verify-web` runs `npm ci`, which rewrites `node_modules`.
- If a later implementation changes user-visible UI, also follow the project’s manual visual evidence process referenced by `.trellis/spec/web/quality-guidelines.md:163`.

## Caveats / Not Found

- No browser screenshot or Playwright pass was performed in this research-only step. Findings are based on static code/docs inspection.
- No code or docs were edited outside this file.
- The Trellis active-task pointer is absent in the current session; user-confirmed task directory was used.
- Some release gap rows are stale relative to current code. This audit treats live code as current truth and calls out stale documentation separately where it could mislead future work.

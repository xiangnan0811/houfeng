# UX-7A Evidence Components and Route Hardening

## Goal

Start UX-7 by hardening UI patterns that are already stable after UX-1 through UX-6: the observability evidence lead/focus pattern and the SPA route loading boundary. The work should reduce future edit cost, preserve the current visual result, and address the long-running Vite main chunk warning without changing backend contracts or page information architecture.

## What I Already Know

* UX-6A/6B/6C established nearly identical evidence lead and priority focus structures across Nodes, Targets, and Events.
* The repeated pattern uses the same CSS grouped selectors in `web/src/styles/pages.css`, but the JSX is still duplicated in page-private support surface files.
* `npm run build` has consistently passed with a Vite warning that the main JS chunk is larger than 500 kB.
* `web/src/app/router.tsx` currently imports all route pages eagerly, so one initial app bundle contains Dashboard, VPS, observability, Settings, and detail page code.
* The project has no e2e/browser automation dependency in repo; visual evidence is local preview/browser sanity plus optional committed screenshots.
* The user explicitly asked this session to continue in the main conversation without subagents. This task will therefore follow Trellis artifacts but implement and check directly in the main session.

## Requirements

* Extract a shared, pure presentation component for the observability evidence lead.
* Extract a shared, pure presentation component for the observability priority/stable focus row.
* Keep the components route-agnostic enough for `components/`: they may render passed React nodes/links/buttons, but should not own route decisions or call APIs.
* Preserve the current product copy, action behavior, links, filter chips, stable states, and support surface layout on `/nodes`, `/targets`, and `/events`.
* Replace repeated page-specific evidence CSS selectors with shared `.evidence-*` classes while keeping existing v2 visual treatment.
* Add focused component tests for the new shared components.
* Update existing Nodes/Targets/Events page tests only as needed to preserve behavior coverage.
* Split route pages with `React.lazy` / `Suspense` at the route boundary so the initial app shell no longer eagerly imports every page.
* Add a route-level loading fallback that follows v2 loading language: calm surface, Chinese copy, no spinner.
* Add or update tests that prove router route matching still works with lazy route elements and that the fallback is present.
* Record local visual evidence for the affected routes because this is user-visible UI structure work.

## Acceptance Criteria

* [x] `/nodes`, `/targets`, and `/events` still show their evidence lead, visible filter context, primary lead action, secondary asset/VPS link, priority focus, and stable focus behavior.
* [x] Clearing Dashboard deep-link filters still works from the support surface when the current filtered result is empty.
* [x] New shared components have colocated Vitest tests.
* [x] Route pages are lazy-loaded through `web/src/app/router.tsx` without breaking route matching or authenticated shell behavior.
* [x] `npm run build` no longer emits the Vite `Some chunks are larger than 500 kB` warning. Build output split route chunks and the entry chunk is `291.72 kB` / `93.20 kB gzip`.
* [x] `npm --prefix web run lint`, focused tests, `npm --prefix web run build`, and full `make verify-web` pass.
* [x] Browser sanity covers `/nodes`, `/targets`, and `/events` at `1440x1000` and `390x900`, checking no blank viewport, no page-level horizontal overflow, and no evidence lead/focus overlap.

## Non-Goals

* Do not change backend API contracts or data semantics.
* Do not alter Dashboard, VPS, Node detail, Target detail, or Settings information architecture in this task.
* Do not introduce React Query, Zustand, Playwright, Cypress, visual regression dependencies, or a new CSS framework.
* Do not make generic design-system atoms out of business-specific evidence components; this pattern belongs in `components/`, not `components/atoms/`.
* Do not mark screenshots accepted without human review.

## Technical Notes

* Candidate files:
  * `web/src/components/ObservabilityEvidenceLead.tsx`
  * `web/src/components/ObservabilityEvidenceFocus.tsx`
  * `web/src/components/ObservabilityEvidenceLead.test.tsx`
  * `web/src/components/ObservabilityEvidenceFocus.test.tsx`
  * `web/src/pages/nodes/NodesSupportSurface.tsx`
  * `web/src/pages/targets/TargetsSupportSurface.tsx`
  * `web/src/pages/events/EventsSupportSurface.tsx`
  * `web/src/app/router.tsx`
  * `web/src/styles/pages.css`
* Applicable specs:
  * `.trellis/spec/web/component-conventions.md`
  * `.trellis/spec/web/directory-structure.md`
  * `.trellis/spec/web/styling-guidelines.md`
  * `.trellis/spec/web/quality-guidelines.md`
  * `.trellis/spec/guides/code-reuse-thinking-guide.md`
  * `docs/design/v2-houfeng/design-language.md`
  * `docs/design/v2-houfeng/component-spec.md`
  * `docs/operations/v2-visual-evidence.md`

## Research References

* `research/local-ux7a-audit.md` — local audit of duplicated evidence UI and eager route bundling.

## Definition of Done

* Work is committed on a non-main branch.
* PR is opened, CI is monitored to green, PR is merged, and local `main` is synced.
* Trellis task is archived and the developer journal records the session.

## Verification

* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- ObservabilityEvidenceLead.test.tsx ObservabilityEvidenceFocus.test.tsx NodesPage.test.tsx TargetsPage.test.tsx EventsPage.test.tsx --run` — pass, 5 files / 86 tests.
* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- RouteModuleFallback.test.tsx api.test.ts --run` — pass, 2 files / 36 tests.
* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- RouteModuleFallback.test.tsx ObservabilityEvidenceLead.test.tsx ObservabilityEvidenceFocus.test.tsx NodesPage.test.tsx TargetsPage.test.tsx EventsPage.test.tsx api.test.ts --run` — pass, 7 files / 122 tests.
* `npm --prefix web run lint` — pass.
* `npm --prefix web run build` — pass, route chunks emitted, no Vite large chunk warning, entry chunk `291.72 kB`.
* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web` — pass, 63 files / 477 tests, build pass.
* Local browser sanity with temporary mocked API and Vite preview:
  * Preview URL: `http://127.0.0.1:5181/`
  * Data source: temporary local mocked API on `http://127.0.0.1:5182`
  * Routes checked: `/nodes`, `/targets`, `/events`
  * Viewports checked: `1440x1000`, `390x900`
  * Result: all six route/viewport combinations passed; document/body scroll width matched viewport width; `.observability-evidence-lead` and `.observability-evidence-focus` were present, non-empty, and within the viewport.
  * Screenshots: none committed; browser sanity was geometry/text based.
  * Limitation: local browser automation used Homebrew Playwright Python with repo-local `TMPDIR`; no CI browser automation was introduced.
* Local environment note: this machine runs Node `v24.14.1` while `web/package.json` declares Node `22.x`; `npm ci` prints `EBADENGINE`, but CI uses Node 22 and local verification completed successfully.

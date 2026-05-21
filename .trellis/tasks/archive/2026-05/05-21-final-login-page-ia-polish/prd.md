# Final LoginPage IA polish

## Goal

Finish the final remaining independently unoptimized routed page by giving `LoginPage` a tiny, truthful v2 IA/visual polish: keep the authentication flow exactly as-is while removing stale product claims and making the login card match the active Houfeng design language.

## Requirements

* Treat this as a frontend-only LoginPage micro-polish, not a new authentication feature.
* Preserve existing auth behavior:
  * `/login` remains the public login route.
  * Submit still calls `useAuth().login(username, password)` with the entered credentials.
  * Successful login still navigates to `next` query param, falling back to `/`, with `replace: true`.
  * Failed login still shows a generic Chinese error without leaking backend details.
  * The error remains an in-place `role="alert"` region.
* Align visible card content with the active v2 LoginPage template:
  * full-screen centered card,
  * seal/aurora feel,
  * `候风` serif brand,
  * `HOUFENG` / Fleet Control Plane framing,
  * `察变 · 守望` motto,
  * primary large login button.
* Remove or reframe the hardcoded `v1.0` footer because it is not backed by a live frontend/version contract and can become false.
* Keep UI copy truthful to current project reality: do not present Houfeng as multi-user SaaS, production packaging, full-permission personal system, completed inventory validation, or a general enterprise platform.
* Use existing atoms and the existing `web/src/pages/LoginPage.css` exception only; do not add dependencies, new routes, API calls, or shared abstractions.
* Keep tests guarding against misleading `单用户` / `全权限` / `个人系统` phrasing and add/adjust assertions for the final visible IA copy.

## Acceptance Criteria

* [ ] Login still submits username/password through `useAuth().login` and redirects through the existing `next` behavior.
* [ ] Failed login still renders the generic error in `role="alert"`.
* [ ] The submit button uses the existing primary large Button contract.
* [ ] The stale hardcoded `v1.0` footer/version claim is removed or replaced with truthful non-version copy.
* [ ] Visible LoginPage copy remains Chinese-first and does not include `单用户`, `全权限`, `个人系统`, false version, or overclaiming platform language.
* [ ] Focused `LoginPage` tests cover auth behavior plus the final IA/truthfulness copy.
* [ ] Web lint/test/build and full repo verification pass, or environment caveats are recorded truthfully.
* [ ] Trellis task is archived, PR/release follow-through completes, and final branch/task state is clean.

## Definition of Done

* Focused LoginPage tests pass.
* `npm --prefix web run lint` passes.
* `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` passes.
* `npm --prefix web run build` passes.
* `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` passes.
* Browser sanity is attempted for `/login` per `docs/operations/v2-visual-evidence.md`; if local browser tooling is unavailable, record the limitation in the task rather than adding dependencies.
* PR/release/main CI/publish-images follow-through completes per project memory.

## Technical Approach

* Keep changes scoped to `web/src/pages/LoginPage.tsx`, `web/src/pages/LoginPage.css`, and `web/src/pages/LoginPage.test.tsx` unless verification exposes a necessary adjacent fix.
* Prefer small JSX/copy/class adjustments over abstractions.
* Use the existing `Button` atom with `variant="primary" size="lg"` for the submit action.
* Replace the hardcoded version footer with truthful product/auth-boundary copy or remove it entirely.
* If styling changes are needed, keep them in the existing LoginPage CSS file and use tokens/BEM; preserve the full-screen centered card layout.

## Decision (ADR-lite)

**Context**: The previous page audit concluded that high-value IA routes are now optimized and `LoginPage` is the only remaining routed page without an independent final polish. The page already has the v2 skeleton but still contains a hardcoded `v1.0` footer and a medium submit button that does not match the active LoginPage component contract.

**Decision**: Do a tiny frontend-only LoginPage pass that removes stale/unsupported product claims and aligns the auth card with the v2 template, while freezing all authentication semantics.

**Consequences**: This completes the page IA batch sequence without opening backend/auth/session scope. The result should be intentionally small; any deeper auth UX, password reset, user management, version API, or onboarding integration remains out of scope.

## Out of Scope

* Backend, auth/session API, user model, permission model, password reset, or multi-user account-management changes.
* New API calls, live version endpoint, release metadata plumbing, or AppShell changes.
* Router/auth-gate changes outside preserving the existing `/login` behavior.
* New dependencies, CSS frameworks, CSS-in-JS, theme token changes, or new page CSS files.
* Claims about production readiness, packaging, Docker/Kubernetes, real-inventory validation, single-user/full-permission personal-system positioning, or enterprise SaaS readiness.
* Reworking already shipped page IA batches.

## Technical Notes

* Current task: `.trellis/tasks/05-21-final-login-page-ia-polish`.
* Feature branch: `feature/final-login-page-ia-20260521`.
* Relevant specs: `.trellis/spec/web/component-conventions.md`, `.trellis/spec/web/styling-guidelines.md`, `.trellis/spec/web/state-and-data.md`, `.trellis/spec/web/quality-guidelines.md`, `.trellis/spec/guides/branch-workflow-governance.md`, `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`, `docs/operations/v2-visual-evidence.md`.

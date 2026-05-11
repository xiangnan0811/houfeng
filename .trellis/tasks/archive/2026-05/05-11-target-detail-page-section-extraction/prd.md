# Target Detail Page Section Extraction

## Background

`web/src/pages/TargetDetailPage.tsx` remains one of the largest frontend route files after the Asset Ledger tasks and the previous `NodeDetailPage` extraction. The roadmap no longer has immediate Asset Ledger implementation work except user-data-dependent real data validation, so the next useful plan-aligned cleanup is to pay down the deferred long-page split risk in the target detail route.

This task is a page-private refactor. It must preserve the current Target detail UX and data contract.

## Scope

- Extract Target detail page-only types, constants, and pure helper functions into `web/src/pages/target-detail/`.
- Extract presentational sections from `TargetDetailPage.tsx` into page-private components under `web/src/pages/target-detail/`.
- Keep `TargetDetailPage.tsx` as the route assembly owner for API calls, effects, request guards, mutation handlers, refs, and state transitions.
- Preserve all existing API calls through `web/src/lib/api.ts`; do not add new endpoints or direct `fetch`.
- Preserve existing copy, class names, focus restoration behavior, confirmation flows, and test-visible interaction behavior.
- Do not move existing cross-page/shared components from `web/src/components/target-detail/`.

## Non-goals

- No visual redesign.
- No API contract changes.
- No backend changes.
- No new global state, React Query, or shared abstraction.
- No release/publish workflow changes.
- No subagent execution.

## Acceptance Criteria

- `TargetDetailPage.tsx` is materially smaller and delegates page section rendering to page-private components.
- Extracted components are controlled/presentational and do not call the API client.
- Extracted helpers are pure and keep the same validation/error behavior.
- Existing `TargetDetailPage` tests pass without behavior rewrites.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green CI, merge, and local `main` sync.

## Verification

- `cd web && npm run lint`
- `cd web && npm run test -- --run src/pages/TargetDetailPage.test.tsx`
- `cd web && npm run build`
- `make verify-web`

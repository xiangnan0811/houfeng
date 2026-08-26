# Ship VPS overview launch P1

## Goal

Deliver the VPS overview launch slice across lifecycle cancellation, terminal-state routing, overview destinations, monitoring deep links, events filtering, verification, PR, release, and post-release cleanup.

## Requirements

- Cancellation previews must be content-addressed by every mutable lifecycle,
  renewal, monitoring, relation, target and recommended-step input that can
  change the resulting plan.
- Cancellation apply must use a serializable, bounded-retry transaction. Raw
  PostgreSQL `40001` and `40P01` conflicts at begin, lock, preview, audit insert,
  step and commit boundaries must retry without writing a failed business
  action; exhaustion must return a typed HTTP 409 contract.
- Ordinary VPS patch entrypoints must not write controlled lifecycle states,
  and cancelled/archived assets must remain read-only even under concurrent
  writes. The locked row remains the authority when classifying failed writes.
- Terminal VPS routes must replace to `/archive/:id` before capability
  selection or auxiliary legacy requests. Only lifecycle-appropriate
  cancellation/archive entrypoints may remain visible.
- Modern and legacy cancellation workbenches must reject stale previews and
  late async results across same-route, A→B and A→B→A generations.
- Overview anomaly and relation actions must resolve only to allowlisted
  application-owned routes or page-owned commands. Event object IDs and
  monitoring runtime-stream URLs must fail closed against traversal,
  fragments, origin confusion and foreign hosts.
- Keyboard, mobile, accessibility and fixture contracts must cover the real
  browser destinations, including native Tab exit and runtime WebSocket use.
- Delivery must pass the pinned Go 1.26.2 toolchain, real PostgreSQL
  concurrency, Node 22 full Web verification and the complete Playwright suite,
  then proceed through protected-main PR, CI and release automation.

## Acceptance Criteria

- [x] Preview digest and stale-generation contracts cover lifecycle, renewal,
  monitoring, relations, targets and recommended steps.
- [x] Cancellation apply retries `40001`/`40P01` at all transaction cut points,
  emits no failed audit for retryable conflicts, and maps exhausted retries to
  `409 lifecycle transaction conflict`.
- [x] A real PostgreSQL test forces two production transactions to wait on the
  VPS row lock, completes without deadlock, emits no failed action, and cleans
  up the holder on every exit path.
- [x] Public ordinary patch paths reject controlled lifecycle states and
  cancelled/archived concurrent updates.
- [x] Terminal routing, workbench visibility and async ownership are covered for
  modern and legacy pages.
- [x] Event and overview destinations, monitoring detail/runtime facts and exact
  runtime-stream origins are covered by unit and browser tests.
- [x] Native Tab exits the VPS management menu in Chromium without restoring
  focus to the trigger or body.
- [x] `make verify-go`, full Web verification, CSS/bundle budgets, production
  build, 199-file/1404-test Vitest and 129-test Playwright are green.
- [ ] Feature commits are reviewed and merged through a protected-main PR.
- [ ] Main CI, Release Please, GitHub release and published container image for
  the resulting version are verified before cleanup.

## Notes

- This task retroactively records the uncommitted branch after repeated
  read-only review. Do not edit the archived predecessor task designs.
- Source branch: `fix/vps-overview-launch-p1`; target branch: `main`.

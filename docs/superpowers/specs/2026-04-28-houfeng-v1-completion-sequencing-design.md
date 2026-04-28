# Houfeng V1 Completion Sequencing Design

## Context

Houfeng is in early V1 implementation. The frozen V1 product, interaction, visual, and technology baseline remains the source of truth:

- Product name: `候风 / Houfeng Fleet Control Plane`
- System shape: single-user control plane, PostgreSQL, and a systemd agent fleet
- Technology path: Go center, Go agent, React/Vite frontend, PostgreSQL
- Visual authority: Unified / Baseline Stitch screens

Current implementation already contains the core V1 skeleton: center service, agent runtime, PostgreSQL schema, React pages, object CRUD/control flows, observation ingestion, incident/event/notification paths, and verification scripts. The remaining work should not reopen product design. It should close implementation gaps against the frozen V1 baseline while preserving correctness and future maintainability.

The user has no local feature priority preference. The desired optimization target is:

1. Complete frozen V1 correctly.
2. Avoid introducing regressions or misleading UI behavior.
3. Establish clean foundations for later development.

## Sequencing Principle

Use **dependency/risk-first sequencing with controlled parallelism**.

Do not optimize primarily for visible UI progress or raw parallel throughput. The order should first make system semantics true, then make product surfaces complete.

### Rules

1. **Fix data correctness and runtime semantics first.**
   - Agent durable buffering and backfill.
   - Node pause, maintenance, and retired states as real sync-plan semantics.
   - Runtime-effective settings, or explicitly marked policy-only settings.

2. **Then complete user-facing acceptance surfaces.**
   - Dashboard abnormal object summaries.
   - Events advanced filters.
   - Trend degradation and trend display.
   - Visual baseline verification.

3. **Use parallel work only where coupling is low.**
   - Safe to parallelize: agent buffer design and dashboard query design.
   - Do not parallelize tightly coupled semantics and UI validation that depends on them.
   - Do not finalize Settings copy before the runtime behavior it describes is settled.

4. **Every slice requires explicit verification.**
   - Backend behavior changes need focused tests first.
   - Frontend interaction changes need Testing Library coverage.
   - Cross-boundary changes need at least one integration-style verification path.
   - Visual work needs reproducible visual evidence.

## Recommended V1 Completion Phases

### Phase 1: Runtime Semantics Correction

Goal: make currently exposed runtime controls and settings truthful.

Scope:

1. **Node sync-plan runtime semantics**
   - `暂停`: stop host sample collection and probe assignments for that node.
   - `维护中`: continue collection, but mark observations with maintenance context so incident/notification behavior is suppressed where appropriate.
   - `已退役`: remove the node from the active fleet for normal collection/assignment purposes.

2. **Settings runtime truthfulness**
   - Correct TLS default frequency semantics to match the frozen V1 expectation of `6h`.
   - Make notification timing flags (`notify_on_started`, `notify_on_escalated`, `notify_on_recovered`) operative, or explicitly label them as policy-only until implemented.
   - Keep ProbeItem creation defaults and settings defaults consistent.

3. **UI/backend consistency**
   - UI copy that says an action stops collection must match backend behavior.
   - UI copy that says a setting affects runtime must reflect actual runtime wiring.

Primary verification:

- Sync-plan tests for Node pause, maintenance, and retired behavior.
- Incident/notification tests for maintenance suppression and notification flags.
- Frontend tests for updated defaults and truthful copy.

### Phase 2: Agent Reliability Closure

Goal: satisfy the frozen V1 requirement that the agent has short-term persistent buffering.

Scope:

1. Add an agent-side durable queue or buffer for locally collected facts.
2. Sync unacknowledged batches to the center.
3. Delete confirmed batches after center acknowledgement.
4. Preserve observations while the center is temporarily unavailable.
5. Mark recovered historical submissions as `is_backfilled=true`.
6. Bound buffer growth by size, age, or both.

Primary verification:

- Agent queue tests for enqueue, retry, ack deletion, restart persistence, and backfill marking.
- Runtime tests for center-unavailable behavior.
- Center ingestion tests confirming backfilled records remain raw facts but suppress noisy notifications where required.

### Phase 3: Retention and Aggregation Execution

Goal: make V1 data retention policy real rather than stored-only.

Scope:

1. Add a retention/cleanup worker.
2. Apply raw layer retention to raw observation tables.
3. Preserve event and notification history according to configured policy.
4. Add minimal aggregate storage or read model if needed for long-term trend/summary support.
5. Update Settings copy so it reflects the actual execution state.

Primary verification:

- Retention policy validation tests.
- Worker tests with controlled timestamps.
- Migration tests for any aggregate schema additions.

### Phase 4: Dashboard and Events Acceptance Surfaces

Goal: complete frozen V1 observability surfaces after runtime semantics are trustworthy.

Scope:

1. Dashboard current abnormal Node summary list.
2. Dashboard current abnormal Target summary list.
3. Event filters for:
   - object type
   - event type
   - time range
   - severity
   - labels
   - notification-only
   - recovery-only
   - maintenance-only
4. Backend query contracts and frontend filter state must stay aligned.

Primary verification:

- Dashboard repository and handler tests for abnormal object summaries.
- Events query tests for each filter class.
- Frontend tests for filter construction, rendering, empty states, and reset behavior.

### Phase 5: Trend Degradation and Trend Surfaces

Goal: complete the third frozen V1 incident class and the detail-page trend expectation.

Scope:

1. Target latency trend degradation.
2. Node `iowait`, `steal`, and load trend degradation.
3. Conservative thresholds that default to `关注` or `告警`, not noisy critical alerts.
4. Node detail and Target detail recent trend display.
5. Avoid introducing a generic rule engine or unrestricted per-object rules.

Primary verification:

- Evaluator tests for conservative trend detection and recovery.
- Runtime facts or aggregate query tests for trend data.
- Frontend tests for detail trend rendering.

### Phase 6: V1 Delivery Verification

Goal: prove the frozen V1 can be delivered and operated.

Scope:

1. Unified / Baseline visual verification.
2. systemd unit examples for center and agent where appropriate.
3. Deployment and local smoke-run documentation.
4. Fresh-install smoke path:
   - create Node
   - enroll agent
   - create Target
   - add ProbeItem
   - receive observations
   - trigger and recover an incident
   - verify event and notification path
5. Final V1 gap checklist with every item either closed or explicitly deferred.

Primary verification:

- Full `./scripts/verify.sh`.
- Visual-verdict or equivalent screenshot evidence for primary baseline screens.
- Documented manual smoke evidence for the complete V1 path.

## Execution Model

Each phase should produce its own implementation plan. Do not create one oversized V1 mega-plan.

Recommended execution loop:

1. Use `superpowers:writing-plans` for the next phase.
2. Execute that plan with `superpowers:subagent-driven-development`.
3. Split phase tasks into bounded work units.
4. Run independent tasks in parallel when file ownership and semantics are disjoint.
5. Keep shared semantic changes sequential.
6. Verify before claiming completion.
7. Commit after coherent slices using the Lore commit protocol.

### Phase-Level Parallelism

Use controlled parallelism inside a phase:

- Backend tests and frontend tests can be prepared in parallel when their contracts are already settled.
- Store/query work and UI rendering work can be parallel after API response shapes are fixed.
- Agent buffer internals should be isolated from center ingestion changes until the batch contract is stable.

Avoid parallelism that creates semantic races:

- Do not finalize UI copy before backend behavior is known.
- Do not implement trend UI before the trend data contract is stable.
- Do not implement retention copy before worker behavior is decided.

## Non-Goals

The V1 completion sequence must not add:

- V2 product capabilities.
- Multi-user or permissions model.
- Generic rule engine.
- Arbitrary script execution.
- Docker orchestration.
- Complex plugin system.
- Per-object unrestricted alert strategy editor.
- Broad redesign of the frozen visual baseline.

## Completion Criteria

V1 completion is reached when:

1. Frozen V1 objects, states, collection, probes, incidents, events, and notifications form a working loop.
2. UI copy and backend behavior are consistent.
3. Agent short-term facts survive temporary center/network failure.
4. Data retention policy has an execution path.
5. Dashboard, Events, and detail pages support the frozen V1 monitoring and troubleshooting path.
6. The primary Unified / Baseline screens have visual verification evidence.
7. systemd/deployment documentation is sufficient to operate one center and N agents.
8. Full automated verification passes.
9. Remaining limitations, if any, are explicit deferred items rather than hidden gaps.

## First Plan Handoff

The next implementation plan should cover **Phase 1: Runtime Semantics Correction** only.

It should be scoped to:

- Node sync-plan semantics for pause, maintenance, and retired states.
- Runtime-effective settings corrections needed before later phases.
- Tests that lock behavior before implementation.

It should not include:

- Agent durable buffer/backfill.
- Retention workers.
- Dashboard abnormal summary implementation.
- Trend degradation.
- Visual QA.

Those belong to later phase plans.

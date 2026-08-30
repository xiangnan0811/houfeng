# Center replay / out-of-order monotonicity audit

## Scope and answer to field-audit question 2

This audit covers the accepted sync transaction, raw monitoring facts, monitoring-instance lifecycle/freshness, latest-fact readers, incident/event/current-health projection, and daily retention aggregates. It is source/test research only; no product code, database, deployment, or git state was changed.

**Short answer:** existing SQL prevents an older accepted batch from decreasing `monitoring_instances.last_heartbeat_at` or `last_sync_at`, prevents it from reverting a non-pending lifecycle, and prevents a stale command result from overwriting a different in-flight action. Exact replay of the same `(monitoring_instance_id, sync_batch_id)` also does not insert the raw facts a second time. It does **not** fully prevent older work from reverting current incident/health state: post-sync incident evaluation runs after the ingest transaction, mutations carry no durable revision/watermark, the writer deletes and recreates the whole active set, and the summary update is unconditional. Equal-observation-time facts and aggregates rebuilt after raw retention introduce two further order/completeness holes.

The intended invariant must be scoped **per binding/trust epoch**. Confirming a rebind or resetting binding intentionally clears `last_heartbeat_at` and `last_sync_at`; that is an administrative epoch boundary, not an out-of-order regression (`internal/center/store/monitoring_instances.go:1494-1515`, `internal/center/store/monitoring_instances.go:1617-1645`).

## What must be monotone or order-invariant

| Surface | Required invariant | Current behavior | Assessment |
|---|---|---|---|
| `last_heartbeat_at` | Within one binding epoch, never less than the greatest accepted heartbeat `observed_at`. | Batch-local maximum plus SQL `GREATEST`; the monitoring-instance row is locked before validation/write. | Protected in implementation, but only SQL-shape unit coverage plus duplicate-only real-PG coverage exists. |
| `last_sync_at` | Within one binding epoch, never less than the greatest successful receipt time. Exact duplicate must not make a node look newly synced. | SQL `GREATEST`; exact duplicate exits before timestamp update. | Protected. Note that a new old/backfilled batch legitimately advances receipt freshness while observation freshness stays old. |
| raw heartbeats/host/probe/IP-quality facts | Append-only; replay of the same carrier batch must be idempotent; different arrival permutations must preserve the same fact multiset. | One targetless batch marker keyed by monitoring instance plus **the first heartbeat's** `sync_batch_id`; raw fact tables otherwise use generated IDs and unconditional insert. | Exact same batch ID protected; logical duplicate under another batch ID, or mixed carrier batch IDs inside one request, is not protected. |
| lifecycle / monitoring / target run state | Agent fact arrival must not undo an administrative state. Pending-to-in-use promotion must have an explicit freshness policy. | Lifecycle is only `pending_enrollment -> in_use` when any host sample is present; other lifecycle values are retained. Paused/retired/archived writes are suppressed. Monitoring/run status is not fact-written. | No backward lifecycle transition. Semantic gap: a backfilled host sample currently counts as first onboarding evidence. |
| command action state | A result may complete only the exact current in-flight action/command. | Update is guarded by pending status, action ID, and command ID. | Protected. |
| latest host/probe/IP-quality read models | Strictly older observation cannot replace newer; equal-time conflict must resolve deterministically and should not let a backfill beat a live fact. | Ordered by `observed_at DESC` and generated row/report ID. | Strictly older protected; equal-time order is arrival-dependent. Incident normalizers sort only by time and can discard SQL tie order. |
| active incidents and object summary (`current_health_status`, active count, primary summary) | The **projection revision**, not severity, must increase monotonically. A mutation built from revision N must not apply after revision N+1. | Mutation has no expected revision/watermark. Writer deletes all active incidents for the object, reinserts the supplied set, and unconditionally rewrites object summary. | **Unprotected; highest-risk gap.** |
| `active_incidents.last_evaluated_at` | Must not decrease for a surviving incident within a projection epoch. | Evaluator assigns the carrier's/evaluation's `when`; store overwrites it without `GREATEST` or CAS. | Unprotected. `GREATEST` alone would not protect recovery/deletion or whole-object summary. |
| state-change events and notifications | Append-only recording time; a logical transition/notification has at most one durable side effect. | Random IDs, no logical uniqueness; exact duplicate sync still invokes post-sync evaluation. | Append-only, but not end-to-end idempotent under concurrent stale evaluations/retries. |
| daily aggregates | Values need not numerically increase, but result must be order-independent for the same fact set; count must not fall merely because a late fact arrived. | Recomputed from retained raw and overwrite-upserted. | Order-independent while all bucket raw remains. Can become partial/regress after raw purge followed by very old late data. |
| target `last_success_at` / `last_failure_at` | If used as latest-outcome timestamps, update each with `GREATEST` and derive outcome from a deterministic latest fact. | Columns/read DTOs exist, but no Center write path was found in the inspected source. | No current replay write-back bug, but no implemented monotonic contract either. |

Health severity itself must **not** be numerically monotone: a real recovery is supposed to move `critical/alert/notice -> normal`. The monotone object is the durable projection revision (or an equivalent source/trigger watermark) so that an older computation cannot apply after a newer one.

## Existing protections and their evidence

### 1. Accepted-batch transaction serializes per monitoring instance

`validateAcceptedSyncBatch` selects the monitoring-instance authority/current states `FOR UPDATE`, then validates bound status, current sync token, current fingerprint for every fact carrier, and suppression states (`internal/center/store/sync_batches.go:346-416`). Because fact insertion and freshness/lifecycle update occur in the same transaction, concurrent `ApplyBatch` calls for one monitoring instance serialize through this row lock.

The accepted path computes one Center receipt time, inserts the batch marker, returns early on a duplicate, writes facts, computes lifecycle from the locked current value, advances sync state, handles guarded command results/dispatch, and commits (`internal/center/store/sync_batches.go:83-155`).

### 2. Freshness timestamps are max-reduced

The heartbeat writer takes the maximum `observed_at` across all heartbeats in the request (`internal/center/store/sync_batches.go:462-502`). The instance update uses:

```sql
last_heartbeat_at = greatest(coalesce(last_heartbeat_at, $2), $2),
last_sync_at      = greatest(coalesce(last_sync_at, $3), $3)
```

and writes the lifecycle selected from the already locked current state (`internal/center/store/sync_batches.go:505-525`). Thus a newly accepted backfill with observation time T1 after a live heartbeat T2 cannot make `last_heartbeat_at < T2`; its current receipt can still make `last_sync_at` newer, which is correct because these fields represent different clocks.

The real-PG ACL/idempotency test proves one batch marker and one heartbeat survive an exact duplicate, and proves that duplicate receipt does not change either stored timestamp (`internal/center/store/sync_batches_postgres_integration_test.go:74-116`). It does **not** run live-T2 then backfill-T1, reverse the permutation, include host/probe facts, or assert incident/current-health state.

### 3. Exact carrier-batch replay is insert-once at the ingest layer

`agent_sync_batches` has primary key `(monitoring_instance_id, sync_batch_id)` (`db/migrations/0045_create_agent_sync_batches.sql:1-6`). The runtime-safe targetless `ON CONFLICT DO NOTHING` marker uses `batch.Heartbeats[0].SyncBatchID`; zero affected rows returns before heartbeat/fact/timestamp/command writes (`internal/center/store/sync_batches.go:101-114`, `internal/center/store/sync_batches.go:158-175`). The unit test explicitly asserts no fact insert and no freshness update for a duplicate (`internal/center/store/sync_batches_test.go:273-311`).

This protection is batch-level, not fact-level. Host/probe writers are unconditional inserts (`internal/center/store/observations.go:53-168`), and their tables have only generated primary keys rather than carrier uniqueness (`db/migrations/0001_initial_schema.sql:65-108`). The HTTP validator checks that each carrier has a non-empty batch ID but does not require all carrier IDs in one request to equal the first heartbeat ID (`internal/center/http/handlers/agent.go:341-457`). Minimal hardening is to validate one canonical batch ID across every heartbeat/host/probe/IP-quality carrier and keep it as the only idempotency identity.

### 4. Agent facts do not reverse administrative state

The lifecycle helper only promotes pending enrollment when a host sample exists and otherwise returns the locked current state (`internal/center/store/sync_batches.go:419-424`). Existing tests cover promotion, remaining pending without a host sample, and retaining non-pending states (`internal/center/store/sync_batches_test.go:313-369`, `internal/center/store/sync_batches_test.go:415-455`). Paused monitoring, retired lifecycle, and archived instances are short-circuited before fact/plan writes (`internal/center/store/sync_batches.go:340-344`).

One unresolved semantic choice remains: `len(batch.Observations.HostSamples) > 0` does not inspect `IsBackfilled` or a source observation time relative to the binding epoch (`internal/center/store/sync_batches.go:126-128`). Recommended default: promotion should require at least one non-backfilled host sample belonging to the current trust epoch; otherwise a replay can certify current onboarding before any fresh observation. If product intent is that durable replay counts as valid first evidence, that exception should be explicit and tested rather than accidental.

Rebind itself rotates/clears sync authority and freshness (`internal/center/store/monitoring_instances.go:1494-1515`); onboarding evidence queries also scope stored facts to the current fingerprint and `received_at >= binding_epoch_started_at` (`internal/center/store/monitoring_instances.go:1342-1364`). Runtime/latest-fact queries do not carry this epoch scope, so after a rebind and before a new fact they can still expose a prior-epoch row. This is adjacent to, but not caused by, current queue replay.

### 5. Command results have an identity CAS

Stale command results update only when `last_action` is still pending and both action and command IDs match; zero rows is ignored (`internal/center/store/sync_batches.go:603-648`). This is the pattern the incident projection currently lacks.

## Remaining regressions and why current SQL is insufficient

### A. Critical: incident/current-health projection has a stale-writer race

`ApplyBatch` commits before the post-sync processor is called (`internal/center/syncing/service.go:89-99`). Post-sync uses `Result.AcceptedAt`, reads the monitoring instance/current incidents/recent facts, evaluates the monitoring instance, then evaluates touched targets (`internal/center/incidents/service.go:346-359`, `internal/center/incidents/service.go:470-558`). These reads and the later write are not under the ingest row lock or a shared projection transaction.

A concrete race:

1. Batch A commits and evaluation A reads incident state/raw snapshot.
2. Batch B commits; evaluation B reads newer state/snapshot and applies mutation B.
3. Delayed evaluation A applies mutation A last.

`IncidentMutation` carries only object identity, active set, events, and notifications—no expected revision or evaluation watermark (`internal/center/incidents/types.go:173-179`). `ApplyIncidentMutation` begins an independent transaction (`internal/center/store/incidents.go:132-154`), deletes **all** current incidents for the object, then inserts the stale supplied set (`internal/center/store/incidents.go:173-218`). `last_evaluated_at` is assigned directly from `excluded`, and because the delete ran first the `ON CONFLICT` branch usually cannot preserve any prior row anyway (`internal/center/store/incidents.go:183-204`). Finally, current health/count/summary are rewritten with no version predicate (`internal/center/store/incidents.go:305-384`).

Consequences include:

- active incident set and `current_health_status` reverting to A's snapshot;
- a surviving incident's `last_evaluated_at` moving backward;
- stale recovery deleting a newer active incident;
- duplicate started/escalated/recovered events, because events receive new random IDs with no logical transition key (`internal/center/store/incidents.go:221-269`);
- duplicate notification delivery/records, because notifications are evaluated and dispatched after the mutation call and also use generated IDs (`internal/center/incidents/service.go:601-607`, `internal/center/incidents/service.go:861-881`, `internal/center/store/incidents.go:273-301`).

Adding `GREATEST(last_evaluated_at, ...)` is not enough: it cannot protect deleted/recovered incidents, the whole active set, object summary, event insertion, or notification side effects.

### B. High: exact duplicate ingest is not end-to-end duplicate-safe

The repository returns an empty plan for a duplicate, but `syncing.Service` still invokes `AfterSuccessfulSync` after every successful repository call (`internal/center/syncing/service.go:89-99`). A duplicate therefore performs another snapshot/evaluation/mutation cycle even though no facts changed. Sequential evaluation often becomes a no-op, but it still rewrites the whole incident projection and creates a concurrency window; together with gap A it can duplicate transition side effects.

The apply result needs an explicit `FactsRecorded` / `Duplicate` disposition. Post-sync incident evaluation should run only for a newly recorded batch (administrative periodic sweeps remain independent). This preserves the current exact-empty sync plan while making duplicate idempotency cover side effects, not only raw rows.

### C. High: equal `observed_at` has arrival-dependent “latest” semantics

Strictly older facts are safe in normal sequential evaluation because latest host/probe readers sort by `observed_at DESC, id DESC` (`internal/center/store/runtime_facts.go:24-57`, `internal/center/store/runtime_facts.go:147-191`; incident snapshot equivalents at `internal/center/incidents/service.go:1200-1248`). A late T1 cannot precede T2 when T1 < T2.

At T1 == T2, generated ID makes the later insert win. A backfilled success/failure at the same timestamp can therefore replace a live fact solely because it arrived later. The evaluator then copies and re-sorts facts using an unstable comparator that compares only `ObservedAt`, losing the SQL tie order (`internal/center/incidents/evaluator.go:603-623`, `internal/center/incidents/evaluator.go:849-857`). Since latest backfilled facts suppress starts but can still create a silent recovery, the tie can change current state (`internal/center/incidents/evaluator.go:29-57`, `internal/center/incidents/evaluator.go:91-143`).

Minimal deterministic rule: order by `(observed_at DESC, is_backfilled ASC, received_at DESC, id DESC)` so an equal-time live fact beats backfill; use stable sorting or the same tuple in Go. If equal-time live facts can legitimately conflict, `received_at DESC, id DESC` defines a deterministic last-received tie break. Add the same policy to IP-quality “latest” selection, which currently uses observed time and report ID (`internal/center/store/ip_quality.go:245-309`).

### D. Medium: evaluator itself permits time regression

`recoverIfNeeded` does not reject `when < previous.LastEvaluatedAt`, and `evaluateTransition` always sets current `LastEvaluatedAt = when` even for a no-op (`internal/center/incidents/evaluator.go:288-353`). A writer-level object revision is the correctness boundary, but a defense-in-depth evaluator guard should ensure an older carrier cannot create a recovery/escalation or lower `LastEvaluatedAt` when called directly.

The current tests deliberately cover “backfilled latest fact silently recovers, without notification” (`internal/center/incidents/evaluator_test.go:267-332`); they do not cover a backfilled fact older than the previous incident watermark, same-time live/backfilled conflicts, or delayed mutation interleaving.

### E. Medium/latent: aggregate overwrite after raw retention

Retention first recomputes each closed UTC day from all currently retained raw rows and overwrite-upserts every aggregate field, then deletes raw older than `rawCutoff` (`internal/center/store/retention.go:35-96`, `internal/center/store/retention.go:112-195`). While a day's raw remains, late backfill is handled correctly and the final aggregate is permutation-independent.

After a day's raw rows have been purged, a newly accepted very old fact for that day becomes the only raw input. The next overwrite-upsert replaces the previously complete count/average/max/p95 with a partial aggregate computed from that late subset. Exact p95 cannot be losslessly merged from an old p95 plus a late row.

This is bounded in the default deployment: Center raw retention defaults/minimum to 30 days (`internal/center/settings/types.go:217-225`, `internal/center/settings/types.go:663-666`) while the Agent durable queue default is 72 hours (`agent/syncqueue/store.go:17-20`, `agent/config/config.go:13-16`). It is still possible if Agent max age is configured beyond Center raw retention, legacy queues violate the current bound, or policies drift. Because the task requires retaining old facts, silently rejecting them is not a valid fix. Either enforce a cross-component invariant `max replay age < raw retention` at configuration/handshake time, or redesign aggregates with retained mergeable state/sketches (especially for p95). The current v0.79.1 replay described in the field audit is expected to be inside the normal 30-day window, so this should be a documented follow-up unless evidence shows otherwise.

### F. Low/adjacent: unused target outcome timestamps lack a contract

`targets.last_success_at` and `last_failure_at` are exposed in types/read paths (`internal/center/targets/types.go:72-77`, `internal/center/store/targets.go:37-74`), but no writer was found in the inspected Center source. They currently cannot be rolled back by replay because they are not updated. If activated, each timestamp must use `GREATEST`, and “current result” must come from the deterministic latest-fact ordering above rather than from arrival order.

## Recommended minimal repair

### 1. Add durable object projection revision and compare-and-swap

Prefer an integer revision over a wall-clock timestamp. `AcceptedAt` is useful diagnostic metadata but comes from process time, may be equal/regress under clock skew, and target projection is shared by batches from multiple monitoring instances.

Minimal robust shape:

1. Add `incident_projection_revision bigint not null default 0` to both `monitoring_instances` and `targets`, or add a dedicated `(object_type, object_id, revision)` projection-head table.
2. Snapshot evaluation reads the object's current revision together with the state used to build the mutation.
3. Extend `IncidentMutation` with `ExpectedProjectionRevision`.
4. At the start of `ApplyIncidentMutation`, lock the object/projection-head row, compare the stored revision, and abort with a typed conflict before deleting/inserting anything if it differs.
5. On success, replace active incidents, insert transition events, update summary, and increment revision in the same transaction. Return `Applied`/new revision.
6. If a stale mutation is rejected, do not dispatch or append its notifications. The service may simply stop because a newer projection already won, or do one bounded re-read/re-evaluate retry if callers require immediate convergence.

An in-process keyed mutex is not sufficient: it does not coordinate multiple Center processes, periodic sweep versus sync processing across processes, or restarts. A `last_evaluated_at` predicate on individual active rows is also insufficient when the correct mutation is a recovery that deletes all rows.

### 2. Extend batch disposition through the service

Return `FactsRecorded=false` (or `Duplicate=true`) from the exact batch-marker conflict path and skip sync-triggered post-processing. Suppressed administrative batches should carry their own explicit disposition too; they currently return success/empty plan and would likewise reach post-sync. Preserve periodic evaluation as the recovery/sweep mechanism.

### 3. Canonicalize one batch ID and latest tie ordering

- Reject a request unless every carrier `sync_batch_id` equals the canonical first heartbeat batch ID.
- Keep raw tables append-only; do not discard old facts.
- Apply live-before-backfill and received-time/ID tie breaking consistently in runtime readers, incident snapshot readers, IP-quality latest readers, and Go normalizers.

### 4. Add evaluator defense in depth

For direct evaluator calls, if carrier/event `when` is before the previous incident's `LastEvaluatedAt`, return `TransitionSkipped` or preserve the previous current record without event/notification. Keep writer CAS as the authoritative concurrency boundary.

## Executable TDD test plan

### RED 1 — real PostgreSQL sync monotonicity permutations

Extend the strict PostgreSQL integration suite with two distinct batch IDs:

1. Apply live T2, then backfilled T1; assert two raw rows retained, `last_heartbeat_at == T2`, `last_sync_at == max(receipt times)`, and non-pending lifecycle unchanged.
2. Apply backfilled T1, then live T2 in a fresh fixture; assert the same final state/fact multiset.
3. Include host sample and probe observation in both batches; assert latest APIs return T2 after both permutations, including T2 host metrics and the T2 heartbeat's agent version (the monitoring-instance list currently selects agent version by `heartbeat.observed_at DESC, heartbeat.id DESC`, `internal/center/store/monitoring_instances.go:434-438`).
4. Repeat each exact batch; assert raw counts, freshness, command state, incident events, and notifications do not change.
5. Run same-time live/backfilled pairs in both arrival orders; assert live wins latest and current-health evaluation.

Run this through the strict direct-runtime PostgreSQL 16 fixture/role, not repository fakes. In one final assertion block, prove: both raw batches/facts remain append-only; exact duplicates add none; both `last_*` columns are the `GREATEST` result; lifecycle does not regress; latest host metrics and latest agent version come from T2 by observation time, not from the later-arriving T1 backfill; and duplicate replay creates no incident/event/notification side effect.

The current real-PG test is necessary infrastructure but only proves exact duplicate behavior for one heartbeat (`internal/center/store/sync_batches_postgres_integration_test.go:74-116`). SQL-mock assertions alone are insufficient for `GREATEST`, unique arbitration, row locking, and transaction ordering.

### RED 2 — deterministic incident stale-writer race on real PostgreSQL

Use barriers/test seams to force:

1. old evaluation reads revision N and builds mutation A;
2. new evaluation reads N and commits mutation B as revision N+1;
3. release A and assert typed projection conflict/`Applied=false`;
4. assert active incidents, every `last_evaluated_at`, object current health/count/summary, event count, and notification count/delivery correspond only to B.

Run for both monitoring instance and target because targets aggregate facts from multiple monitoring instances. Add a periodic-sweep-versus-sync variant. This is the decisive regression that the current incident-store mock tests do not cover.

### RED 3 — service-level duplicate disposition

Apply one new batch then its exact duplicate through `syncing.Service`; assert `AfterSuccessfulSync` is called once, not twice. Add suppressed paused/retired/archived dispositions and assert no sync-triggered incident processing.

### RED 4 — evaluator watermark and equal-time ordering

- previous incident evaluated at T2 plus recovery/escalation carrier at T1 must preserve the previous incident and emit no event/notification;
- equal T2 live + backfilled samples/probes must have the same result in both input permutations;
- backfill strictly older than latest live must remain retained but cannot start/recover/escalate current state;
- a genuinely newer live recovery remains allowed, proving that severity is not incorrectly forced monotone.

### RED 5 — retention completeness boundary

Within raw retention, apply T2 then T1 and reverse order, run retention, and compare the full daily aggregate (counts, averages, max/min, p95, backfilled counts). Add a separate explicit failing test for “aggregate exists, raw purged, very old late fact arrives, retention reruns”; either resolve it under this task through a replay/retention invariant or record it as a scoped follow-up with a fail-closed configuration check.

## Planning decision

No user choice is required for the primary correctness repair: durable object revision + CAS, duplicate post-sync suppression, and deterministic tie ordering are correctness requirements rather than product preferences.

One product choice should be raised only if implementation changes onboarding semantics: **does a backfilled host sample count as the first current-trust observation that promotes `pending_enrollment -> in_use`?** Recommended answer is “no; require a non-backfilled sample from the current epoch,” because replay receipt proves transport recovery, not current host freshness. If the product deliberately wants replay to count, add an explicit acceptance test showing that `last_heartbeat_at` may remain old while lifecycle becomes in-use.

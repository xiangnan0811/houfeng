# Research: Child 4 authoritative source map

- Query: identify the current source/store/API contracts that Child 4 must read before implementing evidence adapters.
- Scope: internal; no product behavior changed.
- Date: 2026-08-10

## Source map

| Evidence contract | Current authority | Boundary to preserve |
| --- | --- | --- |
| `ip_quality.report/v1` | `internal/center/ipquality/types.go`, `internal/center/store/ip_quality.go`, `internal/center/http/handlers/ip_quality.go`, migrations `0039`-`0042` and `0047` | report revision/provider rows are the source of truth; raw diagnostic JSON is retention-only and must never enter a canonical payload |
| `monitoring.host/v1` | `internal/center/runtimefacts/types.go`, `internal/center/store/runtime_facts.go`, `internal/center/store/observations.go`, host sample schema in `0001_initial_schema.sql` | query absolute `observed_at` windows from host samples; do not reuse the row-count/truncating sparkline response |
| `monitoring.probe/v2` | `internal/center/runtimefacts/types.go`, `internal/center/store/runtime_facts.go`, `internal/center/store/observations.go`, probe schema in `0001_initial_schema.sql` | preserve actual coverage, gaps, maintenance/backfill provenance and bounded precision; no zero-fill or extrapolation |
| `monitoring.event/v2` | `internal/center/incidents/service.go`, `internal/center/store/incidents.go`, `state_change_events` writes in `internal/center/store/targets.go` | persist event identity, observed/recorded time, correction/backfill provenance and resulting state; do not infer live semantics from deleted raw facts |
| `subscription.cost/v1` | `internal/center/subscriptioncosts/types.go`, `internal/center/subscriptioncosts/service.go`, `internal/center/store/subscription_costs.go`, migrations `0031`-`0034` and `0048` | snapshot original currency, base currency, rate/date/freshness, budget month and actual coverage; preserve billing-period and renewal-mode history |
| `command.audit/v1` | `internal/center/commandaudits/types.go`, `internal/center/store/command_audits.go`, migrations `0046` and `0050` | metadata-only (`command_id`, action/sensitivity/actor/outcome/time/events); `details`, stdout and stderr are forbidden even when source rows contain them |

## Asset history naming decision

The parent evidence contract (§12.2) closes the initial registry to the six
versioned keys above. It does not define an `asset.history/*` kind. Asset
history remains an authoritative activity/source family (renewal decisions,
price history, IP history and spec snapshots in
`internal/center/store/renewal_decisions.go`) and is covered by the activity
projection contract and the cost/history adapter slice. Adding a separate
registry kind would change the parent contract and must be a reviewed replan;
Child 4 must not invent it silently.

## Cross-layer constraints

- The backend database and record-authorization guides, the web state/data and quality guides, and the shared cross-layer/branch/code-reuse guides apply.
- `0054_create_record_evidence.sql`, its exact current APP ACL fragment and catalog/admission tests are one atomic migration delivery. Fresh and exact repeat are supported; old-database upgrade/backfill is out of scope.
- `cmd/houfeng-center/bootstrap.go` currently wires Records HTTP handlers with a nil `AdmissionGate` owned by Child 10. Evidence capture/save may be implemented and tested but must remain fail-closed in production until that gate is supplied.

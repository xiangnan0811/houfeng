# Houfeng Trend Degradation and Trend Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete V1 conservative trend degradation incidents and detail-page recent trend surfaces.

**Architecture:** Add explicit trend incident classes evaluated from current 24h raw observations against previous 7d daily aggregates. Extend existing runtime facts responses with bounded recent raw series for display, then render compact trend summaries with existing UI vocabulary and no new charting dependency.

**Tech Stack:** Go center service, pgx/PostgreSQL, React/Vite/TypeScript, Vitest/Testing Library.

---

## File structure

- Modify `internal/center/incidents/types.go`
  - Add `IncidentNodeTrendDegradation` and `IncidentTargetLatencyTrendDegradation`.
  - Add narrow aggregate structs used by evaluator/service.
- Modify `internal/center/incidents/evaluator.go`
  - Add `EvaluateNodeTrendDegradation`.
  - Add `EvaluateTargetLatencyTrendDegradationAcrossSeries`.
  - Add helpers for weighted baselines, current-window averages, degradation signals, and conservative recovery.
- Modify `internal/center/incidents/service.go`
  - Extend `SnapshotReader` with daily aggregate reads.
  - Query raw 24h current windows for trend classes without changing existing 30m/6h hard-failure windows.
  - Dispatch new class evaluations.
  - Implement Postgres aggregate queries.
- Modify `internal/center/incidents/evaluator_test.go`
  - Add failing tests for Node trend start/escalation/recovery/suppression.
  - Add failing tests for Target latency trend start/escalation/recovery/suppression.
- Modify `internal/center/incidents/service_test.go`
  - Add failing tests proving trend classes are included in node/target mutations.
- Modify `internal/center/runtimefacts/types.go`
  - Add `RecentHostSamples []HostSample`.
  - Add `RecentProbeObservations []ProbeObservation`.
- Modify `internal/center/store/runtime_facts.go`
  - Add bounded recent host/probe queries.
  - Preserve latest facts behavior.
- Modify `internal/center/store/runtime_facts_test.go`
  - Add failing tests for recent-series reads and bounded ordering.
- Modify `web/src/lib/types.ts`
  - Add `recent_host_samples` and `recent_probe_observations`.
- Modify `web/src/pages/NodeDetailPage.tsx`
  - Add compact `近期趋势` section.
- Modify `web/src/pages/TargetDetailPage.tsx`
  - Add compact `近期延迟趋势` section.
- Modify `web/src/pages/NodeDetailPage.test.tsx`
  - Add trend rendering and empty-state tests.
- Modify `web/src/pages/TargetDetailPage.test.tsx`
  - Add latency trend rendering and empty-state tests.
- Optionally modify `web/src/index.css`
  - Only if existing card spacing is insufficient; do not introduce a new visual system.

---

## Task 1: Backend trend evaluator

**Files:**
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/incidents/evaluator.go`
- Test: `internal/center/incidents/evaluator_test.go`

- [ ] **Step 1: Write failing Node trend evaluator tests**

Add tests that construct current 24h-ish `NodeResourceSample` slices and previous-day `NodeHostDailyAggregate` baselines:

```go
func TestEvaluateNodeTrendDegradationStartsConservatively(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	result := EvaluateNodeTrendDegradation(nil, "nd_001",
		nodeTrendSamples(now, []float64{1.9, 2.0, 2.1}, []float64{11, 12, 13}, []float64{1, 1, 1}),
		[]NodeHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.7, AvgCPUIOWaitPct: 2, AvgCPUStealPct: 0.5}},
	)
	if result.Current == nil || result.Current.IncidentClass != IncidentNodeTrendDegradation {
		t.Fatalf("Current = %#v, want node trend incident", result.Current)
	}
	if result.Current.Severity != SeverityAlert {
		t.Fatalf("Severity = %q, want %q", result.Current.Severity, SeverityAlert)
	}
	if result.Current.Severity == SeverityCritical {
		t.Fatal("trend degradation must not emit critical severity")
	}
}
```

- [ ] **Step 2: Run Node trend test and confirm RED**

Run: `go test ./internal/center/incidents -run TestEvaluateNodeTrendDegradationStartsConservatively -v`

Expected: compile failure because `EvaluateNodeTrendDegradation`, aggregate types, and class constants do not exist.

- [ ] **Step 3: Write failing Target trend evaluator tests**

Add tests that construct successful `runtimefacts.ProbeObservation` records with latency and `TargetProbeDailyAggregate` baselines. Cover single-series `关注`, multi-node or multi-probe `告警`, no `严重`, suppression, and recovery.

- [ ] **Step 4: Run Target trend tests and confirm RED**

Run: `go test ./internal/center/incidents -run 'TestEvaluateTargetLatencyTrend' -v`

Expected: compile failure because target trend evaluator and aggregate types do not exist.

- [ ] **Step 5: Add incident classes and aggregate structs**

Add exact incident classes:

```go
IncidentNodeTrendDegradation          IncidentClass = "node_trend_degradation"
IncidentTargetLatencyTrendDegradation IncidentClass = "target_latency_trend_degradation"
```

Add structs with only needed fields:

```go
type NodeHostDailyAggregate struct {
	BucketDate             time.Time
	SampleCount            int
	AvgLoad5               float64
	AvgCPUIOWaitPct        float64
	AvgCPUStealPct         float64
	BackfilledSampleCount  int
	MaintenanceSampleCount int
}

type TargetProbeDailyAggregate struct {
	TargetID                    string
	ProbeItemID                 string
	BucketDate                  time.Time
	ObservationCount            int
	SuccessCount                int
	AvgLatencyMS                *float64
	P95LatencyMS                *float64
	BackfilledObservationCount  int
	MaintenanceObservationCount int
}
```

- [ ] **Step 6: Implement minimal evaluator logic**

Implement explicit helpers only. Use conservative guards:

- Node current data must have at least 3 usable samples and a non-zero time span.
- Baseline must have at least one usable aggregate day.
- Node metric degradation requires both a relative increase and an absolute floor.
- Target current data must have at least 3 successful latency observations for a ProbeItem.
- Target latency degradation requires current average latency to be at least baseline × 1.8 and at least baseline + 100ms or at least 250ms.
- Severity is capped at `SeverityAlert`.

- [ ] **Step 7: Run evaluator tests and confirm GREEN**

Run: `go test ./internal/center/incidents -run 'TrendDegradation|TargetLatencyTrend' -v`

Expected: PASS.

- [ ] **Step 8: Commit evaluator task**

Commit with Lore protocol. Include `Tested: go test ./internal/center/incidents -run 'TrendDegradation|TargetLatencyTrend' -v`.

---

## Task 2: Backend service and aggregate query dispatch

**Files:**
- Modify: `internal/center/incidents/service.go`
- Test: `internal/center/incidents/service_test.go`

- [ ] **Step 1: Write failing service dispatch tests**

Add Node service test where fake snapshot reader returns:

- active incidents empty
- latest host samples for existing resource checks
- trend host samples for last 24h
- node aggregate baseline

Assert mutation includes `IncidentNodeTrendDegradation`.

Add Target service test where fake snapshot reader returns:

- target observations for failure/TLS window
- target observations for trend current window
- target aggregate baseline

Assert mutation includes `IncidentTargetLatencyTrendDegradation`.

- [ ] **Step 2: Run service trend tests and confirm RED**

Run: `go test ./internal/center/incidents -run 'Trend.*Service|Service.*Trend|Evaluate.*Trend' -v`

Expected: failure because service does not dispatch trend classes and snapshot reader lacks aggregate methods.

- [ ] **Step 3: Extend `SnapshotReader`**

Add:

```go
ListNodeHostDailyAggregates(context.Context, string, time.Time, time.Time) ([]NodeHostDailyAggregate, error)
ListTargetProbeDailyAggregates(context.Context, string, time.Time, time.Time) ([]TargetProbeDailyAggregate, error)
```

- [ ] **Step 4: Dispatch Node trend evaluation**

In `evaluateNode`, keep existing 30m sample read for disk/inode/resource. Add a separate 24h sample read and 7d aggregate read for `IncidentNodeTrendDegradation`.

- [ ] **Step 5: Dispatch Target latency trend evaluation**

In `evaluateTarget`, keep existing 6h observation read for probe failure/TLS. Add a separate 24h observation read and 7d aggregate read for `IncidentTargetLatencyTrendDegradation`.

- [ ] **Step 6: Implement Postgres aggregate queries**

Read from:

- `node_host_sample_daily_aggregates`
- `target_probe_daily_aggregates`

Use previous complete days: `bucket_date >= $2::date and bucket_date < $3::date`.

- [ ] **Step 7: Run service tests and confirm GREEN**

Run: `go test ./internal/center/incidents -v`

Expected: PASS.

- [ ] **Step 8: Commit service task**

Commit with Lore protocol. Include `Tested: go test ./internal/center/incidents -v`.

---

## Task 3: Runtime facts recent-series contract

**Files:**
- Modify: `internal/center/runtimefacts/types.go`
- Modify: `internal/center/store/runtime_facts.go`
- Test: `internal/center/store/runtime_facts_test.go`

- [ ] **Step 1: Write failing store tests**

Add tests proving:

- `GetNodeRuntimeFacts` returns `RecentHostSamples` ordered newest-first and bounded by SQL `limit`.
- `GetTargetRuntimeFacts` returns `RecentProbeObservations` ordered newest-first and bounded by SQL `limit`.
- Existing latest fields still populate.

- [ ] **Step 2: Run store tests and confirm RED**

Run: `go test ./internal/center/store -run RuntimeFacts -v`

Expected: compile/test failure because recent-series fields and queries do not exist.

- [ ] **Step 3: Add runtime facts fields**

Add JSON fields:

```go
RecentHostSamples []HostSample `json:"recent_host_samples"`
RecentProbeObservations []ProbeObservation `json:"recent_probe_observations"`
```

- [ ] **Step 4: Add bounded recent SQL queries**

Use last 24h and explicit caps:

- Host samples: `order by observed_at desc, id desc limit 288`
- Probe observations: `order by observed_at desc, id desc limit 500`

- [ ] **Step 5: Populate recent fields**

Initialize slices to empty, not nil, for stable JSON arrays.

- [ ] **Step 6: Run store and handler tests**

Run:

```bash
go test ./internal/center/store -run RuntimeFacts -v
go test ./internal/center/http/handlers -run RuntimeFacts -v
```

Expected: PASS.

- [ ] **Step 7: Commit runtime facts task**

Commit with Lore protocol. Include the two test commands in `Tested:`.

---

## Task 4: Frontend trend rendering

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Test: `web/src/pages/NodeDetailPage.test.tsx`
- Test: `web/src/pages/TargetDetailPage.test.tsx`
- Optional: `web/src/index.css`

- [ ] **Step 1: Write failing Node detail tests**

Add test data with `recent_host_samples`. Assert the page renders:

- `近期趋势`
- `近 24h 样本`
- `Load5 平均`
- `iowait 平均`
- `steal 平均`

Add empty-state test for no recent samples.

- [ ] **Step 2: Write failing Target detail tests**

Add test data with `recent_probe_observations`. Assert the page renders:

- `近期延迟趋势`
- ProbeItem-level latency average
- observation count
- empty state for no latency samples

- [ ] **Step 3: Run frontend tests and confirm RED**

Run: `cd web && npm test -- --run NodeDetailPage TargetDetailPage`

Expected: tests fail because the sections do not exist.

- [ ] **Step 4: Extend TypeScript types**

Add:

```ts
recent_host_samples: HostSample[]
recent_probe_observations: ProbeObservation[]
```

- [ ] **Step 5: Add Node trend helpers and section**

Compute summaries from `runtimeFacts.recent_host_samples`:

- count
- newest/oldest observed time
- average `load_5`
- average `cpu_iowait_pct`
- average `cpu_steal_pct`
- latest values

Render with existing `DetailSection` and `metric-card`.

- [ ] **Step 6: Add Target latency trend helpers and section**

Filter successful observations with non-null `latency_ms`, group by `probe_item_id`, compute:

- observation count
- distinct node count
- average latency
- max latency
- newest observed time

Render with existing `DetailSection` and `probe-card`.

- [ ] **Step 7: Run frontend tests and build**

Run:

```bash
cd web && npm test -- --run NodeDetailPage TargetDetailPage
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 8: Commit frontend task**

Commit with Lore protocol. Include frontend test/build commands in `Tested:`.

---

## Task 5: Integration review and verification

**Files:**
- All files changed in Tasks 1-4.

- [ ] **Step 1: Run focused backend verification**

Run:

```bash
go test ./internal/center/incidents -v
go test ./internal/center/store -run RuntimeFacts -v
go test ./internal/center/http/handlers -run RuntimeFacts -v
```

Expected: PASS.

- [ ] **Step 2: Run focused frontend verification**

Run:

```bash
cd web && npm test -- --run NodeDetailPage TargetDetailPage
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 3: Run full verification**

Run:

```bash
go test ./...
./scripts/verify.sh
```

Expected: PASS.

- [ ] **Step 4: Request final code review**

Dispatch review with:

- Requirements: this plan and `docs/superpowers/specs/2026-04-28-houfeng-trend-degradation-surfaces-design.md`.
- Diff range: base before Task 1 through current HEAD.
- Review focus: V1 scope control, conservative trend severity, recovery flapping, runtime facts contract, and detail-page rendering.

- [ ] **Step 5: Fix any Critical/Important review findings**

Run the affected focused tests after fixes.

- [ ] **Step 6: Final status**

Report changed files, verification evidence, and any remaining V1 Phase 6 follow-up.

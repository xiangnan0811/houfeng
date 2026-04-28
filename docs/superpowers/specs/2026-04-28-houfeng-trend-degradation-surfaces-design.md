# Houfeng V1 Trend Degradation and Trend Surfaces Design

## Context

This spec closes Phase 5 from `docs/superpowers/specs/2026-04-28-houfeng-v1-completion-sequencing-design.md`.

Frozen V1 requires:

- A third incident family: conservative trend degradation for Node and Target.
- Node trend signals: `load_5`, `cpu_iowait_pct`, and `cpu_steal_pct`.
- Target trend signal: latency rising versus a recent baseline, preferably seen from multiple execution perspectives.
- Detail pages that expose recent trend context.
- No generic rule engine, arbitrary expressions, or per-object unrestricted alert strategy editor.

Current implementation already has raw host/probe observations, active incident transitions, daily aggregate tables, retention aggregation, and detail pages that show only latest runtime facts.

## Framing options considered

### Option A — raw short-window only

Compare the latest few samples to earlier samples in the same raw query window.

- Pros: small implementation.
- Cons: weak alignment with frozen baseline because V1 explicitly calls for a 24h window versus a recent 7d baseline.

### Option B — current raw 24h versus existing 7d daily aggregates

Use raw unsuppressed current-window observations for the last 24h and compare them with the existing daily aggregate tables for previous complete days.

- Pros: matches the frozen baseline, reuses Phase 3 retention/aggregation work, stays explicit and non-generic.
- Cons: trend incidents require enough current data and enough aggregate baseline, so early installs may show trend surfaces before trend incidents can be evaluated.

### Option C — introduce a generic rule/evaluation model now

Create reusable rule definitions and attach them to objects or labels.

- Pros: extensible.
- Cons: directly violates V1 non-goals and would reopen product design.

## Selected design

Use Option B for incident evaluation and a narrow raw recent-series extension for detail-page display.

The system will add explicit incident classes rather than a rule engine:

- `node_trend_degradation`
- `target_latency_trend_degradation`

Trend incidents are conservative:

- They never produce `严重` by themselves.
- A single clearly degraded trend starts at `关注`.
- Multiple corroborating degraded signals or multiple perspectives may escalate to `告警`.
- Recovery requires enough recent safe evidence; one improved sample is not enough.
- Maintenance and backfilled data do not start noisy trend incidents.

## Backend incident semantics

### Node trend degradation

Inputs:

- Current window: raw host samples from the last 24h.
- Baseline window: daily host-sample aggregates for previous complete days in the last 7d.
- Metrics: `load_5`, `cpu_iowait_pct`, `cpu_steal_pct`.

Detection:

- Ignore samples marked `maintenance_context` or `is_backfilled`.
- Require enough current-window coverage and enough baseline aggregate samples.
- Treat a metric as degraded only when both absolute and relative movement are meaningful, avoiding tiny baseline noise.
- `关注`: one metric is clearly above its recent baseline.
- `告警`: two or more metrics are degraded.
- Never emit `严重`.

Recovery:

- If a previous trend incident exists, recover only after a sufficient recent window is back near baseline or below safe absolute levels.
- If current data or baseline is insufficient, preserve the previous incident as-is rather than flapping.

### Target latency trend degradation

Inputs:

- Current window: successful probe observations with latency from the last 24h.
- Baseline window: daily target probe aggregates for previous complete days in the last 7d.
- Grouping: evaluate at ProbeItem level while tracking distinct execution node perspectives from current observations.

Detection:

- Ignore failed probe observations for latency trend detection; hard failures are already handled by `target_probe_failure`.
- Ignore maintenance/backfilled observations.
- Require enough successful latency observations and baseline observations.
- A ProbeItem is degraded when current average latency is materially above its recent baseline with both absolute and relative guards.
- `关注`: one ProbeItem/perspective is degraded.
- `告警`: multiple ProbeItems or multiple execution nodes show degraded latency.
- Never emit `严重`.

Recovery:

- A previous target latency trend incident recovers only when enough current successful latency data exists and no ProbeItem remains materially degraded.

## Runtime facts and detail-page trend surfaces

Extend existing runtime facts contracts instead of adding new endpoints:

- `NodeRuntimeFacts.recent_host_samples`
- `TargetRuntimeFacts.recent_probe_observations`

These fields carry bounded raw recent-series data for detail pages. They are intentionally display data, not a public rule/config surface.

Frontend rendering:

- Node detail adds a `近期趋势` section after `当前主机指标`.
- Target detail adds a `近期延迟趋势` section before the ProbeItem list.
- Use existing `DetailSection`, `metric-card`, `probe-card`, and badge vocabulary.
- No new charting dependency.
- Show compact summaries: sample/observation counts, recent window, latest value, average value, min/max where useful, and empty states when insufficient data exists.

## File boundaries

Expected backend changes:

- `internal/center/incidents/types.go`: incident classes and aggregate structs.
- `internal/center/incidents/evaluator.go`: explicit trend evaluators.
- `internal/center/incidents/service.go`: current 24h reads, baseline aggregate reads, and evaluation dispatch.
- `internal/center/runtimefacts/types.go`: recent-series response fields.
- `internal/center/store/runtime_facts.go`: bounded recent raw series queries.

Expected frontend changes:

- `web/src/lib/types.ts`: recent-series fields.
- `web/src/pages/NodeDetailPage.tsx`: Node trend summary.
- `web/src/pages/TargetDetailPage.tsx`: Target latency trend summary.

## Non-goals

- No generic rule engine.
- No per-object rule editor.
- No arbitrary expression language.
- No new notification channel.
- No charting library.
- No visual redesign away from the Unified / Baseline vocabulary.

## Verification strategy

- Evaluator tests prove Node and Target trend detection, conservative severity, suppression, and recovery.
- Service tests prove the new classes are dispatched and existing classes still coexist.
- Store tests prove runtime facts return latest facts plus bounded recent series.
- Frontend tests prove detail pages render trend summaries and empty states.
- Full verification remains `go test ./...`, `./scripts/verify.sh`, and `cd web && npm run build`.

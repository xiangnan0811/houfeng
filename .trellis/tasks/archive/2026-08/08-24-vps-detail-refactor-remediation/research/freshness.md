# Research: I-03 VPS overview independent freshness and retry ownership

- Query: Propose a concrete independent source-budget implementation, relation SectionState/API compatibility, truthful renewal observation timestamps, Web presentation/retry ownership, and exact RED tests for audit finding I-03.
- Scope: internal
- Date: 2026-08-24

## Findings

### Authority and reproduced defect

The active parent contract is explicit:

- identity remains fatal; monitoring, IP, subscription, services, domains, and activity are independently bounded inside one request budget (.trellis/tasks/08-24-vps-detail-refactor-remediation/prd.md:47-51);
- monitoring, IP, renewal, recent activity, and every relation carry ready|stale|unavailable plus observed_at, last_success_at, and a safe reason (.trellis/tasks/08-24-vps-detail-refactor-remediation/prd.md:52-57);
- next_renew_at is a business deadline, never freshness, and every success/freshness timestamp is at or before generated_at (.trellis/tasks/08-24-vps-detail-refactor-remediation/prd.md:54-55);
- the acceptance proof is slow-monitoring isolation, five local-source failure classes, relation zero-versus-unavailable distinction, and the timestamp invariant (.trellis/tasks/08-24-vps-detail-refactor-remediation/prd.md:96-99).

This is also the archived authority: the overview service is meant to invoke bounded readers concurrently, each degradable source returns SectionState, and the endpoint has both total and per-section budgets (.trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/design.md:121-143,159-172). The completion audit identified the same concrete breaks in final-audit-report.md:109-154.

Current code violates that contract in four linked places:

1. internal/center/vpsoverview/service.go:139-145 gives the entire non-activity LoadSources call one timeout.
2. internal/center/store/vps_overview.go:95-109 then performs monitoring, IP, renewal, and relations sequentially on that same context. A slow monitoring call can therefore consume every later source's execution opportunity.
3. internal/center/vpsoverview/types.go:123-130 has no relation SectionState; service/domain errors are flattened to count=0 plus status=unavailable at internal/center/store/vps_overview.go:278-295.
4. internal/center/store/vps_overview.go:251-254 copies future RenewAt into ObservedAt and LastSuccessAt. subscriptions.Record already exposes source-owned UpdatedAt at internal/center/subscriptions/types.go:63-96.

The Web receives section metadata but drops it in summary rendering (web/src/pages/vps-detail/VPSOverviewSummaryGrid.tsx:18-27), does not carry it at all for relations (web/src/lib/types.ts:3360-3366), and only gives activity/monitoring a shared bottom retry (web/src/pages/vps-detail/VPSOverviewPageView.tsx:85-94).

### Files found

- internal/center/vpsoverview/service.go — current two-goroutine aggregator; one bundled non-activity timeout and one activity timeout.
- internal/center/vpsoverview/types.go — wire model, SectionState, 800 ms current default, and relation shape without a section.
- internal/center/vpsoverview/service_test.go — only bundled-source fake and activity timeout coverage; no sibling isolation.
- internal/center/store/vps_overview.go — sequential source repository, renewal timestamp bug, and relation error-to-zero collapse.
- internal/center/store/vps_overview_test.go — healthy fixture has a future renewal date but does not assert freshness.
- internal/center/subscriptions/types.go — renewal records have RenewAt, CreatedAt, and UpdatedAt.
- internal/center/assetservices/types.go — service rows have UpdatedAt (lines 36-49).
- internal/center/assetdomains/types.go — domain rows have UpdatedAt (lines 31-47).
- internal/center/http/handlers/vps_overview.go and vps_overview_test.go — direct JSON transport boundary and the natural wire-contract assertion.
- web/src/lib/types.ts — handwritten overview types; section state is currently an unrestricted string and relation has no section.
- web/src/lib/recordsApi.ts — permissive overview projection; relations are passed through untouched at lines 577-579.
- web/src/pages/vps-detail/VPSOverviewSummaryGrid.tsx — source-state presentation missing.
- web/src/pages/vps-detail/VPSOverviewRecentActivity.tsx — an unavailable empty activity is currently presented as authoritative “暂无最近活动”.
- web/src/pages/vps-detail/VPSOverviewRelations.tsx — unavailable count is rendered as a real zero and the whole card may be a Link.
- web/src/pages/vps-detail/VPSOverviewPageView.tsx — owns composition but not local freshness.
- web/src/pages/vps-detail/hooks/useVPSOverview.ts — one full-overview refresh owner; it already retains a seeded overview while a refresh is loading or fails.
- web/src/pages/VPSDetailPage.tsx:130-169 — keeps the overview mounted when status=loading and an overview is present, so local retry need not blank the page.
- web/e2e/fixtures/profiles.ts:611-683 — canonical overview fixture; relation fixture must gain section.
- web/e2e/page-states.spec.ts:268-309 — current production-build overview state tests.
- .trellis/tasks/08-24-vps-detail-refactor-remediation/research/overview-gate.md — sibling I-02 decoder research; its relation contract at line 110 currently omits the I-03 section and must be reconciled before implementation.

### Recommended backend contract

#### 1. Split the bundled reader at the service boundary

Keep one repository object, but replace SourceReader.LoadSources with methods whose calls can be independently timed:

~~~go
type SourceReader interface {
    LoadIdentity(context.Context, string) (IdentitySource, error)
    LoadMonitoring(context.Context, string) (MonitoringSource, error)
    LoadIPQuality(context.Context, string) (IPQualitySource, error)
    LoadRenewal(context.Context, string, vpsassets.RenewalDecision) (RenewalSource, error)
    LoadServiceRelation(context.Context, string) (RelationSummary, error)
    LoadDomainRelation(context.Context, string) (RelationSummary, error)
}

type IdentitySource struct {
    Identity Identity
    Facts    []Fact
}

type MonitoringSource struct {
    Section         SectionState
    Status          string
    Detail          string
    Health          string
    ActiveIncidents int
    RelationCount   int
}

type IPQualitySource struct {
    Section   SectionState
    Status    string
    RiskLevel string
    Stale     bool
}

type RenewalSource struct {
    Section             SectionState
    ActiveSubscriptions int
    NextRenewAt         *time.Time
    Status              string
}
~~~

The store methods return source errors; they must no longer convert errors to unavailable themselves. The service is the single owner of timeout/error-to-safe-SectionState mapping. Successful store results may still return stale (for example ip_quality_stale) because that state is source evidence, not transport policy.

Monitoring and subscription relation cards are composed from MonitoringSource and RenewalSource. This avoids duplicate queries. Services and domains each get a separate reader call and context. The response order remains deterministic regardless of completion order:

1. monitoring_instances;
2. subscriptions;
3. services;
4. domains.

No reader opens a per-row query, and the concurrent calls must not be wrapped in one shared transaction/connection. Query count remains the existing one-call-per-authority shape.

#### 2. Make total and per-source budgets explicit

Replace sectionBudget/activityBudget with a value object:

~~~go
type SourceBudgets struct {
    Total      time.Duration
    Identity   time.Duration
    Monitoring time.Duration
    IPQuality  time.Duration
    Renewal    time.Duration
    Services   time.Duration
    Domains    time.Duration
    Activity   time.Duration
}
~~~

Validate every duration as positive at construction. Keep NewService as the production constructor and add a config/test constructor; retain NewServiceWithClock as a compatibility wrapper if other tests depend on it.

For the first remediation, set every default including Total to the existing 800 ms. This preserves the current endpoint wall-clock ceiling instead of silently lengthening degraded requests. Separate fields are still important: they create independent child contexts and allow later tuning from measured PostgreSQL evidence. A child deadline is always capped by the remaining total deadline.

The archived full-scale result was already close to the SLO (overview aggregator p95 about 718 ms and HTTP p95 about 638 ms versus 750 ms; archived implement.md:358-369). Therefore do not choose a larger production timeout merely to make tests pass. Re-run the real PostgreSQL performance test after the concurrency change and report query count/error rate as well as p95.

#### 3. Orchestrate identity first, then all degradable readers concurrently

Recommended flow:

~~~text
validate request/actor
  -> totalCtx = context.WithTimeout(callerCtx, budgets.Total)
  -> LoadIdentity(child(totalCtx, budgets.Identity))
       not found/unauthorized/error => fatal; start no degradable readers
  -> concurrently launch monitoring, IP, renewal, services, domains, activity
       each gets its own child(totalCtx, its named budget)
       each writes one immutable result to a buffered result channel
  -> collect completed results until all arrive or totalCtx expires
       callerCtx cancellation => return caller error
       service total deadline => preserve completed results and mark only pending results unavailable
  -> generatedAt = now().UTC()
  -> validate/bound every SectionState against generatedAt
  -> compose deterministic summary/relations/anomalies and return 200 partial overview
~~~

Use a result channel buffered to the number of launched readers, keyed by a closed internal source enum. Do not have late goroutines mutate the assembled Overview. A buffered channel also prevents a completed late sender from blocking if the total collector has returned. Readers must honor context; the tests should prove this. The total collector remains the outer wall-clock guard if one reader misbehaves.

Identity must complete before degradable calls start. This preserves the fatal 404/unauthorized boundary and avoids doing activity/relationship work for a missing or unauthorized VPS. Once identity succeeds, no degradable error is allowed to become a top-level error.

Safe service-owned reason mapping should be closed and source-specific:

| Source | Deadline | Other error | Source-provided stale |
| --- | --- | --- | --- |
| monitoring | monitoring_timeout | monitoring_unavailable | n/a |
| IP quality | ip_quality_timeout | ip_quality_unavailable | ip_quality_stale |
| renewal | subscription_timeout | subscription_unavailable | n/a |
| services | relation_timeout | relation_unavailable | n/a |
| domains | relation_timeout | relation_unavailable | n/a |
| activity | activity_timeout | activity_unavailable, or activity_projection_unavailable for its known sentinel | activity-owned safe code |

Never include err.Error(), SQL text, endpoint names, cursor/checkpoint data, or worker timestamps in ReasonCode.

### Freshness authority by source

The fields are nullable because a successful empty source has no source observation. Do not invent a timestamp for absence, and do not reuse a global activity checkpoint.

| Surface | observed_at and last_success_at authority | Notes |
| --- | --- | --- |
| overall | generated_at for the derived request result | Equal to generated_at is valid; no local retry. |
| monitoring summary + monitoring relation | selected monitoring instance LastHeartbeatAt | Both surfaces reuse the exact same SectionState. Empty successful link set is ready with null times. |
| IP summary | ipquality.Summary.ObservedAt | Preserve source stale state/reason. |
| renewal summary + subscription relation | maximum non-zero subscriptions.Record.UpdatedAt across every returned row | Include inactive rows because the result also asserts active-count/absence. Never use RenewAt, EndsAt, TrialEndsAt, or NextReminderAt. |
| services relation | maximum non-zero assetservices.Record.UpdatedAt in the returned set | Successful empty set is ready with count=0 and null times. |
| domains relation | maximum non-zero assetdomains.Record.UpdatedAt in the returned set | Successful empty set is ready with count=0 and null times. |
| recent activity | activity Freshness.VisibleObservedAt | Archived privacy authority requires both overview timestamps to use only visible_observed_at; empty authorized scope is null. Never use global projector/checkpoint success. |

For renewal specifically, calculate the two facts independently:

~~~text
next_renew_at = earliest non-zero RenewAt among active subscriptions
observed_at = last_success_at = latest non-zero UpdatedAt among all returned subscriptions
~~~

The result can therefore truthfully contain a future next_renew_at while both freshness timestamps are in the past.

#### Enforce the generated_at invariant once at aggregation

Capture generated_at after collecting the bounded reads, immediately before response assembly. Then run one service helper over monitoring, IP, renewal, activity, and every relation:

- normalize non-null timestamps to UTC;
- if either timestamp is after generated_at, remove only the invalid timestamp;
- change ready to stale (leave unavailable unavailable);
- use safe reason source_timestamp_invalid unless an already-unavailable section has a more useful safe reason;
- never clamp a future timestamp to generated_at, because clamping fabricates an observation.

This makes the wire invariant mechanically true even under a bad source clock while preserving usable facts as stale. Add one table helper in tests that walks every returned section and asserts each non-null timestamp is <= GeneratedAt. NextRenewAt is deliberately excluded from that helper.

### Relation SectionState and API compatibility

Change the Go wire type additively:

~~~go
type RelationSummary struct {
    Kind    string
    Count   int
    Status  string
    Route   string
    Label   string
    Section SectionState
}
~~~

Keep the existing JSON tags and add json:"section" without omitempty. Keep Count as int and keep the existing fields. On a failed relation read, Count remains zero for wire compatibility and Status may remain unavailable for old clients, but the new Web must consult Section before presenting the count. Do not convert Count to nullable and do not add a second relations envelope.

The corresponding TypeScript shape is:

~~~ts
export type VPSOverviewSectionStateName = 'ready' | 'stale' | 'unavailable'

export type VPSOverviewSectionState = {
  state: VPSOverviewSectionStateName
  observed_at: string | null
  last_success_at: string | null
  reason_code: string
}

export type VPSOverviewRelation = {
  kind: string
  count: number
  status?: string
  route: string
  label: string
  section: VPSOverviewSectionState
}
~~~

This is an additive server response, so an old Web safely ignores section. For a rolling deployment, release backend first and Web second. A new Web against an old/invalid backend must fail visibly at the overview contract boundary; it must not synthesize ready and must not choose legacy. No new capability flag or API version is needed.

Cross-child requirement: the I-02 research currently lists relations without section at overview-gate.md:110. Its final decoder must require and project relation.section, validate state against the active PRD's three-value enum, validate both timestamps as RFC3339 strings or null, and ignore only unknown additive fields. Missing relation.section is invalid_shape. This update must land with I-03 or the strict decoder will either erase the new field or reject the final DTO.

### Web presentation and retry ownership

Add one route-private pure component, for example VPSOverviewFreshness, under web/src/pages/vps-detail/. Do not create a global design-system primitive until another page needs the exact contract.

Its inputs should be section, a user-facing source label, onRetry, and retrying. It renders:

- an explicit state label for all three states: “就绪”, “数据可能已过期”, or “暂不可用”;
- “观测” and “最近成功” using the existing Timestamp atom; null renders its existing em dash;
- a Chinese allowlisted explanation for known reason codes; an unknown code renders a generic “来源状态暂不可确认” and never the raw code;
- a local retry button only for stale/unavailable, with an accessible name such as “重试 IP 质量” rather than several indistinguishable “重试”.

Ownership by surface:

- VPSOverviewSummaryGrid accepts onRetry/retrying and renders freshness inside monitoring, IP quality, and renewal cells. Overall is derived and has no local retry.
- VPSOverviewRecentActivity accepts the activity section plus onRetry/retrying. unavailable + no items must say the source is unavailable, not “暂无最近活动”; stale items stay visible with the stale state.
- VPSOverviewRelations accepts onRetry/retrying and renders freshness per card. An unavailable card displays count as “—”, not 0. A retry button must be a sibling of any relation Link, never nested inside a Link.
- VPSOverviewPageView passes one refresh command to these local owners and removes the bottom shared note at lines 85-94 so status/retry is not duplicated.

Transport/revalidation remains owned by useVPSOverview.commands.refresh: every local retry reissues the one existing GET /api/vps/:id/overview. Do not invent source-specific endpoints or local cache authorities. The hook already keeps the previous overview while status=loading and on refresh failure (useVPSOverview.ts:75-106); VPSDetailPage keeps rendering it when overview is non-null (VPSDetailPage.tsx:130-169). Pass retrying = state.status === 'loading' to disable all local retry buttons and prevent duplicate concurrent refreshes.

If a full refresh itself fails, I-02 owns the bounded page/gate error classification. I-03 owns only a successful partial overview's local source states. This prevents two competing retry/error banners.

### Exact RED tests

Write these tests before implementation and confirm that they fail for the audited reason, not because fixtures are incomplete.

#### internal/center/vpsoverview/service_test.go

1. TestServiceGetSlowMonitoringDoesNotConsumeSiblingBudgets
   - granular fake identity returns immediately;
   - monitoring waits for ctx.Done and returns ctx.Err;
   - every sibling checks ctx.Err before returning a distinct ready payload;
   - assert overview succeeds; only monitoring summary and monitoring relation are unavailable with monitoring_timeout; IP, renewal, services, domains, and activity remain ready;
   - assert each source was called exactly once and the fast readers began before monitoring was cancelled.

2. TestServiceGetDegradesOnlyFailingSource
   - table cases: monitoring error, IP error, subscription error, service relation error, domain relation error, activity projection error;
   - assert no top-level error, the expected one section/card and reason change, all siblings retain their values/states, arrays remain non-null, and no raw sentinel text appears in marshaled JSON.

3. TestServiceGetTotalBudgetKeepsCompletedResults
   - identity and two sources complete; remaining sources wait on ctx;
   - assert the call returns a partial overview when Total expires, completed sections remain ready, pending sections are unavailable/timeouts, and every blocking fake observes cancellation.

4. TestServiceGetIdentityFailureIsFatalBeforeLocalReaders
   - identity returns ErrVPSNotFound and separately an arbitrary identity error;
   - assert the error is fatal and all six degradable call counts remain zero.

5. TestServiceGetCallerCancellationIsFatal
   - cancel caller context after identity starts;
   - assert caller cancellation is returned, rather than a synthetic 200 partial response.

6. TestServiceGetBoundsEveryFreshnessTimestampAtGeneratedAt
   - each fake returns future observed/last-success times and renewal also returns a legitimate future NextRenewAt;
   - assert the table walk over summary, recent activity, and every relation finds no freshness time after GeneratedAt; affected ready sections become stale with source_timestamp_invalid; NextRenewAt remains future.

7. TestServiceGetBuildsRelationsInStableOrderAndReusesSourceReads
   - complete readers in reverse order;
   - assert fixed relation order, monitoring/subscription cards share their source SectionState, and monitoring/subscription readers were not called a second time.

Use channel barriers plus context cancellation; do not assert millisecond elapsed ranges. A short test budget may drive DeadlineExceeded, but correctness assertions should be call/state based.

#### internal/center/store/vps_overview_test.go

1. TestVPSOverviewRepositoryRenewalFreshnessUsesLatestUpdatedAtNotRenewAt
   - two active rows: RenewAt = generated+5d/+10d; UpdatedAt = generated-2h/-1h;
   - assert NextRenewAt is +5d; ObservedAt and LastSuccessAt are -1h; neither equals a renewal deadline.

2. TestVPSOverviewRepositoryRenewalEmptyIsReadyWithoutInventedTime
   - empty successful list;
   - assert ready, count=0, next renewal nil, observed/last-success nil.

3. TestVPSOverviewRepositoryRelationFreshnessUsesLatestUpdatedAt
   - service/domain rows with distinct UpdatedAt values;
   - assert each card's own maximum timestamp, count, kind, and state.

4. TestVPSOverviewRepositoryRelationEmptyVersusError
   - successful empty services/domains return ready count=0 and null timestamps;
   - source error is returned to the service and is not flattened to an ordinary zero in the repository.

5. TestVPSOverviewRepositoryGranularReadersUseOneBoundedQueryEach
   - spy exact VPS IDs/renewal filters and call counts;
   - assert no per-row/N+1 or monitoring/subscription duplicate relation query.

#### internal/center/vpsoverview/types_test.go and internal/center/http/handlers/vps_overview_test.go

1. TestOverviewMarshalRelationIncludesSectionState
   - marshal one relation and assert section.state, observed_at, last_success_at, reason_code plus every legacy relation field.
2. Extend TestVPSOverviewHandlerReturnsPayload with a relation section and decode the JSON path; assert the additive field is present and top-level collections remain arrays.

#### web/src/lib/recordsApi.test.ts

Coordinate with I-02's strict decoder tests:

1. a valid relation section is projected and unknown additive fields are stripped;
2. missing/null relation.section, invalid state, non-string reason, and invalid timestamp each reject with InvalidVPSOverviewResponseError('invalid_shape');
3. a valid future business deadline elsewhere does not affect section decoding, while the backend invariant fixture keeps every freshness timestamp <= generated_at.

#### New route-private component tests

Add web/src/pages/vps-detail/VPSOverviewFreshness.test.tsx:

1. table ready/stale/unavailable labels and both Timestamp fields;
2. retry absent for ready and present for stale/unavailable;
3. each click calls onRetry once and disabled retry does not call;
4. known reason maps to bounded Chinese text; unknown/raw sensitive-looking reason is never rendered.

Add web/src/pages/vps-detail/VPSOverviewRelations.test.tsx:

1. ready zero renders 0; unavailable zero renders “—” plus local state/reason/retry;
2. one failed card leaves a ready sibling unchanged;
3. retry is outside the relation link and its accessible name identifies the relation.

Add web/src/pages/vps-detail/VPSOverviewRecentActivity.test.tsx:

1. ready empty renders the true empty state;
2. unavailable empty renders unavailable/retry and not “暂无最近活动”;
3. stale non-empty preserves rows and shows stale freshness.

#### web/src/pages/vps-detail/VPSOverviewPageView.test.tsx

1. table monitoring/IP/renewal/activity/service-relation/domain-relation degraded fixtures;
2. assert the degraded source has exactly one locally named retry, unaffected sources still render ready values, and the old global “部分区段暂不可用” note is absent;
3. click the local retry and assert onRefresh once;
4. assert every displayed freshness timestamp/reason is attached to its own section/card.

#### web/src/pages/vps-detail/hooks/useVPSOverview.test.tsx

1. seeded refresh with a deferred response keeps the prior overview while state.status is loading;
2. a failed refresh retains the prior overview and resolves false;
3. the page disables local retry while refreshing so two source buttons cannot create duplicate user-triggered requests.

#### web/e2e/fixtures/profiles.ts and web/e2e/page-states.spec.ts

Add relation.section to the canonical fixture. Add one partial-overview production profile containing stale IP, unavailable renewal, unavailable service relation, and unavailable activity:

- each local state and named retry is visible;
- renewal/service unavailable are not presented as zero/empty;
- clicking “重试续费” increments only the existing overview request count;
- the page remains mounted during the request;
- desktop and 390 px have no document horizontal overflow, Axe critical/serious is zero, and keyboard focus reaches each retry.

### Focused RED/GREEN and verification commands

Use exact Go 1.26.2 and repository-pinned Node 22.

~~~bash
go test ./internal/center/vpsoverview ./internal/center/store ./internal/center/http/handlers -run 'Overview|VPSOverview' -count=1
go test -race ./internal/center/vpsoverview ./internal/center/store -run 'Overview|VPSOverview' -count=10

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  NODE_ENV=test npm --prefix web run test -- --run \
  src/lib/recordsApi.test.ts \
  src/pages/VPSDetailPage.test.tsx \
  src/pages/vps-detail/VPSOverviewFreshness.test.tsx \
  src/pages/vps-detail/VPSOverviewRelations.test.tsx \
  src/pages/vps-detail/VPSOverviewRecentActivity.test.tsx \
  src/pages/vps-detail/VPSOverviewPageView.test.tsx \
  src/pages/vps-detail/hooks/useVPSOverview.test.tsx

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  npm --prefix web run test:e2e -- page-states.spec.ts accessibility.spec.ts visual-contracts.spec.ts
~~~

Before claiming completion, also run the strict PostgreSQL test named TestPostgresIntegrationVPSOverviewPerformance in both store and handler packages with the required DSN/fixture (never convert missing infrastructure to a skip), then make verify-go, Node 22 make verify-web, and the full Chromium suite npm --prefix web run test:e2e.

### Related specs

- .trellis/spec/backend/quality-guidelines.md — context propagation, package ownership, deterministic tests, real PostgreSQL verification, and no false-green infrastructure.
- .trellis/spec/backend/error-handling.md — stable safe errors at transport boundaries and no internal-error leakage.
- .trellis/spec/web/state-and-data.md — API façade ownership, handwritten Go/TypeScript alignment, retained state during refresh, and loading/error/data tests.
- .trellis/spec/web/component-conventions.md — controlled components, accessible actions, existing atoms, and interaction ownership.
- .trellis/spec/web/quality-guidelines.md — Node 22, Vitest/RTL user-visible assertions, production Chromium, Axe, 390 px, and complete Web gates.
- .trellis/spec/guides/cross-layer-thinking-guide.md — trace authority through backend DTO, façade, hook, component, and browser tests.
- .trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/prd.md:27-31,47-49 — original overview state/failure/performance acceptance authority.
- .trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/design.md:121-143,159-172 — original bounded aggregation, privacy-safe activity freshness, local retry, and total/per-section budget design.

### External references

No external library or web reference is needed. Go 1.26.2 context/time semantics and the repository's existing React 19.2/Vitest 4.1/Playwright 1.61 stack are sufficient; the design deliberately adds no dependency.

## Caveats / Not Found

- The exact production budget values have no separate config authority beyond the existing 800 ms constant. Preserving 800 ms for both total and named child defaults is the least-surprise starting point, but the real PostgreSQL p95/query-count gate must decide whether later tuning is justified.
- There is no persisted per-source overview cache. Therefore an unavailable response may truthfully have last_success_at=null; implementation must not invent a previous success. The Web may retain the prior whole overview while a refresh is in flight/fails, but that is not a backend freshness authority.
- I-01 owns whether a relation has a real destination and whether it is a Link; I-03 only requires that retry is local and never nested inside another interactive element.
- I-02 owns success-body decoding and gate error classification. Its current research relation shape omits section; this is the critical cross-child conflict to resolve before either child freezes tests.
- Capturing generated_at after bounded reads plus fail-closed timestamp validation is required for the active parent's <= generated_at acceptance criterion; merely replacing RenewAt with UpdatedAt is insufficient.
- No product, test, spec, task-planning, Git, or external state was changed by this research; only this research artifact was written.

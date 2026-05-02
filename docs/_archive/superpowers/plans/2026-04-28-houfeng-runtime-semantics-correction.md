# Houfeng Runtime Semantics Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make V1 runtime controls and exposed settings truthful before later reliability and UI acceptance phases.

**Architecture:** Keep this phase narrow. Center sync-plan generation becomes aware of Node lifecycle/runtime state, the agent contract carries host-sample maintenance context, notification delivery honors persisted notification reason flags, and V1 default frequency semantics are corrected without introducing a rules engine.

**Tech Stack:** Go, PostgreSQL-facing store tests with existing fakes, net/http handlers, React/Vite/TypeScript, Vitest, Testing Library

---

## Scope

This plan implements Phase 1 from `docs/superpowers/specs/2026-04-28-houfeng-v1-completion-sequencing-design.md`.

In scope:

- Node `暂停`, `维护中`, and `已退役` semantics in sync-plan generation.
- Host sample maintenance context propagation from center plan to agent submissions.
- Persisted notification reason flags for started/escalated/recovered notifications.
- V1 TLS default frequency correction to `6h`.
- Frontend copy/defaults that match the corrected runtime behavior.

Out of scope:

- Agent durable queue, local buffering, retry, and backfill.
- Retention workers and aggregation.
- Dashboard abnormal object summaries.
- Events advanced filters.
- Trend degradation.
- Visual QA.

## Planned file structure

### Center plan/domain contract

- Modify: `internal/center/nodes/types.go`
  - Export Node monitoring status constants for plan logic.
- Modify: `internal/center/agentplan/types.go`
  - Add host-sample maintenance context to center sync-plan DTO.
- Modify: `internal/contracts/agentapi/types.go`
  - Add host-sample maintenance context to wire contract.
- Modify: `internal/center/store/agent_plan.go`
  - Read Node lifecycle/runtime state and apply V1 semantics.
- Modify: `internal/center/store/agent_plan_test.go`
  - Lock pause, retired, and maintenance plan behavior.
- Modify: `internal/contracts/agentapi/types_sync_plan_test.go`
  - Lock JSON round-trip for host-sample maintenance context.
- Modify: `internal/center/http/handlers/agent.go`
  - Copy the new plan field into API response.
- Modify: `internal/center/http/handlers/agent_test.go`
  - Lock handler response mapping.

### Agent runtime

- Modify: `agent/runtime/runtime.go`
  - Attach host-sample maintenance context to collected HostSample payloads.
- Modify: `agent/runtime/runtime_test.go`
  - Lock maintenance context propagation and plan cloning.

### Incident notification settings

- Modify: `internal/center/incidents/service.go`
  - Honor persisted notification reason flags before sending.
- Modify: `internal/center/incidents/service_test.go`
  - Lock started/escalated/recovered suppression by settings.

### V1 frequency defaults and copy

- Modify: `internal/center/settings/types.go`
  - Change default TLS probe frequency to `6h`.
- Modify: `internal/center/settings/types_test.go`
  - Lock deterministic default shape.
- Modify: `internal/center/http/handlers/settings_test.go`
  - Update expected settings payloads that rely on defaults.
- Modify: `web/src/pages/SettingsPage.tsx`
  - Make global defaults copy truthful after notification flags are operative.
- Modify: `web/src/pages/SettingsPage.test.tsx`
  - Update copy expectations and default TLS fixture where needed.
- Modify: `web/src/pages/TargetDetailPage.tsx`
  - Use V1 kind-specific create defaults: TCP/HTTP `5m`, TLS `6h`.
- Modify: `web/src/pages/TargetDetailPage.test.tsx`
  - Lock TLS create default.

---

## Task 1: Center sync-plan Node runtime semantics

**Files:**
- Modify: `internal/center/nodes/types.go`
- Modify: `internal/center/agentplan/types.go`
- Modify: `internal/center/store/agent_plan.go`
- Modify: `internal/center/store/agent_plan_test.go`

- [x] **Step 1: Add failing sync-plan tests for paused and retired Nodes**

Append this test to `internal/center/store/agent_plan_test.go` near the other `TestBuildSyncPlan...` tests:

```go
func TestBuildSyncPlanSuppressesPausedAndRetiredNodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		lifecycleStatus  string
		monitoringStatus string
	}{
		{
			name:             "paused node",
			lifecycleStatus:  nodes.LifecycleInUse,
			monitoringStatus: "暂停",
		},
		{
			name:             "retired node",
			lifecycleStatus:  nodes.LifecycleRetired,
			monitoringStatus: nodes.MonitoringEnabled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			queryCalled := false
			repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
				queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
					if sql != selectAgentPlanNodeLabelsSQL {
						return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
					}
					if args[0] != "nd_001" {
						return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected node id") }}
					}
					return fakeAgentPlanRow{scan: func(dest ...any) error {
						*(dest[0].(*[]string)) = []string{"edge"}
						if len(dest) == 4 {
							*(dest[1].(*string)) = agentapi.FrequencyTier5m
							*(dest[2].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
								NodeLabels:   []centersettings.NodeLabelOverrideRule{},
								TargetTypes:  []centersettings.TargetTypeOverrideRule{},
								TargetLabels: []centersettings.TargetLabelOverrideRule{},
							})
							*(dest[3].(*bool)) = true
							return nil
						}
						*(dest[1].(*string)) = tt.lifecycleStatus
						*(dest[2].(*string)) = tt.monitoringStatus
						*(dest[3].(*string)) = agentapi.FrequencyTier5m
						*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
							NodeLabels:   []centersettings.NodeLabelOverrideRule{},
							TargetTypes:  []centersettings.TargetTypeOverrideRule{},
							TargetLabels: []centersettings.TargetLabelOverrideRule{},
						})
						*(dest[5].(*bool)) = true
						return nil
					}}
				},
				query: func(context.Context, string, ...any) (pgx.Rows, error) {
					queryCalled = true
					return &fakeAgentPlanRows{}, nil
				},
			}}

			plan, err := repo.BuildSyncPlan(context.Background(), "nd_001")
			if err != nil {
				t.Fatalf("BuildSyncPlan() error = %v", err)
			}
			if plan.HostSampleFrequencyTier != "" {
				t.Fatalf("HostSampleFrequencyTier = %q, want empty disabled tier", plan.HostSampleFrequencyTier)
			}
			if plan.HostSampleMaintenanceContext {
				t.Fatal("HostSampleMaintenanceContext = true, want false for suppressed plan")
			}
			if len(plan.ProbeAssignments) != 0 {
				t.Fatalf("len(ProbeAssignments) = %d, want 0", len(plan.ProbeAssignments))
			}
			if queryCalled {
				t.Fatal("assignment query was called for a suppressed node")
			}
		})
	}
}
```

- [x] **Step 2: Add failing sync-plan test for Node maintenance context**

Append this test to `internal/center/store/agent_plan_test.go`:

```go
func TestBuildSyncPlanMarksNodeMaintenanceContext(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanNodeLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			if args[0] != "nd_maint" {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected node id") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge"}
				if len(dest) == 4 {
					*(dest[1].(*string)) = agentapi.FrequencyTier5m
					*(dest[2].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
						NodeLabels:   []centersettings.NodeLabelOverrideRule{},
						TargetTypes:  []centersettings.TargetTypeOverrideRule{},
						TargetLabels: []centersettings.TargetLabelOverrideRule{},
					})
					*(dest[3].(*bool)) = true
					return nil
				}
				*(dest[1].(*string)) = nodes.LifecycleInUse
				*(dest[2].(*string)) = nodes.MonitoringMaintenance
				*(dest[3].(*string)) = agentapi.FrequencyTier5m
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
					NodeLabels:   []centersettings.NodeLabelOverrideRule{},
					TargetTypes:  []centersettings.TargetTypeOverrideRule{},
					TargetLabels: []centersettings.TargetLabelOverrideRule{},
				})
				*(dest[5].(*bool)) = true
				return nil
			}}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != selectAgentPlanAssignmentsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeAgentPlanRows{rows: []fakeAgentPlanScan{
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_enabled"
					*(dest[1].(*string)) = "api.example.test"
					port := 443
					*(dest[2].(**int)) = &port
					*(dest[3].(*string)) = targets.RunStatusEnabled
					*(dest[4].(*string)) = "pb_http"
					*(dest[5].(*string)) = agentapi.ProbeKindHTTP
					*(dest[6].(*string)) = agentapi.FrequencyTier5m
					*(dest[7].(*int)) = 5
					*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
					if len(dest) > 9 {
						*(dest[9].(*string)) = targets.TargetTypeService
					}
					if len(dest) > 10 {
						*(dest[10].(*[]string)) = []string{"api"}
					}
					return nil
				}},
			}}, nil
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "nd_maint")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier5m)
	}
	if !plan.HostSampleMaintenanceContext {
		t.Fatal("HostSampleMaintenanceContext = false, want true")
	}
	if len(plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(plan.ProbeAssignments))
	}
	if !plan.ProbeAssignments[0].MaintenanceContext {
		t.Fatal("ProbeAssignments[0].MaintenanceContext = false, want true from node maintenance")
	}
}
```

- [x] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/center/store -run 'TestBuildSyncPlan(SuppressesPausedAndRetiredNodes|MarksNodeMaintenanceContext)' -v
```

Expected: FAIL. Acceptable failure forms are compile failure for `HostSampleMaintenanceContext` / `nodes.MonitoringMaintenance`, or assertion failure showing paused/retired nodes still receive a normal plan.

- [x] **Step 4: Export Node monitoring status constants**

In `internal/center/nodes/types.go`, replace the current monitoring constant block:

```go
	MonitoringEnabled          = "启用"
	BindingUnbound             = "未绑定"
```

with:

```go
	MonitoringEnabled     = "启用"
	MonitoringMaintenance = "维护中"
	MonitoringPaused      = "暂停"

	BindingUnbound             = "未绑定"
```

- [x] **Step 5: Add host-sample maintenance field to center plan type**

In `internal/center/agentplan/types.go`, replace `SyncPlan` with:

```go
type SyncPlan struct {
	HostSampleFrequencyTier       string
	HostSampleMaintenanceContext bool
	ProbeAssignments              []ProbeAssignment
}
```

- [x] **Step 6: Make agent-plan SQL read Node lifecycle/runtime state**

In `internal/center/store/agent_plan.go`, replace `selectAgentPlanNodeLabelsSQL` with:

```go
const selectAgentPlanNodeLabelsSQL = `
	select n.labels,
		n.lifecycle_status,
		n.monitoring_status,
		coalesce(nullif(cs.host_sample_frequency_tier, ''), '5m') as host_sample_frequency_tier,
		coalesce(
			cs.override_rules,
			'{"node_labels":[],"target_types":[],"target_labels":[]}'::jsonb
		) as override_rules,
		cs.settings_id is not null as settings_row_present
	from nodes n
	left join center_settings cs on cs.settings_id = $2
	where n.node_id = $1`
```

- [x] **Step 7: Apply pause, maintenance, and retired semantics in plan construction**

In `internal/center/store/agent_plan.go`, update the local variables and scan in `buildSyncPlan` to include lifecycle/runtime state:

```go
	var (
		labels             []string
		lifecycleStatus    string
		monitoringStatus   string
		hostSampleTier     string
		overrideRulesJSON  []byte
		settingsRowPresent bool
	)
	if err := queryer.QueryRow(ctx, selectAgentPlanNodeLabelsSQL, nodeID, centersettings.SingletonID).Scan(&labels, &lifecycleStatus, &monitoringStatus, &hostSampleTier, &overrideRulesJSON, &settingsRowPresent); errors.Is(err, pgx.ErrNoRows) {
		return agentplan.SyncPlan{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("query labels for node %q: %w", nodeID, err)
	}
```

Then insert this helper code near `resolveAgentPlanSettings`:

```go
func nodeSuppressesObservation(lifecycleStatus, monitoringStatus string) bool {
	return lifecycleStatus == nodes.LifecycleRetired || monitoringStatus == nodes.MonitoringPaused
}

func nodeInMaintenance(monitoringStatus string) bool {
	return monitoringStatus == nodes.MonitoringMaintenance
}
```

After resolving settings and before creating the normal plan, insert:

```go
	if nodeSuppressesObservation(lifecycleStatus, monitoringStatus) {
		return agentplan.SyncPlan{
			HostSampleFrequencyTier:       "",
			HostSampleMaintenanceContext: false,
			ProbeAssignments:              make([]agentplan.ProbeAssignment, 0),
		}, nil
	}

	nodeMaintenance := nodeInMaintenance(monitoringStatus)
```

Replace the plan initialization with:

```go
	plan := agentplan.SyncPlan{
		HostSampleFrequencyTier:       resolveHostSampleFrequencyTier(settings.HostSampleFrequencyTier, labels, settings.OverrideRules),
		HostSampleMaintenanceContext: nodeMaintenance,
		ProbeAssignments:              make([]agentplan.ProbeAssignment, 0),
	}
```

Replace assignment maintenance calculation with:

```go
		assignment.MaintenanceContext = nodeMaintenance || runStatus == targets.RunStatusMaintenance
```

- [x] **Step 8: Run focused sync-plan tests**

Run:

```bash
go test ./internal/center/store -run 'TestBuildSyncPlan(SuppressesPausedAndRetiredNodes|MarksNodeMaintenanceContext|UsesPersistedSettings|AppliesSettingsOverrides|ReturnsAssignmentsWhenSettingsRowMissing|ReturnsDefaultCadenceAndNoAssignmentsForLabelLessNode)' -v
```

Expected: PASS.

- [x] **Step 9: Commit center sync-plan semantics**

Run:

```bash
git add internal/center/nodes/types.go internal/center/agentplan/types.go internal/center/store/agent_plan.go internal/center/store/agent_plan_test.go
git commit -m "Make Node runtime state shape sync plans" -m "Node pause and retirement must stop observation assignments, while maintenance must keep collection but mark observations as maintenance-context facts.\n\nConstraint: Frozen V1 defines Node runtime state separately from health state\nConfidence: high\nScope-risk: moderate\nTested: go test ./internal/center/store -run 'TestBuildSyncPlan(SuppressesPausedAndRetiredNodes|MarksNodeMaintenanceContext|UsesPersistedSettings|AppliesSettingsOverrides|ReturnsAssignmentsWhenSettingsRowMissing|ReturnsDefaultCadenceAndNoAssignmentsForLabelLessNode)' -v"
```

---

## Task 2: Agent API and runtime host-sample maintenance context

**Files:**
- Modify: `internal/contracts/agentapi/types.go`
- Modify: `internal/contracts/agentapi/types_sync_plan_test.go`
- Modify: `internal/center/http/handlers/agent.go`
- Modify: `internal/center/http/handlers/agent_test.go`
- Modify: `agent/runtime/runtime.go`
- Modify: `agent/runtime/runtime_test.go`

- [x] **Step 1: Add failing contract and handler assertions**

In `internal/contracts/agentapi/types_sync_plan_test.go`, update `TestSyncPlan` by setting:

```go
	plan := agentapi.SyncPlan{
		HostSampleFrequencyTier:       agentapi.FrequencyTier15m,
		HostSampleMaintenanceContext: true,
		ProbeAssignments: []agentapi.ProbeAssignment{{
```

After the `target_base_port` assertion, add:

```go
	if maintenance, exists := got["host_sample_maintenance_context"]; !exists || maintenance != true {
		t.Fatalf("host_sample_maintenance_context = %#v, exists=%v, want true", maintenance, exists)
	}
```

In `internal/center/http/handlers/agent_test.go`, update `TestAgentSyncHandlerReturnsAcceptedAt` by setting the fake plan:

```go
			Plan: agentplan.SyncPlan{
				HostSampleFrequencyTier:       agentapi.FrequencyTier5m,
				HostSampleMaintenanceContext: true,
```

After the host frequency assertion, add:

```go
	if !body.Plan.HostSampleMaintenanceContext {
		t.Fatal("HostSampleMaintenanceContext = false, want true")
	}
```

- [x] **Step 2: Add failing agent runtime assertion**

In `agent/runtime/runtime_test.go`, update `TestRuntimeUpdatesPlanAndAttachesDueHostSampleAndProbeObservations` by setting the first response plan:

```go
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier:       agentapi.FrequencyTier1m,
					HostSampleMaintenanceContext: true,
```

After the second sync host metadata assertion, add:

```go
	if !secondSync.HostSamples[0].MaintenanceContext {
		t.Fatal("HostSamples[0].MaintenanceContext = false, want true from sync plan")
	}
```

- [x] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/contracts/agentapi -run TestSyncPlan -v
go test ./internal/center/http/handlers -run TestAgentSyncHandlerReturnsAcceptedAt -v
go test ./agent/runtime -run TestRuntimeUpdatesPlanAndAttachesDueHostSampleAndProbeObservations -v
```

Expected: FAIL due missing `HostSampleMaintenanceContext` field or missing propagation.

- [x] **Step 4: Add the wire contract field**

In `internal/contracts/agentapi/types.go`, replace `SyncPlan` with:

```go
type SyncPlan struct {
	HostSampleFrequencyTier       string            `json:"host_sample_frequency_tier"`
	HostSampleMaintenanceContext bool              `json:"host_sample_maintenance_context"`
	ProbeAssignments              []ProbeAssignment `json:"probe_assignments,omitempty"`
}
```

- [x] **Step 5: Map center plan field into API response**

In `internal/center/http/handlers/agent.go`, replace the return block in `syncPlanToAPI` with:

```go
	return &agentapi.SyncPlan{
		HostSampleFrequencyTier:       plan.HostSampleFrequencyTier,
		HostSampleMaintenanceContext: plan.HostSampleMaintenanceContext,
		ProbeAssignments:              assignments,
	}
```

- [x] **Step 6: Propagate host-sample maintenance context in agent runtime**

In `agent/runtime/runtime.go`, update `collectHostSample` after metadata assignment:

```go
	sample.ObservedAt = observedAt
	sample.AgentVersion = agentVersion
	sample.Fingerprint = fingerprint
	sample.SyncBatchID = syncBatchID
	sample.MaintenanceContext = r.currentPlan.HostSampleMaintenanceContext
	r.lastHostSampleAt = observedAt
	return &sample
```

Update `cloneSyncPlan` so it copies the new field. The replacement should preserve existing assignment cloning:

```go
func cloneSyncPlan(plan *agentapi.SyncPlan) *agentapi.SyncPlan {
	if plan == nil {
		return nil
	}
	clone := &agentapi.SyncPlan{
		HostSampleFrequencyTier:       plan.HostSampleFrequencyTier,
		HostSampleMaintenanceContext: plan.HostSampleMaintenanceContext,
		ProbeAssignments:              make([]agentapi.ProbeAssignment, 0, len(plan.ProbeAssignments)),
	}
	for _, assignment := range plan.ProbeAssignments {
		copied := assignment
		copied.Config = append([]byte(nil), assignment.Config...)
		clone.ProbeAssignments = append(clone.ProbeAssignments, copied)
	}
	return clone
}
```

- [x] **Step 7: Run focused contract/runtime tests**

Run:

```bash
go test ./internal/contracts/agentapi -run 'TestSync(ResponseRoundTripWithPlan|Plan)' -v
go test ./internal/center/http/handlers -run TestAgentSyncHandlerReturnsAcceptedAt -v
go test ./agent/runtime -run 'TestRuntime(UpdatesPlanAndAttachesDueHostSampleAndProbeObservations|ReplacesCurrentPlanWithExplicitEmptyPlan)' -v
```

Expected: PASS.

- [x] **Step 8: Commit API/runtime propagation**

Run:

```bash
git add internal/contracts/agentapi/types.go internal/contracts/agentapi/types_sync_plan_test.go internal/center/http/handlers/agent.go internal/center/http/handlers/agent_test.go agent/runtime/runtime.go agent/runtime/runtime_test.go
git commit -m "Carry Node maintenance context into host samples" -m "Host samples need the same maintenance-context truthfulness as probe observations so Node maintenance can suppress interpretation without stopping collection.\n\nConstraint: Node maintenance continues collection, while Node pause stops collection\nConfidence: high\nScope-risk: moderate\nTested: go test ./internal/contracts/agentapi -run 'TestSync(ResponseRoundTripWithPlan|Plan)' -v\nTested: go test ./internal/center/http/handlers -run TestAgentSyncHandlerReturnsAcceptedAt -v\nTested: go test ./agent/runtime -run 'TestRuntime(UpdatesPlanAndAttachesDueHostSampleAndProbeObservations|ReplacesCurrentPlanWithExplicitEmptyPlan)' -v"
```

---

## Task 3: Persisted notification reason flags

**Files:**
- Modify: `internal/center/incidents/service.go`
- Modify: `internal/center/incidents/service_test.go`

- [x] **Step 1: Add failing table test for notification flag suppression**

Append this test to `internal/center/incidents/service_test.go` near other notification tests:

```go
func TestServiceNotificationFlagsSuppressConfiguredReasons(t *testing.T) {
	now := time.Date(2026, time.April, 28, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		reason     NotificationReason
		defaults   centersettings.IncidentDefaults
		transition EvaluationTransition
	}{
		{
			name:   "started disabled",
			reason: NotificationReasonStarted,
			defaults: centersettings.IncidentDefaults{
				NotifyOnStarted:   false,
				NotifyOnEscalated: true,
				NotifyOnRecovered: true,
			},
			transition: TransitionStarted,
		},
		{
			name:   "escalated disabled",
			reason: NotificationReasonEscalated,
			defaults: centersettings.IncidentDefaults{
				NotifyOnStarted:   true,
				NotifyOnEscalated: false,
				NotifyOnRecovered: true,
			},
			transition: TransitionEscalated,
		},
		{
			name:   "recovered disabled",
			reason: NotificationReasonRecovered,
			defaults: centersettings.IncidentDefaults{
				NotifyOnStarted:   true,
				NotifyOnEscalated: true,
				NotifyOnRecovered: false,
			},
			transition: TransitionRecovered,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tt.defaults.HeartbeatIntervalSeconds = 30
			tt.defaults.StaleThresholdIntervals = 3
			tt.defaults.SweepIntervalSeconds = 60
			settings := centersettings.Default()
			settings.IncidentDefaults = tt.defaults
			settingsRepo := &fakeSettingsRepository{
				getSettingsResult:         settings,
				persistedIncidentDefaults: tt.defaults,
				persistedIncidentExists:   true,
			}
			writer := &fakeMutationWriter{}
			notifier := &fakeNotifier{}
			service := NewSettingsBackedService(nil, nil, nil, writer, notifier, settingsRepo, slog.Default(), 30*time.Second, time.Minute)
			service.now = func() time.Time { return now }

			err := service.appendNotificationRecords(context.Background(), ObjectTypeNode, "nd_001", []classEvaluation{{
				class: IncidentNodeResourcePressure,
				result: EvaluationResult{
					Transition: tt.transition,
					Notification: &NotificationDecision{
						ShouldSend: true,
						Channel:    "telegram",
						Reason:     tt.reason,
						Severity:   SeverityAlert,
						Summary:    "suppressed by settings",
					},
				},
			}})
			if err != nil {
				t.Fatalf("appendNotificationRecords() error = %v", err)
			}
			if len(notifier.messages) != 0 {
				t.Fatalf("notifier.messages = %#v, want no sends", notifier.messages)
			}
			if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
				t.Fatalf("notifications = %#v, want one suppressed record", writer.notifications)
			}
			if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
				t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
			}
		})
	}
}
```

- [x] **Step 2: Add passing-control test for enabled reasons**

Append this test to `internal/center/incidents/service_test.go`:

```go
func TestServiceNotificationFlagsAllowEnabledReason(t *testing.T) {
	defaults := centersettings.Default().IncidentDefaults
	defaults.NotifyOnStarted = true
	settings := centersettings.Default()
	settings.IncidentDefaults = defaults
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: defaults,
		persistedIncidentExists:   true,
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewSettingsBackedService(nil, nil, nil, writer, notifier, settingsRepo, slog.Default(), 30*time.Second, time.Minute)

	err := service.appendNotificationRecords(context.Background(), ObjectTypeNode, "nd_001", []classEvaluation{{
		class: IncidentNodeResourcePressure,
		result: EvaluationResult{
			Transition: TransitionStarted,
			Notification: &NotificationDecision{
				ShouldSend: true,
				Channel:    "telegram",
				Reason:     NotificationReasonStarted,
				Severity:   SeverityAlert,
				Summary:    "send allowed",
			},
		},
	}})
	if err != nil {
		t.Fatalf("appendNotificationRecords() error = %v", err)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("notifier.messages = %#v, want one send", notifier.messages)
	}
	if len(writer.notifications) != 1 || writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("notifications = %#v, want sent record", writer.notifications)
	}
}
```

- [x] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/center/incidents -run 'TestServiceNotificationFlags' -v
```

Expected: FAIL because persisted notify flags are not consulted before delivery.

- [x] **Step 4: Add notification policy helpers**

In `internal/center/incidents/service.go`, add this type and helper near `incidentTiming`:

```go
type notificationPolicy struct {
	notifyOnStarted   bool
	notifyOnEscalated bool
	notifyOnRecovered bool
}

func defaultNotificationPolicy() notificationPolicy {
	defaults := centersettings.Default().IncidentDefaults
	return notificationPolicy{
		notifyOnStarted:   defaults.NotifyOnStarted,
		notifyOnEscalated: defaults.NotifyOnEscalated,
		notifyOnRecovered: defaults.NotifyOnRecovered,
	}
}

func notificationPolicyFromDefaults(defaults centersettings.IncidentDefaults) notificationPolicy {
	return notificationPolicy{
		notifyOnStarted:   defaults.NotifyOnStarted,
		notifyOnEscalated: defaults.NotifyOnEscalated,
		notifyOnRecovered: defaults.NotifyOnRecovered,
	}
}

func (p notificationPolicy) enabled(reason NotificationReason) bool {
	switch reason {
	case NotificationReasonStarted:
		return p.notifyOnStarted
	case NotificationReasonEscalated:
		return p.notifyOnEscalated
	case NotificationReasonRecovered:
		return p.notifyOnRecovered
	default:
		return true
	}
}
```

Add this method near `incidentTimingFor`:

```go
func (s *Service) notificationPolicyFor(ctx context.Context) notificationPolicy {
	policy := defaultNotificationPolicy()
	if s.settingsRepo == nil {
		return policy
	}
	if source, ok := s.settingsRepo.(persistedIncidentDefaultsSource); ok {
		defaults, exists, err := source.GetPersistedIncidentDefaults(ctx)
		if err != nil || !exists {
			return policy
		}
		return notificationPolicyFromDefaults(defaults)
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return policy
	}
	return notificationPolicyFromDefaults(settings.IncidentDefaults)
}
```

- [x] **Step 5: Apply notification policy before sending**

In `appendNotificationRecords`, before checking `ShouldSend`, assign:

```go
			shouldSend := evaluation.result.Notification.ShouldSend &&
				s.notificationPolicyFor(ctx).enabled(evaluation.result.Notification.Reason)
```

Then replace:

```go
			if evaluation.result.Notification.ShouldSend && s.notifier != nil {
```

with:

```go
			if shouldSend && s.notifier != nil {
```

The resulting control flow must still append a suppressed `NotificationRecordWrite` when `shouldSend` is false.

- [x] **Step 6: Run focused incident tests**

Run:

```bash
go test ./internal/center/incidents -run 'TestServiceNotificationFlags|TestSettingsAwareNotifier|TestSettingsBacked|TestServiceAfterSuccessfulSyncSuppressesNotificationsWithoutNotifier|TestServiceRecordsFailedNotificationDelivery' -v
```

Expected: PASS.

- [x] **Step 7: Commit notification flag integration**

Run:

```bash
git add internal/center/incidents/service.go internal/center/incidents/service_test.go
git commit -m "Honor persisted notification reason flags" -m "Settings expose started, escalated, and recovered notification controls, so delivery must respect those flags instead of treating them as stored-only data.\n\nConstraint: Suppressed notifications should still leave NotificationRecord evidence\nConfidence: high\nScope-risk: moderate\nTested: go test ./internal/center/incidents -run 'TestServiceNotificationFlags|TestSettingsAwareNotifier|TestSettingsBacked|TestServiceAfterSuccessfulSyncSuppressesNotificationsWithoutNotifier|TestServiceRecordsFailedNotificationDelivery' -v"
```

---

## Task 4: V1 TLS default frequency and truthful frontend defaults/copy

**Files:**
- Modify: `internal/center/settings/types.go`
- Modify: `internal/center/settings/types_test.go`
- Modify: `internal/center/http/handlers/settings_test.go`
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/pages/SettingsPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [x] **Step 1: Add failing settings default assertion for TLS `6h`**

In `internal/center/settings/types_test.go`, update `TestSettingsDefaultProvidesDeterministicSingletonShape` by adding this assertion after the existing HTTP default assertion:

```go
	if got.ProbeFrequencyDefaults.TLS != "6h" {
		t.Fatalf("ProbeFrequencyDefaults.TLS = %q, want %q", got.ProbeFrequencyDefaults.TLS, "6h")
	}
```

- [x] **Step 2: Add failing Target Detail TLS default test**

Append this test to `web/src/pages/TargetDetailPage.test.tsx` near the ProbeItem creation tests:

```tsx
  it('uses the V1 6h default when creating a TLS ProbeItem', async () => {
    fetchMock
      .mockResolvedValueOnce(mockJSONResponse(targetRecord({ target_id: 'tg_001', name: 'Blog', base_port: 443 })))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ latest_probe_observations: [] }))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          probeItemRecord({
            probe_item_id: 'pb_tls',
            target_id: 'tg_001',
            probe_kind: 'tls',
            frequency_tier: '6h',
            config: { port: 443, expiry_warning_days: 14 },
          }),
        ),
      )

    renderWithRoute('/targets/tg_001')

    await waitFor(() => expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '添加 ProbeItem' }))
    fireEvent.change(screen.getByLabelText('Probe 类型'), { target: { value: 'tls' } })

    expect(screen.getByLabelText('频率档位')).toHaveValue('6h')

    fireEvent.click(screen.getByRole('button', { name: '创建 ProbeItem' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/probe-items', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        probe_kind: 'tls',
        enabled: true,
        frequency_tier: '6h',
        timeout_seconds: 5,
        config: { port: 443, expiry_warning_days: 14 },
      }),
    })
  })
```

Use existing local helpers `targetRecord`, `probeItemRecord`, `renderWithRoute`, `mockJSONResponse`, `fetchMock`, `screen`, `waitFor`, and `fireEvent`.

- [x] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/center/settings -run TestSettingsDefaultProvidesDeterministicSingletonShape -v
cd web && npm run test -- TargetDetailPage.test.tsx --run -t 'uses the V1 6h default when creating a TLS ProbeItem'
```

Expected: FAIL because TLS currently defaults to `5m` in settings and the Target detail create form.

- [x] **Step 4: Change backend default TLS frequency**

In `internal/center/settings/types.go`, replace:

```go
				TLS:  targets.FrequencyTier5m,
```

with:

```go
				TLS:  targets.FrequencyTier6h,
```

Review `internal/center/http/handlers/settings_test.go` and keep explicit request payloads unchanged unless the assertion is about `centersettings.Default()`. Explicit user-submitted `tls` values remain valid even when they are `5m` or `15m`.

- [x] **Step 5: Add kind-specific ProbeItem create defaults**

In `web/src/pages/TargetDetailPage.tsx`, add this constant after `FREQUENCY_TIER_OPTIONS`:

```tsx
const DEFAULT_FREQUENCY_BY_PROBE_KIND: Record<ProbeKind, FrequencyTier> = {
  tcp: '5m',
  http: '5m',
  tls: '6h',
}
```

Add this helper near the existing form helpers:

```tsx
function probeCreateFormForKind(current: ProbeCreateFormState, probeKind: ProbeKind): ProbeCreateFormState {
  return {
    ...current,
    probeKind,
    frequencyTier: DEFAULT_FREQUENCY_BY_PROBE_KIND[probeKind],
  }
}
```

In the Probe type `<select>` `onChange`, replace the current field update:

```tsx
                        updateProbeCreateField(
                          'probeKind',
                          event.target.value as ProbeCreateFormState['probeKind'],
                        )
```

with:

```tsx
                        setProbeCreateForm((current) =>
                          probeCreateFormForKind(
                            current,
                            event.target.value as ProbeCreateFormState['probeKind'],
                          ),
                        )
```

Keep edit mode safe: when opening an existing ProbeItem, `formStateForProbeItem` already uses `probeItem.frequency_tier`, so this create-mode default must not override existing records.

- [x] **Step 6: Update Settings copy to reflect operative notification flags**

In `web/src/pages/SettingsPage.tsx`, replace the Global Defaults section intro:

```tsx
        <SectionIntro>当前仅 heartbeat/sweep 时间参数已接入实时异常判定链；其余默认项仍作为持久化策略保存。</SectionIntro>
```

with:

```tsx
        <SectionIntro>heartbeat/sweep 时间参数与通知时机开关已接入实时异常与通知链路。</SectionIntro>
```

Keep the Overrides copy unchanged because incident defaults inside override rules remain stored policy.

- [x] **Step 7: Update Settings page test copy expectation**

In `web/src/pages/SettingsPage.test.tsx`, replace the expected Global Defaults copy:

```tsx
      screen.getByText('当前仅 heartbeat/sweep 时间参数已接入实时异常判定链；其余默认项仍作为持久化策略保存。'),
```

with:

```tsx
      screen.getByText('heartbeat/sweep 时间参数与通知时机开关已接入实时异常与通知链路。'),
```

- [x] **Step 8: Run focused settings/frontend tests**

Run:

```bash
go test ./internal/center/settings -run TestSettingsDefaultProvidesDeterministicSingletonShape -v
go test ./internal/center/http/handlers -run 'TestSettingsHandler' -v
cd web && npm run test -- SettingsPage.test.tsx TargetDetailPage.test.tsx --run
```

Expected: PASS.

- [x] **Step 9: Commit defaults and copy corrections**

Run:

```bash
git add internal/center/settings/types.go internal/center/settings/types_test.go internal/center/http/handlers/settings_test.go web/src/pages/SettingsPage.tsx web/src/pages/SettingsPage.test.tsx web/src/pages/TargetDetailPage.tsx web/src/pages/TargetDetailPage.test.tsx
git commit -m "Align V1 frequency defaults and settings copy" -m "TLS checks are low-frequency V1 probes, and Settings now has operative notification timing flags. Defaults and copy should reflect those semantics instead of presenting misleading stored-only behavior.\n\nConstraint: Global probe defaults still do not rewrite existing ProbeItem rows\nConfidence: high\nScope-risk: narrow\nTested: go test ./internal/center/settings -run TestSettingsDefaultProvidesDeterministicSingletonShape -v\nTested: go test ./internal/center/http/handlers -run 'TestSettingsHandler' -v\nTested: cd web && npm run test -- SettingsPage.test.tsx TargetDetailPage.test.tsx --run"
```

---

## Task 5: Phase-level verification

**Files:**
- No planned source edits unless verification exposes a regression.

- [x] **Step 1: Run focused backend suites touched by this phase**

Run:

```bash
go test ./internal/center/store -run 'TestBuildSyncPlan' -v
go test ./internal/contracts/agentapi -v
go test ./internal/center/http/handlers -run 'Test(AgentSyncHandler|SettingsHandler)' -v
go test ./agent/runtime -v
go test ./internal/center/incidents -v
go test ./internal/center/settings -v
```

Expected: PASS.

- [x] **Step 2: Run full Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [x] **Step 3: Run focused web tests**

Run:

```bash
cd web && npm run test -- SettingsPage.test.tsx TargetDetailPage.test.tsx --run
```

Expected: PASS.

- [x] **Step 4: Run full web verification**

Run:

```bash
cd web && npm run test -- --run && npm run build && npm run lint
```

Expected: PASS. If `npm run lint` is not defined in `web/package.json`, record that exact absence and rely on `./scripts/verify.sh` for the repository-standard verification command.

- [x] **Step 5: Run repository verification**

Run:

```bash
./scripts/verify.sh
```

Expected: PASS.

- [x] **Step 6: Inspect git status**

Run:

```bash
git status --short --branch
```

Expected: clean `main` after all phase commits.

---

## Dependency and parallelization notes

Recommended task order:

1. Task 1 first. It defines center sync-plan semantics.
2. Task 2 second. It carries the new host-sample context through API and agent runtime.
3. Task 3 can run in parallel with Task 2 after Task 1 is committed because it only touches incident notification policy.
4. Task 4 can run in parallel with Task 3 because it touches settings defaults/copy and Target detail defaults.
5. Task 5 must run after all implementation tasks are merged.

Safe parallel split for `superpowers:subagent-driven-development`:

- Subagent A: Task 1 only.
- Subagent B: Task 3 only.
- Subagent C: Task 4 only.
- Task 2 should start after Task 1 because it depends on the new center plan field.
- Task 5 stays in the leader session after integration.

## Self-review checklist

- Spec coverage:
  - Node pause: Task 1 suppresses host/probe plan.
  - Node maintenance: Task 1 marks center plan; Task 2 propagates host-sample context.
  - Node retired: Task 1 suppresses active plan.
  - Notification flags: Task 3 makes global started/escalated/recovered flags operative.
  - TLS default `6h`: Task 4 changes backend default and Target detail create default.
  - UI/backend consistency: Task 2 and Task 4 align data contracts and copy.
- Out-of-scope coverage:
  - Agent durable buffer/backfill excluded.
  - Retention excluded.
  - Dashboard/Event/Trend/Visual work excluded.
- Verification coverage:
  - Focused tests for each changed subsystem.
  - Full Go tests.
  - Focused and full web tests/build.
  - Repository verification script.

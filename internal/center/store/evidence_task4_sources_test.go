package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
)

func TestMonitoringEventEvidenceIsReachableFromIncidentWriterPath(t *testing.T) {
	t.Parallel()

	eventAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	lastHeartbeatAt := eventAt.Add(-4 * time.Minute)
	evaluation := incidents.EvaluateMonitoringInstanceHeartbeatMissing(
		nil,
		"mi_writer_path",
		eventAt,
		&lastHeartbeatAt,
		time.Minute,
	)
	if evaluation.Current == nil || evaluation.Event == nil {
		t.Fatalf("heartbeat evaluation = %#v, want started incident and event", evaluation)
	}

	db := &monitoringEventWriterPathDB{}
	repository := &PostgresIncidentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return db, nil },
		query:   db.Query,
	}
	if err := repository.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeMonitoringInstance,
		ObjectID:   "mi_writer_path",
		Active:     []incidents.IncidentRecord{*evaluation.Current},
		Events:     []incidents.StateChangeEventRecord{*evaluation.Event},
	}); err != nil {
		t.Fatalf("ApplyIncidentMutation() error = %v", err)
	}

	capture, err := repository.LoadMonitoringEventEvidence(
		context.Background(),
		"monitoring_instance",
		"mi_writer_path",
		evidence.TimeWindow{Start: eventAt.Add(-time.Minute), End: eventAt.Add(time.Minute)},
	)
	if err != nil {
		t.Fatalf("LoadMonitoringEventEvidence() error = %v", err)
	}
	if capture.EventCount != 1 {
		t.Fatalf("capture.EventCount = %d, want 1 normally produced incident event", capture.EventCount)
	}
	event := capture.Events[0]
	if event.EventAt != eventAt || event.RecordedAt.Before(event.EventAt) || event.Backfilled || event.CorrectionOfEventID != "" {
		t.Fatalf("event chronology/backfill/correction = %#v, want explicit normal chronology", event)
	}
	if event.Provenance != "center" || event.ProducerVersion != "center-monitoring-events/v1" || event.RuleVersion != "incident-rules/v1" || event.PriorState != "normal" || event.ResultingState != "alert" {
		t.Fatalf("event producer contract = %#v, want center incident rule transition", event)
	}
}

func TestMonitoringEventEvidenceWriterPathRetainsBackfillCorrectionAndProvenance(t *testing.T) {
	t.Parallel()

	eventAt := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	db := &monitoringEventWriterPathDB{}
	repository := &PostgresIncidentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return db, nil },
		query:   db.Query,
	}
	if err := repository.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeMonitoringInstance,
		ObjectID:   "mi_writer_correction",
		Events: []incidents.StateChangeEventRecord{{
			IncidentID:          "inc_monitoring_instance_cpu",
			IncidentClass:       incidents.IncidentMonitoringInstanceResourcePressure,
			ObjectType:          incidents.ObjectTypeMonitoringInstance,
			ObjectID:            "mi_writer_correction",
			EventType:           incidents.EventCorrected,
			Severity:            incidents.SeverityCritical,
			Summary:             "CPU correction",
			CreatedAt:           eventAt,
			IsBackfilled:        true,
			Provenance:          incidents.MonitoringEventProvenanceManualCorrection,
			ProducerVersion:     incidents.MonitoringEventProducerVersion,
			RuleVersion:         incidents.MonitoringEventIncidentRuleVersion,
			PriorState:          "alert",
			ResultingState:      "critical",
			CorrectionOfEventID: "evt_0123456789abcdef",
		}},
	}); err != nil {
		t.Fatalf("ApplyIncidentMutation() error = %v", err)
	}

	capture, err := repository.LoadMonitoringEventEvidence(
		context.Background(),
		"monitoring_instance",
		"mi_writer_correction",
		evidence.TimeWindow{Start: eventAt.Add(-time.Minute), End: eventAt.Add(time.Minute)},
	)
	if err != nil {
		t.Fatalf("LoadMonitoringEventEvidence() error = %v", err)
	}
	if capture.EventCount != 1 {
		t.Fatalf("capture.EventCount = %d, want 1", capture.EventCount)
	}
	event := capture.Events[0]
	if !event.Backfilled || event.CorrectionOfEventID != "evt_0123456789abcdef" || event.Provenance != incidents.MonitoringEventProvenanceManualCorrection || event.PriorState != "alert" || event.ResultingState != "critical" || !event.RecordedAt.After(event.EventAt) {
		t.Fatalf("event = %#v, want retained backfill/correction/chronology/provenance", event)
	}
}

func TestMonitoringEventEvidenceIsReachableFromStateControlWriterPaths(t *testing.T) {
	t.Parallel()

	eventAt := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		sourceType     string
		sourceID       string
		wantRule       string
		wantPriorState string
		wantResult     string
		write          func(context.Context, pgx.Tx) error
	}{
		{
			name:       "monitoring instance binding",
			sourceType: "monitoring_instance", sourceID: "mi_writer_binding",
			wantRule: monitoringEventBindingRuleVersion, wantPriorState: monitoringinstances.BindingPendingConfirmation, wantResult: monitoringinstances.BindingBound,
			write: func(ctx context.Context, tx pgx.Tx) error {
				return insertMonitoringInstanceBindingEvent(ctx, tx, monitoringinstances.Record{MonitoringInstanceID: "mi_writer_binding", BindingStatus: monitoringinstances.BindingBound, UpdatedAt: eventAt}, incidents.EventMonitoringInstanceBindingRebindConfirmed, "binding confirmed", monitoringinstances.BindingPendingConfirmation, monitoringEventProvenanceWeb)
			},
		},
		{
			name:       "monitoring instance lifecycle",
			sourceType: "monitoring_instance", sourceID: "mi_writer_lifecycle",
			wantRule: monitoringEventLifecycleRuleVersion, wantPriorState: monitoringinstances.LifecycleInUse, wantResult: monitoringinstances.LifecycleRetired,
			write: func(ctx context.Context, tx pgx.Tx) error {
				return insertMonitoringInstanceLifecycleEvent(ctx, tx, monitoringinstances.Record{MonitoringInstanceID: "mi_writer_lifecycle", LifecycleStatus: monitoringinstances.LifecycleRetired, UpdatedAt: eventAt}, incidents.EventMonitoringInstanceRetired, "retired", "planned", monitoringinstances.LifecycleInUse, monitoringinstances.LifecycleRetired, monitoringEventProvenanceWeb)
			},
		},
		{
			name:       "monitoring instance runtime",
			sourceType: "monitoring_instance", sourceID: "mi_writer_runtime",
			wantRule: monitoringEventRuntimeRuleVersion, wantPriorState: monitoringinstances.MonitoringEnabled, wantResult: monitoringinstances.MonitoringMaintenance,
			write: func(ctx context.Context, tx pgx.Tx) error {
				return insertMonitoringInstanceRuntimeEvent(ctx, tx, monitoringinstances.Record{MonitoringInstanceID: "mi_writer_runtime", MonitoringStatus: monitoringinstances.MonitoringMaintenance, UpdatedAt: eventAt}, incidents.EventMonitoringInstanceMonitoringMaintenanceEntered, "maintenance", monitoringinstances.MonitoringEnabled, monitoringEventProvenanceWeb)
			},
		},
		{
			name:       "target runtime",
			sourceType: "target", sourceID: "tg_writer_runtime",
			wantRule: monitoringEventTargetRuleVersion, wantPriorState: targets.RunStatusEnabled, wantResult: targets.RunStatusMaintenance,
			write: func(ctx context.Context, tx pgx.Tx) error {
				return insertTargetRuntimeEvent(ctx, tx, targets.TargetRecord{TargetID: "tg_writer_runtime", RunStatus: targets.RunStatusMaintenance, UpdatedAt: eventAt}, incidents.EventTargetMaintenanceEntered, "maintenance", targets.RunStatusEnabled, monitoringEventProvenanceWeb)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := &monitoringEventWriterPathDB{}
			tx := &fakeMonitoringInstanceTx{exec: db.Exec}
			if err := tt.write(context.Background(), tx); err != nil {
				t.Fatalf("writer error = %v", err)
			}
			repository := &PostgresIncidentRepository{query: db.Query}
			capture, err := repository.LoadMonitoringEventEvidence(context.Background(), tt.sourceType, tt.sourceID, evidence.TimeWindow{Start: eventAt.Add(-time.Minute), End: eventAt.Add(time.Minute)})
			if err != nil {
				t.Fatalf("LoadMonitoringEventEvidence() error = %v", err)
			}
			if capture.EventCount != 1 {
				t.Fatalf("capture.EventCount = %d, want 1", capture.EventCount)
			}
			event := capture.Events[0]
			if event.EventAt != eventAt || event.RecordedAt != eventAt || event.Backfilled || event.CorrectionOfEventID != "" || event.Provenance != incidents.MonitoringEventProvenanceWeb || event.ProducerVersion != incidents.MonitoringEventProducerVersion || event.RuleVersion != tt.wantRule || event.PriorState != tt.wantPriorState || event.ResultingState != tt.wantResult {
				t.Fatalf("event = %#v, want complete state-control producer contract", event)
			}
		})
	}
}

type monitoringEventWriterPathDB struct {
	eventArgs []any
}

func (db *monitoringEventWriterPathDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(strings.ToLower(sql), "insert into state_change_events") {
		db.eventArgs = append([]any(nil), args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (db *monitoringEventWriterPathDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeRow{scan: func(dest ...any) error {
		*(dest[0].(*int)) = 1
		*(dest[1].(*string)) = string(incidents.SeverityAlert)
		*(dest[2].(*string)) = "heartbeat missing"
		return nil
	}}
}

func (db *monitoringEventWriterPathDB) Commit(context.Context) error   { return nil }
func (db *monitoringEventWriterPathDB) Rollback(context.Context) error { return nil }

func (db *monitoringEventWriterPathDB) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	if len(db.eventArgs) != 8 {
		return newEvidenceRows(), nil
	}
	payload, ok := db.eventArgs[6].([]byte)
	if !ok {
		return nil, fmt.Errorf("event payload type = %T, want []byte", db.eventArgs[6])
	}
	var values map[string]any
	if err := json.Unmarshal(payload, &values); err != nil {
		return nil, fmt.Errorf("decode writer event payload: %w", err)
	}
	required := []string{"event_at", "is_backfilled", "provenance", "producer_version", "rule_version", "prior_state", "resulting_state"}
	for _, field := range required {
		if _, exists := values[field]; !exists {
			return newEvidenceRows(), nil
		}
	}
	eventAt, err := time.Parse(time.RFC3339Nano, values["event_at"].(string))
	if err != nil {
		return nil, fmt.Errorf("parse writer event_at: %w", err)
	}
	windowStart, windowEnd := args[1].(time.Time), args[2].(time.Time)
	if eventAt.Before(windowStart) || !eventAt.Before(windowEnd) || db.eventArgs[1] != args[3] || db.eventArgs[2] != args[0] {
		return newEvidenceRows(), nil
	}
	return newEvidenceRows(func(dest ...any) error {
		if len(dest) != 20 {
			return fmt.Errorf("event scan destination count = %d, want 20", len(dest))
		}
		*(dest[0].(*string)) = db.eventArgs[0].(string)
		*(dest[1].(*string)) = db.eventArgs[1].(string)
		*(dest[2].(*string)) = db.eventArgs[2].(string)
		*(dest[3].(*string)) = db.eventArgs[3].(string)
		*(dest[4].(*string)) = db.eventArgs[4].(string)
		*(dest[5].(*string)) = db.eventArgs[5].(string)
		*(dest[6].(*string)) = eventAt.Format(time.RFC3339Nano)
		*(dest[7].(*time.Time)) = db.eventArgs[7].(time.Time)
		*(dest[8].(*bool)) = values["is_backfilled"].(bool)
		*(dest[9].(*string)) = values["provenance"].(string)
		*(dest[10].(*string)) = values["producer_version"].(string)
		*(dest[11].(*string)) = values["rule_version"].(string)
		*(dest[12].(*string)) = values["prior_state"].(string)
		*(dest[13].(*string)) = values["resulting_state"].(string)
		*(dest[14].(*string)) = stringValue(values["correction_of_event_id"])
		*(dest[15].(*string)) = ""
		*(dest[16].(*string)) = ""
		*(dest[17].(**float64)) = nil
		*(dest[18].(**float64)) = nil
		*(dest[19].(*bool)) = true
		return nil
	}), nil
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func TestTask4EvidenceSourceSQLUsesAllowlistedBoundedFacts(t *testing.T) {
	t.Parallel()
	for name, sql := range map[string]string{
		"events":   monitoringEventEvidenceSQL,
		"commands": commandAuditEvidenceSQL,
		"cost":     subscriptionCostEvidenceSQL + subscriptionBudgetEvidenceSQL,
		"renewals": assetRenewalEvidenceSQL,
		"prices":   assetPriceEvidenceSQL,
		"ips":      assetIPEvidenceSQL,
		"specs":    assetSpecEvidenceSQL,
	} {
		lower := strings.ToLower(sql)
		if !strings.Contains(lower, ">= $2") || !strings.Contains(lower, "< $3") {
			t.Fatalf("%s SQL lacks half-open authoritative window: %s", name, sql)
		}
		if name != "cost" && !strings.Contains(lower, "limit") {
			t.Fatalf("%s SQL lacks hard source bound: %s", name, sql)
		}
	}
	for _, forbidden := range []string{"fixer_api_key", "raw_json", "stdout", "stderr", "last_action"} {
		if strings.Contains(strings.ToLower(subscriptionCostEvidenceSQL+subscriptionBudgetEvidenceSQL+commandAuditEvidenceSQL), forbidden) {
			t.Fatalf("Task 4 evidence SQL exposes forbidden source field %q", forbidden)
		}
	}
	if strings.Contains(strings.ToLower(commandAuditEvidenceSQL), "details") {
		t.Fatal("command audit evidence SQL must never select details")
	}
	if !strings.Contains(strings.ToLower(subscriptionCostEvidenceSQL), "o.at) as source_watermark") {
		t.Fatal("subscription cost evidence SQL must bind its watermark to the observation transaction")
	}
	if !strings.Contains(strings.ToLower(subscriptionBudgetEvidenceSQL), "b.base_currency") {
		t.Fatal("subscription budget evidence SQL must return its authoritative currency")
	}
	if strings.Contains(strings.ToLower(subscriptionBudgetEvidenceSQL), "s.vps_id =") {
		t.Fatal("global monthly budget spend must not be scoped to the selected VPS")
	}
	for _, required := range []string{"event_at", "recorded_at", "is_backfilled", "correction_of_event_id", "producer_version", "rule_version", "prior_state", "resulting_state"} {
		if !strings.Contains(strings.ToLower(monitoringEventEvidenceSQL), required) {
			t.Fatalf("event evidence SQL lacks explicit %q", required)
		}
	}
}

func TestLoadMonitoringEventEvidenceScansExplicitProvenance(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	eventAt := start.Add(time.Hour)
	recordedAt := eventAt.Add(time.Minute)
	value, threshold := 95.0, 90.0
	db := &evidenceSourceQueryDB{queries: []evidenceRows{newEvidenceRows(func(dest ...any) error {
		if len(dest) != 20 {
			return errors.New("unexpected event evidence scan destination count")
		}
		*(dest[0].(*string)) = "evt_0123456789abcdef"
		*(dest[1].(*string)) = "monitoring_instance"
		*(dest[2].(*string)) = "mi_0123456789abcdef"
		*(dest[3].(*string)) = "incident_started"
		*(dest[4].(*string)) = "告警"
		*(dest[5].(*string)) = "CPU pressure"
		*(dest[6].(*string)) = eventAt.Format(time.RFC3339Nano)
		*(dest[7].(*time.Time)) = recordedAt
		*(dest[8].(*bool)) = true
		*(dest[9].(*string)) = "retention_backfill"
		*(dest[10].(*string)) = "agent/v3"
		*(dest[11].(*string)) = "incident-rules/v4"
		*(dest[12].(*string)) = "normal"
		*(dest[13].(*string)) = "alert"
		*(dest[14].(*string)) = ""
		*(dest[15].(*string)) = "cpu_usage_pct"
		*(dest[16].(*string)) = "percent"
		*(dest[17].(**float64)) = &value
		*(dest[18].(**float64)) = &threshold
		*(dest[19].(*bool)) = true
		return nil
	})}}
	repository := &PostgresIncidentRepository{query: db.Query}
	capture, err := repository.LoadMonitoringEventEvidence(context.Background(), "monitoring_instance", "mi_0123456789abcdef", evidence.TimeWindow{Start: start, End: start.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("LoadMonitoringEventEvidence() error = %v", err)
	}
	if capture.EventCount != 1 || capture.Events[0].EventAt != eventAt || capture.Events[0].RecordedAt != recordedAt || !capture.Events[0].Backfilled || capture.Events[0].RuleVersion != "incident-rules/v4" || len(capture.Events[0].Metrics) != 1 {
		t.Fatalf("capture = %#v, want explicit event facts", capture)
	}
}

func TestLoadMonitoringEventEvidenceRejectsPartialMetricMetadata(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	db := &evidenceSourceQueryDB{queries: []evidenceRows{newEvidenceRows(func(dest ...any) error {
		if len(dest) != 20 {
			return errors.New("unexpected event evidence scan destination count")
		}
		*(dest[0].(*string)) = "evt_0123456789abcdef"
		*(dest[1].(*string)) = "monitoring_instance"
		*(dest[2].(*string)) = "mi_0123456789abcdef"
		*(dest[3].(*string)) = "incident_started"
		*(dest[4].(*string)) = "告警"
		*(dest[5].(*string)) = "CPU pressure"
		*(dest[6].(*string)) = start.Add(time.Hour).Format(time.RFC3339Nano)
		*(dest[7].(*time.Time)) = start.Add(time.Hour + time.Minute)
		*(dest[8].(*bool)) = false
		*(dest[9].(*string)) = "agent_sync"
		*(dest[10].(*string)) = "agent/v3"
		*(dest[11].(*string)) = "incident-rules/v4"
		*(dest[12].(*string)) = "normal"
		*(dest[13].(*string)) = "alert"
		*(dest[14].(*string)) = ""
		*(dest[15].(*string)) = ""
		*(dest[16].(*string)) = "percent"
		*(dest[17].(**float64)) = nil
		*(dest[18].(**float64)) = nil
		*(dest[19].(*bool)) = true
		return nil
	})}}
	repository := &PostgresIncidentRepository{query: db.Query}
	_, err := repository.LoadMonitoringEventEvidence(context.Background(), "monitoring_instance", "mi_0123456789abcdef", evidence.TimeWindow{Start: start, End: start.Add(24 * time.Hour)})
	if err == nil || !strings.Contains(err.Error(), "metadata incomplete") {
		t.Fatalf("LoadMonitoringEventEvidence() error = %v, want partial metric metadata rejection", err)
	}
}

func TestLoadCommandAuditEvidenceNeverReadsDetails(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	exit := 0
	db := &evidenceSourceQueryDB{queries: []evidenceRows{newEvidenceRows(func(dest ...any) error {
		if len(dest) != 15 {
			return errors.New("unexpected command evidence scan destination count")
		}
		values := []string{"aud_01", "act_01", "mi_0123456789abcdef", "edge", "usr_0123456789abcdef01234567", "admin", "Admin", "uptime", "standard", "completed", "succeeded", "agent_sync"}
		for index, value := range values {
			*(dest[index].(*string)) = value
		}
		*(dest[12].(**int)) = &exit
		*(dest[13].(*time.Time)) = start.Add(time.Hour)
		*(dest[14].(*time.Time)) = start.Add(time.Hour + time.Minute)
		return nil
	})}}
	repository := &PostgresCommandAuditRepository{db: db}
	capture, err := repository.LoadCommandAuditEvidence(context.Background(), "mi_0123456789abcdef", evidence.TimeWindow{Start: start, End: start.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("LoadCommandAuditEvidence() error = %v", err)
	}
	if capture.AuditCount != 1 || capture.Audits[0].CommandID != "uptime" || capture.Audits[0].ExitCode == nil || *capture.Audits[0].ExitCode != 0 {
		t.Fatalf("capture = %#v, want metadata-only completed audit", capture)
	}
}

func TestLoadSubscriptionCostEvidenceFreezesSettingsRateBudgetAndCoverage(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	observed := start.Add(20 * 24 * time.Hour)
	queryIndex := 0
	db := fakeSubscriptionCostDB{query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
		queryIndex++
		if queryIndex == 1 {
			return newEvidenceRows(func(dest ...any) error {
				if len(dest) != 23 {
					return errors.New("unexpected subscription evidence scan destination count")
				}
				*(dest[0].(*int)) = 1
				*(dest[1].(*string)) = "sub_0123456789abcdef"
				*(dest[2].(*string)) = "vps_0123456789abcdef"
				*(dest[3].(*string)) = "sub_0123456789abcdef/rate_01/2026-07"
				*(dest[4].(*float64)) = 20
				*(dest[5].(*string)) = "USD"
				*(dest[6].(*string)) = "month"
				*(dest[7].(*int)) = 1
				*(dest[8].(*float64)) = 7.2
				*(dest[9].(*string)) = "frankfurter"
				*(dest[10].(*time.Time)) = start.Add(17 * 24 * time.Hour)
				*(dest[11].(*time.Time)) = observed.Add(-2 * time.Hour)
				*(dest[12].(*bool)) = false
				*(dest[13].(*float64)) = 144
				*(dest[14].(*string)) = "CNY"
				*(dest[15].(*time.Time)) = start.AddDate(0, 0, 1)
				*(dest[16].(*time.Time)) = end
				*(dest[17].(*string)) = "partial"
				*(dest[18].(*int)) = 30
				*(dest[19].(*int)) = 31
				*(dest[20].(*time.Time)) = observed
				*(dest[21].(*time.Time)) = observed
				*(dest[22].(*bool)) = true
				return nil
			}), nil
		}
		return newEvidenceRows(func(dest ...any) error {
			if len(dest) != 9 {
				return errors.New("unexpected budget evidence scan destination count")
			}
			*(dest[0].(*string)) = "subscription_monthly_budgets"
			*(dest[1].(*time.Time)) = start
			*(dest[2].(*string)) = "CNY"
			*(dest[3].(*float64)) = 1000
			*(dest[4].(*int)) = 80
			*(dest[5].(*float64)) = 850
			*(dest[6].(*int64)) = 4
			*(dest[7].(*int64)) = 0
			*(dest[8].(*time.Time)) = observed.Add(time.Minute)
			return nil
		}), nil
	}}
	repository := &PostgresSubscriptionCostRepository{db: db}
	capture, err := repository.LoadSubscriptionCostEvidence(context.Background(), "vps_0123456789abcdef", evidence.TimeWindow{Start: start, End: end})
	if err != nil {
		t.Fatalf("LoadSubscriptionCostEvidence() error = %v", err)
	}
	if capture.OriginalCurrency != "USD" || capture.ConversionRate != 7.2 || capture.BaseCurrency != "CNY" || capture.BudgetCurrency != "CNY" || capture.BudgetStatus != "warning" || capture.CoverageStatus != "partial" {
		t.Fatalf("capture = %#v, want frozen cost/rate/budget/coverage", capture)
	}
}

func TestLoadSubscriptionCostEvidenceRejectsBudgetCurrencyDrift(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	observed := start.Add(20 * 24 * time.Hour)
	queryIndex := 0
	db := fakeSubscriptionCostDB{query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
		queryIndex++
		if queryIndex == 1 {
			return subscriptionCostEvidenceRows(start, end, observed), nil
		}
		return newEvidenceRows(func(dest ...any) error {
			if len(dest) != 9 {
				return errors.New("budget source must return its base currency")
			}
			*(dest[0].(*string)) = "subscription_monthly_budgets"
			*(dest[1].(*time.Time)) = start
			*(dest[2].(*string)) = "USD"
			*(dest[3].(*float64)) = 1000
			*(dest[4].(*int)) = 80
			*(dest[5].(*float64)) = 850
			*(dest[6].(*int64)) = 4
			*(dest[7].(*int64)) = 0
			*(dest[8].(*time.Time)) = observed
			return nil
		}), nil
	}}
	repository := &PostgresSubscriptionCostRepository{db: db}
	_, err := repository.LoadSubscriptionCostEvidence(context.Background(), "vps_0123456789abcdef", evidence.TimeWindow{Start: start, End: end})
	if err == nil || !strings.Contains(err.Error(), "budget currency") {
		t.Fatalf("LoadSubscriptionCostEvidence() error = %v, want budget currency drift rejection", err)
	}
}

func subscriptionCostEvidenceRows(start, end, observed time.Time) pgx.Rows {
	return newEvidenceRows(func(dest ...any) error {
		if len(dest) != 23 {
			return errors.New("unexpected subscription evidence scan destination count")
		}
		*(dest[0].(*int)) = 1
		*(dest[1].(*string)) = "sub_0123456789abcdef"
		*(dest[2].(*string)) = "vps_0123456789abcdef"
		*(dest[3].(*string)) = "sub_0123456789abcdef/rate_01/2026-07"
		*(dest[4].(*float64)) = 20
		*(dest[5].(*string)) = "USD"
		*(dest[6].(*string)) = "month"
		*(dest[7].(*int)) = 1
		*(dest[8].(*float64)) = 7.2
		*(dest[9].(*string)) = "frankfurter"
		*(dest[10].(*time.Time)) = start.Add(17 * 24 * time.Hour)
		*(dest[11].(*time.Time)) = observed.Add(-2 * time.Hour)
		*(dest[12].(*bool)) = false
		*(dest[13].(*float64)) = 144
		*(dest[14].(*string)) = "CNY"
		*(dest[15].(*time.Time)) = start.AddDate(0, 0, 1)
		*(dest[16].(*time.Time)) = end
		*(dest[17].(*string)) = "partial"
		*(dest[18].(*int)) = 30
		*(dest[19].(*int)) = 31
		*(dest[20].(*time.Time)) = observed
		*(dest[21].(*time.Time)) = observed
		*(dest[22].(*bool)) = true
		return nil
	})
}

func TestLoadAssetHistoryUsesAllFourBoundedFamilies(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	db := fakeRenewalDecisionDB{query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		switch {
		case strings.Contains(sql, "from renewal_decisions"):
			return newEvidenceRows(func(dest ...any) error {
				from := "unreviewed"
				*(dest[0].(*string)) = "ren_01"
				*(dest[1].(**string)) = &from
				*(dest[2].(*string)) = "keep"
				*(dest[3].(*string)) = "keep"
				*(dest[4].(*time.Time)) = start.Add(time.Hour)
				*(dest[5].(*time.Time)) = start.Add(time.Hour + time.Minute)
				return nil
			}), nil
		case strings.Contains(sql, "from price_histories"):
			return newEvidenceRows(func(dest ...any) error {
				*(dest[0].(*string)) = "ph_01"
				*(dest[1].(*string)) = "sub_01"
				*(dest[2].(*float64)) = 10
				*(dest[3].(*float64)) = 20
				*(dest[4].(*string)) = "USD"
				*(dest[5].(*string)) = "USD"
				*(dest[6].(*string)) = "month"
				*(dest[7].(*string)) = "month"
				*(dest[8].(*int)) = 1
				*(dest[9].(*int)) = 1
				*(dest[10].(*time.Time)) = start.Add(2 * time.Hour)
				*(dest[11].(*time.Time)) = start.Add(2*time.Hour + time.Minute)
				return nil
			}), nil
		case strings.Contains(sql, "from ip_histories"):
			return newEvidenceRows(func(dest ...any) error {
				*(dest[0].(*string)) = "iph_01"
				*(dest[1].(*string)) = "192.0.2.1"
				*(dest[2].(*string)) = "192.0.2.2"
				*(dest[3].(*string)) = ""
				*(dest[4].(*string)) = ""
				*(dest[5].(*time.Time)) = start.Add(3 * time.Hour)
				*(dest[6].(*time.Time)) = start.Add(3*time.Hour + time.Minute)
				return nil
			}), nil
		case strings.Contains(sql, "from vps_spec_snapshots"):
			return newEvidenceRows(func(dest ...any) error {
				*(dest[0].(*string)) = "vss_01"
				*(dest[1].(*string)) = "VPS-2"
				*(dest[2].(*string)) = "Debian 13"
				*(dest[3].(*string)) = "kvm"
				*(dest[4].(*int)) = 22
				*(dest[5].(*time.Time)) = start.Add(4 * time.Hour)
				*(dest[6].(*time.Time)) = start.Add(4*time.Hour + time.Minute)
				return nil
			}), nil
		default:
			return nil, errors.New("unexpected asset evidence query")
		}
	}}
	repository := &PostgresRenewalDecisionRepository{db: db}
	capture, err := repository.LoadAssetHistory(context.Background(), "vps_0123456789abcdef", evidence.TimeWindow{Start: start, End: start.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("LoadAssetHistory() error = %v", err)
	}
	if capture.FactCount != 4 || len(capture.RenewalDecisions) != 1 || len(capture.PriceHistories) != 1 || len(capture.IPHistories) != 1 || len(capture.SpecSnapshots) != 1 {
		t.Fatalf("capture = %#v, want four authoritative families", capture)
	}
}

func TestLoadAssetHistoryStopsAtGlobalSourceBound(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	scans := make([]func(...any) error, int(evidence.MaxSnapshotDataPoints)+1)
	for index := range scans {
		identity := fmt.Sprintf("ren_%08d", index)
		scans[index] = func(dest ...any) error {
			*(dest[0].(*string)) = identity
			*(dest[1].(**string)) = nil
			*(dest[2].(*string)) = "keep"
			*(dest[3].(*string)) = "keep"
			*(dest[4].(*time.Time)) = start.Add(time.Hour)
			*(dest[5].(*time.Time)) = start.Add(time.Hour + time.Minute)
			return nil
		}
	}
	queryCount := 0
	db := fakeRenewalDecisionDB{query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		queryCount++
		if strings.Contains(sql, "from renewal_decisions") {
			return newEvidenceRows(scans...), nil
		}
		return newEvidenceRows(), nil
	}}
	repository := &PostgresRenewalDecisionRepository{db: db}
	_, err := repository.LoadAssetHistory(context.Background(), "vps_0123456789abcdef", evidence.TimeWindow{Start: start, End: start.Add(24 * time.Hour)})
	if err == nil || !strings.Contains(err.Error(), "exceeds source bound") {
		t.Fatalf("LoadAssetHistory() error = %v, want global source bound rejection", err)
	}
	if queryCount != 1 {
		t.Fatalf("LoadAssetHistory() query count = %d, want early stop after bounded renewal query", queryCount)
	}
}

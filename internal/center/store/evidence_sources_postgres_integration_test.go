package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/targets"
)

func TestPostgresIntegrationEvidenceSources(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-sources", 2)

	now := time.Now().UTC().Truncate(time.Second)
	window := evidence.TimeWindow{Start: now.Add(-30 * 24 * time.Hour), End: now}
	eventWindow := evidence.TimeWindow{Start: window.Start, End: now.Add(5 * time.Minute)}
	partialDay := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -10)
	completeDay := partialDay.AddDate(0, 0, 2)
	seedEvidenceSourceFixtures(t, ctx, fixture.db, fixture.db, now, partialDay, completeDay)

	runtimeRepository := &PostgresRuntimeFactsRepository{db: runtimePool}
	hostCapture, err := runtimeRepository.LoadMonitoringHostEvidence(ctx, "mi_0123456789abcdef", window, time.Hour, []string{
		"cpu_usage_pct", "disk_read_bytes_per_sec", "disk_used_pct", "inode_used_pct", "net_in_bytes_per_sec",
	})
	if err != nil {
		t.Fatalf("LoadMonitoringHostEvidence() error = %v", err)
	}
	if hostCapture.ActualPrecision != 24*time.Hour {
		t.Fatalf("host actual precision = %s, want daily after partial raw retention fallback", hostCapture.ActualPrecision)
	}
	if len(hostCapture.Buckets) != 3 {
		t.Fatalf("host buckets = %d, want recent raw plus two daily buckets: %#v", len(hostCapture.Buckets), hostCapture.Buckets)
	}
	assertEvidenceCaptureHasDailyBucket(t, hostCapture.Buckets, partialDay, 2, 1, 1)
	assertEvidenceCaptureHasDailyBucket(t, hostCapture.Buckets, completeDay, 1, 0, 0)
	if hostCapture.Buckets[2].SourceLayer != adapters.MonitoringSourceRaw || hostCapture.Buckets[2].SampleCount != 1 {
		t.Fatalf("host recent bucket = %#v, want raw one-sample bucket", hostCapture.Buckets[2])
	}
	assertEvidenceBucketMetrics(t, hostCapture.Buckets[2], "disk_read_bytes_per_sec", "disk_used_pct", "inode_used_pct", "net_in_bytes_per_sec")
	monitoringAdapter, err := adapters.NewMonitoringHostAdapter(
		integrationMonitoringSource{host: hostCapture}, integrationMonitoringResolver{kind: recordauth.SourceKindMonitoringInstance, sourceID: "mi_0123456789abcdef"},
		adapters.AdapterOptions{Clock: func() time.Time { return now.Add(time.Hour) }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}
	monitoringActor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatalf("NormalizeActorScope(monitoring) error = %v", err)
	}
	monitoringPreview, err := monitoringAdapter.PreviewCapture(ctx, monitoringActor, evidence.Selection{
		Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window,
		Metrics: []string{"cpu_usage_pct", "disk_read_bytes_per_sec", "disk_used_pct", "inode_used_pct", "net_in_bytes_per_sec"},
	})
	if err != nil {
		t.Fatalf("monitoring PreviewCapture() error = %v", err)
	}
	if monitoringPreview.Quality.GapCount == 0 || monitoringPreview.Quality.MaintenanceCount != 1 || monitoringPreview.Quality.BackfilledCount != 1 {
		t.Fatalf("monitoring quality = %#v, want explicit gap and retention counts", monitoringPreview.Quality)
	}

	probeCapture, err := runtimeRepository.LoadMonitoringProbeEvidence(ctx, "tg_evidence_sources", window, time.Hour, []string{"http_status", "latency_ms", "success_ratio", "tls_expiry_days"})
	if err != nil {
		t.Fatalf("LoadMonitoringProbeEvidence() error = %v", err)
	}
	if probeCapture.ActualPrecision != 24*time.Hour || len(probeCapture.Buckets) != 3 {
		t.Fatalf("probe capture precision/buckets = %s/%d, want daily/3: %#v", probeCapture.ActualPrecision, len(probeCapture.Buckets), probeCapture.Buckets)
	}
	if probeCapture.Buckets[0].SeriesID != "pb_evidence_sources" || probeCapture.Buckets[1].SeriesID != "pb_evidence_sources" {
		t.Fatalf("probe series IDs = %#v, want probe item series", probeCapture.Buckets)
	}
	assertEvidenceBucketMetrics(t, probeCapture.Buckets[2], "http_status", "latency_ms", "success_ratio", "tls_expiry_days")

	ipRepository := &PostgresIPQualityRepository{db: runtimePool}
	report, err := ipRepository.LoadIPQualityEvidence(ctx, "vps_0123456789abcdef", window)
	if err != nil {
		t.Fatalf("LoadIPQualityEvidence() error = %v", err)
	}
	if report.ReportID != "ipq_evidence_valid" || report.StaleAfter != time.Hour || report.Status != "partial" || !report.IsBackfilled || report.AssignmentMode != "link" || report.Ambiguous {
		t.Fatalf("IP report = %#v, want valid linked partial report with custom stale policy", report)
	}
	if len(report.Providers) != 1 || len(report.Services) != 1 || report.Providers[0].Provider != "ipapi.is" || report.Services[0].ProbeStatus != "failure" {
		t.Fatalf("IP matrices = %#v/%#v, want normalized rows", report.Providers, report.Services)
	}

	eventRepository := NewPostgresIncidentRepository(runtimePool)
	eventCapture, err := eventRepository.LoadMonitoringEventEvidence(ctx, string(recordauth.SourceKindMonitoringInstance), "mi_0123456789abcdef", eventWindow)
	if err != nil {
		t.Fatalf("LoadMonitoringEventEvidence() error = %v", err)
	}
	if eventCapture.EventCount != 2 || eventCapture.Events[0].EventType != string(incidents.EventIncidentStarted) || eventCapture.Events[0].ProducerVersion != incidents.MonitoringEventProducerVersion || eventCapture.Events[0].RuleVersion != incidents.MonitoringEventIncidentRuleVersion || eventCapture.Events[1].EventType != string(incidents.EventCorrected) || !eventCapture.Events[1].Backfilled || eventCapture.Events[1].CorrectionOfEventID != eventCapture.Events[0].EventID {
		t.Fatalf("event capture = %#v, want normal and correction facts from the incident writer path", eventCapture)
	}
	malformedEventTimes := []struct {
		sourceID string
		raw      string
	}{
		{sourceID: "mi_evidence_offset_time", raw: now.Add(-90 * time.Minute).In(time.FixedZone("offset", 8*60*60)).Format(time.RFC3339Nano)},
		{sourceID: "mi_evidence_submicro_time", raw: now.Add(-80 * time.Minute).Add(123 * time.Nanosecond).Format(time.RFC3339Nano)},
	}
	for _, malformed := range malformedEventTimes {
		execEvidenceSQL(t, ctx, fixture.db, `
			insert into state_change_events (
				event_id, object_type, object_id, event_type, severity, summary, payload, created_at
			) values (
				'evt_' || $1, 'monitoring_instance', $1, 'incident_started', '告警',
				'event with malformed canonical time', jsonb_build_object(
					'event_at', $2::text, 'is_backfilled', false, 'provenance', 'center',
					'producer_version', 'center-monitoring-events/v1', 'rule_version', 'incident-rules/v1',
					'prior_state', 'normal', 'resulting_state', 'alert'
				), $3
			)`, malformed.sourceID, malformed.raw, now)
		if _, err := eventRepository.LoadMonitoringEventEvidence(ctx, string(recordauth.SourceKindMonitoringInstance), malformed.sourceID, eventWindow); err == nil {
			t.Fatalf("LoadMonitoringEventEvidence(%q) error = nil, want noncanonical persisted event_at %q rejected", malformed.sourceID, malformed.raw)
		}
	}
	execEvidenceSQL(t, ctx, fixture.db, `
		insert into state_change_events (
			event_id, object_type, object_id, event_type, severity, summary, payload, created_at
		) values (
			'evt_evidence_legacy_incomplete', 'monitoring_instance', 'mi_0123456789abcdef',
			'incident_started', '告警', 'legacy event without Task 4 metadata',
			jsonb_build_object('incident_id', 'inc_legacy', 'incident_class', 'monitoring_instance_resource_pressure'),
			$1
		)`, now.Add(-2*time.Hour))
	if _, err := eventRepository.LoadMonitoringEventEvidence(ctx, string(recordauth.SourceKindMonitoringInstance), "mi_0123456789abcdef", eventWindow); err == nil {
		t.Fatal("LoadMonitoringEventEvidence() error = nil, want legacy row in the requested occurrence window to fail closed")
	}

	commandRepository := NewPostgresCommandAuditRepository(runtimePool)
	commandCapture, err := commandRepository.LoadCommandAuditEvidence(ctx, "mi_0123456789abcdef", window)
	if err != nil {
		t.Fatalf("LoadCommandAuditEvidence() error = %v", err)
	}
	if commandCapture.AuditCount != 2 || commandCapture.Audits[1].Outcome != "succeeded" || commandCapture.Audits[1].ExitCode == nil || *commandCapture.Audits[1].ExitCode != 0 {
		t.Fatalf("command capture = %#v, want metadata-only completed action", commandCapture)
	}

	costWindow := evidence.TimeWindow{
		Start: time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
	}
	costRepository := NewPostgresSubscriptionCostRepository(runtimePool)
	costCapture, err := costRepository.LoadSubscriptionCostEvidence(ctx, "vps_0123456789abcdef", costWindow)
	if err != nil {
		t.Fatalf("LoadSubscriptionCostEvidence() error = %v", err)
	}
	if costCapture.OriginalCurrency != "USD" || costCapture.ConversionRate != 7.2 || costCapture.BaseCurrency != "CNY" || costCapture.BudgetMonth != costWindow.Start.AddDate(0, -1, 0).Format("2006-01") || costCapture.BudgetActualSpend != 160 || costCapture.ConvertedSubscriptionCount != 2 || costCapture.BudgetStatus != "over" || costCapture.CoverageStatus != "partial" {
		t.Fatalf("cost capture = %#v, want frozen original/rate/base/budget/coverage facts", costCapture)
	}

	assetRepository := NewPostgresRenewalDecisionRepository(runtimePool)
	assetCapture, err := assetRepository.LoadAssetHistory(ctx, "vps_0123456789abcdef", window)
	if err != nil {
		t.Fatalf("LoadAssetHistory() error = %v", err)
	}
	if assetCapture.FactCount != 4 || len(assetCapture.RenewalDecisions) != 1 || len(assetCapture.PriceHistories) != 1 || len(assetCapture.IPHistories) != 1 || len(assetCapture.SpecSnapshots) != 1 {
		t.Fatalf("asset capture = %#v, want all four authoritative history families", assetCapture)
	}

	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	adapter, err := adapters.NewIPQualityAdapter(
		integrationIPQualitySource{report: report}, integrationEvidenceResolver{sourceID: "vps_0123456789abcdef"},
		adapters.AdapterOptions{Clock: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef", RequestedWindow: window}
	preview, err := adapter.PreviewCapture(ctx, actor, selection)
	if err != nil {
		t.Fatalf("IP PreviewCapture() error = %v", err)
	}
	snapshot, err := adapter.Capture(ctx, actor, evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: [32]byte{1}, ValidUntil: preview.ValidUntil})
	if err != nil {
		t.Fatalf("IP Capture() error = %v", err)
	}
	for _, forbidden := range []string{"raw_json", "diagnostics_json", "extra_json", "fingerprint", "sync_batch_id", "secret-token", "diagnostic-secret", "provider-extra-secret", "service-extra-secret"} {
		if bytes.Contains(snapshot.Bytes(), []byte(forbidden)) {
			t.Fatalf("IP canonical payload contains forbidden source value %q: %s", forbidden, snapshot.Bytes())
		}
	}

	task4Clock := eventWindow.End.Add(time.Hour)
	eventAdapter, err := adapters.NewMonitoringEventAdapter(
		integrationMonitoringEventSource{capture: eventCapture},
		integrationMonitoringResolver{kind: recordauth.SourceKindMonitoringInstance, sourceID: "mi_0123456789abcdef"},
		adapters.AdapterOptions{Clock: func() time.Time { return task4Clock }, NewIntentID: integrationEvidenceIntentID},
	)
	if err != nil {
		t.Fatalf("NewMonitoringEventAdapter() error = %v", err)
	}
	eventSelection := evidence.Selection{Key: evidence.MonitoringEventV2Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: eventWindow}
	eventSnapshot := captureIntegrationEvidence(t, ctx, eventAdapter, monitoringActor, eventSelection)

	commandAdapter, err := adapters.NewCommandAuditAdapter(
		integrationCommandAuditSource{capture: commandCapture},
		integrationMonitoringResolver{kind: recordauth.SourceKindMonitoringInstance, sourceID: "mi_0123456789abcdef"},
		adapters.AdapterOptions{Clock: func() time.Time { return task4Clock }, NewIntentID: integrationEvidenceIntentID},
	)
	if err != nil {
		t.Fatalf("NewCommandAuditAdapter() error = %v", err)
	}
	commandSelection := evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window}
	commandSnapshot := captureIntegrationEvidence(t, ctx, commandAdapter, monitoringActor, commandSelection)

	costAdapter, err := adapters.NewSubscriptionCostAdapter(
		integrationSubscriptionCostSource{capture: costCapture}, integrationEvidenceResolver{sourceID: "vps_0123456789abcdef"},
		adapters.AdapterOptions{Clock: func() time.Time { return task4Clock }, NewIntentID: integrationEvidenceIntentID},
	)
	if err != nil {
		t.Fatalf("NewSubscriptionCostAdapter() error = %v", err)
	}
	costSelection := evidence.Selection{Key: evidence.SubscriptionCostV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef", RequestedWindow: costWindow}
	costSnapshot := captureIntegrationEvidence(t, ctx, costAdapter, actor, costSelection)

	assetAdapter, err := adapters.NewAssetHistoryAdapter(integrationAssetHistorySource{capture: assetCapture})
	if err != nil {
		t.Fatalf("NewAssetHistoryAdapter() error = %v", err)
	}
	if _, err := assetAdapter.Load(ctx, "vps_0123456789abcdef", window); err != nil {
		t.Fatalf("asset history adapter Load() error = %v", err)
	}
	for name, canonical := range map[string][]byte{"event": eventSnapshot.Bytes(), "command": commandSnapshot.Bytes(), "cost": costSnapshot.Bytes()} {
		for _, forbidden := range []string{"event-source-secret", "command-details-secret", "fixer-source-secret", "provider-raw-secret", "stdout", "stderr", "details", "raw_json"} {
			if bytes.Contains(canonical, []byte(forbidden)) {
				t.Fatalf("%s canonical payload contains forbidden source value %q: %s", name, forbidden, canonical)
			}
		}
	}
}

func seedEvidenceSourceFixtures(t *testing.T, ctx context.Context, db, writerDB *pgxpool.Pool, now, partialDay, completeDay time.Time) {
	t.Helper()
	execEvidenceSQL(t, ctx, db, `
		insert into monitoring_instances (monitoring_instance_id, display_name, region, city, provider, lifecycle_status)
		values ('mi_0123456789abcdef', 'Evidence Sources MI', 'Tokyo', 'Tokyo', 'Evidence Provider', '在用')`)
	execEvidenceSQL(t, ctx, db, `
		insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ('vps_0123456789abcdef', 'Evidence Sources VPS', 'active', 'in_use')`)
	execEvidenceSQL(t, ctx, db, `
		insert into vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id, note)
		values ('vnl_evidence_sources', 'vps_0123456789abcdef', 'mi_0123456789abcdef', '')`)
	execEvidenceSQL(t, ctx, db, `
		insert into targets (target_id, name, target_type, host, run_status)
		values ('tg_evidence_sources', 'Evidence Target', 'hostname', 'example.com', 'enabled')`)
	execEvidenceSQL(t, ctx, db, `
		insert into probe_items (probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds)
		values ('pb_evidence_sources', 'tg_evidence_sources', 'http', '5m', 10)`)
	execEvidenceSQL(t, ctx, db, `
		insert into center_settings (settings_id, ip_quality_settings)
		values ('center', jsonb_build_object('stale_after_seconds', 3600))
		on conflict (settings_id) do update set ip_quality_settings = excluded.ip_quality_settings`)

	seedHostSamples(t, ctx, db, now, partialDay)
	seedProbeObservations(t, ctx, db, now, partialDay)
	seedDailyAggregates(t, ctx, db, partialDay, completeDay)
	seedIPQualityReports(t, ctx, db, now)
	seedTask4EvidenceSources(t, ctx, db, writerDB, now)
}

func seedTask4EvidenceSources(t *testing.T, ctx context.Context, db, writerDB *pgxpool.Pool, now time.Time) {
	t.Helper()
	eventAt := now.Add(-4 * time.Hour)
	lastHeartbeatAt := eventAt.Add(-4 * 5 * time.Second)
	evaluation := incidents.EvaluateMonitoringInstanceHeartbeatMissing(nil, "mi_0123456789abcdef", eventAt, &lastHeartbeatAt, 5*time.Second)
	if evaluation.Current == nil || evaluation.Event == nil || evaluation.Event.EventType != incidents.EventIncidentStarted {
		t.Fatalf("heartbeat evaluation = %#v, want incident-start event", evaluation)
	}
	eventRepository := NewPostgresIncidentRepository(writerDB)
	if err := eventRepository.ApplyIncidentMutation(ctx, incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeMonitoringInstance,
		ObjectID:   "mi_0123456789abcdef",
		Active:     []incidents.IncidentRecord{*evaluation.Current},
		Events:     []incidents.StateChangeEventRecord{*evaluation.Event},
	}); err != nil {
		t.Fatalf("ApplyIncidentMutation() for normal Task 4 writer-path fixture: %v", err)
	}
	var startedEventID string
	if err := db.QueryRow(ctx, `
		select event_id
		from state_change_events
		where object_type = 'monitoring_instance'
			and object_id = 'mi_0123456789abcdef'
			and event_type = 'incident_started'`).Scan(&startedEventID); err != nil {
		t.Fatalf("load incident writer event identity: %v", err)
	}
	if err := eventRepository.ApplyIncidentMutation(ctx, incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeMonitoringInstance,
		ObjectID:   "mi_0123456789abcdef",
		Events: []incidents.StateChangeEventRecord{{
			IncidentID:          "inc_monitoring_instance_cpu",
			IncidentClass:       incidents.IncidentMonitoringInstanceResourcePressure,
			ObjectType:          incidents.ObjectTypeMonitoringInstance,
			ObjectID:            "mi_0123456789abcdef",
			EventType:           incidents.EventCorrected,
			Severity:            incidents.SeverityCritical,
			Summary:             "CPU correction",
			CreatedAt:           eventAt.Add(time.Hour),
			IsBackfilled:        true,
			Provenance:          incidents.MonitoringEventProvenanceManualCorrection,
			ProducerVersion:     incidents.MonitoringEventProducerVersion,
			RuleVersion:         incidents.MonitoringEventIncidentRuleVersion,
			PriorState:          "alert",
			ResultingState:      "critical",
			CorrectionOfEventID: startedEventID,
		}},
	}); err != nil {
		t.Fatalf("ApplyIncidentMutation() for Task 4 writer-path fixture: %v", err)
	}

	seedTask4StateControlWriterFixtures(t, ctx, db, writerDB, now)

	execEvidenceSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, monitoring_instance_name_snapshot,
			command_id, sensitivity, event_type, actor_user_id, actor_username_snapshot,
			actor_display_name_snapshot, source, exit_code, occurred_at, details
		) values
			('aud_evidence_queued', 'act_evidence', 'mi_0123456789abcdef', 'Evidence Sources MI',
			 'uptime', 'standard', 'queued', null, 'operator', 'Operator', 'web', null, $1,
			 jsonb_build_object('diagnostic', 'command-details-secret')),
			('aud_evidence_completed', 'act_evidence', 'mi_0123456789abcdef', 'Evidence Sources MI',
			 'uptime', 'standard', 'completed', null, 'operator', 'Operator', 'agent_sync', 0, $1::timestamptz + interval '1 minute',
			 jsonb_build_object('diagnostic', 'https://user:pass@example.com/run?token=command-details-secret'))`, now.Add(-3*time.Hour))

	costStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	execEvidenceSQL(t, ctx, db, `
		update center_settings
		set subscription_cost_settings = jsonb_build_object(
			'base_currency', 'CNY', 'exchange_rate_provider', 'frankfurter',
			'fixer_api_key', 'fixer-source-secret', 'exchange_rate_stale_after_hours', 36,
			'provider_response', 'provider-raw-secret'
		), updated_at = $1`, costStart.Add(20*24*time.Hour))
	execEvidenceSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, billing_period_unit, billing_period_length, started_at,
			renew_at, status, created_at, updated_at
		) values ('sub_evidence_sources', 'vps_0123456789abcdef', 20, 'USD', 'monthly', 1,
			20, 'month', 1, $1::date + 1, $1::date + interval '2 months', 'active', $1, $1)`, costStart)
	execEvidenceSQL(t, ctx, db, `
		insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ('vps_evidence_budget_peer', 'Evidence Budget Peer', 'active', 'idle')`)
	execEvidenceSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, billing_period_unit, billing_period_length, started_at,
			renew_at, status, created_at, updated_at
		) values ('sub_evidence_budget_peer', 'vps_evidence_budget_peer', 30.4, 'CNY', 'monthly', 1,
			30.4, 'month', 1, $1::date, $1::date + interval '2 months', 'active', $1, $1)`, costStart)
	execEvidenceSQL(t, ctx, db, `
		insert into subscription_exchange_rates (
			rate_id, provider, base_currency, quote_currency, rate, rate_date, fetched_at,
			error_summary, created_at, updated_at
		) values ('rate_evidence_sources', 'frankfurter', 'CNY', 'USD', 7.2, $1::date + 17,
			$1::timestamptz + interval '18 days', 'provider-raw-secret', $1, $1::timestamptz + interval '18 days')`, costStart)
	execEvidenceSQL(t, ctx, db, `
		insert into subscription_monthly_budgets (budget_month, base_currency, monthly_limit, warning_pct, note, created_at, updated_at)
		values (($1::date - interval '1 month')::date, 'CNY', 160, 80, 'provider-raw-secret', $1::timestamptz, $1::timestamptz + interval '20 days')`, costStart)

	historyAt := now.Add(-2 * time.Hour)
	execEvidenceSQL(t, ctx, db, `
		insert into renewal_decisions (decision_id, vps_id, from_decision, to_decision, reason, decided_at, created_at)
		values ('ren_evidence_sources', 'vps_0123456789abcdef', 'unreviewed', 'keep', 'keep', $1, $1::timestamptz + interval '1 minute')`, historyAt)
	execEvidenceSQL(t, ctx, db, `
		insert into price_histories (
			price_history_id, subscription_id, vps_id, from_price, to_price, from_currency,
			to_currency, from_billing_cycle, to_billing_cycle, from_billing_months,
			to_billing_months, from_monthly_price, to_monthly_price, from_auto_renew,
			to_auto_renew, from_auto_renew_cancelled, to_auto_renew_cancelled, from_status,
			to_status, from_billing_period_unit, to_billing_period_unit,
			from_billing_period_length, to_billing_period_length, changed_at, created_at
		) values ('ph_evidence_sources', 'sub_evidence_sources', 'vps_0123456789abcdef', 18, 20, 'USD',
			'USD', 'monthly', 'monthly', 1, 1, 18, 20, false, false, false, false, 'active',
			'active', 'month', 'month', 1, 1, $1::timestamptz + interval '10 minutes', $1::timestamptz + interval '11 minutes')`, historyAt)
	execEvidenceSQL(t, ctx, db, `
		insert into ip_histories (ip_history_id, vps_id, from_ipv4, to_ipv4, from_ipv6, to_ipv6, changed_at, created_at)
		values ('iph_evidence_sources', 'vps_0123456789abcdef', '192.0.2.1', '192.0.2.2', '', '', $1::timestamptz + interval '20 minutes', $1::timestamptz + interval '21 minutes')`, historyAt)
	execEvidenceSQL(t, ctx, db, `
		insert into vps_spec_snapshots (snapshot_id, vps_id, product_name, ssh_host, ssh_port, ssh_user, os_name, virtualization, captured_at, created_at)
		values ('vss_evidence_sources', 'vps_0123456789abcdef', 'VPS-2', 'source-only.example', 22, 'root', 'Debian 13', 'kvm', $1::timestamptz + interval '30 minutes', $1::timestamptz + interval '31 minutes')`, historyAt)
}

func seedTask4StateControlWriterFixtures(t *testing.T, ctx context.Context, db, writerDB *pgxpool.Pool, now time.Time) {
	t.Helper()
	execEvidenceSQL(t, ctx, db, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider,
			lifecycle_status, monitoring_status, binding_status, binding_fingerprint
		) values
			('mi_evidence_binding', 'Evidence Binding Writer', 'Tokyo', 'Tokyo', 'Evidence Provider', '在用', '启用', '已绑定', 'fp-evidence-binding'),
			('mi_evidence_lifecycle', 'Evidence Lifecycle Writer', 'Tokyo', 'Tokyo', 'Evidence Provider', '在用', '启用', '已绑定', 'fp-evidence-lifecycle'),
			('mi_evidence_runtime', 'Evidence Runtime Writer', 'Tokyo', 'Tokyo', 'Evidence Provider', '在用', '启用', '已绑定', 'fp-evidence-runtime')`)
	execEvidenceSQL(t, ctx, db, `
		insert into targets (target_id, name, target_type, host, run_status)
		values ('tg_evidence_runtime', 'Evidence Runtime Target', 'service', 'runtime.example.com', '启用')`)

	monitoringRepository := NewPostgresMonitoringInstanceRepository(writerDB)
	if _, err := monitoringRepository.ResetMonitoringInstanceBinding(ctx, "mi_evidence_binding"); err != nil {
		t.Fatalf("ResetMonitoringInstanceBinding() Task 4 PostgreSQL writer path: %v", err)
	}
	if _, err := monitoringRepository.RetireMonitoringInstance(ctx, "mi_evidence_lifecycle", monitoringinstances.LifecycleActionInput{Reason: "Task 4 evidence writer acceptance"}); err != nil {
		t.Fatalf("RetireMonitoringInstance() Task 4 PostgreSQL writer path: %v", err)
	}
	if _, err := monitoringRepository.SetMonitoringInstanceMonitoringMaintenance(ctx, "mi_evidence_runtime"); err != nil {
		t.Fatalf("SetMonitoringInstanceMonitoringMaintenance() Task 4 PostgreSQL writer path: %v", err)
	}
	targetRepository := NewPostgresTargetRepository(writerDB)
	if _, err := targetRepository.SetTargetMaintenance(ctx, "tg_evidence_runtime"); err != nil {
		t.Fatalf("SetTargetMaintenance() Task 4 PostgreSQL writer path: %v", err)
	}

	eventRepository := NewPostgresIncidentRepository(writerDB)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now.Add(5 * time.Minute)}
	tests := []struct {
		name           string
		sourceType     string
		sourceID       string
		eventType      incidents.EventType
		ruleVersion    string
		priorState     string
		resultingState string
	}{
		{name: "binding", sourceType: string(recordauth.SourceKindMonitoringInstance), sourceID: "mi_evidence_binding", eventType: incidents.EventMonitoringInstanceBindingReset, ruleVersion: incidents.MonitoringEventBindingRuleVersion, priorState: monitoringinstances.BindingBound, resultingState: monitoringinstances.BindingUnbound},
		{name: "lifecycle", sourceType: string(recordauth.SourceKindMonitoringInstance), sourceID: "mi_evidence_lifecycle", eventType: incidents.EventMonitoringInstanceRetired, ruleVersion: incidents.MonitoringEventLifecycleRuleVersion, priorState: monitoringinstances.LifecycleInUse, resultingState: monitoringinstances.LifecycleRetired},
		{name: "monitoring runtime", sourceType: string(recordauth.SourceKindMonitoringInstance), sourceID: "mi_evidence_runtime", eventType: incidents.EventMonitoringInstanceMonitoringMaintenanceEntered, ruleVersion: incidents.MonitoringEventRuntimeRuleVersion, priorState: monitoringinstances.MonitoringEnabled, resultingState: monitoringinstances.MonitoringMaintenance},
		{name: "target runtime", sourceType: string(recordauth.SourceKindTarget), sourceID: "tg_evidence_runtime", eventType: incidents.EventTargetMaintenanceEntered, ruleVersion: incidents.MonitoringEventTargetRuleVersion, priorState: targets.RunStatusEnabled, resultingState: targets.RunStatusMaintenance},
	}
	for _, tt := range tests {
		capture, err := eventRepository.LoadMonitoringEventEvidence(ctx, tt.sourceType, tt.sourceID, window)
		if err != nil {
			t.Fatalf("LoadMonitoringEventEvidence() for %s writer: %v", tt.name, err)
		}
		if capture.EventCount != 1 {
			t.Fatalf("%s writer capture = %#v, want one reachable event", tt.name, capture)
		}
		event := capture.Events[0]
		if event.EventType != string(tt.eventType) || event.Provenance != incidents.MonitoringEventProvenanceWeb || event.ProducerVersion != incidents.MonitoringEventProducerVersion || event.RuleVersion != tt.ruleVersion || event.PriorState != tt.priorState || event.ResultingState != tt.resultingState || event.Backfilled || event.CorrectionOfEventID != "" || !event.EventAt.Equal(event.RecordedAt) {
			t.Fatalf("%s writer event = %#v, want authoritative reachable state transition", tt.name, event)
		}
	}
}

func seedHostSamples(t *testing.T, ctx context.Context, db *pgxpool.Pool, now, partialDay time.Time) {
	const insert = `
		insert into host_samples (
			monitoring_instance_id, observed_at, received_at, agent_version,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec, net_out_bytes_per_sec,
			cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
			disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
		) values ($1, $2::timestamptz, $2::timestamptz + interval '1 minute', 'agent/v1', 25, 0.5, 0.7, 0.9, 55, 1000000, 0, 40, 10, 100, 200, 2, 1, 10, 20, 30, 1000, $3, $4, 'sync-evidence')`
	execEvidenceSQL(t, ctx, db, insert, "mi_0123456789abcdef", now.Add(-2*time.Hour), false, false)
	execEvidenceSQL(t, ctx, db, insert, "mi_0123456789abcdef", partialDay.Add(12*time.Hour), true, true)
}

func seedProbeObservations(t *testing.T, ctx context.Context, db *pgxpool.Pool, now, partialDay time.Time) {
	const insert = `
		insert into probe_observations (
			monitoring_instance_id, target_id, probe_item_id, observed_at, received_at,
			result_kind, latency_ms, http_status, tls_expiry_days, maintenance_context, is_backfilled, sync_batch_id
		) values ('mi_0123456789abcdef', 'tg_evidence_sources', 'pb_evidence_sources', $1::timestamptz, $1::timestamptz + interval '1 minute', 'success', 120, 204, 20, $2, $3, 'sync-evidence')`
	execEvidenceSQL(t, ctx, db, insert, now.Add(-2*time.Hour), false, false)
	execEvidenceSQL(t, ctx, db, insert, partialDay.Add(12*time.Hour), true, true)
}

func seedDailyAggregates(t *testing.T, ctx context.Context, db *pgxpool.Pool, partialDay, completeDay time.Time) {
	const hostInsert = `
		insert into monitoring_instance_host_sample_daily_aggregates (
			monitoring_instance_id, bucket_date, sample_count, avg_cpu_usage_pct, max_cpu_usage_pct,
			avg_load_5, max_load_5, avg_mem_used_pct, max_mem_used_pct, avg_cpu_iowait_pct,
			max_cpu_iowait_pct, avg_cpu_steal_pct, max_cpu_steal_pct, avg_disk_busy_pct,
			max_disk_busy_pct, backfilled_sample_count, maintenance_sample_count
		) values ('mi_0123456789abcdef', $1, $2, 30, 60, 0.8, 1.2, 50, 70, 2, 4, 1, 2, 20, 40, $3, $4)`
	execEvidenceSQL(t, ctx, db, hostInsert, partialDay, 2, 1, 1)
	execEvidenceSQL(t, ctx, db, hostInsert, completeDay, 1, 0, 0)

	const probeInsert = `
		insert into target_probe_daily_aggregates (
			target_id, probe_item_id, bucket_date, observation_count, success_count, failure_count,
			avg_latency_ms, p95_latency_ms, min_tls_expiry_days, backfilled_observation_count, maintenance_observation_count
		) values ('tg_evidence_sources', 'pb_evidence_sources', $1, $2, $3, $4, 110, 130, 20, $5, $6)`
	execEvidenceSQL(t, ctx, db, probeInsert, partialDay, 2, 2, 0, 1, 1)
	execEvidenceSQL(t, ctx, db, probeInsert, completeDay, 1, 1, 0, 0, 0)
}

func seedIPQualityReports(t *testing.T, ctx context.Context, db *pgxpool.Pool, now time.Time) {
	execEvidenceSQL(t, ctx, db, `
		insert into ip_quality_reports (
			report_id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			sync_batch_id, ip_address, ip_version, status, asn, organization, risk_level,
			is_backfilled, raw_json, coverage_json, diagnostics_json
		) values ('ipq_evidence_valid', 'mi_0123456789abcdef', $1::timestamptz, $1::timestamptz + interval '1 minute', 'agent/v1', 'fp-valid',
			'sync-valid', '203.0.113.10', 4, 'partial', 'AS64500', 'Example Transit', 'medium', true,
			'{"token":"secret-token"}', '{"expected_provider_count":1,"successful_provider_count":1,"expected_service_count":1,"failed_service_count":1}', '{"diagnostic":"diagnostic-secret"}')`, now.Add(-2*time.Hour))
	execEvidenceSQL(t, ctx, db, `
		insert into ip_quality_provider_results (
			result_id, report_id, provider, status, source_type, usage_type, company_type, risk_level,
			risk_score, region_code, region_name, is_proxy, latency_ms, extra_json
		) values ('ipr_evidence_valid', 'ipq_evidence_valid', 'ipapi.is', 'success', 'default', 'isp', 'business', 'low', '10', 'JP', 'Tokyo', false, 25, '{"extra":"provider-extra-secret"}')`)
	execEvidenceSQL(t, ctx, db, `
		insert into ip_quality_service_unlocks (
			unlock_id, report_id, service, source, status, probe_status, region, unlock_type, extra_json
		) values ('ips_evidence_valid', 'ipq_evidence_valid', 'netflix', 'builtin', 'unknown', 'failure', '', '', '{"extra":"service-extra-secret"}')`)
	execEvidenceSQL(t, ctx, db, `
		insert into ip_quality_reports (
			report_id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			sync_batch_id, ip_address, ip_version, status, risk_level, raw_json
		) values ('ipq_evidence_failure', 'mi_0123456789abcdef', $1::timestamptz, $1::timestamptz + interval '1 minute', 'agent/v1', 'fp-failure',
			'sync-failure', '0.0.0.0', 4, 'failure', 'high', '{"token":"failure-secret"}')`, now.Add(-time.Hour))
}

func assertEvidenceCaptureHasDailyBucket(t *testing.T, buckets []adapters.MonitoringBucket, day time.Time, samples, maintenance, backfilled uint64) {
	t.Helper()
	for _, bucket := range buckets {
		if bucket.SourceLayer == adapters.MonitoringSourceDailyAggregate && bucket.Start.Equal(day.UTC()) {
			if bucket.SampleCount != samples || bucket.MaintenanceCount != maintenance || bucket.BackfilledCount != backfilled {
				t.Fatalf("daily bucket %s counts = %d/%d/%d, want %d/%d/%d", day, bucket.SampleCount, bucket.MaintenanceCount, bucket.BackfilledCount, samples, maintenance, backfilled)
			}
			return
		}
	}
	t.Fatalf("no daily bucket at %s in %#v", day, buckets)
}

func assertEvidenceBucketMetrics(t *testing.T, bucket adapters.MonitoringBucket, expected ...string) {
	t.Helper()
	seen := make(map[string]struct{}, len(bucket.Metrics))
	for _, metric := range bucket.Metrics {
		seen[metric.Name] = struct{}{}
	}
	for _, metric := range expected {
		if _, ok := seen[metric]; !ok {
			t.Fatalf("bucket metrics = %#v, want %q", bucket.Metrics, metric)
		}
	}
}

func execEvidenceSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("evidence fixture SQL error: %v", err)
	}
}

type integrationIPQualitySource struct {
	report adapters.IPQualityEvidenceReport
}

func (source integrationIPQualitySource) LoadIPQualityEvidence(context.Context, string, evidence.TimeWindow) (adapters.IPQualityEvidenceReport, error) {
	return source.report, nil
}

type integrationEvidenceResolver struct {
	sourceID string
}

func (resolver integrationEvidenceResolver) ResolveEvidenceSource(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) (adapters.ResolvedEvidenceSource, error) {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject, ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1})
	if err != nil {
		return adapters.ResolvedEvidenceSource{}, err
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS, SourceID: resolver.sourceID, State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility})
	if err != nil {
		return adapters.ResolvedEvidenceSource{}, err
	}
	identity := evidence.IdentitySnapshot{Type: selection.SourceType, ID: resolver.sourceID, Fields: map[string]string{"display_name": "Evidence Sources VPS"}}
	return adapters.ResolvedEvidenceSource{Subject: identity, Source: identity, Authorization: authorization}, nil
}

type integrationMonitoringSource struct {
	host  adapters.MonitoringSeriesCapture
	probe adapters.MonitoringSeriesCapture
}

type integrationMonitoringEventSource struct {
	capture adapters.MonitoringEventCapture
}

func (source integrationMonitoringEventSource) LoadMonitoringEventEvidence(context.Context, string, string, evidence.TimeWindow) (adapters.MonitoringEventCapture, error) {
	return source.capture, nil
}

type integrationCommandAuditSource struct{ capture adapters.CommandAuditCapture }

func (source integrationCommandAuditSource) LoadCommandAuditEvidence(context.Context, string, evidence.TimeWindow) (adapters.CommandAuditCapture, error) {
	return source.capture, nil
}

type integrationSubscriptionCostSource struct {
	capture adapters.SubscriptionCostCapture
}

func (source integrationSubscriptionCostSource) LoadSubscriptionCostEvidence(context.Context, string, evidence.TimeWindow) (adapters.SubscriptionCostCapture, error) {
	return source.capture, nil
}

type integrationAssetHistorySource struct{ capture adapters.AssetHistoryCapture }

func (source integrationAssetHistorySource) LoadAssetHistory(context.Context, string, evidence.TimeWindow) (adapters.AssetHistoryCapture, error) {
	return source.capture, nil
}

func integrationEvidenceIntentID() (string, error) {
	return "evi_aaaaaaaaaaaaaaaaaaaaaaaa", nil
}

func captureIntegrationEvidence(t *testing.T, ctx context.Context, kind evidence.Kind, actor evidence.ActorScope, selection evidence.Selection) evidence.CanonicalSnapshot {
	t.Helper()
	preview, err := kind.PreviewCapture(ctx, actor, selection)
	if err != nil {
		t.Fatalf("%s PreviewCapture() error = %v", selection.Key, err)
	}
	snapshot, err := kind.Capture(ctx, actor, evidence.Intent{
		ID: preview.IntentID, Key: selection.Key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("integration-preview")), ValidUntil: preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("%s Capture() error = %v", selection.Key, err)
	}
	return snapshot
}

func (source integrationMonitoringSource) LoadMonitoringHostEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (adapters.MonitoringSeriesCapture, error) {
	return source.host, nil
}

func (source integrationMonitoringSource) LoadMonitoringProbeEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (adapters.MonitoringSeriesCapture, error) {
	return source.probe, nil
}

type integrationMonitoringResolver struct {
	kind     recordauth.SourceKind
	sourceID string
}

func (resolver integrationMonitoringResolver) ResolveEvidenceSource(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) (adapters.ResolvedEvidenceSource, error) {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject, ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1})
	if err != nil {
		return adapters.ResolvedEvidenceSource{}, err
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{Version: recordauth.SourceAuthorizationVersionV1, Kind: resolver.kind, SourceID: resolver.sourceID, State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility})
	if err != nil {
		return adapters.ResolvedEvidenceSource{}, err
	}
	identity := evidence.IdentitySnapshot{Type: selection.SourceType, ID: resolver.sourceID, Fields: map[string]string{"display_name": "Evidence source"}}
	return adapters.ResolvedEvidenceSource{Subject: identity, Source: identity, Authorization: authorization}, nil
}

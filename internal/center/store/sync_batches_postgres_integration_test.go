package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationSyncBatchLiveBackfillInterleaving(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "sync-live-backfill-interleaving", 4)
	for _, arrival := range []string{"live-first", "backfill-first"} {
		for _, initialLifecycle := range []string{monitoringinstances.LifecyclePendingEnrollment, monitoringinstances.LifecycleInUse} {
			name := arrival + "/" + initialLifecycle
			t.Run(name, func(t *testing.T) {
				prefix := fmt.Sprintf("interleave_%s_%s", strings.ReplaceAll(arrival, "-", "_"), lifecycleFixtureSuffix(initialLifecycle))
				seedSyncInterleavingFixture(t, ctx, fixture, prefix, initialLifecycle)

				repository := NewPostgresSyncRepository(runtimePool)
				receivedAt := syncInterleavingT2.Add(time.Second)
				repository.now = func() time.Time { return receivedAt }
				notifier := &syncInterleavingNotifier{}
				incidentProcessor := incidents.NewService(
					NewPostgresMonitoringInstanceRepository(runtimePool),
					NewPostgresTargetRepository(runtimePool),
					incidents.NewPostgresSnapshotReader(runtimePool),
					NewPostgresIncidentRepository(runtimePool),
					notifier,
					slog.New(slog.NewTextHandler(io.Discard, nil)),
					time.Hour,
					time.Hour,
				)
				service := syncing.NewService(repository, incidentProcessor)
				liveBatch, backfillBatch := syncInterleavingBatches(prefix)
				ordered := []syncing.Batch{liveBatch, backfillBatch}
				if arrival == "backfill-first" {
					ordered[0], ordered[1] = ordered[1], ordered[0]
				}

				firstResult := requireSyncInterleavingSuccess(t, ctx, service, ordered[0], syncing.ResultDispositionRecorded)
				if !firstResult.AcceptedAt.Equal(receivedAt) {
					t.Fatal("first accepted time mismatch")
				}
				firstProjection := readSyncInterleavingProjectionState(t, ctx, fixture, prefix)
				firstMonitoring := readSyncInterleavingMonitoringState(t, ctx, fixture, prefix)
				if firstMonitoring.lifecycle != monitoringinstances.LifecycleInUse ||
					!firstMonitoring.heartbeatAt.Equal(ordered[0].Heartbeats[0].ObservedAt) ||
					!firstMonitoring.syncAt.Equal(receivedAt) {
					t.Fatal("first recorded batch did not advance lifecycle and timestamps monotonically")
				}
				firstNotificationCount := len(notifier.messages)

				receivedAt = receivedAt.Add(time.Second)
				secondResult := requireSyncInterleavingSuccess(t, ctx, service, ordered[1], syncing.ResultDispositionRecorded)
				if !secondResult.AcceptedAt.Equal(receivedAt) {
					t.Fatal("second accepted time mismatch")
				}
				secondProjection := readSyncInterleavingProjectionState(t, ctx, fixture, prefix)
				secondMonitoring := readSyncInterleavingMonitoringState(t, ctx, fixture, prefix)
				secondRaw := readSyncInterleavingRawState(t, ctx, fixture, prefix)
				assertSyncInterleavingRawMultiset(t, secondRaw)
				assertSyncInterleavingConvergedState(t, ctx, fixture, prefix, initialLifecycle, receivedAt, secondProjection, notifier)
				if arrival == "live-first" {
					if firstProjection != secondProjection || firstNotificationCount != len(notifier.messages) {
						t.Fatal("older backfill changed the live incident projection")
					}
				} else if firstProjection.activeSignature != "" || firstProjection.eventSignature != "" || firstProjection.notificationSignature != "" {
					t.Fatal("older backfill created an incident projection before live facts arrived")
				}

				beforeDuplicateVersions := readSyncInterleavingProjectionVersions(t, ctx, fixture, prefix)
				receivedAt = receivedAt.Add(time.Second)
				requireSyncInterleavingSuccess(t, ctx, service, ordered[1], syncing.ResultDispositionExactDuplicate)
				afterDuplicateProjection := readSyncInterleavingProjectionState(t, ctx, fixture, prefix)
				afterDuplicateMonitoring := readSyncInterleavingMonitoringState(t, ctx, fixture, prefix)
				afterDuplicateVersions := readSyncInterleavingProjectionVersions(t, ctx, fixture, prefix)
				afterDuplicateRaw := readSyncInterleavingRawState(t, ctx, fixture, prefix)
				if afterDuplicateProjection != secondProjection || afterDuplicateVersions != beforeDuplicateVersions || afterDuplicateMonitoring != secondMonitoring {
					t.Fatal("exact duplicate changed the incident projection")
				}
				if afterDuplicateRaw != secondRaw || len(notifier.messages) != secondRaw.notificationCount {
					t.Fatal("exact duplicate appended raw facts, events, or notifications")
				}
			})
		}
	}
}

var (
	syncInterleavingT2 = time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	syncInterleavingT1 = syncInterleavingT2.Add(-10 * time.Minute)
)

type syncInterleavingNotifier struct {
	messages []string
}

func (n *syncInterleavingNotifier) Send(_ context.Context, message string) error {
	n.messages = append(n.messages, message)
	return nil
}

type syncInterleavingProjectionState struct {
	monitoringHealth      string
	monitoringActiveCount int
	monitoringSummary     string
	targetHealth          string
	targetActiveCount     int
	targetSummary         string
	activeSignature       string
	eventSignature        string
	notificationSignature string
}

type syncInterleavingProjectionVersions struct {
	monitoring string
	target     string
}

type syncInterleavingMonitoringState struct {
	lifecycle   string
	heartbeatAt time.Time
	syncAt      time.Time
}

type syncInterleavingRawState struct {
	syncBatchCount      int
	heartbeatCount      int
	heartbeatBackfilled int
	heartbeatBatchCount int
	hostCount           int
	hostBackfilled      int
	hostBatchCount      int
	probeCount          int
	probeBackfilled     int
	probeBatchCount     int
	ipQualityCount      int
	ipQualityBackfilled int
	ipQualityBatchCount int
	eventCount          int
	notificationCount   int
}

func lifecycleFixtureSuffix(lifecycle string) string {
	if lifecycle == monitoringinstances.LifecyclePendingEnrollment {
		return "pending"
	}
	return "in_use"
}

func seedSyncInterleavingFixture(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix, lifecycle string) {
	t.Helper()
	monitoringInstanceID := "mi_" + prefix
	targetID := "tg_" + prefix
	probeItemID := "pb_" + prefix
	vpsID := "vps_" + prefix
	if _, err := fixture.db.Exec(ctx, `
		insert into public.monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, binding_fingerprint, sync_token_hash
		) values ($1,$2,'','','',$3,$4,$5,$6,$7)`,
		monitoringInstanceID, prefix, lifecycle, monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingBound, "fp_"+prefix, hashSyncToken("token_"+prefix),
	); err != nil {
		t.Fatal("seed interleaving monitoring instance")
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.targets (target_id, name, target_type, host, run_status)
		values ($1,$2,'hostname','example.com',$3)`, targetID, prefix, targets.RunStatusEnabled); err != nil {
		t.Fatal("seed interleaving target")
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.probe_items (probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds)
		values ($1,$2,$3,$4,10)`, probeItemID, targetID, agentapi.ProbeKindHTTP, agentapi.FrequencyTier5m); err != nil {
		t.Fatal("seed interleaving probe item")
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, ipv4, lifecycle_status, usage_status)
		values ($1,$2,$3,'active','in_use')`, vpsID, prefix, syncInterleavingIPAddress(prefix)); err != nil {
		t.Fatal("seed interleaving VPS")
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id, note)
		values ($1,$2,$3,'')`, "vnl_"+prefix, vpsID, monitoringInstanceID); err != nil {
		t.Fatal("seed interleaving monitoring link")
	}
}

func syncInterleavingBatches(prefix string) (syncing.Batch, syncing.Batch) {
	monitoringInstanceID := "mi_" + prefix
	targetID := "tg_" + prefix
	probeItemID := "pb_" + prefix
	fingerprint := "fp_" + prefix
	liveBatchID := "sync_live_" + prefix
	backfillBatchID := "sync_backfill_" + prefix
	ipAddress := syncInterleavingIPAddress(prefix)
	liveProbes := make([]observations.ProbeObservationWrite, 0, 3)
	for index := 0; index < 3; index++ {
		liveProbes = append(liveProbes, observations.ProbeObservationWrite{
			MonitoringInstanceID: monitoringInstanceID,
			TargetID:             targetID,
			ProbeItemID:          probeItemID,
			ProbeKind:            agentapi.ProbeKindHTTP,
			ObservedAt:           syncInterleavingT2.Add(-time.Duration(index) * 30 * time.Second),
			AgentVersion:         "agent/live-v2",
			Fingerprint:          fingerprint,
			ResultKind:           agentapi.ProbeResultFailure,
			ErrorCode:            agentapi.ProbeErrorConnect,
			ErrorSummary:         "connection failed",
			SyncBatchID:          liveBatchID,
		})
	}
	live := syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            "token_" + prefix,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt: syncInterleavingT2, AgentVersion: "agent/live-v2", Fingerprint: fingerprint, SyncBatchID: liveBatchID,
		}},
		Observations: observations.BatchWrite{
			MonitoringInstanceID: monitoringInstanceID,
			HostSamples: []observations.HostSampleWrite{{
				MonitoringInstanceID: monitoringInstanceID, ObservedAt: syncInterleavingT2,
				AgentVersion: "agent/live-v2", Fingerprint: fingerprint, CPUUsagePct: 20,
				MemUsedPct: 40, MemAvailableBytes: 2 << 30, MemTotalBytes: 4 << 30,
				DiskUsedPct: 95, DiskTotalBytes: 100 << 30, InodeUsedPct: 10,
				UptimeSeconds: 3600, SyncBatchID: liveBatchID,
			}},
			ProbeObservations: liveProbes,
		},
		IPQualityReports: []ipquality.ReportWrite{{
			MonitoringInstanceID: monitoringInstanceID, ObservedAt: syncInterleavingT2,
			AgentVersion: "agent/live-v2", Fingerprint: fingerprint, SyncBatchID: liveBatchID,
			IPAddress: ipAddress, IPVersion: 4, Status: agentapi.IPQualityStatusSuccess, RiskLevel: "low",
		}},
	}
	backfill := syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            "token_" + prefix,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt: syncInterleavingT1, AgentVersion: "agent/backfill-v1", Fingerprint: fingerprint, SyncBatchID: backfillBatchID, IsBackfilled: true,
		}},
		Observations: observations.BatchWrite{
			MonitoringInstanceID: monitoringInstanceID,
			HostSamples: []observations.HostSampleWrite{{
				MonitoringInstanceID: monitoringInstanceID, ObservedAt: syncInterleavingT1,
				AgentVersion: "agent/backfill-v1", Fingerprint: fingerprint, CPUUsagePct: 10,
				MemUsedPct: 20, MemAvailableBytes: 3 << 30, MemTotalBytes: 4 << 30,
				DiskUsedPct: 10, DiskTotalBytes: 100 << 30, InodeUsedPct: 5,
				UptimeSeconds: 3000, IsBackfilled: true, SyncBatchID: backfillBatchID,
			}},
			ProbeObservations: []observations.ProbeObservationWrite{{
				MonitoringInstanceID: monitoringInstanceID, TargetID: targetID, ProbeItemID: probeItemID,
				ProbeKind: agentapi.ProbeKindHTTP, ObservedAt: syncInterleavingT1,
				AgentVersion: "agent/backfill-v1", Fingerprint: fingerprint,
				ResultKind: agentapi.ProbeResultSuccess, IsBackfilled: true, SyncBatchID: backfillBatchID,
			}},
		},
		IPQualityReports: []ipquality.ReportWrite{{
			MonitoringInstanceID: monitoringInstanceID, ObservedAt: syncInterleavingT1,
			AgentVersion: "agent/backfill-v1", Fingerprint: fingerprint, SyncBatchID: backfillBatchID,
			IPAddress: ipAddress, IPVersion: 4, Status: agentapi.IPQualityStatusSuccess,
			RiskLevel: "high", IsBackfilled: true,
		}},
	}
	return live, backfill
}

func syncInterleavingIPAddress(prefix string) string {
	if strings.Contains(prefix, "backfill_first") {
		if strings.HasSuffix(prefix, "pending") {
			return "203.0.113.41"
		}
		return "203.0.113.42"
	}
	if strings.HasSuffix(prefix, "pending") {
		return "203.0.113.31"
	}
	return "203.0.113.32"
}

func requireSyncInterleavingSuccess(t *testing.T, ctx context.Context, service *syncing.Service, batch syncing.Batch, want syncing.ResultDisposition) syncing.Result {
	t.Helper()
	result, err := service.SyncBatch(ctx, batch)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			t.Fatalf("interleaving sync SQLSTATE = %s", postgresError.Code)
		}
		t.Fatal("interleaving sync failed")
	}
	if result.Disposition != want {
		t.Fatalf("sync disposition = %q, want %q", result.Disposition, want)
	}
	return result
}

func readSyncInterleavingProjectionState(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix string) syncInterleavingProjectionState {
	t.Helper()
	var state syncInterleavingProjectionState
	if err := fixture.db.QueryRow(ctx, `
		select mi.current_health_status,
			mi.current_active_incident_count,
			mi.current_primary_issue_summary,
			t.current_health_status,
			t.current_active_incident_count,
			t.current_primary_issue_summary,
			coalesce((
				select string_agg(concat_ws(':', object_type, object_id, incident_class, severity, status, source_summary), E'\n' order by object_type, object_id, incident_class)
				from active_incidents
				where (object_type = 'monitoring_instance' and object_id = mi.monitoring_instance_id)
					or (object_type = 'target' and object_id = t.target_id)
			), ''),
			coalesce((
				select string_agg(concat_ws(':', object_type, object_id, coalesce(payload->>'incident_class', ''), event_type, coalesce(severity, ''), summary, coalesce(payload->>'is_backfilled', '')), E'\n' order by object_type, object_id, coalesce(payload->>'incident_class', ''), event_type, event_id)
				from state_change_events
				where (object_type = 'monitoring_instance' and object_id = mi.monitoring_instance_id)
					or (object_type = 'target' and object_id = t.target_id)
			), ''),
			coalesce((
				select string_agg(concat_ws(':', object_type, object_id, channel, delivery_status, summary), E'\n' order by object_type, object_id, channel, notification_id)
				from notification_records
				where (object_type = 'monitoring_instance' and object_id = mi.monitoring_instance_id)
					or (object_type = 'target' and object_id = t.target_id)
			), '')
		from monitoring_instances mi
		join targets t on t.target_id = $2
		where mi.monitoring_instance_id = $1`, "mi_"+prefix, "tg_"+prefix).Scan(
		&state.monitoringHealth,
		&state.monitoringActiveCount,
		&state.monitoringSummary,
		&state.targetHealth,
		&state.targetActiveCount,
		&state.targetSummary,
		&state.activeSignature,
		&state.eventSignature,
		&state.notificationSignature,
	); err != nil {
		t.Fatal("read interleaving incident projection")
	}
	return state
}

func readSyncInterleavingProjectionVersions(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix string) syncInterleavingProjectionVersions {
	t.Helper()
	var versions syncInterleavingProjectionVersions
	if err := fixture.db.QueryRow(ctx, `
		select mi.xmin::text, t.xmin::text
		from monitoring_instances mi
		join targets t on t.target_id = $2
		where mi.monitoring_instance_id = $1`, "mi_"+prefix, "tg_"+prefix).Scan(&versions.monitoring, &versions.target); err != nil {
		t.Fatal("read interleaving projection versions")
	}
	return versions
}

func readSyncInterleavingMonitoringState(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix string) syncInterleavingMonitoringState {
	t.Helper()
	var state syncInterleavingMonitoringState
	if err := fixture.db.QueryRow(ctx, `
		select lifecycle_status, last_heartbeat_at, last_sync_at
		from monitoring_instances
		where monitoring_instance_id = $1`, "mi_"+prefix).Scan(&state.lifecycle, &state.heartbeatAt, &state.syncAt); err != nil {
		t.Fatal("read interleaving monitoring state")
	}
	return state
}

func readSyncInterleavingRawState(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix string) syncInterleavingRawState {
	t.Helper()
	var state syncInterleavingRawState
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*)::int from agent_sync_batches where monitoring_instance_id = $1),
			(select count(*)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1),
			(select count(*) filter (where is_backfilled)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1),
			(select count(distinct sync_batch_id)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1),
			(select count(*)::int from host_samples where monitoring_instance_id = $1),
			(select count(*) filter (where is_backfilled)::int from host_samples where monitoring_instance_id = $1),
			(select count(distinct sync_batch_id)::int from host_samples where monitoring_instance_id = $1),
			(select count(*)::int from probe_observations where monitoring_instance_id = $1),
			(select count(*) filter (where is_backfilled)::int from probe_observations where monitoring_instance_id = $1),
			(select count(distinct sync_batch_id)::int from probe_observations where monitoring_instance_id = $1),
			(select count(*)::int from ip_quality_reports where monitoring_instance_id = $1),
			(select count(*) filter (where is_backfilled)::int from ip_quality_reports where monitoring_instance_id = $1),
			(select count(distinct sync_batch_id)::int from ip_quality_reports where monitoring_instance_id = $1),
			(select count(*)::int from state_change_events where (object_type = 'monitoring_instance' and object_id = $1) or (object_type = 'target' and object_id = $2)),
			(select count(*)::int from notification_records where (object_type = 'monitoring_instance' and object_id = $1) or (object_type = 'target' and object_id = $2))`,
		"mi_"+prefix, "tg_"+prefix,
	).Scan(
		&state.syncBatchCount,
		&state.heartbeatCount, &state.heartbeatBackfilled, &state.heartbeatBatchCount,
		&state.hostCount, &state.hostBackfilled, &state.hostBatchCount,
		&state.probeCount, &state.probeBackfilled, &state.probeBatchCount,
		&state.ipQualityCount, &state.ipQualityBackfilled, &state.ipQualityBatchCount,
		&state.eventCount, &state.notificationCount,
	); err != nil {
		t.Fatal("read interleaving raw state")
	}
	return state
}

func assertSyncInterleavingRawMultiset(t *testing.T, state syncInterleavingRawState) {
	t.Helper()
	if state.syncBatchCount != 2 ||
		state.heartbeatCount != 2 || state.heartbeatBackfilled != 1 || state.heartbeatBatchCount != 2 ||
		state.hostCount != 2 || state.hostBackfilled != 1 || state.hostBatchCount != 2 ||
		state.probeCount != 4 || state.probeBackfilled != 1 || state.probeBatchCount != 2 ||
		state.ipQualityCount != 2 || state.ipQualityBackfilled != 1 || state.ipQualityBatchCount != 2 {
		t.Fatalf("raw multiset counts = %#v, want two unique live/backfill batches", state)
	}
}

func assertSyncInterleavingConvergedState(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix, initialLifecycle string, lastRecordedReceive time.Time, projection syncInterleavingProjectionState, notifier *syncInterleavingNotifier) {
	t.Helper()
	monitoringInstanceID := "mi_" + prefix
	targetID := "tg_" + prefix
	vpsID := "vps_" + prefix
	var lifecycle string
	var heartbeatAt, syncAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select lifecycle_status, last_heartbeat_at, last_sync_at
		from monitoring_instances
		where monitoring_instance_id = $1`, monitoringInstanceID).Scan(&lifecycle, &heartbeatAt, &syncAt); err != nil {
		t.Fatal("read interleaving monitoring state")
	}
	if lifecycle != monitoringinstances.LifecycleInUse || !heartbeatAt.Equal(syncInterleavingT2) || !syncAt.Equal(lastRecordedReceive) {
		t.Fatal("monitoring lifecycle or timestamps did not converge monotonically")
	}
	if initialLifecycle != monitoringinstances.LifecyclePendingEnrollment && initialLifecycle != lifecycle {
		t.Fatal("non-pending lifecycle regressed")
	}
	if projection.monitoringActiveCount != 1 || projection.targetActiveCount != 1 ||
		!strings.Contains(projection.activeSignature, string(incidents.IncidentMonitoringInstanceDiskPressure)) ||
		!strings.Contains(projection.activeSignature, string(incidents.IncidentTargetProbeFailure)) ||
		projection.eventSignature == "" || projection.notificationSignature == "" ||
		len(notifier.messages) != 2 {
		t.Fatalf("incident projection did not converge: %#v notifications=%d", projection, len(notifier.messages))
	}

	runtimeRepository := NewPostgresRuntimeFactsRepository(fixture.db)
	hostFacts, err := runtimeRepository.GetMonitoringInstanceRuntimeFacts(ctx, monitoringInstanceID, runtimefacts.WindowRequest{
		Key: "interleaving", StartedAt: syncInterleavingT1.Add(-time.Minute), EndedAt: syncInterleavingT2.Add(time.Minute), BucketCount: 1,
	})
	if err != nil {
		t.Fatal("read interleaving latest host")
	}
	if hostFacts.LatestHostSample == nil || hostFacts.LatestHostSample.AgentVersion != "agent/live-v2" || hostFacts.LatestHostSample.IsBackfilled {
		t.Fatal("latest host did not converge to live T2")
	}
	probeFacts, err := runtimeRepository.GetTargetRuntimeFacts(ctx, targetID, syncInterleavingT1.Add(-time.Minute), 10)
	if err != nil {
		t.Fatal("read interleaving latest probe")
	}
	if len(probeFacts.LatestProbeObservations) != 1 || probeFacts.LatestProbeObservations[0].AgentVersion != "agent/live-v2" || probeFacts.LatestProbeObservations[0].IsBackfilled {
		t.Fatal("latest probe did not converge to live T2")
	}
	monitoringSubject, err := NewPostgresMonitoringInstanceRepository(fixture.db).loadMonitoringRecordSubject(ctx, monitoringInstanceID)
	if err != nil || monitoringSubject.AgentVersion != "agent/live-v2" {
		t.Fatal("latest heartbeat agent version did not converge to live T2")
	}
	report, found, err := NewPostgresIPQualityRepository(fixture.db).latestReportForVPS(ctx, vpsID)
	if err != nil || !found || report.AgentVersion != "agent/live-v2" || report.IsBackfilled {
		t.Fatal("latest IP quality did not converge to live T2")
	}
}

func TestPostgresIntegrationReplaySafeLatestStoreConsumers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	var serverMajor int
	if err := fixture.db.QueryRow(ctx, `select pg_catalog.current_setting('server_version_num')::int / 10000`).Scan(&serverMajor); err != nil {
		t.Fatalf("read PostgreSQL server version: %v", err)
	}
	if serverMajor != 16 {
		t.Fatalf("PostgreSQL server major = %d, want 16", serverMajor)
	}

	for index, arrival := range []string{"backfill-first", "live-first"} {
		t.Run(arrival, func(t *testing.T) {
			prefix := fmt.Sprintf("latest_%d", index+1)
			seedReplaySafeLatestFixture(t, ctx, fixture, prefix, arrival == "backfill-first")
			assertReplaySafeLatestStoreConsumers(t, ctx, fixture, prefix)
		})
	}
}

type replaySafeLatestCandidate struct {
	stableID     int64
	reportSuffix string
	marker       string
	receivedAt   time.Time
	backfilled   bool
	ipAddress    string
}

func seedReplaySafeLatestFixture(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix string, backfillFirst bool) {
	t.Helper()
	monitoringInstanceID := "mi_" + prefix
	targetID := "tg_" + prefix
	probeItemID := "pb_" + prefix
	vpsID := "vps_" + prefix
	observedAt := time.Date(2026, time.August, 30, 6, 0, 0, 0, time.UTC)
	baseID := int64(1000)
	if prefix == "latest_2" {
		baseID = 2000
	}
	candidates := []replaySafeLatestCandidate{
		{stableID: baseID + 99, reportSuffix: "z", marker: "backfill-late-high-key", receivedAt: observedAt.Add(10 * time.Minute), backfilled: true, ipAddress: "203.0.113.99"},
		{stableID: baseID + 98, reportSuffix: "y", marker: "live-old-received-high-key", receivedAt: observedAt.Add(time.Minute), ipAddress: "203.0.113.98"},
		{stableID: baseID + 3, reportSuffix: "c", marker: "live-new-low-key", receivedAt: observedAt.Add(2 * time.Minute), ipAddress: "203.0.113.3"},
		{stableID: baseID + 5, reportSuffix: "e", marker: "winner", receivedAt: observedAt.Add(2 * time.Minute), ipAddress: "203.0.113.5"},
		{stableID: baseID + 4, reportSuffix: "d", marker: "live-new-mid-key", receivedAt: observedAt.Add(2 * time.Minute), ipAddress: "203.0.113.4"},
	}
	if !backfillFirst {
		for left, right := 0, len(candidates)-1; left < right; left, right = left+1, right-1 {
			candidates[left], candidates[right] = candidates[right], candidates[left]
		}
	}

	if _, err := fixture.db.Exec(ctx, `
		insert into public.monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status
		) values ($1, $2, '', '', '', '在用')`, monitoringInstanceID, prefix); err != nil {
		t.Fatalf("seed monitoring instance: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, ipv4, lifecycle_status, usage_status)
		values ($1, $2, '203.0.113.5', 'active', 'in_use')`, vpsID, prefix); err != nil {
		t.Fatalf("seed VPS: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id, note)
		values ($1, $2, $3, '')`, "vnl_"+prefix, vpsID, monitoringInstanceID); err != nil {
		t.Fatalf("seed monitoring link: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.targets (target_id, name, target_type, host, run_status)
		values ($1, $2, 'hostname', 'example.com', 'enabled')`, targetID, prefix); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.probe_items (probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds)
		values ($1, $2, 'http', '5m', 10)`, probeItemID, targetID); err != nil {
		t.Fatalf("seed probe item: %v", err)
	}

	for _, candidate := range candidates {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.host_samples (
				id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
				cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
				swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec, net_out_bytes_per_sec,
				cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
				disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
			) values ($1,$2,$3,$4,$5,'fixture',25,0.5,0.7,0.9,55,1000000,0,40,10,100,200,2,1,10,20,30,1000,false,$6,$7)`,
			candidate.stableID, monitoringInstanceID, observedAt, candidate.receivedAt, candidate.marker, candidate.backfilled, "sync_"+prefix+candidate.reportSuffix,
		); err != nil {
			t.Fatalf("seed host candidate %q: %v", candidate.marker, err)
		}
		if _, err := fixture.db.Exec(ctx, `
			insert into public.probe_observations (
				id, monitoring_instance_id, target_id, probe_item_id, observed_at, received_at,
				agent_version, fingerprint, result_kind, latency_ms, maintenance_context, is_backfilled, sync_batch_id
			) values ($1,$2,$3,$4,$5,$6,$7,'fixture','success',120,false,$8,$9)`,
			candidate.stableID, monitoringInstanceID, targetID, probeItemID, observedAt, candidate.receivedAt, candidate.marker, candidate.backfilled, "sync_"+prefix+candidate.reportSuffix,
		); err != nil {
			t.Fatalf("seed probe candidate %q: %v", candidate.marker, err)
		}
		if _, err := fixture.db.Exec(ctx, `
			insert into public.monitoring_instance_heartbeats (
				id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled
			) values ($1,$2,$3,$4,$5,'fixture',$6,$7)`,
			candidate.stableID, monitoringInstanceID, observedAt, candidate.receivedAt, candidate.marker, "sync_"+prefix+candidate.reportSuffix, candidate.backfilled,
		); err != nil {
			t.Fatalf("seed heartbeat candidate %q: %v", candidate.marker, err)
		}
		if _, err := fixture.db.Exec(ctx, `
			insert into public.ip_quality_reports (
				report_id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
				sync_batch_id, ip_address, ip_version, status, risk_level, is_backfilled
			) values ($1,$2,$3,$4,$5,'fixture',$6,$7,4,'success',$5,$8)`,
			"ipq_"+prefix+"_"+candidate.reportSuffix, monitoringInstanceID, observedAt, candidate.receivedAt, candidate.marker,
			"sync_"+prefix+candidate.reportSuffix, candidate.ipAddress, candidate.backfilled,
		); err != nil {
			t.Fatalf("seed IP-quality candidate %q: %v", candidate.marker, err)
		}
	}
}

func assertReplaySafeLatestStoreConsumers(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix string) {
	t.Helper()
	monitoringInstanceID := "mi_" + prefix
	targetID := "tg_" + prefix
	vpsID := "vps_" + prefix
	observedAt := time.Date(2026, time.August, 30, 6, 0, 0, 0, time.UTC)

	runtimeRepository := NewPostgresRuntimeFactsRepository(fixture.db)
	hostFacts, err := runtimeRepository.GetMonitoringInstanceRuntimeFacts(ctx, monitoringInstanceID, runtimefacts.WindowRequest{
		Key: "latest-ordering", StartedAt: observedAt.Add(-time.Hour), EndedAt: observedAt.Add(time.Hour), BucketCount: 1,
	})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts: %v", err)
	}
	if hostFacts.LatestHostSample == nil || hostFacts.LatestHostSample.AgentVersion != "winner" {
		t.Fatalf("latest host = %#v, want stable-key winner", hostFacts.LatestHostSample)
	}
	probeFacts, err := runtimeRepository.GetTargetRuntimeFacts(ctx, targetID, observedAt.Add(-time.Hour), 20)
	if err != nil {
		t.Fatalf("GetTargetRuntimeFacts: %v", err)
	}
	if len(probeFacts.LatestProbeObservations) != 1 || probeFacts.LatestProbeObservations[0].AgentVersion != "winner" {
		t.Fatalf("latest probes = %#v, want stable-key winner", probeFacts.LatestProbeObservations)
	}

	monitoringSubject, err := NewPostgresMonitoringInstanceRepository(fixture.db).loadMonitoringRecordSubject(ctx, monitoringInstanceID)
	if err != nil {
		t.Fatalf("loadMonitoringRecordSubject: %v", err)
	}
	if monitoringSubject.AgentVersion != "winner" {
		t.Fatalf("heartbeat agent version = %q, want winner", monitoringSubject.AgentVersion)
	}

	ipRepository := NewPostgresIPQualityRepository(fixture.db)
	summaries, err := ipRepository.ListLatestSummariesForVPS(ctx, []string{vpsID})
	if err != nil {
		t.Fatalf("ListLatestSummariesForVPS: %v", err)
	}
	wantReportID := "ipq_" + prefix + "_e"
	if summaries[vpsID].ReportID != wantReportID {
		t.Fatalf("latest IP summary report = %q, want %q", summaries[vpsID].ReportID, wantReportID)
	}
	report, found, err := ipRepository.latestReportForVPS(ctx, vpsID)
	if err != nil {
		t.Fatalf("latestReportForVPS: %v", err)
	}
	if !found || report.ReportID != wantReportID {
		t.Fatalf("latest IP report = %#v found=%t, want %q", report, found, wantReportID)
	}

	facts, err := NewPostgresAssetDecisionRepository(fixture.db).loadFacts(ctx)
	if err != nil {
		t.Fatalf("asset decision loadFacts: %v", err)
	}
	for _, fact := range facts {
		if fact.VPS.VPSID != vpsID {
			continue
		}
		if fact.VPS.IPQualitySummary == nil || fact.VPS.IPQualitySummary.IPAddress != "203.0.113.5" || fact.VPS.IPQualitySummary.RiskLevel != "winner" {
			t.Fatalf("asset decision IP summary = %#v, want canonical winner", fact.VPS.IPQualitySummary)
		}
		return
	}
	t.Fatalf("asset decision facts = %#v, want VPS %q", facts, vpsID)
}

func TestPostgresIntegrationAgentSyncBatchRuntimeACL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	assertAgentSyncBatchPostgresSchemaContract(t, ctx, fixture)
	assertAgentSyncBatchRuntimePrivileges(t, ctx, fixture)

	const (
		monitoringInstanceID = "mi_sync_batch_acl"
		syncBatchID          = "sync_batch_acl"
		syncToken            = "sync-token-acl-fixture"
		fingerprint          = "fingerprint-acl-fixture"
	)
	firstReceivedAt := time.Date(2026, time.August, 30, 3, 30, 0, 0, time.UTC)
	duplicateReceivedAt := firstReceivedAt.Add(time.Minute)
	heartbeatAt := firstReceivedAt.Add(-time.Minute)

	if _, err := fixture.db.Exec(ctx, `
		insert into public.monitoring_instances (
			monitoring_instance_id,
			display_name,
			region,
			city,
			provider,
			lifecycle_status,
			monitoring_status,
			binding_status,
			binding_fingerprint,
			sync_token_hash
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		monitoringInstanceID,
		"Sync batch ACL fixture",
		"",
		"",
		"",
		monitoringinstances.LifecyclePendingEnrollment,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingBound,
		fingerprint,
		hashSyncToken(syncToken),
	); err != nil {
		t.Fatal("seed bound monitoring instance")
	}

	runtimePool := fixture.openDirectRuntimePool(t, ctx, "sync-batch-runtime-acl", 1)
	assertExplicitAgentSyncBatchConflictTargetRejected(t, ctx, runtimePool, monitoringInstanceID)
	repository := NewPostgresSyncRepository(runtimePool)
	receivedAt := firstReceivedAt
	repository.now = func() time.Time { return receivedAt }
	batch := syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            syncToken,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   heartbeatAt,
			AgentVersion: "agent/sync-batch-acl",
			Fingerprint:  fingerprint,
			SyncBatchID:  syncBatchID,
		}},
	}

	requireAgentSyncBatchApplySuccess(t, ctx, repository, batch, "first")
	receivedAt = duplicateReceivedAt
	duplicateResult := requireAgentSyncBatchApplySuccess(t, ctx, repository, batch, "duplicate")
	if duplicateResult.Plan.HostSampleFrequencyTier != "" ||
		duplicateResult.Plan.HostSampleMaintenanceContext ||
		duplicateResult.Plan.IPQualityPlan != nil ||
		duplicateResult.Plan.PendingAction != nil ||
		duplicateResult.Plan.ProbeAssignments == nil ||
		len(duplicateResult.Plan.ProbeAssignments) != 0 {
		t.Fatal("duplicate ApplyBatch returned a non-empty or nil-normalized plan")
	}

	var batchCount, heartbeatCount int64
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*) from public.agent_sync_batches where monitoring_instance_id = $1 and sync_batch_id = $2),
			(select count(*) from public.monitoring_instance_heartbeats where monitoring_instance_id = $1 and sync_batch_id = $2)`,
		monitoringInstanceID,
		syncBatchID,
	).Scan(&batchCount, &heartbeatCount); err != nil {
		t.Fatal("read sync batch and heartbeat counts")
	}
	if batchCount != 1 || heartbeatCount != 1 {
		t.Fatalf("persisted row counts = batch:%d heartbeat:%d, want batch:1 heartbeat:1", batchCount, heartbeatCount)
	}

	var storedHeartbeatAt, storedSyncAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select last_heartbeat_at, last_sync_at
		from public.monitoring_instances
		where monitoring_instance_id = $1`, monitoringInstanceID).Scan(&storedHeartbeatAt, &storedSyncAt); err != nil {
		t.Fatal("read monitoring instance sync timestamps")
	}
	if !storedHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("last_heartbeat_at = %s, want %s", storedHeartbeatAt.UTC().Format(time.RFC3339Nano), heartbeatAt.Format(time.RFC3339Nano))
	}
	if !storedSyncAt.Equal(firstReceivedAt) {
		t.Fatalf("last_sync_at = %s, want first receive time %s after duplicate at %s", storedSyncAt.UTC().Format(time.RFC3339Nano), firstReceivedAt.Format(time.RFC3339Nano), duplicateReceivedAt.Format(time.RFC3339Nano))
	}

	assertAgentSyncBatchRuntimePrivileges(t, ctx, fixture)
}

func assertAgentSyncBatchPostgresSchemaContract(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()

	var serverMajor, uniqueIndexCount, expectedPrimaryKeyCount int
	if err := fixture.db.QueryRow(ctx, `
		select
			pg_catalog.current_setting('server_version_num')::int / 10000,
			(select count(*)::int
			 from pg_catalog.pg_index index_catalog
			 where index_catalog.indrelid = 'public.agent_sync_batches'::pg_catalog.regclass
			   and index_catalog.indisunique),
			(select count(*)::int
			 from pg_catalog.pg_constraint constraint_catalog
			 where constraint_catalog.conrelid = 'public.agent_sync_batches'::pg_catalog.regclass
			   and constraint_catalog.contype = 'p'
			   and pg_catalog.pg_get_constraintdef(constraint_catalog.oid) =
			       'PRIMARY KEY (monitoring_instance_id, sync_batch_id)')`,
	).Scan(&serverMajor, &uniqueIndexCount, &expectedPrimaryKeyCount); err != nil {
		t.Fatal("read agent_sync_batches PostgreSQL schema contract")
	}
	if serverMajor != 16 {
		t.Fatalf("PostgreSQL server major = %d, want 16", serverMajor)
	}
	if uniqueIndexCount != 1 || expectedPrimaryKeyCount != 1 {
		t.Fatalf(
			"agent_sync_batches unique arbiter catalog = indexes:%d expected-primary-key:%d, want indexes:1 expected-primary-key:1",
			uniqueIndexCount,
			expectedPrimaryKeyCount,
		)
	}
}

func assertExplicitAgentSyncBatchConflictTargetRejected(
	t *testing.T,
	ctx context.Context,
	runtimePool interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	monitoringInstanceID string,
) {
	t.Helper()

	_, err := runtimePool.Exec(ctx, `
		insert into public.agent_sync_batches (monitoring_instance_id, sync_batch_id)
		values ($1, 'sync_batch_explicit_target_probe')
		on conflict (monitoring_instance_id, sync_batch_id) do nothing`, monitoringInstanceID)
	if err == nil {
		t.Fatal("explicit agent_sync_batches conflict target succeeded under INSERT-only runtime, want SQLSTATE 42501")
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatal("explicit agent_sync_batches conflict target failed without PostgreSQL typed cause")
	}
	if postgresError.Code != "42501" {
		t.Fatalf("explicit agent_sync_batches conflict target SQLSTATE = %s, want 42501", postgresError.Code)
	}
}

func assertAgentSyncBatchRuntimePrivileges(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()

	var insertPrivilege, selectPrivilege, updatePrivilege, deletePrivilege bool
	var runtimeColumnACLCount int
	if err := fixture.db.QueryRow(ctx, `
		select
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'INSERT'),
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'SELECT'),
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'UPDATE'),
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'DELETE'),
			(select count(*)::int
			 from pg_catalog.pg_attribute attribute
			 cross join lateral pg_catalog.aclexplode(attribute.attacl) acl_entry
			 where attribute.attrelid = 'public.agent_sync_batches'::pg_catalog.regclass
			   and attribute.attnum > 0
			   and not attribute.attisdropped
			   and acl_entry.grantee = (
			       select role.oid from pg_catalog.pg_roles role where role.rolname = $1::name
			   ))`,
		fixture.runtime,
	).Scan(&insertPrivilege, &selectPrivilege, &updatePrivilege, &deletePrivilege, &runtimeColumnACLCount); err != nil {
		t.Fatal("read agent_sync_batches runtime privilege vector")
	}
	if !insertPrivilege || selectPrivilege || updatePrivilege || deletePrivilege || runtimeColumnACLCount != 0 {
		t.Fatalf(
			"agent_sync_batches runtime privileges = insert:%t select:%t update:%t delete:%t column-acl-entries:%d, want insert:true select:false update:false delete:false column-acl-entries:0",
			insertPrivilege,
			selectPrivilege,
			updatePrivilege,
			deletePrivilege,
			runtimeColumnACLCount,
		)
	}
}

func requireAgentSyncBatchApplySuccess(
	t *testing.T,
	ctx context.Context,
	repository *PostgresSyncRepository,
	batch syncing.Batch,
	phase string,
) syncing.Result {
	t.Helper()

	result, err := repository.ApplyBatch(ctx, batch)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			t.Fatalf("%s ApplyBatch PostgreSQL SQLSTATE = %s, want success", phase, postgresError.Code)
		}
		t.Fatalf("%s ApplyBatch failed without PostgreSQL typed cause", phase)
	}
	return result
}

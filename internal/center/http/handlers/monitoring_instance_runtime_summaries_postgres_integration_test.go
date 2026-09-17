package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/observations"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationMonitoringInstanceRuntimeSummariesUseCurrentBindingAndDeterministicLatest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openTemporaryMonitoringSparklinesDatabase(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	epoch := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
	insertRuntimeSummaryMonitoringInstance(t, ctx, db, "mi_runtime_live", "fp-current", epoch, false)
	insertRuntimeSummaryMonitoringInstance(t, ctx, db, "mi_runtime_zero", "fp-zero", epoch, false)
	insertRuntimeSummaryMonitoringInstance(t, ctx, db, "mi_runtime_empty", "fp-empty", epoch, false)
	insertRuntimeSummaryMonitoringInstance(t, ctx, db, "mi_runtime_archived", "fp-archived", epoch, true)
	insertRuntimeSummaryMonitoringInstance(t, ctx, db, "mi_runtime_legacy", "fp-legacy", epoch, false)

	now := time.Now().UTC().Truncate(time.Microsecond)
	// Prior identity and prior binding epoch observations must not leak into the
	// current summary even when they would otherwise sort ahead of live data.
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_live", now.Add(-time.Minute), now.Add(-time.Minute), "fp-old", 11, 11, 11, true, false, "old-identity")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_live", now.Add(-30*time.Second), epoch.Add(-time.Second), "fp-current", 22, 22, 22, true, false, "old-epoch")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_live", now.Add(time.Hour), now.Add(time.Hour), "fp-current", 33, 33, 33, true, false, "future")

	observedAt := now.Add(-2 * time.Minute)
	receivedAt := now.Add(-90 * time.Second)
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_live", observedAt, receivedAt, "fp-current", 101, 1, 1, true, true, "backfilled")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_live", observedAt, receivedAt.Add(-time.Second), "fp-current", 202, 2, 2, true, false, "received-older")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_live", observedAt, receivedAt, "fp-current", 303, 3, 3, true, false, "received-newer-id-1")
	// Same observed_at, is_backfilled, and received_at: the final
	// deterministic tie-breaker is exercised through the HTTP ingest path.
	networkRatesValid := false
	ingestPayload, err := json.Marshal(agentapi.SyncRequest{
		MonitoringInstanceID: "mi_runtime_live",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "test-agent",
			Fingerprint:  "fp-current",
			SyncBatchID:  "http-ingest",
		}},
		HostSamples: []agentapi.HostSamplePayload{{
			ObservedAt:        observedAt,
			AgentVersion:      "test-agent",
			Fingerprint:       "fp-current",
			SyncBatchID:       "http-ingest",
			UptimeSeconds:     404,
			NetInBytesPerSec:  4,
			NetOutBytesPerSec: 4,
			NetworkRatesValid: &networkRatesValid,
		}},
	})
	if err != nil {
		t.Fatalf("marshal HTTP ingest payload: %v", err)
	}
	observationService := observations.NewService(store.NewPostgresObservationRepository(db), nil)
	ingestHandler := AgentSync(&runtimeSummaryHTTPObservationSyncService{
		service:    observationService,
		receivedAt: receivedAt,
	})
	ingestRequest := httptest.NewRequest(
		http.MethodPost,
		agentapi.SyncPath,
		strings.NewReader(string(ingestPayload)),
	)
	ingestRequest.Header.Set("Authorization", "Bearer runtime-summary-sync-token")
	ingestRequest.Header.Set("Content-Type", "application/json")
	ingestRecorder := httptest.NewRecorder()
	ingestHandler.ServeHTTP(ingestRecorder, ingestRequest)
	if ingestRecorder.Code != http.StatusOK {
		t.Fatalf("HTTP ingest status = %d, want %d; body=%s", ingestRecorder.Code, http.StatusOK, ingestRecorder.Body.String())
	}
	var ingestResponse agentapi.SyncResponse
	if err := json.Unmarshal(ingestRecorder.Body.Bytes(), &ingestResponse); err != nil {
		t.Fatalf("decode HTTP ingest response: %v", err)
	}
	if ingestResponse.Status != "accepted" {
		t.Fatalf("HTTP ingest response status = %q, want accepted", ingestResponse.Status)
	}
	var persistedNetworkRatesValid bool
	if err := db.QueryRow(ctx, `
		select network_rates_valid
		from host_samples
		where monitoring_instance_id = $1 and sync_batch_id = $2
		order by id desc
		limit 1
	`, "mi_runtime_live", "http-ingest").Scan(&persistedNetworkRatesValid); err != nil {
		t.Fatalf("read persisted network_rates_valid: %v", err)
	}
	if persistedNetworkRatesValid {
		t.Fatal("persisted network_rates_valid = true, want false")
	}
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_legacy", now.Add(-time.Minute), now.Add(-time.Minute), "fp-legacy", 909, 909, 808, nil, false, "legacy-null")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_zero", now.Add(-time.Minute), now.Add(-time.Minute), "fp-zero", 0, 0, 0, true, false, "zero")

	repository := store.NewPostgresMonitoringInstanceRuntimeSummariesRepository(db)
	result, err := repository.GetMonitoringInstanceRuntimeSummaries(ctx)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeSummaries: %v", err)
	}
	if result.MonitoringInstances["mi_runtime_archived"] != nil {
		t.Fatalf("archived instance unexpectedly returned: %#v", result.MonitoringInstances["mi_runtime_archived"])
	}
	live := result.MonitoringInstances["mi_runtime_live"]
	if live == nil || live.UptimeSeconds != 404 || !live.ObservedAt.Equal(observedAt) || !live.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("live summary = %#v, want id-desc latest current-binding row", live)
	}
	if live.NetInBytesPerSec != nil || live.NetOutBytesPerSec != nil {
		t.Fatalf("invalid network marker produced rates: %#v", live)
	}
	if result.MonitoringInstances["mi_runtime_empty"] != nil {
		t.Fatalf("missing sample = %#v, want nil", result.MonitoringInstances["mi_runtime_empty"])
	}
	zero := result.MonitoringInstances["mi_runtime_zero"]
	if zero == nil || zero.NetInBytesPerSec == nil || *zero.NetInBytesPerSec != 0 || zero.NetOutBytesPerSec == nil || *zero.NetOutBytesPerSec != 0 {
		t.Fatalf("valid zero summary = %#v, want both zero rates retained", zero)
	}
	legacy := result.MonitoringInstances["mi_runtime_legacy"]
	if legacy == nil || legacy.UptimeSeconds != 909 || legacy.NetInBytesPerSec != nil || legacy.NetOutBytesPerSec != nil {
		t.Fatalf("legacy NULL network marker summary = %#v, want nonzero stored rates omitted", legacy)
	}

	handler := MonitoringInstanceRuntimeSummaries(repository)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/runtime-summaries", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("handler status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		ReadAt              time.Time                                          `json:"read_at"`
		MonitoringInstances map[string]*store.MonitoringInstanceRuntimeSummary `json:"monitoring_instances"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode handler response: %v", err)
	}
	if response.ReadAt.IsZero() || response.MonitoringInstances["mi_runtime_empty"] != nil || response.MonitoringInstances["mi_runtime_live"] == nil {
		t.Fatalf("handler response = %#v, want read_at and active summaries", response)
	}

}
func insertRuntimeSummaryMonitoringInstance(t *testing.T, ctx context.Context, db *pgxpool.Pool, id, fingerprint string, epoch time.Time, archived bool) {
	t.Helper()
	var archivedAt any
	if archived {
		archivedAt = time.Now().UTC()
	}
	if _, err := db.Exec(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, "group", region, city, provider,
			lifecycle_status, monitoring_status, binding_status, binding_fingerprint,
			binding_epoch_started_at, archived_at
		) values ($1, $2, 'test', 'test-region', 'test-city', 'test-provider',
			'在用', '启用', '已绑定', $3, $4, $5)
	`, id, id, fingerprint, epoch, archivedAt); err != nil {
		t.Fatalf("insert monitoring instance %q: %v", id, err)
	}
}

func insertRuntimeHostSample(t *testing.T, ctx context.Context, db *pgxpool.Pool, id string, observedAt, receivedAt time.Time, fingerprint string, uptime, netIn, netOut int64, networkRatesValid any, backfilled bool, syncBatchID string) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into host_samples (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			mem_total_bytes, swap_used_pct, disk_used_pct, disk_total_bytes, inode_used_pct,
			net_in_bytes_per_sec, net_out_bytes_per_sec, network_rates_valid,
			cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
			disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
		) values (
			$1, $2, $3, 'test-agent', $4,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			$6, $7, $8, 0, 0, 0, 0, 0, $5, false, $9, $10
		)
	`, id, observedAt, receivedAt, fingerprint, uptime, netIn, netOut, networkRatesValid, backfilled, syncBatchID); err != nil {
		t.Fatalf("insert host sample %q/%q: %v", id, syncBatchID, err)
	}
}

type runtimeSummaryHTTPObservationSyncService struct {
	service    *observations.Service
	receivedAt time.Time
}

func (s *runtimeSummaryHTTPObservationSyncService) SyncBatch(ctx context.Context, batch syncing.Batch) (syncing.Result, error) {
	for index := range batch.Observations.HostSamples {
		batch.Observations.HostSamples[index].ReceivedAt = s.receivedAt
	}
	if err := s.service.Ingest(ctx, batch.Observations); err != nil {
		return syncing.Result{}, err
	}
	return syncing.Result{AcceptedAt: s.receivedAt}, nil
}

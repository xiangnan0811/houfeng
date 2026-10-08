package store_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/agent/hostsample"
	"houfeng/db/migrations"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/retention"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

const (
	cpuSamplingTargetID    = "tgt_cpu_sampling_validity"
	cpuSamplingProbeID     = "prb_cpu_sampling_validity"
	cpuSamplingSyncToken   = "cpu-sampling-validity-secret"
	cpuSamplingAgent       = "agent/cpu-sampling-validity"
	cpuSamplingDailyAgent  = "test/cpu-sampling-daily"
	cpuSamplingDailyPrefix = "cpu-daily"

	cpuSamplingTokenHashPrefix = "hmac-sha256:v1:"
	cpuSamplingTokenHMACKey    = "houfeng-agent-token-default-test-key"
)

// TestPostgresIntegrationCPUSamplingValidity is intentionally a single strict
// integration anchor. It drives production Provider -> HTTPS AgentSync -> the
// PostgreSQL repository -> production runtime HTTP/WebSocket handlers, then
// exercises retention and evidence against the same rows.
func TestPostgresIntegrationCPUSamplingValidity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newRuntimeStreamAuthFixture(t)
	seedCPUSamplingSyncFixture(t, ctx, fixture, cpuSamplingSyncToken)

	syncRepository := store.NewPostgresSyncRepository(fixture.pool)
	syncService := syncing.NewService(
		syncRepository,
		syncing.NewCompositePostSyncProcessor(nil, fixture.hub),
	)
	server := newCPUSamplingTLSService(t, fixture, syncService)
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := runtimeStreamAuthHTTPClientWithJar(server, jar)
	sessionID := runtimeStreamAuthLogin(t, client, server.URL, runtimeStreamAuthPassword)
	if status := runtimeStreamAuthProtectedStatus(t, client, server.URL); status != http.StatusNoContent {
		t.Fatalf("authenticated session probe status = %d, want %d", status, http.StatusNoContent)
	}

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer streamCancel()
	stream, _, err := dialRuntimeStreamAuthURL(t, server.URL, client, sessionID)
	if err != nil {
		t.Fatalf("dial runtime stream: %v", err)
	}
	defer stream.Close(websocketNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, streamCtx)

	provider, observedAt := newCPUSamplingProvider(t)
	collected := make([]agentapi.HostSamplePayload, 0, len(observedAt)+1)
	for index, at := range observedAt {
		sample, err := provider.Collect(at)
		if err != nil {
			t.Fatalf("production Provider.Collect(%d): %v", index, err)
		}
		sample.AgentVersion = cpuSamplingAgent
		sample.Fingerprint = runtimeStreamAuthFingerprint
		sample.SyncBatchID = fmt.Sprintf("cpu-provider-%d", index)
		collected = append(collected, sample)
		t.Logf("production Collect %s: cpu=(usage=%v iowait=%v steal=%v valid=%v) memory=%v load1=%v network=(in=%d out=%d valid=%v)", sample.SyncBatchID, sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct, boolPointerString(sample.CPURatesValid), sample.MemUsedPct, sample.Load1, sample.NetInBytesPerSec, sample.NetOutBytesPerSec, boolPointerString(sample.NetworkRatesValid))
	}

	for index := range collected {
		request := cpuSamplingSyncRequest(collected[index], true, false)
		postCPUSamplingSync(t, client, server.URL, request, cpuSamplingSyncToken)
		message := readRuntimeStreamAuthMessage(t, stream, 5*time.Second)
		assertStreamCPUMessage(t, message, collected[index])
		t.Logf("production sync/runtime stream %s: marker=%v cpu=(%v,%v,%v) mem=%v load1=%v net=(%d,%d)", collected[index].SyncBatchID, boolPointerString(message.Sample.CPURatesValid), message.Sample.CPUUsagePct, message.Sample.CPUIOWaitPct, message.Sample.CPUStealPct, message.Sample.MemUsedPct, message.Sample.Load1, message.Sample.NetInBytesPerSec, message.Sample.NetOutBytesPerSec)
	}

	assertCPUSamplingProviderRows(t, ctx, fixture.pool, collected[2].SyncBatchID)
	var heartbeatCount, hostCount, probeCount int
	if err := fixture.pool.QueryRow(ctx, `select count(*)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1`, runtimeStreamAuthMonitoringID).Scan(&heartbeatCount); err != nil {
		t.Fatalf("count persisted heartbeats: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*)::int from host_samples where monitoring_instance_id = $1`, runtimeStreamAuthMonitoringID).Scan(&hostCount); err != nil {
		t.Fatalf("count persisted host samples: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*)::int from probe_observations where monitoring_instance_id = $1`, runtimeStreamAuthMonitoringID).Scan(&probeCount); err != nil {
		t.Fatalf("count persisted probe observations: %v", err)
	}
	if heartbeatCount != len(collected) || hostCount != len(collected) || probeCount != len(collected) {
		t.Fatalf("production sync persistence counts = heartbeats:%d host:%d probes:%d, want %d each", heartbeatCount, hostCount, probeCount, len(collected))
	}
	t.Logf("production sync/DB evidence: heartbeats=%d host_samples=%d probe_observations=%d; false batch=%s retained non-CPU fields", heartbeatCount, hostCount, probeCount, collected[2].SyncBatchID)

	runtimeRepository := store.NewPostgresRuntimeFactsRepository(fixture.pool)
	sparklineRepository := store.NewPostgresMonitoringInstanceSparklinesRepository(fixture.pool)
	providerWindowStart := observedAt[0].Add(-time.Minute)
	sparklines, err := sparklineRepository.GetMonitoringInstanceSparklines(ctx, []string{"cpu_usage_pct", "mem_used_pct"}, providerWindowStart, 1)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceSparklines: %v", err)
	}
	cpuSparkline := sparklines[runtimeStreamAuthMonitoringID]["cpu_usage_pct"]
	memorySparkline := sparklines[runtimeStreamAuthMonitoringID]["mem_used_pct"]
	if len(cpuSparkline) != 1 || cpuSparkline[0] == nil || *cpuSparkline[0] != 30 {
		t.Fatalf("CPU sparkline = %#v, want 30 from valid 20/false/40 rows", cpuSparkline)
	}
	if len(memorySparkline) != 1 || memorySparkline[0] == nil || *memorySparkline[0] != 75 {
		t.Fatalf("memory sparkline = %#v, want 75 from all host rows", memorySparkline)
	}
	t.Logf("production sparkline evidence: cpu_valid_average=%v memory_average=%v", *cpuSparkline[0], *memorySparkline[0])

	zeroSample := collected[3]
	zeroSample.ObservedAt = observedAt[3].Add(10 * time.Second)
	zeroSample.SyncBatchID = "cpu-provider-valid-zero"
	zeroSample.CPUUsagePct = 0
	zeroSample.CPUIOWaitPct = 0
	zeroSample.CPUStealPct = 0
	zeroSample.CPURatesValid = new(true)
	postCPUSamplingSync(t, client, server.URL, cpuSamplingSyncRequest(zeroSample, false, false), cpuSamplingSyncToken)
	zeroMessage := readRuntimeStreamAuthMessage(t, stream, 5*time.Second)
	if zeroMessage.Sample.CPURatesValid == nil || !*zeroMessage.Sample.CPURatesValid || zeroMessage.Sample.CPUUsagePct != 0 || zeroMessage.Sample.CPUIOWaitPct != 0 || zeroMessage.Sample.CPUStealPct != 0 {
		t.Fatalf("true-zero runtime stream sample = %#v, want explicit true zero CPU rates", zeroMessage.Sample)
	}
	t.Logf("production valid-zero sync/runtime evidence: marker=%v cpu=(%v,%v,%v)", boolPointerString(zeroMessage.Sample.CPURatesValid), zeroMessage.Sample.CPUUsagePct, zeroMessage.Sample.CPUIOWaitPct, zeroMessage.Sample.CPUStealPct)

	factsBeforeRaw, err := runtimeRepository.GetMonitoringInstanceRuntimeFacts(ctx, runtimeStreamAuthMonitoringID, runtimefacts.WindowRequest{Key: "realtime", StartedAt: providerWindowStart, EndedAt: zeroSample.ObservedAt.Add(time.Minute), BucketCount: 8})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts before direct raw values: %v", err)
	}
	if factsBeforeRaw.LatestHostSample == nil || factsBeforeRaw.LatestHostSample.CPURatesValid == nil || !*factsBeforeRaw.LatestHostSample.CPURatesValid || factsBeforeRaw.LatestHostSample.CPUUsagePct != 0 {
		t.Fatalf("latest runtime fact before direct raw values = %#v, want true zero", factsBeforeRaw.LatestHostSample)
	}
	probeFacts, err := runtimeRepository.GetTargetRuntimeFacts(ctx, cpuSamplingTargetID, providerWindowStart, 20)
	if err != nil {
		t.Fatalf("GetTargetRuntimeFacts: %v", err)
	}
	if len(probeFacts.RecentProbeObservations) == 0 {
		t.Fatal("recent probe observations are empty after false CPU sync")
	}
	t.Logf("production runtime repository evidence: latest=%#v recent_host_samples=%d recent_probe_observations=%d", factsBeforeRaw.LatestHostSample, len(factsBeforeRaw.RecentHostSamples), len(probeFacts.RecentProbeObservations))

	seedCPUSamplingEvidenceRows(t, ctx, fixture.pool, time.Now().UTC().Truncate(time.Microsecond))

	directAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	insertCPUSamplingHostSample(t, ctx, fixture.pool, directAt, directAt, math.NaN(), math.Inf(1), math.Inf(-1), nil, 88, 8, false, false, "cpu-direct-nonfinite")
	repositorySample := observations.HostSampleWrite{
		MonitoringInstanceID: runtimeStreamAuthMonitoringID,
		ObservedAt:           directAt.Add(-500 * time.Millisecond),
		ReceivedAt:           directAt.Add(-500 * time.Millisecond),
		AgentVersion:         "test/direct-repository",
		Fingerprint:          runtimeStreamAuthFingerprint,
		CPUUsagePct:          math.NaN(),
		CPUIOWaitPct:         math.Inf(1),
		CPUStealPct:          math.Inf(-1),
		Load1:                8.5,
		MemUsedPct:           89,
		SyncBatchID:          "cpu-direct-repository-nonfinite",
	}
	if err := store.NewPostgresObservationRepository(fixture.pool).RecordBatch(ctx, observations.BatchWrite{
		MonitoringInstanceID: runtimeStreamAuthMonitoringID,
		HostSamples:          []observations.HostSampleWrite{repositorySample},
	}); err != nil {
		t.Fatalf("persist non-finite sample through observation repository: %v", err)
	}
	assertCPUSamplingRawNonFiniteRows(t, ctx, fixture.pool)
	t.Logf("legacy DB evidence: sync_batch_id=%s retained raw NaN/+Inf/-Inf CPU and NULL marker", "cpu-direct-nonfinite")
	t.Logf("observation repository evidence: sync_batch_id=%s retained raw NaN/+Inf/-Inf CPU and normalized false marker", repositorySample.SyncBatchID)

	factsServerResponse := getCPUSamplingRuntimeFactsWithWindow(t, client, server.URL, "realtime")
	if factsServerResponse.LatestHostSample == nil || factsServerResponse.LatestHostSample.CPURatesValid == nil || *factsServerResponse.LatestHostSample.CPURatesValid || factsServerResponse.LatestHostSample.CPUUsagePct != 0 || factsServerResponse.LatestHostSample.CPUIOWaitPct != 0 || factsServerResponse.LatestHostSample.CPUStealPct != 0 || factsServerResponse.LatestHostSample.MemUsedPct != 88 {
		t.Fatalf("HTTP latest non-finite host sample = %#v, want false marker and zero CPU placeholders with memory 88", factsServerResponse.LatestHostSample)
	}
	expectedRecentNonFinite := map[string]float64{
		"cpu-direct-nonfinite":            88,
		"cpu-direct-repository-nonfinite": 89,
	}
	for _, sample := range factsServerResponse.RecentHostSamples {
		expectedMemory, ok := expectedRecentNonFinite[sample.SyncBatchID]
		if !ok {
			continue
		}
		if sample.CPURatesValid == nil || *sample.CPURatesValid || sample.CPUUsagePct != 0 || sample.CPUIOWaitPct != 0 || sample.CPUStealPct != 0 || sample.MemUsedPct != expectedMemory {
			t.Fatalf("HTTP recent non-finite sample = %#v, want JSON-safe false marker, zero CPU placeholders, memory=%v", sample, expectedMemory)
		}
		delete(expectedRecentNonFinite, sample.SyncBatchID)
	}
	if len(expectedRecentNonFinite) != 0 {
		t.Fatalf("HTTP realtime recent host samples omitted non-finite rows: %#v", expectedRecentNonFinite)
	}
	t.Logf("production runtime HTTP evidence: latest marker=%v cpu=(%v,%v,%v) memory=%v recent_nonfinite=true", boolPointerString(factsServerResponse.LatestHostSample.CPURatesValid), factsServerResponse.LatestHostSample.CPUUsagePct, factsServerResponse.LatestHostSample.CPUIOWaitPct, factsServerResponse.LatestHostSample.CPUStealPct, factsServerResponse.LatestHostSample.MemUsedPct)

	if err := fixture.hub.AfterSuccessfulSync(ctx, syncing.Batch{MonitoringInstanceID: runtimeStreamAuthMonitoringID, Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{repositorySample}}}, syncing.Result{Disposition: syncing.ResultDispositionRecorded, AcceptedAt: repositorySample.ReceivedAt}); err != nil {
		t.Fatalf("publish repository non-finite stream seed: %v", err)
	}
	seedMessage := readRuntimeStreamAuthMessage(t, stream, 5*time.Second)
	assertCPUSamplingNonFiniteStreamMessage(t, seedMessage, repositorySample.SyncBatchID, repositorySample.MemUsedPct, repositorySample.Load1)
	t.Logf("production runtime stream seed evidence: batch=%s marker=%v cpu=(%v,%v,%v) memory=%v", seedMessage.Sample.SyncBatchID, boolPointerString(seedMessage.Sample.CPURatesValid), seedMessage.Sample.CPUUsagePct, seedMessage.Sample.CPUIOWaitPct, seedMessage.Sample.CPUStealPct, seedMessage.Sample.MemUsedPct)

	streamRawAt := directAt.Add(time.Second)
	nonFiniteWrite := observations.HostSampleWrite{
		MonitoringInstanceID: runtimeStreamAuthMonitoringID,
		ObservedAt:           streamRawAt,
		ReceivedAt:           streamRawAt,
		AgentVersion:         "test/direct-stream",
		Fingerprint:          runtimeStreamAuthFingerprint,
		CPUUsagePct:          math.NaN(),
		CPUIOWaitPct:         math.Inf(1),
		CPUStealPct:          math.Inf(-1),
		MemUsedPct:           91,
		Load1:                9,
		SyncBatchID:          "cpu-direct-stream-nonfinite",
	}
	if err := fixture.hub.AfterSuccessfulSync(ctx, syncing.Batch{MonitoringInstanceID: runtimeStreamAuthMonitoringID, Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{nonFiniteWrite}}}, syncing.Result{Disposition: syncing.ResultDispositionRecorded, AcceptedAt: streamRawAt}); err != nil {
		t.Fatalf("publish direct non-finite stream append: %v", err)
	}
	nonFiniteMessage := readRuntimeStreamAuthMessage(t, stream, 5*time.Second)
	assertCPUSamplingNonFiniteStreamMessage(t, nonFiniteMessage, nonFiniteWrite.SyncBatchID, nonFiniteWrite.MemUsedPct, nonFiniteWrite.Load1)
	assertCPUSamplingRawNonFiniteRows(t, ctx, fixture.pool)
	t.Logf("production runtime stream append evidence: batch=%s marker=%v cpu=(%v,%v,%v) memory=%v", nonFiniteMessage.Sample.SyncBatchID, boolPointerString(nonFiniteMessage.Sample.CPURatesValid), nonFiniteMessage.Sample.CPUUsagePct, nonFiniteMessage.Sample.CPUIOWaitPct, nonFiniteMessage.Sample.CPUStealPct, nonFiniteMessage.Sample.MemUsedPct)

	t.Run("runtime HTTP API JSON boundary", func(t *testing.T) {
		facts := getCPUSamplingRuntimeFacts(t, client, server.URL)
		if facts.LatestHostSample == nil || facts.LatestHostSample.CPURatesValid == nil || *facts.LatestHostSample.CPURatesValid || facts.LatestHostSample.CPUUsagePct != 0 || facts.LatestHostSample.CPUIOWaitPct != 0 || facts.LatestHostSample.CPUStealPct != 0 {
			t.Fatalf("runtime HTTP latest boundary sample = %#v, want explicit false and finite placeholders", facts.LatestHostSample)
		}
		t.Logf("runtime HTTP subtest evidence: latest=%#v recent=%d", facts.LatestHostSample, len(facts.RecentHostSamples))
	})
	t.Run("retention and daily aggregates", func(t *testing.T) {
		assertCPUSamplingRetentionAndEvidence(t, ctx, fixture.pool)
	})
	t.Run("mixed evidence source metadata", func(t *testing.T) {
		assertCPUSamplingMixedEvidence(t, ctx, fixture.pool)
	})
	t.Run("metric-level CPU evidence gaps", func(t *testing.T) {
		assertCPUSamplingEvidenceGaps(t, ctx, fixture.pool)
	})
	t.Run("migration preserves legacy rows", func(t *testing.T) {
		assertCPUSamplingMigrationPreservesLegacyRows(t, ctx)
	})
}

// websocket.StatusNormalClosure is kept behind a local constant so this test
// file does not need to couple its assertions to the websocket implementation.
const websocketNormalClosure = 1000

func newCPUSamplingTLSService(t *testing.T, fixture *runtimeStreamAuthFixture, syncService *syncing.Service) *httptest.Server {
	t.Helper()
	validator := &runtimeStreamAuthValidator{service: fixture.service}
	authMiddleware := centerhttp.RequireSession(fixture.service, fixture.scope)
	runtimeHandler := handlers.MonitoringInstanceRuntimeStream(fixture.monitoring, fixture.hub, validator)
	router := centerhttp.New(centerhttp.RouterOptions{
		Version:                                "cpu-sampling-validity",
		AgentSyncHandler:                       handlers.AgentSync(syncService),
		MonitoringInstanceRuntimeFactsHandler:  handlers.MonitoringInstanceRuntimeFacts(store.NewPostgresRuntimeFactsRepository(fixture.pool)),
		MonitoringInstanceRuntimeStreamHandler: runtimeHandler,
		AuthLoginHandler:                       handlers.Login(fixture.service),
		AuthLogoutHandler:                      handlers.Logout(fixture.service),
		AuthMeHandler:                          handlers.Me(fixture.service, handlers.RuntimeCapabilities{}),
		AuthChangePasswordHandler:              handlers.ChangePassword(fixture.service),
		AuthMiddleware:                         authMiddleware,
	})
	mux := http.NewServeMux()
	mux.Handle(runtimeStreamAuthSessionProbe, authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("/", router)
	return httptest.NewTLSServer(mux)
}

func seedCPUSamplingSyncFixture(t *testing.T, ctx context.Context, fixture *runtimeStreamAuthFixture, syncToken string) {
	t.Helper()
	tokenHash := cpuSamplingSyncTokenHash(syncToken)
	if _, err := fixture.pool.Exec(ctx, `
		insert into monitoring_agent_sessions (session_id, monitoring_instance_id, token_hash, capability, fingerprint_hash)
		values ($1, $2, $3, 'full', $4)`, "mas_cpu_sampling_validity", runtimeStreamAuthMonitoringID, tokenHash, runtimeStreamAuthFingerprint); err != nil {
		t.Fatalf("seed monitoring agent session: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		insert into targets (target_id, name, target_type, host, run_status)
		values ($1, 'CPU validity target', 'service', 'example.test', '启用')`, cpuSamplingTargetID); err != nil {
		t.Fatalf("seed CPU validity target: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		insert into probe_items (probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds)
		values ($1, $2, 'http', '1m', 5)`, cpuSamplingProbeID, cpuSamplingTargetID); err != nil {
		t.Fatalf("seed CPU validity probe item: %v", err)
	}
}

func cpuSamplingSyncTokenHash(token string) string {
	rootMAC := hmac.New(sha256.New, []byte(cpuSamplingTokenHMACKey))
	_, _ = rootMAC.Write([]byte("houfeng-agent-sync-token-v1"))
	syncKey := rootMAC.Sum(nil)
	tokenMAC := hmac.New(sha256.New, syncKey)
	_, _ = tokenMAC.Write([]byte(token))
	return cpuSamplingTokenHashPrefix + hex.EncodeToString(tokenMAC.Sum(nil))
}

func newCPUSamplingProvider(t *testing.T) (*hostsample.Provider, []time.Time) {
	t.Helper()
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-20 * time.Minute)
	observedAt := []time.Time{base, base.Add(10 * time.Second), base.Add(20 * time.Second), base.Add(30 * time.Second)}
	files := map[string][]string{
		"/proc/loadavg": {
			"1.25 0.75 0.50 1/100 123\n",
			"1.25 0.75 0.50 1/100 124\n",
			"1.25 0.75 0.50 1/100 125\n",
			"1.25 0.75 0.50 1/100 126\n",
		},
		"/proc/meminfo": {
			"MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 500 kB\nSwapFree: 400 kB\n",
			"MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 500 kB\nSwapFree: 400 kB\n",
			"MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 500 kB\nSwapFree: 400 kB\n",
			"MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 500 kB\nSwapFree: 400 kB\n",
		},
		"/proc/uptime": {
			"3600.00 0.00\n",
			"3610.00 0.00\n",
			"3620.00 0.00\n",
			"3630.00 0.00\n",
		},
		"/proc/stat": {
			"cpu  100 0 100 690 10 0 0 0 0 0\n",
			"cpu  120 0 100 770 10 0 0 0 0 0\n",
			"cpu  140 0 100 850 9 0 0 0 0 0\n",
			"cpu  180 0 100 910 9 0 0 0 0 0\n",
		},
		"/proc/net/dev": {
			"Inter-| Receive                                                | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
			"Inter-| Receive                                                | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
			"Inter-| Receive                                                | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
			"Inter-| Receive                                                | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n",
		},
		"/proc/diskstats": {
			"8 0 sda 0 0 100 0 0 0 200 0 0 50 0\n",
			"8 0 sda 0 0 100 0 0 0 200 0 0 50 0\n",
			"8 0 sda 0 0 100 0 0 0 200 0 0 50 0\n",
			"8 0 sda 0 0 100 0 0 0 200 0 0 50 0\n",
		},
	}
	indices := make(map[string]int, len(files))
	readFile := func(path string) ([]byte, error) {
		values, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("unexpected procfs path %q", path)
		}
		index := indices[path]
		if index >= len(values) {
			index = len(values) - 1
		}
		indices[path]++
		return []byte(values[index]), nil
	}
	provider := hostsample.NewWithDeps(readFile, func(string) (hostsample.FilesystemStats, error) {
		return hostsample.FilesystemStats{Blocks: 1000, Bfree: 200, Bsize: 4096, Files: 100, Ffree: 20}, nil
	})
	return provider, observedAt
}

func cpuSamplingSyncRequest(sample agentapi.HostSamplePayload, includeProbe, backfilled bool) agentapi.SyncRequest {
	sample.IsBackfilled = backfilled
	heartbeat := agentapi.MonitoringInstanceHeartbeat{
		ObservedAt: sample.ObservedAt, AgentVersion: sample.AgentVersion, Fingerprint: sample.Fingerprint, SyncBatchID: sample.SyncBatchID, IsBackfilled: backfilled,
	}
	request := agentapi.SyncRequest{
		SessionID:            "mas_cpu_sampling_validity",
		MonitoringInstanceID: runtimeStreamAuthMonitoringID,
		Heartbeats:           []agentapi.MonitoringInstanceHeartbeat{heartbeat},
		HostSamples:          []agentapi.HostSamplePayload{sample},
	}
	if includeProbe {
		latency, status := 25, 200
		request.ProbeObservations = []agentapi.ProbeObservationPayload{{
			TargetID: cpuSamplingTargetID, ProbeItemID: cpuSamplingProbeID, ProbeKind: "http", ObservedAt: sample.ObservedAt,
			AgentVersion: sample.AgentVersion, Fingerprint: sample.Fingerprint, SyncBatchID: sample.SyncBatchID,
			ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latency, HTTPStatus: &status,
		}}
	}
	return request
}

func postCPUSamplingSync(t *testing.T, client *http.Client, serverURL string, request agentapi.SyncRequest, syncToken string) {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal sync request %q: %v", request.HostSamples[0].SyncBatchID, err)
	}
	httpRequest, err := http.NewRequest(http.MethodPost, serverURL+agentapi.SyncPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new sync request: %v", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+syncToken)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		t.Fatalf("POST production sync %q: %v", request.HostSamples[0].SyncBatchID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(response.Body)
		t.Fatalf("POST production sync %q status = %d body=%s", request.HostSamples[0].SyncBatchID, response.StatusCode, bodyBytes)
	}
	var decoded agentapi.SyncResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode production sync response %q: %v", request.HostSamples[0].SyncBatchID, err)
	}
	if decoded.Status != "accepted" {
		t.Fatalf("production sync response %q status = %q, want accepted", request.HostSamples[0].SyncBatchID, decoded.Status)
	}
	t.Logf("production sync evidence: batch=%s response_status=%s accepted_at=%s", request.HostSamples[0].SyncBatchID, decoded.Status, decoded.AcceptedAt.Format(time.RFC3339Nano))
}

func assertStreamCPUMessage(t *testing.T, message runtimefacts.HostSampleStreamMessage, want agentapi.HostSamplePayload) {
	t.Helper()
	if message.Sample.SyncBatchID != want.SyncBatchID {
		t.Fatalf("runtime stream sync_batch_id = %q, want %q", message.Sample.SyncBatchID, want.SyncBatchID)
	}
	if message.Sample.MemUsedPct != want.MemUsedPct || message.Sample.Load1 != want.Load1 || message.Sample.NetInBytesPerSec != want.NetInBytesPerSec || message.Sample.NetOutBytesPerSec != want.NetOutBytesPerSec {
		t.Fatalf("runtime stream non-CPU fields = mem:%v load1:%v net:%d/%d, want mem:%v load1:%v net:%d/%d", message.Sample.MemUsedPct, message.Sample.Load1, message.Sample.NetInBytesPerSec, message.Sample.NetOutBytesPerSec, want.MemUsedPct, want.Load1, want.NetInBytesPerSec, want.NetOutBytesPerSec)
	}
	if want.CPURatesValid == nil || !*want.CPURatesValid {
		if message.Sample.CPURatesValid == nil || *message.Sample.CPURatesValid || message.Sample.CPUUsagePct != 0 || message.Sample.CPUIOWaitPct != 0 || message.Sample.CPUStealPct != 0 {
			t.Fatalf("runtime stream false CPU sample = %#v, want false marker and zero placeholders", message.Sample)
		}
		return
	}
	if message.Sample.CPURatesValid == nil || !*message.Sample.CPURatesValid || message.Sample.CPUUsagePct != want.CPUUsagePct || message.Sample.CPUIOWaitPct != want.CPUIOWaitPct || message.Sample.CPUStealPct != want.CPUStealPct {
		t.Fatalf("runtime stream valid CPU sample = %#v, want %#v", message.Sample, want)
	}
}

func assertCPUSamplingProviderRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, falseBatchID string) {
	t.Helper()
	var marker sql.NullBool
	var usage, iowait, steal, memory, load1 float64
	if err := pool.QueryRow(ctx, `select cpu_rates_valid, cpu_usage_pct, cpu_iowait_pct, cpu_steal_pct, mem_used_pct, load_1 from host_samples where monitoring_instance_id = $1 and sync_batch_id = $2`, runtimeStreamAuthMonitoringID, falseBatchID).Scan(&marker, &usage, &iowait, &steal, &memory, &load1); err != nil {
		t.Fatalf("read false provider host row: %v", err)
	}
	if !marker.Valid || marker.Bool || usage != 0 || iowait != 0 || steal != 0 || memory != 75 || load1 != 1.25 {
		t.Fatalf("false provider host row = marker:%#v usage:%v iowait:%v steal:%v memory:%v load1:%v", marker, usage, iowait, steal, memory, load1)
	}
	var heartbeatCount, probeCount int
	if err := pool.QueryRow(ctx, `select count(*)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1 and sync_batch_id = $2`, runtimeStreamAuthMonitoringID, falseBatchID).Scan(&heartbeatCount); err != nil {
		t.Fatalf("read false provider heartbeat: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from probe_observations where monitoring_instance_id = $1 and sync_batch_id = $2`, runtimeStreamAuthMonitoringID, falseBatchID).Scan(&probeCount); err != nil {
		t.Fatalf("read false provider probe: %v", err)
	}
	if heartbeatCount != 1 || probeCount != 1 {
		t.Fatalf("false provider batch carrier counts = heartbeat:%d probe:%d, want 1/1", heartbeatCount, probeCount)
	}
}

func assertCPUSamplingRawNonFiniteRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	expected := []struct {
		batchID     string
		memory      float64
		load1       float64
		markerValid bool
		markerValue bool
	}{
		{batchID: "cpu-direct-nonfinite", memory: 88, load1: 1, markerValid: false},
		{batchID: "cpu-direct-repository-nonfinite", memory: 89, load1: 8.5, markerValid: true, markerValue: false},
	}
	for _, want := range expected {
		var usage, iowait, steal, memory, load1 float64
		var marker sql.NullBool
		if err := pool.QueryRow(ctx, `select cpu_usage_pct, cpu_iowait_pct, cpu_steal_pct, mem_used_pct, load_1, cpu_rates_valid from host_samples where monitoring_instance_id = $1 and sync_batch_id = $2`, runtimeStreamAuthMonitoringID, want.batchID).Scan(&usage, &iowait, &steal, &memory, &load1, &marker); err != nil {
			t.Fatalf("read non-finite host row %q: %v", want.batchID, err)
		}
		if !math.IsNaN(usage) || !math.IsInf(iowait, 1) || !math.IsInf(steal, -1) || memory != want.memory || load1 != want.load1 || marker.Valid != want.markerValid || (marker.Valid && marker.Bool != want.markerValue) {
			t.Fatalf("raw non-finite host row %q = usage:%v iowait:%v steal:%v memory:%v load1:%v marker:%#v, want raw CPU, memory=%v, load1=%v, marker_valid=%v marker=%v", want.batchID, usage, iowait, steal, memory, load1, marker, want.memory, want.load1, want.markerValid, want.markerValue)
		}
	}
}

func assertCPUSamplingNonFiniteStreamMessage(t *testing.T, message runtimefacts.HostSampleStreamMessage, batchID string, memory, load1 float64) {
	t.Helper()
	if message.Sample.SyncBatchID != batchID || message.Sample.CPURatesValid == nil || *message.Sample.CPURatesValid || message.Sample.CPUUsagePct != 0 || message.Sample.CPUIOWaitPct != 0 || message.Sample.CPUStealPct != 0 || message.Sample.MemUsedPct != memory || message.Sample.Load1 != load1 {
		t.Fatalf("runtime stream non-finite sample = %#v, want batch=%q false marker, zero CPU placeholders, memory=%v load1=%v", message.Sample, batchID, memory, load1)
	}
}

func getCPUSamplingRuntimeFacts(t *testing.T, client *http.Client, serverURL string) runtimefacts.MonitoringInstanceRuntimeFacts {
	return getCPUSamplingRuntimeFactsWithWindow(t, client, serverURL, "24h")
}

func getCPUSamplingRuntimeFactsWithWindow(t *testing.T, client *http.Client, serverURL, window string) runtimefacts.MonitoringInstanceRuntimeFacts {
	t.Helper()
	response, err := client.Get(serverURL + "/api/monitoring-instances/" + runtimeStreamAuthMonitoringID + "/runtime-facts?window=" + url.QueryEscape(window))
	if err != nil {
		t.Fatalf("GET runtime facts window=%q: %v", window, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET runtime facts window=%q status = %d", window, response.StatusCode)
	}
	var facts runtimefacts.MonitoringInstanceRuntimeFacts
	if err := json.NewDecoder(response.Body).Decode(&facts); err != nil {
		t.Fatalf("decode runtime facts window=%q: %v", window, err)
	}
	return facts
}

func seedCPUSamplingEvidenceRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	legacyDay := today.AddDate(0, 0, -40)
	mixedRetentionDay := today.AddDate(0, 0, -35)
	falseDay := today.AddDate(0, 0, -34)
	syntheticMixedDay := today.AddDate(0, 0, -10)

	insertCPUSamplingHostSample(t, ctx, pool, legacyDay.Add(time.Hour), legacyDay.Add(time.Hour), 77, 7, 3, new(true), 11, 1, false, false, "cpu-legacy-raw")
	insertCPUSamplingDailyAggregate(t, ctx, pool, legacyDay, 9, nil, nil, nil, 88, 88, 1, 9, 9, 9, 8, 8, 7, 7, 2, 2, 0, 0, true)

	insertCPUSamplingHostSample(t, ctx, pool, mixedRetentionDay.Add(time.Hour), mixedRetentionDay.Add(time.Hour), 20, 5, 2, new(true), 10, 1, true, false, "cpu-retention-valid-20")
	insertCPUSamplingHostSample(t, ctx, pool, mixedRetentionDay.Add(2*time.Hour), mixedRetentionDay.Add(2*time.Hour), 0, 0, 0, new(false), 20, 2, true, true, "cpu-retention-invalid")
	insertCPUSamplingHostSample(t, ctx, pool, mixedRetentionDay.Add(3*time.Hour), mixedRetentionDay.Add(3*time.Hour), 40, 7, 4, new(true), 30, 3, false, true, "cpu-retention-valid-40")

	insertCPUSamplingHostSample(t, ctx, pool, falseDay.Add(time.Hour), falseDay.Add(time.Hour), 0, 0, 0, new(false), 55, 2, false, false, "cpu-retention-all-false-1")
	insertCPUSamplingHostSample(t, ctx, pool, falseDay.Add(2*time.Hour), falseDay.Add(2*time.Hour), 0, 0, 0, new(false), 65, 4, false, false, "cpu-retention-all-false-2")

	insertCPUSamplingHostSample(t, ctx, pool, syntheticMixedDay.Add(time.Hour), syntheticMixedDay.Add(time.Hour), 20, 2, 1, new(true), 10, 1, false, false, cpuSamplingDailyPrefix+"-raw-20")
	insertCPUSamplingHostSample(t, ctx, pool, syntheticMixedDay.Add(2*time.Hour), syntheticMixedDay.Add(2*time.Hour), 40, 4, 2, new(true), 20, 2, false, false, cpuSamplingDailyPrefix+"-raw-40")
	insertCPUSamplingDailyAggregate(t, ctx, pool, syntheticMixedDay, 3, 2, 0, 0, 30, 40, 2, 3, 15, 20, 3, 5, 5, 2, 0, 0, 0, 0, true)

	t.Logf("seeded retention/evidence rows: legacy_day=%s mixed_day=%s all_false_day=%s synthetic_mixed_day=%s", legacyDay.Format("2006-01-02"), mixedRetentionDay.Format("2006-01-02"), falseDay.Format("2006-01-02"), syntheticMixedDay.Format("2006-01-02"))
}

func insertCPUSamplingHostSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, observedAt, receivedAt time.Time, usage, iowait, steal float64, marker any, memory, load5 float64, backfilled, maintenance bool, syncBatchID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into host_samples (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, cpu_rates_valid, load_1, load_5, load_15, mem_used_pct,
			mem_available_bytes, mem_total_bytes, swap_used_pct, disk_used_pct, disk_total_bytes,
			inode_used_pct, net_in_bytes_per_sec, net_out_bytes_per_sec, network_rates_valid,
			cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
			disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
		) values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $22, $23, $24,
			$25, $26, $27, $28, $29
		)`, runtimeStreamAuthMonitoringID, observedAt, receivedAt, cpuSamplingDailyAgent, runtimeStreamAuthFingerprint,
		usage, marker, 1.0, load5, load5, memory,
		250*1024, 1000*1024, 20.0, 80.0, int64(1000*4096),
		80.0, int64(100), int64(50), true,
		iowait, steal, int64(10), int64(20),
		5.0, int64(3600), maintenance, backfilled, syncBatchID); err != nil {
		t.Fatalf("insert host sample %q: %v", syncBatchID, err)
	}
}

func insertCPUSamplingDailyAggregate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketDate time.Time, sampleCount int, cpuValidSampleCount, cpuValidBackfilledCount, cpuValidMaintenanceCount any, avgCPU, maxCPU, avgLoad, maxLoad, avgMemory, maxMemory, avgIOWait, maxIOWait, avgSteal, maxSteal, avgDisk, maxDisk, backfilledCount, maintenanceCount int, finalized bool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into monitoring_instance_host_sample_daily_aggregates (
			monitoring_instance_id, bucket_date, sample_count,
			cpu_valid_sample_count, cpu_valid_backfilled_sample_count, cpu_valid_maintenance_sample_count,
			avg_cpu_usage_pct, max_cpu_usage_pct, avg_load_5, max_load_5,
			avg_mem_used_pct, max_mem_used_pct, avg_cpu_iowait_pct, max_cpu_iowait_pct,
			avg_cpu_steal_pct, max_cpu_steal_pct, avg_disk_busy_pct, max_disk_busy_pct,
			backfilled_sample_count, maintenance_sample_count, updated_at, finalized
		) values ($1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, now(), $21)
		on conflict (monitoring_instance_id, bucket_date) do update set
			sample_count = excluded.sample_count,
			cpu_valid_sample_count = excluded.cpu_valid_sample_count,
			cpu_valid_backfilled_sample_count = excluded.cpu_valid_backfilled_sample_count,
			cpu_valid_maintenance_sample_count = excluded.cpu_valid_maintenance_sample_count,
			avg_cpu_usage_pct = excluded.avg_cpu_usage_pct,
			max_cpu_usage_pct = excluded.max_cpu_usage_pct,
			avg_load_5 = excluded.avg_load_5,
			max_load_5 = excluded.max_load_5,
			avg_mem_used_pct = excluded.avg_mem_used_pct,
			max_mem_used_pct = excluded.max_mem_used_pct,
			avg_cpu_iowait_pct = excluded.avg_cpu_iowait_pct,
			max_cpu_iowait_pct = excluded.max_cpu_iowait_pct,
			avg_cpu_steal_pct = excluded.avg_cpu_steal_pct,
			max_cpu_steal_pct = excluded.max_cpu_steal_pct,
			avg_disk_busy_pct = excluded.avg_disk_busy_pct,
			max_disk_busy_pct = excluded.max_disk_busy_pct,
			backfilled_sample_count = excluded.backfilled_sample_count,
			maintenance_sample_count = excluded.maintenance_sample_count,
			updated_at = excluded.updated_at,
			finalized = excluded.finalized`, runtimeStreamAuthMonitoringID, bucketDate, sampleCount,
		cpuValidSampleCount, cpuValidBackfilledCount, cpuValidMaintenanceCount,
		avgCPU, maxCPU, avgLoad, maxLoad, avgMemory, maxMemory, avgIOWait, maxIOWait,
		avgSteal, maxSteal, avgDisk, maxDisk, backfilledCount, maintenanceCount, finalized); err != nil {
		t.Fatalf("insert daily aggregate %s: %v", bucketDate.Format("2006-01-02"), err)
	}
}

func assertCPUSamplingRetentionAndEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	policy := retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}
	if _, err := store.NewPostgresRetentionRepository(pool).ApplyRetention(ctx, policy, now); err != nil {
		t.Fatalf("ApplyRetention CPU sampling rows: %v", err)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	mixedDay := today.AddDate(0, 0, -35)
	falseDay := today.AddDate(0, 0, -34)
	legacyDay := today.AddDate(0, 0, -40)
	var sampleCount, cpuCount, cpuBackfilled, cpuMaintenance int
	var avgCPU, avgIOWait, avgSteal, avgMemory *float64
	if err := pool.QueryRow(ctx, `select sample_count, cpu_valid_sample_count, cpu_valid_backfilled_sample_count, cpu_valid_maintenance_sample_count, avg_cpu_usage_pct, avg_cpu_iowait_pct, avg_cpu_steal_pct, avg_mem_used_pct from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = $1 and bucket_date = $2`, runtimeStreamAuthMonitoringID, mixedDay).Scan(&sampleCount, &cpuCount, &cpuBackfilled, &cpuMaintenance, &avgCPU, &avgIOWait, &avgSteal, &avgMemory); err != nil {
		t.Fatalf("read mixed retention aggregate: %v", err)
	}
	if sampleCount != 3 || cpuCount != 2 || cpuBackfilled != 1 || cpuMaintenance != 1 || avgCPU == nil || *avgCPU != 30 || avgIOWait == nil || *avgIOWait != 6 || avgSteal == nil || *avgSteal != 3 || avgMemory == nil || *avgMemory != 20 {
		t.Fatalf("mixed retention aggregate = count:%d cpu:%d/%d/%d avgCPU:%v avgIOWait:%v avgSteal:%v avgMemory:%v", sampleCount, cpuCount, cpuBackfilled, cpuMaintenance, avgCPU, avgIOWait, avgSteal, avgMemory)
	}
	if err := pool.QueryRow(ctx, `select sample_count, cpu_valid_sample_count, avg_cpu_usage_pct, avg_mem_used_pct from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = $1 and bucket_date = $2`, runtimeStreamAuthMonitoringID, falseDay).Scan(&sampleCount, &cpuCount, &avgCPU, &avgMemory); err != nil {
		t.Fatalf("read all-false retention aggregate: %v", err)
	}
	if sampleCount != 2 || cpuCount != 0 || avgCPU != nil || avgMemory == nil || *avgMemory != 60 {
		t.Fatalf("all-false retention aggregate = count:%d cpu:%d avgCPU:%v avgMemory:%v", sampleCount, cpuCount, avgCPU, avgMemory)
	}
	var finalized bool
	var legacySampleCount int
	var legacyCPU float64
	var legacyCPUCount sql.NullInt32
	if err := pool.QueryRow(ctx, `select sample_count, avg_cpu_usage_pct, cpu_valid_sample_count, finalized from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = $1 and bucket_date = $2`, runtimeStreamAuthMonitoringID, legacyDay).Scan(&legacySampleCount, &legacyCPU, &legacyCPUCount, &finalized); err != nil {
		t.Fatalf("read finalized legacy aggregate: %v", err)
	}
	if legacySampleCount != 9 || legacyCPU != 88 || legacyCPUCount.Valid || !finalized {
		t.Fatalf("finalized legacy aggregate = count:%d cpu:%v cpu_count:%#v finalized:%v, want unchanged legacy values", legacySampleCount, legacyCPU, legacyCPUCount, finalized)
	}
	rawCutoff := now.AddDate(0, 0, -30)
	var oldRows int
	if err := pool.QueryRow(ctx, `select count(*)::int from host_samples where monitoring_instance_id = $1 and observed_at < $2`, runtimeStreamAuthMonitoringID, rawCutoff).Scan(&oldRows); err != nil {
		t.Fatalf("count raw rows after retention: %v", err)
	}
	if oldRows != 0 {
		t.Fatalf("raw rows older than raw retention cutoff = %d, want retention cleanup", oldRows)
	}

	repository := store.NewPostgresRuntimeFactsRepository(pool)
	mixedCapture, err := repository.LoadMonitoringHostEvidence(ctx, runtimeStreamAuthMonitoringID, evidence.TimeWindow{Start: mixedDay, End: mixedDay.Add(24 * time.Hour)}, 24*time.Hour, []string{"cpu_usage_pct", "mem_used_pct"})
	if err != nil {
		t.Fatalf("LoadMonitoringHostEvidence mixed retention day: %v", err)
	}
	assertCPUMetricCapture(t, mixedCapture, adapters.MonitoringSourceDailyAggregate, 2, 30, "mixed retention CPU")
	assertMemoryMetricCapture(t, mixedCapture, 3, 20, "mixed retention memory")
	t.Logf("retention/daily evidence: mixed CPU count=2 avg=30 memory count=3 avg=20 source=daily_aggregate; all-false CPU avg=NULL count=0")

	falseCapture, err := repository.LoadMonitoringHostEvidence(ctx, runtimeStreamAuthMonitoringID, evidence.TimeWindow{Start: falseDay, End: falseDay.Add(24 * time.Hour)}, 24*time.Hour, []string{"cpu_usage_pct", "mem_used_pct"})
	if err != nil {
		t.Fatalf("LoadMonitoringHostEvidence all-false day: %v", err)
	}
	for _, bucket := range falseCapture.Buckets {
		for _, metric := range bucket.Metrics {
			if metric.Name == "cpu_usage_pct" {
				t.Fatalf("all-false daily evidence unexpectedly contains CPU metric: %#v", falseCapture)
			}
		}
	}
	if len(falseCapture.Buckets) != 1 || len(falseCapture.Buckets[0].Metrics) != 1 || falseCapture.Buckets[0].Metrics[0].Name != "mem_used_pct" {
		t.Fatalf("all-false daily evidence = %#v, want memory-only bucket", falseCapture)
	}
}

func assertCPUSamplingMixedEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
	repository := store.NewPostgresRuntimeFactsRepository(pool)
	capture, err := repository.LoadMonitoringHostEvidence(ctx, runtimeStreamAuthMonitoringID, evidence.TimeWindow{Start: day, End: day.Add(24 * time.Hour)}, time.Hour, []string{"cpu_usage_pct", "mem_used_pct"})
	if err != nil {
		t.Fatalf("LoadMonitoringHostEvidence mixed raw/daily day: %v", err)
	}
	if capture.ActualPrecision != 24*time.Hour || len(capture.Buckets) != 1 {
		t.Fatalf("mixed raw/daily capture precision/buckets = %s/%d, want 24h/1", capture.ActualPrecision, len(capture.Buckets))
	}
	bucket := capture.Buckets[0]
	if bucket.SourceLayer != adapters.MonitoringSourceMixed || bucket.SampleCount != 3 || bucket.SourceGranularity != 24*time.Hour {
		t.Fatalf("mixed raw/daily bucket metadata = layer:%q count:%d granularity:%s, want mixed/3/24h", bucket.SourceLayer, bucket.SampleCount, bucket.SourceGranularity)
	}
	assertCPUMetricCapture(t, capture, adapters.MonitoringSourceRaw, 2, 30, "mixed raw/daily CPU")
	assertMemoryMetricCapture(t, capture, 3, 15, "mixed raw/daily memory")

	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: runtimeStreamAuthUserID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatalf("normalize evidence actor: %v", err)
	}
	adapter, err := adapters.NewMonitoringHostAdapter(repository, cpuSamplingEvidenceResolver{}, adapters.AdapterOptions{Clock: time.Now})
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter: %v", err)
	}
	preview, err := adapter.PreviewCapture(ctx, actor, evidence.Selection{Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: runtimeStreamAuthMonitoringID, RequestedWindow: evidence.TimeWindow{Start: day, End: day.Add(24 * time.Hour)}, Metrics: []string{"cpu_usage_pct", "mem_used_pct"}, Precision: 24 * time.Hour})
	if err != nil {
		t.Fatalf("PreviewCapture mixed raw/daily: %v", err)
	}
	if preview.CalculationVersion != "monitoring-evidence/v2" || preview.Quality.SampleCount != 3 || preview.Quality.GapCount != 0 || preview.Quality.Partial {
		t.Fatalf("mixed raw/daily preview = calculation:%q quality:%#v, want v2 complete reference count 3", preview.CalculationVersion, preview.Quality)
	}
	t.Logf("mixed evidence: CPU metric count=2 avg=30 source=raw; memory metric count=3 avg=15 source=daily_aggregate; bucket source=mixed reference_count=%d quality=%#v", bucket.SampleCount, preview.Quality)
}

func assertCPUSamplingEvidenceGaps(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	falseDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -34)
	repository := store.NewPostgresRuntimeFactsRepository(pool)
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: runtimeStreamAuthUserID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatalf("normalize gap evidence actor: %v", err)
	}
	adapter, err := adapters.NewMonitoringHostAdapter(repository, cpuSamplingEvidenceResolver{}, adapters.AdapterOptions{Clock: time.Now})
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter for gap: %v", err)
	}
	preview, err := adapter.PreviewCapture(ctx, actor, evidence.Selection{Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: runtimeStreamAuthMonitoringID, RequestedWindow: evidence.TimeWindow{Start: falseDay, End: falseDay.Add(24 * time.Hour)}, Metrics: []string{"cpu_usage_pct", "mem_used_pct"}, Precision: 24 * time.Hour})
	if err != nil {
		t.Fatalf("PreviewCapture all-false CPU gap: %v", err)
	}
	if preview.Quality.GapCount == 0 || !preview.Quality.Partial || preview.Quality.SampleCount != 2 {
		t.Fatalf("all-false CPU gap quality = %#v, want metric gap with memory reference count 2", preview.Quality)
	}
	t.Logf("metric evidence gap: all-false CPU omitted while memory bucket remained; quality=%#v", preview.Quality)
}

func assertCPUMetricCapture(t *testing.T, capture adapters.MonitoringSeriesCapture, source adapters.MonitoringSourceLayer, sampleCount uint64, average float64, label string) {
	t.Helper()
	for _, bucket := range capture.Buckets {
		for _, metric := range bucket.Metrics {
			if metric.Name != "cpu_usage_pct" {
				continue
			}
			if metric.SourceLayer != source || metric.SampleCount != sampleCount || metric.Average == nil || *metric.Average != average {
				t.Fatalf("%s metric = %#v, want source=%q count=%d average=%v", label, metric, source, sampleCount, average)
			}
			return
		}
	}
	t.Fatalf("%s metric missing from capture %#v", label, capture)
}

func assertMemoryMetricCapture(t *testing.T, capture adapters.MonitoringSeriesCapture, sampleCount uint64, average float64, label string) {
	t.Helper()
	for _, bucket := range capture.Buckets {
		for _, metric := range bucket.Metrics {
			if metric.Name != "mem_used_pct" {
				continue
			}
			if metric.SampleCount != sampleCount || metric.Average == nil || *metric.Average != average {
				t.Fatalf("%s metric = %#v, want count=%d average=%v", label, metric, sampleCount, average)
			}
			return
		}
	}
	t.Fatalf("%s metric missing from capture %#v", label, capture)
}

type cpuSamplingEvidenceResolver struct{}

func (cpuSamplingEvidenceResolver) ResolveEvidenceSource(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) (adapters.ResolvedEvidenceSource, error) {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject, ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		return adapters.ResolvedEvidenceSource{}, err
	}
	sourceKind := recordauth.SourceKind(selection.SourceType)
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         sourceKind,
		SourceID:     selection.SourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		return adapters.ResolvedEvidenceSource{}, err
	}
	return adapters.ResolvedEvidenceSource{
		Subject:       evidence.IdentitySnapshot{Type: string(sourceKind), ID: selection.SourceID},
		Source:        evidence.IdentitySnapshot{Type: selection.SourceType, ID: selection.SourceID},
		Authorization: authorization,
	}, nil
}

func boolPointerString(value *bool) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%t", *value)
}

func assertCPUSamplingMigrationPreservesLegacyRows(t *testing.T, ctx context.Context) {
	t.Helper()
	db := openCPUSamplingPartialMigrationDatabase(t, ctx)
	legacyDay := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.Exec(ctx, `
		insert into vps_assets (vps_id, display_name, lifecycle_status) values ('vps_cpu_migration', 'CPU migration', 'active')`); err != nil {
		t.Fatalf("seed pre-0069 VPS: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into monitoring_instances (monitoring_instance_id, vps_id, display_name, region, city, provider, lifecycle_status, monitoring_status, binding_status, binding_fingerprint, binding_epoch_started_at)
		values ('mi_cpu_migration', 'vps_cpu_migration', 'CPU migration', '', '', '', '已接入', '启用', '已绑定', 'fp-cpu-migration', $1)`, legacyDay); err != nil {
		t.Fatalf("seed pre-0069 monitoring instance: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into host_samples (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec, net_out_bytes_per_sec,
			cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
			disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
		) values ('mi_cpu_migration', $1, $1, 'legacy-agent', 'fp-cpu-migration', 77, 1, 1, 1, 55, 10, 0, 1, 1, 1, 1, 2, 3, 1, 1, 1, 1, false, false, 'legacy-cpu-row')`, legacyDay.Add(time.Hour)); err != nil {
		t.Fatalf("seed pre-0069 host row: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into monitoring_instance_host_sample_daily_aggregates (
			monitoring_instance_id, bucket_date, sample_count, avg_cpu_usage_pct, max_cpu_usage_pct,
			avg_load_5, max_load_5, avg_mem_used_pct, max_mem_used_pct, avg_cpu_iowait_pct,
			max_cpu_iowait_pct, avg_cpu_steal_pct, max_cpu_steal_pct, avg_disk_busy_pct,
			max_disk_busy_pct, backfilled_sample_count, maintenance_sample_count, finalized
		) values ('mi_cpu_migration', $1::date, 9, 88, 90, 1, 2, 55, 60, 4, 5, 6, 7, 8, 9, 0, 0, true)`, legacyDay); err != nil {
		t.Fatalf("seed pre-0069 finalized aggregate: %v", err)
	}
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply 0069+ migrations after legacy rows: %v", err)
	}
	var marker sql.NullBool
	var sampleCount int
	var avgCPU float64
	var cpuValid sql.NullInt32
	var finalized bool
	if err := db.QueryRow(ctx, `select cpu_rates_valid from host_samples where sync_batch_id = 'legacy-cpu-row'`).Scan(&marker); err != nil {
		t.Fatalf("read migrated legacy host marker: %v", err)
	}
	if err := db.QueryRow(ctx, `select sample_count, avg_cpu_usage_pct, cpu_valid_sample_count, finalized from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = 'mi_cpu_migration' and bucket_date = $1`, legacyDay).Scan(&sampleCount, &avgCPU, &cpuValid, &finalized); err != nil {
		t.Fatalf("read migrated legacy aggregate: %v", err)
	}
	if marker.Valid || sampleCount != 9 || avgCPU != 88 || cpuValid.Valid || !finalized {
		t.Fatalf("migrated legacy values = marker:%#v count:%d avg:%v cpu_count:%#v finalized:%v, want NULL marker/count and unchanged finalized aggregate", marker, sampleCount, avgCPU, cpuValid, finalized)
	}
	t.Logf("migration evidence: pre-0069 host marker remained NULL; finalized legacy aggregate remained count=%d avg_cpu=%v finalized=%v", sampleCount, avgCPU, finalized)
}

func openCPUSamplingPartialMigrationDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Fatalf("HOUFENG_DATABASE_URL is required for CPU sampling migration integration")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_cpu_sampling_migration_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated migration database name %q", databaseName)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open migration admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := pgx.Identifier{databaseName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create migration database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop migration database: %v", err)
		}
	})
	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	db, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(db.Close)
	if err := applyCPUSamplingMigrationsThrough(t, ctx, db, "0068_normalize_ip_quality_host_address_identity.sql"); err != nil {
		t.Fatalf("apply migrations through 0068: %v", err)
	}
	return db
}

func applyCPUSamplingMigrationsThrough(t *testing.T, ctx context.Context, db *pgxpool.Pool, through string) error {
	t.Helper()
	if _, err := db.Exec(ctx, `create table if not exists schema_migrations (name text primary key, checksum text not null, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	names, err := storemigrate.Names()
	if err != nil {
		return err
	}
	found := false
	for _, name := range names {
		if name > through {
			break
		}
		payload, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		digest := sha256.Sum256(payload)
		tx, err := db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(payload)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations (name, checksum) values ($1, $2)`, name, hex.EncodeToString(digest[:])); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		if name == through {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("migration %q not found", through)
	}
	return nil
}

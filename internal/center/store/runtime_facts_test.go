package store

import (
	"context"
	stdsql "database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

func TestPostgresRuntimeFactsRepositoryImplementsRuntimeFactsRepository(t *testing.T) {
	t.Parallel()

	var repo runtimefacts.Repository = (*PostgresRuntimeFactsRepository)(nil)
	if repo == nil {
		t.Fatal("repository interface assignment returned nil")
	}
}

func TestGetMonitoringInstanceRuntimeFactsReturnsLatestHostSampleAndHostMetricPoints(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	startedAt := observedAt.Add(-24 * time.Hour)
	endedAt := observedAt
	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsMonitoringInstanceExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			case runtimeFactsLatestHostSampleSQL:
				invalid := false
				return fakeRuntimeFactsHostSampleRow(fakeHostSampleValues{
					MonitoringInstanceID: "mi_001",
					ObservedAt:           observedAt,
					ReceivedAt:           observedAt.Add(2 * time.Second),
					AgentVersion:         "agent/v0.1.0",
					Fingerprint:          "fp-001",
					CPUUsagePct:          50,
					Load1:                0.8,
					Load5:                0.7,
					Load15:               0.6,
					MemUsedPct:           72,
					MemAvailableBytes:    2147483648,
					MemTotalBytes:        8589934592,
					DiskUsedPct:          61,
					DiskTotalBytes:       107374182400,
					InodeUsedPct:         22,
					NetInBytesPerSec:     1200,
					NetOutBytesPerSec:    900,
					NetworkRatesValid:    &invalid,
					CPUIOWaitPct:         1.2,
					CPUStealPct:          0.1,
					DiskReadBytesPerSec:  400,
					DiskWriteBytesPerSec: 300,
					DiskBusyPct:          5,
					UptimeSeconds:        3600,
					SyncBatchID:          "sync_001",
				})
			case runtimeFactsHostSampleWindowSummarySQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*stdsql.NullTime)) = stdsql.NullTime{Time: observedAt.Add(-5 * time.Minute), Valid: true}
					*(dest[1].(*stdsql.NullTime)) = stdsql.NullTime{Time: observedAt, Valid: true}
					*(dest[2].(*int)) = 24
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != runtimeFactsHostMetricPointsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeRuntimeFactsRows{rows: []fakeRuntimeFactsScan{
				{scan: func(dest ...any) error {
					*(dest[0].(*time.Time)) = observedAt.Add(-5 * time.Minute)
					*(dest[1].(*int)) = 12
					setFakeRuntimeFactsFloat(dest[2], 45)
					setFakeRuntimeFactsFloat(dest[3], 65)
					setFakeRuntimeFactsFloat(dest[4], 52)
					setFakeRuntimeFactsFloat(dest[5], 15)
					setFakeRuntimeFactsFloat(dest[6], 0.42)
					setFakeRuntimeFactsFloat(dest[7], 0.3)
					setFakeRuntimeFactsFloat(dest[8], 1024)
					setFakeRuntimeFactsFloat(dest[9], 2048)
					return nil
				}},
				{scan: func(dest ...any) error {
					*(dest[0].(*time.Time)) = observedAt
					*(dest[1].(*int)) = 12
					setFakeRuntimeFactsFloat(dest[2], 50)
					setFakeRuntimeFactsFloat(dest[3], 72)
					setFakeRuntimeFactsFloat(dest[4], 61)
					setFakeRuntimeFactsFloat(dest[5], 22)
					setFakeRuntimeFactsFloat(dest[6], 0.7)
					setFakeRuntimeFactsFloat(dest[7], 1.2)
					setFakeRuntimeFactsFloat(dest[8], 1200)
					setFakeRuntimeFactsFloat(dest[9], 900)
					return nil
				}},
			}}, nil
		},
	}}

	facts, err := repo.GetMonitoringInstanceRuntimeFacts(context.Background(), "mi_001", runtimefacts.WindowRequest{
		Key:         "24h",
		StartedAt:   startedAt,
		EndedAt:     endedAt,
		BucketCount: 288,
	})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts() error = %v", err)
	}
	if facts.MonitoringInstanceID != "mi_001" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", facts.MonitoringInstanceID, "mi_001")
	}
	if facts.ReadAt.IsZero() {
		t.Fatal("ReadAt = zero, want a server snapshot timestamp")
	}
	if facts.LatestHostSample == nil {
		t.Fatal("LatestHostSample = nil, want non-nil")
	}
	if facts.LatestHostSample.NetworkRatesValid == nil || *facts.LatestHostSample.NetworkRatesValid {
		t.Fatalf("LatestHostSample.NetworkRatesValid = %v, want explicit false", facts.LatestHostSample.NetworkRatesValid)
	}
	if facts.LatestHostSample.AgentVersion != "agent/v0.1.0" {
		t.Fatalf("LatestHostSample.AgentVersion = %q, want %q", facts.LatestHostSample.AgentVersion, "agent/v0.1.0")
	}
	if facts.LatestHostSample.CPUIOWaitPct != 1.2 {
		t.Fatalf("LatestHostSample.CPUIOWaitPct = %v, want %v", facts.LatestHostSample.CPUIOWaitPct, 1.2)
	}
	if facts.LatestHostSample.MemTotalBytes != 8589934592 {
		t.Fatalf("LatestHostSample.MemTotalBytes = %d, want %d", facts.LatestHostSample.MemTotalBytes, int64(8589934592))
	}
	if facts.LatestHostSample.DiskTotalBytes != 107374182400 {
		t.Fatalf("LatestHostSample.DiskTotalBytes = %d, want %d", facts.LatestHostSample.DiskTotalBytes, int64(107374182400))
	}
	if facts.Window.Key != "24h" || facts.Window.BucketCount != 288 {
		t.Fatalf("Window = %#v, want 24h/288 buckets", facts.Window)
	}
	if facts.Window.SampleCount != 24 {
		t.Fatalf("Window.SampleCount = %d, want 24", facts.Window.SampleCount)
	}
	if facts.Window.AvailableStartedAt == nil || !facts.Window.AvailableStartedAt.Equal(observedAt.Add(-5*time.Minute)) {
		t.Fatalf("Window.AvailableStartedAt = %v, want %v", facts.Window.AvailableStartedAt, observedAt.Add(-5*time.Minute))
	}
	if facts.Window.AvailableEndedAt == nil || !facts.Window.AvailableEndedAt.Equal(observedAt) {
		t.Fatalf("Window.AvailableEndedAt = %v, want %v", facts.Window.AvailableEndedAt, observedAt)
	}
	if len(facts.RecentHostSamples) != 0 {
		t.Fatalf("len(RecentHostSamples) = %d, want 0", len(facts.RecentHostSamples))
	}
	if len(facts.HostMetricPoints) != 2 {
		t.Fatalf("len(HostMetricPoints) = %d, want 2", len(facts.HostMetricPoints))
	}
	if got := facts.HostMetricPoints[0].ObservedAt; !got.Equal(observedAt.Add(-5 * time.Minute)) {
		t.Fatalf("HostMetricPoints[0].ObservedAt = %v, want %v", got, observedAt.Add(-5*time.Minute))
	}
	if got := facts.HostMetricPoints[1].NetOutBytesPerSec; got == nil || *got != 900 {
		t.Fatalf("HostMetricPoints[1].NetOutBytesPerSec = %v, want 900", got)
	}
}

func TestGetMonitoringInstanceRuntimeFactsReturnsRealtimeRecentHostSamples(t *testing.T) {
	t.Parallel()

	endedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	startedAt := endedAt.Add(-time.Hour)
	older := fakeHostSampleValues{
		MonitoringInstanceID: "mi_001",
		ObservedAt:           endedAt.Add(-30 * time.Minute),
		ReceivedAt:           endedAt.Add(-30*time.Minute + 2*time.Second),
		AgentVersion:         "agent/v0.1.0",
		Fingerprint:          "fp-001",
		CPUUsagePct:          42,
		MemUsedPct:           63,
		DiskUsedPct:          51,
		InodeUsedPct:         17,
		Load5:                0.8,
		CPUIOWaitPct:         0.2,
		NetInBytesPerSec:     1024,
		NetOutBytesPerSec:    2048,
		SyncBatchID:          "sync_001",
	}
	latest := older
	latest.ObservedAt = endedAt.Add(-5 * time.Minute)
	latest.ReceivedAt = latest.ObservedAt.Add(2 * time.Second)
	latest.CPUUsagePct = 55
	latest.SyncBatchID = "sync_002"

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsMonitoringInstanceExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			case runtimeFactsLatestHostSampleSQL:
				return fakeRuntimeFactsHostSampleRow(latest)
			case runtimeFactsHostSampleWindowSummarySQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*stdsql.NullTime)) = stdsql.NullTime{Time: older.ObservedAt, Valid: true}
					*(dest[1].(*stdsql.NullTime)) = stdsql.NullTime{Time: latest.ObservedAt, Valid: true}
					*(dest[2].(*int)) = 2
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			switch sql {
			case runtimeFactsHostMetricPointsSQL:
				return &fakeRuntimeFactsRows{}, nil
			case runtimeFactsRecentHostSamplesSQL:
				if args[1] != startedAt || args[2] != endedAt {
					t.Fatalf("recent host sample args = %#v, want started/end window", args)
				}
				return &fakeRuntimeFactsRows{rows: []fakeRuntimeFactsScan{
					fakeRuntimeFactsHostSampleScan(older),
					fakeRuntimeFactsHostSampleScan(latest),
				}}, nil
			default:
				return nil, errors.New("unexpected Query")
			}
		},
	}}

	facts, err := repo.GetMonitoringInstanceRuntimeFacts(context.Background(), "mi_001", runtimefacts.WindowRequest{
		Key:         "realtime",
		StartedAt:   startedAt,
		EndedAt:     endedAt,
		BucketCount: 720,
	})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts() error = %v", err)
	}
	if facts.Window.Key != "realtime" || facts.Window.BucketCount != 720 {
		t.Fatalf("Window = %#v, want realtime/720 buckets", facts.Window)
	}
	if len(facts.RecentHostSamples) != 2 {
		t.Fatalf("len(RecentHostSamples) = %d, want 2", len(facts.RecentHostSamples))
	}
	if got := facts.RecentHostSamples[0].ObservedAt; !got.Equal(older.ObservedAt) {
		t.Fatalf("RecentHostSamples[0].ObservedAt = %v, want %v", got, older.ObservedAt)
	}
	if got := facts.RecentHostSamples[1].SyncBatchID; got != "sync_002" {
		t.Fatalf("RecentHostSamples[1].SyncBatchID = %q, want sync_002", got)
	}
	if got := facts.RecentHostSamples[1].CPUUsagePct; got != 55 {
		t.Fatalf("RecentHostSamples[1].CPUUsagePct = %v, want 55", got)
	}
}

func TestGetMonitoringInstanceRuntimeFactsReturnsNilHostSampleWhenMonitoringInstanceHasNoFactsYet(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsMonitoringInstanceExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			case runtimeFactsLatestHostSampleSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			case runtimeFactsHostSampleWindowSummarySQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[2].(*int)) = 0
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != runtimeFactsHostMetricPointsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeRuntimeFactsRows{rows: emptyFakeRuntimeFactsMetricRows(288)}, nil
		},
	}}

	now := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	facts, err := repo.GetMonitoringInstanceRuntimeFacts(context.Background(), "mi_001", runtimefacts.WindowRequest{
		Key:         "24h",
		StartedAt:   now.Add(-24 * time.Hour),
		EndedAt:     now,
		BucketCount: 288,
	})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts() error = %v", err)
	}
	if facts.LatestHostSample != nil {
		t.Fatalf("LatestHostSample = %#v, want nil", facts.LatestHostSample)
	}
	if facts.RecentHostSamples == nil {
		t.Fatal("RecentHostSamples = nil, want empty slice")
	}
	if len(facts.HostMetricPoints) != 288 {
		t.Fatalf("len(HostMetricPoints) = %d, want one point per configured bucket", len(facts.HostMetricPoints))
	}
	if facts.HostMetricPoints[0].SampleCount != 0 || facts.HostMetricPoints[0].CPUUsagePct != nil || facts.HostMetricPoints[0].NetOutBytesPerSec != nil {
		t.Fatalf("empty bucket = %#v, want zero count with null metrics", facts.HostMetricPoints[0])
	}
	if facts.Window.SampleCount != 0 {
		t.Fatalf("Window.SampleCount = %d, want 0", facts.Window.SampleCount)
	}
}

func TestGetMonitoringInstanceRuntimeFactsReturnsMonitoringInstanceNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRuntimeFactsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	now := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	_, err := repo.GetMonitoringInstanceRuntimeFacts(context.Background(), "mi_missing", runtimefacts.WindowRequest{
		Key:         "24h",
		StartedAt:   now.Add(-24 * time.Hour),
		EndedAt:     now,
		BucketCount: 288,
	})
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts() error = %v, want ErrMonitoringInstanceNotFound", err)
	}
}

func TestGetMonitoringInstanceRuntimeFactsRejectsInvalidWindowBeforeQuerying(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			t.Fatal("QueryRow called for invalid window")
			return fakeRuntimeFactsRow{}
		},
		query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			t.Fatal("Query called for invalid window")
			return nil, nil
		},
	}}

	now := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	_, err := repo.GetMonitoringInstanceRuntimeFacts(context.Background(), "mi_001", runtimefacts.WindowRequest{
		Key:         "bad",
		StartedAt:   now,
		EndedAt:     now,
		BucketCount: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid monitoring runtime window") {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts() error = %v, want invalid window", err)
	}
}

func TestGetTargetRuntimeFactsReturnsLatestProbeObservationsAndRecentProbeObservations(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 24, 9, 5, 0, 0, time.UTC)
	latency := 0
	httpStatus := 200
	tlsExpiryDays := 14
	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsTargetExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			switch sql {
			case runtimeFactsLatestProbeObservationsSQL:
				return &fakeRuntimeFactsRows{rows: []fakeRuntimeFactsScan{{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "mi_001"
					*(dest[1].(*string)) = "tg_001"
					*(dest[2].(*string)) = "pb_001"
					*(dest[3].(*string)) = "http"
					*(dest[4].(*time.Time)) = observedAt
					*(dest[5].(*time.Time)) = observedAt.Add(1500 * time.Millisecond)
					*(dest[6].(*string)) = "agent/v0.1.0"
					*(dest[7].(*string)) = "fp-001"
					*(dest[8].(*string)) = "success"
					*(dest[9].(**int)) = &latency
					*(dest[10].(**int)) = &httpStatus
					*(dest[11].(**int)) = &tlsExpiryDays
					*(dest[12].(*string)) = ""
					*(dest[13].(*string)) = ""
					*(dest[14].(*bool)) = false
					*(dest[15].(*bool)) = false
					*(dest[16].(*string)) = "sync_001"
					return nil
				}}}}, nil
			case runtimeFactsRecentProbeObservationsSQL:
				return &fakeRuntimeFactsRows{rows: []fakeRuntimeFactsScan{
					{scan: func(dest ...any) error {
						*(dest[0].(*string)) = "mi_001"
						*(dest[1].(*string)) = "tg_001"
						*(dest[2].(*string)) = "pb_001"
						*(dest[3].(*string)) = "http"
						*(dest[4].(*time.Time)) = observedAt
						*(dest[5].(*time.Time)) = observedAt.Add(1500 * time.Millisecond)
						*(dest[6].(*string)) = "agent/v0.1.0"
						*(dest[7].(*string)) = "fp-001"
						*(dest[8].(*string)) = "success"
						*(dest[9].(**int)) = &latency
						*(dest[10].(**int)) = &httpStatus
						*(dest[11].(**int)) = &tlsExpiryDays
						*(dest[12].(*string)) = ""
						*(dest[13].(*string)) = ""
						*(dest[14].(*bool)) = false
						*(dest[15].(*bool)) = false
						*(dest[16].(*string)) = "sync_001"
						return nil
					}},
					{scan: func(dest ...any) error {
						older := observedAt.Add(-10 * time.Minute)
						olderLatency := 42
						olderStatus := 200
						olderTLS := 13
						*(dest[0].(*string)) = "mi_002"
						*(dest[1].(*string)) = "tg_001"
						*(dest[2].(*string)) = "pb_002"
						*(dest[3].(*string)) = "http"
						*(dest[4].(*time.Time)) = older
						*(dest[5].(*time.Time)) = older.Add(1200 * time.Millisecond)
						*(dest[6].(*string)) = "agent/v0.0.9"
						*(dest[7].(*string)) = "fp-002"
						*(dest[8].(*string)) = "success"
						*(dest[9].(**int)) = &olderLatency
						*(dest[10].(**int)) = &olderStatus
						*(dest[11].(**int)) = &olderTLS
						*(dest[12].(*string)) = ""
						*(dest[13].(*string)) = ""
						*(dest[14].(*bool)) = false
						*(dest[15].(*bool)) = false
						*(dest[16].(*string)) = "sync_000"
						return nil
					}},
				}}, nil
			default:
				return nil, errors.New("unexpected Query")
			}
		},
	}}

	facts, err := repo.GetTargetRuntimeFacts(context.Background(), "tg_001", time.Now().Add(-24*time.Hour), 500)
	if err != nil {
		t.Fatalf("GetTargetRuntimeFacts() error = %v", err)
	}
	if facts.TargetID != "tg_001" {
		t.Fatalf("TargetID = %q, want %q", facts.TargetID, "tg_001")
	}
	if len(facts.LatestProbeObservations) != 1 {
		t.Fatalf("len(LatestProbeObservations) = %d, want 1", len(facts.LatestProbeObservations))
	}
	observation := facts.LatestProbeObservations[0]
	if observation.ProbeKind != "http" {
		t.Fatalf("ProbeKind = %q, want %q", observation.ProbeKind, "http")
	}
	if observation.LatencyMS == nil || *observation.LatencyMS != 0 {
		t.Fatalf("LatencyMS = %v, want 0", observation.LatencyMS)
	}
	if observation.HTTPStatus == nil || *observation.HTTPStatus != 200 {
		t.Fatalf("HTTPStatus = %v, want 200", observation.HTTPStatus)
	}
	if observation.TLSExpiryDays == nil || *observation.TLSExpiryDays != 14 {
		t.Fatalf("TLSExpiryDays = %v, want 14", observation.TLSExpiryDays)
	}
	if len(facts.RecentProbeObservations) != 2 {
		t.Fatalf("len(RecentProbeObservations) = %d, want 2", len(facts.RecentProbeObservations))
	}
	if got := facts.RecentProbeObservations[0].ObservedAt; !got.Equal(observedAt) {
		t.Fatalf("RecentProbeObservations[0].ObservedAt = %v, want %v", got, observedAt)
	}
	if got := facts.RecentProbeObservations[1].MonitoringInstanceID; got != "mi_002" {
		t.Fatalf("RecentProbeObservations[1].MonitoringInstanceID = %q, want %q", got, "mi_002")
	}
}

func TestGetTargetRuntimeFactsReturnsEmptyFactsForKnownTarget(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsTargetExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
		},
		query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &fakeRuntimeFactsRows{}, nil
		},
	}}

	facts, err := repo.GetTargetRuntimeFacts(context.Background(), "tg_001", time.Now().Add(-24*time.Hour), 500)
	if err != nil {
		t.Fatalf("GetTargetRuntimeFacts() error = %v", err)
	}
	if len(facts.LatestProbeObservations) != 0 {
		t.Fatalf("len(LatestProbeObservations) = %d, want 0", len(facts.LatestProbeObservations))
	}
	if facts.RecentProbeObservations == nil {
		t.Fatal("RecentProbeObservations = nil, want empty slice")
	}
	if len(facts.RecentProbeObservations) != 0 {
		t.Fatalf("len(RecentProbeObservations) = %d, want 0", len(facts.RecentProbeObservations))
	}
}

func TestGetTargetRuntimeFactsReturnsTargetNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRuntimeFactsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	_, err := repo.GetTargetRuntimeFacts(context.Background(), "tg_missing", time.Now().Add(-24*time.Hour), 500)
	if !errors.Is(err, targets.ErrTargetNotFound) {
		t.Fatalf("GetTargetRuntimeFacts() error = %v, want ErrTargetNotFound", err)
	}
}

type fakeRuntimeFactsQueryer struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func (f fakeRuntimeFactsQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

func (f fakeRuntimeFactsQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeRuntimeFactsRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

type fakeRuntimeFactsRow struct {
	scan func(dest ...any) error
}

func (f fakeRuntimeFactsRow) Scan(dest ...any) error {
	return f.scan(dest...)
}

type fakeRuntimeFactsScan struct {
	scan func(dest ...any) error
}

type fakeRuntimeFactsRows struct {
	rows []fakeRuntimeFactsScan
	idx  int
	err  error
}

func (f *fakeRuntimeFactsRows) Close()                                       {}
func (f *fakeRuntimeFactsRows) Err() error                                   { return f.err }
func (f *fakeRuntimeFactsRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeRuntimeFactsRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRuntimeFactsRows) RawValues() [][]byte                          { return nil }
func (f *fakeRuntimeFactsRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeRuntimeFactsRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeRuntimeFactsRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeRuntimeFactsRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

type fakeHostSampleValues struct {
	MonitoringInstanceID string
	ObservedAt           time.Time
	ReceivedAt           time.Time
	AgentVersion         string
	Fingerprint          string
	CPUUsagePct          float64
	Load1                float64
	Load5                float64
	Load15               float64
	MemUsedPct           float64
	MemAvailableBytes    int64
	MemTotalBytes        int64
	SwapUsedPct          float64
	DiskUsedPct          float64
	DiskTotalBytes       int64
	InodeUsedPct         float64
	NetInBytesPerSec     int64
	NetOutBytesPerSec    int64
	NetworkRatesValid    *bool
	CPUIOWaitPct         float64
	CPUStealPct          float64
	DiskReadBytesPerSec  int64
	DiskWriteBytesPerSec int64
	DiskBusyPct          float64
	UptimeSeconds        int64
	SyncBatchID          string
}

func fakeRuntimeFactsHostSampleRow(values fakeHostSampleValues) pgx.Row {
	return fakeRuntimeFactsRow{scan: func(dest ...any) error {
		return scanFakeRuntimeFactsHostSample(dest, values)
	}}
}

func fakeRuntimeFactsHostSampleScan(values fakeHostSampleValues) fakeRuntimeFactsScan {
	return fakeRuntimeFactsScan{scan: func(dest ...any) error {
		return scanFakeRuntimeFactsHostSample(dest, values)
	}}
}

func scanFakeRuntimeFactsHostSample(dest []any, values fakeHostSampleValues) error {
	*(dest[0].(*string)) = values.MonitoringInstanceID
	*(dest[1].(*time.Time)) = values.ObservedAt
	*(dest[2].(*time.Time)) = values.ReceivedAt
	*(dest[3].(*string)) = values.AgentVersion
	*(dest[4].(*string)) = values.Fingerprint
	*(dest[5].(*float64)) = values.CPUUsagePct
	*(dest[6].(*float64)) = values.Load1
	*(dest[7].(*float64)) = values.Load5
	*(dest[8].(*float64)) = values.Load15
	*(dest[9].(*float64)) = values.MemUsedPct
	*(dest[10].(*int64)) = values.MemAvailableBytes
	*(dest[11].(*int64)) = values.MemTotalBytes
	*(dest[12].(*float64)) = values.SwapUsedPct
	*(dest[13].(*float64)) = values.DiskUsedPct
	*(dest[14].(*int64)) = values.DiskTotalBytes
	*(dest[15].(*float64)) = values.InodeUsedPct
	*(dest[16].(*int64)) = values.NetInBytesPerSec
	*(dest[17].(*int64)) = values.NetOutBytesPerSec
	marker := dest[18].(*stdsql.NullBool)
	if values.NetworkRatesValid != nil {
		marker.Valid = true
		marker.Bool = *values.NetworkRatesValid
	}
	*(dest[19].(*float64)) = values.CPUIOWaitPct
	*(dest[20].(*float64)) = values.CPUStealPct
	*(dest[21].(*int64)) = values.DiskReadBytesPerSec
	*(dest[22].(*int64)) = values.DiskWriteBytesPerSec
	*(dest[23].(*float64)) = values.DiskBusyPct
	*(dest[24].(*int64)) = values.UptimeSeconds
	*(dest[25].(*bool)) = false
	*(dest[26].(*bool)) = false
	*(dest[27].(*string)) = values.SyncBatchID
	*(dest[28].(*[]byte)) = []byte("[]")
	return nil
}

func setFakeRuntimeFactsFloat(dest any, value float64) {
	*(dest.(*stdsql.NullFloat64)) = stdsql.NullFloat64{Float64: value, Valid: true}
}
func emptyFakeRuntimeFactsMetricRows(count int) []fakeRuntimeFactsScan {
	rows := make([]fakeRuntimeFactsScan, count)
	for index := range rows {
		rows[index] = fakeRuntimeFactsScan{scan: func(dest ...any) error {
			*(dest[0].(*time.Time)) = time.Time{}
			*(dest[1].(*int)) = 0
			for _, value := range dest[2:] {
				*(value.(*stdsql.NullFloat64)) = stdsql.NullFloat64{}
			}
			return nil
		}}
	}
	return rows
}

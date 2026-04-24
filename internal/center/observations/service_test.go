package observations

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type fakeRepo struct {
	calls []BatchWrite
}

func (f *fakeRepo) RecordBatch(_ context.Context, batch BatchWrite) error {
	f.calls = append(f.calls, batch)
	return nil
}

func TestIngestRuntimeFactsWritesHostSamplesAndProbeObservations(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 4, 24, 8, 0, 0, 0, time.UTC)
	receivedAt := time.Date(2026, 4, 24, 8, 0, 5, 0, time.UTC)
	latencyMS := 123
	httpStatus := 200
	repo := &fakeRepo{}
	service := NewService(repo)
	batch := BatchWrite{
		NodeID: "nd_001",
		HostSamples: []HostSampleWrite{{
			ObservedAt:           observedAt,
			ReceivedAt:           receivedAt,
			CPUUsagePct:          17.2,
			Load1:                0.41,
			Load5:                0.38,
			Load15:               0.35,
			MemUsedPct:           62.4,
			MemAvailableBytes:    8_589_934_592,
			SwapUsedPct:          3.5,
			DiskUsedPct:          54.1,
			InodeUsedPct:         12.2,
			NetInBytesPerSec:     2_048,
			NetOutBytesPerSec:    4_096,
			CPUIOWaitPct:         1.2,
			CPUStealPct:          0.0,
			DiskReadBytesPerSec:  1_024,
			DiskWriteBytesPerSec: 512,
			DiskBusyPct:          7.5,
			UptimeSeconds:        86_400,
			MaintenanceContext:   false,
			IsBackfilled:         false,
			SyncBatchID:          "sync_001",
		}},
		ProbeObservations: []ProbeObservationWrite{{
			TargetID:           "tg_001",
			ProbeItemID:        "pb_001",
			ProbeKind:          "http",
			ObservedAt:         observedAt,
			ReceivedAt:         receivedAt,
			ResultKind:         "success",
			LatencyMS:          &latencyMS,
			HTTPStatus:         &httpStatus,
			MaintenanceContext: false,
			IsBackfilled:       false,
			SyncBatchID:        "sync_001",
		}},
	}

	if err := service.Ingest(context.Background(), batch); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	if len(repo.calls) != 1 {
		t.Fatalf("len(repo.calls) = %d, want 1", len(repo.calls))
	}
	if !reflect.DeepEqual(repo.calls[0], batch) {
		t.Fatalf("repo.calls[0] = %#v, want %#v", repo.calls[0], batch)
	}
}

package observations

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeRepo struct {
	calls              []BatchWrite
	probeMetadataByID  map[string]ProbeMetadata
	getProbeMetadataID []string
}

func (f *fakeRepo) RecordBatch(_ context.Context, batch BatchWrite) error {
	f.calls = append(f.calls, batch)
	return nil
}

func (f *fakeRepo) GetProbeMetadata(_ context.Context, probeItemID string) (ProbeMetadata, error) {
	f.getProbeMetadataID = append(f.getProbeMetadataID, probeItemID)

	metadata, ok := f.probeMetadataByID[probeItemID]
	if !ok {
		return ProbeMetadata{}, ErrProbeMetadataNotFound
	}

	return metadata, nil
}

func TestIngestRuntimeFactsWritesHostSamplesAndProbeObservations(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 4, 24, 8, 0, 0, 0, time.UTC)
	receivedAt := time.Date(2026, 4, 24, 8, 0, 5, 0, time.UTC)
	latencyMS := 123
	httpStatus := 200
	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_001": {
				TargetID:  "tg_001",
				ProbeKind: "http",
			},
		},
	}
	service := NewService(repo, repo)
	batch := BatchWrite{
		MonitoringInstanceID: "mi_batch_should_not_drive_store_writes",
		HostSamples: []HostSampleWrite{{
			MonitoringInstanceID: "mi_001",
			ObservedAt:           observedAt,
			ReceivedAt:           receivedAt,
			AgentVersion:         "agent/v0.1.0",
			Fingerprint:          "fp_host_001",
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
			MonitoringInstanceID: "mi_001",
			TargetID:             "tg_001",
			ProbeItemID:          "pb_001",
			ProbeKind:            "http",
			ObservedAt:           observedAt,
			ReceivedAt:           receivedAt,
			AgentVersion:         "agent/v0.1.0",
			Fingerprint:          "fp_probe_001",
			ResultKind:           "success",
			LatencyMS:            &latencyMS,
			HTTPStatus:           &httpStatus,
			MaintenanceContext:   false,
			IsBackfilled:         false,
			SyncBatchID:          "sync_001",
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
	if repo.calls[0].HostSamples[0].MonitoringInstanceID != "mi_001" {
		t.Fatalf("HostSamples[0].MonitoringInstanceID = %q, want %q", repo.calls[0].HostSamples[0].MonitoringInstanceID, "mi_001")
	}
	if repo.calls[0].HostSamples[0].AgentVersion != "agent/v0.1.0" {
		t.Fatalf("HostSamples[0].AgentVersion = %q, want %q", repo.calls[0].HostSamples[0].AgentVersion, "agent/v0.1.0")
	}
	if repo.calls[0].HostSamples[0].Fingerprint != "fp_host_001" {
		t.Fatalf("HostSamples[0].Fingerprint = %q, want %q", repo.calls[0].HostSamples[0].Fingerprint, "fp_host_001")
	}
	if repo.calls[0].ProbeObservations[0].MonitoringInstanceID != "mi_001" {
		t.Fatalf("ProbeObservations[0].MonitoringInstanceID = %q, want %q", repo.calls[0].ProbeObservations[0].MonitoringInstanceID, "mi_001")
	}
	if repo.calls[0].ProbeObservations[0].AgentVersion != "agent/v0.1.0" {
		t.Fatalf("ProbeObservations[0].AgentVersion = %q, want %q", repo.calls[0].ProbeObservations[0].AgentVersion, "agent/v0.1.0")
	}
	if repo.calls[0].ProbeObservations[0].Fingerprint != "fp_probe_001" {
		t.Fatalf("ProbeObservations[0].Fingerprint = %q, want %q", repo.calls[0].ProbeObservations[0].Fingerprint, "fp_probe_001")
	}
}

func TestIngestRejectsProbeObservationTargetMismatch(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_001": {
				TargetID:  "tg_expected",
				ProbeKind: "http",
			},
		},
	}
	service := NewService(repo, repo)

	err := service.Ingest(context.Background(), BatchWrite{
		ProbeObservations: []ProbeObservationWrite{{
			ProbeItemID: "pb_001",
			TargetID:    "tg_actual",
			ProbeKind:   "http",
		}},
	})
	if !errors.Is(err, ErrInvalidProbeObservation) {
		t.Fatalf("Ingest() error = %v, want ErrInvalidProbeObservation", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(repo.calls) = %d, want 0", len(repo.calls))
	}
}

func TestIngestRejectsProbeObservationKindMismatch(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_001": {
				TargetID:  "tg_001",
				ProbeKind: "tls",
			},
		},
	}
	service := NewService(repo, repo)

	err := service.Ingest(context.Background(), BatchWrite{
		ProbeObservations: []ProbeObservationWrite{{
			ProbeItemID: "pb_001",
			TargetID:    "tg_001",
			ProbeKind:   "http",
		}},
	})
	if !errors.Is(err, ErrInvalidProbeObservation) {
		t.Fatalf("Ingest() error = %v, want ErrInvalidProbeObservation", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(repo.calls) = %d, want 0", len(repo.calls))
	}
}

func TestIngestRejectsUnknownProbeResultKind(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_001": {
				TargetID:  "tg_001",
				ProbeKind: "http",
			},
		},
	}
	service := NewService(repo, repo)

	err := service.Ingest(context.Background(), BatchWrite{
		ProbeObservations: []ProbeObservationWrite{{
			ProbeItemID: "pb_001",
			TargetID:    "tg_001",
			ProbeKind:   "http",
			ResultKind:  "maybe",
		}},
	})
	if !errors.Is(err, ErrInvalidProbeObservation) {
		t.Fatalf("Ingest() error = %v, want ErrInvalidProbeObservation", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(repo.calls) = %d, want 0", len(repo.calls))
	}
}

func TestIngestRejectsFailureObservationWithoutErrorCode(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_001": {
				TargetID:  "tg_001",
				ProbeKind: "http",
			},
		},
	}
	service := NewService(repo, repo)

	err := service.Ingest(context.Background(), BatchWrite{
		ProbeObservations: []ProbeObservationWrite{{
			ProbeItemID:  "pb_001",
			TargetID:     "tg_001",
			ProbeKind:    "http",
			ResultKind:   "failure",
			ErrorSummary: "timeout talking to upstream",
		}},
	})
	if !errors.Is(err, ErrInvalidProbeObservation) {
		t.Fatalf("Ingest() error = %v, want ErrInvalidProbeObservation", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(repo.calls) = %d, want 0", len(repo.calls))
	}
}

func TestIngestRejectsHTTPObservationWithTLSExpiryDays(t *testing.T) {
	t.Parallel()

	tlsExpiryDays := 30
	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_001": {
				TargetID:  "tg_001",
				ProbeKind: "http",
			},
		},
	}
	service := NewService(repo, repo)

	err := service.Ingest(context.Background(), BatchWrite{
		ProbeObservations: []ProbeObservationWrite{{
			ProbeItemID:   "pb_001",
			TargetID:      "tg_001",
			ProbeKind:     "http",
			ResultKind:    "success",
			TLSExpiryDays: &tlsExpiryDays,
		}},
	})
	if !errors.Is(err, ErrInvalidProbeObservation) {
		t.Fatalf("Ingest() error = %v, want ErrInvalidProbeObservation", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(repo.calls) = %d, want 0", len(repo.calls))
	}
}

func TestIngestAcceptsValidSuccessAndFailureProbeObservations(t *testing.T) {
	t.Parallel()

	httpStatus := 200
	repo := &fakeRepo{
		probeMetadataByID: map[string]ProbeMetadata{
			"pb_http": {
				TargetID:  "tg_http",
				ProbeKind: "http",
			},
			"pb_tls": {
				TargetID:  "tg_tls",
				ProbeKind: "tls",
			},
		},
	}
	service := NewService(repo, repo)

	err := service.Ingest(context.Background(), BatchWrite{
		ProbeObservations: []ProbeObservationWrite{
			{
				ProbeItemID: "pb_http",
				TargetID:    "tg_http",
				ProbeKind:   "http",
				ResultKind:  "success",
				HTTPStatus:  &httpStatus,
			},
			{
				ProbeItemID:  "pb_tls",
				TargetID:     "tg_tls",
				ProbeKind:    "tls",
				ResultKind:   "failure",
				ErrorCode:    "tls_handshake",
				ErrorSummary: "certificate handshake failed",
			},
		},
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("len(repo.calls) = %d, want 1", len(repo.calls))
	}
}

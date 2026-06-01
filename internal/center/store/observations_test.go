package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"houfeng/internal/center/observations"
)

func TestPostgresObservationRepositoryImplementsObservationRepository(t *testing.T) {
	t.Parallel()

	var repo observations.Repository = (*PostgresObservationRepository)(nil)
	if repo == nil {
		t.Fatal("repository interface assignment returned nil")
	}
}

func TestPostgresObservationRepositoryRecordBatchReturnsNilForEmptyBatch(t *testing.T) {
	t.Parallel()

	repo := &PostgresObservationRepository{}
	if err := repo.RecordBatch(context.Background(), observations.BatchWrite{MonitoringInstanceID: "mi_001"}); err != nil {
		t.Fatalf("RecordBatch() error = %v", err)
	}
}

func TestRecordBatchUsesPerFactMonitoringInstanceIDsInsteadOfBatchMonitoringInstanceID(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("observations.go")
	if err != nil {
		t.Fatalf("ReadFile(observations.go) error = %v", err)
	}

	text := string(source)
	if strings.Contains(text, "batch.MonitoringInstanceID,") {
		t.Fatalf("RecordBatch() should not write monitoring_instance_id from batch.MonitoringInstanceID:\n%s", text)
	}
	if !strings.Contains(text, "sample.MonitoringInstanceID,") {
		t.Fatal("RecordBatch() should write host_samples.monitoring_instance_id from sample.MonitoringInstanceID")
	}
	if !strings.Contains(text, "observation.MonitoringInstanceID,") {
		t.Fatal("RecordBatch() should write probe_observations.monitoring_instance_id from observation.MonitoringInstanceID")
	}
}

func TestRecordBatchPersistsObservationProvenanceColumns(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("observations.go")
	if err != nil {
		t.Fatalf("ReadFile(observations.go) error = %v", err)
	}

	text := string(source)
	for _, want := range []string{
		"agent_version,",
		"fingerprint,",
		"sample.AgentVersion,",
		"sample.Fingerprint,",
		"observation.AgentVersion,",
		"observation.Fingerprint,",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RecordBatch() source missing %q", want)
		}
	}
}

func TestRecordBatchPersistsHostSampleCapacityColumns(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("observations.go")
	if err != nil {
		t.Fatalf("ReadFile(observations.go) error = %v", err)
	}

	text := string(source)
	for _, want := range []string{
		"mem_total_bytes,",
		"disk_total_bytes,",
		"sample.MemTotalBytes,",
		"sample.DiskTotalBytes,",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RecordBatch() source missing %q", want)
		}
	}
}

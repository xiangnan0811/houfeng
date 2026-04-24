package store

import (
	"testing"

	"houfeng/internal/center/observations"
)

func TestPostgresTargetRepositoryImplementsProbeMetadataRepository(t *testing.T) {
	t.Parallel()

	var repo observations.ProbeMetadataRepository = (*PostgresTargetRepository)(nil)
	if repo == nil {
		t.Fatal("probe metadata repository interface assignment returned nil")
	}
}

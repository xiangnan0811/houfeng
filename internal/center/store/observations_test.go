package store

import (
	"context"
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
	if err := repo.RecordBatch(context.Background(), observations.BatchWrite{NodeID: "nd_001"}); err != nil {
		t.Fatalf("RecordBatch() error = %v", err)
	}
}

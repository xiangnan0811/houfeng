package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
)

type activityTestLeaseKey struct {
	pool       *pgxpool.Pool
	generation uint64
	kind       activity.SourceKind
}

var activityTestLeases = struct {
	sync.Mutex
	leases map[activityTestLeaseKey]activity.SourceLease
}{leases: make(map[activityTestLeaseKey]activity.SourceLease)}

// Concurrent publication tests use the same real committed source claim so the
// head lock, rather than a second claim, is the contention being exercised.
func activityLeaseForTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, generation uint64, kind activity.SourceKind) activity.SourceLease {
	t.Helper()
	key := activityTestLeaseKey{pool: pool, generation: generation, kind: kind}
	activityTestLeases.Lock()
	defer activityTestLeases.Unlock()
	if lease, ok := activityTestLeases.leases[key]; ok {
		return lease
	}
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("activity projection repository: %v", err)
	}
	lease, acquired, err := repository.AcquireSourceLease(ctx, generation, kind, "activity-publish-test", time.Hour)
	if err != nil || !acquired {
		t.Fatalf("acquire activity publication lease: acquired=%t err=%v", acquired, err)
	}
	activityTestLeases.leases[key] = lease
	t.Cleanup(func() {
		activityTestLeases.Lock()
		delete(activityTestLeases.leases, key)
		activityTestLeases.Unlock()
		_ = repository.ReleaseSourceLease(context.Background(), lease)
	})
	return lease
}

func publishActivityTestBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, generation uint64, candidates []activity.CandidateEvent) (ActivityPublishResult, error) {
	t.Helper()
	if len(candidates) == 0 {
		return ActivityPublishResult{}, fmt.Errorf("activity test batch has no source")
	}
	lease := activityLeaseForTest(t, ctx, pool, generation, candidates[0].Source.Kind)
	return PublishActivityBatch(ctx, pool, lease, candidates)
}

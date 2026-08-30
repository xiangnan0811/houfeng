package store

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
)

func TestPostgresIntegrationRecordActivityRuntimeACL(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-activity-runtime-acl", 2)

	assertRecordActivityRuntimeACLContract(t, ctx, fixture)
	if err := EnsureActiveActivityProjectionGeneration(ctx, runtimePool); err != nil {
		failRecordActivityPostgresOperation(t, "ensure active generation", err)
	}

	base := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	firstCandidate := newActivityCandidate(t, "rac_runtime_acl_first", base)
	first, err := PublishActivityBatch(ctx, runtimePool, 1, []activity.CandidateEvent{firstCandidate})
	if err != nil {
		assertRecordActivityRuntimeState(t, ctx, fixture, 0, 0, 0)
		failRecordActivityPostgresOperation(t, "first activity publish", err)
	}
	if first.Inserted != 1 || first.AlreadyPresent != 0 ||
		first.AssignedFrom != 1 || first.AssignedThrough != 1 || first.PublishedThrough != 1 {
		t.Fatalf("first publish result = %+v, want one insert at sequence 1", first)
	}
	assertRecordActivityRuntimeState(t, ctx, fixture, 1, 1, 1)
	assertActivitySequencesAreContiguous(t, ctx, fixture.db, 1, 1)
	assertRecordActivityCanonicalHashes(t, ctx, fixture, firstCandidate)

	exact := requireRecordActivityRuntimePublishSuccess(
		t, ctx, runtimePool, "exact activity retry", []activity.CandidateEvent{firstCandidate},
	)
	if exact.Inserted != 0 || exact.AlreadyPresent != 1 || exact.PublishedThrough != 1 {
		t.Fatalf("exact retry result = %+v, want one already-present fact and watermark 1", exact)
	}
	assertRecordActivityRuntimeState(t, ctx, fixture, 1, 1, 1)

	drifted := firstCandidate
	drifted.Presentation.Title = "canonical hash mismatch"
	drifted.CanonicalHash = drifted.ComputeCanonicalHash()
	mismatchBatch := []activity.CandidateEvent{
		newActivityCandidate(t, "rac_runtime_acl_rolled_back", base.Add(30*time.Second)),
		drifted,
	}
	if _, err := PublishActivityBatch(ctx, runtimePool, 1, mismatchBatch); !errors.Is(err, ErrActivitySourceHashMismatch) {
		if err != nil {
			failRecordActivityPostgresOperation(t, "canonical hash mismatch", err)
		}
		t.Fatal("canonical hash mismatch publish succeeded, want ErrActivitySourceHashMismatch")
	}
	assertRecordActivityRuntimeState(t, ctx, fixture, 1, 1, 1)
	assertActivitySequencesAreContiguous(t, ctx, fixture.db, 1, 1)
	assertRecordActivityCanonicalHashes(t, ctx, fixture, firstCandidate)

	concurrentCandidates := []activity.CandidateEvent{
		newActivityCandidate(t, "rac_runtime_acl_concurrent_a", base.Add(time.Minute)),
		newActivityCandidate(t, "rac_runtime_acl_concurrent_b", base.Add(2*time.Minute)),
	}
	type publishOutcome struct {
		result ActivityPublishResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan publishOutcome, len(concurrentCandidates))
	var workers sync.WaitGroup
	for _, candidate := range concurrentCandidates {
		candidate := candidate
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := PublishActivityBatch(ctx, runtimePool, 1, []activity.CandidateEvent{candidate})
			outcomes <- publishOutcome{result: result, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(outcomes)

	assigned := map[uint64]bool{}
	for outcome := range outcomes {
		if outcome.err != nil {
			failRecordActivityPostgresOperation(t, "concurrent activity publish", outcome.err)
		}
		if outcome.result.Inserted != 1 || outcome.result.AlreadyPresent != 0 ||
			outcome.result.AssignedFrom != outcome.result.AssignedThrough {
			t.Fatalf("concurrent publish result = %+v, want one distinct insertion", outcome.result)
		}
		assigned[outcome.result.AssignedFrom] = true
	}
	if len(assigned) != 2 || !assigned[2] || !assigned[3] {
		t.Fatalf("concurrent assigned sequences = %#v, want 2 and 3", assigned)
	}
	assertRecordActivityRuntimeState(t, ctx, fixture, 3, 3, 3)
	assertActivitySequencesAreContiguous(t, ctx, fixture.db, 1, 3)
	assertRecordActivityCanonicalHashes(t, ctx, fixture, append([]activity.CandidateEvent{firstCandidate}, concurrentCandidates...)...)

	continuation := newActivityCandidate(t, "rac_runtime_acl_continuation", base.Add(3*time.Minute))
	continued := requireRecordActivityRuntimePublishSuccess(
		t, ctx, runtimePool, "continued activity publish", []activity.CandidateEvent{continuation},
	)
	if continued.Inserted != 1 || continued.AssignedFrom != 4 ||
		continued.AssignedThrough != 4 || continued.PublishedThrough != 4 {
		t.Fatalf("continued publish result = %+v, want one insertion at sequence 4", continued)
	}
	assertRecordActivityRuntimeState(t, ctx, fixture, 4, 4, 4)
	assertActivitySequencesAreContiguous(t, ctx, fixture.db, 1, 4)
	assertRecordActivityCanonicalHashes(
		t,
		ctx,
		fixture,
		append(append([]activity.CandidateEvent{firstCandidate}, concurrentCandidates...), continuation)...,
	)
	assertRecordActivityRuntimeACLContract(t, ctx, fixture)
}

func requireRecordActivityRuntimePublishSuccess(
	t *testing.T,
	ctx context.Context,
	runtimePool *pgxpool.Pool,
	phase string,
	candidates []activity.CandidateEvent,
) ActivityPublishResult {
	t.Helper()
	result, err := PublishActivityBatch(ctx, runtimePool, 1, candidates)
	if err != nil {
		failRecordActivityPostgresOperation(t, phase, err)
	}
	return result
}

func assertRecordActivityRuntimeACLContract(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
) {
	t.Helper()
	var (
		serverMajor                        int
		projectionSelect, projectionInsert bool
		projectionUpdate, projectionDelete bool
		headUpdate, revisionIntervalUpdate bool
		projectionColumnACLCount           int
	)
	if err := fixture.db.QueryRow(ctx, `
		select
			pg_catalog.current_setting('server_version_num')::int / 10000,
			pg_catalog.has_table_privilege($1::name, 'public.record_activity_projection', 'SELECT'),
			pg_catalog.has_table_privilege($1::name, 'public.record_activity_projection', 'INSERT'),
			pg_catalog.has_table_privilege($1::name, 'public.record_activity_projection', 'UPDATE'),
			pg_catalog.has_table_privilege($1::name, 'public.record_activity_projection', 'DELETE'),
			pg_catalog.has_table_privilege($1::name, 'public.record_activity_projection_heads', 'UPDATE'),
			pg_catalog.has_table_privilege($1::name, 'public.record_activity_revision_intervals', 'UPDATE'),
			(select count(*)::int
			 from pg_catalog.pg_attribute attribute
			 cross join lateral pg_catalog.aclexplode(attribute.attacl) acl_entry
			 where attribute.attrelid = 'public.record_activity_projection'::pg_catalog.regclass
			   and attribute.attnum > 0
			   and not attribute.attisdropped)`,
		fixture.runtime,
	).Scan(
		&serverMajor,
		&projectionSelect,
		&projectionInsert,
		&projectionUpdate,
		&projectionDelete,
		&headUpdate,
		&revisionIntervalUpdate,
		&projectionColumnACLCount,
	); err != nil {
		t.Fatal("read record activity runtime ACL contract")
	}
	if serverMajor != 16 {
		t.Fatalf("PostgreSQL server major = %d, want 16", serverMajor)
	}
	if !projectionSelect || !projectionInsert || projectionUpdate || !projectionDelete ||
		projectionColumnACLCount != 0 || !headUpdate || !revisionIntervalUpdate {
		t.Fatalf(
			"record activity runtime ACL = projection(S:%t I:%t U:%t D:%t column:%d) heads(U:%t) intervals(U:%t), want projection(true,true,false,true,0) heads(true) intervals(true)",
			projectionSelect,
			projectionInsert,
			projectionUpdate,
			projectionDelete,
			projectionColumnACLCount,
			headUpdate,
			revisionIntervalUpdate,
		)
	}
}

func assertRecordActivityRuntimeState(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	wantProjectionCount int,
	wantSubjectCount int,
	wantWatermark uint64,
) {
	t.Helper()
	var projectionCount, subjectCount int
	var publishedWatermark, allocatedWatermark uint64
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*)::int from public.record_activity_projection),
			(select count(*)::int from public.record_activity_subjects),
			(select published_ingest_sequence
			 from public.record_activity_projection_heads
			 where project_id = 'default' and projection_generation = 1 and head_state = 'active'),
			(select allocated_ingest_sequence
			 from public.record_activity_projection_heads
			 where project_id = 'default' and projection_generation = 1 and head_state = 'active')`,
	).Scan(&projectionCount, &subjectCount, &publishedWatermark, &allocatedWatermark); err != nil {
		t.Fatal("read record activity owner state")
	}
	if projectionCount != wantProjectionCount || subjectCount != wantSubjectCount ||
		publishedWatermark != wantWatermark || allocatedWatermark != wantWatermark {
		t.Fatalf(
			"record activity owner state = projection:%d subjects:%d published:%d allocated:%d, want projection:%d subjects:%d watermarks:%d",
			projectionCount,
			subjectCount,
			publishedWatermark,
			allocatedWatermark,
			wantProjectionCount,
			wantSubjectCount,
			wantWatermark,
		)
	}
}

func assertRecordActivityCanonicalHashes(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	candidates ...activity.CandidateEvent,
) {
	t.Helper()
	for _, candidate := range candidates {
		var stored []byte
		if err := fixture.db.QueryRow(ctx, `
			select canonical_hash
			from public.record_activity_projection
			where activity_id = $1`, candidate.ActivityID).Scan(&stored); err != nil {
			t.Fatal("read record activity canonical hash")
		}
		if !bytes.Equal(stored, candidate.CanonicalHash[:]) {
			t.Fatal("record activity canonical hash mismatch")
		}
	}
}

func failRecordActivityPostgresOperation(t *testing.T, phase string, err error) {
	t.Helper()
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		t.Fatalf("%s PostgreSQL SQLSTATE = %s, want success", phase, postgresError.Code)
	}
	t.Fatalf("%s failed without PostgreSQL typed cause", phase)
}

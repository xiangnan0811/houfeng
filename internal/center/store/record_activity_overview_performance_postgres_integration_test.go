package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	centerrecords "houfeng/internal/center/records"
	overviewperftest "houfeng/internal/center/testsupport/vpsoverviewperf"
	"houfeng/internal/center/vpsoverview"
)

const overviewPerfBudget = 750 * time.Millisecond

// TestPostgresIntegrationVPSOverviewPerformance times the overview aggregator
// against a large subject activity projection (same seed as the timeline test).
func TestPostgresIntegrationVPSOverviewPerformance(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	count := activityPerfRowCount(t)

	t.Logf("seeding %d activity rows for overview performance", count)
	if err := seedActivityPerformanceProjection(ctx, pool, count); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := overviewperftest.SeedAuthority(ctx, pool, activityPerfSubjectID); err != nil {
		t.Fatalf("seed overview authority: %v", err)
	}
	if err := overviewperftest.PrepareMeasurement(ctx, pool); err != nil {
		t.Fatalf("prepare measurement: %v", err)
	}

	trace := &overviewperftest.QueryTrace{}
	runtimePool := overviewperftest.OpenTracedPool(t, ctx, pool, trace)
	vpsRepository := NewPostgresVPSAssetRepository(runtimePool)
	sources, err := NewVPSOverviewRepository(
		vpsRepository,
		NewPostgresVPSMonitoringInstanceLinkRepository(runtimePool),
		NewPostgresIPQualityRepository(runtimePool),
		NewPostgresSettingsRepository(runtimePool),
		NewPostgresSubscriptionRepository(runtimePool),
		NewPostgresAssetServiceRepository(runtimePool),
		NewPostgresAssetDomainRepository(runtimePool),
	)
	if err != nil {
		t.Fatalf("overview sources: %v", err)
	}
	subjects, err := centerrecords.NewSubjectAdapterRegistry([]centerrecords.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(vpsRepository),
		NewMonitoringInstanceRecordSubjectAdapter(NewPostgresMonitoringInstanceRepository(runtimePool)),
		NewTargetRecordSubjectAdapter(NewPostgresTargetRepository(runtimePool)),
	})
	if err != nil {
		t.Fatalf("subject registry: %v", err)
	}
	activityRepository, err := NewActivityProjectionRepository(runtimePool)
	if err != nil {
		t.Fatalf("repository: %v", err)
	}
	codec, err := activity.NewCursorCodec([]byte("houfeng-overview-perf-hmac-key-32b!!"))
	if err != nil {
		t.Fatalf("cursor codec: %v", err)
	}
	activityService, err := activity.NewService(
		activityRepository, activityRepository, NewActivityLiveSubjectResolver(subjects), codec,
	)
	if err != nil {
		t.Fatalf("activity service: %v", err)
	}
	overviewService, err := vpsoverview.NewService(sources, activityService)
	if err != nil {
		t.Fatalf("overview service: %v", err)
	}

	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("actor: %v", err)
	}
	request := vpsoverview.Request{Actor: actor, VPSID: activityPerfSubjectID}

	warmup, err := overviewService.Get(ctx, request)
	if err != nil {
		t.Fatalf("warmup overview: %v", err)
	}
	if err := overviewperftest.ValidateHealthyOverview(warmup, activityPerfSubjectID); err != nil {
		t.Fatalf("warmup unhealthy overview: %v", err)
	}
	if err := trace.Snapshot().Validate(); err != nil {
		t.Fatalf("warmup query contract: %v", err)
	}
	trace.Reset()

	durations := make([]time.Duration, 0, activityPerfRuns)
	for run := 1; run <= activityPerfRuns; run++ {
		trace.Reset()
		started := time.Now()
		overview, err := overviewService.Get(ctx, request)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("run %d overview: %v", run, err)
		}
		if err := overviewperftest.ValidateHealthyOverview(overview, activityPerfSubjectID); err != nil {
			t.Fatalf("run %d unhealthy overview: %v", run, err)
		}
		queryStats := trace.Snapshot()
		if err := queryStats.Validate(); err != nil {
			t.Fatalf("run %d query contract: %v", run, err)
		}
		durations = append(durations, elapsed)
		t.Logf("run %d overview Get = %s (recent=%d queries=%d query_errors=%d query_error_rate=%.2f%%)",
			run, elapsed.Round(time.Millisecond), len(overview.RecentActivity.Items),
			queryStats.Count, queryStats.Errors, queryStats.ErrorRatePercent())
	}

	p50, p95, p99 := durationPercentiles(durations)
	t.Logf("overview timings n=%d p50=%s p95=%s p99=%s budget=%s",
		len(durations), p50.Round(time.Millisecond), p95.Round(time.Millisecond),
		p99.Round(time.Millisecond), overviewPerfBudget)
	if p95 > overviewPerfBudget {
		t.Fatalf("overview p95 = %s, want ≤ %s", p95, overviewPerfBudget)
	}
}

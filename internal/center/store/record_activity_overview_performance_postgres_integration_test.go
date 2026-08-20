package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/vpsoverview"
)

const overviewPerfBudget = 750 * time.Millisecond

type overviewPerfSources struct {
	bundle vpsoverview.SourceBundle
}

func (sources overviewPerfSources) LoadSources(context.Context, string) (vpsoverview.SourceBundle, error) {
	return sources.bundle, nil
}

type overviewPerfLiveSubjects struct{}

func (overviewPerfLiveSubjects) ResolveLive(
	_ context.Context,
	_ recordauth.ActorScope,
	ref activity.SubjectRef,
) (activity.SubjectHeader, error) {
	return activity.SubjectHeader{
		Kind:      ref.Kind,
		SourceID:  ref.SourceID,
		Identity:  map[string]string{"display_name": "perf-vps"},
		LiveRoute: "/vps/" + ref.SourceID,
		Status:    activity.SubjectStatusLive,
	}, nil
}

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
	if _, err := pool.Exec(ctx, `analyze public.record_activity_subjects; analyze public.record_activity_projection`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("repository: %v", err)
	}
	codec, err := activity.NewCursorCodec([]byte("houfeng-overview-perf-hmac-key-32b!!"))
	if err != nil {
		t.Fatalf("cursor codec: %v", err)
	}
	activityService, err := activity.NewService(
		repository, repository, overviewPerfLiveSubjects{}, codec,
	)
	if err != nil {
		t.Fatalf("activity service: %v", err)
	}
	overviewService, err := vpsoverview.NewServiceWithClock(overviewPerfSources{
		bundle: vpsoverview.SourceBundle{
			Identity: vpsoverview.Identity{
				VPSID:           activityPerfSubjectID,
				DisplayName:     "perf-vps",
				LifecycleStatus: "active",
				UsageStatus:     "in_use",
				Labels:          []string{},
			},
			MonitoringSection: vpsoverview.SectionState{State: vpsoverview.SectionReady},
			IPSection:         vpsoverview.SectionState{State: vpsoverview.SectionReady},
			RenewalSection:    vpsoverview.SectionState{State: vpsoverview.SectionReady},
			Facts:             []vpsoverview.Fact{},
			Relations:         []vpsoverview.RelationSummary{},
		},
	}, activityService, func() time.Time { return time.Now().UTC() }, 5*time.Second)
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

	if _, err := overviewService.Get(ctx, request); err != nil {
		t.Fatalf("warmup overview: %v", err)
	}

	durations := make([]time.Duration, 0, activityPerfRuns)
	for run := 1; run <= activityPerfRuns; run++ {
		started := time.Now()
		overview, err := overviewService.Get(ctx, request)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("run %d overview: %v", run, err)
		}
		if overview.Identity.VPSID != activityPerfSubjectID {
			t.Fatalf("run %d identity = %q", run, overview.Identity.VPSID)
		}
		if len(overview.RecentActivity.Items) == 0 {
			t.Fatalf("run %d: expected recent activity items", run)
		}
		durations = append(durations, elapsed)
		t.Logf("run %d overview Get = %s (recent=%d)", run, elapsed.Round(time.Millisecond), len(overview.RecentActivity.Items))
	}

	p50, p95, p99 := durationPercentiles(durations)
	t.Logf("overview timings n=%d p50=%s p95=%s p99=%s budget=%s",
		len(durations), p50.Round(time.Millisecond), p95.Round(time.Millisecond),
		p99.Round(time.Millisecond), overviewPerfBudget)
	if p95 > overviewPerfBudget {
		t.Fatalf("overview p95 = %s, want ≤ %s", p95, overviewPerfBudget)
	}
}

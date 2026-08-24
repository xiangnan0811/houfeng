package vpsoverview

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type fakeSources struct {
	bundle SourceBundle
	err    error
	delay  time.Duration
}

func (fake *fakeSources) wait(ctx context.Context) error {
	if fake.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(fake.delay):
		}
	}
	return nil
}

func (fake *fakeSources) LoadIdentity(ctx context.Context, _ string) (IdentitySource, error) {
	if err := fake.wait(ctx); err != nil {
		return IdentitySource{}, err
	}
	if fake.err != nil {
		return IdentitySource{}, fake.err
	}
	return IdentitySource{Identity: fake.bundle.Identity, Facts: fake.bundle.Facts}, nil
}

func (fake *fakeSources) LoadMonitoring(context.Context, string) (MonitoringSource, error) {
	return MonitoringSource{
		Section: fake.bundle.MonitoringSection, Health: fake.bundle.MonitoringHealth,
		Status: fake.bundle.MonitoringStatus, Detail: fake.bundle.MonitoringDetail,
		ActiveIncidents: fake.bundle.ActiveIncidents,
		Count:           relationCount(fake.bundle.Relations, "monitoring_instances"),
	}, nil
}

func (fake *fakeSources) LoadIPQuality(context.Context, string) (IPQualitySource, error) {
	return IPQualitySource{
		Section: fake.bundle.IPSection, Status: fake.bundle.IPStatus,
		RiskLevel: fake.bundle.IPRiskLevel, Stale: fake.bundle.IPStale,
	}, nil
}

func (fake *fakeSources) LoadRenewal(context.Context, string, string) (RenewalSource, error) {
	return RenewalSource{
		Section: fake.bundle.RenewalSection, ActiveSubscriptions: fake.bundle.ActiveSubscriptions,
		NextRenewAt: fake.bundle.NextRenewAt, Status: fake.bundle.RenewalStatus,
	}, nil
}

func (fake *fakeSources) LoadServiceRelation(context.Context, string) (RelationSource, error) {
	return RelationSource{
		Count:   relationCount(fake.bundle.Relations, "services"),
		Section: relationSection(fake.bundle.Relations, "services"),
	}, nil
}

func (fake *fakeSources) LoadDomainRelation(context.Context, string) (RelationSource, error) {
	return RelationSource{
		Count:   relationCount(fake.bundle.Relations, "domains"),
		Section: relationSection(fake.bundle.Relations, "domains"),
	}, nil
}

func relationCount(relations []RelationSummary, kind string) int {
	for _, relation := range relations {
		if relation.Kind == kind {
			return relation.Count
		}
	}
	return 0
}

func relationSection(relations []RelationSummary, kind string) SectionState {
	for _, relation := range relations {
		if relation.Kind == kind {
			return relation.Section
		}
	}
	return SectionState{State: SectionReady}
}

type fakeActivity struct {
	result activity.ListResult
	err    error
}

func (fake *fakeActivity) List(context.Context, activity.ListRequest) (activity.ListResult, error) {
	if fake.err != nil {
		return activity.ListResult{}, fake.err
	}
	return fake.result, nil
}

func testOverviewActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("actor: %v", err)
	}
	return actor
}

func TestServiceGetHealthyOverview(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	sources := &fakeSources{bundle: SourceBundle{
		Identity: Identity{
			VPSID: "vps_7c2a4e18b09d5f31", DisplayName: "Alpha",
			LifecycleStatus: "active", RenewalDecision: "keep", Labels: []string{},
			UpdatedAt: now,
		},
		MonitoringSection:   SectionState{State: SectionReady},
		MonitoringHealth:    "正常",
		MonitoringStatus:    "启用",
		IPSection:           SectionState{State: SectionReady},
		IPStatus:            "success",
		IPRiskLevel:         "low",
		RenewalSection:      SectionState{State: SectionReady},
		ActiveSubscriptions: 1,
		Facts:               []Fact{{Key: "os_name", Label: "系统", Value: "Debian"}},
		Relations:           []RelationSummary{{Kind: "subscriptions", Count: 1, Route: "/vps/vps_7c2a4e18b09d5f31"}},
	}}
	activityLister := &fakeActivity{result: activity.ListResult{
		Items:     []activity.Event{},
		Freshness: activity.Freshness{State: "ready"},
	}}
	service, err := NewServiceWithClock(sources, activityLister, func() time.Time { return now }, time.Second)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	overview, err := service.Get(context.Background(), Request{
		Actor: testOverviewActor(t), VPSID: "vps_7c2a4e18b09d5f31",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !overview.GeneratedAt.Equal(now) {
		t.Fatalf("generated_at = %s", overview.GeneratedAt)
	}
	if len(overview.Anomalies) != 0 || overview.Anomalies == nil {
		t.Fatalf("anomalies = %#v", overview.Anomalies)
	}
	if overview.Summary.Overall.Status != "healthy" {
		t.Fatalf("overall = %s", overview.Summary.Overall.Status)
	}
	if overview.RecentActivity.Items == nil || overview.Facts == nil || overview.Relations == nil {
		t.Fatal("collections must be non-nil")
	}
	payload, err := json.Marshal(overview)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"projection_generation", "as_of_ingest_sequence", "published_ingest"} {
		if contains(body, forbidden) {
			t.Fatalf("leaked %s in %s", forbidden, body)
		}
	}
}

func TestServiceGetRelationDestinations(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	const vpsID = "vps id+东京"
	sources := &fakeSources{bundle: SourceBundle{
		Identity: Identity{
			VPSID: vpsID, DisplayName: "Alpha", LifecycleStatus: "active",
			RenewalDecision: "keep", Labels: []string{}, UpdatedAt: now,
		},
		MonitoringSection:   SectionState{State: SectionReady},
		MonitoringHealth:    "正常",
		MonitoringStatus:    "启用",
		IPSection:           SectionState{State: SectionReady},
		IPStatus:            "success",
		IPRiskLevel:         "low",
		RenewalSection:      SectionState{State: SectionReady},
		ActiveSubscriptions: 2,
		Facts:               []Fact{},
		Relations: []RelationSummary{
			{Kind: "monitoring_instances", Count: 1, Section: SectionState{State: SectionReady}},
			{Kind: "services", Count: 3, Section: SectionState{State: SectionReady}},
			{Kind: "domains", Count: 4, Section: SectionState{State: SectionReady}},
		},
	}}
	service, err := NewServiceWithClock(
		sources,
		&fakeActivity{result: activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: "ready"}}},
		func() time.Time { return now },
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewServiceWithClock: %v", err)
	}
	overview, err := service.Get(context.Background(), Request{Actor: testOverviewActor(t), VPSID: vpsID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	tests := []struct {
		kind  string
		label string
		count int
		route string
	}{
		{kind: "monitoring_instances", label: "监控实例", count: 1},
		{kind: "subscriptions", label: "订阅", count: 2, route: "/subscriptions?vps_id=" + url.QueryEscape(vpsID)},
		{kind: "services", label: "服务", count: 3},
		{kind: "domains", label: "域名", count: 4},
	}
	if len(overview.Relations) != len(tests) {
		t.Fatalf("relations = %#v, want %d ordered rows", overview.Relations, len(tests))
	}
	for index, tt := range tests {
		got := overview.Relations[index]
		if got.Kind != tt.kind || got.Label != tt.label || got.Count != tt.count || got.Route != tt.route {
			t.Errorf("relation[%d] = %#v, want kind=%q label=%q count=%d route=%q", index, got, tt.kind, tt.label, tt.count, tt.route)
		}
		if got.Section.State != SectionReady {
			t.Errorf("relation[%d].section = %#v, want required ready section", index, got.Section)
		}
	}
}

func TestServiceGetNotFoundIsFatal(t *testing.T) {
	service, err := NewService(&fakeSources{err: ErrVPSNotFound}, &fakeActivity{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.Get(context.Background(), Request{Actor: testOverviewActor(t), VPSID: "vps_missing"})
	if !errors.Is(err, ErrVPSNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestServiceGetDegradesActivityTimeout(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	sources := &fakeSources{bundle: SourceBundle{
		Identity:          Identity{VPSID: "vps_7c2a4e18b09d5f31", Labels: []string{}, UpdatedAt: now, LifecycleStatus: "active"},
		MonitoringSection: SectionState{State: SectionReady}, MonitoringHealth: "正常",
		IPSection: SectionState{State: SectionReady}, RenewalSection: SectionState{State: SectionReady},
		ActiveSubscriptions: 1, Facts: []Fact{}, Relations: []RelationSummary{},
	}}
	activityLister := &fakeActivity{err: context.DeadlineExceeded}
	service, err := NewServiceWithClock(sources, activityLister, func() time.Time { return now }, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	overview, err := service.Get(context.Background(), Request{Actor: testOverviewActor(t), VPSID: "vps_7c2a4e18b09d5f31"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if overview.RecentActivity.Section.State != SectionUnavailable {
		t.Fatalf("activity section = %#v", overview.RecentActivity.Section)
	}
	if overview.RecentActivity.Items == nil {
		t.Fatal("items nil")
	}
}

func TestServiceGetUsesActivitySubjectVPS(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	var got activity.ListRequest
	activityLister := activityCapture{onList: func(request activity.ListRequest) (activity.ListResult, error) {
		got = request
		return activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: "ready"}}, nil
	}}
	sources := &fakeSources{bundle: SourceBundle{
		Identity:          Identity{VPSID: "vps_7c2a4e18b09d5f31", Labels: []string{}, UpdatedAt: now},
		MonitoringSection: SectionState{State: SectionReady},
		IPSection:         SectionState{State: SectionReady},
		RenewalSection:    SectionState{State: SectionReady},
		Facts:             []Fact{}, Relations: []RelationSummary{},
	}}
	service, err := NewServiceWithClock(sources, activityLister, func() time.Time { return now }, time.Second)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := service.Get(context.Background(), Request{Actor: testOverviewActor(t), VPSID: "vps_7c2a4e18b09d5f31"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Query.Subject.Kind != records.SubjectKindVPS || got.Query.Subject.SourceID != "vps_7c2a4e18b09d5f31" {
		t.Fatalf("subject = %#v", got.Query.Subject)
	}
	if got.Query.Limit != RecentActivityLimit {
		t.Fatalf("limit = %d", got.Query.Limit)
	}
}

type activityCapture struct {
	onList func(activity.ListRequest) (activity.ListResult, error)
}

func (capture activityCapture) List(_ context.Context, request activity.ListRequest) (activity.ListResult, error) {
	return capture.onList(request)
}

func contains(body, needle string) bool {
	return strings.Contains(body, needle)
}

package vpsoverview

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"houfeng/internal/center/activity"
)

type isolationSourceReader struct {
	identityFinished chan struct{}
	started          chan string
	once             sync.Once
}

func newIsolationSourceReader() *isolationSourceReader {
	return &isolationSourceReader{
		identityFinished: make(chan struct{}),
		started:          make(chan string, 6),
	}
}

func (reader *isolationSourceReader) LoadIdentity(context.Context, string) (IdentitySource, error) {
	reader.once.Do(func() { close(reader.identityFinished) })
	return IdentitySource{
		Identity: Identity{
			VPSID:           "vps_7c2a4e18b09d5f31",
			LifecycleStatus: "active",
			RenewalDecision: "keep",
			Labels:          []string{},
		},
		Facts: []Fact{},
	}, nil
}

func (reader *isolationSourceReader) assertIdentityFirst(t *testing.T, source string) {
	t.Helper()
	select {
	case <-reader.identityFinished:
	default:
		t.Errorf("%s started before identity finished", source)
	}
}

func (reader *isolationSourceReader) LoadMonitoring(ctx context.Context, _ string) (MonitoringSource, error) {
	reader.assertIdentityFirst(testingContext(ctx), "monitoring")
	reader.started <- "monitoring"
	<-ctx.Done()
	return MonitoringSource{}, ctx.Err()
}

func (reader *isolationSourceReader) LoadIPQuality(ctx context.Context, _ string) (IPQualitySource, error) {
	reader.assertIdentityFirst(testingContext(ctx), "ip_quality")
	reader.started <- "ip_quality"
	return IPQualitySource{Section: SectionState{State: SectionReady}, Status: "success", RiskLevel: "low"}, nil
}

func (reader *isolationSourceReader) LoadRenewal(ctx context.Context, _ string, _ string) (RenewalSource, error) {
	reader.assertIdentityFirst(testingContext(ctx), "renewal")
	reader.started <- "renewal"
	return RenewalSource{Section: SectionState{State: SectionReady}, ActiveSubscriptions: 1, Status: "keep"}, nil
}

func (reader *isolationSourceReader) LoadServiceRelation(ctx context.Context, _ string) (RelationSource, error) {
	reader.assertIdentityFirst(testingContext(ctx), "services")
	reader.started <- "services"
	return RelationSource{Count: 2, Section: SectionState{State: SectionReady}}, nil
}

func (reader *isolationSourceReader) LoadDomainRelation(ctx context.Context, _ string) (RelationSource, error) {
	reader.assertIdentityFirst(testingContext(ctx), "domains")
	reader.started <- "domains"
	return RelationSource{Count: 3, Section: SectionState{State: SectionReady}}, nil
}

type isolationActivityLister struct {
	identityFinished <-chan struct{}
	started          chan<- string
}

func (lister isolationActivityLister) List(ctx context.Context, _ activity.ListRequest) (activity.ListResult, error) {
	select {
	case <-lister.identityFinished:
	default:
		testingContext(ctx).Error("activity started before identity finished")
	}
	lister.started <- "activity"
	return activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: SectionReady}}, nil
}

type testingContextKey struct{}

func withTestingContext(ctx context.Context, t *testing.T) context.Context {
	return context.WithValue(ctx, testingContextKey{}, t)
}

func testingContext(ctx context.Context) *testing.T {
	t, _ := ctx.Value(testingContextKey{}).(*testing.T)
	return t
}

func TestServiceGetIsolatesSlowMonitoringFromSiblingSources(t *testing.T) {
	reader := newIsolationSourceReader()
	service, err := NewServiceWithBudgets(
		reader,
		isolationActivityLister{identityFinished: reader.identityFinished, started: reader.started},
		func() time.Time { return time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC) },
		SourceBudgets{
			Total:      time.Second,
			Identity:   time.Second,
			Monitoring: 20 * time.Millisecond,
			IPQuality:  time.Second,
			Renewal:    time.Second,
			Services:   time.Second,
			Domains:    time.Second,
			Activity:   time.Second,
		},
	)
	if err != nil {
		t.Fatalf("NewServiceWithBudgets: %v", err)
	}

	overview, err := service.Get(withTestingContext(context.Background(), t), Request{
		Actor: testOverviewActor(t),
		VPSID: "vps_7c2a4e18b09d5f31",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	started := map[string]bool{}
	for len(reader.started) > 0 {
		started[<-reader.started] = true
	}
	for _, source := range []string{"monitoring", "ip_quality", "renewal", "services", "domains", "activity"} {
		if !started[source] {
			t.Errorf("%s did not start", source)
		}
	}
	if got := overview.Summary.Monitoring.Section; got.State != SectionUnavailable || got.ReasonCode != "monitoring_timeout" {
		t.Fatalf("monitoring section = %#v", got)
	}
	if overview.Summary.IPQuality.Section.State != SectionReady || overview.Summary.Renewal.Section.State != SectionReady {
		t.Fatalf("ready summary siblings were degraded: %#v", overview.Summary)
	}
	if overview.RecentActivity.Section.State != SectionReady {
		t.Fatalf("activity section = %#v", overview.RecentActivity.Section)
	}
	wantKinds := []string{"monitoring_instances", "subscriptions", "services", "domains"}
	if len(overview.Relations) != len(wantKinds) {
		t.Fatalf("relations = %#v", overview.Relations)
	}
	for index, want := range wantKinds {
		if overview.Relations[index].Kind != want {
			t.Fatalf("relation[%d].kind = %q, want %q", index, overview.Relations[index].Kind, want)
		}
	}
	if overview.Relations[0].Section.State != SectionUnavailable {
		t.Fatalf("monitoring relation = %#v", overview.Relations[0])
	}
	for _, relation := range overview.Relations[1:] {
		if relation.Section.State != SectionReady {
			t.Fatalf("ready relation degraded: %#v", relation)
		}
	}
}

type configuredSourceReader struct {
	identity      IdentitySource
	identityErr   error
	monitoring    MonitoringSource
	monitoringErr error
	ipQuality     IPQualitySource
	ipQualityErr  error
	renewal       RenewalSource
	renewalErr    error
	services      RelationSource
	servicesErr   error
	domains       RelationSource
	domainsErr    error

	identityCalls   atomic.Int32
	monitoringCalls atomic.Int32
	ipQualityCalls  atomic.Int32
	renewalCalls    atomic.Int32
	servicesCalls   atomic.Int32
	domainsCalls    atomic.Int32
}

func healthyConfiguredSourceReader() *configuredSourceReader {
	return &configuredSourceReader{
		identity: IdentitySource{
			Identity: Identity{
				VPSID: "vps_7c2a4e18b09d5f31", LifecycleStatus: "active",
				RenewalDecision: "keep", Labels: []string{},
			},
			Facts: []Fact{},
		},
		monitoring: MonitoringSource{
			Section: SectionState{State: SectionReady}, Health: "healthy", Count: 1,
		},
		ipQuality: IPQualitySource{
			Section: SectionState{State: SectionReady}, Status: "success", RiskLevel: "low",
		},
		renewal: RenewalSource{
			Section: SectionState{State: SectionReady}, ActiveSubscriptions: 1, Status: "keep",
		},
		services: RelationSource{Count: 2, Section: SectionState{State: SectionReady}},
		domains:  RelationSource{Count: 3, Section: SectionState{State: SectionReady}},
	}
}

func (reader *configuredSourceReader) LoadIdentity(context.Context, string) (IdentitySource, error) {
	reader.identityCalls.Add(1)
	return reader.identity, reader.identityErr
}

func (reader *configuredSourceReader) LoadMonitoring(context.Context, string) (MonitoringSource, error) {
	reader.monitoringCalls.Add(1)
	return reader.monitoring, reader.monitoringErr
}

func (reader *configuredSourceReader) LoadIPQuality(context.Context, string) (IPQualitySource, error) {
	reader.ipQualityCalls.Add(1)
	return reader.ipQuality, reader.ipQualityErr
}

func (reader *configuredSourceReader) LoadRenewal(context.Context, string, string) (RenewalSource, error) {
	reader.renewalCalls.Add(1)
	return reader.renewal, reader.renewalErr
}

func (reader *configuredSourceReader) LoadServiceRelation(context.Context, string) (RelationSource, error) {
	reader.servicesCalls.Add(1)
	return reader.services, reader.servicesErr
}

func (reader *configuredSourceReader) LoadDomainRelation(context.Context, string) (RelationSource, error) {
	reader.domainsCalls.Add(1)
	return reader.domains, reader.domainsErr
}

type reverseCompletionSourceReader struct {
	*configuredSourceReader
	started   chan<- string
	completed chan<- string
	release   map[string]<-chan struct{}
}

func (reader *reverseCompletionSourceReader) wait(source string) {
	reader.started <- source
	<-reader.release[source]
	reader.completed <- source
}

func (reader *reverseCompletionSourceReader) LoadMonitoring(context.Context, string) (MonitoringSource, error) {
	reader.monitoringCalls.Add(1)
	reader.wait("monitoring")
	return reader.monitoring, reader.monitoringErr
}

func (reader *reverseCompletionSourceReader) LoadIPQuality(context.Context, string) (IPQualitySource, error) {
	reader.ipQualityCalls.Add(1)
	reader.wait("ip_quality")
	return reader.ipQuality, reader.ipQualityErr
}

func (reader *reverseCompletionSourceReader) LoadRenewal(context.Context, string, string) (RenewalSource, error) {
	reader.renewalCalls.Add(1)
	reader.wait("renewal")
	return reader.renewal, reader.renewalErr
}

func (reader *reverseCompletionSourceReader) LoadServiceRelation(context.Context, string) (RelationSource, error) {
	reader.servicesCalls.Add(1)
	reader.wait("services")
	return reader.services, reader.servicesErr
}

func (reader *reverseCompletionSourceReader) LoadDomainRelation(context.Context, string) (RelationSource, error) {
	reader.domainsCalls.Add(1)
	reader.wait("domains")
	return reader.domains, reader.domainsErr
}

type reverseCompletionActivity struct {
	started   chan<- string
	completed chan<- string
	release   <-chan struct{}
}

func (lister reverseCompletionActivity) List(context.Context, activity.ListRequest) (activity.ListResult, error) {
	lister.started <- "activity"
	<-lister.release
	lister.completed <- "activity"
	return activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: SectionReady}}, nil
}

func TestServiceGetBuildsRelationsInStableOrderAfterReverseCompletionAndReusesReads(t *testing.T) {
	now := time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC)
	configured := healthyConfiguredSourceReader()
	configured.monitoring.Section = SectionState{State: SectionReady, ObservedAt: &now, LastSuccessAt: &now}
	configured.renewal.Section = SectionState{State: SectionReady, ObservedAt: &now, LastSuccessAt: &now}
	started := make(chan string, 6)
	completed := make(chan string, 6)
	releases := map[string]chan struct{}{}
	readOnlyReleases := map[string]<-chan struct{}{}
	for _, source := range []string{"monitoring", "ip_quality", "renewal", "services", "domains", "activity"} {
		releases[source] = make(chan struct{})
		readOnlyReleases[source] = releases[source]
	}
	reader := &reverseCompletionSourceReader{
		configuredSourceReader: configured,
		started:                started,
		completed:              completed,
		release:                readOnlyReleases,
	}
	service, err := NewServiceWithBudgets(reader, reverseCompletionActivity{
		started: started, completed: completed, release: releases["activity"],
	}, func() time.Time { return now.Add(time.Minute) }, uniformSourceBudgets(time.Second))
	if err != nil {
		t.Fatalf("NewServiceWithBudgets: %v", err)
	}
	actor := testOverviewActor(t)
	type result struct {
		overview Overview
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		overview, getErr := service.Get(context.Background(), Request{
			Actor: actor, VPSID: configured.identity.Identity.VPSID,
		})
		resultCh <- result{overview: overview, err: getErr}
	}()

	for range 6 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("not all degradable sources started")
		}
	}
	completionOrder := []string{"domains", "services", "renewal", "ip_quality", "monitoring", "activity"}
	for _, source := range completionOrder {
		close(releases[source])
		select {
		case got := <-completed:
			if got != source {
				t.Fatalf("completion = %q, want %q", got, source)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not complete", source)
		}
	}

	var got result
	select {
	case got = <-resultCh:
	case <-time.After(time.Second):
		t.Fatal("Get did not return after all sources completed")
	}
	if got.err != nil {
		t.Fatalf("Get: %v", got.err)
	}
	wantKinds := []string{"monitoring_instances", "subscriptions", "services", "domains"}
	for index, want := range wantKinds {
		if got.overview.Relations[index].Kind != want {
			t.Fatalf("relation[%d].kind = %q, want %q", index, got.overview.Relations[index].Kind, want)
		}
	}
	if !sameSection(got.overview.Relations[0].Section, got.overview.Summary.Monitoring.Section) ||
		!sameSection(got.overview.Relations[1].Section, got.overview.Summary.Renewal.Section) {
		t.Fatalf("relation freshness did not reuse summary source: %#v", got.overview.Relations)
	}
	if reader.monitoringCalls.Load() != 1 || reader.renewalCalls.Load() != 1 {
		t.Fatalf("source reuse calls monitoring=%d renewal=%d", reader.monitoringCalls.Load(), reader.renewalCalls.Load())
	}
}

func sameSection(left, right SectionState) bool {
	if left.State != right.State || left.ReasonCode != right.ReasonCode {
		return false
	}
	return sameOptionalTime(left.ObservedAt, right.ObservedAt) && sameOptionalTime(left.LastSuccessAt, right.LastSuccessAt)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func TestServiceGetDegradesOnlyFailedSource(t *testing.T) {
	tests := []struct {
		name        string
		fail        func(*configuredSourceReader)
		activityErr error
		want        map[string]string
	}{
		{
			name: "monitoring", fail: func(reader *configuredSourceReader) {
				reader.monitoringErr = errors.New("raw monitoring endpoint secret")
			},
			want: map[string]string{"monitoring": "monitoring_unavailable", "monitoring_relation": "monitoring_unavailable"},
		},
		{
			name: "ip_quality", fail: func(reader *configuredSourceReader) { reader.ipQualityErr = errors.New("raw ip sql secret") },
			want: map[string]string{"ip_quality": "ip_quality_unavailable"},
		},
		{
			name: "subscription", fail: func(reader *configuredSourceReader) { reader.renewalErr = errors.New("raw subscription sql secret") },
			want: map[string]string{"renewal": "subscription_unavailable", "subscription_relation": "subscription_unavailable"},
		},
		{
			name: "service", fail: func(reader *configuredSourceReader) { reader.servicesErr = errors.New("raw service sql secret") },
			want: map[string]string{"service_relation": "relation_unavailable"},
		},
		{
			name: "domain", fail: func(reader *configuredSourceReader) { reader.domainsErr = errors.New("raw domain sql secret") },
			want: map[string]string{"domain_relation": "relation_unavailable"},
		},
		{
			name: "activity", fail: func(*configuredSourceReader) {}, activityErr: errors.New("raw activity checkpoint secret"),
			want: map[string]string{"activity": "activity_unavailable"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := healthyConfiguredSourceReader()
			test.fail(reader)
			service, err := NewService(reader, &fakeActivity{
				result: activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: SectionReady}},
				err:    test.activityErr,
			})
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			overview, err := service.Get(context.Background(), Request{
				Actor: testOverviewActor(t), VPSID: reader.identity.Identity.VPSID,
			})
			if err != nil {
				t.Fatalf("Get: %v", err)
			}

			sections := map[string]SectionState{
				"monitoring":            overview.Summary.Monitoring.Section,
				"ip_quality":            overview.Summary.IPQuality.Section,
				"renewal":               overview.Summary.Renewal.Section,
				"activity":              overview.RecentActivity.Section,
				"monitoring_relation":   overview.Relations[0].Section,
				"subscription_relation": overview.Relations[1].Section,
				"service_relation":      overview.Relations[2].Section,
				"domain_relation":       overview.Relations[3].Section,
			}
			for surface, section := range sections {
				wantReason, degraded := test.want[surface]
				if degraded {
					if section.State != SectionUnavailable || section.ReasonCode != wantReason {
						t.Errorf("%s = %#v, want unavailable/%s", surface, section, wantReason)
					}
					continue
				}
				if section.State != SectionReady {
					t.Errorf("%s = %#v, want ready", surface, section)
				}
			}
			payload, marshalErr := json.Marshal(overview)
			if marshalErr != nil {
				t.Fatalf("marshal: %v", marshalErr)
			}
			if strings.Contains(string(payload), "raw ") || strings.Contains(string(payload), "secret") {
				t.Fatalf("raw source error leaked: %s", payload)
			}
			if reader.monitoringCalls.Load() != 1 || reader.renewalCalls.Load() != 1 {
				t.Fatalf("relation reuse failed: monitoring=%d renewal=%d", reader.monitoringCalls.Load(), reader.renewalCalls.Load())
			}
		})
	}
}

type totalDeadlineSourceReader struct {
	*configuredSourceReader
	monitoringStarted chan struct{}
	releaseMonitoring chan struct{}
	monitoringDone    chan struct{}
}

func (reader *totalDeadlineSourceReader) LoadMonitoring(context.Context, string) (MonitoringSource, error) {
	reader.monitoringCalls.Add(1)
	close(reader.monitoringStarted)
	<-reader.releaseMonitoring
	close(reader.monitoringDone)
	return reader.monitoring, nil
}

func TestServiceGetTotalDeadlinePreservesCompletedSources(t *testing.T) {
	reader := &totalDeadlineSourceReader{
		configuredSourceReader: healthyConfiguredSourceReader(),
		monitoringStarted:      make(chan struct{}),
		releaseMonitoring:      make(chan struct{}),
		monitoringDone:         make(chan struct{}),
	}
	budgets := uniformSourceBudgets(time.Second)
	budgets.Total = 20 * time.Millisecond
	service, err := NewServiceWithBudgets(reader, &fakeActivity{
		result: activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: SectionReady}},
	}, time.Now, budgets)
	if err != nil {
		t.Fatalf("NewServiceWithBudgets: %v", err)
	}

	overview, err := service.Get(context.Background(), Request{
		Actor: testOverviewActor(t), VPSID: reader.identity.Identity.VPSID,
	})
	close(reader.releaseMonitoring)
	select {
	case <-reader.monitoringDone:
	case <-time.After(time.Second):
		t.Fatal("late monitoring reader did not exit after release")
	}
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if overview.Summary.Monitoring.Section.ReasonCode != "monitoring_timeout" {
		t.Fatalf("monitoring = %#v", overview.Summary.Monitoring.Section)
	}
	if overview.Summary.IPQuality.Section.State != SectionReady ||
		overview.Summary.Renewal.Section.State != SectionReady ||
		overview.RecentActivity.Section.State != SectionReady ||
		overview.Relations[2].Section.State != SectionReady ||
		overview.Relations[3].Section.State != SectionReady {
		t.Fatalf("completed source was not preserved: %#v", overview)
	}
}

type cancelBlockingSourceReader struct {
	identity IdentitySource
	started  chan string
}

func (reader *cancelBlockingSourceReader) LoadIdentity(context.Context, string) (IdentitySource, error) {
	return reader.identity, nil
}

func (reader *cancelBlockingSourceReader) wait(ctx context.Context, source string) error {
	reader.started <- source
	<-ctx.Done()
	return ctx.Err()
}

func (reader *cancelBlockingSourceReader) LoadMonitoring(ctx context.Context, _ string) (MonitoringSource, error) {
	return MonitoringSource{}, reader.wait(ctx, "monitoring")
}

func (reader *cancelBlockingSourceReader) LoadIPQuality(ctx context.Context, _ string) (IPQualitySource, error) {
	return IPQualitySource{}, reader.wait(ctx, "ip_quality")
}

func (reader *cancelBlockingSourceReader) LoadRenewal(ctx context.Context, _ string, _ string) (RenewalSource, error) {
	return RenewalSource{}, reader.wait(ctx, "renewal")
}

func (reader *cancelBlockingSourceReader) LoadServiceRelation(ctx context.Context, _ string) (RelationSource, error) {
	return RelationSource{}, reader.wait(ctx, "services")
}

func (reader *cancelBlockingSourceReader) LoadDomainRelation(ctx context.Context, _ string) (RelationSource, error) {
	return RelationSource{}, reader.wait(ctx, "domains")
}

type cancelBlockingActivity struct {
	started chan<- string
}

func (lister cancelBlockingActivity) List(ctx context.Context, _ activity.ListRequest) (activity.ListResult, error) {
	lister.started <- "activity"
	<-ctx.Done()
	return activity.ListResult{}, ctx.Err()
}

func TestServiceGetCallerCancellationIsFatal(t *testing.T) {
	started := make(chan string, 6)
	reader := &cancelBlockingSourceReader{
		identity: healthyConfiguredSourceReader().identity,
		started:  started,
	}
	service, err := NewService(reader, cancelBlockingActivity{started: started})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, getErr := service.Get(ctx, Request{Actor: testOverviewActor(t), VPSID: reader.identity.Identity.VPSID})
		result <- getErr
	}()
	for range 6 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("not all sources started before cancellation")
		}
	}
	cancel()
	select {
	case getErr := <-result:
		if !errors.Is(getErr, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", getErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Get did not return after caller cancellation")
	}
}

type countingActivity struct {
	calls atomic.Int32
}

func (lister *countingActivity) List(context.Context, activity.ListRequest) (activity.ListResult, error) {
	lister.calls.Add(1)
	return activity.ListResult{}, nil
}

func TestServiceGetIdentityFailureDoesNotStartDegradableSources(t *testing.T) {
	reader := healthyConfiguredSourceReader()
	reader.identityErr = ErrVPSNotFound
	activityLister := &countingActivity{}
	service, err := NewService(reader, activityLister)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.Get(context.Background(), Request{
		Actor: testOverviewActor(t), VPSID: reader.identity.Identity.VPSID,
	})
	if !errors.Is(err, ErrVPSNotFound) {
		t.Fatalf("err = %v", err)
	}
	if reader.monitoringCalls.Load() != 0 || reader.ipQualityCalls.Load() != 0 ||
		reader.renewalCalls.Load() != 0 || reader.servicesCalls.Load() != 0 ||
		reader.domainsCalls.Load() != 0 || activityLister.calls.Load() != 0 {
		t.Fatalf("degradable sources started after identity failure: %#v", reader)
	}
}

type observedActivityLister struct {
	observedAt time.Time
	called     atomic.Bool
}

func (lister *observedActivityLister) List(context.Context, activity.ListRequest) (activity.ListResult, error) {
	lister.called.Store(true)
	observed := lister.observedAt
	return activity.ListResult{
		Items: []activity.Event{},
		Freshness: activity.Freshness{
			State:             SectionReady,
			VisibleObservedAt: &observed,
		},
	}, nil
}

func TestServiceGetCapturesGeneratedAtAfterCollectionAndRejectsFutureSourceTimestamps(t *testing.T) {
	generatedAt := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	past := generatedAt.Add(-time.Hour)
	future := generatedAt.Add(time.Hour)
	offsetPast := past.In(time.FixedZone("test-offset", 8*60*60))
	reader := healthyConfiguredSourceReader()
	reader.monitoring.Section = SectionState{
		State: SectionReady, ObservedAt: &future, LastSuccessAt: &future,
	}
	reader.ipQuality.Section = SectionState{
		State: SectionUnavailable, ObservedAt: &future, LastSuccessAt: &future,
		ReasonCode: "ip_quality_unavailable",
	}
	reader.renewal.Section = SectionState{
		State: SectionReady, ObservedAt: &past, LastSuccessAt: &past,
	}
	reader.services.Section = SectionState{
		State: SectionReady, ObservedAt: &future, LastSuccessAt: &future,
	}
	reader.domains.Section = SectionState{
		State: SectionReady, ObservedAt: &offsetPast, LastSuccessAt: &offsetPast,
	}
	activityLister := &observedActivityLister{observedAt: future}
	service, err := NewServiceWithBudgets(reader, activityLister, func() time.Time {
		if reader.monitoringCalls.Load() != 1 || reader.ipQualityCalls.Load() != 1 ||
			reader.renewalCalls.Load() != 1 || reader.servicesCalls.Load() != 1 ||
			reader.domainsCalls.Load() != 1 || !activityLister.called.Load() {
			t.Error("generated_at clock was read before source collection finished")
		}
		return generatedAt
	}, uniformSourceBudgets(time.Second))
	if err != nil {
		t.Fatalf("NewServiceWithBudgets: %v", err)
	}
	overview, err := service.Get(context.Background(), Request{
		Actor: testOverviewActor(t), VPSID: reader.identity.Identity.VPSID,
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	for name, section := range map[string]SectionState{
		"monitoring summary":  overview.Summary.Monitoring.Section,
		"monitoring relation": overview.Relations[0].Section,
		"service relation":    overview.Relations[2].Section,
		"activity":            overview.RecentActivity.Section,
	} {
		if section.State != SectionStale || section.ReasonCode != "source_timestamp_invalid" ||
			section.ObservedAt != nil || section.LastSuccessAt != nil {
			t.Errorf("%s = %#v", name, section)
		}
	}
	if section := overview.Summary.IPQuality.Section; section.State != SectionUnavailable ||
		section.ReasonCode != "ip_quality_unavailable" || section.ObservedAt != nil || section.LastSuccessAt != nil {
		t.Errorf("unavailable IP section = %#v", section)
	}
	if section := overview.Summary.Renewal.Section; section.State != SectionReady ||
		section.ObservedAt == nil || !section.ObservedAt.Equal(past) {
		t.Errorf("past renewal section = %#v", section)
	}
	if section := overview.Relations[3].Section; section.ObservedAt == nil || section.ObservedAt.Location() != time.UTC ||
		!section.ObservedAt.Equal(past) {
		t.Errorf("domain timestamp was not normalized to UTC: %#v", section)
	}
	if !overview.GeneratedAt.Equal(generatedAt) || overview.Summary.Overall.Section.ObservedAt == nil ||
		!overview.Summary.Overall.Section.ObservedAt.Equal(generatedAt) {
		t.Errorf("generated freshness = %#v", overview.Summary.Overall.Section)
	}
	for _, anomaly := range overview.Anomalies {
		if anomaly.Source == "monitoring" && anomaly.EventAt != nil {
			t.Errorf("future monitoring timestamp leaked into anomaly: %#v", anomaly)
		}
	}
}

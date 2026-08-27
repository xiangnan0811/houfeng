package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsoverview"
)

type fakeVPSRepo struct {
	record vpsassets.Record
	err    error
	calls  int
	vpsID  string
}

func (fake *fakeVPSRepo) GetVPSAsset(_ context.Context, vpsID string) (vpsassets.Record, error) {
	fake.calls++
	fake.vpsID = vpsID
	if fake.err != nil {
		return vpsassets.Record{}, fake.err
	}
	return fake.record, nil
}

type fakeMonitoringLinks struct {
	links []assetlinks.MonitoringInstanceSummary
	err   error
	calls int
	vpsID string
}

func (fake *fakeMonitoringLinks) ListMonitoringInstancesForVPS(_ context.Context, vpsID string) ([]assetlinks.MonitoringInstanceSummary, error) {
	fake.calls++
	fake.vpsID = vpsID
	return fake.links, fake.err
}

type fakeIPQuality struct {
	report          ipquality.VPSReport
	summary         *ipquality.Summary
	err             error
	blockUntilDone  bool
	calls           int
	summaryCalls    int
	fullReportCalls int
	vpsID           string
}

func (fake *fakeIPQuality) GetVPSIPQuality(_ context.Context, vpsID string) (ipquality.VPSReport, error) {
	fake.calls++
	fake.fullReportCalls++
	fake.vpsID = vpsID
	return fake.report, fake.err
}

func (fake *fakeIPQuality) GetLatestVPSIPQualitySummary(ctx context.Context, vpsID string) (*ipquality.Summary, error) {
	fake.calls++
	fake.summaryCalls++
	fake.vpsID = vpsID
	if fake.blockUntilDone {
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond)
		return nil, ctx.Err()
	}
	if fake.err != nil {
		return nil, fake.err
	}
	if fake.summary != nil {
		return fake.summary, nil
	}
	return fake.report.Summary, nil
}

type fakeSubscriptions struct {
	rows   []subscriptions.Record
	err    error
	calls  int
	filter subscriptions.ListFilters
}

func (fake *fakeSubscriptions) ListSubscriptions(_ context.Context, filter subscriptions.ListFilters) ([]subscriptions.Record, error) {
	fake.calls++
	fake.filter = filter
	return fake.rows, fake.err
}

type fakeServices struct {
	rows  []assetservices.Record
	err   error
	calls int
	vpsID string
}

func (fake *fakeServices) ListAssetServicesForVPS(_ context.Context, vpsID string) ([]assetservices.Record, error) {
	fake.calls++
	fake.vpsID = vpsID
	return fake.rows, fake.err
}

type fakeDomains struct {
	rows  []assetdomains.Record
	err   error
	calls int
	vpsID string
}

func (fake *fakeDomains) ListAssetDomainsForVPS(_ context.Context, vpsID string) ([]assetdomains.Record, error) {
	fake.calls++
	fake.vpsID = vpsID
	return fake.rows, fake.err
}

func TestVPSOverviewRepositoryLoadsGranularSourcesWithTruthfulFreshness(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	renew := subscriptions.Date{Time: now.Add(5 * 24 * time.Hour)}
	renewalUpdated := now.Add(-time.Hour)
	serviceUpdated := now.Add(-2 * time.Hour)
	domainUpdated := now.Add(-3 * time.Hour)
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{record: vpsassets.Record{
			VPSID: "vps_7c2a4e18b09d5f31", DisplayName: "Alpha", ProviderName: "Example",
			LifecycleStatus: vpsassets.LifecycleActive, RenewalDecision: vpsassets.RenewalKeep,
			OSName: "Debian", Labels: []string{}, UpdatedAt: now,
		}},
		&fakeMonitoringLinks{links: []assetlinks.MonitoringInstanceSummary{{
			MonitoringInstanceID: "mi_1", CurrentHealthStatus: "正常", MonitoringStatus: "启用",
			LifecycleStatus: "在用", LastHeartbeatAt: &now,
		}}},
		&fakeIPQuality{report: ipquality.VPSReport{Summary: &ipquality.Summary{
			Status: "success", RiskLevel: "low", ObservedAt: now,
		}}},
		&fakeIPQualityAvailability{enabled: true},
		&fakeSubscriptions{rows: []subscriptions.Record{{
			Status: subscriptions.StatusActive, RenewAt: &renew, UpdatedAt: now.Add(-2 * time.Hour),
		}, {
			Status: subscriptions.StatusPaused, UpdatedAt: renewalUpdated,
		}}},
		&fakeServices{rows: []assetservices.Record{{UpdatedAt: now.Add(-4 * time.Hour)}, {UpdatedAt: serviceUpdated}}},
		&fakeDomains{rows: []assetdomains.Record{{UpdatedAt: domainUpdated}, {UpdatedAt: now.Add(-5 * time.Hour)}}},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	identity, err := repo.LoadIdentity(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	monitoring, err := repo.LoadMonitoring(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadMonitoring: %v", err)
	}
	ipQuality, err := repo.LoadIPQuality(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadIPQuality: %v", err)
	}
	renewal, err := repo.LoadRenewal(context.Background(), "vps_7c2a4e18b09d5f31", "keep")
	if err != nil {
		t.Fatalf("LoadRenewal: %v", err)
	}
	services, err := repo.LoadServiceRelation(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadServiceRelation: %v", err)
	}
	domains, err := repo.LoadDomainRelation(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadDomainRelation: %v", err)
	}

	if identity.Identity.DisplayName != "Alpha" || monitoring.Health != "正常" || monitoring.Count != 1 {
		t.Fatalf("identity=%#v monitoring=%#v", identity, monitoring)
	}
	if monitoring.Section.ObservedAt == nil || !monitoring.Section.ObservedAt.Equal(now) {
		t.Fatalf("monitoring freshness = %#v", monitoring.Section)
	}
	if ipQuality.Section.ObservedAt == nil || !ipQuality.Section.ObservedAt.Equal(now) {
		t.Fatalf("ip freshness = %#v", ipQuality.Section)
	}
	if renewal.ActiveSubscriptions != 1 || renewal.NextRenewAt == nil || !renewal.NextRenewAt.Equal(renew.Time) {
		t.Fatalf("renewal = %#v", renewal)
	}
	if renewal.Section.ObservedAt == nil || !renewal.Section.ObservedAt.Equal(renewalUpdated) {
		t.Fatalf("renewal freshness = %#v, want max UpdatedAt %s (not RenewAt %s)", renewal.Section, renewalUpdated, renew.Time)
	}
	if services.Count != 2 || services.Section.ObservedAt == nil || !services.Section.ObservedAt.Equal(serviceUpdated) {
		t.Fatalf("services = %#v", services)
	}
	if domains.Count != 2 || domains.Section.ObservedAt == nil || !domains.Section.ObservedAt.Equal(domainUpdated) {
		t.Fatalf("domains = %#v", domains)
	}
}

func TestVPSOverviewRepositoryNotFound(t *testing.T) {
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{err: vpsassets.ErrVPSAssetNotFound},
		&fakeMonitoringLinks{}, &fakeIPQuality{}, &fakeIPQualityAvailability{}, &fakeSubscriptions{},
		&fakeServices{}, &fakeDomains{},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	_, err = repo.LoadIdentity(context.Background(), "vps_missing")
	if !errors.Is(err, vpsoverview.ErrVPSNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestVPSOverviewRepositoryEmptyRenewalIsReadyWithoutInventedTimestamp(t *testing.T) {
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{}, &fakeMonitoringLinks{}, &fakeIPQuality{}, &fakeIPQualityAvailability{}, &fakeSubscriptions{},
		&fakeServices{}, &fakeDomains{},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	renewal, err := repo.LoadRenewal(context.Background(), "vps_7c2a4e18b09d5f31", "observe")
	if err != nil {
		t.Fatalf("LoadRenewal: %v", err)
	}
	if renewal.Section.State != vpsoverview.SectionReady || renewal.Section.ObservedAt != nil || renewal.Section.LastSuccessAt != nil {
		t.Fatalf("renewal = %#v", renewal)
	}
}

func TestVPSOverviewRepositoryGranularReadersUseOneBoundedQueryEach(t *testing.T) {
	const vpsID = "vps_7c2a4e18b09d5f31"
	vps := &fakeVPSRepo{record: vpsassets.Record{VPSID: vpsID, Labels: []string{}}}
	monitoring := &fakeMonitoringLinks{}
	ip := &fakeIPQuality{}
	subs := &fakeSubscriptions{}
	services := &fakeServices{}
	domains := &fakeDomains{}
	availability := &fakeIPQualityAvailability{enabled: true}
	repo, err := NewVPSOverviewRepository(vps, monitoring, ip, availability, subs, services, domains)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}

	if _, err := repo.LoadIdentity(context.Background(), vpsID); err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if _, err := repo.LoadMonitoring(context.Background(), vpsID); err != nil {
		t.Fatalf("LoadMonitoring: %v", err)
	}
	if _, err := repo.LoadIPQuality(context.Background(), vpsID); err != nil {
		t.Fatalf("LoadIPQuality: %v", err)
	}
	if _, err := repo.LoadRenewal(context.Background(), vpsID, "keep"); err != nil {
		t.Fatalf("LoadRenewal: %v", err)
	}
	if _, err := repo.LoadServiceRelation(context.Background(), vpsID); err != nil {
		t.Fatalf("LoadServiceRelation: %v", err)
	}
	if _, err := repo.LoadDomainRelation(context.Background(), vpsID); err != nil {
		t.Fatalf("LoadDomainRelation: %v", err)
	}

	if vps.calls != 1 || monitoring.calls != 1 || ip.calls != 1 || availability.calls != 1 ||
		subs.calls != 1 || services.calls != 1 || domains.calls != 1 {
		t.Fatalf("authority call counts vps=%d monitoring=%d ip=%d availability=%d subscriptions=%d services=%d domains=%d",
			vps.calls, monitoring.calls, ip.calls, availability.calls, subs.calls, services.calls, domains.calls)
	}
	if ip.fullReportCalls != 0 {
		t.Fatalf("GetVPSIPQuality calls = %d, want 0", ip.fullReportCalls)
	}
	if vps.vpsID != vpsID || monitoring.vpsID != vpsID || ip.vpsID != vpsID ||
		services.vpsID != vpsID || domains.vpsID != vpsID {
		t.Fatalf("granular readers used the wrong VPS scope")
	}
	if subs.filter.VPSID != vpsID || subs.filter.Sort != subscriptions.SortRenewAt ||
		subs.filter.Order != subscriptions.OrderAsc {
		t.Fatalf("subscription filter = %#v", subs.filter)
	}
}

func TestVPSOverviewRepositoryRelationEmptyVersusError(t *testing.T) {
	const vpsID = "vps_7c2a4e18b09d5f31"
	services := &fakeServices{}
	domains := &fakeDomains{}
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{}, &fakeMonitoringLinks{}, &fakeIPQuality{}, &fakeIPQualityAvailability{}, &fakeSubscriptions{}, services, domains,
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}

	serviceRelation, err := repo.LoadServiceRelation(context.Background(), vpsID)
	if err != nil {
		t.Fatalf("empty services: %v", err)
	}
	domainRelation, err := repo.LoadDomainRelation(context.Background(), vpsID)
	if err != nil {
		t.Fatalf("empty domains: %v", err)
	}
	for name, relation := range map[string]vpsoverview.RelationSource{
		"services": serviceRelation,
		"domains":  domainRelation,
	} {
		if relation.Count != 0 || relation.Section.State != vpsoverview.SectionReady ||
			relation.Section.ObservedAt != nil || relation.Section.LastSuccessAt != nil {
			t.Errorf("empty %s relation = %#v", name, relation)
		}
	}

	serviceErr := errors.New("service authority unavailable")
	domainErr := errors.New("domain authority unavailable")
	services.err = serviceErr
	domains.err = domainErr
	if _, err := repo.LoadServiceRelation(context.Background(), vpsID); !errors.Is(err, serviceErr) {
		t.Fatalf("service error = %v", err)
	}
	if _, err := repo.LoadDomainRelation(context.Background(), vpsID); !errors.Is(err, domainErr) {
		t.Fatalf("domain error = %v", err)
	}
}

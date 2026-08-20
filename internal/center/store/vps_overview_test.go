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
}

func (fake *fakeVPSRepo) GetVPSAsset(context.Context, string) (vpsassets.Record, error) {
	if fake.err != nil {
		return vpsassets.Record{}, fake.err
	}
	return fake.record, nil
}

type fakeMonitoringLinks struct {
	links []assetlinks.MonitoringInstanceSummary
	err   error
}

func (fake *fakeMonitoringLinks) ListMonitoringInstancesForVPS(context.Context, string) ([]assetlinks.MonitoringInstanceSummary, error) {
	return fake.links, fake.err
}

type fakeIPQuality struct {
	report ipquality.VPSReport
	err    error
}

func (fake *fakeIPQuality) GetVPSIPQuality(context.Context, string) (ipquality.VPSReport, error) {
	return fake.report, fake.err
}

type fakeSubscriptions struct {
	rows []subscriptions.Record
	err  error
}

func (fake *fakeSubscriptions) ListSubscriptions(context.Context, subscriptions.ListFilters) ([]subscriptions.Record, error) {
	return fake.rows, fake.err
}

type fakeServices struct {
	rows []assetservices.Record
	err  error
}

func (fake *fakeServices) ListAssetServicesForVPS(context.Context, string) ([]assetservices.Record, error) {
	return fake.rows, fake.err
}

type fakeDomains struct {
	rows []assetdomains.Record
	err  error
}

func (fake *fakeDomains) ListAssetDomainsForVPS(context.Context, string) ([]assetdomains.Record, error) {
	return fake.rows, fake.err
}

func TestVPSOverviewRepositoryLoadSourcesHealthy(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	renew := subscriptions.Date{Time: now.Add(5 * 24 * time.Hour)}
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
		&fakeSubscriptions{rows: []subscriptions.Record{{
			Status: subscriptions.StatusActive, RenewAt: &renew,
		}}},
		&fakeServices{rows: []assetservices.Record{{}}},
		&fakeDomains{rows: []assetdomains.Record{{}, {}}},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	bundle, err := repo.LoadSources(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if bundle.Identity.DisplayName != "Alpha" || bundle.MonitoringHealth != "正常" {
		t.Fatalf("bundle = %#v", bundle)
	}
	if bundle.ActiveSubscriptions != 1 || bundle.NextRenewAt == nil {
		t.Fatalf("renewal = %#v", bundle)
	}
	if len(bundle.Relations) != 4 {
		t.Fatalf("relations = %#v", bundle.Relations)
	}
}

func TestVPSOverviewRepositoryNotFound(t *testing.T) {
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{err: vpsassets.ErrVPSAssetNotFound},
		&fakeMonitoringLinks{}, &fakeIPQuality{}, &fakeSubscriptions{},
		&fakeServices{}, &fakeDomains{},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	_, err = repo.LoadSources(context.Background(), "vps_missing")
	if !errors.Is(err, vpsoverview.ErrVPSNotFound) {
		t.Fatalf("err = %v", err)
	}
}

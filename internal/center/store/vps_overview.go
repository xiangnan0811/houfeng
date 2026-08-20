package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsoverview"
)

type vpsOverviewAssetSource interface {
	GetVPSAsset(context.Context, string) (vpsassets.Record, error)
}

type vpsOverviewMonitoringSource interface {
	ListMonitoringInstancesForVPS(context.Context, string) ([]assetlinks.MonitoringInstanceSummary, error)
}

type vpsOverviewIPQualitySource interface {
	GetVPSIPQuality(context.Context, string) (ipquality.VPSReport, error)
}

type vpsOverviewSubscriptionSource interface {
	ListSubscriptions(context.Context, subscriptions.ListFilters) ([]subscriptions.Record, error)
}

type vpsOverviewServiceSource interface {
	ListAssetServicesForVPS(context.Context, string) ([]assetservices.Record, error)
}

type vpsOverviewDomainSource interface {
	ListAssetDomainsForVPS(context.Context, string) ([]assetdomains.Record, error)
}

// VPSOverviewRepository loads non-activity overview facts from existing
// repositories. It does not invent a second timeline or status authority.
type VPSOverviewRepository struct {
	vps           vpsOverviewAssetSource
	monitoring    vpsOverviewMonitoringSource
	ipQuality     vpsOverviewIPQualitySource
	subscriptions vpsOverviewSubscriptionSource
	services      vpsOverviewServiceSource
	domains       vpsOverviewDomainSource
}

// NewVPSOverviewRepository wires the overview source reader.
func NewVPSOverviewRepository(
	vps vpsOverviewAssetSource,
	monitoring vpsOverviewMonitoringSource,
	ipQuality vpsOverviewIPQualitySource,
	subs vpsOverviewSubscriptionSource,
	services vpsOverviewServiceSource,
	domains vpsOverviewDomainSource,
) (*VPSOverviewRepository, error) {
	if vps == nil || monitoring == nil || ipQuality == nil || subs == nil || services == nil || domains == nil {
		return nil, fmt.Errorf("%w: dependency", vpsoverview.ErrInvalidOverviewRequest)
	}
	return &VPSOverviewRepository{
		vps: vps, monitoring: monitoring, ipQuality: ipQuality,
		subscriptions: subs, services: services, domains: domains,
	}, nil
}

var _ vpsoverview.SourceReader = (*VPSOverviewRepository)(nil)

// LoadSources returns identity (fatal) plus degraded-capable sections.
func (repository *VPSOverviewRepository) LoadSources(
	ctx context.Context,
	vpsID string,
) (vpsoverview.SourceBundle, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.SourceBundle{}, vpsoverview.ErrInvalidOverviewRequest
	}
	record, err := repository.vps.GetVPSAsset(ctx, vpsID)
	if err != nil {
		if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
			return vpsoverview.SourceBundle{}, vpsoverview.ErrVPSNotFound
		}
		return vpsoverview.SourceBundle{}, err
	}

	bundle := vpsoverview.SourceBundle{
		Identity: identityFromVPS(record),
		Facts:    factsFromVPS(record),
	}

	monitoringLinks, monitoringErr := repository.monitoring.ListMonitoringInstancesForVPS(ctx, vpsID)
	bundle.MonitoringSection, bundle.MonitoringHealth, bundle.MonitoringStatus,
		bundle.MonitoringDetail, bundle.ActiveIncidents = monitoringFromLinks(monitoringLinks, monitoringErr)

	bundle.IPSection, bundle.IPStatus, bundle.IPRiskLevel, bundle.IPStale =
		repository.loadIPQuality(ctx, vpsID)

	bundle.RenewalSection, bundle.ActiveSubscriptions, bundle.NextRenewAt, bundle.RenewalStatus =
		repository.loadRenewal(ctx, vpsID, record.RenewalDecision)

	monitoringCount := 0
	if monitoringErr == nil {
		monitoringCount = len(monitoringLinks)
	}
	bundle.Relations = repository.loadRelations(ctx, vpsID, bundle, monitoringCount)
	return bundle, nil
}

func identityFromVPS(record vpsassets.Record) vpsoverview.Identity {
	labels := record.Labels
	if labels == nil {
		labels = []string{}
	}
	return vpsoverview.Identity{
		VPSID:           record.VPSID,
		DisplayName:     record.DisplayName,
		ProviderName:    record.ProviderName,
		ProductName:     record.ProductName,
		Country:         record.Country,
		Region:          record.Region,
		City:            record.City,
		Datacenter:      record.Datacenter,
		IPv4:            record.IPv4,
		IPv6:            record.IPv6,
		LifecycleStatus: string(record.LifecycleStatus),
		UsageStatus:     string(record.UsageStatus),
		RenewalDecision: string(record.RenewalDecision),
		Importance:      record.Importance,
		Labels:          labels,
		UpdatedAt:       record.UpdatedAt.UTC(),
	}
}

func factsFromVPS(record vpsassets.Record) []vpsoverview.Fact {
	facts := make([]vpsoverview.Fact, 0, 8)
	add := func(key, label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		facts = append(facts, vpsoverview.Fact{Key: key, Label: label, Value: value})
	}
	add("product_name", "产品", record.ProductName)
	add("order_ref", "订单号", record.OrderRef)
	add("os_name", "系统", record.OSName)
	add("virtualization", "虚拟化", record.Virtualization)
	if record.SSHHost != "" {
		user := record.SSHUser
		if user == "" {
			user = "root"
		}
		add("ssh", "SSH", fmt.Sprintf("%s@%s:%d", user, record.SSHHost, record.SSHPort))
	}
	add("ipv4", "IPv4", record.IPv4)
	add("ipv6", "IPv6", record.IPv6)
	add("datacenter", "机房", record.Datacenter)
	return facts
}

func monitoringFromLinks(
	links []assetlinks.MonitoringInstanceSummary,
	err error,
) (section vpsoverview.SectionState, health, status, detail string, incidents int) {
	if err != nil {
		return vpsoverview.SectionState{
			State: vpsoverview.SectionUnavailable, ReasonCode: "monitoring_unavailable",
		}, "", "", "", 0
	}
	section = vpsoverview.SectionState{State: vpsoverview.SectionReady}
	if len(links) == 0 {
		return section, "", "unlinked", "未关联监控实例", 0
	}
	primary := links[0]
	for _, link := range links[1:] {
		if link.LifecycleStatus == "在用" && primary.LifecycleStatus != "在用" {
			primary = link
		}
	}
	health = primary.CurrentHealthStatus
	status = primary.MonitoringStatus
	detail = primary.CurrentPrimaryIssueSummary
	incidents = primary.CurrentActiveIncidentCount
	if primary.LastHeartbeatAt != nil {
		observed := primary.LastHeartbeatAt.UTC()
		section.ObservedAt = &observed
		section.LastSuccessAt = &observed
	}
	return section, health, status, detail, incidents
}

func (repository *VPSOverviewRepository) loadIPQuality(
	ctx context.Context,
	vpsID string,
) (section vpsoverview.SectionState, status, risk string, stale bool) {
	section = vpsoverview.SectionState{State: vpsoverview.SectionReady}
	report, err := repository.ipQuality.GetVPSIPQuality(ctx, vpsID)
	if err != nil {
		return vpsoverview.SectionState{
			State: vpsoverview.SectionUnavailable, ReasonCode: "ip_quality_unavailable",
		}, "", "", false
	}
	if report.Summary == nil {
		return section, "missing", "", false
	}
	summary := report.Summary
	status = summary.Status
	risk = summary.RiskLevel
	stale = summary.Stale
	observed := summary.ObservedAt.UTC()
	section.ObservedAt = &observed
	section.LastSuccessAt = &observed
	if stale {
		section.State = vpsoverview.SectionStale
		section.ReasonCode = "ip_quality_stale"
	}
	return section, status, risk, stale
}

func (repository *VPSOverviewRepository) loadRenewal(
	ctx context.Context,
	vpsID string,
	decision vpsassets.RenewalDecision,
) (section vpsoverview.SectionState, activeCount int, nextRenew *time.Time, status string) {
	section = vpsoverview.SectionState{State: vpsoverview.SectionReady}
	status = string(decision)
	rows, err := repository.subscriptions.ListSubscriptions(ctx, subscriptions.ListFilters{
		VPSID: vpsID,
		Sort:  subscriptions.SortRenewAt,
		Order: subscriptions.OrderAsc,
	})
	if err != nil {
		return vpsoverview.SectionState{
			State: vpsoverview.SectionUnavailable, ReasonCode: "subscription_unavailable",
		}, 0, nil, status
	}
	for _, row := range rows {
		if row.Status == subscriptions.StatusActive {
			activeCount++
			if row.RenewAt != nil && !row.RenewAt.Time.IsZero() {
				candidate := row.RenewAt.Time.UTC()
				if nextRenew == nil || candidate.Before(*nextRenew) {
					nextRenew = &candidate
				}
			}
		}
	}
	if nextRenew != nil {
		section.ObservedAt = nextRenew
		section.LastSuccessAt = nextRenew
	}
	return section, activeCount, nextRenew, status
}

func (repository *VPSOverviewRepository) loadRelations(
	ctx context.Context,
	vpsID string,
	bundle vpsoverview.SourceBundle,
	monitoringCount int,
) []vpsoverview.RelationSummary {
	route := "/vps/" + vpsID
	relations := []vpsoverview.RelationSummary{{
		Kind:   "monitoring_instances",
		Label:  "监控实例",
		Route:  route,
		Count:  monitoringCount,
		Status: firstNonEmptyLocal(bundle.MonitoringHealth, bundle.MonitoringStatus),
	}, {
		Kind:   "subscriptions",
		Label:  "订阅",
		Route:  route,
		Count:  bundle.ActiveSubscriptions,
		Status: bundle.RenewalStatus,
	}}
	if services, err := repository.services.ListAssetServicesForVPS(ctx, vpsID); err == nil {
		relations = append(relations, vpsoverview.RelationSummary{
			Kind: "services", Label: "服务", Route: route, Count: len(services),
		})
	} else {
		relations = append(relations, vpsoverview.RelationSummary{
			Kind: "services", Label: "服务", Route: route, Count: 0, Status: "unavailable",
		})
	}
	if domains, err := repository.domains.ListAssetDomainsForVPS(ctx, vpsID); err == nil {
		relations = append(relations, vpsoverview.RelationSummary{
			Kind: "domains", Label: "域名", Route: route, Count: len(domains),
		})
	} else {
		relations = append(relations, vpsoverview.RelationSummary{
			Kind: "domains", Label: "域名", Route: route, Count: 0, Status: "unavailable",
		})
	}
	return relations
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

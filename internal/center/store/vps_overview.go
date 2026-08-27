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
	GetLatestVPSIPQualitySummary(context.Context, string) (*ipquality.Summary, error)
}

type vpsOverviewIPQualityAvailability interface {
	IPQualityEnabled(context.Context) (bool, error)
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

// VPSOverviewRepository exposes one authority query per independently bounded
// overview source. It does not map source failures to wire states; the overview
// service owns that closed, safe error vocabulary.
type VPSOverviewRepository struct {
	vps           vpsOverviewAssetSource
	monitoring    vpsOverviewMonitoringSource
	ipQuality     vpsOverviewIPQualitySource
	availability  vpsOverviewIPQualityAvailability
	subscriptions vpsOverviewSubscriptionSource
	services      vpsOverviewServiceSource
	domains       vpsOverviewDomainSource
}

// NewVPSOverviewRepository wires the overview source reader.
func NewVPSOverviewRepository(
	vps vpsOverviewAssetSource,
	monitoring vpsOverviewMonitoringSource,
	ipQuality vpsOverviewIPQualitySource,
	availability vpsOverviewIPQualityAvailability,
	subs vpsOverviewSubscriptionSource,
	services vpsOverviewServiceSource,
	domains vpsOverviewDomainSource,
) (*VPSOverviewRepository, error) {
	if vps == nil || monitoring == nil || ipQuality == nil || availability == nil ||
		subs == nil || services == nil || domains == nil {
		return nil, fmt.Errorf("%w: dependency", vpsoverview.ErrInvalidOverviewRequest)
	}
	return &VPSOverviewRepository{
		vps: vps, monitoring: monitoring, ipQuality: ipQuality, availability: availability,
		subscriptions: subs, services: services, domains: domains,
	}, nil
}

var _ vpsoverview.SourceReader = (*VPSOverviewRepository)(nil)

// LoadIdentity performs the fatal authority read before any degradable source
// is allowed to start.
func (repository *VPSOverviewRepository) LoadIdentity(
	ctx context.Context,
	vpsID string,
) (vpsoverview.IdentitySource, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.IdentitySource{}, vpsoverview.ErrInvalidOverviewRequest
	}
	record, err := repository.vps.GetVPSAsset(ctx, vpsID)
	if err != nil {
		if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
			return vpsoverview.IdentitySource{}, vpsoverview.ErrVPSNotFound
		}
		return vpsoverview.IdentitySource{}, err
	}
	return vpsoverview.IdentitySource{
		Identity: identityFromVPS(record),
		Facts:    factsFromVPS(record),
	}, nil
}

// LoadMonitoring performs exactly one monitoring authority query.
func (repository *VPSOverviewRepository) LoadMonitoring(
	ctx context.Context,
	vpsID string,
) (vpsoverview.MonitoringSource, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.MonitoringSource{}, vpsoverview.ErrInvalidOverviewRequest
	}
	links, err := repository.monitoring.ListMonitoringInstancesForVPS(ctx, vpsID)
	if err != nil {
		return vpsoverview.MonitoringSource{}, err
	}
	return monitoringFromLinks(links), nil
}

// LoadIPQuality performs one availability check. When enabled it also does one
// summary-only query. When disabled it returns immediately; history is not part
// of current Overview judgement and must not share the request deadline.
func (repository *VPSOverviewRepository) LoadIPQuality(
	ctx context.Context,
	vpsID string,
) (vpsoverview.IPQualitySource, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.IPQualitySource{}, vpsoverview.ErrInvalidOverviewRequest
	}
	enabled, err := repository.availability.IPQualityEnabled(ctx)
	if err != nil {
		return vpsoverview.IPQualitySource{}, err
	}
	result := vpsoverview.IPQualitySource{Section: vpsoverview.SectionState{State: vpsoverview.SectionReady}}
	if !enabled {
		result.Status = "not_configured"
		return result, nil
	}
	summary, err := repository.ipQuality.GetLatestVPSIPQualitySummary(ctx, vpsID)
	if err != nil {
		return vpsoverview.IPQualitySource{}, err
	}
	if summary == nil {
		result.Status = "missing"
		return result, nil
	}
	result.Status = summary.Status
	result.RiskLevel = summary.RiskLevel
	result.Stale = summary.Stale
	if !summary.ObservedAt.IsZero() {
		observed := summary.ObservedAt.UTC()
		result.Section.ObservedAt = &observed
		result.Section.LastSuccessAt = &observed
	}
	if result.Stale {
		result.Section.State = vpsoverview.SectionStale
		result.Section.ReasonCode = "ip_quality_stale"
	}
	return result, nil
}

// LoadRenewal performs exactly one subscription authority query. RenewAt is a
// business deadline; freshness comes only from the maximum row UpdatedAt.
func (repository *VPSOverviewRepository) LoadRenewal(
	ctx context.Context,
	vpsID string,
	decision string,
) (vpsoverview.RenewalSource, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.RenewalSource{}, vpsoverview.ErrInvalidOverviewRequest
	}
	rows, err := repository.subscriptions.ListSubscriptions(ctx, subscriptions.ListFilters{
		VPSID: vpsID,
		Sort:  subscriptions.SortRenewAt,
		Order: subscriptions.OrderAsc,
	})
	if err != nil {
		return vpsoverview.RenewalSource{}, err
	}
	result := vpsoverview.RenewalSource{
		Section: vpsoverview.SectionState{State: vpsoverview.SectionReady},
		Status:  decision,
	}
	var lastUpdated *time.Time
	for _, row := range rows {
		lastUpdated = newestNonZero(lastUpdated, row.UpdatedAt)
		if row.Status != subscriptions.StatusActive {
			continue
		}
		result.ActiveSubscriptions++
		if row.RenewAt == nil || row.RenewAt.Time.IsZero() {
			continue
		}
		candidate := row.RenewAt.Time.UTC()
		if result.NextRenewAt == nil || candidate.Before(*result.NextRenewAt) {
			result.NextRenewAt = &candidate
		}
	}
	result.Section.ObservedAt = cloneTime(lastUpdated)
	result.Section.LastSuccessAt = cloneTime(lastUpdated)
	return result, nil
}

// LoadServiceRelation performs exactly one service authority query.
func (repository *VPSOverviewRepository) LoadServiceRelation(
	ctx context.Context,
	vpsID string,
) (vpsoverview.RelationSource, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.RelationSource{}, vpsoverview.ErrInvalidOverviewRequest
	}
	rows, err := repository.services.ListAssetServicesForVPS(ctx, vpsID)
	if err != nil {
		return vpsoverview.RelationSource{}, err
	}
	var lastUpdated *time.Time
	for _, row := range rows {
		lastUpdated = newestNonZero(lastUpdated, row.UpdatedAt)
	}
	return vpsoverview.RelationSource{
		Count: len(rows),
		Section: vpsoverview.SectionState{
			State: vpsoverview.SectionReady, ObservedAt: cloneTime(lastUpdated), LastSuccessAt: cloneTime(lastUpdated),
		},
	}, nil
}

// LoadDomainRelation performs exactly one domain authority query.
func (repository *VPSOverviewRepository) LoadDomainRelation(
	ctx context.Context,
	vpsID string,
) (vpsoverview.RelationSource, error) {
	if ctx == nil || repository == nil {
		return vpsoverview.RelationSource{}, vpsoverview.ErrInvalidOverviewRequest
	}
	rows, err := repository.domains.ListAssetDomainsForVPS(ctx, vpsID)
	if err != nil {
		return vpsoverview.RelationSource{}, err
	}
	var lastUpdated *time.Time
	for _, row := range rows {
		lastUpdated = newestNonZero(lastUpdated, row.UpdatedAt)
	}
	return vpsoverview.RelationSource{
		Count: len(rows),
		Section: vpsoverview.SectionState{
			State: vpsoverview.SectionReady, ObservedAt: cloneTime(lastUpdated), LastSuccessAt: cloneTime(lastUpdated),
		},
	}, nil
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

func monitoringFromLinks(links []assetlinks.MonitoringInstanceSummary) vpsoverview.MonitoringSource {
	result := vpsoverview.MonitoringSource{
		Section: vpsoverview.SectionState{State: vpsoverview.SectionReady},
		Count:   len(links),
	}
	if len(links) == 0 {
		result.Status = "unlinked"
		result.Detail = "未关联监控实例"
		return result
	}
	primary := links[0]
	for _, link := range links[1:] {
		if link.LifecycleStatus == "在用" && primary.LifecycleStatus != "在用" {
			primary = link
		}
	}
	result.MonitoringInstanceID = primary.MonitoringInstanceID
	result.Health = primary.CurrentHealthStatus
	result.Status = primary.MonitoringStatus
	result.Detail = primary.CurrentPrimaryIssueSummary
	result.ActiveIncidents = primary.CurrentActiveIncidentCount
	if primary.LastHeartbeatAt != nil && !primary.LastHeartbeatAt.IsZero() {
		observed := primary.LastHeartbeatAt.UTC()
		result.Section.ObservedAt = &observed
		result.Section.LastSuccessAt = &observed
	}
	return result
}

func newestNonZero(current *time.Time, candidate time.Time) *time.Time {
	if candidate.IsZero() {
		return current
	}
	candidate = candidate.UTC()
	if current == nil || candidate.After(*current) {
		return &candidate
	}
	return current
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

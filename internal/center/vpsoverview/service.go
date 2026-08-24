package vpsoverview

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
)

var (
	// ErrVPSNotFound unifies missing and unauthorized VPS for HTTP 404.
	ErrVPSNotFound = errors.New("vps overview not found")
	// ErrInvalidOverviewRequest reports unusable service input.
	ErrInvalidOverviewRequest = errors.New("invalid vps overview request")
)

// Request is one overview load from transport.
type Request struct {
	Actor recordauth.ActorScope
	VPSID string
}

// IdentitySource is the fatal identity read and its stable facts.
type IdentitySource struct {
	Identity Identity
	Facts    []Fact
}

// MonitoringSource is one bounded monitoring read. Count is reused by the
// monitoring relation so aggregation never repeats the authority query.
type MonitoringSource struct {
	Section         SectionState
	Health          string
	Status          string
	Detail          string
	ActiveIncidents int
	Count           int
}

// IPQualitySource is one bounded IP-quality read.
type IPQualitySource struct {
	Section   SectionState
	Status    string
	RiskLevel string
	Stale     bool
}

// RenewalSource is one bounded subscription read. ActiveSubscriptions and its
// Section are reused by the subscription relation.
type RenewalSource struct {
	Section             SectionState
	ActiveSubscriptions int
	NextRenewAt         *time.Time
	Status              string
}

// RelationSource is one bounded service/domain relation read.
type RelationSource struct {
	Count   int
	Section SectionState
}

// SourceBundle remains as a data fixture shape for compatibility tests. It is
// not a SourceReader contract; production aggregation uses the granular types.
type SourceBundle struct {
	Identity Identity

	MonitoringSection SectionState
	MonitoringStatus  string
	MonitoringDetail  string
	ActiveIncidents   int
	MonitoringHealth  string

	IPSection   SectionState
	IPStatus    string
	IPRiskLevel string
	IPStale     bool

	RenewalSection      SectionState
	ActiveSubscriptions int
	NextRenewAt         *time.Time
	RenewalStatus       string

	Relations []RelationSummary
	Facts     []Fact
}

// SourceReader exposes independently bounded authority reads. Identity is
// always loaded first; the remaining methods are safe to run concurrently.
type SourceReader interface {
	LoadIdentity(context.Context, string) (IdentitySource, error)
	LoadMonitoring(context.Context, string) (MonitoringSource, error)
	LoadIPQuality(context.Context, string) (IPQualitySource, error)
	LoadRenewal(context.Context, string, string) (RenewalSource, error)
	LoadServiceRelation(context.Context, string) (RelationSource, error)
	LoadDomainRelation(context.Context, string) (RelationSource, error)
}

// ActivityLister is the subject activity service surface overview needs.
type ActivityLister interface {
	List(context.Context, activity.ListRequest) (activity.ListResult, error)
}

// SourceBudgets bounds the entire overview and every authority read. A source
// child is constrained by both its own budget and the remaining total budget.
type SourceBudgets struct {
	Total      time.Duration
	Identity   time.Duration
	Monitoring time.Duration
	IPQuality  time.Duration
	Renewal    time.Duration
	Services   time.Duration
	Domains    time.Duration
	Activity   time.Duration
}

func defaultSourceBudgets() SourceBudgets {
	return uniformSourceBudgets(DefaultSectionBudget)
}

func uniformSourceBudgets(budget time.Duration) SourceBudgets {
	return SourceBudgets{
		Total: budget, Identity: budget, Monitoring: budget, IPQuality: budget,
		Renewal: budget, Services: budget, Domains: budget, Activity: budget,
	}
}

func (budgets SourceBudgets) valid() bool {
	return budgets.Total > 0 && budgets.Identity > 0 && budgets.Monitoring > 0 &&
		budgets.IPQuality > 0 && budgets.Renewal > 0 && budgets.Services > 0 &&
		budgets.Domains > 0 && budgets.Activity > 0
}

// Service aggregates section readers into one Overview.
type Service struct {
	sources  SourceReader
	activity ActivityLister
	now      func() time.Time
	budgets  SourceBudgets
}

// NewService wires overview aggregation. Both dependencies are required.
func NewService(sources SourceReader, activityLister ActivityLister) (*Service, error) {
	return NewServiceWithBudgets(
		sources,
		activityLister,
		func() time.Time { return time.Now().UTC() },
		defaultSourceBudgets(),
	)
}

// NewServiceWithClock exists for deterministic compatibility tests. The one
// supplied budget is applied to the total request and every source.
func NewServiceWithClock(
	sources SourceReader,
	activityLister ActivityLister,
	now func() time.Time,
	sectionBudget time.Duration,
) (*Service, error) {
	return NewServiceWithBudgets(sources, activityLister, now, uniformSourceBudgets(sectionBudget))
}

// NewServiceWithBudgets exposes deterministic source budgets to focused tests.
func NewServiceWithBudgets(
	sources SourceReader,
	activityLister ActivityLister,
	now func() time.Time,
	budgets SourceBudgets,
) (*Service, error) {
	if nilOverviewDependency(sources) || nilOverviewDependency(activityLister) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidOverviewRequest)
	}
	if now == nil || !budgets.valid() {
		return nil, fmt.Errorf("%w: budgets", ErrInvalidOverviewRequest)
	}
	return &Service{sources: sources, activity: activityLister, now: now, budgets: budgets}, nil
}

type sourceKind uint8

const (
	sourceMonitoring sourceKind = iota
	sourceIPQuality
	sourceRenewal
	sourceServices
	sourceDomains
	sourceActivity
)

type sourceMessage struct {
	kind       sourceKind
	monitoring MonitoringSource
	ipQuality  IPQualitySource
	renewal    RenewalSource
	relation   RelationSource
	activity   ActivitySection
	err        error
}

type sourceCollection struct {
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
	activity      ActivitySection
	activityErr   error
}

func (collection *sourceCollection) apply(message sourceMessage) {
	switch message.kind {
	case sourceMonitoring:
		collection.monitoring, collection.monitoringErr = message.monitoring, message.err
	case sourceIPQuality:
		collection.ipQuality, collection.ipQualityErr = message.ipQuality, message.err
	case sourceRenewal:
		collection.renewal, collection.renewalErr = message.renewal, message.err
	case sourceServices:
		collection.services, collection.servicesErr = message.relation, message.err
	case sourceDomains:
		collection.domains, collection.domainsErr = message.relation, message.err
	case sourceActivity:
		collection.activity, collection.activityErr = message.activity, message.err
	}
}

func (collection *sourceCollection) timeoutPending(pending map[sourceKind]struct{}) {
	for kind := range pending {
		collection.apply(sourceMessage{kind: kind, err: context.DeadlineExceeded})
	}
}

// Get returns one overview assembled after an identity-first, independently
// bounded concurrent source collection.
func (service *Service) Get(ctx context.Context, request Request) (Overview, error) {
	if ctx == nil || service == nil || nilOverviewDependency(service.sources) ||
		nilOverviewDependency(service.activity) || !service.budgets.valid() {
		return Overview{}, ErrInvalidOverviewRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Overview{}, fmt.Errorf("%w: actor", ErrInvalidOverviewRequest)
	}
	vpsID := request.VPSID
	if vpsID == "" {
		return Overview{}, fmt.Errorf("%w: vps id", ErrInvalidOverviewRequest)
	}

	totalCtx, cancelTotal := context.WithTimeout(ctx, service.budgets.Total)
	defer cancelTotal()
	identityCtx, cancelIdentity := context.WithTimeout(totalCtx, service.budgets.Identity)
	identitySource, err := service.sources.LoadIdentity(identityCtx, vpsID)
	cancelIdentity()
	if ctx.Err() != nil {
		return Overview{}, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, ErrVPSNotFound) {
			return Overview{}, ErrVPSNotFound
		}
		return Overview{}, err
	}

	results := make(chan sourceMessage, 6)
	service.launchSources(totalCtx, actor, vpsID, identitySource.Identity.RenewalDecision, results)
	pending := map[sourceKind]struct{}{
		sourceMonitoring: {}, sourceIPQuality: {}, sourceRenewal: {},
		sourceServices: {}, sourceDomains: {}, sourceActivity: {},
	}
	var collected sourceCollection
	for len(pending) > 0 {
		select {
		case <-ctx.Done():
			return Overview{}, ctx.Err()
		case message := <-results:
			if _, waiting := pending[message.kind]; !waiting {
				continue
			}
			delete(pending, message.kind)
			collected.apply(message)
		case <-totalCtx.Done():
			if ctx.Err() != nil {
				return Overview{}, ctx.Err()
			}
			// Preserve every result already published before marking only the
			// genuinely pending sources unavailable.
			draining := true
			for draining {
				select {
				case message := <-results:
					if _, waiting := pending[message.kind]; waiting {
						delete(pending, message.kind)
						collected.apply(message)
					}
				default:
					draining = false
				}
			}
			collected.timeoutPending(pending)
			clear(pending)
		}
	}
	if ctx.Err() != nil {
		return Overview{}, ctx.Err()
	}

	return service.buildOverview(identitySource, collected), nil
}

func (service *Service) launchSources(
	totalCtx context.Context,
	actor recordauth.ActorScope,
	vpsID string,
	renewalDecision string,
	results chan<- sourceMessage,
) {
	go func() {
		child, cancel := context.WithTimeout(totalCtx, service.budgets.Monitoring)
		defer cancel()
		value, err := service.sources.LoadMonitoring(child, vpsID)
		results <- sourceMessage{kind: sourceMonitoring, monitoring: value, err: err}
	}()
	go func() {
		child, cancel := context.WithTimeout(totalCtx, service.budgets.IPQuality)
		defer cancel()
		value, err := service.sources.LoadIPQuality(child, vpsID)
		results <- sourceMessage{kind: sourceIPQuality, ipQuality: value, err: err}
	}()
	go func() {
		child, cancel := context.WithTimeout(totalCtx, service.budgets.Renewal)
		defer cancel()
		value, err := service.sources.LoadRenewal(child, vpsID, renewalDecision)
		results <- sourceMessage{kind: sourceRenewal, renewal: value, err: err}
	}()
	go func() {
		child, cancel := context.WithTimeout(totalCtx, service.budgets.Services)
		defer cancel()
		value, err := service.sources.LoadServiceRelation(child, vpsID)
		results <- sourceMessage{kind: sourceServices, relation: value, err: err}
	}()
	go func() {
		child, cancel := context.WithTimeout(totalCtx, service.budgets.Domains)
		defer cancel()
		value, err := service.sources.LoadDomainRelation(child, vpsID)
		results <- sourceMessage{kind: sourceDomains, relation: value, err: err}
	}()
	go func() {
		child, cancel := context.WithTimeout(totalCtx, service.budgets.Activity)
		defer cancel()
		value, err := service.loadRecentActivity(child, actor, vpsID)
		results <- sourceMessage{kind: sourceActivity, activity: value, err: err}
	}()
}

func (service *Service) buildOverview(identitySource IdentitySource, collected sourceCollection) Overview {
	identity := identitySource.Identity
	if identity.Labels == nil {
		identity.Labels = []string{}
	}
	facts := identitySource.Facts
	if facts == nil {
		facts = []Fact{}
	}

	monitoring := collected.monitoring
	monitoring.Section = successfulOrUnavailableSection(
		monitoring.Section, collected.monitoringErr, sourceMonitoring,
	)
	ipQuality := collected.ipQuality
	ipQuality.Section = successfulOrUnavailableSection(
		ipQuality.Section, collected.ipQualityErr, sourceIPQuality,
	)
	renewal := collected.renewal
	renewal.Section = successfulOrUnavailableSection(
		renewal.Section, collected.renewalErr, sourceRenewal,
	)
	services := collected.services
	services.Section = successfulOrUnavailableSection(
		services.Section, collected.servicesErr, sourceServices,
	)
	domains := collected.domains
	domains.Section = successfulOrUnavailableSection(
		domains.Section, collected.domainsErr, sourceDomains,
	)
	activitySection := collected.activity
	if collected.activityErr != nil {
		activitySection = ActivitySection{
			Section: successfulOrUnavailableSection(SectionState{}, collected.activityErr, sourceActivity),
			Items:   []activity.Event{},
		}
	}
	if activitySection.Section.State == "" {
		activitySection.Section.State = SectionReady
	}
	if activitySection.Items == nil {
		activitySection.Items = []activity.Event{}
	}

	generatedAt := service.now().UTC()
	// Sanitize judgement-source timestamps before anomaly derivation so a
	// rejected future observation cannot escape through anomaly event_at.
	monitoring.Section = normalizeSectionFreshness(monitoring.Section, generatedAt)
	ipQuality.Section = normalizeSectionFreshness(ipQuality.Section, generatedAt)
	renewal.Section = normalizeSectionFreshness(renewal.Section, generatedAt)
	overallSection := SectionState{
		State: SectionReady, ObservedAt: timePointer(generatedAt), LastSuccessAt: timePointer(generatedAt),
	}
	relations := []RelationSummary{
		{
			Kind: "monitoring_instances", Label: "监控实例",
			Count: monitoring.Count, Status: relationStatus(collected.monitoringErr, monitoring.Health, monitoring.Status),
			Section: monitoring.Section,
		},
		{
			Kind: "subscriptions", Label: "订阅", Route: "/subscriptions?vps_id=" + url.QueryEscape(identity.VPSID),
			Count: renewal.ActiveSubscriptions, Status: relationStatus(collected.renewalErr, renewal.Status),
			Section: renewal.Section,
		},
		{
			Kind: "services", Label: "服务",
			Count: services.Count, Status: relationStatus(collected.servicesErr), Section: services.Section,
		},
		{
			Kind: "domains", Label: "域名",
			Count: domains.Count, Status: relationStatus(collected.domainsErr), Section: domains.Section,
		},
	}

	snapshot := snapshotFromSources(generatedAt, identity, monitoring, ipQuality, renewal)
	if activitySection.Section.State == SectionUnavailable {
		snapshot.JudgementSourcesUnavailable = appendUnique(snapshot.JudgementSourcesUnavailable, "activity")
	}
	anomalies := EvaluateAnomalies(snapshot)
	overview := Overview{
		GeneratedAt: generatedAt,
		Identity:    identity,
		Anomalies:   anomalies,
		Summary: Summary{
			Overall: SummaryCell{
				Status: overallStatus(anomalies, identity.LifecycleStatus), Section: overallSection,
			},
			Monitoring: SummaryCell{
				Status: firstNonEmpty(monitoring.Health, monitoring.Status, "unknown"),
				Detail: monitoring.Detail, Section: monitoring.Section,
			},
			IPQuality: SummaryCell{
				Status: firstNonEmpty(ipQuality.RiskLevel, ipQuality.Status, "unknown"),
				Detail: ipQuality.Status, Section: ipQuality.Section,
			},
			Renewal: SummaryCell{
				Status:  firstNonEmpty(renewal.Status, identity.RenewalDecision, "unknown"),
				Section: renewal.Section,
			},
		},
		RecentActivity: activitySection,
		Facts:          facts,
		Relations:      relations,
		Capabilities:   []string{CapabilityRecordsV2Read},
	}
	normalizeOverviewFreshness(&overview)
	return overview
}

func (service *Service) loadRecentActivity(
	ctx context.Context,
	actor recordauth.ActorScope,
	vpsID string,
) (ActivitySection, error) {
	result, err := service.activity.List(ctx, activity.ListRequest{
		Actor: actor,
		Query: activity.Query{
			Subject: SubjectRef(vpsID),
			View:    activity.ViewActivity,
			Limit:   RecentActivityLimit,
		},
	})
	if err != nil {
		if errors.Is(err, activity.ErrSubjectNotFound) {
			return ActivitySection{
				Section: SectionState{State: SectionReady},
				Items:   []activity.Event{},
			}, nil
		}
		return ActivitySection{}, err
	}
	items := result.Items
	if items == nil {
		items = []activity.Event{}
	}
	if len(items) > RecentActivityLimit {
		items = items[:RecentActivityLimit]
	}
	section := SectionState{
		State:      firstNonEmpty(result.Freshness.State, SectionReady),
		ObservedAt: result.Freshness.VisibleObservedAt,
		ReasonCode: result.Freshness.ReasonCode,
	}
	if result.Freshness.VisibleObservedAt != nil {
		observed := result.Freshness.VisibleObservedAt.UTC()
		section.LastSuccessAt = &observed
	}
	return ActivitySection{
		Section:  section,
		Items:    items,
		Snapshot: result.SnapshotCursor,
	}, nil
}

func successfulOrUnavailableSection(section SectionState, err error, kind sourceKind) SectionState {
	if err == nil {
		if section.State == "" {
			section.State = SectionReady
		}
		return section
	}
	return SectionState{State: SectionUnavailable, ReasonCode: reasonForSourceError(kind, err)}
}

func reasonForSourceError(kind sourceKind, err error) string {
	timedOut := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
	switch kind {
	case sourceMonitoring:
		if timedOut {
			return "monitoring_timeout"
		}
		return "monitoring_unavailable"
	case sourceIPQuality:
		if timedOut {
			return "ip_quality_timeout"
		}
		return "ip_quality_unavailable"
	case sourceRenewal:
		if timedOut {
			return "subscription_timeout"
		}
		return "subscription_unavailable"
	case sourceServices, sourceDomains:
		if timedOut {
			return "relation_timeout"
		}
		return "relation_unavailable"
	case sourceActivity:
		if timedOut {
			return "activity_timeout"
		}
		if errors.Is(err, activity.ErrProjectionUnavailable) {
			return "activity_projection_unavailable"
		}
		return "activity_unavailable"
	default:
		return "section_unavailable"
	}
}

func relationStatus(err error, values ...string) string {
	if err != nil {
		return "unavailable"
	}
	return firstNonEmpty(values...)
}

func normalizeOverviewFreshness(overview *Overview) {
	generatedAt := overview.GeneratedAt.UTC()
	overview.GeneratedAt = generatedAt
	overview.Summary.Overall.Section = normalizeSectionFreshness(overview.Summary.Overall.Section, generatedAt)
	overview.Summary.Monitoring.Section = normalizeSectionFreshness(overview.Summary.Monitoring.Section, generatedAt)
	overview.Summary.IPQuality.Section = normalizeSectionFreshness(overview.Summary.IPQuality.Section, generatedAt)
	overview.Summary.Renewal.Section = normalizeSectionFreshness(overview.Summary.Renewal.Section, generatedAt)
	overview.RecentActivity.Section = normalizeSectionFreshness(overview.RecentActivity.Section, generatedAt)
	for index := range overview.Relations {
		overview.Relations[index].Section = normalizeSectionFreshness(overview.Relations[index].Section, generatedAt)
	}
}

func normalizeSectionFreshness(section SectionState, generatedAt time.Time) SectionState {
	invalid := false
	section.ObservedAt, invalid = normalizedSourceTime(section.ObservedAt, generatedAt)
	var lastInvalid bool
	section.LastSuccessAt, lastInvalid = normalizedSourceTime(section.LastSuccessAt, generatedAt)
	invalid = invalid || lastInvalid
	if invalid && section.State != SectionUnavailable {
		section.State = SectionStale
		section.ReasonCode = "source_timestamp_invalid"
	}
	return section
}

func normalizedSourceTime(value *time.Time, generatedAt time.Time) (*time.Time, bool) {
	if value == nil {
		return nil, false
	}
	normalized := value.UTC()
	if normalized.After(generatedAt) {
		return nil, true
	}
	return &normalized, false
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func snapshotFromSources(
	generatedAt time.Time,
	identity Identity,
	monitoring MonitoringSource,
	ipQuality IPQualitySource,
	renewal RenewalSource,
) Snapshot {
	snapshot := Snapshot{
		GeneratedAt:         generatedAt,
		VPSID:               identity.VPSID,
		Identity:            identity,
		LifecycleStatus:     identity.LifecycleStatus,
		RenewalDecision:     identity.RenewalDecision,
		MonitoringHealth:    monitoring.Health,
		MonitoringDetail:    monitoring.Detail,
		ActiveIncidents:     monitoring.ActiveIncidents,
		MonitoringObserved:  monitoring.Section.ObservedAt,
		IPStatus:            ipQuality.Status,
		IPRiskLevel:         ipQuality.RiskLevel,
		IPStale:             ipQuality.Stale,
		IPObservedAt:        ipQuality.Section.ObservedAt,
		ActiveSubscriptions: renewal.ActiveSubscriptions,
		NextRenewAt:         renewal.NextRenewAt,
	}
	snapshot.MonitoringAvailable = monitoring.Section.State != SectionUnavailable
	snapshot.IPAvailable = ipQuality.Section.State != SectionUnavailable
	snapshot.SubscriptionAvailable = renewal.Section.State != SectionUnavailable

	if !snapshot.MonitoringAvailable {
		snapshot.JudgementSourcesUnavailable = append(snapshot.JudgementSourcesUnavailable, "monitoring")
	}
	if !snapshot.IPAvailable {
		snapshot.JudgementSourcesUnavailable = append(snapshot.JudgementSourcesUnavailable, "ip_quality")
	}
	if !snapshot.SubscriptionAvailable {
		snapshot.JudgementSourcesUnavailable = append(snapshot.JudgementSourcesUnavailable, "renewal")
	}
	return snapshot
}

func overallStatus(anomalies []Anomaly, lifecycle string) string {
	for _, anomaly := range anomalies {
		if anomaly.Severity == SeverityCritical {
			return "critical"
		}
	}
	for _, anomaly := range anomalies {
		if anomaly.Severity == SeverityWarning {
			return "attention"
		}
	}
	if lifecycle == "to_cancel" || lifecycle == "to_migrate" {
		return "attention"
	}
	if len(anomalies) > 0 {
		return "notice"
	}
	return "healthy"
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func nilOverviewDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

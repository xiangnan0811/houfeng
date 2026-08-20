package vpsoverview

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
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

// SourceBundle is everything except recent activity. The store owns how these
// facts are loaded; the service owns timeouts, anomalies, and activity.
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

// SourceReader loads non-activity overview facts for one VPS.
type SourceReader interface {
	LoadSources(context.Context, string) (SourceBundle, error)
}

// ActivityLister is the subject activity service surface overview needs.
type ActivityLister interface {
	List(context.Context, activity.ListRequest) (activity.ListResult, error)
}

// Service aggregates section readers into one Overview.
type Service struct {
	sources        SourceReader
	activity       ActivityLister
	now            func() time.Time
	sectionBudget  time.Duration
	activityBudget time.Duration
}

// NewService wires overview aggregation. Both dependencies are required.
func NewService(sources SourceReader, activityLister ActivityLister) (*Service, error) {
	if nilOverviewDependency(sources) || nilOverviewDependency(activityLister) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidOverviewRequest)
	}
	return &Service{
		sources:        sources,
		activity:       activityLister,
		now:            func() time.Time { return time.Now().UTC() },
		sectionBudget:  DefaultSectionBudget,
		activityBudget: DefaultSectionBudget,
	}, nil
}

// NewServiceWithClock exists for deterministic tests.
func NewServiceWithClock(
	sources SourceReader,
	activityLister ActivityLister,
	now func() time.Time,
	sectionBudget time.Duration,
) (*Service, error) {
	service, err := NewService(sources, activityLister)
	if err != nil {
		return nil, err
	}
	if now == nil || sectionBudget <= 0 {
		return nil, fmt.Errorf("%w: clock", ErrInvalidOverviewRequest)
	}
	service.now = now
	service.sectionBudget = sectionBudget
	service.activityBudget = sectionBudget
	return service, nil
}

// Get returns one overview at a uniform generated_at.
func (service *Service) Get(ctx context.Context, request Request) (Overview, error) {
	if ctx == nil || service == nil || nilOverviewDependency(service.sources) ||
		nilOverviewDependency(service.activity) {
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

	generatedAt := service.now().UTC()

	type sourceResult struct {
		bundle SourceBundle
		err    error
	}
	type activityResult struct {
		section ActivitySection
		err     error
	}

	var (
		sourcesOut  sourceResult
		activityOut activityResult
		wait        sync.WaitGroup
	)
	wait.Add(2)

	go func() {
		defer wait.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, service.sectionBudget)
		defer cancel()
		bundle, err := service.sources.LoadSources(sectionCtx, vpsID)
		sourcesOut = sourceResult{bundle: bundle, err: err}
	}()

	go func() {
		defer wait.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, service.activityBudget)
		defer cancel()
		activityOut.section, activityOut.err = service.loadRecentActivity(sectionCtx, actor, vpsID)
	}()

	wait.Wait()

	if sourcesOut.err != nil {
		if errors.Is(sourcesOut.err, ErrVPSNotFound) {
			return Overview{}, ErrVPSNotFound
		}
		return Overview{}, sourcesOut.err
	}
	bundle := sourcesOut.bundle
	if bundle.Identity.Labels == nil {
		bundle.Identity.Labels = []string{}
	}
	if bundle.Facts == nil {
		bundle.Facts = []Fact{}
	}
	if bundle.Relations == nil {
		bundle.Relations = []RelationSummary{}
	}

	activitySection := activityOut.section
	if activityOut.err != nil {
		activitySection = ActivitySection{
			Section: SectionState{
				State:      SectionUnavailable,
				ReasonCode: reasonForError(activityOut.err),
			},
			Items: []activity.Event{},
		}
	}
	if activitySection.Items == nil {
		activitySection.Items = []activity.Event{}
	}

	snapshot := snapshotFromBundle(generatedAt, bundle)
	if activitySection.Section.State == SectionUnavailable {
		snapshot.JudgementSourcesUnavailable = appendUnique(
			snapshot.JudgementSourcesUnavailable, "activity",
		)
	}
	anomalies := EvaluateAnomalies(snapshot)

	overview := Overview{
		GeneratedAt: generatedAt,
		Identity:    bundle.Identity,
		Anomalies:   anomalies,
		Summary: Summary{
			Overall: SummaryCell{
				Status:  overallStatus(anomalies, bundle.Identity.LifecycleStatus),
				Section: SectionState{State: SectionReady, LastSuccessAt: &generatedAt},
			},
			Monitoring: SummaryCell{
				Status:  firstNonEmpty(bundle.MonitoringHealth, bundle.MonitoringStatus, "unknown"),
				Detail:  bundle.MonitoringDetail,
				Section: bundle.MonitoringSection,
			},
			IPQuality: SummaryCell{
				Status:  firstNonEmpty(bundle.IPRiskLevel, bundle.IPStatus, "unknown"),
				Detail:  bundle.IPStatus,
				Section: bundle.IPSection,
			},
			Renewal: SummaryCell{
				Status:  firstNonEmpty(bundle.RenewalStatus, bundle.Identity.RenewalDecision, "unknown"),
				Section: bundle.RenewalSection,
			},
		},
		RecentActivity: activitySection,
		Facts:          bundle.Facts,
		Relations:      bundle.Relations,
		Capabilities:   []string{CapabilityRecordsV2Read},
	}
	return overview, nil
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
			// Subject timeline absence with a live VPS identity is an empty
			// activity section, not a fatal overview miss.
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

func snapshotFromBundle(generatedAt time.Time, bundle SourceBundle) Snapshot {
	snapshot := Snapshot{
		GeneratedAt:         generatedAt,
		VPSID:               bundle.Identity.VPSID,
		Identity:            bundle.Identity,
		LifecycleStatus:     bundle.Identity.LifecycleStatus,
		RenewalDecision:     bundle.Identity.RenewalDecision,
		MonitoringHealth:    bundle.MonitoringHealth,
		MonitoringDetail:    bundle.MonitoringDetail,
		ActiveIncidents:     bundle.ActiveIncidents,
		MonitoringObserved:  bundle.MonitoringSection.ObservedAt,
		IPStatus:            bundle.IPStatus,
		IPRiskLevel:         bundle.IPRiskLevel,
		IPStale:             bundle.IPStale,
		IPObservedAt:        bundle.IPSection.ObservedAt,
		ActiveSubscriptions: bundle.ActiveSubscriptions,
		NextRenewAt:         bundle.NextRenewAt,
	}
	snapshot.MonitoringAvailable = bundle.MonitoringSection.State != SectionUnavailable
	snapshot.IPAvailable = bundle.IPSection.State != SectionUnavailable
	snapshot.SubscriptionAvailable = bundle.RenewalSection.State != SectionUnavailable

	if bundle.MonitoringSection.State == SectionUnavailable {
		snapshot.JudgementSourcesUnavailable = append(snapshot.JudgementSourcesUnavailable, "monitoring")
	}
	if bundle.IPSection.State == SectionUnavailable {
		snapshot.JudgementSourcesUnavailable = append(snapshot.JudgementSourcesUnavailable, "ip_quality")
	}
	if bundle.RenewalSection.State == SectionUnavailable {
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

func reasonForError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "section_timeout"
	case errors.Is(err, activity.ErrProjectionUnavailable):
		return "activity_projection_unavailable"
	default:
		return "section_unavailable"
	}
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

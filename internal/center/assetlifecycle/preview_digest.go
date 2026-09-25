package assetlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"houfeng/internal/center/subscriptions"
)

type cancellationPreviewDigestProjection struct {
	VPS                 cancellationPreviewVPSDigest            `json:"vps"`
	Subscriptions       []cancellationPreviewSubscriptionDigest `json:"subscriptions"`
	MonitoringInstances []cancellationPreviewMonitoringDigest   `json:"monitoring_instances"`
	DependencyImpacts   []cancellationPreviewDependencyDigest   `json:"dependency_impacts"`
	Services            []cancellationPreviewServiceDigest      `json:"services"`
	Domains             []cancellationPreviewDomainDigest       `json:"domains"`
	Targets             []cancellationPreviewTargetDigest       `json:"targets"`
	RecommendedSteps    []cancellationPreviewStepDigest         `json:"recommended_steps"`
	EvaluatedOn         string                                  `json:"evaluated_on"`
}

type cancellationPreviewVPSDigest struct {
	ID              string  `json:"id"`
	LifecycleStatus string  `json:"lifecycle_status"`
	UsageStatus     string  `json:"usage_status"`
	RenewalDecision string  `json:"renewal_decision"`
	ArchivedAt      *string `json:"archived_at"`
}

type cancellationPreviewSubscriptionDigest struct {
	ID                 string  `json:"id"`
	VPSID              string  `json:"vps_id"`
	StartedAt          *string `json:"started_at"`
	EndsAt             *string `json:"ends_at"`
	RenewAt            *string `json:"renew_at"`
	TrialEndsAt        *string `json:"trial_ends_at"`
	Status             string  `json:"status"`
	RenewalMode        string  `json:"renewal_mode"`
	AutoRenew          bool    `json:"auto_renew"`
	AutoRenewCancelled bool    `json:"auto_renew_cancelled"`
}

type cancellationPreviewMonitoringDigest struct {
	ID               string  `json:"id"`
	LifecycleStatus  string  `json:"lifecycle_status"`
	MonitoringStatus string  `json:"monitoring_status"`
	BindingStatus    string  `json:"binding_status"`
	ArchivedAt       *string `json:"archived_at"`
}

type cancellationPreviewDependencyDigest struct {
	ObjectType         string `json:"object_type"`
	ObjectID           string `json:"object_id"`
	VPSID              string `json:"vps_id"`
	VPSLifecycleStatus string `json:"vps_lifecycle_status"`
	RelationType       string `json:"relation_type"`
	RelationID         string `json:"relation_id"`
	RelationStatus     string `json:"relation_status"`
	Classification     string `json:"classification"`
}

type cancellationPreviewServiceDigest struct {
	ID       string  `json:"id"`
	VPSID    string  `json:"vps_id"`
	TargetID *string `json:"target_id"`
	Status   string  `json:"status"`
}

type cancellationPreviewDomainDigest struct {
	ID        string  `json:"id"`
	VPSID     string  `json:"vps_id"`
	ServiceID *string `json:"service_id"`
	TargetID  *string `json:"target_id"`
	Status    string  `json:"status"`
}

type cancellationPreviewTargetDigest struct {
	ID         string   `json:"id"`
	State      string   `json:"state"`
	ServiceIDs []string `json:"service_ids"`
	DomainIDs  []string `json:"domain_ids"`
}

type cancellationPreviewStepDigest struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	StepType   string `json:"step_type"`
	FromState  string `json:"from_state"`
	ToState    string `json:"to_state"`
	Required   bool   `json:"required"`
}

func DigestCancellationPreview(preview CancellationPreview) string {
	projection := cancellationPreviewDigestProjection{
		VPS: cancellationPreviewVPSDigest{
			ID:              preview.VPS.VPSID,
			LifecycleStatus: string(preview.VPS.LifecycleStatus),
			UsageStatus:     string(preview.VPS.UsageStatus),
			RenewalDecision: string(preview.VPS.RenewalDecision),
			ArchivedAt:      timeToken(preview.VPS.ArchivedAt),
		},
		Subscriptions:       make([]cancellationPreviewSubscriptionDigest, 0, len(preview.Subscriptions)),
		MonitoringInstances: make([]cancellationPreviewMonitoringDigest, 0, len(preview.MonitoringInstanceLinks)),
		DependencyImpacts:   make([]cancellationPreviewDependencyDigest, 0, len(preview.DependencyImpacts)),
		Services:            make([]cancellationPreviewServiceDigest, 0, len(preview.Services)),
		Domains:             make([]cancellationPreviewDomainDigest, 0, len(preview.Domains)),
		Targets:             make([]cancellationPreviewTargetDigest, 0, len(preview.TargetLinks)),
		RecommendedSteps:    make([]cancellationPreviewStepDigest, 0, len(preview.RecommendedSteps)),
		EvaluatedOn:         preview.EvaluatedOn.Time.UTC().Format(subscriptions.DateLayout),
	}

	for _, subscription := range preview.Subscriptions {
		record := subscription.Record
		projection.Subscriptions = append(projection.Subscriptions, cancellationPreviewSubscriptionDigest{
			ID:                 record.SubscriptionID,
			VPSID:              record.VPSID,
			StartedAt:          subscriptionDateToken(record.StartedAt),
			EndsAt:             subscriptionDateToken(record.EndsAt),
			RenewAt:            subscriptionDateToken(record.RenewAt),
			TrialEndsAt:        subscriptionDateToken(record.TrialEndsAt),
			Status:             string(record.Status),
			RenewalMode:        record.RenewalMode,
			AutoRenew:          record.AutoRenew,
			AutoRenewCancelled: record.AutoRenewCancelled,
		})
	}
	for _, monitoringInstance := range preview.MonitoringInstanceLinks {
		projection.MonitoringInstances = append(projection.MonitoringInstances, cancellationPreviewMonitoringDigest{
			ID:               monitoringInstance.MonitoringInstanceID,
			LifecycleStatus:  monitoringInstance.LifecycleStatus,
			MonitoringStatus: monitoringInstance.MonitoringStatus,
			BindingStatus:    monitoringInstance.BindingStatus,
			ArchivedAt:       timeToken(monitoringInstance.ArchivedAt),
		})
	}
	for _, impact := range preview.DependencyImpacts {
		projection.DependencyImpacts = append(projection.DependencyImpacts, cancellationPreviewDependencyDigest{
			ObjectType:         impact.ObjectType,
			ObjectID:           impact.ObjectID,
			VPSID:              impact.VPSID,
			VPSLifecycleStatus: impact.VPSLifecycleStatus,
			RelationType:       impact.RelationType,
			RelationID:         impact.RelationID,
			RelationStatus:     impact.RelationStatus,
			Classification:     impact.Classification,
		})
	}
	for _, service := range preview.Services {
		projection.Services = append(projection.Services, cancellationPreviewServiceDigest{
			ID: service.ServiceID, VPSID: service.VPSID,
			TargetID: optionalStringToken(service.TargetID), Status: string(service.Status),
		})
	}
	for _, domain := range preview.Domains {
		projection.Domains = append(projection.Domains, cancellationPreviewDomainDigest{
			ID: domain.DomainID, VPSID: domain.VPSID,
			ServiceID: optionalStringToken(domain.ServiceID), TargetID: optionalStringToken(domain.TargetID),
			Status: string(domain.Status),
		})
	}
	for _, target := range preview.TargetLinks {
		projection.Targets = append(projection.Targets, cancellationPreviewTargetDigest{
			ID: target.TargetID, State: target.RunStatus,
			ServiceIDs: sortedDigestStrings(target.ServiceIDs),
			DomainIDs:  sortedDigestStrings(target.DomainIDs),
		})
	}
	for _, step := range preview.RecommendedSteps {
		projection.RecommendedSteps = append(projection.RecommendedSteps, cancellationPreviewStepDigest{
			ObjectType: step.ObjectType, ObjectID: step.ObjectID, StepType: step.StepType,
			FromState: step.FromState, ToState: step.ToState, Required: step.Required,
		})
	}

	slices.SortFunc(projection.Subscriptions, compareSubscriptionDigest)
	slices.SortFunc(projection.MonitoringInstances, compareMonitoringDigest)
	slices.SortFunc(projection.DependencyImpacts, compareDependencyDigest)
	slices.SortFunc(projection.Services, compareServiceDigest)
	slices.SortFunc(projection.Domains, compareDomainDigest)
	slices.SortFunc(projection.Targets, compareTargetDigest)
	slices.SortFunc(projection.RecommendedSteps, compareStepDigest)

	encoded, err := json.Marshal(projection)
	if err != nil {
		// Every projected field is JSON-safe scalar data or a slice of strings.
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func subscriptionDateToken(value *subscriptions.Date) *string {
	if value == nil {
		return nil
	}
	token := value.Time.UTC().Format(subscriptions.DateLayout)
	return &token
}

func timeToken(value *time.Time) *string {
	if value == nil {
		return nil
	}
	token := value.UTC().Format(time.RFC3339Nano)
	return &token
}

func optionalStringToken(value *string) *string {
	if value == nil {
		return nil
	}
	token := *value
	return &token
}

func sortedDigestStrings(values []string) []string {
	sorted := make([]string, len(values))
	copy(sorted, values)
	slices.Sort(sorted)
	return sorted
}

func compareSubscriptionDigest(a, b cancellationPreviewSubscriptionDigest) int {
	if order := strings.Compare(a.ID, b.ID); order != 0 {
		return order
	}
	if order := strings.Compare(a.VPSID, b.VPSID); order != 0 {
		return order
	}
	if order := compareOptionalString(a.StartedAt, b.StartedAt); order != 0 {
		return order
	}
	if order := compareOptionalString(a.EndsAt, b.EndsAt); order != 0 {
		return order
	}
	if order := compareOptionalString(a.RenewAt, b.RenewAt); order != 0 {
		return order
	}
	if order := compareOptionalString(a.TrialEndsAt, b.TrialEndsAt); order != 0 {
		return order
	}
	if order := strings.Compare(a.Status, b.Status); order != 0 {
		return order
	}
	if order := strings.Compare(a.RenewalMode, b.RenewalMode); order != 0 {
		return order
	}
	if order := compareBool(a.AutoRenew, b.AutoRenew); order != 0 {
		return order
	}
	return compareBool(a.AutoRenewCancelled, b.AutoRenewCancelled)
}

func compareMonitoringDigest(a, b cancellationPreviewMonitoringDigest) int {
	if order := strings.Compare(a.ID, b.ID); order != 0 {
		return order
	}
	if order := strings.Compare(a.LifecycleStatus, b.LifecycleStatus); order != 0 {
		return order
	}
	if order := strings.Compare(a.MonitoringStatus, b.MonitoringStatus); order != 0 {
		return order
	}
	if order := strings.Compare(a.BindingStatus, b.BindingStatus); order != 0 {
		return order
	}
	return compareOptionalString(a.ArchivedAt, b.ArchivedAt)
}

func compareDependencyDigest(a, b cancellationPreviewDependencyDigest) int {
	if order := strings.Compare(a.ObjectType, b.ObjectType); order != 0 {
		return order
	}
	if order := strings.Compare(a.ObjectID, b.ObjectID); order != 0 {
		return order
	}
	if order := strings.Compare(a.VPSID, b.VPSID); order != 0 {
		return order
	}
	if order := strings.Compare(a.VPSLifecycleStatus, b.VPSLifecycleStatus); order != 0 {
		return order
	}
	if order := strings.Compare(a.RelationType, b.RelationType); order != 0 {
		return order
	}
	if order := strings.Compare(a.RelationID, b.RelationID); order != 0 {
		return order
	}
	if order := strings.Compare(a.RelationStatus, b.RelationStatus); order != 0 {
		return order
	}
	return strings.Compare(a.Classification, b.Classification)
}

func compareServiceDigest(a, b cancellationPreviewServiceDigest) int {
	if order := strings.Compare(a.ID, b.ID); order != 0 {
		return order
	}
	if order := strings.Compare(a.VPSID, b.VPSID); order != 0 {
		return order
	}
	if order := compareOptionalString(a.TargetID, b.TargetID); order != 0 {
		return order
	}
	return strings.Compare(a.Status, b.Status)
}

func compareDomainDigest(a, b cancellationPreviewDomainDigest) int {
	if order := strings.Compare(a.ID, b.ID); order != 0 {
		return order
	}
	if order := strings.Compare(a.VPSID, b.VPSID); order != 0 {
		return order
	}
	if order := compareOptionalString(a.ServiceID, b.ServiceID); order != 0 {
		return order
	}
	if order := compareOptionalString(a.TargetID, b.TargetID); order != 0 {
		return order
	}
	return strings.Compare(a.Status, b.Status)
}

func compareTargetDigest(a, b cancellationPreviewTargetDigest) int {
	if order := strings.Compare(a.ID, b.ID); order != 0 {
		return order
	}
	if order := strings.Compare(a.State, b.State); order != 0 {
		return order
	}
	if order := compareStringSlices(a.ServiceIDs, b.ServiceIDs); order != 0 {
		return order
	}
	return compareStringSlices(a.DomainIDs, b.DomainIDs)
}

func compareStepDigest(a, b cancellationPreviewStepDigest) int {
	if order := strings.Compare(a.ObjectType, b.ObjectType); order != 0 {
		return order
	}
	if order := strings.Compare(a.ObjectID, b.ObjectID); order != 0 {
		return order
	}
	if order := strings.Compare(a.StepType, b.StepType); order != 0 {
		return order
	}
	if order := strings.Compare(a.FromState, b.FromState); order != 0 {
		return order
	}
	if order := strings.Compare(a.ToState, b.ToState); order != 0 {
		return order
	}
	return compareBool(a.Required, b.Required)
}

func compareOptionalString(a, b *string) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	return strings.Compare(*a, *b)
}

func compareStringSlices(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if order := strings.Compare(a[i], b[i]); order != 0 {
			return order
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func compareBool(a, b bool) int {
	if a == b {
		return 0
	}
	if !a {
		return -1
	}
	return 1
}

func AttachCancellationPreviewDigest(preview *CancellationPreview) {
	preview.PreviewDigest = DigestCancellationPreview(*preview)
}

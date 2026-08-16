package evidence

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"

	"houfeng/internal/center/recordauth"
)

const (
	DefaultProjectEvidenceCapacityBytes = uint64(10 * 1024 * 1024 * 1024)
	DefaultEvidenceWarningPercent       = uint8(80)
)

var (
	ErrInvalidCapacityPolicy = errors.New("invalid evidence capacity policy")
	ErrCapacityArithmetic    = errors.New("invalid evidence capacity arithmetic")
	ErrCapacityUnavailable   = errors.New("evidence capacity unavailable")
)

// CapacityPolicy is evidence-owned and deliberately independent of attachment
// storage. The policy is an explicit composition input rather than a database
// setting, so Task 7 needs no mutable schema or migration.
type CapacityPolicy struct {
	ProjectLimitBytes uint64
	WarningPercent    uint8
}

func DefaultCapacityPolicy() CapacityPolicy {
	return CapacityPolicy{
		ProjectLimitBytes: DefaultProjectEvidenceCapacityBytes,
		WarningPercent:    DefaultEvidenceWarningPercent,
	}
}

func (policy CapacityPolicy) Validate() error {
	if policy.ProjectLimitBytes == 0 || policy.ProjectLimitBytes > math.MaxInt64 ||
		policy.WarningPercent == 0 || policy.WarningPercent > 100 {
		return ErrInvalidCapacityPolicy
	}
	return nil
}

func (policy CapacityPolicy) WarningThresholdBytes() (uint64, error) {
	if err := policy.Validate(); err != nil {
		return 0, err
	}
	percent := uint64(policy.WarningPercent)
	threshold := (policy.ProjectLimitBytes/100)*percent +
		((policy.ProjectLimitBytes%100)*percent+99)/100
	if threshold == 0 || threshold > policy.ProjectLimitBytes {
		return 0, ErrInvalidCapacityPolicy
	}
	return threshold, nil
}

type CapacityEvaluation struct {
	UsedBytes             uint64
	AdditionalBytes       uint64
	ProjectedBytes        uint64
	LimitBytes            uint64
	WarningThresholdBytes uint64
	Outcome               QuotaOutcome
}

func (policy CapacityPolicy) Evaluate(usedBytes uint64, additionalBytes uint64) (CapacityEvaluation, error) {
	warningThreshold, err := policy.WarningThresholdBytes()
	if err != nil {
		return CapacityEvaluation{}, err
	}
	if additionalBytes > math.MaxUint64-usedBytes {
		return CapacityEvaluation{}, ErrCapacityArithmetic
	}
	projectedBytes := usedBytes + additionalBytes
	evaluation := CapacityEvaluation{
		UsedBytes: usedBytes, AdditionalBytes: additionalBytes, ProjectedBytes: projectedBytes,
		LimitBytes: policy.ProjectLimitBytes, WarningThresholdBytes: warningThreshold,
	}
	switch {
	case projectedBytes > policy.ProjectLimitBytes:
		evaluation.Outcome = QuotaOutcome{Status: QuotaExceeded, Reason: "project evidence quota exceeded"}
	case projectedBytes >= warningThreshold:
		evaluation.Outcome = QuotaOutcome{Status: QuotaWarning, Reason: "project evidence quota warning threshold reached"}
	default:
		evaluation.Outcome = QuotaOutcome{Status: QuotaAllowed}
	}
	return evaluation, nil
}

// ProjectCapacityUsage separates logical snapshot accounting from physical
// content-addressed payload storage and from global orphan storage.
type ProjectCapacityUsage struct {
	ProjectID               string
	LogicalSnapshotCount    uint64
	LogicalSnapshotBytes    uint64
	PhysicalPayloadCount    uint64
	PhysicalCanonicalBytes  uint64
	PhysicalCompressedBytes uint64
	OrphanPayloadCount      uint64
	OrphanCanonicalBytes    uint64
	OrphanCompressedBytes   uint64
}

func (usage ProjectCapacityUsage) Validate(projectID string) error {
	if !validCapacityProjectID(projectID) || usage.ProjectID != projectID ||
		(usage.LogicalSnapshotCount == 0) != (usage.LogicalSnapshotBytes == 0) ||
		(usage.LogicalSnapshotCount == 0) != (usage.PhysicalPayloadCount == 0) ||
		(usage.PhysicalPayloadCount == 0) != (usage.PhysicalCanonicalBytes == 0) ||
		(usage.PhysicalPayloadCount == 0) != (usage.PhysicalCompressedBytes == 0) ||
		usage.PhysicalPayloadCount > usage.LogicalSnapshotCount ||
		usage.PhysicalCanonicalBytes > usage.LogicalSnapshotBytes ||
		(usage.OrphanPayloadCount == 0) != (usage.OrphanCanonicalBytes == 0) ||
		(usage.OrphanPayloadCount == 0) != (usage.OrphanCompressedBytes == 0) ||
		usage.LogicalSnapshotBytes > math.MaxInt64 || usage.PhysicalCanonicalBytes > math.MaxInt64 ||
		usage.PhysicalCompressedBytes > math.MaxInt64 || usage.OrphanCanonicalBytes > math.MaxInt64 ||
		usage.OrphanCompressedBytes > math.MaxInt64 {
		return fmt.Errorf("%w: project usage", ErrCapacityUnavailable)
	}
	return nil
}

type ProjectCapacitySource interface {
	ReadProjectEvidenceCapacity(ctx context.Context, projectID string) (ProjectCapacityUsage, error)
}

type CapacityEnforcer struct {
	policy CapacityPolicy
	source ProjectCapacitySource
}

func NewCapacityEnforcer(policy CapacityPolicy, source ProjectCapacitySource) (*CapacityEnforcer, error) {
	if policy.Validate() != nil || nilCapacityDependency(source) {
		return nil, ErrCapacityUnavailable
	}
	return &CapacityEnforcer{policy: policy, source: source}, nil
}

func (enforcer *CapacityEnforcer) Policy() CapacityPolicy {
	if enforcer == nil {
		return CapacityPolicy{}
	}
	return enforcer.policy
}

func (enforcer *CapacityEnforcer) Evaluate(
	ctx context.Context,
	projectID string,
	additionalBytes uint64,
) (CapacityEvaluation, error) {
	if ctx == nil || enforcer == nil || enforcer.policy.Validate() != nil ||
		nilCapacityDependency(enforcer.source) || !validCapacityProjectID(projectID) ||
		additionalBytes > math.MaxInt64 {
		return CapacityEvaluation{}, ErrCapacityUnavailable
	}
	usage, err := enforcer.source.ReadProjectEvidenceCapacity(ctx, projectID)
	if err != nil || usage.Validate(projectID) != nil {
		warningThreshold, thresholdErr := enforcer.policy.WarningThresholdBytes()
		if thresholdErr != nil {
			return CapacityEvaluation{}, ErrCapacityUnavailable
		}
		return CapacityEvaluation{
			AdditionalBytes: additionalBytes, LimitBytes: enforcer.policy.ProjectLimitBytes,
			WarningThresholdBytes: warningThreshold,
			Outcome:               QuotaOutcome{Status: QuotaUnavailable, Reason: "project evidence capacity unavailable"},
		}, nil
	}
	return enforcer.policy.Evaluate(usage.LogicalSnapshotBytes, additionalBytes)
}

func nilCapacityDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

func validCapacityProjectID(projectID string) bool {
	return recordauth.ValidateProjectID(recordauth.ProjectID(projectID)) == nil
}

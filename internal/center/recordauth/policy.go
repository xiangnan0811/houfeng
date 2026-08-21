package recordauth

import "errors"

// DenialReason is intentionally resource-free. It describes a policy class
// for internal metrics or diagnostics without exposing record IDs, contents,
// visibility grants, or source identifiers.
type DenialReason string

const (
	DenialReasonInvalidActor      DenialReason = "invalid_actor"
	DenialReasonInvalidCapability DenialReason = "invalid_capability"
	DenialReasonInvalidResource   DenialReason = "invalid_resource"
	DenialReasonCrossProject      DenialReason = "cross_project"
	DenialReasonRoleCapability    DenialReason = "role_capability"
	DenialReasonVisibility        DenialReason = "visibility"
	DenialReasonCapture           DenialReason = "capture"
	DenialReasonSource            DenialReason = "source"
)

// DeniedError preserves a reason for trusted in-process callers while
// exposing only the stable ErrDenied message externally.
type DeniedError struct {
	reason DenialReason
}

func (err *DeniedError) Error() string {
	return ErrDenied.Error()
}

func (err *DeniedError) Unwrap() error {
	return ErrDenied
}

// Reason returns the resource-free reason carried by a policy denial.
func (err *DeniedError) Reason() DenialReason {
	if err == nil {
		return ""
	}
	return err.reason
}

// DenialReasonFromError extracts the internal reason when the error came from
// this package. It never parses an error string.
func DenialReasonFromError(err error) (DenialReason, bool) {
	var denied *DeniedError
	if !errors.As(err, &denied) {
		return "", false
	}
	return denied.Reason(), true
}

// Policy is the sole v1 resource authorization implementation.
type Policy struct{}

// Authorize evaluates a capability against validated immutable resource
// evidence. Every failure is fail-closed and satisfies errors.Is(err,
// ErrDenied).
func (Policy) Authorize(actor ActorScope, capability Capability, resource ResourceScope) error {
	normalizedActor, err := NormalizeActorScope(actor)
	if err != nil {
		return denied(DenialReasonInvalidActor)
	}
	if !knownCapability(capability) {
		return denied(DenialReasonInvalidCapability)
	}

	normalizedResource, err := validateCanonicalResourceScope(resource)
	if err != nil {
		return denied(DenialReasonInvalidResource)
	}
	if normalizedActor.ProjectID != normalizedResource.ProjectID {
		return denied(DenialReasonCrossProject)
	}
	if !roleAllowsCapability(normalizedActor.Role, capability) {
		return denied(DenialReasonRoleCapability)
	}

	if !scopeAllowsActor(normalizedResource.Visibility, normalizedActor) {
		return denied(DenialReasonVisibility)
	}
	for _, source := range normalizedResource.Sources {
		if !scopeAllowsActor(source.CaptureScope, normalizedActor) {
			return denied(DenialReasonCapture)
		}
		if source.State == SourceStateLive {
			if source.CurrentScope == nil || !scopeAllowsActor(*source.CurrentScope, normalizedActor) {
				return denied(DenialReasonSource)
			}
			continue
		}
		if source.FinalFloor == nil || !scopeAllowsActor(*source.FinalFloor, normalizedActor) {
			return denied(DenialReasonSource)
		}
	}
	return nil
}

// Authorize is the package-level convenience entry point for the sole v1
// Policy implementation.
func Authorize(actor ActorScope, capability Capability, resource ResourceScope) error {
	return (Policy{}).Authorize(actor, capability, resource)
}

// AllowsCapability evaluates the closed role/capability table without a record
// resource. Use Authorize when a record/source scope is available.
func AllowsCapability(actor ActorScope, capability Capability) error {
	normalized, err := NormalizeActorScope(actor)
	if err != nil {
		return denied(DenialReasonInvalidActor)
	}
	if !knownCapability(capability) {
		return denied(DenialReasonInvalidCapability)
	}
	if !roleAllowsCapability(normalized.Role, capability) {
		return denied(DenialReasonRoleCapability)
	}
	return nil
}

func denied(reason DenialReason) error {
	return &DeniedError{reason: reason}
}

func roleAllowsCapability(role Role, capability Capability) bool {
	switch role {
	case RoleProjectAdmin:
		return knownCapability(capability)
	case RoleViewer:
		switch capability {
		case CapabilityRecordRead,
			CapabilityDraftRead,
			CapabilityEvidenceRead,
			CapabilityAttachmentRead,
			CapabilitySearchRead,
			CapabilityActivityRead,
			CapabilityComparisonRead,
			CapabilityNotificationRead,
			CapabilityExport:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func scopeAllowsActor(scope VisibilityScope, actor ActorScope) bool {
	if scope.Kind == VisibilityKindProject {
		return true
	}
	for _, role := range scope.AllowedRoles {
		if role == actor.Role {
			return true
		}
	}
	for _, allowedGroupID := range scope.AllowedGroupIDs {
		for _, actorGroupID := range actor.GroupIDs {
			if allowedGroupID == actorGroupID {
				return true
			}
		}
	}
	return false
}

func validateCanonicalResourceScope(input ResourceScope) (ResourceScope, error) {
	if input.Version != ResourceScopeVersionV1 {
		return ResourceScope{}, ErrInvalidResourceScope
	}
	if !knownProjectID(input.ProjectID) {
		return ResourceScope{}, ErrInvalidResourceScope
	}
	visibility, err := validateCanonicalVisibilityScope(input.Visibility)
	if err != nil || visibility.ProjectID != input.ProjectID {
		return ResourceScope{}, ErrInvalidResourceScope
	}
	if len(input.Sources) == 0 {
		return ResourceScope{}, ErrInvalidResourceScope
	}

	normalized := ResourceScope{
		Version:    input.Version,
		ProjectID:  input.ProjectID,
		Visibility: visibility,
		Sources:    make([]SourceAuthorization, 0, len(input.Sources)),
	}
	for _, source := range input.Sources {
		normalizedSource, err := validateCanonicalSourceAuthorization(source)
		if err != nil {
			return ResourceScope{}, ErrInvalidResourceScope
		}
		if normalizedSource.CaptureScope.ProjectID != input.ProjectID {
			return ResourceScope{}, ErrInvalidResourceScope
		}
		if normalizedSource.State == SourceStateLive {
			if normalizedSource.CurrentScope == nil || normalizedSource.CurrentScope.ProjectID != input.ProjectID {
				return ResourceScope{}, ErrInvalidResourceScope
			}
		} else if normalizedSource.FinalFloor == nil || normalizedSource.LastLiveScope == nil ||
			normalizedSource.FinalFloor.ProjectID != input.ProjectID || normalizedSource.LastLiveScope.ProjectID != input.ProjectID {
			return ResourceScope{}, ErrInvalidResourceScope
		}
		normalized.Sources = append(normalized.Sources, normalizedSource)
	}
	return normalized, nil
}

func validateCanonicalVisibilityScope(input VisibilityScope) (VisibilityScope, error) {
	normalized, err := NormalizeVisibilityScope(input)
	if err != nil {
		return VisibilityScope{}, ErrInvalidVisibilityScope
	}
	if !sameVisibilityScope(input, normalized) {
		return VisibilityScope{}, ErrInvalidVisibilityScope
	}
	return normalized, nil
}

func validateCanonicalSourceAuthorization(input SourceAuthorization) (SourceAuthorization, error) {
	normalized, err := NormalizeSourceAuthorization(input)
	if err != nil {
		return SourceAuthorization{}, ErrInvalidSourceAuthorization
	}
	if !sameVisibilityScope(input.CaptureScope, normalized.CaptureScope) {
		return SourceAuthorization{}, ErrInvalidSourceAuthorization
	}
	switch input.State {
	case SourceStateLive:
		if input.CurrentScope == nil || normalized.CurrentScope == nil || !sameVisibilityScope(*input.CurrentScope, *normalized.CurrentScope) {
			return SourceAuthorization{}, ErrInvalidSourceAuthorization
		}
	case SourceStateTombstoned:
		if input.FinalFloor == nil || normalized.FinalFloor == nil ||
			input.LastLiveScope == nil || normalized.LastLiveScope == nil ||
			!sameVisibilityScope(*input.FinalFloor, *normalized.FinalFloor) ||
			!sameVisibilityScope(*input.LastLiveScope, *normalized.LastLiveScope) {
			return SourceAuthorization{}, ErrInvalidSourceAuthorization
		}
	default:
		return SourceAuthorization{}, ErrInvalidSourceAuthorization
	}
	if input.Digest != normalized.Digest {
		return SourceAuthorization{}, ErrInvalidSourceAuthorization
	}
	return normalized, nil
}

func sameVisibilityScope(left, right VisibilityScope) bool {
	if left.Version != right.Version ||
		left.Kind != right.Kind ||
		left.ProjectID != right.ProjectID ||
		left.PolicyVersion != right.PolicyVersion ||
		left.PolicyRevision != right.PolicyRevision ||
		left.CanonicalHash != right.CanonicalHash ||
		len(left.AllowedRoles) != len(right.AllowedRoles) ||
		len(left.AllowedGroupIDs) != len(right.AllowedGroupIDs) {
		return false
	}
	for index := range left.AllowedRoles {
		if left.AllowedRoles[index] != right.AllowedRoles[index] {
			return false
		}
	}
	for index := range left.AllowedGroupIDs {
		if left.AllowedGroupIDs[index] != right.AllowedGroupIDs[index] {
			return false
		}
	}
	return true
}

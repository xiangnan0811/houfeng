package records

import "houfeng/internal/center/recordauth"

// RecordAuthorizationEvidence carries the complete immutable resource
// evidence required by the shared record authorization policy.
type RecordAuthorizationEvidence struct {
	ProjectID  recordauth.ProjectID
	Visibility recordauth.VisibilityScope
	Sources    []recordauth.SourceAuthorization
}

// AuthorizeRecordResource is the records-domain seam for the sole v1 policy.
// All visibility, capture, live, and tombstone intersections remain owned by
// recordauth.Policy.
func AuthorizeRecordResource(
	actor recordauth.ActorScope,
	capability recordauth.Capability,
	evidence RecordAuthorizationEvidence,
) error {
	return recordauth.Authorize(actor, capability, recordauth.ResourceScope{
		Version:    recordauth.ResourceScopeVersionV1,
		ProjectID:  evidence.ProjectID,
		Visibility: evidence.Visibility,
		Sources:    append([]recordauth.SourceAuthorization(nil), evidence.Sources...),
	})
}

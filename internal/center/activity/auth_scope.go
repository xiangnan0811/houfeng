package activity

import "houfeng/internal/center/recordauth"

// ProjectAuthScope is the default authorization stamp for activity candidates
// that are project-visible system facts (monitoring, command, evidence, asset
// history). Record-domain candidates must not use this helper: they stamp
// AuthScopeFromVisibility with the revision's authoritative VisibilityScope.
func ProjectAuthScope(projectID recordauth.ProjectID) (recordauth.ResourceScope, error) {
	if projectID == "" {
		projectID = recordauth.ProjectIDDefault
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      projectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		return recordauth.ResourceScope{}, err
	}
	return AuthScopeFromVisibility(visibility), nil
}

// AuthScopeFromVisibility builds the projection AuthScope from a canonical
// visibility document. AuthScopeDigest fingerprints Visibility only; Sources
// are not required for the digest predicate and stay empty here.
func AuthScopeFromVisibility(visibility recordauth.VisibilityScope) recordauth.ResourceScope {
	return recordauth.ResourceScope{
		Version:    recordauth.ResourceScopeVersionV1,
		ProjectID:  visibility.ProjectID,
		Visibility: visibility,
	}
}

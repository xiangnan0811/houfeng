package activity

import (
	"houfeng/internal/center/recordauth"
)

// AuthFilter is the store-facing authorization predicate for activity pages.
type AuthFilter struct {
	Unrestricted       bool
	AllowedAuthDigests [][32]byte
}

// AuthFilterForActor maps a normalized actor to the digest set subject and
// export page queries may apply before ORDER BY. Project admins skip the
// predicate; viewers match only project-visibility digests. Unstamped rows
// (empty visibility → sha256(nil)) are fail-closed and never listed for viewers.
func AuthFilterForActor(actor recordauth.ActorScope) (AuthFilter, error) {
	if actor.Role == recordauth.RoleProjectAdmin {
		return AuthFilter{Unrestricted: true}, nil
	}
	project, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      actor.ProjectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		return AuthFilter{}, err
	}
	return AuthFilter{
		AllowedAuthDigests: [][32]byte{
			project.CanonicalHash,
		},
	}, nil
}

func authFilterForActor(actor recordauth.ActorScope) (AuthFilter, error) {
	return AuthFilterForActor(actor)
}

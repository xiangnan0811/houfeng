package records

import (
	"errors"
	"strings"
	"testing"

	"houfeng/internal/center/recordauth"
)

const testOtherRecordGroupID = "rag_other"

func TestRecordAuthorizationUsesCaptureAndCurrentIntersection(t *testing.T) {
	t.Parallel()

	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	projectScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	currentScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testRecordGroupID})
	source := mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, projectScope, currentScope)

	err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: projectScope,
		Sources:    []recordauth.SourceAuthorization{source},
	})
	if err != nil {
		t.Fatalf("AuthorizeRecordResource() error = %v", err)
	}

	otherScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testOtherRecordGroupID})
	source = mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, projectScope, otherScope)
	assertOpaqueRecordAuthorizationDenied(t, AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: projectScope,
		Sources:    []recordauth.SourceAuthorization{source},
	}))
}

func TestRecordAuthorizationRejectsCurrentWideningThroughPolicy(t *testing.T) {
	t.Parallel()

	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	capture := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testRecordGroupID})
	current := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testRecordGroupID})
	source := mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, capture, current)
	widened := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	source.CurrentScope = &widened

	assertOpaqueRecordAuthorizationDenied(t, AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: capture,
		Sources:    []recordauth.SourceAuthorization{source},
	}))
}

func TestRecordAuthorizationAcceptsWitnessedTombstoneAndDropsDeletedRoute(t *testing.T) {
	t.Parallel()

	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	projectScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	restrictedScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testRecordGroupID})
	source := mustTombstonedAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, projectScope, restrictedScope, restrictedScope)

	err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: projectScope,
		Sources:    []recordauth.SourceAuthorization{source},
	})
	if err != nil {
		t.Fatalf("AuthorizeRecordResource() error = %v", err)
	}

	reference := SubjectReference{
		RegistryVersion: SubjectRegistryVersionV1,
		Kind:            SubjectKindVPS,
		Role:            RelationRoleAffected,
		SourceID:        testRecordVPSID,
		Primary:         true,
	}
	resolved := ResolvedSubject{
		ProjectID:            recordauth.ProjectIDDefault,
		StableID:             testRecordVPSID,
		IdentitySnapshot:     mustSubjectSnapshot(t, SubjectKindVPS, map[string]string{"display_name": "Deleted VPS"}),
		CaptureAuthorization: source,
	}
	got, err := normalizeResolvedSubject(recordauth.ProjectIDDefault, reference, resolved)
	if err != nil {
		t.Fatalf("normalizeResolvedSubject() error = %v", err)
	}
	if got.LiveRoute != "" {
		t.Fatalf("tombstoned LiveRoute = %q, want empty", got.LiveRoute)
	}
	resolved.LiveRoute = "/vps/" + testRecordVPSID
	if _, err := normalizeResolvedSubject(recordauth.ProjectIDDefault, reference, resolved); !errors.Is(err, ErrInvalidResolvedSubject) {
		t.Fatalf("normalizeResolvedSubject() error = %v, want ErrInvalidResolvedSubject", err)
	}
}

func TestRecordAuthorizationRejectsIncompleteOrUnknownTombstoneEvidence(t *testing.T) {
	t.Parallel()

	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	projectScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	restrictedScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testRecordGroupID})
	base := mustTombstonedAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, projectScope, restrictedScope, restrictedScope)
	tests := []struct {
		name   string
		mutate func(recordauth.SourceAuthorization) recordauth.SourceAuthorization
	}{
		{name: "missing floor", mutate: func(source recordauth.SourceAuthorization) recordauth.SourceAuthorization {
			source.FinalFloor = nil
			return source
		}},
		{name: "missing witness", mutate: func(source recordauth.SourceAuthorization) recordauth.SourceAuthorization {
			source.LastLiveScope = nil
			return source
		}},
		{name: "unknown floor kind", mutate: func(source recordauth.SourceAuthorization) recordauth.SourceAuthorization {
			floor := *source.FinalFloor
			floor.Kind = "team"
			source.FinalFloor = &floor
			return source
		}},
		{name: "unknown floor version", mutate: func(source recordauth.SourceAuthorization) recordauth.SourceAuthorization {
			floor := *source.FinalFloor
			floor.Version = 2
			source.FinalFloor = &floor
			return source
		}},
		{name: "floor wider than witness", mutate: func(source recordauth.SourceAuthorization) recordauth.SourceAuthorization {
			floor := projectScope
			source.FinalFloor = &floor
			return source
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOpaqueRecordAuthorizationDenied(t, AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: projectScope,
				Sources:    []recordauth.SourceAuthorization{tt.mutate(base)},
			}))
		})
	}
}

func TestRecordAuthorizationNarrowsAcrossEverySource(t *testing.T) {
	t.Parallel()

	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	projectScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	allowedScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testRecordGroupID})
	otherScope := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{testOtherRecordGroupID})
	sources := []recordauth.SourceAuthorization{
		mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, projectScope, allowedScope),
		mustLiveAuthorization(t, recordauth.SourceKindTarget, testRecordTargetID, projectScope, otherScope),
	}

	assertOpaqueRecordAuthorizationDenied(t, AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: projectScope,
		Sources:    sources,
	}))
}

func mustAuthorizationActor(t *testing.T, role recordauth.Role, groupIDs ...string) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    testRecordAuthorID,
		Role:      role,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  groupIDs,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func mustAuthorizationVisibility(
	t *testing.T,
	kind recordauth.VisibilityKind,
	groupIDs []string,
) recordauth.VisibilityScope {
	t.Helper()
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:         recordauth.VisibilityScopeVersionV1,
		Kind:            kind,
		ProjectID:       recordauth.ProjectIDDefault,
		AllowedGroupIDs: groupIDs,
		PolicyVersion:   recordauth.PolicyVersionV1,
		PolicyRevision:  11,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	return scope
}

func mustLiveAuthorization(
	t *testing.T,
	kind recordauth.SourceKind,
	sourceID string,
	capture recordauth.VisibilityScope,
	current recordauth.VisibilityScope,
) recordauth.SourceAuthorization {
	t.Helper()
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         kind,
		SourceID:     sourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: capture,
		CurrentScope: &current,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return source
}

func mustTombstonedAuthorization(
	t *testing.T,
	kind recordauth.SourceKind,
	sourceID string,
	capture recordauth.VisibilityScope,
	lastLive recordauth.VisibilityScope,
	floor recordauth.VisibilityScope,
) recordauth.SourceAuthorization {
	t.Helper()
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:       recordauth.SourceAuthorizationVersionV1,
		Kind:          kind,
		SourceID:      sourceID,
		State:         recordauth.SourceStateTombstoned,
		CaptureScope:  capture,
		FinalFloor:    &floor,
		LastLiveScope: &lastLive,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return source
}

func assertOpaqueRecordAuthorizationDenied(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("authorization error = %v, want recordauth.ErrDenied", err)
	}
	if err.Error() != recordauth.ErrDenied.Error() {
		t.Fatalf("authorization error = %q, want opaque %q", err, recordauth.ErrDenied)
	}
	for _, forbidden := range []string{testRecordVPSID, testRecordTargetID, testRecordGroupID, testOtherRecordGroupID} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("authorization error %q leaks %q", err, forbidden)
		}
	}
}

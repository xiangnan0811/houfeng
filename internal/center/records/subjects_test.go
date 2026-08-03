package records

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/recordauth"
)

const (
	testRecordMonitoringInstanceID = "mi_0123456789abcdef"
	testRecordTargetID             = "tg_0123456789abcdef"
)

func TestSubjectRegistryNormalizesClosedOrderedReferences(t *testing.T) {
	t.Parallel()

	references := []SubjectReference{
		{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindVPS, Role: RelationRoleAffected, SourceID: testRecordVPSID, Primary: true},
		{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindMonitoringInstance, Role: RelationRoleContext, SourceID: testRecordMonitoringInstanceID},
		{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindTarget, Role: RelationRoleEvidenceSource, SourceID: testRecordTargetID},
	}

	got, err := NormalizeSubjectReferences(references)
	if err != nil {
		t.Fatalf("NormalizeSubjectReferences() error = %v", err)
	}
	if len(got) != len(references) {
		t.Fatalf("NormalizeSubjectReferences() length = %d, want %d", len(got), len(references))
	}
	for index := range references {
		if got[index] != references[index] {
			t.Fatalf("reference %d = %#v, want %#v", index, got[index], references[index])
		}
	}
	references[0].SourceID = "vps_fedcba9876543210"
	got[1].SourceID = "mi_fedcba9876543210"
	again, err := NormalizeSubjectReferences([]SubjectReference{
		{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindVPS, Role: RelationRoleAffected, SourceID: testRecordVPSID, Primary: true},
	})
	if err != nil || again[0].SourceID != testRecordVPSID {
		t.Fatalf("normalized reference changed through caller mutation: %#v, %v", again, err)
	}
}

func TestSubjectRegistryRejectsUnknownDuplicateAndInvalidPrimaryShapes(t *testing.T) {
	t.Parallel()

	base := []SubjectReference{
		{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindVPS, Role: RelationRoleAffected, SourceID: testRecordVPSID, Primary: true},
		{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindMonitoringInstance, Role: RelationRoleContext, SourceID: testRecordMonitoringInstanceID},
	}
	tests := []struct {
		name   string
		mutate func([]SubjectReference) []SubjectReference
	}{
		{name: "unknown version", mutate: func(values []SubjectReference) []SubjectReference { values[0].RegistryVersion = 2; return values }},
		{name: "unknown kind", mutate: func(values []SubjectReference) []SubjectReference { values[0].Kind = "subscription"; return values }},
		{name: "unknown role", mutate: func(values []SubjectReference) []SubjectReference { values[0].Role = "trigger"; return values }},
		{name: "malformed source id", mutate: func(values []SubjectReference) []SubjectReference { values[0].SourceID = "vps_INVALID"; return values }},
		{name: "kind id mismatch", mutate: func(values []SubjectReference) []SubjectReference {
			values[0].SourceID = testRecordMonitoringInstanceID
			return values
		}},
		{name: "no primary", mutate: func(values []SubjectReference) []SubjectReference { values[0].Primary = false; return values }},
		{name: "multiple primary", mutate: func(values []SubjectReference) []SubjectReference { values[1].Primary = true; return values }},
		{name: "duplicate tuple", mutate: func(values []SubjectReference) []SubjectReference {
			duplicate := values[0]
			duplicate.Primary = false
			return append(values, duplicate)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := append([]SubjectReference(nil), base...)
			if _, err := NormalizeSubjectReferences(tt.mutate(values)); !errors.Is(err, ErrInvalidSubjectReference) {
				t.Fatalf("NormalizeSubjectReferences() error = %v, want ErrInvalidSubjectReference", err)
			}
		})
	}
}

func TestSubjectIdentitySnapshotIsServerConstructedAllowlistedAndImmutable(t *testing.T) {
	t.Parallel()

	fields := map[string]string{
		"display_name": "VPS Alpha",
		"provider":     "Example Cloud",
		"region":       "ap-east",
		"purpose":      "edge gateway",
	}
	snapshot, err := NewSubjectIdentitySnapshot(SubjectKindVPS, fields)
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	fields["display_name"] = "input mutation"
	if got := snapshot.Fields()["display_name"]; got != "VPS Alpha" {
		t.Fatalf("snapshot field = %q, want VPS Alpha", got)
	}
	returned := snapshot.Fields()
	returned["display_name"] = "return mutation"
	if got := snapshot.Fields()["display_name"]; got != "VPS Alpha" {
		t.Fatalf("snapshot changed through returned map: %q", got)
	}

	tests := []struct {
		name   string
		kind   SubjectKind
		fields map[string]string
	}{
		{name: "missing display name", kind: SubjectKindVPS, fields: map[string]string{"provider": "Example"}},
		{name: "client acl field", kind: SubjectKindVPS, fields: map[string]string{"display_name": "VPS", "allowed_groups": "rag_admin"}},
		{name: "target field on vps", kind: SubjectKindVPS, fields: map[string]string{"display_name": "VPS", "target_type": "tcp"}},
		{name: "unknown kind", kind: "subscription", fields: map[string]string{"display_name": "Subscription"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSubjectIdentitySnapshot(tt.kind, tt.fields); !errors.Is(err, ErrInvalidSubjectSnapshot) {
				t.Fatalf("NewSubjectIdentitySnapshot() error = %v, want ErrInvalidSubjectSnapshot", err)
			}
		})
	}
}

func TestSubjectAdapterRegistryResolvesAndValidatesServerOwnedEvidence(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	visibility := mustRecordVisibility(t)
	snapshot := mustSubjectSnapshot(t, SubjectKindVPS, map[string]string{"display_name": "VPS Alpha", "provider": "Example Cloud"})
	adapter := &fakeSubjectSourceAdapter{
		kind: SubjectKindVPS,
		resolved: ResolvedSubject{
			ProjectID:            recordauth.ProjectIDDefault,
			StableID:             testRecordVPSID,
			IdentitySnapshot:     snapshot,
			LiveRoute:            "/vps/" + testRecordVPSID,
			CaptureAuthorization: mustSourceAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, visibility, visibility),
		},
	}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	reference := SubjectReference{
		RegistryVersion: SubjectRegistryVersionV1,
		Kind:            SubjectKindVPS,
		Role:            RelationRoleAffected,
		SourceID:        testRecordVPSID,
		Primary:         true,
	}
	resolved, err := registry.Resolve(context.Background(), actor, reference)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.StableID != testRecordVPSID || resolved.ProjectID != recordauth.ProjectIDDefault || resolved.LiveRoute != "/vps/"+testRecordVPSID {
		t.Fatalf("Resolve() = %#v", resolved)
	}
	if adapter.calls != 1 || adapter.reference != reference || adapter.actor.CanonicalHash() != actor.CanonicalHash() {
		t.Fatalf("adapter call = calls:%d actor:%#v reference:%#v", adapter.calls, adapter.actor, adapter.reference)
	}
}

func TestSubjectAdapterRegistryRejectsDuplicateUnknownNilAndInvalidResults(t *testing.T) {
	t.Parallel()

	valid := &fakeSubjectSourceAdapter{kind: SubjectKindVPS}
	if _, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{valid, &fakeSubjectSourceAdapter{kind: SubjectKindVPS}}); !errors.Is(err, ErrInvalidSubjectAdapter) {
		t.Fatalf("duplicate adapter error = %v, want ErrInvalidSubjectAdapter", err)
	}
	if _, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{&fakeSubjectSourceAdapter{kind: "subscription"}}); !errors.Is(err, ErrInvalidSubjectAdapter) {
		t.Fatalf("unknown adapter error = %v, want ErrInvalidSubjectAdapter", err)
	}
	var nilAdapter *fakeSubjectSourceAdapter
	if _, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{nilAdapter}); !errors.Is(err, ErrInvalidSubjectAdapter) {
		t.Fatalf("typed nil adapter error = %v, want ErrInvalidSubjectAdapter", err)
	}

	actor := mustRecordActor(t)
	reference := SubjectReference{RegistryVersion: SubjectRegistryVersionV1, Kind: SubjectKindVPS, Role: RelationRoleAffected, SourceID: testRecordVPSID, Primary: true}
	registry, err := NewSubjectAdapterRegistry(nil)
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry(nil) error = %v", err)
	}
	if _, err := registry.Resolve(context.Background(), actor, reference); !errors.Is(err, ErrSubjectAdapterNotFound) {
		t.Fatalf("missing adapter error = %v, want ErrSubjectAdapterNotFound", err)
	}

	visibility := mustRecordVisibility(t)
	validSnapshot := mustSubjectSnapshot(t, SubjectKindVPS, map[string]string{"display_name": "VPS Alpha"})
	validAuthorization := mustSourceAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, visibility, visibility)
	tamperedAuthorization := validAuthorization
	tamperedAuthorization.Digest[0] ^= 0xff
	tests := []struct {
		name   string
		result ResolvedSubject
	}{
		{name: "project mismatch", result: ResolvedSubject{ProjectID: "other", StableID: testRecordVPSID, IdentitySnapshot: validSnapshot, CaptureAuthorization: validAuthorization}},
		{name: "stable id mismatch", result: ResolvedSubject{ProjectID: recordauth.ProjectIDDefault, StableID: "vps_fedcba9876543210", IdentitySnapshot: validSnapshot, CaptureAuthorization: validAuthorization}},
		{name: "snapshot kind mismatch", result: ResolvedSubject{ProjectID: recordauth.ProjectIDDefault, StableID: testRecordVPSID, IdentitySnapshot: mustSubjectSnapshot(t, SubjectKindTarget, map[string]string{"display_name": "Target", "target_type": "tcp"}), CaptureAuthorization: validAuthorization}},
		{name: "capture kind mismatch", result: ResolvedSubject{ProjectID: recordauth.ProjectIDDefault, StableID: testRecordVPSID, IdentitySnapshot: validSnapshot, CaptureAuthorization: mustSourceAuthorization(t, recordauth.SourceKindMonitoringInstance, testRecordMonitoringInstanceID, visibility, visibility)}},
		{name: "capture digest mismatch", result: ResolvedSubject{ProjectID: recordauth.ProjectIDDefault, StableID: testRecordVPSID, IdentitySnapshot: validSnapshot, CaptureAuthorization: tamperedAuthorization}},
		{name: "unsafe route", result: ResolvedSubject{ProjectID: recordauth.ProjectIDDefault, StableID: testRecordVPSID, IdentitySnapshot: validSnapshot, LiveRoute: "https://example.invalid/vps", CaptureAuthorization: validAuthorization}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &fakeSubjectSourceAdapter{kind: SubjectKindVPS, resolved: tt.result}
			registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			if _, err := registry.Resolve(context.Background(), actor, reference); !errors.Is(err, ErrInvalidResolvedSubject) {
				t.Fatalf("Resolve() error = %v, want ErrInvalidResolvedSubject", err)
			}
		})
	}
}

type fakeSubjectSourceAdapter struct {
	kind      SubjectKind
	resolved  ResolvedSubject
	err       error
	calls     int
	actor     recordauth.ActorScope
	reference SubjectReference
}

func (adapter *fakeSubjectSourceAdapter) Kind() SubjectKind {
	return adapter.kind
}

func (adapter *fakeSubjectSourceAdapter) Resolve(_ context.Context, actor recordauth.ActorScope, reference SubjectReference) (ResolvedSubject, error) {
	adapter.calls++
	adapter.actor = actor.Clone()
	adapter.reference = reference
	return adapter.resolved, adapter.err
}

func mustRecordActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    testRecordAuthorID,
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  []string{testRecordGroupID},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func mustSubjectSnapshot(t *testing.T, kind SubjectKind, fields map[string]string) SubjectIdentitySnapshot {
	t.Helper()
	snapshot, err := NewSubjectIdentitySnapshot(kind, fields)
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	return snapshot
}

func mustSourceAuthorization(
	t *testing.T,
	kind recordauth.SourceKind,
	sourceID string,
	capture recordauth.VisibilityScope,
	current recordauth.VisibilityScope,
) recordauth.SourceAuthorization {
	t.Helper()
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
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
	return authorization
}

package adapters

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestRecordEvidenceSourceResolverClosedMatrix(t *testing.T) {
	t.Parallel()

	actor := resolverTestActor(t)
	dependencyErr := errors.New("source dependency unavailable")
	tests := []struct {
		name       string
		selection  evidence.Selection
		adapter    records.SubjectSourceAdapter
		wantErr    error
		wantKind   recordauth.SourceKind
		wantSource string
	}{
		{name: "vps", selection: resolverTestSelection("vps", "vps_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindVPS, "vps_0123456789abcdef", recordauth.ProjectIDDefault, nil), wantKind: recordauth.SourceKindVPS, wantSource: "vps_0123456789abcdef"},
		{name: "monitoring instance", selection: resolverTestSelection("monitoring_instance", "mi_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindMonitoringInstance, "mi_0123456789abcdef", recordauth.ProjectIDDefault, nil), wantKind: recordauth.SourceKindMonitoringInstance, wantSource: "mi_0123456789abcdef"},
		{name: "target", selection: resolverTestSelection("target", "tg_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindTarget, "tg_0123456789abcdef", recordauth.ProjectIDDefault, nil), wantKind: recordauth.SourceKindTarget, wantSource: "tg_0123456789abcdef"},
		{name: "unknown kind", selection: resolverTestSelection("generic", "vps_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindVPS, "vps_0123456789abcdef", recordauth.ProjectIDDefault, nil), wantErr: ErrEvidenceSourceUnavailable},
		{name: "wrong identifier for kind", selection: resolverTestSelection("vps", "tg_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindVPS, "vps_0123456789abcdef", recordauth.ProjectIDDefault, nil), wantErr: ErrEvidenceSourceUnavailable},
		{name: "wrong project", selection: resolverTestSelection("vps", "vps_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindVPS, "vps_0123456789abcdef", "project_other", nil), wantErr: ErrEvidenceSourceUnavailable},
		{name: "missing source", selection: resolverTestSelection("vps", "vps_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindVPS, "vps_0123456789abcdef", recordauth.ProjectIDDefault, records.ErrSubjectAdapterNotFound), wantErr: ErrEvidenceSourceUnavailable},
		{name: "dependency failure", selection: resolverTestSelection("vps", "vps_0123456789abcdef"), adapter: resolverTestAdapter(t, records.SubjectKindVPS, "vps_0123456789abcdef", recordauth.ProjectIDDefault, dependencyErr), wantErr: ErrEvidenceSourceUnavailable},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{test.adapter})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			resolver, err := NewRecordEvidenceSourceResolver(registry)
			if err != nil {
				t.Fatalf("NewRecordEvidenceSourceResolver() error = %v", err)
			}
			resolved, err := resolver.ResolveEvidenceSource(context.Background(), actor, test.selection)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ResolveEvidenceSource() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveEvidenceSource() error = %v", err)
			}
			if resolved.Subject.Type != string(test.wantKind) || resolved.Subject.ID != test.wantSource ||
				resolved.Source.Type != string(test.wantKind) || resolved.Source.ID != test.wantSource ||
				resolved.Authorization.Kind != test.wantKind || resolved.Authorization.SourceID != test.wantSource ||
				resolved.Authorization.CaptureScope.ProjectID != actor.ProjectID {
				t.Fatalf("ResolveEvidenceSource() = %#v", resolved)
			}
			if resolved.Subject.Fields["display_name"] == "" || resolved.Source.Fields["display_name"] == "" {
				t.Fatalf("ResolveEvidenceSource() omitted canonical identity: %#v", resolved)
			}
		})
	}
}

func resolverTestSelection(sourceType, sourceID string) evidence.Selection {
	return evidence.Selection{Key: evidence.IPQualityReportV1Key(), SourceType: sourceType, SourceID: sourceID}
}

func resolverTestActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

type resolverSubjectAdapter struct {
	kind     records.SubjectKind
	resolved records.ResolvedSubject
	err      error
}

func (adapter resolverSubjectAdapter) Kind() records.SubjectKind { return adapter.kind }

func (adapter resolverSubjectAdapter) Resolve(context.Context, recordauth.ActorScope, records.SubjectReference) (records.ResolvedSubject, error) {
	return adapter.resolved, adapter.err
}

func resolverTestAdapter(t *testing.T, kind records.SubjectKind, sourceID string, projectID recordauth.ProjectID, err error) records.SubjectSourceAdapter {
	t.Helper()
	identity, identityErr := records.NewSubjectIdentitySnapshot(kind, map[string]string{"display_name": "Canonical source"})
	if identityErr != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", identityErr)
	}
	sourceKind := recordauth.SourceKind(kind)
	visibility, visibilityErr := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: projectID, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if visibilityErr != nil {
		// Keep malformed cross-project output available for the resolver to reject.
		visibility = recordauth.VisibilityScope{
			Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
			ProjectID: projectID, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
		}
	}
	authorization, _ := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: sourceKind, SourceID: sourceID,
		State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility,
	})
	return resolverSubjectAdapter{kind: kind, err: err, resolved: records.ResolvedSubject{
		ProjectID: projectID, StableID: sourceID, IdentitySnapshot: identity,
		LiveRoute: "/source/" + sourceID, CaptureAuthorization: authorization,
	}}
}

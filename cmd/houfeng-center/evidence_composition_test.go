package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestEvidenceHandlerUnavailableWithoutApplication(t *testing.T) {
	t.Parallel()

	recorder := exerciseEvidenceHandler(t, handlers.Evidence(nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"evidence_service_unavailable"`) {
		t.Fatalf("fail-closed evidence response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestProductionEvidenceHandlerRejectsInvalidPreview(t *testing.T) {
	t.Parallel()

	source := compositionEvidenceSource{}
	pool := &pgxpool.Pool{}
	gate := &compositionAdmissionGate{}
	allocations := store.NewPostgresRecordBirthRepository(pool, gate, nil)
	birth, err := recordbirth.NewCoordinator(allocations, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	composition, err := newProductionEvidenceComposition(productionEvidenceCompositionDependencies{
		Pool: pool, Gate: gate, Birth: birth, Allocations: allocations, Subjects: compositionSubjects(t),
		Sources: productionEvidenceSources{
			IPQuality: source, Monitoring: source, Events: source, SubscriptionCosts: source, CommandAudits: source,
		},
	})
	if err != nil {
		t.Fatalf("newProductionEvidenceComposition() error = %v", err)
	}
	// Validation rejects this request before any repository or admission call.
	recorder := exerciseEvidenceHandler(t, composition.handler)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("evidence response = %d %s, want invalid request", recorder.Code, recorder.Body.String())
	}
}

func exerciseEvidenceHandler(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/evidence/capture-previews", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), mustBootstrapRecordsActor(t)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestProductionEvidenceCompositionRejectsClosedDependencies(t *testing.T) {
	t.Parallel()

	pool := &pgxpool.Pool{}
	gate := &compositionAdmissionGate{}
	allocations := store.NewPostgresRecordBirthRepository(pool, gate, nil)
	birth, err := recordbirth.NewCoordinator(allocations, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := productionEvidenceCompositionDependencies{
		Pool: pool, Gate: gate, Birth: birth, Allocations: allocations, Subjects: compositionSubjects(t),
		Sources: productionEvidenceSources{
			IPQuality: compositionEvidenceSource{}, Monitoring: compositionEvidenceSource{},
			Events: compositionEvidenceSource{}, SubscriptionCosts: compositionEvidenceSource{},
			CommandAudits: compositionEvidenceSource{},
		},
	}
	var typedNilGate *compositionAdmissionGate
	tests := []struct {
		name   string
		mutate func(*productionEvidenceCompositionDependencies)
	}{
		{name: "nil pool", mutate: func(value *productionEvidenceCompositionDependencies) { value.Pool = nil }},
		{name: "nil gate", mutate: func(value *productionEvidenceCompositionDependencies) { value.Gate = nil }},
		{name: "typed nil gate", mutate: func(value *productionEvidenceCompositionDependencies) { value.Gate = typedNilGate }},
		{name: "nil birth coordinator", mutate: func(value *productionEvidenceCompositionDependencies) { value.Birth = nil }},
		{name: "nil birth allocation repository", mutate: func(value *productionEvidenceCompositionDependencies) { value.Allocations = nil }},
		{name: "empty subjects", mutate: func(value *productionEvidenceCompositionDependencies) {
			value.Subjects, _ = records.NewSubjectAdapterRegistry(nil)
		}},
		{name: "nil IP quality", mutate: func(value *productionEvidenceCompositionDependencies) { value.Sources.IPQuality = nil }},
		{name: "nil monitoring", mutate: func(value *productionEvidenceCompositionDependencies) { value.Sources.Monitoring = nil }},
		{name: "nil events", mutate: func(value *productionEvidenceCompositionDependencies) { value.Sources.Events = nil }},
		{name: "nil subscription costs", mutate: func(value *productionEvidenceCompositionDependencies) { value.Sources.SubscriptionCosts = nil }},
		{name: "nil command audits", mutate: func(value *productionEvidenceCompositionDependencies) { value.Sources.CommandAudits = nil }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies := valid
			test.mutate(&dependencies)
			composition, err := newProductionEvidenceComposition(dependencies)
			if composition != nil || !errors.Is(err, evidence.ErrEvidenceServiceUnavailable) {
				t.Fatalf("newProductionEvidenceComposition() = (%T, %v), want nil/unavailable", composition, err)
			}
		})
	}
}

type compositionAdmissionGate struct{}

func (*compositionAdmissionGate) Admit(context.Context, pgx.Tx) error { return nil }

type compositionSubjectAdapter struct{ kind records.SubjectKind }

func (adapter compositionSubjectAdapter) Kind() records.SubjectKind { return adapter.kind }

func (adapter compositionSubjectAdapter) Resolve(_ context.Context, actor recordauth.ActorScope, reference records.SubjectReference) (records.ResolvedSubject, error) {
	identity, err := records.NewSubjectIdentitySnapshot(adapter.kind, map[string]string{"display_name": "Composition source"})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: actor.ProjectID, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKind(adapter.kind),
		SourceID: reference.SourceID, State: recordauth.SourceStateLive,
		CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	return records.ResolvedSubject{
		ProjectID: actor.ProjectID, StableID: reference.SourceID, IdentitySnapshot: identity,
		LiveRoute: "/source/" + reference.SourceID, CaptureAuthorization: authorization,
	}, nil
}

func compositionSubjects(t *testing.T) records.SubjectAdapterRegistry {
	t.Helper()
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		compositionSubjectAdapter{kind: records.SubjectKindVPS},
		compositionSubjectAdapter{kind: records.SubjectKindMonitoringInstance},
		compositionSubjectAdapter{kind: records.SubjectKindTarget},
	})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	return registry
}

type compositionEvidenceSource struct{}

func (compositionEvidenceSource) LoadIPQualityEvidence(context.Context, string, evidence.TimeWindow) (adapters.IPQualityEvidenceReport, error) {
	return adapters.IPQualityEvidenceReport{}, nil
}
func (compositionEvidenceSource) LoadMonitoringHostEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (adapters.MonitoringSeriesCapture, error) {
	return adapters.MonitoringSeriesCapture{}, nil
}
func (compositionEvidenceSource) LoadMonitoringProbeEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (adapters.MonitoringSeriesCapture, error) {
	return adapters.MonitoringSeriesCapture{}, nil
}
func (compositionEvidenceSource) LoadMonitoringEventEvidence(context.Context, string, string, evidence.TimeWindow) (adapters.MonitoringEventCapture, error) {
	return adapters.MonitoringEventCapture{}, nil
}
func (compositionEvidenceSource) LoadSubscriptionCostEvidence(context.Context, string, evidence.TimeWindow) (adapters.SubscriptionCostCapture, error) {
	return adapters.SubscriptionCostCapture{}, nil
}
func (compositionEvidenceSource) LoadCommandAuditEvidence(context.Context, string, evidence.TimeWindow) (adapters.CommandAuditCapture, error) {
	return adapters.CommandAuditCapture{}, nil
}

var _ store.AdmissionGate = (*compositionAdmissionGate)(nil)

package store

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationEvidenceProductionReadExportAndReferenceReuse(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-production-read", 3)
	writer := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)
	recordRepository := newRecordsPostgresRepository(t, runtimePool, NewRecordEvidenceRevisionParticipant())
	databaseNow := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	capture := storePreparedEvidenceCapture(
		t, "rec_evidenceproduction", "evs_evidenceproduction", "evi_777777777777777777777777", databaseNow,
	)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, writer, capture)
	created, err := recordRepository.CommitRevision(ctx, recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
		"Production evidence", "production-evidence-create",
		storeEvidenceRevisionPreparation(t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()}),
	))
	if err != nil {
		t.Fatalf("CommitRevision(create) error = %v", err)
	}

	subjects := evidenceReadPostgresSubjects(t, nil)
	subjectResolver := NewRecordSubjectReadResolver(subjects, nil)
	authorizations := NewPostgresCurrentRecordAuthorizationSource(runtimePool, subjectResolver, allowRecordPlatformAdmissionGate)
	registry := evidenceReadPostgresRegistry(t)
	repository, err := NewPostgresEvidenceRepositoryWithReadSources(
		runtimePool, allowRecordPlatformAdmissionGate, registry, authorizations, subjectResolver,
	)
	if err != nil {
		t.Fatalf("NewPostgresEvidenceRepositoryWithReadSources() error = %v", err)
	}
	actor := mustStoreRecordActor(t)
	state, err := repository.LoadEvidenceSnapshot(ctx, actor, capture.SnapshotID())
	if err != nil {
		t.Fatalf("LoadEvidenceSnapshot() error = %v", err)
	}
	if state.RecordID != created.RecordID || state.SnapshotID != capture.SnapshotID() ||
		state.Envelope.CanonicalHash != capture.Snapshot().Hash() || !state.SourceAvailable ||
		string(state.CanonicalPayload) != string(capture.Snapshot().Bytes()) {
		t.Fatalf("LoadEvidenceSnapshot() = %#v", state)
	}
	serviceCapacity, err := evidence.NewCapacityEnforcer(evidence.DefaultCapacityPolicy(), repository)
	if err != nil {
		t.Fatalf("NewCapacityEnforcer() error = %v", err)
	}
	service, err := evidence.NewService(registry, repository, repository, serviceCapacity)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	read, err := service.ReadSnapshot(ctx, evidence.ReadSnapshotRequest{Actor: actor, SnapshotID: capture.SnapshotID()})
	if err != nil || read.RecordID != created.RecordID || read.Summary.ReadModel["version"] != "monitoring.probe/v2" {
		t.Fatalf("ReadSnapshot() = %#v, %v", read, err)
	}
	exporter, err := evidence.NewExportAdapter(registry, repository)
	if err != nil {
		t.Fatalf("NewExportAdapter() error = %v", err)
	}
	material, err := exporter.Export(ctx, evidence.ExportRequest{Actor: actor, SnapshotID: capture.SnapshotID(), Mode: evidence.ExportModeSafe})
	if err != nil || material.Key != capture.Snapshot().Envelope().Key || string(material.Bytes) != `{"status":"complete"}` {
		t.Fatalf("Export() = %#v, %v", material, err)
	}
	preparedReference, err := evidence.PrepareExistingSnapshotReference(ctx, repository, actor, created.RecordID, capture.SnapshotID())
	if err != nil {
		t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
	}
	updated, err := recordRepository.CommitRevision(ctx, recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordUpdate, created.RecordID, created.RevisionID,
		created.LockVersion, created.AuthorizationEpoch, "Production evidence reused", "production-evidence-reuse",
		storeEvidenceRevisionPreparation(t, created.RecordID, nil, []evidence.PreparedReference{preparedReference}, []string{capture.SnapshotID()}),
	))
	if err != nil {
		t.Fatalf("CommitRevision(existing reference) error = %v", err)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, created.RecordID, 2, 1, 2, 0)
	if updated.RevisionID == created.RevisionID {
		t.Fatal("existing reference reuse did not create a new revision")
	}

	restricted := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleViewer}, nil, 2)
	restrictedSubjects := evidenceReadPostgresSubjects(t, &restricted)
	restrictedResolver := NewRecordSubjectReadResolver(restrictedSubjects, nil)
	restrictedAuthorizations := NewPostgresCurrentRecordAuthorizationSource(runtimePool, restrictedResolver, allowRecordPlatformAdmissionGate)
	restrictedRepository, err := NewPostgresEvidenceRepositoryWithReadSources(
		runtimePool, allowRecordPlatformAdmissionGate, registry, restrictedAuthorizations, restrictedResolver,
	)
	if err != nil {
		t.Fatalf("NewPostgresEvidenceRepositoryWithReadSources(restricted) error = %v", err)
	}
	if _, err := restrictedRepository.LoadEvidenceSnapshot(ctx, actor, capture.SnapshotID()); !errors.Is(err, evidence.ErrSnapshotNotFound) {
		t.Fatalf("LoadEvidenceSnapshot(permission intersection) error = %v, want opaque not found", err)
	}

	if _, err := fixture.db.Exec(ctx, `alter table public.evidence_snapshots disable trigger evidence_snapshots_reject_update`); err != nil {
		t.Fatalf("disable immutable trigger for corruption injection: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.evidence_snapshots set schema_version = 99 where snapshot_id = $1`, capture.SnapshotID()); err != nil {
		t.Fatalf("inject unknown evidence schema: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `alter table public.evidence_snapshots enable trigger evidence_snapshots_reject_update`); err != nil {
		t.Fatalf("restore immutable trigger: %v", err)
	}
	if _, err := repository.LoadEvidenceSnapshot(ctx, actor, capture.SnapshotID()); !errors.Is(err, evidence.ErrUnknownKindVersion) {
		t.Fatalf("LoadEvidenceSnapshot(unknown schema) error = %v, want unknown version", err)
	}
	if _, err := fixture.db.Exec(ctx, `alter table public.evidence_snapshots disable trigger evidence_snapshots_reject_update`); err != nil {
		t.Fatalf("disable immutable trigger for repair: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.evidence_snapshots set schema_version = 2 where snapshot_id = $1`, capture.SnapshotID()); err != nil {
		t.Fatalf("repair evidence schema: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `alter table public.evidence_snapshots enable trigger evidence_snapshots_reject_update`); err != nil {
		t.Fatalf("restore immutable trigger after repair: %v", err)
	}

	corruptConnection, err := fixture.db.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire corruption connection: %v", err)
	}
	defer corruptConnection.Release()
	if _, err := corruptConnection.Exec(ctx, `set session_replication_role = replica`); err != nil {
		t.Fatalf("disable referential triggers for corruption injection: %v", err)
	}
	captureDigest := capture.Snapshot().Hash()
	if _, err := corruptConnection.Exec(ctx, `delete from public.evidence_payloads where payload_digest = $1`, captureDigest[:]); err != nil {
		t.Fatalf("remove referenced payload for corruption injection: %v", err)
	}
	if _, err := corruptConnection.Exec(ctx, `set session_replication_role = origin`); err != nil {
		t.Fatalf("restore referential triggers: %v", err)
	}
	if _, err := evidence.PrepareExistingSnapshotReference(ctx, repository, actor, created.RecordID, capture.SnapshotID()); !errors.Is(err, evidence.ErrSnapshotNotFound) {
		t.Fatalf("PrepareExistingSnapshotReference(missing payload row) error = %v, want opaque not found", err)
	}
}

type evidenceReadPostgresSubjectAdapter struct {
	kind         records.SubjectKind
	currentScope *recordauth.VisibilityScope
}

func (adapter evidenceReadPostgresSubjectAdapter) Kind() records.SubjectKind { return adapter.kind }

func (adapter evidenceReadPostgresSubjectAdapter) Resolve(_ context.Context, actor recordauth.ActorScope, reference records.SubjectReference) (records.ResolvedSubject, error) {
	identity, err := records.NewSubjectIdentitySnapshot(adapter.kind, map[string]string{"display_name": "PostgreSQL evidence source"})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	capture := mustEvidenceReadPostgresVisibility(actor.ProjectID)
	current := capture
	if adapter.currentScope != nil {
		current = *adapter.currentScope
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: evidenceReadPostgresSourceKind(adapter.kind),
		SourceID: reference.SourceID, State: recordauth.SourceStateLive,
		CaptureScope: capture, CurrentScope: &current,
	})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	return records.ResolvedSubject{
		ProjectID: actor.ProjectID, StableID: reference.SourceID, IdentitySnapshot: identity,
		LiveRoute: "/source/" + reference.SourceID, CaptureAuthorization: authorization,
	}, nil
}

func evidenceReadPostgresSubjects(t *testing.T, targetCurrentScope *recordauth.VisibilityScope) records.SubjectAdapterRegistry {
	t.Helper()
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		evidenceReadPostgresSubjectAdapter{kind: records.SubjectKindVPS},
		evidenceReadPostgresSubjectAdapter{kind: records.SubjectKindMonitoringInstance},
		evidenceReadPostgresSubjectAdapter{kind: records.SubjectKindTarget, currentScope: targetCurrentScope},
	})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	return registry
}

func evidenceReadPostgresSourceKind(kind records.SubjectKind) recordauth.SourceKind {
	return recordauth.SourceKind(kind)
}

func mustEvidenceReadPostgresVisibility(projectID recordauth.ProjectID) recordauth.VisibilityScope {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: projectID, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		panic(err)
	}
	return visibility
}

type evidenceReadPostgresKind struct{ descriptor evidence.Descriptor }

func (kind evidenceReadPostgresKind) Descriptor() evidence.Descriptor { return kind.descriptor }
func (evidenceReadPostgresKind) ValidateSelection(context.Context, evidence.ActorScope, evidence.Selection) error {
	return nil
}
func (evidenceReadPostgresKind) PreviewCapture(context.Context, evidence.ActorScope, evidence.Selection) (evidence.Preview, error) {
	return evidence.Preview{}, nil
}
func (evidenceReadPostgresKind) Capture(context.Context, evidence.ActorScope, evidence.Intent) (evidence.CanonicalSnapshot, error) {
	return evidence.CanonicalSnapshot{}, nil
}
func (evidenceReadPostgresKind) Authorize(context.Context, evidence.ActorScope, evidence.Selection) (evidence.AuthorizationScope, error) {
	return evidence.AuthorizationScope{}, nil
}
func (kind evidenceReadPostgresKind) Summarize(evidence.CanonicalSnapshot) evidence.Summary {
	return evidence.Summary{
		Key: kind.descriptor.Key, RendererVersion: kind.descriptor.Conformance.RendererVersion,
		Title: "Monitoring probe evidence", SearchText: "monitoring probe evidence",
		ReadModel: map[string]any{"version": "monitoring.probe/v2", "status": "complete"},
	}
}
func (kind evidenceReadPostgresKind) Compare(evidence.CanonicalSnapshot, evidence.CanonicalSnapshot, evidence.Alignment) evidence.Comparison {
	return evidence.Comparison{Key: kind.descriptor.Key, Compatible: true}
}
func (kind evidenceReadPostgresKind) Export(evidence.CanonicalSnapshot, evidence.ExportMode) evidence.ExportMaterial {
	return evidence.ExportMaterial{Key: kind.descriptor.Key, MediaType: "application/json", Filename: "evidence.json", Bytes: []byte(`{"status":"complete"}`)}
}

func evidenceReadPostgresRegistry(t *testing.T) evidence.Registry {
	t.Helper()
	registry, err := evidence.NewRegistry([]evidence.Kind{evidenceReadPostgresKind{descriptor: storeEvidenceParticipantDescriptor()}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

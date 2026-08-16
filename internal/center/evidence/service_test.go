package evidence

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"houfeng/internal/center/recordauth"
)

func TestEvidenceServiceCapturePreviewPersistsCompleteServerOwnedBinding(t *testing.T) {
	kind, fixture := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	intents := &captureIntentStoreStub{}
	capacity := mustTestCapacityEnforcer(t, string(fixture.Actor.ProjectID), 0, kind.preview.EstimatedCanonicalBytes*2, 50)
	service, err := NewService(registry, intents, &snapshotReadSourceStub{}, capacity)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.CapturePreview(context.Background(), CapturePreviewRequest{
		Actor: fixture.Actor, RecordID: "rec_service", SnapshotID: "evs_service", Selection: fixture.Selection,
	})
	if err != nil {
		t.Fatalf("CapturePreview() error = %v", err)
	}
	if result.RecordID != "rec_service" || result.SnapshotID != "evs_service" || intents.calls != 1 ||
		intents.recordID != result.RecordID || intents.snapshotID != result.SnapshotID ||
		intents.intent.ID != result.Preview.IntentID || intents.intent.PreviewDigest == [32]byte{} ||
		!reflect.DeepEqual(intents.preview, result.Preview) {
		t.Fatalf("capture result/persistence = %#v / %#v", result, intents)
	}
	if result.Preview.QuotaOutcome.Status != QuotaWarning {
		t.Fatalf("preview quota = %#v, want warning", result.Preview.QuotaOutcome)
	}

	capacity.source.(*projectCapacitySourceStub).usage.LogicalSnapshotBytes = kind.preview.EstimatedCanonicalBytes * 2
	capacity.source.(*projectCapacitySourceStub).usage.LogicalSnapshotCount = 1
	capacity.source.(*projectCapacitySourceStub).usage.PhysicalPayloadCount = 1
	capacity.source.(*projectCapacitySourceStub).usage.PhysicalCanonicalBytes = kind.preview.EstimatedCanonicalBytes
	capacity.source.(*projectCapacitySourceStub).usage.PhysicalCompressedBytes = kind.preview.EstimatedCanonicalBytes
	blocked, err := service.CapturePreview(context.Background(), CapturePreviewRequest{
		Actor: fixture.Actor, RecordID: "rec_service", SnapshotID: "evs_service3", Selection: fixture.Selection,
	})
	if err != nil || blocked.Preview.QuotaOutcome.Status != QuotaExceeded || intents.calls != 1 {
		t.Fatalf("CapturePreview(exceeded) = %#v, %v; persisted calls=%d", blocked, err, intents.calls)
	}
	capacity.source.(*projectCapacitySourceStub).err = errors.New("capacity read failed")
	unavailable, err := service.CapturePreview(context.Background(), CapturePreviewRequest{
		Actor: fixture.Actor, RecordID: "rec_service", SnapshotID: "evs_service4", Selection: fixture.Selection,
	})
	if err != nil || unavailable.Preview.QuotaOutcome.Status != QuotaUnavailable || intents.calls != 1 {
		t.Fatalf("CapturePreview(unavailable) = %#v, %v; persisted calls=%d", unavailable, err, intents.calls)
	}

	capacity.source.(*projectCapacitySourceStub).err = nil
	capacity.source.(*projectCapacitySourceStub).usage.LogicalSnapshotCount = 0
	capacity.source.(*projectCapacitySourceStub).usage.LogicalSnapshotBytes = 0
	capacity.source.(*projectCapacitySourceStub).usage.PhysicalPayloadCount = 0
	capacity.source.(*projectCapacitySourceStub).usage.PhysicalCanonicalBytes = 0
	capacity.source.(*projectCapacitySourceStub).usage.PhysicalCompressedBytes = 0
	kind.previewErr = ErrSourceUnstable
	if _, err := service.CapturePreview(context.Background(), CapturePreviewRequest{
		Actor: fixture.Actor, RecordID: "rec_service", SnapshotID: "evs_service2", Selection: fixture.Selection,
	}); !errors.Is(err, ErrSourceUnstable) {
		t.Fatalf("CapturePreview(unstable source) error = %v", err)
	}
}

func TestEvidenceServiceReadEnforcesCurrentRecordAndSourceIntersectionBeforeKindSummary(t *testing.T) {
	kind, fixture := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	envelope := kind.snapshot.Envelope()
	state := SnapshotReadState{
		RecordID: "rec_service", SnapshotID: "evs_service", Envelope: envelope,
		CanonicalPayload: kind.snapshot.Bytes(), SourceAuthorization: envelope.Authorization, SourceAvailable: true,
		RecordScope: recordauth.ResourceScope{
			Version: recordauth.ResourceScopeVersionV1, ProjectID: fixture.Actor.ProjectID,
			Visibility: envelope.Authorization.CaptureScope,
			Sources:    []recordauth.SourceAuthorization{envelope.Authorization},
		},
	}
	source := &snapshotReadSourceStub{state: state}
	service, err := NewService(registry, &captureIntentStoreStub{}, source,
		mustTestCapacityEnforcer(t, string(fixture.Actor.ProjectID), 0, 1_000, 80))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.ReadSnapshot(context.Background(), ReadSnapshotRequest{Actor: fixture.Actor, SnapshotID: state.SnapshotID})
	if err != nil || result.RecordID != state.RecordID || result.Summary.ReadModel == nil || !result.SourceAvailable {
		t.Fatalf("ReadSnapshot() = %#v, %v", result, err)
	}

	restricted, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindRestricted,
		ProjectID: fixture.Actor.ProjectID, AllowedGroupIDs: []string{"rag_hidden"},
		PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 2,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	current, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: envelope.Authorization.Kind,
		SourceID: envelope.Authorization.SourceID, State: recordauth.SourceStateLive,
		CaptureScope: envelope.Authorization.CaptureScope, CurrentScope: &restricted,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	source.state.SourceAuthorization = current
	if _, err := service.ReadSnapshot(context.Background(), ReadSnapshotRequest{
		Actor: fixture.Actor, SnapshotID: state.SnapshotID,
	}); !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("ReadSnapshot(revoked source) error = %v", err)
	}

	source.state.SourceAuthorization = envelope.Authorization
	kind.summary = &Summary{
		Key: kind.descriptor.Key, RendererVersion: kind.descriptor.Conformance.RendererVersion,
		Title: "host evidence", SearchText: "host evidence",
		ReadModel: map[string]any{"stdout": "must not reach the response"},
	}
	if _, err := service.ReadSnapshot(context.Background(), ReadSnapshotRequest{
		Actor: fixture.Actor, SnapshotID: state.SnapshotID,
	}); !errors.Is(err, ErrEvidenceServiceUnavailable) {
		t.Fatalf("ReadSnapshot(unsafe summary) error = %v, want ErrEvidenceServiceUnavailable", err)
	}

	kind.summary = &Summary{
		Key: kind.descriptor.Key, RendererVersion: kind.descriptor.Conformance.RendererVersion,
		Title: "host evidence", SearchText: "host evidence", ReadModel: map[string]any{"status": "ok"},
	}
	if _, err := service.ReadSnapshot(context.Background(), ReadSnapshotRequest{
		Actor: fixture.Actor, SnapshotID: state.SnapshotID,
	}); !errors.Is(err, ErrEvidenceServiceUnavailable) {
		t.Fatalf("ReadSnapshot(unversioned summary) error = %v, want ErrEvidenceServiceUnavailable", err)
	}
}

func mustTestCapacityEnforcer(
	t *testing.T,
	projectID string,
	logicalBytes uint64,
	limitBytes uint64,
	warningPercent uint8,
) *CapacityEnforcer {
	t.Helper()
	source := &projectCapacitySourceStub{usage: ProjectCapacityUsage{ProjectID: projectID}}
	if logicalBytes > 0 {
		source.usage.LogicalSnapshotCount = 1
		source.usage.LogicalSnapshotBytes = logicalBytes
		source.usage.PhysicalPayloadCount = 1
		source.usage.PhysicalCanonicalBytes = logicalBytes
		source.usage.PhysicalCompressedBytes = logicalBytes
	}
	enforcer, err := NewCapacityEnforcer(CapacityPolicy{
		ProjectLimitBytes: limitBytes,
		WarningPercent:    warningPercent,
	}, source)
	if err != nil {
		t.Fatalf("NewCapacityEnforcer() error = %v", err)
	}
	return enforcer
}

type captureIntentStoreStub struct {
	calls                int
	recordID, snapshotID string
	intent               Intent
	preview              Preview
}

func (store *captureIntentStoreStub) PersistCaptureIntent(
	_ context.Context,
	recordID string,
	snapshotID string,
	intent Intent,
	preview Preview,
) error {
	store.calls++
	store.recordID, store.snapshotID, store.intent, store.preview = recordID, snapshotID, intent, preview
	return nil
}

type snapshotReadSourceStub struct {
	state SnapshotReadState
	err   error
}

func (source *snapshotReadSourceStub) LoadEvidenceSnapshot(
	_ context.Context,
	_ ActorScope,
	_ string,
) (SnapshotReadState, error) {
	return source.state, source.err
}

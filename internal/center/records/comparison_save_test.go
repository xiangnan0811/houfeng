package records

import (
	"context"
	"crypto/sha256"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
)

func TestSaveComparisonRecordIdempotentRetryKeepsFingerprint(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordCreated)
	preparation := mustSaveComparisonRecordPreparation(t, request.RecordID)
	request.EvidencePreparation = preparation
	request.IdempotencyKey = "comparison-save-retry"

	adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	store := &revisionCommitStoreStub{result: RevisionCommitResult{RecordID: request.RecordID, RevisionNo: 1, Created: true}}
	service, err := NewRevisionService(registry, &currentRecordAuthorizationSourceStub{}, store)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}

	if _, err := service.SaveRevision(context.Background(), request); err != nil {
		t.Fatalf("SaveRevision() error = %v", err)
	}
	first, err := store.command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("first fingerprint: %v", err)
	}
	if _, err := service.SaveRevision(context.Background(), request); err != nil {
		t.Fatalf("SaveRevision(retry) error = %v", err)
	}
	second, err := store.command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("retry fingerprint: %v", err)
	}
	if first != second || !reflect.DeepEqual(store.command.EvidencePreparation.SnapshotIDs(), preparation.SnapshotIDs()) {
		t.Fatalf("idempotent retry drifted fingerprint or snapshot IDs")
	}
}

func mustSaveComparisonRecordPreparation(t *testing.T, recordID string) evidence.RevisionPreparation {
	t.Helper()
	source := mustSaveComparisonSourceSnapshot(t)
	copy, err := evidence.NewPreparedComparisonCopy(recordID, "evs_comparisoncopy", "evs_comparisonsource", source)
	if err != nil {
		t.Fatalf("NewPreparedComparisonCopy() error = %v", err)
	}
	result, err := evidence.NewPreparedComparisonResult(recordID, "evs_comparisonresult", mustSaveComparisonResultSnapshot(t))
	if err != nil {
		t.Fatalf("NewPreparedComparisonResult() error = %v", err)
	}
	prepared, err := evidence.NewRevisionPreparationFromComparisonSave(recordID, evidence.ComparisonSavePreparation{
		Token: "cmp1.valid.payload.mac", Copies: []evidence.PreparedComparisonCopy{copy}, Result: result,
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparationFromComparisonSave() error = %v", err)
	}
	return prepared
}

func mustSaveComparisonResultSnapshot(t *testing.T) evidence.CanonicalSnapshot {
	t.Helper()
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	source := mustSaveComparisonSourceSnapshot(t)
	envelope := source.Envelope()
	envelope.Key = evidence.ComparisonResultV1Key()
	envelope.ProducerVersion = "comparison-result/v1"
	envelope.CalculationVersion = evidence.ComparisonCalculationVersion
	envelope.Units = evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "comparison result metadata"}
	envelope.ActualPrecision = evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	envelope.BucketWidth = evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	envelope.Quality = evidence.Quality{Status: evidence.QualityComplete, SampleCount: 2}
	envelope.Redaction = nil
	envelope.CanonicalHash = [32]byte{}
	envelope.CanonicalSize = 0
	payload := map[string]any{
		"version": "comparison_result/v1", "baseline_index": 0, "alignment": string(evidence.CoverageActual),
		"requested_from": "2026-08-10T11:00:00Z", "requested_to": "2026-08-10T12:00:00Z",
		"tolerance_seconds": int64(60), "digest": "abababababababababababababababababababababababababababababababab",
		"registry_version": "evidence-kinds/v1", "calculation_version": evidence.ComparisonCalculationVersion,
		"items": []any{map[string]any{
			"original_snapshot_id": "evs_comparisonsource", "copied_snapshot_id": "evs_comparisoncopy",
			"hash": "1111111111111111111111111111111111111111111111111111111111111111",
			"kind": evidence.IPQualityReportV1Key().String(), "revision_context": string(evidence.RevisionContextNotApplicable),
		}},
		"warnings": []any{}, "system_differences": []any{},
		"available_kinds": []any{evidence.IPQualityReportV1Key().String()},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

func mustSaveComparisonSourceSnapshot(t *testing.T) evidence.CanonicalSnapshot {
	t.Helper()
	authorization := mustRecordSourceAuthorization(t, mustRecordVisibility(t))
	previewedAt := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: previewedAt.Add(-time.Hour), End: previewedAt}
	key := evidence.IPQualityReportV1Key()
	descriptor := evidence.Descriptor{
		Key: key,
		Fields: []evidence.FieldDefinition{
			{Path: "metric_name", Sensitivity: evidence.SensitivityNormal},
			{Path: "metric_value", Sensitivity: evidence.SensitivityNormal},
		},
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         "renderer.v1",
			MaxCanonicalBytes:       evidence.MaxCanonicalPayloadBytes,
		},
	}
	envelope := evidence.SnapshotEnvelope{
		Key:           key,
		Subject:       evidence.IdentitySnapshot{Type: string(authorization.Kind), ID: authorization.SourceID, Fields: map[string]string{"display_name": "Source"}},
		Source:        evidence.IdentitySnapshot{Type: string(authorization.Kind), ID: authorization.SourceID, Fields: map[string]string{"display_name": "Source"}},
		Authorization: authorization, RequestedWindow: window, ActualWindow: window,
		ObservedAt: previewedAt, CapturedAt: previewedAt.Add(time.Minute), ReferencedAt: previewedAt.Add(2 * time.Minute),
		SourceRevision: "revision-1", SourceWatermark: "watermark-1", SourceDigest: sha256.Sum256([]byte("source")),
		ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units:           evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"score": "ratio"}},
		Quality:         evidence.Quality{Status: evidence.QualityComplete, SampleCount: 1},
		Sensitivity:     evidence.SensitivityNormal,
		ActualPrecision: evidence.DurationSemantics{Applicable: false, Reason: "discrete authoritative facts"},
		BucketWidth:     evidence.DurationSemantics{Applicable: false, Reason: "discrete authoritative facts"},
		QuotaOutcome:    evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:       evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "score", "metric_value": "1"}, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

func TestSaveComparisonRecordPreparationRejectsClientOwnedSnapshotIDs(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordCreated)
	request.EvidencePreparation = mustSaveComparisonRecordPreparation(t, request.RecordID)
	request.Values.EvidenceSnapshotIDs = []string{"evs_clientsupplied"}
	adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	store := &revisionCommitStoreStub{}
	service, err := NewRevisionService(registry, &currentRecordAuthorizationSourceStub{}, store)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}
	if _, err := service.SaveRevision(context.Background(), request); err == nil {
		t.Fatal("SaveRevision() accepted client-owned evidence snapshot IDs")
	}
	if store.calls != 0 {
		t.Fatalf("client-owned IDs reached store: %d", store.calls)
	}
}

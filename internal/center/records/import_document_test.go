package records

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
)

func TestImportDocumentRejectsImportedAuthorizationAndUsesLocalActor(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	var saved RevisionSaveRequest
	revisions := &recordApplicationRevisionStub{save: func(_ context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
		saved = request
		return RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_imported", Created: true}, nil
	}}
	application, err := NewApplication(
		&recordApplicationReadStub{},
		revisions,
		&recordApplicationLifecycleStub{},
		&recordApplicationDraftStub{},
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}
	if _, err := application.ImportDocument(context.Background(), ImportDocumentRequest{
		Actor: actor, RecordID: "rec_imported01", Title: "Disk notes",
		BodyMarkdown: "# Body\n", IdempotencyKey: "import-1",
		ImportedAuthorization: "admin",
	}); !errors.Is(err, ErrUntrustedImportIdentity) {
		t.Fatalf("ImportDocument(auth) error = %v, want ErrUntrustedImportIdentity", err)
	}
	got, err := application.ImportDocument(context.Background(), ImportDocumentRequest{
		Actor: actor, RecordID: "rec_imported01", Title: "Disk notes",
		BodyMarkdown: "# Body\n", IdempotencyKey: "import-1",
	})
	if err != nil {
		t.Fatalf("ImportDocument() error = %v", err)
	}
	if got.RecordID != "rec_imported01" || got.RevisionID != "rrv_imported" {
		t.Fatalf("ImportDocument() = %#v", got)
	}
	if saved.Values.OwnerID != actor.UserID || saved.Values.AuthorID != actor.UserID ||
		saved.Values.RecordType != RecordTypeNote || saved.ActivityKind != DomainActivityRecordCreated {
		t.Fatalf("SaveRevision() = %#v", saved)
	}
}

func TestImportDocumentsFinishingCarriesEvidencePreparation(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustImportedComparisonSnapshot(t, kind)
	imported, err := evidence.NewPreparedImportedSnapshot("rec_imported02", "evs_imported02", snapshot)
	if err != nil {
		t.Fatalf("NewPreparedImportedSnapshot() error = %v", err)
	}
	preparation, err := evidence.NewRevisionPreparation("rec_imported02", evidence.RevisionPreparationValues{
		Imported:           []evidence.PreparedImportedSnapshot{imported},
		OrderedSnapshotIDs: []string{imported.SnapshotID()},
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	var saved RevisionSaveRequest
	revisions := &recordApplicationRevisionStub{save: func(_ context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
		saved = request
		return RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_imported", Created: true}, nil
	}}
	application, err := NewApplication(
		&recordApplicationReadStub{},
		revisions,
		&recordApplicationLifecycleStub{},
		&recordApplicationDraftStub{},
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}
	written, err := application.ImportDocumentsFinishing(context.Background(), []ImportDocumentRequest{{
		Actor: actor, RecordID: "rec_imported02", Title: "Disk notes",
		BodyMarkdown: "# Body\n", IdempotencyKey: "import-evidence",
		EvidencePreparation: preparation,
	}}, RevisionCommitFinish{
		OriginKind: "import", OriginDigest: [32]byte{1}, ImportJobID: "rij_imported02",
		JobLockVersion: 1, ActorID: actor.UserID,
	})
	if err != nil {
		t.Fatalf("ImportDocumentsFinishing() error = %v", err)
	}
	if len(written) != 1 || saved.RecordID != "rec_imported02" {
		t.Fatalf("ImportDocumentsFinishing() = %#v saved=%#v", written, saved)
	}
	if !reflect.DeepEqual(saved.EvidencePreparation.SnapshotIDs(), []string{"evs_imported02"}) {
		t.Fatalf("EvidencePreparation snapshot IDs = %#v", saved.EvidencePreparation.SnapshotIDs())
	}
	if len(saved.EvidencePreparation.Imported()) != 1 ||
		saved.EvidencePreparation.Imported()[0].Snapshot().Hash() != snapshot.Hash() {
		t.Fatalf("Imported() = %#v", saved.EvidencePreparation.Imported())
	}
	if len(saved.Values.EvidenceSnapshotIDs) != 0 {
		t.Fatalf("client-owned EvidenceSnapshotIDs = %#v", saved.Values.EvidenceSnapshotIDs)
	}
}

func TestImportDocumentsFinishingCarriesAttachmentIDs(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	payload := []byte("hello notes\n")
	digest := sha256.Sum256(payload)
	item := attachments.ImportedAvailableAttachment{
		AttachmentID:     "att_imported00001",
		DisplayName:      "notes.txt",
		MediaType:        "text/plain",
		LogicalSizeBytes: int64(len(payload)),
		Object: attachments.ObjectVersion{
			Key: "sha256/" + hex.EncodeToString(digest[:]), VersionID: "import-v1",
			SHA256: digest, SizeBytes: int64(len(payload)),
		},
		BackendKind: attachments.BackendKindLocal,
		CreatedBy:   actor.UserID,
	}
	var saved RevisionSaveRequest
	revisions := &recordApplicationRevisionStub{save: func(_ context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
		saved = request
		return RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_imported", Created: true}, nil
	}}
	application, err := NewApplication(
		&recordApplicationReadStub{},
		revisions,
		&recordApplicationLifecycleStub{},
		&recordApplicationDraftStub{},
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}
	written, err := application.ImportDocumentsFinishing(context.Background(), []ImportDocumentRequest{{
		Actor: actor, RecordID: "rec_imported03", Title: "Disk notes",
		BodyMarkdown: "# Body\n", IdempotencyKey: "import-attachments",
		AttachmentIDs: []string{item.AttachmentID}, ImportedAttachments: []attachments.ImportedAvailableAttachment{item},
	}}, RevisionCommitFinish{
		OriginKind: "import", OriginDigest: [32]byte{2}, ImportJobID: "rij_imported03",
		JobLockVersion: 1, ActorID: actor.UserID,
	})
	if err != nil {
		t.Fatalf("ImportDocumentsFinishing() error = %v", err)
	}
	if len(written) != 1 || !reflect.DeepEqual(saved.Values.AttachmentIDs, []string{item.AttachmentID}) {
		t.Fatalf("AttachmentIDs = %#v written=%#v", saved.Values.AttachmentIDs, written)
	}
	if len(saved.ImportedAttachments) != 1 || saved.ImportedAttachments[0].AttachmentID != item.AttachmentID {
		t.Fatalf("ImportedAttachments = %#v", saved.ImportedAttachments)
	}
}

func mustImportedComparisonSnapshot(t *testing.T, kind *evidence.ComparisonResultKind) evidence.CanonicalSnapshot {
	t.Helper()
	authorization := mustRecordSourceAuthorization(t, mustRecordVisibility(t))
	window := evidence.TimeWindow{
		Start: time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
	envelope := evidence.SnapshotEnvelope{
		Key: evidence.ComparisonResultV1Key(),
		Subject: evidence.IdentitySnapshot{
			Type: "target", ID: authorization.SourceID,
			Fields: map[string]string{"display_name": "edge probe"},
		},
		Source: evidence.IdentitySnapshot{
			Type: string(authorization.Kind), ID: authorization.SourceID,
			Fields: map[string]string{"display_name": "edge probe"},
		},
		Authorization:      authorization,
		SourceDigest:       sha256.Sum256([]byte("source")),
		RequestedWindow:    window,
		ActualWindow:       window,
		ObservedAt:         window.End,
		CapturedAt:         window.End.Add(time.Minute),
		ReferencedAt:       window.End.Add(2 * time.Minute),
		SourceRevision:     "revision-1",
		SourceWatermark:    "watermark-1",
		ProducerVersion:    "comparison-result/v1",
		CalculationVersion: evidence.ComparisonCalculationVersion,
		Units:              evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "comparison result metadata"},
		Quality:            evidence.Quality{Status: evidence.QualityComplete, SampleCount: 2},
		Sensitivity:        evidence.SensitivityNormal,
		ActualPrecision:    evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"},
		BucketWidth:        evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"},
		QuotaOutcome:       evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention: evidence.RetentionSemantics{
			Immutable: true, Scope: evidence.RetentionScopeRecordRevision,
			SourceDeletion: evidence.SourceDeletionSnapshotRetained,
		},
	}
	payload := map[string]any{
		"version":             "comparison_result/v1",
		"baseline_index":      0,
		"alignment":           string(evidence.CoverageActual),
		"requested_from":      "2026-08-10T11:00:00Z",
		"requested_to":        "2026-08-10T12:00:00Z",
		"tolerance_seconds":   60,
		"digest":              "abababababababababababababababababababababababababababababababab",
		"registry_version":    "evidence-kinds/v1",
		"calculation_version": evidence.ComparisonCalculationVersion,
		"items": []any{
			map[string]any{
				"original_snapshot_id": "evs_left",
				"copied_snapshot_id":   "evs_leftcopy",
				"hash":                 "1111111111111111111111111111111111111111111111111111111111111111",
				"kind":                 evidence.CommandAuditV1Key().String(),
				"revision_context":     string(evidence.RevisionContextBound),
			},
		},
		"warnings":           []any{},
		"system_differences": []any{},
		"available_kinds":    []any{evidence.CommandAuditV1Key().String()},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

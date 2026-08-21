package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestRecordEvidenceRevisionParticipantProductionFileContainsNoPanic(t *testing.T) {
	t.Parallel()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve participant test source")
	}
	productionFile := filepath.Join(filepath.Dir(testFile), "record_evidence_participant.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), productionFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", productionFile, err)
	}
	var panicCalls int
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == "panic" {
			panicCalls++
		}
		return true
	})
	if panicCalls != 0 {
		t.Fatalf("%s contains %d panic call(s); production store code must fail closed", productionFile, panicCalls)
	}
}

func TestRecordEvidenceRevisionParticipantConsumesCaptureAndWritesOrderedReference(t *testing.T) {
	prepared := storePreparedEvidenceCapture(t, "rec_evidenceparticipant", "evs_captureparticipant", "evi_aaaaaaaaaaaaaaaaaaaaaaaa", time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC))
	preparation := storeEvidenceRevisionPreparation(t, prepared.RecordID(), []evidence.PreparedCapture{prepared}, nil, []string{prepared.SnapshotID()})
	tx := newFakeRecordEvidenceParticipantTx(t, prepared)
	participant := NewRecordEvidenceRevisionParticipant()

	err := participant.ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID: prepared.RecordID(), RevisionID: "rrv_evidenceparticipant",
		},
		Input:               recordsPostgresCompleteRevisionInputWithEvidence(t, "Evidence participant", preparation.SnapshotIDs()),
		EvidencePreparation: preparation,
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if participant.Name() != "evidence" {
		t.Fatalf("participant name = %q, want evidence", participant.Name())
	}
	if tx.queryRowCalls != 3 || tx.capacityLockCalls != 1 || !reflect.DeepEqual(tx.execKinds, []string{"snapshot", "reference"}) {
		t.Fatalf("participant calls = query:%d locks:%d exec:%#v, want capacity check then atomic consume/snapshot/reference", tx.queryRowCalls, tx.capacityLockCalls, tx.execKinds)
	}
	if tx.referenceOrdinal != 0 || tx.referenceSnapshotID != prepared.SnapshotID() {
		t.Fatalf("revision reference = ordinal:%d snapshot:%q", tx.referenceOrdinal, tx.referenceSnapshotID)
	}
	if !strings.Contains(tx.querySQL, "delete from public.evidence_capture_intents") ||
		!strings.Contains(tx.querySQL, "valid_until > transaction_timestamp()") ||
		!strings.Contains(tx.querySQL, "returning") {
		t.Fatalf("intent consume SQL is not a live DELETE ... RETURNING:\n%s", tx.querySQL)
	}
}

func TestRecordEvidenceRevisionParticipantRechecksCapacityBeforeConsumingIntent(t *testing.T) {
	prepared := storePreparedEvidenceCapture(
		t, "rec_evidencecapacity", "evs_evidencecapacity", "evi_dddddddddddddddddddddddd",
		time.Date(2026, 8, 14, 8, 30, 0, 0, time.UTC),
	)
	preparation := storeEvidenceRevisionPreparation(
		t, prepared.RecordID(), []evidence.PreparedCapture{prepared}, nil, []string{prepared.SnapshotID()},
	)
	tx := newFakeRecordEvidenceParticipantTx(t, prepared)
	tx.usedLogicalBytes = int64(evidence.DefaultProjectEvidenceCapacityBytes)

	err := NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{RecordID: prepared.RecordID(), RevisionID: "rrv_evidencecapacity"},
		Input: recordsPostgresCompleteRevisionInputWithEvidence(
			t, "Evidence capacity", preparation.SnapshotIDs(),
		),
		EvidencePreparation: preparation,
	})
	if !errors.Is(err, evidence.ErrPreviewStale) || !errors.Is(err, ErrEvidencePersistenceConflict) {
		t.Fatalf("ApplyRevision() error = %v, want stale-preview persistence conflict", err)
	}
	if tx.capacityLockCalls != 1 || tx.queryRowCalls != 2 {
		t.Fatalf("capacity checks = locks:%d queries:%d, want project read/lock/usage read", tx.capacityLockCalls, tx.queryRowCalls)
	}
	if len(tx.execKinds) != 0 || strings.Contains(tx.querySQL, "delete from public.evidence_capture_intents") {
		t.Fatalf("quota denial consumed intent or wrote logical rows: query=%q exec=%#v", tx.querySQL, tx.execKinds)
	}
}

func TestRecordEvidenceRevisionParticipantCapacityPolicyIsExplicitAndValidated(t *testing.T) {
	policy := evidence.CapacityPolicy{ProjectLimitBytes: 4096, WarningPercent: 75}
	participant, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(policy)
	if err != nil || participant == nil {
		t.Fatalf("NewRecordEvidenceRevisionParticipantWithCapacityPolicy() = (%#v, %v)", participant, err)
	}
	if _, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(evidence.CapacityPolicy{}); !errors.Is(err, evidence.ErrInvalidCapacityPolicy) {
		t.Fatalf("invalid policy error = %v, want ErrInvalidCapacityPolicy", err)
	}
}

func TestRecordEvidenceRevisionParticipantAcceptsCanonicalWarningAtExactBoundary(t *testing.T) {
	prepared := storePreparedEvidenceCaptureWithQuota(
		t, "rec_evidencewarning", "evs_evidencewarning", "evi_eeeeeeeeeeeeeeeeeeeeeeee",
		time.Date(2026, 8, 14, 8, 45, 0, 0, time.UTC),
		evidence.QuotaOutcome{Status: evidence.QuotaWarning, Reason: "project evidence quota warning threshold reached"},
	)
	policy := evidence.CapacityPolicy{ProjectLimitBytes: prepared.Snapshot().Size(), WarningPercent: 80}
	participant, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(policy)
	if err != nil {
		t.Fatalf("NewRecordEvidenceRevisionParticipantWithCapacityPolicy() error = %v", err)
	}
	preparation := storeEvidenceRevisionPreparation(
		t, prepared.RecordID(), []evidence.PreparedCapture{prepared}, nil, []string{prepared.SnapshotID()},
	)
	tx := newFakeRecordEvidenceParticipantTx(t, prepared)
	err = participant.ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{RecordID: prepared.RecordID(), RevisionID: "rrv_evidencewarning"},
		Input: recordsPostgresCompleteRevisionInputWithEvidence(
			t, "Evidence warning", preparation.SnapshotIDs(),
		),
		EvidencePreparation: preparation,
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if tx.capacityLockCalls != 1 || tx.queryRowCalls != 3 ||
		!reflect.DeepEqual(tx.execKinds, []string{"snapshot", "reference"}) {
		t.Fatalf("warning capture calls = locks:%d queries:%d exec:%#v", tx.capacityLockCalls, tx.queryRowCalls, tx.execKinds)
	}
}

func TestRecordEvidenceRevisionParticipantRejectsMissingAndPersistedIntentDriftBeforeSnapshotInsert(t *testing.T) {
	prepared := storePreparedEvidenceCapture(t, "rec_evidencedrift", "evs_capturedrift", "evi_bbbbbbbbbbbbbbbbbbbbbbbb", time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC))
	preparation := storeEvidenceRevisionPreparation(t, prepared.RecordID(), []evidence.PreparedCapture{prepared}, nil, []string{prepared.SnapshotID()})
	base := fakeRecordEvidenceIntentRowFromPrepared(t, prepared)
	tests := []struct {
		name   string
		mutate func(*fakeRecordEvidenceIntentRow)
	}{
		{name: "missing", mutate: func(row *fakeRecordEvidenceIntentRow) { row.err = pgx.ErrNoRows }},
		{name: "record", mutate: func(row *fakeRecordEvidenceIntentRow) { row.recordID = "rec_other" }},
		{name: "kind", mutate: func(row *fakeRecordEvidenceIntentRow) { row.kind = "monitoring.host" }},
		{name: "schema", mutate: func(row *fakeRecordEvidenceIntentRow) { row.schemaVersion++ }},
		{name: "preview digest", mutate: func(row *fakeRecordEvidenceIntentRow) { row.previewDigest[0] ^= 0xff }},
		{name: "source digest", mutate: func(row *fakeRecordEvidenceIntentRow) { row.sourceDigest[0] ^= 0xff }},
		{name: "selection", mutate: func(row *fakeRecordEvidenceIntentRow) {
			var selection evidence.Selection
			if err := json.Unmarshal(row.selectionJSON, &selection); err != nil {
				t.Fatal(err)
			}
			selection.Metrics = append(selection.Metrics, "packet_loss_pct")
			row.selectionJSON, _ = json.Marshal(selection)
		}},
		{name: "preview", mutate: func(row *fakeRecordEvidenceIntentRow) {
			var preview evidence.Preview
			if err := json.Unmarshal(row.previewJSON, &preview); err != nil {
				t.Fatal(err)
			}
			preview.ProducerVersion = "drifted"
			row.previewJSON, _ = json.Marshal(preview)
		}},
		{name: "snapshot", mutate: func(row *fakeRecordEvidenceIntentRow) { row.snapshotID = "evs_other" }},
		{name: "estimated size", mutate: func(row *fakeRecordEvidenceIntentRow) { row.estimatedSize++ }},
		{name: "created time", mutate: func(row *fakeRecordEvidenceIntentRow) { row.createdAt = row.createdAt.Add(time.Microsecond) }},
		{name: "expiry", mutate: func(row *fakeRecordEvidenceIntentRow) { row.validUntil = row.validUntil.Add(time.Microsecond) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := base.clone()
			tt.mutate(&row)
			tx := &fakeRecordEvidenceParticipantTx{intentRow: row}
			err := NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
				Result: records.RevisionCommitResult{
					RecordID: prepared.RecordID(), RevisionID: "rrv_evidencedrift",
				},
				Input:               recordsPostgresCompleteRevisionInputWithEvidence(t, "Evidence drift", preparation.SnapshotIDs()),
				EvidencePreparation: preparation,
			})
			if !errors.Is(err, ErrEvidencePersistenceConflict) {
				t.Fatalf("ApplyRevision() error = %v, want ErrEvidencePersistenceConflict", err)
			}
			if len(tx.execKinds) != 0 {
				t.Fatalf("participant wrote after intent rejection: %#v", tx.execKinds)
			}
		})
	}
}

func TestRecordEvidenceRevisionParticipantValidatesExistingReferenceWithoutPayloadWrite(t *testing.T) {
	actor := storeEvidenceParticipantActor(t)
	reference := storePreparedEvidenceReference(t, actor, "rec_evidencereference", "evs_existingreference")
	preparation := storeEvidenceRevisionPreparation(t, reference.RecordID(), nil, []evidence.PreparedReference{reference}, []string{reference.SnapshotID()})
	tx := &fakeRecordEvidenceParticipantTx{reference: reference}

	err := NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID: reference.RecordID(), RevisionID: "rrv_evidencereference",
		},
		Input:               recordsPostgresCompleteRevisionInputWithEvidence(t, "Evidence reference", preparation.SnapshotIDs()),
		EvidencePreparation: preparation,
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if tx.queryRowCalls != 1 || !strings.Contains(tx.querySQL, "from public.evidence_snapshots") {
		t.Fatalf("existing reference validation query = %q", tx.querySQL)
	}
	if !reflect.DeepEqual(tx.execKinds, []string{"reference"}) {
		t.Fatalf("existing reference writes = %#v, want revision reference only", tx.execKinds)
	}
	if tx.capacityLockCalls != 0 {
		t.Fatalf("existing reference acquired capacity lock %d times", tx.capacityLockCalls)
	}
	for _, sql := range tx.execSQL {
		if strings.Contains(sql, "evidence_payloads") || strings.Contains(sql, "evidence_snapshots") {
			t.Fatalf("existing reference duplicated payload or snapshot:\n%s", sql)
		}
	}
}

func TestRecordEvidenceRevisionParticipantRejectsSnapshotTimestampPostgresWouldRoundBeforeIntentConsume(t *testing.T) {
	prepared := storePreparedEvidenceCaptureWithSnapshotTimeOffset(
		t,
		"rec_evidenceprecision",
		"evs_evidenceprecision",
		"evi_cccccccccccccccccccccccc",
		time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
		time.Nanosecond,
	)
	preparation := storeEvidenceRevisionPreparation(
		t, prepared.RecordID(), []evidence.PreparedCapture{prepared}, nil, []string{prepared.SnapshotID()},
	)
	tx := newFakeRecordEvidenceParticipantTx(t, prepared)

	err := NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID: prepared.RecordID(), RevisionID: "rrv_evidenceprecision",
		},
		Input:               recordsPostgresCompleteRevisionInputWithEvidence(t, "Evidence precision", preparation.SnapshotIDs()),
		EvidencePreparation: preparation,
	})
	if !errors.Is(err, records.ErrInvalidRevisionCommand) {
		t.Fatalf("ApplyRevision() error = %v, want ErrInvalidRevisionCommand", err)
	}
	if tx.queryRowCalls != 0 || len(tx.execKinds) != 0 {
		t.Fatalf("timestamp precision rejection performed DB work: query=%d exec=%#v", tx.queryRowCalls, tx.execKinds)
	}
}

func TestRecordEvidenceRevisionParticipantPersistsImportedSnapshots(t *testing.T) {
	imported := storePreparedImportedSnapshot(t, "rec_importedparticipant", "evs_importedparticipant")
	preparation, err := evidence.NewRevisionPreparation(imported.RecordID(), evidence.RevisionPreparationValues{
		Imported:           []evidence.PreparedImportedSnapshot{imported},
		OrderedSnapshotIDs: []string{imported.SnapshotID()},
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	tx := &fakeRecordEvidenceParticipantTx{projectID: string(recordauth.ProjectIDDefault)}

	err = NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID: imported.RecordID(), RevisionID: "rrv_importedparticipant",
		},
		Input:               recordsPostgresCompleteRevisionInputWithEvidence(t, "Imported evidence", preparation.SnapshotIDs()),
		EvidencePreparation: preparation,
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if tx.capacityLockCalls != 1 || tx.queryRowCalls != 2 ||
		!reflect.DeepEqual(tx.execKinds, []string{"payload", "snapshot", "reference"}) {
		t.Fatalf("imported persist calls = query:%d locks:%d exec:%#v", tx.queryRowCalls, tx.capacityLockCalls, tx.execKinds)
	}
	if tx.referenceOrdinal != 0 || tx.referenceSnapshotID != imported.SnapshotID() {
		t.Fatalf("revision reference = ordinal:%d snapshot:%q", tx.referenceOrdinal, tx.referenceSnapshotID)
	}
	if strings.Contains(tx.querySQL, "delete from public.evidence_capture_intents") {
		t.Fatal("imported persist consumed a capture intent")
	}
}

func storePreparedEvidenceCapture(t *testing.T, recordID, snapshotID, intentID string, previewedAt time.Time) evidence.PreparedCapture {
	return storePreparedEvidenceCaptureWithSnapshotTimeOffset(t, recordID, snapshotID, intentID, previewedAt, 0)
}

func storePreparedEvidenceCaptureWithSnapshotTimeOffset(
	t *testing.T,
	recordID string,
	snapshotID string,
	intentID string,
	previewedAt time.Time,
	snapshotTimeOffset time.Duration,
) evidence.PreparedCapture {
	return storePreparedEvidenceCaptureWithQuotaAndSnapshotTimeOffset(
		t, recordID, snapshotID, intentID, previewedAt,
		evidence.QuotaOutcome{Status: evidence.QuotaAllowed}, snapshotTimeOffset,
	)
}

func storePreparedEvidenceCaptureWithQuota(
	t *testing.T,
	recordID string,
	snapshotID string,
	intentID string,
	previewedAt time.Time,
	quota evidence.QuotaOutcome,
) evidence.PreparedCapture {
	return storePreparedEvidenceCaptureWithQuotaAndSnapshotTimeOffset(
		t, recordID, snapshotID, intentID, previewedAt, quota, 0,
	)
}

func storePreparedEvidenceCaptureWithQuotaAndSnapshotTimeOffset(
	t *testing.T,
	recordID string,
	snapshotID string,
	intentID string,
	previewedAt time.Time,
	quota evidence.QuotaOutcome,
	snapshotTimeOffset time.Duration,
) evidence.PreparedCapture {
	t.Helper()
	descriptor := storeEvidenceParticipantDescriptor()
	authorization := storeEvidenceParticipantAuthorization(t)
	window := evidence.TimeWindow{Start: previewedAt.Add(-time.Hour), End: previewedAt}
	selection := evidence.Selection{
		Key: descriptor.Key, SourceType: string(recordauth.SourceKindTarget), SourceID: authorization.SourceID,
		RequestedWindow: window, Metrics: []string{"latency_ms"}, Precision: time.Minute,
	}
	sourceDigest := sha256.Sum256([]byte("source:" + intentID))
	envelope := evidence.SnapshotEnvelope{
		Key: descriptor.Key,
		Subject: evidence.IdentitySnapshot{
			Type: "target", ID: authorization.SourceID, Fields: map[string]string{"display_name": "Evidence target"},
		},
		Source: evidence.IdentitySnapshot{
			Type: string(recordauth.SourceKindTarget), ID: authorization.SourceID, Fields: map[string]string{"display_name": "Evidence target"},
		},
		Authorization: authorization, RequestedWindow: window, ActualWindow: window,
		ObservedAt:     previewedAt,
		CapturedAt:     previewedAt.Add(time.Minute).Add(snapshotTimeOffset),
		ReferencedAt:   previewedAt.Add(2 * time.Minute).Add(snapshotTimeOffset),
		SourceRevision: "revision-1", SourceWatermark: "watermark-1", SourceDigest: sourceDigest,
		ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units:           evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}},
		Quality:         evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60, BucketCount: 60, DataPointCount: 60},
		Sensitivity:     evidence.SensitivityNormal,
		ActualPrecision: evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		BucketWidth:     evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		QuotaOutcome:    quota,
		Retention: evidence.RetentionSemantics{
			Immutable: true, Scope: evidence.RetentionScopeRecordRevision,
			SourceDeletion: evidence.SourceDeletionSnapshotRetained,
		},
	}
	snapshot, redaction, err := evidence.NewCanonicalSnapshot(
		descriptor,
		envelope,
		map[string]any{"metric_name": "latency_ms", "metric_value": intentID},
		evidence.RedactionNormalOnly,
	)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	preview := evidence.Preview{
		IntentID: intentID, Key: descriptor.Key, Selection: selection,
		Subject: envelope.Subject, Source: envelope.Source,
		RequestedWindow: window, ActualWindow: window, ObservedAt: previewedAt,
		SourceRevision: "revision-1", SourceWatermark: "watermark-1", SourceDigest: sourceDigest,
		ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units: envelope.Units, Quality: envelope.Quality, Sensitivity: envelope.Sensitivity,
		ActualPrecision: envelope.ActualPrecision, BucketWidth: envelope.BucketWidth,
		QuotaOutcome: envelope.QuotaOutcome, Retention: envelope.Retention, Redaction: redaction.Decisions,
		EstimatedCanonicalBytes: snapshot.Size(), RendererVersion: descriptor.Conformance.RendererVersion,
		PreviewedAt: previewedAt, ValidUntil: previewedAt.Add(evidence.CaptureIntentTTL),
	}
	intent := evidence.Intent{
		ID: intentID, Key: descriptor.Key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("preview:" + intentID)), ValidUntil: preview.ValidUntil,
	}
	prepared, err := evidence.PrepareCapture(recordID, snapshotID, descriptor, preview, intent, authorization, snapshot)
	if err != nil {
		t.Fatalf("PrepareCapture() error = %v", err)
	}
	return prepared
}

func storeEvidenceParticipantDescriptor() evidence.Descriptor {
	return evidence.Descriptor{
		Key: evidence.MonitoringProbeV2Key(),
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
}

func storeEvidenceParticipantAuthorization(t *testing.T) evidence.AuthorizationScope {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindTarget,
		SourceID: "tg_0123456789abcdef", State: recordauth.SourceStateLive,
		CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return authorization
}

func storeEvidenceParticipantActor(t *testing.T) evidence.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

type storeEvidenceReferenceSource struct {
	state evidence.ExistingSnapshotReferenceState
}

func (source storeEvidenceReferenceSource) ReauthorizeExistingSnapshot(
	context.Context, evidence.ActorScope, string, string,
) (evidence.ExistingSnapshotReferenceState, error) {
	return source.state, nil
}

func storePreparedEvidenceReference(t *testing.T, actor evidence.ActorScope, recordID, snapshotID string) evidence.PreparedReference {
	t.Helper()
	authorization := storeEvidenceParticipantAuthorization(t)
	state := evidence.ExistingSnapshotReferenceState{
		RecordID: recordID, SnapshotID: snapshotID, Key: evidence.MonitoringProbeV2Key(),
		SourceType: string(authorization.Kind), SourceID: authorization.SourceID,
		CaptureAuthorizationDigest: authorization.Digest,
		PayloadDigest:              sha256.Sum256([]byte("payload:" + snapshotID)),
		Authorization:              authorization,
	}
	prepared, err := evidence.PrepareExistingSnapshotReference(
		context.Background(), storeEvidenceReferenceSource{state: state}, actor, recordID, snapshotID,
	)
	if err != nil {
		t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
	}
	return prepared
}

func storePreparedImportedSnapshot(t *testing.T, recordID, snapshotID string) evidence.PreparedImportedSnapshot {
	t.Helper()
	descriptor := storeEvidenceParticipantDescriptor()
	authorization := storeEvidenceParticipantAuthorization(t)
	previewedAt := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: previewedAt.Add(-time.Hour), End: previewedAt}
	sourceDigest := sha256.Sum256([]byte("source:" + snapshotID))
	envelope := evidence.SnapshotEnvelope{
		Key: descriptor.Key,
		Subject: evidence.IdentitySnapshot{
			Type: "target", ID: authorization.SourceID, Fields: map[string]string{"display_name": "Evidence target"},
		},
		Source: evidence.IdentitySnapshot{
			Type: string(recordauth.SourceKindTarget), ID: authorization.SourceID, Fields: map[string]string{"display_name": "Evidence target"},
		},
		Authorization: authorization, RequestedWindow: window, ActualWindow: window,
		ObservedAt: previewedAt, CapturedAt: previewedAt.Add(time.Minute), ReferencedAt: previewedAt.Add(2 * time.Minute),
		SourceRevision: "revision-1", SourceWatermark: "watermark-1", SourceDigest: sourceDigest,
		ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units:           evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}},
		Quality:         evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60, BucketCount: 60, DataPointCount: 60},
		Sensitivity:     evidence.SensitivityNormal,
		ActualPrecision: evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		BucketWidth:     evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		QuotaOutcome:    evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention: evidence.RetentionSemantics{
			Immutable: true, Scope: evidence.RetentionScopeRecordRevision,
			SourceDeletion: evidence.SourceDeletionSnapshotRetained,
		},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(
		descriptor, envelope, map[string]any{"metric_name": "latency_ms", "metric_value": "imported"},
		evidence.RedactionNormalOnly,
	)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	imported, err := evidence.NewPreparedImportedSnapshot(recordID, snapshotID, snapshot)
	if err != nil {
		t.Fatalf("NewPreparedImportedSnapshot() error = %v", err)
	}
	return imported
}

func storeEvidenceRevisionPreparation(
	t *testing.T,
	recordID string,
	captures []evidence.PreparedCapture,
	references []evidence.PreparedReference,
	ordered []string,
) evidence.RevisionPreparation {
	t.Helper()
	preparation, err := evidence.NewRevisionPreparation(recordID, evidence.RevisionPreparationValues{
		Captures: captures, References: references, OrderedSnapshotIDs: ordered,
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	return preparation
}

type fakeRecordEvidenceIntentRow struct {
	recordID      string
	kind          string
	schemaVersion int64
	previewDigest []byte
	sourceDigest  []byte
	selectionJSON []byte
	previewJSON   []byte
	snapshotID    string
	estimatedSize int64
	createdAt     time.Time
	validUntil    time.Time
	err           error
}

func fakeRecordEvidenceIntentRowFromPrepared(t *testing.T, prepared evidence.PreparedCapture) fakeRecordEvidenceIntentRow {
	t.Helper()
	intent := prepared.Intent()
	preview := prepared.Preview()
	selectionJSON, err := json.Marshal(intent.Selection)
	if err != nil {
		t.Fatal(err)
	}
	previewJSON, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	return fakeRecordEvidenceIntentRow{
		recordID: prepared.RecordID(), kind: string(intent.Key.Kind), schemaVersion: int64(intent.Key.SchemaVersion),
		previewDigest: append([]byte(nil), intent.PreviewDigest[:]...), sourceDigest: append([]byte(nil), preview.SourceDigest[:]...),
		selectionJSON: selectionJSON, previewJSON: previewJSON, snapshotID: prepared.SnapshotID(),
		estimatedSize: int64(preview.EstimatedCanonicalBytes), createdAt: preview.PreviewedAt, validUntil: preview.ValidUntil,
	}
}

func (row fakeRecordEvidenceIntentRow) clone() fakeRecordEvidenceIntentRow {
	row.previewDigest = append([]byte(nil), row.previewDigest...)
	row.sourceDigest = append([]byte(nil), row.sourceDigest...)
	row.selectionJSON = append([]byte(nil), row.selectionJSON...)
	row.previewJSON = append([]byte(nil), row.previewJSON...)
	return row
}

func (row fakeRecordEvidenceIntentRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	values := []any{
		row.recordID, row.kind, row.schemaVersion, row.previewDigest, row.sourceDigest,
		row.selectionJSON, row.previewJSON, row.snapshotID, row.estimatedSize, row.createdAt, row.validUntil,
	}
	if len(dest) != len(values) {
		return errors.New("unexpected evidence intent scan destination count")
	}
	for index := range values {
		target := reflect.ValueOf(dest[index])
		value := reflect.ValueOf(values[index])
		if target.Kind() != reflect.Pointer || !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("unexpected evidence intent scan destination type")
		}
		target.Elem().Set(value)
	}
	return nil
}

type fakeRecordEvidenceParticipantTx struct {
	pgx.Tx
	intentRow           fakeRecordEvidenceIntentRow
	reference           evidence.PreparedReference
	queryRowCalls       int
	querySQL            string
	execKinds           []string
	execSQL             []string
	referenceOrdinal    int64
	referenceSnapshotID string
	projectID           string
	usedLogicalBytes    int64
	capacityLockCalls   int
}

func newFakeRecordEvidenceParticipantTx(t *testing.T, prepared evidence.PreparedCapture) *fakeRecordEvidenceParticipantTx {
	t.Helper()
	return &fakeRecordEvidenceParticipantTx{
		intentRow: fakeRecordEvidenceIntentRowFromPrepared(t, prepared),
		projectID: string(recordauth.ProjectIDDefault),
	}
}

func (tx *fakeRecordEvidenceParticipantTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queryRowCalls++
	tx.querySQL = strings.ToLower(strings.Join(strings.Fields(sql), " "))
	if strings.Contains(tx.querySQL, "select project_id") && strings.Contains(tx.querySQL, "from public.records") {
		projectID := tx.projectID
		if projectID == "" {
			projectID = string(recordauth.ProjectIDDefault)
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			if len(dest) != 1 {
				return errors.New("unexpected evidence project scan destination count")
			}
			target, ok := dest[0].(*string)
			if !ok {
				return errors.New("unexpected evidence project scan destination type")
			}
			*target = projectID
			return nil
		}}
	}
	if strings.Contains(tx.querySQL, "sum(snapshot.logical_size_bytes)") {
		return evidenceCapacityInt64Row(tx.usedLogicalBytes)
	}
	if strings.Contains(tx.querySQL, "delete from public.evidence_capture_intents") {
		return tx.intentRow
	}
	if strings.Contains(tx.querySQL, "from public.evidence_snapshots") {
		return fakeRecordEvidenceReferenceRow{reference: tx.reference}
	}
	return fakeRecordEvidenceIntentRow{err: errors.New("unexpected evidence participant query")}
}

func (tx *fakeRecordEvidenceParticipantTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	tx.execSQL = append(tx.execSQL, compact)
	switch {
	case strings.Contains(compact, "pg_advisory_xact_lock"):
		tx.capacityLockCalls++
		return pgconn.NewCommandTag("SELECT 1"), nil
	case strings.Contains(compact, "insert into public.evidence_payloads"):
		tx.execKinds = append(tx.execKinds, "payload")
	case strings.Contains(compact, "insert into public.evidence_snapshots"):
		tx.execKinds = append(tx.execKinds, "snapshot")
	case strings.Contains(compact, "insert into public.evidence_copy_lineage"):
		tx.execKinds = append(tx.execKinds, "lineage")
	case strings.Contains(compact, "insert into public.record_revision_evidence"):
		tx.execKinds = append(tx.execKinds, "reference")
		tx.referenceOrdinal = args[2].(int64)
		tx.referenceSnapshotID = args[3].(string)
	default:
		return pgconn.CommandTag{}, errors.New("unexpected evidence participant exec")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type fakeRecordEvidenceReferenceRow struct {
	reference evidence.PreparedReference
}

func (row fakeRecordEvidenceReferenceRow) Scan(dest ...any) error {
	authorizationDigest := row.reference.CaptureAuthorizationDigest()
	payloadDigest := row.reference.PayloadDigest()
	values := []any{
		row.reference.RecordID(), row.reference.SnapshotID(), string(row.reference.Key().Kind),
		int64(row.reference.Key().SchemaVersion), row.reference.SourceType(), row.reference.SourceID(),
		append([]byte(nil), authorizationDigest[:]...),
		append([]byte(nil), payloadDigest[:]...),
	}
	if len(dest) != len(values) {
		return errors.New("unexpected evidence reference scan destination count")
	}
	for index := range values {
		target := reflect.ValueOf(dest[index])
		value := reflect.ValueOf(values[index])
		if target.Kind() != reflect.Pointer || !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("unexpected evidence reference scan destination type")
		}
		target.Elem().Set(value)
	}
	return nil
}

func TestRecordEvidenceParticipantTestFixtureUsesDistinctPayloadIdentity(t *testing.T) {
	reference := storePreparedEvidenceReference(t, storeEvidenceParticipantActor(t), "rec_fixture", "evs_fixture")
	payloadDigest := reference.PayloadDigest()
	authorizationDigest := reference.CaptureAuthorizationDigest()
	if bytes.Equal(payloadDigest[:], authorizationDigest[:]) {
		t.Fatal("test fixture payload and authorization digests must be distinct")
	}
}

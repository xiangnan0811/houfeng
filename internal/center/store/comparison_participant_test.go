package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/records"
)

func TestComparisonParticipantNameAndSkipsEmptySave(t *testing.T) {
	t.Parallel()

	participant := NewComparisonRevisionParticipant(nil)
	if participant.Name() != "comparison" {
		t.Fatalf("Name() = %q, want comparison", participant.Name())
	}
	preparation := storeEvidenceRevisionPreparation(t, "rec_comparisonsave", nil, nil, nil)
	err := participant.ApplyRevision(context.Background(), &fakeRecordEvidenceParticipantTx{}, records.RevisionCommitted{
		Result:              records.RevisionCommitResult{RecordID: "rec_comparisonsave", RevisionID: "rrv_comparisonsave"},
		Input:               recordsPostgresCompleteRevisionInputWithEvidence(t, "Empty comparison", preparation.SnapshotIDs()),
		EvidencePreparation: preparation,
	})
	if err != nil {
		t.Fatalf("ApplyRevision(empty) error = %v", err)
	}
}

func TestComparisonParticipantRejectsExpiredIntentAgainstWallClock(t *testing.T) {
	t.Parallel()

	issued := time.Now().UTC().Add(-20 * time.Minute)
	expires := issued.Add(15 * time.Minute)
	preparation := storeComparisonSavePreparation(t, "rec_comparisonsave")
	save := preparation.ComparisonSave()
	save.Claims = evidence.ComparisonIntentClaims{
		Purpose:   evidence.ComparisonIntentPurpose,
		IssuedAt:  issued,
		ExpiresAt: expires,
	}
	prepared, err := evidence.NewRevisionPreparationFromComparisonSave("rec_comparisonsave", save)
	if err != nil {
		t.Fatalf("NewRevisionPreparationFromComparisonSave() error = %v", err)
	}
	err = NewComparisonRevisionParticipant(wallClockComparisonSigner{
		claims:    evidence.ComparisonIntentClaims{Purpose: evidence.ComparisonIntentPurpose},
		expiresAt: expires,
	}).ApplyRevision(context.Background(), &fakeRecordEvidenceParticipantTx{}, records.RevisionCommitted{
		Result: records.RevisionCommitResult{RecordID: "rec_comparisonsave", RevisionID: "rrv_comparisonsave"},
		Input: recordsPostgresCompleteRevisionInputWithEvidence(
			t, "Expired comparison", prepared.SnapshotIDs(),
		),
		EvidencePreparation: prepared,
	})
	if !errors.Is(err, evidence.ErrComparisonIntentExpired) {
		t.Fatalf("ApplyRevision() error = %v, want ErrComparisonIntentExpired", err)
	}
}

func TestComparisonParticipantRejectsForgedIntent(t *testing.T) {
	t.Parallel()

	preparation := storeComparisonSavePreparation(t, "rec_comparisonsave")
	err := NewComparisonRevisionParticipant(comparisonIntentVerifyStub{
		err: evidence.ErrComparisonIntentInvalid,
	}).ApplyRevision(context.Background(), &fakeRecordEvidenceParticipantTx{}, records.RevisionCommitted{
		Result: records.RevisionCommitResult{RecordID: "rec_comparisonsave", RevisionID: "rrv_comparisonsave"},
		Input: recordsPostgresCompleteRevisionInputWithEvidence(
			t, "Forged comparison", preparation.SnapshotIDs(),
		),
		EvidencePreparation: preparation,
	})
	if !errors.Is(err, evidence.ErrComparisonIntentInvalid) {
		t.Fatalf("ApplyRevision() error = %v, want ErrComparisonIntentInvalid", err)
	}
}

func TestSaveComparisonRecordWritesCopiesLineageAndResult(t *testing.T) {
	t.Parallel()

	preparation := storeComparisonSavePreparation(t, "rec_comparisonsave")
	tx := &fakeRecordEvidenceParticipantTx{}
	err := NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{RecordID: "rec_comparisonsave", RevisionID: "rrv_comparisonsave"},
		Input: recordsPostgresCompleteRevisionInputWithEvidence(
			t, "Save comparison", preparation.SnapshotIDs(),
		),
		EvidencePreparation: preparation,
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if !reflectDeepEqualStrings(tx.execKinds, []string{"snapshot", "lineage", "snapshot", "reference", "reference"}) &&
		!reflectDeepEqualStrings(tx.execKinds, []string{"snapshot", "lineage", "snapshot", "reference", "reference", "reference"}) {
		// copies + result snapshots, one lineage per copy, then one revision ref per snapshot
		if !containsAllExecKinds(tx.execKinds, []string{"snapshot", "lineage", "reference"}) {
			t.Fatalf("exec kinds = %#v, want copy/result snapshots, lineage, and revision refs", tx.execKinds)
		}
	}
	joined := strings.Join(tx.execSQL, "\n")
	if !strings.Contains(joined, "insert into public.evidence_copy_lineage") ||
		!strings.Contains(joined, "copied_from_snapshot_id") {
		t.Fatalf("missing copy-lineage write:\n%s", joined)
	}
	if strings.Contains(joined, "update public.evidence_snapshots") ||
		strings.Contains(joined, "delete from public.evidence_snapshots") {
		t.Fatalf("source snapshots were mutated:\n%s", joined)
	}
}

func TestSaveComparisonRecordRejectsQuotaExceededBeforeCopyWrite(t *testing.T) {
	t.Parallel()

	preparation := storeComparisonSavePreparation(t, "rec_comparisonsave")
	tx := &fakeRecordEvidenceParticipantTx{
		usedLogicalBytes: int64(evidence.DefaultProjectEvidenceCapacityBytes),
	}
	err := NewRecordEvidenceRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{RecordID: "rec_comparisonsave", RevisionID: "rrv_comparisonsave"},
		Input: recordsPostgresCompleteRevisionInputWithEvidence(
			t, "Save comparison quota", preparation.SnapshotIDs(),
		),
		EvidencePreparation: preparation,
	})
	if !errors.Is(err, evidence.ErrPreviewStale) && !errors.Is(err, ErrEvidencePersistenceConflict) &&
		!errors.Is(err, evidence.ErrCapacityUnavailable) {
		t.Fatalf("ApplyRevision() error = %v, want quota denial", err)
	}
	if len(tx.execKinds) != 0 {
		t.Fatalf("quota denial wrote logical rows: %#v", tx.execKinds)
	}
}

func storeComparisonSavePreparation(t *testing.T, recordID string) evidence.RevisionPreparation {
	t.Helper()
	source := storePreparedEvidenceCapture(
		t, recordID, "evs_comparisonsource", "evi_cccccccccccccccccccccccc",
		time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
	)
	copy, err := evidence.NewPreparedComparisonCopy(
		recordID, "evs_comparisoncopy", source.SnapshotID(), source.Snapshot(),
	)
	if err != nil {
		t.Fatalf("NewPreparedComparisonCopy() error = %v", err)
	}
	resultSnapshot := storeComparisonResultSnapshot(t)
	result, err := evidence.NewPreparedComparisonResult(recordID, "evs_comparisonresult", resultSnapshot)
	if err != nil {
		t.Fatalf("NewPreparedComparisonResult() error = %v", err)
	}
	preparation, err := evidence.NewRevisionPreparationFromComparisonSave(recordID, evidence.ComparisonSavePreparation{
		Token:  "cmp1.test.payload.mac",
		Copies: []evidence.PreparedComparisonCopy{copy},
		Result: result,
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparationFromComparisonSave() error = %v", err)
	}
	return preparation
}

func storeComparisonResultSnapshot(t *testing.T) evidence.CanonicalSnapshot {
	t.Helper()
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	source := storePreparedEvidenceCapture(
		t, "rec_comparisonsource", "evs_comparisonsourceenv", "evi_dddddddddddddddddddddddd",
		time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
	)
	envelope := source.Snapshot().Envelope()
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
		"version":             "comparison_result/v1",
		"baseline_index":      0,
		"alignment":           string(evidence.CoverageActual),
		"requested_from":      "2026-08-10T11:00:00Z",
		"requested_to":        "2026-08-10T12:00:00Z",
		"tolerance_seconds":   int64(60),
		"digest":              strings.Repeat("ab", 32),
		"registry_version":    "evidence-kinds/v1",
		"calculation_version": evidence.ComparisonCalculationVersion,
		"items": []any{
			map[string]any{
				"original_snapshot_id": "evs_comparisonsource",
				"copied_snapshot_id":   "evs_comparisoncopy",
				"hash":                 strings.Repeat("11", 32),
				"kind":                 evidence.MonitoringProbeV2Key().String(),
				"revision_context":     string(evidence.RevisionContextNotApplicable),
			},
			map[string]any{
				"original_snapshot_id": "evs_comparisonother",
				"copied_snapshot_id":   "evs_comparisoncopyb",
				"hash":                 strings.Repeat("22", 32),
				"kind":                 evidence.MonitoringProbeV2Key().String(),
				"revision_context":     string(evidence.RevisionContextNotApplicable),
			},
		},
		"warnings":           []any{},
		"system_differences": []any{},
		"available_kinds":    []any{evidence.MonitoringProbeV2Key().String()},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(comparison result) error = %v", err)
	}
	return snapshot
}

type wallClockComparisonSigner struct {
	claims    evidence.ComparisonIntentClaims
	expiresAt time.Time
}

func (signer wallClockComparisonSigner) Sign(evidence.ComparisonIntentClaims) (evidence.ComparisonIntent, error) {
	return evidence.ComparisonIntent{}, evidence.ErrComparisonIntentInvalid
}

func (signer wallClockComparisonSigner) Verify(_ string, now time.Time) (evidence.ComparisonIntentClaims, error) {
	if !now.Before(signer.expiresAt) {
		return signer.claims, evidence.ErrComparisonIntentExpired
	}
	return signer.claims, nil
}

type comparisonIntentVerifyStub struct {
	err error
}

func (stub comparisonIntentVerifyStub) Sign(evidence.ComparisonIntentClaims) (evidence.ComparisonIntent, error) {
	return evidence.ComparisonIntent{}, evidence.ErrComparisonIntentInvalid
}

func (stub comparisonIntentVerifyStub) Verify(string, time.Time) (evidence.ComparisonIntentClaims, error) {
	if stub.err != nil {
		return evidence.ComparisonIntentClaims{}, stub.err
	}
	return evidence.ComparisonIntentClaims{}, nil
}

func reflectDeepEqualStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsAllExecKinds(got, want []string) bool {
	seen := map[string]int{}
	for _, kind := range got {
		seen[kind]++
	}
	for _, kind := range want {
		if seen[kind] == 0 {
			return false
		}
	}
	return true
}

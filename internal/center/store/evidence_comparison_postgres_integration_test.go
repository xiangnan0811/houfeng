package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordplatform"
)

func TestEvidenceComparisonCandidatePostgresListsMatchingSubjectsInOneQuery(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-comparison-candidates", 2)
	repository := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_cmpcand", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Comparison candidates"), "comparison-candidates",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	windowStart := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(time.Hour)
	left := evidence.ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcdef"}
	right := evidence.ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcde0"}
	vps := evidence.ComparisonSubjectRef{Kind: "vps", ID: "vps_0123456789abcdef"}
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpleft", recordID: record.RecordID, kind: "monitoring.host", schemaVersion: 1,
		sourceKind: left.Kind, sourceID: left.ID, subjectKind: left.Kind, subjectID: left.ID,
		actualStart: windowStart, actualEnd: windowEnd, digestByte: "a1",
	})
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpright", recordID: record.RecordID, kind: "monitoring.host", schemaVersion: 1,
		sourceKind: right.Kind, sourceID: right.ID, subjectKind: right.Kind, subjectID: right.ID,
		actualStart: windowStart.Add(time.Minute), actualEnd: windowEnd.Add(-time.Minute), digestByte: "a2",
	})
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpold", recordID: record.RecordID, kind: "monitoring.host", schemaVersion: 1,
		sourceKind: left.Kind, sourceID: left.ID, subjectKind: left.Kind, subjectID: left.ID,
		actualStart: windowStart.Add(-3 * time.Hour), actualEnd: windowStart.Add(-2 * time.Hour), digestByte: "a3",
	})
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpcmd", recordID: record.RecordID, kind: "command.audit", schemaVersion: 1,
		sourceKind: left.Kind, sourceID: left.ID, subjectKind: left.Kind, subjectID: left.ID,
		actualStart: windowStart, actualEnd: windowEnd, digestByte: "a4",
	})
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpvpsid", recordID: record.RecordID, kind: "monitoring.host", schemaVersion: 1,
		sourceKind: left.Kind, sourceID: left.ID, subjectKind: vps.Kind, subjectID: vps.ID,
		actualStart: windowStart, actualEnd: windowEnd, digestByte: "a5",
	})

	refs, err := repository.ListComparisonCandidateRefs(
		ctx, []evidence.ComparisonSubjectRef{left, right},
		evidence.TimeWindow{Start: windowStart, End: windowEnd},
		[]evidence.KindKey{evidence.MonitoringHostV1Key()},
	)
	if err != nil {
		t.Fatalf("ListComparisonCandidateRefs() error = %v", err)
	}
	got := comparisonCandidateSnapshotIDs(refs)
	if !containsAllComparisonSnapshotIDs(got, "evs_cmpleft", "evs_cmpright") {
		t.Fatalf("host candidates = %#v, want left and right overlapping host snapshots", got)
	}
	if containsComparisonSnapshotID(got, "evs_cmpold") || containsComparisonSnapshotID(got, "evs_cmpcmd") {
		t.Fatalf("candidates leaked out-of-window or filtered kind: %#v", got)
	}

	identityRefs, err := repository.ListComparisonCandidateRefs(
		ctx, []evidence.ComparisonSubjectRef{vps, right},
		evidence.TimeWindow{Start: windowStart, End: windowEnd},
		[]evidence.KindKey{evidence.MonitoringHostV1Key()},
	)
	if err != nil {
		t.Fatalf("ListComparisonCandidateRefs(vps identity) error = %v", err)
	}
	if !containsComparisonSnapshotID(comparisonCandidateSnapshotIDs(identityRefs), "evs_cmpvpsid") {
		t.Fatalf("identity candidates = %#v, want subject Type/ID match evs_cmpvpsid", comparisonCandidateSnapshotIDs(identityRefs))
	}
}

type comparisonCandidateSeed struct {
	snapshotID, recordID, kind                   string
	schemaVersion                                int
	sourceKind, sourceID, subjectKind, subjectID string
	actualStart, actualEnd                       time.Time
	digestByte                                   string
}

func seedComparisonCandidateSnapshot(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, seed comparisonCandidateSeed) {
	t.Helper()
	payloadDigest := strings.Repeat(seed.digestByte, 32)
	subjectJSON := fmt.Sprintf(
		`{"Type":%q,"ID":%q,"Fields":{"display_name":%q}}`,
		seed.subjectKind, seed.subjectID, seed.snapshotID,
	)
	sourceJSON := fmt.Sprintf(
		`{"Type":%q,"ID":%q,"Fields":{"display_name":%q}}`,
		seed.sourceKind, seed.sourceID, seed.snapshotID,
	)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_payloads (
			payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload
		) values (decode($1::text, 'hex'), 1024, 1, '\x00'::bytea)
		on conflict do nothing`, payloadDigest); err != nil {
		t.Fatalf("seed comparison payload: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_snapshots (
			snapshot_id, record_id, kind, schema_version, source_kind, source_id,
			subject_identity_snapshot, source_identity_snapshot,
			capture_authorization, capture_authorization_digest,
			requested_started_at, requested_ended_at, actual_started_at, actual_ended_at,
			observed_at, captured_at, referenced_at, source_revision, source_digest,
			producer_version, calculation_version, actual_precision, bucket_width,
			unit_semantics, quality, quota_outcome, retention, sensitivity_level,
			redaction, canonical_hash, logical_size_bytes, payload_digest
		) values (
			$1::text, $2::text, $3::text, $4::int,
			$5::text, $6::text,
			$7::jsonb, $8::jsonb,
			'{}'::jsonb, decode(repeat('11', 32), 'hex'),
			$9::timestamptz, $10::timestamptz, $9::timestamptz, $10::timestamptz,
			$10::timestamptz, $10::timestamptz, $10::timestamptz, 'revision-1',
			decode(repeat('22', 32), 'hex'), 'producer-1', 'calculation-1',
			'{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			'{"Status":"complete"}'::jsonb, '{}'::jsonb, '{}'::jsonb, 'normal',
			'[]'::jsonb, decode($11::text, 'hex'), 1024, decode($11::text, 'hex')
		)`,
		seed.snapshotID, seed.recordID, seed.kind, seed.schemaVersion,
		seed.sourceKind, seed.sourceID, subjectJSON, sourceJSON,
		seed.actualStart.UTC(), seed.actualEnd.UTC(), payloadDigest,
	); err != nil {
		t.Fatalf("seed comparison snapshot %s: %v", seed.snapshotID, err)
	}
}

func comparisonCandidateSnapshotIDs(refs []evidence.ComparisonCandidateRef) []string {
	ids := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if _, exists := seen[ref.SnapshotID]; exists {
			continue
		}
		seen[ref.SnapshotID] = struct{}{}
		ids = append(ids, ref.SnapshotID)
	}
	return ids
}

func containsComparisonSnapshotID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func containsAllComparisonSnapshotIDs(ids []string, want ...string) bool {
	for _, id := range want {
		if !containsComparisonSnapshotID(ids, id) {
			return false
		}
	}
	return true
}

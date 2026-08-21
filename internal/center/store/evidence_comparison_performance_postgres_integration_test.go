package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordplatform"
)

func TestEvidenceComparisonCandidatePostgresQueryIsBounded(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-comparison-perf", 2)
	repository := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_cmpperf", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Comparison performance"), "comparison-performance",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	windowStart := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(time.Hour)
	left := evidence.ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcdef"}
	right := evidence.ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcde0"}
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpperfleft", recordID: record.RecordID, kind: "monitoring.host", schemaVersion: 1,
		sourceKind: left.Kind, sourceID: left.ID, subjectKind: left.Kind, subjectID: left.ID,
		actualStart: windowStart, actualEnd: windowEnd, digestByte: "b1",
	})
	seedComparisonCandidateSnapshot(t, ctx, fixture, comparisonCandidateSeed{
		snapshotID: "evs_cmpperfright", recordID: record.RecordID, kind: "monitoring.host", schemaVersion: 1,
		sourceKind: right.Kind, sourceID: right.ID, subjectKind: right.Kind, subjectID: right.ID,
		actualStart: windowStart, actualEnd: windowEnd, digestByte: "b2",
	})

	planRows, err := runtimePool.Query(ctx, "explain "+comparisonCandidateListSQL,
		[]string{left.Kind, right.Kind}, []string{left.ID, right.ID},
		windowStart.UTC(), windowEnd.UTC(),
		[]string{string(evidence.KindMonitoringHost)}, []int64{1},
	)
	if err != nil {
		t.Fatalf("EXPLAIN comparison candidates: %v", err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	planRows.Close()
	if err := planRows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN: %v", err)
	}
	t.Logf("comparison candidate EXPLAIN:\n%s", plan.String())

	began := time.Now()
	refs, err := repository.ListComparisonCandidateRefs(
		ctx, []evidence.ComparisonSubjectRef{left, right},
		evidence.TimeWindow{Start: windowStart, End: windowEnd},
		[]evidence.KindKey{evidence.MonitoringHostV1Key()},
	)
	elapsed := time.Since(began)
	if err != nil {
		t.Fatalf("ListComparisonCandidateRefs() error = %v", err)
	}
	if len(refs) < 2 {
		t.Fatalf("candidate refs = %#v, want at least the two seeded snapshots", refs)
	}
	t.Logf("comparison candidate list elapsed=%s rows=%d (bounded query signal)", elapsed, len(refs))
}

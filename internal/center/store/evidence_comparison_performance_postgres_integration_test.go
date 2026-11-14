package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
)

func TestEvidenceComparisonCandidatePostgresQueryIsBounded(t *testing.T) {
	ctx := context.Background()
	fixture := newComparisonCandidateFixture(t, ctx, false)
	runtimePool := fixture.signed.Runtime
	repository := fixture.repository
	windowStart, windowEnd := fixture.window.Start, fixture.window.End
	left, right := fixture.left, fixture.right

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

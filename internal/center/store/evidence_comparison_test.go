package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestComparisonLiveSubjectResolverRejectsInvalidGrammarWithoutLookup(t *testing.T) {
	t.Parallel()

	resolver := NewComparisonLiveSubjectResolver(records.SubjectAdapterRegistry{})
	err := resolver.ResolveLiveSubject(context.Background(), recordauth.ActorScope{}, evidence.ComparisonSubjectRef{
		Kind: "vps", ID: "vps_not-a-valid-id",
	})
	if !errors.Is(err, evidence.ErrInvalidComparisonSelection) {
		t.Fatalf("ResolveLiveSubject() error = %v, want invalid selection", err)
	}
}

func TestComparisonCandidateListSQLIsOneBatchedSubjectQuery(t *testing.T) {
	t.Parallel()

	compact := strings.Join(strings.Fields(comparisonCandidateListSQL), " ")
	for _, required := range []string{
		"join unnest($1::text[], $2::text[]) as wanted(kind, id)",
		"snapshot.source_kind = wanted.kind and snapshot.source_id = wanted.id",
		"subject_identity_snapshot->>'Type' = wanted.kind",
		"subject_identity_snapshot->>'ID' = wanted.id",
		"from public.record_revision_evidence",
		"cardinality($5::text[]) = 0",
	} {
		if !strings.Contains(compact, required) {
			t.Fatalf("comparison candidate SQL missing %q: %s", required, compact)
		}
	}
	if strings.Contains(compact, "activity") || strings.Contains(compact, "record_search") {
		t.Fatalf("comparison candidate SQL imported activity/search: %s", compact)
	}
}

func TestComparisonSnapshotLoadDecisionMarksAuthorizedPayloadFailureUnreadable(t *testing.T) {
	t.Parallel()

	include, unreadable := comparisonSnapshotLoadDecision(nil, nil)
	if !include || unreadable {
		t.Fatalf("readable payload = (%t, %t), want include and readable", include, unreadable)
	}
	include, unreadable = comparisonSnapshotLoadDecision(evidence.ErrEvidenceServiceUnavailable, nil)
	if !include || !unreadable {
		t.Fatalf("authorized payload failure = (%t, %t), want include and unreadable", include, unreadable)
	}
	include, unreadable = comparisonSnapshotLoadDecision(evidence.ErrSnapshotNotFound, evidence.ErrSnapshotNotFound)
	if include || unreadable {
		t.Fatalf("auth/not-found = (%t, %t), want omit", include, unreadable)
	}
}

func TestComparisonRevisionListSQLIsOneBatchedQuery(t *testing.T) {
	t.Parallel()

	compact := strings.Join(strings.Fields(comparisonRevisionListSQL), " ")
	for _, required := range []string{
		"from public.record_revisions as revision",
		"join unnest($1::text[], $2::text[]) as wanted(record_id, revision_id)",
		"from public.record_revision_evidence as evidence",
		"revision.impact_level",
	} {
		if !strings.Contains(compact, required) {
			t.Fatalf("comparison revision SQL missing %q: %s", required, compact)
		}
	}
	if strings.Contains(compact, "activity") || strings.Contains(compact, "record_search") {
		t.Fatalf("comparison revision SQL imported activity/search: %s", compact)
	}
}

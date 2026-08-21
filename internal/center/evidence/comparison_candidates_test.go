package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestResolveComparisonCandidatesRejectsInvalidSelection(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	window := testWindow()
	subjectA := ComparisonSubjectRef{Kind: "vps", ID: "vps_0123456789abcdef"}
	subjectB := ComparisonSubjectRef{Kind: "vps", ID: "vps_0123456789abcde0"}
	source := &comparisonCandidateSourceStub{}
	tests := []struct {
		name    string
		request ComparisonCandidateRequest
	}{
		{
			name:    "one subject",
			request: ComparisonCandidateRequest{Actor: actor, Subjects: []ComparisonSubjectRef{subjectA}, RequestedWindow: window},
		},
		{
			name: "seven subjects",
			request: ComparisonCandidateRequest{
				Actor: actor,
				Subjects: []ComparisonSubjectRef{
					subjectA, subjectB,
					{Kind: "vps", ID: "vps_0123456789abcde1"},
					{Kind: "vps", ID: "vps_0123456789abcde2"},
					{Kind: "vps", ID: "vps_0123456789abcde3"},
					{Kind: "vps", ID: "vps_0123456789abcde4"},
					{Kind: "vps", ID: "vps_0123456789abcde5"},
				},
				RequestedWindow: window,
			},
		},
		{
			name:    "duplicate subjects",
			request: ComparisonCandidateRequest{Actor: actor, Subjects: []ComparisonSubjectRef{subjectA, subjectA}, RequestedWindow: window},
		},
		{
			name:    "inverted window",
			request: ComparisonCandidateRequest{Actor: actor, Subjects: []ComparisonSubjectRef{subjectA, subjectB}, RequestedWindow: TimeWindow{Start: window.End, End: window.Start}},
		},
		{
			name: "unknown kind filter",
			request: ComparisonCandidateRequest{
				Actor: actor, Subjects: []ComparisonSubjectRef{subjectA, subjectB}, RequestedWindow: window,
				Kinds: []KindKey{{Kind: "monitoring_timeseries", SchemaVersion: 1}},
			},
		},
		{
			name:    "invalid subject grammar",
			request: ComparisonCandidateRequest{Actor: actor, Subjects: []ComparisonSubjectRef{subjectA, {Kind: "vps", ID: "vps_nothex"}}, RequestedWindow: window},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveComparisonCandidates(
				context.Background(), comparisonTestRegistry(t),
				comparisonSubjectResolverStub{}, source, comparisonRecordScopeStub{},
				tt.request,
			)
			if !errors.Is(err, ErrInvalidComparisonSelection) {
				t.Fatalf("ResolveComparisonCandidates() error = %v, want %v", err, ErrInvalidComparisonSelection)
			}
			if source.calls != 0 {
				t.Fatalf("candidate list calls = %d, want 0", source.calls)
			}
		})
	}
}

func TestResolveComparisonCandidatesHidesMissingOrDeniedSubjectWithoutListing(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	visible := ComparisonSubjectRef{Kind: "vps", ID: "vps_0123456789abcdef"}
	hidden := ComparisonSubjectRef{Kind: "vps", ID: "vps_0123456789abcde0"}
	source := &comparisonCandidateSourceStub{refs: []ComparisonCandidateRef{{
		Subject: visible, SnapshotID: "evs_hidden", RecordID: "rec_hidden",
		Kind: CommandAuditV1Key(), CanonicalHash: sha256.Sum256([]byte("hidden")),
	}}}
	_, err := ResolveComparisonCandidates(
		context.Background(), comparisonTestRegistry(t),
		comparisonSubjectResolverStub{missing: map[ComparisonSubjectRef]error{hidden: ErrComparisonSubjectNotFound}},
		source, comparisonRecordScopeStub{},
		ComparisonCandidateRequest{Actor: actor, Subjects: []ComparisonSubjectRef{visible, hidden}, RequestedWindow: testWindow()},
	)
	if !errors.Is(err, ErrComparisonSubjectNotFound) {
		t.Fatalf("ResolveComparisonCandidates() error = %v, want %v", err, ErrComparisonSubjectNotFound)
	}
	if source.calls != 0 {
		t.Fatalf("candidate list calls = %d, want 0 so no sibling count is observed", source.calls)
	}
}

func TestResolveComparisonCandidatesFiltersKindsOmitsUnauthorizedAndSortsDeterministically(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	window := testWindow()
	left := ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcdef"}
	right := ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcde0"}
	allowedScope := comparisonProjectRecordScope(t, left)
	deniedScope := comparisonRestrictedRecordScope(t)
	near := window
	far := TimeWindow{Start: window.Start.Add(-10 * time.Minute), End: window.End.Add(10 * time.Minute)}
	older := window.End.Add(-time.Hour)
	newer := window.End.Add(-time.Minute)
	source := &comparisonCandidateSourceStub{refs: []ComparisonCandidateRef{
		comparisonCandidateRef(t, right, "evs_far", "rec_right", MonitoringHostV1Key(), far, Quality{Status: QualityComplete}, older, allowedScope),
		comparisonCandidateRef(t, left, "evs_denied", "rec_denied", MonitoringHostV1Key(), near, Quality{Status: QualityComplete}, newer, deniedScope),
		comparisonCandidateRef(t, left, "evs_command", "rec_left", CommandAuditV1Key(), near, Quality{Status: QualityComplete}, newer, allowedScope),
		comparisonCandidateRef(t, left, "evs_partial", "rec_left", MonitoringHostV1Key(), near, Quality{Status: QualityPartial, Partial: true, GapCount: 1}, newer, allowedScope),
		comparisonCandidateRef(t, left, "evs_nearb", "rec_left", MonitoringHostV1Key(), near, Quality{Status: QualityComplete}, newer, allowedScope),
		comparisonCandidateRef(t, left, "evs_neara", "rec_left", MonitoringHostV1Key(), near, Quality{Status: QualityComplete}, newer, allowedScope),
		comparisonCandidateRef(t, left, "evs_old", "rec_left", MonitoringHostV1Key(), near, Quality{Status: QualityComplete}, older, allowedScope),
	}}
	result, err := ResolveComparisonCandidates(
		context.Background(), comparisonTestRegistry(t),
		comparisonSubjectResolverStub{}, source,
		comparisonRecordScopeStub{scopes: map[string]recordauth.ResourceScope{
			"rec_left": allowedScope, "rec_right": allowedScope, "rec_denied": deniedScope,
		}},
		ComparisonCandidateRequest{
			Actor: actor, Subjects: []ComparisonSubjectRef{left, right}, RequestedWindow: window,
			Kinds: []KindKey{MonitoringHostV1Key()},
		},
	)
	if err != nil {
		t.Fatalf("ResolveComparisonCandidates() error = %v", err)
	}
	if source.calls != 1 {
		t.Fatalf("candidate list calls = %d, want one batched query", source.calls)
	}
	if !reflect.DeepEqual(source.kinds, []KindKey{MonitoringHostV1Key()}) {
		t.Fatalf("listed kinds = %#v, want host filter only", source.kinds)
	}
	gotIDs := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		gotIDs = append(gotIDs, candidate.SnapshotID)
		if candidate.Recommendation != RecommendationNearestWindow {
			t.Fatalf("candidate %s recommendation = %q", candidate.SnapshotID, candidate.Recommendation)
		}
		if candidate.Kind != MonitoringHostV1Key() {
			t.Fatalf("candidate %s kind = %#v, want host filter", candidate.SnapshotID, candidate.Kind)
		}
	}
	wantIDs := []string{"evs_neara", "evs_nearb", "evs_old", "evs_partial", "evs_far"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("candidate order = %#v, want %#v", gotIDs, wantIDs)
	}
	if !reflect.DeepEqual(result.Subjects, []ComparisonSubjectRef{left, right}) {
		t.Fatalf("subjects = %#v", result.Subjects)
	}
}

type comparisonSubjectResolverStub struct {
	missing map[ComparisonSubjectRef]error
}

func (stub comparisonSubjectResolverStub) ResolveLiveSubject(_ context.Context, _ ActorScope, subject ComparisonSubjectRef) error {
	if err := stub.missing[subject]; err != nil {
		return err
	}
	return nil
}

type comparisonCandidateSourceStub struct {
	refs  []ComparisonCandidateRef
	calls int
	kinds []KindKey
}

func (stub *comparisonCandidateSourceStub) ListComparisonCandidateRefs(
	_ context.Context,
	_ []ComparisonSubjectRef,
	_ TimeWindow,
	kinds []KindKey,
) ([]ComparisonCandidateRef, error) {
	stub.calls++
	stub.kinds = append([]KindKey(nil), kinds...)
	return append([]ComparisonCandidateRef(nil), stub.refs...), nil
}

type comparisonRecordScopeStub struct {
	scopes map[string]recordauth.ResourceScope
}

func (stub comparisonRecordScopeStub) ResolveComparisonRecordScope(_ context.Context, _ ActorScope, recordID string) (recordauth.ResourceScope, error) {
	scope, ok := stub.scopes[recordID]
	if !ok {
		return recordauth.ResourceScope{}, ErrSnapshotNotFound
	}
	return scope, nil
}

func comparisonCandidateRef(
	t *testing.T,
	subject ComparisonSubjectRef,
	snapshotID, recordID string,
	kind KindKey,
	actual TimeWindow,
	quality Quality,
	capturedAt time.Time,
	scope recordauth.ResourceScope,
) ComparisonCandidateRef {
	t.Helper()
	return ComparisonCandidateRef{
		Subject: subject, SnapshotID: snapshotID, RecordID: recordID, Kind: kind,
		CanonicalHash: sha256.Sum256([]byte(snapshotID)), RequestedWindow: actual, ActualWindow: actual,
		Quality: quality, CapturedAt: capturedAt, SourceAuthorization: scope.Sources[0],
	}
}

func comparisonProjectRecordScope(t *testing.T, subject ComparisonSubjectRef) recordauth.ResourceScope {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKind(subject.Kind),
		SourceID: subject.ID, State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return recordauth.ResourceScope{
		Version: recordauth.ResourceScopeVersionV1, ProjectID: recordauth.ProjectIDDefault,
		Visibility: visibility, Sources: []recordauth.SourceAuthorization{source},
	}
}

func comparisonRestrictedRecordScope(t *testing.T) recordauth.ResourceScope {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindRestricted,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS,
		SourceID: "vps_0123456789abcdef", State: recordauth.SourceStateLive,
		CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return recordauth.ResourceScope{
		Version: recordauth.ResourceScopeVersionV1, ProjectID: recordauth.ProjectIDDefault,
		Visibility: visibility, Sources: []recordauth.SourceAuthorization{source},
	}
}

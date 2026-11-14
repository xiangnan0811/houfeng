package evidence

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestComparisonDependenciesBindCanonicalItemsToNativeOwners(t *testing.T) {
	t.Parallel()
	source := comparisonSaveTestSource(t)
	newOwner := source.snapshots["evs_fixeda"]
	newOwner.RecordID = "rec2_80000000000000000000000000000000"
	source.snapshots["evs_fixeda"] = newOwner
	signer := mustComparisonSaveSigner(t)
	now := time.Date(2026, 8, 20, 9, 15, 0, 0, time.UTC)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: testActor(t), Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr("evs_fixeda")},
			{SnapshotID: comparisonStringPtr("evs_fixedb")},
		}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(), Now: now,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("evaluate: %v", err)
	}
	prepared, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, &comparisonSavePayloadSink{}, ComparisonSaveRequest{
		Actor: testActor(t), RecordID: "rec2_70000000000000000000000000000000", Token: resolved.Intent.Token, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := prepared.Result.Snapshot()
	before := string(snapshot.Bytes())
	dependencies, err := ComparisonDependenciesFromNativeClaims(prepared.Result, prepared.Claims)
	if err != nil {
		t.Fatal(err)
	}
	want := []ComparisonDependency{
		{SourceRecordID: source.snapshots["evs_fixeda"].RecordID, SourceSnapshotID: "evs_fixeda"},
		{SourceRecordID: source.snapshots["evs_fixedb"].RecordID, SourceSnapshotID: "evs_fixedb"},
	}
	if !reflect.DeepEqual(dependencies, want) || string(snapshot.Bytes()) != before {
		t.Fatalf("dependencies = %#v, want %#v; canonical unchanged = %v", dependencies, want, string(snapshot.Bytes()) == before)
	}
	for _, name := range []string{"missing owner", "swapped items", "changed snapshot", "changed hash", "changed digest", "missing item"} {
		t.Run(name, func(t *testing.T) {
			claims := prepared.Claims
			claims.Items = append([]ComparisonIntentItem(nil), claims.Items...)
			switch name {
			case "missing owner":
				claims.Items[0].SourceRecordID = ""
			case "swapped items":
				claims.Items[0], claims.Items[1] = claims.Items[1], claims.Items[0]
			case "changed snapshot":
				claims.Items[0].SnapshotID = "evs_collision"
			case "changed hash":
				claims.Items[0].Hash = claims.Items[1].Hash + "00"
			case "changed digest":
				claims.Digest = "different"
			case "missing item":
				claims.Items = claims.Items[:1]
			}
			if _, err := ComparisonDependenciesFromNativeClaims(prepared.Result, claims); !errors.Is(err, ErrComparisonIntentStale) {
				t.Fatalf("mismatched native binding accepted: %v", err)
			}
		})
	}
	items, err := ExtractComparisonDependencyItems(snapshot)
	if err != nil || items[0].OriginalSnapshotID != "evs_fixeda" || items[0].RecordID != "" || items[1].OriginalSnapshotID != "evs_fixedb" {
		t.Fatalf("unresolved canonical references = %#v, %v", items, err)
	}
	wrongResult := prepared.Result
	wrongResult.snapshotID = "evs_unrelated"
	if _, err := ComparisonDependenciesFromNativeClaims(wrongResult, prepared.Claims); !errors.Is(err, ErrComparisonIntentStale) {
		t.Fatalf("accepted unrelated result identity: %v", err)
	}
	payload, err := decodeSnapshotPayload(snapshot.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	payload["items"].([]any)[0].(map[string]any)["copied_snapshot_id"] = "evs_unrelated"
	envelope := snapshot.Envelope()
	envelope.CanonicalHash = [32]byte{}
	envelope.CanonicalSize = 0
	changed, _, err := NewCanonicalSnapshot(comparisonResultDescriptor(), envelope, payload, RedactionNormalOnly)
	if err != nil {
		t.Fatal(err)
	}
	wrongResult = prepared.Result
	wrongResult.snapshot = changed
	if _, err := ComparisonDependenciesFromNativeClaims(wrongResult, prepared.Claims); !errors.Is(err, ErrComparisonIntentStale) {
		t.Fatalf("accepted canonical copy identity unrelated to signed save: %v", err)
	}
	corrupted := snapshot
	corrupted.payload.bytes = append([]byte(nil), snapshot.payload.bytes...)
	corrupted.payload.bytes[len(corrupted.payload.bytes)-1] = ' '
	if _, err := ExtractComparisonDependencyItems(corrupted); err == nil {
		t.Fatal("accepted corrupted canonical bytes")
	}
}

func TestComparisonDependencyCoverageRejectsMissingExtraAndAmbiguousOwners(t *testing.T) {
	t.Parallel()
	items := []ComparisonDependencyItem{
		{OriginalSnapshotID: "evs_source", RevisionContext: RevisionContextNotApplicable},
		{RecordID: "rec_metadata", RevisionID: "rrv_metadata", RevisionContext: RevisionContextBound},
	}
	complete := []ComparisonDependency{
		{SourceRecordID: "rec_metadata", SourceRevisionID: "rrv_metadata"},
		{SourceRecordID: "rec_original", SourceSnapshotID: "evs_source"},
	}
	if !ComparisonDependencyCoverageMatches(items, complete) {
		t.Fatal("complete historical identities require no live source row")
	}
	for _, test := range []struct {
		name string
		rows []ComparisonDependency
	}{
		{"missing metadata-only source", complete[1:]},
		{"missing snapshot source", complete[:1]},
		{"extra source", append(append([]ComparisonDependency(nil), complete...), ComparisonDependency{SourceRecordID: "rec_unrelated", SourceSnapshotID: "evs_other"})},
		{"wrong revision owner", []ComparisonDependency{{SourceRecordID: "rec_different", SourceRevisionID: "rrv_metadata"}, complete[1]}},
		{"duplicate rows", []ComparisonDependency{complete[0], complete[0]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if ComparisonDependencyCoverageMatches(items, test.rows) {
				t.Fatal("incomplete or ambiguous coverage accepted")
			}
		})
	}
	// Repeated canonical selections share one normalized dependency identity.
	if !ComparisonDependencyCoverageMatches(append(items, items[0]), complete) {
		t.Fatal("repeated selection should not require a duplicate dependency row")
	}
	ambiguous := []ComparisonDependency{
		{SourceRecordID: "rec_a", SourceSnapshotID: "evs_source"},
		{SourceRecordID: "rec_b", SourceSnapshotID: "evs_source"},
		complete[0],
	}
	if ComparisonDependencyCoverageMatches(append(items, items[0]), ambiguous) {
		t.Fatal("two certified owner candidates for the same canonical item are ambiguous")
	}
}

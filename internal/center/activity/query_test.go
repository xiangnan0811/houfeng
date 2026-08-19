package activity

import (
	"testing"
	"time"

	"houfeng/internal/center/records"
)

const (
	testVPSSourceID      = "vps_7c2a4e18b09d5f31"
	testOtherVPSSourceID = "vps_7c2a4e18b09d5f32"
	testTargetSourceID   = "tg_5b1d9c740ae23f68"
)

func testSubject() SubjectRef {
	return SubjectRef{Kind: records.SubjectKindVPS, SourceID: testVPSSourceID}
}

// Normalization has to be the single definition of "the same question", because
// the cursor binds to its digest. Two spellings of one query that normalize
// differently would make a next-page token from the first request unusable on
// the second.
func TestNormalizeQueryIsStableAcrossEquivalentSpellings(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	to := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	left, err := NormalizeQuery(Query{
		Subject:    testSubject(),
		View:       ViewActivity,
		Sources:    []SourceKind{SourceKindEvidenceSnapshot, SourceKindRecordDomain, SourceKindRecordDomain},
		EventKinds: []EventKind{EventKindRecordRevised, EventKindRecordCreated},
		From:       from,
		To:         to,
	})
	if err != nil {
		t.Fatalf("normalize left: %v", err)
	}
	right, err := NormalizeQuery(Query{
		Subject:    testSubject(),
		View:       ViewActivity,
		Sources:    []SourceKind{SourceKindRecordDomain, SourceKindEvidenceSnapshot},
		EventKinds: []EventKind{EventKindRecordCreated, EventKindRecordRevised, EventKindRecordCreated},
		From:       from.UTC(),
		To:         to,
		Limit:      DefaultPageSize,
	})
	if err != nil {
		t.Fatalf("normalize right: %v", err)
	}
	if left.Digest() != right.Digest() {
		t.Fatal("equivalent queries produced different digests")
	}
	if left.Limit != DefaultPageSize {
		t.Fatalf("default limit = %d, want %d", left.Limit, DefaultPageSize)
	}
	if left.Versions != VersionsHistory {
		t.Fatalf("default version scope = %q, want %q", left.Versions, VersionsHistory)
	}
}

// Every dimension the server filters on has to change the digest, otherwise a
// cursor would carry a page from one filter into the results of another.
func TestQueryDigestSeparatesEveryFilterDimension(t *testing.T) {
	base, err := NormalizeQuery(Query{Subject: testSubject(), View: ViewActivity})
	if err != nil {
		t.Fatalf("normalize base: %v", err)
	}
	moment := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	variants := map[string]Query{
		"subject kind":  {Subject: SubjectRef{Kind: records.SubjectKindTarget, SourceID: testTargetSourceID}, View: ViewActivity},
		"subject id":    {Subject: SubjectRef{Kind: records.SubjectKindVPS, SourceID: testOtherVPSSourceID}, View: ViewActivity},
		"view":          {Subject: testSubject(), View: ViewRecords},
		"source filter": {Subject: testSubject(), View: ViewActivity, Sources: []SourceKind{SourceKindCommandAudit}},
		"kind filter":   {Subject: testSubject(), View: ViewActivity, EventKinds: []EventKind{EventKindRecordRevised}},
		"from":          {Subject: testSubject(), View: ViewActivity, From: moment},
		"to":            {Subject: testSubject(), View: ViewActivity, To: moment},
		"versions":      {Subject: testSubject(), View: ViewActivity, Versions: VersionsCurrent},
		"limit":         {Subject: testSubject(), View: ViewActivity, Limit: 25},
	}
	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			normalized, err := NormalizeQuery(variant)
			if err != nil {
				t.Fatalf("normalize %s: %v", name, err)
			}
			if normalized.Digest() == base.Digest() {
				t.Fatalf("%s does not change the query digest", name)
			}
		})
	}
}

// The subject kind comes off a URL. Accepting anything the caller writes would
// let a request address a subject registry that does not exist.
func TestNormalizeQueryRejectsInputOutsideTheContract(t *testing.T) {
	cases := map[string]Query{
		"unknown subject kind": {Subject: SubjectRef{Kind: "datacenter", SourceID: "dc_1"}, View: ViewActivity},
		"empty subject id":     {Subject: SubjectRef{Kind: records.SubjectKindVPS}, View: ViewActivity},
		"unknown view":         {Subject: testSubject(), View: "everything"},
		"unknown source":       {Subject: testSubject(), View: ViewActivity, Sources: []SourceKind{"guesswork"}},
		"unknown event kind":   {Subject: testSubject(), View: ViewActivity, EventKinds: []EventKind{"record_state_changed"}},
		"unknown versions":     {Subject: testSubject(), View: ViewActivity, Versions: "latest"},
		"negative limit":       {Subject: testSubject(), View: ViewActivity, Limit: -1},
		"oversized limit":      {Subject: testSubject(), View: ViewActivity, Limit: MaxPageSize + 1},
		"inverted range": {
			Subject: testSubject(),
			View:    ViewActivity,
			From:    time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
			To:      time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeQuery(query); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// view=records is a server-side kind predicate. Resolving it in the domain
// keeps the store and the handler from growing two different ideas of which
// events count as a record.
func TestResolvedEventKindsIntersectViewAndExplicitFilter(t *testing.T) {
	recordsView, err := NormalizeQuery(Query{Subject: testSubject(), View: ViewRecords})
	if err != nil {
		t.Fatalf("normalize records view: %v", err)
	}
	resolved := recordsView.ResolvedEventKinds()
	if len(resolved) == 0 {
		t.Fatal("records view resolved to no event kinds")
	}
	for _, kind := range resolved {
		if kind == EventKindCommentCreated {
			t.Fatal("records view resolved to a collaboration kind")
		}
	}

	narrowed, err := NormalizeQuery(Query{
		Subject:    testSubject(),
		View:       ViewRecords,
		EventKinds: []EventKind{EventKindRecordRevised, EventKindCommentCreated},
	})
	if err != nil {
		t.Fatalf("normalize narrowed records view: %v", err)
	}
	got := narrowed.ResolvedEventKinds()
	if len(got) != 1 || got[0] != EventKindRecordRevised {
		t.Fatalf("resolved kinds = %v, want only the record kind inside the view", got)
	}
}

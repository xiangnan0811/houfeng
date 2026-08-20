package activity

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func testCodec(t *testing.T) *CursorCodec {
	t.Helper()
	codec, err := NewCursorCodec([]byte("houfeng-test-session-hmac-key-material"))
	if err != nil {
		t.Fatalf("build cursor codec: %v", err)
	}
	return codec
}

func testActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	scope, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000001",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("normalize actor scope: %v", err)
	}
	return scope
}

func testCursorValues(t *testing.T) CursorValues {
	t.Helper()
	query, err := NormalizeQuery(Query{Subject: testSubject(), View: ViewActivity})
	if err != nil {
		t.Fatalf("normalize cursor query: %v", err)
	}
	return CursorValues{
		Query:      query,
		Actor:      testActor(t),
		Generation: 7,
		AsOf:       4211,
		ExpiresAt:  time.Now().Add(30 * time.Minute).UTC(),
		SortKey: SortKey{
			EventAt:    time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC),
			RecordedAt: time.Date(2026, 8, 18, 9, 30, 5, 0, time.UTC),
			SourceKind: SourceKindRecordDomain,
			ActivityID: "act_gv2xq4hb7zsm3nkfr6wcyd5ple",
		},
	}
}

func TestCursorRoundTripsTheWatermarkAndFullSortTuple(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)

	token, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	bound, err := codec.Bind(token, values.Query, values.Actor, values.Generation, time.Now().UTC())
	if err != nil {
		t.Fatalf("bind cursor: %v", err)
	}
	if bound.AsOf() != values.AsOf {
		t.Fatalf("as-of watermark = %d, want %d", bound.AsOf(), values.AsOf)
	}
	if bound.Generation() != values.Generation {
		t.Fatalf("generation = %d, want %d", bound.Generation(), values.Generation)
	}
	got := bound.SortKey()
	if !got.EventAt.Equal(values.SortKey.EventAt) || !got.RecordedAt.Equal(values.SortKey.RecordedAt) ||
		got.SourceKind != values.SortKey.SourceKind || got.ActivityID != values.SortKey.ActivityID {
		t.Fatalf("sort key = %#v, want %#v", got, values.SortKey)
	}
}

func TestCursorRoundTripsWatermarkOnlySnapshotToken(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)
	values.AsOf = 0
	values.SortKey = SortKey{}

	token, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode snapshot cursor: %v", err)
	}
	bound, err := codec.Bind(token, values.Query, values.Actor, values.Generation, time.Now().UTC())
	if err != nil {
		t.Fatalf("bind snapshot cursor: %v", err)
	}
	if !bound.FirstPage() {
		t.Fatalf("snapshot cursor FirstPage() = false")
	}
	if bound.AsOf() != 0 {
		t.Fatalf("as-of = %d, want 0", bound.AsOf())
	}
}

// The watermark and the generation are how one authorization scope could learn
// that activity it cannot see is advancing. If the token were merely encoded,
// the browser could read both straight off the URL.
func TestCursorTokenDoesNotRevealServerSideProjectionState(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)

	token, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("cursor is not base64url: %v", err)
	}
	for _, leaked := range []string{
		"4211",
		"as_of",
		"generation",
		"ingest",
		values.SortKey.ActivityID,
		string(values.Query.Subject.SourceID),
	} {
		if strings.Contains(string(decoded), leaked) {
			t.Fatalf("cursor payload exposes %q", leaked)
		}
	}
}

// Two tokens for the same position must not be equal, otherwise a client could
// compare tokens to detect that the server watermark moved, or replay-match one
// page against another.
func TestCursorTokensAreNotComparable(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)

	first, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode first cursor: %v", err)
	}
	second, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode second cursor: %v", err)
	}
	if first == second {
		t.Fatal("two encodings of the same position produced an identical token")
	}
}

// Token length must not track payload length. A shorter subject id or a smaller
// watermark would otherwise be visible from the outside.
func TestCursorTokensUseFixedLengthBuckets(t *testing.T) {
	codec := testCodec(t)
	small := testCursorValues(t)

	large := testCursorValues(t)
	largeQuery, err := NormalizeQuery(Query{
		Subject:    testSubject(),
		View:       ViewActivity,
		Sources:    []SourceKind{SourceKindRecordDomain, SourceKindEvidenceSnapshot, SourceKindAssetHistory, SourceKindMonitoringEvent, SourceKindCommandAudit},
		EventKinds: RecordsViewEventKinds(),
	})
	if err != nil {
		t.Fatalf("normalize large query: %v", err)
	}
	large.Query = largeQuery
	large.AsOf = 1 << 40

	smallToken, err := codec.Encode(small)
	if err != nil {
		t.Fatalf("encode small cursor: %v", err)
	}
	largeToken, err := codec.Encode(large)
	if err != nil {
		t.Fatalf("encode large cursor: %v", err)
	}
	if len(smallToken) != len(largeToken) {
		t.Fatalf("token lengths differ: %d vs %d", len(smallToken), len(largeToken))
	}
}

// A token belongs to one question asked by one actor. Carrying it into a
// different filter or a different authorization namespace would page through
// rows the caller never asked for and may not see.
func TestCursorRefusesADifferentQueryOrActor(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)
	token, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	otherQuery, err := NormalizeQuery(Query{Subject: testSubject(), View: ViewEvidence})
	if err != nil {
		t.Fatalf("normalize other query: %v", err)
	}
	if _, err := codec.Bind(token, otherQuery, values.Actor, values.Generation, time.Now().UTC()); !isCursorInvalid(err) {
		t.Fatalf("binding under a different query returned %v, want ErrCursorInvalid", err)
	}

	otherActor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000002",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("normalize other actor: %v", err)
	}
	if _, err := codec.Bind(token, values.Query, otherActor, values.Generation, time.Now().UTC()); !isCursorInvalid(err) {
		t.Fatalf("binding under a different actor returned %v, want ErrCursorInvalid", err)
	}
}

// A generation change means the watermark was renumbered by a rebuild. That is
// recoverable by replaying the same query from page one, so it has to be
// distinguishable from a client that mangled its filters.
func TestCursorReportsGenerationChangeAsExpiredNotInvalid(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)
	token, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if _, err := codec.Bind(token, values.Query, values.Actor, values.Generation+1, time.Now().UTC()); !isCursorExpired(err) {
		t.Fatalf("binding across generations returned %v, want ErrCursorExpired", err)
	}
}

func TestCursorRejectsExpiredAndTamperedTokens(t *testing.T) {
	codec := testCodec(t)
	values := testCursorValues(t)
	token, err := codec.Encode(values)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	if _, err := codec.Bind(token, values.Query, values.Actor, values.Generation, values.ExpiresAt.Add(time.Second)); !isCursorExpired(err) {
		t.Fatalf("binding an outdated token returned %v, want ErrCursorExpired", err)
	}

	tampered := []byte(token)
	tampered[len(tampered)-1] ^= 'a' ^ 'b'
	if _, err := codec.Bind(string(tampered), values.Query, values.Actor, values.Generation, time.Now().UTC()); !isCursorInvalid(err) {
		t.Fatalf("binding a tampered token returned %v, want ErrCursorInvalid", err)
	}

	if _, err := codec.Bind("", values.Query, values.Actor, values.Generation, time.Now().UTC()); !isCursorInvalid(err) {
		t.Fatalf("binding an empty token returned %v, want ErrCursorInvalid", err)
	}
}

// A token minted by one deployment must not open on another. Deriving the key
// per deployment is what keeps a copied URL from working elsewhere.
func TestCursorFromAnotherDeploymentDoesNotOpen(t *testing.T) {
	values := testCursorValues(t)
	token, err := testCodec(t).Encode(values)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	foreign, err := NewCursorCodec([]byte("a-completely-different-deployment-secret"))
	if err != nil {
		t.Fatalf("build foreign codec: %v", err)
	}
	if _, err := foreign.Bind(token, values.Query, values.Actor, values.Generation, time.Now().UTC()); !isCursorInvalid(err) {
		t.Fatalf("foreign codec accepted the token: %v", err)
	}
}

func TestNewCursorCodecRejectsWeakKeyMaterial(t *testing.T) {
	if _, err := NewCursorCodec(nil); err == nil {
		t.Fatal("a nil key was accepted")
	}
	if _, err := NewCursorCodec([]byte("short")); err == nil {
		t.Fatal("key material below the minimum length was accepted")
	}
}

func TestEncodeRejectsIncompleteCursorValues(t *testing.T) {
	codec := testCodec(t)
	unnormalized := testCursorValues(t)
	unnormalized.Query = Query{Subject: testSubject(), View: ViewActivity}

	cases := map[string]func(CursorValues) CursorValues{
		"unnormalized query":  func(CursorValues) CursorValues { return unnormalized },
		"zero generation":     func(v CursorValues) CursorValues { v.Generation = 0; return v },
		"missing expiry":      func(v CursorValues) CursorValues { v.ExpiresAt = time.Time{}; return v },
		"missing activity id": func(v CursorValues) CursorValues { v.SortKey.ActivityID = ""; return v },
		"missing event time":  func(v CursorValues) CursorValues { v.SortKey.EventAt = time.Time{}; return v },
		"unknown source kind": func(v CursorValues) CursorValues { v.SortKey.SourceKind = "guesswork"; return v },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Encode(mutate(testCursorValues(t))); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

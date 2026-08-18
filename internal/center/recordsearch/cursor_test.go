package recordsearch

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func testActor(t *testing.T, userID string) recordauth.ActorScope {
	t.Helper()

	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    userID,
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func testQuery(t *testing.T, values QueryValues) Query {
	t.Helper()

	if values.Sort == "" {
		values.Sort = SortUpdatedDesc
	}
	query, err := NormalizeQuery(values)
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}
	return query
}

func testCursorValues(t *testing.T, query Query, actor recordauth.ActorScope, now time.Time) CursorValues {
	t.Helper()

	return CursorValues{
		Query:      query,
		Actor:      actor,
		Generation: 7,
		ExpiresAt:  now.Add(time.Hour),
		SortKey: SortKey{
			UpdatedAt: time.Date(2026, 8, 18, 4, 5, 6, 0, time.UTC),
			RecordID:  "rec_0000000000000001",
		},
	}
}

func TestCursorRoundTripsThroughItsOpaqueEncoding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	actor := testActor(t, "usr_000000000000000000000001")
	query := testQuery(t, QueryValues{Tags: []string{"disk"}})
	values := testCursorValues(t, query, actor, now)

	encoded, err := EncodeCursor(values)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	bound, err := BindCursor(encoded, query, actor, values.Generation, now)
	if err != nil {
		t.Fatalf("BindCursor() error = %v", err)
	}
	if bound.SortKey() != values.SortKey {
		t.Fatalf("SortKey() = %+v, want %+v", bound.SortKey(), values.SortKey)
	}
	if bound.Generation() != values.Generation {
		t.Fatalf("Generation() = %d, want %d", bound.Generation(), values.Generation)
	}
}

func TestEncodeCursorRejectsUnusableValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	actor := testActor(t, "usr_000000000000000000000001")
	query := testQuery(t, QueryValues{})
	relevance := testQuery(t, QueryValues{Text: "disk", Sort: SortRelevanceDesc})

	tests := []struct {
		name   string
		mutate func(values *CursorValues)
	}{
		{name: "zero generation", mutate: func(values *CursorValues) { values.Generation = 0 }},
		{name: "zero expiry", mutate: func(values *CursorValues) { values.ExpiresAt = time.Time{} }},
		{name: "unnormalized query", mutate: func(values *CursorValues) { values.Query = Query{} }},
		{name: "unnormalized actor", mutate: func(values *CursorValues) { values.Actor = recordauth.ActorScope{} }},
		{name: "zero sort timestamp", mutate: func(values *CursorValues) { values.SortKey.UpdatedAt = time.Time{} }},
		{name: "missing record id", mutate: func(values *CursorValues) { values.SortKey.RecordID = "" }},
		{name: "malformed record id", mutate: func(values *CursorValues) { values.SortKey.RecordID = "rec_UPPER" }},
		// Relevance only exists for a relevance query. Carrying it on a time-ordered
		// page would let the store resume from a key the SQL never produced.
		{name: "relevance without relevance sort", mutate: func(values *CursorValues) { values.SortKey.Relevance = 0.5 }},
		{name: "relevance sort without relevance", mutate: func(values *CursorValues) { values.Query = relevance }},
		{name: "negative relevance", mutate: func(values *CursorValues) {
			values.Query = relevance
			values.SortKey.Relevance = -1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := testCursorValues(t, query, actor, now)
			tt.mutate(&values)
			if _, err := EncodeCursor(values); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("EncodeCursor() error = %v, want %v", err, ErrInvalidCursor)
			}
		})
	}
}

func TestBindCursorRejectsTamperAndContextDrift(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	actor := testActor(t, "usr_000000000000000000000001")
	other := testActor(t, "usr_000000000000000000000002")
	query := testQuery(t, QueryValues{Tags: []string{"disk"}})
	values := testCursorValues(t, query, actor, now)
	encoded, err := EncodeCursor(values)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	flipped := []byte(encoded)
	flipped[len(flipped)/2] ^= 0x01

	tests := []struct {
		name       string
		encoded    string
		query      Query
		actor      recordauth.ActorScope
		generation uint64
		now        time.Time
	}{
		{name: "empty", encoded: "", query: query, actor: actor, generation: 7, now: now},
		{name: "not base64", encoded: "not*base64", query: query, actor: actor, generation: 7, now: now},
		{name: "truncated", encoded: encoded[:len(encoded)/2], query: query, actor: actor, generation: 7, now: now},
		{name: "flipped byte", encoded: string(flipped), query: query, actor: actor, generation: 7, now: now},
		{name: "empty json object", encoded: base64.RawURLEncoding.EncodeToString([]byte("{}")),
			query: query, actor: actor, generation: 7, now: now},
		{name: "different filter", encoded: encoded,
			query: testQuery(t, QueryValues{Tags: []string{"network"}}), actor: actor, generation: 7, now: now},
		{name: "different page size", encoded: encoded,
			query: testQuery(t, QueryValues{Tags: []string{"disk"}, PageSize: 10}), actor: actor, generation: 7, now: now},
		{name: "different sort", encoded: encoded,
			query: testQuery(t, QueryValues{Tags: []string{"disk"}, Sort: SortUpdatedAsc}), actor: actor, generation: 7, now: now},
		{name: "different actor", encoded: encoded, query: query, actor: other, generation: 7, now: now},
		{name: "unnormalized actor", encoded: encoded, query: query,
			actor: recordauth.ActorScope{}, generation: 7, now: now},
		{name: "republished generation", encoded: encoded, query: query, actor: actor, generation: 8, now: now},
		{name: "expired exactly at the boundary", encoded: encoded, query: query, actor: actor,
			generation: 7, now: values.ExpiresAt},
		{name: "expired", encoded: encoded, query: query, actor: actor,
			generation: 7, now: values.ExpiresAt.Add(time.Second)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := BindCursor(tt.encoded, tt.query, tt.actor, tt.generation, tt.now)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("BindCursor() error = %v, want %v", err, ErrInvalidCursor)
			}
			// A cursor rejection must not say which check failed. The reason
			// separates "you tampered" from "the index was rebuilt" from "your
			// authorization changed", and the caller has no legitimate use for it.
			if err.Error() != ErrInvalidCursor.Error() {
				t.Fatalf("BindCursor() error = %q, want the bare sentinel text", err)
			}
		})
	}
}

// The whole point of binding to the actor digest is that a cursor minted under
// one authorization namespace cannot be replayed after that namespace changes.
func TestBindCursorRejectsGroupMembershipChange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	before, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000001",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  []string{"rag_0000000000000001"},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	after, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000001",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  []string{"rag_0000000000000001", "rag_0000000000000002"},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}

	query := testQuery(t, QueryValues{})
	encoded, err := EncodeCursor(testCursorValues(t, query, before, now))
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if _, err := BindCursor(encoded, query, after, 7, now); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("BindCursor() error = %v, want %v", err, ErrInvalidCursor)
	}
}

func TestBindCursorCarriesRelevanceForRelevanceQueries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	actor := testActor(t, "usr_000000000000000000000001")
	query := testQuery(t, QueryValues{Text: "disk", Sort: SortRelevanceDesc})
	values := testCursorValues(t, query, actor, now)
	values.SortKey.Relevance = 0.1234567890123

	encoded, err := EncodeCursor(values)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	bound, err := BindCursor(encoded, query, actor, values.Generation, now)
	if err != nil {
		t.Fatalf("BindCursor() error = %v", err)
	}
	if bound.SortKey().Relevance != values.SortKey.Relevance {
		t.Fatalf("Relevance = %v, want %v", bound.SortKey().Relevance, values.SortKey.Relevance)
	}
}

// The encoding is transport, not storage: it must stay URL-safe and must not
// expose the operator's query text to anything that logs a URL.
func TestEncodedCursorIsURLSafeAndOpaque(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	actor := testActor(t, "usr_000000000000000000000001")
	query := testQuery(t, QueryValues{Text: "磁盘 secret-project", Tags: []string{"disk"}})
	encoded, err := EncodeCursor(testCursorValues(t, query, actor, now))
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(encoded); err != nil {
		t.Fatalf("encoded cursor is not raw url base64: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	for _, secret := range []string{"磁盘", "secret-project", "disk", actor.UserID} {
		if containsSubstring(string(decoded), secret) {
			t.Fatalf("encoded cursor leaks %q", secret)
		}
	}
}

func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestSubjectFilterPlacementVocabularyIsClosed(t *testing.T) {
	t.Parallel()

	// Placement is a search-owned refinement of the records registry, so it has
	// to stay exactly as wide as the registry allows: one primary per revision,
	// everything else related.
	for _, placement := range []SubjectPlacement{SubjectPlacementAny, SubjectPlacementPrimary, SubjectPlacementRelated} {
		values := QueryValues{Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, Placement: placement}}}
		if _, err := NormalizeQuery(values); err != nil {
			t.Fatalf("NormalizeQuery(placement=%q) error = %v", placement, err)
		}
	}
}

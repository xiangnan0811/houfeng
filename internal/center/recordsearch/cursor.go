package recordsearch

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// SortKey is the position of the last row on a page. It always carries the
// record identifier so resuming is a total order even when two records share a
// timestamp or a relevance rank.
type SortKey struct {
	Relevance float64
	UpdatedAt time.Time
	RecordID  string
}

// CursorValues is everything a next-page token binds to. The query digest
// covers the filters, the sort, and the page size; the actor digest covers the
// authorization namespace; the generation covers the published index.
type CursorValues struct {
	Query      Query
	Actor      recordauth.ActorScope
	Generation uint64
	ExpiresAt  time.Time
	SortKey    SortKey
}

// Cursor is a validated page position. The only way to obtain one from
// transport is BindCursor, so an unbound cursor cannot reach the store.
type Cursor struct {
	generation uint64
	expiresAt  time.Time
	sortKey    SortKey
}

func (cursor Cursor) Generation() uint64 {
	return cursor.generation
}

func (cursor Cursor) ExpiresAt() time.Time {
	return cursor.expiresAt
}

func (cursor Cursor) SortKey() SortKey {
	return cursor.sortKey
}

type cursorEnvelope struct {
	Version     uint64  `json:"v"`
	QueryDigest string  `json:"q"`
	ActorDigest string  `json:"a"`
	Generation  uint64  `json:"g"`
	ExpiresAt   int64   `json:"e"`
	Relevance   float64 `json:"r,omitempty"`
	UpdatedAt   int64   `json:"u"`
	RecordID    string  `json:"i"`
}

// EncodeCursor mints an opaque next-page token. It carries digests rather than
// the query itself, so a token can be logged as a URL parameter without
// exposing what the operator searched for.
func EncodeCursor(values CursorValues) (string, error) {
	queryDigest, actorDigest, err := cursorBindingDigests(values.Query, values.Actor)
	if err != nil {
		return "", err
	}
	if values.Generation == 0 || values.ExpiresAt.IsZero() {
		return "", ErrInvalidCursor
	}
	if err := validateSortKey(values.Query, values.SortKey); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(cursorEnvelope{
		Version:     CursorVersionV1,
		QueryDigest: hex.EncodeToString(queryDigest[:]),
		ActorDigest: hex.EncodeToString(actorDigest[:]),
		Generation:  values.Generation,
		ExpiresAt:   values.ExpiresAt.UTC().Truncate(time.Microsecond).UnixMicro(),
		Relevance:   values.SortKey.Relevance,
		UpdatedAt:   values.SortKey.UpdatedAt.UTC().Truncate(time.Microsecond).UnixMicro(),
		RecordID:    values.SortKey.RecordID,
	})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// BindCursor decodes a token and proves it belongs to this question, this
// actor, this published generation, and this moment. Every failure returns the
// same bare sentinel: the caller may retry from page one, and learns nothing
// about why the token stopped working.
func BindCursor(
	encoded string,
	query Query,
	actor recordauth.ActorScope,
	generation uint64,
	now time.Time,
) (Cursor, error) {
	queryDigest, actorDigest, err := cursorBindingDigests(query, actor)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if encoded == "" || generation == 0 || now.IsZero() {
		return Cursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope cursorEnvelope
	if err := decoder.Decode(&envelope); err != nil || decoder.More() {
		return Cursor{}, ErrInvalidCursor
	}
	if envelope.Version != CursorVersionV1 || envelope.Generation != generation ||
		envelope.QueryDigest != hex.EncodeToString(queryDigest[:]) ||
		envelope.ActorDigest != hex.EncodeToString(actorDigest[:]) {
		return Cursor{}, ErrInvalidCursor
	}
	if envelope.ExpiresAt == 0 || envelope.UpdatedAt == 0 {
		return Cursor{}, ErrInvalidCursor
	}
	expiresAt := time.UnixMicro(envelope.ExpiresAt).UTC()
	if !now.Before(expiresAt) {
		return Cursor{}, ErrInvalidCursor
	}
	sortKey := SortKey{
		Relevance: envelope.Relevance,
		UpdatedAt: time.UnixMicro(envelope.UpdatedAt).UTC(),
		RecordID:  envelope.RecordID,
	}
	if err := validateSortKey(query, sortKey); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{generation: generation, expiresAt: expiresAt, sortKey: sortKey}, nil
}

func cursorBindingDigests(
	query Query,
	actor recordauth.ActorScope,
) (queryDigest, actorDigest [sha256.Size]byte, err error) {
	if !query.normalized() {
		return queryDigest, actorDigest, ErrInvalidCursor
	}
	normalizedActor, actorErr := recordauth.NormalizeActorScope(actor)
	if actorErr != nil {
		return queryDigest, actorDigest, ErrInvalidCursor
	}
	return query.Digest(), normalizedActor.CanonicalHash(), nil
}

func validateSortKey(query Query, sortKey SortKey) error {
	if sortKey.UpdatedAt.IsZero() || !records.ValidRecordRootID(sortKey.RecordID) {
		return ErrInvalidCursor
	}
	// A relevance component only exists for a relevance-ordered page. Carrying
	// one on a time-ordered page, or omitting one from a relevance page, would
	// resume from a key the SQL never produced.
	if query.Sort() == SortRelevanceDesc {
		if sortKey.Relevance <= 0 {
			return ErrInvalidCursor
		}
		return nil
	}
	if sortKey.Relevance != 0 {
		return ErrInvalidCursor
	}
	return nil
}

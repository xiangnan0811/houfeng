package activity

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	// ErrCursorInvalid means the token does not belong to this question, this
	// actor, or this deployment. The caller has to start the page over with a
	// correct request.
	ErrCursorInvalid = errors.New("invalid activity cursor token")
	// ErrCursorExpired means the token was well-formed and ours, but the
	// projection generation moved or the token aged out. Replaying the same
	// query from page one recovers.
	ErrCursorExpired = errors.New("expired activity cursor token")
)

const (
	// MinCursorKeyMaterial keeps a deployment from deriving a page-token key
	// from a handful of characters.
	MinCursorKeyMaterial = 16
	// cursorPlaintextBucket is the fixed plaintext size every token pads to, so
	// the ciphertext length says nothing about the filters or the watermark
	// inside it.
	cursorPlaintextBucket = 512
	// DefaultCursorTTL matches the search cursor so both surfaces age out at
	// the same rate.
	DefaultCursorTTL = 30 * time.Minute
)

// SortKey is the position of the last row on a page. It carries the full
// ordering tuple rather than a timestamp alone, because two events can share an
// event time and a recorded time; without the trailing keys a page boundary
// would drop or repeat rows.
type SortKey struct {
	EventAt    time.Time
	RecordedAt time.Time
	SourceKind SourceKind
	ActivityID string
}

// CursorValues is everything a next-page token binds to.
type CursorValues struct {
	Query      Query
	Actor      recordauth.ActorScope
	Generation uint64
	AsOf       uint64
	ExpiresAt  time.Time
	SortKey    SortKey
}

// Cursor is a validated page position. The only way to obtain one from
// transport is CursorCodec.Bind, so an unverified token cannot reach the store.
type Cursor struct {
	generation uint64
	asOf       uint64
	expiresAt  time.Time
	sortKey    SortKey
}

func (cursor Cursor) Generation() uint64   { return cursor.generation }
func (cursor Cursor) AsOf() uint64         { return cursor.asOf }
func (cursor Cursor) ExpiresAt() time.Time { return cursor.expiresAt }
func (cursor Cursor) SortKey() SortKey     { return cursor.sortKey }

// FirstPage reports whether this cursor is a watermark-only token: it freezes
// generation and as-of, but does not advance past any row. Snapshot tokens and
// the first page of a query both use this shape.
func (cursor Cursor) FirstPage() bool {
	return cursor.sortKey == SortKey{}
}

// CursorCodec seals page positions with AES-GCM. Encryption rather than a
// signed envelope is the requirement here: the payload holds the projection
// generation and the as-of watermark, and a client that could read those would
// learn that activity outside its authorization scope is advancing.
type CursorCodec struct {
	aead cipher.AEAD
}

// NewCursorCodec derives a dedicated key from deployment key material. Deriving
// instead of reusing the material directly keeps a page token from being
// interchangeable with anything else signed by the same secret, and keeps a
// token minted by one deployment from opening on another.
func NewCursorCodec(keyMaterial []byte) (*CursorCodec, error) {
	if len(keyMaterial) < MinCursorKeyMaterial {
		return nil, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, keyMaterial)
	mac.Write([]byte("houfeng.record-activity.cursor.v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, ErrInvalidCursor
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &CursorCodec{aead: aead}, nil
}

// cursorEnvelope is the sealed payload. Field names stay single letters because
// the plaintext is padded to a fixed bucket and every saved byte is room for a
// longer sort key.
type cursorEnvelope struct {
	Version     uint64 `json:"v"`
	QueryDigest string `json:"q"`
	ActorDigest string `json:"a"`
	Generation  uint64 `json:"g"`
	AsOf        uint64 `json:"w"`
	ExpiresAt   int64  `json:"e"`
	EventAt     int64  `json:"t"`
	RecordedAt  int64  `json:"r"`
	SourceKind  string `json:"s"`
	ActivityID  string `json:"i"`
}

// Encode seals a page position into an opaque token.
func (codec *CursorCodec) Encode(values CursorValues) (string, error) {
	queryDigest, actorDigest, err := cursorBindingDigests(values.Query, values.Actor)
	if err != nil {
		return "", err
	}
	// AsOf may be zero when the generation has published nothing yet: the token
	// still freezes that empty watermark so a later publish cannot silently
	// expand a page that already answered "no events".
	if values.Generation == 0 || values.ExpiresAt.IsZero() {
		return "", ErrInvalidCursor
	}
	if err := validateSortKey(values.SortKey); err != nil {
		return "", err
	}

	eventAtMicro := int64(0)
	recordedAtMicro := int64(0)
	if values.SortKey != (SortKey{}) {
		eventAtMicro = values.SortKey.EventAt.UTC().Truncate(time.Microsecond).UnixMicro()
		recordedAtMicro = values.SortKey.RecordedAt.UTC().Truncate(time.Microsecond).UnixMicro()
	}

	payload, err := json.Marshal(cursorEnvelope{
		Version:     CursorVersionV1,
		QueryDigest: hex.EncodeToString(queryDigest[:]),
		ActorDigest: hex.EncodeToString(actorDigest[:]),
		Generation:  values.Generation,
		AsOf:        values.AsOf,
		ExpiresAt:   values.ExpiresAt.UTC().Truncate(time.Microsecond).UnixMicro(),
		EventAt:     eventAtMicro,
		RecordedAt:  recordedAtMicro,
		SourceKind:  string(values.SortKey.SourceKind),
		ActivityID:  values.SortKey.ActivityID,
	})
	if err != nil {
		return "", ErrInvalidCursor
	}
	padded, err := padCursorPlaintext(payload)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, codec.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", ErrInvalidCursor
	}
	sealed := codec.aead.Seal(nonce, nonce, padded, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Bind opens a token and proves it belongs to this question, this actor, this
// generation and this moment.
//
// Unlike the search cursor, this one distinguishes invalid from expired. That is
// safe here only because the token is encrypted and authenticated: a caller
// cannot mint or mutate one, so the distinction cannot be used to probe server
// state. It is worth making because the two have different recoveries — an
// expired token replays the same query from page one, an invalid one means the
// request itself changed.
func (codec *CursorCodec) Bind(
	token string,
	query Query,
	actor recordauth.ActorScope,
	generation uint64,
	now time.Time,
) (Cursor, error) {
	queryDigest, actorDigest, err := cursorBindingDigests(query, actor)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	if token == "" || generation == 0 || now.IsZero() {
		return Cursor{}, ErrCursorInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(sealed) < codec.aead.NonceSize() {
		return Cursor{}, ErrCursorInvalid
	}
	nonce := sealed[:codec.aead.NonceSize()]
	padded, err := codec.aead.Open(nil, nonce, sealed[codec.aead.NonceSize():], nil)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	payload, err := unpadCursorPlaintext(padded)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope cursorEnvelope
	if err := decoder.Decode(&envelope); err != nil || decoder.More() {
		return Cursor{}, ErrCursorInvalid
	}
	if envelope.Version != CursorVersionV1 {
		return Cursor{}, ErrCursorExpired
	}
	if envelope.QueryDigest != hex.EncodeToString(queryDigest[:]) ||
		envelope.ActorDigest != hex.EncodeToString(actorDigest[:]) {
		return Cursor{}, ErrCursorInvalid
	}
	if envelope.Generation != generation {
		return Cursor{}, ErrCursorExpired
	}
	if envelope.ExpiresAt == 0 {
		return Cursor{}, ErrCursorInvalid
	}
	expiresAt := time.UnixMicro(envelope.ExpiresAt).UTC()
	if !now.Before(expiresAt) {
		return Cursor{}, ErrCursorExpired
	}

	var sortKey SortKey
	if envelope.EventAt != 0 || envelope.RecordedAt != 0 ||
		envelope.SourceKind != "" || envelope.ActivityID != "" {
		sortKey = SortKey{
			EventAt:    time.UnixMicro(envelope.EventAt).UTC(),
			RecordedAt: time.UnixMicro(envelope.RecordedAt).UTC(),
			SourceKind: SourceKind(envelope.SourceKind),
			ActivityID: envelope.ActivityID,
		}
		if err := validateSortKey(sortKey); err != nil {
			return Cursor{}, ErrCursorInvalid
		}
	}
	return Cursor{
		generation: envelope.Generation,
		asOf:       envelope.AsOf,
		expiresAt:  expiresAt,
		sortKey:    sortKey,
	}, nil
}

// padCursorPlaintext pads to a fixed bucket so ciphertext length carries no
// information about how many filters the query had or how large the watermark is.
func padCursorPlaintext(payload []byte) ([]byte, error) {
	if len(payload)+4 > cursorPlaintextBucket {
		return nil, ErrInvalidCursor
	}
	padded := make([]byte, cursorPlaintextBucket)
	binary.BigEndian.PutUint32(padded[:4], uint32(len(payload)))
	copy(padded[4:], payload)
	return padded, nil
}

func unpadCursorPlaintext(padded []byte) ([]byte, error) {
	if len(padded) != cursorPlaintextBucket {
		return nil, ErrInvalidCursor
	}
	length := binary.BigEndian.Uint32(padded[:4])
	if int(length)+4 > cursorPlaintextBucket {
		return nil, ErrInvalidCursor
	}
	return padded[4 : 4+length], nil
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

func validateSortKey(sortKey SortKey) error {
	// An empty sort key is a watermark token, not a page position. Encode and
	// Bind both accept it so snapshot_cursor can freeze as-of without inventing
	// a fake activity id.
	if sortKey == (SortKey{}) {
		return nil
	}
	if sortKey.EventAt.IsZero() || sortKey.RecordedAt.IsZero() {
		return ErrInvalidCursor
	}
	if !ValidSourceKind(sortKey.SourceKind) || !ValidActivityID(sortKey.ActivityID) {
		return ErrInvalidCursor
	}
	return nil
}

func isCursorInvalid(err error) bool { return errors.Is(err, ErrCursorInvalid) }
func isCursorExpired(err error) bool { return errors.Is(err, ErrCursorExpired) }

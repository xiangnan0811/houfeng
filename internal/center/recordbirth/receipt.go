package recordbirth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

const receiptDomain = "houfeng.record-birth.receipt.v1"
const maxReceiptBytes = 4096

type EventKind string

const (
	EventReserved EventKind = "reserved"
	EventPrepared EventKind = "prepared"
	EventCreated  EventKind = "created"
)

// Event is a projection of one immutable W2 event, not an authorization token.
// Canonical bytes authenticate the original binding even after it is retired;
// permission to create new effects additionally requires current admission.
type Event struct {
	Binding           Binding
	Sequence          uint64
	PreviousHash      [32]byte
	Kind              EventKind
	ReservationID     string
	RecordID          string
	Request           Request
	InitialCommitment [32]byte
}

func (event Event) Validate() error {
	if event.Binding.Validate() != nil || event.Request.Validate() != nil ||
		event.Request.ProjectID != event.Binding.ProjectID || event.Sequence == 0 || event.Sequence > math.MaxInt64 ||
		(event.PreviousHash == ([32]byte{})) != (event.Sequence == 1) ||
		!hexIdentity(event.ReservationID, "rba_", 32) || !hexIdentity(event.RecordID, "rec2_", 32) ||
		event.RecordID[5] < '8' {
		return ErrInvalid
	}
	switch event.Kind {
	case EventReserved:
		if event.InitialCommitment != ([32]byte{}) {
			return ErrInvalid
		}
	case EventPrepared, EventCreated:
		if event.InitialCommitment == ([32]byte{}) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// EncodeEvent uses source_bytes-style UTF-8 framing (uint32 length, big endian),
// fixed uint64 big-endian generation/sequence, and raw fixed-width digests.
// SQL uses exactly this order; the receipt is these bytes followed by a 32-byte
// HMAC-SHA256. The chain hash covers the complete receipt, including its MAC.
func EncodeEvent(event Event) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	texts := [...]string{
		receiptDomain, event.Binding.AuthorityID, event.Binding.LineageID,
		event.Binding.DeploymentID, event.Binding.ProjectID, event.Binding.KeyID,
		string(event.Kind), event.ReservationID, event.RecordID, event.Request.ActorID,
		string(event.Request.Purpose), event.Request.IdempotencyKey,
	}
	size := 16 + 5*sha256.Size
	for _, text := range texts {
		size += 4 + len(text)
	}
	encoded := make([]byte, 0, size)
	for _, text := range texts[:5] {
		encoded = appendText(encoded, text)
	}
	encoded = binary.BigEndian.AppendUint64(encoded, event.Binding.Generation)
	encoded = appendText(encoded, event.Binding.KeyID)
	encoded = append(encoded, event.Binding.CutoverDigest[:]...)
	encoded = binary.BigEndian.AppendUint64(encoded, event.Sequence)
	encoded = append(encoded, event.PreviousHash[:]...)
	encoded = appendText(encoded, string(event.Kind))
	encoded = appendText(encoded, event.ReservationID)
	encoded = appendText(encoded, event.RecordID)
	encoded = appendText(encoded, event.Request.ActorID)
	encoded = append(encoded, event.Request.ActorScopeDigest[:]...)
	encoded = appendText(encoded, string(event.Request.Purpose))
	encoded = appendText(encoded, event.Request.IdempotencyKey)
	encoded = append(encoded, event.Request.RequestFingerprint[:]...)
	encoded = append(encoded, event.InitialCommitment[:]...)
	return encoded, nil
}

// DecodeReceipt checks canonical structure only. It neither verifies a MAC
// without the private key nor establishes current source/cutover admission.
func DecodeReceipt(receipt []byte) (Event, error) {
	if len(receipt) < sha256.Size || len(receipt) > maxReceiptBytes {
		return Event{}, ErrInvalid
	}
	reader := receiptReader{rest: receipt[:len(receipt)-sha256.Size]}
	if reader.text(128) != receiptDomain {
		return Event{}, ErrInvalid
	}
	event := Event{}
	event.Binding.AuthorityID = reader.text(67)
	event.Binding.LineageID = reader.text(67)
	event.Binding.DeploymentID = reader.text(67)
	event.Binding.ProjectID = reader.text(128)
	event.Binding.Generation = reader.uint64()
	event.Binding.KeyID = reader.text(67)
	event.Binding.CutoverDigest = reader.digest()
	event.Sequence = reader.uint64()
	event.PreviousHash = reader.digest()
	event.Kind = EventKind(reader.text(16))
	event.ReservationID = reader.text(36)
	event.RecordID = reader.text(37)
	event.Request.ActorID = reader.text(128)
	event.Request.ActorScopeDigest = reader.digest()
	event.Request.Purpose = Purpose(reader.text(32))
	event.Request.IdempotencyKey = reader.text(200)
	event.Request.RequestFingerprint = reader.digest()
	event.InitialCommitment = reader.digest()
	event.Request.ProjectID = event.Binding.ProjectID
	if reader.failed || len(reader.rest) != 0 || event.Validate() != nil {
		return Event{}, ErrInvalid
	}
	return event, nil
}

// VerifyReceipt is for trusted key holders and codec parity checks. Runtime
// code obtains database-verified projections; private keys stay in owner tables.
func VerifyReceipt(receipt, key []byte) (Event, error) {
	event, err := DecodeReceipt(receipt)
	if err != nil || len(key) != sha256.Size {
		return Event{}, ErrInvalid
	}
	canonical := receipt[:len(receipt)-sha256.Size]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	if !hmac.Equal(mac.Sum(nil), receipt[len(canonical):]) {
		return Event{}, ErrInvalid
	}
	return event, nil
}

type receiptReader struct {
	rest   []byte
	failed bool
}

func (reader *receiptReader) take(size int) []byte {
	if reader.failed || size < 0 || size > len(reader.rest) {
		reader.failed = true
		return nil
	}
	value := reader.rest[:size]
	reader.rest = reader.rest[size:]
	return value
}

func (reader *receiptReader) text(maximum uint32) string {
	length := reader.take(4)
	if reader.failed {
		return ""
	}
	size := binary.BigEndian.Uint32(length)
	if size > maximum {
		reader.failed = true
		return ""
	}
	return string(reader.take(int(size)))
}

func (reader *receiptReader) uint64() uint64 {
	value := reader.take(8)
	if reader.failed {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (reader *receiptReader) digest() [32]byte {
	var value [32]byte
	copy(value[:], reader.take(32))
	return value
}

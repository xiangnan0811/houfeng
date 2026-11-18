package recorddeletion

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/recordplatform"
)

const (
	ledgerRecordDomain                = "HOUFENG-RECORD-DELETION-V1"
	ledgerDomainIdentityDomain        = "HOUFENG-RECORD-LEDGER-DOMAIN-V1"
	LedgerRecordRoute                 = "record_permanent_delete"
	ledgerCodecVersion         uint64 = 1
	ledgerMaxCanonicalBytes           = 1048576
)

// ErrInvalidLedgerEvidence covers malformed, noncanonical, or conflicting
// ledger evidence. It does not imply that an entry was never committed.
var ErrInvalidLedgerEvidence = errors.New("invalid record deletion ledger evidence")

// CanonicalLedgerEntry is a record-specific projection of an immutable ledger
// row. Canonical belongs to the holder; callers must not mutate shared copies.
type CanonicalLedgerEntry struct {
	DeletionLedgerEntry
	LedgerDomain [sha256.Size]byte
	PreviousHash [sha256.Size]byte
	ConfirmedAt  time.Time
	Canonical    []byte
}

// LedgerDomainDigest binds the immutable W1 domain ID, independently of the
// physical database identity and of the external continuity evidence.
func LedgerDomainDigest(id string) ([sha256.Size]byte, error) {
	if !validLedgerDomainID(id) {
		return [sha256.Size]byte{}, fmt.Errorf("%w: domain ID", ErrInvalidLedgerEvidence)
	}
	body := appendLedgerBytes(nil, []byte(ledgerDomainIdentityDomain))
	body = appendLedgerBytes(body, []byte(id))
	return sha256.Sum256(body), nil
}

// EncodeCanonicalLedgerEntry encodes only record V1 entries. It does not use
// EntryHash or Canonical as inputs; validate a stored projection with Validate.
func EncodeCanonicalLedgerEntry(entry CanonicalLedgerEntry) ([]byte, error) {
	request := entry.Request
	if request.Validate() != nil || entry.Sequence == 0 || entry.Sequence > math.MaxInt64 ||
		zeroDeletionDigest(entry.LedgerDomain) ||
		(entry.Sequence == 1 && !zeroDeletionDigest(entry.PreviousHash)) ||
		(entry.Sequence > 1 && zeroDeletionDigest(entry.PreviousHash)) ||
		(request.EntryType == LedgerEntryDeleteCommit && request.DeletionContractVersion != RecordDeletionContractVersionV1) ||
		request.ReleaseEpoch > math.MaxInt64 || request.DeletionContractVersion > math.MaxInt64 {
		return nil, fmt.Errorf("%w: record projection", ErrInvalidLedgerEvidence)
	}
	if entry.ConfirmedAt.IsZero() || entry.ConfirmedAt.Year() < 1970 || entry.ConfirmedAt.Year() > 9999 || entry.ConfirmedAt.Nanosecond()%1000 != 0 {
		return nil, fmt.Errorf("%w: confirmation time", ErrInvalidLedgerEvidence)
	}
	micros := entry.ConfirmedAt.UnixMicro()
	if micros <= 0 {
		return nil, fmt.Errorf("%w: confirmation time", ErrInvalidLedgerEvidence)
	}
	fingerprint, err := request.RequestFingerprint.PersistedBytes()
	if err != nil {
		return nil, fmt.Errorf("%w: request fingerprint", ErrInvalidLedgerEvidence)
	}
	strings := [...]string{ledgerRecordDomain, string(request.EntryType), string(request.DeploymentID),
		string(request.ProjectID), request.OperationID, request.ActorID, LedgerRecordRoute,
		request.Object.ObjectKind, request.Object.ObjectID, string(request.ReasonCode)}
	for _, value := range strings {
		if !utf8.ValidString(value) || len(value) > 4096 {
			return nil, fmt.Errorf("%w: string", ErrInvalidLedgerEvidence)
		}
	}
	body := make([]byte, 0, 512)
	body = appendLedgerBytes(body, []byte(ledgerRecordDomain))
	body = binary.BigEndian.AppendUint64(body, ledgerCodecVersion)
	body = appendLedgerBytes(body, entry.LedgerDomain[:])
	body = binary.BigEndian.AppendUint64(body, entry.Sequence)
	body = appendLedgerBytes(body, entry.PreviousHash[:])
	for _, value := range strings[1:9] {
		body = appendLedgerBytes(body, []byte(value))
	}
	body = appendLedgerBytes(body, request.TokenCommitment[:])
	body = appendLedgerBytes(body, fingerprint[:])
	body = appendLedgerBytes(body, []byte(request.ReasonCode))
	body = binary.BigEndian.AppendUint64(body, request.DeletionContractVersion)
	body = binary.BigEndian.AppendUint64(body, request.ReleaseEpoch)
	body = binary.BigEndian.AppendUint64(body, uint64(micros))
	if len(body) > ledgerMaxCanonicalBytes {
		return nil, fmt.Errorf("%w: encoded size", ErrInvalidLedgerEvidence)
	}
	return body, nil
}

// DecodeCanonicalLedgerEntry rejects all alternative encodings and derives the
// hash from the exact input. It does not establish that either database stores
// or witnessed the entry.
func DecodeCanonicalLedgerEntry(raw []byte) (CanonicalLedgerEntry, error) {
	if len(raw) == 0 || len(raw) > ledgerMaxCanonicalBytes {
		return CanonicalLedgerEntry{}, fmt.Errorf("%w: encoded size", ErrInvalidLedgerEvidence)
	}
	d := ledgerDecoder{rest: raw}
	domain := d.text(128)
	version := d.uint64()
	var result CanonicalLedgerEntry
	copy(result.LedgerDomain[:], d.fixed(sha256.Size))
	result.Sequence = d.uint64()
	copy(result.PreviousHash[:], d.fixed(sha256.Size))
	result.Request.EntryType = LedgerEntryType(d.text(128))
	result.Request.DeploymentID = recordplatform.DeploymentID(d.text(128))
	result.Request.ProjectID = recordplatform.ProjectID(d.text(128))
	result.Request.OperationID = d.text(128)
	result.Request.ActorID = d.text(128)
	route := d.text(128)
	result.Request.Object.ProjectID = string(result.Request.ProjectID)
	result.Request.Object.ObjectKind = d.text(128)
	result.Request.Object.ObjectID = d.text(128)
	copy(result.Request.TokenCommitment[:], d.fixed(sha256.Size))
	fingerprint := d.fixed(sha256.Size)
	result.Request.ReasonCode = DeletionReasonCode(d.text(128))
	result.Request.DeletionContractVersion = d.uint64()
	result.Request.ReleaseEpoch = d.uint64()
	micros := d.uint64()
	if d.err != nil || len(d.rest) != 0 || domain != ledgerRecordDomain || version != ledgerCodecVersion ||
		route != LedgerRecordRoute || micros == 0 || micros > math.MaxInt64 {
		return CanonicalLedgerEntry{}, fmt.Errorf("%w: canonical fields", ErrInvalidLedgerEvidence)
	}
	var err error
	result.Request.RequestFingerprint, err = recordplatform.ParseTrustedPersistedRequestFingerprintV1(fingerprint)
	if err != nil {
		return CanonicalLedgerEntry{}, fmt.Errorf("%w: request fingerprint", ErrInvalidLedgerEvidence)
	}
	result.ConfirmedAt = time.UnixMicro(int64(micros)).UTC()
	encoded, err := EncodeCanonicalLedgerEntry(result)
	if err != nil || !bytes.Equal(encoded, raw) {
		return CanonicalLedgerEntry{}, fmt.Errorf("%w: noncanonical entry", ErrInvalidLedgerEvidence)
	}
	result.Canonical = append([]byte(nil), raw...)
	result.EntryHash = sha256.Sum256(raw)
	return result, nil
}

// Validate compares the canonical bytes with every record projection field,
// including the entry hash. It must be applied to each independently read row.
func (entry CanonicalLedgerEntry) Validate() error {
	decoded, err := DecodeCanonicalLedgerEntry(entry.Canonical)
	if err != nil || entry.EntryHash != decoded.EntryHash || entry.LedgerDomain != decoded.LedgerDomain ||
		entry.PreviousHash != decoded.PreviousHash || entry.Sequence != decoded.Sequence ||
		!entry.ConfirmedAt.Equal(decoded.ConfirmedAt) || !sameLedgerAppendRequest(entry.Request, decoded.Request) {
		return fmt.Errorf("%w: projection mismatch", ErrInvalidLedgerEvidence)
	}
	return nil
}

func appendLedgerBytes(dst, value []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

type ledgerDecoder struct {
	rest []byte
	err  error
}

func (d *ledgerDecoder) take(n int) []byte {
	if d.err != nil || n < 0 || n > len(d.rest) {
		d.err = ErrInvalidLedgerEvidence
		return nil
	}
	value := d.rest[:n]
	d.rest = d.rest[n:]
	return value
}

func (d *ledgerDecoder) uint64() uint64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func (d *ledgerDecoder) raw(max int) []byte {
	b := d.take(4)
	if b == nil {
		return nil
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(max) || uint64(n) > uint64(len(d.rest)) {
		d.err = ErrInvalidLedgerEvidence
		return nil
	}
	return d.take(int(n))
}

func (d *ledgerDecoder) fixed(size int) []byte {
	b := d.raw(size)
	if len(b) != size {
		d.err = ErrInvalidLedgerEvidence
	}
	return b
}

func (d *ledgerDecoder) text(max int) string {
	b := d.raw(max)
	if !utf8.Valid(b) {
		d.err = ErrInvalidLedgerEvidence
	}
	return string(b)
}

func validLedgerDomainID(id string) bool {
	if len(id) != 67 || id[:3] != "rd-" {
		return false
	}
	for i := 3; i < len(id); i++ {
		if (id[i] < '0' || id[i] > '9') && (id[i] < 'a' || id[i] > 'f') {
			return false
		}
	}
	return true
}

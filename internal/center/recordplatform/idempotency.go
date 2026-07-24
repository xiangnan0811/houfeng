package recordplatform

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidIdempotencyKey    = errors.New("invalid idempotency key")
	ErrInvalidIdempotencyClaim  = errors.New("invalid idempotency claim")
	ErrInvalidIdempotencyRecord = errors.New("invalid idempotency record")
	ErrIdempotencyKeyReused     = errors.New("idempotency key reused")
	ErrIdempotencyInProgress    = errors.New("idempotency operation in progress")
	ErrIdempotencyConflictState = errors.New("idempotency conflict state")
	ErrLostOwnerLease           = errors.New("lost owner lease")
)

// IdempotencyKey is the durable serialization key for a single v1 operation.
// The project, operation kind, and client key must all be exact canonical
// values; no normalization is applied.
type IdempotencyKey struct {
	ProjectID     ProjectID
	OperationKind OperationKind
	Key           string
}

// IdempotencyStatus is the closed persisted state registry.
type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "in_progress"
	IdempotencyStatusCompleted  IdempotencyStatus = "completed"
	IdempotencyStatusConflict   IdempotencyStatus = "conflict"
)

// IdempotencyRecordV1 is the content-free logical shape of an observed
// idempotency row. Completion has no live Owner and carries only a result
// fingerprint; it never returns application content.
type IdempotencyRecordV1 struct {
	Key                IdempotencyKey
	RequestFingerprint PersistedRequestFingerprintV1
	ResultFingerprint  *PersistedRequestFingerprintV1
	Status             IdempotencyStatus
	Owner              *OwnerLease
	ExpiresAt          time.Time
}

// IdempotencyAction identifies the only non-error same-fingerprint outcomes
// an observed row can have.
type IdempotencyAction uint8

const (
	IdempotencyActionReplay IdempotencyAction = iota + 1
	IdempotencyActionTakeover
)

// IdempotencyResolutionV1 intentionally carries only a stored result digest
// for replay; it cannot carry a response body or request payload.
type IdempotencyResolutionV1 struct {
	Action            IdempotencyAction
	ResultFingerprint PersistedRequestFingerprintV1
}

// IdempotencyClaimInputV1 supplies only durable identity and lease durations.
// The store derives all timestamps from transaction_timestamp(), never from a
// caller-provided expiration time.
type IdempotencyClaimInputV1 struct {
	Key                IdempotencyKey
	RequestFingerprint RequestFingerprintV1
	OwnerID            string
	OwnerLeaseDuration time.Duration
	RecordTTL          time.Duration
}

// IdempotencyClaimResultV1 contains either an acquired owner fence or a
// replayable result fingerprint. It never carries request/response content.
type IdempotencyClaimResultV1 struct {
	Owner        *OwnerLease
	ReplayResult *PersistedRequestFingerprintV1
}

// IdempotencyRenewInputV1 carries the exact active owner fence and the two
// persisted duration bounds for a renewal. Both durations are normalized to
// PostgreSQL microseconds before their strict ordering is accepted.
type IdempotencyRenewInputV1 struct {
	Key                IdempotencyKey
	Owner              OwnerLease
	OwnerLeaseDuration time.Duration
	RecordTTL          time.Duration
}

// Validate rejects claims that cannot satisfy the persisted expiry invariant:
// the idempotency row must outlive its active owner lease.
func (input IdempotencyClaimInputV1) Validate() error {
	if err := input.Key.Validate(); err != nil {
		return fmt.Errorf("%w: key", ErrInvalidIdempotencyClaim)
	}
	if err := input.RequestFingerprint.Validate(); err != nil {
		return fmt.Errorf("%w: request fingerprint", ErrInvalidIdempotencyClaim)
	}
	if !validRecordPlatformOwnerID(input.OwnerID) || !validIdempotencyExpiryDurations(input.OwnerLeaseDuration, input.RecordTTL) {
		return fmt.Errorf("%w: owner or expiry", ErrInvalidIdempotencyClaim)
	}
	return nil
}

// Validate rejects renewals whose normalized row expiry could meet or precede
// the renewed owner expiry.
func (input IdempotencyRenewInputV1) Validate() error {
	if err := input.Key.Validate(); err != nil {
		return fmt.Errorf("%w: key", ErrInvalidIdempotencyClaim)
	}
	if err := input.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner", ErrInvalidIdempotencyClaim)
	}
	if !validIdempotencyExpiryDurations(input.OwnerLeaseDuration, input.RecordTTL) {
		return fmt.Errorf("%w: owner or expiry", ErrInvalidIdempotencyClaim)
	}
	return nil
}

// Validate validates the persisted logical shape before the store makes a
// decision from it. A legacy conflict row is structurally readable but must be
// handled fail-closed by ResolveIdempotencyV1.
func (record IdempotencyRecordV1) Validate() error {
	if err := record.Key.Validate(); err != nil {
		return fmt.Errorf("%w: key", ErrInvalidIdempotencyRecord)
	}
	if err := record.RequestFingerprint.Validate(); err != nil {
		return fmt.Errorf("%w: request fingerprint", ErrInvalidIdempotencyRecord)
	}
	if record.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: expiry", ErrInvalidIdempotencyRecord)
	}

	switch record.Status {
	case IdempotencyStatusInProgress:
		if record.Owner == nil || record.Owner.Validate() != nil || record.ResultFingerprint != nil || !record.ExpiresAt.After(record.Owner.ExpiresAt) {
			return fmt.Errorf("%w: in progress ownership", ErrInvalidIdempotencyRecord)
		}
	case IdempotencyStatusCompleted:
		if record.Owner != nil || record.ResultFingerprint == nil || record.ResultFingerprint.Validate() != nil || isZeroPersistedRequestFingerprint(*record.ResultFingerprint) {
			return fmt.Errorf("%w: completed result", ErrInvalidIdempotencyRecord)
		}
	case IdempotencyStatusConflict:
		// An inherited conflict row cannot be repaired or overwritten here.
	default:
		return fmt.Errorf("%w: status", ErrInvalidIdempotencyRecord)
	}
	return nil
}

// Validate verifies the closed compound idempotency key.
func (key IdempotencyKey) Validate() error {
	if err := ValidateProjectID(key.ProjectID); err != nil {
		return fmt.Errorf("%w: project", ErrInvalidIdempotencyKey)
	}
	if err := ValidateOperationKind(key.OperationKind); err != nil {
		return fmt.Errorf("%w: operation", ErrInvalidIdempotencyKey)
	}
	if !validIdempotencyKeyValue(key.Key) {
		return fmt.Errorf("%w: key", ErrInvalidIdempotencyKey)
	}
	return nil
}

// ResolveIdempotencyV1 classifies an observed row using an observed database
// timestamp. The store, not this helper, performs all actual owner-fenced SQL
// transitions at transaction_timestamp().
func ResolveIdempotencyV1(record IdempotencyRecordV1, request RequestFingerprintV1, observedDBTime time.Time) (IdempotencyResolutionV1, error) {
	if err := record.Validate(); err != nil {
		return IdempotencyResolutionV1{}, err
	}
	if err := request.Validate(); err != nil {
		return IdempotencyResolutionV1{}, err
	}
	if observedDBTime.IsZero() {
		return IdempotencyResolutionV1{}, fmt.Errorf("%w: observed database time", ErrInvalidIdempotencyRecord)
	}
	if record.Status == IdempotencyStatusConflict {
		return IdempotencyResolutionV1{}, ErrIdempotencyConflictState
	}
	if !requestFingerprintV1MatchesPersisted(request, record.RequestFingerprint) {
		return IdempotencyResolutionV1{}, ErrIdempotencyKeyReused
	}

	switch record.Status {
	case IdempotencyStatusCompleted:
		return IdempotencyResolutionV1{
			Action:            IdempotencyActionReplay,
			ResultFingerprint: *record.ResultFingerprint,
		}, nil
	case IdempotencyStatusInProgress:
		if !record.Owner.ExpiresAt.After(observedDBTime) {
			return IdempotencyResolutionV1{Action: IdempotencyActionTakeover}, nil
		}
		return IdempotencyResolutionV1{}, ErrIdempotencyInProgress
	default:
		return IdempotencyResolutionV1{}, fmt.Errorf("%w: status", ErrInvalidIdempotencyRecord)
	}
}

// RequireLiveOwnerFenceV1 applies the same owner-id/generation and observed
// live-expiry contract that store SQL uses. It never grants authority based on
// a caller's local time.
func RequireLiveOwnerFenceV1(expected, actual OwnerLease, observedDBTime time.Time) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	if observedDBTime.IsZero() || actual.Validate() != nil || actual.OwnerID != expected.OwnerID || actual.Generation != expected.Generation || !actual.ExpiresAt.After(observedDBTime) {
		return ErrLostOwnerLease
	}
	return nil
}

func validIdempotencyKeyValue(value string) bool {
	if len(value) == 0 || len(value) > 200 {
		return false
	}
	if isDeletionRequestTokenTransportEncoding(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '.' && character != '_' && character != '~' && character != '-' {
			return false
		}
	}
	return true
}

func isZeroPersistedRequestFingerprint(fingerprint PersistedRequestFingerprintV1) bool {
	return fingerprint.digest == [32]byte{}
}

func validIdempotencyExpiryDurations(ownerLeaseDuration, recordTTL time.Duration) bool {
	ownerLeaseMicroseconds := ownerLeaseDuration.Microseconds()
	recordTTLMicroseconds := recordTTL.Microseconds()
	return ownerLeaseMicroseconds > 0 && recordTTLMicroseconds > ownerLeaseMicroseconds
}

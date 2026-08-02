package recordplatform

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalidLease                   = errors.New("invalid lease")
	ErrInvalidClientContentKey        = errors.New("invalid client content lease key")
	ErrLeaseAlreadyHeld               = errors.New("lease already held")
	ErrLeaseRenewalStopped            = errors.New("lease renewal stopped")
	ErrInvalidReservationFence        = errors.New("invalid reservation fence")
	ErrDeletionReservationUnavailable = errors.New("deletion reservation unavailable")
	ErrContentDeliveryEpochMissing    = errors.New("content delivery epoch missing")
	ErrDeletionFenceLeaseLive         = errors.New("deletion fence lease live")
	ErrObjectContentLeaseLive         = errors.New("object content lease live")
)

// LeaseClaimInputV1 supplies the immutable owner identity and a duration that
// the store will apply using PostgreSQL transaction time.
type LeaseClaimInputV1 struct {
	OwnerID       string
	LeaseDuration time.Duration
}

// DeletionFenceLeaseV1 blocks serving when it is live for its object.
type DeletionFenceLeaseV1 struct {
	Object ObjectRef
	Owner  OwnerLease
}

// ObjectContentLeaseV1 is the object-specific input to an in-memory serving
// permit. It is distinct from a client-content lease.
type ObjectContentLeaseV1 struct {
	Object ObjectRef
	Owner  OwnerLease
}

// ClientContentLeaseKeyV1 identifies a client-local content holder. It has no
// object identity and therefore cannot prove object serving authority.
type ClientContentLeaseKeyV1 struct {
	ProjectID string
	ClientID  string
}

// ClientContentLeaseV1 carries a client owner fence without object authority.
type ClientContentLeaseV1 struct {
	Key   ClientContentLeaseKeyV1
	Owner OwnerLease
}

// ServingLeaseV1 is an opaque in-memory description of a serving assertion.
// It carries the database-returned object owner fence and epoch, but is never
// itself an authority decision. Store-backed assertion must re-read all live
// facts before serving.
type ServingLeaseV1 struct {
	Object        ObjectRef
	Owner         OwnerLease
	CapturedEpoch ContentEpoch
}

// ReservationFenceInputV1 identifies a live preview reservation and the new
// object-global owner that will fence its delivery epoch.
type ReservationFenceInputV1 struct {
	ReservationID      string
	Object             ObjectRef
	OwnerID            string
	OwnerLeaseDuration time.Duration
}

// DeletionReservationFenceV1 is the compound reservation/fence token. Its
// owner generation must match the deletion-fence lease written atomically.
type DeletionReservationFenceV1 struct {
	ReservationID string
	Object        ObjectRef
	FenceEpoch    ContentEpoch
	Owner         OwnerLease
}

// ExpiredPrimitiveCleanupResultV1 reports only ephemeral primitive rows. It
// intentionally has no reservation, operation, ledger, or content fields.
type ExpiredPrimitiveCleanupResultV1 struct {
	IdempotencyKeys        int64
	OutboxEvents           int64
	IdentityMutationGuards int64
	DeletionFenceLeases    int64
	ObjectContentLeases    int64
	ClientContentLeases    int64
}

// LeaseWorkGuardV1 tracks a locally observed durable owner only for scheduling.
// It never extends authority locally; a failed store renewal stops work at once.
type LeaseWorkGuardV1 struct {
	mu       sync.Mutex
	clock    Clock
	owner    OwnerLease
	stopped  bool
	renewing bool
}

// Validate rejects claim inputs that cannot be represented by database-time
// microsecond arithmetic.
func (input LeaseClaimInputV1) Validate() error {
	if !validRecordPlatformOwnerID(input.OwnerID) || input.LeaseDuration.Microseconds() <= 0 {
		return fmt.Errorf("%w: owner or duration", ErrInvalidLease)
	}
	return nil
}

// NewLeaseWorkGuardV1 creates a conservative scheduling guard for a live
// observed owner fence.
func NewLeaseWorkGuardV1(clock Clock, owner OwnerLease) (*LeaseWorkGuardV1, error) {
	if isNilRecordPlatformDependency(clock) || owner.Validate() != nil {
		return nil, fmt.Errorf("%w: work guard", ErrInvalidLease)
	}
	return &LeaseWorkGuardV1{clock: clock, owner: owner}, nil
}

// CanContinue reports only whether locally scheduled work must stop. Durable
// writes still require a live owner predicate inside PostgreSQL.
func (guard *LeaseWorkGuardV1) CanContinue() bool {
	if guard == nil {
		return false
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	return guard.canContinueLocked()
}

// Renew invokes a store-backed renewal callback. Any failure, malformed owner,
// or owner-identity/generation drift permanently stops this local work guard.
func (guard *LeaseWorkGuardV1) Renew(renew func(OwnerLease) (OwnerLease, error)) error {
	if guard == nil {
		return ErrLeaseRenewalStopped
	}
	if renew == nil {
		guard.mu.Lock()
		guard.stopped = true
		guard.mu.Unlock()
		return ErrLeaseRenewalStopped
	}

	guard.mu.Lock()
	if guard.renewing || !guard.canContinueLocked() {
		guard.stopped = true
		guard.mu.Unlock()
		return ErrLeaseRenewalStopped
	}
	owner := guard.owner
	guard.renewing = true
	guard.mu.Unlock()

	renewed, err := renew(owner)

	guard.mu.Lock()
	defer guard.mu.Unlock()
	guard.renewing = false
	if guard.stopped || err != nil || renewed.Validate() != nil || renewed.OwnerID != owner.OwnerID || renewed.Generation != owner.Generation || !renewed.LocallyLive(guard.clock) {
		guard.stopped = true
		if err != nil {
			return fmt.Errorf("%w: %w", ErrLeaseRenewalStopped, err)
		}
		return ErrLeaseRenewalStopped
	}
	guard.owner = renewed
	return nil
}

func (guard *LeaseWorkGuardV1) canContinueLocked() bool {
	return !guard.stopped && !guard.renewing && guard.owner.LocallyLive(guard.clock)
}

// Validate rejects noncanonical reservation keys and lease durations before a
// reservation/epoch/fence transaction starts.
func (input ReservationFenceInputV1) Validate() error {
	if !validDeletionReservationID(input.ReservationID) {
		return fmt.Errorf("%w: reservation id", ErrInvalidReservationFence)
	}
	if err := input.Object.Validate(); err != nil {
		return fmt.Errorf("%w: object: %w", ErrInvalidReservationFence, err)
	}
	if err := (LeaseClaimInputV1{OwnerID: input.OwnerID, LeaseDuration: input.OwnerLeaseDuration}).Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidReservationFence, err)
	}
	return nil
}

// Validate verifies a compound reservation fence before renew/release/assert
// paths use its owner triple.
func (fence DeletionReservationFenceV1) Validate() error {
	if !validDeletionReservationID(fence.ReservationID) {
		return fmt.Errorf("%w: reservation id", ErrInvalidReservationFence)
	}
	if fence.FenceEpoch == 0 {
		return fmt.Errorf("%w: fence epoch", ErrInvalidReservationFence)
	}
	if err := fence.Object.Validate(); err != nil {
		return fmt.Errorf("%w: object: %w", ErrInvalidReservationFence, err)
	}
	if err := fence.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidReservationFence, err)
	}
	return nil
}

// Validate verifies a deletion-fence token.
func (lease DeletionFenceLeaseV1) Validate() error {
	if err := lease.Object.Validate(); err != nil {
		return fmt.Errorf("%w: object: %w", ErrInvalidLease, err)
	}
	if err := lease.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidLease, err)
	}
	return nil
}

// Validate verifies an object-content token.
func (lease ObjectContentLeaseV1) Validate() error {
	if err := lease.Object.Validate(); err != nil {
		return fmt.Errorf("%w: object: %w", ErrInvalidLease, err)
	}
	if err := lease.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidLease, err)
	}
	return nil
}

// Validate verifies a client-content key without accepting object identity.
func (key ClientContentLeaseKeyV1) Validate() error {
	if err := ValidateProjectID(ProjectID(key.ProjectID)); err != nil {
		return fmt.Errorf("%w: project", ErrInvalidClientContentKey)
	}
	if !validRecordPlatformOwnerID(key.ClientID) {
		return fmt.Errorf("%w: client", ErrInvalidClientContentKey)
	}
	return nil
}

// Validate verifies a client-content token.
func (lease ClientContentLeaseV1) Validate() error {
	if err := lease.Key.Validate(); err != nil {
		return err
	}
	if err := lease.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidLease, err)
	}
	return nil
}

// Validate checks the structural token returned by a database-backed serving
// acquisition. It deliberately cannot establish serving authority on its own.
func (lease ServingLeaseV1) Validate() error {
	if err := lease.Object.Validate(); err != nil {
		return fmt.Errorf("%w: object: %w", ErrInvalidLease, err)
	}
	if err := lease.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidLease, err)
	}
	return nil
}

// NewServingLeaseV1 converts only an object-content lease into a structural
// serving token; callers cannot substitute a client-content lease. It is not
// a local authorization check: callers must use the repository assertion that
// reads current epoch and deletion-fence state from PostgreSQL.
func NewServingLeaseV1(objectLease ObjectContentLeaseV1, capturedEpoch ContentEpoch) (ServingLeaseV1, error) {
	if err := objectLease.Validate(); err != nil {
		return ServingLeaseV1{}, err
	}
	lease := ServingLeaseV1{Object: objectLease.Object, Owner: objectLease.Owner, CapturedEpoch: capturedEpoch}
	if err := lease.Validate(); err != nil {
		return ServingLeaseV1{}, err
	}
	return lease, nil
}

func validDeletionReservationID(value string) bool {
	if len(value) < len("drs_")+1 || len(value) > len("drs_")+64 || value[:len("drs_")] != "drs_" {
		return false
	}
	for _, character := range value[len("drs_"):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

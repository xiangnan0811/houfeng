package recordplatform

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrInvalidObjectRef             = errors.New("invalid object reference")
	ErrInvalidIdentityMutationGuard = errors.New("invalid identity mutation guard")
)

// ObjectRef identifies a record-platform object without carrying its content.
type ObjectRef struct {
	ProjectID  string
	ObjectKind string
	ObjectID   string
}

// RecordPlatformRelationV1 identifies a relation in the only permitted
// multi-relation lock order.
type RecordPlatformRelationV1 string

const (
	RecordPlatformRelationIdempotencyKeys        RecordPlatformRelationV1 = "record_idempotency_keys"
	RecordPlatformRelationIdentityMutationGuards RecordPlatformRelationV1 = "identity_mutation_guards"
	RecordPlatformRelationDeletionReservations   RecordPlatformRelationV1 = "deletion_reservations"
	RecordPlatformRelationContentDeliveryEpochs  RecordPlatformRelationV1 = "content_delivery_epochs"
	RecordPlatformRelationDeletionFenceLeases    RecordPlatformRelationV1 = "deletion_fence_leases"
	RecordPlatformRelationObjectContentLeases    RecordPlatformRelationV1 = "object_content_leases"
	RecordPlatformRelationClientContentLeases    RecordPlatformRelationV1 = "client_content_leases"
	RecordPlatformRelationOutbox                 RecordPlatformRelationV1 = "record_outbox"
)

var recordPlatformRelationLockOrderV1 = [...]RecordPlatformRelationV1{
	RecordPlatformRelationIdempotencyKeys,
	RecordPlatformRelationIdentityMutationGuards,
	RecordPlatformRelationDeletionReservations,
	RecordPlatformRelationContentDeliveryEpochs,
	RecordPlatformRelationDeletionFenceLeases,
	RecordPlatformRelationObjectContentLeases,
	RecordPlatformRelationClientContentLeases,
	RecordPlatformRelationOutbox,
}

// RecordPlatformRelationLockOrderV1 returns a copy so callers cannot mutate
// the fixed order used by storage transactions.
func RecordPlatformRelationLockOrderV1() []RecordPlatformRelationV1 {
	return append([]RecordPlatformRelationV1(nil), recordPlatformRelationLockOrderV1[:]...)
}

// IdentityMutationGuardKeyV1 serializes an identity-changing operation for an
// object. It intentionally contains no mutation payload or display data.
type IdentityMutationGuardKeyV1 struct {
	Object       ObjectRef
	MutationKind string
}

// IdentityMutationGuardV1 carries a durable owner fence for one guard key.
type IdentityMutationGuardV1 struct {
	Key   IdentityMutationGuardKeyV1
	Owner OwnerLease
}

// Validate rejects unregistered project values and noncanonical storage keys.
func (object ObjectRef) Validate() error {
	if err := ValidateProjectID(ProjectID(object.ProjectID)); err != nil {
		return fmt.Errorf("%w: project", ErrInvalidObjectRef)
	}
	if !validRecordPlatformObjectKind(object.ObjectKind) {
		return fmt.Errorf("%w: object kind", ErrInvalidObjectRef)
	}
	if !validRecordPlatformObjectID(object.ObjectID) {
		return fmt.Errorf("%w: object id", ErrInvalidObjectRef)
	}
	return nil
}

// Validate verifies one durable guard serialization key.
func (key IdentityMutationGuardKeyV1) Validate() error {
	if err := key.Object.Validate(); err != nil {
		return fmt.Errorf("%w: object: %w", ErrInvalidIdentityMutationGuard, err)
	}
	if !validRecordPlatformObjectKind(key.MutationKind) {
		return fmt.Errorf("%w: mutation kind", ErrInvalidIdentityMutationGuard)
	}
	return nil
}

// Validate verifies a durable guard token before it reaches a fenced store
// operation.
func (guard IdentityMutationGuardV1) Validate() error {
	if err := guard.Key.Validate(); err != nil {
		return err
	}
	if err := guard.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidIdentityMutationGuard, err)
	}
	return nil
}

// CanonicalIdentityMutationGuardKeysV1 validates, copies, and sorts guard
// keys before a multi-key acquisition. Duplicate keys are rejected rather than
// silently issuing two owner claims for the same row.
func CanonicalIdentityMutationGuardKeysV1(keys []IdentityMutationGuardKeyV1) ([]IdentityMutationGuardKeyV1, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no keys", ErrInvalidIdentityMutationGuard)
	}
	canonical := append([]IdentityMutationGuardKeyV1(nil), keys...)
	for _, key := range canonical {
		if err := key.Validate(); err != nil {
			return nil, err
		}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return compareIdentityMutationGuardKeys(canonical[left], canonical[right]) < 0
	})
	for index := 1; index < len(canonical); index++ {
		if compareIdentityMutationGuardKeys(canonical[index-1], canonical[index]) == 0 {
			return nil, fmt.Errorf("%w: duplicate key", ErrInvalidIdentityMutationGuard)
		}
	}
	return canonical, nil
}

// CanonicalObjectRefsV1 validates, copies, and sorts object keys for callers
// that acquire a homogeneous lease relation in one transaction.
func CanonicalObjectRefsV1(objects []ObjectRef) ([]ObjectRef, error) {
	if len(objects) == 0 {
		return nil, fmt.Errorf("%w: no objects", ErrInvalidObjectRef)
	}
	canonical := append([]ObjectRef(nil), objects...)
	for _, object := range canonical {
		if err := object.Validate(); err != nil {
			return nil, err
		}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return compareObjectRefs(canonical[left], canonical[right]) < 0
	})
	for index := 1; index < len(canonical); index++ {
		if compareObjectRefs(canonical[index-1], canonical[index]) == 0 {
			return nil, fmt.Errorf("%w: duplicate object", ErrInvalidObjectRef)
		}
	}
	return canonical, nil
}

func compareIdentityMutationGuardKeys(left, right IdentityMutationGuardKeyV1) int {
	if comparison := compareObjectRefs(left.Object, right.Object); comparison != 0 {
		return comparison
	}
	return compareStrings(left.MutationKind, right.MutationKind)
}

func compareObjectRefs(left, right ObjectRef) int {
	if comparison := compareStrings(left.ProjectID, right.ProjectID); comparison != 0 {
		return comparison
	}
	if comparison := compareStrings(left.ObjectKind, right.ObjectKind); comparison != 0 {
		return comparison
	}
	return compareStrings(left.ObjectID, right.ObjectID)
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func validRecordPlatformObjectKind(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	if isDeletionRequestTokenTransportEncoding(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' {
			return false
		}
	}
	return true
}

func validRecordPlatformObjectID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	if isDeletionRequestTokenTransportEncoding(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' {
			return false
		}
	}
	return true
}

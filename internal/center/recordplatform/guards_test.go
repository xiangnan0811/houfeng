package recordplatform

import (
	"errors"
	"reflect"
	"testing"
)

func TestCanonicalIdentityMutationGuardKeysV1SortsCanonicalTuplesAndRejectsDuplicates(t *testing.T) {
	first := IdentityMutationGuardKeyV1{
		Object:       ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: "rec_01"},
		MutationKind: "rename",
	}
	second := IdentityMutationGuardKeyV1{
		Object:       ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: "rec_02"},
		MutationKind: "rename",
	}

	got, err := CanonicalIdentityMutationGuardKeysV1([]IdentityMutationGuardKeyV1{second, first})
	if err != nil {
		t.Fatalf("CanonicalIdentityMutationGuardKeysV1() error = %v", err)
	}
	if want := []IdentityMutationGuardKeyV1{first, second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CanonicalIdentityMutationGuardKeysV1() = %#v, want %#v", got, want)
	}
	if _, err := CanonicalIdentityMutationGuardKeysV1([]IdentityMutationGuardKeyV1{first, first}); !errors.Is(err, ErrInvalidIdentityMutationGuard) {
		t.Fatalf("CanonicalIdentityMutationGuardKeysV1 duplicate error = %v, want ErrInvalidIdentityMutationGuard", err)
	}
}

func TestRecordPlatformRelationLockOrderV1IsFixed(t *testing.T) {
	want := []RecordPlatformRelationV1{
		RecordPlatformRelationIdempotencyKeys,
		RecordPlatformRelationIdentityMutationGuards,
		RecordPlatformRelationDeletionReservations,
		RecordPlatformRelationContentDeliveryEpochs,
		RecordPlatformRelationDeletionFenceLeases,
		RecordPlatformRelationObjectContentLeases,
		RecordPlatformRelationClientContentLeases,
		RecordPlatformRelationOutbox,
	}
	if got := RecordPlatformRelationLockOrderV1(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordPlatformRelationLockOrderV1() = %#v, want %#v", got, want)
	}
}

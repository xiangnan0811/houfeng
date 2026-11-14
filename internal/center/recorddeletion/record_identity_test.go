package recorddeletion

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/recordplatform"
)

func TestBackupInventoryAcceptsRecordIdentityDomains(t *testing.T) {
	for _, recordID := range []string{
		"rec_legacy",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		t.Run(recordID, func(t *testing.T) {
			object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID}
			snapshot, err := ReadDeletionBackupInventory(context.Background(), UnavailableDeletionBackupInventorySource{}, object)
			if err != nil || snapshot.Availability != BackupInventoryUnavailable {
				t.Fatalf("ReadDeletionBackupInventory() = %#v, %v", snapshot, err)
			}
		})
	}
	for _, recordID := range []string{"rrv_revision", "rec2_7000000000000000000000000000000g"} {
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID}
		if _, err := ReadDeletionBackupInventory(context.Background(), UnavailableDeletionBackupInventorySource{}, object); !errors.Is(err, ErrDeletionSafetyUnavailable) {
			t.Fatalf("ReadDeletionBackupInventory(%q) error = %v", recordID, err)
		}
	}
}

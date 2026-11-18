package evidence

import (
	"crypto/sha256"
	"fmt"

	"houfeng/internal/center/recordidentity"
)

// ImportedSnapshotRoute names the persisted, actor-owned import plan that
// allocated the destination record and this snapshot identity.
type ImportedSnapshotRoute struct {
	ImportJobID  string
	ImportPlanID string
}

// PreparedImportedSnapshot is a kind-agnostic snapshot reconstructed from an
// official archive restore wrapper. Import persist uses this path instead of
// live capture intents or forged comparison-save tokens.
type PreparedImportedSnapshot struct {
	recordID   string
	snapshotID string
	snapshot   CanonicalSnapshot
	route      ImportedSnapshotRoute
}

func NewPreparedImportedSnapshot(recordID, snapshotID string, snapshot CanonicalSnapshot, route ImportedSnapshotRoute) (PreparedImportedSnapshot, error) {
	item := PreparedImportedSnapshot{recordID: recordID, snapshotID: snapshotID, snapshot: snapshot, route: route}
	if err := item.Validate(); err != nil {
		return PreparedImportedSnapshot{}, err
	}
	return item, nil
}

func (item PreparedImportedSnapshot) RecordID() string { return item.recordID }

func (item PreparedImportedSnapshot) SnapshotID() string { return item.snapshotID }

func (item PreparedImportedSnapshot) Snapshot() CanonicalSnapshot { return item.snapshot }

func (item PreparedImportedSnapshot) ImportRoute() ImportedSnapshotRoute { return item.route }

func (item PreparedImportedSnapshot) Empty() bool {
	return item.recordID == "" && item.snapshotID == "" && item.snapshot.Size() == 0 && item.route == (ImportedSnapshotRoute{})
}

func (item PreparedImportedSnapshot) Validate() error {
	if !recordidentity.IsValid(item.recordID) || !ValidSnapshotID(item.snapshotID) ||
		validateKnownKindKey(item.snapshot.Envelope().Key) != nil ||
		!validClosedPreparedID(item.route.ImportJobID, "rij_") || !validClosedPreparedID(item.route.ImportPlanID, "rip_") ||
		item.snapshot.Hash() == [sha256.Size]byte{} || item.snapshot.Size() == 0 {
		return fmt.Errorf("%w: imported snapshot", ErrInvalidRevisionPreparation)
	}
	return nil
}

func cloneImportedSnapshots(values []PreparedImportedSnapshot) []PreparedImportedSnapshot {
	if len(values) == 0 {
		return nil
	}
	return append([]PreparedImportedSnapshot(nil), values...)
}

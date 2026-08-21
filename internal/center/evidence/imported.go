package evidence

import (
	"crypto/sha256"
	"fmt"
)

// PreparedImportedSnapshot is a kind-agnostic snapshot reconstructed from an
// official archive restore wrapper. Import persist uses this path instead of
// live capture intents or forged comparison-save tokens.
type PreparedImportedSnapshot struct {
	recordID   string
	snapshotID string
	snapshot   CanonicalSnapshot
}

func NewPreparedImportedSnapshot(recordID, snapshotID string, snapshot CanonicalSnapshot) (PreparedImportedSnapshot, error) {
	item := PreparedImportedSnapshot{recordID: recordID, snapshotID: snapshotID, snapshot: snapshot}
	if err := item.Validate(); err != nil {
		return PreparedImportedSnapshot{}, err
	}
	return item, nil
}

func (item PreparedImportedSnapshot) RecordID() string { return item.recordID }

func (item PreparedImportedSnapshot) SnapshotID() string { return item.snapshotID }

func (item PreparedImportedSnapshot) Snapshot() CanonicalSnapshot { return item.snapshot }

func (item PreparedImportedSnapshot) Empty() bool {
	return item.recordID == "" && item.snapshotID == "" && item.snapshot.Size() == 0
}

func (item PreparedImportedSnapshot) Validate() error {
	if !validClosedPreparedID(item.recordID, "rec_") || !ValidSnapshotID(item.snapshotID) ||
		validateKnownKindKey(item.snapshot.Envelope().Key) != nil ||
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

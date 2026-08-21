package evidence

import (
	"errors"
	"reflect"
	"testing"
)

func TestPreparedImportedSnapshotRejectsUnknownKindAndAcceptsKnownSnapshot(t *testing.T) {
	t.Parallel()

	kind, err := NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustComparisonResultSnapshot(t, kind, comparisonResultTestPayload("evs_leftcopy", "evs_rightcopy"))
	if _, err := NewPreparedImportedSnapshot("rec_imported01", "evs_imported01", CanonicalSnapshot{}); !errors.Is(err, ErrInvalidRevisionPreparation) {
		t.Fatalf("empty snapshot error = %v, want ErrInvalidRevisionPreparation", err)
	}
	prepared, err := NewPreparedImportedSnapshot("rec_imported01", "evs_imported01", snapshot)
	if err != nil {
		t.Fatalf("NewPreparedImportedSnapshot() error = %v", err)
	}
	if prepared.RecordID() != "rec_imported01" || prepared.SnapshotID() != "evs_imported01" ||
		prepared.Snapshot().Hash() != snapshot.Hash() {
		t.Fatalf("PreparedImportedSnapshot() = %#v", prepared)
	}
}

func TestRevisionPreparationIncludesImportedSnapshotsInOrderedIdentities(t *testing.T) {
	t.Parallel()

	kind, err := NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustComparisonResultSnapshot(t, kind, comparisonResultTestPayload("evs_leftcopy", "evs_rightcopy"))
	imported, err := NewPreparedImportedSnapshot("rec_imported02", "evs_imported02", snapshot)
	if err != nil {
		t.Fatalf("NewPreparedImportedSnapshot() error = %v", err)
	}
	if _, err := NewRevisionPreparation("rec_imported02", RevisionPreparationValues{
		Imported: []PreparedImportedSnapshot{imported},
	}); !errors.Is(err, ErrInvalidRevisionPreparation) {
		t.Fatalf("missing ordered identity error = %v, want ErrInvalidRevisionPreparation", err)
	}
	prepared, err := NewRevisionPreparation("rec_imported02", RevisionPreparationValues{
		Imported:           []PreparedImportedSnapshot{imported},
		OrderedSnapshotIDs: []string{imported.SnapshotID()},
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	if prepared.Empty() || !reflect.DeepEqual(prepared.SnapshotIDs(), []string{imported.SnapshotID()}) {
		t.Fatalf("SnapshotIDs() = %#v", prepared.SnapshotIDs())
	}
	if got := prepared.Imported(); len(got) != 1 || got[0].SnapshotID() != imported.SnapshotID() {
		t.Fatalf("Imported() = %#v", got)
	}
}

package migrate

import (
	"reflect"
	"testing"
)

func TestNamesUsesOnlyTheDeletionLedgerMigrationSet(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	if want := []string{"0001_create_deletion_ledger.sql"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Names() = %#v, want %#v", names, want)
	}
}

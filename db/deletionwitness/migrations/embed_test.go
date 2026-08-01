package migrations

import "testing"

func TestFSIncludesFullWitnessMigration(t *testing.T) {
	if _, err := FS.ReadFile("0001_create_full_witness.sql"); err != nil {
		t.Fatalf("read embedded full-witness migration: %v", err)
	}
}

package migrate

import "testing"

func TestNamesIncludesInitialSchema(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}

	if len(names) == 0 {
		t.Fatal("Names() returned no migrations")
	}

	if names[0] != "0001_initial_schema.sql" {
		t.Fatalf("first migration = %q, want %q", names[0], "0001_initial_schema.sql")
	}
}

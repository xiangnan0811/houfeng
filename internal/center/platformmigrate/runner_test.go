package platformmigrate

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestApplyFSRecordsChecksumsAndRejectsLedgerDrift(t *testing.T) {
	ctx := context.Background()
	store := &fakeMigrationStore{}
	initial := fstest.MapFS{
		"0001_create_domain.sql": {Data: []byte("create table example (id bigint primary key);\n")},
	}

	if err := ApplyFS(ctx, store, initial); err != nil {
		t.Fatalf("fresh ApplyFS() error = %v", err)
	}
	if got := store.executed; got != 1 {
		t.Fatalf("fresh executions = %d, want 1", got)
	}
	if len(store.applied) != 1 {
		t.Fatalf("applied ledger = %#v, want one checksum", store.applied)
	}

	if err := ApplyFS(ctx, store, initial); err != nil {
		t.Fatalf("repeat ApplyFS() error = %v", err)
	}
	if got := store.executed; got != 1 {
		t.Fatalf("repeat executions = %d, want 1", got)
	}

	tampered := fstest.MapFS{
		"0001_create_domain.sql": {Data: []byte("create table example (id text primary key);\n")},
	}
	err := ApplyFS(ctx, store, tampered)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered ApplyFS() error = %v, want checksum mismatch", err)
	}
	if got := store.executed; got != 1 {
		t.Fatalf("tampered executions = %d, want 1", got)
	}
}

func TestApplyFSRejectsUnknownAppliedMigration(t *testing.T) {
	ctx := context.Background()
	store := &fakeMigrationStore{applied: map[string]string{"0000_unknown.sql": strings.Repeat("0", 64)}}
	fsys := fstest.MapFS{
		"0001_create_domain.sql": {Data: []byte("create table example (id bigint primary key);\n")},
	}

	err := ApplyFS(ctx, store, fsys)
	if err == nil || !strings.Contains(err.Error(), "unknown applied migration") {
		t.Fatalf("ApplyFS() error = %v, want unknown applied migration", err)
	}
}

type fakeMigrationStore struct {
	applied  map[string]string
	executed int
}

func (s *fakeMigrationStore) EnsureLedger(context.Context) error {
	if s.applied == nil {
		s.applied = make(map[string]string)
	}
	return nil
}

func (s *fakeMigrationStore) Applied(context.Context) (map[string]string, error) {
	copy := make(map[string]string, len(s.applied))
	for name, checksum := range s.applied {
		copy[name] = checksum
	}
	return copy, nil
}

func (s *fakeMigrationStore) Apply(_ context.Context, name, checksum, _ string) error {
	s.executed++
	s.applied[name] = checksum
	return nil
}

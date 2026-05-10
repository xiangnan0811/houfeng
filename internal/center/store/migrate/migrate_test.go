package migrate

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestNamesIncludesBaselineAndFollowupMigrations(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}

	if len(names) < 10 {
		t.Fatalf("len(Names()) = %d, want at least 10", len(names))
	}

	if names[0] != "0001_initial_schema.sql" {
		t.Fatalf("first migration = %q, want %q", names[0], "0001_initial_schema.sql")
	}
	if names[1] != "0002_normalize_status_defaults.sql" {
		t.Fatalf("second migration = %q, want %q", names[1], "0002_normalize_status_defaults.sql")
	}
	if names[2] != "0003_add_sync_token_hash.sql" {
		t.Fatalf("third migration = %q, want %q", names[2], "0003_add_sync_token_hash.sql")
	}
	if names[3] != "0004_add_node_onboarding_binding_state.sql" {
		t.Fatalf("fourth migration = %q, want %q", names[3], "0004_add_node_onboarding_binding_state.sql")
	}
	if names[4] != "0004_add_observation_provenance.sql" {
		t.Fatalf("fifth migration = %q, want %q", names[4], "0004_add_observation_provenance.sql")
	}
	if names[5] != "0005_add_node_binding_epoch.sql" {
		t.Fatalf("sixth migration = %q, want %q", names[5], "0005_add_node_binding_epoch.sql")
	}
	if names[6] != "0006_add_center_settings.sql" {
		t.Fatalf("seventh migration = %q, want %q", names[6], "0006_add_center_settings.sql")
	}
	if names[7] != "0007_add_telegram_runtime_managed.sql" {
		t.Fatalf("eighth migration = %q, want %q", names[7], "0007_add_telegram_runtime_managed.sql")
	}
	if names[8] != "0008_add_retention_aggregates.sql" {
		t.Fatalf("ninth migration = %q, want %q", names[8], "0008_add_retention_aggregates.sql")
	}
	if names[9] != "0009_add_observability_filter_indexes.sql" {
		t.Fatalf("tenth migration = %q, want %q", names[9], "0009_add_observability_filter_indexes.sql")
	}

	expectedTail := []string{
		"0017_add_vps_assets.sql",
		"0018_add_subscriptions.sql",
		"0019_create_vps_node_links.sql",
		"0020_create_renewal_decisions.sql",
		"0021_create_asset_histories.sql",
		"0022_create_experience_logs.sql",
		"0023_create_asset_services.sql",
	}
	offset := 0
	for _, name := range names {
		if offset < len(expectedTail) && name == expectedTail[offset] {
			offset++
		}
	}
	if offset != len(expectedTail) {
		t.Fatalf("migration names missing expected asset ledger tail order %#v in %#v", expectedTail, names)
	}
}

func TestApplyFSExecutesPendingMigrationsInSortedOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql":         {Data: []byte("select 2;")},
		"0001_initial_schema.sql": {Data: []byte("select 1;")},
		"README.txt":              {Data: []byte("ignore me")},
	}
	store := &fakeMigrationStore{}

	if err := applyFS(context.Background(), store, fsys); err != nil {
		t.Fatalf("applyFS() error = %v", err)
	}

	if store.ensureCalls != 1 {
		t.Fatalf("ensureCalls = %d, want 1", store.ensureCalls)
	}

	wantChecks := []string{"0001_initial_schema.sql", "0002_second.sql"}
	if !reflect.DeepEqual(store.appliedChecks, wantChecks) {
		t.Fatalf("appliedChecks = %#v, want %#v", store.appliedChecks, wantChecks)
	}

	wantExec := []string{"select 1;", "select 2;"}
	if !reflect.DeepEqual(store.execSQL, wantExec) {
		t.Fatalf("execSQL = %#v, want %#v", store.execSQL, wantExec)
	}

	wantRecorded := []string{"0001_initial_schema.sql", "0002_second.sql"}
	if !reflect.DeepEqual(store.recorded, wantRecorded) {
		t.Fatalf("recorded = %#v, want %#v", store.recorded, wantRecorded)
	}
}

func TestApplyFSSkipsAlreadyAppliedMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql":         {Data: []byte("select 2;")},
		"0001_initial_schema.sql": {Data: []byte("select 1;")},
	}
	store := &fakeMigrationStore{
		applied: map[string]bool{"0001_initial_schema.sql": true},
	}

	if err := applyFS(context.Background(), store, fsys); err != nil {
		t.Fatalf("applyFS() error = %v", err)
	}

	wantExec := []string{"select 2;"}
	if !reflect.DeepEqual(store.execSQL, wantExec) {
		t.Fatalf("execSQL = %#v, want %#v", store.execSQL, wantExec)
	}

	wantRecorded := []string{"0002_second.sql"}
	if !reflect.DeepEqual(store.recorded, wantRecorded) {
		t.Fatalf("recorded = %#v, want %#v", store.recorded, wantRecorded)
	}
}

func TestApplyFSIncludesMigrationNameOnExecFailure(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql": {Data: []byte("select 2;")},
	}
	store := &fakeMigrationStore{
		execErr: map[string]error{"select 2;": errors.New("boom")},
	}

	err := applyFS(context.Background(), store, fsys)
	if err == nil {
		t.Fatal("applyFS() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "0002_second.sql") {
		t.Fatalf("error %q does not include migration name", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %q does not include underlying exec error", err)
	}
}

type fakeMigrationStore struct {
	ensureCalls   int
	applied       map[string]bool
	appliedChecks []string
	execSQL       []string
	recorded      []string
	execErr       map[string]error
}

func (f *fakeMigrationStore) EnsureLedger(context.Context) error {
	f.ensureCalls++
	return nil
}

func (f *fakeMigrationStore) HasMigration(_ context.Context, name string) (bool, error) {
	f.appliedChecks = append(f.appliedChecks, name)
	return f.applied[name], nil
}

func (f *fakeMigrationStore) ExecMigration(_ context.Context, sql string) error {
	f.execSQL = append(f.execSQL, sql)
	if err := f.execErr[sql]; err != nil {
		return err
	}
	return nil
}

func (f *fakeMigrationStore) RecordMigration(_ context.Context, name string) error {
	f.recorded = append(f.recorded, name)
	return nil
}

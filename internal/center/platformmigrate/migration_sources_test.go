package platformmigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndependentMigrationSourcesOwnTheirLocalDomainIdentity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		domainKind string
		tables     []string
	}{
		{
			name:       "deletion ledger",
			path:       filepath.Join("..", "..", "..", "db", "deletionledger", "migrations", "0001_create_deletion_ledger.sql"),
			domainKind: "deletion_ledger",
			tables:     []string{"deletion_ledger_entries", "deletion_ledger_head"},
		},
		{
			name:       "deletion witness",
			path:       filepath.Join("..", "..", "..", "db", "deletionwitness", "migrations", "0001_create_full_witness.sql"),
			domainKind: "deletion_witness",
			tables:     []string{"deletion_witness_entries", "deletion_witness_head"},
		},
		{
			name:       "recovery control",
			path:       filepath.Join("..", "..", "..", "db", "recoverycontrol", "migrations", "0001_create_recovery_control.sql"),
			domainKind: "recovery_control",
			tables: []string{
				"recovery_trust_entries",
				"recovery_trust_head",
				"recovery_point_manifests",
				"recovery_inventory",
				"backup_attempts",
				"backup_workspaces",
				"restore_attempts",
				"restore_workspaces",
				"recovery_purge_receipts",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read migration source %q: %v", tc.path, err)
			}

			sql := strings.ToLower(string(payload))
			for _, want := range append([]string{
				"create schema if not exists record_platform_internal",
				"create extension if not exists pgcrypto with schema record_platform_internal",
				"from pg_extension",
				"extnamespace",
				"revoke all on schema record_platform_internal from public",
				"create table public.record_platform_domain_identity",
				"create table public.record_platform_domain_attestations",
				"domain_kind text not null check (domain_kind = '" + tc.domainKind + "')",
				"before update or delete or truncate on public.record_platform_domain_identity",
				"before update or delete or truncate on public.record_platform_domain_attestations",
			}, tc.tables...) {
				if !strings.Contains(sql, want) {
					t.Fatalf("migration %q missing local-domain contract %q", tc.path, want)
				}
			}

			for _, forbidden := range []string{"raw_token", "private_key", "local_path", "command_line"} {
				if strings.Contains(sql, forbidden) {
					t.Fatalf("migration %q must not persist %q", tc.path, forbidden)
				}
			}
			if tc.name == "recovery control" && !strings.Contains(sql,
				"workspace_id text not null check (workspace_id ~ '^(rbw|rrw)_[a-z0-9]{1,64}$')") {
				t.Fatalf("migration %q must validate backup and restore workspace IDs exactly", tc.path)
			}
		})
	}
}

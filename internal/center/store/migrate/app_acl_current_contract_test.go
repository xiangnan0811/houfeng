package migrate

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"houfeng/db/migrations"
)

func TestCompileAppACLCurrentSourceContractRejectsMissingFutureFragment(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}

	_, err := compileAppACLCurrentSourceContract(fsys, nil)
	if err == nil || !strings.Contains(err.Error(), `migration "0052_future.sql" has no current APP ACL fragment`) {
		t.Fatalf("compileAppACLCurrentSourceContract() error = %v, want missing-fragment rejection", err)
	}
}

func TestCompileAppACLCurrentSourceContractAcceptsRegisteredFutureMigration(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}

	contract, err := compileAppACLCurrentSourceContract(fsys, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Privileges: func(string) []AppACLPrivilege {
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("compileAppACLCurrentSourceContract() error = %v", err)
	}
	if got := contract.sources.names[len(contract.sources.names)-1]; got != "0052_future.sql" {
		t.Fatalf("last current migration = %q, want 0052_future.sql", got)
	}
}

func TestCompileAppACLCurrentSourceContractRejectsInvalidFragments(t *testing.T) {
	const futureMigration = "0052_future.sql"
	newTable := AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     "public",
		ObjectIdentity: "future_records",
	}
	newFunction := AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     "public",
		ObjectIdentity: "future_function()",
	}
	newTablePrivilege := AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     "public",
		ObjectIdentity: "future_records",
		Privilege:      AppACLPrivilegeSelect,
	}
	emptyPrivileges := func(string) []AppACLPrivilege { return nil }

	for _, tc := range []struct {
		name      string
		fragments []AppACLCurrentMigrationFragment
		want      string
	}{
		{
			name: "duplicate_fragments",
			fragments: []AppACLCurrentMigrationFragment{
				{Migration: futureMigration, Privileges: emptyPrivileges},
				{Migration: futureMigration, Privileges: emptyPrivileges},
			},
			want: "duplicate current APP ACL fragment",
		},
		{
			name: "fragment_for_absent_migration",
			fragments: []AppACLCurrentMigrationFragment{
				{Migration: futureMigration, Privileges: emptyPrivileges},
				{Migration: "0053_absent.sql", Privileges: emptyPrivileges},
			},
			want: "is not present in the current migration sources",
		},
		{
			name: "duplicate_objects",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration:  futureMigration,
				Objects:    []AppACLManagedObjectR1{newTable, newTable},
				Privileges: emptyPrivileges,
			}},
			want: "duplicate managed object",
		},
		{
			name: "duplicate_privileges",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration: futureMigration,
				Objects:   []AppACLManagedObjectR1{newTable},
				Privileges: func(string) []AppACLPrivilege {
					return []AppACLPrivilege{newTablePrivilege, newTablePrivilege}
				},
			}},
			want: "duplicate canonical privilege tuple",
		},
		{
			name: "unknown_subject",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration: futureMigration,
				Objects:   []AppACLManagedObjectR1{newTable},
				Privileges: func(string) []AppACLPrivilege {
					privilege := newTablePrivilege
					privilege.Subject = AppACLSubject("unknown")
					return []AppACLPrivilege{privilege}
				},
			}},
			want: "unknown ACL subject",
		},
		{
			name: "privilege_for_unmanaged_object",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration: futureMigration,
				Privileges: func(string) []AppACLPrivilege {
					return []AppACLPrivilege{newTablePrivilege}
				},
			}},
			want: "privilege references unmanaged object",
		},
		{
			name: "hardening_for_unmanaged_function",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration:  futureMigration,
				Privileges: emptyPrivileges,
				Functions: []AppACLCurrentFunctionContract{{
					SchemaName: "public",
					Identity:   "future_function()",
					Kind:       "f",
				}},
			}},
			want: "function hardening references unmanaged function",
		},
		{
			name: "managed_function_without_hardening",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration:  futureMigration,
				Objects:    []AppACLManagedObjectR1{newFunction},
				Privileges: emptyPrivileges,
			}},
			want: "managed function has no hardening contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := appACLCurrentTestMigrationFS(t)
			fsys[futureMigration] = &fstest.MapFile{Data: []byte("select 'future';")}

			_, err := compileAppACLCurrentSourceContract(fsys, tc.fragments)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("compileAppACLCurrentSourceContract() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func appACLCurrentTestMigrationFS(t *testing.T) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	fsys := make(fstest.MapFS, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", entry.Name(), err)
		}
		fsys[entry.Name()] = &fstest.MapFile{Data: append([]byte(nil), data...)}
	}
	return fsys
}

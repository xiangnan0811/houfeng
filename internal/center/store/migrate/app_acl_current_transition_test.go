package migrate

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"

	"houfeng/db/migrations"
)

func TestAppACLCurrentTransitionCompilerAcceptsExactReleasedProfiles(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatalf("compileAppACLCurrentTransitions() error = %v", err)
	}
	if len(transitions) != 2 {
		t.Fatalf("compiled transition count = %d, want the P62 and P64 profiles", len(transitions))
	}

	p62, p64 := transitions[0], transitions[1]
	if p62.profile != appACLCurrentProfileP62 || p64.profile != appACLCurrentProfileP64 {
		t.Fatalf("compiled transition profiles = %d/%d, want P62/P64", p62.profile, p64.profile)
	}
	if got, want := len(p62.predecessor.sources.names), 63; got != want {
		t.Fatalf("P62 migration count = %d, want %d", got, want)
	}
	if got, want := p62.predecessor.sources.names[62], "0062_create_vps_create_idempotency.sql"; got != want {
		t.Fatalf("P62 final migration = %q, want %q", got, want)
	}
	if got, want := p62.successor.names, []string{
		"0063_tune_heartbeat_incident_policy.sql",
		"0064_add_network_rates_valid.sql",
		"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
		"0066_constrain_monitoring_and_target_state_values.sql",
	}; !equalStringSlices(got, want) {
		t.Fatalf("P62 successor migrations = %#v, want %#v", got, want)
	}
	if got, want := len(p64.predecessor.sources.names), 65; got != want {
		t.Fatalf("P64 migration count = %d, want %d", got, want)
	}
	if got, want := p64.predecessor.sources.names[64], "0064_add_network_rates_valid.sql"; got != want {
		t.Fatalf("P64 final migration = %q, want %q", got, want)
	}
	if got, want := p64.successor.names, []string{
		"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
		"0066_constrain_monitoring_and_target_state_values.sql",
	}; !equalStringSlices(got, want) {
		t.Fatalf("P64 successor migrations = %#v, want %#v", got, want)
	}
	if !bytes.Equal(p62.predecessor.sources.canonicalSet, appACLCurrentV0794MigrationGolden) ||
		!bytes.Equal(p62.predecessorPrivilegeBody, appACLCurrentV0794PrivilegeGolden) ||
		p62.predecessorManifestDigest != appACLCurrentV0794ManifestDigestGolden {
		t.Fatal("compiled P62 profile differs from its immutable released goldens")
	}
	if !bytes.Equal(p64.predecessor.sources.canonicalSet, appACLCurrentV0802MigrationGolden) ||
		!bytes.Equal(p64.predecessorPrivilegeBody, appACLCurrentV0802PrivilegeGolden) {
		t.Fatal("compiled P64 profile differs from the independent v0.80.2 source/privilege goldens")
	}
	if !bytes.Equal(p62.predecessorPrivilegeBody, p64.predecessorPrivilegeBody) {
		t.Fatal("P62 and P64 released profiles unexpectedly differ in privileges")
	}
	privileges, err := ParseCanonicalPrivilegeSetBodyV1(p62.predecessorPrivilegeBody)
	if err != nil {
		t.Fatalf("parse compiled predecessor privileges: %v", err)
	}
	if got, want := privileges.RoleBindings, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("released profile role bindings = %#v, want exact fixed bindings %#v", got, want)
	}

	p62.successor.names[0] = "mutated.sql"
	p64.predecessor.sources.canonicalSet[0] ^= 0xff
	p64.predecessorPrivilegeBody[0] ^= 0xff
	recompiled, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	if recompiled[0].successor.names[0] != "0063_tune_heartbeat_incident_policy.sql" ||
		!bytes.Equal(recompiled[0].predecessor.sources.canonicalSet, appACLCurrentV0794MigrationGolden) ||
		!bytes.Equal(recompiled[1].predecessor.sources.canonicalSet, appACLCurrentV0802MigrationGolden) ||
		!bytes.Equal(recompiled[1].predecessorPrivilegeBody, appACLCurrentV0802PrivilegeGolden) {
		t.Fatal("compiled profile leaked mutable backing storage")
	}
}

func TestAppACLCurrentTransitionCompilerRejectsInvalidDefinitions(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	valid := cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
	for _, tc := range []struct {
		name        string
		definitions []appACLCurrentTransitionDefinition
		want        string
	}{
		{
			name:        "missing profile",
			definitions: valid[:1],
			want:        "exactly",
		},
		{
			name: "duplicate profile",
			definitions: func() []appACLCurrentTransitionDefinition {
				value := cloneAppACLCurrentTransitionDefinitions(valid)
				value[1].profile = appACLCurrentProfileP62
				return value
			}(),
			want: "duplicates",
		},
		{
			name: "wrong predecessor",
			definitions: func() []appACLCurrentTransitionDefinition {
				value := cloneAppACLCurrentTransitionDefinitions(valid)
				value[1].predecessorLastMigration = "0063_tune_heartbeat_incident_policy.sql"
				return value
			}(),
			want: "registered profile",
		},
		{
			name: "incomplete successor suffix",
			definitions: func() []appACLCurrentTransitionDefinition {
				value := cloneAppACLCurrentTransitionDefinitions(valid)
				value[1].successorMigrations = []string{"0066_constrain_monitoring_and_target_state_values.sql"}
				return value
			}(),
			want: "suffix",
		},
		{
			name: "unknown profile",
			definitions: func() []appACLCurrentTransitionDefinition {
				value := cloneAppACLCurrentTransitionDefinitions(valid)
				value[1].profile = 99
				return value
			}(),
			want: "unknown predecessor profile",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := compileAppACLCurrentTransitions(current, tc.definitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("compileAppACLCurrentTransitions() error = %v, want %q rejection", err, tc.want)
			}
			assertAppACLCurrentTransitionRejectedBeforeBeginTx(
				t,
				migrations.FS,
				appACLCurrentMigrationFragments,
				tc.definitions,
				tc.want,
			)
		})
	}
}

func TestAppACLCurrentTransitionCompilerRejectsUnapprovedPrivilegeDelta(t *testing.T) {
	t.Run("additional grant", func(t *testing.T) {
		fragments := cloneAppACLCurrentMigrationFragmentsForTransitionTest(appACLCurrentMigrationFragments)
		originalPrivileges := fragments[len(fragments)-2].Privileges
		fragments[len(fragments)-2].Privileges = func(databaseName string) []AppACLPrivilege {
			privileges := originalPrivileges(databaseName)
			return append(privileges, AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: "asset_services",
				Privilege:      AppACLPrivilegeDelete,
			})
		}
		current, err := compileAppACLCurrentSourceContract(migrations.FS, fragments)
		if err != nil {
			t.Fatalf("compile current source with additional grant: %v", err)
		}
		if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), "exactly") {
			t.Fatalf("compileAppACLCurrentTransitions() error = %v, want exact-delta rejection", err)
		}
	})

	t.Run("removed predecessor grant", func(t *testing.T) {
		current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
		if err != nil {
			t.Fatal(err)
		}
		transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
		if err != nil {
			t.Fatal(err)
		}
		modified := current
		modified.fragments = make([]appACLCurrentCompiledMigrationFragment, len(current.fragments))
		for index, fragment := range current.fragments {
			modified.fragments[index] = cloneAppACLCurrentCompiledMigrationFragment(fragment)
		}
		removed := false
		for index := range modified.fragments {
			if len(modified.fragments[index].Privileges) > 0 {
				modified.fragments[index].Privileges = nil
				removed = true
				break
			}
		}
		if !removed {
			t.Fatal("current source has no migration-fragment privileges to remove")
		}
		if err := validateAppACLCurrentTransitionPrivilegeDelta(transitions[1].predecessorPrivilegeBody, modified); err == nil || !strings.Contains(strings.ToLower(err.Error()), "removes") {
			t.Fatalf("validateAppACLCurrentTransitionPrivilegeDelta() error = %v, want privilege-removal rejection", err)
		}
	})
}

func TestAppACLCurrentTransitionCompilerRejectsReleasedGoldenDrift(t *testing.T) {
	t.Run("migration source", func(t *testing.T) {
		fsys := appACLCurrentTransitionTestFS(t)
		file := fsys["0062_create_vps_create_idempotency.sql"]
		file.Data = append(append([]byte(nil), file.Data...), []byte("\n-- mutated predecessor\n")...)
		current, err := compileAppACLCurrentSourceContract(fsys, appACLCurrentMigrationFragments)
		if err != nil {
			t.Fatalf("compile mutated current source: %v", err)
		}
		if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), "golden") {
			t.Fatalf("compileAppACLCurrentTransitions() error = %v, want migration golden rejection", err)
		}
		assertAppACLCurrentTransitionRejectedBeforeBeginTx(
			t,
			fsys,
			appACLCurrentMigrationFragments,
			appACLCurrentTransitionDefinitions,
			"golden",
		)
	})

	t.Run("canonical body", func(t *testing.T) {
		current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
		if err != nil {
			t.Fatal(err)
		}
		definitions := cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
		definitions[0].predecessorMigrationGolden[0] ^= 0xff
		if _, err := compileAppACLCurrentTransitions(current, definitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), "golden") {
			t.Fatalf("compileAppACLCurrentTransitions() error = %v, want canonical golden rejection", err)
		}
		assertAppACLCurrentTransitionRejectedBeforeBeginTx(
			t,
			migrations.FS,
			appACLCurrentMigrationFragments,
			definitions,
			"golden",
		)
	})
}

func TestAppACLCurrentTransitionRegistryRejectsMissingDefinitionsBeforeBeginTx(t *testing.T) {
	assertAppACLCurrentTransitionRejectedBeforeBeginTx(
		t,
		migrations.FS,
		appACLCurrentMigrationFragments,
		[]appACLCurrentTransitionDefinition{},
		"transition registry has no definitions",
	)
}

func assertAppACLCurrentTransitionRejectedBeforeBeginTx(
	t *testing.T,
	migrationFS fs.FS,
	fragments []AppACLCurrentMigrationFragment,
	definitions []appACLCurrentTransitionDefinition,
	want string,
) {
	t.Helper()
	t.Run("convergence before BeginTx", func(t *testing.T) {
		dependencies := appACLCurrentConvergenceTestDependencies()
		dependencies.transitionDefinitions = cloneAppACLCurrentTransitionDefinitions(definitions)
		beginCalls := 0
		_, err := convergeAppACLCurrentWithDependencies(
			context.Background(),
			func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				beginCalls++
				return nil, errors.New("begin must not run")
			},
			"houfeng_center_runtime",
			"houfeng_platform_admin",
			migrationFS,
			fragments,
			dependencies,
		)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Fatalf("convergeAppACLCurrentWithDependencies() error = %v, want %q rejection", err, want)
		}
		if beginCalls != 0 {
			t.Fatalf("convergence BeginTx calls = %d, want 0", beginCalls)
		}
	})

	t.Run("runtime admission before BeginTx", func(t *testing.T) {
		beginCalls := 0
		err := admitAppACLCurrentRuntimeWithDependencies(
			context.Background(),
			migrationFS,
			fragments,
			appACLCurrentRuntimeAdmissionDependencies{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					return nil, errors.New("begin must not run")
				},
				readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
					return AppACLManifestRuntimeSnapshotV1{}, nil
				},
				readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
					return AppACLEffectiveCatalogSnapshotR1{}, nil
				},
				verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error {
					return nil
				},
				transitionDefinitions: cloneAppACLCurrentTransitionDefinitions(definitions),
			},
		)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Fatalf("admitAppACLCurrentRuntimeWithDependencies() error = %v, want %q rejection", err, want)
		}
		if beginCalls != 0 {
			t.Fatalf("runtime admission BeginTx calls = %d, want 0", beginCalls)
		}
	})
}

func cloneAppACLCurrentMigrationFragmentsForTransitionTest(source []AppACLCurrentMigrationFragment) []AppACLCurrentMigrationFragment {
	cloned := make([]AppACLCurrentMigrationFragment, len(source))
	for index, fragment := range source {
		cloned[index] = cloneAppACLCurrentMigrationFragment(fragment)
	}
	return cloned
}

func appACLCurrentTransitionTestFS(t *testing.T) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	result := make(fstest.MapFS, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = &fstest.MapFile{Data: append([]byte(nil), body...)}
	}
	return result
}

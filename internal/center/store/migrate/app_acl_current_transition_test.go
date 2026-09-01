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

func TestAppACLCurrentTransitionCompilerAcceptsExactV0794Predecessor(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatalf("compileAppACLCurrentTransitions() error = %v", err)
	}
	if len(transitions) != 1 {
		t.Fatalf("compiled transition count = %d, want 1", len(transitions))
	}
	transition := transitions[0]
	if got, want := len(transition.predecessor.sources.names), 63; got != want {
		t.Fatalf("predecessor migration count = %d, want %d", got, want)
	}
	if got, want := transition.predecessor.sources.names[62], "0062_create_vps_create_idempotency.sql"; got != want {
		t.Fatalf("predecessor final migration = %q, want %q", got, want)
	}
	if got, want := transition.successor.names, []string{"0063_tune_heartbeat_incident_policy.sql"}; !equalStringSlices(got, want) {
		t.Fatalf("successor migrations = %#v, want %#v", got, want)
	}
	if !bytes.Equal(transition.predecessor.sources.canonicalSet, appACLCurrentV0794MigrationGolden) {
		t.Fatal("compiled predecessor canonical migration body differs from v0.79.4 golden")
	}
	if !bytes.Equal(transition.predecessorPrivilegeBody, appACLCurrentV0794PrivilegeGolden) {
		t.Fatal("compiled predecessor privilege body differs from v0.79.4 golden")
	}
	if transition.predecessorManifestDigest != appACLCurrentV0794ManifestDigestGolden {
		t.Fatalf("compiled predecessor manifest digest = %x, want %x", transition.predecessorManifestDigest, appACLCurrentV0794ManifestDigestGolden)
	}
	privileges, err := ParseCanonicalPrivilegeSetBodyV1(transition.predecessorPrivilegeBody)
	if err != nil {
		t.Fatalf("parse compiled predecessor privileges: %v", err)
	}
	if got, want := privileges.RoleBindings, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled predecessor role bindings = %#v, want exact Compose bindings %#v", got, want)
	}

	transition.successor.names[0] = "mutated.sql"
	transition.predecessor.sources.canonicalSet[0] ^= 0xff
	transition.predecessorPrivilegeBody[0] ^= 0xff
	recompiled, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	if recompiled[0].successor.names[0] != "0063_tune_heartbeat_incident_policy.sql" ||
		!bytes.Equal(recompiled[0].predecessor.sources.canonicalSet, appACLCurrentV0794MigrationGolden) ||
		!bytes.Equal(recompiled[0].predecessorPrivilegeBody, appACLCurrentV0794PrivilegeGolden) {
		t.Fatal("compiled transition leaked mutable backing storage")
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
		{name: "missing suffix", definitions: func() []appACLCurrentTransitionDefinition {
			value := cloneAppACLCurrentTransitionDefinitions(valid)
			value[0].successorMigrations = nil
			return value
		}(), want: "successor"},
		{name: "duplicate suffix", definitions: func() []appACLCurrentTransitionDefinition {
			value := cloneAppACLCurrentTransitionDefinitions(valid)
			value[0].successorMigrations = []string{"0063_tune_heartbeat_incident_policy.sql", "0063_tune_heartbeat_incident_policy.sql"}
			return value
		}(), want: "duplicate"},
		{name: "unknown suffix", definitions: func() []appACLCurrentTransitionDefinition {
			value := cloneAppACLCurrentTransitionDefinitions(valid)
			value[0].successorMigrations = []string{"0064_unknown.sql"}
			return value
		}(), want: "unknown"},
		{name: "out of order suffix", definitions: func() []appACLCurrentTransitionDefinition {
			value := cloneAppACLCurrentTransitionDefinitions(valid)
			value[0].successorMigrations = []string{"0063_tune_heartbeat_incident_policy.sql", "0062_create_vps_create_idempotency.sql"}
			return value
		}(), want: "order"},
		{name: "unknown predecessor", definitions: func() []appACLCurrentTransitionDefinition {
			value := cloneAppACLCurrentTransitionDefinitions(valid)
			value[0].predecessorLastMigration = "0061_unknown.sql"
			return value
		}(), want: "predecessor"},
		{name: "overlap", definitions: append(cloneAppACLCurrentTransitionDefinitions(valid), cloneAppACLCurrentTransitionDefinitions(valid)[0]), want: "overlap"},
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

func TestAppACLCurrentTransitionCompilerRejectsPrivilegeChangeMarkedUnchanged(t *testing.T) {
	fragments := cloneAppACLCurrentMigrationFragmentsForTransitionTest(appACLCurrentMigrationFragments)
	fragments[len(fragments)-1].Privileges = func(string) []AppACLPrivilege {
		return []AppACLPrivilege{{
			Subject:        AppACLSubjectPlatformAdmin,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "experience_log_create_idempotency",
			Privilege:      AppACLPrivilegeSelect,
		}}
	}
	current, err := compileAppACLCurrentSourceContract(migrations.FS, fragments)
	if err != nil {
		t.Fatalf("compile changed current source: %v", err)
	}
	if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), "privilege") {
		t.Fatalf("compileAppACLCurrentTransitions() error = %v, want unchanged-privilege rejection", err)
	}
	assertAppACLCurrentTransitionRejectedBeforeBeginTx(
		t,
		migrations.FS,
		fragments,
		appACLCurrentTransitionDefinitions,
		"privilege",
	)
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

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
	if len(transitions) != 9 {
		t.Fatalf("compiled transition count = %d, want the P62, P64, P63, P66, P67, P68, P69, P70 and P71 profiles", len(transitions))
	}
	p63 := transitions[2]
	if p63.profile != appACLCurrentProfileP63 || len(p63.predecessor.sources.names) != 64 ||
		!bytes.Equal(p63.predecessor.sources.canonicalSet, appACLCurrentV0796MigrationGolden) ||
		!bytes.Equal(p63.predecessorPrivilegeBody, appACLCurrentV0796PrivilegeGolden) ||
		!equalStringSlices(p63.successor.names, []string{"0064_add_network_rates_valid.sql", "0065_extend_vps_lifecycle_audit_and_snapshot.sql", "0066_constrain_monitoring_and_target_state_values.sql", "0067_refactor_vps_monitoring_lifecycle.sql", "0068_normalize_ip_quality_host_address_identity.sql", "0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P63 profile differs from independent v0.79.6 release goldens or expected suffix")
	}
	p66 := transitions[3]
	if p66.profile != appACLCurrentProfileP66 || len(p66.predecessor.sources.names) != 67 ||
		!bytes.Equal(p66.predecessor.sources.canonicalSet, appACLCurrentV0804MigrationGolden) ||
		!bytes.Equal(p66.predecessorPrivilegeBody, appACLCurrentV0804PrivilegeGolden) ||
		!equalStringSlices(p66.successor.names, []string{"0067_refactor_vps_monitoring_lifecycle.sql", "0068_normalize_ip_quality_host_address_identity.sql", "0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P66 profile differs from independent v0.80.4 release goldens or expected suffix")
	}
	p67 := transitions[4]
	if p67.profile != appACLCurrentProfileP67 || len(p67.predecessor.sources.names) != 68 ||
		!bytes.Equal(p67.predecessor.sources.canonicalSet, appACLCurrentV1153MigrationGolden) ||
		!bytes.Equal(p67.predecessorPrivilegeBody, appACLCurrentV1153PrivilegeGolden) ||
		!equalStringSlices(p67.successor.names, []string{"0068_normalize_ip_quality_host_address_identity.sql", "0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P67 profile differs from independent v1.15.3 release goldens or expected suffix")
	}
	p68 := transitions[5]
	if p68.profile != appACLCurrentProfileP68 || len(p68.predecessor.sources.names) != 69 ||
		!bytes.Equal(p68.predecessor.sources.canonicalSet, appACLCurrentV1157MigrationGolden) ||
		!bytes.Equal(p68.predecessorPrivilegeBody, appACLCurrentV1157PrivilegeGolden) ||
		!equalStringSlices(p68.successor.names, []string{"0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P68 profile differs from the independent v1.15.7 C68 goldens or expected suffix")
	}
	p69 := transitions[6]
	if p69.profile != appACLCurrentProfileP69 || len(p69.predecessor.sources.names) != 70 ||
		p69.predecessor.sources.names[69] != "0069_add_cpu_rates_valid.sql" ||
		!equalStringSlices(p69.successor.names, []string{"0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P69 profile does not represent the C69 predecessor and 0070→0071→0072 successor")
	}
	if !bytes.Equal(p69.predecessorPrivilegeBody, p68.predecessorPrivilegeBody) {
		t.Fatal("P69 C69 predecessor privilege body differs from the unchanged P68 privilege body")
	}
	if got := len(appACLCurrentTransitionDefinitions[6].predecessorManifestDigestGolden); got != 0 {
		t.Fatalf("P69 predecessor manifest digest golden length = %d, want no role-bound digest golden", got)
	}
	p70 := transitions[7]
	if p70.profile != appACLCurrentProfileP70 || len(p70.predecessor.sources.names) != 71 ||
		p70.predecessor.sources.names[70] != "0070_add_record_import_destination_subject.sql" ||
		!bytes.Equal(p70.predecessor.sources.canonicalSet, appACLCurrentC70MigrationGolden) ||
		!bytes.Equal(p70.predecessorPrivilegeBody, appACLCurrentC70PrivilegeGolden) ||
		!equalStringSlices(p70.successor.names, []string{"0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P70 profile does not represent the independently frozen C70 predecessor and 0071→0072 successor")
	}
	if got := len(appACLCurrentTransitionDefinitions[7].predecessorManifestDigestGolden); got != 0 {
		t.Fatalf("P70 predecessor manifest digest golden length = %d, want no role-bound digest golden", got)
	}
	p71 := transitions[8]
	if p71.profile != appACLCurrentProfileP71 || len(p71.predecessor.sources.names) != 72 ||
		p71.predecessor.sources.names[71] != "0071_add_access_management.sql" ||
		!bytes.Equal(p71.predecessor.sources.canonicalSet, appACLCurrentC71MigrationGolden) ||
		!bytes.Equal(p71.predecessorPrivilegeBody, appACLCurrentC71PrivilegeGolden) ||
		!equalStringSlices(p71.successor.names, []string{"0072_add_target_observation_freshness.sql"}) {
		t.Fatal("compiled P71 profile does not represent the independently captured C71 predecessor and 0072 successor")
	}
	if got := len(appACLCurrentTransitionDefinitions[8].predecessorManifestDigestGolden); got != 0 {
		t.Fatalf("P71 predecessor manifest digest golden length = %d, want no role-bound digest golden", got)
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
		"0067_refactor_vps_monitoring_lifecycle.sql",
		"0068_normalize_ip_quality_host_address_identity.sql",
		"0069_add_cpu_rates_valid.sql",
		"0070_add_record_import_destination_subject.sql",
		"0071_add_access_management.sql",
		"0072_add_target_observation_freshness.sql",
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
		"0067_refactor_vps_monitoring_lifecycle.sql",
		"0068_normalize_ip_quality_host_address_identity.sql",
		"0069_add_cpu_rates_valid.sql",
		"0070_add_record_import_destination_subject.sql",
		"0071_add_access_management.sql",
		"0072_add_target_observation_freshness.sql",
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
func TestAppACLCurrentTransitionP70PrivilegeDeltaIsExactlyAccessManagement(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	currentBody, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		t.Fatal(err)
	}
	oldSet, err := ParseCanonicalPrivilegeSetBodyV1(transitions[7].predecessorPrivilegeBody)
	if err != nil {
		t.Fatal(err)
	}
	currentSet, err := ParseCanonicalPrivilegeSetBodyV1(currentBody)
	if err != nil {
		t.Fatal(err)
	}
	oldPrivileges := make(map[AppACLPrivilege]struct{}, len(oldSet.Privileges))
	for _, privilege := range oldSet.Privileges {
		oldPrivileges[privilege] = struct{}{}
	}
	additions := make(map[AppACLPrivilege]struct{})
	for _, privilege := range currentSet.Privileges {
		if _, present := oldPrivileges[privilege]; !present {
			additions[privilege] = struct{}{}
		}
	}
	want := make(map[AppACLPrivilege]struct{})
	for _, privilege := range accessManagementAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
		want[privilege] = struct{}{}
	}
	if !reflect.DeepEqual(additions, want) {
		t.Fatalf("P70 privilege additions = %#v, want exact access-management delta %#v", additions, want)
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
				value[len(value)-1].profile = 99
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
	t.Run("additional grant on C71 access management fragment", func(t *testing.T) {
		fragments := cloneAppACLCurrentMigrationFragmentsForTransitionTest(appACLCurrentMigrationFragments)
		fragmentIndex := appACLCurrentTransitionFragmentIndex(t, fragments, "0071_add_access_management.sql")
		originalPrivileges := fragments[fragmentIndex].Privileges
		fragments[fragmentIndex].Privileges = func(databaseName string) []AppACLPrivilege {
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
		if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil {
			t.Fatal("compileAppACLCurrentTransitions() accepted an additional privilege in the frozen C71 predecessor")
		}
	})
	t.Run("additional grant on C72 freshness fragment", func(t *testing.T) {
		fragments := cloneAppACLCurrentMigrationFragmentsForTransitionTest(appACLCurrentMigrationFragments)
		fragmentIndex := appACLCurrentTransitionFragmentIndex(t, fragments, "0072_add_target_observation_freshness.sql")
		originalPrivileges := fragments[fragmentIndex].Privileges
		fragments[fragmentIndex].Privileges = func(databaseName string) []AppACLPrivilege {
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
			t.Fatalf("compile current source with additional 0072 grant: %v", err)
		}
		if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), "exactly") {
			t.Fatalf("compileAppACLCurrentTransitions() error = %v, want exact current-delta rejection", err)
		}
	})

	t.Run("parser admin drift in C68", func(t *testing.T) {
		fragments := cloneAppACLCurrentMigrationFragmentsForTransitionTest(appACLCurrentMigrationFragments)
		fragmentIndex := appACLCurrentTransitionFragmentIndex(t, fragments, "0068_normalize_ip_quality_host_address_identity.sql")
		originalPrivileges := fragments[fragmentIndex].Privileges
		fragments[fragmentIndex].Privileges = func(databaseName string) []AppACLPrivilege {
			privileges := originalPrivileges(databaseName)
			for index := range privileges {
				if privileges[index].ObjectIdentity == "public.houfeng_parse_host_address(text)" {
					privileges[index].Subject = AppACLSubjectPlatformAdmin
				}
			}
			return privileges
		}
		current, err := compileAppACLCurrentSourceContract(migrations.FS, fragments)
		if err != nil {
			t.Fatalf("compile current source with parser admin drift: %v", err)
		}
		if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil || !strings.Contains(strings.ToLower(err.Error()), "predecessor privilege body differs from released golden") {
			t.Fatalf("compileAppACLCurrentTransitions() error = %v, want independent P68 privilege golden rejection", err)
		}
	})

	t.Run("parser bytea overload in C68 is not managed", func(t *testing.T) {
		fragments := cloneAppACLCurrentMigrationFragmentsForTransitionTest(appACLCurrentMigrationFragments)
		fragmentIndex := appACLCurrentTransitionFragmentIndex(t, fragments, "0068_normalize_ip_quality_host_address_identity.sql")
		originalPrivileges := fragments[fragmentIndex].Privileges
		fragments[fragmentIndex].Privileges = func(databaseName string) []AppACLPrivilege {
			privileges := originalPrivileges(databaseName)
			for index := range privileges {
				if privileges[index].ObjectIdentity == "public.houfeng_parse_host_address(text)" {
					privileges[index].ObjectIdentity = "public.houfeng_parse_host_address(bytea)"
				}
			}
			return privileges
		}
		if _, err := compileAppACLCurrentSourceContract(migrations.FS, fragments); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unmanaged") {
			t.Fatalf("compileAppACLCurrentSourceContract() error = %v, want unmanaged parser overload rejection", err)
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
		if err := validateAppACLCurrentTransitionPrivilegeDelta(transitions[1].predecessorPrivilegeBody, modified, appACLCurrentProfileP64); err == nil || !strings.Contains(strings.ToLower(err.Error()), "removes") {
			t.Fatalf("validateAppACLCurrentTransitionPrivilegeDelta() error = %v, want privilege-removal rejection", err)
		}
	})
	t.Run("P68 removed predecessor grant", func(t *testing.T) {
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
			if modified.fragments[index].Migration == "0068_normalize_ip_quality_host_address_identity.sql" {
				modified.fragments[index].Privileges = nil
				removed = true
				break
			}
		}
		if !removed {
			t.Fatal("C68 migration fragment was not found")
		}
		if err := validateAppACLCurrentTransitionPrivilegeDelta(
			transitions[5].predecessorPrivilegeBody,
			modified,
			appACLCurrentProfileP68,
		); err == nil || !strings.Contains(strings.ToLower(err.Error()), "removes") {
			t.Fatalf("validateAppACLCurrentTransitionPrivilegeDelta() error = %v, want P68 privilege-removal rejection", err)
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
	t.Run("P68 migration source golden by C68 filename", func(t *testing.T) {
		fsys := appACLCurrentTransitionTestFS(t)
		file := fsys["0068_normalize_ip_quality_host_address_identity.sql"]
		file.Data = append(append([]byte(nil), file.Data...), []byte("\n-- mutated C68 predecessor\n")...)
		current, err := compileAppACLCurrentSourceContract(fsys, appACLCurrentMigrationFragments)
		if err != nil {
			t.Fatalf("compile mutated C68 source: %v", err)
		}
		if _, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions); err == nil ||
			!strings.Contains(strings.ToLower(err.Error()), "predecessor migration source differs from released golden") {
			t.Fatalf("compileAppACLCurrentTransitions() error = %v, want independent P68 migration golden rejection", err)
		}
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
func appACLCurrentTransitionFragmentIndex(
	t *testing.T,
	fragments []AppACLCurrentMigrationFragment,
	migration string,
) int {
	t.Helper()
	for index, fragment := range fragments {
		if fragment.Migration == migration {
			return index
		}
	}
	t.Fatalf("migration fragment %q not found", migration)
	return -1
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

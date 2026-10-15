package migrate

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

// Independently stated canonical tuples, not read back from the fragment.
func appACLCurrentProductExpectedPrivileges() []AppACLPrivilege {
	return []AppACLPrivilege{
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_intelligence", Privilege: AppACLPrivilegeInsert},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_intelligence", Privilege: AppACLPrivilegeSelect},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_intelligence", Privilege: AppACLPrivilegeUpdate},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_source_cache", Privilege: AppACLPrivilegeDelete},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_source_cache", Privilege: AppACLPrivilegeInsert},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_source_cache", Privilege: AppACLPrivilegeSelect},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_source_cache", Privilege: AppACLPrivilegeUpdate},
	}
}

func appACLCurrentProductExpectedObservations(runtimeRole string) []AppACLEffectiveCatalogPrivilegeObservationR1 {
	privileges := appACLCurrentProductExpectedPrivileges()
	observations := make([]AppACLEffectiveCatalogPrivilegeObservationR1, 0, len(privileges))
	for _, privilege := range privileges {
		observations = append(observations, AppACLEffectiveCatalogPrivilegeObservationR1{
			Grantee: runtimeRole, ObjectClass: privilege.ObjectClass, SchemaName: privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity, Privilege: privilege.Privilege,
		})
	}
	return observations
}

func TestAppACLCurrentC76GoldenPreservesC75AndFamilyState(t *testing.T) {
	c75, err := ParseCanonicalPrivilegeSetBodyV1(appACLCurrentC75PrivilegeGolden)
	if err != nil {
		t.Fatal(err)
	}
	body, err := CanonicalPrivilegeSetBodyV1(c75.RoleBindings, append(c75.Privileges, AppACLPrivilege{
		Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassView, SchemaName: "public",
		ObjectIdentity: "ip_quality_vps_family_state", Privilege: AppACLPrivilegeSelect,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, appACLCurrentC76PrivilegeGolden) {
		t.Fatal("frozen C76 differs from C75 plus only family-state SELECT")
	}
	entries, err := ParseCanonicalMigrationSetBodyV1(appACLCurrentC76MigrationGolden)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := CanonicalMigrationSetBodyV1(entries[:len(entries)-1])
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 77 || entries[76].Filename != "0076_ip_quality_address_families.sql" || !bytes.Equal(prefix, appACLCurrentC75MigrationGolden) {
		t.Fatal("frozen C76 must retain the exact C75 prefix and append only 0076")
	}
}

func TestAppACLCurrentP76RejectsPublishedSourceDriftBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTransitionTestFS(t)
	file := fsys["0076_ip_quality_address_families.sql"]
	file.Data = append(append([]byte(nil), file.Data...), []byte("\n-- changed published C76 source\n")...)
	assertAppACLCurrentTransitionRejectedBeforeBeginTx(t, fsys, appACLCurrentMigrationFragments, appACLCurrentTransitionDefinitions,
		"predecessor migration source differs from released golden")
}

func TestAppACLCurrentP76PrivilegeDeltaIsExactlyProductTables(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	body, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		t.Fatal(err)
	}
	set, err := ParseCanonicalPrivilegeSetBodyV1(body)
	if err != nil {
		t.Fatal(err)
	}
	predecessor, err := ParseCanonicalPrivilegeSetBodyV1(appACLCurrentC76PrivilegeGolden)
	if err != nil {
		t.Fatal(err)
	}
	old := make(map[AppACLPrivilege]struct{}, len(predecessor.Privileges))
	for _, privilege := range predecessor.Privileges {
		old[privilege] = struct{}{}
	}
	var added []AppACLPrivilege
	for _, privilege := range set.Privileges {
		if _, present := old[privilege]; !present {
			added = append(added, privilege)
		}
	}
	if !reflect.DeepEqual(added, appACLCurrentProductExpectedPrivileges()) {
		t.Fatalf("C76 to C77 additions = %#v, want only seven runtime product grants", added)
	}
	for _, profile := range appACLCurrentTransitionDefinitions {
		for _, change := range []string{"missing", "admin", "grant_option", "intelligence_delete"} {
			t.Run(fmt.Sprintf("profile_%d_%s", profile.profile, change), func(t *testing.T) {
				modified := current
				modified.fragments = make([]appACLCurrentCompiledMigrationFragment, len(current.fragments))
				for index, fragment := range current.fragments {
					modified.fragments[index] = cloneAppACLCurrentCompiledMigrationFragment(fragment)
					if fragment.Migration != "0077_ip_quality_product.sql" {
						continue
					}
					switch change {
					case "missing":
						modified.fragments[index].Privileges = fragment.Privileges[1:]
					case "admin":
						modified.fragments[index].Privileges[0].Subject = AppACLSubjectPlatformAdmin
					case "grant_option":
						modified.fragments[index].Privileges[0].GrantOption = true
					case "intelligence_delete":
						modified.fragments[index].Privileges = append(modified.fragments[index].Privileges, AppACLPrivilege{
							Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public",
							ObjectIdentity: "ip_quality_intelligence", Privilege: AppACLPrivilegeDelete,
						})
					}
				}
				if err := validateAppACLCurrentTransitionPrivilegeDelta(profile.predecessorPrivilegeGolden, modified, profile.profile); err == nil {
					t.Fatal("accepted unapproved product privilege delta")
				}
			})
		}
	}
}

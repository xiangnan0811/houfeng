package migrate

import (
	"strings"
	"testing"
)

func TestVerifyAppACLEffectiveCatalogSnapshotR1AcceptsCompilerDerivedCompleteContract(t *testing.T) {
	contract, err := CompileAppACLEffectiveCatalogContractR1("houfeng", []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, "houfeng_migrator")
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() error = %v", err)
	}

	roleBySubject := make(map[AppACLSubject]string, len(contract.RoleBindings))
	for _, binding := range contract.RoleBindings {
		roleBySubject[binding.Subject] = binding.CatalogRole
	}
	snapshot := AppACLEffectiveCatalogSnapshotR1{
		DatabaseName: contract.DatabaseName,
		Roles: []AppACLEffectiveCatalogRoleStateR1{
			{Name: roleBySubject[AppACLSubjectCenterRuntime], Login: true},
			{Name: roleBySubject[AppACLSubjectPlatformAdmin], Login: true},
			{Name: input.MigratorRole},
		},
	}
	for _, privilege := range contract.Privileges {
		observation := AppACLEffectiveCatalogPrivilegeObservationR1{
			Grantee:        roleBySubject[privilege.Subject],
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
			ColumnName:     privilege.ColumnName,
			Privilege:      privilege.Privilege,
		}
		snapshot.DirectPrivileges = append(snapshot.DirectPrivileges, observation)
		snapshot.EffectivePrivileges = append(snapshot.EffectivePrivileges, observation)
	}
	for _, expected := range input.ExpectedFunctions {
		name := strings.TrimSuffix(strings.TrimPrefix(expected.Identity, "public."), "(bytea)")
		snapshot.Functions = append(snapshot.Functions, AppACLEffectiveCatalogFunctionR1{
			SchemaName:        "public",
			Name:              name,
			IdentityArguments: "bytea",
			Identity:          expected.Identity,
			OwnerRole:         expected.OwnerRole,
			Kind:              "f",
			SecurityDefiner:   true,
			Config:            []string{"search_path=pg_catalog"},
		})
	}

	if err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input); err != nil {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v", err)
	}
}

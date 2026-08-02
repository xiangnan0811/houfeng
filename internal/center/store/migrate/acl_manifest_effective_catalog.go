package migrate

import "fmt"

const appACLEffectiveCatalogR1PrivilegeCount = 204

// AppACLEffectiveCatalogRolePolicyR1 is the expected non-grant catalog state
// for one application role. Every boolean records the presence of that
// capability or surface; r1 requires every one to be false.
type AppACLEffectiveCatalogRolePolicyR1 struct {
	Subject                AppACLSubject
	CatalogRole            string
	Login                  bool
	Inherit                bool
	Superuser              bool
	CreateDatabase         bool
	CreateRole             bool
	Replication            bool
	BypassRLS              bool
	Membership             bool
	TemporaryObjects       bool
	SchemaCreate           bool
	GrantOption            bool
	DefaultACL             bool
	OwnsApplicationObjects bool
	ReusesMigratorRole     bool
}

// AppACLEffectiveCatalogContractR1 is the closed expected application
// catalog state for the r1 ACL manifest. Fixed arrays intentionally keep the
// complete contract comparable for exact catalog convergence checks.
type AppACLEffectiveCatalogContractR1 struct {
	DatabaseName string
	RoleBindings [2]AppACLRoleBinding
	Privileges   [appACLEffectiveCatalogR1PrivilegeCount]AppACLPrivilege
	RolePolicies [2]AppACLEffectiveCatalogRolePolicyR1
}

type appACLEffectiveCatalogFunctionContract struct {
	SchemaName      string
	Identity        string
	OwnerRole       string
	Kind            string
	SecurityDefiner bool
	Config          []string
}

type appACLEffectiveCatalogContract struct {
	DatabaseName      string
	RoleBindings      []AppACLRoleBinding
	Privileges        []AppACLPrivilege
	ManagedObjects    []AppACLManagedObjectR1
	ExpectedFunctions []appACLEffectiveCatalogFunctionContract
}

// CompileAppACLEffectiveCatalogContractR1 derives the r1 expected catalog
// contract from the sole canonical application privilege-set compiler.
func CompileAppACLEffectiveCatalogContractR1(databaseName string, bindings []AppACLRoleBinding) (AppACLEffectiveCatalogContractR1, error) {
	canonicalPrivilegeSet, err := CompileAppACLPrivilegeSetR1(databaseName, bindings)
	if err != nil {
		return AppACLEffectiveCatalogContractR1{}, fmt.Errorf("compile app ACL r1 privilege set: %w", err)
	}
	decoded, err := ParseCanonicalPrivilegeSetBodyV1(canonicalPrivilegeSet)
	if err != nil {
		return AppACLEffectiveCatalogContractR1{}, fmt.Errorf("parse compiled app ACL r1 privilege set: %w", err)
	}

	var contract AppACLEffectiveCatalogContractR1
	if len(decoded.RoleBindings) != len(contract.RoleBindings) {
		return AppACLEffectiveCatalogContractR1{}, fmt.Errorf("compiled app ACL r1 role binding count = %d, want %d", len(decoded.RoleBindings), len(contract.RoleBindings))
	}
	if len(decoded.Privileges) != len(contract.Privileges) {
		return AppACLEffectiveCatalogContractR1{}, fmt.Errorf("compiled app ACL r1 privilege count = %d, want %d", len(decoded.Privileges), len(contract.Privileges))
	}

	contract.DatabaseName = databaseName
	copy(contract.RoleBindings[:], decoded.RoleBindings)
	copy(contract.Privileges[:], decoded.Privileges)
	for index, binding := range contract.RoleBindings {
		contract.RolePolicies[index] = AppACLEffectiveCatalogRolePolicyR1{
			Subject:     binding.Subject,
			CatalogRole: binding.CatalogRole,
		}
	}
	return contract, nil
}

func appACLEffectiveCatalogContractFromR1(
	r1 AppACLEffectiveCatalogContractR1,
	migratorRole string,
) (appACLEffectiveCatalogContract, error) {
	if !validCatalogRoleName(migratorRole) {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("invalid app ACL migrator role")
	}
	compiled, err := CompileAppACLEffectiveCatalogContractR1(r1.DatabaseName, r1.RoleBindings[:])
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("compile frozen r1 catalog contract: %w", err)
	}
	if compiled != r1 {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("app ACL r1 catalog contract does not match compiler output")
	}
	for _, binding := range r1.RoleBindings {
		if binding.CatalogRole == migratorRole {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("app ACL migrator role reuses %s catalog role %q", binding.Subject, binding.CatalogRole)
		}
	}
	surface, err := CompileAppACLManagedSurfaceR1(r1.DatabaseName)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("compile frozen r1 managed surface: %w", err)
	}

	projectors := appACLProjectorFunctionsR1()
	expectedFunctions := make([]appACLEffectiveCatalogFunctionContract, 0, len(projectors))
	for _, projector := range projectors {
		expectedFunctions = append(expectedFunctions, appACLEffectiveCatalogFunctionContract{
			SchemaName:      projector.schemaName,
			Identity:        projector.identity,
			OwnerRole:       migratorRole,
			Kind:            "f",
			SecurityDefiner: true,
			Config:          []string{"search_path=pg_catalog"},
		})
	}

	return appACLEffectiveCatalogContract{
		DatabaseName:      r1.DatabaseName,
		RoleBindings:      append([]AppACLRoleBinding(nil), r1.RoleBindings[:]...),
		Privileges:        append([]AppACLPrivilege(nil), r1.Privileges[:]...),
		ManagedObjects:    append([]AppACLManagedObjectR1(nil), surface.Objects...),
		ExpectedFunctions: expectedFunctions,
	}, nil
}

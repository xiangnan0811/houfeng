package migrate

import "fmt"

const appACLEffectiveCatalogR1PrivilegeCount = 206

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

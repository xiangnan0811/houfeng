package migrate

import (
	"fmt"
	"sort"
	"strings"
)

const appACLEffectiveCatalogPublicGranteeR1 = "PUBLIC"

// AppACLEffectiveCatalogExpectedFunctionR1 identifies one function whose
// identity comes from the compiled privilege contract and whose owner must be
// the scoped migrator role.
type AppACLEffectiveCatalogExpectedFunctionR1 struct {
	Identity  string
	OwnerRole string
}

// AppACLEffectiveCatalogVerifierInputR1 keeps every non-ACL verifier input
// typed. ExpectedFunctions are derived from Contract, never supplied as a
// second hand-maintained privilege list.
type AppACLEffectiveCatalogVerifierInputR1 struct {
	Contract          AppACLEffectiveCatalogContractR1
	MigratorRole      string
	ExpectedFunctions [2]AppACLEffectiveCatalogExpectedFunctionR1
}

// AppACLEffectiveCatalogRoleStateR1 is the catalog state relevant to one
// runtime, administrator, or migrator role.
type AppACLEffectiveCatalogRoleStateR1 struct {
	Name             string
	Login            bool
	Inherit          bool
	Superuser        bool
	CreateDatabase   bool
	CreateRole       bool
	Replication      bool
	BypassRLS        bool
	TemporaryObjects bool
	SchemaCreate     bool
}

// AppACLEffectiveCatalogMembershipR1 records one direct or recursive role
// membership path that touches an application role.
type AppACLEffectiveCatalogMembershipR1 struct {
	MemberRole string
	ParentRole string
}

// AppACLEffectiveCatalogObjectOwnerR1 records ownership of an application
// database, schema, relation, or function surface.
type AppACLEffectiveCatalogObjectOwnerR1 struct {
	ObjectClass    AppACLObjectClass
	SchemaName     string
	ObjectIdentity string
	OwnerRole      string
}

// AppACLEffectiveCatalogPrivilegeObservationR1 is one direct ACL grant or
// effective privilege observed for a tracked grantee.
type AppACLEffectiveCatalogPrivilegeObservationR1 struct {
	Grantee        string
	ObjectClass    AppACLObjectClass
	SchemaName     string
	ObjectIdentity string
	ColumnName     string
	Privilege      AppACLPrivilegeKind
	GrantOption    bool
}

// AppACLEffectiveCatalogColumnACLR1 records any positive public-schema column
// ACL. R1 allows none, regardless of its grantee.
type AppACLEffectiveCatalogColumnACLR1 struct {
	SchemaName   string
	RelationName string
	ColumnName   string
	Grantee      string
	Privilege    AppACLPrivilegeKind
	GrantOption  bool
}

// AppACLEffectiveCatalogDefaultACLR1 records a default ACL that can affect an
// application role or PUBLIC. R1 allows none.
type AppACLEffectiveCatalogDefaultACLR1 struct {
	OwnerRole   string
	SchemaName  string
	ObjectType  string
	Grantee     string
	Privilege   AppACLPrivilegeKind
	GrantOption bool
}

// AppACLEffectiveCatalogFunctionR1 holds the complete pg_proc identity and
// hardening state required for a manifest function.
type AppACLEffectiveCatalogFunctionR1 struct {
	SchemaName        string
	Name              string
	IdentityArguments string
	Identity          string
	OwnerRole         string
	Kind              string
	SecurityDefiner   bool
	Config            []string
}

// AppACLEffectiveCatalogSnapshotR1 is the catalog-only portion of one
// repeatable read-only PostgreSQL snapshot.
type AppACLEffectiveCatalogSnapshotR1 struct {
	DatabaseName        string
	Roles               []AppACLEffectiveCatalogRoleStateR1
	Memberships         []AppACLEffectiveCatalogMembershipR1
	Owners              []AppACLEffectiveCatalogObjectOwnerR1
	DirectPrivileges    []AppACLEffectiveCatalogPrivilegeObservationR1
	EffectivePrivileges []AppACLEffectiveCatalogPrivilegeObservationR1
	ColumnACLs          []AppACLEffectiveCatalogColumnACLR1
	DefaultACLs         []AppACLEffectiveCatalogDefaultACLR1
	Functions           []AppACLEffectiveCatalogFunctionR1
}

// NewAppACLEffectiveCatalogVerifierInputR1 derives every expected function
// identity from the supplied compiler result and binds both functions to the
// explicit scoped migrator role.
func NewAppACLEffectiveCatalogVerifierInputR1(
	contract AppACLEffectiveCatalogContractR1,
	migratorRole string,
) (AppACLEffectiveCatalogVerifierInputR1, error) {
	input := AppACLEffectiveCatalogVerifierInputR1{
		Contract:     contract,
		MigratorRole: migratorRole,
	}
	if !validCatalogRoleName(migratorRole) {
		return AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("invalid app ACL migrator role")
	}
	functions, err := appACLEffectiveCatalogExpectedFunctionsR1(contract, migratorRole)
	if err != nil {
		return AppACLEffectiveCatalogVerifierInputR1{}, err
	}
	input.ExpectedFunctions = functions
	if err := input.Validate(); err != nil {
		return AppACLEffectiveCatalogVerifierInputR1{}, err
	}
	return input, nil
}

// Validate proves that Contract is exactly the r1 compiler output for its
// own database and bindings, preventing caller-provided ACL substitutions.
func (input AppACLEffectiveCatalogVerifierInputR1) Validate() error {
	if !validCatalogRoleName(input.MigratorRole) {
		return fmt.Errorf("invalid app ACL migrator role")
	}
	compiled, err := CompileAppACLEffectiveCatalogContractR1(input.Contract.DatabaseName, input.Contract.RoleBindings[:])
	if err != nil {
		return fmt.Errorf("compile expected app ACL catalog contract: %w", err)
	}
	if compiled != input.Contract {
		return fmt.Errorf("app ACL catalog contract does not match compiler output")
	}
	for _, binding := range input.Contract.RoleBindings {
		if binding.CatalogRole == input.MigratorRole {
			return fmt.Errorf("app ACL migrator role reuses %s catalog role %q", binding.Subject, binding.CatalogRole)
		}
	}
	expectedFunctions, err := appACLEffectiveCatalogExpectedFunctionsR1(input.Contract, input.MigratorRole)
	if err != nil {
		return err
	}
	if expectedFunctions != input.ExpectedFunctions {
		return fmt.Errorf("app ACL expected function input does not match compiled contract")
	}
	return nil
}

func appACLEffectiveCatalogExpectedFunctionsR1(
	contract AppACLEffectiveCatalogContractR1,
	migratorRole string,
) ([2]AppACLEffectiveCatalogExpectedFunctionR1, error) {
	identities := make([]string, 0, len(contract.Privileges))
	for _, privilege := range contract.Privileges {
		if privilege.ObjectClass == AppACLObjectClassFunction {
			if privilege.Privilege != AppACLPrivilegeExecute || privilege.GrantOption || !validFunctionIdentity(privilege.ObjectIdentity) {
				return [2]AppACLEffectiveCatalogExpectedFunctionR1{}, fmt.Errorf("compiled app ACL catalog contract has invalid function privilege")
			}
			identities = append(identities, privilege.ObjectIdentity)
		}
	}
	if len(identities) != 2 {
		return [2]AppACLEffectiveCatalogExpectedFunctionR1{}, fmt.Errorf("compiled app ACL catalog contract has %d manifest functions, want 2", len(identities))
	}
	sort.Strings(identities)
	if identities[0] == identities[1] {
		return [2]AppACLEffectiveCatalogExpectedFunctionR1{}, fmt.Errorf("compiled app ACL catalog contract has duplicate manifest function identity")
	}
	return [2]AppACLEffectiveCatalogExpectedFunctionR1{
		{Identity: identities[0], OwnerRole: migratorRole},
		{Identity: identities[1], OwnerRole: migratorRole},
	}, nil
}

// VerifyAppACLEffectiveCatalogSnapshotR1 compares one PostgreSQL snapshot to
// the closed r1 compiler contract. Missing, duplicate, unknown, or malformed
// catalog facts are all rejected.
func VerifyAppACLEffectiveCatalogSnapshotR1(
	snapshot AppACLEffectiveCatalogSnapshotR1,
	input AppACLEffectiveCatalogVerifierInputR1,
) error {
	if err := input.Validate(); err != nil {
		return fmt.Errorf("validate app ACL effective catalog verifier input: %w", err)
	}
	if snapshot.DatabaseName != input.Contract.DatabaseName {
		return fmt.Errorf("app ACL catalog snapshot database %q does not match expected database %q", snapshot.DatabaseName, input.Contract.DatabaseName)
	}

	roleStates := make(map[string]AppACLEffectiveCatalogRoleStateR1, 3)
	for _, role := range snapshot.Roles {
		if _, exists := roleStates[role.Name]; exists {
			return fmt.Errorf("app ACL catalog snapshot has duplicate role %q", role.Name)
		}
		roleStates[role.Name] = role
	}
	for _, binding := range input.Contract.RoleBindings {
		role, ok := roleStates[binding.CatalogRole]
		if !ok {
			return fmt.Errorf("app ACL catalog snapshot is missing %s role %q", binding.Subject, binding.CatalogRole)
		}
		if !role.Login || role.Inherit || role.Superuser || role.CreateDatabase || role.CreateRole || role.Replication || role.BypassRLS {
			return fmt.Errorf("app ACL role %q must be LOGIN, NOINHERIT, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, and NOBYPASSRLS", role.Name)
		}
	}
	if _, ok := roleStates[input.MigratorRole]; !ok {
		return fmt.Errorf("app ACL catalog snapshot is missing migrator role %q", input.MigratorRole)
	}
	if len(snapshot.Memberships) != 0 {
		membership := snapshot.Memberships[0]
		return fmt.Errorf("app ACL role membership is forbidden: %q -> %q", membership.MemberRole, membership.ParentRole)
	}

	targetRoles := make(map[string]struct{}, len(input.Contract.RoleBindings))
	for _, binding := range input.Contract.RoleBindings {
		targetRoles[binding.CatalogRole] = struct{}{}
	}
	for _, owner := range snapshot.Owners {
		if _, ownedByTargetRole := targetRoles[owner.OwnerRole]; ownedByTargetRole {
			return fmt.Errorf("app ACL role %q is owner of %s %q", owner.OwnerRole, owner.ObjectClass, appACLEffectiveCatalogObjectLabel(owner.SchemaName, owner.ObjectIdentity))
		}
	}
	for _, binding := range input.Contract.RoleBindings {
		role := roleStates[binding.CatalogRole]
		if role.TemporaryObjects {
			return fmt.Errorf("app ACL role %q has TEMP privilege", role.Name)
		}
		if role.SchemaCreate {
			return fmt.Errorf("app ACL role %q has public schema CREATE privilege", role.Name)
		}
	}

	for _, privilege := range snapshot.DirectPrivileges {
		if privilege.Grantee == appACLEffectiveCatalogPublicGranteeR1 {
			return fmt.Errorf("PUBLIC has direct %s privilege on %s", privilege.Privilege, appACLEffectiveCatalogObjectLabel(privilege.SchemaName, privilege.ObjectIdentity))
		}
	}
	if len(snapshot.ColumnACLs) != 0 {
		columnACL := snapshot.ColumnACLs[0]
		return fmt.Errorf("column ACL drift on %s.%s(%s)", columnACL.SchemaName, columnACL.RelationName, columnACL.ColumnName)
	}
	if len(snapshot.DefaultACLs) != 0 {
		defaultACL := snapshot.DefaultACLs[0]
		return fmt.Errorf("default ACL drift for owner %q and grantee %q", defaultACL.OwnerRole, defaultACL.Grantee)
	}
	if err := verifyAppACLEffectiveCatalogFunctionsR1(snapshot.Functions, input.ExpectedFunctions); err != nil {
		return err
	}
	if err := verifyAppACLEffectiveCatalogPrivilegesR1("direct", snapshot.DirectPrivileges, input.Contract); err != nil {
		return err
	}
	if err := verifyAppACLEffectiveCatalogPrivilegesR1("effective", snapshot.EffectivePrivileges, input.Contract); err != nil {
		return err
	}
	return nil
}

func verifyAppACLEffectiveCatalogFunctionsR1(
	functions []AppACLEffectiveCatalogFunctionR1,
	expected [2]AppACLEffectiveCatalogExpectedFunctionR1,
) error {
	for _, want := range expected {
		name := strings.TrimSuffix(strings.TrimPrefix(want.Identity, "public."), "(bytea)")
		matches := make([]AppACLEffectiveCatalogFunctionR1, 0, 1)
		for _, function := range functions {
			if function.SchemaName == "public" && function.Name == name {
				matches = append(matches, function)
			}
		}
		if len(matches) != 1 {
			return fmt.Errorf("function %q has %d overloads, want exactly one", want.Identity, len(matches))
		}
		function := matches[0]
		if function.Identity != want.Identity || function.IdentityArguments != "bytea" {
			return fmt.Errorf("function %q does not have the exact bytea identity", want.Identity)
		}
		if function.OwnerRole != want.OwnerRole {
			return fmt.Errorf("function %q owner %q does not match migrator role %q", want.Identity, function.OwnerRole, want.OwnerRole)
		}
		if function.Kind != "f" {
			return fmt.Errorf("function %q has prokind %q, want f", want.Identity, function.Kind)
		}
		if !function.SecurityDefiner {
			return fmt.Errorf("function %q must be SECURITY DEFINER", want.Identity)
		}
		if len(function.Config) != 1 || function.Config[0] != "search_path=pg_catalog" {
			return fmt.Errorf("function %q must have search_path=pg_catalog", want.Identity)
		}
	}
	return nil
}

func verifyAppACLEffectiveCatalogPrivilegesR1(
	kind string,
	observed []AppACLEffectiveCatalogPrivilegeObservationR1,
	contract AppACLEffectiveCatalogContractR1,
) error {
	bindings := make(map[string]AppACLSubject, len(contract.RoleBindings))
	for _, binding := range contract.RoleBindings {
		bindings[binding.CatalogRole] = binding.Subject
	}
	expected := make(map[AppACLPrivilege]struct{}, len(contract.Privileges))
	for _, privilege := range contract.Privileges {
		expected[privilege] = struct{}{}
	}
	actual := make(map[AppACLPrivilege]struct{}, len(observed))
	for _, observation := range observed {
		if observation.Grantee == appACLEffectiveCatalogPublicGranteeR1 {
			return fmt.Errorf("PUBLIC has %s %s privilege on %s", kind, observation.Privilege, appACLEffectiveCatalogObjectLabel(observation.SchemaName, observation.ObjectIdentity))
		}
		subject, knownRole := bindings[observation.Grantee]
		if !knownRole {
			return fmt.Errorf("%s app ACL privilege has unknown grantee %q", kind, observation.Grantee)
		}
		if observation.GrantOption {
			return fmt.Errorf("%s app ACL privilege has grant option for %q", kind, observation.Grantee)
		}
		privilege := AppACLPrivilege{
			Subject:        subject,
			ObjectClass:    observation.ObjectClass,
			SchemaName:     observation.SchemaName,
			ObjectIdentity: observation.ObjectIdentity,
			ColumnName:     observation.ColumnName,
			Privilege:      observation.Privilege,
			GrantOption:    observation.GrantOption,
		}
		if _, wanted := expected[privilege]; !wanted {
			return fmt.Errorf("unexpected %s app ACL privilege for %q on %s", kind, observation.Grantee, appACLEffectiveCatalogObjectLabel(observation.SchemaName, observation.ObjectIdentity))
		}
		if _, duplicate := actual[privilege]; duplicate {
			return fmt.Errorf("duplicate %s app ACL privilege for %q on %s", kind, observation.Grantee, appACLEffectiveCatalogObjectLabel(observation.SchemaName, observation.ObjectIdentity))
		}
		actual[privilege] = struct{}{}
	}
	for privilege := range expected {
		if _, found := actual[privilege]; !found {
			return fmt.Errorf("missing %s app ACL privilege for %s on %s", kind, privilege.Subject, appACLEffectiveCatalogObjectLabel(privilege.SchemaName, privilege.ObjectIdentity))
		}
	}
	return nil
}

func appACLEffectiveCatalogObjectLabel(schemaName, objectIdentity string) string {
	if schemaName == "" {
		return objectIdentity
	}
	return schemaName + "." + objectIdentity
}

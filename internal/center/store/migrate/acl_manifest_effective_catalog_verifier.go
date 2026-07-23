package migrate

import (
	"fmt"
	"strings"
)

const appACLEffectiveCatalogPublicGranteeR1 = "PUBLIC"
const appACLEffectiveCatalogPublicSchemaDatabaseOwnerRoleR1 = "pg_database_owner"

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

// AppACLEffectiveCatalogExtensionR1 records one named PostgreSQL extension's
// placement. Extension-member procedures remain opaque to the APP function
// ACL surface, but their extension schema is still an admission invariant.
type AppACLEffectiveCatalogExtensionR1 struct {
	ExtensionName string
	SchemaName    string
}

// AppACLEffectiveCatalogSnapshotR1 is the catalog-only portion of one
// repeatable read-only PostgreSQL snapshot.
type AppACLEffectiveCatalogSnapshotR1 struct {
	DatabaseName        string
	SessionUser         string
	CurrentUser         string
	Roles               []AppACLEffectiveCatalogRoleStateR1
	Memberships         []AppACLEffectiveCatalogMembershipR1
	Owners              []AppACLEffectiveCatalogObjectOwnerR1
	DirectPrivileges    []AppACLEffectiveCatalogPrivilegeObservationR1
	EffectivePrivileges []AppACLEffectiveCatalogPrivilegeObservationR1
	ColumnACLs          []AppACLEffectiveCatalogColumnACLR1
	DefaultACLs         []AppACLEffectiveCatalogDefaultACLR1
	Functions           []AppACLEffectiveCatalogFunctionR1
	PGCryptoExtension   AppACLEffectiveCatalogExtensionR1
}

// NewAppACLEffectiveCatalogVerifierInputR1 derives the fixed public projector
// inventory while proving the supplied compiler result grants no persistent
// functions, then binds both projectors to the explicit scoped migrator role.
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
		return fmt.Errorf("app ACL expected function input does not match static projector inventory")
	}
	return nil
}

func appACLEffectiveCatalogExpectedFunctionsR1(
	contract AppACLEffectiveCatalogContractR1,
	migratorRole string,
) ([2]AppACLEffectiveCatalogExpectedFunctionR1, error) {
	for _, privilege := range contract.Privileges {
		if privilege.ObjectClass == AppACLObjectClassFunction {
			return [2]AppACLEffectiveCatalogExpectedFunctionR1{}, fmt.Errorf("compiled app ACL catalog contract must not grant persistent-function EXECUTE")
		}
	}
	projectors := appACLProjectorFunctionsR1()
	return [2]AppACLEffectiveCatalogExpectedFunctionR1{
		{Identity: projectors[0].schemaName + "." + projectors[0].identity, OwnerRole: migratorRole},
		{Identity: projectors[1].schemaName + "." + projectors[1].identity, OwnerRole: migratorRole},
	}, nil
}

// VerifyAppACLEffectiveCatalogSnapshotR1 compares one PostgreSQL snapshot to
// the closed r1 compiled privilege contract and static projector inventory.
// Missing, duplicate, unknown, or malformed catalog facts are all rejected.
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
	if err := verifyAppACLEffectiveCatalogPGCryptoExtensionR1(snapshot.PGCryptoExtension); err != nil {
		return err
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
	for _, roleName := range []string{
		input.Contract.RoleBindings[0].CatalogRole,
		input.Contract.RoleBindings[1].CatalogRole,
		input.MigratorRole,
	} {
		role := roleStates[roleName]
		if !role.Login || role.Inherit || role.Superuser || role.CreateDatabase || role.CreateRole || role.Replication || role.BypassRLS {
			return fmt.Errorf("app ACL role %q must be LOGIN, NOINHERIT, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, and NOBYPASSRLS", role.Name)
		}
	}
	if len(snapshot.Memberships) != 0 {
		membership := snapshot.Memberships[0]
		return fmt.Errorf("app ACL role membership is forbidden: %q -> %q", membership.MemberRole, membership.ParentRole)
	}

	if err := verifyAppACLManagedObjectOwnersR1(snapshot.Owners, input); err != nil {
		return err
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
	for _, defaultACL := range snapshot.DefaultACLs {
		if defaultACL.OwnerRole == input.MigratorRole && (defaultACL.SchemaName == "" || defaultACL.SchemaName == appACLManagedPublicSchemaR1 || defaultACL.SchemaName == appACLManagedInternalSchemaR1) {
			return fmt.Errorf("default ACL drift for migrator owner %q and grantee %q", defaultACL.OwnerRole, defaultACL.Grantee)
		}
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

func verifyAppACLEffectiveCatalogPGCryptoExtensionR1(extension AppACLEffectiveCatalogExtensionR1) error {
	if extension.ExtensionName != "pgcrypto" {
		return fmt.Errorf("pgcrypto extension is missing")
	}
	if extension.SchemaName != appACLManagedInternalSchemaR1 {
		return fmt.Errorf("pgcrypto extension schema %q does not match required schema %q", extension.SchemaName, appACLManagedInternalSchemaR1)
	}
	return nil
}

func verifyAppACLManagedObjectOwnersR1(
	owners []AppACLEffectiveCatalogObjectOwnerR1,
	input AppACLEffectiveCatalogVerifierInputR1,
) error {
	surface, err := CompileAppACLManagedSurfaceR1(input.Contract.DatabaseName)
	if err != nil {
		return fmt.Errorf("compile app ACL managed surface: %w", err)
	}
	expected := make(map[AppACLManagedObjectR1]struct{}, len(surface.Objects))
	for _, object := range surface.Objects {
		expected[object] = struct{}{}
	}
	actual := make(map[AppACLManagedObjectR1]AppACLEffectiveCatalogObjectOwnerR1, len(owners))
	for _, owner := range owners {
		object := AppACLManagedObjectR1{
			ObjectClass:    owner.ObjectClass,
			SchemaName:     owner.SchemaName,
			ObjectIdentity: owner.ObjectIdentity,
		}
		if _, wanted := expected[object]; !wanted {
			return fmt.Errorf("unexpected managed object owner for %s", appACLEffectiveCatalogObjectLabel(owner.SchemaName, owner.ObjectIdentity))
		}
		if _, duplicate := actual[object]; duplicate {
			return fmt.Errorf("duplicate managed object owner for %s", appACLEffectiveCatalogObjectLabel(owner.SchemaName, owner.ObjectIdentity))
		}
		actual[object] = owner
	}
	for object := range expected {
		if _, found := actual[object]; !found {
			return fmt.Errorf("managed object owner is missing for %s", appACLEffectiveCatalogObjectLabel(object.SchemaName, object.ObjectIdentity))
		}
	}

	databaseObject := AppACLManagedObjectR1{ObjectClass: AppACLObjectClassDatabase, ObjectIdentity: input.Contract.DatabaseName}
	databaseOwner := actual[databaseObject]
	if databaseOwner.OwnerRole != input.MigratorRole {
		return fmt.Errorf("managed database owner %q for %s does not match migrator role %q", databaseOwner.OwnerRole, databaseObject.ObjectIdentity, input.MigratorRole)
	}
	for _, object := range surface.Objects {
		owner := actual[object]
		if object.ObjectClass == AppACLObjectClassSchema && object.SchemaName == appACLManagedPublicSchemaR1 && object.ObjectIdentity == appACLManagedPublicSchemaR1 && owner.OwnerRole == appACLEffectiveCatalogPublicSchemaDatabaseOwnerRoleR1 {
			// PostgreSQL's bootstrap public schema is commonly owned by the
			// predefined pg_database_owner role. The exact database owner check
			// above binds that dynamic owner to this direct migrator snapshot.
			continue
		}
		if owner.OwnerRole != input.MigratorRole {
			if object.ObjectClass == AppACLObjectClassSchema && object.SchemaName == appACLManagedPublicSchemaR1 && object.ObjectIdentity == appACLManagedPublicSchemaR1 {
				return fmt.Errorf("managed public schema owner %q does not match migrator role %q or pg_database_owner", owner.OwnerRole, input.MigratorRole)
			}
			return fmt.Errorf("managed object owner %q for %s does not match migrator role %q", owner.OwnerRole, appACLEffectiveCatalogObjectLabel(object.SchemaName, object.ObjectIdentity), input.MigratorRole)
		}
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

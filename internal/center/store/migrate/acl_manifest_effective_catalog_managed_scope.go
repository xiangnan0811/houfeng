package migrate

import (
	"fmt"
	"strings"
)

// appACLManagedSurfaceScopeR1 turns the fixed migration inventory into the
// only object filter used by the catalog reader. Objects in public are not
// automatically APP objects: unrelated shared-database objects must never
// participate in APP admission.
type appACLManagedSurfaceScopeR1 struct {
	objects        map[AppACLManagedObjectR1]struct{}
	projectorNames map[appACLManagedFunctionNameR1]struct{}
}

type appACLManagedFunctionNameR1 struct {
	schemaName string
	name       string
}

func newAppACLManagedSurfaceScopeR1(databaseName string) (appACLManagedSurfaceScopeR1, error) {
	surface, err := CompileAppACLManagedSurfaceR1(databaseName)
	if err != nil {
		return appACLManagedSurfaceScopeR1{}, fmt.Errorf("compile app ACL managed surface: %w", err)
	}
	scope := appACLManagedSurfaceScopeR1{
		objects:        make(map[AppACLManagedObjectR1]struct{}, len(surface.Objects)),
		projectorNames: make(map[appACLManagedFunctionNameR1]struct{}, len(appACLProjectorFunctionsR1())),
	}
	for _, object := range surface.Objects {
		scope.objects[object] = struct{}{}
	}
	for _, projector := range appACLProjectorFunctionsR1() {
		name, _, found := strings.Cut(projector.identity, "(")
		if !found || name == "" {
			return appACLManagedSurfaceScopeR1{}, fmt.Errorf("invalid app ACL projector identity %q", projector.identity)
		}
		scope.projectorNames[appACLManagedFunctionNameR1{schemaName: projector.schemaName, name: name}] = struct{}{}
	}
	return scope, nil
}

func (scope appACLManagedSurfaceScopeR1) containsOwner(owner AppACLEffectiveCatalogObjectOwnerR1) bool {
	if appACLManagedInternalObjectClassR1(owner.ObjectClass, owner.SchemaName) {
		return true
	}
	object := AppACLManagedObjectR1{
		ObjectClass:    owner.ObjectClass,
		SchemaName:     owner.SchemaName,
		ObjectIdentity: owner.ObjectIdentity,
	}
	_, ok := scope.objects[object]
	return ok
}

func (scope appACLManagedSurfaceScopeR1) containsPrivilege(privilege AppACLEffectiveCatalogPrivilegeObservationR1) bool {
	if privilege.ObjectClass == AppACLObjectClassFunction {
		schemaName, name, found := appACLFunctionNameFromQualifiedIdentityR1(privilege.ObjectIdentity)
		if found && scope.containsProjectorName(schemaName, name) {
			return true
		}
	}
	object, found := appACLManagedObjectFromPrivilegeR1(privilege)
	if !found {
		return false
	}
	if appACLManagedInternalObjectClassR1(object.ObjectClass, object.SchemaName) {
		return true
	}
	_, ok := scope.objects[object]
	return ok
}

func (scope appACLManagedSurfaceScopeR1) containsColumnACL(columnACL AppACLEffectiveCatalogColumnACLR1) bool {
	if columnACL.SchemaName == appACLManagedInternalSchemaR1 {
		return true
	}
	for _, objectClass := range []AppACLObjectClass{AppACLObjectClassTable, AppACLObjectClassView} {
		if _, ok := scope.objects[AppACLManagedObjectR1{
			ObjectClass:    objectClass,
			SchemaName:     columnACL.SchemaName,
			ObjectIdentity: columnACL.RelationName,
		}]; ok {
			return true
		}
	}
	return false
}

func (scope appACLManagedSurfaceScopeR1) containsFunction(function AppACLEffectiveCatalogFunctionR1) bool {
	if scope.containsProjectorName(function.SchemaName, function.Name) {
		return true
	}
	if appACLManagedInternalObjectClassR1(AppACLObjectClassFunction, function.SchemaName) {
		return true
	}
	object := AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     function.SchemaName,
		ObjectIdentity: function.Name + "(" + function.IdentityArguments + ")",
	}
	_, ok := scope.objects[object]
	return ok
}

func appACLManagedInternalObjectClassR1(objectClass AppACLObjectClass, schemaName string) bool {
	if schemaName != appACLManagedInternalSchemaR1 {
		return false
	}
	switch objectClass {
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence, AppACLObjectClassFunction:
		return true
	default:
		return false
	}
}

func (scope appACLManagedSurfaceScopeR1) containsProjectorName(schemaName, name string) bool {
	_, ok := scope.projectorNames[appACLManagedFunctionNameR1{schemaName: schemaName, name: name}]
	return ok
}

func (scope appACLManagedSurfaceScopeR1) publicProjectorFunctionNames() []string {
	names := make([]string, 0, len(scope.projectorNames))
	for projector := range scope.projectorNames {
		if projector.schemaName == appACLManagedPublicSchemaR1 {
			names = append(names, projector.name)
		}
	}
	return names
}

func appACLManagedObjectFromPrivilegeR1(privilege AppACLEffectiveCatalogPrivilegeObservationR1) (AppACLManagedObjectR1, bool) {
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabase:
		return AppACLManagedObjectR1{ObjectClass: privilege.ObjectClass, ObjectIdentity: privilege.ObjectIdentity}, true
	case AppACLObjectClassSchema:
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.ObjectIdentity,
			ObjectIdentity: privilege.ObjectIdentity,
		}, true
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence:
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
		}, true
	case AppACLObjectClassFunction:
		schemaName, functionIdentity, found := appACLFunctionIdentityFromQualifiedIdentityR1(privilege.ObjectIdentity)
		if !found {
			return AppACLManagedObjectR1{}, false
		}
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     schemaName,
			ObjectIdentity: functionIdentity,
		}, true
	default:
		return AppACLManagedObjectR1{}, false
	}
}

func appACLFunctionIdentityFromQualifiedIdentityR1(identity string) (schemaName, functionIdentity string, found bool) {
	schemaName, functionIdentity, found = strings.Cut(identity, ".")
	if !found || schemaName == "" || functionIdentity == "" {
		return "", "", false
	}
	functionName, arguments, hasArguments := strings.Cut(functionIdentity, "(")
	return schemaName, functionIdentity, functionName != "" && hasArguments && strings.HasSuffix(arguments, ")")
}

func appACLFunctionNameFromQualifiedIdentityR1(identity string) (schemaName, name string, found bool) {
	schemaName, functionIdentity, found := appACLFunctionIdentityFromQualifiedIdentityR1(identity)
	if !found {
		return "", "", false
	}
	name, _, found = strings.Cut(functionIdentity, "(")
	return schemaName, name, found && name != ""
}

func scopeAppACLEffectiveCatalogSnapshotR1(
	snapshot AppACLEffectiveCatalogSnapshotR1,
	scope appACLManagedSurfaceScopeR1,
) AppACLEffectiveCatalogSnapshotR1 {
	snapshot.Owners = scope.filterOwners(snapshot.Owners)

	snapshot.DirectPrivileges = scopeAppACLEffectiveCatalogPrivilegesR1(snapshot.DirectPrivileges, scope)
	snapshot.EffectivePrivileges = scopeAppACLEffectiveCatalogPrivilegesR1(snapshot.EffectivePrivileges, scope)

	snapshot.ColumnACLs = scope.filterColumnACLs(snapshot.ColumnACLs)
	snapshot.Functions = scope.filterFunctions(snapshot.Functions)

	return snapshot
}

func (scope appACLManagedSurfaceScopeR1) filterOwners(owners []AppACLEffectiveCatalogObjectOwnerR1) []AppACLEffectiveCatalogObjectOwnerR1 {
	filtered := make([]AppACLEffectiveCatalogObjectOwnerR1, 0, len(owners))
	for _, owner := range owners {
		if scope.containsOwner(owner) {
			filtered = append(filtered, owner)
		}
	}
	return filtered
}

func (scope appACLManagedSurfaceScopeR1) filterColumnACLs(columnACLs []AppACLEffectiveCatalogColumnACLR1) []AppACLEffectiveCatalogColumnACLR1 {
	filtered := make([]AppACLEffectiveCatalogColumnACLR1, 0, len(columnACLs))
	for _, columnACL := range columnACLs {
		if scope.containsColumnACL(columnACL) {
			filtered = append(filtered, columnACL)
		}
	}
	return filtered
}

func (scope appACLManagedSurfaceScopeR1) filterFunctions(functions []AppACLEffectiveCatalogFunctionR1) []AppACLEffectiveCatalogFunctionR1 {
	filtered := make([]AppACLEffectiveCatalogFunctionR1, 0, len(functions))
	for _, function := range functions {
		if scope.containsFunction(function) {
			filtered = append(filtered, function)
		}
	}
	return filtered
}

func scopeAppACLEffectiveCatalogPrivilegesR1(
	privileges []AppACLEffectiveCatalogPrivilegeObservationR1,
	scope appACLManagedSurfaceScopeR1,
) []AppACLEffectiveCatalogPrivilegeObservationR1 {
	filtered := make([]AppACLEffectiveCatalogPrivilegeObservationR1, 0, len(privileges))
	for _, privilege := range privileges {
		if scope.containsPrivilege(privilege) {
			filtered = append(filtered, privilege)
		}
	}
	return filtered
}

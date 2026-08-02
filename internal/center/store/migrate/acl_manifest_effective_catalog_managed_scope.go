package migrate

import (
	"fmt"
	"sort"
	"strings"
)

// appACLManagedSurfaceScope turns one compiled migration inventory into the
// only object filter used by the catalog reader. Objects in public are not
// automatically APP objects: unrelated shared-database objects never
// participate in APP admission.
type appACLManagedSurfaceScope struct {
	objects        map[AppACLManagedObjectR1]struct{}
	functionNames  map[appACLManagedFunctionName]struct{}
	managedSchemas map[string]struct{}
	schemaNames    []string
}

type appACLManagedFunctionName struct {
	schemaName string
	name       string
}

type appACLManagedSurfaceScopeR1 = appACLManagedSurfaceScope
type appACLManagedFunctionNameR1 = appACLManagedFunctionName

func newAppACLManagedSurfaceScope(contract appACLEffectiveCatalogContract) (appACLManagedSurfaceScope, error) {
	objects, err := canonicalAppACLManagedObjects(contract.ManagedObjects)
	if err != nil {
		return appACLManagedSurfaceScope{}, fmt.Errorf("canonicalize app ACL managed surface: %w", err)
	}
	scope := appACLManagedSurfaceScope{
		objects:        make(map[AppACLManagedObjectR1]struct{}, len(objects)),
		functionNames:  make(map[appACLManagedFunctionName]struct{}, len(contract.ExpectedFunctions)),
		managedSchemas: make(map[string]struct{}),
	}
	schemaSet := make(map[string]struct{})
	for _, object := range objects {
		scope.objects[object] = struct{}{}
		if object.SchemaName != "" {
			schemaSet[object.SchemaName] = struct{}{}
		}
		if object.ObjectClass == AppACLObjectClassSchema && object.SchemaName != appACLManagedPublicSchemaR1 {
			scope.managedSchemas[object.SchemaName] = struct{}{}
		}
	}
	for _, function := range contract.ExpectedFunctions {
		name, _, found := strings.Cut(function.Identity, "(")
		if !found || name == "" || !validBareCatalogName(function.SchemaName) {
			return appACLManagedSurfaceScope{}, fmt.Errorf("invalid app ACL managed function identity %q.%q", function.SchemaName, function.Identity)
		}
		scope.functionNames[appACLManagedFunctionName{schemaName: function.SchemaName, name: name}] = struct{}{}
		schemaSet[function.SchemaName] = struct{}{}
	}
	for schemaName := range schemaSet {
		scope.schemaNames = append(scope.schemaNames, schemaName)
	}
	sort.Strings(scope.schemaNames)
	return scope, nil
}

func newAppACLManagedSurfaceScopeR1(databaseName string) (appACLManagedSurfaceScopeR1, error) {
	surface, err := CompileAppACLManagedSurfaceR1(databaseName)
	if err != nil {
		return appACLManagedSurfaceScopeR1{}, fmt.Errorf("compile app ACL managed surface: %w", err)
	}
	contract := appACLEffectiveCatalogContract{
		DatabaseName:   databaseName,
		ManagedObjects: surface.Objects,
	}
	for _, projector := range appACLProjectorFunctionsR1() {
		contract.ExpectedFunctions = append(contract.ExpectedFunctions, appACLEffectiveCatalogFunctionContract{
			SchemaName: projector.schemaName,
			Identity:   projector.identity,
		})
	}
	return newAppACLManagedSurfaceScope(contract)
}

func (scope appACLManagedSurfaceScope) containsOwner(owner AppACLEffectiveCatalogObjectOwnerR1) bool {
	if scope.containsManagedSchemaObjectClass(owner.ObjectClass, owner.SchemaName) {
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

func (scope appACLManagedSurfaceScope) containsPrivilege(privilege AppACLEffectiveCatalogPrivilegeObservationR1) bool {
	if privilege.ObjectClass == AppACLObjectClassFunction {
		schemaName, name, found := appACLFunctionNameFromQualifiedIdentityR1(privilege.ObjectIdentity)
		if found && scope.containsFunctionName(schemaName, name) {
			return true
		}
	}
	object, found := appACLManagedObjectFromPrivilegeR1(privilege)
	if !found {
		return false
	}
	if scope.containsManagedSchemaObjectClass(object.ObjectClass, object.SchemaName) {
		return true
	}
	_, ok := scope.objects[object]
	return ok
}

func (scope appACLManagedSurfaceScope) containsColumnACL(columnACL AppACLEffectiveCatalogColumnACLR1) bool {
	if _, managed := scope.managedSchemas[columnACL.SchemaName]; managed {
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

func (scope appACLManagedSurfaceScope) containsFunction(function AppACLEffectiveCatalogFunctionR1) bool {
	if scope.containsFunctionName(function.SchemaName, function.Name) {
		return true
	}
	if scope.containsManagedSchemaObjectClass(AppACLObjectClassFunction, function.SchemaName) {
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

func (scope appACLManagedSurfaceScope) containsManagedSchemaObjectClass(objectClass AppACLObjectClass, schemaName string) bool {
	if _, managed := scope.managedSchemas[schemaName]; !managed {
		return false
	}
	switch objectClass {
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence, AppACLObjectClassFunction:
		return true
	default:
		return false
	}
}

func (scope appACLManagedSurfaceScope) containsFunctionName(schemaName, name string) bool {
	_, ok := scope.functionNames[appACLManagedFunctionName{schemaName: schemaName, name: name}]
	return ok
}

func (scope appACLManagedSurfaceScope) publicProjectorFunctionNames() []string {
	names := make([]string, 0, len(scope.functionNames))
	for function := range scope.functionNames {
		if function.schemaName == appACLManagedPublicSchemaR1 {
			names = append(names, function.name)
		}
	}
	sort.Strings(names)
	return names
}

func (scope appACLManagedSurfaceScope) managedSchemaNames() []string {
	return append([]string(nil), scope.schemaNames...)
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

func scopeAppACLEffectiveCatalogSnapshot(
	snapshot AppACLEffectiveCatalogSnapshotR1,
	scope appACLManagedSurfaceScope,
) AppACLEffectiveCatalogSnapshotR1 {
	snapshot.Owners = scope.filterOwners(snapshot.Owners)

	snapshot.DirectPrivileges = scopeAppACLEffectiveCatalogPrivileges(snapshot.DirectPrivileges, scope)
	snapshot.EffectivePrivileges = scopeAppACLEffectiveCatalogPrivileges(snapshot.EffectivePrivileges, scope)

	snapshot.ColumnACLs = scope.filterColumnACLs(snapshot.ColumnACLs)
	snapshot.Functions = scope.filterFunctions(snapshot.Functions)

	return snapshot
}

func scopeAppACLEffectiveCatalogSnapshotR1(
	snapshot AppACLEffectiveCatalogSnapshotR1,
	scope appACLManagedSurfaceScopeR1,
) AppACLEffectiveCatalogSnapshotR1 {
	return scopeAppACLEffectiveCatalogSnapshot(snapshot, scope)
}

func (scope appACLManagedSurfaceScope) filterOwners(owners []AppACLEffectiveCatalogObjectOwnerR1) []AppACLEffectiveCatalogObjectOwnerR1 {
	filtered := make([]AppACLEffectiveCatalogObjectOwnerR1, 0, len(owners))
	for _, owner := range owners {
		if scope.containsOwner(owner) {
			filtered = append(filtered, owner)
		}
	}
	return filtered
}

func (scope appACLManagedSurfaceScope) filterColumnACLs(columnACLs []AppACLEffectiveCatalogColumnACLR1) []AppACLEffectiveCatalogColumnACLR1 {
	filtered := make([]AppACLEffectiveCatalogColumnACLR1, 0, len(columnACLs))
	for _, columnACL := range columnACLs {
		if scope.containsColumnACL(columnACL) {
			filtered = append(filtered, columnACL)
		}
	}
	return filtered
}

func (scope appACLManagedSurfaceScope) filterFunctions(functions []AppACLEffectiveCatalogFunctionR1) []AppACLEffectiveCatalogFunctionR1 {
	filtered := make([]AppACLEffectiveCatalogFunctionR1, 0, len(functions))
	for _, function := range functions {
		if scope.containsFunction(function) {
			filtered = append(filtered, function)
		}
	}
	return filtered
}

func scopeAppACLEffectiveCatalogPrivileges(
	privileges []AppACLEffectiveCatalogPrivilegeObservationR1,
	scope appACLManagedSurfaceScope,
) []AppACLEffectiveCatalogPrivilegeObservationR1 {
	filtered := make([]AppACLEffectiveCatalogPrivilegeObservationR1, 0, len(privileges))
	for _, privilege := range privileges {
		if scope.containsPrivilege(privilege) {
			filtered = append(filtered, privilege)
		}
	}
	return filtered
}

func scopeAppACLEffectiveCatalogPrivilegesR1(
	privileges []AppACLEffectiveCatalogPrivilegeObservationR1,
	scope appACLManagedSurfaceScopeR1,
) []AppACLEffectiveCatalogPrivilegeObservationR1 {
	return scopeAppACLEffectiveCatalogPrivileges(privileges, scope)
}

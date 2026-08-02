package migrate

import (
	"fmt"
	"io/fs"
	"strings"
)

const appACLCurrentR1BoundaryMigration = "0051_create_record_platform_foundation.sql"
const appACLCurrentValidationDatabase = "app_acl_current_validation"

type AppACLCurrentFunctionContract struct {
	SchemaName      string
	Identity        string
	Kind            string
	SecurityDefiner bool
	Config          []string
}

type AppACLCurrentMigrationFragment struct {
	Migration  string
	Objects    []AppACLManagedObjectR1
	Privileges func(databaseName string) []AppACLPrivilege
	Functions  []AppACLCurrentFunctionContract
}

var appACLCurrentMigrationFragments = []AppACLCurrentMigrationFragment{}

type appACLCurrentSourceContract struct {
	sources   migrationSourceSnapshot
	fragments []AppACLCurrentMigrationFragment
}

func compileAppACLCurrentSourceContract(
	fsys fs.FS,
	fragments []AppACLCurrentMigrationFragment,
) (appACLCurrentSourceContract, error) {
	sources, err := snapshotMigrationSources(fsys)
	if err != nil {
		return appACLCurrentSourceContract{}, fmt.Errorf("snapshot current migration sources: %w", err)
	}
	if err := validateAppACLR1FrozenSourcePrefix(sources); err != nil {
		return appACLCurrentSourceContract{}, fmt.Errorf("validate current migration frozen r1 prefix: %w", err)
	}

	r1SourceCount := len(appACLR1MigrationSourceContract)
	laterNames := sources.names[r1SourceCount:]
	laterSet := make(map[string]struct{}, len(laterNames))
	for _, name := range laterNames {
		if name <= appACLCurrentR1BoundaryMigration {
			return appACLCurrentSourceContract{}, fmt.Errorf("current migration %q must sort after %q", name, appACLCurrentR1BoundaryMigration)
		}
		laterSet[name] = struct{}{}
	}

	fragmentByMigration := make(map[string]AppACLCurrentMigrationFragment, len(fragments))
	for _, fragment := range fragments {
		if _, duplicate := fragmentByMigration[fragment.Migration]; duplicate {
			return appACLCurrentSourceContract{}, fmt.Errorf("duplicate current APP ACL fragment for migration %q", fragment.Migration)
		}
		fragmentByMigration[fragment.Migration] = cloneAppACLCurrentMigrationFragment(fragment)
	}
	for migration := range fragmentByMigration {
		if _, present := laterSet[migration]; !present {
			return appACLCurrentSourceContract{}, fmt.Errorf("current APP ACL fragment migration %q is not present in the current migration sources", migration)
		}
	}

	orderedFragments := make([]AppACLCurrentMigrationFragment, 0, len(laterNames))
	for _, migration := range laterNames {
		fragment, present := fragmentByMigration[migration]
		if !present {
			return appACLCurrentSourceContract{}, fmt.Errorf("migration %q has no current APP ACL fragment", migration)
		}
		orderedFragments = append(orderedFragments, fragment)
	}
	if err := validateAppACLCurrentFragments(orderedFragments); err != nil {
		return appACLCurrentSourceContract{}, err
	}

	return appACLCurrentSourceContract{sources: sources, fragments: orderedFragments}, nil
}

func cloneAppACLCurrentMigrationFragment(fragment AppACLCurrentMigrationFragment) AppACLCurrentMigrationFragment {
	cloned := AppACLCurrentMigrationFragment{
		Migration: fragment.Migration,
		Objects:   append([]AppACLManagedObjectR1(nil), fragment.Objects...),
		Functions: append([]AppACLCurrentFunctionContract(nil), fragment.Functions...),
	}
	for index := range cloned.Functions {
		cloned.Functions[index].Config = append([]string(nil), cloned.Functions[index].Config...)
	}
	if fragment.Privileges != nil {
		compilePrivileges := fragment.Privileges
		cloned.Privileges = func(databaseName string) []AppACLPrivilege {
			return append([]AppACLPrivilege(nil), compilePrivileges(databaseName)...)
		}
	}
	return cloned
}

func validateAppACLCurrentFragments(fragments []AppACLCurrentMigrationFragment) error {
	baseSurface, err := CompileAppACLManagedSurfaceR1(appACLCurrentValidationDatabase)
	if err != nil {
		return fmt.Errorf("compile frozen r1 managed surface for current APP ACL fragments: %w", err)
	}
	managedObjects := make(map[AppACLManagedObjectR1]struct{}, len(baseSurface.Objects))
	for _, object := range baseSurface.Objects {
		managedObjects[object] = struct{}{}
	}
	newFunctions := make(map[AppACLManagedObjectR1]struct{})
	for _, fragment := range fragments {
		for _, object := range fragment.Objects {
			if err := validateAppACLCurrentManagedObject(object); err != nil {
				return fmt.Errorf("current APP ACL fragment %q managed object: %w", fragment.Migration, err)
			}
			if _, duplicate := managedObjects[object]; duplicate {
				return fmt.Errorf("current APP ACL fragment %q has duplicate managed object %#v", fragment.Migration, object)
			}
			managedObjects[object] = struct{}{}
			if object.ObjectClass == AppACLObjectClassFunction {
				newFunctions[object] = struct{}{}
			}
		}
	}

	privileges := appACLPrivilegesR1(appACLCurrentValidationDatabase)
	for _, fragment := range fragments {
		if fragment.Privileges == nil {
			return fmt.Errorf("current APP ACL fragment %q has no privilege compiler", fragment.Migration)
		}
		fragmentPrivileges := fragment.Privileges(appACLCurrentValidationDatabase)
		for _, privilege := range fragmentPrivileges {
			object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
			if err != nil {
				return fmt.Errorf("current APP ACL fragment %q privilege: %w", fragment.Migration, err)
			}
			if _, managed := managedObjects[object]; !managed {
				return fmt.Errorf("current APP ACL fragment %q privilege references unmanaged object %#v", fragment.Migration, object)
			}
		}
		privileges = append(privileges, fragmentPrivileges...)
	}
	if _, err := canonicalPrivileges(privileges); err != nil {
		return fmt.Errorf("validate current APP ACL fragment privileges: %w", err)
	}

	hardenedFunctions := make(map[AppACLManagedObjectR1]struct{}, len(newFunctions))
	for _, fragment := range fragments {
		for _, function := range fragment.Functions {
			object := AppACLManagedObjectR1{
				ObjectClass:    AppACLObjectClassFunction,
				SchemaName:     function.SchemaName,
				ObjectIdentity: function.Identity,
			}
			if err := validateAppACLCurrentManagedObject(object); err != nil {
				return fmt.Errorf("current APP ACL fragment %q function hardening: %w", fragment.Migration, err)
			}
			if _, managed := newFunctions[object]; !managed {
				return fmt.Errorf("current APP ACL fragment %q function hardening references unmanaged function %#v", fragment.Migration, object)
			}
			if _, duplicate := hardenedFunctions[object]; duplicate {
				return fmt.Errorf("current APP ACL fragment %q has duplicate function hardening for %#v", fragment.Migration, object)
			}
			hardenedFunctions[object] = struct{}{}
		}
	}
	for function := range newFunctions {
		if _, hardened := hardenedFunctions[function]; !hardened {
			return fmt.Errorf("current APP ACL managed function has no hardening contract: %#v", function)
		}
	}
	return nil
}

func validateAppACLCurrentManagedObject(object AppACLManagedObjectR1) error {
	switch object.ObjectClass {
	case AppACLObjectClassDatabase:
		if object.SchemaName != "" || !validBareCatalogName(object.ObjectIdentity) {
			return fmt.Errorf("invalid database object %#v", object)
		}
	case AppACLObjectClassSchema:
		if object.SchemaName != object.ObjectIdentity || !validBareCatalogName(object.SchemaName) {
			return fmt.Errorf("invalid schema object %#v", object)
		}
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence:
		if !validBareCatalogName(object.SchemaName) || !validBareCatalogName(object.ObjectIdentity) {
			return fmt.Errorf("invalid relation object %#v", object)
		}
	case AppACLObjectClassFunction:
		if !validBareCatalogName(object.SchemaName) || !validAppACLCurrentFunctionIdentity(object.ObjectIdentity) {
			return fmt.Errorf("invalid function object %#v", object)
		}
	default:
		return fmt.Errorf("unsupported managed object class %q", object.ObjectClass)
	}
	return nil
}

func validAppACLCurrentFunctionIdentity(identity string) bool {
	name, arguments, found := strings.Cut(identity, "(")
	return found && validBareCatalogName(name) && strings.HasSuffix(arguments, ")") && !strings.ContainsRune(arguments, '\x00')
}

func appACLCurrentManagedObjectFromPrivilege(privilege AppACLPrivilege) (AppACLManagedObjectR1, error) {
	if err := validateAppACLPrivilege(privilege); err != nil {
		return AppACLManagedObjectR1{}, err
	}
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabase:
		return AppACLManagedObjectR1{ObjectClass: privilege.ObjectClass, ObjectIdentity: privilege.ObjectIdentity}, nil
	case AppACLObjectClassSchema:
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.ObjectIdentity,
			ObjectIdentity: privilege.ObjectIdentity,
		}, nil
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence:
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
		}, nil
	case AppACLObjectClassFunction:
		schemaName, functionIdentity, found := appACLFunctionIdentityFromQualifiedIdentityR1(privilege.ObjectIdentity)
		if !found {
			return AppACLManagedObjectR1{}, fmt.Errorf("invalid function ACL object identity %q", privilege.ObjectIdentity)
		}
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     schemaName,
			ObjectIdentity: functionIdentity,
		}, nil
	default:
		return AppACLManagedObjectR1{}, fmt.Errorf("unsupported privilege object class %q", privilege.ObjectClass)
	}
}

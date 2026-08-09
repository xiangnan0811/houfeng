package migrate

import (
	"fmt"
	"io/fs"
	"sort"
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

var appACLCurrentMigrationFragments = []AppACLCurrentMigrationFragment{
	recordsCoreAppACLCurrentMigrationFragment(),
	recordAttachmentsAppACLCurrentMigrationFragment(),
}

func recordsCoreAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 10)
	for _, table := range []string{
		"records",
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_drafts",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	objects = append(objects, AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "validate_record_revision_primary_subject()",
	})
	return AppACLCurrentMigrationFragment{
		Migration:  "0052_create_records_core.sql",
		Objects:    objects,
		Privileges: recordsCoreAppACLCurrentPrivileges,
		Functions: []AppACLCurrentFunctionContract{
			{
				SchemaName:      appACLManagedInternalSchemaR1,
				Identity:        "validate_record_revision_primary_subject()",
				Kind:            "f",
				SecurityDefiner: false,
				Config:          []string{"search_path=pg_catalog"},
			},
		},
	}
}

func recordsCoreAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 29)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	appendTable(runtime, "records",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	for _, table := range []string{
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_draft_checkpoints",
		"record_domain_activities",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	appendTable(runtime, "record_drafts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	appendTable(runtime, "record_core_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_core_purge_receipts", AppACLPrivilegeSelect)
	return privileges
}

func recordAttachmentsAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 13)
	for _, table := range []string{
		"blob_objects",
		"attachment_quota_accounts",
		"record_attachments",
		"attachment_uploads",
		"attachment_upload_parts",
		"record_revision_attachments",
		"attachment_processor_jobs",
		"content_processor_workspaces",
		"blob_gc_pins",
		"blob_gc_deletions",
		"blob_publication_intents",
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0053_create_record_attachments.sql",
		Objects:    objects,
		Privileges: recordAttachmentsAppACLCurrentPrivileges,
	}
}

func recordAttachmentsAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 46)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	for _, table := range []string{
		"blob_objects",
		"attachment_upload_parts",
		"record_revision_attachments",
		"blob_gc_pins",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	for _, table := range []string{
		"attachment_quota_accounts",
		"record_attachments",
		"attachment_uploads",
		"attachment_processor_jobs",
		"content_processor_workspaces",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert,
			AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	}
	appendTable(runtime, "blob_gc_deletions",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable(runtime, "blob_publication_intents",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	for _, table := range []string{
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
		appendTable(AppACLSubjectPlatformAdmin, table, AppACLPrivilegeSelect)
	}
	appendTable(AppACLSubjectPlatformAdmin, "blob_gc_deletions", AppACLPrivilegeSelect)
	appendTable(AppACLSubjectPlatformAdmin, "blob_publication_intents", AppACLPrivilegeSelect)
	return privileges
}

type appACLCurrentSourceContract struct {
	sources   migrationSourceSnapshot
	fragments []appACLCurrentCompiledMigrationFragment
}

type appACLCurrentCompiledMigrationFragment struct {
	Migration  string
	Objects    []AppACLManagedObjectR1
	Privileges []AppACLPrivilege
	Functions  []AppACLCurrentFunctionContract
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
	compiledFragments := make([]appACLCurrentCompiledMigrationFragment, 0, len(orderedFragments))
	for _, fragment := range orderedFragments {
		compiled, err := compileAppACLCurrentMigrationFragment(fragment)
		if err != nil {
			return appACLCurrentSourceContract{}, err
		}
		compiledFragments = append(compiledFragments, compiled)
	}
	if err := validateAppACLCurrentFragments(compiledFragments); err != nil {
		return appACLCurrentSourceContract{}, err
	}

	return appACLCurrentSourceContract{sources: sources, fragments: compiledFragments}, nil
}

func cloneAppACLCurrentMigrationFragment(fragment AppACLCurrentMigrationFragment) AppACLCurrentMigrationFragment {
	cloned := AppACLCurrentMigrationFragment{
		Migration:  fragment.Migration,
		Objects:    append([]AppACLManagedObjectR1(nil), fragment.Objects...),
		Privileges: fragment.Privileges,
		Functions:  append([]AppACLCurrentFunctionContract(nil), fragment.Functions...),
	}
	for index := range cloned.Functions {
		cloned.Functions[index].Config = append([]string(nil), cloned.Functions[index].Config...)
	}
	return cloned
}

func compileAppACLCurrentMigrationFragment(
	fragment AppACLCurrentMigrationFragment,
) (appACLCurrentCompiledMigrationFragment, error) {
	if fragment.Privileges == nil {
		return appACLCurrentCompiledMigrationFragment{}, fmt.Errorf("current APP ACL fragment %q has no privilege compiler", fragment.Migration)
	}
	return appACLCurrentCompiledMigrationFragment{
		Migration:  fragment.Migration,
		Objects:    append([]AppACLManagedObjectR1(nil), fragment.Objects...),
		Privileges: append([]AppACLPrivilege(nil), fragment.Privileges(appACLCurrentValidationDatabase)...),
		Functions:  cloneAppACLCurrentFunctionContracts(fragment.Functions),
	}, nil
}

func cloneAppACLCurrentFunctionContracts(
	functions []AppACLCurrentFunctionContract,
) []AppACLCurrentFunctionContract {
	cloned := append([]AppACLCurrentFunctionContract(nil), functions...)
	for index := range cloned {
		cloned[index].Config = append([]string(nil), cloned[index].Config...)
	}
	return cloned
}

func validateAppACLCurrentFragments(fragments []appACLCurrentCompiledMigrationFragment) error {
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
			if err := validateAppACLManagedObject(object); err != nil {
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
		for _, privilege := range fragment.Privileges {
			object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
			if err != nil {
				return fmt.Errorf("current APP ACL fragment %q privilege: %w", fragment.Migration, err)
			}
			if _, managed := managedObjects[object]; !managed {
				return fmt.Errorf("current APP ACL fragment %q privilege references unmanaged object %#v", fragment.Migration, object)
			}
		}
		privileges = append(privileges, fragment.Privileges...)
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
			if err := validateAppACLManagedObject(object); err != nil {
				return fmt.Errorf("current APP ACL fragment %q function hardening: %w", fragment.Migration, err)
			}
			if function.Kind != "f" {
				return fmt.Errorf("current APP ACL fragment %q function %s.%s has unsupported kind %q", fragment.Migration, function.SchemaName, function.Identity, function.Kind)
			}
			for _, setting := range function.Config {
				if strings.ContainsRune(setting, '\x00') {
					return fmt.Errorf("current APP ACL fragment %q function %s.%s has invalid configuration", fragment.Migration, function.SchemaName, function.Identity)
				}
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

func validateAppACLManagedObject(object AppACLManagedObjectR1) error {
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
	_, _, valid := appACLCurrentFunctionIdentityParts(identity)
	return valid
}

func appACLCurrentFunctionIdentityParts(identity string) (name string, arguments string, valid bool) {
	name, arguments, found := strings.Cut(identity, "(")
	if !found || !validBareCatalogName(name) || !strings.HasSuffix(arguments, ")") {
		return "", "", false
	}
	arguments = strings.TrimSuffix(arguments, ")")
	for _, character := range arguments {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("_ ,.[]", character) {
			continue
		}
		return "", "", false
	}
	return name, arguments, true
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

func compileAppACLCurrentCatalogContract(
	source appACLCurrentSourceContract,
	databaseName string,
	bindings []AppACLRoleBinding,
	migratorRole string,
) (appACLEffectiveCatalogContract, error) {
	r1, err := CompileAppACLEffectiveCatalogContractR1(databaseName, bindings)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("compile frozen r1 catalog base: %w", err)
	}
	contract, err := appACLEffectiveCatalogContractFromR1(r1, migratorRole)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("adapt frozen r1 catalog base: %w", err)
	}

	fragmentFunctions := make(map[AppACLManagedObjectR1]struct{})
	for _, fragment := range source.fragments {
		contract.ManagedObjects = append(contract.ManagedObjects, fragment.Objects...)
		for _, object := range fragment.Objects {
			if object.ObjectClass == AppACLObjectClassFunction {
				fragmentFunctions[object] = struct{}{}
			}
		}
		fragmentPrivileges, err := appACLCurrentPrivilegesForDatabase(fragment.Privileges, databaseName)
		if err != nil {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("materialize current APP ACL fragment %q privileges: %w", fragment.Migration, err)
		}
		contract.Privileges = append(contract.Privileges, fragmentPrivileges...)
		for _, function := range fragment.Functions {
			contract.ExpectedFunctions = append(contract.ExpectedFunctions, appACLEffectiveCatalogFunctionContract{
				SchemaName:      function.SchemaName,
				Identity:        function.Identity,
				OwnerRole:       migratorRole,
				Kind:            function.Kind,
				SecurityDefiner: function.SecurityDefiner,
				Config:          append([]string(nil), function.Config...),
			})
		}
	}

	canonicalBody, err := CanonicalPrivilegeSetBodyV1(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL privileges: %w", err)
	}
	canonicalSet, err := ParseCanonicalPrivilegeSetBodyV1(canonicalBody)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("parse current APP ACL privileges: %w", err)
	}
	contract.RoleBindings = append([]AppACLRoleBinding(nil), canonicalSet.RoleBindings...)
	contract.Privileges = append([]AppACLPrivilege(nil), canonicalSet.Privileges...)

	contract.ManagedObjects, err = canonicalAppACLManagedObjects(contract.ManagedObjects)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL managed objects: %w", err)
	}
	contract.ExpectedFunctions, err = canonicalAppACLEffectiveCatalogFunctionContracts(contract.ExpectedFunctions)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL function hardening: %w", err)
	}

	managed := make(map[AppACLManagedObjectR1]struct{}, len(contract.ManagedObjects))
	for _, object := range contract.ManagedObjects {
		managed[object] = struct{}{}
	}
	for _, privilege := range contract.Privileges {
		object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
		if err != nil {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("map current APP ACL privilege to managed object: %w", err)
		}
		if _, ok := managed[object]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL privilege references unmanaged object %#v", object)
		}
	}
	expectedFunctions := make(map[AppACLManagedObjectR1]struct{}, len(contract.ExpectedFunctions))
	for _, function := range contract.ExpectedFunctions {
		object := AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.SchemaName,
			ObjectIdentity: function.Identity,
		}
		if _, ok := managed[object]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL function hardening references unmanaged function %#v", object)
		}
		expectedFunctions[object] = struct{}{}
	}
	for function := range fragmentFunctions {
		if _, ok := expectedFunctions[function]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL managed function has no hardening contract: %#v", function)
		}
	}
	return contract, nil
}

func appACLCurrentPrivilegesForDatabase(
	template []AppACLPrivilege,
	databaseName string,
) ([]AppACLPrivilege, error) {
	if !validBareCatalogName(databaseName) {
		return nil, fmt.Errorf("invalid app ACL database name")
	}
	privileges := append([]AppACLPrivilege(nil), template...)
	for index := range privileges {
		if privileges[index].ObjectClass == AppACLObjectClassDatabase &&
			privileges[index].ObjectIdentity == appACLCurrentValidationDatabase {
			privileges[index].ObjectIdentity = databaseName
		}
	}
	return privileges, nil
}

func canonicalAppACLEffectiveCatalogFunctionContracts(
	functions []appACLEffectiveCatalogFunctionContract,
) ([]appACLEffectiveCatalogFunctionContract, error) {
	ordered := append([]appACLEffectiveCatalogFunctionContract(nil), functions...)
	for index := range ordered {
		ordered[index].Config = append([]string(nil), ordered[index].Config...)
		object := AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     ordered[index].SchemaName,
			ObjectIdentity: ordered[index].Identity,
		}
		if err := validateAppACLManagedObject(object); err != nil {
			return nil, err
		}
		if !validCatalogRoleName(ordered[index].OwnerRole) {
			return nil, fmt.Errorf("invalid function owner role %q", ordered[index].OwnerRole)
		}
		if ordered[index].Kind != "f" {
			return nil, fmt.Errorf("function %s.%s has unsupported kind %q", ordered[index].SchemaName, ordered[index].Identity, ordered[index].Kind)
		}
		for _, setting := range ordered[index].Config {
			if strings.ContainsRune(setting, '\x00') {
				return nil, fmt.Errorf("function %s.%s has invalid configuration", ordered[index].SchemaName, ordered[index].Identity)
			}
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SchemaName != ordered[j].SchemaName {
			return ordered[i].SchemaName < ordered[j].SchemaName
		}
		return ordered[i].Identity < ordered[j].Identity
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].SchemaName == ordered[index].SchemaName && ordered[index-1].Identity == ordered[index].Identity {
			return nil, fmt.Errorf("duplicate function hardening for %s.%s", ordered[index].SchemaName, ordered[index].Identity)
		}
	}
	return ordered, nil
}

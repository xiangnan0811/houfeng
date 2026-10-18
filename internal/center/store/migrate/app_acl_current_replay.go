package migrate

import (
	"fmt"
	"slices"
	"strings"
)

const agentReplayMigration = "0080_bound_agent_sync_replay.sql"

func agentReplayAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	fragment := AppACLCurrentMigrationFragment{Migration: agentReplayMigration}
	for _, table := range []string{"agent_replay_states", "agent_batch_receipts"} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table})
	}
	for _, table := range []string{"agent_sync_batches", "agent_live_signals"} {
		fragment.RetiredObjects = append(fragment.RetiredObjects, AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table})
	}
	for _, identity := range []string{"enforce_agent_replay_state()", "enforce_agent_batch_receipt()"} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: identity})
		fragment.Functions = append(fragment.Functions, AppACLCurrentFunctionContract{SchemaName: appACLManagedPublicSchemaR1, Identity: identity, Kind: "f", Config: []string{"search_path=pg_catalog"}})
	}
	fragment.Privileges = func(string) []AppACLPrivilege {
		privileges := make([]AppACLPrivilege, 0, 6)
		for _, table := range []string{"agent_replay_states", "agent_batch_receipts"} {
			kinds := []AppACLPrivilegeKind{AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate}
			if table == "agent_batch_receipts" {
				kinds[2] = AppACLPrivilegeDelete
			}
			for _, kind := range kinds {
				privileges = append(privileges, AppACLPrivilege{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table, Privilege: kind})
			}
		}
		return privileges
	}
	return fragment
}

// Materialize from the frozen R1 surface in migration order. Every catalog,
// privilege body and DCL consumer uses this same effective surface; historical
// prefixes never see a retirement from a later migration.
func materializeAppACLCurrentFragments(contract *appACLEffectiveCatalogContract, fragments []appACLCurrentCompiledMigrationFragment, databaseName, migratorRole string) (map[AppACLManagedObjectR1]struct{}, error) {
	managed := make(map[AppACLManagedObjectR1]struct{}, len(contract.ManagedObjects))
	for _, object := range contract.ManagedObjects {
		managed[object] = struct{}{}
	}
	functions := make(map[AppACLManagedObjectR1]struct{})
	hardened := make(map[AppACLManagedObjectR1]struct{})
	for _, function := range contract.ExpectedFunctions {
		hardened[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: function.SchemaName, ObjectIdentity: function.Identity}] = struct{}{}
	}
	for _, fragment := range fragments {
		retired := make(map[AppACLManagedObjectR1]struct{}, len(fragment.RetiredObjects))
		for _, object := range fragment.RetiredObjects {
			if err := validateAppACLManagedObject(object); err != nil {
				return nil, err
			}
			if _, exists := managed[object]; !exists {
				return nil, fmt.Errorf("current APP ACL fragment %q retires absent or already retired object %#v", fragment.Migration, object)
			}
			retired[object] = struct{}{}
			delete(managed, object)
			delete(functions, object)
			delete(hardened, object)
		}
		if len(retired) > 0 {
			contract.ManagedObjects = slices.DeleteFunc(contract.ManagedObjects, func(object AppACLManagedObjectR1) bool { _, found := retired[object]; return found })
			contract.Privileges = slices.DeleteFunc(contract.Privileges, func(privilege AppACLPrivilege) bool {
				object, _ := appACLCurrentManagedObjectFromPrivilege(privilege)
				_, found := retired[object]
				return found
			})
			contract.AuxiliaryPrivileges = slices.DeleteFunc(contract.AuxiliaryPrivileges, func(privilege AppACLCurrentAuxiliaryPrivilege) bool {
				object, _ := appACLCurrentAuxiliaryManagedObject(privilege)
				_, found := retired[object]
				return found
			})
			contract.ExpectedFunctions = slices.DeleteFunc(contract.ExpectedFunctions, func(function appACLEffectiveCatalogFunctionContract) bool {
				_, found := retired[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: function.SchemaName, ObjectIdentity: function.Identity}]
				return found
			})
		}
		for _, object := range fragment.Objects {
			if err := validateAppACLManagedObject(object); err != nil {
				return nil, err
			}
			if _, exists := managed[object]; exists {
				return nil, fmt.Errorf("current APP ACL fragment %q has duplicate managed object %#v", fragment.Migration, object)
			}
			if _, exists := retired[object]; exists {
				return nil, fmt.Errorf("current APP ACL fragment %q redeclares retired object %#v", fragment.Migration, object)
			}
			managed[object] = struct{}{}
			contract.ManagedObjects = append(contract.ManagedObjects, object)
			if object.ObjectClass == AppACLObjectClassFunction {
				functions[object] = struct{}{}
			}
		}
		privileges, err := appACLCurrentPrivilegesForDatabase(fragment.Privileges, databaseName)
		if err != nil {
			return nil, err
		}
		for _, privilege := range privileges {
			object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
			if err != nil {
				return nil, err
			}
			if _, exists := managed[object]; !exists {
				return nil, fmt.Errorf("current APP ACL fragment %q privilege references unmanaged object %#v", fragment.Migration, object)
			}
		}
		contract.Privileges = append(contract.Privileges, privileges...)
		auxiliary, err := appACLCurrentAuxiliaryPrivilegesForDatabase(fragment.AuxiliaryPrivileges, databaseName)
		if err != nil {
			return nil, err
		}
		for _, privilege := range auxiliary {
			object, err := appACLCurrentAuxiliaryManagedObject(privilege)
			if err != nil {
				return nil, err
			}
			if _, exists := managed[object]; !exists {
				return nil, fmt.Errorf("current APP ACL fragment %q auxiliary privilege references unmanaged object %#v", fragment.Migration, object)
			}
		}
		contract.AuxiliaryPrivileges = append(contract.AuxiliaryPrivileges, auxiliary...)
		for _, function := range fragment.Functions {
			object := AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: function.SchemaName, ObjectIdentity: function.Identity}
			if err := validateAppACLManagedObject(object); err != nil {
				return nil, err
			}
			if _, exists := functions[object]; !exists {
				return nil, fmt.Errorf("current APP ACL fragment %q hardening references unmanaged function %#v", fragment.Migration, object)
			}
			if _, exists := hardened[object]; exists {
				return nil, fmt.Errorf("current APP ACL fragment %q has duplicate function hardening %#v", fragment.Migration, object)
			}
			if function.Kind != "f" {
				return nil, fmt.Errorf("current APP ACL fragment %q function has unsupported kind %q", fragment.Migration, function.Kind)
			}
			for _, setting := range function.Config {
				if strings.ContainsRune(setting, '\x00') {
					return nil, fmt.Errorf("current APP ACL fragment %q function has invalid configuration", fragment.Migration)
				}
			}
			hardened[object] = struct{}{}
			contract.ExpectedFunctions = append(contract.ExpectedFunctions, appACLEffectiveCatalogFunctionContract{SchemaName: function.SchemaName, Identity: function.Identity, OwnerRole: migratorRole, Kind: function.Kind, SecurityDefiner: function.SecurityDefiner, Config: append([]string(nil), function.Config...)})
		}
	}
	return functions, nil
}

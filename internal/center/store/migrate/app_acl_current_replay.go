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
	workerBound := false
	for _, fragment := range fragments {
		if fragment.Migration == recordDeletionSuccessorMigration {
			workerBound = true
		}
		replaced := make(map[AppACLManagedObjectR1]struct{}, len(fragment.FunctionReplacements))
		for _, replacement := range fragment.FunctionReplacements {
			before, after := replacement.Before, replacement.After
			object := AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: before.SchemaName, ObjectIdentity: before.Identity}
			if before.SchemaName != after.SchemaName || before.Identity != after.Identity {
				return nil, fmt.Errorf("current APP ACL fragment %q changes replacement identity", fragment.Migration)
			}
			if _, duplicate := replaced[object]; duplicate {
				return nil, fmt.Errorf("current APP ACL fragment %q replaces a function twice", fragment.Migration)
			}
			replaced[object] = struct{}{}
			for _, declared := range fragment.Objects {
				if declared == object {
					return nil, fmt.Errorf("current APP ACL fragment %q adds and replaces a function", fragment.Migration)
				}
			}
			for _, retired := range fragment.RetiredObjects {
				if retired == object {
					return nil, fmt.Errorf("current APP ACL fragment %q retires and replaces a function", fragment.Migration)
				}
			}
			for _, declared := range fragment.Functions {
				if declared.SchemaName == object.SchemaName && declared.Identity == object.ObjectIdentity {
					return nil, fmt.Errorf("current APP ACL fragment %q redeclares a replacement", fragment.Migration)
				}
			}
			if _, exists := managed[object]; !exists {
				return nil, fmt.Errorf("current APP ACL fragment %q replaces an unmanaged function", fragment.Migration)
			}
			if _, exists := hardened[object]; !exists {
				return nil, fmt.Errorf("current APP ACL fragment %q replaces an unhardened function", fragment.Migration)
			}
			for _, function := range []AppACLCurrentFunctionContract{before, after} {
				if _, err := canonicalAppACLEffectiveCatalogFunctionContracts([]appACLEffectiveCatalogFunctionContract{{
					SchemaName: function.SchemaName, Identity: function.Identity, OwnerRole: migratorRole,
					Kind: function.Kind, SecurityDefiner: function.SecurityDefiner, Config: function.Config,
				}}); err != nil {
					return nil, fmt.Errorf("current APP ACL fragment %q replacement: %w", fragment.Migration, err)
				}
				settings := make(map[string]struct{}, len(function.Config))
				for _, setting := range function.Config {
					name, value, found := strings.Cut(setting, "=")
					if !found || !validBareCatalogName(name) || value == "" {
						return nil, fmt.Errorf("current APP ACL fragment %q invalid replacement configuration", fragment.Migration)
					}
					if _, duplicate := settings[name]; duplicate {
						return nil, fmt.Errorf("current APP ACL fragment %q duplicate replacement configuration", fragment.Migration)
					}
					settings[name] = struct{}{}
				}
			}
			matched := false
			for index, actual := range contract.ExpectedFunctions {
				if actual.SchemaName != before.SchemaName || actual.Identity != before.Identity {
					continue
				}
				if actual.OwnerRole != migratorRole || actual.Kind != before.Kind ||
					actual.SecurityDefiner != before.SecurityDefiner || !slices.Equal(actual.Config, before.Config) {
					return nil, fmt.Errorf("current APP ACL fragment %q replacement predecessor mismatch", fragment.Migration)
				}
				contract.ExpectedFunctions[index] = appACLEffectiveCatalogFunctionContract{
					SchemaName: after.SchemaName, Identity: after.Identity, OwnerRole: migratorRole,
					Kind: after.Kind, SecurityDefiner: after.SecurityDefiner, Config: append([]string(nil), after.Config...),
				}
				matched = true
				break
			}
			if !matched {
				return nil, fmt.Errorf("current APP ACL fragment %q replacement predecessor absent", fragment.Migration)
			}
		}
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
		revoked, err := appACLCurrentPrivilegesForDatabase(fragment.RevokedPrivileges, databaseName)
		if err != nil {
			return nil, err
		}
		revokedSet := make(map[AppACLPrivilege]struct{}, len(revoked))
		for _, privilege := range revoked {
			if err := validateAppACLPrivilege(privilege); err != nil {
				return nil, fmt.Errorf("current APP ACL fragment %q revokes invalid privilege: %w", fragment.Migration, err)
			}
			if privilege.Subject == AppACLSubjectRecordDeletionWorker && !workerBound {
				return nil, fmt.Errorf("current APP ACL fragment %q revokes a privilege of the unbound %s subject", fragment.Migration, privilege.Subject)
			}
			if _, duplicate := revokedSet[privilege]; duplicate {
				return nil, fmt.Errorf("current APP ACL fragment %q revokes a privilege twice", fragment.Migration)
			}
			revokedSet[privilege] = struct{}{}
		}
		if len(revokedSet) > 0 {
			// Every revoked tuple must be present exactly once in the replayed
			// surface; a missing tuple means the revocation was written against
			// a different history and must not be silently ignored.
			found := make(map[AppACLPrivilege]int, len(revokedSet))
			for _, privilege := range contract.Privileges {
				if _, wanted := revokedSet[privilege]; wanted {
					found[privilege]++
				}
			}
			for privilege := range revokedSet {
				if found[privilege] != 1 {
					return nil, fmt.Errorf("current APP ACL fragment %q revokes privilege %#v that is not granted exactly once by its predecessors", fragment.Migration, privilege)
				}
			}
			contract.Privileges = slices.DeleteFunc(contract.Privileges, func(privilege AppACLPrivilege) bool {
				_, removed := revokedSet[privilege]
				return removed
			})
		}
		for _, privilege := range privileges {
			if privilege.Subject == AppACLSubjectRecordDeletionWorker && !workerBound {
				return nil, fmt.Errorf("current APP ACL fragment %q grants to the unbound %s subject", fragment.Migration, privilege.Subject)
			}
			if _, regranted := revokedSet[privilege]; regranted {
				return nil, fmt.Errorf("current APP ACL fragment %q revokes and re-grants the same privilege", fragment.Migration)
			}
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

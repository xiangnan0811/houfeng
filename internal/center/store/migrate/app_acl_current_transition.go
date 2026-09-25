package migrate

import (
	"bytes"
	_ "embed"
	"fmt"
)

const (
	appACLCurrentTransitionDatabase = "houfeng"
	appACLCurrentTransitionMigrator = "houfeng_migrator"
)

var appACLCurrentTransitionBindings = []AppACLRoleBinding{
	{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_runtime"},
	{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
}

//go:embed testdata/app_acl_current_v0.79.4_migrations.v1.bin
var appACLCurrentV0794MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.79.4_privileges.v1.bin
var appACLCurrentV0794PrivilegeGolden []byte

//go:embed testdata/app_acl_current_v0.79.4_manifest_digest.v1.bin
var appACLCurrentV0794ManifestDigestGoldenBytes []byte

//go:embed testdata/app_acl_current_v0.80.2_migrations.v1.bin
var appACLCurrentV0802MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.80.2_privileges.v1.bin
var appACLCurrentV0802PrivilegeGolden []byte

var appACLCurrentV0794ManifestDigestGolden = func() [32]byte {
	var digest [32]byte
	copy(digest[:], appACLCurrentV0794ManifestDigestGoldenBytes)
	return digest
}()

type appACLCurrentProfileID uint8

const (
	appACLCurrentProfileP62 appACLCurrentProfileID = iota + 1
	appACLCurrentProfileP64
)

type appACLCurrentTransitionDefinition struct {
	profile                         appACLCurrentProfileID
	predecessorLastMigration        string
	successorMigrations             []string
	predecessorMigrationGolden      []byte
	predecessorPrivilegeGolden      []byte
	predecessorManifestDigestGolden []byte
}

type appACLCurrentTransition struct {
	profile                   appACLCurrentProfileID
	predecessor               appACLCurrentSourceContract
	successor                 migrationSourceSnapshot
	predecessorPrivilegeBody  []byte
	predecessorManifestDigest [32]byte
}

var appACLCurrentTransitionDefinitions = []appACLCurrentTransitionDefinition{
	{
		profile:                  appACLCurrentProfileP62,
		predecessorLastMigration: "0062_create_vps_create_idempotency.sql",
		successorMigrations: []string{
			"0063_tune_heartbeat_incident_policy.sql",
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
		},
		predecessorMigrationGolden:      appACLCurrentV0794MigrationGolden,
		predecessorPrivilegeGolden:      appACLCurrentV0794PrivilegeGolden,
		predecessorManifestDigestGolden: appACLCurrentV0794ManifestDigestGoldenBytes,
	},
	{
		profile:                  appACLCurrentProfileP64,
		predecessorLastMigration: "0064_add_network_rates_valid.sql",
		successorMigrations: []string{
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0802MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0802PrivilegeGolden,
	},
}

var appACLCurrentAcceptedPredecessorProfileChains = [][]appACLCurrentProfileID{
	{appACLCurrentProfileP62},
	{appACLCurrentProfileP64},
	{appACLCurrentProfileP62, appACLCurrentProfileP64},
}

func cloneAppACLCurrentTransitionDefinitions(source []appACLCurrentTransitionDefinition) []appACLCurrentTransitionDefinition {
	result := make([]appACLCurrentTransitionDefinition, len(source))
	for index, definition := range source {
		result[index] = definition
		result[index].successorMigrations = append([]string(nil), definition.successorMigrations...)
		result[index].predecessorMigrationGolden = append([]byte(nil), definition.predecessorMigrationGolden...)
		result[index].predecessorPrivilegeGolden = append([]byte(nil), definition.predecessorPrivilegeGolden...)
		result[index].predecessorManifestDigestGolden = append([]byte(nil), definition.predecessorManifestDigestGolden...)
	}
	return result
}

func compileAppACLCurrentTransitions(
	current appACLCurrentSourceContract,
	definitions []appACLCurrentTransitionDefinition,
) ([]appACLCurrentTransition, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("current APP ACL transition registry has no definitions")
	}
	if len(definitions) != 2 {
		return nil, fmt.Errorf("current APP ACL transition registry must contain exactly the P62 and P64 profiles")
	}
	expectedDefinitions := appACLCurrentTransitionDefinitions
	compiled := make([]appACLCurrentTransition, 0, len(definitions))
	claimedProfiles := make(map[appACLCurrentProfileID]struct{}, len(definitions))
	for index, definition := range cloneAppACLCurrentTransitionDefinitions(definitions) {
		if definition.profile != appACLCurrentProfileP62 && definition.profile != appACLCurrentProfileP64 {
			return nil, fmt.Errorf("current APP ACL transition %d has unknown predecessor profile", index)
		}
		if _, duplicate := claimedProfiles[definition.profile]; duplicate {
			return nil, fmt.Errorf("current APP ACL transition %d duplicates predecessor profile", index)
		}
		claimedProfiles[definition.profile] = struct{}{}
		expected := expectedDefinitions[index]
		if definition.profile != expected.profile ||
			definition.predecessorLastMigration != expected.predecessorLastMigration ||
			!equalAppACLCurrentMigrationNames(definition.successorMigrations, expected.successorMigrations) ||
			!bytes.Equal(definition.predecessorMigrationGolden, expected.predecessorMigrationGolden) ||
			!bytes.Equal(definition.predecessorPrivilegeGolden, expected.predecessorPrivilegeGolden) ||
			!bytes.Equal(definition.predecessorManifestDigestGolden, expected.predecessorManifestDigestGolden) {
			return nil, fmt.Errorf("current APP ACL transition %d does not match the registered profile, goldens, and exact current suffix", index)
		}
		predecessorIndex := indexOfAppACLCurrentMigration(current.sources.names, definition.predecessorLastMigration)
		if predecessorIndex < 0 {
			return nil, fmt.Errorf("current APP ACL transition %d has unknown predecessor %q", index, definition.predecessorLastMigration)
		}
		for successorIndex, name := range definition.successorMigrations {
			currentIndex := indexOfAppACLCurrentMigration(current.sources.names, name)
			if currentIndex != predecessorIndex+1+successorIndex {
				return nil, fmt.Errorf("current APP ACL transition %d successor migrations are out of order", index)
			}
		}
		if len(definition.successorMigrations) != len(current.sources.names)-predecessorIndex-1 {
			return nil, fmt.Errorf("current APP ACL transition %d successor migrations are not the exact current suffix", index)
		}
		transition, err := compileAppACLCurrentTransition(current, predecessorIndex, definition)
		if err != nil {
			return nil, fmt.Errorf("compile current APP ACL transition %d: %w", index, err)
		}
		compiled = append(compiled, transition)
	}
	if !bytes.Equal(compiled[0].predecessorPrivilegeBody, compiled[1].predecessorPrivilegeBody) {
		return nil, fmt.Errorf("registered P62 and P64 privilege profiles differ")
	}
	if err := validateAppACLCurrentTransitionPrivilegeDelta(compiled[1].predecessorPrivilegeBody, current); err != nil {
		return nil, err
	}
	return compiled, nil
}

func compileAppACLCurrentTransition(
	current appACLCurrentSourceContract,
	predecessorIndex int,
	definition appACLCurrentTransitionDefinition,
) (appACLCurrentTransition, error) {
	predecessorSources, err := appACLCurrentMigrationSubset(current.sources, 0, predecessorIndex+1)
	if err != nil {
		return appACLCurrentTransition{}, err
	}
	successor, err := appACLCurrentMigrationSubset(current.sources, predecessorIndex+1, len(current.sources.names))
	if err != nil {
		return appACLCurrentTransition{}, err
	}
	predecessor := appACLCurrentSourceContract{sources: predecessorSources}
	for _, fragment := range current.fragments {
		if indexOfAppACLCurrentMigration(predecessorSources.names, fragment.Migration) >= 0 {
			predecessor.fragments = append(predecessor.fragments, cloneAppACLCurrentCompiledMigrationFragment(fragment))
		}
	}
	if !bytes.Equal(predecessor.sources.canonicalSet, definition.predecessorMigrationGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor migration source differs from released golden")
	}
	predecessorPrivileges, err := appACLCurrentTransitionPrivilegeBody(predecessor)
	if err != nil {
		return appACLCurrentTransition{}, err
	}
	if !bytes.Equal(predecessorPrivileges, definition.predecessorPrivilegeGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor privilege body differs from released golden")
	}
	if definition.profile == appACLCurrentProfileP62 && len(definition.predecessorManifestDigestGolden) != 32 {
		return appACLCurrentTransition{}, fmt.Errorf("P62 predecessor manifest digest golden has invalid length")
	}
	if definition.profile == appACLCurrentProfileP64 && len(definition.predecessorManifestDigestGolden) != 0 {
		return appACLCurrentTransition{}, fmt.Errorf("P64 predecessor must support role-bound manifest identities")
	}
	manifest, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, predecessor.sources.canonicalSet, predecessorPrivileges)
	if err != nil {
		return appACLCurrentTransition{}, fmt.Errorf("build predecessor manifest: %w", err)
	}
	if len(definition.predecessorManifestDigestGolden) > 0 &&
		!bytes.Equal(manifest.ManifestDigest[:], definition.predecessorManifestDigestGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor manifest digest differs from released golden")
	}
	return appACLCurrentTransition{
		profile:                   definition.profile,
		predecessor:               predecessor,
		successor:                 successor,
		predecessorPrivilegeBody:  append([]byte(nil), predecessorPrivileges...),
		predecessorManifestDigest: manifest.ManifestDigest,
	}, nil
}

func validateAppACLCurrentTransitionPrivilegeDelta(predecessorPrivilegeBody []byte, current appACLCurrentSourceContract) error {
	oldSet, err := ParseCanonicalPrivilegeSetBodyV1(predecessorPrivilegeBody)
	if err != nil {
		return fmt.Errorf("parse registered APP predecessor privileges: %w", err)
	}
	currentBody, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		return err
	}
	currentSet, err := ParseCanonicalPrivilegeSetBodyV1(currentBody)
	if err != nil {
		return fmt.Errorf("parse current APP privileges: %w", err)
	}
	if !equalAppACLRoleBindings(oldSet.RoleBindings, currentSet.RoleBindings) {
		return fmt.Errorf("registered APP transition changed role bindings")
	}
	currentPrivileges := make(map[AppACLPrivilege]struct{}, len(currentSet.Privileges))
	for _, privilege := range currentSet.Privileges {
		currentPrivileges[privilege] = struct{}{}
	}
	for _, privilege := range oldSet.Privileges {
		if _, retained := currentPrivileges[privilege]; !retained {
			return fmt.Errorf("registered APP transition removes predecessor privilege")
		}
	}
	oldPrivileges := make(map[AppACLPrivilege]struct{}, len(oldSet.Privileges))
	for _, privilege := range oldSet.Privileges {
		oldPrivileges[privilege] = struct{}{}
	}
	additions := make(map[AppACLPrivilege]struct{}, len(currentSet.Privileges)-len(oldSet.Privileges))
	for _, privilege := range currentSet.Privileges {
		if _, existed := oldPrivileges[privilege]; !existed {
			additions[privilege] = struct{}{}
		}
	}
	expectedAdditions := map[AppACLPrivilege]struct{}{
		{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "asset_services",
			Privilege:      AppACLPrivilegeUpdate,
		}: {},
		{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "asset_domains",
			Privilege:      AppACLPrivilegeUpdate,
		}: {},
	}
	if len(additions) != len(expectedAdditions) {
		return fmt.Errorf("registered APP transition privilege delta is not exactly the two approved table UPDATE tuples")
	}
	for privilege := range expectedAdditions {
		if _, present := additions[privilege]; !present {
			return fmt.Errorf("registered APP transition privilege delta is not exactly the two approved table UPDATE tuples")
		}
	}
	return nil
}

func equalAppACLRoleBindings(left, right []AppACLRoleBinding) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func appACLCurrentMigrationSubset(source migrationSourceSnapshot, start, end int) (migrationSourceSnapshot, error) {
	entries, err := ParseCanonicalMigrationSetBodyV1(source.canonicalSet)
	if err != nil {
		return migrationSourceSnapshot{}, fmt.Errorf("parse current migration source body: %w", err)
	}
	if start < 0 || end > len(source.names) || start >= end || len(entries) != len(source.names) {
		return migrationSourceSnapshot{}, fmt.Errorf("current migration source subset is invalid")
	}
	canonical, err := CanonicalMigrationSetBodyV1(entries[start:end])
	if err != nil {
		return migrationSourceSnapshot{}, fmt.Errorf("build current migration source subset: %w", err)
	}
	result := migrationSourceSnapshot{
		sources:      make(map[string]migrationSource, end-start),
		names:        append([]string(nil), source.names[start:end]...),
		canonicalSet: canonical,
	}
	for _, name := range result.names {
		result.sources[name] = source.sources[name]
	}
	return result, nil
}

func appACLCurrentTransitionPrivilegeBody(source appACLCurrentSourceContract) ([]byte, error) {
	return appACLCurrentTransitionPrivilegeBodyFor(
		source,
		appACLCurrentTransitionDatabase,
		appACLCurrentTransitionBindings,
		appACLCurrentTransitionMigrator,
	)
}

func appACLCurrentTransitionPrivilegeBodyFor(
	source appACLCurrentSourceContract,
	databaseName string,
	bindings []AppACLRoleBinding,
	migratorRole string,
) ([]byte, error) {
	catalog, err := compileAppACLCurrentCatalogContract(source, databaseName, bindings, migratorRole)
	if err != nil {
		return nil, fmt.Errorf("compile transition privilege contract: %w", err)
	}
	body, err := CanonicalPrivilegeSetBodyV1(catalog.RoleBindings, catalog.Privileges)
	if err != nil {
		return nil, fmt.Errorf("encode transition privilege contract: %w", err)
	}
	return body, nil
}

func cloneAppACLCurrentCompiledMigrationFragment(source appACLCurrentCompiledMigrationFragment) appACLCurrentCompiledMigrationFragment {
	return appACLCurrentCompiledMigrationFragment{
		Migration:           source.Migration,
		Objects:             append([]AppACLManagedObjectR1(nil), source.Objects...),
		Privileges:          append([]AppACLPrivilege(nil), source.Privileges...),
		AuxiliaryPrivileges: append([]AppACLCurrentAuxiliaryPrivilege(nil), source.AuxiliaryPrivileges...),
		Functions:           cloneAppACLCurrentFunctionContracts(source.Functions),
	}
}

func indexOfAppACLCurrentMigration(names []string, target string) int {
	for index, name := range names {
		if name == target {
			return index
		}
	}
	return -1
}
func equalAppACLCurrentMigrationNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

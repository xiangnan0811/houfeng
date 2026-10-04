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

// Exported from the v0.79.6 release compiler, independently of current sources.
//
//go:embed testdata/app_acl_current_v0.79.6_migrations.v1.bin
var appACLCurrentV0796MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.79.6_privileges.v1.bin
var appACLCurrentV0796PrivilegeGolden []byte

// Exported from the v0.80.4 release compiler, independently of current sources.
// v0.80.3 and v0.80.4 embed the same migration and privilege contract.
//
//go:embed testdata/app_acl_current_v0.80.4_migrations.v1.bin
var appACLCurrentV0804MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.80.4_privileges.v1.bin
var appACLCurrentV0804PrivilegeGolden []byte

// Exported from the v1.15.3 baseline compiler, independently of current sources.
//
//go:embed testdata/app_acl_current_v1.15.3_migrations.v1.bin
var appACLCurrentV1153MigrationGolden []byte

//go:embed testdata/app_acl_current_v1.15.3_privileges.v1.bin
var appACLCurrentV1153PrivilegeGolden []byte

var appACLCurrentV0794ManifestDigestGolden = func() [32]byte {
	var digest [32]byte
	copy(digest[:], appACLCurrentV0794ManifestDigestGoldenBytes)
	return digest
}()

type appACLCurrentProfileID uint8

const (
	appACLCurrentProfileP62 appACLCurrentProfileID = iota + 1
	appACLCurrentProfileP64
	appACLCurrentProfileP63
	appACLCurrentProfileP66
	appACLCurrentProfileP67
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
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
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
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0802MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0802PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP63,
		predecessorLastMigration: "0063_tune_heartbeat_incident_policy.sql",
		successorMigrations: []string{
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0796MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0796PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP66,
		predecessorLastMigration: "0066_constrain_monitoring_and_target_state_values.sql",
		successorMigrations: []string{
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0804MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0804PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP67,
		predecessorLastMigration: "0067_refactor_vps_monitoring_lifecycle.sql",
		successorMigrations: []string{
			"0068_normalize_ip_quality_host_address_identity.sql",
		},
		predecessorMigrationGolden: appACLCurrentV1153MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV1153PrivilegeGolden,
	},
}

var appACLCurrentAcceptedPredecessorProfileChains = [][]appACLCurrentProfileID{
	{appACLCurrentProfileP62},
	{appACLCurrentProfileP64},
	{appACLCurrentProfileP62, appACLCurrentProfileP64},
	{appACLCurrentProfileP63},
	{appACLCurrentProfileP62, appACLCurrentProfileP63},
	{appACLCurrentProfileP66},
	{appACLCurrentProfileP62, appACLCurrentProfileP66},
	{appACLCurrentProfileP64, appACLCurrentProfileP66},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66},
	{appACLCurrentProfileP63, appACLCurrentProfileP66},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66},
	{appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP67},
	{appACLCurrentProfileP64, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP67},
	{appACLCurrentProfileP63, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP67},
	{appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67},
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
	if len(definitions) != 5 {
		return nil, fmt.Errorf("current APP ACL transition registry must contain exactly the P62, P64, P63, P66 and P67 profiles")
	}
	expectedDefinitions := appACLCurrentTransitionDefinitions
	compiled := make([]appACLCurrentTransition, 0, len(definitions))
	claimedProfiles := make(map[appACLCurrentProfileID]struct{}, len(definitions))
	for index, definition := range cloneAppACLCurrentTransitionDefinitions(definitions) {
		if definition.profile != appACLCurrentProfileP62 && definition.profile != appACLCurrentProfileP64 &&
			definition.profile != appACLCurrentProfileP63 && definition.profile != appACLCurrentProfileP66 &&
			definition.profile != appACLCurrentProfileP67 {
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
	// P62/P64/P63 predate 0065, so they share one privilege body and still
	// need its two runtime UPDATE grants. P66 already carries them.
	for _, transition := range compiled[1:3] {
		if !bytes.Equal(compiled[0].predecessorPrivilegeBody, transition.predecessorPrivilegeBody) {
			return nil, fmt.Errorf("registered P62, P64 and P63 privilege profiles differ")
		}
	}
	for _, transition := range compiled {
		if err := validateAppACLCurrentTransitionPrivilegeDelta(
			transition.predecessorPrivilegeBody,
			current,
			transition.profile,
		); err != nil {
			return nil, err
		}
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
	if definition.profile != appACLCurrentProfileP62 && len(definition.predecessorManifestDigestGolden) != 0 {
		return appACLCurrentTransition{}, fmt.Errorf("P63/P64/P66/P67 predecessor must support role-bound manifest identities")
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

func validateAppACLCurrentTransitionPrivilegeDelta(
	predecessorPrivilegeBody []byte,
	current appACLCurrentSourceContract,
	profile appACLCurrentProfileID,
) error {
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
	expectedAdditions := map[AppACLPrivilege]struct{}{}
	switch profile {
	case appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP63:
		for _, table := range []string{"asset_services", "asset_domains"} {
			expectedAdditions[AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      AppACLPrivilegeUpdate,
			}] = struct{}{}
		}
	case appACLCurrentProfileP66, appACLCurrentProfileP67:
	default:
		return fmt.Errorf("registered APP transition has unknown predecessor profile %d", profile)
	}
	if profile != appACLCurrentProfileP67 {
		for _, privilege := range vpsMonitoringLifecycleAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
			expectedAdditions[privilege] = struct{}{}
		}
	}
	for _, privilege := range ipQualityHostAddressIdentityAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
		expectedAdditions[privilege] = struct{}{}
	}
	if len(additions) != len(expectedAdditions) {
		return fmt.Errorf("registered APP transition privilege delta is not exactly the approved current fragments")
	}
	for privilege := range expectedAdditions {
		if _, present := additions[privilege]; !present {
			return fmt.Errorf("registered APP transition privilege delta is not exactly the approved current fragments")
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

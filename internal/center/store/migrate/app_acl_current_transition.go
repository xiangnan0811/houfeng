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

var appACLCurrentV0794ManifestDigestGolden = func() [32]byte {
	var digest [32]byte
	copy(digest[:], appACLCurrentV0794ManifestDigestGoldenBytes)
	return digest
}()

type appACLCurrentTransitionDefinition struct {
	predecessorLastMigration        string
	successorMigrations             []string
	privilegesUnchanged             bool
	predecessorMigrationGolden      []byte
	predecessorPrivilegeGolden      []byte
	predecessorManifestDigestGolden []byte
}

type appACLCurrentTransition struct {
	predecessor               appACLCurrentSourceContract
	successor                 migrationSourceSnapshot
	predecessorPrivilegeBody  []byte
	predecessorManifestDigest [32]byte
}

var appACLCurrentTransitionDefinitions = []appACLCurrentTransitionDefinition{{
	predecessorLastMigration:        "0062_create_vps_create_idempotency.sql",
	successorMigrations:             []string{"0063_tune_heartbeat_incident_policy.sql"},
	privilegesUnchanged:             true,
	predecessorMigrationGolden:      appACLCurrentV0794MigrationGolden,
	predecessorPrivilegeGolden:      appACLCurrentV0794PrivilegeGolden,
	predecessorManifestDigestGolden: appACLCurrentV0794ManifestDigestGoldenBytes,
}}

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
	compiled := make([]appACLCurrentTransition, 0, len(definitions))
	claimedPredecessors := make(map[string]struct{}, len(definitions))
	claimedSuccessors := make(map[string]struct{})
	for index, definition := range cloneAppACLCurrentTransitionDefinitions(definitions) {
		predecessorIndex := -1
		for sourceIndex, name := range current.sources.names {
			if name == definition.predecessorLastMigration {
				predecessorIndex = sourceIndex
				break
			}
		}
		if predecessorIndex < 0 {
			return nil, fmt.Errorf("current APP ACL transition %d has unknown predecessor %q", index, definition.predecessorLastMigration)
		}
		if _, overlap := claimedPredecessors[definition.predecessorLastMigration]; overlap {
			return nil, fmt.Errorf("current APP ACL transition %d overlaps predecessor %q", index, definition.predecessorLastMigration)
		}
		if len(definition.successorMigrations) == 0 {
			return nil, fmt.Errorf("current APP ACL transition %d has no successor migrations", index)
		}
		seen := make(map[string]struct{}, len(definition.successorMigrations))
		for successorIndex, name := range definition.successorMigrations {
			if _, duplicate := seen[name]; duplicate {
				return nil, fmt.Errorf("current APP ACL transition %d has duplicate successor migration %q", index, name)
			}
			seen[name] = struct{}{}
			currentIndex := indexOfAppACLCurrentMigration(current.sources.names, name)
			if currentIndex < 0 {
				return nil, fmt.Errorf("current APP ACL transition %d has unknown successor migration %q", index, name)
			}
			if currentIndex != predecessorIndex+1+successorIndex {
				return nil, fmt.Errorf("current APP ACL transition %d successor migrations are out of order", index)
			}
			if _, overlap := claimedSuccessors[name]; overlap {
				return nil, fmt.Errorf("current APP ACL transition %d overlaps successor migration %q", index, name)
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
		claimedPredecessors[definition.predecessorLastMigration] = struct{}{}
		for _, name := range definition.successorMigrations {
			claimedSuccessors[name] = struct{}{}
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
	if definition.privilegesUnchanged {
		currentPrivileges, err := appACLCurrentTransitionPrivilegeBody(current)
		if err != nil {
			return appACLCurrentTransition{}, err
		}
		if !bytes.Equal(currentPrivileges, predecessorPrivileges) {
			return appACLCurrentTransition{}, fmt.Errorf("transition marks changed privileges as unchanged")
		}
	}
	if len(definition.predecessorManifestDigestGolden) != 32 {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor manifest digest golden has invalid length")
	}
	manifest, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, predecessor.sources.canonicalSet, predecessorPrivileges)
	if err != nil {
		return appACLCurrentTransition{}, fmt.Errorf("build predecessor manifest: %w", err)
	}
	if !bytes.Equal(manifest.ManifestDigest[:], definition.predecessorManifestDigestGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor manifest digest differs from released golden")
	}
	return appACLCurrentTransition{
		predecessor:               predecessor,
		successor:                 successor,
		predecessorPrivilegeBody:  append([]byte(nil), predecessorPrivileges...),
		predecessorManifestDigest: manifest.ManifestDigest,
	}, nil
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
	catalog, err := compileAppACLCurrentCatalogContract(source, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindings, appACLCurrentTransitionMigrator)
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

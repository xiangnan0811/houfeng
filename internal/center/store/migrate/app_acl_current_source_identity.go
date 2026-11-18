package migrate

import (
	"fmt"
	"strconv"
	"strings"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

// AppACLCurrentSourceIdentity describes the embedded current migration source
// and the canonical privilege body for the supplied database and role names.
// Its byte slices are owned by the caller.
type AppACLCurrentSourceIdentity struct {
	SchemaVersion         uint64
	CanonicalMigrationSet []byte
	CanonicalPrivilegeSet []byte
}

// CompileAppACLCurrentSourceIdentity derives the current source identity
// without connecting to PostgreSQL or accepting persisted privileges as input.
func CompileAppACLCurrentSourceIdentity(databaseName string, roles platformmigrate.AppRoleSetV2) (AppACLCurrentSourceIdentity, error) {
	if err := roles.Validate(); err != nil {
		return AppACLCurrentSourceIdentity{}, fmt.Errorf("validate current APP source roles: %w", err)
	}
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		return AppACLCurrentSourceIdentity{}, fmt.Errorf("compile current APP source: %w", err)
	}
	if _, err := compileAppACLCurrentTransitions(source, appACLCurrentTransitionDefinitions); err != nil {
		return AppACLCurrentSourceIdentity{}, fmt.Errorf("compile current APP source profiles: %w", err)
	}
	var schemaVersion uint64
	for _, name := range source.sources.names {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok || prefix == "" {
			return AppACLCurrentSourceIdentity{}, fmt.Errorf("current APP migration %q has no numeric version", name)
		}
		for _, digit := range prefix {
			if digit < '0' || digit > '9' {
				return AppACLCurrentSourceIdentity{}, fmt.Errorf("current APP migration %q has invalid numeric version", name)
			}
		}
		version, err := strconv.ParseUint(prefix, 10, 64)
		if err != nil || version == 0 {
			return AppACLCurrentSourceIdentity{}, fmt.Errorf("current APP migration %q has invalid numeric version", name)
		}
		if version > schemaVersion {
			schemaVersion = version
		}
	}
	if schemaVersion == 0 {
		return AppACLCurrentSourceIdentity{}, fmt.Errorf("current APP migration set is empty")
	}
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: roles.CenterRuntime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: roles.PlatformAdmin},
		{Subject: AppACLSubjectRecordDeletionWorker, CatalogRole: roles.DeletionWorker},
	}
	contract, err := compileAppACLCurrentCatalogContract(source, databaseName, bindings, roles.Migrator)
	if err != nil {
		return AppACLCurrentSourceIdentity{}, fmt.Errorf("compile current APP catalog contract: %w", err)
	}
	privileges, err := canonicalPrivilegeSetBodyForBindings(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return AppACLCurrentSourceIdentity{}, fmt.Errorf("encode current APP privilege set: %w", err)
	}
	return AppACLCurrentSourceIdentity{
		SchemaVersion: schemaVersion, CanonicalMigrationSet: source.sources.canonicalSet, CanonicalPrivilegeSet: privileges,
	}, nil
}

package migrate

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
)

// AppACLManifestRuntimeSnapshotV1 is the complete read-only state needed to
// verify the persisted ACL manifest against the applied and embedded maps.
// A nil Head represents the nullable fresh-install head row.
type AppACLManifestRuntimeSnapshotV1 struct {
	DatabaseName      string
	SessionUser       string
	CurrentUser       string
	Manifests         []AppACLManifestPersistedV1
	Head              *AppACLManifestHeadV1
	AppliedMigrations []MigrationChecksumEntry
}

// AppACLManifestRuntimeReader loads the persisted application migration ledger
// and ACL manifest state without applying migrations or changing catalog ACLs.
type AppACLManifestRuntimeReader interface {
	ReadAppACLManifestRuntimeSnapshotV1(context.Context) (AppACLManifestRuntimeSnapshotV1, error)
}

// VerifyPersistedAppACLManifestRuntimeV1 rejects a nullable or drifting
// manifest head and requires the latest manifest migration and privilege sets
// to match the applied ledger, embedded migration map, and frozen r1 compiler
// output for the observed database and role bindings.
func VerifyPersistedAppACLManifestRuntimeV1(
	ctx context.Context,
	reader AppACLManifestRuntimeReader,
	embeddedMigrations fs.FS,
) (AppACLManifestPersistedV1, error) {
	if reader == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest runtime reader is nil")
	}
	if embeddedMigrations == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("embedded migration filesystem is nil")
	}
	snapshot, err := reader.ReadAppACLManifestRuntimeSnapshotV1(ctx)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read persisted app ACL manifest snapshot: %w", err)
	}
	return verifyAppACLManifestRuntimeSnapshotV1(snapshot, embeddedMigrations)
}

// verifyAppACLManifestRuntimeSnapshotV1 verifies a manifest/ledger snapshot
// already read by a caller-owned transaction. Runtime admission combines this
// with the scoped catalog reader in one PostgreSQL snapshot.
func verifyAppACLManifestRuntimeSnapshotV1(
	snapshot AppACLManifestRuntimeSnapshotV1,
	embeddedMigrations fs.FS,
) (AppACLManifestPersistedV1, error) {
	if embeddedMigrations == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("embedded migration filesystem is nil")
	}
	envelope, err := validateAppACLManifestRuntimeEnvelope(snapshot)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	compiledPrivilegeSet, err := CompileAppACLPrivilegeSetR1(snapshot.DatabaseName, envelope.PrivilegeSet.RoleBindings)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile frozen r1 app ACL privilege set: %w", err)
	}
	if !bytes.Equal(envelope.Latest.CanonicalPrivilegeSet, compiledPrivilegeSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest privilege set does not match frozen r1 compiler output")
	}
	if err := validateAppACLManifestRuntimeRoles(snapshot, envelope); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	embeddedMigrationSet, err := CanonicalMigrationSetFromFS(embeddedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build embedded application migration set: %w", err)
	}
	if !bytes.Equal(envelope.Latest.CanonicalMigrationSet, embeddedMigrationSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest migration set does not match embedded migrations")
	}

	appliedMigrationSet, err := CanonicalMigrationSetBodyV1(snapshot.AppliedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("encode applied application migration ledger: %w", err)
	}
	if !bytes.Equal(envelope.Latest.CanonicalMigrationSet, appliedMigrationSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest migration set does not match applied migration ledger")
	}
	return envelope.Latest, nil
}

type appACLManifestRuntimeEnvelope struct {
	Latest       AppACLManifestPersistedV1
	PrivilegeSet AppACLPrivilegeSet
}

func validateAppACLManifestRuntimeEnvelope(
	snapshot AppACLManifestRuntimeSnapshotV1,
) (appACLManifestRuntimeEnvelope, error) {
	if snapshot.SessionUser != snapshot.CurrentUser {
		return appACLManifestRuntimeEnvelope{}, fmt.Errorf("session user %q does not match current user %q", snapshot.SessionUser, snapshot.CurrentUser)
	}
	if snapshot.Head == nil {
		return appACLManifestRuntimeEnvelope{}, fmt.Errorf("app ACL manifest head is null")
	}
	if err := ValidateAppACLManifestChainV1(snapshot.Manifests, *snapshot.Head); err != nil {
		return appACLManifestRuntimeEnvelope{}, fmt.Errorf("validate persisted app ACL manifest chain: %w", err)
	}

	latest := snapshot.Manifests[len(snapshot.Manifests)-1]
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(latest.CanonicalPrivilegeSet)
	if err != nil {
		return appACLManifestRuntimeEnvelope{}, fmt.Errorf("parse latest app ACL manifest privilege set: %w", err)
	}
	return appACLManifestRuntimeEnvelope{Latest: latest, PrivilegeSet: privilegeSet}, nil
}

func validateAppACLManifestRuntimeRoles(
	snapshot AppACLManifestRuntimeSnapshotV1,
	envelope appACLManifestRuntimeEnvelope,
) error {
	var runtimeRole, adminRole string
	for _, binding := range envelope.PrivilegeSet.RoleBindings {
		switch binding.Subject {
		case AppACLSubjectCenterRuntime:
			runtimeRole = binding.CatalogRole
		case AppACLSubjectPlatformAdmin:
			adminRole = binding.CatalogRole
		}
	}
	if envelope.Latest.MigratorCatalogRole == runtimeRole || envelope.Latest.MigratorCatalogRole == adminRole {
		return fmt.Errorf("latest app ACL manifest migrator catalog role reuses an application role")
	}
	if snapshot.CurrentUser != runtimeRole {
		return fmt.Errorf("current user %q does not match latest app ACL manifest center runtime binding", snapshot.CurrentUser)
	}
	return nil
}

func verifyAppACLCurrentManifestRuntimeSnapshot(
	snapshot AppACLManifestRuntimeSnapshotV1,
	source appACLCurrentSourceContract,
) (AppACLManifestPersistedV1, appACLEffectiveCatalogContract, error) {
	if snapshot.Head == nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, appACLDevelopmentDatabaseRebuildError(
			"APP manifest head is null",
		)
	}
	envelope, err := validateAppACLManifestRuntimeEnvelope(snapshot)
	if err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, err
	}
	if len(snapshot.Manifests) != 1 || snapshot.Head.ManifestRevision != 1 {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, appACLDevelopmentDatabaseRebuildError(
			"APP manifest chain is not the current genesis revision",
		)
	}
	if err := validateAppACLManifestRuntimeRoles(snapshot, envelope); err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, err
	}

	manifestMigrations, err := ParseCanonicalMigrationSetBodyV1(envelope.Latest.CanonicalMigrationSet)
	if err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, fmt.Errorf("parse current app ACL manifest migration set: %w", err)
	}
	if err := compareAppACLCurrentMigrationEntries(source.sources.canonicalSet, manifestMigrations, "persisted manifest"); err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, err
	}
	if err := compareAppACLCurrentMigrationEntries(source.sources.canonicalSet, snapshot.AppliedMigrations, "applied migration ledger"); err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, err
	}

	contract, err := compileAppACLCurrentCatalogContract(
		source,
		snapshot.DatabaseName,
		envelope.PrivilegeSet.RoleBindings,
		envelope.Latest.MigratorCatalogRole,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, fmt.Errorf("compile current app ACL runtime catalog contract: %w", err)
	}
	compiledPrivileges, err := CanonicalPrivilegeSetBodyV1(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, fmt.Errorf("compile current app ACL runtime privilege set: %w", err)
	}
	if !bytes.Equal(envelope.Latest.CanonicalPrivilegeSet, compiledPrivileges) {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, fmt.Errorf("latest app ACL manifest privilege set does not match current compiler output")
	}
	return envelope.Latest, contract, nil
}

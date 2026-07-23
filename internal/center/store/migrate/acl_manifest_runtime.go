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
	if snapshot.SessionUser != snapshot.CurrentUser {
		return AppACLManifestPersistedV1{}, fmt.Errorf("session user %q does not match current user %q", snapshot.SessionUser, snapshot.CurrentUser)
	}
	if snapshot.Head == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest head is null")
	}
	if err := ValidateAppACLManifestChainV1(snapshot.Manifests, *snapshot.Head); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("validate persisted app ACL manifest chain: %w", err)
	}

	latest := snapshot.Manifests[len(snapshot.Manifests)-1]
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(latest.CanonicalPrivilegeSet)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("parse latest app ACL manifest privilege set: %w", err)
	}
	compiledPrivilegeSet, err := CompileAppACLPrivilegeSetR1(snapshot.DatabaseName, privilegeSet.RoleBindings)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile frozen r1 app ACL privilege set: %w", err)
	}
	if !bytes.Equal(latest.CanonicalPrivilegeSet, compiledPrivilegeSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest privilege set does not match frozen r1 compiler output")
	}
	var runtimeRole, adminRole string
	for _, binding := range privilegeSet.RoleBindings {
		switch binding.Subject {
		case AppACLSubjectCenterRuntime:
			runtimeRole = binding.CatalogRole
		case AppACLSubjectPlatformAdmin:
			adminRole = binding.CatalogRole
		}
	}
	if latest.MigratorCatalogRole == runtimeRole || latest.MigratorCatalogRole == adminRole {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest migrator catalog role reuses an application role")
	}
	if snapshot.CurrentUser != runtimeRole {
		return AppACLManifestPersistedV1{}, fmt.Errorf("current user %q does not match latest app ACL manifest center runtime binding", snapshot.CurrentUser)
	}
	embeddedMigrationSet, err := CanonicalMigrationSetFromFS(embeddedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build embedded application migration set: %w", err)
	}
	if !bytes.Equal(latest.CanonicalMigrationSet, embeddedMigrationSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest migration set does not match embedded migrations")
	}

	appliedMigrationSet, err := CanonicalMigrationSetBodyV1(snapshot.AppliedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("encode applied application migration ledger: %w", err)
	}
	if !bytes.Equal(latest.CanonicalMigrationSet, appliedMigrationSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest migration set does not match applied migration ledger")
	}
	return latest, nil
}

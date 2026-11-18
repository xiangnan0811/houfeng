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
	embeddedMigrationSet, err := CanonicalMigrationSetFromFS(embeddedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build embedded application migration set: %w", err)
	}
	return verifyAppACLManifestRuntimeSnapshotWithMigrationSetV1(snapshot, embeddedMigrationSet)
}

func verifyAppACLManifestRuntimeSnapshotWithMigrationSetV1(
	snapshot AppACLManifestRuntimeSnapshotV1,
	embeddedMigrationSet []byte,
) (AppACLManifestPersistedV1, error) {
	if len(embeddedMigrationSet) == 0 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("embedded migration set is empty")
	}
	envelope, err := validateAppACLManifestRuntimeEnvelope(snapshot)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if envelope.Version != AppACLPrivilegeSetV1 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest privilege set is not the frozen r1 v1 body")
	}
	compiledPrivilegeSet, err := CompileAppACLPrivilegeSetR1(snapshot.DatabaseName, envelope.PrivilegeSet.RoleBindings)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile frozen r1 app ACL privilege set: %w", err)
	}
	if !bytes.Equal(envelope.Latest.CanonicalPrivilegeSet, compiledPrivilegeSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("latest app ACL manifest privilege set does not match frozen r1 compiler output")
	}
	if err := validateAppACLManifestRuntimeRoles(snapshot, envelope, AppACLSubjectCenterRuntime); err != nil {
		return AppACLManifestPersistedV1{}, err
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
	Version      AppACLPrivilegeSetVersion
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
	privilegeSet, version, err := parseCanonicalPrivilegeSetBodyAnyVersion(latest.CanonicalPrivilegeSet)
	if err != nil {
		return appACLManifestRuntimeEnvelope{}, fmt.Errorf("parse latest app ACL manifest privilege set: %w", err)
	}
	return appACLManifestRuntimeEnvelope{Latest: latest, PrivilegeSet: privilegeSet, Version: version}, nil
}

// validateAppACLManifestRuntimeRoles binds the direct database identity to
// exactly one expected semantic subject of the latest manifest. A runtime
// login is never admitted as the deletion worker, nor the reverse.
func validateAppACLManifestRuntimeRoles(
	snapshot AppACLManifestRuntimeSnapshotV1,
	envelope appACLManifestRuntimeEnvelope,
	expectedSubject AppACLSubject,
) error {
	expectedRole := ""
	for _, binding := range envelope.PrivilegeSet.RoleBindings {
		if envelope.Latest.MigratorCatalogRole == binding.CatalogRole {
			return fmt.Errorf("latest app ACL manifest migrator catalog role reuses an application role")
		}
		if binding.Subject == expectedSubject {
			expectedRole = binding.CatalogRole
		}
	}
	if expectedRole == "" {
		return fmt.Errorf("latest app ACL manifest has no %s binding", appACLSubjectLabel(expectedSubject))
	}
	if snapshot.CurrentUser != expectedRole {
		return fmt.Errorf("current user %q does not match latest app ACL manifest %s binding", snapshot.CurrentUser, appACLSubjectLabel(expectedSubject))
	}
	return nil
}

func appACLSubjectLabel(subject AppACLSubject) string {
	switch subject {
	case AppACLSubjectCenterRuntime:
		return "center runtime"
	case AppACLSubjectPlatformAdmin:
		return "platform admin"
	case AppACLSubjectRecordDeletionWorker:
		return "record deletion worker"
	default:
		return string(subject)
	}
}

func verifyAppACLCurrentManifestRuntimeSnapshot(
	snapshot AppACLManifestRuntimeSnapshotV1,
	source appACLCurrentSourceContract,
	transitions []appACLCurrentTransition,
	expectedSubject AppACLSubject,
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
	if appACLCurrentSourceRequiresDeletionWorker(source) && envelope.Version != AppACLPrivilegeSetV2 {
		// A v1 latest manifest can only be a registered predecessor of a
		// source that introduces the mandatory worker binding.
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, appACLDevelopmentDatabaseRebuildError(
			"registered APP predecessor requires successor convergence before runtime admission",
		)
	}
	if err := validateAppACLManifestRuntimeRoles(snapshot, envelope, expectedSubject); err != nil {
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
	compiledPrivileges, err := canonicalPrivilegeSetBodyForBindings(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, fmt.Errorf("compile current app ACL runtime privilege set: %w", err)
	}
	shape, err := classifyAppACLCurrentManifestShape(
		source,
		transitions,
		snapshot.AppliedMigrations,
		snapshot.Manifests,
		snapshot.Head,
		compiledPrivileges,
		envelope.Latest.MigratorCatalogRole,
		snapshot.DatabaseName,
		envelope.PrivilegeSet.RoleBindings,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, err
	}
	if shape.kind == appACLCurrentManifestShapePredecessor {
		return AppACLManifestPersistedV1{}, appACLEffectiveCatalogContract{}, appACLDevelopmentDatabaseRebuildError(
			"registered APP predecessor requires successor convergence before runtime admission",
		)
	}
	return shape.latest, contract, nil
}

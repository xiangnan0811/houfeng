package migrate

import (
	"bytes"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	rootmigrations "houfeng/db/migrations"
)

// FrozenAppACLR1StateV1 is the credential-neutral R1 state needed by the R2
// transition. Every field is proven in the caller's transaction.
type FrozenAppACLR1StateV1 struct {
	DatabaseName       string
	ManifestRevision   uint64
	ManifestDigest     [32]byte
	SourceSetBody      []byte
	SourceSetDigest    [32]byte
	PrivilegeSetBody   []byte
	PrivilegeSetDigest [32]byte
	CenterRuntimeRole  string
	PlatformAdminRole  string
	DirectMigratorRole string
}

type frozenAppACLR1EvidenceV1 struct {
	DatabaseName      string
	Manifests         []AppACLManifestPersistedV1
	Head              *AppACLManifestHeadV1
	AppliedMigrations []MigrationChecksumEntry
}

type frozenAppACLR1VerifyDependencies struct {
	loadSources   func() (migrationSourceSnapshot, error)
	readEvidence  func(context.Context, pgx.Tx) (frozenAppACLR1EvidenceV1, error)
	readCatalog   func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error)
	verifyCatalog func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error
}

// VerifyFrozenAppACLR1StateInTx verifies exact R1 evidence without beginning,
// committing, or changing the caller-owned transaction.
func VerifyFrozenAppACLR1StateInTx(ctx context.Context, tx pgx.Tx) (FrozenAppACLR1StateV1, error) {
	return verifyFrozenAppACLR1StateInTxWithDependencies(ctx, tx, frozenAppACLR1VerifyDependencies{
		loadSources: func() (migrationSourceSnapshot, error) {
			return snapshotMigrationSources(rootmigrations.FS)
		},
		readEvidence:  readFrozenAppACLR1EvidenceInTx,
		readCatalog:   readFrozenAppACLR1CatalogInTx,
		verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
	})
}

func verifyFrozenAppACLR1StateInTxWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	dependencies frozenAppACLR1VerifyDependencies,
) (FrozenAppACLR1StateV1, error) {
	if tx == nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("frozen APP ACL R1 verifier has no transaction")
	}
	if dependencies.loadSources == nil || dependencies.readEvidence == nil || dependencies.readCatalog == nil || dependencies.verifyCatalog == nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("frozen APP ACL R1 verifier dependencies are incomplete")
	}

	sources, err := dependencies.loadSources()
	if err != nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("load frozen R1 application sources: %w", err)
	}
	if err := validateAppACLR1FrozenSourceSnapshot(sources); err != nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("verify frozen R1 application sources: %w", err)
	}

	evidence, err := dependencies.readEvidence(ctx, tx)
	if err != nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("read frozen R1 evidence: %w", err)
	}
	state, catalogInput, err := verifyFrozenAppACLR1Evidence(evidence, sources)
	if err != nil {
		return FrozenAppACLR1StateV1{}, err
	}
	catalog, err := dependencies.readCatalog(ctx, tx, catalogInput)
	if err != nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("read frozen R1 catalog: %w", err)
	}
	if err := dependencies.verifyCatalog(catalog, catalogInput); err != nil {
		return FrozenAppACLR1StateV1{}, fmt.Errorf("verify frozen R1 catalog: %w", err)
	}
	return state, nil
}

func verifyFrozenAppACLR1Evidence(
	evidence frozenAppACLR1EvidenceV1,
	sources migrationSourceSnapshot,
) (FrozenAppACLR1StateV1, AppACLEffectiveCatalogVerifierInputR1, error) {
	if !validCatalogRoleName(evidence.DatabaseName) {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 evidence has invalid database name")
	}
	if len(evidence.Manifests) != 1 {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 evidence has %d manifest revisions, want exactly one revision", len(evidence.Manifests))
	}
	if evidence.Head == nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 manifest chain has no head")
	}
	if err := ValidateAppACLManifestChainV1(evidence.Manifests, *evidence.Head); err != nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("validate frozen R1 manifest chain: %w", err)
	}
	manifest := evidence.Manifests[0]
	if manifest.ManifestRevision != 1 {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 manifest revision is %d, want 1", manifest.ManifestRevision)
	}
	if !bytes.Equal(manifest.CanonicalMigrationSet, sources.canonicalSet) {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 manifest source body does not match embedded sources")
	}
	if len(evidence.AppliedMigrations) != len(appACLR1MigrationSourceContract) {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 ledger has %d entries, want 52", len(evidence.AppliedMigrations))
	}
	for index := range evidence.AppliedMigrations {
		if evidence.AppliedMigrations[index] != appACLR1MigrationSourceContract[index] {
			return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 ledger entry %d does not match the source contract", index)
		}
	}
	appliedBody, err := CanonicalMigrationSetBodyV1(evidence.AppliedMigrations)
	if err != nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("encode frozen R1 ledger: %w", err)
	}
	if !bytes.Equal(appliedBody, manifest.CanonicalMigrationSet) {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 ledger does not match manifest sources")
	}

	privileges, err := ParseCanonicalPrivilegeSetBodyV1(manifest.CanonicalPrivilegeSet)
	if err != nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("parse frozen R1 manifest privilege body: %w", err)
	}
	if len(privileges.Privileges) != appACLEffectiveCatalogR1PrivilegeCount {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 manifest privilege body has %d tuples, want 204", len(privileges.Privileges))
	}
	compiledPrivilegeBody, err := CompileAppACLPrivilegeSetR1(evidence.DatabaseName, privileges.RoleBindings)
	if err != nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("compile frozen R1 manifest privilege body: %w", err)
	}
	if !bytes.Equal(compiledPrivilegeBody, manifest.CanonicalPrivilegeSet) {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 manifest privilege body does not match the compiler")
	}

	var centerRuntimeRole, platformAdminRole string
	for _, binding := range privileges.RoleBindings {
		switch binding.Subject {
		case AppACLSubjectCenterRuntime:
			centerRuntimeRole = binding.CatalogRole
		case AppACLSubjectPlatformAdmin:
			platformAdminRole = binding.CatalogRole
		}
	}
	if !validCatalogRoleName(centerRuntimeRole) || !validCatalogRoleName(platformAdminRole) || !validCatalogRoleName(manifest.MigratorCatalogRole) ||
		centerRuntimeRole == platformAdminRole || centerRuntimeRole == manifest.MigratorCatalogRole || platformAdminRole == manifest.MigratorCatalogRole {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("frozen R1 manifest role bindings are invalid")
	}
	contract, err := CompileAppACLEffectiveCatalogContractR1(evidence.DatabaseName, privileges.RoleBindings)
	if err != nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("compile frozen R1 catalog contract: %w", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, manifest.MigratorCatalogRole)
	if err != nil {
		return FrozenAppACLR1StateV1{}, AppACLEffectiveCatalogVerifierInputR1{}, fmt.Errorf("build frozen R1 catalog verifier input: %w", err)
	}

	return FrozenAppACLR1StateV1{
		DatabaseName:       evidence.DatabaseName,
		ManifestRevision:   manifest.ManifestRevision,
		ManifestDigest:     manifest.ManifestDigest,
		SourceSetBody:      append([]byte(nil), manifest.CanonicalMigrationSet...),
		SourceSetDigest:    manifest.MigrationSetDigest,
		PrivilegeSetBody:   append([]byte(nil), manifest.CanonicalPrivilegeSet...),
		PrivilegeSetDigest: manifest.PrivilegeSetDigest,
		CenterRuntimeRole:  centerRuntimeRole,
		PlatformAdminRole:  platformAdminRole,
		DirectMigratorRole: manifest.MigratorCatalogRole,
	}, input, nil
}

func readFrozenAppACLR1EvidenceInTx(ctx context.Context, tx pgx.Tx) (frozenAppACLR1EvidenceV1, error) {
	if tx == nil {
		return frozenAppACLR1EvidenceV1{}, fmt.Errorf("frozen APP ACL R1 evidence reader has no transaction")
	}
	var evidence frozenAppACLR1EvidenceV1
	if err := tx.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&evidence.DatabaseName); err != nil {
		return frozenAppACLR1EvidenceV1{}, fmt.Errorf("read frozen R1 database name: %w", err)
	}
	var err error
	if evidence.Manifests, err = readAppACLManifestRevisionsV1(ctx, tx); err != nil {
		return frozenAppACLR1EvidenceV1{}, err
	}
	if evidence.Head, err = readAppACLManifestHeadV1(ctx, tx); err != nil {
		return frozenAppACLR1EvidenceV1{}, err
	}
	if evidence.AppliedMigrations, err = readAppliedAppMigrationsV1(ctx, tx); err != nil {
		return frozenAppACLR1EvidenceV1{}, err
	}
	return evidence, nil
}

func readFrozenAppACLR1CatalogInTx(
	ctx context.Context,
	tx pgx.Tx,
	input AppACLEffectiveCatalogVerifierInputR1,
) (snapshot AppACLEffectiveCatalogSnapshotR1, err error) {
	if tx == nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("frozen APP ACL R1 catalog reader has no transaction")
	}
	if err := input.Validate(); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("validate frozen R1 catalog input: %w", err)
	}
	if err := tx.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&snapshot.DatabaseName); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("read frozen R1 catalog database name: %w", err)
	}
	scope, err := newAppACLManagedSurfaceScopeR1(snapshot.DatabaseName)
	if err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	delegatedFunctions, err := frozenAppACLR2DelegatedFunctionIdentities()
	if err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.PGCryptoExtension, err = readAppACLEffectiveCatalogPGCryptoExtensionR1(ctx, tx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := verifyAppACLEffectiveCatalogPGCryptoExtensionR1(snapshot.PGCryptoExtension); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("verify frozen R1 pgcrypto placement: %w", err)
	}
	roleNames := []string{input.Contract.RoleBindings[0].CatalogRole, input.Contract.RoleBindings[1].CatalogRole, input.MigratorRole}
	if snapshot.Roles, err = readAppACLEffectiveCatalogRolesR1(ctx, tx, snapshot.DatabaseName, roleNames); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Memberships, err = readAppACLEffectiveCatalogMembershipsR1(ctx, tx, roleNames); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Owners, err = readAppACLEffectiveCatalogOwnersR1(ctx, tx, snapshot.DatabaseName, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.DirectPrivileges, err = readAppACLEffectiveCatalogDirectPrivilegesR1(ctx, tx, snapshot.DatabaseName, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.EffectivePrivileges, err = readAppACLEffectiveCatalogEffectivePrivilegesR1(ctx, tx, snapshot.DatabaseName, roleNames[:2], scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.ColumnACLs, err = readAppACLEffectiveCatalogColumnACLsR1(ctx, tx, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.DefaultACLs, err = readAppACLEffectiveCatalogDefaultACLsR1(ctx, tx, input.MigratorRole); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Functions, err = readAppACLEffectiveCatalogFunctionsR1(ctx, tx, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := verifyAppACLPublicProjectorStructureR1(ctx, tx, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := verifyAppACLOpaqueExtensionMemberReachabilityR1(ctx, tx, roleNames[:2]); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	snapshot = scopeAppACLEffectiveCatalogSnapshotR1(snapshot, scope)
	// R2 catalog predicates prove these exact helpers; frozen R1 retains every other internal owner.
	return delegateFrozenAppACLR2FunctionOwners(snapshot, delegatedFunctions), nil
}

func frozenAppACLR2DelegatedFunctionIdentities() (map[string]struct{}, error) {
	identities := make(map[string]struct{})
	for _, object := range appACLR2KnownReservedObjects() {
		if object.Kind != "function" {
			continue
		}
		schemaName, _, found := appACLFunctionIdentityFromQualifiedIdentityR1(object.Identity)
		if !found || object.Detail != "f" || object.Schema != appACLManagedInternalSchemaR1 || schemaName != object.Schema {
			return nil, fmt.Errorf("invalid reserved APP ACL R2 function %q", object.Identity)
		}
		if _, duplicate := identities[object.Identity]; duplicate {
			return nil, fmt.Errorf("duplicate reserved APP ACL R2 function %q", object.Identity)
		}
		identities[object.Identity] = struct{}{}
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("APP ACL R2 reserved catalog has no functions")
	}
	return identities, nil
}

func delegateFrozenAppACLR2FunctionOwners(
	snapshot AppACLEffectiveCatalogSnapshotR1,
	identities map[string]struct{},
) AppACLEffectiveCatalogSnapshotR1 {
	owners := make([]AppACLEffectiveCatalogObjectOwnerR1, 0, len(snapshot.Owners))
	for _, owner := range snapshot.Owners {
		identity := owner.SchemaName + "." + owner.ObjectIdentity
		_, delegated := identities[identity]
		if owner.ObjectClass == AppACLObjectClassFunction && delegated {
			continue
		}
		owners = append(owners, owner)
	}
	snapshot.Owners = owners
	return snapshot
}

// RequireDirectFrozenAppACLR1RuntimeInTx is the separate R1 actor predicate.
// It is intentionally not part of credential-neutral state verification.
func RequireDirectFrozenAppACLR1RuntimeInTx(ctx context.Context, tx pgx.Tx, state FrozenAppACLR1StateV1) error {
	if tx == nil {
		return fmt.Errorf("frozen APP ACL R1 runtime predicate has no transaction")
	}
	if !validCatalogRoleName(state.CenterRuntimeRole) {
		return fmt.Errorf("frozen APP ACL R1 runtime role is invalid")
	}
	var sessionUser, currentUser string
	if err := tx.QueryRow(ctx, `select session_user::text, current_user::text`).Scan(&sessionUser, &currentUser); err != nil {
		return fmt.Errorf("read frozen APP ACL R1 runtime identity: %w", err)
	}
	if sessionUser != state.CenterRuntimeRole || currentUser != state.CenterRuntimeRole {
		return fmt.Errorf("frozen APP ACL R1 runtime requires direct role %q, got %q/%q", state.CenterRuntimeRole, sessionUser, currentUser)
	}
	return nil
}

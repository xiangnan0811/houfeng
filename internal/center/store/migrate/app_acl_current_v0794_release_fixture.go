//go:build appacl_release_fixture

package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentV0794FragmentDigestGolden = "6a002ce19e63ab9dc7353618227022e5e4259679d31a638acc25db858f8c769c"

// ConvergeAppACLCurrentV0794ReleaseFixture materializes the independently
// frozen v0.79.4 predecessor for strict integration tests. The build tag keeps
// this release-only fixture out of production binaries.
func ConvergeAppACLCurrentV0794ReleaseFixture(
	ctx context.Context,
	db *pgxpool.Pool,
	runtimeRole string,
	adminRole string,
) (AppACLManifestPersistedV1, error) {
	if db == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("v0.79.4 release fixture has no PostgreSQL pool")
	}
	releaseEntries, err := ParseCanonicalMigrationSetBodyV1(appACLCurrentV0794MigrationGolden)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("parse frozen v0.79.4 migration golden: %w", err)
	}
	if len(releaseEntries) != 63 || releaseEntries[len(releaseEntries)-1].Filename != "0062_create_vps_create_idempotency.sql" {
		return AppACLManifestPersistedV1{}, fmt.Errorf("frozen v0.79.4 release fixture migration list is invalid")
	}
	releaseFS := make(fstest.MapFS, len(releaseEntries))
	for _, entry := range releaseEntries {
		body, readErr := fs.ReadFile(migrations.FS, entry.Filename)
		if readErr != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("read frozen v0.79.4 migration %q: %w", entry.Filename, readErr)
		}
		if sha256.Sum256(body) != entry.Checksum {
			return AppACLManifestPersistedV1{}, fmt.Errorf("v0.79.4 migration %q differs from frozen release checksum", entry.Filename)
		}
		releaseFS[entry.Filename] = &fstest.MapFile{Data: append([]byte(nil), body...)}
	}
	releaseFragments := appACLCurrentV0794ReleaseFixtureFragments()
	fragmentDigest, err := appACLCurrentReleaseFixtureFragmentDigest(releaseFragments)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if got := hex.EncodeToString(fragmentDigest[:]); got != appACLCurrentV0794FragmentDigestGolden {
		return AppACLManifestPersistedV1{}, fmt.Errorf("v0.79.4 release fragment digest = %s, want frozen %s", got, appACLCurrentV0794FragmentDigestGolden)
	}
	releaseSource, err := compileAppACLCurrentSourceContract(releaseFS, releaseFragments)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile frozen v0.79.4 release source: %w", err)
	}
	releasePrivileges, err := appACLCurrentTransitionPrivilegeBody(releaseSource)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile frozen v0.79.4 release privileges: %w", err)
	}
	releaseManifest, err := NewAppACLManifestPersistedV1(
		1,
		appACLCurrentTransitionMigrator,
		[32]byte{},
		releaseSource.sources.canonicalSet,
		releasePrivileges,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile frozen v0.79.4 release manifest: %w", err)
	}
	if !bytes.Equal(releaseSource.sources.canonicalSet, appACLCurrentV0794MigrationGolden) ||
		!bytes.Equal(releasePrivileges, appACLCurrentV0794PrivilegeGolden) ||
		releaseManifest.ManifestDigest != appACLCurrentV0794ManifestDigestGolden {
		return AppACLManifestPersistedV1{}, fmt.Errorf("v0.79.4 release fixture differs from frozen release goldens")
	}
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	dependencies.transitionDefinitions = nil
	manifest, err := convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return db.BeginTx(ctx, options)
		},
		runtimeRole,
		adminRole,
		releaseFS,
		releaseFragments,
		dependencies,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if manifest.ManifestRevision != 1 || manifest.PreviousManifestDigest != ([32]byte{}) ||
		manifest.ManifestDigest != appACLCurrentV0794ManifestDigestGolden ||
		!bytes.Equal(manifest.CanonicalMigrationSet, appACLCurrentV0794MigrationGolden) ||
		!bytes.Equal(manifest.CanonicalPrivilegeSet, appACLCurrentV0794PrivilegeGolden) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("materialized v0.79.4 release fixture does not match frozen release state")
	}
	return manifest, nil
}

func appACLCurrentV0794ReleaseFixtureFragments() []AppACLCurrentMigrationFragment {
	return []AppACLCurrentMigrationFragment{
		recordsCoreAppACLCurrentMigrationFragment(),
		recordAttachmentsAppACLCurrentMigrationFragment(),
		recordEvidenceAppACLCurrentMigrationFragment(),
		recordCollaborationAppACLCurrentMigrationFragment(),
		recordSearchAppACLCurrentMigrationFragment(),
		recordActivityAppACLCurrentMigrationFragment(),
		recordPortabilityAppACLCurrentMigrationFragment(),
		recordPortabilityBlobKeyMuslAppACLCurrentMigrationFragment(),
		recordsAuthorityAppACLCurrentMigrationFragment(),
		subscriptionCreateIdempotencyAppACLCurrentMigrationFragment(),
		vpsCreateIdempotencyAppACLCurrentMigrationFragment(),
	}
}

func appACLCurrentReleaseFixtureFragmentDigest(fragments []AppACLCurrentMigrationFragment) ([32]byte, error) {
	type fragmentSnapshot struct {
		Migration           string
		Objects             []AppACLManagedObjectR1
		Privileges          []AppACLPrivilege
		AuxiliaryPrivileges []AppACLCurrentAuxiliaryPrivilege
		Functions           []AppACLCurrentFunctionContract
	}
	snapshot := make([]fragmentSnapshot, 0, len(fragments))
	for _, fragment := range fragments {
		privileges := []AppACLPrivilege(nil)
		if fragment.Privileges != nil {
			privileges = fragment.Privileges(appACLCurrentTransitionDatabase)
		}
		snapshot = append(snapshot, fragmentSnapshot{
			Migration:           fragment.Migration,
			Objects:             append([]AppACLManagedObjectR1(nil), fragment.Objects...),
			Privileges:          append([]AppACLPrivilege(nil), privileges...),
			AuxiliaryPrivileges: append([]AppACLCurrentAuxiliaryPrivilege(nil), fragment.AuxiliaryPrivileges...),
			Functions:           cloneAppACLCurrentFunctionContracts(fragment.Functions),
		})
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode frozen v0.79.4 fragment snapshot: %w", err)
	}
	return sha256.Sum256(body), nil
}

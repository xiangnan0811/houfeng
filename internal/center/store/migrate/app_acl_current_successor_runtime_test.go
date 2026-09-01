package migrate

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/db/migrations"
)

func TestAdmitAppACLCurrentRuntimeAcceptsRegisteredRevisionTwo(t *testing.T) {
	snapshot, _, catalogSnapshot := appACLCurrentRuntimeAdmissionFixture(t, migrations.FS, appACLCurrentMigrationFragments)
	useProductionRuntimeIdentityForSuccessorTest(&snapshot, &catalogSnapshot)
	transition, predecessor, successor, currentApplied := appACLCurrentRegisteredSuccessorFixture(t)
	snapshot.Manifests = []AppACLManifestPersistedV1{predecessor, successor}
	snapshot.Head = &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: successor.ManifestDigest}
	snapshot.AppliedMigrations = currentApplied
	tx := &fakeAppACLRuntimeAdmissionTx{}

	err := admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		migrations.FS,
		appACLCurrentMigrationFragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
				return snapshot, nil
			},
			readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				return catalogSnapshot, nil
			},
			verifyCatalog:         verifyAppACLEffectiveCatalogSnapshot,
			transitionDefinitions: cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions),
		},
	)
	if err != nil {
		t.Fatalf("admit registered revision-two runtime: %v", err)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 || transition.predecessorManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("registered revision-two lifecycle/transition = commit %d rollback %d predecessor %x", tx.commitCalls, tx.rollbackCalls, transition.predecessorManifestDigest)
	}
}

func TestAdmitAppACLCurrentRuntimeRejectsRegisteredPredecessorBeforeCatalog(t *testing.T) {
	snapshot, _, _ := appACLCurrentRuntimeAdmissionFixture(t, migrations.FS, appACLCurrentMigrationFragments)
	useProductionRuntimeIdentityForSuccessorTest(&snapshot, nil)
	_, predecessor, _, _ := appACLCurrentRegisteredSuccessorFixture(t)
	predecessorApplied, err := ParseCanonicalMigrationSetBodyV1(predecessor.CanonicalMigrationSet)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Manifests = []AppACLManifestPersistedV1{predecessor}
	snapshot.Head = &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: predecessor.ManifestDigest}
	snapshot.AppliedMigrations = predecessorApplied
	tx := &fakeAppACLRuntimeAdmissionTx{}

	err = admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		migrations.FS,
		appACLCurrentMigrationFragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
				return snapshot, nil
			},
			readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				t.Fatal("registered predecessor must not admit runtime before successor convergence")
				return AppACLEffectiveCatalogSnapshotR1{}, nil
			},
			verifyCatalog:         verifyAppACLEffectiveCatalogSnapshot,
			transitionDefinitions: cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions),
		},
	)
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("registered predecessor runtime error = %v, want rebuild-required", err)
	}
}

func useProductionRuntimeIdentityForSuccessorTest(
	snapshot *AppACLManifestRuntimeSnapshotV1,
	catalog *AppACLEffectiveCatalogSnapshotR1,
) {
	snapshot.SessionUser = "houfeng_runtime"
	snapshot.CurrentUser = "houfeng_runtime"
	if catalog == nil {
		return
	}
	catalog.SessionUser = "houfeng_runtime"
	catalog.CurrentUser = "houfeng_runtime"
	for index := range catalog.Roles {
		if catalog.Roles[index].Name == "houfeng_center_runtime" {
			catalog.Roles[index].Name = "houfeng_runtime"
		}
	}
	for index := range catalog.DirectPrivileges {
		if catalog.DirectPrivileges[index].Grantee == "houfeng_center_runtime" {
			catalog.DirectPrivileges[index].Grantee = "houfeng_runtime"
		}
	}
	for index := range catalog.EffectivePrivileges {
		if catalog.EffectivePrivileges[index].Grantee == "houfeng_center_runtime" {
			catalog.EffectivePrivileges[index].Grantee = "houfeng_runtime"
		}
	}
}

func appACLCurrentRegisteredSuccessorFixture(t *testing.T) (appACLCurrentTransition, AppACLManifestPersistedV1, AppACLManifestPersistedV1, []MigrationChecksumEntry) {
	t.Helper()
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	transition := transitions[0]
	predecessor, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, transition.predecessor.sources.canonicalSet, transition.predecessorPrivilegeBody)
	if err != nil {
		t.Fatal(err)
	}
	successor, err := NewAppACLManifestPersistedV1(2, appACLCurrentTransitionMigrator, predecessor.ManifestDigest, current.sources.canonicalSet, transition.predecessorPrivilegeBody)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(current.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	return transition, predecessor, successor, applied
}

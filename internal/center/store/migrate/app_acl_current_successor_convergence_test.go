package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

func TestConvergeAppACLCurrentRegisteredPredecessorPublishesRevisionTwo(t *testing.T) {
	transition, predecessor, _, currentApplied := appACLCurrentRegisteredSuccessorFixture(t)
	predecessorApplied, err := ParseCanonicalMigrationSetBodyV1(transition.predecessor.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ := appACLCurrentConvergenceExpected(t, migrations.FS, appACLCurrentMigrationFragments)
	tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.resolveRoles = func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
		return platformmigrate.AppRoleSetV1{CenterRuntime: "houfeng_runtime", PlatformAdmin: "houfeng_platform_admin", Migrator: "houfeng_migrator"}, nil
	}
	dependencies.transitionDefinitions = cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	applied := append([]MigrationChecksumEntry(nil), predecessorApplied...)
	manifests := []AppACLManifestPersistedV1{predecessor}
	head := &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: predecessor.ManifestDigest}
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
		return append([]MigrationChecksumEntry(nil), applied...), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return append([]AppACLManifestPersistedV1(nil), manifests...), nil
	}
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readHeadForUpdate = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	steps := make([]string, 0, 12)
	dependencies.rejectMisplaced = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		steps = append(steps, "placement")
		return nil
	}
	dependencies.rejectLegacy = func(context.Context, pgx.Tx, migrationSourceSnapshot, appACLEffectiveCatalogContract, string) error {
		steps = append(steps, "legacy")
		return nil
	}
	dependencies.preflightTransition = func(context.Context, pgx.Tx, appACLCurrentTransition) (appACLCurrentTransitionPreflight, error) {
		steps = append(steps, "transition-preflight")
		return appACLCurrentTransitionPreflight{incidentDefaults: []byte("before")}, nil
	}
	dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		steps = append(steps, "catalog")
		return AppACLEffectiveCatalogSnapshotR1{}, nil
	}
	dependencies.verifyCatalog = func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error { return nil }
	dependencies.applyPending = func(_ context.Context, _ pgx.Tx, snapshot migrationSourceSnapshot, got []MigrationChecksumEntry) error {
		steps = append(steps, "migration")
		if !reflect.DeepEqual(got, predecessorApplied) || !equalStringSlices(snapshot.names, current.sources.names) {
			t.Fatalf("registered apply input = %#v/%#v", got, snapshot.names)
		}
		applied = append([]MigrationChecksumEntry(nil), currentApplied...)
		return nil
	}
	dependencies.verifyTransitionApplied = func(_ context.Context, _ pgx.Tx, got appACLCurrentTransition, before appACLCurrentTransitionPreflight) error {
		steps = append(steps, "transition-applied")
		if got.predecessorManifestDigest != transition.predecessorManifestDigest || string(before.incidentDefaults) != "before" {
			t.Fatalf("transition applied verification input = %#v/%#v", got, before)
		}
		return nil
	}
	dependencies.verifyTransitionCurrent = func(context.Context, pgx.Tx, appACLCurrentTransition) error {
		steps = append(steps, "transition-current")
		return nil
	}
	dependencies.insertSuccessor = func(_ context.Context, _ pgx.Tx, previous AppACLManifestPersistedV1, migrationBody, privilegeBody []byte) (AppACLManifestPersistedV1, error) {
		steps = append(steps, "revision-two")
		if !reflect.DeepEqual(privilegeBody, transition.predecessorPrivilegeBody) {
			t.Fatal("current privilege compiler changed across empty 0063 transition")
		}
		inserted, buildErr := NewAppACLManifestPersistedV1(2, previous.MigratorCatalogRole, previous.ManifestDigest, migrationBody, privilegeBody)
		if buildErr != nil {
			return AppACLManifestPersistedV1{}, buildErr
		}
		manifests = []AppACLManifestPersistedV1{previous, inserted}
		head = &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: inserted.ManifestDigest}
		return inserted, nil
	}
	dependencies.rejectFresh = currentUnexpectedRejectFresh(t, "registered predecessor")
	dependencies.ensureLedger = currentUnexpectedEnsureLedger(t, "registered predecessor")
	dependencies.applyDCL = currentUnexpectedApplyDCL(t, "registered predecessor")
	dependencies.insertGenesis = currentUnexpectedInsertGenesis(t, "registered predecessor")

	result, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		"houfeng_runtime",
		"houfeng_platform_admin",
		migrations.FS,
		appACLCurrentMigrationFragments,
		dependencies,
	)
	if err != nil {
		t.Fatalf("registered predecessor convergence: %v", err)
	}
	if result.ManifestRevision != 2 || !tx.commitCalled || tx.rollbackCalled != true {
		t.Fatalf("registered predecessor result/lifecycle = revision %d commit %v rollback-defer %v", result.ManifestRevision, tx.commitCalled, tx.rollbackCalled)
	}
	for _, want := range []string{"placement", "legacy", "transition-preflight", "migration", "transition-applied", "revision-two", "transition-current"} {
		if !containsString(steps, want) {
			t.Fatalf("registered successor steps = %#v, missing %q", steps, want)
		}
	}
}

func TestConvergeAppACLCurrentRegisteredSuccessorRepeatIsReadOnly(t *testing.T) {
	transition, predecessor, successor, currentApplied := appACLCurrentRegisteredSuccessorFixture(t)
	tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.resolveRoles = func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
		return platformmigrate.AppRoleSetV1{CenterRuntime: "houfeng_runtime", PlatformAdmin: "houfeng_platform_admin", Migrator: "houfeng_migrator"}, nil
	}
	dependencies.transitionDefinitions = cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return currentApplied, nil }
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return []AppACLManifestPersistedV1{predecessor, successor}, nil
	}
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: successor.ManifestDigest}, nil
	}
	dependencies.preflightTransition = func(context.Context, pgx.Tx, appACLCurrentTransition) (appACLCurrentTransitionPreflight, error) {
		t.Fatal("exact successor repeat must not run predecessor preflight")
		return appACLCurrentTransitionPreflight{}, nil
	}
	dependencies.applyPending = currentUnexpectedApplyPending(t, "registered successor repeat")
	dependencies.applyDCL = currentUnexpectedApplyDCL(t, "registered successor repeat")
	dependencies.insertGenesis = currentUnexpectedInsertGenesis(t, "registered successor repeat")
	dependencies.insertSuccessor = func(context.Context, pgx.Tx, AppACLManifestPersistedV1, []byte, []byte) (AppACLManifestPersistedV1, error) {
		t.Fatal("exact successor repeat must not insert another revision")
		return AppACLManifestPersistedV1{}, nil
	}
	currentVerifications := 0
	dependencies.verifyTransitionCurrent = func(_ context.Context, _ pgx.Tx, got appACLCurrentTransition) error {
		currentVerifications++
		if got.predecessorManifestDigest != transition.predecessorManifestDigest {
			t.Fatal("repeat selected wrong transition")
		}
		return nil
	}

	result, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		"houfeng_runtime",
		"houfeng_platform_admin",
		migrations.FS,
		appACLCurrentMigrationFragments,
		dependencies,
	)
	if err != nil {
		t.Fatalf("registered successor repeat: %v", err)
	}
	if result.ManifestDigest != successor.ManifestDigest || currentVerifications != 1 || !tx.commitCalled {
		t.Fatalf("repeat result/verifications/commit = %x/%d/%v", result.ManifestDigest, currentVerifications, tx.commitCalled)
	}
}

func TestConvergeAppACLCurrentRegisteredPredecessorRollsBackEveryTransitionCutpoint(t *testing.T) {
	for _, failedStage := range []string{
		"preflight",
		"apply",
		"ledger-reread",
		"applied-verifier",
		"post-apply-catalog",
		"successor-insert",
		"head-readback",
		"manifest-readback",
		"final-transition-verifier",
		"final-catalog",
		"commit",
	} {
		failedStage := failedStage
		t.Run(failedStage, func(t *testing.T) {
			transition, predecessor, _, currentApplied := appACLCurrentRegisteredSuccessorFixture(t)
			predecessorApplied, err := ParseCanonicalMigrationSetBodyV1(transition.predecessor.sources.canonicalSet)
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("transition cutpoint failure")
			tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
			if failedStage == "commit" {
				tx.commitErr = failure
			}
			dependencies := appACLCurrentConvergenceTestDependencies()
			dependencies.transitionDefinitions = cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
			dependencies.resolveRoles = func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
				return platformmigrate.AppRoleSetV1{CenterRuntime: "houfeng_runtime", PlatformAdmin: "houfeng_platform_admin", Migrator: "houfeng_migrator"}, nil
			}
			dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
				return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
			}
			applied := append([]MigrationChecksumEntry(nil), predecessorApplied...)
			manifests := []AppACLManifestPersistedV1{predecessor}
			head := &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: predecessor.ManifestDigest}
			events := make([]string, 0, 24)
			fail := func(stage string) error {
				events = append(events, stage)
				if failedStage == stage {
					return failure
				}
				return nil
			}
			dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
				return cloneAppACLManifestHeadForTest(head), nil
			}
			appliedReads := 0
			dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
				appliedReads++
				if appliedReads == 2 {
					if err := fail("ledger-reread"); err != nil {
						return nil, err
					}
				}
				return append([]MigrationChecksumEntry(nil), applied...), nil
			}
			manifestReads := 0
			dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
				manifestReads++
				if manifestReads == 2 {
					if err := fail("manifest-readback"); err != nil {
						return nil, err
					}
				}
				return append([]AppACLManifestPersistedV1(nil), manifests...), nil
			}
			dependencies.preflightTransition = func(context.Context, pgx.Tx, appACLCurrentTransition) (appACLCurrentTransitionPreflight, error) {
				if err := fail("preflight"); err != nil {
					return appACLCurrentTransitionPreflight{}, err
				}
				return appACLCurrentTransitionPreflight{incidentDefaults: []byte("before")}, nil
			}
			dependencies.applyPending = func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
				if err := fail("apply"); err != nil {
					return err
				}
				applied = append([]MigrationChecksumEntry(nil), currentApplied...)
				return nil
			}
			dependencies.verifyTransitionApplied = func(context.Context, pgx.Tx, appACLCurrentTransition, appACLCurrentTransitionPreflight) error {
				return fail("applied-verifier")
			}
			catalogReads := 0
			dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				catalogReads++
				switch catalogReads {
				case 2:
					if err := fail("post-apply-catalog"); err != nil {
						return AppACLEffectiveCatalogSnapshotR1{}, err
					}
				case 3:
					if err := fail("final-catalog"); err != nil {
						return AppACLEffectiveCatalogSnapshotR1{}, err
					}
				}
				return AppACLEffectiveCatalogSnapshotR1{}, nil
			}
			dependencies.verifyCatalog = func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error { return nil }
			dependencies.insertSuccessor = func(_ context.Context, _ pgx.Tx, previous AppACLManifestPersistedV1, migrationBody, privilegeBody []byte) (AppACLManifestPersistedV1, error) {
				if err := fail("successor-insert"); err != nil {
					return AppACLManifestPersistedV1{}, err
				}
				inserted, err := NewAppACLManifestPersistedV1(2, previous.MigratorCatalogRole, previous.ManifestDigest, migrationBody, privilegeBody)
				if err != nil {
					return AppACLManifestPersistedV1{}, err
				}
				manifests = []AppACLManifestPersistedV1{previous, inserted}
				head = &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: inserted.ManifestDigest}
				return inserted, nil
			}
			dependencies.readHeadForUpdate = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
				if err := fail("head-readback"); err != nil {
					return nil, err
				}
				return cloneAppACLManifestHeadForTest(head), nil
			}
			dependencies.verifyTransitionCurrent = func(context.Context, pgx.Tx, appACLCurrentTransition) error {
				return fail("final-transition-verifier")
			}

			_, err = convergeAppACLCurrentWithDependencies(
				context.Background(),
				func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				"houfeng_runtime",
				"houfeng_platform_admin",
				migrations.FS,
				appACLCurrentMigrationFragments,
				dependencies,
			)
			if !errors.Is(err, failure) {
				t.Fatalf("cutpoint error = %v, want wrapped failure", err)
			}
			if !tx.rollbackCalled {
				t.Fatal("failed transition did not execute rollback defer")
			}
			if failedStage != "commit" && tx.commitCalled {
				t.Fatal("failed transition reached commit")
			}
			if failedStage != "commit" && (len(events) == 0 || events[len(events)-1] != failedStage) {
				t.Fatalf("cutpoint %q continued into later seam: events=%#v", failedStage, events)
			}
		})
	}
}

func TestConvergeAppACLCurrentRegisteredPredecessorRetriesWholeSerializableClosure(t *testing.T) {
	transition, predecessor, _, currentApplied := appACLCurrentRegisteredSuccessorFixture(t)
	predecessorApplied, err := ParseCanonicalMigrationSetBodyV1(transition.predecessor.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.transitionDefinitions = cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
	dependencies.resolveRoles = func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
		return platformmigrate.AppRoleSetV1{CenterRuntime: "houfeng_runtime", PlatformAdmin: "houfeng_platform_admin", Migrator: "houfeng_migrator"}, nil
	}
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	var applied []MigrationChecksumEntry
	var manifests []AppACLManifestPersistedV1
	var head *AppACLManifestHeadV1
	attempts := 0
	txs := make([]*recordingAppACLCurrentConvergenceTx, 0, 2)
	begin := func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		attempts++
		applied = append([]MigrationChecksumEntry(nil), predecessorApplied...)
		manifests = []AppACLManifestPersistedV1{predecessor}
		head = &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: predecessor.ManifestDigest}
		tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
		if attempts == 1 {
			tx.commitErr = &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		}
		txs = append(txs, tx)
		return tx, nil
	}
	phaseReads, preflights, applies, inserts, finals := 0, 0, 0, 0, 0
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		phaseReads++
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
		return append([]MigrationChecksumEntry(nil), applied...), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return append([]AppACLManifestPersistedV1(nil), manifests...), nil
	}
	dependencies.readHeadForUpdate = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.preflightTransition = func(context.Context, pgx.Tx, appACLCurrentTransition) (appACLCurrentTransitionPreflight, error) {
		preflights++
		return appACLCurrentTransitionPreflight{}, nil
	}
	dependencies.applyPending = func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
		applies++
		applied = append([]MigrationChecksumEntry(nil), currentApplied...)
		return nil
	}
	dependencies.verifyTransitionApplied = func(context.Context, pgx.Tx, appACLCurrentTransition, appACLCurrentTransitionPreflight) error {
		return nil
	}
	dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		return AppACLEffectiveCatalogSnapshotR1{}, nil
	}
	dependencies.verifyCatalog = func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error { return nil }
	dependencies.insertSuccessor = func(_ context.Context, _ pgx.Tx, previous AppACLManifestPersistedV1, migrationBody, privilegeBody []byte) (AppACLManifestPersistedV1, error) {
		inserts++
		inserted, err := NewAppACLManifestPersistedV1(2, previous.MigratorCatalogRole, previous.ManifestDigest, migrationBody, privilegeBody)
		if err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		manifests = []AppACLManifestPersistedV1{previous, inserted}
		head = &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: inserted.ManifestDigest}
		return inserted, nil
	}
	dependencies.verifyTransitionCurrent = func(context.Context, pgx.Tx, appACLCurrentTransition) error {
		finals++
		return nil
	}

	result, err := convergeAppACLCurrentWithDependencies(
		context.Background(), begin, "houfeng_runtime", "houfeng_platform_admin",
		migrations.FS, appACLCurrentMigrationFragments, dependencies,
	)
	if err != nil {
		t.Fatalf("retry registered predecessor: %v", err)
	}
	if result.ManifestRevision != 2 || attempts != 2 || phaseReads != 2 || preflights != 2 || applies != 2 || inserts != 2 || finals != 2 {
		t.Fatalf("retry closure counts = result:%d attempts:%d phase:%d preflight:%d apply:%d insert:%d final:%d", result.ManifestRevision, attempts, phaseReads, preflights, applies, inserts, finals)
	}
	if len(txs) != 2 || !txs[0].rollbackCalled || !txs[1].commitCalled {
		t.Fatalf("retry transaction lifecycle = %#v", txs)
	}
}

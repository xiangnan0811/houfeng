package migrate

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

func TestConvergeAppACLCurrentRejectsMissingFragmentBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	beginCalls := 0

	_, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			beginCalls++
			return nil, errors.New("begin must not run")
		},
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		fsys,
		nil,
		appACLCurrentConvergenceTestDependencies(),
	)
	if err == nil || !strings.Contains(err.Error(), `migration "0052_future.sql" has no current APP ACL fragment`) {
		t.Fatalf("convergeAppACLCurrentWithDependencies() error = %v, want missing-fragment rejection", err)
	}
	if beginCalls != 0 {
		t.Fatalf("BeginTx calls = %d, want 0", beginCalls)
	}
}

func TestConvergeAppACLCurrentRegisteredFutureMigrationReachesBeginTx(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	beginErr := errors.New("begin sentinel")
	beginCalls := 0

	_, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			beginCalls++
			return nil, beginErr
		},
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		fsys,
		[]AppACLCurrentMigrationFragment{{
			Migration:  "0052_future.sql",
			Privileges: func(string) []AppACLPrivilege { return nil },
		}},
		appACLCurrentConvergenceTestDependencies(),
	)
	if !errors.Is(err, beginErr) {
		t.Fatalf("convergeAppACLCurrentWithDependencies() error = %v, want wrapped begin sentinel", err)
	}
	if beginCalls != 1 {
		t.Fatalf("BeginTx calls = %d, want 1", beginCalls)
	}
}

func TestConvergeAppACLCurrentRejectsNilTransaction(t *testing.T) {
	_, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, nil },
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		appACLCurrentTestMigrationFS(t),
		nil,
		appACLCurrentConvergenceTestDependencies(),
	)
	if err == nil || !strings.Contains(err.Error(), "begin current app ACL convergence transaction returned nil transaction") {
		t.Fatalf("nil transaction error = %v, want defensive rejection", err)
	}
}

func TestConvergeAppACLCurrentDifferentBaselineRequiresRebuildBeforeMutation(t *testing.T) {
	fsys, fragments := appACLCurrentConvergenceFutureSource(t)
	priorSources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	_, currentCatalog, compiledPrivileges := appACLCurrentConvergenceExpected(t, fsys, fragments)
	priorGenesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, priorSources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatal(err)
	}
	priorApplied, err := ParseCanonicalMigrationSetBodyV1(priorSources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	head := &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: priorGenesis.ManifestDigest}
	tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return priorApplied, nil }
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return []AppACLManifestPersistedV1{priorGenesis}, nil
	}
	dependencies.rejectMisplaced = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		t.Fatal("different baseline must be classified before managed-object placement preflight")
		return nil
	}
	dependencies.rejectLegacy = func(context.Context, pgx.Tx, migrationSourceSnapshot, appACLEffectiveCatalogContract, string) error {
		t.Fatal("different baseline must be classified before legacy-ledger preflight")
		return nil
	}
	dependencies.rejectFresh = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		t.Fatal("different baseline must not enter fresh preflight")
		return nil
	}
	dependencies.ensureLedger = func(context.Context, pgx.Tx, map[string]migrationSource) error {
		t.Fatal("different baseline must not create or repair the ledger")
		return nil
	}
	dependencies.applyPending = func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
		t.Fatal("different baseline must not apply migrations")
		return nil
	}
	dependencies.applyDCL = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		t.Fatal("different baseline must not apply DCL")
		return nil
	}
	dependencies.insertGenesis = func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
		t.Fatal("different baseline must not insert a manifest")
		return AppACLManifestPersistedV1{}, nil
	}
	dependencies.readHeadForUpdate = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		t.Fatal("different baseline must not lock or repair the manifest head")
		return nil, nil
	}
	dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		t.Fatal("different baseline must fail before catalog verification")
		return AppACLEffectiveCatalogSnapshotR1{}, nil
	}
	_ = currentCatalog

	_, err = convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		fsys,
		fragments,
		dependencies,
	)
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("convergeAppACLCurrentWithDependencies() error = %v, want rebuild-required sentinel", err)
	}
	if !tx.rollbackCalled || tx.commitCalled {
		t.Fatalf("different baseline transaction commit/rollback = %v/%v, want false/true", tx.commitCalled, tx.rollbackCalled)
	}
}

func TestCompareAppACLCurrentMigrationEntriesChecksumMismatchRequiresRebuild(t *testing.T) {
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ParseCanonicalMigrationSetBodyV1(sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	actual[len(actual)-1].Checksum[0] ^= 0xff

	err = compareAppACLCurrentMigrationEntries(sources.canonicalSet, actual, "applied migration ledger")
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("checksum mismatch error = %v, want rebuild-required sentinel", err)
	}
}

func TestCompareAppACLCurrentMigrationEntriesFilenameMismatchRequiresRebuild(t *testing.T) {
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ParseCanonicalMigrationSetBodyV1(sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	actual[len(actual)-1].Filename = "0051_different_baseline.sql"

	err = compareAppACLCurrentMigrationEntries(sources.canonicalSet, actual, "persisted manifest")
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("filename mismatch error = %v, want rebuild-required sentinel", err)
	}
}

func TestConvergeAppACLCurrentSuccessorManifestRequiresRebuildBeforeCatalogRead(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	sources, _, compiledPrivileges := appACLCurrentConvergenceExpected(t, fsys, nil)
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatal(err)
	}
	successor, err := NewAppACLManifestPersistedV1(2, "houfeng_migrator", genesis.ManifestDigest, sources.sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	head := &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: successor.ManifestDigest}
	tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return applied, nil }
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return []AppACLManifestPersistedV1{genesis, successor}, nil
	}
	dependencies.rejectFresh = currentUnexpectedRejectFresh(t, "successor manifest")
	dependencies.ensureLedger = currentUnexpectedEnsureLedger(t, "successor manifest")
	dependencies.applyPending = currentUnexpectedApplyPending(t, "successor manifest")
	dependencies.applyDCL = currentUnexpectedApplyDCL(t, "successor manifest")
	dependencies.insertGenesis = currentUnexpectedInsertGenesis(t, "successor manifest")
	dependencies.readHeadForUpdate = currentUnexpectedHeadForUpdate(t, "successor manifest")
	dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		t.Fatal("successor manifest must fail before catalog read")
		return AppACLEffectiveCatalogSnapshotR1{}, nil
	}

	_, err = convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		fsys,
		nil,
		dependencies,
	)
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("successor manifest error = %v, want rebuild-required sentinel", err)
	}
	if !tx.rollbackCalled || tx.commitCalled {
		t.Fatalf("successor manifest commit/rollback = %v/%v, want false/true", tx.commitCalled, tx.rollbackCalled)
	}
}

func TestConvergeAppACLCurrentExactRepeatOmitsMutation(t *testing.T) {
	fsys, fragments := appACLCurrentConvergenceFutureSource(t)
	sources, _, compiledPrivileges := appACLCurrentConvergenceExpected(t, fsys, fragments)
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	head := &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: genesis.ManifestDigest}
	tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return applied, nil }
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return []AppACLManifestPersistedV1{genesis}, nil
	}
	dependencies.rejectFresh = currentUnexpectedRejectFresh(t, "exact repeat")
	dependencies.ensureLedger = currentUnexpectedEnsureLedger(t, "exact repeat")
	dependencies.applyPending = currentUnexpectedApplyPending(t, "exact repeat")
	dependencies.applyDCL = currentUnexpectedApplyDCL(t, "exact repeat")
	dependencies.insertGenesis = currentUnexpectedInsertGenesis(t, "exact repeat")
	dependencies.readHeadForUpdate = currentUnexpectedHeadForUpdate(t, "exact repeat")
	preflightCalls := 0
	dependencies.rejectMisplaced = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		preflightCalls++
		return nil
	}
	dependencies.rejectLegacy = func(context.Context, pgx.Tx, migrationSourceSnapshot, appACLEffectiveCatalogContract, string) error {
		preflightCalls++
		return nil
	}
	catalogReads := 0
	dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		catalogReads++
		return AppACLEffectiveCatalogSnapshotR1{}, nil
	}

	var options pgx.TxOptions
	result, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) { options = got; return tx, nil },
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		fsys,
		fragments,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLCurrentWithDependencies() error = %v", err)
	}
	if result.ManifestDigest != genesis.ManifestDigest || !tx.commitCalled || options.IsoLevel != pgx.Serializable || preflightCalls != 2 || catalogReads != 1 {
		t.Fatalf("exact repeat result/commit/isolation/preflight/catalog reads = %x/%v/%v/%d/%d", result.ManifestDigest, tx.commitCalled, options.IsoLevel, preflightCalls, catalogReads)
	}
}

func TestConvergeAppACLCurrentFreshUsesSerializableTransaction(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	sources, _, compiledPrivileges := appACLCurrentConvergenceExpected(t, fsys, nil)
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	tx := &recordingAppACLCurrentConvergenceTx{fakeAppACLConvergenceTx: &fakeAppACLConvergenceTx{}}
	dependencies := appACLCurrentConvergenceTestDependencies()
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{}, nil
	}
	steps := make([]string, 0, 16)
	dependencies.rejectFresh = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		steps = append(steps, "fresh")
		return nil
	}
	dependencies.ensureLedger = func(context.Context, pgx.Tx, map[string]migrationSource) error {
		steps = append(steps, "ledger")
		return nil
	}
	appliedReads := 0
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
		appliedReads++
		if appliedReads == 1 {
			return nil, nil
		}
		return applied, nil
	}
	dependencies.applyPending = func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
		steps = append(steps, "migrations")
		return nil
	}
	dependencies.applyDCL = func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		steps = append(steps, "dcl")
		return nil
	}
	dependencies.readCatalog = func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		steps = append(steps, "catalog")
		return AppACLEffectiveCatalogSnapshotR1{}, nil
	}
	var head *AppACLManifestHeadV1
	var manifests []AppACLManifestPersistedV1
	dependencies.readHeadForUpdate = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return append([]AppACLManifestPersistedV1(nil), manifests...), nil
	}
	dependencies.insertGenesis = func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
		steps = append(steps, "genesis")
		manifests = []AppACLManifestPersistedV1{genesis}
		head = &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: genesis.ManifestDigest}
		return genesis, nil
	}

	var options pgx.TxOptions
	result, err := convergeAppACLCurrentWithDependencies(
		context.Background(),
		func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) { options = got; return tx, nil },
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		fsys,
		nil,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLCurrentWithDependencies() error = %v", err)
	}
	if result.ManifestDigest != genesis.ManifestDigest || !tx.commitCalled || options.IsoLevel != pgx.Serializable {
		t.Fatalf("fresh result/commit/isolation = %x/%v/%v", result.ManifestDigest, tx.commitCalled, options.IsoLevel)
	}
	for _, want := range []string{"fresh", "ledger", "migrations", "dcl", "catalog", "genesis"} {
		if !containsString(steps, want) {
			t.Fatalf("fresh convergence steps = %#v, missing %q", steps, want)
		}
	}
}

func TestConvergeAppACLR1RetainsNullHeadAdoption(t *testing.T) {
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	bindings := appACLCurrentCatalogTestBindings()
	compiledPrivileges, err := CompileAppACLPrivilegeSetR1("houfeng", bindings)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatal(err)
	}
	var head *AppACLManifestHeadV1
	var manifests []AppACLManifestPersistedV1
	ensureCalls := 0
	dependencies := defaultR1ConvergenceTestDependencies(t)
	dependencies.readPhaseState = func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
		return appACLConvergencePhaseState{LedgerExists: true, ManifestRevisionsExists: true, ManifestHeadExists: true}, nil
	}
	dependencies.readHead = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) { return nil, nil }
	dependencies.ensureLedger = func(context.Context, pgx.Tx, map[string]migrationSource) error { ensureCalls++; return nil }
	dependencies.readApplied = func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return applied, nil }
	dependencies.applyPending = func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
		t.Fatal("R1 null-head adoption must not apply migrations")
		return nil
	}
	dependencies.readHeadForUpdate = func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		return cloneAppACLManifestHeadForTest(head), nil
	}
	dependencies.readManifests = func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		return append([]AppACLManifestPersistedV1(nil), manifests...), nil
	}
	dependencies.insertGenesis = func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
		manifests = []AppACLManifestPersistedV1{genesis}
		head = &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: genesis.ManifestDigest}
		return genesis, nil
	}
	tx := &fakeAppACLConvergenceTx{}
	result, err := convergeAppACLR1WithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		sources,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLR1WithDependencies() null-head adoption error = %v", err)
	}
	if result.ManifestDigest != genesis.ManifestDigest || ensureCalls != 1 {
		t.Fatalf("R1 null-head adoption result/ensure calls = %x/%d", result.ManifestDigest, ensureCalls)
	}
}

func TestAppACLCurrentConvergencePlacementPreflightUsesCompiledContract(t *testing.T) {
	const futureTable = "records_current_preflight"
	tx := &appACLCurrentPreflightTx{}
	contract := appACLEffectiveCatalogContract{
		DatabaseName: "houfeng",
		ManagedObjects: []AppACLManagedObjectR1{{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: futureTable,
		}},
	}

	err := defaultAppACLCurrentConvergenceDependencies().rejectMisplaced(
		context.Background(),
		tx,
		contract,
	)
	if err != nil {
		t.Fatalf("current placement preflight error = %v", err)
	}
	if !containsString(tx.relationNames, futureTable) {
		t.Fatalf("current placement relation names = %#v, want %q", tx.relationNames, futureTable)
	}
	if !containsString(tx.relationSchemas, appACLManagedPublicSchemaR1) {
		t.Fatalf("current placement relation schemas = %#v, want %q", tx.relationSchemas, appACLManagedPublicSchemaR1)
	}
}

func TestAppACLCurrentConvergencePreflightsScopeSameNamedRelationsBySchemaTuple(t *testing.T) {
	const sharedTable = "records_current_preflight"
	contract := appACLEffectiveCatalogContract{
		DatabaseName: "houfeng",
		ManagedObjects: []AppACLManagedObjectR1{
			{
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     "records_current_a",
				ObjectIdentity: sharedTable,
			},
			{
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     "records_current_b",
				ObjectIdentity: sharedTable,
			},
		},
	}

	t.Run("fresh", func(t *testing.T) {
		err := rejectFreshAppACLManagedStateForContractInTx(
			context.Background(),
			&appACLCurrentPreflightTx{},
			contract,
		)
		if err != nil {
			t.Fatalf("current fresh tuple-scoped preflight error = %v", err)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		err := rejectNonPublicAppACLLegacyLedgerForContractInTx(
			context.Background(),
			&appACLCurrentLegacyPreflightTx{},
			migrationSourceSnapshot{},
			contract,
			"houfeng_migrator",
		)
		if err != nil {
			t.Fatalf("current legacy tuple-scoped preflight error = %v", err)
		}
	})
}

func TestAppACLCurrentConvergenceLegacyPreflightIgnoresSameNamedRelationOutsideManagedTuple(t *testing.T) {
	const sharedTable = "records_current_preflight"
	contract := appACLEffectiveCatalogContract{
		DatabaseName: "houfeng",
		ManagedObjects: []AppACLManagedObjectR1{{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "records_current",
			ObjectIdentity: sharedTable,
		}},
	}
	tx := &appACLCurrentLegacyPreflightTx{
		managedSchema:   "third_party_private_history",
		managedRelation: sharedTable,
	}

	err := rejectNonPublicAppACLLegacyLedgerForContractInTx(
		context.Background(),
		tx,
		migrationSourceSnapshot{},
		contract,
		"houfeng_migrator",
	)
	if err != nil {
		t.Fatalf("current legacy unrelated tuple preflight error = %v", err)
	}
	if tx.ledgerProbeCalls != 1 {
		t.Fatalf("current legacy ledger probe calls = %d, want 1 for unrelated tuple", tx.ledgerProbeCalls)
	}
}

func TestAppACLCurrentConvergenceLegacyPreflightRejectsLedgerInCompiledManagedSchema(t *testing.T) {
	const managedSchema = "third_party_private_history"
	contract := appACLEffectiveCatalogContract{
		DatabaseName: "houfeng",
		ManagedObjects: []AppACLManagedObjectR1{{
			ObjectClass:    AppACLObjectClassSchema,
			SchemaName:     managedSchema,
			ObjectIdentity: managedSchema,
		}},
	}
	tx := &appACLCurrentLegacyPreflightTx{}

	err := rejectNonPublicAppACLLegacyLedgerForContractInTx(
		context.Background(),
		tx,
		migrationSourceSnapshot{},
		contract,
		"houfeng_migrator",
	)
	if err == nil || !strings.Contains(err.Error(), "managed schema") {
		t.Fatalf("current legacy managed-schema preflight error = %v, want managed schema rejection", err)
	}
	if tx.ledgerProbeCalls != 0 {
		t.Fatalf("current legacy ledger probe calls = %d, want 0 for managed schema", tx.ledgerProbeCalls)
	}
}

func TestAppACLCurrentConvergenceFreshPreflightUsesCompiledContract(t *testing.T) {
	t.Run("managed schema", func(t *testing.T) {
		const futureSchema = "records_current"
		tx := &appACLCurrentPreflightTx{existingSchema: futureSchema}
		contract := appACLEffectiveCatalogContract{
			DatabaseName: "houfeng",
			ManagedObjects: []AppACLManagedObjectR1{{
				ObjectClass:    AppACLObjectClassSchema,
				SchemaName:     futureSchema,
				ObjectIdentity: futureSchema,
			}},
		}

		err := defaultAppACLCurrentConvergenceDependencies().rejectFresh(
			context.Background(),
			tx,
			contract,
		)
		if err == nil || !strings.Contains(err.Error(), futureSchema) {
			t.Fatalf("current fresh preflight error = %v, want existing managed schema rejection", err)
		}
	})

	t.Run("managed public relation", func(t *testing.T) {
		const futureTable = "records_current_preflight"
		tx := &appACLCurrentPreflightTx{
			existingRelationSchema: appACLManagedPublicSchemaR1,
			existingRelation:       futureTable,
		}
		contract := appACLEffectiveCatalogContract{
			DatabaseName: "houfeng",
			ManagedObjects: []AppACLManagedObjectR1{{
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: futureTable,
			}},
		}

		err := defaultAppACLCurrentConvergenceDependencies().rejectFresh(
			context.Background(),
			tx,
			contract,
		)
		if err == nil || !strings.Contains(err.Error(), futureTable) {
			t.Fatalf("current fresh preflight error = %v, want existing managed relation rejection", err)
		}
	})
}

func TestAppACLCurrentConvergenceLegacyPreflightUsesCompiledContract(t *testing.T) {
	const (
		futureMigration = "0052_future.sql"
		futureSchema    = "third_party_private_history"
		futureTable     = "records_current_preflight"
	)
	futureFS := appACLCurrentTestMigrationFS(t)
	futureFS[futureMigration] = &fstest.MapFile{Data: []byte("select 'future';")}
	fragments := []AppACLCurrentMigrationFragment{{
		Migration: futureMigration,
		Objects: []AppACLManagedObjectR1{{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     futureSchema,
			ObjectIdentity: futureTable,
		}},
		Privileges: func(string) []AppACLPrivilege { return nil },
	}}
	source, catalog, _ := appACLCurrentConvergenceExpected(t, futureFS, fragments)
	tx := &appACLCurrentLegacyPreflightTx{
		managedSchema:   futureSchema,
		managedRelation: futureTable,
	}

	err := rejectNonPublicAppACLLegacyLedgerForContractInTx(
		context.Background(),
		tx,
		source.sources,
		catalog,
		"houfeng_migrator",
	)
	if err == nil || !strings.Contains(err.Error(), futureSchema) {
		t.Fatalf("current legacy preflight error = %v, want future managed-object rejection", err)
	}
	if tx.ledgerProbeCalls != 0 {
		t.Fatalf("current legacy ledger probe calls = %d, want 0 after managed-object evidence", tx.ledgerProbeCalls)
	}
}

func appACLCurrentConvergenceFutureSource(t *testing.T) (fs.FS, []AppACLCurrentMigrationFragment) {
	t.Helper()
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	return fsys, []AppACLCurrentMigrationFragment{{
		Migration:  "0052_future.sql",
		Privileges: func(string) []AppACLPrivilege { return nil },
	}}
}

func appACLCurrentConvergenceExpected(
	t *testing.T,
	fsys fs.FS,
	fragments []AppACLCurrentMigrationFragment,
) (appACLCurrentSourceContract, appACLEffectiveCatalogContract, []byte) {
	t.Helper()
	sources, err := compileAppACLCurrentSourceContract(fsys, fragments)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := compileAppACLCurrentCatalogContract(sources, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	privileges, err := CanonicalPrivilegeSetBodyV1(catalog.RoleBindings, catalog.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	return sources, catalog, privileges
}

func appACLCurrentConvergenceTestDependencies() appACLCurrentConvergenceDependencies {
	return appACLCurrentConvergenceDependencies{
		readDatabaseName: func(context.Context, pgx.Tx) (string, error) { return "houfeng", nil },
		resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
			return platformmigrate.AppRoleSetV1{CenterRuntime: "houfeng_center_runtime", PlatformAdmin: "houfeng_platform_admin", Migrator: "houfeng_migrator"}, nil
		},
		rejectMisplaced: func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error { return nil },
		rejectLegacy: func(context.Context, pgx.Tx, migrationSourceSnapshot, appACLEffectiveCatalogContract, string) error {
			return nil
		},
		readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
			return appACLConvergencePhaseState{}, nil
		},
		lockLedger:        func(context.Context, pgx.Tx) error { return nil },
		rejectFresh:       func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error { return nil },
		ensureLedger:      func(context.Context, pgx.Tx, map[string]migrationSource) error { return nil },
		readApplied:       func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return nil, nil },
		applyPending:      func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error { return nil },
		readManifests:     func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) { return nil, nil },
		readHead:          func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) { return nil, nil },
		readHeadForUpdate: func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) { return nil, nil },
		applyDCL:          func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error { return nil },
		readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error { return nil },
		insertGenesis: func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
			return AppACLManifestPersistedV1{}, nil
		},
	}
}

func defaultR1ConvergenceTestDependencies(t *testing.T) appACLConvergenceDependencies {
	t.Helper()
	return appACLConvergenceDependencies{
		readDatabaseName: func(context.Context, pgx.Tx) (string, error) { return "houfeng", nil },
		resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
			return platformmigrate.AppRoleSetV1{CenterRuntime: "houfeng_center_runtime", PlatformAdmin: "houfeng_platform_admin", Migrator: "houfeng_migrator"}, nil
		},
		rejectMisplaced: func(context.Context, pgx.Tx, string) error { return nil },
		readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
			return appACLConvergencePhaseState{}, nil
		},
		lockLedger:    func(context.Context, pgx.Tx) error { return nil },
		rejectLegacy:  func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error { return nil },
		rejectFresh:   func(context.Context, pgx.Tx, string) error { return nil },
		ensureLedger:  func(context.Context, pgx.Tx, map[string]migrationSource) error { return nil },
		readApplied:   func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) { return nil, nil },
		applyPending:  func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error { return nil },
		readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) { return nil, nil },
		readHead:      func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) { return nil, nil },
		readHeadForUpdate: func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
			return nil, nil
		},
		applyDCL: func(context.Context, pgx.Tx, AppACLEffectiveCatalogContractR1) error { return nil },
		readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error { return nil },
		insertGenesis: func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
			return AppACLManifestPersistedV1{}, nil
		},
	}
}

type recordingAppACLCurrentConvergenceTx struct {
	*fakeAppACLConvergenceTx
	commitCalled   bool
	rollbackCalled bool
}

type appACLCurrentPreflightTx struct {
	pgx.Tx
	existingSchema         string
	existingRelationSchema string
	existingRelation       string
	relationSchemas        []string
	relationNames          []string
}

type appACLCurrentLegacyPreflightTx struct {
	pgx.Tx
	managedSchema    string
	managedRelation  string
	ledgerProbeCalls int
}

type appACLCurrentBoolRow struct {
	value bool
}

type appACLCurrentManagedRelationRows struct {
	emptyAppACLConvergenceRows
	returned bool
	name     string
}

func (tx *appACLCurrentPreflightTx) QueryRow(_ context.Context, _ string, arguments ...any) pgx.Row {
	schemaName, _ := arguments[0].(string)
	return appACLCurrentBoolRow{value: schemaName == tx.existingSchema}
}

func (tx *appACLCurrentPreflightTx) Query(_ context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "namespace.nspname = any($1::text[])") && strings.Contains(sql, "relation.relname = any($2::text[])") {
		tx.relationSchemas, _ = arguments[0].([]string)
		tx.relationSchemas = append([]string(nil), tx.relationSchemas...)
		tx.relationNames, _ = arguments[1].([]string)
		tx.relationNames = append([]string(nil), tx.relationNames...)
		return emptyAppACLConvergenceRows{}, nil
	}
	if strings.Contains(sql, "relation.relname = any($1::text[])") {
		tx.relationNames, _ = arguments[0].([]string)
		tx.relationNames = append([]string(nil), tx.relationNames...)
		return emptyAppACLConvergenceRows{}, nil
	}
	if strings.Contains(sql, "select relation.relkind::text") && len(arguments) == 2 {
		schemaName, _ := arguments[0].(string)
		relationNames, _ := arguments[1].([]string)
		if schemaName == tx.existingRelationSchema && containsString(relationNames, tx.existingRelation) {
			return &appACLCurrentManagedRelationRows{name: tx.existingRelation}, nil
		}
	}
	return emptyAppACLConvergenceRows{}, nil
}

func (tx *appACLCurrentLegacyPreflightTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *appACLCurrentLegacyPreflightTx) Query(_ context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "relation.relname = 'schema_migrations'"):
		return &nonPublicLedgerCandidateRows{}, nil
	case strings.Contains(sql, "select relation.relkind::text"):
		schemaName, _ := arguments[0].(string)
		relationNames, _ := arguments[1].([]string)
		if schemaName == tx.managedSchema && containsString(relationNames, tx.managedRelation) {
			return &appACLCurrentManagedRelationRows{name: tx.managedRelation}, nil
		}
		return emptyAppACLConvergenceRows{}, nil
	case strings.Contains(sql, "select procedure.proname::text"):
		return emptyAppACLConvergenceRows{}, nil
	case strings.Contains(sql, "select name from"):
		tx.ledgerProbeCalls++
		return nil, &pgconn.PgError{Code: "42501", Message: "permission denied"}
	default:
		return nil, errors.New("unexpected current legacy preflight query")
	}
}

func (row appACLCurrentBoolRow) Scan(destinations ...any) error {
	if len(destinations) != 1 {
		return errors.New("unexpected current preflight bool destination count")
	}
	destination, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("current preflight bool destination is not *bool")
	}
	*destination = row.value
	return nil
}

func (rows *appACLCurrentManagedRelationRows) Next() bool {
	if rows.returned {
		return false
	}
	rows.returned = true
	return true
}

func (rows *appACLCurrentManagedRelationRows) Scan(destinations ...any) error {
	if len(destinations) != 2 {
		return errors.New("unexpected current managed relation destination count")
	}
	relationKind, ok := destinations[0].(*string)
	if !ok {
		return errors.New("current managed relation kind destination is not *string")
	}
	relationName, ok := destinations[1].(*string)
	if !ok {
		return errors.New("current managed relation name destination is not *string")
	}
	*relationKind = "r"
	*relationName = rows.name
	return nil
}

func (tx *recordingAppACLCurrentConvergenceTx) Commit(context.Context) error {
	tx.commitCalled = true
	return tx.commitErr
}

func (tx *recordingAppACLCurrentConvergenceTx) Rollback(context.Context) error {
	tx.rollbackCalled = true
	return nil
}

func currentUnexpectedRejectFresh(t *testing.T, state string) func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
	return func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		t.Fatalf("%s must not enter fresh preflight", state)
		return nil
	}
}

func currentUnexpectedEnsureLedger(t *testing.T, state string) func(context.Context, pgx.Tx, map[string]migrationSource) error {
	return func(context.Context, pgx.Tx, map[string]migrationSource) error {
		t.Fatalf("%s must not mutate ledger", state)
		return nil
	}
}

func currentUnexpectedApplyPending(t *testing.T, state string) func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
	return func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
		t.Fatalf("%s must not apply migrations", state)
		return nil
	}
}

func currentUnexpectedApplyDCL(t *testing.T, state string) func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
	return func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error {
		t.Fatalf("%s must not apply DCL", state)
		return nil
	}
}

func currentUnexpectedInsertGenesis(t *testing.T, state string) func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
	return func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
		t.Fatalf("%s must not insert manifest", state)
		return AppACLManifestPersistedV1{}, nil
	}
}

func currentUnexpectedHeadForUpdate(t *testing.T, state string) func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
	return func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		t.Fatalf("%s must not lock manifest head", state)
		return nil, nil
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

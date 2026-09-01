package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

var ErrDevelopmentDatabaseRebuildRequired = errors.New(
	"development database must be recreated for the current embedded migrations",
)

type appACLCurrentConvergenceDependencies struct {
	readDatabaseName        func(context.Context, pgx.Tx) (string, error)
	resolveRoles            func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error)
	rejectMisplaced         func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error
	rejectLegacy            func(context.Context, pgx.Tx, migrationSourceSnapshot, appACLEffectiveCatalogContract, string) error
	readPhaseState          func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error)
	lockLedger              func(context.Context, pgx.Tx) error
	rejectFresh             func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error
	ensureLedger            func(context.Context, pgx.Tx, map[string]migrationSource) error
	readApplied             func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error)
	applyPending            func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error
	readManifests           func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error)
	readHead                func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error)
	readHeadForUpdate       func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error)
	applyDCL                func(context.Context, pgx.Tx, appACLEffectiveCatalogContract) error
	readCatalog             func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error)
	verifyCatalog           func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error
	insertGenesis           func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error)
	transitionDefinitions   []appACLCurrentTransitionDefinition
	preflightTransition     func(context.Context, pgx.Tx, appACLCurrentTransition) (appACLCurrentTransitionPreflight, error)
	verifyTransitionApplied func(context.Context, pgx.Tx, appACLCurrentTransition, appACLCurrentTransitionPreflight) error
	verifyTransitionCurrent func(context.Context, pgx.Tx, appACLCurrentTransition) error
	insertSuccessor         func(context.Context, pgx.Tx, AppACLManifestPersistedV1, []byte, []byte) (AppACLManifestPersistedV1, error)
}

func defaultAppACLCurrentConvergenceDependencies() appACLCurrentConvergenceDependencies {
	return appACLCurrentConvergenceDependencies{
		readDatabaseName:  readAppACLConvergenceDatabaseName,
		resolveRoles:      resolveAppACLConvergenceRoles,
		rejectMisplaced:   rejectMisplacedAppACLManagedObjectForContractInTx,
		rejectLegacy:      rejectNonPublicAppACLLegacyLedgerForContractInTx,
		readPhaseState:    readAppACLConvergencePhaseStateInTx,
		lockLedger:        lockAppACLConvergenceLedgerInTx,
		rejectFresh:       rejectFreshAppACLManagedStateForContractInTx,
		ensureLedger:      ensureMigrationLedgerInTx,
		readApplied:       readAppliedAppMigrationsV1,
		applyPending:      applyPendingMigrationSourcesInTx,
		readManifests:     readAppACLManifestRevisionsV1,
		readHead:          readAppACLManifestHeadV1,
		readHeadForUpdate: readAppACLManifestHeadForUpdateV1,
		applyDCL:          applyAppACLConvergenceDCLForContractInTx,
		readCatalog:       readAppACLEffectiveCatalogSnapshotInTx,
		verifyCatalog:     verifyAppACLEffectiveCatalogSnapshot,
		insertGenesis:     insertAppACLManifestGenesisV1,
		transitionDefinitions: cloneAppACLCurrentTransitionDefinitions(
			appACLCurrentTransitionDefinitions,
		),
		preflightTransition:     preflightAppACLCurrentTransitionInTx,
		verifyTransitionApplied: verifyAppliedAppACLCurrentTransitionInTx,
		verifyTransitionCurrent: verifyCurrentAppACLCurrentTransitionInTx,
		insertSuccessor:         insertAppACLManifestSuccessorV1,
	}
}

func ConvergeAppACLCurrent(
	ctx context.Context,
	db *pgxpool.Pool,
	runtimeRole string,
	adminRole string,
) (AppACLManifestPersistedV1, error) {
	if db == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("current app ACL convergence has no PostgreSQL pool")
	}
	return convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return db.BeginTx(ctx, options)
		},
		runtimeRole,
		adminRole,
		migrations.FS,
		appACLCurrentMigrationFragments,
		defaultAppACLCurrentConvergenceDependencies(),
	)
}

func convergeAppACLCurrentWithDependencies(
	ctx context.Context,
	begin appACLConvergenceBeginTx,
	runtimeRole string,
	adminRole string,
	fsys fs.FS,
	fragments []AppACLCurrentMigrationFragment,
	dependencies appACLCurrentConvergenceDependencies,
) (AppACLManifestPersistedV1, error) {
	source, err := compileAppACLCurrentSourceContract(fsys, fragments)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile current app ACL source contract: %w", err)
	}
	if begin == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("current app ACL convergence transaction opener is nil")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	var transitions []appACLCurrentTransition
	if dependencies.transitionDefinitions != nil {
		transitions, err = compileAppACLCurrentTransitions(source, dependencies.transitionDefinitions)
		if err != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("compile current app ACL transitions: %w", err)
		}
	}

	var result AppACLManifestPersistedV1
	err = retryAppACLConvergence(ctx, func() error {
		tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return fmt.Errorf("begin current app ACL convergence transaction: %w", err)
		}
		if tx == nil {
			return fmt.Errorf("begin current app ACL convergence transaction returned nil transaction")
		}
		defer func() {
			_ = tx.Rollback(ctx)
		}()

		manifest, err := convergeAppACLCurrentInTx(ctx, tx, runtimeRole, adminRole, source, transitions, dependencies)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit current app ACL convergence transaction: %w", err)
		}
		result = manifest
		return nil
	})
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	return result, nil
}

func convergeAppACLCurrentInTx(
	ctx context.Context,
	tx pgx.Tx,
	runtimeRole string,
	adminRole string,
	source appACLCurrentSourceContract,
	transitions []appACLCurrentTransition,
	dependencies appACLCurrentConvergenceDependencies,
) (AppACLManifestPersistedV1, error) {
	if tx == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("current app ACL convergence has no PostgreSQL transaction")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if _, err := tx.Exec(ctx, appACLConvergenceHardenedSearchPathSQL); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("set current app ACL convergence search path: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, appACLSchemaAdvisoryLockV1); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("lock current app ACL convergence: %w", err)
	}

	roles, err := dependencies.resolveRoles(ctx, tx, runtimeRole, adminRole)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	databaseName, err := dependencies.readDatabaseName(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: roles.CenterRuntime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: roles.PlatformAdmin},
	}
	contract, err := compileAppACLCurrentCatalogContract(source, databaseName, bindings, roles.Migrator)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile current app ACL catalog contract: %w", err)
	}
	compiledPrivileges, err := CanonicalPrivilegeSetBodyV1(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile current app ACL privileges: %w", err)
	}
	verifierInput, err := newAppACLEffectiveCatalogVerifierInput(contract, roles.Migrator)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build current app ACL catalog verifier input: %w", err)
	}
	phase, err := dependencies.readPhaseState(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}

	fresh := !phase.LedgerExists && !phase.ManifestRevisionsExists && !phase.ManifestHeadExists
	exactCandidate := phase.LedgerExists && phase.ManifestRevisionsExists && phase.ManifestHeadExists
	switch {
	case fresh:
		if err := dependencies.rejectMisplaced(ctx, tx, contract); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		if err := dependencies.rejectLegacy(ctx, tx, source.sources, contract, roles.Migrator); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		if err := dependencies.rejectFresh(ctx, tx, contract); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		return convergeFreshAppACLCurrentInTx(ctx, tx, source, compiledPrivileges, verifierInput, roles.Migrator, dependencies)
	case exactCandidate:
		return verifyExactAppACLCurrentInTx(ctx, tx, source, transitions, compiledPrivileges, verifierInput, roles.Migrator, dependencies)
	default:
		return AppACLManifestPersistedV1{}, appACLDevelopmentDatabaseRebuildError("APP ledger and manifest tables do not form a current baseline")
	}
}

func verifyExactAppACLCurrentInTx(
	ctx context.Context,
	tx pgx.Tx,
	source appACLCurrentSourceContract,
	transitions []appACLCurrentTransition,
	compiledPrivileges []byte,
	verifierInput appACLEffectiveCatalogVerifierInput,
	migratorRole string,
	dependencies appACLCurrentConvergenceDependencies,
) (AppACLManifestPersistedV1, error) {
	head, err := dependencies.readHead(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if head == nil {
		return AppACLManifestPersistedV1{}, appACLDevelopmentDatabaseRebuildError("APP manifest head is null")
	}
	if err := dependencies.lockLedger(ctx, tx); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	applied, err := dependencies.readApplied(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	manifests, err := dependencies.readManifests(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	shape, err := classifyAppACLCurrentManifestShape(
		source,
		transitions,
		applied,
		manifests,
		head,
		compiledPrivileges,
		migratorRole,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := dependencies.rejectMisplaced(ctx, tx, verifierInput.Contract); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := dependencies.rejectLegacy(ctx, tx, source.sources, verifierInput.Contract, migratorRole); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := verifyAppACLCurrentConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if shape.kind == appACLCurrentManifestShapeSuccessor {
		if err := dependencies.verifyTransitionCurrent(ctx, tx, *shape.transition); err != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("verify current registered APP transition: %w", err)
		}
		return shape.latest, nil
	}
	if shape.kind == appACLCurrentManifestShapeGenesis {
		return shape.latest, nil
	}

	before, err := dependencies.preflightTransition(ctx, tx, *shape.transition)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("preflight registered APP transition: %w", err)
	}
	if err := dependencies.applyPending(ctx, tx, source.sources, applied); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("apply registered APP transition migrations: %w", err)
	}
	applied, err = dependencies.readApplied(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := compareAppACLCurrentMigrationEntries(source.sources.canonicalSet, applied, "registered successor migration ledger"); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := dependencies.verifyTransitionApplied(ctx, tx, *shape.transition, before); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("verify applied registered APP transition: %w", err)
	}
	if err := verifyAppACLCurrentConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	inserted, err := dependencies.insertSuccessor(
		ctx,
		tx,
		shape.latest,
		source.sources.canonicalSet,
		compiledPrivileges,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("insert registered APP manifest successor: %w", err)
	}
	head, err = dependencies.readHeadForUpdate(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	manifests, err = dependencies.readManifests(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	finalShape, err := classifyAppACLCurrentManifestShape(
		source,
		transitions,
		applied,
		manifests,
		head,
		compiledPrivileges,
		migratorRole,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read back registered APP successor: %w", err)
	}
	if finalShape.kind != appACLCurrentManifestShapeSuccessor || finalShape.latest.ManifestDigest != inserted.ManifestDigest {
		return AppACLManifestPersistedV1{}, fmt.Errorf("registered APP successor readback does not match inserted revision")
	}
	if err := dependencies.verifyTransitionCurrent(ctx, tx, *shape.transition); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("verify final registered APP transition: %w", err)
	}
	if err := verifyAppACLCurrentConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	return finalShape.latest, nil
}

func convergeFreshAppACLCurrentInTx(
	ctx context.Context,
	tx pgx.Tx,
	source appACLCurrentSourceContract,
	compiledPrivileges []byte,
	verifierInput appACLEffectiveCatalogVerifierInput,
	migratorRole string,
	dependencies appACLCurrentConvergenceDependencies,
) (AppACLManifestPersistedV1, error) {
	if err := dependencies.ensureLedger(ctx, tx, source.sources.sources); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	applied, err := dependencies.readApplied(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if len(applied) != 0 {
		return AppACLManifestPersistedV1{}, appACLDevelopmentDatabaseRebuildError("fresh APP ledger is not empty")
	}
	if err := dependencies.applyPending(ctx, tx, source.sources, applied); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	applied, err = dependencies.readApplied(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := compareAppACLCurrentMigrationEntries(source.sources.canonicalSet, applied, "fresh applied migration ledger"); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	head, err := dependencies.readHeadForUpdate(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	manifests, err := dependencies.readManifests(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if head != nil || len(manifests) != 0 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("fresh current app ACL manifest state is not empty")
	}
	if err := dependencies.applyDCL(ctx, tx, verifierInput.Contract); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := verifyAppACLCurrentConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	inserted, err := dependencies.insertGenesis(ctx, tx, source.sources.canonicalSet, compiledPrivileges, migratorRole)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	head, err = dependencies.readHeadForUpdate(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	manifests, err = dependencies.readManifests(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	persisted, err := checkAppACLManifestGenesisStateV1(
		manifests,
		head,
		source.sources.canonicalSet,
		compiledPrivileges,
		migratorRole,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if persisted == nil || persisted.ManifestDigest != inserted.ManifestDigest {
		return AppACLManifestPersistedV1{}, fmt.Errorf("current app ACL manifest genesis did not persist its inserted revision")
	}
	if err := verifyAppACLCurrentConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	return *persisted, nil
}

func compareAppACLCurrentMigrationEntries(
	expectedBody []byte,
	actual []MigrationChecksumEntry,
	observedState string,
) error {
	expected, err := ParseCanonicalMigrationSetBodyV1(expectedBody)
	if err != nil {
		return fmt.Errorf("parse expected current migration set: %w", err)
	}
	if len(actual) != len(expected) {
		return appACLDevelopmentDatabaseRebuildError("%s contains %d migrations, current build requires %d", observedState, len(actual), len(expected))
	}
	for index := range expected {
		if actual[index].Filename != expected[index].Filename {
			return appACLDevelopmentDatabaseRebuildError("%s migration %d is %q, current build requires %q", observedState, index+1, actual[index].Filename, expected[index].Filename)
		}
		if actual[index].Checksum != expected[index].Checksum {
			return appACLDevelopmentDatabaseRebuildError("%s checksum mismatch for %q", observedState, expected[index].Filename)
		}
	}
	return nil
}

func verifyAppACLCurrentConvergenceCatalog(
	ctx context.Context,
	tx pgx.Tx,
	input appACLEffectiveCatalogVerifierInput,
	dependencies appACLCurrentConvergenceDependencies,
) error {
	snapshot, err := dependencies.readCatalog(ctx, tx, input)
	if err != nil {
		return fmt.Errorf("read current app ACL convergence catalog snapshot: %w", err)
	}
	if err := dependencies.verifyCatalog(snapshot, input); err != nil {
		return fmt.Errorf("verify current app ACL convergence catalog snapshot: %w", err)
	}
	return nil
}

func applyAppACLConvergenceDCLForContractInTx(
	ctx context.Context,
	tx pgx.Tx,
	contract appACLEffectiveCatalogContract,
) error {
	if tx == nil {
		return fmt.Errorf("apply current app ACL convergence DCL has no PostgreSQL transaction")
	}
	statements, err := appACLConvergenceDCLStatementsForContract(contract)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply current app ACL convergence DCL: %w", err)
		}
	}
	return nil
}

func appACLDevelopmentDatabaseRebuildError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrDevelopmentDatabaseRebuildRequired, fmt.Sprintf(format, arguments...))
}

func (dependencies appACLCurrentConvergenceDependencies) validate() error {
	checks := []struct {
		name    string
		missing bool
	}{
		{name: "database reader", missing: dependencies.readDatabaseName == nil},
		{name: "role resolver", missing: dependencies.resolveRoles == nil},
		{name: "managed-object placement detector", missing: dependencies.rejectMisplaced == nil},
		{name: "non-public legacy detector", missing: dependencies.rejectLegacy == nil},
		{name: "phase-state reader", missing: dependencies.readPhaseState == nil},
		{name: "ledger locker", missing: dependencies.lockLedger == nil},
		{name: "fresh-state detector", missing: dependencies.rejectFresh == nil},
		{name: "ledger helper", missing: dependencies.ensureLedger == nil},
		{name: "applied migration reader", missing: dependencies.readApplied == nil},
		{name: "pending migration applier", missing: dependencies.applyPending == nil},
		{name: "manifest revisions reader", missing: dependencies.readManifests == nil},
		{name: "manifest head reader", missing: dependencies.readHead == nil},
		{name: "manifest head locker", missing: dependencies.readHeadForUpdate == nil},
		{name: "DCL applier", missing: dependencies.applyDCL == nil},
		{name: "catalog reader", missing: dependencies.readCatalog == nil},
		{name: "catalog verifier", missing: dependencies.verifyCatalog == nil},
		{name: "manifest genesis inserter", missing: dependencies.insertGenesis == nil},
		{name: "transition preflight", missing: dependencies.preflightTransition == nil},
		{name: "applied transition verifier", missing: dependencies.verifyTransitionApplied == nil},
		{name: "current transition verifier", missing: dependencies.verifyTransitionCurrent == nil},
		{name: "manifest successor inserter", missing: dependencies.insertSuccessor == nil},
	}
	for _, check := range checks {
		if check.missing {
			return fmt.Errorf("current app ACL convergence %s is nil", check.name)
		}
	}
	return nil
}

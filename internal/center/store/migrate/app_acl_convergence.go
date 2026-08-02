package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

const (
	appACLConvergenceMaxAttempts = 3

	appACLConvergenceHardenedSearchPathSQL = `set local search_path = pg_catalog, public`
	// appACLLegacyMigrationSearchPathSQL is a narrow exception for one trusted
	// embedded legacy source: its unqualified DDL must target public, never
	// the caller's $user schema, while PostgreSQL retains implicit pg_catalog
	// lookup precedence. Each source immediately restores the hardened path
	// before the next source or any manifest, DCL, or catalog work.
	appACLLegacyMigrationSearchPathSQL = `set local search_path = public`
)

type appACLConvergenceBeginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)

type appACLConvergenceDependencies struct {
	readDatabaseName  func(context.Context, pgx.Tx) (string, error)
	resolveRoles      func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error)
	rejectMisplaced   func(context.Context, pgx.Tx, string) error
	readPhaseState    func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error)
	lockLedger        func(context.Context, pgx.Tx) error
	rejectLegacy      func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error
	rejectFresh       func(context.Context, pgx.Tx, string) error
	ensureLedger      func(context.Context, pgx.Tx, map[string]migrationSource) error
	readApplied       func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error)
	applyPending      func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error
	readManifests     func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error)
	readHead          func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error)
	readHeadForUpdate func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error)
	applyDCL          func(context.Context, pgx.Tx, AppACLEffectiveCatalogContractR1) error
	readCatalog       func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error)
	verifyCatalog     func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error
	insertGenesis     func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error)
}

func defaultAppACLConvergenceDependencies() appACLConvergenceDependencies {
	return appACLConvergenceDependencies{
		readDatabaseName:  readAppACLConvergenceDatabaseName,
		resolveRoles:      resolveAppACLConvergenceRoles,
		rejectMisplaced:   rejectMisplacedAppACLManagedObjectInTx,
		readPhaseState:    readAppACLConvergencePhaseStateInTx,
		lockLedger:        lockAppACLConvergenceLedgerInTx,
		rejectLegacy:      rejectNonPublicAppACLLegacyLedgerInTx,
		rejectFresh:       rejectFreshAppACLManagedStateInTx,
		ensureLedger:      ensureMigrationLedgerInTx,
		readApplied:       readAppliedAppMigrationsV1,
		applyPending:      applyPendingMigrationSourcesInTx,
		readManifests:     readAppACLManifestRevisionsV1,
		readHead:          readAppACLManifestHeadV1,
		readHeadForUpdate: readAppACLManifestHeadForUpdateV1,
		applyDCL:          applyAppACLConvergenceDCLInTx,
		readCatalog:       readAppACLEffectiveCatalogSnapshotInTxR1,
		verifyCatalog:     VerifyAppACLEffectiveCatalogSnapshotR1,
		insertGenesis:     insertAppACLManifestGenesisV1,
	}
}

// ConvergeAppACLR1 is the only APP-scope writer. It snapshots the embedded
// sources before retries, then runs the complete migration, ACL, catalog, and
// manifest closure on a direct migrator's SERIALIZABLE transaction.
func ConvergeAppACLR1(
	ctx context.Context,
	db *pgxpool.Pool,
	runtimeRole string,
	adminRole string,
) (AppACLManifestPersistedV1, error) {
	if db == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL convergence has no PostgreSQL pool")
	}
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("snapshot embedded application migrations: %w", err)
	}
	return convergeAppACLR1WithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return db.BeginTx(ctx, options)
		},
		runtimeRole,
		adminRole,
		sources,
		defaultAppACLConvergenceDependencies(),
	)
}

func convergeAppACLR1WithDependencies(
	ctx context.Context,
	begin appACLConvergenceBeginTx,
	runtimeRole string,
	adminRole string,
	sources migrationSourceSnapshot,
	dependencies appACLConvergenceDependencies,
) (AppACLManifestPersistedV1, error) {
	if begin == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL convergence transaction opener is nil")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := validateAppACLR1FrozenSourceSnapshot(sources); err != nil {
		return AppACLManifestPersistedV1{}, err
	}

	var result AppACLManifestPersistedV1
	err := retryAppACLConvergence(ctx, func() error {
		tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return fmt.Errorf("begin app ACL convergence transaction: %w", err)
		}
		defer func() {
			_ = tx.Rollback(ctx)
		}()

		manifest, err := convergeAppACLR1InTx(ctx, tx, runtimeRole, adminRole, sources, dependencies)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit app ACL convergence transaction: %w", err)
		}
		result = manifest
		return nil
	})
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	return result, nil
}

func convergeAppACLR1InTx(
	ctx context.Context,
	tx pgx.Tx,
	runtimeRole string,
	adminRole string,
	sources migrationSourceSnapshot,
	dependencies appACLConvergenceDependencies,
) (AppACLManifestPersistedV1, error) {
	if tx == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL convergence has no PostgreSQL transaction")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if _, err := tx.Exec(ctx, appACLConvergenceHardenedSearchPathSQL); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("set app ACL convergence search path: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, appACLSchemaAdvisoryLockV1); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("lock app ACL convergence: %w", err)
	}

	roles, err := dependencies.resolveRoles(ctx, tx, runtimeRole, adminRole)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	databaseName, err := dependencies.readDatabaseName(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := dependencies.rejectMisplaced(ctx, tx, databaseName); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := dependencies.rejectLegacy(ctx, tx, sources, databaseName, roles.Migrator); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	phaseState, err := dependencies.readPhaseState(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if phaseState.ManifestRevisionsExists != phaseState.ManifestHeadExists {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest tables are incomplete")
	}
	if !phaseState.ManifestHeadExists {
		if phaseState.LedgerExists {
			return AppACLManifestPersistedV1{}, fmt.Errorf("pre-existing public migration ledger without app ACL manifest tables is not fresh")
		}
		if err := dependencies.rejectFresh(ctx, tx, databaseName); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
	}

	var phaseHead *AppACLManifestHeadV1
	if phaseState.ManifestHeadExists {
		phaseHead, err = dependencies.readHead(ctx, tx)
		if err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		if phaseHead != nil {
			if !phaseState.LedgerExists {
				return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest head requires a public migration ledger")
			}
			if err := dependencies.lockLedger(ctx, tx); err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			applied, err := dependencies.readApplied(ctx, tx)
			if err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			pending, err := pendingMigrationSourceNames(sources, applied)
			if err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			if len(pending) != 0 {
				return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest head requires a complete public migration ledger")
			}
		} else {
			if !phaseState.LedgerExists {
				return AppACLManifestPersistedV1{}, fmt.Errorf("null app ACL manifest head requires a public migration ledger")
			}
			if err := dependencies.ensureLedger(ctx, tx, sources.sources); err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			applied, err := dependencies.readApplied(ctx, tx)
			if err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			pending, err := pendingMigrationSourceNames(sources, applied)
			if err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			if len(pending) != 0 {
				return AppACLManifestPersistedV1{}, fmt.Errorf("null app ACL manifest head adoption requires a complete migration ledger")
			}
		}
	} else {
		if err := dependencies.ensureLedger(ctx, tx, sources.sources); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		applied, err := dependencies.readApplied(ctx, tx)
		if err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		if err := dependencies.applyPending(ctx, tx, sources, applied); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
	}

	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: roles.CenterRuntime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: roles.PlatformAdmin},
	}
	contract, err := CompileAppACLEffectiveCatalogContractR1(databaseName, bindings)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile app ACL convergence catalog contract: %w", err)
	}
	compiledPrivileges, err := CompileAppACLPrivilegeSetR1(databaseName, bindings)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compile app ACL convergence privileges: %w", err)
	}
	verifierInput, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, roles.Migrator)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build app ACL convergence catalog verifier input: %w", err)
	}

	if _, err := tx.Exec(ctx, `lock table public.app_acl_manifest_revisions in share row exclusive mode`); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("lock app ACL manifest revisions: %w", err)
	}
	head, err := dependencies.readHeadForUpdate(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if !appACLManifestHeadsEqual(phaseHead, head) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest head changed after phase inspection")
	}
	manifests, err := dependencies.readManifests(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	existing, err := checkAppACLManifestGenesisStateV1(
		manifests,
		head,
		sources.canonicalSet,
		compiledPrivileges,
		roles.Migrator,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}

	if existing != nil {
		if err := verifyAppACLConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		return *existing, nil
	}
	if err := dependencies.applyDCL(ctx, tx, contract); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := verifyAppACLConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	inserted, err := dependencies.insertGenesis(ctx, tx, sources.canonicalSet, compiledPrivileges, roles.Migrator)
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
		sources.canonicalSet,
		compiledPrivileges,
		roles.Migrator,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if persisted == nil || persisted.ManifestDigest != inserted.ManifestDigest {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest genesis did not persist its inserted revision")
	}
	if err := verifyAppACLConvergenceCatalog(ctx, tx, verifierInput, dependencies); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	return *persisted, nil
}

func appACLManifestHeadsEqual(left *AppACLManifestHeadV1, right *AppACLManifestHeadV1) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ManifestRevision == right.ManifestRevision && left.ManifestDigest == right.ManifestDigest
}

func (dependencies appACLConvergenceDependencies) validate() error {
	if dependencies.readDatabaseName == nil {
		return fmt.Errorf("app ACL convergence database reader is nil")
	}
	if dependencies.resolveRoles == nil {
		return fmt.Errorf("app ACL convergence role resolver is nil")
	}
	if dependencies.rejectMisplaced == nil {
		return fmt.Errorf("app ACL convergence managed-object placement detector is nil")
	}
	if dependencies.readPhaseState == nil {
		return fmt.Errorf("app ACL convergence phase-state reader is nil")
	}
	if dependencies.lockLedger == nil {
		return fmt.Errorf("app ACL convergence ledger locker is nil")
	}
	if dependencies.rejectLegacy == nil {
		return fmt.Errorf("app ACL convergence non-public legacy detector is nil")
	}
	if dependencies.rejectFresh == nil {
		return fmt.Errorf("app ACL convergence fresh-state detector is nil")
	}
	if dependencies.ensureLedger == nil {
		return fmt.Errorf("app ACL convergence ledger helper is nil")
	}
	if dependencies.readApplied == nil {
		return fmt.Errorf("app ACL convergence applied migration reader is nil")
	}
	if dependencies.applyPending == nil {
		return fmt.Errorf("app ACL convergence pending migration applier is nil")
	}
	if dependencies.readManifests == nil {
		return fmt.Errorf("app ACL convergence manifest revisions reader is nil")
	}
	if dependencies.readHead == nil {
		return fmt.Errorf("app ACL convergence manifest head reader is nil")
	}
	if dependencies.readHeadForUpdate == nil {
		return fmt.Errorf("app ACL convergence manifest head locker is nil")
	}
	if dependencies.applyDCL == nil {
		return fmt.Errorf("app ACL convergence DCL applier is nil")
	}
	if dependencies.readCatalog == nil {
		return fmt.Errorf("app ACL convergence catalog reader is nil")
	}
	if dependencies.verifyCatalog == nil {
		return fmt.Errorf("app ACL convergence catalog verifier is nil")
	}
	if dependencies.insertGenesis == nil {
		return fmt.Errorf("app ACL convergence manifest genesis inserter is nil")
	}
	return nil
}

func readAppACLConvergenceDatabaseName(ctx context.Context, tx pgx.Tx) (string, error) {
	var databaseName string
	if err := tx.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&databaseName); err != nil {
		return "", fmt.Errorf("read app ACL convergence database name: %w", err)
	}
	return databaseName, nil
}

type appACLConvergencePhaseState struct {
	LedgerExists            bool
	ManifestRevisionsExists bool
	ManifestHeadExists      bool
}

func readAppACLConvergencePhaseStateInTx(ctx context.Context, tx pgx.Tx) (appACLConvergencePhaseState, error) {
	if tx == nil {
		return appACLConvergencePhaseState{}, fmt.Errorf("read app ACL convergence phase state has no PostgreSQL transaction")
	}
	var state appACLConvergencePhaseState
	if err := tx.QueryRow(ctx, `
		select pg_catalog.to_regclass('public.schema_migrations') is not null,
		       pg_catalog.to_regclass('public.app_acl_manifest_revisions') is not null,
		       pg_catalog.to_regclass('public.app_acl_manifest_head') is not null
	`).Scan(&state.LedgerExists, &state.ManifestRevisionsExists, &state.ManifestHeadExists); err != nil {
		return appACLConvergencePhaseState{}, fmt.Errorf("read app ACL convergence phase state: %w", err)
	}
	return state, nil
}

func lockAppACLConvergenceLedgerInTx(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return fmt.Errorf("lock app ACL convergence ledger has no PostgreSQL transaction")
	}
	if _, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`); err != nil {
		return fmt.Errorf("lock app ACL convergence ledger: %w", err)
	}
	return nil
}

type appACLNonPublicMigrationLedgerCandidate struct {
	SchemaName         string
	OwnerRole          string
	RowSecurityEnabled bool
	RowSecurityForced  bool
}

type appACLManagedPlacementInventory struct {
	relationNames   []string
	relationSchemas map[string]string
	functionNames   []string
	functionSchemas map[string]string
	managedSchemas  []string
	objectSchemas   []string
}

func compileAppACLManagedPlacementInventoryR1(databaseName string) (appACLManagedPlacementInventory, error) {
	surface, err := CompileAppACLManagedSurfaceR1(databaseName)
	if err != nil {
		return appACLManagedPlacementInventory{}, fmt.Errorf("compile app ACL managed surface for placement check: %w", err)
	}
	return compileAppACLManagedPlacementInventory(surface.Objects)
}

func compileAppACLManagedPlacementInventory(
	objects []AppACLManagedObjectR1,
) (appACLManagedPlacementInventory, error) {
	inventory := appACLManagedPlacementInventory{
		relationSchemas: make(map[string]string),
		functionSchemas: make(map[string]string),
	}
	managedSchemas := make(map[string]struct{})
	objectSchemas := make(map[string]struct{})
	for _, object := range objects {
		if err := validateAppACLManagedObject(object); err != nil {
			return appACLManagedPlacementInventory{}, fmt.Errorf("compile app ACL managed placement inventory: %w", err)
		}
		switch object.ObjectClass {
		case AppACLObjectClassDatabase:
		case AppACLObjectClassSchema:
			if _, exists := managedSchemas[object.SchemaName]; exists {
				continue
			}
			managedSchemas[object.SchemaName] = struct{}{}
			inventory.managedSchemas = append(inventory.managedSchemas, object.SchemaName)
		case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence:
			if _, exists := objectSchemas[object.SchemaName]; !exists {
				objectSchemas[object.SchemaName] = struct{}{}
				inventory.objectSchemas = append(inventory.objectSchemas, object.SchemaName)
			}
			// schema_migrations is a common shared-database name. Its placement and
			// contents remain governed by the dedicated ledger preflight.
			if object.ObjectClass == AppACLObjectClassTable && object.ObjectIdentity == "schema_migrations" {
				continue
			}
			if expectedSchema, exists := inventory.relationSchemas[object.ObjectIdentity]; exists {
				if expectedSchema != object.SchemaName {
					return appACLManagedPlacementInventory{}, fmt.Errorf("managed relation %q has conflicting expected schemas %q and %q", object.ObjectIdentity, expectedSchema, object.SchemaName)
				}
				continue
			}
			inventory.relationSchemas[object.ObjectIdentity] = object.SchemaName
			inventory.relationNames = append(inventory.relationNames, object.ObjectIdentity)
		case AppACLObjectClassFunction:
			if _, exists := objectSchemas[object.SchemaName]; !exists {
				objectSchemas[object.SchemaName] = struct{}{}
				inventory.objectSchemas = append(inventory.objectSchemas, object.SchemaName)
			}
			name, _, found := strings.Cut(object.ObjectIdentity, "(")
			if !found || name == "" {
				return appACLManagedPlacementInventory{}, fmt.Errorf("managed function identity %q has no name", object.ObjectIdentity)
			}
			if expectedSchema, exists := inventory.functionSchemas[name]; exists {
				if expectedSchema != object.SchemaName {
					return appACLManagedPlacementInventory{}, fmt.Errorf("managed function %q has conflicting expected schemas %q and %q", name, expectedSchema, object.SchemaName)
				}
				continue
			}
			inventory.functionSchemas[name] = object.SchemaName
			inventory.functionNames = append(inventory.functionNames, name)
		}
	}
	return inventory, nil
}

// rejectMisplacedAppACLManagedObjectInTx runs before phase classification in
// every convergence mode. It admits a fixed managed name only in the schema
// the frozen r1 managed surface assigns to it.
func rejectMisplacedAppACLManagedObjectInTx(ctx context.Context, tx pgx.Tx, databaseName string) error {
	if tx == nil {
		return fmt.Errorf("detect misplaced app ACL managed object has no PostgreSQL transaction")
	}
	inventory, err := compileAppACLManagedPlacementInventoryR1(databaseName)
	if err != nil {
		return err
	}
	return rejectMisplacedAppACLManagedObjectWithInventoryInTx(ctx, tx, inventory)
}

func rejectMisplacedAppACLManagedObjectForContractInTx(
	ctx context.Context,
	tx pgx.Tx,
	contract appACLEffectiveCatalogContract,
) error {
	if tx == nil {
		return fmt.Errorf("detect misplaced current app ACL managed object has no PostgreSQL transaction")
	}
	inventory, err := compileAppACLManagedPlacementInventory(contract.ManagedObjects)
	if err != nil {
		return err
	}
	return rejectMisplacedAppACLManagedObjectWithInventoryInTx(ctx, tx, inventory)
}

func rejectMisplacedAppACLManagedObjectWithInventoryInTx(
	ctx context.Context,
	tx pgx.Tx,
	inventory appACLManagedPlacementInventory,
) error {

	relationRows, err := tx.Query(ctx, `
		select namespace.nspname,
		       relation.relkind::text,
		       relation.relname::text
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where relation.relkind in ('r', 'p', 'v', 'm', 'S', 'f')
		  and relation.relname = any($1::text[])
		  and namespace.nspname <> 'information_schema'
		  and namespace.nspname !~ '^pg_'
		  and namespace.oid <> pg_catalog.pg_my_temp_schema()
		  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		order by namespace.nspname::text collate "C", relation.relkind, relation.relname::text collate "C"
	`, inventory.relationNames)
	if err != nil {
		return fmt.Errorf("read app ACL managed relation placements: %w", err)
	}
	defer relationRows.Close()
	for relationRows.Next() {
		var schemaName, relationKind, relationName string
		if err := relationRows.Scan(&schemaName, &relationKind, &relationName); err != nil {
			return fmt.Errorf("scan app ACL managed relation placement: %w", err)
		}
		expectedSchema := inventory.relationSchemas[relationName]
		if schemaName != expectedSchema {
			return appACLManagedPlacementError(relationKind, schemaName, relationName, expectedSchema)
		}
	}
	if err := relationRows.Err(); err != nil {
		return fmt.Errorf("iterate app ACL managed relation placements: %w", err)
	}

	functionRows, err := tx.Query(ctx, `
		select namespace.nspname,
		       procedure.proname::text
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where procedure.proname = any($1::text[])
		  and namespace.nspname <> 'information_schema'
		  and namespace.nspname !~ '^pg_'
		  and namespace.oid <> pg_catalog.pg_my_temp_schema()
		  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		order by namespace.nspname::text collate "C", procedure.proname::text collate "C"
	`, inventory.functionNames)
	if err != nil {
		return fmt.Errorf("read app ACL managed function placements: %w", err)
	}
	defer functionRows.Close()
	for functionRows.Next() {
		var schemaName, functionName string
		if err := functionRows.Scan(&schemaName, &functionName); err != nil {
			return fmt.Errorf("scan app ACL managed function placement: %w", err)
		}
		expectedSchema := inventory.functionSchemas[functionName]
		if schemaName != expectedSchema {
			return appACLManagedPlacementError("function", schemaName, functionName, expectedSchema)
		}
	}
	if err := functionRows.Err(); err != nil {
		return fmt.Errorf("iterate app ACL managed function placements: %w", err)
	}
	return nil
}

func appACLManagedPlacementError(objectClass string, schemaName string, objectName string, expectedSchema string) error {
	object := pgx.Identifier{schemaName, objectName}.Sanitize()
	if schemaName != appACLManagedPublicSchemaR1 {
		return fmt.Errorf("app ACL convergence cannot adopt non-public managed object %s %s; expected schema %q", objectClass, object, expectedSchema)
	}
	return fmt.Errorf("app ACL convergence cannot adopt managed object %s %s in schema %q, want %q", objectClass, object, schemaName, expectedSchema)
}

// rejectFreshAppACLManagedStateInTx proves that the fresh branch has no
// pre-existing managed surface to adopt. An eligible legacy adoption has both
// public manifest tables and an exact public ledger, so it does not enter this
// branch.
func rejectFreshAppACLManagedStateInTx(ctx context.Context, tx pgx.Tx, databaseName string) error {
	if tx == nil {
		return fmt.Errorf("detect fresh app ACL managed state has no PostgreSQL transaction")
	}
	inventory, err := compileAppACLManagedPlacementInventoryR1(databaseName)
	if err != nil {
		return err
	}
	return rejectFreshAppACLManagedStateWithInventoryInTx(ctx, tx, inventory)
}

func rejectFreshAppACLManagedStateForContractInTx(
	ctx context.Context,
	tx pgx.Tx,
	contract appACLEffectiveCatalogContract,
) error {
	if tx == nil {
		return fmt.Errorf("detect fresh current app ACL managed state has no PostgreSQL transaction")
	}
	inventory, err := compileAppACLManagedPlacementInventory(contract.ManagedObjects)
	if err != nil {
		return err
	}
	return rejectFreshAppACLManagedStateWithInventoryInTx(ctx, tx, inventory)
}

func rejectFreshAppACLManagedStateWithInventoryInTx(
	ctx context.Context,
	tx pgx.Tx,
	inventory appACLManagedPlacementInventory,
) error {
	for _, schemaName := range inventory.managedSchemas {
		if schemaName == appACLManagedPublicSchemaR1 {
			continue
		}

		var schemaExists bool
		if err := tx.QueryRow(ctx, `
		select exists (
			select 1
			from pg_catalog.pg_namespace
			where nspname = $1
		)
	`, schemaName).Scan(&schemaExists); err != nil {
			return fmt.Errorf("read fresh app ACL managed schema %q state: %w", schemaName, err)
		}
		if schemaExists {
			if schemaName == appACLManagedInternalSchemaR1 {
				return fmt.Errorf("fresh app ACL convergence cannot adopt existing managed internal schema %q", schemaName)
			}
			return fmt.Errorf("fresh app ACL convergence cannot adopt existing managed schema %q", schemaName)
		}
	}

	for _, schemaName := range inventory.objectSchemas {
		object, err := describeAppACLManagedObjectInSchemaWithInventoryInTx(ctx, tx, schemaName, inventory)
		if err != nil {
			return err
		}
		if object == "" {
			continue
		}
		if schemaName == appACLManagedPublicSchemaR1 {
			return fmt.Errorf("fresh app ACL convergence cannot adopt managed public object %s without public-ledger/manifest adoption state", object)
		}
		return fmt.Errorf("fresh app ACL convergence cannot adopt managed object %s without public-ledger/manifest adoption state", object)
	}
	return nil
}

// rejectNonPublicAppACLLegacyLedgerInTx recognizes only a legacy-shaped
// non-public ledger that contains an embedded APP migration name or a fixed
// APP managed object in the same schema. A foreign-owned ledger is allowed to
// remain private only when it is unreadable and has no managed-object evidence.
// A direct-migrator-owned forced-RLS ledger is rejected from pg_class facts,
// because its zero visible rows do not prove it is empty. If the direct
// migrator can read an embedded migration name, it is unsafe legacy APP state
// regardless of its current owner.
func rejectNonPublicAppACLLegacyLedgerInTx(
	ctx context.Context,
	tx pgx.Tx,
	sources migrationSourceSnapshot,
	databaseName string,
	migratorRole string,
) error {
	if tx == nil {
		return fmt.Errorf("detect non-public app migration ledger has no PostgreSQL transaction")
	}
	inventory, err := compileAppACLManagedPlacementInventoryR1(databaseName)
	if err != nil {
		return err
	}
	return rejectNonPublicAppACLLegacyLedgerWithInventoryInTx(ctx, tx, sources, inventory, migratorRole)
}

func rejectNonPublicAppACLLegacyLedgerForContractInTx(
	ctx context.Context,
	tx pgx.Tx,
	sources migrationSourceSnapshot,
	contract appACLEffectiveCatalogContract,
	migratorRole string,
) error {
	if tx == nil {
		return fmt.Errorf("detect non-public current app migration ledger has no PostgreSQL transaction")
	}
	inventory, err := compileAppACLManagedPlacementInventory(contract.ManagedObjects)
	if err != nil {
		return err
	}
	return rejectNonPublicAppACLLegacyLedgerWithInventoryInTx(ctx, tx, sources, inventory, migratorRole)
}

func rejectNonPublicAppACLLegacyLedgerWithInventoryInTx(
	ctx context.Context,
	tx pgx.Tx,
	sources migrationSourceSnapshot,
	inventory appACLManagedPlacementInventory,
	migratorRole string,
) error {
	candidates, err := readAppACLNonPublicMigrationLedgerCandidatesInTx(ctx, tx)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if candidate.OwnerRole == migratorRole && candidate.RowSecurityEnabled && candidate.RowSecurityForced {
			return fmt.Errorf("non-public application migration ledger in schema %q is owned by direct migrator role %q with forced row security", candidate.SchemaName, migratorRole)
		}
		object, err := describeAppACLManagedObjectInSchemaWithInventoryInTx(ctx, tx, candidate.SchemaName, inventory)
		if err != nil {
			return err
		}
		if candidate.SchemaName == appACLManagedInternalSchemaR1 {
			// The internal schema is wholly managed. Unlike an unrelated shared
			// schema, an extra ledger there is itself unsafe APP state even when
			// its row contents cannot be read by this direct migrator.
			return fmt.Errorf("non-public application migration ledger in managed internal schema %q", candidate.SchemaName)
		}
		if object != "" {
			if candidate.OwnerRole != migratorRole {
				return fmt.Errorf("non-public application migration ledger in schema %q is owned by %q, want direct migrator role %q", candidate.SchemaName, candidate.OwnerRole, migratorRole)
			}
			return fmt.Errorf("non-public application migration ledger in schema %q contains managed object %s", candidate.SchemaName, object)
		}
		embeddedName, err := readEmbeddedMigrationNameFromNonPublicLedgerInTx(ctx, tx, candidate.SchemaName, sources)
		if err != nil {
			if candidate.OwnerRole != migratorRole && isAppACLNonPublicLedgerProbePermissionDenied(err) {
				// An unreadable private ledger without a managed companion object is
				// outside the fixed APP surface and must not block convergence.
				continue
			}
			return err
		}
		if embeddedName == "" {
			continue
		}
		if candidate.OwnerRole != migratorRole {
			return fmt.Errorf("non-public application migration ledger in schema %q is owned by %q, want direct migrator role %q", candidate.SchemaName, candidate.OwnerRole, migratorRole)
		}
		return fmt.Errorf("non-public application migration ledger in schema %q contains embedded migration %q", candidate.SchemaName, embeddedName)
	}
	return nil
}

// appACLNonPublicLedgerProbeError marks an original ledger probe error only
// after its savepoint has been rolled back and released successfully. Cleanup
// errors intentionally do not receive this marker and must fail closed.
type appACLNonPublicLedgerProbeError struct {
	cause error
}

func (err *appACLNonPublicLedgerProbeError) Error() string {
	return err.cause.Error()
}

func (err *appACLNonPublicLedgerProbeError) Unwrap() error {
	return err.cause
}

func readEmbeddedMigrationNameFromNonPublicLedgerInTx(
	ctx context.Context,
	tx pgx.Tx,
	schemaName string,
	sources migrationSourceSnapshot,
) (string, error) {
	const ledgerProbeSavepoint = "app_acl_non_public_ledger_probe"
	if _, err := tx.Exec(ctx, `savepoint `+ledgerProbeSavepoint); err != nil {
		return "", fmt.Errorf("save non-public migration ledger probe in schema %q: %w", schemaName, err)
	}
	embeddedName, err := readEmbeddedMigrationNameFromNonPublicLedgerWithoutSavepointInTx(ctx, tx, schemaName, sources)
	if err != nil {
		if _, rollbackErr := tx.Exec(ctx, `rollback to savepoint `+ledgerProbeSavepoint); rollbackErr != nil {
			return "", fmt.Errorf("rollback non-public migration ledger probe in schema %q: %w", schemaName, rollbackErr)
		}
		if _, releaseErr := tx.Exec(ctx, `release savepoint `+ledgerProbeSavepoint); releaseErr != nil {
			return "", fmt.Errorf("release non-public migration ledger probe in schema %q after rollback: %w", schemaName, releaseErr)
		}
		return "", &appACLNonPublicLedgerProbeError{cause: err}
	}
	if _, err := tx.Exec(ctx, `release savepoint `+ledgerProbeSavepoint); err != nil {
		return "", fmt.Errorf("release non-public migration ledger probe in schema %q: %w", schemaName, err)
	}
	return embeddedName, nil
}

func readEmbeddedMigrationNameFromNonPublicLedgerWithoutSavepointInTx(
	ctx context.Context,
	tx pgx.Tx,
	schemaName string,
	sources migrationSourceSnapshot,
) (string, error) {
	rows, err := tx.Query(ctx, `select name from `+pgx.Identifier{schemaName, "schema_migrations"}.Sanitize()+` order by name::text collate "C"`)
	if err != nil {
		return "", fmt.Errorf("read non-public migration ledger in schema %q: %w", schemaName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", fmt.Errorf("scan non-public migration ledger in schema %q: %w", schemaName, err)
		}
		if _, ok := sources.sources[name]; ok {
			return name, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate non-public migration ledger in schema %q: %w", schemaName, err)
	}
	return "", nil
}

func readAppACLNonPublicMigrationLedgerCandidatesInTx(ctx context.Context, tx pgx.Tx) ([]appACLNonPublicMigrationLedgerCandidate, error) {
	rows, err := tx.Query(ctx, `
		select namespace.nspname,
		       owner.rolname,
		       relation.relrowsecurity,
		       relation.relforcerowsecurity
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		join pg_catalog.pg_roles owner on owner.oid = relation.relowner
		where relation.relname = 'schema_migrations'
		  and relation.relkind in ('r', 'p')
		  and namespace.nspname not in ('public', 'information_schema')
		  and namespace.nspname !~ '^pg_'
		  and namespace.oid <> pg_catalog.pg_my_temp_schema()
		  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		  and exists (
			select 1
			from pg_catalog.pg_attribute attribute
			where attribute.attrelid = relation.oid
			  and attribute.attname = 'name'
			  and attribute.atttypid = 'pg_catalog.text'::pg_catalog.regtype
			  and attribute.attnum > 0
			  and not attribute.attisdropped
		  )
		order by namespace.nspname::text collate "C"
	`)
	if err != nil {
		return nil, fmt.Errorf("list non-public migration ledger candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]appACLNonPublicMigrationLedgerCandidate, 0)
	for rows.Next() {
		var candidate appACLNonPublicMigrationLedgerCandidate
		if err := rows.Scan(
			&candidate.SchemaName,
			&candidate.OwnerRole,
			&candidate.RowSecurityEnabled,
			&candidate.RowSecurityForced,
		); err != nil {
			return nil, fmt.Errorf("scan non-public migration ledger candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate non-public migration ledger candidates: %w", err)
	}
	return candidates, nil
}

func describeAppACLManagedObjectInSchemaInTx(ctx context.Context, tx pgx.Tx, databaseName string, schemaName string) (string, error) {
	inventory, err := compileAppACLManagedPlacementInventoryR1(databaseName)
	if err != nil {
		return "", err
	}
	return describeAppACLManagedObjectInSchemaWithInventoryInTx(ctx, tx, schemaName, inventory)
}

func describeAppACLManagedObjectInSchemaWithInventoryInTx(
	ctx context.Context,
	tx pgx.Tx,
	schemaName string,
	inventory appACLManagedPlacementInventory,
) (string, error) {
	rows, err := tx.Query(ctx, `
		select relation.relkind::text,
		       relation.relname::text
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = $1
		  and relation.relkind in ('r', 'p', 'v', 'm', 'S', 'f')
		  and relation.relname = any($2::text[])
		order by relation.relname::text collate "C"
		limit 1
	`, schemaName, inventory.relationNames)
	if err != nil {
		return "", fmt.Errorf("read non-public managed relation surface in schema %q: %w", schemaName, err)
	}
	defer rows.Close()
	if rows.Next() {
		var relationKind, name string
		if err := rows.Scan(&relationKind, &name); err != nil {
			return "", fmt.Errorf("scan non-public managed relation surface in schema %q: %w", schemaName, err)
		}
		return relationKind + " " + pgx.Identifier{schemaName, name}.Sanitize(), nil
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate non-public managed relation surface in schema %q: %w", schemaName, err)
	}

	functionRows, err := tx.Query(ctx, `
		select procedure.proname::text
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where namespace.nspname = $1
		  and procedure.proname = any($2::text[])
		order by procedure.proname::text collate "C"
		limit 1
	`, schemaName, inventory.functionNames)
	if err != nil {
		return "", fmt.Errorf("read non-public managed function surface in schema %q: %w", schemaName, err)
	}
	defer functionRows.Close()
	if functionRows.Next() {
		var name string
		if err := functionRows.Scan(&name); err != nil {
			return "", fmt.Errorf("scan non-public managed function surface in schema %q: %w", schemaName, err)
		}
		return "function " + pgx.Identifier{schemaName, name}.Sanitize(), nil
	}
	if err := functionRows.Err(); err != nil {
		return "", fmt.Errorf("iterate non-public managed function surface in schema %q: %w", schemaName, err)
	}
	return "", nil
}

func resolveAppACLConvergenceRoles(
	ctx context.Context,
	tx pgx.Tx,
	runtimeRole string,
	adminRole string,
) (platformmigrate.AppRoleSetV1, error) {
	var sessionUser, currentUser string
	if err := tx.QueryRow(ctx, `select session_user, current_user`).Scan(&sessionUser, &currentUser); err != nil {
		return platformmigrate.AppRoleSetV1{}, fmt.Errorf("read app ACL convergence identities: %w", err)
	}
	roles, err := directAppACLRoleSet(runtimeRole, adminRole, sessionUser, currentUser)
	if err != nil {
		return platformmigrate.AppRoleSetV1{}, err
	}
	if err := platformmigrate.ProvisionRolesInTx(ctx, tx, roles); err != nil {
		return platformmigrate.AppRoleSetV1{}, fmt.Errorf("preflight app ACL convergence roles: %w", err)
	}
	return roles, nil
}

func verifyAppACLConvergenceCatalog(
	ctx context.Context,
	tx pgx.Tx,
	input AppACLEffectiveCatalogVerifierInputR1,
	dependencies appACLConvergenceDependencies,
) error {
	snapshot, err := dependencies.readCatalog(ctx, tx, input)
	if err != nil {
		return fmt.Errorf("read app ACL convergence catalog snapshot: %w", err)
	}
	if err := dependencies.verifyCatalog(snapshot, input); err != nil {
		return fmt.Errorf("verify app ACL convergence catalog snapshot: %w", err)
	}
	return nil
}

// directAppACLRoleSet binds the caller's direct PostgreSQL identity to the
// migrator role. Runtime and administrator names are caller input; the
// migrator name is never read from a second configuration source.
func directAppACLRoleSet(
	runtimeRole string,
	adminRole string,
	sessionUser string,
	currentUser string,
) (platformmigrate.AppRoleSetV1, error) {
	if sessionUser != currentUser {
		return platformmigrate.AppRoleSetV1{}, fmt.Errorf("app ACL convergence session user %q does not match current user %q", sessionUser, currentUser)
	}
	roles := platformmigrate.AppRoleSetV1{
		CenterRuntime: runtimeRole,
		PlatformAdmin: adminRole,
		Migrator:      currentUser,
	}
	if err := roles.Validate(); err != nil {
		return platformmigrate.AppRoleSetV1{}, fmt.Errorf("validate app ACL convergence roles: %w", err)
	}
	return roles, nil
}

// retryAppACLConvergence retries the caller-supplied whole transaction
// closure, never an individual migration or DCL statement.
func retryAppACLConvergence(ctx context.Context, converge func() error) error {
	if converge == nil {
		return fmt.Errorf("app ACL convergence retry closure is nil")
	}
	var err error
	for attempt := 0; attempt < appACLConvergenceMaxAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = converge(); err == nil {
			return nil
		}
		if !isAppACLConvergenceRetryable(err) || attempt == appACLConvergenceMaxAttempts-1 {
			return err
		}
	}
	return err
}

func isAppACLConvergenceRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func isAppACLConvergencePermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func isAppACLNonPublicLedgerProbePermissionDenied(err error) bool {
	var probeErr *appACLNonPublicLedgerProbeError
	return errors.As(err, &probeErr) && isAppACLConvergencePermissionDenied(probeErr.cause)
}

// pendingMigrationSourceNames accepts only the exact lexical prefix of a
// source snapshot. A checksum mismatch, unknown name, or a ledger hole is
// unsafe historical state and therefore cannot be repaired by convergence.
func pendingMigrationSourceNames(
	snapshot migrationSourceSnapshot,
	applied []MigrationChecksumEntry,
) ([]string, error) {
	if len(snapshot.names) == 0 || len(snapshot.sources) != len(snapshot.names) {
		return nil, fmt.Errorf("migration source snapshot is incomplete")
	}
	if len(applied) > len(snapshot.names) {
		return nil, fmt.Errorf("applied migration ledger is not an exact prefix of embedded migrations")
	}
	for index, entry := range applied {
		wantName := snapshot.names[index]
		if entry.Filename != wantName {
			return nil, fmt.Errorf("applied migration ledger is not an exact prefix of embedded migrations at %q, want %q", entry.Filename, wantName)
		}
		source, ok := snapshot.sources[entry.Filename]
		if !ok {
			return nil, fmt.Errorf("applied migration ledger is not an exact prefix of embedded migrations at %q", entry.Filename)
		}
		if source.checksum != hexChecksum(entry.Checksum) {
			return nil, fmt.Errorf("migration checksum mismatch for %q", entry.Filename)
		}
	}
	return append([]string(nil), snapshot.names[len(applied):]...), nil
}

func hexChecksum(checksum [32]byte) string {
	const hexdigits = "0123456789abcdef"
	encoded := make([]byte, 64)
	for index, value := range checksum {
		encoded[index*2] = hexdigits[value>>4]
		encoded[index*2+1] = hexdigits[value&0x0f]
	}
	return string(encoded)
}

func applyPendingMigrationSourcesInTx(
	ctx context.Context,
	tx pgx.Tx,
	snapshot migrationSourceSnapshot,
	applied []MigrationChecksumEntry,
) error {
	if tx == nil {
		return fmt.Errorf("apply pending migrations has no PostgreSQL transaction")
	}
	pending, err := pendingMigrationSourceNames(snapshot, applied)
	if err != nil {
		return err
	}
	for _, name := range pending {
		source := snapshot.sources[name]
		if _, err := tx.Exec(ctx, appACLLegacyMigrationSearchPathSQL); err != nil {
			return fmt.Errorf("set public migration search path for %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx, source.sql); err != nil {
			return fmt.Errorf("apply migration %q in scoped transaction: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into public.schema_migrations (name, checksum) values ($1, $2)`, name, source.checksum); err != nil {
			return fmt.Errorf("record migration %q in scoped transaction: %w", name, err)
		}
		if _, err := tx.Exec(ctx, appACLConvergenceHardenedSearchPathSQL); err != nil {
			return fmt.Errorf("restore app ACL convergence search path after migration %q: %w", name, err)
		}
	}
	return nil
}

// appACLConvergenceDCLStatements renders the complete fixed-surface DCL
// convergence plan. It revokes every APP-accessible grantee from the managed
// surface, then restores only the frozen compiler tuples.
func appACLConvergenceDCLStatements(contract AppACLEffectiveCatalogContractR1) ([]string, error) {
	compiled, err := CompileAppACLEffectiveCatalogContractR1(contract.DatabaseName, contract.RoleBindings[:])
	if err != nil {
		return nil, fmt.Errorf("compile app ACL convergence contract: %w", err)
	}
	if compiled != contract {
		return nil, fmt.Errorf("app ACL convergence contract does not match compiler output")
	}
	surface, err := CompileAppACLManagedSurfaceR1(contract.DatabaseName)
	if err != nil {
		return nil, fmt.Errorf("compile app ACL convergence managed surface: %w", err)
	}
	return appACLConvergenceDCLStatementsForContract(appACLEffectiveCatalogContract{
		DatabaseName:   contract.DatabaseName,
		RoleBindings:   append([]AppACLRoleBinding(nil), contract.RoleBindings[:]...),
		Privileges:     append([]AppACLPrivilege(nil), contract.Privileges[:]...),
		ManagedObjects: append([]AppACLManagedObjectR1(nil), surface.Objects...),
	})
}

func appACLConvergenceDCLStatementsForContract(contract appACLEffectiveCatalogContract) ([]string, error) {
	canonicalBody, err := CanonicalPrivilegeSetBodyV1(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return nil, fmt.Errorf("canonicalize app ACL convergence contract: %w", err)
	}
	canonicalSet, err := ParseCanonicalPrivilegeSetBodyV1(canonicalBody)
	if err != nil {
		return nil, fmt.Errorf("parse app ACL convergence contract: %w", err)
	}
	managedObjects, err := canonicalAppACLManagedObjects(contract.ManagedObjects)
	if err != nil {
		return nil, fmt.Errorf("canonicalize app ACL convergence managed surface: %w", err)
	}
	managed := make(map[AppACLManagedObjectR1]struct{}, len(managedObjects))
	for _, object := range managedObjects {
		managed[object] = struct{}{}
	}
	for _, privilege := range canonicalSet.Privileges {
		object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
		if err != nil {
			return nil, fmt.Errorf("map app ACL convergence privilege: %w", err)
		}
		if _, ok := managed[object]; !ok {
			return nil, fmt.Errorf("app ACL convergence privilege references unmanaged object %#v", object)
		}
	}

	rolesBySubject := make(map[AppACLSubject]string, len(canonicalSet.RoleBindings))
	grantees := make([]string, 0, len(canonicalSet.RoleBindings)+1)
	grantees = append(grantees, "PUBLIC")
	for _, binding := range canonicalSet.RoleBindings {
		rolesBySubject[binding.Subject] = binding.CatalogRole
		grantees = append(grantees, pgx.Identifier{binding.CatalogRole}.Sanitize())
	}

	statements := make([]string, 0, len(managedObjects)*len(grantees)+len(canonicalSet.Privileges))
	for _, object := range managedObjects {
		target, err := appACLConvergenceRevokeTarget(object)
		if err != nil {
			return nil, err
		}
		for _, grantee := range grantees {
			statements = append(statements, "revoke all privileges "+target+" from "+grantee)
		}
	}
	for _, privilege := range canonicalSet.Privileges {
		role, ok := rolesBySubject[privilege.Subject]
		if !ok {
			return nil, fmt.Errorf("app ACL compiler emitted an unbound subject %q", privilege.Subject)
		}
		target, err := appACLConvergenceGrantTarget(privilege)
		if err != nil {
			return nil, err
		}
		statements = append(statements, "grant "+string(privilege.Privilege)+" "+target+" to "+pgx.Identifier{role}.Sanitize())
	}
	return statements, nil
}

func applyAppACLConvergenceDCLInTx(ctx context.Context, tx pgx.Tx, contract AppACLEffectiveCatalogContractR1) error {
	if tx == nil {
		return fmt.Errorf("apply app ACL convergence DCL has no PostgreSQL transaction")
	}
	statements, err := appACLConvergenceDCLStatements(contract)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply app ACL convergence DCL: %w", err)
		}
	}
	return nil
}

func appACLConvergenceRevokeTarget(object AppACLManagedObjectR1) (string, error) {
	switch object.ObjectClass {
	case AppACLObjectClassDatabase:
		return "on database " + pgx.Identifier{object.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassSchema:
		return "on schema " + pgx.Identifier{object.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassTable, AppACLObjectClassView:
		return "on table " + pgx.Identifier{object.SchemaName, object.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassSequence:
		return "on sequence " + pgx.Identifier{object.SchemaName, object.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassFunction:
		identity, err := appACLConvergenceFunctionIdentity(object.SchemaName, object.ObjectIdentity)
		if err != nil {
			return "", err
		}
		return "on function " + identity, nil
	default:
		return "", fmt.Errorf("unsupported managed APP object class %q", object.ObjectClass)
	}
}

func appACLConvergenceGrantTarget(privilege AppACLPrivilege) (string, error) {
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabase:
		return "on database " + pgx.Identifier{privilege.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassSchema:
		return "on schema " + pgx.Identifier{privilege.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassTable, AppACLObjectClassView:
		return "on table " + pgx.Identifier{privilege.SchemaName, privilege.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassSequence:
		return "on sequence " + pgx.Identifier{privilege.SchemaName, privilege.ObjectIdentity}.Sanitize(), nil
	case AppACLObjectClassFunction:
		schemaName, identity, found := appACLFunctionIdentityFromQualifiedIdentityR1(privilege.ObjectIdentity)
		if !found {
			return "", fmt.Errorf("invalid app ACL function privilege identity %q", privilege.ObjectIdentity)
		}
		functionIdentity, err := appACLConvergenceFunctionIdentity(schemaName, identity)
		if err != nil {
			return "", err
		}
		return "on function " + functionIdentity, nil
	default:
		return "", fmt.Errorf("unsupported app ACL privilege object class %q", privilege.ObjectClass)
	}
}

func appACLConvergenceFunctionIdentity(schemaName string, identity string) (string, error) {
	name, arguments, found := strings.Cut(identity, "(")
	if !found || !validBareCatalogName(schemaName) || !validBareCatalogName(name) || !strings.HasSuffix(arguments, ")") {
		return "", fmt.Errorf("invalid managed APP function identity %q", identity)
	}
	arguments = strings.TrimSuffix(arguments, ")")
	for _, character := range arguments {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("_ ,.[]", character) {
			continue
		}
		return "", fmt.Errorf("invalid managed APP function identity arguments %q", arguments)
	}
	return pgx.Identifier{schemaName, name}.Sanitize() + "(" + arguments + ")", nil
}

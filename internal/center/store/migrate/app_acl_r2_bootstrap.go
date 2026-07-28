package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
)

const (
	appACLR2BootstrapMaxAttempts                  = 3
	appACLR2BootstrapDiscardTimeout               = 10 * time.Second
	appACLR2BootstrapSearchPathSQL                = "set local search_path = pg_catalog, public"
	appACLR2TransitionAdvisoryLockName            = "houfeng.app-acl-r2-privileged-transition.v1"
	appACLR2BootstrapSessionTransitionLockSQL     = "select pg_catalog.pg_advisory_lock(pg_catalog.hashtextextended($1, 0))"
	appACLR2BootstrapTransactionTransitionLockSQL = "select pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended($1, 0))"
	appACLR2BootstrapSessionTransitionUnlockSQL   = "select pg_catalog.pg_advisory_unlock(pg_catalog.hashtextextended($1, 0))"
)

var errAppACLR2BootstrapNilTransaction = errors.New("APP ACL R2 bootstrap transaction opener returned nil transaction")

type appACLR2BootstrapBeginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)

type appACLR2BootstrapReservedConn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Discard(context.Context) error
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Release()
}

type appACLR2BootstrapPoolConn struct {
	*pgxpool.Conn
}

func (conn *appACLR2BootstrapPoolConn) Discard(ctx context.Context) error {
	return conn.Hijack().Close(ctx)
}

type appACLR2BootstrapAcquireConn func(context.Context) (appACLR2BootstrapReservedConn, error)

type appACLR2BootstrapReservedTx struct {
	pgx.Tx
	conn     appACLR2BootstrapReservedConn
	finishMu sync.Mutex
	finished bool
}

func (tx *appACLR2BootstrapReservedTx) Commit(ctx context.Context) error {
	if !tx.claimFinish() {
		return pgx.ErrTxClosed
	}
	defer tx.conn.Release()
	return tx.Tx.Commit(ctx)
}

func (tx *appACLR2BootstrapReservedTx) Rollback(ctx context.Context) error {
	if !tx.claimFinish() {
		return pgx.ErrTxClosed
	}
	defer tx.conn.Release()
	return tx.Tx.Rollback(ctx)
}

func (tx *appACLR2BootstrapReservedTx) claimFinish() bool {
	tx.finishMu.Lock()
	defer tx.finishMu.Unlock()
	if tx.finished {
		return false
	}
	tx.finished = true
	return true
}

type appACLR2BootstrapDependencies struct {
	hardenSearchPath             func(context.Context, pgx.Tx) error
	requireBootstrapActor        func(context.Context, pgx.Tx) error
	readReservedObjects          func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error)
	lockStateTables              func(context.Context, pgx.Tx, bool) error
	classify                     func(context.Context, pgx.Tx) (AppACLR2State, error)
	verifyFrozen                 func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error)
	readBootstrapCatalog         func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2BootstrapCatalogSnapshotV1, error)
	validatePreMutationEvidence  func(AppACLR2BootstrapCatalogSnapshotV1, FrozenAppACLR1StateV1) error
	preflightSourceEvidence      func() error
	executeBootstrapSection      func(context.Context, pgx.Tx) error
	applyL2ACL                   func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) error
	readReceiptSurface           func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2ReceiptCatalogSnapshotV1, error)
	compileReceipt               func(AppACLR2BootstrapCatalogSnapshotV1, AppACLR2ReceiptCatalogSnapshotV1, FrozenAppACLR1StateV1) (AppACLR2BootstrapReceiptV1, error)
	encodeReceipt                func(AppACLR2BootstrapReceiptV1) ([]byte, error)
	insertReceipt                func(context.Context, pgx.Tx, []byte, [32]byte) error
	recoverCommitAcknowledgement func(context.Context) (appACLR2BootstrapACKOutcome, error)
	retryable                    func(error) bool
	safeToRetry                  func(error) bool
}

// BootstrapAppACLR2 creates the bootstrap-owned L2 receipt surface from an
// exact R1 catalog. It never creates or reads M2 contents.
func BootstrapAppACLR2(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return fmt.Errorf("APP ACL R2 bootstrap has no PostgreSQL pool")
	}
	begin := newAppACLR2BootstrapTransitionLockedBegin(func(ctx context.Context) (appACLR2BootstrapReservedConn, error) {
		conn, err := db.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		return &appACLR2BootstrapPoolConn{Conn: conn}, nil
	})
	if begin == nil {
		return fmt.Errorf("APP ACL R2 bootstrap locked transaction opener is nil")
	}
	dependencies := defaultAppACLR2BootstrapDependencies()
	dependencies.recoverCommitAcknowledgement = func(ctx context.Context) (appACLR2BootstrapACKOutcome, error) {
		return recoverAppACLR2BootstrapACKWithDependencies(ctx, begin, defaultAppACLR2BootstrapACKObserverDependencies())
	}
	return bootstrapAppACLR2WithDependencies(ctx, begin, dependencies)
}

func defaultAppACLR2BootstrapDependencies() appACLR2BootstrapDependencies {
	return appACLR2BootstrapDependencies{
		hardenSearchPath:            hardenAppACLR2BootstrapSearchPathInTx,
		requireBootstrapActor:       requireAppACLR2BootstrapActorInTx,
		readReservedObjects:         readAppACLR2BootstrapReservedCatalogObjectsInTx,
		lockStateTables:             lockAppACLR2BootstrapStateTablesInTx,
		classify:                    ClassifyAppACLR2State,
		verifyFrozen:                VerifyFrozenAppACLR1StateInTx,
		readBootstrapCatalog:        ReadAppACLR2BootstrapCatalogSnapshotInTx,
		validatePreMutationEvidence: validateAppACLR2BootstrapPreMutationEvidence,
		preflightSourceEvidence:     preflightAppACLR2BootstrapSourceEvidenceV1,
		executeBootstrapSection:     executeAppACLR2BootstrapSectionInTx,
		applyL2ACL:                  applyAppACLR2BootstrapL2ACLInTx,
		readReceiptSurface:          ReadAppACLR2ReceiptCatalogSnapshotInTx,
		compileReceipt:              CompileAppACLR2BootstrapReceiptFromCatalogV1,
		encodeReceipt:               CanonicalAppACLR2BootstrapReceiptBodyV1,
		insertReceipt:               insertAppACLR2BootstrapReceiptInTx,
		retryable:                   isAppACLR2BootstrapRetryable,
		safeToRetry:                 pgconn.SafeToRetry,
	}
}

func (dependencies appACLR2BootstrapDependencies) validate() error {
	if dependencies.hardenSearchPath == nil || dependencies.requireBootstrapActor == nil ||
		dependencies.readReservedObjects == nil || dependencies.lockStateTables == nil || dependencies.classify == nil ||
		dependencies.verifyFrozen == nil || dependencies.readBootstrapCatalog == nil || dependencies.executeBootstrapSection == nil ||
		dependencies.validatePreMutationEvidence == nil || dependencies.preflightSourceEvidence == nil ||
		dependencies.applyL2ACL == nil || dependencies.readReceiptSurface == nil || dependencies.compileReceipt == nil ||
		dependencies.encodeReceipt == nil || dependencies.insertReceipt == nil || dependencies.recoverCommitAcknowledgement == nil ||
		dependencies.retryable == nil || dependencies.safeToRetry == nil {
		return fmt.Errorf("APP ACL R2 bootstrap dependencies are incomplete")
	}
	return nil
}

func bootstrapAppACLR2WithDependencies(
	ctx context.Context,
	begin appACLR2BootstrapBeginTx,
	dependencies appACLR2BootstrapDependencies,
) error {
	if begin == nil {
		return fmt.Errorf("APP ACL R2 bootstrap transaction opener is nil")
	}
	if err := dependencies.validate(); err != nil {
		return err
	}

	for attempt := 0; attempt < appACLR2BootstrapMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			if dependencies.retryable(err) && attempt+1 < appACLR2BootstrapMaxAttempts {
				continue
			}
			return fmt.Errorf("begin APP ACL R2 bootstrap transaction: %w", err)
		}
		if tx == nil {
			return fmt.Errorf("begin APP ACL R2 bootstrap transaction: %w", errAppACLR2BootstrapNilTransaction)
		}

		attemptErr := bootstrapAppACLR2InTx(ctx, tx, dependencies)
		if attemptErr != nil {
			_ = tx.Rollback(ctx)
			if dependencies.retryable(attemptErr) && attempt+1 < appACLR2BootstrapMaxAttempts {
				continue
			}
			return attemptErr
		}

		commitErr := tx.Commit(ctx)
		if commitErr == nil {
			return nil
		}
		_ = tx.Rollback(ctx)
		// SafeToRetry proves the COMMIT command was never sent, so this is a
		// whole-closure retry rather than an acknowledgement-loss recovery.
		if dependencies.retryable(commitErr) && attempt+1 < appACLR2BootstrapMaxAttempts {
			continue
		}
		if !appACLR2BootstrapCommitMayBeUncertain(commitErr, dependencies.safeToRetry) {
			return fmt.Errorf("commit APP ACL R2 bootstrap transaction: %w", commitErr)
		}

		outcome, recoveryErr := dependencies.recoverCommitAcknowledgement(ctx)
		if recoveryErr != nil {
			return recoveryErr
		}
		switch outcome {
		case appACLR2BootstrapACKOutcomePrepared:
			return nil
		case appACLR2BootstrapACKOutcomeR1:
			if attempt+1 < appACLR2BootstrapMaxAttempts {
				continue
			}
			return fmt.Errorf("APP ACL R2 bootstrap acknowledgement recovery remained at exact R1")
		default:
			return fmt.Errorf("APP ACL R2 bootstrap acknowledgement recovery returned no exact outcome")
		}
	}
	return fmt.Errorf("APP ACL R2 bootstrap exhausted retry attempts")
}

func bootstrapAppACLR2InTx(ctx context.Context, tx pgx.Tx, dependencies appACLR2BootstrapDependencies) error {
	if tx == nil {
		return fmt.Errorf("APP ACL R2 bootstrap has no PostgreSQL transaction")
	}
	if err := dependencies.hardenSearchPath(ctx, tx); err != nil {
		return err
	}
	if err := dependencies.requireBootstrapActor(ctx, tx); err != nil {
		return err
	}
	objects, err := dependencies.readReservedObjects(ctx, tx)
	if err != nil {
		return err
	}
	if err := rejectAppACLR2BootstrapM2OrUnknownInventory(objects); err != nil {
		return err
	}
	if err := dependencies.lockStateTables(ctx, tx, appACLR2ReservedObjectExists(objects, appACLR2L2ReceiptRelation())); err != nil {
		return err
	}

	state, err := dependencies.classify(ctx, tx)
	if err != nil {
		return err
	}
	switch state {
	case AppACLR2StatePrepared:
		return nil
	case AppACLR2StateR1:
		return bootstrapAppACLR2FromExactR1InTx(ctx, tx, dependencies)
	default:
		return fmt.Errorf("APP ACL R2 bootstrap requires exact R1 or PREPARED state")
	}
}

func bootstrapAppACLR2FromExactR1InTx(ctx context.Context, tx pgx.Tx, dependencies appACLR2BootstrapDependencies) error {
	frozen, err := dependencies.verifyFrozen(ctx, tx)
	if err != nil {
		return err
	}
	bootstrapCatalog, err := dependencies.readBootstrapCatalog(ctx, tx, frozen)
	if err != nil {
		return err
	}
	if err := dependencies.validatePreMutationEvidence(bootstrapCatalog, frozen); err != nil {
		return err
	}
	if err := dependencies.preflightSourceEvidence(); err != nil {
		return err
	}
	if err := dependencies.executeBootstrapSection(ctx, tx); err != nil {
		return err
	}
	if err := dependencies.applyL2ACL(ctx, tx, frozen); err != nil {
		return err
	}
	surface, err := dependencies.readReceiptSurface(ctx, tx, frozen)
	if err != nil {
		return err
	}
	receipt, err := dependencies.compileReceipt(bootstrapCatalog, surface, frozen)
	if err != nil {
		return err
	}
	body, err := dependencies.encodeReceipt(receipt)
	if err != nil {
		return err
	}
	if err := dependencies.insertReceipt(ctx, tx, body, sha256.Sum256(body)); err != nil {
		return err
	}
	state, err := dependencies.classify(ctx, tx)
	if err != nil {
		return err
	}
	if state != AppACLR2StatePrepared {
		return fmt.Errorf("APP ACL R2 bootstrap post-write state is not exact PREPARED")
	}
	return nil
}

func validateAppACLR2BootstrapPreMutationEvidence(
	snapshot AppACLR2BootstrapCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) error {
	if err := validateFrozenAppACLR1StateForReceipt(frozen); err != nil {
		return fmt.Errorf("validate frozen APP ACL R1 bootstrap evidence: %w", err)
	}
	if _, _, err := validateAppACLR2BootstrapCatalog(snapshot, frozen); err != nil {
		return fmt.Errorf("validate APP ACL R2 bootstrap catalog evidence: %w", err)
	}
	return nil
}

// preflightAppACLR2BootstrapSourceEvidenceV1 proves the isolated R2 full-source
// snapshot and marker section evidence before any L2 DDL/DCL. Compiler/receipt
// paths revalidate the same facts later for defense in depth.
func preflightAppACLR2BootstrapSourceEvidenceV1() error {
	if _, err := CompileAppACLSourceSetR2V1(); err != nil {
		return fmt.Errorf("preflight APP ACL R2 source set evidence: %w", err)
	}
	if _, err := ReadAppACLR2SourceEvidenceV1(); err != nil {
		return fmt.Errorf("preflight APP ACL R2 source marker evidence: %w", err)
	}
	return nil
}

// readAppACLR2BootstrapReservedCatalogObjectsInTx is the bootstrap-only
// metadata inventory reader. Its SQL transfer is bounded to known+1 so an
// overflow shape is visible without an unbounded catalog scan, and it never
// locks or reads L2/M2 relation contents.
func readAppACLR2BootstrapReservedCatalogObjectsInTx(ctx context.Context, tx pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
	if tx == nil {
		return nil, fmt.Errorf("APP ACL R2 bootstrap reserved-object inventory has no transaction")
	}
	limit := len(appACLR2KnownReservedObjects()) + 1
	rows, err := tx.Query(ctx, `
		select object_kind, schema_name, object_oid, object_identity, object_detail
		from (
			select 'relation'::text as object_kind,
			       namespace.nspname::text as schema_name,
			       relation.oid::bigint as object_oid,
			       relation.relname::text as object_identity,
			       relation.relkind::text as object_detail
			from pg_catalog.pg_class relation
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			where relation.relname like 'app\_acl\_r2\_%' escape '\'
			union all
			select 'function'::text,
			       namespace.nspname::text,
			       procedure.oid::bigint,
			       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
			       procedure.prokind::text
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			where procedure.proname like 'app\_acl\_r2\_%' escape '\'
			union all
			select 'trigger'::text,
			       table_namespace.nspname::text,
			       trigger.oid::bigint,
			       table_relation.relname::text || '.' || trigger.tgname::text,
			       case when trigger.tgisinternal then 'internal' else 'user' end
			from pg_catalog.pg_trigger trigger
			join pg_catalog.pg_class table_relation on table_relation.oid = trigger.tgrelid
			join pg_catalog.pg_namespace table_namespace on table_namespace.oid = table_relation.relnamespace
			where trigger.tgname like 'app\_acl\_r2\_%' escape '\'
		) catalog
		order by object_kind, schema_name, object_identity, object_detail
		limit $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 bootstrap reserved catalog objects: %w", err)
	}
	defer rows.Close()
	objects := make([]AppACLR2ReservedCatalogObjectV1, 0, limit)
	for rows.Next() {
		var object AppACLR2ReservedCatalogObjectV1
		var oid int64
		if err := rows.Scan(&object.Kind, &object.Schema, &oid, &object.Identity, &object.Detail); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 bootstrap reserved catalog object: %w", err)
		}
		if object.OID, err = appACLR2CatalogUint32(oid, "reserved catalog object OID"); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 bootstrap reserved catalog objects: %w", err)
	}
	return objects, nil
}

func hardenAppACLR2BootstrapSearchPathInTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, appACLR2BootstrapSearchPathSQL); err != nil {
		return fmt.Errorf("set APP ACL R2 bootstrap search path: %w", err)
	}
	return nil
}

func requireAppACLR2BootstrapActorInTx(ctx context.Context, tx pgx.Tx) error {
	var sessionUser, currentUser string
	var oid int64
	var superuser bool
	if err := tx.QueryRow(ctx, `
		select session_user,
		       current_user,
		       role.oid::bigint,
		       role.rolsuper
		from pg_catalog.pg_roles role
		where role.rolname = current_user
	`).Scan(&sessionUser, &currentUser, &oid, &superuser); err != nil {
		return fmt.Errorf("read APP ACL R2 bootstrap actor: %w", err)
	}
	if sessionUser != currentUser || oid != 10 || !superuser {
		return fmt.Errorf("APP ACL R2 bootstrap requires a direct OID-10 PostgreSQL superuser session")
	}
	return nil
}

func newAppACLR2BootstrapTransitionLockedBegin(acquire appACLR2BootstrapAcquireConn) appACLR2BootstrapBeginTx {
	if acquire == nil {
		return nil
	}
	return func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		conn, err := acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("reserve APP ACL R2 bootstrap PostgreSQL connection: %w", err)
		}
		if conn == nil {
			return nil, fmt.Errorf("reserve APP ACL R2 bootstrap PostgreSQL connection returned nil")
		}
		releaseConn := true
		discardConn := false
		defer func() {
			if releaseConn {
				if discardConn {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), appACLR2BootstrapDiscardTimeout)
					defer cancel()
					_ = conn.Discard(cleanupCtx)
				} else {
					conn.Release()
				}
			}
		}()

		if err := acquireAppACLR2BootstrapSessionTransitionLock(ctx, conn); err != nil {
			discardConn = true
			return nil, err
		}
		tx, err := conn.BeginTx(ctx, options)
		if err != nil {
			unlockErr := releaseAppACLR2BootstrapSessionTransitionLock(ctx, conn)
			discardConn = unlockErr != nil
			return nil, errors.Join(fmt.Errorf("begin transition-locked APP ACL R2 bootstrap transaction: %w", err), unlockErr)
		}
		if tx == nil {
			unlockErr := releaseAppACLR2BootstrapSessionTransitionLock(ctx, conn)
			discardConn = unlockErr != nil
			return nil, errors.Join(fmt.Errorf("begin transition-locked APP ACL R2 bootstrap transaction: %w", errAppACLR2BootstrapNilTransaction), unlockErr)
		}
		if err := acquireAppACLR2BootstrapTransitionLockInTx(ctx, tx); err != nil {
			rollbackErr := rollbackAppACLR2BootstrapLockHandoff(ctx, tx)
			unlockErr := releaseAppACLR2BootstrapSessionTransitionLock(ctx, conn)
			discardConn = rollbackErr != nil || unlockErr != nil
			return nil, errors.Join(err, rollbackErr, unlockErr)
		}
		if err := releaseAppACLR2BootstrapSessionTransitionLock(ctx, tx); err != nil {
			rollbackErr := rollbackAppACLR2BootstrapLockHandoff(ctx, tx)
			unlockErr := releaseAppACLR2BootstrapSessionTransitionLock(ctx, conn)
			discardConn = true
			return nil, errors.Join(err, rollbackErr, unlockErr)
		}

		releaseConn = false
		return &appACLR2BootstrapReservedTx{Tx: tx, conn: conn}, nil
	}
}

type appACLR2BootstrapTransitionLockExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type appACLR2BootstrapTransitionLockQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func acquireAppACLR2BootstrapSessionTransitionLock(ctx context.Context, conn appACLR2BootstrapTransitionLockExecutor) error {
	if _, err := conn.Exec(ctx, appACLR2BootstrapSessionTransitionLockSQL, appACLR2TransitionAdvisoryLockName); err != nil {
		return fmt.Errorf("lock APP ACL R2 bootstrap transition before transaction: %w", err)
	}
	return nil
}

func acquireAppACLR2BootstrapTransitionLockInTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, appACLR2BootstrapTransactionTransitionLockSQL, appACLR2TransitionAdvisoryLockName); err != nil {
		return fmt.Errorf("lock APP ACL R2 bootstrap transition: %w", err)
	}
	return nil
}

func releaseAppACLR2BootstrapSessionTransitionLock(ctx context.Context, conn appACLR2BootstrapTransitionLockQuerier) error {
	var unlocked bool
	if err := conn.QueryRow(ctx, appACLR2BootstrapSessionTransitionUnlockSQL, appACLR2TransitionAdvisoryLockName).Scan(&unlocked); err != nil {
		return fmt.Errorf("unlock APP ACL R2 bootstrap session transition lock: %w", err)
	}
	if !unlocked {
		return fmt.Errorf("APP ACL R2 bootstrap session transition lock was not held")
	}
	return nil
}

func rollbackAppACLR2BootstrapLockHandoff(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("roll back APP ACL R2 bootstrap lock handoff: %w", err)
	}
	return nil
}

func rejectAppACLR2BootstrapM2OrUnknownInventory(objects []AppACLR2ReservedCatalogObjectV1) error {
	if !appACLR2ReservedObjectsContainOnlyKnown(objects, appACLR2KnownReservedObjects()) {
		return fmt.Errorf("APP ACL R2 bootstrap reserved-object inventory contains an unknown object")
	}
	if !appACLR2ReservedObjectsAbsent(objects, appACLR2M2ReservedObjects()) {
		return fmt.Errorf("APP ACL R2 bootstrap reserved-object inventory contains M2 evidence")
	}
	return nil
}

func lockAppACLR2BootstrapStateTablesInTx(ctx context.Context, tx pgx.Tx, receiptExists bool) error {
	statements := []string{
		"lock table public.app_acl_manifest_head in share row exclusive mode",
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"lock table public.record_platform_domain_identity in share row exclusive mode",
		"lock table public.schema_migrations in share row exclusive mode",
	}
	if receiptExists {
		statements = append(statements, "lock table public.app_acl_r2_bootstrap_receipt in share row exclusive mode")
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("lock APP ACL R2 bootstrap state table: %w", err)
		}
	}
	return nil
}

func executeAppACLR2BootstrapSectionInTx(ctx context.Context, tx pgx.Tx) error {
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2MigrationName)
	if err != nil {
		return fmt.Errorf("read isolated APP ACL R2 bootstrap source: %w", err)
	}
	section, err := appACLR2SourceSection(payload, appACLR2BootstrapBeginMarker, appACLR2BootstrapEndMarker)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 bootstrap source section: %w", err)
	}
	sql, err := appACLR2BootstrapExecutableSQL(section)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("execute APP ACL R2 bootstrap SQL: %w", err)
	}
	return nil
}

func appACLR2BootstrapExecutableSQL(section []byte) ([]byte, error) {
	beginLine := []byte(appACLR2BootstrapBeginMarker + "\n")
	endLine := []byte("-- " + appACLR2BootstrapEndMarker + "\n")
	if !bytes.HasPrefix(section, beginLine) || !bytes.HasSuffix(section, endLine) || len(section) <= len(beginLine)+len(endLine) {
		return nil, fmt.Errorf("APP ACL R2 bootstrap source section markers are malformed")
	}
	payload := append([]byte(nil), section[len(beginLine):len(section)-len(endLine)]...)
	if len(bytes.TrimSpace(payload)) == 0 || bytes.Contains(payload, []byte(appACLR2BootstrapBeginMarker)) || bytes.Contains(payload, []byte(appACLR2BootstrapEndMarker)) ||
		bytes.Contains(payload, []byte(appACLR2FinalizeBeginMarker)) || bytes.Contains(payload, []byte(appACLR2FinalizeEndMarker)) {
		return nil, fmt.Errorf("APP ACL R2 bootstrap SQL payload is not marker-exclusive")
	}
	return payload, nil
}

func applyAppACLR2BootstrapL2ACLInTx(ctx context.Context, tx pgx.Tx, state FrozenAppACLR1StateV1) error {
	roles := []string{state.DirectMigratorRole, state.CenterRuntimeRole, state.PlatformAdminRole}
	for _, role := range roles {
		if !validAppACLR2RoleName(role) {
			return fmt.Errorf("APP ACL R2 bootstrap has invalid control role")
		}
	}
	if roles[0] == roles[1] || roles[0] == roles[2] || roles[1] == roles[2] {
		return fmt.Errorf("APP ACL R2 bootstrap control roles are not distinct")
	}

	grantees := "public, " + pgx.Identifier{roles[0]}.Sanitize() + ", " + pgx.Identifier{roles[1]}.Sanitize() + ", " + pgx.Identifier{roles[2]}.Sanitize()
	statements := []string{
		"revoke all privileges on table public.app_acl_r2_bootstrap_receipt from " + grantees,
		"grant select on table public.app_acl_r2_bootstrap_receipt to " + pgx.Identifier{roles[0]}.Sanitize(),
		"grant select on table public.app_acl_r2_bootstrap_receipt to " + pgx.Identifier{roles[1]}.Sanitize(),
		"revoke all privileges on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) from " + grantees,
		"revoke all privileges on function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation() from " + grantees,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply APP ACL R2 bootstrap receipt ACL: %w", err)
		}
	}
	return nil
}

func insertAppACLR2BootstrapReceiptInTx(ctx context.Context, tx pgx.Tx, body []byte, digest [32]byte) error {
	if len(body) < 1 || len(body) > appACLR2MaximumBodyBytes {
		return fmt.Errorf("APP ACL R2 bootstrap receipt body size is outside bounds")
	}
	if _, err := tx.Exec(ctx, `
		insert into public.app_acl_r2_bootstrap_receipt (singleton, receipt_body, receipt_digest)
		values (true, $1, $2)
	`, body, digest[:]); err != nil {
		return fmt.Errorf("insert APP ACL R2 bootstrap receipt: %w", err)
	}
	return nil
}

func isAppACLR2BootstrapRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func appACLR2BootstrapCommitMayBeUncertain(err error, safeToRetry func(error) bool) bool {
	if err == nil || safeToRetry(err) || isAppACLR2BootstrapRetryable(err) || errors.Is(err, pgx.ErrTxCommitRollback) || errors.Is(err, pgx.ErrTxClosed) {
		return false
	}
	var pgErr *pgconn.PgError
	return !errors.As(err, &pgErr)
}

type appACLR2BootstrapACKOutcome uint8

const (
	appACLR2BootstrapACKOutcomeNone appACLR2BootstrapACKOutcome = iota
	appACLR2BootstrapACKOutcomeR1
	appACLR2BootstrapACKOutcomePrepared
)

type appACLR2BootstrapACKObserverDependencies struct {
	hardenSearchPath      func(context.Context, pgx.Tx) error
	requireBootstrapActor func(context.Context, pgx.Tx) error
	readReservedObjects   func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error)
	verifyFrozen          func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error)
	readL2Rows            func(context.Context, pgx.Tx) ([]appACLR2ReceiptRowV1, error)
	exactL2Rows           func([]appACLR2ReceiptRowV1) bool
	verifyL2Evidence      func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error
}

func defaultAppACLR2BootstrapACKObserverDependencies() appACLR2BootstrapACKObserverDependencies {
	return appACLR2BootstrapACKObserverDependencies{
		hardenSearchPath:      hardenAppACLR2BootstrapSearchPathInTx,
		requireBootstrapActor: requireAppACLR2BootstrapActorInTx,
		readReservedObjects:   readAppACLR2BootstrapReservedCatalogObjectsInTx,
		verifyFrozen:          VerifyFrozenAppACLR1StateInTx,
		readL2Rows:            readAppACLR2ReceiptRowsInTx,
		exactL2Rows:           appACLR2ExactL2Rows,
		verifyL2Evidence:      verifyAppACLR2L2EvidenceInTx,
	}
}

func (dependencies appACLR2BootstrapACKObserverDependencies) validate() error {
	if dependencies.hardenSearchPath == nil || dependencies.requireBootstrapActor == nil ||
		dependencies.readReservedObjects == nil || dependencies.verifyFrozen == nil || dependencies.readL2Rows == nil || dependencies.exactL2Rows == nil || dependencies.verifyL2Evidence == nil {
		return fmt.Errorf("APP ACL R2 bootstrap acknowledgement observer dependencies are incomplete")
	}
	return nil
}

func recoverAppACLR2BootstrapACKWithDependencies(
	ctx context.Context,
	begin appACLR2BootstrapBeginTx,
	dependencies appACLR2BootstrapACKObserverDependencies,
) (appACLR2BootstrapACKOutcome, error) {
	if begin == nil {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("APP ACL R2 bootstrap acknowledgement observer transaction opener is nil")
	}
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadOnly})
	if err != nil {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("begin APP ACL R2 bootstrap acknowledgement observer transaction: %w", err)
	}
	if tx == nil {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("begin APP ACL R2 bootstrap acknowledgement observer transaction: %w", errAppACLR2BootstrapNilTransaction)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	outcome, err := observeAppACLR2BootstrapACKRecoveryInTxWithDependencies(ctx, tx, dependencies)
	if err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	if err := tx.Commit(ctx); err != nil {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("commit APP ACL R2 bootstrap acknowledgement observer transaction: %w", err)
	}
	return outcome, nil
}

// observeAppACLR2BootstrapACKRecoveryInTx is deliberately private. It proves
// only exact R1 or PREPARED after a lost bootstrap commit acknowledgement.
func observeAppACLR2BootstrapACKRecoveryInTx(ctx context.Context, tx pgx.Tx) (appACLR2BootstrapACKOutcome, error) {
	return observeAppACLR2BootstrapACKRecoveryInTxWithDependencies(ctx, tx, defaultAppACLR2BootstrapACKObserverDependencies())
}

func observeAppACLR2BootstrapACKRecoveryInTxWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	dependencies appACLR2BootstrapACKObserverDependencies,
) (appACLR2BootstrapACKOutcome, error) {
	if tx == nil {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("APP ACL R2 bootstrap acknowledgement observer has no transaction")
	}
	if err := dependencies.validate(); err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	if err := dependencies.hardenSearchPath(ctx, tx); err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	if err := dependencies.requireBootstrapActor(ctx, tx); err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	objects, err := dependencies.readReservedObjects(ctx, tx)
	if err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	if len(objects) == 0 {
		if _, err := dependencies.verifyFrozen(ctx, tx); err != nil {
			return appACLR2BootstrapACKOutcomeNone, err
		}
		return appACLR2BootstrapACKOutcomeR1, nil
	}
	if !appACLR2ReservedObjectsContainOnlyKnown(objects, appACLR2L2ReservedObjects()) || !appACLR2ReservedObjectsContain(objects, appACLR2L2ReservedObjects()) {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("APP ACL R2 bootstrap acknowledgement observer inventory is not exact L2")
	}
	frozen, err := dependencies.verifyFrozen(ctx, tx)
	if err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	rows, err := dependencies.readL2Rows(ctx, tx)
	if err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	if !dependencies.exactL2Rows(rows) {
		return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("APP ACL R2 bootstrap acknowledgement observer receipt rows are not exact")
	}
	if err := dependencies.verifyL2Evidence(ctx, tx, frozen, rows[0]); err != nil {
		return appACLR2BootstrapACKOutcomeNone, err
	}
	return appACLR2BootstrapACKOutcomePrepared, nil
}

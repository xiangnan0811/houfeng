package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
)

// appACLR2FinalizeDependencies keeps the finalizer's transaction-bound
// evidence and mutation seams explicit so unit tests can prove that every
// pre-mutation rejection leaves the M2 surface untouched.
type appACLR2FinalizeDependencies struct {
	hardenSearchPath             func(context.Context, pgx.Tx) error
	requireDirectMigratorActor   func(context.Context, pgx.Tx) error
	lockStateTables              func(context.Context, pgx.Tx) error
	classify                     func(context.Context, pgx.Tx) (AppACLR2State, error)
	readCatalogPredicates        func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error)
	readReceipt                  func(context.Context, pgx.Tx) (appACLR2ReceiptRowV1, error)
	preflightSourceEvidence      func() error
	executeFinalizeSection       func(context.Context, pgx.Tx) error
	applyM2ControlACL            func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) error
	insertM2Revision             func(context.Context, pgx.Tx, AppACLManifestR2V1, []byte, [32]byte) error
	compareAndSwapM2Head         func(context.Context, pgx.Tx, AppACLManifestR2V1, [32]byte) error
	readFinalized                func(context.Context, pgx.Tx) error
	recoverCommitAcknowledgement func(context.Context) (AppACLR2State, error)
	safeToRetry                  func(error) bool
}

func (dependencies appACLR2FinalizeDependencies) validate() error {
	if dependencies.hardenSearchPath == nil || dependencies.requireDirectMigratorActor == nil ||
		dependencies.lockStateTables == nil || dependencies.classify == nil || dependencies.readCatalogPredicates == nil ||
		dependencies.readReceipt == nil || dependencies.preflightSourceEvidence == nil ||
		dependencies.executeFinalizeSection == nil || dependencies.applyM2ControlACL == nil || dependencies.insertM2Revision == nil ||
		dependencies.compareAndSwapM2Head == nil || dependencies.readFinalized == nil ||
		dependencies.recoverCommitAcknowledgement == nil || dependencies.safeToRetry == nil {
		return fmt.Errorf("APP ACL R2 finalizer dependencies are incomplete")
	}
	return nil
}

// FinalizeAppACLR2 advances an exact PREPARED catalog to exact FINALIZED.
// It uses the direct migrator's native receipt and M2 SELECT authority only.
func FinalizeAppACLR2(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return fmt.Errorf("APP ACL R2 finalizer has no PostgreSQL pool")
	}
	begin := newAppACLR2BootstrapTransitionLockedBegin(func(ctx context.Context) (appACLR2BootstrapReservedConn, error) {
		conn, err := db.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		return &appACLR2BootstrapPoolConn{Conn: conn}, nil
	})
	if begin == nil {
		return fmt.Errorf("APP ACL R2 finalizer locked transaction opener is nil")
	}
	dependencies := defaultAppACLR2FinalizeDependencies(begin)
	return finalizeAppACLR2WithDependencies(ctx, begin, dependencies)
}

func defaultAppACLR2FinalizeDependencies(begin appACLR2BootstrapBeginTx) appACLR2FinalizeDependencies {
	dependencies := appACLR2FinalizeDependencies{
		hardenSearchPath:           hardenAppACLR2BootstrapSearchPathInTx,
		requireDirectMigratorActor: requireAppACLR2DirectMigratorActorInTx,
		lockStateTables:            lockAppACLR2FinalizeStateTablesInTx,
		classify:                   ClassifyAppACLR2State,
		readCatalogPredicates:      ReadAppACLR2CatalogPredicatesInTx,
		readReceipt:                readAppACLR2FinalizeReceiptInTx,
		preflightSourceEvidence:    preflightAppACLR2FinalizeSourceEvidenceV1,
		executeFinalizeSection:     executeAppACLR2FinalizeSectionInTx,
		applyM2ControlACL:          applyAppACLR2FinalizeM2ControlACLInTx,
		insertM2Revision:           insertAppACLR2FinalizeM2RevisionInTx,
		compareAndSwapM2Head:       compareAndSwapAppACLR2FinalizeM2HeadInTx,
		readFinalized:              readAppACLR2FinalizeExactFinalizedInTx,
		safeToRetry:                pgconn.SafeToRetry,
	}
	dependencies.recoverCommitAcknowledgement = func(ctx context.Context) (AppACLR2State, error) {
		return recoverAppACLR2FinalizeACKWithDependencies(ctx, begin, defaultAppACLR2FinalizeACKDependencies())
	}
	return dependencies
}

func finalizeAppACLR2WithDependencies(
	ctx context.Context,
	begin appACLR2BootstrapBeginTx,
	dependencies appACLR2FinalizeDependencies,
) error {
	if begin == nil {
		return fmt.Errorf("APP ACL R2 finalizer transaction opener is nil")
	}
	if err := dependencies.validate(); err != nil {
		return err
	}
	for attempt := 0; attempt < appACLR2BootstrapMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
		if err != nil {
			if isAppACLR2FinalizeRetryable(err) && attempt+1 < appACLR2BootstrapMaxAttempts {
				continue
			}
			return fmt.Errorf("begin APP ACL R2 finalizer transaction: %w", err)
		}
		if tx == nil {
			return fmt.Errorf("begin APP ACL R2 finalizer transaction: %w", errAppACLR2BootstrapNilTransaction)
		}

		attemptErr := finalizeAppACLR2InTx(ctx, tx, dependencies)
		if attemptErr != nil {
			_ = tx.Rollback(ctx)
			if isAppACLR2FinalizeRetryable(attemptErr) && attempt+1 < appACLR2BootstrapMaxAttempts {
				continue
			}
			return attemptErr
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			if isAppACLR2FinalizeRetryable(err) && attempt+1 < appACLR2BootstrapMaxAttempts {
				continue
			}
			if dependencies.safeToRetry == nil || dependencies.recoverCommitAcknowledgement == nil ||
				!appACLR2BootstrapCommitMayBeUncertain(err, dependencies.safeToRetry) {
				return fmt.Errorf("commit APP ACL R2 finalizer transaction: %w", err)
			}
			state, recoveryErr := dependencies.recoverCommitAcknowledgement(ctx)
			if recoveryErr != nil {
				if isAppACLR2FinalizeRetryable(recoveryErr) && attempt+1 < appACLR2BootstrapMaxAttempts {
					continue
				}
				return recoveryErr
			}
			switch state {
			case AppACLR2StateFinalized:
				return nil
			case AppACLR2StatePrepared:
				if attempt+1 < appACLR2BootstrapMaxAttempts {
					continue
				}
				return fmt.Errorf("APP ACL R2 finalizer acknowledgement recovery remained at exact PREPARED")
			default:
				return fmt.Errorf("APP ACL R2 finalizer acknowledgement recovery did not prove exact FINALIZED or PREPARED state")
			}
		}
		return nil
	}
	return fmt.Errorf("APP ACL R2 finalizer exhausted retry attempts")
}

func isAppACLR2FinalizeRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

type appACLR2FinalizeACKDependencies struct {
	hardenSearchPath           func(context.Context, pgx.Tx) error
	requireDirectMigratorActor func(context.Context, pgx.Tx) error
	lockStateTables            func(context.Context, pgx.Tx) error
	classify                   func(context.Context, pgx.Tx) (AppACLR2State, error)
}

func defaultAppACLR2FinalizeACKDependencies() appACLR2FinalizeACKDependencies {
	return appACLR2FinalizeACKDependencies{
		hardenSearchPath:           hardenAppACLR2BootstrapSearchPathInTx,
		requireDirectMigratorActor: requireAppACLR2DirectMigratorActorInTx,
		lockStateTables:            lockAppACLR2FinalizeStateTablesInTx,
		classify:                   ClassifyAppACLR2State,
	}
}

func (dependencies appACLR2FinalizeACKDependencies) validate() error {
	if dependencies.hardenSearchPath == nil || dependencies.requireDirectMigratorActor == nil ||
		dependencies.lockStateTables == nil || dependencies.classify == nil {
		return fmt.Errorf("APP ACL R2 finalizer acknowledgement recovery dependencies are incomplete")
	}
	return nil
}

func recoverAppACLR2FinalizeACKWithDependencies(
	ctx context.Context,
	begin appACLR2BootstrapBeginTx,
	dependencies appACLR2FinalizeACKDependencies,
) (AppACLR2State, error) {
	if begin == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 finalizer acknowledgement recovery transaction opener is nil")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLR2StateCorrupt, err
	}
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
	if err != nil {
		return AppACLR2StateCorrupt, fmt.Errorf("begin APP ACL R2 finalizer acknowledgement recovery transaction: %w", err)
	}
	if tx == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("begin APP ACL R2 finalizer acknowledgement recovery transaction: %w", errAppACLR2BootstrapNilTransaction)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	state, err := observeAppACLR2FinalizeACKRecoveryInTxWithDependencies(ctx, tx, dependencies)
	if err != nil {
		return AppACLR2StateCorrupt, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLR2StateCorrupt, fmt.Errorf("commit APP ACL R2 finalizer acknowledgement recovery transaction: %w", err)
	}
	return state, nil
}

func observeAppACLR2FinalizeACKRecoveryInTxWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	dependencies appACLR2FinalizeACKDependencies,
) (AppACLR2State, error) {
	if tx == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 finalizer acknowledgement recovery has no transaction")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLR2StateCorrupt, err
	}
	if err := dependencies.hardenSearchPath(ctx, tx); err != nil {
		return AppACLR2StateCorrupt, err
	}
	if err := dependencies.requireDirectMigratorActor(ctx, tx); err != nil {
		return AppACLR2StateCorrupt, err
	}
	if err := dependencies.lockStateTables(ctx, tx); err != nil {
		return AppACLR2StateCorrupt, err
	}
	state, err := dependencies.classify(ctx, tx)
	if err != nil {
		return AppACLR2StateCorrupt, err
	}
	if state != AppACLR2StateFinalized && state != AppACLR2StatePrepared {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 finalizer acknowledgement recovery did not prove exact FINALIZED or PREPARED state")
	}
	return state, nil
}

// requireAppACLR2DirectMigratorActorInTx is the finalizer's sole
// session-identity gate. It compares the direct login to the immutable M1
// migrator binding, while the later frozen verifier remains identity-blind.
func requireAppACLR2DirectMigratorActorInTx(ctx context.Context, tx pgx.Tx) error {
	var sessionUser, currentUser, expectedMigrator string
	var login, inherit, superuser, createDatabase, createRole, replication, bypassRLS bool
	if err := tx.QueryRow(ctx, `
		select session_user::text,
		       current_user::text,
		       role.rolcanlogin,
		       role.rolinherit,
		       role.rolsuper,
		       role.rolcreatedb,
		       role.rolcreaterole,
		       role.rolreplication,
		       role.rolbypassrls,
		       (
			select manifest.migrator_catalog_role::text
			from public.app_acl_manifest_revisions manifest
			where manifest.manifest_revision = 1
			limit 1
		       )
		from pg_catalog.pg_roles role
		where role.rolname = current_user
	`).Scan(
		&sessionUser,
		&currentUser,
		&login,
		&inherit,
		&superuser,
		&createDatabase,
		&createRole,
		&replication,
		&bypassRLS,
		&expectedMigrator,
	); err != nil {
		return fmt.Errorf("read APP ACL R2 finalizer direct migrator actor: %w", err)
	}
	if !validAppACLR2RoleName(expectedMigrator) || sessionUser != expectedMigrator || currentUser != expectedMigrator || sessionUser != currentUser {
		return fmt.Errorf("APP ACL R2 finalizer requires direct migrator role %q, got %q/%q", expectedMigrator, sessionUser, currentUser)
	}
	if !login || inherit || superuser || createDatabase || createRole || replication || bypassRLS {
		return fmt.Errorf("APP ACL R2 finalizer requires a constrained direct migrator login")
	}
	return nil
}

func lockAppACLR2FinalizeStateTablesInTx(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		"lock table public.app_acl_manifest_head in share row exclusive mode",
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"lock table public.record_platform_domain_identity in share row exclusive mode",
		"lock table public.schema_migrations in share row exclusive mode",
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("lock APP ACL R2 finalizer state table: %w", err)
		}
	}
	var receiptPresent, revisionsPresent, headPresent bool
	if err := tx.QueryRow(ctx, `
		select pg_catalog.to_regclass('public.app_acl_r2_bootstrap_receipt') is not null,
		       pg_catalog.to_regclass('public.app_acl_r2_manifest_revisions') is not null,
		       pg_catalog.to_regclass('public.app_acl_r2_manifest_head') is not null
	`).Scan(&receiptPresent, &revisionsPresent, &headPresent); err != nil {
		return fmt.Errorf("read APP ACL R2 finalizer M2 relation presence: %w", err)
	}
	if receiptPresent {
		if _, err := tx.Exec(ctx, "lock table public.app_acl_r2_bootstrap_receipt in access share mode"); err != nil {
			return fmt.Errorf("lock APP ACL R2 finalizer state table: %w", err)
		}
	}
	if revisionsPresent {
		if _, err := tx.Exec(ctx, "lock table public.app_acl_r2_manifest_revisions in access share mode"); err != nil {
			return fmt.Errorf("lock APP ACL R2 finalizer state table: %w", err)
		}
	}
	if headPresent {
		if _, err := tx.Exec(ctx, "lock table public.app_acl_r2_manifest_head in access share mode"); err != nil {
			return fmt.Errorf("lock APP ACL R2 finalizer state table: %w", err)
		}
	}
	return nil
}

// finalizeAppACLR2InTx performs the evidence-only half of the direct
// migrator finalizer. The M2 mutation half is deliberately added only after
// this exact PREPARED gate has succeeded.
func finalizeAppACLR2InTx(ctx context.Context, tx pgx.Tx, dependencies appACLR2FinalizeDependencies) error {
	if tx == nil {
		return fmt.Errorf("APP ACL R2 finalizer has no PostgreSQL transaction")
	}
	if err := dependencies.validate(); err != nil {
		return err
	}
	if err := dependencies.hardenSearchPath(ctx, tx); err != nil {
		return err
	}
	if err := dependencies.requireDirectMigratorActor(ctx, tx); err != nil {
		return err
	}
	if err := dependencies.lockStateTables(ctx, tx); err != nil {
		return err
	}

	state, err := dependencies.classify(ctx, tx)
	if err != nil {
		return err
	}
	if state == AppACLR2StateFinalized {
		return nil
	}
	if state != AppACLR2StatePrepared {
		return fmt.Errorf("APP ACL R2 finalizer requires exact PREPARED state")
	}

	predicates, err := dependencies.readCatalogPredicates(ctx, tx)
	if err != nil {
		return err
	}
	if !predicates.ExactPrepared() {
		return fmt.Errorf("APP ACL R2 finalizer requires the reusable exact PREPARED predicate")
	}
	receipt, err := dependencies.readReceipt(ctx, tx)
	if err != nil {
		return err
	}
	if err := dependencies.preflightSourceEvidence(); err != nil {
		return err
	}
	manifest, body, digest, err := compileAppACLR2FinalizeManifest(predicates.FrozenState, receipt)
	if err != nil {
		return err
	}
	if err := dependencies.executeFinalizeSection(ctx, tx); err != nil {
		return err
	}
	if err := dependencies.insertM2Revision(ctx, tx, manifest, body, digest); err != nil {
		return err
	}
	if err := dependencies.compareAndSwapM2Head(ctx, tx, manifest, digest); err != nil {
		return err
	}
	if err := dependencies.applyM2ControlACL(ctx, tx, predicates.FrozenState); err != nil {
		return err
	}
	if err := dependencies.readFinalized(ctx, tx); err != nil {
		return err
	}
	return nil
}

func readAppACLR2FinalizeReceiptInTx(ctx context.Context, tx pgx.Tx) (appACLR2ReceiptRowV1, error) {
	rows, err := readAppACLR2ReceiptRowsInTx(ctx, tx)
	if err != nil {
		return appACLR2ReceiptRowV1{}, fmt.Errorf("read APP ACL R2 finalizer receipt: %w", err)
	}
	if !appACLR2ExactL2Rows(rows) {
		return appACLR2ReceiptRowV1{}, fmt.Errorf("APP ACL R2 finalizer receipt rows are not exact")
	}
	return rows[0], nil
}

func readAppACLR2FinalizeExactFinalizedInTx(ctx context.Context, tx pgx.Tx) error {
	state, err := ClassifyAppACLR2State(ctx, tx)
	if err != nil {
		return fmt.Errorf("classify APP ACL R2 finalizer post-write state: %w", err)
	}
	if state != AppACLR2StateFinalized {
		return fmt.Errorf("APP ACL R2 finalizer post-write state is not exact FINALIZED")
	}
	return nil
}

func compileAppACLR2FinalizeManifest(
	frozen FrozenAppACLR1StateV1,
	row appACLR2ReceiptRowV1,
) (AppACLManifestR2V1, []byte, [32]byte, error) {
	if err := validateFrozenAppACLR1StateForReceipt(frozen); err != nil {
		return AppACLManifestR2V1{}, nil, [32]byte{}, fmt.Errorf("validate frozen APP ACL R1 finalizer evidence: %w", err)
	}
	if !row.Singleton || len(row.Body) == 0 || sha256.Sum256(row.Body) != row.Digest {
		return AppACLManifestR2V1{}, nil, [32]byte{}, fmt.Errorf("APP ACL R2 finalizer receipt row is not an exact singleton")
	}
	receipt, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(row.Body)
	if err != nil {
		return AppACLManifestR2V1{}, nil, [32]byte{}, fmt.Errorf("parse APP ACL R2 finalizer receipt: %w", err)
	}
	var directMigrator AppACLR2ReceiptRoleV1
	for _, role := range receipt.Roles {
		if role.ControlRole == AppACLControlRoleDirectMigratorR2 {
			directMigrator = role
			break
		}
	}
	if directMigrator.Name == "" || directMigrator.OID == 0 || directMigrator.Name != frozen.DirectMigratorRole {
		return AppACLManifestR2V1{}, nil, [32]byte{}, fmt.Errorf("APP ACL R2 finalizer receipt direct migrator does not match frozen M1")
	}
	controlBody, err := CompileAppACLControlACLBodyR2V1(directMigrator.OID)
	if err != nil {
		return AppACLManifestR2V1{}, nil, [32]byte{}, fmt.Errorf("compile APP ACL R2 M2 control ACL: %w", err)
	}
	manifest := AppACLManifestR2V1{
		ProtocolVersion:            2,
		ManifestRevision:           2,
		M1Revision:                 frozen.ManifestRevision,
		M1ManifestDigest:           frozen.ManifestDigest,
		M1SourceSetDigest:          frozen.SourceSetDigest,
		M1PrivilegeSetDigest:       frozen.PrivilegeSetDigest,
		M1MigratorCatalogRole:      frozen.DirectMigratorRole,
		DirectMigratorName:         directMigrator.Name,
		DirectMigratorOID:          directMigrator.OID,
		R2SourceSetBody:            append([]byte(nil), receipt.R2SourceBody...),
		R2SourceSetDigest:          receipt.R2SourceDigest,
		R2PrivilegeSetBody:         append([]byte(nil), receipt.R2PrivilegeBody...),
		R2PrivilegeSetDigest:       receipt.R2PrivilegeDigest,
		DomainBody:                 append([]byte(nil), receipt.DomainBody...),
		DomainDigest:               receipt.DomainDigest,
		ReceiptDigest:              row.Digest,
		ControlACLBody:             controlBody,
		ControlACLDigest:           sha256.Sum256(controlBody),
		RecordedAtUnixMicroseconds: time.Now().UTC().UnixMicro(),
	}
	body, err := CanonicalAppACLManifestR2BodyV1(manifest)
	if err != nil {
		return AppACLManifestR2V1{}, nil, [32]byte{}, fmt.Errorf("encode APP ACL R2 M2 manifest: %w", err)
	}
	return manifest, body, sha256.Sum256(body), nil
}

func executeAppACLR2FinalizeSectionInTx(ctx context.Context, tx pgx.Tx) error {
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2MigrationName)
	if err != nil {
		return fmt.Errorf("read isolated APP ACL R2 finalizer source: %w", err)
	}
	section, err := appACLR2SourceSection(payload, appACLR2FinalizeBeginMarker, appACLR2FinalizeEndMarker)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 finalizer source section: %w", err)
	}
	return executeAppACLR2FinalizeSQLInTx(ctx, tx, section)
}

const (
	appACLR2FinalizeCanonicalSectionBytes = 4528
	appACLR2FinalizeCanonicalBodyBytes    = 4462
	appACLR2FinalizeEndCommentPrefix      = "-- "
)

var appACLR2FinalizeCanonicalBodyDigest = [32]byte{
	0x47, 0x17, 0x93, 0xde, 0xc8, 0x38, 0xd7, 0x5f,
	0xd7, 0x1c, 0xcc, 0x00, 0x65, 0xc9, 0xfb, 0xa4,
	0x31, 0x9b, 0x1f, 0xd3, 0x62, 0x18, 0xbb, 0x36,
	0xa8, 0x73, 0x4b, 0x85, 0x76, 0x62, 0x47, 0x93,
}

func preflightAppACLR2FinalizeSourceEvidenceV1() error {
	if err := preflightAppACLR2BootstrapSourceEvidenceV1(); err != nil {
		return err
	}
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2MigrationName)
	if err != nil {
		return fmt.Errorf("read isolated APP ACL R2 finalizer source: %w", err)
	}
	section, err := appACLR2SourceSection(payload, appACLR2FinalizeBeginMarker, appACLR2FinalizeEndMarker)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 finalizer source section: %w", err)
	}
	if _, err := appACLR2FinalizeExecutableSQL(section); err != nil {
		return fmt.Errorf("validate APP ACL R2 finalizer source bounds: %w", err)
	}
	return nil
}

func executeAppACLR2FinalizeSQLInTx(ctx context.Context, tx pgx.Tx, section []byte) error {
	sql, err := appACLR2FinalizeExecutableSQL(section)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("execute APP ACL R2 finalizer SQL: %w", err)
	}
	return nil
}

func appACLR2FinalizeExecutableSQL(section []byte) ([]byte, error) {
	beginLine := []byte(appACLR2FinalizeBeginMarker + "\n")
	endLine := []byte("-- " + appACLR2FinalizeEndMarker + "\n")
	if !bytes.HasPrefix(section, beginLine) || !bytes.HasSuffix(section, endLine) || len(section) <= len(beginLine)+len(endLine) {
		return nil, fmt.Errorf("APP ACL R2 finalizer source section markers are malformed")
	}
	if len(section)-len(appACLR2FinalizeEndCommentPrefix) != appACLR2FinalizeCanonicalSectionBytes {
		return nil, fmt.Errorf("APP ACL R2 finalizer source section has %d logical bytes, want %d", len(section)-len(appACLR2FinalizeEndCommentPrefix), appACLR2FinalizeCanonicalSectionBytes)
	}
	payload := append([]byte(nil), section[len(beginLine):len(section)-len(endLine)]...)
	if len(payload) != appACLR2FinalizeCanonicalBodyBytes {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL payload has %d bytes, want %d", len(payload), appACLR2FinalizeCanonicalBodyBytes)
	}
	if len(bytes.TrimSpace(payload)) == 0 || bytes.Contains(payload, []byte(appACLR2BootstrapBeginMarker)) || bytes.Contains(payload, []byte(appACLR2BootstrapEndMarker)) ||
		bytes.Contains(payload, []byte(appACLR2FinalizeBeginMarker)) || bytes.Contains(payload, []byte(appACLR2FinalizeEndMarker)) {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL payload is not marker-exclusive")
	}
	createStatements := append([]byte{'\n'}, payload...)
	if got := bytes.Count(createStatements, []byte("\ncreate function ")); got != 1 {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL has %d top-level functions, want 1", got)
	}
	if got := bytes.Count(createStatements, []byte("\ncreate table ")); got != 2 {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL has %d top-level tables, want 2", got)
	}
	if got := bytes.Count(createStatements, []byte("\ncreate trigger ")); got != 2 {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL has %d top-level triggers, want 2", got)
	}
	if got := bytes.Count(createStatements, []byte("\ncreate ")); got != 5 {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL has %d top-level CREATE statements, want 5", got)
	}
	if !bytes.Contains(payload, []byte("create function record_platform_internal.app_acl_r2_reject_manifest_mutation()\nreturns trigger\n")) ||
		!bytes.Contains(payload, []byte("create table public.app_acl_r2_manifest_revisions (\n")) ||
		!bytes.Contains(payload, []byte("create table public.app_acl_r2_manifest_head (\n")) ||
		!bytes.Contains(payload, []byte("create trigger app_acl_r2_manifest_revisions_immutable\n")) ||
		!bytes.Contains(payload, []byte("create trigger app_acl_r2_manifest_head_immutable\n")) {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL top-level DDL shape is not canonical")
	}
	if got := sha256.Sum256(payload); got != appACLR2FinalizeCanonicalBodyDigest {
		return nil, fmt.Errorf("APP ACL R2 finalizer SQL body digest is not canonical")
	}
	return payload, nil
}

func applyAppACLR2FinalizeM2ControlACLInTx(ctx context.Context, tx pgx.Tx, state FrozenAppACLR1StateV1) error {
	roles := []string{state.DirectMigratorRole, state.CenterRuntimeRole, state.PlatformAdminRole}
	for _, role := range roles {
		if !validAppACLR2RoleName(role) {
			return fmt.Errorf("APP ACL R2 finalizer has invalid control role")
		}
	}
	if roles[0] == roles[1] || roles[0] == roles[2] || roles[1] == roles[2] {
		return fmt.Errorf("APP ACL R2 finalizer control roles are not distinct")
	}

	var bootstrapRole string
	if err := tx.QueryRow(ctx, `
		select role.rolname::text
		from pg_catalog.pg_roles role
		where role.oid = 10
	`).Scan(&bootstrapRole); err != nil {
		return fmt.Errorf("read APP ACL R2 finalizer bootstrap role: %w", err)
	}
	if !validAppACLR2RoleName(bootstrapRole) || bootstrapRole == roles[0] || bootstrapRole == roles[1] || bootstrapRole == roles[2] {
		return fmt.Errorf("APP ACL R2 finalizer bootstrap role is invalid or overlaps a control role")
	}

	quote := func(role string) string { return pgx.Identifier{role}.Sanitize() }
	grantees := "public, " + quote(bootstrapRole) + ", " + quote(roles[0]) + ", " + quote(roles[1]) + ", " + quote(roles[2])
	statements := []string{
		"revoke all privileges on table public.app_acl_r2_manifest_revisions from " + grantees,
		"revoke all privileges on table public.app_acl_r2_manifest_head from " + grantees,
		"grant select on table public.app_acl_r2_manifest_revisions to " + quote(roles[0]),
		"grant select on table public.app_acl_r2_manifest_revisions to " + quote(roles[1]),
		"grant select on table public.app_acl_r2_manifest_head to " + quote(roles[0]),
		"grant select on table public.app_acl_r2_manifest_head to " + quote(roles[1]),
		"revoke all privileges on function record_platform_internal.app_acl_r2_reject_manifest_mutation() from " + grantees,
		"grant execute on function record_platform_internal.app_acl_r2_reject_manifest_mutation() to " + quote(roles[0]),
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply APP ACL R2 M2 control ACL: %w", err)
		}
	}
	return nil
}

func insertAppACLR2FinalizeM2RevisionInTx(
	ctx context.Context,
	tx pgx.Tx,
	manifest AppACLManifestR2V1,
	body []byte,
	digest [32]byte,
) error {
	canonical, err := CanonicalAppACLManifestR2BodyV1(manifest)
	if err != nil {
		return fmt.Errorf("validate APP ACL R2 M2 revision: %w", err)
	}
	if !bytes.Equal(canonical, body) || sha256.Sum256(body) != digest {
		return fmt.Errorf("APP ACL R2 M2 revision body or digest is not canonical")
	}
	recordedAt := time.UnixMicro(manifest.RecordedAtUnixMicroseconds).UTC()
	tag, err := tx.Exec(ctx, `
		insert into public.app_acl_r2_manifest_revisions (
			protocol_version,
			manifest_revision,
			m1_revision,
			m1_manifest_digest,
			m1_source_set_digest,
			m1_privilege_set_digest,
			m1_migrator_catalog_role,
			direct_migrator_name,
			direct_migrator_oid,
			r2_source_set_body,
			r2_source_set_digest,
			r2_privilege_set_body,
			r2_privilege_set_digest,
			domain_body,
			domain_digest,
			receipt_digest,
			control_acl_body,
			control_acl_digest,
			manifest_digest,
			recorded_at
		)
		select $3::smallint,
		       $4::bigint,
		       $1::bigint,
		       $2::bytea,
		       $5::bytea,
		       $6::bytea,
		       $7::text,
		       $8::text,
		       $9::oid,
		       $10::bytea,
		       $11::bytea,
		       $12::bytea,
		       $13::bytea,
		       $14::bytea,
		       $15::bytea,
		       $16::bytea,
		       $17::bytea,
		       $18::bytea,
		       $19::bytea,
		       $20::timestamptz
		from public.app_acl_manifest_revisions as frozen_m1
		where frozen_m1.manifest_revision = $1
		  and frozen_m1.manifest_digest = $2
	`,
		int64(manifest.M1Revision), manifest.M1ManifestDigest[:],
		int16(manifest.ProtocolVersion), int64(manifest.ManifestRevision),
		manifest.M1SourceSetDigest[:], manifest.M1PrivilegeSetDigest[:],
		manifest.M1MigratorCatalogRole, manifest.DirectMigratorName, manifest.DirectMigratorOID,
		manifest.R2SourceSetBody, manifest.R2SourceSetDigest[:],
		manifest.R2PrivilegeSetBody, manifest.R2PrivilegeSetDigest[:],
		manifest.DomainBody, manifest.DomainDigest[:], manifest.ReceiptDigest[:],
		manifest.ControlACLBody, manifest.ControlACLDigest[:], digest[:], recordedAt,
	)
	if err != nil {
		return fmt.Errorf("insert APP ACL R2 M2 revision: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("APP ACL R2 M2 revision frozen-M1 CAS did not insert exactly one row")
	}
	return nil
}

func compareAndSwapAppACLR2FinalizeM2HeadInTx(
	ctx context.Context,
	tx pgx.Tx,
	manifest AppACLManifestR2V1,
	digest [32]byte,
) error {
	canonical, err := CanonicalAppACLManifestR2BodyV1(manifest)
	if err != nil {
		return fmt.Errorf("validate APP ACL R2 M2 head manifest: %w", err)
	}
	if sha256.Sum256(canonical) != digest {
		return fmt.Errorf("APP ACL R2 M2 head manifest digest is not canonical")
	}
	tag, err := tx.Exec(ctx, `
		insert into public.app_acl_r2_manifest_head (
			singleton,
			protocol_version,
			manifest_revision,
			manifest_digest
		)
		select true, $1::smallint, $2::bigint, $3::bytea
		where not exists (
			select 1
			from public.app_acl_r2_manifest_head
		)
	`, int16(manifest.ProtocolVersion), int64(manifest.ManifestRevision), digest[:])
	if err != nil {
		return fmt.Errorf("insert APP ACL R2 M2 head: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("APP ACL R2 M2 head CAS did not insert exactly one row")
	}
	return nil
}

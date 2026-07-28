package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AppACLR2CatalogPredicates are reusable read-only catalog facts. The state
// classifier composes these facts without repeating their catalog checks, and
// later transition writers can gate their target state on the same predicates.
type AppACLR2CatalogPredicates struct {
	FrozenState               FrozenAppACLR1StateV1
	ExactL1M1                 bool
	ExactL2                   bool
	L2Absent                  bool
	ExactM2                   bool
	M2Absent                  bool
	HasUnknownReservedObjects bool
}

// ExactR1 reports whether the evidence is exactly frozen R1 with no R2 object.
func (predicates AppACLR2CatalogPredicates) ExactR1() bool {
	return predicates.ExactL1M1 && predicates.L2Absent && predicates.M2Absent && !predicates.ExactL2 && !predicates.ExactM2 && !predicates.HasUnknownReservedObjects
}

// ExactPrepared reports whether the evidence is exactly prepared, without M2.
func (predicates AppACLR2CatalogPredicates) ExactPrepared() bool {
	return predicates.ExactL1M1 && predicates.ExactL2 && !predicates.L2Absent && predicates.M2Absent && !predicates.ExactM2 && !predicates.HasUnknownReservedObjects
}

// ExactFinalized reports whether the evidence is exactly finalized.
func (predicates AppACLR2CatalogPredicates) ExactFinalized() bool {
	return predicates.ExactL1M1 && predicates.ExactL2 && !predicates.L2Absent && predicates.ExactM2 && !predicates.M2Absent && !predicates.HasUnknownReservedObjects
}

type appACLR2ReceiptRowV1 struct {
	Singleton bool
	Body      []byte
	Digest    [32]byte
}

type appACLR2ManifestRowV1 struct {
	Manifest       AppACLManifestR2V1
	ManifestDigest [32]byte
}

type appACLR2ManifestHeadRowV1 struct {
	Singleton        bool
	ProtocolVersion  uint16
	ManifestRevision uint64
	ManifestDigest   [32]byte
}

type appACLR2CatalogShape struct {
	FrozenExact            bool
	FrozenState            FrozenAppACLR1StateV1
	ReservedObjects        []AppACLR2ReservedCatalogObjectV1
	CatalogStructuralDrift bool
	L2Rows                 []appACLR2ReceiptRowV1
	L2EvidenceExact        bool
	M2Revisions            []appACLR2ManifestRowV1
	M2Heads                []appACLR2ManifestHeadRowV1
	M2ControlACL           AppACLControlACLBodyR2V1
}

type appACLR2CatalogPredicateReadDependencies struct {
	verifyFrozen        func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error)
	readReservedObjects func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error)
	readL2Rows          func(context.Context, pgx.Tx) ([]appACLR2ReceiptRowV1, error)
	verifyL2            func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error
	readM2Revisions     func(context.Context, pgx.Tx) ([]appACLR2ManifestRowV1, error)
	readM2Heads         func(context.Context, pgx.Tx) ([]appACLR2ManifestHeadRowV1, error)
	readM2ControlACL    func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLControlACLBodyR2V1, error)
}

// appACLR2CatalogStructuralDrift marks catalog evidence that was read
// successfully but does not meet the exact L1/M1/L2/M2 contract.
type appACLR2CatalogStructuralDrift struct {
	err error
}

func (drift *appACLR2CatalogStructuralDrift) Error() string {
	return drift.err.Error()
}

func (drift *appACLR2CatalogStructuralDrift) Unwrap() error {
	return drift.err
}

func appACLR2CatalogDrift(err error) error {
	return &appACLR2CatalogStructuralDrift{err: err}
}

func appACLR2IsCatalogDrift(err error) bool {
	var drift *appACLR2CatalogStructuralDrift
	return errors.As(err, &drift)
}

// appACLR2CatalogOperationalError preserves the original query, scan,
// iteration, context, and permission error while distinguishing it from a
// successfully read structural mismatch.
type appACLR2CatalogOperationalError struct {
	err error
}

func (operation *appACLR2CatalogOperationalError) Error() string {
	return operation.err.Error()
}

func (operation *appACLR2CatalogOperationalError) Unwrap() error {
	return operation.err
}

func appACLR2CatalogOperation(err error) error {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return &appACLR2CatalogOperationalError{err: err}
}

func appACLR2IsCatalogOperation(err error) bool {
	var operation *appACLR2CatalogOperationalError
	return errors.As(err, &operation)
}

type appACLR2CatalogOperationalTx struct {
	pgx.Tx
}

func (tx *appACLR2CatalogOperationalTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, arguments...)
	return tag, appACLR2CatalogOperation(err)
}

func (tx *appACLR2CatalogOperationalTx) Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, arguments...)
	if err != nil {
		return nil, appACLR2CatalogOperation(err)
	}
	return &appACLR2CatalogOperationalRows{Rows: rows}, nil
}

func (tx *appACLR2CatalogOperationalTx) QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row {
	return appACLR2CatalogOperationalRow{Row: tx.Tx.QueryRow(ctx, sql, arguments...)}
}

type appACLR2CatalogOperationalRows struct {
	pgx.Rows
}

func (rows *appACLR2CatalogOperationalRows) Err() error {
	return appACLR2CatalogOperation(rows.Rows.Err())
}

func (rows *appACLR2CatalogOperationalRows) Scan(destinations ...any) error {
	return appACLR2CatalogOperation(rows.Rows.Scan(destinations...))
}

type appACLR2CatalogOperationalRow struct {
	pgx.Row
}

func (row appACLR2CatalogOperationalRow) Scan(destinations ...any) error {
	return appACLR2CatalogOperation(row.Row.Scan(destinations...))
}

func appACLR2CatalogOperationalTransaction(tx pgx.Tx) pgx.Tx {
	return &appACLR2CatalogOperationalTx{Tx: tx}
}

// appACLR2CatalogFinishRows closes the query on every return path and gives a
// trailing completion error precedence over a successfully read structural
// mismatch. A preceding operational query or scan error remains the first
// operational failure.
func appACLR2CatalogFinishRows(rows pgx.Rows, resultErr *error, message string) {
	rows.Close()
	completionErr := rows.Err()
	if completionErr == nil || appACLR2IsCatalogOperation(*resultErr) || errors.Is(*resultErr, completionErr) {
		return
	}
	*resultErr = fmt.Errorf("%s: %w", message, completionErr)
}

func verifyAppACLR2FrozenCatalogPredicatesInTx(ctx context.Context, tx pgx.Tx) (FrozenAppACLR1StateV1, error) {
	state, err := VerifyFrozenAppACLR1StateInTx(ctx, appACLR2CatalogOperationalTransaction(tx))
	if err == nil || appACLR2IsCatalogOperation(err) {
		return state, err
	}
	return FrozenAppACLR1StateV1{}, appACLR2CatalogDrift(err)
}

func readAppACLR2ReservedCatalogObjectsForPredicatesInTx(ctx context.Context, tx pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
	objects, err := readAppACLR2ReservedCatalogObjectsInTx(ctx, appACLR2CatalogOperationalTransaction(tx))
	if err == nil || appACLR2IsCatalogOperation(err) {
		return objects, err
	}
	return nil, appACLR2CatalogDrift(err)
}

func readAppACLR2ReceiptRowsForPredicatesInTx(ctx context.Context, tx pgx.Tx) ([]appACLR2ReceiptRowV1, error) {
	operationalTx := appACLR2CatalogOperationalTransaction(tx)
	if err := readAppACLR2EvidenceRelationAuthorityInTx(ctx, operationalTx, "app_acl_r2_bootstrap_receipt"); err != nil {
		if appACLR2IsCatalogOperation(err) {
			return nil, err
		}
		return nil, appACLR2CatalogDrift(err)
	}
	rows, err := readAppACLR2ReceiptRowsInTx(ctx, operationalTx)
	if err == nil || appACLR2IsCatalogOperation(err) {
		return rows, err
	}
	return nil, appACLR2CatalogDrift(err)
}

func verifyAppACLR2L2EvidenceForPredicatesInTx(
	ctx context.Context,
	tx pgx.Tx,
	frozen FrozenAppACLR1StateV1,
	row appACLR2ReceiptRowV1,
) error {
	err := verifyAppACLR2L2EvidenceInTx(ctx, appACLR2CatalogOperationalTransaction(tx), frozen, row)
	if err == nil || appACLR2IsCatalogOperation(err) || appACLR2IsCatalogDrift(err) {
		return err
	}
	return appACLR2CatalogDrift(err)
}

func readAppACLR2ManifestRowsForPredicatesInTx(ctx context.Context, tx pgx.Tx) ([]appACLR2ManifestRowV1, error) {
	operationalTx := appACLR2CatalogOperationalTransaction(tx)
	if err := readAppACLR2EvidenceRelationAuthorityInTx(ctx, operationalTx, "app_acl_r2_manifest_revisions"); err != nil {
		if appACLR2IsCatalogOperation(err) {
			return nil, err
		}
		return nil, appACLR2CatalogDrift(err)
	}
	rows, err := readAppACLR2ManifestRowsInTx(ctx, operationalTx)
	if err == nil || appACLR2IsCatalogOperation(err) {
		return rows, err
	}
	return nil, appACLR2CatalogDrift(err)
}

func readAppACLR2ManifestHeadRowsForPredicatesInTx(ctx context.Context, tx pgx.Tx) ([]appACLR2ManifestHeadRowV1, error) {
	operationalTx := appACLR2CatalogOperationalTransaction(tx)
	if err := readAppACLR2EvidenceRelationAuthorityInTx(ctx, operationalTx, "app_acl_r2_manifest_head"); err != nil {
		if appACLR2IsCatalogOperation(err) {
			return nil, err
		}
		return nil, appACLR2CatalogDrift(err)
	}
	rows, err := readAppACLR2ManifestHeadRowsInTx(ctx, operationalTx)
	if err == nil || appACLR2IsCatalogOperation(err) {
		return rows, err
	}
	return nil, appACLR2CatalogDrift(err)
}

func readAppACLR2EvidenceRelationAuthorityInTx(ctx context.Context, tx pgx.Tx, relation string) (err error) {
	var query string
	switch relation {
	case "app_acl_r2_bootstrap_receipt":
		query = `select * from public.app_acl_r2_bootstrap_receipt limit 0`
	case "app_acl_r2_manifest_revisions":
		query = `select * from public.app_acl_r2_manifest_revisions limit 0`
	case "app_acl_r2_manifest_head":
		query = `select * from public.app_acl_r2_manifest_head limit 0`
	default:
		return appACLR2CatalogDrift(fmt.Errorf("APP ACL R2 evidence relation %q is not allowlisted", relation))
	}
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return err
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 evidence authority probe")
	for rows.Next() {
		return appACLR2CatalogDrift(fmt.Errorf("APP ACL R2 evidence authority probe unexpectedly returned a row"))
	}
	return rows.Err()
}

func readAppACLR2M2ControlACLForPredicatesInTx(ctx context.Context, tx pgx.Tx, frozen FrozenAppACLR1StateV1) (AppACLControlACLBodyR2V1, error) {
	controlACL, err := readAppACLR2M2ControlACLInTx(ctx, appACLR2CatalogOperationalTransaction(tx), frozen)
	if err == nil || appACLR2IsCatalogOperation(err) {
		return controlACL, err
	}
	return AppACLControlACLBodyR2V1{}, appACLR2CatalogDrift(err)
}

// ReadAppACLR2CatalogPredicatesInTx reads and verifies the transition catalog
// through the caller-owned transaction. It never opens, commits, or rolls back
// a transaction; its sole transaction-local GUC mutation pins search_path
// before the first catalog observation. It deliberately does not inspect
// connection identity. Structural mismatches become false predicates; query,
// scan, context, and permission failures remain errors.
func ReadAppACLR2CatalogPredicatesInTx(ctx context.Context, tx pgx.Tx) (AppACLR2CatalogPredicates, error) {
	if tx == nil {
		return AppACLR2CatalogPredicates{}, fmt.Errorf("APP ACL R2 catalog predicate reader has no transaction")
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path = pg_catalog, public"); err != nil {
		return AppACLR2CatalogPredicates{}, fmt.Errorf("set local APP ACL R2 catalog reader search path: %w", err)
	}
	return readAppACLR2CatalogPredicatesInTxWithDependencies(ctx, tx, appACLR2CatalogPredicateReadDependencies{
		verifyFrozen:        verifyAppACLR2FrozenCatalogPredicatesInTx,
		readReservedObjects: readAppACLR2ReservedCatalogObjectsForPredicatesInTx,
		readL2Rows:          readAppACLR2ReceiptRowsForPredicatesInTx,
		verifyL2:            verifyAppACLR2L2EvidenceForPredicatesInTx,
		readM2Revisions:     readAppACLR2ManifestRowsForPredicatesInTx,
		readM2Heads:         readAppACLR2ManifestHeadRowsForPredicatesInTx,
		readM2ControlACL:    readAppACLR2M2ControlACLForPredicatesInTx,
	})
}

func readAppACLR2CatalogPredicatesInTxWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	dependencies appACLR2CatalogPredicateReadDependencies,
) (AppACLR2CatalogPredicates, error) {
	if tx == nil {
		return AppACLR2CatalogPredicates{}, fmt.Errorf("APP ACL R2 catalog predicate reader has no transaction")
	}
	if dependencies.verifyFrozen == nil || dependencies.readReservedObjects == nil || dependencies.readL2Rows == nil || dependencies.verifyL2 == nil ||
		dependencies.readM2Revisions == nil || dependencies.readM2Heads == nil || dependencies.readM2ControlACL == nil {
		return AppACLR2CatalogPredicates{}, fmt.Errorf("APP ACL R2 catalog predicate reader dependencies are incomplete")
	}

	frozen, err := dependencies.verifyFrozen(ctx, tx)
	frozenExact := err == nil
	if err != nil {
		if !appACLR2IsCatalogDrift(err) {
			return AppACLR2CatalogPredicates{}, fmt.Errorf("verify frozen APP ACL R1 catalog evidence: %w", err)
		}
		frozen = FrozenAppACLR1StateV1{}
	}
	objects, err := dependencies.readReservedObjects(ctx, tx)
	catalogStructuralDrift := false
	if err != nil {
		if !appACLR2IsCatalogDrift(err) {
			return AppACLR2CatalogPredicates{}, fmt.Errorf("read APP ACL R2 reserved catalog objects: %w", err)
		}
		objects = nil
		catalogStructuralDrift = true
	}
	shape := appACLR2CatalogShape{
		FrozenExact:            frozenExact,
		FrozenState:            frozen,
		ReservedObjects:        objects,
		CatalogStructuralDrift: catalogStructuralDrift,
	}
	if appACLR2ReservedObjectExists(objects, appACLR2L2ReceiptRelation()) {
		shape.L2Rows, err = dependencies.readL2Rows(ctx, tx)
		if err != nil {
			if !appACLR2IsCatalogDrift(err) {
				return AppACLR2CatalogPredicates{}, fmt.Errorf("read APP ACL R2 L2 receipt rows: %w", err)
			}
			shape.L2Rows = nil
		}
		if shape.FrozenExact && appACLR2ExactL2Rows(shape.L2Rows) {
			if err := dependencies.verifyL2(ctx, tx, frozen, shape.L2Rows[0]); err != nil {
				if !appACLR2IsCatalogDrift(err) {
					return AppACLR2CatalogPredicates{}, fmt.Errorf("verify APP ACL R2 L2 receipt evidence: %w", err)
				}
			} else {
				shape.L2EvidenceExact = true
			}
		}
	}
	if appACLR2ReservedObjectExists(objects, appACLR2M2RevisionsRelation()) {
		shape.M2Revisions, err = dependencies.readM2Revisions(ctx, tx)
		if err != nil {
			if !appACLR2IsCatalogDrift(err) {
				return AppACLR2CatalogPredicates{}, fmt.Errorf("read APP ACL R2 M2 revisions evidence: %w", err)
			}
			shape.M2Revisions = nil
		}
	}
	if appACLR2ReservedObjectExists(objects, appACLR2M2HeadRelation()) {
		shape.M2Heads, err = dependencies.readM2Heads(ctx, tx)
		if err != nil {
			if !appACLR2IsCatalogDrift(err) {
				return AppACLR2CatalogPredicates{}, fmt.Errorf("read APP ACL R2 M2 head evidence: %w", err)
			}
			shape.M2Heads = nil
		}
	}
	if shape.FrozenExact && appACLR2ReservedObjectsContain(objects, appACLR2M2ReservedObjects()) {
		shape.M2ControlACL, err = dependencies.readM2ControlACL(ctx, tx, frozen)
		if err != nil && !appACLR2IsCatalogDrift(err) {
			return AppACLR2CatalogPredicates{}, fmt.Errorf("read APP ACL R2 M2 control catalog evidence: %w", err)
		}
	}
	return evaluateAppACLR2CatalogShape(shape), nil
}

func readAppACLR2ReceiptRowsInTx(ctx context.Context, tx pgx.Tx) (result []appACLR2ReceiptRowV1, err error) {
	rows, err := tx.Query(ctx, `
		select singleton,
		       coalesce(pg_catalog.octet_length(receipt_body), -1)::bigint,
		       case when pg_catalog.octet_length(receipt_body) between 1 and $1::integer
		            then receipt_body else null::bytea end,
		       coalesce(pg_catalog.octet_length(receipt_digest), -1)::bigint,
		       case when pg_catalog.octet_length(receipt_digest) = 32
		            then receipt_digest else null::bytea end
		from public.app_acl_r2_bootstrap_receipt
		limit 2
	`, appACLR2MaximumBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 receipt rows: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 receipt rows")

	result = make([]appACLR2ReceiptRowV1, 0, 1)
	for len(result) < 2 && rows.Next() {
		var row appACLR2ReceiptRowV1
		var bodySize, digestSize int64
		var digest []byte
		if err := rows.Scan(&row.Singleton, &bodySize, &row.Body, &digestSize, &digest); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 receipt row: %w", err)
		}
		if err := appACLR2CatalogBodySizeWithinBounds("receipt", bodySize); err != nil {
			return nil, err
		}
		parsedDigest, err := appACLR2CatalogDigestFromObservedBytes("APP ACL R2 receipt digest", digestSize, digest)
		if err != nil {
			return nil, err
		}
		row.Digest = parsedDigest
		row.Body = append([]byte(nil), row.Body...)
		result = append(result, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 receipt rows: %w", err)
	}
	return result, nil
}

func verifyAppACLR2L2EvidenceInTx(
	ctx context.Context,
	tx pgx.Tx,
	frozen FrozenAppACLR1StateV1,
	row appACLR2ReceiptRowV1,
) error {
	receipt, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(row.Body)
	if err != nil {
		return appACLR2CatalogDrift(fmt.Errorf("parse APP ACL R2 receipt evidence: %w", err))
	}
	bootstrap, err := ReadAppACLR2BootstrapCatalogSnapshotInTx(ctx, tx, frozen)
	if err != nil {
		return err
	}
	surface, err := ReadAppACLR2ReceiptCatalogSnapshotInTx(ctx, tx, frozen)
	if err != nil {
		return err
	}
	surface.ReservedObjects = appACLR2FilterReservedObjects(surface.ReservedObjects, appACLR2L2ReservedObjects())
	if err := VerifyAppACLR2BootstrapReceiptCatalogV1(receipt, bootstrap, surface, frozen); err != nil {
		return appACLR2CatalogDrift(err)
	}
	return nil
}

func appACLR2FilterReservedObjects(
	objects []AppACLR2ReservedCatalogObjectV1,
	want []AppACLR2ReservedCatalogObjectV1,
) []AppACLR2ReservedCatalogObjectV1 {
	wantKeys := make(map[string]struct{}, len(want))
	for _, object := range want {
		wantKeys[appACLR2ReservedObjectKey(object)] = struct{}{}
	}
	result := make([]AppACLR2ReservedCatalogObjectV1, 0, len(want))
	for _, object := range objects {
		if _, exists := wantKeys[appACLR2ReservedObjectKey(object)]; exists {
			result = append(result, object)
		}
	}
	return result
}

const appACLR2M2ManifestRevisionQuery = `
	select protocol_version,
	       manifest_revision,
	       m1_revision,
	       coalesce(pg_catalog.octet_length(m1_manifest_digest), -1)::bigint,
	       case when pg_catalog.octet_length(m1_manifest_digest) = 32
	            then m1_manifest_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(m1_source_set_digest), -1)::bigint,
	       case when pg_catalog.octet_length(m1_source_set_digest) = 32
	            then m1_source_set_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(m1_privilege_set_digest), -1)::bigint,
	       case when pg_catalog.octet_length(m1_privilege_set_digest) = 32
	            then m1_privilege_set_digest else null::bytea end,
	       m1_migrator_catalog_role,
	       direct_migrator_name,
	       direct_migrator_oid::bigint,
	       coalesce(pg_catalog.octet_length(r2_source_set_body), -1)::bigint,
	       case when pg_catalog.octet_length(r2_source_set_body) between 1 and $1::integer
	            then r2_source_set_body else null::bytea end,
	       coalesce(pg_catalog.octet_length(r2_source_set_digest), -1)::bigint,
	       case when pg_catalog.octet_length(r2_source_set_digest) = 32
	            then r2_source_set_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(r2_privilege_set_body), -1)::bigint,
	       case when pg_catalog.octet_length(r2_privilege_set_body) between 1 and $1::integer
	            then r2_privilege_set_body else null::bytea end,
	       coalesce(pg_catalog.octet_length(r2_privilege_set_digest), -1)::bigint,
	       case when pg_catalog.octet_length(r2_privilege_set_digest) = 32
	            then r2_privilege_set_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(domain_body), -1)::bigint,
	       case when pg_catalog.octet_length(domain_body) between 1 and $1::integer
	            then domain_body else null::bytea end,
	       coalesce(pg_catalog.octet_length(domain_digest), -1)::bigint,
	       case when pg_catalog.octet_length(domain_digest) = 32
	            then domain_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(receipt_digest), -1)::bigint,
	       case when pg_catalog.octet_length(receipt_digest) = 32
	            then receipt_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(control_acl_body), -1)::bigint,
	       case when pg_catalog.octet_length(control_acl_body) between 1 and $1::integer
	            then control_acl_body else null::bytea end,
	       coalesce(pg_catalog.octet_length(control_acl_digest), -1)::bigint,
	       case when pg_catalog.octet_length(control_acl_digest) = 32
	            then control_acl_digest else null::bytea end,
	       coalesce(pg_catalog.octet_length(manifest_digest), -1)::bigint,
	       case when pg_catalog.octet_length(manifest_digest) = 32
	            then manifest_digest else null::bytea end,
	       (extract(epoch from recorded_at) * 1000000)::bigint
	from public.app_acl_r2_manifest_revisions
	limit 2
`

func readAppACLR2ManifestRowsInTx(ctx context.Context, tx pgx.Tx) (result []appACLR2ManifestRowV1, err error) {
	rows, err := tx.Query(ctx, appACLR2M2ManifestRevisionQuery, appACLR2MaximumBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 manifest revisions: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 manifest revisions")

	result = make([]appACLR2ManifestRowV1, 0, 1)
	for len(result) < 2 && rows.Next() {
		var protocolVersion, manifestRevision, m1Revision, directMigratorOID int64
		var r2SourceSetBodySize, r2PrivilegeSetBodySize, domainBodySize, controlACLBodySize int64
		var m1ManifestDigestSize, m1SourceSetDigestSize, m1PrivilegeSetDigestSize int64
		var r2SourceSetDigestSize, r2PrivilegeSetDigestSize, domainDigestSize int64
		var receiptDigestSize, controlACLDigestSize, manifestDigestSize int64
		var m1ManifestDigest, m1SourceSetDigest, m1PrivilegeSetDigest []byte
		var r2SourceSetDigest, r2PrivilegeSetDigest, domainDigest, receiptDigest, controlACLDigest, manifestDigest []byte
		var row appACLR2ManifestRowV1
		if err := rows.Scan(
			&protocolVersion,
			&manifestRevision,
			&m1Revision,
			&m1ManifestDigestSize,
			&m1ManifestDigest,
			&m1SourceSetDigestSize,
			&m1SourceSetDigest,
			&m1PrivilegeSetDigestSize,
			&m1PrivilegeSetDigest,
			&row.Manifest.M1MigratorCatalogRole,
			&row.Manifest.DirectMigratorName,
			&directMigratorOID,
			&r2SourceSetBodySize,
			&row.Manifest.R2SourceSetBody,
			&r2SourceSetDigestSize,
			&r2SourceSetDigest,
			&r2PrivilegeSetBodySize,
			&row.Manifest.R2PrivilegeSetBody,
			&r2PrivilegeSetDigestSize,
			&r2PrivilegeSetDigest,
			&domainBodySize,
			&row.Manifest.DomainBody,
			&domainDigestSize,
			&domainDigest,
			&receiptDigestSize,
			&receiptDigest,
			&controlACLBodySize,
			&row.Manifest.ControlACLBody,
			&controlACLDigestSize,
			&controlACLDigest,
			&manifestDigestSize,
			&manifestDigest,
			&row.Manifest.RecordedAtUnixMicroseconds,
		); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 manifest revision: %w", err)
		}
		for _, body := range []struct {
			name string
			size int64
		}{
			{name: "M2 R2 source-set", size: r2SourceSetBodySize},
			{name: "M2 R2 privilege-set", size: r2PrivilegeSetBodySize},
			{name: "M2 domain", size: domainBodySize},
			{name: "M2 control-ACL", size: controlACLBodySize},
		} {
			if err := appACLR2CatalogBodySizeWithinBounds(body.name, body.size); err != nil {
				return nil, err
			}
		}
		var err error
		if row.Manifest.ProtocolVersion, err = appACLR2CatalogUint16(protocolVersion, "M2 protocol version"); err != nil {
			return nil, err
		}
		if row.Manifest.ManifestRevision, err = appACLR2CatalogUint64(manifestRevision, "M2 manifest revision"); err != nil {
			return nil, err
		}
		if row.Manifest.M1Revision, err = appACLR2CatalogUint64(m1Revision, "M2 M1 revision"); err != nil {
			return nil, err
		}
		if row.Manifest.DirectMigratorOID, err = appACLR2CatalogUint32(directMigratorOID, "M2 direct migrator OID"); err != nil {
			return nil, err
		}
		if row.Manifest.M1ManifestDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 M1 manifest digest", m1ManifestDigestSize, m1ManifestDigest); err != nil {
			return nil, err
		}
		if row.Manifest.M1SourceSetDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 M1 source-set digest", m1SourceSetDigestSize, m1SourceSetDigest); err != nil {
			return nil, err
		}
		if row.Manifest.M1PrivilegeSetDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 M1 privilege-set digest", m1PrivilegeSetDigestSize, m1PrivilegeSetDigest); err != nil {
			return nil, err
		}
		if row.Manifest.R2SourceSetDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 R2 source-set digest", r2SourceSetDigestSize, r2SourceSetDigest); err != nil {
			return nil, err
		}
		if row.Manifest.R2PrivilegeSetDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 R2 privilege-set digest", r2PrivilegeSetDigestSize, r2PrivilegeSetDigest); err != nil {
			return nil, err
		}
		if row.Manifest.DomainDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 domain digest", domainDigestSize, domainDigest); err != nil {
			return nil, err
		}
		if row.Manifest.ReceiptDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 receipt digest", receiptDigestSize, receiptDigest); err != nil {
			return nil, err
		}
		if row.Manifest.ControlACLDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 control ACL digest", controlACLDigestSize, controlACLDigest); err != nil {
			return nil, err
		}
		if row.ManifestDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 manifest digest", manifestDigestSize, manifestDigest); err != nil {
			return nil, err
		}
		row.Manifest.R2SourceSetBody = append([]byte(nil), row.Manifest.R2SourceSetBody...)
		row.Manifest.R2PrivilegeSetBody = append([]byte(nil), row.Manifest.R2PrivilegeSetBody...)
		row.Manifest.DomainBody = append([]byte(nil), row.Manifest.DomainBody...)
		row.Manifest.ControlACLBody = append([]byte(nil), row.Manifest.ControlACLBody...)
		result = append(result, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 manifest revisions: %w", err)
	}
	return result, nil
}

func readAppACLR2ManifestHeadRowsInTx(ctx context.Context, tx pgx.Tx) (result []appACLR2ManifestHeadRowV1, err error) {
	rows, err := tx.Query(ctx, `
		select singleton,
		       protocol_version,
		       manifest_revision,
		       coalesce(pg_catalog.octet_length(manifest_digest), -1)::bigint,
		       case when pg_catalog.octet_length(manifest_digest) = 32
		            then manifest_digest else null::bytea end
		from public.app_acl_r2_manifest_head
		limit 2
	`)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 manifest heads: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 manifest heads")

	result = make([]appACLR2ManifestHeadRowV1, 0, 1)
	for len(result) < 2 && rows.Next() {
		var protocolVersion, manifestRevision, digestSize int64
		var digest []byte
		var row appACLR2ManifestHeadRowV1
		if err := rows.Scan(&row.Singleton, &protocolVersion, &manifestRevision, &digestSize, &digest); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 manifest head: %w", err)
		}
		var err error
		if row.ProtocolVersion, err = appACLR2CatalogUint16(protocolVersion, "M2 head protocol version"); err != nil {
			return nil, err
		}
		if row.ManifestRevision, err = appACLR2CatalogUint64(manifestRevision, "M2 head manifest revision"); err != nil {
			return nil, err
		}
		if row.ManifestDigest, err = appACLR2CatalogDigestFromObservedBytes("M2 head manifest digest", digestSize, digest); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 manifest heads: %w", err)
	}
	return result, nil
}

func appACLR2CatalogBodySizeWithinBounds(name string, size int64) error {
	if size < 1 || size > int64(appACLR2MaximumBodyBytes) {
		return fmt.Errorf("APP ACL R2 %s body size is outside bounds", name)
	}
	return nil
}

func appACLR2CatalogDigestFromObservedBytes(name string, size int64, digest []byte) ([sha256.Size]byte, error) {
	if size != sha256.Size {
		return [sha256.Size]byte{}, fmt.Errorf("%s size is not %d bytes", name, sha256.Size)
	}
	return appACLManifestDigestFromBytes(name, digest)
}

func appACLR2CatalogTextFromObservedBytes(name string, size int64, value []byte, maximum int) (string, error) {
	if size < 1 || size > int64(maximum) || int64(len(value)) != size {
		return "", fmt.Errorf("%s size is outside exact production bounds", name)
	}
	return string(value), nil
}

func appACLR2CatalogUint16(value int64, field string) (uint16, error) {
	if value < 0 || value > int64(^uint16(0)) {
		return 0, fmt.Errorf("%s is outside uint16 bounds", field)
	}
	return uint16(value), nil
}

func appACLR2CatalogUint64(value int64, field string) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s is negative", field)
	}
	return uint64(value), nil
}

type appACLR2M2RoleOIDs struct {
	DirectMigrator uint32
	CenterRuntime  uint32
	PlatformAdmin  uint32
}

func readAppACLR2M2ControlACLInTx(
	ctx context.Context,
	tx pgx.Tx,
	frozen FrozenAppACLR1StateV1,
) (AppACLControlACLBodyR2V1, error) {
	roles, err := readAppACLR2M2RoleOIDsInTx(ctx, tx, frozen)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if err := verifyAppACLR2M2RelationsInTx(ctx, tx, frozen, roles); err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if err := verifyAppACLR2M2FunctionInTx(ctx, tx, roles); err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if err := verifyAppACLR2M2TriggersInTx(ctx, tx, roles.DirectMigrator); err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if err := verifyAppACLR2M2DefaultACLAbsenceInTx(ctx, tx, roles.DirectMigrator); err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	return appACLR2ControlACLContract(roles.DirectMigrator), nil
}

func readAppACLR2M2RoleOIDsInTx(
	ctx context.Context,
	tx pgx.Tx,
	frozen FrozenAppACLR1StateV1,
) (roles appACLR2M2RoleOIDs, err error) {
	names := []string{frozen.DirectMigratorRole, frozen.CenterRuntimeRole, frozen.PlatformAdminRole}
	rows, err := tx.Query(ctx, `
		select rolname::text, oid::bigint
		from pg_catalog.pg_roles
		where rolname = any($1::name[])
		order by rolname
	`, names)
	if err != nil {
		return appACLR2M2RoleOIDs{}, fmt.Errorf("read APP ACL R2 M2 role OIDs: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 role OIDs")

	observed := make(map[string]uint32, len(names))
	for rows.Next() {
		var name string
		var oid int64
		if err := rows.Scan(&name, &oid); err != nil {
			return appACLR2M2RoleOIDs{}, fmt.Errorf("scan APP ACL R2 M2 role OID: %w", err)
		}
		parsedOID, err := appACLR2CatalogUint32(oid, "APP ACL R2 M2 role OID")
		if err != nil {
			return appACLR2M2RoleOIDs{}, err
		}
		if _, duplicate := observed[name]; duplicate {
			return appACLR2M2RoleOIDs{}, fmt.Errorf("APP ACL R2 M2 role %q is duplicated", name)
		}
		observed[name] = parsedOID
	}
	if err := rows.Err(); err != nil {
		return appACLR2M2RoleOIDs{}, fmt.Errorf("iterate APP ACL R2 M2 role OIDs: %w", err)
	}
	roles = appACLR2M2RoleOIDs{
		DirectMigrator: observed[frozen.DirectMigratorRole],
		CenterRuntime:  observed[frozen.CenterRuntimeRole],
		PlatformAdmin:  observed[frozen.PlatformAdminRole],
	}
	if len(observed) != len(names) || roles.DirectMigrator == 0 || roles.CenterRuntime == 0 || roles.PlatformAdmin == 0 ||
		roles.DirectMigrator == roles.CenterRuntime || roles.DirectMigrator == roles.PlatformAdmin || roles.CenterRuntime == roles.PlatformAdmin {
		return appACLR2M2RoleOIDs{}, fmt.Errorf("APP ACL R2 M2 control roles are incomplete or non-distinct")
	}
	return roles, nil
}

func verifyAppACLR2M2RelationsInTx(
	ctx context.Context,
	tx pgx.Tx,
	frozen FrozenAppACLR1StateV1,
	roles appACLR2M2RoleOIDs,
) (err error) {
	const head = "app_acl_r2_manifest_head"
	const revisions = "app_acl_r2_manifest_revisions"
	names := []string{head, revisions}
	rows, err := tx.Query(ctx, `
		select relation.relname::text, relation.relowner::bigint, relation.relkind::text
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = 'public'
		  and relation.relname = any($1::name[])
		order by relation.relname
	`, names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 relation owners: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation owners")
	seen := make(map[string]bool, len(names))
	for rows.Next() {
		var name, kind string
		var owner int64
		if err := rows.Scan(&name, &owner, &kind); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 relation owner: %w", err)
		}
		ownerOID, err := appACLR2CatalogUint32(owner, "APP ACL R2 M2 relation owner OID")
		if err != nil {
			return err
		}
		if (name != head && name != revisions) || seen[name] || kind != "r" || ownerOID != roles.DirectMigrator {
			return fmt.Errorf("APP ACL R2 M2 relation %q has catalog identity or owner drift", name)
		}
		seen[name] = true
	}
	appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation owners")
	if err != nil {
		return err
	}
	if len(seen) != len(names) {
		return fmt.Errorf("APP ACL R2 M2 relation catalog is incomplete")
	}
	if err := verifyAppACLR2M2RelationColumnACLsInTx(ctx, tx, names); err != nil {
		return err
	}
	if err := verifyAppACLR2M2RelationPhysicalContractInTx(ctx, tx, names); err != nil {
		return err
	}

	if err := verifyAppACLR2M2RelationGrantsInTx(ctx, tx, names, roles); err != nil {
		return err
	}
	return verifyAppACLR2M2RelationEffectivePrivilegesInTx(ctx, tx, names, roles)
}

func verifyAppACLR2M2RelationGrantsInTx(
	ctx context.Context,
	tx pgx.Tx,
	names []string,
	roles appACLR2M2RoleOIDs,
) (err error) {
	rows, err := tx.Query(ctx, `
		select relation.relname::text,
		       acl_grant.grantee::bigint,
		       acl_grant.privilege_type::text,
		       acl_grant.is_grantable
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		cross join lateral pg_catalog.aclexplode(relation.relacl) acl_grant
		where namespace.nspname = 'public'
		  and relation.relname = any($1::name[])
		order by relation.relname, acl_grant.grantee, acl_grant.privilege_type
	`, names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 relation grants: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation grants")
	grants := make(map[string]map[uint32]bool, len(names))
	for _, name := range names {
		grants[name] = make(map[uint32]bool, 2)
	}
	for rows.Next() {
		var name, privilege string
		var grantee int64
		var grantable bool
		if err := rows.Scan(&name, &grantee, &privilege, &grantable); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 relation grant: %w", err)
		}
		granteeOID, err := appACLR2CatalogUint32(grantee, "APP ACL R2 M2 relation grantee OID")
		if err != nil {
			return err
		}
		if _, knownRelation := grants[name]; !knownRelation ||
			(granteeOID != roles.DirectMigrator && granteeOID != roles.CenterRuntime) || privilege != "SELECT" || grantable {
			return fmt.Errorf("APP ACL R2 M2 relation %q has unexpected ACL grant", name)
		}
		if grants[name][granteeOID] {
			return fmt.Errorf("APP ACL R2 M2 relation %q has duplicate ACL grant", name)
		}
		grants[name][granteeOID] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate APP ACL R2 M2 relation grants: %w", err)
	}
	for _, name := range names {
		if len(grants[name]) != 2 || !grants[name][roles.DirectMigrator] || !grants[name][roles.CenterRuntime] {
			return fmt.Errorf("APP ACL R2 M2 relation %q does not have exact explicit SELECT grants", name)
		}
	}
	return nil
}

func verifyAppACLR2M2RelationColumnACLsInTx(ctx context.Context, tx pgx.Tx, names []string) (err error) {
	rows, err := tx.Query(ctx, `
		select relation.relname::text,
		       attribute.attname::text,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		join pg_catalog.pg_attribute attribute on attribute.attrelid = relation.oid
		cross join lateral pg_catalog.aclexplode(attribute.attacl)
		  as acl_entry(grantor, grantee, privilege_type, is_grantable)
		left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
		where namespace.nspname = 'public'
		  and relation.relname = any($1::name[])
		  and attribute.attnum > 0
		  and attribute.attacl is not null
		  and pg_catalog.cardinality(attribute.attacl) > 0
		order by relation.relname, attribute.attnum, grantee.rolname nulls first, acl_entry.privilege_type, acl_entry.is_grantable
	`, names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 relation column ACLs: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation column ACLs")
	if rows.Next() {
		var relationName, columnName, grantee, privilege string
		var grantOption bool
		if err := rows.Scan(&relationName, &columnName, &grantee, &privilege, &grantOption); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 relation column ACL: %w", err)
		}
		return fmt.Errorf("APP ACL R2 M2 relation %q column %q has column ACL drift for grantee %q", relationName, columnName, grantee)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate APP ACL R2 M2 relation column ACLs: %w", err)
	}
	return nil
}

func verifyAppACLR2M2RelationPhysicalContractInTx(ctx context.Context, tx pgx.Tx, names []string) (err error) {
	rows, err := tx.Query(ctx, `
		select relation.relname::text,
		       relation.relpersistence::text,
		       relation.relispartition,
		       relation.relrowsecurity,
		       relation.relforcerowsecurity
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = 'public'
		  and relation.relname = any($1::name[])
		order by relation.relname
	`, names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 relation physical catalog: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation physical catalog")
	seen := make(map[string]bool, len(names))
	for rows.Next() {
		var name, persistence string
		var partition, rowSecurity, forceRowSecurity bool
		if err := rows.Scan(&name, &persistence, &partition, &rowSecurity, &forceRowSecurity); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 relation physical catalog: %w", err)
		}
		if !appACLR2M2RelationNameKnown(name) || seen[name] || persistence != "p" || partition || rowSecurity || forceRowSecurity {
			return fmt.Errorf("APP ACL R2 M2 relation %q has persistence or physical catalog drift", name)
		}
		seen[name] = true
	}
	appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation physical catalog")
	if err != nil {
		return err
	}
	if len(seen) != len(names) {
		return fmt.Errorf("APP ACL R2 M2 relation physical catalog is incomplete")
	}

	if err := verifyAppACLR2M2RelationColumnsInTx(ctx, tx, names); err != nil {
		return err
	}
	if err := verifyAppACLR2M2RelationInheritanceInTx(ctx, tx, names); err != nil {
		return err
	}
	return verifyAppACLR2M2RelationConstraintsInTx(ctx, tx, names)
}

func appACLR2M2RelationNameKnown(name string) bool {
	return name == "app_acl_r2_manifest_head" || name == "app_acl_r2_manifest_revisions"
}

func verifyAppACLR2M2RelationColumnsInTx(ctx context.Context, tx pgx.Tx, names []string) (err error) {
	rows, err := tx.Query(ctx, `
		select relation.relname::text,
		       attribute.attname::text,
		       pg_catalog.format_type(attribute.atttypid, attribute.atttypmod),
		       attribute.attnotnull,
		       coalesce(pg_catalog.pg_get_expr(default_value.adbin, default_value.adrelid), '')
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		join pg_catalog.pg_attribute attribute on attribute.attrelid = relation.oid
		left join pg_catalog.pg_attrdef default_value
		  on default_value.adrelid = attribute.attrelid
		 and default_value.adnum = attribute.attnum
		where namespace.nspname = 'public'
		  and relation.relname = any($1::name[])
		  and attribute.attnum > 0
		order by relation.relname, attribute.attnum
	`, names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 relation columns: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation columns")
	observed := make(map[string][]AppACLR2ReceiptTableColumnCatalogV1, len(names))
	for rows.Next() {
		var relationName string
		var column AppACLR2ReceiptTableColumnCatalogV1
		if err := rows.Scan(&relationName, &column.Name, &column.Type, &column.NotNull, &column.DefaultExpression); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 relation column: %w", err)
		}
		if !appACLR2M2RelationNameKnown(relationName) {
			return fmt.Errorf("APP ACL R2 M2 column belongs to unknown relation %q", relationName)
		}
		observed[relationName] = append(observed[relationName], column)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate APP ACL R2 M2 relation columns: %w", err)
	}
	for _, name := range names {
		want := appACLR2M2RelationColumns(name)
		got := observed[name]
		if len(got) != len(want) {
			return fmt.Errorf("APP ACL R2 M2 relation %q has %d columns, want %d", name, len(got), len(want))
		}
		for index := range want {
			if got[index].Name != want[index].Name || got[index].Type != want[index].Type || got[index].NotNull != want[index].NotNull ||
				appACLR2NormalizeCatalogDefinition(got[index].DefaultExpression) != want[index].DefaultExpression {
				return fmt.Errorf("APP ACL R2 M2 relation %q column %d has shape drift", name, index)
			}
		}
	}
	return nil
}

func appACLR2M2RelationColumns(name string) []AppACLR2ReceiptTableColumnCatalogV1 {
	switch name {
	case "app_acl_r2_manifest_head":
		return []AppACLR2ReceiptTableColumnCatalogV1{
			{Name: "singleton", Type: "boolean", NotNull: true, DefaultExpression: "true"},
			{Name: "protocol_version", Type: "smallint", NotNull: true},
			{Name: "manifest_revision", Type: "bigint", NotNull: true},
			{Name: "manifest_digest", Type: "bytea", NotNull: true},
		}
	case "app_acl_r2_manifest_revisions":
		return []AppACLR2ReceiptTableColumnCatalogV1{
			{Name: "protocol_version", Type: "smallint", NotNull: true},
			{Name: "manifest_revision", Type: "bigint", NotNull: true},
			{Name: "m1_revision", Type: "bigint", NotNull: true},
			{Name: "m1_manifest_digest", Type: "bytea", NotNull: true},
			{Name: "m1_source_set_digest", Type: "bytea", NotNull: true},
			{Name: "m1_privilege_set_digest", Type: "bytea", NotNull: true},
			{Name: "m1_migrator_catalog_role", Type: "text", NotNull: true},
			{Name: "direct_migrator_name", Type: "text", NotNull: true},
			{Name: "direct_migrator_oid", Type: "oid", NotNull: true},
			{Name: "r2_source_set_body", Type: "bytea", NotNull: true},
			{Name: "r2_source_set_digest", Type: "bytea", NotNull: true},
			{Name: "r2_privilege_set_body", Type: "bytea", NotNull: true},
			{Name: "r2_privilege_set_digest", Type: "bytea", NotNull: true},
			{Name: "domain_body", Type: "bytea", NotNull: true},
			{Name: "domain_digest", Type: "bytea", NotNull: true},
			{Name: "receipt_digest", Type: "bytea", NotNull: true},
			{Name: "control_acl_body", Type: "bytea", NotNull: true},
			{Name: "control_acl_digest", Type: "bytea", NotNull: true},
			{Name: "manifest_digest", Type: "bytea", NotNull: true},
			{Name: "recorded_at", Type: "timestamp with time zone", NotNull: true, DefaultExpression: "transaction_timestamp()"},
		}
	default:
		return nil
	}
}

func verifyAppACLR2M2RelationInheritanceInTx(ctx context.Context, tx pgx.Tx, names []string) (err error) {
	rows, err := tx.Query(ctx, `
		select child.relname::text,
		       inheritance.inhrelid = child.oid,
		       inheritance.inhparent = child.oid
		from pg_catalog.pg_inherits inheritance
		join pg_catalog.pg_class child on child.oid = inheritance.inhrelid
		join pg_catalog.pg_namespace namespace on namespace.oid = child.relnamespace
		where namespace.nspname = 'public'
		  and child.relname = any($1::name[])
		union all
		select parent.relname::text,
		       inheritance.inhrelid = parent.oid,
		       inheritance.inhparent = parent.oid
		from pg_catalog.pg_inherits inheritance
		join pg_catalog.pg_class parent on parent.oid = inheritance.inhparent
		join pg_catalog.pg_namespace namespace on namespace.oid = parent.relnamespace
		where namespace.nspname = 'public'
		  and parent.relname = any($1::name[])
		order by 1, 2, 3
	`, names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 relation inheritance: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation inheritance")
	if rows.Next() {
		var name string
		var isChild, isParent bool
		if err := rows.Scan(&name, &isChild, &isParent); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 relation inheritance: %w", err)
		}
		return fmt.Errorf("APP ACL R2 M2 relation %q has inheritance drift", name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate APP ACL R2 M2 relation inheritance: %w", err)
	}
	return nil
}

func verifyAppACLR2M2RelationConstraintsInTx(ctx context.Context, tx pgx.Tx, names []string) error {
	definitionMaximum := appACLR2M2RelationConstraintDefinitionMaximum()
	for _, name := range names {
		expectedCount, err := appACLR2M2RelationConstraintExpectedCount(name)
		if err != nil {
			return err
		}
		constraints, err := readAppACLR2M2RelationConstraintsInTx(ctx, tx, name, definitionMaximum, expectedCount+1)
		if err != nil {
			return err
		}
		if err := validateAppACLR2M2RelationConstraints(name, constraints); err != nil {
			return err
		}
	}
	return nil
}

func readAppACLR2M2RelationConstraintsInTx(
	ctx context.Context,
	tx pgx.Tx,
	name string,
	definitionMaximum int,
	witnessLimit int,
) (constraints []AppACLR2ReceiptTableConstraintCatalogV1, err error) {
	// PG16: for primary/unique, conindid is the supporting index on conrelid.
	// For foreign keys, conindid is the referenced unique/PK index on confrelid.
	// The expected-index join binds each constraint OID to the exact allowlisted
	// index identity; flags from a renamed real index cannot be paired with an
	// unrelated index recreated under the reserved name.
	rows, err := tx.Query(ctx, `
		select relation.relname::text,
		       constraint_catalog.contype::text,
		       coalesce(pg_catalog.octet_length(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true)), -1)::bigint,
		       case when pg_catalog.octet_length(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true)) between 1 and $2::integer
		            then pg_catalog.convert_to(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true), 'UTF8'::name) else null::bytea end,
		       constraint_catalog.convalidated,
		       constraint_catalog.conindid::bigint,
		       coalesce(index_catalog.indisprimary, false),
		       coalesce(index_catalog.indisunique, false),
		       coalesce(index_catalog.indisvalid and constraint_catalog.conindid = expected_index.oid, false)
		from pg_catalog.pg_constraint constraint_catalog
		join pg_catalog.pg_class relation on relation.oid = constraint_catalog.conrelid
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		left join pg_catalog.pg_index index_catalog
		  on index_catalog.indexrelid = constraint_catalog.conindid
		 and (
		      (constraint_catalog.contype in ('p', 'u') and index_catalog.indrelid = constraint_catalog.conrelid)
		   or (constraint_catalog.contype = 'f' and index_catalog.indrelid = constraint_catalog.confrelid)
		 )
		left join pg_catalog.pg_class expected_index
		  on expected_index.relnamespace = namespace.oid
		 and expected_index.relkind = 'i'
		 and expected_index.relname = case
		       when relation.relname = 'app_acl_r2_manifest_head' and constraint_catalog.contype = 'p'
		         then 'app_acl_r2_manifest_head_pkey'
		       when relation.relname = 'app_acl_r2_manifest_head' and constraint_catalog.contype = 'f'
		         then 'app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key'
		       when relation.relname = 'app_acl_r2_manifest_revisions' and constraint_catalog.contype = 'p'
		         then 'app_acl_r2_manifest_revisions_pkey'
		       when relation.relname = 'app_acl_r2_manifest_revisions' and constraint_catalog.contype = 'u'
		         then 'app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key'
		       when relation.relname = 'app_acl_r2_manifest_revisions' and constraint_catalog.contype = 'f'
		         then 'app_acl_manifest_revisions_manifest_revision_manifest_diges_key'
		     end
		where namespace.nspname = 'public'
		  and relation.relname = $1::name
		limit $3::integer
	`, name, definitionMaximum, witnessLimit)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 M2 relation constraints: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 relation constraints")
	constraints = make([]AppACLR2ReceiptTableConstraintCatalogV1, 0, witnessLimit)
	for len(constraints) < witnessLimit && rows.Next() {
		var observedName string
		var constraint AppACLR2ReceiptTableConstraintCatalogV1
		var definitionSize, indexOID int64
		var definition []byte
		if err := rows.Scan(&observedName, &constraint.Type, &definitionSize, &definition, &constraint.Validated, &indexOID, &constraint.IndexPrimary, &constraint.IndexUnique, &constraint.IndexValid); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 M2 relation constraint: %w", err)
		}
		if observedName != name {
			return nil, fmt.Errorf("APP ACL R2 M2 relation constraint query returned unexpected relation %q", observedName)
		}
		constraint.Definition, err = appACLR2CatalogTextFromObservedBytes("APP ACL R2 M2 relation constraint definition", definitionSize, definition, definitionMaximum)
		if err != nil {
			return nil, err
		}
		if constraint.IndexOID, err = appACLR2CatalogOptionalOID(indexOID, "M2 relation constraint index OID"); err != nil {
			return nil, err
		}
		constraints = append(constraints, constraint)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 M2 relation constraints: %w", err)
	}
	return constraints, nil
}

func appACLR2M2RelationConstraintExpectedCount(name string) (int, error) {
	switch name {
	case "app_acl_r2_manifest_head":
		return 6, nil
	case "app_acl_r2_manifest_revisions":
		return 20, nil
	default:
		return 0, fmt.Errorf("APP ACL R2 M2 relation %q has no exact constraint contract", name)
	}
}

func appACLR2M2RelationConstraintDefinitionMaximum() int {
	// The canonical manifest-digest CHECK is the longest exact PG16 definition.
	// Keeping the raw production definition as the ceiling avoids an unrelated
	// free-form text threshold and makes any longer catalog value non-transferable.
	return len(appACLR2M2ManifestDigestConstraintDefinitionPG16)
}

func validateAppACLR2M2RelationConstraints(name string, constraints []AppACLR2ReceiptTableConstraintCatalogV1) error {
	if len(constraints) == 0 {
		return fmt.Errorf("APP ACL R2 M2 relation %q has no constraints", name)
	}
	canonicalManifestDigestCheck := appACLR2NormalizeCatalogDefinition(appACLR2M2ManifestDigestConstraintDefinitionPG16)
	// Exact expectations match PostgreSQL 16 pg_get_constraintdef under the
	// design-mandated search_path = pg_catalog, public: public relations deparse
	// without a public. qualifier, and text literals retain an explicit ::text cast.
	primaryKey := "primarykey(singleton)"
	foreignKey := "foreignkey(protocol_version,manifest_revision,manifest_digest)referencesapp_acl_r2_manifest_revisions(protocol_version,manifest_revision,manifest_digest)ondeleterestrict"
	uniqueKey := ""
	requiredChecks := []string{"check(singleton)", "check(protocol_version=2)", "check(manifest_revision=2)", "check(octet_length(manifest_digest)=32)"}
	if name == "app_acl_r2_manifest_revisions" {
		primaryKey = "primarykey(protocol_version,manifest_revision)"
		foreignKey = "foreignkey(m1_revision,m1_manifest_digest)referencesapp_acl_manifest_revisions(manifest_revision,manifest_digest)ondeleterestrict"
		uniqueKey = "unique(protocol_version,manifest_revision,manifest_digest)"
		requiredChecks = []string{
			"check(protocol_version=2)",
			"check(manifest_revision=2)",
			"check(m1_revision=1)",
			"check(octet_length(m1_manifest_digest)=32)",
			"check(octet_length(m1_source_set_digest)=32)",
			"check(octet_length(m1_privilege_set_digest)=32)",
			"check(octet_length(r2_source_set_digest)=32)",
			"check(octet_length(r2_privilege_set_digest)=32)",
			"check(octet_length(domain_digest)=32)",
			"check(octet_length(receipt_digest)=32)",
			"check(octet_length(control_acl_digest)=32)",
			"check(octet_length(manifest_digest)=32)",
			"check(r2_source_set_digest=record_platform_internal.digest(r2_source_set_body,'sha256'::text))",
			"check(r2_privilege_set_digest=record_platform_internal.digest(r2_privilege_set_body,'sha256'::text))",
			"check(domain_digest=record_platform_internal.digest(domain_body,'sha256'::text))",
			"check(control_acl_digest=record_platform_internal.digest(control_acl_body,'sha256'::text))",
			canonicalManifestDigestCheck,
		}
	}
	seenPrimary := false
	seenUnique := uniqueKey == ""
	seenForeign := false
	seenChecks := make(map[string]bool, len(requiredChecks))
	for _, constraint := range constraints {
		if !constraint.Validated {
			return fmt.Errorf("APP ACL R2 M2 relation %q has unvalidated constraint %q", name, constraint.Definition)
		}
		definition := appACLR2NormalizeCatalogDefinition(constraint.Definition)
		switch constraint.Type {
		case "p":
			if seenPrimary || definition != primaryKey || constraint.IndexOID == 0 || !constraint.IndexPrimary || !constraint.IndexUnique || !constraint.IndexValid {
				return fmt.Errorf("APP ACL R2 M2 relation %q primary key/index has catalog drift", name)
			}
			seenPrimary = true
		case "u":
			if uniqueKey == "" || seenUnique || definition != uniqueKey || constraint.IndexOID == 0 || constraint.IndexPrimary || !constraint.IndexUnique || !constraint.IndexValid {
				return fmt.Errorf("APP ACL R2 M2 relation %q unique index has catalog drift", name)
			}
			seenUnique = true
		case "f":
			// PG16 FK conindid is the referenced unique/PK supporting index OID.
			// Require a non-zero binding to a valid unique or primary index; the
			// definition already names the exact referenced columns/relation.
			if seenForeign || definition != foreignKey || constraint.IndexOID == 0 || !(constraint.IndexPrimary || constraint.IndexUnique) || !constraint.IndexValid {
				return fmt.Errorf("APP ACL R2 M2 relation %q foreign key has catalog drift", name)
			}
			seenForeign = true
		case "c":
			if constraint.IndexOID != 0 || constraint.IndexPrimary || constraint.IndexUnique || constraint.IndexValid {
				return fmt.Errorf("APP ACL R2 M2 relation %q check constraint has index metadata", name)
			}
			matched := false
			for _, required := range requiredChecks {
				if definition == required {
					if seenChecks[required] {
						return fmt.Errorf("APP ACL R2 M2 relation %q has duplicate check %q", name, required)
					}
					seenChecks[required] = true
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("APP ACL R2 M2 relation %q has unexpected check constraint %q", name, constraint.Definition)
			}
		default:
			return fmt.Errorf("APP ACL R2 M2 relation %q has unexpected constraint type %q", name, constraint.Type)
		}
	}
	if !seenPrimary || !seenUnique || !seenForeign {
		return fmt.Errorf("APP ACL R2 M2 relation %q is missing primary, unique, or foreign-key contract", name)
	}
	for _, required := range requiredChecks {
		if !seenChecks[required] {
			return fmt.Errorf("APP ACL R2 M2 relation %q is missing check constraint %q", name, required)
		}
	}
	return nil
}

func verifyAppACLR2M2RelationEffectivePrivilegesInTx(
	ctx context.Context,
	tx pgx.Tx,
	names []string,
	roles appACLR2M2RoleOIDs,
) (err error) {
	// OID 10 superuser bypass is not ACL evidence; exact direct ACL rows above reject bootstrap and PUBLIC grants.
	rows, err := tx.Query(ctx, `
		select relation.relname::text,
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'SELECT'),
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'INSERT'),
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'UPDATE'),
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'DELETE'),
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'TRUNCATE'),
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'REFERENCES'),
		       pg_catalog.has_table_privilege($1::pg_catalog.oid, relation.oid, 'TRIGGER'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'SELECT'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'INSERT'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'UPDATE'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'DELETE'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'TRUNCATE'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'REFERENCES'),
		       pg_catalog.has_table_privilege($2::pg_catalog.oid, relation.oid, 'TRIGGER'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'SELECT'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'INSERT'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'UPDATE'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'DELETE'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'TRUNCATE'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'REFERENCES'),
		       pg_catalog.has_table_privilege($3::pg_catalog.oid, relation.oid, 'TRIGGER')
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = 'public' and relation.relname = any($4::name[])
		order by relation.relname
	`, int64(roles.DirectMigrator), int64(roles.CenterRuntime), int64(roles.PlatformAdmin), names)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 effective table privileges: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 effective table privileges")
	count := 0
	for rows.Next() {
		var name string
		var directSelect, directInsert, directUpdate, directDelete, directTruncate, directReferences, directTrigger bool
		var runtimeSelect, runtimeInsert, runtimeUpdate, runtimeDelete, runtimeTruncate, runtimeReferences, runtimeTrigger bool
		var adminSelect, adminInsert, adminUpdate, adminDelete, adminTruncate, adminReferences, adminTrigger bool
		if err := rows.Scan(
			&name,
			&directSelect, &directInsert, &directUpdate, &directDelete, &directTruncate, &directReferences, &directTrigger,
			&runtimeSelect, &runtimeInsert, &runtimeUpdate, &runtimeDelete, &runtimeTruncate, &runtimeReferences, &runtimeTrigger,
			&adminSelect, &adminInsert, &adminUpdate, &adminDelete, &adminTruncate, &adminReferences, &adminTrigger,
		); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 effective table privilege: %w", err)
		}
		if !directSelect || directInsert || directUpdate || directDelete || directTruncate || directReferences || directTrigger ||
			!runtimeSelect || runtimeInsert || runtimeUpdate || runtimeDelete || runtimeTruncate || runtimeReferences || runtimeTrigger ||
			adminSelect || adminInsert || adminUpdate || adminDelete || adminTruncate || adminReferences || adminTrigger {
			return fmt.Errorf("APP ACL R2 M2 relation %q has effective privilege drift", name)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate APP ACL R2 M2 effective table privileges: %w", err)
	}
	if count != len(names) {
		return fmt.Errorf("APP ACL R2 M2 effective table privilege catalog is incomplete")
	}
	return nil
}

func verifyAppACLR2M2FunctionInTx(
	ctx context.Context,
	tx pgx.Tx,
	roles appACLR2M2RoleOIDs,
) (err error) {
	const functionName = "app_acl_r2_reject_manifest_mutation"
	var procedureOID, ownerOID int64
	var kind string
	if err := tx.QueryRow(ctx, `
		select procedure.oid::bigint, procedure.proowner::bigint, procedure.prokind::text
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where namespace.nspname = 'record_platform_internal'
		  and procedure.proname = $1
		  and pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''
	`, functionName).Scan(&procedureOID, &ownerOID, &kind); err != nil {
		return fmt.Errorf("read APP ACL R2 M2 helper function: %w", err)
	}
	procedure, err := appACLR2CatalogUint32(procedureOID, "APP ACL R2 M2 function OID")
	if err != nil {
		return err
	}
	owner, err := appACLR2CatalogUint32(ownerOID, "APP ACL R2 M2 function owner OID")
	if err != nil {
		return err
	}
	if procedure == 0 || owner != roles.DirectMigrator || kind != "f" {
		return fmt.Errorf("APP ACL R2 M2 helper function has catalog identity or owner drift")
	}
	profile, err := readAppACLR2M2FunctionProfileInTx(ctx, tx)
	if err != nil {
		return err
	}
	wantProfile := appACLR2M2FunctionProfile(roles.DirectMigrator)
	if len(profile) != 1 || !appACLR2ReceiptHelperCatalogEqual(profile[0], wantProfile) {
		return fmt.Errorf("APP ACL R2 M2 helper function has exact definition drift")
	}

	grantRows, err := tx.Query(ctx, `
		select acl_grant.grantee::bigint, acl_grant.privilege_type::text, acl_grant.is_grantable
		from pg_catalog.pg_proc procedure
		cross join lateral pg_catalog.aclexplode(procedure.proacl) acl_grant
		where procedure.oid = $1
	`, int64(procedure))
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 helper function grants: %w", err)
	}
	defer appACLR2CatalogFinishRows(grantRows, &err, "iterate APP ACL R2 M2 helper function grants")
	grantCount := 0
	for grantRows.Next() {
		var grantee int64
		var privilege string
		var grantable bool
		if err := grantRows.Scan(&grantee, &privilege, &grantable); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 helper function grant: %w", err)
		}
		granteeOID, err := appACLR2CatalogUint32(grantee, "APP ACL R2 M2 helper function grantee OID")
		if err != nil {
			return err
		}
		if granteeOID != roles.DirectMigrator || privilege != "EXECUTE" || grantable {
			return fmt.Errorf("APP ACL R2 M2 helper function has explicit EXECUTE ACL drift")
		}
		grantCount++
	}
	appACLR2CatalogFinishRows(grantRows, &err, "iterate APP ACL R2 M2 helper function grants")
	if err != nil {
		return err
	}
	if grantCount != 1 {
		return fmt.Errorf("APP ACL R2 M2 helper function has explicit EXECUTE ACL drift")
	}

	// OID 10 superuser bypass is not ACL evidence; the exact direct ACL row above rejects bootstrap and PUBLIC grants.
	var directExecute, runtimeExecute, adminExecute bool
	if err := tx.QueryRow(ctx, `
		select pg_catalog.has_function_privilege($1::pg_catalog.oid, $4::pg_catalog.oid, 'EXECUTE'),
		       pg_catalog.has_function_privilege($2::pg_catalog.oid, $4::pg_catalog.oid, 'EXECUTE'),
		       pg_catalog.has_function_privilege($3::pg_catalog.oid, $4::pg_catalog.oid, 'EXECUTE')
	`, int64(roles.DirectMigrator), int64(roles.CenterRuntime), int64(roles.PlatformAdmin), int64(procedure)).Scan(&directExecute, &runtimeExecute, &adminExecute); err != nil {
		return fmt.Errorf("read APP ACL R2 M2 helper effective privileges: %w", err)
	}
	if !directExecute || runtimeExecute || adminExecute {
		return fmt.Errorf("APP ACL R2 M2 helper function has effective EXECUTE drift")
	}
	return nil
}

func appACLR2M2FunctionProfile(ownerOID uint32) AppACLR2ReceiptHelperCatalogV1 {
	return AppACLR2ReceiptHelperCatalogV1{
		Schema:               "record_platform_internal",
		Name:                 "app_acl_r2_reject_manifest_mutation",
		IdentityArguments:    "",
		Identity:             "record_platform_internal.app_acl_r2_reject_manifest_mutation()",
		OwnerOID:             ownerOID,
		Kind:                 "f",
		Result:               "trigger",
		Language:             "plpgsql",
		Volatility:           "v",
		Parallel:             "u",
		Cost:                 100,
		InputArgumentTypes:   "",
		AllArgumentTypesNull: true,
		ArgumentModesNull:    true,
		ArgumentNamesNull:    true,
		ArgumentDefaultsNull: true,
		TransformTypesNull:   true,
		BinaryNull:           true,
		SQLBodyNull:          true,
		Config:               []string{"search_path=pg_catalog"},
		Definition: `CREATE OR REPLACE FUNCTION record_platform_internal.app_acl_r2_reject_manifest_mutation()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'pg_catalog'
AS $function$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_manifest is immutable';
end;
$function$
`,
		Source: `
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_manifest is immutable';
end;
`,
	}
}

func readAppACLR2M2FunctionProfileInTx(ctx context.Context, tx pgx.Tx) (profiles []AppACLR2ReceiptHelperCatalogV1, err error) {
	wantProfile := appACLR2M2FunctionProfile(0)
	definitionMaximum := len(wantProfile.Definition)
	sourceMaximum := len(wantProfile.Source)
	rows, err := tx.Query(ctx, `
		select namespace.nspname::text,
		       procedure.proname::text,
		       pg_catalog.pg_get_function_identity_arguments(procedure.oid),
		       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
		       procedure.proowner::bigint,
		       procedure.prokind::text,
		       pg_catalog.pg_get_function_result(procedure.oid),
		       language.lanname::text,
		       procedure.provolatile::text,
		       procedure.proparallel::text,
		       procedure.prosecdef,
		       procedure.proisstrict,
		       procedure.proleakproof,
		       procedure.proretset,
		       procedure.procost::double precision,
		       procedure.prorows::double precision,
		       procedure.prosupport::pg_catalog.oid::bigint,
		       procedure.provariadic::bigint,
		       procedure.pronargs::bigint,
		       procedure.pronargdefaults::bigint,
		       procedure.proargtypes::text,
		       procedure.proallargtypes is null,
		       procedure.proargmodes is null,
		       procedure.proargnames is null,
		       procedure.proargdefaults is null,
		       procedure.protrftypes is null,
		       procedure.probin is null,
		       procedure.prosqlbody is null,
		       coalesce(procedure.proconfig, '{}'::text[]),
		       coalesce(pg_catalog.octet_length(pg_catalog.pg_get_functiondef(procedure.oid)), -1)::bigint,
		       case when pg_catalog.octet_length(pg_catalog.pg_get_functiondef(procedure.oid)) between 1 and $1::integer
		            then pg_catalog.convert_to(pg_catalog.pg_get_functiondef(procedure.oid), 'UTF8'::name) else null::bytea end,
		       coalesce(pg_catalog.octet_length(procedure.prosrc::text), -1)::bigint,
		       case when pg_catalog.octet_length(procedure.prosrc::text) between 1 and $2::integer
		            then pg_catalog.convert_to(procedure.prosrc::text, 'UTF8'::name) else null::bytea end
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		join pg_catalog.pg_language language on language.oid = procedure.prolang
		where namespace.nspname = 'record_platform_internal'
		  and procedure.proname = 'app_acl_r2_reject_manifest_mutation'
		limit 2
	`, definitionMaximum, sourceMaximum)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 M2 helper function profile: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 helper function profile")
	profiles = make([]AppACLR2ReceiptHelperCatalogV1, 0, 1)
	for len(profiles) < 2 && rows.Next() {
		var profile AppACLR2ReceiptHelperCatalogV1
		var ownerOID, supportFunctionOID, variadicTypeOID, argumentCount, argumentDefaultCount int64
		var definitionSize, sourceSize int64
		var definition, source []byte
		if err := rows.Scan(
			&profile.Schema,
			&profile.Name,
			&profile.IdentityArguments,
			&profile.Identity,
			&ownerOID,
			&profile.Kind,
			&profile.Result,
			&profile.Language,
			&profile.Volatility,
			&profile.Parallel,
			&profile.SecurityDefiner,
			&profile.Strict,
			&profile.Leakproof,
			&profile.ReturnsSet,
			&profile.Cost,
			&profile.Rows,
			&supportFunctionOID,
			&variadicTypeOID,
			&argumentCount,
			&argumentDefaultCount,
			&profile.InputArgumentTypes,
			&profile.AllArgumentTypesNull,
			&profile.ArgumentModesNull,
			&profile.ArgumentNamesNull,
			&profile.ArgumentDefaultsNull,
			&profile.TransformTypesNull,
			&profile.BinaryNull,
			&profile.SQLBodyNull,
			&profile.Config,
			&definitionSize,
			&definition,
			&sourceSize,
			&source,
		); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 M2 helper function profile: %w", err)
		}
		var parseErr error
		if profile.OwnerOID, parseErr = appACLR2CatalogUint32(ownerOID, "M2 helper owner OID"); parseErr != nil {
			return nil, parseErr
		}
		if profile.SupportFunctionOID, parseErr = appACLR2CatalogOptionalOID(supportFunctionOID, "M2 helper support function OID"); parseErr != nil {
			return nil, parseErr
		}
		if profile.VariadicTypeOID, parseErr = appACLR2CatalogOptionalOID(variadicTypeOID, "M2 helper variadic type OID"); parseErr != nil {
			return nil, parseErr
		}
		if argumentCount < 0 || argumentCount > int64(^uint16(0)) || argumentDefaultCount < 0 || argumentDefaultCount > int64(^uint16(0)) {
			return nil, fmt.Errorf("APP ACL R2 M2 helper function argument count is outside uint16 bounds")
		}
		profile.ArgumentCount = uint16(argumentCount)
		profile.ArgumentDefaultCount = uint16(argumentDefaultCount)
		if profile.Definition, parseErr = appACLR2CatalogTextFromObservedBytes("APP ACL R2 M2 helper function definition", definitionSize, definition, definitionMaximum); parseErr != nil {
			return nil, parseErr
		}
		if profile.Source, parseErr = appACLR2CatalogTextFromObservedBytes("APP ACL R2 M2 helper function source", sourceSize, source, sourceMaximum); parseErr != nil {
			return nil, parseErr
		}
		profiles = append(profiles, profile)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 M2 helper function profile: %w", err)
	}
	return profiles, nil
}

const (
	appACLR2M2HeadForeignKeyDefinitionPG16           = "FOREIGN KEY (protocol_version, manifest_revision, manifest_digest) REFERENCES app_acl_r2_manifest_revisions(protocol_version, manifest_revision, manifest_digest) ON DELETE RESTRICT"
	appACLR2M2RevisionsForeignKeyDefinitionPG16      = "FOREIGN KEY (m1_revision, m1_manifest_digest) REFERENCES app_acl_manifest_revisions(manifest_revision, manifest_digest) ON DELETE RESTRICT"
	appACLR2M2ManifestDigestConstraintDefinitionPG16 = "CHECK (manifest_digest = record_platform_internal.digest((((((((((((((((((((((((((convert_to('HOUFENG-APP-ACL-MANIFEST-R2-V1'::text, 'UTF8'::name) || int2send(1::smallint)) || int2send(protocol_version)) || int8send(manifest_revision)) || int8send(m1_revision)) || m1_manifest_digest) || m1_source_set_digest) || m1_privilege_set_digest) || int2send(octet_length(m1_migrator_catalog_role)::smallint)) || convert_to(m1_migrator_catalog_role, 'UTF8'::name)) || int2send(octet_length(direct_migrator_name)::smallint)) || convert_to(direct_migrator_name, 'UTF8'::name)) || int4send(direct_migrator_oid::integer)) || int4send(octet_length(r2_source_set_body))) || r2_source_set_body) || r2_source_set_digest) || int4send(octet_length(r2_privilege_set_body))) || r2_privilege_set_body) || r2_privilege_set_digest) || int4send(octet_length(domain_body))) || domain_body) || domain_digest) || receipt_digest) || int4send(octet_length(control_acl_body))) || control_acl_body) || control_acl_digest) || int8send((EXTRACT(epoch FROM recorded_at) * 1000000::numeric)::bigint), 'sha256'::text))"
)

type appACLR2M2InternalRITriggerExpectation struct {
	TableName            string
	TriggerNamePrefix    string
	FunctionIdentity     string
	TriggerType          int64
	ConstraintDefinition string
	ConstraintRelation   string
	ReferencedRelation   string
	BoundRelation        string
}

func appACLR2M2InternalRITriggerExpectations() []appACLR2M2InternalRITriggerExpectation {
	return []appACLR2M2InternalRITriggerExpectation{
		{
			TableName: "app_acl_manifest_revisions", TriggerNamePrefix: "RI_ConstraintTrigger_a_",
			FunctionIdentity: "pg_catalog.RI_FKey_restrict_del()", TriggerType: 9,
			ConstraintDefinition: appACLR2M2RevisionsForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_revisions",
			ReferencedRelation:   "public.app_acl_manifest_revisions",
			BoundRelation:        "public.app_acl_r2_manifest_revisions",
		},
		{
			TableName: "app_acl_manifest_revisions", TriggerNamePrefix: "RI_ConstraintTrigger_a_",
			FunctionIdentity: "pg_catalog.RI_FKey_noaction_upd()", TriggerType: 17,
			ConstraintDefinition: appACLR2M2RevisionsForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_revisions",
			ReferencedRelation:   "public.app_acl_manifest_revisions",
			BoundRelation:        "public.app_acl_r2_manifest_revisions",
		},
		{
			TableName: "app_acl_r2_manifest_head", TriggerNamePrefix: "RI_ConstraintTrigger_c_",
			FunctionIdentity: "pg_catalog.RI_FKey_check_ins()", TriggerType: 5,
			ConstraintDefinition: appACLR2M2HeadForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_head",
			ReferencedRelation:   "public.app_acl_r2_manifest_revisions",
			BoundRelation:        "public.app_acl_r2_manifest_revisions",
		},
		{
			TableName: "app_acl_r2_manifest_head", TriggerNamePrefix: "RI_ConstraintTrigger_c_",
			FunctionIdentity: "pg_catalog.RI_FKey_check_upd()", TriggerType: 17,
			ConstraintDefinition: appACLR2M2HeadForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_head",
			ReferencedRelation:   "public.app_acl_r2_manifest_revisions",
			BoundRelation:        "public.app_acl_r2_manifest_revisions",
		},
		{
			TableName: "app_acl_r2_manifest_revisions", TriggerNamePrefix: "RI_ConstraintTrigger_a_",
			FunctionIdentity: "pg_catalog.RI_FKey_restrict_del()", TriggerType: 9,
			ConstraintDefinition: appACLR2M2HeadForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_head",
			ReferencedRelation:   "public.app_acl_r2_manifest_revisions",
			BoundRelation:        "public.app_acl_r2_manifest_head",
		},
		{
			TableName: "app_acl_r2_manifest_revisions", TriggerNamePrefix: "RI_ConstraintTrigger_a_",
			FunctionIdentity: "pg_catalog.RI_FKey_noaction_upd()", TriggerType: 17,
			ConstraintDefinition: appACLR2M2HeadForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_head",
			ReferencedRelation:   "public.app_acl_r2_manifest_revisions",
			BoundRelation:        "public.app_acl_r2_manifest_head",
		},
		{
			TableName: "app_acl_r2_manifest_revisions", TriggerNamePrefix: "RI_ConstraintTrigger_c_",
			FunctionIdentity: "pg_catalog.RI_FKey_check_ins()", TriggerType: 5,
			ConstraintDefinition: appACLR2M2RevisionsForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_revisions",
			ReferencedRelation:   "public.app_acl_manifest_revisions",
			BoundRelation:        "public.app_acl_manifest_revisions",
		},
		{
			TableName: "app_acl_r2_manifest_revisions", TriggerNamePrefix: "RI_ConstraintTrigger_c_",
			FunctionIdentity: "pg_catalog.RI_FKey_check_upd()", TriggerType: 17,
			ConstraintDefinition: appACLR2M2RevisionsForeignKeyDefinitionPG16,
			ConstraintRelation:   "public.app_acl_r2_manifest_revisions",
			ReferencedRelation:   "public.app_acl_manifest_revisions",
			BoundRelation:        "public.app_acl_manifest_revisions",
		},
	}
}

func verifyAppACLR2M2TriggersInTx(ctx context.Context, tx pgx.Tx, directMigratorOID uint32) (err error) {
	wantUser := map[string]bool{
		"app_acl_r2_manifest_head.app_acl_r2_manifest_head_immutable":           false,
		"app_acl_r2_manifest_revisions.app_acl_r2_manifest_revisions_immutable": false,
	}
	wantInternal := appACLR2M2InternalRITriggerExpectations()
	userDefinitionMaximum := max(
		len(appACLR2M2TriggerDefinitionPG16("app_acl_r2_manifest_head")),
		len(appACLR2M2TriggerDefinitionPG16("app_acl_r2_manifest_revisions")),
	)
	constraintDefinitionMaximum := max(
		len(appACLR2M2HeadForeignKeyDefinitionPG16),
		len(appACLR2M2RevisionsForeignKeyDefinitionPG16),
	)
	witnessLimit := len(wantUser) + len(wantInternal) + 1
	rows, err := tx.Query(ctx, `
		select table_relation.relname::text,
		       trigger.tgname::text,
		       function_namespace.nspname::text || '.' || function.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(function.oid) || ')',
		       table_relation.relowner::bigint,
		       function.proowner::bigint,
		       trigger.tgenabled = 'O',
		       trigger.tgisinternal,
		       trigger.tgtype::integer,
		       trigger.tgattr::text,
		       trigger.tgqual is not null,
		       trigger.tgnargs::integer,
		       pg_catalog.encode(trigger.tgargs, 'hex'),
		       case when not trigger.tgisinternal
		            then coalesce(pg_catalog.octet_length(pg_catalog.pg_get_triggerdef(trigger.oid, false)), -1)::bigint
		            else 0::bigint end,
		       case when not trigger.tgisinternal
		                  and pg_catalog.octet_length(pg_catalog.pg_get_triggerdef(trigger.oid, false)) between 1 and $1::integer
		            then pg_catalog.convert_to(pg_catalog.pg_get_triggerdef(trigger.oid, false), 'UTF8'::name)
		            else null::bytea end,
		       coalesce(constraint_catalog.contype::text, ''),
		       case when trigger.tgisinternal
		            then coalesce(pg_catalog.octet_length(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true)), -1)::bigint
		            else 0::bigint end,
		       case when trigger.tgisinternal
		                  and pg_catalog.octet_length(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true)) between 1 and $2::integer
		            then pg_catalog.convert_to(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true), 'UTF8'::name)
		            else null::bytea end,
		       coalesce(constraint_catalog.convalidated, false),
		       coalesce(constraint_namespace.nspname::text || '.' || constraint_relation.relname::text, ''),
		       coalesce(referenced_namespace.nspname::text || '.' || referenced_relation.relname::text, ''),
		       coalesce(bound_namespace.nspname::text || '.' || bound_relation.relname::text, '')
		from pg_catalog.pg_trigger trigger
		join pg_catalog.pg_class table_relation on table_relation.oid = trigger.tgrelid
		join pg_catalog.pg_namespace table_namespace on table_namespace.oid = table_relation.relnamespace
		join pg_catalog.pg_proc function on function.oid = trigger.tgfoid
		join pg_catalog.pg_namespace function_namespace on function_namespace.oid = function.pronamespace
		left join pg_catalog.pg_constraint constraint_catalog on constraint_catalog.oid = trigger.tgconstraint
		left join pg_catalog.pg_class constraint_relation on constraint_relation.oid = constraint_catalog.conrelid
		left join pg_catalog.pg_namespace constraint_namespace on constraint_namespace.oid = constraint_relation.relnamespace
		left join pg_catalog.pg_class referenced_relation on referenced_relation.oid = constraint_catalog.confrelid
		left join pg_catalog.pg_namespace referenced_namespace on referenced_namespace.oid = referenced_relation.relnamespace
		left join pg_catalog.pg_class bound_relation on bound_relation.oid = trigger.tgconstrrelid
		left join pg_catalog.pg_namespace bound_namespace on bound_namespace.oid = bound_relation.relnamespace
		where table_namespace.nspname = 'public'
		  and (
		       table_relation.relname in ('app_acl_r2_manifest_head', 'app_acl_r2_manifest_revisions')
		    or (constraint_namespace.nspname = 'public'
		        and constraint_relation.relname in ('app_acl_r2_manifest_head', 'app_acl_r2_manifest_revisions'))
		  )
		limit $3::integer
	`, userDefinitionMaximum, constraintDefinitionMaximum, witnessLimit)
	if err != nil {
		return fmt.Errorf("read APP ACL R2 M2 triggers: %w", err)
	}
	defer appACLR2CatalogFinishRows(rows, &err, "iterate APP ACL R2 M2 triggers")
	seenInternal := make([]bool, len(wantInternal))
	const functionIdentity = "record_platform_internal.app_acl_r2_reject_manifest_mutation()"
	for observed := 0; observed < witnessLimit && rows.Next(); observed++ {
		var tableName, triggerName, identity, attributes, definition string
		var constraintType, constraintDefinition, constraintRelation, referencedRelation, boundRelation string
		var tableOwner, functionOwner, triggerType, definitionSize, constraintDefinitionSize int64
		var enabled, internal, hasQualification, constraintValidated bool
		var argumentCount int64
		var arguments string
		var definitionBytes, constraintDefinitionBytes []byte
		if err := rows.Scan(
			&tableName,
			&triggerName,
			&identity,
			&tableOwner,
			&functionOwner,
			&enabled,
			&internal,
			&triggerType,
			&attributes,
			&hasQualification,
			&argumentCount,
			&arguments,
			&definitionSize,
			&definitionBytes,
			&constraintType,
			&constraintDefinitionSize,
			&constraintDefinitionBytes,
			&constraintValidated,
			&constraintRelation,
			&referencedRelation,
			&boundRelation,
		); err != nil {
			return fmt.Errorf("scan APP ACL R2 M2 trigger: %w", err)
		}
		tableOwnerOID, err := appACLR2CatalogUint32(tableOwner, "APP ACL R2 M2 trigger table owner OID")
		if err != nil {
			return err
		}
		functionOwnerOID, err := appACLR2CatalogUint32(functionOwner, "APP ACL R2 M2 trigger function owner OID")
		if err != nil {
			return err
		}
		if internal {
			if definitionSize != 0 || len(definitionBytes) != 0 {
				return fmt.Errorf("APP ACL R2 M2 internal trigger %q transferred a user trigger definition", triggerName)
			}
			constraintDefinition, err = appACLR2CatalogTextFromObservedBytes(
				"APP ACL R2 M2 internal trigger constraint definition",
				constraintDefinitionSize,
				constraintDefinitionBytes,
				constraintDefinitionMaximum,
			)
			if err != nil {
				return err
			}
			matched := -1
			for index, expected := range wantInternal {
				if tableName == expected.TableName && identity == expected.FunctionIdentity && triggerType == expected.TriggerType &&
					constraintDefinition == expected.ConstraintDefinition && constraintRelation == expected.ConstraintRelation &&
					referencedRelation == expected.ReferencedRelation && boundRelation == expected.BoundRelation {
					matched = index
					break
				}
			}
			if matched < 0 || seenInternal[matched] || tableOwnerOID != directMigratorOID || functionOwnerOID != 10 || !enabled ||
				constraintType != "f" || !constraintValidated || attributes != "" || hasQualification || argumentCount != 0 || arguments != "" ||
				!appACLR2M2InternalRITriggerNameMatches(triggerName, wantInternal[matched].TriggerNamePrefix) {
				return fmt.Errorf("APP ACL R2 M2 internal foreign-key trigger %q on %q has catalog or binding drift", triggerName, tableName)
			}
			seenInternal[matched] = true
			continue
		}
		if constraintDefinitionSize != 0 || len(constraintDefinitionBytes) != 0 {
			return fmt.Errorf("APP ACL R2 M2 user trigger %q transferred an internal constraint definition", triggerName)
		}
		definition, err = appACLR2CatalogTextFromObservedBytes(
			"APP ACL R2 M2 user trigger definition",
			definitionSize,
			definitionBytes,
			userDefinitionMaximum,
		)
		if err != nil {
			return err
		}
		if constraintType != "" || constraintDefinition != "" || constraintValidated || constraintRelation != "" || referencedRelation != "" || boundRelation != "" {
			return fmt.Errorf("APP ACL R2 M2 user trigger %q has constraint binding drift", triggerName)
		}
		key := tableName + "." + triggerName
		if _, exists := wantUser[key]; !exists || wantUser[key] || identity != functionIdentity || tableOwnerOID != directMigratorOID || functionOwnerOID != directMigratorOID ||
			!enabled || triggerType != appACLR2ReceiptTriggerTypeBeforeUpdateDeleteTruncateStatement || attributes != "" || hasQualification {
			return fmt.Errorf("APP ACL R2 M2 trigger %q has catalog drift", key)
		}
		if argumentCount != 0 || arguments != "" || definition != appACLR2M2TriggerDefinitionPG16(tableName) {
			return fmt.Errorf("APP ACL R2 M2 trigger %q has definition or argument drift", key)
		}
		wantUser[key] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate APP ACL R2 M2 triggers: %w", err)
	}
	for key, seen := range wantUser {
		if !seen {
			return fmt.Errorf("APP ACL R2 M2 trigger %q is missing", key)
		}
	}
	for index, seen := range seenInternal {
		if !seen {
			expected := wantInternal[index]
			return fmt.Errorf("APP ACL R2 M2 internal foreign-key trigger for %q on %q is missing", expected.FunctionIdentity, expected.TableName)
		}
	}
	return nil
}

func appACLR2M2InternalRITriggerNameMatches(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	for _, character := range name[len(prefix):] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func appACLR2M2TriggerDefinitionPG16(tableName string) string {
	return "CREATE TRIGGER app_acl_r2_" + map[string]string{
		"app_acl_r2_manifest_head":      "manifest_head_immutable",
		"app_acl_r2_manifest_revisions": "manifest_revisions_immutable",
	}[tableName] + " BEFORE DELETE OR UPDATE OR TRUNCATE ON public." + tableName + " FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_manifest_mutation()"
}

func verifyAppACLR2M2DefaultACLAbsenceInTx(ctx context.Context, tx pgx.Tx, directMigratorOID uint32) error {
	var count int64
	if err := tx.QueryRow(ctx, `
		select pg_catalog.count(*)::bigint
		from pg_catalog.pg_default_acl default_acl
		left join pg_catalog.pg_namespace namespace on namespace.oid = default_acl.defaclnamespace
		where default_acl.defaclrole = $1
		  and ((default_acl.defaclobjtype = 'r'
		        and (default_acl.defaclnamespace = 0 or namespace.nspname = 'public'))
		    or (default_acl.defaclobjtype = 'f'
		        and (default_acl.defaclnamespace = 0 or namespace.nspname = 'record_platform_internal')))
	`, int64(directMigratorOID)).Scan(&count); err != nil {
		return fmt.Errorf("read APP ACL R2 M2 default ACL catalog: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("APP ACL R2 M2 direct migrator has default ACL drift")
	}
	return nil
}

func evaluateAppACLR2CatalogShape(shape appACLR2CatalogShape) AppACLR2CatalogPredicates {
	knownObjects := appACLR2KnownReservedObjects()
	noUnknownObjects := !shape.CatalogStructuralDrift && appACLR2ReservedObjectsContainOnlyKnown(shape.ReservedObjects, knownObjects)
	l2ObjectsPresent := appACLR2ReservedObjectsContain(shape.ReservedObjects, appACLR2L2ReservedObjects())
	m2ObjectsPresent := appACLR2ReservedObjectsContain(shape.ReservedObjects, appACLR2M2ReservedObjects())

	predicates := AppACLR2CatalogPredicates{
		FrozenState:               shape.FrozenState,
		ExactL1M1:                 shape.FrozenExact,
		L2Absent:                  appACLR2ReservedObjectsAbsent(shape.ReservedObjects, appACLR2L2ReservedObjects()) && len(shape.L2Rows) == 0,
		M2Absent:                  appACLR2ReservedObjectsAbsent(shape.ReservedObjects, appACLR2M2ReservedObjects()) && len(shape.M2Revisions) == 0 && len(shape.M2Heads) == 0,
		HasUnknownReservedObjects: !noUnknownObjects,
	}
	predicates.ExactL2 = predicates.ExactL1M1 && noUnknownObjects && l2ObjectsPresent && appACLR2ExactL2Rows(shape.L2Rows) && shape.L2EvidenceExact
	predicates.ExactM2 = predicates.ExactL2 && noUnknownObjects && m2ObjectsPresent && appACLR2ExactM2Shape(shape)
	return predicates
}

func appACLR2ExactL2Rows(rows []appACLR2ReceiptRowV1) bool {
	if len(rows) != 1 || !rows[0].Singleton || len(rows[0].Body) == 0 || sha256.Sum256(rows[0].Body) != rows[0].Digest {
		return false
	}
	_, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(rows[0].Body)
	return err == nil
}

func appACLR2ExactM2Shape(shape appACLR2CatalogShape) bool {
	if len(shape.M2Revisions) != 1 || len(shape.M2Heads) != 1 || len(shape.L2Rows) != 1 {
		return false
	}
	revision := shape.M2Revisions[0]
	receipt, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(shape.L2Rows[0].Body)
	if err != nil {
		return false
	}
	var directMigrator AppACLR2ReceiptRoleV1
	for _, role := range receipt.Roles {
		if role.ControlRole == AppACLControlRoleDirectMigratorR2 {
			directMigrator = role
			break
		}
	}
	if directMigrator.Name == "" ||
		revision.Manifest.DirectMigratorName != directMigrator.Name ||
		revision.Manifest.DirectMigratorOID != directMigrator.OID ||
		!bytes.Equal(revision.Manifest.R2SourceSetBody, receipt.R2SourceBody) ||
		revision.Manifest.R2SourceSetDigest != receipt.R2SourceDigest ||
		!bytes.Equal(revision.Manifest.R2PrivilegeSetBody, receipt.R2PrivilegeBody) ||
		revision.Manifest.R2PrivilegeSetDigest != receipt.R2PrivilegeDigest ||
		!bytes.Equal(revision.Manifest.DomainBody, receipt.DomainBody) ||
		revision.Manifest.DomainDigest != receipt.DomainDigest {
		return false
	}
	if revision.Manifest.M1Revision != shape.FrozenState.ManifestRevision ||
		revision.Manifest.M1ManifestDigest != shape.FrozenState.ManifestDigest ||
		revision.Manifest.M1SourceSetDigest != shape.FrozenState.SourceSetDigest ||
		revision.Manifest.M1PrivilegeSetDigest != shape.FrozenState.PrivilegeSetDigest ||
		revision.Manifest.M1MigratorCatalogRole != shape.FrozenState.DirectMigratorRole ||
		revision.Manifest.ReceiptDigest != shape.L2Rows[0].Digest {
		return false
	}
	manifestBody, err := CanonicalAppACLManifestR2BodyV1(revision.Manifest)
	if err != nil || sha256.Sum256(manifestBody) != revision.ManifestDigest {
		return false
	}
	controlBody, err := CanonicalAppACLControlACLBodyR2V1(shape.M2ControlACL)
	if err != nil || !bytes.Equal(controlBody, revision.Manifest.ControlACLBody) {
		return false
	}
	head := shape.M2Heads[0]
	return head.Singleton && head.ProtocolVersion == revision.Manifest.ProtocolVersion && head.ManifestRevision == revision.Manifest.ManifestRevision && head.ManifestDigest == revision.ManifestDigest
}

func appACLR2L2ReservedObjects() []AppACLR2ReservedCatalogObjectV1 {
	return []AppACLR2ReservedCatalogObjectV1{
		{Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", Detail: "f"},
		{Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", Detail: "f"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt_pkey", Detail: "i"},
		{Kind: "trigger", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt.app_acl_r2_bootstrap_receipt_immutable", Detail: "user"},
	}
}

func appACLR2L2ReceiptRelation() AppACLR2ReservedCatalogObjectV1 {
	return AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"}
}

func appACLR2M2ReservedObjects() []AppACLR2ReservedCatalogObjectV1 {
	return []AppACLR2ReservedCatalogObjectV1{
		{Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_manifest_mutation()", Detail: "f"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "r"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head_pkey", Detail: "i"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions_pkey", Detail: "i"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key", Detail: "i"},
		{Kind: "trigger", Schema: "public", Identity: "app_acl_r2_manifest_head.app_acl_r2_manifest_head_immutable", Detail: "user"},
		{Kind: "trigger", Schema: "public", Identity: "app_acl_r2_manifest_revisions.app_acl_r2_manifest_revisions_immutable", Detail: "user"},
	}
}

func appACLR2M2RevisionsRelation() AppACLR2ReservedCatalogObjectV1 {
	return AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r"}
}

func appACLR2M2HeadRelation() AppACLR2ReservedCatalogObjectV1 {
	return AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "r"}
}

func appACLR2KnownReservedObjects() []AppACLR2ReservedCatalogObjectV1 {
	objects := append([]AppACLR2ReservedCatalogObjectV1(nil), appACLR2L2ReservedObjects()...)
	return append(objects, appACLR2M2ReservedObjects()...)
}

func appACLR2ReservedObjectsContainOnlyKnown(got, want []AppACLR2ReservedCatalogObjectV1) bool {
	if len(got) > len(want) {
		return false
	}
	wantKeys := make(map[string]struct{}, len(want))
	for _, object := range want {
		wantKeys[appACLR2ReservedObjectKey(object)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(got))
	for _, object := range got {
		key := appACLR2ReservedObjectKey(object)
		if object.OID == 0 {
			return false
		}
		if _, exists := wantKeys[key]; !exists {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func appACLR2ReservedObjectExists(got []AppACLR2ReservedCatalogObjectV1, want AppACLR2ReservedCatalogObjectV1) bool {
	for _, object := range got {
		// A reserved evidence relation with the expected name must be read even
		// when its relkind/detail is itself structural drift.
		if object.Kind == "relation" && object.Schema == want.Schema && object.Identity == want.Identity {
			return true
		}
	}
	return false
}

func appACLR2ReservedObjectsContain(got, want []AppACLR2ReservedCatalogObjectV1) bool {
	observed := make(map[string]int, len(got))
	for _, object := range got {
		if object.OID == 0 {
			return false
		}
		observed[appACLR2ReservedObjectKey(object)]++
	}
	for _, object := range want {
		if observed[appACLR2ReservedObjectKey(object)] != 1 {
			return false
		}
	}
	return true
}

func appACLR2ReservedObjectsAbsent(got, want []AppACLR2ReservedCatalogObjectV1) bool {
	wantKeys := make(map[string]struct{}, len(want))
	for _, object := range want {
		wantKeys[appACLR2ReservedObjectKey(object)] = struct{}{}
	}
	for _, object := range got {
		if _, exists := wantKeys[appACLR2ReservedObjectKey(object)]; exists {
			return false
		}
	}
	return true
}

func appACLR2ReservedObjectKey(object AppACLR2ReservedCatalogObjectV1) string {
	return object.Kind + "|" + object.Schema + "|" + object.Identity + "|" + object.Detail
}

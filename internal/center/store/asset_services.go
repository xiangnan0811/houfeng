package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/createidempotency"
	"houfeng/internal/center/ids"
)

var _ assetservices.Repository = (*PostgresAssetServiceRepository)(nil)

type assetServiceDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type assetServiceQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresAssetServiceRepository struct {
	db      assetServiceDB
	beginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func NewPostgresAssetServiceRepository(db *pgxpool.Pool) *PostgresAssetServiceRepository {
	return &PostgresAssetServiceRepository{db: db, beginTx: db.BeginTx}
}

const assetServiceCreateOperation = "asset-service.create"

const assetServiceSelectColumns = `
	service_id,
	vps_id,
	target_id,
	name,
	service_type,
	status,
	url,
	port,
	labels,
	note,
	created_at,
	updated_at`

const assetServiceQualifiedSelectColumns = `
	asset_services.service_id,
	asset_services.vps_id,
	asset_services.target_id,
	asset_services.name,
	asset_services.service_type,
	asset_services.status,
	asset_services.url,
	asset_services.port,
	asset_services.labels,
	asset_services.note,
	asset_services.created_at,
	asset_services.updated_at`

type assetServiceScanner interface {
	Scan(dest ...any) error
}

func scanAssetService(row assetServiceScanner) (assetservices.Record, error) {
	var record assetservices.Record
	if err := row.Scan(
		&record.ServiceID,
		&record.VPSID,
		&record.TargetID,
		&record.Name,
		&record.ServiceType,
		&record.Status,
		&record.URL,
		&record.Port,
		&record.Labels,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return assetservices.Record{}, err
	}
	return record, nil
}

func (r *PostgresAssetServiceRepository) ListAssetServices(ctx context.Context, filters assetservices.ListFilters) ([]assetservices.Record, error) {
	return r.listAssetServices(ctx, filters, true)
}

func (r *PostgresAssetServiceRepository) listAssetServices(ctx context.Context, filters assetservices.ListFilters, currentAssetScope bool) ([]assetservices.Record, error) {
	filters = assetservices.NormalizeListFilters(filters)
	if err := assetservices.ValidateListFilters(filters); err != nil {
		return nil, err
	}

	args := []any{}
	conditions := []string{}
	if filters.VPSID != "" {
		args = append(args, filters.VPSID)
		conditions = append(conditions, fmt.Sprintf("asset_services.vps_id = $%d", len(args)))
	}
	if filters.TargetID != "" {
		args = append(args, filters.TargetID)
		conditions = append(conditions, fmt.Sprintf("asset_services.target_id = $%d", len(args)))
	}
	if filters.ServiceType != "" {
		args = append(args, string(filters.ServiceType))
		conditions = append(conditions, fmt.Sprintf("asset_services.service_type = $%d", len(args)))
	}
	if filters.Status != "" {
		args = append(args, string(filters.Status))
		conditions = append(conditions, fmt.Sprintf("asset_services.status = $%d", len(args)))
	}
	if currentAssetScope {
		conditions = append(conditions, "v.lifecycle_status not in ('cancelled', 'archived')")
	}

	selectColumns := assetServiceSelectColumns
	if currentAssetScope {
		selectColumns = assetServiceQualifiedSelectColumns
	}
	query := `
		select ` + selectColumns + `
		from asset_services`
	if currentAssetScope {
		query += `
		join vps_assets v on v.vps_id = asset_services.vps_id`
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by lower(asset_services.name), asset_services.service_id"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query asset services: %w", err)
	}
	defer rows.Close()

	records := make([]assetservices.Record, 0)
	for rows.Next() {
		record, err := scanAssetService(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset service: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset services: %w", err)
	}
	return records, nil
}

func (r *PostgresAssetServiceRepository) ListAssetServicesForVPS(ctx context.Context, vpsID string) ([]assetservices.Record, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", assetservices.ErrInvalidServiceInput)
	}
	exists, err := r.vpsAssetExists(ctx, vpsID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, assetservices.ErrServiceOwnerNotFound
	}
	return r.listAssetServices(ctx, assetservices.ListFilters{VPSID: vpsID}, false)
}

func (r *PostgresAssetServiceRepository) CreateAssetService(ctx context.Context, input assetservices.CreateInput) (assetservices.Record, error) {
	input = assetservices.NormalizeCreateInput(input)
	if err := assetservices.ValidateCreateInput(input); err != nil {
		return assetservices.Record{}, err
	}
	return insertAssetService(ctx, r.db, input)
}

func (r *PostgresAssetServiceRepository) CreateAssetServiceIdempotent(
	ctx context.Context,
	input assetservices.CreateInput,
	idempotencyKey string,
) (assetservices.Record, bool, error) {
	input = assetservices.NormalizeCreateInput(input)
	if err := assetservices.ValidateCreateInput(input); err != nil {
		return assetservices.Record{}, false, err
	}
	key, err := createidempotency.NormalizeKey(idempotencyKey)
	if err != nil {
		return assetservices.Record{}, false, err
	}
	digest, err := assetServiceCreateDigest(input)
	if err != nil {
		return assetservices.Record{}, false, err
	}
	if r.beginTx == nil {
		return assetservices.Record{}, false, errors.New("asset service repository cannot create idempotently without transaction support")
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return assetservices.Record{}, false, fmt.Errorf("begin asset service create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := createidempotency.NamespacedLockKey(assetServiceCreateOperation, key)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1)::bigint)`, lockKey); err != nil {
		return assetservices.Record{}, false, fmt.Errorf("lock asset service create receipt: %w", err)
	}

	var storedDigest string
	var serviceID string
	err = tx.QueryRow(ctx, `
		select request_digest, service_id
		from asset_service_create_idempotency
		where idempotency_key = $1`, key).Scan(&storedDigest, &serviceID)
	if err == nil {
		if storedDigest != digest {
			return assetservices.Record{}, false, createidempotency.ErrIdempotencyKeyReused
		}
		record, err := scanAssetService(tx.QueryRow(ctx, `
			select `+assetServiceSelectColumns+`
			from asset_services
			where service_id = $1
			  and vps_id = $2`, serviceID, input.VPSID))
		if err != nil {
			return assetservices.Record{}, false, fmt.Errorf("load replayed asset service: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return assetservices.Record{}, false, fmt.Errorf("commit asset service create replay: %w", err)
		}
		return record, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return assetservices.Record{}, false, fmt.Errorf("lookup asset service create receipt: %w", err)
	}

	record, err := insertAssetService(ctx, tx, input)
	if err != nil {
		return assetservices.Record{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		insert into asset_service_create_idempotency (
			idempotency_key,
			request_digest,
			service_id
		) values ($1, $2, $3)`, key, digest, record.ServiceID); err != nil {
		return assetservices.Record{}, false, fmt.Errorf("record asset service create receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetservices.Record{}, false, fmt.Errorf("commit asset service create: %w", err)
	}
	return record, false, nil
}

func assetServiceCreateDigest(input assetservices.CreateInput) (string, error) {
	return createidempotency.DigestNormalizedRequest(input)
}

func insertAssetService(ctx context.Context, db assetServiceQueryer, input assetservices.CreateInput) (assetservices.Record, error) {

	serviceID, err := ids.New("svc")
	if err != nil {
		return assetservices.Record{}, fmt.Errorf("generate asset service id: %w", err)
	}

	record, err := scanAssetService(db.QueryRow(ctx, `
		insert into asset_services (
			service_id,
			vps_id,
			target_id,
			name,
			service_type,
			status,
			url,
			port,
			labels,
			note
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10
		)
		returning `+assetServiceSelectColumns,
		serviceID,
		input.VPSID,
		nullableStringArg(input.TargetID),
		input.Name,
		string(input.ServiceType),
		string(input.Status),
		input.URL,
		assetServicePortArg(input.Port),
		input.Labels,
		input.Note,
	))
	if err != nil {
		if mappedErr := mapAssetServiceWriteError(err); mappedErr != nil {
			return assetservices.Record{}, mappedErr
		}
		return assetservices.Record{}, fmt.Errorf("create asset service: %w", err)
	}
	return record, nil
}

func (r *PostgresAssetServiceRepository) vpsAssetExists(ctx context.Context, vpsID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from vps_assets
			where vps_id = $1
		)`, vpsID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check vps asset %q for services: %w", vpsID, err)
	}
	return exists, nil
}

func assetServicePortArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func mapAssetServiceWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23503":
		switch pgErr.ConstraintName {
		case "asset_services_vps_fk", "asset_services_vps_id_fkey":
			return assetservices.ErrServiceOwnerNotFound
		case "asset_services_target_fk", "asset_services_target_id_fkey":
			return assetservices.ErrServiceTargetNotFound
		default:
			return assetservices.ErrInvalidServiceInput
		}
	case "23514":
		return assetservices.ErrInvalidServiceInput
	default:
		return nil
	}
}

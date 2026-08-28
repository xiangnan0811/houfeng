package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/createidempotency"
	"houfeng/internal/center/ids"
)

var _ assetdomains.Repository = (*PostgresAssetDomainRepository)(nil)

type assetDomainDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type assetDomainQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresAssetDomainRepository struct {
	db      assetDomainDB
	beginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func NewPostgresAssetDomainRepository(db *pgxpool.Pool) *PostgresAssetDomainRepository {
	return &PostgresAssetDomainRepository{db: db, beginTx: db.BeginTx}
}

const assetDomainCreateOperation = "asset-domain.create"

const assetDomainSelectColumns = `
	domain_id,
	vps_id,
	service_id,
	target_id,
	domain_name,
	purpose,
	status,
	registrar,
	expires_at,
	auto_renew,
	https_enabled,
	labels,
	note,
	created_at,
	updated_at`

const assetDomainQualifiedSelectColumns = `
	asset_domains.domain_id,
	asset_domains.vps_id,
	asset_domains.service_id,
	asset_domains.target_id,
	asset_domains.domain_name,
	asset_domains.purpose,
	asset_domains.status,
	asset_domains.registrar,
	asset_domains.expires_at,
	asset_domains.auto_renew,
	asset_domains.https_enabled,
	asset_domains.labels,
	asset_domains.note,
	asset_domains.created_at,
	asset_domains.updated_at`

type assetDomainScanner interface {
	Scan(dest ...any) error
}

func scanAssetDomain(row assetDomainScanner) (assetdomains.Record, error) {
	var record assetdomains.Record
	var expiresAt *time.Time
	if err := row.Scan(
		&record.DomainID,
		&record.VPSID,
		&record.ServiceID,
		&record.TargetID,
		&record.DomainName,
		&record.Purpose,
		&record.Status,
		&record.Registrar,
		&expiresAt,
		&record.AutoRenew,
		&record.HTTPSEnabled,
		&record.Labels,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return assetdomains.Record{}, err
	}
	record.ExpiresAt = assetdomains.DateFromTimePtr(expiresAt)
	return record, nil
}

func (r *PostgresAssetDomainRepository) ListAssetDomains(ctx context.Context, filters assetdomains.ListFilters) ([]assetdomains.Record, error) {
	return r.listAssetDomains(ctx, filters, true)
}

func (r *PostgresAssetDomainRepository) listAssetDomains(ctx context.Context, filters assetdomains.ListFilters, currentAssetScope bool) ([]assetdomains.Record, error) {
	filters = assetdomains.NormalizeListFilters(filters)
	if err := assetdomains.ValidateListFilters(filters); err != nil {
		return nil, err
	}

	args := []any{}
	conditions := []string{}
	if filters.VPSID != "" {
		args = append(args, filters.VPSID)
		conditions = append(conditions, fmt.Sprintf("asset_domains.vps_id = $%d", len(args)))
	}
	if filters.ServiceID != "" {
		args = append(args, filters.ServiceID)
		conditions = append(conditions, fmt.Sprintf("asset_domains.service_id = $%d", len(args)))
	}
	if filters.TargetID != "" {
		args = append(args, filters.TargetID)
		conditions = append(conditions, fmt.Sprintf("asset_domains.target_id = $%d", len(args)))
	}
	if filters.Status != "" {
		args = append(args, string(filters.Status))
		conditions = append(conditions, fmt.Sprintf("asset_domains.status = $%d", len(args)))
	}
	if currentAssetScope {
		conditions = append(conditions, "v.lifecycle_status not in ('cancelled', 'archived')")
	}

	selectColumns := assetDomainSelectColumns
	if currentAssetScope {
		selectColumns = assetDomainQualifiedSelectColumns
	}
	query := `
		select ` + selectColumns + `
		from asset_domains`
	if currentAssetScope {
		query += `
		join vps_assets v on v.vps_id = asset_domains.vps_id`
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by lower(asset_domains.domain_name), asset_domains.domain_id"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query asset domains: %w", err)
	}
	defer rows.Close()

	records := make([]assetdomains.Record, 0)
	for rows.Next() {
		record, err := scanAssetDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset domain: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset domains: %w", err)
	}
	return records, nil
}

func (r *PostgresAssetDomainRepository) ListAssetDomainsForVPS(ctx context.Context, vpsID string) ([]assetdomains.Record, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", assetdomains.ErrInvalidDomainInput)
	}
	exists, err := r.vpsAssetExists(ctx, vpsID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, assetdomains.ErrDomainOwnerNotFound
	}
	return r.listAssetDomains(ctx, assetdomains.ListFilters{VPSID: vpsID}, false)
}

func (r *PostgresAssetDomainRepository) CreateAssetDomain(ctx context.Context, input assetdomains.CreateInput) (assetdomains.Record, error) {
	input = assetdomains.NormalizeCreateInput(input)
	if err := assetdomains.ValidateCreateInput(input); err != nil {
		return assetdomains.Record{}, err
	}
	if input.ServiceID != nil {
		exists, err := r.serviceAssetBelongsToVPS(ctx, *input.ServiceID, input.VPSID)
		if err != nil {
			return assetdomains.Record{}, err
		}
		if !exists {
			return assetdomains.Record{}, assetdomains.ErrDomainServiceNotFound
		}
	}
	return insertAssetDomain(ctx, r.db, input)
}

func (r *PostgresAssetDomainRepository) CreateAssetDomainIdempotent(
	ctx context.Context,
	input assetdomains.CreateInput,
	idempotencyKey string,
) (assetdomains.Record, bool, error) {
	input = assetdomains.NormalizeCreateInput(input)
	if err := assetdomains.ValidateCreateInput(input); err != nil {
		return assetdomains.Record{}, false, err
	}
	key, err := createidempotency.NormalizeKey(idempotencyKey)
	if err != nil {
		return assetdomains.Record{}, false, err
	}
	digest, err := assetDomainCreateDigest(input)
	if err != nil {
		return assetdomains.Record{}, false, err
	}
	if r.beginTx == nil {
		return assetdomains.Record{}, false, errors.New("asset domain repository cannot create idempotently without transaction support")
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return assetdomains.Record{}, false, fmt.Errorf("begin asset domain create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := createidempotency.NamespacedLockKey(assetDomainCreateOperation, key)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1)::bigint)`, lockKey); err != nil {
		return assetdomains.Record{}, false, fmt.Errorf("lock asset domain create receipt: %w", err)
	}

	var storedDigest string
	var domainID string
	err = tx.QueryRow(ctx, `
		select request_digest, domain_id
		from asset_domain_create_idempotency
		where idempotency_key = $1`, key).Scan(&storedDigest, &domainID)
	if err == nil {
		if storedDigest != digest {
			return assetdomains.Record{}, false, createidempotency.ErrIdempotencyKeyReused
		}
		record, err := scanAssetDomain(tx.QueryRow(ctx, `
			select `+assetDomainSelectColumns+`
			from asset_domains
			where domain_id = $1
			  and vps_id = $2`, domainID, input.VPSID))
		if err != nil {
			return assetdomains.Record{}, false, fmt.Errorf("load replayed asset domain: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return assetdomains.Record{}, false, fmt.Errorf("commit asset domain create replay: %w", err)
		}
		return record, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return assetdomains.Record{}, false, fmt.Errorf("lookup asset domain create receipt: %w", err)
	}

	if input.ServiceID != nil {
		exists, err := assetDomainServiceBelongsToVPS(ctx, tx, *input.ServiceID, input.VPSID)
		if err != nil {
			return assetdomains.Record{}, false, fmt.Errorf("check asset domain service scope: %w", err)
		}
		if !exists {
			return assetdomains.Record{}, false, assetdomains.ErrDomainServiceNotFound
		}
	}
	record, err := insertAssetDomain(ctx, tx, input)
	if err != nil {
		return assetdomains.Record{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		insert into asset_domain_create_idempotency (
			idempotency_key,
			request_digest,
			domain_id
		) values ($1, $2, $3)`, key, digest, record.DomainID); err != nil {
		return assetdomains.Record{}, false, fmt.Errorf("record asset domain create receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdomains.Record{}, false, fmt.Errorf("commit asset domain create: %w", err)
	}
	return record, false, nil
}

func assetDomainCreateDigest(input assetdomains.CreateInput) (string, error) {
	return createidempotency.DigestNormalizedRequest(input)
}

func insertAssetDomain(ctx context.Context, db assetDomainQueryer, input assetdomains.CreateInput) (assetdomains.Record, error) {

	domainID, err := ids.New("dom")
	if err != nil {
		return assetdomains.Record{}, fmt.Errorf("generate asset domain id: %w", err)
	}

	record, err := scanAssetDomain(db.QueryRow(ctx, `
		insert into asset_domains (
			domain_id,
			vps_id,
			service_id,
			target_id,
			domain_name,
			purpose,
			status,
			registrar,
			expires_at,
			auto_renew,
			https_enabled,
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
			$10,
			$11,
			$12,
			$13
		)
		returning `+assetDomainSelectColumns,
		domainID,
		input.VPSID,
		nullableStringArg(input.ServiceID),
		nullableStringArg(input.TargetID),
		input.DomainName,
		input.Purpose,
		string(input.Status),
		input.Registrar,
		assetDomainDateArg(input.ExpiresAt),
		input.AutoRenew,
		input.HTTPSEnabled,
		input.Labels,
		input.Note,
	))
	if err != nil {
		if mappedErr := mapAssetDomainWriteError(err); mappedErr != nil {
			return assetdomains.Record{}, mappedErr
		}
		return assetdomains.Record{}, fmt.Errorf("create asset domain: %w", err)
	}
	return record, nil
}

func (r *PostgresAssetDomainRepository) vpsAssetExists(ctx context.Context, vpsID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from vps_assets
			where vps_id = $1
		)`, vpsID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check vps asset %q for domains: %w", vpsID, err)
	}
	return exists, nil
}

func (r *PostgresAssetDomainRepository) serviceAssetBelongsToVPS(ctx context.Context, serviceID, vpsID string) (bool, error) {
	return assetDomainServiceBelongsToVPS(ctx, r.db, serviceID, vpsID)
}

func assetDomainServiceBelongsToVPS(ctx context.Context, db assetDomainQueryer, serviceID, vpsID string) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx, `
		select exists (
			select 1
			from asset_services
			where service_id = $1
			  and vps_id = $2
		)`, serviceID, vpsID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check asset service %q for domain vps %q: %w", serviceID, vpsID, err)
	}
	return exists, nil
}

func assetDomainDateArg(value *assetdomains.Date) any {
	if value == nil {
		return nil
	}
	return value.Time
}

func mapAssetDomainWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23503":
		switch pgErr.ConstraintName {
		case "asset_domains_vps_fk", "asset_domains_vps_id_fkey":
			return assetdomains.ErrDomainOwnerNotFound
		case "asset_domains_service_fk", "asset_domains_service_id_fkey":
			return assetdomains.ErrDomainServiceNotFound
		case "asset_domains_target_fk", "asset_domains_target_id_fkey":
			return assetdomains.ErrDomainTargetNotFound
		default:
			return assetdomains.ErrInvalidDomainInput
		}
	case "23505":
		return assetdomains.ErrDomainConflict
	case "23514":
		return assetdomains.ErrInvalidDomainInput
	default:
		return nil
	}
}

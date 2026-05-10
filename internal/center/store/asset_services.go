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
	"houfeng/internal/center/ids"
)

var _ assetservices.Repository = (*PostgresAssetServiceRepository)(nil)

type assetServiceDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresAssetServiceRepository struct {
	db assetServiceDB
}

func NewPostgresAssetServiceRepository(db *pgxpool.Pool) *PostgresAssetServiceRepository {
	return &PostgresAssetServiceRepository{db: db}
}

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
	filters = assetservices.NormalizeListFilters(filters)
	if err := assetservices.ValidateListFilters(filters); err != nil {
		return nil, err
	}

	args := []any{}
	conditions := []string{}
	if filters.VPSID != "" {
		args = append(args, filters.VPSID)
		conditions = append(conditions, fmt.Sprintf("vps_id = $%d", len(args)))
	}
	if filters.TargetID != "" {
		args = append(args, filters.TargetID)
		conditions = append(conditions, fmt.Sprintf("target_id = $%d", len(args)))
	}
	if filters.ServiceType != "" {
		args = append(args, string(filters.ServiceType))
		conditions = append(conditions, fmt.Sprintf("service_type = $%d", len(args)))
	}
	if filters.Status != "" {
		args = append(args, string(filters.Status))
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	query := `
		select ` + assetServiceSelectColumns + `
		from asset_services`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by lower(name), service_id"

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
	return r.ListAssetServices(ctx, assetservices.ListFilters{VPSID: vpsID})
}

func (r *PostgresAssetServiceRepository) CreateAssetService(ctx context.Context, input assetservices.CreateInput) (assetservices.Record, error) {
	input = assetservices.NormalizeCreateInput(input)
	if err := assetservices.ValidateCreateInput(input); err != nil {
		return assetservices.Record{}, err
	}

	serviceID, err := ids.New("svc")
	if err != nil {
		return assetservices.Record{}, fmt.Errorf("generate asset service id: %w", err)
	}

	record, err := scanAssetService(r.db.QueryRow(ctx, `
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

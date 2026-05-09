package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/vpsassets"
)

var _ vpsassets.Repository = (*PostgresVPSAssetRepository)(nil)

type vpsAssetDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresVPSAssetRepository struct {
	db vpsAssetDB
}

func NewPostgresVPSAssetRepository(db *pgxpool.Pool) *PostgresVPSAssetRepository {
	return &PostgresVPSAssetRepository{db: db}
}

const vpsAssetSelectColumns = `
	vps_id,
	display_name,
	provider_id,
	provider_name,
	product_name,
	order_ref,
	country,
	region,
	city,
	datacenter,
	ipv4,
	ipv6,
	ssh_host,
	ssh_port,
	ssh_user,
	os_name,
	virtualization,
	lifecycle_status,
	usage_status,
	renewal_decision,
	importance,
	labels,
	note,
	created_at,
	updated_at,
	archived_at`

type vpsAssetScanner interface {
	Scan(dest ...any) error
}

func scanVPSAsset(row vpsAssetScanner) (vpsassets.Record, error) {
	var record vpsassets.Record
	if err := row.Scan(
		&record.VPSID,
		&record.DisplayName,
		&record.ProviderID,
		&record.ProviderName,
		&record.ProductName,
		&record.OrderRef,
		&record.Country,
		&record.Region,
		&record.City,
		&record.Datacenter,
		&record.IPv4,
		&record.IPv6,
		&record.SSHHost,
		&record.SSHPort,
		&record.SSHUser,
		&record.OSName,
		&record.Virtualization,
		&record.LifecycleStatus,
		&record.UsageStatus,
		&record.RenewalDecision,
		&record.Importance,
		&record.Labels,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.ArchivedAt,
	); err != nil {
		return vpsassets.Record{}, err
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) ListVPSAssets(ctx context.Context, filters vpsassets.ListFilters) ([]vpsassets.Record, error) {
	filters = vpsassets.NormalizeListFilters(filters)
	if err := vpsassets.ValidateListFilters(filters); err != nil {
		return nil, err
	}

	args := []any{}
	conditions := []string{}
	if filters.ProviderID != "" {
		args = append(args, filters.ProviderID)
		conditions = append(conditions, fmt.Sprintf("provider_id = $%d", len(args)))
	}
	if filters.LifecycleStatus != "" {
		args = append(args, string(filters.LifecycleStatus))
		conditions = append(conditions, fmt.Sprintf("lifecycle_status = $%d", len(args)))
	}
	if filters.UsageStatus != "" {
		args = append(args, string(filters.UsageStatus))
		conditions = append(conditions, fmt.Sprintf("usage_status = $%d", len(args)))
	}
	if filters.RenewalDecision != "" {
		args = append(args, string(filters.RenewalDecision))
		conditions = append(conditions, fmt.Sprintf("renewal_decision = $%d", len(args)))
	}

	query := `
		select ` + vpsAssetSelectColumns + `
		from vps_assets`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by lower(display_name), vps_id"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query vps assets: %w", err)
	}
	defer rows.Close()

	records := make([]vpsassets.Record, 0)
	for rows.Next() {
		record, err := scanVPSAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan vps asset: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vps assets: %w", err)
	}
	return records, nil
}

func (r *PostgresVPSAssetRepository) GetVPSAsset(ctx context.Context, vpsID string) (vpsassets.Record, error) {
	record, err := scanVPSAsset(r.db.QueryRow(ctx, `
		select `+vpsAssetSelectColumns+`
		from vps_assets
		where vps_id = $1`, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("query vps asset %q: %w", vpsID, err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) CreateVPSAsset(ctx context.Context, input vpsassets.CreateInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizeCreateInput(input)
	if err := vpsassets.ValidateCreateInput(input); err != nil {
		return vpsassets.Record{}, err
	}

	vpsID, err := ids.New("vps")
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("generate vps asset id: %w", err)
	}

	record, err := scanVPSAsset(r.db.QueryRow(ctx, `
		insert into vps_assets (
			vps_id,
			display_name,
			provider_id,
			provider_name,
			product_name,
			order_ref,
			country,
			region,
			city,
			datacenter,
			ipv4,
			ipv6,
			ssh_host,
			ssh_port,
			ssh_user,
			os_name,
			virtualization,
			lifecycle_status,
			usage_status,
			renewal_decision,
			importance,
			labels,
			note,
			archived_at
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
			$13,
			$14,
			$15,
			$16,
			$17,
			$18,
			$19,
			$20,
			$21,
			$22,
			$23,
			case when $18::text = 'archived' then now() else null end
		)
		returning `+vpsAssetSelectColumns,
		vpsID,
		input.DisplayName,
		nullableStringArg(input.ProviderID),
		input.ProviderName,
		input.ProductName,
		input.OrderRef,
		input.Country,
		input.Region,
		input.City,
		input.Datacenter,
		input.IPv4,
		input.IPv6,
		input.SSHHost,
		input.SSHPort,
		input.SSHUser,
		input.OSName,
		input.Virtualization,
		string(input.LifecycleStatus),
		string(input.UsageStatus),
		string(input.RenewalDecision),
		input.Importance,
		input.Labels,
		input.Note,
	))
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, fmt.Errorf("create vps asset: %w", err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) PatchVPSAsset(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizePatchInput(input)
	if err := vpsassets.ValidatePatchInput(input); err != nil {
		return vpsassets.Record{}, err
	}
	if !input.HasChanges() {
		return r.GetVPSAsset(ctx, vpsID)
	}

	record, err := scanVPSAsset(r.db.QueryRow(ctx, `
		update vps_assets
		set display_name = case when $2::boolean then $3 else display_name end,
		    provider_id = case when $4::boolean then $5::text else provider_id end,
		    provider_name = case when $6::boolean then $7 else provider_name end,
		    product_name = case when $8::boolean then $9 else product_name end,
		    order_ref = case when $10::boolean then $11 else order_ref end,
		    country = case when $12::boolean then $13 else country end,
		    region = case when $14::boolean then $15 else region end,
		    city = case when $16::boolean then $17 else city end,
		    datacenter = case when $18::boolean then $19 else datacenter end,
		    ipv4 = case when $20::boolean then $21 else ipv4 end,
		    ipv6 = case when $22::boolean then $23 else ipv6 end,
		    ssh_host = case when $24::boolean then $25 else ssh_host end,
		    ssh_port = case when $26::boolean then $27::integer else ssh_port end,
		    ssh_user = case when $28::boolean then $29 else ssh_user end,
		    os_name = case when $30::boolean then $31 else os_name end,
		    virtualization = case when $32::boolean then $33 else virtualization end,
		    lifecycle_status = case when $34::boolean then $35 else lifecycle_status end,
		    usage_status = case when $36::boolean then $37 else usage_status end,
		    renewal_decision = case when $38::boolean then $39 else renewal_decision end,
		    importance = case when $40::boolean then $41 else importance end,
		    labels = case when $42::boolean then $43::text[] else labels end,
		    note = case when $44::boolean then $45 else note end,
		    archived_at = case
		        when $34::boolean and $35::text = 'archived' then coalesce(archived_at, now())
		        when $34::boolean and $35::text <> 'archived' then null
		        else archived_at
		    end,
		    updated_at = now()
		where vps_id = $1
		returning `+vpsAssetSelectColumns,
		vpsID,
		input.DisplayName.Set,
		input.DisplayName.Value,
		input.ProviderID.Set,
		nullableStringArg(input.ProviderID.Value),
		input.ProviderName.Set,
		input.ProviderName.Value,
		input.ProductName.Set,
		input.ProductName.Value,
		input.OrderRef.Set,
		input.OrderRef.Value,
		input.Country.Set,
		input.Country.Value,
		input.Region.Set,
		input.Region.Value,
		input.City.Set,
		input.City.Value,
		input.Datacenter.Set,
		input.Datacenter.Value,
		input.IPv4.Set,
		input.IPv4.Value,
		input.IPv6.Set,
		input.IPv6.Value,
		input.SSHHost.Set,
		input.SSHHost.Value,
		input.SSHPort.Set,
		input.SSHPort.Value,
		input.SSHUser.Set,
		input.SSHUser.Value,
		input.OSName.Set,
		input.OSName.Value,
		input.Virtualization.Set,
		input.Virtualization.Value,
		input.LifecycleStatus.Set,
		string(input.LifecycleStatus.Value),
		input.UsageStatus.Set,
		string(input.UsageStatus.Value),
		input.RenewalDecision.Set,
		string(input.RenewalDecision.Value),
		input.Importance.Set,
		input.Importance.Value,
		input.Labels.Set,
		input.Labels.Values,
		input.Note.Set,
		input.Note.Value,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, fmt.Errorf("patch vps asset %q: %w", vpsID, err)
	}
	return record, nil
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func isVPSAssetInvalidPostgresError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" || pgErr.Code == "23514"
}

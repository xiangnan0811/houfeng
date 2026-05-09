package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/providers"
)

var _ providers.Repository = (*PostgresProviderRepository)(nil)

type providerDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresProviderRepository struct {
	db providerDB
}

func NewPostgresProviderRepository(db *pgxpool.Pool) *PostgresProviderRepository {
	return &PostgresProviderRepository{db: db}
}

const providerSelectColumns = `
	provider_id,
	name,
	website,
	panel_url,
	account_hint,
	country,
	note,
	rating,
	labels,
	created_at,
	updated_at`

type providerScanner interface {
	Scan(dest ...any) error
}

func scanProvider(row providerScanner) (providers.Record, error) {
	var record providers.Record
	if err := row.Scan(
		&record.ProviderID,
		&record.Name,
		&record.Website,
		&record.PanelURL,
		&record.AccountHint,
		&record.Country,
		&record.Note,
		&record.Rating,
		&record.Labels,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return providers.Record{}, err
	}
	return record, nil
}

func (r *PostgresProviderRepository) ListProviders(ctx context.Context) ([]providers.Record, error) {
	rows, err := r.db.Query(ctx, `
		select `+providerSelectColumns+`
		from providers
		order by lower(name), provider_id`)
	if err != nil {
		return nil, fmt.Errorf("query providers: %w", err)
	}
	defer rows.Close()

	records := make([]providers.Record, 0)
	for rows.Next() {
		record, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate providers: %w", err)
	}
	return records, nil
}

func (r *PostgresProviderRepository) GetProvider(ctx context.Context, providerID string) (providers.Record, error) {
	record, err := scanProvider(r.db.QueryRow(ctx, `
		select `+providerSelectColumns+`
		from providers
		where provider_id = $1`, providerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return providers.Record{}, providers.ErrProviderNotFound
	}
	if err != nil {
		return providers.Record{}, fmt.Errorf("query provider %q: %w", providerID, err)
	}
	return record, nil
}

func (r *PostgresProviderRepository) CreateProvider(ctx context.Context, input providers.CreateInput) (providers.Record, error) {
	input = providers.NormalizeCreateInput(input)
	if err := providers.ValidateCreateInput(input); err != nil {
		return providers.Record{}, err
	}

	providerID, err := ids.New("pv")
	if err != nil {
		return providers.Record{}, fmt.Errorf("generate provider id: %w", err)
	}

	record, err := scanProvider(r.db.QueryRow(ctx, `
		insert into providers (
			provider_id,
			name,
			website,
			panel_url,
			account_hint,
			country,
			note,
			rating,
			labels
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9
		)
		returning `+providerSelectColumns,
		providerID,
		input.Name,
		input.Website,
		input.PanelURL,
		input.AccountHint,
		input.Country,
		input.Note,
		providerRatingArg(input.Rating),
		input.Labels,
	))
	if err != nil {
		return providers.Record{}, fmt.Errorf("create provider: %w", err)
	}
	return record, nil
}

func (r *PostgresProviderRepository) PatchProvider(ctx context.Context, providerID string, input providers.PatchInput) (providers.Record, error) {
	input = providers.NormalizePatchInput(input)
	if err := providers.ValidatePatchInput(input); err != nil {
		return providers.Record{}, err
	}
	if !input.HasChanges() {
		return r.GetProvider(ctx, providerID)
	}

	record, err := scanProvider(r.db.QueryRow(ctx, `
		update providers
		set name = case when $2::boolean then $3 else name end,
		    website = case when $4::boolean then $5 else website end,
		    panel_url = case when $6::boolean then $7 else panel_url end,
		    account_hint = case when $8::boolean then $9 else account_hint end,
		    country = case when $10::boolean then $11 else country end,
		    note = case when $12::boolean then $13 else note end,
		    rating = case when $14::boolean then $15::integer else rating end,
		    labels = case when $16::boolean then $17::text[] else labels end,
		    updated_at = now()
		where provider_id = $1
		returning `+providerSelectColumns,
		providerID,
		input.Name.Set,
		input.Name.Value,
		input.Website.Set,
		input.Website.Value,
		input.PanelURL.Set,
		input.PanelURL.Value,
		input.AccountHint.Set,
		input.AccountHint.Value,
		input.Country.Set,
		input.Country.Value,
		input.Note.Set,
		input.Note.Value,
		input.Rating.Set,
		providerRatingArg(input.Rating.Value),
		input.Labels.Set,
		input.Labels.Values,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return providers.Record{}, providers.ErrProviderNotFound
	}
	if err != nil {
		return providers.Record{}, fmt.Errorf("patch provider %q: %w", providerID, err)
	}
	return record, nil
}

func providerRatingArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

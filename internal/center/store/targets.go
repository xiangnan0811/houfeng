package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/targets"
)

type PostgresTargetRepository struct {
	db *pgxpool.Pool
}

func NewPostgresTargetRepository(db *pgxpool.Pool) *PostgresTargetRepository {
	return &PostgresTargetRepository{db: db}
}

const targetSelectColumns = `
	target_id,
	name,
	target_type,
	host,
	base_port,
	execution_node_labels,
	run_status,
	labels,
	note,
	current_health_status,
	current_active_incident_count,
	last_success_at,
	last_failure_at,
	current_primary_issue_summary,
	created_at,
	updated_at`

const probeItemSelectColumns = `
	probe_item_id,
	target_id,
	probe_kind,
	enabled,
	frequency_tier,
	timeout_seconds,
	config,
	created_at,
	updated_at`

type targetScanner interface {
	Scan(dest ...any) error
}

var _ targets.Repository = (*PostgresTargetRepository)(nil)
var _ observations.ProbeMetadataRepository = (*PostgresTargetRepository)(nil)

func scanTarget(row targetScanner) (targets.TargetRecord, error) {
	var record targets.TargetRecord
	if err := row.Scan(
		&record.TargetID,
		&record.Name,
		&record.TargetType,
		&record.Host,
		&record.BasePort,
		&record.ExecutionNodeLabels,
		&record.RunStatus,
		&record.Labels,
		&record.Note,
		&record.CurrentHealthStatus,
		&record.CurrentActiveIncidentCount,
		&record.LastSuccessAt,
		&record.LastFailureAt,
		&record.CurrentPrimaryIssueSummary,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return targets.TargetRecord{}, err
	}
	return record, nil
}

func scanProbeItem(row targetScanner) (targets.ProbeItemRecord, error) {
	var record targets.ProbeItemRecord
	var config []byte
	if err := row.Scan(
		&record.ProbeItemID,
		&record.TargetID,
		&record.ProbeKind,
		&record.Enabled,
		&record.FrequencyTier,
		&record.TimeoutSeconds,
		&config,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return targets.ProbeItemRecord{}, err
	}
	record.Config = json.RawMessage(append([]byte(nil), config...))
	return record, nil
}

func (r *PostgresTargetRepository) ListTargets(ctx context.Context) ([]targets.TargetRecord, error) {
	rows, err := r.db.Query(ctx, `
		select `+targetSelectColumns+`
		from targets
		order by created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("query targets: %w", err)
	}
	defer rows.Close()

	records := make([]targets.TargetRecord, 0)
	for rows.Next() {
		record, err := scanTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate targets: %w", err)
	}

	return records, nil
}

func (r *PostgresTargetRepository) GetTarget(ctx context.Context, targetID string) (targets.TargetRecord, error) {
	record, err := scanTarget(r.db.QueryRow(ctx, `
		select `+targetSelectColumns+`
		from targets
		where target_id = $1`, targetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return targets.TargetRecord{}, targets.ErrTargetNotFound
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("query target %q: %w", targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) CreateTarget(ctx context.Context, input targets.CreateTargetInput) (targets.TargetRecord, error) {
	targetID, err := ids.New("tg")
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("generate target id: %w", err)
	}

	record, err := scanTarget(r.db.QueryRow(ctx, `
		insert into targets (
			target_id,
			name,
			target_type,
			host,
			base_port,
			execution_node_labels,
			run_status,
			labels,
			note,
			current_health_status,
			current_active_incident_count,
			current_primary_issue_summary
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
			0,
			''
		)
		returning `+targetSelectColumns,
		targetID,
		input.Name,
		input.TargetType,
		input.Host,
		input.BasePort,
		input.ExecutionNodeLabels,
		input.RunStatus,
		input.Labels,
		input.Note,
		targets.HealthNormal,
	))
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("create target: %w", err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) ListProbeItems(ctx context.Context, targetID string) ([]targets.ProbeItemRecord, error) {
	if _, err := r.GetTarget(ctx, targetID); err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, `
		select `+probeItemSelectColumns+`
		from probe_items
		where target_id = $1
		order by created_at desc`, targetID)
	if err != nil {
		return nil, fmt.Errorf("query probe items for target %q: %w", targetID, err)
	}
	defer rows.Close()

	records := make([]targets.ProbeItemRecord, 0)
	for rows.Next() {
		record, err := scanProbeItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan probe item: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate probe items: %w", err)
	}

	return records, nil
}

func (r *PostgresTargetRepository) GetProbeMetadata(ctx context.Context, probeItemID string) (observations.ProbeMetadata, error) {
	var metadata observations.ProbeMetadata
	if err := r.db.QueryRow(ctx, `
		select target_id, probe_kind
		from probe_items
		where probe_item_id = $1`, probeItemID).Scan(&metadata.TargetID, &metadata.ProbeKind); errors.Is(err, pgx.ErrNoRows) {
		return observations.ProbeMetadata{}, observations.ErrProbeMetadataNotFound
	} else if err != nil {
		return observations.ProbeMetadata{}, fmt.Errorf("query probe metadata %q: %w", probeItemID, err)
	}

	return metadata, nil
}

func (r *PostgresTargetRepository) CreateProbeItem(ctx context.Context, targetID string, input targets.CreateProbeItemInput) (targets.ProbeItemRecord, error) {
	if _, err := r.GetTarget(ctx, targetID); err != nil {
		return targets.ProbeItemRecord{}, err
	}

	probeItemID, err := ids.New("pb")
	if err != nil {
		return targets.ProbeItemRecord{}, fmt.Errorf("generate probe item id: %w", err)
	}

	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}

	record, err := scanProbeItem(r.db.QueryRow(ctx, `
		insert into probe_items (
			probe_item_id,
			target_id,
			probe_kind,
			enabled,
			frequency_tier,
			timeout_seconds,
			config
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7::jsonb
		)
		returning `+probeItemSelectColumns,
		probeItemID,
		targetID,
		input.ProbeKind,
		input.Enabled,
		input.FrequencyTier,
		input.TimeoutSeconds,
		[]byte(config),
	))
	if err != nil {
		return targets.ProbeItemRecord{}, fmt.Errorf("create probe item for target %q: %w", targetID, err)
	}
	return record, nil
}

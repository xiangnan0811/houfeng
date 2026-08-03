package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/targets"
)

type targetDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresTargetRepository struct {
	db targetDB
}

func NewPostgresTargetRepository(db *pgxpool.Pool) *PostgresTargetRepository {
	return &PostgresTargetRepository{db: db}
}

var ErrInvalidTargetRuntimeTransition = errors.New("invalid target runtime transition")

var targetSelectColumnNames = []string{
	"target_id",
	"name",
	"target_type",
	"host",
	"base_port",
	"execution_monitoring_instance_labels",
	"run_status",
	"group",
	"labels",
	"note",
	"current_health_status",
	"current_active_incident_count",
	"last_success_at",
	"last_failure_at",
	"current_primary_issue_summary",
	"created_at",
	"updated_at",
}

const targetSelectColumns = `
	target_id,
	name,
	target_type,
	host,
	base_port,
	execution_monitoring_instance_labels,
	run_status,
	"group",
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
		&record.ExecutionMonitoringInstanceLabels,
		&record.RunStatus,
		&record.Group,
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

func qualifiedTargetSelectColumns(alias string) string {
	parts := make([]string, 0, len(targetSelectColumnNames))
	for _, column := range targetSelectColumnNames {
		parts = append(parts, alias+"."+column)
	}
	return strings.Join(parts, ",\n\t\t")
}

func scanTargetWithPreviousRunStatus(row targetScanner) (targets.TargetRecord, string, error) {
	var (
		record     targets.TargetRecord
		priorState string
	)
	if err := row.Scan(
		&record.TargetID,
		&record.Name,
		&record.TargetType,
		&record.Host,
		&record.BasePort,
		&record.ExecutionMonitoringInstanceLabels,
		&record.RunStatus,
		&record.Group,
		&record.Labels,
		&record.Note,
		&record.CurrentHealthStatus,
		&record.CurrentActiveIncidentCount,
		&record.LastSuccessAt,
		&record.LastFailureAt,
		&record.CurrentPrimaryIssueSummary,
		&record.CreatedAt,
		&record.UpdatedAt,
		&priorState,
	); err != nil {
		return targets.TargetRecord{}, "", err
	}
	return record, priorState, nil
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
		where not exists (
			select 1
			from (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a
			where a.target_id = targets.target_id
		)
		or exists (
			select 1
			from (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a
			join vps_assets v on v.vps_id = a.vps_id
			where a.target_id = targets.target_id
			  and v.lifecycle_status not in ('cancelled', 'archived')
		)
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

func (r *PostgresTargetRepository) UpdateTargetMetadata(ctx context.Context, targetID string, input targets.UpdateMetadataInput) (targets.TargetRecord, error) {
	args := []any{targetID}
	if input.Group != nil {
		args = append(args, *input.Group)
	} else {
		args = append(args, nil)
	}
	args = append(args, input.Labels, input.Note)
	precondition := ""
	if input.ExpectedUpdatedAt != nil {
		args = append(args, *input.ExpectedUpdatedAt)
		precondition = `
		  and updated_at = $5`
	}

	record, err := scanTarget(r.db.QueryRow(ctx, `
		update targets
		set "group" = coalesce($2, "group"),
		    labels = $3,
		    note = $4,
			    updated_at = now()
		where target_id = $1`+precondition+`
		returning `+targetSelectColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if input.ExpectedUpdatedAt != nil {
			exists, existsErr := r.targetExists(ctx, targetID)
			if existsErr != nil {
				return targets.TargetRecord{}, fmt.Errorf("check target metadata conflict %q: %w", targetID, existsErr)
			}
			if exists {
				return targets.TargetRecord{}, targets.ErrTargetMetadataConflict
			}
		}
		return targets.TargetRecord{}, targets.ErrTargetNotFound
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("update target metadata %q: %w", targetID, err)
	}
	return record, nil
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

func (r *PostgresTargetRepository) loadTargetRecordSubject(ctx context.Context, targetID string) (targetRecordSubject, error) {
	var subject targetRecordSubject
	err := r.db.QueryRow(ctx, `
		select target_id, name, target_type
		from targets
		where target_id = $1`, targetID).Scan(
		&subject.TargetID,
		&subject.DisplayName,
		&subject.TargetType,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return targetRecordSubject{}, targets.ErrTargetNotFound
	}
	if err != nil {
		return targetRecordSubject{}, fmt.Errorf("query target record subject: %w", err)
	}
	return subject, nil
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
			execution_monitoring_instance_labels,
			run_status,
			"group",
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
		input.ExecutionMonitoringInstanceLabels,
		input.RunStatus,
		input.Group,
		input.Labels,
		input.Note,
		targets.HealthNormal,
	))
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("create target: %w", err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) targetExists(ctx context.Context, targetID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from targets
			where target_id = $1
		)`,
		targetID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check target %q existence: %w", targetID, err)
	}
	return exists, nil
}

func insertTargetRuntimeEvent(
	ctx context.Context,
	tx pgx.Tx,
	record targets.TargetRecord,
	eventType incidents.EventType,
	summary string,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate target runtime event id: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"run_status": record.RunStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal target runtime event payload: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		insert into state_change_events (
			event_id,
			object_type,
			object_id,
			event_type,
			severity,
			summary,
			payload,
			created_at
		) values ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`,
		eventID,
		string(incidents.ObjectTypeTarget),
		record.TargetID,
		string(eventType),
		"",
		summary,
		payload,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert runtime event for target %q: %w", record.TargetID, err)
	}
	return nil
}

func (r *PostgresTargetRepository) SetTargetMaintenance(ctx context.Context, targetID string) (targets.TargetRecord, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin set target maintenance transaction for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set run_status = '维护中',
			updated_at = now()
		where target_id = $1
			and run_status = '启用'
		returning `+targetSelectColumns,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.targetExists(ctx, targetID)
		if existsErr != nil {
			return targets.TargetRecord{}, fmt.Errorf("set target maintenance for %q: %w", targetID, existsErr)
		}
		if !exists {
			return targets.TargetRecord{}, fmt.Errorf("%w: target %q", targets.ErrTargetNotFound, targetID)
		}
		return targets.TargetRecord{}, fmt.Errorf("%w: target %q cannot enter maintenance from current run status", ErrInvalidTargetRuntimeTransition, targetID)
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("set target maintenance for %q: %w", targetID, err)
	}
	if err := insertTargetRuntimeEvent(ctx, tx, record, incidents.EventTargetMaintenanceEntered, "目标运行已进入维护"); err != nil {
		return targets.TargetRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit set target maintenance for %q: %w", targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) PauseTargetRun(ctx context.Context, targetID string) (targets.TargetRecord, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin pause target transaction for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set run_status = '暂停',
			updated_at = now()
		where target_id = $1
			and run_status in ('启用', '维护中')
		returning `+targetSelectColumns,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.targetExists(ctx, targetID)
		if existsErr != nil {
			return targets.TargetRecord{}, fmt.Errorf("pause target %q: %w", targetID, existsErr)
		}
		if !exists {
			return targets.TargetRecord{}, fmt.Errorf("%w: target %q", targets.ErrTargetNotFound, targetID)
		}
		return targets.TargetRecord{}, fmt.Errorf("%w: target %q cannot pause from current run status", ErrInvalidTargetRuntimeTransition, targetID)
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("pause target %q: %w", targetID, err)
	}
	if err := insertTargetRuntimeEvent(ctx, tx, record, incidents.EventTargetPaused, "目标运行已暂停"); err != nil {
		return targets.TargetRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit pause target %q: %w", targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) ResumeTargetRun(ctx context.Context, targetID string) (targets.TargetRecord, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin resume target transaction for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, previousStatus, err := scanTargetWithPreviousRunStatus(tx.QueryRow(ctx, `
		with prior as (
			select run_status
			from targets
			where target_id = $1
		),
		updated as (
			update targets
			set run_status = '启用',
				updated_at = now()
			where target_id = $1
				and run_status in ('维护中', '暂停')
			returning `+targetSelectColumns+`
		)
		select `+qualifiedTargetSelectColumns("updated")+`, prior.run_status
		from updated
		join prior on true`,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.targetExists(ctx, targetID)
		if existsErr != nil {
			return targets.TargetRecord{}, fmt.Errorf("resume target %q: %w", targetID, existsErr)
		}
		if !exists {
			return targets.TargetRecord{}, fmt.Errorf("%w: target %q", targets.ErrTargetNotFound, targetID)
		}
		return targets.TargetRecord{}, fmt.Errorf("%w: target %q cannot resume from current run status", ErrInvalidTargetRuntimeTransition, targetID)
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("resume target %q: %w", targetID, err)
	}

	eventType := incidents.EventTargetResumed
	summary := "目标运行已恢复"
	if previousStatus == targets.RunStatusMaintenance {
		eventType = incidents.EventTargetMaintenanceExited
		summary = "目标运行已退出维护"
	}
	if err := insertTargetRuntimeEvent(ctx, tx, record, eventType, summary); err != nil {
		return targets.TargetRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit resume target %q: %w", targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) ArchiveTarget(ctx context.Context, targetID string) (targets.TargetRecord, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin archive target transaction for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set run_status = '已归档',
			updated_at = now()
		where target_id = $1
			and run_status in ('启用', '维护中', '暂停')
		returning `+targetSelectColumns,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.targetExists(ctx, targetID)
		if existsErr != nil {
			return targets.TargetRecord{}, fmt.Errorf("archive target %q: %w", targetID, existsErr)
		}
		if !exists {
			return targets.TargetRecord{}, fmt.Errorf("%w: target %q", targets.ErrTargetNotFound, targetID)
		}
		return targets.TargetRecord{}, fmt.Errorf("%w: target %q cannot archive from current run status", ErrInvalidTargetRuntimeTransition, targetID)
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("archive target %q: %w", targetID, err)
	}
	if err := insertTargetRuntimeEvent(ctx, tx, record, incidents.EventTargetArchived, "目标已归档"); err != nil {
		return targets.TargetRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit archive target %q: %w", targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) RestoreArchivedTargetToPaused(ctx context.Context, targetID string) (targets.TargetRecord, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin restore archived target transaction for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set run_status = '暂停',
			updated_at = now()
		where target_id = $1
			and run_status = '已归档'
		returning `+targetSelectColumns,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.targetExists(ctx, targetID)
		if existsErr != nil {
			return targets.TargetRecord{}, fmt.Errorf("restore archived target %q: %w", targetID, existsErr)
		}
		if !exists {
			return targets.TargetRecord{}, fmt.Errorf("%w: target %q", targets.ErrTargetNotFound, targetID)
		}
		return targets.TargetRecord{}, fmt.Errorf("%w: target %q cannot restore to paused from current run status", ErrInvalidTargetRuntimeTransition, targetID)
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("restore archived target %q: %w", targetID, err)
	}
	if err := insertTargetRuntimeEvent(ctx, tx, record, incidents.EventTargetRestoredToPaused, "目标已恢复到暂停"); err != nil {
		return targets.TargetRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit restore archived target %q: %w", targetID, err)
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
	return getProbeMetadata(ctx, r.db, probeItemID)
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

func (r *PostgresTargetRepository) UpdateProbeItem(ctx context.Context, targetID string, probeItemID string, input targets.UpdateProbeItemInput) (targets.ProbeItemRecord, error) {
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}

	record, err := scanProbeItem(r.db.QueryRow(ctx, `
		update probe_items
		set probe_kind = $3,
			enabled = $4,
			frequency_tier = $5,
			timeout_seconds = $6,
			config = $7::jsonb,
			updated_at = now()
		where target_id = $1
			and probe_item_id = $2
		returning `+probeItemSelectColumns,
		targetID,
		probeItemID,
		input.ProbeKind,
		input.Enabled,
		input.FrequencyTier,
		input.TimeoutSeconds,
		[]byte(config),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, targetErr := r.GetTarget(ctx, targetID); targetErr != nil {
			return targets.ProbeItemRecord{}, targetErr
		}
		return targets.ProbeItemRecord{}, fmt.Errorf("%w: probe item %q under target %q", targets.ErrProbeItemNotFound, probeItemID, targetID)
	}
	if err != nil {
		return targets.ProbeItemRecord{}, fmt.Errorf("update probe item %q for target %q: %w", probeItemID, targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) DeleteProbeItem(ctx context.Context, targetID string, probeItemID string) error {
	tag, err := r.db.Exec(ctx, `
		delete from probe_items
		where target_id = $1
			and probe_item_id = $2`, targetID, probeItemID)
	if err != nil {
		return fmt.Errorf("delete probe item %q for target %q: %w", probeItemID, targetID, err)
	}
	if tag.RowsAffected() == 0 {
		if _, targetErr := r.GetTarget(ctx, targetID); targetErr != nil {
			return targetErr
		}
		return fmt.Errorf("%w: probe item %q under target %q", targets.ErrProbeItemNotFound, probeItemID, targetID)
	}
	return nil
}

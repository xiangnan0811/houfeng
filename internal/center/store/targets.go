package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
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

var _ targets.LifecycleReviewRepository = (*PostgresTargetRepository)(nil)

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
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin target metadata transaction for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

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

	record, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set "group" = coalesce($2, "group"),
		    labels = $3,
		    note = $4,
		    updated_at = now()
		where target_id = $1`+precondition+`
		returning `+targetSelectColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if input.ExpectedUpdatedAt != nil {
			var exists bool
			if existsErr := tx.QueryRow(ctx, `select exists (select 1 from targets where target_id = $1)`, targetID).Scan(&exists); existsErr != nil {
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
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit target metadata update %q: %w", targetID, err)
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

func (r *PostgresTargetRepository) GetTargetLifecycleReview(ctx context.Context, targetID string) (targets.LifecycleReview, error) {
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return targets.LifecycleReview{}, fmt.Errorf("begin target lifecycle review for %q: %w", targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, impacts, digest, err := loadTargetLifecycleReviewTx(ctx, tx, targetID)
	if err != nil {
		return targets.LifecycleReview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.LifecycleReview{}, fmt.Errorf("commit target lifecycle review for %q: %w", targetID, err)
	}
	return targets.LifecycleReview{DependencyImpacts: impacts, PreviewDigest: digest}, nil
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

	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin create target transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanTarget(tx.QueryRow(ctx, `
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
			$11,
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
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit create target: %w", err)
	}
	return record, nil
}

func insertTargetRuntimeEvent(
	ctx context.Context,
	tx pgx.Tx,
	record targets.TargetRecord,
	eventType incidents.EventType,
	summary string,
	priorState string,
	provenance monitoringEventProvenance,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate target runtime event id: %w", err)
	}

	eventAt := canonicalTask4MonitoringEventTimestamp(record.UpdatedAt)
	payload, err := marshalTask4MonitoringEventPayload(task4MonitoringEventPayload{
		ObjectType:          incidents.ObjectTypeTarget,
		EventType:           eventType,
		EventAt:             eventAt,
		RecordedAt:          eventAt,
		IsBackfilled:        false,
		Provenance:          provenance,
		ProducerVersion:     monitoringEventProducerVersion,
		RuleVersion:         monitoringEventTargetRuleVersion,
		PriorState:          priorState,
		ResultingState:      record.RunStatus,
		CorrectionOfEventID: "",
		RunStatus:           record.RunStatus,
	})
	if err != nil {
		return fmt.Errorf("build target runtime event payload: %w", err)
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
		eventAt,
	); err != nil {
		return fmt.Errorf("insert runtime event for target %q: %w", record.TargetID, err)
	}
	return nil
}

var ErrInvalidTargetRuntimeAction = errors.New("invalid target runtime action")

type targetTransitionSpec struct {
	runStatus string
	eventType incidents.EventType
	summary   string
	noChange  bool
}

func validTargetRuntimeAction(action string) bool {
	switch action {
	case "maintenance", "pause", "resume", "archive", "restore_to_paused":
		return true
	default:
		return false
	}
}

func targetTransitionFor(currentStatus, action string) (targetTransitionSpec, error) {
	if !validTargetRuntimeAction(action) {
		return targetTransitionSpec{}, fmt.Errorf("%w: %q", ErrInvalidTargetRuntimeAction, action)
	}
	switch action {
	case "maintenance":
		if currentStatus == targets.RunStatusEnabled {
			return targetTransitionSpec{
				runStatus: targets.RunStatusMaintenance,
				eventType: incidents.EventTargetMaintenanceEntered,
				summary:   "目标运行已进入维护",
			}, nil
		}
	case "pause":
		if currentStatus == targets.RunStatusPaused {
			return targetTransitionSpec{runStatus: targets.RunStatusPaused, noChange: true}, nil
		}
		if currentStatus == targets.RunStatusEnabled || currentStatus == targets.RunStatusMaintenance {
			return targetTransitionSpec{
				runStatus: targets.RunStatusPaused,
				eventType: incidents.EventTargetPaused,
				summary:   "目标运行已暂停",
			}, nil
		}
	case "resume":
		if currentStatus == targets.RunStatusMaintenance {
			return targetTransitionSpec{
				runStatus: targets.RunStatusEnabled,
				eventType: incidents.EventTargetMaintenanceExited,
				summary:   "目标运行已退出维护",
			}, nil
		}
		if currentStatus == targets.RunStatusPaused {
			return targetTransitionSpec{
				runStatus: targets.RunStatusEnabled,
				eventType: incidents.EventTargetResumed,
				summary:   "目标运行已恢复",
			}, nil
		}
	case "archive":
		if currentStatus == targets.RunStatusArchived {
			return targetTransitionSpec{runStatus: targets.RunStatusArchived, noChange: true}, nil
		}
		if currentStatus == targets.RunStatusEnabled || currentStatus == targets.RunStatusMaintenance || currentStatus == targets.RunStatusPaused {
			return targetTransitionSpec{
				runStatus: targets.RunStatusArchived,
				eventType: incidents.EventTargetArchived,
				summary:   "目标已归档",
			}, nil
		}
	case "restore_to_paused":
		if currentStatus == targets.RunStatusArchived {
			return targetTransitionSpec{
				runStatus: targets.RunStatusPaused,
				eventType: incidents.EventTargetRestoredToPaused,
				summary:   "目标已恢复到暂停",
			}, nil
		}
	}
	return targetTransitionSpec{}, fmt.Errorf("%w: action %q cannot transition from run status %q", ErrInvalidTargetRuntimeTransition, action, currentStatus)
}

func targetLifecycleDigestState(record targets.TargetRecord) []string {
	state, _ := json.Marshal(struct {
		Name                              string   `json:"name"`
		TargetType                        string   `json:"target_type"`
		Host                              string   `json:"host"`
		BasePort                          *int     `json:"base_port"`
		ExecutionMonitoringInstanceLabels []string `json:"execution_monitoring_instance_labels"`
		RunStatus                         string   `json:"run_status"`
		Group                             string   `json:"group"`
		Labels                            []string `json:"labels"`
		Note                              string   `json:"note"`
	}{
		Name:                              record.Name,
		TargetType:                        record.TargetType,
		Host:                              record.Host,
		BasePort:                          record.BasePort,
		ExecutionMonitoringInstanceLabels: record.ExecutionMonitoringInstanceLabels,
		RunStatus:                         record.RunStatus,
		Group:                             record.Group,
		Labels:                            record.Labels,
		Note:                              record.Note,
	})
	return []string{string(state)}
}

func loadTargetLifecycleReviewTx(ctx context.Context, tx pgx.Tx, targetID string) (targets.TargetRecord, []assetlinks.DependencyImpact, string, error) {
	record, err := scanTarget(tx.QueryRow(ctx, `
		select `+targetSelectColumns+`
		from targets
		where target_id = $1`, targetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return targets.TargetRecord{}, nil, "", targets.ErrTargetNotFound
	}
	if err != nil {
		return targets.TargetRecord{}, nil, "", fmt.Errorf("load target lifecycle review for %q: %w", targetID, err)
	}

	impacts, err := loadAssetDependencyImpacts(ctx, tx, nil, []string{targetID})
	if err != nil {
		return targets.TargetRecord{}, nil, "", err
	}
	digest := digestAssetManagementReview(assetlifecycle.ObjectTypeTarget, targetID, targetLifecycleDigestState(record), impacts)
	return record, impacts, digest, nil
}

func requireTargetLifecycleConfirmation(impacts []assetlinks.DependencyImpact, expectedDigest string, input assetlinks.GlobalActionConfirmation) error {
	if err := requireSharedAssetConfirmation(impacts, expectedDigest, input); err != nil {
		return err
	}
	if strings.TrimSpace(input.PreviewDigest) == "" || !input.ConfirmSharedImpact {
		for _, impact := range impacts {
			if impact.Classification == assetlinks.DependencyNeedsConfirmation {
				return assetlifecycle.ErrSharedImpactConfirmationRequired
			}
		}
	}
	return nil
}

func (r *PostgresTargetRepository) runTargetLifecycleAction(ctx context.Context, targetID, action string, confirmations ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	if len(confirmations) > 1 {
		return targets.TargetRecord{}, fmt.Errorf("%w: at most one confirmation is allowed", ErrInvalidTargetRuntimeAction)
	}
	var confirmation assetlinks.GlobalActionConfirmation
	if len(confirmations) == 1 {
		confirmation = confirmations[0]
	}

	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("begin target %s transaction for %q: %w", action, targetID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, impacts, digest, err := loadTargetLifecycleReviewTx(ctx, tx, targetID)
	if err != nil {
		return targets.TargetRecord{}, err
	}
	spec, err := targetTransitionFor(current.RunStatus, action)
	if err != nil {
		return targets.TargetRecord{}, err
	}
	if spec.noChange {
		if strings.TrimSpace(confirmation.PreviewDigest) != "" {
			confirmation.ConfirmSharedImpact = true
			err = requireSharedAssetConfirmation(impacts, digest, confirmation)
		}
	} else {
		err = requireTargetLifecycleConfirmation(impacts, digest, confirmation)
	}
	if err != nil {
		return targets.TargetRecord{}, err
	}

	record, _, err := transitionTargetTx(ctx, tx, targetID, action)
	if err != nil {
		return targets.TargetRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return targets.TargetRecord{}, fmt.Errorf("commit target %s transaction for %q: %w", action, targetID, err)
	}
	return record, nil
}

func transitionTargetTx(ctx context.Context, tx pgx.Tx, targetID, action string) (targets.TargetRecord, bool, error) {
	if !validTargetRuntimeAction(action) {
		return targets.TargetRecord{}, false, fmt.Errorf("%w: %q", ErrInvalidTargetRuntimeAction, action)
	}

	current, err := scanTarget(tx.QueryRow(ctx, `
		select `+targetSelectColumns+`
		from targets
		where target_id = $1
		for update`, targetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return targets.TargetRecord{}, false, targets.ErrTargetNotFound
	}
	if err != nil {
		return targets.TargetRecord{}, false, fmt.Errorf("lock target %q for %s: %w", targetID, action, err)
	}

	spec, err := targetTransitionFor(current.RunStatus, action)
	if err != nil {
		return targets.TargetRecord{}, false, err
	}
	if spec.noChange {
		return current, false, nil
	}

	record, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set run_status = $2,
			updated_at = now()
		where target_id = $1
			and run_status = $3
		returning `+targetSelectColumns,
		targetID,
		spec.runStatus,
		current.RunStatus,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return targets.TargetRecord{}, false, fmt.Errorf("%w: target %q changed run status during %s", ErrInvalidTargetRuntimeTransition, targetID, action)
	}
	if err != nil {
		return targets.TargetRecord{}, false, fmt.Errorf("update target %q for %s: %w", targetID, action, err)
	}
	if err := insertTargetRuntimeEvent(ctx, tx, record, spec.eventType, spec.summary, current.RunStatus, monitoringEventProvenanceWeb); err != nil {
		return targets.TargetRecord{}, false, err
	}
	return record, true, nil
}

func (r *PostgresTargetRepository) SetTargetMaintenance(ctx context.Context, targetID string, confirmation ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return r.runTargetLifecycleAction(ctx, targetID, "maintenance", confirmation...)
}

func (r *PostgresTargetRepository) PauseTargetRun(ctx context.Context, targetID string, confirmation ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return r.runTargetLifecycleAction(ctx, targetID, "pause", confirmation...)
}

func (r *PostgresTargetRepository) ResumeTargetRun(ctx context.Context, targetID string, confirmation ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return r.runTargetLifecycleAction(ctx, targetID, "resume", confirmation...)
}

func (r *PostgresTargetRepository) ArchiveTarget(ctx context.Context, targetID string, confirmation ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return r.runTargetLifecycleAction(ctx, targetID, "archive", confirmation...)
}

func (r *PostgresTargetRepository) RestoreArchivedTargetToPaused(ctx context.Context, targetID string, confirmation ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return r.runTargetLifecycleAction(ctx, targetID, "restore_to_paused", confirmation...)
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

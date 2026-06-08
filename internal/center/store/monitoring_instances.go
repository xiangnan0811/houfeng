package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
)

var _ monitoringinstances.Repository = (*PostgresMonitoringInstanceRepository)(nil)
var _ enrollment.Repository = (*PostgresMonitoringInstanceRepository)(nil)
var _ monitoringinstances.OnboardingRepository = (*PostgresMonitoringInstanceRepository)(nil)

type monitoringInstanceDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresMonitoringInstanceRepository struct {
	db monitoringInstanceDB
}

func NewPostgresMonitoringInstanceRepository(db *pgxpool.Pool) *PostgresMonitoringInstanceRepository {
	return &PostgresMonitoringInstanceRepository{db: db}
}

const monitoringInstanceSelectColumns = `
	monitoring_instance_id,
	display_name,
	"group",
	region,
	city,
	provider,
	lifecycle_status,
	monitoring_status,
	binding_status,
	coalesce(enrollment_token_hash, ''),
	enrollment_token_issued_at,
	coalesce(sync_token_hash, ''),
	coalesce(binding_fingerprint, ''),
	binding_epoch_started_at,
	coalesce(pending_binding_fingerprint, ''),
	pending_binding_first_seen_at,
	pending_binding_last_seen_at,
	pending_binding_attempt_count,
	labels,
	note,
	current_health_status,
	last_heartbeat_at,
	last_sync_at,
	current_active_incident_count,
	current_primary_issue_summary,
	pending_action_id,
	pending_action_command_id,
	last_action,
	created_at,
	updated_at`

type monitoringInstanceScanner interface {
	Scan(dest ...any) error
}

const (
	monitoringInstanceMonitoringStatusMaintenance = "维护中"
	monitoringInstanceMonitoringStatusPaused      = "暂停"
)

var ErrInvalidMonitoringInstanceRuntimeTransition = errors.New("invalid monitoring instance runtime transition")

var monitoringInstanceSelectColumnNames = []string{
	"monitoring_instance_id",
	"group",
	"display_name",
	"region",
	"city",
	"provider",
	"lifecycle_status",
	"monitoring_status",
	"binding_status",
	"coalesce(enrollment_token_hash, '')",
	"enrollment_token_issued_at",
	"coalesce(sync_token_hash, '')",
	"coalesce(binding_fingerprint, '')",
	"binding_epoch_started_at",
	"coalesce(pending_binding_fingerprint, '')",
	"pending_binding_first_seen_at",
	"pending_binding_last_seen_at",
	"pending_binding_attempt_count",
	"labels",
	"note",
	"current_health_status",
	"last_heartbeat_at",
	"last_sync_at",
	"current_active_incident_count",
	"current_primary_issue_summary",
	"pending_action_id",
	"pending_action_command_id",
	"last_action",
	"created_at",
	"updated_at",
}

func scanMonitoringInstance(row monitoringInstanceScanner) (monitoringinstances.Record, error) {
	var record monitoringinstances.Record
	var pendingActionID, pendingActionCommandID *string
	if err := row.Scan(
		&record.MonitoringInstanceID,
		&record.DisplayName,
		&record.Group,
		&record.Region,
		&record.City,
		&record.Provider,
		&record.LifecycleStatus,
		&record.MonitoringStatus,
		&record.BindingStatus,
		&record.EnrollmentTokenHash,
		&record.EnrollmentTokenIssuedAt,
		&record.SyncTokenHash,
		&record.BindingFingerprint,
		&record.BindingEpochStartedAt,
		&record.PendingBindingFingerprint,
		&record.PendingBindingFirstSeenAt,
		&record.PendingBindingLastSeenAt,
		&record.PendingBindingAttemptCount,
		&record.Labels,
		&record.Note,
		&record.CurrentHealthStatus,
		&record.LastHeartbeatAt,
		&record.LastSyncAt,
		&record.CurrentActiveIncidentCount,
		&record.CurrentPrimaryIssueSummary,
		&pendingActionID,
		&pendingActionCommandID,
		&record.LastActionRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return monitoringinstances.Record{}, err
	}
	record.LastAction = lastActionFromRaw(record.LastActionRaw)
	return record, nil
}

func lastActionFromRaw(raw json.RawMessage) *monitoringinstances.LastAction {
	if len(raw) == 0 {
		return nil
	}
	var action monitoringinstances.LastAction
	if err := json.Unmarshal(raw, &action); err != nil {
		return nil
	}
	return &action
}

func qualifiedMonitoringInstanceSelectColumns(alias string) string {
	parts := make([]string, 0, len(monitoringInstanceSelectColumnNames))
	for _, column := range monitoringInstanceSelectColumnNames {
		switch column {
		case "coalesce(enrollment_token_hash, '')":
			parts = append(parts, "coalesce("+alias+".enrollment_token_hash, '')")
		case "coalesce(sync_token_hash, '')":
			parts = append(parts, "coalesce("+alias+".sync_token_hash, '')")
		case "coalesce(binding_fingerprint, '')":
			parts = append(parts, "coalesce("+alias+".binding_fingerprint, '')")
		case "coalesce(pending_binding_fingerprint, '')":
			parts = append(parts, "coalesce("+alias+".pending_binding_fingerprint, '')")
		default:
			parts = append(parts, alias+"."+column)
		}
	}
	return strings.Join(parts, ",\n\t\t")
}

func scanMonitoringInstanceWithPreviousMonitoringStatus(row monitoringInstanceScanner) (monitoringinstances.Record, string, error) {
	var (
		record                 monitoringinstances.Record
		priorState             string
		pendingActionID        *string
		pendingActionCommandID *string
	)
	if err := row.Scan(
		&record.MonitoringInstanceID,
		&record.DisplayName,
		&record.Group,
		&record.Region,
		&record.City,
		&record.Provider,
		&record.LifecycleStatus,
		&record.MonitoringStatus,
		&record.BindingStatus,
		&record.EnrollmentTokenHash,
		&record.EnrollmentTokenIssuedAt,
		&record.SyncTokenHash,
		&record.BindingFingerprint,
		&record.BindingEpochStartedAt,
		&record.PendingBindingFingerprint,
		&record.PendingBindingFirstSeenAt,
		&record.PendingBindingLastSeenAt,
		&record.PendingBindingAttemptCount,
		&record.Labels,
		&record.Note,
		&record.CurrentHealthStatus,
		&record.LastHeartbeatAt,
		&record.LastSyncAt,
		&record.CurrentActiveIncidentCount,
		&record.CurrentPrimaryIssueSummary,
		&pendingActionID,
		&pendingActionCommandID,
		&record.LastActionRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
		&priorState,
	); err != nil {
		return monitoringinstances.Record{}, "", err
	}
	record.LastAction = lastActionFromRaw(record.LastActionRaw)
	return record, priorState, nil
}

func scanMonitoringInstanceOnboarding(row monitoringInstanceScanner) (monitoringinstances.OnboardingState, error) {
	var (
		record                 monitoringinstances.Record
		hasHostSample          bool
		hasAcceptedObservation bool
		pendingActionID        *string
		pendingActionCommandID *string
	)
	if err := row.Scan(
		&record.MonitoringInstanceID,
		&record.DisplayName,
		&record.Group,
		&record.Region,
		&record.City,
		&record.Provider,
		&record.LifecycleStatus,
		&record.MonitoringStatus,
		&record.BindingStatus,
		&record.EnrollmentTokenHash,
		&record.EnrollmentTokenIssuedAt,
		&record.SyncTokenHash,
		&record.BindingFingerprint,
		&record.BindingEpochStartedAt,
		&record.PendingBindingFingerprint,
		&record.PendingBindingFirstSeenAt,
		&record.PendingBindingLastSeenAt,
		&record.PendingBindingAttemptCount,
		&record.Labels,
		&record.Note,
		&record.CurrentHealthStatus,
		&record.LastHeartbeatAt,
		&record.LastSyncAt,
		&record.CurrentActiveIncidentCount,
		&record.CurrentPrimaryIssueSummary,
		&pendingActionID,
		&pendingActionCommandID,
		&record.LastActionRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
		&hasHostSample,
		&hasAcceptedObservation,
	); err != nil {
		return monitoringinstances.OnboardingState{}, err
	}
	record.LastAction = lastActionFromRaw(record.LastActionRaw)

	state := monitoringinstances.OnboardingState{
		Record:                           record,
		Phase:                            monitoringinstances.DeriveOnboardingPhase(record, hasHostSample, hasAcceptedObservation),
		HasHostSample:                    hasHostSample,
		HasAcceptedObservation:           hasAcceptedObservation,
		EnrollmentTokenIssuedAt:          record.EnrollmentTokenIssuedAt,
		CurrentBindingFingerprintSummary: monitoringinstances.MaskFingerprintSummary(record.BindingFingerprint),
	}
	if record.PendingBindingFingerprint != "" || record.PendingBindingFirstSeenAt != nil || record.PendingBindingLastSeenAt != nil || record.PendingBindingAttemptCount > 0 {
		state.PendingBinding = &monitoringinstances.PendingBindingMetadata{
			Fingerprint:  record.PendingBindingFingerprint,
			FirstSeenAt:  record.PendingBindingFirstSeenAt,
			LastSeenAt:   record.PendingBindingLastSeenAt,
			AttemptCount: record.PendingBindingAttemptCount,
		}
	}
	return state, nil
}

func hashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func hashEnrollmentToken(token string) string {
	return hashOpaqueToken(token)
}

func hashSyncToken(token string) string {
	return hashOpaqueToken(token)
}

func (r *PostgresMonitoringInstanceRepository) ListMonitoringInstances(ctx context.Context) ([]monitoringinstances.Record, error) {
	rows, err := r.db.Query(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where not exists (
			select 1
			from vps_monitoring_instance_links l
			where l.monitoring_instance_id = monitoring_instances.monitoring_instance_id
			  and l.unlinked_at is null
		)
		or exists (
			select 1
			from vps_monitoring_instance_links l
			join vps_assets v on v.vps_id = l.vps_id
			where l.monitoring_instance_id = monitoring_instances.monitoring_instance_id
			  and l.unlinked_at is null
			  and v.lifecycle_status not in ('cancelled', 'archived')
		)
		order by created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("query monitoring instances: %w", err)
	}
	defer rows.Close()

	records := make([]monitoringinstances.Record, 0)
	for rows.Next() {
		record, err := scanMonitoringInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monitoring instance: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring instances: %w", err)
	}

	return records, nil
}

func (r *PostgresMonitoringInstanceRepository) GetMonitoringInstance(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	record, err := scanMonitoringInstance(r.db.QueryRow(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where monitoring_instance_id = $1`, monitoringInstanceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("query monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) UpdateMonitoringInstanceMetadata(ctx context.Context, monitoringInstanceID string, input monitoringinstances.UpdateMetadataInput) (monitoringinstances.Record, error) {
	args := []any{monitoringInstanceID}
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

	record, err := scanMonitoringInstance(r.db.QueryRow(ctx, `
		update monitoring_instances
		set "group" = coalesce($2, "group"),
		    labels = $3,
		    note = $4,
		    updated_at = now()
		where monitoring_instance_id = $1`+precondition+`
		returning `+monitoringInstanceSelectColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if input.ExpectedUpdatedAt != nil {
			exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
			if existsErr != nil {
				return monitoringinstances.Record{}, fmt.Errorf("check monitoring instance metadata conflict %q: %w", monitoringInstanceID, existsErr)
			}
			if exists {
				return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceMetadataConflict
			}
		}
		return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("update monitoring instance metadata %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) CreateMonitoringInstance(ctx context.Context, input monitoringinstances.CreateInput) (monitoringinstances.Record, error) {
	monitoringInstanceID, err := ids.New("mi")
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("generate monitoring instance id: %w", err)
	}

	record, err := scanMonitoringInstance(r.db.QueryRow(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id,
			display_name,
			"group",
			region,
			city,
			provider,
			lifecycle_status,
			monitoring_status,
			binding_status,
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
			$12,
			0,
			''
		)
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
		input.DisplayName,
		input.Group,
		input.Region,
		input.City,
		input.Provider,
		input.LifecycleStatus,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingUnbound,
		input.Labels,
		input.Note,
		monitoringinstances.HealthNormal,
	))
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("create monitoring instance: %w", err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) CreateLinkedMonitoringInstance(ctx context.Context, vpsID string, input monitoringinstances.CreateInput, linkNote string) (monitoringinstances.Record, assetlinks.Record, error) {
	if strings.TrimSpace(vpsID) == "" {
		return monitoringinstances.Record{}, assetlinks.Record{}, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput
	}

	monitoringInstanceID, err := ids.New("mi")
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("generate monitoring instance id: %w", err)
	}
	linkID, err := ids.New("vnl")
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("generate vps monitoring instance link id: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("begin linked monitoring instance transaction for vps %q: %w", vpsID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id,
			display_name,
			"group",
			region,
			city,
			provider,
			lifecycle_status,
			monitoring_status,
			binding_status,
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
			$12,
			0,
			''
		)
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
		input.DisplayName,
		input.Group,
		input.Region,
		input.City,
		input.Provider,
		input.LifecycleStatus,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingUnbound,
		input.Labels,
		input.Note,
		monitoringinstances.HealthNormal,
	))
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("create monitoring instance for vps %q: %w", vpsID, err)
	}

	link, err := scanVPSMonitoringInstanceLink(tx.QueryRow(ctx, `
		insert into vps_monitoring_instance_links (
			link_id,
			vps_id,
			monitoring_instance_id,
			note
		) values (
			$1,
			$2,
			$3,
			$4
		)
		returning `+vpsMonitoringInstanceLinkSelectColumns,
		linkID,
		vpsID,
		record.MonitoringInstanceID,
		strings.TrimSpace(linkNote),
	))
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, mapVPSMonitoringInstanceLinkWriteError(err, "create linked monitoring instance for vps %q", vpsID)
	}

	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("commit linked monitoring instance transaction for vps %q: %w", vpsID, err)
	}
	return record, link, nil
}

func (r *PostgresMonitoringInstanceRepository) IssueEnrollmentToken(ctx context.Context, monitoringInstanceID string) (string, error) {
	issue, err := r.IssueMonitoringInstanceEnrollmentToken(ctx, monitoringInstanceID)
	if err != nil {
		return "", err
	}
	return issue.Token, nil
}

func (r *PostgresMonitoringInstanceRepository) IssueMonitoringInstanceEnrollmentToken(ctx context.Context, monitoringInstanceID string) (monitoringinstances.EnrollmentTokenIssue, error) {
	token, err := ids.New("enroll")
	if err != nil {
		return monitoringinstances.EnrollmentTokenIssue{}, fmt.Errorf("generate enrollment token: %w", err)
	}

	var issuedAt time.Time
	if err := r.db.QueryRow(ctx, `
		update monitoring_instances
		set enrollment_token_hash = $2,
			enrollment_token_issued_at = now(),
			enrollment_token_consumed_at = null,
			updated_at = now()
		where monitoring_instance_id = $1
		returning enrollment_token_issued_at`,
		monitoringInstanceID,
		hashEnrollmentToken(token),
	).Scan(&issuedAt); errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.EnrollmentTokenIssue{}, monitoringinstances.ErrMonitoringInstanceNotFound
	} else if err != nil {
		return monitoringinstances.EnrollmentTokenIssue{}, fmt.Errorf("issue enrollment token for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	return monitoringinstances.EnrollmentTokenIssue{
		Token:     token,
		IssuedAt:  issuedAt,
		ExpiresAt: issuedAt.Add(monitoringinstances.EnrollmentTokenTTL),
	}, nil
}

func (r *PostgresMonitoringInstanceRepository) GetMonitoringInstanceOnboarding(ctx context.Context, monitoringInstanceID string) (monitoringinstances.OnboardingState, error) {
	state, err := scanMonitoringInstanceOnboarding(r.db.QueryRow(ctx, `
		select `+qualifiedMonitoringInstanceSelectColumns("mi")+`,
			exists (
				select 1
				from host_samples hs
				where hs.monitoring_instance_id = mi.monitoring_instance_id
					and mi.binding_fingerprint <> ''
					and mi.binding_epoch_started_at is not null
					and hs.fingerprint = mi.binding_fingerprint
					and hs.received_at >= mi.binding_epoch_started_at
			),
			exists (
				select 1
				from probe_observations po
				where po.monitoring_instance_id = mi.monitoring_instance_id
					and mi.binding_fingerprint <> ''
					and mi.binding_epoch_started_at is not null
					and po.fingerprint = mi.binding_fingerprint
					and po.received_at >= mi.binding_epoch_started_at
			)
		from monitoring_instances mi
		where mi.monitoring_instance_id = $1`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.OnboardingState{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.OnboardingState{}, fmt.Errorf("query onboarding state for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return state, nil
}

func (r *PostgresMonitoringInstanceRepository) monitoringInstanceExists(ctx context.Context, monitoringInstanceID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from monitoring_instances
			where monitoring_instance_id = $1
		)`,
		monitoringInstanceID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check monitoring instance %q existence: %w", monitoringInstanceID, err)
	}
	return exists, nil
}

func insertMonitoringInstanceBindingEvent(
	ctx context.Context,
	tx pgx.Tx,
	record monitoringinstances.Record,
	eventType incidents.EventType,
	summary string,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate binding event id: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"binding_status": record.BindingStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal binding event payload: %w", err)
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
		string(incidents.ObjectTypeMonitoringInstance),
		record.MonitoringInstanceID,
		string(eventType),
		"",
		summary,
		payload,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert binding event for monitoring instance %q: %w", record.MonitoringInstanceID, err)
	}

	return nil
}

func (r *PostgresMonitoringInstanceRepository) ConfirmMonitoringInstanceRebind(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin confirm monitoring instance rebind transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set binding_status = '已绑定',
			binding_fingerprint = pending_binding_fingerprint,
			binding_epoch_started_at = now(),
			pending_binding_fingerprint = null,
			pending_binding_first_seen_at = null,
			pending_binding_last_seen_at = null,
			pending_binding_attempt_count = 0,
			sync_token_hash = '',
			last_heartbeat_at = null,
			last_sync_at = null,
			updated_at = now()
		where monitoring_instance_id = $1
			and binding_status = '指纹变更待确认'
			and coalesce(pending_binding_fingerprint, '') <> ''
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
		if existsErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("confirm monitoring instance rebind for %q: %w", monitoringInstanceID, existsErr)
		}
		if !exists {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: confirm rebind requires pending fingerprint for monitoring instance %q", monitoringinstances.ErrInvalidBindingTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("confirm monitoring instance rebind for %q: %w", monitoringInstanceID, err)
	}

	if err := insertMonitoringInstanceBindingEvent(
		ctx,
		tx,
		record,
		incidents.EventMonitoringInstanceBindingRebindConfirmed,
		"监控实例已确认新的绑定指纹并等待重新建立稳定观测",
	); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit confirm monitoring instance rebind for %q: %w", monitoringInstanceID, err)
	}

	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) RejectPendingFingerprint(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin reject pending fingerprint transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set binding_status = '已绑定',
			pending_binding_fingerprint = null,
			pending_binding_first_seen_at = null,
			pending_binding_last_seen_at = null,
			pending_binding_attempt_count = 0,
			updated_at = now()
		where monitoring_instance_id = $1
			and binding_status = '指纹变更待确认'
			and coalesce(pending_binding_fingerprint, '') <> ''
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
		if existsErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("reject pending fingerprint for monitoring instance %q: %w", monitoringInstanceID, existsErr)
		}
		if !exists {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: reject pending fingerprint requires pending fingerprint for monitoring instance %q", monitoringinstances.ErrInvalidBindingTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("reject pending fingerprint for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	if err := insertMonitoringInstanceBindingEvent(
		ctx,
		tx,
		record,
		incidents.EventMonitoringInstanceBindingPendingRejected,
		"监控实例已拒绝待确认指纹并保留当前绑定",
	); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit reject pending fingerprint for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) ResetMonitoringInstanceBinding(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin reset monitoring instance binding transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set binding_status = '未绑定',
			binding_fingerprint = '',
			binding_epoch_started_at = null,
			pending_binding_fingerprint = null,
			pending_binding_first_seen_at = null,
			pending_binding_last_seen_at = null,
			pending_binding_attempt_count = 0,
			sync_token_hash = '',
			last_heartbeat_at = null,
			last_sync_at = null,
			updated_at = now()
		where monitoring_instance_id = $1
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("reset monitoring instance binding for %q: %w", monitoringInstanceID, err)
	}

	if err := insertMonitoringInstanceBindingEvent(
		ctx,
		tx,
		record,
		incidents.EventMonitoringInstanceBindingReset,
		"监控实例已重置绑定并等待重新接入",
	); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit reset monitoring instance binding for %q: %w", monitoringInstanceID, err)
	}

	return record, nil
}

func insertMonitoringInstanceLifecycleEvent(
	ctx context.Context,
	tx pgx.Tx,
	record monitoringinstances.Record,
	eventType incidents.EventType,
	summary string,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate monitoring instance lifecycle event id: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"lifecycle_status": record.LifecycleStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal monitoring instance lifecycle event payload: %w", err)
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
		string(incidents.ObjectTypeMonitoringInstance),
		record.MonitoringInstanceID,
		string(eventType),
		"",
		summary,
		payload,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert lifecycle event for monitoring instance %q: %w", record.MonitoringInstanceID, err)
	}

	return nil
}

func insertMonitoringInstanceRuntimeEvent(
	ctx context.Context,
	tx pgx.Tx,
	record monitoringinstances.Record,
	eventType incidents.EventType,
	summary string,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate monitoring instance runtime event id: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"monitoring_status": record.MonitoringStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal monitoring instance runtime event payload: %w", err)
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
		string(incidents.ObjectTypeMonitoringInstance),
		record.MonitoringInstanceID,
		string(eventType),
		"",
		summary,
		payload,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert runtime event for monitoring instance %q: %w", record.MonitoringInstanceID, err)
	}

	return nil
}

func (r *PostgresMonitoringInstanceRepository) SetMonitoringInstanceMonitoringMaintenance(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin set monitoring instance maintenance transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set monitoring_status = '维护中',
			updated_at = now()
		where monitoring_instance_id = $1
			and monitoring_status = '启用'
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
		if existsErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("set monitoring instance maintenance for %q: %w", monitoringInstanceID, existsErr)
		}
		if !exists {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q cannot enter maintenance from current monitoring status", ErrInvalidMonitoringInstanceRuntimeTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("set monitoring instance maintenance for %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, record, incidents.EventMonitoringInstanceMonitoringMaintenanceEntered, "监控实例已进入维护"); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit set monitoring instance maintenance for %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) PauseMonitoringInstanceMonitoring(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin pause monitoring instance monitoring transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set monitoring_status = '暂停',
			updated_at = now()
		where monitoring_instance_id = $1
			and monitoring_status in ('启用', '维护中')
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
		if existsErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("pause monitoring instance monitoring for %q: %w", monitoringInstanceID, existsErr)
		}
		if !exists {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q cannot pause monitoring from current monitoring status", ErrInvalidMonitoringInstanceRuntimeTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("pause monitoring instance monitoring for %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, record, incidents.EventMonitoringInstanceMonitoringPaused, "监控实例监控已暂停"); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit pause monitoring instance monitoring for %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) ResumeMonitoringInstanceMonitoring(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin resume monitoring instance monitoring transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, previousStatus, err := scanMonitoringInstanceWithPreviousMonitoringStatus(tx.QueryRow(ctx, `
		with prior as (
			select monitoring_status
			from monitoring_instances
			where monitoring_instance_id = $1
		),
		updated as (
			update monitoring_instances
			set monitoring_status = '启用',
				updated_at = now()
			where monitoring_instance_id = $1
				and monitoring_status in ('维护中', '暂停')
			returning `+monitoringInstanceSelectColumns+`
		)
		select `+qualifiedMonitoringInstanceSelectColumns("updated")+`, prior.monitoring_status
		from updated
		join prior on true`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
		if existsErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("resume monitoring instance monitoring for %q: %w", monitoringInstanceID, existsErr)
		}
		if !exists {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q cannot resume monitoring from current monitoring status", ErrInvalidMonitoringInstanceRuntimeTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("resume monitoring instance monitoring for %q: %w", monitoringInstanceID, err)
	}

	eventType := incidents.EventMonitoringInstanceMonitoringResumed
	summary := "监控实例监控已恢复"
	if previousStatus == monitoringInstanceMonitoringStatusMaintenance {
		eventType = incidents.EventMonitoringInstanceMonitoringMaintenanceExited
		summary = "监控实例已退出维护"
	}
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, record, eventType, summary); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit resume monitoring instance monitoring for %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) IssueSyncToken(ctx context.Context, monitoringInstanceID string) (string, error) {
	token, err := ids.New("sync")
	if err != nil {
		return "", fmt.Errorf("generate sync token: %w", err)
	}

	tag, err := r.db.Exec(ctx, `
		update monitoring_instances
		set sync_token_hash = $2,
			updated_at = now()
		where monitoring_instance_id = $1`,
		monitoringInstanceID,
		hashSyncToken(token),
	)
	if err != nil {
		return "", fmt.Errorf("issue sync token for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", monitoringinstances.ErrMonitoringInstanceNotFound
	}

	return token, nil
}

func (r *PostgresMonitoringInstanceRepository) FindMonitoringInstanceByEnrollmentToken(ctx context.Context, token string) (monitoringinstances.Record, error) {
	record, err := scanMonitoringInstance(r.db.QueryRow(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where enrollment_token_hash = $1
			and enrollment_token_consumed_at is null
			and enrollment_token_issued_at >= now() - interval '30 minutes'`,
		hashEnrollmentToken(token),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("query monitoring instance by enrollment token: %w", err)
	}
	return record, nil
}

func clearPendingBinding(record *monitoringinstances.Record) {
	record.PendingBindingFingerprint = ""
	record.PendingBindingFirstSeenAt = nil
	record.PendingBindingLastSeenAt = nil
	record.PendingBindingAttemptCount = 0
}

func resolveEnrollmentBindingTransition(record monitoringinstances.Record, newFingerprint string, now time.Time) monitoringinstances.Record {
	next := record
	switch record.BindingStatus {
	case monitoringinstances.BindingUnbound:
		next.BindingStatus = monitoringinstances.BindingBound
		next.BindingFingerprint = newFingerprint
		next.BindingEpochStartedAt = &now
		clearPendingBinding(&next)
		return next
	case monitoringinstances.BindingBound:
		if record.BindingFingerprint == newFingerprint {
			next.BindingStatus = monitoringinstances.BindingBound
			next.BindingFingerprint = record.BindingFingerprint
			clearPendingBinding(&next)
			return next
		}
		next.BindingStatus = monitoringinstances.BindingPendingConfirmation
		next.BindingFingerprint = record.BindingFingerprint
		next.PendingBindingFingerprint = newFingerprint
		next.PendingBindingFirstSeenAt = &now
		next.PendingBindingLastSeenAt = &now
		next.PendingBindingAttemptCount = 1
		return next
	default:
		if record.PendingBindingFingerprint == newFingerprint {
			next.PendingBindingFingerprint = newFingerprint
			if record.PendingBindingFirstSeenAt == nil {
				next.PendingBindingFirstSeenAt = &now
			}
			next.PendingBindingLastSeenAt = &now
			next.PendingBindingAttemptCount = record.PendingBindingAttemptCount + 1
			if next.PendingBindingAttemptCount == 0 {
				next.PendingBindingAttemptCount = 1
			}
			return next
		}
		if record.BindingFingerprint == newFingerprint {
			return next
		}
		next.BindingStatus = monitoringinstances.BindingPendingConfirmation
		next.PendingBindingFingerprint = newFingerprint
		next.PendingBindingFirstSeenAt = &now
		next.PendingBindingLastSeenAt = &now
		next.PendingBindingAttemptCount = 1
		return next
	}
}

func (r *PostgresMonitoringInstanceRepository) ApplyEnrollment(ctx context.Context, input enrollment.EnrollInput) (monitoringinstances.Record, string, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, "", fmt.Errorf("begin enrollment transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		monitoringInstanceID       string
		bindingStatus              string
		bindingFingerprint         string
		bindingEpochStartedAt      *time.Time
		pendingBindingFingerprint  string
		pendingBindingFirstSeenAt  *time.Time
		pendingBindingLastSeenAt   *time.Time
		pendingBindingAttemptCount int
	)
	if err := tx.QueryRow(ctx, `
		select monitoring_instance_id,
			binding_status,
			coalesce(binding_fingerprint, ''),
			binding_epoch_started_at,
			coalesce(pending_binding_fingerprint, ''),
			pending_binding_first_seen_at,
			pending_binding_last_seen_at,
			pending_binding_attempt_count
		from monitoring_instances
		where enrollment_token_hash = $1
			and enrollment_token_consumed_at is null
			and enrollment_token_issued_at >= now() - interval '30 minutes'
		for update`,
		hashEnrollmentToken(input.Token),
	).Scan(
		&monitoringInstanceID,
		&bindingStatus,
		&bindingFingerprint,
		&bindingEpochStartedAt,
		&pendingBindingFingerprint,
		&pendingBindingFirstSeenAt,
		&pendingBindingLastSeenAt,
		&pendingBindingAttemptCount,
	); errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, "", monitoringinstances.ErrMonitoringInstanceNotFound
	} else if err != nil {
		return monitoringinstances.Record{}, "", fmt.Errorf("query monitoring instance by enrollment token for update: %w", err)
	}

	next := resolveEnrollmentBindingTransition(monitoringinstances.Record{
		BindingStatus:              bindingStatus,
		BindingFingerprint:         bindingFingerprint,
		BindingEpochStartedAt:      bindingEpochStartedAt,
		PendingBindingFingerprint:  pendingBindingFingerprint,
		PendingBindingFirstSeenAt:  pendingBindingFirstSeenAt,
		PendingBindingLastSeenAt:   pendingBindingLastSeenAt,
		PendingBindingAttemptCount: pendingBindingAttemptCount,
	}, input.Fingerprint, time.Now().UTC())

	syncToken := ""
	syncTokenHash := ""
	if next.BindingStatus == monitoringinstances.BindingBound {
		syncToken, err = ids.New("sync")
		if err != nil {
			return monitoringinstances.Record{}, "", fmt.Errorf("generate sync token: %w", err)
		}
		syncTokenHash = hashSyncToken(syncToken)
	}

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set binding_status = $2,
			binding_fingerprint = $3,
			binding_epoch_started_at = $4,
			pending_binding_fingerprint = $5,
			pending_binding_first_seen_at = $6,
			pending_binding_last_seen_at = $7,
			pending_binding_attempt_count = $8,
			sync_token_hash = case when $9 <> '' then $9 else sync_token_hash end,
			enrollment_token_consumed_at = now(),
			updated_at = now()
		where monitoring_instance_id = $1
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
		next.BindingStatus,
		next.BindingFingerprint,
		next.BindingEpochStartedAt,
		nullableText(next.PendingBindingFingerprint),
		next.PendingBindingFirstSeenAt,
		next.PendingBindingLastSeenAt,
		next.PendingBindingAttemptCount,
		syncTokenHash,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, "", monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, "", fmt.Errorf("update enrollment binding state for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, "", fmt.Errorf("commit enrollment transaction for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return record, syncToken, nil
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (r *PostgresMonitoringInstanceRepository) RecordAcceptedHeartbeats(ctx context.Context, syncToken string, writes []enrollment.HeartbeatWrite) error {
	if len(writes) == 0 {
		return nil
	}

	monitoringInstanceID := writes[0].MonitoringInstanceID
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin heartbeat transaction for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		bindingStatus       string
		bindingFingerprint  string
		storedSyncTokenHash string
	)
	if err := tx.QueryRow(ctx, `
		select binding_status,
			coalesce(binding_fingerprint, ''),
			coalesce(sync_token_hash, '')
		from monitoring_instances
		where monitoring_instance_id = $1
		for update`,
		monitoringInstanceID,
	).Scan(&bindingStatus, &bindingFingerprint, &storedSyncTokenHash); errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.ErrMonitoringInstanceNotFound
	} else if err != nil {
		return fmt.Errorf("query heartbeat state for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if bindingStatus != monitoringinstances.BindingBound {
		return enrollment.ErrBindingNotAccepted
	}
	if storedSyncTokenHash == "" || storedSyncTokenHash != hashSyncToken(syncToken) {
		return enrollment.ErrInvalidSyncToken
	}

	lastHeartbeatAt := writes[0].ObservedAt
	lastSyncAt := writes[0].ReceivedAt
	for _, write := range writes {
		if write.Fingerprint != bindingFingerprint {
			return enrollment.ErrBindingNotAccepted
		}
		if _, err := tx.Exec(ctx, `
			insert into monitoring_instance_heartbeats (
				monitoring_instance_id,
				observed_at,
				received_at,
				agent_version,
				fingerprint,
				sync_batch_id,
				is_backfilled
			) values (
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				$7
			)`,
			write.MonitoringInstanceID,
			write.ObservedAt,
			write.ReceivedAt,
			write.AgentVersion,
			write.Fingerprint,
			write.SyncBatchID,
			write.IsBackfilled,
		); err != nil {
			return fmt.Errorf("record heartbeat for monitoring instance %q: %w", write.MonitoringInstanceID, err)
		}

		if write.ObservedAt.After(lastHeartbeatAt) {
			lastHeartbeatAt = write.ObservedAt
		}
		if write.ReceivedAt.After(lastSyncAt) {
			lastSyncAt = write.ReceivedAt
		}
	}

	tag, err := tx.Exec(ctx, `
		update monitoring_instances
		set last_heartbeat_at = greatest(coalesce(last_heartbeat_at, $2), $2),
			last_sync_at = greatest(coalesce(last_sync_at, $3), $3),
			updated_at = now()
		where monitoring_instance_id = $1`,
		monitoringInstanceID,
		lastHeartbeatAt,
		lastSyncAt,
	)
	if err != nil {
		return fmt.Errorf("touch heartbeat state for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		return monitoringinstances.ErrMonitoringInstanceNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit heartbeat transaction for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return nil
}

// SetPendingAction queues a command for the agent to execute on its next sync
// and stores a durable pending last_action for UI/API readers.
func (r *PostgresMonitoringInstanceRepository) SetPendingAction(ctx context.Context, monitoringInstanceID, actionID, commandID string) error {
	raw, err := marshalPendingLastAction(actionID, commandID)
	if err != nil {
		return fmt.Errorf("marshal pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	tag, err := r.db.Exec(ctx,
		`UPDATE monitoring_instances SET pending_action_id = $1, pending_action_command_id = $2, last_action = $3, updated_at = now() WHERE monitoring_instance_id = $4`,
		actionID, commandID, raw, monitoringInstanceID)
	if err != nil {
		return fmt.Errorf("set pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		return monitoringinstances.ErrMonitoringInstanceNotFound
	}
	return nil
}

// GetPendingAction returns the queued pending action for a monitoring instance, or empty
// strings if none is queued.
func (r *PostgresMonitoringInstanceRepository) GetPendingAction(ctx context.Context, monitoringInstanceID string) (actionID, commandID string, err error) {
	err = r.db.QueryRow(ctx,
		`SELECT pending_action_id, pending_action_command_id FROM monitoring_instances WHERE monitoring_instance_id = $1 AND pending_action_id IS NOT NULL`,
		monitoringInstanceID).Scan(&actionID, &commandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return actionID, commandID, nil
}

// ClearPendingAction removes the queued pending action for a monitoring instance without
// storing a result. Used when the action has been dispatched to the agent.
func (r *PostgresMonitoringInstanceRepository) ClearPendingAction(ctx context.Context, monitoringInstanceID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE monitoring_instances SET pending_action_id = NULL, pending_action_command_id = NULL WHERE monitoring_instance_id = $1`,
		monitoringInstanceID)
	if err != nil {
		return fmt.Errorf("clear pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return nil
}

// StoreActionResult writes the command execution result into the monitoring instance record.
func (r *PostgresMonitoringInstanceRepository) StoreActionResult(ctx context.Context, monitoringInstanceID string, raw []byte) error {
	_, err := r.db.Exec(ctx,
		`UPDATE monitoring_instances SET last_action = $1, updated_at = now() WHERE monitoring_instance_id = $2`,
		raw, monitoringInstanceID)
	if err != nil {
		return fmt.Errorf("store action result for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return nil
}

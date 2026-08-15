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

type monitoringInstanceQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresMonitoringInstanceRepository struct {
	db          monitoringInstanceDB
	tokenHasher agentTokenHasher
}

func NewPostgresMonitoringInstanceRepository(db *pgxpool.Pool) *PostgresMonitoringInstanceRepository {
	return NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey(db, nil)
}

func NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey(db *pgxpool.Pool, hmacKey []byte) *PostgresMonitoringInstanceRepository {
	return &PostgresMonitoringInstanceRepository{db: db, tokenHasher: newAgentTokenHasher(hmacKey)}
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
	archived_at,
	archived_reason,
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
	"display_name",
	"group",
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
	"archived_at",
	"archived_reason",
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
		&record.ArchivedAt,
		&record.ArchivedReason,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return monitoringinstances.Record{}, err
	}
	record.LastAction = lastActionFromRaw(record.LastActionRaw)
	return record, nil
}

func lastActionFromRaw(raw json.RawMessage) *monitoringinstances.LastAction {
	return lastActionFromRawAt(raw, time.Now().UTC())
}

func lastActionFromRawAt(raw json.RawMessage, now time.Time) *monitoringinstances.LastAction {
	if len(raw) == 0 {
		return nil
	}
	var action monitoringinstances.LastAction
	if err := json.Unmarshal(raw, &action); err != nil {
		return nil
	}
	if action.OutputExpiresAt != nil && !now.Before(action.OutputExpiresAt.UTC()) {
		action.Stdout = ""
		action.Stderr = ""
		action.OutputExpired = true
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

func scanMonitoringInstanceWithPreviousState(row monitoringInstanceScanner) (monitoringinstances.Record, string, error) {
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
		&record.ArchivedAt,
		&record.ArchivedReason,
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
		&record.ArchivedAt,
		&record.ArchivedReason,
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
	return defaultAgentTokenHasher().hashEnrollmentToken(token)
}

func hashSyncToken(token string) string {
	return defaultAgentTokenHasher().hashSyncToken(token)
}

func syncTokenHashesEqual(storedHash, candidateHash string) bool {
	return constantTimeStringEqual(storedHash, candidateHash)
}

func (r *PostgresMonitoringInstanceRepository) ListMonitoringInstances(ctx context.Context, scopes ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error) {
	scope := monitoringinstances.ListScopeActive
	if len(scopes) > 0 {
		normalized, ok := monitoringinstances.NormalizeListScope(scopes[0])
		if !ok {
			return nil, monitoringinstances.ErrInvalidManagementInput
		}
		scope = normalized
	}
	archiveClause := "and archived_at is null"
	switch scope {
	case monitoringinstances.ListScopeArchived:
		archiveClause = "and archived_at is not null"
	case monitoringinstances.ListScopeAll:
		archiveClause = ""
	}

	rows, err := r.db.Query(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where 1 = 1
		`+archiveClause+`
		and (
		not exists (
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

func (r *PostgresMonitoringInstanceRepository) loadMonitoringRecordSubject(
	ctx context.Context,
	monitoringInstanceID string,
) (monitoringRecordSubject, error) {
	var subject monitoringRecordSubject
	err := r.db.QueryRow(ctx, `
		select mi.monitoring_instance_id,
		       mi.display_name,
		       coalesce((
		         select heartbeat.agent_version
		         from monitoring_instance_heartbeats heartbeat
		         where heartbeat.monitoring_instance_id = mi.monitoring_instance_id
		         order by heartbeat.observed_at desc, heartbeat.id desc
		         limit 1
		       ), '')
		from monitoring_instances mi
		where mi.monitoring_instance_id = $1`, monitoringInstanceID).Scan(
		&subject.MonitoringInstanceID,
		&subject.DisplayName,
		&subject.AgentVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringRecordSubject{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringRecordSubject{}, fmt.Errorf("query monitoring-instance record subject: %w", err)
	}
	return subject, nil
}

func (r *PostgresMonitoringInstanceRepository) GetMonitoringInstanceManagementReview(ctx context.Context, monitoringInstanceID string) (monitoringinstances.ManagementReview, error) {
	record, err := r.GetMonitoringInstance(ctx, monitoringInstanceID)
	if err != nil {
		return monitoringinstances.ManagementReview{}, err
	}
	return r.buildMonitoringInstanceManagementReview(ctx, r.db, record, monitoringInstanceID)
}

func (r *PostgresMonitoringInstanceRepository) RetireMonitoringInstance(ctx context.Context, monitoringInstanceID string, input monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error) {
	reason := strings.TrimSpace(input.Reason)
	if monitoringInstanceID = strings.TrimSpace(monitoringInstanceID); monitoringInstanceID == "" || reason == "" {
		return monitoringinstances.Record{}, monitoringinstances.ErrInvalidManagementInput
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin retire monitoring instance transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, previousLifecycleStatus, err := scanMonitoringInstanceWithPreviousState(tx.QueryRow(ctx, `
		with prior as (
			select lifecycle_status
			from monitoring_instances
			where monitoring_instance_id = $1
			for update
		),
		updated as (
			update monitoring_instances
			set lifecycle_status = '已退役',
				monitoring_status = '暂停',
				enrollment_token_hash = null,
				enrollment_token_issued_at = null,
				enrollment_token_consumed_at = null,
				sync_token_hash = '',
				pending_binding_fingerprint = null,
				pending_binding_first_seen_at = null,
				pending_binding_last_seen_at = null,
				pending_binding_attempt_count = 0,
				pending_action_id = null,
				pending_action_command_id = null,
				updated_at = now()
			where monitoring_instance_id = $1
				and archived_at is null
				and lifecycle_status = (select lifecycle_status from prior)
			returning *
		)
		select `+qualifiedMonitoringInstanceSelectColumns("updated")+`, prior.lifecycle_status
		from updated
		join prior on true`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("retire monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceLifecycleEvent(ctx, tx, record, incidents.EventMonitoringInstanceRetired, "监控实例已退役并暂停监控", reason, previousLifecycleStatus, record.LifecycleStatus, monitoringEventProvenanceWeb); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit retire monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) RestoreMonitoringInstanceLifecycle(ctx context.Context, monitoringInstanceID string, input monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error) {
	reason := strings.TrimSpace(input.Reason)
	if monitoringInstanceID = strings.TrimSpace(monitoringInstanceID); monitoringInstanceID == "" || reason == "" {
		return monitoringinstances.Record{}, monitoringinstances.ErrInvalidManagementInput
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin restore monitoring instance lifecycle transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set lifecycle_status = '观察中',
			monitoring_status = '暂停',
			updated_at = now()
		where monitoring_instance_id = $1
			and lifecycle_status = '已退役'
			and archived_at is null
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrManagementActionBlocked
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("restore monitoring instance lifecycle %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceLifecycleEvent(ctx, tx, record, incidents.EventMonitoringInstanceRestoredToObserving, "监控实例已恢复到观察中并保持暂停", reason, monitoringinstances.LifecycleRetired, record.LifecycleStatus, monitoringEventProvenanceWeb); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit restore monitoring instance lifecycle %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) ArchiveMonitoringInstance(ctx context.Context, monitoringInstanceID string, input monitoringinstances.ArchiveInput) (monitoringinstances.Record, error) {
	reason := strings.TrimSpace(input.Reason)
	confirmationName := strings.TrimSpace(input.ConfirmationName)
	if monitoringInstanceID = strings.TrimSpace(monitoringInstanceID); monitoringInstanceID == "" || reason == "" || confirmationName == "" {
		return monitoringinstances.Record{}, monitoringinstances.ErrInvalidManagementInput
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("begin archive monitoring instance transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where monitoring_instance_id = $1
		for update`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("lock monitoring instance for archive %q: %w", monitoringInstanceID, err)
	}
	if strings.TrimSpace(current.DisplayName) != confirmationName {
		return monitoringinstances.Record{}, monitoringinstances.ErrInvalidManagementInput
	}
	review, err := r.buildMonitoringInstanceManagementReview(ctx, tx, current, monitoringInstanceID)
	if err != nil {
		return monitoringinstances.Record{}, err
	}
	if !review.Actions.CanArchive {
		return monitoringinstances.Record{}, monitoringinstances.ErrManagementActionBlocked
	}

	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set archived_at = now(),
			archived_reason = $2,
			monitoring_status = '暂停',
			enrollment_token_hash = null,
			enrollment_token_issued_at = null,
			enrollment_token_consumed_at = null,
			sync_token_hash = '',
			pending_binding_fingerprint = null,
			pending_binding_first_seen_at = null,
			pending_binding_last_seen_at = null,
			pending_binding_attempt_count = 0,
			pending_action_id = null,
			pending_action_command_id = null,
			updated_at = now()
		where monitoring_instance_id = $1
			and archived_at is null
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
		reason,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrManagementActionBlocked
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("archive monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceLifecycleEvent(ctx, tx, record, incidents.EventMonitoringInstanceLifecycleUpdated, "监控实例已归档", reason, "unarchived", "archived", monitoringEventProvenanceWeb); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit archive monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) RestoreMonitoringInstanceFromArchive(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	monitoringInstanceID = strings.TrimSpace(monitoringInstanceID)
	if monitoringInstanceID == "" {
		return monitoringinstances.Record{}, monitoringinstances.ErrInvalidManagementInput
	}

	record, err := scanMonitoringInstance(r.db.QueryRow(ctx, `
		update monitoring_instances
		set archived_at = null,
			archived_reason = '',
			lifecycle_status = '观察中',
			monitoring_status = '暂停',
			updated_at = now()
		where monitoring_instance_id = $1
			and archived_at is not null
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, monitoringinstances.ErrManagementActionBlocked
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("restore monitoring instance from archive %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) PermanentCleanupMonitoringInstance(ctx context.Context, monitoringInstanceID string, input monitoringinstances.PermanentCleanupInput) (monitoringinstances.PermanentCleanupResult, error) {
	reason := strings.TrimSpace(input.Reason)
	confirmationName := strings.TrimSpace(input.ConfirmationName)
	if monitoringInstanceID = strings.TrimSpace(monitoringInstanceID); monitoringInstanceID == "" || reason == "" || confirmationName == "" {
		return monitoringinstances.PermanentCleanupResult{}, monitoringinstances.ErrInvalidManagementInput
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.PermanentCleanupResult{}, fmt.Errorf("begin permanent cleanup monitoring instance transaction for %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where monitoring_instance_id = $1
		for update`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.PermanentCleanupResult{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return monitoringinstances.PermanentCleanupResult{}, fmt.Errorf("lock monitoring instance for permanent cleanup %q: %w", monitoringInstanceID, err)
	}
	if strings.TrimSpace(current.DisplayName) != confirmationName {
		return monitoringinstances.PermanentCleanupResult{}, monitoringinstances.ErrInvalidManagementInput
	}

	review, err := r.buildMonitoringInstanceManagementReview(ctx, tx, current, monitoringInstanceID)
	if err != nil {
		return monitoringinstances.PermanentCleanupResult{}, err
	}
	if !review.Actions.CanPermanentCleanup {
		return monitoringinstances.PermanentCleanupResult{}, monitoringinstances.ErrManagementActionBlocked
	}

	var deletedReferences int64
	for _, stmt := range []string{
		`delete from notification_records where object_type = 'monitoring_instance' and object_id = $1`,
		`delete from active_incidents where object_type = 'monitoring_instance' and object_id = $1`,
		`delete from state_change_events where object_type = 'monitoring_instance' and object_id = $1`,
		`delete from asset_lifecycle_action_steps where object_type = 'monitoring_instance' and object_id = $1`,
	} {
		tag, err := tx.Exec(ctx, stmt, monitoringInstanceID)
		if err != nil {
			return monitoringinstances.PermanentCleanupResult{}, fmt.Errorf("delete monitoring instance references for %q: %w", monitoringInstanceID, err)
		}
		deletedReferences += tag.RowsAffected()
	}

	tag, err := tx.Exec(ctx, `delete from monitoring_instances where monitoring_instance_id = $1`, monitoringInstanceID)
	if err != nil {
		return monitoringinstances.PermanentCleanupResult{}, fmt.Errorf("delete monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		return monitoringinstances.PermanentCleanupResult{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.PermanentCleanupResult{}, fmt.Errorf("commit permanent cleanup monitoring instance %q: %w", monitoringInstanceID, err)
	}

	return monitoringinstances.PermanentCleanupResult{
		MonitoringInstanceID:  monitoringInstanceID,
		Counts:                review.Counts,
		DeletedReferenceCount: int(deletedReferences),
		Deleted:               true,
	}, nil
}

func (r *PostgresMonitoringInstanceRepository) buildMonitoringInstanceManagementReview(ctx context.Context, queryer monitoringInstanceQueryer, record monitoringinstances.Record, monitoringInstanceID string) (monitoringinstances.ManagementReview, error) {
	counts, err := queryMonitoringInstanceManagementCounts(ctx, queryer, monitoringInstanceID)
	if err != nil {
		return monitoringinstances.ManagementReview{}, err
	}
	links, err := queryMonitoringInstanceManagementVPSLinks(ctx, queryer, monitoringInstanceID)
	if err != nil {
		return monitoringinstances.ManagementReview{}, err
	}
	counts.ActiveVPSLinkCount = len(links)

	review := monitoringinstances.ManagementReview{
		Record:                record,
		ActiveVPSLinks:        links,
		Counts:                counts,
		EmptyMistakeCandidate: counts.EvidenceCount() == 0,
	}
	review.Warnings, review.Blockers, review.Actions = deriveMonitoringInstanceManagementFindings(review)
	return review, nil
}

func queryMonitoringInstanceManagementCounts(ctx context.Context, queryer monitoringInstanceQueryer, monitoringInstanceID string) (monitoringinstances.ManagementCounts, error) {
	var counts monitoringinstances.ManagementCounts
	if err := queryer.QueryRow(ctx, `
		select
			(select count(*)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1),
			(select count(*)::int from host_samples where monitoring_instance_id = $1),
			(select count(*)::int from probe_observations where monitoring_instance_id = $1),
			(select count(*)::int from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = $1),
			(select count(*)::int from ip_quality_reports where monitoring_instance_id = $1),
			(select count(*)::int from active_incidents where object_type = 'monitoring_instance' and object_id = $1),
			(select count(*)::int from state_change_events where object_type = 'monitoring_instance' and object_id = $1),
			(select count(*)::int from notification_records where object_type = 'monitoring_instance' and object_id = $1),
			(select count(*)::int from asset_lifecycle_action_steps where object_type = 'monitoring_instance' and object_id = $1),
			(select count(*)::int from monitoring_instance_command_action_audit where monitoring_instance_id = $1),
			(select count(*)::int from vps_monitoring_instance_links where monitoring_instance_id = $1 and unlinked_at is null)`,
		monitoringInstanceID,
	).Scan(
		&counts.HeartbeatCount,
		&counts.HostSampleCount,
		&counts.ProbeObservationCount,
		&counts.HostSampleDailyAggregateCount,
		&counts.IPQualityReportCount,
		&counts.ActiveIncidentCount,
		&counts.StateChangeEventCount,
		&counts.NotificationRecordCount,
		&counts.AssetLifecycleActionStepCount,
		&counts.CommandActionAuditCount,
		&counts.ActiveVPSLinkCount,
	); err != nil {
		return monitoringinstances.ManagementCounts{}, fmt.Errorf("query monitoring instance management counts for %q: %w", monitoringInstanceID, err)
	}
	return counts, nil
}

func queryMonitoringInstanceManagementVPSLinks(ctx context.Context, queryer monitoringInstanceQueryer, monitoringInstanceID string) ([]monitoringinstances.ManagementVPSLink, error) {
	rows, err := queryer.Query(ctx, `
		select
			l.link_id,
			l.vps_id,
			v.display_name,
			v.lifecycle_status,
			v.usage_status,
			l.linked_at,
			l.note
		from vps_monitoring_instance_links l
		join vps_assets v on v.vps_id = l.vps_id
		where l.monitoring_instance_id = $1
			and l.unlinked_at is null
		order by l.linked_at desc, l.link_id desc`,
		monitoringInstanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query monitoring instance management vps links for %q: %w", monitoringInstanceID, err)
	}
	defer rows.Close()

	links := make([]monitoringinstances.ManagementVPSLink, 0)
	for rows.Next() {
		var link monitoringinstances.ManagementVPSLink
		if err := rows.Scan(
			&link.LinkID,
			&link.VPSID,
			&link.DisplayName,
			&link.LifecycleStatus,
			&link.UsageStatus,
			&link.LinkedAt,
			&link.Note,
		); err != nil {
			return nil, fmt.Errorf("scan monitoring instance management vps link for %q: %w", monitoringInstanceID, err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring instance management vps links for %q: %w", monitoringInstanceID, err)
	}
	return links, nil
}

func deriveMonitoringInstanceManagementFindings(review monitoringinstances.ManagementReview) ([]string, []string, monitoringinstances.ManagementActions) {
	warnings := make([]string, 0)
	blockers := make([]string, 0)
	actions := monitoringinstances.ManagementActions{}

	record := review.Record
	archived := record.ArchivedAt != nil
	hasLiveVPSLink := false
	for _, link := range review.ActiveVPSLinks {
		if link.LifecycleStatus != "cancelled" && link.LifecycleStatus != "archived" {
			hasLiveVPSLink = true
			break
		}
	}

	actions.CanRetire = !archived && record.LifecycleStatus != monitoringinstances.LifecycleRetired
	actions.CanRestoreLifecycle = !archived && record.LifecycleStatus == monitoringinstances.LifecycleRetired
	actions.CanRestoreArchive = archived
	actions.CanArchive = !archived && record.LifecycleStatus == monitoringinstances.LifecycleRetired && !hasLiveVPSLink
	actions.CanPermanentCleanup = review.EmptyMistakeCandidate || archived

	if hasLiveVPSLink {
		blockers = append(blockers, "存在仍在当前工作集的 VPS 关联")
	}
	if !archived && !review.EmptyMistakeCandidate && review.Counts.EvidenceCount() > 0 {
		blockers = append(blockers, "存在监控历史或审计引用，永久清理前需要先归档")
	}
	if !archived && record.LifecycleStatus != monitoringinstances.LifecycleRetired {
		warnings = append(warnings, "归档前需要先退役监控实例")
	}
	if review.EmptyMistakeCandidate {
		warnings = append(warnings, "该实例没有观测或审计证据，可作为误创建实例清理")
	}
	return warnings, blockers, actions
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
		  and archived_at is null
		returning `+monitoringInstanceSelectColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		archived, archiveErr := monitoringInstanceArchived(ctx, r.db, monitoringInstanceID)
		if archiveErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("check monitoring instance metadata archive state %q: %w", monitoringInstanceID, archiveErr)
		}
		if archived {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
		if input.ExpectedUpdatedAt != nil {
			exists, existsErr := r.monitoringInstanceExists(ctx, monitoringInstanceID)
			if existsErr != nil {
				return monitoringinstances.Record{}, fmt.Errorf("check monitoring instance metadata conflict %q: %w", monitoringInstanceID, existsErr)
			}
			if !exists {
				return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
			}
			return monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceMetadataConflict
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

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("begin linked monitoring instance transaction for vps %q: %w", vpsID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockVPSAndRejectActiveMonitoringLink(ctx, tx, vpsID); err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, err
	}

	monitoringInstanceID, err := ids.New("mi")
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("generate monitoring instance id: %w", err)
	}
	linkID, err := ids.New("vnl")
	if err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, fmt.Errorf("generate vps monitoring instance link id: %w", err)
	}

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
	token, err := ids.NewSecretToken("enroll")
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
			and archived_at is null
		returning enrollment_token_issued_at`,
		monitoringInstanceID,
		r.tokenHasher.hashEnrollmentToken(token),
	).Scan(&issuedAt); errors.Is(err, pgx.ErrNoRows) {
		archived, archiveErr := monitoringInstanceArchived(ctx, r.db, monitoringInstanceID)
		if archiveErr != nil {
			return monitoringinstances.EnrollmentTokenIssue{}, fmt.Errorf("issue enrollment token for monitoring instance %q: %w", monitoringInstanceID, archiveErr)
		}
		if archived {
			return monitoringinstances.EnrollmentTokenIssue{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
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
	exists, _, err := monitoringInstanceArchiveState(ctx, r.db, monitoringInstanceID)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func monitoringInstanceArchiveState(ctx context.Context, queryer monitoringInstanceQueryer, monitoringInstanceID string) (exists, archived bool, err error) {
	if err := queryer.QueryRow(ctx, `
		select
			exists (
				select 1
				from monitoring_instances
				where monitoring_instance_id = $1
			),
			exists (
				select 1
				from monitoring_instances
				where monitoring_instance_id = $1
					and archived_at is not null
			)`,
		monitoringInstanceID,
	).Scan(&exists, &archived); err != nil {
		return false, false, fmt.Errorf("check monitoring instance %q archive state: %w", monitoringInstanceID, err)
	}
	return exists, archived, nil
}

func monitoringInstanceArchived(ctx context.Context, queryer monitoringInstanceQueryer, monitoringInstanceID string) (bool, error) {
	var archived bool
	if err := queryer.QueryRow(ctx, `
		select exists (
			select 1
			from monitoring_instances
			where monitoring_instance_id = $1
				and archived_at is not null
		)`,
		monitoringInstanceID,
	).Scan(&archived); err != nil {
		return false, fmt.Errorf("check monitoring instance %q archive state: %w", monitoringInstanceID, err)
	}
	return archived, nil
}

func mapMonitoringInstanceArchiveStateMiss(ctx context.Context, queryer monitoringInstanceQueryer, monitoringInstanceID string, fallback error) error {
	exists, archived, err := monitoringInstanceArchiveState(ctx, queryer, monitoringInstanceID)
	if err != nil {
		return err
	}
	if archived {
		return monitoringinstances.ErrArchivedMonitoringInstance
	}
	if !exists {
		return monitoringinstances.ErrMonitoringInstanceNotFound
	}
	return fallback
}

func insertMonitoringInstanceBindingEvent(
	ctx context.Context,
	tx pgx.Tx,
	record monitoringinstances.Record,
	eventType incidents.EventType,
	summary string,
	priorState string,
	provenance monitoringEventProvenance,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate binding event id: %w", err)
	}

	eventAt := canonicalTask4MonitoringEventTimestamp(record.UpdatedAt)
	payload, err := marshalTask4MonitoringEventPayload(task4MonitoringEventPayload{
		ObjectType:          incidents.ObjectTypeMonitoringInstance,
		EventType:           eventType,
		EventAt:             eventAt,
		RecordedAt:          eventAt,
		IsBackfilled:        false,
		Provenance:          provenance,
		ProducerVersion:     monitoringEventProducerVersion,
		RuleVersion:         monitoringEventBindingRuleVersion,
		PriorState:          priorState,
		ResultingState:      record.BindingStatus,
		CorrectionOfEventID: "",
		BindingStatus:       record.BindingStatus,
	})
	if err != nil {
		return fmt.Errorf("build binding event payload: %w", err)
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
		eventAt,
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
			and archived_at is null
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		missErr := mapMonitoringInstanceArchiveStateMiss(ctx, tx, monitoringInstanceID, monitoringinstances.ErrInvalidBindingTransition)
		if errors.Is(missErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		if errors.Is(missErr, monitoringinstances.ErrArchivedMonitoringInstance) {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
		if missErr != nil && !errors.Is(missErr, monitoringinstances.ErrInvalidBindingTransition) {
			return monitoringinstances.Record{}, fmt.Errorf("confirm monitoring instance rebind for %q: %w", monitoringInstanceID, missErr)
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
		monitoringinstances.BindingPendingConfirmation,
		monitoringEventProvenanceWeb,
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
			and archived_at is null
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		missErr := mapMonitoringInstanceArchiveStateMiss(ctx, tx, monitoringInstanceID, monitoringinstances.ErrInvalidBindingTransition)
		if errors.Is(missErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		if errors.Is(missErr, monitoringinstances.ErrArchivedMonitoringInstance) {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
		if missErr != nil && !errors.Is(missErr, monitoringinstances.ErrInvalidBindingTransition) {
			return monitoringinstances.Record{}, fmt.Errorf("reject pending fingerprint for monitoring instance %q: %w", monitoringInstanceID, missErr)
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
		monitoringinstances.BindingPendingConfirmation,
		monitoringEventProvenanceWeb,
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

	record, previousBindingStatus, err := scanMonitoringInstanceWithPreviousState(tx.QueryRow(ctx, `
		with prior as (
			select binding_status
			from monitoring_instances
			where monitoring_instance_id = $1
			for update
		),
		updated as (
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
				and archived_at is null
				and binding_status = (select binding_status from prior)
			returning *
		)
		select `+qualifiedMonitoringInstanceSelectColumns("updated")+`, prior.binding_status
		from updated
		join prior on true`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		archived, archiveErr := monitoringInstanceArchived(ctx, tx, monitoringInstanceID)
		if archiveErr != nil {
			return monitoringinstances.Record{}, fmt.Errorf("reset monitoring instance binding for %q: %w", monitoringInstanceID, archiveErr)
		}
		if archived {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
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
		previousBindingStatus,
		monitoringEventProvenanceWeb,
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
	reason string,
	priorState string,
	resultingState string,
	provenance monitoringEventProvenance,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate monitoring instance lifecycle event id: %w", err)
	}

	eventAt := canonicalTask4MonitoringEventTimestamp(record.UpdatedAt)
	payload, err := marshalTask4MonitoringEventPayload(task4MonitoringEventPayload{
		ObjectType:          incidents.ObjectTypeMonitoringInstance,
		EventType:           eventType,
		EventAt:             eventAt,
		RecordedAt:          eventAt,
		IsBackfilled:        false,
		Provenance:          provenance,
		ProducerVersion:     monitoringEventProducerVersion,
		RuleVersion:         monitoringEventLifecycleRuleVersion,
		PriorState:          priorState,
		ResultingState:      resultingState,
		CorrectionOfEventID: "",
		LifecycleStatus:     record.LifecycleStatus,
		Reason:              strings.TrimSpace(reason),
	})
	if err != nil {
		return fmt.Errorf("build monitoring instance lifecycle event payload: %w", err)
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
		eventAt,
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
	priorState string,
	provenance monitoringEventProvenance,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate monitoring instance runtime event id: %w", err)
	}

	eventAt := canonicalTask4MonitoringEventTimestamp(record.UpdatedAt)
	payload, err := marshalTask4MonitoringEventPayload(task4MonitoringEventPayload{
		ObjectType:          incidents.ObjectTypeMonitoringInstance,
		EventType:           eventType,
		EventAt:             eventAt,
		RecordedAt:          eventAt,
		IsBackfilled:        false,
		Provenance:          provenance,
		ProducerVersion:     monitoringEventProducerVersion,
		RuleVersion:         monitoringEventRuntimeRuleVersion,
		PriorState:          priorState,
		ResultingState:      record.MonitoringStatus,
		CorrectionOfEventID: "",
		MonitoringStatus:    record.MonitoringStatus,
	})
	if err != nil {
		return fmt.Errorf("build monitoring instance runtime event payload: %w", err)
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
		eventAt,
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
			and archived_at is null
		returning `+monitoringInstanceSelectColumns,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		missErr := mapMonitoringInstanceArchiveStateMiss(ctx, tx, monitoringInstanceID, ErrInvalidMonitoringInstanceRuntimeTransition)
		if errors.Is(missErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		if errors.Is(missErr, monitoringinstances.ErrArchivedMonitoringInstance) {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
		if missErr != nil && !errors.Is(missErr, ErrInvalidMonitoringInstanceRuntimeTransition) {
			return monitoringinstances.Record{}, fmt.Errorf("set monitoring instance maintenance for %q: %w", monitoringInstanceID, missErr)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q cannot enter maintenance from current monitoring status", ErrInvalidMonitoringInstanceRuntimeTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("set monitoring instance maintenance for %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, record, incidents.EventMonitoringInstanceMonitoringMaintenanceEntered, "监控实例已进入维护", monitoringinstances.MonitoringEnabled, monitoringEventProvenanceWeb); err != nil {
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

	record, previousStatus, err := scanMonitoringInstanceWithPreviousState(tx.QueryRow(ctx, `
		with prior as (
			select monitoring_status
			from monitoring_instances
			where monitoring_instance_id = $1
			for update
		),
		updated as (
			update monitoring_instances
			set monitoring_status = '暂停',
				updated_at = now()
			where monitoring_instance_id = $1
				and monitoring_status in ('启用', '维护中')
				and archived_at is null
				and monitoring_status = (select monitoring_status from prior)
			returning *
		)
		select `+qualifiedMonitoringInstanceSelectColumns("updated")+`, prior.monitoring_status
		from updated
		join prior on true`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		missErr := mapMonitoringInstanceArchiveStateMiss(ctx, tx, monitoringInstanceID, ErrInvalidMonitoringInstanceRuntimeTransition)
		if errors.Is(missErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		if errors.Is(missErr, monitoringinstances.ErrArchivedMonitoringInstance) {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
		if missErr != nil && !errors.Is(missErr, ErrInvalidMonitoringInstanceRuntimeTransition) {
			return monitoringinstances.Record{}, fmt.Errorf("pause monitoring instance monitoring for %q: %w", monitoringInstanceID, missErr)
		}
		return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q cannot pause monitoring from current monitoring status", ErrInvalidMonitoringInstanceRuntimeTransition, monitoringInstanceID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("pause monitoring instance monitoring for %q: %w", monitoringInstanceID, err)
	}
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, record, incidents.EventMonitoringInstanceMonitoringPaused, "监控实例监控已暂停", previousStatus, monitoringEventProvenanceWeb); err != nil {
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

	record, previousStatus, err := scanMonitoringInstanceWithPreviousState(tx.QueryRow(ctx, `
		with prior as (
			select monitoring_status
			from monitoring_instances
			where monitoring_instance_id = $1
			for update
		),
		updated as (
			update monitoring_instances
			set monitoring_status = '启用',
				updated_at = now()
			where monitoring_instance_id = $1
				and monitoring_status in ('维护中', '暂停')
				and archived_at is null
				and monitoring_status = (select monitoring_status from prior)
			returning *
		)
		select `+qualifiedMonitoringInstanceSelectColumns("updated")+`, prior.monitoring_status
		from updated
		join prior on true`,
		monitoringInstanceID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		missErr := mapMonitoringInstanceArchiveStateMiss(ctx, tx, monitoringInstanceID, ErrInvalidMonitoringInstanceRuntimeTransition)
		if errors.Is(missErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q", monitoringinstances.ErrMonitoringInstanceNotFound, monitoringInstanceID)
		}
		if errors.Is(missErr, monitoringinstances.ErrArchivedMonitoringInstance) {
			return monitoringinstances.Record{}, monitoringinstances.ErrArchivedMonitoringInstance
		}
		if missErr != nil && !errors.Is(missErr, ErrInvalidMonitoringInstanceRuntimeTransition) {
			return monitoringinstances.Record{}, fmt.Errorf("resume monitoring instance monitoring for %q: %w", monitoringInstanceID, missErr)
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
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, record, eventType, summary, previousStatus, monitoringEventProvenanceWeb); err != nil {
		return monitoringinstances.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("commit resume monitoring instance monitoring for %q: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func (r *PostgresMonitoringInstanceRepository) IssueSyncToken(ctx context.Context, monitoringInstanceID string) (string, error) {
	token, err := ids.NewSecretToken("sync")
	if err != nil {
		return "", fmt.Errorf("generate sync token: %w", err)
	}

	tag, err := r.db.Exec(ctx, `
		update monitoring_instances
		set sync_token_hash = $2,
			updated_at = now()
		where monitoring_instance_id = $1
			and archived_at is null`,
		monitoringInstanceID,
		r.tokenHasher.hashSyncToken(token),
	)
	if err != nil {
		return "", fmt.Errorf("issue sync token for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		archived, archiveErr := monitoringInstanceArchived(ctx, r.db, monitoringInstanceID)
		if archiveErr != nil {
			return "", fmt.Errorf("issue sync token for monitoring instance %q: %w", monitoringInstanceID, archiveErr)
		}
		if archived {
			return "", monitoringinstances.ErrArchivedMonitoringInstance
		}
		return "", monitoringinstances.ErrMonitoringInstanceNotFound
	}

	return token, nil
}

func (r *PostgresMonitoringInstanceRepository) FindMonitoringInstanceByEnrollmentToken(ctx context.Context, token string) (monitoringinstances.Record, error) {
	record, err := scanMonitoringInstance(r.db.QueryRow(ctx, `
		select `+monitoringInstanceSelectColumns+`
		from monitoring_instances
		where enrollment_token_hash in ($1, $2)
			and archived_at is null
			and enrollment_token_consumed_at is null
			and enrollment_token_issued_at >= now() - interval '30 minutes'`,
		r.tokenHasher.hashEnrollmentToken(token),
		hashOpaqueToken(token),
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
		storedEnrollmentTokenHash  string
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
			coalesce(enrollment_token_hash, ''),
			binding_status,
			coalesce(binding_fingerprint, ''),
			binding_epoch_started_at,
			coalesce(pending_binding_fingerprint, ''),
			pending_binding_first_seen_at,
			pending_binding_last_seen_at,
			pending_binding_attempt_count
		from monitoring_instances
		where enrollment_token_hash in ($1, $2)
			and archived_at is null
			and enrollment_token_consumed_at is null
			and enrollment_token_issued_at >= now() - interval '30 minutes'
		for update`,
		r.tokenHasher.hashEnrollmentToken(input.Token),
		hashOpaqueToken(input.Token),
	).Scan(
		&monitoringInstanceID,
		&storedEnrollmentTokenHash,
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
	enrollmentTokenHash := ""
	if isLegacySHA256TokenHash(storedEnrollmentTokenHash) {
		enrollmentTokenHash = r.tokenHasher.hashEnrollmentToken(input.Token)
	}
	if next.BindingStatus == monitoringinstances.BindingBound {
		syncToken, err = ids.NewSecretToken("sync")
		if err != nil {
			return monitoringinstances.Record{}, "", fmt.Errorf("generate sync token: %w", err)
		}
		syncTokenHash = r.tokenHasher.hashSyncToken(syncToken)
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
			enrollment_token_hash = case when $10 <> '' then $10 else enrollment_token_hash end,
			enrollment_token_consumed_at = now(),
			updated_at = now()
		where monitoring_instance_id = $1
			and archived_at is null
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
		enrollmentTokenHash,
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
	if storedSyncTokenHash == "" || !r.tokenHasher.syncTokenMatches(storedSyncTokenHash, syncToken) {
		return enrollment.ErrInvalidSyncToken
	}
	if isLegacySHA256TokenHash(storedSyncTokenHash) {
		if _, err := tx.Exec(ctx, `
			update monitoring_instances
			set sync_token_hash = $2,
				updated_at = now()
			where monitoring_instance_id = $1`,
			monitoringInstanceID,
			r.tokenHasher.hashSyncToken(syncToken),
		); err != nil {
			return fmt.Errorf("migrate sync token hash for monitoring instance %q: %w", monitoringInstanceID, err)
		}
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
	return r.QueueCommandAction(ctx, monitoringInstanceID, monitoringinstances.QueueCommandActionInput{
		ActionID:    actionID,
		CommandID:   commandID,
		Sensitivity: "standard",
		Source:      monitoringinstances.CommandActionSourceWeb,
		QueuedAt:    time.Now().UTC(),
	})
}

func (r *PostgresMonitoringInstanceRepository) RecordRejectedCommandAction(ctx context.Context, monitoringInstanceID string, input monitoringinstances.RejectedCommandActionInput) error {
	if err := insertCommandActionAudit(ctx, r.db, commandActionAuditEvent{
		MonitoringInstanceID: monitoringInstanceID,
		CommandID:            input.CommandID,
		Sensitivity:          input.Sensitivity,
		EventType:            "rejected",
		ActorUserID:          input.ActorUserID,
		Source:               input.Source,
		OccurredAt:           input.OccurredAt,
	}); err != nil {
		return fmt.Errorf("insert rejected command action audit for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return nil
}

// QueueCommandAction queues a command for the agent to execute on its next sync
// and stores a durable pending last_action for UI/API readers.
func (r *PostgresMonitoringInstanceRepository) QueueCommandAction(ctx context.Context, monitoringInstanceID string, input monitoringinstances.QueueCommandActionInput) error {
	actionID := input.ActionID
	commandID := input.CommandID
	sensitivity := strings.TrimSpace(input.Sensitivity)
	if sensitivity == "" {
		sensitivity = "standard"
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = monitoringinstances.CommandActionSourceWeb
	}
	queuedAt := input.QueuedAt.UTC()
	if queuedAt.IsZero() {
		queuedAt = time.Now().UTC()
	}

	raw, err := marshalPendingLastAction(actionID, commandID, sensitivity, queuedAt)
	if err != nil {
		return fmt.Errorf("marshal pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin queue command action transaction for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE monitoring_instances SET pending_action_id = $1, pending_action_command_id = $2, last_action = $3, updated_at = now() WHERE monitoring_instance_id = $4 AND archived_at is null`,
		actionID, commandID, raw, monitoringInstanceID)
	if err != nil {
		return fmt.Errorf("set pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		archived, archiveErr := monitoringInstanceArchived(ctx, tx, monitoringInstanceID)
		if archiveErr != nil {
			return fmt.Errorf("set pending action for monitoring instance %q: %w", monitoringInstanceID, archiveErr)
		}
		if archived {
			return monitoringinstances.ErrArchivedMonitoringInstance
		}
		return monitoringinstances.ErrMonitoringInstanceNotFound
	}

	if err := insertCommandActionAudit(ctx, tx, commandActionAuditEvent{
		ActionID:             actionID,
		MonitoringInstanceID: monitoringInstanceID,
		CommandID:            commandID,
		Sensitivity:          sensitivity,
		EventType:            "queued",
		ActorUserID:          input.ActorUserID,
		Source:               source,
		OccurredAt:           queuedAt,
	}); err != nil {
		return fmt.Errorf("insert queued command action audit for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit queue command action transaction for monitoring instance %q: %w", monitoringInstanceID, err)
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

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

	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/nodes"
)

type nodeDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresNodeRepository struct {
	db nodeDB
}

func NewPostgresNodeRepository(db *pgxpool.Pool) *PostgresNodeRepository {
	return &PostgresNodeRepository{db: db}
}

const nodeSelectColumns = `
	node_id,
	display_name,
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
	created_at,
	updated_at`

type nodeScanner interface {
	Scan(dest ...any) error
}

var _ nodes.Repository = (*PostgresNodeRepository)(nil)
var _ enrollment.Repository = (*PostgresNodeRepository)(nil)
var _ nodes.OnboardingRepository = (*PostgresNodeRepository)(nil)

const (
	nodeMonitoringStatusMaintenance = "维护中"
	nodeMonitoringStatusPaused      = "暂停"
)

var ErrInvalidNodeRuntimeTransition = errors.New("invalid node runtime transition")

var nodeSelectColumnNames = []string{
	"node_id",
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
	"created_at",
	"updated_at",
}

func scanNode(row nodeScanner) (nodes.Record, error) {
	var record nodes.Record
	if err := row.Scan(
		&record.NodeID,
		&record.DisplayName,
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
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nodes.Record{}, err
	}
	return record, nil
}

func qualifiedNodeSelectColumns(alias string) string {
	parts := make([]string, 0, len(nodeSelectColumnNames))
	for _, column := range nodeSelectColumnNames {
		base := column
		if idx := strings.Index(column, "("); idx != -1 {
			open := strings.Index(column, "(")
			close := strings.Index(column, ",")
			if open != -1 && close != -1 && close > open+1 {
				base = strings.TrimSpace(column[open+1 : close])
			}
		}
		if base == "labels" || base == "note" || base == "current_active_incident_count" || base == "created_at" || base == "updated_at" || strings.Contains(base, "_") {
			parts = append(parts, alias+"."+base)
			continue
		}
		parts = append(parts, alias+"."+base)
	}
	return strings.Join(parts, ",\n\t\t")
}

func scanNodeWithPreviousMonitoringStatus(row nodeScanner) (nodes.Record, string, error) {
	var (
		record     nodes.Record
		priorState string
	)
	if err := row.Scan(
		&record.NodeID,
		&record.DisplayName,
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
		&record.CreatedAt,
		&record.UpdatedAt,
		&priorState,
	); err != nil {
		return nodes.Record{}, "", err
	}
	return record, priorState, nil
}

func scanNodeOnboarding(row nodeScanner) (nodes.OnboardingState, error) {
	var (
		record                 nodes.Record
		hasHostSample          bool
		hasAcceptedObservation bool
	)
	if err := row.Scan(
		&record.NodeID,
		&record.DisplayName,
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
		&record.CreatedAt,
		&record.UpdatedAt,
		&hasHostSample,
		&hasAcceptedObservation,
	); err != nil {
		return nodes.OnboardingState{}, err
	}

	state := nodes.OnboardingState{
		Record:                           record,
		Phase:                            nodes.DeriveOnboardingPhase(record, hasHostSample, hasAcceptedObservation),
		HasHostSample:                    hasHostSample,
		HasAcceptedObservation:           hasAcceptedObservation,
		EnrollmentTokenIssuedAt:          record.EnrollmentTokenIssuedAt,
		CurrentBindingFingerprintSummary: nodes.MaskFingerprintSummary(record.BindingFingerprint),
	}
	if record.PendingBindingFingerprint != "" || record.PendingBindingFirstSeenAt != nil || record.PendingBindingLastSeenAt != nil || record.PendingBindingAttemptCount > 0 {
		state.PendingBinding = &nodes.PendingBindingMetadata{
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

func (r *PostgresNodeRepository) ListNodes(ctx context.Context) ([]nodes.Record, error) {
	rows, err := r.db.Query(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		order by created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	records := make([]nodes.Record, 0)
	for rows.Next() {
		record, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}

	return records, nil
}

func (r *PostgresNodeRepository) GetNode(ctx context.Context, nodeID string) (nodes.Record, error) {
	record, err := scanNode(r.db.QueryRow(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		where node_id = $1`, nodeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("query node %q: %w", nodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) CreateNode(ctx context.Context, input nodes.CreateInput) (nodes.Record, error) {
	nodeID, err := ids.New("nd")
	if err != nil {
		return nodes.Record{}, fmt.Errorf("generate node id: %w", err)
	}

	record, err := scanNode(r.db.QueryRow(ctx, `
		insert into nodes (
			node_id,
			display_name,
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
			0,
			''
		)
		returning `+nodeSelectColumns,
		nodeID,
		input.DisplayName,
		input.Region,
		input.City,
		input.Provider,
		input.LifecycleStatus,
		nodes.MonitoringEnabled,
		nodes.BindingUnbound,
		input.Labels,
		input.Note,
		nodes.HealthNormal,
	))
	if err != nil {
		return nodes.Record{}, fmt.Errorf("create node: %w", err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) IssueEnrollmentToken(ctx context.Context, nodeID string) (string, error) {
	issue, err := r.IssueNodeEnrollmentToken(ctx, nodeID)
	if err != nil {
		return "", err
	}
	return issue.Token, nil
}

func (r *PostgresNodeRepository) IssueNodeEnrollmentToken(ctx context.Context, nodeID string) (nodes.EnrollmentTokenIssue, error) {
	token, err := ids.New("enroll")
	if err != nil {
		return nodes.EnrollmentTokenIssue{}, fmt.Errorf("generate enrollment token: %w", err)
	}

	var issuedAt time.Time
	if err := r.db.QueryRow(ctx, `
		update nodes
		set enrollment_token_hash = $2,
			enrollment_token_issued_at = now(),
			updated_at = now()
		where node_id = $1
		returning enrollment_token_issued_at`,
		nodeID,
		hashEnrollmentToken(token),
	).Scan(&issuedAt); errors.Is(err, pgx.ErrNoRows) {
		return nodes.EnrollmentTokenIssue{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return nodes.EnrollmentTokenIssue{}, fmt.Errorf("issue enrollment token for node %q: %w", nodeID, err)
	}

	return nodes.EnrollmentTokenIssue{
		Token:    token,
		IssuedAt: issuedAt,
	}, nil
}

func (r *PostgresNodeRepository) GetNodeOnboarding(ctx context.Context, nodeID string) (nodes.OnboardingState, error) {
	state, err := scanNodeOnboarding(r.db.QueryRow(ctx, `
		select `+nodeSelectColumns+`,
			exists (
				select 1
				from host_samples hs
				where hs.node_id = nodes.node_id
					and nodes.binding_fingerprint <> ''
					and nodes.binding_epoch_started_at is not null
					and hs.fingerprint = nodes.binding_fingerprint
					and hs.received_at >= nodes.binding_epoch_started_at
			),
			exists (
				select 1
				from probe_observations po
				where po.node_id = nodes.node_id
					and nodes.binding_fingerprint <> ''
					and nodes.binding_epoch_started_at is not null
					and po.fingerprint = nodes.binding_fingerprint
					and po.received_at >= nodes.binding_epoch_started_at
			)
		from nodes
		where node_id = $1`,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.OnboardingState{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.OnboardingState{}, fmt.Errorf("query onboarding state for node %q: %w", nodeID, err)
	}
	return state, nil
}

func (r *PostgresNodeRepository) nodeExists(ctx context.Context, nodeID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from nodes
			where node_id = $1
		)`,
		nodeID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check node %q existence: %w", nodeID, err)
	}
	return exists, nil
}

func insertNodeBindingEvent(
	ctx context.Context,
	tx pgx.Tx,
	record nodes.Record,
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
		string(incidents.ObjectTypeNode),
		record.NodeID,
		string(eventType),
		"",
		summary,
		payload,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert binding event for node %q: %w", record.NodeID, err)
	}

	return nil
}

func (r *PostgresNodeRepository) ConfirmNodeRebind(ctx context.Context, nodeID string) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin confirm node rebind transaction for %q: %w", nodeID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
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
		where node_id = $1
			and binding_status = '指纹变更待确认'
			and coalesce(pending_binding_fingerprint, '') <> ''
		returning `+nodeSelectColumns,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.nodeExists(ctx, nodeID)
		if existsErr != nil {
			return nodes.Record{}, fmt.Errorf("confirm node rebind for %q: %w", nodeID, existsErr)
		}
		if !exists {
			return nodes.Record{}, fmt.Errorf("%w: node %q", nodes.ErrNodeNotFound, nodeID)
		}
		return nodes.Record{}, fmt.Errorf("%w: confirm rebind requires pending fingerprint for node %q", nodes.ErrInvalidBindingTransition, nodeID)
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("confirm node rebind for %q: %w", nodeID, err)
	}

	if err := insertNodeBindingEvent(
		ctx,
		tx,
		record,
		incidents.EventNodeBindingRebindConfirmed,
		"节点已确认新的绑定指纹并等待重新建立稳定观测",
	); err != nil {
		return nodes.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit confirm node rebind for %q: %w", nodeID, err)
	}

	return record, nil
}

func (r *PostgresNodeRepository) RejectPendingFingerprint(ctx context.Context, nodeID string) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin reject pending fingerprint transaction for %q: %w", nodeID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
		set binding_status = '已绑定',
			pending_binding_fingerprint = null,
			pending_binding_first_seen_at = null,
			pending_binding_last_seen_at = null,
			pending_binding_attempt_count = 0,
			updated_at = now()
		where node_id = $1
			and binding_status = '指纹变更待确认'
			and coalesce(pending_binding_fingerprint, '') <> ''
		returning `+nodeSelectColumns,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.nodeExists(ctx, nodeID)
		if existsErr != nil {
			return nodes.Record{}, fmt.Errorf("reject pending fingerprint for node %q: %w", nodeID, existsErr)
		}
		if !exists {
			return nodes.Record{}, fmt.Errorf("%w: node %q", nodes.ErrNodeNotFound, nodeID)
		}
		return nodes.Record{}, fmt.Errorf("%w: reject pending fingerprint requires pending fingerprint for node %q", nodes.ErrInvalidBindingTransition, nodeID)
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("reject pending fingerprint for node %q: %w", nodeID, err)
	}

	if err := insertNodeBindingEvent(
		ctx,
		tx,
		record,
		incidents.EventNodeBindingPendingRejected,
		"节点已拒绝待确认指纹并保留当前绑定",
	); err != nil {
		return nodes.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit reject pending fingerprint for node %q: %w", nodeID, err)
	}

	return record, nil
}

func (r *PostgresNodeRepository) ResetNodeBinding(ctx context.Context, nodeID string) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin reset node binding transaction for %q: %w", nodeID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
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
		where node_id = $1
		returning `+nodeSelectColumns,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("reset node binding for %q: %w", nodeID, err)
	}

	if err := insertNodeBindingEvent(
		ctx,
		tx,
		record,
		incidents.EventNodeBindingReset,
		"节点已重置绑定并等待重新接入",
	); err != nil {
		return nodes.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit reset node binding for %q: %w", nodeID, err)
	}

	return record, nil
}

func insertNodeRuntimeEvent(
	ctx context.Context,
	tx pgx.Tx,
	record nodes.Record,
	eventType incidents.EventType,
	summary string,
) error {
	eventID, err := ids.New("evt")
	if err != nil {
		return fmt.Errorf("generate node runtime event id: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"monitoring_status": record.MonitoringStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal node runtime event payload: %w", err)
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
		string(incidents.ObjectTypeNode),
		record.NodeID,
		string(eventType),
		"",
		summary,
		payload,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert runtime event for node %q: %w", record.NodeID, err)
	}

	return nil
}

func (r *PostgresNodeRepository) SetNodeMonitoringMaintenance(ctx context.Context, nodeID string) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin set node maintenance transaction for %q: %w", nodeID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
		set monitoring_status = '维护中',
			updated_at = now()
		where node_id = $1
			and monitoring_status = '启用'
		returning `+nodeSelectColumns,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.nodeExists(ctx, nodeID)
		if existsErr != nil {
			return nodes.Record{}, fmt.Errorf("set node maintenance for %q: %w", nodeID, existsErr)
		}
		if !exists {
			return nodes.Record{}, fmt.Errorf("%w: node %q", nodes.ErrNodeNotFound, nodeID)
		}
		return nodes.Record{}, fmt.Errorf("%w: node %q cannot enter maintenance from current monitoring status", ErrInvalidNodeRuntimeTransition, nodeID)
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("set node maintenance for %q: %w", nodeID, err)
	}
	if err := insertNodeRuntimeEvent(ctx, tx, record, incidents.EventNodeMonitoringMaintenanceEntered, "节点监控已进入维护"); err != nil {
		return nodes.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit set node maintenance for %q: %w", nodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) PauseNodeMonitoring(ctx context.Context, nodeID string) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin pause node monitoring transaction for %q: %w", nodeID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
		set monitoring_status = '暂停',
			updated_at = now()
		where node_id = $1
			and monitoring_status in ('启用', '维护中')
		returning `+nodeSelectColumns,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.nodeExists(ctx, nodeID)
		if existsErr != nil {
			return nodes.Record{}, fmt.Errorf("pause node monitoring for %q: %w", nodeID, existsErr)
		}
		if !exists {
			return nodes.Record{}, fmt.Errorf("%w: node %q", nodes.ErrNodeNotFound, nodeID)
		}
		return nodes.Record{}, fmt.Errorf("%w: node %q cannot pause monitoring from current monitoring status", ErrInvalidNodeRuntimeTransition, nodeID)
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("pause node monitoring for %q: %w", nodeID, err)
	}
	if err := insertNodeRuntimeEvent(ctx, tx, record, incidents.EventNodeMonitoringPaused, "节点监控已暂停"); err != nil {
		return nodes.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit pause node monitoring for %q: %w", nodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) ResumeNodeMonitoring(ctx context.Context, nodeID string) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin resume node monitoring transaction for %q: %w", nodeID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, previousStatus, err := scanNodeWithPreviousMonitoringStatus(tx.QueryRow(ctx, `
		with prior as (
			select monitoring_status
			from nodes
			where node_id = $1
		),
		updated as (
			update nodes
			set monitoring_status = '启用',
				updated_at = now()
			where node_id = $1
				and monitoring_status in ('维护中', '暂停')
			returning `+nodeSelectColumns+`
		)
		select `+qualifiedNodeSelectColumns("updated")+`, prior.monitoring_status
		from updated
		join prior on true`,
		nodeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := r.nodeExists(ctx, nodeID)
		if existsErr != nil {
			return nodes.Record{}, fmt.Errorf("resume node monitoring for %q: %w", nodeID, existsErr)
		}
		if !exists {
			return nodes.Record{}, fmt.Errorf("%w: node %q", nodes.ErrNodeNotFound, nodeID)
		}
		return nodes.Record{}, fmt.Errorf("%w: node %q cannot resume monitoring from current monitoring status", ErrInvalidNodeRuntimeTransition, nodeID)
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("resume node monitoring for %q: %w", nodeID, err)
	}

	eventType := incidents.EventNodeMonitoringResumed
	summary := "节点监控已恢复"
	if previousStatus == nodeMonitoringStatusMaintenance {
		eventType = incidents.EventNodeMonitoringMaintenanceExited
		summary = "节点监控已退出维护"
	}
	if err := insertNodeRuntimeEvent(ctx, tx, record, eventType, summary); err != nil {
		return nodes.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit resume node monitoring for %q: %w", nodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) IssueSyncToken(ctx context.Context, nodeID string) (string, error) {
	token, err := ids.New("sync")
	if err != nil {
		return "", fmt.Errorf("generate sync token: %w", err)
	}

	tag, err := r.db.Exec(ctx, `
		update nodes
		set sync_token_hash = $2,
			updated_at = now()
		where node_id = $1`,
		nodeID,
		hashSyncToken(token),
	)
	if err != nil {
		return "", fmt.Errorf("issue sync token for node %q: %w", nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", nodes.ErrNodeNotFound
	}

	return token, nil
}

func (r *PostgresNodeRepository) FindNodeByEnrollmentToken(ctx context.Context, token string) (nodes.Record, error) {
	record, err := scanNode(r.db.QueryRow(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		where enrollment_token_hash = $1`,
		hashEnrollmentToken(token),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("query node by enrollment token: %w", err)
	}
	return record, nil
}

func clearPendingBinding(record *nodes.Record) {
	record.PendingBindingFingerprint = ""
	record.PendingBindingFirstSeenAt = nil
	record.PendingBindingLastSeenAt = nil
	record.PendingBindingAttemptCount = 0
}

func resolveEnrollmentBindingTransition(record nodes.Record, newFingerprint string, now time.Time) nodes.Record {
	next := record
	switch record.BindingStatus {
	case nodes.BindingUnbound:
		next.BindingStatus = nodes.BindingBound
		next.BindingFingerprint = newFingerprint
		next.BindingEpochStartedAt = &now
		clearPendingBinding(&next)
		return next
	case nodes.BindingBound:
		if record.BindingFingerprint == newFingerprint {
			next.BindingStatus = nodes.BindingBound
			next.BindingFingerprint = record.BindingFingerprint
			clearPendingBinding(&next)
			return next
		}
		next.BindingStatus = nodes.BindingPendingConfirmation
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
		next.BindingStatus = nodes.BindingPendingConfirmation
		next.PendingBindingFingerprint = newFingerprint
		next.PendingBindingFirstSeenAt = &now
		next.PendingBindingLastSeenAt = &now
		next.PendingBindingAttemptCount = 1
		return next
	}
}

func (r *PostgresNodeRepository) ApplyEnrollment(ctx context.Context, input enrollment.EnrollInput) (nodes.Record, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nodes.Record{}, fmt.Errorf("begin enrollment transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		nodeID                     string
		bindingStatus              string
		bindingFingerprint         string
		bindingEpochStartedAt      *time.Time
		pendingBindingFingerprint  string
		pendingBindingFirstSeenAt  *time.Time
		pendingBindingLastSeenAt   *time.Time
		pendingBindingAttemptCount int
	)
	if err := tx.QueryRow(ctx, `
		select node_id,
			binding_status,
			coalesce(binding_fingerprint, ''),
			binding_epoch_started_at,
			coalesce(pending_binding_fingerprint, ''),
			pending_binding_first_seen_at,
			pending_binding_last_seen_at,
			pending_binding_attempt_count
		from nodes
		where enrollment_token_hash = $1
		for update`,
		hashEnrollmentToken(input.Token),
	).Scan(
		&nodeID,
		&bindingStatus,
		&bindingFingerprint,
		&bindingEpochStartedAt,
		&pendingBindingFingerprint,
		&pendingBindingFirstSeenAt,
		&pendingBindingLastSeenAt,
		&pendingBindingAttemptCount,
	); errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return nodes.Record{}, fmt.Errorf("query node by enrollment token for update: %w", err)
	}

	next := resolveEnrollmentBindingTransition(nodes.Record{
		BindingStatus:              bindingStatus,
		BindingFingerprint:         bindingFingerprint,
		BindingEpochStartedAt:      bindingEpochStartedAt,
		PendingBindingFingerprint:  pendingBindingFingerprint,
		PendingBindingFirstSeenAt:  pendingBindingFirstSeenAt,
		PendingBindingLastSeenAt:   pendingBindingLastSeenAt,
		PendingBindingAttemptCount: pendingBindingAttemptCount,
	}, input.Fingerprint, time.Now().UTC())
	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
		set binding_status = $2,
			binding_fingerprint = $3,
			binding_epoch_started_at = $4,
			pending_binding_fingerprint = $5,
			pending_binding_first_seen_at = $6,
			pending_binding_last_seen_at = $7,
			pending_binding_attempt_count = $8,
			updated_at = now()
		where node_id = $1
		returning `+nodeSelectColumns,
		nodeID,
		next.BindingStatus,
		next.BindingFingerprint,
		next.BindingEpochStartedAt,
		nullableText(next.PendingBindingFingerprint),
		next.PendingBindingFirstSeenAt,
		next.PendingBindingLastSeenAt,
		next.PendingBindingAttemptCount,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("update enrollment binding state for node %q: %w", nodeID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nodes.Record{}, fmt.Errorf("commit enrollment transaction for node %q: %w", nodeID, err)
	}
	return record, nil
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (r *PostgresNodeRepository) RecordAcceptedHeartbeats(ctx context.Context, syncToken string, writes []enrollment.HeartbeatWrite) error {
	if len(writes) == 0 {
		return nil
	}

	nodeID := writes[0].NodeID
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin heartbeat transaction for node %q: %w", nodeID, err)
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
		from nodes
		where node_id = $1
		for update`,
		nodeID,
	).Scan(&bindingStatus, &bindingFingerprint, &storedSyncTokenHash); errors.Is(err, pgx.ErrNoRows) {
		return nodes.ErrNodeNotFound
	} else if err != nil {
		return fmt.Errorf("query heartbeat state for node %q: %w", nodeID, err)
	}
	if bindingStatus != nodes.BindingBound {
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
			insert into node_heartbeats (
				node_id,
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
			write.NodeID,
			write.ObservedAt,
			write.ReceivedAt,
			write.AgentVersion,
			write.Fingerprint,
			write.SyncBatchID,
			write.IsBackfilled,
		); err != nil {
			return fmt.Errorf("record heartbeat for node %q: %w", write.NodeID, err)
		}

		if write.ObservedAt.After(lastHeartbeatAt) {
			lastHeartbeatAt = write.ObservedAt
		}
		if write.ReceivedAt.After(lastSyncAt) {
			lastSyncAt = write.ReceivedAt
		}
	}

	tag, err := tx.Exec(ctx, `
		update nodes
		set last_heartbeat_at = greatest(coalesce(last_heartbeat_at, $2), $2),
			last_sync_at = greatest(coalesce(last_sync_at, $3), $3),
			updated_at = now()
		where node_id = $1`,
		nodeID,
		lastHeartbeatAt,
		lastSyncAt,
	)
	if err != nil {
		return fmt.Errorf("touch heartbeat state for node %q: %w", nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		return nodes.ErrNodeNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit heartbeat transaction for node %q: %w", nodeID, err)
	}
	return nil
}

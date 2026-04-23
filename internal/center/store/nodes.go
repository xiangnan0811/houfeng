package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/nodes"
)

type PostgresNodeRepository struct {
	db *pgxpool.Pool
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
	coalesce(sync_token_hash, ''),
	coalesce(binding_fingerprint, ''),
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
		&record.SyncTokenHash,
		&record.BindingFingerprint,
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
	token, err := ids.New("enroll")
	if err != nil {
		return "", fmt.Errorf("generate enrollment token: %w", err)
	}

	tag, err := r.db.Exec(ctx, `
		update nodes
		set enrollment_token_hash = $2,
			updated_at = now()
		where node_id = $1`,
		nodeID,
		hashEnrollmentToken(token),
	)
	if err != nil {
		return "", fmt.Errorf("issue enrollment token for node %q: %w", nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", nodes.ErrNodeNotFound
	}

	return token, nil
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

func resolveEnrollmentBindingTransition(currentStatus, currentFingerprint, newFingerprint string) (string, string) {
	switch currentStatus {
	case nodes.BindingUnbound:
		return nodes.BindingBound, newFingerprint
	case nodes.BindingBound:
		if currentFingerprint == newFingerprint {
			return nodes.BindingBound, currentFingerprint
		}
		return nodes.BindingPendingConfirmation, currentFingerprint
	default:
		return currentStatus, currentFingerprint
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
		nodeID             string
		bindingStatus      string
		bindingFingerprint string
	)
	if err := tx.QueryRow(ctx, `
		select node_id,
			binding_status,
			coalesce(binding_fingerprint, '')
		from nodes
		where enrollment_token_hash = $1
		for update`,
		hashEnrollmentToken(input.Token),
	).Scan(&nodeID, &bindingStatus, &bindingFingerprint); errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return nodes.Record{}, fmt.Errorf("query node by enrollment token for update: %w", err)
	}

	nextBindingStatus, nextBindingFingerprint := resolveEnrollmentBindingTransition(bindingStatus, bindingFingerprint, input.Fingerprint)
	record, err := scanNode(tx.QueryRow(ctx, `
		update nodes
		set binding_status = $2,
			binding_fingerprint = $3,
			updated_at = now()
		where node_id = $1
		returning `+nodeSelectColumns,
		nodeID,
		nextBindingStatus,
		nextBindingFingerprint,
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

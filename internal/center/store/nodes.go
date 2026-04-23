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

func hashEnrollmentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
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

func (r *PostgresNodeRepository) UpdateBindingState(ctx context.Context, update enrollment.BindingUpdate) (nodes.Record, error) {
	record, err := scanNode(r.db.QueryRow(ctx, `
		update nodes
		set binding_status = $2,
			binding_fingerprint = $3,
			last_sync_at = now(),
			updated_at = now()
		where node_id = $1
		returning `+nodeSelectColumns,
		update.NodeID,
		update.BindingStatus,
		update.BindingFingerprint,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("update binding state for node %q: %w", update.NodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) RecordHeartbeat(ctx context.Context, write enrollment.HeartbeatWrite) error {
	if _, err := r.db.Exec(ctx, `
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
	return nil
}

func (r *PostgresNodeRepository) TouchHeartbeatState(ctx context.Context, write enrollment.HeartbeatWrite) (nodes.Record, error) {
	record, err := scanNode(r.db.QueryRow(ctx, `
		update nodes
		set last_heartbeat_at = greatest(coalesce(last_heartbeat_at, $2), $2),
			last_sync_at = greatest(coalesce(last_sync_at, $3), $3),
			updated_at = now()
		where node_id = $1
		returning `+nodeSelectColumns,
		write.NodeID,
		write.ObservedAt,
		write.ReceivedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("touch heartbeat state for node %q: %w", write.NodeID, err)
	}
	return record, nil
}

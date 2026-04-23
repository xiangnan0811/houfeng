package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
)

var ErrNodeNotFound = errors.New("node not found")

type NodeRecord struct {
	NodeID                     string     `json:"node_id"`
	DisplayName                string     `json:"display_name"`
	Region                     string     `json:"region"`
	City                       string     `json:"city"`
	Provider                   string     `json:"provider"`
	LifecycleStatus            string     `json:"lifecycle_status"`
	MonitoringStatus           string     `json:"monitoring_status"`
	BindingStatus              string     `json:"binding_status"`
	Labels                     []string   `json:"labels"`
	Note                       string     `json:"note"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastHeartbeatAt            *time.Time `json:"last_heartbeat_at,omitempty"`
	LastSyncAt                 *time.Time `json:"last_sync_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type CreateNodeInput struct {
	DisplayName     string   `json:"display_name"`
	Region          string   `json:"region"`
	City            string   `json:"city"`
	Provider        string   `json:"provider"`
	LifecycleStatus string   `json:"lifecycle_status"`
	Labels          []string `json:"labels"`
	Note            string   `json:"note"`
}

type NodeRepository interface {
	ListNodes(context.Context) ([]NodeRecord, error)
	GetNode(context.Context, string) (NodeRecord, error)
	CreateNode(context.Context, CreateNodeInput) (NodeRecord, error)
}

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

func scanNode(row nodeScanner) (NodeRecord, error) {
	var record NodeRecord
	if err := row.Scan(
		&record.NodeID,
		&record.DisplayName,
		&record.Region,
		&record.City,
		&record.Provider,
		&record.LifecycleStatus,
		&record.MonitoringStatus,
		&record.BindingStatus,
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
		return NodeRecord{}, err
	}
	return record, nil
}

func (r *PostgresNodeRepository) ListNodes(ctx context.Context) ([]NodeRecord, error) {
	rows, err := r.db.Query(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		order by created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]NodeRecord, 0)
	for rows.Next() {
		record, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}

	return nodes, nil
}

func (r *PostgresNodeRepository) GetNode(ctx context.Context, nodeID string) (NodeRecord, error) {
	record, err := scanNode(r.db.QueryRow(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		where node_id = $1`, nodeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return NodeRecord{}, ErrNodeNotFound
	}
	if err != nil {
		return NodeRecord{}, fmt.Errorf("query node %q: %w", nodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) CreateNode(ctx context.Context, input CreateNodeInput) (NodeRecord, error) {
	nodeID, err := ids.New("nd")
	if err != nil {
		return NodeRecord{}, fmt.Errorf("generate node id: %w", err)
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
			'enabled',
			'unbound',
			$7,
			$8,
			'normal',
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
		input.Labels,
		input.Note,
	))
	if err != nil {
		return NodeRecord{}, fmt.Errorf("create node: %w", err)
	}
	return record, nil
}

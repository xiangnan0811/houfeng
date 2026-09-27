package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/vpsmaintenance"
)

type PostgresVPSMaintenanceRepository struct {
	db interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	}
}

func NewPostgresVPSMaintenanceRepository(db *pgxpool.Pool) *PostgresVPSMaintenanceRepository {
	return &PostgresVPSMaintenanceRepository{db: db}
}

func readVPSMaintenanceReview(ctx context.Context, tx pgx.Tx, vpsID string) (vpsmaintenance.Review, error) {
	r := vpsmaintenance.Review{VPSID: vpsID, Resources: []vpsmaintenance.Resource{}}
	err := tx.QueryRow(ctx, `select lifecycle_status,coalesce((select action_id from vps_maintenance_actions where vps_id=$1 and ended_at is null),'') from vps_assets where vps_id=$1`, vpsID).Scan(&r.Lifecycle, &r.ActiveActionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, vpsmaintenance.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	rows, err := tx.Query(ctx, `select 'monitoring_instance',monitoring_instance_id,display_name,monitoring_status,control_revision,false,'{}'::text[],monitoring_status='启用' from monitoring_instances where vps_id=$1 and lifecycle_status<>'已退役' order by monitoring_instance_id`, vpsID)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var item vpsmaintenance.Resource
		if err = rows.Scan(&item.Kind, &item.ResourceID, &item.Name, &item.Control, &item.ControlRevision, &item.Shared, &item.SharedVPSIDs, &item.Eligible); err != nil {
			rows.Close()
			return r, err
		}
		r.Resources = append(r.Resources, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	rows, err = tx.Query(ctx, `with refs as (
	 select target_id,vps_id from asset_service_associations where ended_at is null and target_id is not null
	 union all select target_id,vps_id from asset_domain_associations where ended_at is null and target_id is not null)
	 select 'target',t.target_id,t.name,t.run_status,t.control_revision,
	 exists(select 1 from refs other where other.target_id=t.target_id and other.vps_id<>$1),
	 array(select distinct other.vps_id from refs other where other.target_id=t.target_id and other.vps_id<>$1 order by other.vps_id),
	 (t.run_status='启用' or (t.run_status='维护中' and exists(select 1 from vps_maintenance_effects e join vps_maintenance_actions a on a.action_id=e.action_id where e.resource_kind='target' and e.resource_id=t.target_id and e.applied_revision=t.control_revision and a.ended_at is null)))
	 from targets t where t.lifecycle_status='active' and exists(select 1 from refs where refs.target_id=t.target_id and refs.vps_id=$1) order by t.target_id`, vpsID)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var item vpsmaintenance.Resource
		if err = rows.Scan(&item.Kind, &item.ResourceID, &item.Name, &item.Control, &item.ControlRevision, &item.Shared, &item.SharedVPSIDs, &item.Eligible); err != nil {
			rows.Close()
			return r, err
		}
		r.Resources = append(r.Resources, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	r.PreviewDigest = r.Digest()
	return r, nil
}

func (r *PostgresVPSMaintenanceRepository) Review(ctx context.Context, vpsID string) (vpsmaintenance.Review, error) {
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return vpsmaintenance.Review{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := readVPSMaintenanceReview(ctx, tx, vpsID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *PostgresVPSMaintenanceRepository) Start(ctx context.Context, vpsID string, input vpsmaintenance.StartInput, actor string) (vpsmaintenance.Review, error) {
	if strings.TrimSpace(actor) == "" {
		return vpsmaintenance.Review{}, vpsmaintenance.ErrInvalid
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return vpsmaintenance.Review{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := readVPSMaintenanceReview(ctx, tx, vpsID)
	if err != nil {
		return current, err
	}
	selected, err := vpsmaintenance.Select(current, input)
	if err != nil {
		return current, err
	}
	actionID, err := ids.New("vmaint")
	if err != nil {
		return current, err
	}
	if _, err = tx.Exec(ctx, `insert into vps_maintenance_actions(action_id,vps_id,reason,actor) values($1,$2,$3,$4)`, actionID, vpsID, strings.TrimSpace(input.Reason), actor); err != nil {
		return current, err
	}
	for _, item := range selected {
		var revision int64
		previousControl := item.Control
		if item.Kind == "target" && item.Control == "维护中" {
			// Join an existing VPS maintenance hold without taking ownership of
			// independently configured maintenance or changing its revision.
			err = tx.QueryRow(ctx, `select e.previous_control,e.applied_revision from vps_maintenance_effects e join vps_maintenance_actions a on a.action_id=e.action_id where e.resource_kind='target' and e.resource_id=$1 and e.applied_revision=$2 and a.ended_at is null order by a.started_at,a.action_id limit 1`, item.ResourceID, item.ControlRevision).Scan(&previousControl, &revision)
			if err != nil {
				return current, err
			}
		} else {
			query := `update targets set run_status='维护中',control_revision=control_revision+1,updated_at=now() where target_id=$1 and control_revision=$2 and run_status='启用' returning control_revision`
			if item.Kind == "monitoring_instance" {
				query = `update monitoring_instances set monitoring_status='维护中',control_revision=control_revision+1,updated_at=now() where monitoring_instance_id=$1 and control_revision=$2 and monitoring_status='启用' returning control_revision`
			}
			if err = tx.QueryRow(ctx, query, item.ResourceID, item.ControlRevision).Scan(&revision); err != nil {
				return current, err
			}
		}
		if _, err = tx.Exec(ctx, `insert into vps_maintenance_effects(action_id,resource_kind,resource_id,previous_control,applied_revision) values($1,$2,$3,$4,$5)`, actionID, item.Kind, item.ResourceID, previousControl, revision); err != nil {
			return current, err
		}
	}
	result, err := readVPSMaintenanceReview(ctx, tx, vpsID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *PostgresVPSMaintenanceRepository) End(ctx context.Context, vpsID string, input vpsmaintenance.EndInput, actor string) (vpsmaintenance.Review, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(input.Reason) == "" {
		return vpsmaintenance.Review{}, vpsmaintenance.ErrInvalid
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return vpsmaintenance.Review{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := readVPSMaintenanceReview(ctx, tx, vpsID)
	if err != nil {
		return current, err
	}
	if current.ActiveActionID == "" || current.Lifecycle != "active" {
		return current, vpsmaintenance.ErrConflict
	}
	// A later direct command increments revision even when it sets maintenance
	// again. Such a decision supersedes this operation and must not be restored.
	if _, err = tx.Exec(ctx, `update monitoring_instances m set monitoring_status=e.previous_control,control_revision=m.control_revision+1,updated_at=now() from vps_maintenance_effects e where e.action_id=$1 and e.resource_kind='monitoring_instance' and e.resource_id=m.monitoring_instance_id and m.control_revision=e.applied_revision and m.monitoring_status='维护中' and m.lifecycle_status<>'已退役'`, current.ActiveActionID); err != nil {
		return current, err
	}
	if _, err = tx.Exec(ctx, `update targets t set run_status=e.previous_control,control_revision=t.control_revision+1,updated_at=now() from vps_maintenance_effects e where e.action_id=$1 and e.resource_kind='target' and e.resource_id=t.target_id and t.control_revision=e.applied_revision and t.run_status='维护中' and t.lifecycle_status='active' and not exists(select 1 from vps_maintenance_effects other join vps_maintenance_actions a on a.action_id=other.action_id where other.resource_kind='target' and other.resource_id=t.target_id and other.applied_revision=t.control_revision and a.ended_at is null and a.action_id<>$1)`, current.ActiveActionID); err != nil {
		return current, err
	}
	if _, err = tx.Exec(ctx, `update vps_maintenance_actions set ended_at=now(),ended_reason=$2,ended_by=$3 where action_id=$1`, current.ActiveActionID, strings.TrimSpace(input.Reason), actor); err != nil {
		return current, err
	}
	result, err := readVPSMaintenanceReview(ctx, tx, vpsID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// Archive closes ownership of maintenance effects without re-enabling anything.
func endVPSMaintenanceForArchive(ctx context.Context, tx pgx.Tx, vpsID, reason string) error {
	_, err := tx.Exec(ctx, `update vps_maintenance_actions set ended_at=now(),ended_reason=$2,ended_by='system:archive' where vps_id=$1 and ended_at is null`, vpsID, reason)
	return err
}

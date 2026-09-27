package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"time"
)

func clearVPSPendingCommands(ctx context.Context, tx pgx.Tx, vpsID, reason string) error {
	rows, err := tx.Query(ctx, `select `+monitoringInstanceSelectColumns+` from monitoring_instances where vps_id=$1 order by monitoring_instance_id for update`, vpsID)
	if err != nil {
		return err
	}
	records := []monitoringinstances.Record{}
	for rows.Next() {
		r, err := scanMonitoringInstance(rows)
		if err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, r := range records {
		if err := demoteMonitoringSessionsTx(ctx, tx, r); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `update monitoring_instances set pending_action_id=null,pending_action_command_id=null,last_action=case when last_action->>'status'='pending' then null else last_action end where vps_id=$1`, vpsID)
	return err
}

func archiveVPSMonitoringAndTargets(ctx context.Context, tx pgx.Tx, vpsID, reason string) error {
	var archiveAt time.Time
	if err := tx.QueryRow(ctx, `select now()`).Scan(&archiveAt); err != nil {
		return err
	}
	episode := vpsID + ":" + archiveAt.UTC().Format(time.RFC3339Nano)
	rows, err := tx.Query(ctx, `select monitoring_instance_id from monitoring_instances where vps_id=$1 and lifecycle_status<>'已退役' order by monitoring_instance_id for update`, vpsID)
	if err != nil {
		return err
	}
	var instanceIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		instanceIDs = append(instanceIDs, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range instanceIDs {
		if _, _, err := retireMonitoringInstanceTx(ctx, tx, id, reason); err != nil {
			return err
		}
	}
	if err := clearVPSPendingCommands(ctx, tx, vpsID, reason); err != nil {
		return err
	}
	// All historical installations belong to the same VPS and their active
	// incidents are closed as management actions, never as natural recoveries.
	rows, err = tx.Query(ctx, `select monitoring_instance_id from monitoring_instances where vps_id=$1 order by monitoring_instance_id`, vpsID)
	if err != nil {
		return err
	}
	instanceIDs = nil
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		instanceIDs = append(instanceIDs, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range instanceIDs {
		if err := closeArchiveIncidents(ctx, tx, "monitoring_instance", id, reason); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `with refs as (
 select target_id,vps_id from asset_service_associations where ended_at is null and target_id is not null
 union all select target_id,vps_id from asset_domain_associations where ended_at is null and target_id is not null)
 select distinct target_id,not exists(select 1 from refs other where other.target_id=refs.target_id and other.vps_id<>$1)
 from refs where vps_id=$1 order by target_id`, vpsID)
	if err != nil {
		return err
	}
	type targetImpact struct {
		id        string
		exclusive bool
	}
	var impacts []targetImpact
	for rows.Next() {
		var item targetImpact
		if err := rows.Scan(&item.id, &item.exclusive); err != nil {
			rows.Close()
			return err
		}
		impacts = append(impacts, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, impact := range impacts {
		if !impact.exclusive {
			details, _ := json.Marshal(map[string]string{"target_id": impact.id, "reason": reason})
			if err := upsertVPSFollowup(ctx, tx, vpsID, "shared_target", episode+":"+impact.id, "归档保留了共享探测，请核对后续归属。", details); err != nil {
				return err
			}
			continue
		}
		var alreadyRetired bool
		if err := tx.QueryRow(ctx, `select lifecycle_status='retired' from targets where target_id=$1 for update`, impact.id).Scan(&alreadyRetired); err != nil {
			return err
		}
		if !alreadyRetired {
			if _, _, err := transitionTargetTx(ctx, tx, impact.id, "archive"); err != nil {
				return err
			}
		}
		if err := closeArchiveIncidents(ctx, tx, "target", impact.id, reason); err != nil {
			return err
		}
	}
	var potentialCharge bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from subscriptions where vps_id=$1 and (status='active' or auto_renew)) or exists(select 1 from vps_assets where vps_id=$1 and auto_renew_check in ('unchecked','enabled'))`, vpsID).Scan(&potentialCharge); err != nil {
		return err
	}
	if potentialCharge {
		if err := upsertVPSFollowup(ctx, tx, vpsID, "potential_charge", episode, "已归档资源仍可能扣费，请核对服务商自动续费。", json.RawMessage(`{}`)); err != nil {
			return err
		}
	}
	return nil
}

func closeArchiveIncidents(ctx context.Context, tx pgx.Tx, objectType, objectID, reason string) error {
	rows, err := tx.Query(ctx, `select incident_id,incident_class,severity from active_incidents where object_type=$1 and object_id=$2 order by incident_id for update`, objectType, objectID)
	if err != nil {
		return err
	}
	events := []incidents.StateChangeEventRecord{}
	for rows.Next() {
		var incidentID string
		var class incidents.IncidentClass
		var severity incidents.Severity
		if err := rows.Scan(&incidentID, &class, &severity); err != nil {
			rows.Close()
			return err
		}
		prior := map[incidents.Severity]string{incidents.SeverityNotice: "notice", incidents.SeverityAlert: "alert", incidents.SeverityCritical: "critical"}[severity]
		events = append(events, incidents.StateChangeEventRecord{IncidentID: incidentID, IncidentClass: class, ObjectType: incidents.ObjectType(objectType), ObjectID: objectID, EventType: incidents.EventIncidentClosedByManagement, ClosureReason: reason, Severity: severity, Summary: "管理动作关闭运行异常：" + reason, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Provenance: incidents.MonitoringEventProvenanceCenter, ProducerVersion: incidents.MonitoringEventProducerVersion, RuleVersion: incidents.MonitoringEventIncidentRuleVersion, PriorState: prior, ResultingState: "closed_by_management"})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if err := insertStateChangeEvents(ctx, tx, events); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from active_incidents where object_type=$1 and object_id=$2`, objectType, objectID); err != nil {
		return err
	}
	if objectType == "monitoring_instance" {
		_, err = tx.Exec(ctx, `update monitoring_instances set current_active_incident_count=0,current_primary_issue_summary='',current_health_status='unknown' where monitoring_instance_id=$1`, objectID)
	} else {
		_, err = tx.Exec(ctx, `update targets set current_active_incident_count=0,current_primary_issue_summary='',current_health_status='unknown' where target_id=$1`, objectID)
	}
	return err
}

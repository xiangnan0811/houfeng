package store

import (
	"context"
	"fmt"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
)

// Callers take the graph lock before reading this impact set. Historical edges
// remain visible; classification, not omission, controls action eligibility.
func loadAssetDependencyImpacts(ctx context.Context, db assetLifecycleQueryer, monitoringIDs, targetIDs []string) ([]assetlinks.DependencyImpact, error) {
	rows, err := db.Query(ctx, `
		select 'monitoring_instance', l.monitoring_instance_id, v.vps_id,
			v.lifecycle_status, 'monitoring_instance_link', l.link_id,
			case when l.unlinked_at is null then 'linked' else 'unlinked' end
		from vps_monitoring_instance_links l join vps_assets v using (vps_id)
		where l.monitoring_instance_id = any($1::text[])
		union all
		select 'target', s.target_id, v.vps_id, v.lifecycle_status, 'service', s.service_id, s.status
		from asset_services s join vps_assets v using (vps_id)
		where s.target_id = any($2::text[])
		union all
		select 'target', d.target_id, v.vps_id, v.lifecycle_status, 'domain', d.domain_id, d.status
		from asset_domains d join vps_assets v using (vps_id)
		where d.target_id = any($2::text[])
		order by 1, 2, 3, 5, 6`, monitoringIDs, targetIDs)
	if err != nil {
		return nil, fmt.Errorf("load asset dependency impacts: %w", err)
	}
	defer rows.Close()
	impacts := make([]assetlinks.DependencyImpact, 0)
	for rows.Next() {
		var impact assetlinks.DependencyImpact
		if err := rows.Scan(&impact.ObjectType, &impact.ObjectID, &impact.VPSID, &impact.VPSLifecycleStatus, &impact.RelationType, &impact.RelationID, &impact.RelationStatus); err != nil {
			return nil, fmt.Errorf("scan asset dependency impact: %w", err)
		}
		impact.Classification = assetlinks.ClassifyDependency(impact.VPSLifecycleStatus, impact.RelationType, impact.RelationStatus)
		impacts = append(impacts, impact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read asset dependency impacts: %w", err)
	}
	return impacts, nil
}

func validateCancellationSharedImpacts(vpsID string, preview assetlifecycle.CancellationPreview, input assetlifecycle.ApplyCancellationInput) error {
	selected := make(map[assetlinks.SharedObjectReference]bool, len(input.MonitoringInstanceActions)+len(input.TargetActions))
	for _, action := range input.MonitoringInstanceActions {
		selected[assetlinks.SharedObjectReference{ObjectType: assetlifecycle.ObjectTypeMonitoringInstance, ObjectID: action.MonitoringInstanceID}] = true
	}
	for _, action := range input.TargetActions {
		selected[assetlinks.SharedObjectReference{ObjectType: assetlifecycle.ObjectTypeTarget, ObjectID: action.TargetID}] = true
	}
	for _, object := range input.ConfirmedSharedObjects {
		delete(selected, object)
	}
	for _, impact := range preview.DependencyImpacts {
		object := assetlinks.SharedObjectReference{ObjectType: impact.ObjectType, ObjectID: impact.ObjectID}
		if selected[object] && impact.VPSID != vpsID && impact.RequiresCancellationConfirmation() {
			return fmt.Errorf("%w: %s %q also affects VPS %q", assetlifecycle.ErrSharedImpactConfirmationRequired, impact.ObjectType, impact.ObjectID, impact.VPSID)
		}
	}
	return nil
}

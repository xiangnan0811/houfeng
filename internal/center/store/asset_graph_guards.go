package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func lockAssetVPSLifecycle(ctx context.Context, tx pgx.Tx, vpsID string) (vpsassets.LifecycleStatus, error) {
	var lifecycle vpsassets.LifecycleStatus
	err := tx.QueryRow(ctx, `
		select lifecycle_status
		from vps_assets
		where vps_id = $1
		for update`, vpsID).Scan(&lifecycle)
	return lifecycle, err
}

func isTerminalVPSLifecycle(lifecycle vpsassets.LifecycleStatus) bool {
	return lifecycle == vpsassets.LifecycleCancelled || lifecycle == vpsassets.LifecycleArchived
}

func ensureVPSAcceptsCurrentAssetRelationship(vpsID string, lifecycle vpsassets.LifecycleStatus, relationship string) error {
	if !isTerminalVPSLifecycle(lifecycle) {
		return nil
	}
	return fmt.Errorf("%w: terminal %s vps %q cannot accept a current %s relationship", vpsassets.ErrVPSAssetReadonly, lifecycle, vpsID, relationship)
}

func ensureVPSAllowsAssetRelationshipCreate(vpsID string, lifecycle vpsassets.LifecycleStatus, status string, relationship string) error {
	if !isTerminalVPSLifecycle(lifecycle) {
		return nil
	}
	if status == "paused" || status == "retired" {
		return nil
	}
	return fmt.Errorf("%w: terminal %s vps %q cannot accept a %s relationship in %q state", vpsassets.ErrVPSAssetReadonly, lifecycle, vpsID, relationship, status)
}

func lockAssetTargetRunStatus(ctx context.Context, tx pgx.Tx, targetID string) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `
		select run_status
		from targets
		where target_id = $1
		for update`, targetID).Scan(&status)
	return status, err
}

func ensureTargetAllowsAssetRelationship(targetID, targetStatus, relationshipStatus, relationship string) error {
	if targetStatus != targets.RunStatusArchived || relationshipStatus != "active" {
		return nil
	}
	return fmt.Errorf("%w: archived target %q cannot be referenced by an active %s", targets.ErrTargetMetadataConflict, targetID, relationship)
}

func lockAssetServiceOwner(ctx context.Context, tx pgx.Tx, serviceID string) (string, error) {
	var vpsID string
	err := tx.QueryRow(ctx, `
		select vps_id
		from asset_services
		where service_id = $1
		for update`, serviceID).Scan(&vpsID)
	return vpsID, err
}

func lockMonitoringInstanceForAssetLink(ctx context.Context, tx pgx.Tx, monitoringInstanceID string) (string, *time.Time, error) {
	var lifecycle string
	var archivedAt *time.Time
	err := tx.QueryRow(ctx, `
		select lifecycle_status, archived_at
		from monitoring_instances
		where monitoring_instance_id = $1
		for update`, monitoringInstanceID).Scan(&lifecycle, &archivedAt)
	return lifecycle, archivedAt, err
}

func ensureMonitoringInstanceCanBeLinked(monitoringInstanceID, lifecycle string, archivedAt *time.Time) error {
	if archivedAt == nil && lifecycle != monitoringinstances.LifecycleRetired {
		return nil
	}
	return fmt.Errorf("%w: monitoring instance %q is archived or retired and cannot be linked", assetlinks.ErrVPSMonitoringInstanceLinkConflict, monitoringInstanceID)
}

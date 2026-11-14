package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

// Callers must acquire admission and the asset graph lock before any business
// reads. The graph lock serializes these exact-key checks with retirement; a
// live row or a missing terminal pin never overrides a barrier or unknown guard.
// Keep the transaction interface narrow for sync and incident transaction adapters.
func assertSourceWritable(ctx context.Context, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, kind recordauth.SourceKind, id string) error {
	var unavailable error
	switch kind {
	case "vps":
		unavailable = vpsassets.ErrVPSAssetNotFound
	case "monitoring_instance":
		unavailable = monitoringinstances.ErrMonitoringInstanceNotFound
	case "target":
		unavailable = targets.ErrTargetNotFound
	default:
		return errors.New("unsupported source mutation kind")
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `select
		exists(select 1 from public.source_retirement_barriers
			where project_id='default' and source_kind=$1 and source_id=$2)
		or exists(select 1 from public.source_retirement_coverage_unknown
			where project_id='default' and source_kind=$1 and source_id=$2)
		or public.source_legacy_retirement_blocked($1,$2)`, string(kind), id).Scan(&blocked); err != nil {
		return fmt.Errorf("check source mutation authority: %w", err)
	}
	if blocked {
		return unavailable
	}
	return nil
}

// A monitoring instance's live authority also depends on its actual parent.
// Missing instances retain the caller's existing not-found/CAS handling.
func assertMonitoringSourceWritable(ctx context.Context, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) error {
	if err := assertSourceWritable(ctx, tx, "monitoring_instance", id); err != nil {
		return err
	}
	var parent *string
	if err := tx.QueryRow(ctx, `select vps_id from monitoring_instances where monitoring_instance_id=$1`, id).Scan(&parent); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read monitoring source parent: %w", err)
	}
	if parent != nil && *parent != "" {
		if err := assertSourceWritable(ctx, tx, "vps", *parent); err != nil {
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				return monitoringinstances.ErrMonitoringInstanceNotFound
			}
			return err
		}
	}
	return nil
}

// VPS lifecycle actions also change historical installations and linked targets.
// Discover the complete set while the exclusive graph lock keeps links stable,
// close the result before issuing guards, and check every identity before effects.
func assertVPSLifecycleSourcesWritable(ctx context.Context, tx pgx.Tx, id string) error {
	if err := assertSourceWritable(ctx, tx, "vps", id); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `select 'monitoring_instance',monitoring_instance_id from monitoring_instances where vps_id=$1
		union select 'target',target_id from asset_service_associations where vps_id=$1 and ended_at is null and target_id is not null
		union select 'target',target_id from asset_domain_associations where vps_id=$1 and ended_at is null and target_id is not null
		union select e.resource_kind,e.resource_id from vps_maintenance_effects e
			join vps_maintenance_actions a on a.action_id=e.action_id where a.vps_id=$1 and a.ended_at is null
		order by 1,2`, id)
	if err != nil {
		return fmt.Errorf("read VPS lifecycle source identities: %w", err)
	}
	type sourceKey struct {
		kind recordauth.SourceKind
		id   string
	}
	var keys []sourceKey
	for rows.Next() {
		var key sourceKey
		if err := rows.Scan(&key.kind, &key.id); err != nil {
			rows.Close()
			return fmt.Errorf("scan VPS lifecycle source identity: %w", err)
		}
		keys = append(keys, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range keys {
		if err := assertSourceWritable(ctx, tx, key.kind, key.id); err != nil {
			if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) || errors.Is(err, targets.ErrTargetNotFound) {
				return vpsassets.ErrVPSAssetNotFound
			}
			return err
		}
	}
	return nil
}

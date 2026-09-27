package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
)

func TestVPSStateRepairMIOwnershipCannotBeSharedOrTransferred(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	links := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	record := createVPSStateRepairOwnedMI(t, ctx, pool, repo, "vps_owner_a", "Owned instance")
	if _, err := pool.Exec(ctx, "insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_owner_b','Other owner','active')"); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"vps_owner_a", "vps_owner_b"} {
		if _, err := links.LinkMonitoringInstance(ctx, owner, assetlinks.LinkInput{MonitoringInstanceID: record.MonitoringInstanceID}); !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
			t.Fatalf("link to %s: %v", owner, err)
		}
	}
	if _, err := links.UnlinkMonitoringInstance(ctx, record.VPSID, assetlinks.UnlinkInput{MonitoringInstanceID: record.MonitoringInstanceID}); !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
		t.Fatalf("unlink owner: %v", err)
	}
	if _, err := pool.Exec(ctx, "update monitoring_instances set vps_id='vps_owner_b' where monitoring_instance_id=$1", record.MonitoringInstanceID); err == nil {
		t.Fatal("database allowed owner transfer")
	}
	wire := monitoringinstances.LinkedCreateWireIdentity{DisplayName: "Second current", Provider: "Fixture", Region: "Region", City: "City"}
	if _, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, record.VPSID, wire, "second-current"); !errors.Is(err, assetlinks.ErrVPSActiveMonitoringInstanceExists) {
		t.Fatalf("second current: %v", err)
	}
	paused, err := repo.PauseMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID)
	if err != nil || paused.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("sole owner pause: %#v %v", paused, err)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceMonitoringPaused, 1)
	if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "replacement", IdempotencyKey: "retire-owned"}); err != nil {
		t.Fatal(err)
	}
	next, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, record.VPSID, wire, "replacement-current")
	if err != nil {
		t.Fatal(err)
	}
	if next.MonitoringInstanceID == record.MonitoringInstanceID || next.VPSID != record.VPSID {
		t.Fatal("replacement lost owner or reused retired identity")
	}
	if _, err := repo.ResetMonitoringInstanceBinding(ctx, record.MonitoringInstanceID); !errors.Is(err, monitoringinstances.ErrManagementActionBlocked) {
		t.Fatalf("historical reset competed with current: %v", err)
	}
	history, err := links.ListMonitoringInstancesForVPS(ctx, record.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("owner history has %d instances, want 2", len(history))
	}
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from monitoring_instances where vps_id=$1 and lifecycle_status<>'已退役'", record.VPSID, 1)
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from vps_monitoring_instance_links where monitoring_instance_id=$1 and unlinked_at is not null", record.MonitoringInstanceID, 1)
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from vps_monitoring_instance_links where vps_id=$1", "vps_owner_b", 0)
}

func TestVPSStateRepairMIRemovedStandaloneActionsPreserveHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	record := createVPSStateRepairOwnedMI(t, ctx, pool, repo, "vps_retained_owner", "Retained instance")
	for _, retire := range []bool{false, true} {
		if retire {
			if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "preserve history", IdempotencyKey: "retire-retained"}); err != nil {
				t.Fatal(err)
			}
		}
		review, err := repo.GetMonitoringInstanceManagementReview(ctx, record.MonitoringInstanceID)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{monitoringinstances.ManagementActionPermanentCleanup, monitoringinstances.ManagementActionArchive} {
			if review.ActionReviews[action].Allowed {
				t.Fatalf("removed action %s advertised", action)
			}
		}
		calls := []func() error{
			func() error {
				_, e := repo.PermanentCleanupMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.PermanentCleanupInput{Reason: "remove", ConfirmationName: record.DisplayName})
				return e
			},
			func() error {
				_, e := repo.ArchiveMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.ArchiveInput{Reason: "archive", ConfirmationName: record.DisplayName})
				return e
			},
			func() error {
				_, e := repo.RestoreMonitoringInstanceLifecycle(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "restore"})
				return e
			},
			func() error {
				_, e := repo.RestoreMonitoringInstanceFromArchive(ctx, record.MonitoringInstanceID)
				return e
			},
		}
		for _, call := range calls {
			if err := call(); !errors.Is(err, monitoringinstances.ErrManagementActionBlocked) {
				t.Fatalf("removed action: %v", err)
			}
		}
		persisted, err := repo.GetMonitoringInstance(ctx, record.MonitoringInstanceID)
		if err != nil || persisted.VPSID != record.VPSID {
			t.Fatalf("removed operation altered owner history: %#v %v", persisted, err)
		}
		assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from vps_monitoring_instance_links where monitoring_instance_id=$1", record.MonitoringInstanceID, 1)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
}

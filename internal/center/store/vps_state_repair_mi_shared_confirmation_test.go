package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
)

func TestVPSStateRepairMISharedPauseAndRetirementRequireFreshConfirmation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)

	vpsIDs := []string{"vps_mi_shared_a", "vps_mi_shared_b", "vps_mi_shared_c"}
	for _, vpsID := range vpsIDs {
		if _, err := pool.Exec(ctx, `
			insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
			values ($1, $1, 'active', 'idle')`, vpsID); err != nil {
			t.Fatalf("insert parent VPS %q: %v", vpsID, err)
		}
	}
	record, err := miRepo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Shared MI lifecycle confirmation",
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance: %v", err)
	}
	for _, vpsID := range vpsIDs[:2] {
		if _, err := linkRepo.LinkMonitoringInstance(ctx, vpsID, assetlinks.LinkInput{
			MonitoringInstanceID: record.MonitoringInstanceID,
			Note:                 "shared lifecycle test",
		}); err != nil {
			t.Fatalf("LinkMonitoringInstance to %q: %v", vpsID, err)
		}
	}

	initialReview, err := miRepo.GetMonitoringInstanceManagementReview(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceManagementReview: %v", err)
	}
	if len(initialReview.DependencyImpacts) != 2 {
		t.Fatalf("dependency impacts = %#v, want two current VPS links", initialReview.DependencyImpacts)
	}
	parents := map[string]bool{}
	for _, impact := range initialReview.DependencyImpacts {
		if impact.Classification != assetlinks.DependencyCurrent {
			t.Fatalf("dependency classification = %q, want current", impact.Classification)
		}
		parents[impact.VPSID] = true
	}
	if len(parents) != 2 {
		t.Fatalf("effective parent VPS set = %#v, want two parents", parents)
	}

	if _, err := miRepo.PauseMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("Pause without shared confirmation = %v, want shared confirmation required", err)
	}
	assertVPSStateRepairMIRuntimeState(t, ctx, pool, record.MonitoringInstanceID, monitoringinstances.LifecycleInUse, monitoringinstances.MonitoringEnabled)
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceMonitoringPaused, 0)

	if _, err := linkRepo.LinkMonitoringInstance(ctx, vpsIDs[2], assetlinks.LinkInput{
		MonitoringInstanceID: record.MonitoringInstanceID,
		Note:                 "added after preview",
	}); err != nil {
		t.Fatalf("add third parent after preview: %v", err)
	}
	if _, err := miRepo.PauseMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID, monitoringinstances.RuntimeControlInput{
		GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{PreviewDigest: initialReview.PreviewDigest, ConfirmSharedImpact: true},
	}); !errors.Is(err, assetlifecycle.ErrStaleCancellationPreview) {
		t.Fatalf("Pause with stale shared review = %v, want stale review conflict", err)
	}
	assertVPSStateRepairMIRuntimeState(t, ctx, pool, record.MonitoringInstanceID, monitoringinstances.LifecycleInUse, monitoringinstances.MonitoringEnabled)

	currentReview, err := miRepo.GetMonitoringInstanceManagementReview(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("refresh MI management review: %v", err)
	}
	withoutDigest := monitoringinstances.RuntimeControlInput{
		GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{ConfirmSharedImpact: true},
	}
	if _, err := miRepo.PauseMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID, withoutDigest); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("Pause with shared flag but no digest = %v, want confirmation required", err)
	}
	paused, err := miRepo.PauseMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID, monitoringinstances.RuntimeControlInput{
		GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{PreviewDigest: currentReview.PreviewDigest, ConfirmSharedImpact: true},
	})
	if err != nil {
		t.Fatalf("Pause with current shared confirmation: %v", err)
	}
	if paused.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("paused monitoring status = %q, want paused", paused.MonitoringStatus)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceMonitoringPaused, 1)

	pausedReview, err := miRepo.GetMonitoringInstanceManagementReview(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("Get review before shared retirement: %v", err)
	}
	if _, err := miRepo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "shared retirement"}); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("Retire without shared confirmation = %v, want confirmation required", err)
	}
	assertVPSStateRepairMIRuntimeState(t, ctx, pool, record.MonitoringInstanceID, monitoringinstances.LifecycleInUse, monitoringinstances.MonitoringPaused)
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 0)

	retired, err := miRepo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{
		Reason: "shared retirement",
		GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{
			PreviewDigest:       pausedReview.PreviewDigest,
			ConfirmSharedImpact: true,
		},
	})
	if err != nil {
		t.Fatalf("Retire with current shared confirmation: %v", err)
	}
	if retired.LifecycleStatus != monitoringinstances.LifecycleRetired || retired.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("retired state = (%q, %q), want retired and paused", retired.LifecycleStatus, retired.MonitoringStatus)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
}

func assertVPSStateRepairMIRuntimeState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID, wantLifecycle, wantMonitoring string) {
	t.Helper()
	var lifecycle, monitoring string
	if err := pool.QueryRow(ctx, `
		select lifecycle_status, monitoring_status
		from monitoring_instances
		where monitoring_instance_id = $1`, monitoringInstanceID).Scan(&lifecycle, &monitoring); err != nil {
		t.Fatalf("read MI lifecycle/runtime state: %v", err)
	}
	if lifecycle != wantLifecycle || monitoring != wantMonitoring {
		t.Fatalf("MI state = (%q, %q), want (%q, %q)", lifecycle, monitoring, wantLifecycle, wantMonitoring)
	}
}

func TestVPSStateRepairMIEmptyCleanupUsesOwnCapabilitiesAndConfirmsLinkedParents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	links := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{DisplayName: "Empty mistaken shared instance", Region: "test", City: "test", Provider: "test", LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment, Labels: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{"vps_empty_cleanup_a", "vps_empty_cleanup_b"} {
		if _, err := pool.Exec(ctx, "insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status) values ($1,$1,'active','idle')", parent); err != nil {
			t.Fatal(err)
		}
		if _, err := links.LinkMonitoringInstance(ctx, parent, assetlinks.LinkInput{MonitoringInstanceID: record.MonitoringInstanceID}); err != nil {
			t.Fatal(err)
		}
	}
	review, err := repo.GetMonitoringInstanceManagementReview(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := review.ActionReviews[monitoringinstances.ManagementActionPermanentCleanup]
	if !review.EmptyMistakeCandidate || !cleanup.Allowed || len(cleanup.Blockers) != 0 || review.ActionReviews[monitoringinstances.ManagementActionArchive].Allowed {
		t.Fatalf("linked empty instance capabilities do not distinguish cleanup from archive: %#v", review.ActionReviews)
	}
	for _, parent := range []string{"vps_empty_cleanup_a", "vps_empty_cleanup_b"} {
		found := false
		for _, warning := range cleanup.Warnings {
			if strings.Contains(warning, parent) {
				found = true
			}
		}
		if !found {
			t.Errorf("cleanup warning omits cascaded parent %q", parent)
		}
	}
	input := monitoringinstances.PermanentCleanupInput{Reason: "remove mistaken instance", ConfirmationName: record.DisplayName}
	if _, err := repo.PermanentCleanupMonitoringInstance(ctx, record.MonitoringInstanceID, input); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("unconfirmed cleanup = %v, want shared confirmation", err)
	}
	if _, err := repo.GetMonitoringInstance(ctx, record.MonitoringInstanceID); err != nil {
		t.Fatalf("unconfirmed cleanup removed MI: %v", err)
	}
	input.GlobalActionConfirmation = assetlinks.GlobalActionConfirmation{PreviewDigest: review.PreviewDigest, ConfirmSharedImpact: true}
	if _, err := repo.PermanentCleanupMonitoringInstance(ctx, record.MonitoringInstanceID, input); err != nil {
		t.Fatalf("confirmed empty cleanup: %v", err)
	}
	if _, err := repo.GetMonitoringInstance(ctx, record.MonitoringInstanceID); !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("cleaned instance still exists: %v", err)
	}
	var survivingLinks int
	if err := pool.QueryRow(ctx, "select count(*) from vps_monitoring_instance_links where monitoring_instance_id = $1", record.MonitoringInstanceID).Scan(&survivingLinks); err != nil {
		t.Fatal(err)
	}
	if survivingLinks != 0 {
		t.Fatalf("cleanup left %d dangling links", survivingLinks)
	}
	var survivingParents int
	if err := pool.QueryRow(ctx, "select count(*) from vps_assets where vps_id in ('vps_empty_cleanup_a','vps_empty_cleanup_b')").Scan(&survivingParents); err != nil {
		t.Fatal(err)
	}
	if survivingParents != 2 {
		t.Fatalf("cleanup changed parent assets: %d remain", survivingParents)
	}
}

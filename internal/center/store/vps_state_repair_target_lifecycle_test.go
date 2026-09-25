package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/targets"
)

func TestVPSStateRepairTargetLifecycleConfirmationAndEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresTargetRepository(pool)

	for _, vps := range []struct {
		id   string
		name string
	}{
		{id: "vps_target_parent_a", name: "Target Parent A"},
		{id: "vps_target_parent_b", name: "Target Parent B"},
	} {
		if _, err := pool.Exec(ctx, `
			insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
			values ($1, $2, 'active', 'idle')`, vps.id, vps.name); err != nil {
			t.Fatalf("insert VPS %q: %v", vps.id, err)
		}
	}

	target, err := repo.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "Lifecycle confirmation target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "target-repair.example.test",
		ExecutionMonitoringInstanceLabels: []string{"edge"},
		RunStatus:                         targets.RunStatusEnabled,
		Group:                             "before-review",
		Labels:                            []string{"origin"},
		Note:                              "initial target facts",
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into asset_services (service_id, vps_id, target_id, name, service_type, status)
		values ('svc_target_parent_a', 'vps_target_parent_a', $1, 'web service', 'web', 'active')`, target.TargetID); err != nil {
		t.Fatalf("insert service dependency: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into asset_domains (domain_id, vps_id, target_id, domain_name, status)
		values ('dom_target_parent_b', 'vps_target_parent_b', $1, 'target-parent-b.example.test', 'active')`, target.TargetID); err != nil {
		t.Fatalf("insert domain dependency: %v", err)
	}

	initialReview, err := repo.GetTargetLifecycleReview(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTargetLifecycleReview: %v", err)
	}
	if len(initialReview.DependencyImpacts) != 2 {
		t.Fatalf("dependency impacts = %#v, want service and domain dependencies", initialReview.DependencyImpacts)
	}
	parents := map[string]bool{}
	for _, impact := range initialReview.DependencyImpacts {
		parents[impact.VPSID] = true
		if impact.Classification != assetlinks.DependencyCurrent {
			t.Errorf("dependency %s classification = %q, want current", impact.RelationID, impact.Classification)
		}
	}
	if len(parents) != 2 {
		t.Fatalf("effective parent VPS set = %#v, want two parents", parents)
	}

	group := "after-review"
	if _, err := repo.UpdateTargetMetadata(ctx, target.TargetID, targets.UpdateMetadataInput{
		Group:  &group,
		Labels: []string{"confirmed-facts"},
		Note:   "review fact changed",
	}); err != nil {
		t.Fatalf("UpdateTargetMetadata: %v", err)
	}
	updatedReview, err := repo.GetTargetLifecycleReview(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTargetLifecycleReview after metadata update: %v", err)
	}
	if updatedReview.PreviewDigest == initialReview.PreviewDigest {
		t.Fatal("target metadata change did not invalidate the lifecycle review digest")
	}

	if _, err := repo.PauseTargetRun(ctx, target.TargetID); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("PauseTargetRun without shared confirmation = %v, want shared confirmation required", err)
	}
	assertTargetRunStatus(t, ctx, pool, target.TargetID, targets.RunStatusEnabled)
	assertTargetEventCount(t, ctx, pool, target.TargetID, 0)

	pauseConfirmation := assetlinks.GlobalActionConfirmation{
		PreviewDigest:       updatedReview.PreviewDigest,
		ConfirmSharedImpact: true,
	}
	paused, err := repo.PauseTargetRun(ctx, target.TargetID, pauseConfirmation)
	if err != nil {
		t.Fatalf("PauseTargetRun with current shared confirmation: %v", err)
	}
	if paused.RunStatus != targets.RunStatusPaused {
		t.Fatalf("paused run status = %q, want %q", paused.RunStatus, targets.RunStatusPaused)
	}
	if _, err := repo.PauseTargetRun(ctx, target.TargetID); err != nil {
		t.Fatalf("same-state pause: %v", err)
	}
	assertTargetEventCount(t, ctx, pool, target.TargetID, 1)

	pausedReview, err := repo.GetTargetLifecycleReview(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTargetLifecycleReview while paused: %v", err)
	}
	waiter, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin graph-lock holder: %v", err)
	}
	defer func() { _ = waiter.Rollback(ctx) }()
	if err := lockAssetGraph(ctx, waiter); err != nil {
		t.Fatalf("lock asset graph: %v", err)
	}
	archiveDone := make(chan error, 1)
	go func() {
		actionCtx, actionCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer actionCancel()
		_, actionErr := repo.ArchiveTarget(actionCtx, target.TargetID, assetlinks.GlobalActionConfirmation{
			PreviewDigest:       pausedReview.PreviewDigest,
			ConfirmSharedImpact: true,
		})
		archiveDone <- actionErr
	}()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 1); err != nil {
		t.Fatalf("Target archive did not wait for the asset graph lock: %v", err)
	}
	select {
	case actionErr := <-archiveDone:
		t.Fatalf("Target archive completed before dependency facts changed: %v", actionErr)
	default:
	}
	if _, err := waiter.Exec(ctx, `update vps_assets set lifecycle_status = 'cancelled', renewal_decision = 'cancel', usage_status = 'idle' where vps_id = 'vps_target_parent_b'`); err != nil {
		t.Fatalf("change dependency parent lifecycle: %v", err)
	}
	if err := waiter.Commit(ctx); err != nil {
		t.Fatalf("commit dependency parent change: %v", err)
	}
	if err := <-archiveDone; !errors.Is(err, assetlifecycle.ErrStaleCancellationPreview) {
		t.Fatalf("Target archive with stale review after graph wait = %v, want stale review", err)
	}
	assertTargetRunStatus(t, ctx, pool, target.TargetID, targets.RunStatusPaused)
	assertTargetEventCount(t, ctx, pool, target.TargetID, 1)

	freshReview, err := repo.GetTargetLifecycleReview(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTargetLifecycleReview after dependency change: %v", err)
	}
	if freshReview.PreviewDigest == pausedReview.PreviewDigest {
		t.Fatal("dependency parent lifecycle change did not invalidate the lifecycle review digest")
	}
	if _, err := repo.ArchiveTarget(ctx, target.TargetID, assetlinks.GlobalActionConfirmation{PreviewDigest: freshReview.PreviewDigest}); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("Target archive without explicit shared confirmation = %v, want shared confirmation required", err)
	}
	archived, err := repo.ArchiveTarget(ctx, target.TargetID, assetlinks.GlobalActionConfirmation{
		PreviewDigest:       freshReview.PreviewDigest,
		ConfirmSharedImpact: true,
	})
	if err != nil {
		t.Fatalf("Target archive with current shared confirmation: %v", err)
	}
	if archived.RunStatus != targets.RunStatusArchived {
		t.Fatalf("archived run status = %q, want %q", archived.RunStatus, targets.RunStatusArchived)
	}
	if _, err := repo.ArchiveTarget(ctx, target.TargetID); err != nil {
		t.Fatalf("same-state archive: %v", err)
	}
	if _, err := repo.PauseTargetRun(ctx, target.TargetID); !errors.Is(err, ErrInvalidTargetRuntimeTransition) {
		t.Fatalf("pause archived target = %v, want invalid transition", err)
	}
	assertTargetEventCount(t, ctx, pool, target.TargetID, 2)

	archivedReview, err := repo.GetTargetLifecycleReview(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTargetLifecycleReview while archived: %v", err)
	}
	restored, err := repo.RestoreArchivedTargetToPaused(ctx, target.TargetID, assetlinks.GlobalActionConfirmation{
		PreviewDigest:       archivedReview.PreviewDigest,
		ConfirmSharedImpact: true,
	})
	if err != nil {
		t.Fatalf("RestoreArchivedTargetToPaused: %v", err)
	}
	if restored.RunStatus != targets.RunStatusPaused {
		t.Fatalf("restored run status = %q, want %q", restored.RunStatus, targets.RunStatusPaused)
	}
	assertTargetEvents(t, ctx, pool, target.TargetID, map[string]bool{
		string(incidents.EventTargetPaused):           true,
		string(incidents.EventTargetArchived):         true,
		string(incidents.EventTargetRestoredToPaused): true,
	})
}

func TestVPSStateRepairTargetWritersSerializeOnAssetGraph(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresTargetRepository(pool)

	var created targets.TargetRecord
	assertTargetGraphWriterWaits(t, ctx, pool, func(actionCtx context.Context) error {
		var err error
		created, err = repo.CreateTarget(actionCtx, targets.CreateTargetInput{
			Name:                              "Graph-locked target",
			TargetType:                        targets.TargetTypeService,
			Host:                              "graph-locked.example.test",
			ExecutionMonitoringInstanceLabels: []string{"edge"},
			RunStatus:                         targets.RunStatusEnabled,
			Labels:                            []string{},
		})
		return err
	})
	group := "graph-locked"
	assertTargetGraphWriterWaits(t, ctx, pool, func(actionCtx context.Context) error {
		_, err := repo.UpdateTargetMetadata(actionCtx, created.TargetID, targets.UpdateMetadataInput{Group: &group, Labels: []string{}, Note: ""})
		return err
	})
	assertTargetGraphWriterWaits(t, ctx, pool, func(actionCtx context.Context) error {
		_, err := repo.PauseTargetRun(actionCtx, created.TargetID)
		return err
	})
}

func assertTargetGraphWriterWaits(t *testing.T, ctx context.Context, pool *pgxpool.Pool, writer func(context.Context) error) {
	t.Helper()

	holder, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatalf("begin asset graph lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()

	done := make(chan error, 1)
	go func() {
		actionCtx, actionCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer actionCancel()
		done <- writer(actionCtx)
	}()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 1); err != nil {
		t.Fatalf("Target graph writer did not wait for the graph lock: %v", err)
	}
	select {
	case writerErr := <-done:
		t.Fatalf("Target graph writer completed before lock release: %v", writerErr)
	default:
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release asset graph lock: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Target graph writer after lock release: %v", err)
	}
}

func assertTargetRunStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targetID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `select run_status from targets where target_id = $1`, targetID).Scan(&got); err != nil {
		t.Fatalf("read target run status: %v", err)
	}
	if got != want {
		t.Fatalf("target run status = %q, want %q", got, want)
	}
}

func assertTargetEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targetID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
		select count(*) from state_change_events
		where object_type = $1 and object_id = $2`, string(incidents.ObjectTypeTarget), targetID).Scan(&got); err != nil {
		t.Fatalf("count target events: %v", err)
	}
	if got != want {
		t.Fatalf("target event count = %d, want %d", got, want)
	}
}

func assertTargetEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targetID string, want map[string]bool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		select event_type from state_change_events
		where object_type = $1 and object_id = $2`, string(incidents.ObjectTypeTarget), targetID)
	if err != nil {
		t.Fatalf("query target events: %v", err)
	}
	defer rows.Close()
	got := make(map[string]bool)
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan target event type: %v", err)
		}
		got[eventType] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read target events: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("target event types = %#v, want %#v", got, want)
	}
	for eventType := range want {
		if !got[eventType] {
			t.Errorf("missing target event type %q in %#v", eventType, got)
		}
	}
}
func TestVPSStateRepairTargetUnknownDependencyRequiresConfirmation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresTargetRepository(pool)

	if _, err := pool.Exec(ctx, `
		insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ('vps_target_unknown', 'Unknown dependency VPS', 'active', 'idle')`); err != nil {
		t.Fatalf("insert VPS: %v", err)
	}
	target, err := repo.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "Unknown dependency target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "unknown-dependency.example.test",
		ExecutionMonitoringInstanceLabels: []string{"edge"},
		Labels:                            []string{},
		RunStatus:                         targets.RunStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into asset_services (service_id, vps_id, target_id, name, service_type, status)
		values ('svc_target_unknown', 'vps_target_unknown', $1, 'unknown service', 'web', 'unknown')`, target.TargetID); err != nil {
		t.Fatalf("insert unknown service dependency: %v", err)
	}

	review, err := repo.GetTargetLifecycleReview(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTargetLifecycleReview: %v", err)
	}
	if len(review.DependencyImpacts) != 1 || review.DependencyImpacts[0].Classification != assetlinks.DependencyNeedsConfirmation {
		t.Fatalf("dependency impacts = %#v, want one needs-confirmation dependency", review.DependencyImpacts)
	}
	if _, err := repo.PauseTargetRun(ctx, target.TargetID); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("PauseTargetRun without unknown-dependency confirmation = %v, want confirmation required", err)
	}
	assertTargetRunStatus(t, ctx, pool, target.TargetID, targets.RunStatusEnabled)
	assertTargetEventCount(t, ctx, pool, target.TargetID, 0)

	if _, err := repo.PauseTargetRun(ctx, target.TargetID, assetlinks.GlobalActionConfirmation{
		PreviewDigest:       review.PreviewDigest,
		ConfirmSharedImpact: true,
	}); err != nil {
		t.Fatalf("PauseTargetRun with explicit unknown-dependency confirmation: %v", err)
	}
	assertTargetRunStatus(t, ctx, pool, target.TargetID, targets.RunStatusPaused)
	assertTargetEventCount(t, ctx, pool, target.TargetID, 1)
}

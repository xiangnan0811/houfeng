package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"

	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairSharedTargetArchiveSerializesAgainstNewParentDependency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)

	for _, order := range []struct {
		name        string
		writerFirst bool
	}{
		{name: "new parent dependency first", writerFirst: true},
		{name: "global Target archive first", writerFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			vpsA := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "target-graph-a-"+order.name, vpsassets.LifecycleActive)
			vpsB := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "target-graph-b-"+order.name, vpsassets.LifecycleActive)
			vpsC := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "target-graph-c-"+order.name, vpsassets.LifecycleActive)
			target := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "target-graph-"+strings.ReplaceAll(order.name, " ", "-"))
			if _, err := targetRepo.PauseTargetRun(ctx, target.TargetID); err != nil {
				t.Fatalf("pause shared Target before linking dependencies: %v", err)
			}
			createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsA.VPSID, target.TargetID, "target-graph-a", assetservices.ServiceStatusActive)
			createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsB.VPSID, target.TargetID, "target-graph-b", assetservices.ServiceStatusActive)
			review, err := targetRepo.GetTargetLifecycleReview(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTargetLifecycleReview before C dependency: %v", err)
			}
			if len(review.DependencyImpacts) != 2 {
				t.Fatalf("shared Target initial dependencies = %#v, want A and B", review.DependencyImpacts)
			}
			archive := func(actionCtx context.Context) error {
				_, err := targetRepo.ArchiveTarget(actionCtx, target.TargetID, assetlinks.GlobalActionConfirmation{
					PreviewDigest:       review.PreviewDigest,
					ConfirmSharedImpact: true,
				})
				return err
			}
			addC := func(writeCtx context.Context) error {
				_, err := serviceRepo.CreateAssetService(writeCtx, assetservices.CreateInput{
					VPSID:       vpsC.VPSID,
					TargetID:    new(target.TargetID),
					Name:        "target graph C dependency",
					ServiceType: assetservices.ServiceTypeWeb,
					Status:      assetservices.ServiceStatusActive,
				})
				return err
			}
			var firstErr, secondErr error
			if order.writerFirst {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, addC, archive)
				if firstErr != nil || !errors.Is(secondErr, assetlifecycle.ErrStaleCancellationPreview) {
					t.Fatalf("C-dependency-first serialization = add error %v, archive error %v, want archive stale", firstErr, secondErr)
				}
			} else {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, addC)
				if firstErr != nil || !errors.Is(secondErr, targets.ErrTargetMetadataConflict) {
					t.Fatalf("Target-archive-first serialization = archive error %v, add error %v, want active relation rejected", firstErr, secondErr)
				}
			}
			gotTarget, err := targetRepo.GetTarget(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTarget after graph race: %v", err)
			}
			var dependencies int
			if err := pool.QueryRow(ctx, `select count(*) from asset_service_associations where target_id = $1 and ended_at is null`, target.TargetID).Scan(&dependencies); err != nil {
				t.Fatalf("count shared Target dependencies after graph race: %v", err)
			}
			if order.writerFirst {
				if gotTarget.LifecycleStatus != targets.LifecycleActive || gotTarget.RunStatus != targets.RunStatusPaused || dependencies != 3 {
					t.Fatalf("dependency-first result = Target %q with %d dependencies, want paused with 3", gotTarget.RunStatus, dependencies)
				}
			} else if gotTarget.LifecycleStatus != targets.LifecycleRetired || gotTarget.RunStatus != targets.RunStatusPaused || dependencies != 2 {
				t.Fatalf("archive-first result = Target %q with %d dependencies, want archived with 2", gotTarget.RunStatus, dependencies)
			}
		})
	}
}

func TestVPSStateRepairObjectStatusRemainsIndependentOfTargetRetirement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)

	for _, order := range []struct {
		name            string
		correctionFirst bool
	}{
		{name: "unknown dependency corrected first", correctionFirst: true},
		{name: "Target archived before correction", correctionFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			parent := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "unknown-target-race-"+strings.ReplaceAll(order.name, " ", "-"), vpsassets.LifecycleActive)
			target := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "unknown-target-"+strings.ReplaceAll(order.name, " ", "-"))
			if _, err := targetRepo.PauseTargetRun(ctx, target.TargetID); err != nil {
				t.Fatalf("pause Target before dependency creation: %v", err)
			}
			service := createVPSStateRepairCancellationService(t, ctx, serviceRepo, parent.VPSID, target.TargetID, "unknown relation", assetservices.ServiceStatusUnknown)
			review, err := targetRepo.GetTargetLifecycleReview(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTargetLifecycleReview with unknown dependency: %v", err)
			}
			if len(review.DependencyImpacts) != 1 || review.DependencyImpacts[0].Classification != assetlinks.DependencyCurrent {
				t.Fatalf("unknown object's current association = %#v, want current", review.DependencyImpacts)
			}
			archive := func(actionCtx context.Context) error {
				_, err := targetRepo.ArchiveTarget(actionCtx, target.TargetID, assetlinks.GlobalActionConfirmation{
					PreviewDigest:       review.PreviewDigest,
					ConfirmSharedImpact: true,
				})
				return err
			}
			correct := func(writeCtx context.Context) error {
				_, err := serviceRepo.UpdateStatus(writeCtx, service.ServiceID, assetservices.ServiceStatusActive, "correct unknown dependency")
				return err
			}
			var firstErr, secondErr error
			if order.correctionFirst {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, correct, archive)
				if firstErr != nil || secondErr != nil {
					t.Fatalf("independent object correction then retirement = %v, %v", firstErr, secondErr)
				}
			} else {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, correct)
				if firstErr != nil || secondErr != nil {
					t.Fatalf("retirement then independent object correction = %v, %v", firstErr, secondErr)
				}
			}
			gotTarget, err := targetRepo.GetTarget(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTarget after unknown dependency race: %v", err)
			}
			gotServices, err := serviceRepo.ListAssetServicesForVPS(ctx, parent.VPSID)
			if err != nil {
				t.Fatalf("ListAssetServicesForVPS after unknown dependency race: %v", err)
			}
			if len(gotServices) != 1 {
				t.Fatalf("services after unknown dependency race = %#v, want one service", gotServices)
			}
			if gotTarget.LifecycleStatus != targets.LifecycleRetired || gotTarget.RunStatus != targets.RunStatusPaused || gotServices[0].Status != assetservices.ServiceStatusActive {
				t.Fatalf("independent facts: Target %+v, object status %q, want retired/paused and active object", gotTarget, gotServices[0].Status)
			}
		})
	}
}

func createVPSStateRepairCancellationVPS(t *testing.T, ctx context.Context, repo *PostgresVPSAssetRepository, name string, lifecycle vpsassets.LifecycleStatus) vpsassets.Record {
	t.Helper()
	record, err := repo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     name,
		LifecycleStatus: lifecycle,
		UsageTags:       []string{"闲置"},
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset %q: %v", name, err)
	}
	return record
}

func createVPSStateRepairCancellationMI(t *testing.T, ctx context.Context, repo *PostgresMonitoringInstanceRepository, name string) monitoringinstances.Record {
	t.Helper()
	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     name,
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance %q: %v", name, err)
	}
	return record
}

func createVPSStateRepairCancellationTarget(t *testing.T, ctx context.Context, repo *PostgresTargetRepository, name string) targets.TargetRecord {
	t.Helper()
	record, err := repo.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              name,
		TargetType:                        targets.TargetTypeService,
		Host:                              strings.ReplaceAll(name, "_", "-") + ".example.test",
		ExecutionMonitoringInstanceLabels: []string{"edge"},
		RunStatus:                         targets.RunStatusEnabled,
		Labels:                            []string{},
	})
	if err != nil {
		t.Fatalf("CreateTarget %q: %v", name, err)
	}
	return record
}

func createVPSStateRepairCancellationService(t *testing.T, ctx context.Context, repo *PostgresAssetServiceRepository, vpsID, targetID, name string, status assetservices.ServiceStatus) assetservices.Record {
	t.Helper()
	record, err := repo.CreateAssetService(ctx, assetservices.CreateInput{
		VPSID:       vpsID,
		TargetID:    new(targetID),
		Name:        name,
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      status,
	})
	if err != nil {
		t.Fatalf("CreateAssetService %q: %v", name, err)
	}
	return record
}

func cancellationInputForStateRepairDigest(digest string) assetlifecycle.ApplyCancellationInput {
	return assetlifecycle.ApplyCancellationInput{
		Reason:             "confirm cancellation fixture",
		VPSLifecycleStatus: vpsassets.LifecycleCancelled,
		PreviewDigest:      digest,
	}
}

func assertVPSStateRepairCancellationVPSLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, want vpsassets.LifecycleStatus) {
	t.Helper()
	var got vpsassets.LifecycleStatus
	if err := pool.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id = $1`, vpsID).Scan(&got); err != nil {
		t.Fatalf("read VPS %s lifecycle: %v", vpsID, err)
	}
	if got != want {
		t.Fatalf("VPS %s lifecycle = %q, want %q", vpsID, got, want)
	}
}

func assertVPSStateRepairCancellationImpactClasses(t *testing.T, impacts []assetlinks.DependencyImpact, objectType, objectID string, want map[string]string) {
	t.Helper()
	got := make(map[string]string)
	for _, impact := range impacts {
		if impact.ObjectType != objectType || impact.ObjectID != objectID {
			continue
		}
		if prior, ok := got[impact.VPSID]; ok && prior != impact.Classification {
			t.Fatalf("dependency %s for VPS %s has inconsistent classifications %q and %q", objectID, impact.VPSID, prior, impact.Classification)
		}
		got[impact.VPSID] = impact.Classification
	}
	if len(got) != len(want) {
		t.Fatalf("dependency classifications for %s/%s = %#v, want %#v", objectType, objectID, got, want)
	}
	for vpsID, classification := range want {
		if got[vpsID] != classification {
			t.Fatalf("dependency %s/%s for VPS %s classified %q, want %q", objectType, objectID, vpsID, got[vpsID], classification)
		}
	}
}

func assertVPSStateRepairMIReconciliationPayload(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID string) {
	t.Helper()
	var payload map[string]any
	if err := pool.QueryRow(ctx, `
		select payload from state_change_events
		where object_type = 'monitoring_instance' and object_id = $1 and event_type = $2`,
		monitoringInstanceID, string(incidents.EventMonitoringInstanceRetirementReconciled)).Scan(&payload); err != nil {
		t.Fatalf("read retirement reconciliation payload: %v", err)
	}
	for key, want := range map[string]any{
		"prior_state":     monitoringinstances.LifecycleRetired,
		"resulting_state": monitoringinstances.LifecycleRetired,
		"retirement_monitoring_status_reconciled":   true,
		"retirement_binding_status_reconciled":      true,
		"retirement_enrollment_credentials_revoked": true,
		"retirement_sync_credential_revoked":        true,
		"retirement_pending_binding_cleared":        true,
		"retirement_pending_action_cleared":         true,
	} {
		if payload[key] != want {
			t.Fatalf("retirement reconciliation payload %s = %#v, want %#v (payload %#v)", key, payload[key], want, payload)
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal retirement reconciliation payload: %v", err)
	}
	if strings.Contains(string(encoded), "shared-cancellation-enrollment-token") || strings.Contains(string(encoded), "fingerprint-pending") {
		t.Fatalf("retirement reconciliation event exposed secret state: %s", encoded)
	}
}

func assertStateChangeEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, objectType, objectID, eventType string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
		select count(*) from state_change_events
		where object_type = $1 and object_id = $2 and event_type = $3`, objectType, objectID, eventType).Scan(&got); err != nil {
		t.Fatalf("count state change event %s/%s/%s: %v", objectType, objectID, eventType, err)
	}
	if got != want {
		t.Fatalf("state change event count for %s/%s/%s = %d, want %d", objectType, objectID, eventType, got, want)
	}
}

func raceVPSStateRepairArchiveWithWriter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lifecycleRepo *PostgresAssetLifecycleRepository, vps vpsassets.Record, writerFirst bool, writer func(context.Context) error) (error, error) {
	t.Helper()
	archive := func(actionCtx context.Context) error {
		_, err := lifecycleRepo.ApplyVPSArchive(actionCtx, vps.VPSID, assetlifecycle.ApplyArchiveInput{
			ConfirmationName: vps.DisplayName,
			Reason:           "serialize archive with graph writer",
		})
		return err
	}
	if writerFirst {
		writeErr, archiveErr := raceVPSStateRepairOperationsInOrder(t, ctx, pool, writer, archive)
		return archiveErr, writeErr
	}
	archiveErr, writeErr := raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, writer)
	return archiveErr, writeErr
}

func raceVPSStateRepairOperationsInOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, first, second func(context.Context) error) (error, error) {
	t.Helper()
	holder, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatalf("begin ordered lifecycle graph lock: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- first(ctx) }()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 1); err != nil {
		t.Fatalf("first lifecycle operation did not wait for graph lock: %v", err)
	}
	go func() { secondDone <- second(ctx) }()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 2); err != nil {
		t.Fatalf("second lifecycle operation did not wait for graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release ordered lifecycle graph lock: %v", err)
	}
	return <-firstDone, <-secondDone
}

func assertVPSStateRepairServiceDomainArchiveRaceResult(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID, table string, archiveErr, writeErr error) {
	t.Helper()
	if archiveErr != nil {
		t.Fatalf("archive vs %s create failed: %v", table, archiveErr)
	}
	var lifecycle vpsassets.LifecycleStatus
	if err := pool.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id = $1`, vpsID).Scan(&lifecycle); err != nil {
		t.Fatalf("read raced %s parent lifecycle: %v", table, err)
	}
	var related int
	if err := pool.QueryRow(ctx, `select count(*) from `+table+` where vps_id = $1`, vpsID).Scan(&related); err != nil {
		t.Fatalf("count raced %s rows: %v", table, err)
	}
	switch {
	case writeErr == nil:
		if lifecycle != vpsassets.LifecycleArchived || related != 1 {
			t.Fatalf("writer-first %s result = lifecycle %q, relations %d, want archived parent with retained relation", table, lifecycle, related)
		}
	case errors.Is(writeErr, vpsassets.ErrVPSAssetReadonly):
		if lifecycle != vpsassets.LifecycleArchived || related != 0 {
			t.Fatalf("archive-first %s result = lifecycle %q, relations %d, want archived parent without relation", table, lifecycle, related)
		}
	default:
		t.Fatalf("%s race writer error = %v, want success or terminal-parent conflict", table, writeErr)
	}

}

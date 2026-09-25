package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairDependencyStatusCorrectionWritesAuditAndSkipsNoOp(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "dependency status audit")
	target := createVPSStateRepairDependencyStatusTarget(t, ctx, pool, targets.RunStatusEnabled)

	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)
	service, err := serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{
		VPSID:       vps.VPSID,
		TargetID:    &target.TargetID,
		Name:        "Unknown status service",
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      assetservices.ServiceStatusUnknown,
		URL:         "https://service.example.test",
		Labels:      []string{"prod"},
		Note:        "preserve service metadata",
	})
	if err != nil {
		t.Fatalf("create unknown service: %v", err)
	}
	serviceID := service.ServiceID
	domain, err := domainRepo.CreateAssetDomain(ctx, assetdomains.CreateInput{
		VPSID:        vps.VPSID,
		ServiceID:    &serviceID,
		TargetID:     &target.TargetID,
		DomainName:   "unknown.example.test",
		Purpose:      "primary website",
		Status:       assetdomains.DomainStatusUnknown,
		Registrar:    "example registrar",
		HTTPSEnabled: true,
		Labels:       []string{"prod"},
		Note:         "preserve domain metadata",
	})
	if err != nil {
		t.Fatalf("create unknown domain: %v", err)
	}

	correctedService, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusActive, "verified service owner")
	if err != nil {
		t.Fatalf("correct unknown service status: %v", err)
	}
	if correctedService.Status != assetservices.ServiceStatusActive || correctedService.Name != service.Name || correctedService.TargetID == nil || *correctedService.TargetID != target.TargetID || correctedService.URL != service.URL || correctedService.Note != service.Note {
		t.Fatalf("corrected service = %#v; status should change while its other fields remain unchanged", correctedService)
	}
	correctedDomain, err := domainRepo.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusPaused, "paused after domain review")
	if err != nil {
		t.Fatalf("correct unknown domain status: %v", err)
	}
	if correctedDomain.Status != assetdomains.DomainStatusPaused || correctedDomain.DomainName != domain.DomainName || correctedDomain.ServiceID == nil || *correctedDomain.ServiceID != service.ServiceID || correctedDomain.TargetID == nil || *correctedDomain.TargetID != target.TargetID || correctedDomain.Purpose != domain.Purpose || correctedDomain.Registrar != domain.Registrar || !correctedDomain.HTTPSEnabled || correctedDomain.Note != domain.Note {
		t.Fatalf("corrected domain = %#v; status should change while its other fields remain unchanged", correctedDomain)
	}

	noOpService, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusActive, "repeat confirmation")
	if err != nil {
		t.Fatalf("repeat unchanged service status: %v", err)
	}
	if !noOpService.UpdatedAt.Equal(correctedService.UpdatedAt) {
		t.Fatalf("unchanged service UpdatedAt = %v, want original correction timestamp %v", noOpService.UpdatedAt, correctedService.UpdatedAt)
	}

	rows, err := pool.Query(ctx, `
		select a.action_type, a.status, a.reason, a.summary,
		       s.object_type, s.object_id, s.step_type, s.status,
		       s.before_state, s.after_state, s.message
		from asset_lifecycle_actions a
		join asset_lifecycle_action_steps s on s.action_id = a.action_id
		where a.vps_id = $1 and a.action_type = $2
		order by s.object_type, s.object_id`,
		vps.VPSID,
		assetlifecycle.ActionTypeCorrectDependencyStatus,
	)
	if err != nil {
		t.Fatalf("query dependency correction audit: %v", err)
	}
	defer rows.Close()
	seen := map[string]string{
		service.ServiceID: string(assetservices.ServiceStatusActive),
		domain.DomainID:   string(assetdomains.DomainStatusPaused),
	}
	count := 0
	for rows.Next() {
		var actionType, actionStatus, actionReason string
		var summaryJSON, beforeJSON, afterJSON []byte
		var objectType, objectID, stepType, stepStatus, message string
		if err := rows.Scan(&actionType, &actionStatus, &actionReason, &summaryJSON, &objectType, &objectID, &stepType, &stepStatus, &beforeJSON, &afterJSON, &message); err != nil {
			t.Fatalf("scan dependency correction audit: %v", err)
		}
		count++
		var summary, beforeState, afterState map[string]any
		if err := json.Unmarshal(summaryJSON, &summary); err != nil {
			t.Fatalf("decode action summary: %v", err)
		}
		if err := json.Unmarshal(beforeJSON, &beforeState); err != nil {
			t.Fatalf("decode step before state: %v", err)
		}
		if err := json.Unmarshal(afterJSON, &afterState); err != nil {
			t.Fatalf("decode step after state: %v", err)
		}
		wantStatus, ok := seen[objectID]
		if !ok {
			t.Errorf("unexpected audited object id %q", objectID)
			continue
		}
		wantBefore := "unknown"
		wantReason := "verified service owner"
		if objectID == domain.DomainID {
			wantReason = "paused after domain review"
		}
		if actionType != string(assetlifecycle.ActionTypeCorrectDependencyStatus) || actionStatus != assetlifecycle.ActionStatusCompleted || actionReason != wantReason {
			t.Errorf("action for %s = type:%q status:%q reason:%q", objectID, actionType, actionStatus, actionReason)
		}
		if objectType != map[string]string{service.ServiceID: "service", domain.DomainID: "domain"}[objectID] || stepType != "dependency_status" || stepStatus != assetlifecycle.StepStatusCompleted || message != wantReason {
			t.Errorf("step for %s = object:%q type:%q status:%q message:%q", objectID, objectType, stepType, stepStatus, message)
		}
		if summary["object_type"] != objectType || summary["object_id"] != objectID || summary["before_status"] != wantBefore || summary["after_status"] != wantStatus {
			t.Errorf("summary for %s = %#v", objectID, summary)
		}
		if beforeState["status"] != wantBefore || afterState["status"] != wantStatus {
			t.Errorf("step states for %s = before:%#v after:%#v", objectID, beforeState, afterState)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate dependency correction audit: %v", err)
	}
	if count != 2 {
		t.Fatalf("correction action/step rows = %d, want exactly two and no duplicate for unchanged status", count)
	}
}

func TestVPSStateRepairDependencyStatusCorrectionRespectsVPSAndTargetGuards(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)

	terminalVPS := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "terminal dependency correction")
	service, err := serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{VPSID: terminalVPS.VPSID, Name: "Paused service", Status: assetservices.ServiceStatusPaused})
	if err != nil {
		t.Fatalf("create paused service: %v", err)
	}
	domain, err := domainRepo.CreateAssetDomain(ctx, assetdomains.CreateInput{VPSID: terminalVPS.VPSID, DomainName: "paused.example.test", Status: assetdomains.DomainStatusPaused})
	if err != nil {
		t.Fatalf("create paused domain: %v", err)
	}
	if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = 'cancelled', usage_status = 'idle', renewal_decision = 'cancel' where vps_id = $1`, terminalVPS.VPSID); err != nil {
		t.Fatalf("make parent VPS terminal: %v", err)
	}
	if _, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusActive, "reactivate"); !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
		t.Fatalf("activate dependency on terminal VPS error = %v, want ErrVPSAssetReadonly", err)
	}
	if _, err := domainRepo.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusActive, "reactivate"); !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
		t.Fatalf("activate domain on terminal VPS error = %v, want ErrVPSAssetReadonly", err)
	}
	if record, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusRetired, "retire historical service"); err != nil || record.Status != assetservices.ServiceStatusRetired {
		t.Fatalf("retire service on terminal VPS = %#v, %v; want success", record, err)
	}
	if record, err := domainRepo.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusRetired, "retire historical domain"); err != nil || record.Status != assetdomains.DomainStatusRetired {
		t.Fatalf("retire domain on terminal VPS = %#v, %v; want success", record, err)
	}

	activeVPS := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "archived target dependency correction")
	archivedTarget := createVPSStateRepairDependencyStatusTarget(t, ctx, pool, targets.RunStatusArchived)
	service, err = serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{VPSID: activeVPS.VPSID, TargetID: &archivedTarget.TargetID, Name: "Paused target service", Status: assetservices.ServiceStatusPaused})
	if err != nil {
		t.Fatalf("create paused service for archived target: %v", err)
	}
	domain, err = domainRepo.CreateAssetDomain(ctx, assetdomains.CreateInput{VPSID: activeVPS.VPSID, TargetID: &archivedTarget.TargetID, DomainName: "target-paused.example.test", Status: assetdomains.DomainStatusPaused})
	if err != nil {
		t.Fatalf("create paused domain for archived target: %v", err)
	}
	if _, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusActive, "activate archived target"); !errors.Is(err, targets.ErrTargetMetadataConflict) {
		t.Fatalf("activate dependency on archived target error = %v, want ErrTargetMetadataConflict", err)
	}
	if _, err := domainRepo.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusActive, "activate archived target"); !errors.Is(err, targets.ErrTargetMetadataConflict) {
		t.Fatalf("activate domain on archived target error = %v, want ErrTargetMetadataConflict", err)
	}
	if record, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusRetired, "retire archived target service"); err != nil || record.Status != assetservices.ServiceStatusRetired {
		t.Fatalf("retire service on archived target = %#v, %v; want success", record, err)
	}
	if record, err := domainRepo.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusRetired, "retire archived target domain"); err != nil || record.Status != assetdomains.DomainStatusRetired {
		t.Fatalf("retire domain on archived target = %#v, %v; want success", record, err)
	}
}

func TestVPSStateRepairDependencyStatusCorrectionRechecksParentAfterGraphWait(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "dependency graph lock correction")
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	service, err := serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, Name: "Paused service", Status: assetservices.ServiceStatusPaused})
	if err != nil {
		t.Fatalf("create paused service: %v", err)
	}

	holder, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin graph lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if err := lockAssetGraph(ctx, holder); err != nil {
		t.Fatalf("lock asset graph: %v", err)
	}

	updateDone := make(chan error, 1)
	go func() {
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer updateCancel()
		_, updateErr := serviceRepo.UpdateStatus(updateCtx, service.ServiceID, assetservices.ServiceStatusActive, "late activation")
		updateDone <- updateErr
	}()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 1); err != nil {
		t.Fatalf("status correction did not wait for the graph lock: %v", err)
	}
	if _, err := holder.Exec(ctx, `update vps_assets set lifecycle_status = 'cancelled', usage_status = 'idle', renewal_decision = 'cancel' where vps_id = $1`, vps.VPSID); err != nil {
		t.Fatalf("make parent VPS terminal under graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit terminal parent VPS: %v", err)
	}
	if err := <-updateDone; !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
		t.Fatalf("status correction after graph wait error = %v, want ErrVPSAssetReadonly", err)
	}

	var status string
	if err := pool.QueryRow(ctx, `select status from asset_services where service_id = $1`, service.ServiceID).Scan(&status); err != nil {
		t.Fatalf("read service status after rejected correction: %v", err)
	}
	if status != string(assetservices.ServiceStatusPaused) {
		t.Fatalf("service status after rejected correction = %q, want paused", status)
	}
	var actions int
	if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, vps.VPSID, assetlifecycle.ActionTypeCorrectDependencyStatus).Scan(&actions); err != nil {
		t.Fatalf("count correction actions: %v", err)
	}
	if actions != 0 {
		t.Fatalf("correction audit actions = %d, want none for rejected activation", actions)
	}
}

func TestVPSStateRepairDependencyStatusCorrectionRollsBackWhenAuditStepFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "dependency audit rollback")
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)
	service, err := serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, Name: "Paused service", Status: assetservices.ServiceStatusPaused})
	if err != nil {
		t.Fatalf("create paused service: %v", err)
	}
	domain, err := domainRepo.CreateAssetDomain(ctx, assetdomains.CreateInput{VPSID: vps.VPSID, DomainName: "rollback.example.test", Status: assetdomains.DomainStatusPaused})
	if err != nil {
		t.Fatalf("create paused domain: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		create function reject_dependency_status_step() returns trigger
		language plpgsql as $$ begin raise exception 'injected lifecycle step failure'; end $$`); err != nil {
		t.Fatalf("create lifecycle step failure trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		create trigger reject_dependency_status_step
		before insert on asset_lifecycle_action_steps
		for each row execute function reject_dependency_status_step()`); err != nil {
		t.Fatalf("create lifecycle step failure trigger: %v", err)
	}

	if _, err := serviceRepo.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusRetired, "retire service"); err == nil {
		t.Fatal("service status correction succeeded despite lifecycle step audit failure")
	}
	if _, err := domainRepo.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusRetired, "retire domain"); err == nil {
		t.Fatal("domain status correction succeeded despite lifecycle step audit failure")
	}
	var serviceStatus, domainStatus string
	if err := pool.QueryRow(ctx, `select status from asset_services where service_id = $1`, service.ServiceID).Scan(&serviceStatus); err != nil {
		t.Fatalf("read service status after audit failure: %v", err)
	}
	if err := pool.QueryRow(ctx, `select status from asset_domains where domain_id = $1`, domain.DomainID).Scan(&domainStatus); err != nil {
		t.Fatalf("read domain status after audit failure: %v", err)
	}
	if serviceStatus != string(assetservices.ServiceStatusPaused) || domainStatus != string(assetdomains.DomainStatusPaused) {
		t.Fatalf("statuses after audit failure = service:%q domain:%q, want both paused", serviceStatus, domainStatus)
	}
	var actions int
	if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, vps.VPSID, assetlifecycle.ActionTypeCorrectDependencyStatus).Scan(&actions); err != nil {
		t.Fatalf("count correction actions after audit failure: %v", err)
	}
	if actions != 0 {
		t.Fatalf("correction audit actions after failure = %d, want none", actions)
	}
}

func createVPSStateRepairDependencyStatusVPS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) vpsassets.Record {
	t.Helper()
	vps, err := NewPostgresVPSAssetRepository(pool).CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     name,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("create VPS %q: %v", name, err)
	}
	return vps
}

func createVPSStateRepairDependencyStatusTarget(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status string) targets.TargetRecord {
	t.Helper()
	target, err := NewPostgresTargetRepository(pool).CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "Dependency correction target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "dependency-status.example.test",
		ExecutionMonitoringInstanceLabels: []string{},
		RunStatus:                         status,
		Labels:                            []string{},
	})
	if err != nil {
		t.Fatalf("create target with status %q: %v", status, err)
	}
	return target
}

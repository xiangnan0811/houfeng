package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairRuntimeACLDependencyCorrectionAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "vps-state-runtime-acl", 2)
	vps := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "runtime correction")
	target := createVPSStateRepairDependencyStatusTarget(t, ctx, pool, targets.RunStatusEnabled)
	services := NewPostgresAssetServiceRepository(pool)
	domains := NewPostgresAssetDomainRepository(pool)
	service, err := services.CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, TargetID: &target.TargetID, Name: "runtime service", ServiceType: assetservices.ServiceTypeWeb, Status: assetservices.ServiceStatusUnknown, URL: "https://runtime.example.test", Labels: []string{"preserve"}, Note: "service metadata"})
	if err != nil {
		t.Fatal(err)
	}
	domain, err := domains.CreateAssetDomain(ctx, assetdomains.CreateInput{VPSID: vps.VPSID, TargetID: &target.TargetID, DomainName: "runtime.example.test", Status: assetdomains.DomainStatusUnknown, Purpose: "runtime correction", Registrar: "registrar", HTTPSEnabled: true, Labels: []string{"preserve"}, Note: "domain metadata"})
	if err != nil {
		t.Fatal(err)
	}
	correctedService, err := services.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusRetired, "verified service stopped")
	if err != nil {
		t.Fatalf("runtime service correction: %v", err)
	}
	wantService := service
	wantService.Status = assetservices.ServiceStatusRetired
	wantService.UpdatedAt = correctedService.UpdatedAt
	if !reflect.DeepEqual(correctedService, wantService) {
		t.Fatalf("service correction changed metadata: got %#v want %#v", correctedService, wantService)
	}
	correctedDomain, err := domains.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusPaused, "verified domain paused")
	if err != nil {
		t.Fatalf("runtime domain correction: %v", err)
	}
	wantDomain := domain
	wantDomain.Status = assetdomains.DomainStatusPaused
	wantDomain.UpdatedAt = correctedDomain.UpdatedAt
	if !reflect.DeepEqual(correctedDomain, wantDomain) {
		t.Fatalf("domain correction changed metadata: got %#v want %#v", correctedDomain, wantDomain)
	}
	if _, err := services.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusRetired, "repeat"); err != nil {
		t.Fatal(err)
	}
	if _, err := domains.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusPaused, "repeat"); err != nil {
		t.Fatal(err)
	}
	var auditCount int
	// Audit tables are append-only for runtime; inspect persisted evidence as the fixture owner.
	err = fixture.db.QueryRow(ctx, `select count(*) from asset_lifecycle_actions a join asset_lifecycle_action_steps s on s.action_id=a.action_id where a.vps_id=$1 and a.action_type=$2 and a.status='completed' and s.before_state->>'status'='unknown' and ((s.object_id=$3 and s.after_state->>'status'='retired' and a.reason='verified service stopped') or (s.object_id=$4 and s.after_state->>'status'='paused' and a.reason='verified domain paused'))`, vps.VPSID, assetlifecycle.ActionTypeCorrectDependencyStatus, service.ServiceID, domain.DomainID).Scan(&auditCount)
	if err != nil || auditCount != 2 {
		t.Fatalf("runtime correction audit count=%d err=%v", auditCount, err)
	}
	empty := createVPSStateRepairDependencyStatusVPS(t, ctx, pool, "runtime cancellation")
	lifecycle := NewPostgresAssetLifecycleRepository(pool)
	preview, err := lifecycle.GetVPSCancellationPreview(ctx, empty.VPSID)
	if err != nil {
		t.Fatalf("runtime cancellation preview: %v", err)
	}
	_, err = lifecycle.ApplyVPSCancellation(ctx, empty.VPSID, assetlifecycle.ApplyCancellationInput{Reason: "runtime cancellation verified", VPSLifecycleStatus: vpsassets.LifecycleCancelled, PreviewDigest: preview.PreviewDigest})
	if err != nil {
		t.Fatalf("runtime cancellation: %v", err)
	}
	after, err := lifecycle.GetVPSCancellationPreview(ctx, empty.VPSID)
	if err != nil || after.VPS.LifecycleStatus != vpsassets.LifecycleCancelled {
		t.Fatalf("runtime cancellation readback=%#v err=%v", after, err)
	}
	admin := fixture.openDirectRolePool(t, ctx, fixture.admin, "vps-state-admin-denial", 1)
	for _, table := range []string{"asset_services", "asset_domains"} {
		_, err := pool.Exec(ctx, "delete from public."+table+" where false")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("runtime DELETE %s: %v, want 42501", table, err)
		}
		_, err = admin.Exec(ctx, "update public."+table+" set status=status where false")
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("admin UPDATE %s: %v, want 42501", table, err)
		}
	}
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator, "vps-state-audit-fault", 1)
	if _, err := migrator.Exec(ctx, `create function public.reject_dependency_status_step() returns trigger language plpgsql as $$ begin raise exception 'injected lifecycle step failure'; end $$; create trigger reject_dependency_status_step before insert on public.asset_lifecycle_action_steps for each row execute function public.reject_dependency_status_step()`); err != nil {
		t.Fatal(err)
	}
	for _, correct := range []func() error{
		func() error {
			_, err := services.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusPaused, "audit must rollback")
			return err
		},
		func() error {
			_, err := domains.UpdateStatus(ctx, domain.DomainID, assetdomains.DomainStatusRetired, "audit must rollback")
			return err
		},
	} {
		var pgErr *pgconn.PgError
		if err := correct(); !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
			t.Fatalf("audit injection: %v, want P0001", err)
		}
	}
	var serviceStatus, domainStatus string
	if err := pool.QueryRow(ctx, `select s.status, d.status from asset_services s, asset_domains d where s.service_id=$1 and d.domain_id=$2`, service.ServiceID, domain.DomainID).Scan(&serviceStatus, &domainStatus); err != nil {
		t.Fatal(err)
	}
	if serviceStatus != "retired" || domainStatus != "paused" {
		t.Fatalf("audit failure mutated business state: %s/%s", serviceStatus, domainStatus)
	}
	var actions int
	if err := fixture.db.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id=$1 and action_type=$2`, vps.VPSID, assetlifecycle.ActionTypeCorrectDependencyStatus).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != 2 {
		t.Fatalf("audit failure or same-state correction left extra actions: %d", actions)
	}
}

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairGraphWritersRecheckTerminalVPSAfterGraphWait(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	subscriptionRepo := NewPostgresSubscriptionRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)

	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Graph writer lifecycle wait",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}

	holder, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin graph lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if err := lockAssetGraph(ctx, holder); err != nil {
		t.Fatalf("lock asset graph: %v", err)
	}

	writers := []struct {
		name   string
		create func(context.Context) error
	}{
		{
			name: "subscription",
			create: func(createCtx context.Context) error {
				_, err := subscriptionRepo.CreateSubscription(createCtx, subscriptions.CreateInput{
					VPSID:         vps.VPSID,
					Price:         12,
					Currency:      "USD",
					BillingCycle:  "monthly",
					BillingMonths: 1,
					Status:        subscriptions.StatusActive,
				})
				return err
			},
		},
		{
			name: "service",
			create: func(createCtx context.Context) error {
				_, err := serviceRepo.CreateAssetService(createCtx, assetservices.CreateInput{
					VPSID:       vps.VPSID,
					Name:        "late active dependency",
					ServiceType: assetservices.ServiceTypeWeb,
					Status:      assetservices.ServiceStatusActive,
				})
				return err
			},
		},
		{
			name: "domain",
			create: func(createCtx context.Context) error {
				_, err := domainRepo.CreateAssetDomain(createCtx, assetdomains.CreateInput{
					VPSID:      vps.VPSID,
					DomainName: "blocked.example.com",
					Status:     assetdomains.DomainStatusActive,
				})
				return err
			},
		},
	}
	type writerResult struct {
		name string
		err  error
	}
	createDone := make(chan writerResult, len(writers))
	for _, writer := range writers {
		writer := writer
		go func() {
			createCtx, createCancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer createCancel()
			createDone <- writerResult{name: writer.name, err: writer.create(createCtx)}
		}()
	}

	if err := waitForBlockedLifecycleSessions(ctx, pool, len(writers)); err != nil {
		t.Fatalf("asset relationship creates did not wait for the graph lock: %v", err)
	}
	select {
	case createErr := <-createDone:
		t.Fatalf("asset relationship create completed while graph lock was held: %v", createErr)
	default:
	}

	if _, err := holder.Exec(ctx, `update vps_assets set lifecycle_status = 'archived', archived_at=now(), renewal_decision = 'cancel', updated_at = now() where vps_id = $1`, vps.VPSID); err != nil {
		t.Fatalf("archive vps under graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit terminal vps state: %v", err)
	}

	for range writers {
		result := <-createDone
		if result.name == "subscription" {
			if result.err != nil {
				t.Errorf("supplemental archived billing after graph wait: %v", result.err)
			}
		} else if !errors.Is(result.err, vpsassets.ErrVPSAssetReadonly) {
			t.Errorf("%s current relationship after graph wait error=%v, want readonly", result.name, result.err)
		}
	}

	for _, table := range []string{"subscriptions", "asset_services", "asset_domains"} {
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from `+table+` where vps_id = $1`, vps.VPSID).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		want := 0
		if table == "subscriptions" {
			want = 1
		}
		if count != want {
			t.Errorf("%s rows for archived VPS = %d, want %d", table, count, want)
		}
	}
	stored, err := vpsRepo.GetVPSAsset(ctx, vps.VPSID)
	if err != nil || stored.LifecycleStatus != vpsassets.LifecycleArchived {
		t.Fatalf("billing revived archived VPS: %+v error=%v", stored, err)
	}
}

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
	createDone := make(chan error, len(writers))
	for _, writer := range writers {
		writer := writer
		go func() {
			createCtx, createCancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer createCancel()
			createDone <- writer.create(createCtx)
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

	if _, err := holder.Exec(ctx, `update vps_assets set lifecycle_status = 'cancelled', usage_status = 'idle', renewal_decision = 'cancel', updated_at = now() where vps_id = $1`, vps.VPSID); err != nil {
		t.Fatalf("cancel vps under graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit terminal vps state: %v", err)
	}

	for _, writer := range writers {
		if err := <-createDone; !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
			t.Errorf("%s create after graph wait error = %v, want ErrVPSAssetReadonly", writer.name, err)
		}
	}

	for _, table := range []string{"subscriptions", "asset_services", "asset_domains"} {
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from `+table+` where vps_id = $1`, vps.VPSID).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s rows for terminal VPS = %d, want 0", table, count)
		}
	}
}

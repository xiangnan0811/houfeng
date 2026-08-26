package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestCreateSubscriptionIdempotentReplayAfterLostResponseKeepsOneRow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporarySubscriptionIdempotencyPostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)

	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Idempotent Create",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset: %v", err)
	}

	input := subscriptions.CreateInput{
		VPSID:         vps.VPSID,
		Price:         12,
		Currency:      "USD",
		BillingMonths: 1,
		Note:          "lost-response retry",
	}
	const key = "lost-response-sub-001"

	first, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, input, key)
	if err != nil {
		t.Fatalf("first CreateSubscriptionIdempotent: %v", err)
	}
	if replayed {
		t.Fatal("first CreateSubscriptionIdempotent reported replay")
	}

	second, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, input, key)
	if err != nil {
		t.Fatalf("replay CreateSubscriptionIdempotent: %v", err)
	}
	if !replayed {
		t.Fatal("replay CreateSubscriptionIdempotent did not report replay")
	}
	if second.SubscriptionID != first.SubscriptionID {
		t.Fatalf("replayed id = %q, want %q", second.SubscriptionID, first.SubscriptionID)
	}

	conflict, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, subscriptions.CreateInput{
		VPSID:         vps.VPSID,
		Price:         24,
		Currency:      "USD",
		BillingMonths: 1,
	}, key)
	if !errors.Is(err, subscriptions.ErrIdempotencyKeyReused) {
		t.Fatalf("reused key error = %v, want %v (record=%#v replayed=%v)", err, subscriptions.ErrIdempotencyKeyReused, conflict, replayed)
	}

	var subscriptionCount int
	if err := pool.QueryRow(ctx, `select count(*) from subscriptions where vps_id = $1`, vps.VPSID).Scan(&subscriptionCount); err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if subscriptionCount != 1 {
		t.Fatalf("subscription rows = %d, want 1 after lost-response replay", subscriptionCount)
	}
	var idempotencyCount int
	if err := pool.QueryRow(ctx, `select count(*) from subscription_create_idempotency where idempotency_key = $1`, key).Scan(&idempotencyCount); err != nil {
		t.Fatalf("count idempotency rows: %v", err)
	}
	if idempotencyCount != 1 {
		t.Fatalf("idempotency rows = %d, want 1", idempotencyCount)
	}
}

func openTemporarySubscriptionIdempotencyPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for subscription create idempotency PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for subscription create idempotency PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_sub_idemp_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := storemigrate.Apply(ctx, testPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return testPool
}

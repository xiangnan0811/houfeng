package store

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestApplyVPSCancellationConcurrentWithSubscriptionInsertOverlapsWithoutDeadlock(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)

	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Lock Order Edge",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset: %v", err)
	}

	preview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview: %v", err)
	}

	holderReady := make(chan struct{})
	holderRelease := make(chan struct{})
	var releaseHolder sync.Once
	release := func() {
		releaseHolder.Do(func() { close(holderRelease) })
	}
	defer release()
	go func() {
		holderCtx, holderCancel := context.WithTimeout(ctx, 12*time.Second)
		defer holderCancel()
		tx, beginErr := pool.BeginTx(holderCtx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if beginErr != nil {
			t.Errorf("begin holder tx: %v", beginErr)
			close(holderReady)
			return
		}
		if _, lockErr := getLifecycleVPSAsset(holderCtx, tx, vps.VPSID, true); lockErr != nil {
			t.Errorf("lock VPS: %v", lockErr)
			_ = tx.Rollback(holderCtx)
			close(holderReady)
			return
		}
		close(holderReady)
		select {
		case <-holderRelease:
		case <-holderCtx.Done():
		}
		_ = tx.Rollback(holderCtx)
	}()

	select {
	case <-holderReady:
	case <-ctx.Done():
		t.Fatal("holder did not acquire VPS row lock before timeout")
	}

	applyDone := make(chan error, 1)
	insertDone := make(chan error, 1)
	go func() {
		applyCtx, applyCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer applyCancel()
		_, applyErr := lifecycleRepo.ApplyVPSCancellation(applyCtx, vps.VPSID, assetlifecycle.ApplyCancellationInput{
			Reason:             "lock-order regression",
			VPSLifecycleStatus: vpsassets.LifecycleCancelled,
			PreviewDigest:      preview.PreviewDigest,
		})
		applyDone <- applyErr
	}()
	go func() {
		insertCtx, insertCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer insertCancel()
		_, insertErr := subRepo.CreateSubscription(insertCtx, subscriptions.CreateInput{
			VPSID:         vps.VPSID,
			Price:         12,
			Currency:      "USD",
			BillingMonths: 1,
			Status:        subscriptions.StatusActive,
		})
		insertDone <- insertErr
	}()

	if err := waitForBlockedLifecycleSessions(ctx, pool, 2); err != nil {
		t.Fatalf("production transactions did not overlap on the holder lock: %v", err)
	}
	select {
	case applyErr := <-applyDone:
		t.Fatalf("ApplyVPSCancellation finished before holder release: %v", applyErr)
	case insertErr := <-insertDone:
		t.Fatalf("CreateSubscription finished before holder release: %v", insertErr)
	default:
	}

	release()

	if applyErr := <-applyDone; applyErr != nil {
		t.Fatalf("ApplyVPSCancellation after holder release: %v", applyErr)
	}
	if insertErr := <-insertDone; insertErr != nil {
		t.Fatalf("CreateSubscription after holder release: %v", insertErr)
	}

	var failedActions int
	if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and status = 'failed'`, vps.VPSID).Scan(&failedActions); err != nil {
		t.Fatalf("count failed actions: %v", err)
	}
	if failedActions != 0 {
		t.Fatalf("failed lifecycle actions = %d, want 0 after production apply path", failedActions)
	}
}

func waitForBlockedLifecycleSessions(ctx context.Context, pool *pgxpool.Pool, want int) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := pool.QueryRow(ctx, `
			select count(*)
			from pg_stat_activity
			where datname = current_database()
			  and pid <> pg_backend_pid()
			  and wait_event_type = 'Lock'
			  and state = 'active'`).Scan(&waiting)
		if err != nil {
			return err
		}
		if waiting >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for %d lock waiters", want)
}

func openTemporaryAssetLifecyclePostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for asset lifecycle PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for asset lifecycle PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_asset_life_%d_%d", time.Now().UnixNano(), os.Getpid())
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

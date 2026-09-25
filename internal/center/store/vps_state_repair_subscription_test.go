package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairSubscriptionOwnershipCannotMoveOrReplayAcrossVPS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)

	owner, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Subscription owner",
		LifecycleStatus: vpsassets.LifecycleIdle,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("create owner VPS: %v", err)
	}
	other, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Other subscription owner",
		LifecycleStatus: vpsassets.LifecycleIdle,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("create other VPS: %v", err)
	}

	input := subscriptions.NormalizeCreateInput(subscriptions.CreateInput{
		VPSID:         owner.VPSID,
		Price:         12,
		Currency:      "USD",
		BillingMonths: 1,
		RenewalMode:   string(subscriptions.RenewalModeManual),
		PaymentMethod: "card",
	})
	const key = "repair-sub-owner-001"
	created, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, input, key)
	if err != nil || replayed {
		t.Fatalf("initial create = (%#v, replayed %t, %v), want newly created row", created, replayed, err)
	}

	replay, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, input, key)
	if err != nil || !replayed || replay.SubscriptionID != created.SubscriptionID || replay.VPSID != owner.VPSID {
		t.Fatalf("same key/body replay = (%#v, replayed %t, %v), want original subscription and replayed=true", replay, replayed, err)
	}

	if _, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, subscriptions.CreateInput{
		VPSID:         owner.VPSID,
		Price:         24,
		Currency:      "USD",
		BillingMonths: 1,
		RenewalMode:   string(subscriptions.RenewalModeManual),
		PaymentMethod: "card",
	}, key); !errors.Is(err, subscriptions.ErrIdempotencyKeyReused) || replayed {
		t.Fatalf("same key with changed payload = replayed %t, error %v; want key-reuse conflict", replayed, err)
	}

	if _, err := subRepo.PatchSubscription(ctx, created.SubscriptionID, subscriptions.PatchInput{
		VPSID: subscriptions.PatchString(other.VPSID),
		Note:  subscriptions.PatchString("must not move"),
	}); !errors.Is(err, subscriptions.ErrSubscriptionOwnershipChangeForbidden) {
		t.Fatalf("PATCH changing VPS owner error = %v, want ownership conflict", err)
	}
	unchangedAt := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `update subscriptions set updated_at = $2 where subscription_id = $1`, created.SubscriptionID, unchangedAt); err != nil {
		t.Fatalf("seed subscription update timestamp: %v", err)
	}
	unchanged, err := subRepo.GetSubscription(ctx, created.SubscriptionID)
	if err != nil {
		t.Fatalf("read subscription after rejected move: %v", err)
	}
	if unchanged.VPSID != owner.VPSID || unchanged.Note != "" {
		t.Fatalf("rejected move changed subscription to VPS %q or note %q", unchanged.VPSID, unchanged.Note)
	}

	sameOwner, err := subRepo.PatchSubscription(ctx, created.SubscriptionID, subscriptions.PatchInput{
		VPSID: subscriptions.PatchString(" " + owner.VPSID + " "),
	})
	if err != nil {
		t.Fatalf("PATCH with same normalized VPS owner: %v", err)
	}
	if sameOwner.VPSID != owner.VPSID || !sameOwner.UpdatedAt.Equal(unchanged.UpdatedAt) {
		t.Fatalf("same-owner no-op result = VPS %q, updated_at %s; want VPS %q and unchanged updated_at %s", sameOwner.VPSID, sameOwner.UpdatedAt, owner.VPSID, unchanged.UpdatedAt)
	}
	var historyCount int
	if err := pool.QueryRow(ctx, `select count(*) from price_histories where subscription_id = $1`, created.SubscriptionID).Scan(&historyCount); err != nil {
		t.Fatalf("count subscription history after same-owner no-op: %v", err)
	}
	if historyCount != 0 {
		t.Fatalf("price history rows after same-owner no-op = %d, want 0", historyCount)
	}

	// Build a pre-existing ownership change as historical database state. A
	// replay must validate the receipt's current record, not assume PATCH can
	// never have moved it or return another VPS's subscription to the caller.
	if _, err := pool.Exec(ctx, `update subscriptions set vps_id = $2 where subscription_id = $1`, created.SubscriptionID, other.VPSID); err != nil {
		t.Fatalf("seed historical ownership change: %v", err)
	}
	if replay, replayed, err := subRepo.CreateSubscriptionIdempotent(ctx, input, key); !errors.Is(err, subscriptions.ErrSubscriptionReplayOwnershipConflict) || replayed || replay.SubscriptionID != "" {
		t.Fatalf("replay after historical owner change = (%#v, replayed %t, %v), want dedicated conflict", replay, replayed, err)
	}
	moved, err := subRepo.GetSubscription(ctx, created.SubscriptionID)
	if err != nil {
		t.Fatalf("read subscription after rejected replay: %v", err)
	}
	if moved.VPSID != other.VPSID {
		t.Fatalf("rejected replay changed historical owner to %q, want %q", moved.VPSID, other.VPSID)
	}
}

func TestVPSStateRepairSubscriptionLegacyRenewalPatchPreservesSources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Subscription renewal normalization",
		LifecycleStatus: vpsassets.LifecycleIdle,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("create VPS: %v", err)
	}

	for _, mode := range []string{"gift", "lottery", "bonus", "other"} {
		t.Run("preserve_"+mode, func(t *testing.T) {
			created, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{
				VPSID:         vps.VPSID,
				Price:         12,
				Currency:      "USD",
				BillingMonths: 1,
				RenewalMode:   mode,
				PaymentMethod: "card",
			})
			if err != nil {
				t.Fatalf("create %s subscription: %v", mode, err)
			}
			updated, err := subRepo.PatchSubscription(ctx, created.SubscriptionID, subscriptions.PatchInput{
				AutoRenew:          subscriptions.PatchBool(false),
				AutoRenewCancelled: subscriptions.PatchBool(true),
			})
			if err != nil {
				t.Fatalf("apply legacy cancellation flags to %s source: %v", mode, err)
			}
			if updated.RenewalMode != mode || updated.AutoRenew || updated.AutoRenewCancelled {
				t.Fatalf("%s source after cancellation flags = mode %q, legacy %t/%t; want preserved source and false/false", mode, updated.RenewalMode, updated.AutoRenew, updated.AutoRenewCancelled)
			}
		})
	}
}

func TestVPSStateRepairSubscriptionPartialLegacyFlagsUseLockedRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Subscription partial renewal flags",
		LifecycleStatus: vpsassets.LifecycleIdle,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("create VPS: %v", err)
	}

	tests := []struct {
		name       string
		mode       string
		patch      subscriptions.PatchInput
		wantMode   string
		wantAuto   bool
		wantCancel bool
	}{
		{
			name:       "auto retains omitted auto flag while canceling",
			mode:       "auto",
			patch:      subscriptions.PatchInput{AutoRenewCancelled: subscriptions.PatchBool(true)},
			wantMode:   "auto_cancelled",
			wantCancel: true,
		},
		{
			name:     "manual retains omitted cancelled flag while enabling auto",
			mode:     "manual",
			patch:    subscriptions.PatchInput{AutoRenew: subscriptions.PatchBool(true)},
			wantMode: "auto",
			wantAuto: true,
		},
		{
			name:     "auto cancelled retains omitted auto flag while clearing cancellation",
			mode:     "auto_cancelled",
			patch:    subscriptions.PatchInput{AutoRenewCancelled: subscriptions.PatchBool(false)},
			wantMode: "manual",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{
				VPSID:         vps.VPSID,
				Price:         12,
				Currency:      "USD",
				BillingMonths: 1,
				RenewalMode:   tt.mode,
				PaymentMethod: "card",
			})
			if err != nil {
				t.Fatalf("create %s subscription: %v", tt.mode, err)
			}
			updated, err := subRepo.PatchSubscription(ctx, created.SubscriptionID, tt.patch)
			if err != nil {
				t.Fatalf("partial legacy PATCH for %s: %v", tt.mode, err)
			}
			if updated.RenewalMode != tt.wantMode || updated.AutoRenew != tt.wantAuto || updated.AutoRenewCancelled != tt.wantCancel {
				t.Fatalf("%s after partial PATCH = mode %q, legacy %t/%t; want %q, %t/%t", tt.mode, updated.RenewalMode, updated.AutoRenew, updated.AutoRenewCancelled, tt.wantMode, tt.wantAuto, tt.wantCancel)
			}
		})
	}
}

func TestVPSStateRepairSubscriptionCancelledStatusCanBeCorrectedToActive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Subscription status correction",
		LifecycleStatus: vpsassets.LifecycleIdle,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("create VPS: %v", err)
	}
	created, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{
		VPSID:         vps.VPSID,
		Price:         12,
		Currency:      "USD",
		BillingMonths: 1,
		RenewalMode:   "auto",
		Status:        subscriptions.StatusCancelled,
		PaymentMethod: "card",
	})
	if err != nil {
		t.Fatalf("create cancelled subscription: %v", err)
	}
	updated, err := subRepo.PatchSubscription(ctx, created.SubscriptionID, subscriptions.PatchInput{
		Status: subscriptions.PatchStatus(subscriptions.StatusActive),
	})
	if err != nil {
		t.Fatalf("correct cancelled subscription to active: %v", err)
	}
	if updated.Status != subscriptions.StatusActive || !updated.AutoRenew || updated.AutoRenewCancelled {
		t.Fatalf("corrected subscription = status %q, legacy auto %t/%t; want active with existing auto billing facts", updated.Status, updated.AutoRenew, updated.AutoRenewCancelled)
	}
}
func TestVPSStateRepairSubscriptionReplayMissingObjectRemainsInternalError(t *testing.T) {
	ctx := context.Background()
	input := subscriptions.NormalizeCreateInput(subscriptions.CreateInput{
		VPSID:         "vps_001",
		Price:         12,
		Currency:      "USD",
		BillingMonths: 1,
		RenewalMode:   string(subscriptions.RenewalModeManual),
	})
	digest, err := subscriptions.CreateRequestDigest(input)
	if err != nil {
		t.Fatalf("create request digest: %v", err)
	}

	tx := &fakeSubscriptionTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from subscription_create_idempotency"):
			return fakeSubscriptionRow{scan: func(dest ...any) error {
				*dest[0].(*string) = digest
				*dest[1].(*string) = "sub_missing"
				return nil
			}}
		case strings.Contains(sql, "from subscriptions"):
			return fakeSubscriptionRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		default:
			t.Fatalf("unexpected replay query %q", sql)
			return fakeSubscriptionRow{scan: func(dest ...any) error { return nil }}
		}
	}
	repo := &PostgresSubscriptionRepository{
		db: fakeSubscriptionDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}

	record, replayed, err := repo.CreateSubscriptionIdempotent(ctx, input, "missing-sub-target-key")
	if err == nil || replayed || record.SubscriptionID != "" {
		t.Fatalf("missing receipt target = (%#v, replayed %t, %v), want internal consistency error", record, replayed, err)
	}
	if errors.Is(err, subscriptions.ErrSubscriptionReplayOwnershipConflict) ||
		errors.Is(err, subscriptions.ErrSubscriptionNotFound) ||
		!strings.Contains(err.Error(), "load replayed subscription") {
		t.Fatalf("missing receipt target error = %v, want distinct wrapped internal inconsistency", err)
	}
}

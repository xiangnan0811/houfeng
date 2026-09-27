package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairStateCombinationMatrix(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repository := NewPostgresVPSAssetRepository(pool)
	vps, err := repository.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Independent state matrix"})
	if err != nil {
		t.Fatal(err)
	}
	// Removed values remain negative cases; custom purposes have no lifecycle or renewal coupling.
	lifecycles := []vpsassets.LifecycleStatus{"active", "archived", "idle", "testing", "to_migrate", "to_cancel", "cancelled"}
	renewals := []vpsassets.RenewalDecision{"unreviewed", "keep", "cancel", "observe", "migrate", "auto_renew_cancelled", "replaced"}
	tagSets := [][]string{{}, {"自定义用途"}, {"闲置", "生产"}, {"测试", "归档备份"}}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, lifecycle := range lifecycles {
		for _, renewal := range renewals {
			for index, tags := range tagSets {
				t.Run(fmt.Sprintf("%s/%s/tags%d", lifecycle, renewal, index), func(t *testing.T) {
					allowed := (lifecycle == "active" || lifecycle == "archived") && (renewal == "unreviewed" || renewal == "keep" || renewal == "cancel")
					domainErr := vpsassets.ValidateVPSStateCombination(lifecycle, vpsassets.UsageUnknown, renewal)
					if (domainErr == nil) != allowed || (!allowed && !errors.Is(domainErr, vpsassets.ErrInvalidVPSAssetInput)) {
						t.Fatalf("domain allowed=%t error=%v", allowed, domainErr)
					}
					if _, err := tx.Exec(ctx, "savepoint state_case"); err != nil {
						t.Fatal(err)
					}
					_, dbErr := tx.Exec(ctx, `update vps_assets set lifecycle_status=$2,renewal_decision=$3,usage_tags=$4 where vps_id=$1`, vps.VPSID, lifecycle, renewal, tags)
					if allowed {
						if dbErr != nil {
							t.Errorf("independent facts rejected: %v", dbErr)
						}
					} else {
						var pgErr *pgconn.PgError
						if !errors.As(dbErr, &pgErr) || pgErr.Code != "23514" || (pgErr.ConstraintName != "vps_assets_lifecycle_status_allowed" && pgErr.ConstraintName != "vps_assets_renewal_decision_allowed") {
							t.Errorf("removed enum must fail DB allowed-values constraint: %v", dbErr)
						}
					}
					if _, err := tx.Exec(ctx, "rollback to savepoint state_case"); err != nil {
						t.Fatal(err)
					}
					if _, err := tx.Exec(ctx, "release savepoint state_case"); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestVPSStateRepairOrdinaryPatchKeepsFactsIndependentAndArchiveProtected(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repository := NewPostgresVPSAssetRepository(pool)
	for _, decision := range []vpsassets.RenewalDecision{"unreviewed", "keep", "cancel"} {
		t.Run(string(decision), func(t *testing.T) {
			original, err := repository.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Independent patch " + string(decision), RenewalDecision: decision, UsageTags: []string{"自定义用途"}})
			if err != nil {
				t.Fatal(err)
			}
			patched, err := repository.PatchVPSAsset(ctx, original.VPSID, vpsassets.PatchInput{UsageTags: vpsassets.PatchLabels([]string{"闲置", "生产"}), ExpectedUpdatedAt: &original.UpdatedAt})
			if err != nil {
				t.Fatal(err)
			}
			if patched.LifecycleStatus != "active" || patched.RenewalDecision != decision || !reflect.DeepEqual(patched.UsageTags, []string{"闲置", "生产"}) {
				t.Fatalf("coupled independent facts: %+v", patched)
			}
			if _, err := repository.PatchVPSAsset(ctx, original.VPSID, vpsassets.PatchInput{DisplayName: vpsassets.PatchString("stale writer"), ExpectedUpdatedAt: &original.UpdatedAt}); !errors.Is(err, vpsassets.ErrVPSAssetConflict) {
				t.Fatalf("stale CAS must fail: %v", err)
			}
			for _, removed := range []vpsassets.LifecycleStatus{"idle", "testing", "to_cancel", "to_migrate", "cancelled"} {
				if _, err := repository.PatchVPSAsset(ctx, original.VPSID, vpsassets.PatchInput{LifecycleStatus: vpsassets.PatchLifecycle(removed)}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
					t.Errorf("removed lifecycle %s accepted: %v", removed, err)
				}
			}
			if _, err := repository.PatchVPSAsset(ctx, original.VPSID, vpsassets.PatchInput{LifecycleStatus: vpsassets.PatchLifecycle("archived")}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
				t.Fatalf("ordinary PATCH archived without lifecycle action: %v", err)
			}
			if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status='archived',archived_at=now() where vps_id=$1`, original.VPSID); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.PatchVPSAsset(ctx, original.VPSID, vpsassets.PatchInput{LifecycleStatus: vpsassets.PatchLifecycle("active")}); !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
				t.Fatalf("ordinary PATCH revived archive: %v", err)
			}
			stored, err := repository.GetVPSAsset(ctx, original.VPSID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.LifecycleStatus != "archived" || stored.RenewalDecision != decision || !reflect.DeepEqual(stored.UsageTags, patched.UsageTags) {
				t.Fatalf("rejected patch changed archive: %+v", stored)
			}
		})
	}
}

package store

import (
	"context"
	"errors"
	"fmt"
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
	vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "State combination matrix", vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)

	// These are the complete persisted machine-value sets in assets.md, kept
	// explicit so enum additions require an intentional contract update here.
	lifecycles := []vpsassets.LifecycleStatus{
		vpsassets.LifecycleActive,
		vpsassets.LifecycleIdle,
		vpsassets.LifecycleTesting,
		vpsassets.LifecycleToMigrate,
		vpsassets.LifecycleToCancel,
		vpsassets.LifecycleCancelled,
		vpsassets.LifecycleArchived,
	}
	usages := []vpsassets.UsageStatus{
		vpsassets.UsageInUse,
		vpsassets.UsageIdle,
		vpsassets.UsageStandby,
		vpsassets.UsageTesting,
		vpsassets.UsageUnknown,
	}
	renewals := []vpsassets.RenewalDecision{
		vpsassets.RenewalUnreviewed,
		vpsassets.RenewalKeep,
		vpsassets.RenewalObserve,
		vpsassets.RenewalMigrate,
		vpsassets.RenewalCancel,
		vpsassets.RenewalAutoRenewCancelled,
		vpsassets.RenewalReplaced,
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin state matrix transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const savepoint = "vps_state_combination_case"
	checked := 0
	for _, lifecycle := range lifecycles {
		for _, usage := range usages {
			for _, renewal := range renewals {
				lifecycle, usage, renewal := lifecycle, usage, renewal
				name := fmt.Sprintf("%s/%s/%s", lifecycle, usage, renewal)
				t.Run(name, func(t *testing.T) {
					wantAllowed := vpsStateRepairMatrixAllows(lifecycle, usage, renewal)
					domainErr := vpsassets.ValidateVPSStateCombination(lifecycle, usage, renewal)
					domainAllowed := domainErr == nil
					if domainAllowed != wantAllowed {
						t.Errorf("ValidateVPSStateCombination(%s, %s, %s) allowed = %t, want %t (error %v)", lifecycle, usage, renewal, domainAllowed, wantAllowed, domainErr)
					}
					if !wantAllowed && !errors.Is(domainErr, vpsassets.ErrInvalidVPSAssetInput) {
						t.Errorf("ValidateVPSStateCombination(%s, %s, %s) error = %v, want ErrInvalidVPSAssetInput", lifecycle, usage, renewal, domainErr)
					}

					if _, err := tx.Exec(ctx, "savepoint "+savepoint); err != nil {
						t.Fatalf("create per-combination savepoint: %v", err)
					}
					_, dbErr := tx.Exec(ctx, `
						update vps_assets
						set lifecycle_status = $2, usage_status = $3, renewal_decision = $4
						where vps_id = $1`, vps.VPSID, lifecycle, usage, renewal)
					if wantAllowed {
						if dbErr != nil {
							t.Errorf("current PostgreSQL state constraint rejected allowed combination %s/%s/%s: %v", lifecycle, usage, renewal, dbErr)
						}
					} else {
						var pgErr *pgconn.PgError
						if !errors.As(dbErr, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "vps_assets_state_combination_valid" {
							t.Errorf("current PostgreSQL state constraint error for forbidden combination %s/%s/%s = %v, want SQLSTATE 23514 from vps_assets_state_combination_valid", lifecycle, usage, renewal, dbErr)
						}
					}
					dbAllowed := dbErr == nil
					if dbAllowed != wantAllowed {
						t.Errorf("current PostgreSQL state constraint allowed %s/%s/%s = %t, want %t", lifecycle, usage, renewal, dbAllowed, wantAllowed)
					}
					if _, err := tx.Exec(ctx, "rollback to savepoint "+savepoint); err != nil {
						t.Fatalf("rollback combination to savepoint: %v", err)
					}
					if _, err := tx.Exec(ctx, "release savepoint "+savepoint); err != nil {
						t.Fatalf("release per-combination savepoint: %v", err)
					}
				})
				checked++
			}
		}
	}
	if checked != 7*5*7 {
		t.Fatalf("state combinations checked = %d, want 245 (7 lifecycle × 5 usage × 7 renewal)", checked)
	}
}

// This is the independent contract oracle from docs/spec/contracts/assets.md:
// cancelled requires a cancellation decision and cannot be in use; archived
// cannot be in use; to_cancel requires a cancellation decision; to_migrate
// requires migrate; replaced cannot be active or in use. No other pair is
// restricted.
func vpsStateRepairMatrixAllows(lifecycle vpsassets.LifecycleStatus, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision) bool {
	cancellationDecision := renewal == vpsassets.RenewalCancel || renewal == vpsassets.RenewalAutoRenewCancelled
	switch lifecycle {
	case vpsassets.LifecycleCancelled:
		if !cancellationDecision || usage == vpsassets.UsageInUse {
			return false
		}
	case vpsassets.LifecycleArchived:
		if usage == vpsassets.UsageInUse {
			return false
		}
	case vpsassets.LifecycleToCancel:
		if !cancellationDecision {
			return false
		}
	case vpsassets.LifecycleToMigrate:
		if renewal != vpsassets.RenewalMigrate {
			return false
		}
	}
	if renewal == vpsassets.RenewalReplaced && (lifecycle == vpsassets.LifecycleActive || usage == vpsassets.UsageInUse) {
		return false
	}
	return true
}

func TestVPSStateRepairOrdinaryPatchReopensWorkflowStatesButNotTerminalStates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repository := NewPostgresVPSAssetRepository(pool)
	targetLifecycles := []vpsassets.LifecycleStatus{
		vpsassets.LifecycleActive,
		vpsassets.LifecycleIdle,
		vpsassets.LifecycleTesting,
	}
	sources := []struct {
		lifecycle vpsassets.LifecycleStatus
		usage     vpsassets.UsageStatus
		renewal   vpsassets.RenewalDecision
	}{
		{lifecycle: vpsassets.LifecycleToCancel, usage: vpsassets.UsageIdle, renewal: vpsassets.RenewalCancel},
		{lifecycle: vpsassets.LifecycleToMigrate, usage: vpsassets.UsageIdle, renewal: vpsassets.RenewalMigrate},
	}

	for _, source := range sources {
		for _, target := range targetLifecycles {
			name := fmt.Sprintf("workflow/%s-to-%s", source.lifecycle, target)
			t.Run(name, func(t *testing.T) {
				vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Ordinary patch "+name, vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)
				if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = $2, usage_status = $3, renewal_decision = $4 where vps_id = $1`, vps.VPSID, source.lifecycle, source.usage, source.renewal); err != nil {
					t.Fatalf("seed %s state: %v", source.lifecycle, err)
				}
				patched, err := repository.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{LifecycleStatus: vpsassets.PatchLifecycle(target)})
				if err != nil {
					t.Fatalf("ordinary PATCH from %s to %s: %v", source.lifecycle, target, err)
				}
				if patched.LifecycleStatus != target || patched.UsageStatus != source.usage || patched.RenewalDecision != source.renewal {
					t.Fatalf("ordinary PATCH result = %s/%s/%s, want %s/%s/%s", patched.LifecycleStatus, patched.UsageStatus, patched.RenewalDecision, target, source.usage, source.renewal)
				}
				stored, err := repository.GetVPSAsset(ctx, vps.VPSID)
				if err != nil {
					t.Fatalf("read state after ordinary PATCH: %v", err)
				}
				if stored.LifecycleStatus != target || stored.UsageStatus != source.usage || stored.RenewalDecision != source.renewal {
					t.Fatalf("persisted state after ordinary PATCH = %s/%s/%s, want %s/%s/%s", stored.LifecycleStatus, stored.UsageStatus, stored.RenewalDecision, target, source.usage, source.renewal)
				}
			})
		}
	}

	terminalSources := []struct {
		lifecycle vpsassets.LifecycleStatus
		usage     vpsassets.UsageStatus
		renewal   vpsassets.RenewalDecision
	}{
		{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageIdle, renewal: vpsassets.RenewalCancel},
		{lifecycle: vpsassets.LifecycleArchived, usage: vpsassets.UsageUnknown, renewal: vpsassets.RenewalCancel},
	}
	for _, source := range terminalSources {
		for _, target := range targetLifecycles {
			name := fmt.Sprintf("terminal/%s-to-%s", source.lifecycle, target)
			t.Run(name, func(t *testing.T) {
				vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Ordinary patch "+name, vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)
				_, err := pool.Exec(ctx, `
					update vps_assets
					set lifecycle_status = $2,
					    usage_status = $3,
					    renewal_decision = $4,
					    archived_at = case when $2 = 'archived' then now() else null end
					where vps_id = $1`, vps.VPSID, source.lifecycle, source.usage, source.renewal)
				if err != nil {
					t.Fatalf("seed terminal %s state: %v", source.lifecycle, err)
				}
				if _, err := repository.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{LifecycleStatus: vpsassets.PatchLifecycle(target)}); !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
					t.Fatalf("ordinary PATCH from terminal %s to %s error = %v, want ErrVPSAssetReadonly", source.lifecycle, target, err)
				}
				stored, err := repository.GetVPSAsset(ctx, vps.VPSID)
				if err != nil {
					t.Fatalf("read state after rejected terminal PATCH: %v", err)
				}
				if stored.LifecycleStatus != source.lifecycle || stored.UsageStatus != source.usage || stored.RenewalDecision != source.renewal {
					t.Fatalf("state after rejected terminal PATCH = %s/%s/%s, want unchanged %s/%s/%s", stored.LifecycleStatus, stored.UsageStatus, stored.RenewalDecision, source.lifecycle, source.usage, source.renewal)
				}
			})
		}
	}
}

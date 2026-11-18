package migrate

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/platformmigrate"
)

func testPostgresIntegrationOrdinaryMembership(t *testing.T) {
	t.Run("membership_v2", TestPostgresIntegrationComposeMembershipV2)
	t.Run("fresh_rollback_and_locks", TestPostgresIntegrationComposeMembershipFreshRollbackAndLocks)
	t.Run("heartbeat_concurrent_close", TestPostgresIntegrationComposeHeartbeatConcurrentClose)
	t.Run("legacy_v1_fence", testPostgresLegacyHeartbeatFence)
	t.Run("processor_worker_admission", testPostgresProcessorWorkerAdmission)
}

// This uses the real V2 inventory projector, launch registration and authority
// heartbeat to admit the worker before exercising the effect-time guard.
func testPostgresProcessorWorkerAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	activation, activationBytes, rotationBytes, rotation := composeMembershipFixture(t)
	roles := platformmigrate.AppRoleSetV2{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, DeletionWorker: fixture.deletionWorker, Migrator: fixture.migrator}
	if _, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, rotationBytes); err != nil {
		t.Fatal(err)
	}
	center, processor := birthLaunchFixturePair(activation.DeploymentID, activation.ActivationBundleDigest, nil)
	centerBytes, processorBytes := birthLaunchEncode(t, center), birthLaunchEncode(t, processor)
	revision := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0)
	birthLaunchOpen(t, ctx, owner, revision)
	processorDigest := birthLaunchDigest(processorBytes)
	centerDigest := birthLaunchDigest(centerBytes)
	guard := `select public.record_processor_worker_admit($1::text,$2::text,$3::text,$4::text)`
	args := []any{activation.DeploymentID, "default", "compose-processor", "records.runtime"}

	// The pending pair is not queue-admitted even though its placeholder lease is future.
	pending, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, processorDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Exec(ctx, guard, args...); err == nil {
		t.Fatal("pending worker was admitted")
	}
	if err := pending.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var authorityExisted bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from pg_roles where rolname='houfeng_records_authority')`).Scan(&authorityExisted); err != nil {
		t.Fatal(err)
	}
	if !authorityExisted {
		if _, err := fixture.db.Exec(ctx, `create role houfeng_records_authority login noinherit`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if _, err := fixture.db.Exec(cleanup, `drop owned by houfeng_records_authority; drop role houfeng_records_authority`); err != nil {
				t.Errorf("remove temporary authority role: %v", err)
			}
		})
	}
	if _, err := fixture.db.Exec(ctx, `grant usage on schema public to houfeng_records_authority; grant execute on function public.record_platform_compose_membership_heartbeat_v2(bytea) to houfeng_records_authority`); err != nil {
		t.Fatal(err)
	}
	authority, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = authority.Rollback(ctx) }()
	if _, err := authority.Exec(ctx, `set session authorization houfeng_records_authority`); err != nil {
		t.Fatal(err)
	}
	now := uint64(time.Now().Unix())
	heartbeat := append([]byte("HOUFENG-APP-PROJECTION-COMMAND-V1"), 0, 1, 4, 9)
	heartbeat = append(heartbeat, activation.DeploymentID...)
	heartbeat = append(heartbeat, activation.ActivationBundleDigest[:]...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 2)
	heartbeat = append(heartbeat, rotation.NextWitnessedLedgerHash[:]...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 2)
	heartbeat = append(heartbeat, rotation.NextIdentitySetDigest[:]...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 1)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, now)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, now+90)
	if _, err := authority.Exec(ctx, `select public.record_platform_compose_membership_heartbeat_v2($1::bytea)`, heartbeat); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Exec(ctx, `reset session authorization`); err != nil {
		t.Fatal(err)
	}
	if err := authority.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, setup, role, digest string
		args                      []any
		accept                    bool
	}{
		{"processor", "", fixture.runtime, processorDigest, args, true},
		{"center_launch", "", fixture.runtime, centerDigest, args, false},
		{"missing_launch", "", fixture.runtime, "", args, false},
		{"admin_role", "", fixture.admin, processorDigest, args, false},
		{"worker_role", "", fixture.deletionWorker, processorDigest, args, false},
		{"authority_role", "", "houfeng_records_authority", processorDigest, args, false},
		{"wrong_instance", "", fixture.runtime, processorDigest, []any{activation.DeploymentID, "default", "compose-center", "records.runtime"}, false},
		{"wrong_capability", "", fixture.runtime, processorDigest, []any{activation.DeploymentID, "default", "compose-processor", "records.other"}, false},
		{"wrong_project", "", fixture.runtime, processorDigest, []any{activation.DeploymentID, "other", "compose-processor", "records.runtime"}, false},
		{"stale_epoch", `update public.deployment_membership set deployment_epoch=1 where instance_id='compose-processor'`, fixture.runtime, processorDigest, args, false},
		{"future_fence", `update public.deployment_membership set fence_contract_version=2 where instance_id='compose-processor'`, fixture.runtime, processorDigest, args, true},
		{"below_minimum_fence", `update public.deployment_contract_state set minimum_fence_contract_version=2 where project_id='default'`, fixture.runtime, processorDigest, args, false},
		{"wrong_kind", `update public.deployment_membership set instance_kind='api' where instance_id='compose-processor'`, fixture.runtime, processorDigest, args, false},
		{"queue_closed", `update public.deployment_membership set queue_admitted=false where instance_id='compose-processor'`, fixture.runtime, processorDigest, args, false},
		{"expired", `update public.deployment_membership set heartbeat_expires_at=created_at+interval '1 microsecond' where instance_id='compose-processor'`, fixture.runtime, processorDigest, args, false},
		{"closed_launch", `update record_platform_internal.record_birth_launch_state set opened=false where singleton`, fixture.runtime, processorDigest, args, false},
		{"absent_launch", `delete from record_platform_internal.record_birth_launch_state where singleton`, fixture.runtime, processorDigest, args, false},
		{"dynamic_epoch", `update public.deployment_contract_state set active_domain_identity_epoch=3 where project_id='default'; update public.deployment_membership set deployment_epoch=3 where instance_id='compose-processor'`, fixture.runtime, processorDigest, args, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := fixture.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if tc.setup != "" {
				if _, err := tx.Exec(ctx, tc.setup); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(ctx, `set session authorization `+pgx.Identifier{tc.role}.Sanitize()); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, tc.digest); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, guard, tc.args...)
			if tc.accept && err != nil {
				t.Fatalf("worker admission rejected: %v", err)
			}
			if !tc.accept && err == nil {
				t.Fatal("worker admission accepted")
			}
		})
	}

	// The guard's shared locks must survive until transaction end, so neither a
	// heartbeat update nor launch closure can pass an in-flight record effect.
	active, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = active.Rollback(ctx) }()
	if _, err := active.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, processorDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := active.Exec(ctx, guard, args...); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`update public.deployment_membership set heartbeat_expires_at=heartbeat_expires_at+interval '1 second' where instance_id='compose-processor'`,
		`select record_platform_internal.record_birth_launch_close($1::bigint)`,
	} {
		blocked, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := blocked.Exec(ctx, `set local lock_timeout='100ms'`); err != nil {
			t.Fatal(err)
		}
		if sql[0] == 's' {
			_, err = blocked.Exec(ctx, sql, revision)
		} else {
			_, err = blocked.Exec(ctx, sql)
		}
		requirePostgresSQLState(t, err, "55P03")
		if err := blocked.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := active.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

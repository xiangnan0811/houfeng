package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordplatform"
)

// These commands use the public V1 codec and the specified V2 digest framing,
// independently of the Compose state writer.
func composeMembershipFixture(t *testing.T) (recordplatform.ContractActivationProjectionCommandV1, []byte, []byte, recordplatform.DomainRotationProjectionCommandV1) {
	t.Helper()
	activation := projectorCASActivationCommand()
	activation.MinimumFenceContractVersion = 1
	body := fmt.Sprintf(`{"version":2,"deployment_id":%q,"project_id":"default","memberships":[{"instance_id":"compose-center","instance_kind":"api","capability":"records.runtime","deployment_epoch":2,"fence_contract_version":1,"load_balancer_admitted":true,"queue_admitted":false},{"instance_id":"compose-processor","instance_kind":"worker","capability":"records.runtime","deployment_epoch":2,"fence_contract_version":1,"load_balancer_admitted":false,"queue_admitted":true}]}`, activation.DeploymentID)
	digest := func(domain string, payload []byte) [32]byte {
		framed := append([]byte(domain), 0)
		framed = binary.BigEndian.AppendUint32(framed, uint32(len(payload)))
		framed = append(framed, payload...)
		return sha256.Sum256(framed)
	}
	identity := digest("HOUFENG-COMPOSE-RECORDS-AUTHORITY-IDENTITY-SET-V2", []byte(body))
	material := append([]byte(nil), activation.WitnessedLedgerHash[:]...)
	material = append(material, identity[:]...)
	material = append(material, activation.ActivationBundleDigest[:]...)
	mutation := digest("HOUFENG-COMPOSE-RECORDS-AUTHORITY-MEMBERSHIP-MUTATION-V2", material)
	rotation := projectorCASRotationFromActivation(activation)
	rotation.RotationMutationID = "tm-" + hex.EncodeToString(mutation[:])
	rotation.NextIdentitySetDigest = identity
	rotation.NextAdapterPolicyGeneration = 1
	rotation.NextAdapterPolicyDigest = activation.AdapterPolicyDigest
	rotation.NextMinimumFenceContractVersion = 1
	activationBytes, err := activation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	rotationBytes, err := rotation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return activation, activationBytes, rotationBytes, rotation
}

func TestPostgresIntegrationComposeMembershipV2(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	activation, activationBytes, rotationBytes, rotation := composeMembershipFixture(t)
	if _, err := owner.Exec(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, activationBytes); err != nil {
		t.Fatal(err)
	}
	var originalCreated time.Time
	if err := owner.QueryRow(ctx, `insert into public.deployment_membership(instance_id,deployment_id,project_id,instance_kind,capability,deployment_epoch,fence_contract_version,load_balancer_admitted,queue_admitted,heartbeat_expires_at) values ('compose-center',$1,'default','api','records.runtime',1,1,true,false,transaction_timestamp()+interval '90 seconds') returning created_at`, activation.DeploymentID).Scan(&originalCreated); err != nil {
		t.Fatal(err)
	}
	roles := platformmigrate.AppRoleSetV2{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, DeletionWorker: fixture.deletionWorker, Migrator: fixture.migrator}
	initialCenter, initialProcessor := birthLaunchFixturePair(activation.DeploymentID, activation.ActivationBundleDigest, nil)
	initialRevision := birthLaunchStage(t, ctx, owner, fixture, birthLaunchEncode(t, initialCenter), birthLaunchEncode(t, initialProcessor), 0)
	birthLaunchOpen(t, ctx, owner, initialRevision)
	if _, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, rotationBytes); err == nil {
		t.Fatal("open launch allowed an actual inventory transition")
	}
	birthLaunchClose(t, ctx, owner, initialRevision)
	receipt, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, rotationBytes)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectorCASReceipt(t, rotationBytes, receipt)
	var count int
	var preserved bool
	if err := owner.QueryRow(ctx, `select count(*),bool_and(not load_balancer_admitted and not queue_admitted) and bool_or(instance_id='compose-center' and created_at=$1) from public.deployment_membership where instance_id in ('compose-center','compose-processor') and deployment_epoch=2`, originalCreated).Scan(&count, &preserved); err != nil || count != 2 || !preserved {
		t.Fatalf("pending pair count=%d preserved=%v err=%v", count, preserved, err)
	}
	repeated, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, rotationBytes)
	if err != nil || !bytes.Equal(receipt, repeated) {
		t.Fatalf("exact replay receipt %x err %v", repeated, err)
	}
	other := append([]byte(nil), rotationBytes...)
	other[348] ^= 1
	if _, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, other); err == nil {
		t.Fatal("different successor accepted")
	}
	legacy := append([]byte("HOUFENG-APP-PROJECTION-COMMAND-V1"), 0, 1, 3, 10)
	legacy = append(legacy, activation.DeploymentID...)
	legacy = append(legacy, []byte("defaultcompose-centerapirecords.runtime")...)
	legacy = binary.BigEndian.AppendUint64(legacy, 1)
	legacy = binary.BigEndian.AppendUint64(legacy, 1)
	legacy = append(legacy, 1, 0)
	legacy = binary.BigEndian.AppendUint64(legacy, uint64(time.Now().Unix()))
	legacy = binary.BigEndian.AppendUint64(legacy, uint64(time.Now().Unix()+90))
	if _, err := owner.Exec(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, legacy); err == nil {
		t.Fatal("V1 heartbeat accepted after V2 rotation")
	}
	binary.BigEndian.PutUint64(legacy[143:151], 2)
	if _, err := owner.Exec(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, legacy); err == nil {
		t.Fatal("V1 operation renewed epoch 2")
	}
	center, processor := birthLaunchFixturePair(activation.DeploymentID, activation.ActivationBundleDigest, nil)
	revision := birthLaunchStage(t, ctx, owner, fixture, birthLaunchEncode(t, center), birthLaunchEncode(t, processor), 0)
	birthLaunchOpen(t, ctx, owner, revision)
	var pendingBefore, launchBefore string
	if err := owner.QueryRow(ctx, `select (select jsonb_agg(to_jsonb(m) order by m.instance_id)::text from public.deployment_membership m where m.instance_id in ('compose-center','compose-processor')), (select to_jsonb(l)::text from record_platform_internal.record_birth_launch_state l where l.singleton)`).Scan(&pendingBefore, &launchBefore); err != nil {
		t.Fatal(err)
	}
	openReceipt, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, rotationBytes)
	if err != nil || !bytes.Equal(receipt, openReceipt) {
		t.Fatalf("open pending replay receipt %x err %v", openReceipt, err)
	}
	var pendingAfter, launchAfter string
	if err := owner.QueryRow(ctx, `select (select jsonb_agg(to_jsonb(m) order by m.instance_id)::text from public.deployment_membership m where m.instance_id in ('compose-center','compose-processor')), (select to_jsonb(l)::text from record_platform_internal.record_birth_launch_state l where l.singleton)`).Scan(&pendingAfter, &launchAfter); err != nil || pendingAfter != pendingBefore || launchAfter != launchBefore {
		t.Fatalf("open pending replay changed inventory or launch: %v", err)
	}

	// The fixed authority role is created and granted only inside a rolled-back
	// transaction, leaving the shared PostgreSQL role catalog unchanged.
	tx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `do $$ begin if not exists(select 1 from pg_roles where rolname='houfeng_records_authority') then create role houfeng_records_authority login noinherit; end if; end $$; grant usage on schema public to houfeng_records_authority; grant execute on function public.record_platform_compose_membership_heartbeat_v2(bytea) to houfeng_records_authority; set session authorization houfeng_records_authority`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	heartbeat := append([]byte("HOUFENG-APP-PROJECTION-COMMAND-V1"), 0, 1, 4, 9)
	heartbeat = append(heartbeat, activation.DeploymentID...)
	heartbeat = append(heartbeat, activation.ActivationBundleDigest[:]...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 2)
	heartbeat = append(heartbeat, rotation.NextWitnessedLedgerHash[:]...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 2)
	heartbeat = append(heartbeat, rotation.NextIdentitySetDigest[:]...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 1)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, uint64(now))
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, uint64(now+90))
	var expiry time.Time
	if err := tx.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat_v2($1::bytea)`, heartbeat).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `reset session authorization`); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := tx.QueryRow(ctx, `select count(*)=2 and bool_and(heartbeat_expires_at=$1) and bool_or(instance_id='compose-center' and load_balancer_admitted and not queue_admitted) and bool_or(instance_id='compose-processor' and not load_balancer_admitted and queue_admitted) from public.deployment_membership where instance_id in ('compose-center','compose-processor')`, expiry).Scan(&active); err != nil || !active {
		t.Fatalf("heartbeat pair active=%v err=%v", active, err)
	}
	leasedExpiry := expiry
	for _, tc := range []struct {
		name, setup string
		offset      int
	}{
		{"wrong_operation", "", 35},
		{"stale_counter", "", 143},
		{"partial_pair", `delete from public.deployment_membership where instance_id='compose-processor'`, -1},
		{"mixed_flags", `update public.deployment_membership set load_balancer_admitted=false where instance_id='compose-center'`, -1},
		{"closed_launch", `update record_platform_internal.record_birth_launch_state set opened=false where singleton`, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tx.Exec(ctx, `savepoint heartbeat_case`); err != nil {
				t.Fatal(err)
			}
			if tc.setup != "" {
				if _, err := tx.Exec(ctx, tc.setup); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(ctx, `set session authorization houfeng_records_authority`); err != nil {
				t.Fatal(err)
			}
			command := append([]byte(nil), heartbeat...)
			if tc.offset >= 0 {
				command[tc.offset] ^= 1
			}
			if err := tx.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat_v2($1::bytea)`, command).Scan(&expiry); err == nil {
				t.Fatal("invalid heartbeat accepted")
			}
			if _, err := tx.Exec(ctx, `rollback to savepoint heartbeat_case`); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, `select count(*)=2 and bool_or(instance_id='compose-center' and load_balancer_admitted and not queue_admitted) and bool_or(instance_id='compose-processor' and not load_balancer_admitted and queue_admitted) from public.deployment_membership where instance_id in ('compose-center','compose-processor')`).Scan(&active); err != nil || !active {
				t.Fatalf("failed heartbeat altered pair: active=%v err=%v", active, err)
			}
		})
	}
	for _, role := range []string{fixture.runtime, fixture.admin, fixture.deletionWorker} {
		if _, err := tx.Exec(ctx, `savepoint principal_case`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `set session authorization `+pgx.Identifier{role}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat_v2($1::bytea)`, heartbeat).Scan(&expiry); err == nil {
			t.Fatalf("role %s renewed membership", role)
		}
		if _, err := tx.Exec(ctx, `rollback to savepoint principal_case`); err != nil {
			t.Fatal(err)
		}
	}
	for _, drift := range []string{
		`update record_platform_internal.record_birth_launch_state set center_binding=convert_to('{}','UTF8') where singleton`,
		`update record_platform_internal.record_birth_launch_state set manifest_digest=decode(repeat('00',32),'hex') where singleton`,
	} {
		if _, err := tx.Exec(ctx, `savepoint stale_launch`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, drift); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `set session authorization `+pgx.Identifier{fixture.migrator}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		var ignored []byte
		if err := tx.QueryRow(ctx, `select public.record_platform_compose_inventory_upgrade($1::bytea,$2::bytea)`, activationBytes, rotationBytes).Scan(&ignored); err == nil {
			t.Fatal("stale open launch accepted for replay")
		}
		if _, err := tx.Exec(ctx, `rollback to savepoint stale_launch`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `set session authorization `+pgx.Identifier{fixture.migrator}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	var replayReceipt []byte
	if err := tx.QueryRow(ctx, `select public.record_platform_compose_inventory_upgrade($1::bytea,$2::bytea)`, activationBytes, rotationBytes).Scan(&replayReceipt); err != nil || !bytes.Equal(receipt, replayReceipt) {
		t.Fatalf("active replay receipt %x err %v", replayReceipt, err)
	}
	if _, err := tx.Exec(ctx, `reset session authorization`); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select count(*)=2 and bool_and(heartbeat_expires_at=$1) and bool_or(instance_id='compose-center' and load_balancer_admitted and not queue_admitted) and bool_or(instance_id='compose-processor' and not load_balancer_admitted and queue_admitted) and (select opened from record_platform_internal.record_birth_launch_state where singleton) from public.deployment_membership where instance_id in ('compose-center','compose-processor')`, leasedExpiry).Scan(&active); err != nil || !active {
		t.Fatalf("open replay changed active memberships or launch: %v %v", active, err)
	}
}

func TestPostgresIntegrationComposeMembershipFreshRollbackAndLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	_, activation, rotation, _ := composeMembershipFixture(t)
	roles := platformmigrate.AppRoleSetV2{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, DeletionWorker: fixture.deletionWorker, Migrator: fixture.migrator}
	for _, role := range []string{fixture.runtime, fixture.admin, fixture.deletionWorker} {
		pool := fixture.openDirectRolePool(t, ctx, role)
		if _, err := pool.Exec(ctx, `select public.record_platform_compose_inventory_upgrade($1::bytea,$2::bytea)`, activation, rotation); err == nil {
			t.Fatalf("role %s projected inventory", role)
		}
	}
	for _, seed := range []string{
		`insert into public.source_authority_admission(singleton,deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode) values(true,'dp-'||repeat('a',64),'rd-'||repeat('a',64),'1',1,'checkpoint_fixture',1,1,'active')`,
		`insert into record_platform_internal.record_birth_lineage(authority_id,lineage_id) values ('rd-'||repeat('a',64),'bl-'||repeat('b',64))`,
	} {
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, seed); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `select public.record_platform_compose_inventory_upgrade($1::bytea,$2::bytea)`, activation, rotation); err == nil {
			t.Fatal("installed source or birth accepted")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	cut, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var tentative []byte
	if err := cut.QueryRow(ctx, `select public.record_platform_compose_inventory_upgrade($1::bytea,$2::bytea)`, activation, rotation).Scan(&tentative); err != nil {
		t.Fatal(err)
	}
	if err := cut.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var empty bool
	if err := owner.QueryRow(ctx, `select (select deployment_id is null from public.deployment_contract_state where project_id='default') and not exists(select 1 from public.deployment_membership) and not exists(select 1 from record_platform_internal.compose_membership_transition)`).Scan(&empty); err != nil || !empty {
		t.Fatalf("rollback retained inventory: empty=%v err=%v", empty, err)
	}
	contender := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	holder, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `lock table public.source_authority_admission in share row exclusive mode`); err != nil {
		t.Fatal(err)
	}
	contenderCtx, contenderCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = ProjectComposeMembershipV2(contenderCtx, contender, roles, activation, rotation)
	contenderCancel()
	requirePostgresSQLState(t, err, "40001")
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	receipt, err := ProjectComposeMembershipV2(ctx, owner, roles, activation, rotation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(receipt, tentative) {
		t.Fatalf("receipt after rollback %x != %x", receipt, tentative)
	}
}

func TestPostgresIntegrationComposeHeartbeatConcurrentClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	activation, activationBytes, rotationBytes, rotation := composeMembershipFixture(t)
	roles := platformmigrate.AppRoleSetV2{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, DeletionWorker: fixture.deletionWorker, Migrator: fixture.migrator}
	if _, err := ProjectComposeMembershipV2(ctx, owner, roles, activationBytes, rotationBytes); err != nil {
		t.Fatal(err)
	}
	center, processor := birthLaunchFixturePair(activation.DeploymentID, activation.ActivationBundleDigest, nil)
	revision := birthLaunchStage(t, ctx, owner, fixture, birthLaunchEncode(t, center), birthLaunchEncode(t, processor), 0)
	birthLaunchOpen(t, ctx, owner, revision)
	closing, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closing.Rollback(ctx) }()
	if _, err := closing.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	authority, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = authority.Rollback(ctx) }()
	if _, err := authority.Exec(ctx, `do $$ begin if not exists(select 1 from pg_roles where rolname='houfeng_records_authority') then create role houfeng_records_authority login noinherit; end if; end $$; grant usage on schema public to houfeng_records_authority; grant execute on function public.record_platform_compose_membership_heartbeat_v2(bytea) to houfeng_records_authority; set session authorization houfeng_records_authority`); err != nil {
		t.Fatal(err)
	}
	now := uint64(time.Now().Unix())
	command := append([]byte("HOUFENG-APP-PROJECTION-COMMAND-V1"), 0, 1, 4, 9)
	command = append(command, activation.DeploymentID...)
	command = append(command, activation.ActivationBundleDigest[:]...)
	command = binary.BigEndian.AppendUint64(command, 2)
	command = append(command, rotation.NextWitnessedLedgerHash[:]...)
	command = binary.BigEndian.AppendUint64(command, 2)
	command = append(command, rotation.NextIdentitySetDigest[:]...)
	command = binary.BigEndian.AppendUint64(command, 1)
	command = binary.BigEndian.AppendUint64(command, now)
	command = binary.BigEndian.AppendUint64(command, now+90)
	var expiry time.Time
	heartbeatCtx, heartbeatCancel := context.WithTimeout(ctx, 3*time.Second)
	defer heartbeatCancel()
	err = authority.QueryRow(heartbeatCtx, `select public.record_platform_compose_membership_heartbeat_v2($1::bytea)`, command).Scan(&expiry)
	requirePostgresSQLState(t, err, "40001")
	if err := authority.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := closing.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err := owner.QueryRow(ctx, `select count(*)=2 and bool_and(not load_balancer_admitted and not queue_admitted) from public.deployment_membership where instance_id in ('compose-center','compose-processor')`).Scan(&pending); err != nil || !pending {
		t.Fatalf("close admitted partial pair: pending=%v err=%v", pending, err)
	}
}

func testPostgresLegacyHeartbeatFence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	activation := projectorCASActivationCommand()
	activation.MinimumFenceContractVersion = 2
	command, err := activation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, command); err != nil {
		t.Fatal(err)
	}
	now := uint64(time.Now().Unix())
	heartbeat := append([]byte("HOUFENG-APP-PROJECTION-COMMAND-V1"), 0, 1, 3, 10)
	heartbeat = append(heartbeat, activation.DeploymentID...)
	heartbeat = append(heartbeat, []byte("defaultcompose-centerapirecords.runtime")...)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 1)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, 2)
	heartbeat = append(heartbeat, 1, 0)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, now)
	heartbeat = binary.BigEndian.AppendUint64(heartbeat, now+90)
	var expiry time.Time
	if err := owner.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, heartbeat).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	if !expiry.Equal(time.Unix(int64(now+90), 0)) {
		t.Fatalf("legacy heartbeat expiry = %v", expiry)
	}
}

package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
)

// This consumer fixture seeds membership rows directly. Signed issuer and
// membership projection acceptance are exercised separately by their own tests.
func TestPostgresIntegrationProcessorWorkerAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal, local.local)
	if err != nil {
		t.Fatal(err)
	}
	seed := seedManagedAttachmentProcessor(t, ctx, fixture, local.repository, writer, "workeradmission", []byte("worker admission"))
	var deployment string
	var epoch, fence int64
	if err := fixture.db.QueryRow(ctx, `select deployment_id, active_domain_identity_epoch, minimum_fence_contract_version from public.deployment_contract_state where project_id='default'`).Scan(&deployment, &epoch, &fence); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.deployment_membership
		(instance_id,deployment_id,project_id,instance_kind,capability,deployment_epoch,fence_contract_version,load_balancer_admitted,queue_admitted,heartbeat_expires_at)
		values ('compose-processor',$1,'default','worker','records.runtime',$2,$3,false,true,clock_timestamp()+interval '1 hour')`, deployment, epoch, fence); err != nil {
		t.Fatal(err)
	}
	// The command and registry are real. These synthetic binary digests test
	// database launch admission only, not an executable or filesystem binding.
	var activation []byte
	if err := fixture.db.QueryRow(ctx, `select activation_bundle_digest from public.deployment_contract_state where project_id='default'`).Scan(&activation); err != nil || len(activation) != sha256.Size {
		t.Fatalf("read activation digest: %v", err)
	}
	launch := recordbirth.LaunchBinding{ProtocolVersion: 1, ProcessKind: "center", DeploymentID: deployment, BinaryDigest: sha256.Sum256([]byte("worker admission fixture center"))}
	copy(launch.ActivationBundleDigest[:], activation)
	centerBinding, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		t.Fatal(err)
	}
	launch.ProcessKind = "content-processor"
	launch.BinaryDigest = sha256.Sum256([]byte("worker admission fixture processor"))
	processorBinding, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		t.Fatal(err)
	}
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator, "processor-admission-launch-owner", 1)
	var revision int64
	if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`, centerBinding, processorBinding, fixture.runtime, fixture.deletionWorker).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("stage processor launch: revision=%d error=%v", revision, err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	processorDigest := sha256.Sum256(processorBinding)
	poolConfig := fixture.directRoleConfig(t, fixture.runtime, "processor-admission-consumer")
	poolConfig.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = hex.EncodeToString(processorDigest[:])
	processorPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(processorPool.Close)
	identity := DeploymentMembershipIdentity{InstanceID: "compose-processor", DeploymentID: recordplatform.DeploymentID(deployment), ProjectID: recordplatform.ProjectIDDefault, InstanceKind: "worker", Capability: "records.runtime"}
	configurePool := func(identity DeploymentMembershipIdentity, pool *pgxpool.Pool) *PostgresAttachmentRepository {
		t.Helper()
		gate, err := NewDeploymentMembershipAdmissionGate(identity)
		if err != nil {
			t.Fatal(err)
		}
		repository := NewPostgresAttachmentRepository(pool)
		if err := repository.ConfigureProcessorAdmission(gate); err != nil {
			t.Fatal(err)
		}
		if err := repository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, pool, gate)); err != nil {
			t.Fatal(err)
		}
		return repository
	}
	configure := func(identity DeploymentMembershipIdentity) *PostgresAttachmentRepository {
		return configurePool(identity, processorPool)
	}
	centerDigest := sha256.Sum256(centerBinding)
	centerConfig := fixture.directRoleConfig(t, fixture.runtime, "processor-admission-center-launch")
	centerConfig.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = hex.EncodeToString(centerDigest[:])
	centerPool, err := pgxpool.NewWithConfig(ctx, centerConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(centerPool.Close)
	centerLaunch := configurePool(identity, centerPool)
	repository := configure(identity)
	// Admission holds both authority rows until the consumer transaction ends,
	// so a concurrent revocation or epoch transition cannot pass the check.
	admittedTx, err := processorPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.admitProcessor(ctx, admittedTx); err != nil {
		_ = admittedTx.Rollback(ctx)
		t.Fatalf("admit worker transaction: %v", err)
	}
	for _, query := range []string{
		`select true from public.deployment_contract_state where project_id='default' for update nowait`,
		`select true from public.deployment_membership where instance_id='compose-processor' for update nowait`,
	} {
		tx, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var value bool
		err = tx.QueryRow(ctx, query).Scan(&value)
		_ = tx.Rollback(ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("authority row was not held through transaction: %v", err)
		}
	}
	if err := admittedTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wrongIdentity := identity
	wrongIdentity.InstanceID = "compose-center"
	wrongDeployment := identity
	wrongDeployment.DeploymentID = recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64))
	if wrongDeployment.DeploymentID == identity.DeploymentID {
		wrongDeployment.DeploymentID = recordplatform.DeploymentID("dp-" + strings.Repeat("b", 64))
	}
	wrongCapability := identity
	wrongCapability.Capability = "records.other"
	wrong := configure(wrongIdentity)
	wrongDeploymentRepository := configure(wrongDeployment)
	wrongCapabilityRepository := configure(wrongCapability)
	claimInput := attachments.ProcessorClaimInput{OwnerID: "processor_admission", OwnerLeaseDuration: time.Minute}
	assertUnchanged := func(before string, beforeXID string) {
		t.Helper()
		assertAttachmentLifecycleSnapshot(t, ctx, fixture.db, before)
		var afterXID string
		if err := fixture.db.QueryRow(ctx, `select xmin::text from public.attachment_processor_jobs where processor_job_id=$1`, seed.ProcessorJobID).Scan(&afterXID); err != nil || afterXID != beforeXID {
			t.Fatalf("job changed: xmin before=%s after=%s error=%v", beforeXID, afterXID, err)
		}
	}
	assertDenied := func(repo *PostgresAttachmentRepository, claim *attachments.ProcessorClaim) {
		t.Helper()
		before := attachmentLifecycleSnapshot(t, ctx, fixture.db)
		var beforeXID string
		if err := fixture.db.QueryRow(ctx, `select xmin::text from public.attachment_processor_jobs where processor_job_id=$1`, seed.ProcessorJobID).Scan(&beforeXID); err != nil {
			t.Fatal(err)
		}
		if claim == nil {
			if got, err := repo.ClaimProcessorJob(ctx, claimInput); got != nil || !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("claim with refused worker = (%#v, %v)", got, err)
			}
		} else {
			if _, err := repo.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: *claim, OwnerLeaseDuration: 5 * time.Minute}); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("renew with refused worker = %v", err)
			}
			result := attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeScannerUnavailable}
			if _, err := repo.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{Claim: *claim, Result: result, RetryAt: time.Now().UTC().Add(time.Minute), Limits: attachments.DefaultLimits()}); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("complete with refused worker = %v", err)
			}
		}
		assertUnchanged(before, beforeXID)
	}
	setMembership := func(expression string) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, `update public.deployment_membership set `+expression+` where instance_id='compose-processor'`); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []struct{ name, expression string }{
		{"pending", "queue_admitted=false"},
		{"expired", "queue_admitted=true, heartbeat_expires_at=created_at+interval '1 microsecond'"},
		{"stale epoch", "queue_admitted=true, heartbeat_expires_at=clock_timestamp()+interval '1 hour', deployment_epoch=deployment_epoch+1"},
	} {
		t.Run(state.name+" claim", func(t *testing.T) {
			setMembership(state.expression)
			assertDenied(repository, nil)
			if _, err := fixture.db.Exec(ctx, `update public.deployment_membership set queue_admitted=true, heartbeat_expires_at=clock_timestamp()+interval '1 hour', deployment_epoch=$1 where instance_id='compose-processor'`, epoch); err != nil {
				t.Fatal(err)
			}
		})
	}
	assertDenied(wrong, nil)
	assertDenied(wrongDeploymentRepository, nil)
	assertDenied(wrongCapabilityRepository, nil)
	assertDenied(centerLaunch, nil)
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	assertDenied(repository, nil)
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	// A worker may advertise a newer fence contract than the active minimum;
	// claim, renewal and completion must all accept that current member.
	if _, err := fixture.db.Exec(ctx, `update public.deployment_membership set fence_contract_version=$1 where instance_id='compose-processor'`, fence+1); err != nil {
		t.Fatal(err)
	}
	claim, err := repository.ClaimProcessorJob(ctx, claimInput)
	if err != nil || claim == nil || claim.ProcessorJobID != seed.ProcessorJobID {
		t.Fatalf("admitted claim = (%#v, %v)", claim, err)
	}
	for _, state := range []struct{ name, expression string }{
		{"revoked", "queue_admitted=false"},
		{"expired", "queue_admitted=true, heartbeat_expires_at=created_at+interval '1 microsecond'"},
		{"stale epoch", "queue_admitted=true, heartbeat_expires_at=clock_timestamp()+interval '1 hour', deployment_epoch=deployment_epoch+1"},
	} {
		t.Run(state.name+" renew and complete", func(t *testing.T) {
			setMembership(state.expression)
			assertDenied(repository, claim)
			stale := *claim
			stale.OwnerID = "processor_stale"
			assertDenied(repository, &stale)
			if _, err := fixture.db.Exec(ctx, `update public.deployment_membership set queue_admitted=true, heartbeat_expires_at=clock_timestamp()+interval '1 hour', deployment_epoch=$1 where instance_id='compose-processor'`, epoch); err != nil {
				t.Fatal(err)
			}
		})
	}
	assertDenied(wrong, claim)
	assertDenied(wrongDeploymentRepository, claim)
	assertDenied(wrongCapabilityRepository, claim)
	assertDenied(centerLaunch, claim)
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	before := attachmentLifecycleSnapshot(t, ctx, fixture.db)
	var beforeXID string
	if err := fixture.db.QueryRow(ctx, `select xmin::text from public.attachment_processor_jobs where processor_job_id=$1`, seed.ProcessorJobID).Scan(&beforeXID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: *claim, OwnerLeaseDuration: 5 * time.Minute}); err == nil {
		t.Fatal("closed launch renewed a claim")
	}
	closedResult := attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeScannerUnavailable}
	if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{Claim: *claim, Result: closedResult, RetryAt: time.Now().UTC().Add(time.Minute), Limits: attachments.DefaultLimits()}); err == nil {
		t.Fatal("closed launch completed a claim")
	}
	assertUnchanged(before, beforeXID)
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	renewed, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: *claim, OwnerLeaseDuration: 5 * time.Minute})
	if err != nil || !renewed.LeaseExpiresAt.After(claim.LeaseExpiresAt) {
		t.Fatalf("admitted renewal = (%#v, %v)", renewed, err)
	}
	result := attachments.ProcessorResult{Source: renewed.Source, Profile: renewed.Profile, Code: attachments.ProcessorResultCodeScannerUnavailable}
	completed, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{Claim: renewed, Result: result, RetryAt: time.Now().UTC().Add(time.Minute), Limits: attachments.DefaultLimits()})
	if err != nil || completed.ProcessorState != attachments.ProcessorStateRetryWait {
		t.Fatalf("admitted completion = (%#v, %v)", completed, err)
	}
}

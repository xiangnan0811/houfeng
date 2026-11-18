package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordbirth"
)

func TestPostgresIntegrationProcessorRenewalLaunchAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal, local.local)
	if err != nil {
		t.Fatal(err)
	}
	seed := seedManagedAttachmentProcessor(t, ctx, fixture, local.repository, writer, "renewlaunch", []byte("processor renewal launch admission"))
	claim, err := local.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_renewlaunch", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claim == nil || claim.ProcessorJobID != seed.ProcessorJobID {
		t.Fatalf("claim processor job = (%#v, %v)", claim, err)
	}

	// Stage canonical bindings through the owner command against the real
	// activated fixture. The binary digests identify fixture launches; this
	// exercises database admission, not executable or filesystem verification.
	var deploymentID string
	var activation []byte
	var enrolled bool
	if err := fixture.db.QueryRow(ctx, `select deployment_id, activation_bundle_digest,
		exists(select 1 from public.source_authority_admission where singleton)
		from public.deployment_contract_state where project_id='default'`).Scan(&deploymentID, &activation, &enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled || len(activation) != sha256.Size {
		t.Fatal("launch fixture requires an activated, never-enrolled source")
	}
	launch := recordbirth.LaunchBinding{
		ProtocolVersion: 1, ProcessKind: "center", DeploymentID: deploymentID,
		BinaryDigest: sha256.Sum256([]byte("renewal fixture center executable")),
	}
	copy(launch.ActivationBundleDigest[:], activation)
	center, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		t.Fatal(err)
	}
	launch.ProcessKind = "content-processor"
	launch.BinaryDigest = sha256.Sum256([]byte("renewal fixture processor executable"))
	processor, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		t.Fatal(err)
	}
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator, "renewal-launch-owner", 1)
	var revision int64
	if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`,
		center, processor, fixture.runtime, fixture.deletionWorker).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("stage launch revision=%d: %v", revision, err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatalf("open launch: %v", err)
	}

	openRuntime := func(digest [32]byte, application string) *pgxpool.Pool {
		t.Helper()
		config := fixture.directRoleConfig(t, fixture.runtime, application)
		config.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = hex.EncodeToString(digest[:])
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	runtime := openRuntime(sha256.Sum256(processor), "renewal-launch-current")
	repository := NewPostgresAttachmentRepository(runtime)
	renewed, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: *claim, OwnerLeaseDuration: 3 * time.Minute})
	if err != nil || !renewed.LeaseExpiresAt.After(claim.LeaseExpiresAt) || renewed.LeaseExpiresAt.After(renewed.ExpiresAt) {
		t.Fatalf("current launch renewal = (%#v, %v)", renewed, err)
	}
	want := *claim
	want.LeaseExpiresAt = renewed.LeaseExpiresAt
	if renewed != want {
		t.Fatalf("renewal changed claim identity: got %#v, want %#v", renewed, want)
	}

	// A runtime principal cannot close the owner-managed registry.
	if _, err := runtime.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err == nil {
		t.Fatal("runtime principal closed launch registry")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("runtime launch close error = %v, want 42501", err)
		}
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err != nil {
		t.Fatalf("close launch: %v", err)
	}
	before := attachmentLifecycleSnapshot(t, ctx, fixture.db)
	var beforeXID string
	if err := fixture.db.QueryRow(ctx, `select xmin::text from public.attachment_processor_jobs where processor_job_id=$1`, renewed.ProcessorJobID).Scan(&beforeXID); err != nil {
		t.Fatal(err)
	}
	assertRefused := func(repo *PostgresAttachmentRepository, label string) {
		t.Helper()
		_, err := repo.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: renewed, OwnerLeaseDuration: 5 * time.Minute})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("%s renewal error = %v, want launch admission SQLSTATE 42501", label, err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, fixture.db, before)
		var afterXID string
		if err := fixture.db.QueryRow(ctx, `select xmin::text from public.attachment_processor_jobs where processor_job_id=$1`, renewed.ProcessorJobID).Scan(&afterXID); err != nil {
			t.Fatal(err)
		}
		if afterXID != beforeXID {
			t.Fatalf("%s renewal wrote job row: xmin %s -> %s", label, beforeXID, afterXID)
		}
	}
	assertRefused(repository, "closed launch")
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatalf("reopen exact launch: %v", err)
	}
	stale := NewPostgresAttachmentRepository(openRuntime(sha256.Sum256([]byte("stale fixture launch")), "renewal-launch-stale"))
	assertRefused(stale, "stale launch")

	wrongOwner := renewed
	wrongOwner.OwnerID = "processor_renewlaunch_other"
	if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: wrongOwner, OwnerLeaseDuration: 5 * time.Minute}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("wrong owner renewal error = %v, want claim lost", err)
	}
	assertAttachmentLifecycleSnapshot(t, ctx, fixture.db, before)
	resumed, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: renewed, OwnerLeaseDuration: 5 * time.Minute})
	if err != nil || !resumed.LeaseExpiresAt.After(renewed.LeaseExpiresAt) || resumed.LeaseExpiresAt.After(resumed.ExpiresAt) {
		t.Fatalf("reopened exact launch renewal = (%#v, %v)", resumed, err)
	}
	want = renewed
	want.LeaseExpiresAt = resumed.LeaseExpiresAt
	if resumed != want {
		t.Fatalf("resumed renewal changed claim identity: got %#v, want %#v", resumed, want)
	}
}

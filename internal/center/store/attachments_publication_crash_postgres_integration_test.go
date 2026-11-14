package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

type attachmentPublicationCrashRow struct {
	state             attachments.BlobPublicationState
	outcome           *attachments.BlobPublicationCompletionOutcome
	objectVersion     *string
	cleanupGeneration int64
	attempt           int64
	cleanupOwner      *string
	cleanupLease      *time.Time
	receiptDigest     []byte
	completedAt       *time.Time
	retryAt           *time.Time
}

type attachmentPublicationCrashBackend interface {
	attachments.BlobStore
	attachments.BlobPublicationResolver
}

type attachmentPublicationCrashBackendFactory func(t *testing.T) attachmentPublicationCrashBackend

type attachmentPublicationCrashHarness struct {
	backendKind       attachments.BackendKind
	transportKind     attachments.TransportKind
	newBackendFactory func(t *testing.T, fixture recordPlatformPostgresFixture) attachmentPublicationCrashBackendFactory
}

type attachmentPublicationCrashUpload struct {
	reserve      attachments.ReserveUploadCommand
	temporaryKey string
}

func TestPostgresIntegrationAttachmentBlobPublicationPrepareReplayUsesCurrentActiveIntent(t *testing.T) {
	ctx := context.Background()
	f := newLocalPublicationFixture(t, ctx)
	content := []byte("publication prepare replay current active intent\n")
	historical := f.prepare(t, ctx, "aup_pubpreparehistory", content)
	f.expire(t, ctx, historical)
	request := f.discover(t, ctx, historical, "publication_prepare_replay")
	completed := f.cleanup(t, ctx, request, "publication_prepare_replay")
	if *completed.Outcome != "already_absent" {
		t.Fatal("unwritten historical intent did not complete from physical absence")
	}
	current := f.prepare(t, ctx, "aup_pubpreparecurrent", content)
	replayed, err := f.repository.PrepareBlobPublication(ctx, current.request)
	if err != nil || replayed.PublicationID != current.intent.PublicationID || replayed.PublicationID == historical.intent.PublicationID || replayed.State != attachments.BlobPublicationStatePrepared {
		t.Fatalf("current prepared intent replay = %#v, %v", replayed, err)
	}
	prepareAttachmentPublicationCrashUpload(t, ctx, f.fixture, f.repository, attachmentPublicationCrashHarness{backendKind: attachments.BackendKindLocal, transportKind: attachments.TransportKindLocal}, "aup_pubprepareconflict", content)
	conflicting := current.request
	conflicting.OwnerID = "aup_pubprepareconflict"
	if _, err := f.repository.PrepareBlobPublication(ctx, conflicting); !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("different active owner error = %v, want ErrBlobPublicationConflict", err)
	}
}

func TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocal(t *testing.T) {
	runLocalPublicationRestart(t)
}

func TestPostgresMinIOIntegrationAttachmentBlobPublicationCrashRestartS3(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 and HOUFENG_MINIO_INTEGRATION=1 to run the real PostgreSQL + MinIO publication crash test")
	}
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	harness := attachmentPublicationCrashHarness{
		backendKind:   attachments.BackendKindS3,
		transportKind: attachments.TransportKindS3,
		newBackendFactory: func(t *testing.T, _ recordPlatformPostgresFixture) attachmentPublicationCrashBackendFactory {
			t.Helper()
			return func(t *testing.T) attachmentPublicationCrashBackend {
				t.Helper()
				blob, err := attachments.NewS3BlobStore(client, bucket)
				if err != nil {
					t.Fatalf("NewS3BlobStore() error = %v", err)
				}
				return blob
			}
		},
	}
	fixture := runAttachmentPublicationCrashRestart(t, harness)
	// The expected retry_wait remains durable; do not converge or complete it
	// merely to make another scenario's unscoped RunOnce claim deterministic.
	t.Run("bound_noncurrent_version_stays_pending", func(t *testing.T) {
		runAttachmentPublicationCrashNoncurrentVersion(t, context.Background(), fixture, harness, client, bucket)
	})
}

func TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocalReferenceFences(t *testing.T) {
	runLocalPublicationReferenceFences(t)
}

func runAttachmentPublicationCrashReferenceLockWait(t *testing.T, surface string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := newLocalPublicationFixture(t, ctx)
	content := []byte("publication reference lock wait " + surface)
	retained := f.prepare(t, ctx, "aup_lockretained", content)
	f.put(t, ctx, &retained, content)
	f.bind(t, ctx, &retained)
	f.consume(t, ctx, retained)
	a := f.prepare(t, ctx, "aup_locklate", content)
	f.put(t, ctx, &a, content)
	f.bind(t, ctx, &a)
	f.expire(t, ctx, a)
	request := f.discover(t, ctx, a, "publication_lock_wait")
	newPool := func(application string) *pgxpool.Pool {
		config := f.fixture.directRoleConfig(t, f.fixture.runtime, application)
		config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
		config.MaxConns = 1
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	const writerApplication = "publication-reference-lock-writer"
	writerPool := newPool(writerApplication)
	claimPool := newPool("publication-reference-lock-claim")
	snapshotReady, publishRelease := make(chan struct{}), make(chan struct{})
	claimEntered, claimRelease := make(chan struct{}), make(chan struct{})
	publisherReleased, claimantReleased := false, false
	defer func() {
		if !publisherReleased {
			close(publishRelease)
		}
		if !claimantReleased {
			close(claimRelease)
		}
	}()
	writerBarrierUsed := false
	beginWriter := func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := writerPool.BeginTx(ctx, options)
		if err != nil {
			return nil, err
		}
		// Discovery is read-only; only the first write attempt waits at the
		// pre-claim barrier, while route retries pass through.
		if options.AccessMode == pgx.ReadOnly || writerBarrierUsed {
			return tx, nil
		}
		writerBarrierUsed = true
		var tasks int
		if err := tx.QueryRow(ctx, `select count(*) from public.blob_gc_deletions where namespace_id=$1::uuid and blob_key=$2`, request.NamespaceID, request.Key).Scan(&tasks); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("read pre-claim snapshot: %w", err)
		}
		if tasks != 0 {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("pre-claim snapshot has %d tasks, want 0", tasks)
		}
		close(snapshotReady)
		select {
		case <-publishRelease:
			return tx, nil
		case <-ctx.Done():
			_ = tx.Rollback(ctx)
			return nil, ctx.Err()
		}
	}
	writerDone := make(chan error, 1)
	go func() {
		if surface == "upload_part" {
			repository := NewPostgresAttachmentRepository(writerPool)
			repository.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
				return beginWriter(ctx, options)
			}
			_, err := repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{
				ProjectID: "default", UploadID: a.upload.reserve.UploadID, AuthorID: a.upload.reserve.AuthorID,
				Object: a.written.Version, PublicationIntent: a.intent,
			})
			writerDone <- err
			return
		}
		tx, err := beginWriter(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			writerDone <- err
			return
		}
		_, err = registerDraftUploadPartBlobs(ctx, tx, retained.upload.reserve.DraftID)
		writerDone <- errors.Join(err, tx.Rollback(ctx))
	}()
	select {
	case <-snapshotReady:
	case err := <-writerDone:
		t.Fatalf("publisher failed before snapshot barrier: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	interruption := &localPublicationInterruption{PostgresLocalBlobRepository: NewPostgresLocalBlobRepository(claimPool, nil, nil), beforeBinding: true}
	claimRepository := &localPublicationClaimBarrier{localPublicationInterruption: interruption, entered: claimEntered, release: claimRelease}
	coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, claimRepository)
	if err != nil {
		t.Fatal(err)
	}
	claimDone := make(chan error, 1)
	go func() { _, err := coordinator.Cleanup(ctx, request, "publication_lock_wait"); claimDone <- err }()
	select {
	case <-claimEntered:
	case err := <-claimDone:
		t.Fatalf("cleanup failed before claim commit barrier: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(publishRelease)
	publisherReleased = true
	// The ranked upload writer waits for the claim's quota lock before it
	// checks expiry; metadata also waits on the claim's locks.
	waitForRecordPlatformBackendLock(t, ctx, f.fixture.db, writerApplication)
	close(claimRelease)
	claimantReleased = true
	claimErr := waitForRecordPlatformResult(t, claimDone, "local claim commit")
	publishErr := waitForRecordPlatformResult(t, writerDone, "publication after local claim commit")
	if !errors.Is(claimErr, errLocalPublicationInterrupted) {
		t.Fatalf("claim boundary result: %v", claimErr)
	}
	if surface == "metadata" && !errors.Is(publishErr, records.ErrDraftAttachmentsBusy) || surface == "upload_part" && !errors.Is(publishErr, attachments.ErrUploadExpired) {
		t.Fatalf("late %s publication was not rejected: %v", surface, publishErr)
	}
	var claimed bool
	if err := f.fixture.db.QueryRow(ctx, `select deletion_state='claimed' and owner_id='publication_lock_wait' and owner_generation=1 and cleanup_dispatch_id is not null and not final_identity_bound and receipt_digest is null from public.blob_gc_deletions where deletion_id=$1`, request.DeletionID).Scan(&claimed); err != nil || !claimed {
		t.Fatalf("durable claim changed: %t %v", claimed, err)
	}
	assertAttachmentPublicationCrashReference(t, ctx, f.fixture, retained.upload.reserve.UploadID, retained.written.Version)
	var lateParts int
	if err := f.fixture.db.QueryRow(ctx, `select count(*) from public.attachment_upload_parts where upload_id=$1`, a.upload.reserve.UploadID).Scan(&lateParts); err != nil || lateParts != 0 {
		t.Fatalf("late part published: %d %v", lateParts, err)
	}
	got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, a.written.Version)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("reference race changed exact bytes: %q %v", got, err)
	}
}

func runAttachmentPublicationCrashRestart(t *testing.T, harness attachmentPublicationCrashHarness) recordPlatformPostgresFixture {
	t.Helper()
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)

	t.Run("before_publish", func(t *testing.T) {
		content := []byte("local publication crash before publish\n")
		request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashbefore", harness.backendKind)
		newBackend := harness.newBackendFactory(t, fixture)
		initialRepository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, "publication-crash-before-prepare", 1),
		)
		prepareS3AttachmentPublicationCrashUpload(
			t, ctx, fixture, initialRepository, harness, request.OwnerID, content,
		)
		intent, err := initialRepository.PrepareBlobPublication(ctx, request)
		if err != nil {
			t.Fatalf("PrepareBlobPublication() error = %v", err)
		}

		_, blob, reconciler := newAttachmentPublicationCrashReconciler(
			t, ctx, fixture, harness.backendKind, newBackend, "publication_crash_before", nil,
		)
		convergeAttachmentPublicationCrash(t, ctx, fixture, reconciler, intent.PublicationID)
		row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
		assertAttachmentPublicationCrashTerminal(
			t, row, attachments.BlobPublicationCompletionOutcomeAlreadyAbsent, nil,
		)
		assertAttachmentPublicationCrashNoReferences(t, ctx, fixture, request.Target, "")
		if _, err := blob.ResolveBlobPublicationObject(ctx, request.Target); !errors.Is(err, attachments.ErrBlobNotFound) {
			t.Fatalf("ResolveBlobPublicationObject(absent final key) error = %v, want ErrBlobNotFound", err)
		}
	})

	t.Run("after_publish", func(t *testing.T) {
		content := []byte("local publication crash after publish\n")
		request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashpublish", harness.backendKind)
		newBackend := harness.newBackendFactory(t, fixture)
		initialRepository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, "publication-crash-after-publish-prepare", 1),
		)
		upload := prepareS3AttachmentPublicationCrashUpload(
			t, ctx, fixture, initialRepository, harness, request.OwnerID, content,
		)
		intent, err := initialRepository.PrepareBlobPublication(ctx, request)
		if err != nil {
			t.Fatalf("PrepareBlobPublication() error = %v", err)
		}
		initialBlob := newBackend(t)
		written := putManagedAttachmentPublicationCrash(t, ctx, fixture, initialBlob, request, intent, upload, content)
		version := written.Version

		_, blob, reconciler := newAttachmentPublicationCrashReconciler(
			t, ctx, fixture, harness.backendKind, newBackend, "publication_crash_after_publish", nil,
		)
		resolved, err := blob.ResolveBlobPublicationObject(ctx, request.Target)
		if err != nil || resolved != version {
			t.Fatalf("ResolveBlobPublicationObject(published exact key) = (%#v, %v), want %#v", resolved, err, version)
		}
		convergeAttachmentPublicationCrash(t, ctx, fixture, reconciler, intent.PublicationID)
		row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
		assertAttachmentPublicationCrashTerminal(
			t, row, attachments.BlobPublicationCompletionOutcomeDeleted, &version.VersionID,
		)
		assertAttachmentPublicationCrashNoReferences(t, ctx, fixture, request.Target, version.VersionID)
		assertAttachmentPublicationCrashObjectAbsent(t, ctx, blob, version)
	})

	t.Run("after_version_cas", func(t *testing.T) {
		content := []byte("local publication crash after version cas\n")
		request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashversion", harness.backendKind)
		newBackend := harness.newBackendFactory(t, fixture)
		initialRepository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, "publication-crash-after-version-prepare", 1),
		)
		upload := prepareS3AttachmentPublicationCrashUpload(
			t, ctx, fixture, initialRepository, harness, request.OwnerID, content,
		)
		intent, err := initialRepository.PrepareBlobPublication(ctx, request)
		if err != nil {
			t.Fatalf("PrepareBlobPublication() error = %v", err)
		}
		initialBlob := newBackend(t)
		written := putManagedAttachmentPublicationCrash(t, ctx, fixture, initialBlob, request, intent, upload, content)
		version := written.Version
		published, err := initialRepository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
			Intent: intent, Object: version, ObligationID: written.ObligationID,
		})
		if err != nil {
			t.Fatalf("RecordBlobPublicationVersion() error = %v", err)
		}
		if published.ObjectVersion != version.VersionID || published.State != attachments.BlobPublicationStatePublished {
			t.Fatalf("RecordBlobPublicationVersion() = %#v, want exact version %q", published, version.VersionID)
		}

		_, blob, reconciler := newAttachmentPublicationCrashReconciler(
			t, ctx, fixture, harness.backendKind, newBackend, "publication_crash_after_version", nil,
		)
		convergeAttachmentPublicationCrash(t, ctx, fixture, reconciler, intent.PublicationID)
		row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
		assertAttachmentPublicationCrashTerminal(
			t, row, attachments.BlobPublicationCompletionOutcomeDeleted, &version.VersionID,
		)
		assertAttachmentPublicationCrashNoReferences(t, ctx, fixture, request.Target, version.VersionID)
		assertAttachmentPublicationCrashObjectAbsent(t, ctx, blob, version)
	})

	t.Run("after_metadata_commit", func(t *testing.T) {
		content := []byte("local publication crash after metadata commit\n")
		request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashmeta", harness.backendKind)
		newBackend := harness.newBackendFactory(t, fixture)
		initialRepository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, "publication-crash-metadata-prepare", 1),
		)
		upload := prepareAttachmentPublicationCrashUpload(
			t, ctx, fixture, initialRepository, harness, request.OwnerID, content,
		)
		intent, err := initialRepository.PrepareBlobPublication(ctx, request)
		if err != nil {
			t.Fatalf("PrepareBlobPublication() error = %v", err)
		}
		initialBlob := newBackend(t)
		written := putManagedAttachmentPublicationCrash(t, ctx, fixture, initialBlob, request, intent, upload, content)
		version := written.Version
		published, err := initialRepository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
			Intent: intent, Object: version, ObligationID: written.ObligationID,
		})
		if err != nil {
			t.Fatalf("RecordBlobPublicationVersion() error = %v", err)
		}
		if _, err := initialRepository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{
			ProjectID: "default", UploadID: upload.reserve.UploadID, AuthorID: upload.reserve.AuthorID,
			TemporaryObjectKey: upload.temporaryKey, Object: version, PublicationIntent: published,
		}); err != nil {
			t.Fatalf("RecordUploadedContent() error = %v", err)
		}

		_, blob, reconciler := newAttachmentPublicationCrashReconciler(
			t, ctx, fixture, harness.backendKind, newBackend, "publication_crash_metadata", nil,
		)
		convergeAttachmentPublicationCrash(t, ctx, fixture, reconciler, intent.PublicationID)
		row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
		assertAttachmentPublicationCrashTerminal(
			t, row, attachments.BlobPublicationCompletionOutcomeConsumed, &version.VersionID,
		)
		assertAttachmentPublicationCrashReference(t, ctx, fixture, upload.reserve.UploadID, version)
		info, err := blob.Stat(ctx, version)
		if err != nil || info.Version != version {
			t.Fatalf("BlobStore.Stat(referenced exact version) = (%#v, %v), want %#v", info, err, version)
		}
	})

	t.Run("after_physical_cleanup", func(t *testing.T) {
		content := []byte("local publication crash after physical cleanup\n")
		request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashpurge", harness.backendKind)
		newBackend := harness.newBackendFactory(t, fixture)
		initialRepository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, "publication-crash-physical-prepare", 1),
		)
		upload := prepareS3AttachmentPublicationCrashUpload(
			t, ctx, fixture, initialRepository, harness, request.OwnerID, content,
		)
		intent, err := initialRepository.PrepareBlobPublication(ctx, request)
		if err != nil {
			t.Fatalf("PrepareBlobPublication() error = %v", err)
		}
		initialBlob := newBackend(t)
		written := putManagedAttachmentPublicationCrash(t, ctx, fixture, initialBlob, request, intent, upload, content)
		version := written.Version
		published, err := initialRepository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
			Intent: intent, Object: version, ObligationID: written.ObligationID,
		})
		if err != nil {
			t.Fatalf("RecordBlobPublicationVersion() error = %v", err)
		}

		injected := errors.New("injected crash after physical publication purge")
		firstRepository, firstBlob, firstReconciler := newAttachmentPublicationCrashReconciler(
			t,
			ctx,
			fixture,
			harness.backendKind,
			newBackend,
			"publication_crash_physical_first",
			func(cutpoint attachments.BlobPublicationReconcilerCutpoint) error {
				if cutpoint == attachments.BlobPublicationReconcilerCutpointAfterPhysicalPurge {
					return injected
				}
				return nil
			},
		)
		expireAttachmentPublicationCrashIntent(t, ctx, fixture, intent.PublicationID)
		worked, err := firstReconciler.RunOnce(ctx)
		if !worked || !errors.Is(err, injected) {
			t.Fatalf("BlobPublicationReconciler.RunOnce(first crash) = (%t, %v), want true/%v", worked, err, injected)
		}
		firstRow := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
		if firstRow.state != attachments.BlobPublicationStateCleanupClaimed || firstRow.outcome != nil ||
			firstRow.objectVersion == nil || *firstRow.objectVersion != version.VersionID ||
			firstRow.cleanupOwner == nil || *firstRow.cleanupOwner != "publication_crash_physical_first" ||
			firstRow.cleanupGeneration <= 0 || firstRow.attempt <= 0 || firstRow.cleanupLease == nil {
			t.Fatalf("first physical-cleanup crash row = %#v", firstRow)
		}
		assertAttachmentPublicationCrashObjectAbsent(t, ctx, firstBlob, version)
		assertAttachmentPublicationCrashNoReferences(t, ctx, fixture, request.Target, version.VersionID)

		firstClaimIntent := published
		firstClaimIntent.State = attachments.BlobPublicationStateCleanupClaimed
		firstClaim := attachments.BlobPublicationCleanupClaim{
			Intent:                 firstClaimIntent,
			CleanupOwnerID:         *firstRow.cleanupOwner,
			CleanupGeneration:      firstRow.cleanupGeneration,
			Attempt:                firstRow.attempt,
			ObservedLeaseExpiresAt: firstRow.cleanupLease.UTC(),
		}
		if firstClaim.Validate() != nil {
			t.Fatalf("reconstructed first durable cleanup claim = %#v", firstClaim)
		}
		expireAttachmentPublicationCrashLease(t, ctx, fixture, firstClaim)

		_, blob, reconciler := newAttachmentPublicationCrashReconciler(
			t, ctx, fixture, harness.backendKind, newBackend, "publication_crash_physical_second", nil,
		)
		convergeAttachmentPublicationCrash(t, ctx, fixture, reconciler, intent.PublicationID)
		row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
		assertAttachmentPublicationCrashTerminal(
			t, row, attachments.BlobPublicationCompletionOutcomeAlreadyAbsent, &version.VersionID,
		)
		if row.cleanupGeneration <= firstRow.cleanupGeneration || row.attempt <= firstRow.attempt ||
			row.cleanupOwner == nil || *row.cleanupOwner != "publication_crash_physical_second" {
			t.Fatalf("takeover cleanup identity = %#v, first = %#v", row, firstRow)
		}
		assertAttachmentPublicationCrashObjectAbsent(t, ctx, blob, version)
		assertAttachmentPublicationCrashNoReferences(t, ctx, fixture, request.Target, version.VersionID)

		_, err = firstRepository.CompleteBlobPublicationCleanup(ctx, attachments.BlobPublicationCleanupCompletionRequest{
			Claim: firstClaim, Outcome: attachments.BlobPublicationCompletionOutcomeDeleted,
			Receipt: attachments.DeletionReceipt{Version: version, Deleted: true},
		})
		if !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
			t.Fatalf("CompleteBlobPublicationCleanup(stale first claim) error = %v, want ErrBlobPublicationClaimLost", err)
		}
	})
	return fixture
}

func runAttachmentPublicationCrashSharedObject(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	harness attachmentPublicationCrashHarness,
) {
	t.Helper()
	content := []byte("retained draft shared publication object\n")
	request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashshared", harness.backendKind)
	newBackend := harness.newBackendFactory(t, fixture)
	initialBlob := newBackend(t)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "publication-crash-shared-prepare", 1),
	)
	writer := newAttachmentPublicationCrashWriter(t, ctx, fixture, initialBlob, harness.backendKind)
	version := seedPostgresManagedBlob(t, ctx, fixture, writer, content).Version
	object := attachmentBlobObjectFromVersion(version, harness.backendKind)
	seedAttachmentBlobGCObject(t, ctx, fixture, object, time.Now().UTC())
	seedAttachmentBlobGCOriginalReference(t, ctx, fixture, "pubcrashretained", object)
	protection, err := repository.CreateBlobGCPin(ctx, attachments.CreateBlobGCPinCommand{
		PinID: "bgp_pubcrashretained", OwnerKind: attachments.BlobGCPinOwnerBackupManifest,
		OwnerID: "backup_pubcrashretained", BlobKey: version.Key, BlobObjectVersion: version.VersionID,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateBlobGCPin(retained object) error = %v", err)
	}
	baselineBytes, baselineReadErr := readAttachmentPublicationCrashBytes(ctx, initialBlob, version)
	t.Logf("retained baseline: version=%#v protection=%#v bytes=%q read_error=%v",
		version, protection, baselineBytes, baselineReadErr)
	if baselineReadErr != nil || !bytes.Equal(baselineBytes, content) ||
		protection.LogicalAttachmentCount != 1 || protection.RevisionReferenceCount != 0 ||
		protection.ActivePinCount != 1 || !protection.Protected {
		t.Fatalf("retained draft object baseline does not have exact bytes, one logical ref and one active pin")
	}

	upload := prepareAttachmentPublicationCrashUpload(t, ctx, fixture, repository, harness, request.OwnerID, content)
	intent, err := repository.PrepareBlobPublication(ctx, request)
	if err != nil {
		t.Fatalf("PrepareBlobPublication(shared second owner) error = %v", err)
	}
	written := putManagedAttachmentPublicationCrash(t, ctx, fixture, initialBlob, request, intent, upload, content)
	deduplicated := written.Version
	// Deliberately crash before RecordBlobPublicationVersion/RecordUploadedContent:
	// this intent has no right to destroy the already retained dedup object.
	before := observeAttachmentPublicationCrashObject(t, ctx, fixture, "before shared reconciliation", intent, version)
	beforeRow := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
	t.Logf("shared dedup Put: original=%#v returned=%#v", version, deduplicated)
	if deduplicated != version || !before.publicationMatches ||
		beforeRow.state != attachments.BlobPublicationStatePrepared || beforeRow.objectVersion != nil ||
		beforeRow.outcome != nil || beforeRow.receiptDigest != nil || beforeRow.completedAt != nil ||
		beforeRow.cleanupGeneration != 0 || beforeRow.attempt != 0 ||
		beforeRow.cleanupOwner == nil || *beforeRow.cleanupOwner != "" ||
		beforeRow.cleanupLease != nil || beforeRow.retryAt != nil ||
		before.metadataCount != 1 || before.exactMetadataCount != 1 ||
		before.logicalRefs != 1 || before.revisionRefs != 0 || before.parts != 0 ||
		before.ownerParts != 0 || before.activePins != 1 {
		t.Fatalf("shared publication baseline is not an unbound prepared intent over the retained exact object")
	}

	const cleanupOwner = "publication_crash_shared"
	_, blob, reconciler := newAttachmentPublicationCrashReconciler(
		t, ctx, fixture, harness.backendKind, newBackend, cleanupOwner, nil,
	)
	expireAttachmentPublicationCrashIntent(t, ctx, fixture, intent.PublicationID)
	worked, runErr := reconciler.RunOnce(ctx)
	t.Logf("shared ordinary reconciler: publication=%q worked=%t error=%v", intent.PublicationID, worked, runErr)
	after := observeAttachmentPublicationCrashObject(t, ctx, fixture, "after shared reconciliation", intent, version)
	row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
	got, readErr := readAttachmentPublicationCrashBytes(ctx, blob, version)
	t.Logf("shared retained readback: version=%#v bytes=%q error=%v", version, got, readErr)
	if readErr != nil || !bytes.Equal(got, content) {
		t.Errorf("retained shared bytes = %q, error = %v, want %q", got, readErr, content)
	}
	if !after.publicationMatches || after != before {
		t.Errorf("shared exact metadata/ref/part/pin facts changed: before=%+v after=%+v", before, after)
	}
	if worked || runErr != nil || row.state != attachments.BlobPublicationStatePrepared ||
		row.objectVersion != nil || row.cleanupOwner == nil || *row.cleanupOwner != "" ||
		row.cleanupGeneration != beforeRow.cleanupGeneration || row.attempt != beforeRow.attempt ||
		row.cleanupLease != nil || row.retryAt != nil ||
		row.outcome != nil || row.receiptDigest != nil || row.completedAt != nil {
		t.Errorf("retained unbound intent must remain prepared without claim, version or receipt: worked=%t error=%v row=%#v",
			worked, runErr, row)
	}
}

func runAttachmentPublicationCrashNoncurrentVersion(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	harness attachmentPublicationCrashHarness,
	client *minio.Client,
	bucket string,
) {
	t.Helper()
	content := []byte("bound noncurrent publication exact versions\n")
	request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashnoncurrent", harness.backendKind)
	newBackend := harness.newBackendFactory(t, fixture)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "publication-crash-noncurrent-prepare", 1),
	)
	upload := prepareAttachmentPublicationCrashUpload(t, ctx, fixture, repository, harness, request.OwnerID, content)
	intent, err := repository.PrepareBlobPublication(ctx, request)
	if err != nil {
		t.Fatalf("PrepareBlobPublication(noncurrent) error = %v", err)
	}
	initialBlob := newBackend(t)
	written := putManagedAttachmentPublicationCrash(t, ctx, fixture, initialBlob, request, intent, upload, content)
	v1 := written.Version
	published, err := repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
		Intent: intent, Object: v1, ObligationID: written.ObligationID,
	})
	if err != nil {
		t.Fatalf("RecordBlobPublicationVersion(V1) error = %v", err)
	}
	// Adversarial raw overwrite, not a product writer permission or GC grant.
	v2, err := client.PutObject(ctx, bucket, v1.Key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{})
	t.Logf("noncurrent setup: V1=%#v publication=%#v raw_V2=%#v error=%v", v1, published, v2, err)
	if err != nil || v1.VersionID == "" || v2.VersionID == "" || v2.VersionID == v1.VersionID {
		t.Fatalf("raw overwrite must create a distinct nonempty V2 version: %v", err)
	}
	before := observeAttachmentPublicationCrashObject(t, ctx, fixture, "before noncurrent reconciliation", intent, v1)
	beforeRow := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
	v1Info, v1Bytes, v1Err := readAttachmentPublicationCrashS3Version(ctx, client, bucket, v1.Key, v1.VersionID)
	v2Info, v2Bytes, v2Err := readAttachmentPublicationCrashS3Version(ctx, client, bucket, v1.Key, v2.VersionID)
	current, currentErr := client.StatObject(ctx, bucket, v1.Key, minio.StatObjectOptions{})
	t.Logf("noncurrent baseline: V1_info=%#v V1_bytes=%q V1_error=%v V2_info=%#v V2_bytes=%q V2_error=%v current=%#v current_error=%v",
		v1Info, v1Bytes, v1Err, v2Info, v2Bytes, v2Err, current, currentErr)
	if published.State != attachments.BlobPublicationStatePublished || published.ObjectVersion != v1.VersionID ||
		!before.publicationMatches || beforeRow.state != attachments.BlobPublicationStatePublished ||
		beforeRow.objectVersion == nil || *beforeRow.objectVersion != v1.VersionID ||
		beforeRow.outcome != nil || beforeRow.receiptDigest != nil || beforeRow.completedAt != nil ||
		beforeRow.cleanupGeneration != 0 || beforeRow.attempt != 0 ||
		before.metadataCount != 0 || before.logicalRefs != 0 || before.revisionRefs != 0 ||
		before.parts != 0 || before.ownerParts != 0 || before.activePins != 0 ||
		v1Err != nil || v2Err != nil || currentErr != nil ||
		v1Info.VersionID != v1.VersionID || v2Info.VersionID != v2.VersionID ||
		!bytes.Equal(v1Bytes, content) || !bytes.Equal(v2Bytes, content) || current.VersionID != v2.VersionID {
		t.Fatalf("noncurrent baseline must bind V1 without metadata/refs/parts/pins and retain both exact versions with V2 current")
	}

	const cleanupOwner = "publication_crash_noncurrent"
	_, _, reconciler := newAttachmentPublicationCrashReconciler(
		t, ctx, fixture, harness.backendKind, newBackend, cleanupOwner, nil,
	)
	expireAttachmentPublicationCrashIntent(t, ctx, fixture, intent.PublicationID)
	worked, runErr := reconciler.RunOnce(ctx)
	t.Logf("noncurrent ordinary reconciler: publication=%q worked=%t error=%v", intent.PublicationID, worked, runErr)
	after := observeAttachmentPublicationCrashObject(t, ctx, fixture, "after noncurrent reconciliation", intent, v1)
	row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
	v1Info, v1Bytes, v1Err = readAttachmentPublicationCrashS3Version(ctx, client, bucket, v1.Key, v1.VersionID)
	v2Info, v2Bytes, v2Err = readAttachmentPublicationCrashS3Version(ctx, client, bucket, v1.Key, v2.VersionID)
	current, currentErr = client.StatObject(ctx, bucket, v1.Key, minio.StatObjectOptions{})
	t.Logf("noncurrent readback: V1_info=%#v V1_bytes=%q V1_error=%v V2_info=%#v V2_bytes=%q V2_error=%v current=%#v current_error=%v",
		v1Info, v1Bytes, v1Err, v2Info, v2Bytes, v2Err, current, currentErr)
	if !worked || !errors.Is(runErr, attachments.ErrBlobVersionMismatch) {
		t.Errorf("ordinary RunOnce = (%t, %v), want true/ErrBlobVersionMismatch", worked, runErr)
	}
	if row.state != attachments.BlobPublicationStateRetryWait ||
		row.objectVersion == nil || *row.objectVersion != v1.VersionID ||
		row.outcome != nil || row.receiptDigest != nil || row.completedAt != nil || row.retryAt == nil ||
		row.cleanupOwner == nil || *row.cleanupOwner != cleanupOwner || row.cleanupLease == nil ||
		row.cleanupGeneration != beforeRow.cleanupGeneration+1 || row.attempt != beforeRow.attempt+1 {
		t.Errorf("bound noncurrent publication must remain pending with this exact claim and no terminal receipt: %#v", row)
	}
	if !after.publicationMatches || after != before {
		t.Errorf("noncurrent metadata/ref/part/pin facts changed: before=%+v after=%+v", before, after)
	}
	if v1Err != nil || v2Err != nil || currentErr != nil ||
		v1Info.VersionID != v1.VersionID || v2Info.VersionID != v2.VersionID ||
		!bytes.Equal(v1Bytes, content) || !bytes.Equal(v2Bytes, content) || current.VersionID != v2.VersionID {
		t.Errorf("ordinary cleanup must preserve exact V1/V2 bytes and leave V2 current")
	}
}

type attachmentPublicationCrashObjectFacts struct {
	publicationMatches bool
	metadataCount      int64
	exactMetadataCount int64
	logicalRefs        int64
	revisionRefs       int64
	parts              int64
	ownerParts         int64
	activePins         int64
	details            string
}

func observeAttachmentPublicationCrashObject(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	label string,
	intent attachments.BlobPublicationIntent,
	version attachments.ObjectVersion,
) attachmentPublicationCrashObjectFacts {
	t.Helper()
	var facts attachmentPublicationCrashObjectFacts
	var publication string
	if err := fixture.db.QueryRow(ctx, `
		select to_jsonb(publication)::text,
		       project_id = $2 and owner_kind = $3 and owner_id = $4 and owner_generation = $5
		       and blob_key = $6 and sha256_digest = $7 and size_bytes = $8 and backend_kind = $9
		from public.blob_publication_intents as publication
		where publication_id = $1`,
		intent.PublicationID, intent.ProjectID, intent.OwnerKind, intent.OwnerID, intent.OwnerGeneration,
		version.Key, version.SHA256[:], version.SizeBytes, intent.Target.BackendKind,
	).Scan(&publication, &facts.publicationMatches); err != nil {
		t.Fatalf("%s: read exact publication %q: %v", label, intent.PublicationID, err)
	}
	// JSON preserves nullable values and actual receipt bytes rather than logging
	// Go pointer addresses; include the entire persisted owner/claim/terminal row.
	t.Logf("%s: publication=%s", label, publication)
	if err := fixture.db.QueryRow(ctx, `
		with metadata as (
			select * from public.blob_objects where blob_key = $1
		), logical_refs as (
			select * from public.record_attachments
			where blob_key = $1 or preview_blob_key = $1
		), revision_refs as (
			select revision_ref.* from public.record_revision_attachments as revision_ref
			join logical_refs as attachment on attachment.attachment_id = revision_ref.attachment_id
		), parts as (
			select * from public.attachment_upload_parts where sha256_digest = $2
		), pins as (
			select * from public.blob_gc_pins where blob_key = $1
		)
		select (select count(*) from metadata),
		       (select count(*) from metadata where sha256_digest = $2 and object_version = $3
		        and size_bytes = $4 and backend_kind = $5),
		       (select count(*) from logical_refs),
		       (select count(*) from revision_refs),
		       (select count(*) from parts),
		       (select count(*) from public.attachment_upload_parts where upload_id = $6),
		       (select count(*) from pins where expires_at > transaction_timestamp()),
		       jsonb_build_object(
		         'metadata', coalesce((select jsonb_agg(to_jsonb(m) order by m.object_version) from metadata m), '[]'::jsonb),
		         'logical_refs', coalesce((select jsonb_agg(to_jsonb(a) order by a.attachment_id) from logical_refs a), '[]'::jsonb),
		         'revision_refs', coalesce((select jsonb_agg(to_jsonb(r) order by r.revision_id, r.ordinal) from revision_refs r), '[]'::jsonb),
		         'parts', coalesce((select jsonb_agg(to_jsonb(p) order by p.upload_id, p.part_number) from parts p), '[]'::jsonb),
		         'pins', coalesce((select jsonb_agg(to_jsonb(p) order by p.pin_id) from pins p), '[]'::jsonb)
		       )::text`,
		version.Key, version.SHA256[:], version.VersionID, version.SizeBytes, intent.Target.BackendKind, intent.OwnerID,
	).Scan(
		&facts.metadataCount, &facts.exactMetadataCount, &facts.logicalRefs, &facts.revisionRefs,
		&facts.parts, &facts.ownerParts, &facts.activePins, &facts.details,
	); err != nil {
		t.Fatalf("%s: read key-scoped facts for %q@%q: %v", label, version.Key, version.VersionID, err)
	}
	t.Logf("%s: key=%q version=%q publication_owner_matches=%t metadata=%d exact_metadata=%d logical_refs=%d revision_refs=%d parts=%d owner_parts=%d active_pins=%d facts=%s",
		label, version.Key, version.VersionID, facts.publicationMatches,
		facts.metadataCount, facts.exactMetadataCount, facts.logicalRefs, facts.revisionRefs,
		facts.parts, facts.ownerParts, facts.activePins, facts.details)
	return facts
}

func readAttachmentPublicationCrashBytes(
	ctx context.Context,
	blob attachments.BlobStore,
	version attachments.ObjectVersion,
) ([]byte, error) {
	reader, err := blob.Open(ctx, version, attachments.FullByteRange())
	if err != nil {
		return nil, err
	}
	content, readErr := io.ReadAll(reader)
	return content, errors.Join(readErr, reader.Close())
}

func readAttachmentPublicationCrashS3Version(
	ctx context.Context,
	client *minio.Client,
	bucket, key, versionID string,
) (minio.ObjectInfo, []byte, error) {
	object, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{VersionID: versionID})
	if err != nil {
		return minio.ObjectInfo{}, nil, err
	}
	info, statErr := object.Stat()
	content, readErr := io.ReadAll(object)
	return info, content, errors.Join(statErr, readErr, object.Close())
}

func attachmentPublicationCrashPrepareRequest(
	content []byte,
	uploadID string,
	backendKind attachments.BackendKind,
) attachments.BlobPublicationPrepareRequest {
	digest := sha256.Sum256(content)
	return attachments.BlobPublicationPrepareRequest{
		ProjectID:       "default",
		OwnerKind:       attachments.BlobPublicationOwnerUpload,
		OwnerID:         uploadID,
		OwnerGeneration: 1,
		Target: attachments.BlobPublicationTarget{
			Key: "sha256/" + fmt.Sprintf("%x", digest), SHA256: digest,
			SizeBytes: int64(len(content)), BackendKind: backendKind,
		},
		PublishExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	}
}

func prepareS3AttachmentPublicationCrashUpload(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	repository *PostgresAttachmentRepository,
	harness attachmentPublicationCrashHarness,
	uploadID string,
	content []byte,
) attachmentPublicationCrashUpload {
	t.Helper()
	// 本地传输同样要有真实上传行：写入意图登记会复核所有者是否仍存在。
	return prepareAttachmentPublicationCrashUpload(
		t, ctx, fixture, repository, harness, uploadID, content,
	)
}

func prepareAttachmentPublicationCrashUpload(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	repository *PostgresAttachmentRepository,
	harness attachmentPublicationCrashHarness,
	uploadID string,
	content []byte,
) attachmentPublicationCrashUpload {
	t.Helper()
	const uploadPrefix = "aup_"
	if len(uploadID) <= len(uploadPrefix) || uploadID[:len(uploadPrefix)] != uploadPrefix {
		t.Fatalf("publication crash upload ID = %q, want aup_ prefix", uploadID)
	}
	suffix := uploadID[len(uploadPrefix):]
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: uploadID, AttachmentID: "att_" + suffix,
		DraftID: "rdf_" + suffix, AuthorID: attachmentFixtureAuthorID(suffix),
		DisplayName: suffix + ".txt", MediaType: "text/plain",
		TransportKind: harness.transportKind, DeclaredSizeBytes: int64(len(content)),
		ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: attachments.DefaultLimits(),
	}
	seedAttachmentDraft(t, ctx, fixture, reserve.DraftID, reserve.AuthorID)
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	temporaryKey := attachmentPublicationCrashTemporaryKey(t, harness.backendKind, uploadID)
	switch harness.transportKind {
	case attachments.TransportKindLocal:
		if harness.backendKind != attachments.BackendKindLocal || temporaryKey != "" {
			t.Fatalf("local publication crash backend/temporary key = %q/%q", harness.backendKind, temporaryKey)
		}
		if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{
			ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		}); err != nil {
			t.Fatalf("StartUpload() error = %v", err)
		}
	case attachments.TransportKindS3:
		if harness.backendKind != attachments.BackendKindS3 {
			t.Fatalf("S3 publication crash backend = %q, want s3", harness.backendKind)
		}
		preparation, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
			ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
			CandidateTemporaryObjectKey: temporaryKey,
		})
		if err != nil {
			t.Fatalf("PrepareUpload() error = %v", err)
		}
		if preparation.TemporaryObjectKey != temporaryKey || preparation.TemporaryObjectVersion != "" {
			t.Fatalf("PrepareUpload() temporary identity = %q@%q, want %q@empty",
				preparation.TemporaryObjectKey, preparation.TemporaryObjectVersion, temporaryKey)
		}
		temporaryKey = preparation.TemporaryObjectKey
	default:
		t.Fatalf("unsupported publication crash transport %q", harness.transportKind)
	}
	assertAttachmentPublicationCrashPersistedTemporaryIdentity(
		t, ctx, fixture, harness, uploadID, temporaryKey,
	)
	return attachmentPublicationCrashUpload{reserve: reserve, temporaryKey: temporaryKey}
}

func attachmentPublicationCrashTemporaryKey(
	t *testing.T,
	backendKind attachments.BackendKind,
	ownerID string,
) string {
	t.Helper()
	switch backendKind {
	case attachments.BackendKindLocal:
		return ""
	case attachments.BackendKindS3:
		digest := sha256.Sum256([]byte(t.Name() + "\x00" + ownerID))
		return "temporary/" + fmt.Sprintf("%x", digest)
	default:
		t.Fatalf("unsupported publication crash backend %q", backendKind)
		return ""
	}
}

func assertAttachmentPublicationCrashPersistedTemporaryIdentity(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	harness attachmentPublicationCrashHarness,
	uploadID string,
	wantKey string,
) {
	t.Helper()
	if harness.backendKind == attachments.BackendKindLocal {
		if wantKey != "" {
			t.Fatalf("local publication crash temporary key = %q, want empty", wantKey)
		}
		return
	}
	var state attachments.UploadState
	var transport attachments.TransportKind
	var key, version *string
	if err := fixture.db.QueryRow(ctx, `
		select upload_state, transport_kind, temporary_object_key, temporary_object_version
		from public.attachment_uploads
		where project_id = 'default' and upload_id = $1`, uploadID).Scan(
		&state, &transport, &key, &version,
	); err != nil {
		t.Fatalf("read persisted S3 publication crash temporary identity for %q: %v", uploadID, err)
	}
	if state != attachments.UploadStateUploading || transport != harness.transportKind ||
		key == nil || *key != wantKey || version != nil {
		t.Fatalf("persisted S3 publication crash temporary identity for %q = %q/%q/%v@%v, want uploading/%q/%q@null",
			uploadID, state, transport, key, version, harness.transportKind, wantKey)
	}
}

func newAttachmentPublicationCrashReconciler(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	backendKind attachments.BackendKind,
	newBackend attachmentPublicationCrashBackendFactory,
	cleanupOwner string,
	cutpoint func(attachments.BlobPublicationReconcilerCutpoint) error,
) (*PostgresAttachmentRepository, attachmentPublicationCrashBackend, *attachments.BlobPublicationReconciler) {
	t.Helper()
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, cleanupOwner, 1),
	)
	blob := newBackend(t)
	reconciler, err := attachments.NewBlobPublicationReconciler(
		repository,
		blob,
		blob,
		attachments.BlobPublicationReconcilerConfig{
			ProjectID:          "default",
			BackendKind:        backendKind,
			CleanupOwnerID:     cleanupOwner,
			OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration,
			RetryDelay:         time.Microsecond,
			Cutpoint:           cutpoint,
		},
	)
	if err != nil {
		t.Fatalf("NewBlobPublicationReconciler() error = %v", err)
	}
	return repository, blob, reconciler
}

func convergeAttachmentPublicationCrash(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	reconciler *attachments.BlobPublicationReconciler,
	publicationID string,
) {
	t.Helper()
	expireAttachmentPublicationCrashIntent(t, ctx, fixture, publicationID)
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := reconciler.RunOnce(ctx); err != nil {
			t.Fatalf("BlobPublicationReconciler.RunOnce(%d) error = %v", attempt, err)
		}
		if row := readAttachmentPublicationCrashRow(t, ctx, fixture, publicationID); row.state == attachments.BlobPublicationStateCompleted {
			return
		}
	}
	row := readAttachmentPublicationCrashRow(t, ctx, fixture, publicationID)
	t.Fatalf("Blob publication %q did not converge; row = %#v", publicationID, row)
}

func readAttachmentPublicationCrashRow(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	publicationID string,
) attachmentPublicationCrashRow {
	t.Helper()
	var row attachmentPublicationCrashRow
	if err := fixture.db.QueryRow(ctx, `
		select publication_state, completion_outcome, object_version,
		       cleanup_generation, attempt, cleanup_owner_id, cleanup_lease_expires_at,
		       receipt_digest, completed_at, retry_at
		from public.blob_publication_intents
		where publication_id = $1`, publicationID).Scan(
		&row.state,
		&row.outcome,
		&row.objectVersion,
		&row.cleanupGeneration,
		&row.attempt,
		&row.cleanupOwner,
		&row.cleanupLease,
		&row.receiptDigest,
		&row.completedAt,
		&row.retryAt,
	); err != nil {
		t.Fatalf("read Blob publication crash row %q: %v", publicationID, err)
	}
	return row
}

func assertAttachmentPublicationCrashTerminal(
	t *testing.T,
	row attachmentPublicationCrashRow,
	wantOutcome attachments.BlobPublicationCompletionOutcome,
	wantVersion *string,
) {
	t.Helper()
	if row.state != attachments.BlobPublicationStateCompleted || row.outcome == nil || *row.outcome != wantOutcome {
		t.Fatalf("Blob publication terminal state/outcome = %q/%v, want completed/%q", row.state, row.outcome, wantOutcome)
	}
	if wantVersion == nil {
		if row.objectVersion != nil {
			t.Fatalf("Blob publication object_version = %q, want null", *row.objectVersion)
		}
		return
	}
	if row.objectVersion == nil || *row.objectVersion != *wantVersion {
		t.Fatalf("Blob publication object_version = %v, want %q", row.objectVersion, *wantVersion)
	}
}

func assertAttachmentPublicationCrashNoReferences(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	target attachments.BlobPublicationTarget,
	objectVersion string,
) {
	t.Helper()
	var partCount, blobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.attachment_upload_parts
		        where sha256_digest = $1 and ($2 = '' or object_version = $2)),
		       (select count(*) from public.blob_objects
		        where blob_key = $3 and sha256_digest = $1 and ($2 = '' or object_version = $2))`,
		target.SHA256[:], objectVersion, target.Key,
	).Scan(&partCount, &blobCount); err != nil {
		t.Fatalf("count Blob publication durable references: %v", err)
	}
	if partCount != 0 || blobCount != 0 {
		t.Fatalf("Blob publication durable references part/blob = %d/%d, want 0/0", partCount, blobCount)
	}
}

func assertAttachmentPublicationCrashReference(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	uploadID string,
	version attachments.ObjectVersion,
) {
	t.Helper()
	var partCount, blobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.attachment_upload_parts
		        where upload_id = $1 and part_number = 1 and size_bytes = $2
		          and sha256_digest = $3 and object_version = $4),
		       (select count(*) from public.blob_objects
		        where blob_key = $5 and size_bytes = $2
		          and sha256_digest = $3 and object_version = $4)`,
		uploadID, version.SizeBytes, version.SHA256[:], version.VersionID, version.Key,
	).Scan(&partCount, &blobCount); err != nil {
		t.Fatalf("read consumed Blob publication references: %v", err)
	}
	if partCount != 1 || blobCount != 0 {
		t.Fatalf("consumed Blob publication exact part/blob references = %d/%d, want 1/0", partCount, blobCount)
	}
}

func expireAttachmentPublicationCrashLease(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	claim attachments.BlobPublicationCleanupClaim,
) {
	t.Helper()
	updated, err := fixture.db.Exec(ctx, `
		update public.blob_publication_intents
		set cleanup_lease_expires_at = transaction_timestamp() - interval '1 second'
		where publication_id = $1 and publication_state = 'cleanup_claimed'
		  and cleanup_owner_id = $2 and cleanup_generation = $3 and attempt = $4
		  and cleanup_lease_expires_at = $5`,
		claim.Intent.PublicationID,
		claim.CleanupOwnerID,
		claim.CleanupGeneration,
		claim.Attempt,
		claim.ObservedLeaseExpiresAt,
	)
	if err != nil {
		t.Fatalf("expire Blob publication cleanup lease: %v", err)
	}
	if updated.RowsAffected() != 1 {
		t.Fatalf("expire Blob publication cleanup lease rows = %d, want 1", updated.RowsAffected())
	}
}

func assertAttachmentPublicationCrashObjectAbsent(
	t *testing.T,
	ctx context.Context,
	blob attachments.BlobStore,
	version attachments.ObjectVersion,
) {
	t.Helper()
	if _, err := blob.Stat(ctx, version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("BlobStore.Stat(absent exact version) error = %v, want ErrBlobNotFound", err)
	}
}

func expireAttachmentPublicationCrashIntent(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, publicationID string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `update public.blob_publication_intents set publish_expires_at=clock_timestamp()-interval '1 minute' where publication_id=$1 and publication_state<>'completed'`, publicationID); err != nil {
		t.Fatal(err)
	}
}

func newAttachmentPublicationCrashWriter(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, blob attachments.BlobStore, backend attachments.BackendKind) *attachments.ManagedBlobWriter {
	t.Helper()
	pool := fixture.openDirectRuntimePool(t, ctx, "publication-crash-managed-write", 1)
	writer, err := attachments.NewManagedBlobWriter(blob, backend, NewPostgresLocalBlobRepository(pool, localBlobFixtureWitness(t, pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

func putManagedAttachmentPublicationCrash(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, blob attachments.BlobStore, request attachments.BlobPublicationPrepareRequest, intent attachments.BlobPublicationIntent, upload attachmentPublicationCrashUpload, content []byte) attachments.WrittenBlob {
	t.Helper()
	writer := newAttachmentPublicationCrashWriter(t, ctx, fixture, blob, request.Target.BackendKind)
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: "upload", ID: upload.reserve.UploadID, Generation: 1, DraftID: &upload.reserve.DraftID, PublicationID: &intent.PublicationID}, Content: attachments.PutRequest{ExpectedSHA256: request.Target.SHA256, ExpectedSizeBytes: request.Target.SizeBytes, TemporaryKey: upload.temporaryKey}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	return written
}

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
)

type attachmentPublicationCrashRow struct {
	state             attachments.BlobPublicationState
	outcome           *attachments.BlobPublicationCompletionOutcome
	objectVersion     *string
	cleanupGeneration int64
	attempt           int64
	cleanupOwner      *string
	cleanupLease      *time.Time
}

type attachmentPublicationCrashBackend interface {
	attachments.BlobStore
	attachments.BlobPublicationResolver
}

type attachmentPublicationCrashBackendFactory func(t *testing.T) attachmentPublicationCrashBackend

type attachmentPublicationCrashHarness struct {
	backendKind       attachments.BackendKind
	transportKind     attachments.TransportKind
	newBackendFactory func(t *testing.T) attachmentPublicationCrashBackendFactory
}

type attachmentPublicationCrashUpload struct {
	reserve      attachments.ReserveUploadCommand
	temporaryKey string
}

func TestPostgresIntegrationAttachmentBlobPublicationPrepareReplayUsesCurrentActiveIntent(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "publication-prepare-replay", 2),
	)
	content := []byte("publication prepare replay current active intent\n")
	historicalRequest := attachmentPublicationCrashPrepareRequest(
		content,
		"aup_pubpreparehistory",
		attachments.BackendKindLocal,
	)
	historical, err := repository.PrepareBlobPublication(ctx, historicalRequest)
	if err != nil {
		t.Fatalf("PrepareBlobPublication(historical) error = %v", err)
	}
	claim, err := repository.ClaimBlobPublicationCleanup(ctx, attachments.BlobPublicationCleanupClaimRequest{
		ProjectID:          "default",
		BackendKind:        attachments.BackendKindLocal,
		CleanupOwnerID:     "publication_prepare_replay",
		OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration,
	})
	if err != nil {
		t.Fatalf("ClaimBlobPublicationCleanup(historical) error = %v", err)
	}
	if claim == nil || claim.Intent.PublicationID != historical.PublicationID {
		t.Fatalf("ClaimBlobPublicationCleanup(historical) = %#v, want publication %q", claim, historical.PublicationID)
	}
	if _, err := repository.CompleteBlobPublicationCleanup(ctx, attachments.BlobPublicationCleanupCompletionRequest{
		Claim:   *claim,
		Outcome: attachments.BlobPublicationCompletionOutcomeAlreadyAbsent,
	}); err != nil {
		t.Fatalf("CompleteBlobPublicationCleanup(historical) error = %v", err)
	}

	currentRequest := historicalRequest
	currentRequest.OwnerID = "aup_pubpreparecurrent"
	currentRequest.PublishExpiresAt = time.Now().UTC().Add(time.Hour)
	current, err := repository.PrepareBlobPublication(ctx, currentRequest)
	if err != nil {
		t.Fatalf("PrepareBlobPublication(current) error = %v", err)
	}
	replayed, err := repository.PrepareBlobPublication(ctx, currentRequest)
	if err != nil {
		t.Fatalf("PrepareBlobPublication(current replay) error = %v", err)
	}
	if replayed.PublicationID != current.PublicationID || replayed.State != attachments.BlobPublicationStatePrepared {
		t.Fatalf("PrepareBlobPublication(current replay) = %#v, want current prepared publication %q", replayed, current.PublicationID)
	}

	conflictingRequest := currentRequest
	conflictingRequest.OwnerID = "aup_pubprepareconflict"
	if _, err := repository.PrepareBlobPublication(ctx, conflictingRequest); !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("PrepareBlobPublication(different owner) error = %v, want ErrBlobPublicationConflict", err)
	}
}

func TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocal(t *testing.T) {
	runAttachmentPublicationCrashRestart(t, attachmentPublicationCrashHarness{
		backendKind:       attachments.BackendKindLocal,
		transportKind:     attachments.TransportKindLocal,
		newBackendFactory: newAttachmentPublicationCrashLocalBackendFactory,
	})
}

func TestPostgresMinIOIntegrationAttachmentBlobPublicationCrashRestartS3(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 and HOUFENG_MINIO_INTEGRATION=1 to run the real PostgreSQL + MinIO publication crash test")
	}
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	runAttachmentPublicationCrashRestart(t, attachmentPublicationCrashHarness{
		backendKind:   attachments.BackendKindS3,
		transportKind: attachments.TransportKindS3,
		newBackendFactory: func(t *testing.T) attachmentPublicationCrashBackendFactory {
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
	})
}

func runAttachmentPublicationCrashRestart(t *testing.T, harness attachmentPublicationCrashHarness) {
	t.Helper()
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)

	t.Run("before_publish", func(t *testing.T) {
		content := []byte("local publication crash before publish\n")
		request := attachmentPublicationCrashPrepareRequest(content, "aup_pubcrashbefore", harness.backendKind)
		newBackend := harness.newBackendFactory(t)
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
		newBackend := harness.newBackendFactory(t)
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
		version, err := initialBlob.Put(ctx, attachments.PutRequest{
			ExpectedSHA256: request.Target.SHA256, ExpectedSizeBytes: request.Target.SizeBytes,
			TemporaryKey: upload.temporaryKey,
		}, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("BlobStore.Put() error = %v", err)
		}

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
		newBackend := harness.newBackendFactory(t)
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
		version, err := initialBlob.Put(ctx, attachments.PutRequest{
			ExpectedSHA256: request.Target.SHA256, ExpectedSizeBytes: request.Target.SizeBytes,
			TemporaryKey: upload.temporaryKey,
		}, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("BlobStore.Put() error = %v", err)
		}
		published, err := initialRepository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
			Intent: intent, Object: version,
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
		newBackend := harness.newBackendFactory(t)
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
		version, err := initialBlob.Put(ctx, attachments.PutRequest{
			ExpectedSHA256: request.Target.SHA256, ExpectedSizeBytes: request.Target.SizeBytes,
			TemporaryKey: upload.temporaryKey,
		}, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("BlobStore.Put() error = %v", err)
		}
		published, err := initialRepository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
			Intent: intent, Object: version,
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
		newBackend := harness.newBackendFactory(t)
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
		version, err := initialBlob.Put(ctx, attachments.PutRequest{
			ExpectedSHA256: request.Target.SHA256, ExpectedSizeBytes: request.Target.SizeBytes,
			TemporaryKey: upload.temporaryKey,
		}, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("BlobStore.Put() error = %v", err)
		}
		published, err := initialRepository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
			Intent: intent, Object: version,
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
		PublishExpiresAt: time.Now().UTC().Add(-time.Minute),
	}
}

func newAttachmentPublicationCrashLocalBackendFactory(t *testing.T) attachmentPublicationCrashBackendFactory {
	t.Helper()
	root := filepath.Join(t.TempDir(), "blob-root")
	return func(t *testing.T) attachmentPublicationCrashBackend {
		t.Helper()
		blob, err := attachments.NewLocalBlobStore(root)
		if err != nil {
			t.Fatalf("NewLocalBlobStore() error = %v", err)
		}
		return blob
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
	if harness.backendKind == attachments.BackendKindLocal {
		assertAttachmentPublicationCrashPersistedTemporaryIdentity(
			t, ctx, fixture, harness, uploadID, "",
		)
		return attachmentPublicationCrashUpload{}
	}
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
		DraftID: "rdf_" + suffix, AuthorID: "usr_" + suffix,
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
		       cleanup_generation, attempt, cleanup_owner_id, cleanup_lease_expires_at
		from public.blob_publication_intents
		where publication_id = $1`, publicationID).Scan(
		&row.state,
		&row.outcome,
		&row.objectVersion,
		&row.cleanupGeneration,
		&row.attempt,
		&row.cleanupOwner,
		&row.cleanupLease,
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

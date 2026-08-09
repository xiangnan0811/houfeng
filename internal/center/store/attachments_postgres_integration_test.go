package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
)

func TestPostgresIntegrationAttachmentUploadPreparationPersistsAndReusesTemporaryIdentity(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_uploadprepare", "usr_uploadprepare")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-upload-prepare", 2),
	)
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_uploadprepare", AttachmentID: "att_uploadprepare",
		DraftID: "rdf_uploadprepare", AuthorID: "usr_uploadprepare", DisplayName: "prepare.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindS3, DeclaredSizeBytes: 17,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		Limits: attachments.Limits{
			MaxFileBytes: 17, MaxRecordBytes: 17, MaxProjectBytes: 17,
			WarningPercent: 80, MaxInlineTextPreviewBytes: 1,
		},
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	}); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("StartUpload(S3 without persisted key) error = %v, want ErrAttachmentConflict", err)
	}
	firstKey := "temporary/1111111111111111111111111111111111111111111111111111111111111111"
	secondKey := "temporary/2222222222222222222222222222222222222222222222222222222222222222"
	prepared, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		CandidateTemporaryObjectKey: firstKey,
	})
	if err != nil {
		t.Fatalf("PrepareUpload(first) error = %v", err)
	}
	if prepared.State != attachments.UploadStateUploading || prepared.TemporaryObjectKey != firstKey ||
		prepared.TemporaryObjectVersion != "" || prepared.DeclaredSizeBytes != reserve.DeclaredSizeBytes ||
		prepared.MediaType != reserve.MediaType {
		t.Fatalf("PrepareUpload(first) = %#v", prepared)
	}
	reservationReplay, err := repository.ReserveUpload(ctx, reserve)
	if err != nil {
		t.Fatalf("ReserveUpload(replay after preparation) error = %v", err)
	}
	if reservationReplay.UploadID != reserve.UploadID || reservationReplay.AttachmentID != reserve.AttachmentID ||
		reservationReplay.State != attachments.UploadStateUploading {
		t.Fatalf("ReserveUpload(replay after preparation) = %#v", reservationReplay)
	}
	preparedReplay, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		CandidateTemporaryObjectKey: secondKey,
	})
	if err != nil {
		t.Fatalf("PrepareUpload(replay) error = %v", err)
	}
	if preparedReplay != prepared {
		t.Fatalf("PrepareUpload(replay) = %#v, want %#v", preparedReplay, prepared)
	}

	versionCommand := attachments.RecordTemporaryObjectVersionCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		TemporaryObjectKey: firstKey, TemporaryObjectVersion: "s3-temporary-v1",
	}
	versioned, err := repository.RecordTemporaryObjectVersion(ctx, versionCommand)
	if err != nil {
		t.Fatalf("RecordTemporaryObjectVersion() error = %v", err)
	}
	if versioned.TemporaryObjectKey != firstKey || versioned.TemporaryObjectVersion != "s3-temporary-v1" {
		t.Fatalf("RecordTemporaryObjectVersion() = %#v", versioned)
	}
	survivorDigest := sha256.Sum256([]byte("must not replace surviving temporary object"))
	survivorObject := attachments.ObjectVersion{
		Key: fmt.Sprintf("sha256/%x", survivorDigest), VersionID: "s3-final-after-survivor",
		SHA256: survivorDigest, SizeBytes: int64(len("must not replace surviving temporary object")),
	}
	survivorIntent := preparePublishedAttachmentUploadIntent(
		t, ctx, repository, reserve, survivorObject,
	)
	if _, err := repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		TemporaryObjectKey: firstKey, Object: survivorObject, PublicationIntent: survivorIntent,
	}); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("RecordUploadedContent(after surviving temporary) error = %v, want ErrAttachmentConflict", err)
	}
	versionedReplay, err := repository.RecordTemporaryObjectVersion(ctx, versionCommand)
	if err != nil || versionedReplay != versioned {
		t.Fatalf("RecordTemporaryObjectVersion(replay) = (%#v, %v), want %#v", versionedReplay, err, versioned)
	}
	conflict := versionCommand
	conflict.TemporaryObjectVersion = "s3-temporary-v2"
	if _, err := repository.RecordTemporaryObjectVersion(ctx, conflict); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("RecordTemporaryObjectVersion(conflict) error = %v, want ErrAttachmentConflict", err)
	}
	conflict = versionCommand
	conflict.TemporaryObjectKey = secondKey
	if _, err := repository.RecordTemporaryObjectVersion(ctx, conflict); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("RecordTemporaryObjectVersion(wrong key) error = %v, want ErrAttachmentConflict", err)
	}

	var uploadState, attachmentState attachments.UploadState
	var temporaryKey, temporaryVersion string
	if err := fixture.db.QueryRow(ctx, `
		select upload.upload_state, attachment.attachment_state,
		       upload.temporary_object_key, upload.temporary_object_version
		from public.attachment_uploads upload
		join public.record_attachments attachment on attachment.attachment_id = upload.attachment_id
		where upload.upload_id = $1`, reserve.UploadID).Scan(
		&uploadState, &attachmentState, &temporaryKey, &temporaryVersion,
	); err != nil {
		t.Fatalf("read persisted upload preparation: %v", err)
	}
	if uploadState != attachments.UploadStateUploading || attachmentState != attachments.UploadStateUploading ||
		temporaryKey != firstKey || temporaryVersion != versionCommand.TemporaryObjectVersion {
		t.Fatalf("persisted preparation = %q/%q %q/%q", uploadState, attachmentState, temporaryKey, temporaryVersion)
	}
}

func TestPostgresIntegrationAttachmentFinalIdentityCompletionAndProcessorReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_uploadcomplete", "usr_uploadcomplete")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-upload-complete", 2),
	)
	content := []byte("final upload bytes")
	digest := sha256.Sum256(content)
	temporaryKey := "temporary/3333333333333333333333333333333333333333333333333333333333333333"
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_uploadcomplete", AttachmentID: "att_uploadcomplete",
		DraftID: "rdf_uploadcomplete", AuthorID: "usr_uploadcomplete", DisplayName: "complete.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindS3, DeclaredSizeBytes: int64(len(content)),
		ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: attachments.DefaultLimits(),
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	if _, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		CandidateTemporaryObjectKey: temporaryKey,
	}); err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	object := attachments.ObjectVersion{
		Key: "sha256/" + fmt.Sprintf("%x", digest), VersionID: "s3-final-v1",
		SHA256: digest, SizeBytes: int64(len(content)),
	}
	publicationIntent := preparePublishedAttachmentUploadIntent(t, ctx, repository, reserve, object)
	recordCommand := attachments.RecordUploadedContentCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		TemporaryObjectKey: temporaryKey, Object: object, PublicationIntent: publicationIntent,
	}
	recorded, err := repository.RecordUploadedContent(ctx, recordCommand)
	if err != nil {
		t.Fatalf("RecordUploadedContent() error = %v", err)
	}
	if recorded.Object != object || recorded.TemporaryObjectKey != temporaryKey ||
		recorded.State != attachments.UploadStateUploading {
		t.Fatalf("RecordUploadedContent() = %#v", recorded)
	}
	recordedReplay, err := repository.RecordUploadedContent(ctx, recordCommand)
	if err != nil || recordedReplay != recorded {
		t.Fatalf("RecordUploadedContent(replay) = (%#v, %v), want %#v", recordedReplay, err, recorded)
	}
	conflictingObject := object
	conflictingObject.VersionID = "s3-final-v2"
	conflictingRecordCommand := recordCommand
	conflictingRecordCommand.Object = conflictingObject
	if _, err := repository.RecordUploadedContent(ctx, conflictingRecordCommand); !errors.Is(err, attachments.ErrInvalidAttachmentCommand) {
		t.Fatalf("RecordUploadedContent(conflict) error = %v, want ErrInvalidAttachmentCommand", err)
	}

	fingerprint := sha256.Sum256([]byte("completion-fingerprint"))
	complete := attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		ActualSizeBytes: int64(len(content)), ActualSHA256: digest,
		TemporaryObjectKey: temporaryKey, Object: object, CompletionFingerprint: fingerprint,
		ProcessorJobID: "apj_uploadcomplete", ProcessorProfile: attachments.ProcessorProfileText,
		ProcessorMaxAttempts: 3, ProcessorExpiresAt: reserve.ExpiresAt,
	}
	completed, err := repository.CompleteUploadAndEnqueue(ctx, complete)
	if err != nil {
		t.Fatalf("CompleteUploadAndEnqueue() error = %v", err)
	}
	if completed.State != attachments.UploadStateQuarantined || completed.UploadID != reserve.UploadID ||
		completed.AttachmentID != reserve.AttachmentID {
		t.Fatalf("CompleteUploadAndEnqueue() = %#v", completed)
	}
	if _, err := repository.RecordTemporaryObjectVersion(ctx, attachments.RecordTemporaryObjectVersionCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		TemporaryObjectKey: temporaryKey, TemporaryObjectVersion: "late-temporary-version",
	}); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("RecordTemporaryObjectVersion(after quarantine) error = %v, want ErrAttachmentConflict", err)
	}
	loaded, err := repository.GetUploadedContent(ctx, attachments.UploadMutationCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	})
	if err != nil {
		t.Fatalf("GetUploadedContent(quarantined) error = %v", err)
	}
	if loaded.State != attachments.UploadStateQuarantined || loaded.Object != object {
		t.Fatalf("GetUploadedContent(quarantined) = %#v", loaded)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_uploads
		set created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'
		where upload_id = $1`, reserve.UploadID); err != nil {
		t.Fatalf("expire completed attachment upload fixture: %v", err)
	}
	completedReplay, err := repository.CompleteUploadAndEnqueue(ctx, complete)
	if err != nil || completedReplay != completed {
		t.Fatalf("CompleteUploadAndEnqueue(replay) = (%#v, %v), want %#v", completedReplay, err, completed)
	}
	conflictingComplete := complete
	conflictingComplete.CompletionFingerprint = sha256.Sum256([]byte("other-completion"))
	if _, err := repository.CompleteUploadAndEnqueue(ctx, conflictingComplete); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteUploadAndEnqueue(conflict) error = %v, want ErrAttachmentConflict", err)
	}

	var uploadState, attachmentState attachments.UploadState
	var actualSize, partSize int64
	var actualDigest, partDigest []byte
	var partVersion string
	var temporaryVersion *string
	var jobCount int
	var jobProfile attachments.ProcessorProfile
	if err := fixture.db.QueryRow(ctx, `
		select upload.upload_state, attachment.attachment_state,
		       upload.actual_size_bytes, upload.actual_sha256_digest,
		       upload.temporary_object_version,
		       part.size_bytes, part.sha256_digest, part.object_version,
		       (select count(*)::int from public.attachment_processor_jobs job where job.upload_id = upload.upload_id),
		       (select processor_profile from public.attachment_processor_jobs job where job.upload_id = upload.upload_id)
		from public.attachment_uploads upload
		join public.record_attachments attachment on attachment.attachment_id = upload.attachment_id
		join public.attachment_upload_parts part on part.upload_id = upload.upload_id and part.part_number = 1
		where upload.upload_id = $1`, reserve.UploadID).Scan(
		&uploadState, &attachmentState, &actualSize, &actualDigest, &temporaryVersion,
		&partSize, &partDigest, &partVersion, &jobCount, &jobProfile,
	); err != nil {
		t.Fatalf("read completed attachment upload: %v", err)
	}
	if uploadState != attachments.UploadStateQuarantined || attachmentState != attachments.UploadStateQuarantined ||
		actualSize != int64(len(content)) ||
		!equalAttachmentIntegrationBytes(actualDigest, digest[:]) || partSize != int64(len(content)) ||
		!equalAttachmentIntegrationBytes(partDigest, digest[:]) || partVersion != object.VersionID ||
		temporaryVersion != nil || jobCount != 1 || jobProfile != attachments.ProcessorProfileText {
		t.Fatalf("completed persistence = states %q/%q size %d/%d version %q temp %#v job %d/%q",
			uploadState, attachmentState, actualSize, partSize, partVersion, temporaryVersion, jobCount, jobProfile)
	}
}

func TestPostgresIntegrationAttachmentDirectCompletionAtomicallyPersistsPartAndJob(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_directcomplete", "usr_directcomplete")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-direct-complete", 2),
	)
	content := []byte("direct upload final bytes")
	digest := sha256.Sum256(content)
	temporaryKey := "temporary/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_directcomplete", AttachmentID: "att_directcomplete",
		DraftID: "rdf_directcomplete", AuthorID: "usr_directcomplete", DisplayName: "direct.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindS3,
		DeclaredSizeBytes: int64(len(content)), ExpiresAt: time.Now().UTC().Add(time.Hour),
		Limits: attachments.DefaultLimits(),
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	if _, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		CandidateTemporaryObjectKey: temporaryKey,
	}); err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	object := attachments.ObjectVersion{
		Key: fmt.Sprintf("sha256/%x", digest), VersionID: "direct-final-v1",
		SHA256: digest, SizeBytes: int64(len(content)),
	}
	complete := attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		ActualSizeBytes: int64(len(content)), ActualSHA256: digest,
		TemporaryObjectKey: temporaryKey, Object: object,
		CompletionFingerprint: sha256.Sum256([]byte("direct-completion-fingerprint")),
		ProcessorJobID:        "apj_directcomplete", ProcessorProfile: attachments.ProcessorProfileText,
		ProcessorMaxAttempts: 3, ProcessorExpiresAt: reserve.ExpiresAt,
	}
	if _, err := repository.CompleteUploadAndEnqueue(ctx, complete); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteUploadAndEnqueue(without exact temporary version) error = %v, want ErrAttachmentConflict", err)
	}
	if _, err := repository.RecordTemporaryObjectVersion(ctx, attachments.RecordTemporaryObjectVersionCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		TemporaryObjectKey: temporaryKey, TemporaryObjectVersion: "direct-temporary-v1",
	}); err != nil {
		t.Fatalf("RecordTemporaryObjectVersion() error = %v", err)
	}
	preparation, err := repository.GetUploadCompletionPreparation(ctx, attachments.UploadMutationCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	})
	if err != nil {
		t.Fatalf("GetUploadCompletionPreparation() error = %v", err)
	}
	if preparation.State != attachments.UploadStateUploading || preparation.HasObject ||
		preparation.Object != (attachments.ObjectVersion{}) {
		t.Fatalf("GetUploadCompletionPreparation() = %#v", preparation)
	}
	complete.TemporaryObjectVersion = "direct-temporary-v1"
	complete.PublicationIntent = preparePublishedAttachmentUploadIntent(t, ctx, repository, reserve, object)
	completed, err := repository.CompleteUploadAndEnqueue(ctx, complete)
	if err != nil {
		t.Fatalf("CompleteUploadAndEnqueue() error = %v", err)
	}
	if completed.State != attachments.UploadStateQuarantined {
		t.Fatalf("CompleteUploadAndEnqueue() = %#v", completed)
	}
	loaded, err := repository.GetUploadCompletionPreparation(ctx, attachments.UploadMutationCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	})
	if err != nil || !loaded.HasObject || loaded.Object != object || loaded.State != attachments.UploadStateQuarantined {
		t.Fatalf("GetUploadCompletionPreparation(after complete) = (%#v, %v)", loaded, err)
	}
	var partCount, jobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.attachment_upload_parts where upload_id = $1),
		       (select count(*) from public.attachment_processor_jobs where upload_id = $1)`,
		reserve.UploadID,
	).Scan(&partCount, &jobCount); err != nil {
		t.Fatalf("count direct completion rows: %v", err)
	}
	if partCount != 1 || jobCount != 1 {
		t.Fatalf("direct completion part/job count = %d/%d", partCount, jobCount)
	}
}

func TestPostgresIntegrationAttachmentUploadServiceLocalWorkflow(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	actor := attachmentUploadWorkflowActor(t)
	const draftID = "rdf_localworkflow"
	seedAttachmentDraft(t, ctx, fixture, draftID, actor.UserID)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-local-workflow", 2),
	)
	blob, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	currentTime := time.Now().UTC()
	service := newAttachmentUploadWorkflowService(
		t, repository, blob, attachments.TransportKindLocal, actor, draftID,
		func() time.Time { return currentTime },
	)
	content := []byte("cross-layer local upload bytes")
	expiresAt := currentTime.Add(time.Hour)
	created, err := service.CreateUpload(ctx, attachments.CreateUploadRequest{
		Actor: actor, UploadID: "aup_localworkflow", AttachmentID: "att_localworkflow",
		DraftID: draftID, DisplayName: "local.txt", MediaType: "text/plain",
		DeclaredSizeBytes: int64(len(content)), ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if created.State != attachments.UploadStateCreated || created.Target.TransportKind != attachments.TransportKindLocal ||
		created.Target.TemporaryObjectKey != "" {
		t.Fatalf("CreateUpload() = %#v", created)
	}
	digest := sha256.Sum256(content)
	put, err := service.PutContent(ctx, attachments.PutUploadContentRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
		ExpectedSHA256: digest, Content: bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("PutContent() error = %v", err)
	}
	completed, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
	})
	if err != nil || completed.State != attachments.UploadStateQuarantined {
		t.Fatalf("CompleteUpload() = (%#v, %v)", completed, err)
	}
	assertAttachmentUploadWorkflowRows(t, ctx, fixture, created.UploadID, put.Object, 1)
	currentTime = expiresAt.Add(time.Second)
	replay, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
	})
	if err != nil || replay != completed {
		t.Fatalf("CompleteUpload(replay) = (%#v, %v), want %#v", replay, err, completed)
	}
	assertAttachmentUploadWorkflowReplayConflict(
		t, ctx, repository, created.UploadID, actor.UserID, put.Object, "", "", expiresAt,
	)
}

func TestPostgresIntegrationAttachmentUploadServiceS3DirectWorkflow(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	actor := attachmentUploadWorkflowActor(t)
	const draftID = "rdf_s3directworkflow"
	seedAttachmentDraft(t, ctx, fixture, draftID, actor.UserID)
	postgresRepository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-s3-direct-workflow", 2),
	)
	repository := &attachmentUploadWorkflowRepository{PostgresAttachmentRepository: postgresRepository}
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	s3Blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	blob := &attachmentUploadWorkflowBlob{S3BlobStore: s3Blob}
	currentTime := time.Now().UTC()
	service := newAttachmentUploadWorkflowService(
		t, repository, blob, attachments.TransportKindS3, actor, draftID,
		func() time.Time { return currentTime },
	)
	content := []byte("cross-layer direct S3 upload bytes")
	expiresAt := currentTime.Add(time.Hour)
	created, err := service.CreateUpload(ctx, attachments.CreateUploadRequest{
		Actor: actor, UploadID: "aup_s3directworkflow", AttachmentID: "att_s3directworkflow",
		DraftID: draftID, DisplayName: "direct.txt", MediaType: "text/plain",
		DeclaredSizeBytes: int64(len(content)), ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if created.State != attachments.UploadStateUploading || created.Target.TransportKind != attachments.TransportKindS3 ||
		created.Target.TemporaryObjectKey == "" {
		t.Fatalf("CreateUpload() = %#v", created)
	}
	var persistedKey string
	var persistedVersion *string
	if err := fixture.db.QueryRow(ctx, `
		select temporary_object_key, temporary_object_version
		from public.attachment_uploads where upload_id = $1`, created.UploadID).Scan(
		&persistedKey, &persistedVersion,
	); err != nil {
		t.Fatalf("read persisted direct S3 target: %v", err)
	}
	if persistedKey != created.Target.TemporaryObjectKey || persistedVersion != nil {
		t.Fatalf("persisted direct S3 target = %q/%v", persistedKey, persistedVersion)
	}
	if _, err := blob.ResolveTemporaryVersion(ctx, persistedKey); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("ResolveTemporaryVersion(before client upload) error = %v, want ErrBlobNotFound", err)
	}
	upload, err := client.PutObject(
		ctx, bucket, persistedKey, bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("PutObject(direct temporary) error = %v", err)
	}
	const collisionUploadID = "aup_s3collision"
	const collisionJobID = "apj_s3directworkflow"
	if _, err := postgresRepository.ReserveUpload(ctx, attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: collisionUploadID, AttachmentID: "att_s3collision",
		DraftID: draftID, AuthorID: actor.UserID, DisplayName: "collision.txt", MediaType: "text/plain",
		TransportKind: attachments.TransportKindLocal, DeclaredSizeBytes: 1,
		ExpiresAt: expiresAt, Limits: attachments.DefaultLimits(),
	}); err != nil {
		t.Fatalf("ReserveUpload(processor job collision fixture) error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_processor_jobs (
			processor_job_id, upload_id, attachment_id, processor_profile, max_attempts, expires_at
		) values ($1, $2, $3, 'text', 3, $4)`,
		collisionJobID, collisionUploadID, "att_s3collision", expiresAt,
	); err != nil {
		t.Fatalf("seed processor job collision fixture: %v", err)
	}
	if _, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
	}); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteUpload(processor job collision) error = %v, want ErrAttachmentConflict", err)
	}
	if repository.realCompleteCalls != 1 {
		t.Fatalf("CompleteUpload(processor job collision) real repository calls = %d, want 1", repository.realCompleteCalls)
	}
	resolved, err := blob.ResolveTemporaryVersion(ctx, persistedKey)
	if err != nil || resolved.VersionID != upload.VersionID {
		t.Fatalf("ResolveTemporaryVersion(after transaction rollback) = (%#v, %v)", resolved, err)
	}
	assertAttachmentUploadWorkflowRowCounts(t, ctx, fixture, created.UploadID, 0, 0)
	var rolledBackState attachments.UploadState
	var rolledBackSize *int64
	var rolledBackFingerprint []byte
	if err := fixture.db.QueryRow(ctx, `
		select upload_state, actual_size_bytes, completion_fingerprint
		from public.attachment_uploads where upload_id = $1`, created.UploadID,
	).Scan(&rolledBackState, &rolledBackSize, &rolledBackFingerprint); err != nil {
		t.Fatalf("read target upload after transaction rollback: %v", err)
	}
	if rolledBackState != attachments.UploadStateUploading || rolledBackSize != nil || rolledBackFingerprint != nil {
		t.Fatalf("target upload after transaction rollback = state %q size %v fingerprint %x",
			rolledBackState, rolledBackSize, rolledBackFingerprint)
	}
	deleted, err := fixture.db.Exec(ctx, `
		delete from public.attachment_processor_jobs
		where processor_job_id = $1 and upload_id = $2`, collisionJobID, collisionUploadID)
	if err != nil {
		t.Fatalf("remove processor job collision fixture: %v", err)
	}
	if deleted.RowsAffected() != 1 {
		t.Fatalf("remove processor job collision fixture rows = %d, want 1", deleted.RowsAffected())
	}
	repository.completeErr = errors.New("injected committed result ambiguity")
	if _, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
	}); !errors.Is(err, repository.completeErr) {
		t.Fatalf("CompleteUpload(committed result ambiguity) error = %v", err)
	}
	if repository.realCompleteCalls != 2 {
		t.Fatalf("CompleteUpload(committed result ambiguity) real repository calls = %d, want 2",
			repository.realCompleteCalls)
	}
	digest := sha256.Sum256(content)
	var partDigest []byte
	var partSize int64
	var finalVersion string
	if err := fixture.db.QueryRow(ctx, `
		select size_bytes, sha256_digest, object_version
		from public.attachment_upload_parts where upload_id = $1 and part_number = 1`,
		created.UploadID,
	).Scan(&partSize, &partDigest, &finalVersion); err != nil {
		t.Fatalf("read direct S3 part: %v", err)
	}
	var storedDigest [sha256.Size]byte
	copy(storedDigest[:], partDigest)
	object := attachments.ObjectVersion{
		Key: fmt.Sprintf("sha256/%x", storedDigest), VersionID: finalVersion,
		SHA256: storedDigest, SizeBytes: partSize,
	}
	if storedDigest != digest || partSize != int64(len(content)) {
		t.Fatalf("direct S3 part = %#v", object)
	}
	assertAttachmentUploadWorkflowRows(t, ctx, fixture, created.UploadID, object, 1)
	var durableState attachments.UploadState
	if err := fixture.db.QueryRow(ctx, `
		select upload_state from public.attachment_uploads where upload_id = $1`, created.UploadID,
	).Scan(&durableState); err != nil {
		t.Fatalf("read target upload after committed result ambiguity: %v", err)
	}
	if durableState != attachments.UploadStateQuarantined {
		t.Fatalf("target upload after committed result ambiguity state = %q, want quarantined", durableState)
	}
	resolved, err = blob.ResolveTemporaryVersion(ctx, persistedKey)
	if err != nil || resolved.VersionID != upload.VersionID {
		t.Fatalf("ResolveTemporaryVersion(after committed result ambiguity) = (%#v, %v)", resolved, err)
	}
	resolveCallsBeforeReplay := blob.resolveTemporaryCalls
	openCallsBeforeReplay := blob.openTemporaryCalls
	publishCallsBeforeReplay := blob.publishTemporaryCalls
	deleteCallsBeforeReplay := blob.deleteTemporaryCalls
	repository.completeErr = nil
	completed, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
	})
	wantCompleted := attachments.UploadMutationResult{
		UploadID: created.UploadID, AttachmentID: created.AttachmentID, State: attachments.UploadStateQuarantined,
	}
	if err != nil || completed != wantCompleted {
		t.Fatalf("CompleteUpload(committed replay) = (%#v, %v), want %#v", completed, err, wantCompleted)
	}
	if blob.resolveTemporaryCalls != resolveCallsBeforeReplay || blob.openTemporaryCalls != openCallsBeforeReplay ||
		blob.publishTemporaryCalls != publishCallsBeforeReplay {
		t.Fatalf("CompleteUpload(committed replay) repeated direct verification/publication: resolve/open/publish %d/%d/%d -> %d/%d/%d",
			resolveCallsBeforeReplay, openCallsBeforeReplay, publishCallsBeforeReplay,
			blob.resolveTemporaryCalls, blob.openTemporaryCalls, blob.publishTemporaryCalls)
	}
	if blob.deleteTemporaryCalls != deleteCallsBeforeReplay+1 {
		t.Fatalf("CompleteUpload(committed replay) temporary delete calls = %d, want %d",
			blob.deleteTemporaryCalls, deleteCallsBeforeReplay+1)
	}
	if _, err := client.StatObject(ctx, bucket, persistedKey, minio.StatObjectOptions{
		VersionID: upload.VersionID,
	}); err == nil {
		t.Fatal("exact direct temporary version still exists after committed replay cleanup")
	}
	currentTime = expiresAt.Add(time.Second)
	replay, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{
		Actor: actor, DraftID: draftID, UploadID: created.UploadID,
	})
	if err != nil || replay != completed {
		t.Fatalf("CompleteUpload(expired replay) = (%#v, %v), want %#v", replay, err, completed)
	}
	assertAttachmentUploadWorkflowReplayConflict(
		t, ctx, postgresRepository, created.UploadID, actor.UserID, object,
		persistedKey, upload.VersionID, expiresAt,
	)
}

func TestPostgresIntegrationAttachmentExpiredPreparationIsReconciledAndReleasesReservation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_uploadexpired", "usr_uploadexpired")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-upload-expired", 2),
	)
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_uploadexpired", AttachmentID: "att_uploadexpired",
		DraftID: "rdf_uploadexpired", AuthorID: "usr_uploadexpired", DisplayName: "expired.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindS3, DeclaredSizeBytes: 9,
		ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: attachments.DefaultLimits(),
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_uploads
		set created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'
		where upload_id = $1`, reserve.UploadID); err != nil {
		t.Fatalf("expire attachment upload fixture: %v", err)
	}
	if _, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		CandidateTemporaryObjectKey: "temporary/4444444444444444444444444444444444444444444444444444444444444444",
	}); !errors.Is(err, attachments.ErrUploadExpired) {
		t.Fatalf("PrepareUpload(expired) error = %v, want ErrUploadExpired", err)
	}
	var state attachments.UploadState
	var temporaryKey *string
	if err := fixture.db.QueryRow(ctx, `
		select upload_state, temporary_object_key from public.attachment_uploads where upload_id = $1`,
		reserve.UploadID,
	).Scan(&state, &temporaryKey); err != nil {
		t.Fatalf("read expired attachment upload: %v", err)
	}
	if state != attachments.UploadStateCreated || temporaryKey != nil {
		t.Fatalf("expired upload mutated = state %q key %#v", state, temporaryKey)
	}
	expired, err := repository.ExpireAbandonedUpload(ctx, attachments.AbandonedUploadExpiryInput{
		ProjectID: "default", Limits: attachments.DefaultLimits(),
	})
	if err != nil || expired == nil || expired.UploadID != reserve.UploadID ||
		expired.AttachmentID != reserve.AttachmentID || expired.State != attachments.UploadStateExpired ||
		expired.Quota.Usage.ReservedBytes != 0 {
		t.Fatalf("ExpireAbandonedUpload() = (%#v, %v)", expired, err)
	}
	var attachmentState attachments.UploadState
	var reservedBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select upload.upload_state, attachment.attachment_state, quota.reserved_bytes
		from public.attachment_uploads as upload
		join public.record_attachments as attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = upload.attachment_id
		join public.attachment_quota_accounts as quota
		  on quota.project_id = upload.project_id
		where upload.upload_id = $1`, reserve.UploadID,
	).Scan(&state, &attachmentState, &reservedBytes); err != nil {
		t.Fatalf("read reconciled expired attachment upload: %v", err)
	}
	if state != attachments.UploadStateExpired || attachmentState != attachments.UploadStateExpired || reservedBytes != 0 {
		t.Fatalf("reconciled expired upload states/quota = %q/%q/%d", state, attachmentState, reservedBytes)
	}
	replay, err := repository.ExpireAbandonedUpload(ctx, attachments.AbandonedUploadExpiryInput{
		ProjectID: "default", Limits: attachments.DefaultLimits(),
	})
	if err != nil || replay != nil {
		t.Fatalf("ExpireAbandonedUpload(replay) = (%#v, %v), want nil, nil", replay, err)
	}
}

func TestPostgresIntegrationAttachmentExpiredUploadingSessionReleasesReservation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_uploadingexpired", "usr_uploadingexpired")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-uploading-expired", 2),
	)
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_uploadingexpired", AttachmentID: "att_uploadingexpired",
		DraftID: "rdf_uploadingexpired", AuthorID: "usr_uploadingexpired", DisplayName: "expired-local.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindLocal, DeclaredSizeBytes: 13,
		ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: attachments.DefaultLimits(),
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	if _, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	}); err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_uploads
		set created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'
		where upload_id = $1`, reserve.UploadID); err != nil {
		t.Fatalf("expire uploading attachment fixture: %v", err)
	}

	expired, err := repository.ExpireAbandonedUpload(ctx, attachments.AbandonedUploadExpiryInput{
		ProjectID: "default", Limits: attachments.DefaultLimits(),
	})
	if err != nil || expired == nil || expired.UploadID != reserve.UploadID ||
		expired.State != attachments.UploadStateExpired || expired.Quota.Usage.ReservedBytes != 0 {
		t.Fatalf("ExpireAbandonedUpload(uploading) = (%#v, %v)", expired, err)
	}
	var uploadState, attachmentState attachments.UploadState
	var reservedBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select upload.upload_state, attachment.attachment_state, quota.reserved_bytes
		from public.attachment_uploads as upload
		join public.record_attachments as attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = upload.attachment_id
		join public.attachment_quota_accounts as quota
		  on quota.project_id = upload.project_id
		where upload.upload_id = $1`, reserve.UploadID,
	).Scan(&uploadState, &attachmentState, &reservedBytes); err != nil {
		t.Fatalf("read expired uploading attachment: %v", err)
	}
	if uploadState != attachments.UploadStateExpired || attachmentState != attachments.UploadStateExpired ||
		reservedBytes != 0 {
		t.Fatalf("expired uploading states/quota = %q/%q/%d", uploadState, attachmentState, reservedBytes)
	}
}

func TestPostgresIntegrationAttachmentExpiredS3TemporaryVersionCanBeRecordedForCleanup(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_s3expiredcleanup", "usr_s3expiredcleanup")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-s3-expired-cleanup", 2),
	)
	key := "temporary/5555555555555555555555555555555555555555555555555555555555555555"
	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_s3expiredcleanup", AttachmentID: "att_s3expiredcleanup",
		DraftID: "rdf_s3expiredcleanup", AuthorID: "usr_s3expiredcleanup", DisplayName: "expired-s3.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindS3, DeclaredSizeBytes: 17,
		ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: attachments.DefaultLimits(),
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	preparation, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		CandidateTemporaryObjectKey: key,
	})
	if err != nil || preparation.State != attachments.UploadStateUploading || preparation.TemporaryObjectKey != key ||
		preparation.TemporaryObjectVersion != "" {
		t.Fatalf("PrepareUpload(S3) = (%#v, %v)", preparation, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_uploads
		set created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'
		where upload_id = $1`, reserve.UploadID); err != nil {
		t.Fatalf("expire S3 cleanup fixture: %v", err)
	}
	if expired, err := repository.ExpireAbandonedUpload(ctx, attachments.AbandonedUploadExpiryInput{
		ProjectID: "default", Limits: attachments.DefaultLimits(),
	}); err != nil || expired == nil || expired.State != attachments.UploadStateExpired {
		t.Fatalf("ExpireAbandonedUpload(S3) = (%#v, %v)", expired, err)
	}

	versioned, err := repository.RecordTemporaryObjectVersion(ctx, attachments.RecordTemporaryObjectVersionCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		TemporaryObjectKey: key, TemporaryObjectVersion: "observed-s3-expired-v1",
	})
	if err != nil || versioned.State != attachments.UploadStateExpired ||
		versioned.TemporaryObjectKey != key || versioned.TemporaryObjectVersion != "observed-s3-expired-v1" {
		t.Fatalf("RecordTemporaryObjectVersion(expired) = (%#v, %v)", versioned, err)
	}
	claimed, err := repository.ClaimTemporaryObjectCleanup(ctx, attachments.TemporaryObjectCleanupClaimInput{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil || claimed == nil || claimed.State != attachments.UploadStateExpired ||
		claimed.TemporaryObjectKey != key || claimed.TemporaryObjectVersion != "observed-s3-expired-v1" {
		t.Fatalf("ClaimTemporaryObjectCleanup(expired S3) = (%#v, %v)", claimed, err)
	}
}

func equalAttachmentIntegrationBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestPostgresIntegrationAttachmentConcurrentReservationsRespectEffectiveRecordQuota(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_attquota", "usr_attquota")
	limits := attachments.Limits{
		MaxFileBytes:              1024,
		MaxRecordBytes:            1536,
		MaxProjectBytes:           4096,
		WarningPercent:            80,
		MaxInlineTextPreviewBytes: 512,
	}

	type outcome struct {
		result attachments.UploadReservationResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		repository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("attachment-reserve-%d", index), 1),
		)
		command := attachments.ReserveUploadCommand{
			ProjectID:         "default",
			UploadID:          fmt.Sprintf("aup_quota%d", index),
			AttachmentID:      fmt.Sprintf("att_quota%d", index),
			DraftID:           "rdf_attquota",
			AuthorID:          "usr_attquota",
			DisplayName:       fmt.Sprintf("quota-%d.txt", index),
			MediaType:         "text/plain",
			TransportKind:     attachments.TransportKindLocal,
			DeclaredSizeBytes: 1024,
			ExpiresAt:         time.Now().UTC().Add(time.Hour),
			Limits:            limits,
		}
		go func() {
			<-start
			result, err := repository.ReserveUpload(context.Background(), command)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)

	var winnerCount, quotaCount int
	for range 2 {
		result := waitForRecordPlatformResult(t, outcomes, "concurrent attachment reservation")
		switch {
		case result.err == nil:
			winnerCount++
		case errors.Is(result.err, attachments.ErrQuotaExceeded):
			quotaCount++
		default:
			t.Fatalf("concurrent ReserveUpload() = (%#v, %v)", result.result, result.err)
		}
	}
	if winnerCount != 1 || quotaCount != 1 {
		t.Fatalf("concurrent reservation outcomes winners/quota = %d/%d, want 1/1", winnerCount, quotaCount)
	}

	var attachmentsCount, uploadsCount int
	var logicalBytes, reservedBytes, physicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.record_attachments where draft_id = 'rdf_attquota'),
		  (select count(*)::int from public.attachment_uploads where origin_draft_id = 'rdf_attquota'),
		  logical_bytes, reserved_bytes, physical_bytes
		from public.attachment_quota_accounts
		where project_id = 'default'
	`).Scan(&attachmentsCount, &uploadsCount, &logicalBytes, &reservedBytes, &physicalBytes); err != nil {
		t.Fatalf("read concurrent attachment reservation result: %v", err)
	}
	if attachmentsCount != 1 || uploadsCount != 1 || logicalBytes != 0 || reservedBytes != 1024 || physicalBytes != 0 {
		t.Fatalf("concurrent reservation rows = attachments/uploads %d/%d quota %d/%d/%d", attachmentsCount, uploadsCount, logicalBytes, reservedBytes, physicalBytes)
	}
}

func TestPostgresIntegrationAttachmentQuotaLifecycleAndPhysicalDedupe(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_attlifecycle", "usr_attlifecycle")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-quota-lifecycle", 2),
	)

	var digest [32]byte
	for index := range digest {
		digest[index] = 0xbb
	}
	var completionFingerprint [32]byte
	for index := range completionFingerprint {
		completionFingerprint[index] = 0xcc
	}
	blob := attachments.BlobObject{
		Key:           "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SHA256:        digest,
		ObjectVersion: "local-v1",
		SizeBytes:     9,
		BackendKind:   attachments.BackendKindLocal,
	}
	limits := attachments.Limits{
		MaxFileBytes:              10,
		MaxRecordBytes:            30,
		MaxProjectBytes:           30,
		WarningPercent:            60,
		MaxInlineTextPreviewBytes: 5,
	}

	for index := 0; index < 2; index++ {
		command := attachmentLifecycleReservationCommand(index)
		command.Limits = limits
		if _, err := repository.ReserveUpload(ctx, command); err != nil {
			t.Fatalf("ReserveUpload(%d) error = %v", index, err)
		}
		completeAttachmentIntegrationUpload(t, ctx, repository, command, attachments.ObjectVersion{
			Key: blob.Key, VersionID: blob.ObjectVersion, SHA256: blob.SHA256, SizeBytes: blob.SizeBytes,
		}, completionFingerprint, fmt.Sprintf("apj_lifecycle%d", index))
		admitCommand := attachments.AdmitUploadCommand{
			ProjectID: "default", UploadID: command.UploadID, AuthorID: command.AuthorID, Blob: blob, Limits: limits,
		}
		admitted, err := repository.AdmitUpload(ctx, admitCommand)
		if err != nil {
			t.Fatalf("AdmitUpload(%d) error = %v", index, err)
		}
		admittedReplay, err := repository.AdmitUpload(ctx, admitCommand)
		if err != nil || admittedReplay != admitted {
			t.Fatalf("AdmitUpload(%d) replay = (%#v, %v), want %#v", index, admittedReplay, err, admitted)
		}
		if index == 1 && !admitted.Quota.ProjectWarning {
			t.Fatalf("AdmitUpload(%d) custom 60%% warning = %#v", index, admitted.Quota)
		}
	}

	rejected := attachmentLifecycleReservationCommand(2)
	rejected.Limits = limits
	if _, err := repository.ReserveUpload(ctx, rejected); err != nil {
		t.Fatalf("ReserveUpload(rejected) error = %v", err)
	}
	if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{
		ProjectID: "default", UploadID: rejected.UploadID, AuthorID: rejected.AuthorID,
	}); err != nil {
		t.Fatalf("StartUpload(rejected) error = %v", err)
	}
	rejectCommand := attachments.FailUploadCommand{
		ProjectID: "default", UploadID: rejected.UploadID, AuthorID: rejected.AuthorID,
		TargetState: attachments.UploadStateRejected, Limits: limits,
	}
	rejectedResult, err := repository.FailUpload(ctx, rejectCommand)
	if err != nil {
		t.Fatalf("FailUpload(rejected) error = %v", err)
	}
	rejectedReplay, err := repository.FailUpload(ctx, rejectCommand)
	if err != nil || rejectedReplay != rejectedResult {
		t.Fatalf("FailUpload(rejected) replay = (%#v, %v), want %#v", rejectedReplay, err, rejectedResult)
	}

	expired := attachmentLifecycleReservationCommand(3)
	expired.Limits = limits
	if _, err := repository.ReserveUpload(ctx, expired); err != nil {
		t.Fatalf("ReserveUpload(expired) error = %v", err)
	}
	expireCommand := attachments.FailUploadCommand{
		ProjectID: "default", UploadID: expired.UploadID, AuthorID: expired.AuthorID,
		TargetState: attachments.UploadStateExpired, Limits: limits,
	}
	expiredResult, err := repository.FailUpload(ctx, expireCommand)
	if err != nil {
		t.Fatalf("FailUpload(expired) error = %v", err)
	}
	expiredReplay, err := repository.FailUpload(ctx, expireCommand)
	if err != nil || expiredReplay != expiredResult {
		t.Fatalf("FailUpload(expired) replay = (%#v, %v), want %#v", expiredReplay, err, expiredResult)
	}

	var logicalBytes, reservedBytes, physicalBytes int64
	var availableCount, rejectedCount, expiredCount, blobCount int
	if err := fixture.db.QueryRow(ctx, `
		select quota.logical_bytes, quota.reserved_bytes, quota.physical_bytes,
		  (select count(*)::int from public.attachment_uploads where upload_state = 'available'),
		  (select count(*)::int from public.attachment_uploads where upload_state = 'rejected'),
		  (select count(*)::int from public.attachment_uploads where upload_state = 'expired'),
		  (select count(*)::int from public.blob_objects)
		from public.attachment_quota_accounts quota
		where quota.project_id = 'default'
	`).Scan(&logicalBytes, &reservedBytes, &physicalBytes, &availableCount, &rejectedCount, &expiredCount, &blobCount); err != nil {
		t.Fatalf("read attachment quota lifecycle: %v", err)
	}
	if logicalBytes != 18 || reservedBytes != 0 || physicalBytes != 9 ||
		availableCount != 2 || rejectedCount != 1 || expiredCount != 1 || blobCount != 1 {
		t.Fatalf("attachment quota lifecycle = quota %d/%d/%d states %d/%d/%d blobs %d", logicalBytes, reservedBytes, physicalBytes, availableCount, rejectedCount, expiredCount, blobCount)
	}
	quotaSnapshot, err := repository.GetProjectQuotaSnapshot(ctx, attachments.ProjectQuotaSnapshotCommand{
		ProjectID: "default", Limits: limits,
	})
	if err != nil {
		t.Fatalf("GetProjectQuotaSnapshot() error = %v", err)
	}
	if quotaSnapshot.Usage != (attachments.QuotaUsage{LogicalBytes: 18, PhysicalBytes: 9}) || !quotaSnapshot.ProjectWarning {
		t.Fatalf("GetProjectQuotaSnapshot() = %#v", quotaSnapshot)
	}
}

func TestPostgresIntegrationAttachmentExistingRecordDraftUsesEffectiveRecordQuota(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-effective-record", 2),
	)
	limits := attachments.Limits{
		MaxFileBytes:              10,
		MaxRecordBytes:            10,
		MaxProjectBytes:           100,
		WarningPercent:            80,
		MaxInlineTextPreviewBytes: 5,
	}

	if _, err := fixture.db.Exec(ctx, `
		insert into public.records (record_id) values ('rec_effective1');
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level, visibility_scope,
			visibility_digest, author_id, canonical_hash
		) values (
			'rrv_effective1', 'rec_effective1', 1, 'Effective', '', 1, 'note',
			'informational', '{}'::jsonb, decode(repeat('51', 32), 'hex'),
			'usr_effective1', decode(repeat('52', 32), 'hex')
		);
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values (
			'rrv_effective1', 0, 1, 'vps', 'affected', 'vps_effective1', true,
			'{}'::jsonb, '{}'::jsonb, decode(repeat('53', 32), 'hex')
		);
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
			decode(repeat('dd', 32), 'hex'), 'local-existing-v1', 5, 'local'
		);
		insert into public.record_attachments (
			attachment_id, record_id, attachment_state, display_name, media_type,
			logical_size_bytes, blob_key, blob_object_version, created_by
		) values (
			'att_effectiveold', 'rec_effective1', 'available', 'old.txt', 'text/plain', 5,
			'sha256/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
			'local-existing-v1', 'usr_effective1'
		);
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, physical_bytes
		) values ('default', 5, 5);
		insert into public.record_drafts (
			draft_id, record_id, base_revision_id, author_id, payload, payload_hash,
			draft_version, etag_digest, warning_at, expires_at
		) values (
			'rdf_effective1', 'rec_effective1', 'rrv_effective1', 'usr_effective1',
			'{}'::jsonb, decode(repeat('54', 32), 'hex'), 1,
			decode(repeat('55', 32), 'hex'), now() + interval '1 day', now() + interval '2 days'
		)
	`); err != nil {
		t.Fatalf("seed existing-record attachment quota: %v", err)
	}

	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_effective1", AttachmentID: "att_effectivenew",
		DraftID: "rdf_effective1", AuthorID: "usr_effective1", DisplayName: "new.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindLocal,
		DeclaredSizeBytes: 4, ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: limits,
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload(existing record) error = %v", err)
	}
	var digest [32]byte
	for index := range digest {
		digest[index] = 0xee
	}
	completeAttachmentIntegrationUpload(t, ctx, repository, reserve, attachments.ObjectVersion{
		Key:       "sha256/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		VersionID: "local-new-v1", SHA256: digest, SizeBytes: 4,
	}, [32]byte{1}, "apj_effective1")
	admitted, err := repository.AdmitUpload(ctx, attachments.AdmitUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		Blob: attachments.BlobObject{
			Key:    "sha256/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			SHA256: digest, ObjectVersion: "local-new-v1", SizeBytes: 4, BackendKind: attachments.BackendKindLocal,
		},
		Limits: limits,
	})
	if err != nil {
		t.Fatalf("AdmitUpload(existing record) error = %v", err)
	}
	if admitted.Quota.EffectiveRecordBytes != 9 {
		t.Fatalf("AdmitUpload(existing record) effective bytes = %d, want 9", admitted.Quota.EffectiveRecordBytes)
	}

	reserve.UploadID = "aup_effective2"
	reserve.AttachmentID = "att_effective2"
	reserve.DeclaredSizeBytes = 2
	if _, err := repository.ReserveUpload(ctx, reserve); !errors.Is(err, attachments.ErrQuotaExceeded) {
		t.Fatalf("ReserveUpload(over effective record quota) error = %v, want ErrQuotaExceeded", err)
	}
}

func TestPostgresIntegrationAttachmentCrossRecordCopyRetainsBlobAndChargesLogicalQuota(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-cross-record-copy", 2),
	)

	if _, err := fixture.db.Exec(ctx, `
		insert into public.records (record_id) values ('rec_copysource'), ('rec_copytarget');
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			decode(repeat('bb', 32), 'hex'), 'local-v1', 9, 'local'
		);
		insert into public.record_attachments (
			attachment_id, record_id, attachment_state, display_name, media_type,
			logical_size_bytes, blob_key, blob_object_version, created_by
		) values (
			'att_copysource', 'rec_copysource', 'available', 'source.txt', 'text/plain',
			9, 'sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			'local-v1', 'usr_copysource'
		);
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, physical_bytes
		) values ('default', 9, 9)
	`); err != nil {
		t.Fatalf("seed cross-record attachment copy: %v", err)
	}

	result, err := repository.CopyAttachment(ctx, attachments.CopyAttachmentCommand{
		ProjectID:          "default",
		SourceRecordID:     "rec_copysource",
		TargetRecordID:     "rec_copytarget",
		SourceAttachmentID: "att_copysource",
		TargetAttachmentID: "att_copytarget",
		ActorID:            "usr_copyactor",
		Limits:             attachments.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("CopyAttachment() error = %v", err)
	}
	if result.Quota.Usage != (attachments.QuotaUsage{LogicalBytes: 18, PhysicalBytes: 9}) ||
		result.Quota.EffectiveRecordBytes != 9 {
		t.Fatalf("CopyAttachment() quota = %#v", result.Quota)
	}

	var recordID, copiedFromID, blobKey, blobVersion string
	var logicalBytes, projectLogicalBytes, projectPhysicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select attachment.record_id, attachment.copied_from_attachment_id,
		       attachment.blob_key, attachment.blob_object_version,
		       attachment.logical_size_bytes, quota.logical_bytes, quota.physical_bytes
		from public.record_attachments attachment
		join public.attachment_quota_accounts quota on quota.project_id = attachment.project_id
		where attachment.attachment_id = 'att_copytarget'
	`).Scan(
		&recordID, &copiedFromID, &blobKey, &blobVersion,
		&logicalBytes, &projectLogicalBytes, &projectPhysicalBytes,
	); err != nil {
		t.Fatalf("read cross-record attachment copy: %v", err)
	}
	if recordID != "rec_copytarget" || copiedFromID != "att_copysource" ||
		blobKey != "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		blobVersion != "local-v1" || logicalBytes != 9 || projectLogicalBytes != 18 || projectPhysicalBytes != 9 {
		t.Fatalf("cross-record copy = %s/%s %s@%s logical=%d project=%d/%d",
			recordID, copiedFromID, blobKey, blobVersion, logicalBytes, projectLogicalBytes, projectPhysicalBytes)
	}

	if _, err := fixture.db.Exec(ctx, `delete from public.record_attachments where attachment_id = 'att_copysource'`); err != nil {
		t.Fatalf("delete copied attachment source: %v", err)
	}
	var survivingLineage string
	if err := fixture.db.QueryRow(ctx, `
		select copied_from_attachment_id from public.record_attachments
		where attachment_id = 'att_copytarget'
	`).Scan(&survivingLineage); err != nil {
		t.Fatalf("read detached copied attachment lineage: %v", err)
	}
	if survivingLineage != "att_copysource" {
		t.Fatalf("detached copied attachment lineage = %q", survivingLineage)
	}
}

func TestPostgresIntegrationAttachmentBlobPinAndReferenceProtection(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-protection", 2),
	)
	blobKey := "sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	blobVersion := "local-protected-v1"

	if _, err := fixture.db.Exec(ctx, `
		insert into public.records (record_id) values ('rec_protecta'), ('rec_protectb');
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level, visibility_scope,
			visibility_digest, author_id, canonical_hash
		) values (
			'rrv_protecta', 'rec_protecta', 1, 'Protected', '', 1, 'note',
			'informational', '{}'::jsonb, decode(repeat('41', 32), 'hex'),
			'usr_protecta', decode(repeat('42', 32), 'hex')
		);
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values (
			'rrv_protecta', 0, 1, 'vps', 'affected', 'vps_protecta', true,
			'{}'::jsonb, '{}'::jsonb, decode(repeat('43', 32), 'hex')
		);
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			decode(repeat('cc', 32), 'hex'), 'local-protected-v1', 11, 'local'
		);
		insert into public.record_attachments (
			attachment_id, record_id, copied_from_attachment_id, attachment_state,
			display_name, media_type, logical_size_bytes, blob_key,
			blob_object_version, created_by
		) values
			('att_protecta', 'rec_protecta', null, 'available', 'a.txt', 'text/plain', 11,
			 'sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			 'local-protected-v1', 'usr_protecta'),
			('att_protectb', 'rec_protectb', 'att_protecta', 'available', 'b.txt', 'text/plain', 11,
			 'sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			 'local-protected-v1', 'usr_protectb');
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ('rec_protecta', 'rrv_protecta', 0, 'att_protecta')
	`); err != nil {
		t.Fatalf("seed Blob protection references: %v", err)
	}

	create := attachments.CreateBlobGCPinCommand{
		PinID:             "bgp_protected1",
		OwnerKind:         attachments.BlobGCPinOwnerBackupManifest,
		OwnerID:           "backup_protected1",
		BlobKey:           blobKey,
		BlobObjectVersion: blobVersion,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
	}
	protection, err := repository.CreateBlobGCPin(ctx, create)
	if err != nil {
		t.Fatalf("CreateBlobGCPin() error = %v", err)
	}
	if protection.BlobKey != blobKey || protection.BlobObjectVersion != blobVersion ||
		protection.LogicalAttachmentCount != 2 || protection.RevisionReferenceCount != 1 ||
		protection.ActivePinCount != 1 || !protection.Protected {
		t.Fatalf("CreateBlobGCPin() protection = %#v", protection)
	}

	protection, err = repository.ReleaseBlobGCPin(ctx, attachments.ReleaseBlobGCPinCommand{
		PinID:             create.PinID,
		OwnerKind:         create.OwnerKind,
		OwnerID:           create.OwnerID,
		BlobKey:           create.BlobKey,
		BlobObjectVersion: create.BlobObjectVersion,
	})
	if err != nil {
		t.Fatalf("ReleaseBlobGCPin() error = %v", err)
	}
	if protection.ActivePinCount != 0 || !protection.Protected ||
		protection.LogicalAttachmentCount != 2 || protection.RevisionReferenceCount != 1 {
		t.Fatalf("ReleaseBlobGCPin() referenced protection = %#v", protection)
	}

	if _, err := fixture.db.Exec(ctx, `
		delete from public.record_revision_attachments where revision_id = 'rrv_protecta';
		delete from public.record_attachments where attachment_id in ('att_protecta', 'att_protectb')
	`); err != nil {
		t.Fatalf("remove Blob protection references: %v", err)
	}
	protection, err = repository.GetBlobProtection(ctx, attachments.BlobProtectionCommand{
		BlobKey: blobKey, BlobObjectVersion: blobVersion,
	})
	if err != nil {
		t.Fatalf("GetBlobProtection() error = %v", err)
	}
	if protection.Protected || protection.LogicalAttachmentCount != 0 ||
		protection.RevisionReferenceCount != 0 || protection.ActivePinCount != 0 {
		t.Fatalf("GetBlobProtection() unprotected = %#v", protection)
	}
}

func TestPostgresIntegrationAttachmentBlobGCDurableLifecycleAndReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-gc-lifecycle", 2),
	)
	now := time.Now().UTC()
	young := attachmentBlobGCIntegrationObject("young Blob GC object", "local-gc-young-v1", attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, young, now.Add(-23*time.Hour))
	request := attachments.BlobGCClaimRequest{
		ProjectID: "default", BackendKind: attachments.BackendKindLocal,
		Mode: attachments.BlobGCPurgeModeOrdinary, OrphanedBefore: now.Add(-24 * time.Hour),
		OwnerID: "postgres_gc_worker_1", OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration,
	}
	claim, err := repository.ClaimBlobGC(ctx, request)
	if err != nil || claim != nil {
		t.Fatalf("ClaimBlobGC(younger than watermark) = (%#v, %v), want nil/nil", claim, err)
	}

	orphan := attachmentBlobGCIntegrationObject("durable Blob GC object", "local-gc-durable-v1", attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, orphan, now.Add(-25*time.Hour))
	claim, err = repository.ClaimBlobGC(ctx, request)
	if err != nil {
		t.Fatalf("ClaimBlobGC(orphan) error = %v", err)
	}
	if claim == nil || claim.Candidate.Object != orphan || claim.Mode != attachments.BlobGCPurgeModeOrdinary ||
		claim.OwnerID != request.OwnerID || claim.OwnerGeneration != 1 || claim.Attempt != 1 {
		t.Fatalf("ClaimBlobGC(orphan) = %#v", claim)
	}

	var orphanMetadataCount, youngMetadataCount, claimedDeletionCount int64
	var physicalBytes, quotaVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.blob_objects where blob_key = $1),
		       (select count(*) from public.blob_objects where blob_key = $2),
		       (select count(*) from public.blob_gc_deletions
		         where deletion_id = $3 and deletion_state = 'claimed'),
		       quota.physical_bytes, quota.quota_version
		from public.attachment_quota_accounts quota where quota.project_id = 'default'`,
		orphan.Key, young.Key, claim.DeletionID,
	).Scan(
		&orphanMetadataCount, &youngMetadataCount, &claimedDeletionCount,
		&physicalBytes, &quotaVersion,
	); err != nil {
		t.Fatalf("read durable Blob GC claim state: %v", err)
	}
	wantPhysicalBeforeCompletion := young.SizeBytes + orphan.SizeBytes
	if orphanMetadataCount != 0 || youngMetadataCount != 1 || claimedDeletionCount != 1 ||
		physicalBytes != wantPhysicalBeforeCompletion || quotaVersion != 0 {
		t.Fatalf("durable claim state = metadata %d/%d deletion %d quota %d@%d, want 0/1/1 %d@0",
			orphanMetadataCount, youngMetadataCount, claimedDeletionCount,
			physicalBytes, quotaVersion, wantPhysicalBeforeCompletion)
	}

	receipt := attachments.DeletionReceipt{Version: storeObjectVersion(orphan), Deleted: true}
	completion := attachments.BlobGCCompletionRequest{Claim: *claim, Receipt: receipt}
	result, err := repository.CompleteBlobGC(ctx, completion)
	if err != nil {
		t.Fatalf("CompleteBlobGC() error = %v", err)
	}
	if result.DeletionID != claim.DeletionID || result.Candidate != claim.Candidate || result.Receipt != receipt {
		t.Fatalf("CompleteBlobGC() = %#v", result)
	}
	assertAttachmentBlobGCQuota(t, ctx, fixture, young.SizeBytes, 1)

	replay, err := repository.CompleteBlobGC(ctx, completion)
	if err != nil || replay != result {
		t.Fatalf("CompleteBlobGC(replay) = (%#v, %v), want %#v", replay, err, result)
	}
	assertAttachmentBlobGCQuota(t, ctx, fixture, young.SizeBytes, 1)
	resolved, err := repository.ResolveBlobGC(ctx, attachments.BlobGCResolveRequest(completion))
	if err != nil || resolved == nil || *resolved != result {
		t.Fatalf("ResolveBlobGC(completed) = (%#v, %v), want %#v", resolved, err, result)
	}
}

func TestPostgresIntegrationAttachmentBlobGCProtectsReferencesAndCleansExpiredPins(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-gc-protection", 2),
	)
	now := time.Now().UTC()
	tests := []struct {
		name string
		seed func(*testing.T, attachments.BlobObject)
	}{
		{
			name: "original attachment",
			seed: func(t *testing.T, object attachments.BlobObject) {
				seedAttachmentBlobGCOriginalReference(t, ctx, fixture, "gcoriginal", object)
			},
		},
		{
			name: "preview attachment",
			seed: func(t *testing.T, object attachments.BlobObject) {
				seedAttachmentBlobGCPreviewReference(t, ctx, fixture, "gcpreview", object, now)
			},
		},
		{
			name: "upload part",
			seed: func(t *testing.T, object attachments.BlobObject) {
				seedAttachmentBlobGCUploadPartReference(t, ctx, fixture, "gcuploadpart", object)
			},
		},
		{
			name: "active pin",
			seed: func(t *testing.T, object attachments.BlobObject) {
				seedAttachmentBlobGCPin(t, ctx, fixture, "bgp_gcactive", "gc_active_pin", object,
					now.Add(-time.Hour), now.Add(time.Hour))
			},
		},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := attachmentBlobGCIntegrationObject(
				fmt.Sprintf("protected Blob GC object %d", index),
				fmt.Sprintf("local-gc-protected-v%d", index+1),
				attachments.BackendKindLocal,
			)
			seedAttachmentBlobGCObject(t, ctx, fixture, object, now.Add(-48*time.Hour))
			tt.seed(t, object)
			claim, err := repository.ClaimBlobGC(ctx, attachmentBlobGCPermanentClaimRequest(object, "postgres_gc_protection"))
			if err != nil || claim != nil {
				t.Fatalf("ClaimBlobGC(protected) = (%#v, %v), want nil/nil", claim, err)
			}
			var metadataCount int64
			if err := fixture.db.QueryRow(ctx, `select count(*) from public.blob_objects where blob_key = $1`, object.Key).Scan(&metadataCount); err != nil {
				t.Fatalf("count protected Blob metadata: %v", err)
			}
			if metadataCount != 1 {
				t.Fatalf("protected Blob metadata count = %d, want 1", metadataCount)
			}
		})
	}

	expired := attachmentBlobGCIntegrationObject("expired pin Blob GC object", "local-gc-expired-pin-v1", attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, expired, now.Add(-48*time.Hour))
	seedAttachmentBlobGCPin(t, ctx, fixture, "bgp_gcexpired", "gc_expired_pin", expired,
		now.Add(-48*time.Hour), now.Add(-time.Hour))
	claim, err := repository.ClaimBlobGC(ctx, attachmentBlobGCPermanentClaimRequest(expired, "postgres_gc_expired_pin"))
	if err != nil {
		t.Fatalf("ClaimBlobGC(expired pin) error = %v", err)
	}
	if claim == nil || claim.Candidate.Object != expired {
		t.Fatalf("ClaimBlobGC(expired pin) = %#v", claim)
	}
	var pinCount, metadataCount int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.blob_gc_pins where pin_id = 'bgp_gcexpired'),
		       (select count(*) from public.blob_objects where blob_key = $1)`, expired.Key,
	).Scan(&pinCount, &metadataCount); err != nil {
		t.Fatalf("read expired pin GC state: %v", err)
	}
	if pinCount != 0 || metadataCount != 0 {
		t.Fatalf("expired pin GC state = pin %d metadata %d, want 0/0", pinCount, metadataCount)
	}
}

func TestPostgresIntegrationAttachmentBlobGCRetryTakeoverFencesStaleOwner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-gc-takeover", 2),
	)
	now := time.Now().UTC()
	object := attachmentBlobGCIntegrationObject("retry Blob GC object", "local-gc-retry-v1", attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, object, now.Add(-48*time.Hour))
	first, err := repository.ClaimBlobGC(ctx, attachmentBlobGCPermanentClaimRequest(object, "postgres_gc_owner_old"))
	if err != nil || first == nil {
		t.Fatalf("ClaimBlobGC(first) = (%#v, %v)", first, err)
	}
	if err := repository.RetryBlobGC(ctx, attachments.BlobGCRetryRequest{
		Claim: *first, RetryAt: time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatalf("RetryBlobGC() error = %v", err)
	}
	second, err := repository.ClaimBlobGC(ctx, attachmentBlobGCPermanentClaimRequest(object, "postgres_gc_owner_new"))
	if err != nil {
		t.Fatalf("ClaimBlobGC(takeover) error = %v", err)
	}
	if second == nil || second.DeletionID != first.DeletionID || second.OwnerID != "postgres_gc_owner_new" ||
		second.OwnerGeneration != first.OwnerGeneration+1 || second.Attempt != first.Attempt+1 {
		t.Fatalf("ClaimBlobGC(takeover) = %#v, first %#v", second, first)
	}
	receipt := attachments.DeletionReceipt{Version: storeObjectVersion(object), Deleted: false}
	if _, err := repository.CompleteBlobGC(ctx, attachments.BlobGCCompletionRequest{
		Claim: *first, Receipt: receipt,
	}); !errors.Is(err, attachments.ErrBlobGCClaimLost) {
		t.Fatalf("CompleteBlobGC(stale owner) error = %v, want ErrBlobGCClaimLost", err)
	}
	result, err := repository.CompleteBlobGC(ctx, attachments.BlobGCCompletionRequest{
		Claim: *second, Receipt: receipt,
	})
	if err != nil {
		t.Fatalf("CompleteBlobGC(current owner) error = %v", err)
	}
	if result.Receipt.Deleted || result.DeletionID != second.DeletionID {
		t.Fatalf("CompleteBlobGC(current owner) = %#v", result)
	}
	assertAttachmentBlobGCQuota(t, ctx, fixture, 0, 1)
}

func TestPostgresIntegrationAttachmentBlobGCFenceBlocksNewReferences(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-blob-gc-publication-fence", 2)
	repository := NewPostgresAttachmentRepository(runtimePool)
	now := time.Now().UTC()
	object := attachmentBlobGCIntegrationObject("publication fence Blob GC object", "local-gc-fence-v1", attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, object, now.Add(-48*time.Hour))
	seedAttachmentBlobGCUploadShell(t, ctx, fixture, "gcfence")
	claim, err := repository.ClaimBlobGC(ctx, attachmentBlobGCPermanentClaimRequest(object, "postgres_gc_fence"))
	if err != nil || claim == nil {
		t.Fatalf("ClaimBlobGC() = (%#v, %v)", claim, err)
	}

	blobTx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Blob metadata fence transaction: %v", err)
	}
	if inserted, err := ensureAttachmentBlob(ctx, blobTx, object); inserted || !errors.Is(err, attachments.ErrBlobGCProtected) {
		_ = blobTx.Rollback(ctx)
		t.Fatalf("ensureAttachmentBlob(after claim) = (%t, %v), want false/ErrBlobGCProtected", inserted, err)
	}
	if err := blobTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback Blob metadata fence transaction: %v", err)
	}

	partTx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin upload-part fence transaction: %v", err)
	}
	if err := ensureAttachmentUploadPart(ctx, partTx, "aup_gcfence", storeObjectVersion(object)); !errors.Is(err, attachments.ErrBlobGCProtected) {
		_ = partTx.Rollback(ctx)
		t.Fatalf("ensureAttachmentUploadPart(after claim) error = %v, want ErrBlobGCProtected", err)
	}
	if err := partTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback upload-part fence transaction: %v", err)
	}

	_, pinErr := repository.CreateBlobGCPin(ctx, attachments.CreateBlobGCPinCommand{
		PinID: "bgp_gcfence", OwnerKind: attachments.BlobGCPinOwnerBackupManifest,
		OwnerID: "gc_fence_pin", BlobKey: object.Key, BlobObjectVersion: object.ObjectVersion,
		ExpiresAt: now.Add(time.Hour),
	})
	if pinErr == nil {
		t.Fatal("CreateBlobGCPin(after claim) succeeded")
	}
	var blobCount, partCount, pinCount, activeDeletionCount int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.blob_objects where blob_key = $1),
		       (select count(*) from public.attachment_upload_parts
		         where sha256_digest = $2 and object_version = $3),
		       (select count(*) from public.blob_gc_pins
		         where blob_key = $1 and blob_object_version = $3),
		       (select count(*) from public.blob_gc_deletions
		         where deletion_id = $4 and deletion_state <> 'completed')`,
		object.Key, object.SHA256[:], object.ObjectVersion, claim.DeletionID,
	).Scan(&blobCount, &partCount, &pinCount, &activeDeletionCount); err != nil {
		t.Fatalf("read publication fence state: %v", err)
	}
	if blobCount != 0 || partCount != 0 || pinCount != 0 || activeDeletionCount != 1 {
		t.Fatalf("publication fence state = blob %d part %d pin %d active deletion %d, want 0/0/0/1",
			blobCount, partCount, pinCount, activeDeletionCount)
	}
}

func TestPostgresIntegrationAttachmentBlobGCLocalWorkerWorkflow(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-gc-local-worker", 2),
	)
	blob, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	worker, err := attachments.NewBlobGCWorker(repository, blob, attachments.BlobGCWorkerOptions{
		BackendKind: attachments.BackendKindLocal, OwnerID: "postgres_local_gc_worker",
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	content := []byte("real local Blob GC bytes")
	digest := sha256.Sum256(content)
	version, err := blob.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("LocalBlobStore.Put() error = %v", err)
	}
	object := attachmentBlobObjectFromVersion(version, attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, object, time.Now().UTC().Add(-48*time.Hour))
	worked, err := worker.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("BlobGCWorker.RunOnce(deleted) = (%t, %v), want true/nil", worked, err)
	}
	if _, err := blob.Stat(ctx, version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("LocalBlobStore.Stat(after GC) error = %v, want ErrBlobNotFound", err)
	}
	assertAttachmentBlobGCTerminalResult(t, ctx, fixture, object, "deleted")
	assertAttachmentBlobGCQuota(t, ctx, fixture, 0, 1)

	absentContent := []byte("already absent local Blob GC bytes")
	absentDigest := sha256.Sum256(absentContent)
	absentVersion, err := blob.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: absentDigest, ExpectedSizeBytes: int64(len(absentContent)),
	}, bytes.NewReader(absentContent))
	if err != nil {
		t.Fatalf("LocalBlobStore.Put(already absent) error = %v", err)
	}
	deleted, err := blob.Delete(ctx, absentVersion)
	if err != nil || !deleted.Deleted {
		t.Fatalf("LocalBlobStore.Delete(pre-GC) = (%#v, %v)", deleted, err)
	}
	absentObject := attachmentBlobObjectFromVersion(absentVersion, attachments.BackendKindLocal)
	seedAttachmentBlobGCObject(t, ctx, fixture, absentObject, time.Now().UTC().Add(-48*time.Hour))
	worked, err = worker.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("BlobGCWorker.RunOnce(already absent) = (%t, %v), want true/nil", worked, err)
	}
	assertAttachmentBlobGCTerminalResult(t, ctx, fixture, absentObject, "already_absent")
	assertAttachmentBlobGCQuota(t, ctx, fixture, 0, 2)
	worked, err = worker.RunOnce(ctx)
	if err != nil || worked {
		t.Fatalf("BlobGCWorker.RunOnce(replay) = (%t, %v), want false/nil", worked, err)
	}
}

func TestPostgresMinIOIntegrationAttachmentBlobGCExactVersionWorkflow(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-gc-minio-worker", 2),
	)
	worker, err := attachments.NewBlobGCWorker(repository, blob, attachments.BlobGCWorkerOptions{
		BackendKind: attachments.BackendKindS3, OwnerID: "postgres_minio_gc_worker",
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	content := []byte("real MinIO exact-version Blob GC bytes")
	digest := sha256.Sum256(content)
	temporaryDigest := sha256.Sum256([]byte("MinIO Blob GC temporary object"))
	version, err := blob.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
		TemporaryKey: fmt.Sprintf("temporary/%x", temporaryDigest),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("S3BlobStore.Put() error = %v", err)
	}
	object := attachmentBlobObjectFromVersion(version, attachments.BackendKindS3)
	seedAttachmentBlobGCObject(t, ctx, fixture, object, time.Now().UTC().Add(-48*time.Hour))
	worked, err := worker.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("BlobGCWorker.RunOnce(MinIO deleted) = (%t, %v), want true/nil", worked, err)
	}
	if _, err := blob.Stat(ctx, version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("S3BlobStore.Stat(after GC) error = %v, want ErrBlobNotFound", err)
	}
	assertAttachmentBlobGCTerminalResult(t, ctx, fixture, object, "deleted")
	assertAttachmentBlobGCQuota(t, ctx, fixture, 0, 1)

	absentContent := []byte("already absent MinIO exact-version Blob GC bytes")
	absentDigest := sha256.Sum256(absentContent)
	absentTemporaryDigest := sha256.Sum256([]byte("absent MinIO Blob GC temporary object"))
	absentVersion, err := blob.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: absentDigest, ExpectedSizeBytes: int64(len(absentContent)),
		TemporaryKey: fmt.Sprintf("temporary/%x", absentTemporaryDigest),
	}, bytes.NewReader(absentContent))
	if err != nil {
		t.Fatalf("S3BlobStore.Put(already absent) error = %v", err)
	}
	deleted, err := blob.Delete(ctx, absentVersion)
	if err != nil || !deleted.Deleted {
		t.Fatalf("S3BlobStore.Delete(pre-GC) = (%#v, %v)", deleted, err)
	}
	absentObject := attachmentBlobObjectFromVersion(absentVersion, attachments.BackendKindS3)
	seedAttachmentBlobGCObject(t, ctx, fixture, absentObject, time.Now().UTC().Add(-48*time.Hour))
	worked, err = worker.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("BlobGCWorker.RunOnce(MinIO already absent) = (%t, %v), want true/nil", worked, err)
	}
	assertAttachmentBlobGCTerminalResult(t, ctx, fixture, absentObject, "already_absent")
	assertAttachmentBlobGCQuota(t, ctx, fixture, 0, 2)
	worked, err = worker.RunOnce(ctx)
	if err != nil || worked {
		t.Fatalf("BlobGCWorker.RunOnce(MinIO replay) = (%t, %v), want false/nil", worked, err)
	}
}

func TestPostgresIntegrationAttachmentTemporaryObjectCleanupPersistsVersionAndMarker(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_cleanuprestart", "usr_cleanuprestart")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-cleanup-restart", 2),
	)
	expiresAt := time.Now().UTC().Add(time.Hour)
	limits := attachments.DefaultLimits()
	if _, err := repository.ReserveUpload(ctx, attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_cleanuprestart", AttachmentID: "att_cleanuprestart",
		DraftID: "rdf_cleanuprestart", AuthorID: "usr_cleanuprestart", DisplayName: "restart.zip",
		MediaType: "application/zip", TransportKind: attachments.TransportKindS3,
		DeclaredSizeBytes: 32, ExpiresAt: expiresAt, Limits: limits,
	}); err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	key := "temporary/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: "aup_cleanuprestart", AuthorID: "usr_cleanuprestart",
		CandidateTemporaryObjectKey: key,
	}); err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_uploads
		set created_at = transaction_timestamp() - interval '2 minutes',
		    expires_at = transaction_timestamp() - interval '1 minute'
		where upload_id = 'aup_cleanuprestart'`); err != nil {
		t.Fatalf("expire cleanup fixture: %v", err)
	}
	claimed, err := repository.ClaimTemporaryObjectCleanup(ctx, attachments.TemporaryObjectCleanupClaimInput{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil || claimed == nil || claimed.TemporaryObjectKey != key || claimed.TemporaryObjectVersion != "" {
		t.Fatalf("ClaimTemporaryObjectCleanup() = (%#v, %v), want known key without version", claimed, err)
	}
	versioned, err := repository.RecordTemporaryObjectVersion(ctx, attachments.RecordTemporaryObjectVersionCommand{
		ProjectID: "default", UploadID: claimed.UploadID, AuthorID: claimed.AuthorID,
		TemporaryObjectKey: key, TemporaryObjectVersion: "restart-observed-v1",
	})
	if err != nil || versioned.TemporaryObjectVersion != "restart-observed-v1" {
		t.Fatalf("RecordTemporaryObjectVersion() = (%#v, %v)", versioned, err)
	}
	claimed.TemporaryObjectVersion = versioned.TemporaryObjectVersion
	if err := repository.MarkTemporaryObjectCleaned(ctx, *claimed); err != nil {
		t.Fatalf("MarkTemporaryObjectCleaned() error = %v", err)
	}
	if replay, err := repository.ClaimTemporaryObjectCleanup(ctx, attachments.TemporaryObjectCleanupClaimInput{
		ProjectID: "default", RetryDelay: time.Minute,
	}); err != nil || replay != nil {
		t.Fatalf("ClaimTemporaryObjectCleanup(replay) = (%#v, %v), want no candidate", replay, err)
	}
	var deletedAt *time.Time
	if err := fixture.db.QueryRow(ctx, `
		select temporary_object_deleted_at
		from public.attachment_uploads
		where upload_id = 'aup_cleanuprestart'`).Scan(&deletedAt); err != nil {
		t.Fatalf("read cleanup marker: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("temporary object cleanup marker is null")
	}
}

func TestPostgresIntegrationAttachmentProcessorClaimEligibilityAndOrdering(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-eligibility", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)

	queuedSource := attachmentProcessorBlob(0x11, 11, "claim-queued-v1")
	queuedSource.BackendKind = attachments.BackendKindS3
	queued := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimtieb", Source: queuedSource,
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
		CreatedAt: now.Add(-30 * time.Minute), ExpiresAt: now.Add(2 * time.Minute),
		MaxAttempts: 4, ReservedQuotaBytes: 11,
	})
	dueRetry := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimtiea", Source: attachmentProcessorBlob(0x12, 12, "claim-retry-v1"),
		State: attachments.ProcessorStateRetryWait, Profile: attachments.ProcessorProfileArchive,
		Attempt: 1, OwnerGeneration: 4, RetryAt: attachmentProcessorTime(now.Add(-time.Minute)),
		CreatedAt: now.Add(-30 * time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 4, ReservedQuotaBytes: 12,
	})
	stale := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimstale", Source: attachmentProcessorBlob(0x13, 13, "claim-stale-v1"),
		State: attachments.ProcessorStateClaimed, Profile: attachments.ProcessorProfileArchive,
		Attempt: 2, OwnerID: "processor_stale", OwnerGeneration: 8,
		LeaseExpiresAt: attachmentProcessorTime(now.Add(-time.Minute)),
		CreatedAt:      now.Add(-10 * time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 4, ReservedQuotaBytes: 13,
	})
	seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimretryfuture", Source: attachmentProcessorBlob(0x14, 14, "claim-retry-future-v1"),
		State: attachments.ProcessorStateRetryWait, Profile: attachments.ProcessorProfileArchive,
		Attempt: 1, OwnerGeneration: 2, RetryAt: attachmentProcessorTime(now.Add(time.Hour)),
		CreatedAt: now.Add(-50 * time.Minute), ExpiresAt: now.Add(2 * time.Hour),
		MaxAttempts: 4, ReservedQuotaBytes: 14,
	})
	seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimlive", Source: attachmentProcessorBlob(0x15, 15, "claim-live-v1"),
		State: attachments.ProcessorStateClaimed, Profile: attachments.ProcessorProfileArchive,
		Attempt: 1, OwnerID: "processor_live", OwnerGeneration: 3,
		LeaseExpiresAt: attachmentProcessorTime(now.Add(time.Hour)),
		CreatedAt:      now.Add(-40 * time.Minute), ExpiresAt: now.Add(2 * time.Hour),
		MaxAttempts: 4, ReservedQuotaBytes: 15,
	})
	seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimmaxed", Source: attachmentProcessorBlob(0x16, 16, "claim-maxed-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
		Attempt: 4, OwnerGeneration: 4,
		CreatedAt: now.Add(-60 * time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 4, ReservedQuotaBytes: 16,
	})
	seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimoverall", Source: attachmentProcessorBlob(0x17, 17, "claim-overall-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
		MaxAttempts: 4, ReservedQuotaBytes: 17,
	})
	seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimrejected", Source: attachmentProcessorBlob(0x18, 18, "claim-rejected-v1"),
		State: attachments.ProcessorStateRejected, Profile: attachments.ProcessorProfileArchive,
		Attempt: 1, OwnerGeneration: 1, UploadState: attachments.UploadStateRejected,
		ResultCode: attachments.ProcessorResultCodeMalware,
		CreatedAt:  now.Add(-90 * time.Minute), ExpiresAt: now.Add(time.Hour), MaxAttempts: 4,
	})
	seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "claimexpired", Source: attachmentProcessorBlob(0x19, 19, "claim-expired-v1"),
		State: attachments.ProcessorStateExpired, Profile: attachments.ProcessorProfileArchive,
		Attempt: 2, OwnerGeneration: 2, UploadState: attachments.UploadStateExpired,
		ResultCode: attachments.ProcessorResultCodeTimeout,
		CreatedAt:  now.Add(-80 * time.Minute), ExpiresAt: now.Add(time.Hour), MaxAttempts: 4,
	})

	wants := []struct {
		seed       attachmentProcessorSeedResult
		attempt    int64
		generation int64
	}{
		{seed: dueRetry, attempt: 2, generation: 5},
		{seed: queued, attempt: 1, generation: 1},
		{seed: stale, attempt: 3, generation: 9},
	}
	for index, want := range wants {
		claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
			OwnerID: fmt.Sprintf("processor_claim_%d", index), OwnerLeaseDuration: 5 * time.Minute,
		})
		if err != nil {
			t.Fatalf("ClaimProcessorJob(%d) error = %v", index, err)
		}
		if claim == nil || claim.ProcessorJobID != want.seed.ProcessorJobID ||
			claim.UploadID != want.seed.UploadID || claim.AttachmentID != want.seed.AttachmentID ||
			claim.DisplayName != want.seed.DisplayName || claim.DeclaredMediaType != want.seed.DeclaredMediaType ||
			claim.Source != want.seed.Source || claim.Profile != want.seed.Profile ||
			claim.Attempt != want.attempt || claim.OwnerGeneration != want.generation ||
			claim.OwnerID != fmt.Sprintf("processor_claim_%d", index) ||
			claim.MaxAttempts != want.seed.MaxAttempts || !claim.ExpiresAt.Equal(want.seed.ExpiresAt) {
			t.Fatalf("ClaimProcessorJob(%d) = %#v, want exact durable claim for %#v", index, claim, want)
		}
		if !claim.LeaseExpiresAt.After(now) || claim.LeaseExpiresAt.After(claim.ExpiresAt) {
			t.Fatalf("ClaimProcessorJob(%d) lease = %s, overall expiry %s", index, claim.LeaseExpiresAt, claim.ExpiresAt)
		}
	}
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_claim_none", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claim != nil {
		t.Fatalf("ClaimProcessorJob(no remaining eligible) = (%#v, %v), want nil, nil", claim, err)
	}

	assertAttachmentProcessorPersistedJob(t, ctx, fixture, queued.ProcessorJobID,
		attachments.ProcessorStateClaimed, 1, 1, "processor_claim_1")
	assertAttachmentProcessorPersistedJob(t, ctx, fixture, dueRetry.ProcessorJobID,
		attachments.ProcessorStateClaimed, 2, 5, "processor_claim_0")
	assertAttachmentProcessorPersistedJob(t, ctx, fixture, stale.ProcessorJobID,
		attachments.ProcessorStateClaimed, 3, 9, "processor_claim_2")
	for _, unavailable := range []struct {
		jobID      string
		state      attachments.ProcessorState
		attempt    int64
		generation int64
		owner      string
	}{
		{jobID: "apj_claimretryfuture", state: attachments.ProcessorStateRetryWait, attempt: 1, generation: 2},
		{jobID: "apj_claimlive", state: attachments.ProcessorStateClaimed, attempt: 1, generation: 3, owner: "processor_live"},
		{jobID: "apj_claimmaxed", state: attachments.ProcessorStateQueued, attempt: 4, generation: 4},
		{jobID: "apj_claimoverall", state: attachments.ProcessorStateQueued},
		{jobID: "apj_claimrejected", state: attachments.ProcessorStateRejected, attempt: 1, generation: 1},
		{jobID: "apj_claimexpired", state: attachments.ProcessorStateExpired, attempt: 2, generation: 2},
	} {
		assertAttachmentProcessorPersistedJob(t, ctx, fixture, unavailable.jobID,
			unavailable.state, unavailable.attempt, unavailable.generation, unavailable.owner)
	}
}

func TestPostgresIntegrationAttachmentProcessorConcurrentClaimsAreDisjoint(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	fixture.installAttachmentProcessorClaimBlocker(t, ctx)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for index, suffix := range []string{"concurrenta", "concurrentb"} {
		seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
			Suffix: suffix, Source: attachmentProcessorBlob(byte(0x21+index), int64(21+index), "concurrent-v1"),
			State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
			CreatedAt: now.Add(-time.Minute + time.Duration(index)*time.Second), ExpiresAt: now.Add(time.Hour),
			MaxAttempts: 3, ReservedQuotaBytes: int64(21 + index),
		})
	}

	blocker := fixture.openDirectRuntimeConnection(t, ctx)
	defer blocker.Close(ctx)
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_lock($1)`, attachmentProcessorClaimBlockLock); err != nil {
		t.Fatalf("hold attachment processor claim blocker: %v", err)
	}
	defer func() {
		if _, err := blocker.Exec(context.Background(), `select pg_catalog.pg_advisory_unlock($1)`, attachmentProcessorClaimBlockLock); err != nil {
			t.Errorf("release attachment processor claim blocker: %v", err)
		}
	}()

	type claimResult struct {
		claim *attachments.ProcessorClaim
		err   error
	}
	results := make(chan claimResult, 2)
	start := func(owner, applicationName string) {
		repository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, applicationName, 1),
		)
		go func() {
			claim, err := repository.ClaimProcessorJob(context.Background(), attachments.ProcessorClaimInput{
				OwnerID: owner, OwnerLeaseDuration: time.Minute,
			})
			results <- claimResult{claim: claim, err: err}
		}()
	}
	start("processor_concurrent_a", "attachment-processor-concurrent-a")
	waitForRecordPlatformBackendLock(t, ctx, fixture.db, "attachment-processor-concurrent-a")
	start("processor_concurrent_b", "attachment-processor-concurrent-b")
	waitForRecordPlatformBackendLock(t, ctx, fixture.db, "attachment-processor-concurrent-b")
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_unlock($1)`, attachmentProcessorClaimBlockLock); err != nil {
		t.Fatalf("release attachment processor claim blocker: %v", err)
	}

	first := waitForRecordPlatformResult(t, results, "first attachment processor claim")
	second := waitForRecordPlatformResult(t, results, "second attachment processor claim")
	if first.err != nil || second.err != nil || first.claim == nil || second.claim == nil {
		t.Fatalf("concurrent ClaimProcessorJob() = (%#v, %v) / (%#v, %v)", first.claim, first.err, second.claim, second.err)
	}
	if first.claim.ProcessorJobID == second.claim.ProcessorJobID ||
		first.claim.UploadID == second.claim.UploadID || first.claim.AttachmentID == second.claim.AttachmentID {
		t.Fatalf("concurrent claims overlap: %#v / %#v", first.claim, second.claim)
	}
	if first.claim.OwnerID == second.claim.OwnerID || first.claim.Attempt != 1 || second.claim.Attempt != 1 {
		t.Fatalf("concurrent claim owners/attempts = %#v / %#v", first.claim, second.claim)
	}
}

func TestPostgresIntegrationAttachmentProcessorBoundedExpiryReleasesReservationExactlyOnce(t *testing.T) {
	tests := []struct {
		name      string
		seed      func(time.Time) attachmentProcessorSeed
		wantCode  attachments.ProcessorResultCode
		wantToken func(attachmentProcessorSeed, attachmentProcessorSeedResult, string) (string, time.Time)
	}{
		{
			name: "stale final-attempt claim",
			seed: func(now time.Time) attachmentProcessorSeed {
				return attachmentProcessorSeed{
					Suffix: "reapstale", Source: attachmentProcessorBlob(0xb1, 41, "reap-stale-v1"),
					State: attachments.ProcessorStateClaimed, Profile: attachments.ProcessorProfileArchive,
					Attempt: 3, MaxAttempts: 3, OwnerID: "processor_reap_stale", OwnerGeneration: 7,
					LeaseExpiresAt: attachmentProcessorTime(now.Add(-time.Minute)),
					CreatedAt:      now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), ReservedQuotaBytes: 41,
				}
			},
			wantCode: attachments.ProcessorResultCodeProcessingError,
			wantToken: func(seed attachmentProcessorSeed, _ attachmentProcessorSeedResult, _ string) (string, time.Time) {
				return seed.OwnerID, *seed.LeaseExpiresAt
			},
		},
		{
			name: "queued overall deadline",
			seed: func(now time.Time) attachmentProcessorSeed {
				return attachmentProcessorSeed{
					Suffix: "reapqueued", Source: attachmentProcessorBlob(0xb2, 42, "reap-queued-v1"),
					State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
					CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
					MaxAttempts: 3, ReservedQuotaBytes: 42,
				}
			},
			wantCode: attachments.ProcessorResultCodeTimeout,
			wantToken: func(_ attachmentProcessorSeed, result attachmentProcessorSeedResult, reaperOwner string) (string, time.Time) {
				return reaperOwner, result.ExpiresAt
			},
		},
		{
			name: "retry-wait overall deadline preserves prior result",
			seed: func(now time.Time) attachmentProcessorSeed {
				return attachmentProcessorSeed{
					Suffix: "reapretry", Source: attachmentProcessorBlob(0xb3, 43, "reap-retry-v1"),
					State: attachments.ProcessorStateRetryWait, Profile: attachments.ProcessorProfileArchive,
					Attempt: 1, MaxAttempts: 3, OwnerGeneration: 2,
					RetryAt:              attachmentProcessorTime(now.Add(time.Hour)),
					ResultCode:           attachments.ProcessorResultCodeScannerUnavailable,
					ResultOwnerID:        "processor_reap_prior",
					ResultLeaseExpiresAt: attachmentProcessorTime(now.Add(-90 * time.Minute)),
					CreatedAt:            now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
					ReservedQuotaBytes: 43,
				}
			},
			wantCode: attachments.ProcessorResultCodeScannerUnavailable,
			wantToken: func(seed attachmentProcessorSeed, _ attachmentProcessorSeedResult, _ string) (string, time.Time) {
				return seed.ResultOwnerID, *seed.ResultLeaseExpiresAt
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRecordsPostgresFixture(t, ctx)
			repository := NewPostgresAttachmentRepository(
				fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("attachment-processor-reaper-%d", index), 2),
			)
			now := time.Now().UTC().Truncate(time.Microsecond)
			seedInput := test.seed(now)
			seed := seedAttachmentProcessorJob(t, ctx, fixture, seedInput)
			reaperOwner := fmt.Sprintf("processor_expiry_reaper_%d", index)
			wantOwner, wantLeaseExpiry := test.wantToken(seedInput, seed, reaperOwner)
			wantResult := attachments.ProcessorResult{
				Source: seed.Source, Profile: seed.Profile, Code: test.wantCode,
			}
			wantDigest, err := wantResult.Digest()
			if err != nil {
				t.Fatalf("ProcessorResult.Digest(%s) error = %v", test.wantCode, err)
			}

			before := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			expired, err := repository.ExpireBoundedProcessorJob(ctx, attachments.ProcessorExpiryInput{
				ProjectID: "default", OwnerID: reaperOwner, Limits: attachments.DefaultLimits(),
			})
			if err != nil || expired == nil {
				t.Fatalf("ExpireBoundedProcessorJob() = (%#v, %v)", expired, err)
			}
			if expired.ProjectID != "default" || expired.ProcessorJobID != seed.ProcessorJobID ||
				expired.UploadID != seed.UploadID || expired.AttachmentID != seed.AttachmentID ||
				expired.ProcessorState != attachments.ProcessorStateExpired ||
				expired.UploadState != attachments.UploadStateExpired ||
				expired.AttachmentState != attachments.UploadStateExpired ||
				expired.ResultCode != test.wantCode || expired.ResultDigest != wantDigest ||
				expired.Quota.Usage != (attachments.QuotaUsage{}) ||
				expired.UploadState == attachments.UploadStateAvailable {
				t.Fatalf("ExpireBoundedProcessorJob() = %#v", expired)
			}
			assertAttachmentProcessorBoundedExpiryRows(
				t, ctx, fixture, seed, test.wantCode, wantDigest, wantOwner, wantLeaseExpiry,
			)

			firstToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			if firstToken.QuotaVersion != before.QuotaVersion+1 || firstToken.JobXID == before.JobXID ||
				firstToken.UploadXID == before.UploadXID || firstToken.AttachmentXID == before.AttachmentXID ||
				firstToken.QuotaXID == before.QuotaXID || firstToken.BlobXIDs != "" {
				t.Fatalf("bounded expiry mutation token = before %#v after %#v", before, firstToken)
			}
			replay, err := repository.ExpireBoundedProcessorJob(ctx, attachments.ProcessorExpiryInput{
				ProjectID: "default", OwnerID: reaperOwner, Limits: attachments.DefaultLimits(),
			})
			if err != nil || replay != nil {
				t.Fatalf("ExpireBoundedProcessorJob(replay) = (%#v, %v), want nil, nil", replay, err)
			}
			afterReplay := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			if afterReplay != firstToken {
				t.Fatalf("bounded expiry replay mutated durable rows:\nbefore %#v\nafter  %#v", firstToken, afterReplay)
			}
		})
	}
}

func TestPostgresIntegrationAttachmentProcessorBoundedExpiryLeavesLiveFinalAttemptUntouched(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-reaper-live", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "reaplive", Source: attachmentProcessorBlob(0xb4, 44, "reap-live-v1"),
		State: attachments.ProcessorStateClaimed, Profile: attachments.ProcessorProfileArchive,
		Attempt: 3, MaxAttempts: 3, OwnerID: "processor_reap_live", OwnerGeneration: 5,
		LeaseExpiresAt: attachmentProcessorTime(now.Add(10 * time.Minute)),
		CreatedAt:      now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), ReservedQuotaBytes: 44,
	})
	before := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	result, err := repository.ExpireBoundedProcessorJob(ctx, attachments.ProcessorExpiryInput{
		ProjectID: "default", OwnerID: "processor_expiry_reaper", Limits: attachments.DefaultLimits(),
	})
	if err != nil || result != nil {
		t.Fatalf("ExpireBoundedProcessorJob(live final attempt) = (%#v, %v), want nil, nil", result, err)
	}
	after := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if after != before {
		t.Fatalf("live final-attempt reaper mutated durable rows:\nbefore %#v\nafter  %#v", before, after)
	}
	assertAttachmentProcessorPersistedJob(t, ctx, fixture, seed.ProcessorJobID,
		attachments.ProcessorStateClaimed, 3, 5, "processor_reap_live")
	var reservedBytes, quotaVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select reserved_bytes, quota_version
		from public.attachment_quota_accounts where project_id = 'default'`,
	).Scan(&reservedBytes, &quotaVersion); err != nil {
		t.Fatalf("read live final-attempt quota: %v", err)
	}
	if reservedBytes != seed.Source.SizeBytes || quotaVersion != 0 {
		t.Fatalf("live final-attempt quota = reserved %d version %d", reservedBytes, quotaVersion)
	}
}

func TestPostgresIntegrationAttachmentProcessorConcurrentBoundedExpiryReleasesOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "reapconcurrent", Source: attachmentProcessorBlob(0xb5, 45, "reap-concurrent-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: 45,
	})

	type expiryResult struct {
		result *attachments.ProcessorCompletionResult
		err    error
	}
	results := make(chan expiryResult, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		repository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("attachment-processor-reaper-concurrent-%d", index), 1),
		)
		go func(repository *PostgresAttachmentRepository) {
			<-start
			result, err := repository.ExpireBoundedProcessorJob(context.Background(), attachments.ProcessorExpiryInput{
				ProjectID: "default", OwnerID: "processor_expiry_concurrent", Limits: attachments.DefaultLimits(),
			})
			results <- expiryResult{result: result, err: err}
		}(repository)
	}
	close(start)
	first := waitForRecordPlatformResult(t, results, "first concurrent attachment processor expiry")
	second := waitForRecordPlatformResult(t, results, "second concurrent attachment processor expiry")
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent ExpireBoundedProcessorJob() errors = %v / %v", first.err, second.err)
	}
	nonNil := 0
	for _, result := range []*attachments.ProcessorCompletionResult{first.result, second.result} {
		if result != nil {
			nonNil++
			if result.ProcessorJobID != seed.ProcessorJobID || result.ProcessorState != attachments.ProcessorStateExpired {
				t.Fatalf("concurrent bounded expiry result = %#v", result)
			}
		}
	}
	if nonNil != 1 {
		t.Fatalf("concurrent bounded expiry non-nil result count = %d, want 1: %#v / %#v",
			nonNil, first.result, second.result)
	}
	var reservedBytes, quotaVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select reserved_bytes, quota_version
		from public.attachment_quota_accounts where project_id = 'default'`,
	).Scan(&reservedBytes, &quotaVersion); err != nil {
		t.Fatalf("read concurrent bounded expiry quota: %v", err)
	}
	if reservedBytes != 0 || quotaVersion != 1 {
		t.Fatalf("concurrent bounded expiry quota = reserved %d version %d, want 0/1", reservedBytes, quotaVersion)
	}
}

func TestPostgresIntegrationAttachmentProcessorLeaseAndWorkspaceFencing(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "workspace", Source: attachmentProcessorBlob(0x31, 31, "workspace-source-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(30 * time.Minute),
		MaxAttempts: 4, ReservedQuotaBytes: 31,
	})
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_a", OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v)", claim, err)
	}
	if claim.ProcessorJobID != seed.ProcessorJobID || claim.Attempt != 1 || claim.OwnerGeneration != 1 {
		t.Fatalf("ClaimProcessorJob() = %#v, want first workspace attempt", claim)
	}

	pathDigest := sha256.Sum256([]byte("private workspace path"))
	registration := attachments.ProcessorWorkspaceRegistration{
		Claim: *claim, WorkspaceID: "cpw_workspace1",
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.LeaseExpiresAt,
	}
	claimMismatches := []struct {
		name   string
		mutate func(*attachments.ProcessorClaim)
	}{
		{name: "upload", mutate: func(input *attachments.ProcessorClaim) {
			input.UploadID = "aup_workspaceforged"
		}},
		{name: "attachment", mutate: func(input *attachments.ProcessorClaim) {
			input.AttachmentID = "att_workspaceforged"
		}},
		{name: "source", mutate: func(input *attachments.ProcessorClaim) {
			input.Source = attachmentProcessorBlob(0x32, input.Source.SizeBytes, "workspace-forged-v1")
		}},
		{name: "profile", mutate: func(input *attachments.ProcessorClaim) {
			input.Profile = attachments.ProcessorProfileText
		}},
		{name: "max attempts", mutate: func(input *attachments.ProcessorClaim) {
			input.MaxAttempts++
		}},
		{name: "overall expiry", mutate: func(input *attachments.ProcessorClaim) {
			input.ExpiresAt = input.ExpiresAt.Add(time.Minute)
		}},
	}
	for index, mismatch := range claimMismatches {
		t.Run("forged claim "+mismatch.name, func(t *testing.T) {
			forged := *claim
			mismatch.mutate(&forged)
			var beforeLease time.Time
			if err := fixture.db.QueryRow(ctx, `
				select lease_expires_at from public.attachment_processor_jobs
				where processor_job_id = $1`, claim.ProcessorJobID,
			).Scan(&beforeLease); err != nil {
				t.Fatalf("read lease before forged %s mutation: %v", mismatch.name, err)
			}
			if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{
				Claim: forged, OwnerLeaseDuration: 4 * time.Minute,
			}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
				t.Fatalf("RenewProcessorClaim(forged %s) error = %v, want ErrProcessorClaimLost", mismatch.name, err)
			}
			var afterLease time.Time
			if err := fixture.db.QueryRow(ctx, `
				select lease_expires_at from public.attachment_processor_jobs
				where processor_job_id = $1`, claim.ProcessorJobID,
			).Scan(&afterLease); err != nil {
				t.Fatalf("read lease after forged %s mutation: %v", mismatch.name, err)
			}
			if !afterLease.Equal(beforeLease) {
				t.Fatalf("forged %s renewal advanced lease %s -> %s", mismatch.name, beforeLease, afterLease)
			}

			forgedRegistration := registration
			forgedRegistration.Claim = forged
			forgedRegistration.WorkspaceID = fmt.Sprintf("cpw_claimfence%d", index)
			if _, err := repository.RegisterProcessorWorkspace(ctx, forgedRegistration); !errors.Is(err, attachments.ErrProcessorClaimLost) {
				t.Fatalf("RegisterProcessorWorkspace(forged %s) error = %v, want ErrProcessorClaimLost", mismatch.name, err)
			}
			var workspaceCount int64
			if err := fixture.db.QueryRow(ctx, `
				select count(*) from public.content_processor_workspaces where workspace_id = $1`,
				forgedRegistration.WorkspaceID,
			).Scan(&workspaceCount); err != nil {
				t.Fatalf("count forged %s workspace registration: %v", mismatch.name, err)
			}
			if workspaceCount != 0 {
				t.Fatalf("forged %s workspace registration inserted %d rows", mismatch.name, workspaceCount)
			}
		})
	}
	workspace, err := repository.RegisterProcessorWorkspace(ctx, registration)
	if err != nil {
		t.Fatalf("RegisterProcessorWorkspace() error = %v", err)
	}
	if workspace.WorkspaceID != registration.WorkspaceID || workspace.ProcessorJobID != claim.ProcessorJobID ||
		workspace.Attempt != claim.Attempt || workspace.State != attachments.ProcessorWorkspaceStateRegistered ||
		workspace.WorkspacePathDigest != pathDigest || !workspace.ExpiresAt.Equal(registration.ExpiresAt) {
		t.Fatalf("RegisterProcessorWorkspace() = %#v", workspace)
	}
	for _, mismatch := range claimMismatches {
		t.Run("workspace transition forged claim "+mismatch.name, func(t *testing.T) {
			forged := *claim
			mismatch.mutate(&forged)
			transition := attachments.ProcessorWorkspaceTransition{
				WorkspaceID: registration.WorkspaceID, WorkspacePathDigest: pathDigest,
				Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(forged),
			}
			if _, err := repository.MaterializeProcessorWorkspace(ctx, transition); !errors.Is(err, attachments.ErrProcessorClaimLost) {
				t.Fatalf("MaterializeProcessorWorkspace(forged %s) error = %v, want ErrProcessorClaimLost", mismatch.name, err)
			}
			if _, err := repository.BeginProcessorWorkspacePurge(ctx, transition); !errors.Is(err, attachments.ErrProcessorClaimLost) {
				t.Fatalf("BeginProcessorWorkspacePurge(forged %s) error = %v, want ErrProcessorClaimLost", mismatch.name, err)
			}
			var state string
			if err := fixture.db.QueryRow(ctx, `
				select workspace_state from public.content_processor_workspaces where workspace_id = $1`,
				registration.WorkspaceID).Scan(&state); err != nil {
				t.Fatalf("read workspace after forged %s transition: %v", mismatch.name, err)
			}
			if state != string(attachments.ProcessorWorkspaceStateRegistered) {
				t.Fatalf("forged %s transition changed workspace state to %q", mismatch.name, state)
			}
		})
	}
	var beforeXID, beforeUpdatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, updated_at::text
		from public.content_processor_workspaces where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&beforeXID, &beforeUpdatedAt); err != nil {
		t.Fatalf("read registered processor workspace: %v", err)
	}
	replay, err := repository.RegisterProcessorWorkspace(ctx, registration)
	if err != nil || replay != workspace {
		t.Fatalf("RegisterProcessorWorkspace(replay) = (%#v, %v), want %#v", replay, err, workspace)
	}
	var afterXID, afterUpdatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, updated_at::text
		from public.content_processor_workspaces where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&afterXID, &afterUpdatedAt); err != nil {
		t.Fatalf("read replayed processor workspace: %v", err)
	}
	if beforeXID != afterXID || beforeUpdatedAt != afterUpdatedAt {
		t.Fatalf("exact workspace replay mutated row xid/updated_at %q/%q -> %q/%q",
			beforeXID, beforeUpdatedAt, afterXID, afterUpdatedAt)
	}

	mismatches := []struct {
		name   string
		mutate func(*attachments.ProcessorWorkspaceRegistration)
	}{
		{name: "workspace ID", mutate: func(input *attachments.ProcessorWorkspaceRegistration) {
			input.WorkspaceID = "cpw_workspaceother"
		}},
		{name: "path digest", mutate: func(input *attachments.ProcessorWorkspaceRegistration) {
			input.WorkspacePathDigest = sha256.Sum256([]byte("other workspace path"))
		}},
		{name: "expiry", mutate: func(input *attachments.ProcessorWorkspaceRegistration) {
			input.ExpiresAt = input.ExpiresAt.Add(-time.Second)
		}},
	}
	for _, test := range mismatches {
		t.Run("mismatched replay "+test.name, func(t *testing.T) {
			input := registration
			test.mutate(&input)
			if _, err := repository.RegisterProcessorWorkspace(ctx, input); !errors.Is(err, attachments.ErrAttachmentConflict) {
				t.Fatalf("RegisterProcessorWorkspace(mismatched %s) error = %v, want ErrAttachmentConflict", test.name, err)
			}
		})
	}
	invalidExpiry := registration
	invalidExpiry.WorkspaceID = "cpw_workspaceexpired"
	invalidExpiry.ExpiresAt = claim.ExpiresAt.Add(time.Second)
	if _, err := repository.RegisterProcessorWorkspace(ctx, invalidExpiry); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("RegisterProcessorWorkspace(unbounded expiry) error = %v, want ErrInvalidProcessorCommand", err)
	}

	renewed, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{
		Claim: *claim, OwnerLeaseDuration: 4 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RenewProcessorClaim() error = %v", err)
	}
	if renewed.Attempt != claim.Attempt || renewed.OwnerGeneration != claim.OwnerGeneration ||
		renewed.OwnerID != claim.OwnerID || !renewed.LeaseExpiresAt.After(claim.LeaseExpiresAt) ||
		renewed.LeaseExpiresAt.After(renewed.ExpiresAt) {
		t.Fatalf("RenewProcessorClaim() = %#v, want expiry-only renewal from %#v", renewed, claim)
	}
	if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{
		Claim: *claim, OwnerLeaseDuration: time.Minute,
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("RenewProcessorClaim(pre-renew token) error = %v, want ErrProcessorClaimLost", err)
	}
	if _, err := repository.RegisterProcessorWorkspace(ctx, registration); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("RegisterProcessorWorkspace(pre-renew token replay) error = %v, want ErrProcessorClaimLost", err)
	}
	staleResult := attachments.ProcessorResult{
		Source: renewed.Source, Profile: renewed.Profile,
		Code: attachments.ProcessorResultCodeScannerUnavailable,
	}
	if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
		Claim: *claim, Result: staleResult, RetryAt: now.Add(10 * time.Minute), Limits: attachments.DefaultLimits(),
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("CompleteProcessorJob(pre-renew token) error = %v, want ErrProcessorClaimLost", err)
	}

	wrongOwner := renewed
	wrongOwner.OwnerID = "processor_workspace_wrong"
	if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{
		Claim: wrongOwner, OwnerLeaseDuration: time.Minute,
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("RenewProcessorClaim(stale owner) error = %v, want ErrProcessorClaimLost", err)
	}
	wrongGeneration := renewed
	wrongGeneration.OwnerGeneration++
	if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{
		Claim: wrongGeneration, OwnerLeaseDuration: time.Minute,
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("RenewProcessorClaim(stale generation) error = %v, want ErrProcessorClaimLost", err)
	}
	wrongAttempt := renewed
	wrongAttempt.Attempt++
	if _, err := repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{
		Claim: wrongAttempt, OwnerLeaseDuration: time.Minute,
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("RenewProcessorClaim(stale attempt) error = %v, want ErrProcessorClaimLost", err)
	}

	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set lease_expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, renewed.ProcessorJobID); err != nil {
		t.Fatalf("expire attachment processor lease: %v", err)
	}
	if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
		Claim: renewed, Result: staleResult, RetryAt: now.Add(10 * time.Minute), Limits: attachments.DefaultLimits(),
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("CompleteProcessorJob(expired lease) error = %v, want ErrProcessorClaimLost", err)
	}
	takeover, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_b", OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil || takeover == nil {
		t.Fatalf("ClaimProcessorJob(takeover) = (%#v, %v)", takeover, err)
	}
	if takeover.ProcessorJobID != renewed.ProcessorJobID || takeover.Attempt != renewed.Attempt+1 ||
		takeover.OwnerGeneration != renewed.OwnerGeneration+1 || takeover.OwnerID != "processor_workspace_b" {
		t.Fatalf("ClaimProcessorJob(takeover) = %#v, prior %#v", takeover, renewed)
	}
	if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
		Claim: renewed, Result: staleResult, RetryAt: now.Add(10 * time.Minute), Limits: attachments.DefaultLimits(),
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("CompleteProcessorJob(takeover-stale token) error = %v, want ErrProcessorClaimLost", err)
	}
	var workspaceCount int64
	if err := fixture.db.QueryRow(ctx, `
		select count(*) from public.content_processor_workspaces where processor_job_id = $1`, claim.ProcessorJobID,
	).Scan(&workspaceCount); err != nil {
		t.Fatalf("count processor workspaces: %v", err)
	}
	if workspaceCount != 1 {
		t.Fatalf("processor workspace count = %d, want one exact registration", workspaceCount)
	}
}

func TestPostgresIntegrationAttachmentProcessorWorkspaceMaterializeAndPurgeReceipt(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace-purge", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "workspacepurge", Source: attachmentProcessorBlob(0x39, 39, "workspace-purge-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: 39,
	})
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_purge", OwnerLeaseDuration: 100 * time.Millisecond,
	})
	if err != nil || claim == nil || claim.ProcessorJobID != seed.ProcessorJobID {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v)", claim, err)
	}
	pathDigest := sha256.Sum256([]byte("private derived purge workspace"))
	registration := attachments.ProcessorWorkspaceRegistration{
		Claim: *claim, WorkspaceID: "cpw_workspacepurge1",
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.LeaseExpiresAt,
	}
	if _, err := repository.RegisterProcessorWorkspace(ctx, registration); err != nil {
		t.Fatalf("RegisterProcessorWorkspace() error = %v", err)
	}
	transition := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: registration.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(*claim),
	}
	materialized, err := repository.MaterializeProcessorWorkspace(ctx, transition)
	if err != nil || materialized.State != attachments.ProcessorWorkspaceStateMaterialized {
		t.Fatalf("MaterializeProcessorWorkspace() = (%#v, %v)", materialized, err)
	}
	var materializedXID, materializedUpdatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, updated_at::text from public.content_processor_workspaces
		where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&materializedXID, &materializedUpdatedAt); err != nil {
		t.Fatalf("read materialized workspace identity: %v", err)
	}
	materializedReplay, err := repository.MaterializeProcessorWorkspace(ctx, transition)
	if err != nil || materializedReplay != materialized {
		t.Fatalf("MaterializeProcessorWorkspace(replay) = (%#v, %v), want %#v", materializedReplay, err, materialized)
	}
	var replayXID, replayUpdatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, updated_at::text from public.content_processor_workspaces
		where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&replayXID, &replayUpdatedAt); err != nil {
		t.Fatalf("read replayed materialized workspace identity: %v", err)
	}
	if replayXID != materializedXID || replayUpdatedAt != materializedUpdatedAt {
		t.Fatalf("materialize replay mutated row %q/%q -> %q/%q",
			materializedXID, materializedUpdatedAt, replayXID, replayUpdatedAt)
	}

	time.Sleep(150 * time.Millisecond)
	plan, err := repository.BeginProcessorWorkspacePurge(ctx, transition)
	if err != nil || plan.Workspace.State != attachments.ProcessorWorkspaceStatePurging || plan.Receipt != nil {
		t.Fatalf("BeginProcessorWorkspacePurge() = (%#v, %v)", plan, err)
	}
	receipt, err := attachments.NewProcessorWorkspacePurgeReceipt(registration.WorkspaceID, 2, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("NewProcessorWorkspacePurgeReceipt() error = %v", err)
	}
	completed, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: transition, Receipt: receipt,
	})
	if err != nil || completed != receipt {
		t.Fatalf("CompleteProcessorWorkspacePurge() = (%#v, %v), want %#v", completed, err, receipt)
	}
	var receiptXID, receiptCreatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, created_at::text from public.content_workspace_purge_receipts
		where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&receiptXID, &receiptCreatedAt); err != nil {
		t.Fatalf("read purge receipt identity: %v", err)
	}
	replayPlan, err := repository.BeginProcessorWorkspacePurge(ctx, transition)
	if err != nil || replayPlan.Workspace.State != attachments.ProcessorWorkspaceStatePurged ||
		replayPlan.Receipt == nil || *replayPlan.Receipt != receipt {
		t.Fatalf("BeginProcessorWorkspacePurge(replay) = (%#v, %v), want receipt %#v", replayPlan, err, receipt)
	}
	completedReplay, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: transition, Receipt: receipt,
	})
	if err != nil || completedReplay != receipt {
		t.Fatalf("CompleteProcessorWorkspacePurge(replay) = (%#v, %v), want %#v", completedReplay, err, receipt)
	}
	var replayReceiptXID, replayReceiptCreatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, created_at::text from public.content_workspace_purge_receipts
		where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&replayReceiptXID, &replayReceiptCreatedAt); err != nil {
		t.Fatalf("read replayed purge receipt identity: %v", err)
	}
	if replayReceiptXID != receiptXID || replayReceiptCreatedAt != receiptCreatedAt {
		t.Fatalf("purge receipt replay mutated row %q/%q -> %q/%q",
			receiptXID, receiptCreatedAt, replayReceiptXID, replayReceiptCreatedAt)
	}
	liveReconciliation := transition
	liveReconciliation.Authorization = attachments.NewProcessorWorkspaceReconciliationAuthorization()
	reconciliationReplay, err := repository.BeginProcessorWorkspacePurge(ctx, liveReconciliation)
	if err != nil || reconciliationReplay.Receipt == nil || *reconciliationReplay.Receipt != receipt {
		t.Fatalf("BeginProcessorWorkspacePurge(expired-lease reconciliation replay) = (%#v, %v), want receipt %#v", reconciliationReplay, err, receipt)
	}
	if _, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: liveReconciliation, Receipt: receipt,
	}); err != nil {
		t.Fatalf("CompleteProcessorWorkspacePurge(expired-lease reconciliation replay) error = %v", err)
	}
	mismatched, err := attachments.NewProcessorWorkspacePurgeReceipt(registration.WorkspaceID, 3, receipt.VerifiedAbsentAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: transition, Receipt: mismatched,
	}); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteProcessorWorkspacePurge(mismatch) error = %v, want ErrAttachmentConflict", err)
	}
	wrongPath := transition
	wrongPath.WorkspacePathDigest[0] ^= 0xff
	if _, err := repository.BeginProcessorWorkspacePurge(ctx, wrongPath); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("BeginProcessorWorkspacePurge(path drift) error = %v, want ErrProcessorClaimLost", err)
	}

	if _, err := fixture.db.Exec(ctx, `
		delete from public.content_processor_workspaces where workspace_id = $1`, registration.WorkspaceID,
	); err != nil {
		t.Fatalf("delete terminal processor workspace row: %v", err)
	}
	receiptOnlyTransition := transition
	receiptOnlyTransition.Authorization = attachments.NewProcessorWorkspaceReconciliationAuthorization()
	receiptOnlyPlan, err := repository.BeginProcessorWorkspacePurge(ctx, receiptOnlyTransition)
	if err != nil || receiptOnlyPlan.Workspace != (attachments.ProcessorWorkspace{}) ||
		receiptOnlyPlan.Receipt == nil || *receiptOnlyPlan.Receipt != receipt {
		t.Fatalf("BeginProcessorWorkspacePurge(receipt-only replay) = (%#v, %v), want immutable %#v",
			receiptOnlyPlan, err, receipt)
	}
	var receiptOnlyXID, receiptOnlyCreatedAt string
	if err := fixture.db.QueryRow(ctx, `
		select xmin::text, created_at::text from public.content_workspace_purge_receipts
		where workspace_id = $1`, registration.WorkspaceID,
	).Scan(&receiptOnlyXID, &receiptOnlyCreatedAt); err != nil {
		t.Fatalf("read receipt-only replay identity: %v", err)
	}
	if receiptOnlyXID != receiptXID || receiptOnlyCreatedAt != receiptCreatedAt {
		t.Fatalf("receipt-only replay mutated row %q/%q -> %q/%q",
			receiptXID, receiptCreatedAt, receiptOnlyXID, receiptOnlyCreatedAt)
	}

	tracingPathDigest := sha256.Sum256([]byte("concurrent processor workspace purge path"))
	tracingWorkspaceID := "cpw_workspacepurgerace1"
	if _, err := fixture.db.Exec(ctx, `
		insert into public.content_processor_workspaces (
			workspace_id, processor_job_id, attempt, workspace_state,
			workspace_path_digest, created_at, updated_at, expires_at
		) values ($1, $2, $3, 'purging', $4, $5, $5, $6)`,
		tracingWorkspaceID, claim.ProcessorJobID, claim.Attempt,
		tracingPathDigest[:], now, claim.ExpiresAt,
	); err != nil {
		t.Fatalf("seed concurrent purging workspace: %v", err)
	}
	tracingTransition := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: tracingWorkspaceID, WorkspacePathDigest: tracingPathDigest,
		Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(*claim),
	}
	firstCandidate, err := attachments.NewProcessorWorkspacePurgeReceipt(
		tracingWorkspaceID, 1, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	secondCandidate, err := attachments.NewProcessorWorkspacePurgeReceipt(
		tracingWorkspaceID, 2, now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	type concurrentPurgeResult struct {
		receipt attachments.ProcessorWorkspacePurgeReceipt
		err     error
	}
	start := make(chan struct{})
	results := make(chan concurrentPurgeResult, 2)
	for _, candidate := range []attachments.ProcessorWorkspacePurgeReceipt{firstCandidate, secondCandidate} {
		candidate := candidate
		go func() {
			<-start
			completed, completeErr := repository.CompleteProcessorWorkspacePurge(
				ctx,
				attachments.ProcessorWorkspacePurgeCompletion{
					Workspace: tracingTransition, Receipt: candidate,
				},
			)
			results <- concurrentPurgeResult{receipt: completed, err: completeErr}
		}()
	}
	close(start)
	var winner attachments.ProcessorWorkspacePurgeReceipt
	successes, conflicts := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result.receipt
		case errors.Is(result.err, attachments.ErrAttachmentConflict):
			conflicts++
		default:
			t.Fatalf("concurrent CompleteProcessorWorkspacePurge() error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 || (winner != firstCandidate && winner != secondCandidate) {
		t.Fatalf("concurrent purge results successes=%d conflicts=%d winner=%#v", successes, conflicts, winner)
	}
	converged, err := repository.BeginProcessorWorkspacePurge(ctx, tracingTransition)
	if err != nil || converged.Receipt == nil || *converged.Receipt != winner {
		t.Fatalf("BeginProcessorWorkspacePurge(concurrent loser replay) = (%#v, %v), want %#v",
			converged, err, winner)
	}
	var storedReceiptDigest []byte
	if err := fixture.db.QueryRow(ctx, `
		select receipt_digest from public.content_workspace_purge_receipts where workspace_id = $1`,
		tracingWorkspaceID,
	).Scan(&storedReceiptDigest); err != nil {
		t.Fatalf("read concurrent winning receipt: %v", err)
	}
	if !bytes.Equal(storedReceiptDigest, winner.ReceiptDigest[:]) {
		t.Fatalf("stored concurrent receipt digest = %x, want immutable winner %x",
			storedReceiptDigest, winner.ReceiptDigest)
	}
}

func TestPostgresIntegrationAttachmentProcessorWorkspaceRejectsStaleTakeover(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace-stale", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "workspacestaletakeover", Source: attachmentProcessorBlob(0x41, 41, "workspace-stale-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: 41,
	})
	claimA, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_stale_a", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claimA == nil {
		t.Fatalf("ClaimProcessorJob(worker A) = (%#v, %v)", claimA, err)
	}
	pathDigest := sha256.Sum256([]byte("stale takeover workspace path"))
	registration := attachments.ProcessorWorkspaceRegistration{
		Claim: *claimA, WorkspaceID: "cpw_workspacestalestale1",
		WorkspacePathDigest: pathDigest, ExpiresAt: claimA.LeaseExpiresAt,
	}
	if _, err := repository.RegisterProcessorWorkspace(ctx, registration); err != nil {
		t.Fatalf("RegisterProcessorWorkspace(worker A) error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set lease_expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, claimA.ProcessorJobID); err != nil {
		t.Fatalf("expire worker A lease: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.content_processor_workspaces
		set created_at = transaction_timestamp() - interval '2 seconds',
		    expires_at = transaction_timestamp() - interval '1 second'
		where workspace_id = $1`, registration.WorkspaceID); err != nil {
		t.Fatalf("expire worker A workspace: %v", err)
	}
	claimB, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_stale_b", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claimB == nil || claimB.OwnerGeneration <= claimA.OwnerGeneration || claimB.Attempt <= claimA.Attempt {
		t.Fatalf("ClaimProcessorJob(worker B) = (%#v, %v), want takeover", claimB, err)
	}
	transitionA := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: registration.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(*claimA),
	}
	if _, err := repository.MaterializeProcessorWorkspace(ctx, transitionA); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("MaterializeProcessorWorkspace(stale worker A) error = %v, want ErrProcessorClaimLost", err)
	}
	var state string
	if err := fixture.db.QueryRow(ctx, `
		select workspace_state from public.content_processor_workspaces where workspace_id = $1`,
		registration.WorkspaceID).Scan(&state); err != nil {
		t.Fatalf("read workspace state after stale materialize: %v", err)
	}
	if state != string(attachments.ProcessorWorkspaceStateRegistered) {
		t.Fatalf("stale materialize changed workspace state to %q", state)
	}
	if _, err := repository.BeginProcessorWorkspacePurge(ctx, transitionA); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("BeginProcessorWorkspacePurge(stale worker A) error = %v, want ErrProcessorClaimLost", err)
	}
	if err := fixture.db.QueryRow(ctx, `
		select workspace_state from public.content_processor_workspaces where workspace_id = $1`,
		registration.WorkspaceID).Scan(&state); err != nil {
		t.Fatalf("read workspace state after stale purge: %v", err)
	}
	if state != string(attachments.ProcessorWorkspaceStateRegistered) {
		t.Fatalf("stale purge changed workspace state to %q", state)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.content_processor_workspaces
		set workspace_state = 'purging'
		where workspace_id = $1`, registration.WorkspaceID); err != nil {
		t.Fatalf("seed purging workspace for stale completion: %v", err)
	}
	staleReceipt, err := attachments.NewProcessorWorkspacePurgeReceipt(
		registration.WorkspaceID, 1, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewProcessorWorkspacePurgeReceipt(stale completion) error = %v", err)
	}
	if _, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: transitionA, Receipt: staleReceipt,
	}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
		t.Fatalf("CompleteProcessorWorkspacePurge(stale worker A) error = %v, want ErrProcessorClaimLost", err)
	}
	var receiptCount int64
	if err := fixture.db.QueryRow(ctx, `
		select count(*) from public.content_workspace_purge_receipts where workspace_id = $1`,
		registration.WorkspaceID).Scan(&receiptCount); err != nil {
		t.Fatalf("count stale completion receipts: %v", err)
	}
	if receiptCount != 0 {
		t.Fatalf("stale completion inserted %d purge receipts", receiptCount)
	}
	pathDigestB := sha256.Sum256([]byte("live takeover workspace path"))
	registrationB := attachments.ProcessorWorkspaceRegistration{
		Claim: *claimB, WorkspaceID: "cpw_workspacestalestakeover2",
		WorkspacePathDigest: pathDigestB, ExpiresAt: claimB.LeaseExpiresAt,
	}
	if _, err := repository.RegisterProcessorWorkspace(ctx, registrationB); err != nil {
		t.Fatalf("RegisterProcessorWorkspace(worker B) error = %v", err)
	}
	// Reconciliation may clean worker A's expired workspace after takeover, but
	// it must not touch worker B's live workspace under the same job.
	reconciliation := transitionA
	reconciliation.Authorization = attachments.NewProcessorWorkspaceReconciliationAuthorization()
	plan, err := repository.BeginProcessorWorkspacePurge(ctx, reconciliation)
	if err != nil || plan.Workspace.State != attachments.ProcessorWorkspaceStatePurging || plan.Receipt != nil {
		t.Fatalf("BeginProcessorWorkspacePurge(expired takeover reconciliation) = (%#v, %v), want purging plan", plan, err)
	}
	reconciliationReceipt, err := attachments.NewProcessorWorkspacePurgeReceipt(
		registration.WorkspaceID, 0, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewProcessorWorkspacePurgeReceipt(expired takeover) error = %v", err)
	}
	if _, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: reconciliation, Receipt: reconciliationReceipt,
	}); err != nil {
		t.Fatalf("CompleteProcessorWorkspacePurge(expired takeover) error = %v", err)
	}
	transitionB := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: registrationB.WorkspaceID, WorkspacePathDigest: pathDigestB,
		Authorization: attachments.NewProcessorWorkspaceReconciliationAuthorization(),
	}
	if _, err := repository.BeginProcessorWorkspacePurge(ctx, transitionB); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("BeginProcessorWorkspacePurge(live takeover reconciliation) error = %v, want ErrAttachmentConflict", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, claimB.ProcessorJobID); err != nil {
		t.Fatalf("expire processor job overall deadline: %v", err)
	}
	plan, err = repository.BeginProcessorWorkspacePurge(ctx, transitionB)
	if err != nil || plan.Workspace.State != attachments.ProcessorWorkspaceStatePurging || plan.Receipt != nil {
		t.Fatalf("BeginProcessorWorkspacePurge(expired reconciliation) = (%#v, %v), want purging plan", plan, err)
	}
	_ = seed
}

func TestPostgresIntegrationAttachmentProcessorWorkspaceMaterializeAndLiveReconciliationFenceConcurrently(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	materializeRepository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace-materialize-race", 1),
	)
	purgeRepository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace-purge-race", 1),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "workspaceconcurrentfence", Source: attachmentProcessorBlob(0x42, 42, "workspace-concurrent-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: 42,
	})
	claim, err := materializeRepository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_concurrent", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v)", claim, err)
	}
	pathDigest := sha256.Sum256([]byte("concurrent materialize and purge path"))
	registration := attachments.ProcessorWorkspaceRegistration{
		Claim: *claim, WorkspaceID: "cpw_workspaceconcurrent1",
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.LeaseExpiresAt,
	}
	if _, err := materializeRepository.RegisterProcessorWorkspace(ctx, registration); err != nil {
		t.Fatalf("RegisterProcessorWorkspace() error = %v", err)
	}
	workerTransition := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: registration.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(*claim),
	}
	reconciliationTransition := workerTransition
	reconciliationTransition.Authorization = attachments.NewProcessorWorkspaceReconciliationAuthorization()
	start := make(chan struct{})
	type materializeResult struct {
		workspace attachments.ProcessorWorkspace
		err       error
	}
	type purgeResult struct {
		plan attachments.ProcessorWorkspacePurgePlan
		err  error
	}
	materializedResults := make(chan materializeResult, 1)
	purgeResults := make(chan purgeResult, 1)
	go func() {
		<-start
		workspace, err := materializeRepository.MaterializeProcessorWorkspace(ctx, workerTransition)
		materializedResults <- materializeResult{workspace: workspace, err: err}
	}()
	go func() {
		<-start
		plan, err := purgeRepository.BeginProcessorWorkspacePurge(ctx, reconciliationTransition)
		purgeResults <- purgeResult{plan: plan, err: err}
	}()
	close(start)
	materialized := <-materializedResults
	purged := <-purgeResults
	if materialized.err != nil || materialized.workspace.State != attachments.ProcessorWorkspaceStateMaterialized {
		t.Fatalf("concurrent materialization = (%#v, %v)", materialized.workspace, materialized.err)
	}
	if !errors.Is(purged.err, attachments.ErrAttachmentConflict) {
		t.Fatalf("concurrent live reconciliation purge = (%#v, %v), want ErrAttachmentConflict", purged.plan, purged.err)
	}
	var state string
	if err := fixture.db.QueryRow(ctx, `
		select workspace_state from public.content_processor_workspaces where workspace_id = $1`,
		registration.WorkspaceID).Scan(&state); err != nil {
		t.Fatalf("read concurrent workspace state: %v", err)
	}
	if state != string(attachments.ProcessorWorkspaceStateMaterialized) {
		t.Fatalf("concurrent reconciliation changed workspace state to %q", state)
	}
	_ = seed
}

func TestPostgresIntegrationAttachmentProcessorWorkspaceReconciliationCleansExpiredClaimLease(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace-expired-lease", 1),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "workspaceexpiredlease", Source: attachmentProcessorBlob(0x43, 43, "workspace-expired-lease-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: 43,
	})
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_expired", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v)", claim, err)
	}
	pathDigest := sha256.Sum256([]byte("expired claim lease workspace path"))
	registration := attachments.ProcessorWorkspaceRegistration{
		Claim: *claim, WorkspaceID: "cpw_workspaceexpiredlease1",
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.ExpiresAt,
	}
	if _, err := repository.RegisterProcessorWorkspace(ctx, registration); err != nil {
		t.Fatalf("RegisterProcessorWorkspace() error = %v", err)
	}
	reconciliation := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: registration.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: attachments.NewProcessorWorkspaceReconciliationAuthorization(),
	}
	if _, err := repository.BeginProcessorWorkspacePurge(ctx, reconciliation); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("BeginProcessorWorkspacePurge(live lease) error = %v, want ErrAttachmentConflict", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set lease_expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, claim.ProcessorJobID); err != nil {
		t.Fatalf("expire claim lease: %v", err)
	}
	plan, err := repository.BeginProcessorWorkspacePurge(ctx, reconciliation)
	if err != nil || plan.Workspace.State != attachments.ProcessorWorkspaceStatePurging || plan.Receipt != nil {
		t.Fatalf("BeginProcessorWorkspacePurge(expired claim lease) = (%#v, %v), want purging plan", plan, err)
	}
	receipt, err := attachments.NewProcessorWorkspacePurgeReceipt(registration.WorkspaceID, 0, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("NewProcessorWorkspacePurgeReceipt() error = %v", err)
	}
	if _, err := repository.CompleteProcessorWorkspacePurge(ctx, attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: reconciliation, Receipt: receipt,
	}); err != nil {
		t.Fatalf("CompleteProcessorWorkspacePurge() error = %v", err)
	}
	_ = seed
}

func TestPostgresIntegrationAttachmentProcessorWorkspaceCleanupClaimSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-workspace-cleanup-restart", 1),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "workspacecleanuprestart", Source: attachmentProcessorBlob(0x44, 44, "workspace-cleanup-restart-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: 44,
	})
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_workspace_cleanup", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v)", claim, err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "processor-root")
	workspaceID := "cpw_workspacecleanuprestart1"
	workspacePath := filepath.Join(workspaceRoot, workspaceID)
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		t.Fatalf("create workspace residue: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "source.bin"), []byte("crash residue"), 0o600); err != nil {
		t.Fatalf("write workspace residue: %v", err)
	}
	pathDigest := sha256.Sum256([]byte(workspacePath))
	registration := attachments.ProcessorWorkspaceRegistration{
		Claim: *claim, WorkspaceID: workspaceID,
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.ExpiresAt,
	}
	if _, err := repository.RegisterProcessorWorkspace(ctx, registration); err != nil {
		t.Fatalf("RegisterProcessorWorkspace() error = %v", err)
	}
	cleanupInput := attachments.ProcessorWorkspaceCleanupClaimInput{
		ProjectID: "default", RetryDelay: time.Microsecond,
	}
	if candidate, err := repository.ClaimProcessorWorkspaceCleanup(ctx, cleanupInput); err != nil || candidate != nil {
		t.Fatalf("ClaimProcessorWorkspaceCleanup(live lease) = (%#v, %v), want no claim", candidate, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set lease_expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, claim.ProcessorJobID); err != nil {
		t.Fatalf("expire cleanup candidate lease: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.content_processor_workspaces
		set updated_at = transaction_timestamp() - interval '1 second'
		where workspace_id = $1`, workspaceID); err != nil {
		t.Fatalf("age cleanup candidate workspace: %v", err)
	}
	candidate, err := repository.ClaimProcessorWorkspaceCleanup(ctx, cleanupInput)
	if err != nil || candidate == nil || candidate.WorkspaceID != workspaceID ||
		candidate.WorkspacePathDigest != pathDigest {
		t.Fatalf("ClaimProcessorWorkspaceCleanup(expired lease) = (%#v, %v)", candidate, err)
	}
	var state string
	if err := fixture.db.QueryRow(ctx, `
		select workspace_state from public.content_processor_workspaces where workspace_id = $1`,
		workspaceID).Scan(&state); err != nil {
		t.Fatalf("read claimed cleanup state: %v", err)
	}
	if state != string(attachments.ProcessorWorkspaceStatePurging) {
		t.Fatalf("claimed cleanup state = %q, want purging", state)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.content_processor_workspaces
		set updated_at = transaction_timestamp() - interval '1 second'
		where workspace_id = $1`, workspaceID); err != nil {
		t.Fatalf("age crash-restart cleanup claim: %v", err)
	}
	reconciler, err := attachments.NewProcessorWorkspaceReconciler(
		repository,
		attachments.ProcessorWorkspaceReconcilerConfig{
			Root: workspaceRoot, CleanupTimeout: 5 * time.Second, RetryDelay: time.Microsecond,
		},
	)
	if err != nil {
		t.Fatalf("NewProcessorWorkspaceReconciler() error = %v", err)
	}
	claimed, err := reconciler.RunOnce(ctx)
	if err != nil || !claimed {
		t.Fatalf("RunOnce(restart) = (%t, %v), want claimed success", claimed, err)
	}
	if _, err := os.Lstat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace residue after restart reconciliation = %v", err)
	}
	var receiptCount int64
	if err := fixture.db.QueryRow(ctx, `
		select workspace.workspace_state,
		       (select count(*) from public.content_workspace_purge_receipts as receipt
		        where receipt.workspace_id = workspace.workspace_id)
		from public.content_processor_workspaces as workspace
		where workspace.workspace_id = $1`, workspaceID).Scan(&state, &receiptCount); err != nil {
		t.Fatalf("read restart reconciliation result: %v", err)
	}
	if state != string(attachments.ProcessorWorkspaceStatePurged) || receiptCount != 1 {
		t.Fatalf("restart reconciliation state/receipts = %q/%d, want purged/1", state, receiptCount)
	}
	claimed, err = reconciler.RunOnce(ctx)
	if err != nil || claimed {
		t.Fatalf("RunOnce(replay) = (%t, %v), want empty success", claimed, err)
	}
	_ = seed
}

func TestPostgresMinIOIntegrationAttachmentProcessorS3WorkspaceWorkflow(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set PostgreSQL and MinIO integration environments to run the processor S3 workspace workflow")
	}
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-s3-workspace", 2),
	)
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	content := []byte("real PostgreSQL MinIO processor workspace\n")
	digest := sha256.Sum256(content)
	temporaryDigest := sha256.Sum256([]byte(t.Name()))
	sourceVersion, err := blob.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
		TemporaryKey: fmt.Sprintf("temporary/%x", temporaryDigest[:]),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("S3BlobStore.Put(source) error = %v", err)
	}
	source := attachments.BlobObject{
		Key: sourceVersion.Key, ObjectVersion: sourceVersion.VersionID,
		SHA256: sourceVersion.SHA256, SizeBytes: sourceVersion.SizeBytes,
		BackendKind: attachments.BackendKindS3,
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "s3workspaceworkflow", Source: source,
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
	})

	limits := attachments.DefaultLimits()
	admissionLimits := attachments.DefaultAdmissionLimits(limits)
	preview, err := attachments.NewPreviewProcessor(attachments.PreviewConfig{
		MaxSourceBytes: limits.MaxFileBytes, MaxOutputBytes: limits.MaxInlineTextPreviewBytes,
		MaxImagePixels: admissionLimits.MaxImagePixels,
		PDFInfoBinary:  "/usr/bin/pdfinfo", PDFToPPMBinary: "/usr/bin/pdftoppm",
	})
	if err != nil {
		t.Fatalf("NewPreviewProcessor() error = %v", err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "processor-root")
	workspace, err := attachments.NewContentProcessorWorkspace(
		attachments.ContentProcessorWorkspaceConfig{
			Root: workspaceRoot, MaxSourceBytes: limits.MaxFileBytes, CleanupTimeout: 5 * time.Second,
		},
		repository,
		preview,
	)
	if err != nil {
		t.Fatalf("NewContentProcessorWorkspace() error = %v", err)
	}
	worker, err := attachments.NewProcessorWorker(repository, blob, workspace, attachments.ProcessorWorkerConfig{
		OwnerID: "processor_s3_workspace", OwnerLeaseDuration: time.Minute,
		Limits: limits, AdmissionLimits: admissionLimits,
		PreviewBackendKind: attachments.BackendKindS3,
	})
	if err != nil {
		t.Fatalf("NewProcessorWorker() error = %v", err)
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("ProcessorWorker.RunOnce() error = %v", err)
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("ProcessorWorker.RunOnce(replay) error = %v", err)
	}

	var processorState attachments.ProcessorState
	var uploadState, attachmentState attachments.UploadState
	var previewKey, previewVersion, previewMediaType string
	var previewSizeBytes int64
	var workspaceCount, receiptCount, reservedBytes, physicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select job.processor_state, upload.upload_state, attachment.attachment_state,
		       attachment.preview_blob_key, attachment.preview_blob_object_version,
		       attachment.preview_media_type, attachment.preview_size_bytes,
		       (select count(*) from public.content_processor_workspaces
		        where processor_job_id = job.processor_job_id),
		       (select count(*) from public.content_workspace_purge_receipts as receipt
		        join public.content_processor_workspaces as workspace
		          on workspace.workspace_id = receipt.workspace_id
		        where workspace.processor_job_id = job.processor_job_id),
		       quota.reserved_bytes, quota.physical_bytes
		from public.attachment_processor_jobs as job
		join public.attachment_uploads as upload on upload.upload_id = job.upload_id
		join public.record_attachments as attachment on attachment.attachment_id = job.attachment_id
		join public.attachment_quota_accounts as quota on quota.project_id = attachment.project_id
		where job.processor_job_id = $1`, seed.ProcessorJobID,
	).Scan(
		&processorState, &uploadState, &attachmentState,
		&previewKey, &previewVersion, &previewMediaType, &previewSizeBytes,
		&workspaceCount, &receiptCount, &reservedBytes, &physicalBytes,
	); err != nil {
		t.Fatalf("read processor S3 workspace result: %v", err)
	}
	if processorState != attachments.ProcessorStateSucceeded ||
		uploadState != attachments.UploadStateAvailable ||
		attachmentState != attachments.UploadStateAvailable {
		t.Fatalf("processor S3 workspace states = %q/%q/%q", processorState, uploadState, attachmentState)
	}
	if previewKey != source.Key || previewVersion != source.ObjectVersion ||
		previewMediaType != attachments.ManagedPreviewMediaTypeTextUTF8 || previewSizeBytes != int64(len(content)) {
		t.Fatalf("processor S3 workspace preview = %q/%q/%q/%d, source %#v",
			previewKey, previewVersion, previewMediaType, previewSizeBytes, source)
	}
	if workspaceCount != 1 || receiptCount != 1 || reservedBytes != 0 || physicalBytes != int64(len(content)) {
		t.Fatalf("processor S3 workspace durable counts = workspaces %d receipts %d reserved %d physical %d",
			workspaceCount, receiptCount, reservedBytes, physicalBytes)
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatalf("read processor workspace root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("processor workspace residue = %#v", entries)
	}
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix: "temporary/", Recursive: true, WithVersions: true,
	}) {
		if object.Err != nil {
			t.Fatalf("ListObjects(temporary residue) error = %v", object.Err)
		}
		t.Fatalf("processor S3 temporary residue = key %q version %q", object.Key, object.VersionID)
	}
	if _, err := blob.Stat(ctx, sourceVersion); err != nil {
		t.Fatalf("S3BlobStore.Stat(source after processor) error = %v", err)
	}
}

func TestPostgresIntegrationAttachmentProcessorCleanCompletionIsAtomicReplaySafeAndPhysicallyDeduplicated(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-clean", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	source := attachmentProcessorBlob(0x41, 41, "clean-source-v1")
	preview := attachmentProcessorBlob(0x42, 17, "clean-preview-v1")
	source.BackendKind = attachments.BackendKindS3
	preview.BackendKind = attachments.BackendKindS3
	firstSeed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "cleanfirst", Source: source, State: attachments.ProcessorStateQueued,
		Profile: attachments.ProcessorProfileText, CreatedAt: now.Add(-2 * time.Minute),
		ExpiresAt: now.Add(time.Hour), MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
	})
	secondSeed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "cleansecond", Source: source, State: attachments.ProcessorStateQueued,
		Profile: attachments.ProcessorProfileText, CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour), MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
	})
	processorResult := attachments.ProcessorResult{
		Source: source, Profile: attachments.ProcessorProfileText,
		Code: attachments.ProcessorResultCodeClean, HasPreview: true,
		Preview: attachments.ManagedPreview{
			Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8,
		},
	}
	resultDigest, err := processorResult.Digest()
	if err != nil {
		t.Fatalf("ProcessorResult.Digest() error = %v", err)
	}

	firstClaim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_clean_first", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil || firstClaim == nil || firstClaim.ProcessorJobID != firstSeed.ProcessorJobID {
		t.Fatalf("ClaimProcessorJob(first clean) = (%#v, %v)", firstClaim, err)
	}
	completionInput := attachments.ProcessorCompletionInput{
		Claim: *firstClaim, Result: processorResult, Limits: attachments.DefaultLimits(),
	}
	completionInput.PreviewPublicationIntent = preparePublishedAttachmentPreviewIntent(
		t, ctx, repository, *firstClaim, preview,
	)
	completed, err := repository.CompleteProcessorJob(ctx, completionInput)
	if err != nil {
		t.Fatalf("CompleteProcessorJob(first clean) error = %v", err)
	}
	if completed.ProcessorJobID != firstSeed.ProcessorJobID || completed.UploadID != firstSeed.UploadID ||
		completed.AttachmentID != firstSeed.AttachmentID || completed.ProcessorState != attachments.ProcessorStateSucceeded ||
		completed.UploadState != attachments.UploadStateAvailable || completed.AttachmentState != attachments.UploadStateAvailable ||
		completed.ResultCode != attachments.ProcessorResultCodeClean || completed.ResultDigest != resultDigest ||
		completed.Quota.Usage != (attachments.QuotaUsage{
			LogicalBytes: source.SizeBytes, ReservedBytes: source.SizeBytes,
			PhysicalBytes: source.SizeBytes + preview.SizeBytes,
		}) {
		t.Fatalf("CompleteProcessorJob(first clean) = %#v", completed)
	}
	assertAttachmentProcessorCleanRows(t, ctx, fixture, firstSeed, *firstClaim, source, preview, resultDigest)

	var replayQuotaVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select quota_version from public.attachment_quota_accounts where project_id = 'default'`,
	).Scan(&replayQuotaVersion); err != nil {
		t.Fatalf("read clean completion quota version: %v", err)
	}
	beforeReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, firstSeed.ProcessorJobID)
	replayed, err := repository.CompleteProcessorJob(ctx, completionInput)
	if err != nil || replayed != completed {
		t.Fatalf("CompleteProcessorJob(clean replay) = (%#v, %v), want %#v", replayed, err, completed)
	}
	afterReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, firstSeed.ProcessorJobID)
	if afterReplayToken != beforeReplayToken {
		t.Fatalf("exact clean completion replay mutated durable rows:\nbefore %#v\nafter  %#v",
			beforeReplayToken, afterReplayToken)
	}
	for _, mismatch := range []struct {
		name   string
		mutate func(*attachments.ProcessorCompletionInput)
	}{
		{name: "result owner", mutate: func(input *attachments.ProcessorCompletionInput) {
			input.Claim.OwnerID = "processor_clean_other"
		}},
		{name: "observed lease expiry", mutate: func(input *attachments.ProcessorCompletionInput) {
			input.Claim.LeaseExpiresAt = input.Claim.LeaseExpiresAt.Add(-time.Second)
		}},
	} {
		t.Run("mismatched "+mismatch.name+" replay", func(t *testing.T) {
			input := completionInput
			mismatch.mutate(&input)
			beforeMismatchToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, firstSeed.ProcessorJobID)
			if _, err := repository.CompleteProcessorJob(ctx, input); !errors.Is(err, attachments.ErrProcessorClaimLost) {
				t.Fatalf("CompleteProcessorJob(mismatched %s replay) error = %v, want ErrProcessorClaimLost",
					mismatch.name, err)
			}
			afterMismatchToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, firstSeed.ProcessorJobID)
			if afterMismatchToken != beforeMismatchToken {
				t.Fatalf("mismatched %s replay mutated durable rows:\nbefore %#v\nafter  %#v",
					mismatch.name, beforeMismatchToken, afterMismatchToken)
			}
		})
	}
	var afterReplayVersion, afterReplayBlobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select quota.quota_version, (select count(*) from public.blob_objects)
		from public.attachment_quota_accounts quota where quota.project_id = 'default'`,
	).Scan(&afterReplayVersion, &afterReplayBlobCount); err != nil {
		t.Fatalf("read clean replay effects: %v", err)
	}
	if afterReplayVersion != replayQuotaVersion || afterReplayBlobCount != 2 {
		t.Fatalf("clean replay quota version/blob count = %d/%d, want %d/2",
			afterReplayVersion, afterReplayBlobCount, replayQuotaVersion)
	}
	mismatchedPreview := attachmentProcessorBlob(0x43, 9, "clean-preview-other-v1")
	mismatchedResult := processorResult
	mismatchedResult.Preview.Blob = mismatchedPreview
	beforeMismatchToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, firstSeed.ProcessorJobID)
	if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
		Claim: *firstClaim, Result: mismatchedResult, Limits: attachments.DefaultLimits(),
	}); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteProcessorJob(mismatched terminal replay) error = %v, want ErrAttachmentConflict", err)
	}
	afterMismatchToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, firstSeed.ProcessorJobID)
	if afterMismatchToken != beforeMismatchToken {
		t.Fatalf("mismatched clean completion replay mutated durable rows:\nbefore %#v\nafter  %#v",
			beforeMismatchToken, afterMismatchToken)
	}

	secondClaim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_clean_second", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil || secondClaim == nil || secondClaim.ProcessorJobID != secondSeed.ProcessorJobID {
		t.Fatalf("ClaimProcessorJob(second clean) = (%#v, %v)", secondClaim, err)
	}
	secondCompletionInput := attachments.ProcessorCompletionInput{
		Claim: *secondClaim, Result: processorResult, Limits: attachments.DefaultLimits(),
	}
	secondCompletionInput.PreviewPublicationIntent = preparePublishedAttachmentPreviewIntent(
		t, ctx, repository, *secondClaim, preview,
	)
	secondCompleted, err := repository.CompleteProcessorJob(ctx, secondCompletionInput)
	if err != nil {
		t.Fatalf("CompleteProcessorJob(second clean) error = %v", err)
	}
	wantUsage := attachments.QuotaUsage{
		LogicalBytes:  2 * source.SizeBytes,
		PhysicalBytes: source.SizeBytes + preview.SizeBytes,
	}
	if secondCompleted.Quota.Usage != wantUsage {
		t.Fatalf("CompleteProcessorJob(second deduplicated clean) quota = %#v, want %#v",
			secondCompleted.Quota.Usage, wantUsage)
	}
	assertAttachmentProcessorCleanRows(t, ctx, fixture, secondSeed, *secondClaim, source, preview, resultDigest)

	var logicalBytes, reservedBytes, physicalBytes, quotaVersion, blobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select quota.logical_bytes, quota.reserved_bytes, quota.physical_bytes, quota.quota_version,
		       (select count(*) from public.blob_objects)
		from public.attachment_quota_accounts quota where quota.project_id = 'default'`,
	).Scan(&logicalBytes, &reservedBytes, &physicalBytes, &quotaVersion, &blobCount); err != nil {
		t.Fatalf("read deduplicated clean quota: %v", err)
	}
	if logicalBytes != wantUsage.LogicalBytes || reservedBytes != 0 || physicalBytes != wantUsage.PhysicalBytes ||
		quotaVersion != 2 || blobCount != 2 {
		t.Fatalf("deduplicated clean quota/blob = logical %d reserved %d physical %d version %d blobs %d",
			logicalBytes, reservedBytes, physicalBytes, quotaVersion, blobCount)
	}
	noClaim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_clean_terminal", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || noClaim != nil {
		t.Fatalf("ClaimProcessorJob(after terminal clean) = (%#v, %v), want nil, nil", noClaim, err)
	}
}

func TestPostgresIntegrationAttachmentProcessorPreviewBoundaryRejectsInitialAndReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-preview-boundary", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	source := attachmentProcessorBlob(0x47, 47, "preview-boundary-source-v1")
	preview := attachmentProcessorBlob(0x48, 48, "preview-boundary-preview-v1")
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "previewboundary", Source: source, State: attachments.ProcessorStateQueued,
		Profile: attachments.ProcessorProfileText, CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour), MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
	})
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_preview_boundary", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob(preview boundary) = (%#v, %v)", claim, err)
	}
	limits := attachments.DefaultLimits()
	oversized := preview
	oversized.SizeBytes = limits.MaxInlineTextPreviewBytes + 1
	oversizedResult := attachments.ProcessorResult{
		Source: source, Profile: attachments.ProcessorProfileText,
		Code: attachments.ProcessorResultCodeClean, HasPreview: true,
		Preview: attachments.ManagedPreview{Blob: oversized, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8},
	}
	if err := oversizedResult.Validate(); err != nil {
		t.Fatalf("oversized preview result shape error = %v", err)
	}
	initialInput := attachments.ProcessorCompletionInput{
		Claim: *claim, Result: oversizedResult, Limits: limits,
	}
	beforeInitial := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if _, err := repository.CompleteProcessorJob(ctx, initialInput); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("CompleteProcessorJob(initial oversized preview) error = %v, want ErrInvalidProcessorCommand", err)
	}
	afterInitial := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if afterInitial != beforeInitial {
		t.Fatalf("initial oversized preview mutated durable rows:\nbefore %#v\nafter  %#v", beforeInitial, afterInitial)
	}
	assertAttachmentProcessorPersistedJob(t, ctx, fixture, seed.ProcessorJobID,
		attachments.ProcessorStateClaimed, claim.Attempt, claim.OwnerGeneration, claim.OwnerID)

	validResult := oversizedResult
	validResult.Preview.Blob = preview
	validInput := attachments.ProcessorCompletionInput{Claim: *claim, Result: validResult, Limits: limits}
	validInput.PreviewPublicationIntent = preparePublishedAttachmentPreviewIntent(
		t, ctx, repository, *claim, preview,
	)
	if _, err := repository.CompleteProcessorJob(ctx, validInput); err != nil {
		t.Fatalf("CompleteProcessorJob(valid preview after rejected initial) error = %v", err)
	}
	beforeReplay := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if _, err := repository.CompleteProcessorJob(ctx, initialInput); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("CompleteProcessorJob(oversized replay) error = %v, want ErrInvalidProcessorCommand", err)
	}
	afterReplay := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if afterReplay != beforeReplay {
		t.Fatalf("oversized replay mutated durable rows:\nbefore %#v\nafter  %#v", beforeReplay, afterReplay)
	}
	mediaMismatch := validInput
	mediaMismatch.Result.Preview.MediaType = attachments.ManagedPreviewMediaTypePNG
	if _, err := repository.CompleteProcessorJob(ctx, mediaMismatch); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("CompleteProcessorJob(profile/media replay mismatch) error = %v, want ErrInvalidProcessorCommand", err)
	}
	afterMediaMismatch := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if afterMediaMismatch != beforeReplay {
		t.Fatalf("profile/media replay mismatch mutated durable rows:\nbefore %#v\nafter  %#v", beforeReplay, afterMediaMismatch)
	}
}

func TestPostgresIntegrationAttachmentProcessorRejectedResultsReleaseReservationExactlyOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-rejected", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	codes := []attachments.ProcessorResultCode{
		attachments.ProcessorResultCodeMalware,
		attachments.ProcessorResultCodeUnsafeContent,
	}
	for index, code := range codes {
		seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
			Suffix: fmt.Sprintf("rejected%d", index),
			Source: attachmentProcessorBlob(byte(0x51+index), int64(51+index), fmt.Sprintf("rejected-%d-v1", index)),
			State:  attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
			CreatedAt: now.Add(time.Duration(index-2) * time.Minute), ExpiresAt: now.Add(time.Hour),
			MaxAttempts: 3, ReservedQuotaBytes: int64(51 + index),
		})
		claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
			OwnerID: fmt.Sprintf("processor_rejected_%d", index), OwnerLeaseDuration: 5 * time.Minute,
		})
		if err != nil || claim == nil || claim.ProcessorJobID != seed.ProcessorJobID {
			t.Fatalf("ClaimProcessorJob(%s) = (%#v, %v)", code, claim, err)
		}
		processorResult := attachments.ProcessorResult{Source: seed.Source, Profile: seed.Profile, Code: code}
		resultDigest, err := processorResult.Digest()
		if err != nil {
			t.Fatalf("ProcessorResult.Digest(%s) error = %v", code, err)
		}
		input := attachments.ProcessorCompletionInput{
			Claim: *claim, Result: processorResult, Limits: attachments.DefaultLimits(),
		}
		completed, err := repository.CompleteProcessorJob(ctx, input)
		if err != nil {
			t.Fatalf("CompleteProcessorJob(%s) error = %v", code, err)
		}
		if completed.ProcessorState != attachments.ProcessorStateRejected ||
			completed.UploadState != attachments.UploadStateRejected ||
			completed.AttachmentState != attachments.UploadStateRejected ||
			completed.ResultCode != code || completed.ResultDigest != resultDigest ||
			completed.Quota.Usage != (attachments.QuotaUsage{}) {
			t.Fatalf("CompleteProcessorJob(%s) = %#v", code, completed)
		}
		var quotaVersion int64
		if err := fixture.db.QueryRow(ctx, `
			select quota_version from public.attachment_quota_accounts where project_id = 'default'`,
		).Scan(&quotaVersion); err != nil {
			t.Fatalf("read rejected quota version: %v", err)
		}
		if quotaVersion != int64(index+1) {
			t.Fatalf("%s quota version = %d, want %d after one reservation release",
				code, quotaVersion, index+1)
		}
		beforeReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
		replay, err := repository.CompleteProcessorJob(ctx, input)
		if err != nil || replay != completed {
			t.Fatalf("CompleteProcessorJob(%s replay) = (%#v, %v), want %#v", code, replay, err, completed)
		}
		afterReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
		if afterReplayToken != beforeReplayToken {
			t.Fatalf("exact %s completion replay mutated durable rows:\nbefore %#v\nafter  %#v",
				code, beforeReplayToken, afterReplayToken)
		}
		var afterReplayVersion int64
		if err := fixture.db.QueryRow(ctx, `
			select quota_version from public.attachment_quota_accounts where project_id = 'default'`,
		).Scan(&afterReplayVersion); err != nil {
			t.Fatalf("read rejected replay quota version: %v", err)
		}
		if afterReplayVersion != quotaVersion {
			t.Fatalf("%s replay quota version = %d, want %d", code, afterReplayVersion, quotaVersion)
		}
		assertAttachmentProcessorFailureRows(t, ctx, fixture, seed, *claim, attachments.ProcessorStateRejected,
			attachments.UploadStateRejected, code, resultDigest)
	}
	var logicalBytes, reservedBytes, physicalBytes, blobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select quota.logical_bytes, quota.reserved_bytes, quota.physical_bytes,
		       (select count(*) from public.blob_objects)
		from public.attachment_quota_accounts quota where quota.project_id = 'default'`,
	).Scan(&logicalBytes, &reservedBytes, &physicalBytes, &blobCount); err != nil {
		t.Fatalf("read rejected processor quota: %v", err)
	}
	if logicalBytes != 0 || reservedBytes != 0 || physicalBytes != 0 || blobCount != 0 {
		t.Fatalf("rejected results persisted content/quota = logical %d reserved %d physical %d blobs %d",
			logicalBytes, reservedBytes, physicalBytes, blobCount)
	}
}

func TestPostgresIntegrationAttachmentProcessorRetryableResultsStayQuarantinedThenExpireAtBounds(t *testing.T) {
	for index, code := range []attachments.ProcessorResultCode{
		attachments.ProcessorResultCodeScannerUnavailable,
		attachments.ProcessorResultCodeTimeout,
		attachments.ProcessorResultCodeProcessingError,
	} {
		t.Run(string(code), func(t *testing.T) {
			ctx := context.Background()
			fixture := newRecordsPostgresFixture(t, ctx)
			repository := NewPostgresAttachmentRepository(
				fixture.openDirectRuntimePool(t, ctx, "attachment-processor-retry-"+string(code), 2),
			)
			now := time.Now().UTC().Truncate(time.Microsecond)
			seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
				Suffix: fmt.Sprintf("retrybound%d", index),
				Source: attachmentProcessorBlob(byte(0x61+index), int64(61+index), fmt.Sprintf("retry-%d-v1", index)),
				State:  attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
				CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
				MaxAttempts: 2, ReservedQuotaBytes: int64(61 + index),
			})
			claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
				OwnerID: fmt.Sprintf("processor_retry_%d", index), OwnerLeaseDuration: 5 * time.Minute,
			})
			if err != nil || claim == nil || claim.Attempt != 1 {
				t.Fatalf("ClaimProcessorJob(first %s) = (%#v, %v)", code, claim, err)
			}
			processorResult := attachments.ProcessorResult{Source: seed.Source, Profile: seed.Profile, Code: code}
			resultDigest, err := processorResult.Digest()
			if err != nil {
				t.Fatalf("ProcessorResult.Digest(%s) error = %v", code, err)
			}
			retryAt := now.Add(20 * time.Minute)
			if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
				Claim: *claim, Result: processorResult,
				RetryAt: now.Add(-time.Minute), Limits: attachments.DefaultLimits(),
			}); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
				t.Fatalf("CompleteProcessorJob(%s past retry_at) error = %v, want ErrInvalidProcessorCommand", code, err)
			}
			assertAttachmentProcessorPersistedJob(t, ctx, fixture, seed.ProcessorJobID,
				attachments.ProcessorStateClaimed, claim.Attempt, claim.OwnerGeneration, claim.OwnerID)
			retryInput := attachments.ProcessorCompletionInput{
				Claim: *claim, Result: processorResult, RetryAt: retryAt, Limits: attachments.DefaultLimits(),
			}
			completed, err := repository.CompleteProcessorJob(ctx, retryInput)
			if err != nil {
				t.Fatalf("CompleteProcessorJob(first %s) error = %v", code, err)
			}
			if completed.ProcessorState != attachments.ProcessorStateRetryWait ||
				completed.UploadState != attachments.UploadStateQuarantined ||
				completed.AttachmentState != attachments.UploadStateQuarantined ||
				completed.ResultCode != code || completed.ResultDigest != resultDigest ||
				completed.Quota.Usage != (attachments.QuotaUsage{ReservedBytes: seed.Source.SizeBytes}) {
				t.Fatalf("CompleteProcessorJob(first %s) = %#v", code, completed)
			}
			beforeRetryToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			for _, mismatch := range []struct {
				name   string
				mutate func(*attachments.ProcessorCompletionInput)
			}{
				{name: "result owner", mutate: func(input *attachments.ProcessorCompletionInput) {
					input.Claim.OwnerID = "processor_retry_forged"
				}},
				{name: "observed lease expiry", mutate: func(input *attachments.ProcessorCompletionInput) {
					input.Claim.LeaseExpiresAt = input.Claim.LeaseExpiresAt.Add(-time.Second)
				}},
			} {
				forgedReplay := retryInput
				mismatch.mutate(&forgedReplay)
				if _, err := repository.CompleteProcessorJob(ctx, forgedReplay); !errors.Is(err, attachments.ErrProcessorClaimLost) {
					t.Fatalf("CompleteProcessorJob(retry_wait %s forged %s) error = %v, want ErrProcessorClaimLost",
						code, mismatch.name, err)
				}
				afterForgedToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
				if afterForgedToken != beforeRetryToken {
					t.Fatalf("retry_wait %s forged %s replay mutated durable rows:\nbefore %#v\nafter  %#v",
						code, mismatch.name, beforeRetryToken, afterForgedToken)
				}
			}
			var retryXID, retryUpdatedAt string
			var retryQuotaVersion int64
			if err := fixture.db.QueryRow(ctx, `
				select job.xmin::text, job.updated_at::text, quota.quota_version
				from public.attachment_processor_jobs job
				join public.attachment_quota_accounts quota on quota.project_id = 'default'
				where job.processor_job_id = $1`, seed.ProcessorJobID,
			).Scan(&retryXID, &retryUpdatedAt, &retryQuotaVersion); err != nil {
				t.Fatalf("read %s retry replay baseline: %v", code, err)
			}
			if retryQuotaVersion != 0 {
				t.Fatalf("retryable %s changed quota version to %d, want 0", code, retryQuotaVersion)
			}
			replayedRetry, err := repository.CompleteProcessorJob(ctx, retryInput)
			if err != nil || replayedRetry != completed {
				t.Fatalf("CompleteProcessorJob(retry_wait replay %s) = (%#v, %v), want %#v",
					code, replayedRetry, err, completed)
			}
			var afterRetryXID, afterRetryUpdatedAt string
			var afterRetryQuotaVersion int64
			if err := fixture.db.QueryRow(ctx, `
				select job.xmin::text, job.updated_at::text, quota.quota_version
				from public.attachment_processor_jobs job
				join public.attachment_quota_accounts quota on quota.project_id = 'default'
				where job.processor_job_id = $1`, seed.ProcessorJobID,
			).Scan(&afterRetryXID, &afterRetryUpdatedAt, &afterRetryQuotaVersion); err != nil {
				t.Fatalf("read %s retry replay effects: %v", code, err)
			}
			if afterRetryXID != retryXID || afterRetryUpdatedAt != retryUpdatedAt ||
				afterRetryQuotaVersion != retryQuotaVersion {
				t.Fatalf("retry_wait replay %s mutated job/quota %q/%q/%d -> %q/%q/%d",
					code, retryXID, retryUpdatedAt, retryQuotaVersion,
					afterRetryXID, afterRetryUpdatedAt, afterRetryQuotaVersion)
			}
			afterRetryToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			if afterRetryToken != beforeRetryToken {
				t.Fatalf("exact retry_wait %s replay mutated durable rows:\nbefore %#v\nafter  %#v",
					code, beforeRetryToken, afterRetryToken)
			}
			mismatchedRetryInput := retryInput
			mismatchedRetryInput.RetryAt = retryAt.Add(time.Second)
			beforeMismatchToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			if _, err := repository.CompleteProcessorJob(ctx, mismatchedRetryInput); !errors.Is(err, attachments.ErrAttachmentConflict) {
				t.Fatalf("CompleteProcessorJob(mismatched retry_wait replay %s) error = %v, want ErrAttachmentConflict", code, err)
			}
			afterMismatchToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			if afterMismatchToken != beforeMismatchToken {
				t.Fatalf("mismatched retry_wait %s replay mutated durable rows:\nbefore %#v\nafter  %#v",
					code, beforeMismatchToken, afterMismatchToken)
			}
			if claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
				OwnerID: "processor_retry_early", OwnerLeaseDuration: time.Minute,
			}); err != nil || claim != nil {
				t.Fatalf("ClaimProcessorJob(%s before retry_at) = (%#v, %v), want nil, nil", code, claim, err)
			}
			assertAttachmentProcessorFailureRows(t, ctx, fixture, seed, *claim, attachments.ProcessorStateRetryWait,
				attachments.UploadStateQuarantined, code, resultDigest)

			if _, err := fixture.db.Exec(ctx, `
				update public.attachment_processor_jobs
				set retry_at = transaction_timestamp() - interval '1 second'
				where processor_job_id = $1`, seed.ProcessorJobID); err != nil {
				t.Fatalf("make %s retry due: %v", code, err)
			}
			secondClaim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
				OwnerID: fmt.Sprintf("processor_retry_second_%d", index), OwnerLeaseDuration: 5 * time.Minute,
			})
			if err != nil || secondClaim == nil || secondClaim.Attempt != 2 ||
				secondClaim.OwnerGeneration != claim.OwnerGeneration+1 {
				t.Fatalf("ClaimProcessorJob(second %s) = (%#v, %v), first %#v", code, secondClaim, err, claim)
			}
			expired, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
				Claim: *secondClaim, Result: processorResult,
				RetryAt: now.Add(30 * time.Minute), Limits: attachments.DefaultLimits(),
			})
			if err != nil {
				t.Fatalf("CompleteProcessorJob(max-attempt %s) error = %v", code, err)
			}
			if expired.ProcessorState != attachments.ProcessorStateExpired ||
				expired.UploadState != attachments.UploadStateExpired ||
				expired.AttachmentState != attachments.UploadStateExpired ||
				expired.Quota.Usage != (attachments.QuotaUsage{}) || expired.UploadState == attachments.UploadStateAvailable {
				t.Fatalf("CompleteProcessorJob(max-attempt %s) = %#v", code, expired)
			}
			var quotaVersion int64
			if err := fixture.db.QueryRow(ctx, `
				select quota_version from public.attachment_quota_accounts where project_id = 'default'`,
			).Scan(&quotaVersion); err != nil {
				t.Fatalf("read max-attempt quota version: %v", err)
			}
			if quotaVersion != 1 {
				t.Fatalf("max-attempt %s quota version = %d, want one reservation release", code, quotaVersion)
			}
			beforeReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			replayed, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
				Claim: *secondClaim, Result: processorResult,
				RetryAt: now.Add(30 * time.Minute), Limits: attachments.DefaultLimits(),
			})
			if err != nil || replayed != expired {
				t.Fatalf("CompleteProcessorJob(expired replay %s) = (%#v, %v), want %#v", code, replayed, err, expired)
			}
			afterReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
			if afterReplayToken != beforeReplayToken {
				t.Fatalf("exact max-attempt %s completion replay mutated durable rows:\nbefore %#v\nafter  %#v",
					code, beforeReplayToken, afterReplayToken)
			}
			var afterReplayVersion int64
			if err := fixture.db.QueryRow(ctx, `
				select quota_version from public.attachment_quota_accounts where project_id = 'default'`,
			).Scan(&afterReplayVersion); err != nil {
				t.Fatalf("read max-attempt replay quota version: %v", err)
			}
			if afterReplayVersion != quotaVersion {
				t.Fatalf("max-attempt %s replay quota version = %d, want %d", code, afterReplayVersion, quotaVersion)
			}
			assertAttachmentProcessorFailureRows(t, ctx, fixture, seed, *secondClaim, attachments.ProcessorStateExpired,
				attachments.UploadStateExpired, code, resultDigest)
		})
	}
}

func TestPostgresIntegrationAttachmentProcessorRetryableResultExpiresWhenRetryCrossesOverallDeadline(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-processor-overall-expiry", 2),
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "retryoverall", Source: attachmentProcessorBlob(0x71, 71, "retry-overall-v1"),
		State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(6 * time.Minute),
		MaxAttempts: 4, ReservedQuotaBytes: 71,
	})
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_retry_overall", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob(overall expiry) = (%#v, %v)", claim, err)
	}
	if claim.LeaseExpiresAt.After(claim.ExpiresAt) || !claim.LeaseExpiresAt.After(now) {
		t.Fatalf("ClaimProcessorJob(overall expiry) lease = %s, deadline %s, now %s",
			claim.LeaseExpiresAt, claim.ExpiresAt, now)
	}
	processorResult := attachments.ProcessorResult{
		Source: seed.Source, Profile: seed.Profile, Code: attachments.ProcessorResultCodeScannerUnavailable,
	}
	resultDigest, err := processorResult.Digest()
	if err != nil {
		t.Fatalf("ProcessorResult.Digest(overall expiry) error = %v", err)
	}
	completionInput := attachments.ProcessorCompletionInput{
		Claim: *claim, Result: processorResult,
		RetryAt: claim.ExpiresAt.Add(time.Minute), Limits: attachments.DefaultLimits(),
	}
	completed, err := repository.CompleteProcessorJob(ctx, completionInput)
	if err != nil {
		t.Fatalf("CompleteProcessorJob(overall expiry) error = %v", err)
	}
	if completed.ProcessorState != attachments.ProcessorStateExpired ||
		completed.UploadState != attachments.UploadStateExpired ||
		completed.AttachmentState != attachments.UploadStateExpired ||
		completed.ResultCode != processorResult.Code || completed.ResultDigest != resultDigest ||
		completed.Quota.Usage != (attachments.QuotaUsage{}) || completed.UploadState == attachments.UploadStateAvailable {
		t.Fatalf("CompleteProcessorJob(overall expiry) = %#v", completed)
	}
	assertAttachmentProcessorFailureRows(t, ctx, fixture, seed, *claim, attachments.ProcessorStateExpired,
		attachments.UploadStateExpired, processorResult.Code, resultDigest)
	beforeReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if beforeReplayToken.QuotaVersion != 1 {
		t.Fatalf("overall-expiry quota version = %d, want one reservation release", beforeReplayToken.QuotaVersion)
	}
	replayed, err := repository.CompleteProcessorJob(ctx, completionInput)
	if err != nil || replayed != completed {
		t.Fatalf("CompleteProcessorJob(overall-expiry replay) = (%#v, %v), want %#v", replayed, err, completed)
	}
	afterReplayToken := readAttachmentProcessorPersistenceToken(t, ctx, fixture, seed.ProcessorJobID)
	if afterReplayToken != beforeReplayToken || afterReplayToken.BlobXIDs != "" {
		t.Fatalf("overall-expiry replay persisted or mutated content rows:\nbefore %#v\nafter  %#v",
			beforeReplayToken, afterReplayToken)
	}
}

func TestPostgresIntegrationAttachmentProcessorCompletionRollsBackAtWriteCutpoints(t *testing.T) {
	tests := []struct {
		name            string
		install         func(*testing.T, context.Context, recordPlatformPostgresFixture)
		previewConflict bool
		wantBlobCount   int64
		wantError       error
		wantErrorText   string
	}{
		{name: "preview identity conflict", previewConflict: true, wantBlobCount: 1,
			wantError: attachments.ErrAttachmentConflict},
		{name: "logical attachment write", install: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
			fixture.installAttachmentProcessorFailureTrigger(t, ctx, "record_attachments")
		}, wantErrorText: "injected attachment processor write cutpoint"},
		{name: "quota write", install: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
			fixture.installAttachmentProcessorFailureTrigger(t, ctx, "attachment_quota_accounts")
		}, wantErrorText: "injected attachment processor write cutpoint"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRecordsPostgresFixture(t, ctx)
			repository := NewPostgresAttachmentRepository(
				fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("attachment-processor-cutpoint-%d", index), 2),
			)
			now := time.Now().UTC().Truncate(time.Microsecond)
			source := attachmentProcessorBlob(byte(0x81+index), int64(81+index), fmt.Sprintf("cutpoint-source-%d-v1", index))
			preview := attachmentProcessorBlob(byte(0x91+index), int64(21+index), fmt.Sprintf("cutpoint-preview-%d-v1", index))
			seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
				Suffix: fmt.Sprintf("cutpoint%d", index), Source: source,
				State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
				CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
				MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
			})
			claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
				OwnerID: fmt.Sprintf("processor_cutpoint_%d", index), OwnerLeaseDuration: 5 * time.Minute,
			})
			if err != nil || claim == nil {
				t.Fatalf("ClaimProcessorJob(cutpoint) = (%#v, %v)", claim, err)
			}
			if test.previewConflict {
				if _, err := fixture.db.Exec(ctx, `
					insert into public.blob_objects (
						blob_key, sha256_digest, object_version, size_bytes, backend_kind
					) values ($1, $2, 'conflicting-preview-version', $3, $4)`,
					preview.Key, preview.SHA256[:], preview.SizeBytes, preview.BackendKind,
				); err != nil {
					t.Fatalf("seed conflicting preview Blob: %v", err)
				}
			}
			if test.install != nil {
				test.install(t, ctx, fixture)
			}
			processorResult := attachments.ProcessorResult{
				Source: source, Profile: attachments.ProcessorProfileText,
				Code: attachments.ProcessorResultCodeClean, HasPreview: true,
				Preview: attachments.ManagedPreview{
					Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8,
				},
			}
			completionInput := attachments.ProcessorCompletionInput{
				Claim: *claim, Result: processorResult, Limits: attachments.DefaultLimits(),
			}
			completionInput.PreviewPublicationIntent = preparePublishedAttachmentPreviewIntent(
				t, ctx, repository, *claim, preview,
			)
			_, err = repository.CompleteProcessorJob(ctx, completionInput)
			if err == nil {
				t.Fatal("CompleteProcessorJob(cutpoint) error = nil")
			}
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("CompleteProcessorJob(%s) error = %v, want %v", test.name, err, test.wantError)
			}
			if test.wantErrorText != "" && !strings.Contains(err.Error(), test.wantErrorText) {
				t.Fatalf("CompleteProcessorJob(%s) error = %v, want injected cutpoint evidence", test.name, err)
			}
			assertAttachmentProcessorFailedCompletionRolledBack(
				t, ctx, fixture, seed, *claim, test.wantBlobCount,
				attachments.QuotaUsage{ReservedBytes: source.SizeBytes}, 0,
			)
		})
	}
}

func TestPostgresIntegrationAttachmentProcessorCompletionProtectsQuotaVersionAndOverflow(t *testing.T) {
	tests := []struct {
		name      string
		logical   int64
		physical  int64
		version   int64
		wantError error
	}{
		{name: "quota version overflow", version: math.MaxInt64, wantError: attachments.ErrQuotaOverflow},
		{name: "logical byte overflow", logical: math.MaxInt64, wantError: attachments.ErrQuotaOverflow},
		{name: "physical byte overflow", physical: math.MaxInt64, wantError: attachments.ErrQuotaOverflow},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRecordsPostgresFixture(t, ctx)
			repository := NewPostgresAttachmentRepository(
				fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("attachment-processor-overflow-%d", index), 2),
			)
			now := time.Now().UTC().Truncate(time.Microsecond)
			source := attachmentProcessorBlob(byte(0xa1+index), int64(31+index), fmt.Sprintf("overflow-source-%d-v1", index))
			seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
				Suffix: fmt.Sprintf("overflow%d", index), Source: source,
				State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileArchive,
				CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
				MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
			})
			if _, err := fixture.db.Exec(ctx, `
				update public.attachment_quota_accounts
				set logical_bytes = $1, physical_bytes = $2, quota_version = $3
				where project_id = 'default'`, test.logical, test.physical, test.version); err != nil {
				t.Fatalf("seed processor quota overflow: %v", err)
			}
			claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
				OwnerID: fmt.Sprintf("processor_overflow_%d", index), OwnerLeaseDuration: 5 * time.Minute,
			})
			if err != nil || claim == nil {
				t.Fatalf("ClaimProcessorJob(overflow) = (%#v, %v)", claim, err)
			}
			processorResult := attachments.ProcessorResult{
				Source: source, Profile: attachments.ProcessorProfileArchive,
				Code: attachments.ProcessorResultCodeClean,
			}
			if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
				Claim: *claim, Result: processorResult, Limits: attachments.DefaultLimits(),
			}); !errors.Is(err, test.wantError) {
				t.Fatalf("CompleteProcessorJob(%s) error = %v, want %v", test.name, err, test.wantError)
			}
			assertAttachmentProcessorFailedCompletionRolledBack(
				t, ctx, fixture, seed, *claim, 0,
				attachments.QuotaUsage{
					LogicalBytes: test.logical, ReservedBytes: source.SizeBytes, PhysicalBytes: test.physical,
				},
				test.version,
			)
		})
	}
}

type attachmentUploadWorkflowAuthorizer struct {
	actor   recordauth.ActorScope
	draftID string
}

func (authorizer attachmentUploadWorkflowAuthorizer) AuthorizeDraftAttachmentUpload(
	_ context.Context,
	actor recordauth.ActorScope,
	draftID string,
) error {
	if actor.UserID != authorizer.actor.UserID || actor.ProjectID != authorizer.actor.ProjectID ||
		draftID != authorizer.draftID {
		return recordauth.ErrDenied
	}
	return nil
}

type attachmentUploadWorkflowRepository struct {
	*PostgresAttachmentRepository
	completeErr       error
	realCompleteCalls int
}

type attachmentUploadWorkflowBlob struct {
	*attachments.S3BlobStore
	resolveTemporaryCalls int
	openTemporaryCalls    int
	publishTemporaryCalls int
	deleteTemporaryCalls  int
}

func (blob *attachmentUploadWorkflowBlob) ResolveTemporaryVersion(
	ctx context.Context,
	key string,
) (attachments.TemporaryObjectVersion, error) {
	blob.resolveTemporaryCalls++
	return blob.S3BlobStore.ResolveTemporaryVersion(ctx, key)
}

func (blob *attachmentUploadWorkflowBlob) OpenTemporaryVersion(
	ctx context.Context,
	request attachments.TemporaryObjectReadRequest,
) (io.ReadCloser, error) {
	blob.openTemporaryCalls++
	return blob.S3BlobStore.OpenTemporaryVersion(ctx, request)
}

func (blob *attachmentUploadWorkflowBlob) PublishTemporaryVersion(
	ctx context.Context,
	request attachments.TemporaryObjectPublishRequest,
) (attachments.ObjectVersion, error) {
	blob.publishTemporaryCalls++
	return blob.S3BlobStore.PublishTemporaryVersion(ctx, request)
}

func (blob *attachmentUploadWorkflowBlob) DeleteTemporaryVersion(
	ctx context.Context,
	version attachments.TemporaryObjectVersion,
) error {
	blob.deleteTemporaryCalls++
	return blob.S3BlobStore.DeleteTemporaryVersion(ctx, version)
}

func (repository *attachmentUploadWorkflowRepository) CompleteUploadAndEnqueue(
	ctx context.Context,
	command attachments.CompleteUploadAndEnqueueCommand,
) (attachments.UploadMutationResult, error) {
	repository.realCompleteCalls++
	result, err := repository.PostgresAttachmentRepository.CompleteUploadAndEnqueue(ctx, command)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if repository.completeErr != nil {
		return attachments.UploadMutationResult{}, repository.completeErr
	}
	return result, nil
}

func attachmentUploadWorkflowActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func newAttachmentUploadWorkflowService(
	t *testing.T,
	repository attachments.UploadServiceRepository,
	blob attachments.BlobStore,
	transport attachments.TransportKind,
	actor recordauth.ActorScope,
	draftID string,
	now func() time.Time,
) *attachments.UploadService {
	t.Helper()
	service, err := attachments.NewUploadService(
		attachmentUploadWorkflowAuthorizer{actor: actor, draftID: draftID},
		repository,
		blob,
		attachments.UploadServiceOptions{
			TransportKind: transport, Limits: attachments.DefaultLimits(), Now: now, ProcessorMaxAttempts: 3,
		},
	)
	if err != nil {
		t.Fatalf("NewUploadService() error = %v", err)
	}
	return service
}

func assertAttachmentUploadWorkflowRows(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	uploadID string,
	wantObject attachments.ObjectVersion,
	wantJobs int64,
) {
	t.Helper()
	var size int64
	var digest []byte
	var version string
	if err := fixture.db.QueryRow(ctx, `
		select size_bytes, sha256_digest, object_version
		from public.attachment_upload_parts where upload_id = $1 and part_number = 1`,
		uploadID,
	).Scan(&size, &digest, &version); err != nil {
		t.Fatalf("read attachment workflow part: %v", err)
	}
	if size != wantObject.SizeBytes || version != wantObject.VersionID ||
		!bytes.Equal(digest, wantObject.SHA256[:]) {
		t.Fatalf("attachment workflow part = size %d digest %x version %q, want %#v",
			size, digest, version, wantObject)
	}
	assertAttachmentUploadWorkflowRowCounts(t, ctx, fixture, uploadID, 1, wantJobs)
}

func assertAttachmentUploadWorkflowRowCounts(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	uploadID string,
	wantParts int64,
	wantJobs int64,
) {
	t.Helper()
	var partCount, jobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*) from public.attachment_upload_parts where upload_id = $1),
		       (select count(*) from public.attachment_processor_jobs where upload_id = $1)`,
		uploadID,
	).Scan(&partCount, &jobCount); err != nil {
		t.Fatalf("count attachment workflow rows: %v", err)
	}
	if partCount != wantParts || jobCount != wantJobs {
		t.Fatalf("attachment workflow part/job count = %d/%d, want %d/%d",
			partCount, jobCount, wantParts, wantJobs)
	}
}

func assertAttachmentUploadWorkflowReplayConflict(
	t *testing.T,
	ctx context.Context,
	repository *PostgresAttachmentRepository,
	uploadID string,
	authorID string,
	object attachments.ObjectVersion,
	temporaryKey string,
	temporaryVersion string,
	expiresAt time.Time,
) {
	t.Helper()
	_, err := repository.CompleteUploadAndEnqueue(ctx, attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: "default", UploadID: uploadID, AuthorID: authorID,
		ActualSizeBytes: object.SizeBytes, ActualSHA256: object.SHA256,
		TemporaryObjectKey: temporaryKey, TemporaryObjectVersion: temporaryVersion,
		Object: object, CompletionFingerprint: sha256.Sum256([]byte("mismatched workflow replay")),
		ProcessorJobID: "apj_" + uploadID[len("aup_"):], ProcessorProfile: attachments.ProcessorProfileText,
		ProcessorMaxAttempts: 3, ProcessorExpiresAt: expiresAt,
	})
	if !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteUploadAndEnqueue(mismatched replay) error = %v, want ErrAttachmentConflict", err)
	}
}

func newAttachmentUploadWorkflowMinIO(t *testing.T) (*minio.Client, string) {
	t.Helper()
	if os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_MINIO_INTEGRATION=1 to run the real MinIO workflow")
	}
	required := func(name string) string {
		t.Helper()
		value := os.Getenv(name)
		if value == "" {
			t.Fatalf("%s is required", name)
		}
		return value
	}
	secure := false
	if value := os.Getenv("HOUFENG_MINIO_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			t.Fatalf("parse HOUFENG_MINIO_SECURE: %v", err)
		}
		secure = parsed
	}
	client, err := minio.New(required("HOUFENG_MINIO_ENDPOINT"), &minio.Options{
		Creds: credentials.NewStaticV4(
			required("HOUFENG_MINIO_ACCESS_KEY"), required("HOUFENG_MINIO_SECRET_KEY"), "",
		),
		Secure: secure,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	suffix := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())))
	bucket := fmt.Sprintf("%.32s-workflow-%x", required("HOUFENG_MINIO_BUCKET"), suffix[:6])
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("MakeBucket() error = %v", err)
	}
	if err := client.EnableVersioning(ctx, bucket); err != nil {
		t.Fatalf("EnableVersioning() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for object := range client.ListObjects(cleanupCtx, bucket, minio.ListObjectsOptions{
			Recursive: true, WithVersions: true,
		}) {
			if object.Err != nil {
				t.Errorf("ListObjects(cleanup) error = %v", object.Err)
				continue
			}
			if err := client.RemoveObject(cleanupCtx, bucket, object.Key, minio.RemoveObjectOptions{
				VersionID: object.VersionID,
			}); err != nil {
				t.Errorf("RemoveObject(cleanup) error = %v", err)
			}
		}
		if err := client.RemoveBucket(cleanupCtx, bucket); err != nil {
			t.Errorf("RemoveBucket(cleanup) error = %v", err)
		}
	})
	return client, bucket
}

func attachmentBlobGCIntegrationObject(
	content string,
	version string,
	backend attachments.BackendKind,
) attachments.BlobObject {
	digest := sha256.Sum256([]byte(content))
	return attachments.BlobObject{
		Key: "sha256/" + fmt.Sprintf("%x", digest), SHA256: digest,
		ObjectVersion: version, SizeBytes: int64(len(content)), BackendKind: backend,
	}
}

func attachmentBlobObjectFromVersion(
	version attachments.ObjectVersion,
	backend attachments.BackendKind,
) attachments.BlobObject {
	return attachments.BlobObject{
		Key: version.Key, SHA256: version.SHA256, ObjectVersion: version.VersionID,
		SizeBytes: version.SizeBytes, BackendKind: backend,
	}
}

func attachmentBlobGCPermanentClaimRequest(
	object attachments.BlobObject,
	ownerID string,
) attachments.BlobGCClaimRequest {
	return attachments.BlobGCClaimRequest{
		ProjectID: "default", BackendKind: object.BackendKind,
		Mode: attachments.BlobGCPurgeModePermanent, Object: object,
		OwnerID: ownerID, OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration,
	}
}

func seedAttachmentBlobGCObject(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	object attachments.BlobObject,
	createdAt time.Time,
) {
	t.Helper()
	if object.Validate() != nil || createdAt.IsZero() {
		t.Fatalf("invalid Blob GC integration object = %#v at %s", object, createdAt)
	}
	tx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Blob GC object seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind, created_at
		) values ($1, $2, $3, $4, $5, $6)`,
		object.Key, object.SHA256[:], object.ObjectVersion, object.SizeBytes,
		object.BackendKind, createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed Blob GC object %q: %v", object.Key, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_quota_accounts (project_id, physical_bytes)
		values ('default', $1)
		on conflict (project_id) do update
		set physical_bytes = public.attachment_quota_accounts.physical_bytes + excluded.physical_bytes`,
		object.SizeBytes,
	); err != nil {
		t.Fatalf("seed Blob GC physical quota for %q: %v", object.Key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit Blob GC object seed %q: %v", object.Key, err)
	}
}

func seedAttachmentBlobGCOriginalReference(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	suffix string,
	object attachments.BlobObject,
) {
	t.Helper()
	draftID, authorID := "rdf_"+suffix, "usr_"+suffix
	seedAttachmentDraft(t, ctx, fixture, draftID, authorID)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, draft_id, origin_draft_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ($1, $2, $2, 'available', $3, 'text/plain', $4, $5, $6, $7)`,
		"att_"+suffix, draftID, suffix+".txt", object.SizeBytes,
		object.Key, object.ObjectVersion, authorID,
	); err != nil {
		t.Fatalf("seed original Blob GC reference %q: %v", suffix, err)
	}
}

func seedAttachmentBlobGCPreviewReference(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	suffix string,
	preview attachments.BlobObject,
	createdAt time.Time,
) {
	t.Helper()
	original := attachmentBlobGCIntegrationObject(
		"original for "+suffix, "local-gc-"+suffix+"-original-v1", attachments.BackendKindLocal,
	)
	seedAttachmentBlobGCObject(t, ctx, fixture, original, createdAt.Add(-time.Hour))
	draftID, authorID := "rdf_"+suffix, "usr_"+suffix
	seedAttachmentDraft(t, ctx, fixture, draftID, authorID)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, draft_id, origin_draft_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version,
			preview_blob_key, preview_blob_object_version,
			preview_media_type, preview_size_bytes, created_by
		) values (
			$1, $2, $2, 'available', $3, 'text/plain', $4,
			$5, $6, $7, $8, 'text/plain; charset=utf-8', $9, $10
		)`,
		"att_"+suffix, draftID, suffix+".txt", original.SizeBytes,
		original.Key, original.ObjectVersion, preview.Key, preview.ObjectVersion,
		preview.SizeBytes, authorID,
	); err != nil {
		t.Fatalf("seed preview Blob GC reference %q: %v", suffix, err)
	}
}

func seedAttachmentBlobGCUploadShell(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	suffix string,
) {
	t.Helper()
	draftID, authorID := "rdf_"+suffix, "usr_"+suffix
	seedAttachmentDraft(t, ctx, fixture, draftID, authorID)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, draft_id, origin_draft_id, attachment_state,
			display_name, media_type, logical_size_bytes, created_by
		) values ($1, $2, $2, 'uploading', $3, 'text/plain', 1, $4)`,
		"att_"+suffix, draftID, suffix+".txt", authorID,
	); err != nil {
		t.Fatalf("seed Blob GC upload attachment %q: %v", suffix, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_uploads (
			upload_id, attachment_id, origin_draft_id, author_id,
			upload_state, transport_kind, declared_size_bytes,
			reserved_size_bytes, expires_at
		) values ($1, $2, $3, $4, 'uploading', 'local', 1, 1,
			transaction_timestamp() + interval '1 hour')`,
		"aup_"+suffix, "att_"+suffix, draftID, authorID,
	); err != nil {
		t.Fatalf("seed Blob GC upload session %q: %v", suffix, err)
	}
}

func seedAttachmentBlobGCUploadPartReference(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	suffix string,
	object attachments.BlobObject,
) {
	t.Helper()
	seedAttachmentBlobGCUploadShell(t, ctx, fixture, suffix)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_upload_parts (
			upload_id, part_number, size_bytes, sha256_digest, object_version
		) values ($1, 1, $2, $3, $4)`,
		"aup_"+suffix, object.SizeBytes, object.SHA256[:], object.ObjectVersion,
	); err != nil {
		t.Fatalf("seed upload-part Blob GC reference %q: %v", suffix, err)
	}
}

func seedAttachmentBlobGCPin(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	pinID string,
	ownerID string,
	object attachments.BlobObject,
	createdAt time.Time,
	expiresAt time.Time,
) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_gc_pins (
			pin_id, pin_owner_kind, pin_owner_id, blob_key,
			blob_object_version, created_at, expires_at
		) values ($1, 'backup_manifest', $2, $3, $4, $5, $6)`,
		pinID, ownerID, object.Key, object.ObjectVersion, createdAt.UTC(), expiresAt.UTC(),
	); err != nil {
		t.Fatalf("seed Blob GC pin %q: %v", pinID, err)
	}
}

func assertAttachmentBlobGCQuota(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	wantPhysicalBytes int64,
	wantQuotaVersion int64,
) {
	t.Helper()
	var physicalBytes, quotaVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select physical_bytes, quota_version
		from public.attachment_quota_accounts where project_id = 'default'`,
	).Scan(&physicalBytes, &quotaVersion); err != nil {
		t.Fatalf("read Blob GC quota: %v", err)
	}
	if physicalBytes != wantPhysicalBytes || quotaVersion != wantQuotaVersion {
		t.Fatalf("Blob GC quota = %d@%d, want %d@%d",
			physicalBytes, quotaVersion, wantPhysicalBytes, wantQuotaVersion)
	}
}

func assertAttachmentBlobGCTerminalResult(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	object attachments.BlobObject,
	wantResult string,
) {
	t.Helper()
	var state, result string
	var receiptDigest []byte
	var completedAt *time.Time
	if err := fixture.db.QueryRow(ctx, `
		select deletion_state, physical_delete_result, receipt_digest, completed_at
		from public.blob_gc_deletions
		where blob_key = $1 and object_version = $2
		order by created_at desc limit 1`,
		object.Key, object.ObjectVersion,
	).Scan(&state, &result, &receiptDigest, &completedAt); err != nil {
		t.Fatalf("read terminal Blob GC deletion %q: %v", object.Key, err)
	}
	if state != "completed" || result != wantResult || len(receiptDigest) != sha256.Size || completedAt == nil {
		t.Fatalf("terminal Blob GC deletion %q = %q/%q digest %x completed %#v",
			object.Key, state, result, receiptDigest, completedAt)
	}
}

func seedAttachmentDraft(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, draftID, authorID string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_drafts (
			draft_id, author_id, payload, payload_hash, draft_version,
			etag_digest, warning_at, expires_at
		) values (
			$1, $2, '{}'::jsonb, decode(repeat('31', 32), 'hex'), 1,
			decode(repeat('32', 32), 'hex'), now() + interval '1 day', now() + interval '2 days'
		)
	`, draftID, authorID); err != nil {
		t.Fatalf("seed attachment draft %q: %v", draftID, err)
	}
}

func attachmentLifecycleReservationCommand(index int) attachments.ReserveUploadCommand {
	return attachments.ReserveUploadCommand{
		ProjectID:         "default",
		UploadID:          fmt.Sprintf("aup_lifecycle%d", index),
		AttachmentID:      fmt.Sprintf("att_lifecycle%d", index),
		DraftID:           "rdf_attlifecycle",
		AuthorID:          "usr_attlifecycle",
		DisplayName:       fmt.Sprintf("lifecycle-%d.txt", index),
		MediaType:         "text/plain",
		TransportKind:     attachments.TransportKindLocal,
		DeclaredSizeBytes: 9,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		Limits:            attachments.DefaultLimits(),
	}
}

func completeAttachmentIntegrationUpload(
	t *testing.T,
	ctx context.Context,
	repository *PostgresAttachmentRepository,
	reserve attachments.ReserveUploadCommand,
	object attachments.ObjectVersion,
	fingerprint [sha256.Size]byte,
	processorJobID string,
) attachments.UploadMutationResult {
	t.Helper()
	if _, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	}); err != nil {
		t.Fatalf("PrepareUpload(%s) error = %v", reserve.UploadID, err)
	}
	publicationIntent := preparePublishedAttachmentUploadIntent(t, ctx, repository, reserve, object)
	if _, err := repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		Object: object, PublicationIntent: publicationIntent,
	}); err != nil {
		t.Fatalf("RecordUploadedContent(%s) error = %v", reserve.UploadID, err)
	}
	command := attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		ActualSizeBytes: object.SizeBytes, ActualSHA256: object.SHA256, Object: object,
		CompletionFingerprint: fingerprint, ProcessorJobID: processorJobID,
		ProcessorProfile: attachments.ProcessorProfileText, ProcessorMaxAttempts: 3,
		ProcessorExpiresAt: reserve.ExpiresAt,
	}
	completed, err := repository.CompleteUploadAndEnqueue(ctx, command)
	if err != nil {
		t.Fatalf("CompleteUploadAndEnqueue(%s) error = %v", reserve.UploadID, err)
	}
	completedReplay, err := repository.CompleteUploadAndEnqueue(ctx, command)
	if err != nil || completedReplay != completed {
		t.Fatalf("CompleteUploadAndEnqueue(%s replay) = (%#v, %v), want %#v", reserve.UploadID, completedReplay, err, completed)
	}
	return completed
}

func preparePublishedAttachmentUploadIntent(
	t *testing.T,
	ctx context.Context,
	repository *PostgresAttachmentRepository,
	reserve attachments.ReserveUploadCommand,
	object attachments.ObjectVersion,
) attachments.BlobPublicationIntent {
	t.Helper()
	var backend attachments.BackendKind
	switch reserve.TransportKind {
	case attachments.TransportKindLocal:
		backend = attachments.BackendKindLocal
	case attachments.TransportKindS3:
		backend = attachments.BackendKindS3
	default:
		t.Fatalf("unsupported attachment publication transport %q", reserve.TransportKind)
	}
	return preparePublishedAttachmentIntent(t, ctx, repository, attachments.BlobPublicationPrepareRequest{
		ProjectID: reserve.ProjectID, OwnerKind: attachments.BlobPublicationOwnerUpload,
		OwnerID: reserve.UploadID, OwnerGeneration: 1,
		Target: attachments.BlobPublicationTarget{
			Key: object.Key, SHA256: object.SHA256, SizeBytes: object.SizeBytes, BackendKind: backend,
		},
		PublishExpiresAt: reserve.ExpiresAt,
	}, object)
}

func preparePublishedAttachmentPreviewIntent(
	t *testing.T,
	ctx context.Context,
	repository *PostgresAttachmentRepository,
	claim attachments.ProcessorClaim,
	preview attachments.BlobObject,
) attachments.BlobPublicationIntent {
	t.Helper()
	return preparePublishedAttachmentIntent(t, ctx, repository, attachments.BlobPublicationPrepareRequest{
		ProjectID: claim.ProjectID, OwnerKind: attachments.BlobPublicationOwnerProcessorPreview,
		OwnerID: claim.ProcessorJobID, OwnerGeneration: claim.OwnerGeneration,
		Target: attachments.BlobPublicationTarget{
			Key: preview.Key, SHA256: preview.SHA256,
			SizeBytes: preview.SizeBytes, BackendKind: preview.BackendKind,
		},
		PublishExpiresAt: claim.ExpiresAt,
	}, attachments.ObjectVersion{
		Key: preview.Key, VersionID: preview.ObjectVersion,
		SHA256: preview.SHA256, SizeBytes: preview.SizeBytes,
	})
}

func preparePublishedAttachmentIntent(
	t *testing.T,
	ctx context.Context,
	repository *PostgresAttachmentRepository,
	request attachments.BlobPublicationPrepareRequest,
	object attachments.ObjectVersion,
) attachments.BlobPublicationIntent {
	t.Helper()
	intent, err := repository.PrepareBlobPublication(ctx, request)
	if err != nil {
		t.Fatalf("PrepareBlobPublication(%s/%s) error = %v", request.OwnerKind, request.OwnerID, err)
	}
	if intent.State == attachments.BlobPublicationStatePublished {
		if intent.ObjectVersion != object.VersionID {
			t.Fatalf("PrepareBlobPublication(%s/%s) replay version = %q, want %q",
				request.OwnerKind, request.OwnerID, intent.ObjectVersion, object.VersionID)
		}
		return intent
	}
	published, err := repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{
		Intent: intent, Object: object,
	})
	if err != nil {
		t.Fatalf("RecordBlobPublicationVersion(%s/%s) error = %v", request.OwnerKind, request.OwnerID, err)
	}
	return published
}

const attachmentProcessorClaimBlockLock int64 = 8240053

type attachmentProcessorSeed struct {
	Suffix               string
	Source               attachments.BlobObject
	State                attachments.ProcessorState
	Profile              attachments.ProcessorProfile
	Attempt              int64
	MaxAttempts          int64
	OwnerID              string
	OwnerGeneration      int64
	LeaseExpiresAt       *time.Time
	RetryAt              *time.Time
	ResultCode           attachments.ProcessorResultCode
	ResultOwnerID        string
	ResultLeaseExpiresAt *time.Time
	CreatedAt            time.Time
	ExpiresAt            time.Time
	UploadState          attachments.UploadState
	ReservedQuotaBytes   int64
}

type attachmentProcessorSeedResult struct {
	ProcessorJobID    string
	UploadID          string
	AttachmentID      string
	DisplayName       string
	DeclaredMediaType string
	DraftID           string
	AuthorID          string
	Source            attachments.BlobObject
	Profile           attachments.ProcessorProfile
	MaxAttempts       int64
	ExpiresAt         time.Time
}

type attachmentProcessorPersistenceToken struct {
	JobXID              string
	JobUpdatedAt        string
	UploadXID           string
	UploadUpdatedAt     string
	AttachmentXID       string
	AttachmentUpdatedAt string
	QuotaXID            string
	QuotaUpdatedAt      string
	QuotaVersion        int64
	BlobXIDs            string
}

func seedAttachmentProcessorJob(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	seed attachmentProcessorSeed,
) attachmentProcessorSeedResult {
	t.Helper()
	if seed.Suffix == "" || seed.Source.Validate() != nil || seed.State == "" || seed.Profile == "" {
		t.Fatalf("invalid attachment processor seed = %#v", seed)
	}
	if seed.CreatedAt.IsZero() {
		seed.CreatedAt = time.Now().UTC().Add(-time.Minute)
	}
	if seed.ExpiresAt.IsZero() {
		seed.ExpiresAt = time.Now().UTC().Add(time.Hour)
	}
	seed.CreatedAt = seed.CreatedAt.UTC().Truncate(time.Microsecond)
	seed.ExpiresAt = seed.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !seed.ExpiresAt.After(seed.CreatedAt) {
		t.Fatalf("attachment processor seed expiry %s is not after creation %s", seed.ExpiresAt, seed.CreatedAt)
	}
	if seed.MaxAttempts == 0 {
		seed.MaxAttempts = 3
	}
	if seed.UploadState == "" {
		seed.UploadState = attachments.UploadStateQuarantined
	}
	displayName, declaredMediaType := attachmentProcessorSeedMetadata(seed.Suffix, seed.Profile)
	if seed.State == attachments.ProcessorStateRetryWait && seed.ResultCode == "" {
		seed.ResultCode = attachments.ProcessorResultCodeScannerUnavailable
	}
	result := attachmentProcessorSeedResult{
		ProcessorJobID:    "apj_" + seed.Suffix,
		UploadID:          "aup_" + seed.Suffix,
		AttachmentID:      "att_" + seed.Suffix,
		DisplayName:       displayName,
		DeclaredMediaType: declaredMediaType,
		DraftID:           "rdf_" + seed.Suffix,
		AuthorID:          "usr_" + seed.Suffix,
		Source:            seed.Source,
		Profile:           seed.Profile,
		MaxAttempts:       seed.MaxAttempts,
		ExpiresAt:         seed.ExpiresAt,
	}
	seedAttachmentDraft(t, ctx, fixture, result.DraftID, result.AuthorID)

	var resultCode any
	var resultDigest any
	resultOwnerID := ""
	var resultLeaseExpiresAt any
	if seed.ResultCode != "" {
		digest, err := (attachments.ProcessorResult{
			Source: seed.Source, Profile: seed.Profile, Code: seed.ResultCode,
		}).Digest()
		if err != nil {
			t.Fatalf("build attachment processor seed result: %v", err)
		}
		resultCode = seed.ResultCode
		resultDigest = digest[:]
		resultOwnerID = seed.ResultOwnerID
		if resultOwnerID == "" {
			resultOwnerID = "processor_seed"
		}
		if seed.ResultLeaseExpiresAt == nil {
			resultLeaseExpiresAt = seed.CreatedAt.Add(time.Minute)
		} else {
			resultLeaseExpiresAt = seed.ResultLeaseExpiresAt.UTC().Truncate(time.Microsecond)
		}
	}
	transport := attachments.TransportKindLocal
	if seed.Source.BackendKind == attachments.BackendKindS3 {
		transport = attachments.TransportKindS3
	}
	completionFingerprint := sha256.Sum256([]byte("completion-" + seed.Suffix))
	tx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin attachment processor seed %q: %v", seed.Suffix, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, draft_id, origin_draft_id,
			attachment_state, display_name, media_type, logical_size_bytes,
			created_by, created_at, updated_at
		) values ($1, 'default', $2, $2, $3, $4, $5, $6, $7, $8, $8)`,
		result.AttachmentID,
		result.DraftID,
		seed.UploadState,
		result.DisplayName,
		result.DeclaredMediaType,
		seed.Source.SizeBytes,
		result.AuthorID,
		seed.CreatedAt,
	); err != nil {
		t.Fatalf("seed attachment processor attachment %q: %v", seed.Suffix, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_uploads (
			upload_id, project_id, attachment_id, origin_draft_id, author_id,
			upload_state, transport_kind, declared_size_bytes, reserved_size_bytes,
			actual_size_bytes, actual_sha256_digest, completion_fingerprint,
			completed_at, created_at, updated_at, expires_at
		) values ($1, 'default', $2, $3, $4, $5, $6, $7, $7, $7, $8, $9, $10, $10, $10, $11)`,
		result.UploadID,
		result.AttachmentID,
		result.DraftID,
		result.AuthorID,
		seed.UploadState,
		transport,
		seed.Source.SizeBytes,
		seed.Source.SHA256[:],
		completionFingerprint[:],
		seed.CreatedAt,
		seed.ExpiresAt,
	); err != nil {
		t.Fatalf("seed attachment processor upload %q: %v", seed.Suffix, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_upload_parts (
			upload_id, part_number, size_bytes, sha256_digest, object_version, created_at
		) values ($1, 1, $2, $3, $4, $5)`,
		result.UploadID,
		seed.Source.SizeBytes,
		seed.Source.SHA256[:],
		seed.Source.ObjectVersion,
		seed.CreatedAt,
	); err != nil {
		t.Fatalf("seed attachment processor upload part %q: %v", seed.Suffix, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_processor_jobs (
			processor_job_id, upload_id, attachment_id, processor_state, processor_profile,
			attempt, max_attempts, owner_id, owner_generation, lease_expires_at,
			retry_at, result_code, result_digest, result_owner_id, result_lease_expires_at,
			created_at, updated_at, expires_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16, $17)`,
		result.ProcessorJobID,
		result.UploadID,
		result.AttachmentID,
		seed.State,
		seed.Profile,
		seed.Attempt,
		seed.MaxAttempts,
		seed.OwnerID,
		seed.OwnerGeneration,
		seed.LeaseExpiresAt,
		seed.RetryAt,
		resultCode,
		resultDigest,
		resultOwnerID,
		resultLeaseExpiresAt,
		seed.CreatedAt,
		seed.ExpiresAt,
	); err != nil {
		t.Fatalf("seed attachment processor job %q: %v", seed.Suffix, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_quota_accounts (project_id, reserved_bytes)
		values ('default', $1)
		on conflict (project_id) do update
		set reserved_bytes = public.attachment_quota_accounts.reserved_bytes + excluded.reserved_bytes`,
		seed.ReservedQuotaBytes,
	); err != nil {
		t.Fatalf("seed attachment processor quota %q: %v", seed.Suffix, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit attachment processor seed %q: %v", seed.Suffix, err)
	}
	return result
}

func attachmentProcessorSeedMetadata(suffix string, profile attachments.ProcessorProfile) (string, string) {
	switch profile {
	case attachments.ProcessorProfileImage:
		return suffix + ".png", "image/png"
	case attachments.ProcessorProfilePDF:
		return suffix + ".pdf", "application/pdf"
	case attachments.ProcessorProfileText:
		return suffix + ".txt", "text/plain"
	case attachments.ProcessorProfileArchive:
		return suffix + ".zip", "application/zip"
	default:
		return suffix + ".bin", "application/octet-stream"
	}
}

func attachmentProcessorTime(value time.Time) *time.Time {
	value = value.UTC().Truncate(time.Microsecond)
	return &value
}

func readAttachmentProcessorPersistenceToken(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	jobID string,
) attachmentProcessorPersistenceToken {
	t.Helper()
	var token attachmentProcessorPersistenceToken
	if err := fixture.db.QueryRow(ctx, `
		select job.xmin::text, job.updated_at::text,
		       upload.xmin::text, upload.updated_at::text,
		       attachment.xmin::text, attachment.updated_at::text,
		       quota.xmin::text, quota.updated_at::text, quota.quota_version,
		       (select coalesce(string_agg(
		          blob.blob_key || ':' || blob.xmin::text, ',' order by blob.blob_key
		       ), '') from public.blob_objects blob)
		from public.attachment_processor_jobs job
		join public.attachment_uploads upload on upload.upload_id = job.upload_id
		join public.record_attachments attachment on attachment.attachment_id = job.attachment_id
		join public.attachment_quota_accounts quota on quota.project_id = attachment.project_id
		where job.processor_job_id = $1`, jobID,
	).Scan(
		&token.JobXID, &token.JobUpdatedAt,
		&token.UploadXID, &token.UploadUpdatedAt,
		&token.AttachmentXID, &token.AttachmentUpdatedAt,
		&token.QuotaXID, &token.QuotaUpdatedAt, &token.QuotaVersion,
		&token.BlobXIDs,
	); err != nil {
		t.Fatalf("read attachment processor persistence token for %q: %v", jobID, err)
	}
	return token
}

func assertAttachmentProcessorPersistedJob(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	jobID string,
	wantState attachments.ProcessorState,
	wantAttempt int64,
	wantGeneration int64,
	wantOwner string,
) {
	t.Helper()
	var state attachments.ProcessorState
	var attempt, generation int64
	var owner string
	var leaseExpiresAt, retryAt *time.Time
	var resultCode *attachments.ProcessorResultCode
	var resultDigest []byte
	var resultOwnerID string
	var resultLeaseExpiresAt *time.Time
	if err := fixture.db.QueryRow(ctx, `
		select processor_state, attempt, owner_generation, owner_id,
		       lease_expires_at, retry_at, result_code, result_digest,
		       result_owner_id, result_lease_expires_at
		from public.attachment_processor_jobs where processor_job_id = $1`, jobID,
	).Scan(
		&state, &attempt, &generation, &owner,
		&leaseExpiresAt, &retryAt, &resultCode, &resultDigest,
		&resultOwnerID, &resultLeaseExpiresAt,
	); err != nil {
		t.Fatalf("read attachment processor job %q: %v", jobID, err)
	}
	if state != wantState || attempt != wantAttempt || generation != wantGeneration || owner != wantOwner {
		t.Fatalf("attachment processor job %q = %q attempt %d generation %d owner %q, want %q/%d/%d/%q",
			jobID, state, attempt, generation, owner, wantState, wantAttempt, wantGeneration, wantOwner)
	}
	if state == attachments.ProcessorStateClaimed &&
		(leaseExpiresAt == nil || retryAt != nil || resultCode != nil || resultDigest != nil ||
			resultOwnerID != "" || resultLeaseExpiresAt != nil) {
		t.Fatalf("claimed processor job %q retained retry/result state: lease %#v retry %#v result %#v/%x token %q/%#v",
			jobID, leaseExpiresAt, retryAt, resultCode, resultDigest, resultOwnerID, resultLeaseExpiresAt)
	}
	if state == attachments.ProcessorStateQueued &&
		(resultCode != nil || resultDigest != nil || resultOwnerID != "" || resultLeaseExpiresAt != nil) {
		t.Fatalf("queued processor job %q retained result state %#v/%x token %q/%#v",
			jobID, resultCode, resultDigest, resultOwnerID, resultLeaseExpiresAt)
	}
	if state == attachments.ProcessorStateRetryWait || state == attachments.ProcessorStateSucceeded ||
		state == attachments.ProcessorStateRejected || state == attachments.ProcessorStateExpired {
		if resultCode == nil || len(resultDigest) != sha256.Size || resultOwnerID == "" || resultLeaseExpiresAt == nil {
			t.Fatalf("completed processor job %q missing result claim token: result %#v/%x token %q/%#v",
				jobID, resultCode, resultDigest, resultOwnerID, resultLeaseExpiresAt)
		}
	}
}

func assertAttachmentProcessorCleanRows(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	seed attachmentProcessorSeedResult,
	claim attachments.ProcessorClaim,
	source attachments.BlobObject,
	preview attachments.BlobObject,
	wantDigest [sha256.Size]byte,
) {
	t.Helper()
	var processorState attachments.ProcessorState
	var uploadState, attachmentState attachments.UploadState
	var resultCode attachments.ProcessorResultCode
	var resultDigest []byte
	var ownerID, resultOwnerID, blobKey, blobVersion, previewKey, previewVersion, previewMediaType string
	var leaseExpiresAt, resultLeaseExpiresAt *time.Time
	var logicalSize, previewSize int64
	if err := fixture.db.QueryRow(ctx, `
		select job.processor_state, job.result_code, job.result_digest,
		       job.owner_id, job.lease_expires_at, job.result_owner_id, job.result_lease_expires_at,
		       upload.upload_state, attachment.attachment_state, attachment.logical_size_bytes,
		       attachment.blob_key, attachment.blob_object_version,
		       attachment.preview_blob_key, attachment.preview_blob_object_version,
		       attachment.preview_media_type, attachment.preview_size_bytes
		from public.attachment_processor_jobs job
		join public.attachment_uploads upload on upload.upload_id = job.upload_id
		join public.record_attachments attachment on attachment.attachment_id = job.attachment_id
		where job.processor_job_id = $1`, seed.ProcessorJobID,
	).Scan(
		&processorState, &resultCode, &resultDigest, &ownerID, &leaseExpiresAt,
		&resultOwnerID, &resultLeaseExpiresAt,
		&uploadState, &attachmentState, &logicalSize, &blobKey, &blobVersion,
		&previewKey, &previewVersion, &previewMediaType, &previewSize,
	); err != nil {
		t.Fatalf("read clean attachment processor rows: %v", err)
	}
	if processorState != attachments.ProcessorStateSucceeded || resultCode != attachments.ProcessorResultCodeClean ||
		!bytes.Equal(resultDigest, wantDigest[:]) || ownerID != "" || leaseExpiresAt != nil ||
		resultOwnerID != claim.OwnerID || resultLeaseExpiresAt == nil ||
		!resultLeaseExpiresAt.Equal(claim.LeaseExpiresAt) ||
		uploadState != attachments.UploadStateAvailable || attachmentState != attachments.UploadStateAvailable ||
		logicalSize != source.SizeBytes || blobKey != source.Key || blobVersion != source.ObjectVersion ||
		previewKey != preview.Key || previewVersion != preview.ObjectVersion ||
		previewMediaType != attachments.ManagedPreviewMediaTypeTextUTF8 || previewSize != preview.SizeBytes {
		t.Fatalf("clean processor persistence = state %q code %q digest %x live %q/%#v result token %q/%#v upload/attachment %q/%q original %s@%s/%d preview %s@%s %q/%d",
			processorState, resultCode, resultDigest, ownerID, leaseExpiresAt, resultOwnerID, resultLeaseExpiresAt,
			uploadState, attachmentState,
			blobKey, blobVersion, logicalSize, previewKey, previewVersion, previewMediaType, previewSize)
	}
	for _, object := range []attachments.BlobObject{source, preview} {
		var digest []byte
		var version string
		var size int64
		var backend attachments.BackendKind
		if err := fixture.db.QueryRow(ctx, `
			select sha256_digest, object_version, size_bytes, backend_kind
			from public.blob_objects where blob_key = $1`, object.Key,
		).Scan(&digest, &version, &size, &backend); err != nil {
			t.Fatalf("read committed processor Blob %q: %v", object.Key, err)
		}
		if !bytes.Equal(digest, object.SHA256[:]) || version != object.ObjectVersion ||
			size != object.SizeBytes || backend != object.BackendKind {
			t.Fatalf("processor Blob %q = %x@%q/%d/%q, want %#v", object.Key, digest, version, size, backend, object)
		}
	}
}

func assertAttachmentProcessorFailureRows(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	seed attachmentProcessorSeedResult,
	claim attachments.ProcessorClaim,
	wantProcessorState attachments.ProcessorState,
	wantUploadState attachments.UploadState,
	wantCode attachments.ProcessorResultCode,
	wantDigest [sha256.Size]byte,
) {
	t.Helper()
	var processorState attachments.ProcessorState
	var uploadState, attachmentState attachments.UploadState
	var resultCode attachments.ProcessorResultCode
	var resultDigest []byte
	var ownerID, resultOwnerID string
	var leaseExpiresAt, resultLeaseExpiresAt *time.Time
	var retryAt *time.Time
	if err := fixture.db.QueryRow(ctx, `
		select job.processor_state, job.result_code, job.result_digest,
		       job.owner_id, job.lease_expires_at, job.retry_at,
		       job.result_owner_id, job.result_lease_expires_at,
		       upload.upload_state, attachment.attachment_state
		from public.attachment_processor_jobs job
		join public.attachment_uploads upload on upload.upload_id = job.upload_id
		join public.record_attachments attachment on attachment.attachment_id = job.attachment_id
		where job.processor_job_id = $1`, seed.ProcessorJobID,
	).Scan(
		&processorState, &resultCode, &resultDigest, &ownerID, &leaseExpiresAt, &retryAt,
		&resultOwnerID, &resultLeaseExpiresAt,
		&uploadState, &attachmentState,
	); err != nil {
		t.Fatalf("read failed attachment processor rows: %v", err)
	}
	if processorState != wantProcessorState || resultCode != wantCode ||
		!bytes.Equal(resultDigest, wantDigest[:]) || ownerID != "" || leaseExpiresAt != nil ||
		resultOwnerID != claim.OwnerID || resultLeaseExpiresAt == nil ||
		!resultLeaseExpiresAt.Equal(claim.LeaseExpiresAt) ||
		uploadState != wantUploadState || attachmentState != wantUploadState {
		t.Fatalf("failed processor persistence = state %q code %q digest %x live %q/%#v result token %q/%#v retry %#v upload/attachment %q/%q",
			processorState, resultCode, resultDigest, ownerID, leaseExpiresAt,
			resultOwnerID, resultLeaseExpiresAt, retryAt, uploadState, attachmentState)
	}
	if wantProcessorState == attachments.ProcessorStateRetryWait && retryAt == nil {
		t.Fatal("retry_wait processor result did not persist caller retry_at")
	}
	if wantProcessorState != attachments.ProcessorStateRetryWait && retryAt != nil {
		t.Fatalf("terminal processor result retained retry_at %s", *retryAt)
	}
	var sourceBlobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select count(*) from public.blob_objects where blob_key = $1`, seed.Source.Key,
	).Scan(&sourceBlobCount); err != nil {
		t.Fatalf("count failed processor source Blob %q: %v", seed.Source.Key, err)
	}
	if sourceBlobCount != 0 {
		t.Fatalf("failed processor result persisted source Blob %q", seed.Source.Key)
	}
}

func assertAttachmentProcessorBoundedExpiryRows(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	seed attachmentProcessorSeedResult,
	wantCode attachments.ProcessorResultCode,
	wantDigest [sha256.Size]byte,
	wantResultOwner string,
	wantResultLeaseExpiry time.Time,
) {
	t.Helper()
	var processorState attachments.ProcessorState
	var uploadState, attachmentState attachments.UploadState
	var resultCode attachments.ProcessorResultCode
	var resultDigest []byte
	var ownerID, resultOwnerID string
	var leaseExpiresAt, retryAt *time.Time
	var resultLeaseExpiresAt time.Time
	var blobKey, previewBlobKey *string
	if err := fixture.db.QueryRow(ctx, `
		select job.processor_state, job.result_code, job.result_digest,
		       job.owner_id, job.lease_expires_at, job.retry_at,
		       job.result_owner_id, job.result_lease_expires_at,
		       upload.upload_state, attachment.attachment_state,
		       attachment.blob_key, attachment.preview_blob_key
		from public.attachment_processor_jobs job
		join public.attachment_uploads upload on upload.upload_id = job.upload_id
		join public.record_attachments attachment on attachment.attachment_id = job.attachment_id
		where job.processor_job_id = $1`, seed.ProcessorJobID,
	).Scan(
		&processorState, &resultCode, &resultDigest,
		&ownerID, &leaseExpiresAt, &retryAt,
		&resultOwnerID, &resultLeaseExpiresAt,
		&uploadState, &attachmentState, &blobKey, &previewBlobKey,
	); err != nil {
		t.Fatalf("read bounded attachment processor expiry rows: %v", err)
	}
	if processorState != attachments.ProcessorStateExpired ||
		resultCode != wantCode || !bytes.Equal(resultDigest, wantDigest[:]) ||
		ownerID != "" || leaseExpiresAt != nil || retryAt != nil ||
		resultOwnerID != wantResultOwner || !resultLeaseExpiresAt.Equal(wantResultLeaseExpiry) ||
		uploadState != attachments.UploadStateExpired || attachmentState != attachments.UploadStateExpired ||
		blobKey != nil || previewBlobKey != nil ||
		uploadState == attachments.UploadStateAvailable || attachmentState == attachments.UploadStateAvailable {
		t.Fatalf("bounded processor expiry persistence = state %q code %q digest %x live %q/%#v retry %#v result token %q/%s upload/attachment %q/%q blobs %#v/%#v",
			processorState, resultCode, resultDigest, ownerID, leaseExpiresAt, retryAt,
			resultOwnerID, resultLeaseExpiresAt, uploadState, attachmentState, blobKey, previewBlobKey)
	}
	var usage attachments.QuotaUsage
	var quotaVersion, blobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select quota.logical_bytes, quota.reserved_bytes, quota.physical_bytes, quota.quota_version,
		       (select count(*) from public.blob_objects)
		from public.attachment_quota_accounts quota where quota.project_id = 'default'`,
	).Scan(&usage.LogicalBytes, &usage.ReservedBytes, &usage.PhysicalBytes, &quotaVersion, &blobCount); err != nil {
		t.Fatalf("read bounded attachment processor expiry quota: %v", err)
	}
	if usage != (attachments.QuotaUsage{}) || quotaVersion != 1 || blobCount != 0 {
		t.Fatalf("bounded processor expiry quota/blob = %#v version %d blobs %d, want zero/1/0",
			usage, quotaVersion, blobCount)
	}
}

func assertAttachmentProcessorFailedCompletionRolledBack(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	seed attachmentProcessorSeedResult,
	claim attachments.ProcessorClaim,
	wantBlobCount int64,
	wantUsage attachments.QuotaUsage,
	wantQuotaVersion int64,
) {
	t.Helper()
	var processorState attachments.ProcessorState
	var attempt, generation int64
	var ownerID string
	var leaseExpiresAt time.Time
	var resultCode *attachments.ProcessorResultCode
	var resultDigest []byte
	var resultOwnerID string
	var resultLeaseExpiresAt *time.Time
	var uploadState, attachmentState attachments.UploadState
	var blobKey, previewKey *string
	if err := fixture.db.QueryRow(ctx, `
		select job.processor_state, job.attempt, job.owner_id, job.owner_generation,
		       job.lease_expires_at, job.result_code, job.result_digest,
		       job.result_owner_id, job.result_lease_expires_at,
		       upload.upload_state, attachment.attachment_state,
		       attachment.blob_key, attachment.preview_blob_key
		from public.attachment_processor_jobs job
		join public.attachment_uploads upload on upload.upload_id = job.upload_id
		join public.record_attachments attachment on attachment.attachment_id = job.attachment_id
		where job.processor_job_id = $1`, seed.ProcessorJobID,
	).Scan(
		&processorState, &attempt, &ownerID, &generation, &leaseExpiresAt, &resultCode, &resultDigest,
		&resultOwnerID, &resultLeaseExpiresAt,
		&uploadState, &attachmentState, &blobKey, &previewKey,
	); err != nil {
		t.Fatalf("read rolled-back processor completion: %v", err)
	}
	if processorState != attachments.ProcessorStateClaimed || attempt != claim.Attempt || ownerID != claim.OwnerID ||
		generation != claim.OwnerGeneration || !leaseExpiresAt.Equal(claim.LeaseExpiresAt) ||
		resultCode != nil || resultDigest != nil || resultOwnerID != "" || resultLeaseExpiresAt != nil ||
		uploadState != attachments.UploadStateQuarantined ||
		attachmentState != attachments.UploadStateQuarantined || blobKey != nil || previewKey != nil {
		t.Fatalf("failed processor completion did not roll back: state %q attempt %d owner %q/%d lease %s result %#v/%x token %q/%#v upload/attachment %q/%q blobs %#v/%#v",
			processorState, attempt, ownerID, generation, leaseExpiresAt, resultCode, resultDigest,
			resultOwnerID, resultLeaseExpiresAt,
			uploadState, attachmentState, blobKey, previewKey)
	}
	var usage attachments.QuotaUsage
	var quotaVersion, blobCount int64
	if err := fixture.db.QueryRow(ctx, `
		select quota.logical_bytes, quota.reserved_bytes, quota.physical_bytes, quota.quota_version,
		       (select count(*) from public.blob_objects)
		from public.attachment_quota_accounts quota where quota.project_id = 'default'`,
	).Scan(&usage.LogicalBytes, &usage.ReservedBytes, &usage.PhysicalBytes, &quotaVersion, &blobCount); err != nil {
		t.Fatalf("read rolled-back processor quota: %v", err)
	}
	if usage != wantUsage || quotaVersion != wantQuotaVersion || blobCount != wantBlobCount {
		t.Fatalf("rolled-back processor quota/blob = %#v version %d blobs %d, want %#v/%d/%d",
			usage, quotaVersion, blobCount, wantUsage, wantQuotaVersion, wantBlobCount)
	}
}

func (fixture recordPlatformPostgresFixture) installAttachmentProcessorClaimBlocker(
	t *testing.T,
	ctx context.Context,
) {
	t.Helper()
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "attachment-processor-claim-trigger", 1)
	for _, definition := range []string{
		`create function public.attachment_processor_test_block_claim_v1()
			returns trigger
			language plpgsql
			as $$
			begin
				perform pg_catalog.pg_advisory_xact_lock(` + fmt.Sprint(attachmentProcessorClaimBlockLock) + `);
				return new;
			end;
			$$`,
		`create trigger attachment_processor_test_block_claim_v1
			before update on public.attachment_processor_jobs
			for each row
			when (new.processor_state = 'claimed')
			execute function public.attachment_processor_test_block_claim_v1()`,
		`grant execute on function public.attachment_processor_test_block_claim_v1() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := migratorPool.Exec(ctx, definition); err != nil {
			t.Fatalf("install attachment processor claim blocker: %v", err)
		}
	}
}

func (fixture recordPlatformPostgresFixture) installAttachmentProcessorFailureTrigger(
	t *testing.T,
	ctx context.Context,
	table string,
) {
	t.Helper()
	var triggerName string
	switch table {
	case "record_attachments":
		triggerName = "attachment_processor_test_fail_attachment_v1"
	case "attachment_quota_accounts":
		triggerName = "attachment_processor_test_fail_quota_v1"
	default:
		t.Fatalf("unsupported attachment processor failure table %q", table)
	}
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "attachment-processor-failure-trigger", 1)
	functionName := pgx.Identifier{triggerName}.Sanitize()
	tableName := pgx.Identifier{table}.Sanitize()
	for _, definition := range []string{
		`create function public.` + functionName + `()
			returns trigger
			language plpgsql
			as $$
			begin
				raise exception 'injected attachment processor write cutpoint';
			end;
			$$`,
		`create trigger ` + functionName + `
			before update on public.` + tableName + `
			for each row execute function public.` + functionName + `()`,
		`grant execute on function public.` + functionName + `() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := migratorPool.Exec(ctx, definition); err != nil {
			t.Fatalf("install attachment processor failure trigger on %s: %v", table, err)
		}
	}
}

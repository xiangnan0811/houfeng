package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
)

// These fixtures keep the actual registration, physical write, publication and
// processor claim contracts while allowing quota tests to choose their limits.
func putManagedAttachmentUpload(t *testing.T, ctx context.Context, repository *PostgresAttachmentRepository, writer attachments.BlobWriter, reserve attachments.ReserveUploadCommand, content []byte) attachments.WrittenBlob {
	t.Helper()
	if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	intent, err := repository.PrepareBlobPublication(ctx, attachments.BlobPublicationPrepareRequest{
		ProjectID: reserve.ProjectID, NamespaceID: writer.NamespaceID(), OwnerKind: attachments.BlobPublicationOwnerUpload,
		OwnerID: reserve.UploadID, OwnerGeneration: 1, PublishExpiresAt: reserve.ExpiresAt,
		Target: attachments.BlobPublicationTarget{Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest, SizeBytes: int64(len(content)), BackendKind: attachments.BackendKindLocal},
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: reserve.ProjectID, Origin: attachments.WriteOrigin{
		Kind: "upload", ID: reserve.UploadID, Generation: 1, DraftID: &reserve.DraftID, PublicationID: &intent.PublicationID,
	}, Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	intent, err = repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: intent, Object: written.Version, ObligationID: written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID, Object: written.Version, PublicationIntent: intent}); err != nil {
		t.Fatal(err)
	}
	return written
}

func enqueueManagedAttachmentUpload(t *testing.T, ctx context.Context, repository *PostgresAttachmentRepository, reserve attachments.ReserveUploadCommand, written attachments.WrittenBlob, jobID string) {
	t.Helper()
	command := attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		ActualSizeBytes: written.Version.SizeBytes, ActualSHA256: written.Version.SHA256, Object: written.Version,
		CompletionFingerprint: written.Version.SHA256, ProcessorJobID: jobID,
		ProcessorProfile: attachments.ProcessorProfileText, ProcessorMaxAttempts: 3, ProcessorExpiresAt: reserve.ExpiresAt,
	}
	result, err := repository.CompleteUploadAndEnqueue(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := repository.CompleteUploadAndEnqueue(ctx, command)
	if err != nil || replay != result {
		t.Fatalf("managed upload completion replay: %#v %v; first=%#v", replay, err, result)
	}
}

func completeManagedAttachmentProcessor(t *testing.T, ctx context.Context, repository *PostgresAttachmentRepository, writer attachments.BlobWriter, uploadID string, content []byte, limits attachments.Limits) attachments.ProcessorCompletionResult {
	t.Helper()
	claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "managed_fixture_processor", OwnerLeaseDuration: 5 * time.Minute})
	if err != nil || claim == nil || claim.UploadID != uploadID {
		t.Fatalf("expected real processor claim for %s: %#v %v", uploadID, claim, err)
	}
	preview, intent := prepareManagedAttachmentPreview(t, ctx, repository, writer, *claim, content)
	input := attachments.ProcessorCompletionInput{Claim: *claim, Limits: limits, PreviewPublicationIntent: intent,
		Result: attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean, HasPreview: true, Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}}
	result, err := repository.CompleteProcessorJob(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := repository.CompleteProcessorJob(ctx, input)
	if err != nil || replay != result {
		t.Fatalf("managed processor completion replay: %#v %v; first=%#v", replay, err, result)
	}
	return result
}

func prepareManagedAttachmentPreview(t *testing.T, ctx context.Context, repository *PostgresAttachmentRepository, writer attachments.BlobWriter, claim attachments.ProcessorClaim, content []byte) (attachments.BlobObject, attachments.BlobPublicationIntent) {
	t.Helper()
	digest := sha256.Sum256(content)
	intent, err := repository.PrepareBlobPublication(ctx, attachments.BlobPublicationPrepareRequest{
		ProjectID: claim.ProjectID, NamespaceID: writer.NamespaceID(), OwnerKind: attachments.BlobPublicationOwnerProcessorPreview,
		OwnerID: claim.ProcessorJobID, OwnerGeneration: claim.OwnerGeneration, ClaimOwnerID: claim.OwnerID, ClaimExpiresAt: claim.LeaseExpiresAt,
		PublishExpiresAt: claim.ExpiresAt, Target: attachments.BlobPublicationTarget{Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest, SizeBytes: int64(len(content)), BackendKind: attachments.BackendKindLocal},
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: claim.ProjectID, Origin: attachments.WriteOrigin{
		Kind: "processor_preview", ID: claim.ProcessorJobID, Generation: claim.OwnerGeneration, OwnerID: &claim.OwnerID,
		OwnerGeneration: &claim.OwnerGeneration, OwnerExpiresAt: &claim.LeaseExpiresAt, DraftID: &claim.DraftID, PublicationID: &intent.PublicationID,
	}, Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	intent, err = repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: intent, Object: written.Version, ObligationID: written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	return attachments.BlobObject{Key: written.Version.Key, SHA256: written.Version.SHA256, ObjectVersion: written.Version.VersionID, SizeBytes: written.Version.SizeBytes, BackendKind: attachments.BackendKindLocal, NamespaceID: written.NamespaceID}, intent
}

func seedManagedAttachmentProcessor(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, repository *PostgresAttachmentRepository, writer attachments.BlobWriter, suffix string, content []byte) attachmentProcessorSeedResult {
	t.Helper()
	reserve := draftReleaseReservation("rdf_"+suffix, 0)
	reserve.DeclaredSizeBytes = int64(len(content))
	seedAttachmentDraft(t, ctx, fixture, reserve.DraftID, reserve.AuthorID)
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatal(err)
	}
	written := putManagedAttachmentUpload(t, ctx, repository, writer, reserve, content)
	jobID := "apj_" + suffix
	enqueueManagedAttachmentUpload(t, ctx, repository, reserve, written, jobID)
	source := attachments.BlobObject{Key: written.Version.Key, SHA256: written.Version.SHA256, ObjectVersion: written.Version.VersionID, SizeBytes: written.Version.SizeBytes, BackendKind: attachments.BackendKindLocal, NamespaceID: written.NamespaceID}
	return attachmentProcessorSeedResult{ProcessorJobID: jobID, UploadID: reserve.UploadID, AttachmentID: reserve.AttachmentID, DisplayName: reserve.DisplayName, DeclaredMediaType: reserve.MediaType, DraftID: reserve.DraftID, AuthorID: reserve.AuthorID, Source: source, Profile: attachments.ProcessorProfileText, MaxAttempts: 3, ExpiresAt: reserve.ExpiresAt}
}

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

func localPreviewRequest(claim attachments.ProcessorClaim, namespace string, content []byte) attachments.BlobPublicationPrepareRequest {
	digest := sha256.Sum256(content)
	return attachments.BlobPublicationPrepareRequest{
		ProjectID: claim.ProjectID, NamespaceID: namespace, OwnerKind: attachments.BlobPublicationOwnerProcessorPreview,
		OwnerID: claim.ProcessorJobID, OwnerGeneration: claim.OwnerGeneration, ClaimOwnerID: claim.OwnerID,
		ClaimExpiresAt: claim.LeaseExpiresAt, PublishExpiresAt: claim.ExpiresAt,
		Target: attachments.BlobPublicationTarget{Key: fmt.Sprintf("sha256/%x", digest), SHA256: digest, SizeBytes: int64(len(content)), BackendKind: attachments.BackendKindLocal},
	}
}

func TestPostgresIntegrationLocalPublicationContract(t *testing.T) {
	for _, state := range []string{"completed_upload", "released_processor", "finished_processor"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			f := newLocalPublicationFixture(t, ctx)
			writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
			if err != nil {
				t.Fatal(err)
			}
			drafts := newRecordsPostgresDraftRepository(f.pool)
			draft := createRecordsPostgresDraft(t, ctx, drafts, "rdf_contractowner", recordsPostgresDraftPayload(t, "owner lifecycle"))
			seedDraftReleaseUpload(t, ctx, f.repository, writer, draft.DraftID, 0, draftReleaseQuarantined)
			reserve := draftReleaseReservation(draft.DraftID, 0)
			content := []byte("new owner publication")
			var request attachments.BlobPublicationPrepareRequest
			var expected error
			if state == "completed_upload" {
				request = attachmentPublicationCrashPrepareRequest(content, reserve.UploadID, attachments.BackendKindLocal)
				request.NamespaceID = writer.NamespaceID()
				expected = attachments.ErrAttachmentConflict
			} else {
				claim, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "contract_processor", OwnerLeaseDuration: time.Minute})
				if err != nil || claim == nil {
					t.Fatalf("claim processor: %#v %v", claim, err)
				}
				request = localPreviewRequest(*claim, writer.NamespaceID(), content)
				expected = attachments.ErrProcessorClaimLost
				if state == "released_processor" {
					if _, err := f.fixture.db.Exec(ctx, `update public.attachment_processor_jobs set lease_expires_at=clock_timestamp()-interval '1 second' where processor_job_id=$1`, claim.ProcessorJobID); err != nil {
						t.Fatal(err)
					}
					if err := drafts.DeleteDraft(ctx, records.DraftDeleteCommand{DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded}); err != nil {
						t.Fatal(err)
					}
				} else {
					preview, intent := prepareManagedAttachmentPreview(t, ctx, f.repository, writer, *claim, []byte("finished processor preview"))
					result := attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean, HasPreview: true, Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}
					if _, err := f.repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{Claim: *claim, Result: result, PreviewPublicationIntent: intent, Limits: attachments.DefaultLimits()}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := f.repository.PrepareBlobPublication(ctx, request); !errors.Is(err, expected) {
				t.Fatalf("nonwritable owner error = %v, want %v", err, expected)
			}
			var count int
			if err := f.fixture.db.QueryRow(ctx, `select count(*) from public.blob_publication_intents where blob_key=$1`, request.Target.Key).Scan(&count); err != nil || count != 0 {
				t.Fatalf("nonwritable owner created %d intents: %v", count, err)
			}
		})
	}

	t.Run("published_replay_and_drift", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		content := []byte("exact published replay")
		a := f.prepare(t, ctx, "aup_contractreplay", content)
		prepared := a.intent
		f.put(t, ctx, &a, content)
		f.bind(t, ctx, &a)
		if got, err := f.repository.PrepareBlobPublication(ctx, a.request); err != nil || got != a.intent {
			t.Fatalf("published prepare replay: %#v %v", got, err)
		}
		versionRequest := attachments.BlobPublicationVersionRequest{Intent: prepared, Object: a.written.Version, ObligationID: a.written.ObligationID}
		if got, err := f.repository.RecordBlobPublicationVersion(ctx, versionRequest); err != nil || got != a.intent {
			t.Fatalf("published version replay: %#v %v", got, err)
		}
		var before string
		if err := f.fixture.db.QueryRow(ctx, `select to_jsonb(p)::text from public.blob_publication_intents p where publication_id=$1`, a.intent.PublicationID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name   string
			mutate func(*attachments.BlobPublicationPrepareRequest)
		}{
			{"size", func(r *attachments.BlobPublicationPrepareRequest) { r.Target.SizeBytes++ }},
			{"backend", func(r *attachments.BlobPublicationPrepareRequest) {
				r.Target.BackendKind = attachments.BackendKindS3
				r.NamespaceID = ""
			}},
			{"expiry", func(r *attachments.BlobPublicationPrepareRequest) {
				r.PublishExpiresAt = r.PublishExpiresAt.Add(time.Minute)
			}},
		} {
			t.Run(test.name, func(t *testing.T) {
				request := a.request
				test.mutate(&request)
				if err := request.Validate(); err != nil {
					t.Fatalf("invalid drift fixture: %v", err)
				}
				if _, err := f.repository.PrepareBlobPublication(ctx, request); !errors.Is(err, attachments.ErrBlobPublicationConflict) {
					t.Fatalf("prepare drift = %v", err)
				}
			})
		}
		for _, test := range []struct {
			name   string
			mutate func(*attachments.BlobPublicationVersionRequest)
		}{
			{"version", func(r *attachments.BlobPublicationVersionRequest) {
				r.Object.VersionID = "different-valid-object-version"
			}},
			{"owner", func(r *attachments.BlobPublicationVersionRequest) { r.Intent.OwnerID = "aup_contractother" }},
			{"size", func(r *attachments.BlobPublicationVersionRequest) { r.Intent.Target.SizeBytes++; r.Object.SizeBytes++ }},
			{"backend", func(r *attachments.BlobPublicationVersionRequest) {
				r.Intent.Target.BackendKind = attachments.BackendKindS3
				r.Intent.NamespaceID = ""
			}},
			{"expiry", func(r *attachments.BlobPublicationVersionRequest) {
				r.Intent.PublishExpiresAt = r.Intent.PublishExpiresAt.Add(time.Minute)
			}},
			{"digest", func(r *attachments.BlobPublicationVersionRequest) {
				digest := sha256.Sum256([]byte("another valid publication target"))
				key := fmt.Sprintf("sha256/%x", digest)
				r.Intent.Target.Key, r.Intent.Target.SHA256 = key, digest
				r.Object.Key, r.Object.SHA256 = key, digest
			}},
		} {
			t.Run("version_"+test.name, func(t *testing.T) {
				drift := versionRequest
				test.mutate(&drift)
				if err := drift.Validate(); err != nil {
					t.Fatalf("invalid version drift fixture: %v", err)
				}
				if _, err := f.repository.RecordBlobPublicationVersion(ctx, drift); !errors.Is(err, attachments.ErrBlobPublicationConflict) {
					t.Fatalf("version identity drift = %v", err)
				}
			})
		}
		var after string
		if err := f.fixture.db.QueryRow(ctx, `select to_jsonb(p)::text from public.blob_publication_intents p where publication_id=$1`, a.intent.PublicationID).Scan(&after); err != nil || before != after {
			t.Fatalf("drift changed publication: %v", err)
		}
		actual, err := readAttachmentPublicationCrashBytes(ctx, f.blob, a.written.Version)
		if err != nil || !bytes.Equal(actual, content) {
			t.Fatalf("replay changed bytes: %q %v", actual, err)
		}
	})

	t.Run("prepared_preview_takeover", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		seed := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "contracttakeover", []byte("takeover source"))
		first, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "contract_old", OwnerLeaseDuration: time.Minute})
		if err != nil || first == nil {
			t.Fatalf("first claim: %#v %v", first, err)
		}
		oldRequest := localPreviewRequest(*first, writer.NamespaceID(), []byte("takeover prepared preview"))
		old, err := f.repository.PrepareBlobPublication(ctx, oldRequest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.fixture.db.Exec(ctx, `update public.attachment_processor_jobs set lease_expires_at=clock_timestamp()-interval '1 second' where processor_job_id=$1`, seed.ProcessorJobID); err != nil {
			t.Fatal(err)
		}
		second, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "contract_new", OwnerLeaseDuration: time.Minute})
		if err != nil || second == nil || second.OwnerGeneration <= first.OwnerGeneration {
			t.Fatalf("takeover claim: %#v %v", second, err)
		}
		request := localPreviewRequest(*second, writer.NamespaceID(), []byte("takeover prepared preview"))
		rebound, err := f.repository.PrepareBlobPublication(ctx, request)
		if err != nil || rebound.PublicationID != old.PublicationID || rebound.State != attachments.BlobPublicationStatePrepared || rebound.OwnerGeneration != second.OwnerGeneration || rebound.ObjectVersion != "" || rebound.ObligationID != "" {
			t.Fatalf("prepared takeover: %#v %v", rebound, err)
		}
		if _, err := f.repository.PrepareBlobPublication(ctx, oldRequest); err == nil {
			t.Fatal("stale processor prepared after takeover")
		}
		var count int
		if err := f.fixture.db.QueryRow(ctx, `select count(*) from public.blob_publication_intents where blob_key=$1`, request.Target.Key).Scan(&count); err != nil || count != 1 {
			t.Fatalf("takeover duplicated intent: %d %v", count, err)
		}
	})

	t.Run("active_cleanup_rejects_prepare_and_late_version", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		content := []byte("claimed cleanup publication")
		a := f.prepare(t, ctx, "aup_contractclaimed", content)
		prepared := a.intent
		f.put(t, ctx, &a, content)
		other := prepareAttachmentPublicationCrashUpload(t, ctx, f.fixture, f.repository, attachmentPublicationCrashHarness{backendKind: attachments.BackendKindLocal, transportKind: attachments.TransportKindLocal}, "aup_contractlate", content)
		late := attachmentPublicationCrashPrepareRequest(content, other.reserve.UploadID, attachments.BackendKindLocal)
		late.NamespaceID = f.blob.Namespace().NamespaceID
		f.expire(t, ctx, a)
		cleanup := f.discover(t, ctx, a, "contract_cleanup")
		interruption := &localPublicationInterruption{PostgresLocalBlobRepository: f.local, beforeBinding: true}
		coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, interruption)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := coordinator.Cleanup(ctx, cleanup, "contract_cleanup"); !errors.Is(err, errLocalPublicationInterrupted) || !interruption.blocked {
			t.Fatalf("claim boundary: %v", err)
		}
		var before, after string
		if err := f.fixture.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, cleanup.DeletionID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repository.PrepareBlobPublication(ctx, late); !errors.Is(err, attachments.ErrBlobGCProtected) {
			t.Fatalf("prepare during cleanup = %v, want ErrBlobGCProtected", err)
		}
		if _, err := f.repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: prepared, Object: a.written.Version, ObligationID: a.written.ObligationID}); !errors.Is(err, attachments.ErrBlobPublicationConflict) {
			t.Fatalf("late version during cleanup = %v", err)
		}
		var count int
		if err := f.fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,(select count(*) from public.blob_publication_intents where owner_id=$2) from public.blob_gc_deletions g where deletion_id=$1`, cleanup.DeletionID, late.OwnerID).Scan(&after, &count); err != nil || before != after || count != 0 {
			t.Fatalf("late publisher changed claim or created intent: %d %v", count, err)
		}
		actual, err := readAttachmentPublicationCrashBytes(ctx, f.blob, a.written.Version)
		if err != nil || !bytes.Equal(actual, content) {
			t.Fatalf("late publication changed bytes: %q %v", actual, err)
		}
	})

	t.Run("processor_preview_consumption_rollback", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		seed := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "contractpreview", []byte("source for preview rollback"))
		claim, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "contract_preview", OwnerLeaseDuration: time.Minute})
		if err != nil || claim == nil {
			t.Fatalf("claim processor: %#v %v", claim, err)
		}
		previewContent := []byte("valid size preview")
		preview, intent := prepareManagedAttachmentPreview(t, ctx, f.repository, writer, *claim, previewContent)
		input := attachments.ProcessorCompletionInput{Claim: *claim, Limits: attachments.DefaultLimits(), PreviewPublicationIntent: intent,
			Result: attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean, HasPreview: true, Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}}
		snapshot := func() string {
			t.Helper()
			var got string
			if err := f.fixture.db.QueryRow(ctx, `select jsonb_build_object(
				'job',(select to_jsonb(j) from public.attachment_processor_jobs j where processor_job_id=$1),
				'upload',(select to_jsonb(u) from public.attachment_uploads u where upload_id=$2),
				'attachment',(select to_jsonb(a) from public.record_attachments a where attachment_id=$3),
				'quota',(select to_jsonb(q) from public.attachment_quota_accounts q where project_id='default'),
				'blobs',(select coalesce(jsonb_agg(to_jsonb(b) order by blob_key,object_version),'[]'::jsonb) from public.blob_objects b),
				'intent',(select to_jsonb(p) from public.blob_publication_intents p where publication_id=$4))::text`, seed.ProcessorJobID, seed.UploadID, seed.AttachmentID, intent.PublicationID).Scan(&got); err != nil {
				t.Fatal(err)
			}
			return got
		}
		before := snapshot()
		missing := input
		missing.PreviewPublicationIntent = attachments.BlobPublicationIntent{}
		if _, err := f.repository.CompleteProcessorJob(ctx, missing); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
			t.Fatalf("missing preview intent = %v", err)
		}
		if after := snapshot(); after != before {
			t.Fatal("missing preview intent changed durable state")
		}
		if _, err := f.fixture.db.Exec(ctx, `create function public.reject_preview_contract_consume() returns trigger language plpgsql as $$ begin raise exception 'preview contract consume failure'; end $$; create trigger reject_preview_contract_consume before update on public.blob_publication_intents for each row when (new.publication_state='completed') execute function public.reject_preview_contract_consume()`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repository.CompleteProcessorJob(ctx, input); err == nil || !strings.Contains(err.Error(), "preview contract consume failure") {
			t.Fatalf("preview consume trigger not reached: %v", err)
		}
		if after := snapshot(); after != before {
			t.Fatalf("failed preview consume changed durable state:\nbefore %s\nafter %s", before, after)
		}
		if _, err := f.fixture.db.Exec(ctx, `drop trigger reject_preview_contract_consume on public.blob_publication_intents; drop function public.reject_preview_contract_consume()`); err != nil {
			t.Fatal(err)
		}
		result, err := f.repository.CompleteProcessorJob(ctx, input)
		if err != nil || result.UploadState != attachments.UploadStateAvailable {
			t.Fatalf("preview completion retry: %#v %v", result, err)
		}
		replay, err := f.repository.CompleteProcessorJob(ctx, input)
		if err != nil || replay != result {
			t.Fatalf("preview completion replay: %#v %v", replay, err)
		}
		object := attachments.ObjectVersion{Key: preview.Key, VersionID: preview.ObjectVersion, SHA256: preview.SHA256, SizeBytes: preview.SizeBytes}
		actual, err := readAttachmentPublicationCrashBytes(ctx, f.blob, object)
		if err != nil || !bytes.Equal(actual, previewContent) {
			t.Fatalf("preview bytes changed: %q %v", actual, err)
		}
	})
}

func TestPostgresMinIOIntegrationPublicationCleanupContract(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("real PostgreSQL and MinIO integration are required")
	}
	for _, scenario := range []string{"unresolved_retry", "exact_version_and_completion", "referenced_legacy_publication"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRecordsPostgresFixture(t, ctx)
			pool := fixture.openDirectRuntimePool(t, ctx, "s3-publication-contract", 2)
			repository := NewPostgresAttachmentRepository(pool)
			client, bucket := newAttachmentUploadWorkflowMinIO(t)
			blob, err := attachments.NewS3BlobStore(client, bucket)
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("physical s3 cleanup " + scenario)
			owner := "aup_s3contract" + strings.ReplaceAll(scenario, "_", "")
			harness := attachmentPublicationCrashHarness{backendKind: attachments.BackendKindS3, transportKind: attachments.TransportKindS3}
			upload := prepareS3AttachmentPublicationCrashUpload(t, ctx, fixture, repository, harness, owner, content)
			request := attachmentPublicationCrashPrepareRequest(content, owner, attachments.BackendKindS3)
			intent, err := repository.PrepareBlobPublication(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			var written attachments.WrittenBlob
			if scenario != "unresolved_retry" {
				written = putManagedAttachmentPublicationCrash(t, ctx, fixture, blob, request, intent, upload, content)
				actual, err := readAttachmentPublicationCrashBytes(ctx, blob, written.Version)
				if err != nil || !bytes.Equal(actual, content) {
					t.Fatalf("physical S3 seed: %q %v", actual, err)
				}
			}
			if scenario == "referenced_legacy_publication" {
				published, err := repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: intent, Object: written.Version, ObligationID: written.ObligationID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{ProjectID: "default", UploadID: upload.reserve.UploadID, AuthorID: upload.reserve.AuthorID, TemporaryObjectKey: upload.temporaryKey, Object: written.Version, PublicationIntent: published}); err != nil {
					t.Fatal(err)
				}
				// Model a legacy publication whose real metadata commit survived while the
				// publication completion did not; the exact S3 bytes and part are real.
				if _, err := fixture.db.Exec(ctx, `update public.blob_publication_intents set publication_state='published',completion_outcome=null,receipt_digest=null,completed_at=null,publish_expires_at=clock_timestamp()-interval '1 minute' where publication_id=$1`, intent.PublicationID); err != nil {
					t.Fatal(err)
				}
				claim, err := repository.ClaimBlobPublicationCleanup(ctx, attachments.BlobPublicationCleanupClaimRequest{ProjectID: "default", BackendKind: attachments.BackendKindS3, CleanupOwnerID: "s3_contract_reference", OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration})
				if err != nil || claim != nil {
					t.Fatalf("referenced legacy publication claim: %#v %v", claim, err)
				}
				row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
				assertAttachmentPublicationCrashTerminal(t, row, attachments.BlobPublicationCompletionOutcomeConsumed, &written.Version.VersionID)
				assertAttachmentPublicationCrashReference(t, ctx, fixture, upload.reserve.UploadID, written.Version)
				actual, err := readAttachmentPublicationCrashBytes(ctx, blob, written.Version)
				if err != nil || !bytes.Equal(actual, content) {
					t.Fatalf("referenced S3 bytes lost: %q %v", actual, err)
				}
				return
			}
			expireAttachmentPublicationCrashIntent(t, ctx, fixture, intent.PublicationID)
			claimRequest := attachments.BlobPublicationCleanupClaimRequest{ProjectID: "default", BackendKind: attachments.BackendKindS3, CleanupOwnerID: "s3_contract_first", OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration}
			claim, err := repository.ClaimBlobPublicationCleanup(ctx, claimRequest)
			if err != nil || claim == nil || claim.Intent.PublicationID != intent.PublicationID || claim.Intent.ObjectVersion != "" {
				t.Fatalf("unresolved cleanup claim: %#v %v", claim, err)
			}
			mutations := []struct {
				name   string
				mutate func(*attachments.BlobPublicationCleanupClaim)
			}{
				{"owner", func(c *attachments.BlobPublicationCleanupClaim) { c.CleanupOwnerID = "s3_contract_stale" }},
				{"generation", func(c *attachments.BlobPublicationCleanupClaim) { c.CleanupGeneration++ }},
				{"attempt", func(c *attachments.BlobPublicationCleanupClaim) { c.Attempt++ }},
				{"lease", func(c *attachments.BlobPublicationCleanupClaim) {
					c.ObservedLeaseExpiresAt = c.ObservedLeaseExpiresAt.Add(time.Second)
				}},
			}
			if scenario == "unresolved_retry" {
				if _, err := blob.ResolveBlobPublicationObject(ctx, request.Target); !errors.Is(err, attachments.ErrBlobNotFound) {
					t.Fatalf("expected physical absence: %v", err)
				}
				retryAt := time.Now().UTC().Add(time.Minute)
				for _, mutation := range mutations {
					stale := *claim
					mutation.mutate(&stale)
					if err := repository.RetryBlobPublicationCleanup(ctx, attachments.BlobPublicationCleanupRetryRequest{Claim: stale, RetryAt: retryAt}); !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
						t.Fatalf("stale %s retry: %v", mutation.name, err)
					}
				}
				retry := attachments.BlobPublicationCleanupRetryRequest{Claim: *claim, RetryAt: retryAt}
				if err := repository.RetryBlobPublicationCleanup(ctx, retry); err != nil {
					t.Fatal(err)
				}
				if err := repository.RetryBlobPublicationCleanup(ctx, retry); err != nil {
					t.Fatalf("exact unresolved retry replay: %v", err)
				}
				row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
				if row.state != attachments.BlobPublicationStateRetryWait || row.objectVersion != nil || row.outcome != nil {
					t.Fatalf("unresolved retry state: %#v", row)
				}
				if _, err := fixture.db.Exec(ctx, `update public.blob_publication_intents set retry_at=clock_timestamp()-interval '1 second' where publication_id=$1`, intent.PublicationID); err != nil {
					t.Fatal(err)
				}
				claimRequest.CleanupOwnerID = "s3_contract_second"
				second, err := repository.ClaimBlobPublicationCleanup(ctx, claimRequest)
				if err != nil || second == nil || second.CleanupGeneration <= claim.CleanupGeneration {
					t.Fatalf("retry takeover: %#v %v", second, err)
				}
				completion := attachments.BlobPublicationCleanupCompletionRequest{Claim: *second, Outcome: attachments.BlobPublicationCompletionOutcomeAlreadyAbsent}
				first, err := repository.CompleteBlobPublicationCleanup(ctx, completion)
				if err != nil {
					t.Fatal(err)
				}
				replayed, err := repository.CompleteBlobPublicationCleanup(ctx, completion)
				if err != nil || replayed != first {
					t.Fatalf("absent completion replay: %#v %v", replayed, err)
				}
				return
			}
			resolved, err := blob.ResolveBlobPublicationObject(ctx, request.Target)
			if err != nil || resolved != written.Version {
				t.Fatalf("resolve exact S3 version: %#v %v", resolved, err)
			}
			for _, mutation := range mutations {
				stale := *claim
				mutation.mutate(&stale)
				if _, err := repository.RecordBlobPublicationCleanupVersion(ctx, attachments.BlobPublicationCleanupVersionRequest{Claim: stale, Object: resolved}); !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
					t.Fatalf("stale %s version bind: %v", mutation.name, err)
				}
			}
			versionRequest := attachments.BlobPublicationCleanupVersionRequest{Claim: *claim, Object: resolved}
			bound, err := repository.RecordBlobPublicationCleanupVersion(ctx, versionRequest)
			if err != nil || bound.Intent.ObjectVersion != resolved.VersionID {
				t.Fatalf("version bind: %#v %v", bound, err)
			}
			replayed, err := repository.RecordBlobPublicationCleanupVersion(ctx, versionRequest)
			if err != nil || replayed != bound {
				t.Fatalf("version bind replay: %#v %v", replayed, err)
			}
			drift := versionRequest
			drift.Object.VersionID = "other-valid-version"
			if _, err := repository.RecordBlobPublicationCleanupVersion(ctx, drift); !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
				t.Fatalf("version drift = %v", err)
			}
			receipt, err := blob.Delete(ctx, resolved)
			if err != nil || !receipt.Deleted || receipt.Version != resolved {
				t.Fatalf("exact S3 deletion: %#v %v", receipt, err)
			}
			assertAttachmentPublicationCrashObjectAbsent(t, ctx, blob, resolved)
			for _, mutation := range mutations {
				stale := bound
				mutation.mutate(&stale)
				if _, err := repository.CompleteBlobPublicationCleanup(ctx, attachments.BlobPublicationCleanupCompletionRequest{Claim: stale, Outcome: attachments.BlobPublicationCompletionOutcomeDeleted, Receipt: receipt}); !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
					t.Fatalf("stale %s completion: %v", mutation.name, err)
				}
			}
			completion := attachments.BlobPublicationCleanupCompletionRequest{Claim: bound, Outcome: attachments.BlobPublicationCompletionOutcomeDeleted, Receipt: receipt}
			result, err := repository.CompleteBlobPublicationCleanup(ctx, completion)
			if err != nil {
				t.Fatal(err)
			}
			again, err := repository.CompleteBlobPublicationCleanup(ctx, completion)
			if err != nil || again != result {
				t.Fatalf("exact completion replay: %#v %v", again, err)
			}
			row := readAttachmentPublicationCrashRow(t, ctx, fixture, intent.PublicationID)
			assertAttachmentPublicationCrashTerminal(t, row, attachments.BlobPublicationCompletionOutcomeDeleted, &resolved.VersionID)
		})
	}
}

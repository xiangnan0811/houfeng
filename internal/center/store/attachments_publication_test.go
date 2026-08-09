package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
)

type blobPublicationStoreContract interface {
	PrepareBlobPublication(context.Context, attachments.BlobPublicationPrepareRequest) (attachments.BlobPublicationIntent, error)
	RecordBlobPublicationVersion(context.Context, attachments.BlobPublicationVersionRequest) (attachments.BlobPublicationIntent, error)
	ClaimBlobPublicationCleanup(context.Context, attachments.BlobPublicationCleanupClaimRequest) (*attachments.BlobPublicationCleanupClaim, error)
	RetryBlobPublicationCleanup(context.Context, attachments.BlobPublicationCleanupRetryRequest) error
	CompleteBlobPublicationCleanup(context.Context, attachments.BlobPublicationCleanupCompletionRequest) (attachments.BlobPublicationCleanupResult, error)
}

var _ blobPublicationStoreContract = (*PostgresAttachmentRepository)(nil)

func TestPostgresAttachmentRepositoryPreparesBlobPublicationAfterCanonicalLocks(t *testing.T) {
	request := publicationStorePrepareRequest(0x11, attachments.BlobPublicationOwnerUpload, "aup_publication1", 1)
	prepared := publicationStoreIntent("bpi_publication1", request, "")
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationStepWithArgs(publicationQuery("gc_fence", []any{false},
			"from public.blob_gc_deletions", "blob_key = $1", "deletion_state <> 'completed'"),
			publicationStoreExactArgs(request.Target.Key)),
		publicationStepWithArgs(publicationStepWithBindings(publicationQuery("intent_insert", publicationStoreIntentValues(prepared),
			"insert into public.blob_publication_intents", "on conflict", "do nothing", "returning"),
			publicationStorePrepareInsertSQLBindings()...),
			publicationStorePrepareArgs(prepared.PublicationID, request)),
	)
	repository := publicationStoreRepository(tx, prepared.PublicationID)

	got, err := repository.PrepareBlobPublication(context.Background(), request)
	if err != nil || got != prepared {
		t.Fatalf("PrepareBlobPublication() = (%#v, %v), want %#v", got, err, prepared)
	}
	tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "intent_insert", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryNormalizesPublicationExpiryToMicroseconds(t *testing.T) {
	request := publicationStorePrepareRequest(0x12, attachments.BlobPublicationOwnerUpload, "aup_publication12", 1)
	request.PublishExpiresAt = request.PublishExpiresAt.Add(987 * time.Nanosecond)
	normalizedExpiry := request.PublishExpiresAt.UTC().Truncate(time.Microsecond)
	requestForStorage := request
	requestForStorage.PublishExpiresAt = normalizedExpiry
	prepared := publicationStoreIntent("bpi_publication12", requestForStorage, "")
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationStepWithArgs(publicationQuery("gc_fence", []any{false},
			"from public.blob_gc_deletions", "blob_key = $1", "deletion_state <> 'completed'"),
			publicationStoreExactArgs(request.Target.Key)),
		publicationStepWithArgs(publicationStepWithBindings(publicationQuery("intent_insert", publicationStoreIntentValues(prepared),
			"insert into public.blob_publication_intents", "returning"),
			publicationStorePrepareInsertSQLBindings()...),
			publicationStorePrepareArgs(prepared.PublicationID, requestForStorage)),
	)
	repository := publicationStoreRepository(tx, prepared.PublicationID)

	got, err := repository.PrepareBlobPublication(context.Background(), request)
	if err != nil || got != prepared {
		t.Fatalf("PrepareBlobPublication(non-microsecond expiry) = (%#v, %v), want %#v", got, err, prepared)
	}
	tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "intent_insert", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryRejectsPublicationAcrossActiveGCFence(t *testing.T) {
	request := publicationStorePrepareRequest(0x22, attachments.BlobPublicationOwnerUpload, "aup_publication2", 1)
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationQueryWithout("gc_fence", []any{true}, []string{"object_version"},
			"from public.blob_gc_deletions", "blob_key = $1", "deletion_state <> 'completed'"),
	)
	repository := publicationStoreRepository(tx, "bpi_publication2")

	_, err := repository.PrepareBlobPublication(context.Background(), request)
	if !errors.Is(err, attachments.ErrBlobGCProtected) {
		t.Fatalf("PrepareBlobPublication(active GC) error = %v, want ErrBlobGCProtected", err)
	}
	tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "rollback")
}

func TestPostgresAttachmentRepositoryReplaysOnlyExactPublicationPrepare(t *testing.T) {
	request := publicationStorePrepareRequest(0x33, attachments.BlobPublicationOwnerUpload, "aup_publication3", 1)
	for _, test := range []struct {
		name          string
		state         attachments.BlobPublicationState
		objectVersion string
		outcome       *attachments.BlobPublicationCompletionOutcome
		mutate        func(*attachments.BlobPublicationIntent)
		wantConflict  bool
	}{
		{name: "prepared", state: attachments.BlobPublicationStatePrepared},
		{name: "published", state: attachments.BlobPublicationStatePublished, objectVersion: "object-version-3"},
		{name: "cleanup claimed", state: attachments.BlobPublicationStateCleanupClaimed, objectVersion: "object-version-3", wantConflict: true},
		{name: "retry wait before version resolve", state: attachments.BlobPublicationStateRetryWait, wantConflict: true},
		{name: "retry wait with version", state: attachments.BlobPublicationStateRetryWait, objectVersion: "object-version-3", wantConflict: true},
		{name: "completed deleted", state: attachments.BlobPublicationStateCompleted, objectVersion: "object-version-3", outcome: publicationStoreCompletionOutcome(attachments.BlobPublicationCompletionOutcomeDeleted), wantConflict: true},
		{name: "completed already absent", state: attachments.BlobPublicationStateCompleted, outcome: publicationStoreCompletionOutcome(attachments.BlobPublicationCompletionOutcomeAlreadyAbsent), wantConflict: true},
		{name: "conflicting owner", state: attachments.BlobPublicationStatePrepared, mutate: func(intent *attachments.BlobPublicationIntent) { intent.OwnerID = "aup_conflict3" }, wantConflict: true},
		{name: "owner generation drift", state: attachments.BlobPublicationStatePrepared, mutate: func(intent *attachments.BlobPublicationIntent) { intent.OwnerGeneration++ }, wantConflict: true},
		{name: "target size drift", state: attachments.BlobPublicationStatePrepared, mutate: func(intent *attachments.BlobPublicationIntent) { intent.Target.SizeBytes++ }, wantConflict: true},
		{name: "backend drift", state: attachments.BlobPublicationStatePrepared, mutate: func(intent *attachments.BlobPublicationIntent) { intent.Target.BackendKind = attachments.BackendKindS3 }, wantConflict: true},
		{name: "publish expiry drift", state: attachments.BlobPublicationStatePrepared, mutate: func(intent *attachments.BlobPublicationIntent) {
			intent.PublishExpiresAt = intent.PublishExpiresAt.Add(time.Minute)
		}, wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := publicationStoreIntent("bpi_existing3", request, test.objectVersion)
			stored.State = test.state
			if test.mutate != nil {
				test.mutate(&stored)
			}
			tx := newPublicationStoreTx(t,
				publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
				publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
				publicationStepWithArgs(publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
					publicationStoreExactArgs(request.Target.Key)),
				publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("intent_insert", pgx.ErrNoRows,
					"insert into public.blob_publication_intents", "on conflict", "do nothing", "returning"),
					publicationStorePrepareInsertSQLBindings()...),
					publicationStorePrepareArgs("bpi_attempt3", request)),
				publicationStepWithArgs(publicationStepWithBindings(publicationQuery("intent_replay",
					append(publicationStoreIntentValues(stored), test.outcome),
					"from public.blob_publication_intents",
					"publication_state", "object_version", "completion_outcome",
					"owner_kind", "owner_id", "owner_generation"),
					publicationStorePrepareReplaySQLBindings()...),
					publicationStorePrepareReplayArgs(request)),
			)
			repository := publicationStoreRepository(tx, "bpi_attempt3")

			got, err := repository.PrepareBlobPublication(context.Background(), request)
			if test.wantConflict {
				if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
					t.Fatalf("PrepareBlobPublication(%s) error = %v, want ErrBlobPublicationConflict", test.name, err)
				}
				if tx.committed {
					t.Fatalf("PrepareBlobPublication(%s) committed a conflicting replay", test.name)
				}
				tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "intent_insert", "intent_replay", "rollback")
				return
			}
			if err != nil || got != stored {
				t.Fatalf("PrepareBlobPublication(%s) = (%#v, %v), want %#v", test.name, got, err, stored)
			}
			tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "intent_insert", "intent_replay", "commit", "rollback")
		})
	}
}

func TestPostgresAttachmentRepositoryPrepareReplaySelectsExactActiveIntent(t *testing.T) {
	request := publicationStorePrepareRequest(0x34, attachments.BlobPublicationOwnerUpload, "aup_publication34", 1)
	prepared := publicationStoreIntent("bpi_existing34", request, "")
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationStepWithArgs(publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
			publicationStoreExactArgs(request.Target.Key)),
		publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("intent_insert", pgx.ErrNoRows,
			"insert into public.blob_publication_intents", "on conflict", "do nothing", "returning"),
			publicationStorePrepareInsertSQLBindings()...),
			publicationStorePrepareArgs("bpi_attempt34", request)),
		publicationStepWithArgs(publicationStepWithBindings(publicationQuery("intent_replay",
			append(publicationStoreIntentValues(prepared), nil),
			"from public.blob_publication_intents"),
			publicationStorePrepareReplaySQLBindings()...),
			publicationStorePrepareReplayArgs(request)),
	)
	repository := publicationStoreRepository(tx, "bpi_attempt34")

	got, err := repository.PrepareBlobPublication(context.Background(), request)
	if err != nil || got != prepared {
		t.Fatalf("PrepareBlobPublication(exact active replay) = (%#v, %v), want %#v", got, err, prepared)
	}
	tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "intent_insert", "intent_replay", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryRebindsProcessorPreviewPublicationAfterClaimTakeover(t *testing.T) {
	for _, test := range []struct {
		name          string
		objectVersion string
	}{
		{name: "prepared"},
		{name: "published", objectVersion: "preview-version-35"},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldRequest := publicationStorePrepareRequest(0x35, attachments.BlobPublicationOwnerProcessorPreview, "apj_publication35", 1)
			newRequest := oldRequest
			newRequest.OwnerGeneration = 2
			oldIntent := publicationStoreIntent("bpi_existing35", oldRequest, test.objectVersion)
			reboundIntent := oldIntent
			reboundIntent.OwnerGeneration = newRequest.OwnerGeneration
			tx := newPublicationStoreTx(t,
				publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
				publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
				publicationStepWithArgs(publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
					publicationStoreExactArgs(newRequest.Target.Key)),
				publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("intent_insert", pgx.ErrNoRows,
					"insert into public.blob_publication_intents", "on conflict", "do nothing", "returning"),
					publicationStorePrepareInsertSQLBindings()...),
					publicationStorePrepareArgs("bpi_attempt35", newRequest)),
				publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("intent_replay", pgx.ErrNoRows,
					"from public.blob_publication_intents", "owner_generation = $4"),
					publicationStorePrepareReplaySQLBindings()...),
					publicationStorePrepareReplayArgs(newRequest)),
				publicationStepWithArgs(publicationStepWithBindings(publicationQuery("intent_rebind", publicationStoreIntentValues(reboundIntent),
					"update public.blob_publication_intents", "set owner_generation = $1", "publication_state in ('prepared', 'published')"),
					"owner_generation < $1", "owner_kind = $3", "owner_id = $4"),
					publicationStoreExactArgs(
						newRequest.OwnerGeneration, newRequest.ProjectID, newRequest.OwnerKind, newRequest.OwnerID,
						newRequest.Target.Key, newRequest.Target.SHA256[:], newRequest.Target.SizeBytes,
						newRequest.Target.BackendKind, newRequest.PublishExpiresAt.UTC().Truncate(time.Microsecond),
					)),
			)
			repository := publicationStoreRepository(tx, "bpi_attempt35")

			got, err := repository.PrepareBlobPublication(context.Background(), newRequest)
			if err != nil || got != reboundIntent {
				t.Fatalf("PrepareBlobPublication(processor takeover) = (%#v, %v), want %#v", got, err, reboundIntent)
			}
			tx.assertDone("blob_lock", "upload_parts_lock", "gc_fence", "intent_insert", "intent_replay", "intent_rebind", "commit", "rollback")
		})
	}
}

func TestPostgresAttachmentRepositoryRejectsStalePreviewVersionAfterClaimTakeover(t *testing.T) {
	oldRequest := publicationStorePrepareRequest(0x36, attachments.BlobPublicationOwnerProcessorPreview, "apj_publication36", 1)
	oldIntent := publicationStoreIntent("bpi_existing36", oldRequest, "")
	observed := publicationStoreObjectVersion(oldIntent, "preview-version-36")
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("version_cas", pgx.ErrNoRows,
			"update public.blob_publication_intents", "owner_generation = $6", "publication_state = 'prepared'"),
			publicationStoreVersionCASSQLBindings()...),
			publicationStoreVersionArgs(oldIntent, observed)),
		publicationStepWithArgs(publicationQueryError("version_replay", pgx.ErrNoRows,
			"from public.blob_publication_intents", "publication_id = $1", "publication_state = 'published'"),
			publicationStoreExactArgs(oldIntent.PublicationID)),
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.RecordBlobPublicationVersion(context.Background(), attachments.BlobPublicationVersionRequest{
		Intent: oldIntent, Object: observed,
	})
	if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("RecordBlobPublicationVersion(stale preview generation) error = %v, want ErrBlobPublicationConflict", err)
	}
	tx.assertDone("version_cas", "version_replay", "rollback")
}

func TestPostgresAttachmentRepositoryPublishesVersionWithExactPreparedCAS(t *testing.T) {
	request := publicationStorePrepareRequest(0x44, attachments.BlobPublicationOwnerUpload, "aup_publication4", 1)
	prepared := publicationStoreIntent("bpi_publication4", request, "")
	observed := publicationStoreObjectVersion(prepared, "object-version-4")
	published := prepared
	published.ObjectVersion = observed.VersionID
	published.State = attachments.BlobPublicationStatePublished
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationStepWithBindings(publicationQuery("version_cas", publicationStoreIntentValues(published),
			"update public.blob_publication_intents", "publication_state = 'published'",
			"publication_state = 'prepared'", "object_version is null",
			"returning"), publicationStoreVersionCASSQLBindings()...),
			publicationStoreVersionArgs(prepared, observed)),
	)
	repository := publicationStoreRepository(tx, "unused")

	got, err := repository.RecordBlobPublicationVersion(context.Background(), attachments.BlobPublicationVersionRequest{
		Intent: prepared, Object: observed,
	})
	if err != nil || got != published {
		t.Fatalf("RecordBlobPublicationVersion() = (%#v, %v), want %#v", got, err, published)
	}
	tx.assertDone("version_cas", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryRecordsCleanupVersionWithExactClaimCAS(t *testing.T) {
	claim := publicationStoreCleanupClaim(0x46, "bpi_publication46", "")
	object := publicationStoreObjectVersion(claim.Intent, "object-version-46")
	updated := claim
	updated.Intent.ObjectVersion = object.VersionID
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationStepWithBindings(publicationQuery("cleanup_version_cas", publicationStoreClaimValues(updated),
			"update public.blob_publication_intents", "set object_version = $2",
			"publication_state = 'cleanup_claimed'", "object_version is null",
			"cleanup_owner_id = $3", "cleanup_generation = $4", "attempt = $5",
			"cleanup_lease_expires_at = $6", "cleanup_lease_expires_at > transaction_timestamp()",
			"returning"), publicationStoreCleanupVersionCASSQLBindings()...),
			publicationStoreCleanupVersionArgs(claim, object)),
	)
	repository := publicationStoreRepository(tx, "unused")

	got, err := repository.RecordBlobPublicationCleanupVersion(context.Background(), attachments.BlobPublicationCleanupVersionRequest{
		Claim: claim, Object: object,
	})
	if err != nil || got != updated {
		t.Fatalf("RecordBlobPublicationCleanupVersion() = (%#v, %v), want %#v", got, err, updated)
	}
	tx.assertDone("cleanup_version_cas", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryReplaysExactCleanupVersionCAS(t *testing.T) {
	claim := publicationStoreCleanupClaim(0x48, "bpi_publication48", "")
	object := publicationStoreObjectVersion(claim.Intent, "object-version-48")
	updated := claim
	updated.Intent.ObjectVersion = object.VersionID
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("cleanup_version_cas", pgx.ErrNoRows,
			"update public.blob_publication_intents", "set object_version = $2",
			"publication_state = 'cleanup_claimed'", "object_version is null"),
			publicationStoreCleanupVersionCASSQLBindings()...),
			publicationStoreCleanupVersionArgs(claim, object)),
		publicationStepWithArgs(publicationQuery("cleanup_version_replay", publicationStoreClaimValues(updated),
			"from public.blob_publication_intents", "publication_state = 'cleanup_claimed'",
			"publication_id = $1", "object_version = $2", "cleanup_owner_id = $3",
			"cleanup_generation = $4", "attempt = $5", "cleanup_lease_expires_at = $6",
			"cleanup_lease_expires_at > transaction_timestamp()", "project_id = $7",
			"owner_kind = $8", "owner_id = $9", "owner_generation = $10",
			"blob_key = $11", "sha256_digest = $12", "size_bytes = $13",
			"backend_kind = $14"), publicationStoreCleanupVersionArgs(claim, object)),
	)
	repository := publicationStoreRepository(tx, "unused")

	got, err := repository.RecordBlobPublicationCleanupVersion(context.Background(), attachments.BlobPublicationCleanupVersionRequest{
		Claim: claim, Object: object,
	})
	if err != nil || got != updated {
		t.Fatalf("RecordBlobPublicationCleanupVersion(replay) = (%#v, %v), want %#v", got, err, updated)
	}
	tx.assertDone("cleanup_version_cas", "cleanup_version_replay", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryRejectsStaleCleanupVersionCAS(t *testing.T) {
	claim := publicationStoreCleanupClaim(0x47, "bpi_publication47", "")
	object := publicationStoreObjectVersion(claim.Intent, "object-version-47")
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("cleanup_version_cas", pgx.ErrNoRows,
			"update public.blob_publication_intents", "set object_version = $2",
			"publication_state = 'cleanup_claimed'", "object_version is null"),
			publicationStoreCleanupVersionCASSQLBindings()...),
			publicationStoreCleanupVersionArgs(claim, object)),
		publicationStepWithArgs(publicationQuery("cleanup_version_replay", publicationStoreClaimValues(claim),
			"from public.blob_publication_intents", "publication_state = 'cleanup_claimed'",
			"publication_id = $1", "object_version = $2", "cleanup_owner_id = $3",
			"cleanup_generation = $4", "attempt = $5", "cleanup_lease_expires_at = $6",
			"cleanup_lease_expires_at > transaction_timestamp()", "project_id = $7",
			"owner_kind = $8", "owner_id = $9", "owner_generation = $10",
			"blob_key = $11", "sha256_digest = $12", "size_bytes = $13",
			"backend_kind = $14"), publicationStoreCleanupVersionArgs(claim, object)),
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.RecordBlobPublicationCleanupVersion(context.Background(), attachments.BlobPublicationCleanupVersionRequest{
		Claim: claim, Object: object,
	})
	if !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
		t.Fatalf("RecordBlobPublicationCleanupVersion(stale) error = %v, want ErrBlobPublicationClaimLost", err)
	}
	tx.assertDone("cleanup_version_cas", "cleanup_version_replay", "rollback")
}

func TestPostgresAttachmentRepositoryRejectsObservedObjectDriftBeforeVersionCAS(t *testing.T) {
	request := publicationStorePrepareRequest(0x45, attachments.BlobPublicationOwnerUpload, "aup_publication45", 1)
	prepared := publicationStoreIntent("bpi_publication45", request, "")
	observed := publicationStoreObjectVersion(prepared, "object-version-45")
	for _, test := range []struct {
		name                   string
		mutate                 func(*attachments.ObjectVersion)
		wantValidObjectFixture bool
	}{
		{name: "valid alternate key and digest", wantValidObjectFixture: true, mutate: func(object *attachments.ObjectVersion) {
			alternateDigest := sha256.Sum256([]byte("alternate-store-publication-object"))
			object.Key = fmt.Sprintf("sha256/%x", alternateDigest)
			object.SHA256 = alternateDigest
		}},
		{name: "valid alternate size", wantValidObjectFixture: true, mutate: func(object *attachments.ObjectVersion) {
			object.SizeBytes++
		}},
		{name: "empty version", mutate: func(object *attachments.ObjectVersion) {
			object.VersionID = ""
		}},
		{name: "oversized version", mutate: func(object *attachments.ObjectVersion) {
			object.VersionID = strings.Repeat("v", 1025)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalidObject := observed
			test.mutate(&invalidObject)
			if test.wantValidObjectFixture {
				if err := invalidObject.Validate(); err != nil {
					t.Fatalf("alternate ObjectVersion fixture is invalid: %v", err)
				}
			}
			tx := newPublicationStoreTx(t)
			repository := publicationStoreRepository(tx, "unused")

			_, err := repository.RecordBlobPublicationVersion(context.Background(), attachments.BlobPublicationVersionRequest{
				Intent: prepared, Object: invalidObject,
			})
			if !errors.Is(err, attachments.ErrInvalidBlobPublicationRequest) {
				t.Fatalf("RecordBlobPublicationVersion(%s) error = %v, want ErrInvalidBlobPublicationRequest", test.name, err)
			}
			tx.assertDone()
		})
	}
}

func TestPostgresAttachmentRepositoryRejectsPublisherAfterCleanupClaim(t *testing.T) {
	request := publicationStorePrepareRequest(0x55, attachments.BlobPublicationOwnerUpload, "aup_publication5", 1)
	prepared := publicationStoreIntent("bpi_publication5", request, "")
	observed := publicationStoreObjectVersion(prepared, "object-version-5")
	cleanupClaimed := prepared
	cleanupClaimed.State = attachments.BlobPublicationStateCleanupClaimed
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("version_cas", pgx.ErrNoRows,
			"update public.blob_publication_intents", "publication_state = 'prepared'"),
			publicationStoreVersionCASSQLBindings()...),
			publicationStoreVersionArgs(prepared, observed)),
		publicationStepWithArgs(publicationQuery("cleanup_fence", publicationStoreIntentValues(cleanupClaimed),
			"from public.blob_publication_intents", "publication_id = $1",
			"publication_state"), publicationStoreExactArgs(prepared.PublicationID)),
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.RecordBlobPublicationVersion(context.Background(), attachments.BlobPublicationVersionRequest{
		Intent: prepared, Object: observed,
	})
	if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("RecordBlobPublicationVersion(cleanup claimed) error = %v, want ErrBlobPublicationConflict", err)
	}
	tx.assertDone("version_cas", "cleanup_fence", "rollback")
}

func TestPostgresAttachmentRepositoryReplaysOnlyExactPublishedVersion(t *testing.T) {
	request := publicationStorePrepareRequest(0x56, attachments.BlobPublicationOwnerUpload, "aup_publication56", 1)
	prepared := publicationStoreIntent("bpi_publication56", request, "")
	published := prepared
	published.State = attachments.BlobPublicationStatePublished
	published.ObjectVersion = "object-version-56"
	for _, test := range []struct {
		name            string
		objectVersion   string
		mutateStored    func(*attachments.BlobPublicationIntent)
		wantValidStored bool
		wantConflict    bool
	}{
		{name: "same version", objectVersion: published.ObjectVersion},
		{name: "different version", objectVersion: "object-version-56-conflict", wantConflict: true},
		{name: "different valid owner identity", objectVersion: published.ObjectVersion, wantValidStored: true, wantConflict: true, mutateStored: func(intent *attachments.BlobPublicationIntent) {
			intent.OwnerID = "aup_publication57"
		}},
		{name: "different valid digest identity", objectVersion: published.ObjectVersion, wantValidStored: true, wantConflict: true, mutateStored: func(intent *attachments.BlobPublicationIntent) {
			alternateDigest := sha256.Sum256([]byte("alternate-published-publication"))
			intent.Target.Key = fmt.Sprintf("sha256/%x", alternateDigest)
			intent.Target.SHA256 = alternateDigest
		}},
		{name: "owner generation drift", objectVersion: published.ObjectVersion, mutateStored: func(intent *attachments.BlobPublicationIntent) { intent.OwnerGeneration++ }, wantConflict: true},
		{name: "target size drift", objectVersion: published.ObjectVersion, mutateStored: func(intent *attachments.BlobPublicationIntent) { intent.Target.SizeBytes++ }, wantConflict: true},
		{name: "backend drift", objectVersion: published.ObjectVersion, mutateStored: func(intent *attachments.BlobPublicationIntent) { intent.Target.BackendKind = attachments.BackendKindS3 }, wantConflict: true},
		{name: "publish expiry drift", objectVersion: published.ObjectVersion, mutateStored: func(intent *attachments.BlobPublicationIntent) {
			intent.PublishExpiresAt = intent.PublishExpiresAt.Add(time.Minute)
		}, wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed := publicationStoreObjectVersion(prepared, test.objectVersion)
			stored := published
			if test.mutateStored != nil {
				test.mutateStored(&stored)
			}
			if test.wantValidStored {
				if err := stored.Validate(); err != nil {
					t.Fatalf("stored replay drift fixture is invalid: %v", err)
				}
			}
			tx := newPublicationStoreTx(t,
				publicationStepWithArgs(publicationStepWithBindings(publicationQueryError("version_cas", pgx.ErrNoRows,
					"update public.blob_publication_intents", "publication_state = 'prepared'", "object_version is null"),
					publicationStoreVersionCASSQLBindings()...),
					publicationStoreVersionArgs(prepared, observed)),
				publicationStepWithArgs(publicationQuery("version_replay", publicationStoreIntentValues(stored),
					"from public.blob_publication_intents", "publication_id = $1",
					"publication_state = 'published'", "owner_kind", "owner_id", "owner_generation",
					"blob_key", "sha256_digest", "object_version", "size_bytes", "backend_kind"),
					publicationStoreExactArgs(prepared.PublicationID)),
			)
			repository := publicationStoreRepository(tx, "unused")

			got, err := repository.RecordBlobPublicationVersion(context.Background(), attachments.BlobPublicationVersionRequest{
				Intent: prepared, Object: observed,
			})
			if test.wantConflict {
				if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
					t.Fatalf("RecordBlobPublicationVersion(%s) error = %v, want ErrBlobPublicationConflict", test.name, err)
				}
				tx.assertDone("version_cas", "version_replay", "rollback")
				return
			}
			if err != nil || got != published {
				t.Fatalf("RecordBlobPublicationVersion(%s) = (%#v, %v), want %#v", test.name, got, err, published)
			}
			tx.assertDone("version_cas", "version_replay", "commit", "rollback")
		})
	}
}

func TestUploadPartMetadataConsumesExactPublishedIntentInSameTransaction(t *testing.T) {
	request := publicationStorePrepareRequest(0x66, attachments.BlobPublicationOwnerUpload, "aup_publication6", 1)
	intent := publicationStoreIntent("bpi_publication6", request, "object-version-6")
	object := attachments.ObjectVersion{
		Key: intent.Target.Key, VersionID: intent.ObjectVersion,
		SHA256: intent.Target.SHA256, SizeBytes: intent.Target.SizeBytes,
	}
	tx := newPublicationStoreTx(t,
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
		publicationExec("upload_part_insert", "INSERT 1", "insert into public.attachment_upload_parts"),
		publicationStepWithArgs(publicationStepWithBindings(publicationExec("intent_consume", "UPDATE 1",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "publication_state = 'published'"),
			publicationStoreConsumeSQLBindings()...),
			publicationStoreConsumeArgs(intent)),
	)

	err := consumeBlobPublicationForUploadPart(context.Background(), tx, "aup_publication6", object, intent)
	if err != nil {
		t.Fatalf("consumeBlobPublicationForUploadPart() error = %v", err)
	}
	tx.assertDone("upload_parts_lock", "gc_fence", "upload_part_insert", "intent_consume")
}

func TestPreviewBlobMetadataConsumesExactPublishedIntentInSameTransaction(t *testing.T) {
	request := publicationStorePrepareRequest(0x77, attachments.BlobPublicationOwnerProcessorPreview, "apj_publication7", 7)
	intent := publicationStoreIntent("bpi_publication7", request, "object-version-7")
	object := attachments.BlobObject{
		Key: intent.Target.Key, SHA256: intent.Target.SHA256, ObjectVersion: intent.ObjectVersion,
		SizeBytes: intent.Target.SizeBytes, BackendKind: intent.Target.BackendKind,
	}
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
		publicationExec("blob_insert", "INSERT 1", "insert into public.blob_objects"),
		publicationStepWithArgs(publicationStepWithBindings(publicationExec("intent_consume", "UPDATE 1",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "publication_state = 'published'"),
			publicationStoreConsumeSQLBindings()...),
			publicationStoreConsumeArgs(intent)),
	)

	inserted, err := consumeBlobPublicationForBlobObject(context.Background(), tx, object, intent)
	if err != nil || !inserted {
		t.Fatalf("consumeBlobPublicationForBlobObject() = (%t, %v), want true/nil", inserted, err)
	}
	tx.assertDone("blob_lock", "gc_fence", "blob_insert", "intent_consume")
}

func TestPostgresAttachmentRepositoryRecordUploadedContentRollsBackWhenPublicationConsumeLosesCAS(t *testing.T) {
	request := publicationStorePrepareRequest(0x78, attachments.BlobPublicationOwnerUpload, "aup_publication78", 1)
	intent := publicationStoreIntent("bpi_publication78", request, "object-version-78")
	object := attachments.ObjectVersion{
		Key: intent.Target.Key, VersionID: intent.ObjectVersion,
		SHA256: intent.Target.SHA256, SizeBytes: intent.Target.SizeBytes,
	}
	expiresAt := request.PublishExpiresAt.Add(time.Hour)
	tx := newPublicationStoreTx(t,
		publicationQuery("upload_route", []any{
			request.ProjectID, request.OwnerID, "att_publication78", "rdf_publication78", "usr_publication78",
		}, "from public.attachment_uploads", "where project_id = $1"),
		publicationQuery("draft_route", []any{request.ProjectID, nil},
			"from public.record_drafts", "where draft_id = $1"),
		publicationQuery("draft_lock", []any{request.ProjectID, nil},
			"from public.record_drafts", "for update"),
		publicationQuery("upload_lock", []any{
			request.ProjectID, request.OwnerID, "att_publication78", "rdf_publication78", "usr_publication78",
			attachments.UploadStateUploading, object.SizeBytes, object.SizeBytes,
			nil, []byte(nil), nil, nil, []byte(nil),
		}, "from public.attachment_uploads", "for update"),
		publicationQuery("upload_preparation", []any{
			attachments.TransportKindLocal, expiresAt,
			request.ProjectID, "att_publication78", "rdf_publication78",
			attachments.UploadStateUploading, "text/plain",
		}, "join public.record_attachments"),
		publicationQuery("upload_expiry", []any{false}, "expires_at <= transaction_timestamp()"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
		publicationExec("upload_part_insert", "INSERT 0 1", "insert into public.attachment_upload_parts"),
		publicationStepWithArgs(publicationStepWithBindings(publicationExec("intent_consume", "UPDATE 0",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "publication_state = 'published'"),
			publicationStoreConsumeSQLBindings()...),
			publicationStoreConsumeArgs(intent)),
		publicationStoreConsumedReplayStep("intent_consume_replay", intent, false),
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.RecordUploadedContent(context.Background(), attachments.RecordUploadedContentCommand{
		ProjectID: request.ProjectID, UploadID: request.OwnerID, AuthorID: "usr_publication78",
		Object: object, PublicationIntent: intent,
	})
	if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("RecordUploadedContent(publication consume conflict) error = %v, want ErrBlobPublicationConflict", err)
	}
	if tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("RecordUploadedContent(publication consume conflict) transaction committed=%t rollbacks=%d",
			tx.committed, tx.rollbackCount)
	}
	tx.assertDone(
		"upload_route", "draft_route", "draft_lock", "upload_lock", "upload_preparation", "upload_expiry",
		"upload_parts_lock", "gc_fence", "upload_part_insert", "intent_consume", "intent_consume_replay", "rollback",
	)
}

func TestConsumeBlobPublicationForUploadPartReplaysExactConsumedIntent(t *testing.T) {
	request := publicationStorePrepareRequest(0x78, attachments.BlobPublicationOwnerUpload, "aup_publication78", 1)
	intent := publicationStoreIntent("bpi_publication78", request, "object-version-78")
	object := attachments.ObjectVersion{
		Key: intent.Target.Key, VersionID: intent.ObjectVersion,
		SHA256: intent.Target.SHA256, SizeBytes: intent.Target.SizeBytes,
	}
	tx := newPublicationStoreTx(t,
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationStepWithArgs(publicationQuery("gc_fence", []any{false},
			"from public.blob_gc_deletions", "blob_key = $1", "object_version = $2"),
			publicationStoreExactArgs(object.Key, object.VersionID)),
		publicationExec("upload_part_insert", "INSERT 0 0", "insert into public.attachment_upload_parts"),
		publicationStepWithArgs(publicationQuery("gc_fence_recheck", []any{false},
			"from public.blob_gc_deletions", "blob_key = $1", "object_version = $2"),
			publicationStoreExactArgs(object.Key, object.VersionID)),
		publicationStepWithArgs(publicationQuery("upload_part_replay",
			[]any{object.SizeBytes, object.SHA256[:], object.VersionID},
			"from public.attachment_upload_parts", "upload_id = $1", "part_number = 1"),
			publicationStoreExactArgs(request.OwnerID)),
		publicationStepWithArgs(publicationStepWithBindings(publicationExec("intent_consume", "UPDATE 0",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "publication_state = 'published'"),
			publicationStoreConsumeSQLBindings()...),
			publicationStoreConsumeArgs(intent)),
		publicationStoreConsumedReplayStep("intent_consume_replay", intent, true),
	)

	if err := consumeBlobPublicationForUploadPart(
		context.Background(), tx, request.OwnerID, object, intent,
	); err != nil {
		t.Fatalf("consumeBlobPublicationForUploadPart(exact consumed replay) error = %v", err)
	}
	tx.assertDone(
		"upload_parts_lock", "gc_fence", "upload_part_insert", "gc_fence_recheck",
		"upload_part_replay", "intent_consume", "intent_consume_replay",
	)
}

func TestPostgresAttachmentRepositoryCompleteUploadAndEnqueueRollsBackWhenPublicationConsumeLosesCAS(t *testing.T) {
	request := publicationStorePrepareRequest(0x79, attachments.BlobPublicationOwnerUpload, "aup_publication79", 1)
	request.Target.BackendKind = attachments.BackendKindS3
	intent := publicationStoreIntent("bpi_publication79", request, "object-version-79")
	object := attachments.ObjectVersion{
		Key: intent.Target.Key, VersionID: intent.ObjectVersion,
		SHA256: intent.Target.SHA256, SizeBytes: intent.Target.SizeBytes,
	}
	temporaryKey := "temporary/7979797979797979797979797979797979797979797979797979797979797979"
	temporaryVersion := "temporary-version-79"
	expiresAt := request.PublishExpiresAt.Add(time.Hour)
	fingerprint := sha256.Sum256([]byte("publication completion 79"))
	tx := newPublicationStoreTx(t,
		publicationQuery("upload_route", []any{
			request.ProjectID, request.OwnerID, "att_publication79", "rdf_publication79", "usr_publication79",
		}, "from public.attachment_uploads", "where project_id = $1"),
		publicationQuery("draft_route", []any{request.ProjectID, nil},
			"from public.record_drafts", "where draft_id = $1"),
		publicationQuery("draft_lock", []any{request.ProjectID, nil},
			"from public.record_drafts", "for update"),
		publicationQuery("upload_lock", []any{
			request.ProjectID, request.OwnerID, "att_publication79", "rdf_publication79", "usr_publication79",
			attachments.UploadStateUploading, object.SizeBytes, object.SizeBytes,
			nil, []byte(nil), &temporaryKey, &temporaryVersion, []byte(nil),
		}, "from public.attachment_uploads", "for update"),
		publicationQuery("upload_preparation", []any{
			attachments.TransportKindS3, expiresAt,
			request.ProjectID, "att_publication79", "rdf_publication79",
			attachments.UploadStateUploading, "text/plain",
		}, "join public.record_attachments"),
		publicationQueryError("upload_part_read", pgx.ErrNoRows,
			"from public.attachment_upload_parts", "part_number = 1"),
		publicationQuery("upload_expiry", []any{false}, "expires_at <= transaction_timestamp()"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationQuery("gc_fence", []any{false}, "from public.blob_gc_deletions"),
		publicationExec("upload_part_insert", "INSERT 0 1", "insert into public.attachment_upload_parts"),
		publicationStepWithArgs(publicationStepWithBindings(publicationExec("intent_consume", "UPDATE 0",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "publication_state = 'published'"),
			publicationStoreConsumeSQLBindings()...),
			publicationStoreConsumeArgs(intent)),
		publicationStoreConsumedReplayStep("intent_consume_replay", intent, false),
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.CompleteUploadAndEnqueue(context.Background(), attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: request.ProjectID, UploadID: request.OwnerID, AuthorID: "usr_publication79",
		ActualSizeBytes: object.SizeBytes, ActualSHA256: object.SHA256,
		TemporaryObjectKey: temporaryKey, TemporaryObjectVersion: temporaryVersion,
		Object: object, PublicationIntent: intent, CompletionFingerprint: fingerprint,
		ProcessorJobID: "apj_publication79", ProcessorProfile: attachments.ProcessorProfileText,
		ProcessorMaxAttempts: 3, ProcessorExpiresAt: expiresAt,
	})
	if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("CompleteUploadAndEnqueue(publication consume conflict) error = %v, want ErrBlobPublicationConflict", err)
	}
	if tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("CompleteUploadAndEnqueue(publication consume conflict) transaction committed=%t rollbacks=%d",
			tx.committed, tx.rollbackCount)
	}
	tx.assertDone(
		"upload_route", "draft_route", "draft_lock", "upload_lock", "upload_preparation", "upload_part_read",
		"upload_expiry", "upload_parts_lock", "gc_fence", "upload_part_insert", "intent_consume",
		"intent_consume_replay", "rollback",
	)
}

func TestPostgresAttachmentRepositoryCompleteUploadAndEnqueueRequiresIntentForNewUploadPart(t *testing.T) {
	request := publicationStorePrepareRequest(0x7a, attachments.BlobPublicationOwnerUpload, "aup_publication7a", 1)
	request.Target.BackendKind = attachments.BackendKindS3
	object := attachments.ObjectVersion{
		Key: request.Target.Key, VersionID: "object-version-7a",
		SHA256: request.Target.SHA256, SizeBytes: request.Target.SizeBytes,
	}
	temporaryKey := "temporary/7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a"
	temporaryVersion := "temporary-version-7a"
	expiresAt := request.PublishExpiresAt.Add(time.Hour)
	fingerprint := sha256.Sum256([]byte("publication completion 7a"))
	tx := newPublicationStoreTx(t,
		publicationQuery("upload_route", []any{
			request.ProjectID, request.OwnerID, "att_publication7a", "rdf_publication7a", "usr_publication7a",
		}, "from public.attachment_uploads", "where project_id = $1"),
		publicationQuery("draft_route", []any{request.ProjectID, nil},
			"from public.record_drafts", "where draft_id = $1"),
		publicationQuery("draft_lock", []any{request.ProjectID, nil},
			"from public.record_drafts", "for update"),
		publicationQuery("upload_lock", []any{
			request.ProjectID, request.OwnerID, "att_publication7a", "rdf_publication7a", "usr_publication7a",
			attachments.UploadStateUploading, object.SizeBytes, object.SizeBytes,
			nil, []byte(nil), &temporaryKey, &temporaryVersion, []byte(nil),
		}, "from public.attachment_uploads", "for update"),
		publicationQuery("upload_preparation", []any{
			attachments.TransportKindS3, expiresAt,
			request.ProjectID, "att_publication7a", "rdf_publication7a",
			attachments.UploadStateUploading, "text/plain",
		}, "join public.record_attachments"),
		publicationQueryError("upload_part_read", pgx.ErrNoRows,
			"from public.attachment_upload_parts", "part_number = 1"),
		publicationQuery("upload_expiry", []any{false}, "expires_at <= transaction_timestamp()"),
		publicationStoreStep{
			kind: "exec", label: "unexpected_upload_parts_lock",
			contains: []string{"lock table public.attachment_upload_parts"},
			err:      errors.New("missing publication intent reached upload-part publication"),
		},
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.CompleteUploadAndEnqueue(context.Background(), attachments.CompleteUploadAndEnqueueCommand{
		ProjectID: request.ProjectID, UploadID: request.OwnerID, AuthorID: "usr_publication7a",
		ActualSizeBytes: object.SizeBytes, ActualSHA256: object.SHA256,
		TemporaryObjectKey: temporaryKey, TemporaryObjectVersion: temporaryVersion,
		Object: object, CompletionFingerprint: fingerprint,
		ProcessorJobID: "apj_publication7a", ProcessorProfile: attachments.ProcessorProfileText,
		ProcessorMaxAttempts: 3, ProcessorExpiresAt: expiresAt,
	})
	if !errors.Is(err, attachments.ErrInvalidAttachmentCommand) {
		t.Fatalf("CompleteUploadAndEnqueue(new upload part without publication intent) error = %v, want ErrInvalidAttachmentCommand", err)
	}
	if slices.Contains(tx.seen, "unexpected_upload_parts_lock") {
		t.Fatal("CompleteUploadAndEnqueue reached upload-part publication without a durable intent")
	}
}

func TestPostgresAttachmentRepositoryCompleteProcessorJobRequiresIntentForNewPreview(t *testing.T) {
	sourceDigest := sha256.Sum256([]byte("publication processor source 7b"))
	source := attachments.BlobObject{
		Key: "sha256/" + fmt.Sprintf("%x", sourceDigest), SHA256: sourceDigest,
		ObjectVersion: "source-version-7b", SizeBytes: 31, BackendKind: attachments.BackendKindLocal,
	}
	leaseExpiresAt := time.Date(2026, time.August, 7, 12, 5, 0, 0, time.UTC)
	expiresAt := leaseExpiresAt.Add(time.Hour)
	claim := attachments.ProcessorClaim{
		ProjectID: "default", ProcessorJobID: "apj_publication7b",
		UploadID: "aup_publication7b", AttachmentID: "att_publication7b",
		DisplayName: "publication.txt", DeclaredMediaType: "text/plain",
		Source: source, Profile: attachments.ProcessorProfileText,
		Attempt: 1, MaxAttempts: 3, OwnerID: "processor_publication7b", OwnerGeneration: 7,
		LeaseExpiresAt: leaseExpiresAt, ExpiresAt: expiresAt,
	}
	previewDigest := sha256.Sum256([]byte("publication processor preview 7b"))
	result := attachments.ProcessorResult{
		Source: source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean,
		HasPreview: true,
		Preview: attachments.ManagedPreview{
			Blob: attachments.BlobObject{
				Key: "sha256/" + fmt.Sprintf("%x", previewDigest), SHA256: previewDigest,
				ObjectVersion: "preview-version-7b", SizeBytes: 23, BackendKind: attachments.BackendKindLocal,
			},
			MediaType: attachments.ManagedPreviewMediaTypeTextUTF8,
		},
	}
	databaseNow := leaseExpiresAt.Add(-time.Minute)
	jobValues := []any{
		claim.ProcessorJobID, claim.UploadID, claim.AttachmentID, attachments.ProcessorStateClaimed,
		claim.Profile, claim.Attempt, claim.MaxAttempts, claim.OwnerID, claim.OwnerGeneration,
		&leaseExpiresAt, nil, nil, []byte(nil), "", nil, expiresAt, databaseNow,
	}
	tx := newPublicationStoreTx(t,
		publicationQuery("processor_preflight", jobValues,
			"from public.attachment_processor_jobs", "processor_state = 'claimed'"),
		publicationStoreStep{
			kind: "exec", label: "unexpected_quota_ensure",
			contains: []string{"insert into public.attachment_quota_accounts"},
			err:      errors.New("missing preview publication intent reached processor commit"),
		},
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.CompleteProcessorJob(context.Background(), attachments.ProcessorCompletionInput{
		Claim: claim, Result: result, Limits: attachments.DefaultLimits(),
	})
	if !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("CompleteProcessorJob(new preview without publication intent) error = %v, want ErrInvalidProcessorCommand", err)
	}
	if slices.Contains(tx.seen, "unexpected_quota_ensure") {
		t.Fatal("CompleteProcessorJob reached durable processor writes without a preview publication intent")
	}
}

func TestPostgresAttachmentRepositoryCompleteProcessorJobRollsBackWhenPreviewPublicationConsumeLosesCAS(t *testing.T) {
	sourceDigest := sha256.Sum256([]byte("publication processor source 80"))
	source := attachments.BlobObject{
		Key: "sha256/" + fmt.Sprintf("%x", sourceDigest), SHA256: sourceDigest,
		ObjectVersion: "source-version-80", SizeBytes: 31, BackendKind: attachments.BackendKindLocal,
	}
	leaseExpiresAt := time.Date(2026, time.August, 7, 12, 5, 0, 0, time.UTC)
	expiresAt := leaseExpiresAt.Add(time.Hour)
	claim := attachments.ProcessorClaim{
		ProjectID: "default", ProcessorJobID: "apj_publication80",
		UploadID: "aup_publication80", AttachmentID: "att_publication80",
		DisplayName: "publication.txt", DeclaredMediaType: "text/plain",
		Source: source, Profile: attachments.ProcessorProfileText,
		Attempt: 1, MaxAttempts: 3, OwnerID: "processor_publication80", OwnerGeneration: 7,
		LeaseExpiresAt: leaseExpiresAt, ExpiresAt: expiresAt,
	}
	request := publicationStorePrepareRequest(
		0x80, attachments.BlobPublicationOwnerProcessorPreview, claim.ProcessorJobID, claim.OwnerGeneration,
	)
	request.PublishExpiresAt = expiresAt
	intent := publicationStoreIntent("bpi_publication80", request, "preview-version-80")
	preview := attachments.BlobObject{
		Key: intent.Target.Key, SHA256: intent.Target.SHA256, ObjectVersion: intent.ObjectVersion,
		SizeBytes: intent.Target.SizeBytes, BackendKind: intent.Target.BackendKind,
	}
	result := attachments.ProcessorResult{
		Source: source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean,
		HasPreview: true,
		Preview: attachments.ManagedPreview{
			Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8,
		},
	}
	actualSize := source.SizeBytes
	draftID := "rdf_publication80"
	databaseNow := leaseExpiresAt.Add(-time.Minute)
	completionFingerprint := sha256.Sum256([]byte("publication processor completion 80"))
	jobValues := []any{
		claim.ProcessorJobID, claim.UploadID, claim.AttachmentID, attachments.ProcessorStateClaimed,
		claim.Profile, claim.Attempt, claim.MaxAttempts, claim.OwnerID, claim.OwnerGeneration,
		&leaseExpiresAt, nil, nil, []byte(nil), "", nil, expiresAt, databaseNow,
	}
	tx := newPublicationStoreTx(t,
		publicationQuery("processor_preflight", jobValues,
			"from public.attachment_processor_jobs", "processor_state = 'claimed'"),
		publicationExec("quota_ensure", "INSERT 0 1", "insert into public.attachment_quota_accounts"),
		publicationQuery("quota_lock", []any{int64(0), source.SizeBytes, int64(0), int64(0)},
			"from public.attachment_quota_accounts", "for update"),
		publicationQuery("processor_upload", []any{
			claim.ProjectID, claim.UploadID, claim.AttachmentID, draftID, "usr_publication80",
			attachments.UploadStateQuarantined, source.SizeBytes, source.SizeBytes,
			&actualSize, source.SHA256[:], nil, nil, completionFingerprint[:],
			attachments.TransportKindLocal, attachments.UploadStateQuarantined,
			nil, &draftID, nil, nil, nil, nil, nil, nil,
		}, "join public.record_attachments", "for update of upload, attachment"),
		publicationQuery("upload_part_read", []any{source.SizeBytes, source.SHA256[:], source.ObjectVersion},
			"from public.attachment_upload_parts", "part_number = 1"),
		publicationQuery("processor_job_lock", jobValues,
			"from public.attachment_processor_jobs", "for update"),
		publicationExec("source_blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationQuery("source_gc_fence", []any{false}, "from public.blob_gc_deletions"),
		publicationExec("source_blob_insert", "INSERT 0 1", "insert into public.blob_objects"),
		publicationExec("preview_blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationQuery("preview_gc_fence", []any{false}, "from public.blob_gc_deletions"),
		publicationExec("preview_blob_insert", "INSERT 0 1", "insert into public.blob_objects"),
		publicationStepWithArgs(publicationStepWithBindings(publicationExec("intent_consume", "UPDATE 0",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "publication_state = 'published'"),
			publicationStoreConsumeSQLBindings()...),
			publicationStoreConsumeArgs(intent)),
		publicationStoreConsumedReplayStep("intent_consume_replay", intent, false),
	)
	repository := publicationStoreRepository(tx, "unused")

	_, err := repository.CompleteProcessorJob(context.Background(), attachments.ProcessorCompletionInput{
		Claim: claim, Result: result, Limits: attachments.DefaultLimits(),
		PreviewPublicationIntent: intent,
	})
	if !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("CompleteProcessorJob(preview publication consume conflict) error = %v, want ErrBlobPublicationConflict", err)
	}
	if tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("CompleteProcessorJob(preview publication consume conflict) transaction committed=%t rollbacks=%d",
			tx.committed, tx.rollbackCount)
	}
	tx.assertDone(
		"processor_preflight", "quota_ensure", "quota_lock", "processor_upload", "upload_part_read",
		"processor_job_lock", "source_blob_lock", "source_gc_fence", "source_blob_insert",
		"preview_blob_lock", "preview_gc_fence", "preview_blob_insert", "intent_consume",
		"intent_consume_replay", "rollback",
	)
}

func TestPostgresAttachmentRepositoryClaimsPublicationCleanupWithDatabaseLease(t *testing.T) {
	request := publicationStorePrepareRequest(0x88, attachments.BlobPublicationOwnerProcessorPreview, "apj_publication8", 3)
	intent := publicationStoreIntent("bpi_publication8", request, "object-version-8")
	leaseExpiresAt := request.PublishExpiresAt.Add(attachments.DefaultBlobPublicationCleanupLeaseDuration)
	claimRequest := attachments.BlobPublicationCleanupClaimRequest{
		ProjectID: "default", BackendKind: intent.Target.BackendKind,
		CleanupOwnerID:     "publication_reconciler_8",
		OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration,
	}
	claim := attachments.BlobPublicationCleanupClaim{
		Intent: intent, CleanupOwnerID: "publication_reconciler_8",
		CleanupGeneration: 2, Attempt: 3, ObservedLeaseExpiresAt: leaseExpiresAt,
	}
	claim.Intent.State = attachments.BlobPublicationStateCleanupClaimed
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationExec("consume_reconcile", "UPDATE 0",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "from public.attachment_upload_parts",
			"publication_state = 'published'", "publish_expires_at <= transaction_timestamp()",
			"from public.blob_objects", "part.object_version = publication.object_version",
			"blob.object_version = publication.object_version"),
		publicationStepWithArgs(publicationQuery("cleanup_claim", publicationStoreClaimValues(claim),
			"from public.blob_publication_intents", "for update skip locked",
			"publication_state = 'prepared'", "publication_state = 'published'",
			"publication_state = 'retry_wait'", "publication_state = 'cleanup_claimed'",
			"project_id = $1", "backend_kind = $2",
			"publish_expires_at <= transaction_timestamp()", "retry_at <= transaction_timestamp()",
			"cleanup_lease_expires_at <= transaction_timestamp()",
			"set cleanup_owner_id = $3",
			"cleanup_generation = publication.cleanup_generation + 1",
			"attempt = publication.attempt + 1",
			"cleanup_lease_expires_at = transaction_timestamp() + make_interval(secs => $4)",
			"returning"), publicationStoreCleanupClaimArgs(claimRequest)),
	)
	repository := publicationStoreRepository(tx, "unused")

	got, err := repository.ClaimBlobPublicationCleanup(context.Background(), claimRequest)
	if err != nil || got == nil || *got != claim {
		t.Fatalf("ClaimBlobPublicationCleanup() = (%#v, %v), want %#v", got, err, claim)
	}
	if !got.ObservedLeaseExpiresAt.Equal(leaseExpiresAt) {
		t.Fatalf("cleanup observed lease = %v, want database value %v", got.ObservedLeaseExpiresAt, leaseExpiresAt)
	}
	tx.assertDone("blob_lock", "upload_parts_lock", "consume_reconcile", "cleanup_claim", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryConsumesReferencedExpiredPublicationBeforeCleanupClaim(t *testing.T) {
	request := publicationStorePrepareRequest(0x89, attachments.BlobPublicationOwnerUpload, "aup_publication89", 1)
	intent := publicationStoreIntent("bpi_publication89", request, "object-version-89")
	tx := newPublicationStoreTx(t,
		publicationExec("blob_lock", "LOCK TABLE", "lock table public.blob_objects"),
		publicationExec("upload_parts_lock", "LOCK TABLE", "lock table public.attachment_upload_parts"),
		publicationExec("consume_reconcile", "UPDATE 1",
			"with candidate as", "for update skip locked",
			"update public.blob_publication_intents", "publication_state = 'completed'",
			"completion_outcome = 'consumed'", "completed_at = transaction_timestamp()",
			"publication_state = 'published'", "publish_expires_at <= transaction_timestamp()",
			"project_id =", "backend_kind =",
			"from public.attachment_upload_parts", "part.sha256_digest = publication.sha256_digest",
			"part.object_version = publication.object_version", "part.size_bytes = publication.size_bytes",
			"from public.blob_objects", "blob.blob_key = publication.blob_key",
			"blob.sha256_digest = publication.sha256_digest",
			"blob.object_version = publication.object_version",
			"blob.size_bytes = publication.size_bytes", "blob.backend_kind = publication.backend_kind"),
	)
	repository := publicationStoreRepository(tx, "unused")

	got, err := repository.ClaimBlobPublicationCleanup(context.Background(), attachments.BlobPublicationCleanupClaimRequest{
		ProjectID: intent.ProjectID, BackendKind: intent.Target.BackendKind,
		CleanupOwnerID:     "publication_reconciler_89",
		OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration,
	})
	if err != nil || got != nil {
		t.Fatalf("ClaimBlobPublicationCleanup(referenced) = (%#v, %v), want nil/nil", got, err)
	}
	tx.assertDone("blob_lock", "upload_parts_lock", "consume_reconcile", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryFencesPublicationCleanupRetry(t *testing.T) {
	current := publicationStoreCleanupClaim(0x99, "bpi_publication9", "object-version-9")
	for _, test := range []struct {
		name   string
		mutate func(*attachments.BlobPublicationCleanupClaim)
	}{
		{name: "owner", mutate: func(claim *attachments.BlobPublicationCleanupClaim) {
			claim.CleanupOwnerID = "publication_reconciler_stale"
		}},
		{name: "generation", mutate: func(claim *attachments.BlobPublicationCleanupClaim) { claim.CleanupGeneration++ }},
		{name: "attempt", mutate: func(claim *attachments.BlobPublicationCleanupClaim) { claim.Attempt++ }},
		{name: "observed lease", mutate: func(claim *attachments.BlobPublicationCleanupClaim) {
			claim.ObservedLeaseExpiresAt = claim.ObservedLeaseExpiresAt.Add(time.Second)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stale := current
			test.mutate(&stale)
			retryAt := stale.ObservedLeaseExpiresAt.Add(time.Minute)
			tx := newPublicationStoreTx(t,
				publicationStepWithArgs(publicationExec("cleanup_retry", "UPDATE 0",
					"update public.blob_publication_intents", "publication_state = 'retry_wait'",
					"publication_id = $1", "cleanup_owner_id = $2", "cleanup_generation = $3",
					"attempt = $4", "cleanup_lease_expires_at = $5",
					"cleanup_lease_expires_at > transaction_timestamp()",
					"publication_state = 'cleanup_claimed'", "retry_at",
					"project_id =", "owner_kind =", "owner_id =", "owner_generation =",
					"blob_key =", "sha256_digest =", "object_version is not distinct from $13",
					"size_bytes =", "backend_kind ="),
					publicationStoreRetryArgs(stale, retryAt)),
				publicationStepWithArgs(publicationQuery("retry_replay", []any{false},
					"from public.blob_publication_intents", "publication_state = 'retry_wait'",
					"publication_id", "cleanup_owner_id", "cleanup_generation", "attempt",
					"cleanup_lease_expires_at", "retry_at", "project_id", "owner_kind", "owner_id",
					"owner_generation", "blob_key", "sha256_digest",
					"object_version is not distinct from $13", "size_bytes", "backend_kind"),
					publicationStoreRetryArgs(stale, retryAt)),
			)
			repository := publicationStoreRepository(tx, "unused")

			err := repository.RetryBlobPublicationCleanup(context.Background(), attachments.BlobPublicationCleanupRetryRequest{
				Claim: stale, RetryAt: retryAt,
			})
			if !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
				t.Fatalf("RetryBlobPublicationCleanup(stale %s) error = %v, want ErrBlobPublicationClaimLost", test.name, err)
			}
			tx.assertDone("cleanup_retry", "retry_replay", "rollback")
		})
	}
}

func TestPostgresAttachmentRepositoryReplaysExactPublicationCleanupRetry(t *testing.T) {
	claim := publicationStoreCleanupClaim(0xaa, "bpi_publicationa", "object-version-a")
	retryAt := claim.ObservedLeaseExpiresAt.Add(time.Minute)
	tx := newPublicationStoreTx(t,
		publicationStepWithArgs(publicationExec("cleanup_retry", "UPDATE 0",
			"update public.blob_publication_intents", "publication_state = 'retry_wait'",
			"cleanup_owner_id", "cleanup_generation", "attempt", "cleanup_lease_expires_at",
			"project_id =", "owner_kind =", "owner_id =", "owner_generation =",
			"blob_key =", "sha256_digest =", "object_version is not distinct from $13",
			"size_bytes =", "backend_kind ="),
			publicationStoreRetryArgs(claim, retryAt)),
		publicationStepWithArgs(publicationQuery("retry_replay", []any{true},
			"from public.blob_publication_intents", "publication_state = 'retry_wait'",
			"publication_id", "cleanup_owner_id", "cleanup_generation", "attempt",
			"cleanup_lease_expires_at", "retry_at", "project_id", "owner_kind", "owner_id",
			"owner_generation", "blob_key", "sha256_digest",
			"object_version is not distinct from $13", "size_bytes", "backend_kind"),
			publicationStoreRetryArgs(claim, retryAt)),
	)
	repository := publicationStoreRepository(tx, "unused")

	err := repository.RetryBlobPublicationCleanup(context.Background(), attachments.BlobPublicationCleanupRetryRequest{
		Claim: claim, RetryAt: retryAt,
	})
	if err != nil {
		t.Fatalf("RetryBlobPublicationCleanup(replay) error = %v", err)
	}
	tx.assertDone("cleanup_retry", "retry_replay", "commit", "rollback")
}

func TestPostgresAttachmentRepositoryRetriesAndReplaysUnresolvedPublicationCleanup(t *testing.T) {
	current := publicationStoreCleanupClaim(0xab, "bpi_publicationab", "")
	for _, test := range []struct {
		name          string
		update        string
		replayMatch   *bool
		mutate        func(*attachments.BlobPublicationCleanupClaim)
		wantClaimLost bool
		steps         []string
	}{
		{
			name: "first retry", update: "UPDATE 1",
			steps: []string{"cleanup_retry", "commit", "rollback"},
		},
		{
			name: "durable replay", update: "UPDATE 0", replayMatch: publicationStoreBool(true),
			steps: []string{"cleanup_retry", "retry_replay", "commit", "rollback"},
		},
		{
			name: "stale fence", update: "UPDATE 0", replayMatch: publicationStoreBool(false),
			mutate: func(claim *attachments.BlobPublicationCleanupClaim) {
				claim.CleanupGeneration++
			},
			wantClaimLost: true,
			steps:         []string{"cleanup_retry", "retry_replay", "rollback"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := current
			if test.mutate != nil {
				test.mutate(&claim)
			}
			retryAt := claim.ObservedLeaseExpiresAt.Add(time.Minute)
			script := []publicationStoreStep{
				publicationStepWithArgs(publicationExec("cleanup_retry", test.update,
					"update public.blob_publication_intents", "publication_state = 'retry_wait'",
					"retry_at = $6", "publication_id = $1", "cleanup_owner_id = $2",
					"cleanup_generation = $3", "attempt = $4", "cleanup_lease_expires_at = $5",
					"project_id = $7", "owner_kind = $8", "owner_id = $9", "owner_generation = $10",
					"blob_key = $11", "sha256_digest = $12",
					"object_version is not distinct from $13", "size_bytes = $14", "backend_kind = $15"),
					publicationStoreRetryArgs(claim, retryAt)),
			}
			if test.replayMatch != nil {
				script = append(script, publicationStepWithArgs(publicationQuery("retry_replay", []any{*test.replayMatch},
					"from public.blob_publication_intents", "publication_state = 'retry_wait'",
					"retry_at = $6", "publication_id = $1", "cleanup_owner_id = $2",
					"cleanup_generation = $3", "attempt = $4", "cleanup_lease_expires_at = $5",
					"project_id = $7", "owner_kind = $8", "owner_id = $9", "owner_generation = $10",
					"blob_key = $11", "sha256_digest = $12",
					"object_version is not distinct from $13", "size_bytes = $14", "backend_kind = $15"),
					publicationStoreRetryArgs(claim, retryAt)))
			}
			tx := newPublicationStoreTx(t, script...)
			repository := publicationStoreRepository(tx, "unused")

			err := repository.RetryBlobPublicationCleanup(context.Background(), attachments.BlobPublicationCleanupRetryRequest{
				Claim: claim, RetryAt: retryAt,
			})
			if test.wantClaimLost {
				if !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
					t.Fatalf("RetryBlobPublicationCleanup(%s) error = %v, want ErrBlobPublicationClaimLost", test.name, err)
				}
			} else if err != nil {
				t.Fatalf("RetryBlobPublicationCleanup(%s) error = %v", test.name, err)
			}
			tx.assertDone(test.steps...)
		})
	}
}

func TestPostgresAttachmentRepositoryCompletesAndReplaysExactPublicationCleanup(t *testing.T) {
	for _, outcomeTest := range []struct {
		name          string
		objectVersion string
		outcome       attachments.BlobPublicationCompletionOutcome
		deleted       bool
	}{
		{
			name: "deleted", objectVersion: "object-version-b",
			outcome: attachments.BlobPublicationCompletionOutcomeDeleted, deleted: true,
		},
		{
			name: "already absent with known version", objectVersion: "object-version-b",
			outcome: attachments.BlobPublicationCompletionOutcomeAlreadyAbsent,
		},
		{
			name:    "already absent before version resolution",
			outcome: attachments.BlobPublicationCompletionOutcomeAlreadyAbsent,
		},
	} {
		t.Run(outcomeTest.name, func(t *testing.T) {
			claim := publicationStoreCleanupClaim(0xbb, "bpi_publicationb", outcomeTest.objectVersion)
			exactObject := attachments.BlobObject{}
			receipt := attachments.DeletionReceipt{}
			if object, ok := claim.Intent.Object(); ok {
				exactObject = object
				receipt = attachments.DeletionReceipt{
					Version: publicationStoreObjectVersion(claim.Intent, claim.Intent.ObjectVersion),
					Deleted: outcomeTest.deleted,
				}
			}
			completion := attachments.BlobPublicationCleanupCompletionRequest{
				Claim: claim, Outcome: outcomeTest.outcome, Receipt: receipt,
			}
			expected := attachments.BlobPublicationCleanupResult{
				PublicationID: claim.Intent.PublicationID,
				Object:        exactObject,
				Outcome:       outcomeTest.outcome,
				Receipt:       receipt,
			}
			for _, test := range []struct {
				name        string
				update      string
				replayMatch *bool
				steps       []string
			}{
				{name: "first completion", update: "UPDATE 1", steps: []string{"cleanup_complete", "commit", "rollback"}},
				{name: "durable replay", update: "UPDATE 0", replayMatch: publicationStoreBool(true), steps: []string{"cleanup_complete", "completion_replay", "commit", "rollback"}},
			} {
				t.Run(test.name, func(t *testing.T) {
					script := []publicationStoreStep{
						publicationStepWithArgs(publicationExec("cleanup_complete", test.update,
							"update public.blob_publication_intents", "publication_state = 'completed'",
							"completion_outcome = $6", "receipt_digest = $7", "completed_at = transaction_timestamp()",
							"publication_id = $1", "cleanup_owner_id = $2", "cleanup_generation = $3",
							"attempt = $4", "cleanup_lease_expires_at = $5",
							"cleanup_lease_expires_at > transaction_timestamp()",
							"publication_state = 'cleanup_claimed'",
							"project_id = $8", "owner_kind = $9", "owner_id = $10", "owner_generation = $11",
							"blob_key = $12", "sha256_digest = $13",
							"object_version is not distinct from $14",
							"size_bytes = $15", "backend_kind = $16"),
							publicationStoreCompletionArgs(claim, outcomeTest.outcome)),
					}
					if test.replayMatch != nil {
						script = append(script, publicationStepWithArgs(publicationQuery("completion_replay", []any{*test.replayMatch},
							"from public.blob_publication_intents", "publication_state = 'completed'",
							"completion_outcome = $6", "receipt_digest = $7", "publication_id = $1",
							"cleanup_owner_id = $2", "cleanup_generation = $3", "attempt = $4",
							"cleanup_lease_expires_at = $5", "project_id = $8", "owner_kind = $9",
							"owner_id = $10", "owner_generation = $11", "blob_key = $12",
							"sha256_digest = $13", "object_version is not distinct from $14",
							"size_bytes = $15",
							"backend_kind = $16"),
							publicationStoreCompletionArgs(claim, outcomeTest.outcome)))
					}
					tx := newPublicationStoreTx(t, script...)
					repository := publicationStoreRepository(tx, "unused")

					result, err := repository.CompleteBlobPublicationCleanup(context.Background(), completion)
					if err != nil {
						t.Fatalf("CompleteBlobPublicationCleanup(%s) error = %v", test.name, err)
					}
					if result != expected {
						t.Fatalf("CompleteBlobPublicationCleanup(%s) = %#v, want %#v", test.name, result, expected)
					}
					if err := result.ValidateAgainst(completion); err != nil {
						t.Fatalf("CompleteBlobPublicationCleanup(%s) result binding error = %v", test.name, err)
					}
					tx.assertDone(test.steps...)
				})
			}
		})
	}
}

func TestPostgresAttachmentRepositoryRejectsEveryStalePublicationCleanupCompletionFence(t *testing.T) {
	current := publicationStoreCleanupClaim(0xbc, "bpi_publicationbc", "object-version-bc")
	receipt := attachments.DeletionReceipt{
		Version: attachments.ObjectVersion{
			Key: current.Intent.Target.Key, VersionID: current.Intent.ObjectVersion,
			SHA256: current.Intent.Target.SHA256, SizeBytes: current.Intent.Target.SizeBytes,
		},
		Deleted: true,
	}
	for _, test := range []struct {
		name   string
		mutate func(*attachments.BlobPublicationCleanupClaim)
	}{
		{name: "owner", mutate: func(claim *attachments.BlobPublicationCleanupClaim) {
			claim.CleanupOwnerID = "publication_reconciler_stale"
		}},
		{name: "generation", mutate: func(claim *attachments.BlobPublicationCleanupClaim) { claim.CleanupGeneration++ }},
		{name: "attempt", mutate: func(claim *attachments.BlobPublicationCleanupClaim) { claim.Attempt++ }},
		{name: "observed lease", mutate: func(claim *attachments.BlobPublicationCleanupClaim) {
			claim.ObservedLeaseExpiresAt = claim.ObservedLeaseExpiresAt.Add(time.Second)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stale := current
			test.mutate(&stale)
			tx := newPublicationStoreTx(t,
				publicationStepWithArgs(publicationExec("cleanup_complete", "UPDATE 0",
					"update public.blob_publication_intents", "publication_state = 'completed'",
					"completion_outcome = $6", "receipt_digest = $7",
					"publication_id = $1", "cleanup_owner_id = $2", "cleanup_generation = $3",
					"attempt = $4", "cleanup_lease_expires_at = $5",
					"cleanup_lease_expires_at > transaction_timestamp()",
					"publication_state = 'cleanup_claimed'",
					"project_id = $8", "owner_kind = $9", "owner_id = $10", "owner_generation = $11",
					"blob_key = $12", "sha256_digest = $13",
					"object_version is not distinct from $14",
					"size_bytes = $15", "backend_kind = $16"),
					publicationStoreCompletionArgs(stale, attachments.BlobPublicationCompletionOutcomeDeleted)),
				publicationStepWithArgs(publicationQuery("completion_replay", []any{false},
					"from public.blob_publication_intents", "publication_state = 'completed'",
					"completion_outcome = $6", "receipt_digest = $7", "publication_id = $1",
					"cleanup_owner_id = $2", "cleanup_generation = $3", "attempt = $4",
					"cleanup_lease_expires_at = $5", "project_id = $8", "owner_kind = $9",
					"owner_id = $10", "owner_generation = $11", "blob_key = $12",
					"sha256_digest = $13", "object_version is not distinct from $14",
					"size_bytes = $15",
					"backend_kind = $16"),
					publicationStoreCompletionArgs(stale, attachments.BlobPublicationCompletionOutcomeDeleted)),
			)
			repository := publicationStoreRepository(tx, "unused")

			_, err := repository.CompleteBlobPublicationCleanup(context.Background(), attachments.BlobPublicationCleanupCompletionRequest{
				Claim: stale, Outcome: attachments.BlobPublicationCompletionOutcomeDeleted, Receipt: receipt,
			})
			if !errors.Is(err, attachments.ErrBlobPublicationClaimLost) {
				t.Fatalf("CompleteBlobPublicationCleanup(stale %s) error = %v, want ErrBlobPublicationClaimLost", test.name, err)
			}
			tx.assertDone("cleanup_complete", "completion_replay", "rollback")
		})
	}
}

func publicationStoreBool(value bool) *bool {
	return &value
}

func publicationStoreCompletionOutcome(
	value attachments.BlobPublicationCompletionOutcome,
) *attachments.BlobPublicationCompletionOutcome {
	return &value
}

func TestBlobGCCandidateExcludesEveryNonterminalPublicationByDigestKey(t *testing.T) {
	tx := newPublicationStoreTx(t,
		publicationQueryWithout("candidate", nil, []string{"publication.object_version"},
			"from public.blob_objects as blob",
			"from public.blob_publication_intents as publication",
			"publication.blob_key = blob.blob_key",
			"publication.publication_state <> 'completed'"),
	)

	_, err := lockDurableBlobGCCandidate(context.Background(), tx, attachments.BlobGCClaimRequest{
		ProjectID: "default", BackendKind: attachments.BackendKindLocal,
		Mode:           attachments.BlobGCPurgeModeOrdinary,
		OrphanedBefore: time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC),
		OwnerID:        "gc_publication_test", OwnerLeaseDuration: time.Minute,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("lockDurableBlobGCCandidate() error = %v, want pgx.ErrNoRows", err)
	}
	tx.assertDone("candidate")
}

func publicationStoreRepository(tx attachmentTx, publicationID string) *PostgresAttachmentRepository {
	return &PostgresAttachmentRepository{
		beginTx:              func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
		newBlobPublicationID: func() (string, error) { return publicationID, nil },
	}
}

func publicationStorePrepareRequest(
	fill byte,
	ownerKind attachments.BlobPublicationOwnerKind,
	ownerID string,
	ownerGeneration int64,
) attachments.BlobPublicationPrepareRequest {
	digest := sha256.Sum256(bytes.Repeat([]byte{fill}, sha256.Size))
	return attachments.BlobPublicationPrepareRequest{
		ProjectID: "default", OwnerKind: ownerKind, OwnerID: ownerID,
		OwnerGeneration: ownerGeneration,
		Target: attachments.BlobPublicationTarget{
			Key: fmt.Sprintf("sha256/%x", digest), SHA256: digest,
			SizeBytes: int64(fill) + 1, BackendKind: attachments.BackendKindLocal,
		},
		PublishExpiresAt: time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC),
	}
}

func publicationStoreIntent(
	publicationID string,
	request attachments.BlobPublicationPrepareRequest,
	objectVersion string,
) attachments.BlobPublicationIntent {
	state := attachments.BlobPublicationStatePrepared
	if objectVersion != "" {
		state = attachments.BlobPublicationStatePublished
	}
	return attachments.BlobPublicationIntent{
		PublicationID: publicationID, ProjectID: request.ProjectID,
		OwnerKind: request.OwnerKind, OwnerID: request.OwnerID,
		OwnerGeneration: request.OwnerGeneration, Target: request.Target,
		ObjectVersion: objectVersion, State: state, PublishExpiresAt: request.PublishExpiresAt,
	}
}

func publicationStoreObjectVersion(
	intent attachments.BlobPublicationIntent,
	versionID string,
) attachments.ObjectVersion {
	return attachments.ObjectVersion{
		Key: intent.Target.Key, VersionID: versionID,
		SHA256: intent.Target.SHA256, SizeBytes: intent.Target.SizeBytes,
	}
}

func publicationStoreCleanupClaim(fill byte, publicationID, objectVersion string) attachments.BlobPublicationCleanupClaim {
	request := publicationStorePrepareRequest(fill, attachments.BlobPublicationOwnerProcessorPreview, "apj_cleanup1", 4)
	intent := publicationStoreIntent(publicationID, request, objectVersion)
	intent.State = attachments.BlobPublicationStateCleanupClaimed
	return attachments.BlobPublicationCleanupClaim{
		Intent:         intent,
		CleanupOwnerID: "publication_reconciler_1", CleanupGeneration: 2, Attempt: 3,
		ObservedLeaseExpiresAt: request.PublishExpiresAt.Add(attachments.DefaultBlobPublicationCleanupLeaseDuration),
	}
}

func publicationStoreIntentValues(intent attachments.BlobPublicationIntent) []any {
	return []any{
		intent.PublicationID, intent.ProjectID, intent.OwnerKind, intent.OwnerID,
		intent.OwnerGeneration, intent.Target.Key, intent.Target.SHA256[:],
		intent.Target.SizeBytes, intent.Target.BackendKind, intent.ObjectVersion, intent.State,
		intent.PublishExpiresAt,
	}
}

func publicationStoreClaimValues(claim attachments.BlobPublicationCleanupClaim) []any {
	return append(publicationStoreIntentValues(claim.Intent),
		claim.CleanupOwnerID, claim.CleanupGeneration, claim.Attempt, claim.ObservedLeaseExpiresAt)
}

func publicationStoreCleanupClaimArgs(
	request attachments.BlobPublicationCleanupClaimRequest,
) func(*testing.T, []any) {
	return publicationStoreExactArgs(
		request.ProjectID,
		request.BackendKind,
		request.CleanupOwnerID,
		int64(request.OwnerLeaseDuration/time.Second),
	)
}

type publicationStoreStep struct {
	kind       string
	label      string
	contains   []string
	without    []string
	commandTag string
	values     []any
	err        error
	assertArgs func(*testing.T, []any)
}

func publicationExec(label, commandTag string, contains ...string) publicationStoreStep {
	return publicationStoreStep{kind: "exec", label: label, commandTag: commandTag, contains: contains}
}

func publicationQuery(label string, values []any, contains ...string) publicationStoreStep {
	return publicationStoreStep{kind: "query", label: label, values: values, contains: contains}
}

func publicationQueryWithout(label string, values []any, without []string, contains ...string) publicationStoreStep {
	return publicationStoreStep{kind: "query", label: label, values: values, contains: contains, without: without}
}

func publicationQueryError(label string, err error, contains ...string) publicationStoreStep {
	return publicationStoreStep{kind: "query", label: label, err: err, contains: contains}
}

func publicationStepWithArgs(step publicationStoreStep, assertArgs func(*testing.T, []any)) publicationStoreStep {
	step.assertArgs = assertArgs
	return step
}

func publicationStepWithBindings(step publicationStoreStep, bindings ...string) publicationStoreStep {
	step.contains = append(step.contains, bindings...)
	return step
}

type publicationStoreTx struct {
	pgx.Tx
	t             *testing.T
	script        []publicationStoreStep
	seen          []string
	committed     bool
	rollbackCount int
}

func newPublicationStoreTx(t *testing.T, script ...publicationStoreStep) *publicationStoreTx {
	t.Helper()
	return &publicationStoreTx{t: t, script: script}
}

func (tx *publicationStoreTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := tx.take("exec", sql, args)
	return pgconn.NewCommandTag(step.commandTag), step.err
}

func (tx *publicationStoreTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	step := tx.take("query", sql, args)
	return publicationStoreRow{values: step.values, err: step.err}
}

func (tx *publicationStoreTx) Commit(context.Context) error {
	tx.committed = true
	tx.seen = append(tx.seen, "commit")
	return nil
}

func (tx *publicationStoreTx) Rollback(context.Context) error {
	tx.rollbackCount++
	tx.seen = append(tx.seen, "rollback")
	return nil
}

func (tx *publicationStoreTx) take(kind, sql string, args []any) publicationStoreStep {
	tx.t.Helper()
	if len(tx.script) == 0 {
		tx.t.Fatalf("unexpected publication %s:\n%s", kind, sql)
	}
	step := tx.script[0]
	tx.script = tx.script[1:]
	if step.kind != kind {
		tx.t.Fatalf("publication step %q kind = %q, want %q:\n%s", step.label, kind, step.kind, sql)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	for _, fragment := range step.contains {
		if !publicationSQLContains(compact, fragment) {
			tx.t.Fatalf("publication step %q SQL omitted %q:\n%s", step.label, fragment, compact)
		}
	}
	for _, fragment := range step.without {
		if publicationSQLContains(compact, fragment) {
			tx.t.Fatalf("publication step %q SQL unexpectedly contains %q:\n%s", step.label, fragment, compact)
		}
	}
	if step.assertArgs != nil {
		step.assertArgs(tx.t, args)
	}
	tx.seen = append(tx.seen, step.label)
	return step
}

func (tx *publicationStoreTx) assertDone(want ...string) {
	tx.t.Helper()
	if len(tx.script) != 0 {
		tx.t.Fatalf("publication script has %d unconsumed steps; seen %#v", len(tx.script), tx.seen)
	}
	if !reflect.DeepEqual(tx.seen, want) {
		tx.t.Fatalf("publication steps = %#v, want %#v", tx.seen, want)
	}
}

type publicationStoreRow struct {
	values []any
	err    error
}

func (row publicationStoreRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if row.values == nil {
		return pgx.ErrNoRows
	}
	if len(dest) != len(row.values) {
		return fmt.Errorf("publication scan destination count %d, want %d", len(dest), len(row.values))
	}
	for index := range dest {
		target := reflect.ValueOf(dest[index])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return fmt.Errorf("publication scan destination %d is not a pointer", index)
		}
		value := reflect.ValueOf(row.values[index])
		if !value.IsValid() {
			target.Elem().Set(reflect.Zero(target.Elem().Type()))
			continue
		}
		if !value.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("publication scan destination %d type mismatch: %v -> %v", index, value.Type(), target.Elem().Type())
		}
		target.Elem().Set(value)
	}
	return nil
}

func publicationSQLContains(compactSQL, fragment string) bool {
	haystack := publicationSQLTokens(compactSQL)
	needle := publicationSQLTokens(fragment)
	if len(needle) == 0 {
		return true
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		matched := true
		for offset := range needle {
			if haystack[start+offset] != needle[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func publicationSQLTokens(value string) []string {
	value = strings.ToLower(value)
	tokens := make([]string, 0, len(value)/4)
	for index := 0; index < len(value); {
		if value[index] == ' ' || value[index] == '\t' || value[index] == '\n' || value[index] == '\r' {
			index++
			continue
		}
		if publicationSQLWordByte(value[index]) {
			end := index + 1
			for end < len(value) && publicationSQLWordByte(value[end]) {
				end++
			}
			tokens = append(tokens, value[index:end])
			index = end
			continue
		}
		tokens = append(tokens, value[index:index+1])
		index++
	}
	return tokens
}

func publicationSQLWordByte(value byte) bool {
	return value == '_' ||
		value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'z'
}

func TestPublicationSQLContainsMatchesWholeTokens(t *testing.T) {
	t.Parallel()

	if publicationSQLContains("where publication_id = $10", "publication_id = $1") {
		t.Fatal("publication SQL matcher accepted $1 inside $10")
	}
	if publicationSQLContains("where cleanup_owner_id = $10", "owner_id = $10") {
		t.Fatal("publication SQL matcher accepted owner_id inside cleanup_owner_id")
	}
	if !publicationSQLContains("where publication_id=$1 and owner_id = $10", "publication_id = $1") {
		t.Fatal("publication SQL matcher rejected exact placeholder binding")
	}
	if publicationSQLContains(
		"object_version is not distinct from $130",
		"object_version is not distinct from $13",
	) {
		t.Fatal("publication SQL matcher accepted $13 inside $130")
	}
	if !publicationSQLContains(
		"object_version is not distinct from $13",
		"object_version is not distinct from $13",
	) {
		t.Fatal("publication SQL matcher rejected exact NULL-safe placeholder binding")
	}
}

func TestPublicationStoreSQLObjectVersionUsesNullForUnresolvedIdentity(t *testing.T) {
	t.Parallel()

	if got := publicationStoreSQLObjectVersion(""); got != nil {
		t.Fatalf("publicationStoreSQLObjectVersion(empty) = %#v, want nil", got)
	}
	if got := publicationStoreSQLObjectVersion("object-version-1"); got != "object-version-1" {
		t.Fatalf("publicationStoreSQLObjectVersion(known) = %#v, want object-version-1", got)
	}
}

func publicationStorePrepareInsertSQLBindings() []string {
	return []string{
		"(publication_id, project_id, owner_kind, owner_id, owner_generation, blob_key, sha256_digest, " +
			"size_bytes, backend_kind, publication_state, publish_expires_at) values " +
			"($1, $2, $3, $4, $5, $6, $7, $8, $9, 'prepared', $10)",
	}
}

func publicationStorePrepareReplaySQLBindings() []string {
	return []string{
		"where publication_state <> 'completed'",
		"project_id = $1",
		"owner_kind = $2",
		"owner_id = $3",
		"owner_generation = $4",
		"blob_key = $5",
		"sha256_digest = $6",
		"size_bytes = $7",
		"backend_kind = $8",
		"publish_expires_at = $9",
	}
}

func publicationStoreVersionCASSQLBindings() []string {
	return []string{
		"set object_version = $2, publication_state = 'published'",
		"where publication_id = $1",
		"project_id = $3",
		"owner_kind = $4",
		"owner_id = $5",
		"owner_generation = $6",
		"blob_key = $7",
		"sha256_digest = $8",
		"size_bytes = $9",
		"backend_kind = $10",
		"publish_expires_at = $11",
	}
}

func publicationStoreCleanupVersionCASSQLBindings() []string {
	return []string{
		"set object_version = $2",
		"where publication_id = $1",
		"cleanup_owner_id = $3",
		"cleanup_generation = $4",
		"attempt = $5",
		"cleanup_lease_expires_at = $6",
		"project_id = $7",
		"owner_kind = $8",
		"owner_id = $9",
		"owner_generation = $10",
		"blob_key = $11",
		"sha256_digest = $12",
		"size_bytes = $13",
		"backend_kind = $14",
	}
}

func publicationStoreConsumeSQLBindings() []string {
	return []string{
		"where publication_id = $1",
		"owner_kind = $2",
		"owner_id = $3",
		"owner_generation = $4",
		"blob_key = $5",
		"sha256_digest = $6",
		"object_version = $7",
		"size_bytes = $8",
		"backend_kind = $9",
	}
}

func publicationStoreConsumedReplayStep(
	label string,
	intent attachments.BlobPublicationIntent,
	replay bool,
) publicationStoreStep {
	bindings := append(publicationStoreConsumeSQLBindings(),
		"project_id = $10", "publish_expires_at = $11")
	return publicationStepWithArgs(publicationStepWithBindings(publicationQuery(label, []any{replay},
		"select exists", "from public.blob_publication_intents",
		"publication_state = 'completed'", "completion_outcome = 'consumed'",
		"receipt_digest = sha256_digest"), bindings...),
		publicationStoreExactArgs(
			intent.PublicationID,
			intent.OwnerKind,
			intent.OwnerID,
			intent.OwnerGeneration,
			intent.Target.Key,
			intent.Target.SHA256[:],
			intent.ObjectVersion,
			intent.Target.SizeBytes,
			intent.Target.BackendKind,
			intent.ProjectID,
			intent.PublishExpiresAt.UTC().Truncate(time.Microsecond),
		))
}

func publicationStoreExactArgs(want ...any) func(*testing.T, []any) {
	return func(t *testing.T, args []any) {
		t.Helper()
		if len(args) != len(want) {
			t.Fatalf("publication argument count = %d, want %d: %#v", len(args), len(want), args)
		}
		for index := range want {
			if !reflect.DeepEqual(args[index], want[index]) {
				t.Fatalf("publication arg[%d] = %#v, want %#v; all args %#v", index, args[index], want[index], args)
			}
		}
	}
}

func publicationStorePrepareArgs(
	publicationID string,
	request attachments.BlobPublicationPrepareRequest,
) func(*testing.T, []any) {
	return publicationStoreExactArgs(publicationID,
		request.ProjectID, request.OwnerKind, request.OwnerID, request.OwnerGeneration,
		request.Target.Key, request.Target.SHA256[:], request.Target.SizeBytes,
		request.Target.BackendKind, request.PublishExpiresAt)
}

func publicationStorePrepareReplayArgs(
	request attachments.BlobPublicationPrepareRequest,
) func(*testing.T, []any) {
	return publicationStoreExactArgs(
		request.ProjectID,
		request.OwnerKind,
		request.OwnerID,
		request.OwnerGeneration,
		request.Target.Key,
		request.Target.SHA256[:],
		request.Target.SizeBytes,
		request.Target.BackendKind,
		request.PublishExpiresAt.UTC().Truncate(time.Microsecond),
	)
}

func publicationStoreVersionArgs(
	intent attachments.BlobPublicationIntent,
	object attachments.ObjectVersion,
) func(*testing.T, []any) {
	return publicationStoreExactArgs(intent.PublicationID, object.VersionID,
		intent.ProjectID, intent.OwnerKind, intent.OwnerID, intent.OwnerGeneration,
		intent.Target.Key, intent.Target.SHA256[:], intent.Target.SizeBytes,
		intent.Target.BackendKind, intent.PublishExpiresAt)
}

func publicationStoreCleanupVersionArgs(
	claim attachments.BlobPublicationCleanupClaim,
	object attachments.ObjectVersion,
) func(*testing.T, []any) {
	return publicationStoreExactArgs(
		claim.Intent.PublicationID, object.VersionID,
		claim.CleanupOwnerID, claim.CleanupGeneration, claim.Attempt,
		claim.ObservedLeaseExpiresAt,
		claim.Intent.ProjectID, claim.Intent.OwnerKind, claim.Intent.OwnerID,
		claim.Intent.OwnerGeneration, claim.Intent.Target.Key,
		claim.Intent.Target.SHA256[:], claim.Intent.Target.SizeBytes,
		claim.Intent.Target.BackendKind,
	)
}

func publicationStoreConsumeArgs(intent attachments.BlobPublicationIntent) func(*testing.T, []any) {
	return publicationStoreExactArgs(
		intent.PublicationID,
		intent.OwnerKind,
		intent.OwnerID,
		intent.OwnerGeneration,
		intent.Target.Key,
		intent.Target.SHA256[:],
		intent.ObjectVersion,
		intent.Target.SizeBytes,
		intent.Target.BackendKind,
	)
}

func publicationStoreRetryArgs(
	claim attachments.BlobPublicationCleanupClaim,
	retryAt time.Time,
) func(*testing.T, []any) {
	return publicationStoreExactArgs(
		claim.Intent.PublicationID,
		claim.CleanupOwnerID,
		claim.CleanupGeneration,
		claim.Attempt,
		claim.ObservedLeaseExpiresAt,
		retryAt,
		claim.Intent.ProjectID,
		claim.Intent.OwnerKind,
		claim.Intent.OwnerID,
		claim.Intent.OwnerGeneration,
		claim.Intent.Target.Key,
		claim.Intent.Target.SHA256[:],
		publicationStoreSQLObjectVersion(claim.Intent.ObjectVersion),
		claim.Intent.Target.SizeBytes,
		claim.Intent.Target.BackendKind,
	)
}

func publicationStoreCompletionArgs(
	claim attachments.BlobPublicationCleanupClaim,
	outcome attachments.BlobPublicationCompletionOutcome,
) func(*testing.T, []any) {
	receiptDigest := publicationStoreCleanupReceiptDigest(claim.Intent, outcome)
	return publicationStoreExactArgs(
		claim.Intent.PublicationID,
		claim.CleanupOwnerID,
		claim.CleanupGeneration,
		claim.Attempt,
		claim.ObservedLeaseExpiresAt,
		outcome,
		receiptDigest[:],
		claim.Intent.ProjectID,
		claim.Intent.OwnerKind,
		claim.Intent.OwnerID,
		claim.Intent.OwnerGeneration,
		claim.Intent.Target.Key,
		claim.Intent.Target.SHA256[:],
		publicationStoreSQLObjectVersion(claim.Intent.ObjectVersion),
		claim.Intent.Target.SizeBytes,
		claim.Intent.Target.BackendKind,
	)
}

func publicationStoreSQLObjectVersion(objectVersion string) any {
	if objectVersion == "" {
		return nil
	}
	return objectVersion
}

func publicationStoreCleanupReceiptDigest(
	intent attachments.BlobPublicationIntent,
	outcome attachments.BlobPublicationCompletionOutcome,
) [sha256.Size]byte {
	encoded := make([]byte, 0, 512)
	appendField := func(value []byte) {
		encoded = binary.BigEndian.AppendUint64(encoded, uint64(len(value)))
		encoded = append(encoded, value...)
	}
	appendString := func(value string) {
		appendField([]byte(value))
	}
	appendInt64 := func(value int64) {
		var field [8]byte
		binary.BigEndian.PutUint64(field[:], uint64(value))
		appendField(field[:])
	}

	appendString("houfeng.attachments.blob-publication-cleanup-receipt.v1")
	appendString(intent.PublicationID)
	appendString(intent.ProjectID)
	appendString(string(intent.OwnerKind))
	appendString(intent.OwnerID)
	appendInt64(intent.OwnerGeneration)
	appendString(intent.Target.Key)
	appendField(intent.Target.SHA256[:])
	appendString(intent.ObjectVersion)
	appendInt64(intent.Target.SizeBytes)
	appendString(string(intent.Target.BackendKind))
	appendString(string(outcome))
	return sha256.Sum256(encoded)
}

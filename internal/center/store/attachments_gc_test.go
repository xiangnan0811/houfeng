package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
)

var _ attachments.BlobGCRepository = (*PostgresAttachmentRepository)(nil)

func TestPostgresAttachmentRepositoryClaimsOrdinaryBlobWithDurableMetadataFence(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x11, "local-gc-v1", 9, attachments.BackendKindLocal)
	claim := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModeOrdinary, "gc_worker_1")
	tx := &durableBlobGCTx{
		candidate: &claim.Candidate,
		inserted:  &claim,
	}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
		newBlobGCDeletionID: func() (string, error) {
			return claim.DeletionID, nil
		},
	}
	request := attachments.BlobGCClaimRequest{
		ProjectID: "default", BackendKind: attachments.BackendKindLocal,
		Mode: attachments.BlobGCPurgeModeOrdinary, OrphanedBefore: now.Add(-24 * time.Hour),
		OwnerID: "gc_worker_1", OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration,
	}

	got, err := repository.ClaimBlobGC(context.Background(), request)
	if err != nil {
		t.Fatalf("ClaimBlobGC() error = %v", err)
	}
	if got == nil || *got != claim {
		t.Fatalf("ClaimBlobGC() = %#v, want %#v", got, claim)
	}
	wantSteps := []string{
		"reclaim", "blob_table_lock", "upload_parts_lock", "candidate", "expired_pins",
		"claim_insert", "metadata_delete", "commit", "rollback",
	}
	if !reflect.DeepEqual(tx.steps, wantSteps) {
		t.Fatalf("ClaimBlobGC() steps = %#v, want %#v", tx.steps, wantSteps)
	}
	assertDurableBlobGCCandidateSQL(t, tx.candidateSQL, true)
	if !strings.Contains(tx.claimInsertSQL, "insert into public.blob_gc_deletions") ||
		!strings.Contains(tx.metadataDeleteSQL, "delete from public.blob_objects") {
		t.Fatalf("claim insert/metadata delete SQL missing durable fence:\n%s\n%s", tx.claimInsertSQL, tx.metadataDeleteSQL)
	}
}

func TestPostgresAttachmentRepositoryPermanentClaimBypassesOnlyWatermark(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x22, "s3-gc-v2", 17, attachments.BackendKindS3)
	claim := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModePermanent, "gc_worker_2")
	tx := &durableBlobGCTx{candidate: &claim.Candidate, inserted: &claim}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
		newBlobGCDeletionID: func() (string, error) {
			return claim.DeletionID, nil
		},
	}

	got, err := repository.ClaimBlobGC(context.Background(), attachments.BlobGCClaimRequest{
		ProjectID: "default", BackendKind: attachments.BackendKindS3,
		Mode: attachments.BlobGCPurgeModePermanent, Object: object,
		OwnerID: "gc_worker_2", OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration,
	})
	if err != nil || got == nil {
		t.Fatalf("ClaimBlobGC(permanent) = (%#v, %v)", got, err)
	}
	assertDurableBlobGCCandidateSQL(t, tx.candidateSQL, false)
	for _, fragment := range []string{
		"blob.blob_key = $2", "blob.object_version = $3", "blob.sha256_digest = $4", "blob.size_bytes = $5",
	} {
		if !strings.Contains(tx.candidateSQL, fragment) {
			t.Fatalf("permanent candidate SQL omitted %q:\n%s", fragment, tx.candidateSQL)
		}
	}
}

func TestPostgresAttachmentRepositoryReclaimsDueDeletionWithNewGeneration(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x33, "local-gc-v3", 12, attachments.BackendKindLocal)
	reclaimed := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModeOrdinary, "gc_worker_new")
	reclaimed.OwnerGeneration = 4
	reclaimed.Attempt = 5
	tx := &durableBlobGCTx{reclaimed: &reclaimed}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
		newBlobGCDeletionID: func() (string, error) {
			t.Fatal("reclaim must not allocate a new deletion ID")
			return "", nil
		},
	}

	got, err := repository.ClaimBlobGC(context.Background(), attachments.BlobGCClaimRequest{
		ProjectID: "default", BackendKind: attachments.BackendKindLocal,
		Mode: attachments.BlobGCPurgeModeOrdinary, OrphanedBefore: now.Add(-24 * time.Hour),
		OwnerID: "gc_worker_new", OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration,
	})
	if err != nil || got == nil || *got != reclaimed {
		t.Fatalf("ClaimBlobGC(reclaim) = (%#v, %v), want %#v", got, err, reclaimed)
	}
	if !reflect.DeepEqual(tx.steps, []string{"reclaim", "commit", "rollback"}) {
		t.Fatalf("reclaim steps = %#v", tx.steps)
	}
	for _, fragment := range []string{
		"owner_generation = deletion.owner_generation + 1",
		"attempt = deletion.attempt + 1",
		"deletion_state = 'retry_wait'",
		"lease_expires_at <= transaction_timestamp()",
		"for update skip locked",
	} {
		if !strings.Contains(tx.reclaimSQL, fragment) {
			t.Fatalf("reclaim SQL omitted %q:\n%s", fragment, tx.reclaimSQL)
		}
	}
}

func TestPostgresAttachmentRepositoryCompletesExactClaimAndDecrementsPhysicalQuotaOnce(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x44, "local-gc-v4", 9, attachments.BackendKindLocal)
	claim := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModeOrdinary, "gc_worker_4")
	receipt := attachments.DeletionReceipt{Version: storeObjectVersion(object), Deleted: true}
	tx := &durableBlobGCTx{
		stored:       &durableBlobGCStored{claim: claim, state: "claimed"},
		quotaUsage:   attachments.QuotaUsage{LogicalBytes: 30, ReservedBytes: 2, PhysicalBytes: 29},
		quotaVersion: 4,
	}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	result, err := repository.CompleteBlobGC(context.Background(), attachments.BlobGCCompletionRequest{
		Claim: claim, Receipt: receipt,
	})
	if err != nil {
		t.Fatalf("CompleteBlobGC() error = %v", err)
	}
	if result.DeletionID != claim.DeletionID || result.Candidate != claim.Candidate || result.Receipt != receipt {
		t.Fatalf("CompleteBlobGC() = %#v", result)
	}
	wantSteps := []string{"claim_lock", "quota_lock", "claim_complete", "quota_update", "commit", "rollback"}
	if !reflect.DeepEqual(tx.steps, wantSteps) {
		t.Fatalf("CompleteBlobGC() steps = %#v, want %#v", tx.steps, wantSteps)
	}
	if tx.updatedUsage != (attachments.QuotaUsage{LogicalBytes: 30, ReservedBytes: 2, PhysicalBytes: 20}) {
		t.Fatalf("updated quota = %#v", tx.updatedUsage)
	}
	if !strings.Contains(tx.claimCompleteSQL, "owner_generation = $3") ||
		!strings.Contains(tx.claimCompleteSQL, "deletion_state = 'claimed'") ||
		!strings.Contains(tx.claimCompleteSQL, "receipt_digest = $8") {
		t.Fatalf("completion SQL omitted exact owner/result CAS:\n%s", tx.claimCompleteSQL)
	}
}

func TestPostgresAttachmentRepositoryCompletionReplayDoesNotDecrementQuotaTwice(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x55, "s3-gc-v5", 15, attachments.BackendKindS3)
	claim := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModePermanent, "gc_worker_5")
	receipt := attachments.DeletionReceipt{Version: storeObjectVersion(object), Deleted: false}
	digest := blobGCReceiptDigest(claim, receipt)
	resultCode := "already_absent"
	completedAt := now.Add(time.Second)
	tx := &durableBlobGCTx{stored: &durableBlobGCStored{
		claim: claim, state: "completed", physicalResult: &resultCode,
		receiptDigest: digest[:], completedAt: &completedAt,
	}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	result, err := repository.CompleteBlobGC(context.Background(), attachments.BlobGCCompletionRequest{
		Claim: claim, Receipt: receipt,
	})
	if err != nil || result.Receipt != receipt {
		t.Fatalf("CompleteBlobGC(replay) = (%#v, %v)", result, err)
	}
	if !reflect.DeepEqual(tx.steps, []string{"claim_lock", "commit", "rollback"}) {
		t.Fatalf("completion replay steps = %#v", tx.steps)
	}
}

func TestPostgresAttachmentRepositoryRejectsStaleCompletionOwner(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x66, "local-gc-v6", 11, attachments.BackendKindLocal)
	claim := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModeOrdinary, "gc_worker_old")
	current := claim
	current.OwnerID = "gc_worker_new"
	current.OwnerGeneration++
	tx := &durableBlobGCTx{stored: &durableBlobGCStored{claim: current, state: "claimed"}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	_, err := repository.CompleteBlobGC(context.Background(), attachments.BlobGCCompletionRequest{
		Claim: claim, Receipt: attachments.DeletionReceipt{Version: storeObjectVersion(object), Deleted: true},
	})
	if !errors.Is(err, attachments.ErrBlobGCClaimLost) {
		t.Fatalf("CompleteBlobGC(stale) error = %v, want ErrBlobGCClaimLost", err)
	}
	if slicesContain(tx.steps, "quota_lock") || slicesContain(tx.steps, "quota_update") {
		t.Fatalf("stale completion touched quota: %#v", tx.steps)
	}
}

func TestPostgresAttachmentRepositorySchedulesRetryWithExactOwnerFence(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	claim := storeBlobGCTestClaim(now, storeBlobGCTestObject(0x77, "local-gc-v7", 8, attachments.BackendKindLocal), attachments.BlobGCPurgeModeOrdinary, "gc_worker_7")
	tx := &durableBlobGCTx{}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	err := repository.RetryBlobGC(context.Background(), attachments.BlobGCRetryRequest{
		Claim: claim, RetryAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("RetryBlobGC() error = %v", err)
	}
	if !reflect.DeepEqual(tx.steps, []string{"claim_retry", "commit", "rollback"}) {
		t.Fatalf("retry steps = %#v", tx.steps)
	}
	for _, fragment := range []string{
		"deletion_id = $1", "owner_id = $2", "owner_generation = $3",
		"lease_expires_at = $4", "deletion_state = 'claimed'", "retry_at = $5",
	} {
		if !strings.Contains(tx.claimRetrySQL, fragment) {
			t.Fatalf("retry SQL omitted %q:\n%s", fragment, tx.claimRetrySQL)
		}
	}
}

func TestPostgresAttachmentRepositoryResolvesOnlyExactTerminalReceipt(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := storeBlobGCTestObject(0x88, "s3-gc-v8", 18, attachments.BackendKindS3)
	claim := storeBlobGCTestClaim(now, object, attachments.BlobGCPurgeModeOrdinary, "gc_worker_8")
	receipt := attachments.DeletionReceipt{Version: storeObjectVersion(object), Deleted: true}
	digest := blobGCReceiptDigest(claim, receipt)
	resultCode := "deleted"
	completedAt := now.Add(time.Second)
	tx := &durableBlobGCTx{stored: &durableBlobGCStored{
		claim: claim, state: "completed", physicalResult: &resultCode,
		receiptDigest: digest[:], completedAt: &completedAt,
	}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	result, err := repository.ResolveBlobGC(context.Background(), attachments.BlobGCResolveRequest{
		Claim: claim, Receipt: receipt,
	})
	if err != nil || result == nil || result.Receipt != receipt {
		t.Fatalf("ResolveBlobGC() = (%#v, %v)", result, err)
	}
	if !reflect.DeepEqual(tx.steps, []string{"claim_resolve", "commit", "rollback"}) {
		t.Fatalf("resolve steps = %#v", tx.steps)
	}
}

func TestEnsureAttachmentBlobRejectsActiveDurableDeletionFence(t *testing.T) {
	object := storeBlobGCTestObject(0x99, "local-gc-v9", 19, attachments.BackendKindLocal)
	tx := &blobGCPublicationFenceTx{active: true}

	inserted, err := ensureAttachmentBlob(context.Background(), tx, object)
	if inserted || !errors.Is(err, attachments.ErrBlobGCProtected) {
		t.Fatalf("ensureAttachmentBlob(active fence) = (%t, %v), want false/ErrBlobGCProtected", inserted, err)
	}
	if want := []string{"blob_table_lock", "active_fence_check"}; !reflect.DeepEqual(tx.steps, want) {
		t.Fatalf("ensureAttachmentBlob(active fence) steps = %#v, want %#v", tx.steps, want)
	}
	if tx.insertSQL != "" {
		t.Fatalf("ensureAttachmentBlob(active fence) attempted insert:\n%s", tx.insertSQL)
	}
}

func TestEnsureAttachmentBlobLocksBeforePublishingWithoutDeletionFence(t *testing.T) {
	object := storeBlobGCTestObject(0x98, "local-gc-v8", 18, attachments.BackendKindLocal)
	tx := &blobGCPublicationFenceTx{inserted: true}

	inserted, err := ensureAttachmentBlob(context.Background(), tx, object)
	if err != nil || !inserted {
		t.Fatalf("ensureAttachmentBlob(no fence) = (%t, %v), want true/nil", inserted, err)
	}
	if want := []string{"blob_table_lock", "active_fence_check", "insert"}; !reflect.DeepEqual(tx.steps, want) {
		t.Fatalf("ensureAttachmentBlob(no fence) steps = %#v, want %#v", tx.steps, want)
	}
	for _, fragment := range []string{
		"insert into public.blob_objects", "select $1, $2, $3, $4, $5",
		"from public.blob_gc_deletions", "deletion_state <> 'completed'",
	} {
		if !strings.Contains(tx.insertSQL, fragment) {
			t.Fatalf("Blob metadata insert SQL omitted %q:\n%s", fragment, tx.insertSQL)
		}
	}
}

func TestEnsureAttachmentUploadPartRejectsActiveDurableDeletionFence(t *testing.T) {
	object := storeBlobGCTestObject(0xaa, "s3-gc-v10", 23, attachments.BackendKindS3)
	tx := &blobGCPublicationFenceTx{active: true}

	err := ensureAttachmentUploadPart(context.Background(), tx, "aup_gcfence", storeObjectVersion(object))
	if !errors.Is(err, attachments.ErrBlobGCProtected) {
		t.Fatalf("ensureAttachmentUploadPart(active fence) error = %v, want ErrBlobGCProtected", err)
	}
	if want := []string{"upload_parts_table_lock", "active_fence_check"}; !reflect.DeepEqual(tx.steps, want) {
		t.Fatalf("ensureAttachmentUploadPart(active fence) steps = %#v, want %#v", tx.steps, want)
	}
	if tx.insertSQL != "" {
		t.Fatalf("ensureAttachmentUploadPart(active fence) attempted insert:\n%s", tx.insertSQL)
	}
}

func TestEnsureAttachmentUploadPartLocksBeforePublishingWithoutDeletionFence(t *testing.T) {
	object := storeBlobGCTestObject(0xab, "s3-gc-v11", 24, attachments.BackendKindS3)
	tx := &blobGCPublicationFenceTx{inserted: true}

	err := ensureAttachmentUploadPart(context.Background(), tx, "aup_gcpublish", storeObjectVersion(object))
	if err != nil {
		t.Fatalf("ensureAttachmentUploadPart(no fence) error = %v", err)
	}
	if want := []string{"upload_parts_table_lock", "active_fence_check", "insert"}; !reflect.DeepEqual(tx.steps, want) {
		t.Fatalf("ensureAttachmentUploadPart(no fence) steps = %#v, want %#v", tx.steps, want)
	}
	for _, fragment := range []string{
		"insert into public.attachment_upload_parts", "select $1, 1, $2, $3, $4",
		"from public.blob_gc_deletions", "deletion_state <> 'completed'",
	} {
		if !strings.Contains(tx.insertSQL, fragment) {
			t.Fatalf("upload-part insert SQL omitted %q:\n%s", fragment, tx.insertSQL)
		}
	}
}

func assertDurableBlobGCCandidateSQL(t *testing.T, sql string, ordinary bool) {
	t.Helper()
	for _, fragment := range []string{
		"attachment.blob_key = blob.blob_key",
		"attachment.preview_blob_key = blob.blob_key",
		"from public.record_revision_attachments",
		"from public.attachment_upload_parts",
		"pin.expires_at > transaction_timestamp()",
		"from public.blob_gc_deletions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("candidate SQL omitted %q:\n%s", fragment, sql)
		}
	}
	if strings.Contains(sql, "for update") {
		t.Fatalf("candidate SQL requires UPDATE privilege on immutable Blob metadata:\n%s", sql)
	}
	if hasWatermark := strings.Contains(sql, "blob.created_at <= $2"); hasWatermark != ordinary {
		t.Fatalf("candidate SQL watermark present = %t, want %t:\n%s", hasWatermark, ordinary, sql)
	}
	if hasDatabaseWatermark := strings.Contains(sql, "blob.created_at <= transaction_timestamp() - interval '24 hours'"); hasDatabaseWatermark != ordinary {
		t.Fatalf("candidate SQL database watermark present = %t, want %t:\n%s", hasDatabaseWatermark, ordinary, sql)
	}
}

type durableBlobGCStored struct {
	claim          attachments.BlobGCClaim
	state          string
	retryAt        *time.Time
	physicalResult *string
	receiptDigest  []byte
	completedAt    *time.Time
}

type blobGCPublicationFenceTx struct {
	pgx.Tx
	active    bool
	inserted  bool
	insertSQL string
	steps     []string
}

func (tx *blobGCPublicationFenceTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	if strings.HasPrefix(compact, "lock table public.blob_objects") {
		tx.steps = append(tx.steps, "blob_table_lock")
		return pgconn.NewCommandTag("LOCK TABLE"), nil
	}
	if strings.HasPrefix(compact, "lock table public.attachment_upload_parts") {
		tx.steps = append(tx.steps, "upload_parts_table_lock")
		return pgconn.NewCommandTag("LOCK TABLE"), nil
	}
	if strings.HasPrefix(compact, "insert into public.blob_objects") ||
		strings.HasPrefix(compact, "insert into public.attachment_upload_parts") {
		tx.steps = append(tx.steps, "insert")
		tx.insertSQL = compact
		if tx.inserted {
			return pgconn.NewCommandTag("INSERT 1"), nil
		}
		return pgconn.NewCommandTag("INSERT 0"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("unexpected publication fence exec: %s", compact)
}

func (tx *blobGCPublicationFenceTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	if strings.Contains(compact, "from public.blob_gc_deletions") {
		tx.steps = append(tx.steps, "active_fence_check")
		return durableBlobGCRow{values: []any{tx.active}}
	}
	if strings.Contains(compact, "from public.blob_objects") ||
		strings.Contains(compact, "from public.attachment_upload_parts") {
		return durableBlobGCRow{err: pgx.ErrNoRows}
	}
	return durableBlobGCRow{err: fmt.Errorf("unexpected publication fence query: %s", compact)}
}

func (*blobGCPublicationFenceTx) Commit(context.Context) error   { return nil }
func (*blobGCPublicationFenceTx) Rollback(context.Context) error { return nil }

type durableBlobGCTx struct {
	pgx.Tx
	steps             []string
	reclaimed         *attachments.BlobGCClaim
	candidate         *attachments.BlobGCCandidate
	inserted          *attachments.BlobGCClaim
	stored            *durableBlobGCStored
	quotaUsage        attachments.QuotaUsage
	quotaVersion      int64
	updatedUsage      attachments.QuotaUsage
	reclaimSQL        string
	candidateSQL      string
	claimInsertSQL    string
	metadataDeleteSQL string
	claimCompleteSQL  string
	claimRetrySQL     string
	committed         bool
}

func (tx *durableBlobGCTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "update public.blob_gc_deletions as deletion") && strings.Contains(compact, "owner_generation = deletion.owner_generation + 1"):
		tx.steps = append(tx.steps, "reclaim")
		tx.reclaimSQL = compact
		return durableBlobGCClaimRow(tx.reclaimed)
	case strings.Contains(compact, "from public.blob_objects as blob") && strings.Contains(compact, "order by blob.created_at"):
		tx.steps = append(tx.steps, "candidate")
		tx.candidateSQL = compact
		if tx.candidate == nil {
			return durableBlobGCRow{err: pgx.ErrNoRows}
		}
		return durableBlobGCRow{values: []any{
			tx.candidate.Object.Key, tx.candidate.Object.SHA256[:], tx.candidate.Object.ObjectVersion,
			tx.candidate.Object.SizeBytes, tx.candidate.Object.BackendKind, tx.candidate.CreatedAt,
		}}
	case strings.Contains(compact, "insert into public.blob_gc_deletions"):
		tx.steps = append(tx.steps, "claim_insert")
		tx.claimInsertSQL = compact
		return durableBlobGCClaimRow(tx.inserted)
	case strings.Contains(compact, "from public.blob_gc_deletions as deletion") && strings.Contains(compact, "for update"):
		tx.steps = append(tx.steps, "claim_lock")
		return durableBlobGCStoredRow(tx.stored)
	case strings.Contains(compact, "from public.attachment_quota_accounts") && strings.Contains(compact, "for update"):
		tx.steps = append(tx.steps, "quota_lock")
		return durableBlobGCRow{values: []any{
			tx.quotaUsage.LogicalBytes, tx.quotaUsage.ReservedBytes,
			tx.quotaUsage.PhysicalBytes, tx.quotaVersion,
		}}
	case strings.Contains(compact, "from public.blob_gc_deletions as deletion") && strings.Contains(compact, "deletion.deletion_state = 'completed'"):
		tx.steps = append(tx.steps, "claim_resolve")
		return durableBlobGCStoredRow(tx.stored)
	default:
		return durableBlobGCRow{err: fmt.Errorf("unexpected durable Blob GC query: %s", compact)}
	}
}

func (tx *durableBlobGCTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.HasPrefix(compact, "lock table public.blob_objects"):
		tx.steps = append(tx.steps, "blob_table_lock")
		return pgconn.NewCommandTag("LOCK TABLE"), nil
	case strings.HasPrefix(compact, "lock table public.attachment_upload_parts"):
		tx.steps = append(tx.steps, "upload_parts_lock")
		return pgconn.NewCommandTag("LOCK TABLE"), nil
	case strings.HasPrefix(compact, "delete from public.blob_gc_pins"):
		tx.steps = append(tx.steps, "expired_pins")
		return pgconn.NewCommandTag("DELETE 0"), nil
	case strings.HasPrefix(compact, "delete from public.blob_objects"):
		tx.steps = append(tx.steps, "metadata_delete")
		tx.metadataDeleteSQL = compact
		return pgconn.NewCommandTag("DELETE 1"), nil
	case strings.HasPrefix(compact, "update public.blob_gc_deletions") && strings.Contains(compact, "deletion_state = 'completed'"):
		tx.steps = append(tx.steps, "claim_complete")
		tx.claimCompleteSQL = compact
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(compact, "update public.blob_gc_deletions") && strings.Contains(compact, "deletion_state = 'retry_wait'"):
		tx.steps = append(tx.steps, "claim_retry")
		tx.claimRetrySQL = compact
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(compact, "update public.attachment_quota_accounts"):
		tx.steps = append(tx.steps, "quota_update")
		tx.updatedUsage = attachments.QuotaUsage{
			LogicalBytes: args[1].(int64), ReservedBytes: args[2].(int64), PhysicalBytes: args[3].(int64),
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected durable Blob GC exec: %s", compact)
	}
}

func (tx *durableBlobGCTx) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	tx.committed = true
	return nil
}

func (tx *durableBlobGCTx) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback")
	return nil
}

type durableBlobGCRow struct {
	values []any
	err    error
}

func (row durableBlobGCRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return fmt.Errorf("durable Blob GC scan destination count %d, want %d", len(dest), len(row.values))
	}
	for index := range dest {
		target := reflect.ValueOf(dest[index])
		value := reflect.ValueOf(row.values[index])
		if target.Kind() != reflect.Pointer || !value.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("durable Blob GC scan destination %d type mismatch: %v -> %v", index, value.Type(), target.Elem().Type())
		}
		target.Elem().Set(value)
	}
	return nil
}

func durableBlobGCClaimRow(claim *attachments.BlobGCClaim) pgx.Row {
	if claim == nil {
		return durableBlobGCRow{err: pgx.ErrNoRows}
	}
	return durableBlobGCRow{values: durableBlobGCClaimValues(*claim)}
}

func durableBlobGCStoredRow(stored *durableBlobGCStored) pgx.Row {
	if stored == nil {
		return durableBlobGCRow{err: pgx.ErrNoRows}
	}
	values := durableBlobGCClaimValues(stored.claim)
	values = append(values, stored.state, stored.retryAt, stored.physicalResult, stored.receiptDigest, stored.completedAt)
	return durableBlobGCRow{values: values}
}

func durableBlobGCClaimValues(claim attachments.BlobGCClaim) []any {
	return []any{
		claim.DeletionID, claim.ProjectID, claim.Mode, claim.Candidate.Object.Key,
		claim.Candidate.Object.SHA256[:], claim.Candidate.Object.ObjectVersion,
		claim.Candidate.Object.SizeBytes, claim.Candidate.Object.BackendKind,
		claim.Candidate.CreatedAt, claim.OwnerID, claim.OwnerGeneration, claim.Attempt,
		claim.LeaseExpiresAt,
	}
}

func storeBlobGCTestClaim(
	now time.Time,
	object attachments.BlobObject,
	mode attachments.BlobGCPurgeMode,
	ownerID string,
) attachments.BlobGCClaim {
	return attachments.BlobGCClaim{
		DeletionID: "bgd_0123456789abcdef", ProjectID: "default", Mode: mode,
		Candidate: attachments.BlobGCCandidate{Object: object, CreatedAt: now.Add(-48 * time.Hour)},
		OwnerID:   ownerID, OwnerGeneration: 1, Attempt: 1,
		LeaseExpiresAt: now.Add(attachments.DefaultBlobGCLeaseDuration),
	}
}

func storeBlobGCTestObject(fill byte, version string, size int64, backend attachments.BackendKind) attachments.BlobObject {
	var digest [sha256.Size]byte
	copy(digest[:], bytes.Repeat([]byte{fill}, sha256.Size))
	return attachments.BlobObject{
		Key: fmt.Sprintf("sha256/%x", digest), SHA256: digest, ObjectVersion: version,
		SizeBytes: size, BackendKind: backend,
	}
}

func storeObjectVersion(object attachments.BlobObject) attachments.ObjectVersion {
	return attachments.ObjectVersion{
		Key: object.Key, VersionID: object.ObjectVersion, SHA256: object.SHA256, SizeBytes: object.SizeBytes,
	}
}

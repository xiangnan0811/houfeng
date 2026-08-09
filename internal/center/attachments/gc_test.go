package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestBlobGCWorkerCommitsClaimBeforePhysicalDeleteAndCompletion(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	claim := blobGCTestClaim(now, blobGCTestObject(0x11, "local-gc-v1", 9, BackendKindLocal))
	events := make([]string, 0, 3)
	repository := &blobGCRepositoryStub{claim: &claim, events: &events}
	blob := &blobGCStoreStub{events: &events}
	worker, err := NewBlobGCWorker(repository, blob, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindLocal, OwnerID: "gc_worker_1",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	collected, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !collected {
		t.Fatal("RunOnce() collected = false, want true")
	}
	wantEvents := []string{"claim_committed", "physical_delete", "completion_committed"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("RunOnce() events = %#v, want %#v", events, wantEvents)
	}
	if len(repository.claimRequests) != 1 {
		t.Fatalf("claim requests = %d, want 1", len(repository.claimRequests))
	}
	request := repository.claimRequests[0]
	if request.Mode != BlobGCPurgeModeOrdinary || request.ProjectID != "default" ||
		request.BackendKind != BackendKindLocal || request.OwnerID != "gc_worker_1" ||
		request.OwnerLeaseDuration != DefaultBlobGCLeaseDuration ||
		!request.OrphanedBefore.Equal(now.Add(-DefaultBlobGCOrphanGracePeriod)) ||
		request.Object != (BlobObject{}) {
		t.Fatalf("ordinary claim request = %#v", request)
	}
	if len(repository.completions) != 1 || repository.completions[0].Claim != claim ||
		repository.completions[0].Receipt.Version != objectVersionFromBlobObject(claim.Candidate.Object) {
		t.Fatalf("completion = %#v", repository.completions)
	}
}

func TestBlobGCWorkerPermanentPurgeBypassesOnlyWatermark(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	object := blobGCTestObject(0x22, "s3-gc-v2", 17, BackendKindS3)
	claim := blobGCTestClaim(now, object)
	claim.Mode = BlobGCPurgeModePermanent
	claim.OwnerID = "gc_worker_2"
	repository := &blobGCRepositoryStub{claim: &claim}
	worker, err := NewBlobGCWorker(repository, &blobGCStoreStub{}, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindS3, OwnerID: "gc_worker_2",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	result, err := worker.PurgePermanent(context.Background(), object)
	if err != nil {
		t.Fatalf("PurgePermanent() error = %v", err)
	}
	if result.DeletionID != claim.DeletionID || result.Candidate.Object != object || !result.Receipt.Deleted {
		t.Fatalf("PurgePermanent() = %#v", result)
	}
	request := repository.claimRequests[0]
	if request.Mode != BlobGCPurgeModePermanent || request.OrphanedBefore != (time.Time{}) ||
		request.Object != object || request.BackendKind != object.BackendKind {
		t.Fatalf("permanent claim request = %#v", request)
	}
}

func TestBlobGCWorkerSchedulesRetryAfterPhysicalDeleteFailure(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	claim := blobGCTestClaim(now, blobGCTestObject(0x33, "local-gc-v3", 12, BackendKindLocal))
	claim.OwnerID = "gc_worker_3"
	deleteErr := errors.New("injected delete failure")
	repository := &blobGCRepositoryStub{claim: &claim}
	blob := &blobGCStoreStub{deleteErr: deleteErr}
	worker, err := NewBlobGCWorker(repository, blob, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindLocal, OwnerID: "gc_worker_3",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	collected, err := worker.RunOnce(context.Background())
	if !collected || !errors.Is(err, deleteErr) {
		t.Fatalf("RunOnce(delete failure) = (%t, %v), want true/delete error", collected, err)
	}
	if len(repository.completions) != 0 || len(repository.retries) != 1 {
		t.Fatalf("completion/retry counts = %d/%d, want 0/1", len(repository.completions), len(repository.retries))
	}
	retry := repository.retries[0]
	if retry.Claim != claim || !retry.RetryAt.Equal(now.Add(DefaultBlobGCRetryDelay)) {
		t.Fatalf("retry request = %#v", retry)
	}
}

func TestBlobGCWorkerCompletesAlreadyAbsentExactObject(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	claim := blobGCTestClaim(now, blobGCTestObject(0x44, "local-gc-v4", 12, BackendKindLocal))
	claim.OwnerID = "gc_worker_4"
	repository := &blobGCRepositoryStub{claim: &claim}
	blob := &blobGCStoreStub{alreadyAbsent: true}
	worker, err := NewBlobGCWorker(repository, blob, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindLocal, OwnerID: "gc_worker_4",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	collected, err := worker.RunOnce(context.Background())
	if err != nil || !collected {
		t.Fatalf("RunOnce(already absent) = (%t, %v), want true/nil", collected, err)
	}
	if len(repository.completions) != 1 || repository.completions[0].Receipt.Deleted {
		t.Fatalf("completion receipt = %#v, want exact already-absent receipt", repository.completions)
	}
}

func TestBlobGCWorkerResolvesCompletionCommitAmbiguity(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	claim := blobGCTestClaim(now, blobGCTestObject(0x55, "s3-gc-v5", 21, BackendKindS3))
	claim.OwnerID = "gc_worker_5"
	receipt := DeletionReceipt{Version: objectVersionFromBlobObject(claim.Candidate.Object), Deleted: true}
	resolved := blobGCTestResult(claim, receipt)
	commitErr := errors.New("injected ambiguous commit acknowledgement")
	repository := &blobGCRepositoryStub{claim: &claim, completeErr: commitErr, resolved: &resolved}
	worker, err := NewBlobGCWorker(repository, &blobGCStoreStub{}, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindS3, OwnerID: "gc_worker_5",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	collected, err := worker.RunOnce(context.Background())
	if err != nil || !collected {
		t.Fatalf("RunOnce(ambiguous completion) = (%t, %v), want true/nil", collected, err)
	}
	if len(repository.resolutions) != 1 || repository.resolutions[0] != (BlobGCResolveRequest{Claim: claim, Receipt: receipt}) {
		t.Fatalf("resolve requests = %#v", repository.resolutions)
	}
}

func TestBlobGCWorkerRejectsMismatchedPhysicalDeleteReceipt(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	claim := blobGCTestClaim(now, blobGCTestObject(0x66, "local-gc-v6", 12, BackendKindLocal))
	claim.OwnerID = "gc_worker_6"
	wrong := objectVersionFromBlobObject(blobGCTestObject(0x77, "local-wrong-v1", 12, BackendKindLocal))
	repository := &blobGCRepositoryStub{claim: &claim}
	blob := &blobGCStoreStub{receiptVersion: wrong}
	worker, err := NewBlobGCWorker(repository, blob, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindLocal, OwnerID: "gc_worker_6", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}

	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, ErrBlobGCConflict) {
		t.Fatalf("RunOnce() error = %v, want ErrBlobGCConflict", err)
	}
	if len(repository.completions) != 0 || len(repository.retries) != 1 {
		t.Fatalf("completion/retry counts = %d/%d, want 0/1", len(repository.completions), len(repository.retries))
	}
}

func TestBlobGCWorkerDoesNotClaimAfterCancellation(t *testing.T) {
	repository := &blobGCRepositoryStub{}
	worker, err := NewBlobGCWorker(repository, &blobGCStoreStub{}, BlobGCWorkerOptions{
		ProjectID: "default", BackendKind: BackendKindLocal, OwnerID: "gc_worker_7",
	})
	if err != nil {
		t.Fatalf("NewBlobGCWorker() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if collected, err := worker.RunOnce(ctx); collected || !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce(cancelled) = (%t, %v), want false/context.Canceled", collected, err)
	}
	if len(repository.claimRequests) != 0 {
		t.Fatalf("claim requests = %d, want 0", len(repository.claimRequests))
	}
}

type blobGCRepositoryStub struct {
	claimRequests []BlobGCClaimRequest
	completions   []BlobGCCompletionRequest
	retries       []BlobGCRetryRequest
	resolutions   []BlobGCResolveRequest
	claim         *BlobGCClaim
	resolved      *BlobGCPurgeResult
	claimErr      error
	completeErr   error
	retryErr      error
	resolveErr    error
	events        *[]string
}

func (stub *blobGCRepositoryStub) ClaimBlobGC(
	_ context.Context,
	request BlobGCClaimRequest,
) (*BlobGCClaim, error) {
	stub.claimRequests = append(stub.claimRequests, request)
	if stub.claimErr != nil || stub.claim == nil {
		return nil, stub.claimErr
	}
	if stub.events != nil {
		*stub.events = append(*stub.events, "claim_committed")
	}
	claim := *stub.claim
	return &claim, nil
}

func (stub *blobGCRepositoryStub) CompleteBlobGC(
	_ context.Context,
	request BlobGCCompletionRequest,
) (BlobGCPurgeResult, error) {
	stub.completions = append(stub.completions, request)
	if stub.completeErr != nil {
		return BlobGCPurgeResult{}, stub.completeErr
	}
	if stub.events != nil {
		*stub.events = append(*stub.events, "completion_committed")
	}
	return blobGCTestResult(request.Claim, request.Receipt), nil
}

func (stub *blobGCRepositoryStub) RetryBlobGC(_ context.Context, request BlobGCRetryRequest) error {
	stub.retries = append(stub.retries, request)
	if stub.events != nil {
		*stub.events = append(*stub.events, "retry_committed")
	}
	return stub.retryErr
}

func (stub *blobGCRepositoryStub) ResolveBlobGC(
	_ context.Context,
	request BlobGCResolveRequest,
) (*BlobGCPurgeResult, error) {
	stub.resolutions = append(stub.resolutions, request)
	if stub.resolveErr != nil || stub.resolved == nil {
		return nil, stub.resolveErr
	}
	result := *stub.resolved
	return &result, nil
}

type blobGCStoreStub struct {
	deleteCalls    int
	deleted        ObjectVersion
	receiptVersion ObjectVersion
	deleteErr      error
	alreadyAbsent  bool
	events         *[]string
}

func (*blobGCStoreStub) Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error) {
	return ObjectVersion{}, errors.New("unexpected put")
}

func (*blobGCStoreStub) Open(context.Context, ObjectVersion, ByteRange) (io.ReadCloser, error) {
	return nil, errors.New("unexpected open")
}

func (*blobGCStoreStub) Stat(context.Context, ObjectVersion) (ObjectInfo, error) {
	return ObjectInfo{}, errors.New("unexpected stat")
}

func (stub *blobGCStoreStub) Delete(_ context.Context, version ObjectVersion) (DeletionReceipt, error) {
	stub.deleteCalls++
	stub.deleted = version
	if stub.events != nil {
		*stub.events = append(*stub.events, "physical_delete")
	}
	if stub.deleteErr != nil {
		return DeletionReceipt{Version: version}, stub.deleteErr
	}
	receiptVersion := stub.receiptVersion
	if receiptVersion == (ObjectVersion{}) {
		receiptVersion = version
	}
	return DeletionReceipt{Version: receiptVersion, Deleted: !stub.alreadyAbsent}, nil
}

func blobGCTestClaim(now time.Time, object BlobObject) BlobGCClaim {
	return BlobGCClaim{
		DeletionID: "bgd_0123456789abcdef", ProjectID: "default", Mode: BlobGCPurgeModeOrdinary,
		Candidate: BlobGCCandidate{Object: object, CreatedAt: now.Add(-48 * time.Hour)},
		OwnerID:   "gc_worker_1", OwnerGeneration: 1, Attempt: 1,
		LeaseExpiresAt: now.Add(DefaultBlobGCLeaseDuration),
	}
}

func blobGCTestResult(claim BlobGCClaim, receipt DeletionReceipt) BlobGCPurgeResult {
	return BlobGCPurgeResult{DeletionID: claim.DeletionID, Candidate: claim.Candidate, Receipt: receipt}
}

func blobGCTestObject(fill byte, version string, size int64, backend BackendKind) BlobObject {
	var digest [sha256.Size]byte
	copy(digest[:], bytes.Repeat([]byte{fill}, sha256.Size))
	return BlobObject{
		Key: "sha256/" + hexDigest(digest), SHA256: digest, ObjectVersion: version,
		SizeBytes: size, BackendKind: backend,
	}
}

func objectVersionFromBlobObject(object BlobObject) ObjectVersion {
	return ObjectVersion{
		Key: object.Key, VersionID: object.ObjectVersion, SHA256: object.SHA256, SizeBytes: object.SizeBytes,
	}
}

package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type publicationReconcilerRepositoryStub struct {
	claim       *BlobPublicationCleanupClaim
	claimErr    error
	versionErr  error
	completeErr error
	retryErr    error
	claimed     int
	versioned   []BlobPublicationCleanupVersionRequest
	completed   []BlobPublicationCleanupCompletionRequest
	retried     []BlobPublicationCleanupRetryRequest
	result      BlobPublicationCleanupResult
}

func (repository *publicationReconcilerRepositoryStub) ClaimBlobPublicationCleanup(
	_ context.Context,
	_ BlobPublicationCleanupClaimRequest,
) (*BlobPublicationCleanupClaim, error) {
	repository.claimed++
	if repository.claimErr != nil {
		return nil, repository.claimErr
	}
	if repository.claim == nil {
		return nil, nil
	}
	claim := *repository.claim
	return &claim, nil
}

func (repository *publicationReconcilerRepositoryStub) RecordBlobPublicationCleanupVersion(
	_ context.Context,
	request BlobPublicationCleanupVersionRequest,
) (BlobPublicationCleanupClaim, error) {
	repository.versioned = append(repository.versioned, request)
	if repository.versionErr != nil {
		return BlobPublicationCleanupClaim{}, repository.versionErr
	}
	claim := request.Claim
	claim.Intent.ObjectVersion = request.Object.VersionID
	return claim, nil
}

func (repository *publicationReconcilerRepositoryStub) RetryBlobPublicationCleanup(
	_ context.Context,
	request BlobPublicationCleanupRetryRequest,
) error {
	repository.retried = append(repository.retried, request)
	return repository.retryErr
}

func (repository *publicationReconcilerRepositoryStub) CompleteBlobPublicationCleanup(
	_ context.Context,
	request BlobPublicationCleanupCompletionRequest,
) (BlobPublicationCleanupResult, error) {
	repository.completed = append(repository.completed, request)
	if repository.completeErr != nil {
		return BlobPublicationCleanupResult{}, repository.completeErr
	}
	if repository.result == (BlobPublicationCleanupResult{}) {
		return newBlobPublicationReconcilerResult(request), nil
	}
	return repository.result, nil
}

type publicationResolverStub struct {
	targets []BlobPublicationTarget
	object  ObjectVersion
	err     error
}

func (resolver *publicationResolverStub) ResolveBlobPublicationObject(
	_ context.Context,
	target BlobPublicationTarget,
) (ObjectVersion, error) {
	resolver.targets = append(resolver.targets, target)
	if resolver.err != nil {
		return ObjectVersion{}, resolver.err
	}
	return resolver.object, nil
}

type publicationBlobStoreStub struct {
	deleted []ObjectVersion
	receipt DeletionReceipt
	err     error
}

func (store *publicationBlobStoreStub) Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error) {
	return ObjectVersion{}, ErrInvalidBlobRequest
}

func (store *publicationBlobStoreStub) Open(context.Context, ObjectVersion, ByteRange) (io.ReadCloser, error) {
	return nil, ErrInvalidBlobRequest
}

func (store *publicationBlobStoreStub) Stat(context.Context, ObjectVersion) (ObjectInfo, error) {
	return ObjectInfo{}, ErrInvalidBlobRequest
}

func (store *publicationBlobStoreStub) Delete(_ context.Context, object ObjectVersion) (DeletionReceipt, error) {
	store.deleted = append(store.deleted, object)
	if store.err != nil {
		return DeletionReceipt{}, store.err
	}
	if store.receipt == (DeletionReceipt{}) {
		return DeletionReceipt{Version: object, Deleted: true}, nil
	}
	return store.receipt, nil
}

func TestBlobPublicationReconcilerCompletesUnresolvedAlreadyAbsentWithoutFabricatingVersion(t *testing.T) {
	claim := testBlobPublicationCleanupClaim("")
	claim.Intent.Target.BackendKind = BackendKindS3
	repository := &publicationReconcilerRepositoryStub{claim: &claim}
	resolver := &publicationResolverStub{err: ErrBlobNotFound}
	blob := &publicationBlobStoreStub{}
	reconciler, err := NewBlobPublicationReconciler(repository, resolver, blob, BlobPublicationReconcilerConfig{
		ProjectID: "default", BackendKind: BackendKindS3, CleanupOwnerID: "publication_reconciler_1",
		RetryDelay: time.Minute, Now: func() time.Time { return claim.ObservedLeaseExpiresAt.Add(-time.Minute) },
	})
	if err != nil {
		t.Fatalf("NewBlobPublicationReconciler() error = %v", err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce() = (%t, %v), want terminal absent success", claimed, err)
	}
	if len(blob.deleted) != 0 || len(repository.versioned) != 0 {
		t.Fatalf("unresolved absence performed physical/version mutation: deleted=%#v versioned=%#v", blob.deleted, repository.versioned)
	}
	if len(repository.completed) != 1 {
		t.Fatalf("completion calls = %d, want 1", len(repository.completed))
	}
	completion := repository.completed[0]
	if completion.Outcome != BlobPublicationCompletionOutcomeAlreadyAbsent ||
		completion.Receipt != (DeletionReceipt{}) || completion.Claim.Intent.ObjectVersion != "" {
		t.Fatalf("unresolved absence completion = %#v, want zero receipt/version", completion)
	}
}

func TestBlobPublicationReconcilerSchedulesRetryOnResolverFailure(t *testing.T) {
	claim := testBlobPublicationCleanupClaim("")
	claim.Intent.Target.BackendKind = BackendKindS3
	repository := &publicationReconcilerRepositoryStub{claim: &claim}
	resolver := &publicationResolverStub{err: errors.New("resolver unavailable")}
	reconciler, err := NewBlobPublicationReconciler(repository, resolver, &publicationBlobStoreStub{}, BlobPublicationReconcilerConfig{
		ProjectID: "default", BackendKind: BackendKindS3, CleanupOwnerID: "publication_reconciler_1",
		RetryDelay: time.Minute, Now: func() time.Time { return claim.ObservedLeaseExpiresAt.Add(-time.Minute) },
	})
	if err != nil {
		t.Fatalf("NewBlobPublicationReconciler() error = %v", err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if !claimed || err == nil {
		t.Fatalf("RunOnce() = (%t, %v), want claimed retry error", claimed, err)
	}
	if len(repository.retried) != 1 || repository.retried[0].Claim != claim {
		t.Fatalf("retry requests = %#v, want original claim", repository.retried)
	}
}

func TestLocalBlobStoreResolvesFinalPublicationByExactDigestKey(t *testing.T) {
	store := newTestLocalBlobStore(t, filepath.Join(t.TempDir(), "blobs"), localBlobHooks{})
	content := []byte("publication resolver local")
	digest := sha256.Sum256(content)
	written, err := store.Put(context.Background(), PutRequest{
		ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
	}, bytesReader(content))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	target := BlobPublicationTarget{
		Key: "sha256/" + hexDigest(digest), SHA256: digest,
		SizeBytes: int64(len(content)), BackendKind: BackendKindLocal,
	}
	resolved, err := store.ResolveBlobPublicationObject(context.Background(), target)
	if err != nil {
		t.Fatalf("ResolveBlobPublicationObject() error = %v", err)
	}
	if resolved != written {
		t.Fatalf("resolved object = %#v, want exact local identity", resolved)
	}
}

func TestLocalBlobStorePublicationResolverRejectsMissingSizeAndHashDrift(t *testing.T) {
	content := []byte("publication resolver local edge cases")
	digest := sha256.Sum256(content)
	target := BlobPublicationTarget{
		Key: "sha256/" + hexDigest(digest), SHA256: digest,
		SizeBytes: int64(len(content)), BackendKind: BackendKindLocal,
	}

	t.Run("exact key missing", func(t *testing.T) {
		store := newTestLocalBlobStore(t, filepath.Join(t.TempDir(), "blobs"), localBlobHooks{})
		if _, err := store.ResolveBlobPublicationObject(context.Background(), target); !errors.Is(err, ErrBlobNotFound) {
			t.Fatalf("ResolveBlobPublicationObject(missing) error = %v, want ErrBlobNotFound", err)
		}
	})

	t.Run("size mismatch", func(t *testing.T) {
		store := newTestLocalBlobStore(t, filepath.Join(t.TempDir(), "blobs"), localBlobHooks{})
		if _, err := store.Put(context.Background(), PutRequest{
			ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
		}, bytesReader(content)); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		mismatched := target
		mismatched.SizeBytes++
		if _, err := store.ResolveBlobPublicationObject(context.Background(), mismatched); !errors.Is(err, ErrBlobSizeMismatch) {
			t.Fatalf("ResolveBlobPublicationObject(size mismatch) error = %v, want ErrBlobSizeMismatch", err)
		}
	})

	t.Run("hash mismatch", func(t *testing.T) {
		store := newTestLocalBlobStore(t, filepath.Join(t.TempDir(), "blobs"), localBlobHooks{})
		version, err := store.Put(context.Background(), PutRequest{
			ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
		}, bytesReader(content))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		objectPath := filepath.Join(store.root, version.Key)
		corrupt := make([]byte, len(content))
		for index := range corrupt {
			corrupt[index] = byte(index + 1)
		}
		if err := os.WriteFile(objectPath, corrupt, 0o600); err != nil {
			t.Fatalf("corrupt exact local object: %v", err)
		}
		if _, err := store.ResolveBlobPublicationObject(context.Background(), target); !errors.Is(err, ErrBlobHashMismatch) {
			t.Fatalf("ResolveBlobPublicationObject(hash mismatch) error = %v, want ErrBlobHashMismatch", err)
		}
	})
}

func bytesReader(value []byte) io.Reader { return &publicationBytesReader{value: value} }

type publicationBytesReader struct {
	value []byte
	index int
}

func (reader *publicationBytesReader) Read(buffer []byte) (int, error) {
	if reader.index == len(reader.value) {
		return 0, io.EOF
	}
	n := copy(buffer, reader.value[reader.index:])
	reader.index += n
	return n, nil
}

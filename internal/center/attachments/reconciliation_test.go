package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type workspaceCleanupRepositoryStub struct {
	*fakeProcessorWorkspaceRepository
	candidate  *ProcessorWorkspaceCleanupCandidate
	claimCalls int
}

func (repository *workspaceCleanupRepositoryStub) ClaimProcessorWorkspaceCleanup(
	_ context.Context,
	_ ProcessorWorkspaceCleanupClaimInput,
) (*ProcessorWorkspaceCleanupCandidate, error) {
	repository.claimCalls++
	if repository.candidate == nil {
		return nil, nil
	}
	candidate := *repository.candidate
	repository.candidate = nil
	return &candidate, nil
}

func TestProcessorWorkspaceReconcilerClaimsAndPurgesDurableWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "processor-root")
	workspaceID := "cpw_reconcileworkspace1"
	workspacePath := filepath.Join(root, workspaceID)
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "source.bin"), []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	pathDigest := sha256.Sum256([]byte(workspacePath))
	base := newFakeProcessorWorkspaceRepository()
	base.workspace = ProcessorWorkspace{
		WorkspaceID: workspaceID, ProcessorJobID: "apj_reconcileworkspace1",
		Attempt: 1, State: ProcessorWorkspaceStateMaterialized,
		WorkspacePathDigest: pathDigest, ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}
	repository := &workspaceCleanupRepositoryStub{
		fakeProcessorWorkspaceRepository: base,
		candidate: &ProcessorWorkspaceCleanupCandidate{
			WorkspaceID: workspaceID, WorkspacePathDigest: pathDigest,
		},
	}
	reconciler, err := NewProcessorWorkspaceReconciler(repository, ProcessorWorkspaceReconcilerConfig{
		Root: root, CleanupTimeout: time.Second, RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewProcessorWorkspaceReconciler() error = %v", err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce() = (%t, %v), want claimed success", claimed, err)
	}
	if _, err := os.Lstat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace residue after reconciliation = %v", err)
	}
	if repository.claimCalls != 1 || repository.beginPurgeCalls != 1 ||
		repository.completePurgeCalls != 1 || repository.receipt.WorkspaceID != workspaceID {
		t.Fatalf("workspace reconciliation calls = claim %d begin %d complete %d receipt %#v",
			repository.claimCalls, repository.beginPurgeCalls,
			repository.completePurgeCalls, repository.receipt)
	}
	claimed, err = reconciler.RunOnce(context.Background())
	if err != nil || claimed {
		t.Fatalf("RunOnce(empty replay) = (%t, %v), want false, nil", claimed, err)
	}
}

func TestProcessorWorkspaceReconcilerCancellationStopsBeforeClaim(t *testing.T) {
	repository := &workspaceCleanupRepositoryStub{
		fakeProcessorWorkspaceRepository: newFakeProcessorWorkspaceRepository(),
	}
	reconciler, err := NewProcessorWorkspaceReconciler(repository, ProcessorWorkspaceReconcilerConfig{
		Root:           filepath.Join(t.TempDir(), "processor-root"),
		CleanupTimeout: time.Second, RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewProcessorWorkspaceReconciler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	claimed, err := reconciler.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) || claimed || repository.claimCalls != 0 {
		t.Fatalf("RunOnce(cancelled) = (%t, %v), claim calls %d", claimed, err, repository.claimCalls)
	}
}

type temporaryCleanupRepositoryStub struct {
	candidate      *TemporaryObjectCleanupCandidate
	claimCalls     int
	recordCalls    []RecordTemporaryObjectVersionCommand
	marked         []TemporaryObjectCleanupCandidate
	recordErr      error
	markErr        error
	leaveCandidate bool
}

func (repository *temporaryCleanupRepositoryStub) ClaimTemporaryObjectCleanup(
	_ context.Context,
	_ TemporaryObjectCleanupClaimInput,
) (*TemporaryObjectCleanupCandidate, error) {
	repository.claimCalls++
	if repository.candidate == nil {
		return nil, nil
	}
	candidate := *repository.candidate
	return &candidate, nil
}

func (repository *temporaryCleanupRepositoryStub) RecordTemporaryObjectVersion(
	_ context.Context,
	command RecordTemporaryObjectVersionCommand,
) (UploadPreparation, error) {
	repository.recordCalls = append(repository.recordCalls, command)
	if repository.recordErr != nil {
		return UploadPreparation{}, repository.recordErr
	}
	if repository.candidate == nil || repository.candidate.TemporaryObjectKey != command.TemporaryObjectKey {
		return UploadPreparation{}, ErrAttachmentConflict
	}
	repository.candidate.TemporaryObjectVersion = command.TemporaryObjectVersion
	return UploadPreparation{
		ProjectID:              repository.candidate.ProjectID,
		UploadID:               repository.candidate.UploadID,
		AuthorID:               repository.candidate.AuthorID,
		State:                  repository.candidate.State,
		TransportKind:          TransportKindS3,
		TemporaryObjectKey:     command.TemporaryObjectKey,
		TemporaryObjectVersion: command.TemporaryObjectVersion,
		ExpiresAt:              repository.candidate.ExpiresAt,
	}, nil
}

func (repository *temporaryCleanupRepositoryStub) MarkTemporaryObjectCleaned(
	_ context.Context,
	candidate TemporaryObjectCleanupCandidate,
) error {
	if repository.markErr != nil {
		return repository.markErr
	}
	repository.marked = append(repository.marked, candidate)
	if !repository.leaveCandidate {
		repository.candidate = nil
	}
	return nil
}

type temporaryCleanupBlobStub struct {
	resolved     TemporaryObjectVersion
	resolveErr   error
	deleted      []TemporaryObjectVersion
	deleteErr    error
	resolveCalls []string
}

func (blob *temporaryCleanupBlobStub) ResolveTemporaryVersion(_ context.Context, key string) (TemporaryObjectVersion, error) {
	blob.resolveCalls = append(blob.resolveCalls, key)
	if blob.resolveErr != nil {
		return TemporaryObjectVersion{}, blob.resolveErr
	}
	return blob.resolved, nil
}

func (blob *temporaryCleanupBlobStub) DeleteTemporaryVersion(_ context.Context, version TemporaryObjectVersion) error {
	blob.deleted = append(blob.deleted, version)
	return blob.deleteErr
}

func (blob *temporaryCleanupBlobStub) OpenTemporaryVersion(context.Context, TemporaryObjectReadRequest) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (blob *temporaryCleanupBlobStub) PublishTemporaryVersion(context.Context, TemporaryObjectPublishRequest) (ObjectVersion, error) {
	return ObjectVersion{}, ErrInvalidBlobRequest
}

func TestTemporaryObjectReconcilerResolvesKnownKeyCASesVersionAndDeletesExactObject(t *testing.T) {
	candidate := temporaryCleanupCandidate()
	candidate.TemporaryObjectVersion = "persisted-v1"
	repository := &temporaryCleanupRepositoryStub{candidate: &candidate}
	blob := &temporaryCleanupBlobStub{}
	reconciler, err := NewTemporaryObjectReconciler(repository, blob, TemporaryObjectReconcilerConfig{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce() = (%t, %v), want claimed success", claimed, err)
	}
	if len(blob.resolveCalls) != 0 {
		t.Fatalf("known temporary version was resolved = %#v", blob.resolveCalls)
	}
	if len(blob.deleted) != 1 || blob.deleted[0] != (TemporaryObjectVersion{Key: candidate.TemporaryObjectKey, VersionID: "persisted-v1"}) {
		t.Fatalf("deleted versions = %#v, want exact persisted version", blob.deleted)
	}
	if len(repository.marked) != 1 || repository.marked[0].TemporaryObjectVersion != "persisted-v1" {
		t.Fatalf("cleanup marks = %#v", repository.marked)
	}
}

func TestTemporaryObjectReconcilerResolvesMissingVersionThenCASesAndDeletes(t *testing.T) {
	candidate := temporaryCleanupCandidate()
	repository := &temporaryCleanupRepositoryStub{candidate: &candidate}
	blob := &temporaryCleanupBlobStub{resolved: TemporaryObjectVersion{Key: candidate.TemporaryObjectKey, VersionID: "observed-v1"}}
	reconciler, err := NewTemporaryObjectReconciler(repository, blob, TemporaryObjectReconcilerConfig{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce() = (%t, %v), want claimed success", claimed, err)
	}
	if len(blob.resolveCalls) != 1 || blob.resolveCalls[0] != candidate.TemporaryObjectKey {
		t.Fatalf("resolve calls = %#v, want one known-key lookup", blob.resolveCalls)
	}
	if len(repository.recordCalls) != 1 || repository.recordCalls[0].TemporaryObjectVersion != "observed-v1" {
		t.Fatalf("CAS calls = %#v, want observed version", repository.recordCalls)
	}
	if len(blob.deleted) != 1 || blob.deleted[0].VersionID != "observed-v1" {
		t.Fatalf("deleted versions = %#v, want observed version", blob.deleted)
	}
}

func TestTemporaryObjectReconcilerFailsClosedWhenKnownKeyIsMissing(t *testing.T) {
	candidate := temporaryCleanupCandidate()
	repository := &temporaryCleanupRepositoryStub{candidate: &candidate}
	blob := &temporaryCleanupBlobStub{resolveErr: ErrBlobNotFound}
	reconciler, err := NewTemporaryObjectReconciler(repository, blob, TemporaryObjectReconcilerConfig{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if !claimed || !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("RunOnce() = (%t, %v), want bounded missing-object failure", claimed, err)
	}
	if len(repository.recordCalls) != 0 || len(blob.deleted) != 0 || len(repository.marked) != 0 {
		t.Fatalf("missing object caused mutation: CAS=%#v delete=%#v mark=%#v", repository.recordCalls, blob.deleted, repository.marked)
	}
}

func TestTemporaryObjectReconcilerDoesNotDeleteReplacedVersion(t *testing.T) {
	candidate := temporaryCleanupCandidate()
	candidate.TemporaryObjectVersion = "persisted-v1"
	repository := &temporaryCleanupRepositoryStub{candidate: &candidate}
	blob := &temporaryCleanupBlobStub{deleteErr: ErrBlobVersionMismatch}
	reconciler, err := NewTemporaryObjectReconciler(repository, blob, TemporaryObjectReconcilerConfig{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := reconciler.RunOnce(context.Background())
	if !claimed || !errors.Is(err, ErrBlobVersionMismatch) {
		t.Fatalf("RunOnce() = (%t, %v), want version mismatch", claimed, err)
	}
	if len(repository.marked) != 0 {
		t.Fatalf("replaced version was marked cleaned: %#v", repository.marked)
	}
}

func TestTemporaryObjectReconcilerReplaysAfterPhysicalDeleteCutpoint(t *testing.T) {
	candidate := temporaryCleanupCandidate()
	repository := &temporaryCleanupRepositoryStub{candidate: &candidate, leaveCandidate: true}
	blob := &temporaryCleanupBlobStub{resolved: TemporaryObjectVersion{Key: candidate.TemporaryObjectKey, VersionID: "observed-v1"}}
	fired := false
	reconciler, err := NewTemporaryObjectReconciler(repository, blob, TemporaryObjectReconcilerConfig{
		ProjectID: "default", RetryDelay: time.Minute,
		Cutpoint: func(cutpoint TemporaryObjectReconcilerCutpoint) error {
			if cutpoint == TemporaryObjectReconcilerCutpointAfterPhysicalPurge && !fired {
				fired = true
				return errors.New("simulated crash")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := reconciler.RunOnce(context.Background()); !claimed || err == nil {
		t.Fatalf("first RunOnce() = (%t, %v), want cutpoint failure", claimed, err)
	}
	if len(repository.marked) != 0 || len(blob.deleted) != 1 {
		t.Fatalf("cutpoint state = marks:%#v deletes:%#v", repository.marked, blob.deleted)
	}
	reconciler.config.Cutpoint = nil
	if claimed, err := reconciler.RunOnce(context.Background()); !claimed || err != nil {
		t.Fatalf("replay RunOnce() = (%t, %v), want success", claimed, err)
	}
	if len(blob.deleted) != 2 || len(repository.marked) != 1 {
		t.Fatalf("replay state = deletes:%#v marks:%#v", blob.deleted, repository.marked)
	}
}

func TestTemporaryObjectReconcilerStopsBeforeClaimWhenCancelled(t *testing.T) {
	repository := &temporaryCleanupRepositoryStub{candidate: func() *TemporaryObjectCleanupCandidate {
		candidate := temporaryCleanupCandidate()
		return &candidate
	}()}
	reconciler, err := NewTemporaryObjectReconciler(repository, &temporaryCleanupBlobStub{}, TemporaryObjectReconcilerConfig{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	claimed, err := reconciler.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) || claimed {
		t.Fatalf("RunOnce(cancelled) = (%t, %v), want no claim and cancellation", claimed, err)
	}
	if repository.claimCalls != 0 {
		t.Fatalf("cancelled reconciliation claimed %d candidates", repository.claimCalls)
	}
}

func temporaryCleanupCandidate() TemporaryObjectCleanupCandidate {
	return TemporaryObjectCleanupCandidate{
		ProjectID: "default", UploadID: "aup_cleanup1", AuthorID: "usr_cleanup1",
		TemporaryObjectKey: "temporary/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		State:              UploadStateExpired,
		ExpiresAt:          time.Now().UTC().Add(-time.Minute),
	}
}

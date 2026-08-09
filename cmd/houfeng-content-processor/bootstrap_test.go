package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
)

type processorTestDB struct {
	pool   *pgxpool.Pool
	closed bool
}

func (db *processorTestDB) Close()              { db.closed = true }
func (db *processorTestDB) Pool() *pgxpool.Pool { return db.pool }

type processorTestWorker struct {
	calls  int
	called chan struct{}
	err    error
}

type processorBlockingWorker struct {
	started chan struct{}
}

func (worker *processorBlockingWorker) Run(ctx context.Context) error {
	close(worker.started)
	<-ctx.Done()
	return ctx.Err()
}

func (worker *processorTestWorker) Run(ctx context.Context) error {
	worker.calls++
	if worker.called != nil {
		close(worker.called)
	}
	if worker.err != nil {
		return worker.err
	}
	return nil
}

type processorTestReconciler struct {
	calls       int
	claim       bool
	err         error
	started     chan struct{}
	allowWorker chan struct{}
	callEvents  chan int
}

func (reconciler *processorTestReconciler) RunOnce(ctx context.Context) (bool, error) {
	reconciler.calls++
	if reconciler.started != nil && reconciler.calls == 1 {
		close(reconciler.started)
	}
	if reconciler.callEvents != nil {
		reconciler.callEvents <- reconciler.calls
	}
	if reconciler.allowWorker != nil {
		select {
		case <-reconciler.allowWorker:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return reconciler.claim, reconciler.err
}

func TestProcessorReconcilerGroupRunsSeriallyUntilClaim(t *testing.T) {
	first := &processorTestReconciler{}
	second := &processorTestReconciler{claim: true}
	third := &processorTestReconciler{claim: true}
	group, err := newProcessorReconcilerGroup(first, nil, second, third)
	if err != nil {
		t.Fatalf("newProcessorReconcilerGroup() error = %v", err)
	}
	claimed, err := group.RunOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("RunOnce() = (%t, %v), want claimed success", claimed, err)
	}
	if first.calls != 1 || second.calls != 1 || third.calls != 0 {
		t.Fatalf("reconciler calls = first:%d second:%d third:%d", first.calls, second.calls, third.calls)
	}
}

func TestContentProcessorRuntimeContinuesReconciliationUntilCancellation(t *testing.T) {
	config := testProcessorConfig()
	callEvents := make(chan int, 4)
	reconciler := &processorTestReconciler{callEvents: callEvents}
	worker := &processorBlockingWorker{started: make(chan struct{})}
	backgroundSleepStarted := make(chan struct{})
	allowBackgroundPass := make(chan struct{})
	sleepCalls := 0
	runtime := &contentProcessorRuntime{
		reconciler: reconciler,
		worker:     worker,
		config:     config,
		now:        time.Now,
		sleep: func(ctx context.Context, _ time.Duration) error {
			sleepCalls++
			if sleepCalls == 1 {
				close(backgroundSleepStarted)
				select {
				case <-allowBackgroundPass:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			<-ctx.Done()
			return ctx.Err()
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runtime.Run(ctx)
	}()

	if call := <-callEvents; call != 1 {
		t.Fatalf("startup reconciliation call = %d, want 1", call)
	}
	<-worker.started
	<-backgroundSleepStarted
	close(allowBackgroundPass)
	if call := <-callEvents; call != 2 {
		t.Fatalf("background reconciliation call = %d, want 2", call)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(cancelled) error = %v, want context.Canceled", err)
	}
	if reconciler.calls != 2 {
		t.Fatalf("reconciliation calls after cancellation = %d, want 2", reconciler.calls)
	}
}

type processorTestBlob struct{}

func (processorTestBlob) Put(context.Context, attachments.PutRequest, io.Reader) (attachments.ObjectVersion, error) {
	return attachments.ObjectVersion{}, errors.New("not implemented")
}
func (processorTestBlob) Open(context.Context, attachments.ObjectVersion, attachments.ByteRange) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (processorTestBlob) Stat(context.Context, attachments.ObjectVersion) (attachments.ObjectInfo, error) {
	return attachments.ObjectInfo{}, errors.New("not implemented")
}
func (processorTestBlob) Delete(context.Context, attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	return attachments.DeletionReceipt{}, errors.New("not implemented")
}
func (processorTestBlob) ResolveBlobPublicationObject(context.Context, attachments.BlobPublicationTarget) (attachments.ObjectVersion, error) {
	return attachments.ObjectVersion{}, attachments.ErrBlobNotFound
}

type processorTestTemporaryStore struct{ processorTestBlob }

func (processorTestTemporaryStore) ResolveTemporaryVersion(context.Context, string) (attachments.TemporaryObjectVersion, error) {
	return attachments.TemporaryObjectVersion{}, attachments.ErrBlobNotFound
}
func (processorTestTemporaryStore) OpenTemporaryVersion(context.Context, attachments.TemporaryObjectReadRequest) (io.ReadCloser, error) {
	return nil, attachments.ErrBlobNotFound
}
func (processorTestTemporaryStore) PublishTemporaryVersion(context.Context, attachments.TemporaryObjectPublishRequest) (attachments.ObjectVersion, error) {
	return attachments.ObjectVersion{}, attachments.ErrBlobNotFound
}
func (processorTestTemporaryStore) DeleteTemporaryVersion(context.Context, attachments.TemporaryObjectVersion) error {
	return nil
}

type processorTestRepository struct{}

func (processorTestRepository) ExpireAbandonedUpload(
	context.Context,
	attachments.AbandonedUploadExpiryInput,
) (*attachments.UploadMutationResult, error) {
	return nil, nil
}

func (processorTestRepository) PrepareBlobPublication(
	_ context.Context,
	request attachments.BlobPublicationPrepareRequest,
) (attachments.BlobPublicationIntent, error) {
	return attachments.BlobPublicationIntent{
		PublicationID: "bpi_processor_test", ProjectID: request.ProjectID,
		OwnerKind: request.OwnerKind, OwnerID: request.OwnerID, OwnerGeneration: request.OwnerGeneration,
		Target: request.Target, State: attachments.BlobPublicationStatePrepared,
		PublishExpiresAt: request.PublishExpiresAt,
	}, nil
}

func (processorTestRepository) RecordBlobPublicationVersion(
	_ context.Context,
	request attachments.BlobPublicationVersionRequest,
) (attachments.BlobPublicationIntent, error) {
	intent := request.Intent
	intent.ObjectVersion = request.Object.VersionID
	intent.State = attachments.BlobPublicationStatePublished
	return intent, nil
}

func (processorTestRepository) RegisterProcessorWorkspace(context.Context, attachments.ProcessorWorkspaceRegistration) (attachments.ProcessorWorkspace, error) {
	return attachments.ProcessorWorkspace{}, nil
}
func (processorTestRepository) MaterializeProcessorWorkspace(context.Context, attachments.ProcessorWorkspaceTransition) (attachments.ProcessorWorkspace, error) {
	return attachments.ProcessorWorkspace{}, nil
}
func (processorTestRepository) BeginProcessorWorkspacePurge(context.Context, attachments.ProcessorWorkspaceTransition) (attachments.ProcessorWorkspacePurgePlan, error) {
	return attachments.ProcessorWorkspacePurgePlan{}, nil
}
func (processorTestRepository) CompleteProcessorWorkspacePurge(context.Context, attachments.ProcessorWorkspacePurgeCompletion) (attachments.ProcessorWorkspacePurgeReceipt, error) {
	return attachments.ProcessorWorkspacePurgeReceipt{}, nil
}
func (processorTestRepository) ClaimProcessorJob(context.Context, attachments.ProcessorClaimInput) (*attachments.ProcessorClaim, error) {
	return nil, nil
}
func (processorTestRepository) RenewProcessorClaim(context.Context, attachments.ProcessorRenewInput) (attachments.ProcessorClaim, error) {
	return attachments.ProcessorClaim{}, nil
}
func (processorTestRepository) CompleteProcessorJob(context.Context, attachments.ProcessorCompletionInput) (attachments.ProcessorCompletionResult, error) {
	return attachments.ProcessorCompletionResult{}, nil
}
func (processorTestRepository) ExpireBoundedProcessorJob(context.Context, attachments.ProcessorExpiryInput) (*attachments.ProcessorCompletionResult, error) {
	return nil, nil
}
func (processorTestRepository) ClaimProcessorWorkspaceCleanup(context.Context, attachments.ProcessorWorkspaceCleanupClaimInput) (*attachments.ProcessorWorkspaceCleanupCandidate, error) {
	return nil, nil
}
func (processorTestRepository) ClaimTemporaryObjectCleanup(context.Context, attachments.TemporaryObjectCleanupClaimInput) (*attachments.TemporaryObjectCleanupCandidate, error) {
	return nil, nil
}
func (processorTestRepository) RecordTemporaryObjectVersion(context.Context, attachments.RecordTemporaryObjectVersionCommand) (attachments.UploadPreparation, error) {
	return attachments.UploadPreparation{}, nil
}
func (processorTestRepository) MarkTemporaryObjectCleaned(context.Context, attachments.TemporaryObjectCleanupCandidate) error {
	return nil
}
func (processorTestRepository) ClaimBlobPublicationCleanup(context.Context, attachments.BlobPublicationCleanupClaimRequest) (*attachments.BlobPublicationCleanupClaim, error) {
	return nil, nil
}
func (processorTestRepository) RecordBlobPublicationCleanupVersion(_ context.Context, request attachments.BlobPublicationCleanupVersionRequest) (attachments.BlobPublicationCleanupClaim, error) {
	return request.Claim, nil
}
func (processorTestRepository) RetryBlobPublicationCleanup(context.Context, attachments.BlobPublicationCleanupRetryRequest) error {
	return nil
}
func (processorTestRepository) CompleteBlobPublicationCleanup(_ context.Context, request attachments.BlobPublicationCleanupCompletionRequest) (attachments.BlobPublicationCleanupResult, error) {
	return attachments.BlobPublicationCleanupResult{
		PublicationID: request.Claim.Intent.PublicationID,
		Outcome:       request.Outcome,
		Receipt:       request.Receipt,
	}, nil
}

type processorRepositoryWithoutPublication struct {
	attachments.ProcessorRepository
}

type processorBlobWithoutPublicationResolver struct {
	attachments.BlobStore
}

type processorPublicationFactoryRepository struct {
	processorTestRepository
	claimRequest attachments.BlobPublicationCleanupClaimRequest
	retryRequest attachments.BlobPublicationCleanupRetryRequest
}

func (repository *processorPublicationFactoryRepository) ClaimBlobPublicationCleanup(
	_ context.Context,
	request attachments.BlobPublicationCleanupClaimRequest,
) (*attachments.BlobPublicationCleanupClaim, error) {
	repository.claimRequest = request
	digest := sha256.Sum256([]byte("bootstrap publication factory"))
	return &attachments.BlobPublicationCleanupClaim{
		Intent: attachments.BlobPublicationIntent{
			PublicationID: "bpi_bootstrapfactory", ProjectID: "default",
			OwnerKind: attachments.BlobPublicationOwnerUpload, OwnerID: "aup_bootstrapfactory", OwnerGeneration: 1,
			Target: attachments.BlobPublicationTarget{
				Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest,
				SizeBytes: int64(len("bootstrap publication factory")), BackendKind: request.BackendKind,
			},
			State: attachments.BlobPublicationStateCleanupClaimed, PublishExpiresAt: time.Now().UTC().Add(-time.Minute),
		},
		CleanupOwnerID: request.CleanupOwnerID, CleanupGeneration: 1, Attempt: 1,
		ObservedLeaseExpiresAt: time.Now().UTC().Add(time.Hour),
	}, nil
}

func (repository *processorPublicationFactoryRepository) RetryBlobPublicationCleanup(
	_ context.Context,
	request attachments.BlobPublicationCleanupRetryRequest,
) error {
	repository.retryRequest = request
	return nil
}

type processorPublicationFactoryBlob struct {
	processorTestBlob
	resolveErr error
}

func (blob *processorPublicationFactoryBlob) ResolveBlobPublicationObject(
	context.Context,
	attachments.BlobPublicationTarget,
) (attachments.ObjectVersion, error) {
	return attachments.ObjectVersion{}, blob.resolveErr
}

// processorBootstrapCutpointRepository is deliberately small but durable in
// shape: the default factories below must be usable with the same repository
// contract that production wiring receives.  Its workspace methods return
// deterministic states so the test can reach the first filesystem cutpoint
// without depending on PostgreSQL.
type processorBootstrapCutpointRepository struct {
	processorTestRepository
	claim                     attachments.ProcessorClaim
	workspaceCleanupCandidate *attachments.ProcessorWorkspaceCleanupCandidate
	temporaryCleanupCandidate attachments.TemporaryObjectCleanupCandidate
}

type processorRestartReconciliationRepository struct {
	processorTestRepository
	candidate   *attachments.TemporaryObjectCleanupCandidate
	recordCalls []attachments.RecordTemporaryObjectVersionCommand
	markCalls   []attachments.TemporaryObjectCleanupCandidate
}

func (repository *processorRestartReconciliationRepository) ClaimTemporaryObjectCleanup(
	_ context.Context,
	_ attachments.TemporaryObjectCleanupClaimInput,
) (*attachments.TemporaryObjectCleanupCandidate, error) {
	if repository.candidate == nil {
		return nil, nil
	}
	candidate := *repository.candidate
	return &candidate, nil
}

func (repository *processorRestartReconciliationRepository) RecordTemporaryObjectVersion(
	_ context.Context,
	command attachments.RecordTemporaryObjectVersionCommand,
) (attachments.UploadPreparation, error) {
	repository.recordCalls = append(repository.recordCalls, command)
	if repository.candidate == nil || repository.candidate.TemporaryObjectVersion != "" ||
		repository.candidate.ProjectID != command.ProjectID || repository.candidate.UploadID != command.UploadID ||
		repository.candidate.AuthorID != command.AuthorID ||
		repository.candidate.TemporaryObjectKey != command.TemporaryObjectKey {
		return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
	}
	repository.candidate.TemporaryObjectVersion = command.TemporaryObjectVersion
	return attachments.UploadPreparation{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
		State: repository.candidate.State, TransportKind: attachments.TransportKindS3,
		TemporaryObjectKey: command.TemporaryObjectKey, TemporaryObjectVersion: command.TemporaryObjectVersion,
		ExpiresAt: repository.candidate.ExpiresAt,
	}, nil
}

func (repository *processorRestartReconciliationRepository) MarkTemporaryObjectCleaned(
	_ context.Context,
	candidate attachments.TemporaryObjectCleanupCandidate,
) error {
	repository.markCalls = append(repository.markCalls, candidate)
	if repository.candidate == nil || *repository.candidate != candidate {
		return attachments.ErrTemporaryObjectReconciliationConflict
	}
	repository.candidate = nil
	return nil
}

type processorRestartTemporaryStore struct {
	processorTestTemporaryStore
	resolved     attachments.TemporaryObjectVersion
	resolveCalls []string
	deleteCalls  []attachments.TemporaryObjectVersion
}

func (store *processorRestartTemporaryStore) ResolveTemporaryVersion(
	_ context.Context,
	key string,
) (attachments.TemporaryObjectVersion, error) {
	store.resolveCalls = append(store.resolveCalls, key)
	return store.resolved, nil
}

func (store *processorRestartTemporaryStore) DeleteTemporaryVersion(
	_ context.Context,
	version attachments.TemporaryObjectVersion,
) error {
	store.deleteCalls = append(store.deleteCalls, version)
	return nil
}

func (repository *processorBootstrapCutpointRepository) RegisterProcessorWorkspace(
	_ context.Context,
	registration attachments.ProcessorWorkspaceRegistration,
) (attachments.ProcessorWorkspace, error) {
	return attachments.ProcessorWorkspace{
		WorkspaceID: registration.WorkspaceID, ProcessorJobID: registration.Claim.ProcessorJobID,
		Attempt: registration.Claim.Attempt, State: attachments.ProcessorWorkspaceStateRegistered,
		WorkspacePathDigest: registration.WorkspacePathDigest, ExpiresAt: registration.ExpiresAt,
	}, nil
}

func (repository *processorBootstrapCutpointRepository) MaterializeProcessorWorkspace(
	_ context.Context,
	transition attachments.ProcessorWorkspaceTransition,
) (attachments.ProcessorWorkspace, error) {
	claim := transition.Authorization.Claim
	return attachments.ProcessorWorkspace{
		WorkspaceID: transition.WorkspaceID, ProcessorJobID: claim.ProcessorJobID,
		Attempt: claim.Attempt, State: attachments.ProcessorWorkspaceStateMaterialized,
		WorkspacePathDigest: transition.WorkspacePathDigest, ExpiresAt: claim.LeaseExpiresAt,
	}, nil
}

func (repository *processorBootstrapCutpointRepository) BeginProcessorWorkspacePurge(
	_ context.Context,
	transition attachments.ProcessorWorkspaceTransition,
) (attachments.ProcessorWorkspacePurgePlan, error) {
	if transition.Authorization.Mode == attachments.ProcessorWorkspaceAuthorizationReconciliation {
		return attachments.ProcessorWorkspacePurgePlan{
			Workspace: attachments.ProcessorWorkspace{
				WorkspaceID: transition.WorkspaceID, ProcessorJobID: "apj_bootstrapcleanup",
				Attempt: 1, State: attachments.ProcessorWorkspaceStatePurging,
				WorkspacePathDigest: transition.WorkspacePathDigest, ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		}, nil
	}
	claim := transition.Authorization.Claim
	receipt, err := attachments.NewProcessorWorkspacePurgeReceipt(
		transition.WorkspaceID, 0, time.Unix(1, 0),
	)
	if err != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, err
	}
	return attachments.ProcessorWorkspacePurgePlan{
		Workspace: attachments.ProcessorWorkspace{
			WorkspaceID: transition.WorkspaceID, ProcessorJobID: claim.ProcessorJobID,
			Attempt: claim.Attempt, State: attachments.ProcessorWorkspaceStatePurged,
			WorkspacePathDigest: transition.WorkspacePathDigest, ExpiresAt: claim.LeaseExpiresAt,
		},
		Receipt: &receipt,
	}, nil
}

func (repository *processorBootstrapCutpointRepository) CompleteProcessorWorkspacePurge(
	_ context.Context,
	completion attachments.ProcessorWorkspacePurgeCompletion,
) (attachments.ProcessorWorkspacePurgeReceipt, error) {
	return completion.Receipt, nil
}

func (repository *processorBootstrapCutpointRepository) ClaimProcessorJob(
	_ context.Context,
	_ attachments.ProcessorClaimInput,
) (*attachments.ProcessorClaim, error) {
	claim := repository.claim
	return &claim, nil
}

func (repository *processorBootstrapCutpointRepository) RenewProcessorClaim(
	_ context.Context,
	input attachments.ProcessorRenewInput,
) (attachments.ProcessorClaim, error) {
	return input.Claim, nil
}

func (repository *processorBootstrapCutpointRepository) CompleteProcessorJob(
	_ context.Context,
	_ attachments.ProcessorCompletionInput,
) (attachments.ProcessorCompletionResult, error) {
	return attachments.ProcessorCompletionResult{}, nil
}

func (repository *processorBootstrapCutpointRepository) ClaimProcessorWorkspaceCleanup(
	_ context.Context,
	_ attachments.ProcessorWorkspaceCleanupClaimInput,
) (*attachments.ProcessorWorkspaceCleanupCandidate, error) {
	candidate := repository.workspaceCleanupCandidate
	repository.workspaceCleanupCandidate = nil
	return candidate, nil
}

func (repository *processorBootstrapCutpointRepository) ClaimTemporaryObjectCleanup(
	_ context.Context,
	_ attachments.TemporaryObjectCleanupClaimInput,
) (*attachments.TemporaryObjectCleanupCandidate, error) {
	candidate := repository.temporaryCleanupCandidate
	return &candidate, nil
}

func processorBootstrapCutpointClaim() attachments.ProcessorClaim {
	content := []byte("bootstrap cutpoint source")
	digest := sha256.Sum256(content)
	now := time.Now().UTC().Truncate(time.Microsecond)
	return attachments.ProcessorClaim{
		ProjectID: "default", ProcessorJobID: "apj_bootstrapcutpoint",
		UploadID: "aup_bootstrapcutpoint", AttachmentID: "att_bootstrapcutpoint",
		DisplayName: "notes.txt", DeclaredMediaType: "text/plain",
		Source: attachments.BlobObject{
			Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest,
			ObjectVersion: "source-v1", SizeBytes: int64(len(content)), BackendKind: attachments.BackendKindS3,
		},
		Profile: attachments.ProcessorProfileText, Attempt: 1, MaxAttempts: 3,
		OwnerID: "content-processor", OwnerGeneration: 1,
		LeaseExpiresAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour),
	}
}

func TestContentProcessorDefaultFactoriesPropagateCutpointObserver(t *testing.T) {
	config := testProcessorConfig()
	config.WorkspaceRoot = filepath.Join(t.TempDir(), "processor-root")
	claim := processorBootstrapCutpointClaim()
	cleanupWorkspaceID := "cpw_bootstrapcleanup"
	cleanupWorkspacePath := filepath.Join(config.WorkspaceRoot, cleanupWorkspaceID)
	if err := os.MkdirAll(cleanupWorkspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	cleanupWorkspaceDigest := sha256.Sum256([]byte(cleanupWorkspacePath))
	repository := &processorBootstrapCutpointRepository{
		claim: claim,
		workspaceCleanupCandidate: &attachments.ProcessorWorkspaceCleanupCandidate{
			WorkspaceID: cleanupWorkspaceID, WorkspacePathDigest: cleanupWorkspaceDigest,
		},
		temporaryCleanupCandidate: attachments.TemporaryObjectCleanupCandidate{
			ProjectID: "default", UploadID: "aup_bootstrapcleanup", AuthorID: "usr_bootstrapcleanup",
			TemporaryObjectKey: "temporary/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			State:              attachments.UploadStateExpired, ExpiresAt: time.Now().UTC().Add(-time.Minute),
		},
	}
	var got []string
	cutpoint := func(name string) error {
		got = append(got, name)
		return errors.New("injected bootstrap cutpoint")
	}
	dependencies := (processorBootstrapDeps{
		cutpoint: cutpoint,
		newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository {
			return repository
		},
		newBlobStore: func(contentProcessorConfig) (attachments.BlobStore, error) {
			return processorTestTemporaryStore{}, nil
		},
		newScanner: func(contentProcessorConfig) (attachments.ProcessorScanner, error) {
			return nil, nil
		},
		newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			return testPreviewProcessor(), nil
		},
	}).withDefaults()

	workspace, err := dependencies.newWorkspace(config, repository, testPreviewProcessor())
	if err != nil {
		t.Fatalf("default workspace factory error = %v", err)
	}
	if _, _, err := workspace.Process(context.Background(), attachments.ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: "cpw_bootstrapcutpoint", ExpiresAt: claim.LeaseExpiresAt,
		Source: bytes.NewReader([]byte("bootstrap cutpoint source")),
	}); err == nil {
		t.Fatal("workspace.Process() unexpectedly succeeded through injected cutpoint")
	}

	worker, err := dependencies.newWorker(
		repository, processorTestTemporaryStore{}, processorTestWorkspace{}, config, nil,
	)
	if err != nil {
		t.Fatalf("default worker factory error = %v", err)
	}
	if err := worker.(*attachments.ProcessorWorker).RunOnce(context.Background()); err == nil {
		t.Fatal("worker.RunOnce() unexpectedly succeeded through injected cutpoint")
	}

	workspaceReconciler, err := dependencies.newWorkspaceReconciler(repository, config)
	if err != nil {
		t.Fatalf("default workspace reconciler factory error = %v", err)
	}
	if _, err := workspaceReconciler.RunOnce(context.Background()); err == nil {
		t.Fatal("workspace reconciler RunOnce() unexpectedly succeeded through injected cutpoint")
	}

	reconciler, err := dependencies.newReconciler(repository, processorTestTemporaryStore{}, config)
	if err != nil {
		t.Fatalf("default reconciler factory error = %v", err)
	}
	if _, err := reconciler.RunOnce(context.Background()); err == nil {
		t.Fatal("reconciler.RunOnce() unexpectedly succeeded through injected cutpoint")
	}

	want := []string{
		string(attachments.ProcessorWorkspaceCutpointAfterMkdir),
		string(attachments.ProcessorWorkerCutpointAfterClaim),
		string(attachments.ProcessorWorkspaceCutpointAfterPhysicalPurge),
		string(attachments.TemporaryObjectReconcilerCutpointAfterClaim),
	}
	next := 0
	for _, name := range got {
		if next < len(want) && name == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("default factory cutpoints = %#v, want ordered observer calls containing %#v", got, want)
	}
}

func TestContentProcessorDefaultPublicationReconcilerFactoryFailsClosedWithoutContracts(t *testing.T) {
	config := testProcessorConfig()
	config.S3SecretKey = "content-that-must-not-appear"
	dependencies := (processorBootstrapDeps{}).withDefaults()

	tests := []struct {
		name       string
		repository attachments.ProcessorRepository
		blob       attachments.BlobStore
		wantError  string
	}{
		{
			name:       "repository contract absent",
			repository: processorRepositoryWithoutPublication{ProcessorRepository: processorTestRepository{}},
			blob:       processorTestTemporaryStore{},
			wantError:  "attachment repository does not support Blob publication cleanup",
		},
		{
			name:       "Blob resolver contract absent",
			repository: processorTestRepository{},
			blob:       processorBlobWithoutPublicationResolver{BlobStore: processorTestBlob{}},
			wantError:  "Blob store does not support final-object publication resolution",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dependencies.newPublicationReconciler(tt.repository, tt.blob, config)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("newPublicationReconciler() error = %v, want %q", err, tt.wantError)
			}
			if strings.Contains(err.Error(), config.S3SecretKey) {
				t.Fatalf("newPublicationReconciler() error contains configured content: %v", err)
			}
		})
	}
}

func TestContentProcessorDefaultPublicationReconcilerFactoryUsesDurableCleanupConfig(t *testing.T) {
	config := testProcessorConfig()
	config.ReconciliationRetryDelay = 37 * time.Second
	wantResolverErr := errors.New("injected exact-key resolution failure")
	repository := &processorPublicationFactoryRepository{}
	blob := &processorPublicationFactoryBlob{resolveErr: wantResolverErr}
	var cutpoints []string
	dependencies := (processorBootstrapDeps{
		cutpoint: func(cutpoint string) error {
			cutpoints = append(cutpoints, cutpoint)
			return nil
		},
	}).withDefaults()

	reconciler, err := dependencies.newPublicationReconciler(repository, blob, config)
	if err != nil {
		t.Fatalf("newPublicationReconciler() error = %v", err)
	}
	before := time.Now().UTC()
	claimed, err := reconciler.RunOnce(context.Background())
	after := time.Now().UTC()
	if !claimed || !errors.Is(err, wantResolverErr) {
		t.Fatalf("RunOnce() = (%t, %v), want claimed resolver failure", claimed, err)
	}
	if got := repository.claimRequest; got.ProjectID != "default" || got.BackendKind != config.BlobBackend ||
		got.CleanupOwnerID != "blob_publication_reconciler" ||
		got.OwnerLeaseDuration != attachments.DefaultBlobPublicationCleanupLeaseDuration {
		t.Fatalf("cleanup claim request = %#v", got)
	}
	if retryAt := repository.retryRequest.RetryAt; retryAt.Before(before.Add(config.ReconciliationRetryDelay)) ||
		retryAt.After(after.Add(config.ReconciliationRetryDelay)) {
		t.Fatalf("retry at = %v, want observed now + %v", retryAt, config.ReconciliationRetryDelay)
	}
	if got := strings.Join(cutpoints, ","); got != string(attachments.BlobPublicationReconcilerCutpointAfterClaim) {
		t.Fatalf("publication cutpoints = %q, want typed cutpoint converted to string", got)
	}
}

func TestContentProcessorRestartReconciliationResolvesUncommittedS3Version(t *testing.T) {
	config := testProcessorConfig()
	config.ReconciliationMaxItems = 1
	candidate := attachments.TemporaryObjectCleanupCandidate{
		ProjectID: "default", UploadID: "aup_restartcleanup", AuthorID: "usr_restartcleanup",
		TemporaryObjectKey: "temporary/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		State:              attachments.UploadStateExpired, ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}
	repository := &processorRestartReconciliationRepository{candidate: &candidate}
	store := &processorRestartTemporaryStore{resolved: attachments.TemporaryObjectVersion{
		Key: candidate.TemporaryObjectKey, VersionID: "restart-observed-v1",
	}}
	newDependencies := func(cutpoint func(string) error) processorBootstrapDeps {
		return processorBootstrapDeps{
			openPostgres: func(context.Context, string) (processorPostgresDB, error) {
				return &processorTestDB{}, nil
			},
			newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository {
				return repository
			},
			newBlobStore: func(contentProcessorConfig) (attachments.BlobStore, error) {
				return store, nil
			},
			newScanner: func(contentProcessorConfig) (attachments.ProcessorScanner, error) {
				return nil, nil
			},
			newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
				return testPreviewProcessor(), nil
			},
			newWorkspace: func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error) {
				return processorTestWorkspace{}, nil
			},
			newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
				return &processorTestWorker{}, nil
			},
			logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			sleep:    func(context.Context, time.Duration) error { return nil },
			cutpoint: cutpoint,
		}
	}

	firstRuntime, firstCleanup, err := bootstrapContentProcessor(context.Background(), config, newDependencies(
		func(current string) error {
			if current == string(attachments.TemporaryObjectReconcilerCutpointAfterVersionResolve) {
				return errors.New("simulated process exit after temporary version resolution")
			}
			return nil
		},
	))
	if err != nil {
		t.Fatalf("bootstrapContentProcessor(first process) error = %v", err)
	}
	if err := firstRuntime.runStartupReconciliation(context.Background()); err != nil {
		t.Fatalf("runStartupReconciliation(first process) error = %v", err)
	}
	firstCleanup()
	if repository.candidate == nil || repository.candidate.TemporaryObjectVersion != "" ||
		len(repository.recordCalls) != 0 || len(repository.markCalls) != 0 || len(store.deleteCalls) != 0 {
		t.Fatalf("pre-restart durable state = candidate %#v records %#v marks %#v deletes %#v",
			repository.candidate, repository.recordCalls, repository.markCalls, store.deleteCalls)
	}

	config.ReconciliationMaxItems = 3
	secondRuntime, secondCleanup, err := bootstrapContentProcessor(
		context.Background(), config, newDependencies(nil),
	)
	if err != nil {
		t.Fatalf("bootstrapContentProcessor(restarted process) error = %v", err)
	}
	defer secondCleanup()
	if err := secondRuntime.runStartupReconciliation(context.Background()); err != nil {
		t.Fatalf("runStartupReconciliation(restarted process) error = %v", err)
	}
	if repository.candidate != nil || len(repository.recordCalls) != 1 || len(repository.markCalls) != 1 ||
		len(store.resolveCalls) != 2 || len(store.deleteCalls) != 1 || store.deleteCalls[0] != store.resolved {
		t.Fatalf("restart convergence = candidate %#v records %#v marks %#v resolves %#v deletes %#v",
			repository.candidate, repository.recordCalls, repository.markCalls,
			store.resolveCalls, store.deleteCalls)
	}
	if err := secondRuntime.runStartupReconciliation(context.Background()); err != nil {
		t.Fatalf("runStartupReconciliation(replay) error = %v", err)
	}
	if len(repository.recordCalls) != 1 || len(repository.markCalls) != 1 ||
		len(store.resolveCalls) != 2 || len(store.deleteCalls) != 1 {
		t.Fatalf("restart replay mutated state = records %#v marks %#v resolves %#v deletes %#v",
			repository.recordCalls, repository.markCalls, store.resolveCalls, store.deleteCalls)
	}
}

func testProcessorConfig() contentProcessorConfig {
	return contentProcessorConfig{
		DatabaseURL:              "postgres://processor",
		BlobBackend:              attachments.BackendKindS3,
		S3Endpoint:               "127.0.0.1:9000",
		S3AccessKey:              "access",
		S3SecretKey:              "secret",
		S3Bucket:                 "attachments",
		WorkspaceRoot:            "/var/lib/houfeng-test/processor-workdir",
		PDFInfoBinary:            "/usr/bin/pdfinfo",
		PDFToPPMBinary:           "/usr/bin/pdftoppm",
		ProcessorOwnerID:         "processor-test",
		OwnerLeaseDuration:       time.Minute,
		WorkspaceCleanupTimeout:  time.Second,
		ReconciliationMaxItems:   3,
		ReconciliationMaxRuntime: time.Second,
		ReconciliationRetryDelay: time.Millisecond,
		ProcessorMaxAttempts:     3,
		ProcessorJobTTL:          time.Hour,
		Limits:                   attachments.DefaultLimits(),
	}
}

func TestContentProcessorBootstrapRunsReconciliationBeforeWorker(t *testing.T) {
	config := testProcessorConfig()
	db := &processorTestDB{}
	workspaceReconciler := &processorTestReconciler{claim: false}
	publicationReconciler := &processorTestReconciler{claim: false}
	temporaryReconciler := &processorTestReconciler{claim: false}
	worker := &processorTestWorker{}
	var order []string
	deps := processorBootstrapDeps{
		openPostgres: func(context.Context, string) (processorPostgresDB, error) {
			order = append(order, "postgres")
			return db, nil
		},
		newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository {
			order = append(order, "repository")
			return processorTestRepository{}
		},
		newBlobStore: func(contentProcessorConfig) (attachments.BlobStore, error) {
			order = append(order, "blob")
			return processorTestTemporaryStore{}, nil
		},
		newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			order = append(order, "preview")
			return testPreviewProcessor(), nil
		},
		newWorkspace: func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error) {
			order = append(order, "workspace")
			return processorTestWorkspace{}, nil
		},
		newWorkspaceReconciler: func(attachments.ProcessorRepository, contentProcessorConfig) (processorReconciler, error) {
			order = append(order, "workspace_reconciler")
			return workspaceReconciler, nil
		},
		newPublicationReconciler: func(attachments.ProcessorRepository, attachments.BlobStore, contentProcessorConfig) (processorReconciler, error) {
			order = append(order, "publication_reconciler")
			return publicationReconciler, nil
		},
		newReconciler: func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error) {
			order = append(order, "reconciler")
			return temporaryReconciler, nil
		},
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
			order = append(order, "worker")
			return worker, nil
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	runner, cleanup, err := bootstrapContentProcessor(context.Background(), config, deps)
	if err != nil {
		t.Fatalf("bootstrapContentProcessor() error = %v", err)
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if worker.calls != 1 {
		t.Fatalf("worker calls = %d, want 1", worker.calls)
	}
	if got := strings.Join(order, ","); got != "postgres,repository,blob,preview,workspace,workspace_reconciler,publication_reconciler,reconciler,worker" {
		t.Fatalf("construction order = %q", got)
	}
	if workspaceReconciler.calls != 1 || publicationReconciler.calls != 1 || temporaryReconciler.calls != 1 {
		t.Fatalf("startup reconciliation calls = workspace:%d publication:%d temporary:%d, want 1 each",
			workspaceReconciler.calls, publicationReconciler.calls, temporaryReconciler.calls)
	}
	cleanup()
	if !db.closed {
		t.Fatal("cleanup did not close database")
	}
}

func TestContentProcessorBootstrapIncludesPublicationReconciliationForLocalBackend(t *testing.T) {
	config := testProcessorConfig()
	config.BlobBackend = attachments.BackendKindLocal
	config.BlobRoot = "/var/lib/houfeng-test/blob-root"
	publicationReconciler := &processorTestReconciler{}
	worker := &processorTestWorker{}
	publicationFactoryCalls := 0
	temporaryFactoryCalls := 0
	runner, cleanup, err := bootstrapContentProcessor(context.Background(), config, processorBootstrapDeps{
		openPostgres: func(context.Context, string) (processorPostgresDB, error) {
			return &processorTestDB{}, nil
		},
		newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository {
			return processorTestRepository{}
		},
		newBlobStore: func(contentProcessorConfig) (attachments.BlobStore, error) {
			return processorTestBlob{}, nil
		},
		newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			return testPreviewProcessor(), nil
		},
		newWorkspace: func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error) {
			return processorTestWorkspace{}, nil
		},
		newWorkspaceReconciler: func(attachments.ProcessorRepository, contentProcessorConfig) (processorReconciler, error) {
			return &processorTestReconciler{}, nil
		},
		newPublicationReconciler: func(attachments.ProcessorRepository, attachments.BlobStore, contentProcessorConfig) (processorReconciler, error) {
			publicationFactoryCalls++
			return publicationReconciler, nil
		},
		newReconciler: func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error) {
			temporaryFactoryCalls++
			return &processorTestReconciler{}, nil
		},
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
			return worker, nil
		},
	})
	if err != nil {
		t.Fatalf("bootstrapContentProcessor() error = %v", err)
	}
	defer cleanup()
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if publicationFactoryCalls != 1 || publicationReconciler.calls != 1 || temporaryFactoryCalls != 0 || worker.calls != 1 {
		t.Fatalf("local bootstrap calls = publication factory:%d run:%d temporary factory:%d worker:%d",
			publicationFactoryCalls, publicationReconciler.calls, temporaryFactoryCalls, worker.calls)
	}
}

func TestContentProcessorBootstrapCancellationStopsBeforeClaimAndWorker(t *testing.T) {
	config := testProcessorConfig()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reconciler := &processorTestReconciler{claim: true}
	temporaryReconciler := &processorTestReconciler{}
	worker := &processorTestWorker{}
	runner, cleanup, err := bootstrapContentProcessor(context.Background(), config, processorBootstrapDeps{
		openPostgres: func(context.Context, string) (processorPostgresDB, error) {
			return &processorTestDB{}, nil
		},
		newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository { return processorTestRepository{} },
		newBlobStore:            func(contentProcessorConfig) (attachments.BlobStore, error) { return processorTestTemporaryStore{}, nil },
		newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			return testPreviewProcessor(), nil
		},
		newWorkspace: func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error) {
			return processorTestWorkspace{}, nil
		},
		newWorkspaceReconciler: func(attachments.ProcessorRepository, contentProcessorConfig) (processorReconciler, error) {
			return reconciler, nil
		},
		newReconciler: func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error) {
			return temporaryReconciler, nil
		},
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
			return worker, nil
		},
	})
	if err != nil {
		t.Fatalf("bootstrapContentProcessor() error = %v", err)
	}
	defer cleanup()
	if err := runner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(cancelled) error = %v, want context.Canceled", err)
	}
	if reconciler.calls != 0 || worker.calls != 0 {
		t.Fatalf("cancelled startup calls = reconciliation:%d worker:%d", reconciler.calls, worker.calls)
	}
}

func TestContentProcessorBootstrapBoundsReconciliationFailures(t *testing.T) {
	config := testProcessorConfig()
	config.ReconciliationMaxItems = 2
	reconciler := &processorTestReconciler{claim: true, err: attachments.ErrBlobVersionMismatch}
	temporaryReconciler := &processorTestReconciler{}
	worker := &processorTestWorker{}
	sleepCalls := 0
	runner, cleanup, err := bootstrapContentProcessor(context.Background(), config, processorBootstrapDeps{
		openPostgres:            func(context.Context, string) (processorPostgresDB, error) { return &processorTestDB{}, nil },
		newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository { return processorTestRepository{} },
		newBlobStore:            func(contentProcessorConfig) (attachments.BlobStore, error) { return processorTestTemporaryStore{}, nil },
		newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			return testPreviewProcessor(), nil
		},
		newWorkspace: func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error) {
			return processorTestWorkspace{}, nil
		},
		newWorkspaceReconciler: func(attachments.ProcessorRepository, contentProcessorConfig) (processorReconciler, error) {
			return reconciler, nil
		},
		newReconciler: func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error) {
			return temporaryReconciler, nil
		},
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
			return worker, nil
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		sleep: func(ctx context.Context, _ time.Duration) error {
			sleepCalls++
			if sleepCalls <= config.ReconciliationMaxItems {
				return nil
			}
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("bootstrapContentProcessor() error = %v", err)
	}
	defer cleanup()
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want worker to start after bounded reconciliation", err)
	}
	if reconciler.calls != 2 || worker.calls != 1 {
		t.Fatalf("bounded startup calls = reconciliation:%d worker:%d", reconciler.calls, worker.calls)
	}
}

func TestContentProcessorRuntimeBoundsBlockingStartupReconciliation(t *testing.T) {
	config := testProcessorConfig()
	config.ReconciliationMaxRuntime = 10 * time.Millisecond
	reconciler := &processorTestReconciler{
		started:     make(chan struct{}),
		allowWorker: make(chan struct{}),
	}
	worker := &processorTestWorker{}
	runtime := &contentProcessorRuntime{
		reconciler: reconciler,
		worker:     worker,
		config:     config,
		now:        time.Now,
		sleep:      sleepContentProcessor,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want worker to start after the startup reconciliation window", err)
	}
	if reconciler.calls != 1 || worker.calls != 1 {
		t.Fatalf("bounded blocking startup calls = reconciliation:%d worker:%d, want 1 each",
			reconciler.calls, worker.calls)
	}
}

func TestContentProcessorRuntimeBoundsStartupReconciliationRetrySleep(t *testing.T) {
	config := testProcessorConfig()
	config.ReconciliationMaxRuntime = 10 * time.Millisecond
	reconciler := &processorTestReconciler{err: attachments.ErrBlobVersionMismatch}
	sleepCalls := 0
	runtime := &contentProcessorRuntime{
		reconciler: reconciler,
		config:     config,
		now:        time.Now,
		sleep: func(ctx context.Context, _ time.Duration) error {
			sleepCalls++
			<-ctx.Done()
			return ctx.Err()
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := runtime.runStartupReconciliation(ctx); err != nil {
		t.Fatalf("runStartupReconciliation() error = %v, want bounded completion", err)
	}
	if reconciler.calls != 1 || sleepCalls != 1 {
		t.Fatalf("bounded startup retry calls = reconciliation:%d sleep:%d, want 1 each",
			reconciler.calls, sleepCalls)
	}
}

func TestContentProcessorRuntimePropagatesCancellationDuringStartupReconciliation(t *testing.T) {
	config := testProcessorConfig()
	reconciler := &processorTestReconciler{
		started:     make(chan struct{}),
		allowWorker: make(chan struct{}),
	}
	worker := &processorTestWorker{}
	runtime := &contentProcessorRuntime{
		reconciler: reconciler,
		worker:     worker,
		config:     config,
		now:        time.Now,
		sleep:      sleepContentProcessor,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runtime.Run(ctx)
	}()

	<-reconciler.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(cancelled during startup reconciliation) error = %v, want context.Canceled", err)
	}
	if reconciler.calls != 1 || worker.calls != 0 {
		t.Fatalf("cancelled startup calls = reconciliation:%d worker:%d, want 1/0",
			reconciler.calls, worker.calls)
	}
}

func TestLoadContentProcessorConfigRejectsIncompleteS3Secrets(t *testing.T) {
	clearProcessorEnv(t)
	setProcessorEnv(t, map[string]string{
		"HOUFENG_DATABASE_URL":                     "postgres://processor",
		"HOUFENG_ATTACHMENT_BLOB_BACKEND":          "s3",
		"HOUFENG_ATTACHMENT_S3_ENDPOINT":           "127.0.0.1:9000",
		"HOUFENG_ATTACHMENT_S3_ACCESS_KEY":         "access",
		"HOUFENG_ATTACHMENT_S3_BUCKET":             "attachments",
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT": "/var/lib/houfeng-test/processor-workdir",
	})
	if _, err := loadContentProcessorConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_ATTACHMENT_S3_SECRET_KEY") {
		t.Fatalf("loadContentProcessorConfig() error = %v, want missing S3 secret", err)
	}
}

func TestProcessorErrorClassDoesNotExposeDetails(t *testing.T) {
	secret := "super-secret-value"
	wrapped := errors.New(secret)
	class := safeProcessorErrorClass(wrapped)
	if class == "" || strings.Contains(class, secret) {
		t.Fatalf("safeProcessorErrorClass() = %q, contains secret", class)
	}
}

func TestContentProcessorRunClosesDatabaseAfterWorkerFailure(t *testing.T) {
	config := testProcessorConfig()
	database := &processorTestDB{}
	wantErr := errors.New("worker stopped")
	worker := &processorTestWorker{err: wantErr}
	err := runContentProcessor(context.Background(), config, processorBootstrapDeps{
		openPostgres:            func(context.Context, string) (processorPostgresDB, error) { return database, nil },
		newAttachmentRepository: func(*pgxpool.Pool) attachments.ProcessorRepository { return processorTestRepository{} },
		newBlobStore:            func(contentProcessorConfig) (attachments.BlobStore, error) { return processorTestTemporaryStore{}, nil },
		newPreviewProcessor: func(contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			return testPreviewProcessor(), nil
		},
		newWorkspace: func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error) {
			return processorTestWorkspace{}, nil
		},
		newWorkspaceReconciler: func(attachments.ProcessorRepository, contentProcessorConfig) (processorReconciler, error) {
			return &processorTestReconciler{}, nil
		},
		newReconciler: func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error) {
			return &processorTestReconciler{}, nil
		},
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
			return worker, nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runContentProcessor() error = %v, want worker failure", err)
	}
	if !database.closed {
		t.Fatal("runContentProcessor() did not close database")
	}
}

type processorTestWorkspace struct{}

func (processorTestWorkspace) Process(context.Context, attachments.ProcessorWorkspaceProcessRequest) (attachments.PreviewArtifact, attachments.ProcessorWorkspacePurgeReceipt, error) {
	return attachments.PreviewArtifact{}, attachments.ProcessorWorkspacePurgeReceipt{}, nil
}

func testPreviewProcessor() *attachments.PreviewProcessor {
	processor, _ := attachments.NewPreviewProcessor(attachments.PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 1024,
		PDFInfoBinary: "/usr/bin/pdfinfo", PDFToPPMBinary: "/usr/bin/pdftoppm",
	})
	return processor
}

func clearProcessorEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HOUFENG_DATABASE_URL", "HOUFENG_ATTACHMENT_BLOB_BACKEND", "HOUFENG_ATTACHMENT_BLOB_ROOT",
		"HOUFENG_ATTACHMENT_S3_ENDPOINT", "HOUFENG_ATTACHMENT_S3_ACCESS_KEY", "HOUFENG_ATTACHMENT_S3_SECRET_KEY",
		"HOUFENG_ATTACHMENT_S3_BUCKET", "HOUFENG_ATTACHMENT_S3_SECURE", "HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT",
	} {
		t.Setenv(key, "")
	}
}

func setProcessorEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		t.Setenv(key, value)
	}
}

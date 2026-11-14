package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/sourceretirement"
)

type processorTestDB struct {
	pool       *pgxpool.Pool
	closed     bool
	closeCalls int
}

func (db *processorTestDB) Close() {
	db.closed = true
	db.closeCalls++
}

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

func TestProcessorReconcilerGroupDoesNotStarveLaterCleanup(t *testing.T) {
	first := &processorTestReconciler{claim: true}
	second := &processorTestReconciler{claim: true}
	third := &processorTestReconciler{claim: true}
	group, err := newProcessorReconcilerGroup(first, second, third)
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if claimed, err := group.RunOnce(context.Background()); err != nil || !claimed {
			t.Fatalf("cleanup progress: claimed=%t error=%v", claimed, err)
		}
	}
	if first.calls != 2 || second.calls != 2 || third.calls != 2 {
		t.Fatalf("busy cleanup starved another owner: %d/%d/%d", first.calls, second.calls, third.calls)
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
			_, err := dependencies.newPublicationReconciler(tt.repository, tt.blob, nil, config)
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

	reconciler, err := dependencies.newPublicationReconciler(repository, blob, nil, config)
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
			loadRuntimeLaunch:   processorTestLaunch,
			verifyRuntimeLaunch: processorTestVerifyLaunch,
			openPostgres: func(context.Context, string) (processorPostgresDB, error) {
				return &processorTestDB{}, nil
			},
			newAttachmentRepository: func(context.Context, contentProcessorConfig, *pgxpool.Pool) (attachments.ProcessorRepository, func(), error) {
				return repository, func() {}, nil
			},
			newBlobStore: func(context.Context, contentProcessorConfig, attachments.LocalNamespaceRepository) (attachments.BlobStore, error) {
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
			newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, attachments.BlobWriter, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
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

var processorTestDeployment = "dp-" + strings.Repeat("c", 64)

func processorTestLaunch(_ string, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
	return recordbirth.LaunchBinding{ProcessKind: kind, DeploymentID: processorTestDeployment}, sha256.Sum256([]byte("processor test launch")), nil
}

func processorTestVerifyLaunch(context.Context, *pgxpool.Pool, *recordbirth.PostgresAuthority, recordbirth.LaunchBinding) error {
	return nil
}

func testProcessorConfig() contentProcessorConfig {
	return contentProcessorConfig{
		DatabaseURL:              "postgres://processor",
		RecordDeploymentID:       processorTestDeployment,
		RecordInstanceID:         "processor-test",
		RecordInstanceKind:       "worker",
		RecordInstanceCapability: "records.runtime",
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

func TestContentProcessorBootstrapCancellationStopsBeforeClaimAndWorker(t *testing.T) {
	config := testProcessorConfig()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reconciler := &processorTestReconciler{claim: true}
	temporaryReconciler := &processorTestReconciler{}
	worker := &processorTestWorker{}
	runner, cleanup, err := bootstrapContentProcessor(context.Background(), config, processorBootstrapDeps{
		loadRuntimeLaunch:   processorTestLaunch,
		verifyRuntimeLaunch: processorTestVerifyLaunch,
		openPostgres: func(context.Context, string) (processorPostgresDB, error) {
			return &processorTestDB{}, nil
		},
		newAttachmentRepository: func(context.Context, contentProcessorConfig, *pgxpool.Pool) (attachments.ProcessorRepository, func(), error) {
			return processorTestRepository{}, func() {}, nil
		},
		newBlobStore: func(context.Context, contentProcessorConfig, attachments.LocalNamespaceRepository) (attachments.BlobStore, error) {
			return processorTestTemporaryStore{}, nil
		},
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
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, attachments.BlobWriter, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
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
		loadRuntimeLaunch:   processorTestLaunch,
		verifyRuntimeLaunch: processorTestVerifyLaunch,
		openPostgres:        func(context.Context, string) (processorPostgresDB, error) { return &processorTestDB{}, nil },
		newAttachmentRepository: func(context.Context, contentProcessorConfig, *pgxpool.Pool) (attachments.ProcessorRepository, func(), error) {
			return processorTestRepository{}, func() {}, nil
		},
		newBlobStore: func(context.Context, contentProcessorConfig, attachments.LocalNamespaceRepository) (attachments.BlobStore, error) {
			return processorTestTemporaryStore{}, nil
		},
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
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, attachments.BlobWriter, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
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

func TestLoadContentProcessorConfigRequiresExplicitBlobBackend(t *testing.T) {
	clearProcessorEnv(t)
	unsetProcessorEnv(t, "HOUFENG_ATTACHMENT_BLOB_BACKEND")
	setProcessorEnv(t, map[string]string{
		"HOUFENG_DATABASE_URL":                     "postgres://processor",
		"HOUFENG_ATTACHMENT_BLOB_ROOT":             "/var/lib/houfeng-test/attachments",
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT": "/var/lib/houfeng-test/processor-workdir",
	})

	if _, err := loadContentProcessorConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_ATTACHMENT_BLOB_BACKEND") {
		t.Fatalf("loadContentProcessorConfig() error = %v, want explicit Blob backend requirement", err)
	}
}

func TestLoadContentProcessorConfigUsesSharedAttachmentBounds(t *testing.T) {
	clearProcessorEnv(t)
	setProcessorEnv(t, map[string]string{
		"HOUFENG_DATABASE_URL":                     "postgres://processor",
		"HOUFENG_ATTACHMENT_BLOB_BACKEND":          "local",
		"HOUFENG_ATTACHMENT_BLOB_ROOT":             "/var/lib/houfeng-test/attachments",
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT": "/var/lib/houfeng-test/processor-workdir",
		"HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS":   "9",
		"HOUFENG_CLAMAV_NETWORK":                   "tcp",
		"HOUFENG_CLAMAV_ADDRESS":                   "clamav.internal:3310",
		"HOUFENG_CLAMAV_DIAL_TIMEOUT":              "4s",
		"HOUFENG_CLAMAV_OPERATION_TIMEOUT":         "50s",
		"HOUFENG_CLAMAV_CHUNK_SIZE":                "16384",
		"HOUFENG_CLAMAV_RESPONSE_LIMIT":            "1024",
	})

	cfg, err := loadContentProcessorConfig()
	if err != nil {
		t.Fatalf("loadContentProcessorConfig() error = %v", err)
	}
	if cfg.BlobBackend != attachments.BackendKindLocal || cfg.BlobRoot != "/var/lib/houfeng-test/attachments" ||
		cfg.ProcessorMaxAttempts != 9 || cfg.Limits != attachments.DefaultLimits() {
		t.Fatalf("shared attachment config = %#v", cfg)
	}
	if cfg.ClamAVNetwork != "tcp" || cfg.ClamAVAddress != "clamav.internal:3310" ||
		cfg.ClamAVDialTimeout != 4*time.Second || cfg.ClamAVOperationTimeout != 50*time.Second ||
		cfg.ClamAVChunkSize != 16384 || cfg.ClamAVResponseLimit != 1024 {
		t.Fatalf("shared scanner config = %#v", cfg)
	}
}

func TestLoadContentProcessorConfigReadsBirthReaderAndLaunch(t *testing.T) {
	clearProcessorEnv(t)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR" {
			unsetProcessorEnv(t, name)
		}
	}
	credential := filepath.Join(t.TempDir(), "reader-credential")
	if err := os.WriteFile(credential, []byte("fixture-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(t.TempDir(), "reader-identity.json")
	launch := filepath.Join(t.TempDir(), "processor-launch.json")
	setProcessorEnv(t, map[string]string{
		"HOUFENG_DATABASE_URL":                        "postgres://processor",
		"HOUFENG_ATTACHMENT_BLOB_BACKEND":             "local",
		"HOUFENG_ATTACHMENT_BLOB_ROOT":                "/var/lib/houfeng-test/attachments",
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT":    "/var/lib/houfeng-test/processor-workdir",
		"HOUFENG_RECORD_BIRTH_READER_DATABASE_URL":    "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable",
		"HOUFENG_RECORD_BIRTH_READER_IDENTITY_FILE":   identity,
		"HOUFENG_RECORD_BIRTH_READER_CREDENTIAL_FILE": credential,
		"HOUFENG_RECORD_BIRTH_LAUNCH_FILE":            launch,
	})
	cfg, err := loadContentProcessorConfig()
	if err != nil {
		t.Fatalf("load processor birth configuration: %v", err)
	}
	if cfg.BirthLaunchFile != launch || cfg.BirthReader.IdentityFile != identity || cfg.BirthReader.CredentialFile != credential ||
		cfg.BirthReader.Connection == nil || cfg.BirthReader.Connection.ConnConfig.User != "birth_reader" ||
		cfg.BirthReader.Connection.ConnConfig.Password != "fixture-password" {
		t.Fatal("processor did not load the independent reader and launch configuration")
	}
	unsetProcessorEnv(t, "HOUFENG_RECORD_BIRTH_READER_CREDENTIAL_FILE")
	if _, err := loadContentProcessorConfig(); err == nil {
		t.Fatal("processor accepted an incomplete independent reader configuration")
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

func TestContentProcessorRejectsUntrustedLaunchBeforeOpeningAPP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dsn    string
		loader func(string, string) (recordbirth.LaunchBinding, [32]byte, error)
	}{
		{"missing launch", "postgres://processor", func(string, string) (recordbirth.LaunchBinding, [32]byte, error) {
			return recordbirth.LaunchBinding{}, [32]byte{}, errors.New("missing")
		}},
		{"different deployment", "postgres://processor", func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
			launch, digest, err := processorTestLaunch(path, kind)
			launch.DeploymentID = "dp-" + strings.Repeat("d", 64)
			return launch, digest, err
		}},
		{"different process", "postgres://processor", func(path, _ string) (recordbirth.LaunchBinding, [32]byte, error) {
			return processorTestLaunch(path, "center")
		}},
		{"preloaded digest", "postgres://processor?houfeng.birth_launch_digest=old", processorTestLaunch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testProcessorConfig()
			cfg.DatabaseURL = tc.dsn
			opened := false
			_, _, err := bootstrapContentProcessor(context.Background(), cfg, processorBootstrapDeps{
				loadRuntimeLaunch: tc.loader,
				openPostgres: func(context.Context, string) (processorPostgresDB, error) {
					opened = true
					return nil, errors.New("unexpected APP connection")
				},
			})
			if err == nil || opened {
				t.Fatalf("launch rejection = %v, APP opened = %t", err, opened)
			}
		})
	}
}

func TestContentProcessorDefaultLaunchLoaderRejectsMissingFile(t *testing.T) {
	cfg := testProcessorConfig()
	cfg.BirthLaunchFile = filepath.Join(t.TempDir(), "missing-launch.json")
	opened := false
	_, _, err := bootstrapContentProcessor(context.Background(), cfg, processorBootstrapDeps{
		openPostgres: func(context.Context, string) (processorPostgresDB, error) {
			opened = true
			return nil, errors.New("unexpected APP connection")
		},
	})
	if err == nil || opened {
		t.Fatalf("missing launch error = %v, APP opened = %t", err, opened)
	}
}

func TestContentProcessorClosesAPPWhenLaunchContinuityFails(t *testing.T) {
	database := &processorTestDB{}
	wantErr := errors.New("checkpoint ancestry unavailable")
	cfg := testProcessorConfig()
	cfg.BirthLaunchFile = "/protected/processor-launch.json"
	cfg.BirthReader.IdentityFile = "/not-read-for-never-installed"
	_, _, err := bootstrapContentProcessor(context.Background(), cfg, processorBootstrapDeps{
		loadRuntimeLaunch: func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
			if path != cfg.BirthLaunchFile || kind != "content-processor" {
				t.Fatalf("launch request = %q %q", path, kind)
			}
			return processorTestLaunch(path, kind)
		},
		openPostgres: func(_ context.Context, dsn string) (processorPostgresDB, error) {
			connection, parseErr := pgxpool.ParseConfig(dsn)
			if parseErr != nil {
				t.Fatalf("parse APP connection: %v", parseErr)
			}
			_, digest, _ := processorTestLaunch("", "content-processor")
			if got := connection.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"]; got != hex.EncodeToString(digest[:]) {
				t.Fatalf("APP launch digest = %q", got)
			}
			return database, nil
		},
		verifyRuntimeLaunch: func(_ context.Context, _ *pgxpool.Pool, reader *recordbirth.PostgresAuthority, launch recordbirth.LaunchBinding) error {
			if reader != nil || launch.BirthInstalled {
				t.Fatal("never-installed startup unexpectedly opened W2")
			}
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) || database.closeCalls != 1 {
		t.Fatalf("verification failure = %v, APP closes = %d", err, database.closeCalls)
	}
}

func TestContentProcessorInstalledBirthRequiresReader(t *testing.T) {
	database := &processorTestDB{}
	verified := false
	_, _, err := bootstrapContentProcessor(context.Background(), testProcessorConfig(), processorBootstrapDeps{
		loadRuntimeLaunch: func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
			launch, digest, err := processorTestLaunch(path, kind)
			launch.BirthInstalled = true
			return launch, digest, err
		},
		openPostgres: func(context.Context, string) (processorPostgresDB, error) { return database, nil },
		verifyRuntimeLaunch: func(context.Context, *pgxpool.Pool, *recordbirth.PostgresAuthority, recordbirth.LaunchBinding) error {
			verified = true
			return nil
		},
	})
	if err == nil || verified || database.closeCalls != 1 {
		t.Fatalf("missing reader error = %v, verified = %t, APP closes = %d", err, verified, database.closeCalls)
	}
}

func TestOpenProcessorBirthReaderUsesDistinctPrincipal(t *testing.T) {
	identity := sourceretirement.DomainIdentity{ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "12345", DatabaseOID: 16384, DatabaseName: "fixture", Epoch: 1}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	identityFile := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(identityFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	appConfig, err := pgxpool.ParseConfig("postgres://app_runtime@127.0.0.1:1/fixture?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	app, err := pgxpool.NewWithConfig(context.Background(), appConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, tc := range []struct {
		name string
		user string
		bad  bool
	}{
		{"independent", "birth_reader", false},
		{"APP principal", "app_runtime", true},
		{"source principal", "source_reader", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testProcessorConfig()
			cfg.SourceWitnessDatabaseURL = "postgres://source_reader@127.0.0.1:1/fixture?sslmode=disable"
			cfg.BirthReader.Connection, err = pgxpool.ParseConfig("postgres://" + tc.user + "@127.0.0.1:1/fixture?sslmode=disable")
			if err != nil {
				t.Fatal(err)
			}
			cfg.BirthReader.IdentityFile, cfg.BirthReader.CredentialFile = identityFile, "fixture-credential"
			reader, pool, err := openProcessorBirthReader(context.Background(), cfg, app)
			if tc.bad {
				if err == nil || reader != nil || pool != nil {
					t.Fatal("accepted reused reader principal")
				}
				return
			}
			if err != nil || reader == nil || pool == nil {
				t.Fatalf("open independent reader: %v", err)
			}
			pool.Close()
		})
	}
	t.Run("installed startup passes the independent reader to verification", func(t *testing.T) {
		cfg := testProcessorConfig()
		cfg.SourceWitnessDatabaseURL = "postgres://source_reader@127.0.0.1:1/fixture?sslmode=disable"
		cfg.BirthReader.Connection, err = pgxpool.ParseConfig("postgres://birth_reader@127.0.0.1:1/fixture?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		cfg.BirthReader.IdentityFile, cfg.BirthReader.CredentialFile = identityFile, "fixture-credential"
		database := &processorTestDB{pool: app}
		wantErr := errors.New("stop after reader verification")
		verified := false
		_, _, err := bootstrapContentProcessor(context.Background(), cfg, processorBootstrapDeps{
			loadRuntimeLaunch: func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
				launch, digest, err := processorTestLaunch(path, kind)
				launch.BirthInstalled = true
				return launch, digest, err
			},
			openPostgres: func(context.Context, string) (processorPostgresDB, error) { return database, nil },
			verifyRuntimeLaunch: func(_ context.Context, _ *pgxpool.Pool, reader *recordbirth.PostgresAuthority, _ recordbirth.LaunchBinding) error {
				verified = reader != nil
				return wantErr
			},
		})
		if !errors.Is(err, wantErr) || !verified || database.closeCalls != 1 {
			t.Fatalf("reader verification = %v, verified = %t, APP closes = %d", err, verified, database.closeCalls)
		}
	})
}

func TestContentProcessorRunClosesDatabaseAfterWorkerFailure(t *testing.T) {
	config := testProcessorConfig()
	database := &processorTestDB{}
	authorityCloses := 0
	wantErr := errors.New("worker stopped")
	worker := &processorTestWorker{err: wantErr}
	err := runContentProcessor(context.Background(), config, processorBootstrapDeps{
		loadRuntimeLaunch:   processorTestLaunch,
		verifyRuntimeLaunch: processorTestVerifyLaunch,
		openPostgres:        func(context.Context, string) (processorPostgresDB, error) { return database, nil },
		newAttachmentRepository: func(context.Context, contentProcessorConfig, *pgxpool.Pool) (attachments.ProcessorRepository, func(), error) {
			return processorTestRepository{}, func() { authorityCloses++ }, nil
		},
		newBlobStore: func(context.Context, contentProcessorConfig, attachments.LocalNamespaceRepository) (attachments.BlobStore, error) {
			return processorTestTemporaryStore{}, nil
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
		newReconciler: func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error) {
			return &processorTestReconciler{}, nil
		},
		newWorker: func(attachments.ProcessorRepository, attachments.BlobStore, attachments.BlobWriter, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error) {
			return worker, nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runContentProcessor() error = %v, want worker failure", err)
	}
	if !database.closed {
		t.Fatal("runContentProcessor() did not close database")
	}
	if authorityCloses != 1 {
		t.Fatalf("runContentProcessor() authority closes = %d, want 1", authorityCloses)
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
		"HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE", "HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE",
		"HOUFENG_CLAMAV_NETWORK", "HOUFENG_CLAMAV_ADDRESS",
		"HOUFENG_CLAMAV_DIAL_TIMEOUT", "HOUFENG_CLAMAV_OPERATION_TIMEOUT", "HOUFENG_CLAMAV_CHUNK_SIZE",
		"HOUFENG_CLAMAV_RESPONSE_LIMIT",
	} {
		t.Setenv(key, "")
	}
	for _, key := range []string{"HOUFENG_RECORD_BIRTH_LAUNCH_FILE", "HOUFENG_RECORD_BIRTH_READER_DATABASE_URL", "HOUFENG_RECORD_BIRTH_READER_IDENTITY_FILE", "HOUFENG_RECORD_BIRTH_READER_CREDENTIAL_FILE"} {
		unsetProcessorEnv(t, key)
	}
}

func setProcessorEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func unsetProcessorEnv(t *testing.T, key string) {
	t.Helper()
	value, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}

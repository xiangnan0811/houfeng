package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"time"
)

type workerRepositoryStub struct {
	claim             *ProcessorClaim
	completed         []ProcessorCompletionInput
	publicationIntent BlobPublicationIntent
	renewed           int
	claimCount        int
	abandonedExpired  int
	abandonedResult   *UploadMutationResult
	abandonedErr      error
	expired           int
	expireErr         error
}

func (r *workerRepositoryStub) ClaimProcessorJob(context.Context, ProcessorClaimInput) (*ProcessorClaim, error) {
	r.claimCount++
	claim := r.claim
	r.claim = nil
	return claim, nil
}
func (r *workerRepositoryStub) RenewProcessorClaim(_ context.Context, input ProcessorRenewInput) (ProcessorClaim, error) {
	r.renewed++
	return input.Claim, nil
}
func (r *workerRepositoryStub) PrepareBlobPublication(_ context.Context, request BlobPublicationPrepareRequest) (BlobPublicationIntent, error) {
	if r.publicationIntent != (BlobPublicationIntent{}) {
		return r.publicationIntent, nil
	}
	return BlobPublicationIntent{
		PublicationID: "bpi_worker1", ProjectID: request.ProjectID,
		OwnerKind: request.OwnerKind, OwnerID: request.OwnerID, OwnerGeneration: request.OwnerGeneration,
		Target: request.Target, State: BlobPublicationStatePrepared, PublishExpiresAt: request.PublishExpiresAt,
	}, nil
}
func (r *workerRepositoryStub) RecordBlobPublicationVersion(_ context.Context, request BlobPublicationVersionRequest) (BlobPublicationIntent, error) {
	intent := request.Intent
	if intent.State == BlobPublicationStatePublished {
		return intent, nil
	}
	intent.ObjectVersion = request.Object.VersionID
	intent.State = BlobPublicationStatePublished
	r.publicationIntent = intent
	return intent, nil
}
func (r *workerRepositoryStub) CompleteProcessorJob(_ context.Context, input ProcessorCompletionInput) (ProcessorCompletionResult, error) {
	r.completed = append(r.completed, input)
	return ProcessorCompletionResult{ProjectID: "default", ProcessorJobID: input.Claim.ProcessorJobID, UploadID: input.Claim.UploadID, AttachmentID: input.Claim.AttachmentID, ProcessorState: ProcessorStateSucceeded, ResultCode: input.Result.Code}, nil
}
func (r *workerRepositoryStub) ExpireAbandonedUpload(context.Context, AbandonedUploadExpiryInput) (*UploadMutationResult, error) {
	r.abandonedExpired++
	return r.abandonedResult, r.abandonedErr
}
func (r *workerRepositoryStub) ExpireBoundedProcessorJob(context.Context, ProcessorExpiryInput) (*ProcessorCompletionResult, error) {
	r.expired++
	return nil, r.expireErr
}
func (r *workerRepositoryStub) RegisterProcessorWorkspace(context.Context, ProcessorWorkspaceRegistration) (ProcessorWorkspace, error) {
	return ProcessorWorkspace{}, nil
}
func (r *workerRepositoryStub) MaterializeProcessorWorkspace(context.Context, ProcessorWorkspaceTransition) (ProcessorWorkspace, error) {
	return ProcessorWorkspace{}, nil
}
func (r *workerRepositoryStub) BeginProcessorWorkspacePurge(context.Context, ProcessorWorkspaceTransition) (ProcessorWorkspacePurgePlan, error) {
	return ProcessorWorkspacePurgePlan{}, nil
}
func (r *workerRepositoryStub) CompleteProcessorWorkspacePurge(context.Context, ProcessorWorkspacePurgeCompletion) (ProcessorWorkspacePurgeReceipt, error) {
	return ProcessorWorkspacePurgeReceipt{}, nil
}

type workerBlobStub struct {
	opened   []ObjectVersion
	data     []byte
	put      int
	closeErr error
}

func (b *workerBlobStub) Put(_ context.Context, request PutRequest, reader io.Reader) (ObjectVersion, error) {
	b.put++
	content, _ := io.ReadAll(reader)
	return ObjectVersion{Key: "sha256/" + hexDigest(request.ExpectedSHA256), VersionID: "preview-v1", SHA256: request.ExpectedSHA256, SizeBytes: int64(len(content))}, nil
}
func (b *workerBlobStub) Open(_ context.Context, version ObjectVersion, _ ByteRange) (io.ReadCloser, error) {
	b.opened = append(b.opened, version)
	return workerReadCloser{Reader: bytes.NewReader(b.data), closeErr: b.closeErr}, nil
}
func (b *workerBlobStub) Stat(context.Context, ObjectVersion) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (b *workerBlobStub) Delete(context.Context, ObjectVersion) (DeletionReceipt, error) {
	return DeletionReceipt{}, nil
}

type workerReadCloser struct {
	io.Reader
	closeErr error
}

func (r workerReadCloser) Close() error { return r.closeErr }

type workerWorkspaceStub struct {
	artifact PreviewArtifact
	err      error
	seen     ProcessorWorkspaceProcessRequest
}

func (w *workerWorkspaceStub) Process(_ context.Context, request ProcessorWorkspaceProcessRequest) (PreviewArtifact, ProcessorWorkspacePurgeReceipt, error) {
	w.seen = request
	receipt, err := NewProcessorWorkspacePurgeReceipt(request.WorkspaceID, 1, time.Unix(1, 0))
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	return w.artifact, receipt, w.err
}

func TestProcessorWorkerExpiresAbandonedUploadBeforeClaimingProcessorJob(t *testing.T) {
	result := &UploadMutationResult{
		UploadID: "aup_abandoned1", AttachmentID: "att_abandoned1", State: UploadStateExpired,
	}
	claim := workerTestClaim([]byte("hello"), ProcessorProfileText)
	repository := &workerRepositoryStub{claim: &claim, abandonedResult: result}
	worker, err := NewProcessorWorker(
		repository,
		&workerBlobStub{data: []byte("hello")},
		&workerWorkspaceStub{},
		ProcessorWorkerConfig{
			OwnerID: "worker1", OwnerLeaseDuration: time.Minute,
			Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repository.abandonedExpired != 1 || repository.expired != 0 || repository.claimCount != 0 {
		t.Fatalf("expiry/processor calls = abandoned %d bounded %d claims %d, want 1/0/0",
			repository.abandonedExpired, repository.expired, repository.claimCount)
	}
}

func TestProcessorWorkerClaimsExactSourceProcessesAndCompletes(t *testing.T) {
	source := []byte("hello")
	claim := workerTestClaim(source, ProcessorProfileText)
	repository := &workerRepositoryStub{claim: &claim}
	blob := &workerBlobStub{data: source}
	workspace := &workerWorkspaceStub{artifact: PreviewArtifact{HasPreview: true, MediaType: ManagedPreviewMediaTypeTextUTF8, Bytes: source}}
	worker, err := NewProcessorWorker(repository, blob, workspace, ProcessorWorkerConfig{OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(blob.opened) != 2 || blob.opened[0].Key != claim.Source.Key || blob.opened[0].VersionID != claim.Source.ObjectVersion || blob.opened[1] != blob.opened[0] {
		t.Fatalf("opened source = %#v, want exact claim source", blob.opened)
	}
	if len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeClean || !repository.completed[0].Result.HasPreview {
		t.Fatalf("completion = %#v", repository.completed)
	}
	if repository.completed[0].PreviewPublicationIntent.State != BlobPublicationStatePublished {
		t.Fatalf("completion preview publication intent = %#v, want published", repository.completed[0].PreviewPublicationIntent)
	}
}

func TestProcessorWorkerCancellationDoesNotComplete(t *testing.T) {
	source := []byte("hello")
	claim := workerTestClaim(source, ProcessorProfileText)
	repository := &workerRepositoryStub{claim: &claim}
	blob := &workerBlobStub{data: source}
	workspace := &workerWorkspaceStub{err: context.Canceled}
	worker, err := NewProcessorWorker(repository, blob, workspace, ProcessorWorkerConfig{OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want cancellation", err)
	}
	if len(repository.completed) != 0 {
		t.Fatalf("cancellation completed job: %#v", repository.completed)
	}
}

func TestProcessorWorkerArchiveRequiresScanner(t *testing.T) {
	content := archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("archive")}})
	claim := workerTestClaim(content, ProcessorProfileArchive)
	claim.DisplayName = "bundle.zip"
	claim.DeclaredMediaType = "application/zip"
	repository := &workerRepositoryStub{claim: &claim}
	worker, err := NewProcessorWorker(repository, &workerBlobStub{data: content}, &workerWorkspaceStub{}, ProcessorWorkerConfig{OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeScannerUnavailable {
		t.Fatalf("archive completion = %#v", repository.completed)
	}
}

func TestProcessorWorkerArchiveScannerVerdictIsTyped(t *testing.T) {
	content := archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("archive")}})
	claim := workerTestClaim(content, ProcessorProfileArchive)
	claim.DisplayName = "bundle.zip"
	claim.DeclaredMediaType = "application/zip"
	repository := &workerRepositoryStub{claim: &claim}
	blob := &workerBlobStub{data: content}
	workspace := &workerWorkspaceStub{}
	worker, err := NewProcessorWorker(repository, blob, workspace, ProcessorWorkerConfig{OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3, Scan: func(_ context.Context, content io.Reader) (ProcessorResultCode, error) {
		got, _ := io.ReadAll(content)
		if !bytes.Equal(got, blob.data) {
			t.Fatalf("scanner content differs from exact archive")
		}
		return ProcessorResultCodeMalware, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(blob.opened) != 2 || len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeMalware {
		t.Fatalf("scanner completion = %#v opened=%#v", repository.completed, blob.opened)
	}
}

func TestProcessorWorkerSourceCloseFailureDoesNotCompleteClean(t *testing.T) {
	source := []byte("hello")
	claim := workerTestClaim(source, ProcessorProfileText)
	repository := &workerRepositoryStub{claim: &claim}
	worker, err := NewProcessorWorker(repository, &workerBlobStub{data: source, closeErr: errors.New("close")}, &workerWorkspaceStub{artifact: PreviewArtifact{HasPreview: true, MediaType: ManagedPreviewMediaTypeTextUTF8, Bytes: source}}, ProcessorWorkerConfig{OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeProcessingError {
		t.Fatalf("close failure completion = %#v", repository.completed)
	}
}

func TestProcessorWorkerRunsAdmissionBeforePreview(t *testing.T) {
	source := []byte("<script>alert(1)</script>")
	claim := workerTestClaim(source, ProcessorProfileText)
	claim.DisplayName = "notes.txt"
	claim.DeclaredMediaType = "text/plain"
	repository := &workerRepositoryStub{claim: &claim}
	blob := &workerBlobStub{data: source}
	workspace := &workerWorkspaceStub{artifact: PreviewArtifact{
		HasPreview: true, MediaType: ManagedPreviewMediaTypeTextUTF8, Bytes: source,
	}}
	worker, err := NewProcessorWorker(repository, blob, workspace, ProcessorWorkerConfig{
		OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(),
		PreviewBackendKind: BackendKindS3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeUnsafeContent {
		t.Fatalf("admission completion = %#v, want unsafe_content", repository.completed)
	}
	if workspace.seen.WorkspaceID != "" {
		t.Fatalf("unsafe content reached workspace: %#v", workspace.seen)
	}
}

func TestProcessorWorkerRejectsPreviewBackendMismatch(t *testing.T) {
	source := []byte("hello")
	claim := workerTestClaim(source, ProcessorProfileText)
	claim.DisplayName = "notes.txt"
	claim.DeclaredMediaType = "text/plain"
	repository := &workerRepositoryStub{claim: &claim}
	blob := &workerBlobStub{data: source}
	workspace := &workerWorkspaceStub{artifact: PreviewArtifact{
		HasPreview: true, MediaType: ManagedPreviewMediaTypeTextUTF8, Bytes: source,
	}}
	worker, err := NewProcessorWorker(repository, blob, workspace, ProcessorWorkerConfig{
		OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(),
		PreviewBackendKind: BackendKindLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeProcessingError {
		t.Fatalf("preview backend mismatch completion = %#v, want processing_error", repository.completed)
	}
	if blob.put != 0 {
		t.Fatalf("preview backend mismatch published %d objects", blob.put)
	}
}

func TestProcessorWorkerRejectsMissingAdmissionMetadata(t *testing.T) {
	claim := workerTestClaim([]byte("hello"), ProcessorProfileText)
	claim.DisplayName = ""
	repository := &workerRepositoryStub{claim: &claim}
	worker, err := NewProcessorWorker(repository, &workerBlobStub{data: []byte("hello")}, &workerWorkspaceStub{}, ProcessorWorkerConfig{
		OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(),
		PreviewBackendKind: BackendKindS3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repository.completed) != 1 || repository.completed[0].Result.Code != ProcessorResultCodeProcessingError {
		t.Fatalf("missing metadata completion = %#v, want processing_error", repository.completed)
	}
}

func workerTestClaim(source []byte, profile ProcessorProfile) ProcessorClaim {
	digest := sha256.Sum256(source)
	now := time.Now().UTC().Truncate(time.Microsecond)
	return ProcessorClaim{ProjectID: "default", ProcessorJobID: "apj_worker1", UploadID: "aup_worker1", AttachmentID: "att_worker1", DisplayName: "notes.txt", DeclaredMediaType: "text/plain", Source: BlobObject{Key: "sha256/" + hexDigest(digest), SHA256: digest, ObjectVersion: "source-v1", SizeBytes: int64(len(source)), BackendKind: BackendKindS3}, Profile: profile, Attempt: 1, MaxAttempts: 3, OwnerID: "worker1", OwnerGeneration: 1, LeaseExpiresAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}
}

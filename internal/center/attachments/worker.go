package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strconv"
	"time"
)

// processorWorkspaceRunner is the small seam used by the worker.  The
// concrete ContentProcessorWorkspace implements this interface.
type processorWorkspaceRunner interface {
	Process(context.Context, ProcessorWorkspaceProcessRequest) (PreviewArtifact, ProcessorWorkspacePurgeReceipt, error)
}

// ProcessorScanner is injected so workers can use the ClamAV implementation
// without coupling orchestration to a particular transport.
type ProcessorScanner func(context.Context, io.Reader) (ProcessorResultCode, error)

// ProcessorWorkerCutpoint identifies a durable boundary at which tests or a
// supervisor may simulate a process crash. Hooks are optional and carry no
// attachment identity or content.
type ProcessorWorkerCutpoint string

const (
	ProcessorWorkerCutpointAfterClaim        ProcessorWorkerCutpoint = "after_claim"
	ProcessorWorkerCutpointAfterResultCommit ProcessorWorkerCutpoint = "after_result_commit"
)

type ProcessorWorkerConfig struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
	Limits             Limits
	AdmissionLimits    AdmissionLimits
	PreviewBackendKind BackendKind
	Now                func() time.Time
	Sleep              func(context.Context, time.Duration) error
	Backoff            func(int64) time.Duration
	NewWorkspaceID     func(ProcessorClaim) (string, error)
	Scan               ProcessorScanner
	Cutpoint           func(ProcessorWorkerCutpoint) error
}

type ProcessorWorker struct {
	repository ProcessorRepository
	blob       BlobStore
	workspace  processorWorkspaceRunner
	config     ProcessorWorkerConfig
}

func NewProcessorWorker(repository ProcessorRepository, blob BlobStore, workspace processorWorkspaceRunner, config ProcessorWorkerConfig) (*ProcessorWorker, error) {
	if repository == nil || blob == nil || workspace == nil || !validProcessorOwnerID(config.OwnerID) ||
		config.OwnerLeaseDuration < time.Microsecond || config.OwnerLeaseDuration > maxProcessorOwnerLeaseDuration ||
		config.Limits.Validate() != nil ||
		(config.PreviewBackendKind != BackendKindLocal && config.PreviewBackendKind != BackendKindS3) {
		return nil, ErrInvalidProcessorCommand
	}
	if config.AdmissionLimits == (AdmissionLimits{}) {
		config.AdmissionLimits = DefaultAdmissionLimits(config.Limits)
	}
	if config.AdmissionLimits.Validate() != nil {
		return nil, ErrInvalidProcessorCommand
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Sleep == nil {
		config.Sleep = sleepProcessorWorker
	}
	if config.Backoff == nil {
		config.Backoff = defaultProcessorWorkerBackoff
	}
	if config.NewWorkspaceID == nil {
		config.NewWorkspaceID = defaultProcessorWorkspaceID
	}
	return &ProcessorWorker{repository: repository, blob: blob, workspace: workspace, config: config}, nil
}

// Run drains jobs until cancellation.  There is at most one claim per loop
// iteration, and all waiting is bounded by the injected backoff/sleeper.
func (worker *ProcessorWorker) Run(ctx context.Context) error {
	if ctx == nil || worker == nil {
		return ErrInvalidProcessorCommand
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		claimed, err := worker.runOnce(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if sleepErr := worker.config.Sleep(ctx, worker.config.Backoff(0)); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		if !claimed {
			if err := worker.config.Sleep(ctx, worker.config.Backoff(0)); err != nil {
				return err
			}
		}
	}
}

// RunOnce performs one bounded claim/process/complete cycle.
func (worker *ProcessorWorker) RunOnce(ctx context.Context) error {
	_, err := worker.runOnce(ctx)
	return err
}

func (worker *ProcessorWorker) runOnce(ctx context.Context) (bool, error) {
	if ctx == nil || worker == nil {
		return false, ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	expiredUpload, err := worker.repository.ExpireAbandonedUpload(ctx, AbandonedUploadExpiryInput{
		ProjectID: "default", Limits: worker.config.Limits,
	})
	if err != nil {
		return false, err
	}
	if expiredUpload != nil {
		if ValidateUploadID(expiredUpload.UploadID) != nil ||
			ValidateAttachmentID(expiredUpload.AttachmentID) != nil ||
			expiredUpload.State != UploadStateExpired {
			return true, ErrAttachmentConflict
		}
		return true, nil
	}
	expiredJob, err := worker.repository.ExpireBoundedProcessorJob(ctx, ProcessorExpiryInput{
		ProjectID: "default", OwnerID: worker.config.OwnerID, Limits: worker.config.Limits,
	})
	if err != nil {
		return false, err
	}
	if expiredJob != nil {
		return true, nil
	}
	claim, err := worker.repository.ClaimProcessorJob(ctx, ProcessorClaimInput{
		OwnerID: worker.config.OwnerID, OwnerLeaseDuration: worker.config.OwnerLeaseDuration,
	})
	if err != nil {
		return false, err
	}
	if claim == nil {
		return false, nil
	}
	if claim.Validate() != nil {
		return true, ErrInvalidProcessorCommand
	}
	if err := worker.hitCutpoint(ProcessorWorkerCutpointAfterClaim); err != nil {
		return true, err
	}

	// Renew before opening the source. This bounds the processing window to a
	// lease owned by this worker and lets the repository reject a lost claim.
	renewed, err := worker.repository.RenewProcessorClaim(ctx, ProcessorRenewInput{
		Claim: *claim, OwnerLeaseDuration: worker.config.OwnerLeaseDuration,
	})
	if err != nil {
		return true, err
	}
	if renewed.Source != claim.Source || renewed.ProcessorJobID != claim.ProcessorJobID || renewed.OwnerGeneration != claim.OwnerGeneration {
		return true, ErrProcessorClaimLost
	}
	*claim = renewed

	result, previewPublicationIntent, processErr := worker.processClaim(ctx, *claim)
	if ctxErr := ctx.Err(); ctxErr != nil {
		// A shutdown must never be turned into a successful completion.
		return true, ctxErr
	}
	if processErr != nil {
		if result.Code == "" {
			result = ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: processorResultCode(processErr)}
		}
	}
	if result.Validate() != nil {
		result = ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: ProcessorResultCodeProcessingError}
	}
	completion := ProcessorCompletionInput{
		Claim: *claim, Result: result, Limits: worker.config.Limits,
		PreviewPublicationIntent: previewPublicationIntent,
	}
	if retryableProcessorResultCode(result.Code) {
		completion.RetryAt = worker.config.Now().Add(worker.config.Backoff(claim.Attempt))
	}
	if _, err = worker.repository.CompleteProcessorJob(ctx, completion); err != nil {
		return true, err
	}
	if err := worker.hitCutpoint(ProcessorWorkerCutpointAfterResultCommit); err != nil {
		return true, err
	}
	return true, nil
}

func (worker *ProcessorWorker) hitCutpoint(cutpoint ProcessorWorkerCutpoint) error {
	if worker == nil || worker.config.Cutpoint == nil {
		return nil
	}
	return worker.config.Cutpoint(cutpoint)
}

func (worker *ProcessorWorker) processClaim(
	ctx context.Context,
	claim ProcessorClaim,
) (ProcessorResult, BlobPublicationIntent, error) {
	processCtx, cancel := context.WithDeadline(ctx, claim.LeaseExpiresAt)
	defer cancel()
	if err := processCtx.Err(); err != nil {
		return ProcessorResult{}, BlobPublicationIntent{}, err
	}
	if claim.Source.BackendKind != worker.config.PreviewBackendKind {
		return ProcessorResult{}, BlobPublicationIntent{}, ErrInvalidProcessorCommand
	}
	admission, err := worker.admitClaim(processCtx, claim)
	if err != nil {
		return ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: processorResultCode(err)}, BlobPublicationIntent{}, err
	}
	if admission.Profile != claim.Profile {
		return ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: ProcessorResultCodeUnsafeContent}, BlobPublicationIntent{}, ErrProcessorAdmissionMismatch
	}
	workspaceID, err := worker.config.NewWorkspaceID(claim)
	if err != nil || ValidateWorkspaceID(workspaceID) != nil {
		return ProcessorResult{}, BlobPublicationIntent{}, ErrInvalidProcessorCommand
	}
	if claim.Profile == ProcessorProfileArchive {
		if worker.config.Scan == nil {
			return ProcessorResult{}, BlobPublicationIntent{}, ErrArchiveScannerUnavailable
		}
		reader, err := worker.openSource(processCtx, claim)
		if err != nil {
			return ProcessorResult{}, BlobPublicationIntent{}, err
		}
		code, scanErr := worker.config.Scan(processCtx, reader)
		closeErr := reader.Close()
		if scanErr == nil && closeErr != nil {
			return ProcessorResult{}, BlobPublicationIntent{}, closeErr
		}
		if scanErr != nil {
			if code == "" {
				code = processorResultCode(scanErr)
			}
			return ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: code}, BlobPublicationIntent{}, scanErr
		}
		if code == "" {
			return ProcessorResult{}, BlobPublicationIntent{}, ErrInvalidProcessorResult
		}
		if code != ProcessorResultCodeClean {
			return ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: code}, BlobPublicationIntent{}, nil
		}
	}
	reader, err := worker.openSource(processCtx, claim)
	if err != nil {
		return ProcessorResult{}, BlobPublicationIntent{}, err
	}
	artifact, receipt, processErr := worker.workspace.Process(processCtx, ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: workspaceID, ExpiresAt: claim.LeaseExpiresAt, Source: reader,
	})
	closeErr := reader.Close()
	if processErr == nil && closeErr != nil {
		processErr = closeErr
	}
	if processErr != nil {
		return ProcessorResult{}, BlobPublicationIntent{}, processErr
	}
	if receipt.Validate() != nil {
		return ProcessorResult{}, BlobPublicationIntent{}, ErrInvalidProcessorResult
	}
	result := ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: ProcessorResultCodeClean, HasPreview: artifact.HasPreview}
	if artifact.HasPreview {
		if len(artifact.Bytes) == 0 {
			return ProcessorResult{}, BlobPublicationIntent{}, ErrInvalidProcessorResult
		}
		digest := sha256.Sum256(artifact.Bytes)
		temporaryDigest := sha256.Sum256(append([]byte(claim.ProcessorJobID+":"), digest[:]...))
		publicationIntent, err := worker.preparePreviewPublication(processCtx, claim, digest, int64(len(artifact.Bytes)))
		if err != nil {
			return ProcessorResult{}, BlobPublicationIntent{}, err
		}
		version, err := worker.blob.Put(processCtx, PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(artifact.Bytes)), TemporaryKey: "temporary/" + hexDigest(temporaryDigest)}, bytes.NewReader(artifact.Bytes))
		if err != nil {
			return ProcessorResult{}, BlobPublicationIntent{}, err
		}
		if version.Validate() != nil || version.SHA256 != digest || version.SizeBytes != int64(len(artifact.Bytes)) {
			return ProcessorResult{}, BlobPublicationIntent{}, ErrInvalidProcessorResult
		}
		publicationIntent, err = worker.recordPreviewPublicationVersion(processCtx, publicationIntent, version)
		if err != nil {
			return ProcessorResult{}, BlobPublicationIntent{}, err
		}
		result.Preview = ManagedPreview{Blob: BlobObject{Key: version.Key, ObjectVersion: version.VersionID, SHA256: version.SHA256, SizeBytes: version.SizeBytes, BackendKind: worker.config.PreviewBackendKind}, MediaType: artifact.MediaType}
		return result, publicationIntent, nil
	}
	return result, BlobPublicationIntent{}, nil
}

func (worker *ProcessorWorker) preparePreviewPublication(
	ctx context.Context,
	claim ProcessorClaim,
	digest [sha256.Size]byte,
	sizeBytes int64,
) (BlobPublicationIntent, error) {
	target := BlobPublicationTarget{
		Key: "sha256/" + hexDigest(digest), SHA256: digest,
		SizeBytes: sizeBytes, BackendKind: worker.config.PreviewBackendKind,
	}
	request := BlobPublicationPrepareRequest{
		ProjectID: claim.ProjectID, OwnerKind: BlobPublicationOwnerProcessorPreview,
		OwnerID: claim.ProcessorJobID, OwnerGeneration: claim.OwnerGeneration,
		Target: target, PublishExpiresAt: claim.ExpiresAt.UTC(),
	}
	intent, err := worker.repository.PrepareBlobPublication(ctx, request)
	if err != nil {
		return BlobPublicationIntent{}, err
	}
	if intent.Validate() != nil || intent.ProjectID != request.ProjectID ||
		intent.OwnerKind != request.OwnerKind || intent.OwnerID != request.OwnerID ||
		intent.OwnerGeneration != request.OwnerGeneration || intent.Target != target ||
		!intent.PublishExpiresAt.Equal(request.PublishExpiresAt) ||
		(intent.State != BlobPublicationStatePrepared && intent.State != BlobPublicationStatePublished) {
		return BlobPublicationIntent{}, ErrAttachmentConflict
	}
	return intent, nil
}

func (worker *ProcessorWorker) recordPreviewPublicationVersion(
	ctx context.Context,
	intent BlobPublicationIntent,
	object ObjectVersion,
) (BlobPublicationIntent, error) {
	if intent.State == BlobPublicationStatePublished {
		if intent.ObjectVersion != object.VersionID || !blobPublicationObjectMatchesTarget(object, intent.Target) {
			return BlobPublicationIntent{}, ErrAttachmentConflict
		}
		return intent, nil
	}
	if intent.State != BlobPublicationStatePrepared || !blobPublicationObjectMatchesTarget(object, intent.Target) {
		return BlobPublicationIntent{}, ErrAttachmentConflict
	}
	published, err := worker.repository.RecordBlobPublicationVersion(ctx, BlobPublicationVersionRequest{
		Intent: intent, Object: object,
	})
	if err != nil {
		return BlobPublicationIntent{}, err
	}
	if published.Validate() != nil || published.State != BlobPublicationStatePublished ||
		published.PublicationID != intent.PublicationID || published.ProjectID != intent.ProjectID ||
		published.OwnerKind != intent.OwnerKind || published.OwnerID != intent.OwnerID ||
		published.OwnerGeneration != intent.OwnerGeneration || published.Target != intent.Target ||
		published.ObjectVersion != object.VersionID || !published.PublishExpiresAt.Equal(intent.PublishExpiresAt) {
		return BlobPublicationIntent{}, ErrAttachmentConflict
	}
	return published, nil
}

func (worker *ProcessorWorker) admitClaim(ctx context.Context, claim ProcessorClaim) (AdmissionResult, error) {
	if !validAdmissionDisplayName(claim.DisplayName) || claim.DeclaredMediaType == "" || len(claim.DeclaredMediaType) > 255 {
		return AdmissionResult{}, ErrInvalidProcessorCommand
	}
	reader, err := worker.openSource(ctx, claim)
	if err != nil {
		return AdmissionResult{}, err
	}
	status := ScannerStatusUnconfigured
	if worker.config.Scan != nil {
		status = ScannerStatusHealthy
	}
	result, admissionErr := AdmitContent(ctx, AdmissionRequest{
		DisplayName: claim.DisplayName, DeclaredMediaType: claim.DeclaredMediaType,
		SizeBytes: claim.Source.SizeBytes, Content: reader, ScannerStatus: status,
	}, worker.config.AdmissionLimits)
	closeErr := reader.Close()
	if admissionErr == nil && closeErr != nil {
		admissionErr = closeErr
	}
	return result, admissionErr
}

func (worker *ProcessorWorker) openSource(ctx context.Context, claim ProcessorClaim) (io.ReadCloser, error) {
	return worker.blob.Open(ctx, ObjectVersion{Key: claim.Source.Key, VersionID: claim.Source.ObjectVersion, SHA256: claim.Source.SHA256, SizeBytes: claim.Source.SizeBytes}, FullByteRange())
}

func processorResultCode(err error) ProcessorResultCode {
	switch {
	case err == nil:
		return ProcessorResultCodeClean
	case errors.Is(err, ErrArchiveScannerUnavailable), errors.Is(err, ErrClamAVScannerDaemon):
		return ProcessorResultCodeScannerUnavailable
	case errors.Is(err, ErrClamAVScannerTimeout), errors.Is(err, context.DeadlineExceeded):
		return ProcessorResultCodeTimeout
	case errors.Is(err, ErrAdmissionRejected), errors.Is(err, ErrAdmissionLimitExceeded), errors.Is(err, ErrInvalidPreviewContent), errors.Is(err, ErrPreviewLimitExceeded):
		return ProcessorResultCodeUnsafeContent
	case errors.Is(err, ErrProcessorAdmissionMismatch):
		return ProcessorResultCodeUnsafeContent
	default:
		return ProcessorResultCodeProcessingError
	}
}

func defaultProcessorWorkspaceID(claim ProcessorClaim) (string, error) {
	digest := sha256.Sum256([]byte(claim.ProcessorJobID + ":" + strconv.FormatInt(claim.Attempt, 10)))
	return "cpw_" + hexDigest(digest)[:32], nil
}

func defaultProcessorWorkerBackoff(attempt int64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

func sleepProcessorWorker(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

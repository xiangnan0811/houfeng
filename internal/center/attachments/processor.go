package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidProcessorResult     = errors.New("invalid processor result")
	ErrInvalidProcessorCommand    = errors.New("invalid processor command")
	ErrProcessorClaimLost         = errors.New("attachment processor claim lost")
	ErrProcessorAdmissionMismatch = errors.New("processor admission profile mismatch")
)

const (
	processorResultCanonicalDomainV1 = "houfeng.attachments.processor-result.v1"
	maxManagedPreviewMediaTypeLength = 255
	maxProcessorOwnerLeaseDuration   = 24 * time.Hour
)

type ProcessorResultCode string

const (
	ProcessorResultCodeClean              ProcessorResultCode = "clean"
	ProcessorResultCodeMalware            ProcessorResultCode = "malware"
	ProcessorResultCodeUnsafeContent      ProcessorResultCode = "unsafe_content"
	ProcessorResultCodeScannerUnavailable ProcessorResultCode = "scanner_unavailable"
	ProcessorResultCodeTimeout            ProcessorResultCode = "timeout"
	ProcessorResultCodeProcessingError    ProcessorResultCode = "processing_error"
)

type ProcessorState string

const (
	ProcessorStateQueued    ProcessorState = "queued"
	ProcessorStateClaimed   ProcessorState = "claimed"
	ProcessorStateRetryWait ProcessorState = "retry_wait"
	ProcessorStateSucceeded ProcessorState = "succeeded"
	ProcessorStateRejected  ProcessorState = "rejected"
	ProcessorStateExpired   ProcessorState = "expired"
)

type ProcessorClaimInput struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
}

func (input ProcessorClaimInput) Validate() error {
	if !validProcessorOwnerID(input.OwnerID) ||
		input.OwnerLeaseDuration < time.Microsecond ||
		input.OwnerLeaseDuration > maxProcessorOwnerLeaseDuration {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorClaim struct {
	ProjectID         string
	ProcessorJobID    string
	UploadID          string
	AttachmentID      string
	DisplayName       string
	DeclaredMediaType string
	Source            BlobObject
	Profile           ProcessorProfile
	Attempt           int64
	MaxAttempts       int64
	OwnerID           string
	OwnerGeneration   int64
	LeaseExpiresAt    time.Time
	ExpiresAt         time.Time
}

func (claim ProcessorClaim) Validate() error {
	if claim.ProjectID != "default" || ValidateProcessorJobID(claim.ProcessorJobID) != nil ||
		ValidateUploadID(claim.UploadID) != nil || ValidateAttachmentID(claim.AttachmentID) != nil ||
		claim.Source.Validate() != nil || !knownProcessorProfile(claim.Profile) ||
		claim.Attempt <= 0 || claim.MaxAttempts <= 0 || claim.Attempt > claim.MaxAttempts ||
		!validProcessorOwnerID(claim.OwnerID) || claim.OwnerGeneration <= 0 ||
		claim.LeaseExpiresAt.IsZero() || claim.ExpiresAt.IsZero() ||
		claim.LeaseExpiresAt.After(claim.ExpiresAt) {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorRenewInput struct {
	Claim              ProcessorClaim
	OwnerLeaseDuration time.Duration
}

func (input ProcessorRenewInput) Validate() error {
	if input.Claim.Validate() != nil || (ProcessorClaimInput{
		OwnerID: input.Claim.OwnerID, OwnerLeaseDuration: input.OwnerLeaseDuration,
	}).Validate() != nil {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorWorkspaceState string

const (
	ProcessorWorkspaceStateRegistered   ProcessorWorkspaceState = "registered"
	ProcessorWorkspaceStateMaterialized ProcessorWorkspaceState = "materialized"
	ProcessorWorkspaceStatePurging      ProcessorWorkspaceState = "purging"
	ProcessorWorkspaceStatePurged       ProcessorWorkspaceState = "purged"
)

type ProcessorWorkspace struct {
	WorkspaceID         string
	ProcessorJobID      string
	Attempt             int64
	State               ProcessorWorkspaceState
	WorkspacePathDigest [sha256.Size]byte
	ExpiresAt           time.Time
}

func (workspace ProcessorWorkspace) Validate() error {
	if ValidateWorkspaceID(workspace.WorkspaceID) != nil ||
		ValidateProcessorJobID(workspace.ProcessorJobID) != nil || workspace.Attempt <= 0 ||
		!knownProcessorWorkspaceState(workspace.State) ||
		workspace.WorkspacePathDigest == [sha256.Size]byte{} || workspace.ExpiresAt.IsZero() {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorWorkspaceRegistration struct {
	Claim               ProcessorClaim
	WorkspaceID         string
	WorkspacePathDigest [sha256.Size]byte
	ExpiresAt           time.Time
}

func (registration ProcessorWorkspaceRegistration) Validate() error {
	if registration.Claim.Validate() != nil || ValidateWorkspaceID(registration.WorkspaceID) != nil ||
		registration.WorkspacePathDigest == [sha256.Size]byte{} || registration.ExpiresAt.IsZero() ||
		registration.ExpiresAt.After(registration.Claim.ExpiresAt) {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorCompletionInput struct {
	Claim                    ProcessorClaim
	Result                   ProcessorResult
	RetryAt                  time.Time
	Limits                   Limits
	PreviewPublicationIntent BlobPublicationIntent
}

func (input ProcessorCompletionInput) Validate() error {
	if input.Claim.Validate() != nil {
		return ErrInvalidProcessorCommand
	}
	if err := ValidateProcessorResultForCompletion(input.Result, input.Limits); err != nil {
		return err
	}
	retryable := retryableProcessorResultCode(input.Result.Code)
	if retryable == input.RetryAt.IsZero() {
		return ErrInvalidProcessorCommand
	}
	if input.PreviewPublicationIntent != (BlobPublicationIntent{}) {
		intent := input.PreviewPublicationIntent
		if !input.Result.HasPreview || intent.Validate() != nil ||
			intent.State != BlobPublicationStatePublished || intent.ProjectID != input.Claim.ProjectID ||
			intent.OwnerKind != BlobPublicationOwnerProcessorPreview ||
			intent.OwnerID != input.Claim.ProcessorJobID ||
			intent.OwnerGeneration != input.Claim.OwnerGeneration {
			return ErrInvalidProcessorCommand
		}
		object, ok := intent.Object()
		if !ok || object != input.Result.Preview.Blob {
			return ErrInvalidProcessorCommand
		}
	}
	return nil
}

// ValidateProcessorResultForCompletion applies the processor-result contract
// that must hold before a completion can become durable. It is exported so
// repository implementations can repeat the boundary check immediately before
// opening/writing a transaction, including terminal replay paths.
func ValidateProcessorResultForCompletion(result ProcessorResult, limits Limits) error {
	if limits.Validate() != nil {
		return ErrInvalidProcessorCommand
	}
	if err := result.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidProcessorCommand, err)
	}
	if err := validateManagedPreviewWithinLimits(result, limits); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidProcessorCommand, err)
	}
	return nil
}

type ProcessorCompletionResult struct {
	ProjectID       string
	ProcessorJobID  string
	UploadID        string
	AttachmentID    string
	ProcessorState  ProcessorState
	UploadState     UploadState
	AttachmentState UploadState
	ResultCode      ProcessorResultCode
	ResultDigest    [sha256.Size]byte
	Quota           QuotaSnapshot
}

type ProcessorExpiryInput struct {
	ProjectID string
	OwnerID   string
	Limits    Limits
}

func (input ProcessorExpiryInput) Validate() error {
	if input.ProjectID != "default" || !validProcessorOwnerID(input.OwnerID) ||
		input.Limits.Validate() != nil {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type AbandonedUploadExpiryInput struct {
	ProjectID string
	Limits    Limits
}

func (input AbandonedUploadExpiryInput) Validate() error {
	if input.ProjectID != "default" || input.Limits.Validate() != nil {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorRepository interface {
	BlobPublicationRepository
	ProcessorWorkspaceRepository
	ExpireAbandonedUpload(context.Context, AbandonedUploadExpiryInput) (*UploadMutationResult, error)
	ClaimProcessorJob(context.Context, ProcessorClaimInput) (*ProcessorClaim, error)
	RenewProcessorClaim(context.Context, ProcessorRenewInput) (ProcessorClaim, error)
	CompleteProcessorJob(context.Context, ProcessorCompletionInput) (ProcessorCompletionResult, error)
	ExpireBoundedProcessorJob(context.Context, ProcessorExpiryInput) (*ProcessorCompletionResult, error)
}

const (
	ManagedPreviewMediaTypePNG      = "image/png"
	ManagedPreviewMediaTypeTextUTF8 = "text/plain; charset=utf-8"
)

type ManagedPreview struct {
	Blob      BlobObject
	MediaType string
}

func (preview ManagedPreview) Validate() error {
	if preview.Blob.Validate() != nil || len(preview.MediaType) == 0 ||
		len(preview.MediaType) > maxManagedPreviewMediaTypeLength ||
		(preview.MediaType != ManagedPreviewMediaTypePNG &&
			preview.MediaType != ManagedPreviewMediaTypeTextUTF8) {
		return ErrInvalidProcessorResult
	}
	return nil
}

type ProcessorResult struct {
	Source     BlobObject
	Profile    ProcessorProfile
	Code       ProcessorResultCode
	HasPreview bool
	Preview    ManagedPreview
}

func (result ProcessorResult) Validate() error {
	if result.Source.Validate() != nil || !knownProcessorProfile(result.Profile) ||
		!knownProcessorResultCode(result.Code) {
		return ErrInvalidProcessorResult
	}
	if !result.HasPreview {
		if result.Preview != (ManagedPreview{}) ||
			(result.Code == ProcessorResultCodeClean && result.Profile != ProcessorProfileArchive) {
			return ErrInvalidProcessorResult
		}
		return nil
	}
	if result.Code != ProcessorResultCodeClean || result.Preview.Validate() != nil {
		return ErrInvalidProcessorResult
	}
	wantMediaType, ok := managedPreviewMediaTypeForProfile(result.Profile)
	if !ok || result.Preview.MediaType != wantMediaType {
		return ErrInvalidProcessorResult
	}
	return nil
}

// validateManagedPreviewWithinLimits is the durable completion boundary for
// processor-produced previews. Preview producers have their own bounded
// rendering configuration, but a processor result may be fabricated or come
// from an older worker; durable writes must therefore enforce the profile,
// media-type, and byte-size contract again at this boundary.
func validateManagedPreviewWithinLimits(result ProcessorResult, limits Limits) error {
	if !result.HasPreview {
		return nil
	}
	expectedMediaType, ok := managedPreviewMediaTypeForProfile(result.Profile)
	if !ok || result.Preview.MediaType != expectedMediaType {
		return ErrInvalidProcessorResult
	}
	limit, ok := managedPreviewByteLimit(result.Profile, limits)
	if !ok || result.Preview.Blob.SizeBytes > limit {
		return ErrInvalidProcessorResult
	}
	return nil
}

// managedPreviewByteLimit centralizes the durable upper bounds. Text previews
// use their dedicated inline-text allowance; non-text previews derive their
// bound from the existing attachment quota limits rather than a second magic
// constant. The minimum keeps this safe if the Limits ordering is ever relaxed.
func managedPreviewByteLimit(profile ProcessorProfile, limits Limits) (int64, bool) {
	switch profile {
	case ProcessorProfileText:
		return limits.MaxInlineTextPreviewBytes, true
	case ProcessorProfileImage, ProcessorProfilePDF:
		return maxNonTextManagedPreviewBytes(limits), true
	default:
		return 0, false
	}
}

func maxNonTextManagedPreviewBytes(limits Limits) int64 {
	limit := limits.MaxFileBytes
	if limits.MaxRecordBytes < limit {
		limit = limits.MaxRecordBytes
	}
	if limits.MaxProjectBytes < limit {
		limit = limits.MaxProjectBytes
	}
	return limit
}

func (result ProcessorResult) Digest() ([sha256.Size]byte, error) {
	if err := result.Validate(); err != nil {
		return [sha256.Size]byte{}, err
	}
	return processorResultCanonicalDigest(result), nil
}

func knownProcessorResultCode(code ProcessorResultCode) bool {
	switch code {
	case ProcessorResultCodeClean, ProcessorResultCodeMalware,
		ProcessorResultCodeUnsafeContent, ProcessorResultCodeScannerUnavailable,
		ProcessorResultCodeTimeout, ProcessorResultCodeProcessingError:
		return true
	default:
		return false
	}
}

func retryableProcessorResultCode(code ProcessorResultCode) bool {
	return code == ProcessorResultCodeScannerUnavailable || code == ProcessorResultCodeTimeout ||
		code == ProcessorResultCodeProcessingError
}

func knownProcessorState(state ProcessorState) bool {
	switch state {
	case ProcessorStateQueued, ProcessorStateClaimed, ProcessorStateRetryWait,
		ProcessorStateSucceeded, ProcessorStateRejected, ProcessorStateExpired:
		return true
	default:
		return false
	}
}

func knownProcessorWorkspaceState(state ProcessorWorkspaceState) bool {
	switch state {
	case ProcessorWorkspaceStateRegistered, ProcessorWorkspaceStateMaterialized,
		ProcessorWorkspaceStatePurging, ProcessorWorkspaceStatePurged:
		return true
	default:
		return false
	}
}

func validProcessorOwnerID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func managedPreviewMediaTypeForProfile(profile ProcessorProfile) (string, bool) {
	switch profile {
	case ProcessorProfileImage, ProcessorProfilePDF:
		return ManagedPreviewMediaTypePNG, true
	case ProcessorProfileText:
		return ManagedPreviewMediaTypeTextUTF8, true
	default:
		return "", false
	}
}

func processorResultCanonicalDigest(result ProcessorResult) [sha256.Size]byte {
	// Strings are length-prefixed; SHA-256 values keep their fixed width.
	encoder := processorResultCanonicalEncoder{}
	encoder.string(processorResultCanonicalDomainV1)
	encoder.blob(result.Source)
	encoder.string(string(result.Profile))
	encoder.string(string(result.Code))
	encoder.boolean(result.HasPreview)
	if result.HasPreview {
		encoder.blob(result.Preview.Blob)
		encoder.string(result.Preview.MediaType)
	}
	return sha256.Sum256(encoder.bytes)
}

type processorResultCanonicalEncoder struct {
	bytes []byte
}

func (encoder *processorResultCanonicalEncoder) boolean(value bool) {
	if value {
		encoder.bytes = append(encoder.bytes, 1)
		return
	}
	encoder.bytes = append(encoder.bytes, 0)
}

func (encoder *processorResultCanonicalEncoder) uint64(value uint64) {
	encoder.bytes = append(encoder.bytes,
		byte(value>>56), byte(value>>48), byte(value>>40), byte(value>>32),
		byte(value>>24), byte(value>>16), byte(value>>8), byte(value),
	)
}

func (encoder *processorResultCanonicalEncoder) string(value string) {
	length := uint32(len(value))
	encoder.bytes = append(encoder.bytes,
		byte(length>>24), byte(length>>16), byte(length>>8), byte(length),
	)
	encoder.bytes = append(encoder.bytes, value...)
}

func (encoder *processorResultCanonicalEncoder) blob(object BlobObject) {
	encoder.string(object.Key)
	encoder.string(object.ObjectVersion)
	encoder.bytes = append(encoder.bytes, object.SHA256[:]...)
	encoder.uint64(uint64(object.SizeBytes))
	encoder.string(string(object.BackendKind))
}

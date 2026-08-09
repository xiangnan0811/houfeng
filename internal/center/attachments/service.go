package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidUploadServiceRequest = errors.New("invalid attachment upload service request")
	ErrUploadExpired               = errors.New("attachment upload expired")
)

type ProcessorProfile string

const (
	ProcessorProfileImage   ProcessorProfile = "image"
	ProcessorProfilePDF     ProcessorProfile = "pdf"
	ProcessorProfileText    ProcessorProfile = "text"
	ProcessorProfileArchive ProcessorProfile = "archive"
)

type UploadTarget struct {
	TransportKind      TransportKind
	UploadID           string
	TemporaryObjectKey string
	UploadURL          string
	Method             string
	RequiredHeaders    []string
}

type CreateUploadRequest struct {
	Actor             recordauth.ActorScope
	UploadID          string
	AttachmentID      string
	DraftID           string
	DisplayName       string
	MediaType         string
	DeclaredSizeBytes int64
	ExpiresAt         time.Time
}

type CreateUploadResult struct {
	UploadID     string
	AttachmentID string
	State        UploadState
	Quota        QuotaSnapshot
	Target       UploadTarget
}

type PutUploadContentRequest struct {
	Actor          recordauth.ActorScope
	DraftID        string
	UploadID       string
	ExpectedSHA256 [sha256.Size]byte
	Content        io.Reader
}

type PutUploadContentResult struct {
	UploadID     string
	AttachmentID string
	Object       ObjectVersion
}

type CompleteUploadRequest struct {
	Actor    recordauth.ActorScope
	DraftID  string
	UploadID string
}

type PrepareUploadCommand struct {
	ProjectID                   string
	UploadID                    string
	AuthorID                    string
	CandidateTemporaryObjectKey string
}

func (command PrepareUploadCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || (command.CandidateTemporaryObjectKey != "" &&
		!validS3BlobTemporaryKey(command.CandidateTemporaryObjectKey)) {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type UploadPreparation struct {
	ProjectID              string
	UploadID               string
	AttachmentID           string
	DraftID                string
	AuthorID               string
	State                  UploadState
	TransportKind          TransportKind
	DeclaredSizeBytes      int64
	MediaType              string
	ExpiresAt              time.Time
	TemporaryObjectKey     string
	TemporaryObjectVersion string
}

type RecordUploadedContentCommand struct {
	ProjectID          string
	UploadID           string
	AuthorID           string
	TemporaryObjectKey string
	Object             ObjectVersion
	PublicationIntent  BlobPublicationIntent
}

func (command RecordUploadedContentCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || command.Object.Validate() != nil ||
		(command.TemporaryObjectKey != "" && !validS3BlobTemporaryKey(command.TemporaryObjectKey)) {
		return ErrInvalidAttachmentCommand
	}
	if !blobPublicationIntentMatchesUploadObject(
		command.PublicationIntent,
		command.ProjectID,
		command.UploadID,
		command.Object,
	) {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type RecordTemporaryObjectVersionCommand struct {
	ProjectID              string
	UploadID               string
	AuthorID               string
	TemporaryObjectKey     string
	TemporaryObjectVersion string
}

func (command RecordTemporaryObjectVersionCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || !validS3BlobTemporaryKey(command.TemporaryObjectKey) ||
		command.TemporaryObjectVersion == "" || len(command.TemporaryObjectVersion) > 1024 {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type UploadedContent struct {
	ProjectID              string
	UploadID               string
	AttachmentID           string
	DraftID                string
	AuthorID               string
	State                  UploadState
	TransportKind          TransportKind
	MediaType              string
	ExpiresAt              time.Time
	TemporaryObjectKey     string
	TemporaryObjectVersion string
	Object                 ObjectVersion
}

type UploadCompletionPreparation struct {
	UploadPreparation
	Object            ObjectVersion
	HasObject         bool
	PublicationIntent BlobPublicationIntent
}

type CompleteUploadAndEnqueueCommand struct {
	ProjectID              string
	UploadID               string
	AuthorID               string
	ActualSizeBytes        int64
	ActualSHA256           [sha256.Size]byte
	TemporaryObjectKey     string
	TemporaryObjectVersion string
	Object                 ObjectVersion
	PublicationIntent      BlobPublicationIntent
	CompletionFingerprint  [sha256.Size]byte
	ProcessorJobID         string
	ProcessorProfile       ProcessorProfile
	ProcessorMaxAttempts   int64
	ProcessorExpiresAt     time.Time
}

func (command CompleteUploadAndEnqueueCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || command.Object.Validate() != nil || command.ActualSizeBytes != command.Object.SizeBytes ||
		command.ActualSHA256 != command.Object.SHA256 || command.CompletionFingerprint == [sha256.Size]byte{} ||
		(command.TemporaryObjectVersion != "" && command.TemporaryObjectKey == "") ||
		(command.TemporaryObjectKey != "" && !validS3BlobTemporaryKey(command.TemporaryObjectKey)) ||
		len(command.TemporaryObjectVersion) > 1024 || ValidateProcessorJobID(command.ProcessorJobID) != nil ||
		!knownProcessorProfile(command.ProcessorProfile) || command.ProcessorMaxAttempts <= 0 ||
		command.ProcessorExpiresAt.IsZero() {
		return ErrInvalidAttachmentCommand
	}
	if command.PublicationIntent != (BlobPublicationIntent{}) &&
		!blobPublicationIntentMatchesUploadObject(command.PublicationIntent, command.ProjectID, command.UploadID, command.Object) {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

func blobPublicationIntentMatchesUploadObject(
	intent BlobPublicationIntent,
	projectID string,
	uploadID string,
	object ObjectVersion,
) bool {
	if intent.Validate() != nil || intent.State != BlobPublicationStatePublished ||
		intent.ProjectID != projectID || intent.OwnerKind != BlobPublicationOwnerUpload ||
		intent.OwnerID != uploadID || intent.ObjectVersion != object.VersionID {
		return false
	}
	return blobPublicationObjectMatchesTarget(object, intent.Target)
}

type DraftAttachmentUploadAuthorizer interface {
	AuthorizeDraftAttachmentUpload(context.Context, recordauth.ActorScope, string) error
}

type UploadServiceRepository interface {
	BlobPublicationRepository
	ReserveUpload(context.Context, ReserveUploadCommand) (UploadReservationResult, error)
	PrepareUpload(context.Context, PrepareUploadCommand) (UploadPreparation, error)
	RecordTemporaryObjectVersion(context.Context, RecordTemporaryObjectVersionCommand) (UploadPreparation, error)
	RecordUploadedContent(context.Context, RecordUploadedContentCommand) (UploadedContent, error)
	GetUploadedContent(context.Context, UploadMutationCommand) (UploadedContent, error)
	GetUploadCompletionPreparation(context.Context, UploadMutationCommand) (UploadCompletionPreparation, error)
	CompleteUploadAndEnqueue(context.Context, CompleteUploadAndEnqueueCommand) (UploadMutationResult, error)
}

type ArchiveScannerReadiness func(context.Context) (ScannerStatus, error)

type UploadServiceOptions struct {
	TransportKind           TransportKind
	Limits                  Limits
	Now                     func() time.Time
	ArchiveScannerReadiness ArchiveScannerReadiness
	NewTemporaryObjectKey   func() (string, error)
	ResolveProcessorProfile func(string) (ProcessorProfile, error)
	ProcessorMaxAttempts    int64
}

type UploadService struct {
	authorizer              DraftAttachmentUploadAuthorizer
	repository              UploadServiceRepository
	blob                    BlobStore
	temporary               TemporaryObjectStore
	presigner               TemporaryUploadPresigner
	transport               TransportKind
	limits                  Limits
	now                     func() time.Time
	archiveScannerReadiness ArchiveScannerReadiness
	newTemporaryObjectKey   func() (string, error)
	resolveProcessorProfile func(string) (ProcessorProfile, error)
	processorMaxAttempts    int64
}

func NewUploadService(
	authorizer DraftAttachmentUploadAuthorizer,
	repository UploadServiceRepository,
	blob BlobStore,
	options UploadServiceOptions,
) (*UploadService, error) {
	if nilUploadServiceDependency(authorizer) || nilUploadServiceDependency(repository) ||
		nilUploadServiceDependency(blob) ||
		(options.TransportKind != TransportKindLocal && options.TransportKind != TransportKindS3) ||
		options.Limits.Validate() != nil || options.ProcessorMaxAttempts <= 0 {
		return nil, fmt.Errorf("%w: dependency or options", ErrInvalidUploadServiceRequest)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewTemporaryObjectKey == nil {
		options.NewTemporaryObjectKey = newS3BlobTemporaryKey
	}
	if options.ResolveProcessorProfile == nil {
		options.ResolveProcessorProfile = processorProfileForMediaTypeHint
	}
	var temporary TemporaryObjectStore
	var presigner TemporaryUploadPresigner
	if options.TransportKind == TransportKindS3 {
		var temporaryOK, presignerOK bool
		temporary, temporaryOK = blob.(TemporaryObjectStore)
		presigner, presignerOK = blob.(TemporaryUploadPresigner)
		if !temporaryOK || nilUploadServiceDependency(temporary) {
			return nil, fmt.Errorf("%w: S3 temporary object store", ErrInvalidUploadServiceRequest)
		}
		if !presignerOK || nilUploadServiceDependency(presigner) {
			return nil, fmt.Errorf("%w: S3 temporary upload presigner", ErrInvalidUploadServiceRequest)
		}
	}
	return &UploadService{
		authorizer: authorizer, repository: repository, blob: blob, temporary: temporary, presigner: presigner,
		transport: options.TransportKind, limits: options.Limits, now: options.Now,
		archiveScannerReadiness: options.ArchiveScannerReadiness,
		newTemporaryObjectKey:   options.NewTemporaryObjectKey,
		resolveProcessorProfile: options.ResolveProcessorProfile,
		processorMaxAttempts:    options.ProcessorMaxAttempts,
	}, nil
}

func (service *UploadService) CreateUpload(
	ctx context.Context,
	request CreateUploadRequest,
) (CreateUploadResult, error) {
	if err := service.validateActorRequest(ctx, request.Actor, request.DraftID, request.UploadID); err != nil {
		return CreateUploadResult{}, err
	}
	command := ReserveUploadCommand{
		ProjectID: string(request.Actor.ProjectID), UploadID: request.UploadID,
		AttachmentID: request.AttachmentID, DraftID: request.DraftID, AuthorID: request.Actor.UserID,
		DisplayName: request.DisplayName, MediaType: request.MediaType, TransportKind: service.transport,
		DeclaredSizeBytes: request.DeclaredSizeBytes, ExpiresAt: request.ExpiresAt.UTC(), Limits: service.limits,
	}
	if command.Validate() != nil || !request.ExpiresAt.After(service.now()) {
		return CreateUploadResult{}, ErrInvalidUploadServiceRequest
	}
	if archiveUploadDeclaration(request.DisplayName, request.MediaType) {
		if err := service.requireArchiveScanner(ctx); err != nil {
			return CreateUploadResult{}, err
		}
	}
	if err := service.authorizer.AuthorizeDraftAttachmentUpload(ctx, request.Actor.Clone(), request.DraftID); err != nil {
		return CreateUploadResult{}, err
	}
	reservation, err := service.repository.ReserveUpload(ctx, command)
	if err != nil {
		return CreateUploadResult{}, err
	}
	reservationStateValid := reservation.State == UploadStateCreated ||
		(service.transport == TransportKindS3 && reservation.State == UploadStateUploading)
	if reservation.UploadID != request.UploadID || reservation.AttachmentID != request.AttachmentID ||
		!reservationStateValid {
		return CreateUploadResult{}, ErrAttachmentConflict
	}
	result := CreateUploadResult{
		UploadID: reservation.UploadID, AttachmentID: reservation.AttachmentID,
		State: reservation.State, Quota: reservation.Quota,
		Target: UploadTarget{TransportKind: service.transport, UploadID: reservation.UploadID},
	}
	if service.transport != TransportKindS3 {
		return result, nil
	}
	candidateKey, err := service.newS3TemporaryObjectKey()
	if err != nil {
		return CreateUploadResult{}, err
	}
	preparation, err := service.repository.PrepareUpload(ctx, PrepareUploadCommand{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
		CandidateTemporaryObjectKey: candidateKey,
	})
	if err != nil {
		return CreateUploadResult{}, err
	}
	if err := validateUploadPreparation(preparation, request.Actor, request.DraftID, service.transport); err != nil ||
		preparation.AttachmentID != request.AttachmentID || preparation.TemporaryObjectKey == "" {
		return CreateUploadResult{}, ErrAttachmentConflict
	}
	result.State = preparation.State
	result.Target.TemporaryObjectKey = preparation.TemporaryObjectKey
	remaining := preparation.ExpiresAt.Sub(service.now())
	if remaining < time.Second {
		return CreateUploadResult{}, ErrUploadExpired
	}
	presignTTL := 15 * time.Minute
	if remaining < presignTTL {
		presignTTL = remaining
	}
	uploadURL, method, requiredHeaders, err := service.presigner.PresignTemporaryUpload(
		ctx, preparation.TemporaryObjectKey, presignTTL,
	)
	if err != nil {
		return CreateUploadResult{}, fmt.Errorf("presign attachment temporary upload: %w", err)
	}
	if !validTemporaryUploadInstruction(uploadURL, method, requiredHeaders) {
		return CreateUploadResult{}, ErrAttachmentConflict
	}
	result.Target.UploadURL = uploadURL
	result.Target.Method = method
	result.Target.RequiredHeaders = make([]string, len(requiredHeaders))
	copy(result.Target.RequiredHeaders, requiredHeaders)
	return result, nil
}

func validTemporaryUploadInstruction(uploadURL, method string, requiredHeaders []string) bool {
	if method != http.MethodPut || requiredHeaders == nil || len(requiredHeaders) > 16 ||
		len(uploadURL) == 0 || len(uploadURL) > 8192 || strings.ContainsAny(uploadURL, "\r\n") {
		return false
	}
	parsed, err := url.Parse(uploadURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	seen := make(map[string]struct{}, len(requiredHeaders))
	for _, header := range requiredHeaders {
		canonical := http.CanonicalHeaderKey(header)
		if header == "" || header != strings.TrimSpace(header) || len(header) > 256 ||
			strings.ContainsAny(header, "\r\n") || canonical == "" ||
			strings.EqualFold(canonical, "Authorization") {
			return false
		}
		if _, exists := seen[canonical]; exists {
			return false
		}
		seen[canonical] = struct{}{}
	}
	return true
}

func (service *UploadService) requireArchiveScanner(ctx context.Context) error {
	if service.archiveScannerReadiness == nil {
		return ErrArchiveScannerUnavailable
	}
	status, err := service.archiveScannerReadiness(ctx)
	if err != nil || RequireArchiveScanner(status) != nil {
		return ErrArchiveScannerUnavailable
	}
	return nil
}

func archiveUploadDeclaration(displayName, mediaType string) bool {
	switch strings.ToLower(path.Ext(displayName)) {
	case ".zip", ".tar", ".gz", ".zst", ".zstd":
		return true
	}
	if separator := strings.IndexByte(mediaType, ';'); separator >= 0 {
		mediaType = mediaType[:separator]
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "application/zip", "application/x-tar", "application/gzip", "application/zstd":
		return true
	default:
		return false
	}
}

func (service *UploadService) PutContent(
	ctx context.Context,
	request PutUploadContentRequest,
) (PutUploadContentResult, error) {
	if err := service.validateActorRequest(ctx, request.Actor, request.DraftID, request.UploadID); err != nil ||
		nilUploadServiceDependency(request.Content) || request.ExpectedSHA256 == [sha256.Size]byte{} {
		return PutUploadContentResult{}, ErrInvalidUploadServiceRequest
	}
	if err := service.authorizer.AuthorizeDraftAttachmentUpload(ctx, request.Actor.Clone(), request.DraftID); err != nil {
		return PutUploadContentResult{}, err
	}
	candidateKey := ""
	if service.transport == TransportKindS3 {
		var err error
		candidateKey, err = service.newS3TemporaryObjectKey()
		if err != nil {
			return PutUploadContentResult{}, err
		}
	}
	preparation, err := service.repository.PrepareUpload(ctx, PrepareUploadCommand{
		ProjectID: string(request.Actor.ProjectID), UploadID: request.UploadID, AuthorID: request.Actor.UserID,
		CandidateTemporaryObjectKey: candidateKey,
	})
	if err != nil {
		return PutUploadContentResult{}, err
	}
	if err := validateUploadPreparation(preparation, request.Actor, request.DraftID, service.transport); err != nil {
		return PutUploadContentResult{}, err
	}
	if !preparation.ExpiresAt.After(service.now()) {
		return PutUploadContentResult{}, ErrUploadExpired
	}
	if service.transport == TransportKindS3 {
		if preparation.TemporaryObjectVersion != "" {
			return PutUploadContentResult{}, ErrAttachmentConflict
		}
		surviving, err := service.temporary.ResolveTemporaryVersion(ctx, preparation.TemporaryObjectKey)
		if err == nil {
			if surviving.Validate() != nil || surviving.Key != preparation.TemporaryObjectKey {
				return PutUploadContentResult{}, ErrAttachmentConflict
			}
			versioned, recordErr := service.repository.RecordTemporaryObjectVersion(ctx, RecordTemporaryObjectVersionCommand{
				ProjectID: preparation.ProjectID, UploadID: preparation.UploadID, AuthorID: preparation.AuthorID,
				TemporaryObjectKey: surviving.Key, TemporaryObjectVersion: surviving.VersionID,
			})
			if recordErr != nil {
				return PutUploadContentResult{}, recordErr
			}
			if versioned.TemporaryObjectKey != surviving.Key || versioned.TemporaryObjectVersion != surviving.VersionID ||
				versioned.State != UploadStateUploading {
				return PutUploadContentResult{}, ErrAttachmentConflict
			}
			return PutUploadContentResult{}, ErrAttachmentConflict
		}
		if !errors.Is(err, ErrBlobNotFound) {
			return PutUploadContentResult{}, err
		}
	}
	publicationIntent, err := service.prepareUploadPublication(
		ctx, preparation, request.ExpectedSHA256, preparation.DeclaredSizeBytes,
	)
	if err != nil {
		return PutUploadContentResult{}, err
	}
	object, err := service.blob.Put(ctx, PutRequest{
		ExpectedSHA256: request.ExpectedSHA256, ExpectedSizeBytes: preparation.DeclaredSizeBytes,
		TemporaryKey: preparation.TemporaryObjectKey,
	}, request.Content)
	if err != nil {
		return PutUploadContentResult{}, err
	}
	if object.Validate() != nil || object.SHA256 != request.ExpectedSHA256 ||
		object.SizeBytes != preparation.DeclaredSizeBytes {
		return PutUploadContentResult{}, ErrAttachmentConflict
	}
	publicationIntent, err = service.recordUploadPublicationVersion(ctx, publicationIntent, object)
	if err != nil {
		return PutUploadContentResult{}, err
	}
	content, err := service.repository.RecordUploadedContent(ctx, RecordUploadedContentCommand{
		ProjectID: preparation.ProjectID, UploadID: preparation.UploadID, AuthorID: preparation.AuthorID,
		TemporaryObjectKey: preparation.TemporaryObjectKey, Object: object, PublicationIntent: publicationIntent,
	})
	if err != nil {
		return PutUploadContentResult{}, err
	}
	if err := validateUploadedContent(content, request.Actor, request.DraftID, service.transport); err != nil ||
		content.Object != object || content.AttachmentID != preparation.AttachmentID {
		return PutUploadContentResult{}, ErrAttachmentConflict
	}
	return PutUploadContentResult{UploadID: content.UploadID, AttachmentID: content.AttachmentID, Object: content.Object}, nil
}

func (service *UploadService) CompleteUpload(
	ctx context.Context,
	request CompleteUploadRequest,
) (UploadMutationResult, error) {
	if err := service.validateActorRequest(ctx, request.Actor, request.DraftID, request.UploadID); err != nil {
		return UploadMutationResult{}, err
	}
	if err := service.authorizer.AuthorizeDraftAttachmentUpload(ctx, request.Actor.Clone(), request.DraftID); err != nil {
		return UploadMutationResult{}, err
	}
	completion, err := service.repository.GetUploadCompletionPreparation(ctx, UploadMutationCommand{
		ProjectID: string(request.Actor.ProjectID), UploadID: request.UploadID, AuthorID: request.Actor.UserID,
	})
	if err != nil {
		return UploadMutationResult{}, err
	}
	if err := validateUploadCompletionPreparation(completion, request.Actor, request.DraftID, service.transport); err != nil {
		return UploadMutationResult{}, err
	}
	object := completion.Object
	actualSize, actualDigest := object.SizeBytes, object.SHA256
	if completion.State == UploadStateUploading {
		if !completion.ExpiresAt.After(service.now()) {
			return UploadMutationResult{}, ErrUploadExpired
		}
		if completion.HasObject {
			info, err := service.blob.Stat(ctx, object)
			if err != nil {
				return UploadMutationResult{}, err
			}
			if info.Version != object {
				return UploadMutationResult{}, ErrAttachmentConflict
			}
			actualSize, actualDigest, err = service.verifyObjectBytes(ctx, object)
			if err != nil {
				return UploadMutationResult{}, err
			}
		} else {
			if service.transport != TransportKindS3 {
				return UploadMutationResult{}, ErrAttachmentConflict
			}
			completion, object, actualSize, actualDigest, err = service.publishDirectS3Upload(ctx, completion)
			if err != nil {
				return UploadMutationResult{}, err
			}
		}
	} else if !completion.HasObject {
		return UploadMutationResult{}, ErrAttachmentConflict
	}
	content := uploadedContentFromCompletion(completion, object)
	profile, err := service.resolveProcessorProfile(content.MediaType)
	if err != nil || !knownProcessorProfile(profile) {
		return UploadMutationResult{}, ErrInvalidAttachmentCommand
	}
	fingerprint := uploadCompletionFingerprint(content, actualSize, actualDigest)
	result, err := service.repository.CompleteUploadAndEnqueue(ctx, CompleteUploadAndEnqueueCommand{
		ProjectID: content.ProjectID, UploadID: content.UploadID, AuthorID: content.AuthorID,
		ActualSizeBytes: actualSize, ActualSHA256: actualDigest,
		TemporaryObjectKey: content.TemporaryObjectKey, TemporaryObjectVersion: content.TemporaryObjectVersion,
		Object: content.Object, PublicationIntent: completion.PublicationIntent, CompletionFingerprint: fingerprint,
		ProcessorJobID: processorJobID(content.UploadID), ProcessorProfile: profile,
		ProcessorMaxAttempts: service.processorMaxAttempts, ProcessorExpiresAt: content.ExpiresAt,
	})
	if err != nil {
		return UploadMutationResult{}, err
	}
	if service.transport == TransportKindS3 && content.TemporaryObjectVersion != "" {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s3BlobCleanupTimeout)
		defer cancel()
		if err := service.temporary.DeleteTemporaryVersion(cleanupCtx, TemporaryObjectVersion{
			Key: content.TemporaryObjectKey, VersionID: content.TemporaryObjectVersion,
		}); err != nil {
			return result, fmt.Errorf("clean completed attachment temporary object: %w", err)
		}
	}
	return result, nil
}

func (service *UploadService) publishDirectS3Upload(
	ctx context.Context,
	completion UploadCompletionPreparation,
) (UploadCompletionPreparation, ObjectVersion, int64, [sha256.Size]byte, error) {
	resolved, err := service.temporary.ResolveTemporaryVersion(ctx, completion.TemporaryObjectKey)
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	if resolved.Validate() != nil || resolved.Key != completion.TemporaryObjectKey ||
		(completion.TemporaryObjectVersion != "" && completion.TemporaryObjectVersion != resolved.VersionID) {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, ErrAttachmentConflict
	}
	versioned, err := service.repository.RecordTemporaryObjectVersion(ctx, RecordTemporaryObjectVersionCommand{
		ProjectID: completion.ProjectID, UploadID: completion.UploadID, AuthorID: completion.AuthorID,
		TemporaryObjectKey: resolved.Key, TemporaryObjectVersion: resolved.VersionID,
	})
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	if err := validateUploadPreparation(versioned, recordauth.ActorScope{
		UserID: completion.AuthorID, ProjectID: recordauth.ProjectID(completion.ProjectID),
	}, completion.DraftID, TransportKindS3); err != nil ||
		versioned.AttachmentID != completion.AttachmentID || versioned.DeclaredSizeBytes != completion.DeclaredSizeBytes ||
		versioned.MediaType != completion.MediaType || versioned.TemporaryObjectKey != resolved.Key ||
		versioned.TemporaryObjectVersion != resolved.VersionID {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, ErrAttachmentConflict
	}
	completion.UploadPreparation = versioned
	reader, err := service.temporary.OpenTemporaryVersion(ctx, TemporaryObjectReadRequest{
		Version: resolved, ExpectedSizeBytes: completion.DeclaredSizeBytes,
	})
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	actualSize, digest, err := verifyAttachmentBytes(reader, completion.DeclaredSizeBytes)
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	publicationIntent, err := service.prepareUploadPublication(ctx, completion.UploadPreparation, digest, actualSize)
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	published, err := service.temporary.PublishTemporaryVersion(ctx, TemporaryObjectPublishRequest{
		Version: resolved, ExpectedSHA256: digest, ExpectedSizeBytes: actualSize,
	})
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	if published.Validate() != nil || published.SHA256 != digest || published.SizeBytes != actualSize {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, ErrAttachmentConflict
	}
	publicationIntent, err = service.recordUploadPublicationVersion(ctx, publicationIntent, published)
	if err != nil {
		return UploadCompletionPreparation{}, ObjectVersion{}, 0, [sha256.Size]byte{}, err
	}
	completion.Object = published
	completion.HasObject = true
	completion.PublicationIntent = publicationIntent
	return completion, published, actualSize, digest, nil
}

func (service *UploadService) prepareUploadPublication(
	ctx context.Context,
	preparation UploadPreparation,
	digest [sha256.Size]byte,
	sizeBytes int64,
) (BlobPublicationIntent, error) {
	backend, ok := blobBackendForTransport(preparation.TransportKind)
	if !ok {
		return BlobPublicationIntent{}, ErrInvalidAttachmentCommand
	}
	target := BlobPublicationTarget{
		Key: "sha256/" + hexDigest(digest), SHA256: digest,
		SizeBytes: sizeBytes, BackendKind: backend,
	}
	request := BlobPublicationPrepareRequest{
		ProjectID: preparation.ProjectID, OwnerKind: BlobPublicationOwnerUpload,
		OwnerID: preparation.UploadID, OwnerGeneration: 1, Target: target,
		PublishExpiresAt: preparation.ExpiresAt.UTC(),
	}
	intent, err := service.repository.PrepareBlobPublication(ctx, request)
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

func (service *UploadService) recordUploadPublicationVersion(
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
	published, err := service.repository.RecordBlobPublicationVersion(ctx, BlobPublicationVersionRequest{
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

func blobBackendForTransport(transport TransportKind) (BackendKind, bool) {
	switch transport {
	case TransportKindLocal:
		return BackendKindLocal, true
	case TransportKindS3:
		return BackendKindS3, true
	default:
		return "", false
	}
}

func (service *UploadService) validateActorRequest(
	ctx context.Context,
	actor recordauth.ActorScope,
	draftID string,
	uploadID string,
) error {
	if ctx == nil || service == nil || nilUploadServiceDependency(service.authorizer) ||
		nilUploadServiceDependency(service.repository) || nilUploadServiceDependency(service.blob) ||
		ctx.Err() != nil || len(actor.CanonicalBytes()) == 0 ||
		!validPrefixedID(draftID, "rdf_") || ValidateUploadID(uploadID) != nil {
		return ErrInvalidUploadServiceRequest
	}
	return nil
}

func (service *UploadService) newS3TemporaryObjectKey() (string, error) {
	key, err := service.newTemporaryObjectKey()
	if err != nil {
		return "", fmt.Errorf("generate attachment upload temporary key: %w", err)
	}
	if !validS3BlobTemporaryKey(key) {
		return "", ErrInvalidUploadServiceRequest
	}
	return key, nil
}

func (service *UploadService) verifyObjectBytes(
	ctx context.Context,
	object ObjectVersion,
) (int64, [sha256.Size]byte, error) {
	reader, err := service.blob.Open(ctx, object, FullByteRange())
	if err != nil {
		return 0, [sha256.Size]byte{}, err
	}
	actualSize, digest, err := verifyAttachmentBytes(reader, object.SizeBytes)
	if err != nil {
		return 0, [sha256.Size]byte{}, err
	}
	if digest != object.SHA256 {
		return 0, [sha256.Size]byte{}, ErrBlobHashMismatch
	}
	return actualSize, digest, nil
}

func verifyAttachmentBytes(reader io.ReadCloser, expectedSize int64) (int64, [sha256.Size]byte, error) {
	hasher := sha256.New()
	actualSize, readErr := io.Copy(hasher, io.LimitReader(reader, expectedSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return 0, [sha256.Size]byte{}, errors.Join(readErr, closeErr)
	}
	if actualSize != expectedSize {
		return 0, [sha256.Size]byte{}, ErrBlobSizeMismatch
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return actualSize, digest, nil
}

func validateUploadPreparation(
	preparation UploadPreparation,
	actor recordauth.ActorScope,
	draftID string,
	transport TransportKind,
) error {
	if preparation.ProjectID != string(actor.ProjectID) || preparation.AuthorID != actor.UserID ||
		preparation.DraftID != draftID || ValidateUploadID(preparation.UploadID) != nil ||
		ValidateAttachmentID(preparation.AttachmentID) != nil || preparation.TransportKind != transport ||
		preparation.State != UploadStateUploading || preparation.DeclaredSizeBytes <= 0 ||
		!validAttachmentText(preparation.MediaType) || preparation.ExpiresAt.IsZero() ||
		(transport == TransportKindLocal && (preparation.TemporaryObjectKey != "" || preparation.TemporaryObjectVersion != "")) ||
		(transport == TransportKindS3 && !validS3BlobTemporaryKey(preparation.TemporaryObjectKey)) {
		return ErrAttachmentConflict
	}
	return nil
}

func validateUploadedContent(
	content UploadedContent,
	actor recordauth.ActorScope,
	draftID string,
	transport TransportKind,
) error {
	if validateUploadedContentIdentity(content, actor, draftID, transport) != nil ||
		content.State != UploadStateUploading {
		return ErrAttachmentConflict
	}
	return nil
}

func validateCompletionContent(
	content UploadedContent,
	actor recordauth.ActorScope,
	draftID string,
	transport TransportKind,
) error {
	if validateUploadedContentIdentity(content, actor, draftID, transport) != nil ||
		(content.State != UploadStateUploading && content.State != UploadStateQuarantined &&
			content.State != UploadStateAvailable) {
		return ErrAttachmentConflict
	}
	return nil
}

func validateUploadCompletionPreparation(
	completion UploadCompletionPreparation,
	actor recordauth.ActorScope,
	draftID string,
	transport TransportKind,
) error {
	preparation := completion.UploadPreparation
	if preparation.ProjectID != string(actor.ProjectID) || preparation.AuthorID != actor.UserID ||
		preparation.DraftID != draftID || ValidateUploadID(preparation.UploadID) != nil ||
		ValidateAttachmentID(preparation.AttachmentID) != nil || preparation.TransportKind != transport ||
		preparation.DeclaredSizeBytes <= 0 || !validAttachmentText(preparation.MediaType) ||
		preparation.ExpiresAt.IsZero() ||
		(preparation.State != UploadStateUploading && preparation.State != UploadStateQuarantined &&
			preparation.State != UploadStateAvailable) ||
		(transport == TransportKindLocal && (preparation.TemporaryObjectKey != "" ||
			preparation.TemporaryObjectVersion != "")) ||
		(transport == TransportKindS3 && !validS3BlobTemporaryKey(preparation.TemporaryObjectKey)) ||
		(completion.HasObject && completion.Object.Validate() != nil) ||
		(!completion.HasObject && completion.Object != (ObjectVersion{})) {
		return ErrAttachmentConflict
	}
	return nil
}

func uploadedContentFromCompletion(
	completion UploadCompletionPreparation,
	object ObjectVersion,
) UploadedContent {
	preparation := completion.UploadPreparation
	return UploadedContent{
		ProjectID: preparation.ProjectID, UploadID: preparation.UploadID,
		AttachmentID: preparation.AttachmentID, DraftID: preparation.DraftID,
		AuthorID: preparation.AuthorID, State: preparation.State,
		TransportKind: preparation.TransportKind, MediaType: preparation.MediaType,
		ExpiresAt: preparation.ExpiresAt, TemporaryObjectKey: preparation.TemporaryObjectKey,
		TemporaryObjectVersion: preparation.TemporaryObjectVersion, Object: object,
	}
}

func validateUploadedContentIdentity(
	content UploadedContent,
	actor recordauth.ActorScope,
	draftID string,
	transport TransportKind,
) error {
	if content.ProjectID != string(actor.ProjectID) || content.AuthorID != actor.UserID || content.DraftID != draftID ||
		ValidateUploadID(content.UploadID) != nil || ValidateAttachmentID(content.AttachmentID) != nil ||
		content.TransportKind != transport ||
		!validAttachmentText(content.MediaType) || content.ExpiresAt.IsZero() || content.Object.Validate() != nil ||
		(transport == TransportKindLocal && (content.TemporaryObjectKey != "" || content.TemporaryObjectVersion != "")) ||
		(transport == TransportKindS3 && !validS3BlobTemporaryKey(content.TemporaryObjectKey)) {
		return ErrAttachmentConflict
	}
	return nil
}

func uploadCompletionFingerprint(
	content UploadedContent,
	actualSize int64,
	digest [sha256.Size]byte,
) [sha256.Size]byte {
	hasher := sha256.New()
	for _, value := range []string{
		"houfeng.attachment-upload-completion.v1", content.ProjectID, content.UploadID,
		content.AttachmentID, content.DraftID, content.AuthorID, content.TemporaryObjectKey,
		content.TemporaryObjectVersion, content.Object.Key, content.Object.VersionID,
	} {
		_, _ = hasher.Write([]byte(value))
		_, _ = hasher.Write([]byte{0})
	}
	var encodedSize [8]byte
	binary.BigEndian.PutUint64(encodedSize[:], uint64(actualSize))
	_, _ = hasher.Write(encodedSize[:])
	_, _ = hasher.Write(digest[:])
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], hasher.Sum(nil))
	return fingerprint
}

func processorJobID(uploadID string) string {
	return "apj_" + strings.TrimPrefix(uploadID, "aup_")
}

func processorProfileForMediaTypeHint(mediaType string) (ProcessorProfile, error) {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return ProcessorProfileImage, nil
	case mediaType == "application/pdf":
		return ProcessorProfilePDF, nil
	case strings.HasPrefix(mediaType, "text/"), mediaType == "application/json", mediaType == "application/yaml":
		return ProcessorProfileText, nil
	case mediaType == "application/zip", mediaType == "application/gzip", mediaType == "application/x-tar",
		mediaType == "application/zstd":
		return ProcessorProfileArchive, nil
	default:
		return "", ErrInvalidAttachmentCommand
	}
}

func knownProcessorProfile(profile ProcessorProfile) bool {
	return profile == ProcessorProfileImage || profile == ProcessorProfilePDF ||
		profile == ProcessorProfileText || profile == ProcessorProfileArchive
}

// Package attachments owns transport-neutral attachment, upload, quota, and
// Blob contracts.
package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf8"
)

const (
	MiB int64 = 1 << 20
	GiB int64 = 1 << 30
)

var (
	ErrInvalidAttachmentID          = errors.New("invalid attachment id")
	ErrInvalidUploadID              = errors.New("invalid attachment upload id")
	ErrInvalidProcessorJobID        = errors.New("invalid attachment processor job id")
	ErrInvalidWorkspaceID           = errors.New("invalid content processor workspace id")
	ErrInvalidBlobGCPinID           = errors.New("invalid blob gc pin id")
	ErrInvalidAttachmentReferences  = errors.New("invalid attachment references")
	ErrInvalidLimits                = errors.New("invalid attachment limits")
	ErrInvalidUploadStateTransition = errors.New("invalid attachment upload state transition")
	ErrInvalidScannerStatus         = errors.New("invalid attachment scanner status")
	ErrArchiveScannerUnavailable    = errors.New("archive scanner unavailable")
	ErrInvalidQuotaUsage            = errors.New("invalid attachment quota usage")
	ErrQuotaExceeded                = errors.New("attachment quota exceeded")
	ErrQuotaOverflow                = errors.New("attachment quota arithmetic overflow")
	ErrInvalidAttachmentCommand     = errors.New("invalid attachment command")
	ErrAttachmentOwnerNotFound      = errors.New("attachment owner not found")
	ErrAttachmentConflict           = errors.New("attachment conflict")
)

type QuotaScope string

const (
	QuotaScopeFile    QuotaScope = "file"
	QuotaScopeRecord  QuotaScope = "record"
	QuotaScopeProject QuotaScope = "project"
)

type QuotaExceededError struct {
	Scope     QuotaScope
	Limit     int64
	Current   int64
	Requested int64
}

func (err *QuotaExceededError) Error() string {
	if err == nil {
		return ErrQuotaExceeded.Error()
	}
	return fmt.Sprintf("%s: %s bytes %d + %d exceeds %d", ErrQuotaExceeded, err.Scope, err.Current, err.Requested, err.Limit)
}

func (err *QuotaExceededError) Unwrap() error {
	return ErrQuotaExceeded
}

type Limits struct {
	MaxFileBytes              int64
	MaxRecordBytes            int64
	MaxProjectBytes           int64
	WarningPercent            uint32
	MaxInlineTextPreviewBytes int64
}

func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:              50 * MiB,
		MaxRecordBytes:            500 * MiB,
		MaxProjectBytes:           10 * GiB,
		WarningPercent:            80,
		MaxInlineTextPreviewBytes: 5 * MiB,
	}
}

func (limits Limits) Validate() error {
	if limits.MaxFileBytes <= 0 || limits.MaxRecordBytes <= 0 || limits.MaxProjectBytes <= 0 ||
		limits.MaxInlineTextPreviewBytes <= 0 || limits.MaxFileBytes > limits.MaxRecordBytes ||
		limits.MaxRecordBytes > limits.MaxProjectBytes ||
		limits.MaxInlineTextPreviewBytes > limits.MaxFileBytes ||
		limits.WarningPercent == 0 || limits.WarningPercent > 100 {
		return ErrInvalidLimits
	}
	return nil
}

type QuotaUsage struct {
	LogicalBytes  int64
	ReservedBytes int64
	PhysicalBytes int64
}

type UploadReservationQuotaDecision struct {
	ProjectReservedBytes int64
	EffectiveRecordBytes int64
	ProjectWarning       bool
}

type QuotaSnapshot struct {
	Usage                QuotaUsage
	EffectiveRecordBytes int64
	ProjectWarning       bool
}

type ProjectQuotaSnapshotCommand struct {
	ProjectID string
	Limits    Limits
}

func (command ProjectQuotaSnapshotCommand) Validate() error {
	if command.ProjectID != "default" || command.Limits.Validate() != nil {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type ProjectQuotaSnapshot struct {
	Usage          QuotaUsage
	ProjectWarning bool
}

type UploadReservationResult struct {
	UploadID     string
	AttachmentID string
	State        UploadState
	Quota        QuotaSnapshot
}

func EvaluateUploadReservationQuota(
	usage QuotaUsage,
	effectiveRecordBytes int64,
	requestedBytes int64,
	limits Limits,
) (UploadReservationQuotaDecision, error) {
	if err := limits.Validate(); err != nil {
		return UploadReservationQuotaDecision{}, err
	}
	if err := usage.validate(); err != nil || effectiveRecordBytes < 0 || requestedBytes <= 0 {
		return UploadReservationQuotaDecision{}, ErrInvalidQuotaUsage
	}
	if requestedBytes > limits.MaxFileBytes {
		return UploadReservationQuotaDecision{}, newQuotaExceededError(QuotaScopeFile, limits.MaxFileBytes, 0, requestedBytes)
	}

	nextRecordBytes, err := checkedQuotaAdd(effectiveRecordBytes, requestedBytes)
	if err != nil {
		return UploadReservationQuotaDecision{}, err
	}
	projectBytes, err := checkedQuotaAdd(usage.LogicalBytes, usage.ReservedBytes)
	if err != nil {
		return UploadReservationQuotaDecision{}, err
	}
	nextProjectBytes, err := checkedQuotaAdd(projectBytes, requestedBytes)
	if err != nil {
		return UploadReservationQuotaDecision{}, err
	}
	nextReservedBytes, err := checkedQuotaAdd(usage.ReservedBytes, requestedBytes)
	if err != nil {
		return UploadReservationQuotaDecision{}, err
	}
	if nextRecordBytes > limits.MaxRecordBytes {
		return UploadReservationQuotaDecision{}, newQuotaExceededError(
			QuotaScopeRecord, limits.MaxRecordBytes, effectiveRecordBytes, requestedBytes,
		)
	}
	if nextProjectBytes > limits.MaxProjectBytes {
		return UploadReservationQuotaDecision{}, newQuotaExceededError(
			QuotaScopeProject, limits.MaxProjectBytes, projectBytes, requestedBytes,
		)
	}

	return UploadReservationQuotaDecision{
		ProjectReservedBytes: nextReservedBytes,
		EffectiveRecordBytes: nextRecordBytes,
		ProjectWarning:       nextProjectBytes >= quotaWarningThreshold(limits.MaxProjectBytes, limits.WarningPercent),
	}, nil
}

func (usage QuotaUsage) SolidifyReservation(reservedBytes, logicalBytes, physicalBytes int64) (QuotaUsage, error) {
	if err := usage.validate(); err != nil || reservedBytes < 0 || logicalBytes < 0 || physicalBytes < 0 ||
		reservedBytes > usage.ReservedBytes {
		return QuotaUsage{}, ErrInvalidQuotaUsage
	}
	nextLogicalBytes, err := checkedQuotaAdd(usage.LogicalBytes, logicalBytes)
	if err != nil {
		return QuotaUsage{}, err
	}
	nextPhysicalBytes, err := checkedQuotaAdd(usage.PhysicalBytes, physicalBytes)
	if err != nil {
		return QuotaUsage{}, err
	}
	return QuotaUsage{
		LogicalBytes:  nextLogicalBytes,
		ReservedBytes: usage.ReservedBytes - reservedBytes,
		PhysicalBytes: nextPhysicalBytes,
	}, nil
}

func (usage QuotaUsage) ReleaseReservation(reservedBytes int64) (QuotaUsage, error) {
	if err := usage.validate(); err != nil || reservedBytes < 0 || reservedBytes > usage.ReservedBytes {
		return QuotaUsage{}, ErrInvalidQuotaUsage
	}
	usage.ReservedBytes -= reservedBytes
	return usage, nil
}

func (usage QuotaUsage) ProjectWarning(limits Limits) (bool, error) {
	if err := usage.validate(); err != nil {
		return false, err
	}
	if err := limits.Validate(); err != nil {
		return false, err
	}
	projectBytes, err := checkedQuotaAdd(usage.LogicalBytes, usage.ReservedBytes)
	if err != nil {
		return false, err
	}
	return projectBytes >= quotaWarningThreshold(limits.MaxProjectBytes, limits.WarningPercent), nil
}

func (usage QuotaUsage) validate() error {
	if usage.LogicalBytes < 0 || usage.ReservedBytes < 0 || usage.PhysicalBytes < 0 {
		return ErrInvalidQuotaUsage
	}
	return nil
}

func checkedQuotaAdd(left, right int64) (int64, error) {
	if left < 0 || right < 0 {
		return 0, ErrInvalidQuotaUsage
	}
	if left > math.MaxInt64-right {
		return 0, ErrQuotaOverflow
	}
	return left + right, nil
}

func quotaWarningThreshold(limit int64, warningPercent uint32) int64 {
	whole := (limit / 100) * int64(warningPercent)
	remainder := ((limit % 100) * int64(warningPercent)) + 99
	return whole + remainder/100
}

func newQuotaExceededError(scope QuotaScope, limit, current, requested int64) error {
	return &QuotaExceededError{
		Scope:     scope,
		Limit:     limit,
		Current:   current,
		Requested: requested,
	}
}

type UploadState string

const (
	UploadStateCreated     UploadState = "created"
	UploadStateUploading   UploadState = "uploading"
	UploadStateQuarantined UploadState = "quarantined"
	UploadStateAvailable   UploadState = "available"
	UploadStateRejected    UploadState = "rejected"
	UploadStateExpired     UploadState = "expired"
)

type TransportKind string

const (
	TransportKindLocal TransportKind = "local"
	TransportKindS3    TransportKind = "s3"
)

type BackendKind string

const (
	BackendKindLocal BackendKind = "local"
	BackendKindS3    BackendKind = "s3"
)

type BlobObject struct {
	Key           string
	SHA256        [sha256.Size]byte
	ObjectVersion string
	SizeBytes     int64
	BackendKind   BackendKind
}

func (object BlobObject) Validate() error {
	wantKey := "sha256/" + hex.EncodeToString(object.SHA256[:])
	if object.Key != wantKey || object.ObjectVersion == "" || len(object.ObjectVersion) > 1024 ||
		object.SizeBytes <= 0 || (object.BackendKind != BackendKindLocal && object.BackendKind != BackendKindS3) {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type ReserveUploadCommand struct {
	ProjectID         string
	UploadID          string
	AttachmentID      string
	DraftID           string
	AuthorID          string
	DisplayName       string
	MediaType         string
	TransportKind     TransportKind
	DeclaredSizeBytes int64
	ExpiresAt         time.Time
	Limits            Limits
}

type UploadMutationCommand struct {
	ProjectID string
	UploadID  string
	AuthorID  string
}

func (command UploadMutationCommand) Validate() error {
	if command.ProjectID != "default" || ValidateUploadID(command.UploadID) != nil ||
		!validPrefixedID(command.AuthorID, "usr_") {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type CompleteUploadContentCommand struct {
	ProjectID              string
	UploadID               string
	AuthorID               string
	ActualSizeBytes        int64
	ActualSHA256           [sha256.Size]byte
	TemporaryObjectKey     string
	TemporaryObjectVersion string
	CompletionFingerprint  [sha256.Size]byte
}

func (command CompleteUploadContentCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || command.ActualSizeBytes <= 0 || command.ActualSHA256 == [sha256.Size]byte{} ||
		command.TemporaryObjectKey == "" || len(command.TemporaryObjectKey) > 1024 ||
		command.TemporaryObjectVersion == "" || len(command.TemporaryObjectVersion) > 1024 ||
		command.CompletionFingerprint == [sha256.Size]byte{} {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type AdmitUploadCommand struct {
	ProjectID string
	UploadID  string
	AuthorID  string
	Blob      BlobObject
	Limits    Limits
}

func (command AdmitUploadCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || command.Blob.Validate() != nil || command.Limits.Validate() != nil {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type FailUploadCommand struct {
	ProjectID   string
	UploadID    string
	AuthorID    string
	TargetState UploadState
	Limits      Limits
}

func (command FailUploadCommand) Validate() error {
	base := UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	if base.Validate() != nil || (command.TargetState != UploadStateRejected && command.TargetState != UploadStateExpired) ||
		command.Limits.Validate() != nil {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type UploadMutationResult struct {
	UploadID     string
	AttachmentID string
	State        UploadState
	Quota        QuotaSnapshot
}

type CopyAttachmentCommand struct {
	ProjectID          string
	SourceRecordID     string
	TargetRecordID     string
	SourceAttachmentID string
	TargetAttachmentID string
	ActorID            string
	Limits             Limits
}

func (command CopyAttachmentCommand) Validate() error {
	if command.ProjectID != "default" || !validPrefixedID(command.SourceRecordID, "rec_") ||
		!validPrefixedID(command.TargetRecordID, "rec_") || command.SourceRecordID == command.TargetRecordID ||
		ValidateAttachmentID(command.SourceAttachmentID) != nil ||
		ValidateAttachmentID(command.TargetAttachmentID) != nil ||
		command.SourceAttachmentID == command.TargetAttachmentID || !validPrefixedID(command.ActorID, "usr_") ||
		command.Limits.Validate() != nil {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type CopyAttachmentResult struct {
	AttachmentID           string
	CopiedFromAttachmentID string
	Quota                  QuotaSnapshot
}

type BlobGCPinOwnerKind string

const (
	BlobGCPinOwnerBackupManifest      BlobGCPinOwnerKind = "backup_manifest"
	BlobGCPinOwnerRestoreAttempt      BlobGCPinOwnerKind = "restore_attempt"
	BlobGCPinOwnerImportPlan          BlobGCPinOwnerKind = "import_plan"
	BlobGCPinOwnerRevisionTransaction BlobGCPinOwnerKind = "revision_transaction"
)

type BlobProtectionCommand struct {
	BlobKey           string
	BlobObjectVersion string
}

func (command BlobProtectionCommand) Validate() error {
	if !validBlobReference(command.BlobKey, command.BlobObjectVersion) {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type CreateBlobGCPinCommand struct {
	PinID             string
	OwnerKind         BlobGCPinOwnerKind
	OwnerID           string
	BlobKey           string
	BlobObjectVersion string
	ExpiresAt         time.Time
}

func (command CreateBlobGCPinCommand) Validate() error {
	if ValidateBlobGCPinID(command.PinID) != nil || !validBlobGCPinOwnerKind(command.OwnerKind) ||
		!validBlobGCPinOwnerID(command.OwnerID) ||
		!validBlobReference(command.BlobKey, command.BlobObjectVersion) || command.ExpiresAt.IsZero() {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type ReleaseBlobGCPinCommand struct {
	PinID             string
	OwnerKind         BlobGCPinOwnerKind
	OwnerID           string
	BlobKey           string
	BlobObjectVersion string
}

func (command ReleaseBlobGCPinCommand) Validate() error {
	if ValidateBlobGCPinID(command.PinID) != nil || !validBlobGCPinOwnerKind(command.OwnerKind) ||
		!validBlobGCPinOwnerID(command.OwnerID) || !validBlobReference(command.BlobKey, command.BlobObjectVersion) {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type BlobProtection struct {
	BlobKey                string
	BlobObjectVersion      string
	LogicalAttachmentCount int64
	RevisionReferenceCount int64
	ActivePinCount         int64
	Protected              bool
}

func validBlobGCPinOwnerKind(kind BlobGCPinOwnerKind) bool {
	switch kind {
	case BlobGCPinOwnerBackupManifest, BlobGCPinOwnerRestoreAttempt,
		BlobGCPinOwnerImportPlan, BlobGCPinOwnerRevisionTransaction:
		return true
	default:
		return false
	}
}

func validBlobGCPinOwnerID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
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

func validBlobReference(key, version string) bool {
	const prefix = "sha256/"
	if len(key) != len(prefix)+(sha256.Size*2) || key[:len(prefix)] != prefix ||
		version == "" || len(version) > 1024 {
		return false
	}
	digest, err := hex.DecodeString(key[len(prefix):])
	return err == nil && hex.EncodeToString(digest) == key[len(prefix):]
}

func (command ReserveUploadCommand) Validate() error {
	if command.ProjectID != "default" || ValidateUploadID(command.UploadID) != nil ||
		ValidateAttachmentID(command.AttachmentID) != nil || !validPrefixedID(command.DraftID, "rdf_") ||
		!validPrefixedID(command.AuthorID, "usr_") || !validAttachmentText(command.DisplayName) ||
		!validAttachmentText(command.MediaType) ||
		(command.TransportKind != TransportKindLocal && command.TransportKind != TransportKindS3) ||
		command.DeclaredSizeBytes <= 0 || command.ExpiresAt.IsZero() || command.Limits.Validate() != nil {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

func validAttachmentText(value string) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 255
}

func ValidateUploadStateTransition(from, to UploadState) error {
	valid := false
	switch from {
	case UploadStateCreated:
		valid = to == UploadStateUploading || to == UploadStateExpired
	case UploadStateUploading:
		valid = to == UploadStateQuarantined || to == UploadStateRejected || to == UploadStateExpired
	case UploadStateQuarantined:
		valid = to == UploadStateAvailable || to == UploadStateRejected || to == UploadStateExpired
	case UploadStateAvailable, UploadStateRejected, UploadStateExpired:
		valid = false
	default:
		valid = false
	}
	if !valid {
		return fmt.Errorf("%w: %q to %q", ErrInvalidUploadStateTransition, from, to)
	}
	return nil
}

type ScannerStatus string

const (
	ScannerStatusUnconfigured ScannerStatus = "unconfigured"
	ScannerStatusUnhealthy    ScannerStatus = "unhealthy"
	ScannerStatusHealthy      ScannerStatus = "healthy"
)

func RequireArchiveScanner(status ScannerStatus) error {
	switch status {
	case ScannerStatusHealthy:
		return nil
	case ScannerStatusUnconfigured, ScannerStatusUnhealthy:
		return ErrArchiveScannerUnavailable
	default:
		return ErrInvalidScannerStatus
	}
}

type AttachmentReference struct {
	AttachmentID string
}

package attachments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrLocalNamespaceClosed = errors.New("local Blob namespace is closed or unavailable")
	ErrLocalIdentityChanged = errors.New("local Blob physical identity changed")
	ErrManagedBlobRequired  = errors.New("local Blob mutation requires managed dispatch or cleanup")
	ErrWriteOutcomeUnknown  = errors.New("Blob write outcome is not durably confirmed")
	ErrLocalGuardExpired    = errors.New("local Blob key guard has expired")
)

// LocalNamespace binds one admitted physical incarnation. IDs, marker contents
// and directory identities come from provisioning, never from runtime defaults.
type LocalNamespace struct {
	NamespaceID      string  `json:"namespace_id"`
	IncarnationID    string  `json:"incarnation_id"`
	RequestID        string  `json:"request_id"`
	RootPath         string  `json:"root_path"`
	BootID           string  `json:"boot_id"`
	Device           uint64  `json:"device"`
	RootInode        uint64  `json:"root_inode"`
	DigestInode      uint64  `json:"digest_inode"`
	LocksInode       uint64  `json:"locks_inode"`
	MarkerInode      uint64  `json:"marker_inode"`
	OwnerUID         uint32  `json:"owner_uid"`
	OwnerGID         uint32  `json:"owner_gid"`
	MarkerDigest     string  `json:"marker_digest"`
	MarkerNonce      string  `json:"marker_nonce"`
	SourceRecoveryID *string `json:"source_recovery_id"`
	AdmissionState   string  `json:"-"`
}

type LocalAnchor struct {
	NamespaceID   string `json:"namespace_id"`
	IncarnationID string `json:"incarnation_id"`
	Key           string `json:"key"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	Nonce         string `json:"nonce"`
}

type LocalNamespaceAdmission struct {
	NamespaceID   string `json:"namespace_id"`
	IncarnationID string `json:"incarnation_id"`
	RequestID     string `json:"request_id"`
	MarkerDigest  string `json:"marker_digest"`
}

type LocalNamespaceRepository interface {
	LoadLocalNamespace(context.Context, string) (LocalNamespace, error)
	LoadLocalAnchor(context.Context, string, string, string) (LocalAnchor, error)
	RegisterLocalAnchor(context.Context, LocalAnchor) (LocalAnchor, error)
}

type LocalNamespaceAdministrator interface {
	CheckLocalProvisionAuthority(context.Context) error
	ProvisionLocalNamespace(context.Context, LocalNamespace) (LocalNamespace, error)
	AdmitLocalNamespace(context.Context, LocalNamespaceAdmission) (LocalNamespace, error)
	LoadLocalNamespace(context.Context, string) (LocalNamespace, error)
}

// WriteOrigin names the actual repository owner. The repository derives any
// record and its epoch from that route, or authenticates a standalone lifecycle.
type WriteOrigin struct {
	Kind               string     `json:"origin_kind"`
	ID                 string     `json:"origin_id"`
	Generation         int64      `json:"origin_generation"`
	OwnerID            *string    `json:"origin_owner_id"`
	OwnerGeneration    *int64     `json:"origin_owner_generation"`
	OwnerExpiresAt     *time.Time `json:"origin_owner_expires_at"`
	DraftID            *string    `json:"draft_id"`
	PlanDigest         *string    `json:"plan_digest"`
	ArchiveDigest      *string    `json:"archive_digest"`
	PublicationID      *string    `json:"publication_id"`
	TargetRecordID     *string    `json:"target_record_id"`
	TargetAttachmentID *string    `json:"target_attachment_id"`
}

type WriteRequest struct {
	Content   PutRequest
	ProjectID string
	Origin    WriteOrigin
}

type WriteRegistration struct {
	WriteOrigin
	ObligationID   string      `json:"obligation_id"`
	WriterInstance string      `json:"writer_instance"`
	RequestSeq     int64       `json:"request_seq"`
	ProjectID      string      `json:"project_id"`
	NamespaceID    *string     `json:"namespace_id"`
	IncarnationID  *string     `json:"incarnation_id"`
	BackendKind    BackendKind `json:"backend_kind"`
	StorageBinding *string     `json:"storage_binding"`
	Key            string      `json:"key"`
	SHA256         string      `json:"sha256"`
	Size           int64       `json:"size"`
}

type WriteIdentity struct {
	ObligationID   string `json:"obligation_id"`
	WriterInstance string `json:"writer_instance"`
}

type WriteDispatch struct {
	WriteIdentity
	DispatchID  string  `json:"dispatch_id"`
	AnchorInode *uint64 `json:"anchor_inode"`
	AnchorNonce *string `json:"anchor_nonce"`
}

type WriteTemporaryCreation struct {
	WriteIdentity
	DispatchID string `json:"dispatch_id"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
}

type WriteReturn struct {
	WriteIdentity
	DispatchID string  `json:"dispatch_id"`
	Outcome    string  `json:"outcome"`
	Version    *string `json:"version"`
	Key        string  `json:"key"`
	SHA256     string  `json:"sha256"`
	Size       int64   `json:"size"`
}

type WriteObligation struct {
	ID                 string      `json:"obligation_id"`
	WriterInstance     string      `json:"writer_instance"`
	RequestSeq         int64       `json:"request_seq"`
	ProjectID          string      `json:"project_id"`
	NamespaceID        *string     `json:"namespace_id"`
	IncarnationID      *string     `json:"incarnation_id"`
	BackendKind        BackendKind `json:"backend_kind"`
	StorageBinding     *string     `json:"storage_binding"`
	Key                string      `json:"canonical_key"`
	SHA256             string      `json:"sha256"`
	Size               int64       `json:"size_bytes"`
	TemporaryKey       *string     `json:"temporary_key"`
	DispatchID         *string     `json:"dispatch_id"`
	DispatchedAt       *time.Time  `json:"dispatched_at"`
	TemporaryDevice    *uint64     `json:"temporary_device"`
	TemporaryInode     *uint64     `json:"temporary_inode"`
	Version            *string     `json:"object_version"`
	Outcome            string      `json:"outcome"`
	ReturnedAt         *time.Time  `json:"returned_at"`
	HandedOffAt        *time.Time  `json:"handed_off_at"`
	CleanupOwnerID     *string     `json:"cleanup_owner_id"`
	CleanupGeneration  int64       `json:"cleanup_generation"`
	CleanupExpiresAt   *time.Time  `json:"cleanup_expires_at"`
	CleanupDispatchID  *string     `json:"cleanup_dispatch_id"`
	CleanupOutcome     *string     `json:"cleanup_outcome"`
	CleanupCompletedAt *time.Time  `json:"cleanup_completed_at"`
	OperationID        *string     `json:"operation_id"`
}

type WrittenBlob struct {
	Version      ObjectVersion
	ObligationID string
	NamespaceID  string
}

type ManagedWriteRepository interface {
	RegisterWrite(context.Context, WriteRegistration) (WriteObligation, error)
	ResolveWrite(context.Context, WriteIdentity) (WriteObligation, error)
	DispatchWrite(context.Context, WriteDispatch) (WriteObligation, error)
	RecordWriteTemporary(context.Context, WriteTemporaryCreation) (WriteObligation, error)
	RecordWriteReturn(context.Context, WriteReturn) (WriteObligation, error)
}

func newLocalUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	var result [36]byte
	hex.Encode(result[0:8], value[0:4])
	result[8] = '-'
	hex.Encode(result[9:13], value[4:6])
	result[13] = '-'
	hex.Encode(result[14:18], value[6:8])
	result[18] = '-'
	hex.Encode(result[19:23], value[8:10])
	result[23] = '-'
	hex.Encode(result[24:36], value[10:16])
	return string(result[:]), nil
}

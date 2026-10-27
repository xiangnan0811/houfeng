package attachments

import (
	"context"
	"crypto/sha256"
)

// ImportBlobGCObservation binds one pre-Put snapshot of retained GC history to
// a digest. It is a trusted in-process value, not a client authorization token.
type ImportBlobGCObservation struct {
	digest        [sha256.Size]byte
	deletionCount int64
	observed      bool
}

func NewImportBlobGCObservation(digest [sha256.Size]byte, deletionCount int64) (ImportBlobGCObservation, error) {
	if deletionCount < 0 {
		return ImportBlobGCObservation{}, ErrInvalidAttachmentCommand
	}
	return ImportBlobGCObservation{digest: digest, deletionCount: deletionCount, observed: true}, nil
}

func (observation ImportBlobGCObservation) Validate(object ObjectVersion) error {
	if !observation.observed || observation.deletionCount < 0 ||
		object.Validate() != nil || object.SHA256 != observation.digest {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

func (observation ImportBlobGCObservation) DeletionCount() int64 {
	return observation.deletionCount
}

type ImportBlobGCObserver interface {
	ObserveImportBlobGC(context.Context, [][sha256.Size]byte) ([]ImportBlobGCObservation, error)
}

// ImportedAvailableAttachment is an admitted, already-published Blob that
// import persist binds to a newly created record inside the revision
// transaction. Callers must not trust archive MIME or path claims.
type ImportedAvailableAttachment struct {
	AttachmentID     string
	DisplayName      string
	MediaType        string
	LogicalSizeBytes int64
	Object           ObjectVersion
	BackendKind      BackendKind
	CreatedBy        string
	GCObservation    ImportBlobGCObservation `json:"-"`
}

func (item ImportedAvailableAttachment) Validate() error {
	if ValidateAttachmentID(item.AttachmentID) != nil || !validAdmissionDisplayName(item.DisplayName) ||
		item.MediaType == "" || len(item.MediaType) > 255 || item.LogicalSizeBytes <= 0 ||
		item.Object.Validate() != nil || item.Object.SizeBytes != item.LogicalSizeBytes ||
		item.GCObservation.Validate(item.Object) != nil ||
		(item.BackendKind != BackendKindLocal && item.BackendKind != BackendKindS3) ||
		!validPrefixedID(item.CreatedBy, "usr_") {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

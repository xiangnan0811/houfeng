package attachments

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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
}

func (item ImportedAvailableAttachment) Validate() error {
	if ValidateAttachmentID(item.AttachmentID) != nil || !validAdmissionDisplayName(item.DisplayName) ||
		item.MediaType == "" || len(item.MediaType) > 255 || item.LogicalSizeBytes <= 0 ||
		item.Object.Validate() != nil || item.Object.SizeBytes != item.LogicalSizeBytes ||
		(item.BackendKind != BackendKindLocal && item.BackendKind != BackendKindS3) ||
		!validPrefixedID(item.CreatedBy, "usr_") {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

func InsertImportedAvailableAttachments(
	ctx context.Context,
	tx pgx.Tx,
	projectID, recordID string,
	items []ImportedAvailableAttachment,
) error {
	if ctx == nil || tx == nil || projectID != "default" || !validPrefixedID(recordID, "rec_") {
		return ErrInvalidAttachmentCommand
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Validate() != nil {
			return ErrInvalidAttachmentCommand
		}
		if _, exists := seen[item.AttachmentID]; exists {
			return ErrInvalidAttachmentReferences
		}
		seen[item.AttachmentID] = struct{}{}
		if _, err := tx.Exec(ctx, `
			insert into public.blob_objects (
				blob_key, sha256_digest, object_version, size_bytes, backend_kind
			) values ($1, $2, $3, $4, $5)
			on conflict (blob_key) do nothing`,
			item.Object.Key, item.Object.SHA256[:], item.Object.VersionID,
			item.Object.SizeBytes, string(item.BackendKind),
		); err != nil {
			return fmt.Errorf("insert imported attachment blob: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into public.record_attachments (
				attachment_id, project_id, record_id,
				attachment_state, display_name, media_type, logical_size_bytes,
				blob_key, blob_object_version, created_by
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			item.AttachmentID, projectID, recordID, UploadStateAvailable,
			item.DisplayName, item.MediaType, item.LogicalSizeBytes,
			item.Object.Key, item.Object.VersionID, item.CreatedBy,
		); err != nil {
			return fmt.Errorf("insert imported attachment: %w", err)
		}
	}
	return nil
}

package attachments

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RevisionAttachmentCommit contains the Records identities needed to publish
// ordered attachment references inside the caller-owned PostgreSQL transaction.
type RevisionAttachmentCommit struct {
	ProjectID     string
	RecordID      string
	RevisionID    string
	DraftID       string
	AttachmentIDs []string
}

// ApplyRevisionAttachments transfers exact draft-owned available attachments
// and writes immutable ordered revision references. It performs database work
// only through tx and never calls a Blob backend or processor.
func ApplyRevisionAttachments(ctx context.Context, tx pgx.Tx, commit RevisionAttachmentCommit) error {
	if ctx == nil || tx == nil || commit.ProjectID != "default" ||
		!validPrefixedID(commit.RecordID, "rec_") || !validPrefixedID(commit.RevisionID, "rrv_") ||
		(commit.DraftID != "" && !validPrefixedID(commit.DraftID, "rdf_")) {
		return ErrInvalidAttachmentCommand
	}
	references, err := normalizeRevisionAttachmentIDs(commit.AttachmentIDs)
	if err != nil {
		return err
	}
	for ordinal, attachmentID := range references {
		var recordID, draftID *string
		var state string
		err := tx.QueryRow(ctx, `
			select record_id, draft_id, attachment_state
			from public.record_attachments
			where project_id = $1 and attachment_id = $2
			for update`, commit.ProjectID, attachmentID).Scan(&recordID, &draftID, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAttachmentConflict
		}
		if err != nil {
			return fmt.Errorf("lock revision attachment: %w", err)
		}
		if UploadState(state) != UploadStateAvailable {
			return ErrAttachmentConflict
		}
		switch {
		case recordID != nil && *recordID == commit.RecordID && draftID == nil:
			// Existing logical attachment already belongs to this record.
		case recordID == nil && draftID != nil && commit.DraftID != "" && *draftID == commit.DraftID:
			tag, err := tx.Exec(ctx, `
				update public.record_attachments
				set record_id = $3, draft_id = null, updated_at = transaction_timestamp()
				where project_id = $1 and attachment_id = $2
				  and record_id is null and draft_id = $4 and attachment_state = $5`,
				commit.ProjectID, attachmentID, commit.RecordID, commit.DraftID, UploadStateAvailable,
			)
			if err != nil {
				return fmt.Errorf("transfer revision attachment: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return ErrAttachmentConflict
			}
		default:
			return ErrAttachmentConflict
		}
		if _, err := tx.Exec(ctx, `
			insert into public.record_revision_attachments (
				record_id, revision_id, ordinal, attachment_id
			) values ($1, $2, $3, $4)`,
			commit.RecordID, commit.RevisionID, int64(ordinal), attachmentID,
		); err != nil {
			return fmt.Errorf("insert record revision attachment: %w", err)
		}
	}
	return nil
}

func normalizeRevisionAttachmentIDs(values []string) ([]string, error) {
	references := make([]AttachmentReference, 0, len(values))
	for _, attachmentID := range values {
		references = append(references, AttachmentReference{AttachmentID: attachmentID})
	}
	normalized, err := NormalizeAttachmentReferences(references)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(normalized))
	for _, reference := range normalized {
		result = append(result, reference.AttachmentID)
	}
	return result, nil
}

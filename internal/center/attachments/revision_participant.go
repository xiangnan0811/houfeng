package attachments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordidentity"
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
		!recordidentity.IsValid(commit.RecordID) || !validPrefixedID(commit.RevisionID, "rrv_") ||
		(commit.DraftID != "" && !validPrefixedID(commit.DraftID, "rdf_")) {
		return ErrInvalidAttachmentCommand
	}
	references, err := normalizeRevisionAttachmentIDs(commit.AttachmentIDs)
	if err != nil {
		return err
	}
	for ordinal, attachmentID := range references {
		body, err := json.Marshal(struct {
			ProjectID    string `json:"project_id"`
			RecordID     string `json:"record_id"`
			RevisionID   string `json:"revision_id"`
			DraftID      string `json:"draft_id"`
			AttachmentID string `json:"attachment_id"`
			Ordinal      int    `json:"ordinal"`
		}{commit.ProjectID, commit.RecordID, commit.RevisionID, commit.DraftID, attachmentID, ordinal})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select public.record_revision_attachment_publish($1)`, body); err != nil {
			var databaseError *pgconn.PgError
			if errors.As(err, &databaseError) && (databaseError.Code == "55000" || databaseError.Code == "P0002") {
				return fmt.Errorf("%w: %w", ErrAttachmentConflict, err)
			}
			return fmt.Errorf("publish revision attachment: %w", err)
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

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

type recordAttachmentRevisionParticipant struct{}

func NewRecordAttachmentRevisionParticipant() records.RevisionParticipant {
	return recordAttachmentRevisionParticipant{}
}

func (recordAttachmentRevisionParticipant) Name() string { return "attachments" }

func (recordAttachmentRevisionParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	if len(committed.ImportedAttachments) > 0 {
		if err := attachments.InsertImportedAvailableAttachments(
			ctx, tx, "default", committed.Result.RecordID, committed.ImportedAttachments,
		); err != nil {
			switch {
			case errors.Is(err, attachments.ErrAttachmentConflict),
				errors.Is(err, attachments.ErrInvalidAttachmentCommand),
				errors.Is(err, attachments.ErrInvalidAttachmentReferences):
				return fmt.Errorf("%w: %w", records.ErrInvalidRevisionCommand, err)
			default:
				return fmt.Errorf("insert imported revision attachments: %w", err)
			}
		}
	}
	err := attachments.ApplyRevisionAttachments(ctx, tx, attachments.RevisionAttachmentCommit{
		ProjectID:     "default",
		RecordID:      committed.Result.RecordID,
		RevisionID:    committed.Result.RevisionID,
		DraftID:       committed.DraftID,
		AttachmentIDs: committed.Input.AttachmentIDs(),
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, attachments.ErrAttachmentConflict),
		errors.Is(err, attachments.ErrInvalidAttachmentCommand),
		errors.Is(err, attachments.ErrInvalidAttachmentReferences):
		return fmt.Errorf("%w: %w", records.ErrInvalidRevisionCommand, err)
	default:
		return fmt.Errorf("apply record revision attachments: %w", err)
	}
}

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

func TestRecordAttachmentRevisionParticipantAdaptsRecordsCommitAndMapsValidationFailure(t *testing.T) {
	t.Parallel()

	participant := NewRecordAttachmentRevisionParticipant()
	if participant.Name() != "attachments" {
		t.Fatalf("Name() = %q", participant.Name())
	}
	err := participant.ApplyRevision(context.Background(), missingRecordAttachmentTx{}, records.RevisionCommitted{
		DraftID: "rdf_publish",
		Result: records.RevisionCommitResult{
			RecordID: "rec_publish", RevisionID: "rrv_publish",
		},
		Input: mustStoreAttachmentRevisionInput(t, []string{"att_missing"}),
	})
	if !errors.Is(err, records.ErrInvalidRevisionCommand) || !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("ApplyRevision() error = %v, want Records validation and attachment conflict", err)
	}
}

func TestRecordAttachmentRevisionParticipantDoesNotMisclassifyDatabaseFailureAsSemanticInput(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database unavailable")
	participant := NewRecordAttachmentRevisionParticipant()
	err := participant.ApplyRevision(context.Background(), failingRecordAttachmentTx{err: wantErr}, records.RevisionCommitted{
		DraftID: "rdf_publish",
		Result: records.RevisionCommitResult{
			RecordID: "rec_publish", RevisionID: "rrv_publish",
		},
		Input: mustStoreAttachmentRevisionInput(t, []string{"att_databasefailure"}),
	})
	if !errors.Is(err, wantErr) || errors.Is(err, records.ErrInvalidRevisionCommand) {
		t.Fatalf("ApplyRevision() error = %v, want database cause without semantic classification", err)
	}
}

type missingRecordAttachmentTx struct{ pgx.Tx }

func (missingRecordAttachmentTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRecordReadRow{err: pgx.ErrNoRows}
}

type failingRecordAttachmentTx struct {
	pgx.Tx
	err error
}

func (tx failingRecordAttachmentTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRecordReadRow{err: tx.err}
}

func mustStoreAttachmentRevisionInput(t *testing.T, attachmentIDs []string) records.CompleteRevisionInput {
	t.Helper()
	return recordsPostgresCompleteRevisionInput(t, "Attachment participant", attachmentIDs...)
}

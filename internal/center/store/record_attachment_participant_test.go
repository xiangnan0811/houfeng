package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

func TestRecordAttachmentRevisionParticipantInsertsImportedThenBinds(t *testing.T) {
	t.Parallel()

	payload := []byte("hello notes\n")
	digest := sha256.Sum256(payload)
	item := attachments.ImportedAvailableAttachment{
		AttachmentID:     "att_imported00001",
		DisplayName:      "notes.txt",
		MediaType:        "text/plain",
		LogicalSizeBytes: int64(len(payload)),
		Object: attachments.ObjectVersion{
			Key: "sha256/" + hex.EncodeToString(digest[:]), VersionID: "import-v1",
			SHA256: digest, SizeBytes: int64(len(payload)),
		},
		BackendKind: attachments.BackendKindLocal,
		CreatedBy:   "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if item.Validate() != nil {
		t.Fatalf("fixture Validate() = %v", item.Validate())
	}
	tx := &importedAttachmentParticipantTx{owners: map[string]revisionAttachmentOwner{}}
	err := NewRecordAttachmentRevisionParticipant().ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result:              records.RevisionCommitResult{RecordID: "rec_imported01", RevisionID: "rrv_imported01"},
		Input:               recordsPostgresCompleteRevisionInput(t, "Imported notes", item.AttachmentID),
		ImportedAttachments: []attachments.ImportedAvailableAttachment{item},
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if !tx.insertedBlob || !tx.insertedAttachment {
		t.Fatalf("inserts blob=%t attachment=%t", tx.insertedBlob, tx.insertedAttachment)
	}
	if len(tx.refs) != 1 || tx.refs[0].attachmentID != item.AttachmentID || tx.refs[0].recordID != "rec_imported01" {
		t.Fatalf("revision refs = %#v", tx.refs)
	}
}

type importedAttachmentParticipantTx struct {
	pgx.Tx
	owners             map[string]revisionAttachmentOwner
	insertedBlob       bool
	insertedAttachment bool
	refs               []revisionAttachmentRef
}

type revisionAttachmentOwner struct {
	recordID string
	draftID  string
	state    attachments.UploadState
}

type revisionAttachmentRef struct {
	recordID     string
	revisionID   string
	ordinal      int64
	attachmentID string
}

func (tx *importedAttachmentParticipantTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	attachmentID, _ := args[1].(string)
	owner, ok := tx.owners[attachmentID]
	if !ok {
		return importedAttachmentRow{err: pgx.ErrNoRows}
	}
	return importedAttachmentRow{scan: func(dest ...any) error {
		if owner.recordID != "" {
			value := owner.recordID
			*(dest[0].(**string)) = &value
		}
		if owner.draftID != "" {
			value := owner.draftID
			*(dest[1].(**string)) = &value
		}
		*(dest[2].(*string)) = string(owner.state)
		return nil
	}}
}

func (tx *importedAttachmentParticipantTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "insert into public.blob_objects"):
		tx.insertedBlob = true
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(compact, "insert into public.record_attachments"):
		id, _ := args[0].(string)
		recordID, _ := args[2].(string)
		tx.insertedAttachment = true
		tx.owners[id] = revisionAttachmentOwner{recordID: recordID, state: attachments.UploadStateAvailable}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(compact, "insert into public.record_revision_attachments"):
		tx.refs = append(tx.refs, revisionAttachmentRef{
			recordID: args[0].(string), revisionID: args[1].(string),
			ordinal: args[2].(int64), attachmentID: args[3].(string),
		})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, errors.New("unexpected imported attachment participant SQL")
	}
}

type importedAttachmentRow struct {
	scan func(...any) error
	err  error
}

func (row importedAttachmentRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return row.scan(dest...)
}

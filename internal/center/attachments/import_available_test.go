package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestInsertImportedAvailableAttachmentsWritesBlobAndRow(t *testing.T) {
	t.Parallel()

	item := mustImportedAvailableAttachment(t, "att_imported00001", "notes.txt", []byte("hello notes\n"))
	tx := &importedAvailableTx{}
	if err := InsertImportedAvailableAttachments(context.Background(), tx, "default", "rec_imported01", []ImportedAvailableAttachment{item}); err != nil {
		t.Fatalf("InsertImportedAvailableAttachments() error = %v", err)
	}
	if len(tx.blobs) != 1 || tx.blobs[0] != item.Object.Key {
		t.Fatalf("blob inserts = %#v", tx.blobs)
	}
	if len(tx.attachments) != 1 || tx.attachments[0].id != item.AttachmentID ||
		tx.attachments[0].recordID != "rec_imported01" || tx.attachments[0].state != string(UploadStateAvailable) {
		t.Fatalf("attachment inserts = %#v", tx.attachments)
	}
}

func TestImportedAvailableAttachmentValidateRejectsArchiveClaims(t *testing.T) {
	t.Parallel()

	item := mustImportedAvailableAttachment(t, "att_imported00001", "notes.txt", []byte("hello notes\n"))
	item.MediaType = ""
	if item.Validate() == nil {
		t.Fatal("empty media type validated")
	}
}

func mustImportedAvailableAttachment(t *testing.T, attachmentID, displayName string, payload []byte) ImportedAvailableAttachment {
	t.Helper()
	digest := sha256.Sum256(payload)
	item := ImportedAvailableAttachment{
		AttachmentID:     attachmentID,
		DisplayName:      displayName,
		MediaType:        "text/plain",
		LogicalSizeBytes: int64(len(payload)),
		Object: ObjectVersion{
			Key: "sha256/" + hex.EncodeToString(digest[:]), VersionID: "import-v1",
			SHA256: digest, SizeBytes: int64(len(payload)),
		},
		BackendKind: BackendKindLocal,
		CreatedBy:   "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if item.Validate() != nil {
		t.Fatalf("fixture Validate() = %v", item.Validate())
	}
	return item
}

type importedAvailableTx struct {
	pgx.Tx
	blobs       []string
	attachments []importedAvailableRow
}

type importedAvailableRow struct {
	id       string
	recordID string
	state    string
}

func (tx *importedAvailableTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "insert into public.blob_objects"):
		key, _ := args[0].(string)
		tx.blobs = append(tx.blobs, key)
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(compact, "insert into public.record_attachments"):
		id, _ := args[0].(string)
		recordID, _ := args[2].(string)
		state := fmt.Sprint(args[3])
		tx.attachments = append(tx.attachments, importedAvailableRow{id: id, recordID: recordID, state: state})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, errors.New("unexpected imported attachment SQL")
	}
}

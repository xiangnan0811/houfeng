package attachments

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestApplyRevisionAttachmentsTransfersExactDraftOwnedAvailableRowsAndPreservesOrder(t *testing.T) {
	t.Parallel()

	tx := &revisionAttachmentTx{owners: map[string]revisionAttachmentOwner{
		"att_draftowned":  {draftID: "rdf_publish", state: UploadStateAvailable},
		"att_recordowned": {recordID: "rec_publish", state: UploadStateAvailable},
	}}
	err := ApplyRevisionAttachments(context.Background(), tx, RevisionAttachmentCommit{
		ProjectID:     "default",
		RecordID:      "rec_publish",
		RevisionID:    "rrv_publish",
		DraftID:       "rdf_publish",
		AttachmentIDs: []string{"att_draftowned", "att_recordowned"},
	})
	if err != nil {
		t.Fatalf("ApplyRevisionAttachments() error = %v", err)
	}
	if got := tx.owners["att_draftowned"]; got.recordID != "rec_publish" || got.draftID != "" {
		t.Fatalf("transferred owner = %#v", got)
	}
	if !reflect.DeepEqual(tx.refs, []revisionAttachmentRef{
		{recordID: "rec_publish", revisionID: "rrv_publish", ordinal: 0, attachmentID: "att_draftowned"},
		{recordID: "rec_publish", revisionID: "rrv_publish", ordinal: 1, attachmentID: "att_recordowned"},
	}) {
		t.Fatalf("revision refs = %#v", tx.refs)
	}
}

func TestApplyRevisionAttachmentsRejectsForeignAndUnavailableRowsBeforeReferenceInsert(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		owner revisionAttachmentOwner
	}{
		{name: "foreign draft", owner: revisionAttachmentOwner{draftID: "rdf_foreign", state: UploadStateAvailable}},
		{name: "foreign record", owner: revisionAttachmentOwner{recordID: "rec_foreign", state: UploadStateAvailable}},
		{name: "pending", owner: revisionAttachmentOwner{draftID: "rdf_publish", state: UploadStateQuarantined}},
		{name: "expired", owner: revisionAttachmentOwner{draftID: "rdf_publish", state: UploadStateExpired}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &revisionAttachmentTx{owners: map[string]revisionAttachmentOwner{"att_candidate": tt.owner}}
			err := ApplyRevisionAttachments(context.Background(), tx, RevisionAttachmentCommit{
				ProjectID:     "default",
				RecordID:      "rec_publish",
				RevisionID:    "rrv_publish",
				DraftID:       "rdf_publish",
				AttachmentIDs: []string{"att_candidate"},
			})
			if !errors.Is(err, ErrAttachmentConflict) {
				t.Fatalf("ApplyRevisionAttachments() error = %v, want ErrAttachmentConflict", err)
			}
			if len(tx.refs) != 0 {
				t.Fatalf("revision refs = %#v, want none", tx.refs)
			}
		})
	}
}

type revisionAttachmentOwner struct {
	recordID string
	draftID  string
	state    UploadState
}

type revisionAttachmentRef struct {
	recordID     string
	revisionID   string
	ordinal      int64
	attachmentID string
}

type revisionAttachmentTx struct {
	pgx.Tx
	owners map[string]revisionAttachmentOwner
	refs   []revisionAttachmentRef
}

func (tx *revisionAttachmentTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	attachmentID, _ := args[1].(string)
	owner, ok := tx.owners[attachmentID]
	if !ok {
		return revisionAttachmentRow{err: pgx.ErrNoRows}
	}
	return revisionAttachmentRow{scan: func(dest ...any) error {
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

func (tx *revisionAttachmentTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.HasPrefix(compact, "update public.record_attachments"):
		attachmentID := args[1].(string)
		owner := tx.owners[attachmentID]
		owner.recordID = args[2].(string)
		owner.draftID = ""
		tx.owners[attachmentID] = owner
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(compact, "insert into public.record_revision_attachments"):
		tx.refs = append(tx.refs, revisionAttachmentRef{
			recordID: args[0].(string), revisionID: args[1].(string),
			ordinal: args[2].(int64), attachmentID: args[3].(string),
		})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, errors.New("unexpected revision attachment SQL")
	}
}

type revisionAttachmentRow struct {
	scan func(...any) error
	err  error
}

func (row revisionAttachmentRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return row.scan(dest...)
}

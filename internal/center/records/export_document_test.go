package records

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"houfeng/internal/center/recordauth"
)

func TestApplicationExportDocumentUsesCurrentOrHistoricalRevision(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	currentInput := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	historicalValues := validCompleteRevisionValues(t)
	historicalValues.Title = "historical title"
	historicalValues.BodyMarkdown = "historical body\n"
	historicalValues.EvidenceSnapshotIDs = []string{testRecordEvidenceID1}
	historicalInput := mustCompleteRevisionInput(t, historicalValues)
	read := &recordApplicationReadStub{
		getRecord: func(_ context.Context, request RecordGetRequest) (Record, error) {
			if request.RecordID != "rec_exportdoc" || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("GetRecord() request = %#v", request)
			}
			return Record{
				RecordID:           request.RecordID,
				ProjectID:          recordauth.ProjectIDDefault,
				CurrentRevisionID:  "rrv_currentexport",
				LockVersion:        4,
				AuthorizationEpoch: 9,
				Current: RecordRevision{
					RecordID:   request.RecordID,
					RevisionID: "rrv_currentexport",
					Input:      currentInput,
				},
			}, nil
		},
		getRevision: func(_ context.Context, request RecordRevisionGetRequest) (RecordRevision, error) {
			if request.RevisionID != "rrv_historicalexport" {
				t.Fatalf("GetRevision() request = %#v", request)
			}
			return RecordRevision{
				RecordID:   request.RecordID,
				RevisionID: request.RevisionID,
				Input:      historicalInput,
			}, nil
		},
	}
	application := mustRecordApplication(t, read, &recordApplicationDraftStub{})

	current, err := application.ExportDocument(context.Background(), ExportDocumentRequest{
		Actor: actor, RecordID: "rec_exportdoc",
	})
	if err != nil {
		t.Fatalf("ExportDocument(current) error = %v", err)
	}
	if current.RevisionID != "rrv_currentexport" || current.Title != currentInput.Title() ||
		current.AuthorizationEpoch != 9 || current.LockVersion != 4 {
		t.Fatalf("current document = %#v", current)
	}

	historical, err := application.ExportDocument(context.Background(), ExportDocumentRequest{
		Actor: actor, RecordID: "rec_exportdoc", RevisionID: "rrv_historicalexport",
	})
	if err != nil {
		t.Fatalf("ExportDocument(historical) error = %v", err)
	}
	if historical.RevisionID != "rrv_historicalexport" || historical.Title != "historical title" ||
		historical.BodyMarkdown != "historical body\n" ||
		!reflect.DeepEqual(historical.EvidenceSnapshotIDs, []string{testRecordEvidenceID1}) {
		t.Fatalf("historical document = %#v", historical)
	}
}

func TestApplicationExportDocumentPropagatesDeniedReads(t *testing.T) {
	t.Parallel()

	application := mustRecordApplication(t, &recordApplicationReadStub{
		getRecord: func(context.Context, RecordGetRequest) (Record, error) {
			return Record{}, recordauth.ErrDenied
		},
	}, &recordApplicationDraftStub{})
	if _, err := application.ExportDocument(context.Background(), ExportDocumentRequest{
		Actor: mustRecordActor(t), RecordID: "rec_deniedexport",
	}); !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("ExportDocument() error = %v, want ErrDenied", err)
	}
}

package records

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestImportDocumentRejectsImportedAuthorizationAndUsesLocalActor(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	var saved RevisionSaveRequest
	revisions := &recordApplicationRevisionStub{save: func(_ context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
		saved = request
		return RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_imported", Created: true}, nil
	}}
	application, err := NewApplication(
		&recordApplicationReadStub{},
		revisions,
		&recordApplicationLifecycleStub{},
		&recordApplicationDraftStub{},
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}
	if _, err := application.ImportDocument(context.Background(), ImportDocumentRequest{
		Actor: actor, RecordID: "rec_imported01", Title: "Disk notes",
		BodyMarkdown: "# Body\n", IdempotencyKey: "import-1",
		ImportedAuthorization: "admin",
	}); !errors.Is(err, ErrUntrustedImportIdentity) {
		t.Fatalf("ImportDocument(auth) error = %v, want ErrUntrustedImportIdentity", err)
	}
	got, err := application.ImportDocument(context.Background(), ImportDocumentRequest{
		Actor: actor, RecordID: "rec_imported01", Title: "Disk notes",
		BodyMarkdown: "# Body\n", IdempotencyKey: "import-1",
	})
	if err != nil {
		t.Fatalf("ImportDocument() error = %v", err)
	}
	if got.RecordID != "rec_imported01" || got.RevisionID != "rrv_imported" {
		t.Fatalf("ImportDocument() = %#v", got)
	}
	if saved.Values.OwnerID != actor.UserID || saved.Values.AuthorID != actor.UserID ||
		saved.Values.RecordType != RecordTypeNote || saved.ActivityKind != DomainActivityRecordCreated {
		t.Fatalf("SaveRevision() = %#v", saved)
	}
}

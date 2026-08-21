package records

import (
	"context"

	"houfeng/internal/center/recordauth"
)

type ImportDocumentRequest struct {
	Actor                 recordauth.ActorScope
	RecordID              string
	Title                 string
	BodyMarkdown          string
	IdempotencyKey        string
	ImportedAuthorization string
	ImportedRole          string
}

type ImportedDocument struct {
	RecordID   string
	RevisionID string
}

type RevisionCommitFinish struct {
	OriginKind     string
	OriginDigest   [32]byte
	SourceRecord   string
	ImportJobID    string
	JobLockVersion uint64
	ActorID        string
}

func (finish RevisionCommitFinish) Validate() error {
	if finish.OriginKind == "" || finish.OriginDigest == [32]byte{} ||
		finish.ImportJobID == "" || finish.JobLockVersion == 0 || finish.ActorID == "" {
		return ErrInvalidRevisionCommand
	}
	return nil
}

func ImportedRevisionValues(actor recordauth.ActorScope, title, body string) (CompleteRevisionValues, error) {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      actor.ProjectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		return CompleteRevisionValues{}, err
	}
	return CompleteRevisionValues{
		Title:                  title,
		BodyMarkdown:           body,
		MarkdownDialectVersion: MarkdownDialectVersionV1,
		RecordType:             RecordTypeNote,
		ImpactLevel:            ImpactLevel("normal"),
		VisibilityScope:        visibility,
		OwnerID:                actor.UserID,
		AuthorID:               actor.UserID,
		SaveReason:             "imported archive",
	}, nil
}

func (application *Application) ImportDocument(
	ctx context.Context,
	request ImportDocumentRequest,
) (ImportedDocument, error) {
	written, err := application.ImportDocuments(ctx, []ImportDocumentRequest{request})
	if err != nil {
		return ImportedDocument{}, err
	}
	if len(written) != 1 {
		return ImportedDocument{}, ErrInvalidApplicationRequest
	}
	return written[0], nil
}

func (application *Application) ImportDocuments(
	ctx context.Context,
	requests []ImportDocumentRequest,
) ([]ImportedDocument, error) {
	if ctx == nil || application == nil || len(requests) == 0 {
		return nil, ErrInvalidApplicationRequest
	}
	saves := make([]RevisionSaveRequest, 0, len(requests))
	for _, request := range requests {
		if request.ImportedAuthorization != "" || request.ImportedRole != "" {
			return nil, ErrUntrustedImportIdentity
		}
		values, err := ImportedRevisionValues(request.Actor, request.Title, request.BodyMarkdown)
		if err != nil {
			return nil, err
		}
		saves = append(saves, RevisionSaveRequest{
			Actor:              request.Actor,
			RecordID:           request.RecordID,
			Values:             values,
			ActivityKind:       DomainActivityRecordCreated,
			IdempotencyKey:     request.IdempotencyKey,
			IdempotencyOwnerID: application.options.IdempotencyOwnerID,
			OwnerLeaseDuration: application.options.OwnerLeaseDuration,
			IdempotencyTTL:     application.options.IdempotencyTTL,
			OutboxTTL:          application.options.OutboxTTL,
		})
	}
	results, err := application.revisions.SaveRevisions(ctx, saves)
	if err != nil {
		return nil, err
	}
	return importedDocumentsFromResults(results), nil
}

func (application *Application) ImportDocumentsFinishing(
	ctx context.Context,
	requests []ImportDocumentRequest,
	finish RevisionCommitFinish,
) ([]ImportedDocument, error) {
	if ctx == nil || application == nil || len(requests) == 0 || finish.Validate() != nil {
		return nil, ErrInvalidApplicationRequest
	}
	saves := make([]RevisionSaveRequest, 0, len(requests))
	for _, request := range requests {
		if request.ImportedAuthorization != "" || request.ImportedRole != "" {
			return nil, ErrUntrustedImportIdentity
		}
		values, err := ImportedRevisionValues(request.Actor, request.Title, request.BodyMarkdown)
		if err != nil {
			return nil, err
		}
		saves = append(saves, RevisionSaveRequest{
			Actor:              request.Actor,
			RecordID:           request.RecordID,
			Values:             values,
			ActivityKind:       DomainActivityRecordCreated,
			IdempotencyKey:     request.IdempotencyKey,
			IdempotencyOwnerID: application.options.IdempotencyOwnerID,
			OwnerLeaseDuration: application.options.OwnerLeaseDuration,
			IdempotencyTTL:     application.options.IdempotencyTTL,
			OutboxTTL:          application.options.OutboxTTL,
		})
	}
	results, err := application.revisions.SaveRevisionsFinishing(ctx, saves, finish)
	if err != nil {
		return nil, err
	}
	return importedDocumentsFromResults(results), nil
}

func importedDocumentsFromResults(results []RevisionCommitResult) []ImportedDocument {
	written := make([]ImportedDocument, 0, len(results))
	for _, result := range results {
		written = append(written, ImportedDocument{RecordID: result.RecordID, RevisionID: result.RevisionID})
	}
	return written
}

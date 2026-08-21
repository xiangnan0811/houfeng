package records

import (
	"context"

	"houfeng/internal/center/recordauth"
)

// ExportDocument is the allowlisted revision view Child 10 markdown export
// consumes. New export methods stay here so portability never reads store rows.
type ExportDocument struct {
	RecordID            string
	RevisionID          string
	Title               string
	BodyMarkdown        string
	AuthorizationEpoch  uint64
	LockVersion         uint64
	EvidenceSnapshotIDs []string
	AttachmentIDs       []string
}

type ExportDocumentRequest struct {
	Actor      recordauth.ActorScope
	RecordID   string
	RevisionID string
}

func (application *Application) ExportDocument(
	ctx context.Context,
	request ExportDocumentRequest,
) (ExportDocument, error) {
	if ctx == nil || application == nil {
		return ExportDocument{}, ErrInvalidApplicationRequest
	}
	record, err := application.GetRecord(ctx, RecordGetRequest{Actor: request.Actor, RecordID: request.RecordID})
	if err != nil {
		return ExportDocument{}, err
	}
	revision := record.Current
	if request.RevisionID != "" && request.RevisionID != record.CurrentRevisionID {
		revision, err = application.GetRevision(ctx, RecordRevisionGetRequest{
			Actor: request.Actor, RecordID: request.RecordID, RevisionID: request.RevisionID,
		})
		if err != nil {
			return ExportDocument{}, err
		}
	}
	if revision.RecordID != record.RecordID || revision.RevisionID == "" {
		return ExportDocument{}, ErrInvalidApplicationRequest
	}
	return ExportDocument{
		RecordID:            record.RecordID,
		RevisionID:          revision.RevisionID,
		Title:               revision.Input.Title(),
		BodyMarkdown:        revision.Input.BodyMarkdown(),
		AuthorizationEpoch:  record.AuthorizationEpoch,
		LockVersion:         record.LockVersion,
		EvidenceSnapshotIDs: revision.Input.EvidenceSnapshotIDs(),
		AttachmentIDs:       revision.Input.AttachmentIDs(),
	}, nil
}

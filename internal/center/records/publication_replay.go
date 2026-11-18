package records

import (
	"crypto/sha256"
	"fmt"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
)

// CompletedCreateCandidate is a content-free hint from a completed create
// claim. It does not grant permission to read the record or its revision.
type CompletedCreateCandidate struct {
	RecordID           string
	RevisionID         string
	RequestFingerprint [32]byte
}

// CompletedCreateFingerprint reconstructs the original legacy create request
// fingerprint from an already-authorized first revision and the submitted draft
// binding. It cannot authenticate the original transport body or consumed
// capture-intent IDs.
func CompletedCreateFingerprint(actor recordauth.ActorScope, draftID string, etag DraftETag, revision RecordRevision) ([32]byte, error) {
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil || normalized.UserID != revision.Input.AuthorID() {
		return [32]byte{}, fmt.Errorf("%w: create replay actor", ErrInvalidRevisionServiceRequest)
	}
	if _, err := etag.Digest(); err != nil || !validDraftID(draftID) ||
		recordidentity.Classify(revision.RecordID) != recordidentity.Legacy ||
		!validRevisionID(revision.RevisionID) || revision.RevisionNo != 1 || revision.BaseRevisionID != "" ||
		revision.Input.Title() == "" || revision.Input.CanonicalHash() == [sha256.Size]byte{} ||
		revision.Input.visibilityScope.ProjectID != normalized.ProjectID {
		return [32]byte{}, fmt.Errorf("%w: create replay revision or draft", ErrInvalidRevisionServiceRequest)
	}

	request := RevisionSaveRequest{
		RecordID: revision.RecordID, DraftID: draftID, DraftETag: etag,
		ActivityKind: DomainActivityRecordCreated,
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordCreate,
		ProjectID:          recordplatform.ProjectID(normalized.ProjectID),
		ActorScopeDigest:   normalized.CanonicalHash(),
		RequestScopeDigest: revisionRequestScopeDigest(request),
		PayloadDigest:      revisionCommitPayloadDigest(revision.Input),
	})
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: create replay fingerprint", ErrInvalidRevisionServiceRequest)
	}
	return fingerprint.PersistedBytes()
}

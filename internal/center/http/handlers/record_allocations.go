package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type recordAllocationRepository interface {
	GetAllocation(context.Context, recordauth.ActorScope, string) (recordbirth.Allocation, error)
	BindPublication(context.Context, recordauth.ActorScope, string, string, [32]byte) (recordbirth.Allocation, error)
	LookupPublication(context.Context, recordauth.ActorScope, string, [32]byte) (recordbirth.Allocation, bool, error)
}

type recordCreateReplayLookup func(context.Context, recordauth.ActorScope, string) (records.CompletedCreateCandidate, bool, error)

type recordComparisonReplayService interface {
	VerifyComparisonSaveReplay(context.Context, recordauth.ActorScope, string, string) ([]string, error)
}

// This transport is an exact reference to a server-owned allocation. None of
// its fields grants authority; publication reloads and compares the durable row.
type recordAllocationResponse struct {
	AllocationID   string              `json:"allocation_id"`
	ReservationID  string              `json:"reservation_id"`
	RecordID       string              `json:"record_id"`
	Purpose        recordbirth.Purpose `json:"purpose"`
	RequestDigest  string              `json:"request_digest"`
	Generation     uint64              `json:"generation"`
	IdempotencyKey string              `json:"idempotency_key"`
}

func newRecordAllocationResponse(allocation recordbirth.Allocation) recordAllocationResponse {
	return recordAllocationResponse{
		AllocationID: allocation.AllocationID, ReservationID: allocation.ReservationID,
		RecordID: allocation.RecordID, Purpose: allocation.Request.Purpose,
		RequestDigest: hex.EncodeToString(allocation.Request.RequestFingerprint[:]),
		Generation:    allocation.Binding.Generation, IdempotencyKey: allocation.Request.IdempotencyKey,
	}
}

func handleRecordAllocations(w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope, options RecordHandlerOptions) {
	if request.Method != http.MethodPost {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	key, ok := recordIdempotencyKey(request)
	if !ok {
		writeRecordBirthError(w, recordbirth.ErrInvalid)
		return
	}
	var input struct {
		Purpose       recordbirth.Purpose `json:"purpose"`
		RequestDigest string              `json:"request_digest"`
	}
	if !decodeRecordsRequestJSON(w, request, &input) {
		return
	}
	if input.Purpose != recordbirth.PurposeCreate && input.Purpose != recordbirth.PurposeEvidence && input.Purpose != recordbirth.PurposeComparison {
		writeRecordBirthError(w, recordbirth.ErrInvalid)
		return
	}
	digest, err := parseRecordAllocationDigest(input.RequestDigest)
	if err != nil {
		writeRecordBirthError(w, err)
		return
	}
	allocationRequest, err := newRecordAllocationRequest(actor, input.Purpose, key, digest)
	if err != nil {
		writeRecordBirthError(w, err)
		return
	}
	if options.Birth == nil {
		writeRecordBirthError(w, recordbirth.ErrUnavailable)
		return
	}
	allocation, err := options.Birth.Reserve(request.Context(), allocationRequest)
	if err != nil {
		writeRecordBirthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newRecordAllocationResponse(allocation))
}

func newRecordAllocationRequest(actor recordauth.ActorScope, purpose recordbirth.Purpose, key string, digest [32]byte) (recordbirth.Request, error) {
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return recordbirth.Request{}, err
	}
	if err := recordauth.AllowsCapability(normalized, recordauth.CapabilityRecordCreate); err != nil {
		return recordbirth.Request{}, err
	}
	result := recordbirth.Request{
		ProjectID: string(normalized.ProjectID), ActorID: normalized.UserID, ActorScopeDigest: normalized.CanonicalHash(),
		Purpose: purpose, IdempotencyKey: key, RequestFingerprint: digest,
	}
	return result, result.Validate()
}

func parseRecordAllocationDigest(value string) ([32]byte, error) {
	var result [32]byte
	if len(value) != 64 {
		return result, recordbirth.ErrInvalid
	}
	for index := range value {
		ch := value[index]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return result, recordbirth.ErrInvalid
		}
	}
	if _, err := hex.Decode(result[:], []byte(value)); err != nil || result == ([32]byte{}) {
		return [32]byte{}, recordbirth.ErrInvalid
	}
	return result, nil
}

func recordPublicationDigest(input recordPublishRequest) ([32]byte, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return [32]byte{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("houfeng.records.publication.v1\x00"))
	_, _ = hash.Write(canonical)
	var digest [32]byte
	hash.Sum(digest[:0])
	return digest, nil
}

// prepareRecordCreatePublication runs before draft or evidence preparation. A
// committed response is authorized and returned here, so replay has no content
// I/O and does not depend on a draft or intent that successful creation consumed.
func prepareRecordCreatePublication(
	w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope,
	application recordHandlerApplication, options RecordHandlerOptions,
	input recordPublishRequest, key string,
) (recordbirth.Allocation, bool) {
	if input.ComparisonIntent != "" && len(input.EvidenceItems) != 0 {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "comparison save cannot include client evidence items", nil)
		return recordbirth.Allocation{}, false
	}
	if options.Birth == nil || options.Allocations == nil || options.CreateReplay == nil {
		writeRecordBirthError(w, recordbirth.ErrUnavailable)
		return recordbirth.Allocation{}, false
	}
	digest, err := recordPublicationDigest(input)
	if err != nil {
		writeRecordInternalError(w)
		return recordbirth.Allocation{}, false
	}
	allocation, found, err := options.Allocations.LookupPublication(request.Context(), actor, key, digest)
	if err != nil {
		writeRecordBirthError(w, err)
		return recordbirth.Allocation{}, false
	}
	if found {
		if allocation.Validate() != nil {
			writeRecordBirthError(w, recordbirth.ErrUnavailable)
			return recordbirth.Allocation{}, false
		}
		switch allocation.State {
		case recordbirth.StateFinished, recordbirth.StateConfirmed:
			writeCompletedRecordPublication(w, request, actor, application, options, allocation)
			return recordbirth.Allocation{}, false
		case recordbirth.StateTombstoned:
			writeRecordNotFound(w)
			return recordbirth.Allocation{}, false
		case recordbirth.StateConsumed, recordbirth.StatePending:
			writeRecordBirthError(w, recordbirth.ErrPending)
			return recordbirth.Allocation{}, false
		}
	} else {
		candidate, completed, err := options.CreateReplay(request.Context(), actor, key)
		if err != nil {
			writeRecordBirthError(w, err)
			return recordbirth.Allocation{}, false
		}
		if completed {
			writeLegacyCompletedRecordPublication(w, request, actor, application, options, input, candidate)
			return recordbirth.Allocation{}, false
		}
		if input.Allocation != nil {
			allocation, err = options.Allocations.GetAllocation(request.Context(), actor, input.Allocation.AllocationID)
		} else if input.RecordID == "" && input.ComparisonIntent == "" && len(input.EvidenceItems) == 0 {
			var allocationRequest recordbirth.Request
			allocationRequest, err = newRecordAllocationRequest(actor, recordbirth.PurposeCreate, key, digest)
			if err == nil {
				allocation, err = options.Birth.Reserve(request.Context(), allocationRequest)
			}
		} else {
			err = recordbirth.ErrInvalid
		}
		if err != nil {
			writeRecordBirthError(w, err)
			return recordbirth.Allocation{}, false
		}
	}
	if err := validatePublicationAllocation(actor, input, allocation); err != nil {
		writeRecordBirthError(w, err)
		return recordbirth.Allocation{}, false
	}
	allocation, err = options.Allocations.BindPublication(request.Context(), actor, allocation.AllocationID, key, digest)
	if err != nil {
		writeRecordBirthError(w, err)
		return recordbirth.Allocation{}, false
	}
	return allocation, true
}

func validatePublicationAllocation(actor recordauth.ActorScope, input recordPublishRequest, allocation recordbirth.Allocation) error {
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil || allocation.Validate() != nil || allocation.Request.ActorID != normalized.UserID ||
		allocation.Request.ProjectID != string(normalized.ProjectID) || allocation.Request.ActorScopeDigest != normalized.CanonicalHash() {
		return recordbirth.ErrConflict
	}
	if input.Allocation != nil {
		if newRecordAllocationResponse(allocation) != *input.Allocation || input.RecordID != allocation.RecordID {
			return recordbirth.ErrConflict
		}
	} else if input.RecordID != "" || input.ComparisonIntent != "" || len(input.EvidenceItems) != 0 {
		return recordbirth.ErrInvalid
	}
	if input.ComparisonIntent != "" {
		if len(input.EvidenceItems) != 0 || allocation.Request.Purpose != recordbirth.PurposeComparison {
			return recordbirth.ErrConflict
		}
	} else if allocation.Request.Purpose != recordbirth.PurposeCreate && allocation.Request.Purpose != recordbirth.PurposeEvidence {
		return recordbirth.ErrConflict
	}
	if allocation.State == recordbirth.StateTombstoned || allocation.State == recordbirth.StateConsumed || allocation.State == recordbirth.StatePending {
		return recordbirth.ErrPending
	}
	return nil
}

func writeCompletedRecordPublication(w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope, application recordHandlerApplication, options RecordHandlerOptions, allocation recordbirth.Allocation) {
	revision, err := application.GetRevision(request.Context(), records.RecordRevisionGetRequest{
		Actor: actor, RecordID: allocation.RecordID, RevisionID: allocation.Creation.RevisionID,
	})
	if err != nil {
		writeRecordsApplicationError(w, err)
		return
	}
	if revision.RecordID != allocation.RecordID || revision.RevisionID != allocation.Creation.RevisionID ||
		revision.RevisionNo != 1 || revision.BaseRevisionID != "" || revision.Input.AuthorID() != allocation.Request.ActorID ||
		revision.Input.CanonicalHash() != allocation.Creation.CanonicalHash {
		writeRecordBirthError(w, recordbirth.ErrUnavailable)
		return
	}
	result := completedPublicationResult(revision)
	if allocation.Domain == recordidentity.Certifiable {
		result.CertificationPending = options.Birth.ConfirmCreate(request.Context(), allocation.RecordID) != nil
	}
	writeJSON(w, http.StatusOK, newRecordMutationResponse(result))
}

func completedPublicationResult(revision records.RecordRevision) records.RevisionCommitResult {
	return records.RevisionCommitResult{
		RecordID: revision.RecordID, RevisionID: revision.RevisionID, RevisionNo: 1,
		LockVersion: 1, AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive,
		Created: true, Replayed: true, CommittedAt: revision.CreatedAt,
	}
}

func writeLegacyCompletedRecordPublication(w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope, application recordHandlerApplication, options RecordHandlerOptions, input recordPublishRequest, candidate records.CompletedCreateCandidate) {
	if input.Allocation != nil || recordidentity.Classify(candidate.RecordID) != recordidentity.Legacy ||
		(input.RecordID != "" && input.RecordID != candidate.RecordID) {
		writeRecordBirthError(w, recordbirth.ErrConflict)
		return
	}
	revision, err := application.GetRevision(request.Context(), records.RecordRevisionGetRequest{Actor: actor, RecordID: candidate.RecordID, RevisionID: candidate.RevisionID})
	if err != nil {
		writeRecordsApplicationError(w, err)
		return
	}
	if revision.RecordID != candidate.RecordID || revision.RevisionID != candidate.RevisionID ||
		revision.RevisionNo != 1 || revision.BaseRevisionID != "" || revision.Input.AuthorID() != actor.UserID {
		writeRecordBirthError(w, recordbirth.ErrConflict)
		return
	}
	if input.DraftID == "" && input.DraftETag == "" {
		// The predecessor browser retained only the record, key and comparison
		// identity. This is an authorized lookup of its committed result, never
		// a request to republish or an attestation of missing draft metadata.
		if input.RecordID != candidate.RecordID || input.ComparisonIntent == "" || len(input.EvidenceItems) != 0 {
			writeRecordBirthError(w, recordbirth.ErrInvalid)
			return
		}
	} else {
		etag, err := records.ParseDraftETag(input.DraftETag)
		if err != nil {
			writeRecordBirthError(w, recordbirth.ErrInvalid)
			return
		}
		fingerprint, err := records.CompletedCreateFingerprint(actor, input.DraftID, etag, revision)
		if err != nil || fingerprint != candidate.RequestFingerprint {
			writeRecordBirthError(w, recordbirth.ErrConflict)
			return
		}
	}
	var expected []string
	if input.ComparisonIntent != "" {
		if len(input.EvidenceItems) != 0 || options.ComparisonReplay == nil {
			writeRecordBirthError(w, recordbirth.ErrUnavailable)
			return
		}
		expected, err = options.ComparisonReplay.VerifyComparisonSaveReplay(request.Context(), actor, candidate.RecordID, input.ComparisonIntent)
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
	} else {
		expected = make([]string, 0, len(input.EvidenceItems))
		for _, item := range input.EvidenceItems {
			// Legacy consumption deleted the capture-intent mapping. Neither a
			// key nor the resulting snapshot can retrospectively prove that ID.
			if item.CaptureIntentID != "" || item.ExistingSnapshotID == "" {
				writeRecordBirthError(w, recordbirth.ErrUnavailable)
				return
			}
			expected = append(expected, item.ExistingSnapshotID)
		}
	}
	if !slices.Equal(expected, revision.Input.EvidenceSnapshotIDs()) {
		writeRecordBirthError(w, recordbirth.ErrConflict)
		return
	}
	writeJSON(w, http.StatusOK, newRecordMutationResponse(completedPublicationResult(revision)))
}

func writeRecordBirthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordbirth.ErrInvalid):
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid record allocation or publication", nil)
	case errors.Is(err, recordbirth.ErrConflict), errors.Is(err, recordplatform.ErrIdempotencyKeyReused):
		writeRecordError(w, http.StatusConflict, "idempotency_conflict", "record publication request conflicts with its original binding", nil)
	case errors.Is(err, recordbirth.ErrPending):
		writeRecordError(w, http.StatusServiceUnavailable, "publication_pending", "record publication outcome is pending", nil)
	case errors.Is(err, recordbirth.ErrUnavailable), errors.Is(err, recordbirth.ErrUncertified):
		writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
	default:
		writeRecordsApplicationError(w, err)
	}
}

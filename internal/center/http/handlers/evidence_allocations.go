package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
)

// Existing records are authorized by CapturePreview. A new target must already
// have a durable, actor-bound allocation before an intent can be persisted.
func prepareEvidencePreviewAllocation(w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope, options EvidenceHandlerOptions, input evidenceCapturePreviewInput) (*recordAllocationResponse, string, bool) {
	if err := recordauth.AllowsCapability(actor, recordauth.CapabilityEvidenceCreate); err != nil {
		writeRecordBirthError(w, err)
		return nil, "", false
	}
	if input.Allocation == nil && input.RecordID != "" {
		if !recordidentity.IsValid(input.RecordID) {
			writeRecordBirthError(w, recordbirth.ErrInvalid)
			return nil, "", false
		}
		return nil, input.RecordID, true
	}
	if options.Birth == nil || options.Allocations == nil {
		writeRecordBirthError(w, recordbirth.ErrUnavailable)
		return nil, "", false
	}
	var allocation recordbirth.Allocation
	var err error
	if input.Allocation != nil {
		allocation, err = options.Allocations.GetAllocation(request.Context(), actor, input.Allocation.AllocationID)
	} else {
		key, ok := recordIdempotencyKey(request)
		if !ok {
			writeRecordBirthError(w, recordbirth.ErrInvalid)
			return nil, "", false
		}
		canonical, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			writeRecordInternalError(w)
			return nil, "", false
		}
		digest := sha256.Sum256(append([]byte("houfeng.records.evidence-allocation.v1\x00"), canonical...))
		allocationRequest, requestErr := newRecordAllocationRequest(actor, recordbirth.PurposeEvidence, key, digest)
		if requestErr != nil {
			writeRecordBirthError(w, requestErr)
			return nil, "", false
		}
		allocation, err = options.Birth.Reserve(request.Context(), allocationRequest)
	}
	if err != nil {
		writeRecordBirthError(w, err)
		return nil, "", false
	}
	if err := validatePublicationAllocation(actor, recordPublishRequest{Allocation: input.Allocation, RecordID: input.RecordID}, allocation); err != nil {
		writeRecordBirthError(w, err)
		return nil, "", false
	}
	if allocation.State != recordbirth.StateAllocated {
		writeRecordBirthError(w, recordbirth.ErrConflict)
		return nil, "", false
	}
	response := newRecordAllocationResponse(allocation)
	return &response, allocation.RecordID, true
}

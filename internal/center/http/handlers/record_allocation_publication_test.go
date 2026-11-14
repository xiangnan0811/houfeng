package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
)

// handlerBirthMemory is a boundary double for the APP commands. It supplies
// state to the real coordinator; it does not model database durability, locks,
// admission, transaction rollback, or external birth authority.
type handlerBirthMemory struct {
	requests     map[string]recordbirth.Allocation
	allocations  map[string]recordbirth.Allocation
	publications map[string]handlerPublicationBinding
	begins       int
}

type handlerPublicationBinding struct {
	allocationID string
	digest       [32]byte
}

func newHandlerBirthFixture(t *testing.T) (RecordHandlerOptions, *handlerBirthMemory) {
	t.Helper()
	memory := &handlerBirthMemory{
		requests: make(map[string]recordbirth.Allocation), allocations: make(map[string]recordbirth.Allocation),
		publications: make(map[string]handlerPublicationBinding),
	}
	coordinator, err := recordbirth.NewCoordinator(memory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return RecordHandlerOptions{
		Birth: coordinator, Allocations: memory,
		CreateReplay: func(context.Context, recordauth.ActorScope, string) (records.CompletedCreateCandidate, bool, error) {
			return records.CompletedCreateCandidate{}, false, nil
		},
	}, memory
}

func handlerBirthRequestKey(request recordbirth.Request) string {
	return request.ProjectID + "\x00" + request.ActorID + "\x00" + string(request.Purpose) + "\x00" + request.IdempotencyKey
}

func handlerPublicationKey(actor recordauth.ActorScope, key string) string {
	return string(actor.ProjectID) + "\x00" + actor.UserID + "\x00" + key
}

func (memory *handlerBirthMemory) FindAllocation(_ context.Context, request recordbirth.Request) (recordbirth.Allocation, bool, error) {
	allocation, found := memory.requests[handlerBirthRequestKey(request)]
	if found && allocation.Request != request {
		return recordbirth.Allocation{}, false, recordbirth.ErrConflict
	}
	return allocation, found, nil
}

func (*handlerBirthMemory) CurrentBinding(context.Context) (recordbirth.Binding, bool, error) {
	return recordbirth.Binding{}, false, nil
}

func (*handlerBirthMemory) ContinuityBinding(context.Context) (recordbirth.Binding, bool, error) {
	return recordbirth.Binding{}, false, nil
}

func (*handlerBirthMemory) CheckpointProof(context.Context, recordbirth.Request, recordbirth.CheckpointHead) ([]byte, error) {
	return nil, recordbirth.ErrUnavailable
}

func (memory *handlerBirthMemory) BeginAllocation(_ context.Context, request recordbirth.Request, binding *recordbirth.Binding, proof []byte) (recordbirth.Allocation, error) {
	if binding != nil || len(proof) != 0 {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	if allocation, found, err := memory.FindAllocation(context.Background(), request); found || err != nil {
		return allocation, err
	}
	memory.begins++
	identity := fmt.Sprintf("%032x", memory.begins)
	allocation := recordbirth.Allocation{
		AllocationID: "rqa_" + identity, ReservationID: "rba_" + identity, RecordID: "rec2_" + identity,
		Domain: recordidentity.Ordinary, Request: request, State: recordbirth.StateAllocated,
	}
	memory.requests[handlerBirthRequestKey(request)] = allocation
	memory.allocations[allocation.AllocationID] = allocation
	return allocation, nil
}

func (*handlerBirthMemory) RegisterReceipt(context.Context, recordbirth.Request, []byte, []byte) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}

func (*handlerBirthMemory) PrepareAllocation(context.Context, recordbirth.Request, recordbirth.InitialCreation, []byte) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}

func (*handlerBirthMemory) CommittedCreation(context.Context, string) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}

func (*handlerBirthMemory) VerifyCertification(context.Context, string, recordbirth.Observation) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}

func (memory *handlerBirthMemory) GetAllocation(_ context.Context, actor recordauth.ActorScope, id string) (recordbirth.Allocation, error) {
	allocation, found := memory.allocations[id]
	if !found || allocation.Request.ActorID != actor.UserID || allocation.Request.ProjectID != string(actor.ProjectID) {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	return allocation, nil
}

func (memory *handlerBirthMemory) BindPublication(ctx context.Context, actor recordauth.ActorScope, id, key string, digest [32]byte) (recordbirth.Allocation, error) {
	allocation, err := memory.GetAllocation(ctx, actor, id)
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if allocation.Request.ActorScopeDigest != actor.CanonicalHash() {
		return recordbirth.Allocation{}, recordbirth.ErrConflict
	}
	publicationKey := handlerPublicationKey(actor, key)
	if existing, found := memory.publications[publicationKey]; found && (existing.allocationID != id || existing.digest != digest) {
		return recordbirth.Allocation{}, recordbirth.ErrConflict
	}
	memory.publications[publicationKey] = handlerPublicationBinding{allocationID: id, digest: digest}
	return allocation, nil
}

func (memory *handlerBirthMemory) LookupPublication(_ context.Context, actor recordauth.ActorScope, key string, digest [32]byte) (recordbirth.Allocation, bool, error) {
	binding, found := memory.publications[handlerPublicationKey(actor, key)]
	if !found {
		return recordbirth.Allocation{}, false, nil
	}
	if binding.digest != digest {
		return recordbirth.Allocation{}, false, recordbirth.ErrConflict
	}
	return memory.allocations[binding.allocationID], true, nil
}

func (memory *handlerBirthMemory) finishPublication(t *testing.T, actor recordauth.ActorScope, key string, revision records.RecordRevision) {
	t.Helper()
	binding, found := memory.publications[handlerPublicationKey(actor, key)]
	if !found {
		t.Fatal("test publication was not bound")
	}
	allocation := memory.allocations[binding.allocationID]
	allocation.State = recordbirth.StateFinished
	allocation.Creation = recordbirth.InitialCreation{
		ReservationID: allocation.ReservationID, RecordID: allocation.RecordID,
		RevisionID: revision.RevisionID, AuthorID: actor.UserID,
		CanonicalHash: revision.Input.CanonicalHash(), RequestFingerprint: binding.digest,
	}
	if err := allocation.Validate(); err != nil {
		t.Fatalf("invalid completed allocation fixture: %v", err)
	}
	memory.allocations[allocation.AllocationID] = allocation
	memory.requests[handlerBirthRequestKey(allocation.Request)] = allocation
}

func mustHandlerAllocation(t *testing.T, handler http.Handler, actor recordauth.ActorScope, purpose recordbirth.Purpose, key string) recordAllocationResponse {
	t.Helper()
	body := `{"purpose":` + strconvQuote(string(purpose)) + `,"request_digest":"1111111111111111111111111111111111111111111111111111111111111111"}`
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records/allocations", body)
	request.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("allocate: status %d body %s", recorder.Code, recorder.Body.String())
	}
	var allocation recordAllocationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &allocation); err != nil || recordidentity.Classify(allocation.RecordID) != recordidentity.Ordinary {
		t.Fatalf("allocation = %#v, error %v", allocation, err)
	}
	return allocation
}

func handlerAllocationJSON(t *testing.T, allocation recordAllocationResponse) string {
	t.Helper()
	encoded, err := json.Marshal(allocation)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

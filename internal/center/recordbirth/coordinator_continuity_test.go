package recordbirth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"houfeng/internal/center/recordidentity"
)

type coordinatorTestApplication struct {
	binding       Binding
	installed     bool
	enabled       bool
	allocation    Allocation
	found         bool
	hideFirst     bool
	finds         int
	begins        int
	prepares      int
	checkpoints   int
	registers     int
	verifications int
	lastProof     []byte
}

func (app *coordinatorTestApplication) ContinuityBinding(context.Context) (Binding, bool, error) {
	if !app.installed {
		return Binding{}, false, nil
	}
	return app.binding, true, nil
}

func (app *coordinatorTestApplication) CurrentBinding(context.Context) (Binding, bool, error) {
	if !app.enabled {
		return Binding{}, false, nil
	}
	return app.binding, true, nil
}

func (app *coordinatorTestApplication) FindAllocation(_ context.Context, request Request) (Allocation, bool, error) {
	app.finds++
	if app.hideFirst {
		app.hideFirst = false
		return Allocation{}, false, nil
	}
	if app.found && app.allocation.Request != request {
		return Allocation{}, false, ErrConflict
	}
	return app.allocation, app.found, nil
}

func (app *coordinatorTestApplication) BeginAllocation(_ context.Context, request Request, binding *Binding, proof []byte) (Allocation, error) {
	app.begins++
	app.lastProof = bytes.Clone(proof)
	if app.found {
		return app.allocation, nil
	}
	app.allocation = Allocation{
		AllocationID: "rqa_" + strings.Repeat("1", 32), Request: request,
		Domain: recordidentity.Ordinary, State: StateAllocated,
		ReservationID: "rba_" + strings.Repeat("2", 32), RecordID: "rec2_" + strings.Repeat("3", 32),
	}
	if binding != nil {
		app.allocation.Domain = recordidentity.Certifiable
		app.allocation.Binding = *binding
		app.allocation.State = StatePending
		app.allocation.ReservationID, app.allocation.RecordID = "", ""
	}
	app.found = true
	return app.allocation, nil
}

func coordinatorTestSign(canonical []byte) []byte {
	key := bytes.Repeat([]byte{0x72}, sha256.Size)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return append(canonical, mac.Sum(nil)...)
}

func (app *coordinatorTestApplication) CheckpointProof(_ context.Context, request Request, expected CheckpointHead) ([]byte, error) {
	app.checkpoints++
	checkpoint := Checkpoint{
		Binding: app.allocation.Binding, Sequence: 1, AllocationID: app.allocation.AllocationID, Request: request,
	}
	hash, err := checkpoint.Hash()
	if err != nil {
		return nil, err
	}
	proof := CheckpointProof{Binding: app.binding, ExpectedAnchor: expected,
		ProposedHead: CheckpointHead{Sequence: 1, Hash: hash}, Checkpoint: checkpoint}
	canonical, err := EncodeCheckpointProof(proof)
	if err != nil {
		return nil, err
	}
	return coordinatorTestSign(canonical), nil
}

func (app *coordinatorTestApplication) RegisterReceipt(_ context.Context, request Request, raw, proof []byte) (Allocation, error) {
	app.registers++
	app.lastProof = bytes.Clone(proof)
	event, err := DecodeReceipt(raw)
	if err != nil || event.Request != request {
		return Allocation{}, ErrInvalid
	}
	app.allocation.ReservationID = event.ReservationID
	app.allocation.RecordID = event.RecordID
	app.allocation.ReserveReceipt = bytes.Clone(raw)
	app.allocation.State = StateAllocated
	return app.allocation, nil
}

func (app *coordinatorTestApplication) PrepareAllocation(_ context.Context, _ Request, _ InitialCreation, _ []byte) (Allocation, error) {
	app.prepares++
	return app.allocation, nil
}

func (app *coordinatorTestApplication) CommittedCreation(_ context.Context, _ string) (Allocation, error) {
	return app.allocation, nil
}

func (app *coordinatorTestApplication) VerifyCertification(_ context.Context, recordID string, _ Observation) (Allocation, error) {
	app.verifications++
	if app.allocation.RecordID != recordID {
		return Allocation{}, ErrUncertified
	}
	return app.allocation, nil
}

type coordinatorTestReader struct {
	observation ContinuityObservation
	err         error
	mismatch    bool
	reads       int
}

func (reader *coordinatorTestReader) Continuity(_ context.Context, _ Binding) (ContinuityObservation, error) {
	reader.reads++
	if reader.err != nil {
		return ContinuityObservation{}, reader.err
	}
	observation := reader.observation
	canonical, err := EncodeContinuityObservation(observation)
	if err != nil {
		return ContinuityObservation{}, err
	}
	observation.Proof = coordinatorTestSign(canonical)
	if reader.mismatch {
		observation.Enabled = !observation.Enabled
	}
	return observation, nil
}

type coordinatorTestObserver struct {
	*coordinatorTestReader
	result   Observation
	observes int
}

func (reader *coordinatorTestObserver) Observe(_ context.Context, _ Binding, _ string) (Observation, error) {
	reader.observes++
	return reader.result, nil
}

type coordinatorTestProducer struct {
	reader     *coordinatorTestReader
	observeErr error
	conflicts  int
	reserves   int
	confirms   int
	observes   int
	proofs     []CheckpointProof
}

func (producer *coordinatorTestProducer) Observe(_ context.Context, binding Binding, _ string) (Observation, error) {
	producer.observes++
	if producer.observeErr != nil {
		return Observation{}, producer.observeErr
	}
	return Observation{Binding: binding}, nil
}

func (producer *coordinatorTestProducer) Reserve(_ context.Context, binding Binding, request Request, raw []byte) ([]byte, error) {
	producer.reserves++
	proof, err := DecodeCheckpointProof(raw)
	if err != nil || proof.Binding != binding || proof.Checkpoint.Request != request {
		return nil, ErrInvalid
	}
	producer.proofs = append(producer.proofs, proof)
	if producer.reserves <= producer.conflicts {
		producer.reader.observation.Anchor = proof.ProposedHead
		return nil, ErrCheckpointChanged
	}
	event := Event{Binding: binding, Sequence: 1, Kind: EventReserved,
		ReservationID: "rba_" + strings.Repeat("2", 32), RecordID: "rec2_" + strings.Repeat("8", 32), Request: request}
	canonical, err := EncodeEvent(event)
	if err != nil {
		return nil, err
	}
	return coordinatorTestSign(canonical), nil
}

func (*coordinatorTestProducer) Prepare(context.Context, Binding, string, [32]byte) ([]byte, error) {
	return nil, ErrUnavailable
}

func (producer *coordinatorTestProducer) Confirm(context.Context, Binding, string, [32]byte) ([]byte, error) {
	producer.confirms++
	return nil, ErrUnavailable
}

func coordinatorTestBinding() Binding {
	return Binding{
		AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
		DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", Generation: 1,
		KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: sha256.Sum256([]byte("cutover")),
	}
}

func coordinatorTestRequest() Request {
	return Request{ProjectID: "default", ActorID: "usr_one", ActorScopeDigest: sha256.Sum256([]byte("scope")),
		Purpose: PurposeCreate, IdempotencyKey: "continuity-test", RequestFingerprint: sha256.Sum256([]byte("request"))}
}

func TestCoordinatorRequiresFreshContinuityBeforeEffects(t *testing.T) {
	ctx := context.Background()
	binding := coordinatorTestBinding()
	request := coordinatorTestRequest()

	t.Run("never installed permits ordinary allocation", func(t *testing.T) {
		app := &coordinatorTestApplication{}
		coordinator, err := NewCoordinator(app, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		allocation, err := coordinator.Reserve(ctx, request)
		if err != nil || allocation.Domain != recordidentity.Ordinary || len(allocation.ContinuityProof) != 0 || app.begins != 1 {
			t.Fatalf("ordinary allocation=%+v begins=%d err=%v", allocation, app.begins, err)
		}
	})

	t.Run("installed without producer still requires reader", func(t *testing.T) {
		app := &coordinatorTestApplication{binding: binding, installed: true, enabled: true}
		reader := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding, Enabled: true}}
		coordinator, err := NewCoordinator(app, reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		allocation, err := coordinator.Reserve(ctx, request)
		if err != nil || allocation.Domain != recordidentity.Ordinary || len(allocation.ContinuityProof) == 0 ||
			!bytes.Equal(allocation.ContinuityProof, app.lastProof) || len(app.allocation.ContinuityProof) != 0 {
			t.Fatalf("installed ordinary allocation=%+v err=%v", allocation, err)
		}
	})

	t.Run("unavailable producer permits ordinary after continuity", func(t *testing.T) {
		app := &coordinatorTestApplication{binding: binding, installed: true, enabled: true}
		reader := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding, Enabled: true}}
		producer := &coordinatorTestProducer{observeErr: ErrUnavailable}
		coordinator, err := NewCoordinator(app, reader, producer)
		if err != nil {
			t.Fatal(err)
		}
		allocation, err := coordinator.Reserve(ctx, request)
		if err != nil || allocation.Domain != recordidentity.Ordinary || reader.reads == 0 || producer.observes != 1 ||
			producer.reserves != 0 || len(allocation.ContinuityProof) == 0 {
			t.Fatalf("producer failure allocation=%+v err=%v reader=%d observe=%d reserve=%d",
				allocation, err, reader.reads, producer.observes, producer.reserves)
		}
	})

	t.Run("disabled lineage still supplies continuity", func(t *testing.T) {
		app := &coordinatorTestApplication{binding: binding, installed: true}
		reader := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding}}
		coordinator, err := NewCoordinator(app, reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		allocation, err := coordinator.Reserve(ctx, request)
		if err != nil || allocation.Domain != recordidentity.Ordinary || len(allocation.ContinuityProof) == 0 {
			t.Fatalf("disabled lineage allocation=%+v err=%v", allocation, err)
		}
	})

	t.Run("concurrent certified winner cannot downgrade", func(t *testing.T) {
		winner := Allocation{AllocationID: "rqa_" + strings.Repeat("1", 32), Request: request,
			Domain: recordidentity.Certifiable, State: StatePending, Binding: binding}
		app := &coordinatorTestApplication{binding: binding, installed: true, enabled: true,
			allocation: winner, found: true, hideFirst: true}
		reader := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding, Enabled: true}}
		coordinator, err := NewCoordinator(app, reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := coordinator.Reserve(ctx, request); !errors.Is(err, ErrPending) || app.begins != 1 || app.allocation.Domain != recordidentity.Certifiable {
			t.Fatalf("winning allocation=%+v begins=%d err=%v", app.allocation, app.begins, err)
		}
	})

	for _, scenario := range []string{"reader missing", "reader failed", "projection mismatch", "lineage vanished"} {
		t.Run(scenario, func(t *testing.T) {
			app := &coordinatorTestApplication{binding: binding, installed: scenario != "lineage vanished", enabled: true,
				allocation: Allocation{AllocationID: "rqa_" + strings.Repeat("1", 32), Request: request,
					Domain: recordidentity.Ordinary, State: StateAllocated, ReservationID: "rba_" + strings.Repeat("2", 32),
					RecordID: "rec2_" + strings.Repeat("3", 32)}, found: true}
			var reader ContinuityReader
			if scenario != "reader missing" {
				stub := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding, Enabled: true}}
				stub.mismatch = scenario == "projection mismatch"
				if scenario == "reader failed" {
					stub.err = errors.New("reader unavailable")
				}
				reader = stub
			}
			coordinator, err := NewCoordinator(app, reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := coordinator.Reserve(ctx, request); !errors.Is(err, ErrUnavailable) || app.finds != 1 || app.begins != 0 {
				t.Fatalf("reserve err=%v finds=%d begins=%d", err, app.finds, app.begins)
			}
			creation := InitialCreation{ReservationID: app.allocation.ReservationID, RecordID: app.allocation.RecordID,
				RevisionID: "rrv_test", AuthorID: request.ActorID, CanonicalHash: sha256.Sum256([]byte("revision")),
				RequestFingerprint: sha256.Sum256([]byte("creation"))}
			if _, err := coordinator.PrepareCreate(ctx, request, creation); !errors.Is(err, ErrUnavailable) || app.prepares != 0 {
				t.Fatalf("prepare err=%v effects=%d", err, app.prepares)
			}
		})
	}
}

func TestCoordinatorRetriesCheckpointWithoutChangingAllocation(t *testing.T) {
	ctx := context.Background()
	binding := coordinatorTestBinding()
	app := &coordinatorTestApplication{binding: binding, installed: true, enabled: true}
	reader := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding, Enabled: true}}
	producer := &coordinatorTestProducer{reader: reader, conflicts: 1}
	coordinator, err := NewCoordinator(app, reader, producer)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := coordinator.Reserve(ctx, coordinatorTestRequest())
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Domain != recordidentity.Certifiable || app.begins != 1 || app.checkpoints != 2 ||
		producer.reserves != 2 || app.registers != 1 || len(producer.proofs) != 2 ||
		producer.proofs[0].Checkpoint.AllocationID != allocation.AllocationID ||
		producer.proofs[1].Checkpoint != producer.proofs[0].Checkpoint ||
		producer.proofs[1].ExpectedAnchor != producer.proofs[0].ProposedHead ||
		len(allocation.ContinuityProof) == 0 || !bytes.Equal(allocation.ContinuityProof, app.lastProof) {
		t.Fatalf("unexpected retry allocation=%+v begins=%d checkpoints=%d reserves=%d registers=%d proofs=%+v",
			allocation, app.begins, app.checkpoints, producer.reserves, app.registers, producer.proofs)
	}
}

func TestCoordinatorCheckpointRetryRemainsPending(t *testing.T) {
	binding := coordinatorTestBinding()
	app := &coordinatorTestApplication{binding: binding, installed: true, enabled: true}
	reader := &coordinatorTestReader{observation: ContinuityObservation{Binding: binding, Enabled: true}}
	producer := &coordinatorTestProducer{reader: reader, conflicts: 4}
	coordinator, err := NewCoordinator(app, reader, producer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Reserve(context.Background(), coordinatorTestRequest())
	if !errors.Is(err, ErrPending) || app.begins != 1 || app.checkpoints != 3 || producer.reserves != 3 ||
		app.registers != 0 || app.allocation.State != StatePending || app.allocation.Domain != recordidentity.Certifiable {
		t.Fatalf("retry err=%v begins=%d checkpoints=%d reserves=%d registers=%d allocation=%+v",
			err, app.begins, app.checkpoints, producer.reserves, app.registers, app.allocation)
	}
}

func coordinatorTestCompleted(t *testing.T, state State) Allocation {
	t.Helper()
	request := coordinatorTestRequest()
	allocation := Allocation{
		AllocationID: "rqa_" + strings.Repeat("1", 32), Request: request,
		Domain: recordidentity.Ordinary, State: state, ReservationID: "rba_" + strings.Repeat("2", 32),
		RecordID: "rec2_" + strings.Repeat("3", 32),
	}
	if state == StateConfirmed {
		allocation.Domain = recordidentity.Certifiable
		allocation.Binding = coordinatorTestBinding()
		allocation.RecordID = "rec2_" + strings.Repeat("8", 32)
	}
	allocation.Creation = InitialCreation{ReservationID: allocation.ReservationID, RecordID: allocation.RecordID,
		RevisionID: "rrv_test", AuthorID: request.ActorID, CanonicalHash: sha256.Sum256([]byte("revision")),
		RequestFingerprint: sha256.Sum256([]byte("creation"))}
	if state == StateConfirmed {
		commitment, err := allocation.Creation.Commitment()
		if err != nil {
			t.Fatal(err)
		}
		event := Event{Binding: allocation.Binding, Sequence: 1, Kind: EventReserved, ReservationID: allocation.ReservationID,
			RecordID: allocation.RecordID, Request: request}
		encode := func() []byte {
			t.Helper()
			canonical, err := EncodeEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			return coordinatorTestSign(canonical)
		}
		allocation.ReserveReceipt = encode()
		event.Sequence, event.PreviousHash, event.Kind, event.InitialCommitment = 2, sha256.Sum256(allocation.ReserveReceipt), EventPrepared, commitment
		allocation.PrepareReceipt = encode()
		event.Sequence, event.PreviousHash, event.Kind = 3, sha256.Sum256(allocation.PrepareReceipt), EventCreated
		allocation.CreatedReceipt = encode()
	}
	return allocation
}

func TestCoordinatorCompletedReadsSurviveReaderOutage(t *testing.T) {
	ctx := context.Background()
	for _, state := range []State{StateFinished, StateConfirmed, StateTombstoned} {
		t.Run(string(state), func(t *testing.T) {
			allocation := coordinatorTestCompleted(t, state)
			app := &coordinatorTestApplication{binding: coordinatorTestBinding(), installed: true, enabled: true,
				allocation: allocation, found: true}
			reader := &coordinatorTestReader{err: errors.New("reader unavailable")}
			producer := &coordinatorTestProducer{}
			coordinator, err := NewCoordinator(app, reader, producer)
			if err != nil {
				t.Fatal(err)
			}
			got, err := coordinator.Reserve(ctx, allocation.Request)
			if err != nil || got.AllocationID != allocation.AllocationID || got.State != state ||
				reader.reads != 0 || app.begins != 0 || len(got.ContinuityProof) != 0 {
				t.Fatalf("completed read=%+v err=%v reader calls=%d", got, err, reader.reads)
			}
			if state != StateTombstoned {
				if err := coordinator.ConfirmCreate(ctx, allocation.RecordID); err != nil || producer.confirms != 0 || reader.reads != 0 {
					t.Fatalf("read-only confirmation err=%v producer calls=%d reader calls=%d", err, producer.confirms, reader.reads)
				}
			}
		})
	}
}

func TestCoordinatorCertificationUsesIndependentReader(t *testing.T) {
	allocation := coordinatorTestCompleted(t, StateConfirmed)
	app := &coordinatorTestApplication{binding: allocation.Binding, installed: true, enabled: true,
		allocation: allocation, found: true}
	reader := &coordinatorTestObserver{
		coordinatorTestReader: &coordinatorTestReader{observation: ContinuityObservation{Binding: allocation.Binding, Enabled: true}},
		result: Observation{Binding: allocation.Binding, Reserved: allocation.ReserveReceipt,
			Prepared: allocation.PrepareReceipt, Created: allocation.CreatedReceipt},
	}
	coordinator, err := NewCoordinator(app, reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	certification, err := coordinator.ReadCertification(context.Background(), allocation.RecordID)
	if err != nil || certification.AllocationID != allocation.AllocationID || certification.Binding != allocation.Binding {
		t.Fatalf("independent reader certification=%+v err=%v", certification, err)
	}
}

func TestCoordinatorCertificationObservation(t *testing.T) {
	allocation := coordinatorTestCompleted(t, StateConfirmed)
	newFixture := func(t *testing.T) (*Coordinator, *coordinatorTestApplication, *coordinatorTestObserver) {
		t.Helper()
		app := &coordinatorTestApplication{binding: allocation.Binding, installed: true, enabled: true, allocation: allocation}
		reader := &coordinatorTestObserver{
			coordinatorTestReader: &coordinatorTestReader{observation: ContinuityObservation{Binding: allocation.Binding, Enabled: true}},
			result:                Observation{Binding: allocation.Binding, Reserved: allocation.ReserveReceipt, Prepared: allocation.PrepareReceipt, Created: allocation.CreatedReceipt},
		}
		coordinator, err := NewCoordinator(app, reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		return coordinator, app, reader
	}
	t.Run("observation remains separate from APP verification", func(t *testing.T) {
		coordinator, app, reader := newFixture(t)
		observation, err := coordinator.ReadCertificationObservation(context.Background(), allocation.RecordID)
		if err != nil || observation.Binding != allocation.Binding || !bytes.Equal(observation.Created, allocation.CreatedReceipt) ||
			reader.reads != 1 || reader.observes != 1 || app.verifications != 0 {
			t.Fatalf("observation=%+v error=%v continuity=%d observations=%d verifications=%d", observation, err, reader.reads, reader.observes, app.verifications)
		}
		_, err = coordinator.ReadCertification(context.Background(), allocation.RecordID)
		if err != nil || reader.reads != 2 || reader.observes != 2 || app.verifications != 1 {
			t.Fatalf("certification error=%v continuity=%d observations=%d verifications=%d", err, reader.reads, reader.observes, app.verifications)
		}
	})
	for _, tc := range []struct {
		name     string
		mutate   func(*coordinatorTestApplication, *coordinatorTestObserver)
		want     error
		observes int
	}{
		{"disabled APP", func(app *coordinatorTestApplication, _ *coordinatorTestObserver) { app.enabled = false }, ErrUncertified, 0},
		{"disabled continuity", func(_ *coordinatorTestApplication, reader *coordinatorTestObserver) {
			reader.observation.Enabled = false
		}, ErrUncertified, 0},
		{"missing creation", func(_ *coordinatorTestApplication, reader *coordinatorTestObserver) { reader.result.Created = nil }, ErrUncertified, 1},
		{"wrong binding", func(_ *coordinatorTestApplication, reader *coordinatorTestObserver) {
			reader.result.Binding = Binding{}
		}, ErrUncertified, 1},
		{"continuity error", func(_ *coordinatorTestApplication, reader *coordinatorTestObserver) { reader.err = ErrUnavailable }, ErrUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coordinator, app, reader := newFixture(t)
			tc.mutate(app, reader)
			_, err := coordinator.ReadCertificationObservation(context.Background(), allocation.RecordID)
			if !errors.Is(err, tc.want) || reader.observes != tc.observes || app.verifications != 0 {
				t.Fatalf("error=%v observations=%d verifications=%d", err, reader.observes, app.verifications)
			}
		})
	}
}

func TestCoordinatorConfirmRequiresContinuityBeforeProducer(t *testing.T) {
	allocation := coordinatorTestCompleted(t, StateConfirmed)
	allocation.State = StateFinished
	allocation.CreatedReceipt = nil
	app := &coordinatorTestApplication{binding: allocation.Binding, installed: true, enabled: true,
		allocation: allocation, found: true}
	reader := &coordinatorTestReader{err: errors.New("reader unavailable")}
	producer := &coordinatorTestProducer{}
	coordinator, err := NewCoordinator(app, reader, producer)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ConfirmCreate(context.Background(), allocation.RecordID); !errors.Is(err, ErrUnavailable) || producer.confirms != 0 {
		t.Fatalf("confirm err=%v producer calls=%d", err, producer.confirms)
	}
}

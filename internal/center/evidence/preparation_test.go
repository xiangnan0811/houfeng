package evidence

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestRevisionPreparerPreparesFreshCaptureAndExistingReferenceInRequestOrder(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	actor := testActor(t)
	events := make([]string, 0, 6)
	kind := &revisionPreparationKindStub{
		kindStub: &kindStub{
			descriptor:    inputs.descriptor,
			authorization: inputs.authorization,
			snapshot:      inputs.snapshot,
		},
		events: &events,
	}
	registry, err := NewRegistry([]Kind{kind})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	intentSource := &captureIntentBindingSourceStub{
		binding: CaptureIntentBinding{
			RecordID: inputs.recordID, SnapshotID: inputs.snapshotID,
			Intent: inputs.intent, Preview: inputs.preview,
		},
		events: &events,
	}
	payloadSink := &capturePayloadSinkStub{events: &events}
	referenceState := testExistingSnapshotReferenceState(t, "evs_reference1")
	referenceState.RecordID = inputs.recordID
	referenceSource := &revisionPreparationReferenceSourceStub{state: referenceState, events: &events}
	preparer, err := NewRevisionPreparer(registry, intentSource, payloadSink, referenceSource,
		mustPreparationCapacity(t, actor.ProjectID, 0, DefaultProjectEvidenceCapacityBytes, 80))
	if err != nil {
		t.Fatalf("NewRevisionPreparer() error = %v", err)
	}

	prepared, err := preparer.Prepare(context.Background(), actor, RevisionPreparationRequest{
		RecordID: inputs.recordID,
		Items: []RevisionPreparationItem{
			{ExistingSnapshotID: referenceState.SnapshotID},
			{CaptureIntentID: inputs.intent.ID},
		},
	})
	if err != nil {
		t.Fatalf("RevisionPreparer.Prepare() error = %v", err)
	}
	if err := prepared.ValidateForRecord(inputs.recordID); err != nil {
		t.Fatalf("RevisionPreparation.ValidateForRecord() error = %v", err)
	}
	if got, want := prepared.SnapshotIDs(), []string{referenceState.SnapshotID, inputs.snapshotID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RevisionPreparation.SnapshotIDs() = %#v, want %#v", got, want)
	}
	if len(prepared.Captures()) != 1 || len(prepared.References()) != 1 {
		t.Fatalf("prepared values = %d captures/%d references, want 1/1", len(prepared.Captures()), len(prepared.References()))
	}
	if got, want := events, []string{"reference", "load", "selection", "authorize", "capture", "payload"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preparation events = %#v, want %#v", got, want)
	}
	if intentSource.recordID != inputs.recordID || intentSource.intentID != inputs.intent.ID {
		t.Fatalf("intent load input = %q/%q, want %q/%q", intentSource.recordID, intentSource.intentID, inputs.recordID, inputs.intent.ID)
	}
	if payloadSink.calls != 1 || payloadSink.snapshot.Hash() != inputs.snapshot.Hash() {
		t.Fatalf("payload persistence = %d calls/digest %x, want 1/%x", payloadSink.calls, payloadSink.snapshot.Hash(), inputs.snapshot.Hash())
	}
	if referenceSource.calls != 1 || kind.previewCalls != 0 {
		t.Fatalf("reference/capture calls = %d reauthorizations/%d previews, want 1/0", referenceSource.calls, kind.previewCalls)
	}
}

func TestRevisionPreparerRejectsCaptureDriftBeforePersistingPayload(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	inputs.snapshot.envelope.SourceRevision = "revision-drifted"
	kind := &revisionPreparationKindStub{kindStub: &kindStub{
		descriptor: inputs.descriptor, authorization: inputs.authorization, snapshot: inputs.snapshot,
	}}
	registry, err := NewRegistry([]Kind{kind})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	sink := &capturePayloadSinkStub{}
	preparer, err := NewRevisionPreparer(
		registry,
		&captureIntentBindingSourceStub{binding: CaptureIntentBinding{
			RecordID: inputs.recordID, SnapshotID: inputs.snapshotID,
			Intent: inputs.intent, Preview: inputs.preview,
		}},
		sink,
		&revisionPreparationReferenceSourceStub{},
		mustPreparationCapacity(t, testActor(t).ProjectID, 0, DefaultProjectEvidenceCapacityBytes, 80),
	)
	if err != nil {
		t.Fatalf("NewRevisionPreparer() error = %v", err)
	}

	_, err = preparer.Prepare(context.Background(), testActor(t), RevisionPreparationRequest{
		RecordID: inputs.recordID,
		Items:    []RevisionPreparationItem{{CaptureIntentID: inputs.intent.ID}},
	})
	if !errors.Is(err, ErrInvalidPreparedCapture) {
		t.Fatalf("RevisionPreparer.Prepare() error = %v, want ErrInvalidPreparedCapture", err)
	}
	if sink.calls != 0 {
		t.Fatalf("payload persistence calls = %d, want zero before drift rejection", sink.calls)
	}
}

func TestRevisionPreparerDoesNotConstructCaptureWhenPayloadPersistenceFails(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	persistErr := errors.New("payload store unavailable")
	kind := &revisionPreparationKindStub{kindStub: &kindStub{
		descriptor: inputs.descriptor, authorization: inputs.authorization, snapshot: inputs.snapshot,
	}}
	registry, err := NewRegistry([]Kind{kind})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	preparer, err := NewRevisionPreparer(
		registry,
		&captureIntentBindingSourceStub{binding: CaptureIntentBinding{
			RecordID: inputs.recordID, SnapshotID: inputs.snapshotID,
			Intent: inputs.intent, Preview: inputs.preview,
		}},
		&capturePayloadSinkStub{err: persistErr},
		&revisionPreparationReferenceSourceStub{},
		mustPreparationCapacity(t, testActor(t).ProjectID, 0, DefaultProjectEvidenceCapacityBytes, 80),
	)
	if err != nil {
		t.Fatalf("NewRevisionPreparer() error = %v", err)
	}

	prepared, err := preparer.Prepare(context.Background(), testActor(t), RevisionPreparationRequest{
		RecordID: inputs.recordID,
		Items:    []RevisionPreparationItem{{CaptureIntentID: inputs.intent.ID}},
	})
	if !errors.Is(err, persistErr) {
		t.Fatalf("RevisionPreparer.Prepare() error = %v, want payload store error", err)
	}
	if !prepared.Empty() {
		t.Fatal("RevisionPreparer.Prepare() returned partial preparation after payload persistence failure")
	}
}

func TestCaptureIntentBindingRequiresExactPersistedLifetime(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	inputs.preview.ValidUntil = inputs.preview.PreviewedAt.Add(CaptureIntentTTL - 1)
	inputs.intent.ValidUntil = inputs.preview.ValidUntil
	binding := CaptureIntentBinding{
		RecordID: inputs.recordID, SnapshotID: inputs.snapshotID,
		Intent: inputs.intent, Preview: inputs.preview,
	}

	if err := binding.Validate(); !errors.Is(err, ErrInvalidCaptureIntentBinding) {
		t.Fatalf("CaptureIntentBinding.Validate() error = %v, want ErrInvalidCaptureIntentBinding", err)
	}
}

func TestRevisionPreparerRejectsNoncanonicalRawBindingTimestampsBeforeAdapters(t *testing.T) {
	offset := time.FixedZone("persisted-offset", 5*60*60+30*60)
	representations := []struct {
		name      string
		transform func(time.Time) time.Time
	}{
		{name: "offset", transform: func(value time.Time) time.Time { return value.In(offset) }},
		{name: "monotonic", transform: equivalentMonotonicTime},
		{name: "sub-microsecond", transform: func(value time.Time) time.Time { return value.Add(time.Nanosecond) }},
	}
	timestamps := []struct {
		name   string
		mutate func(*CaptureIntentBinding, func(time.Time) time.Time)
	}{
		{name: "intent selection requested start", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Intent.Selection.RequestedWindow.Start = transform(binding.Intent.Selection.RequestedWindow.Start)
		}},
		{name: "intent selection requested end", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Intent.Selection.RequestedWindow.End = transform(binding.Intent.Selection.RequestedWindow.End)
		}},
		{name: "intent valid until", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Intent.ValidUntil = transform(binding.Intent.ValidUntil)
		}},
		{name: "preview selection requested start", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.Selection.RequestedWindow.Start = transform(binding.Preview.Selection.RequestedWindow.Start)
		}},
		{name: "preview selection requested end", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.Selection.RequestedWindow.End = transform(binding.Preview.Selection.RequestedWindow.End)
		}},
		{name: "preview requested start", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.RequestedWindow.Start = transform(binding.Preview.RequestedWindow.Start)
		}},
		{name: "preview requested end", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.RequestedWindow.End = transform(binding.Preview.RequestedWindow.End)
		}},
		{name: "preview actual start", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.ActualWindow.Start = transform(binding.Preview.ActualWindow.Start)
		}},
		{name: "preview actual end", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.ActualWindow.End = transform(binding.Preview.ActualWindow.End)
		}},
		{name: "preview observed at", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.ObservedAt = transform(binding.Preview.ObservedAt)
		}},
		{name: "previewed at", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.PreviewedAt = transform(binding.Preview.PreviewedAt)
		}},
		{name: "preview valid until", mutate: func(binding *CaptureIntentBinding, transform func(time.Time) time.Time) {
			binding.Preview.ValidUntil = transform(binding.Preview.ValidUntil)
		}},
	}

	for _, representation := range representations {
		for _, timestamp := range timestamps {
			t.Run(representation.name+"/"+timestamp.name, func(t *testing.T) {
				inputs := newPreparedCaptureTestInputs(t)
				binding := CaptureIntentBinding{
					RecordID: inputs.recordID, SnapshotID: inputs.snapshotID,
					Intent: inputs.intent, Preview: inputs.preview,
				}
				timestamp.mutate(&binding, representation.transform)
				events := make([]string, 0, 5)
				kind := &revisionPreparationKindStub{
					kindStub: &kindStub{
						descriptor: inputs.descriptor, authorization: inputs.authorization, snapshot: inputs.snapshot,
					},
					events: &events,
				}
				registry, err := NewRegistry([]Kind{kind})
				if err != nil {
					t.Fatalf("NewRegistry() error = %v", err)
				}
				source := &captureIntentBindingSourceStub{binding: binding, events: &events}
				sink := &capturePayloadSinkStub{events: &events}
				preparer, err := NewRevisionPreparer(registry, source, sink, &revisionPreparationReferenceSourceStub{},
					mustPreparationCapacity(t, testActor(t).ProjectID, 0, DefaultProjectEvidenceCapacityBytes, 80))
				if err != nil {
					t.Fatalf("NewRevisionPreparer() error = %v", err)
				}

				_, err = preparer.Prepare(context.Background(), testActor(t), RevisionPreparationRequest{
					RecordID: inputs.recordID,
					Items:    []RevisionPreparationItem{{CaptureIntentID: inputs.intent.ID}},
				})
				if !errors.Is(err, ErrInvalidCaptureIntentBinding) {
					t.Fatalf("RevisionPreparer.Prepare() error = %v, want ErrInvalidCaptureIntentBinding", err)
				}
				if got, want := events, []string{"load"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("preparation events = %#v, want %#v", got, want)
				}
				if sink.calls != 0 || kind.previewCalls != 0 {
					t.Fatalf("payload/preview calls = %d/%d, want zero", sink.calls, kind.previewCalls)
				}
			})
		}
	}
}

func TestRevisionPreparerFinalCapacityRecheckAndExistingReferenceExemption(t *testing.T) {
	actor := testActor(t)
	inputs := newPreparedCaptureTestInputs(t)
	kind := &revisionPreparationKindStub{kindStub: &kindStub{
		descriptor: inputs.descriptor, authorization: inputs.authorization, snapshot: inputs.snapshot,
	}}
	registry, err := NewRegistry([]Kind{kind})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	source := &projectCapacitySourceStub{usage: ProjectCapacityUsage{
		ProjectID: string(actor.ProjectID), LogicalSnapshotCount: 1, LogicalSnapshotBytes: 1_000,
		PhysicalPayloadCount: 1, PhysicalCanonicalBytes: 1_000, PhysicalCompressedBytes: 1_000,
	}}
	capacity, err := NewCapacityEnforcer(CapacityPolicy{ProjectLimitBytes: 1_000, WarningPercent: 80}, source)
	if err != nil {
		t.Fatalf("NewCapacityEnforcer() error = %v", err)
	}
	sink := &capturePayloadSinkStub{}
	preparer, err := NewRevisionPreparer(
		registry,
		&captureIntentBindingSourceStub{binding: CaptureIntentBinding{
			RecordID: inputs.recordID, SnapshotID: inputs.snapshotID, Intent: inputs.intent, Preview: inputs.preview,
		}},
		sink,
		&revisionPreparationReferenceSourceStub{},
		capacity,
	)
	if err != nil {
		t.Fatalf("NewRevisionPreparer() error = %v", err)
	}
	if _, err := preparer.Prepare(context.Background(), actor, RevisionPreparationRequest{
		RecordID: inputs.recordID, Items: []RevisionPreparationItem{{CaptureIntentID: inputs.intent.ID}},
	}); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("Prepare(stale capacity) error = %v, want ErrPreviewStale", err)
	}
	if sink.calls != 0 {
		t.Fatalf("payload calls = %d, want zero on capacity denial", sink.calls)
	}

	reference := testExistingSnapshotReferenceState(t, "evs_referencequota")
	reference.RecordID = inputs.recordID
	referenceSource := &revisionPreparationReferenceSourceStub{state: reference}
	preparer, err = NewRevisionPreparer(registry, &captureIntentBindingSourceStub{}, sink, referenceSource, capacity)
	if err != nil {
		t.Fatalf("NewRevisionPreparer(reference) error = %v", err)
	}
	beforeCapacityCalls := source.calls
	prepared, err := preparer.Prepare(context.Background(), actor, RevisionPreparationRequest{
		RecordID: inputs.recordID, Items: []RevisionPreparationItem{{ExistingSnapshotID: reference.SnapshotID}},
	})
	if err != nil || len(prepared.References()) != 1 || source.calls != beforeCapacityCalls {
		t.Fatalf("Prepare(existing reference) = %#v, %v; capacity calls %d -> %d", prepared, err, beforeCapacityCalls, source.calls)
	}
}

func mustPreparationCapacity(
	t *testing.T,
	projectID recordauth.ProjectID,
	logicalBytes uint64,
	limitBytes uint64,
	warningPercent uint8,
) *CapacityEnforcer {
	t.Helper()
	return mustTestCapacityEnforcer(t, string(projectID), logicalBytes, limitBytes, warningPercent)
}

func equivalentMonotonicTime(value time.Time) time.Time {
	now := time.Now()
	return now.Add(value.Sub(now))
}

type revisionPreparationKindStub struct {
	*kindStub
	events       *[]string
	previewCalls int
}

func (stub *revisionPreparationKindStub) ValidateSelection(_ context.Context, _ ActorScope, _ Selection) error {
	stub.record("selection")
	return nil
}

func (stub *revisionPreparationKindStub) PreviewCapture(context.Context, ActorScope, Selection) (Preview, error) {
	stub.previewCalls++
	return Preview{}, errors.New("preview must not run during revision preparation")
}

func (stub *revisionPreparationKindStub) Authorize(context.Context, ActorScope, Selection) (AuthorizationScope, error) {
	stub.record("authorize")
	return stub.authorization, nil
}

func (stub *revisionPreparationKindStub) Capture(context.Context, ActorScope, Intent) (CanonicalSnapshot, error) {
	stub.record("capture")
	return stub.snapshot, nil
}

func (stub *revisionPreparationKindStub) record(event string) {
	if stub.events != nil {
		*stub.events = append(*stub.events, event)
	}
}

type captureIntentBindingSourceStub struct {
	binding  CaptureIntentBinding
	err      error
	events   *[]string
	recordID string
	intentID string
}

func (source *captureIntentBindingSourceStub) LoadCaptureIntentBinding(
	_ context.Context,
	recordID string,
	intentID string,
) (CaptureIntentBinding, error) {
	if source.events != nil {
		*source.events = append(*source.events, "load")
	}
	source.recordID = recordID
	source.intentID = intentID
	return source.binding, source.err
}

type capturePayloadSinkStub struct {
	calls    int
	snapshot CanonicalSnapshot
	err      error
	events   *[]string
}

func (sink *capturePayloadSinkStub) PersistCapturePayload(_ context.Context, snapshot CanonicalSnapshot) error {
	sink.calls++
	sink.snapshot = snapshot
	if sink.events != nil {
		*sink.events = append(*sink.events, "payload")
	}
	return sink.err
}

type revisionPreparationReferenceSourceStub struct {
	state  ExistingSnapshotReferenceState
	err    error
	calls  int
	events *[]string
}

func (source *revisionPreparationReferenceSourceStub) ReauthorizeExistingSnapshot(
	context.Context,
	ActorScope,
	string,
	string,
) (ExistingSnapshotReferenceState, error) {
	source.calls++
	if source.events != nil {
		*source.events = append(*source.events, "reference")
	}
	return source.state, source.err
}

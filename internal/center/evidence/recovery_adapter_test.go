package evidence

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestRecoveryAdapterReplaysValidatedLogicalInventoryThenRunsGlobalGC(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	inventory := EvidenceRecoveryInventory{
		Payloads: []EvidenceRecoveryPayload{{Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes()}},
		Snapshots: []EvidenceRecoverySnapshot{{
			RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: kind.snapshot.Envelope(),
			PayloadDigest: kind.snapshot.Hash(),
		}},
		CaptureIntents: []EvidenceRecoveryCaptureIntent{},
		RevisionReferences: []EvidenceRecoveryRevisionReference{{
			RecordID: "rec_recovery", RevisionID: "rrv_recovery", Ordinal: 0, SnapshotID: "evs_recovery",
			Caption: "decision baseline", ReferenceRole: EvidenceReferenceRoleDecisionSupport,
		}},
		CopyLineage: []EvidenceRecoveryCopyLineage{{
			SnapshotID: "evs_recovery", CopiedFromSnapshotID: "evs_deleted", CopyReason: "",
		}},
	}
	repository := &evidenceRecoveryRepositoryStub{}
	adapter, err := NewRecoveryAdapter(registry, repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	if err := adapter.Replay(context.Background(), inventory); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if repository.restoreCalls != 1 || repository.gcCalls != 1 || repository.order != "restore,gc" {
		t.Fatalf("repository calls/order = %d/%d/%q", repository.restoreCalls, repository.gcCalls, repository.order)
	}
	if repository.inventory.Payloads == nil || repository.inventory.Snapshots == nil ||
		repository.inventory.CaptureIntents == nil || repository.inventory.RevisionReferences == nil ||
		repository.inventory.CopyLineage == nil {
		t.Fatalf("replayed inventory lost canonical non-nil collections = %#v", repository.inventory)
	}
}

func TestRecoveryAdapterPreservesWitnessedSourceAuthorizationFloor(t *testing.T) {
	kind, _ := testConformingKind(t)
	envelope := kind.snapshot.Envelope()
	capture := envelope.Authorization.CaptureScope
	tombstoned, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: envelope.Authorization.Kind,
		SourceID: envelope.Authorization.SourceID, State: recordauth.SourceStateTombstoned,
		CaptureScope: capture, FinalFloor: &capture, LastLiveScope: &capture,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	envelope.Authorization = tombstoned
	snapshot, err := RestoreCanonicalSnapshot(kind.descriptor, envelope, kind.snapshot.Bytes())
	if err != nil {
		t.Fatalf("RestoreCanonicalSnapshot() error = %v", err)
	}
	registry, _ := NewRegistry([]Kind{kind})
	repository := &evidenceRecoveryRepositoryStub{}
	adapter, _ := NewRecoveryAdapter(registry, repository)
	inventory := EvidenceRecoveryInventory{
		Payloads: []EvidenceRecoveryPayload{{Key: kind.descriptor.Key, Digest: snapshot.Hash(), CanonicalPayload: snapshot.Bytes()}},
		Snapshots: []EvidenceRecoverySnapshot{{
			RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: snapshot.Envelope(), PayloadDigest: snapshot.Hash(),
		}},
		CaptureIntents: []EvidenceRecoveryCaptureIntent{}, RevisionReferences: []EvidenceRecoveryRevisionReference{},
		CopyLineage: []EvidenceRecoveryCopyLineage{},
	}
	if err := adapter.Replay(context.Background(), inventory); err != nil {
		t.Fatalf("Replay(tombstoned source floor) error = %v", err)
	}
	got := repository.inventory.Snapshots[0].Envelope.Authorization
	if !reflect.DeepEqual(got, tombstoned) || got.FinalFloor == nil || got.LastLiveScope == nil {
		t.Fatalf("replayed source authorization = %#v, want %#v", got, tombstoned)
	}
}

func TestRecoveryAdapterFailsClosedOnUnknownKindBeforeReplayOrGC(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	envelope := kind.snapshot.Envelope()
	envelope.Key = KindKey{Kind: KindName("comparison.result"), SchemaVersion: 99}
	inventory := EvidenceRecoveryInventory{
		Payloads:       []EvidenceRecoveryPayload{{Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes()}},
		Snapshots:      []EvidenceRecoverySnapshot{{RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: envelope, PayloadDigest: kind.snapshot.Hash()}},
		CaptureIntents: []EvidenceRecoveryCaptureIntent{}, RevisionReferences: []EvidenceRecoveryRevisionReference{}, CopyLineage: []EvidenceRecoveryCopyLineage{},
	}
	repository := &evidenceRecoveryRepositoryStub{}
	adapter, _ := NewRecoveryAdapter(registry, repository)
	if err := adapter.Replay(context.Background(), inventory); !errors.Is(err, ErrKindNotRegistered) {
		t.Fatalf("Replay(unknown kind) error = %v", err)
	}
	if repository.restoreCalls != 0 || repository.gcCalls != 0 {
		t.Fatalf("unknown kind reached repository = %d/%d", repository.restoreCalls, repository.gcCalls)
	}
}

func TestRecoveryAdapterRejectsUnreferencedPayloadBeforeReplayOrGC(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	inventory := EvidenceRecoveryInventory{
		Payloads: []EvidenceRecoveryPayload{{
			Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes(),
		}},
		Snapshots:          []EvidenceRecoverySnapshot{},
		CaptureIntents:     []EvidenceRecoveryCaptureIntent{},
		RevisionReferences: []EvidenceRecoveryRevisionReference{},
		CopyLineage:        []EvidenceRecoveryCopyLineage{},
	}
	repository := &evidenceRecoveryRepositoryStub{}
	adapter, _ := NewRecoveryAdapter(registry, repository)
	if err := adapter.Replay(context.Background(), inventory); !errors.Is(err, ErrInvalidRecoveryInventory) {
		t.Fatalf("Replay(unreferenced payload) error = %v, want ErrInvalidRecoveryInventory", err)
	}
	if repository.restoreCalls != 0 || repository.gcCalls != 0 {
		t.Fatalf("unreferenced payload reached repository = %d/%d", repository.restoreCalls, repository.gcCalls)
	}
}

func TestRecoveryAdapterDefensivelyClonesNestedInventoryBeforeReplay(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	inventory := EvidenceRecoveryInventory{
		Payloads: []EvidenceRecoveryPayload{{
			Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes(),
		}},
		Snapshots: []EvidenceRecoverySnapshot{{
			RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: kind.snapshot.Envelope(),
			PayloadDigest: kind.snapshot.Hash(),
		}},
		CaptureIntents:     []EvidenceRecoveryCaptureIntent{},
		RevisionReferences: []EvidenceRecoveryRevisionReference{},
		CopyLineage:        []EvidenceRecoveryCopyLineage{},
	}
	wantName := inventory.Snapshots[0].Envelope.Subject.Fields["display_name"]
	wantPayload := append([]byte(nil), inventory.Payloads[0].CanonicalPayload...)
	wantPolicyRevision := inventory.Snapshots[0].Envelope.Authorization.CurrentScope.PolicyRevision
	repository := &evidenceRecoveryRepositoryStub{mutateRestoreInput: true}
	adapter, _ := NewRecoveryAdapter(registry, repository)
	if err := adapter.Replay(context.Background(), inventory); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if inventory.Snapshots[0].Envelope.Subject.Fields["display_name"] != wantName ||
		!reflect.DeepEqual(inventory.Payloads[0].CanonicalPayload, wantPayload) ||
		inventory.Snapshots[0].Envelope.Authorization.CurrentScope.PolicyRevision != wantPolicyRevision {
		t.Fatalf("Replay() mutated caller-owned nested inventory = %#v", inventory)
	}
}

func TestRecoveryAdapterRejectsNoncanonicalSnapshotTimestampsBeforeReplay(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	offset := time.FixedZone("recovery-offset", 5*60*60+30*60)
	representations := []struct {
		name      string
		transform func(time.Time) time.Time
	}{
		{name: "offset", transform: func(value time.Time) time.Time { return value.In(offset) }},
		{name: "sub-microsecond", transform: func(value time.Time) time.Time { return value.Add(time.Nanosecond) }},
	}
	timestamps := []struct {
		name   string
		mutate func(*SnapshotEnvelope, func(time.Time) time.Time)
	}{
		{name: "requested start", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.RequestedWindow.Start = transform(envelope.RequestedWindow.Start)
		}},
		{name: "requested end", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.RequestedWindow.End = transform(envelope.RequestedWindow.End)
		}},
		{name: "actual start", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.ActualWindow.Start = transform(envelope.ActualWindow.Start)
		}},
		{name: "actual end", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.ActualWindow.End = transform(envelope.ActualWindow.End)
		}},
		{name: "observed at", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.ObservedAt = transform(envelope.ObservedAt)
		}},
		{name: "captured at", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.CapturedAt = transform(envelope.CapturedAt)
		}},
		{name: "referenced at", mutate: func(envelope *SnapshotEnvelope, transform func(time.Time) time.Time) {
			envelope.ReferencedAt = transform(envelope.ReferencedAt)
		}},
	}
	for _, representation := range representations {
		for _, timestamp := range timestamps {
			t.Run(representation.name+"/"+timestamp.name, func(t *testing.T) {
				envelope := kind.snapshot.Envelope()
				timestamp.mutate(&envelope, representation.transform)
				inventory := EvidenceRecoveryInventory{
					Payloads: []EvidenceRecoveryPayload{{
						Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes(),
					}},
					Snapshots: []EvidenceRecoverySnapshot{{
						RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: envelope,
						PayloadDigest: kind.snapshot.Hash(),
					}},
					CaptureIntents:     []EvidenceRecoveryCaptureIntent{},
					RevisionReferences: []EvidenceRecoveryRevisionReference{},
					CopyLineage:        []EvidenceRecoveryCopyLineage{},
				}
				repository := &evidenceRecoveryRepositoryStub{}
				adapter, _ := NewRecoveryAdapter(registry, repository)
				if err := adapter.Replay(context.Background(), inventory); !errors.Is(err, ErrInvalidRecoveryInventory) {
					t.Fatalf("Replay(noncanonical timestamp) error = %v, want ErrInvalidRecoveryInventory", err)
				}
				if repository.restoreCalls != 0 || repository.gcCalls != 0 {
					t.Fatalf("noncanonical timestamp reached repository = %d/%d", repository.restoreCalls, repository.gcCalls)
				}
			})
		}
	}
}

type evidenceRecoveryRepositoryStub struct {
	restoreCalls       int
	gcCalls            int
	order              string
	inventory          EvidenceRecoveryInventory
	mutateRestoreInput bool
}

func (repository *evidenceRecoveryRepositoryStub) RestoreEvidenceInventory(_ context.Context, inventory EvidenceRecoveryInventory) error {
	repository.restoreCalls++
	repository.order = "restore"
	repository.inventory = inventory
	if repository.mutateRestoreInput {
		repository.inventory.Payloads[0].CanonicalPayload[0] ^= 0xff
		repository.inventory.Snapshots[0].Envelope.Subject.Fields["display_name"] = "mutated"
		repository.inventory.Snapshots[0].Envelope.Authorization.CurrentScope.PolicyRevision++
	}
	return nil
}

func (repository *evidenceRecoveryRepositoryStub) CollectUnreferencedEvidencePayloads(context.Context) error {
	repository.gcCalls++
	repository.order += ",gc"
	return nil
}

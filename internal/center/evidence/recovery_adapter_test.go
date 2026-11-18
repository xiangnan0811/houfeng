package evidence

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestRecoveryInventoryValidatesLogicalReferences(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	inventory := EvidenceRecoveryInventory{
		Payloads: []EvidenceRecoveryPayload{{Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes()}},
		Snapshots: []EvidenceRecoverySnapshot{{
			RecordID: "rec2_80000000000000000000000000000000", SnapshotID: "evs_recovery", Envelope: kind.snapshot.Envelope(),
			PayloadDigest: kind.snapshot.Hash(),
		}},
		CaptureIntents: []EvidenceRecoveryCaptureIntent{},
		RevisionReferences: []EvidenceRecoveryRevisionReference{{
			RecordID: "rec2_80000000000000000000000000000000", RevisionID: "rrv_recovery", Ordinal: 0, SnapshotID: "evs_recovery",
			Caption: "decision baseline", ReferenceRole: EvidenceReferenceRoleDecisionSupport,
		}},
		CopyLineage: []EvidenceRecoveryCopyLineage{{
			SnapshotID: "evs_recovery", CopiedFromSnapshotID: "evs_deleted", CopyReason: "",
		}},
	}
	if err := validateEvidenceRecoveryInventory(registry, inventory); err != nil {
		t.Fatalf("validateEvidenceRecoveryInventory() error = %v", err)
	}
}

func TestRecoveryInventoryPreservesWitnessedSourceAuthorizationFloor(t *testing.T) {
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
	inventory := EvidenceRecoveryInventory{
		Payloads: []EvidenceRecoveryPayload{{Key: kind.descriptor.Key, Digest: snapshot.Hash(), CanonicalPayload: snapshot.Bytes()}},
		Snapshots: []EvidenceRecoverySnapshot{{
			RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: snapshot.Envelope(), PayloadDigest: snapshot.Hash(),
		}},
		CaptureIntents: []EvidenceRecoveryCaptureIntent{}, RevisionReferences: []EvidenceRecoveryRevisionReference{},
		CopyLineage: []EvidenceRecoveryCopyLineage{},
	}
	if err := validateEvidenceRecoveryInventory(registry, inventory); err != nil {
		t.Fatalf("validateEvidenceRecoveryInventory(tombstoned source floor) error = %v", err)
	}
	got := inventory.Snapshots[0].Envelope.Authorization
	if !reflect.DeepEqual(got, tombstoned) || got.FinalFloor == nil || got.LastLiveScope == nil {
		t.Fatalf("replayed source authorization = %#v, want %#v", got, tombstoned)
	}
}

func TestRecoveryInventoryRejectsUnknownKind(t *testing.T) {
	kind, _ := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	envelope := kind.snapshot.Envelope()
	envelope.Key = KindKey{Kind: KindName("comparison.result"), SchemaVersion: 99}
	inventory := EvidenceRecoveryInventory{
		Payloads:       []EvidenceRecoveryPayload{{Key: kind.descriptor.Key, Digest: kind.snapshot.Hash(), CanonicalPayload: kind.snapshot.Bytes()}},
		Snapshots:      []EvidenceRecoverySnapshot{{RecordID: "rec_recovery", SnapshotID: "evs_recovery", Envelope: envelope, PayloadDigest: kind.snapshot.Hash()}},
		CaptureIntents: []EvidenceRecoveryCaptureIntent{}, RevisionReferences: []EvidenceRecoveryRevisionReference{}, CopyLineage: []EvidenceRecoveryCopyLineage{},
	}
	if err := validateEvidenceRecoveryInventory(registry, inventory); !errors.Is(err, ErrUnknownKindVersion) && !errors.Is(err, ErrKindNotRegistered) {
		t.Fatalf("validateEvidenceRecoveryInventory(unknown kind) error = %v", err)
	}
}

func TestRecoveryInventoryRejectsUnreferencedPayload(t *testing.T) {
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
	if err := validateEvidenceRecoveryInventory(registry, inventory); !errors.Is(err, ErrInvalidRecoveryInventory) {
		t.Fatalf("validateEvidenceRecoveryInventory(unreferenced payload) error = %v", err)
	}
}

func TestRecoveryInventoryRejectsNoncanonicalSnapshotTimestamps(t *testing.T) {
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
				if err := validateEvidenceRecoveryInventory(registry, inventory); !errors.Is(err, ErrInvalidRecoveryInventory) {
					t.Fatalf("validateEvidenceRecoveryInventory(noncanonical timestamp) error = %v", err)
				}
			})
		}
	}
}

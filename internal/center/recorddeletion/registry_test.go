package recorddeletion

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestNewRegistryRejectsNilDuplicateExtraAndOverlappingAdapters(t *testing.T) {
	t.Parallel()

	attachmentSurfaces := RecordAttachmentsSurfaceNames()
	attachments := newReadinessAdapterStub(t, AdapterNameRecordAttachments, attachmentSurfaces, true, 1)
	evidence := newReadinessAdapterStub(t, AdapterNameRecordEvidence, []SurfaceName{"fixture.evidence"}, true, 2)
	var nilAdapter *readinessAdapterStub

	tests := []struct {
		name     string
		adapters []Adapter
	}{
		{name: "nil adapter", adapters: []Adapter{nil}},
		{name: "typed nil adapter", adapters: []Adapter{nilAdapter}},
		{name: "duplicate adapter", adapters: []Adapter{attachments, attachments}},
		{name: "extra adapter", adapters: []Adapter{&readinessAdapterStub{descriptor: AdapterDescriptor{name: "record_unknown", surfaces: []SurfaceName{"fixture.unknown"}}}}},
		{name: "invalid descriptor", adapters: []Adapter{&readinessAdapterStub{descriptor: AdapterDescriptor{name: AdapterNameRecordAttachments}}}},
		{
			name: "overlapping surface",
			adapters: []Adapter{
				attachments,
				&readinessAdapterStub{descriptor: mustAdapterDescriptor(t, AdapterNameRecordEvidence, []SurfaceName{attachmentSurfaces[0]}), health: mustHealthSnapshot(t, true, 3)},
			},
		},
		{name: "valid controls", adapters: []Adapter{attachments, evidence}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRegistry(tt.adapters)
			if tt.name == "valid controls" {
				if err != nil {
					t.Fatalf("NewRegistry() error = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidAdapterRegistry) {
				t.Fatalf("NewRegistry() error = %v, want ErrInvalidAdapterRegistry", err)
			}
		})
	}
}

func TestRegistryReadinessSnapshotIsDeterministicAndDefensive(t *testing.T) {
	t.Parallel()

	forward := completeReadinessAdapters(t)
	reverse := append([]Adapter(nil), forward...)
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}

	forwardRegistry, err := NewRegistry(forward)
	if err != nil {
		t.Fatalf("NewRegistry(forward) error = %v", err)
	}
	reverseRegistry, err := NewRegistry(reverse)
	if err != nil {
		t.Fatalf("NewRegistry(reverse) error = %v", err)
	}
	forwardSnapshot, err := forwardRegistry.ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot(forward) error = %v", err)
	}
	reverseSnapshot, err := reverseRegistry.ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot(reverse) error = %v", err)
	}
	if !forwardSnapshot.Ready() || !reverseSnapshot.Ready() {
		t.Fatalf("complete snapshots ready = forward:%t reverse:%t", forwardSnapshot.Ready(), reverseSnapshot.Ready())
	}
	if got := forwardSnapshot.MissingAdapterNames(); len(got) != 0 {
		t.Fatalf("complete snapshot missing adapters = %#v", got)
	}
	if forwardSnapshot.Digest() != reverseSnapshot.Digest() {
		t.Fatalf("readiness digest depends on registration order: %x != %x", forwardSnapshot.Digest(), reverseSnapshot.Digest())
	}

	adapters := forwardSnapshot.Adapters()
	wantNames := RequiredAdapterNames()
	gotNames := make([]AdapterName, 0, len(adapters))
	for _, adapter := range adapters {
		gotNames = append(gotNames, adapter.Name())
		if len(adapter.Surfaces()) == 0 || adapter.Health().Revision() == 0 {
			t.Fatalf("adapter snapshot = %#v", adapter)
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("snapshot adapter names = %#v, want %#v", gotNames, wantNames)
	}

	adapters[0].surfaces[0] = "tampered"
	if got := forwardSnapshot.Adapters()[0].Surfaces(); reflect.DeepEqual(got, []SurfaceName{"tampered"}) || got[0] == "tampered" {
		t.Fatalf("snapshot surfaces changed through caller mutation: %#v", got)
	}
}

func TestRegistryReadinessDigestChangesWithHealthProof(t *testing.T) {
	t.Parallel()

	firstAdapters := completeReadinessAdapters(t)
	secondAdapters := completeReadinessAdapters(t)
	secondAdapters[3].(*readinessAdapterStub).health = mustHealthSnapshot(t, true, 99)

	firstRegistry, err := NewRegistry(firstAdapters)
	if err != nil {
		t.Fatalf("NewRegistry(first) error = %v", err)
	}
	secondRegistry, err := NewRegistry(secondAdapters)
	if err != nil {
		t.Fatalf("NewRegistry(second) error = %v", err)
	}
	first, err := firstRegistry.ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot(first) error = %v", err)
	}
	second, err := secondRegistry.ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot(second) error = %v", err)
	}
	if first.Digest() == second.Digest() {
		t.Fatalf("health proof change did not change readiness digest: %x", first.Digest())
	}
}

func TestRegistryCapturesDescriptorAtConstruction(t *testing.T) {
	t.Parallel()

	adapter := newReadinessAdapterStub(
		t,
		AdapterNameRecordAttachments,
		RecordAttachmentsSurfaceNames(),
		true,
		1,
	)
	registry, err := NewRegistry([]Adapter{adapter})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	adapter.descriptor.surfaces[0] = "tampered.after_registration"

	snapshot, err := registry.ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot() error = %v", err)
	}
	if got := snapshot.Adapters()[0].Surfaces(); !reflect.DeepEqual(got, RecordAttachmentsSurfaceNames()) {
		t.Fatalf("registered descriptor surfaces = %#v, want construction-time snapshot", got)
	}
}

func TestRegistryReadinessRejectsContextCancelledByLastHealthProbe(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	adapters := completeReadinessAdapters(t)
	adapters[len(adapters)-1].(*readinessAdapterStub).afterHealth = cancel
	registry, err := NewRegistry(adapters)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	snapshot, err := registry.RequireReady(ctx)
	if !errors.Is(err, ErrDeletionSafetyUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("RequireReady() error = %v, want deletion safety unavailable and context canceled", err)
	}
	if snapshot.Ready() {
		t.Fatal("cancelled readiness snapshot reports ready")
	}
}

func TestRegistryRequireReadyFailsClosedBeforeMutation(t *testing.T) {
	t.Parallel()

	coreOnly := newReadinessAdapterStub(t, AdapterNameRecordCore, RecordCoreSurfaceNames(), true, 1)
	registries := []struct {
		name     string
		adapters []Adapter
		missing  []AdapterName
	}{
		{name: "empty registry", missing: RequiredAdapterNames()},
		{name: "core only with empty content state", adapters: []Adapter{coreOnly}, missing: RequiredAdapterNames()[1:]},
	}
	for _, tt := range registries {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry, err := NewRegistry(tt.adapters)
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			mutationCalls := 0
			snapshot, err := registry.RequireReady(context.Background())
			if err == nil || !errors.Is(err, ErrDeletionSafetyUnavailable) {
				t.Fatalf("RequireReady() error = %v, want ErrDeletionSafetyUnavailable", err)
			}
			if snapshot.Ready() {
				t.Fatal("incomplete registry reports ready")
			}
			if !reflect.DeepEqual(snapshot.MissingAdapterNames(), tt.missing) {
				t.Fatalf("missing adapters = %#v, want %#v", snapshot.MissingAdapterNames(), tt.missing)
			}
			if err == nil {
				mutationCalls++
			}
			if mutationCalls != 0 {
				t.Fatalf("readiness failure allowed %d mutation calls", mutationCalls)
			}
		})
	}
}

func TestRegistryRequireReadyRejectsUnhealthyErrorAndInvalidSnapshots(t *testing.T) {
	t.Parallel()

	wantHealthErr := errors.New("health dependency unavailable")
	tests := []struct {
		name   string
		mutate func([]Adapter)
		wantIs error
	}{
		{
			name: "unhealthy adapter",
			mutate: func(adapters []Adapter) {
				adapters[4].(*readinessAdapterStub).health = mustHealthSnapshot(t, false, 30)
			},
		},
		{
			name: "health error",
			mutate: func(adapters []Adapter) {
				adapters[4].(*readinessAdapterStub).err = wantHealthErr
			},
			wantIs: wantHealthErr,
		},
		{
			name: "invalid health snapshot",
			mutate: func(adapters []Adapter) {
				adapters[4].(*readinessAdapterStub).health = AdapterHealthSnapshot{}
			},
			wantIs: ErrInvalidAdapterHealthSnapshot,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			adapters := completeReadinessAdapters(t)
			tt.mutate(adapters)
			registry, err := NewRegistry(adapters)
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			snapshot, err := registry.RequireReady(context.Background())
			if err == nil || !errors.Is(err, ErrDeletionSafetyUnavailable) {
				t.Fatalf("RequireReady() error = %v, want ErrDeletionSafetyUnavailable", err)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("RequireReady() error = %v, want wrapped %v", err, tt.wantIs)
			}
			if snapshot.Ready() {
				t.Fatal("failed health snapshot reports ready")
			}
		})
	}
}

func TestRegistryRequireReadyAcceptsExplicitCompleteFixture(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(completeReadinessAdapters(t))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	snapshot, err := registry.RequireReady(context.Background())
	if err != nil {
		t.Fatalf("RequireReady() error = %v", err)
	}
	if !snapshot.Ready() || len(snapshot.Adapters()) != len(RequiredAdapterNames()) {
		t.Fatalf("complete fixture snapshot = ready:%t adapters:%d", snapshot.Ready(), len(snapshot.Adapters()))
	}
}

type readinessAdapterStub struct {
	descriptor  AdapterDescriptor
	health      AdapterHealthSnapshot
	err         error
	afterHealth func()
	healthCalls int
}

func (adapter *readinessAdapterStub) Descriptor() AdapterDescriptor {
	if adapter == nil {
		return AdapterDescriptor{}
	}
	return adapter.descriptor
}

func (adapter *readinessAdapterStub) HealthSnapshot(context.Context) (AdapterHealthSnapshot, error) {
	adapter.healthCalls++
	if adapter.afterHealth != nil {
		adapter.afterHealth()
	}
	return adapter.health, adapter.err
}

func completeReadinessAdapters(t *testing.T) []Adapter {
	t.Helper()

	names := RequiredAdapterNames()
	adapters := make([]Adapter, 0, len(names))
	for index, name := range names {
		surfaces := []SurfaceName{SurfaceName(fmt.Sprintf("fixture.%s", name))}
		switch name {
		case AdapterNameRecordCore:
			surfaces = RecordCoreSurfaceNames()
		case AdapterNameRecordAttachments:
			surfaces = RecordAttachmentsSurfaceNames()
		}
		adapters = append(adapters, newReadinessAdapterStub(t, name, surfaces, true, byte(index+1)))
	}
	return adapters
}

func newReadinessAdapterStub(
	t *testing.T,
	name AdapterName,
	surfaces []SurfaceName,
	healthy bool,
	proofSeed byte,
) *readinessAdapterStub {
	t.Helper()
	return &readinessAdapterStub{
		descriptor: mustAdapterDescriptor(t, name, surfaces),
		health:     mustHealthSnapshot(t, healthy, proofSeed),
	}
}

func mustAdapterDescriptor(t *testing.T, name AdapterName, surfaces []SurfaceName) AdapterDescriptor {
	t.Helper()
	descriptor, err := NewAdapterDescriptor(name, surfaces)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(%q) error = %v", name, err)
	}
	return descriptor
}

func mustHealthSnapshot(t *testing.T, healthy bool, proofSeed byte) AdapterHealthSnapshot {
	t.Helper()
	snapshot, err := NewAdapterHealthSnapshot(healthy, uint64(proofSeed), testHealthProof(proofSeed))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	return snapshot
}

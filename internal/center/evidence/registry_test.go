package evidence

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestRegistryKnownKeysAndLookup(t *testing.T) {
	keys := KnownKindKeys()
	want := []KindKey{
		IPQualityReportV1Key(),
		MonitoringHostV1Key(),
		MonitoringProbeV2Key(),
		MonitoringEventV2Key(),
		SubscriptionCostV1Key(),
		CommandAuditV1Key(),
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("KnownKindKeys() = %#v, want %#v", keys, want)
	}

	kinds := make([]Kind, 0, len(keys))
	for _, key := range keys {
		kinds = append(kinds, &kindStub{descriptor: testDescriptor(t, key)})
	}
	registry, err := NewRegistry(kinds)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	for _, key := range keys {
		kind, err := registry.Lookup(key.Kind, key.SchemaVersion)
		if err != nil {
			t.Fatalf("Lookup(%q) error = %v", key, err)
		}
		if got := kind.Descriptor().Key; got != key {
			t.Fatalf("Lookup(%q).Descriptor().Key = %q", key, got)
		}
	}
	if _, err := registry.Lookup(KindMonitoringProbe, 1); !errors.Is(err, ErrUnknownKindVersion) {
		t.Fatalf("Lookup(monitoring.probe/v1) error = %v, want ErrUnknownKindVersion", err)
	}
	if _, err := registry.Lookup(KindName("route.quality"), 1); !errors.Is(err, ErrKindNotRegistered) {
		t.Fatalf("Lookup(route.quality/v1) error = %v, want ErrKindNotRegistered", err)
	}
}

func TestRegistryStandardKeyAccessorsReturnIndependentValues(t *testing.T) {
	accessors := []func() KindKey{
		IPQualityReportV1Key,
		MonitoringHostV1Key,
		MonitoringProbeV2Key,
		MonitoringEventV2Key,
		SubscriptionCostV1Key,
		CommandAuditV1Key,
	}
	for _, accessor := range accessors {
		first := accessor()
		first.Kind = "mutated"
		first.SchemaVersion = 99
		second := accessor()
		if second.Kind == first.Kind || second.SchemaVersion == first.SchemaVersion {
			t.Fatalf("standard key accessor retained caller mutation: first=%#v second=%#v", first, second)
		}
	}
}

func TestRegistryRejectsDuplicateUnknownAndMissingConformance(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringHostV1Key())
	tests := []struct {
		name  string
		kinds []Kind
		want  error
	}{
		{
			name:  "duplicate",
			kinds: []Kind{&kindStub{descriptor: descriptor}, &kindStub{descriptor: descriptor}},
			want:  ErrInvalidKindRegistry,
		},
		{
			name: "unknown version",
			kinds: []Kind{&kindStub{descriptor: Descriptor{
				Key:         KindKey{Kind: KindMonitoringHost, SchemaVersion: 2},
				Fields:      descriptor.Fields,
				Conformance: descriptor.Conformance,
			}}},
			want: ErrUnknownKindVersion,
		},
		{
			name: "missing conformance",
			kinds: []Kind{&kindStub{descriptor: Descriptor{
				Key:    descriptor.Key,
				Fields: descriptor.Fields,
			}}},
			want: ErrInvalidKindDescriptor,
		},
		{
			name:  "typed nil",
			kinds: []Kind{(*kindStub)(nil)},
			want:  ErrInvalidKindRegistry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRegistry(tt.kinds); !errors.Is(err, tt.want) {
				t.Fatalf("NewRegistry() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRegistryRejectsForbiddenMisclassificationAndAmbiguousPaths(t *testing.T) {
	descriptor := testDescriptor(t, CommandAuditV1Key())
	for index := range descriptor.Fields {
		if descriptor.Fields[index].Path == "stdout" {
			descriptor.Fields[index].Sensitivity = SensitivityNormal
		}
	}
	if err := descriptor.Validate(); !errors.Is(err, ErrInvalidKindDescriptor) {
		t.Fatalf("Descriptor.Validate(misclassified stdout) error = %v, want ErrInvalidKindDescriptor", err)
	}

	descriptor = testDescriptor(t, MonitoringProbeV2Key())
	descriptor.Fields = append(descriptor.Fields, FieldDefinition{Path: "tags", Sensitivity: SensitivityNormal})
	if err := descriptor.Validate(); !errors.Is(err, ErrInvalidKindDescriptor) {
		t.Fatalf("Descriptor.Validate(parent/child paths) error = %v, want ErrInvalidKindDescriptor", err)
	}
}

func TestRegistryRejectsCompoundForbiddenFieldNames(t *testing.T) {
	for _, path := range []string{
		"command_output",
		"output_preview",
		"command_details",
		"url_query",
		"url_fragment",
		"command_output_preview",
		"command_stdout_preview",
		"archived_url_query_value",
	} {
		t.Run(path, func(t *testing.T) {
			descriptor := testDescriptor(t, CommandAuditV1Key())
			descriptor.Fields = append(descriptor.Fields, FieldDefinition{
				Path:        path,
				Sensitivity: SensitivityNormal,
			})
			if err := descriptor.Validate(); !errors.Is(err, ErrInvalidKindDescriptor) {
				t.Fatalf("Descriptor.Validate(%q) error = %v, want ErrInvalidKindDescriptor", path, err)
			}
		})
	}
}

func TestRegistryFreezesDescriptorMetadata(t *testing.T) {
	stub := &kindStub{descriptor: testDescriptor(t, MonitoringHostV1Key())}
	registry, err := NewRegistry([]Kind{stub})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	stub.descriptor.Conformance.RendererVersion = "mutated.v1"
	stub.descriptor.Fields[0].Path = "mutated"

	kind, err := registry.LookupKey(MonitoringHostV1Key())
	if err != nil {
		t.Fatalf("LookupKey() error = %v", err)
	}
	descriptor := kind.Descriptor()
	if descriptor.Conformance.RendererVersion != "renderer.v1" || descriptor.Fields[0].Path == "mutated" {
		t.Fatalf("registered descriptor drifted: %#v", descriptor)
	}
	descriptor.Fields[0].Path = "caller_mutated"
	if got := kind.Descriptor().Fields[0].Path; got == "caller_mutated" {
		t.Fatalf("Descriptor() returned mutable registry storage: %q", got)
	}
}

func TestRegistrySupportsConcurrentReadOnlyUse(t *testing.T) {
	stub := &kindStub{descriptor: testDescriptor(t, MonitoringHostV1Key())}
	registry, err := NewRegistry([]Kind{stub})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				kind, lookupErr := registry.LookupKey(MonitoringHostV1Key())
				if lookupErr != nil || kind.Descriptor().Key != MonitoringHostV1Key() || len(registry.Keys()) != 1 {
					t.Errorf("concurrent registry read: kind=%v lookupErr=%v keys=%v", kind, lookupErr, registry.Keys())
					return
				}
			}
		}()
	}
	wait.Wait()
}

type kindStub struct {
	descriptor       Descriptor
	preview          Preview
	authorization    AuthorizationScope
	snapshot         CanonicalSnapshot
	previewCalled    bool
	previewErr       error
	authorizeCalled  bool
	selectionChecked bool
	captureCalled    bool
	exportCalls      int
	exportMaterial   *ExportMaterial
	summary          *Summary
	comparison       *Comparison
}

func (stub *kindStub) Descriptor() Descriptor { return stub.descriptor }

func (stub *kindStub) ValidateSelection(context.Context, ActorScope, Selection) error {
	stub.selectionChecked = true
	return nil
}

func (stub *kindStub) PreviewCapture(context.Context, ActorScope, Selection) (Preview, error) {
	stub.previewCalled = true
	return stub.preview, stub.previewErr
}

func (stub *kindStub) Capture(context.Context, ActorScope, Intent) (CanonicalSnapshot, error) {
	stub.captureCalled = true
	return stub.snapshot, nil
}

func (stub *kindStub) Authorize(context.Context, ActorScope, Selection) (AuthorizationScope, error) {
	stub.authorizeCalled = true
	return stub.authorization, nil
}

func (stub *kindStub) Summarize(CanonicalSnapshot) Summary {
	if stub.summary != nil {
		return *stub.summary
	}
	return Summary{Key: stub.descriptor.Key, RendererVersion: stub.descriptor.Conformance.RendererVersion, Title: "summary", SearchText: "summary", ReadModel: map[string]any{"version": "test_read_model/v1", "status": "ok"}}
}

func (stub *kindStub) Compare(CanonicalSnapshot, CanonicalSnapshot, Alignment) Comparison {
	if stub.comparison != nil {
		return *stub.comparison
	}
	return Comparison{Key: stub.descriptor.Key, Compatible: true, Values: map[string]any{"equal": true}}
}

func (stub *kindStub) Export(CanonicalSnapshot, ExportMode) ExportMaterial {
	stub.exportCalls++
	if stub.exportMaterial != nil {
		return *stub.exportMaterial
	}
	return ExportMaterial{Key: stub.descriptor.Key, MediaType: "application/json", Filename: "evidence.json", Bytes: []byte("{}")}
}

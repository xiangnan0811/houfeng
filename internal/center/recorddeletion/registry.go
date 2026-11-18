package recorddeletion

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"reflect"
)

const readinessSnapshotVersionV1 uint64 = 1

const readinessSnapshotDomainV1 = "houfeng.record-deletion.adapter-readiness.v1"

type registeredAdapter struct {
	adapter    Adapter
	descriptor AdapterDescriptor
}

type Registry struct {
	adapters map[AdapterName]registeredAdapter
}

func NewRegistry(adapters []Adapter) (Registry, error) {
	registry := Registry{adapters: make(map[AdapterName]registeredAdapter, len(adapters))}
	surfaceOwners := make(map[SurfaceName]AdapterName)
	for _, adapter := range adapters {
		if nilAdapter(adapter) {
			return Registry{}, fmt.Errorf("%w: nil adapter", ErrInvalidAdapterRegistry)
		}
		descriptor := adapter.Descriptor()
		if err := descriptor.validate(); err != nil {
			return Registry{}, fmt.Errorf("%w: %w", ErrInvalidAdapterRegistry, err)
		}
		descriptor = AdapterDescriptor{name: descriptor.name, surfaces: descriptor.Surfaces()}
		name := descriptor.Name()
		if _, exists := registry.adapters[name]; exists {
			return Registry{}, fmt.Errorf("%w: duplicate adapter %q", ErrInvalidAdapterRegistry, name)
		}
		for _, surface := range descriptor.surfaces {
			if owner, exists := surfaceOwners[surface]; exists {
				return Registry{}, fmt.Errorf(
					"%w: surface %q owned by %q and %q",
					ErrInvalidAdapterRegistry,
					surface,
					owner,
					name,
				)
			}
			surfaceOwners[surface] = name
		}
		registry.adapters[name] = registeredAdapter{adapter: adapter, descriptor: descriptor}
	}
	return registry, nil
}

type ReadinessSnapshot struct {
	adapters []AdapterReadinessSnapshot
	missing  []AdapterName
	ready    bool
	digest   [sha256.Size]byte
}

func (snapshot ReadinessSnapshot) Adapters() []AdapterReadinessSnapshot {
	cloned := make([]AdapterReadinessSnapshot, len(snapshot.adapters))
	for index, adapter := range snapshot.adapters {
		cloned[index] = AdapterReadinessSnapshot{
			name:     adapter.name,
			surfaces: append([]SurfaceName(nil), adapter.surfaces...),
			health:   adapter.health,
		}
	}
	return cloned
}

func (snapshot ReadinessSnapshot) MissingAdapterNames() []AdapterName {
	return append([]AdapterName(nil), snapshot.missing...)
}

func (snapshot ReadinessSnapshot) Ready() bool {
	return snapshot.ready
}

func (snapshot ReadinessSnapshot) Digest() [sha256.Size]byte {
	return snapshot.digest
}

func (registry Registry) ReadinessSnapshot(ctx context.Context) (ReadinessSnapshot, error) {
	if ctx == nil {
		return ReadinessSnapshot{}, fmt.Errorf("%w: nil context", ErrDeletionSafetyUnavailable)
	}

	snapshot := ReadinessSnapshot{ready: true}
	for _, name := range requiredAdapterNames {
		registered, exists := registry.adapters[name]
		if !exists {
			snapshot.ready = false
			snapshot.missing = append(snapshot.missing, name)
			continue
		}
		if err := ctx.Err(); err != nil {
			return ReadinessSnapshot{}, fmt.Errorf("%w: adapter health context: %w", ErrDeletionSafetyUnavailable, err)
		}
		health, err := registered.adapter.HealthSnapshot(ctx)
		if err != nil {
			return ReadinessSnapshot{}, fmt.Errorf(
				"%w: adapter %q health: %w",
				ErrDeletionSafetyUnavailable,
				name,
				err,
			)
		}
		if err := ctx.Err(); err != nil {
			return ReadinessSnapshot{}, fmt.Errorf("%w: adapter health context: %w", ErrDeletionSafetyUnavailable, err)
		}
		if err := health.validate(); err != nil {
			return ReadinessSnapshot{}, fmt.Errorf(
				"%w: adapter %q health: %w",
				ErrDeletionSafetyUnavailable,
				name,
				err,
			)
		}
		if !health.Healthy() {
			snapshot.ready = false
		}
		snapshot.adapters = append(snapshot.adapters, AdapterReadinessSnapshot{
			name:     name,
			surfaces: registered.descriptor.Surfaces(),
			health:   health,
		})
	}
	snapshot.digest = digestReadinessSnapshot(snapshot)
	return snapshot, nil
}

func (registry Registry) RequireReady(ctx context.Context) (ReadinessSnapshot, error) {
	snapshot, err := registry.ReadinessSnapshot(ctx)
	if err != nil {
		return ReadinessSnapshot{}, err
	}
	if !snapshot.Ready() {
		return snapshot, ErrDeletionSafetyUnavailable
	}
	return snapshot, nil
}

func digestReadinessSnapshot(snapshot ReadinessSnapshot) [sha256.Size]byte {
	payload := make([]byte, 0, 1024)
	payload = appendLengthPrefixed(payload, readinessSnapshotDomainV1)
	payload = appendUint64(payload, readinessSnapshotVersionV1)

	adapterIndex := 0
	for _, requiredName := range requiredAdapterNames {
		payload = appendLengthPrefixed(payload, string(requiredName))
		if adapterIndex >= len(snapshot.adapters) || snapshot.adapters[adapterIndex].name != requiredName {
			payload = append(payload, 0)
			continue
		}
		adapter := snapshot.adapters[adapterIndex]
		adapterIndex++
		payload = append(payload, 1)
		payload = appendUint64(payload, uint64(len(adapter.surfaces)))
		for _, surface := range adapter.surfaces {
			payload = appendLengthPrefixed(payload, string(surface))
		}
		if adapter.health.Healthy() {
			payload = append(payload, 1)
		} else {
			payload = append(payload, 0)
		}
		payload = appendUint64(payload, adapter.health.Revision())
		proof := adapter.health.ProofDigest()
		payload = append(payload, proof[:]...)
	}
	return sha256.Sum256(payload)
}

// digestAdapterPreviewSnapshots is the shared codec for V1 registry previews
// and issuer previews. The original preview domain and version are retained.
func digestAdapterPreviewSnapshots(previews map[AdapterName]AdapterPreviewSnapshot) (adapterPreviewAggregate, error) {
	payload := make([]byte, 0, 1024)
	payload = appendLengthPrefixed(payload, deletionAdapterPreviewDomainV2)
	payload = appendUint64(payload, 2)
	aggregate := adapterPreviewAggregate{
		onlinePurgeScopes: make([]AdapterName, 0, len(requiredAdapterNames)),
		survivingCopies:   make([]SurvivingCopySummary, 0),
	}
	for _, name := range requiredAdapterNames {
		snapshot, ok := previews[name]
		if !ok || snapshot.Validate() != nil {
			return adapterPreviewAggregate{}, fmt.Errorf("%w: adapter %q preview snapshot", ErrInvalidDeletionPreview, name)
		}
		aggregate.onlinePurgeScopes = append(aggregate.onlinePurgeScopes, name)
		payload = appendLengthPrefixed(payload, string(name))
		payload = append(payload, snapshot.DependencyDigest[:]...)
		payload = append(payload, snapshot.ImpactDigest[:]...)
		payload = appendUint64(payload, uint64(len(snapshot.SurvivingCopies)))
		for _, surviving := range snapshot.SurvivingCopies {
			summary := SurvivingCopySummary{
				Scope: name, Kind: surviving.Kind, CountStatus: surviving.CountStatus,
				CopyCount: cloneSurvivingCopyCount(surviving.CopyCount),
			}
			if err := summary.Validate(); err != nil {
				return adapterPreviewAggregate{}, fmt.Errorf("%w: adapter %q surviving copy owner", ErrInvalidDeletionPreview, name)
			}
			payload = appendLengthPrefixed(payload, string(summary.Kind))
			payload = appendLengthPrefixed(payload, string(summary.CountStatus))
			if summary.CountStatus == SurvivingCopyCountKnown {
				payload = appendUint64(payload, *summary.CopyCount)
			}
			aggregate.survivingCopies = append(aggregate.survivingCopies, summary)
		}
	}
	aggregate.digest = sha256.Sum256(payload)
	return aggregate, nil
}

func appendLengthPrefixed(target []byte, value string) []byte {
	target = appendUint64(target, uint64(len(value)))
	return append(target, value...)
}

func appendUint64(target []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(target, encoded[:]...)
}

func nilAdapter(adapter Adapter) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

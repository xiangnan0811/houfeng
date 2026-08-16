package evidence

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var knownKindKeys = []KindKey{
	IPQualityReportV1Key(),
	MonitoringHostV1Key(),
	MonitoringProbeV2Key(),
	MonitoringEventV2Key(),
	SubscriptionCostV1Key(),
	CommandAuditV1Key(),
}

type registeredKind struct {
	kind       Kind
	descriptor Descriptor
}

// Registry is immutable after NewRegistry returns. Lookup, LookupKey, and Keys
// are safe for concurrent read-only use; the registered Kind implementations
// retain responsibility for their own concurrency safety.
type Registry struct {
	kinds map[KindKey]registeredKind
}

func (key KindKey) String() string {
	return string(key.Kind) + "/v" + strconv.FormatUint(uint64(key.SchemaVersion), 10)
}

func KnownKindKeys() []KindKey {
	return append([]KindKey(nil), knownKindKeys...)
}

func ParseKindKey(value string) (KindKey, error) {
	separator := strings.LastIndex(value, "/v")
	if separator <= 0 || separator+2 >= len(value) {
		return KindKey{}, fmt.Errorf("%w: key", ErrKindNotRegistered)
	}
	version, err := strconv.ParseUint(value[separator+2:], 10, 16)
	if err != nil || version == 0 || strconv.FormatUint(version, 10) != value[separator+2:] {
		return KindKey{}, fmt.Errorf("%w: schema version", ErrUnknownKindVersion)
	}
	key := KindKey{Kind: KindName(value[:separator]), SchemaVersion: SchemaVersion(version)}
	if err := validateKnownKindKey(key); err != nil {
		return KindKey{}, err
	}
	return key, nil
}

func (descriptor Descriptor) Validate() error {
	if err := validateKnownKindKey(descriptor.Key); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidKindDescriptor, err)
	}
	metadata := descriptor.Conformance
	if metadata.CanonicalizationVersion != CanonicalizationVersionV1 ||
		metadata.ForbiddenCorpusVersion != ForbiddenCorpusVersionV1 ||
		!validRegistryToken(metadata.RendererVersion) ||
		metadata.MaxCanonicalBytes == 0 || metadata.MaxCanonicalBytes > MaxCanonicalPayloadBytes {
		return fmt.Errorf("%w: conformance metadata", ErrInvalidKindDescriptor)
	}
	if len(descriptor.Fields) == 0 {
		return fmt.Errorf("%w: empty field schema", ErrInvalidKindDescriptor)
	}
	seen := make(map[string]struct{}, len(descriptor.Fields))
	paths := make([]string, 0, len(descriptor.Fields))
	hasNormalField := false
	for _, field := range descriptor.Fields {
		if !validFieldPath(field.Path) || !knownSensitivity(field.Sensitivity) || !knownFieldFormat(field.Format) {
			return fmt.Errorf("%w: field schema", ErrInvalidKindDescriptor)
		}
		if _, exists := seen[field.Path]; exists {
			return fmt.Errorf("%w: duplicate field %q", ErrInvalidKindDescriptor, field.Path)
		}
		if forbiddenFieldPath(field.Path) && field.Sensitivity != SensitivityForbidden {
			return fmt.Errorf("%w: forbidden field %q is misclassified", ErrInvalidKindDescriptor, field.Path)
		}
		seen[field.Path] = struct{}{}
		paths = append(paths, field.Path)
		if field.Sensitivity == SensitivityNormal {
			hasNormalField = true
		}
	}
	if !hasNormalField {
		return fmt.Errorf("%w: no normal field", ErrInvalidKindDescriptor)
	}
	for left, path := range paths {
		for right, candidate := range paths {
			if left != right && strings.HasPrefix(candidate, path+".") {
				return fmt.Errorf("%w: ambiguous fields %q and %q", ErrInvalidKindDescriptor, path, candidate)
			}
		}
	}
	return nil
}

func NewRegistry(kinds []Kind) (Registry, error) {
	registry := Registry{kinds: make(map[KindKey]registeredKind, len(kinds))}
	for _, kind := range kinds {
		if nilKind(kind) {
			return Registry{}, fmt.Errorf("%w: nil kind", ErrInvalidKindRegistry)
		}
		descriptor := kind.Descriptor()
		if err := descriptor.Validate(); err != nil {
			return Registry{}, fmt.Errorf("%w: %w", ErrInvalidKindRegistry, err)
		}
		descriptor = cloneDescriptor(descriptor)
		if _, exists := registry.kinds[descriptor.Key]; exists {
			return Registry{}, fmt.Errorf("%w: duplicate key %q", ErrInvalidKindRegistry, descriptor.Key)
		}
		registry.kinds[descriptor.Key] = registeredKind{kind: kind, descriptor: descriptor}
	}
	return registry, nil
}

func (registry Registry) Lookup(name KindName, version SchemaVersion) (Kind, error) {
	key := KindKey{Kind: name, SchemaVersion: version}
	if err := validateKnownKindKey(key); err != nil {
		return nil, err
	}
	registered, exists := registry.kinds[key]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrKindNotRegistered, key)
	}
	return registered, nil
}

func (registry Registry) LookupKey(key KindKey) (Kind, error) {
	return registry.Lookup(key.Kind, key.SchemaVersion)
}

func (registry Registry) Keys() []KindKey {
	keys := make([]KindKey, 0, len(registry.kinds))
	for key := range registry.kinds {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left].String() < keys[right].String() })
	return keys
}

func validateKnownKindKey(key KindKey) error {
	nameKnown := false
	for _, known := range knownKindKeys {
		if known.Kind != key.Kind {
			continue
		}
		nameKnown = true
		if known.SchemaVersion == key.SchemaVersion {
			return nil
		}
	}
	if nameKnown {
		return fmt.Errorf("%w: %s", ErrUnknownKindVersion, key)
	}
	return fmt.Errorf("%w: %s", ErrKindNotRegistered, key)
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	descriptor.Fields = append([]FieldDefinition(nil), descriptor.Fields...)
	return descriptor
}

func nilKind(kind Kind) bool {
	if kind == nil {
		return true
	}
	value := reflect.ValueOf(kind)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (registered registeredKind) Descriptor() Descriptor {
	return cloneDescriptor(registered.descriptor)
}

func (registered registeredKind) ValidateSelection(ctx context.Context, actor ActorScope, selection Selection) error {
	return registered.kind.ValidateSelection(ctx, actor, selection)
}

func (registered registeredKind) PreviewCapture(ctx context.Context, actor ActorScope, selection Selection) (Preview, error) {
	return registered.kind.PreviewCapture(ctx, actor, selection)
}

func (registered registeredKind) Capture(ctx context.Context, actor ActorScope, intent Intent) (CanonicalSnapshot, error) {
	return registered.kind.Capture(ctx, actor, intent)
}

func (registered registeredKind) Authorize(ctx context.Context, actor ActorScope, selection Selection) (AuthorizationScope, error) {
	return registered.kind.Authorize(ctx, actor, selection)
}

func (registered registeredKind) Summarize(snapshot CanonicalSnapshot) Summary {
	return registered.kind.Summarize(snapshot)
}

func (registered registeredKind) Compare(left, right CanonicalSnapshot, alignment Alignment) Comparison {
	return registered.kind.Compare(left, right, alignment)
}

func (registered registeredKind) Export(snapshot CanonicalSnapshot, mode ExportMode) ExportMaterial {
	return registered.kind.Export(snapshot, mode)
}

func knownSensitivity(value Sensitivity) bool {
	switch value {
	case SensitivityNormal, SensitivitySensitiveTopology, SensitivityForbidden:
		return true
	default:
		return false
	}
}

func knownFieldFormat(value FieldFormat) bool {
	return value == FieldFormatText || value == FieldFormatURL
}

func validFieldPath(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, segment := range strings.Split(value, ".") {
		if !validRegistryToken(segment) {
			return false
		}
	}
	return true
}

func validRegistryToken(value string) bool {
	if value == "" || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	last := value[len(value)-1]
	return (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
}

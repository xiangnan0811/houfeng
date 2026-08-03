package records

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
)

const SubjectRegistryVersionV1 uint64 = 1

const (
	SubjectKindVPS                SubjectKind = "vps"
	SubjectKindMonitoringInstance SubjectKind = "monitoring_instance"
	SubjectKindTarget             SubjectKind = "target"
)

const (
	RelationRoleAffected       RelationRole = "affected"
	RelationRoleContext        RelationRole = "context"
	RelationRoleEvidenceSource RelationRole = "evidence_source"
)

var (
	ErrInvalidSubjectReference = errors.New("invalid record subject reference")
	ErrInvalidSubjectSnapshot  = errors.New("invalid record subject identity snapshot")
	ErrInvalidSubjectAdapter   = errors.New("invalid record subject adapter")
	ErrSubjectAdapterNotFound  = errors.New("record subject adapter not found")
	ErrInvalidResolvedSubject  = errors.New("invalid resolved record subject")
)

type SubjectReference struct {
	RegistryVersion uint64
	Kind            SubjectKind
	Role            RelationRole
	SourceID        string
	Primary         bool
}

// SubjectIdentitySnapshot is a server-constructed, immutable display
// identity. It intentionally cannot carry authorization fields.
type SubjectIdentitySnapshot struct {
	kind   SubjectKind
	fields map[string]string
}

func (snapshot SubjectIdentitySnapshot) Kind() SubjectKind {
	return snapshot.kind
}

func (snapshot SubjectIdentitySnapshot) Fields() map[string]string {
	return cloneStringMap(snapshot.fields)
}

type SubjectSourceAdapter interface {
	Kind() SubjectKind
	Resolve(context.Context, recordauth.ActorScope, SubjectReference) (ResolvedSubject, error)
}

type ResolvedSubject struct {
	ProjectID            recordauth.ProjectID
	StableID             string
	IdentitySnapshot     SubjectIdentitySnapshot
	LiveRoute            string
	CaptureAuthorization recordauth.SourceAuthorization
}

type SubjectAdapterRegistry struct {
	adapters map[SubjectKind]SubjectSourceAdapter
}

func NormalizeSubjectReferences(input []SubjectReference) ([]SubjectReference, error) {
	normalized := make([]SubjectReference, 0, len(input))
	seen := make(map[subjectReferenceKey]struct{}, len(input))
	primaryCount := 0
	for _, reference := range input {
		if err := validateSubjectReference(reference); err != nil {
			return nil, err
		}
		key := subjectReferenceKey{kind: reference.Kind, role: reference.Role, sourceID: reference.SourceID}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: duplicate tuple", ErrInvalidSubjectReference)
		}
		seen[key] = struct{}{}
		if reference.Primary {
			primaryCount++
		}
		normalized = append(normalized, reference)
	}
	if primaryCount != 1 {
		return nil, fmt.Errorf("%w: primary cardinality", ErrInvalidSubjectReference)
	}
	return normalized, nil
}

func NewSubjectIdentitySnapshot(kind SubjectKind, input map[string]string) (SubjectIdentitySnapshot, error) {
	if !knownSubjectKind(kind) {
		return SubjectIdentitySnapshot{}, fmt.Errorf("%w: kind", ErrInvalidSubjectSnapshot)
	}
	fields := make(map[string]string, len(input))
	for key, value := range input {
		if !allowedSubjectSnapshotField(kind, key) || !utf8.ValidString(value) {
			return SubjectIdentitySnapshot{}, fmt.Errorf("%w: field", ErrInvalidSubjectSnapshot)
		}
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			return SubjectIdentitySnapshot{}, fmt.Errorf("%w: empty field", ErrInvalidSubjectSnapshot)
		}
		fields[key] = normalized
	}
	if strings.TrimSpace(fields["display_name"]) == "" {
		return SubjectIdentitySnapshot{}, fmt.Errorf("%w: display name", ErrInvalidSubjectSnapshot)
	}
	return SubjectIdentitySnapshot{kind: kind, fields: fields}, nil
}

func NewSubjectAdapterRegistry(input []SubjectSourceAdapter) (SubjectAdapterRegistry, error) {
	registry := SubjectAdapterRegistry{adapters: make(map[SubjectKind]SubjectSourceAdapter, len(input))}
	for _, adapter := range input {
		if nilSubjectSourceAdapter(adapter) {
			return SubjectAdapterRegistry{}, fmt.Errorf("%w: nil adapter", ErrInvalidSubjectAdapter)
		}
		kind := adapter.Kind()
		if !knownSubjectKind(kind) {
			return SubjectAdapterRegistry{}, fmt.Errorf("%w: kind", ErrInvalidSubjectAdapter)
		}
		if _, exists := registry.adapters[kind]; exists {
			return SubjectAdapterRegistry{}, fmt.Errorf("%w: duplicate kind", ErrInvalidSubjectAdapter)
		}
		registry.adapters[kind] = adapter
	}
	return registry, nil
}

func (registry SubjectAdapterRegistry) Resolve(
	ctx context.Context,
	actor recordauth.ActorScope,
	reference SubjectReference,
) (ResolvedSubject, error) {
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return ResolvedSubject{}, fmt.Errorf("%w: actor", ErrInvalidResolvedSubject)
	}
	if err := validateSubjectReference(reference); err != nil {
		return ResolvedSubject{}, err
	}
	adapter, ok := registry.adapters[reference.Kind]
	if !ok {
		return ResolvedSubject{}, fmt.Errorf("%w: kind", ErrSubjectAdapterNotFound)
	}

	resolved, err := adapter.Resolve(ctx, normalizedActor.Clone(), reference)
	if err != nil {
		return ResolvedSubject{}, fmt.Errorf("resolve record subject: %w", err)
	}
	return normalizeResolvedSubject(normalizedActor.ProjectID, reference, resolved)
}

type subjectReferenceKey struct {
	kind     SubjectKind
	role     RelationRole
	sourceID string
}

func validateSubjectReference(reference SubjectReference) error {
	if reference.RegistryVersion != SubjectRegistryVersionV1 {
		return fmt.Errorf("%w: registry version", ErrInvalidSubjectReference)
	}
	if !knownSubjectKind(reference.Kind) {
		return fmt.Errorf("%w: kind", ErrInvalidSubjectReference)
	}
	if !knownRelationRole(reference.Role) {
		return fmt.Errorf("%w: role", ErrInvalidSubjectReference)
	}
	if !validSubjectSourceID(reference.Kind, reference.SourceID) {
		return fmt.Errorf("%w: source identifier", ErrInvalidSubjectReference)
	}
	return nil
}

func normalizeResolvedSubject(
	projectID recordauth.ProjectID,
	reference SubjectReference,
	resolved ResolvedSubject,
) (ResolvedSubject, error) {
	if resolved.ProjectID != projectID || recordauth.ValidateProjectID(resolved.ProjectID) != nil {
		return ResolvedSubject{}, fmt.Errorf("%w: project", ErrInvalidResolvedSubject)
	}
	if resolved.StableID != reference.SourceID {
		return ResolvedSubject{}, fmt.Errorf("%w: stable identifier", ErrInvalidResolvedSubject)
	}

	snapshot, err := NewSubjectIdentitySnapshot(resolved.IdentitySnapshot.Kind(), resolved.IdentitySnapshot.Fields())
	if err != nil || snapshot.Kind() != reference.Kind {
		return ResolvedSubject{}, fmt.Errorf("%w: identity snapshot", ErrInvalidResolvedSubject)
	}

	authorization, err := recordauth.NormalizeSourceAuthorization(resolved.CaptureAuthorization)
	expectedSourceKind, sourceKindOK := recordAuthSourceKind(reference.Kind)
	if err != nil || !sourceKindOK || authorization.Digest != resolved.CaptureAuthorization.Digest ||
		authorization.Kind != expectedSourceKind || authorization.SourceID != reference.SourceID ||
		authorization.CaptureScope.ProjectID != resolved.ProjectID {
		return ResolvedSubject{}, fmt.Errorf("%w: capture authorization", ErrInvalidResolvedSubject)
	}
	if !safeLiveRoute(resolved.LiveRoute) ||
		(authorization.State == recordauth.SourceStateTombstoned && resolved.LiveRoute != "") {
		return ResolvedSubject{}, fmt.Errorf("%w: live route", ErrInvalidResolvedSubject)
	}

	return ResolvedSubject{
		ProjectID:            resolved.ProjectID,
		StableID:             resolved.StableID,
		IdentitySnapshot:     snapshot,
		LiveRoute:            resolved.LiveRoute,
		CaptureAuthorization: authorization,
	}, nil
}

func knownSubjectKind(kind SubjectKind) bool {
	switch kind {
	case SubjectKindVPS, SubjectKindMonitoringInstance, SubjectKindTarget:
		return true
	default:
		return false
	}
}

func knownRelationRole(role RelationRole) bool {
	switch role {
	case RelationRoleAffected, RelationRoleContext, RelationRoleEvidenceSource:
		return true
	default:
		return false
	}
}

func recordAuthSourceKind(kind SubjectKind) (recordauth.SourceKind, bool) {
	switch kind {
	case SubjectKindVPS:
		return recordauth.SourceKindVPS, true
	case SubjectKindMonitoringInstance:
		return recordauth.SourceKindMonitoringInstance, true
	case SubjectKindTarget:
		return recordauth.SourceKindTarget, true
	default:
		return "", false
	}
}

func validSubjectSourceID(kind SubjectKind, value string) bool {
	var prefix string
	switch kind {
	case SubjectKindVPS:
		prefix = "vps_"
	case SubjectKindMonitoringInstance:
		prefix = "mi_"
	case SubjectKindTarget:
		prefix = "tg_"
	default:
		return false
	}
	if len(value) != len(prefix)+16 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for index := len(prefix); index < len(value); index++ {
		if !((value[index] >= '0' && value[index] <= '9') || (value[index] >= 'a' && value[index] <= 'f')) {
			return false
		}
	}
	return true
}

func allowedSubjectSnapshotField(kind SubjectKind, field string) bool {
	if field == "display_name" {
		return true
	}
	switch kind {
	case SubjectKindVPS:
		return field == "provider" || field == "region" || field == "purpose"
	case SubjectKindMonitoringInstance:
		return field == "version"
	case SubjectKindTarget:
		return field == "target_type"
	default:
		return false
	}
}

func nilSubjectSourceAdapter(adapter SubjectSourceAdapter) bool {
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

func safeLiveRoute(route string) bool {
	if route == "" {
		return true
	}
	parsed, err := url.Parse(route)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//")
}

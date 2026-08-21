// Package recordauth owns the closed v1 authorization vocabulary used by
// record resources. It intentionally depends only on the Go standard library
// so HTTP, storage, and future record-domain callers share one policy model.
package recordauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
)

// ProjectID is a closed project identifier. The v1 policy has exactly one
// project; callers must not derive it from an HTTP request or other client
// input.
type ProjectID string

const ProjectIDDefault ProjectID = "default"

// Role is a closed actor role understood by the v1 policy.
type Role string

const (
	RoleProjectAdmin Role = "project_admin"
	RoleViewer       Role = "viewer"
)

// Capability is a closed operation vocabulary. Values are deliberately not
// parsed, normalized, or accepted from arbitrary strings.
type Capability string

const (
	CapabilityRecordRead            Capability = "record.read"
	CapabilityRecordCreate          Capability = "record.create"
	CapabilityRecordUpdate          Capability = "record.update"
	CapabilityRecordDelete          Capability = "record.delete"
	CapabilityRecordPermanentDelete Capability = "record.permanent_delete"

	CapabilityDraftRead    Capability = "draft.read"
	CapabilityDraftCreate  Capability = "draft.create"
	CapabilityDraftUpdate  Capability = "draft.update"
	CapabilityDraftDelete  Capability = "draft.delete"
	CapabilityDraftPublish Capability = "draft.publish"

	CapabilityEvidenceRead   Capability = "evidence.read"
	CapabilityEvidenceCreate Capability = "evidence.create"
	CapabilityEvidenceUpdate Capability = "evidence.update"
	CapabilityEvidenceDelete Capability = "evidence.delete"

	CapabilityAttachmentRead   Capability = "attachment.read"
	CapabilityAttachmentCreate Capability = "attachment.create"
	CapabilityAttachmentUpdate Capability = "attachment.update"
	CapabilityAttachmentDelete Capability = "attachment.delete"

	CapabilitySearchRead              Capability = "search.read"
	CapabilityActivityRead            Capability = "activity.read"
	CapabilityComparisonRead          Capability = "comparison.read"
	CapabilityNotificationRead        Capability = "notification.read"
	CapabilityNotificationManage      Capability = "notification.manage"
	CapabilityImport                  Capability = "import.execute"
	CapabilityExport                  Capability = "export.execute"
	CapabilityExportSensitiveTopology Capability = "record.export_sensitive_topology"

	// CapabilityPermanentDelete is the stable short name for the record
	// permanent-deletion operation.
	CapabilityPermanentDelete Capability = CapabilityRecordPermanentDelete

	// Plural aliases are names only; they do not introduce additional values
	// into the closed capability registry.
	CapabilityRecordsRead   Capability = CapabilityRecordRead
	CapabilityRecordsCreate Capability = CapabilityRecordCreate
	CapabilityRecordsUpdate Capability = CapabilityRecordUpdate
	CapabilityRecordsDelete Capability = CapabilityRecordDelete
)

// ScopeVersion identifies the independently-versioned canonical documents.
type ScopeVersion uint8

const (
	VisibilityScopeVersionV1     ScopeVersion = 1
	SourceAuthorizationVersionV1 ScopeVersion = 1
	ResourceScopeVersionV1       ScopeVersion = 1

	PolicyVersionV1 uint64 = 1
)

// VisibilityKind describes whether a scope is project wide or restricted to
// explicit roles and/or groups.
type VisibilityKind string

const (
	VisibilityKindProject    VisibilityKind = "project"
	VisibilityKindRestricted VisibilityKind = "restricted"
)

// SourceKind is the closed v1 source registry.
type SourceKind string

const (
	SourceKindVPS                SourceKind = "vps"
	SourceKindMonitoringInstance SourceKind = "monitoring_instance"
	SourceKindTarget             SourceKind = "target"
)

// SourceState identifies the only supported source-evidence shapes.
type SourceState string

const (
	SourceStateLive       SourceState = "live"
	SourceStateTombstoned SourceState = "tombstoned"
)

var (
	ErrInvalidActorScope          = errors.New("invalid record authorization actor scope")
	ErrInvalidVisibilityScope     = errors.New("invalid record authorization visibility scope")
	ErrInvalidSourceAuthorization = errors.New("invalid record authorization source authorization")
	ErrInvalidResourceScope       = errors.New("invalid record authorization resource scope")

	// ErrDenied is deliberately a stable sentinel. Callers can map it to an
	// opaque resource response without inspecting details of the resource.
	ErrDenied = errors.New("record authorization denied")
)

// ScopeRepository is the only persistence-facing dependency accepted by
// session middleware. It returns stable group identifiers only.
type ScopeRepository interface {
	ListActorGroupIDs(context.Context, ProjectID, string) ([]string, error)
}

// ActorScope is the trusted, normalized scope placed into a request context.
// Construct it through NormalizeActorScope; Policy revalidates it before use.
type ActorScope struct {
	UserID    string
	Role      Role
	ProjectID ProjectID
	GroupIDs  []string

	canonicalBytes []byte
	canonicalHash  [sha256.Size]byte
}

// Clone returns a defensive copy suitable for a context boundary.
func (scope ActorScope) Clone() ActorScope {
	copyScope := scope
	copyScope.GroupIDs = append([]string(nil), scope.GroupIDs...)
	copyScope.canonicalBytes = append([]byte(nil), scope.canonicalBytes...)
	return copyScope
}

// CanonicalBytes returns a fresh encoding of the actor's normalized shape. An
// invalid mutated value has no canonical representation.
func (scope ActorScope) CanonicalBytes() []byte {
	normalized, err := NormalizeActorScope(ActorScope{
		UserID:    scope.UserID,
		Role:      scope.Role,
		ProjectID: scope.ProjectID,
		GroupIDs:  scope.GroupIDs,
	})
	if err != nil {
		return nil
	}
	return append([]byte(nil), normalized.canonicalBytes...)
}

// CanonicalHash returns the SHA-256 digest of CanonicalBytes. It is stable for
// logically equivalent actor group input regardless of caller ordering.
func (scope ActorScope) CanonicalHash() [sha256.Size]byte {
	return sha256.Sum256(scope.CanonicalBytes())
}

// NormalizeActorScope is the sole actor canonicalization path. It validates
// exact project and role registries, validates opaque identifiers, sorts and
// de-duplicates group IDs, and returns only defensive copies.
func NormalizeActorScope(input ActorScope) (ActorScope, error) {
	if err := ValidateActorUserID(input.UserID); err != nil {
		return ActorScope{}, fmt.Errorf("%w: user id", ErrInvalidActorScope)
	}
	if !knownRole(input.Role) {
		return ActorScope{}, fmt.Errorf("%w: role", ErrInvalidActorScope)
	}
	if err := ValidateProjectID(input.ProjectID); err != nil {
		return ActorScope{}, fmt.Errorf("%w: project", ErrInvalidActorScope)
	}

	groupIDs, err := normalizeGroupIDs(input.GroupIDs)
	if err != nil {
		return ActorScope{}, fmt.Errorf("%w: %w", ErrInvalidActorScope, err)
	}

	normalized := ActorScope{
		UserID:    input.UserID,
		Role:      input.Role,
		ProjectID: input.ProjectID,
		GroupIDs:  groupIDs,
	}
	normalized.canonicalBytes = canonicalActorBytes(normalized)
	normalized.canonicalHash = sha256.Sum256(normalized.canonicalBytes)
	return normalized, nil
}

// ValidateProjectID verifies the closed project registry without applying any
// string normalization.
func ValidateProjectID(projectID ProjectID) error {
	if !knownProjectID(projectID) {
		return fmt.Errorf("%w: project", ErrInvalidActorScope)
	}
	return nil
}

// ValidateActorUserID verifies the v1 opaque user identifier grammar.
func ValidateActorUserID(userID string) error {
	if !validUserID(userID) {
		return fmt.Errorf("%w: user id", ErrInvalidActorScope)
	}
	return nil
}

// ValidateGroupID verifies the persisted record-access-group identifier
// grammar. It is used by the repository before a database value becomes part
// of a trusted actor scope.
func ValidateGroupID(groupID string) error {
	if !validGroupID(groupID) {
		return fmt.Errorf("%w: group id", ErrInvalidActorScope)
	}
	return nil
}

// VisibilityScope is a canonical document used for resource visibility,
// source capture visibility, live visibility, and tombstone final floors.
// CanonicalHash must be produced by NormalizeVisibilityScope before a Policy
// will trust the document as persisted source evidence.
type VisibilityScope struct {
	Version         ScopeVersion
	Kind            VisibilityKind
	ProjectID       ProjectID
	AllowedRoles    []Role
	AllowedGroupIDs []string
	PolicyVersion   uint64
	PolicyRevision  uint64
	CanonicalHash   [sha256.Size]byte

	canonicalBytes []byte
}

// CanonicalBytes returns a defensive canonical visibility encoding. Invalid
// mutated values do not produce an encoding.
func (scope VisibilityScope) CanonicalBytes() []byte {
	normalized, err := NormalizeVisibilityScope(VisibilityScope{
		Version:         scope.Version,
		Kind:            scope.Kind,
		ProjectID:       scope.ProjectID,
		AllowedRoles:    scope.AllowedRoles,
		AllowedGroupIDs: scope.AllowedGroupIDs,
		PolicyVersion:   scope.PolicyVersion,
		PolicyRevision:  scope.PolicyRevision,
	})
	if err != nil {
		return nil
	}
	return append([]byte(nil), normalized.canonicalBytes...)
}

// CanonicalHashValue recomputes the scope hash from canonical evidence.
func (scope VisibilityScope) CanonicalHashValue() [sha256.Size]byte {
	return sha256.Sum256(scope.CanonicalBytes())
}

// NormalizeVisibilityScope canonicalizes role and group grants and computes
// their evidence hash. A project scope cannot carry ignored grants; a
// restricted scope may intentionally contain no grants, which means deny all.
func NormalizeVisibilityScope(input VisibilityScope) (VisibilityScope, error) {
	if input.Version != VisibilityScopeVersionV1 {
		return VisibilityScope{}, fmt.Errorf("%w: version", ErrInvalidVisibilityScope)
	}
	if !knownVisibilityKind(input.Kind) {
		return VisibilityScope{}, fmt.Errorf("%w: kind", ErrInvalidVisibilityScope)
	}
	if !knownProjectID(input.ProjectID) {
		return VisibilityScope{}, fmt.Errorf("%w: project", ErrInvalidVisibilityScope)
	}
	if input.PolicyVersion != PolicyVersionV1 {
		return VisibilityScope{}, fmt.Errorf("%w: policy version", ErrInvalidVisibilityScope)
	}
	if input.PolicyRevision == 0 {
		return VisibilityScope{}, fmt.Errorf("%w: policy revision", ErrInvalidVisibilityScope)
	}

	roles, err := normalizeRoles(input.AllowedRoles)
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: %w", ErrInvalidVisibilityScope, err)
	}
	groupIDs, err := normalizeGroupIDs(input.AllowedGroupIDs)
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: %w", ErrInvalidVisibilityScope, err)
	}
	if input.Kind == VisibilityKindProject && (len(roles) != 0 || len(groupIDs) != 0) {
		return VisibilityScope{}, fmt.Errorf("%w: project grants", ErrInvalidVisibilityScope)
	}

	normalized := VisibilityScope{
		Version:         input.Version,
		Kind:            input.Kind,
		ProjectID:       input.ProjectID,
		AllowedRoles:    roles,
		AllowedGroupIDs: groupIDs,
		PolicyVersion:   input.PolicyVersion,
		PolicyRevision:  input.PolicyRevision,
	}
	normalized.canonicalBytes = canonicalVisibilityBytes(normalized)
	normalized.CanonicalHash = sha256.Sum256(normalized.canonicalBytes)
	return normalized, nil
}

// ParseCanonicalVisibilityScope restores a VisibilityScope from trusted
// canonical bytes. It refuses unknown versions, leftover input, and any
// document whose re-encoded bytes do not match the original exactly.
func ParseCanonicalVisibilityScope(raw []byte) (VisibilityScope, error) {
	decoder := visibilityDecoder{rest: raw}
	domain, err := decoder.string()
	if err != nil || domain != "recordauth.visibility.v1" {
		return VisibilityScope{}, fmt.Errorf("%w: domain", ErrInvalidVisibilityScope)
	}
	version, err := decoder.byte()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: version", ErrInvalidVisibilityScope)
	}
	kind, err := decoder.string()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: kind", ErrInvalidVisibilityScope)
	}
	projectID, err := decoder.string()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: project", ErrInvalidVisibilityScope)
	}
	roleCount, err := decoder.length()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: roles", ErrInvalidVisibilityScope)
	}
	roles := make([]Role, 0, roleCount)
	for index := 0; index < roleCount; index++ {
		role, err := decoder.string()
		if err != nil {
			return VisibilityScope{}, fmt.Errorf("%w: role", ErrInvalidVisibilityScope)
		}
		roles = append(roles, Role(role))
	}
	groupCount, err := decoder.length()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: groups", ErrInvalidVisibilityScope)
	}
	groupIDs := make([]string, 0, groupCount)
	for index := 0; index < groupCount; index++ {
		groupID, err := decoder.string()
		if err != nil {
			return VisibilityScope{}, fmt.Errorf("%w: group", ErrInvalidVisibilityScope)
		}
		groupIDs = append(groupIDs, groupID)
	}
	policyVersion, err := decoder.uint64()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: policy version", ErrInvalidVisibilityScope)
	}
	policyRevision, err := decoder.uint64()
	if err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: policy revision", ErrInvalidVisibilityScope)
	}
	if err := decoder.done(); err != nil {
		return VisibilityScope{}, fmt.Errorf("%w: trailing bytes", ErrInvalidVisibilityScope)
	}
	normalized, err := NormalizeVisibilityScope(VisibilityScope{
		Version:         ScopeVersion(version),
		Kind:            VisibilityKind(kind),
		ProjectID:       ProjectID(projectID),
		AllowedRoles:    roles,
		AllowedGroupIDs: groupIDs,
		PolicyVersion:   policyVersion,
		PolicyRevision:  policyRevision,
	})
	if err != nil {
		return VisibilityScope{}, err
	}
	if !bytes.Equal(normalized.canonicalBytes, raw) {
		return VisibilityScope{}, fmt.Errorf("%w: non-canonical encoding", ErrInvalidVisibilityScope)
	}
	return normalized, nil
}

// SourceAuthorization is a strict tagged union. A live source carries a
// current scope; a tombstoned source carries its complete final floor and the
// canonical last-live-scope witness needed to preserve transition monotonicity.
type SourceAuthorization struct {
	Version       ScopeVersion
	Kind          SourceKind
	SourceID      string
	State         SourceState
	CaptureScope  VisibilityScope
	CurrentScope  *VisibilityScope
	FinalFloor    *VisibilityScope
	LastLiveScope *VisibilityScope
	Digest        [sha256.Size]byte
}

// NormalizeSourceAuthorization returns canonical source evidence and its
// digest. It refuses live and tombstone-transition widening before the
// evidence can be persisted.
func NormalizeSourceAuthorization(input SourceAuthorization) (SourceAuthorization, error) {
	if input.Version != SourceAuthorizationVersionV1 {
		return SourceAuthorization{}, fmt.Errorf("%w: version", ErrInvalidSourceAuthorization)
	}
	if !knownSourceKind(input.Kind) {
		return SourceAuthorization{}, fmt.Errorf("%w: kind", ErrInvalidSourceAuthorization)
	}
	if !validSourceID(input.Kind, input.SourceID) {
		return SourceAuthorization{}, fmt.Errorf("%w: source identifier", ErrInvalidSourceAuthorization)
	}
	if !knownSourceState(input.State) {
		return SourceAuthorization{}, fmt.Errorf("%w: state", ErrInvalidSourceAuthorization)
	}

	capture, err := NormalizeVisibilityScope(input.CaptureScope)
	if err != nil {
		return SourceAuthorization{}, fmt.Errorf("%w: capture scope", ErrInvalidSourceAuthorization)
	}

	normalized := SourceAuthorization{
		Version:      input.Version,
		Kind:         input.Kind,
		SourceID:     input.SourceID,
		State:        input.State,
		CaptureScope: capture,
	}
	switch input.State {
	case SourceStateLive:
		if input.CurrentScope == nil || input.FinalFloor != nil || input.LastLiveScope != nil {
			return SourceAuthorization{}, fmt.Errorf("%w: live union", ErrInvalidSourceAuthorization)
		}
		current, err := NormalizeVisibilityScope(*input.CurrentScope)
		if err != nil {
			return SourceAuthorization{}, fmt.Errorf("%w: current scope", ErrInvalidSourceAuthorization)
		}
		if current.ProjectID != capture.ProjectID {
			return SourceAuthorization{}, fmt.Errorf("%w: current project", ErrInvalidSourceAuthorization)
		}
		if !scopeNoWiderThan(current, capture) {
			return SourceAuthorization{}, fmt.Errorf("%w: live widening", ErrInvalidSourceAuthorization)
		}
		normalized.CurrentScope = &current
	case SourceStateTombstoned:
		if input.CurrentScope != nil || input.FinalFloor == nil || input.LastLiveScope == nil {
			return SourceAuthorization{}, fmt.Errorf("%w: tombstoned union", ErrInvalidSourceAuthorization)
		}
		lastLive, err := NormalizeVisibilityScope(*input.LastLiveScope)
		if err != nil {
			return SourceAuthorization{}, fmt.Errorf("%w: last live scope", ErrInvalidSourceAuthorization)
		}
		if lastLive.ProjectID != capture.ProjectID {
			return SourceAuthorization{}, fmt.Errorf("%w: last live project", ErrInvalidSourceAuthorization)
		}
		if !scopeNoWiderThan(lastLive, capture) {
			return SourceAuthorization{}, fmt.Errorf("%w: last live widening", ErrInvalidSourceAuthorization)
		}
		floor, err := NormalizeVisibilityScope(*input.FinalFloor)
		if err != nil {
			return SourceAuthorization{}, fmt.Errorf("%w: final floor", ErrInvalidSourceAuthorization)
		}
		if floor.ProjectID != capture.ProjectID {
			return SourceAuthorization{}, fmt.Errorf("%w: floor project", ErrInvalidSourceAuthorization)
		}
		if !scopeNoWiderThan(floor, lastLive) {
			return SourceAuthorization{}, fmt.Errorf("%w: tombstone widening", ErrInvalidSourceAuthorization)
		}
		normalized.FinalFloor = &floor
		normalized.LastLiveScope = &lastLive
	default:
		return SourceAuthorization{}, fmt.Errorf("%w: state", ErrInvalidSourceAuthorization)
	}
	normalized.Digest = sha256.Sum256(canonicalSourceBytes(normalized))
	return normalized, nil
}

// ResourceScope describes all immutable authorization evidence needed for a
// resource decision. A resource must carry at least one source evidence item.
type ResourceScope struct {
	Version    ScopeVersion
	ProjectID  ProjectID
	Visibility VisibilityScope
	Sources    []SourceAuthorization
}

func knownProjectID(projectID ProjectID) bool {
	return projectID == ProjectIDDefault
}

func knownRole(role Role) bool {
	switch role {
	case RoleProjectAdmin, RoleViewer:
		return true
	default:
		return false
	}
}

func knownCapability(capability Capability) bool {
	switch capability {
	case CapabilityRecordRead,
		CapabilityRecordCreate,
		CapabilityRecordUpdate,
		CapabilityRecordDelete,
		CapabilityRecordPermanentDelete,
		CapabilityDraftRead,
		CapabilityDraftCreate,
		CapabilityDraftUpdate,
		CapabilityDraftDelete,
		CapabilityDraftPublish,
		CapabilityEvidenceRead,
		CapabilityEvidenceCreate,
		CapabilityEvidenceUpdate,
		CapabilityEvidenceDelete,
		CapabilityAttachmentRead,
		CapabilityAttachmentCreate,
		CapabilityAttachmentUpdate,
		CapabilityAttachmentDelete,
		CapabilitySearchRead,
		CapabilityActivityRead,
		CapabilityComparisonRead,
		CapabilityNotificationRead,
		CapabilityNotificationManage,
		CapabilityImport,
		CapabilityExport,
		CapabilityExportSensitiveTopology:
		return true
	default:
		return false
	}
}

func knownVisibilityKind(kind VisibilityKind) bool {
	return kind == VisibilityKindProject || kind == VisibilityKindRestricted
}

func knownSourceKind(kind SourceKind) bool {
	switch kind {
	case SourceKindVPS, SourceKindMonitoringInstance, SourceKindTarget:
		return true
	default:
		return false
	}
}

func knownSourceState(state SourceState) bool {
	return state == SourceStateLive || state == SourceStateTombstoned
}

func validUserID(value string) bool {
	if len(value) != len("usr_")+24 || value[:len("usr_")] != "usr_" {
		return false
	}
	return allLowerHex(value[len("usr_"):])
}

func validGroupID(value string) bool {
	if len(value) <= len("rag_") || len(value) > len("rag_")+64 || value[:len("rag_")] != "rag_" {
		return false
	}
	for i := len("rag_"); i < len(value); i++ {
		if !isLowerAlphaNumeric(value[i]) {
			return false
		}
	}
	return true
}

func validSourceID(kind SourceKind, value string) bool {
	var prefix string
	switch kind {
	case SourceKindVPS:
		prefix = "vps_"
	case SourceKindMonitoringInstance:
		prefix = "mi_"
	case SourceKindTarget:
		prefix = "tg_"
	default:
		return false
	}
	if len(value) != len(prefix)+16 || value[:len(prefix)] != prefix {
		return false
	}
	return allLowerHex(value[len(prefix):])
}

func allLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !((value[i] >= '0' && value[i] <= '9') || (value[i] >= 'a' && value[i] <= 'f')) {
			return false
		}
	}
	return true
}

func isLowerAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9')
}

func normalizeRoles(input []Role) ([]Role, error) {
	roles := append([]Role(nil), input...)
	for _, role := range roles {
		if !knownRole(role) {
			return nil, errors.New("unknown role")
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return deduplicateRoles(roles), nil
}

func normalizeGroupIDs(input []string) ([]string, error) {
	groupIDs := append([]string(nil), input...)
	for _, groupID := range groupIDs {
		if !validGroupID(groupID) {
			return nil, errors.New("malformed group id")
		}
	}
	sort.Strings(groupIDs)
	return deduplicateStrings(groupIDs), nil
}

func deduplicateRoles(input []Role) []Role {
	if len(input) == 0 {
		return nil
	}
	output := make([]Role, 0, len(input))
	for _, value := range input {
		if len(output) == 0 || output[len(output)-1] != value {
			output = append(output, value)
		}
	}
	return output
}

func deduplicateStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if len(output) == 0 || output[len(output)-1] != value {
			output = append(output, value)
		}
	}
	return output
}

type canonicalEncoder struct {
	bytes []byte
}

func (encoder *canonicalEncoder) byte(value byte) {
	encoder.bytes = append(encoder.bytes, value)
}

func (encoder *canonicalEncoder) uint64(value uint64) {
	encoder.bytes = append(encoder.bytes,
		byte(value>>56), byte(value>>48), byte(value>>40), byte(value>>32),
		byte(value>>24), byte(value>>16), byte(value>>8), byte(value),
	)
}

func (encoder *canonicalEncoder) length(value int) {
	unsigned := uint32(value)
	encoder.bytes = append(encoder.bytes,
		byte(unsigned>>24), byte(unsigned>>16), byte(unsigned>>8), byte(unsigned),
	)
}

func (encoder *canonicalEncoder) string(value string) {
	encoder.length(len(value))
	encoder.bytes = append(encoder.bytes, value...)
}

func (encoder *canonicalEncoder) raw(value []byte) {
	encoder.length(len(value))
	encoder.bytes = append(encoder.bytes, value...)
}

func canonicalActorBytes(scope ActorScope) []byte {
	encoder := canonicalEncoder{}
	encoder.string("recordauth.actor.v1")
	encoder.string(scope.UserID)
	encoder.string(string(scope.Role))
	encoder.string(string(scope.ProjectID))
	encoder.length(len(scope.GroupIDs))
	for _, groupID := range scope.GroupIDs {
		encoder.string(groupID)
	}
	return encoder.bytes
}

type visibilityDecoder struct {
	rest []byte
}

func (decoder *visibilityDecoder) byte() (byte, error) {
	if len(decoder.rest) < 1 {
		return 0, ErrInvalidVisibilityScope
	}
	value := decoder.rest[0]
	decoder.rest = decoder.rest[1:]
	return value, nil
}

func (decoder *visibilityDecoder) length() (int, error) {
	if len(decoder.rest) < 4 {
		return 0, ErrInvalidVisibilityScope
	}
	value := int(decoder.rest[0])<<24 | int(decoder.rest[1])<<16 | int(decoder.rest[2])<<8 | int(decoder.rest[3])
	decoder.rest = decoder.rest[4:]
	if value < 0 {
		return 0, ErrInvalidVisibilityScope
	}
	return value, nil
}

func (decoder *visibilityDecoder) uint64() (uint64, error) {
	if len(decoder.rest) < 8 {
		return 0, ErrInvalidVisibilityScope
	}
	value := uint64(decoder.rest[0])<<56 | uint64(decoder.rest[1])<<48 | uint64(decoder.rest[2])<<40 | uint64(decoder.rest[3])<<32 |
		uint64(decoder.rest[4])<<24 | uint64(decoder.rest[5])<<16 | uint64(decoder.rest[6])<<8 | uint64(decoder.rest[7])
	decoder.rest = decoder.rest[8:]
	return value, nil
}

func (decoder *visibilityDecoder) string() (string, error) {
	length, err := decoder.length()
	if err != nil || length > len(decoder.rest) {
		return "", ErrInvalidVisibilityScope
	}
	value := string(decoder.rest[:length])
	decoder.rest = decoder.rest[length:]
	return value, nil
}

func (decoder *visibilityDecoder) done() error {
	if len(decoder.rest) != 0 {
		return ErrInvalidVisibilityScope
	}
	return nil
}

func canonicalVisibilityBytes(scope VisibilityScope) []byte {
	encoder := canonicalEncoder{}
	encoder.string("recordauth.visibility.v1")
	encoder.byte(byte(scope.Version))
	encoder.string(string(scope.Kind))
	encoder.string(string(scope.ProjectID))
	encoder.length(len(scope.AllowedRoles))
	for _, role := range scope.AllowedRoles {
		encoder.string(string(role))
	}
	encoder.length(len(scope.AllowedGroupIDs))
	for _, groupID := range scope.AllowedGroupIDs {
		encoder.string(groupID)
	}
	encoder.uint64(scope.PolicyVersion)
	encoder.uint64(scope.PolicyRevision)
	return encoder.bytes
}

func canonicalSourceBytes(source SourceAuthorization) []byte {
	encoder := canonicalEncoder{}
	encoder.string("recordauth.source.v1")
	encoder.byte(byte(source.Version))
	encoder.string(string(source.Kind))
	encoder.string(source.SourceID)
	encoder.string(string(source.State))
	encoder.raw(source.CaptureScope.CanonicalBytes())
	switch source.State {
	case SourceStateLive:
		encoder.byte(1)
		if source.CurrentScope != nil {
			encoder.raw(source.CurrentScope.CanonicalBytes())
		}
	case SourceStateTombstoned:
		encoder.byte(2)
		if source.FinalFloor != nil {
			encoder.raw(source.FinalFloor.CanonicalBytes())
		}
		if source.LastLiveScope != nil {
			encoder.raw(source.LastLiveScope.CanonicalBytes())
		}
	}
	return encoder.bytes
}

func scopeNoWiderThan(candidate, baseline VisibilityScope) bool {
	if baseline.Kind == VisibilityKindProject {
		return true
	}
	if candidate.Kind != VisibilityKindRestricted {
		return false
	}
	return rolesSubset(candidate.AllowedRoles, baseline.AllowedRoles) && stringsSubset(candidate.AllowedGroupIDs, baseline.AllowedGroupIDs)
}

func rolesSubset(candidate, baseline []Role) bool {
	for _, candidateRole := range candidate {
		found := false
		for _, baselineRole := range baseline {
			if candidateRole == baselineRole {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func stringsSubset(candidate, baseline []string) bool {
	for _, candidateValue := range candidate {
		found := false
		for _, baselineValue := range baseline {
			if candidateValue == baselineValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

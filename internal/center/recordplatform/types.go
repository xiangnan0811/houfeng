package recordplatform

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"time"
)

const (
	deletionRequestTokenV1Prefix    = "drt1_"
	deletionRequestTokenV1RawLength = 32
	deletionRequestTokenV1Domain    = "houfeng-deletion-request-token-v1"
)

var (
	ErrInvalidRecordPlatformInput  = errors.New("invalid record platform input")
	ErrInvalidDeletionRequestToken = errors.New("invalid deletion request token")
	ErrInvalidRequestFingerprint   = errors.New("invalid request fingerprint")
	ErrInvalidOwnerLease           = errors.New("invalid owner lease")
)

// ProjectID is a closed record-platform project identifier.
type ProjectID string

const ProjectIDDefault ProjectID = "default"

// DeploymentID is the immutable deployment identity bound to deletion-token
// commitments.
type DeploymentID string

// Clock supplies a local scheduling time. It is never a substitute for the
// database-time liveness predicates used by durable owner transitions.
type Clock interface {
	Now() time.Time
}

// OwnerLease is the durable owner-generation fence observed from a database
// row. ExpiresAt is useful for local stop scheduling, not local authority.
type OwnerLease struct {
	OwnerID    string
	Generation uint64
	ExpiresAt  time.Time
}

// ContentEpoch is the durable pivot that invalidates stale object-content
// deliveries. It is compared only with a freshly authorized epoch.
type ContentEpoch uint64

// OperationKind is the closed idempotency operation registry for v1 request
// fingerprints. It is not parsed from arbitrary caller strings.
type OperationKind string

const (
	OperationKindRecordCreate          OperationKind = "record_create"
	OperationKindRecordUpdate          OperationKind = "record_update"
	OperationKindRecordDelete          OperationKind = "record_delete"
	OperationKindRecordPermanentDelete OperationKind = "record_permanent_delete"
	OperationKindDeletionPreview       OperationKind = "deletion_preview"
	OperationKindDeletionFence         OperationKind = "deletion_fence"
	OperationKindRecordActionCreate    OperationKind = "record_action_create"
	OperationKindRecordActionUpdate    OperationKind = "record_action_update"
	OperationKindRecordActionComplete  OperationKind = "record_action_complete"
	OperationKindRecordActionCancel    OperationKind = "record_action_cancel"
	OperationKindRecordActionReopen    OperationKind = "record_action_reopen"
)

// RequestFingerprintVersion identifies the binary request-identity codec.
type RequestFingerprintVersion uint8

const RequestFingerprintVersionV1 RequestFingerprintVersion = 1

// RequestFingerprintInputV1 contains only already-validated canonical
// identities and fixed-size digests. It deliberately has no payload map, JSON,
// free-text scope, or caller-provided serialization hook.
type RequestFingerprintInputV1 struct {
	Version            RequestFingerprintVersion
	OperationKind      OperationKind
	ProjectID          ProjectID
	ActorScopeDigest   [sha256.Size]byte
	RequestScopeDigest [sha256.Size]byte
	PayloadDigest      [sha256.Size]byte
}

// RequestFingerprintV1 is the canonical, write-capable request identity
// digest. It is issued only by FingerprintRequestV1 and is never request
// content or a token preimage.
type RequestFingerprintV1 struct {
	digest [sha256.Size]byte
	sealed bool
}

// persistedRequestFingerprintSealV1 keeps persisted readback values distinct
// from canonical write fingerprints at the Go type-conversion boundary.
type persistedRequestFingerprintSealV1 struct {
	trusted bool
}

// PersistedRequestFingerprintV1 is an opaque, read-only request fingerprint
// reconstructed from trusted persistence. It intentionally has no conversion
// to RequestFingerprintV1 and cannot be used to claim or complete work.
type PersistedRequestFingerprintV1 struct {
	digest [sha256.Size]byte
	sealed persistedRequestFingerprintSealV1
}

// DeletionRequestTokenTransportV1 is a strictly parsed, opaque transport
// value. It deliberately does not carry issuance provenance and cannot derive
// a persistable commitment.
type DeletionRequestTokenTransportV1 struct {
	raw [deletionRequestTokenV1RawLength]byte
}

// IssuedDeletionRequestTokenV1 is the opaque capability returned only by the
// crypto/rand issuance path. Its unexported provenance bit means a parsed
// transport cannot be converted into a commitment-producing value outside this
// package.
type IssuedDeletionRequestTokenV1 struct {
	raw    [deletionRequestTokenV1RawLength]byte
	issued bool
}

// DeletionRequestTokenV1 remains the compatibility name for an issued token.
// Parsing intentionally returns DeletionRequestTokenTransportV1 instead.
type DeletionRequestTokenV1 = IssuedDeletionRequestTokenV1

// NewIssuedDeletionRequestTokenV1 generates exactly 32 cryptographically
// secure random bytes for a request-lifetime deletion token.
func NewIssuedDeletionRequestTokenV1() (IssuedDeletionRequestTokenV1, error) {
	var token IssuedDeletionRequestTokenV1
	if _, err := rand.Read(token.raw[:]); err != nil {
		return IssuedDeletionRequestTokenV1{}, fmt.Errorf("generate deletion request token: %w", err)
	}
	token.issued = true
	return token, nil
}

// NewDeletionRequestTokenV1 is the compatibility name for the issued-token
// constructor. New callers should use NewIssuedDeletionRequestTokenV1 to make
// the capability boundary explicit.
func NewDeletionRequestTokenV1() (IssuedDeletionRequestTokenV1, error) {
	return NewIssuedDeletionRequestTokenV1()
}

// ParseDeletionRequestTokenTransportV1 validates the only v1 transport form.
// It returns a transport-only value because parsing can validate grammar but
// cannot prove crypto/rand issuance provenance.
func ParseDeletionRequestTokenTransportV1(transport string) (DeletionRequestTokenTransportV1, error) {
	raw, ok := decodeDeletionRequestTokenV1Transport(transport)
	if !ok {
		return DeletionRequestTokenTransportV1{}, invalidDeletionRequestToken()
	}

	return DeletionRequestTokenTransportV1{raw: raw}, nil
}

// ParseDeletionRequestTokenV1 is the compatibility name for the strict
// transport parser. Its return type is intentionally not an issued capability.
func ParseDeletionRequestTokenV1(transport string) (DeletionRequestTokenTransportV1, error) {
	return ParseDeletionRequestTokenTransportV1(transport)
}

// Transport returns the explicit v1 transport representation. Callers must
// treat this value as secret request input and must not persist or log it.
func (token DeletionRequestTokenTransportV1) Transport() string {
	return deletionRequestTokenV1Prefix + base64.RawURLEncoding.EncodeToString(token.raw[:])
}

// Transport returns the explicit v1 transport representation for an issued
// capability. It does not expose the raw bytes.
func (token IssuedDeletionRequestTokenV1) Transport() string {
	return deletionRequestTokenV1Prefix + base64.RawURLEncoding.EncodeToString(token.raw[:])
}

// String intentionally redacts this secret from ordinary formatting/logging.
func (DeletionRequestTokenTransportV1) String() string {
	return "deletion-request-token-v1(redacted)"
}

// GoString prevents Go-syntax formatting from recursively exposing raw bytes.
func (token DeletionRequestTokenTransportV1) GoString() string {
	return token.String()
}

// String intentionally redacts an issued token from ordinary formatting/logging.
func (IssuedDeletionRequestTokenV1) String() string {
	return "deletion-request-token-v1(redacted)"
}

// GoString prevents Go-syntax formatting from recursively exposing raw bytes.
func (token IssuedDeletionRequestTokenV1) GoString() string {
	return token.String()
}

// Commitment returns the only persistable identity derived from this token.
// The raw token is intentionally not returned or accepted by persistence APIs.
func (token IssuedDeletionRequestTokenV1) Commitment(deploymentID DeploymentID, projectID ProjectID) ([sha256.Size]byte, error) {
	if !token.issued {
		return [sha256.Size]byte{}, invalidDeletionRequestToken()
	}
	return deletionRequestTokenCommitmentV1(token.raw, deploymentID, projectID)
}

// MatchesCommitment verifies a persisted commitment in constant time without
// turning caller-supplied transport back into a write-capable commitment.
func (token DeletionRequestTokenTransportV1) MatchesCommitment(
	deploymentID DeploymentID,
	projectID ProjectID,
	expected [sha256.Size]byte,
) bool {
	commitment, err := deletionRequestTokenCommitmentV1(token.raw, deploymentID, projectID)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(commitment[:], expected[:]) == 1
}

func deletionRequestTokenCommitmentV1(
	raw [deletionRequestTokenV1RawLength]byte,
	deploymentID DeploymentID,
	projectID ProjectID,
) ([sha256.Size]byte, error) {
	if err := ValidateDeploymentID(deploymentID); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := ValidateProjectID(projectID); err != nil {
		return [sha256.Size]byte{}, err
	}

	preimage := make([]byte, 0, len(deletionRequestTokenV1Domain)+1+len(deploymentID)+1+len(projectID)+1+len(raw))
	preimage = append(preimage, deletionRequestTokenV1Domain...)
	preimage = append(preimage, 0)
	preimage = append(preimage, string(deploymentID)...)
	preimage = append(preimage, 0)
	preimage = append(preimage, string(projectID)...)
	preimage = append(preimage, 0)
	preimage = append(preimage, raw[:]...)
	return sha256.Sum256(preimage), nil
}

// ValidateProjectID rejects any unregistered v1 project value without trying
// to trim or normalize caller input.
func ValidateProjectID(projectID ProjectID) error {
	if projectID != ProjectIDDefault {
		return fmt.Errorf("%w: project id", ErrInvalidRecordPlatformInput)
	}
	return nil
}

// ValidateDeploymentID verifies the exact stable deployment identifier form.
func ValidateDeploymentID(deploymentID DeploymentID) error {
	value := string(deploymentID)
	if len(value) != 67 || value[:3] != "dp-" {
		return fmt.Errorf("%w: deployment id", ErrInvalidRecordPlatformInput)
	}
	for _, character := range value[3:] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("%w: deployment id", ErrInvalidRecordPlatformInput)
		}
	}
	return nil
}

// Validate verifies structural owner-token validity before it reaches a
// persistence method. It does not make a local expiry decision authoritative.
func (lease OwnerLease) Validate() error {
	if !validRecordPlatformOwnerID(lease.OwnerID) || lease.Generation == 0 || lease.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: owner lease", ErrInvalidOwnerLease)
	}
	return nil
}

// LocallyLive is a conservative scheduling helper. A nil clock or malformed
// token is not live; callers must still rely on the database owner fence for
// any durable write.
func (lease OwnerLease) LocallyLive(clock Clock) bool {
	return !isNilRecordPlatformDependency(clock) && lease.Validate() == nil && lease.ExpiresAt.After(clock.Now())
}

func isNilRecordPlatformDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

// MarshalBinary produces the only v1 request-fingerprint preimage. Fixed
// field ordering and explicit length prefixes make the encoding unambiguous.
func (input RequestFingerprintInputV1) MarshalBinary() ([]byte, error) {
	if err := input.validate(); err != nil {
		return nil, err
	}

	body := make([]byte, 0, 1+(4+len(input.OperationKind))+(4+len(input.ProjectID))+3*(4+sha256.Size))
	body = append(body, byte(input.Version))
	body = appendRecordPlatformLengthPrefixed(body, []byte(input.OperationKind))
	body = appendRecordPlatformLengthPrefixed(body, []byte(input.ProjectID))
	body = appendRecordPlatformLengthPrefixed(body, input.ActorScopeDigest[:])
	body = appendRecordPlatformLengthPrefixed(body, input.RequestScopeDigest[:])
	body = appendRecordPlatformLengthPrefixed(body, input.PayloadDigest[:])
	return body, nil
}

// FingerprintRequestV1 hashes the canonical v1 body. Callers can provide a
// payload digest but cannot bypass the codec with unstructured content.
func FingerprintRequestV1(input RequestFingerprintInputV1) (RequestFingerprintV1, error) {
	body, err := input.MarshalBinary()
	if err != nil {
		return RequestFingerprintV1{}, err
	}
	return newSealedRequestFingerprintV1(sha256.Sum256(body)), nil
}

// ParseTrustedPersistedRequestFingerprintV1 reconstructs a read-only
// fingerprint only from an exact 32-byte value already read from trusted
// persistence. It is a storage readback boundary, not a caller-input factory;
// claim and completion callers must use FingerprintRequestV1.
func ParseTrustedPersistedRequestFingerprintV1(raw []byte) (PersistedRequestFingerprintV1, error) {
	if len(raw) != sha256.Size {
		return PersistedRequestFingerprintV1{}, invalidRequestFingerprint()
	}
	var digest [sha256.Size]byte
	copy(digest[:], raw)
	return newSealedPersistedRequestFingerprintV1(digest), nil
}

// Validate rejects an unsealed zero/literal value before it can reach a claim,
// completion, or persisted-record decision.
func (fingerprint RequestFingerprintV1) Validate() error {
	if !fingerprint.sealed {
		return invalidRequestFingerprint()
	}
	return nil
}

// Equal compares two issued fingerprints without exposing either digest.
func (fingerprint RequestFingerprintV1) Equal(other RequestFingerprintV1) bool {
	return fingerprint.sealed && other.sealed && fingerprint.digest == other.digest
}

// MatchesPersisted compares a canonical request fingerprint with trusted
// storage readback without making the readback value write-capable.
func (fingerprint RequestFingerprintV1) MatchesPersisted(other PersistedRequestFingerprintV1) bool {
	return requestFingerprintV1MatchesPersisted(fingerprint, other)
}

// PersistedBytes returns an immutable fixed-size copy for the storage boundary.
// It never exposes the fingerprint's backing storage as a mutable slice.
func (fingerprint RequestFingerprintV1) PersistedBytes() ([sha256.Size]byte, error) {
	if err := fingerprint.Validate(); err != nil {
		return [sha256.Size]byte{}, err
	}
	return fingerprint.digest, nil
}

func newSealedRequestFingerprintV1(digest [sha256.Size]byte) RequestFingerprintV1 {
	return RequestFingerprintV1{digest: digest, sealed: true}
}

// Validate rejects an unsealed literal before it can influence a persisted
// record, replay, or idempotency decision.
func (fingerprint PersistedRequestFingerprintV1) Validate() error {
	if !fingerprint.sealed.trusted {
		return invalidRequestFingerprint()
	}
	return nil
}

// Equal compares two persisted readback fingerprints without exposing either
// digest or making either value write-capable.
func (fingerprint PersistedRequestFingerprintV1) Equal(other PersistedRequestFingerprintV1) bool {
	return fingerprint.sealed.trusted && other.sealed.trusted && fingerprint.digest == other.digest
}

func newSealedPersistedRequestFingerprintV1(digest [sha256.Size]byte) PersistedRequestFingerprintV1 {
	return PersistedRequestFingerprintV1{digest: digest, sealed: persistedRequestFingerprintSealV1{trusted: true}}
}

func requestFingerprintV1MatchesPersisted(request RequestFingerprintV1, persisted PersistedRequestFingerprintV1) bool {
	return request.sealed && persisted.sealed.trusted && request.digest == persisted.digest
}

// ParseRequestFingerprintInputV1 accepts only the exact v1 length-prefixed
// body. It does not offer a permissive map, JSON, or text fallback.
func ParseRequestFingerprintInputV1(body []byte) (RequestFingerprintInputV1, error) {
	reader := requestFingerprintReader{body: body}
	version, err := reader.version()
	if err != nil {
		return RequestFingerprintInputV1{}, err
	}
	operationKind, err := reader.operationKind()
	if err != nil {
		return RequestFingerprintInputV1{}, err
	}
	projectID, err := reader.projectID()
	if err != nil {
		return RequestFingerprintInputV1{}, err
	}
	actorScopeDigest, err := reader.digest()
	if err != nil {
		return RequestFingerprintInputV1{}, err
	}
	requestScopeDigest, err := reader.digest()
	if err != nil {
		return RequestFingerprintInputV1{}, err
	}
	payloadDigest, err := reader.digest()
	if err != nil {
		return RequestFingerprintInputV1{}, err
	}
	if err := reader.done(); err != nil {
		return RequestFingerprintInputV1{}, err
	}

	input := RequestFingerprintInputV1{
		Version:            version,
		OperationKind:      operationKind,
		ProjectID:          projectID,
		ActorScopeDigest:   actorScopeDigest,
		RequestScopeDigest: requestScopeDigest,
		PayloadDigest:      payloadDigest,
	}
	canonical, err := input.MarshalBinary()
	if err != nil || !bytes.Equal(canonical, body) {
		return RequestFingerprintInputV1{}, invalidRequestFingerprint()
	}
	return input, nil
}

func (input RequestFingerprintInputV1) validate() error {
	if input.Version != RequestFingerprintVersionV1 {
		return fmt.Errorf("%w: request fingerprint version", ErrInvalidRecordPlatformInput)
	}
	if err := ValidateOperationKind(input.OperationKind); err != nil {
		return err
	}
	return ValidateProjectID(input.ProjectID)
}

// ValidateOperationKind rejects unknown values instead of treating the
// database's broad storage grammar as a business-operation registry.
func ValidateOperationKind(operationKind OperationKind) error {
	switch operationKind {
	case OperationKindRecordCreate,
		OperationKindRecordUpdate,
		OperationKindRecordDelete,
		OperationKindRecordPermanentDelete,
		OperationKindDeletionPreview,
		OperationKindDeletionFence,
		OperationKindRecordActionCreate,
		OperationKindRecordActionUpdate,
		OperationKindRecordActionComplete,
		OperationKindRecordActionCancel,
		OperationKindRecordActionReopen:
		return nil
	default:
		return fmt.Errorf("%w: operation kind", ErrInvalidRecordPlatformInput)
	}
}

func appendRecordPlatformLengthPrefixed(body, field []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(field)))
	body = append(body, length[:]...)
	return append(body, field...)
}

func validRecordPlatformOwnerID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	if isDeletionRequestTokenTransportEncoding(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func isCanonicalDeletionRequestTokenTransport(value string) bool {
	_, ok := decodeDeletionRequestTokenV1Transport(value)
	return ok
}

// isDeletionRequestTokenTransportEncoding recognizes a decodable v1-sized
// token spelling anywhere in a value, including encodings with noncanonical
// unused base64url bits. Durable identifiers must reject all of them so a
// parser-rejected alias cannot persist the same raw secret through an unrelated
// identifier field.
func isDeletionRequestTokenTransportEncoding(value string) bool {
	transportLength := len(deletionRequestTokenV1Prefix) + base64.RawURLEncoding.EncodedLen(deletionRequestTokenV1RawLength)
	if len(value) < transportLength {
		return false
	}
	for start := 0; start <= len(value)-transportLength; start++ {
		if value[start:start+len(deletionRequestTokenV1Prefix)] != deletionRequestTokenV1Prefix {
			continue
		}
		payloadStart := start + len(deletionRequestTokenV1Prefix)
		decoded, err := base64.RawURLEncoding.DecodeString(value[payloadStart : start+transportLength])
		if err == nil && len(decoded) == deletionRequestTokenV1RawLength {
			return true
		}
	}
	return false
}

func decodeDeletionRequestTokenV1Transport(transport string) ([deletionRequestTokenV1RawLength]byte, bool) {
	if len(transport) != len(deletionRequestTokenV1Prefix)+base64.RawURLEncoding.EncodedLen(deletionRequestTokenV1RawLength) ||
		transport[:len(deletionRequestTokenV1Prefix)] != deletionRequestTokenV1Prefix {
		return [deletionRequestTokenV1RawLength]byte{}, false
	}

	payload := transport[len(deletionRequestTokenV1Prefix):]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || len(decoded) != deletionRequestTokenV1RawLength || base64.RawURLEncoding.EncodeToString(decoded) != payload {
		return [deletionRequestTokenV1RawLength]byte{}, false
	}
	var raw [deletionRequestTokenV1RawLength]byte
	copy(raw[:], decoded)
	return raw, true
}

type requestFingerprintReader struct {
	body   []byte
	offset int
}

func (reader *requestFingerprintReader) version() (RequestFingerprintVersion, error) {
	if reader.offset >= len(reader.body) {
		return 0, invalidRequestFingerprint()
	}
	value := RequestFingerprintVersion(reader.body[reader.offset])
	reader.offset++
	return value, nil
}

func (reader *requestFingerprintReader) operationKind() (OperationKind, error) {
	field, err := reader.field()
	if err != nil {
		return "", err
	}
	return OperationKind(field), nil
}

func (reader *requestFingerprintReader) projectID() (ProjectID, error) {
	field, err := reader.field()
	if err != nil {
		return "", err
	}
	return ProjectID(field), nil
}

func (reader *requestFingerprintReader) digest() ([sha256.Size]byte, error) {
	field, err := reader.field()
	if err != nil || len(field) != sha256.Size {
		return [sha256.Size]byte{}, invalidRequestFingerprint()
	}
	var digest [sha256.Size]byte
	copy(digest[:], field)
	return digest, nil
}

func (reader *requestFingerprintReader) field() ([]byte, error) {
	if len(reader.body)-reader.offset < 4 {
		return nil, invalidRequestFingerprint()
	}
	length := binary.BigEndian.Uint32(reader.body[reader.offset : reader.offset+4])
	reader.offset += 4
	if uint64(length) > uint64(len(reader.body)-reader.offset) {
		return nil, invalidRequestFingerprint()
	}
	end := reader.offset + int(length)
	field := reader.body[reader.offset:end]
	reader.offset = end
	return field, nil
}

func (reader *requestFingerprintReader) done() error {
	if reader.offset != len(reader.body) {
		return invalidRequestFingerprint()
	}
	return nil
}

func invalidDeletionRequestToken() error {
	return fmt.Errorf("%w: transport", ErrInvalidDeletionRequestToken)
}

func invalidRequestFingerprint() error {
	return fmt.Errorf("%w: body", ErrInvalidRequestFingerprint)
}

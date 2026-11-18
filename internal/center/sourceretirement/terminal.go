// Package sourceretirement encodes W2 source retirement evidence. It does not
// establish witness trust, admission generation, or trusted-head policy.
package sourceretirement

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
)

const (
	terminalDomain          = "HOUFENG-SOURCE-TERMINAL-V2"
	terminalVersion  uint32 = 2
	maxTerminalBytes        = 65536
	maxScopeBytes           = 4096
	maxStringBytes          = 128
	identityBytes           = 67
)

// ErrInvalidTerminal identifies malformed, noncanonical, or mismatched evidence.
var ErrInvalidTerminal = errors.New("invalid source retirement terminal")

// SourceKey identifies a source independently of its presence in the APP DB.
type SourceKey struct {
	DeploymentID string
	ProjectID    recordauth.ProjectID
	Kind         recordauth.SourceKind
	ID           string
}

// DomainIdentity binds evidence to the independent physical witness domain.
type DomainIdentity struct {
	ID               string
	SystemIdentifier string
	DatabaseOID      uint32
	DatabaseName     string
	Epoch            uint64
}

// Terminal is the complete projection of a canonical W2 terminal entry.
// Scopes must already be canonical, including their evidence hashes.
type Terminal struct {
	Domain           DomainIdentity
	Source           SourceKey
	RetirementID     string
	OriginGeneration uint64
	BarrierDigest    [32]byte
	LastLive         recordauth.VisibilityScope
	FinalFloor       recordauth.VisibilityScope
	Sequence         uint64
	PreviousHash     [32]byte
}

// EncodeTerminal refuses invalid or mutated values rather than normalizing
// them into acceptable evidence. The returned bytes belong to the caller.
func EncodeTerminal(value Terminal) ([]byte, error) {
	lastLive, floor, err := validateTerminal(value)
	if err != nil {
		return nil, err
	}
	stringsInOrder := [...]string{
		terminalDomain, value.Domain.ID, value.Domain.SystemIdentifier,
		value.Domain.DatabaseName, value.Source.DeploymentID,
		string(value.Source.ProjectID), string(value.Source.Kind), value.Source.ID,
		value.RetirementID,
	}
	size := 4 + 4 + 8 + 8 + 8 + 4 + 32 + 4 + len(lastLive) + 4 + len(floor) + 4 + 32
	for _, text := range stringsInOrder {
		size += 4 + len(text)
	}
	if size > maxTerminalBytes {
		return nil, invalidTerminal("size")
	}
	encoder := terminalEncoder{bytes: make([]byte, 0, size)}
	encoder.text(terminalDomain)
	encoder.uint32(terminalVersion)
	encoder.text(value.Domain.ID)
	encoder.text(value.Domain.SystemIdentifier)
	encoder.uint32(value.Domain.DatabaseOID)
	encoder.text(value.Domain.DatabaseName)
	encoder.uint64(value.Domain.Epoch)
	encoder.text(value.Source.DeploymentID)
	encoder.text(string(value.Source.ProjectID))
	encoder.text(string(value.Source.Kind))
	encoder.text(value.Source.ID)
	encoder.text(value.RetirementID)
	encoder.uint64(value.OriginGeneration)
	encoder.raw(value.BarrierDigest[:])
	encoder.raw(lastLive)
	encoder.raw(floor)
	encoder.uint64(value.Sequence)
	encoder.raw(value.PreviousHash[:])
	return encoder.bytes, nil
}

// DecodeTerminal accepts only the strict, complete canonical W2 encoding.
// Lengths are bounded against both field limits and remaining input before
// conversion, slicing, or allocation, including nested scope grant counts.
func DecodeTerminal(canonical []byte) (Terminal, error) {
	if len(canonical) > maxTerminalBytes {
		return Terminal{}, invalidTerminal("size")
	}
	decoder := terminalDecoder{rest: canonical}
	if decoder.text(maxStringBytes) != terminalDomain || decoder.uint32() != terminalVersion {
		return Terminal{}, invalidTerminal("domain or version")
	}
	value := Terminal{}
	value.Domain.ID = decoder.text(identityBytes)
	value.Domain.SystemIdentifier = decoder.text(maxStringBytes)
	value.Domain.DatabaseOID = decoder.uint32()
	value.Domain.DatabaseName = decoder.text(maxStringBytes)
	value.Domain.Epoch = decoder.uint64()
	value.Source.DeploymentID = decoder.text(identityBytes)
	value.Source.ProjectID = recordauth.ProjectID(decoder.text(maxStringBytes))
	value.Source.Kind = recordauth.SourceKind(decoder.text(maxStringBytes))
	value.Source.ID = decoder.text(maxStringBytes)
	value.RetirementID = decoder.text(maxStringBytes)
	value.OriginGeneration = decoder.uint64()
	barrier := decoder.raw(sha256.Size)
	lastLive := decoder.raw(maxScopeBytes)
	floor := decoder.raw(maxScopeBytes)
	value.Sequence = decoder.uint64()
	previous := decoder.raw(sha256.Size)
	if decoder.err != nil || len(decoder.rest) != 0 || len(barrier) != sha256.Size || len(previous) != sha256.Size {
		return Terminal{}, invalidTerminal("encoding")
	}
	copy(value.BarrierDigest[:], barrier)
	copy(value.PreviousHash[:], previous)
	var err error
	value.LastLive, err = parseTerminalScope(lastLive)
	if err != nil {
		return Terminal{}, err
	}
	value.FinalFloor, err = parseTerminalScope(floor)
	if err != nil {
		return Terminal{}, err
	}
	if _, _, err := validateTerminal(value); err != nil {
		return Terminal{}, err
	}
	return value, nil
}

// TerminalHash hashes the entire canonical entry, including its chain links.
func TerminalHash(value Terminal) ([32]byte, error) {
	canonical, err := EncodeTerminal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(canonical), nil
}

// VerifyTerminal checks strict canonical decoding, every projected field, and
// the supplied entry hash. Callers must separately authenticate the domain,
// generation, sequence/previous link, and trusted head before authorizing use.
func VerifyTerminal(canonical []byte, projected Terminal, expectedHash [32]byte) error {
	if _, err := DecodeTerminal(canonical); err != nil {
		return err
	}
	projection, err := EncodeTerminal(projected)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, projection) {
		return invalidTerminal("projection")
	}
	actualHash := sha256.Sum256(canonical)
	if subtle.ConstantTimeCompare(actualHash[:], expectedHash[:]) != 1 {
		return invalidTerminal("hash")
	}
	return nil
}

func validateTerminal(value Terminal) ([]byte, []byte, error) {
	if !validIdentity(value.Domain.ID, "rd-") || !validIdentity(value.Source.DeploymentID, "dp-") {
		return nil, nil, invalidTerminal("identity")
	}
	identifier := value.Domain.SystemIdentifier
	if len(identifier) == 0 || len(identifier) > 20 || identifier[0] < '1' || identifier[0] > '9' {
		return nil, nil, invalidTerminal("system identifier")
	}
	for index := range identifier {
		if identifier[index] < '0' || identifier[index] > '9' {
			return nil, nil, invalidTerminal("system identifier")
		}
	}
	if _, err := strconv.ParseUint(identifier, 10, 64); err != nil {
		return nil, nil, invalidTerminal("system identifier")
	}
	name := value.Domain.DatabaseName
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return nil, nil, invalidTerminal("database name")
	}
	for index := range name {
		if !lowerAlphaNumeric(name[index]) && name[index] != '_' {
			return nil, nil, invalidTerminal("database name")
		}
	}
	if value.Domain.DatabaseOID == 0 || value.Domain.Epoch == 0 || value.OriginGeneration == 0 || value.Sequence == 0 {
		return nil, nil, invalidTerminal("positive integer")
	}
	if value.Source.ProjectID != recordauth.ProjectIDDefault || !validOpaqueID(value.Source.ID) || !validOpaqueID(value.RetirementID) {
		return nil, nil, invalidTerminal("source or retirement identity")
	}
	if value.BarrierDigest == ([32]byte{}) || (value.PreviousHash == ([32]byte{})) != (value.Sequence == 1) {
		return nil, nil, invalidTerminal("barrier or chain")
	}
	lastLive, err := canonicalTerminalScope(value.LastLive, value.Source.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	floor, err := canonicalTerminalScope(value.FinalFloor, value.Source.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	// Reuse the production source registry and tombstone monotonicity policy
	// on the actual identity; no codec-specific authorization policy is added.
	_, err = recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:       recordauth.SourceAuthorizationVersionV1,
		Kind:          value.Source.Kind,
		SourceID:      value.Source.ID,
		State:         recordauth.SourceStateTombstoned,
		CaptureScope:  value.LastLive,
		LastLiveScope: &value.LastLive,
		FinalFloor:    &value.FinalFloor,
	})
	if err != nil {
		return nil, nil, invalidTerminal("scope transition")
	}
	return lastLive, floor, nil
}

func canonicalTerminalScope(scope recordauth.VisibilityScope, project recordauth.ProjectID) ([]byte, error) {
	// Bound the canonical size before calling normalizers that allocate grant
	// slices. Four bytes per grant is a conservative lower bound.
	size := 4 + len("recordauth.visibility.v1") + 1 + 4 + len(scope.Kind) + 4 + len(scope.ProjectID) + 4 + 4 + 8 + 8
	if scope.ProjectID != project || len(scope.AllowedRoles) > maxScopeBytes/4 || len(scope.AllowedGroupIDs) > maxScopeBytes/4 || size > maxScopeBytes {
		return nil, invalidTerminal("scope size or project")
	}
	for _, role := range scope.AllowedRoles {
		if len(role) > maxScopeBytes || size > maxScopeBytes-4-len(role) {
			return nil, invalidTerminal("scope size")
		}
		size += 4 + len(role)
	}
	for _, group := range scope.AllowedGroupIDs {
		if len(group) > maxScopeBytes || size > maxScopeBytes-4-len(group) {
			return nil, invalidTerminal("scope size")
		}
		size += 4 + len(group)
	}
	normalized, err := recordauth.NormalizeVisibilityScope(scope)
	if err != nil || normalized.CanonicalHash != scope.CanonicalHash || !slices.Equal(normalized.AllowedRoles, scope.AllowedRoles) || !slices.Equal(normalized.AllowedGroupIDs, scope.AllowedGroupIDs) {
		return nil, invalidTerminal("noncanonical scope")
	}
	return normalized.CanonicalBytes(), nil
}

func parseTerminalScope(raw []byte) (recordauth.VisibilityScope, error) {
	// The existing strict scope parser allocates directly from its nested
	// counts. Preflight those counts against the bounded input first.
	decoder := terminalDecoder{rest: raw}
	decoder.raw(maxScopeBytes)
	decoder.take(1)
	decoder.raw(maxScopeBytes)
	decoder.raw(maxScopeBytes)
	for range 2 {
		count := decoder.uint32()
		if uint64(count) > uint64(len(decoder.rest)/4) {
			return recordauth.VisibilityScope{}, invalidTerminal("scope grant count")
		}
		for range count {
			if decoder.err != nil {
				break
			}
			decoder.raw(maxScopeBytes)
		}
	}
	decoder.uint64()
	decoder.uint64()
	if decoder.err != nil || len(decoder.rest) != 0 {
		return recordauth.VisibilityScope{}, invalidTerminal("scope encoding")
	}
	scope, err := recordauth.ParseCanonicalVisibilityScope(raw)
	if err != nil {
		return recordauth.VisibilityScope{}, invalidTerminal("noncanonical scope")
	}
	return scope, nil
}

func validIdentity(value, prefix string) bool {
	if len(value) != identityBytes || !strings.HasPrefix(value, prefix) {
		return false
	}
	for index := len(prefix); index < len(value); index++ {
		if (value[index] < '0' || value[index] > '9') && (value[index] < 'a' || value[index] > 'f') {
			return false
		}
	}
	return true
}

func validOpaqueID(value string) bool {
	if len(value) == 0 || len(value) > maxStringBytes {
		return false
	}
	for index := range value {
		if !lowerAlphaNumeric(value[index]) && value[index] != '_' && value[index] != '-' {
			return false
		}
	}
	return true
}

func lowerAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9')
}

func invalidTerminal(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidTerminal, reason)
}

type terminalEncoder struct {
	bytes []byte
}

func (encoder *terminalEncoder) uint32(value uint32) {
	encoder.bytes = binary.BigEndian.AppendUint32(encoder.bytes, value)
}

func (encoder *terminalEncoder) uint64(value uint64) {
	encoder.bytes = binary.BigEndian.AppendUint64(encoder.bytes, value)
}

func (encoder *terminalEncoder) text(value string) {
	encoder.uint32(uint32(len(value)))
	encoder.bytes = append(encoder.bytes, value...)
}

func (encoder *terminalEncoder) raw(value []byte) {
	encoder.uint32(uint32(len(value)))
	encoder.bytes = append(encoder.bytes, value...)
}

type terminalDecoder struct {
	rest []byte
	err  error
}

func (decoder *terminalDecoder) take(size int) []byte {
	if decoder.err != nil {
		return nil
	}
	if size > len(decoder.rest) {
		decoder.err = ErrInvalidTerminal
		return nil
	}
	value := decoder.rest[:size]
	decoder.rest = decoder.rest[size:]
	return value
}

func (decoder *terminalDecoder) uint32() uint32 {
	value := decoder.take(4)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint32(value)
}

func (decoder *terminalDecoder) uint64() uint64 {
	value := decoder.take(8)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (decoder *terminalDecoder) raw(maximum int) []byte {
	size := decoder.uint32()
	if decoder.err != nil {
		return nil
	}
	if uint64(size) > uint64(maximum) || uint64(size) > uint64(len(decoder.rest)) {
		decoder.err = ErrInvalidTerminal
		return nil
	}
	return decoder.take(int(size))
}

func (decoder *terminalDecoder) text(maximum int) string {
	value := decoder.raw(maximum)
	if !utf8.Valid(value) {
		decoder.err = ErrInvalidTerminal
		return ""
	}
	return string(value)
}

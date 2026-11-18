package deletionledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/sourceretirement"
)

const (
	sourceLedgerDomain  = "HOUFENG-SOURCE-RETIREMENT-LEDGER-V1"
	sourceOriginDomain  = "HOUFENG-SOURCE-RETIREMENT-ORIGIN-V1"
	sourceTokenDomain   = "HOUFENG-SOURCE-RETIREMENT-TOKEN-V1"
	sourceRequestDomain = "HOUFENG-SOURCE-RETIREMENT-REQUEST-V1"
	sourceRoute         = "source_permanent_delete"
	sourceAction        = "retire_source_authorization"
	sourceScopeLimit    = 4096
	sourceEntryLimit    = 1048576
)

// SourceApplicationIdentity binds the original APP database, not a record.
type SourceApplicationIdentity struct {
	ID, SystemIdentifier, DatabaseName string
	DatabaseOID                        uint32
	Epoch                              uint64
}

func (identity SourceApplicationIdentity) Validate() error {
	// APP has the same physical identity grammar as W1, but a separate kind.
	return (recorddeletion.LedgerPhysicalIdentity{ID: identity.ID, Kind: "deletion_ledger", SystemIdentifier: identity.SystemIdentifier,
		DatabaseOID: identity.DatabaseOID, DatabaseName: identity.DatabaseName, Epoch: identity.Epoch}).Validate()
}

// SourceOrigin is the original, committed APP projection's identity and facts.
type SourceOrigin struct {
	Application         SourceApplicationIdentity
	Source              sourceretirement.SourceKey
	Revision            [32]byte
	LastLive            recordauth.VisibilityScope
	TombstoneRecordedAt time.Time
}

type SourceRetirementRequest struct {
	OperationID, ActorUserID            string
	Origin                              SourceOrigin
	FinalFloor                          recordauth.VisibilityScope
	TokenCommitment, RequestFingerprint [32]byte
}

type SourceRetirementEntry struct {
	Request      SourceRetirementRequest
	LedgerDomain [32]byte
	Sequence     uint64
	PreviousHash [32]byte
	ConfirmedAt  time.Time
	Canonical    []byte
	EntryHash    [32]byte
}

func sourceInvalid(detail string) error {
	return fmt.Errorf("%w: source %s", recorddeletion.ErrInvalidLedgerEvidence, detail)
}

func validSourceKey(key sourceretirement.SourceKey) bool {
	if len(key.DeploymentID) != 67 || key.DeploymentID[:3] != "dp-" || key.ProjectID != recordauth.ProjectIDDefault {
		return false
	}
	for _, b := range key.DeploymentID[3:] {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	var prefix string
	switch key.Kind {
	case recordauth.SourceKindVPS:
		prefix = "vps_"
	case recordauth.SourceKindMonitoringInstance:
		prefix = "mi_"
	case recordauth.SourceKindTarget:
		prefix = "tg_"
	default:
		return false
	}
	if len(key.ID) != len(prefix)+16 || key.ID[:len(prefix)] != prefix {
		return false
	}
	for _, b := range key.ID[len(prefix):] {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

func validSourceOperation(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, b := range value {
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '_' && b != '-' {
			return false
		}
	}
	return true
}

func sourceMicros(t time.Time) (uint64, error) {
	if t.IsZero() || t.Location() != time.UTC || t.Year() < 1970 || t.Year() > 9999 || t.Nanosecond()%1000 != 0 {
		return 0, sourceInvalid("timestamp")
	}
	micros := t.UnixMicro()
	if micros < 0 || !time.UnixMicro(micros).UTC().Equal(t) {
		return 0, sourceInvalid("timestamp")
	}
	return uint64(micros), nil
}

func sourceScope(scope recordauth.VisibilityScope, project recordauth.ProjectID) ([]byte, error) {
	// Bound the work before normalization, including inputs with duplicate grants.
	size := 4 + len("recordauth.visibility.v1") + 1 + 4 + len(scope.Kind) + 4 + len(scope.ProjectID) + 4 + 4 + 8 + 8
	if scope.ProjectID != project || size > sourceScopeLimit || scope.PolicyVersion > math.MaxInt64 || scope.PolicyRevision > math.MaxInt64 || len(scope.AllowedRoles) > sourceScopeLimit/4 || len(scope.AllowedGroupIDs) > sourceScopeLimit/4 {
		return nil, sourceInvalid("scope")
	}
	for _, role := range scope.AllowedRoles {
		if len(role) > sourceScopeLimit || size > sourceScopeLimit-4-len(role) {
			return nil, sourceInvalid("scope size")
		}
		size += 4 + len(role)
	}
	for _, group := range scope.AllowedGroupIDs {
		if len(group) > sourceScopeLimit || size > sourceScopeLimit-4-len(group) {
			return nil, sourceInvalid("scope size")
		}
		size += 4 + len(group)
	}
	normalized, err := recordauth.NormalizeVisibilityScope(scope)
	if err != nil || normalized.CanonicalHash != scope.CanonicalHash || !slices.Equal(normalized.AllowedRoles, scope.AllowedRoles) || !slices.Equal(normalized.AllowedGroupIDs, scope.AllowedGroupIDs) {
		return nil, sourceInvalid("noncanonical scope")
	}
	body := normalized.CanonicalBytes()
	if len(body) == 0 || len(body) > sourceScopeLimit {
		return nil, sourceInvalid("scope size")
	}
	return body, nil
}

func sourceFields(request SourceRetirementRequest) (origin, live, floor []byte, err error) {
	key := request.Origin.Source
	if !validSourceKey(key) || !validSourceOperation(request.OperationID) || recordauth.ValidateActorUserID(request.ActorUserID) != nil || request.TokenCommitment == ([32]byte{}) {
		return nil, nil, nil, sourceInvalid("request identity")
	}
	origin, err = EncodeSourceOrigin(request.Origin)
	if err != nil {
		return nil, nil, nil, err
	}
	live, err = sourceScope(request.Origin.LastLive, key.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	floor, err = sourceScope(request.FinalFloor, key.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	_, err = recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{Version: recordauth.SourceAuthorizationVersionV1,
		Kind: key.Kind, SourceID: key.ID, State: recordauth.SourceStateTombstoned,
		CaptureScope: request.Origin.LastLive, LastLiveScope: &request.Origin.LastLive, FinalFloor: &request.FinalFloor})
	if err != nil {
		return nil, nil, nil, sourceInvalid("floor transition")
	}
	return origin, live, floor, nil
}

func (request SourceRetirementRequest) Validate() error {
	digest, err := SourceRequestFingerprint(request)
	if err != nil {
		return err
	}
	if request.RequestFingerprint == ([32]byte{}) || request.RequestFingerprint != digest {
		return sourceInvalid("request fingerprint")
	}
	return nil
}

func appendSourceBytes(dst []byte, value []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

func EncodeSourceOrigin(origin SourceOrigin) ([]byte, error) {
	if origin.Application.Validate() != nil || !validSourceKey(origin.Source) || origin.Revision == ([32]byte{}) {
		return nil, sourceInvalid("origin identity")
	}
	micros, err := sourceMicros(origin.TombstoneRecordedAt)
	if err != nil {
		return nil, err
	}
	live, err := sourceScope(origin.LastLive, origin.Source.ProjectID)
	if err != nil {
		return nil, err
	}
	var body []byte
	body = appendSourceBytes(body, []byte(sourceOriginDomain))
	body = binary.BigEndian.AppendUint64(body, 1)
	body = appendSourceBytes(body, []byte(origin.Application.ID))
	body = appendSourceBytes(body, []byte(origin.Application.SystemIdentifier))
	body = binary.BigEndian.AppendUint64(body, uint64(origin.Application.DatabaseOID))
	body = appendSourceBytes(body, []byte(origin.Application.DatabaseName))
	body = binary.BigEndian.AppendUint64(body, origin.Application.Epoch)
	for _, value := range []string{sourceAction, origin.Source.DeploymentID, string(origin.Source.ProjectID), string(origin.Source.Kind), origin.Source.ID} {
		body = appendSourceBytes(body, []byte(value))
	}
	body = appendSourceBytes(body, origin.Revision[:])
	body = appendSourceBytes(body, live)
	body = binary.BigEndian.AppendUint64(body, micros)
	if len(body) > sourceScopeLimit {
		return nil, sourceInvalid("origin size")
	}
	return body, nil
}

func SourceTokenCommitment(key sourceretirement.SourceKey, token [32]byte) ([32]byte, error) {
	if !validSourceKey(key) {
		return [32]byte{}, sourceInvalid("token identity")
	}
	body := appendSourceBytes(nil, []byte(sourceTokenDomain))
	body = appendSourceBytes(body, []byte(key.DeploymentID))
	body = appendSourceBytes(body, []byte(key.ProjectID))
	body = appendSourceBytes(body, token[:])
	return sha256.Sum256(body), nil
}

// SourceRequestFingerprint deliberately excludes the fingerprint field itself.
func SourceRequestFingerprint(request SourceRetirementRequest) ([32]byte, error) {
	origin, live, floor, err := sourceFields(request)
	if err != nil {
		return [32]byte{}, err
	}
	key := request.Origin.Source
	body := appendSourceBytes(nil, []byte(sourceRequestDomain))
	for _, value := range []string{key.DeploymentID, string(key.ProjectID), request.OperationID, request.ActorUserID, string(key.Kind), key.ID} {
		body = appendSourceBytes(body, []byte(value))
	}
	body = appendSourceBytes(body, request.TokenCommitment[:])
	body = appendSourceBytes(body, origin)
	body = appendSourceBytes(body, live)
	body = appendSourceBytes(body, floor)
	body = binary.BigEndian.AppendUint64(body, 2)
	return sha256.Sum256(body), nil
}

func EncodeSourceRetirementEntry(entry SourceRetirementEntry) ([]byte, error) {
	if entry.Request.Validate() != nil || entry.Sequence == 0 || entry.Sequence > math.MaxInt64 || entry.LedgerDomain == ([32]byte{}) || (entry.PreviousHash == ([32]byte{})) != (entry.Sequence == 1) {
		return nil, sourceInvalid("entry identity")
	}
	micros, err := sourceMicros(entry.ConfirmedAt)
	if err != nil {
		return nil, err
	}
	origin, _, floor, err := sourceFields(entry.Request)
	if err != nil {
		return nil, err
	}
	key := entry.Request.Origin.Source
	body := appendSourceBytes(nil, []byte(sourceLedgerDomain))
	body = binary.BigEndian.AppendUint64(body, 1)
	body = appendSourceBytes(body, entry.LedgerDomain[:])
	body = binary.BigEndian.AppendUint64(body, entry.Sequence)
	body = appendSourceBytes(body, entry.PreviousHash[:])
	for _, value := range []string{"delete_commit", key.DeploymentID, string(key.ProjectID), entry.Request.OperationID, entry.Request.ActorUserID, sourceRoute, string(key.Kind), key.ID} {
		body = appendSourceBytes(body, []byte(value))
	}
	body = appendSourceBytes(body, origin)
	body = appendSourceBytes(body, floor)
	floorHash := sha256.Sum256(floor)
	body = appendSourceBytes(body, floorHash[:])
	body = appendSourceBytes(body, entry.Request.TokenCommitment[:])
	body = appendSourceBytes(body, entry.Request.RequestFingerprint[:])
	body = appendSourceBytes(body, []byte("user_confirmed"))
	body = binary.BigEndian.AppendUint64(body, 2)
	body = binary.BigEndian.AppendUint64(body, 0)
	body = binary.BigEndian.AppendUint64(body, micros)
	if len(body) > sourceEntryLimit {
		return nil, sourceInvalid("entry size")
	}
	return body, nil
}

type sourceDecoder struct {
	rest []byte
	err  error
}

func (d *sourceDecoder) take(n int) []byte {
	if d.err != nil || n < 0 || n > len(d.rest) {
		d.err = sourceInvalid("encoding")
		return nil
	}
	value := d.rest[:n]
	d.rest = d.rest[n:]
	return value
}
func (d *sourceDecoder) u32() uint32 {
	b := d.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}
func (d *sourceDecoder) u64() uint64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}
func (d *sourceDecoder) raw(max int) []byte {
	n := d.u32()
	if d.err != nil || uint64(n) > uint64(max) || uint64(n) > uint64(len(d.rest)) {
		d.err = sourceInvalid("length")
		return nil
	}
	return d.take(int(n))
}
func (d *sourceDecoder) fixed(n int) []byte {
	b := d.raw(n)
	if len(b) != n {
		d.err = sourceInvalid("digest")
	}
	return b
}
func (d *sourceDecoder) text(max int) string {
	b := d.raw(max)
	if !utf8.Valid(b) {
		d.err = sourceInvalid("UTF-8")
	}
	return string(b)
}

func parseSourceScope(raw []byte) (recordauth.VisibilityScope, error) {
	// The shared parser allocates from nested counts; validate them first.
	d := sourceDecoder{rest: raw}
	d.raw(sourceScopeLimit)
	d.take(1)
	d.raw(sourceScopeLimit)
	d.raw(sourceScopeLimit)
	for range 2 {
		count := d.u32()
		if uint64(count) > uint64(len(d.rest)/4) {
			return recordauth.VisibilityScope{}, sourceInvalid("scope count")
		}
		for range count {
			d.raw(sourceScopeLimit)
			if d.err != nil {
				break
			}
		}
	}
	d.u64()
	d.u64()
	if d.err != nil || len(d.rest) != 0 {
		return recordauth.VisibilityScope{}, sourceInvalid("scope encoding")
	}
	scope, err := recordauth.ParseCanonicalVisibilityScope(raw)
	if err != nil {
		return recordauth.VisibilityScope{}, sourceInvalid("scope encoding")
	}
	if _, err := sourceScope(scope, scope.ProjectID); err != nil {
		return recordauth.VisibilityScope{}, err
	}
	return scope, nil
}

func decodeSourceOrigin(raw []byte) (SourceOrigin, error) {
	if len(raw) == 0 || len(raw) > sourceScopeLimit {
		return SourceOrigin{}, sourceInvalid("origin size")
	}
	d := sourceDecoder{rest: raw}
	domain, version := d.text(128), d.u64()
	var origin SourceOrigin
	origin.Application.ID = d.text(128)
	origin.Application.SystemIdentifier = d.text(128)
	oid := d.u64()
	origin.Application.DatabaseName = d.text(128)
	origin.Application.Epoch = d.u64()
	action := d.text(128)
	origin.Source.DeploymentID = d.text(128)
	origin.Source.ProjectID = recordauth.ProjectID(d.text(128))
	origin.Source.Kind = recordauth.SourceKind(d.text(128))
	origin.Source.ID = d.text(128)
	copy(origin.Revision[:], d.fixed(32))
	live := d.raw(sourceScopeLimit)
	micros := d.u64()
	if d.err != nil || len(d.rest) != 0 || domain != sourceOriginDomain || version != 1 || action != sourceAction || oid == 0 || oid > math.MaxUint32 || micros > math.MaxInt64 {
		return SourceOrigin{}, sourceInvalid("origin encoding")
	}
	origin.Application.DatabaseOID = uint32(oid)
	origin.TombstoneRecordedAt = time.UnixMicro(int64(micros)).UTC()
	var err error
	origin.LastLive, err = parseSourceScope(live)
	if err != nil {
		return SourceOrigin{}, err
	}
	encoded, err := EncodeSourceOrigin(origin)
	if err != nil || !bytes.Equal(encoded, raw) {
		return SourceOrigin{}, sourceInvalid("origin projection")
	}
	return origin, nil
}

func DecodeSourceRetirementEntry(raw []byte) (SourceRetirementEntry, error) {
	if len(raw) == 0 || len(raw) > sourceEntryLimit {
		return SourceRetirementEntry{}, sourceInvalid("entry size")
	}
	d := sourceDecoder{rest: raw}
	domain, version := d.text(128), d.u64()
	var entry SourceRetirementEntry
	copy(entry.LedgerDomain[:], d.fixed(32))
	entry.Sequence = d.u64()
	copy(entry.PreviousHash[:], d.fixed(32))
	kind := d.text(128)
	deployment := d.text(128)
	project := d.text(128)
	entry.Request.OperationID = d.text(128)
	entry.Request.ActorUserID = d.text(128)
	route := d.text(128)
	objectKind := d.text(128)
	objectID := d.text(128)
	origin := d.raw(sourceScopeLimit)
	floor := d.raw(sourceScopeLimit)
	floorHash := d.fixed(32)
	copy(entry.Request.TokenCommitment[:], d.fixed(32))
	copy(entry.Request.RequestFingerprint[:], d.fixed(32))
	reason, contract, release, micros := d.text(128), d.u64(), d.u64(), d.u64()
	if d.err != nil || len(d.rest) != 0 || domain != sourceLedgerDomain || version != 1 || kind != "delete_commit" || route != sourceRoute || reason != "user_confirmed" || contract != 2 || release != 0 || micros > math.MaxInt64 {
		return SourceRetirementEntry{}, sourceInvalid("entry encoding")
	}
	var err error
	entry.Request.Origin, err = decodeSourceOrigin(origin)
	if err != nil {
		return SourceRetirementEntry{}, err
	}
	key := entry.Request.Origin.Source
	if key.DeploymentID != deployment || string(key.ProjectID) != project || string(key.Kind) != objectKind || key.ID != objectID {
		return SourceRetirementEntry{}, sourceInvalid("entry origin binding")
	}
	entry.Request.FinalFloor, err = parseSourceScope(floor)
	if err != nil {
		return SourceRetirementEntry{}, err
	}
	floorDigest := sha256.Sum256(floor)
	if !bytes.Equal(floorHash, floorDigest[:]) {
		return SourceRetirementEntry{}, sourceInvalid("floor hash")
	}
	entry.ConfirmedAt = time.UnixMicro(int64(micros)).UTC()
	encoded, err := EncodeSourceRetirementEntry(entry)
	if err != nil || !bytes.Equal(encoded, raw) {
		return SourceRetirementEntry{}, sourceInvalid("entry projection")
	}
	entry.Canonical = append([]byte(nil), raw...)
	entry.EntryHash = sha256.Sum256(raw)
	return entry, nil
}

func (entry SourceRetirementEntry) Validate() error {
	encoded, err := EncodeSourceRetirementEntry(entry)
	if err != nil || !bytes.Equal(encoded, entry.Canonical) || sha256.Sum256(entry.Canonical) != entry.EntryHash || entry.EntryHash == ([32]byte{}) {
		return sourceInvalid("entry projection")
	}
	return nil
}

func cloneSourceScope(scope recordauth.VisibilityScope) recordauth.VisibilityScope {
	scope.AllowedRoles = slices.Clone(scope.AllowedRoles)
	scope.AllowedGroupIDs = slices.Clone(scope.AllowedGroupIDs)
	return scope
}
func cloneSourceRequest(request SourceRetirementRequest) SourceRetirementRequest {
	request.Origin.LastLive = cloneSourceScope(request.Origin.LastLive)
	request.FinalFloor = cloneSourceScope(request.FinalFloor)
	return request
}
func cloneSourceEntry(entry SourceRetirementEntry) SourceRetirementEntry {
	entry.Request = cloneSourceRequest(entry.Request)
	entry.Canonical = bytes.Clone(entry.Canonical)
	return entry
}

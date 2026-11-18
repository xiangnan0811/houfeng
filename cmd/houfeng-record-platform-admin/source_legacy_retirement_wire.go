package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"slices"
	"time"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
)

const (
	legacySourceAction          = "retire_source_authorization"
	legacySourceContractVersion = 2
	legacySourceFileVersion     = 1
	legacySourceScopeLimit      = 4096
)

type legacySourceKey struct {
	DeploymentID string                `json:"deployment_id"`
	ProjectID    recordauth.ProjectID  `json:"project_id"`
	Kind         recordauth.SourceKind `json:"kind"`
	ID           string                `json:"id"`
}

func (key legacySourceKey) source() (sourceretirement.SourceKey, error) {
	if recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(key.DeploymentID)) != nil ||
		recordauth.ValidateProjectID(key.ProjectID) != nil {
		return sourceretirement.SourceKey{}, errLegacySourceInput
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
		return sourceretirement.SourceKey{}, errLegacySourceInput
	}
	if len(key.ID) != len(prefix)+16 || key.ID[:len(prefix)] != prefix {
		return sourceretirement.SourceKey{}, errLegacySourceInput
	}
	digits := key.ID[len(prefix):]
	decoded, err := hex.DecodeString(digits)
	if err != nil || hex.EncodeToString(decoded) != digits {
		return sourceretirement.SourceKey{}, errLegacySourceInput
	}
	return sourceretirement.SourceKey{DeploymentID: key.DeploymentID, ProjectID: key.ProjectID, Kind: key.Kind, ID: key.ID}, nil
}

func legacySourceKeyFrom(key sourceretirement.SourceKey) legacySourceKey {
	return legacySourceKey{DeploymentID: key.DeploymentID, ProjectID: key.ProjectID, Kind: key.Kind, ID: key.ID}
}

type legacySourceScope struct {
	Version         recordauth.ScopeVersion   `json:"version"`
	Kind            recordauth.VisibilityKind `json:"kind"`
	ProjectID       recordauth.ProjectID      `json:"project_id"`
	AllowedRoles    []recordauth.Role         `json:"allowed_roles"`
	AllowedGroupIDs []string                  `json:"allowed_group_ids"`
	PolicyVersion   uint64                    `json:"policy_version"`
	PolicyRevision  uint64                    `json:"policy_revision"`
	CanonicalHash   string                    `json:"canonical_hash"`
}

func (wire legacySourceScope) scope() (recordauth.VisibilityScope, error) {
	if wire.AllowedRoles == nil || wire.AllowedGroupIDs == nil || wire.PolicyVersion > math.MaxInt64 ||
		wire.PolicyRevision > math.MaxInt64 || len(wire.AllowedRoles) > legacySourceScopeLimit/4 ||
		len(wire.AllowedGroupIDs) > legacySourceScopeLimit/4 {
		return recordauth.VisibilityScope{}, errLegacySourceInput
	}
	hash, err := legacySourceDigest(wire.CanonicalHash)
	if err != nil {
		return recordauth.VisibilityScope{}, errLegacySourceInput
	}
	input := recordauth.VisibilityScope{Version: wire.Version, Kind: wire.Kind, ProjectID: wire.ProjectID,
		AllowedRoles: wire.AllowedRoles, AllowedGroupIDs: wire.AllowedGroupIDs,
		PolicyVersion: wire.PolicyVersion, PolicyRevision: wire.PolicyRevision}
	scope, err := recordauth.NormalizeVisibilityScope(input)
	if err != nil || scope.CanonicalHash != hash || !slices.Equal(scope.AllowedRoles, wire.AllowedRoles) ||
		!slices.Equal(scope.AllowedGroupIDs, wire.AllowedGroupIDs) || len(scope.CanonicalBytes()) > legacySourceScopeLimit {
		return recordauth.VisibilityScope{}, errLegacySourceInput
	}
	return scope, nil
}

func legacySourceScopeFrom(scope recordauth.VisibilityScope) legacySourceScope {
	return legacySourceScope{Version: scope.Version, Kind: scope.Kind, ProjectID: scope.ProjectID,
		AllowedRoles: append([]recordauth.Role{}, scope.AllowedRoles...), AllowedGroupIDs: append([]string{}, scope.AllowedGroupIDs...),
		PolicyVersion: scope.PolicyVersion, PolicyRevision: scope.PolicyRevision,
		CanonicalHash: hex.EncodeToString(scope.CanonicalHash[:])}
}

type legacySourceApplication struct {
	ID               string `json:"id"`
	SystemIdentifier string `json:"system_identifier"`
	DatabaseName     string `json:"database_name"`
	DatabaseOID      uint32 `json:"database_oid"`
	Epoch            uint64 `json:"epoch"`
}

type legacySourceOrigin struct {
	Application         legacySourceApplication `json:"application"`
	Source              legacySourceKey         `json:"source"`
	Revision            string                  `json:"revision"`
	LastLive            legacySourceScope       `json:"last_live"`
	TombstoneRecordedAt string                  `json:"tombstone_recorded_at"`
}

type legacySourceRequest struct {
	OperationID        string             `json:"operation_id"`
	ActorUserID        string             `json:"actor_user_id"`
	Origin             legacySourceOrigin `json:"origin"`
	FinalFloor         legacySourceScope  `json:"final_floor"`
	TokenCommitment    string             `json:"token_commitment"`
	RequestFingerprint string             `json:"request_fingerprint"`
}

type legacySourceTokenFile struct {
	Version                 uint64              `json:"version"`
	Action                  string              `json:"action"`
	DeletionContractVersion uint64              `json:"deletion_contract_version"`
	Request                 legacySourceRequest `json:"request"`
	Token                   string              `json:"token"`
}

var legacySourceKeySchema = sourceRequestSchema{"deployment_id": nil, "project_id": nil, "kind": nil, "id": nil}
var legacySourceScopeSchema = sourceRequestSchema{
	"version": nil, "kind": nil, "project_id": nil, "allowed_roles": nil, "allowed_group_ids": nil,
	"policy_version": nil, "policy_revision": nil, "canonical_hash": nil,
}
var legacySourceTokenSchema = sourceRequestSchema{
	"version": nil, "action": nil, "deletion_contract_version": nil,
	"request": {
		"operation_id": nil, "actor_user_id": nil,
		"origin": {"application": {"id": nil, "system_identifier": nil, "database_name": nil, "database_oid": nil, "epoch": nil},
			"source": legacySourceKeySchema, "revision": nil, "last_live": legacySourceScopeSchema, "tombstone_recorded_at": nil},
		"final_floor": legacySourceScopeSchema, "token_commitment": nil, "request_fingerprint": nil,
	},
	"token": nil,
}

func legacySourceDigest(value string) ([32]byte, error) {
	var result [32]byte
	if len(value) != hex.EncodedLen(len(result)) {
		return result, errLegacySourceInput
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return result, errLegacySourceInput
	}
	copy(result[:], decoded)
	return result, nil
}

// Null array elements also need rejection: unmarshalling null into a Go string
// otherwise silently produces an empty value.
func decodeLegacySourceJSON(body []byte, destination any, schema sourceRequestSchema) error {
	if len(body) == 0 || len(body) > sourceRetirementFileLimit || decodeSourceEnrollmentRequest(body, destination, schema) != nil {
		return errLegacySourceInput
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil || token == nil {
			return errLegacySourceInput
		}
	}
}

func legacySourceTokenFileFrom(request deletionledger.SourceRetirementRequest, token [32]byte) legacySourceTokenFile {
	app := request.Origin.Application
	return legacySourceTokenFile{
		Version: legacySourceFileVersion, Action: legacySourceAction, DeletionContractVersion: legacySourceContractVersion,
		Request: legacySourceRequest{
			OperationID: request.OperationID, ActorUserID: request.ActorUserID,
			Origin: legacySourceOrigin{
				Application: legacySourceApplication{ID: app.ID, SystemIdentifier: app.SystemIdentifier, DatabaseName: app.DatabaseName,
					DatabaseOID: app.DatabaseOID, Epoch: app.Epoch},
				Source: legacySourceKeyFrom(request.Origin.Source), Revision: hex.EncodeToString(request.Origin.Revision[:]),
				LastLive: legacySourceScopeFrom(request.Origin.LastLive), TombstoneRecordedAt: request.Origin.TombstoneRecordedAt.Format(time.RFC3339Nano),
			},
			FinalFloor: legacySourceScopeFrom(request.FinalFloor), TokenCommitment: hex.EncodeToString(request.TokenCommitment[:]),
			RequestFingerprint: hex.EncodeToString(request.RequestFingerprint[:]),
		},
		Token: hex.EncodeToString(token[:]),
	}
}

func (wire legacySourceTokenFile) request() (deletionledger.SourceRetirementRequest, [32]byte, error) {
	var empty deletionledger.SourceRetirementRequest
	var noToken [32]byte
	if wire.Version != legacySourceFileVersion || wire.Action != legacySourceAction || wire.DeletionContractVersion != legacySourceContractVersion {
		return empty, noToken, errLegacySourceInput
	}
	key, err := wire.Request.Origin.Source.source()
	if err != nil {
		return empty, noToken, errLegacySourceInput
	}
	live, err := wire.Request.Origin.LastLive.scope()
	if err != nil {
		return empty, noToken, errLegacySourceInput
	}
	floor, err := wire.Request.FinalFloor.scope()
	if err != nil {
		return empty, noToken, errLegacySourceInput
	}
	revision, e1 := legacySourceDigest(wire.Request.Origin.Revision)
	commitment, e2 := legacySourceDigest(wire.Request.TokenCommitment)
	fingerprint, e3 := legacySourceDigest(wire.Request.RequestFingerprint)
	token, e4 := legacySourceDigest(wire.Token)
	recorded, e5 := time.Parse(time.RFC3339Nano, wire.Request.Origin.TombstoneRecordedAt)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil ||
		recorded.Location() != time.UTC || recorded.Format(time.RFC3339Nano) != wire.Request.Origin.TombstoneRecordedAt {
		return empty, noToken, errLegacySourceInput
	}
	app := wire.Request.Origin.Application
	request := deletionledger.SourceRetirementRequest{
		OperationID: wire.Request.OperationID, ActorUserID: wire.Request.ActorUserID,
		Origin: deletionledger.SourceOrigin{Application: deletionledger.SourceApplicationIdentity{ID: app.ID, SystemIdentifier: app.SystemIdentifier,
			DatabaseName: app.DatabaseName, DatabaseOID: app.DatabaseOID, Epoch: app.Epoch}, Source: key, Revision: revision,
			LastLive: live, TombstoneRecordedAt: recorded},
		FinalFloor: floor, TokenCommitment: commitment, RequestFingerprint: fingerprint,
	}
	actual, err := deletionledger.SourceTokenCommitment(key, token)
	if request.Validate() != nil || err != nil || actual != commitment {
		clear(token[:])
		return empty, noToken, errLegacySourceInput
	}
	return request, token, nil
}

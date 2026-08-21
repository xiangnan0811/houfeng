package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	sourceDeletionWitnessRoute        = "source_permanent_delete"
	sourceDeletionWitnessEntryType    = "delete_commit"
	sourceDeletionWitnessEntryVersion = int16(1)
)

// WitnessedRecordSubjectTombstoneReader binds contract-activation identity and
// reads source-deletion authority from the independent full-witness store.
// A digest-only APP `source_deletion_tombstones` row is never sufficient.
var _ WitnessedRecordSubjectTombstoneSource = (*WitnessedRecordSubjectTombstoneReader)(nil)

type WitnessedRecordSubjectTombstoneReader struct {
	deploymentID recordplatform.DeploymentID
	projectID    recordauth.ProjectID
	witness      currentRecordAuthorizationDB
	local        currentRecordAuthorizationDB
}

func NewWitnessedRecordSubjectTombstoneReader(
	deploymentID recordplatform.DeploymentID,
	projectID recordauth.ProjectID,
	witness currentRecordAuthorizationDB,
	local currentRecordAuthorizationDB,
) (*WitnessedRecordSubjectTombstoneReader, error) {
	if string(deploymentID) != "" {
		if err := recordplatform.ValidateDeploymentID(deploymentID); err != nil {
			return nil, fmt.Errorf("%w: deployment id", ErrRecordSubjectUnavailable)
		}
	}
	if projectID != recordauth.ProjectIDDefault {
		return nil, fmt.Errorf("%w: project id", ErrRecordSubjectUnavailable)
	}
	if nilRecordSubjectDependency(witness) {
		witness = nil
	}
	if nilRecordSubjectDependency(local) {
		local = nil
	}
	return &WitnessedRecordSubjectTombstoneReader{
		deploymentID: deploymentID,
		projectID:    projectID,
		witness:      witness,
		local:        local,
	}, nil
}

func (reader *WitnessedRecordSubjectTombstoneReader) ResolveWitnessedRecordSubjectTombstone(
	ctx context.Context,
	projectID recordauth.ProjectID,
	kind recordauth.SourceKind,
	sourceID string,
) (WitnessedRecordSubjectTombstone, error) {
	if ctx == nil || reader == nil || nilRecordSubjectDependency(reader.witness) ||
		reader.deploymentID == "" || projectID != reader.projectID {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	objectKind, ok := witnessedSourceObjectKind(kind)
	if !ok {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}

	var (
		entryVersion           int16
		entryType              string
		route                  string
		gotObjectKind          string
		gotObjectID            string
		gotDeploymentID        string
		gotProjectID           string
		authorizationFloor     []byte
		authorizationFloorHash []byte
		originIdentity         []byte
	)
	err := reader.witness.QueryRow(ctx, `
		select entry_version,
		       entry_type,
		       route,
		       object_kind,
		       object_id,
		       deployment_id,
		       project_id,
		       authorization_floor,
		       authorization_floor_hash,
		       origin_identity
		from public.deletion_witness_entries
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
		  and deployment_id = $4
		  and route = $5
		  and entry_type = $6
		order by sequence desc
		limit 1
	`, string(projectID), objectKind, sourceID, string(reader.deploymentID), sourceDeletionWitnessRoute, sourceDeletionWitnessEntryType).Scan(
		&entryVersion,
		&entryType,
		&route,
		&gotObjectKind,
		&gotObjectID,
		&gotDeploymentID,
		&gotProjectID,
		&authorizationFloor,
		&authorizationFloorHash,
		&originIdentity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WitnessedRecordSubjectTombstone{}, ErrWitnessedRecordSubjectTombstoneNotFound
	}
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, fmt.Errorf("%w: source deletion witness", ErrRecordSubjectUnavailable)
	}
	if entryVersion != sourceDeletionWitnessEntryVersion ||
		entryType != sourceDeletionWitnessEntryType ||
		route != sourceDeletionWitnessRoute ||
		gotObjectKind != objectKind ||
		gotObjectID != sourceID ||
		gotDeploymentID != string(reader.deploymentID) ||
		gotProjectID != string(projectID) ||
		len(authorizationFloorHash) != sha256.Size ||
		sha256.Sum256(authorizationFloor) != digest32(authorizationFloorHash) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}

	floor, err := recordauth.ParseCanonicalVisibilityScope(authorizationFloor)
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, fmt.Errorf("%w: authorization floor", ErrRecordSubjectUnavailable)
	}
	lastLive, err := recordauth.ParseCanonicalVisibilityScope(originIdentity)
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, fmt.Errorf("%w: last live scope", ErrRecordSubjectUnavailable)
	}
	if err := assertLocalSourceDeletionDigest(ctx, reader.local, projectID, kind, sourceID, floor.CanonicalHash); err != nil {
		return WitnessedRecordSubjectTombstone{}, err
	}
	return WitnessedRecordSubjectTombstone{
		Version:                  WitnessedRecordSubjectTombstoneVersionV1,
		ProjectID:                projectID,
		Kind:                     kind,
		SourceID:                 sourceID,
		AuthorizationFloor:       floor,
		LastLiveScope:            lastLive,
		AuthorizationFloorDigest: floor.CanonicalHash,
	}, nil
}

func assertLocalSourceDeletionDigest(
	ctx context.Context,
	local currentRecordAuthorizationDB,
	projectID recordauth.ProjectID,
	kind recordauth.SourceKind,
	sourceID string,
	want [32]byte,
) error {
	if nilRecordSubjectDependency(local) {
		return nil
	}
	var digest []byte
	err := local.QueryRow(ctx, `
		select authorization_floor_digest
		from public.source_deletion_tombstones
		where project_id = $1
		  and source_kind = $2
		  and source_id = $3
	`, string(projectID), string(kind), sourceID).Scan(&digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: local source deletion digest", ErrRecordSubjectUnavailable)
	}
	if len(digest) != sha256.Size || digest32(digest) != want {
		return ErrRecordSubjectUnavailable
	}
	return nil
}

func witnessedSourceObjectKind(kind recordauth.SourceKind) (string, bool) {
	switch kind {
	case recordauth.SourceKindVPS:
		return "vps", true
	case recordauth.SourceKindMonitoringInstance:
		return "monitoring_instance", true
	case recordauth.SourceKindTarget:
		return "target", true
	default:
		return "", false
	}
}

func digest32(value []byte) [32]byte {
	var digest [32]byte
	copy(digest[:], value)
	return digest
}

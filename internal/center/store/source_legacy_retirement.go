package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
)

// LegacySourceObservation is an actual C80 APP observation, not a retirement
// receipt. Its timestamp is selected before any irreversible source mutation.
type LegacySourceObservation struct {
	Actor      recordauth.ActorScope
	Revision   [32]byte
	LastLive   recordauth.VisibilityScope
	RecordedAt time.Time
}

// ObserveLegacySourceRetirement locks and observes the original live source in
// the caller's READ COMMITTED transaction. The caller owns the transaction and
// must hold the independent maintenance fence throughout the observation.
func ObserveLegacySourceRetirement(ctx context.Context, tx pgx.Tx, actorUserID string, key sourceretirement.SourceKey) (LegacySourceObservation, error) {
	observation, err := observeLegacySourceRetirement(ctx, tx, actorUserID, key)
	if err != nil {
		return LegacySourceObservation{}, err
	}
	var recordedAt time.Time
	if err := tx.QueryRow(ctx, `select pg_catalog.clock_timestamp()`).Scan(&recordedAt); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("read legacy observation time", err)
	}
	recordedAt = recordedAt.UTC()
	// PostgreSQL timestamps have microsecond precision. Check the full
	// roundtrip instead of silently truncating a different original instant.
	if recordedAt.IsZero() || recordedAt.Year() < 1970 || recordedAt.Year() > 9999 ||
		recordedAt.Nanosecond()%1000 != 0 || recordedAt.UnixMicro() <= 0 ||
		!time.UnixMicro(recordedAt.UnixMicro()).UTC().Equal(recordedAt) {
		return LegacySourceObservation{}, sourceRetirementFailure("validate legacy observation time", nil)
	}
	var roundTrip time.Time
	if err := tx.QueryRow(ctx, `select $1::timestamptz`, recordedAt).Scan(&roundTrip); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("roundtrip legacy observation time", err)
	}
	if !roundTrip.Equal(recordedAt) || roundTrip.Nanosecond()%1000 != 0 {
		return LegacySourceObservation{}, sourceRetirementFailure("compare legacy observation time roundtrip", nil)
	}
	observation.RecordedAt = recordedAt
	return observation, nil
}

// observeLegacySourceRetirement deliberately uses only C80 relations and
// fixed SQL. In particular, the C81 source admission and revision functions
// are unavailable on the predecessor being observed.
func observeLegacySourceRetirement(ctx context.Context, tx pgx.Tx, actorUserID string, key sourceretirement.SourceKey) (LegacySourceObservation, error) {
	if ctx == nil || tx == nil {
		return LegacySourceObservation{}, sourceRetirementFailure("configure legacy observation", nil)
	}
	if err := recordauth.ValidateActorUserID(actorUserID); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("validate legacy actor", err)
	}
	lastLive, err := legacySourceLiveScope(key)
	if err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("validate legacy source", err)
	}

	// The graph lock is a separate statement, so READ COMMITTED takes a fresh
	// business snapshot after a wait for the previous holder.
	var isolation string
	if err := tx.QueryRow(ctx, `show transaction_isolation`).Scan(&isolation); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("read legacy transaction isolation", err)
	}
	if isolation != "read committed" {
		return LegacySourceObservation{}, sourceRetirementFailure("validate legacy transaction isolation", nil)
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(1213154899, 1)`); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("lock legacy source graph", err)
	}
	if _, err := tx.Exec(ctx, `lock table public.users, public.record_access_groups, public.record_access_group_members in share mode`); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("lock legacy actor and groups", err)
	}

	var role string
	var groupIDs []string
	if err := tx.QueryRow(ctx, `select u.role, array(
		select m.group_id from public.record_access_group_members m
		join public.record_access_groups g on g.group_id = m.group_id
		where m.user_id = u.user_id and g.project_id = $2
		order by m.group_id collate "C")
		from public.users u where u.user_id = $1 and u.disabled_at is null`, actorUserID, string(key.ProjectID)).Scan(&role, &groupIDs); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("read legacy actor", err)
	}
	if role != "admin" {
		return LegacySourceObservation{}, sourceRetirementFailure("authorize legacy actor", nil)
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: actorUserID, Role: recordauth.RoleProjectAdmin, ProjectID: key.ProjectID, GroupIDs: groupIDs,
	})
	if err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("normalize legacy actor", err)
	}

	// The deployment tuple must match the activated APP singleton. Physical
	// APP identity and maintenance provenance remain the caller's obligation.
	var deployment string
	if err := tx.QueryRow(ctx, `select deployment_id from public.deployment_contract_state
		where project_id = $1 and activation_sequence = 1
		and active_profile in ('postgres_sync', 's3_worm') and minimum_fence_contract_version > 0
		for share`, string(key.ProjectID)).Scan(&deployment); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("read legacy deployment", err)
	}
	if deployment != key.DeploymentID {
		return LegacySourceObservation{}, sourceRetirementFailure("compare legacy deployment", nil)
	}

	revision, err := legacySourceRevision(ctx, tx, key)
	if err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("read legacy source revision", err)
	}
	var tombstoned bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from public.source_deletion_tombstones
		where project_id = $1 and source_kind = $2 and source_id = $3)`,
		string(key.ProjectID), string(key.Kind), key.ID).Scan(&tombstoned); err != nil {
		return LegacySourceObservation{}, sourceRetirementFailure("read legacy tombstone absence", err)
	}
	if tombstoned {
		return LegacySourceObservation{}, sourceRetirementFailure("source already has legacy tombstone", nil)
	}
	return LegacySourceObservation{Actor: actor, Revision: revision, LastLive: lastLive}, nil
}

func legacySourceLiveScope(key sourceretirement.SourceKey) (recordauth.VisibilityScope, error) {
	if err := recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(key.DeploymentID)); err != nil {
		return recordauth.VisibilityScope{}, err
	}
	if err := recordauth.ValidateProjectID(key.ProjectID); err != nil {
		return recordauth.VisibilityScope{}, err
	}
	// The existing VPS, monitoring instance and target live adapters all emit
	// this project-wide source scope, independently of presentation fields.
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: key.ProjectID, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: recordSubjectSourcePolicyRevisionV1,
	})
	if err != nil {
		return recordauth.VisibilityScope{}, err
	}
	_, err = recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: key.Kind, SourceID: key.ID,
		State: recordauth.SourceStateLive, CaptureScope: scope, CurrentScope: &scope,
	})
	if err != nil {
		return recordauth.VisibilityScope{}, err
	}
	return scope, nil
}

func legacySourceRevision(ctx context.Context, tx pgx.Tx, key sourceretirement.SourceKey) ([32]byte, error) {
	var statement string
	switch key.Kind {
	case recordauth.SourceKindVPS:
		statement = `select pg_catalog.convert_to(pg_catalog.jsonb_build_array(vps_id, extract(epoch from updated_at), lifecycle_status)::text, 'UTF8')
			from public.vps_assets where vps_id = $1 for update`
	case recordauth.SourceKindMonitoringInstance:
		statement = `select pg_catalog.convert_to(pg_catalog.jsonb_build_array(monitoring_instance_id, extract(epoch from metadata_updated_at), control_revision, lifecycle_status)::text, 'UTF8')
			from public.monitoring_instances where monitoring_instance_id = $1 for update`
	case recordauth.SourceKindTarget:
		statement = `select pg_catalog.convert_to(pg_catalog.jsonb_build_array(target_id, extract(epoch from updated_at), control_revision, run_status)::text, 'UTF8')
			from public.targets where target_id = $1 for update`
	default:
		return [32]byte{}, recordauth.ErrInvalidSourceAuthorization
	}
	var body []byte
	if err := tx.QueryRow(ctx, statement, key.ID).Scan(&body); err != nil {
		return [32]byte{}, err
	}
	// Match the existing source-retirement revision byte-for-byte without
	// calling its C81-only SQL function or depending on a pgcrypto grant.
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("HOUFENG-SOURCE-REVISION-V2"))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(key.Kind)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write([]byte(key.Kind))
	_, _ = hasher.Write(body)
	var revision [32]byte
	hasher.Sum(revision[:0])
	if revision == ([32]byte{}) {
		return [32]byte{}, recordauth.ErrInvalidSourceAuthorization
	}
	return revision, nil
}

// InsertLegacySourceRetirement writes only the original request's C80
// projection after reobserving the actor, source, revision and absence in the
// same transaction. It neither commits nor establishes a ledger capability.
func InsertLegacySourceRetirement(ctx context.Context, tx pgx.Tx, request deletionledger.SourceRetirementRequest) error {
	if ctx == nil || tx == nil {
		return sourceRetirementFailure("configure legacy insertion", nil)
	}
	if err := request.Validate(); err != nil {
		return sourceRetirementFailure("validate legacy request", err)
	}
	observation, err := observeLegacySourceRetirement(ctx, tx, request.ActorUserID, request.Origin.Source)
	if err != nil {
		return err
	}
	if observation.Revision != request.Origin.Revision ||
		!bytes.Equal(observation.LastLive.CanonicalBytes(), request.Origin.LastLive.CanonicalBytes()) {
		return sourceRetirementFailure("compare original legacy observation", nil)
	}
	floor, err := sourceRetirementCanonicalScope(request.FinalFloor, request.Origin.Source.ProjectID)
	if err != nil {
		return sourceRetirementFailure("validate legacy floor", err)
	}
	if err := sourceRetirementTransition(request.Origin.Source, observation.LastLive, floor); err != nil {
		return sourceRetirementFailure("validate legacy floor transition", err)
	}
	digest := floor.CanonicalHashValue()
	key := request.Origin.Source
	result, err := tx.Exec(ctx, `insert into public.source_deletion_tombstones
		(project_id, source_kind, source_id, authorization_floor_digest, deleted_at)
		values ($1, $2, $3, $4, $5)`, string(key.ProjectID), string(key.Kind), key.ID,
		digest[:], request.Origin.TombstoneRecordedAt)
	if err != nil {
		return sourceRetirementFailure("insert legacy tombstone", err)
	}
	if result.RowsAffected() != 1 {
		return sourceRetirementFailure("validate legacy insert count", nil)
	}
	return ReadLegacySourceRetirementProjection(ctx, tx, request)
}

// ReadLegacySourceRetirementProjection checks only the exact committed C80
// projection. A match is not authority without the caller's physical APP
// identity, original absence, backend-settlement and intent provenance.
func ReadLegacySourceRetirementProjection(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, request deletionledger.SourceRetirementRequest) error {
	if ctx == nil || db == nil {
		return sourceRetirementFailure("configure legacy projection read", nil)
	}
	if err := request.Validate(); err != nil {
		return sourceRetirementFailure("validate legacy projection request", err)
	}
	key := request.Origin.Source
	var deployment, project, kind, id string
	var digest []byte
	var deletedAt time.Time
	if err := db.QueryRow(ctx, `select c.deployment_id, t.project_id, t.source_kind, t.source_id, t.authorization_floor_digest, t.deleted_at
		from public.source_deletion_tombstones t
		join public.deployment_contract_state c on c.project_id = t.project_id
		where t.project_id = $1 and t.source_kind = $2 and t.source_id = $3
		and c.deployment_id = $4 and c.activation_sequence = 1
		and c.active_profile in ('postgres_sync', 's3_worm') and c.minimum_fence_contract_version > 0`,
		string(key.ProjectID), string(key.Kind), key.ID, key.DeploymentID).
		Scan(&deployment, &project, &kind, &id, &digest, &deletedAt); err != nil {
		return sourceRetirementFailure("read legacy projection", err)
	}
	wantDigest := request.FinalFloor.CanonicalHashValue()
	if deployment != key.DeploymentID || project != string(key.ProjectID) || kind != string(key.Kind) || id != key.ID ||
		!bytes.Equal(digest, wantDigest[:]) || !deletedAt.Equal(request.Origin.TombstoneRecordedAt) {
		return sourceRetirementFailure("compare legacy projection", nil)
	}
	return nil
}

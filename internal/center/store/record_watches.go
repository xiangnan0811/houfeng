package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type PostgresRecordWatchRepository struct {
	platform      *PostgresRecordPlatformRepository
	members       CollaborationMembershipReader
	authorization *PostgresCurrentRecordAuthorizationSource
}

func NewPostgresRecordWatchRepository(
	pool *pgxpool.Pool,
	gate AdmissionGate,
	members CollaborationMembershipReader,
	authorization *PostgresCurrentRecordAuthorizationSource,
) *PostgresRecordWatchRepository {
	return &PostgresRecordWatchRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate), members: members, authorization: authorization,
	}
}

func (repository *PostgresRecordWatchRepository) SetWatch(ctx context.Context, command recordcollaboration.WatchCommand) (recordcollaboration.WatchStatus, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) ||
		repository.authorization == nil || nilRecordSubjectDependency(repository.authorization.resolver) {
		return recordcollaboration.WatchStatus{}, recordcollaboration.ErrInvalidWatchCommand
	}
	if err := command.Validate(); err != nil {
		return recordcollaboration.WatchStatus{}, err
	}
	var result recordcollaboration.WatchStatus
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, command.Idempotency)
		if err != nil {
			return err
		}
		if (claim.ReplayResult == nil) == (claim.Owner == nil) {
			return recordcollaboration.ErrInvalidWatchCommand
		}
		if err := assertRecordMutationFence(ctx, transaction.tx, command.RecordID); err != nil {
			return err
		}
		binding, err := loadCollaborationRecordFenceBinding(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		root, err := lockRecordRoot(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		if root.currentRevisionID == nil || *root.currentRevisionID != command.CurrentRevisionID ||
			root.lockVersion != command.RecordLockVersion || root.authorizationEpoch != command.AuthorizationEpoch ||
			root.lifecycle != records.LifecycleActive || root.projectID != string(recordplatform.ProjectIDDefault) {
			return recordcollaboration.ErrWatchConflict
		}
		member, err := repository.members.ReadMemberActor(ctx, transaction.tx, command.Actor.ProjectID, command.Actor.UserID)
		if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
			return recordauth.ErrDenied
		}
		if err != nil {
			return err
		}
		if member.UserID != command.Actor.UserID || member.ProjectID != command.Actor.ProjectID || member.Role != command.Actor.Role {
			return recordauth.ErrDenied
		}
		currentAuthorization, err := repository.authorization.resolveCurrentAuthorizationInTransaction(
			ctx, transaction.tx, member, command.RecordID,
		)
		if err != nil {
			return err
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityNotificationManage, currentAuthorization.Evidence); err != nil {
			return err
		}
		if !currentRecordAuthorizationMatchesExpected(currentAuthorization, command.RecordID, command.CurrentRevisionID, command.RecordLockVersion,
			command.AuthorizationEpoch, command.AuthorizationEvidence) {
			return recordcollaboration.ErrWatchConflict
		}
		current, err := loadRecordWatchStatus(ctx, transaction.tx, command.RecordID, command.Actor.UserID, binding.Epoch(), true)
		if err != nil {
			return err
		}
		if claim.ReplayResult != nil {
			resultFingerprint, err := current.ResultFingerprint(command.Idempotency.Key)
			if err != nil || !resultFingerprint.MatchesPersisted(*claim.ReplayResult) {
				return recordplatform.ErrIdempotencyConflictState
			}
			if current.Version > 0 {
				marker, err := loadRecordWatchResultFingerprint(ctx, transaction.tx, command.RecordID, command.Actor.UserID)
				if err != nil || !resultFingerprint.MatchesPersisted(marker) {
					return recordplatform.ErrIdempotencyConflictState
				}
			}
			result = current
			return nil
		}
		if current.Version != command.ExpectedVersion {
			return recordcollaboration.ErrWatchConflict
		}
		next, err := nextRecordWatchPreference(current, command.Preference, uint64(binding.Epoch()))
		if err != nil {
			return err
		}
		if current.Version == 0 {
			var updatedAt time.Time
			provisionalMarker := make([]byte, sha256.Size)
			err := transaction.tx.QueryRow(ctx, `
				insert into public.record_followers (
					project_id, record_id, user_id, follower_version, manual_preference,
					preference_result_fingerprint, record_fence_epoch, created_at, updated_at
				) values ($1, $2, $3, 1, $4, $5, $6, transaction_timestamp(), transaction_timestamp())
				returning updated_at`, recordplatform.ProjectIDDefault, command.RecordID, command.Actor.UserID,
				string(command.Preference), provisionalMarker, int64(binding.Epoch())).Scan(&updatedAt)
			if err != nil {
				return fmt.Errorf("insert record watch preference: %w", err)
			}
			next.UpdatedAt = updatedAt.UTC()
			result = next
		} else {
			var updatedAt time.Time
			err := transaction.tx.QueryRow(ctx, `
				update public.record_followers
				set follower_version = follower_version + 1,
				    manual_preference = $4,
				    record_fence_epoch = $5,
				    updated_at = transaction_timestamp()
				where record_id = $1 and user_id = $2 and follower_version = $3
				returning updated_at`, command.RecordID, command.Actor.UserID, int64(current.Version),
				string(command.Preference), int64(binding.Epoch())).Scan(&updatedAt)
			if errors.Is(err, pgx.ErrNoRows) {
				return recordcollaboration.ErrWatchConflict
			}
			if err != nil {
				return fmt.Errorf("update record watch preference: %w", err)
			}
			next.UpdatedAt = updatedAt.UTC()
			result = next
		}
		resultFingerprint, err := result.ResultFingerprint(command.Idempotency.Key)
		if err != nil {
			return err
		}
		if result.Version > 0 {
			persisted, err := resultFingerprint.PersistedBytes()
			if err != nil {
				return err
			}
			tag, err := transaction.tx.Exec(ctx, `
				update public.record_followers
				set preference_result_fingerprint = $5
				where record_id = $1 and user_id = $2 and follower_version = $3 and record_fence_epoch = $4`,
				result.RecordID, result.UserID, int64(result.Version), int64(result.RecordFenceEpoch), persisted[:])
			if err != nil {
				return fmt.Errorf("bind record watch result fingerprint: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return recordcollaboration.ErrWatchConflict
			}
		}
		if err := transaction.CompleteIdempotency(ctx, command.Idempotency.Key, *claim.Owner, resultFingerprint); err != nil {
			return err
		}
		return result.Validate()
	})
	if err != nil {
		return recordcollaboration.WatchStatus{}, err
	}
	return result, nil
}

func loadRecordWatchResultFingerprint(
	ctx context.Context,
	tx pgx.Tx,
	recordID string,
	userID string,
) (recordplatform.PersistedRequestFingerprintV1, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `
		select preference_result_fingerprint
		from public.record_followers
		where record_id = $1 and user_id = $2`, recordID, userID).Scan(&raw); err != nil {
		return recordplatform.PersistedRequestFingerprintV1{}, err
	}
	return recordplatform.ParseTrustedPersistedRequestFingerprintV1(raw)
}

func (repository *PostgresRecordWatchRepository) GetWatch(ctx context.Context, command recordcollaboration.WatchReadCommand) (recordcollaboration.WatchStatus, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) ||
		repository.authorization == nil || nilRecordSubjectDependency(repository.authorization.resolver) {
		return recordcollaboration.WatchStatus{}, recordcollaboration.ErrInvalidWatchCommand
	}
	if err := command.Validate(); err != nil {
		return recordcollaboration.WatchStatus{}, err
	}
	var result recordcollaboration.WatchStatus
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := assertRecordReadFence(ctx, transaction.tx, command.RecordID); err != nil {
			return err
		}
		binding, err := loadCollaborationRecordReadFenceBinding(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		root, err := lockRecordRootForCommentRead(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		if root.currentRevisionID == nil || *root.currentRevisionID != command.CurrentRevisionID ||
			root.lockVersion != command.RecordLockVersion || root.authorizationEpoch != command.AuthorizationEpoch ||
			root.lifecycle != records.LifecycleActive || root.projectID != string(recordplatform.ProjectIDDefault) {
			return recordcollaboration.ErrWatchConflict
		}
		member, err := repository.members.ReadMemberActor(ctx, transaction.tx, command.Actor.ProjectID, command.Actor.UserID)
		if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
			return recordauth.ErrDenied
		}
		if err != nil {
			return err
		}
		if member.UserID != command.Actor.UserID || member.ProjectID != command.Actor.ProjectID || member.Role != command.Actor.Role {
			return recordauth.ErrDenied
		}
		currentAuthorization, err := repository.authorization.resolveCurrentAuthorizationInTransaction(
			ctx, transaction.tx, member, command.RecordID,
		)
		if err != nil {
			return err
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityNotificationRead, currentAuthorization.Evidence); err != nil {
			return err
		}
		if !currentRecordAuthorizationMatchesExpected(currentAuthorization, command.RecordID, command.CurrentRevisionID, command.RecordLockVersion,
			command.AuthorizationEpoch, command.AuthorizationEvidence) {
			return recordcollaboration.ErrWatchConflict
		}
		result, err = loadRecordWatchStatus(ctx, transaction.tx, command.RecordID, command.Actor.UserID, binding.Epoch(), false)
		return err
	})
	if err != nil {
		return recordcollaboration.WatchStatus{}, err
	}
	return result, nil
}

func currentRecordAuthorizationMatchesExpected(
	current records.CurrentRecordAuthorization,
	recordID string,
	currentRevisionID string,
	lockVersion uint64,
	authorizationEpoch uint64,
	expected records.RecordAuthorizationEvidence,
) bool {
	if current.RecordID != recordID || current.CurrentRevisionID != currentRevisionID || current.LockVersion != lockVersion ||
		current.AuthorizationEpoch != authorizationEpoch || current.Lifecycle != records.LifecycleActive ||
		current.Evidence.ProjectID != expected.ProjectID || current.Evidence.Visibility.CanonicalHash != expected.Visibility.CanonicalHash ||
		len(current.Evidence.Sources) != len(expected.Sources) {
		return false
	}
	for index := range current.Evidence.Sources {
		left, right := current.Evidence.Sources[index], expected.Sources[index]
		if left.Version != right.Version || left.Kind != right.Kind || left.SourceID != right.SourceID ||
			left.State != right.State || left.Digest != right.Digest {
			return false
		}
	}
	return true
}

func loadRecordWatchStatus(ctx context.Context, tx pgx.Tx, recordID, userID string, epoch recordplatform.ContentEpoch, forUpdate bool) (recordcollaboration.WatchStatus, error) {
	sql := `
		select follower_version, manual_preference,
		       follows_author, follows_owner, follows_participant,
		       follows_comment, follows_mention, follows_action,
		       record_fence_epoch, updated_at
		from public.record_followers
		where record_id = $1 and user_id = $2`
	if forUpdate {
		sql += " for update"
	}
	status := recordcollaboration.WatchStatus{RecordID: recordID, UserID: userID, Preference: recordcollaboration.FollowerPreferenceDefault, RecordFenceEpoch: uint64(epoch)}
	var version, fence int64
	var preference string
	err := tx.QueryRow(ctx, sql, recordID, userID).Scan(
		&version, &preference, &status.Sources.Author, &status.Sources.Owner, &status.Sources.Participant,
		&status.Sources.Comment, &status.Sources.Mention, &status.Sources.Action, &fence, &status.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return recordcollaboration.WatchStatus{}, fmt.Errorf("read record watch preference: %w", err)
	}
	if version <= 0 || fence < 0 || uint64(fence) != uint64(epoch) {
		return recordcollaboration.WatchStatus{}, recordcollaboration.ErrWatchConflict
	}
	status.Version = uint64(version)
	status.Preference = recordcollaboration.FollowerPreference(preference)
	status.RecordFenceEpoch = uint64(fence)
	status.UpdatedAt = status.UpdatedAt.UTC()
	if status.Validate() != nil {
		return recordcollaboration.WatchStatus{}, recordcollaboration.ErrWatchConflict
	}
	return status, nil
}

func nextRecordWatchPreference(current recordcollaboration.WatchStatus, preference recordcollaboration.FollowerPreference, fenceEpoch uint64) (recordcollaboration.WatchStatus, error) {
	if recordcollaboration.ValidateFollowerPreference(preference) != nil || current.Validate() != nil || current.Version >= math.MaxInt64 || fenceEpoch > math.MaxInt64 {
		return recordcollaboration.WatchStatus{}, recordcollaboration.ErrInvalidWatchCommand
	}
	next := current
	if next.Version == 0 {
		next.Version = 1
	} else {
		next.Version++
	}
	next.Preference = preference
	next.RecordFenceEpoch = fenceEpoch
	next.UpdatedAt = time.Time{}
	return next, nil
}

var _ recordcollaboration.WatchStore = (*PostgresRecordWatchRepository)(nil)

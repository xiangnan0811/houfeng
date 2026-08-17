package store

import (
	"context"
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
	platform *PostgresRecordPlatformRepository
	members  CollaborationMembershipReader
}

func NewPostgresRecordWatchRepository(pool *pgxpool.Pool, gate AdmissionGate, members CollaborationMembershipReader) *PostgresRecordWatchRepository {
	return &PostgresRecordWatchRepository{platform: NewPostgresRecordPlatformRepository(pool, gate), members: members}
}

func (repository *PostgresRecordWatchRepository) SetWatch(ctx context.Context, command recordcollaboration.WatchCommand) (recordcollaboration.WatchStatus, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) {
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
		if err != nil {
			return err
		}
		if member.UserID != command.Actor.UserID || member.ProjectID != command.Actor.ProjectID {
			return recordcollaboration.ErrMembershipDenied
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityNotificationManage, command.AuthorizationEvidence); err != nil {
			return err
		}
		current, err := loadRecordWatchStatus(ctx, transaction.tx, command.RecordID, command.Actor.UserID, binding.Epoch(), true)
		if err != nil {
			return err
		}
		if claim.ReplayResult != nil {
			if !command.ResultFingerprint.MatchesPersisted(*claim.ReplayResult) {
				return recordplatform.ErrIdempotencyConflictState
			}
			result = current
			return nil
		}
		if current.Version != command.ExpectedVersion {
			return recordcollaboration.ErrWatchConflict
		}
		if current.Version == 0 && command.Preference == recordcollaboration.FollowerPreferenceDefault {
			result = current
		} else {
			next, remove, err := nextRecordWatchPreference(current, command.Preference, uint64(binding.Epoch()))
			if err != nil {
				return err
			}
			if remove {
				var removed int64
				encoded, err := encodeCollaborationDeleteCommand(collaborationRemoveFollowerFunctionCommand{
					RecordID: command.RecordID, UserID: command.Actor.UserID,
					Version: int64(current.Version), FenceEpoch: int64(binding.Epoch()),
				})
				if err != nil {
					return recordcollaboration.ErrWatchConflict
				}
				err = transaction.tx.QueryRow(ctx, `
					select public.record_collaboration_remove_follower($1)`, encoded).Scan(&removed)
				if err != nil {
					return fmt.Errorf("delete empty record watch preference: %w", err)
				}
				if removed != 1 {
					return recordcollaboration.ErrWatchConflict
				}
				result = next
			} else if current.Version == 0 {
				var updatedAt time.Time
				err := transaction.tx.QueryRow(ctx, `
					insert into public.record_followers (
						project_id, record_id, user_id, follower_version, manual_preference,
						record_fence_epoch, created_at, updated_at
					) values ($1, $2, $3, 1, $4, $5, transaction_timestamp(), transaction_timestamp())
					returning updated_at`, recordplatform.ProjectIDDefault, command.RecordID, command.Actor.UserID,
					string(command.Preference), int64(binding.Epoch())).Scan(&updatedAt)
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
		}
		if err := transaction.CompleteIdempotency(ctx, command.Idempotency.Key, *claim.Owner, command.ResultFingerprint); err != nil {
			return err
		}
		return result.Validate()
	})
	if err != nil {
		return recordcollaboration.WatchStatus{}, err
	}
	return result, nil
}

func (repository *PostgresRecordWatchRepository) GetWatch(ctx context.Context, command recordcollaboration.WatchReadCommand) (recordcollaboration.WatchStatus, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) {
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
		if err != nil {
			return err
		}
		if member.UserID != command.Actor.UserID || member.ProjectID != command.Actor.ProjectID {
			return recordcollaboration.ErrMembershipDenied
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityNotificationRead, command.AuthorizationEvidence); err != nil {
			return err
		}
		result, err = loadRecordWatchStatus(ctx, transaction.tx, command.RecordID, command.Actor.UserID, binding.Epoch(), false)
		return err
	})
	if err != nil {
		return recordcollaboration.WatchStatus{}, err
	}
	return result, nil
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

func nextRecordWatchPreference(current recordcollaboration.WatchStatus, preference recordcollaboration.FollowerPreference, fenceEpoch uint64) (recordcollaboration.WatchStatus, bool, error) {
	if recordcollaboration.ValidateFollowerPreference(preference) != nil || current.Validate() != nil || current.Version >= math.MaxInt64 || fenceEpoch > math.MaxInt64 {
		return recordcollaboration.WatchStatus{}, false, recordcollaboration.ErrInvalidWatchCommand
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
	if preference == recordcollaboration.FollowerPreferenceDefault && !next.Sources.Any() {
		next.Version = 0
		next.UpdatedAt = time.Time{}
		return next, true, nil
	}
	return next, false, nil
}

var _ recordcollaboration.WatchStore = (*PostgresRecordWatchRepository)(nil)

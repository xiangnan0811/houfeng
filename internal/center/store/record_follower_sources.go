package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
)

type collaborationAutomaticFollowerSources struct {
	userID  string
	comment bool
	mention bool
	action  bool
}

func upsertCollaborationAutomaticFollowerSources(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
	sources []collaborationAutomaticFollowerSources,
) error {
	if ctx == nil || nilCollaborationMembershipTx(tx) || binding.Validate() != nil {
		return recordcollaboration.ErrInvalidNotificationFacts
	}
	byUser := make(map[string]collaborationAutomaticFollowerSources, len(sources))
	for _, source := range sources {
		if recordauth.ValidateActorUserID(source.userID) != nil || (!source.comment && !source.mention && !source.action) {
			return recordcollaboration.ErrInvalidNotificationFacts
		}
		merged := byUser[source.userID]
		merged.userID = source.userID
		merged.comment = merged.comment || source.comment
		merged.mention = merged.mention || source.mention
		merged.action = merged.action || source.action
		byUser[source.userID] = merged
	}
	userIDs := make([]string, 0, len(byUser))
	for userID := range byUser {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	for _, userID := range userIDs {
		source := byUser[userID]
		if _, err := tx.Exec(ctx, `
			insert into public.record_followers (
				project_id, record_id, user_id, follower_version,
				follows_comment, follows_mention, follows_action, record_fence_epoch
			) values ($1, $2, $3, 1, $4, $5, $6, $7)
			on conflict (record_id, user_id) do update
			set follower_version = record_followers.follower_version + 1,
			    follows_comment = record_followers.follows_comment or excluded.follows_comment,
			    follows_mention = record_followers.follows_mention or excluded.follows_mention,
			    follows_action = record_followers.follows_action or excluded.follows_action,
			    record_fence_epoch = excluded.record_fence_epoch,
			    updated_at = transaction_timestamp()
			where record_followers.record_fence_epoch is distinct from excluded.record_fence_epoch
			   or (excluded.follows_comment and not record_followers.follows_comment)
			   or (excluded.follows_mention and not record_followers.follows_mention)
			   or (excluded.follows_action and not record_followers.follows_action)`,
			recordplatform.ProjectIDDefault, binding.RecordID(), userID,
			source.comment, source.mention, source.action, int64(binding.Epoch()),
		); err != nil {
			return fmt.Errorf("upsert collaboration automatic follower source: %w", err)
		}
	}
	return nil
}

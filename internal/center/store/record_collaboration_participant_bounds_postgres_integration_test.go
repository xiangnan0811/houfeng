package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"houfeng/internal/center/recordcollaboration"
)

func TestPostgresIntegrationCollaborationRevisionParticipantBoundsDesiredFollowersAt512(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	participantIDs := make([]string, 0, 511)
	for index := 0; index < 511; index++ {
		userID := fmt.Sprintf("usr_%024x", index+1)
		participantIDs = append(participantIDs, userID)
		if _, err := fixture.db.Exec(ctx, `
			insert into public.users (user_id, username, password_hash, display_name, role)
			values ($1, $2, 'test-hash', $2, 'admin')`, userID, fmt.Sprintf("collaboration-bound-%d", index)); err != nil {
			t.Fatalf("seed collaboration bound user %q: %v", userID, err)
		}
	}
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "collaboration-follower-bound", 2)
	birthFixture := ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}
	repository := newRevisionMutationAdmittedRepository(t, birthFixture,
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()))

	exactInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: participantIDs[:510],
	})
	exact, err := repository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture, exactInput, "collaboration-bound-exact"))
	if err != nil {
		t.Fatalf("CommitRevision(512 desired followers) error = %v", err)
	}
	var followerCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_followers where record_id = $1`, exact.RecordID,
	).Scan(&followerCount); err != nil {
		t.Fatalf("count exact-bound followers: %v", err)
	}
	if followerCount != 512 {
		t.Fatalf("exact-bound follower count = %d, want 512", followerCount)
	}

	overInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: participantIDs,
	})
	overCommand := revisionBirthBatchCommand(t, ctx, birthFixture, overInput, "collaboration-bound-over")
	if got, err := repository.CommitRevision(ctx, overCommand); !errors.Is(err, recordcollaboration.ErrRevisionParticipationUnavailable) || got.RecordID != "" {
		t.Fatalf("CommitRevision(513 desired followers) = (%#v, %v), want bounded rollback", got, err)
	}
	var rootCount, followerResidue, keyCount int
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.records where record_id = $1),
		  (select count(*)::int from public.record_followers where record_id = $1),
		  (select count(*)::int from public.record_idempotency_keys where idempotency_key = 'collaboration-bound-over')
	`, overCommand.RecordID).Scan(&rootCount, &followerResidue, &keyCount); err != nil {
		t.Fatalf("count over-bound rollback residue: %v", err)
	}
	if rootCount != 0 || followerResidue != 0 || keyCount != 0 {
		t.Fatalf("over-bound residue roots/followers/keys = %d/%d/%d, want zero", rootCount, followerResidue, keyCount)
	}
}

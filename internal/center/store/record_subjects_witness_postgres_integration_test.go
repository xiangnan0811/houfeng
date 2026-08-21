package store

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresWitnessedRecordSubjectTombstoneSourceRejectsDigestOnlyLocalProjection(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "witnessed-subject-tombstone", 1)
	floor := mustStoreProjectVisibility(t)

	if _, err := pool.Exec(ctx, `
		insert into public.source_deletion_tombstones (
			project_id, source_kind, source_id, authorization_floor_digest
		) values ($1, $2, $3, $4)
	`, string(recordauth.ProjectIDDefault), string(recordauth.SourceKindVPS), testStoreRecordVPSID, floor.CanonicalHash[:]); err != nil {
		t.Fatalf("insert digest-only source_deletion_tombstones: %v", err)
	}

	identity := testDeploymentMembershipIdentity()
	reader, err := NewWitnessedRecordSubjectTombstoneReader(
		recordplatform.DeploymentID(identity.DeploymentID),
		recordauth.ProjectID(identity.ProjectID),
		nil,
		pool,
	)
	if err != nil {
		t.Fatalf("NewWitnessedRecordSubjectTombstoneReader() error = %v", err)
	}
	_, err = reader.ResolveWitnessedRecordSubjectTombstone(
		ctx,
		recordauth.ProjectIDDefault,
		recordauth.SourceKindVPS,
		testStoreRecordVPSID,
	)
	if !errors.Is(err, ErrRecordSubjectUnavailable) && !errors.Is(err, ErrWitnessedRecordSubjectTombstoneNotFound) {
		t.Fatalf("digest-only Resolve() error = %v, want fail-closed", err)
	}
}

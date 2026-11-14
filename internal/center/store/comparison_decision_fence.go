package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

// Two-integer advisory keys use PostgreSQL's separate key space from the
// single-bigint hashes used by idempotency and capacity coordination.
const (
	recordAdvisoryClassID            int32 = 1213154899
	assetGraphAdvisoryKey            int32 = 1
	comparisonDecisionFenceKey       int32 = 81
	recordSearchCoordinationKey      int32 = 82
	recordIdempotencyAdvisoryClassID int32 = 1213154900
)

// Call after source/asset-graph and required namespace admission, before any
// record or actor row locks. The transaction owns this fence through commit;
// a session advisory lock is not a substitute for that lifetime.
func lockComparisonDecisionForWriter(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock_shared($1, $2)`, recordAdvisoryClassID, comparisonDecisionFenceKey); err != nil {
		return fmt.Errorf("lock comparison decision for writer: %w", err)
	}
	return nil
}

// Generation absence is serialized before any generation or rebuild-job row.
// C81 supports exactly the default project; another project needs its own
// explicit key contract rather than a hash folded into this fixed domain.
func lockRecordSearchCoordination(ctx context.Context, tx pgx.Tx, projectID string) error {
	if projectID != string(recordplatform.ProjectIDDefault) {
		return fmt.Errorf("invalid record search coordination project")
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock($1, $2)`, recordAdvisoryClassID, recordSearchCoordinationKey); err != nil {
		return fmt.Errorf("lock record search coordination: %w", err)
	}
	return nil
}

// The compound key scope exists even before its durable idempotency row.
// Hash collisions only serialize unrelated keys; exact identity and request
// fingerprint checks remain on the persistent row under this transaction lock.
func lockRecordIdempotencyScope(ctx context.Context, tx pgx.Tx, key recordplatform.IdempotencyKey) error {
	framed, err := recordIdempotencyScopeFrame(key)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock($1, pg_catalog.hashtext($2))`, recordIdempotencyAdvisoryClassID, framed); err != nil {
		return fmt.Errorf("lock record idempotency scope: %w", err)
	}
	return nil
}

func recordIdempotencyScopeFrame(key recordplatform.IdempotencyKey) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%s%d:%s%d:%s", len(key.ProjectID), key.ProjectID,
		len(key.OperationKind), key.OperationKind, len(key.Key), key.Key), nil
}

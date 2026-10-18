package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
)

type acceptedSyncReplayState struct {
	retiredThrough int64
}

// Call only after graph and instance/session locks. The permanent session binding
// is never replaced, including evidence-only and stopped-collection sessions.
func acceptSyncReplayState(ctx context.Context, tx syncBatchTx, batch syncing.Batch) (acceptedSyncReplayState, error) {
	if _, err := tx.Exec(ctx, `insert into agent_replay_states(session_id,producer_id) values($1,$2) on conflict do nothing`, batch.SessionID, batch.ProducerID); err != nil {
		return acceptedSyncReplayState{}, fmt.Errorf("initialize agent replay state: %w", err)
	}
	var producer string
	var floor, allocated int64
	if err := tx.QueryRow(ctx, `select producer_id::text,retired_through,allocated_through from agent_replay_states where session_id=$1 for no key update`, batch.SessionID).Scan(&producer, &floor, &allocated); err != nil {
		return acceptedSyncReplayState{}, fmt.Errorf("lock agent replay state: %w", err)
	}
	if !strings.EqualFold(producer, batch.ProducerID) {
		return acceptedSyncReplayState{}, syncing.ErrSyncProducerMismatch
	}
	if batch.RetiredThrough > floor {
		floor = batch.RetiredThrough
	}
	if batch.BatchSequence > 0 && batch.BatchSequence <= floor {
		return acceptedSyncReplayState{}, syncing.ErrSyncBatchRetired
	}
	if batch.AllocatedThrough > allocated {
		allocated = batch.AllocatedThrough
	}
	// The database trigger is the sole receipt cleanup authority. This update
	// and all following evidence/facts/commands share the enclosing transaction.
	if _, err := tx.Exec(ctx, `update agent_replay_states set retired_through=$2,allocated_through=$3 where session_id=$1 and (retired_through<>$2 or allocated_through<>$3)`, batch.SessionID, floor, allocated); err != nil {
		return acceptedSyncReplayState{}, fmt.Errorf("advance agent replay controls: %w", err)
	}
	return acceptedSyncReplayState{retiredThrough: floor}, nil
}

func equalSyncDigest(stored []byte, expected [32]byte) bool {
	return bytes.Equal(stored, expected[:])
}

func isSyncAdmissionError(err error) bool {
	for _, admission := range []error{
		syncing.ErrInvalidSyncToken, syncing.ErrBindingNotAccepted, syncing.ErrHeartbeatRequired,
		syncing.ErrSyncProtocolRequired, syncing.ErrInvalidReplayState, syncing.ErrSyncProducerMismatch,
		syncing.ErrSyncBatchRetired, syncing.ErrSyncBatchIdentityConflict,
		monitoringinstances.ErrMonitoringInstanceNotFound, observations.ErrInvalidProbeObservation,
	} {
		if errors.Is(err, admission) {
			return true
		}
	}
	return false
}

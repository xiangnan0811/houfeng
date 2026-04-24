package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
)

type syncBatchTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresSyncRepository struct {
	beginTx func(context.Context, pgx.TxOptions) (syncBatchTx, error)
}

func NewPostgresSyncRepository(db *pgxpool.Pool) *PostgresSyncRepository {
	return &PostgresSyncRepository{
		beginTx: func(ctx context.Context, options pgx.TxOptions) (syncBatchTx, error) {
			return db.BeginTx(ctx, options)
		},
	}
}

var _ syncing.Repository = (*PostgresSyncRepository)(nil)

func (r *PostgresSyncRepository) ApplyBatch(ctx context.Context, batch syncing.Batch) error {
	if len(batch.Heartbeats) == 0 {
		if len(batch.Observations.HostSamples) == 0 && len(batch.Observations.ProbeObservations) == 0 {
			return nil
		}

		return syncing.ErrHeartbeatRequired
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin sync batch transaction for node %q: %w", batch.NodeID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	bindingFingerprint, err := validateAcceptedSyncBatch(ctx, tx, batch)
	if err != nil {
		return err
	}

	receivedAt := time.Now().UTC()
	observationBatch := batchWithReceivedAt(batch.Observations, receivedAt)
	if err := validateProbeObservations(ctx, tx, observationBatch.ProbeObservations); err != nil {
		return err
	}

	lastHeartbeatAt, err := recordHeartbeatBatch(ctx, tx, batch.NodeID, bindingFingerprint, receivedAt, batch.Heartbeats)
	if err != nil {
		return err
	}
	if err := recordObservationBatch(ctx, tx, observationBatch); err != nil {
		return err
	}
	if err := advanceNodeSyncState(ctx, tx, batch.NodeID, lastHeartbeatAt, receivedAt); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sync batch transaction for node %q: %w", batch.NodeID, err)
	}

	return nil
}

func validateAcceptedSyncBatch(ctx context.Context, tx syncBatchTx, batch syncing.Batch) (string, error) {
	var (
		bindingStatus       string
		bindingFingerprint  string
		storedSyncTokenHash string
	)
	if err := tx.QueryRow(ctx, `
		select binding_status,
			coalesce(binding_fingerprint, ''),
			coalesce(sync_token_hash, '')
		from nodes
		where node_id = $1
		for update`,
		batch.NodeID,
	).Scan(&bindingStatus, &bindingFingerprint, &storedSyncTokenHash); errors.Is(err, pgx.ErrNoRows) {
		return "", nodes.ErrNodeNotFound
	} else if err != nil {
		return "", fmt.Errorf("query sync batch state for node %q: %w", batch.NodeID, err)
	}
	if bindingStatus != nodes.BindingBound {
		return "", syncing.ErrBindingNotAccepted
	}
	if storedSyncTokenHash == "" || storedSyncTokenHash != hashSyncToken(batch.SyncToken) {
		return "", syncing.ErrInvalidSyncToken
	}

	for _, heartbeat := range batch.Heartbeats {
		if heartbeat.Fingerprint != bindingFingerprint {
			return "", syncing.ErrBindingNotAccepted
		}
	}
	for _, sample := range batch.Observations.HostSamples {
		if sample.Fingerprint != bindingFingerprint {
			return "", syncing.ErrBindingNotAccepted
		}
	}
	for _, observation := range batch.Observations.ProbeObservations {
		if observation.Fingerprint != bindingFingerprint {
			return "", syncing.ErrBindingNotAccepted
		}
	}

	return bindingFingerprint, nil
}

func validateProbeObservations(ctx context.Context, tx syncBatchTx, writes []observations.ProbeObservationWrite) error {
	for _, observation := range writes {
		if err := observations.ValidateProbeObservation(observation); err != nil {
			return err
		}

		metadata, err := getProbeMetadata(ctx, tx, observation.ProbeItemID)
		if err != nil {
			if errors.Is(err, observations.ErrProbeMetadataNotFound) {
				return fmt.Errorf("%w: probe_item_id %q not found", observations.ErrInvalidProbeObservation, observation.ProbeItemID)
			}
			return fmt.Errorf("lookup probe metadata for %q: %w", observation.ProbeItemID, err)
		}
		if metadata.TargetID != observation.TargetID {
			return fmt.Errorf(
				"%w: probe_item_id %q belongs to target_id %q, got %q",
				observations.ErrInvalidProbeObservation,
				observation.ProbeItemID,
				metadata.TargetID,
				observation.TargetID,
			)
		}
		if metadata.ProbeKind != observation.ProbeKind {
			return fmt.Errorf(
				"%w: probe_item_id %q expects probe_kind %q, got %q",
				observations.ErrInvalidProbeObservation,
				observation.ProbeItemID,
				metadata.ProbeKind,
				observation.ProbeKind,
			)
		}
	}

	return nil
}

func recordHeartbeatBatch(ctx context.Context, tx syncBatchTx, nodeID, bindingFingerprint string, receivedAt time.Time, writes []syncing.HeartbeatPayload) (time.Time, error) {
	lastHeartbeatAt := writes[0].ObservedAt
	for _, write := range writes {
		if write.Fingerprint != bindingFingerprint {
			return time.Time{}, syncing.ErrBindingNotAccepted
		}
		if _, err := tx.Exec(ctx, `
			insert into node_heartbeats (
				node_id,
				observed_at,
				received_at,
				agent_version,
				fingerprint,
				sync_batch_id,
				is_backfilled
			) values (
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				$7
			)`,
			nodeID,
			write.ObservedAt,
			receivedAt,
			write.AgentVersion,
			write.Fingerprint,
			write.SyncBatchID,
			false,
		); err != nil {
			return time.Time{}, fmt.Errorf("record heartbeat for node %q: %w", nodeID, err)
		}

		if write.ObservedAt.After(lastHeartbeatAt) {
			lastHeartbeatAt = write.ObservedAt
		}
	}

	return lastHeartbeatAt, nil
}

func advanceNodeSyncState(ctx context.Context, tx syncBatchTx, nodeID string, lastHeartbeatAt, lastSyncAt time.Time) error {
	tag, err := tx.Exec(ctx, `
		update nodes
		set last_heartbeat_at = greatest(coalesce(last_heartbeat_at, $2), $2),
			last_sync_at = greatest(coalesce(last_sync_at, $3), $3),
			updated_at = now()
		where node_id = $1`,
		nodeID,
		lastHeartbeatAt,
		lastSyncAt,
	)
	if err != nil {
		return fmt.Errorf("touch sync batch state for node %q: %w", nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		return nodes.ErrNodeNotFound
	}

	return nil
}

func batchWithReceivedAt(batch observations.BatchWrite, receivedAt time.Time) observations.BatchWrite {
	out := observations.BatchWrite{
		NodeID:            batch.NodeID,
		HostSamples:       make([]observations.HostSampleWrite, 0, len(batch.HostSamples)),
		ProbeObservations: make([]observations.ProbeObservationWrite, 0, len(batch.ProbeObservations)),
	}

	for _, sample := range batch.HostSamples {
		sample.ReceivedAt = receivedAt
		out.HostSamples = append(out.HostSamples, sample)
	}
	for _, observation := range batch.ProbeObservations {
		observation.ReceivedAt = receivedAt
		out.ProbeObservations = append(out.ProbeObservations, observation)
	}

	return out
}

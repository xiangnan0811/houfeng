package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
)

type syncBatchTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresSyncRepository struct {
	beginTx              func(context.Context, pgx.TxOptions) (syncBatchTx, error)
	newIPQualityReportID func() (string, error)
	tokenHasher          agentTokenHasher
	now                  func() time.Time
	receptionFault       func(context.Context, error)
}

func NewPostgresSyncRepository(db *pgxpool.Pool) *PostgresSyncRepository {
	return NewPostgresSyncRepositoryWithTokenHMACKey(db, nil)
}

func NewPostgresSyncRepositoryWithTokenHMACKey(db *pgxpool.Pool, hmacKey []byte) *PostgresSyncRepository {
	return &PostgresSyncRepository{
		beginTx: func(ctx context.Context, options pgx.TxOptions) (syncBatchTx, error) {
			return db.BeginTx(ctx, options)
		},
		newIPQualityReportID: func() (string, error) {
			return ids.New("ipq")
		},
		tokenHasher: newAgentTokenHasher(hmacKey),
		now:         func() time.Time { return time.Now().UTC() },
		receptionFault: func(ctx context.Context, err error) {
			faultCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = MarkReceiverHealthFault(faultCtx, db, "sync_persistence_failure")
		},
	}
}

var _ syncing.Repository = (*PostgresSyncRepository)(nil)

func (r *PostgresSyncRepository) nowUTC() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
}

func (r *PostgresSyncRepository) ApplyBatch(ctx context.Context, batch syncing.Batch) (syncing.Result, error) {
	result, err := r.applyBatch(ctx, batch)
	if err != nil && r.receptionFault != nil && !errors.Is(err, syncing.ErrInvalidSyncToken) && !errors.Is(err, syncing.ErrBindingNotAccepted) && !errors.Is(err, syncing.ErrHeartbeatRequired) && !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) && !errors.Is(err, observations.ErrInvalidProbeObservation) {
		r.receptionFault(ctx, err)
	}
	return result, err
}

func (r *PostgresSyncRepository) applyBatch(ctx context.Context, batch syncing.Batch) (syncing.Result, error) {
	if len(batch.Heartbeats) == 0 && batch.LiveSignal == nil {
		if len(batch.Observations.HostSamples) == 0 && len(batch.Observations.ProbeObservations) == 0 {
			return syncing.Result{}, nil
		}

		return syncing.Result{}, syncing.ErrHeartbeatRequired
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return syncing.Result{}, fmt.Errorf("begin sync batch transaction for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		return syncing.Result{}, fmt.Errorf("lock asset graph for sync batch of monitoring instance %q: %w", batch.MonitoringInstanceID, err)
	}

	syncState, err := r.validateAcceptedSyncBatch(ctx, tx, batch)
	if err != nil {
		return syncing.Result{}, err
	}

	receivedAt := r.nowUTC()
	if err := recordTrustedLiveSignal(ctx, tx, batch, syncState, receivedAt); err != nil {
		return syncing.Result{}, err
	}
	if syncState.SuppressWritesAndPlan() {
		plan := agentplan.SyncPlan{ProbeAssignments: make([]agentplan.ProbeAssignment, 0)}
		if err := tx.Commit(ctx); err != nil {
			return syncing.Result{}, fmt.Errorf("commit suppressed sync batch transaction for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}
		return syncing.Result{
			StopCollection: true,
			Disposition:    syncing.ResultDispositionSuppressed,
			AcceptedAt:     receivedAt,
			Plan:           plan,
		}, nil
	}
	if len(batch.Heartbeats) == 0 {
		plan, err := buildSyncPlan(ctx, tx, batch.MonitoringInstanceID)
		if err != nil {
			return syncing.Result{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return syncing.Result{}, err
		}
		return syncing.Result{Disposition: syncing.ResultDispositionSuppressed, AcceptedAt: receivedAt, Plan: plan}, nil
	}

	observationBatch := batchWithReceivedAt(batch.Observations, receivedAt)
	if err := validateProbeObservations(ctx, tx, observationBatch.ProbeObservations); err != nil {
		return syncing.Result{}, err
	}
	recorded, err := recordAgentSyncBatch(ctx, tx, batch)
	if err != nil {
		return syncing.Result{}, err
	}
	if !recorded {
		plan := agentplan.SyncPlan{ProbeAssignments: make([]agentplan.ProbeAssignment, 0)}
		if err := tx.Commit(ctx); err != nil {
			return syncing.Result{}, fmt.Errorf("commit duplicate sync batch transaction for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}
		return syncing.Result{
			Disposition: syncing.ResultDispositionExactDuplicate,
			AcceptedAt:  receivedAt,
			Plan:        plan,
		}, nil
	}

	lastHeartbeatAt, err := recordHeartbeatBatch(ctx, tx, batch.MonitoringInstanceID, syncState.BindingFingerprint, receivedAt, batch.Heartbeats)
	if err != nil {
		return syncing.Result{}, err
	}
	if err := recordObservationBatch(ctx, tx, observationBatch); err != nil {
		return syncing.Result{}, err
	}
	if err := recordIPQualityReports(ctx, tx, r.newIPQualityReportID, batch.IPQualityReports, receivedAt); err != nil {
		return syncing.Result{}, err
	}
	nextLifecycleStatus := lifecycleStatusAfterAcceptedSync(syncState.LifecycleStatus, batch.LiveSignal != nil)
	if err := advanceMonitoringInstanceSyncState(ctx, tx, batch.MonitoringInstanceID, lastHeartbeatAt, receivedAt, nextLifecycleStatus); err != nil {
		return syncing.Result{}, err
	}

	// Store command results before dispatching a newly queued action so stale
	// results cannot overwrite the new in-flight last_action.
	if err := storeCommandResults(ctx, tx, batch, receivedAt); err != nil {
		return syncing.Result{}, err
	}

	pendingAction, err := dispatchPendingAction(ctx, tx, batch.MonitoringInstanceID, receivedAt)
	if err != nil {
		return syncing.Result{}, err
	}

	plan, err := buildSyncPlan(ctx, tx, batch.MonitoringInstanceID)
	if err != nil {
		return syncing.Result{}, err
	}
	plan.PendingAction = pendingAction

	if err := tx.Commit(ctx); err != nil {
		return syncing.Result{}, fmt.Errorf("commit sync batch transaction for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
	}

	return syncing.Result{
		Disposition: syncing.ResultDispositionRecorded,
		AcceptedAt:  receivedAt,
		Plan:        plan,
	}, nil
}

func recordAgentSyncBatch(ctx context.Context, tx syncBatchTx, batch syncing.Batch) (bool, error) {
	// Keep this targetless so the runtime role remains INSERT-only on agent_sync_batches.
	tag, err := tx.Exec(ctx, `
		insert into agent_sync_batches (
			monitoring_instance_id,
			sync_batch_id
		) values (
			$1,
			$2
		)
		on conflict do nothing`,
		batch.MonitoringInstanceID,
		batch.Heartbeats[0].SyncBatchID,
	)
	if err != nil {
		return false, fmt.Errorf("record agent sync batch %q for monitoring instance %q: %w", batch.Heartbeats[0].SyncBatchID, batch.MonitoringInstanceID, err)
	}
	return tag.RowsAffected() > 0, nil
}

func recordIPQualityReports(ctx context.Context, tx syncBatchTx, newReportID func() (string, error), reports []ipquality.ReportWrite, receivedAt time.Time) error {
	if len(reports) == 0 {
		return nil
	}
	if newReportID == nil {
		newReportID = func() (string, error) { return ids.New("ipq") }
	}
	for _, report := range reports {
		if err := ipquality.ValidateReportWrite(report); err != nil {
			return err
		}
		reportID, err := newReportID()
		if err != nil {
			return fmt.Errorf("generate ip quality report id: %w", err)
		}
		if report.ReceivedAt.IsZero() {
			report.ReceivedAt = receivedAt
		}
		if err := writeIPQualityReport(ctx, tx, reportID, report, json.RawMessage(`null`)); err != nil {
			return err
		}
	}
	return nil
}

type acceptedSyncBatchState struct {
	Capability         string
	VPSID              string
	BindingFingerprint string
	LifecycleStatus    string
	MonitoringStatus   string
	Archived           bool
	VPSArchivedAt      *time.Time
}

func (s acceptedSyncBatchState) SuppressWritesAndPlan() bool {
	return s.Capability == "evidence_only" || s.Archived ||
		s.LifecycleStatus == monitoringinstances.LifecycleRetired ||
		s.MonitoringStatus == monitoringinstances.MonitoringPaused
}

func (r *PostgresSyncRepository) validateAcceptedSyncBatch(ctx context.Context, tx syncBatchTx, batch syncing.Batch) (acceptedSyncBatchState, error) {
	var (
		bindingStatus       string
		bindingFingerprint  string
		storedSyncTokenHash string
		lifecycleStatus     string
		monitoringStatus    string
		archived            bool
		capability          string
		vpsID               string
		vpsArchivedAt       *time.Time
	)
	if err := tx.QueryRow(ctx, `
		select mi.binding_status, s.fingerprint_hash, s.token_hash,
			mi.lifecycle_status, mi.monitoring_status, v.lifecycle_status = 'archived', s.capability, mi.vps_id, v.archived_at
		from monitoring_instances mi
		join monitoring_agent_sessions s on s.monitoring_instance_id=mi.monitoring_instance_id
		join vps_assets v on v.vps_id=mi.vps_id
		where mi.monitoring_instance_id = $1 and s.session_id=$2
		for update of mi,s`,
		batch.MonitoringInstanceID, batch.SessionID,
	).Scan(&bindingStatus, &bindingFingerprint, &storedSyncTokenHash, &lifecycleStatus, &monitoringStatus, &archived, &capability, &vpsID, &vpsArchivedAt); errors.Is(err, pgx.ErrNoRows) {
		return acceptedSyncBatchState{}, syncing.ErrInvalidSyncToken
	} else if err != nil {
		return acceptedSyncBatchState{}, fmt.Errorf("query sync batch state for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
	}
	if !isHMACAgentTokenHash(storedSyncTokenHash) || !r.tokenHasher.syncTokenMatches(storedSyncTokenHash, batch.SyncToken) {
		return acceptedSyncBatchState{}, syncing.ErrInvalidSyncToken
	}
	// A binding transition cannot hide an already issued session's online
	// evidence. Its own fingerprint still authenticates it; collection remains
	// stopped until a newly accepted enrollment grants a new full session.
	if bindingStatus != monitoringinstances.BindingBound {
		capability = "evidence_only"
	}
	if batch.LiveSignal != nil && batch.LiveSignal.Fingerprint != bindingFingerprint {
		return acceptedSyncBatchState{}, syncing.ErrBindingNotAccepted
	}

	for _, heartbeat := range batch.Heartbeats {
		if heartbeat.Fingerprint != bindingFingerprint {
			return acceptedSyncBatchState{}, syncing.ErrBindingNotAccepted
		}
	}
	for _, sample := range batch.Observations.HostSamples {
		if sample.Fingerprint != bindingFingerprint {
			return acceptedSyncBatchState{}, syncing.ErrBindingNotAccepted
		}
	}
	for _, observation := range batch.Observations.ProbeObservations {
		if observation.Fingerprint != bindingFingerprint {
			return acceptedSyncBatchState{}, syncing.ErrBindingNotAccepted
		}
	}
	for _, report := range batch.IPQualityReports {
		if report.Fingerprint != bindingFingerprint {
			return acceptedSyncBatchState{}, syncing.ErrBindingNotAccepted
		}
	}

	return acceptedSyncBatchState{
		Capability:         capability,
		VPSID:              vpsID,
		VPSArchivedAt:      vpsArchivedAt,
		BindingFingerprint: bindingFingerprint,
		LifecycleStatus:    lifecycleStatus,
		MonitoringStatus:   monitoringStatus,
		Archived:           archived,
	}, nil
}

func lifecycleStatusAfterAcceptedSync(current string, hasHostSample bool) string {
	if hasHostSample && current == monitoringinstances.LifecyclePendingEnrollment {
		return monitoringinstances.LifecycleInUse
	}
	return current
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

func recordHeartbeatBatch(ctx context.Context, tx syncBatchTx, monitoringInstanceID, bindingFingerprint string, receivedAt time.Time, writes []syncing.HeartbeatPayload) (time.Time, error) {
	lastHeartbeatAt := writes[0].ObservedAt
	for _, write := range writes {
		if write.Fingerprint != bindingFingerprint {
			return time.Time{}, syncing.ErrBindingNotAccepted
		}
		if _, err := tx.Exec(ctx, `
			insert into monitoring_instance_heartbeats (
				monitoring_instance_id,
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
			monitoringInstanceID,
			write.ObservedAt,
			receivedAt,
			write.AgentVersion,
			write.Fingerprint,
			write.SyncBatchID,
			write.IsBackfilled,
		); err != nil {
			return time.Time{}, fmt.Errorf("record heartbeat for monitoring instance %q: %w", monitoringInstanceID, err)
		}

		if write.ObservedAt.After(lastHeartbeatAt) {
			lastHeartbeatAt = write.ObservedAt
		}
	}

	return lastHeartbeatAt, nil
}

func advanceMonitoringInstanceSyncState(ctx context.Context, tx syncBatchTx, monitoringInstanceID string, lastHeartbeatAt, lastSyncAt time.Time, lifecycleStatus string) error {
	tag, err := tx.Exec(ctx, `
		update monitoring_instances
		set last_heartbeat_at = greatest(coalesce(last_heartbeat_at, $2), $2),
			last_sync_at = greatest(coalesce(last_sync_at, $3), $3),
			lifecycle_status = $4,
			updated_at = now()
		where monitoring_instance_id = $1`,
		monitoringInstanceID,
		lastHeartbeatAt,
		lastSyncAt,
		lifecycleStatus,
	)
	if err != nil {
		return fmt.Errorf("touch sync batch state for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		return monitoringinstances.ErrMonitoringInstanceNotFound
	}

	return nil
}

func batchWithReceivedAt(batch observations.BatchWrite, receivedAt time.Time) observations.BatchWrite {
	out := observations.BatchWrite{
		MonitoringInstanceID: batch.MonitoringInstanceID,
		HostSamples:          make([]observations.HostSampleWrite, 0, len(batch.HostSamples)),
		ProbeObservations:    make([]observations.ProbeObservationWrite, 0, len(batch.ProbeObservations)),
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

// dispatchPendingAction reads the queued action, clears the queue columns,
// and leaves a durable in-flight last_action for result identity matching.
func dispatchPendingAction(ctx context.Context, tx syncBatchTx, monitoringInstanceID string, dispatchedAt time.Time) (*agentplan.PendingAction, error) {
	var actionID, commandID *string
	var lastActionRaw []byte
	if err := tx.QueryRow(ctx,
		`SELECT pending_action_id, pending_action_command_id, last_action FROM monitoring_instances WHERE monitoring_instance_id = $1 AND pending_action_id IS NOT NULL`,
		monitoringInstanceID).Scan(&actionID, &commandID, &lastActionRaw); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("query pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if actionID == nil || commandID == nil {
		return nil, nil
	}

	raw, err := marshalDispatchedPendingLastAction(*actionID, *commandID, lastActionRaw, dispatchedAt)
	if err != nil {
		return nil, fmt.Errorf("marshal pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	tag, err := tx.Exec(ctx,
		`UPDATE monitoring_instances
		SET pending_action_id = NULL,
			pending_action_command_id = NULL,
			last_action = $2,
			updated_at = now()
		WHERE monitoring_instance_id = $1
			AND pending_action_id = $3
			AND pending_action_command_id = $4`,
		monitoringInstanceID, raw, *actionID, *commandID)
	if err != nil {
		return nil, fmt.Errorf("clear pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}

	if err := insertCommandActionAudit(ctx, tx, commandActionAuditEvent{
		ActionID:             *actionID,
		MonitoringInstanceID: monitoringInstanceID,
		CommandID:            *commandID,
		Sensitivity:          sensitivityForKnownCommand(*commandID),
		EventType:            "dispatched",
		Source:               monitoringinstances.CommandActionSourceAgentSync,
		OccurredAt:           dispatchedAt,
	}); err != nil {
		return nil, fmt.Errorf("insert dispatched command action audit for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	return &agentplan.PendingAction{
		CommandID: *commandID,
		ActionID:  *actionID,
	}, nil
}

// storeCommandResults persists command execution results only when they match
// the action currently marked in-flight for the monitoring instance.
func storeCommandResults(ctx context.Context, tx syncBatchTx, batch syncing.Batch, completedAt time.Time) error {
	if len(batch.CommandResults) == 0 {
		return nil
	}

	for _, result := range batch.CommandResults {
		if result.ActionID == "" || result.CommandID == "" {
			continue
		}
		raw, err := marshalCompletedLastAction(result.ActionID, result.CommandID, result.Stdout, result.Stderr, result.ExitCode, completedAt)
		if err != nil {
			return fmt.Errorf("marshal command result for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}

		tag, err := tx.Exec(ctx,
			`UPDATE monitoring_instances
			SET last_action = $1,
				updated_at = now()
			WHERE monitoring_instance_id = $2
				AND last_action->>'status' = $3
				AND last_action->>'action_id' = $4
				AND last_action->>'command_id' = $5`,
			raw, batch.MonitoringInstanceID, commandActionStatusPending, result.ActionID, result.CommandID)
		if err != nil {
			return fmt.Errorf("store command result for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}

		exitCode := result.ExitCode
		if err := insertCommandActionAudit(ctx, tx, commandActionAuditEvent{
			ActionID:             result.ActionID,
			MonitoringInstanceID: batch.MonitoringInstanceID,
			CommandID:            result.CommandID,
			Sensitivity:          sensitivityForKnownCommand(result.CommandID),
			EventType:            "completed",
			Source:               monitoringinstances.CommandActionSourceAgentSync,
			ExitCode:             &exitCode,
			OccurredAt:           completedAt,
		}); err != nil {
			return fmt.Errorf("insert completed command action audit for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}
	}

	return nil
}

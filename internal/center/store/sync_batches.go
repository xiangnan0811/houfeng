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
}

func NewPostgresSyncRepository(db *pgxpool.Pool) *PostgresSyncRepository {
	return &PostgresSyncRepository{
		beginTx: func(ctx context.Context, options pgx.TxOptions) (syncBatchTx, error) {
			return db.BeginTx(ctx, options)
		},
		newIPQualityReportID: func() (string, error) {
			return ids.New("ipq")
		},
	}
}

var _ syncing.Repository = (*PostgresSyncRepository)(nil)

func (r *PostgresSyncRepository) ApplyBatch(ctx context.Context, batch syncing.Batch) (syncing.Result, error) {
	if len(batch.Heartbeats) == 0 {
		if len(batch.Observations.HostSamples) == 0 && len(batch.Observations.ProbeObservations) == 0 {
			return syncing.Result{}, nil
		}

		return syncing.Result{}, syncing.ErrHeartbeatRequired
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return syncing.Result{}, fmt.Errorf("begin sync batch transaction for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	syncState, err := validateAcceptedSyncBatch(ctx, tx, batch)
	if err != nil {
		return syncing.Result{}, err
	}

	receivedAt := time.Now().UTC()
	observationBatch := batchWithReceivedAt(batch.Observations, receivedAt)
	if err := validateProbeObservations(ctx, tx, observationBatch.ProbeObservations); err != nil {
		return syncing.Result{}, err
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
	nextLifecycleStatus := lifecycleStatusAfterAcceptedSync(syncState.LifecycleStatus, len(batch.Observations.HostSamples) > 0)
	if err := advanceMonitoringInstanceSyncState(ctx, tx, batch.MonitoringInstanceID, lastHeartbeatAt, receivedAt, nextLifecycleStatus); err != nil {
		return syncing.Result{}, err
	}

	// Store command results before dispatching a newly queued action so stale
	// results cannot overwrite the new in-flight last_action.
	if err := storeCommandResults(ctx, tx, batch); err != nil {
		return syncing.Result{}, err
	}

	pendingAction, err := dispatchPendingAction(ctx, tx, batch.MonitoringInstanceID)
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
		AcceptedAt: receivedAt,
		Plan:       plan,
	}, nil
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
		reportReceivedAt := report.ReceivedAt
		if reportReceivedAt.IsZero() {
			reportReceivedAt = receivedAt
		}
		rawJSON := []byte(nil)
		if len(report.RawJSON) > 0 {
			rawJSON = ipquality.SanitizeRawJSON(report.RawJSON)
		} else {
			rawJSON = json.RawMessage(`null`)
		}
		if _, err := tx.Exec(ctx, `
			insert into ip_quality_reports (
				report_id,
				monitoring_instance_id,
				observed_at,
				received_at,
				agent_version,
				fingerprint,
				sync_batch_id,
				ip_address,
				ip_version,
				status,
				asn,
				organization,
				latitude,
				longitude,
				use_region_code,
				use_region_name,
				registered_region_code,
				registered_region_name,
				risk_level,
				error_code,
				error_summary,
				is_backfilled,
				raw_json
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb)`,
			reportID,
			report.MonitoringInstanceID,
			report.ObservedAt,
			reportReceivedAt,
			report.AgentVersion,
			report.Fingerprint,
			report.SyncBatchID,
			report.IPAddress,
			report.IPVersion,
			report.Status,
			report.ASN,
			report.Organization,
			report.Latitude,
			report.Longitude,
			report.UseRegionCode,
			report.UseRegionName,
			report.RegisteredRegionCode,
			report.RegisteredRegionName,
			report.RiskLevel,
			report.ErrorCode,
			report.ErrorSummary,
			report.IsBackfilled,
			rawJSON,
		); err != nil {
			return fmt.Errorf("insert ip quality report for monitoring instance %q: %w", report.MonitoringInstanceID, err)
		}
		for _, provider := range report.ProviderResults {
			resultID, err := ids.New("ipqp")
			if err != nil {
				return fmt.Errorf("generate ip quality provider result id: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				insert into ip_quality_provider_results (
					result_id,
					report_id,
					provider,
					usage_type,
					company_type,
					risk_level,
					risk_score,
					region_code,
					region_name,
					is_proxy,
					is_tor,
					is_vpn,
					is_server,
					is_abuser,
					is_robot,
					error_code,
					error_summary
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
				resultID,
				reportID,
				provider.Provider,
				provider.UsageType,
				provider.CompanyType,
				provider.RiskLevel,
				provider.RiskScore,
				provider.RegionCode,
				provider.RegionName,
				provider.IsProxy,
				provider.IsTor,
				provider.IsVPN,
				provider.IsServer,
				provider.IsAbuser,
				provider.IsRobot,
				provider.ErrorCode,
				provider.ErrorSummary,
			); err != nil {
				return fmt.Errorf("insert ip quality provider result for report %q: %w", reportID, err)
			}
		}
		for _, unlock := range report.ServiceUnlocks {
			unlockID, err := ids.New("ipqu")
			if err != nil {
				return fmt.Errorf("generate ip quality service unlock id: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				insert into ip_quality_service_unlocks (
					unlock_id,
					report_id,
					service,
					status,
					region,
					unlock_type,
					error_code,
					error_summary
				) values ($1,$2,$3,$4,$5,$6,$7,$8)`,
				unlockID,
				reportID,
				unlock.Service,
				unlock.Status,
				unlock.Region,
				unlock.UnlockType,
				unlock.ErrorCode,
				unlock.ErrorSummary,
			); err != nil {
				return fmt.Errorf("insert ip quality service unlock for report %q: %w", reportID, err)
			}
		}
	}
	return nil
}

type acceptedSyncBatchState struct {
	BindingFingerprint string
	LifecycleStatus    string
}

func validateAcceptedSyncBatch(ctx context.Context, tx syncBatchTx, batch syncing.Batch) (acceptedSyncBatchState, error) {
	var (
		bindingStatus       string
		bindingFingerprint  string
		storedSyncTokenHash string
		lifecycleStatus     string
	)
	if err := tx.QueryRow(ctx, `
		select binding_status,
			coalesce(binding_fingerprint, ''),
			coalesce(sync_token_hash, ''),
			lifecycle_status
		from monitoring_instances
		where monitoring_instance_id = $1
		for update`,
		batch.MonitoringInstanceID,
	).Scan(&bindingStatus, &bindingFingerprint, &storedSyncTokenHash, &lifecycleStatus); errors.Is(err, pgx.ErrNoRows) {
		return acceptedSyncBatchState{}, monitoringinstances.ErrMonitoringInstanceNotFound
	} else if err != nil {
		return acceptedSyncBatchState{}, fmt.Errorf("query sync batch state for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
	}
	if bindingStatus != monitoringinstances.BindingBound {
		return acceptedSyncBatchState{}, syncing.ErrBindingNotAccepted
	}
	if storedSyncTokenHash == "" || storedSyncTokenHash != hashSyncToken(batch.SyncToken) {
		return acceptedSyncBatchState{}, syncing.ErrInvalidSyncToken
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
		BindingFingerprint: bindingFingerprint,
		LifecycleStatus:    lifecycleStatus,
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
func dispatchPendingAction(ctx context.Context, tx syncBatchTx, monitoringInstanceID string) (*agentplan.PendingAction, error) {
	var actionID, commandID *string
	if err := tx.QueryRow(ctx,
		`SELECT pending_action_id, pending_action_command_id FROM monitoring_instances WHERE monitoring_instance_id = $1 AND pending_action_id IS NOT NULL`,
		monitoringInstanceID).Scan(&actionID, &commandID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("query pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	if actionID == nil || commandID == nil {
		return nil, nil
	}

	raw, err := marshalPendingLastAction(*actionID, *commandID)
	if err != nil {
		return nil, fmt.Errorf("marshal pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE monitoring_instances
		SET pending_action_id = NULL,
			pending_action_command_id = NULL,
			last_action = $2,
			updated_at = now()
		WHERE monitoring_instance_id = $1
			AND pending_action_id = $3
			AND pending_action_command_id = $4`,
		monitoringInstanceID, raw, *actionID, *commandID); err != nil {
		return nil, fmt.Errorf("clear pending action for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	return &agentplan.PendingAction{
		CommandID: *commandID,
		ActionID:  *actionID,
	}, nil
}

// storeCommandResults persists command execution results only when they match
// the action currently marked in-flight for the monitoring instance.
func storeCommandResults(ctx context.Context, tx syncBatchTx, batch syncing.Batch) error {
	if len(batch.CommandResults) == 0 {
		return nil
	}

	for _, result := range batch.CommandResults {
		if result.ActionID == "" || result.CommandID == "" {
			continue
		}
		raw, err := marshalCompletedLastAction(result.ActionID, result.CommandID, result.Stdout, result.Stderr, result.ExitCode)
		if err != nil {
			return fmt.Errorf("marshal command result for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE monitoring_instances
			SET last_action = $1,
				updated_at = now()
			WHERE monitoring_instance_id = $2
				AND last_action->>'status' = $3
				AND last_action->>'action_id' = $4
				AND last_action->>'command_id' = $5`,
			raw, batch.MonitoringInstanceID, commandActionStatusPending, result.ActionID, result.CommandID); err != nil {
			return fmt.Errorf("store command result for monitoring instance %q: %w", batch.MonitoringInstanceID, err)
		}
	}

	return nil
}

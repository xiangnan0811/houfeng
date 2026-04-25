package incidents

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/syncing"
)

const defaultHeartbeatInterval = 30 * time.Second

type NodeRepository interface {
	GetNode(context.Context, string) (nodes.Record, error)
	ListNodes(context.Context) ([]nodes.Record, error)
}

type SnapshotReader interface {
	ListActiveIncidents(context.Context, ObjectType, string) ([]IncidentRecord, error)
	ListRecentHostSamples(context.Context, string, time.Time) ([]runtimefacts.HostSample, error)
	ListRecentProbeObservations(context.Context, string, time.Time) ([]runtimefacts.ProbeObservation, error)
}

type MutationWriter interface {
	ApplyIncidentMutation(context.Context, IncidentMutation) error
}

type Notifier interface {
	Send(context.Context, string) error
}

type Service struct {
	nodes             NodeRepository
	snapshots         SnapshotReader
	writer            MutationWriter
	notifier          Notifier
	logger            *slog.Logger
	now               func() time.Time
	heartbeatInterval time.Duration
	sweepInterval     time.Duration
}

func NewService(nodesRepo NodeRepository, snapshots SnapshotReader, writer MutationWriter, notifier Notifier, logger *slog.Logger, heartbeatInterval, sweepInterval time.Duration) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatInterval
	}
	if sweepInterval <= 0 {
		sweepInterval = time.Minute
	}
	return &Service{
		nodes:             nodesRepo,
		snapshots:         snapshots,
		writer:            writer,
		notifier:          notifier,
		logger:            logger,
		now:               func() time.Time { return time.Now().UTC() },
		heartbeatInterval: heartbeatInterval,
		sweepInterval:     sweepInterval,
	}
}

func (s *Service) AfterSuccessfulSync(ctx context.Context, batch syncing.Batch, result syncing.Result) {
	now := result.AcceptedAt
	if now.IsZero() {
		now = s.now()
	}
	if err := s.evaluateNode(ctx, batch.NodeID, now); err != nil {
		s.logger.Error("evaluate node incidents after sync failed", "node_id", batch.NodeID, "error", err)
	}
	for _, targetID := range uniqueTargetIDs(batch.Observations.ProbeObservations) {
		if err := s.evaluateTarget(ctx, targetID, now); err != nil {
			s.logger.Error("evaluate target incidents after sync failed", "target_id", targetID, "error", err)
		}
	}
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case tick := <-ticker.C:
			if err := s.EvaluateStaleNodes(ctx, tick.UTC()); err != nil {
				return err
			}
		}
	}
}

func (s *Service) EvaluateStaleNodes(ctx context.Context, now time.Time) error {
	records, err := s.nodes.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("list nodes for stale sweep: %w", err)
	}
	for _, record := range records {
		if err := s.evaluateNodeHeartbeatOnly(ctx, record, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) evaluateNodeHeartbeatOnly(ctx context.Context, record nodes.Record, now time.Time) error {
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeNode, record.NodeID)
	if err != nil {
		return fmt.Errorf("list previous node incidents for %q: %w", record.NodeID, err)
	}
	previousByClass := incidentsByClass(previous)
	result := EvaluateNodeHeartbeatMissing(previousByClass[IncidentNodeHeartbeatMissing], record.NodeID, now, record.LastHeartbeatAt, s.heartbeatInterval)
	mutation, err := s.buildMutation(ctx, ObjectTypeNode, record.NodeID, previous, []classEvaluation{{class: IncidentNodeHeartbeatMissing, result: result}})
	if err != nil {
		return err
	}
	return s.writer.ApplyIncidentMutation(ctx, mutation)
}

func (s *Service) evaluateNode(ctx context.Context, nodeID string, now time.Time) error {
	record, err := s.nodes.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node %q for incident evaluation: %w", nodeID, err)
	}
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeNode, nodeID)
	if err != nil {
		return fmt.Errorf("list previous node incidents for %q: %w", nodeID, err)
	}
	previousByClass := incidentsByClass(previous)
	evaluations := []classEvaluation{{
		class:  IncidentNodeHeartbeatMissing,
		result: EvaluateNodeHeartbeatMissing(previousByClass[IncidentNodeHeartbeatMissing], nodeID, now, record.LastHeartbeatAt, s.heartbeatInterval),
	}}

	hostSamples, err := s.snapshots.ListRecentHostSamples(ctx, nodeID, now.Add(-30*time.Minute))
	if err != nil {
		return fmt.Errorf("list recent host samples for %q: %w", nodeID, err)
	}
	if len(hostSamples) > 0 {
		latest := &hostSamples[0]
		resourceSamples := make([]NodeResourceSample, 0, len(hostSamples))
		for _, sample := range hostSamples {
			resourceSamples = append(resourceSamples, NodeResourceSample{
				ObservedAt:         sample.ObservedAt,
				CPUUsagePct:        sample.CPUUsagePct,
				NormalizedLoad5:    sample.Load5,
				MemUsedPct:         sample.MemUsedPct,
				MemAvailableBytes:  sample.MemAvailableBytes,
				SwapUsedPct:        sample.SwapUsedPct,
				CPUIOWaitPct:       sample.CPUIOWaitPct,
				CPUStealPct:        sample.CPUStealPct,
				MaintenanceContext: sample.MaintenanceContext,
				IsBackfilled:       sample.IsBackfilled,
			})
		}
		evaluations = append(evaluations,
			classEvaluation{class: IncidentNodeDiskPressure, result: EvaluateNodeDiskPressure(previousByClass[IncidentNodeDiskPressure], nodeID, latest)},
			classEvaluation{class: IncidentNodeInodePressure, result: EvaluateNodeInodePressure(previousByClass[IncidentNodeInodePressure], nodeID, latest)},
			classEvaluation{class: IncidentNodeResourcePressure, result: EvaluateNodeResourcePressure(previousByClass[IncidentNodeResourcePressure], nodeID, resourceSamples)},
		)
	}

	mutation, err := s.buildMutation(ctx, ObjectTypeNode, nodeID, previous, evaluations)
	if err != nil {
		return err
	}
	return s.writer.ApplyIncidentMutation(ctx, mutation)
}

func (s *Service) evaluateTarget(ctx context.Context, targetID string, now time.Time) error {
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeTarget, targetID)
	if err != nil {
		return fmt.Errorf("list previous target incidents for %q: %w", targetID, err)
	}
	previousByClass := incidentsByClass(previous)
	observations, err := s.snapshots.ListRecentProbeObservations(ctx, targetID, now.Add(-6*time.Hour))
	if err != nil {
		return fmt.Errorf("list recent probe observations for %q: %w", targetID, err)
	}

	evaluations := []classEvaluation{
		{class: IncidentTargetProbeFailure, result: EvaluateTargetProbeFailure(previousByClass[IncidentTargetProbeFailure], targetID, observations)},
		{class: IncidentTargetTLSExpiry, result: EvaluateTargetTLSExpiry(previousByClass[IncidentTargetTLSExpiry], targetID, observations)},
	}
	mutation, err := s.buildMutation(ctx, ObjectTypeTarget, targetID, previous, evaluations)
	if err != nil {
		return err
	}
	return s.writer.ApplyIncidentMutation(ctx, mutation)
}

type classEvaluation struct {
	class  IncidentClass
	result EvaluationResult
}

func (s *Service) buildMutation(ctx context.Context, objectType ObjectType, objectID string, previous []IncidentRecord, evaluations []classEvaluation) (IncidentMutation, error) {
	activeByClass := incidentsByClass(previous)
	mutation := IncidentMutation{
		ObjectType:    objectType,
		ObjectID:      objectID,
		Active:        make([]IncidentRecord, 0),
		Events:        make([]StateChangeEventRecord, 0),
		Notifications: make([]NotificationRecordWrite, 0),
	}

	for _, evaluation := range evaluations {
		switch evaluation.result.Transition {
		case TransitionRecovered:
			delete(activeByClass, evaluation.class)
		case TransitionSkipped:
			if previousIncident, ok := activeByClass[evaluation.class]; ok {
				delete(activeByClass, evaluation.class)
				mutation.Events = append(mutation.Events, StateChangeEventRecord{
					IncidentID:    previousIncident.IncidentID,
					IncidentClass: previousIncident.IncidentClass,
					ObjectType:    objectType,
					ObjectID:      objectID,
					EventType:     EventIncidentRecovered,
					Severity:      previousIncident.Severity,
					Summary:       "维护或补传 observation 使该异常退出活跃集合",
					CreatedAt:     s.now(),
				})
			}
		default:
			if evaluation.result.Current != nil {
				activeByClass[evaluation.class] = evaluation.result.Current
			}
		}
		if evaluation.result.Event != nil {
			mutation.Events = append(mutation.Events, *evaluation.result.Event)
		}
		if evaluation.result.Notification != nil {
			record, err := s.dispatchNotification(ctx, objectType, objectID, evaluation)
			if err != nil {
				return IncidentMutation{}, err
			}
			mutation.Notifications = append(mutation.Notifications, record)
		}
	}

	for _, incident := range activeByClass {
		mutation.Active = append(mutation.Active, *incident)
	}
	sort.Slice(mutation.Active, func(i, j int) bool {
		return mutation.Active[i].IncidentClass < mutation.Active[j].IncidentClass
	})
	return mutation, nil
}

func (s *Service) dispatchNotification(ctx context.Context, objectType ObjectType, objectID string, evaluation classEvaluation) (NotificationRecordWrite, error) {
	decision := evaluation.result.Notification
	incidentID := ""
	if evaluation.result.Current != nil {
		incidentID = evaluation.result.Current.IncidentID
	}
	if incidentID == "" && evaluation.result.Event != nil {
		incidentID = evaluation.result.Event.IncidentID
	}
	status := DeliveryStatusSuppressed
	var sentAt *time.Time
	if decision.ShouldSend && s.notifier != nil {
		if err := s.notifier.Send(ctx, decision.Summary); err != nil {
			s.logger.Error("send incident notification failed", "object_type", objectType, "object_id", objectID, "error", err)
		} else {
			status = DeliveryStatusSent
			now := s.now()
			sentAt = &now
		}
	}
	return NotificationRecordWrite{
		IncidentID:     incidentID,
		ObjectType:     objectType,
		ObjectID:       objectID,
		Channel:        decision.Channel,
		DeliveryStatus: status,
		Summary:        decision.Summary,
		SentAt:         sentAt,
	}, nil
}

func incidentsByClass(records []IncidentRecord) map[IncidentClass]*IncidentRecord {
	out := make(map[IncidentClass]*IncidentRecord, len(records))
	for i := range records {
		out[records[i].IncidentClass] = &records[i]
	}
	return out
}

func uniqueTargetIDs(items []observations.ProbeObservationWrite) []string {
	seen := map[string]struct{}{}
	targetIDs := make([]string, 0)
	for _, item := range items {
		if item.TargetID == "" {
			continue
		}
		if _, ok := seen[item.TargetID]; ok {
			continue
		}
		seen[item.TargetID] = struct{}{}
		targetIDs = append(targetIDs, item.TargetID)
	}
	sort.Strings(targetIDs)
	return targetIDs
}

type PostgresSnapshotReader struct {
	db *pgxpool.Pool
}

func NewPostgresSnapshotReader(db *pgxpool.Pool) *PostgresSnapshotReader {
	return &PostgresSnapshotReader{db: db}
}

func (r *PostgresSnapshotReader) ListActiveIncidents(ctx context.Context, objectType ObjectType, objectID string) ([]IncidentRecord, error) {
	rows, err := r.db.Query(ctx, `
		select incident_id, object_type, object_id, incident_class, severity, started_at, last_evaluated_at, status, source_summary
		from active_incidents
		where object_type = $1 and object_id = $2
		order by incident_class`, string(objectType), objectID)
	if err != nil {
		return nil, fmt.Errorf("query active incidents for %s %q: %w", objectType, objectID, err)
	}
	defer rows.Close()

	records := make([]IncidentRecord, 0)
	for rows.Next() {
		var record IncidentRecord
		var objectTypeValue, incidentClass, severity string
		if err := rows.Scan(&record.IncidentID, &objectTypeValue, &record.ObjectID, &incidentClass, &severity, &record.StartedAt, &record.LastEvaluatedAt, &record.Status, &record.SourceSummary); err != nil {
			return nil, fmt.Errorf("scan active incident: %w", err)
		}
		record.ObjectType = ObjectType(objectTypeValue)
		record.IncidentClass = IncidentClass(incidentClass)
		record.Severity = Severity(severity)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active incidents: %w", err)
	}
	return records, nil
}

func (r *PostgresSnapshotReader) ListRecentHostSamples(ctx context.Context, nodeID string, since time.Time) ([]runtimefacts.HostSample, error) {
	rows, err := r.db.Query(ctx, `
		select
			node_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec,
			net_out_bytes_per_sec, cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec,
			disk_write_bytes_per_sec, disk_busy_pct, uptime_seconds,
			maintenance_context, is_backfilled, sync_batch_id
		from host_samples
		where node_id = $1 and observed_at >= $2
		order by observed_at desc, id desc`, nodeID, since)
	if err != nil {
		return nil, fmt.Errorf("query host samples for %q: %w", nodeID, err)
	}
	defer rows.Close()
	out := make([]runtimefacts.HostSample, 0)
	for rows.Next() {
		var sample runtimefacts.HostSample
		if err := rows.Scan(
			&sample.NodeID, &sample.ObservedAt, &sample.ReceivedAt, &sample.AgentVersion, &sample.Fingerprint,
			&sample.CPUUsagePct, &sample.Load1, &sample.Load5, &sample.Load15, &sample.MemUsedPct, &sample.MemAvailableBytes,
			&sample.SwapUsedPct, &sample.DiskUsedPct, &sample.InodeUsedPct, &sample.NetInBytesPerSec,
			&sample.NetOutBytesPerSec, &sample.CPUIOWaitPct, &sample.CPUStealPct, &sample.DiskReadBytesPerSec,
			&sample.DiskWriteBytesPerSec, &sample.DiskBusyPct, &sample.UptimeSeconds,
			&sample.MaintenanceContext, &sample.IsBackfilled, &sample.SyncBatchID,
		); err != nil {
			return nil, fmt.Errorf("scan host sample: %w", err)
		}
		out = append(out, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate host samples: %w", err)
	}
	return out, nil
}

func (r *PostgresSnapshotReader) ListRecentProbeObservations(ctx context.Context, targetID string, since time.Time) ([]runtimefacts.ProbeObservation, error) {
	rows, err := r.db.Query(ctx, `
		select
			po.node_id, po.target_id, po.probe_item_id, pi.probe_kind,
			po.observed_at, po.received_at, po.agent_version, po.fingerprint,
			po.result_kind, po.latency_ms, po.http_status, po.tls_expiry_days,
			coalesce(po.error_code, ''), coalesce(po.error_summary, ''),
			po.maintenance_context, po.is_backfilled, po.sync_batch_id
		from probe_observations po
		join probe_items pi on pi.probe_item_id = po.probe_item_id
		where po.target_id = $1 and po.observed_at >= $2
		order by po.observed_at desc, po.id desc`, targetID, since)
	if err != nil {
		return nil, fmt.Errorf("query probe observations for %q: %w", targetID, err)
	}
	defer rows.Close()
	out := make([]runtimefacts.ProbeObservation, 0)
	for rows.Next() {
		var observation runtimefacts.ProbeObservation
		if err := rows.Scan(
			&observation.NodeID, &observation.TargetID, &observation.ProbeItemID, &observation.ProbeKind,
			&observation.ObservedAt, &observation.ReceivedAt, &observation.AgentVersion, &observation.Fingerprint,
			&observation.ResultKind, &observation.LatencyMS, &observation.HTTPStatus, &observation.TLSExpiryDays,
			&observation.ErrorCode, &observation.ErrorSummary,
			&observation.MaintenanceContext, &observation.IsBackfilled, &observation.SyncBatchID,
		); err != nil {
			return nil, fmt.Errorf("scan probe observation: %w", err)
		}
		out = append(out, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate probe observations: %w", err)
	}
	return out, nil
}

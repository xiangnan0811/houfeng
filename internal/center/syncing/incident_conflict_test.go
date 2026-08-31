package syncing_test

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/agentplan"
	incidentservice "houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/syncing"
)

func TestServiceCommittedSyncProjectionConflictsPreserveResponse(t *testing.T) {
	acceptedAt := time.Date(2026, time.August, 30, 14, 0, 0, 0, time.UTC)
	want := syncing.Result{
		Disposition: syncing.ResultDispositionRecorded,
		AcceptedAt:  acceptedAt,
		Plan: agentplan.SyncPlan{
			HostSampleFrequencyTier:      "1m",
			HostSampleMaintenanceContext: true,
			PendingAction: &agentplan.PendingAction{
				ActionID:  "act_committed",
				CommandID: "cmd_committed",
			},
		},
	}
	writer := &conflictingIncidentWriter{}
	incidentProcessor := incidentservice.NewService(
		fixedMonitoringInstanceRepository{record: monitoringinstances.Record{
			MonitoringInstanceID: "mi_committed",
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			LifecycleStatus:      monitoringinstances.LifecycleInUse,
			LastHeartbeatAt:      &acceptedAt,
		}},
		nil,
		&fixedIncidentSnapshotReader{},
		writer,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Minute,
		time.Minute,
	)
	service := syncing.NewService(fixedSyncRepository{result: want}, incidentProcessor)

	got, err := service.SyncBatch(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_committed"})
	if err != nil {
		t.Fatalf("SyncBatch() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncBatch() result = %#v, want committed repository result %#v", got, want)
	}
	if writer.applyCalls != 2 || writer.notificationCalls != 0 {
		t.Fatalf("incident writer apply=%d notifications=%d, want one retry then safe yield", writer.applyCalls, writer.notificationCalls)
	}
}

type fixedSyncRepository struct {
	result syncing.Result
}

func (r fixedSyncRepository) ApplyBatch(context.Context, syncing.Batch) (syncing.Result, error) {
	return r.result, nil
}

type fixedMonitoringInstanceRepository struct {
	record monitoringinstances.Record
}

func (r fixedMonitoringInstanceRepository) GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error) {
	return r.record, nil
}

func (r fixedMonitoringInstanceRepository) ListMonitoringInstances(context.Context, ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error) {
	return []monitoringinstances.Record{r.record}, nil
}

type fixedIncidentSnapshotReader struct {
	rowVersionReads int
}

func (r *fixedIncidentSnapshotReader) GetObjectRowVersion(context.Context, incidentservice.ObjectType, string) (string, error) {
	r.rowVersionReads++
	if r.rowVersionReads == 1 {
		return "opaque-version-one", nil
	}
	return "opaque-version-two", nil
}

func (*fixedIncidentSnapshotReader) ListActiveIncidents(context.Context, incidentservice.ObjectType, string) ([]incidentservice.IncidentRecord, error) {
	return nil, nil
}

func (*fixedIncidentSnapshotReader) ListRecentLiveHeartbeatReceipts(context.Context, string, time.Time) ([]incidentservice.LiveHeartbeatReceipt, error) {
	return nil, nil
}

func (*fixedIncidentSnapshotReader) ListRecentHostSamples(context.Context, string, time.Time) ([]runtimefacts.HostSample, error) {
	return nil, nil
}

func (*fixedIncidentSnapshotReader) ListRecentProbeObservations(context.Context, string, time.Time) ([]runtimefacts.ProbeObservation, error) {
	return nil, nil
}

func (*fixedIncidentSnapshotReader) ListMonitoringInstanceHostDailyAggregates(context.Context, string, time.Time, time.Time) ([]incidentservice.MonitoringInstanceHostDailyAggregate, error) {
	return nil, nil
}

func (*fixedIncidentSnapshotReader) ListTargetProbeDailyAggregates(context.Context, string, time.Time, time.Time) ([]incidentservice.TargetProbeDailyAggregate, error) {
	return nil, nil
}

type conflictingIncidentWriter struct {
	applyCalls        int
	notificationCalls int
}

func (w *conflictingIncidentWriter) ApplyIncidentMutation(context.Context, incidentservice.IncidentMutation) error {
	w.applyCalls++
	return incidentservice.ErrIncidentProjectionConflict
}

func (w *conflictingIncidentWriter) AppendNotificationRecords(context.Context, []incidentservice.NotificationRecordWrite) error {
	w.notificationCalls++
	return nil
}

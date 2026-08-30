package incidents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

type fakeMonitoringInstanceRepo struct {
	getMonitoringInstanceResult   monitoringinstances.Record
	getMonitoringInstanceResults  map[string][]monitoringinstances.Record
	getMonitoringInstanceErrors   map[string][]error
	listMonitoringInstancesResult []monitoringinstances.Record
	trace                         *[]string
}

func (f *fakeMonitoringInstanceRepo) GetMonitoringInstance(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	appendIncidentTestTrace(f.trace, "get:monitoring_instance:"+monitoringInstanceID)
	if errs := f.getMonitoringInstanceErrors[monitoringInstanceID]; len(errs) > 0 {
		err := errs[0]
		f.getMonitoringInstanceErrors[monitoringInstanceID] = errs[1:]
		return monitoringinstances.Record{}, err
	}
	if records := f.getMonitoringInstanceResults[monitoringInstanceID]; len(records) > 0 {
		record := records[0]
		f.getMonitoringInstanceResults[monitoringInstanceID] = records[1:]
		return record, nil
	}
	if f.getMonitoringInstanceResult.MonitoringInstanceID != "" {
		return f.getMonitoringInstanceResult, nil
	}
	for _, record := range f.listMonitoringInstancesResult {
		if record.MonitoringInstanceID == monitoringInstanceID {
			return record, nil
		}
	}
	return f.getMonitoringInstanceResult, nil
}
func (f *fakeMonitoringInstanceRepo) ListMonitoringInstances(context.Context, ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error) {
	return f.listMonitoringInstancesResult, nil
}

type fakeTargetRepo struct {
	listTargetsResult  []targets.TargetRecord
	getTargetResults   map[string]targets.TargetRecord
	getTargetSequences map[string][]targets.TargetRecord
	getTargetErrors    map[string][]error
	trace              *[]string
}

func (f *fakeTargetRepo) ListTargets(context.Context) ([]targets.TargetRecord, error) {
	return f.listTargetsResult, nil
}

func (f *fakeTargetRepo) GetTarget(_ context.Context, targetID string) (targets.TargetRecord, error) {
	appendIncidentTestTrace(f.trace, "get:target:"+targetID)
	if errs := f.getTargetErrors[targetID]; len(errs) > 0 {
		err := errs[0]
		f.getTargetErrors[targetID] = errs[1:]
		return targets.TargetRecord{}, err
	}
	if records := f.getTargetSequences[targetID]; len(records) > 0 {
		record := records[0]
		f.getTargetSequences[targetID] = records[1:]
		return record, nil
	}
	if f.getTargetResults == nil {
		for _, record := range f.listTargetsResult {
			if record.TargetID == targetID {
				return record, nil
			}
		}
		return targets.TargetRecord{TargetID: targetID, RunStatus: targets.RunStatusEnabled}, nil
	}
	record, ok := f.getTargetResults[targetID]
	if !ok {
		return targets.TargetRecord{TargetID: targetID, RunStatus: targets.RunStatusEnabled}, nil
	}
	return record, nil
}

var _ TargetRepository = (*fakeTargetRepo)(nil)

type fakeSnapshotReader struct {
	activeByObject               map[string][]IncidentRecord
	activeByObjectSequences      map[string][][]IncidentRecord
	hostSamples                  map[string][]runtimefacts.HostSample
	hostSampleSequences          map[string][][]runtimefacts.HostSample
	probeObs                     map[string][]runtimefacts.ProbeObservation
	probeObservationSequences    map[string][][]runtimefacts.ProbeObservation
	monitoringInstanceAggregates map[string][]MonitoringInstanceHostDailyAggregate
	targetAggregates             map[string][]TargetProbeDailyAggregate
	rowVersionSequences          map[string][]string
	rowVersionErrors             map[string][]error
	trace                        *[]string
}

func (f *fakeSnapshotReader) GetObjectRowVersion(
	ctx context.Context,
	objectType ObjectType,
	objectID string,
) (string, error) {
	_ = ctx
	key := string(objectType) + ":" + objectID
	appendIncidentTestTrace(f.trace, "version:"+key)
	if errs := f.rowVersionErrors[key]; len(errs) > 0 {
		err := errs[0]
		f.rowVersionErrors[key] = errs[1:]
		return "", err
	}
	if versions := f.rowVersionSequences[key]; len(versions) > 0 {
		version := versions[0]
		f.rowVersionSequences[key] = versions[1:]
		return version, nil
	}
	return "test-row-version", nil
}

func (f *fakeSnapshotReader) ListActiveIncidents(_ context.Context, objectType ObjectType, objectID string) ([]IncidentRecord, error) {
	key := string(objectType) + ":" + objectID
	appendIncidentTestTrace(f.trace, "active:"+key)
	if sequences := f.activeByObjectSequences[key]; len(sequences) > 0 {
		records := sequences[0]
		f.activeByObjectSequences[key] = sequences[1:]
		return append([]IncidentRecord(nil), records...), nil
	}
	return append([]IncidentRecord(nil), f.activeByObject[key]...), nil
}
func (f *fakeSnapshotReader) ListRecentHostSamples(_ context.Context, monitoringInstanceID string, _ time.Time) ([]runtimefacts.HostSample, error) {
	appendIncidentTestTrace(f.trace, "host:"+monitoringInstanceID)
	if sequences := f.hostSampleSequences[monitoringInstanceID]; len(sequences) > 0 {
		samples := sequences[0]
		f.hostSampleSequences[monitoringInstanceID] = sequences[1:]
		return append([]runtimefacts.HostSample(nil), samples...), nil
	}
	return append([]runtimefacts.HostSample(nil), f.hostSamples[monitoringInstanceID]...), nil
}
func (f *fakeSnapshotReader) ListRecentProbeObservations(_ context.Context, targetID string, _ time.Time) ([]runtimefacts.ProbeObservation, error) {
	appendIncidentTestTrace(f.trace, "probe:"+targetID)
	if sequences := f.probeObservationSequences[targetID]; len(sequences) > 0 {
		observations := sequences[0]
		f.probeObservationSequences[targetID] = sequences[1:]
		return append([]runtimefacts.ProbeObservation(nil), observations...), nil
	}
	return append([]runtimefacts.ProbeObservation(nil), f.probeObs[targetID]...), nil
}
func (f *fakeSnapshotReader) ListMonitoringInstanceHostDailyAggregates(_ context.Context, monitoringInstanceID string, _, _ time.Time) ([]MonitoringInstanceHostDailyAggregate, error) {
	appendIncidentTestTrace(f.trace, "host-aggregate:"+monitoringInstanceID)
	return append([]MonitoringInstanceHostDailyAggregate(nil), f.monitoringInstanceAggregates[monitoringInstanceID]...), nil
}
func (f *fakeSnapshotReader) ListTargetProbeDailyAggregates(_ context.Context, targetID string, _, _ time.Time) ([]TargetProbeDailyAggregate, error) {
	appendIncidentTestTrace(f.trace, "probe-aggregate:"+targetID)
	return append([]TargetProbeDailyAggregate(nil), f.targetAggregates[targetID]...), nil
}

type fakeMutationWriter struct {
	mutations     []IncidentMutation
	notifications [][]NotificationRecordWrite
	applyErrors   []error
	appendErrors  []error
	trace         *[]string
}

func (f *fakeMutationWriter) ApplyIncidentMutation(_ context.Context, mutation IncidentMutation) error {
	appendIncidentTestTrace(f.trace, "apply:"+string(mutation.ObjectType)+":"+mutation.ObjectID)
	f.mutations = append(f.mutations, mutation)
	if len(f.applyErrors) > 0 {
		err := f.applyErrors[0]
		f.applyErrors = f.applyErrors[1:]
		return err
	}
	return nil
}

func (f *fakeMutationWriter) AppendNotificationRecords(_ context.Context, records []NotificationRecordWrite) error {
	f.notifications = append(f.notifications, records)
	if len(f.appendErrors) > 0 {
		err := f.appendErrors[0]
		f.appendErrors = f.appendErrors[1:]
		return err
	}
	return nil
}

func appendIncidentTestTrace(trace *[]string, entry string) {
	if trace != nil {
		*trace = append(*trace, entry)
	}
}

type fakeIncidentSnapshotDB struct {
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeIncidentSnapshotDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (f fakeIncidentSnapshotDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		panic("unexpected QueryRow call")
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeIncidentSnapshotRow struct {
	scan func(...any) error
}

func (r fakeIncidentSnapshotRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeNotifier struct {
	messages []string
	err      error
}

func (f *fakeNotifier) Send(_ context.Context, summary string) error {
	f.messages = append(f.messages, summary)
	return f.err
}

type fakeNotificationDispatcher struct {
	deliveries []NotificationDelivery
	channels   []NotificationChannel
	summaries  []string
}

func (f *fakeNotificationDispatcher) Dispatch(_ context.Context, summary string) []NotificationDelivery {
	f.summaries = append(f.summaries, summary)
	return append([]NotificationDelivery(nil), f.deliveries...)
}

func (f *fakeNotificationDispatcher) NotificationChannels(context.Context) []NotificationChannel {
	return append([]NotificationChannel(nil), f.channels...)
}

type fakeSettingsRepository struct {
	getSettingsResult            centersettings.CenterSettings
	getSettingsErr               error
	persistedTelegram            centersettings.TelegramSettings
	persistedExists              bool
	persistedErr                 error
	persistedIncidentDefaults    centersettings.IncidentDefaults
	persistedIncidentExists      bool
	persistedIncidentDefaultsErr error
}

type tracedSettingsRepository struct {
	settings centersettings.CenterSettings
	trace    *[]string
}

func (r *tracedSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	appendIncidentTestTrace(r.trace, "settings")
	return r.settings, nil
}

func TestIncidentSnapshotSeriesSQLUsesReplaySafeLatestOrdering(t *testing.T) {
	t.Parallel()

	if !strings.Contains(incidentRecentHostSamplesSQL, "order by observed_at desc, is_backfilled asc, received_at desc, id desc") {
		t.Fatalf("incidentRecentHostSamplesSQL = %q, want replay-safe host series ordering", incidentRecentHostSamplesSQL)
	}
	if !strings.Contains(incidentRecentProbeObservationsSQL, "order by po.observed_at desc, po.is_backfilled asc, po.received_at desc, po.id desc") {
		t.Fatalf("incidentRecentProbeObservationsSQL = %q, want replay-safe probe series ordering", incidentRecentProbeObservationsSQL)
	}
}

func TestServiceMonitoringInstanceProjectionConflictRereadsAndRetriesOnce(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	firstHeartbeat := now.Add(-5 * time.Minute)
	trace := make([]string, 0)
	monitoringInstanceID := "mi_retry_secret"
	repo := &fakeMonitoringInstanceRepo{
		getMonitoringInstanceResults: map[string][]monitoringinstances.Record{
			monitoringInstanceID: {
				{MonitoringInstanceID: monitoringInstanceID, MonitoringStatus: monitoringinstances.MonitoringEnabled, LifecycleStatus: monitoringinstances.LifecycleInUse, LastHeartbeatAt: &firstHeartbeat},
				{MonitoringInstanceID: monitoringInstanceID, MonitoringStatus: monitoringinstances.MonitoringEnabled, LifecycleStatus: monitoringinstances.LifecycleInUse, LastHeartbeatAt: &now},
			},
		},
		trace: &trace,
	}
	healthy := []runtimefacts.HostSample{{ObservedAt: now, DiskUsedPct: 10}}
	critical := []runtimefacts.HostSample{{ObservedAt: now.Add(time.Second), DiskUsedPct: 99}}
	snapshots := &fakeSnapshotReader{
		rowVersionSequences: map[string][]string{
			"monitoring_instance:" + monitoringInstanceID: {"xmin-secret-v1", "xmin-secret-v2"},
		},
		hostSampleSequences: map[string][][]runtimefacts.HostSample{
			monitoringInstanceID: {healthy, healthy, critical, critical},
		},
		activeByObjectSequences: map[string][][]IncidentRecord{
			"monitoring_instance:" + monitoringInstanceID: {
				{activeIncident(ObjectTypeMonitoringInstance, monitoringInstanceID, IncidentMonitoringInstanceDiskPressure, now.Add(-time.Hour))},
				nil,
			},
		},
		trace: &trace,
	}
	writer := &fakeMutationWriter{
		applyErrors: []error{fmt.Errorf("conflict contains xmin-secret-v1: %w", ErrIncidentProjectionConflict), nil},
		trace:       &trace,
	}
	notifier := &fakeNotifier{}
	settings := centersettings.Default()
	settingsRepo := &tracedSettingsRepository{settings: settings, trace: &trace}
	service := NewSettingsBackedService(repo, &fakeTargetRepo{}, snapshots, writer, notifier, settingsRepo, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.evaluateMonitoringInstance(context.Background(), monitoringInstanceID, now); err != nil {
		t.Fatalf("evaluateMonitoringInstance() error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("mutations = %#v, want conflict attempt plus one retry", writer.mutations)
	}
	if writer.mutations[0].ExpectedObjectRowVersion != "xmin-secret-v1" || writer.mutations[1].ExpectedObjectRowVersion != "xmin-secret-v2" {
		t.Fatalf("mutation row versions = %q, %q, want v1 then v2", writer.mutations[0].ExpectedObjectRowVersion, writer.mutations[1].ExpectedObjectRowVersion)
	}
	if !mutationsContainIncident(writer.mutations[1:], IncidentMonitoringInstanceDiskPressure) {
		t.Fatalf("retry mutation = %#v, want second-attempt critical disk fact to win", writer.mutations[1])
	}
	if !mutationsContainIncident(writer.mutations[:1], IncidentMonitoringInstanceHeartbeatMissing) || mutationsContainIncident(writer.mutations[1:], IncidentMonitoringInstanceHeartbeatMissing) {
		t.Fatalf("mutations = %#v, want second attempt to use fresh heartbeat/object and active set", writer.mutations)
	}
	if len(notifier.messages) != 1 || len(writer.notifications) != 1 {
		t.Fatalf("notifications = %#v messages = %#v, want exactly one after successful retry", writer.notifications, notifier.messages)
	}
	wantAttemptTrace := []string{
		"version:monitoring_instance:" + monitoringInstanceID,
		"get:monitoring_instance:" + monitoringInstanceID,
		"active:monitoring_instance:" + monitoringInstanceID,
		"settings",
		"host:" + monitoringInstanceID,
		"settings",
		"host:" + monitoringInstanceID,
		"host-aggregate:" + monitoringInstanceID,
		"apply:monitoring_instance:" + monitoringInstanceID,
	}
	wantTrace := append(append([]string{}, wantAttemptTrace...), wantAttemptTrace...)
	wantTrace = append(wantTrace, "settings")
	if strings.Join(trace, "|") != strings.Join(wantTrace, "|") {
		t.Fatalf("trace = %#v, want two token-first full attempts %#v", trace, wantTrace)
	}
}

func TestServiceTargetProjectionConflictRereadsAndRetriesOnce(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 30, 0, 0, time.UTC)
	trace := make([]string, 0)
	targetID := "tg_retry_secret"
	targetRepo := &fakeTargetRepo{
		getTargetSequences: map[string][]targets.TargetRecord{
			targetID: {
				{TargetID: targetID, Name: "first-object", RunStatus: targets.RunStatusEnabled},
				{TargetID: targetID, Name: "second-object", RunStatus: targets.RunStatusEnabled},
			},
		},
		trace: &trace,
	}
	probeSuccess := []runtimefacts.ProbeObservation{
		{ObservedAt: now, TargetID: targetID, ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_retry", MonitoringInstanceID: "mi_retry", ResultKind: agentapi.ProbeResultSuccess},
	}
	probeFailure := []runtimefacts.ProbeObservation{
		{ObservedAt: now.Add(3 * time.Second), TargetID: targetID, ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_retry", MonitoringInstanceID: "mi_retry", ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(2 * time.Second), TargetID: targetID, ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_retry", MonitoringInstanceID: "mi_retry", ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(time.Second), TargetID: targetID, ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_retry", MonitoringInstanceID: "mi_retry", ResultKind: agentapi.ProbeResultFailure},
	}
	snapshots := &fakeSnapshotReader{
		rowVersionSequences: map[string][]string{
			"target:" + targetID: {"target-xmin-secret-v1", "target-xmin-secret-v2"},
		},
		probeObservationSequences: map[string][][]runtimefacts.ProbeObservation{
			targetID: {probeSuccess, probeSuccess, probeFailure, probeFailure},
		},
		activeByObjectSequences: map[string][][]IncidentRecord{
			"target:" + targetID: {
				{activeIncident(ObjectTypeTarget, targetID, IncidentTargetProbeFailure, now.Add(-time.Hour))},
				nil,
			},
		},
		trace: &trace,
	}
	writer := &fakeMutationWriter{
		applyErrors: []error{fmt.Errorf("target conflict secret: %w", ErrIncidentProjectionConflict), nil},
		trace:       &trace,
	}
	notifier := &fakeNotifier{}
	service := NewService(&fakeMonitoringInstanceRepo{}, targetRepo, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.evaluateTarget(context.Background(), targetID, now); err != nil {
		t.Fatalf("evaluateTarget() error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("mutations = %#v, want conflict attempt plus one retry", writer.mutations)
	}
	if writer.mutations[0].ExpectedObjectRowVersion != "target-xmin-secret-v1" || writer.mutations[1].ExpectedObjectRowVersion != "target-xmin-secret-v2" {
		t.Fatalf("mutation row versions = %q, %q, want v1 then v2", writer.mutations[0].ExpectedObjectRowVersion, writer.mutations[1].ExpectedObjectRowVersion)
	}
	if !mutationsContainIncident(writer.mutations[1:], IncidentTargetProbeFailure) {
		t.Fatalf("retry mutation = %#v, want second-attempt probe failure to win", writer.mutations[1])
	}
	if len(writer.mutations[0].Active) != 1 || len(writer.mutations[1].Active) != 1 || writer.mutations[0].Active[0].SourceSummary == writer.mutations[1].Active[0].SourceSummary {
		t.Fatalf("mutations = %#v, want retry to replace first active snapshot with second-attempt facts", writer.mutations)
	}
	if len(notifier.messages) != 1 || len(writer.notifications) != 1 {
		t.Fatalf("notifications = %#v messages = %#v, want exactly one after successful retry", writer.notifications, notifier.messages)
	}
	wantAttemptTrace := []string{
		"version:target:" + targetID,
		"get:target:" + targetID,
		"active:target:" + targetID,
		"probe:" + targetID,
		"probe:" + targetID,
		"probe-aggregate:" + targetID,
		"apply:target:" + targetID,
	}
	wantTrace := append(append([]string{}, wantAttemptTrace...), wantAttemptTrace...)
	if strings.Join(trace, "|") != strings.Join(wantTrace, "|") {
		t.Fatalf("trace = %#v, want two token-first full attempts %#v", trace, wantTrace)
	}
}

func TestServiceProjectionConflictRetryUsesMonotonicFreshEvaluationTime(t *testing.T) {
	acceptedAt := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	retryNow := acceptedAt.Add(10 * time.Minute)
	lastHeartbeatAt := acceptedAt.Add(-time.Minute)
	monitoringInstanceID := "mi_monotonic_retry"
	competing := activeIncident(
		ObjectTypeMonitoringInstance,
		monitoringInstanceID,
		IncidentMonitoringInstanceHeartbeatMissing,
		acceptedAt.Add(-time.Hour),
	)
	competing.Severity = SeverityCritical
	competing.LastEvaluatedAt = retryNow
	competing.SourceSummary = "competitor B evaluated at retry time"
	repo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{
		MonitoringInstanceID: monitoringInstanceID,
		MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		LifecycleStatus:      monitoringinstances.LifecycleInUse,
		LastHeartbeatAt:      &lastHeartbeatAt,
	}}
	snapshots := &fakeSnapshotReader{
		rowVersionSequences: map[string][]string{
			"monitoring_instance:" + monitoringInstanceID: {"version-before-b", "version-after-b"},
		},
		activeByObjectSequences: map[string][][]IncidentRecord{
			"monitoring_instance:" + monitoringInstanceID: {nil, {competing}},
		},
	}
	writer := &fakeMutationWriter{applyErrors: []error{ErrIncidentProjectionConflict, nil}}
	notifier := &fakeNotifier{}
	service := NewService(repo, &fakeTargetRepo{}, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return retryNow }

	if err := service.AfterSuccessfulSync(
		context.Background(),
		syncing.Batch{MonitoringInstanceID: monitoringInstanceID},
		syncing.Result{AcceptedAt: acceptedAt},
	); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("mutations = %#v, want first conflict and one retry", writer.mutations)
	}
	retryMutation := writer.mutations[1]
	if retryMutation.ExpectedObjectRowVersion != "version-after-b" {
		t.Fatalf("retry row version = %q, want fresh competitor version", retryMutation.ExpectedObjectRowVersion)
	}
	if len(retryMutation.Active) != 1 || retryMutation.Active[0].IncidentClass != IncidentMonitoringInstanceHeartbeatMissing {
		t.Fatalf("retry Active = %#v, want competitor heartbeat incident preserved", retryMutation.Active)
	}
	if !retryMutation.Active[0].LastEvaluatedAt.Equal(retryNow) {
		t.Fatalf("retry LastEvaluatedAt = %v, want monotonic %v", retryMutation.Active[0].LastEvaluatedAt, retryNow)
	}
	if len(retryMutation.Events) != 0 {
		t.Fatalf("retry Events = %#v, want no stale recovery/escalation event", retryMutation.Events)
	}
	if len(writer.notifications) != 0 || len(notifier.messages) != 0 {
		t.Fatalf("notifications = %#v messages = %#v, want none for competitor-preserving retry", writer.notifications, notifier.messages)
	}
}

func TestServiceSecondProjectionConflictYieldsWithoutNotificationAndWarnsSafely(t *testing.T) {
	evaluationNow := time.Date(2026, time.August, 30, 11, 0, 0, 0, time.UTC)
	clockNow := evaluationNow
	monitoringInstanceID := "mi_do_not_log_7f31"
	repo := &fakeMonitoringInstanceRepo{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: monitoringInstanceID,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			LifecycleStatus:      monitoringinstances.LifecycleInUse,
			LastHeartbeatAt:      &evaluationNow,
		},
	}
	snapshots := &fakeSnapshotReader{
		rowVersionSequences: map[string][]string{
			"monitoring_instance:" + monitoringInstanceID: {
				"xmin-do-not-log-a", "xmin-do-not-log-b",
				"xmin-do-not-log-c", "xmin-do-not-log-d",
				"xmin-do-not-log-e", "xmin-do-not-log-f",
			},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			monitoringInstanceID: {{ObservedAt: evaluationNow, DiskUsedPct: 99}},
		},
	}
	writer := &fakeMutationWriter{applyErrors: []error{
		fmt.Errorf("conflict-secret-a: %w", ErrIncidentProjectionConflict),
		fmt.Errorf("conflict-secret-b: %w", ErrIncidentProjectionConflict),
		fmt.Errorf("conflict-secret-c: %w", ErrIncidentProjectionConflict),
		fmt.Errorf("conflict-secret-d: %w", ErrIncidentProjectionConflict),
		fmt.Errorf("conflict-secret-e: %w", ErrIncidentProjectionConflict),
		fmt.Errorf("conflict-secret-f: %w", ErrIncidentProjectionConflict),
	}}
	notifier := &fakeNotifier{}
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	service := NewService(repo, &fakeTargetRepo{}, snapshots, writer, notifier, logger, time.Minute, time.Minute)
	service.now = func() time.Time { return clockNow }

	for call := 0; call < 2; call++ {
		if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: monitoringInstanceID}, syncing.Result{AcceptedAt: evaluationNow}); err != nil {
			t.Fatalf("AfterSuccessfulSync() call %d error = %v", call+1, err)
		}
	}
	if len(writer.mutations) != 4 {
		t.Fatalf("mutations = %#v, want two attempts per post-sync evaluation", writer.mutations)
	}
	if len(writer.notifications) != 0 || len(notifier.messages) != 0 {
		t.Fatalf("notifications = %#v messages = %#v, want none after exhausted conflicts", writer.notifications, notifier.messages)
	}
	const warning = "incident projection conflict retry exhausted"
	if got := strings.Count(logOutput.String(), warning); got != 1 {
		t.Fatalf("warning count = %d, want one within 60s; logs=%q", got, logOutput.String())
	}
	if !strings.Contains(logOutput.String(), "level=ERROR") || strings.Contains(logOutput.String(), "level=WARN") {
		t.Fatalf("logs = %q, want repository-standard ERROR level for exhausted retry", logOutput.String())
	}
	if !strings.Contains(logOutput.String(), `error="incident projection conflict"`) {
		t.Fatalf("logs = %q, want stable projection conflict error field", logOutput.String())
	}
	for _, secret := range []string{monitoringInstanceID, "xmin-do-not-log", "conflict-secret"} {
		if strings.Contains(logOutput.String(), secret) {
			t.Fatalf("logs contain secret %q: %q", secret, logOutput.String())
		}
	}
	if !strings.Contains(logOutput.String(), "object_type=monitoring_instance") || !strings.Contains(logOutput.String(), "classification=concurrent_update") {
		t.Fatalf("logs = %q, want only fixed conflict classification and object type", logOutput.String())
	}

	clockNow = clockNow.Add(61 * time.Second)
	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: monitoringInstanceID}, syncing.Result{AcceptedAt: evaluationNow}); err != nil {
		t.Fatalf("AfterSuccessfulSync() after throttle interval error = %v", err)
	}
	if got := strings.Count(logOutput.String(), warning); got != 2 {
		t.Fatalf("warning count after interval = %d, want 2; logs=%q", got, logOutput.String())
	}
}

func TestServiceProjectionConflictWarningLimiterIsThreadSafe(t *testing.T) {
	var logOutput bytes.Buffer
	service := NewService(nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(&logOutput, nil)), time.Minute, time.Minute)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 30, 11, 15, 0, 0, time.UTC)
	}

	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			service.warnProjectionConflictRetryExhausted(ObjectTypeMonitoringInstance)
		}()
	}
	wait.Wait()

	if got := strings.Count(logOutput.String(), "incident projection conflict retry exhausted"); got != 1 {
		t.Fatalf("warning count = %d, want one under concurrent callers; logs=%q", got, logOutput.String())
	}
}

func TestServicePeriodicSweepProjectionConflictYieldsAndContinues(t *testing.T) {
	now := time.Date(2026, time.August, 30, 11, 30, 0, 0, time.UTC)
	targetRepo := &fakeTargetRepo{
		listTargetsResult: []targets.TargetRecord{
			{TargetID: "tg_conflict", RunStatus: targets.RunStatusEnabled},
			{TargetID: "tg_next", RunStatus: targets.RunStatusEnabled},
		},
	}
	snapshots := &fakeSnapshotReader{rowVersionSequences: map[string][]string{
		"target:tg_conflict": {"conflict-v1", "conflict-v2"},
		"target:tg_next":     {"next-v1"},
	}}
	writer := &fakeMutationWriter{applyErrors: []error{
		ErrIncidentProjectionConflict,
		fmt.Errorf("wrapped second conflict: %w", ErrIncidentProjectionConflict),
		nil,
	}}
	service := NewService(
		&fakeMonitoringInstanceRepo{},
		targetRepo,
		snapshots,
		writer,
		nil,
		slog.Default(),
		time.Minute,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.EvaluatePeriodicState(context.Background(), now); err != nil {
		t.Fatalf("EvaluatePeriodicState() error = %v, want exhausted conflict to yield", err)
	}
	if len(writer.mutations) != 3 {
		t.Fatalf("mutations = %#v, want two conflict attempts then next target", writer.mutations)
	}
	if got := writer.mutations[2]; got.ObjectID != "tg_next" || got.ExpectedObjectRowVersion != "next-v1" {
		t.Fatalf("next target mutation = %#v, want sweep continuation with fresh token", got)
	}
}

func TestServiceInactiveObjectsUseTokenFirstAdministrativeRecoveryCAS(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	trace := make([]string, 0)
	monitoringRepo := &fakeMonitoringInstanceRepo{
		listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_inactive", MonitoringStatus: monitoringinstances.MonitoringEnabled}},
		getMonitoringInstanceResults: map[string][]monitoringinstances.Record{
			"mi_inactive": {{MonitoringInstanceID: "mi_inactive", MonitoringStatus: monitoringinstances.MonitoringPaused, LifecycleStatus: monitoringinstances.LifecycleInUse}},
		},
		trace: &trace,
	}
	targetRepo := &fakeTargetRepo{
		listTargetsResult: []targets.TargetRecord{{TargetID: "tg_inactive", RunStatus: targets.RunStatusEnabled}},
		getTargetSequences: map[string][]targets.TargetRecord{
			"tg_inactive": {{TargetID: "tg_inactive", RunStatus: targets.RunStatusPaused}},
		},
		trace: &trace,
	}
	snapshots := &fakeSnapshotReader{
		rowVersionSequences: map[string][]string{
			"monitoring_instance:mi_inactive": {"mi-admin-version"},
			"target:tg_inactive":              {"target-admin-version"},
		},
		activeByObject: map[string][]IncidentRecord{
			"monitoring_instance:mi_inactive": {activeIncident(ObjectTypeMonitoringInstance, "mi_inactive", IncidentMonitoringInstanceHeartbeatMissing, now.Add(-time.Hour))},
			"target:tg_inactive":              {activeIncident(ObjectTypeTarget, "tg_inactive", IncidentTargetProbeFailure, now.Add(-time.Hour))},
		},
		trace: &trace,
	}
	writer := &fakeMutationWriter{trace: &trace}
	service := NewService(monitoringRepo, targetRepo, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.EvaluatePeriodicState(context.Background(), now); err != nil {
		t.Fatalf("EvaluatePeriodicState() error = %v", err)
	}
	if len(writer.mutations) != 2 {
		t.Fatalf("mutations = %#v, want MI and target administrative recovery", writer.mutations)
	}
	for i, expected := range []struct {
		objectType ObjectType
		objectID   string
		version    string
	}{
		{ObjectTypeMonitoringInstance, "mi_inactive", "mi-admin-version"},
		{ObjectTypeTarget, "tg_inactive", "target-admin-version"},
	} {
		mutation := writer.mutations[i]
		if mutation.ObjectType != expected.objectType || mutation.ObjectID != expected.objectID || mutation.ExpectedObjectRowVersion != expected.version {
			t.Fatalf("mutation[%d] = %#v, want token-guarded %s recovery", i, mutation, expected.objectType)
		}
		if len(mutation.Active) != 0 || len(mutation.Events) != 1 || mutation.Events[0].EventType != EventIncidentRecovered {
			t.Fatalf("mutation[%d] = %#v, want administrative recovery", i, mutation)
		}
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want administrative recovery to remain silent", writer.notifications)
	}
	for _, unexpected := range []string{"host:mi_inactive", "probe:tg_inactive"} {
		if slicesContainIncidentTestTrace(trace, unexpected) {
			t.Fatalf("trace = %#v, inactive recovery unexpectedly read raw facts", trace)
		}
	}
	assertIncidentTestTraceBefore(t, trace, "version:monitoring_instance:mi_inactive", "get:monitoring_instance:mi_inactive", "active:monitoring_instance:mi_inactive")
	assertIncidentTestTraceBefore(t, trace, "version:target:tg_inactive", "get:target:tg_inactive", "active:target:tg_inactive")
}

func TestServiceInactiveObjectsWithoutIncidentsDoNotChurnSummaryOrRowVersion(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 15, 0, 0, time.UTC)
	monitoringRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{
		MonitoringInstanceID: "mi_inactive_empty",
		MonitoringStatus:     monitoringinstances.MonitoringPaused,
		LifecycleStatus:      monitoringinstances.LifecycleInUse,
	}}}
	targetRepo := &fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{
		TargetID:  "tg_inactive_empty",
		RunStatus: targets.RunStatusArchived,
	}}}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringRepo, targetRepo, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	for sweep := 0; sweep < 2; sweep++ {
		if err := service.EvaluatePeriodicState(context.Background(), now.Add(time.Duration(sweep)*time.Minute)); err != nil {
			t.Fatalf("EvaluatePeriodicState() sweep %d error = %v", sweep+1, err)
		}
	}
	if len(writer.mutations) != 0 {
		t.Fatalf("mutations = %#v, want no empty administrative writes across repeated sweeps", writer.mutations)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want none for empty administrative recovery", writer.notifications)
	}
}

func TestServiceEnumerationRaceObjectDeletionYieldsSafely(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 20, 0, 0, time.UTC)
	tests := []struct {
		name       string
		newService func(*fakeMutationWriter) *Service
		evaluate   func(*Service) error
	}{
		{
			name: "monitoring instance deleted before row version read",
			newService: func(writer *fakeMutationWriter) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_deleted_version"}}},
					&fakeTargetRepo{},
					&fakeSnapshotReader{rowVersionErrors: map[string][]error{"monitoring_instance:mi_deleted_version": {ErrIncidentProjectionObjectNotFound}}},
					writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name: "monitoring instance deleted before fresh get",
			newService: func(writer *fakeMutationWriter) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{
						listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_deleted_get"}},
						getMonitoringInstanceErrors:   map[string][]error{"mi_deleted_get": {monitoringinstances.ErrMonitoringInstanceNotFound}},
					},
					&fakeTargetRepo{}, &fakeSnapshotReader{}, writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name: "target deleted before row version read",
			newService: func(writer *fakeMutationWriter) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{TargetID: "tg_deleted_version"}}},
					&fakeSnapshotReader{rowVersionErrors: map[string][]error{"target:tg_deleted_version": {ErrIncidentProjectionObjectNotFound}}},
					writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
		{
			name: "target deleted before fresh get",
			newService: func(writer *fakeMutationWriter) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{
						listTargetsResult: []targets.TargetRecord{{TargetID: "tg_deleted_get"}},
						getTargetErrors:   map[string][]error{"tg_deleted_get": {targets.ErrTargetNotFound}},
					},
					&fakeSnapshotReader{}, writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeMutationWriter{}
			service := tt.newService(writer)
			if err := tt.evaluate(service); err != nil {
				t.Fatalf("periodic evaluation error = %v, want deleted enumeration member to yield", err)
			}
			if len(writer.mutations) != 0 || len(writer.notifications) != 0 {
				t.Fatalf("mutations = %#v notifications = %#v, want no side effects for deleted object", writer.mutations, writer.notifications)
			}
		})
	}
}

func TestServiceEnumerationRaceNonNotFoundErrorsPropagateWithoutRetry(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 25, 0, 0, time.UTC)
	monitoringInstanceRowVersionErr := errors.New("monitoring instance row-version unavailable")
	monitoringInstanceUnclassifiedNoRowsErr := fmt.Errorf("monitoring instance row-version query returned no rows without object classification: %w", pgx.ErrNoRows)
	monitoringInstanceGetErr := errors.New("monitoring instance read unavailable")
	targetRowVersionErr := errors.New("target row-version unavailable")
	targetUnclassifiedNoRowsErr := fmt.Errorf("target row-version query returned no rows without object classification: %w", pgx.ErrNoRows)
	targetGetErr := errors.New("target read unavailable")
	tests := []struct {
		name         string
		wantErr      error
		attemptTrace string
		newService   func(*fakeMutationWriter, *[]string) *Service
		evaluate     func(*Service) error
	}{
		{
			name:         "monitoring instance row-version ordinary error",
			wantErr:      monitoringInstanceRowVersionErr,
			attemptTrace: "version:monitoring_instance:mi_version_error",
			newService: func(writer *fakeMutationWriter, trace *[]string) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_version_error"}}},
					&fakeTargetRepo{},
					&fakeSnapshotReader{
						rowVersionErrors: map[string][]error{"monitoring_instance:mi_version_error": {monitoringInstanceRowVersionErr}},
						trace:            trace,
					},
					writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name:         "monitoring instance row-version unclassified no rows",
			wantErr:      monitoringInstanceUnclassifiedNoRowsErr,
			attemptTrace: "version:monitoring_instance:mi_unclassified_no_rows",
			newService: func(writer *fakeMutationWriter, trace *[]string) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_unclassified_no_rows"}}},
					&fakeTargetRepo{},
					&fakeSnapshotReader{
						rowVersionErrors: map[string][]error{"monitoring_instance:mi_unclassified_no_rows": {monitoringInstanceUnclassifiedNoRowsErr}},
						trace:            trace,
					},
					writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name:         "monitoring instance Get ordinary error",
			wantErr:      monitoringInstanceGetErr,
			attemptTrace: "get:monitoring_instance:mi_get_error",
			newService: func(writer *fakeMutationWriter, trace *[]string) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{
						listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_get_error"}},
						getMonitoringInstanceErrors:   map[string][]error{"mi_get_error": {monitoringInstanceGetErr}},
						trace:                         trace,
					},
					&fakeTargetRepo{}, &fakeSnapshotReader{trace: trace}, writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name:         "target row-version ordinary error",
			wantErr:      targetRowVersionErr,
			attemptTrace: "version:target:tg_version_error",
			newService: func(writer *fakeMutationWriter, trace *[]string) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{TargetID: "tg_version_error"}}},
					&fakeSnapshotReader{
						rowVersionErrors: map[string][]error{"target:tg_version_error": {targetRowVersionErr}},
						trace:            trace,
					},
					writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
		{
			name:         "target row-version unclassified no rows",
			wantErr:      targetUnclassifiedNoRowsErr,
			attemptTrace: "version:target:tg_unclassified_no_rows",
			newService: func(writer *fakeMutationWriter, trace *[]string) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{TargetID: "tg_unclassified_no_rows"}}},
					&fakeSnapshotReader{
						rowVersionErrors: map[string][]error{"target:tg_unclassified_no_rows": {targetUnclassifiedNoRowsErr}},
						trace:            trace,
					},
					writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
		{
			name:         "target Get ordinary error",
			wantErr:      targetGetErr,
			attemptTrace: "get:target:tg_get_error",
			newService: func(writer *fakeMutationWriter, trace *[]string) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{
						listTargetsResult: []targets.TargetRecord{{TargetID: "tg_get_error"}},
						getTargetErrors:   map[string][]error{"tg_get_error": {targetGetErr}},
						trace:             trace,
					},
					&fakeSnapshotReader{trace: trace}, writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0)
			writer := &fakeMutationWriter{}
			service := tt.newService(writer, &trace)
			err := tt.evaluate(service)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("periodic evaluation error = %v, want ordinary error %v to propagate", err, tt.wantErr)
			}
			if len(writer.mutations) != 0 || len(writer.notifications) != 0 {
				t.Fatalf("mutations = %#v notifications = %#v, want no side effects for ordinary read error", writer.mutations, writer.notifications)
			}
			attempts := 0
			for _, entry := range trace {
				if entry == tt.attemptTrace {
					attempts++
				}
			}
			if attempts != 1 {
				t.Fatalf("trace = %#v, want exactly one %q attempt", trace, tt.attemptTrace)
			}
		})
	}
}

func TestServiceWriterGuardMissingObjectYieldsSafely(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 27, 0, 0, time.UTC)
	missingObject := fmt.Errorf("writer guard object disappeared: %w", ErrIncidentProjectionObjectNotFound)
	tests := []struct {
		name       string
		newService func(*fakeMutationWriter, *fakeNotifier) *Service
		evaluate   func(*Service) error
		objectType ObjectType
		objectID   string
	}{
		{
			name: "monitoring instance deleted after evaluation before writer guard",
			newService: func(writer *fakeMutationWriter, notifier *fakeNotifier) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_deleted_at_guard"}}},
					&fakeTargetRepo{}, &fakeSnapshotReader{}, writer, notifier, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
			objectType: ObjectTypeMonitoringInstance,
			objectID:   "mi_deleted_at_guard",
		},
		{
			name: "target deleted after evaluation before writer guard",
			newService: func(writer *fakeMutationWriter, notifier *fakeNotifier) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{TargetID: "tg_deleted_at_guard"}}},
					&fakeSnapshotReader{}, writer, notifier, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
			objectType: ObjectTypeTarget,
			objectID:   "tg_deleted_at_guard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeMutationWriter{applyErrors: []error{missingObject}}
			notifier := &fakeNotifier{}
			service := tt.newService(writer, notifier)

			if err := tt.evaluate(service); err != nil {
				t.Fatalf("periodic evaluation error = %v, want writer-guard deletion to yield", err)
			}
			if len(writer.mutations) != 1 {
				t.Fatalf("mutation attempts = %#v, want one completed evaluation and no retry", writer.mutations)
			}
			if writer.mutations[0].ObjectType != tt.objectType || writer.mutations[0].ObjectID != tt.objectID {
				t.Fatalf("mutation attempt = %#v, want %s %q", writer.mutations[0], tt.objectType, tt.objectID)
			}
			if len(writer.notifications) != 0 || len(notifier.messages) != 0 {
				t.Fatalf("notifications = %#v dispatched = %#v, want no notification side effects", writer.notifications, notifier.messages)
			}
		})
	}
}

func TestServiceWriterGuardMissingObjectSweepContinuesWithNextObject(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 28, 0, 0, time.UTC)
	tests := []struct {
		name       string
		missingID  string
		nextID     string
		newService func(*fakeMutationWriter) *Service
		evaluate   func(*Service) error
	}{
		{
			name:      "monitoring instance sweep",
			missingID: "mi_deleted_at_guard",
			nextID:    "mi_next",
			newService: func(writer *fakeMutationWriter) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_deleted_at_guard"}, {MonitoringInstanceID: "mi_next"}}},
					&fakeTargetRepo{}, &fakeSnapshotReader{}, writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name:      "target sweep",
			missingID: "tg_deleted_at_guard",
			nextID:    "tg_next",
			newService: func(writer *fakeMutationWriter) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{TargetID: "tg_deleted_at_guard"}, {TargetID: "tg_next"}}},
					&fakeSnapshotReader{}, writer, nil, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeMutationWriter{applyErrors: []error{
				fmt.Errorf("first object disappeared: %w", ErrIncidentProjectionObjectNotFound),
				nil,
			}}
			service := tt.newService(writer)

			if err := tt.evaluate(service); err != nil {
				t.Fatalf("periodic evaluation error = %v, want deleted object to yield and sweep to continue", err)
			}
			if len(writer.mutations) != 2 {
				t.Fatalf("mutation attempts = %d, want deleted object followed by next object", len(writer.mutations))
			}
			if writer.mutations[0].ObjectID != tt.missingID || writer.mutations[1].ObjectID != tt.nextID {
				t.Fatalf("mutation order mismatch, want deleted object followed by next object")
			}
			if len(writer.notifications) != 0 {
				t.Fatalf("notification batches = %d, want none", len(writer.notifications))
			}
		})
	}
}

func TestServiceWriterGuardOrdinaryErrorsPropagateWithoutRetry(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 29, 0, 0, time.UTC)
	monitoringInstanceDBErr := errors.New("monitoring instance guard database unavailable")
	targetNoRowsErr := fmt.Errorf("non-guard writer query returned no rows: %w", pgx.ErrNoRows)
	tests := []struct {
		name       string
		writerErr  error
		newService func(*fakeMutationWriter, *fakeNotifier) *Service
		evaluate   func(*Service) error
	}{
		{
			name:      "monitoring instance ordinary database error",
			writerErr: monitoringInstanceDBErr,
			newService: func(writer *fakeMutationWriter, notifier *fakeNotifier) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_guard_error"}}},
					&fakeTargetRepo{}, &fakeSnapshotReader{}, writer, notifier, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluateStaleMonitoringInstances(context.Background(), now)
			},
		},
		{
			name:      "target arbitrary pgx no rows remains an error",
			writerErr: targetNoRowsErr,
			newService: func(writer *fakeMutationWriter, notifier *fakeNotifier) *Service {
				return NewService(
					&fakeMonitoringInstanceRepo{},
					&fakeTargetRepo{listTargetsResult: []targets.TargetRecord{{TargetID: "tg_guard_error"}}},
					&fakeSnapshotReader{}, writer, notifier, slog.Default(), time.Minute, time.Minute,
				)
			},
			evaluate: func(service *Service) error {
				return service.EvaluatePeriodicState(context.Background(), now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeMutationWriter{applyErrors: []error{tt.writerErr}}
			notifier := &fakeNotifier{}
			service := tt.newService(writer, notifier)

			err := tt.evaluate(service)
			if !errors.Is(err, tt.writerErr) {
				t.Fatalf("periodic evaluation error = %v, want ordinary writer error %v", err, tt.writerErr)
			}
			if len(writer.mutations) != 1 {
				t.Fatalf("mutation attempts = %#v, want exactly one and no retry", writer.mutations)
			}
			if len(writer.notifications) != 0 || len(notifier.messages) != 0 {
				t.Fatalf("notifications = %#v dispatched = %#v, want none after failed Apply", writer.notifications, notifier.messages)
			}
		})
	}
}

func TestServiceHeartbeatOnlySweepUsesFreshObjectAndRowVersionCAS(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 30, 0, 0, time.UTC)
	stale := now.Add(-5 * time.Minute)
	trace := make([]string, 0)
	repo := &fakeMonitoringInstanceRepo{
		listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_heartbeat", LastHeartbeatAt: &now}},
		getMonitoringInstanceResults: map[string][]monitoringinstances.Record{
			"mi_heartbeat": {{MonitoringInstanceID: "mi_heartbeat", MonitoringStatus: monitoringinstances.MonitoringEnabled, LifecycleStatus: monitoringinstances.LifecycleInUse, LastHeartbeatAt: &stale}},
		},
		trace: &trace,
	}
	snapshots := &fakeSnapshotReader{
		rowVersionSequences: map[string][]string{"monitoring_instance:mi_heartbeat": {"heartbeat-version"}},
		trace:               &trace,
	}
	writer := &fakeMutationWriter{trace: &trace}
	service := NewService(repo, &fakeTargetRepo{}, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 1 || writer.mutations[0].ExpectedObjectRowVersion != "heartbeat-version" {
		t.Fatalf("mutations = %#v, want heartbeat-only CAS token", writer.mutations)
	}
	if !mutationsContainIncident(writer.mutations, IncidentMonitoringInstanceHeartbeatMissing) {
		t.Fatalf("mutation = %#v, want fresh stale heartbeat record to drive incident", writer.mutations[0])
	}
	assertIncidentTestTraceBefore(t, trace, "version:monitoring_instance:mi_heartbeat", "get:monitoring_instance:mi_heartbeat", "active:monitoring_instance:mi_heartbeat")
}

func TestServiceNotificationAppendFailureDoesNotRetryEvaluation(t *testing.T) {
	now := time.Date(2026, time.August, 30, 13, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{
		MonitoringInstanceID: "mi_notify",
		MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		LifecycleStatus:      monitoringinstances.LifecycleInUse,
		LastHeartbeatAt:      &now,
	}}
	snapshots := &fakeSnapshotReader{hostSamples: map[string][]runtimefacts.HostSample{
		"mi_notify": {{ObservedAt: now, DiskUsedPct: 99}},
	}}
	appendErr := errors.New("append notification failed")
	writer := &fakeMutationWriter{appendErrors: []error{appendErr}}
	notifier := &fakeNotifier{}
	service := NewService(repo, &fakeTargetRepo{}, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	err := service.evaluateMonitoringInstance(context.Background(), "mi_notify", now)
	if !errors.Is(err, appendErr) {
		t.Fatalf("evaluateMonitoringInstance() error = %v, want append error", err)
	}
	if len(writer.mutations) != 1 || len(writer.notifications) != 1 || len(notifier.messages) != 1 {
		t.Fatalf("mutations=%d notifications=%d sends=%d, want no evaluation retry after notification side effect", len(writer.mutations), len(writer.notifications), len(notifier.messages))
	}
}

func TestPostgresSnapshotReaderGetsOpaqueObjectRowVersionWithFixedQueries(t *testing.T) {
	tests := []struct {
		name       string
		objectType ObjectType
		objectID   string
		wantSQL    string
	}{
		{name: "monitoring instance", objectType: ObjectTypeMonitoringInstance, objectID: "mi_row", wantSQL: "select xmin::text from monitoring_instances where monitoring_instance_id = $1"},
		{name: "target", objectType: ObjectTypeTarget, objectID: "tg_row", wantSQL: "select xmin::text from targets where target_id = $1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotSQL string
			var gotArgs []any
			reader := &PostgresSnapshotReader{db: fakeIncidentSnapshotDB{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
				gotSQL = strings.Join(strings.Fields(sql), " ")
				gotArgs = append([]any(nil), args...)
				return fakeIncidentSnapshotRow{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "opaque-xmin-token"
					return nil
				}}
			}}}

			got, err := reader.GetObjectRowVersion(context.Background(), tt.objectType, tt.objectID)
			if err != nil {
				t.Fatalf("GetObjectRowVersion() error = %v", err)
			}
			if got != "opaque-xmin-token" || gotSQL != tt.wantSQL || len(gotArgs) != 1 || gotArgs[0] != tt.objectID {
				t.Fatalf("GetObjectRowVersion() = %q SQL=%q args=%#v, want opaque token and fixed query", got, gotSQL, gotArgs)
			}
		})
	}

	t.Run("missing object fails closed", func(t *testing.T) {
		reader := &PostgresSnapshotReader{db: fakeIncidentSnapshotDB{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeIncidentSnapshotRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}}}
		if token, err := reader.GetObjectRowVersion(context.Background(), ObjectTypeTarget, "tg_missing"); err == nil || token != "" || !errors.Is(err, ErrIncidentProjectionObjectNotFound) || !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("GetObjectRowVersion() token=%q err=%v, want stable object-not-found classification with pgx.ErrNoRows cause", token, err)
		}
	})

	t.Run("empty token fails closed", func(t *testing.T) {
		reader := &PostgresSnapshotReader{db: fakeIncidentSnapshotDB{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeIncidentSnapshotRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = ""
				return nil
			}}
		}}}
		if token, err := reader.GetObjectRowVersion(context.Background(), ObjectTypeTarget, "tg_empty"); err == nil || token != "" {
			t.Fatalf("GetObjectRowVersion() token=%q err=%v, want fail closed empty token", token, err)
		}
	})

	t.Run("unsupported type and empty id fail before query", func(t *testing.T) {
		reader := &PostgresSnapshotReader{}
		for _, input := range []struct {
			objectType ObjectType
			objectID   string
		}{
			{ObjectTypeVPS, "vps_unsupported"},
			{ObjectTypeTarget, ""},
		} {
			if token, err := reader.GetObjectRowVersion(context.Background(), input.objectType, input.objectID); err == nil || token != "" {
				t.Fatalf("GetObjectRowVersion(%q,%q) token=%q err=%v, want fail closed", input.objectType, input.objectID, token, err)
			}
		}
	})
}

func (f *fakeSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if f.getSettingsErr != nil {
		return centersettings.CenterSettings{}, f.getSettingsErr
	}
	return f.getSettingsResult, nil
}

func (f *fakeSettingsRepository) PutSettings(context.Context, centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	panic("unexpected PutSettings call")
}

func (f *fakeSettingsRepository) GetPersistedTelegramSettings(context.Context) (centersettings.TelegramSettings, bool, error) {
	if f.persistedErr != nil {
		return centersettings.TelegramSettings{}, false, f.persistedErr
	}
	if !f.persistedExists {
		return centersettings.TelegramSettings{}, false, nil
	}
	return f.persistedTelegram, true, nil
}

func (f *fakeSettingsRepository) GetPersistedIncidentDefaults(context.Context) (centersettings.IncidentDefaults, bool, error) {
	if f.persistedIncidentDefaultsErr != nil {
		return centersettings.IncidentDefaults{}, false, f.persistedIncidentDefaultsErr
	}
	if !f.persistedIncidentExists {
		return centersettings.IncidentDefaults{}, false, nil
	}
	return f.persistedIncidentDefaults, true, nil
}

func TestServiceNotificationFlags(t *testing.T) {
	makeEvaluation := func(reason NotificationReason) classEvaluation {
		return classEvaluation{
			class: IncidentMonitoringInstanceDiskPressure,
			result: EvaluationResult{
				Current: &IncidentRecord{
					IncidentID:    "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
					ObjectType:    ObjectTypeMonitoringInstance,
					ObjectID:      "mi_001",
					IncidentClass: IncidentMonitoringInstanceDiskPressure,
				},
				Notification: &NotificationDecision{
					ShouldSend: true,
					Channel:    NotificationChannelTelegram,
					Reason:     reason,
					Summary:    "summary",
				},
			},
		}
	}

	tests := []struct {
		name                 string
		reason               NotificationReason
		persistedDefaults    centersettings.IncidentDefaults
		settingsDefaults     centersettings.IncidentDefaults
		expectStatus         DeliveryStatus
		expectSendCount      int
		persistedAvailable   bool
		persistedUnavailable bool
		settingsAvailable    bool
		useNilRepo           bool
	}{
		{
			name:              "started suppressed by persisted defaults",
			reason:            NotificationReasonStarted,
			persistedDefaults: centersettings.IncidentDefaults{NotifyOnStarted: false, NotifyOnEscalated: true, NotifyOnRecovered: true},
			expectStatus:      DeliveryStatusSuppressed,
		},
		{
			name:              "escalated suppressed by persisted defaults",
			reason:            NotificationReasonEscalated,
			persistedDefaults: centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: false, NotifyOnRecovered: true},
			expectStatus:      DeliveryStatusSuppressed,
		},
		{
			name:              "recovered suppressed by persisted defaults",
			reason:            NotificationReasonRecovered,
			persistedDefaults: centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: true, NotifyOnRecovered: false},
			expectStatus:      DeliveryStatusSuppressed,
		},
		{
			name:               "started enabled by persisted defaults sends notification",
			reason:             NotificationReasonStarted,
			persistedDefaults:  centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: false, NotifyOnRecovered: false},
			expectStatus:       DeliveryStatusSent,
			expectSendCount:    1,
			persistedAvailable: true,
		},
		{
			name:            "defaults apply when settings repo is nil",
			reason:          NotificationReasonRecovered,
			expectStatus:    DeliveryStatusSent,
			expectSendCount: 1,
			useNilRepo:      true,
		},
		{
			name:                 "get settings suppresses when persisted defaults are unavailable",
			reason:               NotificationReasonRecovered,
			settingsDefaults:     centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: true, NotifyOnRecovered: false},
			expectStatus:         DeliveryStatusSuppressed,
			persistedUnavailable: true,
			settingsAvailable:    true,
		},
		{
			name:              "get settings sends when persisted defaults row is absent",
			reason:            NotificationReasonRecovered,
			settingsDefaults:  centersettings.IncidentDefaults{NotifyOnStarted: false, NotifyOnEscalated: false, NotifyOnRecovered: true},
			expectStatus:      DeliveryStatusSent,
			expectSendCount:   1,
			settingsAvailable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeMutationWriter{}
			notifier := &fakeNotifier{}

			var settingsRepo SettingsRepository
			if !tt.useNilRepo {
				repo := &fakeSettingsRepository{}
				if tt.persistedUnavailable {
					repo.persistedIncidentDefaultsErr = errors.New("persisted defaults unavailable")
				} else if tt.persistedAvailable || tt.expectStatus == DeliveryStatusSuppressed {
					repo.persistedIncidentDefaults = tt.persistedDefaults
					repo.persistedIncidentExists = true
				}
				if tt.settingsAvailable {
					repo.getSettingsResult = centersettings.CenterSettings{IncidentDefaults: tt.settingsDefaults}
				}
				settingsRepo = repo
			}

			service := NewSettingsBackedService(nil, nil, nil, writer, notifier, settingsRepo, slog.Default(), 30*time.Second, time.Minute)
			service.now = func() time.Time {
				return time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
			}

			if err := service.appendNotificationRecords(context.Background(), ObjectTypeMonitoringInstance, "mi_001", []classEvaluation{makeEvaluation(tt.reason)}); err != nil {
				t.Fatalf("appendNotificationRecords() error = %v", err)
			}
			if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
				t.Fatalf("notifications = %#v, want one record", writer.notifications)
			}
			record := writer.notifications[0][0]
			if record.DeliveryStatus != tt.expectStatus {
				t.Fatalf("DeliveryStatus = %q, want %q", record.DeliveryStatus, tt.expectStatus)
			}
			if got := len(notifier.messages); got != tt.expectSendCount {
				t.Fatalf("len(notifier.messages) = %d, want %d", got, tt.expectSendCount)
			}
		})
	}
}

func TestServiceAfterSuccessfulSyncEvaluatesMonitoringInstanceAndTouchedTargets(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {{ObservedAt: now, DiskUsedPct: 92}},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("len(mutations) = %d, want 2", len(writer.mutations))
	}
	if len(writer.notifications) == 0 {
		t.Fatal("notifications were not recorded")
	}
	if len(notifier.messages) == 0 {
		t.Fatal("notifier.messages = 0, want at least one notification")
	}
}

func TestServiceAfterSuccessfulSyncSuppressesNotificationsWithoutNotifier(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
		t.Fatalf("notifications = %#v, want suppressed notification record", writer.notifications)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
	}
}

func TestServiceRecordsFailedNotificationDelivery(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{err: errors.New("send failed")}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusFailed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusFailed)
	}
}

func TestServiceRecordsNotificationDeliveryPerChannel(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	dispatcher := &fakeNotificationDispatcher{
		deliveries: []NotificationDelivery{
			{Channel: NotificationChannelTelegram, Status: DeliveryStatusSent},
			{Channel: NotificationChannelFeishu, Status: DeliveryStatusFailed, Error: errors.New("feishu failed")},
		},
		channels: []NotificationChannel{NotificationChannelTelegram, NotificationChannelFeishu},
	}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.dispatcher = dispatcher
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(dispatcher.summaries) != 1 {
		t.Fatalf("dispatcher summaries = %#v, want one send", dispatcher.summaries)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 2 {
		t.Fatalf("notifications = %#v, want two channel records", writer.notifications)
	}
	gotByChannel := map[NotificationChannel]NotificationRecordWrite{}
	for _, record := range writer.notifications[0] {
		gotByChannel[record.Channel] = record
	}
	telegram := gotByChannel[NotificationChannelTelegram]
	if telegram.DeliveryStatus != DeliveryStatusSent || telegram.SentAt == nil {
		t.Fatalf("telegram record = %#v, want sent with sent_at", telegram)
	}
	feishu := gotByChannel[NotificationChannelFeishu]
	if feishu.DeliveryStatus != DeliveryStatusFailed || feishu.SentAt != nil {
		t.Fatalf("feishu record = %#v, want failed without sent_at", feishu)
	}
}

func TestServiceSuppressesNotificationPerResolvedChannel(t *testing.T) {
	writer := &fakeMutationWriter{}
	dispatcher := &fakeNotificationDispatcher{
		channels: []NotificationChannel{NotificationChannelTelegram, NotificationChannelFeishu},
	}
	settingsRepo := &fakeSettingsRepository{
		persistedIncidentDefaults: centersettings.IncidentDefaults{
			NotifyOnStarted:   false,
			NotifyOnEscalated: true,
			NotifyOnRecovered: true,
		},
		persistedIncidentExists: true,
	}
	service := NewSettingsBackedService(nil, nil, nil, writer, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)
	service.dispatcher = dispatcher

	evaluation := classEvaluation{
		class: IncidentMonitoringInstanceDiskPressure,
		result: EvaluationResult{
			Current: &IncidentRecord{
				IncidentID:    "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
				ObjectType:    ObjectTypeMonitoringInstance,
				ObjectID:      "mi_001",
				IncidentClass: IncidentMonitoringInstanceDiskPressure,
			},
			Notification: &NotificationDecision{
				ShouldSend: true,
				Channel:    NotificationChannelTelegram,
				Reason:     NotificationReasonStarted,
				Summary:    "summary",
			},
		},
	}

	if err := service.appendNotificationRecords(context.Background(), ObjectTypeMonitoringInstance, "mi_001", []classEvaluation{evaluation}); err != nil {
		t.Fatalf("appendNotificationRecords() error = %v", err)
	}
	if len(dispatcher.summaries) != 0 {
		t.Fatalf("dispatcher summaries = %#v, want no sends when policy suppresses", dispatcher.summaries)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 2 {
		t.Fatalf("notifications = %#v, want two suppressed records", writer.notifications)
	}
	for _, record := range writer.notifications[0] {
		if record.DeliveryStatus != DeliveryStatusSuppressed {
			t.Fatalf("record = %#v, want suppressed", record)
		}
	}
}

func TestSettingsAwareNotifierSuppressesWhenPersistedTelegramIsDisabled(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		persistedTelegram: centersettings.TelegramSettings{RuntimeManaged: true},
		persistedExists:   true,
	}
	buildCalls := 0
	service := NewService(
		monitoringInstanceRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			buildCalls++
			return &fakeNotifier{}
		}, fallbackNotifier),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
		t.Fatalf("notifications = %#v, want one suppressed notification record", writer.notifications)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
	}
	if buildCalls != 0 {
		t.Fatalf("buildCalls = %d, want 0", buildCalls)
	}
	if len(fallbackNotifier.messages) != 0 {
		t.Fatalf("fallbackNotifier.messages = %#v, want no env-fallback send when persisted Telegram is disabled", fallbackNotifier.messages)
	}
}

func TestSettingsAwareNotifierUsesFallbackWhenFreshDBWouldAutoCreateDisabledDefaults(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		persistedExists:   false,
	}
	service := NewService(
		monitoringInstanceRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			return &fakeNotifier{}
		}, fallbackNotifier),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(fallbackNotifier.messages) != 1 {
		t.Fatalf("fallbackNotifier.messages = %#v, want one env-fallback send for fresh DB behavior", fallbackNotifier.messages)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSent)
	}
}

func TestSettingsAwareNotifierUsesFallbackWhenPersistedTelegramIsNotManagingRuntime(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		persistedTelegram: centersettings.TelegramSettings{
			BotToken:       "persisted-bot-token",
			ChatID:         "persisted-chat-id",
			RuntimeManaged: false,
		},
		persistedExists: true,
	}
	buildCalls := 0
	service := NewService(
		monitoringInstanceRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			buildCalls++
			return &fakeNotifier{}
		}, fallbackNotifier),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(fallbackNotifier.messages) != 1 {
		t.Fatalf("fallbackNotifier.messages = %#v, want one env-fallback send when persisted Telegram is not managing runtime", fallbackNotifier.messages)
	}
	if buildCalls != 0 {
		t.Fatalf("buildCalls = %d, want 0", buildCalls)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSent)
	}
}

func TestSettingsAwareNotifierUsesPersistedTelegramConfig(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	liveNotifier := &fakeNotifier{}
	settings := centersettings.Default()
	settings.Telegram.BotToken = "persisted-bot-token"
	settings.Telegram.ChatID = "persisted-chat-id"
	settings.Telegram.RuntimeManaged = true
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: settings,
		persistedTelegram: settings.Telegram,
		persistedExists:   true,
	}
	var gotBotToken string
	var gotChatID string
	service := NewService(
		monitoringInstanceRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			gotBotToken = botToken
			gotChatID = chatID
			return liveNotifier
		}, &fakeNotifier{}),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if gotBotToken != settings.Telegram.BotToken {
		t.Fatalf("got bot token = %q, want %q", gotBotToken, settings.Telegram.BotToken)
	}
	if gotChatID != settings.Telegram.ChatID {
		t.Fatalf("got chat id = %q, want %q", gotChatID, settings.Telegram.ChatID)
	}
	if len(liveNotifier.messages) != 1 {
		t.Fatalf("liveNotifier.messages = %#v, want one send", liveNotifier.messages)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSent)
	}
}

func TestSettingsAwareNotificationDispatcherSendsFeishuOnly(t *testing.T) {
	settings := centersettings.Default()
	settings.FeishuEnabled = true
	settings.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/test"
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: settings,
		persistedTelegram: centersettings.TelegramSettings{RuntimeManaged: true},
		persistedExists:   true,
	}
	feishuNotifier := &fakeNotifier{}
	buildTelegramCalls := 0
	var gotWebhookURL string
	dispatcher := NewSettingsAwareNotificationDispatcher(
		settingsRepo,
		func(botToken, chatID string) Notifier {
			buildTelegramCalls++
			return &fakeNotifier{}
		},
		func(webhookURL string) Notifier {
			gotWebhookURL = webhookURL
			return feishuNotifier
		},
		&fakeNotifier{},
	)

	deliveries := dispatcher.Dispatch(context.Background(), "incident started")
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %#v, want one feishu delivery", deliveries)
	}
	gotByChannel := deliveriesByChannel(deliveries)
	if gotByChannel[NotificationChannelFeishu].Status != DeliveryStatusSent {
		t.Fatalf("feishu delivery = %#v, want sent", gotByChannel[NotificationChannelFeishu])
	}
	if gotWebhookURL != settings.FeishuWebhookURL {
		t.Fatalf("got webhook URL = %q, want settings webhook", gotWebhookURL)
	}
	if len(feishuNotifier.messages) != 1 || feishuNotifier.messages[0] != "incident started" {
		t.Fatalf("feishu messages = %#v, want one summary", feishuNotifier.messages)
	}
	if buildTelegramCalls != 0 {
		t.Fatalf("buildTelegramCalls = %d, want 0 for disabled persisted Telegram", buildTelegramCalls)
	}
}

func TestSettingsAwareNotificationDispatcherSendsTelegramAndFeishu(t *testing.T) {
	settings := centersettings.Default()
	settings.Telegram = centersettings.TelegramSettings{
		BotToken:       "persisted-bot-token",
		ChatID:         "persisted-chat-id",
		RuntimeManaged: true,
	}
	settings.FeishuEnabled = true
	settings.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/test"
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: settings,
		persistedTelegram: settings.Telegram,
		persistedExists:   true,
	}
	telegramNotifier := &fakeNotifier{}
	feishuNotifier := &fakeNotifier{err: errors.New("feishu failed")}
	dispatcher := NewSettingsAwareNotificationDispatcher(
		settingsRepo,
		func(botToken, chatID string) Notifier {
			if botToken != settings.Telegram.BotToken || chatID != settings.Telegram.ChatID {
				t.Fatalf("telegram config = %q/%q, want persisted settings", botToken, chatID)
			}
			return telegramNotifier
		},
		func(string) Notifier { return feishuNotifier },
		&fakeNotifier{},
	)

	deliveries := dispatcher.Dispatch(context.Background(), "incident started")
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %#v, want two channel deliveries", deliveries)
	}
	gotByChannel := deliveriesByChannel(deliveries)
	if gotByChannel[NotificationChannelTelegram].Status != DeliveryStatusSent {
		t.Fatalf("telegram delivery = %#v, want sent", gotByChannel[NotificationChannelTelegram])
	}
	feishu := gotByChannel[NotificationChannelFeishu]
	if feishu.Status != DeliveryStatusFailed || feishu.Error == nil {
		t.Fatalf("feishu delivery = %#v, want failed with error", feishu)
	}
	if len(telegramNotifier.messages) != 1 || len(feishuNotifier.messages) != 1 {
		t.Fatalf("telegram messages = %#v, feishu messages = %#v, want one each", telegramNotifier.messages, feishuNotifier.messages)
	}
}

func TestSettingsBackedHeartbeatIntervalUsesPersistedSettings(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-75 * time.Second)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &stale}}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	settings := centersettings.Default()
	settings.IncidentDefaults.HeartbeatIntervalSeconds = 120
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: settings.IncidentDefaults,
		persistedIncidentExists:   true,
	}
	service := NewSettingsBackedService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 0 {
		t.Fatalf("Active = %#v, want no heartbeat incident when persisted heartbeat interval is longer", writer.mutations[0].Active)
	}
}

func TestSettingsBackedHeartbeatStaleThresholdUsesPersistedSettings(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-3 * time.Minute)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_001", MonitoringStatus: monitoringinstances.MonitoringEnabled, LifecycleStatus: monitoringinstances.LifecycleInUse, LastHeartbeatAt: &stale}}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	settings := centersettings.Default()
	settings.IncidentDefaults.HeartbeatIntervalSeconds = 60
	settings.IncidentDefaults.StaleThresholdIntervals = 4
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: settings.IncidentDefaults,
		persistedIncidentExists:   true,
	}
	service := NewSettingsBackedService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, settingsRepo, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].Severity != SeverityNotice {
		t.Fatalf("Active = %#v, want notice incident when missed is one below alert threshold", writer.mutations[0].Active)
	}
}

func TestSettingsBackedSweepIntervalUsesPersistedSettings(t *testing.T) {
	settings := centersettings.Default()
	settings.IncidentDefaults.SweepIntervalSeconds = 180
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: settings.IncidentDefaults,
		persistedIncidentExists:   true,
	}
	service := NewSettingsBackedService(nil, nil, nil, nil, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)

	if got := service.sweepIntervalFor(context.Background()); got != 3*time.Minute {
		t.Fatalf("sweepIntervalFor() = %v, want %v", got, 3*time.Minute)
	}
}

func TestIncidentTimingFallsBackWhenPersistedSettingsAbsentOrUnavailable(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeSettingsRepository
	}{
		{
			name: "absent",
			repo: &fakeSettingsRepository{},
		},
		{
			name: "unavailable",
			repo: &fakeSettingsRepository{persistedIncidentDefaultsErr: errors.New("boom")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewSettingsBackedService(nil, nil, nil, nil, nil, tt.repo, slog.Default(), 45*time.Second, 2*time.Minute)

			if got := service.heartbeatIntervalFor(context.Background()); got != 45*time.Second {
				t.Fatalf("heartbeatIntervalFor() = %v, want %v", got, 45*time.Second)
			}
			if got := service.sweepIntervalFor(context.Background()); got != 2*time.Minute {
				t.Fatalf("sweepIntervalFor() = %v, want %v", got, 2*time.Minute)
			}
		})
	}
}

func TestServiceEvaluateStaleMonitoringInstancesCreatesHeartbeatIncident(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-3 * time.Minute)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &stale}}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].IncidentClass != IncidentMonitoringInstanceHeartbeatMissing {
		t.Fatalf("mutation = %#v, want heartbeat incident", writer.mutations[0])
	}
}

func TestServiceEvaluateStaleMonitoringInstancesRecoversNonRunningMonitoringInstances(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-5 * time.Minute)
	records := []monitoringinstances.Record{
		{MonitoringInstanceID: "mi_paused", MonitoringStatus: monitoringinstances.MonitoringPaused, LifecycleStatus: monitoringinstances.LifecycleInUse, LastHeartbeatAt: &stale},
		{MonitoringInstanceID: "mi_maintenance", MonitoringStatus: monitoringinstances.MonitoringMaintenance, LifecycleStatus: monitoringinstances.LifecycleInUse, LastHeartbeatAt: &stale},
		{MonitoringInstanceID: "mi_retired", MonitoringStatus: monitoringinstances.MonitoringEnabled, LifecycleStatus: monitoringinstances.LifecycleRetired, LastHeartbeatAt: &stale},
	}
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: records}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{activeByObject: map[string][]IncidentRecord{
		"monitoring_instance:mi_paused":      {activeIncident(ObjectTypeMonitoringInstance, "mi_paused", IncidentMonitoringInstanceHeartbeatMissing, now.Add(-time.Hour))},
		"monitoring_instance:mi_maintenance": {activeIncident(ObjectTypeMonitoringInstance, "mi_maintenance", IncidentMonitoringInstanceDiskPressure, now.Add(-time.Hour))},
		"monitoring_instance:mi_retired":     {activeIncident(ObjectTypeMonitoringInstance, "mi_retired", IncidentMonitoringInstanceResourcePressure, now.Add(-time.Hour))},
	}}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 3 {
		t.Fatalf("mutations = %#v, want one recovery mutation per non-running monitoring instance", writer.mutations)
	}
	for _, mutation := range writer.mutations {
		if mutation.ObjectType != ObjectTypeMonitoringInstance {
			t.Fatalf("mutation.ObjectType = %q, want monitoring_instance", mutation.ObjectType)
		}
		if len(mutation.Active) != 0 {
			t.Fatalf("mutation.Active = %#v, want no active incidents after administrative recovery", mutation.Active)
		}
		if len(mutation.Events) != 1 || mutation.Events[0].EventType != EventIncidentRecovered {
			t.Fatalf("mutation.Events = %#v, want one recovered event", mutation.Events)
		}
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want no administrative recovery notification records", writer.notifications)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no administrative recovery sends", notifier.messages)
	}
}

func TestServiceEvaluateStaleMonitoringInstancesRecoversArchivedMonitoringInstance(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-5 * time.Minute)
	archivedAt := now.Add(-time.Hour)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{
		MonitoringInstanceID: "mi_archived",
		MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		LifecycleStatus:      monitoringinstances.LifecycleInUse,
		LastHeartbeatAt:      &stale,
		ArchivedAt:           &archivedAt,
	}}}
	snapshots := &fakeSnapshotReader{activeByObject: map[string][]IncidentRecord{
		"monitoring_instance:mi_archived": {activeIncident(ObjectTypeMonitoringInstance, "mi_archived", IncidentMonitoringInstanceHeartbeatMissing, now.Add(-time.Hour))},
	}}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, &fakeTargetRepo{}, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("mutations = %#v, want one archived monitoring instance recovery mutation", writer.mutations)
	}
	mutation := writer.mutations[0]
	if mutation.ObjectType != ObjectTypeMonitoringInstance || mutation.ObjectID != "mi_archived" {
		t.Fatalf("mutation = %#v, want monitoring_instance mi_archived", mutation)
	}
	if len(mutation.Active) != 0 {
		t.Fatalf("mutation.Active = %#v, want no active incidents after archived administrative recovery", mutation.Active)
	}
	if len(mutation.Events) != 1 || mutation.Events[0].EventType != EventIncidentRecovered || mutation.Events[0].Summary != "监控实例已归档，当前异常按行政下线收敛" {
		t.Fatalf("mutation.Events = %#v, want archived recovered event", mutation.Events)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want no administrative recovery notification records", writer.notifications)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no administrative recovery sends", notifier.messages)
	}
}

func TestServiceAfterSuccessfulSyncRecoversInactiveMonitoringInstanceWithoutMetricEvaluation(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_paused",
			MonitoringStatus:     monitoringinstances.MonitoringPaused,
			LifecycleStatus:      monitoringinstances.LifecycleInUse,
			LastHeartbeatAt:      &now,
		},
	}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"monitoring_instance:mi_paused": {activeIncident(ObjectTypeMonitoringInstance, "mi_paused", IncidentMonitoringInstanceDiskPressure, now.Add(-time.Hour))},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_paused": {{ObservedAt: now, DiskUsedPct: 99}},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, &fakeTargetRepo{}, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_paused"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("mutations = %#v, want one recovery mutation", writer.mutations)
	}
	mutation := writer.mutations[0]
	if len(mutation.Active) != 0 {
		t.Fatalf("Active = %#v, want no active incidents for paused monitoring instance", mutation.Active)
	}
	if len(mutation.Events) != 1 || mutation.Events[0].IncidentClass != IncidentMonitoringInstanceDiskPressure || mutation.Events[0].EventType != EventIncidentRecovered {
		t.Fatalf("Events = %#v, want disk incident recovered", mutation.Events)
	}
	if len(writer.notifications) != 0 || len(notifier.messages) != 0 {
		t.Fatalf("notifications = %#v messages = %#v, want no administrative notifications", writer.notifications, notifier.messages)
	}
}

func TestServiceEvaluatePeriodicStateRecoversInactiveTargets(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: nil}
	targetRepo := &fakeTargetRepo{listTargetsResult: []targets.TargetRecord{
		{TargetID: "tg_paused", RunStatus: targets.RunStatusPaused},
		{TargetID: "tg_archived", RunStatus: targets.RunStatusArchived},
	}}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"target:tg_paused":   {activeIncident(ObjectTypeTarget, "tg_paused", IncidentTargetProbeFailure, now.Add(-time.Hour))},
			"target:tg_archived": {activeIncident(ObjectTypeTarget, "tg_archived", IncidentTargetTLSExpiry, now.Add(-time.Hour))},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_paused": {
				{ObservedAt: now, TargetID: "tg_paused", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_paused", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
			},
			"tg_archived": {
				{ObservedAt: now, TargetID: "tg_archived", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "expired"},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.EvaluatePeriodicState(context.Background(), now); err != nil {
		t.Fatalf("EvaluatePeriodicState() error = %v", err)
	}
	if len(writer.mutations) != 2 {
		t.Fatalf("mutations = %#v, want one recovery mutation per inactive target", writer.mutations)
	}
	for _, mutation := range writer.mutations {
		if mutation.ObjectType != ObjectTypeTarget {
			t.Fatalf("mutation.ObjectType = %q, want target", mutation.ObjectType)
		}
		if len(mutation.Active) != 0 {
			t.Fatalf("mutation.Active = %#v, want no active target incidents after inactive convergence", mutation.Active)
		}
		if len(mutation.Events) != 1 || mutation.Events[0].EventType != EventIncidentRecovered {
			t.Fatalf("mutation.Events = %#v, want one recovered event", mutation.Events)
		}
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want no administrative recovery notification records", writer.notifications)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no administrative recovery sends", notifier.messages)
	}
}

func TestServiceAfterSuccessfulSyncRecoversInactiveTouchedTargets(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			LifecycleStatus:      monitoringinstances.LifecycleInUse,
			LastHeartbeatAt:      &now,
		},
	}
	targetRepo := &fakeTargetRepo{getTargetResults: map[string]targets.TargetRecord{
		"tg_paused": {TargetID: "tg_paused", RunStatus: targets.RunStatusPaused},
	}}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"target:tg_paused": {activeIncident(ObjectTypeTarget, "tg_paused", IncidentTargetProbeFailure, now.Add(-time.Hour))},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_paused": {
				{ObservedAt: now, TargetID: "tg_paused", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_paused", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), time.Minute, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_paused"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 2 {
		t.Fatalf("mutations = %#v, want monitoring instance mutation and target recovery mutation", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	if targetMutation.ObjectType != ObjectTypeTarget || targetMutation.ObjectID != "tg_paused" {
		t.Fatalf("target mutation = %#v, want target tg_paused", targetMutation)
	}
	if len(targetMutation.Active) != 0 {
		t.Fatalf("target Active = %#v, want no active incidents for paused touched target", targetMutation.Active)
	}
	if len(targetMutation.Events) != 1 || targetMutation.Events[0].EventType != EventIncidentRecovered {
		t.Fatalf("target Events = %#v, want recovered event", targetMutation.Events)
	}
	if len(writer.notifications) != 0 || len(notifier.messages) != 0 {
		t.Fatalf("notifications = %#v messages = %#v, want no administrative notifications", writer.notifications, notifier.messages)
	}
}

func TestServiceAfterSuccessfulSyncUsesStoredLoadForResourcePressure(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {
				{ObservedAt: now, Load5: 6.2},
				{ObservedAt: now.Add(-8 * time.Minute), Load5: 6.3},
				{ObservedAt: now.Add(-15 * time.Minute), Load5: 6.1},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	found := false
	for _, incident := range writer.mutations[0].Active {
		if incident.IncidentClass == IncidentMonitoringInstanceResourcePressure {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mutation = %#v, want load-driven resource incident", writer.mutations[0])
	}
}

func TestMonitoringInstanceResourceSamplesFromHostSamplesPreservesReplayOrderingMetadata(t *testing.T) {
	t.Parallel()
	observedAt := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	receivedAt := observedAt.Add(3 * time.Minute)

	got := monitoringInstanceResourceSamplesFromHostSamples([]runtimefacts.HostSample{{
		ObservedAt: observedAt, ReceivedAt: receivedAt, IsBackfilled: true,
	}})

	if len(got) != 1 || !got[0].ReceivedAt.Equal(receivedAt) || !got[0].IsBackfilled {
		t.Fatalf("resource samples = %#v, want received/backfill provenance preserved", got)
	}
}

func TestSettingsBackedResourcePressureUsesPersistedLoadAndIOWaitThresholds(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	settings := centersettings.Default()
	settings.IncidentDefaults.Load5Warning = 4
	settings.IncidentDefaults.Load5Critical = 8
	settings.IncidentDefaults.IOWaitWarningPct = 20
	settings.IncidentDefaults.IOWaitCriticalPct = 50
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: settings.IncidentDefaults,
		persistedIncidentExists:   true,
	}

	quietWriter := &fakeMutationWriter{}
	quietService := NewSettingsBackedService(
		&fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_load", LastHeartbeatAt: &now}},
		&fakeTargetRepo{},
		&fakeSnapshotReader{hostSamples: map[string][]runtimefacts.HostSample{
			"mi_load": {
				{ObservedAt: now, Load5: 3.5},
				{ObservedAt: now.Add(-8 * time.Minute), Load5: 3.5},
				{ObservedAt: now.Add(-15 * time.Minute), Load5: 3.5},
			},
		}},
		quietWriter,
		nil,
		settingsRepo,
		slog.Default(),
		time.Minute,
		time.Minute,
	)
	quietService.now = func() time.Time { return now }
	if err := quietService.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_load"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync(load) error = %v", err)
	}
	for _, incident := range quietWriter.mutations[0].Active {
		if incident.IncidentClass == IncidentMonitoringInstanceResourcePressure {
			t.Fatalf("Active = %#v, want no resource incident below configured load warning", quietWriter.mutations[0].Active)
		}
	}

	iowaitWriter := &fakeMutationWriter{}
	iowaitService := NewSettingsBackedService(
		&fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_iowait", LastHeartbeatAt: &now}},
		&fakeTargetRepo{},
		&fakeSnapshotReader{hostSamples: map[string][]runtimefacts.HostSample{
			"mi_iowait": {
				{ObservedAt: now, CPUIOWaitPct: 55},
				{ObservedAt: now.Add(-15 * time.Minute), CPUIOWaitPct: 55},
				{ObservedAt: now.Add(-30 * time.Minute), CPUIOWaitPct: 55},
			},
		}},
		iowaitWriter,
		nil,
		settingsRepo,
		slog.Default(),
		time.Minute,
		time.Minute,
	)
	iowaitService.now = func() time.Time { return now }
	if err := iowaitService.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_iowait"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync(iowait) error = %v", err)
	}
	foundCritical := false
	for _, incident := range iowaitWriter.mutations[0].Active {
		if incident.IncidentClass == IncidentMonitoringInstanceResourcePressure && incident.Severity == SeverityCritical {
			foundCritical = true
		}
	}
	if !foundCritical {
		t.Fatalf("Active = %#v, want critical resource incident above configured iowait critical", iowaitWriter.mutations[0].Active)
	}
}

func TestServiceAfterSuccessfulSyncDispatchesMonitoringInstanceTrendDegradation(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {
				{ObservedAt: now, Load5: 2.1, CPUIOWaitPct: 12, CPUStealPct: 0.5},
				{ObservedAt: now.Add(-30 * time.Minute), Load5: 2.0, CPUIOWaitPct: 11, CPUStealPct: 0.4},
				{ObservedAt: now.Add(-23 * time.Hour), Load5: 1.9, CPUIOWaitPct: 13, CPUStealPct: 0.5},
			},
		},
		monitoringInstanceAggregates: map[string][]MonitoringInstanceHostDailyAggregate{
			"mi_001": {{
				BucketDate:      now.AddDate(0, 0, -1),
				SampleCount:     288,
				AvgLoad5:        0.7,
				AvgCPUIOWaitPct: 2,
				AvgCPUStealPct:  0.2,
			}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if !mutationsContainIncident(writer.mutations, IncidentMonitoringInstanceTrendDegradation) {
		t.Fatalf("mutations = %#v, want monitoringInstance trend degradation incident", writer.mutations)
	}
}

func TestServiceAfterSuccessfulSyncDispatchesTargetLatencyTrendDegradation(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	latencyA := 450
	latencyB := 460
	latencyC := 470
	baselineLatency := 120.0
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latencyA},
				{ObservedAt: now.Add(-30 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_002", ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latencyB},
				{ObservedAt: now.Add(-23 * time.Hour), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latencyC},
			},
		},
		targetAggregates: map[string][]TargetProbeDailyAggregate{
			"tg_001": {{
				TargetID:         "tg_001",
				ProbeItemID:      "pb_001",
				BucketDate:       now.AddDate(0, 0, -1),
				ObservationCount: 288,
				SuccessCount:     288,
				AvgLatencyMS:     &baselineLatency,
			}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if !mutationsContainIncident(writer.mutations, IncidentTargetLatencyTrendDegradation) {
		t.Fatalf("mutations = %#v, want target latency trend degradation incident", writer.mutations)
	}
}

func TestServiceSkippedEvaluationPreservesPriorIncidentDuringMaintenance(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"monitoring_instance:mi_001": {{
				IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
				ObjectType:      ObjectTypeMonitoringInstance,
				ObjectID:        "mi_001",
				IncidentClass:   IncidentMonitoringInstanceDiskPressure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "磁盘使用率 92.0%",
			}},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {{ObservedAt: now, DiskUsedPct: 99, MaintenanceContext: true}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].IncidentClass != IncidentMonitoringInstanceDiskPressure {
		t.Fatalf("Active = %#v, want prior incident preserved on skipped maintenance evaluation", writer.mutations[0].Active)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("Notifications = %#v, want none on skipped maintenance evaluation", writer.notifications)
	}
	if len(writer.mutations[0].Events) != 0 {
		t.Fatalf("Events = %#v, want none on skipped maintenance evaluation", writer.mutations[0].Events)
	}
}

func TestServiceMaintenanceRecoveryClosesIncidentSilently(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"monitoring_instance:mi_001": {{
				IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
				ObjectType:      ObjectTypeMonitoringInstance,
				ObjectID:        "mi_001",
				IncidentClass:   IncidentMonitoringInstanceDiskPressure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "磁盘使用率 92.0%",
			}},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {{ObservedAt: now, DiskUsedPct: 40, MaintenanceContext: true}},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 0 {
		t.Fatalf("Active = %#v, want recovered incident removed from active set", writer.mutations[0].Active)
	}
	if len(writer.mutations[0].Events) != 1 || writer.mutations[0].Events[0].EventType != EventIncidentRecovered {
		t.Fatalf("Events = %#v, want recovered event", writer.mutations[0].Events)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
		t.Fatalf("notifications = %#v, want one suppressed recovery record", writer.notifications)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no Telegram send during maintenance recovery", notifier.messages)
	}
}

func TestServiceDoesNotRecoverTargetProbeFailureUntilAllSeriesRecover(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	previous := IncidentRecord{
		IncidentID:      "inc_target_tg_001_target_probe_failure",
		ObjectType:      ObjectTypeTarget,
		ObjectID:        "tg_001",
		IncidentClass:   IncidentTargetProbeFailure,
		Severity:        SeverityAlert,
		StartedAt:       now.Add(-time.Hour),
		LastEvaluatedAt: now.Add(-time.Minute),
		Status:          IncidentStatusActive,
		SourceSummary:   "HTTP 探针连续失败 3 次",
	}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"target:tg_001": {previous},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure},
				{ObservedAt: now.Add(-3 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want monitoringInstance and target mutations", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	if len(targetMutation.Active) != 1 || targetMutation.Active[0].IncidentClass != IncidentTargetProbeFailure {
		t.Fatalf("target mutation = %#v, want target probe failure to remain active", targetMutation)
	}
}

func TestMultiSeriesRecoveriesRecordAnyBackfilledContributor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	previousProbe := &IncidentRecord{
		IncidentID:    "inc_target_tg_probe_target_probe_failure",
		ObjectType:    ObjectTypeTarget,
		ObjectID:      "tg_probe",
		IncidentClass: IncidentTargetProbeFailure,
		Severity:      SeverityAlert,
		Status:        IncidentStatusActive,
	}
	previousTLS := &IncidentRecord{
		IncidentID:    "inc_target_tg_tls_target_tls_expiry",
		ObjectType:    ObjectTypeTarget,
		ObjectID:      "tg_tls",
		IncidentClass: IncidentTargetTLSExpiry,
		Severity:      SeverityAlert,
		Status:        IncidentStatusActive,
	}
	safeDays := 45

	tests := []struct {
		name     string
		evaluate func() EvaluationResult
	}{
		{
			name: "probe failure",
			evaluate: func() EvaluationResult {
				return evaluateTargetProbeFailureAcrossSeries(previousProbe, "tg_probe", []runtimefacts.ProbeObservation{
					{ObservedAt: now, TargetID: "tg_probe", ProbeItemID: "pb_current", MonitoringInstanceID: "mi_current", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess},
					{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeItemID: "pb_current", MonitoringInstanceID: "mi_current", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess},
					{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_probe", ProbeItemID: "pb_backfill", MonitoringInstanceID: "mi_backfill", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, IsBackfilled: true},
					{ObservedAt: now.Add(-3 * time.Minute), TargetID: "tg_probe", ProbeItemID: "pb_backfill", MonitoringInstanceID: "mi_backfill", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, IsBackfilled: true},
				})
			},
		},
		{
			name: "TLS expiry",
			evaluate: func() EvaluationResult {
				return evaluateTargetTLSExpiryAcrossSeries(previousTLS, "tg_tls", []runtimefacts.ProbeObservation{
					{ObservedAt: now, TargetID: "tg_tls", ProbeItemID: "pb_current", MonitoringInstanceID: "mi_current", ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &safeDays},
					{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_tls", ProbeItemID: "pb_backfill", MonitoringInstanceID: "mi_backfill", ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &safeDays, IsBackfilled: true},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.evaluate()
			if result.Event == nil || result.Event.EventType != EventIncidentRecovered {
				t.Fatalf("event = %#v, want recovery", result.Event)
			}
			if !result.Event.IsBackfilled {
				t.Fatalf("event = %#v, want backfilled=true when any recovery contributor is backfilled", result.Event)
			}
			if result.Notification == nil || result.Notification.ShouldSend {
				t.Fatalf("notification = %#v, want suppressed backfilled recovery", result.Notification)
			}
		})
	}
}

func TestServiceAfterSuccessfulSyncDoesNotNotifyForBackfilledProbeFailure(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{
			ProbeObservations: []observations.ProbeObservationWrite{{
				MonitoringInstanceID: "mi_001",
				TargetID:             "tg_001",
				ProbeItemID:          "pb_001",
				ProbeKind:            agentapi.ProbeKindHTTP,
				ObservedAt:           now,
				ResultKind:           agentapi.ProbeResultFailure,
				ErrorSummary:         "503",
				IsBackfilled:         true,
			}},
		},
	}, syncing.Result{AcceptedAt: now})
	if err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want monitoringInstance and target mutations", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	if targetMutation.ObjectType != ObjectTypeTarget || targetMutation.ObjectID != "tg_001" {
		t.Fatalf("target mutation identity = %#v, want target tg_001", targetMutation)
	}
	if len(targetMutation.Active) != 0 {
		t.Fatalf("target mutation Active = %#v, want no active backfilled incident", targetMutation.Active)
	}
	if len(targetMutation.Events) != 0 {
		t.Fatalf("target mutation Events = %#v, want no notification-driving events", targetMutation.Events)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want none for backfilled probe failure", writer.notifications)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no sends for backfilled probe failure", notifier.messages)
	}
}

func TestServiceEvaluatesTLSExpiryFromTLSOnlySeries(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	expiry := 2
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &expiry},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want target mutation", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	found := false
	for _, incident := range targetMutation.Active {
		if incident.IncidentClass == IncidentTargetTLSExpiry && incident.Severity == SeverityCritical {
			found = true
		}
	}
	if !found {
		t.Fatalf("target mutation = %#v, want TLS expiry incident from TLS-only series", targetMutation)
	}
}

func TestServiceMaintenanceTargetRecoveriesAreSilent(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	expirySafeDays := 45
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"target:tg_probe": {{
				IncidentID:      "inc_target_tg_probe_target_probe_failure",
				ObjectType:      ObjectTypeTarget,
				ObjectID:        "tg_probe",
				IncidentClass:   IncidentTargetProbeFailure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "HTTP 探针连续失败 3 次",
			}},
			"target:tg_tls": {{
				IncidentID:      "inc_target_tg_tls_target_tls_expiry",
				ObjectType:      ObjectTypeTarget,
				ObjectID:        "tg_tls",
				IncidentClass:   IncidentTargetTLSExpiry,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-2 * time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "TLS 证书剩余 14 天",
			}},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_probe": {
				{ObservedAt: now, TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now, TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
			},
			"tg_tls": {
				{ObservedAt: now, TargetID: "tg_tls", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &expirySafeDays, IsBackfilled: true},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.evaluateTarget(context.Background(), "tg_probe", now); err != nil {
		t.Fatalf("evaluateTarget(tg_probe) error = %v", err)
	}
	if err := service.evaluateTarget(context.Background(), "tg_tls", now); err != nil {
		t.Fatalf("evaluateTarget(tg_tls) error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("len(mutations) = %d, want 2", len(writer.mutations))
	}
	for _, mutation := range writer.mutations {
		if len(mutation.Active) != 0 {
			t.Fatalf("mutation.Active = %#v, want recovered incidents removed", mutation.Active)
		}
		if len(mutation.Events) != 1 || mutation.Events[0].EventType != EventIncidentRecovered {
			t.Fatalf("mutation.Events = %#v, want recovered event", mutation.Events)
		}
	}
	if len(writer.notifications) != 2 {
		t.Fatalf("notifications = %#v, want two suppressed recovery batches", writer.notifications)
	}
	for _, batch := range writer.notifications {
		if len(batch) != 1 {
			t.Fatalf("notification batch = %#v, want one record", batch)
		}
		if batch[0].DeliveryStatus != DeliveryStatusSuppressed {
			t.Fatalf("DeliveryStatus = %q, want %q", batch[0].DeliveryStatus, DeliveryStatusSuppressed)
		}
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no Telegram sends for maintenance/backfill recoveries", notifier.messages)
	}
}

func storeProbeBatch(targetID string) observations.BatchWrite {
	latency := 83
	status := 503
	return observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{{
			MonitoringInstanceID: "mi_001",
			TargetID:             targetID,
			ProbeItemID:          "pb_001",
			ProbeKind:            agentapi.ProbeKindHTTP,
			ResultKind:           agentapi.ProbeResultFailure,
			LatencyMS:            &latency,
			HTTPStatus:           &status,
			ErrorSummary:         "503",
		}},
	}
}

func activeIncident(objectType ObjectType, objectID string, class IncidentClass, startedAt time.Time) IncidentRecord {
	return IncidentRecord{
		IncidentID:      "inc_" + string(objectType) + "_" + objectID + "_" + string(class),
		ObjectType:      objectType,
		ObjectID:        objectID,
		IncidentClass:   class,
		Severity:        SeverityAlert,
		StartedAt:       startedAt,
		LastEvaluatedAt: startedAt,
		Status:          IncidentStatusActive,
		SourceSummary:   "existing active incident",
	}
}

func deliveriesByChannel(deliveries []NotificationDelivery) map[NotificationChannel]NotificationDelivery {
	out := make(map[NotificationChannel]NotificationDelivery, len(deliveries))
	for _, delivery := range deliveries {
		out[delivery.Channel] = delivery
	}
	return out
}

func mutationsContainIncident(mutations []IncidentMutation, class IncidentClass) bool {
	for _, mutation := range mutations {
		for _, incident := range mutation.Active {
			if incident.IncidentClass == class {
				return true
			}
		}
	}
	return false
}

func slicesContainIncidentTestTrace(trace []string, want string) bool {
	for _, entry := range trace {
		if entry == want {
			return true
		}
	}
	return false
}

func assertIncidentTestTraceBefore(t *testing.T, trace []string, entries ...string) {
	t.Helper()
	position := -1
	for _, want := range entries {
		found := -1
		for i := position + 1; i < len(trace); i++ {
			if trace[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("trace = %#v, want %q after position %d", trace, want, position)
		}
		position = found
	}
}

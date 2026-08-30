package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
)

const (
	incidentProjectionCASMutationBlockLock      int64 = 917_004_113
	incidentProjectionCASBlockedApplicationName       = "incident-projection-cas-overlap-b"
)

func TestPostgresIntegrationIncidentProjectionCAS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "incident-projection-cas", 2)
	assertIncidentProjectionRuntimeACLContract(t, ctx, fixture)

	tests := []struct {
		name          string
		objectType    incidents.ObjectType
		objectID      string
		incidentClass incidents.IncidentClass
	}{
		{
			name:          "monitoring instance",
			objectType:    incidents.ObjectTypeMonitoringInstance,
			objectID:      "mi_incident_projection_cas",
			incidentClass: incidents.IncidentMonitoringInstanceResourcePressure,
		},
		{
			name:          "target",
			objectType:    incidents.ObjectTypeTarget,
			objectID:      "tg_incident_projection_cas",
			incidentClass: incidents.IncidentTargetProbeFailure,
		},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedIncidentProjectionCASObject(t, ctx, fixture.db, tt.objectType, tt.objectID)

			repository := NewPostgresIncidentRepository(runtimePool)
			staleRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			newerAt := time.Now().UTC().Add(-time.Duration(index+1) * time.Hour).Truncate(time.Microsecond)
			newerIncidentID := "inc_projection_cas_b_" + string(rune('a'+index))
			newerSummary := "newer projection B"
			newerMutation := incidentProjectionCASMutation(
				tt.objectType,
				tt.objectID,
				tt.incidentClass,
				newerIncidentID,
				newerSummary,
				incidents.SeverityAlert,
				"alert",
				newerAt,
				staleRowVersion,
			)
			if err := repository.ApplyIncidentMutation(ctx, newerMutation); err != nil {
				failIncidentProjectionPostgresOperation(t, "apply newer incident projection", err)
			}

			newerRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			if newerRowVersion == staleRowVersion {
				t.Fatal("object row version did not advance after newer projection")
			}
			// Notification dispatch/append is intentionally after the successful
			// mutation. Service unit tests own dispatch ordering; this PostgreSQL
			// regression does not claim external exactly-once delivery.
			if err := repository.AppendNotificationRecords(ctx, []incidents.NotificationRecordWrite{{
				IncidentID:     newerIncidentID,
				ObjectType:     tt.objectType,
				ObjectID:       tt.objectID,
				Channel:        incidents.NotificationChannelTelegram,
				DeliveryStatus: incidents.DeliveryStatusSent,
				Summary:        newerSummary,
				SentAt:         &newerAt,
			}}); err != nil {
				failIncidentProjectionPostgresOperation(t, "append newer notification record", err)
			}

			staleIncidentID := "inc_projection_cas_a_" + string(rune('a'+index))
			staleSummary := "stale projection A"
			before := readIncidentProjectionCASState(
				t, ctx, fixture.db, tt.objectType, tt.objectID, tt.incidentClass,
				newerIncidentID, staleIncidentID, newerSummary, staleSummary, newerAt,
				staleRowVersion, newerRowVersion,
			)
			assertIncidentProjectionCASNewerState(t, before)

			staleMutation := incidentProjectionCASMutation(
				tt.objectType,
				tt.objectID,
				tt.incidentClass,
				staleIncidentID,
				staleSummary,
				incidents.SeverityCritical,
				"critical",
				newerAt.Add(-time.Minute),
				staleRowVersion,
			)
			err := repository.ApplyIncidentMutation(ctx, staleMutation)
			if !errors.Is(err, incidents.ErrIncidentProjectionConflict) {
				t.Fatal("stale incident projection error classification mismatch")
			}
			if strings.Contains(err.Error(), staleRowVersion) || strings.Contains(err.Error(), newerRowVersion) {
				t.Fatal("incident projection conflict exposed an opaque row version")
			}

			afterRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			if afterRowVersion != newerRowVersion {
				t.Fatal("object row version changed after stale projection conflict")
			}
			after := readIncidentProjectionCASState(
				t, ctx, fixture.db, tt.objectType, tt.objectID, tt.incidentClass,
				newerIncidentID, staleIncidentID, newerSummary, staleSummary, newerAt,
				staleRowVersion, newerRowVersion,
			)
			assertIncidentProjectionCASNewerState(t, after)
			if after != before {
				t.Fatalf(
					"projection state changed after stale conflict: active=%d/%d newer-active=%d/%d stale-active=%d/%d summary=%t/%t events=%d/%d newer-events=%d/%d stale-events=%d/%d notifications=%d/%d newer-notifications=%d/%d",
					before.activeCount, after.activeCount,
					before.newerActiveCount, after.newerActiveCount,
					before.staleActiveCount, after.staleActiveCount,
					before.summaryMatches, after.summaryMatches,
					before.eventCount, after.eventCount,
					before.newerEventCount, after.newerEventCount,
					before.staleEventCount, after.staleEventCount,
					before.notificationCount, after.notificationCount,
					before.newerNotificationCount, after.newerNotificationCount,
				)
			}
		})
	}
}

func TestPostgresIntegrationIncidentProjectionCASDeletedObjectBeforeWriterGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "incident-projection-cas-deleted-object", 2)
	repository := NewPostgresIncidentRepository(runtimePool)
	tests := []struct {
		name          string
		objectType    incidents.ObjectType
		objectID      string
		incidentClass incidents.IncidentClass
	}{
		{
			name:          "monitoring instance",
			objectType:    incidents.ObjectTypeMonitoringInstance,
			objectID:      "mi_incident_projection_deleted",
			incidentClass: incidents.IncidentMonitoringInstanceResourcePressure,
		},
		{
			name:          "target",
			objectType:    incidents.ObjectTypeTarget,
			objectID:      "tg_incident_projection_deleted",
			incidentClass: incidents.IncidentTargetProbeFailure,
		},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedIncidentProjectionCASObject(t, ctx, fixture.db, tt.objectType, tt.objectID)
			rowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			deleteIncidentProjectionCASObject(t, ctx, fixture.db, tt.objectType, tt.objectID)

			evaluatedAt := time.Now().UTC().Add(-time.Duration(index+1) * time.Hour).Truncate(time.Microsecond)
			err := repository.ApplyIncidentMutation(ctx, incidentProjectionCASMutation(
				tt.objectType,
				tt.objectID,
				tt.incidentClass,
				"inc_projection_deleted_"+string(rune('a'+index)),
				"deleted object must not project",
				incidents.SeverityCritical,
				"critical",
				evaluatedAt,
				rowVersion,
			))
			if !errors.Is(err, incidents.ErrIncidentProjectionObjectNotFound) {
				t.Fatalf("deleted-object projection error = %v, want stable missing-object classification", err)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("deleted-object projection error = %v, want pgx.ErrNoRows cause", err)
			}
			if strings.Contains(err.Error(), rowVersion) {
				t.Fatal("deleted-object projection error exposed an opaque row version")
			}

			objectCount, activeCount, eventCount, notificationCount := readIncidentProjectionDeletedObjectState(t, ctx, fixture.db, tt.objectType, tt.objectID)
			if objectCount != 0 || activeCount != 0 || eventCount != 0 || notificationCount != 0 {
				t.Fatalf(
					"deleted-object projection side effects: object=%d active=%d events=%d notifications=%d, want all zero",
					objectCount, activeCount, eventCount, notificationCount,
				)
			}
		})
	}
}

func TestPostgresIntegrationIncidentProjectionCASConcurrentStaleWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	installIncidentProjectionCASMutationBlocker(t, ctx, fixture)
	assertIncidentProjectionRuntimeACLContract(t, ctx, fixture)

	tests := []struct {
		name          string
		objectType    incidents.ObjectType
		objectID      string
		incidentClass incidents.IncidentClass
	}{
		{
			name:          "monitoring instance",
			objectType:    incidents.ObjectTypeMonitoringInstance,
			objectID:      "mi_incident_projection_overlap",
			incidentClass: incidents.IncidentMonitoringInstanceResourcePressure,
		},
		{
			name:          "target",
			objectType:    incidents.ObjectTypeTarget,
			objectID:      "tg_incident_projection_overlap",
			incidentClass: incidents.IncidentTargetProbeFailure,
		},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedIncidentProjectionCASObject(t, ctx, fixture.db, tt.objectType, tt.objectID)
			newerPool := fixture.openDirectRuntimePool(t, ctx, incidentProjectionCASBlockedApplicationName, 1)
			stalePool := fixture.openDirectRuntimePool(t, ctx, "incident-projection-cas-overlap-a", 1)
			initialRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, stalePool, tt.objectType, tt.objectID)
			evaluatedAt := time.Now().UTC().Add(-time.Duration(index+1) * time.Hour).Truncate(time.Microsecond)
			newerIncidentID := "inc_projection_overlap_b_" + string(rune('a'+index))
			staleIncidentID := "inc_projection_overlap_a_" + string(rune('a'+index))
			newerSummary := "overlapping projection B"
			staleSummary := "overlapping projection A"

			blocker, err := fixture.db.Acquire(ctx)
			if err != nil {
				failIncidentProjectionPostgresOperation(t, "acquire incident projection blocker", err)
			}
			blockReleased := false
			defer func() {
				if !blockReleased {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cleanupCancel()
					_, _ = blocker.Exec(cleanupCtx, `select pg_catalog.pg_advisory_unlock($1)`, incidentProjectionCASMutationBlockLock)
				}
				blocker.Release()
			}()
			if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_lock($1)`, incidentProjectionCASMutationBlockLock); err != nil {
				failIncidentProjectionPostgresOperation(t, "acquire incident projection advisory blocker", err)
			}
			blockerPID := readIncidentProjectionBackendPID(t, ctx, blocker)

			newerPID := readIncidentProjectionBackendPID(t, ctx, newerPool)
			stalePID := readIncidentProjectionBackendPID(t, ctx, stalePool)
			newerRepository := NewPostgresIncidentRepository(newerPool)
			staleRepository := NewPostgresIncidentRepository(stalePool)

			newerResult := make(chan error, 1)
			go func() {
				newerResult <- newerRepository.ApplyIncidentMutation(ctx, incidentProjectionCASMutation(
					tt.objectType, tt.objectID, tt.incidentClass,
					newerIncidentID, newerSummary, incidents.SeverityAlert, "alert",
					evaluatedAt, initialRowVersion,
				))
			}()
			waitForIncidentProjectionBackendBlock(t, ctx, fixture.db, newerPID, blockerPID,
				"newer projection did not reach the post-guard mutation blocker")

			staleResult := make(chan error, 1)
			go func() {
				staleResult <- staleRepository.ApplyIncidentMutation(ctx, incidentProjectionCASMutation(
					tt.objectType, tt.objectID, tt.incidentClass,
					staleIncidentID, staleSummary, incidents.SeverityCritical, "critical",
					evaluatedAt.Add(-time.Minute), initialRowVersion,
				))
			}()
			waitForIncidentProjectionBackendBlock(t, ctx, fixture.db, stalePID, newerPID,
				"stale projection did not wait on the guarded object row")
			assertIncidentProjectionMutationPending(t, newerResult, "newer projection completed before blocker release")
			assertIncidentProjectionMutationPending(t, staleResult, "stale projection completed before newer projection")

			if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_unlock($1)`, incidentProjectionCASMutationBlockLock); err != nil {
				failIncidentProjectionPostgresOperation(t, "release incident projection advisory blocker", err)
			}
			blockReleased = true

			if err := waitForIncidentProjectionMutationResult(t, ctx, newerResult, "newer incident projection"); err != nil {
				failIncidentProjectionPostgresOperation(t, "apply overlapping newer incident projection", err)
			}
			newerRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, newerPool, tt.objectType, tt.objectID)
			if newerRowVersion == initialRowVersion {
				t.Fatal("object row version did not advance after overlapping newer projection")
			}
			staleErr := waitForIncidentProjectionMutationResult(t, ctx, staleResult, "stale incident projection")
			if !errors.Is(staleErr, incidents.ErrIncidentProjectionConflict) {
				t.Fatal("overlapping stale incident projection error classification mismatch")
			}
			if strings.Contains(staleErr.Error(), initialRowVersion) || strings.Contains(staleErr.Error(), newerRowVersion) {
				t.Fatal("overlapping incident projection conflict exposed an opaque row version")
			}

			state := readIncidentProjectionCASState(
				t, ctx, fixture.db, tt.objectType, tt.objectID, tt.incidentClass,
				newerIncidentID, staleIncidentID, newerSummary, staleSummary, evaluatedAt,
				initialRowVersion, newerRowVersion,
			)
			assertIncidentProjectionCASState(t, state, 0)
		})
	}
}

func TestPostgresIntegrationIncidentProjectionCASRejectsUnrelatedRuntimeUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	assertIncidentProjectionRuntimeACLContract(t, ctx, fixture)
	tests := []struct {
		name          string
		objectType    incidents.ObjectType
		objectID      string
		incidentClass incidents.IncidentClass
	}{
		{
			name:          "monitoring instance",
			objectType:    incidents.ObjectTypeMonitoringInstance,
			objectID:      "mi_incident_projection_unrelated",
			incidentClass: incidents.IncidentMonitoringInstanceResourcePressure,
		},
		{
			name:          "target",
			objectType:    incidents.ObjectTypeTarget,
			objectID:      "tg_incident_projection_unrelated",
			incidentClass: incidents.IncidentTargetProbeFailure,
		},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedIncidentProjectionCASObject(t, ctx, fixture.db, tt.objectType, tt.objectID)
			runtimePool := fixture.openDirectRuntimePool(t, ctx, "incident-projection-cas-unrelated-"+string(rune('a'+index)), 1)
			repository := NewPostgresIncidentRepository(runtimePool)
			staleRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			const unrelatedNote = "runtime unrelated edit"
			updateIncidentProjectionUnrelatedField(t, ctx, runtimePool, tt.objectType, tt.objectID, unrelatedNote)
			currentRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			if currentRowVersion == staleRowVersion {
				t.Fatal("object row version did not advance after unrelated runtime update")
			}
			before := readIncidentProjectionUnrelatedState(t, ctx, fixture.db, tt.objectType, tt.objectID, unrelatedNote)
			assertIncidentProjectionUnrelatedState(t, before)

			evaluatedAt := time.Now().UTC().Add(-time.Duration(index+1) * time.Hour).Truncate(time.Microsecond)
			err := repository.ApplyIncidentMutation(ctx, incidentProjectionCASMutation(
				tt.objectType, tt.objectID, tt.incidentClass,
				"inc_projection_unrelated_"+string(rune('a'+index)), "must remain absent",
				incidents.SeverityAlert, "alert", evaluatedAt, staleRowVersion,
			))
			if !errors.Is(err, incidents.ErrIncidentProjectionConflict) {
				t.Fatal("unrelated runtime update stale projection error classification mismatch")
			}
			if strings.Contains(err.Error(), staleRowVersion) || strings.Contains(err.Error(), currentRowVersion) {
				t.Fatal("unrelated runtime update conflict exposed an opaque row version")
			}
			afterRowVersion := readIncidentProjectionObjectRowVersion(t, ctx, runtimePool, tt.objectType, tt.objectID)
			if afterRowVersion != currentRowVersion {
				t.Fatal("object row version changed after unrelated-update conflict")
			}
			after := readIncidentProjectionUnrelatedState(t, ctx, fixture.db, tt.objectType, tt.objectID, unrelatedNote)
			assertIncidentProjectionUnrelatedState(t, after)
			if after != before {
				t.Fatalf(
					"unrelated-update state changed after stale conflict: note=%t/%t summary=%t/%t active=%d/%d events=%d/%d notifications=%d/%d",
					before.noteMatches, after.noteMatches,
					before.summaryUnchanged, after.summaryUnchanged,
					before.activeCount, after.activeCount,
					before.eventCount, after.eventCount,
					before.notificationCount, after.notificationCount,
				)
			}
		})
	}
}

func seedIncidentProjectionCASObject(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
) {
	t.Helper()
	var err error
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		_, err = db.Exec(ctx, `
			insert into public.monitoring_instances (
				monitoring_instance_id, display_name, region, city, provider,
				lifecycle_status, monitoring_status, binding_status
			) values ($1, 'Incident projection CAS MI', '', '', '', $2, $3, $4)`,
			objectID,
			monitoringinstances.LifecycleInUse,
			monitoringinstances.MonitoringEnabled,
			monitoringinstances.BindingBound,
		)
	case incidents.ObjectTypeTarget:
		_, err = db.Exec(ctx, `
			insert into public.targets (target_id, name, target_type, host, run_status)
			values ($1, 'Incident projection CAS target', 'hostname', 'cas.invalid', $2)`,
			objectID,
			targets.RunStatusEnabled,
		)
	default:
		t.Fatal("incident projection fixture object type is unsupported")
	}
	if err != nil {
		failIncidentProjectionPostgresOperation(t, "seed incident projection object", err)
	}
}

func deleteIncidentProjectionCASObject(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
) {
	t.Helper()
	var err error
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		_, err = db.Exec(ctx, `delete from public.monitoring_instances where monitoring_instance_id = $1`, objectID)
	case incidents.ObjectTypeTarget:
		_, err = db.Exec(ctx, `delete from public.targets where target_id = $1`, objectID)
	default:
		t.Fatal("incident projection delete object type is unsupported")
	}
	if err != nil {
		failIncidentProjectionPostgresOperation(t, "delete incident projection object", err)
	}
}

func readIncidentProjectionDeletedObjectState(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
) (objectCount, activeCount, eventCount, notificationCount int) {
	t.Helper()
	var objectCountSQL string
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		objectCountSQL = `(select count(*)::int from public.monitoring_instances where monitoring_instance_id = $2)`
	case incidents.ObjectTypeTarget:
		objectCountSQL = `(select count(*)::int from public.targets where target_id = $2)`
	default:
		t.Fatal("incident projection deleted-state object type is unsupported")
	}
	err := db.QueryRow(ctx, `select `+objectCountSQL+`,
		(select count(*)::int from public.active_incidents where object_type = $1 and object_id = $2),
		(select count(*)::int from public.state_change_events where object_type = $1 and object_id = $2),
		(select count(*)::int from public.notification_records where object_type = $1 and object_id = $2)`,
		string(objectType), objectID,
	).Scan(&objectCount, &activeCount, &eventCount, &notificationCount)
	if err != nil {
		failIncidentProjectionPostgresOperation(t, "read deleted incident projection state", err)
	}
	return objectCount, activeCount, eventCount, notificationCount
}

func readIncidentProjectionObjectRowVersion(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
) string {
	t.Helper()
	var (
		rowVersion string
		err        error
	)
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		err = db.QueryRow(ctx, `
			select xmin::text
			from public.monitoring_instances
			where monitoring_instance_id = $1`, objectID).Scan(&rowVersion)
	case incidents.ObjectTypeTarget:
		err = db.QueryRow(ctx, `
			select xmin::text
			from public.targets
			where target_id = $1`, objectID).Scan(&rowVersion)
	default:
		t.Fatal("incident projection row-version object type is unsupported")
	}
	if err != nil {
		failIncidentProjectionPostgresOperation(t, "read incident projection row version", err)
	}
	if rowVersion == "" {
		t.Fatal("incident projection row version is empty")
	}
	return rowVersion
}

func incidentProjectionCASMutation(
	objectType incidents.ObjectType,
	objectID string,
	incidentClass incidents.IncidentClass,
	incidentID string,
	summary string,
	severity incidents.Severity,
	resultingState string,
	evaluatedAt time.Time,
	expectedRowVersion string,
) incidents.IncidentMutation {
	return incidents.IncidentMutation{
		ObjectType:               objectType,
		ObjectID:                 objectID,
		ExpectedObjectRowVersion: expectedRowVersion,
		Active: []incidents.IncidentRecord{{
			IncidentID:      incidentID,
			ObjectType:      objectType,
			ObjectID:        objectID,
			IncidentClass:   incidentClass,
			Severity:        severity,
			StartedAt:       evaluatedAt,
			LastEvaluatedAt: evaluatedAt,
			Status:          incidents.IncidentStatusActive,
			SourceSummary:   summary,
		}},
		Events: []incidents.StateChangeEventRecord{{
			IncidentID:      incidentID,
			IncidentClass:   incidentClass,
			ObjectType:      objectType,
			ObjectID:        objectID,
			EventType:       incidents.EventIncidentStarted,
			Severity:        severity,
			Summary:         summary,
			CreatedAt:       evaluatedAt,
			Provenance:      incidents.MonitoringEventProvenanceCenter,
			ProducerVersion: incidents.MonitoringEventProducerVersion,
			RuleVersion:     incidents.MonitoringEventIncidentRuleVersion,
			PriorState:      "normal",
			ResultingState:  resultingState,
		}},
	}
}

type incidentProjectionCASState struct {
	activeCount            int
	newerActiveCount       int
	staleActiveCount       int
	summaryMatches         bool
	eventCount             int
	newerEventCount        int
	staleEventCount        int
	eventPayloadTokenCount int
	notificationCount      int
	newerNotificationCount int
}

const incidentProjectionCASMonitoringInstanceStateSQL = `
	select
		(select count(*)::int from public.active_incidents where object_type = $1 and object_id = $2),
		(select count(*)::int from public.active_incidents
		 where object_type = $1 and object_id = $2 and incident_id = $3 and incident_class = $4
		   and severity = $5 and started_at = $6 and last_evaluated_at = $6
		   and status = 'active' and source_summary = $7),
		(select count(*)::int from public.active_incidents
		 where object_type = $1 and object_id = $2 and incident_id = $8),
		coalesce((select current_health_status = $5
		                 and current_active_incident_count = 1
		                 and current_primary_issue_summary = $7
		          from public.monitoring_instances where monitoring_instance_id = $2), false),
		(select count(*)::int from public.state_change_events where object_type = $1 and object_id = $2),
			(select count(*)::int from public.state_change_events
			 where object_type = $1 and object_id = $2 and event_type = 'incident_started'
			   and severity = $5 and summary = $7),
			(select count(*)::int from public.state_change_events
			 where object_type = $1 and object_id = $2 and summary = $9),
				(select count(*)::int from public.state_change_events
				 where object_type = $1 and object_id = $2
				   and (pg_catalog.jsonb_path_exists(
				          payload,
				          '$.** ? (@ == $token)',
				          pg_catalog.jsonb_build_object('token', pg_catalog.to_jsonb($10::text)))
				        or pg_catalog.jsonb_path_exists(
				          payload,
				          '$.** ? (@ == $token)',
				          pg_catalog.jsonb_build_object('token', pg_catalog.to_jsonb($11::text))))),
			(select count(*)::int from public.notification_records where object_type = $1 and object_id = $2),
		(select count(*)::int from public.notification_records
		 where object_type = $1 and object_id = $2 and incident_id = $3
		   and channel = 'telegram' and delivery_status = 'sent' and summary = $7 and sent_at = $6)`

const incidentProjectionCASTargetStateSQL = `
	select
		(select count(*)::int from public.active_incidents where object_type = $1 and object_id = $2),
		(select count(*)::int from public.active_incidents
		 where object_type = $1 and object_id = $2 and incident_id = $3 and incident_class = $4
		   and severity = $5 and started_at = $6 and last_evaluated_at = $6
		   and status = 'active' and source_summary = $7),
		(select count(*)::int from public.active_incidents
		 where object_type = $1 and object_id = $2 and incident_id = $8),
		coalesce((select current_health_status = $5
		                 and current_active_incident_count = 1
		                 and current_primary_issue_summary = $7
		          from public.targets where target_id = $2), false),
		(select count(*)::int from public.state_change_events where object_type = $1 and object_id = $2),
			(select count(*)::int from public.state_change_events
			 where object_type = $1 and object_id = $2 and event_type = 'incident_started'
			   and severity = $5 and summary = $7),
			(select count(*)::int from public.state_change_events
			 where object_type = $1 and object_id = $2 and summary = $9),
				(select count(*)::int from public.state_change_events
				 where object_type = $1 and object_id = $2
				   and (pg_catalog.jsonb_path_exists(
				          payload,
				          '$.** ? (@ == $token)',
				          pg_catalog.jsonb_build_object('token', pg_catalog.to_jsonb($10::text)))
				        or pg_catalog.jsonb_path_exists(
				          payload,
				          '$.** ? (@ == $token)',
				          pg_catalog.jsonb_build_object('token', pg_catalog.to_jsonb($11::text))))),
			(select count(*)::int from public.notification_records where object_type = $1 and object_id = $2),
		(select count(*)::int from public.notification_records
		 where object_type = $1 and object_id = $2 and incident_id = $3
		   and channel = 'telegram' and delivery_status = 'sent' and summary = $7 and sent_at = $6)`

func readIncidentProjectionCASState(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
	incidentClass incidents.IncidentClass,
	newerIncidentID string,
	staleIncidentID string,
	newerSummary string,
	staleSummary string,
	newerAt time.Time,
	staleRowVersion string,
	newerRowVersion string,
) incidentProjectionCASState {
	t.Helper()
	var query string
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		query = incidentProjectionCASMonitoringInstanceStateSQL
	case incidents.ObjectTypeTarget:
		query = incidentProjectionCASTargetStateSQL
	default:
		t.Fatal("incident projection state object type is unsupported")
	}
	var state incidentProjectionCASState
	err := db.QueryRow(
		ctx,
		query,
		string(objectType),
		objectID,
		newerIncidentID,
		string(incidentClass),
		string(incidents.SeverityAlert),
		newerAt,
		newerSummary,
		staleIncidentID,
		staleSummary,
		staleRowVersion,
		newerRowVersion,
	).Scan(
		&state.activeCount,
		&state.newerActiveCount,
		&state.staleActiveCount,
		&state.summaryMatches,
		&state.eventCount,
		&state.newerEventCount,
		&state.staleEventCount,
		&state.eventPayloadTokenCount,
		&state.notificationCount,
		&state.newerNotificationCount,
	)
	if err != nil {
		failIncidentProjectionPostgresOperation(t, "read incident projection state", err)
	}
	return state
}

func assertIncidentProjectionCASNewerState(t *testing.T, state incidentProjectionCASState) {
	t.Helper()
	assertIncidentProjectionCASState(t, state, 1)
}

func assertIncidentProjectionCASState(t *testing.T, state incidentProjectionCASState, wantNotificationCount int) {
	t.Helper()
	// This integration assertion owns persisted event-payload privacy. The
	// syncing boundary's TestServiceCommittedSyncProjectionConflictsPreserveResponse
	// separately proves that a projection conflict does not replace the committed
	// sync response; incidents service tests own sanitized conflict logging.
	if state.activeCount != 1 || state.newerActiveCount != 1 || state.staleActiveCount != 0 ||
		!state.summaryMatches || state.eventCount != 1 || state.newerEventCount != 1 ||
		state.staleEventCount != 0 || state.eventPayloadTokenCount != 0 ||
		state.notificationCount != wantNotificationCount || state.newerNotificationCount != wantNotificationCount {
		t.Fatalf(
			"incident projection state = active:%d newer-active:%d stale-active:%d summary:%t events:%d newer-events:%d stale-events:%d payload-token-events:%d notifications:%d newer-notifications:%d, want 1/1/0/true/1/1/0/0/%d/%d",
			state.activeCount,
			state.newerActiveCount,
			state.staleActiveCount,
			state.summaryMatches,
			state.eventCount,
			state.newerEventCount,
			state.staleEventCount,
			state.eventPayloadTokenCount,
			state.notificationCount,
			state.newerNotificationCount,
			wantNotificationCount,
			wantNotificationCount,
		)
	}
}

type incidentProjectionUnrelatedState struct {
	noteMatches       bool
	summaryUnchanged  bool
	activeCount       int
	eventCount        int
	notificationCount int
}

func updateIncidentProjectionUnrelatedField(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
	note string,
) {
	t.Helper()
	var (
		tag pgconn.CommandTag
		err error
	)
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		tag, err = db.Exec(ctx, `
			update public.monitoring_instances
			set note = $2
			where monitoring_instance_id = $1`, objectID, note)
	case incidents.ObjectTypeTarget:
		tag, err = db.Exec(ctx, `
			update public.targets
			set note = $2
			where target_id = $1`, objectID, note)
	default:
		t.Fatal("incident projection unrelated-update object type is unsupported")
	}
	if err != nil {
		failIncidentProjectionPostgresOperation(t, "update unrelated runtime object field", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatal("unrelated runtime object update affected an unexpected row count")
	}
}

func readIncidentProjectionUnrelatedState(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	objectType incidents.ObjectType,
	objectID string,
	note string,
) incidentProjectionUnrelatedState {
	t.Helper()
	var query string
	switch objectType {
	case incidents.ObjectTypeMonitoringInstance:
		query = `
			select note = $2,
			       current_health_status = $3 and current_active_incident_count = 0 and current_primary_issue_summary = '',
			       (select count(*)::int from public.active_incidents where object_type = $4 and object_id = $1),
			       (select count(*)::int from public.state_change_events where object_type = $4 and object_id = $1),
			       (select count(*)::int from public.notification_records where object_type = $4 and object_id = $1)
			from public.monitoring_instances
			where monitoring_instance_id = $1`
	case incidents.ObjectTypeTarget:
		query = `
			select note = $2,
			       current_health_status = $3 and current_active_incident_count = 0 and current_primary_issue_summary = '',
			       (select count(*)::int from public.active_incidents where object_type = $4 and object_id = $1),
			       (select count(*)::int from public.state_change_events where object_type = $4 and object_id = $1),
			       (select count(*)::int from public.notification_records where object_type = $4 and object_id = $1)
			from public.targets
			where target_id = $1`
	default:
		t.Fatal("incident projection unrelated-state object type is unsupported")
	}
	var state incidentProjectionUnrelatedState
	if err := db.QueryRow(ctx, query, objectID, note, string(incidents.SeverityNormal), string(objectType)).Scan(
		&state.noteMatches,
		&state.summaryUnchanged,
		&state.activeCount,
		&state.eventCount,
		&state.notificationCount,
	); err != nil {
		failIncidentProjectionPostgresOperation(t, "read unrelated-update incident projection state", err)
	}
	return state
}

func assertIncidentProjectionUnrelatedState(t *testing.T, state incidentProjectionUnrelatedState) {
	t.Helper()
	if !state.noteMatches || !state.summaryUnchanged || state.activeCount != 0 || state.eventCount != 0 || state.notificationCount != 0 {
		t.Fatalf(
			"unrelated-update projection state = note:%t summary:%t active:%d events:%d notifications:%d, want true/true/0/0/0",
			state.noteMatches,
			state.summaryUnchanged,
			state.activeCount,
			state.eventCount,
			state.notificationCount,
		)
	}
}

func installIncidentProjectionCASMutationBlocker(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
) {
	t.Helper()
	for _, statement := range []string{
		`create function public.houfeng_test_block_incident_projection_cas_v1()
			returns trigger
			language plpgsql
			set search_path = pg_catalog
			as $function$
			begin
				if pg_catalog.current_setting('application_name') = 'incident-projection-cas-overlap-b' then
					perform pg_catalog.pg_advisory_xact_lock(917004113);
				end if;
				return null;
			end
			$function$`,
		`create trigger houfeng_test_block_incident_projection_cas_v1
			before delete on public.active_incidents
			for each statement
			execute function public.houfeng_test_block_incident_projection_cas_v1()`,
	} {
		if _, err := fixture.db.Exec(ctx, statement); err != nil {
			failIncidentProjectionPostgresOperation(t, "install incident projection mutation blocker", err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := fixture.db.Exec(cleanupCtx, `drop trigger if exists houfeng_test_block_incident_projection_cas_v1 on public.active_incidents`); err != nil {
			t.Error("drop incident projection mutation blocker trigger")
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop function if exists public.houfeng_test_block_incident_projection_cas_v1()`); err != nil {
			t.Error("drop incident projection mutation blocker function")
		}
	})
}

type incidentProjectionQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readIncidentProjectionBackendPID(
	t *testing.T,
	ctx context.Context,
	db incidentProjectionQueryRower,
) int32 {
	t.Helper()
	var pid int32
	if err := db.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&pid); err != nil {
		failIncidentProjectionPostgresOperation(t, "read incident projection backend identity", err)
	}
	return pid
}

func waitForIncidentProjectionBackendBlock(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	blockedPID int32,
	blockerPID int32,
	failureMessage string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := db.QueryRow(ctx, `
			select $2::integer = any(pg_catalog.pg_blocking_pids($1::integer))`,
			blockedPID,
			blockerPID,
		).Scan(&blocked); err != nil {
			failIncidentProjectionPostgresOperation(t, "observe incident projection backend lock", err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(failureMessage)
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(failureMessage)
		}
	}
}

func assertIncidentProjectionMutationPending(t *testing.T, result <-chan error, failureMessage string) {
	t.Helper()
	select {
	case <-result:
		t.Fatal(failureMessage)
	default:
	}
}

func waitForIncidentProjectionMutationResult(
	t *testing.T,
	ctx context.Context,
	result <-chan error,
	failureMessage string,
) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatal(failureMessage)
		return nil
	}
}

func assertIncidentProjectionRuntimeACLContract(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
) {
	t.Helper()
	var (
		serverMajor                                                    int
		monitoringSelect, monitoringUpdate, targetSelect, targetUpdate bool
		columnACLCount                                                 int
	)
	if err := fixture.db.QueryRow(ctx, `
		select
			pg_catalog.current_setting('server_version_num')::int / 10000,
			pg_catalog.has_table_privilege($1::name, 'public.monitoring_instances', 'SELECT'),
			pg_catalog.has_table_privilege($1::name, 'public.monitoring_instances', 'UPDATE'),
			pg_catalog.has_table_privilege($1::name, 'public.targets', 'SELECT'),
			pg_catalog.has_table_privilege($1::name, 'public.targets', 'UPDATE'),
			(select count(*)::int
			 from pg_catalog.pg_attribute attribute
			 cross join lateral pg_catalog.aclexplode(attribute.attacl) acl_entry
			 where attribute.attrelid = any(array[
			     'public.monitoring_instances'::pg_catalog.regclass,
			     'public.targets'::pg_catalog.regclass
			 ])
			   and attribute.attnum > 0
			   and not attribute.attisdropped)`,
		fixture.runtime,
	).Scan(
		&serverMajor,
		&monitoringSelect,
		&monitoringUpdate,
		&targetSelect,
		&targetUpdate,
		&columnACLCount,
	); err != nil {
		t.Fatal("read incident projection runtime ACL contract")
	}
	if serverMajor != 16 {
		t.Fatalf("PostgreSQL server major = %d, want 16", serverMajor)
	}
	if !monitoringSelect || !monitoringUpdate || !targetSelect || !targetUpdate || columnACLCount != 0 {
		t.Fatalf(
			"incident projection base-table ACL = monitoring(S:%t U:%t) target(S:%t U:%t) column:%d, want true/true/true/true/0",
			monitoringSelect,
			monitoringUpdate,
			targetSelect,
			targetUpdate,
			columnACLCount,
		)
	}
}

func failIncidentProjectionPostgresOperation(t *testing.T, phase string, err error) {
	t.Helper()
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		t.Fatalf("%s PostgreSQL SQLSTATE = %s, want success", phase, postgresError.Code)
	}
	t.Fatalf("%s failed without PostgreSQL typed cause", phase)
}

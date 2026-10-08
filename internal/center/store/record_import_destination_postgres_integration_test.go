package store_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresIntegrationImportDestinationSubjectsFinishWithServerSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newImportDestinationPostgresFixture(t, ctx)
	workflow := newImportDestinationWorkflow(t, ctx, fixture.db)
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)

	vps, err := workflow.vps.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Import destination VPS",
		ProviderName:    "destination-provider",
		Region:          "destination-region",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset() error = %v", err)
	}
	owner, err := workflow.vps.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Monitoring owner VPS",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset(owner) error = %v", err)
	}
	mi, _, err := workflow.monitoring.CreateLinkedMonitoringInstance(ctx, owner.VPSID, monitoringinstances.CreateInput{
		DisplayName:     "Import destination monitoring",
		LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
		Labels:          []string{},
	}, "")
	if err != nil {
		t.Fatalf("CreateLinkedMonitoringInstance() error = %v", err)
	}
	target, err := workflow.targets.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "Import destination target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "import-destination.example.test",
		ExecutionMonitoringInstanceLabels: []string{},
		Labels:                            []string{},
		RunStatus:                         targets.RunStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateTarget() error = %v", err)
	}

	tests := []struct {
		name        string
		destination records.SubjectReference
		displayName string
		fields      map[string]string
	}{
		{
			name:        "vps",
			destination: importDestinationReference(records.SubjectKindVPS, vps.VPSID),
			displayName: vps.DisplayName,
			fields:      map[string]string{"provider": vps.ProviderName, "region": vps.Region},
		},
		{
			name:        "monitoring_instance",
			destination: importDestinationReference(records.SubjectKindMonitoringInstance, mi.MonitoringInstanceID),
			displayName: mi.DisplayName,
			fields:      map[string]string{},
		},
		{
			name:        "target",
			destination: importDestinationReference(records.SubjectKindTarget, target.TargetID),
			displayName: target.Name,
			fields:      map[string]string{"target_type": target.TargetType},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := importDestinationArchive(t, "rec_source_"+test.name, "client identity must not be persisted")
			preview, err := workflow.service.DryRun(ctx, portability.DryRunRequest{
				Actor: actor, IdempotencyKey: "destination-snapshot-" + test.name,
				Archive: archive, DestinationSubject: test.destination,
			})
			if err != nil {
				t.Fatalf("DryRun() error = %v", err)
			}
			if preview.DestinationSubject.SubjectKind != test.destination.Kind ||
				preview.DestinationSubject.SubjectID != test.destination.SourceID {
				t.Fatalf("DryRun destination = %#v, want %s/%s", preview.DestinationSubject, test.destination.Kind, test.destination.SourceID)
			}
			replay, err := workflow.service.DryRun(ctx, portability.DryRunRequest{
				Actor: actor, IdempotencyKey: "destination-snapshot-" + test.name,
				Archive: archive, DestinationSubject: test.destination,
			})
			if err != nil {
				t.Fatalf("DryRun(replay) error = %v", err)
			}
			if replay.PlanID != preview.PlanID || replay.LockVersion != preview.LockVersion ||
				len(replay.Remaps) != len(preview.Remaps) ||
				(len(replay.Remaps) == 1 && replay.Remaps[0].TargetID != preview.Remaps[0].TargetID) {
				t.Fatalf("DryRun(replay) = %#v, want stable plan/remaps from %#v", replay, preview)
			}
			applied, err := workflow.service.Apply(ctx, portability.ApplyRequest{
				Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion,
			})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if applied.JobState != store.RecordImportJobStateApplied || len(applied.RecordIDs) != 1 {
				t.Fatalf("Apply() = %#v, want one applied record", applied)
			}
			assertImportDestinationServerSnapshot(t, ctx, fixture.db, applied.RecordIDs[0], test.destination, test.displayName, test.fields)
		})
	}
}

func TestPostgresIntegrationImportDestinationClaimCASAndConcurrentReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newImportDestinationPostgresFixture(t, ctx)
	repository := store.NewPostgresRecordPortabilityRepository(fixture.db, importDestinationAdmissionGate())
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	destination := importDestinationReference(records.SubjectKindTarget, "tg_0123456789abcdef")
	archive := importDestinationArchive(t, "rec_source_cas", "CAS archive")
	digest := sha256.Sum256(archive)
	expiresAt := time.Now().UTC().Add(time.Hour)

	first, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "destination-cas-replay", ArchiveDigest: digest,
		DestinationSubject: destination, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("ClaimImportJob(first) error = %v", err)
	}
	plan, err := repository.SaveImportPlan(ctx, store.SaveRecordImportPlanInput{
		ImportJobID: first.ImportJobID, PlanDigest: sha256.Sum256([]byte("destination-cas-plan")),
		ObjectCount: 1, RemapCount: 1,
		Remaps:             []store.ImportRemap{{EntityKind: "record", SourceID: "rec_source_cas", TargetID: "rec_import_cas"}},
		Documents:          []store.ImportDocumentPlan{{SourceID: "rec_source_cas", TargetID: "rec_import_cas", Title: "CAS archive", Body: "# CAS archive\n"}},
		DestinationSubject: destination, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("SaveImportPlan() error = %v", err)
	}
	if err := repository.AdvanceImportJob(ctx, store.AdvanceRecordImportJobInput{
		ImportJobID: first.ImportJobID, LockVersion: first.LockVersion, JobState: store.RecordImportJobStatePlanned,
	}); err != nil {
		t.Fatalf("AdvanceImportJob(planned) error = %v", err)
	}

	replay, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "destination-cas-replay", ArchiveDigest: digest,
		DestinationSubject: destination, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("ClaimImportJob(replay) error = %v", err)
	}
	if replay.ImportJobID != first.ImportJobID || replay.PlanID != plan.ImportPlanID || replay.DestinationSubject != destination {
		t.Fatalf("ClaimImportJob(replay) = %#v, want job=%s plan=%s destination=%#v", replay, first.ImportJobID, plan.ImportPlanID, destination)
	}

	if _, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "destination-cas-replay", ArchiveDigest: digest,
		DestinationSubject: importDestinationReference(records.SubjectKindVPS, "vps_0123456789abcdef"), ExpiresAt: expiresAt,
	}); !errors.Is(err, store.ErrRecordImportCASConflict) {
		t.Fatalf("ClaimImportJob(different tuple) error = %v, want CAS conflict", err)
	}
	otherArchive := importDestinationArchive(t, "rec_source_cas_other", "different archive")
	otherDigest := sha256.Sum256(otherArchive)
	if _, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "destination-cas-replay", ArchiveDigest: otherDigest,
		DestinationSubject: destination, ExpiresAt: expiresAt,
	}); !errors.Is(err, store.ErrRecordImportCASConflict) {
		t.Fatalf("ClaimImportJob(different archive) error = %v, want CAS conflict", err)
	}

	concurrentArchive := importDestinationArchive(t, "rec_source_concurrent_cas", "concurrent CAS archive")
	concurrentDigest := sha256.Sum256(concurrentArchive)
	left := importDestinationReference(records.SubjectKindTarget, "tg_0123456789abcdea")
	right := importDestinationReference(records.SubjectKindVPS, "vps_0123456789abcdea")
	start := make(chan struct{})
	type claimOutcome struct {
		job store.RecordImportJob
		err error
	}
	outcomes := make(chan claimOutcome, 2)
	for _, candidate := range []records.SubjectReference{left, right} {
		candidate := candidate
		go func() {
			<-start
			job, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
				ActorID: actor.UserID, IdempotencyKey: "destination-concurrent-cas", ArchiveDigest: concurrentDigest,
				DestinationSubject: candidate, ExpiresAt: expiresAt,
			})
			outcomes <- claimOutcome{job: job, err: err}
		}()
	}
	close(start)
	var success, conflict int
	var winner store.RecordImportJob
	for range 2 {
		outcome := <-outcomes
		switch {
		case outcome.err == nil:
			success++
			winner = outcome.job
		case errors.Is(outcome.err, store.ErrRecordImportCASConflict):
			conflict++
		default:
			t.Fatalf("concurrent ClaimImportJob() = (%#v, %v), want one success and one CAS conflict", outcome.job, outcome.err)
		}
	}
	if success != 1 || conflict != 1 || winner.ImportJobID == "" {
		t.Fatalf("concurrent claims success/conflict=%d/%d winner=%#v", success, conflict, winner)
	}
}

func TestPostgresIntegrationImportDestinationLegacyNullFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newImportDestinationPostgresFixture(t, ctx)
	repository := store.NewPostgresRecordPortabilityRepository(fixture.db, importDestinationAdmissionGate())
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	destination := importDestinationReference(records.SubjectKindTarget, "tg_0123456789abcdef")
	archive := importDestinationArchive(t, "rec_source_legacy", "legacy destination")
	digest := sha256.Sum256(archive)
	job, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "destination-legacy-null", ArchiveDigest: digest,
		DestinationSubject: destination, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ClaimImportJob() error = %v", err)
	}
	plan, err := repository.SaveImportPlan(ctx, store.SaveRecordImportPlanInput{
		ImportJobID: job.ImportJobID, PlanDigest: sha256.Sum256([]byte("legacy-plan")), ObjectCount: 1, RemapCount: 1,
		Remaps:             []store.ImportRemap{{EntityKind: "record", SourceID: "rec_source_legacy", TargetID: "rec_import_legacy"}},
		Documents:          []store.ImportDocumentPlan{{SourceID: "rec_source_legacy", TargetID: "rec_import_legacy", Title: "legacy", Body: "# legacy\n"}},
		DestinationSubject: destination, ExpiresAt: job.ExpiresAt,
	})
	if err != nil {
		t.Fatalf("SaveImportPlan() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.record_import_jobs
		set destination_subject_kind = null, destination_subject_source_id = null
		where import_job_id = $1`, job.ImportJobID); err != nil {
		t.Fatalf("clear legacy destination columns: %v", err)
	}
	if _, err := repository.LoadImportJob(ctx, job.ImportJobID); !errors.Is(err, store.ErrRecordImportCASConflict) {
		t.Fatalf("LoadImportJob(legacy NULL) error = %v, want CAS conflict", err)
	}
	if _, err := repository.LoadImportPlan(ctx, plan.ImportPlanID); !errors.Is(err, store.ErrRecordImportCASConflict) {
		t.Fatalf("LoadImportPlan(legacy NULL) error = %v, want CAS conflict", err)
	}
	if _, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "destination-legacy-null", ArchiveDigest: digest,
		DestinationSubject: destination, ExpiresAt: job.ExpiresAt,
	}); !errors.Is(err, store.ErrRecordImportCASConflict) {
		t.Fatalf("ClaimImportJob(legacy NULL replay) error = %v, want CAS conflict", err)
	}
}

func TestPostgresIntegrationImportDestinationAuthorizationRevokedNoEffects(t *testing.T) {
	for _, test := range []struct {
		name       string
		remove     bool
		viewerRole bool
	}{
		{name: "source removed after dry run", remove: true},
		{name: "import capability revoked after dry run", viewerRole: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fixture := newImportDestinationPostgresFixture(t, ctx)
			workflow := newImportDestinationWorkflow(t, ctx, fixture.db)
			admin := importDestinationActor(t, recordauth.RoleProjectAdmin)
			target, err := workflow.targets.CreateTarget(ctx, targets.CreateTargetInput{
				Name:                              "revocable import target",
				TargetType:                        targets.TargetTypeService,
				Host:                              "revocable-import.example.test",
				ExecutionMonitoringInstanceLabels: []string{},
				Labels:                            []string{},
				RunStatus:                         targets.RunStatusEnabled,
			})
			if err != nil {
				t.Fatalf("CreateTarget() error = %v", err)
			}
			destination := importDestinationReference(records.SubjectKindTarget, target.TargetID)
			archive := importDestinationArchive(t, "rec_source_revocable", "revocable import")
			preview, err := workflow.service.DryRun(ctx, portability.DryRunRequest{
				Actor: admin, IdempotencyKey: "destination-revocable", Archive: archive, DestinationSubject: destination,
			})
			if err != nil {
				t.Fatalf("DryRun() error = %v", err)
			}
			if test.remove {
				if _, err := fixture.db.Exec(ctx, `delete from public.targets where target_id = $1`, target.TargetID); err != nil {
					t.Fatalf("delete destination target: %v", err)
				}
			}
			applyActor := admin
			if test.viewerRole {
				applyActor = importDestinationActor(t, recordauth.RoleViewer)
			}
			if _, err := workflow.service.Apply(ctx, portability.ApplyRequest{
				Actor: applyActor, PlanID: preview.PlanID, LockVersion: preview.LockVersion,
			}); !errors.Is(err, portability.ErrExportUnauthorized) {
				t.Fatalf("Apply() error = %v, want unauthorized", err)
			}
			targetRecordID := preview.Remaps[0].TargetID
			assertImportDestinationNoEffects(t, ctx, fixture.db, targetRecordID, sha256.Sum256(archive), preview.PlanID, admin.UserID)
		})
	}
}

func TestPostgresIntegrationImportDestinationFinalRevisionRecheckMapsAndRollsBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newImportDestinationPostgresFixture(t, ctx)
	vps := store.NewPostgresVPSAssetRepository(fixture.db)
	monitoring := store.NewPostgresMonitoringInstanceRepository(fixture.db)
	targetsRepository := store.NewPostgresTargetRepository(fixture.db)
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	target, err := targetsRepository.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "final recheck target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "final-recheck.example.test",
		ExecutionMonitoringInstanceLabels: []string{},
		Labels:                            []string{},
		RunStatus:                         targets.RunStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateTarget() error = %v", err)
	}
	destination := importDestinationReference(records.SubjectKindTarget, target.TargetID)
	subjectAdapter := newFinalRecheckPostgresSubjectAdapter(t, actor, destination, []error{
		store.ErrRecordSubjectNotFound,
		store.ErrRecordSubjectUnavailable,
		recordauth.ErrDenied,
		records.ErrInvalidResolvedSubject,
	})
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{subjectAdapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	workflow := newImportDestinationWorkflowWithSubjects(
		t, ctx, fixture.db, vps, monitoring, targetsRepository, subjects,
	)
	handler := handlers.RecordPortability(workflow.service)

	invalidQueryArchive := importDestinationArchive(t, "rec_source_invalid_query", "invalid destination query")
	invalidQuery := httptest.NewRequest(
		http.MethodPost,
		"/api/record-imports/dry-run?destination_subject_kind=target&destination_subject_id="+target.TargetID+"&unexpected=x",
		bytes.NewReader(invalidQueryArchive),
	)
	invalidQuery.Header.Set("Content-Type", "application/zip")
	invalidQuery.Header.Set("Idempotency-Key", "final-recheck-invalid-query")
	invalidQuery = invalidQuery.WithContext(sessionctx.WithActorScope(invalidQuery.Context(), actor))
	invalidQueryResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidQueryResponse, invalidQuery)
	assertImportDestinationHTTPError(t, invalidQueryResponse, http.StatusBadRequest, "invalid_request")

	tests := []struct {
		name       string
		wantStatus int
		wantCode   string
	}{
		{name: "missing", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "unavailable", wantStatus: http.StatusServiceUnavailable, wantCode: "export_unavailable"},
		{name: "denied", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "invalid resolved", wantStatus: http.StatusServiceUnavailable, wantCode: "export_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := importDestinationArchive(t, "rec_source_final_"+strings.ReplaceAll(test.name, " ", "_"), "final recheck "+test.name)
			preview, err := workflow.service.DryRun(ctx, portability.DryRunRequest{
				Actor: actor, IdempotencyKey: "final-recheck-" + strings.ReplaceAll(test.name, " ", "-"),
				Archive: archive, DestinationSubject: destination,
			})
			if err != nil {
				t.Fatalf("DryRun() error = %v", err)
			}
			applyRequest := httptest.NewRequest(
				http.MethodPost,
				"/api/record-imports/"+preview.PlanID+"/apply",
				strings.NewReader(fmt.Sprintf(`{"lock_version":%d}`, preview.LockVersion)),
			)
			applyRequest.Header.Set("Content-Type", "application/json")
			applyRequest = applyRequest.WithContext(sessionctx.WithActorScope(applyRequest.Context(), actor))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, applyRequest)
			assertImportDestinationHTTPError(t, response, test.wantStatus, test.wantCode)
			if test.wantStatus == http.StatusNotFound && strings.Contains(response.Body.String(), "record subject") {
				t.Fatalf("opaque 404 leaked subject resolution detail: %s", response.Body.String())
			}
			assertImportDestinationNoEffects(
				t, ctx, fixture.db, preview.Remaps[0].TargetID,
				sha256.Sum256(archive), preview.PlanID, actor.UserID,
			)
		})
	}
	if subjectAdapter.calls != len(tests)*3 {
		t.Fatalf("subject Resolve calls = %d, want %d dry-run/apply/final calls", subjectAdapter.calls, len(tests)*3)
	}
}

func TestPostgresIntegrationImportDestinationConcurrentApplyCommitsOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newImportDestinationPostgresFixture(t, ctx)
	workflow := newImportDestinationWorkflow(t, ctx, fixture.db)
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	target, err := workflow.targets.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "concurrent apply target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "concurrent-apply.example.test",
		ExecutionMonitoringInstanceLabels: []string{},
		Labels:                            []string{},
		RunStatus:                         targets.RunStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateTarget() error = %v", err)
	}
	archive := importDestinationArchive(t, "rec_source_concurrent_apply", "concurrent apply")
	preview, err := workflow.service.DryRun(ctx, portability.DryRunRequest{
		Actor: actor, IdempotencyKey: "destination-concurrent-apply", Archive: archive,
		DestinationSubject: importDestinationReference(records.SubjectKindTarget, target.TargetID),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	request := portability.ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion}
	start := make(chan struct{})
	type applyOutcome struct {
		result portability.ApplyResult
		err    error
	}
	outcomes := make(chan applyOutcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := workflow.service.Apply(ctx, request)
			outcomes <- applyOutcome{result: result, err: err}
		}()
	}
	close(start)
	var success, failure int
	var winner portability.ApplyResult
	for range 2 {
		outcome := <-outcomes
		if outcome.err == nil {
			success++
			winner = outcome.result
		} else {
			failure++
			if !errors.Is(outcome.err, portability.ErrImportCASConflict) && !errors.Is(outcome.err, portability.ErrImportOriginConflict) {
				t.Fatalf("concurrent Apply() error = %v, want CAS/origin conflict", outcome.err)
			}
		}
	}
	if success != 1 || failure != 1 || len(winner.RecordIDs) != 1 {
		t.Fatalf("concurrent Apply() success/failure=%d/%d winner=%#v", success, failure, winner)
	}
	if replay, err := workflow.service.Apply(ctx, request); err != nil || len(replay.RecordIDs) != 1 || replay.RecordIDs[0] != winner.RecordIDs[0] {
		t.Fatalf("Apply(replay) = %#v, %v, want winner replay", replay, err)
	}
	assertImportDestinationAppliedCounts(t, ctx, fixture.db, winner.RecordIDs[0], sha256.Sum256(archive), preview.PlanID, actor.UserID)
}

func TestPostgresIntegrationImportDestinationMultiDocumentRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newImportDestinationPostgresFixture(t, ctx)
	workflow := newImportDestinationWorkflow(t, ctx, fixture.db)
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	target, err := workflow.targets.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "multi-document target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "multi-document.example.test",
		ExecutionMonitoringInstanceLabels: []string{},
		Labels:                            []string{},
		RunStatus:                         targets.RunStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateTarget() error = %v", err)
	}
	destination := importDestinationReference(records.SubjectKindTarget, target.TargetID)
	archive := importDestinationArchive(t, "rec_source_multi_a", "first imported document", "rec_source_multi_b", "second imported document")
	preview, err := workflow.service.DryRun(ctx, portability.DryRunRequest{
		Actor: actor, IdempotencyKey: "destination-multi-document", Archive: archive, DestinationSubject: destination,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if len(preview.Remaps) != 2 {
		t.Fatalf("DryRun remaps = %#v, want two documents", preview.Remaps)
	}
	collisionID := preview.Remaps[1].TargetID
	values, err := records.ImportedRevisionValues(actor, "existing collision", "# existing collision\n")
	if err != nil {
		t.Fatalf("ImportedRevisionValues() error = %v", err)
	}
	if _, err := workflow.application.CreateRecord(ctx, records.RecordCreateRequest{
		Actor: actor, RecordID: collisionID, Values: values,
		SubjectReferences: []records.SubjectReference{destination}, IdempotencyKey: "destination-collision-seed",
	}); err != nil {
		t.Fatalf("CreateRecord(collision seed) error = %v", err)
	}

	if _, err := workflow.service.Apply(ctx, portability.ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion}); err == nil {
		t.Fatal("Apply(multi-document duplicate) error = nil, want transaction failure")
	}
	firstImportedID := preview.Remaps[0].TargetID
	assertImportDestinationNoEffects(t, ctx, fixture.db, firstImportedID, sha256.Sum256(archive), preview.PlanID, actor.UserID)
	var existingCount int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.records where record_id = $1`, collisionID).Scan(&existingCount); err != nil {
		t.Fatalf("count collision record: %v", err)
	}
	if existingCount != 1 {
		t.Fatalf("collision record count = %d, want one pre-existing record", existingCount)
	}
}

type importDestinationPostgresFixture struct {
	db *pgxpool.Pool
}

func newImportDestinationPostgresFixture(t *testing.T, ctx context.Context) importDestinationPostgresFixture {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for import destination PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for import destination PostgreSQL integration tests")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_import_destination_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := pgx.Identifier{databaseName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})
	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := storemigrate.Apply(ctx, testPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return importDestinationPostgresFixture{db: testPool}
}

type importDestinationWorkflow struct {
	db          *pgxpool.Pool
	vps         *store.PostgresVPSAssetRepository
	monitoring  *store.PostgresMonitoringInstanceRepository
	targets     *store.PostgresTargetRepository
	service     *portability.Service
	application *records.Application
}

func newImportDestinationWorkflow(t *testing.T, ctx context.Context, db *pgxpool.Pool) importDestinationWorkflow {
	t.Helper()
	vps := store.NewPostgresVPSAssetRepository(db)
	monitoring := store.NewPostgresMonitoringInstanceRepository(db)
	targetsRepository := store.NewPostgresTargetRepository(db)
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vps),
		store.NewMonitoringInstanceRecordSubjectAdapter(monitoring),
		store.NewTargetRecordSubjectAdapter(targetsRepository),
	})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	return newImportDestinationWorkflowWithSubjects(
		t, ctx, db, vps, monitoring, targetsRepository, subjects,
	)
}

func newImportDestinationWorkflowWithSubjects(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	vps *store.PostgresVPSAssetRepository,
	monitoring *store.PostgresMonitoringInstanceRepository,
	targetsRepository *store.PostgresTargetRepository,
	subjects records.SubjectAdapterRegistry,
) importDestinationWorkflow {
	t.Helper()
	gate := importDestinationAdmissionGate()
	readResolver := store.NewRecordSubjectReadResolver(subjects, nil)
	current := store.NewPostgresCurrentRecordAuthorizationSource(db, readResolver, gate)
	revisionStore, err := store.NewPostgresRecordRepository(db, gate, nil)
	if err != nil {
		t.Fatalf("NewPostgresRecordRepository() error = %v", err)
	}
	revisionService, err := records.NewRevisionService(subjects, current, revisionStore)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}
	application, err := records.NewApplication(
		importDestinationReadStub{}, revisionService, importDestinationLifecycleStub{}, importDestinationDraftStub{},
		records.ApplicationOptions{
			IdempotencyOwnerID: "import-destination-test",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     time.Hour,
			OutboxTTL:          time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}
	imports := store.NewPostgresRecordPortabilityRepository(db, gate)
	blobStore, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	service, err := portability.NewService(portability.Options{
		Enabled:     true,
		BackendKind: "local",
		Documents:   importDestinationDocumentSource{},
		Jobs:        imports,
		Imports:     imports,
		Importer:    application,
		Subjects:    subjects,
		Staging:     portability.NewLeasedBlobStore(blobStore),
		Rebuilder:   portability.NewAuthoritativeProjectionRebuilder(),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return importDestinationWorkflow{
		db: db, vps: vps, monitoring: monitoring, targets: targetsRepository,
		service: service, application: application,
	}
}

func importDestinationAdmissionGate() store.AdmissionGate {
	return store.AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil })
}

func importDestinationActor(t *testing.T, role recordauth.Role) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: role,
		ProjectID: recordauth.ProjectIDDefault, GroupIDs: []string{"rag_records"},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func importDestinationReference(kind records.SubjectKind, sourceID string) records.SubjectReference {
	return records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            kind,
		Role:            records.RelationRoleAffected,
		SourceID:        sourceID,
		Primary:         true,
	}
}

type importDestinationArchiveMember struct {
	Path           string
	Classification string
	Payload        []byte
}

func importDestinationArchive(t *testing.T, sourceIDsAndTitles ...string) []byte {
	t.Helper()
	if len(sourceIDsAndTitles) == 0 || len(sourceIDsAndTitles)%2 != 0 {
		t.Fatal("importDestinationArchive requires source ID/title pairs")
	}
	members := make([]importDestinationArchiveMember, 0, len(sourceIDsAndTitles)/2)
	for index := 0; index < len(sourceIDsAndTitles); index += 2 {
		sourceID, title := sourceIDsAndTitles[index], sourceIDsAndTitles[index+1]
		members = append(members, importDestinationArchiveMember{
			Path:           "records/" + sourceID + "/document.md",
			Classification: "markdown",
			Payload:        []byte("# " + title + "\n\nclient supplied identity marker\n"),
		})
	}
	sort.Slice(members, func(left, right int) bool { return members[left].Path < members[right].Path })
	manifest := struct {
		Format string `json:"format"`
		Files  []struct {
			Path           string `json:"path"`
			SHA256         string `json:"sha256"`
			Size           uint64 `json:"size"`
			Classification string `json:"classification"`
		} `json:"files"`
	}{Format: "houfeng-record-archive/v1"}
	manifest.Files = make([]struct {
		Path           string `json:"path"`
		SHA256         string `json:"sha256"`
		Size           uint64 `json:"size"`
		Classification string `json:"classification"`
	}, 0, len(members))
	for _, member := range members {
		digest := sha256.Sum256(member.Payload)
		manifest.Files = append(manifest.Files, struct {
			Path           string `json:"path"`
			SHA256         string `json:"sha256"`
			Size           uint64 `json:"size"`
			Classification string `json:"classification"`
		}{
			Path: member.Path, SHA256: hex.EncodeToString(digest[:]), Size: uint64(len(member.Payload)), Classification: member.Classification,
		})
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal archive manifest: %v", err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	writeMember := func(name string, payload []byte) {
		header := &zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Unix(0, 0).UTC()}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create archive member %q: %v", name, err)
		}
		if _, err := file.Write(payload); err != nil {
			t.Fatalf("write archive member %q: %v", name, err)
		}
	}
	writeMember("manifest.json", manifestJSON)
	for _, member := range members {
		writeMember(member.Path, member.Payload)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return buffer.Bytes()
}

func assertImportDestinationServerSnapshot(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	recordID string,
	destination records.SubjectReference,
	displayName string,
	extraFields map[string]string,
) {
	t.Helper()
	var revisionID string
	if err := db.QueryRow(ctx, `select current_revision_id from public.records where record_id = $1`, recordID).Scan(&revisionID); err != nil {
		t.Fatalf("read imported current revision %q: %v", recordID, err)
	}
	var (
		kind, role, sourceID string
		primary              bool
		identityJSON         []byte
		authorizationJSON    []byte
		digest               []byte
	)
	if err := db.QueryRow(ctx, `
		select subject_kind, relation_role, source_id, is_primary,
		       identity_snapshot, capture_authorization, capture_authorization_digest
		from public.record_revision_subjects
		where revision_id = $1`, revisionID).Scan(
		&kind, &role, &sourceID, &primary, &identityJSON, &authorizationJSON, &digest,
	); err != nil {
		t.Fatalf("read imported subject snapshot: %v", err)
	}
	if kind != string(destination.Kind) || role != string(records.RelationRoleAffected) || sourceID != destination.SourceID || !primary {
		t.Fatalf("stored subject tuple = %s/%s/%s primary=%t, want %s/affected/%s primary=true", kind, role, sourceID, primary, destination.Kind, destination.SourceID)
	}
	var identity map[string]string
	if err := json.Unmarshal(identityJSON, &identity); err != nil {
		t.Fatalf("decode stored identity snapshot: %v", err)
	}
	if identity["display_name"] != displayName {
		t.Fatalf("stored identity display_name = %q, want server value %q", identity["display_name"], displayName)
	}
	if strings.Contains(identity["display_name"], "client supplied") {
		t.Fatalf("stored identity retained client marker: %#v", identity)
	}
	for key, want := range extraFields {
		if identity[key] != want {
			t.Fatalf("stored identity %s = %q, want server value %q (%#v)", key, identity[key], want, identity)
		}
	}
	if len(authorizationJSON) == 0 || len(digest) != sha256.Size {
		t.Fatalf("stored authorization evidence is incomplete: json=%d digest=%d", len(authorizationJSON), len(digest))
	}
	var subjectCount int
	if err := db.QueryRow(ctx, `select count(*)::int from public.record_revision_subjects where revision_id = $1`, revisionID).Scan(&subjectCount); err != nil {
		t.Fatalf("count stored subjects: %v", err)
	}
	if subjectCount != 1 {
		t.Fatalf("stored subject count = %d, want one unique primary affected subject", subjectCount)
	}
}

func assertImportDestinationHTTPError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("HTTP status = %d, want %d: %s", response.Code, wantStatus, response.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode HTTP error response: %v (%s)", err, response.Body.String())
	}
	if payload.Code != wantCode {
		t.Fatalf("HTTP error code = %q, want %q: %s", payload.Code, wantCode, response.Body.String())
	}
}

func assertImportDestinationNoEffects(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	recordID string,
	originDigest [32]byte,
	planID string,
	actorID string,
) {
	t.Helper()
	var recordsCount, originCount int
	if err := db.QueryRow(ctx, `select count(*)::int from public.records where record_id = $1`, recordID).Scan(&recordsCount); err != nil {
		t.Fatalf("count rejected imported record: %v", err)
	}
	if err := db.QueryRow(ctx, `select count(*)::int from public.record_origins where origin_digest = $1`, originDigest[:]).Scan(&originCount); err != nil {
		t.Fatalf("count rejected import origin: %v", err)
	}
	if recordsCount != 0 || originCount != 0 {
		t.Fatalf("rejected import rows = records %d origins %d, want zero", recordsCount, originCount)
	}
	var state string
	var jobCount int
	if err := db.QueryRow(ctx, `
		select count(*)::int, coalesce(max(job_state), '')
		from public.record_import_jobs jobs
		join public.record_import_plans plans on plans.import_job_id = jobs.import_job_id
		where plans.import_plan_id = $1 and jobs.actor_id = $2`, planID, actorID).Scan(&jobCount, &state); err != nil {
		t.Fatalf("read rejected import job: %v", err)
	}
	if jobCount != 1 || state != store.RecordImportJobStatePlanned {
		t.Fatalf("rejected import job count/state = %d/%q, want 1/planned", jobCount, state)
	}
}

func assertImportDestinationAppliedCounts(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	recordID string,
	originDigest [32]byte,
	planID string,
	actorID string,
) {
	t.Helper()
	var recordsCount, originCount int
	if err := db.QueryRow(ctx, `select count(*)::int from public.records where record_id = $1`, recordID).Scan(&recordsCount); err != nil {
		t.Fatalf("count applied imported record: %v", err)
	}
	if err := db.QueryRow(ctx, `select count(*)::int from public.record_origins where origin_digest = $1`, originDigest[:]).Scan(&originCount); err != nil {
		t.Fatalf("count applied import origin: %v", err)
	}
	if recordsCount != 1 || originCount != 1 {
		t.Fatalf("applied import rows = records %d origins %d, want one each", recordsCount, originCount)
	}
	var state string
	if err := db.QueryRow(ctx, `
		select jobs.job_state
		from public.record_import_jobs jobs
		join public.record_import_plans plans on plans.import_job_id = jobs.import_job_id
		where plans.import_plan_id = $1 and jobs.actor_id = $2`, planID, actorID).Scan(&state); err != nil {
		t.Fatalf("read applied import job: %v", err)
	}
	if state != store.RecordImportJobStateApplied {
		t.Fatalf("applied import job state = %q, want applied", state)
	}
}

// The following small adapters keep the test on the real records.Application
// and RevisionService path while avoiding unrelated read/lifecycle/draft setup.
type importDestinationDocumentSource struct{}

func (importDestinationDocumentSource) ExportDocument(context.Context, records.ExportDocumentRequest) (records.ExportDocument, error) {
	return records.ExportDocument{}, portability.ErrExportUnavailable
}

type importDestinationReadStub struct{}

func (importDestinationReadStub) GetRecord(context.Context, records.RecordGetRequest) (records.Record, error) {
	return records.Record{}, records.ErrInvalidApplicationRequest
}
func (importDestinationReadStub) ListRecords(context.Context, records.RecordListRequest) (records.RecordListResult, error) {
	return records.RecordListResult{}, records.ErrInvalidApplicationRequest
}
func (importDestinationReadStub) GetRevision(context.Context, records.RecordRevisionGetRequest) (records.RecordRevision, error) {
	return records.RecordRevision{}, records.ErrInvalidApplicationRequest
}
func (importDestinationReadStub) ListRevisions(context.Context, records.RecordRevisionListRequest) ([]records.RecordRevision, error) {
	return nil, records.ErrInvalidApplicationRequest
}

type importDestinationLifecycleStub struct{}

func (importDestinationLifecycleStub) ChangeLifecycle(context.Context, records.RecordLifecycleRequest) (records.RecordLifecycleResult, error) {
	return records.RecordLifecycleResult{}, records.ErrInvalidApplicationRequest
}

type importDestinationDraftStub struct{}

func (importDestinationDraftStub) ReadDraft(context.Context, records.DraftReadRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}
func (importDestinationDraftStub) ListDrafts(context.Context, records.DraftListRequest) (records.DraftListResult, error) {
	return records.DraftListResult{}, records.ErrInvalidApplicationRequest
}
func (importDestinationDraftStub) CreateDraft(context.Context, records.DraftCreateRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}
func (importDestinationDraftStub) PatchDraft(context.Context, records.DraftPatchRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}
func (importDestinationDraftStub) DiscardDraft(context.Context, records.DraftDiscardRequest) error {
	return records.ErrInvalidApplicationRequest
}
func (importDestinationDraftStub) PreparePublish(context.Context, records.DraftPublishRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}

type finalRecheckPostgresSubjectAdapter struct {
	reference records.SubjectReference
	resolved  records.ResolvedSubject
	outcomes  []error
	calls     int
}

func newFinalRecheckPostgresSubjectAdapter(
	t *testing.T,
	actor recordauth.ActorScope,
	reference records.SubjectReference,
	outcomes []error,
) *finalRecheckPostgresSubjectAdapter {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      actor.ProjectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindTarget,
		SourceID:     reference.SourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	identity, err := records.NewSubjectIdentitySnapshot(reference.Kind, map[string]string{
		"display_name": "final recheck target",
	})
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	return &finalRecheckPostgresSubjectAdapter{
		reference: reference,
		resolved: records.ResolvedSubject{
			ProjectID:            actor.ProjectID,
			StableID:             reference.SourceID,
			IdentitySnapshot:     identity,
			LiveRoute:            "/targets/" + reference.SourceID,
			CaptureAuthorization: authorization,
		},
		outcomes: append([]error(nil), outcomes...),
	}
}

func (adapter *finalRecheckPostgresSubjectAdapter) Kind() records.SubjectKind {
	return records.SubjectKindTarget
}

func (adapter *finalRecheckPostgresSubjectAdapter) Resolve(
	_ context.Context,
	_ recordauth.ActorScope,
	reference records.SubjectReference,
) (records.ResolvedSubject, error) {
	call := adapter.calls
	adapter.calls++
	if reference != adapter.reference {
		return records.ResolvedSubject{}, records.ErrInvalidSubjectReference
	}
	if call%3 == 2 {
		return records.ResolvedSubject{}, adapter.outcomes[call/3]
	}
	return adapter.resolved, nil
}

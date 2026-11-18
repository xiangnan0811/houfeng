package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresIntegrationImportAttachmentGCFence(t *testing.T) {
	runImportAttachmentGCFence(t, attachments.BackendKindLocal)
}

func TestPostgresMinIOIntegrationImportAttachmentGCFence(t *testing.T) {
	runImportAttachmentGCFence(t, attachments.BackendKindS3)
}

func runImportAttachmentGCFence(t *testing.T, backend attachments.BackendKind) {
	t.Helper()
	for _, producer := range []string{"worker", "direct_purge"} {
		t.Run(producer, func(t *testing.T) {
			for _, scenario := range []string{"active_before_put", "committed_claim_paused_io", "completed_after_put_rejects_late_registration"} {
				t.Run(scenario, func(t *testing.T) { runImportGCClaimFirst(t, backend, producer, scenario, false) })
			}
			t.Run("fresh_observation_after_completion", func(t *testing.T) { runImportGCFresh(t, backend, producer) })
			for _, rr := range []bool{false, true} {
				name := "read_committed_default"
				if rr {
					name = "repeatable_read_default"
				}
				t.Run(name, func(t *testing.T) {
					t.Run("uncommitted_claim_invisible_at_observation", func(t *testing.T) { runImportGCClaimFirst(t, backend, producer, "uncommitted_claim", rr) })
					t.Run("import_first_waiting_gc_retains_shared_blob", func(t *testing.T) { runImportGCImportFirst(t, backend, producer, rr) })
				})
			}
		})
	}
	t.Run("duplicate_crossrecord_replay_and_unrelated_key", func(t *testing.T) { runImportGCAccounting(t, backend) })
	t.Run("failure_boundaries", func(t *testing.T) { runImportGCFailures(t, backend) })
	t.Run("three_commit_paths_default_repeatable_read", func(t *testing.T) { runImportGCCommitPaths(t, backend) })
}

func newImportGCTestFixture(t *testing.T, backend attachments.BackendKind, rr bool) (context.Context, store.ImportAttachmentGCFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	fixture := store.NewImportAttachmentGCBackendFixture(t, ctx, backend, rr)
	// Cancel blocked I/O before fixture cleanup closes its runtime pools.
	t.Cleanup(cancel)
	return ctx, fixture
}

func putImportGCObject(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, payload []byte) attachments.ObjectVersion {
	t.Helper()
	return fixture.SeedBlob(payload).Version
}

func putImportGCProducerObject(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, producer string, payload []byte) attachments.ObjectVersion {
	t.Helper()
	if fixture.Backend == attachments.BackendKindS3 && producer == "direct_purge" {
		return fixture.SeedLegacyS3Blob(payload)
	}
	return putImportGCObject(t, ctx, fixture, payload)
}

// Both paths delegate real GC, including durable completion and quota settlement.
func seedImportGCProducer(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, producer string, object attachments.ObjectVersion) func(attachments.BlobGCDeleter) (bool, error) {
	t.Helper()
	if producer == "direct_purge" {
		purge := fixture.SeedSource(object)
		return func(deleter attachments.BlobGCDeleter) (bool, error) { return true, purge(ctx, deleter) }
	}
	fixture.SeedOrphan(object)
	return func(deleter attachments.BlobGCDeleter) (bool, error) {
		worker, err := attachments.NewBlobGCWorker(fixture.Repository, deleter, attachments.BlobGCWorkerOptions{BackendKind: fixture.Backend, LocalCleanup: fixture.LocalCleanup})
		if err != nil {
			return false, err
		}
		return worker.RunOnce(ctx)
	}
}

func importGCSourceRecordID(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture) string {
	t.Helper()
	var recordID string
	if err := fixture.Runtime.QueryRow(ctx, `select record_id from public.record_attachments where attachment_id='att_gcsource'`).Scan(&recordID); err != nil || recordID == "" {
		t.Fatalf("load actual purge source record: %q %v", recordID, err)
	}
	return recordID
}

func assertImportGCSourcePurged(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, recordID string) {
	t.Helper()
	var refs, attachments int
	if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.record_revision_attachments where record_id=$1), (select count(*) from public.record_attachments where record_id=$1)`, recordID).Scan(&refs, &attachments); err != nil {
		t.Fatal(err)
	}
	if refs != 0 || attachments != 0 {
		t.Fatalf("source purge incomplete: refs=%d attachments=%d", refs, attachments)
	}
}

type importGCOutcome struct {
	result portability.ApplyResult
	err    error
}
type importGCDeleteOutcome struct {
	worked bool
	err    error
}

func startImportGCApply(ctx context.Context, service *portability.Service, request portability.ApplyRequest) <-chan importGCOutcome {
	done := make(chan importGCOutcome, 1)
	go func() { result, err := service.Apply(ctx, request); done <- importGCOutcome{result, err} }()
	return done
}

func awaitImportGCApply(t *testing.T, ctx context.Context, done <-chan importGCOutcome) importGCOutcome {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return importGCOutcome{}
	}
}

func awaitImportGCDelete(t *testing.T, ctx context.Context, done <-chan importGCDeleteOutcome) importGCDeleteOutcome {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return importGCDeleteOutcome{}
	}
}

func startImportGCDelete(run func(attachments.BlobGCDeleter) (bool, error), deleter attachments.BlobGCDeleter) <-chan importGCDeleteOutcome {
	done := make(chan importGCDeleteOutcome, 1)
	go func() { worked, err := run(deleter); done <- importGCDeleteOutcome{worked, err} }()
	return done
}

func planImportGC(t *testing.T, ctx context.Context, service *portability.Service, destination records.SubjectReference, archive []byte, key string) (portability.ImportPlanView, portability.ApplyRequest) {
	t.Helper()
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	plan, err := service.DryRun(ctx, portability.DryRunRequest{Actor: actor, IdempotencyKey: key, Archive: archive, DestinationSubject: destination})
	if err != nil {
		t.Fatal(err)
	}
	return plan, portability.ApplyRequest{Actor: actor, PlanID: plan.PlanID, LockVersion: plan.LockVersion}
}

func runImportGCClaimFirst(t *testing.T, backend attachments.BackendKind, producer, scenario string, rr bool) {
	if backend == attachments.BackendKindLocal {
		runLocalImportGCClaimFirst(t, producer, scenario, rr)
		return
	}
	ctx, fixture := newImportGCTestFixture(t, backend, rr)
	payload := []byte("import GC fence real attachment\n")
	object := putImportGCProducerObject(t, ctx, fixture, producer, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	put := newImportGCPutBarrier(fixture.Writer)
	service, destination := newImportGCService(t, ctx, fixture, put)
	archive := importGCArchive(t, payload)
	if scenario == "active_before_put" {
		archive = importGCMultiArchive(t, []byte("unprotected earlier attachment\n"), payload)
	}
	plan, request := planImportGC(t, ctx, service, destination, archive, "import-gc-claim-first")
	before := importGCSnapshot(t, ctx, fixture, plan, archive)
	deletion := newImportGCDeleteBarrier(fixture.Deleter)
	var gcDone <-chan importGCDeleteOutcome
	var applyDone <-chan importGCOutcome
	var commit *store.ImportAttachmentCommitBarrier
	if scenario == "active_before_put" {
		gcDone = startImportGCDelete(runGC, deletion)
		awaitImportGCSignal(t, ctx, deletion.entered)
		assertImportGCHistory(t, ctx, fixture, object.Key, 1, 0)
		applyDone = startImportGCApply(ctx, service, request)
	} else if scenario == "uncommitted_claim" {
		commit = fixture.BlockNextClaimCommit()
		gcDone = startImportGCDelete(runGC, deletion)
		awaitImportGCSignal(t, ctx, commit.Entered)
		assertImportGCHistory(t, ctx, fixture, object.Key, 0, 0)
		applyDone = startImportGCApply(ctx, service, request)
		close(put.release)
		if err := fixture.WaitForGCSerialization(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-applyDone:
			t.Fatalf("Apply crossed uncommitted GC lock: %+v", got)
		default:
		}
		close(commit.Release)
		awaitImportGCSignal(t, ctx, deletion.entered)
		assertImportGCHistory(t, ctx, fixture, object.Key, 1, 0)
	} else {
		applyDone = startImportGCApply(ctx, service, request)
		awaitImportGCPut(t, ctx, put, applyDone, object)
		gcDone = startImportGCDelete(runGC, deletion)
		if backend == attachments.BackendKindS3 && producer == "direct_purge" {
			gc := awaitImportGCDelete(t, ctx, gcDone)
			var conflict *pgconn.PgError
			if !errors.As(gc.err, &conflict) || conflict.Code != "55000" {
				t.Fatalf("purge ignored the live import obligation: %v", gc.err)
			}
			select {
			case <-deletion.entered:
				t.Fatal("purge reached S3 while an import obligation was active")
			default:
			}
			assertImportGCHistory(t, ctx, fixture, object.Key, 0, 0)
			fixture.RefreshHeartbeat()
			close(put.release)
			got := awaitImportGCApply(t, ctx, applyDone)
			if got.err != nil || got.result.JobState != "applied" {
				t.Fatalf("live import after refused purge = %+v, %v", got.result, got.err)
			}
			assertImportGCBytes(t, ctx, fixture, object, payload)
			return
		}
		awaitImportGCSignal(t, ctx, deletion.entered)
		assertImportGCHistory(t, ctx, fixture, object.Key, 1, 0)
		if scenario == "completed_after_put_rejects_late_registration" {
			close(deletion.release)
			gc := awaitImportGCDelete(t, ctx, gcDone)
			if gc.err != nil || !gc.worked {
				t.Fatalf("real GC = %t, %v", gc.worked, gc.err)
			}
			gcDone = nil
			if _, err := fixture.Blobs.Stat(ctx, object); !errors.Is(err, attachments.ErrBlobNotFound) {
				t.Fatalf("deleted V1 Stat = %v", err)
			}
			assertImportGCHistory(t, ctx, fixture, object.Key, 0, 1)
		}
		fixture.RefreshHeartbeat()
		close(put.release)
	}
	got := awaitImportGCApply(t, ctx, applyDone)
	if !errors.Is(got.err, portability.ErrImportCASConflict) {
		t.Fatalf("late import result=%+v err=%v, want CAS conflict", got.result, got.err)
	}
	if scenario == "active_before_put" && put.calls.Load() != 0 {
		t.Fatalf("active baseline performed %d Puts", put.calls.Load())
	}
	// Direct purge legitimately removed its source; inspect only target effects.
	after := importGCSnapshot(t, ctx, fixture, plan, archive)
	assertImportGCNoEffects(t, after)
	if gcDone != nil {
		close(deletion.release)
		gc := awaitImportGCDelete(t, ctx, gcDone)
		if gc.err != nil || !gc.worked {
			t.Fatalf("real GC = %t, %v", gc.worked, gc.err)
		}
	}
	assertImportGCHistory(t, ctx, fixture, object.Key, 0, 1)
	final := importGCSnapshot(t, ctx, fixture, plan, archive)
	wantLogical := before.logical
	if producer == "direct_purge" {
		wantLogical -= object.SizeBytes
	}
	if final.logical != wantLogical || final.physical != before.physical-object.SizeBytes || final.reserved != before.reserved {
		t.Fatalf("failed import/GC quota = %+v, before %+v", final, before)
	}
	t.Logf("Put V1=%s; producer=%s; scenario=%s; Apply conflict; target refs/attachments/origins/revisions=0; completion=1; quota settled", object.VersionID, producer, scenario)
	if scenario == "completed_after_put_rejects_late_registration" {
		t.Run("same_planned_apply_reobserves_after_conflict", func(t *testing.T) {
			fixture.RefreshHeartbeat()
			retried, err := service.Apply(ctx, request)
			if err != nil || retried.JobState != "applied" || put.calls.Load() != 2 {
				t.Fatalf("planned retry=%+v err=%v attachment Puts=%d", retried, err, put.calls.Load())
			}
			var fresh attachments.ObjectVersion
			select {
			case fresh = <-put.returned:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if backend == attachments.BackendKindS3 && fresh.VersionID == object.VersionID {
				t.Fatal("planned retry reused deleted S3 version")
			}
			assertImportGCBytes(t, ctx, fixture, fresh, payload)
			retryEffects := importGCSnapshot(t, ctx, fixture, plan, archive)
			if retryEffects.refs != 1 || retryEffects.available != 1 || retryEffects.origins != 1 || retryEffects.revisions != 1 || retryEffects.job != "applied" || retryEffects.logical != final.logical+object.SizeBytes || retryEffects.physical != final.physical+object.SizeBytes || retryEffects.reserved != final.reserved {
				t.Fatalf("planned retry effects=%+v after failure=%+v", retryEffects, final)
			}
			assertImportGCHistory(t, ctx, fixture, object.Key, 0, 1)
		})
	}
}

func runImportGCFresh(t *testing.T, backend attachments.BackendKind, producer string) {
	ctx, fixture := newImportGCTestFixture(t, backend, false)
	payload := []byte("fresh import after retained GC history\n")
	old := putImportGCProducerObject(t, ctx, fixture, producer, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, old)
	if worked, err := runGC(fixture.Deleter); err != nil || !worked {
		t.Fatalf("GC = %t, %v", worked, err)
	}
	assertImportGCHistory(t, ctx, fixture, old.Key, 0, 1)
	if _, err := fixture.Blobs.Stat(ctx, old); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("old exact version still present: %v", err)
	}
	capture := &importGCPutCapture{BlobWriter: fixture.Writer}
	service, destination := newImportGCService(t, ctx, fixture, capture)
	archive := importGCArchive(t, payload)
	plan, request := planImportGC(t, ctx, service, destination, archive, "import-gc-fresh")
	got, err := service.Apply(ctx, request)
	if err != nil || got.JobState != "applied" {
		t.Fatalf("fresh Apply = %+v, %v", got, err)
	}
	if len(capture.objects) != 1 {
		t.Fatalf("fresh Put count=%d", len(capture.objects))
	}
	fresh := capture.objects[0]
	if fresh.VersionID == old.VersionID || fresh.Key != old.Key || fresh.SHA256 != old.SHA256 || fresh.SizeBytes != old.SizeBytes {
		t.Fatalf("fresh Put did not preserve content with a new materialization: %+v / %+v", fresh, old)
	}
	assertImportGCBytes(t, ctx, fixture, fresh, payload)
	snapshot := importGCSnapshot(t, ctx, fixture, plan, archive)
	if snapshot.refs != 1 || snapshot.available != 1 || snapshot.origins != 1 || snapshot.revisions != 1 || snapshot.job != "applied" || snapshot.logical != int64(len(payload)) || snapshot.physical != int64(len(payload)) || snapshot.reserved != 0 {
		t.Fatalf("fresh import effects=%+v", snapshot)
	}
	assertImportGCHistory(t, ctx, fixture, fresh.Key, 0, 1)
}

// The participant barrier executes after real attachment registration, holding
// the production Blob lock and quota lock until the revision transaction ends.
type importGCRevisionBarrier struct {
	entered chan struct{}
	release chan struct{}
}

func (*importGCRevisionBarrier) Name() string { return "import_gc_commit_barrier" }
func (*importGCRevisionBarrier) MutationResources(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	return records.RevisionMutationResources{}, nil
}
func (barrier *importGCRevisionBarrier) ApplyRevision(ctx context.Context, _ pgx.Tx, _ records.RevisionCommitted) error {
	close(barrier.entered)
	select {
	case <-barrier.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runImportGCImportFirst(t *testing.T, backend attachments.BackendKind, producer string, rr bool) {
	ctx, fixture := newImportGCTestFixture(t, backend, rr)
	payload := []byte("shared retained by import-first commit\n")
	object := putImportGCProducerObject(t, ctx, fixture, producer, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	var sourceRecordID string
	if producer == "direct_purge" {
		sourceRecordID = importGCSourceRecordID(t, ctx, fixture)
	}
	commit := &importGCRevisionBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	service, destination := newImportGCService(t, ctx, fixture, fixture.Writer, commit)
	archive := importGCArchive(t, payload)
	plan, request := planImportGC(t, ctx, service, destination, archive, "import-gc-import-first")
	applyDone := startImportGCApply(ctx, service, request)
	awaitImportGCSignal(t, ctx, commit.entered)
	gcDone := startImportGCDelete(runGC, fixture.Deleter)
	if err := fixture.WaitForGCSerialization(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-gcDone:
		t.Fatalf("GC crossed import lock: %+v", got)
	default:
	}
	close(commit.release)
	got := awaitImportGCApply(t, ctx, applyDone)
	if got.err != nil || got.result.JobState != "applied" {
		t.Fatalf("import-first Apply = %+v, %v", got.result, got.err)
	}
	gc := awaitImportGCDelete(t, ctx, gcDone)
	if gc.err != nil || (backend == attachments.BackendKindLocal && !gc.worked) || (backend == attachments.BackendKindS3 && producer == "worker" && gc.worked) {
		t.Fatalf("GC after import = %t, %v", gc.worked, gc.err)
	}
	completed := 0
	if backend == attachments.BackendKindLocal {
		completed = 1
		assertImportGCRetained(t, ctx, fixture, object.Key)
	}
	assertImportGCHistory(t, ctx, fixture, object.Key, 0, completed)
	assertImportGCBytes(t, ctx, fixture, object, payload)
	snapshot := importGCSnapshot(t, ctx, fixture, plan, archive)
	wantLogical := object.SizeBytes
	if snapshot.refs != 1 || snapshot.available != 1 || snapshot.origins != 1 || snapshot.job != "applied" || snapshot.logical != wantLogical || snapshot.physical != object.SizeBytes || snapshot.reserved != 0 {
		t.Fatalf("retained shared import effects=%+v", snapshot)
	}
	if producer == "direct_purge" {
		assertImportGCSourcePurged(t, ctx, fixture, sourceRecordID)
	}
}

func runImportGCAccounting(t *testing.T, backend attachments.BackendKind) {
	ctx, fixture := newImportGCTestFixture(t, backend, false)
	payload := []byte("duplicate import accounting\n")
	other := putImportGCObject(t, ctx, fixture, []byte("unrelated durable GC history\n"))
	fixture.SeedOrphan(other)
	fixture.SetQuota(0, 11, other.SizeBytes, 1)
	capture := &importGCPutCapture{BlobWriter: fixture.Writer}
	// Another key's complete lifecycle occurs after this import's baseline.
	observer := &importGCObserverHook{delegate: fixture.Repository, after: func() error {
		worker, err := attachments.NewBlobGCWorker(fixture.Repository, fixture.Deleter, attachments.BlobGCWorkerOptions{BackendKind: backend, LocalCleanup: fixture.LocalCleanup})
		if err != nil {
			return err
		}
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			return err
		}
		if !worked {
			return errors.New("unrelated GC found no orphan")
		}
		return nil
	}}
	service, destination := newImportGCServiceWithObserver(t, ctx, fixture, capture, observer)
	archive := importGCMultiArchive(t, payload)
	plan, request := planImportGC(t, ctx, service, destination, archive, "import-gc-accounting")
	got, err := service.Apply(ctx, request)
	if err != nil || got.JobState != "applied" || len(got.RecordIDs) != 2 {
		t.Fatalf("duplicate Apply = %+v, %v", got, err)
	}
	snapshot := importGCSnapshot(t, ctx, fixture, plan, archive)
	if snapshot.refs != 3 || snapshot.available != 3 || snapshot.origins != 1 || snapshot.revisions != 2 || snapshot.logical != int64(len(payload))*3 || snapshot.physical != int64(len(payload)) || snapshot.reserved != 11 || snapshot.version != 4 {
		t.Fatalf("duplicate/crossrecord accounting=%+v", snapshot)
	}
	if len(capture.objects) != 3 {
		t.Fatalf("attachment Puts=%d, want 3", len(capture.objects))
	}
	for _, object := range capture.objects {
		if object != capture.objects[0] {
			t.Fatal("same content used multiple physical identities")
		}
		assertImportGCBytes(t, ctx, fixture, object, payload)
	}
	calls := len(capture.objects)
	replay, err := service.Apply(ctx, request)
	if err != nil || replay.JobState != "applied" || len(capture.objects) != calls {
		t.Fatalf("replay=%+v err=%v Puts=%d/%d", replay, err, len(capture.objects), calls)
	}
	if after := importGCSnapshot(t, ctx, fixture, plan, archive); after != snapshot {
		t.Fatalf("applied replay changed effects: %+v -> %+v", snapshot, after)
	}
	assertImportGCHistory(t, ctx, fixture, other.Key, 0, 1)
}

func runImportGCFailures(t *testing.T, backend attachments.BackendKind) {
	for _, scenario := range []string{"zero_token", "wrong_key_token", "metadata_identity", "logical_overflow", "physical_overflow", "version_overflow", "second_record_rolls_back_first", "observation_database_error", "registration_database_error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, fixture := newImportGCTestFixture(t, backend, false)
			payload := []byte("import GC atomic failure\n")
			archive := importGCArchive(t, payload)
			if scenario == "second_record_rolls_back_first" {
				archive = importGCMultiArchive(t, payload)
			}
			if scenario == "metadata_identity" {
				object := putImportGCObject(t, ctx, fixture, payload)
				metadata := object
				metadata.SizeBytes++
				fixture.SeedOrphan(metadata)
			}
			switch scenario {
			case "logical_overflow":
				fixture.SetQuota(math.MaxInt64, 9, 0, 1)
			case "physical_overflow":
				fixture.SetQuota(0, 9, math.MaxInt64, 1)
			case "version_overflow":
				fixture.SetQuota(0, 9, 0, math.MaxInt64)
			case "second_record_rolls_back_first":
				fixture.SetQuota(math.MaxInt64-int64(len(payload))*2, 9, 0, 1)
			}
			capture := &importGCPutCapture{BlobWriter: fixture.Writer}
			var observer attachments.ImportBlobGCObserver = fixture.Repository
			if scenario == "zero_token" || scenario == "wrong_key_token" {
				observer = &importGCInvalidObserver{delegate: fixture.Repository, wrongKey: scenario == "wrong_key_token"}
			}
			if scenario == "observation_database_error" {
				fixture.FailNextObservation()
			}
			var participants []records.RevisionParticipant
			witness := &importGCRevisionWitness{expectedAttachments: 2}
			if scenario == "second_record_rolls_back_first" {
				participants = append(participants, witness)
			}
			if scenario == "registration_database_error" {
				participants = append(participants, importGCDatabaseFailureParticipant{})
			}
			service, destination := newImportGCServiceWithObserver(t, ctx, fixture, capture, observer, participants...)
			plan, request := planImportGC(t, ctx, service, destination, archive, "import-gc-failure")
			before := importGCSnapshot(t, ctx, fixture, plan, archive)
			_, err := service.Apply(ctx, request)
			if err == nil || errors.Is(err, portability.ErrImportCASConflict) {
				t.Fatalf("non-GC failure changed to success/conflict: %v", err)
			}
			if scenario == "logical_overflow" || scenario == "physical_overflow" || scenario == "version_overflow" || scenario == "second_record_rolls_back_first" {
				if !errors.Is(err, attachments.ErrQuotaOverflow) {
					t.Fatalf("quota boundary = %v, want overflow", err)
				}
			}
			if scenario == "second_record_rolls_back_first" && witness.calls.Load() != 1 {
				t.Fatalf("first record did not complete registration before later rollback: calls=%d", witness.calls.Load())
			}
			if scenario == "observation_database_error" || scenario == "registration_database_error" {
				code := "25P02"
				if scenario == "registration_database_error" {
					code = "22012"
				}
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != code {
					t.Fatalf("database failure not propagated: %v", err)
				}
			}
			after := importGCSnapshot(t, ctx, fixture, plan, archive)
			assertImportGCNoEffects(t, after)
			if after.logical != before.logical || after.physical != before.physical || after.reserved != before.reserved || after.version != before.version {
				t.Fatalf("failure changed quota: %+v -> %+v", before, after)
			}
			if scenario == "observation_database_error" && len(capture.objects) != 0 {
				t.Fatal("database observation failure performed attachment Put")
			}
			if scenario != "metadata_identity" {
				var blobs int
				if err := fixture.Runtime.QueryRow(ctx, `select count(*) from public.blob_objects where blob_key=$1`, fmt.Sprintf("sha256/%x", sha256.Sum256(payload))).Scan(&blobs); err != nil {
					t.Fatal(err)
				}
				if blobs != 0 {
					t.Fatalf("failed import retained %d metadata rows", blobs)
				}
			}
		})
	}
}

// Real repository entry points run against a default-RR connection. A commit
// occurring while they wait for the Blob table must be observed after the lock.
func runImportGCCommitPaths(t *testing.T, backend attachments.BackendKind) {
	for _, path := range []string{"CommitRevision", "CommitRevisions"} {
		edges := []string{"uncommitted_claim_worker", "uncommitted_claim_direct_purge", "zero_token", "wrong_key_token", "duplicate_attachment_id", "duplicate_key_second_token"}
		if path == "CommitRevisions" {
			edges = append(edges, "second_record_failure")
		}
		for _, edge := range edges {
			name := edge
			if backend == attachments.BackendKindLocal {
				switch edge {
				case "uncommitted_claim_worker":
					name = "live_write_defers_worker"
				case "uncommitted_claim_direct_purge":
					name = "live_write_defers_direct_purge"
				}
			}
			t.Run(path+"/"+name, func(t *testing.T) {
				if (backend == attachments.BackendKindLocal && (edge == "uncommitted_claim_worker" || edge == "uncommitted_claim_direct_purge")) ||
					(backend == attachments.BackendKindS3 && edge == "uncommitted_claim_direct_purge") {
					producer := "worker"
					if edge == "uncommitted_claim_direct_purge" {
						producer = "direct_purge"
					}
					runRawImportCleanupDefers(t, backend, path, producer)
					return
				}
				ctx, fixture := newImportGCTestFixture(t, backend, true)
				payload := []byte("direct revision commit GC fence\n")
				write, allocations := newImportGCRawWriter(t, ctx, fixture, payload)
				item := write(allocations[0].RecordID, "att_gccommitone")
				object := item.Object
				var err error
				items := []attachments.ImportedAvailableAttachment{item}
				var gcDone <-chan importGCDeleteOutcome
				var barrier *store.ImportAttachmentCommitBarrier
				if edge == "uncommitted_claim_worker" || edge == "uncommitted_claim_direct_purge" {
					producer := "worker"
					if edge == "uncommitted_claim_direct_purge" {
						producer = "direct_purge"
					}
					runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
					barrier = fixture.BlockNextClaimCommit()
					gcDone = startImportGCDelete(runGC, fixture.Deleter)
					awaitImportGCSignal(t, ctx, barrier.Entered)
				}
				switch edge {
				case "zero_token":
					items[0].GCObservation = attachments.ImportBlobGCObservation{}
				case "wrong_key_token":
					items[0].GCObservation, _ = attachments.NewImportBlobGCObservation(sha256.Sum256([]byte("other")), 0)
				case "duplicate_attachment_id":
					items = append(items, item)
				case "duplicate_key_second_token":
					second := write(allocations[0].RecordID, "att_gccommittwo")
					second.GCObservation, _ = attachments.NewImportBlobGCObservation(object.SHA256, 1)
					items = append(items, second)
				}
				batches := [][]attachments.ImportedAvailableAttachment{items}
				if edge == "second_record_failure" {
					bad := write(allocations[1].RecordID, "att_gccrossrecord")
					bad.GCObservation, _ = attachments.NewImportBlobGCObservation(object.SHA256, 1)
					batches = append(batches, []attachments.ImportedAvailableAttachment{bad})
				}
				done := make(chan error, 1)
				go func() { done <- fixture.CommitImported(ctx, path, batches, allocations) }()
				if barrier != nil {
					if err := fixture.WaitForGCSerialization(ctx); err != nil {
						t.Fatal(err)
					}
					close(barrier.Release)
				}
				select {
				case err = <-done:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if err == nil {
					t.Fatal("invalid/fenced import committed")
				}
				if barrier != nil || edge == "duplicate_key_second_token" || edge == "second_record_failure" {
					if !errors.Is(err, attachments.ErrBlobGCProtected) {
						t.Fatalf("fence error=%v", err)
					}
				}
				if gcDone != nil {
					gc := awaitImportGCDelete(t, ctx, gcDone)
					if !gc.worked || gc.err != nil {
						t.Fatalf("GC=%+v", gc)
					}
				}
				var roots, refs, available int
				if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.records where record_id=any($1::text[])), (select count(*) from public.record_revision_attachments), (select count(*) from public.record_attachments)`, []string{allocations[0].RecordID, allocations[1].RecordID}).Scan(&roots, &refs, &available); err != nil {
					t.Fatal(err)
				}
				if roots != 0 || refs != 0 || available != 0 {
					t.Fatalf("failed %s left roots=%d refs=%d attachments=%d", path, roots, refs, available)
				}
				var logical, physical, reserved int64
				if err := fixture.Runtime.QueryRow(ctx, `select coalesce((select logical_bytes from public.attachment_quota_accounts where project_id='default'),0), coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0), coalesce((select reserved_bytes from public.attachment_quota_accounts where project_id='default'),0)`).Scan(&logical, &physical, &reserved); err != nil {
					t.Fatal(err)
				}
				if logical != 0 || physical != 0 || reserved != 0 {
					t.Fatalf("failed raw commit quota=%d/%d/%d", logical, physical, reserved)
				}
			})
		}
		for _, producer := range []string{"worker", "direct_purge"} {
			t.Run(path+"/import_first_"+producer, func(t *testing.T) { runImportGCRawImportFirst(t, backend, path, producer) })
		}
	}
	// CommitRevisionsFinishing is the real Application/Apply path, including its
	// import-job/origin atomicity, not a synthetic finish-row setup.
	t.Run("CommitRevisionsFinishing/uncommitted_claim", func(t *testing.T) { runImportGCClaimFirst(t, backend, "worker", "uncommitted_claim", true) })
}

func runImportGCRawImportFirst(t *testing.T, backend attachments.BackendKind, path, producer string) {
	ctx, fixture := newImportGCTestFixture(t, backend, true)
	payload := []byte("default RR raw import-first shared identity\n")
	write, allocations := newImportGCRawWriter(t, ctx, fixture, payload)
	item := write(allocations[0].RecordID, "att_gccommitone")
	object := item.Object
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	var sourceRecordID string
	if producer == "direct_purge" {
		sourceRecordID = importGCSourceRecordID(t, ctx, fixture)
	}
	var err error
	barrier := fixture.BlockNextImportCommit()
	done := make(chan error, 1)
	go func() {
		done <- fixture.CommitImported(ctx, path, [][]attachments.ImportedAvailableAttachment{{item}}, allocations)
	}()
	awaitImportGCSignal(t, ctx, barrier.Entered)
	gcDone := startImportGCDelete(runGC, fixture.Deleter)
	if err := fixture.WaitForGCSerialization(ctx); err != nil {
		t.Fatal(err)
	}
	close(barrier.Release)
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err != nil {
		t.Fatalf("%s import-first = %v", path, err)
	}
	gc := awaitImportGCDelete(t, ctx, gcDone)
	if gc.err != nil || (backend == attachments.BackendKindLocal && !gc.worked) || (backend == attachments.BackendKindS3 && producer == "worker" && gc.worked) {
		t.Fatalf("%s producer=%s GC=%+v", path, producer, gc)
	}
	if producer == "direct_purge" {
		assertImportGCSourcePurged(t, ctx, fixture, sourceRecordID)
	}
	completed := 0
	if backend == attachments.BackendKindLocal {
		completed = 1
		assertImportGCRetained(t, ctx, fixture, object.Key)
	}
	assertImportGCHistory(t, ctx, fixture, object.Key, 0, completed)
	assertImportGCBytes(t, ctx, fixture, object, payload)
	var refs, available int
	var logical, physical, reserved int64
	if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.record_revision_attachments where record_id=$1), (select count(*) from public.record_attachments where record_id=$1 and attachment_state='available'), logical_bytes, physical_bytes, reserved_bytes from public.attachment_quota_accounts where project_id='default'`, allocations[0].RecordID).Scan(&refs, &available, &logical, &physical, &reserved); err != nil {
		t.Fatal(err)
	}
	if refs != 1 || available != 1 || logical != object.SizeBytes || physical != object.SizeBytes || reserved != 0 {
		t.Fatalf("raw import-first refs=%d available=%d quota=%d/%d/%d", refs, available, logical, physical, reserved)
	}
}

type importGCObserverHook struct {
	delegate attachments.ImportBlobGCObserver
	after    func() error
}

func (observer *importGCObserverHook) ObserveImportBlobGC(ctx context.Context, digests [][sha256.Size]byte) ([]attachments.ImportBlobGCObservation, error) {
	observations, err := observer.delegate.ObserveImportBlobGC(ctx, digests)
	if err != nil {
		return nil, err
	}
	if err := observer.after(); err != nil {
		return nil, err
	}
	return observations, nil
}

type importGCInvalidObserver struct {
	delegate attachments.ImportBlobGCObserver
	wrongKey bool
}

func (observer *importGCInvalidObserver) ObserveImportBlobGC(ctx context.Context, digests [][sha256.Size]byte) ([]attachments.ImportBlobGCObservation, error) {
	observations, err := observer.delegate.ObserveImportBlobGC(ctx, digests)
	if err != nil {
		return nil, err
	}
	if observer.wrongKey {
		observations[0], err = attachments.NewImportBlobGCObservation(sha256.Sum256([]byte("wrong key")), 0)
	} else {
		observations[0] = attachments.ImportBlobGCObservation{}
	}
	return observations, err
}

type importGCDatabaseFailureParticipant struct{}

func (importGCDatabaseFailureParticipant) Name() string { return "import_gc_database_failure" }
func (importGCDatabaseFailureParticipant) MutationResources(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	return records.RevisionMutationResources{}, nil
}
func (importGCDatabaseFailureParticipant) ApplyRevision(ctx context.Context, tx pgx.Tx, committed records.RevisionCommitted) error {
	var attachmentsCount, refsCount int
	var logical, physical int64
	if err := tx.QueryRow(ctx, `select (select count(*) from public.record_attachments where record_id=$1), (select count(*) from public.record_revision_attachments where record_id=$1), logical_bytes, physical_bytes from public.attachment_quota_accounts where project_id='default'`, committed.Result.RecordID).Scan(&attachmentsCount, &refsCount, &logical, &physical); err != nil {
		return err
	}
	if attachmentsCount != 1 || refsCount != 1 || logical <= 0 || physical <= 0 {
		return fmt.Errorf("database failure did not follow real registration: attachments=%d refs=%d quota=%d/%d", attachmentsCount, refsCount, logical, physical)
	}
	_, err := tx.Exec(ctx, `select 1 / 0`)
	return err
}

type importGCRevisionWitness struct {
	expectedAttachments int
	calls               atomic.Int64
}

func (*importGCRevisionWitness) Name() string { return "import_gc_revision_witness" }
func (*importGCRevisionWitness) MutationResources(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	return records.RevisionMutationResources{}, nil
}
func (witness *importGCRevisionWitness) ApplyRevision(_ context.Context, _ pgx.Tx, committed records.RevisionCommitted) error {
	if len(committed.ImportedAttachments) != witness.expectedAttachments {
		return fmt.Errorf("unexpected first-record attachment count=%d", len(committed.ImportedAttachments))
	}
	witness.calls.Add(1)
	return nil
}

type importGCEffects struct {
	refs, available, origins, revisions  int
	job                                  string
	logical, reserved, physical, version int64
}

func importGCSnapshot(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, plan portability.ImportPlanView, archive []byte) importGCEffects {
	t.Helper()
	var targets []string
	for _, remap := range plan.Remaps {
		if remap.EntityKind == "record" {
			targets = append(targets, remap.TargetID)
		}
	}
	if len(targets) == 0 {
		t.Fatal("plan has no target record remap")
	}
	digest := sha256.Sum256(archive)
	var got importGCEffects
	if err := fixture.Runtime.QueryRow(ctx, `select
		(select count(*) from public.record_revision_attachments where record_id=any($1::text[])),
		(select count(*) from public.record_attachments where record_id=any($1::text[]) and attachment_state='available'),
		(select count(*) from public.record_origins where origin_digest=$2),
		(select count(*) from public.record_revisions where record_id=any($1::text[])),
		(select jobs.job_state from public.record_import_jobs jobs join public.record_import_plans plans on plans.import_job_id=jobs.import_job_id where plans.import_plan_id=$3),
		coalesce((select logical_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select reserved_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select quota_version from public.attachment_quota_accounts where project_id='default'),0)`, targets, digest[:], plan.PlanID).Scan(&got.refs, &got.available, &got.origins, &got.revisions, &got.job, &got.logical, &got.reserved, &got.physical, &got.version); err != nil {
		t.Fatal(err)
	}
	return got
}

func assertImportGCNoEffects(t *testing.T, got importGCEffects) {
	t.Helper()
	if got.refs != 0 || got.available != 0 || got.origins != 0 || got.revisions != 0 || got.job != "planned" {
		t.Fatalf("failed import left target effects=%+v", got)
	}
}
func assertImportGCHistory(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, key string, active, complete int) {
	t.Helper()
	var gotActive, gotComplete int
	if err := fixture.Runtime.QueryRow(ctx, `select count(*) filter(where deletion_state<>'completed'), count(*) filter(where deletion_state='completed') from public.blob_gc_deletions where blob_key=$1`, key).Scan(&gotActive, &gotComplete); err != nil {
		t.Fatal(err)
	}
	if gotActive != active || gotComplete != complete {
		t.Fatalf("history active/completed=%d/%d want %d/%d", gotActive, gotComplete, active, complete)
	}
}
func assertImportGCBytes(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, object attachments.ObjectVersion, want []byte) {
	t.Helper()
	reader, err := fixture.Blobs.Open(ctx, object, attachments.FullByteRange())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("exact object bytes=%q err=%v want=%q", got, err, want)
	}
}
func awaitImportGCSignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func awaitImportGCPut(t *testing.T, ctx context.Context, barrier *importGCPutBarrier, done <-chan importGCOutcome, want attachments.ObjectVersion) {
	t.Helper()
	select {
	case object := <-barrier.returned:
		if object != want {
			t.Fatalf("real Put=%+v want exact seeded=%+v", object, want)
		}
	case got := <-done:
		t.Fatalf("Apply did not reach real Put: %+v err=%v", got.result, got.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

type importGCPutBarrier struct {
	attachments.BlobWriter
	calls    atomic.Int64
	returned chan attachments.ObjectVersion
	release  chan struct{}
}

func newImportGCPutBarrier(blob attachments.BlobWriter) *importGCPutBarrier {
	return &importGCPutBarrier{BlobWriter: blob, returned: make(chan attachments.ObjectVersion, 1), release: make(chan struct{})}
}
func (barrier *importGCPutBarrier) Put(ctx context.Context, request attachments.WriteRequest, reader io.Reader) (attachments.WrittenBlob, error) {
	barrier.calls.Add(1)
	object, err := barrier.BlobWriter.Put(ctx, request, reader)
	if err != nil {
		return object, err
	}
	select {
	case barrier.returned <- object.Version:
	case <-ctx.Done():
		return attachments.WrittenBlob{}, ctx.Err()
	}
	select {
	case <-barrier.release:
		return object, nil
	case <-ctx.Done():
		return attachments.WrittenBlob{}, ctx.Err()
	}
}

type importGCDeleteBarrier struct {
	attachments.BlobGCDeleter
	entered chan struct{}
	release chan struct{}
}

func newImportGCDeleteBarrier(deleter attachments.BlobGCDeleter) *importGCDeleteBarrier {
	return &importGCDeleteBarrier{BlobGCDeleter: deleter, entered: make(chan struct{}), release: make(chan struct{})}
}
func (barrier *importGCDeleteBarrier) DeleteGCVersion(ctx context.Context, object attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	close(barrier.entered)
	select {
	case <-barrier.release:
		return barrier.BlobGCDeleter.DeleteGCVersion(ctx, object)
	case <-ctx.Done():
		return attachments.DeletionReceipt{}, ctx.Err()
	}
}

type importGCPutCapture struct {
	attachments.BlobWriter
	objects []attachments.ObjectVersion
}

func (capture *importGCPutCapture) Put(ctx context.Context, request attachments.WriteRequest, reader io.Reader) (attachments.WrittenBlob, error) {
	object, err := capture.BlobWriter.Put(ctx, request, reader)
	if err == nil {
		capture.objects = append(capture.objects, object.Version)
	}
	return object, err
}

func newImportGCService(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, blobs attachments.BlobWriter, participants ...records.RevisionParticipant) (*portability.Service, records.SubjectReference) {
	return newImportGCServiceWithObserver(t, ctx, fixture, blobs, fixture.Repository, participants...)
}
func newImportGCServiceWithObserver(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, blobs attachments.BlobWriter, observer attachments.ImportBlobGCObserver, participants ...records.RevisionParticipant) (*portability.Service, records.SubjectReference) {
	t.Helper()
	vpsRepository := store.NewPostgresVPSAssetRepository(fixture.Runtime)
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{store.NewVPSRecordSubjectAdapter(vpsRepository)})
	if err != nil {
		t.Fatal(err)
	}
	tombstones, err := store.NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, fixture.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	current := store.NewPostgresCurrentRecordAuthorizationSource(fixture.Runtime, store.NewRecordSubjectReadResolver(subjects, tombstones), fixture.Gate)
	allParticipants := append([]records.RevisionParticipant{store.NewRecordAttachmentRevisionParticipant()}, participants...)
	revisionStore, err := store.NewPostgresRecordRepository(fixture.Runtime, fixture.Gate, allParticipants, current, nil)
	if err != nil {
		t.Fatal(err)
	}
	birthRepository := store.NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil)
	birth, err := recordbirth.NewCoordinator(birthRepository, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := records.NewRevisionService(subjects, current, revisionStore, birth)
	if err != nil {
		t.Fatal(err)
	}
	application, err := records.NewApplication(importDestinationReadStub{}, revisions, importDestinationLifecycleStub{}, importDestinationDraftStub{}, records.ApplicationOptions{IdempotencyOwnerID: "import-gc-test", OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour, OutboxTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	imports := store.NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, tombstones, nil)
	service, err := portability.NewService(portability.Options{Enabled: true, BackendKind: string(fixture.Backend), Documents: importDestinationDocumentSource{}, Jobs: imports, Imports: imports, Importer: application, BirthCoordinator: birth, BirthAllocations: birthRepository, ImportAuthorization: current, Subjects: subjects, Staging: portability.NewLeasedBlobStore(fixture.Blobs, fixture.Writer), AttachmentBlobs: fixture.Blobs, AttachmentWriter: blobs, ImportBlobGC: observer, Rebuilder: portability.NewAuthoritativeProjectionRebuilder()})
	if err != nil {
		t.Fatal(err)
	}
	vps, err := vpsRepository.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "GC import destination", LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse})
	if err != nil {
		t.Fatal(err)
	}
	return service, importDestinationReference(records.SubjectKindVPS, vps.VPSID)
}

func importGCArchive(t *testing.T, payload []byte) []byte {
	t.Helper()
	archive, err := portability.WriteArchiveV1([]portability.ArchiveEntry{
		{Path: "records/rec_gcsource/document.md", Classification: "markdown", Payload: []byte("# GC import\n\n<!-- houfeng-ref:v1 attachment att_gcsource -->\n[attachment](houfeng-attachment:att_gcsource)\n")},
		{Path: "records/rec_gcsource/attachments/att_gcsource/fence.txt", Classification: "attachment", Payload: payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	return archive
}
func importGCMultiArchive(t *testing.T, payload []byte, laterPayload ...[]byte) []byte {
	t.Helper()
	later := payload
	if len(laterPayload) != 0 {
		later = laterPayload[0]
	}
	archive, err := portability.WriteArchiveV1([]portability.ArchiveEntry{
		{Path: "records/rec_gcfirst/document.md", Classification: "markdown", Payload: []byte("# First\n\n<!-- houfeng-ref:v1 attachment att_gcone -->\n[one](houfeng-attachment:att_gcone)\n\n<!-- houfeng-ref:v1 attachment att_gctwo -->\n[two](houfeng-attachment:att_gctwo)\n")},
		{Path: "records/rec_gcfirst/attachments/att_gcone/one.txt", Classification: "attachment", Payload: payload},
		{Path: "records/rec_gcfirst/attachments/att_gctwo/two.txt", Classification: "attachment", Payload: later},
		{Path: "records/rec_gcsecond/document.md", Classification: "markdown", Payload: []byte("# Second\n\n<!-- houfeng-ref:v1 attachment att_gcthree -->\n[three](houfeng-attachment:att_gcthree)\n")},
		{Path: "records/rec_gcsecond/attachments/att_gcthree/three.txt", Classification: "attachment", Payload: later},
	})
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

// Local managed writes register before touching bytes. Pause the observation
// or durable preparation instead of asking cleanup to pass a live Put.
func runLocalImportGCClaimFirst(t *testing.T, producer, scenario string, rr bool) {
	ctx, fixture := newImportGCTestFixture(t, attachments.BackendKindLocal, rr)
	payload := []byte("local import GC fence attachment\n")
	object := putImportGCObject(t, ctx, fixture, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	capture := &importGCPutCapture{BlobWriter: fixture.Writer}
	observed, resume := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { releaseImportGCSignal(resume) })
	var observedOnce atomic.Bool
	observer := &importGCObserverHook{delegate: fixture.Repository, after: func() error {
		if !observedOnce.CompareAndSwap(false, true) {
			return nil
		}
		close(observed)
		select {
		case <-resume:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	if scenario == "active_before_put" {
		close(resume)
	}
	service, destination := newImportGCServiceWithObserver(t, ctx, fixture, capture, observer)
	archive := importGCArchive(t, payload)
	if scenario == "active_before_put" {
		archive = importGCMultiArchive(t, []byte("unprotected earlier attachment\n"), payload)
	}
	plan, request := planImportGC(t, ctx, service, destination, archive, "local-import-gc-claim-first")
	before := importGCSnapshot(t, ctx, fixture, plan, archive)
	var barrier *store.ImportAttachmentCommitBarrier
	var gcDone <-chan importGCDeleteOutcome
	var applyDone <-chan importGCOutcome
	if scenario == "uncommitted_claim" {
		barrier = fixture.BlockLocalPreparation()
		gcDone = startImportGCDelete(runGC, fixture.Deleter)
		awaitLocalImportGCBarrier(t, ctx, barrier.Entered, gcDone)
		assertImportGCHistory(t, ctx, fixture, object.Key, 0, 0)
		applyDone = startImportGCApply(ctx, service, request)
		awaitLocalImportObservation(t, ctx, observed, applyDone)
		close(resume)
		if err := fixture.WaitForGCSerialization(ctx); err != nil {
			t.Fatal(err)
		}
		close(barrier.Release)
	} else {
		if scenario != "active_before_put" {
			applyDone = startImportGCApply(ctx, service, request)
			awaitLocalImportObservation(t, ctx, observed, applyDone)
		}
		barrier = fixture.BlockLocalPhysical()
		gcDone = startImportGCDelete(runGC, fixture.Deleter)
		awaitLocalImportGCBarrier(t, ctx, barrier.Entered, gcDone)
		assertImportGCHistory(t, ctx, fixture, object.Key, 1, 0)
		if scenario == "completed_after_put_rejects_late_registration" {
			close(barrier.Release)
			gc := awaitImportGCDelete(t, ctx, gcDone)
			if gc.err != nil || !gc.worked {
				t.Fatalf("local cleanup: %+v", gc)
			}
			gcDone = nil
			if _, err := fixture.Blobs.Stat(ctx, object); !errors.Is(err, attachments.ErrBlobNotFound) {
				t.Fatalf("old materialization remains: %v", err)
			}
		}
		if scenario == "active_before_put" {
			applyDone = startImportGCApply(ctx, service, request)
		} else {
			close(resume)
		}
	}
	got := awaitImportGCApply(t, ctx, applyDone)
	if !errors.Is(got.err, portability.ErrImportCASConflict) {
		t.Fatalf("local late import: %+v %v", got.result, got.err)
	}
	assertImportGCNoEffects(t, importGCSnapshot(t, ctx, fixture, plan, archive))
	if scenario == "active_before_put" && len(capture.objects) != 0 {
		t.Fatal("active cleanup allowed attachment writes")
	}
	if gcDone != nil {
		if scenario != "uncommitted_claim" {
			close(barrier.Release)
		}
		gc := awaitImportGCDelete(t, ctx, gcDone)
		if gc.err != nil || !gc.worked {
			t.Fatalf("local cleanup: %+v", gc)
		}
	}
	assertImportGCHistory(t, ctx, fixture, object.Key, 0, 1)
	final := importGCSnapshot(t, ctx, fixture, plan, archive)
	wantLogical := before.logical
	if producer == "direct_purge" {
		wantLogical -= object.SizeBytes
	}
	if final.logical != wantLogical || final.physical != before.physical-object.SizeBytes || final.reserved != before.reserved {
		t.Fatalf("failed local import/cleanup quota: %+v, before %+v", final, before)
	}
	if scenario == "completed_after_put_rejects_late_registration" {
		fixture.RefreshHeartbeat()
		retried, err := service.Apply(ctx, request)
		if err != nil || retried.JobState != "applied" {
			t.Fatalf("fresh observation retry: %+v %v", retried, err)
		}
		fresh := capture.objects[len(capture.objects)-1]
		if fresh.VersionID == object.VersionID {
			t.Fatal("retry reused deleted local materialization")
		}
		assertImportGCBytes(t, ctx, fixture, fresh, payload)
		after := importGCSnapshot(t, ctx, fixture, plan, archive)
		if after.refs != 1 || after.available != 1 || after.origins != 1 || after.revisions != 1 || after.logical != final.logical+object.SizeBytes || after.physical != final.physical+object.SizeBytes || after.reserved != final.reserved {
			t.Fatalf("retry effects: %+v", after)
		}
	}
}

func releaseImportGCSignal(signal chan struct{}) {
	select {
	case <-signal:
	default:
		close(signal)
	}
}

func awaitLocalImportGCBarrier(t *testing.T, ctx context.Context, entered <-chan struct{}, done <-chan importGCDeleteOutcome) {
	t.Helper()
	select {
	case <-entered:
	case got := <-done:
		t.Fatalf("cleanup exited before barrier: %+v", got)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func awaitLocalImportObservation(t *testing.T, ctx context.Context, observed <-chan struct{}, done <-chan importGCOutcome) {
	t.Helper()
	select {
	case <-observed:
	case got := <-done:
		t.Fatalf("import exited before observation: %+v", got)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func assertImportGCRetained(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, key string) {
	t.Helper()
	var retained bool
	if err := fixture.Runtime.QueryRow(ctx, `select count(*)=1 and bool_and(physical_delete_result='retained_referenced' and receipt_digest is not null) from public.blob_gc_deletions where blob_key=$1`, key).Scan(&retained); err != nil || !retained {
		t.Fatalf("shared local cleanup lacks retained receipt: %t %v", retained, err)
	}
}

func newImportGCRawWriter(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, payload []byte) (func(string, string) attachments.ImportedAvailableAttachment, []recordbirth.Allocation) {
	t.Helper()
	repository := store.NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, importDestinationWitness(t, fixture.Runtime), nil)
	actor := importDestinationActor(t, recordauth.RoleProjectAdmin)
	destination := importDestinationReference(records.SubjectKindVPS, "vps_0123456789abcdef")
	archiveDigest := sha256.Sum256(importGCArchive(t, payload))
	planDigest := sha256.Sum256([]byte("raw managed import plan"))
	expires := time.Now().UTC().Add(time.Hour)
	job, err := repository.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "raw-managed-import", ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	allocations := []recordbirth.Allocation{
		reserveImportFixture(t, ctx, fixture.Runtime, fixture.Gate, actor, job, "rec_sourceone"),
		reserveImportFixture(t, ctx, fixture.Runtime, fixture.Gate, actor, job, "rec_sourcetwo"),
	}
	_, err = repository.SaveImportPlan(ctx, store.SaveRecordImportPlanInput{
		ImportJobID: job.ImportJobID, PlanDigest: planDigest, ObjectCount: 5, RemapCount: 5,
		Actor: actor, ArchiveDigest: archiveDigest,
		Allocations: allocations,
		Remaps: []store.ImportRemap{
			{EntityKind: "record", SourceID: "rec_sourceone", TargetID: allocations[0].RecordID, BirthAllocationID: allocations[0].AllocationID},
			{EntityKind: "record", SourceID: "rec_sourcetwo", TargetID: allocations[1].RecordID, BirthAllocationID: allocations[1].AllocationID},
			{EntityKind: "attachment", SourceID: "att_sourceone", TargetID: "att_gccommitone", OwnerTargetRecordID: allocations[0].RecordID},
			{EntityKind: "attachment", SourceID: "att_sourcetwo", TargetID: "att_gccommittwo", OwnerTargetRecordID: allocations[0].RecordID},
			{EntityKind: "attachment", SourceID: "att_sourcethree", TargetID: "att_gccrossrecord", OwnerTargetRecordID: allocations[1].RecordID},
		},
		Documents:          []store.ImportDocumentPlan{{SourceID: "rec_sourceone", TargetID: allocations[0].RecordID, Title: "One", Body: "One"}, {SourceID: "rec_sourcetwo", TargetID: allocations[1].RecordID, Title: "Two", Body: "Two"}},
		DestinationSubject: destination, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AdvanceImportJob(ctx, store.AdvanceRecordImportJobInput{ImportJobID: job.ImportJobID, LockVersion: job.LockVersion, JobState: store.RecordImportJobStatePlanned}); err != nil {
		t.Fatal(err)
	}
	job, err = repository.LoadImportJob(ctx, job.ImportJobID)
	if err != nil {
		t.Fatal(err)
	}
	planHex, archiveHex := fmt.Sprintf("%x", planDigest), fmt.Sprintf("%x", archiveDigest)
	return func(recordID, attachmentID string) attachments.ImportedAvailableAttachment {
		t.Helper()
		digest := sha256.Sum256(payload)
		observations, err := fixture.Repository.ObserveImportBlobGC(ctx, [][sha256.Size]byte{digest})
		if err != nil {
			t.Fatal(err)
		}
		temporary, err := attachments.NewBlobTemporaryKey()
		if err != nil {
			t.Fatal(err)
		}
		written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(payload)), TemporaryKey: temporary}, Origin: attachments.WriteOrigin{Kind: "import_attachment", ID: job.ImportJobID, Generation: int64(job.LockVersion), PlanDigest: &planHex, ArchiveDigest: &archiveHex, TargetRecordID: &recordID, TargetAttachmentID: &attachmentID}}, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		return attachments.ImportedAvailableAttachment{AttachmentID: attachmentID, DisplayName: "fence.txt", MediaType: "text/plain", LogicalSizeBytes: int64(len(payload)), Object: written.Version, NamespaceID: written.NamespaceID, ObligationID: written.ObligationID, BackendKind: fixture.Backend, CreatedBy: actor.UserID, GCObservation: observations[0]}
	}, allocations
}

func runRawImportCleanupDefers(t *testing.T, backend attachments.BackendKind, path, producer string) {
	ctx, fixture := newImportGCTestFixture(t, backend, true)
	payload := []byte("live raw import prevents a blocking cleanup task\n")
	write, allocations := newImportGCRawWriter(t, ctx, fixture, payload)
	item := write(allocations[0].RecordID, "att_gccommitone")
	runGC := seedImportGCProducer(t, ctx, fixture, producer, item.Object)
	var sourceRecordID string
	if producer == "direct_purge" {
		sourceRecordID = importGCSourceRecordID(t, ctx, fixture)
	}
	_, err := runGC(fixture.Deleter)
	var conflict *pgconn.PgError
	if !errors.As(err, &conflict) || conflict.Code != "55000" ||
		(backend == attachments.BackendKindLocal && conflict.Message != "writable responsibility prevents local GC") {
		t.Fatalf("cleanup ignored live raw import: %v", err)
	}
	assertImportGCHistory(t, ctx, fixture, item.Object.Key, 0, 0)
	if err := fixture.CommitImported(ctx, path, [][]attachments.ImportedAvailableAttachment{{item}}, allocations); err != nil {
		t.Fatalf("cleanup refusal blocked valid raw import: %v", err)
	}
	assertImportGCBytes(t, ctx, fixture, item.Object, payload)
	if sourceRecordID != "" {
		var sourceAttachments, sourceRefs int
		if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.record_attachments where record_id=$1), (select count(*) from public.record_revision_attachments where record_id=$1)`, sourceRecordID).Scan(&sourceAttachments, &sourceRefs); err != nil || sourceAttachments != 1 || sourceRefs != 1 {
			t.Fatalf("deferred purge changed source: attachments=%d refs=%d err=%v", sourceAttachments, sourceRefs, err)
		}
	}
	var available, refs int
	if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.record_attachments where record_id=$1 and attachment_state='available'),(select count(*) from public.record_revision_attachments where record_id=$1)`, allocations[0].RecordID).Scan(&available, &refs); err != nil || available != 1 || refs != 1 {
		t.Fatalf("raw import effects: available=%d refs=%d err=%v", available, refs, err)
	}
}

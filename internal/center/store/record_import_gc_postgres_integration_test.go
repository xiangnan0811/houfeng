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
	digest := sha256.Sum256(payload)
	temporary := sha256.Sum256([]byte(t.Name()))
	object, err := fixture.Blobs.Put(ctx, attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(payload)), TemporaryKey: fmt.Sprintf("temporary/%x", temporary)}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	return object
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
		worker, err := attachments.NewBlobGCWorker(fixture.Repository, deleter, attachments.BlobGCWorkerOptions{BackendKind: fixture.Backend})
		if err != nil {
			return false, err
		}
		return worker.RunOnce(ctx)
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
	ctx, fixture := newImportGCTestFixture(t, backend, rr)
	payload := []byte("import GC fence real attachment\n")
	object := putImportGCObject(t, ctx, fixture, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	put := newImportGCPutBarrier(fixture.Blobs)
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
		awaitImportGCPut(t, ctx, put, applyDone, object)
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
	old := putImportGCObject(t, ctx, fixture, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, old)
	if worked, err := runGC(fixture.Deleter); err != nil || !worked {
		t.Fatalf("GC = %t, %v", worked, err)
	}
	assertImportGCHistory(t, ctx, fixture, old.Key, 0, 1)
	if _, err := fixture.Blobs.Stat(ctx, old); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("old exact version still present: %v", err)
	}
	capture := &importGCPutCapture{BlobStore: fixture.Blobs}
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
	if backend == attachments.BackendKindS3 && fresh.VersionID == old.VersionID {
		t.Fatal("fresh S3 Put reused deleted version")
	}
	if backend == attachments.BackendKindLocal && fresh != old {
		t.Fatalf("local digest identity changed: %+v / %+v", fresh, old)
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
	object := putImportGCObject(t, ctx, fixture, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	commit := &importGCRevisionBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	service, destination := newImportGCService(t, ctx, fixture, fixture.Blobs, commit)
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
	if gc.err != nil || (producer == "worker" && gc.worked) {
		t.Fatalf("GC after import = %t, %v", gc.worked, gc.err)
	}
	assertImportGCHistory(t, ctx, fixture, object.Key, 0, 0)
	assertImportGCBytes(t, ctx, fixture, object, payload)
	snapshot := importGCSnapshot(t, ctx, fixture, plan, archive)
	wantLogical := object.SizeBytes
	if snapshot.refs != 1 || snapshot.available != 1 || snapshot.origins != 1 || snapshot.job != "applied" || snapshot.logical != wantLogical || snapshot.physical != object.SizeBytes || snapshot.reserved != 0 {
		t.Fatalf("retained shared import effects=%+v", snapshot)
	}
	if producer == "direct_purge" {
		var sourceRefs, sourceAttachments int
		if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.record_revision_attachments where record_id='rec_gcsource'), (select count(*) from public.record_attachments where record_id='rec_gcsource')`).Scan(&sourceRefs, &sourceAttachments); err != nil {
			t.Fatal(err)
		}
		if sourceRefs != 0 || sourceAttachments != 0 {
			t.Fatalf("source purge incomplete: refs=%d attachments=%d", sourceRefs, sourceAttachments)
		}
	}
}

func runImportGCAccounting(t *testing.T, backend attachments.BackendKind) {
	ctx, fixture := newImportGCTestFixture(t, backend, false)
	payload := []byte("duplicate import accounting\n")
	other := putImportGCObject(t, ctx, fixture, []byte("unrelated durable GC history\n"))
	fixture.SeedOrphan(other)
	fixture.SetQuota(0, 11, other.SizeBytes, 1)
	capture := &importGCPutCapture{BlobStore: fixture.Blobs}
	// Another key's complete lifecycle occurs after this import's baseline.
	observer := &importGCObserverHook{delegate: fixture.Repository, after: func() error {
		worker, err := attachments.NewBlobGCWorker(fixture.Repository, fixture.Deleter, attachments.BlobGCWorkerOptions{BackendKind: backend})
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
			capture := &importGCPutCapture{BlobStore: fixture.Blobs}
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
			t.Run(path+"/"+edge, func(t *testing.T) {
				ctx, fixture := newImportGCTestFixture(t, backend, true)
				payload := []byte("direct revision commit GC fence\n")
				object := putImportGCObject(t, ctx, fixture, payload)
				observations, err := fixture.Repository.ObserveImportBlobGC(ctx, [][sha256.Size]byte{object.SHA256})
				if err != nil {
					t.Fatal(err)
				}
				item := attachments.ImportedAvailableAttachment{AttachmentID: "att_gccommitone", DisplayName: "fence.txt", MediaType: "text/plain", LogicalSizeBytes: object.SizeBytes, Object: object, BackendKind: backend, CreatedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", GCObservation: observations[0]}
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
					second := item
					second.AttachmentID = "att_gccommittwo"
					second.GCObservation, _ = attachments.NewImportBlobGCObservation(object.SHA256, 1)
					items = append(items, second)
				}
				batches := [][]attachments.ImportedAvailableAttachment{items}
				if edge == "second_record_failure" {
					bad := item
					bad.AttachmentID = "att_gccommittwo"
					bad.GCObservation, _ = attachments.NewImportBlobGCObservation(object.SHA256, 1)
					batches = append(batches, []attachments.ImportedAvailableAttachment{bad})
				}
				done := make(chan error, 1)
				go func() { done <- fixture.CommitImported(ctx, path, batches) }()
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
				if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.records where record_id like 'rec_gccommit%'), (select count(*) from public.record_revision_attachments), (select count(*) from public.record_attachments)`).Scan(&roots, &refs, &available); err != nil {
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
	object := putImportGCObject(t, ctx, fixture, payload)
	runGC := seedImportGCProducer(t, ctx, fixture, producer, object)
	observations, err := fixture.Repository.ObserveImportBlobGC(ctx, [][sha256.Size]byte{object.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	item := attachments.ImportedAvailableAttachment{AttachmentID: "att_gccommitone", DisplayName: "fence.txt", MediaType: "text/plain", LogicalSizeBytes: object.SizeBytes, Object: object, BackendKind: backend, CreatedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", GCObservation: observations[0]}
	barrier := fixture.BlockNextImportCommit()
	done := make(chan error, 1)
	go func() { done <- fixture.CommitImported(ctx, path, [][]attachments.ImportedAvailableAttachment{{item}}) }()
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
	if gc.err != nil || (producer == "worker" && gc.worked) {
		t.Fatalf("%s producer=%s GC=%+v", path, producer, gc)
	}
	assertImportGCHistory(t, ctx, fixture, object.Key, 0, 0)
	assertImportGCBytes(t, ctx, fixture, object, payload)
	var refs, available int
	var logical, physical, reserved int64
	if err := fixture.Runtime.QueryRow(ctx, `select (select count(*) from public.record_revision_attachments where record_id='rec_gccommit0'), (select count(*) from public.record_attachments where record_id='rec_gccommit0' and attachment_state='available'), logical_bytes, physical_bytes, reserved_bytes from public.attachment_quota_accounts where project_id='default'`).Scan(&refs, &available, &logical, &physical, &reserved); err != nil {
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
	attachments.BlobStore
	calls    atomic.Int64
	returned chan attachments.ObjectVersion
	release  chan struct{}
}

func newImportGCPutBarrier(blob attachments.BlobStore) *importGCPutBarrier {
	return &importGCPutBarrier{BlobStore: blob, returned: make(chan attachments.ObjectVersion, 1), release: make(chan struct{})}
}
func (barrier *importGCPutBarrier) Put(ctx context.Context, request attachments.PutRequest, reader io.Reader) (attachments.ObjectVersion, error) {
	barrier.calls.Add(1)
	object, err := barrier.BlobStore.Put(ctx, request, reader)
	if err != nil {
		return object, err
	}
	select {
	case barrier.returned <- object:
	case <-ctx.Done():
		return attachments.ObjectVersion{}, ctx.Err()
	}
	select {
	case <-barrier.release:
		return object, nil
	case <-ctx.Done():
		return attachments.ObjectVersion{}, ctx.Err()
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
	attachments.BlobStore
	objects []attachments.ObjectVersion
}

func (capture *importGCPutCapture) Put(ctx context.Context, request attachments.PutRequest, reader io.Reader) (attachments.ObjectVersion, error) {
	object, err := capture.BlobStore.Put(ctx, request, reader)
	if err == nil {
		capture.objects = append(capture.objects, object)
	}
	return object, err
}

func newImportGCService(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, blobs attachments.BlobStore, participants ...records.RevisionParticipant) (*portability.Service, records.SubjectReference) {
	return newImportGCServiceWithObserver(t, ctx, fixture, blobs, fixture.Repository, participants...)
}
func newImportGCServiceWithObserver(t *testing.T, ctx context.Context, fixture store.ImportAttachmentGCFixture, blobs attachments.BlobStore, observer attachments.ImportBlobGCObserver, participants ...records.RevisionParticipant) (*portability.Service, records.SubjectReference) {
	t.Helper()
	vpsRepository := store.NewPostgresVPSAssetRepository(fixture.Runtime)
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{store.NewVPSRecordSubjectAdapter(vpsRepository)})
	if err != nil {
		t.Fatal(err)
	}
	current := store.NewPostgresCurrentRecordAuthorizationSource(fixture.Runtime, store.NewRecordSubjectReadResolver(subjects, nil), fixture.Gate)
	allParticipants := append([]records.RevisionParticipant{store.NewRecordAttachmentRevisionParticipant()}, participants...)
	revisionStore, err := store.NewPostgresRecordRepository(fixture.Runtime, fixture.Gate, allParticipants)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := records.NewRevisionService(subjects, current, revisionStore)
	if err != nil {
		t.Fatal(err)
	}
	application, err := records.NewApplication(importDestinationReadStub{}, revisions, importDestinationLifecycleStub{}, importDestinationDraftStub{}, records.ApplicationOptions{IdempotencyOwnerID: "import-gc-test", OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour, OutboxTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	imports := store.NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate)
	staging, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := portability.NewService(portability.Options{Enabled: true, BackendKind: string(fixture.Backend), Documents: importDestinationDocumentSource{}, Jobs: imports, Imports: imports, Importer: application, Subjects: subjects, Staging: portability.NewLeasedBlobStore(staging), AttachmentBlobs: blobs, ImportBlobGC: observer, Rebuilder: portability.NewAuthoritativeProjectionRebuilder()})
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

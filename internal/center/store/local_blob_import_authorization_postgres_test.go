package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
)

type importWriteTestFixture struct {
	platform      recordPlatformPostgresFixture
	poolName      string
	repository    *PostgresLocalBlobRepository
	blob          *attachments.LocalBlobStore
	job           RecordImportJob
	plan          RecordImportPlan
	allocation    recordbirth.Allocation
	archiveDigest [32]byte
}

func reserveImportWriteFixture(t *testing.T, ctx context.Context, coordinator *recordbirth.Coordinator, actor recordauth.ActorScope, job RecordImportJob, sourceID string) recordbirth.Allocation {
	t.Helper()
	identity, err := json.Marshal(struct {
		Domain   string
		JobID    string
		SourceID string
	}{"houfeng.record-import-allocation-key.v1", job.ImportJobID, sourceID})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := json.Marshal(struct {
		Domain        string
		JobID         string
		ArchiveDigest [32]byte
		SourceID      string
		Destination   records.SubjectReference
	}{"houfeng.record-import-allocation.v1", job.ImportJobID, job.ArchiveDigest, sourceID, job.DestinationSubject})
	if err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256(identity)
	allocation, err := coordinator.Reserve(ctx, recordbirth.Request{
		ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
		Purpose: recordbirth.PurposeImport, IdempotencyKey: "import-" + hex.EncodeToString(key[:]),
		RequestFingerprint: sha256.Sum256(binding),
	})
	if err != nil {
		t.Fatal(err)
	}
	return allocation
}

func newImportWriteTestFixture(t *testing.T, ctx context.Context, planned bool) importWriteTestFixture {
	t.Helper()
	platform := newRecordsPostgresFixture(t, ctx)
	const poolName = "import-dispatch-authorization"
	pool := platform.openDirectRuntimePool(t, ctx, poolName, 2)
	witness := localBlobFixtureWitness(t, pool)
	blob, err := newPostgresLocalBlobStore(t, ctx, platform, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatal(err)
	}
	actor := storeEvidenceParticipantActor(t)
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	archiveDigest := sha256.Sum256([]byte("import-dispatch-test-archive"))
	imports := NewPostgresRecordPortabilityRepository(pool, allowRecordPlatformAdmissionGate, witness, nil)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "import-dispatch-job",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	fixture := importWriteTestFixture{platform: platform, poolName: poolName,
		repository: NewPostgresLocalBlobRepository(pool, witness, nil), blob: blob, job: job, archiveDigest: archiveDigest}
	if !planned {
		return fixture
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(pool, allowRecordPlatformAdmissionGate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_original")
	planDigest := sha256.Sum256([]byte("import-dispatch-plan"))
	plan, err := imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{
		ImportJobID: job.ImportJobID, Actor: actor, ArchiveDigest: archiveDigest, PlanDigest: planDigest,
		ObjectCount: 2, RemapCount: 2, Allocations: []recordbirth.Allocation{allocation},
		Remaps: []ImportRemap{
			{EntityKind: "record", SourceID: "rec_original", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID},
			{EntityKind: "attachment", SourceID: "att_original", TargetID: "att_importdispatch", OwnerTargetRecordID: allocation.RecordID},
		},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_original", TargetID: allocation.RecordID, Title: "Imported", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := imports.AdvanceImportJob(ctx, AdvanceRecordImportJobInput{ImportJobID: job.ImportJobID, LockVersion: job.LockVersion, JobState: RecordImportJobStatePlanned}); err != nil {
		t.Fatal(err)
	}
	job, err = imports.LoadImportJob(ctx, job.ImportJobID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.job, fixture.plan, fixture.allocation = job, plan, allocation
	return fixture
}

func (fixture importWriteTestFixture) request(content []byte, planned bool) attachments.WriteRequest {
	archive := hex.EncodeToString(fixture.archiveDigest[:])
	origin := attachments.WriteOrigin{Kind: "import_archive", ID: fixture.job.ImportJobID, Generation: int64(fixture.job.LockVersion), ArchiveDigest: &archive}
	if planned {
		plan := hex.EncodeToString(fixture.plan.PlanDigest[:])
		record, attachment := fixture.allocation.RecordID, "att_importdispatch"
		origin.Kind, origin.PlanDigest, origin.TargetRecordID, origin.TargetAttachmentID = "import_attachment", &plan, &record, &attachment
	}
	return attachments.WriteRequest{ProjectID: "default", Origin: origin,
		Content: attachments.PutRequest{ExpectedSHA256: sha256.Sum256(content), ExpectedSizeBytes: int64(len(content))}}
}

type importWriteBarrier struct {
	attachments.ManagedWriteRepository
	registration chan attachments.WriteRegistration
	dispatch     chan attachments.WriteDispatch
	release      chan struct{}
	after        bool
}

func newImportWriteBarrier(repository attachments.ManagedWriteRepository, after bool) *importWriteBarrier {
	return &importWriteBarrier{ManagedWriteRepository: repository, registration: make(chan attachments.WriteRegistration, 2),
		dispatch: make(chan attachments.WriteDispatch, 1), release: make(chan struct{}), after: after}
}

func (barrier *importWriteBarrier) RegisterWrite(ctx context.Context, registration attachments.WriteRegistration) (attachments.WriteObligation, error) {
	result, err := barrier.ManagedWriteRepository.RegisterWrite(ctx, registration)
	if err == nil {
		barrier.registration <- registration
	}
	return result, err
}

func (barrier *importWriteBarrier) DispatchWrite(ctx context.Context, dispatch attachments.WriteDispatch) (attachments.WriteObligation, error) {
	if barrier.after {
		result, err := barrier.ManagedWriteRepository.DispatchWrite(ctx, dispatch)
		if err != nil {
			return result, err
		}
		barrier.dispatch <- dispatch
		select {
		case <-barrier.release:
			return result, nil
		case <-ctx.Done():
			return attachments.WriteObligation{}, ctx.Err()
		}
	}
	barrier.dispatch <- dispatch
	select {
	case <-barrier.release:
		return barrier.ManagedWriteRepository.DispatchWrite(ctx, dispatch)
	case <-ctx.Done():
		return attachments.WriteObligation{}, ctx.Err()
	}
}

type importWriteCountingReader struct {
	reader io.Reader
	reads  atomic.Int64
}

func (reader *importWriteCountingReader) Read(buffer []byte) (int, error) {
	reader.reads.Add(1)
	return reader.reader.Read(buffer)
}

type importWriteResult struct {
	written attachments.WrittenBlob
	err     error
}

func startImportWrite(t *testing.T, ctx context.Context, fixture importWriteTestFixture, barrier *importWriteBarrier, request attachments.WriteRequest, content []byte) (<-chan importWriteResult, *importWriteCountingReader) {
	t.Helper()
	writer, err := attachments.NewManagedBlobWriter(fixture.blob, attachments.BackendKindLocal, barrier)
	if err != nil {
		t.Fatal(err)
	}
	reader := &importWriteCountingReader{reader: bytes.NewReader(content)}
	result := make(chan importWriteResult, 1)
	go func() {
		written, err := writer.Put(ctx, request, reader)
		result <- importWriteResult{written: written, err: err}
	}()
	return result, reader
}

func awaitImportWriteDispatch(t *testing.T, ctx context.Context, barrier *importWriteBarrier, result <-chan importWriteResult) (attachments.WriteRegistration, attachments.WriteDispatch) {
	t.Helper()
	var registration attachments.WriteRegistration
	select {
	case registration = <-barrier.registration:
	case got := <-result:
		t.Fatalf("write failed before registration barrier: %v", got.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case dispatch := <-barrier.dispatch:
		return registration, dispatch
	case got := <-result:
		t.Fatalf("write failed before dispatch barrier: %v", got.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return attachments.WriteRegistration{}, attachments.WriteDispatch{}
}

func assertImportWriteNotDispatched(t *testing.T, ctx context.Context, fixture importWriteTestFixture, registration attachments.WriteRegistration, reader *importWriteCountingReader) {
	t.Helper()
	var dispatched bool
	if err := fixture.platform.db.QueryRow(ctx, `select dispatched_at is not null from public.record_write_obligations where obligation_id=$1`, registration.ObligationID).Scan(&dispatched); err != nil {
		t.Fatal(err)
	}
	if dispatched || reader.reads.Load() != 0 {
		t.Fatalf("rejected write dispatched=%t reader calls=%d", dispatched, reader.reads.Load())
	}
	if _, err := os.Stat(filepath.Join(fixture.blob.Namespace().RootPath, registration.Key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected write created physical bytes: %v", err)
	}
}

func TestPostgresIntegrationImportWriteDispatchAuthorization(t *testing.T) {
	t.Run("revocation_before_dispatch_and_admitted_late_outcome", func(t *testing.T) {
		for _, scenario := range []struct {
			name           string
			after, planned bool
		}{{"before_archive", false, false}, {"before_attachment", false, true}, {"after_archive", true, false}, {"after_attachment", true, true}} {
			t.Run(scenario.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				fixture := newImportWriteTestFixture(t, ctx, scenario.planned)
				barrier := newImportWriteBarrier(fixture.repository, scenario.after)
				content := []byte("import-dispatch-test-archive")
				if scenario.planned {
					content = []byte("authorized import attachment")
				}
				result, reader := startImportWrite(t, ctx, fixture, barrier, fixture.request(content, scenario.planned), content)
				registration, dispatch := awaitImportWriteDispatch(t, ctx, barrier, result)
				if _, err := fixture.platform.db.Exec(ctx, `update public.users set disabled_at=clock_timestamp() where user_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa'`); err != nil {
					t.Fatal(err)
				}
				close(barrier.release)
				got := <-result
				if !scenario.after {
					if got.err == nil {
						t.Fatal("revoked actor dispatched a new write")
					}
					assertImportWriteNotDispatched(t, ctx, fixture, registration, reader)
					return
				}
				if got.err != nil || reader.reads.Load() == 0 {
					t.Fatalf("admitted physical write did not finish: %v, reads=%d", got.err, reader.reads.Load())
				}
				if _, err := fixture.blob.Stat(ctx, got.written.Version); err != nil {
					t.Fatalf("admitted bytes unavailable: %v", err)
				}
				observed, err := fixture.repository.ResolveWrite(ctx, dispatch.WriteIdentity)
				if err != nil || observed.Outcome != "materialized" || observed.ReturnedAt == nil {
					t.Fatalf("late outcome was lost: %+v %v", observed, err)
				}
				reads := reader.reads.Load()
				if replay, err := fixture.repository.DispatchWrite(ctx, dispatch); err != nil || replay.DispatchID == nil || *replay.DispatchID != dispatch.DispatchID {
					t.Fatalf("exact dispatch replay failed after revocation: %+v %v", replay, err)
				}
				if replay, err := fixture.repository.RegisterWrite(ctx, registration); err != nil || replay.ID != registration.ObligationID {
					t.Fatalf("exact registration replay failed after revocation: %+v %v", replay, err)
				}
				otherDispatch := dispatch
				otherDispatch.DispatchID = localBlobFixtureUUID(t)
				if _, err := fixture.repository.DispatchWrite(ctx, otherDispatch); err == nil {
					t.Fatal("conflicting dispatch acquired a second send identity")
				}
				changedRegistration := registration
				changedRegistration.Size++
				if _, err := fixture.repository.RegisterWrite(ctx, changedRegistration); err == nil {
					t.Fatal("changed registration was accepted as an exact replay")
				}
				if reader.reads.Load() != reads {
					t.Fatal("dispatch replay started a second physical send")
				}
				// A replay only returned the durable dispatch identity; it did not
				// invoke the Blob writer or manufacture another obligation.
				var count int
				if err := fixture.platform.db.QueryRow(ctx, `select count(*) from public.record_write_obligations where origin_id=$1`, fixture.job.ImportJobID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("unexpected replay responsibility count=%d, %v", count, err)
				}
				if !scenario.planned {
					if _, err := fixture.platform.db.Exec(ctx, `delete from public.record_import_jobs where import_job_id=$1`, fixture.job.ImportJobID); err != nil {
						t.Fatal(err)
					}
					cleanup, err := attachments.NewLocalCleanupCoordinator(fixture.blob, fixture.repository)
					if err != nil {
						t.Fatal(err)
					}
					worker, err := attachments.NewBlobGCWorker(NewPostgresAttachmentRepository(fixture.repository.pool), fixture.blob,
						attachments.BlobGCWorkerOptions{BackendKind: attachments.BackendKindLocal, LocalCleanup: cleanup,
							Now: func() time.Time { return time.Now().Add(48 * time.Hour) }})
					if err != nil {
						t.Fatal(err)
					}
					if worked, err := worker.RunOnce(ctx); err != nil || !worked {
						t.Fatalf("cleanup after revocation: worked=%t error=%v", worked, err)
					}
					if _, err := fixture.blob.Stat(ctx, got.written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
						t.Fatalf("late outcome cleanup left bytes: %v", err)
					}
				}
			})
		}
	})

	for _, scenario := range []string{"job", "actor", "membership", "destination", "plan", "targets"} {
		t.Run("route_drift_while_waiting_"+scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			planned := scenario == "plan" || scenario == "targets"
			fixture := newImportWriteTestFixture(t, ctx, planned)
			barrier := newImportWriteBarrier(fixture.repository, false)
			content := []byte("import-dispatch-test-archive")
			if planned {
				content = []byte("planned import attachment")
			}
			result, reader := startImportWrite(t, ctx, fixture, barrier, fixture.request(content, planned), content)
			registration, _ := awaitImportWriteDispatch(t, ctx, barrier, result)
			holder, err := fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback(ctx) }()
			if scenario == "membership" {
				_, err = holder.Exec(ctx, `select 1 from public.users where user_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa' for update`)
			} else {
				_, err = holder.Exec(ctx, `select 1 from public.record_import_jobs where import_job_id=$1 for update`, fixture.job.ImportJobID)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(barrier.release)
			waitForRecordPlatformBackendLock(t, ctx, fixture.platform.db, fixture.poolName)
			switch scenario {
			case "job":
				_, err = holder.Exec(ctx, `update public.record_import_jobs set job_state='failed',lock_version=lock_version+1 where import_job_id=$1`, fixture.job.ImportJobID)
			case "actor":
				_, err = holder.Exec(ctx, `update public.record_import_jobs set actor_id='usr_bbbbbbbbbbbbbbbbbbbbbbbb' where import_job_id=$1`, fixture.job.ImportJobID)
			case "membership":
				_, err = holder.Exec(ctx, `update public.users set disabled_at=clock_timestamp() where user_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa'`)
			case "destination":
				_, err = holder.Exec(ctx, `update public.record_import_jobs set destination_subject_source_id='vps_ffffffffffffffff' where import_job_id=$1`, fixture.job.ImportJobID)
			case "plan":
				_, err = holder.Exec(ctx, `update public.record_import_plans set plan_digest=decode(repeat('33',32),'hex') where import_job_id=$1`, fixture.job.ImportJobID)
			case "targets":
				_, err = holder.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1`, registration.ObligationID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if got := <-result; got.err == nil {
				t.Fatal("changed import route dispatched a physical write")
			}
			assertImportWriteNotDispatched(t, ctx, fixture, registration, reader)
		})
	}

	t.Run("direct_membership_insert_cannot_race_dispatch", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		fixture := newImportWriteTestFixture(t, ctx, false)
		if _, err := fixture.platform.db.Exec(ctx, `insert into public.record_access_groups(group_id,display_name) values('rag_importdispatch','Import dispatch')`); err != nil {
			t.Fatal(err)
		}
		barrier := newImportWriteBarrier(fixture.repository, false)
		content := []byte("import-dispatch-test-archive")
		result, reader := startImportWrite(t, ctx, fixture, barrier, fixture.request(content, false), content)
		registration, _ := awaitImportWriteDispatch(t, ctx, barrier, result)
		mutator, err := fixture.repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = mutator.Rollback(ctx) }()
		if _, err := mutator.Exec(ctx, `insert into public.record_access_group_members(group_id,user_id) values('rag_importdispatch','usr_aaaaaaaaaaaaaaaaaaaaaaaa')`); err != nil {
			t.Fatal(err)
		}
		close(barrier.release)
		got := waitForRecordPlatformResult(t, result, "membership-fenced import dispatch")
		if got.err == nil {
			t.Fatal("concurrent membership insert admitted a new dispatch")
		}
		assertImportWriteNotDispatched(t, ctx, fixture, registration, reader)
		if err := mutator.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("wrong_planned_attachment_owner", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		fixture := newImportWriteTestFixture(t, ctx, true)
		content := []byte("wrong planned attachment")
		request := fixture.request(content, true)
		wrong := "att_wrongowner"
		request.Origin.TargetAttachmentID = &wrong
		writer, err := attachments.NewManagedBlobWriter(fixture.blob, attachments.BackendKindLocal, fixture.repository)
		if err != nil {
			t.Fatal(err)
		}
		reader := &importWriteCountingReader{reader: bytes.NewReader(content)}
		if _, err := writer.Put(ctx, request, reader); err == nil || reader.reads.Load() != 0 {
			t.Fatalf("wrong attachment owner wrote content: %v, reads=%d", err, reader.reads.Load())
		}
	})
}

func TestPostgresIntegrationImportWriteWitnessUnavailableKeepsLateFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newSourceRetirementPGFixture(t, ctx)
	fixture.refresh()
	seedRecordsPostgresUsers(t, ctx, fixture.app)
	vps, _ := fixture.newVPS(t, ctx, "import dispatch witness")
	witness := fixture.tombstoneReader(t)
	repository := NewPostgresLocalBlobRepository(fixture.runtime, witness, nil)
	imports := NewPostgresRecordPortabilityRepository(fixture.runtime, fixture.gate, witness, nil)
	archiveDigest := sha256.Sum256([]byte("witnessed import archive"))
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: vps.VPSID, Primary: true}
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "witness-dispatch",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(archiveDigest[:])
	binding := configuredTestS3Binding(t)
	registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &digest},
		ObligationID: "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", ""), WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1,
		ProjectID: "default", BackendKind: attachments.BackendKindS3, StorageBinding: &binding, Key: "sha256/" + digest, SHA256: digest, Size: int64(len("witnessed import archive"))}
	if _, err := repository.RegisterWrite(ctx, registration); err != nil {
		t.Fatal(err)
	}
	dispatch := attachments.WriteDispatch{WriteIdentity: attachments.WriteIdentity{ObligationID: registration.ObligationID, WriterInstance: registration.WriterInstance}, DispatchID: localBlobFixtureUUID(t)}
	withoutWitness := NewPostgresLocalBlobRepository(fixture.runtime, nil, nil)
	if _, err := withoutWitness.DispatchWrite(ctx, dispatch); err == nil {
		t.Fatal("new dispatch accepted without a source witness")
	}
	newRegistration := registration
	newRegistration.ObligationID = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
	newRegistration.WriterInstance = localBlobFixtureUUID(t)
	if _, err := withoutWitness.RegisterWrite(ctx, newRegistration); err == nil {
		t.Fatal("new registration accepted without a source witness")
	}
	if replay, err := withoutWitness.RegisterWrite(ctx, registration); err != nil || replay.ID != registration.ObligationID {
		t.Fatalf("exact registration replay required a witness: %+v %v", replay, err)
	}
	if _, err := repository.DispatchWrite(ctx, dispatch); err != nil {
		t.Fatal(err)
	}
	retiredVPS, sourceKey := fixture.newVPS(t, ctx, "revoked import destination")
	retiredDestination := destination
	retiredDestination.SourceID = retiredVPS.VPSID
	retiredJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "revoked-witness-dispatch",
		ArchiveDigest: archiveDigest, DestinationSubject: retiredDestination, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	retired := registration
	retired.ID, retired.Generation = retiredJob.ImportJobID, int64(retiredJob.LockVersion)
	retired.ObligationID = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
	retired.WriterInstance = localBlobFixtureUUID(t)
	if _, err := repository.RegisterWrite(ctx, retired); err != nil {
		t.Fatal(err)
	}
	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 1)
	_, retirement := fixture.prepare(t, ctx, sourceKey, "import_dispatch_revoked", floor)
	if _, err := fixture.service.CommitBarrier(ctx, retirement); err != nil {
		t.Fatal(err)
	}
	retiredDispatch := attachments.WriteDispatch{WriteIdentity: attachments.WriteIdentity{ObligationID: retired.ObligationID, WriterInstance: retired.WriterInstance}, DispatchID: localBlobFixtureUUID(t)}
	if _, err := repository.DispatchWrite(ctx, retiredDispatch); err == nil {
		t.Fatal("source-retirement barrier allowed a new import dispatch")
	}
	dispatchCommand, err := json.Marshal(retiredDispatch)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	err = fixture.runtime.QueryRow(ctx, `select to_jsonb(o) from public.record_write_dispatch($1) o`, dispatchCommand).Scan(&raw)
	var sourceError *pgconn.PgError
	if !errors.As(err, &sourceError) || sourceError.Code != "55000" || sourceError.Message != "evidence source unavailable" {
		t.Fatalf("restricted SQL source barrier dispatch: %v", err)
	}
	blockedRegistration := retired
	blockedRegistration.ObligationID = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
	blockedRegistration.WriterInstance = localBlobFixtureUUID(t)
	blockedCommand, err := json.Marshal(blockedRegistration)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.runtime.QueryRow(ctx, `select to_jsonb(o) from public.record_write_register($1) o`, blockedCommand).Scan(&raw)
	sourceError = nil
	if !errors.As(err, &sourceError) || sourceError.Code != "55000" || sourceError.Message != "evidence source unavailable" {
		t.Fatalf("restricted SQL source barrier registration: %v", err)
	}
	if observed, err := repository.ResolveWrite(ctx, retiredDispatch.WriteIdentity); err != nil || observed.DispatchedAt != nil {
		t.Fatalf("revoked source responsibility dispatched: %+v %v", observed, err)
	}
	pending := registration
	pending.ObligationID = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
	pending.WriterInstance = localBlobFixtureUUID(t)
	if _, err := repository.RegisterWrite(ctx, pending); err != nil {
		t.Fatal(err)
	}
	pendingDispatch := attachments.WriteDispatch{WriteIdentity: attachments.WriteIdentity{ObligationID: pending.ObligationID, WriterInstance: pending.WriterInstance}, DispatchID: localBlobFixtureUUID(t)}
	// Close the real independent W2 reader after the original dispatch committed.
	fixture.witnessReaderPool.Close()
	if _, err := repository.DispatchWrite(ctx, pendingDispatch); err == nil {
		t.Fatal("registered import dispatched without the enrolled W2 reader")
	}
	if observed, err := repository.ResolveWrite(ctx, pendingDispatch.WriteIdentity); err != nil || observed.DispatchedAt != nil {
		t.Fatalf("unavailable W2 changed pending dispatch: %+v %v", observed, err)
	}
	second := registration
	second.ObligationID = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
	second.RequestSeq++
	if _, err := repository.RegisterWrite(ctx, second); err == nil {
		t.Fatal("new import write accepted without the enrolled W2 reader")
	}
	var exists bool
	if err := fixture.runtime.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations where obligation_id=$1)`, second.ObligationID).Scan(&exists); err != nil || exists {
		t.Fatalf("new obligation exists=%t, %v", exists, err)
	}
	if replay, err := repository.DispatchWrite(ctx, dispatch); err != nil || replay.DispatchID == nil || *replay.DispatchID != dispatch.DispatchID {
		t.Fatalf("committed dispatch readback unavailable: %+v %v", replay, err)
	}
	if replay, err := repository.RegisterWrite(ctx, registration); err != nil || replay.ID != registration.ObligationID {
		t.Fatalf("committed registration readback unavailable: %+v %v", replay, err)
	}
	if _, err := repository.RecordWriteReturn(ctx, attachments.WriteReturn{WriteIdentity: dispatch.WriteIdentity, DispatchID: dispatch.DispatchID,
		Outcome: "remote_unknown", Key: registration.Key, SHA256: registration.SHA256, Size: registration.Size}); err != nil {
		t.Fatalf("late unknown outcome lost after W2 outage: %v", err)
	}
	if observed, err := repository.ResolveWrite(ctx, dispatch.WriteIdentity); err != nil || observed.Outcome != "remote_unknown" || observed.ReturnedAt == nil {
		t.Fatalf("late outcome readback: %+v %v", observed, err)
	}
}

func TestPostgresIntegrationImportWriteJobBeforeBlobFence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newImportWriteTestFixture(t, ctx, false)
	jobHolder, err := fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = jobHolder.Rollback(ctx) }()
	if _, err := jobHolder.Exec(ctx, `select 1 from public.record_import_jobs where import_job_id=$1 for update`, fixture.job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	blobHolder, err := fixture.repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blobHolder.Rollback(ctx) }()
	if _, err := blobHolder.Exec(ctx, `select public.record_revision_blob_fence()`); err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(fixture.archiveDigest[:])
	binding := configuredTestS3Binding(t)
	registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: fixture.job.ImportJobID,
		Generation: int64(fixture.job.LockVersion), ArchiveDigest: &digest},
		ObligationID: "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", ""), WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1,
		ProjectID: "default", BackendKind: attachments.BackendKindS3, StorageBinding: &binding, Key: "sha256/" + digest, SHA256: digest, Size: int64(len("import-dispatch-test-archive"))}
	body, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	// Both ranks are held by other connections. The restricted command must
	// reject the earlier job lock immediately, not wait on the Blob fence first.
	commandCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	var raw []byte
	err = fixture.repository.pool.QueryRow(commandCtx, `select to_jsonb(o) from public.record_write_register($1) o`, body).Scan(&raw)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "40001" {
		t.Fatalf("contended import command = %v, want earlier-rank 40001", err)
	}
}

func TestPostgresIntegrationImportHandoffJobBeforeBlobFence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	witness := localBlobFixtureWitness(t, fixture.Runtime)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	archiveDigest := sha256.Sum256([]byte("handoff lock ordering archive"))
	destination := records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            records.SubjectKindVPS, Role: records.RelationRoleAffected,
		SourceID: testStoreRecordVPSID, Primary: true,
	}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{
		ActorID: actor.UserID, IdempotencyKey: "handoff-lock-order-job",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_handoffsource")
	planDigest := sha256.Sum256([]byte("handoff lock ordering plan"))
	const attachmentID = "att_handofflockorder"
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{
		ImportJobID: job.ImportJobID, Actor: actor, ArchiveDigest: archiveDigest, PlanDigest: planDigest,
		ObjectCount: 2, RemapCount: 2, Allocations: []recordbirth.Allocation{allocation},
		Remaps: []ImportRemap{
			{EntityKind: "record", SourceID: "rec_handoffsource", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID},
			{EntityKind: "attachment", SourceID: "att_handoffsource", TargetID: attachmentID, OwnerTargetRecordID: allocation.RecordID},
		},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_handoffsource", TargetID: allocation.RecordID, Title: "Handoff", Body: "Handoff"}},
		DestinationSubject: destination, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := imports.AdvanceImportJob(ctx, AdvanceRecordImportJobInput{
		ImportJobID: job.ImportJobID, LockVersion: job.LockVersion, JobState: RecordImportJobStatePlanned,
	}); err != nil {
		t.Fatal(err)
	}
	job, err = imports.LoadImportJob(ctx, job.ImportJobID)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("real import attachment awaiting handoff")
	contentDigest := sha256.Sum256(content)
	archiveHex, planHex := hex.EncodeToString(archiveDigest[:]), hex.EncodeToString(planDigest[:])
	recordID := allocation.RecordID
	written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{
		ProjectID: "default",
		Origin: attachments.WriteOrigin{Kind: "import_attachment", ID: job.ImportJobID, Generation: int64(job.LockVersion),
			ArchiveDigest: &archiveHex, PlanDigest: &planHex, TargetRecordID: &recordID, TargetAttachmentID: new(attachmentID)},
		Content: attachments.PutRequest{ExpectedSHA256: contentDigest, ExpectedSizeBytes: int64(len(content))},
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatalf("create actual imported target revision: %v", err)
	}
	var beforeObligation, beforeTarget string
	var awaitingHandoff bool
	readState := func() (string, string, bool, error) {
		var obligation, target string
		var pending bool
		err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(o)::text,to_jsonb(t)::text,
			o.record_id is null and o.record_epoch is null and o.outcome='materialized' and o.returned_at is not null
			and t.record_epoch is null and t.bound_at is null and t.handed_off_at is null
			and exists(select 1 from public.records r where r.record_id=t.record_id and r.current_revision_id is not null)
			from public.record_write_obligations o join public.record_write_obligation_targets t using(obligation_id)
			where o.obligation_id=$1 and t.record_id=$2 and t.attachment_id=$3`, written.ObligationID, recordID, attachmentID).Scan(&obligation, &target, &pending)
		return obligation, target, pending, err
	}
	beforeObligation, beforeTarget, awaitingHandoff, err = readState()
	if err != nil || !awaitingHandoff {
		t.Fatalf("real import obligation is not awaiting handoff: pending=%t error=%v", awaitingHandoff, err)
	}
	jobHolder, err := fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = jobHolder.Rollback(ctx) }()
	var lockedJobID string
	if err := jobHolder.QueryRow(ctx, `select import_job_id from public.record_import_jobs where import_job_id=$1 for update`, job.ImportJobID).Scan(&lockedJobID); err != nil || lockedJobID != job.ImportJobID {
		t.Fatalf("lock exact import job: job=%q error=%v", lockedJobID, err)
	}
	blobHolder, err := fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blobHolder.Rollback(ctx) }()
	if _, err := blobHolder.Exec(ctx, `select public.record_revision_blob_fence()`); err != nil {
		t.Fatal(err)
	}
	command, err := json.Marshal(struct {
		RecordID         string `json:"record_id"`
		OriginKind       string `json:"origin_kind"`
		OriginID         string `json:"origin_id"`
		OriginGeneration uint64 `json:"origin_generation"`
	}{recordID, "import_attachment", job.ImportJobID, job.LockVersion})
	if err != nil {
		t.Fatal(err)
	}
	// Holding both ranks makes reverse ordering wait on the Blob fence. The
	// correct command reaches the earlier job NOWAIT and returns 40001 instead.
	commandCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	var bound int64
	err = fixture.Runtime.QueryRow(commandCtx, `select public.record_write_bind_record($1)`, command).Scan(&bound)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "40001" || postgresError.Message != "record write handoff scope busy; restart transaction" {
		t.Fatalf("contended import handoff = %v, want job-lock 40001", err)
	}
	afterObligation, afterTarget, awaitingHandoff, err := readState()
	if err != nil || !awaitingHandoff || afterObligation != beforeObligation || afterTarget != beforeTarget {
		t.Fatalf("failed handoff changed durable responsibility: pending=%t error=%v", awaitingHandoff, err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); err != nil {
		t.Fatalf("contended handoff lost existing Blob bytes: %v", err)
	}
	if err := jobHolder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := blobHolder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// A rolled-back positive control proves the same real route is valid once
	// contention is gone, while leaving its handoff state unchanged.
	control, err := fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Rollback(ctx) }()
	if err := control.QueryRow(ctx, `select public.record_write_bind_record($1)`, command).Scan(&bound); err != nil || bound != 1 {
		t.Fatalf("uncontended handoff control: bound=%d error=%v", bound, err)
	}
	if err := control.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	afterObligation, afterTarget, awaitingHandoff, err = readState()
	if err != nil || !awaitingHandoff || afterObligation != beforeObligation || afterTarget != beforeTarget {
		t.Fatalf("rolled-back handoff control changed durable responsibility: pending=%t error=%v", awaitingHandoff, err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); err != nil {
		t.Fatalf("rolled-back handoff control lost existing Blob bytes: %v", err)
	}
}

// runV1RetainedOwnerHandoffs exercises a real local archive shared by two
// records. The source is registered and materialized by the managed writer;
// neither the survivor nor the successor is inferred from a matching key.
func runV1RetainedOwnerHandoffs(t *testing.T) {
	t.Run("attachment_only", func(t *testing.T) {
		t.Run("consumed_owner", func(t *testing.T) { v1OwnerHandoffDeduplicated(t, true, false) })
		t.Run("revoked_origin_control", func(t *testing.T) { v1OwnerHandoffDeduplicated(t, true, true) })
		t.Run("unconsumed_owner_unresolved", v1OwnerHandoffUnconsumed)
	})
	t.Run("upload_part_only", func(t *testing.T) { v1OwnerHandoffDeduplicated(t, false, false) })
	t.Run("artifact_only", v1OwnerHandoffArtifactOnly)
	t.Run("obligation_only", v1OwnerHandoffObligationOnly)
	t.Run("handoff_order_and_prerequisites", func(t *testing.T) {
		t.Run("v2", v1OwnerHandoffV2Successor)
		t.Run("ordinary_closed_source", v1OwnerHandoffOrdinary)
		t.Run("ordinary_published_export", func(t *testing.T) { v1OwnerHandoffDeduplicated(t, false, false) })
		t.Run("ordinary_open_source", v1OwnerHandoffOpenOrdinary)
		t.Run("creator_short_read", func(t *testing.T) { v1OwnerHandoffFailedCreator(t, false) })
		t.Run("creator_wrong_hash", func(t *testing.T) { v1OwnerHandoffFailedCreator(t, true) })
		t.Run("creator_after_publish_failed_return", func(t *testing.T) { v1OwnerHandoffUnconfirmedCreator(t, false) })
		t.Run("creator_after_publish_lost_return", func(t *testing.T) { v1OwnerHandoffUnconfirmedCreator(t, true) })
	})
	t.Run("last_owner_serialization", v1OwnerHandoffSharedLocal)
	t.Run("parent_route_and_early_constraint_invalidation", func(t *testing.T) {
		t.Run("parent_route", func(t *testing.T) { v1OwnerHandoffDeduplicated(t, true, false) })
		t.Run("early_constraint", v1OwnerHandoffSharedLocal)
	})
	t.Run("independent_same_version_local", v1OwnerHandoffIndependentSameVersionLocal)
	t.Run("pending_key_and_distinct_identity", v1OwnerHandoffSharedLocal)
	t.Run("absent_predecessor_replacement", func(t *testing.T) {
		t.Run("registered_absent", func(t *testing.T) { v1OwnerHandoffAbsentReplacement(t, "registered_absent") })
		t.Run("failed_absent", func(t *testing.T) { v1OwnerHandoffAbsentReplacement(t, "failed_absent") })
		t.Run("local_excluded_absent", func(t *testing.T) { v1OwnerHandoffAbsentReplacement(t, "local_excluded_absent") })
		t.Run("failed_present_fault_state", func(t *testing.T) { v1OwnerHandoffAbsentReplacement(t, "failed_present_fault_state") })
	})
}

func v1OwnerHandoffSharedLocal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	firstRecord := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 shared source owner")
	firstID := firstRecord.RecordID
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	lastID, written, content := seedV2SharedArchive(t, ctx, fixture, local, firstID)
	third := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 concurrent shared archive survivor")
	thirdDigest := sha256.Sum256([]byte(third.RecordID))
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_entity_mappings
		(import_plan_id,entity_kind,source_id,source_identity_digest,target_id)
		values('rip_v2shared','record',$1,$2,$1)`, third.RecordID, thirdDigest[:]); err != nil {
		t.Fatal(err)
	}
	makeTarget := func(id, suffix string) recorddeletion.PurgeTarget {
		t.Helper()
		record := records.RevisionCommitResult{RecordID: id}
		var lock, epoch int64
		if err := fixture.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch
			from public.records where record_id=$1`, id).Scan(&record.RevisionID, &lock, &epoch); err != nil {
			t.Fatal(err)
		}
		record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
			OperationID: "rpo_v1ownerhandoff" + suffix, ReservationID: "drs_v1ownerhandoff" + suffix,
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: id},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		return seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
	}
	first, last := makeTarget(firstID, "first"), makeTarget(lastID, "last")
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-owner-handoff", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	firstReceipt, err := worker.PurgeRecordPortability(ctx, first, surface)
	if err != nil {
		t.Fatalf("first V1 shared purge: %v", err)
	}
	var targetID, artifactFrame, obligationFrame, artifactSaved string
	var evidenceCount int
	err = fixture.db.QueryRow(ctx, `select t.target_id,jsonb_array_length(t.retained_evidence),
		jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_import_artifacts',jsonb_build_array(a.import_artifact_id),
		 jsonb_build_array(a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,
		 to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),a.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex'),a.obligation_id),
		 jsonb_build_array(a.import_job_id,a.artifact_role,jsonb_build_array(j.import_job_id,j.project_id)))::text,
		jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_write_obligations',jsonb_build_array(o.obligation_id),
		 jsonb_build_array(o.backend_kind,o.canonical_key,o.object_version,encode(o.sha256_digest,'hex'),o.size_bytes,
		 to_char(o.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex')),
		 jsonb_build_array(o.project_id,o.origin_kind,o.origin_id,o.origin_generation,o.record_id,o.record_epoch,o.publication_id))::text,
		(select e.value->>2 from jsonb_array_elements(t.retained_evidence) e(value) where e.value->>0='import_artifact' and e.value->>1=a.import_artifact_id)
		from public.record_portability_v1_targets t
		join public.record_write_obligations o on o.obligation_id=$2
		join public.record_import_artifacts a on a.obligation_id=o.obligation_id
		join public.record_import_jobs j on j.import_job_id=a.import_job_id
		where t.operation_id=$1 and t.disposition='retained_shared' and a.import_artifact_id='ria_v2shared'`,
		first.Operation.OperationID, written.ObligationID).Scan(&targetID, &evidenceCount, &artifactFrame, &obligationFrame, &artifactSaved)
	if err != nil {
		t.Fatalf("read live shared owner and parent identities: %v", err)
	}
	if evidenceCount < 1 {
		t.Fatal("shared archive has no retained owner evidence")
	}
	artifactHash, obligationHash := sha256.Sum256([]byte(artifactFrame)), sha256.Sum256([]byte(obligationFrame))
	if artifactSaved != hex.EncodeToString(artifactHash[:]) {
		t.Fatalf("retained artifact digest mismatch: %s", artifactSaved)
	}
	obligationDigest := hex.EncodeToString(obligationHash[:])
	var artifactValid, artifactWrong, obligationValid, obligationWrong bool
	err = fixture.db.QueryRow(ctx, `select
		record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'import_artifact','ria_v2shared',decode($3,'hex')),
		record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'import_artifact','ria_v2shared',decode(repeat('ff',32),'hex')),
		record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'write_obligation',$4,decode($5,'hex')),
		record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'write_obligation',$4,decode(repeat('ff',32),'hex'))`,
		first.Operation.OperationID, targetID, artifactSaved, written.ObligationID, obligationDigest).
		Scan(&artifactValid, &artifactWrong, &obligationValid, &obligationWrong)
	if err != nil || !artifactValid || artifactWrong || !obligationValid || obligationWrong {
		t.Fatalf("live OLD proof artifact=%v/%v obligation=%v/%v: %v", artifactValid, artifactWrong, obligationValid, obligationWrong, err)
	}
	t.Run("same_statement_last_owners", func(t *testing.T) {
		tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp()
			where obligation_id=$1 and record_id=any($2::text[])`, written.ObligationID, []string{lastID, third.RecordID}); err != nil {
			t.Fatal(err)
		}
		result, err := tx.Exec(ctx, `delete from public.record_import_entity_mappings
			where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=any($1::text[])`, []string{lastID, third.RecordID})
		if err != nil {
			t.Fatalf("remove both survivor mappings: %v", err)
		}
		if result.RowsAffected() != 2 {
			t.Fatalf("removed %d survivor mappings, want 2", result.RowsAffected())
		}
		_, err = tx.Exec(ctx, `set constraints all immediate`)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "55000" {
			t.Fatalf("last owners without handoff = %v, want 55000", err)
		}
	})
	t.Run("concurrent_last_owners", func(t *testing.T) {
		firstTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = firstTx.Rollback(context.Background()) }()
		secondTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = secondTx.Rollback(context.Background()) }()
		var firstPID, secondPID int32
		if err := firstTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&firstPID); err != nil {
			t.Fatal(err)
		}
		if err := secondTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&secondPID); err != nil {
			t.Fatal(err)
		}
		if _, err := firstTx.Exec(ctx, `select 1 from public.record_import_entity_mappings
			where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=$1 for update`, lastID); err != nil {
			t.Fatal(err)
		}
		if _, err := firstTx.Exec(ctx, `select 1 from public.record_write_obligation_targets
			where obligation_id=$1 and record_id=$2 for update`, written.ObligationID, lastID); err != nil {
			t.Fatal(err)
		}
		if _, err := firstTx.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp()
			where obligation_id=$1 and record_id=$2`, written.ObligationID, third.RecordID); err != nil {
			t.Fatal(err)
		}
		result, err := firstTx.Exec(ctx, `delete from public.record_import_entity_mappings
			where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=$1`, third.RecordID)
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("remove first survivor: %v", err)
		}
		resultCh := make(chan error, 1)
		go func() {
			_, deleteErr := secondTx.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp()
				where obligation_id=$1 and record_id=$2`, written.ObligationID, lastID)
			if deleteErr == nil {
				_, deleteErr = secondTx.Exec(ctx, `delete from public.record_import_entity_mappings
					where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=$1`, lastID)
			}
			if deleteErr == nil {
				deleteErr = secondTx.Commit(ctx)
			}
			resultCh <- deleteErr
		}()
		waitCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		waitForPostgresCollaborationBlocker(t, waitCtx, fixture, secondPID, firstPID)
		stop()
		if err := firstTx.Commit(ctx); err != nil {
			t.Fatalf("commit first survivor removal: %v", err)
		}
		deleteErr := <-resultCh
		var denied *pgconn.PgError
		if !errors.As(deleteErr, &denied) || denied.Code != "55000" {
			t.Fatalf("concurrent final survivor removal = %v, want 55000", deleteErr)
		}
		var survivors int
		if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_import_entity_mappings
			where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=any($1::text[])`,
			[]string{lastID, third.RecordID}).Scan(&survivors); err != nil || survivors != 1 {
			t.Fatalf("concurrent survivor count=%d: %v", survivors, err)
		}
	})
	if _, err := local.blob.Stat(ctx, written.Version); err != nil {
		t.Fatalf("retained source bytes missing: %v", err)
	}
	reader, err := local.blob.Open(ctx, written.Version, attachments.FullByteRange())
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("retained bytes=%q read=%v close=%v", got, readErr, closeErr)
	}
	actor := storeEvidenceParticipantActor(t)
	imports := NewPostgresRecordPortabilityRepository(local.pool, fixture.admissionGate, localBlobFixtureWitness(t, local.pool), nil)
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1,
		Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	newJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID,
		IdempotencyKey: "v1-owner-handoff-later", ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	otherJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID,
		IdempotencyKey: "v1-owner-handoff-other-namespace", ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	otherBlob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "other-namespace"))
	if err != nil {
		t.Fatal(err)
	}
	otherWriter, err := attachments.NewManagedBlobWriter(otherBlob, attachments.BackendKindLocal,
		NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal,
		NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	request := attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: newJob.ImportJobID, Generation: int64(newJob.LockVersion), ArchiveDigest: &digestText},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}
	prepared, err := worker.preparePortabilityV1(ctx, last, surface)
	if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 1 {
		t.Fatalf("prepare last owner's exact source: %#v, %v", prepared, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, last.Operation.OperationID, prepared.InventoryDigest)
	taskID := prepared.LocalTargets[0].DeletionID
	var linked, retained bool
	err = fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_obligations c
		join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=c.obligation_id
		join public.record_write_obligations o on o.obligation_id=l.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2 and g.deletion_id=$3 and g.deletion_state='pending'
		and o.operation_id=$1 and l.seal_obligation and c.snapshot->>'object_version'=$4),
		record_platform_internal.record_portability_v1_retained_proof($5,$6,null,null)`,
		last.Operation.OperationID, written.ObligationID, taskID, written.Version.VersionID, first.Operation.OperationID, targetID).Scan(&linked, &retained)
	if err != nil || !linked || !retained {
		t.Fatalf("pending successor original source linked=%v retained=%v: %v", linked, retained, err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, first, surface); err != nil || replay != firstReceipt {
		t.Fatalf("pending successor changed retained receipt: %#v, %v", replay, err)
	}
	t.Run("pending_key_blocks_new_source", func(t *testing.T) {
		if _, err := writer.Put(ctx, request, bytes.NewReader(content)); err == nil {
			t.Fatal("pending cleanup admitted a new same-key source")
		}
		var count int
		if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_write_obligations
			where origin_kind='import_archive' and origin_id=$1`, newJob.ImportJobID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rejected same-key source count=%d: %v", count, err)
		}
		if _, err := local.blob.Stat(ctx, written.Version); err != nil {
			t.Fatalf("rejected source changed original bytes: %v", err)
		}
	})
	t.Run("distinct_namespace_remains_writable", func(t *testing.T) {
		otherRequest := attachments.WriteRequest{ProjectID: "default",
			Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: otherJob.ImportJobID, Generation: int64(otherJob.LockVersion), ArchiveDigest: &digestText},
			Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}
		other, err := otherWriter.Put(ctx, otherRequest, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("write same key in distinct namespace: %v", err)
		}
		if other.Version.Key != written.Version.Key || other.NamespaceID == written.NamespaceID || other.ObligationID == written.ObligationID {
			t.Fatalf("distinct namespace did not establish independent identity: original=%#v other=%#v", written, other)
		}
		if _, err := otherBlob.Stat(ctx, other.Version); err != nil {
			t.Fatalf("distinct namespace source missing: %v", err)
		}
	})
	t.Run("early_constraint_then_link_invalidation", func(t *testing.T) {
		tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, `set constraints all immediate`); err != nil {
			t.Fatalf("valid successor rejected by early constraint: %v", err)
		}
		_, err = tx.Exec(ctx, `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`, taskID, written.ObligationID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "55000" {
			t.Fatalf("removing exact source after early check = %v, want 55000", err)
		}
	})
	lastReceipt, err := worker.PurgeRecordPortability(ctx, last, surface)
	if err != nil {
		t.Fatalf("complete exact local successor: %v", err)
	}
	if _, err := local.blob.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("completed successor left source bytes: %v", err)
	}
	for _, item := range []struct {
		target  recorddeletion.PurgeTarget
		receipt recorddeletion.AdapterPurgeReceipt
	}{{first, firstReceipt}, {last, lastReceipt}} {
		if err := worker.VerifyRecordPortabilityPurge(ctx, item.target, surface, item.receipt); err != nil {
			t.Fatalf("verify completed source handoff: %v", err)
		}
	}
	var oldTask, oldObligation, oldReceipt string
	if err := fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text,to_jsonb(r)::text
		from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2
		join public.record_portability_purge_receipts r on r.operation_id=$3 where g.deletion_id=$1`,
		taskID, written.ObligationID, last.Operation.OperationID).Scan(&oldTask, &oldObligation, &oldReceipt); err != nil {
		t.Fatalf("capture completed original task and receipt: %v", err)
	}
	oldQuota := v1OwnerHandoffReadQuota(t, ctx, fixture)
	later, err := writer.Put(ctx, request, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("later same-key source after completion: %v", err)
	}
	if later.ObligationID == written.ObligationID || later.Version.Key != written.Version.Key || later.Version.VersionID == written.Version.VersionID {
		t.Fatalf("later write did not establish a distinct source: old=%#v new=%#v", written, later)
	}
	var untouched bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations
		where obligation_id=$2 and operation_id is null and cleanup_completed_at is null and quota_settled_at is null)
		and not exists(select 1 from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2)`,
		taskID, later.ObligationID).Scan(&untouched); err != nil || !untouched {
		t.Fatalf("completed task captured later source: untouched=%v err=%v", untouched, err)
	}
	if _, err := local.blob.Stat(ctx, later.Version); err != nil {
		t.Fatalf("later source bytes missing: %v", err)
	}
	// Publish B's actual replacement archive and attach it to a real record,
	// using the same metadata fixture as the original shared archive above.
	replacement := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 replacement archive owner")
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `select expires_at from public.record_import_jobs where import_job_id=$1`, newJob.ImportJobID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"artifact_id": "ria_v1replacement", "job_id": newJob.ImportJobID, "kind": "archive",
		"backend_kind": "local", "namespace_id": local.blob.Namespace().NamespaceID, "key": later.Version.Key,
		"version": later.Version.VersionID, "sha256": digestText, "size": len(content), "expires_at": expiry, "obligation_id": later.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.pool.Exec(ctx, `select public.record_import_artifact_publish($1)`, payload); err != nil {
		t.Fatalf("publish real replacement archive: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_plans(import_plan_id,import_job_id,plan_digest,object_count,remap_count,expires_at)
		values('rip_v1replacement',$1,$2,1,0,clock_timestamp()+interval '1 hour')`, newJob.ImportJobID, digest[:]); err != nil {
		t.Fatal(err)
	}
	replacementDigest := sha256.Sum256([]byte(replacement.RecordID))
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_entity_mappings(import_plan_id,entity_kind,source_id,source_identity_digest,target_id)
		values('rip_v1replacement','record',$1,$2,$1)`, replacement.RecordID, replacementDigest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, newJob.ImportJobID); err != nil {
		t.Fatal(err)
	}
	blockedJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID,
		IdempotencyKey: "v1-replacement-blocked-writer", ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// A blob accounting row requires an existing quota account, as in the
	// original shared fixture. The replacement inode is charged only once.
	if oldQuota.exists {
		if _, err := fixture.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
			values($1,$2,$3,$4,'local',$5)`, later.Version.Key, later.Version.SHA256[:], later.Version.VersionID,
			later.Version.SizeBytes, local.blob.Namespace().NamespaceID); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=physical_bytes+$1 where project_id='default'`, later.Version.SizeBytes); err != nil {
			t.Fatal(err)
		}
	}
	quotaBeforeReplacement := v1OwnerHandoffReadQuota(t, ctx, fixture)
	replacementTarget := makeTarget(replacement.RecordID, "replacement")
	replacementPreparation, err := worker.preparePortabilityV1(ctx, replacementTarget, surface)
	if err != nil || replacementPreparation.UnresolvedCount != 0 || len(replacementPreparation.LocalTargets) != 1 {
		t.Fatalf("prepare replacement physical cleanup: %#v, %v", replacementPreparation, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, replacementTarget.Operation.OperationID, replacementPreparation.InventoryDigest)
	replacementTask := replacementPreparation.LocalTargets[0]
	if replacementTask.DeletionID == taskID || replacementTask.Key != later.Version.Key || replacementTask.ExpectedVersion != later.Version.VersionID {
		t.Fatalf("replacement task has wrong physical identity: %#v", replacementTask)
	}
	var exact bool
	err = fixture.db.QueryRow(ctx, `select count(*)=1 and bool_and(l.obligation_id=$2 and l.seal_obligation)
		and not exists(select 1 from public.local_blob_gc_obligations old where old.deletion_id=$1 and old.obligation_id=$3)
		from public.local_blob_gc_obligations l where l.deletion_id=$1`, replacementTask.DeletionID, later.ObligationID, written.ObligationID).Scan(&exact)
	if err != nil || !exact {
		t.Fatalf("replacement pending task has wrong sealed source set=%v: %v", exact, err)
	}
	t.Run("pending_replacement_rejects_new_same_key_writer", func(t *testing.T) {
		var beforeTask, beforeSource string
		if err := fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text from public.blob_gc_deletions g
			join public.record_write_obligations o on o.obligation_id=$2 where g.deletion_id=$1`, replacementTask.DeletionID, later.ObligationID).
			Scan(&beforeTask, &beforeSource); err != nil {
			t.Fatal(err)
		}
		beforeQuota := v1OwnerHandoffReadQuota(t, ctx, fixture)
		blockedRequest := attachments.WriteRequest{ProjectID: "default",
			Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: blockedJob.ImportJobID, Generation: int64(blockedJob.LockVersion), ArchiveDigest: &digestText},
			Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}
		if _, err := writer.Put(ctx, blockedRequest, bytes.NewReader(content)); err == nil {
			t.Fatal("pending replacement accepted a new same-key writer")
		}
		var afterTask, afterSource string
		var absent bool
		err := fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text,not exists(select 1 from public.record_write_obligations
			where origin_kind='import_archive' and origin_id=$3) from public.blob_gc_deletions g
			join public.record_write_obligations o on o.obligation_id=$2 where g.deletion_id=$1`,
			replacementTask.DeletionID, later.ObligationID, blockedJob.ImportJobID).Scan(&afterTask, &afterSource, &absent)
		if err != nil || !absent || beforeTask != afterTask || beforeSource != afterSource || v1OwnerHandoffReadQuota(t, ctx, fixture) != beforeQuota {
			t.Fatalf("rejected writer changed task/source/quota or registered an obligation: absent=%v err=%v", absent, err)
		}
		if _, err := local.blob.Stat(ctx, later.Version); err != nil {
			t.Fatalf("rejected writer changed replacement inode: %v", err)
		}
	})
	replacementReceipt, err := worker.PurgeRecordPortability(ctx, replacementTarget, surface)
	if err != nil {
		t.Fatalf("claim, bind and complete replacement local cleanup: %v", err)
	}
	if _, err := local.blob.Stat(ctx, later.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("replacement inode remains after cleanup: %v", err)
	}
	err = fixture.db.QueryRow(ctx, `select g.deletion_state='completed' and g.physical_delete_result='deleted'
		and g.final_identity_bound and g.final_was_present and g.object_version=$4 and g.cleanup_dispatch_id is not null
		and g.inventory_digest is not null and g.completion_command is not null and g.receipt_digest is not null
		and record_platform_internal.record_portability_v1_local_task_proof(g)
		and o.cleanup_completed_at is not null and o.quota_settled_at is not null and o.cleanup_receipt=g.receipt_digest
		and (select count(*)=1 and bool_and(l.obligation_id=$2 and l.seal_obligation)
			from public.local_blob_gc_obligations l where l.deletion_id=g.deletion_id)
		and not exists(select 1 from public.local_blob_gc_obligations l where l.deletion_id=g.deletion_id and l.obligation_id=$3)
		from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2 where g.deletion_id=$1`,
		replacementTask.DeletionID, later.ObligationID, written.ObligationID, later.Version.VersionID).Scan(&exact)
	if err != nil || !exact {
		t.Fatalf("replacement completed source/task proof=%v: %v", exact, err)
	}
	quotaAfterReplacement := v1OwnerHandoffReadQuota(t, ctx, fixture)
	expectedQuota := quotaBeforeReplacement
	if expectedQuota.exists {
		expectedQuota.physical -= later.Version.SizeBytes
		expectedQuota.version++
	}
	if quotaAfterReplacement != expectedQuota {
		t.Fatalf("replacement quota settlement: before=%#v after=%#v expected=%#v", quotaBeforeReplacement, quotaAfterReplacement, expectedQuota)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, replacementTarget, surface, replacementReceipt); err != nil {
		t.Fatalf("verify replacement portability receipt: %v", err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, replacementTarget, surface); err != nil || replay != replacementReceipt {
		t.Fatalf("replacement portability replay=%#v: %v", replay, err)
	}
	var oldTaskAfter, oldObligationAfter, oldReceiptAfter string
	if err := fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text,to_jsonb(r)::text
		from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2
		join public.record_portability_purge_receipts r on r.operation_id=$3 where g.deletion_id=$1`,
		taskID, written.ObligationID, last.Operation.OperationID).Scan(&oldTaskAfter, &oldObligationAfter, &oldReceiptAfter); err != nil ||
		oldTaskAfter != oldTask || oldObligationAfter != oldObligation || oldReceiptAfter != oldReceipt {
		t.Fatalf("replacement cleanup changed original task, obligation, or receipt: %v", err)
	}
	if quotaReplay := v1OwnerHandoffReadQuota(t, ctx, fixture); quotaReplay != quotaAfterReplacement {
		t.Fatalf("replacement replay changed quota: after=%#v replay=%#v", quotaAfterReplacement, quotaReplay)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, last, surface, lastReceipt); err != nil {
		t.Fatalf("replacement cleanup invalidated original receipt: %v", err)
	}
}

func v1OwnerHandoffV2Successor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 retained by V2 successor")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	lastID, written, _ := seedV2SharedArchive(t, ctx, fixture, local, first.RecordID)
	firstOp := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1handoffv2first", ReservationID: "drs_v1handoffv2first",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	firstTarget := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, firstOp)
	var lastRevision string
	if err := fixture.db.QueryRow(ctx, `select current_revision_id from public.records where record_id=$1`, lastID).Scan(&lastRevision); err != nil {
		t.Fatal(err)
	}
	v2Op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
		OperationID: "rpo_v1handoffv2last", ReservationID: "drs_v1handoffv2last",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: lastID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	v2Target := recorddeletion.PurgeTarget{Operation: v2Op, OwnerClaim: seedLocalAttachmentDeletionOperation(t, ctx, fixture, v2Op, lastRevision)}
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-handoff-v2-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	firstReceipt, err := worker.PurgeRecordPortability(ctx, firstTarget, surface)
	if err != nil {
		t.Fatalf("first V1 shared purge: %v", err)
	}
	var retainedTarget string
	if err := fixture.db.QueryRow(ctx, `select t.target_id from public.record_portability_v1_targets t
		join public.record_portability_v1_obligations c using(operation_id,target_id)
		where t.operation_id=$1 and t.disposition='retained_shared' and c.obligation_id=$2`,
		firstOp.OperationID, written.ObligationID).Scan(&retainedTarget); err != nil {
		t.Fatal(err)
	}
	_, binding, body, err := localAdapterPurgeBinding(coordinator, v2Target, surface)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("immediate_constraint_rolls_back_early_sequence", func(t *testing.T) {
		var before string
		if err := fixture.db.QueryRow(ctx, `select to_jsonb(o)::text from public.record_write_obligations o where obligation_id=$1`, written.ObligationID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, `set constraints all immediate`); err != nil {
			t.Fatal(err)
		}
		var result []byte
		err = tx.QueryRow(ctx, `select public.local_portability_purge_prepare($1)`, body).Scan(&result)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || (denied.Code != "23503" && denied.Code != "55000") {
			t.Fatalf("V2 preparation with early constraint = %v", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var after string
		var clean bool
		err = fixture.db.QueryRow(ctx, `select to_jsonb(o)::text,
			not exists(select 1 from public.blob_gc_deletions where operation_id=$2)
			and not exists(select 1 from public.record_portability_cleanup_preparations where operation_id=$2)
			and exists(select 1 from public.record_import_entity_mappings where import_plan_id='rip_v2shared' and target_id=$3)
			from public.record_write_obligations o where obligation_id=$1`, written.ObligationID, v2Op.OperationID, lastID).Scan(&after, &clean)
		if err != nil || !clean || before != after {
			t.Fatalf("early V2 failure left effects: clean=%v err=%v", clean, err)
		}
	})
	pending, err := NewPostgresLocalBlobRepository(pool, nil, nil).PrepareLocalCleanup(ctx,
		attachments.LocalCleanupPreparation{LocalCleanupOperation: binding.LocalCleanupOperation,
			DeletionID: "bgd_v2lateowner", NamespaceID: binding.NamespaceID, IncarnationID: binding.IncarnationID, Key: written.Version.Key})
	if err != nil || pending.State != "pending" {
		t.Fatalf("prepare provisional V2 task=%#v: %v", pending, err)
	}
	var provisional, provisionalProof bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations o
		where o.obligation_id=$4 and o.operation_id is null)
		and not exists(select 1 from public.record_portability_cleanup_preparations where operation_id=$3)
		and not exists(select 1 from public.local_blob_gc_obligations where deletion_id=$5 and obligation_id=$4 and seal_obligation),
		record_platform_internal.record_portability_v1_local_successor($1,$2,$5)`,
		firstOp.OperationID, retainedTarget, v2Op.OperationID, written.ObligationID, pending.ID).Scan(&provisional, &provisionalProof); err != nil || !provisional || provisionalProof {
		t.Fatalf("provisional V2 task carried responsibility=%v proof=%v: %v", provisional, provisionalProof, err)
	}
	t.Run("unassigned_source_cannot_be_sealed", func(t *testing.T) {
		_, err := fixture.db.Exec(ctx, `insert into public.local_blob_gc_obligations(deletion_id,obligation_id,seal_obligation) values($1,$2,true)`, pending.ID, written.ObligationID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "55000" {
			t.Fatalf("sealed unassigned V2 source: %v", err)
		}
	})
	t.Run("unassigned_unprepared_unsealed_cannot_remove_owner", func(t *testing.T) {
		cut, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cut.Rollback(context.Background()) }()
		var cutErr error
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{`update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, []any{written.ObligationID, lastID}},
			{`delete from public.record_import_entity_mappings where import_plan_id='rip_v2shared' and target_id=$1`, []any{lastID}},
			{`delete from public.record_import_artifacts where import_artifact_id='ria_v2shared'`, nil},
			{`delete from public.record_import_plans where import_plan_id='rip_v2shared'`, nil},
			{`delete from public.record_import_jobs where import_job_id='rij_v2shared'`, nil},
			{`set constraints all immediate`, nil},
		} {
			if _, cutErr = cut.Exec(ctx, step.sql, step.args...); cutErr != nil {
				break
			}
		}
		var denied *pgconn.PgError
		if !errors.As(cutErr, &denied) || denied.Code != "55000" {
			t.Fatalf("provisional V2 task allowed owner removal: %v", cutErr)
		}
	})
	var projection map[string]any
	if err := worker.localPortabilityPurgeCommand(ctx, `select public.local_portability_purge_prepare($1)`, body, &projection); err != nil {
		t.Fatalf("real V2 final-owner preparation: %v", err)
	}
	var taskID string
	var valid bool
	err = fixture.db.QueryRow(ctx, `select g.deletion_id,
		record_platform_internal.record_portability_v1_local_successor($1,$2,g.deletion_id)
		from public.record_portability_cleanup_preparations p
		join public.record_portability_cleanup_targets target on target.operation_id=p.operation_id
		join public.blob_gc_deletions g on g.deletion_id=target.deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.seal_obligation
		join public.record_write_obligations o on o.obligation_id=l.obligation_id
		where p.operation_id=$3 and o.obligation_id=$4 and o.operation_id=$3 and g.deletion_state='pending'
		and (p.namespace_id,p.incarnation_id)=(g.namespace_id,g.incarnation_id)`,
		firstOp.OperationID, retainedTarget, v2Op.OperationID, written.ObligationID).Scan(&taskID, &valid)
	if err != nil || !valid {
		t.Fatalf("V2 pending same-source proof valid=%v: %v", valid, err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, firstTarget, surface); err != nil || replay != firstReceipt {
		t.Fatalf("V2 handoff changed V1 receipt: %#v, %v", replay, err)
	}
	for _, statement := range []string{
		`delete from public.record_portability_cleanup_preparations where operation_id=$1`,
		`update public.record_portability_cleanup_preparations set removed_rows=removed_rows+1 where operation_id=$1`,
		`delete from public.record_portability_cleanup_targets where operation_id=$1`,
	} {
		_, mutationErr := fixture.db.Exec(ctx, statement, v2Op.OperationID)
		var immutable *pgconn.PgError
		if !errors.As(mutationErr, &immutable) || immutable.Code != "55000" {
			t.Fatalf("mutating V2 immutable preparation %q: %v", statement, mutationErr)
		}
	}
	_, unassignErr := fixture.db.Exec(ctx, `update public.record_write_obligations set operation_id=null where obligation_id=$1`, written.ObligationID)
	var assignmentDenied *pgconn.PgError
	if !errors.As(unassignErr, &assignmentDenied) || assignmentDenied.Code != "55000" {
		t.Fatalf("V2 assigned source lost immutable operation: %v", unassignErr)
	}
	tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`, taskID, written.ObligationID); err != nil {
		t.Fatalf("stage missing pending V2 source link: %v", err)
	}
	if err := tx.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`,
		firstOp.OperationID, retainedTarget, taskID).Scan(&valid); err != nil || valid {
		t.Fatalf("V2 successor accepted missing same-source seal: valid=%v err=%v", valid, err)
	}
	_, err = tx.Exec(ctx, `set constraints all immediate`)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "55000" {
		t.Fatalf("commit without sealed source = %v, want 55000", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := local.blob.Stat(ctx, written.Version); err != nil {
		t.Fatalf("V2 preparation changed source bytes: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, firstTarget, surface, firstReceipt); err != nil {
		t.Fatal(err)
	}
}

// The portability writer creates the inode first. The upload writer is a
// different obligation and must record the actual version returned by the
// existing inode, not a fabricated version derived from the upload ID.
func v1OwnerHandoffDeduplicated(t *testing.T, attachment, revokeOrigin bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 portability original")
	neighbor := seedRecordWithoutSearchProjection(t, ctx, fixture, "Deduplicated upload owner")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	creator := seedV2ExportArtifact(t, ctx, fixture, local, first.RecordID)
	content := []byte("exclusive export artifact for V2 deletion")
	upload := local.prepare(t, ctx, "aup_v1dedupowner", content)
	local.put(t, ctx, &upload, content)
	local.bind(t, ctx, &upload)
	local.consume(t, ctx, upload)
	if upload.written.ObligationID == creator.ObligationID || upload.written.Version != creator.Version ||
		creator.Version.VersionID != "local-v2-"+creator.ObligationID {
		t.Fatalf("upload did not deduplicate original inode: creator=%#v upload=%#v", creator, upload.written)
	}
	var provenance bool
	err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations a
		join public.record_write_obligations b on b.obligation_id=$2
		join public.blob_publication_intents p on p.publication_id=b.publication_id
		join public.attachment_upload_parts part on part.obligation_id=b.obligation_id
		where a.obligation_id=$1 and a.origin_kind='export_artifact' and a.outcome='materialized'
		and b.origin_kind='upload' and b.outcome='materialized' and b.dispatch_id is not null and b.returned_at is not null
		and b.object_version=a.object_version and b.object_version=$3
		and (b.namespace_id,b.incarnation_id,b.canonical_key,b.sha256_digest,b.size_bytes)
		 is not distinct from (a.namespace_id,a.incarnation_id,a.canonical_key,a.sha256_digest,a.size_bytes)
		and (convert_from(b.return_command,'UTF8')::jsonb)->>'version'=a.object_version
		and p.publication_state='completed' and p.completion_outcome='consumed'
		and part.upload_id=$4 and part.object_version=a.object_version)`, creator.ObligationID,
		upload.written.ObligationID, creator.Version.VersionID, upload.upload.reserve.UploadID).Scan(&provenance)
	if err != nil || !provenance {
		t.Fatalf("deduplicated original/owner provenance=%v: %v", provenance, err)
	}
	// The part-only fixture has real bytes but no attachment publication to
	// account for them. Register the measured inode's actual blob identity and
	// quota in the same way as the existing shared-archive fixture.
	if !attachment {
		account, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = account.Rollback(context.Background()) }()
		if _, err := account.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
			values($1,$2,$3,$4,'local',$5)`, creator.Version.Key, creator.Version.SHA256[:], creator.Version.VersionID,
			creator.Version.SizeBytes, local.blob.Namespace().NamespaceID); err != nil {
			t.Fatal(err)
		}
		if _, err := account.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=physical_bytes+$1 where project_id='default'`, creator.Version.SizeBytes); err != nil {
			t.Fatal(err)
		}
		if err := account.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("creator_identity_immutable", func(t *testing.T) {
		var beforeA, beforeB string
		if err := fixture.db.QueryRow(ctx, `select to_jsonb(a)::text,to_jsonb(b)::text from public.record_write_obligations a
			join public.record_write_obligations b on b.obligation_id=$2 where a.obligation_id=$1`,
			creator.ObligationID, upload.written.ObligationID).Scan(&beforeA, &beforeB); err != nil {
			t.Fatal(err)
		}
		for _, probe := range []struct{ sql, id string }{
			{`update public.record_write_obligations set object_version='local-v2-rwo_missingcreator' where obligation_id=$1`, upload.written.ObligationID},
			{`update public.record_write_obligations set sha256_digest=decode(repeat('ff',32),'hex') where obligation_id=$1`, creator.ObligationID},
			{`update public.record_write_obligations set size_bytes=size_bytes+1 where obligation_id=$1`, creator.ObligationID},
			{`update public.record_write_obligations set namespace_id=gen_random_uuid() where obligation_id=$1`, creator.ObligationID},
			{`update public.record_write_obligations set incarnation_id=gen_random_uuid() where obligation_id=$1`, creator.ObligationID},
			{`update public.record_write_obligations set registration_command=convert_to('{}','UTF8') where obligation_id=$1`, creator.ObligationID},
			{`update public.record_write_obligations set dispatch_id=gen_random_uuid() where obligation_id=$1`, creator.ObligationID},
			{`delete from public.record_write_obligations where obligation_id=$1`, creator.ObligationID},
		} {
			_, mutationErr := fixture.db.Exec(ctx, probe.sql, probe.id)
			var denied *pgconn.PgError
			if !errors.As(mutationErr, &denied) || denied.Code != "55000" {
				t.Fatalf("protected creator mutation %q: %v", probe.sql, mutationErr)
			}
		}
		for _, statement := range []string{
			`update public.record_write_obligations set outcome='failed' where obligation_id=$1`,
			`update public.record_write_obligations set return_command=convert_to('{}','UTF8') where obligation_id=$1`,
		} {
			_, mutationErr := local.pool.Exec(ctx, statement, creator.ObligationID)
			var denied *pgconn.PgError
			if !errors.As(mutationErr, &denied) || denied.Code != "42501" {
				t.Fatalf("runtime rewrote source return %q: %v", statement, mutationErr)
			}
		}
		var afterA, afterB string
		if err := fixture.db.QueryRow(ctx, `select to_jsonb(a)::text,to_jsonb(b)::text from public.record_write_obligations a
			join public.record_write_obligations b on b.obligation_id=$2 where a.obligation_id=$1`,
			creator.ObligationID, upload.written.ObligationID).Scan(&afterA, &afterB); err != nil || beforeA != afterA || beforeB != afterB {
			t.Fatalf("rejected creator mutation changed source rows: %v", err)
		}
	})
	if attachment {
		writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal, local.local)
		if err != nil {
			t.Fatal(err)
		}
		enqueueManagedAttachmentUpload(t, ctx, local.repository, upload.upload.reserve, upload.written, "apj_v1dedupowner")
		completeManagedAttachmentProcessor(t, ctx, local.repository, writer, upload.upload.reserve.UploadID,
			[]byte("distinct preview for the retained upload"), upload.upload.reserve.Limits)
		result, err := fixture.db.Exec(ctx, `delete from public.attachment_upload_parts where upload_id=$1 and part_number=1`, upload.upload.reserve.UploadID)
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("remove completed upload part for attachment-only fixture: %v", err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_drafts set record_id=$1,base_revision_id=(select current_revision_id from public.records where record_id=$1) where draft_id=$2`, neighbor.RecordID, upload.upload.reserve.DraftID); err != nil {
		t.Fatalf("associate surviving draft with real neighboring record: %v", err)
	}
	if revokeOrigin {
		var exportLock int64
		if err := fixture.db.QueryRow(ctx, `select lock_version from public.record_export_jobs where export_job_id='rej_v2exclusive'`).Scan(&exportLock); err != nil {
			t.Fatal(err)
		}
		revoke, err := json.Marshal(map[string]any{"job_id": "rej_v2exclusive", "lock_version": exportLock})
		if err != nil {
			t.Fatal(err)
		}
		var revoked int64
		if err := local.pool.QueryRow(ctx, `select public.record_export_artifact_revoke($1)`, revoke).Scan(&revoked); err != nil || revoked != 1 {
			t.Fatalf("close original export writer through revoke: rows=%d err=%v", revoked, err)
		}
	}
	if !revokeOrigin {
		var published bool
		if err := fixture.db.QueryRow(ctx, `select j.job_state='published' and j.expires_at>clock_timestamp()
			and o.handed_off_at is null and o.operation_id is null and o.cleanup_completed_at is null and o.quota_settled_at is null
			from public.record_export_jobs j join public.record_write_obligations o on o.origin_id=j.export_job_id
			where j.export_job_id='rej_v2exclusive' and o.obligation_id=$1`, creator.ObligationID).Scan(&published); err != nil || !published {
			t.Fatalf("normal original export was not published and open=%v: %v", published, err)
		}
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1dedupowner", ReservationID: "drs_v1dedupowner",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-dedup-owner", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	prepared, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 0 {
		t.Fatalf("deduplicated surviving owner preparation=%#v: %v", prepared, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, op.OperationID, prepared.InventoryDigest)
	kind, ownerID := "upload_part", upload.upload.reserve.UploadID+":1"
	if attachment {
		kind, ownerID = "attachment", upload.upload.reserve.AttachmentID
	}
	var targetID, saved string
	err = fixture.db.QueryRow(ctx, `select t.target_id,e.value->>2 from public.record_portability_v1_targets t,
		jsonb_array_elements(t.retained_evidence) e(value) where t.operation_id=$1 and t.disposition='retained_shared'
		and t.obligation_id=$2 and t.object_version_id=$3 and e.value->>0=$4 and e.value->>1=$5`,
		op.OperationID, creator.ObligationID, creator.Version.VersionID, kind, ownerID).Scan(&targetID, &saved)
	if err != nil {
		t.Fatalf("saved exact deduplicated owner: %v", err)
	}
	var ownerFrame string
	if attachment {
		err = fixture.db.QueryRow(ctx, `select jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_attachments',
			jsonb_build_array(a.attachment_id),jsonb_build_array(b.backend_kind,b.blob_key,b.object_version,encode(b.sha256_digest,'hex'),b.size_bytes,
			to_char(b.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),b.namespace_id,o.incarnation_id,
			encode(o.storage_binding,'hex'),o.obligation_id,a.blob_key,a.blob_object_version,a.preview_blob_key,a.preview_blob_object_version,
			a.logical_size_bytes,a.preview_size_bytes,to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
			jsonb_build_array(a.project_id,a.record_id,a.draft_id,d.record_id,a.origin_draft_id))::text
			from public.record_attachments a left join public.record_drafts d on d.draft_id=a.draft_id
			join public.blob_objects b on b.blob_key=$1 and b.object_version=$2
			join public.record_write_obligations o on o.obligation_id=$3 where a.attachment_id=$4`,
			creator.Version.Key, creator.Version.VersionID, upload.written.ObligationID, upload.upload.reserve.AttachmentID).Scan(&ownerFrame)
	} else {
		err = fixture.db.QueryRow(ctx, `select jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','attachment_upload_parts',
			jsonb_build_array(part.upload_id,part.part_number),jsonb_build_array(u.transport_kind,o.canonical_key,part.object_version,
			encode(part.sha256_digest,'hex'),part.size_bytes,to_char(part.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
			part.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex'),part.obligation_id),
			jsonb_build_array(a.attachment_id,a.project_id,a.record_id,a.draft_id,d.record_id,u.origin_draft_id))::text
			from public.attachment_upload_parts part join public.attachment_uploads u using(upload_id)
			join public.record_attachments a on a.attachment_id=u.attachment_id
			left join public.record_drafts d on d.draft_id=a.draft_id
			join public.record_write_obligations o on o.obligation_id=part.obligation_id
			where part.upload_id=$1 and part.part_number=1`, upload.upload.reserve.UploadID).Scan(&ownerFrame)
	}
	if err != nil {
		t.Fatalf("read actual retained owner and parent: %v", err)
	}
	ownerHash := sha256.Sum256([]byte(ownerFrame))
	if saved != hex.EncodeToString(ownerHash[:]) {
		t.Fatalf("retained %s digest does not match actual owner", kind)
	}
	var valid, forged bool
	err = fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_old_owner_valid($1,$2,$3,$4,decode($5,'hex')),
		record_platform_internal.record_portability_v1_old_owner_valid($1,$2,$3,$4,decode(repeat('ff',32),'hex'))`,
		op.OperationID, targetID, kind, ownerID, saved).Scan(&valid, &forged)
	if err != nil || !valid || forged {
		t.Fatalf("deduplicated OLD owner valid=%v forged=%v: %v", valid, forged, err)
	}
	if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
		t.Fatalf("retained deduplicated inode missing: %v", err)
	}
	tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if attachment {
		_, err = tx.Exec(ctx, `update public.record_drafts set record_id=null,base_revision_id=null where draft_id=$1`, upload.upload.reserve.DraftID)
	} else {
		_, err = tx.Exec(ctx, `delete from public.attachment_upload_parts where upload_id=$1 and part_number=1`, upload.upload.reserve.UploadID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `set constraints all immediate`)
	}
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "55000" {
		t.Fatalf("last deduplicated owner removed without handoff: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil {
		t.Fatalf("finish retained original before successor: %v", err)
	}
	var closed, unsettled bool
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.local_blob_origin_closed(obligation_id),
		operation_id is null and handed_off_at is not null and cleanup_completed_at is null and quota_settled_at is null
		from public.record_write_obligations where obligation_id=$1`, creator.ObligationID).Scan(&closed, &unsettled); err != nil || !closed || !unsettled {
		t.Fatalf("retained original writer closure=%v unsettled=%v: %v", closed, unsettled, err)
	}
	task, err := NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil).PrepareLocalCleanup(ctx,
		attachments.LocalCleanupPreparation{DeletionID: "bgd_v1dedupsuccessor", NamespaceID: local.blob.Namespace().NamespaceID,
			IncarnationID: local.blob.Namespace().IncarnationID, Key: creator.Version.Key})
	if err != nil || task.State != "pending" || task.OperationID != nil {
		t.Fatalf("ordinary exact dedup successor=%#v: %v", task, err)
	}
	var exact bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.local_blob_gc_obligations
		where deletion_id=$3 and obligation_id=$4 and seal_obligation)
		and record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`,
		op.OperationID, targetID, task.ID, creator.ObligationID).Scan(&exact); err != nil || !exact {
		t.Fatalf("ordinary successor did not seal physical creator A=%v: %v", exact, err)
	}
	if attachment {
		t.Run("equal_version_other_obligation_is_not_creator", func(t *testing.T) {
			cut, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cut.Rollback(context.Background()) }()
			var otherSealed bool
			if err := cut.QueryRow(ctx, `select exists(select 1 from public.local_blob_gc_obligations
				where deletion_id=$1 and obligation_id=$2 and seal_obligation)`, task.ID, upload.written.ObligationID).Scan(&otherSealed); err != nil || !otherSealed {
				t.Fatalf("closed deduplicated B was not linked independently=%v: %v", otherSealed, err)
			}
			if _, err := cut.Exec(ctx, `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`, task.ID, creator.ObligationID); err != nil {
				t.Fatal(err)
			}
			if err := cut.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`,
				op.OperationID, targetID, task.ID).Scan(&exact); err != nil || exact {
				t.Fatalf("equal-version B replaced creator A=%v: %v", exact, err)
			}
		})
	}
	removeTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = removeTx.Rollback(context.Background()) }()
	if attachment {
		_, err = removeTx.Exec(ctx, `update public.record_drafts set record_id=null,base_revision_id=null where draft_id=$1`, upload.upload.reserve.DraftID)
	} else {
		_, err = removeTx.Exec(ctx, `delete from public.attachment_upload_parts where upload_id=$1 and part_number=1`, upload.upload.reserve.UploadID)
	}
	if err == nil {
		_, err = removeTx.Exec(ctx, `set constraints all immediate`)
	}
	if err != nil {
		t.Fatalf("remove owner after exact ordinary handoff: %v", err)
	}
	if err := removeTx.Commit(ctx); err != nil {
		t.Fatalf("commit owner removal after handoff: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatalf("V1 receipt after ordinary handoff: %v", err)
	}
	if err := fixture.db.QueryRow(ctx, `select operation_id is null and cleanup_completed_at is null and quota_settled_at is null
		from public.record_write_obligations where obligation_id=$1`, creator.ObligationID).Scan(&unsettled); err != nil || !unsettled {
		t.Fatalf("pending handoff settled original prematurely=%v: %v", !unsettled, err)
	}
	if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
		t.Fatalf("pending handoff changed original inode: %v", err)
	}
	if !attachment {
		var physicalBefore, quotaVersionBefore int64
		if err := fixture.db.QueryRow(ctx, `select physical_bytes,quota_version from public.attachment_quota_accounts where project_id='default'`).
			Scan(&physicalBefore, &quotaVersionBefore); err != nil {
			t.Fatal(err)
		}
		ordinary, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(local.pool, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		request := attachments.LocalCleanupPreparation{DeletionID: task.ID, NamespaceID: local.blob.Namespace().NamespaceID,
			IncarnationID: local.blob.Namespace().IncarnationID, Key: creator.Version.Key}
		completed, err := ordinary.Cleanup(ctx, request, "v1_retained_ordinary")
		if err != nil || completed.State != "completed" || completed.Outcome == nil || *completed.Outcome != "deleted" || completed.ReceiptDigest == nil {
			t.Fatalf("complete ordinary original successor=%#v: %v", completed, err)
		}
		if _, err := local.blob.Stat(ctx, creator.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
			t.Fatalf("ordinary successor left original physical version: %v", err)
		}
		var physicalAfter, quotaVersionAfter int64
		var proof, settled bool
		var taskBefore, originalBefore string
		err = fixture.db.QueryRow(ctx, `select q.physical_bytes,q.quota_version,
			record_platform_internal.record_portability_v1_local_task_proof(g)
			and record_platform_internal.record_portability_v1_local_successor($1,$2,g.deletion_id)
			and record_platform_internal.record_portability_v1_finished($1),
			o.operation_id is null and o.cleanup_completed_at is not null and o.quota_settled_at is not null
			and o.cleanup_outcome='deleted' and o.cleanup_receipt=g.receipt_digest
			and exists(select 1 from public.local_blob_gc_obligations l
				where l.deletion_id=g.deletion_id and l.obligation_id=o.obligation_id and l.seal_obligation),
			to_jsonb(g)::text,to_jsonb(o)::text
			from public.attachment_quota_accounts q cross join public.blob_gc_deletions g
			join public.record_write_obligations o on o.obligation_id=$4
			where q.project_id='default' and g.deletion_id=$3`, op.OperationID, targetID, task.ID, creator.ObligationID).
			Scan(&physicalAfter, &quotaVersionAfter, &proof, &settled, &taskBefore, &originalBefore)
		if err != nil || !proof || !settled || physicalAfter != physicalBefore-creator.Version.SizeBytes || quotaVersionAfter != quotaVersionBefore+1 {
			t.Fatalf("ordinary physical proof=%v settled=%v quota=%d/%d version=%d/%d: %v",
				proof, settled, physicalBefore, physicalAfter, quotaVersionBefore, quotaVersionAfter, err)
		}
		if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
			t.Fatalf("V1 finish after ordinary physical completion: %v", err)
		}
		replay, err := ordinary.Cleanup(ctx, request, "v1_retained_ordinary")
		if err != nil || replay.State != "completed" || replay.ReceiptDigest == nil || *replay.ReceiptDigest != *completed.ReceiptDigest {
			t.Fatalf("ordinary completion replay=%#v: %v", replay, err)
		}
		var taskAfter, originalAfter string
		var physicalReplay, quotaVersionReplay int64
		err = fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text,q.physical_bytes,q.quota_version
			from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2
			cross join public.attachment_quota_accounts q where g.deletion_id=$1 and q.project_id='default'`, task.ID, creator.ObligationID).
			Scan(&taskAfter, &originalAfter, &physicalReplay, &quotaVersionReplay)
		if err != nil || taskBefore != taskAfter || originalBefore != originalAfter || physicalReplay != physicalAfter || quotaVersionReplay != quotaVersionAfter {
			t.Fatalf("ordinary completion replay changed task/source/quota: %v", err)
		}
	}
}

func v1OwnerHandoffObligationOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, localBlobFixtureWitness(t, fixture.Runtime), nil)
	content := []byte("V1 obligation-only shared managed archive")
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-obligation-only",
		ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &digestText},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	var namespace string
	if err := fixture.platform.db.QueryRow(ctx, `select j.expires_at,o.namespace_id::text from public.record_import_jobs j
		join public.record_write_obligations o on o.origin_id=j.import_job_id where j.import_job_id=$1 and o.obligation_id=$2`,
		job.ImportJobID, written.ObligationID).Scan(&expiry, &namespace); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"artifact_id": "ria_v1obligationonly", "job_id": job.ImportJobID, "kind": "archive",
		"backend_kind": "local", "namespace_id": namespace, "key": written.Version.Key, "version": written.Version.VersionID,
		"sha256": digestText, "size": len(content), "expires_at": expiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Runtime.Exec(ctx, `select public.record_import_artifact_publish($1)`, payload); err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1obligationfirst")
	last := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1obligationlast")
	planDigest := sha256.Sum256([]byte("V1 obligation-only shared plan"))
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor,
		ArchiveDigest: digest, PlanDigest: planDigest, ObjectCount: 2, RemapCount: 2,
		Allocations: []recordbirth.Allocation{first, last}, Remaps: []ImportRemap{
			{EntityKind: "record", SourceID: "rec_v1obligationfirst", TargetID: first.RecordID, BirthAllocationID: first.AllocationID},
			{EntityKind: "record", SourceID: "rec_v1obligationlast", TargetID: last.RecordID, BirthAllocationID: last.AllocationID}},
		Documents: []ImportDocumentPlan{{SourceID: "rec_v1obligationfirst", TargetID: first.RecordID, Title: "First", Body: "Body"},
			{SourceID: "rec_v1obligationlast", TargetID: last.RecordID, Title: "Last", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevisions", [][]attachments.ImportedAvailableAttachment{{}, {}}, []recordbirth.Allocation{first, last}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.platform.db.Exec(ctx, `delete from public.record_import_artifacts where import_artifact_id='ria_v1obligationonly'`)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("remove published metadata, preserving original obligation: %v", err)
	}
	var original bool
	if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations o
		join public.record_write_obligation_targets ot using(obligation_id) where o.obligation_id=$1 and ot.record_id=$2
		and ot.handed_off_at is null and o.outcome='materialized' and o.returned_at is not null and o.published_at is not null
		and o.object_version=$3) and not exists(select 1 from public.record_import_artifacts where obligation_id=$1)`,
		written.ObligationID, last.RecordID, written.Version.VersionID).Scan(&original); err != nil || !original {
		t.Fatalf("genuine outside obligation without artifact=%v: %v", original, err)
	}
	makeTarget := func(id, suffix string) recorddeletion.PurgeTarget {
		t.Helper()
		record := records.RevisionCommitResult{RecordID: id}
		var lock, epoch int64
		if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, id).
			Scan(&record.RevisionID, &lock, &epoch); err != nil {
			t.Fatal(err)
		}
		record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
			OperationID: "rpo_v1obligationonly" + suffix, ReservationID: "drs_v1obligationonly" + suffix,
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: id},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		return seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, op)
	}
	firstTarget, lastTarget := makeTarget(first.RecordID, "first"), makeTarget(last.RecordID, "last")
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-obligation-only-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	firstReceipt, err := worker.PurgeRecordPortability(ctx, firstTarget, surface)
	if err != nil {
		t.Fatalf("retain outside obligation: %v", err)
	}
	var targetID, saved, frame string
	var count int
	err = fixture.platform.db.QueryRow(ctx, `select t.target_id,jsonb_array_length(t.retained_evidence),e.value->>2,
		jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_write_obligations',jsonb_build_array(o.obligation_id),
		jsonb_build_array(o.backend_kind,o.canonical_key,o.object_version,encode(o.sha256_digest,'hex'),o.size_bytes,
		to_char(o.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex')),
		jsonb_build_array(o.project_id,o.origin_kind,o.origin_id,o.origin_generation,o.record_id,o.record_epoch,o.publication_id))::text
		from public.record_portability_v1_targets t join public.record_write_obligations o on o.obligation_id=$2,
		jsonb_array_elements(t.retained_evidence) e(value) where t.operation_id=$1 and t.disposition='retained_shared'
		and e.value->>0='write_obligation' and e.value->>1=o.obligation_id`, firstTarget.Operation.OperationID, written.ObligationID).
		Scan(&targetID, &count, &saved, &frame)
	if err != nil || count != 1 {
		t.Fatalf("isolated obligation evidence count=%d: %v", count, err)
	}
	hash := sha256.Sum256([]byte(frame))
	if saved != hex.EncodeToString(hash[:]) {
		t.Fatal("obligation OLD digest does not match actual source")
	}
	var valid bool
	if err := fixture.platform.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'write_obligation',$3,decode($4,'hex'))`,
		firstTarget.Operation.OperationID, targetID, written.ObligationID, saved).Scan(&valid); err != nil || !valid {
		t.Fatalf("original obligation OLD proof=%v: %v", valid, err)
	}
	prepared, err := worker.preparePortabilityV1(ctx, lastTarget, surface)
	if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 1 {
		t.Fatalf("obligation successor=%#v: %v", prepared, err)
	}
	var exact bool
	if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_obligations c
		join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.local_blob_gc_obligations l on l.deletion_id=t.local_deletion_id and l.obligation_id=c.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2 and l.deletion_id=$3 and l.seal_obligation)`,
		lastTarget.Operation.OperationID, written.ObligationID, prepared.LocalTargets[0].DeletionID).Scan(&exact); err != nil || !exact {
		t.Fatalf("obligation successor lacks original sealed source=%v: %v", exact, err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, firstTarget, surface); err != nil || replay != firstReceipt {
		t.Fatalf("obligation successor replay=%#v: %v", replay, err)
	}
	if _, err := worker.PurgeRecordPortability(ctx, lastTarget, surface); err != nil {
		t.Fatalf("complete original obligation: %v", err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("obligation source after completion: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, firstTarget, surface, firstReceipt); err != nil {
		t.Fatal(err)
	}
}

func v1OwnerHandoffOrdinary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 ordinary successor owner")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	lastID, written, _ := seedV2SharedArchive(t, ctx, fixture, local, first.RecordID)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1ordinaryowner", ReservationID: "drs_v1ordinaryowner",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-ordinary-owner", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil {
		t.Fatalf("retain ordinary successor source: %v", err)
	}
	var retainedTarget string
	if err := fixture.db.QueryRow(ctx, `select t.target_id from public.record_portability_v1_targets t
		join public.record_portability_v1_obligations c using(operation_id,target_id)
		where t.operation_id=$1 and c.obligation_id=$2 and t.disposition='retained_shared'`, op.OperationID, written.ObligationID).Scan(&retainedTarget); err != nil {
		t.Fatal(err)
	}
	var closed bool
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.local_blob_origin_closed($1)`, written.ObligationID).Scan(&closed); err != nil || !closed {
		t.Fatalf("original import writer is not closed=%v: %v", closed, err)
	}
	task, err := NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil).PrepareLocalCleanup(ctx,
		attachments.LocalCleanupPreparation{DeletionID: "bgd_v1ordinaryowner", NamespaceID: local.blob.Namespace().NamespaceID,
			IncarnationID: local.blob.Namespace().IncarnationID, Key: written.Version.Key})
	if err != nil || task.State != "pending" || task.OperationID != nil {
		t.Fatalf("ordinary pending preparation=%#v: %v", task, err)
	}
	var linked, successor bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.local_blob_gc_obligations where deletion_id=$3 and obligation_id=$4 and seal_obligation),
		record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`, op.OperationID, retainedTarget, task.ID, written.ObligationID).
		Scan(&linked, &successor); err != nil || !linked || !successor {
		t.Fatalf("ordinary same-source link=%v successor=%v: %v", linked, successor, err)
	}
	t.Run("missing_sealed_original", func(t *testing.T) {
		cut, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cut.Rollback(context.Background()) }()
		if _, err := cut.Exec(ctx, `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`, task.ID, written.ObligationID); err != nil {
			t.Fatal(err)
		}
		var exact bool
		if err := cut.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`, op.OperationID, retainedTarget, task.ID).Scan(&exact); err != nil || exact {
			t.Fatalf("ordinary task without original seal accepted=%v: %v", exact, err)
		}
		var cutErr error
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{`update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, []any{written.ObligationID, lastID}},
			{`delete from public.record_import_entity_mappings where import_plan_id='rip_v2shared' and target_id=$1`, []any{lastID}},
			{`delete from public.record_import_artifacts where import_artifact_id='ria_v2shared'`, nil},
			{`delete from public.record_import_plans where import_plan_id='rip_v2shared'`, nil},
			{`delete from public.record_import_jobs where import_job_id='rij_v2shared'`, nil},
			{`set constraints all immediate`, nil},
		} {
			if _, cutErr = cut.Exec(ctx, step.sql, step.args...); cutErr != nil {
				break
			}
		}
		var denied *pgconn.PgError
		if !errors.As(cutErr, &denied) || denied.Code != "55000" {
			t.Fatalf("ordinary missing original seal = %v", cutErr)
		}
	})
	tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{`update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, []any{written.ObligationID, lastID}},
		{`delete from public.record_import_entity_mappings where import_plan_id='rip_v2shared' and target_id=$1`, []any{lastID}},
		{`delete from public.record_import_artifacts where import_artifact_id='ria_v2shared'`, nil},
		{`delete from public.record_import_plans where import_plan_id='rip_v2shared'`, nil},
		{`delete from public.record_import_jobs where import_job_id='rij_v2shared'`, nil},
		{`set constraints all immediate`, nil},
	} {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("ordinary exact handoff step %q: %v", step.sql, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ordinary exact handoff: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatalf("ordinary handoff lost V1 proof: %v", err)
	}
	if _, err := local.blob.Stat(ctx, written.Version); err != nil {
		t.Fatalf("pending ordinary handoff changed bytes: %v", err)
	}
}

func v1OwnerHandoffOpenOrdinary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newImportWriteTestFixture(t, ctx, false)
	content := []byte("import-dispatch-test-archive")
	writer, err := attachments.NewManagedBlobWriter(fixture.blob, attachments.BackendKindLocal, fixture.repository)
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, fixture.request(content, false), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	var before string
	var closed bool
	if err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(o)::text,record_platform_internal.local_blob_origin_closed(o.obligation_id)
		from public.record_write_obligations o where o.obligation_id=$1`, written.ObligationID).Scan(&before, &closed); err != nil || closed {
		t.Fatalf("open source closed=%v: %v", closed, err)
	}
	_, err = fixture.repository.PrepareLocalCleanup(ctx, attachments.LocalCleanupPreparation{DeletionID: "bgd_v1ordinaryopen",
		NamespaceID: fixture.blob.Namespace().NamespaceID, IncarnationID: fixture.blob.Namespace().IncarnationID, Key: written.Version.Key})
	if err == nil {
		t.Fatal("ordinary cleanup accepted an open unpublished writer")
	}
	var after string
	var absent bool
	if err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(o)::text,not exists(select 1 from public.blob_gc_deletions where deletion_id='bgd_v1ordinaryopen')
		from public.record_write_obligations o where o.obligation_id=$1`, written.ObligationID).Scan(&after, &absent); err != nil || before != after || !absent {
		t.Fatalf("open-source rejection changed durable state: absent=%v err=%v", absent, err)
	}
	if _, err := fixture.blob.Stat(ctx, written.Version); err != nil {
		t.Fatalf("rejected ordinary cleanup changed bytes: %v", err)
	}
}

func v1OwnerHandoffUnconsumed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 unresolved upload provenance")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	creator := seedV2ExportArtifact(t, ctx, fixture, local, first.RecordID)
	content := []byte("exclusive export artifact for V2 deletion")
	upload := local.prepare(t, ctx, "aup_v1unconsumed", content)
	local.put(t, ctx, &upload, content)
	local.bind(t, ctx, &upload)
	if upload.written.ObligationID == creator.ObligationID || upload.written.Version != creator.Version {
		t.Fatalf("unconsumed upload did not deduplicate real original: creator=%#v upload=%#v", creator, upload.written)
	}
	var unfinishedBefore string
	var unfinished bool
	err := fixture.db.QueryRow(ctx, `select to_jsonb(o)::text,o.cleanup_completed_at is null and o.operation_id is null
		and o.outcome='materialized' and o.returned_at is not null and o.canonical_key=$2 and o.object_version=$3
		from public.record_write_obligations o where o.obligation_id=$1`, upload.written.ObligationID,
		creator.Version.Key, creator.Version.VersionID).Scan(&unfinishedBefore, &unfinished)
	if err != nil || !unfinished {
		t.Fatalf("same-key writer is not an unfinished original obligation=%v: %v", unfinished, err)
	}
	var before string
	if err := fixture.db.QueryRow(ctx, `select to_jsonb(a)::text from public.record_export_artifacts a where export_artifact_id='rxa_v2exclusive'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1unconsumed", ReservationID: "drs_v1unconsumed",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-unconsumed-owner", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	prepared, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || prepared.UnresolvedCount == 0 || len(prepared.LocalTargets) != 0 || len(prepared.S3Targets) != 0 {
		t.Fatalf("unconsumed owner preparation=%#v: %v", prepared, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, op.OperationID, prepared.InventoryDigest)
	if _, err := worker.PurgeRecordPortability(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("unresolved same-key writer authorized cleanup: %v", err)
	}
	var unfinishedAfter string
	if err := fixture.db.QueryRow(ctx, `select to_jsonb(o)::text from public.record_write_obligations o where o.obligation_id=$1`, upload.written.ObligationID).
		Scan(&unfinishedAfter); err != nil || unfinishedAfter != unfinishedBefore {
		t.Fatalf("unresolved preparation changed unfinished writer: %v", err)
	}
	var after string
	var safe bool
	err = fixture.db.QueryRow(ctx, `select to_jsonb(a)::text,
		exists(select 1 from public.blob_publication_intents p where p.publication_id=$2 and p.publication_state='published')
		and not exists(select 1 from public.attachment_upload_parts where upload_id=$3)
		and not exists(select 1 from public.blob_gc_deletions where operation_id=$1)
		and not exists(select 1 from public.record_portability_purge_receipts where operation_id=$1)
		from public.record_export_artifacts a where a.export_artifact_id='rxa_v2exclusive'`,
		op.OperationID, upload.intent.PublicationID, upload.upload.reserve.UploadID).Scan(&after, &safe)
	if err != nil || before != after || !safe {
		t.Fatalf("unresolved owner changed source or dispatched: safe=%v err=%v", safe, err)
	}
	if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
		t.Fatalf("unresolved owner changed source bytes: %v", err)
	}
}

func v1OwnerHandoffArtifactOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 artifact-only original")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	creator := seedV2ExportArtifact(t, ctx, fixture, local, first.RecordID)
	content := []byte("exclusive export artifact for V2 deletion")
	digest := sha256.Sum256(content)
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs(export_job_id,actor_id,idempotency_key,
		export_kind,export_mode,job_state,request_fingerprint,inventory_digest,authorization_epoch,record_id,revision_id,
		owner_id,owner_generation,lease_expires_at,expires_at)
		values('rej_v1outside','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v1outside','archive','safe','staging',$1,$1,1,null,null,
		'local_export_worker',1,clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`, digest[:]).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal,
		NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	out, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "export_artifact", ID: "rej_v1outside", Generation: 1},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if out.ObligationID == creator.ObligationID || out.Version != creator.Version {
		t.Fatalf("outside artifact did not deduplicate original: %#v / %#v", creator, out)
	}
	payload, err := json.Marshal(map[string]any{"artifact_id": "rxa_v1outside", "job_id": "rej_v1outside", "kind": "archive",
		"content_type": "application/zip", "backend_kind": "local", "namespace_id": local.blob.Namespace().NamespaceID,
		"key": out.Version.Key, "version": out.Version.VersionID, "sha256": hex.EncodeToString(digest[:]),
		"size": len(content), "expires_at": expiry, "obligation_id": out.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.pool.Exec(ctx, `select public.record_export_artifact_publish($1)`, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_export_jobs set job_state='published',owner_id='',owner_generation=0,lease_expires_at=null where export_job_id='rej_v1outside'`); err != nil {
		t.Fatal(err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1artifactonly", ReservationID: "drs_v1artifactonly",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-artifact-only-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil {
		t.Fatalf("retain outside artifact: %v", err)
	}
	var targetID, saved, frame string
	var count int
	err = fixture.db.QueryRow(ctx, `select t.target_id,jsonb_array_length(t.retained_evidence),e.value->>2,
		jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_export_artifacts',jsonb_build_array(a.export_artifact_id),
		jsonb_build_array(a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,
		to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),a.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex'),a.obligation_id),
		jsonb_build_array(a.export_job_id,a.artifact_kind,jsonb_build_array(j.export_job_id,j.project_id,j.record_id,j.export_kind)))::text
		from public.record_portability_v1_targets t join public.record_export_artifacts a on a.export_artifact_id='rxa_v1outside'
		join public.record_export_jobs j on j.export_job_id=a.export_job_id join public.record_write_obligations o on o.obligation_id=a.obligation_id,
		jsonb_array_elements(t.retained_evidence) e(value) where t.operation_id=$1 and t.disposition='retained_shared'
		and e.value->>0='export_artifact' and e.value->>1=a.export_artifact_id`, op.OperationID).Scan(&targetID, &count, &saved, &frame)
	if err != nil || count != 1 {
		t.Fatalf("outside artifact evidence count=%d: %v", count, err)
	}
	hash := sha256.Sum256([]byte(frame))
	if saved != hex.EncodeToString(hash[:]) {
		t.Fatal("outside artifact OLD digest does not match actual row and parent")
	}
	var valid, forged bool
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'export_artifact','rxa_v1outside',decode($3,'hex')),
		record_platform_internal.record_portability_v1_old_owner_valid($1,$2,'export_artifact','rxa_v1outside',decode(repeat('ff',32),'hex'))`,
		op.OperationID, targetID, saved).Scan(&valid, &forged); err != nil || !valid || forged {
		t.Fatalf("artifact OLD proof=%v forged=%v: %v", valid, forged, err)
	}
	if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
		t.Fatalf("outside artifact lost original bytes: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatal(err)
	}
	cut, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cut.Rollback(context.Background()) }()
	_, cutErr := cut.Exec(ctx, `delete from public.record_export_artifacts where export_artifact_id='rxa_v1outside'`)
	if cutErr == nil {
		_, cutErr = cut.Exec(ctx, `set constraints all immediate`)
	}
	var denied *pgconn.PgError
	if !errors.As(cutErr, &denied) || denied.Code != "55000" {
		t.Fatalf("last artifact removed without successor: %v", cutErr)
	}
	if err := cut.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil).PrepareLocalCleanup(ctx,
		attachments.LocalCleanupPreparation{DeletionID: "bgd_v1artifacthandoff", NamespaceID: local.blob.Namespace().NamespaceID,
			IncarnationID: local.blob.Namespace().IncarnationID, Key: creator.Version.Key})
	if err != nil || task.State != "pending" {
		t.Fatalf("artifact original successor=%#v: %v", task, err)
	}
	var exact bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.local_blob_gc_obligations
		where deletion_id=$3 and obligation_id=$4 and seal_obligation)
		and record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`,
		op.OperationID, targetID, task.ID, creator.ObligationID).Scan(&exact); err != nil || !exact {
		t.Fatalf("artifact handoff did not seal original A=%v: %v", exact, err)
	}
	var version int64
	if err := fixture.db.QueryRow(ctx, `select lock_version from public.record_export_jobs where export_job_id='rej_v1outside'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	revoke, err := json.Marshal(map[string]any{"job_id": "rej_v1outside", "lock_version": version})
	if err != nil {
		t.Fatal(err)
	}
	var revoked int64
	if err := local.pool.QueryRow(ctx, `select public.record_export_artifact_revoke($1)`, revoke).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("close outside artifact origin: %d, %v", revoked, err)
	}
	finish, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = finish.Rollback(context.Background()) }()
	if _, err := finish.Exec(ctx, `delete from public.record_export_artifacts where export_artifact_id='rxa_v1outside'`); err != nil {
		t.Fatal(err)
	}
	if _, err := finish.Exec(ctx, `delete from public.record_export_jobs where export_job_id='rej_v1outside'`); err != nil {
		t.Fatal(err)
	}
	if _, err := finish.Exec(ctx, `set constraints all immediate`); err != nil {
		t.Fatalf("artifact handoff constraint: %v", err)
	}
	if err := finish.Commit(ctx); err != nil {
		t.Fatalf("commit artifact handoff: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatalf("artifact handoff changed receipt: %v", err)
	}
	if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
		t.Fatalf("pending artifact handoff changed bytes: %v", err)
	}
}

// An unsuccessful original writer does not create its expected final inode.
// A later successful writer for the same key is the physical creator of its
// own version, and cannot become a retained owner of the missing old version.
func v1OwnerHandoffFailedCreator(t *testing.T, wrongHash bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 failed physical creator")
	neighbor := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 later successful writer")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	content := []byte("V1 failed creator and later successful writer")
	digest := sha256.Sum256(content)
	var expires time.Time
	if err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs(export_job_id,actor_id,idempotency_key,
		export_kind,export_mode,job_state,request_fingerprint,inventory_digest,authorization_epoch,record_id,revision_id,
		owner_id,owner_generation,lease_expires_at,expires_at)
		values('rej_v1failedcreator','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v1failedcreator','markdown','safe','staging',$1,$1,1,$2,$3,
		'local_export_worker',1,clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`,
		digest[:], first.RecordID, first.RevisionID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal,
		NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	input := append([]byte(nil), content...)
	want := attachments.ErrBlobSizeMismatch
	if wrongHash {
		input[0] ^= 1
		want = attachments.ErrBlobHashMismatch
	} else {
		input = input[:len(input)-1]
	}
	_, err = writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "export_artifact", ID: "rej_v1failedcreator", Generation: 1},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(input))
	if !errors.Is(err, want) {
		t.Fatalf("real failed creator = %v, want %v", err, want)
	}
	var originalID string
	var failed bool
	if err := fixture.db.QueryRow(ctx, `select obligation_id,outcome='failed' and dispatched_at is not null and returned_at is not null
		and object_version is null and temporary_inode is not null and return_command is not null
		from public.record_write_obligations where origin_kind='export_artifact' and origin_id='rej_v1failedcreator'`).Scan(&originalID, &failed); err != nil || !failed {
		t.Fatalf("real failed original provenance=%v: %v", failed, err)
	}
	oldVersion := attachments.ObjectVersion{Key: "sha256/" + hex.EncodeToString(digest[:]),
		VersionID: "local-v2-" + originalID, SHA256: digest, SizeBytes: int64(len(content))}
	if _, err := local.blob.Stat(ctx, oldVersion); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("failed creator unexpectedly materialized final version: %v", err)
	}
	upload := local.prepare(t, ctx, "aup_v1failedcreator", content)
	local.put(t, ctx, &upload, content)
	local.bind(t, ctx, &upload)
	local.consume(t, ctx, upload)
	if upload.written.ObligationID == originalID || upload.written.Version.Key != oldVersion.Key ||
		upload.written.Version.VersionID != "local-v2-"+upload.written.ObligationID || upload.written.Version.VersionID == oldVersion.VersionID {
		t.Fatalf("later writer did not create its own actual version: old=%#v new=%#v", oldVersion, upload.written)
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_drafts set record_id=$1,base_revision_id=(select current_revision_id from public.records where record_id=$1) where draft_id=$2`, neighbor.RecordID, upload.upload.reserve.DraftID); err != nil {
		t.Fatal(err)
	}
	var originalValid, laterValid bool
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_owner_version(a),
		record_platform_internal.record_portability_v1_local_owner_version(b)
		from public.record_write_obligations a join public.record_write_obligations b on b.obligation_id=$2 where a.obligation_id=$1`,
		originalID, upload.written.ObligationID).Scan(&originalValid, &laterValid); err != nil || originalValid || !laterValid {
		t.Fatalf("failed/later creator validation=%v/%v: %v", originalValid, laterValid, err)
	}
	var lockVersion int64
	if err := fixture.db.QueryRow(ctx, `select lock_version from public.record_export_jobs where export_job_id='rej_v1failedcreator'`).Scan(&lockVersion); err != nil {
		t.Fatal(err)
	}
	revoke, err := json.Marshal(map[string]any{"job_id": "rej_v1failedcreator", "lock_version": lockVersion})
	if err != nil {
		t.Fatal(err)
	}
	var revoked int64
	if err := local.pool.QueryRow(ctx, `select public.record_export_artifact_revoke($1)`, revoke).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("close failed creator origin: %d, %v", revoked, err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1failedcreator", ReservationID: "drs_v1failedcreator",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-failed-creator", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	prepared, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil {
		t.Fatalf("prepare real failed creator inventory: %v", err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, op.OperationID, prepared.InventoryDigest)
	var captured, matched, retained bool
	err = fixture.db.QueryRow(ctx, `select c.snapshot->>'outcome'='failed' and c.snapshot->>'object_version' is null
		and t.object_version_id is null and t.obligation_id=$2,
		record_platform_internal.record_portability_v1_candidate_match('attachment_upload_parts',to_jsonb(part),t),
		exists(select 1 from jsonb_array_elements(t.retained_evidence) e(value)
			where e.value->>0='upload_part' and e.value->>1=part.upload_id||':'||part.part_number::text)
		from public.record_portability_v1_obligations c join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.attachment_upload_parts part on part.upload_id=$3 and part.part_number=1
		where c.operation_id=$1 and c.obligation_id=$2`, op.OperationID, originalID, upload.upload.reserve.UploadID).
		Scan(&captured, &matched, &retained)
	if err != nil || !captured || matched || retained {
		t.Fatalf("failed original snapshot=%v later version matched=%v retained=%v: %v", captured, matched, retained, err)
	}
	if _, err := local.blob.Stat(ctx, upload.written.Version); err != nil {
		t.Fatalf("failed original preparation changed later physical version: %v", err)
	}
}

// Cut the real writer after its final link and directory sync. A failing
// response or a killed writer leaves genuine bytes bearing A's materialization
// ID without a materialized return. B's real EEXIST return must not make A's
// unconfirmed version an authenticated retained survivor.
func v1OwnerHandoffUnconfirmedCreator(t *testing.T, kill bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	blob, ok := fixture.Blobs.(*attachments.LocalBlobStore)
	if !ok {
		t.Fatalf("local fixture store=%T", fixture.Blobs)
	}
	actor := storeEvidenceParticipantActor(t)
	witness := localBlobFixtureWitness(t, fixture.Runtime)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	content := []byte("real post-publication creator without a materialized return")
	digest := sha256.Sum256(content)
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID,
		IdempotencyKey: "v1-unconfirmed-physical-creator", ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	if job.LockVersion != 1 {
		t.Fatalf("process fixture origin generation=%d", job.LockVersion)
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "attachments-process.test")
	build := exec.Command("go", "test", "-c", "-o", binary, "./internal/center/attachments")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build existing process fixture: %v: %s", err, output)
	}
	fixture.RefreshHeartbeat()
	events, eventWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	controlReader, control, err := os.Pipe()
	if err != nil {
		_ = events.Close()
		_ = eventWriter.Close()
		t.Fatal(err)
	}
	process := exec.Command(binary, "-test.run=^TestLocalBlobProcessHelper$", "-test.count=1")
	process.Env = append(os.Environ(), "HOUFENG_LOCAL_PROCESS_ROOT="+blob.Namespace().RootPath,
		"HOUFENG_LOCAL_PROCESS_DATABASE_URL="+contentProcessorCrashDatabaseURL(fixture.platform.directRoleConfig(t, fixture.platform.runtime, "v1-unconfirmed-creator")),
		"HOUFENG_LOCAL_PROCESS_MODE=put", "HOUFENG_LOCAL_PROCESS_JOB="+job.ImportJobID,
		"HOUFENG_LOCAL_PROCESS_CONTENT="+string(content), "HOUFENG_LOCAL_PROCESS_CUTPOINT=after-publish")
	process.ExtraFiles = []*os.File{eventWriter, controlReader}
	var output bytes.Buffer
	process.Stdout, process.Stderr = &output, &output
	if err := process.Start(); err != nil {
		_ = events.Close()
		_ = eventWriter.Close()
		_ = controlReader.Close()
		_ = control.Close()
		t.Fatal(err)
	}
	_ = eventWriter.Close()
	_ = controlReader.Close()
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
		_ = events.Close()
		_ = control.Close()
	})
	if err := events.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var signal [1]byte
	if _, err := io.ReadFull(events, signal[:]); err != nil || signal[0] != 'B' {
		t.Fatalf("after-publish barrier=%q: %v", signal, err)
	}
	var originalID string
	var atBoundary bool
	if err := fixture.platform.db.QueryRow(ctx, `select obligation_id,outcome='registered' and dispatched_at is not null
		and temporary_inode is not null and returned_at is null and object_version is null from public.record_write_obligations
		where origin_kind='import_archive' and origin_id=$1`, job.ImportJobID).Scan(&originalID, &atBoundary); err != nil || !atBoundary {
		t.Fatalf("after-publish original boundary=%v: %v", atBoundary, err)
	}
	actual := attachments.ObjectVersion{Key: "sha256/" + hex.EncodeToString(digest[:]), VersionID: "local-v2-" + originalID,
		SHA256: digest, SizeBytes: int64(len(content))}
	if _, err := blob.Stat(ctx, actual); err != nil {
		t.Fatalf("real final link absent at after-publish cut: %v", err)
	}
	if kill {
		if err := process.Process.Kill(); err != nil {
			t.Fatal(err)
		}
	} else if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	waitErr := process.Wait()
	waited = true
	if !errors.As(waitErr, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("writer did not end at cut: %v: %s", waitErr, output.String())
	}
	var originalState bool
	if err := fixture.platform.db.QueryRow(ctx, `select dispatched_at is not null and
		case when $2 then returned_at is null and outcome='registered' and object_version is null
		else returned_at is not null and outcome='failed' and object_version=$3
		and (convert_from(return_command,'UTF8')::jsonb)->>'version'=$3 end
		from public.record_write_obligations where obligation_id=$1`, originalID, kill, actual.VersionID).Scan(&originalState); err != nil || !originalState {
		t.Fatalf("original cut return state=%v: %v", originalState, err)
	}
	fixture.RefreshHeartbeat()
	local := &localPublicationFixture{fixture: fixture.platform, pool: fixture.Runtime, blob: blob,
		local: NewPostgresLocalBlobRepository(fixture.Runtime, witness, nil), repository: NewPostgresAttachmentRepository(fixture.Runtime)}
	if err := local.repository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, fixture.Runtime, fixture.Gate)); err != nil {
		t.Fatal(err)
	}
	upload := local.prepare(t, ctx, "aup_v1unconfirmedcreator", content)
	local.put(t, ctx, &upload, content)
	local.bind(t, ctx, &upload)
	local.consume(t, ctx, upload)
	if upload.written.ObligationID == originalID || upload.written.Version != actual {
		t.Fatalf("later real writer did not return original inode version: A=%#v B=%#v", actual, upload.written)
	}
	var ownerValid bool
	if err := fixture.platform.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_owner_version(o)
		from public.record_write_obligations o where o.obligation_id=$1`, upload.written.ObligationID).Scan(&ownerValid); err != nil || ownerValid {
		t.Fatalf("unconfirmed A authenticated B as survivor=%v: %v", ownerValid, err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1unconfirmedcreator")
	neighbor := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1unconfirmedneighbor")
	planDigest := sha256.Sum256([]byte("unconfirmed physical creator import plan"))
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor,
		ArchiveDigest: digest, PlanDigest: planDigest, ObjectCount: 2, RemapCount: 2,
		Allocations: []recordbirth.Allocation{allocation, neighbor}, Remaps: []ImportRemap{
			{EntityKind: "record", SourceID: "rec_v1unconfirmedcreator", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID},
			{EntityKind: "record", SourceID: "rec_v1unconfirmedneighbor", TargetID: neighbor.RecordID, BirthAllocationID: neighbor.AllocationID}},
		Documents: []ImportDocumentPlan{{SourceID: "rec_v1unconfirmedcreator", TargetID: allocation.RecordID, Title: "Unconfirmed", Body: "Body"},
			{SourceID: "rec_v1unconfirmedneighbor", TargetID: neighbor.RecordID, Title: "Neighbor", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevisions", [][]attachments.ImportedAvailableAttachment{{}, {}}, []recordbirth.Allocation{allocation, neighbor}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_drafts set record_id=$1,base_revision_id=(select current_revision_id from public.records where record_id=$1) where draft_id=$2`, neighbor.RecordID, upload.upload.reserve.DraftID); err != nil {
		t.Fatal(err)
	}
	record := records.RevisionCommitResult{RecordID: allocation.RecordID}
	var lock, epoch int64
	if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, record.RecordID).
		Scan(&record.RevisionID, &lock, &epoch); err != nil {
		t.Fatal(err)
	}
	record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1unconfirmedcreator", ReservationID: "drs_v1unconfirmedcreator",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, op)
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-unconfirmed-creator-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	prepared, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || prepared.UnresolvedCount == 0 || len(prepared.LocalTargets) != 0 || len(prepared.S3Targets) != 0 {
		t.Fatalf("unconfirmed physical creator preparation=%#v: %v", prepared, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, op.OperationID, prepared.InventoryDigest)
	var snapshotVersion *string
	if !kill {
		snapshotVersion = &actual.VersionID
	}
	var originalPreserved, bRetained, dispatched bool
	err = fixture.platform.db.QueryRow(ctx, `select c.snapshot->>'object_version' is not distinct from $5::text and c.snapshot->>'outcome'=$3
		and t.object_version_id is not distinct from $5::text and t.disposition='unresolved',
		exists(select 1 from jsonb_array_elements(t.retained_evidence) e(value) where e.value->>0='upload_part' and e.value->>1=$4),
		exists(select 1 from public.blob_gc_deletions g where g.operation_id=$1)
		from public.record_portability_v1_obligations c join public.record_portability_v1_targets t using(operation_id,target_id)
		where c.operation_id=$1 and c.obligation_id=$2`, op.OperationID, originalID,
		map[bool]string{true: "registered", false: "failed"}[kill], upload.upload.reserve.UploadID+":1", snapshotVersion).Scan(&originalPreserved, &bRetained, &dispatched)
	if err != nil || !originalPreserved || bRetained || dispatched {
		t.Fatalf("unconfirmed source preserved=%v B retained=%v dispatched=%v: %v", originalPreserved, bRetained, dispatched, err)
	}
	if _, err := blob.Stat(ctx, actual); err != nil {
		t.Fatalf("unresolved inventory changed actual source: %v", err)
	}
}

type v1OwnerHandoffQuota struct {
	exists   bool
	physical int64
	version  int64
}

func v1OwnerHandoffReadQuota(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) v1OwnerHandoffQuota {
	t.Helper()
	var quota v1OwnerHandoffQuota
	if err := fixture.db.QueryRow(ctx, `select
		exists(select 1 from public.attachment_quota_accounts where project_id='default'),
		coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select quota_version from public.attachment_quota_accounts where project_id='default'),0)`).Scan(
		&quota.exists, &quota.physical, &quota.version); err != nil {
		t.Fatalf("read attachment quota: %v", err)
	}
	return quota
}

// Both exports write through their own managed obligations. The second Put
// observes the creator's inode through the real local EEXIST path.
func v1OwnerHandoffIndependentSameVersionLocal(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "creator_first"
		if reverse {
			name = "deduplicated_first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			creatorRecord := seedRecordWithoutSearchProjection(t, ctx, fixture, "Independent local creator")
			otherRecord := seedRecordWithoutSearchProjection(t, ctx, fixture, "Independent local deduplicated export")
			local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
			creator := seedV2ExportArtifact(t, ctx, fixture, local, creatorRecord.RecordID)
			content := []byte("exclusive export artifact for V2 deletion")
			digest := sha256.Sum256(content)
			var expiry time.Time
			err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs
				(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
				 authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
				values('rej_v1independent','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v1independent','markdown','safe','staging',$1,$1,1,$2,$3,
				 'local_export_worker',1,clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`,
				digest[:], otherRecord.RecordID, otherRecord.RevisionID).Scan(&expiry)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal,
				NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
			if err != nil {
				t.Fatal(err)
			}
			other, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
				Origin:  attachments.WriteOrigin{Kind: "export_artifact", ID: "rej_v1independent", Generation: 1},
				Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
			if err != nil {
				t.Fatalf("write independently through EEXIST: %v", err)
			}
			if creator.ObligationID == other.ObligationID || creator.Version != other.Version ||
				creator.Version.VersionID != "local-v2-"+creator.ObligationID {
				t.Fatalf("independent writers did not share the creator inode: creator=%#v other=%#v", creator, other)
			}
			payload, err := json.Marshal(map[string]any{"artifact_id": "rxa_v1independent", "job_id": "rej_v1independent",
				"kind": "markdown", "content_type": "text/markdown", "backend_kind": "local", "namespace_id": local.blob.Namespace().NamespaceID,
				"key": other.Version.Key, "version": other.Version.VersionID, "sha256": hex.EncodeToString(digest[:]),
				"size": len(content), "expires_at": expiry, "obligation_id": other.ObligationID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := local.pool.Exec(ctx, `select public.record_export_artifact_publish($1)`, payload); err != nil {
				t.Fatalf("publish independent artifact: %v", err)
			}
			if _, err := fixture.db.Exec(ctx, `update public.record_export_jobs set job_state='published',owner_id='',owner_generation=0,
				lease_expires_at=null where export_job_id='rej_v1independent'`); err != nil {
				t.Fatal(err)
			}
			var provenance bool
			err = fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations a
				join public.record_write_obligations b on b.obligation_id=$2
				join public.record_export_artifacts aa on aa.obligation_id=a.obligation_id
				join public.record_export_artifacts bb on bb.obligation_id=b.obligation_id
				where a.obligation_id=$1 and a.origin_id='rej_v2exclusive' and b.origin_id='rej_v1independent'
				and a.outcome='materialized' and b.outcome='materialized' and a.returned_at is not null and b.returned_at is not null
				and (a.namespace_id,a.incarnation_id,a.canonical_key,a.object_version,a.sha256_digest,a.size_bytes)
				 is not distinct from (b.namespace_id,b.incarnation_id,b.canonical_key,b.object_version,b.sha256_digest,b.size_bytes)
				and (convert_from(a.return_command,'UTF8')::jsonb)->>'version'=a.object_version
				and (convert_from(b.return_command,'UTF8')::jsonb)->>'version'=a.object_version)`, creator.ObligationID, other.ObligationID).Scan(&provenance)
			if err != nil || !provenance {
				t.Fatalf("independent original registration and return provenance=%v: %v", provenance, err)
			}
			// A real same-key writer in another admitted namespace must never be
			// folded into this inode's inventory or task.
			unrelatedRecord := seedRecordWithoutSearchProjection(t, ctx, fixture, "Independent unrelated namespace")
			if _, err := fixture.db.Exec(ctx, `insert into public.record_export_jobs
				(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
				 authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
				values('rej_v1unrelated','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v1unrelated','markdown','safe','staging',$1,$1,1,$2,$3,
				 'local_export_worker',1,clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour')`,
				digest[:], unrelatedRecord.RecordID, unrelatedRecord.RevisionID); err != nil {
				t.Fatal(err)
			}
			unrelatedBlob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "unrelated-namespace"))
			if err != nil {
				t.Fatal(err)
			}
			unrelatedWriter, err := attachments.NewManagedBlobWriter(unrelatedBlob, attachments.BackendKindLocal,
				NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
			if err != nil {
				t.Fatal(err)
			}
			unrelated, err := unrelatedWriter.Put(ctx, attachments.WriteRequest{ProjectID: "default",
				Origin:  attachments.WriteOrigin{Kind: "export_artifact", ID: "rej_v1unrelated", Generation: 1},
				Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
			if err != nil || unrelated.Version.Key != creator.Version.Key || unrelated.NamespaceID == creator.NamespaceID || unrelated.ObligationID == creator.ObligationID {
				t.Fatalf("write unrelated same-key source: %#v, %v", unrelated, err)
			}
			// Only account for an inode when this fixture already has a quota
			// account; managed exports do not create one by themselves.
			if v1OwnerHandoffReadQuota(t, ctx, fixture).exists {
				if _, err := fixture.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
					values($1,$2,$3,$4,'local',$5)`, creator.Version.Key, creator.Version.SHA256[:], creator.Version.VersionID,
					creator.Version.SizeBytes, local.blob.Namespace().NamespaceID); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.db.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=physical_bytes+$1 where project_id='default'`, creator.Version.SizeBytes); err != nil {
					t.Fatal(err)
				}
			}
			makeTarget := func(record records.RevisionCommitResult, suffix string) recorddeletion.PurgeTarget {
				t.Helper()
				op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
					OperationID: "rpo_v1independent" + suffix, ReservationID: "drs_v1independent" + suffix,
					Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
					ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
				return seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
			}
			first, last := makeTarget(creatorRecord, "creator"), makeTarget(otherRecord, "other")
			if reverse {
				first, last = last, first
			}
			pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-independent-local-worker", 2)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
				t.Fatal(err)
			}
			cleanup, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
			if err := worker.ConfigurePortabilityDeletionLocalCleanup(cleanup); err != nil {
				t.Fatal(err)
			}
			quotaBefore := v1OwnerHandoffReadQuota(t, ctx, fixture)
			surface := recorddeletion.RecordPortabilitySurfaceDigest()
			firstReceipt, err := worker.PurgeRecordPortability(ctx, first, surface)
			if err != nil {
				t.Fatalf("retain independent outside writer: %v", err)
			}
			var untouched bool
			err = fixture.db.QueryRow(ctx, `select count(*)=2 and bool_and(operation_id is null and cleanup_completed_at is null and quota_settled_at is null)
				from public.record_write_obligations where obligation_id=any($1::text[])`, []string{creator.ObligationID, other.ObligationID}).Scan(&untouched)
			if err != nil || !untouched {
				t.Fatalf("retained writers assigned or settled: untouched=%v err=%v", untouched, err)
			}
			var targetID string
			var retained bool
			err = fixture.db.QueryRow(ctx, `select t.target_id,record_platform_internal.record_portability_v1_retained_proof(t.operation_id,t.target_id,null,null)
				from public.record_portability_v1_targets t where t.operation_id=$1 and t.disposition='retained_shared'
				and t.blob_key=$2 and t.object_version_id=$3`, first.Operation.OperationID, creator.Version.Key, creator.Version.VersionID).Scan(&targetID, &retained)
			if err != nil || !retained {
				t.Fatalf("first operation did not retain exact live owner=%v: %v", retained, err)
			}
			survivorArtifact := "rxa_v1independent"
			if reverse {
				survivorArtifact = "rxa_v2exclusive"
			}
			var savedOwner, ownerFrame, firstDigest string
			err = fixture.db.QueryRow(ctx, `select e.value->>2,
				jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_export_artifacts',jsonb_build_array(a.export_artifact_id),
				 jsonb_build_array(a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,
				 to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),a.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex'),a.obligation_id),
				 jsonb_build_array(a.export_job_id,a.artifact_kind,jsonb_build_array(j.export_job_id,j.project_id,j.record_id,j.export_kind)))::text,
				encode(p.inventory_digest,'hex') from public.record_portability_v1_targets t
				join public.record_portability_v1_preparations p using(operation_id)
				join public.record_export_artifacts a on a.export_artifact_id=$3
				join public.record_export_jobs j on j.export_job_id=a.export_job_id
				join public.record_write_obligations o on o.obligation_id=a.obligation_id,
				jsonb_array_elements(t.retained_evidence) e(value)
				where t.operation_id=$1 and t.target_id=$2 and e.value->>0='export_artifact' and e.value->>1=a.export_artifact_id`,
				first.Operation.OperationID, targetID, survivorArtifact).Scan(&savedOwner, &ownerFrame, &firstDigest)
			if err != nil {
				t.Fatalf("read independent surviving artifact and parent: %v", err)
			}
			ownerHash := sha256.Sum256([]byte(ownerFrame))
			if savedOwner != hex.EncodeToString(ownerHash[:]) {
				t.Fatal("surviving independent artifact digest differs from real parent and source")
			}
			assertV1SavedInventoryDigest(t, ctx, fixture, first.Operation.OperationID, firstDigest)
			if retainedQuota := v1OwnerHandoffReadQuota(t, ctx, fixture); retainedQuota != quotaBefore {
				t.Fatalf("retained operation changed quota: before=%#v after=%#v", quotaBefore, retainedQuota)
			}
			if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
				t.Fatalf("first operation changed original inode: %v", err)
			}
			prepared, err := worker.preparePortabilityV1(ctx, last, surface)
			if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 1 {
				t.Fatalf("prepare last independent writer: %#v, %v", prepared, err)
			}
			assertV1SavedInventoryDigest(t, ctx, fixture, last.Operation.OperationID, prepared.InventoryDigest)
			taskID := prepared.LocalTargets[0].DeletionID
			var captured, linked, assigned int
			err = fixture.db.QueryRow(ctx, `select count(distinct c.obligation_id),count(distinct l.obligation_id),
				count(distinct o.obligation_id) filter (where o.operation_id=$1 and o.cleanup_completed_at is null and o.quota_settled_at is null)
				from public.record_portability_v1_obligations c join public.record_portability_v1_targets t on t.operation_id=c.operation_id and t.target_id=c.target_id
				join public.record_write_obligations o on o.obligation_id=c.obligation_id
				left join public.local_blob_gc_obligations l on l.deletion_id=t.local_deletion_id and l.obligation_id=c.obligation_id and l.seal_obligation
				where c.operation_id=$1 and c.obligation_id=any($2::text[]) and t.local_deletion_id=$3
				and c.snapshot->>'object_version'=$4`, last.Operation.OperationID, []string{creator.ObligationID, other.ObligationID}, taskID, creator.Version.VersionID).
				Scan(&captured, &linked, &assigned)
			if err != nil || captured != 2 || linked != 2 || assigned != 2 {
				t.Fatalf("last task original inventory=%d sealed=%d assigned=%d: %v", captured, linked, assigned, err)
			}
			var unrelatedUntouched bool
			err = fixture.db.QueryRow(ctx, `select o.operation_id is null and o.cleanup_completed_at is null and o.quota_settled_at is null
				and not exists(select 1 from public.record_portability_v1_obligations c where c.operation_id in ($1,$2) and c.obligation_id=o.obligation_id)
				and not exists(select 1 from public.local_blob_gc_obligations l where l.deletion_id=$3 and l.obligation_id=o.obligation_id)
				from public.record_write_obligations o where o.obligation_id=$4`, first.Operation.OperationID, last.Operation.OperationID,
				taskID, unrelated.ObligationID).Scan(&unrelatedUntouched)
			if err != nil || !unrelatedUntouched {
				t.Fatalf("unrelated namespace source was adopted=%v: %v", !unrelatedUntouched, err)
			}
			t.Run("extra_unrelated_link", func(t *testing.T) {
				tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(context.Background()) }()
				_, err = tx.Exec(ctx, `insert into public.local_blob_gc_obligations(deletion_id,obligation_id,seal_obligation) values($1,$2,false)`, taskID, unrelated.ObligationID)
				var denied *pgconn.PgError
				if !errors.As(err, &denied) || denied.Code != "55000" {
					t.Fatalf("unrelated source linked to task: %v", err)
				}
			})

			for _, missing := range []string{creator.ObligationID, other.ObligationID} {
				t.Run("missing_link_"+missing, func(t *testing.T) {
					tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = tx.Rollback(context.Background()) }()
					_, err = tx.Exec(ctx, `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`, taskID, missing)
					if err == nil {
						var proof bool
						if err := tx.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_successor($1,$2,$3)`, first.Operation.OperationID, targetID, taskID).Scan(&proof); err != nil || proof {
							t.Fatalf("missing original source accepted as successor=%v: %v", proof, err)
						}
						_, err = tx.Exec(ctx, `set constraints all immediate`)
					}
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55000" {
						t.Fatalf("missing source link did not reject transaction: %v", err)
					}
				})
			}
			if replay, err := worker.PurgeRecordPortability(ctx, first, surface); err != nil || replay != firstReceipt {
				t.Fatalf("first receipt changed across pending handoff: %#v, %v", replay, err)
			}
			lastReceipt, err := worker.PurgeRecordPortability(ctx, last, surface)
			if err != nil {
				t.Fatalf("complete both independent obligations: %v", err)
			}
			if _, err := local.blob.Stat(ctx, creator.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
				t.Fatalf("completed task left creator inode: %v", err)
			}
			if _, err := unrelatedBlob.Stat(ctx, unrelated.Version); err != nil {
				t.Fatalf("independent namespace bytes changed: %v", err)
			}
			var completed, settled bool
			err = fixture.db.QueryRow(ctx, `select
				g.deletion_state='completed' and g.physical_delete_result='deleted' and g.receipt_digest is not null,
				(select count(*)=2 and bool_and(o.cleanup_completed_at is not null and o.quota_settled_at is not null
				 and o.cleanup_receipt=g.receipt_digest and l.seal_obligation) from public.record_write_obligations o
				 join public.local_blob_gc_obligations l on l.obligation_id=o.obligation_id and l.deletion_id=g.deletion_id
				 where o.obligation_id=any($2::text[]))
				from public.blob_gc_deletions g where g.deletion_id=$1`,
				taskID, []string{creator.ObligationID, other.ObligationID}).Scan(&completed, &settled)
			if err != nil || !completed || !settled {
				t.Fatalf("completion=%v settled=%v: %v", completed, settled, err)
			}
			quotaAfter := v1OwnerHandoffReadQuota(t, ctx, fixture)
			expectedQuota := quotaBefore
			if expectedQuota.exists {
				expectedQuota.physical -= creator.Version.SizeBytes
				expectedQuota.version++
			}
			if quotaAfter != expectedQuota {
				t.Fatalf("physical settlement quota: before=%#v after=%#v expected=%#v", quotaBefore, quotaAfter, expectedQuota)
			}
			for _, item := range []struct {
				target  recorddeletion.PurgeTarget
				receipt recorddeletion.AdapterPurgeReceipt
			}{{first, firstReceipt}, {last, lastReceipt}} {
				if err := worker.VerifyRecordPortabilityPurge(ctx, item.target, surface, item.receipt); err != nil {
					t.Fatalf("verify independent handoff: %v", err)
				}
				if replay, err := worker.PurgeRecordPortability(ctx, item.target, surface); err != nil || replay != item.receipt {
					t.Fatalf("replay independent receipt: %#v, %v", replay, err)
				}
			}
			if quotaReplay := v1OwnerHandoffReadQuota(t, ctx, fixture); quotaReplay != quotaAfter {
				t.Fatalf("receipt replay changed quota: after=%#v replay=%#v", quotaAfter, quotaReplay)
			}
		})
	}
	t.Run("concurrent_distinct_owners", v1OwnerHandoffConcurrentIndependentOwners)
	t.Run("concurrent_independent_exports", v1OwnerHandoffConcurrentIndependentExports)
	t.Run("unknown_writer", v1OwnerHandoffUnconsumed)
	t.Run("parent_route_and_creator_identity", func(t *testing.T) { v1OwnerHandoffDeduplicated(t, true, false) })
}

// Hold one real survivor row until PostgreSQL reports the other connection is
// blocked, then require the second transaction to observe the lost survivor.
func v1OwnerHandoffConcurrentIndependentOwners(t *testing.T) {
	for _, isolation := range []pgx.TxIsoLevel{pgx.ReadCommitted, pgx.RepeatableRead} {
		name := "read_committed"
		if isolation == pgx.RepeatableRead {
			name = "repeatable_read"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			first := seedRecordWithoutSearchProjection(t, ctx, fixture, "Concurrent retained source")
			local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
			lastID, written, _ := seedV2SharedArchive(t, ctx, fixture, local, first.RecordID)
			third := seedRecordWithoutSearchProjection(t, ctx, fixture, "Concurrent second surviving owner")
			digest := sha256.Sum256([]byte(third.RecordID))
			if _, err := fixture.db.Exec(ctx, `insert into public.record_import_entity_mappings
				(import_plan_id,entity_kind,source_id,source_identity_digest,target_id)
				values('rip_v2shared','record',$1,$2,$1)`, third.RecordID, digest[:]); err != nil {
				t.Fatal(err)
			}
			op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
				OperationID: "rpo_v1independentconcurrent", ReservationID: "drs_v1independentconcurrent",
				Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
				ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
			target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, first, op)
			pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-independent-concurrent", 2)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
				t.Fatal(err)
			}
			worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
			surface := recorddeletion.RecordPortabilitySurfaceDigest()
			receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
			if err != nil {
				t.Fatalf("prepare concurrent retained source: %v", err)
			}
			firstTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = firstTx.Rollback(context.Background()) }()
			secondTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = secondTx.Rollback(context.Background()) }()
			var firstPID, secondPID int32
			if err := firstTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&firstPID); err != nil {
				t.Fatal(err)
			}
			// Establish the second snapshot before the first owner is removed.
			var snapshotOwners int
			if err := secondTx.QueryRow(ctx, `select pg_backend_pid(),count(*) from public.record_import_entity_mappings
				where import_plan_id='rip_v2shared' and target_id=any($1::text[])`, []string{lastID, third.RecordID}).Scan(&secondPID, &snapshotOwners); err != nil || snapshotOwners != 2 {
				t.Fatalf("pre-removal owners=%d: %v", snapshotOwners, err)
			}
			if _, err := firstTx.Exec(ctx, `select 1 from public.record_import_entity_mappings
				where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=$1 for update`, lastID); err != nil {
				t.Fatal(err)
			}
			if _, err := firstTx.Exec(ctx, `select 1 from public.record_write_obligation_targets where obligation_id=$1 and record_id=$2 for update`, written.ObligationID, lastID); err != nil {
				t.Fatal(err)
			}
			if _, err := firstTx.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, written.ObligationID, third.RecordID); err != nil {
				t.Fatal(err)
			}
			result, err := firstTx.Exec(ctx, `delete from public.record_import_entity_mappings where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=$1`, third.RecordID)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatalf("remove first real owner: %v", err)
			}
			resultCh := make(chan error, 1)
			go func() {
				_, err := secondTx.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, written.ObligationID, lastID)
				if err == nil {
					_, err = secondTx.Exec(ctx, `delete from public.record_import_entity_mappings where import_plan_id='rip_v2shared' and entity_kind='record' and target_id=$1`, lastID)
				}
				if err == nil {
					err = secondTx.Commit(ctx)
				}
				resultCh <- err
			}()
			waitCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			waitForPostgresCollaborationBlocker(t, waitCtx, fixture, secondPID, firstPID)
			stop()
			if err := firstTx.Commit(ctx); err != nil {
				t.Fatalf("commit first owner removal: %v", err)
			}
			var denied *pgconn.PgError
			if err := <-resultCh; !errors.As(err, &denied) || (denied.Code != "40001" && denied.Code != "55000") {
				t.Fatalf("second last-owner removal was not rejected: %v", err)
			}
			var remaining int
			if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_import_entity_mappings where import_plan_id='rip_v2shared'
				and entity_kind='record' and target_id=any($1::text[])`, []string{lastID, third.RecordID}).Scan(&remaining); err != nil || remaining != 1 {
				t.Fatalf("concurrent removal left %d owners: %v", remaining, err)
			}
			if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
				t.Fatalf("concurrent removal lost retained proof: %v", err)
			}
			if _, err := local.blob.Stat(ctx, written.Version); err != nil {
				t.Fatalf("concurrent removal changed retained bytes: %v", err)
			}
		})
	}
}

func v1OwnerHandoffConcurrentIndependentExports(t *testing.T) {
	for _, isolation := range []pgx.TxIsoLevel{pgx.ReadCommitted, pgx.RepeatableRead} {
		name := "read_committed"
		if isolation == pgx.RepeatableRead {
			name = "repeatable_read"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			original := seedRecordWithoutSearchProjection(t, ctx, fixture, "Concurrent creator export")
			owners := []records.RevisionCommitResult{
				seedRecordWithoutSearchProjection(t, ctx, fixture, "Concurrent export owner B"),
				seedRecordWithoutSearchProjection(t, ctx, fixture, "Concurrent export owner C"),
			}
			local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
			creator := seedV2ExportArtifact(t, ctx, fixture, local, original.RecordID)
			content := []byte("exclusive export artifact for V2 deletion")
			digest := sha256.Sum256(content)
			writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal,
				NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
			if err != nil {
				t.Fatal(err)
			}
			jobs := []string{"rej_v1concurrentb", "rej_v1concurrentc"}
			artifacts := []string{"rxa_v1concurrentb", "rxa_v1concurrentc"}
			obligations := make([]string, 0, 2)
			for i, record := range owners {
				var expiry time.Time
				err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs
					(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
					 authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
					values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'markdown','safe','staging',$2,$2,1,$3,$4,
					 'local_export_worker',1,clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`,
					jobs[i], digest[:], record.RecordID, record.RevisionID).Scan(&expiry)
				if err != nil {
					t.Fatal(err)
				}
				written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
					Origin:  attachments.WriteOrigin{Kind: "export_artifact", ID: jobs[i], Generation: 1},
					Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
				if err != nil || written.Version != creator.Version || written.ObligationID == creator.ObligationID || (i > 0 && written.ObligationID == obligations[0]) {
					t.Fatalf("independent concurrent writer %d: %#v, %v", i, written, err)
				}
				obligations = append(obligations, written.ObligationID)
				payload, err := json.Marshal(map[string]any{"artifact_id": artifacts[i], "job_id": jobs[i], "kind": "markdown",
					"content_type": "text/markdown", "backend_kind": "local", "namespace_id": local.blob.Namespace().NamespaceID,
					"key": written.Version.Key, "version": written.Version.VersionID, "sha256": hex.EncodeToString(digest[:]),
					"size": len(content), "expires_at": expiry, "obligation_id": written.ObligationID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := local.pool.Exec(ctx, `select public.record_export_artifact_publish($1)`, payload); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.db.Exec(ctx, `update public.record_export_jobs set job_state='published',owner_id='',owner_generation=0,lease_expires_at=null where export_job_id=$1`, jobs[i]); err != nil {
					t.Fatal(err)
				}
			}
			op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
				OperationID: "rpo_v1concurrentexports", ReservationID: "drs_v1concurrentexports",
				Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: original.RecordID},
				ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
			target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, original, op)
			pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-concurrent-exports", 2)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
				t.Fatal(err)
			}
			worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
			surface := recorddeletion.RecordPortabilitySurfaceDigest()
			receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
			if err != nil {
				t.Fatalf("retain independent export owners: %v", err)
			}
			var evidence int
			err = fixture.db.QueryRow(ctx, `select count(*) from public.record_portability_v1_targets t,
				jsonb_array_elements(t.retained_evidence) e(value) where t.operation_id=$1 and t.disposition='retained_shared'
				and e.value->>0='export_artifact' and e.value->>1=any($2::text[])`, op.OperationID, artifacts).Scan(&evidence)
			if err != nil || evidence != 2 {
				t.Fatalf("independent export survivor evidence=%d: %v", evidence, err)
			}
			firstTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = firstTx.Rollback(context.Background()) }()
			secondTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = secondTx.Rollback(context.Background()) }()
			var firstPID, secondPID int32
			if err := firstTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&firstPID); err != nil {
				t.Fatal(err)
			}
			var snapshotCount int
			if err := secondTx.QueryRow(ctx, `select pg_backend_pid(),count(*) from public.record_export_artifacts where export_artifact_id=any($1::text[])`, artifacts).Scan(&secondPID, &snapshotCount); err != nil || snapshotCount != 2 {
				t.Fatalf("initial independent owners=%d: %v", snapshotCount, err)
			}
			if _, err := firstTx.Exec(ctx, `select 1 from public.record_export_artifacts where export_artifact_id=$1 for update`, artifacts[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := firstTx.Exec(ctx, `delete from public.record_export_artifacts where export_artifact_id=$1`, artifacts[1]); err != nil {
				t.Fatalf("remove first independent artifact: %v", err)
			}
			if _, err := firstTx.Exec(ctx, `delete from public.record_export_jobs where export_job_id=$1`, jobs[1]); err != nil {
				t.Fatalf("remove first independent parent: %v", err)
			}
			resultCh := make(chan error, 1)
			go func() {
				_, err := secondTx.Exec(ctx, `delete from public.record_export_artifacts where export_artifact_id=$1`, artifacts[0])
				if err == nil {
					_, err = secondTx.Exec(ctx, `delete from public.record_export_jobs where export_job_id=$1`, jobs[0])
				}
				if err == nil {
					err = secondTx.Commit(ctx)
				}
				resultCh <- err
			}()
			waitCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			waitForPostgresCollaborationBlocker(t, waitCtx, fixture, secondPID, firstPID)
			stop()
			if err := firstTx.Commit(ctx); err != nil {
				t.Fatalf("commit first independent owner removal: %v", err)
			}
			var denied *pgconn.PgError
			if err := <-resultCh; !errors.As(err, &denied) || (denied.Code != "40001" && denied.Code != "55000") {
				t.Fatalf("second independent owner removal was not rejected: %v", err)
			}
			var remaining int
			if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_export_artifacts where export_artifact_id=any($1::text[])`, artifacts).Scan(&remaining); err != nil || remaining != 1 {
				t.Fatalf("independent export survivors=%d: %v", remaining, err)
			}
			if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
				t.Fatalf("independent owner removal lost retained proof: %v", err)
			}
			if _, err := local.blob.Stat(ctx, creator.Version); err != nil {
				t.Fatalf("independent owner removal changed creator bytes: %v", err)
			}
		})
	}
}

// The completed predecessor has no returned object version. The present
// variant models uncertain final-link recovery after an authentic failed
// return; it is a consumer-recovery fault state, not a normal writer result.
func v1OwnerHandoffAbsentReplacement(t *testing.T, mode string) {
	present := mode == "failed_present_fault_state"
	failed := mode == "failed_absent" || present
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	witness := localBlobFixtureWitness(t, fixture.Runtime)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	content := []byte("replacement after genuine absent predecessor")
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	firstJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID,
		IdempotencyKey: "v1-absent-replacement-original", ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	var originalID string
	if failed {
		if present {
			v1LocalCrashFailedPresentSource(t, ctx, fixture, firstJob.ImportJobID, int64(firstJob.LockVersion), content)
		} else {
			_, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
				Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: firstJob.ImportJobID, Generation: int64(firstJob.LockVersion), ArchiveDigest: &digestText},
				Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content[:len(content)-1]))
			if !errors.Is(err, attachments.ErrBlobSizeMismatch) {
				t.Fatalf("original managed writer did not fail before publication: %v", err)
			}
		}
		if err := fixture.platform.db.QueryRow(ctx, `select obligation_id from public.record_write_obligations
			where origin_kind='import_archive' and origin_id=$1`, firstJob.ImportJobID).Scan(&originalID); err != nil {
			t.Fatal(err)
		}
	} else {
		namespace, incarnation := fixture.LocalCleanup.NamespaceID(), fixture.LocalCleanup.IncarnationID()
		originalID = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
		registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: firstJob.ImportJobID,
			Generation: int64(firstJob.LockVersion), ArchiveDigest: &digestText}, ObligationID: originalID,
			WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1, ProjectID: "default", NamespaceID: &namespace, IncarnationID: &incarnation,
			BackendKind: attachments.BackendKindLocal, Key: "sha256/" + digestText, SHA256: digestText, Size: int64(len(content))}
		if _, err := NewPostgresLocalBlobRepository(fixture.Runtime, witness, nil).RegisterWrite(ctx, registration); err != nil {
			t.Fatal(err)
		}
	}
	var genuine bool
	if err := fixture.platform.db.QueryRow(ctx, `select o.object_version is null and o.registration_command is not null
		and o.cleanup_completed_at is null and o.canonical_key=$2 and o.origin_id=$3
		and case when $4 then o.outcome='failed' and o.dispatch_id is not null and o.dispatched_at is not null
			and o.returned_at is not null and o.return_command is not null and (not $5 or o.temporary_inode is not null)
		else o.outcome='registered' and o.dispatch_id is null and o.dispatched_at is null and o.returned_at is null end
		from public.record_write_obligations o where o.obligation_id=$1`, originalID, "sha256/"+digestText, firstJob.ImportJobID, failed, present).Scan(&genuine); err != nil || !genuine {
		t.Fatalf("original NULL-version registration/return proof=%v: %v", genuine, err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, firstJob, "rec_v1absentreplacement")
	planDigest := sha256.Sum256([]byte("absent predecessor archive plan"))
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: firstJob.ImportJobID, Actor: actor, ArchiveDigest: digest,
		PlanDigest: planDigest, ObjectCount: 1, RemapCount: 1, Allocations: []recordbirth.Allocation{allocation},
		Remaps:             []ImportRemap{{EntityKind: "record", SourceID: "rec_v1absentreplacement", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID}},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_v1absentreplacement", TargetID: allocation.RecordID, Title: "Absent predecessor", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, firstJob.ImportJobID); err != nil {
		t.Fatal(err)
	}
	makeTarget := func(recordID, suffix string) recorddeletion.PurgeTarget {
		t.Helper()
		record := records.RevisionCommitResult{RecordID: recordID}
		var lock, epoch int64
		if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, recordID).
			Scan(&record.RevisionID, &lock, &epoch); err != nil {
			t.Fatal(err)
		}
		record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
			OperationID: "rpo_v1absentreplacement" + suffix, ReservationID: "drs_v1absentreplacement" + suffix,
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		return seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, op)
	}
	first := makeTarget(allocation.RecordID, "first")
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-absent-replacement-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	firstPrep, err := worker.preparePortabilityV1(ctx, first, surface)
	if err != nil || firstPrep.UnresolvedCount != 0 || len(firstPrep.LocalTargets) != 1 || firstPrep.LocalTargets[0].ExpectedVersion != "local-v2-"+originalID {
		t.Fatalf("absent original preparation=%#v: %v", firstPrep, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, first.Operation.OperationID, firstPrep.InventoryDigest)
	firstReceipt, err := worker.PurgeRecordPortability(ctx, first, surface)
	if err != nil {
		t.Fatalf("complete absent original physical proof: %v", err)
	}
	var originalTask, originalSource, originalReceipt string
	var originalProof bool
	err = fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text,to_jsonb(r)::text,
		o.object_version is null and o.cleanup_completed_at is not null and o.quota_settled_at is not null
		and c.snapshot->>'object_version' is null and t.object_version_id is null
		and c.snapshot->>'outcome'=case when $3 then 'failed' else 'registered' end
		and o.outcome=case when $3 then 'failed' else 'local_excluded' end
		and record_platform_internal.record_portability_v1_local_source_expected(to_jsonb(o),o.obligation_id)='local-v2-'||o.obligation_id
		and record_platform_internal.record_portability_v1_local_source_expected(c.snapshot,c.obligation_id)='local-v2-'||o.obligation_id
		and g.deletion_state='completed' and g.final_identity_bound and g.final_was_present=$4
		and g.object_version is not distinct from (case when $4 then 'local-v2-'||o.obligation_id end)
		and (g.final_device is not null)=$4 and (g.final_inode is not null)=$4
		and g.physical_delete_result=(case when $4 then 'deleted' else 'already_absent' end)
		and l.seal_obligation and o.cleanup_receipt=g.receipt_digest
		and record_platform_internal.record_portability_v1_local_task_proof(g)
		and not exists(select 1 from public.record_portability_v1_artifacts a where a.operation_id=c.operation_id)
		from public.record_portability_v1_targets t join public.record_portability_v1_obligations c using(operation_id,target_id)
		join public.record_write_obligations o on o.obligation_id=c.obligation_id
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=o.obligation_id
		join public.record_portability_purge_receipts r on r.operation_id=t.operation_id
		where t.operation_id=$1 and o.obligation_id=$2`, first.Operation.OperationID, originalID, failed, present).
		Scan(&originalTask, &originalSource, &originalReceipt, &originalProof)
	if err != nil || !originalProof {
		t.Fatalf("completed NULL-version original proof=%v: %v", originalProof, err)
	}
	if present {
		root := fixture.Blobs.(*attachments.LocalBlobStore).Namespace().RootPath
		for _, name := range []string{"sha256/" + digestText, "sha256/.blob-tmp-" + originalID} {
			if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("completed failed-present predecessor left %s: %v", name, err)
			}
		}
	}
	secondJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID,
		IdempotencyKey: "v1-absent-replacement-new", ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: secondJob.ImportJobID, Generation: int64(secondJob.LockVersion), ArchiveDigest: &digestText},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil || written.ObligationID == originalID || written.Version.Key != "sha256/"+digestText || written.Version.VersionID != "local-v2-"+written.ObligationID {
		t.Fatalf("write genuine replacement version=%#v: %v", written, err)
	}
	var sameLocation bool
	if err := fixture.platform.db.QueryRow(ctx, `select
		(a.namespace_id,a.incarnation_id,a.canonical_key,a.sha256_digest,a.size_bytes)
		is not distinct from (b.namespace_id,b.incarnation_id,b.canonical_key,b.sha256_digest,b.size_bytes)
		and a.object_version is null and b.object_version=$3 and b.outcome='materialized'
		and b.return_command is not null and (convert_from(b.return_command,'UTF8')::jsonb)->>'version'=b.object_version
		from public.record_write_obligations a join public.record_write_obligations b on b.obligation_id=$2 where a.obligation_id=$1`,
		originalID, written.ObligationID, written.Version.VersionID).Scan(&sameLocation); err != nil || !sameLocation || written.NamespaceID != fixture.LocalCleanup.NamespaceID() {
		t.Fatalf("replacement did not use the same authenticated location=%v: %v", sameLocation, err)
	}
	// The import fixture keeps its live membership gate on fixture.Gate;
	// fixture.platform is only its database setup/readback handle.
	fixture.RefreshHeartbeat()
	recordRepository := newRevisionMutationAdmittedRepository(t, fixture)
	replacement, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, fixture,
		recordsPostgresCompleteRevisionInput(t, "Replacement after absent local source"), "v1-absent-replacement-revision"))
	if err != nil {
		t.Fatalf("create admitted replacement record: %v", err)
	}
	var expiry time.Time
	if err := fixture.platform.db.QueryRow(ctx, `select expires_at from public.record_import_jobs where import_job_id=$1`, secondJob.ImportJobID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"artifact_id": "ria_v1absentreplacement", "job_id": secondJob.ImportJobID, "kind": "archive",
		"backend_kind": "local", "namespace_id": fixture.LocalCleanup.NamespaceID(), "key": written.Version.Key,
		"version": written.Version.VersionID, "sha256": digestText, "size": len(content), "expires_at": expiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Runtime.Exec(ctx, `select public.record_import_artifact_publish($1)`, payload); err != nil {
		t.Fatalf("publish replacement archive: %v", err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `insert into public.record_import_plans(import_plan_id,import_job_id,plan_digest,object_count,remap_count,expires_at)
		values('rip_v1absentreplacement',$1,$2,1,0,clock_timestamp()+interval '1 hour')`, secondJob.ImportJobID, digest[:]); err != nil {
		t.Fatal(err)
	}
	identity := sha256.Sum256([]byte(replacement.RecordID))
	if _, err := fixture.platform.db.Exec(ctx, `insert into public.record_import_entity_mappings(import_plan_id,entity_kind,source_id,source_identity_digest,target_id)
		values('rip_v1absentreplacement','record',$1,$2,$1)`, replacement.RecordID, identity[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, secondJob.ImportJobID); err != nil {
		t.Fatal(err)
	}
	// Charge the measured B inode once so this case exercises quota settlement.
	if _, err := fixture.platform.db.Exec(ctx, `insert into public.attachment_quota_accounts(project_id) values('default') on conflict (project_id) do nothing`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
		values($1,$2,$3,$4,'local',$5)`, written.Version.Key, written.Version.SHA256[:], written.Version.VersionID, written.Version.SizeBytes, fixture.LocalCleanup.NamespaceID()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=physical_bytes+$1 where project_id='default'`, written.Version.SizeBytes); err != nil {
		t.Fatal(err)
	}
	quotaBefore := v1OwnerHandoffReadQuota(t, ctx, fixture.platform)
	second := makeTarget(replacement.RecordID, "second")
	secondPrep, err := worker.preparePortabilityV1(ctx, second, surface)
	if err != nil || secondPrep.UnresolvedCount != 0 || len(secondPrep.LocalTargets) != 1 || secondPrep.LocalTargets[0].ExpectedVersion != written.Version.VersionID {
		t.Fatalf("prepare replacement after absent source=%#v: %v", secondPrep, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, second.Operation.OperationID, secondPrep.InventoryDigest)
	secondTask := secondPrep.LocalTargets[0].DeletionID
	var exact bool
	err = fixture.platform.db.QueryRow(ctx, `select count(*)=1 and bool_and(l.obligation_id=$2 and l.seal_obligation)
		and not exists(select 1 from public.local_blob_gc_obligations x where x.deletion_id=$1 and x.obligation_id=$3)
		from public.local_blob_gc_obligations l where l.deletion_id=$1`, secondTask, written.ObligationID, originalID).Scan(&exact)
	if err != nil || !exact || secondTask == firstPrep.LocalTargets[0].DeletionID {
		t.Fatalf("pending replacement links exactly B=%v: %v", exact, err)
	}
	secondReceipt, err := worker.PurgeRecordPortability(ctx, second, surface)
	if err != nil {
		t.Fatalf("claim, bind and complete replacement after absent source: %v", err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("replacement inode remains: %v", err)
	}
	err = fixture.platform.db.QueryRow(ctx, `select g.deletion_state='completed' and g.physical_delete_result='deleted'
		and g.final_identity_bound and g.final_was_present and g.object_version=$4 and g.cleanup_dispatch_id is not null
		and record_platform_internal.record_portability_v1_local_task_proof(g)
		and o.cleanup_completed_at is not null and o.quota_settled_at is not null and o.cleanup_receipt=g.receipt_digest
		and (select count(*)=1 and bool_and(l.obligation_id=$2 and l.seal_obligation) from public.local_blob_gc_obligations l where l.deletion_id=g.deletion_id)
		and not exists(select 1 from public.local_blob_gc_obligations l where l.deletion_id=g.deletion_id and l.obligation_id=$3)
		from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2 where g.deletion_id=$1`,
		secondTask, written.ObligationID, originalID, written.Version.VersionID).Scan(&exact)
	if err != nil || !exact {
		t.Fatalf("completed replacement physical proof=%v: %v", exact, err)
	}
	quotaAfter := v1OwnerHandoffReadQuota(t, ctx, fixture.platform)
	expectedQuota := quotaBefore
	expectedQuota.physical -= written.Version.SizeBytes
	expectedQuota.version++
	if quotaAfter != expectedQuota {
		t.Fatalf("replacement quota settlement before=%#v after=%#v expected=%#v", quotaBefore, quotaAfter, expectedQuota)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, second, surface, secondReceipt); err != nil {
		t.Fatalf("verify replacement receipt: %v", err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, second, surface); err != nil || replay != secondReceipt {
		t.Fatalf("replacement receipt replay=%#v: %v", replay, err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, first, surface, firstReceipt); err != nil {
		t.Fatalf("verify original absent receipt after replacement: %v", err)
	}
	var taskAfter, sourceAfter, receiptAfter string
	err = fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text,to_jsonb(o)::text,to_jsonb(r)::text
		from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2
		join public.record_portability_purge_receipts r on r.operation_id=$3 where g.deletion_id=$1`,
		firstPrep.LocalTargets[0].DeletionID, originalID, first.Operation.OperationID).Scan(&taskAfter, &sourceAfter, &receiptAfter)
	if err != nil || taskAfter != originalTask || sourceAfter != originalSource || receiptAfter != originalReceipt {
		t.Fatalf("replacement changed original task/source/receipt: %v", err)
	}
	if replayQuota := v1OwnerHandoffReadQuota(t, ctx, fixture.platform); replayQuota != quotaAfter {
		t.Fatalf("replacement replay changed quota: after=%#v replay=%#v", quotaAfter, replayQuota)
	}
}

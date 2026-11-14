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
	"houfeng/internal/center/records"
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
	registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &digest},
		ObligationID: "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", ""), WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1,
		ProjectID: "default", BackendKind: attachments.BackendKindS3, Key: "sha256/" + digest, SHA256: digest, Size: int64(len("witnessed import archive"))}
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
	registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: fixture.job.ImportJobID,
		Generation: int64(fixture.job.LockVersion), ArchiveDigest: &digest},
		ObligationID: "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", ""), WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1,
		ProjectID: "default", BackendKind: attachments.BackendKindS3, Key: "sha256/" + digest, SHA256: digest, Size: int64(len("import-dispatch-test-archive"))}
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

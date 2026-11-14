package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
)

// The catalog's authority principal is cluster-wide. Never overwrite a role
// belonging to another fixture, and release ownership only after all cleanup.
var importAttachmentAuthorityMu sync.Mutex

// ImportAttachmentGCFixture exposes constrained runtime access and narrowly
// typed setup/control operations, never a privileged pool or SQL executor.
type ImportAttachmentGCFixture struct {
	// Internal tests use this only for fixture setup and readback; exported
	// consumers continue to receive the constrained direct-runtime pool.
	platform               recordPlatformPostgresFixture
	Runtime                *pgxpool.Pool
	Gate                   AdmissionGate
	Backend                attachments.BackendKind
	Blobs                  attachments.BlobStore
	Writer                 attachments.BlobWriter
	LocalCleanup           *attachments.LocalCleanupCoordinator
	SeedBlob               func([]byte) attachments.WrittenBlob
	SeedLegacyS3Blob       func([]byte) attachments.ObjectVersion
	Deleter                attachments.BlobGCDeleter
	Repository             *PostgresAttachmentRepository
	RefreshHeartbeat       func()
	SeedOrphan             func(attachments.ObjectVersion)
	SeedSource             func(attachments.ObjectVersion) func(context.Context, attachments.BlobGCDeleter) error
	BlockNextClaimCommit   func() *ImportAttachmentCommitBarrier
	BlockNextImportCommit  func() *ImportAttachmentCommitBarrier
	BlockLocalPreparation  func() *ImportAttachmentCommitBarrier
	BlockLocalPhysical     func() *ImportAttachmentCommitBarrier
	SetQuota               func(logical, reserved, physical, version int64)
	FailNextObservation    func()
	CommitImported         func(context.Context, string, [][]attachments.ImportedAvailableAttachment, []recordbirth.Allocation) error
	WaitForGCSerialization func(context.Context) error
}

type ImportAttachmentCommitBarrier struct {
	Entered <-chan struct{}
	Release chan struct{}
}

type importAttachmentCommitTx struct {
	pgx.Tx
	entered chan struct{}
	release <-chan struct{}
}

func (tx *importAttachmentCommitTx) Commit(ctx context.Context) error {
	close(tx.entered)
	select {
	case <-tx.release:
		return tx.Tx.Commit(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// importAttachmentBeforeC81Tracer seeds an explicitly requested historical
// fixture inside the real migrator transaction, immediately before C81. The
// seed receives the deployment this fixture activates, as C80 rows carried it.
type importAttachmentBeforeC81Tracer struct {
	migration  string
	deployment recordplatform.DeploymentID
	seed       func(context.Context, *pgx.Conn, recordplatform.DeploymentID) error
	seen       bool
	err        error
}

func (tracer *importAttachmentBeforeC81Tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL != tracer.migration {
		return ctx
	}
	if tracer.seen {
		tracer.err = fmt.Errorf("historical seed reached C81 more than once")
	} else {
		tracer.seen = true
		tracer.err = tracer.seed(ctx, conn, tracer.deployment)
		if tracer.err == nil {
			if _, err := conn.Exec(ctx, `set constraints all immediate`); err != nil {
				tracer.err = fmt.Errorf("finish predecessor deferred constraints: %w", err)
			}
		}
	}
	if tracer.err != nil {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	return ctx
}

func (*importAttachmentBeforeC81Tracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func NewImportAttachmentGCBackendFixture(t *testing.T, ctx context.Context, backend attachments.BackendKind, repeatableRead bool) ImportAttachmentGCFixture {
	t.Helper()
	return newImportAttachmentGCBackendFixture(t, ctx, backend, repeatableRead, nil)
}

func newImportAttachmentGCBackendFixture(t *testing.T, ctx context.Context, backend attachments.BackendKind, repeatableRead bool, beforeC81 func(context.Context, *pgx.Conn, recordplatform.DeploymentID) error) ImportAttachmentGCFixture {

	t.Helper()
	importAttachmentAuthorityMu.Lock()
	t.Cleanup(importAttachmentAuthorityMu.Unlock)
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	stateRoot := filepath.Join(t.TempDir(), "authority")
	state, err := recordauthority.CreateComposeState(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	const authorityRole = "houfeng_records_authority"
	var authorityExists bool
	if err := fixture.db.QueryRow(ctx, `select pg_catalog.to_regrole($1) is not null`, authorityRole).Scan(&authorityExists); err != nil {
		t.Fatal(err)
	}
	if authorityExists {
		t.Fatal("owned import fixture requires absent authority role")
	}
	if err := platformmigrate.ProvisionComposeBootstrap(ctx, fixture.db, platformmigrate.ComposeBootstrapConfig{
		DatabaseName: fixture.databaseName, BootstrapRole: fixture.bootstrapOwner, AuthorityRole: authorityRole,
		Roles:     platformmigrate.AppRoleSetV2{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, DeletionWorker: fixture.deletionWorker, Migrator: fixture.migrator},
		Passwords: platformmigrate.ComposeRolePasswords{Runtime: fixture.passwords[fixture.runtime], PlatformAdmin: fixture.passwords[fixture.admin], DeletionWorker: fixture.passwords[fixture.deletionWorker], Migrator: fixture.passwords[fixture.migrator], Authority: state.DatabasePassword()},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.passwords[authorityRole] = state.DatabasePassword()
	fixture.dropRole(t, authorityRole)
	var migrator *pgxpool.Pool
	var tracer *importAttachmentBeforeC81Tracer
	if beforeC81 == nil {
		migrator = fixture.openDirectRolePool(t, ctx, fixture.migrator, "import-gc-migrator", 1)
	} else {
		migration, err := fs.ReadFile(migrations.FS, "0081_record_deletion_successor.sql")
		if err != nil {
			t.Fatalf("read C81 for historical fixture: %v", err)
		}
		tracer = &importAttachmentBeforeC81Tracer{migration: string(migration), deployment: state.DeploymentID, seed: beforeC81}
		config := fixture.directRoleConfig(t, fixture.migrator, "import-gc-migrator")
		config.MaxConns = 1
		config.ConnConfig.Tracer = tracer
		migrator, err = pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatalf("open historical fixture migrator: %v", err)
		}
		t.Cleanup(migrator.Close)
	}
	_, err = storemigrate.ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker)
	if tracer != nil && tracer.err != nil {
		t.Fatalf("seed pre-C81 historical fixture: %v", tracer.err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if tracer != nil && !tracer.seen {
		t.Fatal("historical fixture did not reach the embedded C81 migration")
	}
	command, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var receipt []byte
	if err := migrator.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, command).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if err := recordauthority.PersistActivationReceipt(stateRoot, state, receipt); err != nil {
		t.Fatal(err)
	}
	authority := fixture.openDirectRolePool(t, ctx, authorityRole, "import-gc-authority", 1)
	refresh := func() {
		t.Helper()
		command, want, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		var got time.Time
		if err := authority.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, command).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Fatalf("heartbeat expiry = %s, want %s", got, want)
		}
	}
	refresh()
	runtime := fixture.openDirectRuntimePool(t, ctx, "import-gc-runtime", 8)
	if repeatableRead {
		config := runtime.Config()
		config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
		runtime, err = pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(runtime.Close)
	}
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	var sessionUser, currentUser, defaultIsolation string
	if err := runtime.QueryRow(ctx, `select session_user, current_user, current_setting('default_transaction_isolation')`).Scan(&sessionUser, &currentUser, &defaultIsolation); err != nil {
		t.Fatal(err)
	}
	if sessionUser != fixture.runtime || currentUser != fixture.runtime {
		t.Fatal("fixture did not use a direct constrained runtime")
	}
	if repeatableRead && defaultIsolation != "repeatable read" {
		t.Fatalf("default isolation = %q", defaultIsolation)
	}
	gate, err := NewDeploymentMembershipAdmissionGate(DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: state.DeploymentID, ProjectID: recordplatform.ProjectIDDefault,
		InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	var blob attachments.BlobStore
	var deleter attachments.BlobGCDeleter
	switch backend {
	case attachments.BackendKindLocal:
		local, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
		if err != nil {
			t.Fatal(err)
		}
		blob, deleter = local, local
	case attachments.BackendKindS3:
		client, bucket := newAttachmentUploadWorkflowMinIO(t)
		s3, err := attachments.NewS3BlobStore(client, bucket)
		if err != nil {
			t.Fatal(err)
		}
		blob, deleter = s3, s3
	default:
		t.Fatalf("unsupported fixture backend %q", backend)
	}
	managedRepository := NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil)
	writer, err := attachments.NewManagedBlobWriter(blob, backend, managedRepository)
	if err != nil {
		t.Fatal(err)
	}
	var localCleanup *attachments.LocalCleanupCoordinator
	localBarriers := &importLocalCleanupBarriers{}
	if local, ok := blob.(*attachments.LocalBlobStore); ok {
		localCleanup, err = attachments.NewLocalCleanupCoordinator(local, &importLocalCleanupRepository{PostgresLocalBlobRepository: managedRepository, barriers: localBarriers})
		if err != nil {
			t.Fatal(err)
		}
	}
	repository := NewPostgresAttachmentRepository(runtime)
	var barrierMu sync.Mutex
	var nextBarrier *importAttachmentCommitTx
	var nextImportBarrier *importAttachmentCommitTx
	var failObservation bool
	repository.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
		tx, err := runtime.BeginTx(ctx, options)
		if err != nil {
			return nil, err
		}
		barrierMu.Lock()
		barrier, fail := nextBarrier, failObservation
		nextBarrier, failObservation = nil, false
		barrierMu.Unlock()
		if fail {
			// A genuine server-side failure aborts this real runtime transaction.
			// The subsequent history read must propagate the database error.
			if _, err := tx.Exec(ctx, `select 1 / 0`); err == nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("expected division-by-zero setup failure")
			}
		}
		if barrier != nil {
			barrier.Tx = tx
			return barrier, nil
		}
		return tx, nil
	}
	seedOrphan := func(object attachments.ObjectVersion) {
		metadata := attachmentBlobObjectFromVersion(object, backend)
		metadata.NamespaceID = writer.NamespaceID()
		seedAttachmentBlobGCObject(t, ctx, fixture, metadata, time.Now().UTC().Add(-48*time.Hour))
	}
	seedRecordsPostgresUsers(t, ctx, fixture)
	if _, err := fixture.db.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status,usage_status) values($1,'PostgreSQL VPS','active','in_use') on conflict(vps_id) do nothing`, testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	if beforeC81 != nil {
		// Preserve reopenable pools for cleanup and heartbeat while allowing the
		// runtime to observe every session for settled activity heads.
		migrator.Reset()
		authority.Reset()
	}
	return ImportAttachmentGCFixture{
		platform: fixture,
		Runtime:  runtime, Gate: gate, Backend: backend, Blobs: blob, Deleter: deleter, Repository: repository, RefreshHeartbeat: refresh,
		Writer: writer, LocalCleanup: localCleanup,
		BlockLocalPreparation: func() *ImportAttachmentCommitBarrier { return localBarriers.arm(t, true) },
		BlockLocalPhysical:    func() *ImportAttachmentCommitBarrier { return localBarriers.arm(t, false) },
		SeedBlob: func(content []byte) attachments.WrittenBlob {
			return seedPostgresManagedBlob(t, ctx, fixture, writer, content)
		},
		// Seed fixed legacy-style S3 bytes in a never-enrolled test APP. They
		// have no managed-write history; this is not a source admission proof.
		// Import writes still use the managed writer and retain their obligations.
		SeedLegacyS3Blob: func(content []byte) attachments.ObjectVersion {
			if backend != attachments.BackendKindS3 {
				t.Fatal("legacy S3 seed requires S3 backend")
			}
			var neverEnrolled bool
			if err := fixture.db.QueryRow(ctx, `select not exists(select 1 from public.source_authority_admission)`).Scan(&neverEnrolled); err != nil || !neverEnrolled {
				t.Fatalf("legacy S3 seed requires never-enrolled APP: %t %v", neverEnrolled, err)
			}
			digest := sha256.Sum256(content)
			temporary, err := attachments.NewBlobTemporaryKey()
			if err != nil {
				t.Fatal(err)
			}
			version, err := blob.Put(ctx, attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)), TemporaryKey: temporary}, bytes.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			return version
		},
		SeedOrphan: seedOrphan,
		SeedSource: func(object attachments.ObjectVersion) func(context.Context, attachments.BlobGCDeleter) error {
			seedRecordsPostgresUsers(t, ctx, fixture)
			if _, err := runtime.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status,usage_status) values($1,'PostgreSQL VPS','active','in_use') on conflict(vps_id) do nothing`, testStoreRecordVPSID); err != nil {
				t.Fatal(err)
			}
			birthFixture := ImportAttachmentGCFixture{Runtime: runtime, Gate: gate}
			recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
			command := revisionBirthBatchCommand(t, ctx, birthFixture, recordsPostgresCompleteRevisionInput(t, "GC source"), "import-gc-source")
			record, err := recordRepository.CommitRevision(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			seedOrphan(object)
			if _, err := fixture.db.Exec(ctx, `insert into public.record_attachments (attachment_id, project_id, record_id, attachment_state, display_name, media_type, logical_size_bytes, blob_key, blob_object_version, created_by) values ('att_gcsource', 'default', $1, 'available', 'fence.txt', 'text/plain', $2, $3, $4, 'usr_records1')`, record.RecordID, object.SizeBytes, object.Key, object.VersionID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.db.Exec(ctx, `insert into public.record_revision_attachments (record_id, revision_id, ordinal, attachment_id) values ($1, $2, 0, 'att_gcsource')`, record.RecordID, record.RevisionID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.db.Exec(ctx, `update public.attachment_quota_accounts set logical_bytes=$1 where project_id='default'`, object.SizeBytes); err != nil {
				t.Fatal(err)
			}
			operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_importgc", ReservationID: "drs_importgc", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: testStoreRecordPlatformDigest(0x92)}
			if local, ok := blob.(*attachments.LocalBlobStore); ok {
				operation.ContractVersion = recorddeletion.DeletionContractV2
				operation.LedgerSequence, operation.LedgerEntryHash = 0, [32]byte{}
				claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
				workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "import-local-purge", 2)
				workerRepository := NewPostgresAttachmentRepository(workerPool)
				workerRepository.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
					tx, err := workerPool.BeginTx(ctx, options)
					if err != nil {
						return nil, err
					}
					if barrier := localBarriers.take(true); barrier != nil {
						barrier.Tx = tx
						return barrier, nil
					}
					return tx, nil
				}
				coordinator, err := attachments.NewLocalCleanupCoordinator(local, &importLocalCleanupRepository{PostgresLocalBlobRepository: NewPostgresLocalBlobRepository(workerPool, nil, nil), barriers: localBarriers})
				if err != nil {
					t.Fatal(err)
				}
				if err := workerRepository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
					t.Fatal(err)
				}
				adapter, err := attachments.NewDeletionAdapter(workerRepository)
				if err != nil {
					t.Fatal(err)
				}
				return func(ctx context.Context, _ attachments.BlobGCDeleter) error {
					target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
					receipt, err := adapter.PurgeDeletion(ctx, target)
					if err != nil {
						return err
					}
					return adapter.VerifyDeletion(ctx, target, receipt)
				}
			}
			target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
			workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "import-s3-purge", 2)
			workerRepository := NewPostgresAttachmentRepository(workerPool)
			workerRepository.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
				tx, err := workerPool.BeginTx(ctx, options)
				if err != nil {
					return nil, err
				}
				barrierMu.Lock()
				barrier := nextBarrier
				nextBarrier = nil
				barrierMu.Unlock()
				if barrier != nil {
					barrier.Tx = tx
					return barrier, nil
				}
				return tx, nil
			}
			return func(ctx context.Context, deleter attachments.BlobGCDeleter) error {
				if err := workerRepository.ConfigureAttachmentDeletionBlobStore(backend, deleter); err != nil {
					return err
				}
				adapter, err := attachments.NewDeletionAdapter(workerRepository)
				if err != nil {
					return err
				}
				receipt, err := adapter.PurgeDeletion(ctx, target)
				if err != nil {
					return err
				}
				return adapter.VerifyDeletion(ctx, target, receipt)
			}
		},
		BlockNextClaimCommit: func() *ImportAttachmentCommitBarrier {
			barrierMu.Lock()
			defer barrierMu.Unlock()
			if nextBarrier != nil {
				t.Fatal("claim commit barrier already armed")
			}
			entered, release := make(chan struct{}), make(chan struct{})
			nextBarrier = &importAttachmentCommitTx{entered: entered, release: release}
			return &ImportAttachmentCommitBarrier{Entered: entered, Release: release}
		},
		BlockNextImportCommit: func() *ImportAttachmentCommitBarrier {
			barrierMu.Lock()
			defer barrierMu.Unlock()
			if nextImportBarrier != nil {
				t.Fatal("import commit barrier already armed")
			}
			entered, release := make(chan struct{}), make(chan struct{})
			nextImportBarrier = &importAttachmentCommitTx{entered: entered, release: release}
			return &ImportAttachmentCommitBarrier{Entered: entered, Release: release}
		},
		SetQuota: func(logical, reserved, physical, version int64) {
			if _, err := fixture.db.Exec(ctx, `insert into public.attachment_quota_accounts (project_id, logical_bytes, reserved_bytes, physical_bytes, quota_version) values ('default',$1,$2,$3,$4) on conflict (project_id) do update set logical_bytes=$1,reserved_bytes=$2,physical_bytes=$3,quota_version=$4`, logical, reserved, physical, version); err != nil {
				t.Fatal(err)
			}
		},
		FailNextObservation: func() { barrierMu.Lock(); failObservation = true; barrierMu.Unlock() },
		CommitImported: func(ctx context.Context, path string, batches [][]attachments.ImportedAvailableAttachment, allocations []recordbirth.Allocation) error {
			repository, err := NewPostgresRecordRepository(runtime, gate, []records.RevisionParticipant{NewRecordAttachmentRevisionParticipant()}, newRecordsPostgresAuthorization(t, runtime, gate), nil)
			if err != nil {
				return err
			}
			repository.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				tx, err := runtime.BeginTx(ctx, options)
				if err != nil {
					return nil, err
				}
				// Authority prefetch commits a separate read-only transaction before
				// the writer has acquired quota or Blob locks.
				if options.AccessMode != pgx.ReadOnly {
					barrierMu.Lock()
					barrier := nextImportBarrier
					nextImportBarrier = nil
					barrierMu.Unlock()
					if barrier != nil {
						barrier.Tx = tx
						return barrier, nil
					}
				}
				return tx, nil
			}
			commands := make([]records.RevisionCommitCommand, len(batches))
			if len(allocations) < len(batches) {
				return fmt.Errorf("missing import allocations")
			}
			actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
			if err != nil {
				return err
			}
			for i, items := range batches {
				ids := make([]string, 0, len(items))
				seen := make(map[string]bool, len(items))
				for _, item := range items {
					if !seen[item.AttachmentID] {
						ids = append(ids, item.AttachmentID)
						seen[item.AttachmentID] = true
					}
				}
				commands[i] = recordsPostgresRevisionCommandForActor(t, recordplatform.OperationKindRecordCreate, allocations[i].RecordID, "", 0, 0, recordsPostgresCompleteRevisionInput(t, "GC commit", ids...), fmt.Sprintf("gc-commit-%d", i), actor)
				commands[i] = prepareRevisionBirthCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtime, Gate: gate}, allocations[i], commands[i])
				commands[i].ImportedAttachments = items
			}
			if path == "CommitRevision" {
				_, err = repository.CommitRevision(ctx, commands[0])
				return err
			}
			if path == "CommitRevisions" {
				_, err = repository.CommitRevisions(ctx, commands)
				return err
			}
			return fmt.Errorf("unsupported typed commit path %q", path)
		},
		WaitForGCSerialization: func(ctx context.Context) error {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				if err := runtime.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_locks locks join pg_catalog.pg_stat_activity activity on activity.pid=locks.pid where activity.datname=current_database() and activity.usename in (current_user,$1) and not locks.granted)`, fixture.deletionWorker).Scan(&waiting); err != nil {
					return err
				}
				if waiting {
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
		},
	}
}

type importLocalCleanupBarriers struct {
	mu          sync.Mutex
	preparation *importAttachmentCommitTx
	physical    *importAttachmentCommitTx
}

func (barriers *importLocalCleanupBarriers) arm(t *testing.T, preparation bool) *ImportAttachmentCommitBarrier {
	t.Helper()
	barriers.mu.Lock()
	defer barriers.mu.Unlock()
	selected := &barriers.physical
	if preparation {
		selected = &barriers.preparation
	}
	if *selected != nil {
		t.Fatal("local cleanup barrier already armed")
	}
	release := make(chan struct{})
	barrier := &importAttachmentCommitTx{entered: make(chan struct{}), release: release}
	*selected = barrier
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	return &ImportAttachmentCommitBarrier{Entered: barrier.entered, Release: release}
}

func (barriers *importLocalCleanupBarriers) take(preparation bool) *importAttachmentCommitTx {
	barriers.mu.Lock()
	defer barriers.mu.Unlock()
	selected := &barriers.physical
	if preparation {
		selected = &barriers.preparation
	}
	barrier := *selected
	*selected = nil
	return barrier
}

type importLocalCleanupRepository struct {
	*PostgresLocalBlobRepository
	barriers *importLocalCleanupBarriers
}

func (repository *importLocalCleanupRepository) PrepareLocalCleanup(ctx context.Context, request attachments.LocalCleanupPreparation) (attachments.LocalCleanupTask, error) {
	barrier := repository.barriers.take(true)
	if barrier == nil {
		return repository.PostgresLocalBlobRepository.PrepareLocalCleanup(ctx, request)
	}
	var task attachments.LocalCleanupTask
	body, err := json.Marshal(request)
	if err != nil {
		return task, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return task, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	statement := `select to_jsonb(g) from public.local_blob_gc_prepare($1) g`
	if request.OperationID != nil {
		statement = `select to_jsonb(g) from public.local_blob_purge_prepare($1) g`
	}
	var encoded []byte
	if err := tx.QueryRow(ctx, statement, body).Scan(&encoded); err != nil {
		return task, err
	}
	if err := json.Unmarshal(encoded, &task); err != nil {
		return task, err
	}
	if err := normalizeLocalCleanupDigests(&task); err != nil {
		return task, err
	}
	barrier.Tx = tx
	if err := barrier.Commit(ctx); err != nil {
		return task, err
	}
	return task, nil
}

func (repository *importLocalCleanupRepository) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	snapshot, err := repository.PostgresLocalBlobRepository.BindLocalCleanupFinal(ctx, request)
	if err != nil {
		return snapshot, err
	}
	if barrier := repository.barriers.take(false); barrier != nil {
		close(barrier.entered)
		select {
		case <-barrier.release:
		case <-ctx.Done():
			return snapshot, ctx.Err()
		}
	}
	return snapshot, nil
}

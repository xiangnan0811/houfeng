package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
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
	Runtime                *pgxpool.Pool
	Gate                   AdmissionGate
	Backend                attachments.BackendKind
	Blobs                  attachments.BlobStore
	Deleter                attachments.BlobGCDeleter
	Repository             *PostgresAttachmentRepository
	RefreshHeartbeat       func()
	SeedOrphan             func(attachments.ObjectVersion)
	SeedSource             func(attachments.ObjectVersion) func(context.Context, attachments.BlobGCDeleter) error
	BlockNextClaimCommit   func() *ImportAttachmentCommitBarrier
	BlockNextImportCommit  func() *ImportAttachmentCommitBarrier
	SetQuota               func(logical, reserved, physical, version int64)
	FailNextObservation    func()
	CommitImported         func(context.Context, string, [][]attachments.ImportedAvailableAttachment) error
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

func NewImportAttachmentGCBackendFixture(t *testing.T, ctx context.Context, backend attachments.BackendKind, repeatableRead bool) ImportAttachmentGCFixture {
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
		Roles:     platformmigrate.AppRoleSetV1{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, Migrator: fixture.migrator},
		Passwords: platformmigrate.ComposeRolePasswords{Runtime: fixture.passwords[fixture.runtime], PlatformAdmin: fixture.passwords[fixture.admin], Migrator: fixture.passwords[fixture.migrator], Authority: state.DatabasePassword()},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.passwords[authorityRole] = state.DatabasePassword()
	fixture.dropRole(t, authorityRole)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator, "import-gc-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatal(err)
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
		local, err := attachments.NewLocalBlobStore(t.TempDir())
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
		seedAttachmentBlobGCObject(t, ctx, fixture, attachmentBlobObjectFromVersion(object, backend), time.Now().UTC().Add(-48*time.Hour))
	}
	return ImportAttachmentGCFixture{
		Runtime: runtime, Gate: gate, Backend: backend, Blobs: blob, Deleter: deleter, Repository: repository, RefreshHeartbeat: refresh,
		SeedOrphan: seedOrphan,
		SeedSource: func(object attachments.ObjectVersion) func(context.Context, attachments.BlobGCDeleter) error {
			recordRepository, err := NewPostgresRecordRepository(runtime, gate, nil)
			if err != nil {
				t.Fatal(err)
			}
			record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate, "rec_gcsource", "", 0, 0, recordsPostgresCompleteRevisionInput(t, "GC source"), "import-gc-source"))
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
			operation := recorddeletion.DeletionOperation{OperationID: "rpo_importgc", ReservationID: "drs_importgc", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: testStoreRecordPlatformDigest(0x92)}
			seedAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
			return func(ctx context.Context, deleter attachments.BlobGCDeleter) error {
				if err := repository.ConfigureAttachmentDeletionBlobStore(backend, deleter); err != nil {
					return err
				}
				adapter, err := attachments.NewDeletionAdapter(repository)
				if err != nil {
					return err
				}
				target := recorddeletion.PurgeTarget{Operation: operation}
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
		CommitImported: func(ctx context.Context, path string, batches [][]attachments.ImportedAvailableAttachment) error {
			repository, err := NewPostgresRecordRepository(runtime, gate, []records.RevisionParticipant{NewRecordAttachmentRevisionParticipant()})
			if err != nil {
				return err
			}
			repository.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				tx, err := runtime.BeginTx(ctx, options)
				if err != nil {
					return nil, err
				}
				barrierMu.Lock()
				barrier := nextImportBarrier
				nextImportBarrier = nil
				barrierMu.Unlock()
				if barrier != nil {
					barrier.Tx = tx
					return barrier, nil
				}
				return tx, nil
			}
			commands := make([]records.RevisionCommitCommand, len(batches))
			for i, items := range batches {
				ids := make([]string, 0, len(items))
				seen := make(map[string]bool, len(items))
				for _, item := range items {
					if !seen[item.AttachmentID] {
						ids = append(ids, item.AttachmentID)
						seen[item.AttachmentID] = true
					}
				}
				commands[i] = recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate, fmt.Sprintf("rec_gccommit%d", i), "", 0, 0, recordsPostgresCompleteRevisionInput(t, "GC commit", ids...), fmt.Sprintf("gc-commit-%d", i))
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
				if err := runtime.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_locks locks join pg_catalog.pg_stat_activity activity on activity.pid=locks.pid where activity.datname=current_database() and activity.usename=current_user and not locks.granted)`).Scan(&waiting); err != nil {
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

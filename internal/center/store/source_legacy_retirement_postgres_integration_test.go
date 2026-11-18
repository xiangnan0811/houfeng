package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

const legacyRetirementAdmin = "usr_000000000000000000000001"

type legacyRetirementPGFixture struct {
	db         *pgxpool.Pool
	deployment string
	app        deletionledger.SourceApplicationIdentity
	baseline   string
}

func newLegacyRetirementPGFixture(t *testing.T, ctx context.Context) *legacyRetirementPGFixture {
	t.Helper()
	db := openRecordPlatformTemporaryPostgresDatabase(t, ctx)
	// The lock and visibility checks need distinct concurrent backends even
	// when the runner's connection string limits its ordinary pool to one.
	config := db.Config()
	if config.MaxConns < 3 {
		config.MaxConns = 3
		var err error
		db, err = pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatalf("open concurrent C80 fixture pool: %v", err)
		}
		t.Cleanup(db.Close)
	}
	if err := applyIPQualityMigrationsThrough(t, ctx, db, "0080_bound_agent_sync_replay.sql"); err != nil {
		t.Fatalf("install frozen C80 migrations: %v", err)
	}
	var c80, c81, successor bool
	if err := db.QueryRow(ctx, `select
		exists(select 1 from public.schema_migrations where name='0080_bound_agent_sync_replay.sql'),
		exists(select 1 from public.schema_migrations where name='0081_record_deletion_successor.sql'),
		pg_catalog.to_regclass('public.source_authority_admission') is not null`).Scan(&c80, &c81, &successor); err != nil || !c80 || c81 || successor {
		t.Fatalf("fixture is not C80: c80=%t c81=%t successor=%t error=%v", c80, c81, successor, err)
	}

	// Install the real C80 deployment projection through its existing CAS
	// command. This fixture does not claim external maintenance admission.
	state, err := recordauthority.CreateComposeState(filepath.Join(t.TempDir(), "authority"))
	if err != nil {
		t.Fatal(err)
	}
	activation, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var receipt []byte
	if err := db.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, activation).Scan(&receipt); err != nil || len(receipt) != 32 {
		t.Fatalf("activate C80 deployment projection: receipt=%x error=%v", receipt, err)
	}
	physical, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, db, platformmigrate.DomainKindApplication)
	if err != nil {
		t.Fatalf("provision physical APP identity: %v", err)
	}
	f := &legacyRetirementPGFixture{
		db: db, deployment: string(state.DeploymentID),
		app: deletionledger.SourceApplicationIdentity{
			ID: physical.ID, SystemIdentifier: physical.PostgresSystemIdentifier,
			DatabaseOID: physical.DatabaseOID, DatabaseName: physical.DatabaseName, Epoch: uint64(physical.IdentityEpoch),
		},
	}
	for _, statement := range []string{
		`insert into public.users(user_id,username,password_hash,display_name,role,disabled_at) values
			('usr_000000000000000000000001','legacy-admin','hash','Admin','admin',null),
			('usr_000000000000000000000002','legacy-viewer','hash','Viewer','viewer',null),
			('usr_000000000000000000000003','legacy-disabled','hash','Disabled','admin',now())`,
		`insert into public.record_access_groups(group_id,display_name) values
			('rag_beta','Beta'),('rag_alpha','Alpha'),('rag_other','Other')`,
		`insert into public.record_access_group_members(group_id,user_id) values
			('rag_beta','usr_000000000000000000000001'),('rag_alpha','usr_000000000000000000000001'),
			('rag_other','usr_000000000000000000000002')`,
		`insert into public.vps_assets(vps_id,display_name,lifecycle_status) values
			('vps_0000000000000001','Original VPS','active'),('vps_0000000000000002','Neighbor VPS','active')`,
		`insert into public.monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status) values
			('mi_0000000000000001','vps_0000000000000001','Original MI','','','','待接入','暂停','未绑定'),
			('mi_0000000000000002','vps_0000000000000002','Neighbor MI','','','','待接入','暂停','未绑定')`,
		`insert into public.targets(target_id,name,target_type,host,run_status) values
			('tg_0000000000000001','Original target','hostname','first.invalid','启用'),
			('tg_0000000000000002','Neighbor target','hostname','neighbor.invalid','启用')`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("seed C80 APP fixture: %v", err)
		}
	}
	f.baseline = f.sourceSnapshot(t, ctx)
	return f
}

func (f *legacyRetirementPGFixture) sourceSnapshot(t *testing.T, ctx context.Context) string {
	t.Helper()
	var snapshot string
	if err := f.db.QueryRow(ctx, `select jsonb_build_array(
		(select jsonb_agg(to_jsonb(v) order by v.vps_id) from public.vps_assets v),
		(select jsonb_agg(to_jsonb(m) order by m.monitoring_instance_id) from public.monitoring_instances m),
		(select jsonb_agg(to_jsonb(t) order by t.target_id) from public.targets t))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (f *legacyRetirementPGFixture) assertUnchanged(t *testing.T, ctx context.Context, tombstones int) {
	t.Helper()
	if actual := f.sourceSnapshot(t, ctx); actual != f.baseline {
		t.Fatal("retirement changed original or neighboring source rows")
	}
	var count int
	if err := f.db.QueryRow(ctx, `select count(*) from public.source_deletion_tombstones`).Scan(&count); err != nil || count != tombstones {
		t.Fatalf("legacy tombstone count=%d want=%d error=%v", count, tombstones, err)
	}
}

func (f *legacyRetirementPGFixture) key(kind recordauth.SourceKind, id string) sourceretirement.SourceKey {
	return sourceretirement.SourceKey{DeploymentID: f.deployment, ProjectID: recordauth.ProjectIDDefault, Kind: kind, ID: id}
}

func (f *legacyRetirementPGFixture) observe(t *testing.T, ctx context.Context, key sourceretirement.SourceKey) LegacySourceObservation {
	t.Helper()
	tx, err := f.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	observation, err := ObserveLegacySourceRetirement(ctx, tx, legacyRetirementAdmin, key)
	if err != nil {
		t.Fatalf("observe %s: %v", key.Kind, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return observation
}

func (f *legacyRetirementPGFixture) request(t *testing.T, key sourceretirement.SourceKey, observation LegacySourceObservation) deletionledger.SourceRetirementRequest {
	t.Helper()
	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 1)
	token, err := deletionledger.SourceTokenCommitment(key, [32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	request := deletionledger.SourceRetirementRequest{
		OperationID: "legacy_retirement_" + string(key.Kind), ActorUserID: legacyRetirementAdmin,
		Origin: deletionledger.SourceOrigin{Application: f.app, Source: key, Revision: observation.Revision,
			LastLive: observation.LastLive, TombstoneRecordedAt: observation.RecordedAt},
		FinalFloor: floor, TokenCommitment: token,
	}
	return legacyRetirementFingerprint(t, request)
}

func legacyRetirementFingerprint(t *testing.T, request deletionledger.SourceRetirementRequest) deletionledger.SourceRetirementRequest {
	t.Helper()
	fingerprint, err := deletionledger.SourceRequestFingerprint(request)
	if err != nil {
		t.Fatalf("fingerprint fixture request: %v", err)
	}
	request.RequestFingerprint = fingerprint
	if err := request.Validate(); err != nil {
		t.Fatalf("validate fixture request: %v", err)
	}
	return request
}

func TestPostgresIntegrationLegacySourceRetirement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := newLegacyRetirementPGFixture(t, ctx)
	keys := []sourceretirement.SourceKey{
		f.key(recordauth.SourceKindVPS, "vps_0000000000000001"),
		f.key(recordauth.SourceKindMonitoringInstance, "mi_0000000000000001"),
		f.key(recordauth.SourceKindTarget, "tg_0000000000000001"),
	}
	neighbors := []sourceretirement.SourceKey{
		f.key(recordauth.SourceKindVPS, "vps_0000000000000002"),
		f.key(recordauth.SourceKindMonitoringInstance, "mi_0000000000000002"),
		f.key(recordauth.SourceKindTarget, "tg_0000000000000002"),
	}
	observations := make([]LegacySourceObservation, len(keys))
	requests := make([]deletionledger.SourceRetirementRequest, len(keys))
	for index, key := range keys {
		observations[index] = f.observe(t, ctx, key)
		requests[index] = f.request(t, key, observations[index])
		if observations[index].Revision != legacyRetirementSQLRevision(t, ctx, f.db, key) {
			t.Fatalf("%s revision differs from the actual source-retirement SQL inputs", key.Kind)
		}
		if !reflect.DeepEqual(observations[index].Actor.GroupIDs, []string{"rag_alpha", "rag_beta"}) ||
			observations[index].Actor.UserID != legacyRetirementAdmin || observations[index].Actor.Role != recordauth.RoleProjectAdmin {
			t.Fatalf("actor is not the persisted enabled admin with sorted groups: %+v", observations[index].Actor)
		}
		when := observations[index].RecordedAt
		if when.Location() != time.UTC || when.Nanosecond()%1000 != 0 || !time.UnixMicro(when.UnixMicro()).UTC().Equal(when) {
			t.Fatalf("observation time is not PostgreSQL microsecond UTC: %v", when)
		}
		legacyRetirementAssertLiveAdapter(t, ctx, f.db, key, observations[index])
	}
	f.assertUnchanged(t, ctx, 0)

	t.Run("isolation_and_held_locks", func(t *testing.T) {
		legacyRetirementAssertLocks(t, ctx, f.db, keys[0], neighbors[0])
		for _, isolation := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
			tx, err := f.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ObserveLegacySourceRetirement(ctx, tx, legacyRetirementAdmin, keys[0])
			_ = tx.Rollback(ctx)
			if err == nil {
				t.Fatalf("accepted stale-snapshot isolation %q", isolation)
			}
		}
		f.assertUnchanged(t, ctx, 0)
	})

	t.Run("invalid_actor_and_source", func(t *testing.T) {
		wrongDeployment := keys[0]
		wrongDeployment.DeploymentID = "dp-" + strings.Repeat("b", 64)
		if wrongDeployment.DeploymentID == f.deployment {
			wrongDeployment.DeploymentID = "dp-" + strings.Repeat("c", 64)
		}
		wrongProject := keys[0]
		wrongProject.ProjectID = "other"
		missing := keys[0]
		missing.ID = "vps_ffffffffffffffff"
		invalidKind := keys[0]
		invalidKind.Kind = "other"
		for _, tc := range []struct {
			name  string
			actor string
			key   sourceretirement.SourceKey
		}{
			{"malformed_actor", "admin", keys[0]},
			{"missing_actor", "usr_ffffffffffffffffffffffff", keys[0]},
			{"viewer", "usr_000000000000000000000002", keys[0]},
			{"disabled_admin", "usr_000000000000000000000003", keys[0]},
			{"wrong_deployment", legacyRetirementAdmin, wrongDeployment},
			{"wrong_project", legacyRetirementAdmin, wrongProject},
			{"missing_source", legacyRetirementAdmin, missing},
			{"wrong_kind", legacyRetirementAdmin, invalidKind},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tx, err := f.db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_, err = ObserveLegacySourceRetirement(ctx, tx, tc.actor, tc.key)
				_ = tx.Rollback(ctx)
				if err == nil {
					t.Fatal("invalid observation was accepted")
				}
				f.assertUnchanged(t, ctx, 0)
			})
		}
	})

	for index, request := range requests {
		index, request := index, request
		t.Run(string(keys[index].Kind)+"_stale_cas", func(t *testing.T) {
			tx, err := f.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var statement string
			switch keys[index].Kind {
			case recordauth.SourceKindVPS:
				statement = `update public.vps_assets set updated_at=updated_at+interval '1 microsecond' where vps_id=$1`
			case recordauth.SourceKindMonitoringInstance:
				statement = `update public.monitoring_instances set control_revision=control_revision+1 where monitoring_instance_id=$1`
			case recordauth.SourceKindTarget:
				statement = `update public.targets set control_revision=control_revision+1 where target_id=$1`
			}
			if _, err := tx.Exec(ctx, statement, keys[index].ID); err != nil {
				t.Fatal(err)
			}
			if err := InsertLegacySourceRetirement(ctx, tx, request); err == nil {
				t.Fatal("stale source CAS accepted")
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			f.assertUnchanged(t, ctx, 0)
		})
	}

	t.Run("request_revalidation", func(t *testing.T) {
		valid := requests[0]
		wrongDeployment := valid
		wrongDeployment.Origin.Source.DeploymentID = "dp-" + strings.Repeat("b", 64)
		if wrongDeployment.Origin.Source.DeploymentID == f.deployment {
			wrongDeployment.Origin.Source.DeploymentID = "dp-" + strings.Repeat("c", 64)
		}
		wrongDeployment = legacyRetirementFingerprint(t, wrongDeployment)
		otherSource := valid
		otherSource.Origin.Source = neighbors[0]
		otherSource = legacyRetirementFingerprint(t, otherSource)
		wrongLive := valid
		wrongLive.Origin.LastLive = mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 1)
		wrongLive = legacyRetirementFingerprint(t, wrongLive)
		viewer := valid
		viewer.ActorUserID = "usr_000000000000000000000002"
		viewer = legacyRetirementFingerprint(t, viewer)
		disabled := valid
		disabled.ActorUserID = "usr_000000000000000000000003"
		disabled = legacyRetirementFingerprint(t, disabled)
		badFingerprint := valid
		badFingerprint.RequestFingerprint[0] ^= 1
		widened := wrongLive
		widened.FinalFloor = observations[0].LastLive
		if _, err := deletionledger.SourceRequestFingerprint(widened); err == nil {
			t.Fatal("floor widened beyond the original last-live scope")
		}
		for _, tc := range []struct {
			name    string
			request deletionledger.SourceRetirementRequest
		}{
			{"wrong_deployment", wrongDeployment}, {"neighbor_identity", otherSource}, {"changed_last_live", wrongLive},
			{"viewer", viewer}, {"disabled", disabled}, {"changed_fingerprint", badFingerprint}, {"widened_floor", widened},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tx, err := f.db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				err = InsertLegacySourceRetirement(ctx, tx, tc.request)
				_ = tx.Rollback(ctx)
				if err == nil {
					t.Fatal("invalid original request accepted")
				}
				f.assertUnchanged(t, ctx, 0)
			})
		}
		tx, err := f.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `update public.users set role='viewer' where user_id=$1`, legacyRetirementAdmin); err != nil {
			t.Fatal(err)
		}
		if err := InsertLegacySourceRetirement(ctx, tx, valid); err == nil {
			t.Fatal("actor revoked after preview was accepted")
		}
		_ = tx.Rollback(ctx)
		f.assertUnchanged(t, ctx, 0)
	})

	t.Run("existing_conflicting_tombstone", func(t *testing.T) {
		tx, err := f.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `insert into public.source_deletion_tombstones(project_id,source_kind,source_id,authorization_floor_digest,deleted_at)
			values ('default','vps',$1,decode(repeat('44',32),'hex'),$2)`, keys[0].ID, observations[0].RecordedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := ObserveLegacySourceRetirement(ctx, tx, legacyRetirementAdmin, keys[0]); err == nil {
			t.Fatal("preview accepted a source with an existing tombstone")
		}
		if err := InsertLegacySourceRetirement(ctx, tx, requests[0]); err == nil {
			t.Fatal("existing tombstone was adopted by a new request")
		}
		if err := ReadLegacySourceRetirementProjection(ctx, tx, requests[0]); err == nil {
			t.Fatal("conflicting tombstone digest matched the original request")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		f.assertUnchanged(t, ctx, 0)
	})

	for index, request := range requests {
		index, request := index, request
		t.Run(string(keys[index].Kind)+"_original_projection", func(t *testing.T) {
			if err := ReadLegacySourceRetirementProjection(ctx, f.db, request); err == nil {
				t.Fatal("missing tombstone was accepted")
			}
			tx, err := f.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func(original pgx.Tx) { _ = original.Rollback(ctx) }(tx)
			if err := InsertLegacySourceRetirement(ctx, tx, request); err != nil {
				t.Fatalf("insert original tombstone: %v", err)
			}
			if err := ReadLegacySourceRetirementProjection(ctx, tx, request); err != nil {
				t.Fatalf("read transaction-local original projection: %v", err)
			}
			if err := ReadLegacySourceRetirementProjection(ctx, f.db, request); err == nil {
				t.Fatal("insertion escaped the caller-owned transaction")
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := ReadLegacySourceRetirementProjection(ctx, f.db, request); err == nil {
				t.Fatal("rolled back tombstone was treated as committed")
			}
			tx, err = f.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := InsertLegacySourceRetirement(ctx, tx, request); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := ReadLegacySourceRetirementProjection(ctx, f.db, request); err != nil {
				t.Fatalf("committed exact projection: %v", err)
			}
			replay, err := f.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ObserveLegacySourceRetirement(ctx, replay, legacyRetirementAdmin, keys[index]); err == nil {
				t.Fatal("existing exact tombstone was accepted as a fresh preview")
			}
			if err := InsertLegacySourceRetirement(ctx, replay, request); err == nil {
				t.Fatal("existing exact tombstone was accepted as a fresh insertion")
			}
			if err := replay.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var digest []byte
			var deletedAt time.Time
			if err := f.db.QueryRow(ctx, `select authorization_floor_digest,deleted_at from public.source_deletion_tombstones
				where project_id=$1 and source_kind=$2 and source_id=$3`, string(keys[index].ProjectID), string(keys[index].Kind), keys[index].ID).Scan(&digest, &deletedAt); err != nil {
				t.Fatal(err)
			}
			wantDigest := request.FinalFloor.CanonicalHashValue()
			if !bytes.Equal(digest, wantDigest[:]) || !deletedAt.Equal(observations[index].RecordedAt) {
				t.Fatal("projection did not preserve the original floor digest and preview time")
			}
			changedTime := request
			changedTime.Origin.TombstoneRecordedAt = changedTime.Origin.TombstoneRecordedAt.Add(time.Microsecond)
			changedTime = legacyRetirementFingerprint(t, changedTime)
			changedFloor := request
			changedFloor.FinalFloor = mustStoreRestrictedVisibility(t, nil, nil, 1)
			changedFloor = legacyRetirementFingerprint(t, changedFloor)
			changedDeployment := request
			changedDeployment.Origin.Source.DeploymentID = "dp-" + strings.Repeat("b", 64)
			if changedDeployment.Origin.Source.DeploymentID == f.deployment {
				changedDeployment.Origin.Source.DeploymentID = "dp-" + strings.Repeat("c", 64)
			}
			changedDeployment = legacyRetirementFingerprint(t, changedDeployment)
			otherSource := request
			otherSource.Origin.Source = neighbors[index]
			otherSource = legacyRetirementFingerprint(t, otherSource)
			for name, changed := range map[string]deletionledger.SourceRetirementRequest{
				"time": changedTime, "floor": changedFloor, "deployment": changedDeployment, "neighbor": otherSource,
			} {
				if err := ReadLegacySourceRetirementProjection(ctx, f.db, changed); err == nil {
					t.Fatalf("projection accepted changed %s", name)
				}
			}
			// Retirement leaves the source and a distinct neighbor physically
			// present; the projection is not a physical deletion receipt.
			neighbor := f.observe(t, ctx, neighbors[index])
			if neighbor.Revision == ([32]byte{}) {
				t.Fatal("neighbor was not independently observable")
			}
			f.assertUnchanged(t, ctx, index+1)
		})
	}
}

// Compute the established SQL revision independently of the Go primitive.
func legacyRetirementSQLRevision(t *testing.T, ctx context.Context, db *pgxpool.Pool, key sourceretirement.SourceKey) [32]byte {
	t.Helper()
	var body string
	switch key.Kind {
	case recordauth.SourceKindVPS:
		body = `select jsonb_build_array(vps_id,extract(epoch from updated_at),lifecycle_status)::text from public.vps_assets where vps_id=$1`
	case recordauth.SourceKindMonitoringInstance:
		body = `select jsonb_build_array(monitoring_instance_id,extract(epoch from metadata_updated_at),control_revision,lifecycle_status)::text from public.monitoring_instances where monitoring_instance_id=$1`
	case recordauth.SourceKindTarget:
		body = `select jsonb_build_array(target_id,extract(epoch from updated_at),control_revision,run_status)::text from public.targets where target_id=$1`
	default:
		t.Fatal("unsupported source")
	}
	var sourceBody string
	if err := db.QueryRow(ctx, body, key.ID).Scan(&sourceBody); err != nil {
		t.Fatal(err)
	}
	var digest []byte
	if err := db.QueryRow(ctx, `select record_platform_internal.digest(
		convert_to('HOUFENG-SOURCE-REVISION-V2','UTF8') || int4send(octet_length(convert_to($1::text,'UTF8'))) ||
		convert_to($1::text,'UTF8') || convert_to($2::text,'UTF8'), 'sha256')`, string(key.Kind), sourceBody).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	var revision [32]byte
	if len(digest) != len(revision) {
		t.Fatalf("SQL revision length=%d", len(digest))
	}
	copy(revision[:], digest)
	return revision
}

func legacyRetirementAssertLiveAdapter(t *testing.T, ctx context.Context, db *pgxpool.Pool, key sourceretirement.SourceKey, observation LegacySourceObservation) {
	t.Helper()
	var adapter records.SubjectSourceAdapter
	var kind records.SubjectKind
	switch key.Kind {
	case recordauth.SourceKindVPS:
		adapter, kind = NewVPSRecordSubjectAdapter(NewPostgresVPSAssetRepository(db)), records.SubjectKindVPS
	case recordauth.SourceKindMonitoringInstance:
		adapter, kind = NewMonitoringInstanceRecordSubjectAdapter(NewPostgresMonitoringInstanceRepository(db)), records.SubjectKindMonitoringInstance
	case recordauth.SourceKindTarget:
		adapter, kind = NewTargetRecordSubjectAdapter(NewPostgresTargetRepository(db)), records.SubjectKindTarget
	default:
		t.Fatal("unsupported source adapter")
	}
	resolved, err := adapter.Resolve(ctx, observation.Actor, storeRecordReference(kind, key.ID))
	if err != nil || resolved.CaptureAuthorization.CurrentScope == nil {
		t.Fatalf("resolve actual live source adapter: %v", err)
	}
	if !bytes.Equal(resolved.CaptureAuthorization.CurrentScope.CanonicalBytes(), observation.LastLive.CanonicalBytes()) {
		t.Fatal("observed last-live scope differs from the actual live adapter")
	}
}

func legacyRetirementAssertLocks(t *testing.T, ctx context.Context, db *pgxpool.Pool, key, neighbor sourceretirement.SourceKey) {
	t.Helper()
	holder, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := ObserveLegacySourceRetirement(ctx, holder, legacyRetirementAdmin, key); err != nil {
		t.Fatal(err)
	}
	contender, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func(original pgx.Tx) { _ = original.Rollback(ctx) }(contender)
	var acquired bool
	if err := contender.QueryRow(ctx, `select pg_try_advisory_xact_lock(1213154899,1)`).Scan(&acquired); err != nil || acquired {
		t.Fatalf("graph lock is not held: acquired=%t error=%v", acquired, err)
	}
	if err := contender.QueryRow(ctx, `select vps_id from public.vps_assets where vps_id=$1 for update nowait`, neighbor.ID).Scan(new(string)); err != nil {
		t.Fatalf("neighbor source row should remain independently lockable: %v", err)
	}
	_, err = contender.Exec(ctx, `select vps_id from public.vps_assets where vps_id=$1 for update nowait`, key.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("source row lock was not held: %v", err)
	}
	_ = contender.Rollback(ctx)
	for _, table := range []string{"users", "record_access_groups", "record_access_group_members"} {
		contender, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func(original pgx.Tx) { _ = original.Rollback(ctx) }(contender)
		_, err = contender.Exec(ctx, `lock table public.`+pgx.Identifier{table}.Sanitize()+` in row exclusive mode nowait`)
		pgErr = nil
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("%s actor/group share lock was not held: %v", table, err)
		}
		_ = contender.Rollback(ctx)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	contender, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = contender.Rollback(ctx) }()
	if err := contender.QueryRow(ctx, `select pg_try_advisory_xact_lock(1213154899,1)`).Scan(&acquired); err != nil || !acquired {
		t.Fatalf("graph lock survived transaction end: acquired=%t error=%v", acquired, err)
	}
}

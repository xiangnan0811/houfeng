package platformmigrate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordbirth"
)

type configuredBirthIdentity struct {
	ID               string
	SystemIdentifier string
	DatabaseOID      uint32
	DatabaseName     string
	Epoch            uint64
}

func TestPostgresIntegrationConfiguredBirthProducerPhysicalIdentity(t *testing.T) {
	f := newBirthAuthorityFixture(t)
	ctx := context.Background()
	var identity configuredBirthIdentity
	var epoch int64
	if err := f.db.QueryRow(ctx, `select d.domain_id, c.system_identifier::text, db.oid, db.datname, d.identity_epoch
		from public.record_platform_domain_identity d cross join pg_catalog.pg_control_system() c
		join pg_catalog.pg_database db on db.datname=current_database()`).Scan(
		&identity.ID, &identity.SystemIdentifier, &identity.DatabaseOID, &identity.DatabaseName, &epoch); err != nil {
		t.Fatal(err)
	}
	if epoch <= 0 {
		t.Fatal("invalid witness identity epoch")
	}
	identity.Epoch = uint64(epoch)
	appConfig, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	appConfig.ConnConfig.Database = "hf_app_" + strings.TrimPrefix(f.config.ConnConfig.Database, "hf_w2_") + "_birth"
	appOwner, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appOwner.Close)
	role := "hf_bapp_" + strings.TrimPrefix(f.config.ConnConfig.Database, "hf_w2_")
	quotedRole := pgx.Identifier{role}.Sanitize()
	var createRole string
	if err := appOwner.QueryRow(ctx, `select format('create role %I login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L',$1::text,$2::text)`, role, f.password).Scan(&createRole); err != nil {
		t.Fatal(err)
	}
	if _, err := appOwner.Exec(ctx, createRole); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := appOwner.Exec(context.Background(), "drop owned by "+quotedRole); err != nil {
			t.Errorf("drop APP fixture grants: %v", err)
		}
		if _, err := appOwner.Exec(context.Background(), "drop role "+quotedRole); err != nil {
			t.Errorf("drop APP fixture role: %v", err)
		}
	})
	if _, err := appOwner.Exec(ctx, "grant usage on schema public to "+quotedRole); err != nil {
		t.Fatal(err)
	}
	if _, err := appOwner.Exec(ctx, "grant execute on function public.source_maintenance_status() to "+quotedRole); err != nil {
		t.Fatal(err)
	}
	// This isolated fixture uses the real APP status function with a direct,
	// current-generation runtime principal; it does not perform enrollment.
	if _, err := appOwner.Exec(ctx, `insert into public.source_application_principals(principal,generation,purpose) values($1,1,'runtime')`, role); err != nil {
		t.Fatal(err)
	}
	appDirectConfig := appConfig.Copy()
	appDirectConfig.ConnConfig.User, appDirectConfig.ConnConfig.Password = role, f.password
	appDirect, err := pgxpool.NewWithConfig(ctx, appDirectConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appDirect.Close)
	setAdmission := func(value configuredBirthIdentity) {
		t.Helper()
		_, err := appOwner.Exec(ctx, `insert into public.source_authority_admission
			(singleton,deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode)
			values(true,$1,$2,$3,$4::oid,$5,$6,1,'active')
			on conflict(singleton) do update set witness_domain_id=excluded.witness_domain_id,
			witness_system_identifier=excluded.witness_system_identifier,witness_database_oid=excluded.witness_database_oid,
			witness_database_name=excluded.witness_database_name,witness_identity_epoch=excluded.witness_identity_epoch`,
			f.binding.DeploymentID, value.ID, value.SystemIdentifier, value.DatabaseOID, value.DatabaseName, value.Epoch)
		if err != nil {
			t.Fatal(err)
		}
	}
	makeAuthority := func(value configuredBirthIdentity) *recordbirth.PostgresAuthority {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "witness.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		authority, err := recordbirth.NewConfiguredPostgresAuthority(f.producer, appDirect, path)
		if err != nil {
			t.Fatal(err)
		}
		return authority
	}
	setAdmission(identity)
	assertSourceAuthorityDenied(t, appDirect, `select * from public.source_authority_admission`)
	var projected configuredBirthIdentity
	var projectedEpoch int64
	if err := appDirect.QueryRow(ctx, `select witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch
		from public.source_maintenance_status() where singleton`).Scan(&projected.ID, &projected.SystemIdentifier, &projected.DatabaseOID, &projected.DatabaseName, &projectedEpoch); err != nil || projectedEpoch <= 0 {
		t.Fatalf("read restricted APP witness identity: epoch=%d, error=%v", projectedEpoch, err)
	}
	projected.Epoch = uint64(projectedEpoch)
	if projected != identity {
		t.Fatalf("restricted APP witness identity = %+v, want %+v", projected, identity)
	}
	valid := makeAuthority(identity)
	if _, err := valid.Observe(ctx, f.binding, ""); err != nil {
		t.Fatalf("configured observation: %v", err)
	}
	request := recordbirth.Request{ProjectID: "default", ActorID: "usr_configured", ActorScopeDigest: sha256.Sum256([]byte("scope")),
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: "configured-one", RequestFingerprint: sha256.Sum256([]byte("request one"))}
	firstCheckpoint := f.appendCheckpoint(t, f.binding, request)
	first, err := valid.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, firstCheckpoint))
	if err != nil {
		t.Fatalf("configured reserve: %v", err)
	}
	firstEvent, err := recordbirth.VerifyReceipt(first, f.key)
	if err != nil {
		t.Fatal(err)
	}
	request.IdempotencyKey = "configured-two"
	request.RequestFingerprint = sha256.Sum256([]byte("request two"))
	secondCheckpoint := f.appendCheckpoint(t, f.binding, request)
	second, err := valid.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, secondCheckpoint))
	if err != nil {
		t.Fatalf("second configured reserve: %v", err)
	}
	secondEvent, err := recordbirth.VerifyReceipt(second, f.key)
	if err != nil {
		t.Fatal(err)
	}
	initial := sha256.Sum256([]byte("initial configured birth"))
	if _, err := valid.Prepare(ctx, f.binding, secondEvent.ReservationID, initial); err != nil {
		t.Fatalf("configured prepare: %v", err)
	}
	checkpointBefore := f.checkpointState(t)
	if checkpointBefore.coverage != 2 || checkpointBefore.anchor != f.appHead {
		t.Fatalf("configured checkpoint state: %+v", checkpointBefore)
	}
	assertUnchanged := func() {
		t.Helper()
		var requests, events, head int
		if err := f.db.QueryRow(ctx, `select (select count(*) from public.record_birth_requests),
			(select count(*) from public.record_birth_events),sequence from public.record_birth_head where singleton`).Scan(&requests, &events, &head); err != nil || requests != 2 || events != 3 || head != 3 {
			t.Fatalf("wrong identity changed W2: requests=%d events=%d head=%d error=%v", requests, events, head, err)
		}
		if after := f.checkpointState(t); after != checkpointBefore {
			t.Fatalf("wrong identity changed checkpoint state: before=%+v after=%+v", checkpointBefore, after)
		}
		f.assertCheckpointCoverage(t, firstCheckpoint)
		f.assertCheckpointCoverage(t, secondCheckpoint)
	}
	changes := map[string]func(*configuredBirthIdentity){
		"authority": func(v *configuredBirthIdentity) { v.ID = "rd-" + strings.Repeat("f", 64) },
		"system": func(v *configuredBirthIdentity) {
			if v.SystemIdentifier == "1" {
				v.SystemIdentifier = "2"
			} else {
				v.SystemIdentifier = "1"
			}
		},
		"database oid":  func(v *configuredBirthIdentity) { v.DatabaseOID++ },
		"database name": func(v *configuredBirthIdentity) { v.DatabaseName += "_other" },
		"epoch":         func(v *configuredBirthIdentity) { v.Epoch++ },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			wrong := identity
			change(&wrong)
			// APP agrees with the wrong file, so the admitted W2 observation must
			// independently reject the real physical mismatch before each effect.
			setAdmission(wrong)
			authority := makeAuthority(wrong)
			if _, err := authority.Observe(ctx, f.binding, ""); !errors.Is(err, recordbirth.ErrUnavailable) {
				t.Fatalf("observe = %v", err)
			}
			changed := request
			changed.IdempotencyKey = "rejected-" + strings.ReplaceAll(name, " ", "-")
			checkpoint := f.appendCheckpoint(t, f.binding, changed)
			if _, err := authority.Reserve(ctx, f.binding, changed, f.checkpointProof(t, f.binding, f.key, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
				t.Fatalf("reserve = %v", err)
			}
			if _, err := authority.Prepare(ctx, f.binding, firstEvent.ReservationID, initial); !errors.Is(err, recordbirth.ErrUnavailable) {
				t.Fatalf("prepare = %v", err)
			}
			if _, err := authority.Confirm(ctx, f.binding, secondEvent.ReservationID, initial); !errors.Is(err, recordbirth.ErrUnavailable) {
				t.Fatalf("confirm = %v", err)
			}
			assertUnchanged()
		})
	}
	// Every physical field is read from the restricted APP projection on each
	// call. Drift must stop the configured producer before any W2 effect.
	for name, change := range changes {
		t.Run("APP "+name, func(t *testing.T) {
			wrong := identity
			change(&wrong)
			setAdmission(wrong)
			if _, err := valid.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, secondCheckpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
				t.Fatalf("APP identity drift = %v", err)
			}
			assertUnchanged()
		})
	}
	if _, err := appOwner.Exec(ctx, `update public.source_authority_admission set generation=2 where singleton`); err != nil {
		t.Fatal(err)
	}
	assertSourceAuthorityDenied(t, appDirect, `select * from public.source_maintenance_status()`)
	if _, err := valid.Observe(ctx, f.binding, ""); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("stale APP principal admitted configured observation: %v", err)
	}
	assertUnchanged()
	if _, err := appOwner.Exec(ctx, `update public.source_authority_admission set generation=1 where singleton`); err != nil {
		t.Fatal(err)
	}
	setAdmission(identity)
	if _, err := valid.Prepare(ctx, f.binding, firstEvent.ReservationID, initial); err != nil {
		t.Fatalf("valid prepare: %v", err)
	}
	created, err := valid.Confirm(ctx, f.binding, secondEvent.ReservationID, initial)
	if err != nil {
		t.Fatalf("valid confirm: %v", err)
	}
	observation, err := valid.Observe(ctx, f.binding, secondEvent.RecordID)
	if err != nil || len(created) == 0 || string(observation.Created) != string(created) {
		t.Fatalf("valid configured observation: %+v %v", observation, err)
	}
	assertSourceAuthorityDenied(t, f.producer, `select * from public.record_birth_keys`)
}

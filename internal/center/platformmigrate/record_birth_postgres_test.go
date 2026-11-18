package platformmigrate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordbirth"
)

func TestSourceAuthorityBirthRolesMustBeSeparate(t *testing.T) {
	roles := SourceAuthorityRoles{Owner: "owner", Reader: "reader", Producer: "producer", Provisioner: "provisioner"}
	if err := roles.Validate(); err != nil {
		t.Fatal(err)
	}
	roles.BirthProducer = "birth_producer"
	if err := roles.Validate(); err == nil {
		t.Fatal("accepted unpaired birth producer")
	}
	roles.BirthReader = "birth_reader"
	if err := roles.Validate(); err != nil {
		t.Fatal(err)
	}
	roles.BirthReader = roles.Reader
	if err := roles.Validate(); err == nil {
		t.Fatal("accepted reused source reader")
	}
}

func TestPostgresIntegrationRecordBirthSuccessorConvergence(t *testing.T) {
	ctx := context.Background()
	t.Run("fresh_and_successor_repeat", func(t *testing.T) {
		f := newSourceAuthorityFixtureWithPredecessor(t, false)
		first, err := ProvisionSourceAuthority(ctx, f.db, f.roles)
		if err != nil {
			t.Fatalf("fresh convergence: %v", err)
		}
		again, err := ProvisionSourceAuthority(ctx, f.db, f.roles)
		if err != nil || first != again {
			t.Fatalf("successor replay: %v", err)
		}
		assertBirthMigrationState(t, f, 3, true)
		var disabled bool
		if err := f.db.QueryRow(ctx, `select not exists(select 1 from public.record_birth_enabled)
			and not exists(select 1 from public.record_birth_keys)
			and not exists(select 1 from public.record_birth_lineage)`).Scan(&disabled); err != nil || !disabled {
			t.Fatalf("fresh successor was not disabled: %v", err)
		}
	})
	t.Run("enrolled_predecessor_and_successor_repeat", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		before := provisionLegacyWitness(t, f)
		after, err := ProvisionSourceAuthority(ctx, f.db, f.roles)
		if err != nil || before != after {
			t.Fatalf("predecessor convergence: %v", err)
		}
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err != nil {
			t.Fatalf("successor repeat: %v", err)
		}
		assertBirthMigrationState(t, f, 3, true)
	})
	t.Run("mixed_catalog_is_not_repaired", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.exec(t, `create table public.record_birth_unexpected(id integer)`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("mixed catalog accepted")
		}
		assertBirthMigrationState(t, f, 2, false)
	})
	t.Run("predecessor_checksum_drift_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.exec(t, `update public.schema_migrations set checksum=repeat('0',64) where name='0002_source_retirement_authority.sql'`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("predecessor checksum drift accepted")
		}
		assertBirthMigrationState(t, f, 2, false)
	})
	t.Run("predecessor_trigger_drift_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		provisionLegacyWitness(t, f)
		f.exec(t, `alter table public.source_retirement_prepares disable trigger source_retirement_prepares_immutable`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("disabled predecessor trigger accepted")
		}
		assertBirthMigrationState(t, f, 2, false)
	})
	t.Run("successor_function_drift_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		f.exec(t, `create or replace function public.record_birth_reserve(p_command bytea) returns bytea
			language sql security definer set search_path=pg_catalog as $$ select null::bytea $$`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("altered successor function accepted")
		}
		assertBirthMigrationState(t, f, 3, true)
	})
	t.Run("checkpoint_coverage_trigger_drift_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		f.exec(t, `alter table public.record_birth_request_coverage disable trigger record_birth_request_coverage_immutable`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("disabled checkpoint coverage trigger accepted")
		}
		assertBirthMigrationState(t, f, 3, true)
	})
	t.Run("checkpoint_anchor_public_grant_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		f.exec(t, `grant select on public.record_birth_checkpoint_anchor to public`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("public checkpoint anchor grant accepted")
		}
		assertBirthMigrationState(t, f, 3, true)
	})
	t.Run("migration_and_ownership_roll_back_together", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		provisionLegacyWitness(t, f)
		f.exec(t, `create function public.fixture_reject_transfer() returns event_trigger language plpgsql as $$
			begin raise exception 'fixture abort during owner transfer'; end $$`)
		f.exec(t, `create event trigger fixture_reject_transfer on ddl_command_end
			when tag in ('ALTER FUNCTION') execute function public.fixture_reject_transfer()`)
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err == nil {
			t.Fatal("injected transfer failure accepted")
		}
		assertBirthMigrationState(t, f, 2, false)
		var owner string
		if err := f.db.QueryRow(ctx, `select pg_get_userbyid(proowner) from pg_proc
			where oid='public.source_authority_snapshot()'::regprocedure`).Scan(&owner); err != nil || owner != f.roles.Owner {
			t.Fatalf("predecessor ownership changed after rollback: %q %v", owner, err)
		}
	})
}

func assertBirthMigrationState(t *testing.T, f *sourceAuthorityFixture, expected int, exists bool) {
	t.Helper()
	var count int
	var installed bool
	if err := f.db.QueryRow(context.Background(), `select (select count(*) from public.schema_migrations),
		to_regclass('public.record_birth_keys') is not null`).Scan(&count, &installed); err != nil || count != expected || installed != exists {
		t.Fatalf("ledger=%d installed=%v error=%v", count, installed, err)
	}
}

// Build the exact previously shipped 0002 enrollment and grants to exercise a
// genuine enrolled predecessor rather than an empty successor substitute.
func provisionLegacyWitness(t *testing.T, f *sourceAuthorityFixture) DomainIdentity {
	t.Helper()
	ctx := context.Background()
	identity, err := ProvisionPostgresDomainIdentity(ctx, f.db, DomainKindDeletionWitness)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into public.source_authority_state(singleton,generation,mode) values(true,1,'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into public.source_authority_principals(principal,purpose,generation)
		values ($1,'reader',1),($2,'producer',1),($3,'provisioner',1)`,
		f.roles.Reader, f.roles.Producer, f.roles.Provisioner); err != nil {
		t.Fatal(err)
	}
	objects, err := sourceAuthorityObjects(ctx, tx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.managed && object.kind != "sequence" {
			if _, err := tx.Exec(ctx, "alter "+object.kind+" "+object.identity+" owner to "+pgx.Identifier{f.roles.Owner}.Sanitize()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, grant := range sourceAuthorityGrants(f.roles, objects) {
		object := sourceAuthorityFindObject(objects, grant.kind, grant.oid)
		if _, err := tx.Exec(ctx, "grant "+grant.privilege+" on "+grant.kind+" "+object.identity+" to "+pgx.Identifier{grant.role}.Sanitize()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return identity
}

type birthAuthorityFixture struct {
	*sourceAuthorityFixture
	producer *pgxpool.Pool
	reader   *pgxpool.Pool
	binding  recordbirth.Binding
	key      []byte
	appHead  recordbirth.CheckpointHead
}

func newBirthAuthorityFixture(t *testing.T) *birthAuthorityFixture {
	t.Helper()
	ctx := context.Background()
	f := newSourceAuthorityFixture(t)
	f.roles.BirthProducer = f.roles.Producer + "_birth"
	f.roles.BirthReader = f.roles.Reader + "_birth"
	f.createRole(t, f.roles.BirthProducer, true)
	f.createRole(t, f.roles.BirthReader, true)
	identity, err := ProvisionSourceAuthority(ctx, f.db, f.roles)
	if err != nil {
		t.Fatalf("provision disabled successor: %v", err)
	}
	producer, reader := f.login(t, f.roles.BirthProducer), f.login(t, f.roles.BirthReader)
	provisioner := f.login(t, f.roles.Provisioner)
	app := f.applicationIdentity(t, "birth")
	binding := recordbirth.Binding{AuthorityID: identity.ID, LineageID: "bl-" + strings.Repeat("b", 64),
		DeploymentID: "dp-" + strings.Repeat("c", 64), ProjectID: "default", Generation: 1,
		KeyID: "bk-" + strings.Repeat("d", 64), CutoverDigest: sha256.Sum256([]byte("birth fixture cutover"))}
	disabledReader, err := recordbirth.NewPostgresAuthority(reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabledReader.Observe(ctx, binding, ""); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("disabled authority admitted observation: %v", err)
	}
	if _, err := provisioner.Exec(ctx, `select public.source_application_enroll($1,$2,$3,$4::oid,$5,$6)`,
		binding.DeploymentID, app.ID, app.PostgresSystemIdentifier, app.DatabaseOID, app.DatabaseName, app.IdentityEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.Exec(ctx, `select public.source_enrollment_credentials($1,$2,$3,$4)`,
		[]string{"runtime", "admin", "worker", "migrator", "producer"},
		[]string{"app_runtime", "app_admin", "app_worker", "app_migrator", f.roles.Producer},
		[][]byte{bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{5}, 32)},
		[][]byte{bytes.Repeat([]byte{11}, 32), bytes.Repeat([]byte{12}, 32), bytes.Repeat([]byte{13}, 32), bytes.Repeat([]byte{14}, 32), bytes.Repeat([]byte{15}, 32)}); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.Exec(ctx, `select public.source_application_activate($1)`, bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	commitment := sha256.Sum256(key)
	// These fixture inserts stand in for the separate trusted offline operator.
	// The runtime principals have no privilege to perform any of them.
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_keys(key_id,key_bytes,key_commitment) values($1,$2,$3)`, binding.KeyID, key, commitment[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_lineage(singleton,authority_id,lineage_id) values(true,$1,$2)`, binding.AuthorityID, binding.LineageID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_bindings(generation,authority_id,lineage_id,deployment_id,project_id,key_id,cutover_digest)
		values($1,$2,$3,$4,$5,$6,$7)`, binding.Generation, binding.AuthorityID, binding.LineageID, binding.DeploymentID, binding.ProjectID, binding.KeyID, binding.CutoverDigest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_principals(principal,purpose,generation) values($1,'producer',1),($2,'reader',1)`, f.roles.BirthProducer, f.roles.BirthReader); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,$1,$2,$3)`, binding.Generation, binding.KeyID, binding.CutoverDigest[:]); err != nil {
		t.Fatal(err)
	}
	return &birthAuthorityFixture{sourceAuthorityFixture: f, producer: producer, reader: reader, binding: binding, key: key}
}

func TestPostgresIntegrationRecordBirthAuthority(t *testing.T) {
	f := newBirthAuthorityFixture(t)
	ctx := context.Background()
	producer, err := recordbirth.NewPostgresAuthority(f.producer)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := recordbirth.NewPostgresAuthority(f.reader)
	if err != nil {
		t.Fatal(err)
	}
	request := recordbirth.Request{ProjectID: "default", ActorID: "usr_fixture", ActorScopeDigest: sha256.Sum256([]byte("scope")),
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: "create-1", RequestFingerprint: sha256.Sum256([]byte("request"))}
	availability, err := reader.Observe(ctx, f.binding, "")
	if err != nil || availability.HeadSequence != 0 || len(availability.Reserved) != 0 {
		t.Fatalf("availability: %+v %v", availability, err)
	}
	producerAvailability, err := producer.Observe(ctx, f.binding, "")
	if err != nil || producerAvailability.Binding != f.binding || producerAvailability.HeadSequence != 0 || len(producerAvailability.Reserved) != 0 {
		t.Fatalf("producer availability: %+v %v", producerAvailability, err)
	}
	continuity, err := reader.Continuity(ctx, f.binding)
	if err != nil || continuity.Binding != f.binding || !continuity.Enabled || continuity.Anchor != (recordbirth.CheckpointHead{}) {
		t.Fatalf("registered continuity reader = %+v, %v", continuity, err)
	}
	if _, err := producer.Continuity(ctx, f.binding); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("producer unexpectedly read continuity: %v", err)
	}
	for _, pool := range []*pgxpool.Pool{f.producer, f.reader, f.login(t, f.roles.Producer)} {
		assertSourceAuthorityDenied(t, pool, `select * from public.record_birth_keys`)
		assertSourceAuthorityDenied(t, pool, `insert into public.record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,1,'invalid',$1)`, make([]byte, 32))
		assertSourceAuthorityDenied(t, pool, `select record_platform_internal.record_birth_admit('{}'::jsonb,true)`)
	}
	assertSourceAuthorityDenied(t, f.reader, `select public.record_birth_reserve($1)`, []byte(`{}`))
	assertSourceAuthorityDenied(t, f.producer, `select public.source_authority_snapshot()`)
	checkpoint := f.appendCheckpoint(t, f.binding, request)
	reserved, err := producer.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	r, err := recordbirth.VerifyReceipt(reserved, f.key)
	if err != nil || r.Kind != recordbirth.EventReserved || r.Request != request || r.Sequence != 1 {
		t.Fatalf("reserved receipt: %+v %v", r, err)
	}
	if got, err := producer.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint)); err != nil || !bytes.Equal(got, reserved) {
		t.Fatalf("reserve replay: %v", err)
	}
	changed := request
	changed.ActorScopeDigest = sha256.Sum256([]byte("different scope"))
	if _, err := producer.Reserve(ctx, f.binding, changed, f.conflictingCheckpointProof(t, f.binding, f.key, checkpoint, changed)); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("scope conflict: %v", err)
	}
	changed = request
	changed.RequestFingerprint = sha256.Sum256([]byte("different request"))
	if _, err := producer.Reserve(ctx, f.binding, changed, f.conflictingCheckpointProof(t, f.binding, f.key, checkpoint, changed)); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("fingerprint conflict: %v", err)
	}
	initial := sha256.Sum256([]byte("initial commitment"))
	if _, err := producer.Confirm(ctx, f.binding, r.ReservationID, initial); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("confirm without prepare: %v", err)
	}
	prepared, err := producer.Prepare(ctx, f.binding, r.ReservationID, initial)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	p, err := recordbirth.VerifyReceipt(prepared, f.key)
	if err != nil || p.Kind != recordbirth.EventPrepared || p.PreviousHash != sha256.Sum256(reserved) {
		t.Fatalf("prepare chain: %+v %v", p, err)
	}
	if got, err := producer.Prepare(ctx, f.binding, r.ReservationID, initial); err != nil || !bytes.Equal(got, prepared) {
		t.Fatalf("prepare replay: %v", err)
	}
	if _, err := producer.Prepare(ctx, f.binding, r.ReservationID, sha256.Sum256([]byte("other"))); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("prepare conflict: %v", err)
	}
	created, err := producer.Confirm(ctx, f.binding, r.ReservationID, initial)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	c, err := recordbirth.VerifyReceipt(created, f.key)
	if err != nil || c.Kind != recordbirth.EventCreated || c.PreviousHash != sha256.Sum256(prepared) {
		t.Fatalf("created chain: %+v %v", c, err)
	}
	if got, err := producer.Confirm(ctx, f.binding, r.ReservationID, initial); err != nil || !bytes.Equal(got, created) {
		t.Fatalf("confirm replay: %v", err)
	}
	observation, err := reader.Observe(ctx, f.binding, r.RecordID)
	if err != nil || !bytes.Equal(observation.Reserved, reserved) || !bytes.Equal(observation.Prepared, prepared) ||
		!bytes.Equal(observation.Created, created) || observation.HeadSequence != 3 || observation.HeadHash != sha256.Sum256(created) {
		t.Fatalf("created observation: %+v %v", observation, err)
	}
	// The durable request identity excludes the current key/cutover; a trusted
	// offline binding change must replay the original event and never allocate.
	rotated := f.binding
	rotated.KeyID = "bk-" + strings.Repeat("e", 64)
	rotated.CutoverDigest = sha256.Sum256([]byte("next cutover"))
	newKey := bytes.Repeat([]byte{0x43}, 32)
	keyHash := sha256.Sum256(newKey)
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_keys values($1,$2,$3)`, rotated.KeyID, newKey, keyHash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_bindings(generation,authority_id,lineage_id,deployment_id,project_id,key_id,cutover_digest)
		values($1,$2,$3,$4,$5,$6,$7)`, rotated.Generation, rotated.AuthorityID, rotated.LineageID, rotated.DeploymentID, rotated.ProjectID, rotated.KeyID, rotated.CutoverDigest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `update public.record_birth_enabled set key_id=$1,cutover_digest=$2 where singleton`, rotated.KeyID, rotated.CutoverDigest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("retired binding accepted: %v", err)
	}
	if got, err := producer.Reserve(ctx, rotated, request, f.checkpointProof(t, rotated, newKey, checkpoint)); err != nil || !bytes.Equal(got, reserved) {
		t.Fatalf("rotated reserve replay: %v", err)
	}
	f.assertCheckpointCoverage(t, checkpoint)
	if got, err := producer.Confirm(ctx, rotated, r.ReservationID, initial); err != nil || !bytes.Equal(got, created) {
		t.Fatalf("rotated confirm replay: %v", err)
	}
	if got, err := reader.Observe(ctx, rotated, r.RecordID); err != nil || !bytes.Equal(got.Created, created) {
		t.Fatalf("rotated observation: %v", err)
	}
	changed = request
	changed.ActorID = "usr_other"
	otherCheckpoint := f.appendCheckpoint(t, rotated, changed)
	other, err := producer.Reserve(ctx, rotated, changed, f.checkpointProof(t, rotated, newKey, otherCheckpoint))
	if err != nil {
		t.Fatalf("distinct actor reserve: %v", err)
	}
	otherEvent, err := recordbirth.VerifyReceipt(other, newKey)
	if err != nil || otherEvent.RecordID == r.RecordID || otherEvent.ReservationID == r.ReservationID {
		t.Fatal("distinct request reused identity")
	}
	f.assertCheckpointCoverage(t, checkpoint)
	f.assertCheckpointCoverage(t, otherCheckpoint)
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_requests(reservation_id,record_id,authority_id,lineage_id,project_id,actor_id,actor_scope_digest,purpose,idempotency_key,request_fingerprint)
		values($1,$2,$3,$4,$5,'usr_collision',$6,'record_create','collision',$7)`, "rba_"+strings.Repeat("f", 32), r.RecordID, rotated.AuthorityID, rotated.LineageID, rotated.ProjectID, request.ActorScopeDigest[:], request.RequestFingerprint[:]); err == nil {
		t.Fatal("reused record ID accepted")
	}
	producerName := pgx.Identifier{f.roles.BirthProducer}.Sanitize()
	readerName := pgx.Identifier{f.roles.BirthReader}.Sanitize()
	f.exec(t, "alter role "+producerName+" inherit")
	if _, err := producer.Reserve(ctx, rotated, request, f.checkpointProof(t, rotated, newKey, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("unsafe producer attributes admitted: %v", err)
	}
	f.exec(t, "alter role "+producerName+" noinherit")
	f.exec(t, "grant "+producerName+" to "+readerName)
	if _, err := producer.Reserve(ctx, rotated, request, f.checkpointProof(t, rotated, newKey, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("producer with member admitted: %v", err)
	}
	if _, err := reader.Observe(ctx, rotated, r.RecordID); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("reader with membership admitted: %v", err)
	}
	f.exec(t, "revoke "+producerName+" from "+readerName)
	if _, err := f.db.Exec(ctx, `update public.source_authority_state set generation=2 where singleton`); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.Reserve(ctx, rotated, request, f.checkpointProof(t, rotated, newKey, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("old generation admitted: %v", err)
	}
	if _, err := f.db.Exec(ctx, `update public.source_authority_state set generation=1 where singleton`); err != nil {
		t.Fatal(err)
	}
	assertBirthCommandParsing(t, f, rotated, request, f.checkpointProof(t, rotated, newKey, checkpoint))
	assertBirthCodecParity(t, f)
	// Corrupt an otherwise correctly rehashed stored receipt; observe must
	// authenticate the MAC, not merely trust the public chain hash.
	if _, err := f.db.Exec(ctx, `alter table public.record_birth_events disable trigger record_birth_events_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `update public.record_birth_events set
		receipt=set_byte(receipt,octet_length(receipt)-1,get_byte(receipt,octet_length(receipt)-1)#1),
		receipt_hash=record_platform_internal.digest(set_byte(receipt,octet_length(receipt)-1,get_byte(receipt,octet_length(receipt)-1)#1),'sha256')
		where reservation_id=$1 and kind='created'`, r.ReservationID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Observe(ctx, rotated, r.RecordID); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("bad MAC observed: %v", err)
	}
}

func TestPostgresIntegrationRecordBirthRejectsOldProducerAfterRecovery(t *testing.T) {
	f := newBirthAuthorityFixture(t)
	ctx := context.Background()
	producer, err := recordbirth.NewPostgresAuthority(f.producer)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := recordbirth.NewPostgresAuthority(f.reader)
	if err != nil {
		t.Fatal(err)
	}
	if observation, err := reader.Continuity(ctx, f.binding); err != nil || observation.Binding != f.binding || !observation.Enabled {
		t.Fatalf("registered reader before recovery = %+v, %v", observation, err)
	}
	request := recordbirth.Request{ProjectID: "default", ActorID: "usr_recovery", ActorScopeDigest: sha256.Sum256([]byte("recovery scope")),
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: "recovery-key", RequestFingerprint: sha256.Sum256([]byte("recovery request"))}
	checkpoint := f.appendCheckpoint(t, f.binding, request)
	reserved, err := producer.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint))
	if err != nil {
		t.Fatalf("reserve before recovery: %v", err)
	}
	event, err := recordbirth.DecodeReceipt(reserved)
	if err != nil {
		t.Fatal(err)
	}
	recoveryRole := f.roles.Producer + "_rec"
	nextProducer := f.roles.Producer + "_next"
	f.createRole(t, recoveryRole, true)
	f.createRole(t, nextProducer, false)
	f.exec(t, "grant usage on schema public to "+pgx.Identifier{recoveryRole}.Sanitize())
	f.exec(t, "grant execute on function public.source_recovery_seal(text,bytea), public.source_recovery_credentials(text,text[],text[],bytea[],bytea[],bytea), public.source_recovery_advance(text,text,text,bytea,bytea,bytea) to "+pgx.Identifier{recoveryRole}.Sanitize())
	provisioner := f.login(t, f.roles.Provisioner)
	if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'recovery',2,'birth_recovery')`, recoveryRole); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.Exec(ctx, `select public.source_recovery_begin('birth_recovery',$1,$2::name)`, bytes.Repeat([]byte{51}, 32), recoveryRole); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("old producer admitted during recovery: %v", err)
	}
	if _, err := reader.Continuity(ctx, f.binding); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("old continuity reader admitted during recovery: %v", err)
	}
	if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'producer',2,null)`, nextProducer); err != nil {
		t.Fatal(err)
	}
	recovery := f.login(t, recoveryRole)
	if _, err := recovery.Exec(ctx, `select public.source_recovery_seal('birth_recovery',$1)`, bytes.Repeat([]byte{52}, 32)); err != nil {
		t.Fatal(err)
	}
	var seal []byte
	if err := f.db.QueryRow(ctx, `select coverage_seal from public.source_authority_recoveries where recovery_id='birth_recovery'`).Scan(&seal); err != nil {
		t.Fatal(err)
	}
	manifest := bytes.Repeat([]byte{53}, 32)
	if _, err := recovery.Exec(ctx, `select public.source_recovery_credentials('birth_recovery',$1,$2,$3,$4,$5)`,
		[]string{"runtime", "admin", "worker", "migrator", "producer"},
		[]string{"next_runtime", "next_admin", "next_worker", "next_migrator", nextProducer},
		[][]byte{bytes.Repeat([]byte{61}, 32), bytes.Repeat([]byte{62}, 32), bytes.Repeat([]byte{63}, 32), bytes.Repeat([]byte{64}, 32), bytes.Repeat([]byte{65}, 32)},
		[][]byte{bytes.Repeat([]byte{71}, 32), bytes.Repeat([]byte{72}, 32), bytes.Repeat([]byte{73}, 32), bytes.Repeat([]byte{74}, 32), bytes.Repeat([]byte{75}, 32)}, manifest); err != nil {
		t.Fatal(err)
	}
	phases := []string{"credentials_staged", "witness_activated", "app_active_login_closed", "credentials_published", "logins_open", "completed"}
	for i := range len(phases) - 1 {
		if _, err := recovery.Exec(ctx, `select public.source_recovery_advance('birth_recovery',$1,$2,$3,$4,$5)`,
			phases[i], phases[i+1], seal, manifest, bytes.Repeat([]byte{byte(80 + i)}, 32)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := producer.Reserve(ctx, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint)); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("old producer admitted after recovery: %v", err)
	}
	if _, err := producer.Prepare(ctx, f.binding, event.ReservationID, sha256.Sum256([]byte("initial"))); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("old prepare admitted after recovery: %v", err)
	}
	if _, err := reader.Continuity(ctx, f.binding); !errors.Is(err, recordbirth.ErrUnavailable) {
		t.Fatalf("old continuity reader admitted after recovery: %v", err)
	}
	var count, sequence int
	if err := f.db.QueryRow(ctx, `select (select count(*) from public.record_birth_requests),sequence from public.record_birth_head where singleton`).Scan(&count, &sequence); err != nil || count != 1 || sequence != 1 {
		t.Fatalf("recovery rejection changed birth history: requests=%d sequence=%d error=%v", count, sequence, err)
	}
	f.assertCheckpointCoverage(t, checkpoint)
}

func assertBirthCommandParsing(t *testing.T, f *birthAuthorityFixture, b recordbirth.Binding, request recordbirth.Request, proof []byte) {
	t.Helper()
	command := map[string]any{"authority_id": b.AuthorityID, "lineage_id": b.LineageID, "deployment_id": b.DeploymentID,
		"project_id": b.ProjectID, "generation": b.Generation, "key_id": b.KeyID, "cutover_digest": hex.EncodeToString(b.CutoverDigest[:]),
		"actor_id": request.ActorID, "actor_scope_digest": hex.EncodeToString(request.ActorScopeDigest[:]), "purpose": request.Purpose,
		"idempotency_key": request.IdempotencyKey, "request_fingerprint": hex.EncodeToString(request.RequestFingerprint[:]),
		"checkpoint_proof": hex.EncodeToString(proof)}
	base, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		strings.Replace(string(base), "{", `{"authority_id":"`+b.AuthorityID+`",`, 1),
		strings.Replace(string(base), `"generation":1`, `"generation":1e0`, 1),
		strings.Replace(string(base), `"generation":1`, `"generation":9223372036854775808`, 1),
		strings.Replace(string(base), `"generation":1`, `"generation":"1"`, 1),
		strings.Replace(string(base), "{", `{"extra":true,`, 1),
		strings.Replace(string(base), hex.EncodeToString(b.CutoverDigest[:]), strings.Repeat("A", 64), 1),
	}
	delete(command, "checkpoint_proof")
	missingProof, _ := json.Marshal(command)
	command["checkpoint_proof"] = hex.EncodeToString(proof)
	delete(command, "actor_id")
	missing, _ := json.Marshal(command)
	cases = append(cases, string(missing), string(missingProof))
	for _, body := range cases {
		assertSourceAuthorityError(t, f.producer, "22023", `select public.record_birth_reserve($1)`, []byte(body))
	}
}

func assertBirthCodecParity(t *testing.T, f *birthAuthorityFixture) {
	t.Helper()
	var cutover, scope, fingerprint [32]byte
	for i := range cutover {
		cutover[i], scope[i], fingerprint[i] = 6, 8, 7
	}
	binding := recordbirth.Binding{AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
		DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", Generation: 1, KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: cutover}
	request := recordbirth.Request{ProjectID: "default", ActorID: "usr_one", ActorScopeDigest: scope,
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: "create-1", RequestFingerprint: fingerprint}
	event := recordbirth.Event{Binding: binding, Sequence: 1, Kind: recordbirth.EventReserved, ReservationID: "rba_" + strings.Repeat("5", 32),
		RecordID: "rec2_8" + strings.Repeat("0", 31), Request: request}
	encoded, err := recordbirth.EncodeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{9}, 32)
	var sqlEncoded, sqlReceipt []byte
	err = f.db.QueryRow(context.Background(), `select c,c||record_platform_internal.hmac(c,$18::bytea,'sha256') from (
		select record_platform_internal.record_birth_encode(
		row($1::bigint,$2::text,$3::text,$4::text,$5::text,$6::text,$7::bytea)::public.record_birth_bindings,
		row($8::text,$9::text,$2::text,$3::text,$5::text,$10::text,$11::bytea,$12::text,$13::text,$14::bytea)::public.record_birth_requests,
		$15::text,$16::bigint,$17::bytea,decode(repeat('00',32),'hex')) c) v`,
		binding.Generation, binding.AuthorityID, binding.LineageID, binding.DeploymentID, binding.ProjectID, binding.KeyID, binding.CutoverDigest[:],
		event.ReservationID, event.RecordID, request.ActorID, request.ActorScopeDigest[:], string(request.Purpose), request.IdempotencyKey, request.RequestFingerprint[:],
		string(event.Kind), event.Sequence, event.PreviousHash[:], key).Scan(&sqlEncoded, &sqlReceipt)
	if err != nil {
		t.Fatalf("SQL codec vector: %v", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(encoded)
	if !bytes.Equal(encoded, sqlEncoded) || !bytes.Equal(sqlReceipt, append(append([]byte(nil), encoded...), mac.Sum(nil)...)) {
		t.Fatal("SQL/Go fixed receipt codec mismatch")
	}
	if len(encoded) != 639 || hex.EncodeToString(mac.Sum(nil)) != "9460215189261ed92b9349c7ecc2aeb63c5fb896bbc8f422dacd545be555ee5f" {
		t.Fatal("fixed vector canonical length or MAC mismatch")
	}
	receiptHash := sha256.Sum256(sqlReceipt)
	if hex.EncodeToString(receiptHash[:]) != "cd0882cf8ed1bce0bd82c65fed1a8486336c8c4432d87065722746f9cb28ab73" {
		t.Fatal("fixed vector receipt hash mismatch")
	}
	if got, err := recordbirth.VerifyReceipt(sqlReceipt, key); err != nil || got != event {
		t.Fatalf("fixed vector verify: %+v %v", got, err)
	}
	for _, position := range []int{0, len(sqlReceipt) / 2, len(sqlReceipt) - 1} {
		tampered := bytes.Clone(sqlReceipt)
		tampered[position] ^= 1
		if _, err := recordbirth.VerifyReceipt(tampered, key); err == nil {
			t.Fatalf("tamper at %d accepted", position)
		}
	}
}

package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

func birthTestRequest(key string) recordbirth.Request {
	return recordbirth.Request{
		ProjectID: "default", ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		ActorScopeDigest: sha256.Sum256([]byte("birth test scope")), Purpose: recordbirth.PurposeCreate,
		IdempotencyKey: key, RequestFingerprint: sha256.Sum256([]byte("birth test allocation request")),
	}
}

func birthTestCreation(allocation recordbirth.Allocation, revision string) recordbirth.InitialCreation {
	return recordbirth.InitialCreation{
		ReservationID: allocation.ReservationID, RecordID: allocation.RecordID, RevisionID: revision,
		AuthorID: allocation.Request.ActorID, CanonicalHash: sha256.Sum256([]byte("canonical first revision")),
		RequestFingerprint: sha256.Sum256([]byte("canonical revision command")),
	}
}

func TestPostgresIntegrationRecordBirthOrdinary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	repository := NewPostgresRecordBirthRepository(runtime, gate, nil)
	coordinator, err := recordbirth.NewCoordinator(repository, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, enabled, err := repository.CurrentBinding(ctx); err != nil || enabled {
		t.Fatalf("no-source binding = %t, %v", enabled, err)
	}

	request := birthTestRequest("birth-ordinary-replay")
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: request.ActorID, ProjectID: recordauth.ProjectIDDefault, Role: recordauth.RoleProjectAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	request.ActorScopeDigest = actor.CanonicalHash()
	allocation, err := coordinator.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Domain != recordidentity.Ordinary || allocation.State != recordbirth.StateAllocated ||
		recordidentity.Classify(allocation.RecordID) != recordidentity.Ordinary {
		t.Fatalf("ordinary allocation = %+v", allocation)
	}
	const publicationKey = "publish-first-revision"
	publicationDigest := sha256.Sum256([]byte("canonical publication request"))
	if _, found, err := repository.LookupPublication(ctx, actor, publicationKey, publicationDigest); err != nil || found {
		t.Fatalf("new publication lookup = found %t, %v", found, err)
	}
	changedScope := actor
	changedScope.Role = recordauth.RoleViewer
	if _, err := repository.BindPublication(ctx, changedScope, allocation.AllocationID, publicationKey, publicationDigest); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("new binding accepted changed actor scope: %v", err)
	}
	if bound, err := repository.BindPublication(ctx, actor, allocation.AllocationID, publicationKey, publicationDigest); err != nil || bound.AllocationID != allocation.AllocationID {
		t.Fatalf("bind publication = %+v, %v", bound, err)
	}
	if bound, err := repository.BindPublication(ctx, changedScope, allocation.AllocationID, publicationKey, publicationDigest); err != nil || bound.AllocationID != allocation.AllocationID {
		t.Fatalf("same publication after scope change = %+v, %v", bound, err)
	}
	if _, err := repository.BindPublication(ctx, actor, allocation.AllocationID, "different-publication-key", publicationDigest); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("allocation accepted a second publication key: %v", err)
	}
	if _, _, err := repository.LookupPublication(ctx, actor, publicationKey, sha256.Sum256([]byte("changed body"))); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("publication key reused with another body: %v", err)
	}
	otherActor := actor
	otherActor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	if _, found, err := repository.LookupPublication(ctx, otherActor, publicationKey, publicationDigest); err != nil || found {
		t.Fatalf("publication leaked to other actor: found=%t, %v", found, err)
	}
	if _, err := repository.GetAllocation(ctx, otherActor, allocation.AllocationID); err == nil {
		t.Fatal("allocation lookup accepted another actor")
	}
	replayed, err := coordinator.Reserve(ctx, request)
	if err != nil || replayed.AllocationID != allocation.AllocationID || replayed.RecordID != allocation.RecordID || replayed.ReservationID != allocation.ReservationID {
		t.Fatalf("ordinary replay = %+v, %v", replayed, err)
	}
	changed := request
	changed.RequestFingerprint = sha256.Sum256([]byte("different allocation body"))
	if _, _, err := repository.FindAllocation(ctx, changed); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("changed request fingerprint error = %v", err)
	}
	changed = request
	changed.ActorScopeDigest = sha256.Sum256([]byte("different actor scope"))
	if _, err := repository.BeginAllocation(ctx, changed, nil, nil); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("changed actor scope error = %v", err)
	}
	valid, err := json.Marshal(birthRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{
		`null`, `{}`, strings.TrimSuffix(string(valid), "}") + `,"actor_id":"usr_duplicate"}`,
		strings.Replace(string(valid), `"actor_id":"`+request.ActorID+`"`, `"actor_id":null`, 1),
		strings.Replace(string(valid), hex.EncodeToString(request.ActorScopeDigest[:]), strings.Repeat("A", 64), 1),
	} {
		_, err := runtime.Exec(ctx, `select public.record_birth_allocation_find($1)`, []byte(malformed))
		birthRequireSQLState(t, err, "22023")
	}
	begin, err := json.Marshal(struct {
		birthRequestCommand
		birthBindingCommand
		Domain          string `json:"domain"`
		ContinuityProof string `json:"continuity_proof"`
	}{birthRequest(request), birthBindingCommand{}, "ordinary", ""})
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{`"generation":"0"`, `"generation":0.0`, `"generation":0e0`} {
		malformed := strings.Replace(string(begin), `"generation":0`, replacement, 1)
		_, err := runtime.Exec(ctx, `select public.record_birth_allocation_begin($1)`, []byte(malformed))
		birthRequireSQLState(t, err, "22023")
	}

	concurrent := birthTestRequest("birth-concurrent-key")
	const workers = 6
	type outcome struct {
		value recordbirth.Allocation
		err   error
	}
	results := make(chan outcome, workers)
	start := make(chan struct{})
	for range workers {
		go func() {
			<-start
			value, err := repository.BeginAllocation(ctx, concurrent, nil, nil)
			results <- outcome{value, err}
		}()
	}
	close(start)
	var winner recordbirth.Allocation
	for index := range workers {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent allocation: %v", got.err)
		}
		if index == 0 {
			winner = got.value
		}
		if got.value.RecordID != winner.RecordID || got.value.ReservationID != winner.ReservationID || got.value.AllocationID != winner.AllocationID {
			t.Fatalf("same request produced different allocations: %+v versus %+v", got.value, winner)
		}
	}
	otherRequest := birthTestRequest("birth-other-key")
	otherRequest.ActorScopeDigest = actor.CanonicalHash()
	other, err := repository.BeginAllocation(ctx, otherRequest, nil, nil)
	if err != nil || other.RecordID == winner.RecordID || other.ReservationID == winner.ReservationID {
		t.Fatalf("distinct request did not allocate separately: %+v, %v", other, err)
	}
	if _, err := repository.BindPublication(ctx, actor, other.AllocationID, publicationKey, publicationDigest); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("publication key bound to two allocations: %v", err)
	}

	creation := birthTestCreation(allocation, "rrv_birthordinary")
	permit, err := coordinator.PrepareCreate(ctx, request, creation)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Exec(ctx, `insert into public.records(record_id,project_id,lifecycle) values($1,'default','active')`, allocation.RecordID)
	birthRequireSQLState(t, err, "42501")
	_, err = runtime.Exec(ctx, `delete from public.records where record_id=$1`, allocation.RecordID)
	birthRequireSQLState(t, err, "42501")
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator, "birth-legacy-denial", 1)
	_, err = migrator.Exec(ctx, `insert into public.records(record_id,project_id,lifecycle) values('rec_newlegacy','default','active')`)
	birthRequireSQLState(t, err, "42501")

	// The deferred constraint rolls both the provisional root and consume back.
	tx := birthWriterTx(t, ctx, runtime, gate, allocation.RecordID, nil)
	if err := consumeRecordBirthOnTx(ctx, tx, request, permit); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("committed a root without its finished first revision")
	}
	after, found, err := repository.FindAllocation(ctx, request)
	if err != nil || !found || after.State != recordbirth.StatePrepared {
		t.Fatalf("failed consume did not roll back: %+v %v", after, err)
	}

	tx = birthWriterTx(t, ctx, runtime, gate, allocation.RecordID, nil)
	if err := consumeRecordBirthOnTx(ctx, tx, request, permit); err != nil {
		t.Fatal(err)
	}
	birthInsertFirstRevision(t, ctx, tx, creation)
	wrong := permit
	wrong.Creation.CanonicalHash = sha256.Sum256([]byte("different first revision"))
	if err := finishRecordBirthOnTx(ctx, tx, wrong); err == nil {
		t.Fatal("finish accepted a different canonical hash")
	}
	_ = tx.Rollback(ctx)

	tx = birthWriterTx(t, ctx, runtime, gate, allocation.RecordID, nil)
	if err := consumeRecordBirthOnTx(ctx, tx, request, permit); err != nil {
		t.Fatal(err)
	}
	wrongCreation := creation
	wrongCreation.CanonicalHash = sha256.Sum256([]byte("incorrect stored first revision"))
	birthInsertFirstRevision(t, ctx, tx, wrongCreation)
	if err := finishRecordBirthOnTx(ctx, tx, permit); err == nil {
		t.Fatal("finish accepted a different stored first revision")
	}
	_ = tx.Rollback(ctx)

	tx = birthWriterTx(t, ctx, runtime, gate, allocation.RecordID, nil)
	if err := consumeRecordBirthOnTx(ctx, tx, request, permit); err != nil {
		t.Fatal(err)
	}
	birthInsertFirstRevision(t, ctx, tx, creation)
	if err := finishRecordBirthOnTx(ctx, tx, permit); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit exact birth: %v", err)
	}
	committed, err := repository.CommittedCreation(ctx, allocation.RecordID)
	if err != nil || committed.State != recordbirth.StateFinished || committed.Creation != creation {
		t.Fatalf("committed first revision = %+v, %v", committed, err)
	}
	if foundAllocation, found, err := repository.LookupPublication(ctx, changedScope, publicationKey, publicationDigest); err != nil || !found || foundAllocation.State != recordbirth.StateFinished || foundAllocation.Creation != creation {
		t.Fatalf("completed publication replay = %+v, found=%t, %v", foundAllocation, found, err)
	}
	if observed, err := repository.GetAllocation(ctx, changedScope, allocation.AllocationID); err != nil || observed.Creation != creation {
		t.Fatalf("same actor allocation after scope change = %+v, %v", observed, err)
	}
	_, err = runtime.Exec(ctx, `update public.records set record_id=record_id where record_id=$1`, allocation.RecordID)
	birthRequireSQLState(t, err, "42501")

	birthFixtureRemoveRecord(t, ctx, fixture.db, creation)
	after, found, err = repository.FindAllocation(ctx, request)
	if err != nil || !found || after.State != recordbirth.StateTombstoned || after.RecordID != allocation.RecordID {
		t.Fatalf("purge lost birth tombstone: %+v, %v", after, err)
	}
	if foundAllocation, found, err := repository.LookupPublication(ctx, actor, publicationKey, publicationDigest); err != nil || !found || foundAllocation.State != recordbirth.StateTombstoned {
		t.Fatalf("publication tombstone lookup = %+v, found=%t, %v", foundAllocation, found, err)
	}
	if _, err := coordinator.PrepareCreate(ctx, request, creation); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("tombstoned create allowed: %v", err)
	}
	tx = birthWriterTx(t, ctx, runtime, gate, allocation.RecordID, nil)
	if err := consumeRecordBirthOnTx(ctx, tx, request, permit); err == nil {
		t.Fatal("consumed a tombstoned allocation")
	}
	_ = tx.Rollback(ctx)
}

func birthWriterTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, gate AdmissionGate, recordID string, proof []byte) pgx.Tx {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Admit(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select set_config('houfeng.birth_continuity_proof',$1,true)`, hex.EncodeToString(proof)); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	resources, err := normalizeRecordMutationDeclarations(records.RevisionMutationResources{
		Records: []records.RevisionMutationRecord{{ProjectID: "default", RecordID: recordID}},
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := acquireRecordMutationResourceRange(ctx, tx, resources, 10, 50); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if proof == nil {
		proof = []byte{}
	}
	if _, err := tx.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, proof); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := acquireRecordMutationResourceRange(ctx, tx, resources, 60, 60); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	return tx
}

func birthInsertFirstRevision(t *testing.T, ctx context.Context, tx pgx.Tx, creation recordbirth.InitialCreation) {
	t.Helper()
	visibility := sha256.Sum256([]byte("birth test visibility"))
	if _, err := tx.Exec(ctx, `insert into public.record_revisions
		(revision_id,record_id,project_id,revision_no,title,body_markdown,markdown_dialect_version,record_type,
		 impact_level,visibility_scope,visibility_digest,author_id,canonical_hash)
		values($1,$2,'default',1,'first','body',1,'note','informational','{}'::jsonb,$3,$4,$5)`,
		creation.RevisionID, creation.RecordID, visibility[:], creation.AuthorID, creation.CanonicalHash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into public.record_revision_subjects
		(revision_id,ordinal,registry_version,subject_kind,relation_role,source_id,is_primary,identity_snapshot,capture_authorization,capture_authorization_digest)
		values($1,0,1,'vps','affected','vps_birthfixture',true,'{}'::jsonb,'{}'::jsonb,$2)`, creation.RevisionID, visibility[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update public.records set current_revision_id=$2,current_title='first',current_record_type='note',
		current_impact_level='informational',current_visibility_scope='{}'::jsonb,current_visibility_digest=$3 where record_id=$1`,
		creation.RecordID, creation.RevisionID, visibility[:]); err != nil {
		t.Fatal(err)
	}
}

// Fixture-only physical cleanup drives the real root tombstone trigger. The
// production core purge command and its worker authorization are a separate gate.
func birthFixtureRemoveRecord(t *testing.T, ctx context.Context, owner *pgxpool.Pool, creation recordbirth.InitialCreation) {
	t.Helper()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update public.records set current_revision_id=null,current_title=null,current_record_type=null,
		current_impact_level=null,current_visibility_scope=null,current_visibility_digest=null where record_id=$1`, creation.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `delete from public.record_revision_subjects where revision_id=$1`, creation.RevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `delete from public.record_revisions where record_id=$1`, creation.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `delete from public.records where record_id=$1`, creation.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func birthRequireSQLState(t *testing.T, err error, state string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != state {
		t.Fatalf("SQL error = %v, want SQLSTATE %s", err, state)
	}
}

// The W2 side uses the real constrained birth producer and event commands.
// Test-only bootstrap inserts install a shared key/binding after production
// source enrollment; they do not stand in for the offline deployment fence.
func birthTestAuthority(t *testing.T, ctx context.Context, fixture *sourceRetirementPGFixture) (*recordbirth.PostgresAuthority, *recordbirth.PostgresAuthority, recordbirth.Binding) {
	t.Helper()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	producerRole, readerRole := "hf_birth_producer_"+suffix, "hf_birth_reader_"+suffix
	passwords := map[string]string{
		producerRole: recordPlatformIntegrationPassword(t),
		readerRole:   recordPlatformIntegrationPassword(t),
	}
	createdRoles := make([]string, 0, 2)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, role := range createdRoles {
			identifier := pgx.Identifier{role}.Sanitize()
			if _, err := fixture.witnessDB.Exec(cleanup, `drop owned by `+identifier); err != nil {
				t.Errorf("drop W2 birth grants for %s: %v", role, err)
			}
			if _, err := fixture.witnessDB.Exec(cleanup, `drop role `+identifier); err != nil {
				t.Errorf("drop W2 birth role %s: %v", role, err)
			}
		}
	})
	for _, role := range []string{producerRole, readerRole} {
		var createRole string
		if err := fixture.witnessDB.QueryRow(ctx, `select format('create role %I login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L',$1::text,$2::text)`, role, passwords[role]).Scan(&createRole); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.witnessDB.Exec(ctx, createRole); err != nil {
			t.Fatal(err)
		}
		createdRoles = append(createdRoles, role)
	}
	var database string
	if err := fixture.witnessDB.QueryRow(ctx, `select current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct {
		principal string
		functions string
	}{
		{producerRole, "public.record_birth_reserve(bytea),public.record_birth_prepare(bytea),public.record_birth_confirm(bytea),public.record_birth_observe(bytea),public.record_birth_observe_request(bytea)"},
		{readerRole, "public.record_birth_continuity(bytea),public.record_birth_observe(bytea),public.record_birth_observe_request(bytea)"},
	} {
		identifier := pgx.Identifier{role.principal}.Sanitize()
		for _, statement := range []string{
			`grant connect on database ` + pgx.Identifier{database}.Sanitize() + ` to ` + identifier,
			`grant usage on schema public to ` + identifier,
			`grant execute on function ` + role.functions + ` to ` + identifier,
		} {
			if _, err := fixture.witnessDB.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	binding := recordbirth.Binding{LineageID: "bl-" + strings.Repeat("a", 64), DeploymentID: fixture.deployment,
		ProjectID: "default", Generation: 1, KeyID: "bk-" + strings.Repeat("b", 64),
		CutoverDigest: sha256.Sum256([]byte("test-only writer cutover"))}
	if err := fixture.app.db.QueryRow(ctx, `select witness_domain_id from public.source_authority_admission where singleton`).Scan(&binding.AuthorityID); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	keyDigest := sha256.Sum256(key)
	for _, db := range []*pgxpool.Pool{fixture.witnessDB, fixture.app.db} {
		schema := "public"
		if db == fixture.app.db {
			schema = "record_platform_internal"
		}
		prefix := pgx.Identifier{schema}.Sanitize() + "."
		if _, err := db.Exec(ctx, `insert into `+prefix+`record_birth_lineage(singleton,authority_id,lineage_id) values(true,$1,$2)`, binding.AuthorityID, binding.LineageID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `insert into `+prefix+`record_birth_keys(key_id,key_bytes,key_commitment) values($1,$2,$3)`, binding.KeyID, key, keyDigest[:]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `insert into `+prefix+`record_birth_bindings(generation,authority_id,lineage_id,deployment_id,project_id,key_id,cutover_digest)
			values($1,$2,$3,$4,$5,$6,$7)`, binding.Generation, binding.AuthorityID, binding.LineageID, binding.DeploymentID, binding.ProjectID, binding.KeyID, binding.CutoverDigest[:]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `insert into `+prefix+`record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,$1,$2,$3)`, binding.Generation, binding.KeyID, binding.CutoverDigest[:]); err != nil {
			t.Fatal(err)
		}
	}
	// This fixture installs launch state through the owner-only commands after
	// the real source activation. Its private birth rows and synthetic binary
	// digests are not evidence of the protected initializer or offline fence.
	var witness sourceretirement.DomainIdentity
	var witnessOID int64
	var generation, witnessEpoch int64
	var activationDigest []byte
	if err := fixture.app.db.QueryRow(ctx, `select s.witness_domain_id,s.witness_system_identifier,s.witness_database_oid::bigint,s.witness_database_name,s.witness_identity_epoch,s.generation,c.activation_bundle_digest
		from public.source_authority_admission s join public.deployment_contract_state c on c.project_id='default' where s.singleton`).Scan(
		&witness.ID, &witness.SystemIdentifier, &witnessOID, &witness.DatabaseName, &witnessEpoch, &generation, &activationDigest); err != nil {
		t.Fatalf("read fixture launch authority: %v", err)
	}
	if len(activationDigest) != 32 || generation != int64(binding.Generation) || witnessOID <= 0 || witnessOID > int64(^uint32(0)) || witnessEpoch <= 0 {
		t.Fatal("invalid fixture launch activation or source identity")
	}
	witness.DatabaseOID, witness.Epoch = uint32(witnessOID), uint64(witnessEpoch)
	launch := recordbirth.LaunchBinding{
		ProtocolVersion: 1, ProcessKind: "center", DeploymentID: binding.DeploymentID,
		SourceGeneration: binding.Generation, BirthInstalled: true, Witness: witness, BirthBinding: binding,
		BinaryDigest: sha256.Sum256([]byte("fixture center executable")),
	}
	copy(launch.ActivationBundleDigest[:], activationDigest)
	centerBytes, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		t.Fatalf("encode fixture center launch: %v", err)
	}
	launch.ProcessKind = "content-processor"
	launch.BinaryDigest = sha256.Sum256([]byte("fixture processor executable"))
	processorBytes, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		t.Fatalf("encode fixture processor launch: %v", err)
	}
	owner := fixture.app.openDirectRolePool(t, ctx, fixture.app.migrator, "birth-launch-owner", 1)
	var revision int64
	if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`, centerBytes, processorBytes, fixture.app.runtime, fixture.app.deletionWorker).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("stage fixture launch revision=%d: %v", revision, err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatalf("open fixture launch: %v", err)
	}
	runtimeConfig := fixture.runtime.Config().Copy()
	if runtimeConfig.ConnConfig.RuntimeParams == nil {
		runtimeConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	launchDigest := sha256.Sum256(centerBytes)
	runtimeConfig.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = hex.EncodeToString(launchDigest[:])
	launchedRuntime, err := pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatalf("open fixture launch-bound runtime: %v", err)
	}
	t.Cleanup(launchedRuntime.Close)
	fixture.runtime.Close()
	fixture.runtime = launchedRuntime
	var admittedGeneration int64
	if err := fixture.runtime.QueryRow(ctx, `select public.source_application_admit()`).Scan(&admittedGeneration); err != nil || admittedGeneration != generation {
		t.Fatalf("admit launched fixture runtime generation=%d: %v", admittedGeneration, err)
	}
	if _, err := fixture.witnessDB.Exec(ctx, `insert into public.record_birth_principals(principal,purpose,generation) values($1,'producer',1),($2,'reader',1)`, producerRole, readerRole); err != nil {
		t.Fatal(err)
	}
	openBirthPool := func(role string) *pgxpool.Pool {
		t.Helper()
		config := fixture.witnessDB.Config().Copy()
		config.ConnConfig.User, config.ConnConfig.Password = role, passwords[role]
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		var session, current string
		if err := pool.QueryRow(ctx, `select session_user,current_user`).Scan(&session, &current); err != nil || session != role || current != role {
			t.Fatalf("W2 direct birth principal = %q/%q, %v", session, current, err)
		}
		return pool
	}
	producer, err := recordbirth.NewPostgresAuthority(openBirthPool(producerRole))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := recordbirth.NewPostgresContinuityReader(openBirthPool(readerRole), fixture.runtime, witness)
	if err != nil {
		t.Fatal(err)
	}
	return producer, reader, binding
}

func TestPostgresIntegrationRecordBirthReceiptsAndNoDemotion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newSourceRetirementPGFixture(t, ctx)
	fixture.refresh()
	authority, reader, binding := birthTestAuthority(t, ctx, fixture)
	app := NewPostgresRecordBirthRepository(fixture.runtime, fixture.gate, nil)
	coordinator, err := recordbirth.NewCoordinator(app, reader, authority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.db.Exec(ctx, `update public.source_authority_admission set mode='closed' where singleton`); err != nil {
		t.Fatal(err)
	}
	if _, found, err := app.FindAllocation(ctx, birthTestRequest("absent-during-recovery")); err == nil || found {
		t.Fatalf("closed source admitted an absent allocation: found=%t, error=%v", found, err)
	}
	if _, err := fixture.app.db.Exec(ctx, `update public.source_authority_admission set mode='active' where singleton`); err != nil {
		t.Fatal(err)
	}
	request := birthTestRequest("birth-real-w2")
	request.ActorID = "usr_000000000000000000000001"
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: request.ActorID, ProjectID: recordauth.ProjectIDDefault, Role: recordauth.RoleProjectAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	request.ActorScopeDigest = actor.CanonicalHash()
	allocation, err := coordinator.Reserve(ctx, request)
	if err != nil || allocation.Domain != recordidentity.Certifiable {
		t.Fatalf("certified reserve = %+v, %v", allocation, err)
	}
	publicationDigest := sha256.Sum256([]byte("certified publication"))
	if _, err := app.BindPublication(ctx, actor, allocation.AllocationID, "certified-publication", publicationDigest); err == nil {
		t.Fatal("installed lineage accepted publication without continuity")
	}
	publication := NewPostgresRecordBirthRepository(fixture.runtime, fixture.gate, coordinator)
	if bound, err := publication.BindPublication(ctx, actor, allocation.AllocationID, "certified-publication", publicationDigest); err != nil ||
		bound.AllocationID != allocation.AllocationID || len(bound.ContinuityProof) == 0 {
		t.Fatalf("certified publication = %+v, %v", bound, err)
	}
	freshProof := func() []byte {
		t.Helper()
		proof, err := coordinator.ReadContinuity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}
	reserve := append([]byte(nil), allocation.ReserveReceipt...)
	tampered := append([]byte(nil), reserve...)
	tampered[len(tampered)-1] ^= 1
	if _, err := app.RegisterReceipt(ctx, request, tampered, freshProof()); !errors.Is(err, recordbirth.ErrInvalid) {
		t.Fatalf("tampered witness receipt accepted: %v", err)
	}
	creation := birthTestCreation(allocation, "rrv_birthcertified")
	permit, err := coordinator.PrepareCreate(ctx, request, creation)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := app.RegisterReceipt(ctx, request, reserve, freshProof()); err != nil || again.State != recordbirth.StatePrepared {
		t.Fatalf("old reserve demoted prepared state: %+v, %v", again, err)
	}
	proof := freshProof()
	permit.ContinuityProof = proof
	tx := birthWriterTx(t, ctx, fixture.runtime, fixture.gate, allocation.RecordID, proof)
	if err := consumeRecordBirthOnTx(ctx, tx, request, permit); err != nil {
		t.Fatal(err)
	}
	birthInsertFirstRevision(t, ctx, tx, creation)
	if err := finishRecordBirthOnTx(ctx, tx, permit); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ConfirmCreate(ctx, allocation.RecordID); err != nil {
		t.Fatalf("confirm committed birth: %v", err)
	}
	confirmed, found, err := app.FindAllocation(ctx, request)
	if err != nil || !found || confirmed.State != recordbirth.StateConfirmed {
		t.Fatalf("confirmed birth = %+v, %v", confirmed, err)
	}
	if proof, err := coordinator.ReadCertification(ctx, allocation.RecordID); err != nil || proof.RecordID != allocation.RecordID || proof.Binding != binding {
		t.Fatalf("APP/W2 exact birth observation = %+v, %v", proof, err)
	}
	observation, err := authority.Observe(ctx, binding, allocation.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	observation.HeadHash[0] ^= 1
	if _, err := app.VerifyCertification(ctx, allocation.RecordID, observation); err == nil {
		t.Fatal("certification accepted a mismatched witness head")
	}
	for _, pool := range []*pgxpool.Pool{fixture.runtime, fixture.app.openDirectRolePool(t, ctx, fixture.app.deletionWorker, "birth-private-worker", 1)} {
		_, err := pool.Exec(ctx, `select key_bytes from record_platform_internal.record_birth_keys`)
		birthRequireSQLState(t, err, "42501")
		_, err = pool.Exec(ctx, `insert into record_platform_internal.record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,1,'ignored',$1)`, make([]byte, 32))
		birthRequireSQLState(t, err, "42501")
	}
	birthFixtureRemoveRecord(t, ctx, fixture.app.db, creation)
	for _, old := range [][]byte{reserve, permit.PrepareReceipt, confirmed.CreatedReceipt} {
		again, err := app.RegisterReceipt(ctx, request, old, freshProof())
		if err != nil || again.State != recordbirth.StateTombstoned || again.RecordID != allocation.RecordID {
			t.Fatalf("historical receipt demoted tombstone: %+v, %v", again, err)
		}
	}
	if _, err := coordinator.PrepareCreate(ctx, request, creation); !errors.Is(err, recordbirth.ErrConflict) {
		t.Fatalf("prepared receipt resurrected deleted record: %v", err)
	}
}

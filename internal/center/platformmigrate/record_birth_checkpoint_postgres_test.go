package platformmigrate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordbirth"
)

// These helpers model committed APP checkpoints and sign with the explicit
// fixture key. Production code obtains proofs from the restricted APP command.
func (f *birthAuthorityFixture) appendCheckpoint(t *testing.T, binding recordbirth.Binding, request recordbirth.Request) recordbirth.Checkpoint {
	t.Helper()
	checkpoint := recordbirth.Checkpoint{
		Binding: binding, Sequence: f.appHead.Sequence + 1, PreviousHash: f.appHead.Hash,
		AllocationID: fmt.Sprintf("rqa_%032x", f.appHead.Sequence+1), Request: request,
	}
	hash, err := checkpoint.Hash()
	if err != nil {
		t.Fatalf("encode fixture checkpoint: %v", err)
	}
	f.appHead = recordbirth.CheckpointHead{Sequence: checkpoint.Sequence, Hash: hash}
	return checkpoint
}

func (f *birthAuthorityFixture) checkpointAnchor(t *testing.T) recordbirth.CheckpointHead {
	t.Helper()
	var sequence int64
	var hash []byte
	if err := f.db.QueryRow(context.Background(), `select sequence,checkpoint_hash from public.record_birth_checkpoint_anchor where singleton`).Scan(&sequence, &hash); err != nil {
		t.Fatalf("read fixture witness anchor: %v", err)
	}
	if sequence < 0 || len(hash) != sha256.Size {
		t.Fatal("invalid fixture witness anchor")
	}
	var head recordbirth.CheckpointHead
	head.Sequence = uint64(sequence)
	copy(head.Hash[:], hash)
	return head
}

func signFixtureCheckpoint(t *testing.T, binding recordbirth.Binding, key []byte, checkpoint recordbirth.Checkpoint, expected, proposed recordbirth.CheckpointHead) []byte {
	t.Helper()
	proof, err := recordbirth.EncodeCheckpointProof(recordbirth.CheckpointProof{
		Binding: binding, ExpectedAnchor: expected, ProposedHead: proposed, Checkpoint: checkpoint,
	})
	if err != nil {
		t.Fatalf("encode fixture checkpoint proof: %v", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(proof)
	return append(proof, mac.Sum(nil)...)
}

func (f *birthAuthorityFixture) checkpointProof(t *testing.T, binding recordbirth.Binding, key []byte, checkpoint recordbirth.Checkpoint) []byte {
	t.Helper()
	return signFixtureCheckpoint(t, binding, key, checkpoint, f.checkpointAnchor(t), f.appHead)
}

// This deliberately signs a conflicting entry to test the W2 authority boundary.
func (f *birthAuthorityFixture) conflictingCheckpointProof(t *testing.T, binding recordbirth.Binding, key []byte, original recordbirth.Checkpoint, request recordbirth.Request) []byte {
	t.Helper()
	conflict := original
	conflict.Request = request
	conflict.Sequence = f.appHead.Sequence + 1
	conflict.PreviousHash = f.appHead.Hash
	hash, err := conflict.Hash()
	if err != nil {
		t.Fatal(err)
	}
	proposed := recordbirth.CheckpointHead{Sequence: conflict.Sequence, Hash: hash}
	return signFixtureCheckpoint(t, binding, key, conflict, f.checkpointAnchor(t), proposed)
}

func birthFixtureReserveCommand(t *testing.T, binding recordbirth.Binding, request recordbirth.Request, proof []byte) []byte {
	t.Helper()
	command := map[string]any{
		"authority_id": binding.AuthorityID, "lineage_id": binding.LineageID, "deployment_id": binding.DeploymentID,
		"project_id": binding.ProjectID, "generation": binding.Generation, "key_id": binding.KeyID,
		"cutover_digest": hex.EncodeToString(binding.CutoverDigest[:]), "actor_id": request.ActorID,
		"actor_scope_digest": hex.EncodeToString(request.ActorScopeDigest[:]), "purpose": request.Purpose,
		"idempotency_key": request.IdempotencyKey, "request_fingerprint": hex.EncodeToString(request.RequestFingerprint[:]),
		"checkpoint_proof": hex.EncodeToString(proof),
	}
	data, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f *birthAuthorityFixture) reserveCheckpoint(t *testing.T, binding recordbirth.Binding, request recordbirth.Request, proof []byte) []byte {
	t.Helper()
	var receipt []byte
	if err := f.producer.QueryRow(context.Background(), `select public.record_birth_reserve($1)`, birthFixtureReserveCommand(t, binding, request, proof)).Scan(&receipt); err != nil {
		t.Fatalf("reserve fixture checkpoint: %v", err)
	}
	return receipt
}

type birthCheckpointState struct {
	anchor                     recordbirth.CheckpointHead
	coverage, requests, events int64
}

func (f *birthAuthorityFixture) checkpointState(t *testing.T) birthCheckpointState {
	t.Helper()
	state := birthCheckpointState{anchor: f.checkpointAnchor(t)}
	if err := f.db.QueryRow(context.Background(), `select
		(select count(*) from public.record_birth_request_coverage),
		(select count(*) from public.record_birth_requests),
		(select count(*) from public.record_birth_events)`).Scan(&state.coverage, &state.requests, &state.events); err != nil {
		t.Fatalf("read fixture checkpoint state: %v", err)
	}
	return state
}

func (f *birthAuthorityFixture) assertCheckpointCoverage(t *testing.T, checkpoint recordbirth.Checkpoint) {
	t.Helper()
	var allocation string
	var sequence int64
	var hash, entry []byte
	request := checkpoint.Request
	if err := f.db.QueryRow(context.Background(), `select allocation_id,checkpoint_sequence,checkpoint_hash,checkpoint_bytes
		from public.record_birth_request_coverage where lineage_id=$1 and project_id=$2 and actor_id=$3 and purpose=$4 and idempotency_key=$5`,
		checkpoint.Binding.LineageID, request.ProjectID, request.ActorID, string(request.Purpose), request.IdempotencyKey).Scan(&allocation, &sequence, &hash, &entry); err != nil {
		t.Fatalf("read fixture checkpoint coverage: %v", err)
	}
	wantEntry, err := recordbirth.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(wantEntry)
	if allocation != checkpoint.AllocationID || sequence != int64(checkpoint.Sequence) || !bytes.Equal(hash, wantHash[:]) || !bytes.Equal(entry, wantEntry) {
		t.Fatalf("checkpoint identity changed: allocation=%s sequence=%d", allocation, sequence)
	}
}

func TestPostgresIntegrationRecordBirthCheckpointProof(t *testing.T) {
	t.Run("tamper_wrong_mac_and_current_generation", func(t *testing.T) {
		f := newBirthAuthorityFixture(t)
		request := recordbirth.Request{ProjectID: "default", ActorID: "usr_checkpoint", ActorScopeDigest: sha256.Sum256([]byte("checkpoint scope")),
			Purpose: recordbirth.PurposeCreate, IdempotencyKey: "checkpoint-a", RequestFingerprint: sha256.Sum256([]byte("checkpoint a"))}
		checkpoint := f.appendCheckpoint(t, f.binding, request)
		proof := f.checkpointProof(t, f.binding, f.key, checkpoint)
		before := f.checkpointState(t)

		tampered := bytes.Clone(proof)
		at := bytes.Index(tampered, []byte(f.binding.AuthorityID))
		if at < 0 {
			t.Fatal("authority missing from fixture proof")
		}
		at += len("rd-")
		if tampered[at] == 'a' {
			tampered[at] = 'b'
		} else {
			tampered[at] = 'a'
		}
		wrongMAC := signFixtureCheckpoint(t, f.binding, bytes.Repeat([]byte{0x99}, 32), checkpoint, before.anchor, f.appHead)
		wrongGeneration := f.binding
		wrongGeneration.Generation++
		staleBinding := signFixtureCheckpoint(t, wrongGeneration, f.key, checkpoint, before.anchor, f.appHead)
		for name, candidate := range map[string][]byte{"tampered": tampered, "wrong MAC": wrongMAC, "wrong current generation": staleBinding} {
			t.Run(name, func(t *testing.T) {
				assertSourceAuthorityError(t, f.producer, "42501", `select public.record_birth_reserve($1)`, birthFixtureReserveCommand(t, f.binding, request, candidate))
				if got := f.checkpointState(t); got != before {
					t.Fatalf("rejected proof changed witness: before=%+v after=%+v", before, got)
				}
			})
		}
		assertSourceAuthorityError(t, f.producer, "22023", `select public.record_birth_reserve($1)`, birthFixtureReserveCommand(t, f.binding, request, nil))
		if got := f.checkpointState(t); got != before {
			t.Fatalf("missing proof changed witness: before=%+v after=%+v", before, got)
		}
	})

	t.Run("CAS_retry_and_immutable_coverage", func(t *testing.T) {
		f := newBirthAuthorityFixture(t)
		requestA := recordbirth.Request{ProjectID: "default", ActorID: "usr_checkpoint", ActorScopeDigest: sha256.Sum256([]byte("checkpoint scope")),
			Purpose: recordbirth.PurposeCreate, IdempotencyKey: "checkpoint-a", RequestFingerprint: sha256.Sum256([]byte("checkpoint a"))}
		requestB := requestA
		requestB.IdempotencyKey = "checkpoint-b"
		requestB.RequestFingerprint = sha256.Sum256([]byte("checkpoint b"))
		a := f.appendCheckpoint(t, f.binding, requestA)
		headA := f.appHead
		b := f.appendCheckpoint(t, f.binding, requestB)
		headB := f.appHead
		zero := recordbirth.CheckpointHead{}
		proofA := signFixtureCheckpoint(t, f.binding, f.key, a, zero, headA)
		staleB := signFixtureCheckpoint(t, f.binding, f.key, b, zero, headB)
		receiptA := f.reserveCheckpoint(t, f.binding, requestA, proofA)
		before := f.checkpointState(t)
		if before.anchor != headA || before.coverage != 1 || before.requests != 1 || before.events != 1 {
			t.Fatalf("unexpected first reserve state: %+v", before)
		}
		assertSourceAuthorityError(t, f.producer, "40001", `select public.record_birth_reserve($1)`, birthFixtureReserveCommand(t, f.binding, requestB, staleB))
		if got := f.checkpointState(t); got != before {
			t.Fatalf("CAS conflict created effects: before=%+v after=%+v", before, got)
		}
		proofB := f.checkpointProof(t, f.binding, f.key, b)
		receiptB := f.reserveCheckpoint(t, f.binding, requestB, proofB)
		if got := f.reserveCheckpoint(t, f.binding, requestB, f.checkpointProof(t, f.binding, f.key, b)); !bytes.Equal(got, receiptB) {
			t.Fatal("fresh-anchor retry changed the reservation")
		}
		if got := f.reserveCheckpoint(t, f.binding, requestA, f.checkpointProof(t, f.binding, f.key, a)); !bytes.Equal(got, receiptA) {
			t.Fatal("covered request replay changed the reservation")
		}
		f.assertCheckpointCoverage(t, a)
		f.assertCheckpointCoverage(t, b)
		before = f.checkpointState(t)
		if before.anchor != headB || before.coverage != 2 || before.requests != 2 || before.events != 2 {
			t.Fatalf("unexpected retry state: %+v", before)
		}
		conflict := a
		conflict.Sequence = headB.Sequence + 1
		conflict.PreviousHash = headB.Hash
		for name, allocation := range map[string]string{"same allocation changed checkpoint": a.AllocationID, "changed allocation": "rqa_ffffffffffffffffffffffffffffffff"} {
			t.Run(name, func(t *testing.T) {
				conflict.AllocationID = allocation
				hash, err := conflict.Hash()
				if err != nil {
					t.Fatal(err)
				}
				proposed := recordbirth.CheckpointHead{Sequence: conflict.Sequence, Hash: hash}
				proof := signFixtureCheckpoint(t, f.binding, f.key, conflict, before.anchor, proposed)
				assertSourceAuthorityError(t, f.producer, "23505", `select public.record_birth_reserve($1)`, birthFixtureReserveCommand(t, f.binding, requestA, proof))
				if got := f.checkpointState(t); got != before {
					t.Fatalf("coverage conflict changed witness: before=%+v after=%+v", before, got)
				}
				f.assertCheckpointCoverage(t, a)
			})
		}
	})

	t.Run("later_head_includes_uncovered_older_request", func(t *testing.T) {
		f := newBirthAuthorityFixture(t)
		requestA := recordbirth.Request{ProjectID: "default", ActorID: "usr_checkpoint", ActorScopeDigest: sha256.Sum256([]byte("checkpoint scope")),
			Purpose: recordbirth.PurposeCreate, IdempotencyKey: "older-a", RequestFingerprint: sha256.Sum256([]byte("older request"))}
		requestB := requestA
		requestB.IdempotencyKey = "later-b"
		requestB.RequestFingerprint = sha256.Sum256([]byte("later request"))
		a := f.appendCheckpoint(t, f.binding, requestA)
		b := f.appendCheckpoint(t, f.binding, requestB)
		head := f.appHead
		f.reserveCheckpoint(t, f.binding, requestB, f.checkpointProof(t, f.binding, f.key, b))
		before := f.checkpointState(t)
		if before.anchor != head || before.coverage != 1 || before.requests != 1 || before.events != 1 {
			t.Fatalf("unexpected later request state: %+v", before)
		}
		proofA := f.checkpointProof(t, f.binding, f.key, a)
		decoded, err := recordbirth.VerifyCheckpointProof(proofA, f.key)
		if err != nil || decoded.ExpectedAnchor != head || decoded.ProposedHead != head {
			t.Fatalf("fixture did not use the existing head for the older request: %+v %v", decoded, err)
		}
		receiptA := f.reserveCheckpoint(t, f.binding, requestA, proofA)
		if got := f.reserveCheckpoint(t, f.binding, requestA, f.checkpointProof(t, f.binding, f.key, a)); !bytes.Equal(got, receiptA) {
			t.Fatal("older request retry changed its reservation")
		}
		f.assertCheckpointCoverage(t, a)
		f.assertCheckpointCoverage(t, b)
		after := f.checkpointState(t)
		if after.anchor != head || after.coverage != 2 || after.requests != 2 || after.events != 2 {
			t.Fatalf("older request did not retain the later head: %+v", after)
		}
	})
}

func TestPostgresIntegrationRecordBirthCheckpointCatalog(t *testing.T) {
	f := newBirthAuthorityFixture(t)
	ctx := context.Background()
	// This oracle is deliberately enumerated independently of the managed lists.
	tables := []string{
		"record_birth_keys", "record_birth_lineage", "record_birth_bindings", "record_birth_enabled",
		"record_birth_principals", "record_birth_requests", "record_birth_head", "record_birth_events",
		"record_birth_checkpoint_anchor", "record_birth_request_coverage",
	}
	functions := []string{
		"record_platform_internal.record_birth_command(bytea,text[])",
		"record_platform_internal.record_birth_admit(jsonb,boolean)",
		"record_platform_internal.record_birth_checkpoint_decode(bytea)",
		"record_platform_internal.record_birth_checkpoint_accept(jsonb,public.record_birth_bindings)",
		"record_platform_internal.record_birth_binding_bytes(public.record_birth_bindings)",
		"record_platform_internal.record_birth_encode(public.record_birth_bindings,public.record_birth_requests,text,bigint,bytea,bytea)",
		"record_platform_internal.record_birth_verify(public.record_birth_events)",
		"record_platform_internal.record_birth_append(public.record_birth_bindings,public.record_birth_requests,text,bytea)",
		"record_platform_internal.record_birth_transition(bytea,text)",
		"public.record_birth_reserve(bytea)", "public.record_birth_prepare(bytea)",
		"public.record_birth_confirm(bytea)", "public.record_birth_observe(bytea)",
		"public.record_birth_continuity(bytea)", "public.record_birth_observe_request(bytea)",
	}
	var tableCount, functionCount int
	if err := f.db.QueryRow(ctx, `select
		(select count(*) from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		 where n.nspname in ('public','record_platform_internal') and starts_with(c.relname,'record_birth_') and c.relkind in ('r','p','S','v','m','f')),
		(select count(*) from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid=p.pronamespace
		 where n.nspname in ('public','record_platform_internal') and starts_with(p.proname,'record_birth_'))`).Scan(&tableCount, &functionCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != len(tables) || functionCount != len(functions) {
		t.Fatalf("birth catalog size: tables=%d functions=%d", tableCount, functionCount)
	}
	roles := []string{f.roles.Reader, f.roles.Producer, f.roles.Provisioner, f.roles.BirthProducer, f.roles.BirthReader}
	for _, table := range tables {
		identity := "public." + table
		var owner string
		var publicGrant bool
		if err := f.db.QueryRow(ctx, `select pg_catalog.pg_get_userbyid(c.relowner), exists (
			select 1 from pg_catalog.aclexplode(coalesce(c.relacl,pg_catalog.acldefault('r',c.relowner))) a where a.grantee=0)
			from pg_catalog.pg_class c where c.oid=pg_catalog.to_regclass($1) and c.relkind='r'`, identity).Scan(&owner, &publicGrant); err != nil || owner != f.roles.Owner || publicGrant {
			t.Fatalf("birth table %s owner=%q public=%v error=%v", identity, owner, publicGrant, err)
		}
		for _, role := range roles {
			for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
				var granted bool
				if err := f.db.QueryRow(ctx, `select pg_catalog.has_table_privilege($1::name,pg_catalog.to_regclass($2),$3)`, role, identity, privilege).Scan(&granted); err != nil || granted {
					t.Fatalf("birth table %s role=%s privilege=%s granted=%v error=%v", identity, role, privilege, granted, err)
				}
			}
		}
	}
	for _, function := range functions {
		var owner string
		var secured, publicGrant bool
		if err := f.db.QueryRow(ctx, `select pg_catalog.pg_get_userbyid(p.proowner),
			p.prosecdef and coalesce(p.proconfig=array['search_path=pg_catalog']::text[],false), exists (
			select 1 from pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a where a.grantee=0 and a.privilege_type='EXECUTE')
			from pg_catalog.pg_proc p where p.oid=pg_catalog.to_regprocedure($1)`, function).Scan(&owner, &secured, &publicGrant); err != nil || owner != f.roles.Owner || !secured || publicGrant {
			t.Fatalf("birth function %s owner=%q secured=%v public=%v error=%v", function, owner, secured, publicGrant, err)
		}
		for _, role := range roles {
			var granted bool
			if err := f.db.QueryRow(ctx, `select pg_catalog.has_function_privilege($1::name,pg_catalog.to_regprocedure($2),'EXECUTE')`, role, function).Scan(&granted); err != nil {
				t.Fatal(err)
			}
			readerCall := function == "public.record_birth_observe(bytea)" || function == "public.record_birth_continuity(bytea)" || function == "public.record_birth_observe_request(bytea)"
			producerCall := function == "public.record_birth_observe(bytea)" || function == "public.record_birth_observe_request(bytea)" || function == "public.record_birth_reserve(bytea)" || function == "public.record_birth_prepare(bytea)" || function == "public.record_birth_confirm(bytea)"
			want := role == f.roles.BirthProducer && producerCall || role == f.roles.BirthReader && readerCall
			if granted != want {
				t.Fatalf("birth function %s role=%s granted=%v want=%v", function, role, granted, want)
			}
		}
	}
}

func TestPostgresIntegrationRecordBirthDisabledContinuity(t *testing.T) {
	f := newBirthAuthorityFixture(t)
	ctx := context.Background()
	request := recordbirth.Request{ProjectID: "default", ActorID: "usr_continuity", ActorScopeDigest: sha256.Sum256([]byte("continuity scope")),
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: "continuity-a", RequestFingerprint: sha256.Sum256([]byte("continuity request"))}
	checkpoint := f.appendCheckpoint(t, f.binding, request)
	reserved := f.reserveCheckpoint(t, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint))
	event, err := recordbirth.VerifyReceipt(reserved, f.key)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := recordbirth.NewPostgresAuthority(f.producer)
	if err != nil {
		t.Fatal(err)
	}
	initial := sha256.Sum256([]byte("continuity initial"))
	prepared, err := producer.Prepare(ctx, f.binding, event.ReservationID, initial)
	if err != nil {
		t.Fatalf("prepare continuity fixture: %v", err)
	}
	created, err := producer.Confirm(ctx, f.binding, event.ReservationID, initial)
	if err != nil {
		t.Fatalf("confirm continuity fixture: %v", err)
	}
	bindingCommand, err := json.Marshal(map[string]any{
		"authority_id": f.binding.AuthorityID, "lineage_id": f.binding.LineageID,
		"deployment_id": f.binding.DeploymentID, "project_id": f.binding.ProjectID,
		"generation": f.binding.Generation, "key_id": f.binding.KeyID,
		"cutover_digest": hex.EncodeToString(f.binding.CutoverDigest[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	requestCommand := func(request recordbirth.Request) []byte {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal(birthFixtureReserveCommand(t, f.binding, request, nil), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "checkpoint_proof")
		command, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	type physicalIdentity struct {
		system   string
		oid      uint32
		database string
		epoch    int64
	}
	var expected physicalIdentity
	if err := f.db.QueryRow(ctx, `select (select system_identifier::text from pg_catalog.pg_control_system()),
		(select oid from pg_catalog.pg_database where datname=current_database()),current_database(),
		(select identity_epoch from public.record_platform_domain_identity)`).Scan(&expected.system, &expected.oid, &expected.database, &expected.epoch); err != nil {
		t.Fatal(err)
	}
	state := f.checkpointState(t)
	if state.anchor != f.appHead || state.coverage != 1 || state.requests != 1 || state.events != 3 {
		t.Fatalf("continuity fixture state: %+v", state)
	}
	readContinuity := func(pool *pgxpool.Pool, enabled byte) []byte {
		t.Helper()
		var proof []byte
		var actual physicalIdentity
		if err := pool.QueryRow(ctx, `select proof,system_identifier,database_oid,database_name,identity_epoch from public.record_birth_continuity($1)`, bindingCommand).Scan(
			&proof, &actual.system, &actual.oid, &actual.database, &actual.epoch); err != nil {
			t.Fatalf("read continuity: %v", err)
		}
		if actual != expected || len(proof) < 4+8+32+1+sha256.Size {
			t.Fatalf("continuity physical identity or proof length: %+v", actual)
		}
		canonical := proof[:len(proof)-sha256.Size]
		mac := hmac.New(sha256.New, f.key)
		_, _ = mac.Write(canonical)
		domain := "houfeng.record-birth.continuity.v1"
		if binary.BigEndian.Uint32(canonical[:4]) != uint32(len(domain)) || len(canonical) < 4+len(domain)+41 ||
			string(canonical[4:4+len(domain)]) != domain || !hmac.Equal(proof[len(canonical):], mac.Sum(nil)) {
			t.Fatal("continuity framing or MAC mismatch")
		}
		anchorAt := len(canonical) - 41
		if binary.BigEndian.Uint64(canonical[anchorAt:anchorAt+8]) != state.anchor.Sequence ||
			!bytes.Equal(canonical[anchorAt+8:anchorAt+40], state.anchor.Hash[:]) || canonical[anchorAt+40] != enabled {
			t.Fatal("continuity anchor or enabled state mismatch")
		}
		return proof
	}
	readRequest := func(pool *pgxpool.Pool, request recordbirth.Request, exists bool) {
		t.Helper()
		var entry, gotReserved, gotPrepared, gotCreated []byte
		var recordID string
		var actual physicalIdentity
		if err := pool.QueryRow(ctx, `select checkpoint_bytes,coalesce(record_id,''),reserved,prepared,created,
			system_identifier,database_oid,database_name,identity_epoch from public.record_birth_observe_request($1)`, requestCommand(request)).Scan(
			&entry, &recordID, &gotReserved, &gotPrepared, &gotCreated, &actual.system, &actual.oid, &actual.database, &actual.epoch); err != nil {
			t.Fatalf("observe request: %v", err)
		}
		if actual != expected {
			t.Fatalf("request physical identity: %+v", actual)
		}
		if !exists {
			if entry != nil || recordID != "" || gotReserved != nil || gotPrepared != nil || gotCreated != nil {
				t.Fatal("unknown request returned birth history")
			}
			return
		}
		wantEntry, err := recordbirth.EncodeCheckpoint(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(entry, wantEntry) || recordID != event.RecordID || !bytes.Equal(gotReserved, reserved) ||
			!bytes.Equal(gotPrepared, prepared) || !bytes.Equal(gotCreated, created) {
			t.Fatal("observed request did not retain the original allocation and events")
		}
	}
	unknown := request
	unknown.IdempotencyKey = "continuity-unknown"
	readContinuity(f.reader, 1)
	assertSourceAuthorityDenied(t, f.producer, `select * from public.record_birth_continuity($1)`, bindingCommand)
	// Reject even a registered reader if any writer function becomes usable.
	// Each grant is exercised on the same live reader session that invokes
	// continuity and is revoked before the next case.
	for _, function := range []string{"record_birth_reserve", "record_birth_prepare", "record_birth_confirm"} {
		t.Run("reader_misgranted_"+function, func(t *testing.T) {
			target := "public." + pgx.Identifier{function}.Sanitize() + "(bytea)"
			role := pgx.Identifier{f.roles.BirthReader}.Sanitize()
			if _, err := f.db.Exec(ctx, "grant execute on function "+target+" to "+role); err != nil {
				t.Fatalf("misgrant %s to fixture reader: %v", function, err)
			}
			defer func() {
				if _, err := f.db.Exec(context.Background(), "revoke execute on function "+target+" from "+role); err != nil {
					t.Errorf("revoke fixture misgrant %s: %v", function, err)
				}
			}()
			assertSourceAuthorityDenied(t, f.reader, `select * from public.record_birth_continuity($1)`, bindingCommand)
			if after := f.checkpointState(t); after != state {
				t.Fatalf("misgranted reader changed witness: before=%+v after=%+v", state, after)
			}
		})
		readContinuity(f.reader, 1)
	}
	t.Run("producer_without_writer_grants_is_not_reader", func(t *testing.T) {
		role := pgx.Identifier{f.roles.BirthProducer}.Sanitize()
		writers := "public.record_birth_reserve(bytea),public.record_birth_prepare(bytea),public.record_birth_confirm(bytea)"
		if _, err := f.db.Exec(ctx, "revoke execute on function "+writers+" from "+role); err != nil {
			t.Fatalf("remove fixture producer writer grants: %v", err)
		}
		defer func() {
			if _, err := f.db.Exec(context.Background(), "grant execute on function "+writers+" to "+role); err != nil {
				t.Errorf("restore fixture producer grants: %v", err)
			}
		}()
		if _, err := f.db.Exec(ctx, "grant execute on function public.record_birth_continuity(bytea) to "+role); err != nil {
			t.Fatalf("misgrant continuity to fixture producer: %v", err)
		}
		defer func() {
			if _, err := f.db.Exec(context.Background(), "revoke execute on function public.record_birth_continuity(bytea) from "+role); err != nil {
				t.Errorf("revoke fixture producer continuity grant: %v", err)
			}
		}()
		assertSourceAuthorityDenied(t, f.producer, `select * from public.record_birth_continuity($1)`, bindingCommand)
		if after := f.checkpointState(t); after != state {
			t.Fatalf("producer continuity attempt changed witness: before=%+v after=%+v", state, after)
		}
	})
	for _, pool := range []*pgxpool.Pool{f.reader, f.producer} {
		readRequest(pool, request, true)
		readRequest(pool, unknown, false)
		changed := request
		changed.RequestFingerprint = sha256.Sum256([]byte("different request"))
		assertSourceAuthorityError(t, pool, "23505", `select * from public.record_birth_observe_request($1)`, requestCommand(changed))
		assertSourceAuthorityDenied(t, pool, `select * from public.record_birth_checkpoint_anchor`)
		assertSourceAuthorityDenied(t, pool, `select * from public.record_birth_request_coverage`)
	}
	sourceReader := f.login(t, f.roles.Reader)
	assertSourceAuthorityDenied(t, sourceReader, `select * from public.record_birth_continuity($1)`, bindingCommand)
	assertSourceAuthorityDenied(t, sourceReader, `select * from public.record_birth_observe_request($1)`, requestCommand(request))
	t.Run("unregistered_reader_with_execute_is_rejected", func(t *testing.T) {
		role := pgx.Identifier{f.roles.Reader}.Sanitize()
		if _, err := f.db.Exec(ctx, "grant execute on function public.record_birth_continuity(bytea) to "+role); err != nil {
			t.Fatalf("grant fixture source reader continuity: %v", err)
		}
		defer func() {
			if _, err := f.db.Exec(context.Background(), "revoke execute on function public.record_birth_continuity(bytea) from "+role); err != nil {
				t.Errorf("revoke fixture source reader continuity: %v", err)
			}
		}()
		assertSourceAuthorityDenied(t, sourceReader, `select * from public.record_birth_continuity($1)`, bindingCommand)
	})
	if _, err := f.db.Exec(ctx, `delete from public.record_birth_enabled where singleton`); err != nil {
		t.Fatal(err)
	}
	readContinuity(f.reader, 0)
	assertSourceAuthorityDenied(t, f.producer, `select * from public.record_birth_continuity($1)`, bindingCommand)
	for _, pool := range []*pgxpool.Pool{f.reader, f.producer} {
		readRequest(pool, request, true)
	}
	assertSourceAuthorityError(t, f.producer, "42501", `select public.record_birth_reserve($1)`,
		birthFixtureReserveCommand(t, f.binding, request, f.checkpointProof(t, f.binding, f.key, checkpoint)))
	if after := f.checkpointState(t); after != state {
		t.Fatalf("disabled reads or rejected write changed witness: before=%+v after=%+v", state, after)
	}
	// Multiple historical bindings in the same generation cannot identify a
	// current disabled authority; neither reader may guess one.
	other := f.binding
	other.KeyID = "bk-" + strings.Repeat("e", 64)
	other.CutoverDigest = sha256.Sum256([]byte("ambiguous cutover"))
	key := bytes.Repeat([]byte{0x43}, 32)
	commitment := sha256.Sum256(key)
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_keys(key_id,key_bytes,key_commitment) values($1,$2,$3)`, other.KeyID, key, commitment[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `insert into public.record_birth_bindings(generation,authority_id,lineage_id,deployment_id,project_id,key_id,cutover_digest)
		values($1,$2,$3,$4,$5,$6,$7)`, other.Generation, other.AuthorityID, other.LineageID, other.DeploymentID, other.ProjectID, other.KeyID, other.CutoverDigest[:]); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []*pgxpool.Pool{f.reader, f.producer} {
		assertSourceAuthorityError(t, pool, "42501", `select * from public.record_birth_continuity($1)`, bindingCommand)
		assertSourceAuthorityError(t, pool, "42501", `select * from public.record_birth_observe_request($1)`, requestCommand(request))
	}
}

package migrate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordbirth"
)

func TestBirthContinuityBindingRequiresPrivilegeSetV2(t *testing.T) {
	privileges := []AppACLPrivilege{{
		Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction,
		ObjectIdentity: "public.record_birth_continuity_binding()", Privilege: AppACLPrivilegeExecute,
	}}
	body, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, privileges)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCanonicalPrivilegeSetBodyV2(body)
	if err != nil || len(parsed.Privileges) != 1 || parsed.Privileges[0] != privileges[0] {
		t.Fatalf("successor continuity authority = %#v, %v", parsed, err)
	}
	if _, err := CanonicalPrivilegeSetBodyV1(appACLCurrentTransitionBindings, privileges); err == nil {
		t.Fatal("historical encoder accepted successor continuity authority")
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV1(rawCanonicalPrivilegeSetBody(appACLCurrentTransitionBindings, privileges)); err == nil {
		t.Fatal("historical parser accepted successor continuity authority")
	}
	for _, identity := range []string{"public.record_birth_continuity_binding(text)", "public.unlisted_continuity_binding()"} {
		privileges[0].ObjectIdentity = identity
		if _, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, privileges); err == nil {
			t.Fatalf("successor accepted unregistered function identity %q", identity)
		}
	}
}

// This exercises APP's restricted SQL with an admitted direct LOGIN. The
// binding, source admission, and receipt key are isolated test fixtures; this
// test does not assert W2 enrollment, reserve, or deployment fencing.
func TestPostgresIntegrationRecordBirthCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatalf("converge APP: %v", err)
	}
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker)
	admin := fixture.openDirectRolePool(t, ctx, fixture.admin)
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("admit direct runtime: %v", err)
	}
	if err := AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatalf("admit direct worker: %v", err)
	}
	neverInstalled := checkpointBegin(t, ctx, runtime, checkpointBeginCommand(checkpointRequest("checkpoint-never-installed"), nil, nil))
	if neverInstalled.Domain != "ordinary" || neverInstalled.State != "allocated" {
		t.Fatalf("never-installed ordinary allocation = %+v", neverInstalled)
	}
	checkpointAssertHead(t, ctx, owner, recordbirth.CheckpointHead{}, 0)

	binding := recordbirth.Binding{
		AuthorityID: "rd-" + strings.Repeat("a", 64), LineageID: "bl-" + strings.Repeat("b", 64),
		DeploymentID: "dp-" + strings.Repeat("c", 64), ProjectID: "default", Generation: 1,
		KeyID: "bk-" + strings.Repeat("d", 64), CutoverDigest: sha256.Sum256([]byte("checkpoint fixture cutover")),
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	checkpointSeedAuthority(t, ctx, owner, fixture.runtime, fixture.deletionWorker, binding, key)
	activation := birthLaunchActivateFixture(t, ctx, owner, binding.DeploymentID)
	center, processor := birthLaunchFixturePair(binding.DeploymentID, activation.ActivationBundleDigest, &binding)
	centerBytes, processorBytes := birthLaunchEncode(t, center), birthLaunchEncode(t, processor)
	revision := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0)
	birthLaunchOpen(t, ctx, owner, revision)
	runtime = birthLaunchPoolWithDigest(t, ctx, runtime, centerBytes)
	worker = birthLaunchPoolWithDigest(t, ctx, worker, processorBytes)
	var admitted int64
	if err := runtime.QueryRow(ctx, `select public.source_application_admit()`).Scan(&admitted); err != nil || admitted != 1 {
		t.Fatalf("runtime source admission = %d, %v", admitted, err)
	}
	checkpointAssertPrivileges(t, ctx, runtime, admin, worker)

	request := checkpointRequest("checkpoint-first")
	zero := recordbirth.CheckpointHead{}
	continuity := checkpointFixtureContinuity(t, binding, key, zero, true)
	checkpointRejectContinuity(t, ctx, runtime, binding, key)
	begin := checkpointBeginCommand(request, &binding, continuity)
	checkpointAssertHead(t, ctx, owner, zero, 0)

	// A signer in the same transaction as its own allocation must never expose
	// a proof, even if PostgreSQL was switched to READ ONLY after the write.
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rolledBack := checkpointBegin(t, ctx, tx, begin)
	if rolledBack.State != "pending" || rolledBack.Domain != "certifiable" {
		t.Fatalf("uncommitted allocation = %+v", rolledBack)
	}
	checkpointAssertHead(t, ctx, owner, zero, 0)
	checkpointAssertAllocation(t, ctx, owner, rolledBack.AllocationID, 0, 0)
	checkpointSwitchReadOnlyAfterWrite(t, ctx, tx)
	_, err = tx.Exec(ctx, `select public.record_birth_checkpoint_proof($1::bytea)`, checkpointJSON(t, checkpointProofCommand(request, zero)))
	requirePostgresSQLState(t, err, "25000")
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("roll back allocation and checkpoint: %v", err)
	}
	checkpointAssertHead(t, ctx, owner, zero, 0)
	checkpointAssertAllocation(t, ctx, owner, rolledBack.AllocationID, 0, 0)

	// The committed allocation and its checkpoint appear together; replaying
	// the same stable request neither selects a new ID nor appends again.
	tx, err = runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	first := checkpointBegin(t, ctx, tx, begin)
	checkpointAssertHead(t, ctx, owner, zero, 0)
	checkpointAssertAllocation(t, ctx, owner, first.AllocationID, 0, 0)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit allocation and checkpoint: %v", err)
	}
	firstHead := checkpointReadHead(t, ctx, owner)
	checkpointAssertHead(t, ctx, owner, firstHead, 1)
	checkpointAssertAllocation(t, ctx, owner, first.AllocationID, 1, 1)
	if firstHead.Sequence != 1 || firstHead.Hash == ([32]byte{}) {
		t.Fatalf("first committed head = %+v", firstHead)
	}
	replayed := checkpointBegin(t, ctx, runtime, begin)
	if replayed.AllocationID != first.AllocationID || replayed.State != first.State || replayed.Domain != first.Domain {
		t.Fatalf("allocation replay = %+v, want %+v", replayed, first)
	}
	checkpointAssertHead(t, ctx, owner, firstHead, 1)
	ordinary := checkpointBegin(t, ctx, runtime, checkpointBeginCommand(checkpointRequest("checkpoint-ordinary"), nil, continuity))
	if ordinary.State != "allocated" || ordinary.Domain != "ordinary" {
		t.Fatalf("ordinary allocation = %+v", ordinary)
	}
	checkpointAssertHead(t, ctx, owner, firstHead, 1)
	checkpointAssertAllocation(t, ctx, owner, ordinary.AllocationID, 1, 0)
	tamperedContinuity := append([]byte(nil), continuity...)
	tamperedContinuity[len(tamperedContinuity)-1] ^= 1
	for _, key := range []string{"checkpoint-never-installed", "checkpoint-ordinary"} {
		_, err := runtime.Exec(ctx, `select public.record_birth_allocation_begin($1::bytea)`,
			checkpointJSON(t, checkpointBeginCommand(checkpointRequest(key), nil, tamperedContinuity)))
		requirePostgresSQLState(t, err, "42501")
	}
	oldOrdinary := checkpointBegin(t, ctx, runtime, checkpointBeginCommand(checkpointRequest("checkpoint-never-installed"), nil, continuity))
	if oldOrdinary.AllocationID != neverInstalled.AllocationID || oldOrdinary.RecordID != neverInstalled.RecordID {
		t.Fatalf("pre-installation ordinary replay changed identity: %+v, want %+v", oldOrdinary, neverInstalled)
	}
	checkpointAssertEpochContinuity(t, ctx, runtime, ordinary.RecordID, continuity, tamperedContinuity)
	checkpointAssertEpochLockOrder(t, ctx, owner, runtime, ordinary.RecordID, continuity)
	// Even a correctly authenticated W2 anchor cannot authorize a pending
	// replay or a new ordinary allocation when APP has not reached that anchor.
	aheadContinuity := checkpointFixtureContinuity(t, binding, key,
		recordbirth.CheckpointHead{Sequence: 2, Hash: sha256.Sum256([]byte("W2 ahead of APP"))}, true)
	for _, command := range []map[string]any{
		checkpointBeginCommand(checkpointRequest("checkpoint-never-installed"), nil, aheadContinuity),
		checkpointBeginCommand(checkpointRequest("checkpoint-ordinary"), nil, aheadContinuity),
		checkpointBeginCommand(request, &binding, aheadContinuity),
		checkpointBeginCommand(checkpointRequest("checkpoint-blocked-ordinary"), nil, aheadContinuity),
	} {
		_, err := runtime.Exec(ctx, `select public.record_birth_allocation_begin($1::bytea)`, checkpointJSON(t, command))
		requirePostgresSQLState(t, err, "55000")
	}
	var blockedCount int
	if err := owner.QueryRow(ctx, `select count(*)::int from record_platform_internal.record_birth_allocations
		where idempotency_key='checkpoint-blocked-ordinary'`).Scan(&blockedCount); err != nil || blockedCount != 0 {
		t.Fatalf("continuity-unknown ordinary allocations = %d: %v", blockedCount, err)
	}
	checkpointAssertAllocation(t, ctx, owner, ordinary.AllocationID, 1, 0)
	checkpointAssertHead(t, ctx, owner, firstHead, 1)

	// READ WRITE is rejected even before any xid is assigned.
	_, err = runtime.Exec(ctx, `select public.record_birth_checkpoint_proof($1::bytea)`, checkpointJSON(t, checkpointProofCommand(request, zero)))
	requirePostgresSQLState(t, err, "25000")
	proof1Bytes := checkpointProof(t, ctx, runtime, request, zero)
	proof1 := checkpointVerifyProof(t, proof1Bytes, key)
	if proof1.Binding != binding || proof1.ExpectedAnchor != zero || proof1.ProposedHead != firstHead ||
		proof1.Checkpoint.Binding != binding || proof1.Checkpoint.Request != request ||
		proof1.Checkpoint.AllocationID != first.AllocationID || proof1.Checkpoint.Sequence != 1 ||
		proof1.Checkpoint.PreviousHash != ([32]byte{}) {
		t.Fatalf("first proof = %+v", proof1)
	}
	wrongKey := bytes.Repeat([]byte{0x43}, 32)
	if _, err := recordbirth.VerifyCheckpointProof(proof1Bytes, wrongKey); err == nil {
		t.Fatal("proof accepted a different trusted key")
	}

	// A rolled-back savepoint still assigned the top-level transaction an xid.
	// Even a proof of a different already committed request must be rejected.
	tx, err = runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	subtx, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkpointBegin(t, ctx, subtx, checkpointBeginCommand(checkpointRequest("checkpoint-savepoint"), &binding, continuity))
	if err := subtx.Rollback(ctx); err != nil {
		t.Fatalf("roll back savepoint: %v", err)
	}
	checkpointSwitchReadOnlyAfterWrite(t, ctx, tx)
	_, err = tx.Exec(ctx, `select public.record_birth_checkpoint_proof($1::bytea)`, checkpointJSON(t, checkpointProofCommand(request, zero)))
	requirePostgresSQLState(t, err, "25000")
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	checkpointAssertHead(t, ctx, owner, firstHead, 1)

	secondRequest := checkpointRequest("checkpoint-second")
	second := checkpointBegin(t, ctx, runtime, checkpointBeginCommand(secondRequest, &binding, continuity))
	secondHead := checkpointReadHead(t, ctx, owner)
	checkpointAssertHead(t, ctx, owner, secondHead, 2)
	if secondHead.Sequence != 2 || secondHead.Hash == firstHead.Hash {
		t.Fatalf("second committed head = %+v, first = %+v", secondHead, firstHead)
	}
	secondProof := checkpointVerifyProof(t, checkpointProof(t, ctx, runtime, secondRequest, firstHead), key)
	if secondProof.Checkpoint.AllocationID != second.AllocationID || secondProof.Checkpoint.Sequence != 2 ||
		secondProof.Checkpoint.PreviousHash != firstHead.Hash || secondProof.ProposedHead != secondHead {
		t.Fatalf("second checkpoint proof = %+v", secondProof)
	}
	// An older canonical APP anchor is valid: W2 performs the current-anchor
	// CAS. The old request remains included after a later checkpoint is added.
	oldProofBytes := checkpointProof(t, ctx, runtime, request, firstHead)
	oldProof := checkpointVerifyProof(t, oldProofBytes, key)
	if oldProof.ExpectedAnchor != firstHead || oldProof.ProposedHead != secondHead || oldProof.Checkpoint != proof1.Checkpoint {
		t.Fatalf("older included request proof = %+v", oldProof)
	}
	genesisProof := checkpointVerifyProof(t, checkpointProof(t, ctx, runtime, request, zero), key)
	if genesisProof.ExpectedAnchor != zero || genesisProof.ProposedHead != secondHead || genesisProof.Checkpoint != proof1.Checkpoint {
		t.Fatalf("genesis anchor proof = %+v", genesisProof)
	}
	wrongHash := firstHead
	wrongHash.Hash[0] ^= 1
	future := recordbirth.CheckpointHead{Sequence: 3, Hash: sha256.Sum256([]byte("unknown future checkpoint"))}
	wrongGenesis := recordbirth.CheckpointHead{Hash: sha256.Sum256([]byte("nonzero genesis"))}
	for _, anchor := range []recordbirth.CheckpointHead{wrongHash, future, wrongGenesis} {
		checkpointRejectProof(t, ctx, runtime, request, anchor, "55000")
	}
	checkpointRejectProof(t, ctx, runtime, checkpointRequest("checkpoint-ordinary"), zero, "55000")

	// Owner-level attempts at changing historical checkpoint facts are rejected
	// as well as runtime/worker access. This is independent of allocation state.
	for _, statement := range []string{
		`update record_platform_internal.record_birth_checkpoints set checkpoint_hash=checkpoint_hash`,
		`delete from record_platform_internal.record_birth_checkpoints`,
		`truncate record_platform_internal.record_birth_checkpoints`,
	} {
		_, err := owner.Exec(ctx, statement)
		requirePostgresSQLState(t, err, "55000")
	}
	checkpointAssertHead(t, ctx, owner, secondHead, 2)

	checkpointAdvanceAndTombstone(t, ctx, fixture.db, runtime, request, binding, key, continuity, first.AllocationID)
	checkpointAssertHead(t, ctx, owner, secondHead, 2)
	after := checkpointProof(t, ctx, runtime, request, firstHead)
	if !bytes.Equal(after, oldProofBytes) {
		t.Fatal("allocation progression or tombstone changed the committed checkpoint proof")
	}
	if got := checkpointVerifyProof(t, after, key); got.Checkpoint != proof1.Checkpoint || got.ProposedHead != secondHead {
		t.Fatalf("tombstone lost checkpoint identity: %+v", got)
	}
}

type checkpointQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type checkpointAllocation struct {
	AllocationID string `json:"allocation_id"`
	Domain       string `json:"domain"`
	State        string `json:"state"`
	RecordID     string `json:"record_id"`
}

func checkpointSeedAuthority(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtime, worker string, binding recordbirth.Binding, key []byte) {
	t.Helper()
	commitment := sha256.Sum256(key)
	statements := []struct {
		sql  string
		args []any
	}{
		{`insert into public.source_authority_admission(singleton,deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode) values(true,$1,$2,'1',1,'checkpoint_fixture',1,1,'active')`, []any{binding.DeploymentID, binding.AuthorityID}},
		{`insert into public.source_application_principals(principal,generation,purpose) values($1,1,'runtime')`, []any{runtime}},
		{`insert into public.source_application_principals(principal,generation,purpose) values($1,1,'worker')`, []any{worker}},
		{`insert into record_platform_internal.record_birth_lineage(singleton,authority_id,lineage_id) values(true,$1,$2)`, []any{binding.AuthorityID, binding.LineageID}},
		{`insert into record_platform_internal.record_birth_keys(key_id,key_bytes,key_commitment) values($1,$2,$3)`, []any{binding.KeyID, key, commitment[:]}},
		{`insert into record_platform_internal.record_birth_bindings(generation,key_id,cutover_digest,authority_id,lineage_id,deployment_id,project_id) values($1,$2,$3,$4,$5,$6,$7)`, []any{binding.Generation, binding.KeyID, binding.CutoverDigest[:], binding.AuthorityID, binding.LineageID, binding.DeploymentID, binding.ProjectID}},
		{`insert into record_platform_internal.record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,$1,$2,$3)`, []any{binding.Generation, binding.KeyID, binding.CutoverDigest[:]}},
	}
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("install isolated checkpoint fixture: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit isolated checkpoint fixture: %v", err)
	}
}

func checkpointFixtureContinuity(t *testing.T, binding recordbirth.Binding, key []byte, anchor recordbirth.CheckpointHead, enabled bool) []byte {
	t.Helper()
	canonical, err := recordbirth.EncodeContinuityObservation(recordbirth.ContinuityObservation{
		Binding: binding, Anchor: anchor, Enabled: enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return append(canonical, mac.Sum(nil)...)
}

func checkpointRejectContinuity(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, binding recordbirth.Binding, key []byte) {
	t.Helper()
	valid := checkpointFixtureContinuity(t, binding, key, recordbirth.CheckpointHead{}, true)
	if _, err := runtime.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, valid); err != nil {
		t.Fatalf("accept fixture continuity: %v", err)
	}
	tampered := append([]byte(nil), valid...)
	tampered[len(tampered)-1] ^= 1
	ahead := recordbirth.CheckpointHead{Sequence: 1, Hash: sha256.Sum256([]byte("W2 ahead of empty APP"))}
	for _, tc := range []struct {
		name  string
		proof []byte
		state string
	}{
		{"missing", []byte{}, "42501"},
		{"tampered", tampered, "42501"},
		{"authority_ahead", checkpointFixtureContinuity(t, binding, key, ahead, true), "55000"},
		{"disabled_mismatch", checkpointFixtureContinuity(t, binding, key, recordbirth.CheckpointHead{}, false), "55000"},
	} {
		t.Run("continuity_"+tc.name, func(t *testing.T) {
			_, err := runtime.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, tc.proof)
			requirePostgresSQLState(t, err, tc.state)
		})
	}
}

func checkpointAssertEpochContinuity(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, recordID string, valid, tampered []byte) {
	t.Helper()
	const insert = `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch)
		values('default','record',$1,0)`
	for _, tc := range []struct {
		name  string
		proof []byte
	}{{"missing", nil}, {"forged_guc", tampered}} {
		t.Run("epoch_"+tc.name, func(t *testing.T) {
			tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if tc.proof != nil {
				if _, err := tx.Exec(ctx, `select set_config('houfeng.birth_continuity_proof',$1,true)`, hex.EncodeToString(tc.proof)); err != nil {
					t.Fatal(err)
				}
			}
			_, err = tx.Exec(ctx, insert, recordID)
			requirePostgresSQLState(t, err, "42501")
		})
	}
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, valid); err != nil {
		t.Fatalf("assert continuity before epoch: %v", err)
	}
	if _, err := tx.Exec(ctx, insert, recordID); err != nil {
		t.Fatalf("admitted epoch insert: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("roll back admitted epoch fixture: %v", err)
	}
}

// Hold rank 55 in one connection. Epoch's earlier-rank validation must still
// complete, while the ranked assertion must reject the competing head lock.
func checkpointAssertEpochLockOrder(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, recordID string, proof []byte) {
	t.Helper()
	holder, err := owner.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	var singleton bool
	if err := holder.QueryRow(ctx, `select singleton from record_platform_internal.record_birth_checkpoint_head where singleton for update`).Scan(&singleton); err != nil || !singleton {
		t.Fatalf("lock checkpoint head: singleton=%t, %v", singleton, err)
	}
	epoch, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = epoch.Rollback(ctx) }()
	if _, err := epoch.Exec(ctx, `set local statement_timeout='3s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := epoch.Exec(ctx, `select set_config('houfeng.birth_continuity_proof',$1,true)`, hex.EncodeToString(proof)); err != nil {
		t.Fatal(err)
	}
	if _, err := epoch.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch)
		values('default','record',$1,0)`, recordID); err != nil {
		t.Fatalf("epoch validation blocked or failed under held checkpoint head: %v", err)
	}
	if err := epoch.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertion, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = assertion.Rollback(ctx) }()
	if _, err := assertion.Exec(ctx, `set local statement_timeout='3s'`); err != nil {
		t.Fatal(err)
	}
	_, err = assertion.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, proof)
	requirePostgresSQLState(t, err, "40001")
	if err := assertion.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, proof); err != nil {
		t.Fatalf("continuity assertion after head release: %v", err)
	}
}

func checkpointRequest(key string) recordbirth.Request {
	return recordbirth.Request{
		ProjectID: "default", ActorID: "usr_checkpointfixture", ActorScopeDigest: sha256.Sum256([]byte("checkpoint fixture actor")),
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: key, RequestFingerprint: sha256.Sum256([]byte("checkpoint fixture request " + key)),
	}
}

func checkpointRequestFields(request recordbirth.Request) map[string]any {
	return map[string]any{
		"project_id": request.ProjectID, "actor_id": request.ActorID,
		"actor_scope_digest": hex.EncodeToString(request.ActorScopeDigest[:]), "purpose": request.Purpose,
		"idempotency_key": request.IdempotencyKey, "request_fingerprint": hex.EncodeToString(request.RequestFingerprint[:]),
	}
}

func checkpointBindingFields(fields map[string]any, binding recordbirth.Binding) {
	fields["authority_id"], fields["lineage_id"], fields["deployment_id"] = binding.AuthorityID, binding.LineageID, binding.DeploymentID
	fields["generation"], fields["key_id"] = binding.Generation, binding.KeyID
	fields["cutover_digest"] = ""
	if binding != (recordbirth.Binding{}) {
		fields["cutover_digest"] = hex.EncodeToString(binding.CutoverDigest[:])
	}
}

func checkpointBeginCommand(request recordbirth.Request, binding *recordbirth.Binding, continuity []byte) map[string]any {
	fields := checkpointRequestFields(request)
	fields["continuity_proof"] = hex.EncodeToString(continuity)
	fields["domain"] = "ordinary"
	if binding == nil {
		checkpointBindingFields(fields, recordbirth.Binding{})
	} else {
		fields["domain"] = "certifiable"
		checkpointBindingFields(fields, *binding)
	}
	return fields
}

func checkpointJSON(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func checkpointBegin(t *testing.T, ctx context.Context, db checkpointQuery, fields map[string]any) checkpointAllocation {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(ctx, `select public.record_birth_allocation_begin($1::bytea)`, checkpointJSON(t, fields)).Scan(&raw); err != nil {
		t.Fatalf("begin checkpoint allocation: %v", err)
	}
	var allocation checkpointAllocation
	if err := json.Unmarshal(raw, &allocation); err != nil || allocation.AllocationID == "" {
		t.Fatalf("decode checkpoint allocation %s: %v", raw, err)
	}
	return allocation
}

func checkpointReadHead(t *testing.T, ctx context.Context, db checkpointQuery) recordbirth.CheckpointHead {
	t.Helper()
	var sequence int64
	var raw []byte
	if err := db.QueryRow(ctx, `select sequence,checkpoint_hash from record_platform_internal.record_birth_checkpoint_head where singleton`).Scan(&sequence, &raw); err != nil {
		t.Fatalf("read committed checkpoint head: %v", err)
	}
	if sequence < 0 || len(raw) != sha256.Size {
		t.Fatalf("invalid checkpoint head sequence=%d hash length=%d", sequence, len(raw))
	}
	var head recordbirth.CheckpointHead
	head.Sequence = uint64(sequence)
	copy(head.Hash[:], raw)
	return head
}

func checkpointAssertHead(t *testing.T, ctx context.Context, db checkpointQuery, want recordbirth.CheckpointHead, count int) {
	t.Helper()
	got := checkpointReadHead(t, ctx, db)
	if got != want {
		t.Fatalf("checkpoint head = %+v, want %+v", got, want)
	}
	var actual int
	if err := db.QueryRow(ctx, `select count(*)::int from record_platform_internal.record_birth_checkpoints`).Scan(&actual); err != nil || actual != count {
		t.Fatalf("checkpoint count = %d, want %d: %v", actual, count, err)
	}
}

func checkpointAssertAllocation(t *testing.T, ctx context.Context, db checkpointQuery, allocationID string, allocations, checkpoints int) {
	t.Helper()
	var actualAllocations, actualCheckpoints int
	if err := db.QueryRow(ctx, `select count(*)::int, count(c.allocation_id)::int
		from record_platform_internal.record_birth_allocations a
		left join record_platform_internal.record_birth_checkpoints c using (allocation_id)
		where a.allocation_id=$1`, allocationID).Scan(&actualAllocations, &actualCheckpoints); err != nil ||
		actualAllocations != allocations || actualCheckpoints != checkpoints {
		t.Fatalf("allocation %s has %d allocations and %d checkpoints, want %d/%d: %v", allocationID, actualAllocations, actualCheckpoints, allocations, checkpoints, err)
	}
}

func checkpointProofCommand(request recordbirth.Request, anchor recordbirth.CheckpointHead) map[string]any {
	fields := checkpointRequestFields(request)
	fields["expected_sequence"] = anchor.Sequence
	fields["expected_hash"] = hex.EncodeToString(anchor.Hash[:])
	return fields
}

func checkpointProof(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, request recordbirth.Request, anchor recordbirth.CheckpointHead) []byte {
	t.Helper()
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var readOnly, unassigned bool
	if err := tx.QueryRow(ctx, `select current_setting('transaction_read_only')='on', pg_current_xact_id_if_assigned() is null`).Scan(&readOnly, &unassigned); err != nil || !readOnly || !unassigned {
		t.Fatalf("fresh proof transaction readonly=%t xid-unassigned=%t: %v", readOnly, unassigned, err)
	}
	var proof []byte
	if err := tx.QueryRow(ctx, `select public.record_birth_checkpoint_proof($1::bytea)`, checkpointJSON(t, checkpointProofCommand(request, anchor))).Scan(&proof); err != nil {
		t.Fatalf("read committed checkpoint proof: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("finish proof transaction: %v", err)
	}
	return proof
}

func checkpointRejectProof(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, request recordbirth.Request, anchor recordbirth.CheckpointHead, state string) {
	t.Helper()
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `select public.record_birth_checkpoint_proof($1::bytea)`, checkpointJSON(t, checkpointProofCommand(request, anchor)))
	requirePostgresSQLState(t, err, state)
}

func checkpointVerifyProof(t *testing.T, raw, key []byte) recordbirth.CheckpointProof {
	t.Helper()
	decoded, err := recordbirth.DecodeCheckpointProof(raw)
	if err != nil {
		t.Fatalf("decode SQL checkpoint proof: %v", err)
	}
	verified, err := recordbirth.VerifyCheckpointProof(raw, key)
	if err != nil || decoded != verified {
		t.Fatalf("verify SQL checkpoint proof: %v", err)
	}
	return verified
}

func checkpointSwitchReadOnlyAfterWrite(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(ctx, `set local transaction_read_only=on`); err != nil {
		t.Fatalf("switch writing transaction to read only: %v", err)
	}
	var readOnly, assigned bool
	if err := tx.QueryRow(ctx, `select current_setting('transaction_read_only')='on', pg_current_xact_id_if_assigned() is not null`).Scan(&readOnly, &assigned); err != nil || !readOnly || !assigned {
		t.Fatalf("writing transaction readonly=%t xid-assigned=%t: %v", readOnly, assigned, err)
	}
}

func checkpointAssertPrivileges(t *testing.T, ctx context.Context, runtime, admin, worker *pgxpool.Pool) {
	t.Helper()
	for _, role := range []struct {
		name  string
		db    *pgxpool.Pool
		proof bool
	}{{"runtime", runtime, true}, {"admin", admin, false}, {"worker", worker, false}} {
		for _, identity := range []string{"public.record_birth_checkpoint_proof(bytea)",
			"public.record_birth_continuity_binding()", "public.record_birth_continuity_assert(bytea)"} {
			var allowed bool
			if err := role.db.QueryRow(ctx, `select has_function_privilege(current_user,$1,'execute')`, identity).Scan(&allowed); err != nil || allowed != role.proof {
				t.Fatalf("%s EXECUTE %s = %t, want %t: %v", role.name, identity, allowed, role.proof, err)
			}
		}
		for _, name := range []string{"record_birth_binding_bytes", "record_birth_checkpoint_bytes", "record_birth_checkpoint_append",
			"record_birth_installed_binding", "record_birth_continuity_validate", "record_birth_continuity_check", "record_birth_continuity_guard"} {
			var allowed bool
			if err := role.db.QueryRow(ctx, `select has_function_privilege(current_user,p.oid,'execute')
				from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid=p.pronamespace
				where n.nspname='record_platform_internal' and p.proname=$1`, name).Scan(&allowed); err != nil || allowed {
				t.Fatalf("%s internal EXECUTE %s = %t: %v", role.name, name, allowed, err)
			}
		}
		for _, name := range []string{"record_birth_checkpoint_head", "record_birth_checkpoints"} {
			var allowed bool
			if err := role.db.QueryRow(ctx, `select has_table_privilege(current_user,c.oid,'select,insert,update,delete,truncate')
				from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
				where n.nspname='record_platform_internal' and c.relname=$1`, name).Scan(&allowed); err != nil || allowed {
				t.Fatalf("%s checkpoint table access %s = %t: %v", role.name, name, allowed, err)
			}
		}
		if !role.proof {
			_, err := role.db.Exec(ctx, `select public.record_birth_checkpoint_proof($1::bytea)`, []byte(`{}`))
			requirePostgresSQLState(t, err, "42501")
			_, err = role.db.Exec(ctx, `select public.record_birth_continuity_binding()`)
			requirePostgresSQLState(t, err, "42501")
			_, err = role.db.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, []byte{})
			requirePostgresSQLState(t, err, "42501")
		}
	}
}

// Fixture-signed receipts drive the real APP receipt and creation commands.
// They test checkpoint survival only, not W2 provenance or purge authority.
func checkpointAdvanceAndTombstone(t *testing.T, ctx context.Context, bootstrap, runtime *pgxpool.Pool, request recordbirth.Request, binding recordbirth.Binding, key, continuity []byte, allocationID string) {
	t.Helper()
	var superuser bool
	if err := bootstrap.QueryRow(ctx, `select rolsuper from pg_catalog.pg_roles where rolname=session_user`).Scan(&superuser); err != nil || !superuser {
		t.Fatalf("checkpoint tombstone fixture requires the integration runner's superuser bootstrap connection: %v", err)
	}
	reservationID := "rba_" + strings.Repeat("1", 32)
	recordID := "rec2_8" + strings.Repeat("2", 31)
	event := recordbirth.Event{Binding: binding, Sequence: 1, Kind: recordbirth.EventReserved,
		ReservationID: reservationID, RecordID: recordID, Request: request}
	reserve := checkpointRegisterFixtureReceipt(t, ctx, runtime, event, key, continuity, "allocated")
	creation := recordbirth.InitialCreation{ReservationID: reservationID, RecordID: recordID,
		RevisionID: "rrv_checkpointfixture", AuthorID: request.ActorID,
		CanonicalHash:      sha256.Sum256([]byte("checkpoint fixture revision")),
		RequestFingerprint: sha256.Sum256([]byte("checkpoint fixture creation"))}
	fields := checkpointRequestFields(request)
	checkpointCreationFields(fields, creation)
	fields["continuity_proof"] = hex.EncodeToString(continuity)
	checkpointAssertState(t, ctx, runtime, `select public.record_birth_prepare_create($1::bytea)`, fields, allocationID, "preparing")
	commitment, err := creation.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	event.Sequence, event.PreviousHash, event.Kind, event.InitialCommitment = 2, sha256.Sum256(reserve), recordbirth.EventPrepared, commitment
	prepared := checkpointRegisterFixtureReceipt(t, ctx, runtime, event, key, continuity, "prepared")
	if _, err := bootstrap.Exec(ctx, `insert into public.users(user_id,username,password_hash,display_name,role) values($1,$1,'fixture','Checkpoint fixture','admin')`, request.ActorID); err != nil {
		t.Fatalf("insert revision author fixture: %v", err)
	}
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, continuity); err != nil {
		t.Fatalf("assert ranked creation continuity: %v", err)
	}
	if _, err := tx.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values('default','record',$1,0)`, recordID); err != nil {
		t.Fatalf("create record epoch: %v", err)
	}
	fields = checkpointRequestFields(request)
	checkpointCreationFields(fields, creation)
	fields["allocation_id"], fields["prepare_receipt"] = allocationID, hex.EncodeToString(prepared)
	fields["continuity_proof"] = hex.EncodeToString(continuity)
	if _, err := tx.Exec(ctx, `select public.record_birth_consume($1::bytea)`, checkpointJSON(t, fields)); err != nil {
		t.Fatalf("consume prepared birth: %v", err)
	}
	visibility := sha256.Sum256([]byte("checkpoint fixture visibility"))
	if _, err := tx.Exec(ctx, `insert into public.record_revisions(revision_id,record_id,project_id,revision_no,title,body_markdown,markdown_dialect_version,record_type,impact_level,visibility_scope,visibility_digest,author_id,canonical_hash) values($1,$2,'default',1,'first','body',1,'note','informational','{}'::jsonb,$3,$4,$5)`, creation.RevisionID, recordID, visibility[:], creation.AuthorID, creation.CanonicalHash[:]); err != nil {
		t.Fatalf("insert first revision: %v", err)
	}
	if _, err := tx.Exec(ctx, `insert into public.record_revision_subjects(revision_id,ordinal,registry_version,subject_kind,relation_role,source_id,is_primary,identity_snapshot,capture_authorization,capture_authorization_digest) values($1,0,1,'vps','affected','vps_birthfixture',true,'{}'::jsonb,'{}'::jsonb,$2)`, creation.RevisionID, visibility[:]); err != nil {
		t.Fatalf("insert first revision subject: %v", err)
	}
	if _, err := tx.Exec(ctx, `update public.records set current_revision_id=$2,current_title='first',current_record_type='note',current_impact_level='informational',current_visibility_scope='{}'::jsonb,current_visibility_digest=$3 where record_id=$1`, recordID, creation.RevisionID, visibility[:]); err != nil {
		t.Fatalf("publish first revision: %v", err)
	}
	finish := map[string]any{"allocation_id": allocationID, "author_id": creation.AuthorID, "prepare_receipt": hex.EncodeToString(prepared)}
	checkpointCreationFields(finish, creation)
	finish["continuity_proof"] = hex.EncodeToString(continuity)
	if _, err := tx.Exec(ctx, `select public.record_birth_finish($1::bytea)`, checkpointJSON(t, finish)); err != nil {
		t.Fatalf("finish first revision: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first revision: %v", err)
	}
	checkpointAssertState(t, ctx, runtime, `select public.record_birth_allocation_find($1::bytea)`, checkpointRequestFields(request), allocationID, "finished")

	// As in the existing record-birth fixture, superuser cleanup models removal
	// only to exercise the real root tombstone trigger; it is not a purge test.
	tx, err = bootstrap.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update public.records set current_revision_id=null,current_title=null,current_record_type=null,current_impact_level=null,current_visibility_scope=null,current_visibility_digest=null where record_id=$1`, recordID); err != nil {
		t.Fatalf("clear fixture current revision: %v", err)
	}
	if _, err := tx.Exec(ctx, `delete from public.record_revision_subjects where revision_id=$1`, creation.RevisionID); err != nil {
		t.Fatalf("remove fixture revision subject: %v", err)
	}
	if _, err := tx.Exec(ctx, `delete from public.record_revisions where record_id=$1`, recordID); err != nil {
		t.Fatalf("remove fixture revision: %v", err)
	}
	if _, err := tx.Exec(ctx, `delete from public.records where record_id=$1`, recordID); err != nil {
		t.Fatalf("remove fixture root: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fixture root removal: %v", err)
	}
	checkpointAssertState(t, ctx, runtime, `select public.record_birth_allocation_find($1::bytea)`, checkpointRequestFields(request), allocationID, "tombstoned")
}

func checkpointCreationFields(fields map[string]any, creation recordbirth.InitialCreation) {
	fields["reservation_id"], fields["record_id"], fields["revision_id"] = creation.ReservationID, creation.RecordID, creation.RevisionID
	fields["canonical_hash"] = hex.EncodeToString(creation.CanonicalHash[:])
	fields["creation_request_fingerprint"] = hex.EncodeToString(creation.RequestFingerprint[:])
}

func checkpointRegisterFixtureReceipt(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, event recordbirth.Event, key, continuity []byte, state string) []byte {
	t.Helper()
	canonical, err := recordbirth.EncodeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	receipt := append(canonical, mac.Sum(nil)...)
	fields := checkpointRequestFields(event.Request)
	checkpointBindingFields(fields, event.Binding)
	fields["continuity_proof"] = hex.EncodeToString(continuity)
	fields["sequence"], fields["previous_hash"], fields["kind"] = event.Sequence, hex.EncodeToString(event.PreviousHash[:]), event.Kind
	fields["reservation_id"], fields["record_id"] = event.ReservationID, event.RecordID
	fields["initial_commitment"], fields["receipt"] = hex.EncodeToString(event.InitialCommitment[:]), hex.EncodeToString(receipt)
	var raw []byte
	if err := runtime.QueryRow(ctx, `select public.record_birth_receipt_register($1::bytea)`, checkpointJSON(t, fields)).Scan(&raw); err != nil {
		t.Fatalf("register fixture %s receipt: %v", event.Kind, err)
	}
	var allocation checkpointAllocation
	if err := json.Unmarshal(raw, &allocation); err != nil || allocation.State != state || allocation.RecordID != event.RecordID {
		t.Fatalf("fixture %s receipt projection = %s, %v", event.Kind, raw, err)
	}
	return receipt
}

func checkpointAssertState(t *testing.T, ctx context.Context, db checkpointQuery, sql string, fields map[string]any, allocationID, state string) {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(ctx, sql, checkpointJSON(t, fields)).Scan(&raw); err != nil {
		t.Fatalf("read birth state %s: %v", state, err)
	}
	var allocation checkpointAllocation
	if err := json.Unmarshal(raw, &allocation); err != nil || allocation.AllocationID != allocationID || allocation.State != state {
		t.Fatalf("birth state = %s, want %s/%s: %v", raw, allocationID, state, err)
	}
}

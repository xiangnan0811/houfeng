package deletionledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	ledgerMigrations "houfeng/db/deletionledger/migrations"
	witnessMigrations "houfeng/db/deletionwitness/migrations"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
)

// This fixture requires the normal isolated, physically distinct W1/W2 PG
// runner. The fence is an explicit trusted test dependency; production APP
// provenance and the actual maintenance fence are exercised by the coordinator.
func TestPostgresIntegrationSourceRetirementLedger(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required")
	}
	ctx := context.Background()
	primaryAdmin := sourceIsolatedPool(t, ctx, "HOUFENG_DELETION_LEDGER_DATABASE_URL", "w1")
	witnessAdmin := sourceIsolatedPool(t, ctx, "HOUFENG_DELETION_WITNESS_DATABASE_URL", "w2")
	if err := platformmigrate.Apply(ctx, primaryAdmin, ledgerMigrations.FS); err != nil {
		t.Fatal(err)
	}
	witnessSQL, err := witnessMigrations.FS.ReadFile("0001_create_full_witness.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := platformmigrate.Apply(ctx, witnessAdmin, fstest.MapFS{"0001_create_full_witness.sql": &fstest.MapFile{Data: witnessSQL}}); err != nil {
		t.Fatal(err)
	}
	p, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, primaryAdmin, platformmigrate.DomainKindDeletionLedger)
	if err != nil {
		t.Fatal(err)
	}
	w, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, witnessAdmin, platformmigrate.DomainKindDeletionWitness)
	if err != nil {
		t.Fatal(err)
	}
	pID, wID := ledgerIntegrationIdentity(p), ledgerIntegrationIdentity(w)
	primaryRead, primaryWrite := ledgerIntegrationRoles(t, ctx, primaryAdmin, "HOUFENG_DELETION_LEDGER_DATABASE_URL", false)
	witnessRead, witnessWrite := ledgerIntegrationRoles(t, ctx, witnessAdmin, "HOUFENG_DELETION_WITNESS_DATABASE_URL", true)
	primary, err := NewPrimary(primaryWrite, pID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewPrimary(primaryRead, pID)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := NewWitness(witnessWrite, primary, wID)
	if err != nil {
		t.Fatal(err)
	}
	witnessReader, err := NewWitness(witnessRead, reader, wID)
	if err != nil {
		t.Fatal(err)
	}
	trust := recorddeletion.LedgerVerificationTrust{Primary: pID, Witness: wID}

	// An unknown source predecessor is retained as an opaque chain link, then
	// copied from W1 with every original SQL column and never gains authority.
	opaque := []byte("independent opaque source predecessor")
	sourceInsertOpaque(t, ctx, primaryAdmin, false, "sro_opaque", opaque, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	missingRequest := sourceTestRequest(t, recordauth.SourceKindVPS, "vps_0000000000000000", "sro_missing")
	missing, err := reader.ResolveSourceRetirement(ctx, missingRequest)
	if err != nil || missing.Committed {
		t.Fatalf("missing resolution must stay unknown: %+v %v", missing, err)
	}

	cases := []struct {
		kind recordauth.SourceKind
		id   string
	}{
		{recordauth.SourceKindVPS, "vps_0123456789abcdef"},
		{recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"},
		{recordauth.SourceKindTarget, "tg_0123456789abcdef"},
	}
	var commits []SourceRetirementCommit
	var fences []*sourceTestFence
	var entries []SourceRetirementEntry
	var recordEntry recorddeletion.DeletionLedgerEntry
	for i, tc := range cases {
		if i == 1 {
			var err error
			recordEntry, err = primary.AppendDeletionEntry(ctx, ledgerIntegrationRequest(t, "rpo_mixedsource"))
			if err != nil {
				t.Fatalf("append mixed record: %v", err)
			}
		}
		request := sourceTestRequest(t, tc.kind, tc.id, fmt.Sprintf("sro_actual%d", i))
		fence := &sourceTestFence{phase: 1, request: cloneSourceRequest(request)}
		fence.trust = trust
		fence.trust.Continuity = fence
		maker, err := NewSourceRetirementCommitVerifier(fence)
		if err != nil {
			t.Fatal(err)
		}
		commit, err := maker.Verify(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			// Simulate loss after a real committed transaction, not an invented
			// absence result or a second operation identity.
			fence.failAfter = true
			if _, err := primary.AppendSourceRetirement(ctx, commit); err == nil {
				t.Fatal("lost append acknowledgement reported success")
			}
			fence.failAfter = false
			resolved, err := reader.ResolveSourceRetirement(ctx, request)
			if err != nil || !resolved.Committed || resolved.Entry.Sequence != 2 {
				t.Fatalf("resolve committed original: %+v %v", resolved, err)
			}
		}
		entry, err := primary.AppendSourceRetirement(ctx, commit)
		if err != nil {
			t.Fatalf("append %s: %v", tc.kind, err)
		}
		replay, err := primary.AppendSourceRetirement(ctx, commit)
		if err != nil || !equalSourceEntry(entry, replay) {
			t.Fatalf("exact replay %s: %v", tc.kind, err)
		}
		if _, err := reader.AppendSourceRetirement(ctx, commit); err == nil {
			t.Fatal("read-only W1 role appended")
		}
		conflict := cloneSourceRequest(request)
		conflict.ActorUserID = fmt.Sprintf("usr_%024x", 1)
		conflict.RequestFingerprint, err = SourceRequestFingerprint(conflict)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reader.ResolveSourceRetirement(ctx, conflict); err == nil {
			t.Fatal("conflicting original operation resolved")
		}
		conflictFence := &sourceTestFence{phase: 1, request: cloneSourceRequest(conflict), trust: trust}
		conflictFence.trust.Continuity = conflictFence
		conflictMaker, err := NewSourceRetirementCommitVerifier(conflictFence)
		if err != nil {
			t.Fatal(err)
		}
		conflictCommit, err := conflictMaker.Verify(ctx, conflict)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := primary.AppendSourceRetirement(ctx, conflictCommit); err == nil {
			t.Fatal("conflicting original operation appended")
		}
		fence.phase++
		if _, err := primary.AppendSourceRetirement(ctx, commit); err == nil {
			t.Fatal("stale APP phase appended")
		}
		commit, err = maker.Verify(ctx, request)
		if err != nil {
			t.Fatalf("reverify current phase: %v", err)
		}
		commits = append(commits, commit)
		fences = append(fences, fence)
		entries = append(entries, entry)
	}
	locator := recorddeletion.LedgerLocator{LedgerDomain: entries[2].LedgerDomain, Sequence: entries[2].Sequence, EntryHash: entries[2].EntryHash}
	verifier, err := NewSourceRetirementVerifier(reader, witnessReader, fences[2].trust)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(ctx, locator); !errors.Is(err, recorddeletion.ErrLedgerPending) {
		t.Fatalf("unwitnessed source = %v", err)
	}
	if _, err := witnessReader.ConfirmSourceRetirement(ctx, commits[2], entries[2]); err == nil {
		t.Fatal("read-only W2 role copied prefix")
	}
	wrongFence := &sourceTestFence{phase: 1, request: cloneSourceRequest(fences[2].request), trust: trust}
	wrongFence.trust.Continuity = fences[2]
	wrongMaker, err := NewSourceRetirementCommitVerifier(wrongFence)
	if err != nil {
		t.Fatal(err)
	}
	wrongCommit, err := wrongMaker.Verify(ctx, wrongFence.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := witness.ConfirmSourceRetirement(ctx, wrongCommit, entries[2]); err == nil {
		t.Fatal("different continuity guard allowed witness copy")
	}
	fences[2].request.Origin.Revision[0] ^= 1
	if _, err := witness.ConfirmSourceRetirement(ctx, commits[2], entries[2]); err == nil {
		t.Fatal("changed committed APP projection allowed witness copy")
	}
	fences[2].request.Origin.Revision[0] ^= 1
	fences[2].phase++
	if _, err := witness.ConfirmSourceRetirement(ctx, commits[2], entries[2]); err == nil {
		t.Fatal("stale phase allowed witness copy")
	}
	currentMaker, err := NewSourceRetirementCommitVerifier(fences[2])
	if err != nil {
		t.Fatal(err)
	}
	commits[2], err = currentMaker.Verify(ctx, fences[2].request)
	if err != nil {
		t.Fatal(err)
	}
	forged := cloneSourceEntry(entries[2])
	forged.ConfirmedAt = forged.ConfirmedAt.Add(time.Microsecond)
	forged.Canonical, err = EncodeSourceRetirementEntry(forged)
	if err != nil {
		t.Fatal(err)
	}
	forged.EntryHash = sha256.Sum256(forged.Canonical)
	if _, err := witness.ConfirmSourceRetirement(ctx, commits[2], forged); err == nil {
		t.Fatal("caller-fabricated source entry was witnessed")
	}
	if _, err := verifier.Verify(ctx, locator); !errors.Is(err, recorddeletion.ErrLedgerPending) {
		t.Fatalf("rejected witness requests changed W2: %v", err)
	}
	fences[2].failAfter = true
	if result, err := witness.ConfirmSourceRetirement(ctx, commits[2], entries[2]); err == nil || result.Valid() {
		t.Fatal("lost witness acknowledgement yielded authority")
	}
	fences[2].failAfter = false
	verified, err := witness.ConfirmSourceRetirement(ctx, commits[2], entries[2])
	if err != nil || !verified.Valid() || verified.Locator() != locator {
		t.Fatalf("confirm original source prefix: %v", err)
	}
	again, err := verifier.Verify(ctx, locator)
	if err != nil || !again.Valid() || again.ProofDigest() != verified.ProofDigest() {
		t.Fatalf("independent verification: %v", err)
	}
	copyOut := again.Entry()
	copyOut.Canonical[0] ^= 1
	copyOut.Request.FinalFloor.AllowedGroupIDs[0] = "rag_changed"
	if !again.Valid() || !equalSourceEntry(again.Entry(), entries[2]) {
		t.Fatal("verified authority leaked mutable evidence")
	}
	for i := range commits {
		verified, err := witness.ConfirmSourceRetirement(ctx, commits[i], entries[i])
		if err != nil || !verified.Valid() || verified.Entry().EntryHash != entries[i].EntryHash {
			t.Fatalf("earlier source witness replay %d: %v", i, err)
		}
	}
	w1Sources, err := reader.ReadSourceRetirements(ctx)
	if err != nil || len(w1Sources) != 3 {
		t.Fatalf("W1 typed inventory = %d %v", len(w1Sources), err)
	}
	w2Sources, err := witnessReader.ReadSourceRetirements(ctx)
	if err != nil || len(w2Sources) != 3 {
		t.Fatalf("W2 typed inventory = %d %v", len(w2Sources), err)
	}
	for i := range entries {
		if !equalSourceEntry(w1Sources[i], entries[i]) || !equalSourceEntry(w2Sources[i], entries[i]) {
			t.Fatalf("typed inventory mismatch %d", i)
		}
	}
	recordVerifier, err := recorddeletion.NewLedgerVerifier(reader, witnessReader, fences[2].trust)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordVerifier.Verify(ctx, locator); !errors.Is(err, recorddeletion.ErrInvalidLedgerEvidence) {
		t.Fatalf("source entry granted record authority: %v", err)
	}
	recordLocator := recorddeletion.LedgerLocator{LedgerDomain: entries[0].LedgerDomain, Sequence: recordEntry.Sequence, EntryHash: recordEntry.EntryHash}
	if verifiedRecord, err := recordVerifier.Verify(ctx, recordLocator); err != nil || !verifiedRecord.Valid() {
		t.Fatalf("mixed record lost record authority: %v", err)
	}
	if _, err := witness.ConfirmDeletionEntry(ctx, recordEntry); err != nil {
		t.Fatalf("record prefix replay: %v", err)
	}

	// Same canonical bytes/hash but different opaque SQL projection on W2 is a
	// real full-prefix disagreement, not a hash fork. Neither source effect nor
	// source verification may silently accept it.
	divergent := []byte("opaque witness projection disagreement")
	sourceInsertOpaque(t, ctx, primaryAdmin, false, "sro_divergent", divergent, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	sourceInsertOpaque(t, ctx, witnessAdmin, true, "sro_divergent", divergent, "usr_bbbbbbbbbbbbbbbbbbbbbbbb")
	request := sourceTestRequest(t, recordauth.SourceKindVPS, "vps_aaaaaaaaaaaaaaaa", "sro_afterfork")
	forkFence := &sourceTestFence{phase: 1, request: cloneSourceRequest(request), trust: trust}
	forkFence.trust.Continuity = forkFence
	maker, err := NewSourceRetirementCommitVerifier(forkFence)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := maker.Verify(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	last, err := primary.AppendSourceRetirement(ctx, commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := witness.ConfirmSourceRetirement(ctx, commit, last); !errors.Is(err, recorddeletion.ErrLedgerContinuity) {
		t.Fatalf("witness projection disagreement accepted: %v", err)
	}
	if _, err := verifier.Verify(ctx, locator); !errors.Is(err, recorddeletion.ErrLedgerContinuity) {
		t.Fatalf("full prefix disagreement verified: %v", err)
	}

	// SQL accepts a correct canonical hash and a forged operation projection.
	// The actual W1 reader must reject it before any typed authority is issued.
	sourceInsertForgedProjection(t, ctx, primaryAdmin, primary.client.ledgerDomain, request)
	if _, err := reader.ReadSourceRetirements(ctx); !errors.Is(err, recorddeletion.ErrInvalidLedgerEvidence) {
		t.Fatalf("forged source projection read: %v", err)
	}
}

// Keep this test's intentionally divergent immutable chains in dedicated
// disposable databases, including when the record integration test runs in
// the same process against the same pair of PostgreSQL servers.
func sourceIsolatedPool(t *testing.T, ctx context.Context, env, suffix string) *pgxpool.Pool {
	t.Helper()
	base := ledgerIntegrationPool(t, ctx, env)
	name := fmt.Sprintf("hf_src_%s_%d_%d", suffix, os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := base.Exec(ctx, "create database "+quoted); err != nil {
		t.Fatalf("create isolated %s database: %v", suffix, err)
	}
	t.Cleanup(func() {
		if _, err := base.Exec(context.Background(), "drop database "+quoted); err != nil {
			t.Errorf("drop isolated %s database: %v", suffix, err)
		}
	})
	config, err := pgxpool.ParseConfig(os.Getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	dsn, err := url.Parse(os.Getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	dsn.Path, dsn.RawPath = "/"+name, ""
	t.Setenv(env, dsn.String())
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func sourceInsertOpaque(t *testing.T, ctx context.Context, pool *pgxpool.Pool, witness bool, operation string, raw []byte, actor string) {
	t.Helper()
	table, head := "public.deletion_ledger_entries", "public.deletion_ledger_head"
	if witness {
		table, head = "public.deletion_witness_entries", "public.deletion_witness_head"
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sequence int64
	var previous []byte
	if err := tx.QueryRow(ctx, `select sequence,entry_hash from `+head+` where singleton for update`).Scan(&sequence, &previous); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	_, err = tx.Exec(ctx, `insert into `+table+` (sequence,entry_version,entry_type,deployment_id,project_id,operation_id,actor_id,route,object_kind,object_id,
 deletion_request_token_commitment,request_fingerprint,reason_code,release_epoch,confirmed_at,previous_hash,canonical_entry,entry_hash)
 values($1,1,'attempt_not_committed',$2,'default',$3,$4,'source_permanent_delete','vps','vps_legacy',$5,$5,'source_removed',1,$6,$7,$8,$9)`,
		sequence+1, "dp-"+string(bytes.Repeat([]byte{'a'}, 64)), operation, actor, bytes.Repeat([]byte{1}, 32), time.UnixMicro(1).UTC(), previous, raw, hash[:])
	if err != nil {
		t.Fatalf("insert opaque source: %v", err)
	}
	if _, err := tx.Exec(ctx, `update `+head+` set sequence=$1,entry_hash=$2 where singleton`, sequence+1, hash[:]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func sourceInsertForgedProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, domain [32]byte, request SourceRetirementRequest) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sequence int64
	var previous []byte
	if err := tx.QueryRow(ctx, `select sequence,entry_hash from public.deletion_ledger_head where singleton for update`).Scan(&sequence, &previous); err != nil {
		t.Fatal(err)
	}
	request.OperationID = "sro_validcanonical"
	request.RequestFingerprint, err = SourceRequestFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	entry := SourceRetirementEntry{Request: request, LedgerDomain: domain, Sequence: uint64(sequence + 1), ConfirmedAt: time.UnixMicro(1700000002000000).UTC()}
	copy(entry.PreviousHash[:], previous)
	entry.Canonical, err = EncodeSourceRetirementEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = sha256.Sum256(entry.Canonical)
	row, err := sourceStoredRow(entry)
	if err != nil {
		t.Fatal(err)
	}
	row.operation = "sro_forgedprojection"
	if err := insertPrimary(ctx, tx, row); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update public.deletion_ledger_head set sequence=$1,entry_hash=$2 where singleton`, sequence+1, entry.EntryHash[:]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

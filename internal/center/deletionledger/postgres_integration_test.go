package deletionledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	ledgerMigrations "houfeng/db/deletionledger/migrations"
	witnessMigrations "houfeng/db/deletionwitness/migrations"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

type integrationGuard struct{}

func (integrationGuard) VerifyLedgerContinuity(context.Context, recorddeletion.LedgerContinuityObservation) error {
	return nil
}

func TestPostgresIntegrationRecordLedgerAndWitness(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required")
	}
	ctx := context.Background()
	primaryAdmin := ledgerIntegrationPool(t, ctx, "HOUFENG_DELETION_LEDGER_DATABASE_URL")
	witnessAdmin := ledgerIntegrationPool(t, ctx, "HOUFENG_DELETION_WITNESS_DATABASE_URL")
	if err := platformmigrate.Apply(ctx, primaryAdmin, ledgerMigrations.FS); err != nil {
		t.Fatalf("apply W1: %v", err)
	}
	witnessSQL, err := witnessMigrations.FS.ReadFile("0001_create_full_witness.sql")
	if err != nil {
		t.Fatal(err)
	}
	witnessFS := fstest.MapFS{"0001_create_full_witness.sql": &fstest.MapFile{Data: witnessSQL}}
	if err := platformmigrate.Apply(ctx, witnessAdmin, witnessFS); err != nil {
		t.Fatalf("apply W2 predecessor: %v", err)
	}
	primaryIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, primaryAdmin, platformmigrate.DomainKindDeletionLedger)
	if err != nil {
		t.Fatalf("provision W1: %v", err)
	}
	witnessIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, witnessAdmin, platformmigrate.DomainKindDeletionWitness)
	if err != nil {
		t.Fatalf("provision W2: %v", err)
	}
	pID, wID := ledgerIntegrationIdentity(primaryIdentity), ledgerIntegrationIdentity(witnessIdentity)
	primaryReadPool, primaryWritePool := ledgerIntegrationRoles(t, ctx, primaryAdmin, "HOUFENG_DELETION_LEDGER_DATABASE_URL", false)
	witnessReadPool, witnessWritePool := ledgerIntegrationRoles(t, ctx, witnessAdmin, "HOUFENG_DELETION_WITNESS_DATABASE_URL", true)
	primary, err := NewPrimary(primaryWritePool, pID)
	if err != nil {
		t.Fatal(err)
	}
	primaryReader, err := NewPrimary(primaryReadPool, pID)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := NewWitness(witnessWritePool, primary, wID)
	if err != nil {
		t.Fatal(err)
	}
	witnessReader, err := NewWitness(witnessReadPool, primaryReader, wID)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := recorddeletion.LedgerDomainDigest(pID.ID)
	if err != nil {
		t.Fatal(err)
	}
	trust := recorddeletion.LedgerVerificationTrust{Primary: pID, Witness: wID, Continuity: integrationGuard{}}
	verifier, err := recorddeletion.NewLedgerVerifier(primaryReader, witnessReader, trust)
	if err != nil {
		t.Fatal(err)
	}

	// A source row's format is deliberately opaque to the record codec. The
	// copy must preserve its raw chain identity without granting source replay.
	sourceRaw := []byte("opaque source predecessor")
	sourceHash := sha256.Sum256(sourceRaw)
	sourceTx, err := primaryAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sourceTx.Rollback(ctx) }()
	_, err = sourceTx.Exec(ctx, `insert into public.deletion_ledger_entries
 (sequence,entry_version,entry_type,deployment_id,project_id,operation_id,actor_id,route,object_kind,object_id,
 deletion_request_token_commitment,request_fingerprint,reason_code,release_epoch,confirmed_at,previous_hash,canonical_entry,entry_hash)
 values (1,1,'attempt_not_committed',$1,'default','sro_predecessor','usr_aaaaaaaaaaaaaaaaaaaaaaaa',
 'source_permanent_delete','vps','vps_legacy',$2,$2,'source_removed',1,$3,$4,$5,$6)`,
		"dp-"+strings.Repeat("a", 64), bytes.Repeat([]byte{1}, 32), time.UnixMicro(1).UTC(), make([]byte, 32), sourceRaw, sourceHash[:])
	if err != nil {
		t.Fatalf("insert opaque source predecessor: %v", err)
	}
	if _, err := sourceTx.Exec(ctx, `update public.deletion_ledger_head set sequence=1,entry_hash=$1 where singleton`, sourceHash[:]); err != nil {
		t.Fatal(err)
	}
	if err := sourceTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	firstRequest := ledgerIntegrationRequest(t, "rpo_ledgerone")
	first, err := primary.AppendDeletionEntry(ctx, firstRequest)
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	replay, err := primary.AppendDeletionEntry(ctx, firstRequest)
	if err != nil || replay.Sequence != first.Sequence || replay.EntryHash != first.EntryHash {
		t.Fatalf("exact append replay = %+v, %v", replay, err)
	}
	conflict := firstRequest
	conflict.ActorID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := primary.AppendDeletionEntry(ctx, conflict); err == nil {
		t.Fatal("conflicting operation replay accepted")
	}
	missingRequest := ledgerIntegrationRequest(t, "rpo_missing")
	missing, err := primary.ResolveDeletionEntry(ctx, missingRequest)
	if err != nil || missing.Kind != recorddeletion.LedgerResolutionUnresolved || missing.Entry != nil {
		t.Fatalf("missing resolution = %+v, %v", missing, err)
	}
	outcome := missingRequest.AttemptNotCommitted(1)
	if _, err := primary.AppendDeletionEntry(ctx, outcome); err == nil {
		t.Fatal("unfenced absence append accepted")
	}
	if _, err := primaryReader.AppendDeletionEntry(ctx, missingRequest); err == nil {
		t.Fatal("reader appended with restricted role")
	}
	firstLocator := recorddeletion.LedgerLocator{LedgerDomain: domain, Sequence: first.Sequence, EntryHash: first.EntryHash}
	if _, err := verifier.Verify(ctx, firstLocator); !errors.Is(err, recorddeletion.ErrLedgerPending) {
		t.Fatalf("unwitnessed = %v", err)
	}

	secondRequest := ledgerIntegrationRequest(t, "rpo_ledgertwo")
	second, err := primary.AppendDeletionEntry(ctx, secondRequest)
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	secondReceipt, err := witness.ConfirmDeletionEntry(ctx, second)
	if err != nil {
		t.Fatalf("copy complete prefix: %v", err)
	}
	if _, err := verifier.Verify(ctx, recorddeletion.LedgerLocator{LedgerDomain: domain, Sequence: 1, EntryHash: sourceHash}); !errors.Is(err, recorddeletion.ErrInvalidLedgerEvidence) {
		t.Fatalf("source predecessor gained record replay authority: %v", err)
	}
	firstReceipt, err := witness.ConfirmDeletionEntry(ctx, first)
	if err != nil {
		t.Fatalf("confirm earlier copy: %v", err)
	}
	firstAgain, err := witness.ConfirmDeletionEntry(ctx, first)
	if err != nil || firstAgain != firstReceipt {
		t.Fatalf("witness replay = %+v, %v", firstAgain, err)
	}
	verified, err := verifier.Verify(ctx, firstLocator)
	if err != nil || !verified.Valid() || verified.ProofDigest() != firstReceipt.ProofDigest || verified.Entry().EntryHash != first.EntryHash {
		t.Fatalf("physical verification = %+v, %v", verified.Locator(), err)
	}
	secondLocator := recorddeletion.LedgerLocator{LedgerDomain: domain, Sequence: second.Sequence, EntryHash: second.EntryHash}
	verifiedSecond, err := verifier.Verify(ctx, secondLocator)
	if err != nil || !verifiedSecond.Valid() || verifiedSecond.ProofDigest() != secondReceipt.ProofDigest {
		t.Fatalf("second physical verification: %v", err)
	}
	resolved, err := primaryReader.ResolveDeletionEntry(ctx, firstRequest)
	if err != nil || resolved.Kind != recorddeletion.LedgerResolutionCommitted || resolved.Entry == nil || resolved.Entry.EntryHash != first.EntryHash {
		t.Fatalf("exact resolve = %+v, %v", resolved, err)
	}

	// The real schema permits a hash-correct canonical blob whose projected
	// operation differs. Verification must reject it despite a valid SQL hash.
	thirdRequest := ledgerIntegrationRequest(t, "rpo_realidentity")
	third := recorddeletion.CanonicalLedgerEntry{DeletionLedgerEntry: recorddeletion.DeletionLedgerEntry{Request: thirdRequest, Sequence: second.Sequence + 1},
		LedgerDomain: domain, PreviousHash: second.EntryHash, ConfirmedAt: time.UnixMicro(1).UTC()}
	canonical, err := recorddeletion.EncodeCanonicalLedgerEntry(third)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	fingerprint, err := thirdRequest.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	_, err = primaryAdmin.Exec(ctx, `insert into public.deletion_ledger_entries
 (sequence,entry_version,entry_type,deployment_id,project_id,operation_id,actor_id,route,object_kind,object_id,
 deletion_request_token_commitment,request_fingerprint,deletion_contract_version,reason_code,confirmed_at,previous_hash,canonical_entry,entry_hash)
 values ($1,1,'delete_commit',$2,'default','rpo_forged',$3,'record_permanent_delete','record',$4,$5,$6,1,'user_confirmed',$7,$8,$9,$10)`,
		int64(third.Sequence), string(thirdRequest.DeploymentID), thirdRequest.ActorID, thirdRequest.Object.ObjectID,
		thirdRequest.TokenCommitment[:], fingerprint[:], third.ConfirmedAt, third.PreviousHash[:], canonical, hash[:])
	if err != nil {
		t.Fatalf("insert deliberately conflicting predecessor: %v", err)
	}
	_, err = primaryAdmin.Exec(ctx, `update public.deletion_ledger_head set sequence=$1,entry_hash=$2 where singleton`, int64(third.Sequence), hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := primaryReader.ReadLedgerSnapshot(ctx); !errors.Is(err, recorddeletion.ErrInvalidLedgerEvidence) {
		t.Fatalf("projection mismatch accepted: %v", err)
	}
	if _, err := verifier.Verify(ctx, firstLocator); !errors.Is(err, recorddeletion.ErrInvalidLedgerEvidence) {
		t.Fatalf("corrupt primary accepted: %v", err)
	}
}

func ledgerIntegrationPool(t *testing.T, ctx context.Context, env string) *pgxpool.Pool {
	t.Helper()
	url := strings.TrimSpace(os.Getenv(env))
	if url == "" {
		t.Fatalf("%s required", env)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open %s: %v", env, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func ledgerIntegrationIdentity(identity platformmigrate.DomainIdentity) recorddeletion.LedgerPhysicalIdentity {
	return recorddeletion.LedgerPhysicalIdentity{ID: identity.ID, Kind: string(identity.Kind), SystemIdentifier: identity.PostgresSystemIdentifier,
		DatabaseOID: identity.DatabaseOID, DatabaseName: identity.DatabaseName, Epoch: uint64(identity.IdentityEpoch)}
}

func ledgerIntegrationRoles(t *testing.T, ctx context.Context, admin *pgxpool.Pool, env string, witness bool) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	prefix := "g2_ledger_"
	if witness {
		prefix = "g2_witness_"
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	password := "integration-only-password"
	names := []string{prefix + "reader_" + suffix, prefix + "writer_" + suffix}
	var pools []*pgxpool.Pool
	for i, name := range names {
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := admin.Exec(ctx, `create role `+quoted+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password 'integration-only-password'`); err != nil {
			t.Fatalf("create restricted role: %v", err)
		}
		t.Cleanup(func() {
			if _, err := admin.Exec(context.Background(), `drop owned by `+quoted); err != nil {
				t.Errorf("drop role grants: %v", err)
			}
			if _, err := admin.Exec(context.Background(), `drop role `+quoted); err != nil {
				t.Errorf("drop role: %v", err)
			}
		})
		tables := `public.record_platform_domain_identity,public.deletion_ledger_head,public.deletion_ledger_entries,public.deletion_ledger_checkpoints`
		head, entries := `public.deletion_ledger_head`, `public.deletion_ledger_entries`
		if witness {
			tables = `public.record_platform_domain_identity,public.deletion_witness_head,public.deletion_witness_entries`
			head, entries = `public.deletion_witness_head`, `public.deletion_witness_entries`
		}
		grants := []string{`grant select on ` + tables + ` to ` + quoted, `grant execute on function pg_catalog.pg_control_system() to ` + quoted}
		if i == 1 {
			grants = append(grants, `grant insert on `+entries+` to `+quoted, `grant update (sequence,entry_hash,updated_at) on `+head+` to `+quoted,
				`grant usage on schema record_platform_internal to `+quoted,
				`grant execute on function record_platform_internal.digest(bytea,text) to `+quoted)
		}
		for _, grant := range grants {
			if _, err := admin.Exec(ctx, grant); err != nil {
				t.Fatalf("grant restricted privilege: %v", err)
			}
		}
		config, err := pgxpool.ParseConfig(os.Getenv(env))
		if err != nil {
			t.Fatal(err)
		}
		config.ConnConfig.User, config.ConnConfig.Password = name, password
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		pools = append(pools, pool)
	}
	return pools[0], pools[1]
}

func ledgerIntegrationRequest(t *testing.T, operation string) recorddeletion.LedgerAppendRequest {
	t.Helper()
	fingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(bytes.Repeat([]byte{0x31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var token [32]byte
	token[0] = 1
	return recorddeletion.LedgerAppendRequest{EntryType: recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID: recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64)), ProjectID: recordplatform.ProjectIDDefault,
		OperationID: operation, ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:          recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		TokenCommitment: token, RequestFingerprint: fingerprint, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, DeletionContractVersion: 1}
}

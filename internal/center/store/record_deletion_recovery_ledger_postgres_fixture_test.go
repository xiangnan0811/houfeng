package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	ledgermigrations "houfeng/db/deletionledger/migrations"
	witnessmigrations "houfeng/db/deletionwitness/migrations"
	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

var errRecordsRecoveryFence = errors.New("store recovery test fence unavailable")

// recordsPostgresRecoveryFence is the store tests' live maintenance fence for a
// never-enrolled APP. Verification and replay must run inside its held scope;
// it binds one explicit recovery identity and checks continuity by rereading
// both ledger heads directly. The fixture created both ledger databases, so the
// genesis anchors are the actual trusted starting points. Offline isolation of
// APP producers is the production deploy.RecordLedgerFence's responsibility and
// is not exercised here.
type recordsPostgresRecoveryFence struct {
	mu            sync.Mutex
	held          int
	rid           string
	neverEnrolled bool
	primary       recorddeletion.LedgerPhysicalIdentity
	witness       recorddeletion.LedgerPhysicalIdentity
	primaryDB     *pgxpool.Pool
	witnessDB     *pgxpool.Pool
}

func (fence *recordsPostgresRecoveryFence) WithHeldLedgerFence(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil || fn == nil {
		return errRecordsRecoveryFence
	}
	fence.mu.Lock()
	fence.held++
	fence.mu.Unlock()
	defer func() {
		fence.mu.Lock()
		fence.held--
		fence.mu.Unlock()
	}()
	return fn(ctx)
}

func (fence *recordsPostgresRecoveryFence) isHeld() bool {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	return fence.held > 0
}

func (fence *recordsPostgresRecoveryFence) RecoveryBinding() (string, uint64, error) {
	if !fence.isHeld() || !fence.neverEnrolled {
		return "", 0, errRecordsRecoveryFence
	}
	return fence.rid, 0, nil
}

func (fence *recordsPostgresRecoveryFence) NeverEnrolled() (bool, error) {
	if !fence.isHeld() {
		return false, errRecordsRecoveryFence
	}
	return fence.neverEnrolled, nil
}

func (fence *recordsPostgresRecoveryFence) VerifyLedgerContinuity(ctx context.Context, observation recorddeletion.LedgerContinuityObservation) error {
	if ctx == nil || !fence.isHeld() || observation.Primary != fence.primary || observation.Witness != fence.witness ||
		observation.PrimaryAnchor != (recorddeletion.LedgerHead{}) || observation.WitnessAnchor != (recorddeletion.LedgerHead{}) {
		return errRecordsRecoveryFence
	}
	for _, current := range []struct {
		db       *pgxpool.Pool
		query    string
		observed recorddeletion.LedgerHead
	}{
		{fence.primaryDB, `select sequence, entry_hash from public.deletion_ledger_head where singleton`, observation.PrimaryHead},
		{fence.witnessDB, `select sequence, entry_hash from public.deletion_witness_head where singleton`, observation.WitnessHead},
	} {
		var sequence int64
		var hash []byte
		if err := current.db.QueryRow(ctx, current.query).Scan(&sequence, &hash); err != nil {
			return errors.Join(errRecordsRecoveryFence, err)
		}
		if sequence < 0 || uint64(sequence) != current.observed.Sequence || !bytes.Equal(hash, current.observed.Hash[:]) {
			return errRecordsRecoveryFence
		}
	}
	return nil
}

var _ recorddeletion.RecoveryMaintenanceFence = (*recordsPostgresRecoveryFence)(nil)

// recordsPostgresRecoveryLedger owns fresh primary and witness databases on the
// runner's independent ledger servers. Entries are appended, witnessed,
// verified and installed in APP by the production clients and commands.
type recordsPostgresRecoveryLedger struct {
	fence     *recordsPostgresRecoveryFence
	primaryDB *pgxpool.Pool
	primary   *deletionledger.Primary
	witness   *deletionledger.Witness
	verifier  *recorddeletion.LedgerVerifier
	domain    [sha256.Size]byte
	admin     *pgxpool.Pool
	adapter   *recorddeletion.RecoveryAdapter
}

func newRecordsPostgresRecoveryLedger(t *testing.T, ctx context.Context, legacy ImportAttachmentGCFixture) *recordsPostgresRecoveryLedger {
	t.Helper()
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(entropy[:])
	primaryDB, primaryConfig := newRecordsPostgresRecoveryLedgerDatabase(t, ctx, "HOUFENG_DELETION_LEDGER_DATABASE_URL", "hf_rl_"+suffix, []string{"hf_rl_r_" + suffix, "hf_rl_w_" + suffix})
	witnessDB, witnessConfig := newRecordsPostgresRecoveryLedgerDatabase(t, ctx, "HOUFENG_DELETION_WITNESS_DATABASE_URL", "hf_rw_"+suffix, []string{"hf_rw_r_" + suffix, "hf_rw_w_" + suffix})
	if err := platformmigrate.Apply(ctx, primaryDB, ledgermigrations.FS); err != nil {
		t.Fatalf("apply recovery W1: %v", err)
	}
	witnessSQL, err := witnessmigrations.FS.ReadFile("0001_create_full_witness.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := platformmigrate.Apply(ctx, witnessDB, fstest.MapFS{"0001_create_full_witness.sql": &fstest.MapFile{Data: witnessSQL}}); err != nil {
		t.Fatalf("apply recovery W2 predecessor: %v", err)
	}
	primaryIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, primaryDB, platformmigrate.DomainKindDeletionLedger)
	if err != nil {
		t.Fatalf("provision recovery W1 identity: %v", err)
	}
	witnessIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, witnessDB, platformmigrate.DomainKindDeletionWitness)
	if err != nil {
		t.Fatalf("provision recovery W2 identity: %v", err)
	}
	primaryID, witnessID := recordsPostgresRecoveryIdentity(primaryIdentity), recordsPostgresRecoveryIdentity(witnessIdentity)
	primaryRead, primaryWrite := recordsPostgresRecoveryLedgerRoles(t, ctx, primaryDB, primaryConfig, "hf_rl_r_"+suffix, "hf_rl_w_"+suffix, false)
	witnessRead, witnessWrite := recordsPostgresRecoveryLedgerRoles(t, ctx, witnessDB, witnessConfig, "hf_rw_r_"+suffix, "hf_rw_w_"+suffix, true)
	primary, err := deletionledger.NewPrimary(primaryWrite, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	primaryReader, err := deletionledger.NewPrimary(primaryRead, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := deletionledger.NewWitness(witnessWrite, primary, witnessID)
	if err != nil {
		t.Fatal(err)
	}
	witnessReader, err := deletionledger.NewWitness(witnessRead, primaryReader, witnessID)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := recorddeletion.LedgerDomainDigest(primaryID.ID)
	if err != nil {
		t.Fatal(err)
	}
	var neverEnrolled bool
	if err := legacy.platform.db.QueryRow(ctx, `select not exists(select 1 from public.source_authority_admission)`).Scan(&neverEnrolled); err != nil || !neverEnrolled {
		t.Fatalf("recovery fixture requires a never-enrolled APP: never_enrolled=%t error=%v", neverEnrolled, err)
	}
	fence := &recordsPostgresRecoveryFence{
		rid: "store-recovery-" + suffix, neverEnrolled: true,
		primary: primaryID, witness: witnessID, primaryDB: primaryDB, witnessDB: witnessDB,
	}
	verifier, err := recorddeletion.NewLedgerVerifier(primaryReader, witnessReader, recorddeletion.LedgerVerificationTrust{
		Primary: primaryID, Witness: witnessID, Continuity: fence,
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := legacy.platform.openDirectRolePool(t, ctx, legacy.platform.admin, "record-recovery-admin", 2)
	// Production replays through the direct platform-admin pool without the
	// runtime admission gate; the SQL commands grant replay only to that role.
	adapter, err := recorddeletion.NewRecoveryAdapter(NewPostgresRecordDeletionRepository(admin, nil), verifier, fence)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	return &recordsPostgresRecoveryLedger{
		fence: fence, primaryDB: primaryDB, primary: primary, witness: witness,
		verifier: verifier, domain: domain, admin: admin, adapter: adapter,
	}
}

func recordsPostgresRecoveryIdentity(identity platformmigrate.DomainIdentity) recorddeletion.LedgerPhysicalIdentity {
	return recorddeletion.LedgerPhysicalIdentity{ID: identity.ID, Kind: string(identity.Kind), SystemIdentifier: identity.PostgresSystemIdentifier,
		DatabaseOID: identity.DatabaseOID, DatabaseName: identity.DatabaseName, Epoch: uint64(identity.IdentityEpoch)}
}

// The database and its restricted roles are owned by this test alone and are
// removed after every pool registered later has closed.
func newRecordsPostgresRecoveryLedgerDatabase(t *testing.T, ctx context.Context, env, database string, roles []string) (*pgxpool.Pool, *pgxpool.Config) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv(env))
	if url == "" {
		t.Fatalf("%s is required for record deletion recovery", env)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil || config.ConnConfig.User != "postgres" {
		t.Fatalf("%s must be a direct postgres bootstrap URL: %v", env, err)
	}
	bootstrap, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bootstrap.Close)
	if _, err := bootstrap.Exec(ctx, "create database "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatalf("create recovery ledger database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := bootstrap.Exec(cleanup, "drop database "+pgx.Identifier{database}.Sanitize()+" with (force)"); err != nil {
			t.Errorf("drop recovery ledger database: %v", err)
		}
		for _, role := range roles {
			if _, err := bootstrap.Exec(cleanup, "drop role if exists "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Errorf("drop recovery ledger role: %v", err)
			}
		}
	})
	owned := config.Copy()
	owned.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, owned)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, owned
}

func recordsPostgresRecoveryLedgerRoles(t *testing.T, ctx context.Context, admin *pgxpool.Pool, config *pgxpool.Config, reader, writer string, witness bool) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	tables := `public.record_platform_domain_identity,public.deletion_ledger_head,public.deletion_ledger_entries,public.deletion_ledger_checkpoints`
	head, entries := `public.deletion_ledger_head`, `public.deletion_ledger_entries`
	if witness {
		tables = `public.record_platform_domain_identity,public.deletion_witness_head,public.deletion_witness_entries`
		head, entries = `public.deletion_witness_head`, `public.deletion_witness_entries`
	}
	var pools []*pgxpool.Pool
	for index, role := range []string{reader, writer} {
		password := recordPlatformIntegrationPassword(t)
		quoted := pgx.Identifier{role}.Sanitize()
		var create string
		if err := admin.QueryRow(ctx, `select format('create role %I login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L', $1::text, $2::text)`, role, password).Scan(&create); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, create); err != nil {
			t.Fatalf("create restricted recovery ledger role: %v", err)
		}
		grants := []string{`grant select on ` + tables + ` to ` + quoted, `grant execute on function pg_catalog.pg_control_system() to ` + quoted}
		if index == 1 {
			grants = append(grants, `grant insert on `+entries+` to `+quoted, `grant update (sequence,entry_hash,updated_at) on `+head+` to `+quoted,
				`grant usage on schema record_platform_internal to `+quoted,
				`grant execute on function record_platform_internal.digest(bytea,text) to `+quoted)
		}
		for _, grant := range grants {
			if _, err := admin.Exec(ctx, grant); err != nil {
				t.Fatalf("grant restricted recovery ledger privilege: %v", err)
			}
		}
		roleConfig := config.Copy()
		roleConfig.ConnConfig.User, roleConfig.ConnConfig.Password = role, password
		pool, err := pgxpool.NewWithConfig(ctx, roleConfig)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		pools = append(pools, pool)
	}
	return pools[0], pools[1]
}

// append commits a delete_commit through the production primary client. An
// attempt_not_committed row is written only by the deploy absence ceremony,
// which this package cannot run; it is inserted here as that ceremony does,
// a canonical hash-chained row that the verifier independently decodes.
func (ledger *recordsPostgresRecoveryLedger) append(t *testing.T, ctx context.Context, request recorddeletion.LedgerAppendRequest) recorddeletion.DeletionLedgerEntry {
	t.Helper()
	if request.EntryType == recorddeletion.LedgerEntryDeleteCommit {
		entry, err := ledger.primary.AppendDeletionEntry(ctx, request)
		if err != nil {
			t.Fatalf("append recovery delete commit: %v", err)
		}
		return entry
	}
	tx, err := ledger.primaryDB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sequence int64
	var head []byte
	if err := tx.QueryRow(ctx, `select sequence, entry_hash from public.deletion_ledger_head where singleton for update`).Scan(&sequence, &head); err != nil {
		t.Fatal(err)
	}
	canonical := recorddeletion.CanonicalLedgerEntry{
		DeletionLedgerEntry: recorddeletion.DeletionLedgerEntry{Request: request, Sequence: uint64(sequence + 1)},
		LedgerDomain:        ledger.domain, ConfirmedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	copy(canonical.PreviousHash[:], head)
	encoded, err := recorddeletion.EncodeCanonicalLedgerEntry(canonical)
	if err != nil {
		t.Fatalf("encode recovery outcome: %v", err)
	}
	hash := sha256.Sum256(encoded)
	fingerprint, err := request.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into public.deletion_ledger_entries
		(sequence,entry_version,entry_type,deployment_id,project_id,operation_id,actor_id,route,object_kind,object_id,
		 deletion_request_token_commitment,request_fingerprint,reason_code,release_epoch,confirmed_at,previous_hash,canonical_entry,entry_hash)
		values ($1,1,'attempt_not_committed',$2,$3,$4,$5,'record_permanent_delete',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		int64(canonical.Sequence), string(request.DeploymentID), string(request.ProjectID), request.OperationID, request.ActorID,
		request.Object.ObjectKind, request.Object.ObjectID, request.TokenCommitment[:], fingerprint[:], string(request.ReasonCode),
		int64(request.ReleaseEpoch), canonical.ConfirmedAt, canonical.PreviousHash[:], encoded, hash[:]); err != nil {
		t.Fatalf("insert recovery outcome: %v", err)
	}
	if _, err := tx.Exec(ctx, `update public.deletion_ledger_head set sequence=$1, entry_hash=$2, updated_at=clock_timestamp() where singleton`,
		int64(canonical.Sequence), hash[:]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return recorddeletion.DeletionLedgerEntry{Request: request, Sequence: canonical.Sequence, EntryHash: hash}
}

func (ledger *recordsPostgresRecoveryLedger) locator(entry recorddeletion.DeletionLedgerEntry) recorddeletion.LedgerLocator {
	return recorddeletion.LedgerLocator{LedgerDomain: ledger.domain, Sequence: entry.Sequence, EntryHash: entry.EntryHash}
}

// record appends, witnesses and installs one entry in APP exactly as the
// management coordinator does, returning the verified witness proof digest.
func (ledger *recordsPostgresRecoveryLedger) record(t *testing.T, ctx context.Context, request recorddeletion.LedgerAppendRequest) (recorddeletion.DeletionLedgerEntry, [sha256.Size]byte) {
	t.Helper()
	entry := ledger.append(t, ctx, request)
	receipt, err := ledger.witness.ConfirmDeletionEntry(ctx, entry)
	if err != nil {
		t.Fatalf("witness recovery entry: %v", err)
	}
	locator := ledger.locator(entry)
	var proof [sha256.Size]byte
	err = ledger.fence.WithHeldLedgerFence(ctx, func(ctx context.Context) error {
		verified, err := ledger.verifier.Verify(ctx, locator)
		if err != nil {
			return err
		}
		if !verified.Valid() || verified.Locator() != locator || verified.ProofDigest() != receipt.ProofDigest {
			return errRecordsRecoveryFence
		}
		rid, generation, err := ledger.fence.RecoveryBinding()
		if err != nil {
			return err
		}
		payload, err := recorddeletion.EncodeLegacyVerifiedEntry(verified, rid, generation)
		if err != nil {
			return err
		}
		tx, err := ledger.admin.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `select public.record_deletion_verify_legacy_entry($1::bytea)`, payload); err != nil {
			return err
		}
		proof = verified.ProofDigest()
		return tx.Commit(ctx)
	})
	if err != nil {
		t.Fatalf("verify and install recovery entry: %v", err)
	}
	return entry, proof
}

// prior witnesses an earlier, unrelated history entry and returns the cursor
// that APP has already applied up to. It is never installed or replayed.
func (ledger *recordsPostgresRecoveryLedger) prior(t *testing.T, ctx context.Context, deploymentID recordplatform.DeploymentID) recorddeletion.RecoveryReplayCursor {
	t.Helper()
	fingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(bytes.Repeat([]byte{0x41}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var token [32]byte
	token[0] = 0x42
	entry := ledger.append(t, ctx, recorddeletion.LedgerAppendRequest{
		EntryType: recorddeletion.LedgerEntryDeleteCommit, DeploymentID: deploymentID, ProjectID: recordplatform.ProjectIDDefault,
		OperationID: "rpo_pgrecoveryprior", ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:          recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_pgrecoveryprior"},
		TokenCommitment: token, RequestFingerprint: fingerprint, ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
	})
	if _, err := ledger.witness.ConfirmDeletionEntry(ctx, entry); err != nil {
		t.Fatalf("witness prior recovery entry: %v", err)
	}
	return recorddeletion.RecoveryReplayCursor{Sequence: entry.Sequence, EntryHash: entry.EntryHash}
}

func (ledger *recordsPostgresRecoveryLedger) replayRequest(cursor recorddeletion.RecoveryReplayCursor, entry recorddeletion.DeletionLedgerEntry) recorddeletion.RecoveryReplayRequest {
	return recorddeletion.RecoveryReplayRequest{Cursor: cursor, Locator: ledger.locator(entry)}
}

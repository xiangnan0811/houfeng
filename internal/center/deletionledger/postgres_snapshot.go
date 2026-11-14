// Package deletionledger provides clients for the independent deletion ledger
// and full-copy witness databases. Pool credentials and lifecycle belong to the
// trusted management coordinator, not the deletion worker.
package deletionledger

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

const ledgerColumns = `sequence, entry_version, entry_type, deployment_id, project_id, operation_id,
 actor_id, route, object_kind, object_id, origin_identity, authorization_floor,
 authorization_floor_hash, deletion_request_token_commitment, request_fingerprint,
 deletion_contract_version, reason_code, release_epoch, confirmed_at, previous_hash,
 canonical_entry, entry_hash`

type storedRow struct {
	sequence                                                              int64
	version                                                               int16
	entryType, deployment, project, operation, actor, route, kind, object string
	origin, floor, floorHash, token, fingerprint                          []byte
	contract                                                              *int64
	reason                                                                string
	release                                                               *int64
	confirmed                                                             time.Time
	previous, canonical, hash                                             []byte
}

type client struct {
	pool         *pgxpool.Pool
	expected     recorddeletion.LedgerPhysicalIdentity
	ledgerDomain [sha256.Size]byte
	witness      bool
}

type Primary struct{ client client }
type Witness struct {
	client  client
	primary *Primary
}

var _ recorddeletion.DeletionLedger = (*Primary)(nil)
var _ recorddeletion.DeletionEntryWitness = (*Witness)(nil)
var _ recorddeletion.LedgerSnapshotReader = (*Primary)(nil)
var _ recorddeletion.LedgerSnapshotReader = (*Witness)(nil)

// NewPrimary binds a pool to the complete expected physical W1 identity. The
// pool must be created with the project's strict authority connection policy.
func NewPrimary(pool *pgxpool.Pool, expected recorddeletion.LedgerPhysicalIdentity) (*Primary, error) {
	if pool == nil || expected.Validate() != nil || expected.Kind != "deletion_ledger" {
		return nil, fmt.Errorf("%w: primary configuration", recorddeletion.ErrLedgerContinuity)
	}
	domain, err := recorddeletion.LedgerDomainDigest(expected.ID)
	if err != nil {
		return nil, err
	}
	return &Primary{client: client{pool: pool, expected: expected, ledgerDomain: domain}}, nil
}

// NewWitness binds a full-copy W2 pool to its own physical identity and the
// primary from which exact rows are read. It does not own either pool.
func NewWitness(pool *pgxpool.Pool, primary *Primary, expected recorddeletion.LedgerPhysicalIdentity) (*Witness, error) {
	if pool == nil || primary == nil || primary.client.pool == nil || expected.Validate() != nil || expected.Kind != "deletion_witness" ||
		expected.ID == primary.client.expected.ID || expected.SystemIdentifier == primary.client.expected.SystemIdentifier {
		return nil, fmt.Errorf("%w: witness configuration", recorddeletion.ErrLedgerContinuity)
	}
	return &Witness{client: client{pool: pool, expected: expected, ledgerDomain: primary.client.ledgerDomain, witness: true}, primary: primary}, nil
}

func (primary *Primary) ReadLedgerSnapshot(ctx context.Context) (recorddeletion.LedgerSnapshot, error) {
	if primary == nil {
		return recorddeletion.LedgerSnapshot{}, recorddeletion.ErrLedgerContinuity
	}
	return primary.client.readSnapshot(ctx)
}

func (witness *Witness) ReadLedgerSnapshot(ctx context.Context) (recorddeletion.LedgerSnapshot, error) {
	if witness == nil {
		return recorddeletion.LedgerSnapshot{}, recorddeletion.ErrLedgerContinuity
	}
	return witness.client.readSnapshot(ctx)
}

func (client *client) readSnapshot(ctx context.Context) (recorddeletion.LedgerSnapshot, error) {
	if ctx == nil || client == nil || client.pool == nil {
		return recorddeletion.LedgerSnapshot{}, recorddeletion.ErrLedgerContinuity
	}
	tx, err := client.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, failure("begin snapshot", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, _, err := client.snapshotTx(ctx, tx, false)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.LedgerSnapshot{}, failure("commit snapshot", err)
	}
	return snapshot, nil
}

func (client *client) snapshotTx(ctx context.Context, tx pgx.Tx, lock bool) (recorddeletion.LedgerSnapshot, []storedRow, error) {
	identity, err := client.readIdentity(ctx, tx)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	var query string
	if client.witness {
		query = `select sequence, entry_hash from public.deletion_witness_head where singleton`
	} else {
		query = `select sequence, entry_hash from public.deletion_ledger_head where singleton`
	}
	if lock {
		query += ` for update`
	}
	var sequence int64
	var hash []byte
	if err := tx.QueryRow(ctx, query).Scan(&sequence, &hash); err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, failure("read head", err)
	}
	if sequence < 0 || len(hash) != sha256.Size {
		return recorddeletion.LedgerSnapshot{}, nil, fmt.Errorf("%w: head", recorddeletion.ErrLedgerContinuity)
	}
	snapshot := recorddeletion.LedgerSnapshot{Identity: identity}
	snapshot.Head.Sequence = uint64(sequence)
	copy(snapshot.Head.Hash[:], hash)
	var rowQuery string
	if client.witness {
		rowQuery = `select ` + ledgerColumns + ` from public.deletion_witness_entries order by sequence`
	} else {
		rowQuery = `select ` + ledgerColumns + ` from public.deletion_ledger_entries order by sequence`
	}
	rows, err := tx.Query(ctx, rowQuery)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, failure("read chain", err)
	}
	var stored []storedRow
	for rows.Next() {
		var row storedRow
		if err := rows.Scan(&row.sequence, &row.version, &row.entryType, &row.deployment, &row.project, &row.operation,
			&row.actor, &row.route, &row.kind, &row.object, &row.origin, &row.floor, &row.floorHash, &row.token,
			&row.fingerprint, &row.contract, &row.reason, &row.release, &row.confirmed, &row.previous, &row.canonical, &row.hash); err != nil {
			rows.Close()
			return recorddeletion.LedgerSnapshot{}, nil, failure("scan chain", err)
		}
		projection, err := client.projectRow(row)
		if err != nil {
			rows.Close()
			return recorddeletion.LedgerSnapshot{}, nil, err
		}
		stored = append(stored, row)
		snapshot.Entries = append(snapshot.Entries, projection)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return recorddeletion.LedgerSnapshot{}, nil, failure("read chain", err)
	}
	rows.Close()
	if !client.witness {
		checkpoints, err := tx.Query(ctx, `select sequence, entry_hash from public.deletion_ledger_checkpoints order by sequence`)
		if err != nil {
			return recorddeletion.LedgerSnapshot{}, nil, failure("read checkpoints", err)
		}
		for checkpoints.Next() {
			var seq int64
			var digest []byte
			if err := checkpoints.Scan(&seq, &digest); err != nil {
				checkpoints.Close()
				return recorddeletion.LedgerSnapshot{}, nil, failure("scan checkpoints", err)
			}
			if seq < 0 || len(digest) != sha256.Size {
				checkpoints.Close()
				return recorddeletion.LedgerSnapshot{}, nil, fmt.Errorf("%w: checkpoint", recorddeletion.ErrLedgerContinuity)
			}
			var head recorddeletion.LedgerHead
			head.Sequence = uint64(seq)
			copy(head.Hash[:], digest)
			snapshot.Checkpoints = append(snapshot.Checkpoints, head)
		}
		if err := checkpoints.Err(); err != nil {
			checkpoints.Close()
			return recorddeletion.LedgerSnapshot{}, nil, failure("read checkpoints", err)
		}
		checkpoints.Close()
	}
	if err := recorddeletion.ValidateLedgerSnapshot(snapshot, client.ledgerDomain); err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	return snapshot, stored, nil
}

func (client *client) readIdentity(ctx context.Context, tx pgx.Tx) (recorddeletion.LedgerPhysicalIdentity, error) {
	rows, err := tx.Query(ctx, `select domain_id, domain_kind, identity_epoch, identity_mode,
 coalesce(postgres_system_identifier,''), database_oid, database_name from public.record_platform_domain_identity`)
	if err != nil {
		return recorddeletion.LedgerPhysicalIdentity{}, failure("read domain identity", err)
	}
	var actual recorddeletion.LedgerPhysicalIdentity
	var mode string
	var epoch int64
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			rows.Close()
			return recorddeletion.LedgerPhysicalIdentity{}, fmt.Errorf("%w: multiple identities", recorddeletion.ErrLedgerContinuity)
		}
		if err := rows.Scan(&actual.ID, &actual.Kind, &epoch, &mode, &actual.SystemIdentifier, &actual.DatabaseOID, &actual.DatabaseName); err != nil {
			rows.Close()
			return recorddeletion.LedgerPhysicalIdentity{}, failure("scan domain identity", err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return recorddeletion.LedgerPhysicalIdentity{}, failure("read domain identity", err)
	}
	rows.Close()
	if count != 1 || epoch <= 0 || mode != "postgres_system" {
		return recorddeletion.LedgerPhysicalIdentity{}, fmt.Errorf("%w: domain identity", recorddeletion.ErrLedgerContinuity)
	}
	actual.Epoch = uint64(epoch)
	var system string
	var oid uint32
	var name string
	if err := tx.QueryRow(ctx, `select s.system_identifier::text, d.oid, d.datname
 from pg_catalog.pg_control_system() s join pg_catalog.pg_database d on d.datname=current_database()`).Scan(&system, &oid, &name); err != nil {
		return recorddeletion.LedgerPhysicalIdentity{}, failure("read live physical identity", err)
	}
	if actual != client.expected || actual.Validate() != nil || system != actual.SystemIdentifier || oid != actual.DatabaseOID || name != actual.DatabaseName {
		return recorddeletion.LedgerPhysicalIdentity{}, fmt.Errorf("%w: physical identity mismatch", recorddeletion.ErrLedgerContinuity)
	}
	return actual, nil
}

func (client *client) projectRow(row storedRow) (recorddeletion.LedgerChainEntry, error) {
	if row.sequence <= 0 || len(row.previous) != sha256.Size || len(row.hash) != sha256.Size || len(row.canonical) == 0 {
		return recorddeletion.LedgerChainEntry{}, fmt.Errorf("%w: row shape", recorddeletion.ErrInvalidLedgerEvidence)
	}
	projection := recorddeletion.LedgerChainEntry{Sequence: uint64(row.sequence), Canonical: append([]byte(nil), row.canonical...), Route: row.route}
	copy(projection.PreviousHash[:], row.previous)
	copy(projection.EntryHash[:], row.hash)
	if claimsSourceCodec(row.canonical) {
		if _, err := client.sourceRow(row); err != nil {
			return recorddeletion.LedgerChainEntry{}, err
		}
	}
	if row.route != recorddeletion.LedgerRecordRoute {
		return projection, nil
	}
	if row.version != 1 || row.origin != nil || row.floor != nil || row.floorHash != nil || len(row.token) != sha256.Size || len(row.fingerprint) != sha256.Size {
		return recorddeletion.LedgerChainEntry{}, fmt.Errorf("%w: record row shape", recorddeletion.ErrInvalidLedgerEvidence)
	}
	var contract, release uint64
	if row.contract != nil {
		if *row.contract <= 0 || *row.contract > math.MaxInt64 {
			return recorddeletion.LedgerChainEntry{}, recorddeletion.ErrInvalidLedgerEvidence
		}
		contract = uint64(*row.contract)
	}
	if row.release != nil {
		if *row.release <= 0 || *row.release > math.MaxInt64 {
			return recorddeletion.LedgerChainEntry{}, recorddeletion.ErrInvalidLedgerEvidence
		}
		release = uint64(*row.release)
	}
	fingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(row.fingerprint)
	if err != nil {
		return recorddeletion.LedgerChainEntry{}, recorddeletion.ErrInvalidLedgerEvidence
	}
	request := recorddeletion.LedgerAppendRequest{EntryType: recorddeletion.LedgerEntryType(row.entryType),
		DeploymentID: recordplatform.DeploymentID(row.deployment), ProjectID: recordplatform.ProjectID(row.project),
		OperationID: row.operation, ActorID: row.actor, Object: recordplatform.ObjectRef{ProjectID: row.project, ObjectKind: row.kind, ObjectID: row.object},
		RequestFingerprint: fingerprint, ReasonCode: recorddeletion.DeletionReasonCode(row.reason), DeletionContractVersion: contract, ReleaseEpoch: release}
	copy(request.TokenCommitment[:], row.token)
	entry := recorddeletion.CanonicalLedgerEntry{DeletionLedgerEntry: recorddeletion.DeletionLedgerEntry{Request: request, Sequence: uint64(row.sequence), EntryHash: projection.EntryHash},
		LedgerDomain: client.ledgerDomain, PreviousHash: projection.PreviousHash, ConfirmedAt: row.confirmed, Canonical: append([]byte(nil), row.canonical...)}
	if entry.Validate() != nil {
		return recorddeletion.LedgerChainEntry{}, fmt.Errorf("%w: record projection", recorddeletion.ErrInvalidLedgerEvidence)
	}
	projection.Record = &entry
	return projection, nil
}

// Connection errors may contain DSN material; retain the cause for errors.Is
// without rendering credentials into logs.
type clientError struct {
	operation string
	cause     error
}

func (err *clientError) Error() string { return "deletion ledger " + err.operation + " failed" }
func (err *clientError) Unwrap() error { return err.cause }
func failure(operation string, cause error) error {
	return &clientError{operation: operation, cause: cause}
}

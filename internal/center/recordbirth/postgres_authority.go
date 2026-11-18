package recordbirth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresAuthority uses a dedicated direct-login W2 principal. The pool remains
// the caller's responsibility and no receipt key is read into this process.
type PostgresAuthority struct {
	pool        *pgxpool.Pool
	application *pgxpool.Pool
	expected    *postgresPhysicalIdentity
}

var _ Authority = (*PostgresAuthority)(nil)

func NewPostgresAuthority(pool *pgxpool.Pool) (*PostgresAuthority, error) {
	if pool == nil {
		return nil, fmt.Errorf("%w: birth authority pool is required", ErrInvalid)
	}
	return &PostgresAuthority{pool: pool}, nil
}

type postgresBirthBinding struct {
	AuthorityID   string `json:"authority_id"`
	LineageID     string `json:"lineage_id"`
	DeploymentID  string `json:"deployment_id"`
	ProjectID     string `json:"project_id"`
	Generation    uint64 `json:"generation"`
	KeyID         string `json:"key_id"`
	CutoverDigest string `json:"cutover_digest"`
}

func postgresBinding(binding Binding) postgresBirthBinding {
	return postgresBirthBinding{
		AuthorityID: binding.AuthorityID, LineageID: binding.LineageID,
		DeploymentID: binding.DeploymentID, ProjectID: binding.ProjectID,
		Generation: binding.Generation, KeyID: binding.KeyID,
		CutoverDigest: hex.EncodeToString(binding.CutoverDigest[:]),
	}
}

type postgresBirthRequest struct {
	postgresBirthBinding
	ActorID            string  `json:"actor_id"`
	ActorScopeDigest   string  `json:"actor_scope_digest"`
	Purpose            Purpose `json:"purpose"`
	IdempotencyKey     string  `json:"idempotency_key"`
	RequestFingerprint string  `json:"request_fingerprint"`
}

func postgresRequest(binding Binding, request Request) postgresBirthRequest {
	return postgresBirthRequest{
		postgresBirthBinding: postgresBinding(binding), ActorID: request.ActorID,
		ActorScopeDigest: hex.EncodeToString(request.ActorScopeDigest[:]), Purpose: request.Purpose,
		IdempotencyKey: request.IdempotencyKey, RequestFingerprint: hex.EncodeToString(request.RequestFingerprint[:]),
	}
}

func (authority *PostgresAuthority) Reserve(ctx context.Context, binding Binding, request Request, checkpointProof []byte) ([]byte, error) {
	if binding.Validate() != nil || request.Validate() != nil || binding.ProjectID != request.ProjectID {
		return nil, ErrInvalid
	}
	proof, err := DecodeCheckpointProof(checkpointProof)
	if err != nil || proof.Binding != binding || proof.Checkpoint.Request != request {
		return nil, ErrInvalid
	}
	command := struct {
		postgresBirthRequest
		CheckpointProof string `json:"checkpoint_proof"`
	}{postgresRequest(binding, request), hex.EncodeToString(checkpointProof)}
	var receipt []byte
	if err := authority.call(ctx, binding, `select public.record_birth_reserve($1)`, command, &receipt); err != nil {
		return nil, err
	}
	event, err := DecodeReceipt(receipt)
	if err != nil || event.Kind != EventReserved || event.Request != request || !sameBirthLineage(event.Binding, binding) {
		return nil, fmt.Errorf("%w: invalid reserved event projection", ErrPending)
	}
	return receipt, nil
}

func (authority *PostgresAuthority) Prepare(ctx context.Context, binding Binding, reservationID string, initial [32]byte) ([]byte, error) {
	return authority.transition(ctx, binding, reservationID, initial, EventPrepared,
		`select public.record_birth_prepare($1)`)
}

func (authority *PostgresAuthority) Confirm(ctx context.Context, binding Binding, reservationID string, initial [32]byte) ([]byte, error) {
	return authority.transition(ctx, binding, reservationID, initial, EventCreated,
		`select public.record_birth_confirm($1)`)
}

func (authority *PostgresAuthority) transition(ctx context.Context, binding Binding, reservationID string, initial [32]byte, kind EventKind, query string) ([]byte, error) {
	if binding.Validate() != nil || !hexIdentity(reservationID, "rba_", 32) || initial == ([32]byte{}) {
		return nil, ErrInvalid
	}
	command := struct {
		postgresBirthBinding
		ReservationID     string `json:"reservation_id"`
		InitialCommitment string `json:"initial_commitment"`
	}{postgresBinding(binding), reservationID, hex.EncodeToString(initial[:])}
	var receipt []byte
	if err := authority.call(ctx, binding, query, command, &receipt); err != nil {
		return nil, err
	}
	event, err := DecodeReceipt(receipt)
	if err != nil || event.Kind != kind || event.ReservationID != reservationID ||
		event.InitialCommitment != initial || !sameBirthLineage(event.Binding, binding) {
		return nil, fmt.Errorf("%w: invalid birth transition projection", ErrPending)
	}
	return receipt, nil
}

func (authority *PostgresAuthority) Observe(ctx context.Context, binding Binding, recordID string) (Observation, error) {
	if ctx == nil {
		return Observation{}, ErrInvalid
	}
	if binding.Validate() != nil || (recordID != "" && (!hexIdentity(recordID, "rec2_", 32) || recordID[5] < '8')) {
		return Observation{}, ErrInvalid
	}
	if authority == nil || authority.pool == nil {
		return Observation{}, ErrUnavailable
	}
	if err := authority.checkApplicationIdentity(ctx); err != nil {
		return Observation{}, err
	}
	return authority.observe(ctx, authority.pool, binding, recordID)
}

type postgresBirthQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// observe uses the supplied connection or transaction. When invoked inside a
// mutation the source singleton lock taken by SQL remains held until commit.
func (authority *PostgresAuthority) observe(ctx context.Context, query postgresBirthQuery, binding Binding, recordID string) (Observation, error) {
	command := struct {
		postgresBirthBinding
		RecordID string `json:"record_id"`
	}{postgresBinding(binding), recordID}
	data, err := json.Marshal(command)
	if err != nil {
		return Observation{}, fmt.Errorf("%w: encode observation: %v", ErrInvalid, err)
	}
	var result Observation
	var actual postgresPhysicalIdentity
	var generation, sequence, identityEpoch int64
	var cutover, head []byte
	err = query.QueryRow(ctx, `select authority_id,lineage_id,deployment_id,project_id,generation,key_id,
		cutover_digest,reserved,prepared,created,head_sequence,head_hash,
		system_identifier,database_oid,database_name,identity_epoch from public.record_birth_observe($1)`, data).Scan(
		&result.Binding.AuthorityID, &result.Binding.LineageID, &result.Binding.DeploymentID,
		&result.Binding.ProjectID, &generation, &result.Binding.KeyID, &cutover,
		&result.Reserved, &result.Prepared, &result.Created, &sequence, &head,
		&actual.SystemIdentifier, &actual.DatabaseOID, &actual.DatabaseName, &identityEpoch)
	if err != nil {
		return Observation{}, postgresBirthError(err)
	}
	actual.ID = result.Binding.AuthorityID
	if generation <= 0 || sequence < 0 || identityEpoch <= 0 || len(cutover) != 32 || len(head) != 32 {
		return Observation{}, fmt.Errorf("%w: invalid birth observation", ErrUnavailable)
	}
	actual.Epoch = uint64(identityEpoch)
	if authority.expected != nil && actual != *authority.expected {
		return Observation{}, fmt.Errorf("%w: birth authority physical identity mismatch", ErrUnavailable)
	}
	result.Binding.Generation = uint64(generation)
	result.HeadSequence = uint64(sequence)
	copy(result.Binding.CutoverDigest[:], cutover)
	copy(result.HeadHash[:], head)
	if result.Binding != binding || (result.HeadSequence == 0) != (result.HeadHash == ([32]byte{})) {
		return Observation{}, fmt.Errorf("%w: birth observation binding or head mismatch", ErrUnavailable)
	}
	if err := validateBirthObservation(result, recordID); err != nil {
		return Observation{}, err
	}
	return result, nil
}

func validateBirthObservation(observation Observation, recordID string) error {
	return validateBirthEvents(observation, recordID, nil, true)
}

// Request observations have no global event head, but must match the complete
// request and current lineage while preserving the existing event ordering.
func validateBirthEvents(observation Observation, recordID string, request *Request, checkHead bool) error {
	var reserved, prepared Event
	for index, receipt := range [][]byte{observation.Reserved, observation.Prepared, observation.Created} {
		if len(receipt) == 0 {
			continue
		}
		event, err := DecodeReceipt(receipt)
		if err != nil || event.RecordID != recordID || !sameBirthLineage(event.Binding, observation.Binding) ||
			(checkHead && event.Sequence > observation.HeadSequence) ||
			(request != nil && (event.Request != *request || event.Binding.DeploymentID != observation.Binding.DeploymentID ||
				event.Binding.Generation > observation.Binding.Generation)) {
			return fmt.Errorf("%w: invalid observed birth event", ErrUnavailable)
		}
		if checkHead && event.Sequence == observation.HeadSequence && sha256.Sum256(receipt) != observation.HeadHash {
			return fmt.Errorf("%w: observed birth head mismatch", ErrUnavailable)
		}
		switch index {
		case 0:
			if event.Kind != EventReserved {
				return ErrUnavailable
			}
			reserved = event
		case 1:
			if event.Kind != EventPrepared || reserved.Kind != EventReserved ||
				event.ReservationID != reserved.ReservationID || event.Request != reserved.Request || event.Sequence <= reserved.Sequence {
				return ErrUnavailable
			}
			if event.Sequence == reserved.Sequence+1 && event.PreviousHash != sha256.Sum256(observation.Reserved) {
				return ErrUnavailable
			}
			prepared = event
		case 2:
			if event.Kind != EventCreated || prepared.Kind != EventPrepared || event.ReservationID != prepared.ReservationID ||
				event.Request != prepared.Request || event.InitialCommitment != prepared.InitialCommitment || event.Sequence <= prepared.Sequence {
				return ErrUnavailable
			}
			if event.Sequence == prepared.Sequence+1 && event.PreviousHash != sha256.Sum256(observation.Prepared) {
				return ErrUnavailable
			}
		}
	}
	if recordID == "" && (len(observation.Reserved) != 0 || len(observation.Prepared) != 0 || len(observation.Created) != 0) {
		return ErrUnavailable
	}
	return nil
}

func sameBirthLineage(left, right Binding) bool {
	return left.AuthorityID == right.AuthorityID && left.LineageID == right.LineageID && left.ProjectID == right.ProjectID
}

func (authority *PostgresAuthority) call(ctx context.Context, binding Binding, query string, command any, receipt *[]byte) error {
	if ctx == nil {
		return ErrInvalid
	}
	if authority == nil || authority.pool == nil {
		return ErrUnavailable
	}
	data, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("%w: encode birth command: %v", ErrInvalid, err)
	}
	if authority.expected == nil {
		// Fixture and library callers retain the original single-statement path.
		if err := authority.pool.QueryRow(ctx, query, data).Scan(receipt); err != nil {
			return postgresBirthError(err)
		}
		return nil
	}
	if err := authority.checkApplicationIdentity(ctx); err != nil {
		return err
	}
	tx, err := authority.pool.Begin(ctx)
	if err != nil {
		return postgresBirthError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := authority.observe(ctx, tx, binding, ""); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, query, data).Scan(receipt); err != nil {
		return postgresBirthError(err)
	}
	// A failed commit may have succeeded on W2. Never issue a replacement
	// request or synthesize a new reservation to resolve that uncertainty.
	if err := tx.Commit(ctx); err != nil {
		return postgresBirthError(err)
	}
	return nil
}

// Check the configured witness against the admitted APP projection, without
// opening a source-reader connection or trusting a previously cached result.
func (authority *PostgresAuthority) checkApplicationIdentity(ctx context.Context) error {
	if authority.expected == nil {
		return nil
	}
	if authority.application == nil {
		return ErrUnavailable
	}
	identity := authority.expected
	var matches bool
	err := authority.application.QueryRow(ctx, `select witness_domain_id=$1 and witness_system_identifier=$2
		and witness_database_oid=$3 and witness_database_name=$4 and witness_identity_epoch=$5
		from public.source_maintenance_status() where singleton`, identity.ID, identity.SystemIdentifier,
		identity.DatabaseOID, identity.DatabaseName, identity.Epoch).Scan(&matches)
	if err != nil || !matches {
		return fmt.Errorf("%w: birth authority does not match application admission", ErrUnavailable)
	}
	return nil
}

func postgresBirthError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Severity == "FATAL" || pgErr.Severity == "PANIC" || strings.HasPrefix(pgErr.Code, "08") {
			return fmt.Errorf("%w: birth authority outcome could not be confirmed", ErrPending)
		}
		if pgErr.Code == "40001" && pgErr.Message == "checkpoint anchor changed" {
			return ErrCheckpointChanged
		}
		switch pgErr.Code {
		case "22023":
			return fmt.Errorf("%w: birth command rejected", ErrInvalid)
		case "23505":
			return fmt.Errorf("%w: birth request binding conflict", ErrConflict)
		default:
			return fmt.Errorf("%w: birth authority rejected command (%s)", ErrUnavailable, pgErr.Code)
		}
	}
	return fmt.Errorf("%w: birth authority outcome could not be confirmed", ErrPending)
}

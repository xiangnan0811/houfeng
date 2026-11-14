package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
)

// PostgresRecordBirthRepository uses the ordinary admitted APP transaction
// path. Witness calls belong to the coordinator and never run inside it.
type PostgresRecordBirthRepository struct {
	platform   *PostgresRecordPlatformRepository
	continuity recordbirth.ContinuitySource
}

var _ recordbirth.Application = (*PostgresRecordBirthRepository)(nil)

func NewPostgresRecordBirthRepository(pool *pgxpool.Pool, gate AdmissionGate, continuity recordbirth.ContinuitySource) *PostgresRecordBirthRepository {
	return &PostgresRecordBirthRepository{platform: NewPostgresRecordPlatformRepository(pool, gate), continuity: continuity}
}

type birthRequestCommand struct {
	ProjectID          string              `json:"project_id"`
	ActorID            string              `json:"actor_id"`
	ActorScopeDigest   string              `json:"actor_scope_digest"`
	Purpose            recordbirth.Purpose `json:"purpose"`
	IdempotencyKey     string              `json:"idempotency_key"`
	RequestFingerprint string              `json:"request_fingerprint"`
}

func birthRequest(request recordbirth.Request) birthRequestCommand {
	return birthRequestCommand{
		ProjectID: request.ProjectID, ActorID: request.ActorID,
		ActorScopeDigest: hex.EncodeToString(request.ActorScopeDigest[:]), Purpose: request.Purpose,
		IdempotencyKey: request.IdempotencyKey, RequestFingerprint: hex.EncodeToString(request.RequestFingerprint[:]),
	}
}

type birthBindingCommand struct {
	AuthorityID   string `json:"authority_id"`
	LineageID     string `json:"lineage_id"`
	DeploymentID  string `json:"deployment_id"`
	Generation    uint64 `json:"generation"`
	KeyID         string `json:"key_id"`
	CutoverDigest string `json:"cutover_digest"`
}

func birthBinding(binding recordbirth.Binding) birthBindingCommand {
	value := birthBindingCommand{
		AuthorityID: binding.AuthorityID, LineageID: binding.LineageID,
		DeploymentID: binding.DeploymentID, Generation: binding.Generation, KeyID: binding.KeyID,
	}
	if binding != (recordbirth.Binding{}) {
		value.CutoverDigest = hex.EncodeToString(binding.CutoverDigest[:])
	}
	return value
}

type birthCreationCommand struct {
	ReservationID              string `json:"reservation_id"`
	RecordID                   string `json:"record_id"`
	RevisionID                 string `json:"revision_id"`
	CanonicalHash              string `json:"canonical_hash"`
	CreationRequestFingerprint string `json:"creation_request_fingerprint"`
}

func birthCreation(creation recordbirth.InitialCreation) birthCreationCommand {
	return birthCreationCommand{
		ReservationID: creation.ReservationID, RecordID: creation.RecordID, RevisionID: creation.RevisionID,
		CanonicalHash:              hex.EncodeToString(creation.CanonicalHash[:]),
		CreationRequestFingerprint: hex.EncodeToString(creation.RequestFingerprint[:]),
	}
}

type birthAllocationProjection struct {
	AllocationID string            `json:"allocation_id"`
	Domain       string            `json:"domain"`
	State        recordbirth.State `json:"state"`
	birthRequestCommand
	birthBindingCommand
	ReservationID              string `json:"reservation_id"`
	RecordID                   string `json:"record_id"`
	RevisionID                 string `json:"revision_id"`
	CanonicalHash              string `json:"canonical_hash"`
	CreationRequestFingerprint string `json:"creation_request_fingerprint"`
	ReserveReceipt             string `json:"reserve_receipt"`
	PrepareReceipt             string `json:"prepare_receipt"`
	CreatedReceipt             string `json:"created_receipt"`
}

func decodeBirthJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: malformed APP birth projection: %v", recordbirth.ErrUnavailable, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: extra APP birth projection data", recordbirth.ErrUnavailable)
	}
	return nil
}

func decodeBirthDigest(raw string) ([32]byte, error) {
	var digest [32]byte
	if len(raw) != 2*len(digest) || !birthLowerHex(raw) {
		return digest, fmt.Errorf("%w: malformed APP birth digest", recordbirth.ErrUnavailable)
	}
	if _, err := hex.Decode(digest[:], []byte(raw)); err != nil {
		return digest, fmt.Errorf("%w: malformed APP birth digest", recordbirth.ErrUnavailable)
	}
	return digest, nil
}

func decodeBirthBytes(raw string) ([]byte, error) {
	if len(raw) > 8192 || len(raw)%2 != 0 || !birthLowerHex(raw) {
		return nil, fmt.Errorf("%w: malformed APP birth receipt", recordbirth.ErrUnavailable)
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed APP birth receipt", recordbirth.ErrUnavailable)
	}
	return decoded, nil
}

func birthLowerHex(raw string) bool {
	for index := range raw {
		ch := raw[index]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func (projection birthAllocationProjection) allocation() (recordbirth.Allocation, error) {
	var result recordbirth.Allocation
	result.AllocationID, result.State = projection.AllocationID, projection.State
	switch projection.Domain {
	case "ordinary":
		result.Domain = recordidentity.Ordinary
	case "certifiable":
		result.Domain = recordidentity.Certifiable
	default:
		return result, recordbirth.ErrUnavailable
	}
	var err error
	result.Request = recordbirth.Request{
		ProjectID: projection.ProjectID, ActorID: projection.ActorID, Purpose: projection.Purpose,
		IdempotencyKey: projection.IdempotencyKey,
	}
	if result.Request.ActorScopeDigest, err = decodeBirthDigest(projection.ActorScopeDigest); err != nil {
		return result, err
	}
	if result.Request.RequestFingerprint, err = decodeBirthDigest(projection.RequestFingerprint); err != nil {
		return result, err
	}
	if result.Domain == recordidentity.Certifiable {
		result.Binding = recordbirth.Binding{
			AuthorityID: projection.AuthorityID, LineageID: projection.LineageID, DeploymentID: projection.DeploymentID,
			ProjectID: projection.ProjectID, Generation: projection.Generation, KeyID: projection.KeyID,
		}
		if result.Binding.CutoverDigest, err = decodeBirthDigest(projection.CutoverDigest); err != nil {
			return result, err
		}
	} else if projection.AuthorityID != "" || projection.LineageID != "" || projection.DeploymentID != "" ||
		projection.Generation != 0 || projection.KeyID != "" || projection.CutoverDigest != "" {
		return result, recordbirth.ErrUnavailable
	}
	result.ReservationID, result.RecordID = projection.ReservationID, projection.RecordID
	if projection.RevisionID != "" {
		result.Creation = recordbirth.InitialCreation{
			ReservationID: projection.ReservationID, RecordID: projection.RecordID,
			RevisionID: projection.RevisionID, AuthorID: projection.ActorID,
		}
		if result.Creation.CanonicalHash, err = decodeBirthDigest(projection.CanonicalHash); err != nil {
			return result, err
		}
		if result.Creation.RequestFingerprint, err = decodeBirthDigest(projection.CreationRequestFingerprint); err != nil {
			return result, err
		}
	} else if projection.CanonicalHash != "" || projection.CreationRequestFingerprint != "" {
		return result, recordbirth.ErrUnavailable
	}
	if result.ReserveReceipt, err = decodeBirthBytes(projection.ReserveReceipt); err != nil {
		return result, err
	}
	if result.PrepareReceipt, err = decodeBirthBytes(projection.PrepareReceipt); err != nil {
		return result, err
	}
	if result.CreatedReceipt, err = decodeBirthBytes(projection.CreatedReceipt); err != nil {
		return result, err
	}
	if result.Validate() != nil {
		return recordbirth.Allocation{}, fmt.Errorf("%w: inconsistent APP birth projection", recordbirth.ErrUnavailable)
	}
	return result, nil
}

func (repository *PostgresRecordBirthRepository) query(ctx context.Context, statement string, command any) ([]byte, error) {
	return repository.queryWithOptions(ctx, statement, command, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
}

func (repository *PostgresRecordBirthRepository) queryWithOptions(ctx context.Context, statement string, command any, options pgx.TxOptions) ([]byte, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return nil, recordbirth.ErrInvalid
	}
	var encoded []byte
	var err error
	if command != nil {
		encoded, err = json.Marshal(command)
		if err != nil {
			return nil, fmt.Errorf("%w: encode birth command: %v", recordbirth.ErrInvalid, err)
		}
	}
	var raw []byte
	err = repository.platform.runRecordPlatformTransaction(ctx, options,
		func(ctx context.Context, tx *RecordPlatformTransaction) error {
			if command == nil {
				return tx.tx.QueryRow(ctx, statement).Scan(&raw)
			}
			return tx.tx.QueryRow(ctx, statement, encoded).Scan(&raw)
		})
	if err != nil {
		return nil, appBirthError(err)
	}
	return raw, nil
}

func (repository *PostgresRecordBirthRepository) allocation(ctx context.Context, statement string, command any) (recordbirth.Allocation, bool, error) {
	raw, err := repository.query(ctx, statement, command)
	if err != nil {
		return recordbirth.Allocation{}, false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return recordbirth.Allocation{}, false, nil
	}
	var projection birthAllocationProjection
	if err := decodeBirthJSON(raw, &projection); err != nil {
		return recordbirth.Allocation{}, false, err
	}
	allocation, err := projection.allocation()
	return allocation, err == nil, err
}

func (repository *PostgresRecordBirthRepository) FindAllocation(ctx context.Context, request recordbirth.Request) (recordbirth.Allocation, bool, error) {
	if request.Validate() != nil {
		return recordbirth.Allocation{}, false, recordbirth.ErrInvalid
	}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_allocation_find($1)`, birthRequest(request))
	if err == nil && found && allocation.Request != request {
		return recordbirth.Allocation{}, false, recordbirth.ErrUnavailable
	}
	return allocation, found, err
}

// GetAllocation checks current actor identity and APP admission. A completed
// record still needs its normal current read authorization and read fence.
func (repository *PostgresRecordBirthRepository) GetAllocation(ctx context.Context, actor recordauth.ActorScope, allocationID string) (recordbirth.Allocation, error) {
	actor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	command := struct {
		ProjectID    string `json:"project_id"`
		ActorID      string `json:"actor_id"`
		AllocationID string `json:"allocation_id"`
	}{string(actor.ProjectID), actor.UserID, allocationID}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_allocation_get($1)`, command)
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if !found || allocation.AllocationID != allocationID || allocation.Request.ActorID != actor.UserID ||
		allocation.Request.ProjectID != string(actor.ProjectID) {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	return allocation, nil
}

// BindPublication fixes transport replay identity before a draft or intent is
// consumed. An exact existing binding replays; a new one needs the allocation's
// original actor-scope digest.
func (repository *PostgresRecordBirthRepository) BindPublication(ctx context.Context, actor recordauth.ActorScope, allocationID, key string, digest [32]byte) (recordbirth.Allocation, error) {
	actor, err := recordauth.NormalizeActorScope(actor)
	if ctx == nil || repository == nil || repository.platform == nil || err != nil || !validBirthPublicationKey(actor, key) || digest == ([32]byte{}) {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	var continuityProof []byte
	if repository.continuity != nil {
		continuityProof, err = repository.continuity.ReadContinuity(ctx)
		if err != nil {
			return recordbirth.Allocation{}, err
		}
	}
	scope := actor.CanonicalHash()
	command := struct {
		AllocationID      string `json:"allocation_id"`
		ActorID           string `json:"actor_id"`
		ActorScopeDigest  string `json:"actor_scope_digest"`
		PublicationKey    string `json:"publication_key"`
		PublicationDigest string `json:"publication_digest"`
		ContinuityProof   string `json:"continuity_proof"`
	}{allocationID, actor.UserID, hex.EncodeToString(scope[:]), key, hex.EncodeToString(digest[:]), hex.EncodeToString(continuityProof)}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_publication_bind($1)`, command)
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if !found || allocation.AllocationID != allocationID || allocation.Request.ActorID != actor.UserID ||
		allocation.Request.ProjectID != string(actor.ProjectID) {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	allocation.ContinuityProof = continuityProof
	return allocation, nil
}

// LookupPublication checks identity and the exact submitted transport digest;
// the caller must authorize a current record read before returning its content.
func (repository *PostgresRecordBirthRepository) LookupPublication(ctx context.Context, actor recordauth.ActorScope, key string, digest [32]byte) (recordbirth.Allocation, bool, error) {
	actor, err := recordauth.NormalizeActorScope(actor)
	if err != nil || !validBirthPublicationKey(actor, key) || digest == ([32]byte{}) {
		return recordbirth.Allocation{}, false, recordbirth.ErrInvalid
	}
	command := struct {
		ProjectID         string `json:"project_id"`
		ActorID           string `json:"actor_id"`
		PublicationKey    string `json:"publication_key"`
		PublicationDigest string `json:"publication_digest"`
	}{string(actor.ProjectID), actor.UserID, key, hex.EncodeToString(digest[:])}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_publication_lookup($1)`, command)
	if err != nil || !found {
		return allocation, found, err
	}
	if allocation.Request.ActorID != actor.UserID || allocation.Request.ProjectID != string(actor.ProjectID) {
		return recordbirth.Allocation{}, false, recordbirth.ErrUnavailable
	}
	return allocation, true, nil
}

func validBirthPublicationKey(actor recordauth.ActorScope, key string) bool {
	return (recordplatform.IdempotencyKey{
		ProjectID: recordplatform.ProjectID(actor.ProjectID), OperationKind: recordplatform.OperationKindRecordCreate, Key: key,
	}).Validate() == nil
}

func (repository *PostgresRecordBirthRepository) CurrentBinding(ctx context.Context) (recordbirth.Binding, bool, error) {
	return repository.binding(ctx, `select public.record_birth_current_binding()`)
}

func (repository *PostgresRecordBirthRepository) ContinuityBinding(ctx context.Context) (recordbirth.Binding, bool, error) {
	return repository.binding(ctx, `select public.record_birth_continuity_binding()`)
}

func (repository *PostgresRecordBirthRepository) binding(ctx context.Context, statement string) (recordbirth.Binding, bool, error) {
	raw, err := repository.query(ctx, statement, nil)
	if err != nil {
		return recordbirth.Binding{}, false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return recordbirth.Binding{}, false, nil
	}
	var projection struct {
		birthBindingCommand
		ProjectID string `json:"project_id"`
	}
	if err := decodeBirthJSON(raw, &projection); err != nil {
		return recordbirth.Binding{}, false, err
	}
	cutover, err := decodeBirthDigest(projection.CutoverDigest)
	if err != nil {
		return recordbirth.Binding{}, false, err
	}
	binding := recordbirth.Binding{
		AuthorityID: projection.AuthorityID, LineageID: projection.LineageID, DeploymentID: projection.DeploymentID,
		ProjectID: projection.ProjectID, Generation: projection.Generation, KeyID: projection.KeyID, CutoverDigest: cutover,
	}
	if binding.Validate() != nil {
		return recordbirth.Binding{}, false, recordbirth.ErrUnavailable
	}
	return binding, true, nil
}

// CheckpointProof reads only committed APP state, after the allocation command
// has finished. The transaction is read-only and commits before returning.
func (repository *PostgresRecordBirthRepository) CheckpointProof(ctx context.Context, request recordbirth.Request, anchor recordbirth.CheckpointHead) ([]byte, error) {
	if request.Validate() != nil || anchor.Validate() != nil {
		return nil, recordbirth.ErrInvalid
	}
	command := struct {
		birthRequestCommand
		ExpectedSequence uint64 `json:"expected_sequence"`
		ExpectedHash     string `json:"expected_hash"`
	}{birthRequest(request), anchor.Sequence, hex.EncodeToString(anchor.Hash[:])}
	raw, err := repository.queryWithOptions(ctx, `select public.record_birth_checkpoint_proof($1)`, command,
		pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	proof, err := recordbirth.DecodeCheckpointProof(raw)
	if err != nil || proof.Checkpoint.Request != request || proof.ExpectedAnchor != anchor {
		return nil, fmt.Errorf("%w: invalid APP checkpoint proof", recordbirth.ErrUnavailable)
	}
	return raw, nil
}

func (repository *PostgresRecordBirthRepository) BeginAllocation(ctx context.Context, request recordbirth.Request, binding *recordbirth.Binding, continuityProof []byte) (recordbirth.Allocation, error) {
	if request.Validate() != nil {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	command := struct {
		birthRequestCommand
		birthBindingCommand
		Domain          string `json:"domain"`
		ContinuityProof string `json:"continuity_proof"`
	}{birthRequestCommand: birthRequest(request), Domain: "ordinary", ContinuityProof: hex.EncodeToString(continuityProof)}
	if binding != nil {
		if binding.Validate() != nil || binding.ProjectID != request.ProjectID {
			return recordbirth.Allocation{}, recordbirth.ErrInvalid
		}
		command.Domain, command.birthBindingCommand = "certifiable", birthBinding(*binding)
	}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_allocation_begin($1)`, command)
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if !found || allocation.Request != request {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	allocation.ContinuityProof = continuityProof
	return allocation, nil
}

func (repository *PostgresRecordBirthRepository) RegisterReceipt(ctx context.Context, request recordbirth.Request, raw, continuityProof []byte) (recordbirth.Allocation, error) {
	event, err := recordbirth.DecodeReceipt(raw)
	if request.Validate() != nil || err != nil || event.Request != request {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	command := struct {
		birthRequestCommand
		birthBindingCommand
		Sequence          uint64                `json:"sequence"`
		PreviousHash      string                `json:"previous_hash"`
		Kind              recordbirth.EventKind `json:"kind"`
		ReservationID     string                `json:"reservation_id"`
		RecordID          string                `json:"record_id"`
		InitialCommitment string                `json:"initial_commitment"`
		Receipt           string                `json:"receipt"`
		ContinuityProof   string                `json:"continuity_proof"`
	}{birthRequest(request), birthBinding(event.Binding), event.Sequence, hex.EncodeToString(event.PreviousHash[:]),
		event.Kind, event.ReservationID, event.RecordID, hex.EncodeToString(event.InitialCommitment[:]), hex.EncodeToString(raw), hex.EncodeToString(continuityProof)}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_receipt_register($1)`, command)
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if !found || allocation.Request != request {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	allocation.ContinuityProof = continuityProof
	return allocation, nil
}

func (repository *PostgresRecordBirthRepository) PrepareAllocation(ctx context.Context, request recordbirth.Request, creation recordbirth.InitialCreation, continuityProof []byte) (recordbirth.Allocation, error) {
	if request.Validate() != nil || creation.Validate() != nil || creation.AuthorID != request.ActorID {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	command := struct {
		birthRequestCommand
		birthCreationCommand
		ContinuityProof string `json:"continuity_proof"`
	}{birthRequest(request), birthCreation(creation), hex.EncodeToString(continuityProof)}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_prepare_create($1)`, command)
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if !found || allocation.Request != request || allocation.Creation != creation {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	allocation.ContinuityProof = continuityProof
	return allocation, nil
}

func (repository *PostgresRecordBirthRepository) CommittedCreation(ctx context.Context, recordID string) (recordbirth.Allocation, error) {
	if domain := recordidentity.Classify(recordID); domain != recordidentity.Ordinary && domain != recordidentity.Certifiable {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	allocation, found, err := repository.allocation(ctx, `select public.record_birth_committed($1)`, struct {
		RecordID string `json:"record_id"`
	}{recordID})
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if !found || allocation.RecordID != recordID {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	return allocation, nil
}

func (repository *PostgresRecordBirthRepository) VerifyCertification(ctx context.Context, recordID string, observation recordbirth.Observation) (recordbirth.Allocation, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		recordidentity.Classify(recordID) != recordidentity.Certifiable || observation.Binding.Validate() != nil ||
		observation.HeadSequence == 0 || observation.HeadSequence > math.MaxInt64 || observation.HeadHash == ([32]byte{}) {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	var allocation recordbirth.Allocation
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted},
		func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			var err error
			allocation, err = verifyRecordBirthCertificationOnTx(ctx, transaction.tx, recordID, observation)
			return err
		})
	if err != nil {
		if errors.Is(err, recordbirth.ErrInvalid) || errors.Is(err, recordbirth.ErrUncertified) || errors.Is(err, recordbirth.ErrUnavailable) {
			return recordbirth.Allocation{}, err
		}
		return recordbirth.Allocation{}, appBirthError(err)
	}
	return allocation, nil
}

func verifyRecordBirthCertificationOnTx(ctx context.Context, tx pgx.Tx, recordID string, observation recordbirth.Observation) (recordbirth.Allocation, error) {
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) || recordidentity.Classify(recordID) != recordidentity.Certifiable || observation.Binding.Validate() != nil ||
		observation.HeadSequence == 0 || observation.HeadSequence > math.MaxInt64 || observation.HeadHash == ([32]byte{}) {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	command := struct {
		birthBindingCommand
		ProjectID    string `json:"project_id"`
		RecordID     string `json:"record_id"`
		HeadSequence uint64 `json:"head_sequence"`
		HeadHash     string `json:"head_hash"`
		Reserved     string `json:"reserved"`
		Prepared     string `json:"prepared"`
		Created      string `json:"created"`
	}{birthBinding(observation.Binding), observation.Binding.ProjectID, recordID, observation.HeadSequence,
		hex.EncodeToString(observation.HeadHash[:]), hex.EncodeToString(observation.Reserved),
		hex.EncodeToString(observation.Prepared), hex.EncodeToString(observation.Created)}
	body, err := json.Marshal(command)
	if err != nil {
		return recordbirth.Allocation{}, fmt.Errorf("%w: encode birth command: %v", recordbirth.ErrInvalid, err)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `select public.record_birth_certification($1)`, body).Scan(&raw); err != nil {
		return recordbirth.Allocation{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return recordbirth.Allocation{}, recordbirth.ErrUncertified
	}
	var projection birthAllocationProjection
	if err := decodeBirthJSON(raw, &projection); err != nil {
		return recordbirth.Allocation{}, err
	}
	allocation, err := projection.allocation()
	if err != nil {
		return recordbirth.Allocation{}, err
	}
	if allocation.RecordID != recordID || allocation.State != recordbirth.StateConfirmed {
		return recordbirth.Allocation{}, recordbirth.ErrUncertified
	}
	return allocation, nil
}

func consumeRecordBirthOnTx(ctx context.Context, tx pgx.Tx, request recordbirth.Request, permit recordbirth.Permit) error {
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) || request.Validate() != nil || permit.Creation.Validate() != nil ||
		permit.Creation.AuthorID != request.ActorID || permit.ReservationID != permit.Creation.ReservationID || permit.RecordID != permit.Creation.RecordID {
		return recordbirth.ErrInvalid
	}
	command := struct {
		birthRequestCommand
		birthCreationCommand
		AllocationID    string `json:"allocation_id"`
		PrepareReceipt  string `json:"prepare_receipt"`
		ContinuityProof string `json:"continuity_proof"`
	}{birthRequest(request), birthCreation(permit.Creation), permit.AllocationID, hex.EncodeToString(permit.PrepareReceipt), hex.EncodeToString(permit.ContinuityProof)}
	body, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("%w: encode birth permit: %v", recordbirth.ErrInvalid, err)
	}
	if _, err := tx.Exec(ctx, `select public.record_birth_consume($1)`, body); err != nil {
		return fmt.Errorf("consume birth permit: %w", err)
	}
	return nil
}

func finishRecordBirthOnTx(ctx context.Context, tx pgx.Tx, permit recordbirth.Permit) error {
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) || permit.Creation.Validate() != nil ||
		permit.ReservationID != permit.Creation.ReservationID || permit.RecordID != permit.Creation.RecordID {
		return recordbirth.ErrInvalid
	}
	command := struct {
		birthCreationCommand
		AllocationID    string `json:"allocation_id"`
		AuthorID        string `json:"author_id"`
		PrepareReceipt  string `json:"prepare_receipt"`
		ContinuityProof string `json:"continuity_proof"`
	}{birthCreation(permit.Creation), permit.AllocationID, permit.Creation.AuthorID, hex.EncodeToString(permit.PrepareReceipt), hex.EncodeToString(permit.ContinuityProof)}
	body, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("%w: encode birth finish: %v", recordbirth.ErrInvalid, err)
	}
	if _, err := tx.Exec(ctx, `select public.record_birth_finish($1)`, body); err != nil {
		return fmt.Errorf("finish birth permit: %w", err)
	}
	return nil
}

func appBirthError(err error) error {
	if errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		return fmt.Errorf("%w: APP admission: %w", recordbirth.ErrUnavailable, err)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22023":
			return fmt.Errorf("%w: APP rejected birth command: %w", recordbirth.ErrInvalid, err)
		case "23505", "23514":
			return fmt.Errorf("%w: APP birth binding conflict: %w", recordbirth.ErrConflict, err)
		default:
			return fmt.Errorf("%w: APP birth command rejected: %w", recordbirth.ErrUnavailable, err)
		}
	}
	return fmt.Errorf("%w: APP birth outcome could not be confirmed: %w", recordbirth.ErrPending, err)
}

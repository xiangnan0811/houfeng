package recordbirth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrCheckpointChanged identifies an anchor CAS conflict. A coordinator may
// read the current anchor and obtain a new proof for the same allocation.
var ErrCheckpointChanged = errors.New("record birth checkpoint anchor changed")

var _ ContinuityReader = (*PostgresAuthority)(nil)

// RequestObservation is the W2 coverage and event history for one stable
// request. A nil Checkpoint means the complete observed request is absent.
type RequestObservation struct {
	Checkpoint *Checkpoint
	RecordID   string
	Reserved   []byte
	Prepared   []byte
	Created    []byte
}

// Continuity reads the authoritative anchor and enabled state without giving
// the runtime access to the signing key. A returned token is meaningful only
// as a fresh observation in the caller's admitted continuity protocol.
func (authority *PostgresAuthority) Continuity(ctx context.Context, binding Binding) (ContinuityObservation, error) {
	if ctx == nil || binding.Validate() != nil {
		return ContinuityObservation{}, ErrInvalid
	}
	if authority == nil || authority.pool == nil {
		return ContinuityObservation{}, ErrUnavailable
	}
	if err := authority.checkApplicationIdentity(ctx); err != nil {
		return ContinuityObservation{}, err
	}
	command, err := json.Marshal(postgresBinding(binding))
	if err != nil {
		return ContinuityObservation{}, fmt.Errorf("%w: encode continuity command: %v", ErrInvalid, err)
	}
	var raw []byte
	var actual postgresPhysicalIdentity
	var epoch int64
	err = authority.pool.QueryRow(ctx, `select proof,system_identifier,database_oid,database_name,identity_epoch
		from public.record_birth_continuity($1)`, command).Scan(
		&raw, &actual.SystemIdentifier, &actual.DatabaseOID, &actual.DatabaseName, &epoch)
	if err != nil {
		return ContinuityObservation{}, postgresBirthError(err)
	}
	if err := authority.checkObservedPhysicalIdentity(binding, actual, epoch); err != nil {
		return ContinuityObservation{}, err
	}
	observation, err := DecodeContinuityProof(raw)
	if err != nil || observation.Binding != binding {
		return ContinuityObservation{}, fmt.Errorf("%w: invalid continuity observation", ErrUnavailable)
	}
	return observation, nil
}

// ObserveRequest reads W2 coverage by stable request identity, including when
// the corresponding APP request or record is missing. It does not establish
// APP continuity or make an absence result sufficient to change domains.
func (authority *PostgresAuthority) ObserveRequest(ctx context.Context, binding Binding, request Request) (RequestObservation, error) {
	if ctx == nil || binding.Validate() != nil || request.Validate() != nil || binding.ProjectID != request.ProjectID {
		return RequestObservation{}, ErrInvalid
	}
	if authority == nil || authority.pool == nil {
		return RequestObservation{}, ErrUnavailable
	}
	if err := authority.checkApplicationIdentity(ctx); err != nil {
		return RequestObservation{}, err
	}
	command, err := json.Marshal(postgresRequest(binding, request))
	if err != nil {
		return RequestObservation{}, fmt.Errorf("%w: encode request observation: %v", ErrInvalid, err)
	}
	var result RequestObservation
	var checkpointBytes []byte
	var recordID *string
	var actual postgresPhysicalIdentity
	var epoch int64
	err = authority.pool.QueryRow(ctx, `select checkpoint_bytes,record_id,reserved,prepared,created,
		system_identifier,database_oid,database_name,identity_epoch
		from public.record_birth_observe_request($1)`, command).Scan(
		&checkpointBytes, &recordID, &result.Reserved, &result.Prepared, &result.Created,
		&actual.SystemIdentifier, &actual.DatabaseOID, &actual.DatabaseName, &epoch)
	if err != nil {
		return RequestObservation{}, postgresBirthError(err)
	}
	if err := authority.checkObservedPhysicalIdentity(binding, actual, epoch); err != nil {
		return RequestObservation{}, err
	}
	if checkpointBytes == nil && recordID == nil && result.Reserved == nil && result.Prepared == nil && result.Created == nil {
		return RequestObservation{}, nil
	}
	if len(checkpointBytes) == 0 || len(checkpointBytes) > maxCheckpointProofBytes || recordID == nil || *recordID == "" ||
		len(result.Reserved) == 0 || (result.Prepared != nil && len(result.Prepared) == 0) ||
		(result.Created != nil && len(result.Created) == 0) {
		return RequestObservation{}, fmt.Errorf("%w: incomplete request observation", ErrUnavailable)
	}
	reader := receiptReader{rest: checkpointBytes}
	checkpoint := readCheckpoint(&reader)
	if reader.failed || len(reader.rest) != 0 || checkpoint.Validate() != nil || checkpoint.Request != request ||
		!sameBirthLineage(checkpoint.Binding, binding) || checkpoint.Binding.DeploymentID != binding.DeploymentID ||
		checkpoint.Binding.Generation > binding.Generation {
		return RequestObservation{}, fmt.Errorf("%w: invalid observed checkpoint", ErrUnavailable)
	}
	result.Checkpoint = &checkpoint
	result.RecordID = *recordID
	events := Observation{
		Binding: binding, Reserved: result.Reserved, Prepared: result.Prepared, Created: result.Created,
	}
	if err := validateBirthEvents(events, result.RecordID, &request, false); err != nil {
		return RequestObservation{}, err
	}
	return result, nil
}

func (authority *PostgresAuthority) checkObservedPhysicalIdentity(binding Binding, actual postgresPhysicalIdentity, epoch int64) error {
	if epoch <= 0 {
		return fmt.Errorf("%w: invalid birth authority physical identity", ErrUnavailable)
	}
	actual.ID = binding.AuthorityID
	actual.Epoch = uint64(epoch)
	if authority.expected != nil && actual != *authority.expected {
		return fmt.Errorf("%w: birth authority physical identity mismatch", ErrUnavailable)
	}
	return nil
}

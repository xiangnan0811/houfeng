package recordauthority

import (
	"encoding/binary"
	"errors"
	"math"
	"time"

	"houfeng/internal/center/recordplatform"
)

const (
	composeHeartbeatCommandMagic      = "HOUFENG-APP-PROJECTION-COMMAND-V1"
	composeHeartbeatCommandVersion    = 1
	composeHeartbeatCommandOperation  = 3
	composeHeartbeatCommandFieldCount = 10
	composeHeartbeatCommandLength     = 177
	composeHeartbeatLeaseSeconds      = 90
)

var ErrComposeHeartbeatInvalid = errors.New("Compose Records membership heartbeat is invalid")

// MarshalMembershipHeartbeatCommandV1 derives the complete heartbeat identity
// from verified durable state. Callers supply only the issue time; deployment,
// project, instance, capability, epoch, fence, and admission flags are closed.
func MarshalMembershipHeartbeatCommandV1(
	state VerifiedComposeState,
	issuedAt time.Time,
) ([]byte, time.Time, error) {
	if err := validateComposeHeartbeatState(state); err != nil {
		return nil, time.Time{}, err
	}
	issuedAtEpoch := issuedAt.Unix()
	if issuedAt.IsZero() || issuedAtEpoch <= 0 || issuedAtEpoch > math.MaxInt64-composeHeartbeatLeaseSeconds {
		return nil, time.Time{}, ErrComposeHeartbeatInvalid
	}
	expiresAtEpoch := issuedAtEpoch + composeHeartbeatLeaseSeconds

	raw := make([]byte, 0, composeHeartbeatCommandLength)
	raw = append(raw, composeHeartbeatCommandMagic...)
	raw = append(raw, 0, composeHeartbeatCommandVersion, composeHeartbeatCommandOperation, composeHeartbeatCommandFieldCount)
	raw = append(raw, string(state.DeploymentID)...)
	raw = append(raw, string(recordplatform.ProjectIDDefault)...)
	raw = append(raw, "compose-center"...)
	raw = append(raw, "api"...)
	raw = append(raw, "records.runtime"...)
	raw = appendComposeHeartbeatUint64(raw, state.Memberships[0].DeploymentEpoch)
	raw = appendComposeHeartbeatUint64(raw, state.Memberships[0].FenceContractVersion)
	raw = append(raw, 1, 0)
	raw = appendComposeHeartbeatUint64(raw, uint64(issuedAtEpoch))
	raw = appendComposeHeartbeatUint64(raw, uint64(expiresAtEpoch))
	if len(raw) != composeHeartbeatCommandLength {
		return nil, time.Time{}, ErrComposeHeartbeatInvalid
	}
	return raw, time.Unix(expiresAtEpoch, 0).UTC(), nil
}

func validateComposeHeartbeatState(state VerifiedComposeState) error {
	if err := recordplatform.ValidateDeploymentID(state.DeploymentID); err != nil {
		return ErrComposeHeartbeatInvalid
	}
	if state.ActivationCommand.DeploymentID != string(state.DeploymentID) ||
		state.ActivationCommand.ActiveProfile != recordplatform.ProjectionProfilePostgresSync {
		return ErrComposeHeartbeatInvalid
	}
	if _, err := state.ActivationCommand.MarshalBinary(); err != nil {
		return ErrComposeHeartbeatInvalid
	}
	want := ComposeMembership{
		InstanceID:           "compose-center",
		InstanceKind:         "api",
		Capability:           "records.runtime",
		DeploymentEpoch:      state.ActivationCommand.IdentitySetEpoch,
		FenceContractVersion: state.ActivationCommand.MinimumFenceContractVersion,
		LoadBalancerAdmitted: true,
		QueueAdmitted:        false,
	}
	if len(state.Memberships) != 1 || state.Memberships[0] != want {
		return ErrComposeHeartbeatInvalid
	}
	return nil
}

func appendComposeHeartbeatUint64(destination []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(destination, encoded[:]...)
}

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

// MarshalMembershipHeartbeatCommandV2 encodes the current signed inventory.
// Callers must separately reverify durable state, receipt and database state.
func MarshalMembershipHeartbeatCommandV2(state VerifiedComposeState, issuedAt time.Time) ([]byte, time.Time, error) {
	if err := validateComposeHeartbeatStateV2(state); err != nil {
		return nil, time.Time{}, err
	}
	seconds := issuedAt.Unix()
	if issuedAt.IsZero() || seconds <= 0 || seconds > math.MaxInt64-composeHeartbeatLeaseSeconds {
		return nil, time.Time{}, ErrComposeHeartbeatInvalid
	}
	rotation := state.MembershipRotation
	expires := seconds + composeHeartbeatLeaseSeconds
	raw := make([]byte, 0, 240)
	raw = append(raw, composeHeartbeatCommandMagic...)
	raw = append(raw, 0, composeHeartbeatCommandVersion, 4, 9)
	raw = append(raw, string(state.DeploymentID)...)
	raw = append(raw, state.ActivationCommand.ActivationBundleDigest[:]...)
	raw = appendComposeHeartbeatUint64(raw, rotation.NextWitnessedLedgerSequence)
	raw = append(raw, rotation.NextWitnessedLedgerHash[:]...)
	raw = appendComposeHeartbeatUint64(raw, rotation.NextIdentitySetEpoch)
	raw = append(raw, rotation.NextIdentitySetDigest[:]...)
	raw = appendComposeHeartbeatUint64(raw, rotation.NextMinimumFenceContractVersion)
	raw = appendComposeHeartbeatUint64(raw, uint64(seconds))
	raw = appendComposeHeartbeatUint64(raw, uint64(expires))
	if len(raw) != 240 {
		return nil, time.Time{}, ErrComposeHeartbeatInvalid
	}
	return raw, time.Unix(expires, 0).UTC(), nil
}

func validateComposeHeartbeatStateV2(state VerifiedComposeState) error {
	if recordplatform.ValidateDeploymentID(state.DeploymentID) != nil || state.MembershipVersion != 2 ||
		state.MembershipRotation == nil || state.verifiedMembershipRotation == nil ||
		state.ActivationCommand != state.genesisActivationCommand ||
		*state.MembershipRotation != *state.verifiedMembershipRotation ||
		state.ActivationCommand.DeploymentID != string(state.DeploymentID) ||
		state.ActivationCommand.ActiveProfile != recordplatform.ProjectionProfilePostgresSync {
		return ErrComposeHeartbeatInvalid
	}
	if _, err := state.ActivationCommand.MarshalBinary(); err != nil {
		return ErrComposeHeartbeatInvalid
	}
	rotation := state.MembershipRotation
	if _, err := rotation.MarshalBinary(); err != nil {
		return ErrComposeHeartbeatInvalid
	}
	genesis := state.ActivationCommand
	if rotation.DeploymentID != genesis.DeploymentID || rotation.ActiveProfile != genesis.ActiveProfile ||
		rotation.ExpectedWitnessedLedgerSequence != genesis.WitnessedLedgerSequence || rotation.ExpectedWitnessedLedgerHash != genesis.WitnessedLedgerHash ||
		rotation.ExpectedIdentitySetEpoch != genesis.IdentitySetEpoch || rotation.ExpectedIdentitySetDigest != genesis.IdentitySetDigest ||
		rotation.ExpectedAdapterPolicyGeneration != genesis.AdapterPolicyGeneration || rotation.ExpectedAdapterPolicyDigest != genesis.AdapterPolicyDigest ||
		rotation.ExpectedMinimumFenceContractVersion != genesis.MinimumFenceContractVersion || rotation.ExpectedTrustRevision != genesis.TrustRevision || rotation.ExpectedTrustHeadHash != genesis.TrustHeadHash ||
		rotation.NextWitnessedLedgerSequence != 2 || rotation.NextIdentitySetEpoch != 2 ||
		rotation.NextIdentitySetDigest != membershipIdentityDigestV2(genesis.DeploymentID) ||
		rotation.NextAdapterPolicyGeneration != genesis.AdapterPolicyGeneration || rotation.NextAdapterPolicyDigest != genesis.AdapterPolicyDigest ||
		rotation.NextMinimumFenceContractVersion != 1 || rotation.NextTrustRevision != 2 {
		return ErrComposeHeartbeatInvalid
	}
	want := composeMembershipInventoryV2()
	if len(state.Memberships) != len(want) {
		return ErrComposeHeartbeatInvalid
	}
	for i, member := range want {
		if state.Memberships[i] != (ComposeMembership{member.InstanceID, member.InstanceKind, member.Capability, member.DeploymentEpoch, member.FenceContractVersion, member.LoadBalancerAdmitted, member.QueueAdmitted}) {
			return ErrComposeHeartbeatInvalid
		}
	}
	return nil
}

func validateComposeHeartbeatState(state VerifiedComposeState) error {
	if state.MembershipVersion != 1 || state.MembershipRotation != nil || state.verifiedMembershipRotation != nil || state.ActivationCommand != state.genesisActivationCommand {
		return ErrComposeHeartbeatInvalid
	}
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

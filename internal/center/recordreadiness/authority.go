package recordreadiness

import (
	"context"

	"houfeng/internal/center/store"
)

type membershipAuthority struct {
	gate store.AdmissionGate
}

func MembershipAuthority(gate store.AdmissionGate) Authority {
	if nilCapability(gate) {
		return nil
	}
	return &membershipAuthority{gate: gate}
}

func (authority *membershipAuthority) Kind() CapabilityKind {
	return CapabilityAuthorityMembership
}

func (authority *membershipAuthority) Probe(ctx context.Context) (AuthorityReport, error) {
	if ctx == nil || authority == nil || nilCapability(authority.gate) {
		return AuthorityReport{Reason: AuthorityTypedNil}, ErrReadinessUnavailable
	}
	return AuthorityReport{Healthy: true, Reason: AuthorityOK}, nil
}

type witnessAuthority struct {
	source store.WitnessedRecordSubjectTombstoneSource
}

func WitnessAuthority(source store.WitnessedRecordSubjectTombstoneSource) Authority {
	if nilCapability(source) {
		return nil
	}
	return &witnessAuthority{source: source}
}

func (authority *witnessAuthority) Kind() CapabilityKind {
	return CapabilityAuthorityWitness
}

func (authority *witnessAuthority) Probe(ctx context.Context) (AuthorityReport, error) {
	if ctx == nil || authority == nil || nilCapability(authority.source) {
		return AuthorityReport{Reason: AuthorityTypedNil}, ErrReadinessUnavailable
	}
	return AuthorityReport{Healthy: true, Reason: AuthorityOK}, nil
}

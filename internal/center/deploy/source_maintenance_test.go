package deploy

import (
	"testing"

	"houfeng/internal/center/sourceretirement"
)

func TestSourceMaintenanceActiveRequiresMatchingCompletedHandoff(t *testing.T) {
	domain := sourceretirement.DomainIdentity{ID: "domain", SystemIdentifier: "123", DatabaseOID: 42, DatabaseName: "witness", Epoch: 1}
	config := SourceMaintenanceConfig{Witness: domain}
	admission := sourceAdmission{Witness: domain, Generation: 2, Mode: "active", RecoveryID: "recovery"}
	state := sourceretirement.Observation{Domain: domain, Generation: 2, Mode: "active", RecoveryID: "recovery", HandoffCompleted: true}
	if !sourceActive(&admission, &config, state) {
		t.Fatal("matching completed handoff rejected")
	}
	cases := []struct {
		name      string
		admission sourceAdmission
		config    SourceMaintenanceConfig
		state     sourceretirement.Observation
	}{
		{"generation", sourceAdmission{Witness: domain, Generation: 1, Mode: "active", RecoveryID: "recovery"}, config, state},
		{"closed", sourceAdmission{Witness: domain, Generation: 2, Mode: "ready_closed", RecoveryID: "recovery"}, config, state},
		{"recovery-id", sourceAdmission{Witness: domain, Generation: 2, Mode: "active", RecoveryID: "other"}, config, state},
		{"physical", sourceAdmission{Witness: sourceretirement.DomainIdentity{}, Generation: 2, Mode: "active", RecoveryID: "recovery"}, config, state},
		{"witness-recovering", admission, config, sourceretirement.Observation{Domain: domain, Generation: 2, Mode: "recovering", RecoveryID: "recovery", HandoffCompleted: true}},
		{"incomplete", admission, config, sourceretirement.Observation{Domain: domain, Generation: 2, Mode: "active", RecoveryID: "recovery"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if sourceActive(&tc.admission, &tc.config, tc.state) {
				t.Fatal("invalid handoff accepted")
			}
		})
	}
	if sourceActive(nil, &config, state) || sourceActive(&admission, nil, state) {
		t.Fatal("missing authority accepted")
	}
}

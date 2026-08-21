package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

func TestNewDeploymentMembershipAdmissionGateRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	valid := testDeploymentMembershipIdentity()
	tests := map[string]DeploymentMembershipIdentity{
		"empty instance":       withDeploymentMembershipInstance(valid, ""),
		"uppercase instance":   withDeploymentMembershipInstance(valid, "API_01"),
		"invalid deployment":   withDeploymentMembershipDeployment(valid, "dp-not-hex"),
		"non-default project":  withDeploymentMembershipProject(valid, "other"),
		"unknown kind":         withDeploymentMembershipKind(valid, "center"),
		"empty capability":     withDeploymentMembershipCapability(valid, ""),
		"uppercase capability": withDeploymentMembershipCapability(valid, "Records.Runtime"),
	}
	for name, identity := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewDeploymentMembershipAdmissionGate(identity); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("NewDeploymentMembershipAdmissionGate() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
			}
		})
	}
}

func TestDeploymentMembershipAdmissionGateTypedNilAndNilTxFailClosed(t *testing.T) {
	t.Parallel()

	var typedNil *DeploymentMembershipAdmissionGate
	if err := typedNil.Admit(context.Background(), &fakeRecordPlatformTx{}); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("typed-nil Admit() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}

	gate := mustDeploymentMembershipAdmissionGate(t)
	if err := gate.Admit(context.Background(), nil); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("Admit(nil tx) error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}
}

func TestDeploymentMembershipAdmissionGateRejectsMissingStaleWrongAndUnactivatedAuthority(t *testing.T) {
	t.Parallel()

	identity := testDeploymentMembershipIdentity()
	now := time.Date(2026, time.August, 21, 4, 0, 0, 0, time.UTC)
	liveMembership := admissionMembershipRow{
		InstanceID:           identity.InstanceID,
		DeploymentID:         string(identity.DeploymentID),
		ProjectID:            string(identity.ProjectID),
		InstanceKind:         identity.InstanceKind,
		DeploymentEpoch:      1,
		FenceContractVersion: 2,
		Capability:           identity.Capability,
		LoadBalancerAdmitted: true,
		HeartbeatLive:        true,
		HeartbeatExpiresAt:   now.Add(time.Minute),
	}
	activatedContract := admissionContractRow{
		DeploymentID:                string(identity.DeploymentID),
		MinimumFenceContractVersion: 1,
		ActiveProfile:               "postgres_sync",
	}

	tests := map[string]struct {
		membership *admissionMembershipRow
		contract   *admissionContractRow
	}{
		"empty membership": {membership: nil, contract: &activatedContract},
		"stale heartbeat": {
			membership: &admissionMembershipRow{
				InstanceID: identity.InstanceID, DeploymentID: string(identity.DeploymentID),
				ProjectID: string(identity.ProjectID), InstanceKind: identity.InstanceKind,
				DeploymentEpoch: 1, FenceContractVersion: 2, Capability: identity.Capability,
				LoadBalancerAdmitted: true, HeartbeatLive: false, HeartbeatExpiresAt: now.Add(-time.Second),
			},
			contract: &activatedContract,
		},
		"wrong deployment": {
			membership: &admissionMembershipRow{
				InstanceID: identity.InstanceID, DeploymentID: "dp-" + strings.Repeat("b", 64),
				ProjectID: string(identity.ProjectID), InstanceKind: identity.InstanceKind,
				DeploymentEpoch: 1, FenceContractVersion: 2, Capability: identity.Capability,
				LoadBalancerAdmitted: true, HeartbeatLive: true, HeartbeatExpiresAt: now.Add(time.Minute),
			},
			contract: &activatedContract,
		},
		"unactivated contract": {membership: &liveMembership, contract: &admissionContractRow{}},
		"contract drift": {
			membership: &liveMembership,
			contract: &admissionContractRow{
				DeploymentID: "dp-" + strings.Repeat("c", 64), MinimumFenceContractVersion: 1, ActiveProfile: "postgres_sync",
			},
		},
		"stale fence": {
			membership: &admissionMembershipRow{
				InstanceID: identity.InstanceID, DeploymentID: string(identity.DeploymentID),
				ProjectID: string(identity.ProjectID), InstanceKind: identity.InstanceKind,
				DeploymentEpoch: 1, FenceContractVersion: 1, Capability: identity.Capability,
				LoadBalancerAdmitted: true, HeartbeatLive: true, HeartbeatExpiresAt: now.Add(time.Minute),
			},
			contract: &admissionContractRow{
				DeploymentID: string(identity.DeploymentID), MinimumFenceContractVersion: 3, ActiveProfile: "postgres_sync",
			},
		},
		"api not load-balancer admitted": {
			membership: &admissionMembershipRow{
				InstanceID: identity.InstanceID, DeploymentID: string(identity.DeploymentID),
				ProjectID: string(identity.ProjectID), InstanceKind: identity.InstanceKind,
				DeploymentEpoch: 1, FenceContractVersion: 2, Capability: identity.Capability,
				QueueAdmitted: true, HeartbeatLive: true, HeartbeatExpiresAt: now.Add(time.Minute),
			},
			contract: &activatedContract,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tx := newAdmissionGateFakeTx(test.membership, test.contract)
			gate := mustDeploymentMembershipAdmissionGate(t)
			if err := gate.Admit(context.Background(), tx); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("Admit() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
			}
			if tx.execCount != 0 {
				t.Fatalf("Admit() execCount = %d, want 0 writes", tx.execCount)
			}
		})
	}
}

func TestDeploymentMembershipAdmissionGateAdmitsMatchingMembershipAndActivatedContractOnCallerTx(t *testing.T) {
	t.Parallel()

	identity := testDeploymentMembershipIdentity()
	tx := newAdmissionGateFakeTx(
		&admissionMembershipRow{
			InstanceID: identity.InstanceID, DeploymentID: string(identity.DeploymentID),
			ProjectID: string(identity.ProjectID), InstanceKind: identity.InstanceKind,
			DeploymentEpoch: 1, FenceContractVersion: 2, Capability: identity.Capability,
			LoadBalancerAdmitted: true, HeartbeatLive: true,
			HeartbeatExpiresAt: time.Date(2026, time.August, 21, 5, 0, 0, 0, time.UTC),
		},
		&admissionContractRow{
			DeploymentID: string(identity.DeploymentID), MinimumFenceContractVersion: 1, ActiveProfile: "postgres_sync",
		},
	)

	gate := mustDeploymentMembershipAdmissionGate(t)
	if err := gate.Admit(context.Background(), tx); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if tx.execCount != 0 {
		t.Fatalf("Admit() execCount = %d, want read-only", tx.execCount)
	}
	if len(tx.querySQL) != 2 {
		t.Fatalf("Admit() queries = %#v, want membership then contract", tx.querySQL)
	}
	if !strings.Contains(tx.querySQL[0], "from public.deployment_membership") ||
		!strings.Contains(tx.querySQL[0], "heartbeat_expires_at > transaction_timestamp()") {
		t.Fatalf("membership SQL = %s, want database-time heartbeat predicate", tx.querySQL[0])
	}
	if !strings.Contains(tx.querySQL[1], "from public.deployment_contract_state") {
		t.Fatalf("contract SQL = %s", tx.querySQL[1])
	}
	if got, ok := tx.queryArgs[0][0].(string); !ok || got != identity.InstanceID {
		t.Fatalf("membership query args = %#v, want bound instance", tx.queryArgs[0])
	}
	if got, ok := tx.queryArgs[1][0].(string); !ok || got != string(identity.ProjectID) {
		t.Fatalf("contract query args = %#v, want bound project", tx.queryArgs[1])
	}
}

func testDeploymentMembershipIdentity() DeploymentMembershipIdentity {
	return DeploymentMembershipIdentity{
		InstanceID:   "api-01",
		DeploymentID: recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64)),
		ProjectID:    recordplatform.ProjectIDDefault,
		InstanceKind: "api",
		Capability:   "records.runtime",
	}
}

func mustDeploymentMembershipAdmissionGate(t *testing.T) *DeploymentMembershipAdmissionGate {
	t.Helper()
	gate, err := NewDeploymentMembershipAdmissionGate(testDeploymentMembershipIdentity())
	if err != nil {
		t.Fatalf("NewDeploymentMembershipAdmissionGate() error = %v", err)
	}
	return gate
}

func withDeploymentMembershipInstance(identity DeploymentMembershipIdentity, instanceID string) DeploymentMembershipIdentity {
	identity.InstanceID = instanceID
	return identity
}

func withDeploymentMembershipDeployment(identity DeploymentMembershipIdentity, deploymentID string) DeploymentMembershipIdentity {
	identity.DeploymentID = recordplatform.DeploymentID(deploymentID)
	return identity
}

func withDeploymentMembershipProject(identity DeploymentMembershipIdentity, projectID string) DeploymentMembershipIdentity {
	identity.ProjectID = recordplatform.ProjectID(projectID)
	return identity
}

func withDeploymentMembershipKind(identity DeploymentMembershipIdentity, kind string) DeploymentMembershipIdentity {
	identity.InstanceKind = kind
	return identity
}

func withDeploymentMembershipCapability(identity DeploymentMembershipIdentity, capability string) DeploymentMembershipIdentity {
	identity.Capability = capability
	return identity
}

type admissionMembershipRow struct {
	InstanceID           string
	DeploymentID         string
	ProjectID            string
	InstanceKind         string
	DeploymentEpoch      int64
	FenceContractVersion int64
	Capability           string
	LoadBalancerAdmitted bool
	QueueAdmitted        bool
	HeartbeatLive        bool
	HeartbeatExpiresAt   time.Time
}

type admissionContractRow struct {
	DeploymentID                string
	MinimumFenceContractVersion int64
	ActiveProfile               string
}

func newAdmissionGateFakeTx(membership *admissionMembershipRow, contract *admissionContractRow) *fakeRecordPlatformTx {
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, query string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(query, "from public.deployment_membership"):
			if membership == nil {
				return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}
			row := *membership
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = row.InstanceID
				*(dest[1].(*string)) = row.DeploymentID
				*(dest[2].(*string)) = row.ProjectID
				*(dest[3].(*string)) = row.InstanceKind
				*(dest[4].(*int64)) = row.DeploymentEpoch
				*(dest[5].(*int64)) = row.FenceContractVersion
				*(dest[6].(*string)) = row.Capability
				*(dest[7].(*bool)) = row.LoadBalancerAdmitted
				*(dest[8].(*bool)) = row.QueueAdmitted
				*(dest[9].(*bool)) = row.HeartbeatLive
				*(dest[10].(*time.Time)) = row.HeartbeatExpiresAt
				return nil
			}}
		case strings.Contains(query, "from public.deployment_contract_state"):
			if contract == nil {
				return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}
			row := *contract
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*sql.NullString)) = sql.NullString{String: row.DeploymentID, Valid: row.DeploymentID != ""}
				*(dest[1].(*int64)) = row.MinimumFenceContractVersion
				*(dest[2].(*sql.NullString)) = sql.NullString{String: row.ActiveProfile, Valid: row.ActiveProfile != ""}
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	return tx
}

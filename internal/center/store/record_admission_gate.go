package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

const (
	deploymentMembershipInstanceKindAPI      = "api"
	deploymentMembershipInstanceKindWorker   = "worker"
	deploymentMembershipInstanceKindRecovery = "recovery"
)

// DeploymentMembershipIdentity is the immutable process identity bound when
// the named admission gate is constructed. Admit never reads identity from an
// event or payload.
type DeploymentMembershipIdentity struct {
	InstanceID   string
	DeploymentID recordplatform.DeploymentID
	ProjectID    recordplatform.ProjectID
	InstanceKind string
	Capability   string
}

// DeploymentMembershipAdmissionGate admits record-platform writes by reading
// the caller's already-open transaction against 0051 membership and contract
// state. Empty, stale, drifted, or unactivated authority fail closed.
type DeploymentMembershipAdmissionGate struct {
	identity DeploymentMembershipIdentity
}

var _ AdmissionGate = (*DeploymentMembershipAdmissionGate)(nil)

func NewDeploymentMembershipAdmissionGate(identity DeploymentMembershipIdentity) (*DeploymentMembershipAdmissionGate, error) {
	if err := validateDeploymentMembershipIdentity(identity); err != nil {
		return nil, err
	}
	return &DeploymentMembershipAdmissionGate{identity: identity}, nil
}

func (gate *DeploymentMembershipAdmissionGate) Admit(ctx context.Context, tx pgx.Tx) error {
	if gate == nil || tx == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	if _, err := tx.Exec(ctx, `select public.source_application_admit()`); err != nil {
		return fmt.Errorf("%w: source generation", ErrRecordPlatformAdmissionUnavailable)
	}
	return gate.admitMembership(ctx, tx)
}

func (gate *DeploymentMembershipAdmissionGate) admitMembership(ctx context.Context, tx pgx.Tx) error {

	var (
		instanceID           string
		deploymentID         string
		projectID            string
		instanceKind         string
		deploymentEpoch      int64
		fenceContractVersion int64
		capability           string
		loadBalancerAdmitted bool
		queueAdmitted        bool
		heartbeatLive        bool
		heartbeatExpiresAt   time.Time
	)
	err := tx.QueryRow(ctx, `
		select instance_id,
		       deployment_id,
		       project_id,
		       instance_kind,
		       deployment_epoch,
		       fence_contract_version,
		       capability,
		       load_balancer_admitted,
		       queue_admitted,
		       heartbeat_expires_at > clock_timestamp(),
		       heartbeat_expires_at
		from public.deployment_membership
		where instance_id = $1
		  and heartbeat_expires_at > clock_timestamp()
	`, gate.identity.InstanceID).Scan(
		&instanceID,
		&deploymentID,
		&projectID,
		&instanceKind,
		&deploymentEpoch,
		&fenceContractVersion,
		&capability,
		&loadBalancerAdmitted,
		&queueAdmitted,
		&heartbeatLive,
		&heartbeatExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("%w: deployment membership", ErrRecordPlatformAdmissionUnavailable)
	}
	if !heartbeatLive ||
		instanceID != gate.identity.InstanceID ||
		deploymentID != string(gate.identity.DeploymentID) ||
		projectID != string(gate.identity.ProjectID) ||
		instanceKind != gate.identity.InstanceKind ||
		capability != gate.identity.Capability ||
		deploymentEpoch <= 0 ||
		fenceContractVersion <= 0 ||
		heartbeatExpiresAt.IsZero() {
		return ErrRecordPlatformAdmissionUnavailable
	}
	if !deploymentMembershipKindAdmitted(instanceKind, loadBalancerAdmitted, queueAdmitted) {
		return ErrRecordPlatformAdmissionUnavailable
	}

	var (
		contractDeploymentID sql.NullString
		minimumFenceVersion  int64
		activeProfile        sql.NullString
	)
	err = tx.QueryRow(ctx, `
		select deployment_id,
		       minimum_fence_contract_version,
		       active_profile
		from public.deployment_contract_state
		where project_id = $1
	`, string(gate.identity.ProjectID)).Scan(
		&contractDeploymentID,
		&minimumFenceVersion,
		&activeProfile,
	)
	if err != nil {
		return fmt.Errorf("%w: deployment contract", ErrRecordPlatformAdmissionUnavailable)
	}
	if !contractDeploymentID.Valid ||
		contractDeploymentID.String != string(gate.identity.DeploymentID) ||
		!activeProfile.Valid ||
		(activeProfile.String != "postgres_sync" && activeProfile.String != "s3_worm") ||
		minimumFenceVersion <= 0 ||
		fenceContractVersion < minimumFenceVersion {
		return ErrRecordPlatformAdmissionUnavailable
	}
	return nil
}

// BootstrapDeploymentMembershipAdmissionGate is restricted to the direct
// PostgreSQL bootstrap principal. Initial source enrollment cannot use runtime
// source admission: the bootstrap principal deliberately has no runtime binding.
// The membership and contract checks remain identical to ordinary admission.
type BootstrapDeploymentMembershipAdmissionGate struct {
	membership *DeploymentMembershipAdmissionGate
}

func NewBootstrapDeploymentMembershipAdmissionGate(identity DeploymentMembershipIdentity) (*BootstrapDeploymentMembershipAdmissionGate, error) {
	membership, err := NewDeploymentMembershipAdmissionGate(identity)
	if err != nil {
		return nil, err
	}
	return &BootstrapDeploymentMembershipAdmissionGate{membership: membership}, nil
}

func (gate *BootstrapDeploymentMembershipAdmissionGate) Admit(ctx context.Context, tx pgx.Tx) error {
	if gate == nil || gate.membership == nil || tx == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	var bootstrap bool
	if err := tx.QueryRow(ctx, `select session_user = current_user and r.oid = 10 and r.rolsuper
		and current_setting('server_version_num')::integer / 10000 = 16
		from pg_catalog.pg_roles r where r.rolname = current_user`).Scan(&bootstrap); err != nil || !bootstrap {
		return fmt.Errorf("%w: direct bootstrap identity", ErrRecordPlatformAdmissionUnavailable)
	}
	return gate.membership.admitMembership(ctx, tx)
}

func validateDeploymentMembershipIdentity(identity DeploymentMembershipIdentity) error {
	if !validDeploymentMembershipInstanceID(identity.InstanceID) {
		return fmt.Errorf("%w: instance id", ErrRecordPlatformAdmissionUnavailable)
	}
	if err := recordplatform.ValidateDeploymentID(identity.DeploymentID); err != nil {
		return fmt.Errorf("%w: deployment id", ErrRecordPlatformAdmissionUnavailable)
	}
	if err := recordplatform.ValidateProjectID(identity.ProjectID); err != nil {
		return fmt.Errorf("%w: project id", ErrRecordPlatformAdmissionUnavailable)
	}
	switch identity.InstanceKind {
	case deploymentMembershipInstanceKindAPI, deploymentMembershipInstanceKindWorker, deploymentMembershipInstanceKindRecovery:
	default:
		return fmt.Errorf("%w: instance kind", ErrRecordPlatformAdmissionUnavailable)
	}
	if !validDeploymentMembershipCapability(identity.Capability) {
		return fmt.Errorf("%w: capability", ErrRecordPlatformAdmissionUnavailable)
	}
	return nil
}

func deploymentMembershipKindAdmitted(kind string, loadBalancerAdmitted, queueAdmitted bool) bool {
	switch kind {
	case deploymentMembershipInstanceKindAPI:
		return loadBalancerAdmitted
	case deploymentMembershipInstanceKindWorker, deploymentMembershipInstanceKindRecovery:
		return queueAdmitted
	default:
		return false
	}
}

func validDeploymentMembershipInstanceID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validDeploymentMembershipCapability(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '.' {
			return false
		}
	}
	return true
}

package recordreadiness

import (
	"context"
	"crypto/sha256"
	"errors"

	"houfeng/internal/center/recorddeletion"
)

var (
	ErrInvalidCapabilityRegistry = errors.New("invalid record readiness capability registry")
	ErrReadinessUnavailable      = errors.New("record readiness unavailable")
	ErrContentLeak               = errors.New("record readiness content leak")
)

const CapabilityContractVersionV1 uint32 = 1

type CapabilityKind string

const (
	CapabilityDeletionRecordCore               CapabilityKind = "deletion.record_core"
	CapabilityDeletionRecordAttachments        CapabilityKind = "deletion.record_attachments"
	CapabilityDeletionRecordEvidence           CapabilityKind = "deletion.record_evidence"
	CapabilityDeletionRecordMarkdownClient     CapabilityKind = "deletion.record_markdown_client"
	CapabilityDeletionRecordSearch             CapabilityKind = "deletion.record_search"
	CapabilityDeletionRecordActivityProjection CapabilityKind = "deletion.record_activity_projection"
	CapabilityDeletionRecordComparison         CapabilityKind = "deletion.record_comparison"
	CapabilityDeletionRecordCollaboration      CapabilityKind = "deletion.record_collaboration"
	CapabilityDeletionRecordPortability        CapabilityKind = "deletion.record_portability"

	CapabilityRecoveryRecordCore               CapabilityKind = "recovery.record_core"
	CapabilityRecoveryRecordAttachments        CapabilityKind = "recovery.record_attachments"
	CapabilityRecoveryRecordEvidence           CapabilityKind = "recovery.record_evidence"
	CapabilityRecoveryRecordSearch             CapabilityKind = "recovery.record_search"
	CapabilityRecoveryRecordActivityProjection CapabilityKind = "recovery.record_activity_projection"
	CapabilityRecoveryRecordCollaboration      CapabilityKind = "recovery.record_collaboration"
	CapabilityRecoveryRecordPortability        CapabilityKind = "recovery.record_portability"

	CapabilityAuthorityMembership CapabilityKind = "authority.deployment_membership"
	CapabilityAuthorityWitness    CapabilityKind = "authority.source_deletion_witness"

	CapabilityBackupOrchestration CapabilityKind = "backup.orchestration"
	CapabilityRestoreReplay       CapabilityKind = "restore.replay"
)

var requiredCapabilityKinds = []CapabilityKind{
	CapabilityDeletionRecordCore,
	CapabilityDeletionRecordAttachments,
	CapabilityDeletionRecordEvidence,
	CapabilityDeletionRecordMarkdownClient,
	CapabilityDeletionRecordSearch,
	CapabilityDeletionRecordActivityProjection,
	CapabilityDeletionRecordComparison,
	CapabilityDeletionRecordCollaboration,
	CapabilityDeletionRecordPortability,
	CapabilityRecoveryRecordCore,
	CapabilityRecoveryRecordAttachments,
	CapabilityRecoveryRecordEvidence,
	CapabilityRecoveryRecordSearch,
	CapabilityRecoveryRecordActivityProjection,
	CapabilityRecoveryRecordCollaboration,
	CapabilityRecoveryRecordPortability,
	CapabilityAuthorityMembership,
	CapabilityAuthorityWitness,
	CapabilityBackupOrchestration,
	CapabilityRestoreReplay,
}

func RequiredCapabilityKinds() []CapabilityKind {
	return append([]CapabilityKind(nil), requiredCapabilityKinds...)
}

func DeletionCapabilityKind(name recorddeletion.AdapterName) CapabilityKind {
	return CapabilityKind("deletion." + string(name))
}

func RecoveryCapabilityKind(name recorddeletion.AdapterName) CapabilityKind {
	return CapabilityKind("recovery." + string(name))
}

type PermanentDeleteState string

const (
	PermanentDeleteDisabled PermanentDeleteState = "disabled"
	PermanentDeleteEnabled  PermanentDeleteState = "enabled"
)

type CapabilityReason string

const (
	CapabilityReasonPresent      CapabilityReason = "present"
	CapabilityReasonMissing      CapabilityReason = "missing"
	CapabilityReasonUnhealthy    CapabilityReason = "unhealthy"
	CapabilityReasonIncompatible CapabilityReason = "incompatible"
	CapabilityReasonClosed       CapabilityReason = "closed"
	CapabilityReasonDuplicate    CapabilityReason = "duplicate"
	CapabilityReasonUnknown      CapabilityReason = "unknown"
)

type AuthorityReason string

const (
	AuthorityOK              AuthorityReason = "ok"
	AuthorityNil             AuthorityReason = "nil"
	AuthorityTypedNil        AuthorityReason = "typed_nil"
	AuthorityStale           AuthorityReason = "stale"
	AuthorityWrongDeployment AuthorityReason = "wrong_deployment"
	AuthorityDiscontinuous   AuthorityReason = "discontinuous"
	AuthorityOutage          AuthorityReason = "outage"
)

type AuthorityReport struct {
	Healthy bool
	Reason  AuthorityReason
}

type RecoveryAdapter interface {
	Kind() CapabilityKind
	Version() uint32
	Health(context.Context) error
}

type OrchestrationAdapter interface {
	Kind() CapabilityKind
	Version() uint32
	Health(context.Context) error
}

type Authority interface {
	Kind() CapabilityKind
	Probe(context.Context) (AuthorityReport, error)
}

type RegistryInput struct {
	DeletionAdapters []recorddeletion.Adapter
	RecoveryAdapters []RecoveryAdapter
	Membership       Authority
	Witness          Authority
	Backup           OrchestrationAdapter
	Restore          OrchestrationAdapter
}

type CapabilityStatus struct {
	kind    CapabilityKind
	family  string
	healthy bool
	reason  CapabilityReason
	version uint32
}

func (status CapabilityStatus) Kind() CapabilityKind { return status.kind }

func (status CapabilityStatus) Family() string { return status.family }

func (status CapabilityStatus) Healthy() bool { return status.healthy }

func (status CapabilityStatus) Reason() CapabilityReason { return status.reason }

func (status CapabilityStatus) Version() uint32 { return status.version }

type StatusMatrix struct {
	permanentDelete PermanentDeleteState
	rows            []CapabilityStatus
	digest          [sha256.Size]byte
}

func (matrix StatusMatrix) PermanentDelete() PermanentDeleteState {
	return matrix.permanentDelete
}

func (matrix StatusMatrix) Rows() []CapabilityStatus {
	cloned := make([]CapabilityStatus, len(matrix.rows))
	copy(cloned, matrix.rows)
	return cloned
}

func (matrix StatusMatrix) Missing() []CapabilityKind {
	missing := make([]CapabilityKind, 0)
	for _, row := range matrix.rows {
		if row.reason == CapabilityReasonMissing {
			missing = append(missing, row.kind)
		}
	}
	return missing
}

func (matrix StatusMatrix) Digest() [sha256.Size]byte {
	return matrix.digest
}

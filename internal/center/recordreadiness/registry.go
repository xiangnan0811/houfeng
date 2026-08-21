package recordreadiness

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"houfeng/internal/center/recorddeletion"
)

var knownRecoveryKinds = map[CapabilityKind]struct{}{
	CapabilityRecoveryRecordCore:               {},
	CapabilityRecoveryRecordAttachments:        {},
	CapabilityRecoveryRecordEvidence:           {},
	CapabilityRecoveryRecordSearch:             {},
	CapabilityRecoveryRecordActivityProjection: {},
	CapabilityRecoveryRecordCollaboration:      {},
	CapabilityRecoveryRecordPortability:        {},
}

type Registry struct {
	deletion   map[CapabilityKind]recorddeletion.Adapter
	recovery   map[CapabilityKind]RecoveryAdapter
	membership Authority
	witness    Authority
	backup     OrchestrationAdapter
	restore    OrchestrationAdapter
}

func NewRegistry(input RegistryInput) (*Registry, error) {
	if _, err := recorddeletion.NewRegistry(input.DeletionAdapters); err != nil {
		return nil, fmt.Errorf("%w: deletion: %w", ErrInvalidCapabilityRegistry, err)
	}

	recovery := make(map[CapabilityKind]RecoveryAdapter, len(input.RecoveryAdapters))
	for _, adapter := range input.RecoveryAdapters {
		if nilCapability(adapter) {
			return nil, fmt.Errorf("%w: nil recovery adapter", ErrInvalidCapabilityRegistry)
		}
		kind := adapter.Kind()
		if _, known := knownRecoveryKinds[kind]; !known {
			return nil, fmt.Errorf("%w: unknown recovery %q", ErrInvalidCapabilityRegistry, kind)
		}
		if adapter.Version() != CapabilityContractVersionV1 {
			return nil, fmt.Errorf("%w: incompatible recovery %q", ErrInvalidCapabilityRegistry, kind)
		}
		if _, exists := recovery[kind]; exists {
			return nil, fmt.Errorf("%w: duplicate recovery %q", ErrInvalidCapabilityRegistry, kind)
		}
		recovery[kind] = adapter
	}

	membership, err := validateAuthority(input.Membership, CapabilityAuthorityMembership)
	if err != nil {
		return nil, err
	}
	witness, err := validateAuthority(input.Witness, CapabilityAuthorityWitness)
	if err != nil {
		return nil, err
	}
	if nilCapability(membership) != nilCapability(witness) {
		return nil, fmt.Errorf("%w: membership and witness must be paired", ErrInvalidCapabilityRegistry)
	}

	backup, err := validateOrchestration(input.Backup, CapabilityBackupOrchestration)
	if err != nil {
		return nil, err
	}
	restore, err := validateOrchestration(input.Restore, CapabilityRestoreReplay)
	if err != nil {
		return nil, err
	}
	if nilCapability(backup) != nilCapability(restore) {
		return nil, fmt.Errorf("%w: backup and restore must be paired", ErrInvalidCapabilityRegistry)
	}

	deletion := make(map[CapabilityKind]recorddeletion.Adapter, len(input.DeletionAdapters))
	for _, adapter := range input.DeletionAdapters {
		deletion[DeletionCapabilityKind(adapter.Descriptor().Name())] = adapter
	}

	return &Registry{
		deletion:   deletion,
		recovery:   recovery,
		membership: membership,
		witness:    witness,
		backup:     backup,
		restore:    restore,
	}, nil
}

func validateAuthority(authority Authority, want CapabilityKind) (Authority, error) {
	if nilCapability(authority) {
		return nil, nil
	}
	if authority.Kind() != want {
		return nil, fmt.Errorf("%w: authority kind %q", ErrInvalidCapabilityRegistry, authority.Kind())
	}
	return authority, nil
}

func validateOrchestration(adapter OrchestrationAdapter, want CapabilityKind) (OrchestrationAdapter, error) {
	if nilCapability(adapter) {
		return nil, nil
	}
	if adapter.Kind() != want {
		return nil, fmt.Errorf("%w: orchestration kind %q", ErrInvalidCapabilityRegistry, adapter.Kind())
	}
	if adapter.Version() != CapabilityContractVersionV1 {
		return nil, fmt.Errorf("%w: incompatible orchestration %q", ErrInvalidCapabilityRegistry, want)
	}
	return adapter, nil
}

func (registry *Registry) Evaluate(ctx context.Context) (StatusMatrix, error) {
	if ctx == nil || registry == nil {
		return StatusMatrix{}, ErrReadinessUnavailable
	}

	rows := make([]CapabilityStatus, 0, len(requiredCapabilityKinds))
	enabled := true
	for _, kind := range requiredCapabilityKinds {
		row := registry.evaluateKind(ctx, kind)
		if !row.healthy || row.reason != CapabilityReasonPresent {
			enabled = false
		}
		rows = append(rows, row)
	}

	state := PermanentDeleteDisabled
	if enabled {
		state = PermanentDeleteEnabled
	}
	matrix := StatusMatrix{permanentDelete: state, rows: rows}
	encoded, err := matrix.Encode()
	if err != nil {
		return StatusMatrix{}, err
	}
	matrix.digest = sha256.Sum256(encoded)
	return matrix, nil
}

func (registry *Registry) evaluateKind(ctx context.Context, kind CapabilityKind) CapabilityStatus {
	family := familyOf(kind)
	switch family {
	case "deletion":
		adapter, ok := registry.deletion[kind]
		if !ok {
			return missingStatus(kind, family)
		}
		health, err := adapter.HealthSnapshot(ctx)
		if err != nil || health.Validate() != nil || !health.Healthy() {
			return CapabilityStatus{kind: kind, family: family, reason: CapabilityReasonUnhealthy, version: CapabilityContractVersionV1}
		}
		return presentStatus(kind, family, CapabilityContractVersionV1)
	case "recovery":
		adapter, ok := registry.recovery[kind]
		if !ok {
			return missingStatus(kind, family)
		}
		if err := adapter.Health(ctx); err != nil {
			return CapabilityStatus{kind: kind, family: family, reason: CapabilityReasonUnhealthy, version: adapter.Version()}
		}
		return presentStatus(kind, family, adapter.Version())
	case "authority":
		authority := registry.membership
		if kind == CapabilityAuthorityWitness {
			authority = registry.witness
		}
		if nilCapability(authority) {
			return missingStatus(kind, family)
		}
		report, err := authority.Probe(ctx)
		if err != nil || !report.Healthy || report.Reason != AuthorityOK {
			return CapabilityStatus{kind: kind, family: family, reason: CapabilityReasonClosed, version: CapabilityContractVersionV1}
		}
		return presentStatus(kind, family, CapabilityContractVersionV1)
	case "backup":
		if nilCapability(registry.backup) {
			return missingStatus(kind, family)
		}
		if err := registry.backup.Health(ctx); err != nil {
			return CapabilityStatus{kind: kind, family: family, reason: CapabilityReasonUnhealthy, version: registry.backup.Version()}
		}
		return presentStatus(kind, family, registry.backup.Version())
	case "restore":
		if nilCapability(registry.restore) {
			return missingStatus(kind, family)
		}
		if err := registry.restore.Health(ctx); err != nil {
			return CapabilityStatus{kind: kind, family: family, reason: CapabilityReasonUnhealthy, version: registry.restore.Version()}
		}
		return presentStatus(kind, family, registry.restore.Version())
	default:
		return missingStatus(kind, family)
	}
}

func missingStatus(kind CapabilityKind, family string) CapabilityStatus {
	return CapabilityStatus{kind: kind, family: family, reason: CapabilityReasonMissing}
}

func presentStatus(kind CapabilityKind, family string, version uint32) CapabilityStatus {
	return CapabilityStatus{
		kind:    kind,
		family:  family,
		healthy: true,
		reason:  CapabilityReasonPresent,
		version: version,
	}
}

func familyOf(kind CapabilityKind) string {
	text := string(kind)
	index := strings.IndexByte(text, '.')
	if index <= 0 {
		return ""
	}
	return text[:index]
}

func (matrix StatusMatrix) Encode() ([]byte, error) {
	payload := encodedStatusMatrix{
		PermanentDelete: matrix.permanentDelete,
		Rows:            make([]encodedCapabilityStatus, 0, len(matrix.rows)),
	}
	for _, row := range matrix.rows {
		payload.Rows = append(payload.Rows, encodedCapabilityStatus{
			Kind:    row.kind,
			Family:  row.family,
			Healthy: row.healthy,
			Reason:  row.reason,
			Version: row.version,
		})
	}
	return json.Marshal(payload)
}

type encodedStatusMatrix struct {
	PermanentDelete PermanentDeleteState      `json:"permanent_delete"`
	Rows            []encodedCapabilityStatus `json:"rows"`
}

type encodedCapabilityStatus struct {
	Kind    CapabilityKind   `json:"kind"`
	Family  string           `json:"family"`
	Healthy bool             `json:"healthy"`
	Reason  CapabilityReason `json:"reason"`
	Version uint32           `json:"version"`
}

func nilCapability(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

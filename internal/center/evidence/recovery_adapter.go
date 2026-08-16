package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

const EvidenceRecoveryContractVersionV1 uint64 = 1

var (
	ErrInvalidRecoveryAdapter   = errors.New("invalid evidence recovery adapter")
	ErrInvalidRecoveryInventory = errors.New("invalid evidence recovery inventory")
)

type EvidenceRecoveryPayload struct {
	Key              KindKey
	Digest           [sha256.Size]byte
	CanonicalPayload []byte
}

type EvidenceRecoverySnapshot struct {
	RecordID      string
	SnapshotID    string
	Envelope      SnapshotEnvelope
	PayloadDigest [sha256.Size]byte
}

type EvidenceRecoveryCaptureIntent = CaptureIntentBinding

type EvidenceReferenceRole string

const (
	EvidenceReferenceRoleEvidence        EvidenceReferenceRole = "evidence"
	EvidenceReferenceRoleContext         EvidenceReferenceRole = "context"
	EvidenceReferenceRoleDecisionSupport EvidenceReferenceRole = "decision_support"
)

type EvidenceRecoveryRevisionReference struct {
	RecordID      string
	RevisionID    string
	Ordinal       uint64
	SnapshotID    string
	Caption       string
	ReferenceRole EvidenceReferenceRole
}

type EvidenceRecoveryCopyLineage struct {
	SnapshotID           string
	CopiedFromSnapshotID string
	CopyReason           string
}

type EvidenceRecoveryInventory struct {
	Payloads           []EvidenceRecoveryPayload
	Snapshots          []EvidenceRecoverySnapshot
	CaptureIntents     []EvidenceRecoveryCaptureIntent
	RevisionReferences []EvidenceRecoveryRevisionReference
	CopyLineage        []EvidenceRecoveryCopyLineage
}

type EvidenceRecoveryRepository interface {
	RestoreEvidenceInventory(context.Context, EvidenceRecoveryInventory) error
	// CollectUnreferencedEvidencePayloads must use a global logical-snapshot
	// reference check. Record-local ownership is insufficient for deduplicated
	// payloads shared by a surviving explicit copy.
	CollectUnreferencedEvidencePayloads(context.Context) error
}

type RecoveryAdapter struct {
	registry   Registry
	repository EvidenceRecoveryRepository
}

func NewRecoveryAdapter(registry Registry, repository EvidenceRecoveryRepository) (*RecoveryAdapter, error) {
	if len(registry.kinds) == 0 || nilRevisionPreparationDependency(repository) {
		return nil, ErrInvalidRecoveryAdapter
	}
	return &RecoveryAdapter{registry: registry, repository: repository}, nil
}

func (adapter *RecoveryAdapter) ContractVersion() uint64 {
	return EvidenceRecoveryContractVersionV1
}

func (adapter *RecoveryAdapter) Replay(ctx context.Context, inventory EvidenceRecoveryInventory) error {
	if ctx == nil || adapter == nil || len(adapter.registry.kinds) == 0 ||
		nilRevisionPreparationDependency(adapter.repository) {
		return ErrInvalidRecoveryAdapter
	}
	if err := validateEvidenceRecoveryInventory(adapter.registry, inventory); err != nil {
		return err
	}
	if err := adapter.repository.RestoreEvidenceInventory(ctx, cloneEvidenceRecoveryInventory(inventory)); err != nil {
		return fmt.Errorf("restore evidence inventory: %w", err)
	}
	if err := adapter.repository.CollectUnreferencedEvidencePayloads(ctx); err != nil {
		return fmt.Errorf("collect globally unreferenced evidence payloads: %w", err)
	}
	return nil
}

func validateEvidenceRecoveryInventory(registry Registry, inventory EvidenceRecoveryInventory) error {
	if inventory.Payloads == nil || inventory.Snapshots == nil || inventory.CaptureIntents == nil ||
		inventory.RevisionReferences == nil || inventory.CopyLineage == nil {
		return ErrInvalidRecoveryInventory
	}
	payloads := make(map[[sha256.Size]byte]EvidenceRecoveryPayload, len(inventory.Payloads))
	for index, payload := range inventory.Payloads {
		kind, err := registry.LookupKey(payload.Key)
		if err != nil {
			return err
		}
		decoded, err := DecodeCanonicalPayload(kind.Descriptor(), payload.CanonicalPayload)
		if err != nil || decoded.Hash() != payload.Digest || payload.Digest == [sha256.Size]byte{} ||
			(index > 0 && bytes.Compare(inventory.Payloads[index-1].Digest[:], payload.Digest[:]) >= 0) {
			return ErrInvalidRecoveryInventory
		}
		payloads[payload.Digest] = payload
	}
	snapshots := make(map[string]EvidenceRecoverySnapshot, len(inventory.Snapshots))
	referencedPayloads := make(map[[sha256.Size]byte]struct{}, len(inventory.Snapshots))
	for index, snapshot := range inventory.Snapshots {
		if !validClosedPreparedID(snapshot.RecordID, "rec_") || !ValidSnapshotID(snapshot.SnapshotID) ||
			(index > 0 && inventory.Snapshots[index-1].SnapshotID >= snapshot.SnapshotID) ||
			snapshot.PayloadDigest != snapshot.Envelope.CanonicalHash ||
			!recoverySnapshotTimestampsCanonical(snapshot.Envelope) {
			return ErrInvalidRecoveryInventory
		}
		kind, err := registry.LookupKey(snapshot.Envelope.Key)
		if err != nil {
			return err
		}
		payload, exists := payloads[snapshot.PayloadDigest]
		if !exists || payload.Key != snapshot.Envelope.Key {
			return ErrInvalidRecoveryInventory
		}
		if _, err := RestoreCanonicalSnapshot(kind.Descriptor(), snapshot.Envelope, payload.CanonicalPayload); err != nil {
			return ErrInvalidRecoveryInventory
		}
		snapshots[snapshot.SnapshotID] = snapshot
		referencedPayloads[snapshot.PayloadDigest] = struct{}{}
	}
	// A recovery inventory contains authoritative logical evidence, not orphan
	// cache material. Requiring every replayed payload to back at least one
	// replayed snapshot keeps retries deterministic; the post-replay global GC
	// remains responsible for unrelated pre-existing orphans.
	if len(referencedPayloads) != len(payloads) {
		return ErrInvalidRecoveryInventory
	}
	for index, binding := range inventory.CaptureIntents {
		if binding.Validate() != nil || (index > 0 && inventory.CaptureIntents[index-1].Intent.ID >= binding.Intent.ID) {
			return ErrInvalidRecoveryInventory
		}
		if _, err := registry.LookupKey(binding.Intent.Key); err != nil {
			return err
		}
	}
	lastRevisionKey := ""
	var nextOrdinal uint64
	for index, reference := range inventory.RevisionReferences {
		if !validClosedPreparedID(reference.RecordID, "rec_") || !validClosedPreparedID(reference.RevisionID, "rrv_") ||
			!ValidSnapshotID(reference.SnapshotID) || !validEvidenceRecoveryReferenceRole(reference.ReferenceRole) ||
			len(reference.Caption) > 1024 || !utf8.ValidString(reference.Caption) ||
			(index > 0 && compareEvidenceRecoveryReference(inventory.RevisionReferences[index-1], reference) >= 0) {
			return ErrInvalidRecoveryInventory
		}
		revisionKey := reference.RecordID + "\x00" + reference.RevisionID
		if revisionKey != lastRevisionKey {
			lastRevisionKey, nextOrdinal = revisionKey, 0
		}
		if reference.Ordinal != nextOrdinal {
			return ErrInvalidRecoveryInventory
		}
		nextOrdinal++
		owned, exists := snapshots[reference.SnapshotID]
		if !exists || owned.RecordID != reference.RecordID {
			return ErrInvalidRecoveryInventory
		}
	}
	for index, lineage := range inventory.CopyLineage {
		if !ValidSnapshotID(lineage.SnapshotID) || !ValidSnapshotID(lineage.CopiedFromSnapshotID) ||
			lineage.SnapshotID == lineage.CopiedFromSnapshotID ||
			(index > 0 && inventory.CopyLineage[index-1].SnapshotID >= lineage.SnapshotID) {
			return ErrInvalidRecoveryInventory
		}
		if _, exists := snapshots[lineage.SnapshotID]; !exists {
			return ErrInvalidRecoveryInventory
		}
		// copied_from may intentionally be absent after its owner record is
		// purged; the surviving copy and its lineage remain authoritative.
	}
	return nil
}

func recoverySnapshotTimestampsCanonical(envelope SnapshotEnvelope) bool {
	values := []time.Time{
		envelope.RequestedWindow.Start,
		envelope.RequestedWindow.End,
		envelope.ActualWindow.Start,
		envelope.ActualWindow.End,
		envelope.ObservedAt,
		envelope.CapturedAt,
		envelope.ReferencedAt,
	}
	for _, value := range values {
		if value.IsZero() || value.Location() != time.UTC || value != value.Round(0) ||
			value.Nanosecond()%int(time.Microsecond) != 0 {
			return false
		}
	}
	return true
}

func validEvidenceRecoveryReferenceRole(role EvidenceReferenceRole) bool {
	return role == EvidenceReferenceRoleEvidence || role == EvidenceReferenceRoleContext ||
		role == EvidenceReferenceRoleDecisionSupport
}

func compareEvidenceRecoveryReference(left, right EvidenceRecoveryRevisionReference) int {
	leftKey := left.RecordID + "\x00" + left.RevisionID
	rightKey := right.RecordID + "\x00" + right.RevisionID
	if leftKey < rightKey {
		return -1
	}
	if leftKey > rightKey {
		return 1
	}
	if left.Ordinal < right.Ordinal {
		return -1
	}
	if left.Ordinal > right.Ordinal {
		return 1
	}
	return 0
}

func cloneEvidenceRecoveryInventory(inventory EvidenceRecoveryInventory) EvidenceRecoveryInventory {
	cloned := EvidenceRecoveryInventory{
		Payloads:           make([]EvidenceRecoveryPayload, len(inventory.Payloads)),
		Snapshots:          make([]EvidenceRecoverySnapshot, len(inventory.Snapshots)),
		CaptureIntents:     make([]EvidenceRecoveryCaptureIntent, len(inventory.CaptureIntents)),
		RevisionReferences: make([]EvidenceRecoveryRevisionReference, len(inventory.RevisionReferences)),
		CopyLineage:        make([]EvidenceRecoveryCopyLineage, len(inventory.CopyLineage)),
	}
	for index, snapshot := range inventory.Snapshots {
		cloned.Snapshots[index] = snapshot
		cloned.Snapshots[index].Envelope = cloneSnapshotEnvelope(snapshot.Envelope)
	}
	copy(cloned.RevisionReferences, inventory.RevisionReferences)
	copy(cloned.CopyLineage, inventory.CopyLineage)
	for index, payload := range inventory.Payloads {
		cloned.Payloads[index] = payload
		cloned.Payloads[index].CanonicalPayload = append([]byte(nil), payload.CanonicalPayload...)
	}
	for index, binding := range inventory.CaptureIntents {
		cloned.CaptureIntents[index] = cloneCaptureIntentBinding(binding)
	}
	return cloned
}

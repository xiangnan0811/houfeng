package recorddeletion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
)

type BackupInventoryAvailability string

const (
	BackupInventoryAvailable   BackupInventoryAvailability = "available"
	BackupInventoryUnavailable BackupInventoryAvailability = "unavailable"

	IssuerCompletionScopeOnlineOnly = "online_only"

	issuerPreviewBindingDomainV2 = "houfeng.record-deletion.issuer-preview.v2"
)

// BackupInventorySnapshot distinguishes a successfully enumerated, possibly
// empty inventory from an inventory that cannot be enumerated. Unavailable is
// an explicit provider result, never a conversion of a provider error to zero.
// It does not establish backup deletion or restore safety.
type BackupInventorySnapshot struct {
	Availability    BackupInventoryAvailability
	InventoryDigest *[32]byte
	ManagedBackup   *ManagedBackupSummary
}

func (snapshot BackupInventorySnapshot) Validate() error {
	switch snapshot.Availability {
	case BackupInventoryAvailable:
		if snapshot.InventoryDigest == nil || zeroDeletionDigest(*snapshot.InventoryDigest) ||
			snapshot.ManagedBackup == nil || snapshot.ManagedBackup.Validate() != nil {
			return fmt.Errorf("%w: available backup inventory", ErrDeletionSafetyUnavailable)
		}
	case BackupInventoryUnavailable:
		if snapshot.InventoryDigest != nil || snapshot.ManagedBackup != nil {
			return fmt.Errorf("%w: unavailable backup inventory carries values", ErrDeletionSafetyUnavailable)
		}
	default:
		return fmt.Errorf("%w: backup inventory availability", ErrDeletionSafetyUnavailable)
	}
	return nil
}

type DeletionBackupInventorySource interface {
	Snapshot(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error)
}

// UnavailableDeletionBackupInventorySource is the explicit production choice
// until a retained-backup enumerator exists. It does not pretend to enumerate
// an empty inventory, and must not be used as an error fallback for a provider.
type UnavailableDeletionBackupInventorySource struct{}

func (UnavailableDeletionBackupInventorySource) Snapshot(ctx context.Context, object recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
	if err := validateBackupInventoryRequest(ctx, object); err != nil {
		return BackupInventorySnapshot{}, err
	}
	return BackupInventorySnapshot{Availability: BackupInventoryUnavailable}, nil
}

// ReadDeletionBackupInventory is the validated provider boundary for a future
// issuer. Provider errors (including cancellation, identity, parse and database
// errors) are returned unchanged, never treated as explicit unavailability.
// Returned pointer values are owned by the caller rather than the provider.
func ReadDeletionBackupInventory(ctx context.Context, source DeletionBackupInventorySource, object recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
	if err := validateBackupInventoryRequest(ctx, object); err != nil {
		return BackupInventorySnapshot{}, err
	}
	if nilDeletionServiceDependency(source) {
		return BackupInventorySnapshot{}, fmt.Errorf("%w: backup inventory source", ErrDeletionSafetyUnavailable)
	}
	snapshot, err := source.Snapshot(ctx, object)
	if err != nil {
		return BackupInventorySnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return BackupInventorySnapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return BackupInventorySnapshot{}, err
	}
	if snapshot.InventoryDigest != nil {
		digest := *snapshot.InventoryDigest
		backup := *snapshot.ManagedBackup
		snapshot.InventoryDigest = &digest
		snapshot.ManagedBackup = &backup
	}
	return snapshot, nil
}

func validateBackupInventoryRequest(ctx context.Context, object recordplatform.ObjectRef) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil backup inventory context", ErrDeletionSafetyUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if object.Validate() != nil || object.ObjectKind != "record" || !recordidentity.IsValid(object.ObjectID) {
		return fmt.Errorf("%w: backup inventory object", ErrDeletionSafetyUnavailable)
	}
	return nil
}

type IssuerPreviewResult struct {
	ReservationID string
	Token         recordplatform.IssuedDeletionRequestTokenV1
	ExpiresAt     time.Time
	Summary       IssuerPreviewSummary
}

// DigestIssuerAdapterSnapshots uses the same framing as the registry with
// the fixed, validated descriptor for each required adapter. Backup inventory
// is supplied separately by its owning source.
func DigestIssuerAdapterSnapshots(health map[AdapterName]AdapterHealthSnapshot, previews map[AdapterName]AdapterPreviewSnapshot) (readiness [32]byte, preview [32]byte, summary IssuerPreviewSummary, err error) {
	if len(health) != len(requiredAdapterNames) {
		return readiness, preview, summary, fmt.Errorf("%w: incomplete adapter health", ErrDeletionSafetyUnavailable)
	}
	if len(previews) != len(requiredAdapterNames) {
		return readiness, preview, summary, fmt.Errorf("%w: incomplete adapter previews", ErrInvalidDeletionPreview)
	}
	snapshot := ReadinessSnapshot{ready: true, adapters: make([]AdapterReadinessSnapshot, 0, len(requiredAdapterNames))}
	for _, name := range requiredAdapterNames {
		item, ok := health[name]
		if !ok || item.validate() != nil || !item.Healthy() {
			return readiness, preview, summary, fmt.Errorf("%w: adapter %q health", ErrDeletionSafetyUnavailable, name)
		}
		descriptor, descriptorErr := issuerAdapterDescriptor(name)
		if descriptorErr != nil {
			return readiness, preview, summary, fmt.Errorf("%w: adapter %q descriptor: %w", ErrDeletionSafetyUnavailable, name, descriptorErr)
		}
		snapshot.adapters = append(snapshot.adapters, AdapterReadinessSnapshot{name: name, surfaces: descriptor.surfaces, health: item})
	}
	aggregate, aggregateErr := digestAdapterPreviewSnapshots(previews)
	if aggregateErr != nil {
		return readiness, preview, summary, aggregateErr
	}
	return digestReadinessSnapshot(snapshot), aggregate.digest, IssuerPreviewSummary{
		OnlinePurgeScopes: aggregate.onlinePurgeScopes,
		SurvivingCopies:   aggregate.survivingCopies,
		CompletionScope:   IssuerCompletionScopeOnlineOnly,
	}, nil
}

func issuerAdapterDescriptor(name AdapterName) (AdapterDescriptor, error) {
	var surfaces []SurfaceName
	switch name {
	case AdapterNameRecordCore:
		surfaces = recordCoreSurfaceNames
	case AdapterNameRecordAttachments:
		surfaces = recordAttachmentsSurfaceNames
	case AdapterNameRecordEvidence:
		surfaces = recordEvidenceSurfaceNames
	case AdapterNameRecordMarkdownClient:
		surfaces = recordMarkdownClientSurfaceNames
	case AdapterNameRecordSearch:
		surfaces = recordSearchSurfaceNames
	case AdapterNameRecordActivityProjection:
		surfaces = recordActivitySurfaceNames
	case AdapterNameRecordComparison:
		surfaces = recordComparisonSurfaceNames
	case AdapterNameRecordCollaboration:
		surfaces = recordCollaborationSurfaceNames
	case AdapterNameRecordPortability:
		surfaces = recordPortabilitySurfaceNames
	}
	return NewAdapterDescriptor(name, surfaces)
}

// IssuerPreviewSummary is the online-only V2 disclosure, not the legacy
// witness/ledger-backed PreviewSummary. It deliberately has no LedgerHealth.
type IssuerPreviewSummary struct {
	OnlinePurgeScopes []AdapterName
	SurvivingCopies   []SurvivingCopySummary
	BackupInventory   BackupInventorySnapshot
	CompletionScope   string
}

func (summary IssuerPreviewSummary) Validate() error {
	if err := summary.BackupInventory.Validate(); err != nil {
		return err
	}
	if summary.CompletionScope != IssuerCompletionScopeOnlineOnly ||
		!slices.Equal(summary.OnlinePurgeScopes, requiredAdapterNames) || summary.SurvivingCopies == nil {
		return fmt.Errorf("%w: issuer preview summary", ErrInvalidDeletionPreview)
	}
	lastAdapterIndex, lastKindIndex := -1, -1
	for _, surviving := range summary.SurvivingCopies {
		if err := surviving.Validate(); err != nil {
			return err
		}
		adapterIndex, _ := deletionAdapterNameIndex(surviving.Scope)
		kindIndex, _ := survivingCopyKindIndex(surviving.Kind)
		if adapterIndex < lastAdapterIndex || (adapterIndex == lastAdapterIndex && kindIndex <= lastKindIndex) {
			return fmt.Errorf("%w: issuer surviving copy order", ErrInvalidDeletionPreview)
		}
		lastAdapterIndex, lastKindIndex = adapterIndex, kindIndex
	}
	return nil
}

// MarshalJSON uses a separate internal evidence DTO. Neither this DTO nor the
// V2 inventory changes the disabled HTTP endpoint or its legacy managed-backup
// contract. Unknown inventory has explicit null values, never omitted or zero.
func (summary IssuerPreviewSummary) MarshalJSON() ([]byte, error) {
	if err := summary.Validate(); err != nil {
		return nil, err
	}
	copies := make([]issuerSurvivingCopyJSON, len(summary.SurvivingCopies))
	for index, surviving := range summary.SurvivingCopies {
		copies[index] = issuerSurvivingCopyJSON{
			Scope: surviving.Scope, Kind: surviving.Kind,
			CountStatus: surviving.CountStatus, CopyCount: surviving.CopyCount,
		}
	}
	disclosure := "This operation clears online data only; it does not promise deletion of backups."
	if summary.BackupInventory.Availability == BackupInventoryUnavailable {
		disclosure = "Backup inventory is unknown. " + disclosure
	}
	return json.Marshal(issuerPreviewSummaryJSON{
		OnlinePurgeScopes: summary.OnlinePurgeScopes,
		SurvivingCopies:   copies,
		BackupInventory:   summary.BackupInventory.jsonDTO(),
		CompletionScope:   summary.CompletionScope,
		Disclosure:        disclosure,
	})
}

func (snapshot BackupInventorySnapshot) MarshalJSON() ([]byte, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(snapshot.jsonDTO())
}

type issuerPreviewSummaryJSON struct {
	OnlinePurgeScopes []AdapterName             `json:"online_purge_scopes"`
	SurvivingCopies   []issuerSurvivingCopyJSON `json:"surviving_copies"`
	BackupInventory   backupInventoryJSON       `json:"backup_inventory"`
	CompletionScope   string                    `json:"completion_scope"`
	Disclosure        string                    `json:"disclosure"`
}

type issuerSurvivingCopyJSON struct {
	Scope       AdapterName              `json:"scope"`
	Kind        SurvivingCopyKind        `json:"kind"`
	CountStatus SurvivingCopyCountStatus `json:"count_status"`
	CopyCount   *uint64                  `json:"copy_count"`
}

type backupInventoryJSON struct {
	Availability    BackupInventoryAvailability `json:"availability"`
	InventoryDigest *string                     `json:"inventory_digest"`
	ManagedBackup   *issuerManagedBackupJSON    `json:"managed_backup"`
}

type issuerManagedBackupJSON struct {
	RetainedCopyCount    uint64     `json:"retained_copy_count"`
	MaximumRetentionDays uint32     `json:"maximum_retention_days"`
	LatestExpiresAt      *time.Time `json:"latest_expires_at"`
}

// jsonDTO is only called after validation at a public marshaling boundary.
func (snapshot BackupInventorySnapshot) jsonDTO() backupInventoryJSON {
	dto := backupInventoryJSON{Availability: snapshot.Availability}
	if snapshot.Availability == BackupInventoryAvailable {
		digest := hex.EncodeToString(snapshot.InventoryDigest[:])
		dto.InventoryDigest = &digest
		backup := snapshot.ManagedBackup
		dto.ManagedBackup = &issuerManagedBackupJSON{
			RetainedCopyCount: backup.RetainedCopyCount, MaximumRetentionDays: backup.MaximumRetentionDays,
		}
		if !backup.LatestExpiresAt.IsZero() {
			expiry := backup.LatestExpiresAt.UTC()
			dto.ManagedBackup.LatestExpiresAt = &expiry
		}
	}
	return dto
}

// IssuerPreviewBinding contains only the facts committed by a V2 preview.
// Digests must come from their real owning inventories. Hashing these facts
// neither verifies source provenance nor proves adapter readiness: the true
// issuer still needs transactional authorization, Registry.RequireReady and
// atomic preview/gate persistence. This contract performs no I/O or issuance.
// Lifecycle is restricted to the two states from which V2 deletion is allowed.
type IssuerPreviewBinding struct {
	Actor                     recordauth.ActorScope
	Object                    recordplatform.ObjectRef
	Lifecycle                 string
	CurrentRevisionID         string
	LockVersion               uint64
	AuthorizationEpoch        uint64
	ContentDeliveryEpoch      recordplatform.ContentEpoch
	Authorization             recordauth.ResourceScope
	AdapterReadinessDigest    [sha256.Size]byte
	AdapterPreviewDigest      [sha256.Size]byte
	DependencyGraphDigest     [sha256.Size]byte
	ProcessorInventoryDigest  [sha256.Size]byte
	ObligationInventoryDigest [sha256.Size]byte
	BackupInventory           BackupInventorySnapshot
}

func (binding IssuerPreviewBinding) Validate() error {
	if binding.Object.Validate() != nil || binding.Object.ObjectKind != "record" ||
		!recordidentity.IsValid(binding.Object.ObjectID) || !validDeletionRevisionID(binding.CurrentRevisionID) ||
		(binding.Lifecycle != "active" && binding.Lifecycle != "trashed") ||
		binding.LockVersion == 0 || binding.AuthorizationEpoch == 0 ||
		zeroDeletionDigest(binding.AdapterReadinessDigest) || zeroDeletionDigest(binding.AdapterPreviewDigest) ||
		zeroDeletionDigest(binding.DependencyGraphDigest) || zeroDeletionDigest(binding.ProcessorInventoryDigest) ||
		zeroDeletionDigest(binding.ObligationInventoryDigest) {
		return fmt.Errorf("%w: issuer preview binding", ErrInvalidDeletionPreview)
	}
	if err := binding.BackupInventory.Validate(); err != nil {
		return err
	}
	if string(binding.Actor.ProjectID) != binding.Object.ProjectID ||
		string(binding.Authorization.ProjectID) != binding.Object.ProjectID {
		return fmt.Errorf("%w: issuer preview project", ErrInvalidDeletionPreview)
	}
	return recordauth.Authorize(binding.Actor, recordauth.CapabilityRecordPermanentDelete, binding.Authorization)
}

// DigestIssuerPreviewBinding uses the existing big-endian uint64 and
// length-prefixed string framing and canonical actor/authorization hashes.
// Inventory availability is encoded only after all online facts: 1 followed by
// digest/count/days/optional UTC UnixNano expiry, or 2 with no backup values.
// The final field is always the fixed online_only completion scope.
func DigestIssuerPreviewBinding(binding IssuerPreviewBinding) ([sha256.Size]byte, error) {
	if err := binding.Validate(); err != nil {
		return [sha256.Size]byte{}, err
	}
	payload := make([]byte, 0, 768)
	payload = appendLengthPrefixed(payload, issuerPreviewBindingDomainV2)
	payload = appendUint64(payload, 2)
	actorDigest := binding.Actor.CanonicalHash()
	payload = append(payload, actorDigest[:]...)
	payload = appendLengthPrefixed(payload, binding.Object.ProjectID)
	payload = appendLengthPrefixed(payload, binding.Object.ObjectKind)
	payload = appendLengthPrefixed(payload, binding.Object.ObjectID)
	payload = appendLengthPrefixed(payload, string(binding.Lifecycle))
	payload = appendLengthPrefixed(payload, binding.CurrentRevisionID)
	payload = appendUint64(payload, binding.LockVersion)
	payload = appendUint64(payload, binding.AuthorizationEpoch)
	payload = appendUint64(payload, uint64(binding.ContentDeliveryEpoch))
	authorizationDigest := digestDeletionAuthorization(binding.Authorization)
	payload = append(payload, authorizationDigest[:]...)
	payload = append(payload, binding.AdapterReadinessDigest[:]...)
	payload = append(payload, binding.AdapterPreviewDigest[:]...)
	payload = append(payload, binding.DependencyGraphDigest[:]...)
	payload = append(payload, binding.ProcessorInventoryDigest[:]...)
	payload = append(payload, binding.ObligationInventoryDigest[:]...)
	if binding.BackupInventory.Availability == BackupInventoryAvailable {
		payload = append(payload, 1)
		payload = append(payload, binding.BackupInventory.InventoryDigest[:]...)
		backup := binding.BackupInventory.ManagedBackup
		payload = appendUint64(payload, backup.RetainedCopyCount)
		payload = appendUint64(payload, uint64(backup.MaximumRetentionDays))
		if backup.LatestExpiresAt.IsZero() {
			payload = append(payload, 0)
		} else {
			payload = append(payload, 1)
			payload = appendUint64(payload, uint64(backup.LatestExpiresAt.UTC().UnixNano()))
		}
	} else {
		payload = append(payload, 2)
	}
	payload = appendLengthPrefixed(payload, IssuerCompletionScopeOnlineOnly)
	return sha256.Sum256(payload), nil
}

// CheckIssuerPreviewBinding compares a freshly recomputed V2 binding to its
// stored preview digest. Drift is stale, not a new authorization or a V1 retry.
// This pure comparison is not SQL admission and cannot create an operation.
func CheckIssuerPreviewBinding(expected [sha256.Size]byte, current IssuerPreviewBinding) error {
	if zeroDeletionDigest(expected) {
		return fmt.Errorf("%w: empty issuer preview commitment", ErrInvalidDeletionPreview)
	}
	actual, err := DigestIssuerPreviewBinding(current)
	if err != nil {
		return err
	}
	if expected != actual {
		return ErrDeletionPreviewStale
	}
	return nil
}

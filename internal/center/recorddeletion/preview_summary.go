package recorddeletion

import (
	"fmt"
	"slices"
	"time"
)

type SurvivingCopyKind string

const (
	SurvivingCopyKindOtherRecord              SurvivingCopyKind = "other_record"
	SurvivingCopyKindDeliveredExport          SurvivingCopyKind = "delivered_export"
	SurvivingCopyKindDeliveredNotification    SurvivingCopyKind = "delivered_notification"
	SurvivingCopyKindOfflineBrowserBuffer     SurvivingCopyKind = "offline_browser_buffer"
	SurvivingCopyKindPossibleExternalDelivery SurvivingCopyKind = "possible_external_delivery"
)

// SurvivingCopyCountStatus states whether an owning adapter can count a
// surviving copy class. Unknown is a closed, owner-restricted disclosure and
// never stands in for a zero or one count.
type SurvivingCopyCountStatus string

const (
	SurvivingCopyCountKnown   SurvivingCopyCountStatus = "known"
	SurvivingCopyCountUnknown SurvivingCopyCountStatus = "unknown"
)

// AdapterSurvivingCopy is an identity-free count owned by one deletion adapter.
// Known copies carry a positive count; unknown copies carry no count.
type AdapterSurvivingCopy struct {
	Kind        SurvivingCopyKind
	CountStatus SurvivingCopyCountStatus
	CopyCount   *uint64
}

// KnownAdapterSurvivingCopy returns a known surviving copy that owns its count.
func KnownAdapterSurvivingCopy(kind SurvivingCopyKind, count uint64) AdapterSurvivingCopy {
	return AdapterSurvivingCopy{Kind: kind, CountStatus: SurvivingCopyCountKnown, CopyCount: &count}
}

// UnknownAdapterSurvivingCopy returns a surviving copy whose count cannot be
// enumerated by its owning adapter.
func UnknownAdapterSurvivingCopy(kind SurvivingCopyKind) AdapterSurvivingCopy {
	return AdapterSurvivingCopy{Kind: kind, CountStatus: SurvivingCopyCountUnknown}
}

// SurvivingCopySummary adds the stable owning scope used by the transport.
type SurvivingCopySummary struct {
	Scope       AdapterName
	Kind        SurvivingCopyKind
	CountStatus SurvivingCopyCountStatus
	CopyCount   *uint64
}

// KnownSurvivingCopySummary returns a known summary entry that owns its count.
func KnownSurvivingCopySummary(scope AdapterName, kind SurvivingCopyKind, count uint64) SurvivingCopySummary {
	return SurvivingCopySummary{Scope: scope, Kind: kind, CountStatus: SurvivingCopyCountKnown, CopyCount: &count}
}

// UnknownSurvivingCopySummary returns an unknown-count summary entry.
func UnknownSurvivingCopySummary(scope AdapterName, kind SurvivingCopyKind) SurvivingCopySummary {
	return SurvivingCopySummary{Scope: scope, Kind: kind, CountStatus: SurvivingCopyCountUnknown}
}

// Validate checks one owner-scoped surviving copy against the closed scope,
// kind and count-status contract.
func (surviving SurvivingCopySummary) Validate() error {
	if _, ok := deletionAdapterNameIndex(surviving.Scope); !ok {
		return fmt.Errorf("%w: surviving copy scope", ErrInvalidDeletionPreview)
	}
	if _, ok := survivingCopyKindIndex(surviving.Kind); !ok {
		return fmt.Errorf("%w: surviving copy kind", ErrInvalidDeletionPreview)
	}
	if !validSurvivingCopyCount(surviving.Kind, surviving.CountStatus, surviving.CopyCount) ||
		(surviving.CountStatus == SurvivingCopyCountUnknown && !unknownSurvivingCopyOwner(surviving.Scope, surviving.Kind)) {
		return fmt.Errorf("%w: surviving copy count", ErrInvalidDeletionPreview)
	}
	return nil
}

func (surviving SurvivingCopySummary) clone() SurvivingCopySummary {
	surviving.CopyCount = cloneSurvivingCopyCount(surviving.CopyCount)
	return surviving
}

type ManagedBackupSummary struct {
	RetainedCopyCount    uint64
	MaximumRetentionDays uint32
	LatestExpiresAt      time.Time
}

func (summary ManagedBackupSummary) Validate() error {
	if summary.RetainedCopyCount == 0 {
		if !summary.LatestExpiresAt.IsZero() {
			return fmt.Errorf("%w: backup expiry without retained copy", ErrInvalidDeletionPreview)
		}
		return nil
	}
	if summary.MaximumRetentionDays == 0 || summary.LatestExpiresAt.IsZero() {
		return fmt.Errorf("%w: incomplete managed backup summary", ErrInvalidDeletionPreview)
	}
	return nil
}

type LedgerHealth string

const LedgerHealthHealthy LedgerHealth = "healthy"

type PreviewSummary struct {
	OnlinePurgeScopes []AdapterName
	SurvivingCopies   []SurvivingCopySummary
	ManagedBackup     ManagedBackupSummary
	LedgerHealth      LedgerHealth
}

func (summary PreviewSummary) Validate() error {
	if !slices.Equal(summary.OnlinePurgeScopes, requiredAdapterNames) || summary.SurvivingCopies == nil ||
		summary.ManagedBackup.Validate() != nil || summary.LedgerHealth != LedgerHealthHealthy {
		return fmt.Errorf("%w: preview summary", ErrInvalidDeletionPreview)
	}

	lastAdapterIndex := -1
	lastKindIndex := -1
	for _, surviving := range summary.SurvivingCopies {
		if surviving.Validate() != nil {
			return fmt.Errorf("%w: surviving copy summary", ErrInvalidDeletionPreview)
		}
		adapterIndex, _ := deletionAdapterNameIndex(surviving.Scope)
		kindIndex, _ := survivingCopyKindIndex(surviving.Kind)
		if adapterIndex < lastAdapterIndex || (adapterIndex == lastAdapterIndex && kindIndex <= lastKindIndex) {
			return fmt.Errorf("%w: surviving copy summary", ErrInvalidDeletionPreview)
		}
		if adapterIndex != lastAdapterIndex {
			lastKindIndex = -1
		}
		lastAdapterIndex = adapterIndex
		lastKindIndex = kindIndex
	}
	return nil
}

func (summary PreviewSummary) clone() PreviewSummary {
	var survivingCopies []SurvivingCopySummary
	if summary.SurvivingCopies != nil {
		survivingCopies = make([]SurvivingCopySummary, len(summary.SurvivingCopies))
		for index, surviving := range summary.SurvivingCopies {
			survivingCopies[index] = surviving.clone()
		}
	}
	return PreviewSummary{
		OnlinePurgeScopes: append([]AdapterName(nil), summary.OnlinePurgeScopes...),
		SurvivingCopies:   survivingCopies,
		ManagedBackup:     summary.ManagedBackup,
		LedgerHealth:      summary.LedgerHealth,
	}
}

// validateAdapterSurvivingCopies checks one adapter snapshot. It can only
// check kind-level rules; the owning scope of unknown counts is enforced by
// the registry and by SurvivingCopySummary.Validate.
func validateAdapterSurvivingCopies(copies []AdapterSurvivingCopy) error {
	if copies == nil {
		return fmt.Errorf("%w: nil surviving copy snapshot", ErrInvalidDeletionPreview)
	}
	lastKindIndex := -1
	for _, surviving := range copies {
		kindIndex, ok := survivingCopyKindIndex(surviving.Kind)
		if !ok || !validSurvivingCopyCount(surviving.Kind, surviving.CountStatus, surviving.CopyCount) ||
			kindIndex <= lastKindIndex {
			return fmt.Errorf("%w: adapter surviving copy snapshot", ErrInvalidDeletionPreview)
		}
		lastKindIndex = kindIndex
	}
	return nil
}

func validSurvivingCopyCount(kind SurvivingCopyKind, status SurvivingCopyCountStatus, count *uint64) bool {
	switch status {
	case SurvivingCopyCountKnown:
		return count != nil && *count > 0
	case SurvivingCopyCountUnknown:
		return count == nil && kind == SurvivingCopyKindOfflineBrowserBuffer
	default:
		return false
	}
}

// unknownSurvivingCopyOwner is the only scope/kind pair whose copies cannot
// be enumerated: offline browser buffers owned by the Markdown client adapter.
func unknownSurvivingCopyOwner(scope AdapterName, kind SurvivingCopyKind) bool {
	return scope == AdapterNameRecordMarkdownClient && kind == SurvivingCopyKindOfflineBrowserBuffer
}

func cloneSurvivingCopyCount(count *uint64) *uint64 {
	if count == nil {
		return nil
	}
	owned := *count
	return &owned
}

func deletionAdapterNameIndex(name AdapterName) (int, bool) {
	for index, candidate := range requiredAdapterNames {
		if candidate == name {
			return index, true
		}
	}
	return 0, false
}

func survivingCopyKindIndex(kind SurvivingCopyKind) (int, bool) {
	switch kind {
	case SurvivingCopyKindOtherRecord:
		return 0, true
	case SurvivingCopyKindDeliveredExport:
		return 1, true
	case SurvivingCopyKindDeliveredNotification:
		return 2, true
	case SurvivingCopyKindOfflineBrowserBuffer:
		return 3, true
	case SurvivingCopyKindPossibleExternalDelivery:
		return 4, true
	default:
		return 0, false
	}
}

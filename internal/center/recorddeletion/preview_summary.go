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

// AdapterSurvivingCopy is an identity-free count owned by one deletion adapter.
type AdapterSurvivingCopy struct {
	Kind      SurvivingCopyKind
	CopyCount uint64
}

// SurvivingCopySummary adds the stable owning scope used by the transport.
type SurvivingCopySummary struct {
	Scope     AdapterName
	Kind      SurvivingCopyKind
	CopyCount uint64
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
		adapterIndex, adapterOK := deletionAdapterNameIndex(surviving.Scope)
		kindIndex, kindOK := survivingCopyKindIndex(surviving.Kind)
		if !adapterOK || !kindOK || surviving.CopyCount == 0 || adapterIndex < lastAdapterIndex ||
			(adapterIndex == lastAdapterIndex && kindIndex <= lastKindIndex) {
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
	return PreviewSummary{
		OnlinePurgeScopes: append([]AdapterName(nil), summary.OnlinePurgeScopes...),
		SurvivingCopies:   append([]SurvivingCopySummary(nil), summary.SurvivingCopies...),
		ManagedBackup:     summary.ManagedBackup,
		LedgerHealth:      summary.LedgerHealth,
	}
}

func validateAdapterSurvivingCopies(copies []AdapterSurvivingCopy) error {
	if copies == nil {
		return fmt.Errorf("%w: nil surviving copy snapshot", ErrInvalidDeletionPreview)
	}
	lastKindIndex := -1
	for _, surviving := range copies {
		kindIndex, ok := survivingCopyKindIndex(surviving.Kind)
		if !ok || surviving.CopyCount == 0 || kindIndex <= lastKindIndex {
			return fmt.Errorf("%w: adapter surviving copy snapshot", ErrInvalidDeletionPreview)
		}
		lastKindIndex = kindIndex
	}
	return nil
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

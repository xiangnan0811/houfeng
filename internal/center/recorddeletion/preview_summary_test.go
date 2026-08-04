package recorddeletion

import (
	"errors"
	"testing"
)

func TestAdapterPreviewSnapshotRejectsInvalidSurvivingCopies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		copies []AdapterSurvivingCopy
	}{
		{name: "nil", copies: nil},
		{name: "unknown kind", copies: []AdapterSurvivingCopy{{Kind: SurvivingCopyKind("unknown"), CopyCount: 1}}},
		{name: "zero count", copies: []AdapterSurvivingCopy{{Kind: SurvivingCopyKindOtherRecord}}},
		{name: "unordered", copies: []AdapterSurvivingCopy{
			{Kind: SurvivingCopyKindDeliveredExport, CopyCount: 1},
			{Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
		}},
		{name: "duplicate", copies: []AdapterSurvivingCopy{
			{Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
			{Kind: SurvivingCopyKindOtherRecord, CopyCount: 2},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := AdapterPreviewSnapshot{
				DependencyDigest: deletionTestDigest(1),
				ImpactDigest:     deletionTestDigest(2),
				SurvivingCopies:  tt.copies,
			}

			if err := snapshot.Validate(); !errors.Is(err, ErrInvalidDeletionPreview) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDeletionPreview", err)
			}
		})
	}
}

func TestPreviewSummaryRejectsInvalidOrUnorderedSurvivingCopies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		copies []SurvivingCopySummary
	}{
		{name: "nil", copies: nil},
		{name: "unknown scope", copies: []SurvivingCopySummary{{
			Scope: AdapterName("unknown"), Kind: SurvivingCopyKindOtherRecord, CopyCount: 1,
		}}},
		{name: "unknown kind", copies: []SurvivingCopySummary{{
			Scope: AdapterNameRecordCore, Kind: SurvivingCopyKind("unknown"), CopyCount: 1,
		}}},
		{name: "zero count", copies: []SurvivingCopySummary{{
			Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord,
		}}},
		{name: "unordered scopes", copies: []SurvivingCopySummary{
			{Scope: AdapterNameRecordAttachments, Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
			{Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
		}},
		{name: "unordered kinds", copies: []SurvivingCopySummary{
			{Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindDeliveredExport, CopyCount: 1},
			{Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
		}},
		{name: "duplicate", copies: []SurvivingCopySummary{
			{Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
			{Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord, CopyCount: 2},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := PreviewSummary{
				OnlinePurgeScopes: RequiredAdapterNames(),
				SurvivingCopies:   tt.copies,
				LedgerHealth:      LedgerHealthHealthy,
			}

			if err := summary.Validate(); !errors.Is(err, ErrInvalidDeletionPreview) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDeletionPreview", err)
			}
		})
	}
}

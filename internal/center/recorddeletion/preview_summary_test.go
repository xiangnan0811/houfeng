package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"

	"houfeng/internal/center/recordplatform"
)

func TestAdapterPreviewSnapshotRejectsInvalidSurvivingCopies(t *testing.T) {
	t.Parallel()

	zero := uint64(0)
	one := uint64(1)
	tests := []struct {
		name   string
		copies []AdapterSurvivingCopy
	}{
		{name: "nil", copies: nil},
		{name: "unknown kind", copies: []AdapterSurvivingCopy{KnownAdapterSurvivingCopy(SurvivingCopyKind("unknown"), 1)}},
		{name: "known zero count", copies: []AdapterSurvivingCopy{KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 0)}},
		{name: "known nil count", copies: []AdapterSurvivingCopy{{
			Kind: SurvivingCopyKindOtherRecord, CountStatus: SurvivingCopyCountKnown,
		}}},
		{name: "missing status", copies: []AdapterSurvivingCopy{{
			Kind: SurvivingCopyKindOtherRecord, CopyCount: &one,
		}}},
		{name: "unrecognized status", copies: []AdapterSurvivingCopy{{
			Kind: SurvivingCopyKindOtherRecord, CountStatus: SurvivingCopyCountStatus("estimated"), CopyCount: &one,
		}}},
		{name: "unknown with count", copies: []AdapterSurvivingCopy{{
			Kind: SurvivingCopyKindOfflineBrowserBuffer, CountStatus: SurvivingCopyCountUnknown, CopyCount: &one,
		}}},
		{name: "unknown with zero count", copies: []AdapterSurvivingCopy{{
			Kind: SurvivingCopyKindOfflineBrowserBuffer, CountStatus: SurvivingCopyCountUnknown, CopyCount: &zero,
		}}},
		{name: "unknown for countable kind", copies: []AdapterSurvivingCopy{UnknownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord)}},
		{name: "unordered", copies: []AdapterSurvivingCopy{
			KnownAdapterSurvivingCopy(SurvivingCopyKindDeliveredExport, 1),
			KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 1),
		}},
		{name: "duplicate", copies: []AdapterSurvivingCopy{
			KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 1),
			KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 2),
		}},
		{name: "duplicate across count status", copies: []AdapterSurvivingCopy{
			KnownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer, 1),
			UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer),
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

func TestAdapterPreviewSnapshotAcceptsKnownAndUncountableOfflineBuffer(t *testing.T) {
	t.Parallel()

	snapshot := AdapterPreviewSnapshot{
		DependencyDigest: deletionTestDigest(1),
		ImpactDigest:     deletionTestDigest(2),
		SurvivingCopies: []AdapterSurvivingCopy{
			KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 3),
			UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer),
		},
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPreviewSummaryRejectsInvalidOrUnorderedSurvivingCopies(t *testing.T) {
	t.Parallel()

	zero := uint64(0)
	one := uint64(1)
	tests := []struct {
		name   string
		copies []SurvivingCopySummary
	}{
		{name: "nil", copies: nil},
		{name: "unknown scope", copies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterName("unknown"), SurvivingCopyKindOtherRecord, 1),
		}},
		{name: "unknown kind", copies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKind("unknown"), 1),
		}},
		{name: "known zero count", copies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKindOtherRecord, 0),
		}},
		{name: "known nil count", copies: []SurvivingCopySummary{{
			Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord, CountStatus: SurvivingCopyCountKnown,
		}}},
		{name: "missing status", copies: []SurvivingCopySummary{{
			Scope: AdapterNameRecordCore, Kind: SurvivingCopyKindOtherRecord, CopyCount: &one,
		}}},
		{name: "unknown with count", copies: []SurvivingCopySummary{{
			Scope: AdapterNameRecordMarkdownClient, Kind: SurvivingCopyKindOfflineBrowserBuffer,
			CountStatus: SurvivingCopyCountUnknown, CopyCount: &one,
		}}},
		{name: "unknown with zero count", copies: []SurvivingCopySummary{{
			Scope: AdapterNameRecordMarkdownClient, Kind: SurvivingCopyKindOfflineBrowserBuffer,
			CountStatus: SurvivingCopyCountUnknown, CopyCount: &zero,
		}}},
		{name: "unknown wrong owner", copies: []SurvivingCopySummary{
			UnknownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOfflineBrowserBuffer),
		}},
		{name: "unknown wrong kind for owner", copies: []SurvivingCopySummary{
			UnknownSurvivingCopySummary(AdapterNameRecordMarkdownClient, SurvivingCopyKindOtherRecord),
		}},
		{name: "unordered scopes", copies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOtherRecord, 1),
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKindOtherRecord, 1),
		}},
		{name: "unordered kinds", copies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKindDeliveredExport, 1),
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKindOtherRecord, 1),
		}},
		{name: "duplicate", copies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKindOtherRecord, 1),
			KnownSurvivingCopySummary(AdapterNameRecordCore, SurvivingCopyKindOtherRecord, 2),
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

func TestPreviewSummaryAcceptsOwnedUnknownOfflineBuffer(t *testing.T) {
	t.Parallel()

	summary := PreviewSummary{
		OnlinePurgeScopes: RequiredAdapterNames(),
		SurvivingCopies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOtherRecord, 2),
			UnknownSurvivingCopySummary(AdapterNameRecordMarkdownClient, SurvivingCopyKindOfflineBrowserBuffer),
		},
		LedgerHealth: LedgerHealthHealthy,
	}
	if err := summary.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPreviewSummaryCloneOwnsCopyCounts(t *testing.T) {
	t.Parallel()

	summary := PreviewSummary{
		OnlinePurgeScopes: RequiredAdapterNames(),
		SurvivingCopies: []SurvivingCopySummary{
			KnownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOtherRecord, 2),
			UnknownSurvivingCopySummary(AdapterNameRecordMarkdownClient, SurvivingCopyKindOfflineBrowserBuffer),
		},
		LedgerHealth: LedgerHealthHealthy,
	}
	cloned := summary.clone()
	*summary.SurvivingCopies[0].CopyCount = 99
	summary.SurvivingCopies[1].CountStatus = SurvivingCopyCountKnown

	if cloned.SurvivingCopies[0].CopyCount == summary.SurvivingCopies[0].CopyCount ||
		*cloned.SurvivingCopies[0].CopyCount != 2 ||
		cloned.SurvivingCopies[1].CountStatus != SurvivingCopyCountUnknown || cloned.SurvivingCopies[1].CopyCount != nil {
		t.Fatalf("clone() aliases surviving copy counts: %#v", cloned.SurvivingCopies)
	}
	empty := PreviewSummary{SurvivingCopies: []SurvivingCopySummary{}}.clone()
	if empty.SurvivingCopies == nil || len(empty.SurvivingCopies) != 0 {
		t.Fatalf("clone() empty survivors = %#v, want non-nil empty", empty.SurvivingCopies)
	}
}

func TestRegistryPreviewEnforcesUnknownSurvivingCopyOwner(t *testing.T) {
	t.Parallel()

	t.Run("wrong owner", func(t *testing.T) {
		registry, adapters := deletionTestRegistry(t)
		adapters[1].preview.SurvivingCopies = []AdapterSurvivingCopy{
			UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer),
		}
		if err := adapters[1].preview.Validate(); err != nil {
			t.Fatalf("adapter snapshot Validate() error = %v, want kind-level acceptance", err)
		}

		_, err := registry.previewSnapshot(context.Background(), deletionTestPreviewTarget(t))
		if !errors.Is(err, ErrDeletionSafetyUnavailable) {
			t.Fatalf("previewSnapshot() error = %v, want ErrDeletionSafetyUnavailable", err)
		}
	})

	t.Run("markdown client owner", func(t *testing.T) {
		registry, adapters := deletionTestRegistry(t)
		markdown := deletionTestAdapterStub(t, adapters, AdapterNameRecordMarkdownClient)
		markdown.preview.SurvivingCopies = []AdapterSurvivingCopy{
			UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer),
		}

		aggregate, err := registry.previewSnapshot(context.Background(), deletionTestPreviewTarget(t))
		if err != nil {
			t.Fatalf("previewSnapshot() error = %v", err)
		}
		want := []SurvivingCopySummary{
			UnknownSurvivingCopySummary(AdapterNameRecordMarkdownClient, SurvivingCopyKindOfflineBrowserBuffer),
		}
		if !reflect.DeepEqual(aggregate.survivingCopies, want) || aggregate.survivingCopies[0].CopyCount != nil {
			t.Fatalf("previewSnapshot() survivors = %#v, want %#v", aggregate.survivingCopies, want)
		}
	})
}

func TestRegistryPreviewCommitmentDistinguishesCountStatusAndOwnsCounts(t *testing.T) {
	t.Parallel()

	digestFor := func(t *testing.T, copies []AdapterSurvivingCopy) adapterPreviewAggregate {
		t.Helper()
		registry, adapters := deletionTestRegistry(t)
		deletionTestAdapterStub(t, adapters, AdapterNameRecordMarkdownClient).preview.SurvivingCopies = copies
		aggregate, err := registry.previewSnapshot(context.Background(), deletionTestPreviewTarget(t))
		if err != nil {
			t.Fatalf("previewSnapshot() error = %v", err)
		}
		return aggregate
	}

	unknown := digestFor(t, []AdapterSurvivingCopy{UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer)})
	knownOne := digestFor(t, []AdapterSurvivingCopy{KnownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer, 1)})
	knownTwo := digestFor(t, []AdapterSurvivingCopy{KnownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer, 2)})
	none := digestFor(t, []AdapterSurvivingCopy{})
	digests := map[[sha256.Size]byte]string{}
	for name, aggregate := range map[string]adapterPreviewAggregate{
		"unknown": unknown, "known one": knownOne, "known two": knownTwo, "none": none,
	} {
		if previous, ok := digests[aggregate.digest]; ok {
			t.Fatalf("adapter preview digest for %q equals %q", name, previous)
		}
		digests[aggregate.digest] = name
	}

	registry, adapters := deletionTestRegistry(t)
	attachments := deletionTestAdapterStub(t, adapters, AdapterNameRecordAttachments)
	attachments.preview.SurvivingCopies = []AdapterSurvivingCopy{KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 4)}
	aggregate, err := registry.previewSnapshot(context.Background(), deletionTestPreviewTarget(t))
	if err != nil {
		t.Fatalf("previewSnapshot() error = %v", err)
	}
	*attachments.preview.SurvivingCopies[0].CopyCount = 5
	if aggregate.survivingCopies[0].CopyCount == attachments.preview.SurvivingCopies[0].CopyCount ||
		*aggregate.survivingCopies[0].CopyCount != 4 {
		t.Fatalf("previewSnapshot() aliases adapter count: %#v", aggregate.survivingCopies)
	}
}

func TestRegistryPreviewCommitmentUsesV2CountStatusEncoding(t *testing.T) {
	t.Parallel()

	registry, adapters := deletionTestRegistry(t)
	deletionTestAdapterStub(t, adapters, AdapterNameRecordAttachments).preview.SurvivingCopies = []AdapterSurvivingCopy{
		KnownAdapterSurvivingCopy(SurvivingCopyKindOtherRecord, 3),
	}
	deletionTestAdapterStub(t, adapters, AdapterNameRecordMarkdownClient).preview.SurvivingCopies = []AdapterSurvivingCopy{
		UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer),
	}

	aggregate, err := registry.previewSnapshot(context.Background(), deletionTestPreviewTarget(t))
	if err != nil {
		t.Fatalf("previewSnapshot() error = %v", err)
	}
	if want := deletionTestAdapterPreviewDigestV2(t, adapters); aggregate.digest != want {
		t.Fatalf("previewSnapshot() digest = %x, want v2 encoding %x", aggregate.digest, want)
	}
}

func TestServiceExecuteRejectsLegacyV1AdapterPreviewCommitment(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	ctx := context.Background()
	readiness, err := fixture.service.registry.RequireReady(ctx)
	if err != nil {
		t.Fatalf("RequireReady() error = %v", err)
	}
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}
	legacyBinding := digestPreviewBinding(
		fixture.actor,
		object,
		fixture.records.snapshot,
		readiness.Digest(),
		deletionTestAdapterPreviewDigestV1(t, fixture.adapters),
		fixture.repository.stored.WitnessHead,
	)
	if legacyBinding == fixture.repository.stored.BindingDigest {
		t.Fatal("legacy v1 adapter preview commitment matches the v2 binding")
	}
	fixture.repository.stored.BindingDigest = legacyBinding

	_, err = fixture.service.Execute(ctx, ExecuteRequest{
		Actor:         fixture.actor,
		RecordID:      "rec_01",
		ReservationID: fixture.repository.stored.ReservationID,
		Token:         fixture.token,
		ReasonCode:    DeletionReasonUserConfirmed,
	})
	if !errors.Is(err, ErrDeletionPreviewStale) {
		t.Fatalf("Execute() error = %v, want ErrDeletionPreviewStale", err)
	}
	if fixture.repository.reserveCalls != 0 || fixture.witness.verifyCalls != 0 {
		t.Fatalf("legacy preview reserve=%d witness-verify=%d, want zero", fixture.repository.reserveCalls, fixture.witness.verifyCalls)
	}
}

func deletionTestPreviewTarget(t *testing.T) PreviewTarget {
	t.Helper()
	target := previewTarget(
		recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		deletionTestRecordSnapshot(t),
	)
	if err := target.Validate(); err != nil {
		t.Fatalf("PreviewTarget.Validate() error = %v", err)
	}
	return target
}

func deletionTestAdapterStub(t *testing.T, adapters []*deletionServiceAdapterStub, name AdapterName) *deletionServiceAdapterStub {
	t.Helper()
	for _, adapter := range adapters {
		if adapter.name == name {
			return adapter
		}
	}
	t.Fatalf("adapter %q not registered", name)
	return nil
}

// deletionTestAdapterPreviewDigestV2 independently spells out the v2 adapter
// preview commitment: the count status is committed before the count, and the
// count is only committed for known copies.
func deletionTestAdapterPreviewDigestV2(t *testing.T, adapters []*deletionServiceAdapterStub) [sha256.Size]byte {
	t.Helper()
	payload := appendLengthPrefixed(nil, "houfeng.record-deletion.adapter-preview.v2")
	payload = appendUint64(payload, 2)
	for _, adapter := range deletionTestAdaptersInRequiredOrder(t, adapters) {
		payload = appendLengthPrefixed(payload, string(adapter.name))
		payload = append(payload, adapter.preview.DependencyDigest[:]...)
		payload = append(payload, adapter.preview.ImpactDigest[:]...)
		payload = appendUint64(payload, uint64(len(adapter.preview.SurvivingCopies)))
		for _, surviving := range adapter.preview.SurvivingCopies {
			payload = appendLengthPrefixed(payload, string(surviving.Kind))
			payload = appendLengthPrefixed(payload, string(surviving.CountStatus))
			if surviving.CountStatus == SurvivingCopyCountKnown {
				payload = appendUint64(payload, *surviving.CopyCount)
			}
		}
	}
	return sha256.Sum256(payload)
}

// deletionTestAdapterPreviewDigestV1 reproduces the retired v1 commitment,
// which encoded every surviving copy as a bare count.
func deletionTestAdapterPreviewDigestV1(t *testing.T, adapters []*deletionServiceAdapterStub) [sha256.Size]byte {
	t.Helper()
	payload := appendLengthPrefixed(nil, "houfeng.record-deletion.adapter-preview.v1")
	payload = appendUint64(payload, 1)
	for _, adapter := range deletionTestAdaptersInRequiredOrder(t, adapters) {
		payload = appendLengthPrefixed(payload, string(adapter.name))
		payload = append(payload, adapter.preview.DependencyDigest[:]...)
		payload = append(payload, adapter.preview.ImpactDigest[:]...)
		payload = appendUint64(payload, uint64(len(adapter.preview.SurvivingCopies)))
		for _, surviving := range adapter.preview.SurvivingCopies {
			if surviving.CopyCount == nil {
				t.Fatalf("v1 commitment cannot encode unknown count for %q", adapter.name)
			}
			payload = appendLengthPrefixed(payload, string(surviving.Kind))
			payload = appendUint64(payload, *surviving.CopyCount)
		}
	}
	return sha256.Sum256(payload)
}

func deletionTestAdaptersInRequiredOrder(t *testing.T, adapters []*deletionServiceAdapterStub) []*deletionServiceAdapterStub {
	t.Helper()
	ordered := make([]*deletionServiceAdapterStub, 0, len(adapters))
	for _, name := range RequiredAdapterNames() {
		ordered = append(ordered, deletionTestAdapterStub(t, adapters, name))
	}
	return ordered
}

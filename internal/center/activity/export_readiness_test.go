package activity

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

// A compile-time golden for Child 10: if this method set changes, Child 10's
// consumers break at build time rather than at an export that looks complete.
var _ ActivityExportReader = (*exportReaderContractStub)(nil)

type exportReaderContractStub struct{}

func (*exportReaderContractStub) Readiness(
	context.Context,
	recordauth.ActorScope,
	RecordSelection,
) (ReadinessVector, error) {
	return ReadinessVector{}, nil
}

func (*exportReaderContractStub) ScanRecordPage(
	context.Context,
	recordauth.ActorScope,
	RecordSelection,
	ActivitySnapshot,
	PageCursor,
) (ActivityPage, error) {
	return ActivityPage{}, nil
}

// The frozen signature is what Child 10 imports. Renaming a parameter type or
// adding a third method must fail this test, not ship as a silent contract drift.
func TestActivityExportReaderMethodSetIsFrozen(t *testing.T) {
	readerType := reflect.TypeOf((*ActivityExportReader)(nil)).Elem()
	if readerType.NumMethod() != 2 {
		t.Fatalf("ActivityExportReader has %d methods, want exactly Readiness and ScanRecordPage", readerType.NumMethod())
	}

	readiness, ok := readerType.MethodByName("Readiness")
	if !ok {
		t.Fatal("ActivityExportReader lost Readiness")
	}
	if readiness.Type.NumIn() != 3 || readiness.Type.NumOut() != 2 {
		t.Fatalf("Readiness signature = %v, want (ctx, ActorScope, RecordSelection) (ReadinessVector, error)", readiness.Type)
	}
	if readiness.Type.In(1) != reflect.TypeOf(recordauth.ActorScope{}) ||
		readiness.Type.In(2) != reflect.TypeOf(RecordSelection{}) ||
		readiness.Type.Out(0) != reflect.TypeOf(ReadinessVector{}) {
		t.Fatalf("Readiness parameter types drifted: %v", readiness.Type)
	}

	scan, ok := readerType.MethodByName("ScanRecordPage")
	if !ok {
		t.Fatal("ActivityExportReader lost ScanRecordPage")
	}
	if scan.Type.NumIn() != 5 || scan.Type.NumOut() != 2 {
		t.Fatalf("ScanRecordPage signature = %v", scan.Type)
	}
	if scan.Type.In(1) != reflect.TypeOf(recordauth.ActorScope{}) ||
		scan.Type.In(2) != reflect.TypeOf(RecordSelection{}) ||
		scan.Type.In(3) != reflect.TypeOf(ActivitySnapshot{}) ||
		scan.Type.In(4) != reflect.TypeOf(PageCursor{}) ||
		scan.Type.Out(0) != reflect.TypeOf(ActivityPage{}) {
		t.Fatalf("ScanRecordPage parameter types drifted: %v", scan.Type)
	}
}

func testExportSelection(t *testing.T, recordIDs ...string) RecordSelection {
	t.Helper()
	selection, err := NormalizeRecordSelection(RecordSelection{RecordIDs: recordIDs})
	if err != nil {
		t.Fatalf("normalize selection: %v", err)
	}
	return selection
}

func testSettledSource(kind SourceKind, through time.Time) SourceReadiness {
	return SourceReadiness{
		Kind:     kind,
		Head:     NewSettledSourceHead(kind, through, 42),
		CaughtUp: true,
	}
}

func TestNormalizeRecordSelectionSortsAndDeduplicates(t *testing.T) {
	selection, err := NormalizeRecordSelection(RecordSelection{
		RecordIDs: []string{"rec_bbbb", "rec_aaaa", "rec_bbbb", "rec_cccc"},
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	want := []string{"rec_aaaa", "rec_bbbb", "rec_cccc"}
	if !reflect.DeepEqual(selection.RecordIDs, want) {
		t.Fatalf("RecordIDs = %v, want %v", selection.RecordIDs, want)
	}
	if !selection.Normalized() {
		t.Fatal("normalized selection must report Normalized()")
	}
}

func TestNormalizeRecordSelectionRejectsEmptyOrInvalidIDs(t *testing.T) {
	for name, input := range map[string]RecordSelection{
		"no records":        {RecordIDs: nil},
		"empty id":          {RecordIDs: []string{""}},
		"wrong prefix":      {RecordIDs: []string{"rrv_aaaa"}},
		"uppercase letters": {RecordIDs: []string{"rec_AAAA"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeRecordSelection(input); !errors.Is(err, ErrInvalidRecordSelection) {
				t.Fatalf("error = %v, want ErrInvalidRecordSelection", err)
			}
		})
	}
}

// Two callers listing the same records in different orders must produce the same
// proof. Without that, an export could be rejected on re-read for no reason.
func TestRecordSelectionDigestIsOrderIndependent(t *testing.T) {
	left := testExportSelection(t, "rec_one", "rec_two")
	right := testExportSelection(t, "rec_two", "rec_one")
	leftDigest, err := left.Digest()
	if err != nil {
		t.Fatalf("left digest: %v", err)
	}
	rightDigest, err := right.Digest()
	if err != nil {
		t.Fatalf("right digest: %v", err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("digests differ for the same set: %x vs %x", leftDigest, rightDigest)
	}

	other := testExportSelection(t, "rec_one", "rec_three")
	otherDigest, err := other.Digest()
	if err != nil {
		t.Fatalf("other digest: %v", err)
	}
	if leftDigest == otherDigest {
		t.Fatal("different selections must not share a digest")
	}
}

func TestRecordSelectionDigestRefusesAnUnnormalizedValue(t *testing.T) {
	if _, err := (RecordSelection{RecordIDs: []string{"rec_one"}}).Digest(); !errors.Is(err, ErrSelectionNotNormalized) {
		t.Fatalf("error = %v, want ErrSelectionNotNormalized", err)
	}
}

func TestExportReadinessDigestBindsActorSelectionAndVector(t *testing.T) {
	actor := testActor(t)
	selection := testExportSelection(t, "rec_exportone")
	through := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	sources := []SourceReadiness{
		testSettledSource(SourceKindRecordDomain, through),
		testSettledSource(SourceKindEvidenceSnapshot, through),
	}
	snapshot := ActivitySnapshot{
		ProjectionGeneration:    3,
		PublishedIngestSequence: 100,
	}

	digest, err := ExportReadinessDigest(ExportScope{Actor: actor}, selection, snapshot, sources)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if digest == ([sha256.Size]byte{}) {
		t.Fatal("digest must not be empty")
	}

	for name, mutate := range map[string]func() (ExportScope, RecordSelection, ActivitySnapshot, []SourceReadiness){
		"different actor": func() (ExportScope, RecordSelection, ActivitySnapshot, []SourceReadiness) {
			other, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
				UserID:    "usr_000000000000000000000002",
				Role:      recordauth.RoleProjectAdmin,
				ProjectID: recordauth.ProjectIDDefault,
			})
			if err != nil {
				t.Fatalf("other actor: %v", err)
			}
			return ExportScope{Actor: other}, selection, snapshot, sources
		},
		"different selection": func() (ExportScope, RecordSelection, ActivitySnapshot, []SourceReadiness) {
			return ExportScope{Actor: actor}, testExportSelection(t, "rec_exporttwo"), snapshot, sources
		},
		"different generation": func() (ExportScope, RecordSelection, ActivitySnapshot, []SourceReadiness) {
			changed := snapshot
			changed.ProjectionGeneration = 4
			return ExportScope{Actor: actor}, selection, changed, sources
		},
		"different published head": func() (ExportScope, RecordSelection, ActivitySnapshot, []SourceReadiness) {
			changed := snapshot
			changed.PublishedIngestSequence = 101
			return ExportScope{Actor: actor}, selection, changed, sources
		},
		"source not caught up": func() (ExportScope, RecordSelection, ActivitySnapshot, []SourceReadiness) {
			changed := append([]SourceReadiness(nil), sources...)
			changed[0].CaughtUp = false
			return ExportScope{Actor: actor}, selection, snapshot, changed
		},
	} {
		t.Run(name, func(t *testing.T) {
			scope, selected, snap, src := mutate()
			other, err := ExportReadinessDigest(scope, selected, snap, src)
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			if other == digest {
				t.Fatalf("%s left the readiness digest unchanged", name)
			}
		})
	}
}

func TestReadinessVectorValidateBindingRejectsAForeignDigest(t *testing.T) {
	actor := testActor(t)
	selection := testExportSelection(t, "rec_bindone")
	through := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	sources := []SourceReadiness{testSettledSource(SourceKindRecordDomain, through)}
	digest, err := ExportReadinessDigest(ExportScope{Actor: actor}, selection, ActivitySnapshot{
		ProjectionGeneration: 1, PublishedIngestSequence: 10,
	}, sources)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	vector := ReadinessVector{
		Snapshot: ActivitySnapshot{
			ProjectionGeneration:    1,
			PublishedIngestSequence: 10,
			ReadinessDigest:         digest,
		},
		Sources: sources,
	}
	if err := vector.ValidateBinding(ExportScope{Actor: actor}, selection); err != nil {
		t.Fatalf("matching binding must pass: %v", err)
	}

	foreign := vector
	foreign.Snapshot.ReadinessDigest = sha256.Sum256([]byte("forged"))
	if err := foreign.ValidateBinding(ExportScope{Actor: actor}, selection); !errors.Is(err, ErrReadinessBindingMismatch) {
		t.Fatalf("error = %v, want ErrReadinessBindingMismatch", err)
	}
}

func TestPageCursorBindsSnapshotActorAndSelection(t *testing.T) {
	actor := testActor(t)
	selection := testExportSelection(t, "rec_pageone")
	snapshot := ActivitySnapshot{
		ProjectionGeneration:    2,
		PublishedIngestSequence: 50,
		ReadinessDigest:         sha256.Sum256([]byte("snapshot")),
	}
	position := SortKey{
		EventAt:    time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC),
		RecordedAt: time.Date(2026, 8, 19, 10, 0, 1, 0, time.UTC),
		SourceKind: SourceKindRecordDomain,
		ActivityID: "act_gv2xq4hb7zsm3nkfr6wcyd5ple",
	}

	cursor, err := NewPageCursor(ExportScope{Actor: actor}, selection, snapshot, position)
	if err != nil {
		t.Fatalf("new page cursor: %v", err)
	}
	if cursor.FirstPage() {
		t.Fatal("a minted cursor is not the first page")
	}
	if err := cursor.Validate(ExportScope{Actor: actor}, selection, snapshot); err != nil {
		t.Fatalf("validate matching cursor: %v", err)
	}

	if err := (PageCursor{}).Validate(ExportScope{Actor: actor}, selection, snapshot); err != nil {
		t.Fatalf("first page must validate: %v", err)
	}

	otherSelection := testExportSelection(t, "rec_pagetwo")
	if err := cursor.Validate(ExportScope{Actor: actor}, otherSelection, snapshot); !errors.Is(err, ErrPageCursorMismatch) {
		t.Fatalf("error = %v, want ErrPageCursorMismatch for a different selection", err)
	}

	otherSnapshot := snapshot
	otherSnapshot.ReadinessDigest = sha256.Sum256([]byte("other"))
	if err := cursor.Validate(ExportScope{Actor: actor}, selection, otherSnapshot); !errors.Is(err, ErrPageCursorMismatch) {
		t.Fatalf("error = %v, want ErrPageCursorMismatch for a different snapshot", err)
	}
}

// A bare sequence is not a page cursor. Export refuses to invent one from a
// number alone, so Child 10 cannot accidentally page by published head.
func TestPageCursorCannotBeConstructedFromABareSequence(t *testing.T) {
	cursorType := reflect.TypeOf(PageCursor{})
	for _, forbidden := range []string{"Sequence", "IngestSequence", "AsOf", "PublishedIngestSequence"} {
		if _, ok := cursorType.FieldByName(forbidden); ok {
			t.Fatalf("PageCursor must not expose a bare %s field; bind through digests instead", forbidden)
		}
	}
}

func TestActivityEnvelopeSortKeyMatchesCanonicalOrder(t *testing.T) {
	envelope := ActivityEnvelope{
		ActivityID: "act_gv2xq4hb7zsm3nkfr6wcyd5ple",
		EventAt:    time.Date(2026, 8, 19, 11, 0, 0, 0, time.UTC),
		RecordedAt: time.Date(2026, 8, 19, 11, 0, 2, 0, time.UTC),
		Source:     SourceIdentity{Kind: SourceKindMonitoringEvent, EventID: "evt_1", Version: 1},
	}
	key := envelope.SortKeyValue()
	if key.EventAt != envelope.EventAt || key.RecordedAt != envelope.RecordedAt ||
		key.SourceKind != envelope.Source.Kind || key.ActivityID != envelope.ActivityID {
		t.Fatalf("SortKeyValue() = %+v, want the envelope's own order tuple", key)
	}
}

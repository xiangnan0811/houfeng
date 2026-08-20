package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

type exportHeadStub struct {
	head PublishedHead
	err  error
}

func (stub *exportHeadStub) LoadPublishedHead(context.Context) (PublishedHead, error) {
	if stub.err != nil {
		return PublishedHead{}, stub.err
	}
	return stub.head, nil
}

type exportAdapterStub struct {
	kind      SourceKind
	head      SourceHead
	readiness SourceReadiness
	err       error
}

func (stub *exportAdapterStub) Kind() SourceKind { return stub.kind }
func (stub *exportAdapterStub) ScanAfter(context.Context, ScanWindow, int) ([]CandidateEvent, error) {
	return nil, nil
}
func (stub *exportAdapterStub) IncrementalHead(context.Context) (SourceHead, error) {
	return stub.head, nil
}
func (stub *exportAdapterStub) AuthoritativeHead(context.Context, ExportScope) (SourceHead, error) {
	return stub.head, stub.err
}
func (stub *exportAdapterStub) Readiness(context.Context, ExportScope, SourceHead) (SourceReadiness, error) {
	return stub.readiness, stub.err
}

type exportPageStub struct {
	page activityPageCapture
	err  error
}

type activityPageCapture struct {
	called bool
	page   ActivityPage
}

func (stub *exportPageStub) ScanExportRecordPage(
	context.Context,
	recordauth.ActorScope,
	RecordSelection,
	ActivitySnapshot,
	PageCursor,
	int,
) (ActivityPage, error) {
	stub.page.called = true
	if stub.err != nil {
		return ActivityPage{}, stub.err
	}
	return stub.page.page, nil
}

func TestExportReaderReadinessBindsDigestAndFailsWhenSourceBehind(t *testing.T) {
	through := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	settled := NewSettledSourceHead(SourceKindRecordDomain, through, 1)
	ready := SourceReadiness{Kind: SourceKindRecordDomain, Head: settled, CaughtUp: true}
	heads := &exportHeadStub{head: PublishedHead{Generation: 3, PublishedIngestSequence: 40}}
	pages := &exportPageStub{}
	reader, err := NewExportReader(ExportReaderDeps{
		HeadStore: heads,
		Pages:     pages,
		Adapters:  []ExportReadySourceAdapter{&exportAdapterStub{kind: SourceKindRecordDomain, head: settled, readiness: ready}},
	})
	if err != nil {
		t.Fatalf("NewExportReader: %v", err)
	}
	actor := testActor(t)
	selection := testExportSelection(t, "rec_exportone")
	vector, err := reader.Readiness(context.Background(), actor, selection)
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	if vector.Snapshot.ProjectionGeneration != 3 || vector.Snapshot.PublishedIngestSequence != 40 {
		t.Fatalf("snapshot = %#v", vector.Snapshot)
	}
	if err := vector.ValidateBinding(ExportScope{Actor: actor}, selection); err != nil {
		t.Fatalf("ValidateBinding: %v", err)
	}

	behind := ready
	behind.CaughtUp = false
	readerBehind, err := NewExportReader(ExportReaderDeps{
		HeadStore: heads,
		Pages:     pages,
		Adapters: []ExportReadySourceAdapter{
			&exportAdapterStub{kind: SourceKindRecordDomain, head: settled, readiness: behind},
		},
	})
	if err != nil {
		t.Fatalf("NewExportReader behind: %v", err)
	}
	if _, err := readerBehind.Readiness(context.Background(), actor, selection); !errors.Is(err, ErrExportNotReady) {
		t.Fatalf("Readiness error = %v, want ErrExportNotReady", err)
	}
}

func TestExportReaderScanRejectsDriftedSnapshot(t *testing.T) {
	through := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	settled := NewSettledSourceHead(SourceKindRecordDomain, through, 1)
	ready := SourceReadiness{Kind: SourceKindRecordDomain, Head: settled, CaughtUp: true}
	heads := &exportHeadStub{head: PublishedHead{Generation: 3, PublishedIngestSequence: 40}}
	pages := &exportPageStub{page: activityPageCapture{page: ActivityPage{Envelopes: []ActivityEnvelope{}}}}
	reader, err := NewExportReader(ExportReaderDeps{
		HeadStore: heads,
		Pages:     pages,
		Adapters:  []ExportReadySourceAdapter{&exportAdapterStub{kind: SourceKindRecordDomain, head: settled, readiness: ready}},
	})
	if err != nil {
		t.Fatalf("NewExportReader: %v", err)
	}
	actor := testActor(t)
	selection := testExportSelection(t, "rec_exportone")
	vector, err := reader.Readiness(context.Background(), actor, selection)
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	drifted := vector.Snapshot
	drifted.PublishedIngestSequence++
	if _, err := reader.ScanRecordPage(context.Background(), actor, selection, drifted, PageCursor{}); !errors.Is(err, ErrExportSnapshotMismatch) {
		t.Fatalf("ScanRecordPage error = %v, want ErrExportSnapshotMismatch", err)
	}
}

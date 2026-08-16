package evidence

import (
	"context"
	"errors"
	"testing"
)

func TestExportAdapterInvokesOnlyRegisteredKindExporter(t *testing.T) {
	kind, fixture := testConformingKind(t)
	exported := ExportMaterial{Key: fixture.Selection.Key, MediaType: "application/json", Filename: "host.json", Bytes: []byte(`{"safe":true}`)}
	kind.exportMaterial = &exported
	registry, err := NewRegistry([]Kind{kind})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	source := &authorizedSnapshotSourceStub{recordID: "rec_export", snapshot: kind.snapshot}
	adapter, err := NewExportAdapter(registry, source)
	if err != nil {
		t.Fatalf("NewExportAdapter() error = %v", err)
	}
	got, err := adapter.Export(context.Background(), ExportRequest{
		Actor: fixture.Actor, SnapshotID: "evs_export", Mode: ExportModeSafe,
	})
	if err != nil || got.Filename != exported.Filename || string(got.Bytes) != string(exported.Bytes) {
		t.Fatalf("Export() = %#v, %v", got, err)
	}
	if source.calls != 1 || kind.exportCalls != 1 {
		t.Fatalf("source/export calls = %d/%d, want 1/1", source.calls, kind.exportCalls)
	}
}

func TestExportAdapterFailsClosedForUnknownKindAndInvalidExporterMaterial(t *testing.T) {
	kind, fixture := testConformingKind(t)
	registry, _ := NewRegistry([]Kind{kind})
	unknown := kind.snapshot
	envelope := unknown.Envelope()
	envelope.Key = KindKey{Kind: "unknown.kind", SchemaVersion: 1}
	adapter, _ := NewExportAdapter(registry, &authorizedSnapshotSourceStub{recordID: "rec_export", keyOverride: &envelope.Key, snapshot: unknown})
	if _, err := adapter.Export(context.Background(), ExportRequest{Actor: fixture.Actor, SnapshotID: "evs_export", Mode: ExportModeSafe}); !errors.Is(err, ErrKindNotRegistered) {
		t.Fatalf("unknown kind export error = %v", err)
	}

	kind.exportMaterial = &ExportMaterial{Key: fixture.Selection.Key, MediaType: "", Filename: "", Bytes: nil}
	adapter, _ = NewExportAdapter(registry, &authorizedSnapshotSourceStub{recordID: "rec_export", snapshot: kind.snapshot})
	if _, err := adapter.Export(context.Background(), ExportRequest{Actor: fixture.Actor, SnapshotID: "evs_export", Mode: ExportModeSafe}); !errors.Is(err, ErrExportUnavailable) {
		t.Fatalf("invalid material export error = %v", err)
	}

	kind.exportMaterial = &ExportMaterial{
		Key: fixture.Selection.Key, MediaType: "application/json", Filename: "host.json",
		Bytes: []byte(`{"stdout":"must not escape through a data-dependent exporter"}`),
	}
	adapter, _ = NewExportAdapter(registry, &authorizedSnapshotSourceStub{recordID: "rec_export", snapshot: kind.snapshot})
	if _, err := adapter.Export(context.Background(), ExportRequest{Actor: fixture.Actor, SnapshotID: "evs_export", Mode: ExportModeSafe}); !errors.Is(err, ErrExportUnavailable) {
		t.Fatalf("unsafe runtime material export error = %v, want ErrExportUnavailable", err)
	}
}

type authorizedSnapshotSourceStub struct {
	recordID    string
	snapshot    CanonicalSnapshot
	keyOverride *KindKey
	calls       int
}

func (source *authorizedSnapshotSourceStub) LoadAuthorizedEvidenceSnapshot(
	context.Context,
	ActorScope,
	string,
) (AuthorizedSnapshot, error) {
	source.calls++
	key := source.snapshot.Envelope().Key
	if source.keyOverride != nil {
		key = *source.keyOverride
	}
	return AuthorizedSnapshot{RecordID: source.recordID, SnapshotID: "evs_export", Key: key, Snapshot: source.snapshot}, nil
}

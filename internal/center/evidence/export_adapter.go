package evidence

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidExportRequest = errors.New("invalid evidence export request")
	ErrExportUnavailable    = errors.New("evidence export unavailable")
)

type AuthorizedSnapshot struct {
	RecordID   string
	SnapshotID string
	Key        KindKey
	Snapshot   CanonicalSnapshot
}

type AuthorizedSnapshotSource interface {
	LoadAuthorizedEvidenceSnapshot(context.Context, ActorScope, string) (AuthorizedSnapshot, error)
}

type ExportRequest struct {
	Actor      ActorScope
	SnapshotID string
	Mode       ExportMode
}

type ExportAdapter struct {
	registry Registry
	source   AuthorizedSnapshotSource
}

func NewExportAdapter(registry Registry, source AuthorizedSnapshotSource) (*ExportAdapter, error) {
	if len(registry.kinds) == 0 || nilRevisionPreparationDependency(source) {
		return nil, ErrExportUnavailable
	}
	return &ExportAdapter{registry: registry, source: source}, nil
}

func (adapter *ExportAdapter) Export(ctx context.Context, request ExportRequest) (ExportMaterial, error) {
	if ctx == nil || adapter == nil || len(adapter.registry.kinds) == 0 ||
		nilRevisionPreparationDependency(adapter.source) || !ValidSnapshotID(request.SnapshotID) ||
		(request.Mode != ExportModeSafe && request.Mode != ExportModeSensitiveTopology) {
		return ExportMaterial{}, ErrInvalidExportRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil || !reflect.DeepEqual(actor, request.Actor) {
		return ExportMaterial{}, ErrInvalidExportRequest
	}
	authorized, err := adapter.source.LoadAuthorizedEvidenceSnapshot(ctx, actor.Clone(), request.SnapshotID)
	if err != nil {
		return ExportMaterial{}, err
	}
	if authorized.SnapshotID != request.SnapshotID || !validClosedPreparedID(authorized.RecordID, "rec_") {
		return ExportMaterial{}, ErrExportUnavailable
	}
	kind, err := adapter.registry.LookupKey(authorized.Key)
	if err != nil {
		return ExportMaterial{}, err
	}
	if authorized.Snapshot.Envelope().Key != authorized.Key || authorized.Snapshot.Validate(kind.Descriptor()) != nil {
		return ExportMaterial{}, ErrExportUnavailable
	}
	material := kind.Export(cloneCanonicalSnapshot(authorized.Snapshot), request.Mode)
	if material.Key != authorized.Key || !validExportMediaType(material.MediaType) ||
		!validExportFilename(material.Filename) || len(material.Bytes) == 0 || uint64(len(material.Bytes)) > MaxCanonicalPayloadBytes ||
		validateExportMaterial(material) != nil {
		return ExportMaterial{}, ErrExportUnavailable
	}
	material.Bytes = append([]byte(nil), material.Bytes...)
	return material, nil
}

func validExportMediaType(value string) bool {
	return value == "application/json" || value == "text/csv" || value == "text/plain"
}

func validExportFilename(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\\x00\r\n") || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return !strings.HasPrefix(value, ".")
}

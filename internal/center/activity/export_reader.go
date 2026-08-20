package activity

import (
	"context"
	"errors"
	"fmt"

	"houfeng/internal/center/recordauth"
)

var (
	// ErrExportNotReady means at least one registered source cannot prove
	// completeness at the published head. Archives must fail closed rather than
	// ship a partial timeline as complete.
	ErrExportNotReady = errors.New("activity export is not ready")
	// ErrExportSnapshotMismatch means ScanRecordPage was asked to read a
	// snapshot that no longer matches the live readiness proof.
	ErrExportSnapshotMismatch = errors.New("activity export snapshot mismatch")
)

// ExportReaderDeps are the store and adapter surfaces an export reader needs.
// Adapters supply settled heads and per-source readiness; the store supplies the
// published generation watermark and record-scoped pages.
type ExportReaderDeps struct {
	HeadStore PublishedHeadStore
	Adapters  []ExportReadySourceAdapter
	Pages     ExportPageStore
}

// ExportPageStore reads projection rows for an explicit record selection at a
// fixed snapshot. It is narrower than SubjectPageStore on purpose: export never
// pages by subject.
type ExportPageStore interface {
	ScanExportRecordPage(
		ctx context.Context,
		actor recordauth.ActorScope,
		selection RecordSelection,
		snapshot ActivitySnapshot,
		cursor PageCursor,
		limit int,
	) (ActivityPage, error)
}

// ExportReader implements ActivityExportReader for Child 10 consumers.
type ExportReader struct {
	heads    PublishedHeadStore
	adapters []ExportReadySourceAdapter
	pages    ExportPageStore
}

// NewExportReader wires the frozen export seam. Every dependency is required so
// an incomplete bootstrap cannot claim readiness.
func NewExportReader(deps ExportReaderDeps) (*ExportReader, error) {
	if nilActivityDependency(deps.HeadStore) || nilActivityDependency(deps.Pages) || len(deps.Adapters) == 0 {
		return nil, fmt.Errorf("%w: export reader dependency", ErrInvalidListRequest)
	}
	adapters := make([]ExportReadySourceAdapter, len(deps.Adapters))
	copy(adapters, deps.Adapters)
	return &ExportReader{heads: deps.HeadStore, adapters: adapters, pages: deps.Pages}, nil
}

// Readiness aggregates the published head with every registered source's settled
// readiness vector and binds the digest to the actor and selection.
func (reader *ExportReader) Readiness(
	ctx context.Context,
	actor recordauth.ActorScope,
	selection RecordSelection,
) (ReadinessVector, error) {
	if ctx == nil || reader == nil {
		return ReadinessVector{}, ErrInvalidListRequest
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return ReadinessVector{}, fmt.Errorf("%w: actor", ErrInvalidListRequest)
	}
	normalizedSelection, err := NormalizeRecordSelection(selection)
	if err != nil {
		return ReadinessVector{}, err
	}
	head, err := reader.heads.LoadPublishedHead(ctx)
	if err != nil {
		return ReadinessVector{}, err
	}
	if head.Generation == 0 {
		return ReadinessVector{}, ErrProjectionUnavailable
	}

	scope := ExportScope{Actor: normalizedActor}
	sources := make([]SourceReadiness, 0, len(reader.adapters))
	for _, adapter := range reader.adapters {
		sourceHead, err := adapter.AuthoritativeHead(ctx, scope)
		if err != nil {
			return ReadinessVector{}, err
		}
		readiness, err := adapter.Readiness(ctx, scope, sourceHead)
		if err != nil {
			return ReadinessVector{}, err
		}
		sources = append(sources, readiness)
	}

	snapshot := ActivitySnapshot{
		ProjectionGeneration:    head.Generation,
		PublishedIngestSequence: head.PublishedIngestSequence,
	}
	digest, err := ExportReadinessDigest(scope, normalizedSelection, snapshot, sources)
	if err != nil {
		return ReadinessVector{}, err
	}
	snapshot.ReadinessDigest = digest
	vector := ReadinessVector{Snapshot: snapshot, Sources: sources}
	required := make([]SourceKind, 0, len(reader.adapters))
	for _, adapter := range reader.adapters {
		required = append(required, adapter.Kind())
	}
	if err := vector.ValidateForExport(required); err != nil {
		return ReadinessVector{}, fmt.Errorf("%w: %v", ErrExportNotReady, err)
	}
	return vector, nil
}

// ScanRecordPage reads one authorized page of the selection at exactly the
// readiness snapshot the caller proved earlier.
func (reader *ExportReader) ScanRecordPage(
	ctx context.Context,
	actor recordauth.ActorScope,
	selection RecordSelection,
	snapshot ActivitySnapshot,
	cursor PageCursor,
) (ActivityPage, error) {
	if ctx == nil || reader == nil {
		return ActivityPage{}, ErrInvalidListRequest
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return ActivityPage{}, fmt.Errorf("%w: actor", ErrInvalidListRequest)
	}
	normalizedSelection, err := NormalizeRecordSelection(selection)
	if err != nil {
		return ActivityPage{}, err
	}
	scope := ExportScope{Actor: normalizedActor}
	if err := cursor.Validate(scope, normalizedSelection, snapshot); err != nil {
		return ActivityPage{}, err
	}

	live, err := reader.Readiness(ctx, normalizedActor, normalizedSelection)
	if err != nil {
		return ActivityPage{}, err
	}
	if live.Snapshot.ProjectionGeneration != snapshot.ProjectionGeneration ||
		live.Snapshot.PublishedIngestSequence != snapshot.PublishedIngestSequence ||
		live.Snapshot.ReadinessDigest != snapshot.ReadinessDigest {
		return ActivityPage{}, ErrExportSnapshotMismatch
	}

	return reader.pages.ScanExportRecordPage(
		ctx,
		normalizedActor,
		normalizedSelection,
		snapshot,
		cursor,
		ExportPageLimit,
	)
}

var _ ActivityExportReader = (*ExportReader)(nil)

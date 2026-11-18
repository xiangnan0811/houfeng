package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

type currentRecordAuthorizationTransactionResolver interface {
	resolveCurrentAuthorizationInTransaction(context.Context, pgx.Tx, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error)
}

type currentRecordAuthorizationPrefetcher interface {
	prepareCurrentAuthorization(context.Context, []string) (currentRecordAuthorizationTransactionResolver, error)
}

type discoveredRecordSubjectRoute struct {
	projectID  recordauth.ProjectID
	references map[records.SubjectReference]struct{}
	denied     error
}

// preparedCurrentRecordAuthorization is private to one operation. Its discovery
// and witness observations are not authorization: every use rereads APP facts
// under the caller's transaction and resolves with the current actor.
type preparedCurrentRecordAuthorization struct {
	source    *PostgresCurrentRecordAuthorizationSource
	routes    map[string]discoveredRecordSubjectRoute
	authority *PreparedSourceAuthority
}

var _ currentRecordAuthorizationPrefetcher = (*PostgresCurrentRecordAuthorizationSource)(nil)
var _ currentRecordAuthorizationTransactionResolver = (*preparedCurrentRecordAuthorization)(nil)

func (source *PostgresCurrentRecordAuthorizationSource) prepareCurrentAuthorization(
	ctx context.Context,
	recordIDs []string,
) (currentRecordAuthorizationTransactionResolver, error) {
	if ctx == nil || source == nil || source.load == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	resolver, ok := source.resolver.(*RecordSubjectReadResolver)
	if !ok || resolver == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	reader, ok := resolver.tombstones.(*WitnessedRecordSubjectTombstoneReader)
	if !ok || reader == nil {
		return nil, ErrRecordSubjectUnavailable
	}

	routes := make(map[string]discoveredRecordSubjectRoute, len(recordIDs))
	keys := make([]sourceretirement.SourceKey, 0)
	seenKeys := make(map[sourceretirement.SourceKey]struct{})
	for _, recordID := range recordIDs {
		if _, exists := routes[recordID]; exists {
			continue
		}
		snapshot, err := source.load(ctx, recordID)
		if err != nil {
			if errors.Is(err, records.ErrRecordNotFound) || errors.Is(err, records.ErrRecordDeletionReserved) {
				routes[recordID] = discoveredRecordSubjectRoute{denied: err}
				continue
			}
			return nil, fmt.Errorf("load current record authorization: %w", err)
		}
		normalized, err := normalizeCurrentRecordAuthorizationSnapshot(recordID, snapshot.projectID, snapshot)
		if err != nil {
			return nil, err
		}
		route := discoveredRecordSubjectRoute{
			projectID:  normalized.projectID,
			references: make(map[records.SubjectReference]struct{}, len(normalized.subjects)),
		}
		for _, subject := range normalized.subjects {
			route.references[subject.Reference] = struct{}{}
			kind, ok := recordSubjectSourceKind(subject.Reference.Kind)
			if !ok {
				return nil, ErrRecordSubjectUnavailable
			}
			key := reader.sourceKey(normalized.projectID, kind, subject.Reference.SourceID)
			if _, exists := seenKeys[key]; !exists {
				seenKeys[key] = struct{}{}
				keys = append(keys, key)
			}
		}
		routes[recordID] = route
	}

	authority, err := reader.Prefetch(ctx, keys)
	if err != nil {
		return nil, err
	}
	return &preparedCurrentRecordAuthorization{source: source, routes: routes, authority: authority}, nil
}

func (prepared *preparedCurrentRecordAuthorization) resolveCurrentAuthorizationInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	actor recordauth.ActorScope,
	recordID string,
) (records.CurrentRecordAuthorization, error) {
	if ctx == nil || prepared == nil || prepared.source == nil || prepared.authority == nil ||
		nilRecordSubjectDependency(tx) {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}
	route, ok := prepared.routes[recordID]
	if !ok {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}
	if route.denied != nil {
		return records.CurrentRecordAuthorization{}, route.denied
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil || normalizedActor.ProjectID != route.projectID {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}

	loader := &PostgresCurrentRecordAuthorizationSource{db: tx}
	snapshot, err := loader.loadCurrentRecordAuthorizationSnapshot(ctx, recordID)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	normalized, err := normalizeCurrentRecordAuthorizationSnapshot(recordID, normalizedActor.ProjectID, snapshot)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	if len(normalized.subjects) != len(route.references) {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}
	for _, subject := range normalized.subjects {
		if _, exists := route.references[subject.Reference]; !exists {
			return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
		}
	}

	liveSource := transactionRecordSubjectSource{db: tx}
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(liveSource),
		newMonitoringInstanceRecordSubjectAdapter(liveSource),
		newTargetRecordSubjectAdapter(liveSource),
	})
	if err != nil {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}
	loader.resolver = NewRecordSubjectReadResolver(registry, prepared.authority.ForTransaction(tx))
	sources, err := loader.resolveRecordAuthorizationSubjects(ctx, normalizedActor, normalized.projectID, normalized.subjects)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	return records.CurrentRecordAuthorization{
		RecordID:           normalized.recordID,
		CurrentRevisionID:  normalized.currentRevisionID,
		LockVersion:        normalized.lockVersion,
		AuthorizationEpoch: normalized.authorizationEpoch,
		Lifecycle:          normalized.lifecycle,
		Evidence: records.RecordAuthorizationEvidence{
			ProjectID:  normalized.projectID,
			Visibility: normalized.visibility,
			Sources:    sources,
		},
	}, nil
}

type transactionRecordSubjectSource struct {
	db currentRecordAuthorizationDB
}

func (source transactionRecordSubjectSource) loadVPSRecordSubject(ctx context.Context, id string) (vpsRecordSubject, error) {
	return loadVPSRecordSubject(ctx, source.db, id)
}

func (source transactionRecordSubjectSource) loadMonitoringRecordSubject(ctx context.Context, id string) (monitoringRecordSubject, error) {
	return loadMonitoringRecordSubject(ctx, source.db, id)
}

func (source transactionRecordSubjectSource) loadTargetRecordSubject(ctx context.Context, id string) (targetRecordSubject, error) {
	return loadTargetRecordSubject(ctx, source.db, id)
}

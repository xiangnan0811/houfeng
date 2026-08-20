package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// Compile-time wiring: the projection repository is both the head source and the
// subject page store the list service depends on.
var (
	_ activity.PublishedHeadStore = (*ActivityProjectionRepository)(nil)
	_ activity.SubjectPageStore   = (*ActivityProjectionRepository)(nil)
)

// LoadPublishedHead returns the generation currently serving reads and the
// contiguous ingest watermark it has published through.
func (repository *ActivityProjectionRepository) LoadPublishedHead(
	ctx context.Context,
) (activity.PublishedHead, error) {
	if ctx == nil || repository == nil || repository.pool == nil {
		return activity.PublishedHead{}, activity.ErrProjectionUnavailable
	}
	var (
		generation int64
		published  int64
	)
	err := repository.pool.QueryRow(ctx, `
		select projection_generation, published_ingest_sequence
		from public.record_activity_projection_heads
		where project_id = $1 and head_state = 'active'`,
		recordplatform.ProjectIDDefault,
	).Scan(&generation, &published)
	if errors.Is(err, pgx.ErrNoRows) {
		return activity.PublishedHead{}, activity.ErrProjectionUnavailable
	}
	if err != nil {
		return activity.PublishedHead{}, fmt.Errorf("load activity published head: %w", err)
	}
	if generation <= 0 {
		return activity.PublishedHead{}, activity.ErrProjectionUnavailable
	}
	return activity.PublishedHead{
		Generation:              uint64(generation),
		PublishedIngestSequence: uint64(published),
	}, nil
}

// ListSubjectPage applies every subject/auth/as-of/view/source/kind/time and
// revision-validity predicate on the denormalized relation rows before ORDER and
// LIMIT, then PK-joins presentation. That order is what keeps a sparse filter
// from returning short pages or leaking unauthorized activity via keyset holes.
func (repository *ActivityProjectionRepository) ListSubjectPage(
	ctx context.Context,
	req activity.SubjectPageRequest,
) (activity.SubjectPageResult, error) {
	if ctx == nil || repository == nil || repository.pool == nil {
		return activity.SubjectPageResult{}, activity.ErrInvalidListRequest
	}
	if !req.Query.Normalized() || req.Generation == 0 || req.Limit < 1 {
		return activity.SubjectPageResult{}, activity.ErrInvalidListRequest
	}

	known, tombstoneIdentity, err := repository.resolveSubjectPresence(ctx, req)
	if err != nil {
		return activity.SubjectPageResult{}, err
	}

	statement, arguments, err := buildSubjectActivityCandidateQuery(req)
	if err != nil {
		return activity.SubjectPageResult{}, err
	}
	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return activity.SubjectPageResult{}, fmt.Errorf("list subject activity candidates: %w", err)
	}
	defer rows.Close()

	activityIDs := make([]string, 0, req.Limit)
	for rows.Next() {
		var activityID string
		if err := rows.Scan(&activityID); err != nil {
			return activity.SubjectPageResult{}, fmt.Errorf("scan subject activity candidate: %w", err)
		}
		activityIDs = append(activityIDs, activityID)
	}
	if err := rows.Err(); err != nil {
		return activity.SubjectPageResult{}, fmt.Errorf("list subject activity candidates: %w", err)
	}

	events, err := repository.loadActivityEvents(ctx, req.Generation, activityIDs)
	if err != nil {
		return activity.SubjectPageResult{}, err
	}
	return activity.SubjectPageResult{
		Events:            events,
		SubjectKnown:      known,
		TombstoneIdentity: tombstoneIdentity,
	}, nil
}

// HasNewerAuthorized reports whether any authorized matching relation exists
// strictly after the fixed page watermark, up to the current published head
// carried in req.AsOf. Hidden scopes never flip this for a viewer who cannot
// see them, because the same auth digests gate both the page and this check.
func (repository *ActivityProjectionRepository) HasNewerAuthorized(
	ctx context.Context,
	req activity.SubjectPageRequest,
	afterSequence uint64,
) (bool, error) {
	if ctx == nil || repository == nil || repository.pool == nil {
		return false, activity.ErrInvalidListRequest
	}
	if !req.Query.Normalized() || req.Generation == 0 {
		return false, activity.ErrInvalidListRequest
	}
	if req.AsOf <= afterSequence {
		return false, nil
	}

	probe := req
	probe.After = nil
	probe.Limit = 1
	// Constrain the candidate window to (afterSequence, req.AsOf].
	statement, arguments, err := buildSubjectActivityCandidateQueryWithSequenceBounds(probe, afterSequence+1, req.AsOf)
	if err != nil {
		return false, err
	}
	var activityID string
	err = repository.pool.QueryRow(ctx, statement, arguments...).Scan(&activityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has newer authorized activity: %w", err)
	}
	return true, nil
}

// MaxVisibleObservedAt returns the newest recorded_at among authorized matches
// at the fixed as-of, or nil when the scope is empty.
func (repository *ActivityProjectionRepository) MaxVisibleObservedAt(
	ctx context.Context,
	req activity.SubjectPageRequest,
) (*time.Time, error) {
	if ctx == nil || repository == nil || repository.pool == nil {
		return nil, activity.ErrInvalidListRequest
	}
	if !req.Query.Normalized() || req.Generation == 0 {
		return nil, activity.ErrInvalidListRequest
	}

	builder := newSubjectActivityFilterBuilder(req)
	builder.addSequenceBounds(1, req.AsOf)
	whereSQL, arguments := builder.whereClause()
	// Top-1 by recorded_at (not aggregate max) lets the observed index stop early
	// instead of scanning every authorized subject relation under the watermark.
	query := `
		select s.recorded_at
		from public.record_activity_subjects s
		where ` + whereSQL + `
		order by s.recorded_at desc, s.activity_id asc
		limit 1`
	var observed *time.Time
	err := repository.pool.QueryRow(ctx, query, arguments...).Scan(&observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("max visible activity observed_at: %w", err)
	}
	if observed == nil {
		return nil, nil
	}
	utc := observed.UTC()
	return &utc, nil
}

// LoadSourceStatuses returns safe per-source readiness without checkpoint
// sequences or worker clocks that would leak other authorization scopes.
func (repository *ActivityProjectionRepository) LoadSourceStatuses(
	ctx context.Context,
	generation uint64,
) ([]activity.SourceStatus, error) {
	if ctx == nil || repository == nil || repository.pool == nil || generation == 0 {
		return nil, activity.ErrInvalidListRequest
	}
	rows, err := repository.pool.Query(ctx, `
		select source_kind, caught_up, last_error_code,
		       lease_expires_at is not null and lease_expires_at < now() as lease_expired
		from public.record_activity_projection_checkpoints
		where project_id = $1 and projection_generation = $2
		order by source_kind`,
		recordplatform.ProjectIDDefault, generation,
	)
	if err != nil {
		return nil, fmt.Errorf("load activity source statuses: %w", err)
	}
	defer rows.Close()

	statuses := make([]activity.SourceStatus, 0, 5)
	seen := make(map[activity.SourceKind]bool, 5)
	for rows.Next() {
		var (
			kindRaw      string
			caughtUp     bool
			lastError    string
			leaseExpired bool
		)
		if err := rows.Scan(&kindRaw, &caughtUp, &lastError, &leaseExpired); err != nil {
			return nil, fmt.Errorf("scan activity source status: %w", err)
		}
		kind := activity.SourceKind(kindRaw)
		seen[kind] = true
		status := activity.SourceStatus{SourceKind: kind, State: "ready"}
		switch {
		case lastError != "":
			status.State = "unavailable"
			status.ReasonCode = "source_error"
		case leaseExpired:
			status.State = "stale"
			status.ReasonCode = "lease_expired"
		case !caughtUp:
			status.State = "stale"
			status.ReasonCode = "catching_up"
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load activity source statuses: %w", err)
	}

	for _, kind := range []activity.SourceKind{
		activity.SourceKindRecordDomain,
		activity.SourceKindEvidenceSnapshot,
		activity.SourceKindAssetHistory,
		activity.SourceKindMonitoringEvent,
		activity.SourceKindCommandAudit,
	} {
		if seen[kind] {
			continue
		}
		statuses = append(statuses, activity.SourceStatus{
			SourceKind: kind,
			State:      "stale",
			ReasonCode: "checkpoint_missing",
		})
	}
	return statuses, nil
}

func (repository *ActivityProjectionRepository) resolveSubjectPresence(
	ctx context.Context,
	req activity.SubjectPageRequest,
) (bool, map[string]string, error) {
	builder := newSubjectActivityFilterBuilder(req)
	builder.clearViewFilters()
	builder.addSequenceBounds(1, req.AsOf)
	whereSQL, arguments := builder.whereClause()

	var known bool
	if err := repository.pool.QueryRow(ctx, `
		select exists (
		  select 1
		  from public.record_activity_subjects s
		  where `+whereSQL+`
		)`, arguments...).Scan(&known); err != nil {
		return false, nil, fmt.Errorf("resolve activity subject presence: %w", err)
	}
	if !known {
		return false, nil, nil
	}

	identitySQL := `
		select s.identity_snapshot
		from public.record_activity_subjects s
		where ` + whereSQL + `
		  and s.tombstoned
		order by s.event_at desc, s.recorded_at desc, s.source_kind asc, s.activity_id asc
		limit 1`
	var raw []byte
	err := repository.pool.QueryRow(ctx, identitySQL, arguments...).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("resolve activity subject tombstone identity: %w", err)
	}
	identity := map[string]string{}
	if err := json.Unmarshal(raw, &identity); err != nil {
		return false, nil, fmt.Errorf("decode activity subject tombstone identity: %w", err)
	}
	return true, identity, nil
}

func (repository *ActivityProjectionRepository) loadActivityEvents(
	ctx context.Context,
	generation uint64,
	activityIDs []string,
) ([]activity.Event, error) {
	if len(activityIDs) == 0 {
		return []activity.Event{}, nil
	}
	rows, err := repository.pool.Query(ctx, `
		select p.activity_id, p.ingest_sequence, p.event_kind, p.event_at, p.recorded_at,
		       p.source_kind, p.source_event_id, p.source_version,
		       p.backfilled, p.actor_id, p.presentation_json, p.corrects_activity_id
		from public.record_activity_projection p
		join unnest($1::text[]) with ordinality as requested(activity_id, ordinal)
		  on requested.activity_id = p.activity_id
		where p.projection_generation = $2
		order by requested.ordinal`,
		activityIDs, generation,
	)
	if err != nil {
		return nil, fmt.Errorf("load activity events: %w", err)
	}
	defer rows.Close()

	events := make([]activity.Event, 0, len(activityIDs))
	for rows.Next() {
		var (
			event          activity.Event
			ingestSequence int64
			eventKind      string
			sourceKind     string
			sourceEventID  string
			sourceVersion  int64
			actorID        *string
			presentation   []byte
			corrects       *string
		)
		if err := rows.Scan(
			&event.ActivityID, &ingestSequence, &eventKind, &event.EventAt, &event.RecordedAt,
			&sourceKind, &sourceEventID, &sourceVersion,
			&event.Backfilled, &actorID, &presentation, &corrects,
		); err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		if err := json.Unmarshal(presentation, &event.Presentation); err != nil {
			return nil, fmt.Errorf("decode activity presentation for %s: %w", event.ActivityID, err)
		}
		event.EventKind = activity.EventKind(eventKind)
		event.SourceKind = activity.SourceKind(sourceKind)
		event.Source = activity.SourceIdentity{
			Kind:    activity.SourceKind(sourceKind),
			EventID: sourceEventID,
			Version: uint64(sourceVersion),
		}
		event.IngestSequence = uint64(ingestSequence)
		event.EventAt = event.EventAt.UTC()
		event.RecordedAt = event.RecordedAt.UTC()
		if actorID != nil {
			event.Actor = &activity.ActorSnapshot{ActorID: *actorID}
		}
		if corrects != nil {
			event.Corrects = *corrects
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load activity events: %w", err)
	}

	subjectsByActivity, err := repository.loadActivitySubjects(ctx, activityIDs)
	if err != nil {
		return nil, err
	}
	for index := range events {
		subjects := subjectsByActivity[events[index].ActivityID]
		if subjects == nil {
			subjects = []activity.SubjectSnapshot{}
		}
		events[index].Subjects = subjects
	}
	return events, nil
}

func (repository *ActivityProjectionRepository) loadActivitySubjects(
	ctx context.Context,
	activityIDs []string,
) (map[string][]activity.SubjectSnapshot, error) {
	rows, err := repository.pool.Query(ctx, `
		select activity_id, subject_kind, subject_source_id, relation_role, is_primary,
		       identity_snapshot, live_route, tombstoned
		from public.record_activity_subjects
		where activity_id = any($1::text[])
		order by activity_id, is_primary desc, relation_order asc, relation_role asc`,
		activityIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("load activity subjects: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]activity.SubjectSnapshot, len(activityIDs))
	for rows.Next() {
		var (
			activityID string
			snapshot   activity.SubjectSnapshot
			identity   []byte
			liveRoute  *string
		)
		if err := rows.Scan(
			&activityID, &snapshot.Kind, &snapshot.SourceID, &snapshot.Role, &snapshot.Primary,
			&identity, &liveRoute, &snapshot.Tombstoned,
		); err != nil {
			return nil, fmt.Errorf("scan activity subject: %w", err)
		}
		snapshot.Identity = map[string]string{}
		if err := json.Unmarshal(identity, &snapshot.Identity); err != nil {
			return nil, fmt.Errorf("decode activity subject identity: %w", err)
		}
		if liveRoute != nil {
			snapshot.LiveRoute = *liveRoute
		}
		out[activityID] = append(out[activityID], snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load activity subjects: %w", err)
	}
	return out, nil
}

func buildSubjectActivityCandidateQuery(req activity.SubjectPageRequest) (string, []any, error) {
	return buildSubjectActivityCandidateQueryWithSequenceBounds(req, 1, req.AsOf)
}

func buildSubjectActivityCandidateQueryWithSequenceBounds(
	req activity.SubjectPageRequest,
	fromInclusive uint64,
	throughInclusive uint64,
) (string, []any, error) {
	builder := newSubjectActivityFilterBuilder(req)
	builder.addSequenceBounds(fromInclusive, throughInclusive)
	if req.After != nil {
		builder.addKeyset(*req.After)
	}
	whereSQL, arguments := builder.whereClause()
	arguments = append(arguments, req.Limit)
	limitArg := len(arguments)

	// DISTINCT ON collapses multiple relation roles for one activity into a
	// single candidate before the timeline ORDER BY. Predicates above already
	// ran against the relation table, so this never filters after LIMIT.
	statement := `
		with matched as (
		  select distinct on (s.activity_id)
		    s.activity_id, s.event_at, s.recorded_at, s.source_kind
		  from public.record_activity_subjects s
		  where ` + whereSQL + `
		  order by s.activity_id, s.is_primary desc, s.relation_order asc, s.relation_role asc
		)
		select activity_id
		from matched
		order by event_at desc, recorded_at desc, source_kind asc, activity_id asc
		limit $` + fmt.Sprintf("%d", limitArg)
	return statement, arguments, nil
}

type subjectActivityFilterBuilder struct {
	req        activity.SubjectPageRequest
	conditions []string
	arguments  []any
	skipView   bool
}

func newSubjectActivityFilterBuilder(req activity.SubjectPageRequest) *subjectActivityFilterBuilder {
	builder := &subjectActivityFilterBuilder{req: req}
	builder.conditions = append(builder.conditions,
		fmt.Sprintf("s.subject_kind = $%d", builder.push(string(req.Query.Subject.Kind))),
		fmt.Sprintf("s.subject_source_id = $%d", builder.push(req.Query.Subject.SourceID)),
		fmt.Sprintf("s.projection_generation = $%d", builder.push(int64(req.Generation))),
	)
	if !req.AuthUnrestricted {
		digests := make([][]byte, 0, len(req.AllowedAuthDigests))
		for _, digest := range req.AllowedAuthDigests {
			copyDigest := digest
			digests = append(digests, copyDigest[:])
		}
		builder.conditions = append(builder.conditions,
			fmt.Sprintf("s.auth_scope_digest = any($%d::bytea[])", builder.push(digests)),
		)
	}
	return builder
}

func (builder *subjectActivityFilterBuilder) clearViewFilters() {
	builder.skipView = true
}

func (builder *subjectActivityFilterBuilder) addSequenceBounds(fromInclusive, throughInclusive uint64) {
	builder.conditions = append(builder.conditions,
		fmt.Sprintf("s.ingest_sequence >= $%d", builder.push(int64(fromInclusive))),
		fmt.Sprintf("s.ingest_sequence <= $%d", builder.push(int64(throughInclusive))),
	)
	if builder.skipView {
		return
	}
	builder.addViewPredicates()
}

func (builder *subjectActivityFilterBuilder) addViewPredicates() {
	query := builder.req.Query
	if kinds := query.ResolvedEventKinds(); kinds != nil {
		if len(kinds) == 0 {
			builder.conditions = append(builder.conditions, "false")
			return
		}
		values := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			values = append(values, string(kind))
		}
		builder.conditions = append(builder.conditions,
			fmt.Sprintf("s.event_kind = any($%d::text[])", builder.push(values)),
		)
	}
	if len(query.Sources) > 0 {
		values := make([]string, 0, len(query.Sources))
		for _, source := range query.Sources {
			values = append(values, string(source))
		}
		builder.conditions = append(builder.conditions,
			fmt.Sprintf("s.source_kind = any($%d::text[])", builder.push(values)),
		)
	}
	if !query.From.IsZero() {
		builder.conditions = append(builder.conditions,
			fmt.Sprintf("s.event_at >= $%d", builder.push(query.From.UTC())),
		)
	}
	if !query.To.IsZero() {
		builder.conditions = append(builder.conditions,
			fmt.Sprintf("s.event_at <= $%d", builder.push(query.To.UTC())),
		)
	}
	if query.Versions == activity.VersionsCurrent {
		asOf := builder.req.AsOf
		builder.conditions = append(builder.conditions, fmt.Sprintf(`(
		  s.revision_id is null
		  or exists (
		    select 1
		    from public.record_activity_revision_intervals i
		    where i.project_id = s.project_id
		      and i.projection_generation = s.projection_generation
		      and i.record_id = s.record_id
		      and i.revision_id = s.revision_id
		      and i.valid_from_ingest_sequence <= $%d
		      and (i.valid_to_ingest_sequence is null or i.valid_to_ingest_sequence > $%d)
		  )
		)`, builder.push(int64(asOf)), builder.push(int64(asOf))))
	}
}

func (builder *subjectActivityFilterBuilder) addKeyset(after activity.SortKey) {
	builder.conditions = append(builder.conditions, fmt.Sprintf(`(
	  s.event_at < $%d
	  or (s.event_at = $%d and s.recorded_at < $%d)
	  or (s.event_at = $%d and s.recorded_at = $%d and s.source_kind > $%d)
	  or (s.event_at = $%d and s.recorded_at = $%d and s.source_kind = $%d and s.activity_id > $%d)
	)`,
		builder.push(after.EventAt.UTC()),
		builder.push(after.EventAt.UTC()), builder.push(after.RecordedAt.UTC()),
		builder.push(after.EventAt.UTC()), builder.push(after.RecordedAt.UTC()), builder.push(string(after.SourceKind)),
		builder.push(after.EventAt.UTC()), builder.push(after.RecordedAt.UTC()),
		builder.push(string(after.SourceKind)), builder.push(after.ActivityID),
	))
}

func (builder *subjectActivityFilterBuilder) push(value any) int {
	builder.arguments = append(builder.arguments, value)
	return len(builder.arguments)
}

func (builder *subjectActivityFilterBuilder) whereClause() (string, []any) {
	if len(builder.conditions) == 0 {
		return "true", builder.arguments
	}
	joined := builder.conditions[0]
	for _, condition := range builder.conditions[1:] {
		joined += " and " + condition
	}
	return joined, builder.arguments
}

// ActivityLiveSubjectResolver adapts the records subject registry for the
// activity list path. Missing or unauthorized live subjects collapse to the
// unified not-found sentinel the service already maps to HTTP 404.
type ActivityLiveSubjectResolver struct {
	registry records.SubjectAdapterRegistry
}

// NewActivityLiveSubjectResolver wraps a closed subject adapter registry.
func NewActivityLiveSubjectResolver(registry records.SubjectAdapterRegistry) *ActivityLiveSubjectResolver {
	return &ActivityLiveSubjectResolver{registry: registry}
}

// ResolveLive returns the live identity bar for one subject reference.
func (resolver *ActivityLiveSubjectResolver) ResolveLive(
	ctx context.Context,
	actor recordauth.ActorScope,
	ref activity.SubjectRef,
) (activity.SubjectHeader, error) {
	if ctx == nil || resolver == nil {
		return activity.SubjectHeader{}, activity.ErrSubjectNotFound
	}
	resolved, err := resolver.registry.Resolve(ctx, actor, records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            ref.Kind,
		SourceID:        ref.SourceID,
		Role:            records.RelationRoleAffected,
		Primary:         true,
	})
	if err != nil {
		if errors.Is(err, ErrRecordSubjectNotFound) ||
			errors.Is(err, records.ErrInvalidSubjectReference) ||
			errors.Is(err, records.ErrSubjectAdapterNotFound) {
			return activity.SubjectHeader{}, activity.ErrSubjectNotFound
		}
		return activity.SubjectHeader{}, err
	}
	identity := resolved.IdentitySnapshot.Fields()
	if identity == nil {
		identity = map[string]string{}
	}
	return activity.SubjectHeader{
		Kind:      ref.Kind,
		SourceID:  ref.SourceID,
		Identity:  identity,
		LiveRoute: resolved.LiveRoute,
		Status:    activity.SubjectStatusLive,
	}, nil
}

// Ensure pool type is referenced when tests construct repositories without heads.
var _ = (*pgxpool.Pool)(nil)

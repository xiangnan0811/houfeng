package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/recordsearch"
)

var _ recordsearch.CandidateStore = (*PostgresRecordSearchStore)(nil)

// PostgresRecordSearchStore reads the search index. It answers with identity and
// sort position only: the index stores a visibility digest rather than the grants
// behind it, so deciding who may see a row stays with the authorized read path.
type PostgresRecordSearchStore struct {
	platform *PostgresRecordPlatformRepository
}

func NewPostgresRecordSearchStore(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordSearchStore {
	return &PostgresRecordSearchStore{platform: NewPostgresRecordPlatformRepository(pool, gate)}
}

// PublishedGeneration reports the generation currently serving reads. Returning
// zero rather than an error when none exists lets the service report an
// unavailable index in one place.
func (store *PostgresRecordSearchStore) PublishedGeneration(ctx context.Context) (uint64, error) {
	if ctx == nil || store == nil || store.platform == nil {
		return 0, fmt.Errorf("%w: search store", recordsearch.ErrInvalidSearchRequest)
	}
	var generation int64
	err := store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		row := transaction.tx.QueryRow(ctx, `
			select generation
			from public.record_search_generations
			where project_id = $1 and generation_state = 'published'`,
			recordplatform.ProjectIDDefault,
		)
		if err := row.Scan(&generation); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				generation = 0
				return nil
			}
			return fmt.Errorf("read published search generation: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if generation < 0 {
		return 0, fmt.Errorf("%w: published generation", recordsearch.ErrInvalidSearchRequest)
	}
	return uint64(generation), nil
}

func (store *PostgresRecordSearchStore) ListSearchCandidates(
	ctx context.Context,
	page recordsearch.CandidatePage,
) ([]recordsearch.Candidate, error) {
	if ctx == nil || store == nil || store.platform == nil || page.Generation == 0 ||
		page.Limit == 0 || uint64(page.Limit) > maxRecordReadPageSize {
		return nil, fmt.Errorf("%w: candidate page", recordsearch.ErrInvalidSearchRequest)
	}
	statement, arguments, err := buildRecordSearchCandidateQuery(page)
	if err != nil {
		return nil, err
	}

	var candidates []recordsearch.Candidate
	err = store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		// The generation is re-checked inside the read transaction because a
		// rebuild may have published a new one since the caller resolved it. A
		// silent empty page would look like "no matches" instead of "start over".
		var published bool
		if err := transaction.tx.QueryRow(ctx, `
			select true
			from public.record_search_generations
			where project_id = $1 and generation = $2 and generation_state = 'published'`,
			recordplatform.ProjectIDDefault, int64(page.Generation),
		).Scan(&published); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recordsearch.ErrGenerationSuperseded
			}
			return fmt.Errorf("verify published search generation: %w", err)
		}

		rows, err := transaction.tx.Query(ctx, statement, arguments...)
		if err != nil {
			return fmt.Errorf("list search candidates: %w", err)
		}
		if rows == nil {
			return fmt.Errorf("%w: candidate rows", recordsearch.ErrInvalidSearchRequest)
		}
		defer rows.Close()
		for rows.Next() {
			var candidate recordsearch.Candidate
			if err := rows.Scan(&candidate.RecordID, &candidate.UpdatedAt); err != nil {
				return fmt.Errorf("scan search candidate: %w", err)
			}
			candidate.UpdatedAt = candidate.UpdatedAt.UTC()
			if !validStoredRecordIdentity(candidate.RecordID, "rec_") || candidate.UpdatedAt.IsZero() {
				return fmt.Errorf("%w: candidate row", recordsearch.ErrInvalidSearchRequest)
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate search candidates: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, recordsearch.ErrGenerationSuperseded) {
			return nil, recordsearch.ErrGenerationSuperseded
		}
		return nil, err
	}
	return candidates, nil
}

// searchQueryBuilder accumulates predicates and their bound values together, so
// a filter can only ever reach SQL as a placeholder. No caller-supplied value is
// ever formatted into the statement text.
type searchQueryBuilder struct {
	arguments  []any
	conditions []string
}

func (builder *searchQueryBuilder) bind(value any) string {
	builder.arguments = append(builder.arguments, value)
	return "$" + strconv.Itoa(len(builder.arguments))
}

func (builder *searchQueryBuilder) require(condition string) {
	builder.conditions = append(builder.conditions, condition)
}

func buildRecordSearchCandidateQuery(page recordsearch.CandidatePage) (string, []any, error) {
	query := page.Query
	builder := &searchQueryBuilder{}
	builder.require("document.generation = " + builder.bind(int64(page.Generation)))
	builder.require("document.project_id = " + builder.bind(recordplatform.ProjectIDDefault))

	if text := query.Text(); text != "" {
		// The stored search_text column is already lowercased, so the term is
		// lowercased by the same database function rather than in Go: a Go-side
		// fold could disagree with the column and hide a document from its own
		// words. Wildcards in the term are escaped so a literal % stays literal.
		builder.require(fmt.Sprintf(
			"document.search_text like ('%%' || lower(%s) || '%%') escape '\\'",
			builder.bind(escapeSearchTextPattern(text)),
		))
	}
	if values := stringsFromSearchValues(query.Lifecycles()); len(values) > 0 {
		builder.require("document.lifecycle = any(" + builder.bind(values) + "::text[])")
	}
	if values := stringsFromSearchValues(query.Types()); len(values) > 0 {
		builder.require("document.record_type = any(" + builder.bind(values) + "::text[])")
	}
	if values := stringsFromSearchValues(query.Statuses()); len(values) > 0 {
		builder.require("document.business_status = any(" + builder.bind(values) + "::text[])")
	}
	if values := stringsFromSearchValues(query.StatusGroups()); len(values) > 0 {
		builder.require("document.status_group = any(" + builder.bind(values) + "::text[])")
	}
	if values := query.OwnerIDs(); len(values) > 0 {
		builder.require("document.owner_id = any(" + builder.bind(values) + "::text[])")
	}
	// Participants and tags are arrays on both sides, so overlap is the match:
	// one shared value is a hit, which is what OR-ing the filter values means.
	if values := query.ParticipantIDs(); len(values) > 0 {
		builder.require("document.participant_ids && " + builder.bind(values) + "::text[]")
	}
	if values := query.Tags(); len(values) > 0 {
		builder.require("document.tags && " + builder.bind(values) + "::text[]")
	}
	if err := requireSearchFollowUpState(builder, query.FollowUp()); err != nil {
		return "", nil, err
	}
	if err := requireSearchActionState(builder, query.Action()); err != nil {
		return "", nil, err
	}
	requireSearchTimeRange(builder, "document.occurred_at", query.Occurred())
	requireSearchTimeRange(builder, "document.record_updated_at", query.Updated())
	if err := requireSearchSubjects(builder, query.Subjects()); err != nil {
		return "", nil, err
	}

	comparison, direction := "<", "desc"
	if query.Sort() == recordsearch.SortUpdatedAsc {
		comparison, direction = ">", "asc"
	}
	if page.After != nil {
		if page.After.UpdatedAt.IsZero() || !validStoredRecordIdentity(page.After.RecordID, "rec_") {
			return "", nil, fmt.Errorf("%w: candidate position", recordsearch.ErrInvalidSearchRequest)
		}
		builder.require(fmt.Sprintf(
			"(document.record_updated_at, document.record_id) %s (%s, %s)",
			comparison,
			builder.bind(page.After.UpdatedAt.UTC()),
			builder.bind(page.After.RecordID),
		))
	}
	// A record under a committed or fenced deletion reservation is on its way
	// out, so it is withheld here for the same reason the record list withholds
	// it: the index entry outlives the reservation by design.
	builder.require(fmt.Sprintf(`not exists (
			select 1
			from public.deletion_reservations as reservation
			where reservation.project_id = %s
			  and reservation.object_kind = %s
			  and reservation.object_id = document.record_id
			  and reservation.state in ('fenced', 'committed')
		)`,
		builder.bind(recordplatform.ProjectIDDefault),
		builder.bind(recordObjectKind),
	))

	statement := fmt.Sprintf(`
		select document.record_id, document.record_updated_at
		from public.record_search_documents as document
		where %s
		order by document.record_updated_at %s, document.record_id %s
		limit %s`,
		strings.Join(builder.conditions, "\n		  and "),
		direction,
		direction,
		builder.bind(int64(page.Limit)),
	)
	return statement, builder.arguments, nil
}

func requireSearchFollowUpState(builder *searchQueryBuilder, state recordsearch.FollowUpState) error {
	switch state {
	case recordsearch.FollowUpAny:
		return nil
	case recordsearch.FollowUpNone:
		builder.require("document.follow_up_at is null")
		return nil
	case recordsearch.FollowUpScheduled:
		builder.require("document.follow_up_at >= transaction_timestamp()")
		return nil
	case recordsearch.FollowUpOverdue:
		builder.require("document.follow_up_at < transaction_timestamp()")
		return nil
	default:
		return fmt.Errorf("%w: follow up state", recordsearch.ErrInvalidSearchRequest)
	}
}

// requireSearchActionState reads the projected action counters rather than
// joining the live action tables. The counters are written by the same
// transaction that wrote the record, so the index answers from one generation
// instead of mixing indexed content with a moving join.
func requireSearchActionState(builder *searchQueryBuilder, state recordsearch.ActionState) error {
	switch state {
	case recordsearch.ActionAny:
		return nil
	case recordsearch.ActionNone:
		builder.require("document.open_action_count = 0")
		return nil
	case recordsearch.ActionOpen:
		builder.require("document.open_action_count > 0")
		return nil
	case recordsearch.ActionOverdue:
		builder.require("document.next_action_due_at < transaction_timestamp()")
		return nil
	default:
		return fmt.Errorf("%w: action state", recordsearch.ErrInvalidSearchRequest)
	}
}

// requireSearchTimeRange applies a half-open range. A null column never matches
// a bounded range, which is the intended reading: a record with no occurrence
// date is not "in" any occurrence window.
func requireSearchTimeRange(builder *searchQueryBuilder, column string, span recordsearch.TimeRange) {
	if span.From != nil {
		builder.require(column + " >= " + builder.bind(span.From.UTC()))
	}
	if span.To != nil {
		builder.require(column + " < " + builder.bind(span.To.UTC()))
	}
}

func requireSearchSubjects(builder *searchQueryBuilder, filters []recordsearch.SubjectFilter) error {
	if len(filters) == 0 {
		return nil
	}
	clauses := make([]string, 0, len(filters))
	for _, filter := range filters {
		conditions := []string{
			"subject.generation = document.generation",
			"subject.record_id = document.record_id",
			"subject.subject_kind = " + builder.bind(string(filter.Kind)),
		}
		if filter.Role != "" {
			conditions = append(conditions, "subject.relation_role = "+builder.bind(string(filter.Role)))
		}
		if filter.SourceID != "" {
			conditions = append(conditions, "subject.source_id = "+builder.bind(filter.SourceID))
		}
		switch filter.Placement {
		case recordsearch.SubjectPlacementAny:
		case recordsearch.SubjectPlacementPrimary:
			conditions = append(conditions, "subject.is_primary")
		case recordsearch.SubjectPlacementRelated:
			conditions = append(conditions, "not subject.is_primary")
		default:
			return fmt.Errorf("%w: subject placement", recordsearch.ErrInvalidSearchRequest)
		}
		clauses = append(clauses, fmt.Sprintf(`exists (
			select 1
			from public.record_search_subjects as subject
			where %s
		)`, strings.Join(conditions, " and ")))
	}
	// Subject filters are OR-ed like every other repeated field, so they are
	// grouped before joining the AND-ed conditions around them.
	builder.require("(" + strings.Join(clauses, "\n		  or ") + ")")
	return nil
}

// escapeSearchTextPattern makes the operator's term a literal. Without this a
// term containing % would match every document, and one containing _ would match
// an unintended character.
func escapeSearchTextPattern(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, character := range text {
		switch character {
		case '\\', '%', '_':
			builder.WriteRune('\\')
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

func stringsFromSearchValues[Value ~string](values []Value) []string {
	if len(values) == 0 {
		return nil
	}
	converted := make([]string, 0, len(values))
	for _, value := range values {
		converted = append(converted, string(value))
	}
	return converted
}

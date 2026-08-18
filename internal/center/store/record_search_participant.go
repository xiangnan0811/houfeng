package store

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordmarkdown"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
)

// recordSearchRevisionParticipant projects the committed revision into the
// search index inside the transaction that wrote it, so a record and its index
// entry become visible together or not at all.
//
// It writes every generation that a reader or a rebuild could consult: the
// published one that serves queries now, and the building one that a shadow
// rebuild will publish next. Writing only the published generation would let a
// rebuild that started before this commit publish an index missing this record.
type recordSearchRevisionParticipant struct{}

func NewRecordSearchRevisionParticipant() records.RevisionParticipant {
	return recordSearchRevisionParticipant{}
}

// Name orders this participant last among the built-in ones, which is what the
// projection needs: collaboration has already settled the fence epoch and the
// action rows this document reports.
func (recordSearchRevisionParticipant) Name() string { return "search" }

func (participant recordSearchRevisionParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	if ctx == nil || nilRecordSearchParticipantTx(tx) {
		return fmt.Errorf("%w: search projection dependency", records.ErrInvalidRevisionCommand)
	}
	facts, err := recordSearchDocumentFacts(ctx, tx, committed)
	if err != nil {
		return err
	}
	generations, err := upsertRecordSearchDocument(ctx, tx, facts)
	if err != nil {
		return err
	}
	return replaceRecordSearchSubjects(ctx, tx, facts, generations)
}

// recordSearchDocumentFacts assembles the projection from the revision that was
// just committed, reading only the two facts the revision does not carry: the
// record's creation instant and its delivery fence epoch.
func recordSearchDocumentFacts(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) (recordsearch.DocumentFacts, error) {
	result := committed.Result
	input := committed.Input
	if !records.ValidRecordRootID(result.RecordID) || !records.ValidRevisionID(result.RevisionID) {
		return recordsearch.DocumentFacts{}, fmt.Errorf("%w: search projection identity", records.ErrInvalidRevisionCommand)
	}

	var createdAt time.Time
	var fenceEpoch int64
	if err := tx.QueryRow(ctx, `
		select record.created_at, epoch.delivery_epoch
		from public.records as record
		join public.content_delivery_epochs as epoch
		  on epoch.project_id = record.project_id
		 and epoch.object_kind = 'record'
		 and epoch.object_id = record.record_id
		where record.record_id = $1 and record.project_id = $2`,
		result.RecordID, recordplatform.ProjectIDDefault,
	).Scan(&createdAt, &fenceEpoch); err != nil {
		return recordsearch.DocumentFacts{}, fmt.Errorf("read search projection record facts: %w", err)
	}
	if fenceEpoch < 0 {
		return recordsearch.DocumentFacts{}, fmt.Errorf("%w: search projection fence", records.ErrInvalidRevisionCommand)
	}

	subjects := make([]recordsearch.DocumentSubject, 0, len(input.Subjects()))
	for _, subject := range input.Subjects() {
		subjects = append(subjects, recordsearch.DocumentSubject{
			Kind:     subject.Kind,
			Role:     subject.Role,
			SourceID: subject.SourceID,
			Primary:  subject.Primary,
		})
	}
	participants := make([]string, 0, len(input.Participants()))
	for _, snapshot := range input.Participants() {
		participants = append(participants, snapshot.ParticipantID)
	}

	facts, err := recordsearch.NormalizeDocumentFacts(recordsearch.DocumentFactValues{
		RecordID:           result.RecordID,
		CurrentRevisionID:  result.RevisionID,
		LockVersion:        result.LockVersion,
		AuthorizationEpoch: result.AuthorizationEpoch,
		FenceEpoch:         uint64(fenceEpoch),
		Lifecycle:          result.Lifecycle,
		RecordType:         input.RecordType(),
		BusinessStatus:     input.BusinessStatus(),
		ImpactLevel:        input.ImpactLevel(),
		OwnerID:            input.OwnerID(),
		Title:              input.Title(),
		Text: recordsearch.DeriveDocumentTextFromMarkdown(
			input.BodyMarkdown(),
			recordSearchAuthorizedReferences(input),
		),
		Tags:             input.Tags(),
		ParticipantIDs:   participants,
		VisibilityKind:   string(input.VisibilityScope().Kind),
		VisibilityDigest: input.VisibilityScope().CanonicalHash,
		OccurredAt:       input.OccurredAt(),
		CompletedAt:      input.CompletedAt(),
		FollowUpAt:       input.FollowUpAt(),
		RecordCreatedAt:  createdAt,
		RecordUpdatedAt:  result.CommittedAt,
		Subjects:         subjects,
	})
	if err != nil {
		return recordsearch.DocumentFacts{}, fmt.Errorf("%w: %w", records.ErrInvalidRevisionCommand, err)
	}
	return facts, nil
}

// recordSearchAuthorizedReferences lets the body's own attachment and evidence
// references parse. Without them a body that embeds evidence would fall back to
// title-only text and quietly lose its prose from the index.
func recordSearchAuthorizedReferences(input records.CompleteRevisionInput) []recordmarkdown.DocumentReference {
	attachments := input.AttachmentIDs()
	snapshots := input.EvidenceSnapshotIDs()
	references := make([]recordmarkdown.DocumentReference, 0, len(attachments)+len(snapshots))
	for _, id := range attachments {
		references = append(references, recordmarkdown.DocumentReference{Kind: "attachment", ID: id})
	}
	for _, id := range snapshots {
		references = append(references, recordmarkdown.DocumentReference{Kind: "evidence", ID: id})
	}
	return references
}

// upsertRecordSearchDocument writes the document into every live generation and
// returns the generations it actually wrote. The lock-version fence is what
// makes a rebuild safe to run against a moving record set: a rebuild worker
// replaying an older snapshot cannot overwrite a newer live commit.
func upsertRecordSearchDocument(
	ctx context.Context,
	tx pgx.Tx,
	facts recordsearch.DocumentFacts,
) ([]int64, error) {
	digest := facts.Digest()
	visibility := facts.VisibilityDigest()
	rows, err := tx.Query(ctx, `
		insert into public.record_search_documents (
		  generation, record_id, project_id, current_revision_id, record_lock_version,
		  authorization_epoch, record_fence_epoch, lifecycle, record_type, business_status,
		  status_group, impact_level, owner_id, title, plain_text, tags, participant_ids,
		  visibility_kind, visibility_digest, open_action_count, next_action_due_at,
		  occurred_at, completed_at, follow_up_at, record_created_at, record_updated_at,
		  document_digest
		)
		select
		  generation.generation, $1, $2, $3, $4, $5, $6, $7, $8,
		  nullif($9, ''), nullif($10, ''), $11, nullif($12, ''), $13, $14, $15, $16, $17, $18,
		  coalesce(action.open_count, 0), action.next_due_at,
		  $19, $20, $21, $22, $23, $24
		from public.record_search_generations as generation
		left join lateral (
		  select count(*) as open_count, min(due_at) as next_due_at
		  from public.record_actions as record_action
		  where record_action.record_id = $1
		    and record_action.record_fence_epoch = $6
		    and record_action.status = 'open'
		) as action on true
		where generation.project_id = $2
		  and generation.generation_state in ('published', 'building')
		on conflict (generation, record_id) do update set
		  current_revision_id = excluded.current_revision_id,
		  record_lock_version = excluded.record_lock_version,
		  authorization_epoch = excluded.authorization_epoch,
		  record_fence_epoch = excluded.record_fence_epoch,
		  lifecycle = excluded.lifecycle,
		  record_type = excluded.record_type,
		  business_status = excluded.business_status,
		  status_group = excluded.status_group,
		  impact_level = excluded.impact_level,
		  owner_id = excluded.owner_id,
		  title = excluded.title,
		  plain_text = excluded.plain_text,
		  tags = excluded.tags,
		  participant_ids = excluded.participant_ids,
		  visibility_kind = excluded.visibility_kind,
		  visibility_digest = excluded.visibility_digest,
		  open_action_count = excluded.open_action_count,
		  next_action_due_at = excluded.next_action_due_at,
		  occurred_at = excluded.occurred_at,
		  completed_at = excluded.completed_at,
		  follow_up_at = excluded.follow_up_at,
		  record_created_at = excluded.record_created_at,
		  record_updated_at = excluded.record_updated_at,
		  document_digest = excluded.document_digest,
		  updated_at = transaction_timestamp()
		where excluded.record_lock_version >= public.record_search_documents.record_lock_version
		  and excluded.record_fence_epoch >= public.record_search_documents.record_fence_epoch
		returning generation`,
		facts.RecordID(), recordplatform.ProjectIDDefault, facts.CurrentRevisionID(),
		int64(facts.LockVersion()), int64(facts.AuthorizationEpoch()), int64(facts.FenceEpoch()),
		string(facts.Lifecycle()), string(facts.RecordType()), string(facts.BusinessStatus()),
		string(facts.StatusGroup()), string(facts.ImpactLevel()), facts.OwnerID(),
		facts.Title(), facts.Text(), facts.Tags(), facts.ParticipantIDs(),
		facts.VisibilityKind(), visibility[:],
		facts.OccurredAt(), facts.CompletedAt(), facts.FollowUpAt(),
		facts.RecordCreatedAt(), facts.RecordUpdatedAt(), digest[:],
	)
	if err != nil {
		return nil, fmt.Errorf("project search document: %w", err)
	}
	defer rows.Close()
	generations := make([]int64, 0, 2)
	for rows.Next() {
		var generation int64
		if err := rows.Scan(&generation); err != nil {
			return nil, fmt.Errorf("scan projected search generation: %w", err)
		}
		generations = append(generations, generation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read projected search generations: %w", err)
	}
	return generations, nil
}

// replaceRecordSearchSubjects rewrites the subject edges only for generations
// whose document this commit actually won, so a fenced-out stale write cannot
// leave the document from one revision beside the subjects of another.
func replaceRecordSearchSubjects(
	ctx context.Context,
	tx pgx.Tx,
	facts recordsearch.DocumentFacts,
	generations []int64,
) error {
	if len(generations) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		delete from public.record_search_subjects
		where record_id = $1 and generation = any($2::bigint[])`,
		facts.RecordID(), generations,
	); err != nil {
		return fmt.Errorf("clear projected search subjects: %w", err)
	}
	subjects := facts.Subjects()
	if len(subjects) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(subjects))
	roles := make([]string, 0, len(subjects))
	sourceIDs := make([]string, 0, len(subjects))
	primaries := make([]bool, 0, len(subjects))
	for _, subject := range subjects {
		kinds = append(kinds, string(subject.Kind))
		roles = append(roles, string(subject.Role))
		sourceIDs = append(sourceIDs, subject.SourceID)
		primaries = append(primaries, subject.Primary)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.record_search_subjects (
		  generation, record_id, subject_kind, relation_role, source_id, is_primary
		)
		select generation, $1, subject.kind, subject.role, subject.source_id, subject.is_primary
		from unnest($2::bigint[]) as generation
		cross join unnest($3::text[], $4::text[], $5::text[], $6::boolean[])
		  as subject(kind, role, source_id, is_primary)`,
		facts.RecordID(), generations, kinds, roles, sourceIDs, primaries,
	); err != nil {
		return fmt.Errorf("insert projected search subjects: %w", err)
	}
	return nil
}

// projectRecordSearchLifecycle follows an archive or restore into the index. The
// lifecycle path writes no revision, so participants never run for it; without
// this the index would keep serving the previous lifecycle and an archived
// record would still answer an active-only search.
//
// The projected content does not change, so this updates the control columns
// only and leaves the content digest alone. The same lock-version fence applies,
// so a lifecycle move cannot roll back a newer commit.
func projectRecordSearchLifecycle(
	ctx context.Context,
	tx pgx.Tx,
	recordID string,
	lifecycle records.Lifecycle,
	lockVersion uint64,
	authorizationEpoch uint64,
	changedAt time.Time,
) error {
	if ctx == nil || nilRecordSearchParticipantTx(tx) || !records.ValidRecordRootID(recordID) ||
		records.ValidateLifecycle(lifecycle) != nil || changedAt.IsZero() {
		return fmt.Errorf("%w: search lifecycle projection", records.ErrInvalidRecordLifecycleCommand)
	}
	if _, err := tx.Exec(ctx, `
		update public.record_search_documents as document
		set lifecycle = $3,
		    record_lock_version = $4,
		    authorization_epoch = $5,
		    record_updated_at = $6,
		    updated_at = transaction_timestamp()
		where document.record_id = $1
		  and $4 >= document.record_lock_version
		  and document.generation in (
		    select generation from public.record_search_generations
		    where project_id = $2 and generation_state in ('published', 'building')
		  )`,
		recordID, recordplatform.ProjectIDDefault, string(lifecycle),
		int64(lockVersion), int64(authorizationEpoch), changedAt.UTC(),
	); err != nil {
		return fmt.Errorf("project search lifecycle: %w", err)
	}
	return nil
}

func nilRecordSearchParticipantTx(tx pgx.Tx) bool {
	if tx == nil {
		return true
	}
	value := reflect.ValueOf(tx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

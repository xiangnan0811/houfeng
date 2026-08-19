package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type recordSearchProjectedDocument struct {
	generation   int64
	revisionID   string
	lockVersion  int64
	fenceEpoch   int64
	lifecycle    string
	recordType   string
	title        string
	plainText    string
	searchText   string
	tags         []string
	participants []string
	subjectCount int
	primaryCount int
}

// A committed revision has to be searchable in the generation that serves
// queries, and searchable by words from its body rather than only its title.
func TestPostgresIntegrationRecordSearchProjectionIndexesCommittedRevision(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-project", 1)
	seedRecordSearchGeneration(t, ctx, fixture.db, 1, "published")
	repository := newRecordsPostgresRepository(t, pool, NewRecordSearchRevisionParticipant())

	committed, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgsearchproject",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "磁盘故障排查"),
		"record-search-project-key",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	document := readRecordSearchProjectedDocument(t, ctx, fixture.db, committed.RecordID, 1)
	if document.revisionID != committed.RevisionID || document.lockVersion != int64(committed.LockVersion) {
		t.Fatalf("projected revision = %q/%d, want %q/%d",
			document.revisionID, document.lockVersion, committed.RevisionID, committed.LockVersion)
	}
	if document.lifecycle != "active" || document.recordType != string(records.RecordTypeNote) {
		t.Fatalf("projected lifecycle/type = %q/%q, want active/note", document.lifecycle, document.recordType)
	}
	if document.title != "磁盘故障排查" || document.plainText != "磁盘故障排查" {
		t.Fatalf("projected title/text = %q/%q, want the title and the flattened heading",
			document.title, document.plainText)
	}
	if document.searchText == "" || document.searchText != "磁盘故障排查 磁盘故障排查" {
		t.Fatalf("generated search text = %q, want the folded title and body", document.searchText)
	}
	if len(document.tags) != 1 || document.tags[0] != "postgres" ||
		len(document.participants) != 1 || document.participants[0] != "usr_bbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("projected tags/participants = %v/%v", document.tags, document.participants)
	}
	if document.subjectCount != 1 || document.primaryCount != 1 {
		t.Fatalf("projected subjects = %d total %d primary, want 1/1", document.subjectCount, document.primaryCount)
	}
}

// A shadow rebuild builds the next generation while the published one still
// serves reads, so a commit during the rebuild must land in both. Otherwise
// publishing the rebuilt generation would silently drop the record.
func TestPostgresIntegrationRecordSearchProjectionWritesPublishedAndBuildingGenerations(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-dual", 1)
	seedRecordSearchGeneration(t, ctx, fixture.db, 1, "published")
	seedRecordSearchGeneration(t, ctx, fixture.db, 2, "building")
	seedRecordSearchGeneration(t, ctx, fixture.db, 3, "superseded")
	repository := newRecordsPostgresRepository(t, pool, NewRecordSearchRevisionParticipant())

	committed, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgsearchdual",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Dual generation record"),
		"record-search-dual-key",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	for _, generation := range []int64{1, 2} {
		document := readRecordSearchProjectedDocument(t, ctx, fixture.db, committed.RecordID, generation)
		if document.revisionID != committed.RevisionID || document.subjectCount != 1 {
			t.Fatalf("generation %d document = %#v, want the committed revision with its subject", generation, document)
		}
	}
	var retiredCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_search_documents
		where record_id = $1 and generation = 3`, committed.RecordID).Scan(&retiredCount); err != nil {
		t.Fatalf("count superseded generation documents: %v", err)
	}
	if retiredCount != 0 {
		t.Fatalf("superseded generation document count = %d, want 0", retiredCount)
	}
}

// The lock-version fence is what makes a rebuild safe against a moving record
// set: a worker replaying an older snapshot of the same record must not roll the
// index back over a newer live commit.
func TestPostgresIntegrationRecordSearchProjectionFenceRejectsStaleWrite(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-fence", 1)
	seedRecordSearchGeneration(t, ctx, fixture.db, 1, "published")
	repository := newRecordsPostgresRepository(t, pool, NewRecordSearchRevisionParticipant())

	created, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgsearchfence",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Fence baseline"),
		"record-search-fence-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(create) error = %v", err)
	}
	updated, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		created.RecordID,
		created.RevisionID,
		created.LockVersion,
		created.AuthorizationEpoch,
		recordsPostgresCompleteRevisionInput(t, "Fence newer revision"),
		"record-search-fence-update",
	))
	if err != nil {
		t.Fatalf("CommitRevision(update) error = %v", err)
	}
	if updated.LockVersion <= created.LockVersion {
		t.Fatalf("update lock version = %d, want above %d", updated.LockVersion, created.LockVersion)
	}

	before := readRecordSearchProjectedDocument(t, ctx, fixture.db, created.RecordID, 1)
	if before.title != "Fence newer revision" {
		t.Fatalf("projected title before stale write = %q, want the newer revision", before.title)
	}
	applyStaleRecordSearchProjection(t, ctx, pool, created.RecordID, created.RevisionID, created.LockVersion)
	after := readRecordSearchProjectedDocument(t, ctx, fixture.db, created.RecordID, 1)
	if after.title != before.title || after.revisionID != before.revisionID ||
		after.lockVersion != before.lockVersion || after.subjectCount != before.subjectCount {
		t.Fatalf("stale write changed the document from %#v to %#v", before, after)
	}
}

// With no generation to write, a commit still has to succeed. A fresh install
// has no index yet, and refusing the write would make the whole records surface
// depend on search bootstrap order.
func TestPostgresIntegrationRecordSearchProjectionWithoutGenerationStillCommits(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-nogen", 1)
	repository := newRecordsPostgresRepository(t, pool, NewRecordSearchRevisionParticipant())

	committed, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgsearchnogen",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "No generation record"),
		"record-search-nogen-key",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	var documentCount, subjectCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_search_documents where record_id = $1),
		       (select count(*)::int from public.record_search_subjects where record_id = $1)`,
		committed.RecordID).Scan(&documentCount, &subjectCount); err != nil {
		t.Fatalf("count projected rows: %v", err)
	}
	if documentCount != 0 || subjectCount != 0 {
		t.Fatalf("projected rows = %d documents %d subjects, want none", documentCount, subjectCount)
	}
}

// Archiving is a lifecycle move on the same record, and the index has to follow
// it or an archived record would keep appearing in an active-only search.
func TestPostgresIntegrationRecordSearchProjectionFollowsLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-lifecycle", 1)
	seedRecordSearchGeneration(t, ctx, fixture.db, 1, "published")
	repository := newRecordsPostgresRepository(t, pool, NewRecordSearchRevisionParticipant())

	created, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgsearchlifecycle",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Lifecycle record"),
		"record-search-lifecycle-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(create) error = %v", err)
	}
	archived, err := repository.CommitRecordLifecycle(ctx, recordsPostgresLifecycleCommand(
		t,
		created,
		records.LifecycleArchived,
		"record-search-lifecycle-archive",
	))
	if err != nil {
		t.Fatalf("CommitRecordLifecycle(archive) error = %v", err)
	}

	document := readRecordSearchProjectedDocument(t, ctx, fixture.db, created.RecordID, 1)
	if document.lifecycle != string(records.LifecycleArchived) ||
		document.lockVersion != int64(archived.LockVersion) {
		t.Fatalf("projected lifecycle/lock = %q/%d, want archived/%d",
			document.lifecycle, document.lockVersion, archived.LockVersion)
	}

	restored, err := repository.CommitRecordLifecycle(ctx, recordsPostgresLifecycleCommandFromLifecycleResult(
		t,
		archived,
		records.LifecycleActive,
		"record-search-lifecycle-restore",
	))
	if err != nil {
		t.Fatalf("CommitRecordLifecycle(restore) error = %v", err)
	}
	restoredDocument := readRecordSearchProjectedDocument(t, ctx, fixture.db, created.RecordID, 1)
	if restoredDocument.lifecycle != string(records.LifecycleActive) ||
		restoredDocument.lockVersion != int64(restored.LockVersion) {
		t.Fatalf("restored lifecycle/lock = %q/%d, want active/%d",
			restoredDocument.lifecycle, restoredDocument.lockVersion, restored.LockVersion)
	}
}

// Bootstrap runs on every start, so it has to create the first published
// generation and then leave it alone. Publishing a second one would break the
// single-published-generation invariant that every query depends on.
func TestPostgresIntegrationEnsurePublishedRecordSearchGenerationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-bootstrap", 1)

	for attempt := range 3 {
		if err := EnsurePublishedRecordSearchGeneration(ctx, pool); err != nil {
			t.Fatalf("EnsurePublishedRecordSearchGeneration() attempt %d error = %v", attempt+1, err)
		}
	}
	var publishedCount, totalCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_search_generations where generation_state = 'published'),
		       (select count(*)::int from public.record_search_generations)`).Scan(&publishedCount, &totalCount); err != nil {
		t.Fatalf("count generations: %v", err)
	}
	if publishedCount != 1 || totalCount != 1 {
		t.Fatalf("generations = %d published of %d total, want exactly one", publishedCount, totalCount)
	}
}

// A generation number is never reused, so bootstrapping after a failed rebuild
// has to publish the next number rather than collide with the failed row.
func TestPostgresIntegrationEnsurePublishedRecordSearchGenerationSkipsUsedNumbers(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "record-search-bootstrap-skip", 1)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_search_generations (generation, generation_state, failure_reason)
		values (1, 'failed', 'rebuild_aborted')`); err != nil {
		t.Fatalf("seed failed generation: %v", err)
	}

	if err := EnsurePublishedRecordSearchGeneration(ctx, pool); err != nil {
		t.Fatalf("EnsurePublishedRecordSearchGeneration() error = %v", err)
	}
	var published int64
	if err := fixture.db.QueryRow(ctx, `
		select generation from public.record_search_generations
		where generation_state = 'published'`).Scan(&published); err != nil {
		t.Fatalf("read published generation: %v", err)
	}
	if published != 2 {
		t.Fatalf("published generation = %d, want 2 after a failed generation 1", published)
	}
}

func seedRecordSearchGeneration(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	generation int64,
	state string,
) {
	t.Helper()
	published := "null"
	superseded := "null"
	switch state {
	case "published":
		published = "now()"
	case "superseded":
		superseded = "now()"
	}
	if _, err := db.Exec(ctx, `
		insert into public.record_search_generations (generation, generation_state, published_at, superseded_at)
		values ($1, $2, `+published+`, `+superseded+`)`, generation, state); err != nil {
		t.Fatalf("seed search generation %d (%s): %v", generation, state, err)
	}
}

func readRecordSearchProjectedDocument(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	recordID string,
	generation int64,
) recordSearchProjectedDocument {
	t.Helper()
	document := recordSearchProjectedDocument{generation: generation}
	if err := db.QueryRow(ctx, `
		select document.current_revision_id, document.record_lock_version, document.record_fence_epoch,
		       document.lifecycle, document.record_type, document.title, document.plain_text,
		       document.search_text, document.tags, document.participant_ids,
		       (select count(*)::int from public.record_search_subjects as subject
		         where subject.generation = document.generation and subject.record_id = document.record_id),
		       (select count(*)::int from public.record_search_subjects as subject
		         where subject.generation = document.generation and subject.record_id = document.record_id
		           and subject.is_primary)
		from public.record_search_documents as document
		where document.record_id = $1 and document.generation = $2`, recordID, generation).Scan(
		&document.revisionID, &document.lockVersion, &document.fenceEpoch,
		&document.lifecycle, &document.recordType, &document.title, &document.plainText,
		&document.searchText, &document.tags, &document.participants,
		&document.subjectCount, &document.primaryCount,
	); err != nil {
		t.Fatalf("read projected search document for %q generation %d: %v", recordID, generation, err)
	}
	return document
}

// applyStaleRecordSearchProjection replays an older revision through the same
// participant, which is what a rebuild worker resuming from a stale snapshot
// does.
func applyStaleRecordSearchProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	recordID string,
	revisionID string,
	lockVersion uint64,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin stale projection transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := NewRecordSearchRevisionParticipant().ApplyRevision(ctx, tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID:           recordID,
			RevisionID:         revisionID,
			RevisionNo:         1,
			LockVersion:        lockVersion,
			AuthorizationEpoch: lockVersion,
			Lifecycle:          records.LifecycleActive,
			CommittedAt:        time.Now().UTC(),
		},
		Input: recordsPostgresCompleteRevisionInput(t, "Fence stale revision"),
	}); err != nil {
		t.Fatalf("ApplyRevision(stale) error = %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit stale projection transaction: %v", err)
	}
}

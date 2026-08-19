package migrate

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var recordActivityCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)
var recordActivityIndexPattern = regexp.MustCompile(`create (?:unique )?index if not exists ([a-z0-9_]+)`)

func recordActivityMigrationSQL(t *testing.T) string {
	t.Helper()
	payload, err := migrations.FS.ReadFile("0057_create_record_activity.sql")
	if err != nil {
		t.Fatalf("read 0057 record-activity migration: %v", err)
	}
	return strings.ToLower(string(payload))
}

func normalizedRecordActivityMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.Join(strings.Fields(recordActivityMigrationSQL(t)), " ")
}

func recordActivityTableDefinition(t *testing.T, table string) string {
	t.Helper()
	sql := recordActivityMigrationSQL(t)
	start := strings.Index(sql, "create table if not exists public."+table+" (")
	if start < 0 {
		t.Fatalf("0057 record-activity migration missing %s table", table)
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatalf("0057 record-activity %s table is unterminated", table)
	}
	return sql[start : start+end]
}

func normalizedRecordActivityTableDefinition(t *testing.T, table string) string {
	t.Helper()
	return strings.Join(strings.Fields(recordActivityTableDefinition(t, table)), " ")
}

func TestRecordActivityMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordActivityCreateTablePattern.FindAllStringSubmatch(recordActivityMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"record_activity_projection_heads",
		"record_activity_projection",
		"record_activity_subjects",
		"record_activity_projection_checkpoints",
		"record_activity_revision_intervals",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0057 record-activity tables = %#v, want %#v", got, want)
	}
}

// The projection is derived. If it grew an authoritative table, or duplicated a
// platform primitive, dropping and rebuilding it would lose real data.
func TestRecordActivityMigrationOwnsNoAuthoritativeOrDuplicateSurface(t *testing.T) {
	sql := recordActivityMigrationSQL(t)
	for _, forbidden := range []string{
		"record_activity_comments",
		"record_activity_actions",
		"record_activity_evidence",
		"record_activity_leases",
		"experience_logs",
	} {
		if strings.Contains(sql, "create table if not exists public."+forbidden) {
			t.Fatalf("0057 defines forbidden table %q", forbidden)
		}
	}
	if strings.Contains(sql, "create extension") {
		t.Fatal("0057 must not install an extension; the projection needs none")
	}
}

// The published head is the only thing that decides which sequence numbers a
// reader may see. One row per generation is what lets the final publish
// transaction take a row lock and serialize allocation, so a delayed low batch
// cannot be overtaken by a high one.
func TestRecordActivityMigrationKeepsOneLockableHeadPerGeneration(t *testing.T) {
	definition := normalizedRecordActivityTableDefinition(t, "record_activity_projection_heads")
	for _, want := range []string{
		"projection_generation bigint not null",
		"published_ingest_sequence bigint not null default 0",
		"primary key (project_id, projection_generation)",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_activity_projection_heads must contain %q\ngot: %s", want, definition)
		}
	}
	if !strings.Contains(definition, "check (published_ingest_sequence >= 0)") {
		t.Fatal("the published head must be constrained to a non-negative watermark")
	}
	indexes := strings.Join(recordActivityIndexNames(t), " ")
	if !strings.Contains(indexes, "uq_record_activity_projection_heads_active") {
		t.Fatalf("0057 must constrain the active generation; indexes = %s", indexes)
	}
}

// The unique key is what makes a retry idempotent. Without every component the
// projector would treat a correction as a duplicate of the fact it corrects.
func TestRecordActivityMigrationKeysProjectionOnFullSourceIdentity(t *testing.T) {
	sql := normalizedRecordActivityMigrationSQL(t)
	if want := "unique (source_kind, source_event_id, source_version, event_kind)"; !strings.Contains(sql, want) {
		t.Fatalf("0057 must key the projection on %q", want)
	}
	definition := normalizedRecordActivityTableDefinition(t, "record_activity_projection")
	for _, want := range []string{
		"activity_id text primary key check (activity_id ~ '^act_[a-z0-9]{1,64}$')",
		"ingest_sequence bigint not null",
		"event_at timestamptz not null",
		"recorded_at timestamptz not null",
		"backfilled boolean not null default false",
		"canonical_hash bytea not null check (octet_length(canonical_hash) = 32)",
		"auth_scope_digest bytea not null check (octet_length(auth_scope_digest) = 32)",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_activity_projection must contain %q\ngot: %s", want, definition)
		}
	}
	if !strings.Contains(definition, "unique (projection_generation, ingest_sequence)") {
		t.Fatal("the ingest sequence must be unique inside its generation")
	}
}

// A system fact must not become a second copy of record content or command
// output. The presentation column is a registered, bounded shape.
func TestRecordActivityMigrationStoresBoundedPresentationOnly(t *testing.T) {
	definition := normalizedRecordActivityTableDefinition(t, "record_activity_projection")
	if !strings.Contains(definition, "presentation_version bigint not null check (presentation_version = 1)") {
		t.Fatalf("presentation must be versioned and pinned\ngot: %s", definition)
	}
	if !strings.Contains(definition, "presentation_json jsonb not null") {
		t.Fatalf("presentation must be stored as a bounded jsonb document\ngot: %s", definition)
	}
	if !strings.Contains(definition, "pg_column_size(presentation_json) <= 4096") {
		t.Fatal("presentation must be size-bounded so a body or command output cannot fit")
	}
	for _, forbidden := range []string{
		"body_markdown",
		"stdout",
		"stderr",
		"raw_payload",
		"details",
	} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("record_activity_projection carries forbidden column %q", forbidden)
		}
	}
}

// Every filter dimension is redundantly stored on the relation rows so the
// planner can apply all of them before ORDER BY and LIMIT. Fetching 101 ids and
// filtering afterwards produces short pages on sparse filters.
func TestRecordActivityMigrationRedundantlyStoresEveryFilterDimensionOnRelations(t *testing.T) {
	definition := normalizedRecordActivityTableDefinition(t, "record_activity_subjects")
	for _, want := range []string{
		"subject_kind text not null",
		"subject_source_id text not null",
		"relation_role text not null",
		"is_primary boolean not null",
		"identity_snapshot jsonb not null",
		"tombstoned boolean not null default false",
		"event_kind text not null",
		"source_kind text not null",
		"event_at timestamptz not null",
		"recorded_at timestamptz not null",
		"ingest_sequence bigint not null",
		"auth_scope_digest bytea not null",
		"relation_hash bytea not null check (octet_length(relation_hash) = 32)",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_activity_subjects must contain %q\ngot: %s", want, definition)
		}
	}
	if !strings.Contains(definition, "record_id text") ||
		!strings.Contains(definition, "revision_id text") ||
		!strings.Contains(definition, "evidence_snapshot_id text") {
		t.Fatalf("relations must carry typed record/revision/evidence columns\ngot: %s", definition)
	}
}

// A subject that is deleted keeps its identity snapshot and its tombstone. A
// foreign key to the live subject table would delete the history instead.
func TestRecordActivityMigrationDoesNotCascadeFromLiveSubjectsOrSources(t *testing.T) {
	sql := normalizedRecordActivityMigrationSQL(t)
	for _, forbidden := range []string{
		"references public.vps_assets",
		"references public.monitoring_instances",
		"references public.targets",
		"references public.records(record_id)",
		"references public.evidence_snapshots",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0057 must not bind the projection to a live source with %q", forbidden)
		}
	}
	if !strings.Contains(sql, "on delete cascade") {
		t.Fatal("relation rows must cascade from their own projection parent")
	}
}

// The checkpoint must never claim completion from a sequence maximum. It stores
// the source head it actually scanned through.
func TestRecordActivityMigrationCheckpointsAgainstSourceHeadNotSequenceMax(t *testing.T) {
	definition := normalizedRecordActivityTableDefinition(t, "record_activity_projection_checkpoints")
	for _, want := range []string{
		"source_kind text not null",
		"recorded_through timestamptz",
		"lease_owner_id text",
		"lease_expires_at timestamptz",
		"last_success_at timestamptz",
		"attempt bigint not null default 0",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_activity_projection_checkpoints must contain %q\ngot: %s", want, definition)
		}
	}
	// A position expressed as an ingest sequence would be a position in our own
	// output rather than in the source, so a source row that commits after we
	// read past its neighbour could never be found again.
	if strings.Contains(definition, "ingest_sequence") {
		t.Fatalf("the checkpoint must position itself in the source, not in the projection\ngot: %s", definition)
	}
	if !strings.Contains(definition, "last_error_code text not null default ''") {
		t.Fatal("the checkpoint must carry a safe reason code rather than a free-form error")
	}
}

// versions=current resolves against intervals held at the page watermark. If it
// joined the live current-revision pointer instead, membership would change
// under the reader mid-pagination.
func TestRecordActivityMigrationTracksRevisionValidityAsClosedIntervals(t *testing.T) {
	definition := normalizedRecordActivityTableDefinition(t, "record_activity_revision_intervals")
	for _, want := range []string{
		"record_id text not null",
		"revision_id text not null",
		"valid_from_ingest_sequence bigint not null",
		"valid_to_ingest_sequence bigint",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_activity_revision_intervals must contain %q\ngot: %s", want, definition)
		}
	}
	if !strings.Contains(definition, "check (valid_to_ingest_sequence is null or valid_to_ingest_sequence > valid_from_ingest_sequence)") {
		t.Fatal("an interval must be non-empty and forward-going")
	}
}

// These are the shapes the query plan depends on. Losing one turns the
// watermark-bounded page into a scan.
func TestRecordActivityMigrationIndexesTheReviewedQueryShapes(t *testing.T) {
	got := recordActivityIndexNames(t)
	for _, want := range []string{
		"idx_record_activity_subjects_timeline",
		"idx_record_activity_subjects_event_kind",
		"idx_record_activity_subjects_source_kind",
		"idx_record_activity_subjects_watermark",
		"idx_record_activity_revision_intervals_validity",
		"idx_record_activity_projection_source",
	} {
		if !containsString(got, want) {
			t.Fatalf("0057 is missing index %q; indexes = %v", want, got)
		}
	}
	sql := normalizedRecordActivityMigrationSQL(t)
	if want := "(subject_kind, subject_source_id, event_at desc, recorded_at desc, source_kind, activity_id)"; !strings.Contains(sql, want) {
		t.Fatalf("the general timeline index must order by %q", want)
	}
}

func TestRecordActivityMigrationAddsNoUngrantableRuntimeSurface(t *testing.T) {
	sql := normalizedRecordActivityMigrationSQL(t)
	for _, forbidden := range []string{"create schema", "create role", "grant all", "security invoker"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0057 contains ungrantable surface %q", forbidden)
		}
	}
}

func recordActivityIndexNames(t *testing.T) []string {
	t.Helper()
	matches := recordActivityIndexPattern.FindAllStringSubmatch(recordActivityMigrationSQL(t), -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match[1])
	}
	return names
}

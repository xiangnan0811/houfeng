package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The static SQL assertions prove what the migration says. This one proves
// PostgreSQL agrees: the CHECK expressions are actually accepted, the covering
// indexes are actually buildable, and the constraints actually reject the rows
// they are meant to reject.
func TestPostgresIntegrationRecordActivitySchema(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() record-activity migration error = %v", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() record-activity exact repeat error = %v", err)
	}

	for _, table := range []string{
		"record_activity_projection_heads",
		"record_activity_projection",
		"record_activity_subjects",
		"record_activity_projection_checkpoints",
		"record_activity_revision_intervals",
		"record_activity_purge_receipts",
	} {
		assertSingleStringValue(t, ctx, db, "select to_regclass('public."+table+"')::text", table)
	}
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from public.schema_migrations
		where name = '0057_create_record_activity.sql'
	`, 1)

	// The timeline index carries the whole ordering tuple and covers the filter
	// columns. If it degraded to a prefix, a watermark-bounded page would sort
	// on the heap instead.
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_catalog.pg_indexes
		where schemaname = 'public'
		  and indexname = 'idx_record_activity_subjects_timeline'
		  and lower(indexdef) like '%event_at desc, recorded_at desc, source_kind, activity_id%'
		  and lower(indexdef) like '%include%auth_scope_digest%'
	`, 1)

	// Exactly one generation may be active, so the final publish transaction has
	// exactly one row to lock.
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_catalog.pg_indexes
		where schemaname = 'public'
		  and indexname = 'uq_record_activity_projection_heads_active'
		  and lower(indexdef) like '%unique%'
		  and lower(indexdef) like '%head_state%'
	`, 1)

	// The projection must not be deleted out from under a reader by a cascade
	// from a live business table. Only its own child relation rows cascade.
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_catalog.pg_constraint
		where contype = 'f'
		  and connamespace = 'public'::regnamespace
		  and conrelid in (
		    'public.record_activity_projection'::regclass,
		    'public.record_activity_subjects'::regclass,
		    'public.record_activity_projection_checkpoints'::regclass,
		    'public.record_activity_revision_intervals'::regclass
		  )
		  and confdeltype not in ('r', 'c')
	`, 0)
}

// A schema is only as good as what it refuses. Each case here is a rule the
// projector depends on: if the database accepted the row, a bug in the projector
// would become silently stored bad data instead of a failed transaction.
func TestPostgresIntegrationRecordActivityConstraintsRejectInvalidRows(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	mustExec(t, ctx, db, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 1, 0, 0)
	`)

	insert := `
		insert into public.record_activity_projection (
		  activity_id, projection_generation, ingest_sequence, event_kind, event_at, recorded_at,
		  source_kind, source_event_id, source_version, presentation_version, presentation_json,
		  auth_scope_digest, canonical_hash
		) values (
		  $1, 1, $2, $3, now(), now(),
		  $4, $5, $6, $7, $8::jsonb,
		  decode(repeat('61', 32), 'hex'), decode(repeat('62', 32), 'hex')
		)`
	validPresentation := `{"version":1,"title":"记录已修订"}`

	mustExec(t, ctx, db, insert,
		"act_aaaaaaaaaaaaaaaaaaaaaaaaaa", 1, "record_revised",
		"record_domain", "rac_first", 1, 1, validPresentation)

	rejections := map[string][]any{
		"malformed activity id": {
			"activity_XX", 2, "record_revised", "record_domain", "rac_second", 1, 1, validPresentation,
		},
		"unregistered source kind": {
			"act_bbbbbbbbbbbbbbbbbbbbbbbbbb", 3, "record_revised", "guesswork", "rac_third", 1, 1, validPresentation,
		},
		"zero source version": {
			"act_cccccccccccccccccccccccccc", 4, "record_revised", "record_domain", "rac_fourth", 0, 1, validPresentation,
		},
		"unregistered presentation version": {
			"act_dddddddddddddddddddddddddd", 5, "record_revised", "record_domain", "rac_fifth", 1, 2, validPresentation,
		},
		"presentation large enough to carry a body": {
			"act_eeeeeeeeeeeeeeeeeeeeeeeeee", 6, "record_revised", "record_domain", "rac_sixth", 1, 1,
			`{"version":1,"title":"` + strings.Repeat("超", 4096) + `"}`,
		},
		"duplicate source identity": {
			"act_ffffffffffffffffffffffffff", 7, "record_revised", "record_domain", "rac_first", 1, 1, validPresentation,
		},
		"duplicate ingest sequence in one generation": {
			"act_gggggggggggggggggggggggggg", 1, "record_revised", "record_domain", "rac_eighth", 1, 1, validPresentation,
		},
	}
	for name, args := range rejections {
		t.Run(name, func(t *testing.T) {
			if _, err := db.Exec(ctx, insert, args...); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// A record may hold only one open revision interval at a time. Two would make
// versions=current ambiguous at the same watermark.
func TestPostgresIntegrationRecordActivityAllowsOneOpenRevisionIntervalPerRecord(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	mustExec(t, ctx, db, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 1, 0, 0)
	`)

	insertInterval := `
		insert into public.record_activity_revision_intervals (
		  projection_generation, record_id, revision_id, revision_no,
		  valid_from_ingest_sequence, valid_to_ingest_sequence,
		  source_kind, source_event_id, source_version
		) values (1, $1, $2, $3, $4, $5, 'record_domain', $6, 1)`

	mustExec(t, ctx, db, insertInterval, "rec_one", "rrv_one", 1, 1, nil, "rac_i1")
	if _, err := db.Exec(ctx, insertInterval, "rec_one", "rrv_two", 2, 5, nil, "rac_i2"); err == nil {
		t.Fatal("a second open interval for the same record was accepted")
	}

	// Closing the first interval is what makes room for the next one.
	mustExec(t, ctx, db, `
		update public.record_activity_revision_intervals
		set valid_to_ingest_sequence = 5
		where record_id = 'rec_one' and revision_id = 'rrv_one'
	`)
	mustExec(t, ctx, db, insertInterval, "rec_one", "rrv_two", 2, 5, nil, "rac_i2")

	if _, err := db.Exec(ctx, insertInterval, "rec_two", "rrv_three", 1, 9, 9, "rac_i3"); err == nil {
		t.Fatal("an empty interval was accepted")
	}
}

func mustExec(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", strings.Join(strings.Fields(sql), " "), err)
	}
}

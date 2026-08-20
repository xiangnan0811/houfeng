package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func mustSeedVisibility(t *testing.T, kind recordauth.VisibilityKind, groupIDs []string) (scopeJSON []byte, digest []byte) {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:         recordauth.VisibilityScopeVersionV1,
		Kind:            kind,
		ProjectID:       recordauth.ProjectIDDefault,
		AllowedGroupIDs: groupIDs,
		PolicyVersion:   recordauth.PolicyVersionV1,
		PolicyRevision:  1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope(%s) error = %v", kind, err)
	}
	raw, err := json.Marshal(visibility)
	if err != nil {
		t.Fatalf("json.Marshal(visibility) error = %v", err)
	}
	digest = append([]byte(nil), visibility.CanonicalHash[:]...)
	return raw, digest
}

// seedRecordDomainActivityFixture builds the real shape the adapter reads: a
// record with two revisions carrying different subject sets, plus domain activity
// rows both with and without a revision of their own.
func seedRecordDomainActivityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	projectJSON, projectDigest := mustSeedVisibility(t, recordauth.VisibilityKindProject, nil)
	statements := []struct {
		sql       string
		arguments []any
	}{
		{sql: `insert into public.records (record_id) values ('rec_domainsrc')`},
		{
			sql: `insert into public.record_revisions (
				revision_id, record_id, revision_no, title, body_markdown,
				markdown_dialect_version, record_type, impact_level, visibility_scope,
				visibility_digest, author_id, canonical_hash, created_at
			) values
			  ('rrv_domainone', 'rec_domainsrc', 1, 'First', '', 1, 'note',
			   'informational', $1::jsonb, $2,
			   'usr_000000000000000000000001', decode(repeat('62', 32), 'hex'), $3),
			  ('rrv_domaintwo', 'rec_domainsrc', 2, 'Second', '', 1, 'note',
			   'informational', $1::jsonb, $2,
			   'usr_000000000000000000000001', decode(repeat('64', 32), 'hex'), $4)`,
			arguments: []any{projectJSON, projectDigest, base, base.Add(time.Minute)},
		},
		{sql: `insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values
		  ('rrv_domainone', 0, 1, 'vps', 'affected', 'vps_7c2a4e18b09d5f31', true,
		   '{"display_name": "hk-edge-01"}'::jsonb, '{}'::jsonb, decode(repeat('65', 32), 'hex')),
		  ('rrv_domaintwo', 0, 1, 'vps', 'affected', 'vps_7c2a4e18b09d5f31', true,
		   '{"display_name": "hk-edge-01"}'::jsonb, '{}'::jsonb, decode(repeat('66', 32), 'hex')),
		  ('rrv_domaintwo', 1, 1, 'target', 'context', 'tg_9f8e7d6c5b4a3210', false,
		   '{"display_name": "edge-https"}'::jsonb, '{}'::jsonb, decode(repeat('67', 32), 'hex'))`},
		{
			sql: `insert into public.record_domain_activities (
				activity_id, record_id, revision_id, event_kind, source_event_id,
				source_version, actor_id, authorization_epoch, record_lock_version,
				event_at, recorded_at
			) values
			  ('rac_domaincreate', 'rec_domainsrc', 'rrv_domainone', 'record_created',
			   'rrv_domainone', 1, 'usr_000000000000000000000001', 0, 1, $1, $1),
			  ('rac_domainrevise', 'rec_domainsrc', 'rrv_domaintwo', 'record_revised',
			   'rrv_domaintwo', 2, 'usr_000000000000000000000001', 0, 2, $2, $2),
			  ('rac_domainaction', 'rec_domainsrc', null, 'action_created',
			   'raev_domainone', 1, 'usr_000000000000000000000001', 0, 2, $3, $3)`,
			arguments: []any{base, base.Add(time.Minute), base.Add(2 * time.Minute)},
		},
	}
	// The primary-subject validator is a deferred constraint trigger, so a revision
	// and its subjects have to arrive in one transaction exactly as the production
	// commit path does them.
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	for _, statement := range statements {
		if _, err := transaction.Exec(ctx, statement.sql, statement.arguments...); err != nil {
			t.Fatalf("seed record domain activity fixture: %v", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
}

func newRecordDomainActivitySourceFixture(
	t *testing.T,
	ctx context.Context,
	base time.Time,
) (*RecordDomainActivitySource, *pgxpool.Pool) {
	t.Helper()
	pool := openActivityTestPool(t, ctx)
	seedRecordDomainActivityFixture(t, ctx, pool, base)
	source, err := NewRecordDomainActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new record domain activity source: %v", err)
	}
	return source, pool
}

func TestPostgresIntegrationRecordDomainSourceReadsSubjectsFromTheRightRevision(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newRecordDomainActivitySourceFixture(t, ctx, base)

	head, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("incremental head: %v", err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: head.RecordedThrough}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("scanned %d candidates, want 3", len(candidates))
	}

	byEventID := make(map[string]activity.CandidateEvent, len(candidates))
	for _, candidate := range candidates {
		byEventID[candidate.Source.EventID] = candidate
		if candidate.AuthScope.Visibility.Kind != recordauth.VisibilityKindProject {
			t.Fatalf("candidate %s AuthScope kind = %q, want project from revision",
				candidate.Source.EventID, candidate.AuthScope.Visibility.Kind)
		}
		project, err := activity.ProjectAuthScope(recordauth.ProjectIDDefault)
		if err != nil {
			t.Fatalf("ProjectAuthScope() error = %v", err)
		}
		if candidate.AuthScopeDigest() != project.Visibility.CanonicalHash {
			t.Fatalf("candidate %s auth digest does not match project visibility", candidate.Source.EventID)
		}
	}

	// Revision 1 had one subject; the event that belongs to it must not pick up
	// the subject the record gained later.
	created := byEventID["rac_domaincreate"]
	if len(created.Subjects) != 1 {
		t.Fatalf("record_created has %d subjects, want the 1 its revision had", len(created.Subjects))
	}
	if created.Subjects[0].SourceID != "vps_7c2a4e18b09d5f31" || !created.Subjects[0].Primary {
		t.Fatalf("record_created subject = %+v", created.Subjects[0])
	}
	if got := created.Subjects[0].Identity["display_name"]; got != "hk-edge-01" {
		t.Fatalf("identity snapshot display name = %q, want the captured one", got)
	}

	// Revision 2 added a second subject, in ordinal order.
	revised := byEventID["rac_domainrevise"]
	if len(revised.Subjects) != 2 {
		t.Fatalf("record_revised has %d subjects, want 2", len(revised.Subjects))
	}
	if !revised.Subjects[0].Primary || revised.Subjects[1].Primary {
		t.Fatalf("subjects arrived out of ordinal order: %+v", revised.Subjects)
	}

	// An action event names no revision of its own. Resolving the revision current
	// at its event time is what keeps it reachable instead of subject-less.
	action := byEventID["rac_domainaction"]
	if len(action.Subjects) != 2 {
		t.Fatalf("action_created has %d subjects, want the 2 from the revision current at its event time", len(action.Subjects))
	}
	if action.RevisionID != "rrv_domaintwo" {
		t.Fatalf("action revision = %q, want rrv_domaintwo", action.RevisionID)
	}
}

func TestPostgresIntegrationRecordDomainSourceHonoursBothWindowBounds(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newRecordDomainActivitySourceFixture(t, ctx, base)

	// A window that ends before the later rows must not return them: the projector
	// advances its position to the window bound and would skip whatever leaked.
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(30 * time.Second)}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "rac_domaincreate" {
		t.Fatalf("upper bound leaked %d rows: %+v", len(candidates), candidates)
	}

	// A window that starts after the first row must not return it either.
	candidates, err = source.ScanAfter(ctx, activity.ScanWindow{
		From:    base.Add(90 * time.Second),
		Through: base.Add(time.Hour),
	}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "rac_domainaction" {
		t.Fatalf("lower bound returned %d rows: %+v", len(candidates), candidates)
	}
}

func TestPostgresIntegrationRecordDomainSourceOrdersAndLimitsByRecordedTime(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newRecordDomainActivitySourceFixture(t, ctx, base)

	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(time.Hour)}, 2)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("scanned %d candidates, want the page limit of 2", len(candidates))
	}
	// Paging depends on the page being the oldest rows in recorded order.
	if candidates[0].RecordedAt.After(candidates[1].RecordedAt) {
		t.Fatalf("page is not ordered by recorded time: %s then %s", candidates[0].RecordedAt, candidates[1].RecordedAt)
	}
	if candidates[0].Source.EventID != "rac_domaincreate" || candidates[1].Source.EventID != "rac_domainrevise" {
		t.Fatalf("page returned the wrong rows: %q, %q", candidates[0].Source.EventID, candidates[1].Source.EventID)
	}
}

// The scan is the projector's hot path. Without an index on recorded time it
// sequentially scans the whole activity log on every pass.
func TestPostgresIntegrationRecordDomainSourceScanUsesTheRecordedTimeIndex(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, pool := newRecordDomainActivitySourceFixture(t, ctx, base)

	var indexed bool
	if err := pool.QueryRow(ctx, `
		select exists (
		  select 1 from pg_indexes
		  where schemaname = 'public'
		    and tablename = 'record_domain_activities'
		    and indexdef like '%recorded_at%'
		)`).Scan(&indexed); err != nil {
		t.Fatalf("inspect indexes: %v", err)
	}
	if !indexed {
		t.Fatalf("record_domain_activities has no index covering recorded_at, so every projection pass scans the whole log")
	}
}

// An export must not be told the source is settled on evidence that only proves
// where the incremental scan reached.
func TestPostgresIntegrationRecordDomainSourceReadinessNeedsASettledHead(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newRecordDomainActivitySourceFixture(t, ctx, base)

	incremental, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("incremental head: %v", err)
	}
	if incremental.SupportsCompletenessClaim() {
		t.Fatalf("the incremental head must not be usable as completeness evidence")
	}
	if _, err := source.Readiness(ctx, activity.ExportScope{}, incremental); err == nil {
		t.Fatalf("readiness accepted an incremental head as proof")
	}

	// The settled head needs transaction start times for every session. Whether
	// this role can see them is a deployment property, so both outcomes are
	// legitimate; what must not happen is a claim without the evidence.
	settled, err := source.AuthoritativeHead(ctx, activity.ExportScope{})
	if err != nil {
		if !incremental.RecordedThrough.IsZero() {
			t.Logf("settled head unavailable to this role, failing closed as designed: %v", err)
		}
		return
	}
	if !settled.SupportsCompletenessClaim() {
		t.Fatalf("AuthoritativeHead returned a head that cannot back a claim: %+v", settled)
	}
	readiness, err := source.Readiness(ctx, activity.ExportScope{}, settled)
	if err != nil {
		t.Fatalf("readiness rejected a settled head: %v", err)
	}
	if readiness.CaughtUp {
		t.Fatalf("readiness reported caught up before any projector checkpoint: %+v", readiness)
	}
	if readiness.Kind != activity.SourceKindRecordDomain {
		t.Fatalf("readiness kind = %s", readiness.Kind)
	}

	repository, err := NewActivityProjectionRepository(source.pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:            activity.SourceKindRecordDomain,
		RecordedThrough: settled.RecordedThrough,
		CaughtUp:        true,
	}); err != nil {
		t.Fatalf("save caught-up checkpoint: %v", err)
	}
	caughtUp, err := source.Readiness(ctx, activity.ExportScope{}, settled)
	if err != nil {
		t.Fatalf("readiness after checkpoint: %v", err)
	}
	if !caughtUp.CaughtUp || caughtUp.Kind != activity.SourceKindRecordDomain {
		t.Fatalf("readiness after checkpoint = %+v", caughtUp)
	}
}

// The whole point of the adapter is that the projector can consume it unchanged.
func TestPostgresIntegrationRecordDomainSourceProjectsThroughTheProjector(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	pool := openActivityTestPool(t, ctx)
	seedRecordDomainActivityFixture(t, ctx, pool, base)

	source, err := NewRecordDomainActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{source},
		Checkpoints: repository,
		Publisher:   repository,
		BatchSize:   2,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}

	report, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("pass failed: %v", err)
	}
	outcome, _ := report.Source(activity.SourceKindRecordDomain)
	if outcome.Inserted != 3 {
		t.Fatalf("inserted %d rows, want 3", outcome.Inserted)
	}
	if !outcome.CaughtUp {
		t.Fatalf("pass did not finish caught up: %+v", outcome)
	}

	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 3 {
		t.Fatalf("projected %d rows, want 3", len(projected))
	}
	assertContiguousFromOne(t, projected)

	// Relations are what a subject-scoped timeline query filters on, so the
	// two-subject revision has to contribute both.
	var relations int
	if err := pool.QueryRow(ctx, `select count(*) from public.record_activity_subjects`).Scan(&relations); err != nil {
		t.Fatalf("count relations: %v", err)
	}
	if relations != 5 {
		t.Fatalf("wrote %d relation rows, want 5 (1 + 2 + 2)", relations)
	}

	// Running it again must not add anything: the source key is stable.
	second, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if err := second.Err(); err != nil {
		t.Fatalf("second pass failed: %v", err)
	}
	if repeated, _ := second.Source(activity.SourceKindRecordDomain); repeated.Inserted != 0 {
		t.Fatalf("second pass inserted %d rows, want 0", repeated.Inserted)
	}
}

// The pointer that `versions=current` resolves has to come out of a real commit
// log, not just from hand-built candidates. This fixture has both commits and an
// action, so it also proves the action does not move the pointer.
func TestPostgresIntegrationRecordDomainSourceOpensIntervalsForItsCommits(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	pool := openActivityTestPool(t, ctx)
	seedRecordDomainActivityFixture(t, ctx, pool, base)

	source, err := NewRecordDomainActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{source},
		Checkpoints: repository,
		Publisher:   repository,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	report, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("pass failed: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, "rec_domainsrc")
	if len(intervals) != 2 {
		t.Fatalf("intervals = %+v, want one per commit and none for the action", intervals)
	}
	if intervals[0].revisionID != "rrv_domainone" || intervals[1].revisionID != "rrv_domaintwo" {
		t.Fatalf("intervals = %+v, want the two revisions in commit order", intervals)
	}
	if intervals[0].validTo == nil || *intervals[0].validTo != intervals[1].validFrom {
		t.Fatalf("intervals = %+v, want the first closed exactly where the second opens", intervals)
	}
	if intervals[1].validTo != nil {
		t.Fatalf("the latest revision must hold the pointer, got %+v", intervals[1])
	}
	if intervals[0].revisionNo != 1 || intervals[1].revisionNo != 2 {
		t.Fatalf("revision numbers = %d,%d, want 1,2 from the revision rows",
			intervals[0].revisionNo, intervals[1].revisionNo)
	}

	// A second pass inserts nothing, so it must not touch the pointer either.
	if _, err := projector.ProjectOnce(ctx, 1); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if again := loadRevisionIntervals(t, ctx, pool, 1, "rec_domainsrc"); len(again) != 2 ||
		again[1].validTo != nil {
		t.Fatalf("intervals after a repeat pass = %+v, want them unchanged", again)
	}
}

// Restricted revision visibility must survive projection as a distinct auth
// digest. A project viewer allowlist matches only project digests, so restricted
// rows stay hidden even though they share the same subject timeline.
func TestPostgresIntegrationRecordDomainRestrictedVisibilityHidesFromViewer(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	projectJSON, projectDigest := mustSeedVisibility(t, recordauth.VisibilityKindProject, nil)
	restrictedJSON, restrictedDigest := mustSeedVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_opsrestricted01"})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{sql: `insert into public.records (record_id) values ('rec_authproject'), ('rec_authrestrict')`},
		{
			sql: `insert into public.record_revisions (
				revision_id, record_id, revision_no, title, body_markdown,
				markdown_dialect_version, record_type, impact_level, visibility_scope,
				visibility_digest, author_id, canonical_hash, created_at
			) values
			  ('rrv_authproject', 'rec_authproject', 1, 'Public note', '', 1, 'note',
			   'informational', $1::jsonb, $2,
			   'usr_000000000000000000000001', decode(repeat('71', 32), 'hex'), $5),
			  ('rrv_authrestrict', 'rec_authrestrict', 1, 'Secret note', '', 1, 'note',
			   'informational', $3::jsonb, $4,
			   'usr_000000000000000000000001', decode(repeat('72', 32), 'hex'), $6)`,
			args: []any{projectJSON, projectDigest, restrictedJSON, restrictedDigest, base, base.Add(time.Minute)},
		},
		{sql: `insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values
		  ('rrv_authproject', 0, 1, 'vps', 'affected', 'vps_7c2a4e18b09d5f31', true,
		   '{"display_name": "hk-edge-01"}'::jsonb, '{}'::jsonb, decode(repeat('73', 32), 'hex')),
		  ('rrv_authrestrict', 0, 1, 'vps', 'affected', 'vps_7c2a4e18b09d5f31', true,
		   '{"display_name": "hk-edge-01"}'::jsonb, '{}'::jsonb, decode(repeat('74', 32), 'hex'))`},
		{
			sql: `insert into public.record_domain_activities (
				activity_id, record_id, revision_id, event_kind, source_event_id,
				source_version, actor_id, authorization_epoch, record_lock_version,
				event_at, recorded_at
			) values
			  ('rac_authproject', 'rec_authproject', 'rrv_authproject', 'record_created',
			   'rrv_authproject', 1, 'usr_000000000000000000000001', 0, 1, $1, $1),
			  ('rac_authrestrict', 'rec_authrestrict', 'rrv_authrestrict', 'record_created',
			   'rrv_authrestrict', 1, 'usr_000000000000000000000001', 0, 1, $2, $2)`,
			args: []any{base, base.Add(time.Minute)},
		},
	} {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed auth visibility fixture: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit auth visibility fixture: %v", err)
	}

	source, err := NewRecordDomainActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	head, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	scanned, err := source.ScanAfter(ctx, activity.ScanWindow{Through: head.RecordedThrough}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	byID := make(map[string]activity.CandidateEvent, len(scanned))
	for _, candidate := range scanned {
		byID[candidate.Source.EventID] = candidate
	}
	projectCandidate, ok := byID["rac_authproject"]
	if !ok {
		t.Fatalf("missing project candidate in %#v", byID)
	}
	restrictedCandidate, ok := byID["rac_authrestrict"]
	if !ok {
		t.Fatalf("missing restricted candidate in %#v", byID)
	}
	if projectCandidate.AuthScope.Visibility.Kind != recordauth.VisibilityKindProject {
		t.Fatalf("project candidate kind = %q", projectCandidate.AuthScope.Visibility.Kind)
	}
	if restrictedCandidate.AuthScope.Visibility.Kind != recordauth.VisibilityKindRestricted {
		t.Fatalf("restricted candidate kind = %q", restrictedCandidate.AuthScope.Visibility.Kind)
	}
	if projectCandidate.AuthScopeDigest() == restrictedCandidate.AuthScopeDigest() {
		t.Fatal("project and restricted digests must differ")
	}

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{source},
		Checkpoints: repository,
		Publisher:   repository,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	report, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("project once failed: %v", err)
	}
	outcome, _ := report.Source(activity.SourceKindRecordDomain)
	if outcome.Inserted != 2 {
		t.Fatalf("inserted %d, want 2", outcome.Inserted)
	}

	publishedHead, err := repository.LoadPublishedHead(ctx)
	if err != nil {
		t.Fatalf("load published head: %v", err)
	}
	query, err := activity.NormalizeQuery(activity.Query{
		Subject: activity.SubjectRef{
			Kind: records.SubjectKindVPS, SourceID: "vps_7c2a4e18b09d5f31",
		},
		View:  activity.ViewActivity,
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("normalize query: %v", err)
	}

	viewerFilter, err := activity.AuthFilterForActor(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000002",
		Role:      recordauth.RoleViewer,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("AuthFilterForActor(viewer) error = %v", err)
	}
	viewerPage, err := repository.ListSubjectPage(ctx, activity.SubjectPageRequest{
		Query:              query,
		Generation:         publishedHead.Generation,
		AsOf:               publishedHead.PublishedIngestSequence,
		Limit:              50,
		AuthUnrestricted:   viewerFilter.Unrestricted,
		AllowedAuthDigests: viewerFilter.AllowedAuthDigests,
	})
	if err != nil {
		t.Fatalf("viewer ListSubjectPage: %v", err)
	}
	if len(viewerPage.Events) != 1 {
		t.Fatalf("viewer saw %d events, want only the project-visible one", len(viewerPage.Events))
	}
	if viewerPage.Events[0].ActivityID != projectCandidate.ActivityID {
		t.Fatalf("viewer event = %s, want %s", viewerPage.Events[0].ActivityID, projectCandidate.ActivityID)
	}

	adminPage, err := repository.ListSubjectPage(ctx, activity.SubjectPageRequest{
		Query:            query,
		Generation:       publishedHead.Generation,
		AsOf:             publishedHead.PublishedIngestSequence,
		Limit:            50,
		AuthUnrestricted: true,
	})
	if err != nil {
		t.Fatalf("admin ListSubjectPage: %v", err)
	}
	if len(adminPage.Events) != 2 {
		t.Fatalf("admin saw %d events, want both project and restricted", len(adminPage.Events))
	}
}

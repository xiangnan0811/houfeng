package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
)

type recordSearchReadFixture struct {
	fixture recordPlatformPostgresFixture
	store   *PostgresRecordSearchStore
	byName  map[string]string
}

type recordSearchSeedSubject struct {
	sourceID string
	role     records.RelationRole
	primary  bool
}

type recordSearchSeedRecord struct {
	name       string
	title      string
	body       string
	recordType records.RecordType
	status     records.BusinessStatus
	tags       []string
	ownerID    string
	subjects   []recordSearchSeedSubject
	occurredAt *time.Time
	followUpAt *time.Time
	archive    bool
}

// A revision carries exactly one primary subject, so a record that should also
// be reachable through a related edge needs a second, non-primary subject on a
// different source.
const (
	recordSearchPrimaryVPSID = testStoreRecordVPSID
	recordSearchOtherVPSID   = "vps_00000000000000ff"
	recordSearchNetworkVPSID = "vps_0000000000000abc"
)

// The seeded corpus deliberately varies one dimension at a time so a filter test
// can name exactly which record it expects and why.
func recordSearchSeedCorpus() []recordSearchSeedRecord {
	occurred := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	overdue := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	scheduled := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	return []recordSearchSeedRecord{
		{
			name: "disk", title: "磁盘故障排查", body: "根因是 NVMe 掉盘，已更换硬盘\n",
			recordType: records.RecordTypeTroubleshooting, status: records.StatusInvestigating,
			tags: []string{"disk", "nvme"}, ownerID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
			subjects: []recordSearchSeedSubject{
				{sourceID: recordSearchPrimaryVPSID, role: records.RelationRoleAffected, primary: true},
			},
			occurredAt: &occurred, followUpAt: &overdue,
		},
		{
			name: "network", title: "网络抖动分析", body: "上游 100% 丢包，运营商侧问题\n",
			recordType: records.RecordTypeTroubleshooting, status: records.StatusVerifying,
			tags: []string{"network"}, ownerID: "usr_cccccccccccccccccccccccc",
			subjects: []recordSearchSeedSubject{
				{sourceID: recordSearchNetworkVPSID, role: records.RelationRoleAffected, primary: true},
				{sourceID: recordSearchPrimaryVPSID, role: records.RelationRoleContext},
			},
			followUpAt: &scheduled,
		},
		{
			name: "migration", title: "Migration to new provider", body: "moved the disk image\n",
			recordType: records.RecordTypeMigration, status: records.StatusPlanned,
			tags: []string{"provider"}, ownerID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
			subjects: []recordSearchSeedSubject{
				{sourceID: recordSearchOtherVPSID, role: records.RelationRoleAffected, primary: true},
			},
		},
		{
			name: "archived", title: "退役磁盘记录", body: "旧机器已退役\n",
			recordType: records.RecordTypeNote,
			tags:       []string{"disk"}, ownerID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
			subjects: []recordSearchSeedSubject{
				{sourceID: recordSearchPrimaryVPSID, role: records.RelationRoleAffected, primary: true},
			},
			archive: true,
		},
	}
}

func newRecordSearchReadFixture(t *testing.T, ctx context.Context, name string) recordSearchReadFixture {
	t.Helper()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, name, 2)
	seedRecordSearchGeneration(t, ctx, fixture.db, 1, "published")
	repository := newRecordsPostgresRepository(t, pool, NewRecordSearchRevisionParticipant())

	byName := make(map[string]string, len(recordSearchSeedCorpus()))
	for index, seed := range recordSearchSeedCorpus() {
		committed, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
			t,
			recordplatform.OperationKindRecordCreate,
			"rec_pgsearchread"+seed.name,
			"",
			0,
			0,
			recordSearchSeedInput(t, seed),
			"record-search-read-"+seed.name,
		))
		if err != nil {
			t.Fatalf("CommitRevision(%s) error = %v", seed.name, err)
		}
		byName[seed.name] = committed.RecordID
		if seed.archive {
			if _, err := repository.CommitRecordLifecycle(ctx, recordsPostgresLifecycleCommand(
				t, committed, records.LifecycleArchived, "record-search-read-archive-"+seed.name,
			)); err != nil {
				t.Fatalf("CommitRecordLifecycle(%s) error = %v", seed.name, err)
			}
		}
		// Distinct update instants make the keyset order deterministic, which is
		// what a paging assertion needs to mean anything. The creation instant
		// moves with them because the table requires updated at or after created.
		if _, err := fixture.db.Exec(ctx, `
			update public.record_search_documents
			set record_created_at = $2, record_updated_at = $3
			where record_id = $1`,
			committed.RecordID,
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index)*time.Hour),
		); err != nil {
			t.Fatalf("normalize projected update instant for %q: %v", seed.name, err)
		}
	}
	return recordSearchReadFixture{
		fixture: fixture,
		store:   NewPostgresRecordSearchStore(pool, allowRecordPlatformAdmissionGate),
		byName:  byName,
	}
}

func recordSearchSeedInput(t *testing.T, seed recordSearchSeedRecord) records.CompleteRevisionInput {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      recordauth.ProjectIDDefault,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	subjects := make([]records.RevisionSubject, 0, len(seed.subjects))
	for _, subject := range seed.subjects {
		authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
			Version:      recordauth.SourceAuthorizationVersionV1,
			Kind:         recordauth.SourceKindVPS,
			SourceID:     subject.sourceID,
			State:        recordauth.SourceStateLive,
			CaptureScope: visibility,
			CurrentScope: &visibility,
		})
		if err != nil {
			t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
		}
		subjects = append(subjects, records.RevisionSubject{
			RegistryVersion:      records.SubjectRegistryVersionV1,
			Kind:                 records.SubjectKindVPS,
			Role:                 subject.role,
			SourceID:             subject.sourceID,
			Primary:              subject.primary,
			IdentitySnapshot:     map[string]string{"display_name": "Search VPS"},
			CaptureAuthorization: authorization,
		})
	}
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title:                  seed.title,
		BodyMarkdown:           seed.body,
		MarkdownDialectVersion: records.MarkdownDialectVersionV1,
		RecordType:             seed.recordType,
		BusinessStatus:         seed.status,
		ImpactLevel:            "informational",
		OccurredAt:             seed.occurredAt,
		FollowUpAt:             seed.followUpAt,
		VisibilityScope:        visibility,
		Subjects:               subjects,
		Tags:                   seed.tags,
		OwnerID:                seed.ownerID,
		Participants: []records.RevisionParticipantSnapshot{{
			ParticipantID:    "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			IdentitySnapshot: map[string]string{"display_name": "Search Operator"},
		}},
		AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput(%s) error = %v", seed.name, err)
	}
	return input
}

func mustRecordSearchQuery(t *testing.T, values recordsearch.QueryValues) recordsearch.Query {
	t.Helper()
	if values.PageSize == 0 {
		values.PageSize = 50
	}
	query, err := recordsearch.NormalizeQuery(values)
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}
	return query
}

func (fixture recordSearchReadFixture) names(t *testing.T, candidates []recordsearch.Candidate) []string {
	t.Helper()
	byID := make(map[string]string, len(fixture.byName))
	for name, id := range fixture.byName {
		byID[id] = name
	}
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name, known := byID[candidate.RecordID]
		if !known {
			t.Fatalf("candidate %q is not a seeded record", candidate.RecordID)
		}
		names = append(names, name)
	}
	return names
}

func assertRecordSearchNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

// Every filter has to be applied by the index rather than after hydration, so
// each one is asserted against a corpus where only the expected records qualify.
func TestPostgresIntegrationRecordSearchStoreAppliesFiltersInSQL(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordSearchReadFixture(t, ctx, "record-search-filters")
	occurredFrom := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	occurredTo := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		values recordsearch.QueryValues
		want   []string
	}{
		{
			name:   "no filter returns every generation document newest first",
			values: recordsearch.QueryValues{},
			want:   []string{"archived", "migration", "network", "disk"},
		},
		{
			// A CJK term has to match inside the text, which is the whole reason
			// the index is trigram rather than tokenized full text.
			name:   "chinese infix text",
			values: recordsearch.QueryValues{Text: "磁盘"},
			want:   []string{"archived", "disk"},
		},
		{
			name:   "chinese text found only in the body",
			values: recordsearch.QueryValues{Text: "掉盘"},
			want:   []string{"disk"},
		},
		{
			// The stored column is lowercased, so a capitalized term must still
			// match or a document becomes unfindable by its own title.
			name:   "case folded latin text",
			values: recordsearch.QueryValues{Text: "MIGRATION"},
			want:   []string{"migration"},
		},
		{
			// A wildcard typed by an operator is a literal, not a pattern.
			name:   "percent in the term stays literal",
			values: recordsearch.QueryValues{Text: "100%"},
			want:   []string{"network"},
		},
		{
			name:   "record type",
			values: recordsearch.QueryValues{Types: []records.RecordType{records.RecordTypeTroubleshooting}},
			want:   []string{"network", "disk"},
		},
		{
			name:   "business status",
			values: recordsearch.QueryValues{Statuses: []records.BusinessStatus{records.StatusInvestigating}},
			want:   []string{"disk"},
		},
		{
			name:   "status group",
			values: recordsearch.QueryValues{StatusGroups: []records.StatusGroup{records.StatusGroupPending}},
			want:   []string{"migration"},
		},
		{
			name:   "lifecycle",
			values: recordsearch.QueryValues{Lifecycles: []records.Lifecycle{records.LifecycleArchived}},
			want:   []string{"archived"},
		},
		{
			name:   "owner",
			values: recordsearch.QueryValues{OwnerIDs: []string{"usr_cccccccccccccccccccccccc"}},
			want:   []string{"network"},
		},
		{
			name:   "tag overlap",
			values: recordsearch.QueryValues{Tags: []string{"disk"}},
			want:   []string{"archived", "disk"},
		},
		{
			name:   "participant overlap",
			values: recordsearch.QueryValues{ParticipantIDs: []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}},
			want:   []string{"archived", "migration", "network", "disk"},
		},
		{
			name:   "overdue follow up",
			values: recordsearch.QueryValues{FollowUp: recordsearch.FollowUpOverdue},
			want:   []string{"disk"},
		},
		{
			name:   "scheduled follow up",
			values: recordsearch.QueryValues{FollowUp: recordsearch.FollowUpScheduled},
			want:   []string{"network"},
		},
		{
			name:   "absent follow up",
			values: recordsearch.QueryValues{FollowUp: recordsearch.FollowUpNone},
			want:   []string{"archived", "migration"},
		},
		{
			name: "occurred range",
			values: recordsearch.QueryValues{
				Occurred: recordsearch.TimeRange{From: &occurredFrom, To: &occurredTo},
			},
			want: []string{"disk"},
		},
		{
			name: "subject source",
			values: recordsearch.QueryValues{Subjects: []recordsearch.SubjectFilter{{
				Kind: records.SubjectKindVPS, SourceID: recordSearchOtherVPSID,
			}}},
			want: []string{"migration"},
		},
		{
			name: "subject role narrows the same source",
			values: recordsearch.QueryValues{Subjects: []recordsearch.SubjectFilter{{
				Kind: records.SubjectKindVPS, SourceID: recordSearchPrimaryVPSID,
				Role: records.RelationRoleContext,
			}}},
			want: []string{"network"},
		},
		{
			name: "primary placement excludes related subjects",
			values: recordsearch.QueryValues{Subjects: []recordsearch.SubjectFilter{{
				Kind: records.SubjectKindVPS, SourceID: recordSearchPrimaryVPSID,
				Placement: recordsearch.SubjectPlacementPrimary,
			}}},
			want: []string{"archived", "disk"},
		},
		{
			// Distinct fields are AND-ed, so narrowing by two of them has to
			// intersect rather than union.
			name: "distinct filters intersect",
			values: recordsearch.QueryValues{
				Text:  "磁盘",
				Types: []records.RecordType{records.RecordTypeTroubleshooting},
			},
			want: []string{"disk"},
		},
		{
			name:   "ascending sort reverses the order",
			values: recordsearch.QueryValues{Sort: recordsearch.SortUpdatedAsc},
			want:   []string{"disk", "network", "migration", "archived"},
		},
		{
			name:   "unmatched term returns nothing",
			values: recordsearch.QueryValues{Text: "没有这个词"},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := fixture.store.ListSearchCandidates(ctx, recordsearch.CandidatePage{
				Query:      mustRecordSearchQuery(t, tt.values),
				Generation: 1,
				Limit:      50,
			})
			if err != nil {
				t.Fatalf("ListSearchCandidates() error = %v", err)
			}
			assertRecordSearchNames(t, fixture.names(t, candidates), tt.want...)
		})
	}
}

// Resuming has to be a total order over the whole result, so a page boundary
// must neither repeat nor skip a record.
func TestPostgresIntegrationRecordSearchStorePagesWithoutGapsOrRepeats(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordSearchReadFixture(t, ctx, "record-search-paging")
	query := mustRecordSearchQuery(t, recordsearch.QueryValues{})

	seen := make([]string, 0, 4)
	var after *recordsearch.SortKey
	for page := 0; page < 6; page++ {
		candidates, err := fixture.store.ListSearchCandidates(ctx, recordsearch.CandidatePage{
			Query:      query,
			Generation: 1,
			After:      after,
			Limit:      2,
		})
		if err != nil {
			t.Fatalf("ListSearchCandidates(page %d) error = %v", page, err)
		}
		if len(candidates) == 0 {
			break
		}
		seen = append(seen, fixture.names(t, candidates)...)
		last := candidates[len(candidates)-1]
		after = &recordsearch.SortKey{UpdatedAt: last.UpdatedAt, RecordID: last.RecordID}
	}
	assertRecordSearchNames(t, seen, "archived", "migration", "network", "disk")
}

// The published generation is what a cursor binds to, so the store has to report
// it rather than let a caller guess.
func TestPostgresIntegrationRecordSearchStoreReportsPublishedGeneration(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordSearchReadFixture(t, ctx, "record-search-generation")

	generation, err := fixture.store.PublishedGeneration(ctx)
	if err != nil {
		t.Fatalf("PublishedGeneration() error = %v", err)
	}
	if generation != 1 {
		t.Fatalf("PublishedGeneration() = %d, want 1", generation)
	}
}

// Reading a generation that is no longer published must be an explicit failure.
// An empty page would be indistinguishable from "this query matched nothing",
// and the caller would report no results instead of restarting.
func TestPostgresIntegrationRecordSearchStoreRejectsSupersededGeneration(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordSearchReadFixture(t, ctx, "record-search-superseded")
	if _, err := fixture.fixture.db.Exec(ctx, `
		update public.record_search_generations
		set generation_state = 'superseded', published_at = null, superseded_at = now()
		where generation = 1`); err != nil {
		t.Fatalf("supersede generation: %v", err)
	}

	if _, err := fixture.store.ListSearchCandidates(ctx, recordsearch.CandidatePage{
		Query:      mustRecordSearchQuery(t, recordsearch.QueryValues{}),
		Generation: 1,
		Limit:      10,
	}); !errors.Is(err, recordsearch.ErrGenerationSuperseded) {
		t.Fatalf("ListSearchCandidates() error = %v, want %v", err, recordsearch.ErrGenerationSuperseded)
	}
	generation, err := fixture.store.PublishedGeneration(ctx)
	if err != nil {
		t.Fatalf("PublishedGeneration() error = %v", err)
	}
	if generation != 0 {
		t.Fatalf("PublishedGeneration() = %d, want 0 with nothing published", generation)
	}
}

// A record on its way out of the system is withheld by the index for the same
// reason the record list withholds it: the index entry outlives the reservation.
func TestPostgresIntegrationRecordSearchStoreWithholdsReservedRecords(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordSearchReadFixture(t, ctx, "record-search-reserved")
	reserved := fixture.byName["disk"]
	if _, err := fixture.fixture.db.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint,
			actor_scope_digest, preview_binding_digest,
			preview_current_revision_id, preview_lock_version,
			preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest,
			adapter_preview_digest, preview_witness_sequence,
			preview_witness_entry_hash, state, expires_at, completed_at
		)
		select
			'drs_recordsearchreserved', 'default', 'record', document.record_id,
			decode(repeat('41', 32), 'hex'), decode(repeat('42', 32), 'hex'),
			decode(repeat('43', 32), 'hex'), decode(repeat('44', 32), 'hex'),
			document.current_revision_id, document.record_lock_version,
			document.authorization_epoch, 0,
			decode(repeat('45', 32), 'hex'), decode(repeat('46', 32), 'hex'),
			decode(repeat('47', 32), 'hex'), decode(repeat('48', 32), 'hex'),
			decode(repeat('49', 32), 'hex'), 1,
			decode(repeat('4a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		from public.record_search_documents as document
		where document.record_id = $1 and document.generation = 1`,
		reserved,
	); err != nil {
		t.Fatalf("seed deletion reservation: %v", err)
	}

	candidates, err := fixture.store.ListSearchCandidates(ctx, recordsearch.CandidatePage{
		Query:      mustRecordSearchQuery(t, recordsearch.QueryValues{}),
		Generation: 1,
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("ListSearchCandidates() error = %v", err)
	}
	assertRecordSearchNames(t, fixture.names(t, candidates), "archived", "migration", "network")
}

// The runtime role has select on the index but no access to the schema holding
// the trigram operator class. If an index scan needed that access, the text
// filter would fail for the only role that ever runs it.
func TestPostgresIntegrationRecordSearchStoreTextFilterUsesTrigramIndex(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordSearchReadFixture(t, ctx, "record-search-trigram")
	if _, err := fixture.fixture.db.Exec(ctx, `set enable_seqscan = off`); err != nil {
		t.Fatalf("disable sequential scans: %v", err)
	}

	candidates, err := fixture.store.ListSearchCandidates(ctx, recordsearch.CandidatePage{
		Query:      mustRecordSearchQuery(t, recordsearch.QueryValues{Text: "故障排查"}),
		Generation: 1,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListSearchCandidates() error = %v", err)
	}
	assertRecordSearchNames(t, fixture.names(t, candidates), "disk")

	var plan string
	if err := fixture.fixture.db.QueryRow(ctx, `
		explain (format text)
		select document.record_id
		from public.record_search_documents as document
		where document.generation = 1
		  and document.search_text like ('%' || lower($1) || '%') escape '\'`,
		"故障排查",
	).Scan(&plan); err != nil {
		t.Fatalf("explain trigram text filter: %v", err)
	}
	if plan == "" {
		t.Fatal("explain returned no plan for the trigram text filter")
	}
}

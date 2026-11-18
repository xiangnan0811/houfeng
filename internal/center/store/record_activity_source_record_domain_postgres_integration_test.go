package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// Commit both revisions and the action through admitted runtime writers. Their
// resulting domain rows are the source coordinates the projector must recheck.
func newRecordDomainActivitySourceFixture(
	t *testing.T,
	ctx context.Context,
) (*RecordDomainActivitySource, activityRecordFixture, []records.RevisionCommitResult, []activity.CandidateEvent) {
	t.Helper()
	fixture := newActivityRecordFixture(t, ctx)
	if _, err := fixture.owner.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'Additional VPS', 'active', 'in_use')`, testEvidenceVPSSourceID); err != nil {
		t.Fatalf("seed second activity source: %v", err)
	}
	results := fixture.commitRevisions(t, ctx, "domain-source",
		activityRecordInput(t, "First", ""), activityRecordInput(t, "Second", testEvidenceVPSSourceID))
	fixture.createAction(t, ctx, results[1], "domain-source-action")
	source, err := NewRecordDomainActivitySource(fixture.runtime, activityTestNamespace())
	if err != nil {
		t.Fatalf("new record domain activity source: %v", err)
	}
	candidates := fixture.domainCandidates(t, ctx)
	if len(candidates) != 3 {
		t.Fatalf("committed domain fixture has %d activities, want 3", len(candidates))
	}
	return source, fixture, results, candidates
}

func TestPostgresIntegrationRecordDomainSourceReadsSubjectsFromTheRightRevision(t *testing.T) {
	ctx := context.Background()
	source, _, results, committed := newRecordDomainActivitySourceFixture(t, ctx)
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: committed[len(committed)-1].RecordedAt}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("scanned %d candidates, want 3", len(candidates))
	}

	byKind := make(map[activity.EventKind]activity.CandidateEvent, len(candidates))
	for _, candidate := range candidates {
		byKind[candidate.EventKind] = candidate
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
	created := byKind[activity.EventKindRecordCreated]
	if created.RecordID != results[0].RecordID || created.SourceRecordID != results[0].RecordID || created.RevisionID != results[0].RevisionID {
		t.Fatalf("record_created route refs = record %q revision %q, want %q/%q",
			created.RecordID, created.RevisionID, results[0].RecordID, results[0].RevisionID)
	}
	if created.EvidenceID != "" {
		t.Fatalf("record_created unexpectedly carries evidence ref %q", created.EvidenceID)
	}
	if len(created.Subjects) != 1 {
		t.Fatalf("record_created has %d subjects, want the 1 its revision had", len(created.Subjects))
	}
	if created.Subjects[0].SourceID != testStoreRecordVPSID || !created.Subjects[0].Primary {
		t.Fatalf("record_created subject = %+v", created.Subjects[0])
	}
	if got := created.Subjects[0].Identity["display_name"]; got != "PostgreSQL VPS" {
		t.Fatalf("identity snapshot display name = %q, want the captured one", got)
	}

	// Revision 2 added a second subject, in ordinal order.
	revised := byKind[activity.EventKindRecordRevised]
	if len(revised.Subjects) != 2 {
		t.Fatalf("record_revised has %d subjects, want 2", len(revised.Subjects))
	}
	if !revised.Subjects[0].Primary || revised.Subjects[1].Primary {
		t.Fatalf("subjects arrived out of ordinal order: %+v", revised.Subjects)
	}

	// An action event names no revision of its own. Resolving the revision current
	// at its event time is what keeps it reachable instead of subject-less.
	action := byKind[activity.EventKind("action_created")]
	if len(action.Subjects) != 2 {
		t.Fatalf("action_created has %d subjects, want the 2 from the revision current at its event time", len(action.Subjects))
	}
	if action.RevisionID != results[1].RevisionID {
		t.Fatalf("action revision = %q, want %q", action.RevisionID, results[1].RevisionID)
	}
}

func TestPostgresIntegrationRecordDomainSourceHonoursBothWindowBounds(t *testing.T) {
	ctx := context.Background()
	source, _, _, committed := newRecordDomainActivitySourceFixture(t, ctx)

	// Bound the scan by times actually assigned by the committed writers.
	beforeFirst := committed[0].RecordedAt.Add(-time.Microsecond)
	before, err := source.ScanAfter(ctx, activity.ScanWindow{Through: beforeFirst}, 50)
	if err != nil || len(before) != 0 {
		t.Fatalf("upper bound before first activity = %+v, error %v", before, err)
	}
	upper := committed[1].RecordedAt
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: upper}, 50)
	if err != nil {
		t.Fatalf("scan upper bound: %v", err)
	}
	want := 0
	for _, candidate := range committed {
		if !candidate.RecordedAt.After(upper) {
			want++
		}
	}
	if len(candidates) != want {
		t.Fatalf("upper bound returned %d rows, want %d: %+v", len(candidates), want, candidates)
	}
	for index, candidate := range candidates {
		if candidate.Source.EventID != committed[index].Source.EventID {
			t.Fatalf("upper-bound row %d = %s, want %s", index, candidate.Source.EventID, committed[index].Source.EventID)
		}
	}

	// The time and exclusive event cursor together exclude every earlier row,
	// including any that share the boundary timestamp.
	candidates, err = source.ScanAfter(ctx, activity.ScanWindow{
		From: committed[1].RecordedAt, AfterEventID: committed[1].Source.EventID,
		Through: committed[2].RecordedAt,
	}, 50)
	if err != nil {
		t.Fatalf("scan lower bound: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != committed[2].Source.EventID {
		t.Fatalf("lower bound returned %d rows: %+v", len(candidates), candidates)
	}
}

func TestPostgresIntegrationRecordDomainSourceOrdersAndLimitsByRecordedTime(t *testing.T) {
	ctx := context.Background()
	source, _, _, committed := newRecordDomainActivitySourceFixture(t, ctx)
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: committed[len(committed)-1].RecordedAt}, 2)
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
	if candidates[0].Source.EventID != committed[0].Source.EventID || candidates[1].Source.EventID != committed[1].Source.EventID {
		t.Fatalf("page returned the wrong rows: %q, %q", candidates[0].Source.EventID, candidates[1].Source.EventID)
	}
}

// An export must not be told the source is settled on evidence that only proves
// where the incremental scan reached.
func TestPostgresIntegrationRecordDomainSourceReadinessNeedsASettledHead(t *testing.T) {
	ctx := context.Background()
	source, _, _, _ := newRecordDomainActivitySourceFixture(t, ctx)

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
	lease := mustActivitySourceLease(t, ctx, repository, 1, activity.SourceKindRecordDomain)
	if err := repository.SaveCheckpoint(ctx, lease, activity.SourceCheckpoint{
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

// The projector consumes the real adapter scan and committed source routes.
func TestPostgresIntegrationRecordDomainSourceProjectsThroughTheProjector(t *testing.T) {
	ctx := context.Background()
	source, fixture, _, committed := newRecordDomainActivitySourceFixture(t, ctx)
	pool := fixture.runtime
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{activityCommittedRecordSource{RecordDomainActivitySource: source, through: committed[len(committed)-1].RecordedAt}},
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
	source, fixture, results, committed := newRecordDomainActivitySourceFixture(t, ctx)
	pool := fixture.runtime
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{activityCommittedRecordSource{RecordDomainActivitySource: source, through: committed[len(committed)-1].RecordedAt}},
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

	intervals := loadRevisionIntervals(t, ctx, pool, 1, results[0].RecordID)
	if len(intervals) != 2 {
		t.Fatalf("intervals = %+v, want one per commit and none for the action", intervals)
	}
	if intervals[0].revisionID != results[0].RevisionID || intervals[1].revisionID != results[1].RevisionID {
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
	if again := loadRevisionIntervals(t, ctx, pool, 1, results[0].RecordID); len(again) != 2 ||
		again[1].validTo != nil {
		t.Fatalf("intervals after a repeat pass = %+v, want them unchanged", again)
	}
}

// Restricted revision visibility must survive projection as a distinct auth
// digest. A project viewer allowlist matches only project digests, so restricted
// rows stay hidden even though they share the same subject timeline.
func TestPostgresIntegrationRecordDomainRestrictedVisibilityHidesFromViewer(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	projectResult := fixture.commitRevisions(t, ctx, "activity-project-visibility", activityRecordInput(t, "Public note", ""))[0]
	const groupID = "rag_opsrestricted01"
	const authorID = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := fixture.owner.Exec(ctx, `insert into public.record_access_groups (group_id, display_name) values ($1, 'Activity restricted')`, groupID); err != nil {
		t.Fatalf("seed restricted group: %v", err)
	}
	if _, err := fixture.owner.Exec(ctx, `insert into public.record_access_group_members (group_id, user_id) values ($1, $2)`, groupID, authorID); err != nil {
		t.Fatalf("seed restricted membership: %v", err)
	}
	actor := collaborationActor(t, authorID, []string{groupID})
	input := accessManagementRestrictedRevisionInput(t, "Secret note", groupID, authorID)
	command := revisionBirthBatchCommandForActor(t, ctx, ImportAttachmentGCFixture{Runtime: pool, Gate: fixture.gate}, input, "activity-restricted-visibility", actor)
	restrictedResult, err := fixture.repository.CommitRevision(ctx, command)
	if err != nil || !restrictedResult.Created {
		t.Fatalf("commit restricted activity record: result=%+v error=%v", restrictedResult, err)
	}
	source, err := NewRecordDomainActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	committed := fixture.domainCandidates(t, ctx)
	if len(committed) != 2 {
		t.Fatalf("scanned %d committed activity candidates, want 2", len(committed))
	}
	byRecord := make(map[string]activity.CandidateEvent, len(committed))
	for _, candidate := range committed {
		byRecord[candidate.RecordID] = candidate
	}
	projectCandidate, ok := byRecord[projectResult.RecordID]
	if !ok || projectCandidate.RevisionID != projectResult.RevisionID {
		t.Fatalf("missing project candidate in %#v", byRecord)
	}
	restrictedCandidate, ok := byRecord[restrictedResult.RecordID]
	if !ok || restrictedCandidate.RevisionID != restrictedResult.RevisionID {
		t.Fatalf("missing restricted candidate in %#v", byRecord)
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
		Adapters:    []activity.SourceAdapter{activityCommittedRecordSource{RecordDomainActivitySource: source, through: committed[len(committed)-1].RecordedAt}},
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
			Kind: records.SubjectKindVPS, SourceID: testStoreRecordVPSID,
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
	if viewerPage.Events[0].RecordID != projectCandidate.RecordID ||
		viewerPage.Events[0].RevisionID != projectCandidate.RevisionID {
		t.Fatalf("viewer route refs = record %q revision %q, want project refs %q/%q",
			viewerPage.Events[0].RecordID, viewerPage.Events[0].RevisionID,
			projectCandidate.RecordID, projectCandidate.RevisionID)
	}
	if viewerPage.Events[0].RecordID == restrictedCandidate.RecordID ||
		viewerPage.Events[0].RevisionID == restrictedCandidate.RevisionID {
		t.Fatalf("viewer page leaked restricted route refs: %+v", viewerPage.Events[0])
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
	byActivityID := make(map[string]activity.Event, len(adminPage.Events))
	for _, event := range adminPage.Events {
		byActivityID[event.ActivityID] = event
	}
	restrictedEvent, ok := byActivityID[restrictedCandidate.ActivityID]
	if !ok {
		t.Fatalf("admin page omitted restricted event: %+v", adminPage.Events)
	}
	if restrictedEvent.RecordID != restrictedCandidate.RecordID ||
		restrictedEvent.RevisionID != restrictedCandidate.RevisionID {
		t.Fatalf("admin restricted route refs = record %q revision %q, want %q/%q",
			restrictedEvent.RecordID, restrictedEvent.RevisionID,
			restrictedCandidate.RecordID, restrictedCandidate.RevisionID)
	}
}

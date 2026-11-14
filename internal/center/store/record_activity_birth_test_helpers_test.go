package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// activityRecordFixture creates records through the same signed admission and
// birth reservation/commit path as a runtime writer.
type activityRecordFixture struct {
	owner      *pgxpool.Pool
	runtime    *pgxpool.Pool
	gate       AdmissionGate
	repository *PostgresRecordRepository
}

func newActivityRecordFixture(t *testing.T, ctx context.Context) activityRecordFixture {
	t.Helper()
	platform, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	return makeActivityRecordFixture(t, ctx, platform.db, runtime, gate)
}

func makeActivityRecordFixture(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, gate AdmissionGate) activityRecordFixture {
	t.Helper()
	if _, err := owner.Exec(ctx, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 1, 0, 0)`); err != nil {
		t.Fatalf("seed active activity generation: %v", err)
	}
	return activityRecordFixture{
		owner: owner, runtime: runtime, gate: gate,
		repository: newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtime, Gate: gate}),
	}
}

func activityRecordInput(t *testing.T, title string, extraVPS string) records.CompleteRevisionInput {
	t.Helper()
	base := recordsPostgresCompleteRevisionInput(t, title)
	if extraVPS == "" {
		return base
	}
	visibility := base.VisibilityScope()
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS,
		SourceID: extraVPS, State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatal(err)
	}
	subjects := append(base.Subjects(), records.RevisionSubject{
		RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleContext, SourceID: extraVPS,
		IdentitySnapshot: map[string]string{"display_name": "Additional VPS"}, CaptureAuthorization: authorization,
	})
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title: base.Title(), BodyMarkdown: base.BodyMarkdown(), MarkdownDialectVersion: base.MarkdownDialectVersion(),
		RecordType: base.RecordType(), BusinessStatus: base.BusinessStatus(), ImpactLevel: base.ImpactLevel(),
		VisibilityScope: visibility, Subjects: subjects, Tags: base.Tags(), OwnerID: base.OwnerID(),
		Participants: base.Participants(), AuthorID: base.AuthorID(),
	})
	if err != nil {
		t.Fatalf("normalize activity revision: %v", err)
	}
	return input
}

func (fixture activityRecordFixture) commitRevisions(t *testing.T, ctx context.Context, key string, inputs ...records.CompleteRevisionInput) []records.RevisionCommitResult {
	t.Helper()
	if len(inputs) == 0 {
		t.Fatal("activity fixture requires a first revision")
	}
	birth := ImportAttachmentGCFixture{Runtime: fixture.runtime, Gate: fixture.gate}
	command := revisionBirthBatchCommand(t, ctx, birth, inputs[0], key+"-create")
	results := make([]records.RevisionCommitResult, 0, len(inputs))
	for index, input := range inputs {
		if index > 0 {
			previous := results[index-1]
			command = recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordUpdate,
				previous.RecordID, previous.RevisionID, previous.LockVersion, previous.AuthorizationEpoch,
				input, fmt.Sprintf("%s-update-%d", key, index))
		}
		result, err := fixture.repository.CommitRevision(ctx, command)
		if err != nil || !result.Created || result.RevisionNo != uint64(index+1) {
			t.Fatalf("commit activity revision %d: result=%+v error=%v", index+1, result, err)
		}
		results = append(results, result)
	}
	return results
}

func (fixture activityRecordFixture) domainCandidates(t *testing.T, ctx context.Context) []activity.CandidateEvent {
	t.Helper()
	source, err := NewRecordDomainActivitySource(fixture.owner, activityTestNamespace())
	if err != nil {
		t.Fatal(err)
	}
	var through time.Time
	if err := fixture.owner.QueryRow(ctx, `select clock_timestamp()`).Scan(&through); err != nil {
		t.Fatal(err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: through}, 100)
	if err != nil {
		t.Fatalf("scan committed record-domain activity: %v", err)
	}
	return candidates
}

func (fixture activityRecordFixture) revisionCandidates(t *testing.T, ctx context.Context, results []records.RevisionCommitResult) []activity.CandidateEvent {
	t.Helper()
	byRevision := make(map[string]activity.CandidateEvent, len(results))
	for _, candidate := range fixture.domainCandidates(t, ctx) {
		if candidate.OpensRevision {
			byRevision[candidate.RevisionID] = candidate
		}
	}
	candidates := make([]activity.CandidateEvent, 0, len(results))
	for _, result := range results {
		candidate, ok := byRevision[result.RevisionID]
		if !ok || candidate.RecordID != result.RecordID || candidate.SourceRecordID != result.RecordID || candidate.RevisionNo != result.RevisionNo {
			t.Fatalf("committed revision %s has no matching source route: %+v", result.RevisionID, candidate)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func (fixture activityRecordFixture) createAction(t *testing.T, ctx context.Context, parent records.RevisionCommitResult, key string) activity.CandidateEvent {
	t.Helper()
	fields := mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Activity fixture action"})
	command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_activityfixture", 0, fields, key)
	current, err := newRecordsPostgresAuthorization(t, fixture.runtime, fixture.gate).
		ResolveCurrentRecordAuthorization(ctx, command.Actor, parent.RecordID)
	if err != nil {
		t.Fatalf("resolve action authorization: %v", err)
	}
	command.AuthorizationEvidence = current.Evidence
	if _, err := newPostgresActionRepositoryForTest(t, fixture.runtime, fixture.gate).CommitAction(ctx, command); err != nil {
		t.Fatalf("create real activity action: %v", err)
	}
	for _, candidate := range fixture.domainCandidates(t, ctx) {
		if candidate.RecordID == parent.RecordID && candidate.EventKind == activity.EventKind("action_created") {
			return candidate
		}
	}
	t.Fatal("real action has no record-domain activity")
	return activity.CandidateEvent{}
}

// The source still performs its real SQL scan. Since this fixture has already
// committed every source row, bound the projector at the observed last row
// instead of waiting for the production 30-second incremental safety lag.
type activityCommittedRecordSource struct {
	*RecordDomainActivitySource
	through time.Time
}

func (source activityCommittedRecordSource) IncrementalHead(context.Context) (activity.SourceHead, error) {
	return activity.NewIncrementalSourceHead(activity.SourceKindRecordDomain, source.through, 0), nil
}

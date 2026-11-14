package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresIntegrationRevisionMutationDiscoveryDraftAndStaleSets(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	pool := fixture.Runtime
	if _, err := pool.Exec(ctx, `
		insert into public.record_access_groups(group_id,display_name) values ('rag_discovery','Discovery group');
		insert into public.record_access_group_members(group_id,user_id) values ('rag_discovery','usr_aaaaaaaaaaaaaaaaaaaaaaaa');
		insert into public.record_search_generations(generation,generation_state,published_at)
		values (9101,'published',transaction_timestamp()),(9102,'building',null)`); err != nil {
		t.Fatal(err)
	}
	participants := []records.RevisionParticipant{
		NewRecordAttachmentRevisionParticipant(), NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
		NewComparisonRevisionParticipant(nil), NewRecordEvidenceRevisionParticipant(), NewRecordSearchRevisionParticipant(),
	}
	repository := newRevisionMutationAdmittedRepository(t, fixture, participants...)
	actor := recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault, GroupIDs: []string{"rag_discovery"}}
	firstCommand := revisionBirthBatchCommandForActor(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Before discovery"), "discovery-create", actor)
	first, err := repository.CommitRevision(ctx, firstCommand)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.record_followers(project_id,record_id,user_id,follower_version,follows_author,follows_comment,record_fence_epoch)
		values ('default',$1,'usr_dddddddddddddddddddddddd',1,true,true,0)`, first.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.record_search_subjects(generation,record_id,subject_kind,relation_role,source_id,is_primary)
		select generation,record_id,subject_kind,'context',source_id,false from public.record_search_subjects where record_id=$1`, first.RecordID); err != nil {
		t.Fatal(err)
	}
	draftRepository := NewPostgresRecordDraftRepository(pool, fixture.Gate)
	if err := draftRepository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, pool, fixture.Gate)); err != nil {
		t.Fatal(err)
	}
	draft, err := draftRepository.CreateDraft(ctx, records.DraftCreateCommand{DraftID: "rdf_discovery", ProjectID: "default", RecordID: first.RecordID, BaseRevisionID: first.RevisionID, AuthorID: recordsPostgresDraftAuthorID, Payload: recordsPostgresDraftPayload(t, "Publish discovery"), Policy: records.DefaultDraftRetentionPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	attachmentRepository := NewPostgresAttachmentRepository(pool)
	if err := attachmentRepository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, pool, fixture.Gate)); err != nil {
		t.Fatal(err)
	}
	writer := fixture.Writer
	keep := seedDraftReleaseUpload(t, ctx, attachmentRepository, writer, draft.DraftID, 0, draftReleaseAvailable)
	unused := seedDraftReleaseUpload(t, ctx, attachmentRepository, writer, draft.DraftID, 1, draftReleaseQuarantined)
	pending := seedDraftReleaseUpload(t, ctx, attachmentRepository, writer, draft.DraftID, 2, draftReleaseUploading)
	object := draftReleaseTarget(draft.DraftID, 2)
	seedDraftReleasePart(t, ctx, attachmentRepository, writer, draft.DraftID, 2)
	jobID := "apj_discovery1"
	if _, err := pool.Exec(ctx, `
		insert into public.content_processor_workspaces(workspace_id,processor_job_id,attempt,workspace_state,workspace_path_digest,expires_at,purged_at)
		values ('cpw_discovery',$1,1,'purged',decode(repeat('cd',32),'hex'),transaction_timestamp()+interval '1 hour',transaction_timestamp())`, jobID); err != nil {
		t.Fatal(err)
	}
	// The stored part already consumed its publication; a later in-flight PUT
	// has a different target and remains independently discoverable.
	publication, err := attachmentRepository.PrepareBlobPublication(ctx, attachments.BlobPublicationPrepareRequest{
		ProjectID: "default", NamespaceID: writer.NamespaceID(), OwnerKind: attachments.BlobPublicationOwnerUpload,
		OwnerID: "aup_discovery2", OwnerGeneration: 1, Target: draftReleaseTarget(draft.DraftID, 3), PublishExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	template := recordsPostgresCompleteRevisionInput(t, "After discovery", keep)
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title: template.Title(), BodyMarkdown: template.BodyMarkdown(), MarkdownDialectVersion: template.MarkdownDialectVersion(),
		RecordType: template.RecordType(), ImpactLevel: template.ImpactLevel(), VisibilityScope: template.VisibilityScope(),
		Subjects: template.Subjects(), Tags: template.Tags(), OwnerID: "usr_cccccccccccccccccccccccc",
		Participants: template.Participants(), AttachmentIDs: template.AttachmentIDs(), AuthorID: template.AuthorID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The birth used the current scope; this read-only discovery deliberately
	// omits the group so the persisted membership must supply the lock route.
	discoveryActor := actor
	discoveryActor.GroupIDs = nil
	command := recordsPostgresRevisionCommandForActor(t, recordplatform.OperationKindRecordUpdate, first.RecordID, first.RevisionID, first.LockVersion, first.AuthorizationEpoch, input, "discovery-update", discoveryActor)
	command.DraftID, command.DraftETag = draft.DraftID, draft.ETag
	registry, err := records.NewRevisionParticipantRegistry(participants)
	if err != nil {
		t.Fatal(err)
	}
	tx := beginRevisionMutationDiscoveryReadOnly(t, ctx, fixture)
	defer func() { _ = tx.Rollback(ctx) }()
	before := revisionMutationDiscoveryState(t, ctx, tx)
	resources, err := registry.MutationResources(ctx, tx, command)
	if err != nil {
		t.Fatal(err)
	}
	assertRevisionMutationEffectBindings(t, resources, command)
	expectedRevisionID, err := command.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	expectedOwnerActivity := collaborationRevisionActivityID(expectedRevisionID, "record_owner_changed")
	if !slices.ContainsFunc(resources.NewRevisionEffects, func(effect records.RevisionMutationNewRevisionEffect) bool {
		return effect.Kind == records.RevisionMutationNewCollaborationActivity && effect.EventKind == records.RevisionMutationEventOwnerChanged && effect.ActivityID == expectedOwnerActivity
	}) {
		t.Fatalf("missing exact changed-owner activity identity: %#v", resources.NewRevisionEffects)
	}
	if !slices.ContainsFunc(resources.NewRevisionEffects, func(effect records.RevisionMutationNewRevisionEffect) bool {
		return effect.Kind == records.RevisionMutationNewCollaborationOutbox && effect.EventKind == records.RevisionMutationEventOwnerChanged && effect.ActivityID == ""
	}) {
		t.Fatalf("missing exact changed-owner outbox semantic scope: %#v", resources.NewRevisionEffects)
	}
	again, err := registry.MutationResources(ctx, tx, command)
	if err != nil || !reflect.DeepEqual(resources, again) {
		t.Fatalf("unstable discovery: %v", err)
	}
	for _, id := range []string{keep, unused, pending} {
		if !slices.ContainsFunc(resources.Attachments, func(v records.RevisionMutationAttachment) bool {
			return v.AttachmentID == id && v.DraftID == draft.DraftID
		}) {
			t.Fatalf("missing draft attachment %s: %#v", id, resources.Attachments)
		}
	}
	if len(resources.Uploads) != 3 || len(resources.Jobs) != 2 || len(resources.Workspaces) != 1 || len(resources.Publications) != 1 || resources.Publications[0].PublicationID != publication.PublicationID {
		t.Fatalf("incomplete release footprint: %#v", resources)
	}
	var observedUpload, observedOwner string
	var observedGeneration int64
	var observedExpiry *time.Time
	if err := tx.QueryRow(ctx, `select upload_id,owner_id,owner_generation,lease_expires_at from public.attachment_processor_jobs where processor_job_id=$1`, jobID).Scan(&observedUpload, &observedOwner, &observedGeneration, &observedExpiry); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(resources.Jobs, func(job records.RevisionMutationJob) bool {
		return job.ProcessorJobID == jobID && job.UploadID == observedUpload && job.OwnerID == observedOwner && job.OwnerGeneration == observedGeneration && ((observedExpiry == nil && job.LeaseExpiresAt.IsZero()) || (observedExpiry != nil && job.LeaseExpiresAt.Equal(*observedExpiry)))
	}) {
		t.Fatalf("processor route/owner observation missing: %#v", resources.Jobs)
	}
	if got := resources.Publications[0]; got.ProjectID != "default" || got.OwnerGeneration != 1 || got.BackendKind != attachments.BackendKindLocal || got.NamespaceID != writer.NamespaceID() || got.SizeBytes != publication.Target.SizeBytes {
		t.Fatalf("publication route/generation observation = %#v", got)
	}
	if !slices.ContainsFunc(resources.Parts, func(v records.RevisionMutationPart) bool {
		return v.UploadID == "aup_discovery2" && v.PartNumber == 1 && v.Object.Key == object.Key
	}) {
		t.Fatalf("missing unreferenced upload part: %#v", resources.Parts)
	}
	if !slices.ContainsFunc(resources.Followers, func(v records.RevisionMutationFollower) bool {
		return v.UserID == "usr_dddddddddddddddddddddddd" && v.FollowsComment
	}) {
		t.Fatalf("missing stale follower: %#v", resources.Followers)
	}
	if len(resources.Generations) != 2 || len(resources.Documents) != 2 || !slices.ContainsFunc(resources.SearchSubjects, func(v records.RevisionMutationSearchSubject) bool {
		return v.RelationRole == records.RelationRoleContext
	}) {
		t.Fatalf("missing live generation/old subject: %#v", resources)
	}
	if !slices.ContainsFunc(resources.Memberships, func(v records.RevisionMutationMembership) bool {
		return v.GroupID == "rag_discovery" && v.UserID == command.Actor.UserID
	}) {
		t.Fatalf("missing persisted membership: %#v", resources.Memberships)
	}
	if len(command.Actor.GroupIDs) != 0 {
		t.Fatal("fixture accidentally supplied persisted group in actor")
	}
	for _, subject := range command.Input.Subjects() {
		if !slices.ContainsFunc(resources.Sources, func(v records.RevisionMutationSource) bool {
			return v.ProjectID == "default" && v.SourceKind == subject.Kind && v.SourceID == subject.SourceID
		}) {
			t.Fatalf("missing concrete source route: %#v", resources.Sources)
		}
	}
	declarations, err := discoverRevisionMutationResources(ctx, tx, registry, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations.order) == 0 || len(declarations.resources.Records) != 1 {
		t.Fatalf("missing private base declaration: %#v", declarations)
	}
	assertRevisionMutationBaseScopes(t, declarations.resources, command)
	if after := revisionMutationDiscoveryState(t, ctx, tx); after != before {
		t.Fatalf("discovery changed persistent state\nbefore=%s\nafter=%s", before, after)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func revisionMutationDiscoveryState(t *testing.T, ctx context.Context, tx pgx.Tx) string {
	t.Helper()
	var state string
	if err := tx.QueryRow(ctx, `select jsonb_build_object(
		'records',(select jsonb_agg(to_jsonb(r) order by record_id) from public.records r),
		'drafts',(select jsonb_agg(to_jsonb(r) order by draft_id) from public.record_drafts r),
		'attachments',(select jsonb_agg(to_jsonb(r) order by attachment_id) from public.record_attachments r),
		'uploads',(select jsonb_agg(to_jsonb(r) order by upload_id) from public.attachment_uploads r),
		'parts',(select jsonb_agg(to_jsonb(r) order by upload_id,part_number) from public.attachment_upload_parts r),
		'jobs',(select jsonb_agg(to_jsonb(r) order by processor_job_id) from public.attachment_processor_jobs r),
		'publications',(select jsonb_agg(to_jsonb(r) order by publication_id) from public.blob_publication_intents r),
		'followers',(select jsonb_agg(to_jsonb(r) order by record_id,user_id) from public.record_followers r),
		'search_subjects',(select jsonb_agg(to_jsonb(r) order by generation,record_id,subject_kind,source_id,relation_role) from public.record_search_subjects r),
		'snapshots',(select jsonb_agg(to_jsonb(r) order by snapshot_id) from public.evidence_snapshots r),
		'intents',(select jsonb_agg(to_jsonb(r) order by intent_id) from public.evidence_capture_intents r),
		'dependencies',(select jsonb_agg(to_jsonb(r) order by result_snapshot_id,source_record_id,source_revision_id,source_snapshot_id) from public.comparison_result_dependencies r),
		'completions',(select jsonb_agg(to_jsonb(r) order by result_snapshot_id) from public.comparison_result_dependency_index_completions r)
	)::text`).Scan(&state); err != nil {
		t.Fatal(fmt.Errorf("read discovery state: %w", err))
	}
	return state
}

func TestPostgresIntegrationRevisionMutationDiscoveryCertifiedCopies(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	pool := fixture.Runtime
	vpsRepository := NewPostgresVPSAssetRepository(pool)
	vps, err := vpsRepository.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Discovery source", LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse})
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{NewVPSRecordSubjectAdapter(vpsRepository)})
	if err != nil {
		t.Fatal(err)
	}
	tombstones, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, pool)
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewRecordSubjectReadResolver(subjects, tombstones)
	current := NewPostgresCurrentRecordAuthorizationSource(pool, resolver, fixture.Gate)
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := evidence.NewRegistry([]evidence.Kind{kind})
	if err != nil {
		t.Fatal(err)
	}
	evidenceRepository, err := NewPostgresEvidenceRepositoryWithReadSources(pool, fixture.Gate, kinds, current, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := evidence.NewCapacityEnforcer(evidence.DefaultCapacityPolicy(), evidenceRepository)
	if err != nil {
		t.Fatal(err)
	}
	service, err := evidence.NewService(kinds, evidenceRepository, evidenceRepository, capacity)
	if err != nil {
		t.Fatal(err)
	}
	keyDirectory := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDirectory, "discovery"), key, 0o400); err != nil {
		t.Fatal(err)
	}
	signer, err := evidence.OpenComparisonIntentKeyring(keyDirectory, "discovery", nil)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := evidence.NewComparisonAdmission(64 << 20)
	if err != nil {
		t.Fatal(err)
	}
	service = service.WithFixedComparison(evidenceRepository, admission, signer)
	participants := []records.RevisionParticipant{NewRecordAttachmentRevisionParticipant(), NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()), NewComparisonRevisionParticipant(signer), NewRecordEvidenceRevisionParticipant(), NewRecordSearchRevisionParticipant()}
	registry, err := records.NewRevisionParticipantRegistry(participants)
	if err != nil {
		t.Fatal(err)
	}
	repository := newRevisionMutationAdmittedRepository(t, fixture, participants...)
	actor, err := recordauth.NormalizeActorScope(storeEvidenceParticipantActor(t))
	if err != nil {
		t.Fatal(err)
	}
	reference := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: vps.VPSID, Primary: true}
	resolved, err := subjects.Resolve(ctx, actor, reference)
	if err != nil {
		t.Fatal(err)
	}
	allocations := make(map[string]recordbirth.Allocation)
	targetID := func(label string) string {
		if allocation, ok := allocations[label]; ok {
			return allocation.RecordID
		}
		allocation := reserveRevisionBirth(t, ctx, fixture, actor, label, recordbirth.PurposeComparison)
		allocations[label] = allocation
		allocations[allocation.RecordID] = allocation
		return allocation.RecordID
	}
	makeCommand := func(recordID string, preparation evidence.RevisionPreparation) records.RevisionCommitCommand {
		t.Helper()
		recordID = targetID(recordID)
		input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
			Title: recordID, BodyMarkdown: "# " + recordID + "\n", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
			RecordType: records.RecordTypeNote, ImpactLevel: "informational", VisibilityScope: resolved.CaptureAuthorization.CaptureScope,
			Subjects:            []records.RevisionSubject{{RegistryVersion: reference.RegistryVersion, Kind: reference.Kind, Role: reference.Role, SourceID: reference.SourceID, Primary: true, IdentitySnapshot: resolved.IdentitySnapshot.Fields(), CaptureAuthorization: resolved.CaptureAuthorization}},
			EvidenceSnapshotIDs: preparation.SnapshotIDs(), AuthorID: actor.UserID,
		})
		if err != nil {
			t.Fatal(err)
		}
		command := recordsPostgresRevisionCommandForActor(t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0, input, recordID, actor)
		command.EvidencePreparation = preparation
		return prepareRevisionBirthCommand(t, ctx, fixture, allocations[recordID], command)
	}
	create := func(recordID string) records.RevisionCommitResult {
		t.Helper()
		recordID = targetID(recordID)
		preparation, err := evidence.NewRevisionPreparation(recordID, evidence.RevisionPreparationValues{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := repository.CommitRevision(ctx, makeCommand(recordID, preparation))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	a, other := create("rec_discoverysourcea"), create("rec_discoverysourceb")
	prepare := func(recordID string, items []evidence.ComparisonFixedItem) evidence.ComparisonSavePreparation {
		t.Helper()
		recordID = targetID(recordID)
		now := time.Now().UTC()
		result, err := service.EvaluateFixedComparison(ctx, evidence.ComparisonEvaluateRequest{Actor: actor, Items: items, BaselineIndex: 0, Alignment: evidence.CoverageActual, RequestedWindow: evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}, Now: now})
		if err != nil || result.Intent == nil || !result.SaveEligibility.Eligible {
			t.Fatalf("prepare real comparison: eligible=%v intent=%v error=%v", result.SaveEligibility.Eligible, result.Intent != nil, err)
		}
		save, err := service.PrepareComparisonSave(ctx, evidence.ComparisonSaveRequest{Actor: actor, AllocationID: allocations[recordID].AllocationID, RecordID: recordID, Token: result.Intent.Token, Now: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		return save
	}
	parent := prepare("rec_discoveryparent", []evidence.ComparisonFixedItem{{Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}}, {Revision: &evidence.ComparisonFixedRevision{RecordID: other.RecordID, RevisionID: other.RevisionID}}})
	parentRecordID := targetID("rec_discoveryparent")
	parentPreparation, err := evidence.NewRevisionPreparationFromComparisonSave(parentRecordID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CommitRevision(ctx, makeCommand("rec_discoveryparent", parentPreparation)); err != nil {
		t.Fatal(err)
	}
	parentID := parent.Result.SnapshotID()
	copied := prepare("rec_discoverycopy", []evidence.ComparisonFixedItem{{SnapshotID: &parentID}, {Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}}})
	if len(copied.Copies) != 1 {
		t.Fatalf("copy count=%d", len(copied.Copies))
	}
	copyRecordID := targetID("rec_discoverycopy")
	copyPreparation, err := evidence.NewRevisionPreparationFromComparisonSave(copyRecordID, copied)
	if err != nil {
		t.Fatal(err)
	}
	command := makeCommand("rec_discoverycopy", copyPreparation)
	tx := beginRevisionMutationDiscoveryReadOnly(t, ctx, fixture)
	defer func() { _ = tx.Rollback(ctx) }()
	before := revisionMutationDiscoveryState(t, ctx, tx)
	resources, err := registry.MutationResources(ctx, tx, command)
	if err != nil {
		t.Fatal(err)
	}
	assertRevisionMutationEffectBindings(t, resources, command)
	if !slices.ContainsFunc(resources.Snapshots, func(v records.RevisionMutationSnapshot) bool {
		return v.SnapshotID == parentID && v.RecordID == parentRecordID && v.PayloadDigest == parent.Result.Snapshot().Hash()
	}) {
		t.Fatalf("copy parent owner was guessed or omitted: %#v", resources.Snapshots)
	}
	for _, source := range []records.RevisionCommitResult{a, other} {
		if !slices.ContainsFunc(resources.Dependencies, func(v records.RevisionMutationDependency) bool {
			return v.ResultSnapshotID == copied.Copies[0].SnapshotID() && v.SourceRecordID == source.RecordID && v.SourceRevisionID == source.RevisionID && v.SourceSnapshotID == ""
		}) {
			t.Fatalf("missing certified inherited source %s: %#v", source.RecordID, resources.Dependencies)
		}
		if !slices.ContainsFunc(resources.Records, func(v records.RevisionMutationRecord) bool { return v.RecordID == source.RecordID }) {
			t.Fatalf("missing inherited record route %s", source.RecordID)
		}
	}
	declarations, err := discoverRevisionMutationResources(ctx, tx, registry, command)
	if err != nil {
		t.Fatal(err)
	}
	assertRevisionMutationBaseScopes(t, declarations.resources, command)
	if after := revisionMutationDiscoveryState(t, ctx, tx); after != before {
		t.Fatal("certified-copy discovery changed persistent state")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIntegrationRevisionMutationDiscoveryCaptureIntentIsNotConsumed(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	evidenceBirthTestSource(t, ctx, fixture.platform.db)
	pool := fixture.Runtime
	evidenceRepository := NewPostgresEvidenceRepository(pool, fixture.Gate, nil)
	var now time.Time
	if err := pool.QueryRow(ctx, `select transaction_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	actor := storeEvidenceParticipantActor(t)
	allocation := reserveRevisionBirth(t, ctx, fixture, actor, "capture-discovery", recordbirth.PurposeEvidence)
	capture := storePreparedEvidenceCapture(t, allocation.RecordID, "evs_discoverycapture", "evi_111111111111111111111111", now.UTC())
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
	preparation := storeEvidenceRevisionPreparation(t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()})
	command := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0, "Capture discovery", "capture-discovery", preparation)
	command = prepareRevisionBirthCommand(t, ctx, fixture, allocation, command)
	registry, err := records.NewRevisionParticipantRegistry([]records.RevisionParticipant{NewRecordEvidenceRevisionParticipant()})
	if err != nil {
		t.Fatal(err)
	}
	tx := beginRevisionMutationDiscoveryReadOnly(t, ctx, fixture)
	defer func() { _ = tx.Rollback(ctx) }()
	before := revisionMutationDiscoveryState(t, ctx, tx)
	resources, err := registry.MutationResources(ctx, tx, command)
	if err != nil {
		t.Fatal(err)
	}
	assertRevisionMutationEffectBindings(t, resources, command)
	if len(resources.CaptureIntents) != 1 || resources.CaptureIntents[0].IntentID != capture.Intent().ID || resources.CaptureIntents[0].SnapshotID != capture.SnapshotID() || len(resources.Snapshots) != 1 || resources.Snapshots[0].RecordID != capture.RecordID() {
		t.Fatalf("capture footprint=%#v", resources)
	}
	if after := revisionMutationDiscoveryState(t, ctx, tx); after != before {
		t.Fatal("capture discovery consumed intent or wrote snapshot")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionMutationDeclarationsOrderClosedFamilies(t *testing.T) {
	declarations, err := normalizeRecordMutationDeclarations(records.RevisionMutationResources{
		Users:   []records.RevisionMutationUser{{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa"}},
		Records: []records.RevisionMutationRecord{{ProjectID: "default", RecordID: "rec_order"}},
		Sources: []records.RevisionMutationSource{
			{ProjectID: "default", SourceKind: records.SubjectKindTarget, SourceID: "tg_0123456789abcdef"},
			{ProjectID: "default", SourceKind: records.SubjectKindVPS, SourceID: "vps_0123456789abcdef"},
			{ProjectID: "default", SourceKind: records.SubjectKindMonitoringInstance, SourceID: "mi_0123456789abcdef"},
		},
		RecordGateScopes: []records.RevisionMutationRecordGateScope{{ProjectID: "default", RecordID: "rec_order"}},
		Idempotency:      []records.RevisionMutationIdempotency{{Key: recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "order-key"}}},
		Revisions:        []records.RevisionMutationRevision{{RecordID: "rec_order", RevisionID: "rrv_existing"}},
		Projects:         []records.RevisionMutationProject{{ProjectID: "default", Kind: records.RevisionMutationAttachmentQuota}, {ProjectID: "default", Kind: records.RevisionMutationEvidenceCapacity}},
		BlobTables:       []records.RevisionMutationBlobTable{records.RevisionMutationUploadPartsTable, records.RevisionMutationBlobMetadataTable},
		NewRevisionEffects: []records.RevisionMutationNewRevisionEffect{
			{ProjectID: "default", RecordID: "rec_order", RevisionID: "rrv_order", Kind: records.RevisionMutationNewCollaborationOutbox, EventKind: records.RevisionMutationEventOwnerChanged},
			{ProjectID: "default", RecordID: "rec_order", RevisionID: "rrv_order", Kind: records.RevisionMutationNewEvidenceReference, SnapshotID: "evs_order"},
			{ProjectID: "default", RecordID: "rec_order", RevisionID: "rrv_order", Kind: records.RevisionMutationNewRevisionRows, ActivityID: "rac_order", EventKind: records.RevisionMutationEventKind(records.DomainActivityRecordCreated)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		rank, subrank uint8
		family        recordMutationFamily
	}{
		{10, 0, recordMutationUsers}, {10, 3, recordMutationSources}, {10, 4, recordMutationSources}, {10, 5, recordMutationSources},
		{30, 0, recordMutationEpochScopes}, {40, 0, recordMutationLeaseScopes}, {40, 1, recordMutationContentLeaseScopes}, {50, 0, recordMutationRecordGateScopes}, {60, 0, recordMutationRoots},
		{70, 0, recordMutationProjects}, {70, 1, recordMutationProjects}, {71, 0, recordMutationIdempotency},
		{73, 0, recordMutationBlobTables}, {73, 1, recordMutationBlobTables},
		{74, 4, recordMutationRevisions}, {74, 4, recordMutationNewRevisionEffects}, {74, 11, recordMutationNewRevisionEffects}, {75, 5, recordMutationNewRevisionEffects},
	}
	if len(declarations.order) != len(want) {
		t.Fatalf("declaration count=%d want=%d", len(declarations.order), len(want))
	}
	for index, expected := range want {
		actual := declarations.order[index]
		if actual.rank != expected.rank || actual.subrank != expected.subrank || actual.family != expected.family {
			t.Fatalf("order[%d]=%#v want=%#v", index, actual, expected)
		}
	}
	kinds := make([]records.SubjectKind, 0, 3)
	for _, declaration := range declarations.order {
		if declaration.family == recordMutationSources {
			kinds = append(kinds, declarations.resources.Sources[declaration.index].SourceKind)
		}
	}
	if !slices.Equal(kinds, []records.SubjectKind{records.SubjectKindVPS, records.SubjectKindMonitoringInstance, records.SubjectKindTarget}) {
		t.Fatalf("source declaration precedence = %v", kinds)
	}
}

func seedRevisionMutationDiscoveryUsers(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture) {
	t.Helper()
	for index, userID := range []string{"usr_aaaaaaaaaaaaaaaaaaaaaaaa", "usr_bbbbbbbbbbbbbbbbbbbbbbbb", "usr_cccccccccccccccccccccccc", "usr_dddddddddddddddddddddddd"} {
		var role string
		var enabled bool
		err := fixture.Runtime.QueryRow(ctx, `select role, disabled_at is null from public.users where user_id=$1`, userID).Scan(&role, &enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := fixture.Runtime.Exec(ctx, `insert into public.users(user_id,username,password_hash,display_name,role) values ($1,$2,'test-hash',$2,'admin')`, userID, fmt.Sprintf("discovery-user-%d", index)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if role != "admin" || !enabled {
			t.Fatalf("discovery user %q has role %q and enabled=%t; expected enabled admin", userID, role, enabled)
		}
	}
	if _, err := fixture.Runtime.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status,usage_status) values ($1,'PostgreSQL VPS','active','in_use') on conflict(vps_id) do nothing`, testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
}

func newRevisionMutationAdmittedRepository(t *testing.T, fixture ImportAttachmentGCFixture, participants ...records.RevisionParticipant) *PostgresRecordRepository {
	t.Helper()
	witness, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, fixture.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	live := transactionRecordSubjectSource{db: fixture.Runtime}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live), newMonitoringInstanceRecordSubjectAdapter(live), newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		t.Fatal(err)
	}
	authorization := NewPostgresCurrentRecordAuthorizationSource(fixture.Runtime, NewRecordSubjectReadResolver(subjects, witness), fixture.Gate)
	repository, err := NewPostgresRecordRepository(fixture.Runtime, fixture.Gate, participants, authorization, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func beginRevisionMutationDiscoveryReadOnly(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture) pgx.Tx {
	t.Helper()
	fixture.RefreshHeartbeat()
	tx, err := fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	// Match the production transaction sequence: verify the actual signed
	// membership/contract on this exact transaction before invoking discovery.
	if err := fixture.Gate.Admit(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	return tx
}

func assertRevisionMutationEffectBindings(t *testing.T, resources records.RevisionMutationResources, command records.RevisionCommitCommand) {
	t.Helper()
	revisionID, err := command.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	activityID, err := command.ActivityID()
	if err != nil {
		t.Fatal(err)
	}
	for _, effect := range resources.NewRevisionEffects {
		if effect.RecordID != command.RecordID || effect.RevisionID != revisionID {
			t.Fatalf("effect does not bind exact command revision: %#v want=%s", effect, revisionID)
		}
		if effect.Kind == records.RevisionMutationNewRevisionRows && (effect.ActivityID != activityID || effect.EventKind != records.RevisionMutationEventKind(command.ActivityKind)) {
			t.Fatalf("core activity does not bind exact command identity: %#v", effect)
		}
	}
}

func assertRevisionMutationBaseScopes(t *testing.T, resources records.RevisionMutationResources, command records.RevisionCommitCommand) {
	t.Helper()
	assertRevisionMutationEffectBindings(t, resources, command)
	if len(resources.Idempotency) != 1 || resources.Idempotency[0].Key != command.Idempotency.Key {
		t.Fatalf("base lost exact command idempotency key: %#v", resources.Idempotency)
	}
	if len(resources.RecordGateScopes) != len(resources.Records) {
		t.Fatalf("incomplete absence-unproven gate scopes: %#v", resources.RecordGateScopes)
	}
	for _, record := range resources.Records {
		if !slices.ContainsFunc(resources.RecordGateScopes, func(scope records.RevisionMutationRecordGateScope) bool {
			return scope.ProjectID == record.ProjectID && scope.RecordID == record.RecordID
		}) {
			t.Fatalf("missing exact gate scope for %#v", record)
		}
	}
	if !slices.ContainsFunc(resources.NewRevisionEffects, func(effect records.RevisionMutationNewRevisionEffect) bool {
		return effect.Kind == records.RevisionMutationNewRevisionRows
	}) {
		t.Fatal("missing exact future revision parent effect")
	}
}

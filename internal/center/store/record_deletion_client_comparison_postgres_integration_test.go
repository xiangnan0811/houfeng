package store_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresIntegrationRecordDeletionClientAndComparison(t *testing.T) {
	ctx := context.Background()
	fixture := store.NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	vpsRepository := store.NewPostgresVPSAssetRepository(fixture.Runtime)
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vpsRepository),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(fixture.Runtime)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(fixture.Runtime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	tombstones, err := store.NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, fixture.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	resolver := store.NewRecordSubjectReadResolver(subjects, tombstones)
	current := store.NewPostgresCurrentRecordAuthorizationSource(fixture.Runtime, resolver, fixture.Gate)
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := evidence.NewRegistry([]evidence.Kind{kind})
	if err != nil {
		t.Fatal(err)
	}
	evidenceRepository, err := store.NewPostgresEvidenceRepositoryWithReadSources(fixture.Runtime, fixture.Gate, registry, current, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := evidence.NewCapacityEnforcer(evidence.DefaultCapacityPolicy(), evidenceRepository)
	if err != nil {
		t.Fatal(err)
	}
	application, err := evidence.NewService(registry, evidenceRepository, evidenceRepository, capacity)
	if err != nil {
		t.Fatal(err)
	}
	keyDirectory := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(keyDirectory, "cmp_g2")
	if err := os.WriteFile(keyPath, key, 0o400); err != nil {
		t.Fatal(err)
	}
	signer, err := evidence.OpenComparisonIntentKeyring(keyDirectory, "cmp_g2", nil)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := evidence.NewComparisonAdmission(64 << 20)
	if err != nil {
		t.Fatal(err)
	}
	application = application.WithFixedComparison(evidenceRepository, admission, signer)
	repository, err := store.NewPostgresRecordRepository(fixture.Runtime, fixture.Gate, []records.RevisionParticipant{
		store.NewRecordAttachmentRevisionParticipant(),
		store.NewCollaborationRevisionParticipant(store.NewPostgresCollaborationMembershipReader()),
		store.NewComparisonRevisionParticipant(signer),
		store.NewRecordEvidenceRevisionParticipant(),
		store.NewRecordSearchRevisionParticipant(),
	}, current, nil)
	if err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(store.NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := records.NewRevisionService(subjects, current, repository, birth)
	if err != nil {
		t.Fatal(err)
	}
	vps, err := vpsRepository.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName: "G2 comparison source", LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	allocations := make(map[string]recordbirth.Allocation)
	targetID := func(label string) string {
		if allocation, ok := allocations[label]; ok {
			return allocation.RecordID
		}
		request := recordbirth.Request{
			ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
			Purpose: recordbirth.PurposeComparison, IdempotencyKey: label,
			RequestFingerprint: sha256.Sum256([]byte("comparison fixture allocation:" + label)),
		}
		allocation, err := birth.Reserve(ctx, request)
		if err != nil {
			t.Fatalf("reserve comparison target %q: %v", label, err)
		}
		allocations[label] = allocation
		allocations[allocation.RecordID] = allocation
		return allocation.RecordID
	}
	save := func(id, key string, preparation evidence.RevisionPreparation) (records.RevisionCommitResult, error) {
		id = targetID(id)
		allocation := allocations[id]
		return revisions.SaveRevision(ctx, records.RevisionSaveRequest{
			Actor: actor, RecordID: id, Allocation: &allocation,
			Values: records.CompleteRevisionValues{
				Title: id, BodyMarkdown: "# " + id + "\n", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
				RecordType: records.RecordTypeNote, ImpactLevel: "informational", VisibilityScope: visibility,
			},
			SubjectReferences:   []records.SubjectReference{importDestinationReference(records.SubjectKindVPS, vps.VPSID)},
			EvidencePreparation: preparation, ActivityKind: records.DomainActivityRecordCreated,
			IdempotencyKey: key, IdempotencyOwnerID: "g2-comparison", OwnerLeaseDuration: time.Minute,
			IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
		})
	}
	create := func(id string) records.RevisionCommitResult {
		t.Helper()
		id = targetID(id)
		preparation, err := evidence.NewRevisionPreparation(id, evidence.RevisionPreparationValues{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := save(id, id, preparation)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	a := create("rec_g2cmpa")
	other := create("rec_g2cmpother")
	t.Run("empty comparison index still returns a verified header", func(t *testing.T) {
		want := comparisonFixtureFramedDigestFields([][]byte{[]byte("houfeng.comparison.dependency-index.v1")})
		got, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx)
		if err != nil || got.VerifiedSetDigest != want {
			t.Fatalf("empty index = (%x, %v), want %x", got.VerifiedSetDigest, err, want)
		}
		if err := evidenceRepository.VerifyComparisonDependencyIndex(ctx); err != nil {
			t.Fatalf("verify empty comparison index: %v", err)
		}
	})
	prepare := func(id string, items []evidence.ComparisonFixedItem) evidence.ComparisonSavePreparation {
		t.Helper()
		id = targetID(id)
		now := time.Now().UTC()
		result, err := application.EvaluateFixedComparison(ctx, evidence.ComparisonEvaluateRequest{
			Actor: actor, Items: items, BaselineIndex: 0, Alignment: evidence.CoverageActual,
			RequestedWindow: evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}, Now: now,
		})
		if err != nil || !result.SaveEligibility.Eligible || result.Intent == nil {
			t.Fatalf("evaluate production comparison: eligible=%v intent=%v error=%v", result.SaveEligibility.Eligible, result.Intent != nil, err)
		}
		allocationID := allocations[id].AllocationID
		var rootExists bool
		if err := fixture.Runtime.QueryRow(ctx, `select exists(select 1 from public.records where record_id=$1)`, id).Scan(&rootExists); err != nil {
			t.Fatal(err)
		}
		if rootExists {
			allocationID = ""
		}
		prepared, err := application.PrepareComparisonSave(ctx, evidence.ComparisonSaveRequest{
			Actor: actor, AllocationID: allocationID, RecordID: id, Token: result.Intent.Token, Now: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	commit := func(id string, prepared evidence.ComparisonSavePreparation) error {
		id = targetID(id)
		preparation, err := evidence.NewRevisionPreparationFromComparisonSave(id, prepared)
		if err != nil {
			return err
		}
		_, err = save(id, id, preparation)
		return err
	}
	readDependencies := func(snapshotID string) []evidence.ComparisonDependency {
		t.Helper()
		rows, err := fixture.Runtime.Query(ctx, `select source_record_id, coalesce(source_revision_id, ''), coalesce(source_snapshot_id, '')
			from public.comparison_result_dependencies where result_snapshot_id = $1
			order by source_record_id collate "C", coalesce(source_revision_id, '') collate "C", coalesce(source_snapshot_id, '') collate "C"`, snapshotID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var result []evidence.ComparisonDependency
		for rows.Next() {
			var dependency evidence.ComparisonDependency
			if err := rows.Scan(&dependency.SourceRecordID, &dependency.SourceRevisionID, &dependency.SourceSnapshotID); err != nil {
				t.Fatal(err)
			}
			result = append(result, dependency)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	assertCompletion := func(snapshotID string, want [32]byte) {
		t.Helper()
		var digest []byte
		if err := fixture.Runtime.QueryRow(ctx, `select payload_digest from public.comparison_result_dependency_index_completions where result_snapshot_id = $1`, snapshotID).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(digest, want[:]) {
			t.Fatal("completion does not bind the actual canonical payload")
		}
	}
	metadata := prepare("rec_g2cmpmetadata", []evidence.ComparisonFixedItem{
		{Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}},
		{Revision: &evidence.ComparisonFixedRevision{RecordID: other.RecordID, RevisionID: other.RevisionID}},
	})
	if err := commit("rec_g2cmpmetadata", metadata); err != nil {
		t.Fatal(err)
	}
	wantMetadata := []evidence.ComparisonDependency{
		{SourceRecordID: a.RecordID, SourceRevisionID: a.RevisionID},
		{SourceRecordID: other.RecordID, SourceRevisionID: other.RevisionID},
	}
	sort.Slice(wantMetadata, func(i, j int) bool { return wantMetadata[i].SourceRecordID < wantMetadata[j].SourceRecordID })
	if got := readDependencies(metadata.Result.SnapshotID()); !reflect.DeepEqual(got, wantMetadata) {
		t.Fatalf("metadata-only dependencies = %#v, want %#v", got, wantMetadata)
	}
	assertCompletion(metadata.Result.SnapshotID(), metadata.Result.Snapshot().Hash())

	metadataID := metadata.Result.SnapshotID()
	copied := prepare("rec_g2cmpcopy", []evidence.ComparisonFixedItem{
		{SnapshotID: &metadataID},
		{Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}},
	})
	if err := commit("rec_g2cmpcopy", copied); err != nil {
		t.Fatal(err)
	}
	if len(copied.Copies) != 1 {
		t.Fatalf("comparison copy count = %d, want 1", len(copied.Copies))
	}
	if got := readDependencies(copied.Copies[0].SnapshotID()); !reflect.DeepEqual(got, wantMetadata) {
		t.Fatalf("copied result changed source owners: %#v", got)
	}
	assertCompletion(copied.Copies[0].SnapshotID(), metadata.Result.Snapshot().Hash())
	wantCopied := []evidence.ComparisonDependency{
		{SourceRecordID: a.RecordID, SourceRevisionID: a.RevisionID},
		{SourceRecordID: targetID("rec_g2cmpmetadata"), SourceSnapshotID: metadataID},
	}
	sort.Slice(wantCopied, func(i, j int) bool { return wantCopied[i].SourceRecordID < wantCopied[j].SourceRecordID })
	if got := readDependencies(copied.Result.SnapshotID()); !reflect.DeepEqual(got, wantCopied) {
		t.Fatalf("mixed snapshot/metadata dependencies = %#v, want %#v", got, wantCopied)
	}
	assertCompletion(copied.Result.SnapshotID(), copied.Result.Snapshot().Hash())
	if err := evidenceRepository.VerifyComparisonDependencyIndex(ctx); err != nil {
		t.Fatalf("complete native result set is not ready: %v", err)
	}
	initialHealth, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Native self-owned results are not surviving copies. A retained result
	// with two different revisions of A is one copy, not two source rows.
	own := prepare(a.RecordID, []evidence.ComparisonFixedItem{
		{Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}},
		{Revision: &evidence.ComparisonFixedRevision{RecordID: other.RecordID, RevisionID: other.RevisionID}},
	})
	ownPreparation, err := evidence.NewRevisionPreparationFromComparisonSave(a.RecordID, own)
	if err != nil {
		t.Fatal(err)
	}
	oldA := a
	a, err = revisions.SaveRevision(ctx, records.RevisionSaveRequest{
		Actor: actor, RecordID: a.RecordID, BaseRevisionID: a.RevisionID,
		LockVersion: a.LockVersion, AuthorizationEpoch: a.AuthorizationEpoch,
		Values: records.CompleteRevisionValues{
			Title: a.RecordID, BodyMarkdown: "# retained A revision\n", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
			RecordType: records.RecordTypeNote, ImpactLevel: "informational", VisibilityScope: visibility,
		},
		SubjectReferences:   []records.SubjectReference{importDestinationReference(records.SubjectKindVPS, vps.VPSID)},
		EvidencePreparation: ownPreparation, ActivityKind: records.DomainActivityRecordRevised,
		IdempotencyKey: "g2-comparison-own-result", IdempotencyOwnerID: "g2-comparison",
		OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	multiple := prepare("rec_g2cmpmultiple", []evidence.ComparisonFixedItem{
		{Revision: &evidence.ComparisonFixedRevision{RecordID: oldA.RecordID, RevisionID: oldA.RevisionID}},
		{Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}},
	})
	if err := commit("rec_g2cmpmultiple", multiple); err != nil {
		t.Fatal(err)
	}
	if got := readDependencies(multiple.Result.SnapshotID()); len(got) != 2 ||
		got[0].SourceRecordID != a.RecordID || got[1].SourceRecordID != a.RecordID {
		t.Fatalf("multiple-source result lacks two actual A dependencies: %#v", got)
	}
	// The new A revision contains own.Result. Selecting that revision copies
	// its result into B as well as creating multiple.Result. That copied
	// canonical result retains A's old-revision dependency and is a separate
	// surviving snapshot, not a duplicate dependency row of multiple.Result.
	if len(multiple.Copies) != 1 || multiple.Copies[0].CopiedFromSnapshotID() != own.Result.SnapshotID() {
		t.Fatalf("new A revision did not produce its one authentic result copy: %#v", multiple.Copies)
	}
	if got, want := readDependencies(multiple.Copies[0].SnapshotID()), readDependencies(own.Result.SnapshotID()); !reflect.DeepEqual(got, want) {
		t.Fatalf("copied A result dependencies = %#v, want original native dependencies %#v", got, want)
	}
	expectedRetainedSnapshotIDs := []string{
		metadata.Result.SnapshotID(),
		copied.Copies[0].SnapshotID(),
		copied.Result.SnapshotID(),
		multiple.Copies[0].SnapshotID(),
		multiple.Result.SnapshotID(),
	}
	sort.Strings(expectedRetainedSnapshotIDs)
	var actualRetainedSnapshotIDs []string
	if err := fixture.Runtime.QueryRow(ctx, `select coalesce(array_agg(snapshot.snapshot_id
		order by snapshot.snapshot_id collate "C"), '{}'::text[])
		from public.evidence_snapshots as snapshot
		join public.records as owner on owner.record_id = snapshot.record_id
		where owner.project_id = $1 and owner.record_id <> $2 and snapshot.kind = $3
		  and exists (select 1 from public.comparison_result_dependencies as dependency
		    where dependency.result_snapshot_id = snapshot.snapshot_id
		      and dependency.project_id = owner.project_id and dependency.source_record_id = $2)`,
		string(recordauth.ProjectIDDefault), a.RecordID, string(evidence.ComparisonResultV1Key().Kind),
	).Scan(&actualRetainedSnapshotIDs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualRetainedSnapshotIDs, expectedRetainedSnapshotIDs) {
		t.Fatalf("actual retained result snapshots = %q, want exact native fixture set %q (excluding A-owned %q)",
			actualRetainedSnapshotIDs, expectedRetainedSnapshotIDs, own.Result.SnapshotID())
	}
	target := recorddeletion.PreviewTarget{
		Object:            recordplatform.ObjectRef{ProjectID: string(recordauth.ProjectIDDefault), ObjectKind: "record", ObjectID: a.RecordID},
		CurrentRevisionID: a.RevisionID, LockVersion: a.LockVersion, AuthorizationEpoch: a.AuthorizationEpoch,
		DependencyGraphDigest: sha256.Sum256([]byte("g2-comparison-dependency-graph")),
	}
	var deliveryEpoch int64
	if err := fixture.Runtime.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs
		where project_id = $1 and object_kind = 'record' and object_id = $2`,
		target.Object.ProjectID, target.Object.ObjectID).Scan(&deliveryEpoch); err != nil {
		t.Fatal(err)
	}
	target.ContentDeliveryEpoch = recordplatform.ContentEpoch(deliveryEpoch)
	retainedState := func() []byte {
		t.Helper()
		var state []byte
		if err := fixture.Runtime.QueryRow(ctx, `select pg_catalog.convert_to(jsonb_build_object(
			'snapshots', (select jsonb_agg(jsonb_build_array(to_jsonb(snapshot), to_jsonb(payload))
			  order by snapshot.snapshot_id collate "C") from public.evidence_snapshots as snapshot
			  join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
			  where snapshot.record_id <> $1),
			'lineage', (select jsonb_agg(to_jsonb(lineage) order by lineage.snapshot_id collate "C")
			  from public.evidence_copy_lineage as lineage join public.evidence_snapshots as snapshot
			    on snapshot.snapshot_id = lineage.snapshot_id where snapshot.record_id <> $1),
			'references', (select jsonb_agg(to_jsonb(reference)
			  order by reference.revision_id collate "C", reference.snapshot_id collate "C")
			  from public.record_revision_evidence as reference where reference.record_id <> $1),
			'dependencies', (select jsonb_agg(to_jsonb(dependency)
			  order by dependency.result_snapshot_id collate "C", dependency.source_record_id collate "C",
			    dependency.source_revision_id collate "C", dependency.source_snapshot_id collate "C")
			  from public.comparison_result_dependencies as dependency join public.evidence_snapshots as snapshot
			    on snapshot.snapshot_id = dependency.result_snapshot_id where snapshot.record_id <> $1)
			)::text, 'UTF8')`, a.RecordID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	t.Run("comparison preview counts distinct retained results without identities", func(t *testing.T) {
		before := retainedState()
		health, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx)
		if err != nil || health.VerifiedSetDigest == [32]byte{} || health.VerifiedSetDigest == initialHealth.VerifiedSetDigest {
			t.Fatalf("verified index-only health = (%#v, %v)", health, err)
		}
		preview, err := evidenceRepository.PreviewComparisonDeletion(ctx, target)
		if err != nil || preview.Validate() != nil {
			t.Fatalf("comparison preview = (%#v, %v)", preview, err)
		}
		if len(preview.SurvivingCopies) != 1 || preview.SurvivingCopies[0].Kind != recorddeletion.SurvivingCopyKindOtherRecord ||
			preview.SurvivingCopies[0].CountStatus != recorddeletion.SurvivingCopyCountKnown ||
			preview.SurvivingCopies[0].CopyCount == nil {
			t.Fatalf("distinct other-owner result disclosure has invalid shape: %#v", preview.SurvivingCopies)
		}
		if got, want := *preview.SurvivingCopies[0].CopyCount, uint64(len(expectedRetainedSnapshotIDs)); got != want {
			t.Fatalf("distinct other-owner result count = %d, want %d for exact retained snapshots %q; A-owned %q excluded",
				got, want, expectedRetainedSnapshotIDs, own.Result.SnapshotID())
		}
		repeated, err := evidenceRepository.PreviewComparisonDeletion(ctx, target)
		if err != nil || !reflect.DeepEqual(preview, repeated) {
			t.Fatalf("unchanged preview drifted: (%#v, %v)", repeated, err)
		}
		disclosure, err := json.Marshal(preview)
		if err != nil {
			t.Fatal(err)
		}
		identities := append([]string{a.RecordID, a.RevisionID, oldA.RevisionID, other.RecordID,
			other.RevisionID, own.Result.SnapshotID(), targetID("rec_g2cmpmetadata"), targetID("rec_g2cmpcopy"), targetID("rec_g2cmpmultiple")},
			expectedRetainedSnapshotIDs...)
		for _, identity := range identities {
			if bytes.Contains(disclosure, []byte(identity)) {
				t.Fatalf("comparison disclosure contains source/result identity %q", identity)
			}
		}
		if after := retainedState(); !bytes.Equal(before, after) {
			t.Fatal("read-only preview changed retained B bytes, lineage, references, or dependencies")
		}
	})
	t.Run("comparison decision preserves digest framing and inventory bytes", func(t *testing.T) {
		multipleDependencies := []evidence.ComparisonDependency{
			{SourceRecordID: a.RecordID, SourceRevisionID: oldA.RevisionID},
			{SourceRecordID: a.RecordID, SourceRevisionID: a.RevisionID, SourceSnapshotID: own.Result.SnapshotID()},
		}
		entries := []comparisonFixtureIndexEntry{
			{metadata.Result.SnapshotID(), targetID("rec_g2cmpmetadata"), metadata.Result.Snapshot().Hash(), wantMetadata},
			{copied.Copies[0].SnapshotID(), targetID("rec_g2cmpcopy"), metadata.Result.Snapshot().Hash(), wantMetadata},
			{copied.Result.SnapshotID(), targetID("rec_g2cmpcopy"), copied.Result.Snapshot().Hash(), wantCopied},
			{own.Result.SnapshotID(), a.RecordID, own.Result.Snapshot().Hash(), wantMetadata},
			{multiple.Copies[0].SnapshotID(), targetID("rec_g2cmpmultiple"), own.Result.Snapshot().Hash(), wantMetadata},
			{multiple.Result.SnapshotID(), targetID("rec_g2cmpmultiple"), multiple.Result.Snapshot().Hash(), multipleDependencies},
		}
		wantIndex := comparisonFixtureIndexDigest(entries)
		index, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx)
		if err != nil || index.VerifiedSetDigest != wantIndex {
			t.Fatalf("index digest = (%x, %v), want %x", index.VerifiedSetDigest, err, wantIndex)
		}
		material, count := comparisonLegacyInventory(t, ctx, fixture.Runtime, target)
		if count != int64(len(expectedRetainedSnapshotIDs)) {
			t.Fatalf("legacy inventory retained count = %d, want %d", count, len(expectedRetainedSnapshotIDs))
		}
		var countBytes [8]byte
		binary.BigEndian.PutUint64(countBytes[:], uint64(count))
		wantDependency := comparisonFixtureFramedDigest("houfeng.record-comparison.deletion-preview.v1", target.DependencyGraphDigest[:], wantIndex[:], material)
		wantImpact := comparisonFixtureFramedDigest("houfeng.record-comparison.deletion-impact.v1", wantIndex[:], material, countBytes[:])
		preview, err := evidenceRepository.PreviewComparisonDeletion(ctx, target)
		if err != nil || preview.DependencyDigest != wantDependency || preview.ImpactDigest != wantImpact {
			t.Fatalf("preview digests = (%x, %x, %v), want (%x, %x)", preview.DependencyDigest, preview.ImpactDigest, err, wantDependency, wantImpact)
		}
		surfaceDigest := recorddeletion.RecordComparisonSurfaceDigest()
		wantHealth := comparisonFixtureFramedDigest("houfeng.record-comparison.deletion-health.v1", surfaceDigest[:], wantIndex[:])
		health, err := evidenceRepository.RecordComparisonHealth(ctx)
		if err != nil || !health.Healthy() || health.Revision() != 1 || health.ProofDigest() != wantHealth {
			t.Fatalf("adapter health = (%#v, %v), want proof %x", health, err, wantHealth)
		}
	})
	t.Run("runtime comparison mutations obey the decision fence and current routes", func(t *testing.T) {
		// Remove a certified copy index to exercise an absent completion as well
		// as a new client row. The fixture restores the exact original rows.
		copyID := copied.Copies[0].SnapshotID()
		if err := store.DamageComparisonIndexForTest(t, ctx, fixture.Runtime, copyID, "drop_index", "", "", ""); err != nil {
			t.Fatal(err)
		}
		store.SeedComparisonClientInventoryForTest(t, ctx, fixture.Runtime, a.RecordID)
		readInventory := func() []byte {
			t.Helper()
			var state []byte
			if err := fixture.Runtime.QueryRow(ctx, `select convert_to(jsonb_build_object(
				'dependencies',(select jsonb_agg(to_jsonb(d) order by d.result_snapshot_id,d.source_record_id,d.source_revision_id,d.source_snapshot_id) from public.comparison_result_dependencies d),
				'completions',(select jsonb_agg(to_jsonb(c) order by c.result_snapshot_id) from public.comparison_result_dependency_index_completions c),
				'workbenches',(select jsonb_agg(to_jsonb(w) order by w.user_id,w.browser_id,w.workbench_digest) from public.comparison_client_workbenches w),
				'saves',(select jsonb_agg(to_jsonb(s) order by s.user_id,s.browser_id,s.save_digest) from public.comparison_client_save_attempts s),
				'snapshots',(select jsonb_agg(to_jsonb(s) order by s.snapshot_id) from public.evidence_snapshots s),
				'payloads',(select jsonb_agg(to_jsonb(p) order by p.payload_digest) from public.evidence_payloads p),
				'lineage',(select jsonb_agg(to_jsonb(l) order by l.snapshot_id) from public.evidence_copy_lineage l),
				'references',(select jsonb_agg(to_jsonb(r) order by r.revision_id,r.ordinal) from public.record_revision_evidence r),
				'roots',(select jsonb_agg(to_jsonb(r) order by r.record_id) from public.records r),
				'epochs',(select jsonb_agg(to_jsonb(e) order by e.object_id) from public.content_delivery_epochs e where e.object_kind='record'))::text,'UTF8')`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			return state
		}
		before := readInventory()
		holder, err := fixture.Runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(context.Background()) }()
		if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(1213154899,81)`); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`insert into public.comparison_result_dependencies (result_snapshot_id,source_record_id,source_revision_id) values ($1,$2,'rrv_unprotected')`,
			`update public.comparison_result_dependencies set source_record_id=source_record_id where result_snapshot_id=$1 and source_record_id=$2`,
			`delete from public.comparison_result_dependencies where result_snapshot_id=$1 and source_record_id=$2`,
			`insert into public.comparison_result_dependency_index_completions (result_snapshot_id,payload_digest) values ($1,decode(repeat('ab',32),'hex'))`,
			`update public.comparison_result_dependency_index_completions set payload_digest=payload_digest where result_snapshot_id=$1`,
			`delete from public.comparison_result_dependency_index_completions where result_snapshot_id=$1`,
			`insert into public.comparison_client_workbenches (user_id,browser_id,workbench_digest,destination_record_id,source_record_ids,generation) values ('usr_guard','guard',decode(repeat('ab',32),'hex'),$1,array[$2],1)`,
			`update public.comparison_client_workbenches set generation=generation+1 where destination_record_id=$1`,
			`delete from public.comparison_client_workbenches where destination_record_id=$1`,
			`insert into public.comparison_client_save_attempts (user_id,browser_id,save_digest,destination_record_id,source_record_ids,idempotency_key,generation) values ('usr_guard','guard',decode(repeat('ab',32),'hex'),$1,array[$2],'guard',1)`,
			`update public.comparison_client_save_attempts set generation=generation+1 where destination_record_id=$1`,
			`delete from public.comparison_client_save_attempts where destination_record_id=$1`,
		} {
			args := []any{copyID}
			if strings.Contains(statement, "$2") {
				args = append(args, a.RecordID)
			}
			if strings.Contains(statement, "comparison_client_") {
				args[0] = a.RecordID
			} else if !strings.HasPrefix(statement, "insert") {
				args[0] = metadataID
			}
			_, err := fixture.Runtime.Exec(ctx, statement, args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("raw comparison mutation %q = %v, want insufficient privilege", statement, err)
			}
		}
		for _, probe := range []struct {
			statement string
			args      []any
		}{
			{`insert into public.evidence_snapshots select (jsonb_populate_record(null::public.evidence_snapshots,to_jsonb(s)||jsonb_build_object('snapshot_id','evs_guardunseen'))).* from public.evidence_snapshots s where s.snapshot_id=$1`, []any{metadataID}},
			{`delete from public.evidence_snapshots where snapshot_id=$1`, []any{metadataID}},
			{`insert into public.evidence_copy_lineage(snapshot_id,copied_from_snapshot_id,copy_reason) values ($1,$2,'explicit comparison copy')`, []any{metadataID, copyID}},
			{`delete from public.evidence_copy_lineage where snapshot_id=$1`, []any{copyID}},
			{`insert into public.record_revision_evidence(record_id,revision_id,ordinal,snapshot_id) select record_id,revision_id,999,snapshot_id from public.record_revision_evidence where snapshot_id=$1 limit 1`, []any{metadataID}},
			{`delete from public.record_revision_evidence where snapshot_id=$1`, []any{metadataID}},
			{`delete from public.evidence_payloads where payload_digest=(select payload_digest from public.evidence_snapshots where snapshot_id=$1)`, []any{metadataID}},
			{`update public.records set current_revision_id=current_revision_id where record_id=$1`, []any{a.RecordID}},
			{`update public.content_delivery_epochs set delivery_epoch=delivery_epoch+1 where project_id='default' and object_kind='record' and object_id=$1`, []any{a.RecordID}},
			{`insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values ('default','record','rec_guardunseen',0)`, nil},
			{`delete from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1`, []any{a.RecordID}},
		} {
			mutation, err := fixture.Runtime.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = mutation.Exec(ctx, probe.statement, probe.args...)
			_ = mutation.Rollback(ctx)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
				t.Fatalf("comparison input mutation %q = %v, want fence retry", probe.statement, err)
			}
		}
		makeCommand := func(recordID, inheritedFrom string, dependencies []evidence.ComparisonDependency) []byte {
			t.Helper()
			items := make([]map[string]string, 0, len(dependencies))
			for _, dependency := range dependencies {
				items = append(items, map[string]string{"record_id": dependency.SourceRecordID,
					"revision_id": dependency.SourceRevisionID, "snapshot_id": dependency.SourceSnapshotID})
			}
			payload, err := json.Marshal(map[string]any{"record_id": recordID, "snapshot_id": copyID,
				"payload_digest": fmt.Sprintf("%x", metadata.Result.Snapshot().Hash()), "inherited_from": inheritedFrom, "dependencies": items})
			if err != nil {
				t.Fatal(err)
			}
			return payload
		}
		owner := targetID("rec_g2cmpcopy")
		valid := makeCommand(owner, metadataID, wantMetadata)
		_, err = fixture.Runtime.Exec(ctx, `select public.record_comparison_index_write($1::bytea,$2::bytea)`, valid, metadata.Result.Snapshot().Bytes())
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
			t.Fatalf("index command under exclusive comparison fence = %v, want retry", err)
		}
		if after := readInventory(); !bytes.Equal(after, before) {
			t.Fatal("runtime mutation changed comparison inventory under the exclusive fence")
		}
		if err := holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = fixture.Runtime.Exec(ctx, `select public.record_comparison_index_write($1::bytea,$2::bytea)`,
			makeCommand(other.RecordID, metadataID, wantMetadata), metadata.Result.Snapshot().Bytes())
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			t.Fatalf("changed destination route = %v, want closed", err)
		}
		forged := append([]evidence.ComparisonDependency(nil), wantMetadata...)
		for index := range forged {
			if forged[index].SourceRecordID == a.RecordID {
				forged[index].SourceRecordID = other.RecordID
			}
		}
		_, err = fixture.Runtime.Exec(ctx, `select public.record_comparison_index_write($1::bytea,$2::bytea)`,
			makeCommand(owner, metadataID, forged), metadata.Result.Snapshot().Bytes())
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			t.Fatalf("changed source route = %v, want closed", err)
		}
		_, err = fixture.Runtime.Exec(ctx, `select public.record_comparison_index_write($1::bytea,$2::bytea)`,
			makeCommand(owner, own.Result.SnapshotID(), wantMetadata), metadata.Result.Snapshot().Bytes())
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			t.Fatalf("changed certified parent route = %v, want closed", err)
		}
		if after := readInventory(); !bytes.Equal(after, before) {
			t.Fatal("rejected comparison route changed the durable inventory")
		}
		if _, err := fixture.Runtime.Exec(ctx, `select public.record_comparison_index_write($1::bytea,$2::bytea)`,
			valid, metadata.Result.Snapshot().Bytes()); err != nil {
			t.Fatalf("publish the actual comparison route: %v", err)
		}
		if got := readDependencies(copyID); !reflect.DeepEqual(got, wantMetadata) {
			t.Fatalf("guarded comparison dependencies = %#v, want %#v", got, wantMetadata)
		}
		assertCompletion(copyID, metadata.Result.Snapshot().Hash())
	})
	assertDecisionIncomplete := func(t *testing.T) {
		t.Helper()
		if err := evidenceRepository.VerifyComparisonDependencyIndex(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) {
			t.Errorf("maintenance verification with damaged canonical result = %v", err)
		}
		if got, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) || got.VerifiedSetDigest != [32]byte{} {
			t.Errorf("index health with damaged canonical result = (%#v, %v)", got, err)
		}
		if got, err := evidenceRepository.RecordComparisonHealth(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) || got != (recorddeletion.AdapterHealthSnapshot{}) {
			t.Errorf("adapter health with damaged canonical result = (%#v, %v)", got, err)
		}
		if got, err := evidenceRepository.PreviewComparisonDeletion(ctx, target); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) || !reflect.DeepEqual(got, recorddeletion.AdapterPreviewSnapshot{}) {
			t.Errorf("preview with damaged canonical result = (%#v, %v)", got, err)
		}
	}
	t.Run("READ COMMITTED decision remains on one statement snapshot", func(t *testing.T) {
		beforeIndex, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforeHealth, err := evidenceRepository.RecordComparisonHealth(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforePreview, err := evidenceRepository.PreviewComparisonDeletion(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		deleted := false
		testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		tx, err := fixture.Runtime.BeginTx(testCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if err := fixture.Gate.Admit(testCtx, tx); err != nil {
			t.Fatal(err)
		}
		index, health, preview, err := store.ReadComparisonDecisionForTest(testCtx, tx, &target, func() error {
			if err := store.DamageComparisonIndexForTest(t, testCtx, fixture.Runtime, multiple.Result.SnapshotID(),
				"missing_dependency", a.RecordID, oldA.RevisionID, ""); err != nil {
				return err
			}
			deleted = true
			return nil
		})
		if err != nil || !deleted || index != beforeIndex.VerifiedSetDigest || health != beforeHealth || !reflect.DeepEqual(preview, beforePreview) {
			t.Fatalf("decision spanning committed dependency deletion = (%x, %#v, %#v, %v), want pre-mutation decision", index, health, preview, err)
		}
		if _, _, _, err := store.ReadComparisonDecisionForTest(testCtx, tx, nil, nil); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) {
			t.Fatalf("next READ COMMITTED statement did not observe removed coverage: %v", err)
		}
	})
	t.Run("missing comparison payload rejects every decision", func(t *testing.T) {
		store.CorruptComparisonPayloadForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "missing")
		assertDecisionIncomplete(t)
	})
	t.Run("corrupt compressed comparison payload rejects every decision", func(t *testing.T) {
		store.CorruptComparisonPayloadForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "compressed")
		assertDecisionIncomplete(t)
	})
	t.Run("valid gzip with different canonical content rejects every decision", func(t *testing.T) {
		store.CorruptComparisonPayloadForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "mismatch")
		assertDecisionIncomplete(t)
	})
	t.Run("malformed canonical comparison rejects every decision", func(t *testing.T) {
		store.CorruptComparisonPayloadForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "canonical")
		assertDecisionIncomplete(t)
	})
	t.Run("missing comparison completion rejects every decision", func(t *testing.T) {
		if err := store.DamageComparisonIndexForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "missing_completion", "", "", ""); err != nil {
			t.Fatal(err)
		}
		assertDecisionIncomplete(t)
	})
	t.Run("mismatched completion digest rejects every decision", func(t *testing.T) {
		if err := store.DamageComparisonIndexForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "wrong_completion", "", "", ""); err != nil {
			t.Fatal(err)
		}
		assertDecisionIncomplete(t)
	})
	t.Run("seven dependencies reject every decision", func(t *testing.T) {
		if err := store.DamageComparisonIndexForTest(t, ctx, fixture.Runtime, metadata.Result.SnapshotID(), "extra_dependencies", "", "", ""); err != nil {
			t.Fatal(err)
		}
		assertDecisionIncomplete(t)
	})
	t.Run("comparison preview rejects stale target CAS", func(t *testing.T) {
		for _, field := range []string{"revision", "lock", "authorization", "content", "root"} {
			t.Run(field, func(t *testing.T) {
				stale := target
				switch field {
				case "revision":
					stale.CurrentRevisionID = oldA.RevisionID
				case "lock":
					stale.LockVersion++
				case "authorization":
					stale.AuthorizationEpoch++
				case "content":
					stale.ContentDeliveryEpoch++
				case "root":
					stale.Object.ObjectID = "rec_g2cmpmissing"
				}
				got, err := evidenceRepository.PreviewComparisonDeletion(ctx, stale)
				if !errors.Is(err, recorddeletion.ErrDeletionPreviewStale) || !reflect.DeepEqual(got, recorddeletion.AdapterPreviewSnapshot{}) {
					t.Fatalf("stale %s preview = (%#v, %v)", field, got, err)
				}
			})
		}
	})
	t.Run("comparison preview rejects corrupt canonical coverage", func(t *testing.T) {
		if err := store.DamageComparisonIndexForTest(t, ctx, fixture.Runtime, multiple.Result.SnapshotID(),
			"wrong_revision", a.RecordID, oldA.RevisionID, other.RevisionID); err != nil {
			t.Fatal(err)
		}
		if err := evidenceRepository.VerifyComparisonDependencyIndex(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) {
			t.Fatalf("corrupt coverage passed canonical verification: %v", err)
		}
		if got, err := evidenceRepository.PreviewComparisonDeletion(ctx, target); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) ||
			!reflect.DeepEqual(got, recorddeletion.AdapterPreviewSnapshot{}) {
			t.Fatalf("corrupt coverage disclosed a preview: (%#v, %v)", got, err)
		}
		if got, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) ||
			got.VerifiedSetDigest != [32]byte{} {
			t.Fatalf("corrupt coverage reported index health: (%#v, %v)", got, err)
		}
	})

	forged := prepare("rec_g2cmpforged", []evidence.ComparisonFixedItem{
		{SnapshotID: &metadataID},
		{Revision: &evidence.ComparisonFixedRevision{RecordID: a.RecordID, RevisionID: a.RevisionID}},
	})
	forged.Claims.Items[0].SourceRecordID = "rec_g2cmpforged"
	if err := commit("rec_g2cmpforged", forged); !errors.Is(err, evidence.ErrComparisonIntentStale) {
		t.Fatalf("changed unsigned preparation owner: %v", err)
	}
	var persisted bool
	if err := fixture.Runtime.QueryRow(ctx, `select exists (select 1 from public.records where record_id = $1)`, targetID("rec_g2cmpforged")).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted || len(readDependencies(forged.Result.SnapshotID())) != 0 {
		t.Fatal("rejected owner left a partial record or dependency index")
	}

	// Lose only the derived copy index, never canonical evidence. Backfill must
	// reconstruct it from its certified parent and source, not guessed IDs.
	if err := store.DamageComparisonIndexForTest(t, ctx, fixture.Runtime, copied.Copies[0].SnapshotID(), "drop_index", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := evidenceRepository.VerifyComparisonDependencyIndex(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) {
		t.Fatalf("missing copy index did not block readiness: %v", err)
	}
	if got, err := evidenceRepository.PreviewComparisonDeletion(ctx, target); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) ||
		!reflect.DeepEqual(got, recorddeletion.AdapterPreviewSnapshot{}) {
		t.Fatalf("missing index disclosed a preview: (%#v, %v)", got, err)
	}
	// Build real job, allocation and mapping rows. Both records belong to the
	// same plan, but only the first owns this imported evidence identity.
	importFixture := func(label string, wrongOwner bool) (evidence.PreparedImportedSnapshot, error) {
		t.Helper()
		portability := store.NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, tombstones, nil)
		destination := importDestinationReference(records.SubjectKindVPS, vps.VPSID)
		archiveDigest := sha256.Sum256([]byte("comparison-import-archive:" + label))
		job, err := portability.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{ActorID: actor.UserID,
			IdempotencyKey: label, ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if err != nil {
			return evidence.PreparedImportedSnapshot{}, err
		}
		sourceIDs := []string{"rec_archivea", "rec_archiveb"}
		allocated := make([]recordbirth.Allocation, 0, len(sourceIDs))
		remaps := make([]store.ImportRemap, 0, 3)
		documents := make([]store.ImportDocumentPlan, 0, 2)
		for _, sourceID := range sourceIDs {
			identity, err := json.Marshal(struct{ Domain, JobID, SourceID string }{"houfeng.record-import-allocation-key.v1", job.ImportJobID, sourceID})
			if err != nil {
				return evidence.PreparedImportedSnapshot{}, err
			}
			keyDigest := sha256.Sum256(identity)
			fingerprint, err := json.Marshal(struct {
				Domain        string
				JobID         string
				ArchiveDigest [32]byte
				SourceID      string
				Destination   records.SubjectReference
			}{"houfeng.record-import-allocation.v1", job.ImportJobID, job.ArchiveDigest, sourceID, destination})
			if err != nil {
				return evidence.PreparedImportedSnapshot{}, err
			}
			allocation, err := birth.Reserve(ctx, recordbirth.Request{ProjectID: string(actor.ProjectID), ActorID: actor.UserID,
				ActorScopeDigest: actor.CanonicalHash(), Purpose: recordbirth.PurposeImport,
				IdempotencyKey: fmt.Sprintf("import-%x", keyDigest), RequestFingerprint: sha256.Sum256(fingerprint)})
			if err != nil {
				return evidence.PreparedImportedSnapshot{}, err
			}
			allocated = append(allocated, allocation)
			remaps = append(remaps, store.ImportRemap{EntityKind: "record", SourceID: sourceID, TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID})
			documents = append(documents, store.ImportDocumentPlan{SourceID: sourceID, TargetID: allocation.RecordID, Title: sourceID})
		}
		snapshotID := "evs_g2cmpimport"
		if wrongOwner {
			snapshotID = "evs_g2cmpimportwrong"
		}
		remaps = append(remaps, store.ImportRemap{EntityKind: "evidence", SourceID: "evs_archiveresult", TargetID: snapshotID, OwnerTargetRecordID: allocated[0].RecordID})
		planDigest := sha256.Sum256([]byte("comparison-import-plan:" + label))
		plan, err := portability.SaveImportPlan(ctx, store.SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor,
			ArchiveDigest: archiveDigest, PlanDigest: planDigest, ObjectCount: 3, RemapCount: 3, Remaps: remaps,
			Allocations: allocated, Documents: documents, DestinationSubject: destination, ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if err != nil {
			return evidence.PreparedImportedSnapshot{}, err
		}
		if err := portability.AdvanceImportJob(ctx, store.AdvanceRecordImportJobInput{ImportJobID: job.ImportJobID,
			LockVersion: job.LockVersion, JobState: store.RecordImportJobStatePlanned}); err != nil {
			return evidence.PreparedImportedSnapshot{}, err
		}
		ownerIndex := 0
		if wrongOwner {
			ownerIndex = 1
		}
		imported, err := evidence.NewPreparedImportedSnapshot(allocated[ownerIndex].RecordID, snapshotID, metadata.Result.Snapshot(),
			evidence.ImportedSnapshotRoute{ImportJobID: job.ImportJobID, ImportPlanID: plan.ImportPlanID})
		if err != nil {
			return evidence.PreparedImportedSnapshot{}, err
		}
		requests := make([]records.RevisionSaveRequest, 0, 2)
		for index := range allocated {
			values := evidence.RevisionPreparationValues{}
			if index == ownerIndex {
				values.Imported = []evidence.PreparedImportedSnapshot{imported}
				values.OrderedSnapshotIDs = []string{snapshotID}
			}
			preparation, err := evidence.NewRevisionPreparation(allocated[index].RecordID, values)
			if err != nil {
				return evidence.PreparedImportedSnapshot{}, err
			}
			requests = append(requests, records.RevisionSaveRequest{Actor: actor, RecordID: allocated[index].RecordID, Allocation: &allocated[index],
				Values: records.CompleteRevisionValues{Title: sourceIDs[index], BodyMarkdown: "# import\n", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
					RecordType: records.RecordTypeNote, ImpactLevel: "informational", VisibilityScope: visibility},
				SubjectReferences: []records.SubjectReference{destination}, EvidencePreparation: preparation, ActivityKind: records.DomainActivityRecordCreated,
				IdempotencyKey: label + sourceIDs[index], IdempotencyOwnerID: "g2-comparison", OwnerLeaseDuration: time.Minute,
				IdempotencyTTL: time.Hour, OutboxTTL: time.Hour})
		}
		_, err = revisions.SaveRevisionsFinishing(ctx, requests, records.RevisionCommitFinish{Actor: actor, OriginKind: "import",
			OriginDigest: archiveDigest, SourceRecord: sourceIDs[0], ImportJobID: job.ImportJobID, JobLockVersion: job.LockVersion + 1})
		if wrongOwner {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "55000" || pgErr.Message != "evidence payload binding unavailable or ambiguous" {
				return imported, fmt.Errorf("same-plan wrong owner did not reach restricted payload guard: %w", err)
			}
			var count int
			if scanErr := fixture.Runtime.QueryRow(ctx, `select count(*) from public.records where record_id=any($1::text[])`, []string{allocated[0].RecordID, allocated[1].RecordID}).Scan(&count); scanErr != nil || count != 0 {
				return imported, fmt.Errorf("wrong-owner import left %d roots: %v", count, scanErr)
			}
			return imported, nil
		}
		return imported, err
	}
	if _, err := importFixture("comparison-import-wrong-owner", true); err != nil {
		t.Fatal(err)
	}
	imported, err := importFixture("comparison-import-valid", false)
	if err != nil {
		t.Fatal(err)
	}
	// The imported bytes deliberately contain IDs which really exist locally.
	// Canonical equality must not turn that collision into ownership evidence.
	// Five exact retained snapshots above, plus the original A-owned result
	// and this unresolved import: seven actual canonical results in the sweep.
	expectedSweepSnapshotIDs := append([]string{}, expectedRetainedSnapshotIDs...)
	expectedSweepSnapshotIDs = append(expectedSweepSnapshotIDs, own.Result.SnapshotID(), imported.SnapshotID())
	sort.Strings(expectedSweepSnapshotIDs)
	var actualSweepSnapshotIDs []string
	if err := fixture.Runtime.QueryRow(ctx, `select coalesce(array_agg(snapshot_id
		order by snapshot_id collate "C"), '{}'::text[]) from public.evidence_snapshots where kind = $1`,
		string(evidence.ComparisonResultV1Key().Kind)).Scan(&actualSweepSnapshotIDs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualSweepSnapshotIDs, expectedSweepSnapshotIDs) {
		t.Fatalf("actual canonical sweep set = %q, want seven explicit native/import fixture snapshots %q",
			actualSweepSnapshotIDs, expectedSweepSnapshotIDs)
	}
	var after string
	var examined, incomplete uint64
	for passes := 0; ; passes++ {
		if passes > 8 {
			t.Fatal("bounded index sweep did not terminate")
		}
		batch, err := evidenceRepository.BackfillComparisonDependencyIndex(ctx, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if batch.Examined > 1 {
			t.Fatalf("index pass exceeded its bound: %#v", batch)
		}
		examined += batch.Examined
		incomplete += batch.Incomplete
		after = batch.AfterSnapshotID
		if after == "" {
			break
		}
	}
	if wantExamined := uint64(len(expectedSweepSnapshotIDs)); examined != wantExamined || incomplete != 1 {
		t.Fatalf("index sweep examined=%d incomplete=%d, want %d/1 for explicit snapshot set %q",
			examined, incomplete, wantExamined, expectedSweepSnapshotIDs)
	}
	if got := readDependencies(copied.Copies[0].SnapshotID()); !reflect.DeepEqual(got, wantMetadata) {
		t.Fatalf("certified copy backfill = %#v, want %#v", got, wantMetadata)
	}
	assertCompletion(copied.Copies[0].SnapshotID(), metadata.Result.Snapshot().Hash())
	if len(readDependencies(imported.SnapshotID())) != 0 {
		t.Fatal("imported canonical IDs were guessed to be local dependencies")
	}
	if err := fixture.Runtime.QueryRow(ctx, `select exists (select 1 from public.comparison_result_dependency_index_completions where result_snapshot_id = $1)`, imported.SnapshotID()).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted {
		t.Fatal("unresolved import was falsely certified complete")
	}
	if err := evidenceRepository.VerifyComparisonDependencyIndex(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) {
		t.Fatalf("unresolved import did not block readiness: %v", err)
	}
	if got, err := evidenceRepository.PreviewComparisonDeletion(ctx, target); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) ||
		!reflect.DeepEqual(got, recorddeletion.AdapterPreviewSnapshot{}) {
		t.Fatalf("unproven imported provenance disclosed a preview: (%#v, %v)", got, err)
	}
	if got, err := evidenceRepository.ComparisonDependencyIndexHealth(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) || got.VerifiedSetDigest != [32]byte{} {
		t.Fatalf("unproven imported provenance reported index health: (%#v, %v)", got, err)
	}
	if got, err := evidenceRepository.RecordComparisonHealth(ctx); !errors.Is(err, evidence.ErrComparisonDependencyIndexIncomplete) || got != (recorddeletion.AdapterHealthSnapshot{}) {
		t.Fatalf("unproven imported provenance reported adapter health: (%#v, %v)", got, err)
	}
	t.Run("trash restore preserves revision and original replay", func(t *testing.T) {
		if err := store.EnsurePublishedRecordSearchGeneration(ctx, fixture.Runtime); err != nil {
			t.Fatal(err)
		}
		readService, err := records.NewRecordReadService(current, current, repository)
		if err != nil {
			t.Fatal(err)
		}
		lifecycle, err := records.NewRecordLifecycleService(current, repository)
		if err != nil {
			t.Fatal(err)
		}
		draftRepository := store.NewPostgresRecordDraftRepository(fixture.Runtime, fixture.Gate)
		if err := draftRepository.ConfigureRecordMutationAuthorization(store.NewPostgresCollaborationMembershipReader(), current); err != nil {
			t.Fatal(err)
		}
		drafts, err := records.NewDraftService(draftRepository, current)
		if err != nil {
			t.Fatal(err)
		}
		app, err := records.NewApplication(readService, revisions, lifecycle, drafts, records.ApplicationOptions{
			IdempotencyOwnerID: "g2-lifecycle", OwnerLeaseDuration: time.Minute,
			IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		head := create("rec_g2trash")
		request := records.RecordTrashStateChangeRequest{
			Actor: actor, RecordID: head.RecordID, CurrentRevisionID: head.RevisionID,
			LockVersion: head.LockVersion, AuthorizationEpoch: head.AuthorizationEpoch,
			TargetLifecycle: records.LifecycleTrashed, IdempotencyKey: "g2-trash-first",
		}
		trashed, err := app.ChangeTrashState(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if trashed.Lifecycle != records.LifecycleTrashed || trashed.CurrentRevisionID != head.RevisionID ||
			trashed.LockVersion != head.LockVersion+1 || trashed.AuthorizationEpoch != head.AuthorizationEpoch+1 || trashed.Replayed {
			t.Fatalf("trash result: %#v", trashed)
		}
		assertRoot := func(want records.RecordLifecycleResult, wantTrashedAt *time.Time) {
			t.Helper()
			var state, revision, searchState string
			var lock, epoch, revisionCount int64
			var trashedAt *time.Time
			if err := fixture.Runtime.QueryRow(ctx, `select lifecycle, current_revision_id, lock_version, authorization_epoch, trashed_at,
				(select count(*) from public.record_revisions where record_id = $1)
				from public.records where record_id = $1`, head.RecordID).Scan(&state, &revision, &lock, &epoch, &trashedAt, &revisionCount); err != nil {
				t.Fatal(err)
			}
			if state != string(want.Lifecycle) || revision != head.RevisionID || lock != int64(want.LockVersion) ||
				epoch != int64(want.AuthorizationEpoch) || revisionCount != 1 {
				t.Fatalf("root changed revision or control facts: state=%s revision=%s lock=%d epoch=%d revisions=%d", state, revision, lock, epoch, revisionCount)
			}
			if (trashedAt == nil) != (wantTrashedAt == nil) || (trashedAt != nil && !trashedAt.Equal(*wantTrashedAt)) {
				t.Fatalf("trashed_at=%v, want %v", trashedAt, wantTrashedAt)
			}
			if err := fixture.Runtime.QueryRow(ctx, `select lifecycle from public.record_search_documents where record_id = $1 order by generation desc limit 1`, head.RecordID).Scan(&searchState); err != nil {
				t.Fatal(err)
			}
			if searchState != state {
				t.Fatalf("search lifecycle=%s, root=%s", searchState, state)
			}
		}
		assertRoot(trashed, &trashed.ChangedAt)
		readTrashed, err := app.GetRecord(ctx, records.RecordGetRequest{Actor: actor, RecordID: head.RecordID})
		if err != nil || readTrashed.Lifecycle != records.LifecycleTrashed || !readTrashed.Capabilities.Read ||
			readTrashed.Capabilities.Update || readTrashed.Capabilities.Archive || readTrashed.Capabilities.Restore || readTrashed.Capabilities.Draft {
			t.Fatalf("trash read capabilities=(%#v,%v)", readTrashed.Capabilities, err)
		}
		replay, err := app.ChangeTrashState(ctx, request)
		wantReplay := trashed
		wantReplay.Replayed = true
		if err != nil || replay != wantReplay {
			t.Fatalf("same-body trash replay=(%#v,%v), want %#v", replay, err, wantReplay)
		}
		assertRoot(trashed, &trashed.ChangedAt)
		if _, err := app.ChangeLifecycle(ctx, records.RecordLifecycleChangeRequest{
			Actor: actor, RecordID: head.RecordID, TargetLifecycle: records.LifecycleActive, IdempotencyKey: "g2-old-unarchive",
		}); !errors.Is(err, records.ErrRecordRevisionConflict) {
			t.Fatalf("old archive API restored trash: %v", err)
		}
		changedBody := request
		changedBody.TargetLifecycle = records.LifecycleActive
		if _, err := app.ChangeTrashState(ctx, changedBody); !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) {
			t.Fatalf("same key different body: %v", err)
		}
		sameState := request
		sameState.IdempotencyKey = "g2-trash-same-state"
		sameState.LockVersion, sameState.AuthorizationEpoch = trashed.LockVersion, trashed.AuthorizationEpoch
		if _, err := app.ChangeTrashState(ctx, sameState); !errors.Is(err, records.ErrRecordRevisionConflict) {
			t.Fatalf("new key same state: %v", err)
		}
		restore := sameState
		restore.TargetLifecycle, restore.IdempotencyKey = records.LifecycleActive, "g2-trash-restore"
		restored, err := app.ChangeTrashState(ctx, restore)
		if err != nil || restored.Lifecycle != records.LifecycleActive ||
			restored.LockVersion != trashed.LockVersion+1 || restored.AuthorizationEpoch != trashed.AuthorizationEpoch+1 {
			t.Fatalf("restore=(%#v,%v)", restored, err)
		}
		assertRoot(restored, nil)
		replay, err = app.ChangeTrashState(ctx, request)
		if err != nil || replay != wantReplay {
			t.Fatalf("old trash replay after restore=(%#v,%v), want %#v", replay, err, wantReplay)
		}
		assertRoot(restored, nil)
		stale := request
		stale.IdempotencyKey = "g2-trash-stale"
		if _, err := app.ChangeTrashState(ctx, stale); !errors.Is(err, records.ErrRecordRevisionConflict) {
			t.Fatalf("stale new request: %v", err)
		}
		denied := request
		denied.Actor = importDestinationActor(t, recordauth.RoleViewer)
		denied.IdempotencyKey = "g2-trash-denied"
		if _, err := app.ChangeTrashState(ctx, denied); !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("viewer trash: %v", err)
		}
		assertRoot(restored, nil)
	})
}

type comparisonFixtureIndexEntry struct {
	snapshotID   string
	ownerID      string
	payloadHash  [32]byte
	dependencies []evidence.ComparisonDependency
}

func comparisonFixtureIndexDigest(entries []comparisonFixtureIndexEntry) [32]byte {
	sort.Slice(entries, func(i, j int) bool { return entries[i].snapshotID < entries[j].snapshotID })
	fields := [][]byte{[]byte("houfeng.comparison.dependency-index.v1")}
	for _, entry := range entries {
		dependencies := append([]evidence.ComparisonDependency(nil), entry.dependencies...)
		sort.Slice(dependencies, func(i, j int) bool {
			left, right := dependencies[i], dependencies[j]
			if left.SourceRecordID != right.SourceRecordID {
				return left.SourceRecordID < right.SourceRecordID
			}
			if left.SourceRevisionID != right.SourceRevisionID {
				return left.SourceRevisionID < right.SourceRevisionID
			}
			return left.SourceSnapshotID < right.SourceSnapshotID
		})
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(len(dependencies)))
		fields = append(fields, []byte(entry.snapshotID), []byte(entry.ownerID), entry.payloadHash[:], count[:])
		for _, dependency := range dependencies {
			fields = append(fields, []byte(dependency.SourceRecordID), []byte(dependency.SourceRevisionID), []byte(dependency.SourceSnapshotID))
		}
	}
	return comparisonFixtureFramedDigestFields(fields)
}

func comparisonFixtureFramedDigest(domain string, fields ...[]byte) [32]byte {
	return comparisonFixtureFramedDigestFields(append([][]byte{[]byte(domain)}, fields...))
}

func comparisonFixtureFramedDigestFields(fields [][]byte) [32]byte {
	var framed bytes.Buffer
	for _, field := range fields {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		framed.Write(length[:])
		framed.Write(field)
	}
	return sha256.Sum256(framed.Bytes())
}

// This is the pre-change inventory query, kept as a separate oracle for the
// PostgreSQL jsonb::text bytes, including timestamp rendering and ordering.
func comparisonLegacyInventory(t *testing.T, ctx context.Context, db *pgxpool.Pool, target recorddeletion.PreviewTarget) ([]byte, int64) {
	t.Helper()
	var material []byte
	var surviving int64
	err := db.QueryRow(ctx, `
		with owned as materialized (
		  select snapshot_id, payload_digest from public.evidence_snapshots
		  where record_id = $2 and kind = $3
		), retained as materialized (
		  select distinct result.snapshot_id
		  from public.comparison_result_dependencies as dependency
		  join public.evidence_snapshots as result on result.snapshot_id = dependency.result_snapshot_id
		  join public.records as owner on owner.record_id = result.record_id
		  where dependency.project_id = $1 and owner.project_id = $1
		    and dependency.source_record_id = $2 and owner.record_id <> $2 and result.kind = $3
		)
		select pg_catalog.convert_to(jsonb_build_object(
		  'results', coalesce((select jsonb_agg(jsonb_build_array(snapshot_id, encode(payload_digest, 'hex'))
		    order by snapshot_id collate "C") from owned), '[]'::jsonb),
		  'dependencies', coalesce((select jsonb_agg(jsonb_build_array(
		    dependency.result_snapshot_id, dependency.source_record_id, dependency.source_revision_id,
		    dependency.source_snapshot_id, dependency.project_id, dependency.created_at)
		    order by dependency.result_snapshot_id collate "C", dependency.source_record_id collate "C",
		      dependency.source_revision_id collate "C", dependency.source_snapshot_id collate "C")
		    from public.comparison_result_dependencies as dependency
		    join owned on owned.snapshot_id = dependency.result_snapshot_id), '[]'::jsonb),
		  'completions', coalesce((select jsonb_agg(jsonb_build_array(
		    completion.result_snapshot_id, encode(completion.payload_digest, 'hex'), completion.project_id, completion.indexed_at)
		    order by completion.result_snapshot_id collate "C")
		    from public.comparison_result_dependency_index_completions as completion
		    join owned on owned.snapshot_id = completion.result_snapshot_id), '[]'::jsonb),
		  'workbenches', coalesce((select jsonb_agg(jsonb_build_array(user_id, browser_id,
		    encode(workbench_digest, 'hex'), project_id, destination_record_id, source_record_ids,
		    generation, registered_at, updated_at) order by user_id collate "C", browser_id collate "C", workbench_digest)
		    from public.comparison_client_workbenches where project_id = $1
		      and (destination_record_id = $2 or $2 = any(source_record_ids))), '[]'::jsonb),
		  'save_attempts', coalesce((select jsonb_agg(jsonb_build_array(user_id, browser_id,
		    encode(save_digest, 'hex'), project_id, destination_record_id, source_record_ids, idempotency_key,
		    generation, registered_at, updated_at) order by user_id collate "C", browser_id collate "C", save_digest)
		    from public.comparison_client_save_attempts where project_id = $1
		      and (destination_record_id = $2 or $2 = any(source_record_ids))), '[]'::jsonb),
		  'retained_results', coalesce((select jsonb_agg(snapshot_id order by snapshot_id collate "C") from retained), '[]'::jsonb)
		)::text, 'UTF8'), (select count(*) from retained)`,
		target.Object.ProjectID, target.Object.ObjectID, string(evidence.ComparisonResultV1Key().Kind),
	).Scan(&material, &surviving)
	if err != nil {
		t.Fatal(err)
	}
	return material, surviving
}

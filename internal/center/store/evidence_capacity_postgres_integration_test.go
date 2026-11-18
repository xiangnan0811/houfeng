package store

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const evidenceCapacityWarningReason = "project evidence quota warning threshold reached"

func TestPostgresIntegrationEvidenceCapacityExactBoundaryAndAccounting(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture, runtimePool := signed.platform, signed.Runtime
	repository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-capacity-boundary", recordbirth.PurposeEvidence)
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	capture := storePreparedEvidenceCaptureWithQuota(
		t, allocation.RecordID, "evs_pgcapacityboundary", "evi_a1a1a1a1a1a1a1a1a1a1a1a1", now,
		evidence.QuotaOutcome{Status: evidence.QuotaWarning, Reason: evidenceCapacityWarningReason},
	)
	policy := evidence.CapacityPolicy{ProjectLimitBytes: capture.Snapshot().Size(), WarningPercent: 80}
	participant, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(policy)
	if err != nil {
		t.Fatalf("NewRecordEvidenceRevisionParticipantWithCapacityPolicy() error = %v", err)
	}
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, participant)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, capture, allocation.AllocationID)
	var canonicalSize, compressedSize int64
	digest := capture.Snapshot().Hash()
	if err := runtimePool.QueryRow(ctx, `select canonical_size_bytes,compressed_size_bytes from public.evidence_payloads where payload_digest=$1`, digest[:]).Scan(&canonicalSize, &compressedSize); err != nil {
		t.Fatal(err)
	}
	payload := EvidencePayloadMetadata{CanonicalSizeBytes: uint64(canonicalSize), CompressedSizeBytes: uint64(compressedSize)}
	preparation := storeEvidenceRevisionPreparation(
		t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
	)
	command := recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
		"Exact evidence capacity", "evidence-capacity-boundary", preparation,
	)
	command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
	if _, err := recordRepository.CommitRevision(ctx, command); err != nil {
		t.Fatalf("CommitRevision(exact boundary) error = %v", err)
	}

	orphan := storeEvidenceSnapshotFixture(t, "capacity orphan")
	seedEvidencePayloadAt(t, ctx, fixture, orphan, now.Add(-EvidencePayloadOrphanGracePeriod-time.Minute))
	usage, err := repository.ReadProjectEvidenceCapacity(ctx, string(recordauth.ProjectIDDefault))
	if err != nil {
		t.Fatalf("ReadProjectEvidenceCapacity() error = %v", err)
	}
	if usage.LogicalSnapshotCount != 1 || usage.LogicalSnapshotBytes != capture.Snapshot().Size() ||
		usage.PhysicalPayloadCount != 1 || usage.PhysicalCanonicalBytes != payload.CanonicalSizeBytes ||
		usage.PhysicalCompressedBytes != payload.CompressedSizeBytes || usage.OrphanPayloadCount != 1 ||
		usage.OrphanCanonicalBytes != orphan.Size() {
		t.Fatalf("project evidence usage = %#v", usage)
	}
	aggregate, err := repository.ReadEvidenceCapacityAggregate(ctx)
	if err != nil {
		t.Fatalf("ReadEvidenceCapacityAggregate() error = %v", err)
	}
	if aggregate.ProjectCount != 1 || aggregate.LogicalSnapshotBytes != capture.Snapshot().Size() ||
		aggregate.HighestProjectLogicalBytes != capture.Snapshot().Size() ||
		aggregate.PhysicalCanonicalBytes != payload.CanonicalSizeBytes || aggregate.OrphanPayloadCount != 1 {
		t.Fatalf("aggregate evidence capacity = %#v", aggregate)
	}
	backlog, err := repository.ReadEvidenceLifecycleBacklog(ctx, 10)
	if err != nil {
		t.Fatalf("ReadEvidenceLifecycleBacklog() error = %v", err)
	}
	if !reflect.DeepEqual(backlog, evidence.EvidenceLifecycleBacklog{EligibleOrphanPayloadCount: 1}) {
		t.Fatalf("ReadEvidenceLifecycleBacklog() = %#v", backlog)
	}
}

func TestPostgresIntegrationEvidenceCapacityOverBoundaryRollsBackAndPreservesIntent(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture, runtimePool := signed.platform, signed.Runtime
	repository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	firstAllocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-capacity-first", recordbirth.PurposeEvidence)
	staleAllocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-capacity-stale", recordbirth.PurposeEvidence)
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	quota := evidence.QuotaOutcome{Status: evidence.QuotaWarning, Reason: evidenceCapacityWarningReason}
	first := storePreparedEvidenceCaptureWithQuota(
		t, firstAllocation.RecordID, "evs_pgcapacityfirst", "evi_b1b1b1b1b1b1b1b1b1b1b1b1", now, quota,
	)
	stale := storePreparedEvidenceCaptureWithQuota(
		t, staleAllocation.RecordID, "evs_pgcapacitystale", "evi_b2b2b2b2b2b2b2b2b2b2b2b2", now, quota,
	)
	policy := evidence.CapacityPolicy{
		ProjectLimitBytes: first.Snapshot().Size() + stale.Snapshot().Size() - 1,
		WarningPercent:    1,
	}
	participant, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(policy)
	if err != nil {
		t.Fatalf("NewRecordEvidenceRevisionParticipantWithCapacityPolicy() error = %v", err)
	}
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, participant)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, first, firstAllocation.AllocationID)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, stale, staleAllocation.AllocationID)
	firstPreparation := storeEvidenceRevisionPreparation(
		t, first.RecordID(), []evidence.PreparedCapture{first}, nil, []string{first.SnapshotID()},
	)
	firstCommand := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, first.RecordID(), "", 0, 0,
		"First quota use", "evidence-capacity-first", firstPreparation)
	firstCommand = prepareRevisionBirthCommand(t, ctx, signed, firstAllocation, firstCommand)
	if _, err := recordRepository.CommitRevision(ctx, firstCommand); err != nil {
		t.Fatalf("CommitRevision(first) error = %v", err)
	}
	stalePreparation := storeEvidenceRevisionPreparation(
		t, stale.RecordID(), []evidence.PreparedCapture{stale}, nil, []string{stale.SnapshotID()},
	)
	staleCommand := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, stale.RecordID(), "", 0, 0,
		"Stale quota use", "evidence-capacity-stale", stalePreparation)
	staleCommand = prepareRevisionBirthCommand(t, ctx, signed, staleAllocation, staleCommand)
	_, err = recordRepository.CommitRevision(ctx, staleCommand)
	if !errors.Is(err, evidence.ErrPreviewStale) || !errors.Is(err, ErrEvidencePersistenceConflict) {
		t.Fatalf("CommitRevision(over boundary) error = %v, want stale capacity conflict", err)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, stale.RecordID(), 0, 0, 0, 1)
}

func TestPostgresIntegrationEvidenceCapacityCumulativelyAccountsMultiCaptureRevision(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture, runtimePool := signed.platform, signed.Runtime
	repository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-capacity-multi-capture", recordbirth.PurposeEvidence)
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	quota := evidence.QuotaOutcome{Status: evidence.QuotaWarning, Reason: evidenceCapacityWarningReason}
	captures := []evidence.PreparedCapture{
		storePreparedEvidenceCaptureWithQuota(
			t, allocation.RecordID, "evs_pgcapacitymultia", "evi_d1d1d1d1d1d1d1d1d1d1d1d1", now, quota,
		),
		storePreparedEvidenceCaptureWithQuota(
			t, allocation.RecordID, "evs_pgcapacitymultib", "evi_d2d2d2d2d2d2d2d2d2d2d2d2", now, quota,
		),
	}
	limit := captures[0].Snapshot().Size() + captures[1].Snapshot().Size()
	participant, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(evidence.CapacityPolicy{
		ProjectLimitBytes: limit,
		WarningPercent:    1,
	})
	if err != nil {
		t.Fatalf("NewRecordEvidenceRevisionParticipantWithCapacityPolicy() error = %v", err)
	}
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, participant)
	orderedSnapshotIDs := make([]string, 0, len(captures))
	for _, capture := range captures {
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, capture, allocation.AllocationID)
		orderedSnapshotIDs = append(orderedSnapshotIDs, capture.SnapshotID())
	}
	preparation := storeEvidenceRevisionPreparation(
		t, captures[0].RecordID(), captures, nil, orderedSnapshotIDs,
	)
	command := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, captures[0].RecordID(), "", 0, 0,
		"Cumulative evidence capacity", "evidence-capacity-multi-capture", preparation)
	command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
	if _, err := recordRepository.CommitRevision(ctx, command); err != nil {
		t.Fatalf("CommitRevision(multi capture) error = %v", err)
	}
	usage, err := repository.ReadProjectEvidenceCapacity(ctx, string(recordauth.ProjectIDDefault))
	if err != nil {
		t.Fatalf("ReadProjectEvidenceCapacity() error = %v", err)
	}
	if usage.LogicalSnapshotCount != 2 || usage.LogicalSnapshotBytes != limit {
		t.Fatalf("multi-capture usage = %#v, want 2 snapshots/%d bytes", usage, limit)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, captures[0].RecordID(), 1, 2, 2, 0)
}

func TestPostgresIntegrationEvidenceCapacityConcurrentCapturesCannotOversubscribeProject(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture, runtimePool := signed.platform, signed.Runtime
	repository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	allocations := []recordbirth.Allocation{
		reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-capacity-race-a", recordbirth.PurposeEvidence),
		reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-capacity-race-b", recordbirth.PurposeEvidence),
	}
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	quota := evidence.QuotaOutcome{Status: evidence.QuotaWarning, Reason: evidenceCapacityWarningReason}
	captures := []evidence.PreparedCapture{
		storePreparedEvidenceCaptureWithQuota(
			t, allocations[0].RecordID, "evs_pgcapacityracea", "evi_c1c1c1c1c1c1c1c1c1c1c1c1", now, quota,
		),
		storePreparedEvidenceCaptureWithQuota(
			t, allocations[1].RecordID, "evs_pgcapacityraceb", "evi_c2c2c2c2c2c2c2c2c2c2c2c2", now, quota,
		),
	}
	limit := max(captures[0].Snapshot().Size(), captures[1].Snapshot().Size())
	policy := evidence.CapacityPolicy{ProjectLimitBytes: limit, WarningPercent: 1}
	participant, err := NewRecordEvidenceRevisionParticipantWithCapacityPolicy(policy)
	if err != nil {
		t.Fatalf("NewRecordEvidenceRevisionParticipantWithCapacityPolicy() error = %v", err)
	}
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, participant)
	commands := make([]records.RevisionCommitCommand, 0, len(captures))
	for index, capture := range captures {
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, capture, allocations[index].AllocationID)
		preparation := storeEvidenceRevisionPreparation(
			t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
		)
		command := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
			"Concurrent quota", "evidence-capacity-race-"+string(rune('a'+index)), preparation)
		commands = append(commands, prepareRevisionBirthCommand(t, ctx, signed, allocations[index], command))
	}
	type result struct{ err error }
	start := make(chan struct{})
	results := make(chan result, len(commands))
	var ready sync.WaitGroup
	ready.Add(len(commands))
	for _, item := range commands {
		command := item
		go func() {
			ready.Done()
			<-start
			_, err := recordRepository.CommitRevision(ctx, command)
			results <- result{err: err}
		}()
	}
	ready.Wait()
	close(start)
	var succeeded, rejected int
	for range commands {
		outcome := <-results
		switch {
		case outcome.err == nil:
			succeeded++
		case errors.Is(outcome.err, evidence.ErrPreviewStale) && errors.Is(outcome.err, ErrEvidencePersistenceConflict):
			rejected++
		default:
			t.Fatalf("concurrent CommitRevision() error = %v", outcome.err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent quota results = succeeded:%d rejected:%d, want 1/1", succeeded, rejected)
	}
	usage, err := repository.ReadProjectEvidenceCapacity(ctx, string(recordauth.ProjectIDDefault))
	if err != nil {
		t.Fatalf("ReadProjectEvidenceCapacity() error = %v", err)
	}
	if usage.LogicalSnapshotCount != 1 || usage.LogicalSnapshotBytes > limit {
		t.Fatalf("concurrent quota usage = %#v, limit %d", usage, limit)
	}
	var intentCount int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.evidence_capture_intents`).Scan(&intentCount); err != nil {
		t.Fatalf("count remaining evidence intents: %v", err)
	}
	if intentCount != 1 {
		t.Fatalf("remaining evidence intents = %d, want losing intent preserved", intentCount)
	}
}

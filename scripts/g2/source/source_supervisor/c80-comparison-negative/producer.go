package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type result struct {
	Version            uint64 `json:"version"`
	Status             string `json:"status"`
	RecordID           string `json:"record_id"`
	BaseRevisionID     string `json:"base_revision_id"`
	ComparedRevisionID string `json:"compared_revision_id"`
	RevisionID         string `json:"revision_id"`
	SnapshotID         string `json:"snapshot_id"`
	ReadbackVerified   bool   `json:"readback_verified"`
}

func memberActor(ctx context.Context, pool *pgxpool.Pool, gate store.AdmissionGate, userID string) (recordauth.ActorScope, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return recordauth.ActorScope{}, stageError("membership_unavailable")
	}
	defer tx.Rollback(ctx)
	if err := gate.Admit(ctx, tx); err != nil {
		return recordauth.ActorScope{}, stageError("membership_unavailable")
	}
	actor, err := store.NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, userID)
	if err != nil || actor.UserID != userID || actor.ProjectID != recordauth.ProjectIDDefault ||
		actor.Role != recordauth.RoleProjectAdmin {
		return recordauth.ActorScope{}, stageError("membership_unavailable")
	}
	if err := tx.Commit(ctx); err != nil {
		return recordauth.ActorScope{}, stageError("membership_unavailable")
	}
	return actor, nil
}

func produce(ctx context.Context, pool *pgxpool.Pool, input privateInput, signer *evidence.FileComparisonIntentSigner) (result, error) {
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: recordplatform.DeploymentID(input.DeploymentID),
		ProjectID: recordplatform.ProjectIDDefault, InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		return result{}, stageError("membership_unavailable")
	}
	actor, err := memberActor(ctx, pool, gate, input.ActorUserID)
	if err != nil {
		return result{}, err
	}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(store.NewPostgresVPSAssetRepository(pool)),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(pool)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(pool)),
	})
	if err != nil {
		return result{}, stageError("record_authority_unavailable")
	}
	resolver := store.NewRecordSubjectReadResolver(subjects, nil)
	current := store.NewPostgresCurrentRecordAuthorizationSource(pool, resolver, gate)
	recordRepository, err := store.NewPostgresRecordRepository(pool, gate, []records.RevisionParticipant{
		store.NewComparisonRevisionParticipant(signer),
		store.NewRecordEvidenceRevisionParticipant(),
	})
	if err != nil {
		return result{}, stageError("record_authority_unavailable")
	}
	reader, err := records.NewRecordReadService(current, current, recordRepository)
	if err != nil {
		return result{}, stageError("record_authority_unavailable")
	}
	writer, err := records.NewRevisionService(subjects, current, recordRepository)
	if err != nil {
		return result{}, stageError("record_authority_unavailable")
	}
	original, err := reader.GetRecord(ctx, records.RecordGetRequest{Actor: actor, RecordID: input.RecordID})
	if err != nil || original.RecordID != input.RecordID || original.ProjectID != actor.ProjectID ||
		original.Lifecycle != records.LifecycleActive || !original.Capabilities.Read || !original.Capabilities.Update ||
		original.Current.RevisionID != original.CurrentRevisionID || original.Current.RevisionNo != 1 ||
		original.Current.Input.AuthorID() != actor.UserID || len(original.Current.Input.Subjects()) != 1 ||
		len(original.Current.Input.AttachmentIDs()) != 0 || len(original.Current.Input.EvidenceSnapshotIDs()) != 0 {
		return result{}, stageError("record_authority_unavailable")
	}
	values, references := revisionValues(original.Current.Input)
	values.BodyMarkdown += "\nComparison negative source revision.\n"
	firstKey, err := ids.New("g2cmp")
	if err != nil {
		return result{}, stageError("revision_unavailable")
	}
	second, err := writer.SaveRevision(ctx, revisionRequest(actor, original, values, references, evidence.RevisionPreparation{}, firstKey))
	if err != nil || second.RecordID != original.RecordID || second.RevisionID == original.CurrentRevisionID ||
		!records.ValidRevisionID(second.RevisionID) || second.RevisionNo != 2 || !second.Created || second.Replayed ||
		second.Lifecycle != records.LifecycleActive {
		return result{}, stageError("revision_unavailable")
	}

	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		return result{}, stageError("comparison_unavailable")
	}
	registry, err := evidence.NewRegistry([]evidence.Kind{kind})
	if err != nil {
		return result{}, stageError("comparison_unavailable")
	}
	evidenceRepository, err := store.NewPostgresEvidenceRepositoryWithReadSources(pool, gate, registry, current, resolver)
	if err != nil {
		return result{}, stageError("comparison_unavailable")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	comparison, err := evidence.ResolveFixedComparison(ctx, registry, evidenceRepository, signer, evidence.ComparisonEvaluateRequest{
		Actor: actor,
		Items: []evidence.ComparisonFixedItem{
			{Revision: &evidence.ComparisonFixedRevision{RecordID: input.RecordID, RevisionID: original.CurrentRevisionID}},
			{Revision: &evidence.ComparisonFixedRevision{RecordID: input.RecordID, RevisionID: second.RevisionID}},
		},
		BaselineIndex: 0, Alignment: evidence.CoverageActual,
		RequestedWindow: evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}, Now: now,
	})
	if err != nil || comparison.Intent == nil || !comparison.SaveEligibility.Eligible || len(comparison.Items) != 2 ||
		comparison.Items[0].RecordID != input.RecordID || comparison.Items[0].RevisionID != original.CurrentRevisionID ||
		comparison.Items[1].RecordID != input.RecordID || comparison.Items[1].RevisionID != second.RevisionID ||
		comparison.Items[0].SnapshotID != "" || comparison.Items[1].SnapshotID != "" {
		return result{}, stageError("comparison_unavailable")
	}
	save, err := evidence.PrepareComparisonSave(ctx, registry, evidenceRepository, signer, evidenceRepository,
		evidence.ComparisonSaveRequest{Actor: actor, RecordID: input.RecordID, Token: comparison.Intent.Token})
	if err != nil || len(save.Copies) != 0 || save.Result.Empty() || save.Result.RecordID() != input.RecordID ||
		!evidence.ValidSnapshotID(save.Result.SnapshotID()) || save.Result.Snapshot().Envelope().Key != evidence.ComparisonResultV1Key() {
		return result{}, stageError("comparison_unavailable")
	}
	preparation, err := evidence.NewRevisionPreparationFromComparisonSave(input.RecordID, save)
	if err != nil || len(preparation.SnapshotIDs()) != 1 || preparation.SnapshotIDs()[0] != save.Result.SnapshotID() {
		return result{}, stageError("comparison_unavailable")
	}
	updated, err := reader.GetRecord(ctx, records.RecordGetRequest{Actor: actor, RecordID: input.RecordID})
	if err != nil || updated.CurrentRevisionID != second.RevisionID || updated.LockVersion != second.LockVersion ||
		updated.AuthorizationEpoch != second.AuthorizationEpoch || !updated.Capabilities.Update {
		return result{}, stageError("record_authority_unavailable")
	}
	values, references = revisionValues(updated.Current.Input)
	secondKey, err := ids.New("g2cmp")
	if err != nil {
		return result{}, stageError("revision_unavailable")
	}
	committed, err := writer.SaveRevision(ctx, revisionRequest(actor, updated, values, references, preparation, secondKey))
	if err != nil || committed.RecordID != input.RecordID || committed.RevisionID == second.RevisionID ||
		!records.ValidRevisionID(committed.RevisionID) || committed.RevisionNo != 3 || !committed.Created || committed.Replayed ||
		committed.Lifecycle != records.LifecycleActive {
		return result{}, stageError("revision_unavailable")
	}
	out := result{
		Version: 1, Status: "produced", RecordID: input.RecordID,
		BaseRevisionID: original.CurrentRevisionID, ComparedRevisionID: second.RevisionID,
		RevisionID: committed.RevisionID, SnapshotID: save.Result.SnapshotID(),
	}
	if err := verifyProduced(ctx, pool, gate, out, save.Result.Snapshot(), original.Current.Input.Subjects()[0]); err != nil {
		return result{}, stageError("readback_unavailable")
	}
	out.ReadbackVerified = true
	return out, nil
}

func revisionValues(input records.CompleteRevisionInput) (records.CompleteRevisionValues, []records.SubjectReference) {
	values := records.CompleteRevisionValues{
		Title: input.Title(), BodyMarkdown: input.BodyMarkdown(), MarkdownDialectVersion: input.MarkdownDialectVersion(),
		RecordType: input.RecordType(), BusinessStatus: input.BusinessStatus(), ImpactLevel: input.ImpactLevel(),
		OccurredAt: input.OccurredAt(), CompletedAt: input.CompletedAt(), VisibilityScope: input.VisibilityScope(),
		Tags: input.Tags(), OwnerID: input.OwnerID(), Participants: input.Participants(), AttachmentIDs: input.AttachmentIDs(),
		FollowUpAt: input.FollowUpAt(), Template: input.Template(), SaveReason: "comparison negative fixture",
	}
	subjects := input.Subjects()
	references := make([]records.SubjectReference, 0, len(subjects))
	for _, subject := range subjects {
		references = append(references, records.SubjectReference{
			RegistryVersion: subject.RegistryVersion, Kind: subject.Kind, Role: subject.Role,
			SourceID: subject.SourceID, Primary: subject.Primary,
		})
	}
	return values, references
}

func revisionRequest(actor recordauth.ActorScope, current records.Record, values records.CompleteRevisionValues,
	references []records.SubjectReference, preparation evidence.RevisionPreparation, key string) records.RevisionSaveRequest {
	return records.RevisionSaveRequest{
		Actor: actor, RecordID: current.RecordID, BaseRevisionID: current.CurrentRevisionID,
		LockVersion: current.LockVersion, AuthorizationEpoch: current.AuthorizationEpoch,
		Values: values, SubjectReferences: references, EvidencePreparation: preparation,
		ActivityKind: records.DomainActivityRecordRevised, IdempotencyKey: key,
		IdempotencyOwnerID: "g2_c80_comparison_negative", OwnerLeaseDuration: 30 * time.Second,
		IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
	}
}

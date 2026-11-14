package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"sort"

	"houfeng/internal/center/records"
)

// recordMutationResources is deliberately not MutationLocks: it holds no
// authority, receipt, admission, or source-terminal pin. Declaration dependency
// order includes non-acquirable scopes and is not an executed lock protocol.
type recordMutationResources struct {
	resources       records.RevisionMutationResources
	order           []recordMutationDeclaration
	continuityProof []byte
}

type recordMutationFamily uint8

const (
	recordMutationUsers recordMutationFamily = iota
	recordMutationGroups
	recordMutationMemberships
	recordMutationSources
	recordMutationReservations
	recordMutationEpochScopes
	recordMutationLeaseScopes
	recordMutationContentLeaseScopes
	recordMutationRecordGateScopes
	recordMutationBirthContinuity
	recordMutationIdempotency
	recordMutationImportJobs
	recordMutationExportJobs
	recordMutationSearchRebuildJobs
	recordMutationRoots
	recordMutationProjects
	recordMutationGenerations
	recordMutationDrafts
	recordMutationAttachments
	recordMutationUploads
	recordMutationWorkspaces
	recordMutationJobs
	recordMutationBlobTables
	recordMutationBlobs
	recordMutationParts
	recordMutationWriteObligations
	recordMutationPublications
	recordMutationPayloads
	recordMutationCaptureIntents
	recordMutationSnapshots
	recordMutationLineages
	recordMutationDependencies
	recordMutationCompletions
	recordMutationRevisions
	recordMutationRevisionParticipants
	recordMutationFollowers
	recordMutationActions
	recordMutationDocuments
	recordMutationSearchSubjects
	recordMutationNewRevisionEffects
	recordMutationImports
	recordMutationBirths
	recordMutationAllocations
)

type recordMutationDeclaration struct {
	rank, subrank uint8
	family        recordMutationFamily
	index         int
}

func normalizeRecordMutationDeclarations(sets ...records.RevisionMutationResources) (recordMutationResources, error) {
	normalized, err := records.CombineRevisionMutationResources(sets...)
	if err != nil {
		return recordMutationResources{}, err
	}
	// A reservation or gate without its epoch/root route must never bypass
	// the absence-serialization point.
	for _, reservation := range normalized.Reservations {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: reservation.ProjectID, RecordID: reservation.RecordID})
	}
	// Birth rows are protected from direct runtime SELECT. Their immutable
	// routes are acquired by the restricted consume command in the batch's
	// rank-60 phase, after all epoch, lease and gate scopes have been held.
	for _, birth := range normalized.Births {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: birth.Request.ProjectID, RecordID: birth.Creation.RecordID})
	}
	for _, allocation := range normalized.Allocations {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: allocation.Request.ProjectID, RecordID: allocation.RecordID})
	}
	for _, gate := range normalized.RecordGateScopes {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: gate.ProjectID, RecordID: gate.RecordID})
	}
	for _, root := range normalized.RootObservations {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: root.ProjectID, RecordID: root.RecordID})
	}
	for _, revision := range normalized.Revisions {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: revision.RecordID})
	}
	for _, draft := range normalized.Drafts {
		if draft.RecordID != "" {
			normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: draft.ProjectID, RecordID: draft.RecordID})
		}
	}
	for _, attachment := range normalized.Attachments {
		if attachment.RecordID != "" {
			normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: attachment.ProjectID, RecordID: attachment.RecordID})
		}
	}
	for _, obligation := range normalized.WriteObligations {
		if obligation.RecordID != "" {
			normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: obligation.ProjectID, RecordID: obligation.RecordID})
		}
	}
	for _, snapshot := range normalized.Snapshots {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: snapshot.RecordID})
	}
	for _, dependency := range normalized.Dependencies {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: dependency.SourceRecordID})
	}
	for _, job := range normalized.ExportJobs {
		if job.RecordID != "" {
			normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: job.ProjectID, RecordID: job.RecordID})
		}
	}
	for _, follower := range normalized.Followers {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: follower.ProjectID, RecordID: follower.RecordID})
	}
	for _, action := range normalized.Actions {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: action.ProjectID, RecordID: action.RecordID})
	}
	for _, document := range normalized.Documents {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: document.ProjectID, RecordID: document.RecordID})
	}
	for _, subject := range normalized.SearchSubjects {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: subject.ProjectID, RecordID: subject.RecordID})
	}
	for _, effect := range normalized.NewRevisionEffects {
		normalized.Records = append(normalized.Records, records.RevisionMutationRecord{ProjectID: effect.ProjectID, RecordID: effect.RecordID})
	}
	normalized, err = records.CombineRevisionMutationResources(normalized)
	if err != nil {
		return recordMutationResources{}, err
	}
	// Every record route needs a gate check, including a missing root. Blob
	// table acquisition also requires the quota row before the table fence.
	var required records.RevisionMutationResources
	for _, record := range normalized.Records {
		required.RecordGateScopes = append(required.RecordGateScopes, records.RevisionMutationRecordGateScope{ProjectID: record.ProjectID, RecordID: record.RecordID})
	}
	if len(normalized.Blobs) != 0 || len(normalized.Parts) != 0 || len(normalized.Imports) != 0 || len(normalized.Publications) != 0 || len(normalized.WriteObligations) != 0 {
		required.BlobTables = append(required.BlobTables, records.RevisionMutationBlobMetadataTable, records.RevisionMutationUploadPartsTable)
	}
	if len(normalized.BlobTables) != 0 || len(required.BlobTables) != 0 {
		required.Projects = append(required.Projects, records.RevisionMutationProject{ProjectID: "default", Kind: records.RevisionMutationAttachmentQuota})
	}
	normalized, err = records.CombineRevisionMutationResources(normalized, required)
	if err != nil {
		return recordMutationResources{}, err
	}
	out := recordMutationResources{resources: normalized}
	add := func(rank, subrank uint8, family recordMutationFamily, count int) {
		for index := range count {
			out.order = append(out.order, recordMutationDeclaration{rank, subrank, family, index})
		}
	}
	add(10, 0, recordMutationUsers, len(normalized.Users))
	add(10, 1, recordMutationGroups, len(normalized.Groups))
	add(10, 2, recordMutationMemberships, len(normalized.Memberships))
	for index, source := range normalized.Sources {
		var subrank uint8
		switch source.SourceKind {
		case records.SubjectKindVPS:
			subrank = 3
		case records.SubjectKindMonitoringInstance:
			subrank = 4
		case records.SubjectKindTarget:
			subrank = 5
		}
		out.order = append(out.order, recordMutationDeclaration{10, subrank, recordMutationSources, index})
	}
	add(20, 0, recordMutationReservations, len(normalized.Reservations))
	// These are exact object coordination scopes even when their rows have not
	// been initialized. Absence is not an acquired row lock.
	add(30, 0, recordMutationEpochScopes, len(normalized.Records))
	add(40, 0, recordMutationLeaseScopes, len(normalized.Records))
	add(40, 1, recordMutationContentLeaseScopes, len(normalized.Records))
	// A gate route is absence-unproven and has no invented operation/owner.
	add(50, 0, recordMutationRecordGateScopes, len(normalized.RecordGateScopes))
	if len(normalized.Births) != 0 || len(normalized.Allocations) != 0 {
		add(55, 0, recordMutationBirthContinuity, 1)
	}
	add(60, 0, recordMutationRoots, len(normalized.Records))
	add(60, 1, recordMutationAllocations, len(normalized.Allocations))
	// Birth rows are acquired by the revision batch's fixed restricted command.
	// Generic acquisition must reject this family rather than silently skip it.
	add(60, 2, recordMutationBirths, len(normalized.Births))
	for index, project := range normalized.Projects {
		var subrank uint8
		switch project.Kind {
		case records.RevisionMutationEvidenceCapacity:
			subrank = 0
		case records.RevisionMutationAttachmentQuota:
			subrank = 1
		case records.RevisionMutationSearchCoordination:
			subrank = 2
		}
		out.order = append(out.order, recordMutationDeclaration{70, subrank, recordMutationProjects, index})
	}
	add(70, 3, recordMutationGenerations, len(normalized.Generations))
	add(71, 0, recordMutationIdempotency, len(normalized.Idempotency))
	add(71, 1, recordMutationImportJobs, len(normalized.ImportJobs))
	add(71, 2, recordMutationExportJobs, len(normalized.ExportJobs))
	add(71, 3, recordMutationSearchRebuildJobs, len(normalized.SearchRebuildJobs))
	add(72, 0, recordMutationDrafts, len(normalized.Drafts))
	add(72, 1, recordMutationAttachments, len(normalized.Attachments))
	add(72, 2, recordMutationUploads, len(normalized.Uploads))
	add(72, 3, recordMutationWorkspaces, len(normalized.Workspaces))
	add(72, 4, recordMutationJobs, len(normalized.Jobs))
	for index, table := range normalized.BlobTables {
		var subrank uint8
		if table == records.RevisionMutationUploadPartsTable {
			subrank = 1
		}
		out.order = append(out.order, recordMutationDeclaration{73, subrank, recordMutationBlobTables, index})
	}
	add(74, 0, recordMutationBlobs, len(normalized.Blobs))
	add(74, 0, recordMutationImports, len(normalized.Imports))
	add(74, 1, recordMutationParts, len(normalized.Parts))
	add(74, 2, recordMutationWriteObligations, len(normalized.WriteObligations))
	add(74, 3, recordMutationPublications, len(normalized.Publications))
	add(74, 3, recordMutationCaptureIntents, len(normalized.CaptureIntents))
	add(74, 4, recordMutationPayloads, len(normalized.Payloads))
	add(74, 5, recordMutationSnapshots, len(normalized.Snapshots))
	add(74, 6, recordMutationLineages, len(normalized.Lineages))
	add(74, 7, recordMutationDependencies, len(normalized.Dependencies))
	add(74, 8, recordMutationCompletions, len(normalized.Completions))
	// Existing revision parents precede their reference children.
	add(74, 4, recordMutationRevisions, len(normalized.Revisions))
	add(74, 10, recordMutationRevisionParticipants, len(normalized.RevisionParticipants))
	add(75, 0, recordMutationFollowers, len(normalized.Followers))
	add(75, 1, recordMutationActions, len(normalized.Actions))
	add(75, 2, recordMutationDocuments, len(normalized.Documents))
	add(75, 3, recordMutationSearchSubjects, len(normalized.SearchSubjects))
	// A scope is never upgraded into a fabricated existing row identity here.
	for index, effect := range normalized.NewRevisionEffects {
		rank, subrank := uint8(74), uint8(11)
		if effect.Kind == records.RevisionMutationNewRevisionRows {
			// The exact future revision-parent identity precedes child
			// effects; it is not an acquired existing row lock.
			subrank = 4
		}
		if effect.Kind == records.RevisionMutationNewCollaborationActivity {
			rank, subrank = 75, 4
		}
		if effect.Kind == records.RevisionMutationNewCollaborationOutbox {
			rank, subrank = 75, 5
		}
		out.order = append(out.order, recordMutationDeclaration{rank, subrank, recordMutationNewRevisionEffects, index})
	}
	sort.SliceStable(out.order, func(i, j int) bool {
		left, right := out.order[i], out.order[j]
		if left.rank != right.rank {
			return left.rank < right.rank
		}
		if left.subrank != right.subrank {
			return left.subrank < right.subrank
		}
		if left.family != right.family {
			return left.family < right.family
		}
		return left.index < right.index
	})
	return out, nil
}

// discoverRevisionMutationResources is an internal entrypoint for the upcoming
// coordinator. It performs only unlocked SELECTs on the exact supplied tx. No
// write path calls it merely to discard declarations, and no repository API
// implies that discovery acquired authority.
func discoverRevisionMutationResources(ctx context.Context, tx pgx.Tx, registry records.RevisionParticipantRegistry, command records.RevisionCommitCommand) (recordMutationResources, error) {
	if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
		return recordMutationResources{}, err
	}
	participantResources, err := registry.MutationResources(ctx, tx, command)
	if err != nil {
		return recordMutationResources{}, err
	}
	base, err := discoverRevisionMutationBase(ctx, tx, command, participantResources)
	if err != nil {
		return recordMutationResources{}, err
	}
	return normalizeRecordMutationDeclarations(base, participantResources)
}

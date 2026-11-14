package records

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
)

var ErrInvalidRevisionMutationResources = errors.New("invalid revision mutation resources")

// RevisionMutationResources is unlocked discovery, not an acquired lock set or
// authorization evidence. Mutable observations must be reread by the future
// coordinator after acquisition. Missing source rows do not establish terminal
// authority. No resource accepts SQL, a table name, a lock mode, or a rank.
type RevisionMutationResources struct {
	Users                []RevisionMutationUser
	Groups               []RevisionMutationGroup
	Memberships          []RevisionMutationMembership
	Sources              []RevisionMutationSource
	Records              []RevisionMutationRecord
	RootObservations     []RevisionMutationRootObservation
	Reservations         []RevisionMutationReservation
	Births               []RevisionMutationBirth
	Allocations          []RevisionMutationAllocation
	RecordGateScopes     []RevisionMutationRecordGateScope
	Idempotency          []RevisionMutationIdempotency
	ImportJobs           []RevisionMutationImportJob
	ExportJobs           []RevisionMutationExportJob
	SearchRebuildJobs    []RevisionMutationSearchRebuildJob
	Projects             []RevisionMutationProject
	Drafts               []RevisionMutationDraft
	Attachments          []RevisionMutationAttachment
	Imports              []attachments.ImportedAvailableAttachment
	Uploads              []RevisionMutationUpload
	Workspaces           []RevisionMutationWorkspace
	Jobs                 []RevisionMutationJob
	BlobTables           []RevisionMutationBlobTable
	Blobs                []attachments.BlobObject
	Parts                []RevisionMutationPart
	WriteObligations     []RevisionMutationWriteObligation
	Publications         []RevisionMutationPublication
	Payloads             []RevisionMutationPayload
	CaptureIntents       []RevisionMutationCaptureIntent
	Snapshots            []RevisionMutationSnapshot
	Lineages             []RevisionMutationLineage
	Dependencies         []RevisionMutationDependency
	Completions          []RevisionMutationCompletion
	Revisions            []RevisionMutationRevision
	RevisionParticipants []RevisionMutationRevisionParticipant
	Followers            []RevisionMutationFollower
	Actions              []RevisionMutationAction
	Generations          []RevisionMutationGeneration
	Documents            []RevisionMutationDocument
	SearchSubjects       []RevisionMutationSearchSubject
	NewRevisionEffects   []RevisionMutationNewRevisionEffect
}

type RevisionMutationUser struct {
	UserID, Role string
	Disabled     bool
}
type RevisionMutationGroup struct{ ProjectID, GroupID string }
type RevisionMutationMembership struct{ GroupID, UserID string }
type RevisionMutationSource struct {
	ProjectID  string
	SourceKind SubjectKind
	SourceID   string
}
type RevisionMutationRecord struct{ ProjectID, RecordID string }
type RevisionMutationRootObservation struct {
	ProjectID, RecordID, CurrentRevisionID, Lifecycle string
	LockVersion, AuthorizationEpoch                   int64
	Exists                                            bool
}
type RevisionMutationReservation struct {
	ProjectID, RecordID, ReservationID, State string
	FenceEpoch                                int64
	OwnerID                                   string
	OwnerGeneration                           int64
	OwnerExpiresAt                            time.Time
}

// RevisionMutationBirth is the exact immutable request and first-revision
// route. Its protected APP row is acquired by the restricted birth command,
// not by discovery or by direct runtime table access.
type RevisionMutationBirth struct {
	AllocationID string
	Request      recordbirth.Request
	Creation     recordbirth.InitialCreation
}

// RevisionMutationAllocation protects a pre-root reservation without consuming
// it or creating a record. Its request is the complete immutable binding.
type RevisionMutationAllocation struct {
	AllocationID string
	Request      recordbirth.Request
	RecordID     string
}

// RevisionMutationRecordGateScope names the exact unique record route to check.
// It is absence-unproven: neither gate absence nor a gate/owner operation has
// been observed or acquired by declaring it.
type RevisionMutationRecordGateScope struct{ ProjectID, RecordID string }

// RevisionMutationIdempotency carries the existing command's compound key, not
// an observed row, owner lease, replay result, or acquired claim.
type RevisionMutationIdempotency struct{ Key recordplatform.IdempotencyKey }

type RevisionMutationProjectKind string

const (
	RevisionMutationEvidenceCapacity   RevisionMutationProjectKind = "evidence_capacity"
	RevisionMutationAttachmentQuota    RevisionMutationProjectKind = "attachment_quota"
	RevisionMutationSearchCoordination RevisionMutationProjectKind = "search_coordination"
)

type RevisionMutationProject struct {
	ProjectID string
	Kind      RevisionMutationProjectKind
}

// Job observations identify persisted routes and versions. They are not
// claims; callers must compare them after the corresponding row lock.
type RevisionMutationImportJob struct {
	ProjectID, JobID, ActorID, State, OwnerID string
	LockVersion, OwnerGeneration              int64
	LeaseExpiresAt                            time.Time
	ArchiveDigest, PlanDigest                 [32]byte
}

type RevisionMutationExportJob struct {
	ProjectID, JobID, ActorID, State, OwnerID string
	RecordID, RevisionID                      string
	LockVersion, OwnerGeneration              int64
	AuthorizationEpoch                        int64
	LeaseExpiresAt                            time.Time
	RequestFingerprint, InventoryDigest       [32]byte
}

type RevisionMutationSearchRebuildJob struct {
	ProjectID, JobID, State, OwnerID, ResumeAfterRecordID string
	Generation, ProcessedCount                            int64
	LeaseExpiresAt                                        time.Time
}

// RevisionMutationDraft includes the checkpoint deletion effect scoped to this
// exact draft. It does not invent existing checkpoint row identities.
type RevisionMutationDraft struct {
	ProjectID, DraftID, RecordID, AuthorID string
	ETag                                   [32]byte
	BaseRevisionID                         string
}
type RevisionMutationAttachment struct {
	ProjectID, AttachmentID, RecordID, DraftID, State string
	LogicalSizeBytes                                  int64
}
type RevisionMutationUpload struct {
	ProjectID, UploadID, AttachmentID, OriginDraftID, State string
	Transport                                               attachments.TransportKind
	ReservedSizeBytes                                       int64
	TemporaryObjectKey                                      string
	TemporaryObjectDeletedAt                                time.Time
}
type RevisionMutationWorkspace struct{ WorkspaceID, ProcessorJobID, State string }
type RevisionMutationJob struct {
	ProcessorJobID, AttachmentID, State string
	LeaseExpiresAt                      time.Time
	UploadID, OwnerID                   string
	OwnerGeneration                     int64
}
type RevisionMutationBlobTable string

const (
	RevisionMutationBlobMetadataTable RevisionMutationBlobTable = "blob_metadata"
	RevisionMutationUploadPartsTable  RevisionMutationBlobTable = "upload_parts"
)

type RevisionMutationPart struct {
	UploadID   string
	PartNumber int64
	Object     attachments.BlobObject
}

// RevisionMutationWriteObligation is a discovered durable write route and
// outcome, not authority to dispatch, publish, clean up, or settle it.
type RevisionMutationWriteObligation struct {
	ProjectID, ObligationID, OriginKind, OriginID, OriginOwnerID string
	WriterInstance, WriterPrincipal, OperationID, TemporaryKey   string
	RequestSeq, TemporaryDevice, TemporaryInode                  int64
	TemporaryCreatedAt                                           time.Time
	OriginGeneration, OriginOwnerGeneration                      int64
	OriginOwnerExpiresAt                                         time.Time
	RecordID                                                     string
	RecordEpoch                                                  int64
	NamespaceID, IncarnationID                                   string
	BackendKind                                                  attachments.BackendKind
	CanonicalKey                                                 string
	SHA256, PlanDigest, ArchiveDigest                            [32]byte
	SizeBytes                                                    int64
	PublicationID, ObjectVersion, Outcome, DispatchID            string
	DispatchedAt, ReturnedAt, HandedOffAt, PublishedAt           time.Time
	CleanupOwnerID                                               string
	CleanupGeneration                                            int64
	CleanupExpiresAt                                             time.Time
	CleanupDispatchID, CleanupOutcome                            string
	CleanupReceipt                                               [32]byte
	CleanupCompletedAt                                           time.Time
	QuotaSettlementID                                            string
	QuotaSettledAt                                               time.Time
}
type RevisionMutationPublication struct {
	PublicationID                          string
	OwnerKind                              attachments.BlobPublicationOwnerKind
	OwnerID, BlobKey, ObjectVersion, State string
	ProjectID                              string
	OwnerGeneration                        int64
	BackendKind                            attachments.BackendKind
	NamespaceID                            string
	SizeBytes                              int64
	CleanupOwnerID                         string
	CleanupGeneration                      int64
	CleanupLeaseExpiresAt                  time.Time
	ObligationID                           string
}
type RevisionMutationPayload struct{ PayloadDigest [32]byte }
type RevisionMutationCaptureIntent struct {
	IntentID, RecordID, SnapshotID string
	Kind                           evidence.KindKey
	PreviewDigest, SourceDigest    [32]byte
	SelectionJSON, PreviewJSON     string
	EstimatedSizeBytes             int64
	CreatedAt, ValidUntil          time.Time
}
type RevisionMutationSnapshot struct {
	SnapshotID, RecordID string
	PayloadDigest        [32]byte
	SourceKind           SubjectKind
	SourceID             string
	AuthorizationDigest  [32]byte
}
type RevisionMutationLineage struct{ SnapshotID, CopiedFromSnapshotID string }
type RevisionMutationDependency struct{ ResultSnapshotID, SourceRecordID, SourceRevisionID, SourceSnapshotID string }
type RevisionMutationCompletion struct {
	ResultSnapshotID string
	PayloadDigest    [32]byte
}
type RevisionMutationRevision struct{ RecordID, RevisionID string }
type RevisionMutationRevisionParticipant struct{ RevisionID, UserID string }
type RevisionMutationFollower struct {
	ProjectID, RecordID, UserID                                                                    string
	RecordFenceEpoch                                                                               int64
	ManualPreference                                                                               string
	FollowsAuthor, FollowsOwner, FollowsParticipant, FollowsComment, FollowsMention, FollowsAction bool
	PreferenceResultFingerprint                                                                    string
}
type RevisionMutationAction struct {
	ProjectID, RecordID, ActionID, Status string
	RecordFenceEpoch                      int64
	DueAt                                 time.Time
}
type RevisionMutationGeneration struct {
	ProjectID  string
	Generation int64
	State      string
}
type RevisionMutationDocument struct {
	ProjectID, RecordID string
	Generation          int64
}
type RevisionMutationSearchSubject struct {
	ProjectID, RecordID string
	Generation          int64
	SubjectKind         SubjectKind
	SourceID            string
	RelationRole        RelationRole
}

// These declarations describe future effects, not acquired existing row locks.
// Revision and activity identities are the existing command/helper derivations;
// only the genuinely generated outbox row retains an exact semantic scope.
type RevisionMutationNewRevisionEffectKind string

const (
	RevisionMutationNewRevisionRows          RevisionMutationNewRevisionEffectKind = "revision_rows"
	RevisionMutationNewAttachmentReference   RevisionMutationNewRevisionEffectKind = "attachment_reference"
	RevisionMutationNewEvidenceReference     RevisionMutationNewRevisionEffectKind = "evidence_reference"
	RevisionMutationNewCollaborationActivity RevisionMutationNewRevisionEffectKind = "collaboration_activity"
	RevisionMutationNewCollaborationOutbox   RevisionMutationNewRevisionEffectKind = "collaboration_outbox"
)

type RevisionMutationEventKind string

const (
	RevisionMutationEventOwnerChanged       RevisionMutationEventKind = "record_owner_changed"
	RevisionMutationEventParticipantChanged RevisionMutationEventKind = "record_participant_changed"
	RevisionMutationEventFollowUpChanged    RevisionMutationEventKind = "record_follow_up_changed"
)

type RevisionMutationNewRevisionEffect struct {
	ProjectID, RecordID, RevisionID string
	Kind                            RevisionMutationNewRevisionEffectKind
	Ordinal                         int64
	AttachmentID, SnapshotID        string
	ActivityID                      string
	EventKind                       RevisionMutationEventKind
}

// CombineRevisionMutationResources validates each family, rejects contradictory
// observations of the same identity, and returns detached, deterministically
// ordered slices. Imports retain the original private pre-Put GC observation.
func CombineRevisionMutationResources(sets ...RevisionMutationResources) (RevisionMutationResources, error) {
	var out RevisionMutationResources
	for _, set := range sets {
		out.Users = append(out.Users, set.Users...)
		out.Groups = append(out.Groups, set.Groups...)
		out.Memberships = append(out.Memberships, set.Memberships...)
		out.Sources = append(out.Sources, set.Sources...)
		out.Records = append(out.Records, set.Records...)
		out.RootObservations = append(out.RootObservations, set.RootObservations...)
		out.Reservations = append(out.Reservations, set.Reservations...)
		out.Births = append(out.Births, set.Births...)
		out.Allocations = append(out.Allocations, set.Allocations...)
		out.RecordGateScopes = append(out.RecordGateScopes, set.RecordGateScopes...)
		out.Idempotency = append(out.Idempotency, set.Idempotency...)
		out.ImportJobs = append(out.ImportJobs, set.ImportJobs...)
		out.ExportJobs = append(out.ExportJobs, set.ExportJobs...)
		out.SearchRebuildJobs = append(out.SearchRebuildJobs, set.SearchRebuildJobs...)
		out.Projects = append(out.Projects, set.Projects...)
		out.Drafts = append(out.Drafts, set.Drafts...)
		out.Attachments = append(out.Attachments, set.Attachments...)
		out.Imports = append(out.Imports, set.Imports...)
		out.Uploads = append(out.Uploads, set.Uploads...)
		out.Workspaces = append(out.Workspaces, set.Workspaces...)
		out.Jobs = append(out.Jobs, set.Jobs...)
		out.BlobTables = append(out.BlobTables, set.BlobTables...)
		out.Blobs = append(out.Blobs, set.Blobs...)
		out.Parts = append(out.Parts, set.Parts...)
		out.WriteObligations = append(out.WriteObligations, set.WriteObligations...)
		out.Publications = append(out.Publications, set.Publications...)
		out.Payloads = append(out.Payloads, set.Payloads...)
		out.CaptureIntents = append(out.CaptureIntents, set.CaptureIntents...)
		out.Snapshots = append(out.Snapshots, set.Snapshots...)
		out.Lineages = append(out.Lineages, set.Lineages...)
		out.Dependencies = append(out.Dependencies, set.Dependencies...)
		out.Completions = append(out.Completions, set.Completions...)
		out.Revisions = append(out.Revisions, set.Revisions...)
		out.RevisionParticipants = append(out.RevisionParticipants, set.RevisionParticipants...)
		out.Followers = append(out.Followers, set.Followers...)
		out.Actions = append(out.Actions, set.Actions...)
		out.Generations = append(out.Generations, set.Generations...)
		out.Documents = append(out.Documents, set.Documents...)
		out.SearchSubjects = append(out.SearchSubjects, set.SearchSubjects...)
		out.NewRevisionEffects = append(out.NewRevisionEffects, set.NewRevisionEffects...)
	}
	for index := range out.Reservations {
		out.Reservations[index].OwnerExpiresAt = out.Reservations[index].OwnerExpiresAt.UTC()
	}
	for index := range out.Uploads {
		out.Uploads[index].TemporaryObjectDeletedAt = out.Uploads[index].TemporaryObjectDeletedAt.UTC()
	}
	for index := range out.Jobs {
		out.Jobs[index].LeaseExpiresAt = out.Jobs[index].LeaseExpiresAt.UTC()
	}
	for index := range out.ImportJobs {
		out.ImportJobs[index].LeaseExpiresAt = out.ImportJobs[index].LeaseExpiresAt.UTC()
	}
	for index := range out.ExportJobs {
		out.ExportJobs[index].LeaseExpiresAt = out.ExportJobs[index].LeaseExpiresAt.UTC()
	}
	for index := range out.SearchRebuildJobs {
		out.SearchRebuildJobs[index].LeaseExpiresAt = out.SearchRebuildJobs[index].LeaseExpiresAt.UTC()
	}
	for index := range out.WriteObligations {
		item := &out.WriteObligations[index]
		item.OriginOwnerExpiresAt = item.OriginOwnerExpiresAt.UTC()
		item.TemporaryCreatedAt = item.TemporaryCreatedAt.UTC()
		item.DispatchedAt = item.DispatchedAt.UTC()
		item.ReturnedAt = item.ReturnedAt.UTC()
		item.HandedOffAt = item.HandedOffAt.UTC()
		item.PublishedAt = item.PublishedAt.UTC()
		item.CleanupExpiresAt = item.CleanupExpiresAt.UTC()
		item.CleanupCompletedAt = item.CleanupCompletedAt.UTC()
		item.QuotaSettledAt = item.QuotaSettledAt.UTC()
	}
	for index := range out.Publications {
		out.Publications[index].CleanupLeaseExpiresAt = out.Publications[index].CleanupLeaseExpiresAt.UTC()
	}
	for index := range out.CaptureIntents {
		out.CaptureIntents[index].CreatedAt = out.CaptureIntents[index].CreatedAt.UTC()
		out.CaptureIntents[index].ValidUntil = out.CaptureIntents[index].ValidUntil.UTC()
	}
	for index := range out.Actions {
		out.Actions[index].DueAt = out.Actions[index].DueAt.UTC()
	}
	var err error
	out.Users, err = normalizeMutationFamily(out.Users, func(v RevisionMutationUser) (string, bool) {
		return v.UserID, recordauth.ValidateActorUserID(v.UserID) == nil
	})
	out.Groups, err = normalizeMutationFamilyUnlessError(err, out.Groups, func(v RevisionMutationGroup) (string, bool) {
		return mutationKey(v.ProjectID, v.GroupID), mutationProject(v.ProjectID) && recordauth.ValidateGroupID(v.GroupID) == nil
	})
	out.Memberships, err = normalizeMutationFamilyUnlessError(err, out.Memberships, func(v RevisionMutationMembership) (string, bool) {
		return mutationKey(v.GroupID, v.UserID), recordauth.ValidateGroupID(v.GroupID) == nil && recordauth.ValidateActorUserID(v.UserID) == nil
	})
	out.Sources, err = normalizeMutationFamilyUnlessError(err, out.Sources, func(v RevisionMutationSource) (string, bool) {
		return mutationKey(v.ProjectID, string(v.SourceKind), v.SourceID), mutationProject(v.ProjectID) && ValidSubjectSourceID(v.SourceKind, v.SourceID)
	})
	out.Records, err = normalizeMutationFamilyUnlessError(err, out.Records, func(v RevisionMutationRecord) (string, bool) {
		return mutationKey(v.ProjectID, v.RecordID), mutationRecord(v.ProjectID, v.RecordID)
	})
	out.RootObservations, err = normalizeMutationFamilyUnlessError(err, out.RootObservations, func(v RevisionMutationRootObservation) (string, bool) {
		valid := mutationRecord(v.ProjectID, v.RecordID) && v.LockVersion >= 0 && v.AuthorizationEpoch >= 0
		if v.Exists {
			valid = valid && ValidateLifecycle(Lifecycle(v.Lifecycle)) == nil && (v.CurrentRevisionID == "" || ValidRevisionID(v.CurrentRevisionID))
		} else {
			valid = valid && v.CurrentRevisionID == "" && v.Lifecycle == "" && v.LockVersion == 0 && v.AuthorizationEpoch == 0
		}
		return mutationKey(v.ProjectID, v.RecordID), valid
	})
	out.Reservations, err = normalizeMutationFamilyUnlessError(err, out.Reservations, func(v RevisionMutationReservation) (string, bool) {
		return v.ReservationID, mutationRecord(v.ProjectID, v.RecordID) && mutationPrefixedID(v.ReservationID, "drs_") && v.FenceEpoch >= 0 && v.OwnerGeneration >= 0 && (v.State == "previewed" || v.State == "fenced" || v.State == "committed")
	})
	out.Births, err = normalizeMutationFamilyUnlessError(err, out.Births, func(v RevisionMutationBirth) (string, bool) {
		return v.AllocationID, mutationBirthID(v.AllocationID, "rqa_") && v.Request.Validate() == nil &&
			v.Creation.Validate() == nil && v.Creation.AuthorID == v.Request.ActorID
	})
	out.Births, err = normalizeMutationFamilyUnlessError(err, out.Births, func(v RevisionMutationBirth) (string, bool) {
		return v.Creation.ReservationID, true
	})
	out.Births, err = normalizeMutationFamilyUnlessError(err, out.Births, func(v RevisionMutationBirth) (string, bool) {
		return mutationKey(v.Request.ProjectID, v.Creation.RecordID), true
	})
	out.Allocations, err = normalizeMutationFamilyUnlessError(err, out.Allocations, func(v RevisionMutationAllocation) (string, bool) {
		return v.AllocationID, mutationBirthID(v.AllocationID, "rqa_") && v.Request.Validate() == nil &&
			(recordidentity.Classify(v.RecordID) == recordidentity.Ordinary || recordidentity.Classify(v.RecordID) == recordidentity.Certifiable)
	})
	out.Allocations, err = normalizeMutationFamilyUnlessError(err, out.Allocations, func(v RevisionMutationAllocation) (string, bool) {
		return mutationKey(v.Request.ProjectID, v.RecordID), true
	})
	out.RecordGateScopes, err = normalizeMutationFamilyUnlessError(err, out.RecordGateScopes, func(v RevisionMutationRecordGateScope) (string, bool) {
		return mutationKey(v.ProjectID, v.RecordID), mutationRecord(v.ProjectID, v.RecordID)
	})
	out.Idempotency, err = normalizeMutationFamilyUnlessError(err, out.Idempotency, func(v RevisionMutationIdempotency) (string, bool) {
		return mutationKey(string(v.Key.ProjectID), string(v.Key.OperationKind), v.Key.Key), v.Key.Validate() == nil
	})
	out.ImportJobs, err = normalizeMutationFamilyUnlessError(err, out.ImportJobs, func(v RevisionMutationImportJob) (string, bool) {
		return mutationKey(v.ProjectID, v.JobID), mutationProject(v.ProjectID) && mutationPrefixedID(v.JobID, "rij_") && (v.ActorID == "" || recordauth.ValidateActorUserID(v.ActorID) == nil) && v.LockVersion >= 0 && v.OwnerGeneration >= 0
	})
	out.ExportJobs, err = normalizeMutationFamilyUnlessError(err, out.ExportJobs, func(v RevisionMutationExportJob) (string, bool) {
		return mutationKey(v.ProjectID, v.JobID), mutationProject(v.ProjectID) && mutationPrefixedID(v.JobID, "rej_") && (v.ActorID == "" || recordauth.ValidateActorUserID(v.ActorID) == nil) && (v.RecordID == "" || recordidentity.IsValid(v.RecordID)) && (v.RevisionID == "" || ValidRevisionID(v.RevisionID)) && v.LockVersion >= 0 && v.OwnerGeneration >= 0 && v.AuthorizationEpoch >= 0
	})
	out.SearchRebuildJobs, err = normalizeMutationFamilyUnlessError(err, out.SearchRebuildJobs, func(v RevisionMutationSearchRebuildJob) (string, bool) {
		return mutationKey(v.ProjectID, v.JobID), mutationProject(v.ProjectID) && mutationPrefixedID(v.JobID, "rsj_") && (v.ResumeAfterRecordID == "" || recordidentity.IsValid(v.ResumeAfterRecordID)) && v.Generation >= 0 && v.ProcessedCount >= 0
	})
	out.Projects, err = normalizeMutationFamilyUnlessError(err, out.Projects, func(v RevisionMutationProject) (string, bool) {
		return mutationKey(v.ProjectID, string(v.Kind)), mutationProject(v.ProjectID) && (v.Kind == RevisionMutationEvidenceCapacity || v.Kind == RevisionMutationAttachmentQuota || v.Kind == RevisionMutationSearchCoordination)
	})
	out.Drafts, err = normalizeMutationFamilyUnlessError(err, out.Drafts, func(v RevisionMutationDraft) (string, bool) {
		return v.DraftID, mutationProject(v.ProjectID) && validDraftID(v.DraftID) && (v.RecordID == "" || recordidentity.IsValid(v.RecordID)) && (v.BaseRevisionID == "" || ValidRevisionID(v.BaseRevisionID)) && recordauth.ValidateActorUserID(v.AuthorID) == nil && v.ETag != [32]byte{}
	})
	out.Attachments, err = normalizeMutationFamilyUnlessError(err, out.Attachments, func(v RevisionMutationAttachment) (string, bool) {
		return v.AttachmentID, mutationProject(v.ProjectID) && attachments.ValidateAttachmentID(v.AttachmentID) == nil && ((v.RecordID != "" && v.DraftID == "" && recordidentity.IsValid(v.RecordID)) || (v.DraftID != "" && v.RecordID == "" && validDraftID(v.DraftID))) && v.LogicalSizeBytes >= 0 && mutationAttachmentState(v.State)
	})
	out.Imports, err = normalizeMutationFamilyUnlessError(err, out.Imports, func(v attachments.ImportedAvailableAttachment) (string, bool) {
		return v.AttachmentID, v.Validate() == nil
	})
	out.Uploads, err = normalizeMutationFamilyUnlessError(err, out.Uploads, func(v RevisionMutationUpload) (string, bool) {
		return v.UploadID, mutationProject(v.ProjectID) && attachments.ValidateUploadID(v.UploadID) == nil && attachments.ValidateAttachmentID(v.AttachmentID) == nil && (v.OriginDraftID == "" || validDraftID(v.OriginDraftID)) && v.ReservedSizeBytes > 0 && mutationAttachmentState(v.State) && (v.Transport == attachments.TransportKindLocal || v.Transport == attachments.TransportKindS3)
	})
	out.Workspaces, err = normalizeMutationFamilyUnlessError(err, out.Workspaces, func(v RevisionMutationWorkspace) (string, bool) {
		return v.WorkspaceID, attachments.ValidateWorkspaceID(v.WorkspaceID) == nil && attachments.ValidateProcessorJobID(v.ProcessorJobID) == nil && (v.State == "registered" || v.State == "materialized" || v.State == "purging" || v.State == "purged")
	})
	out.Jobs, err = normalizeMutationFamilyUnlessError(err, out.Jobs, func(v RevisionMutationJob) (string, bool) {
		return v.ProcessorJobID, attachments.ValidateProcessorJobID(v.ProcessorJobID) == nil && attachments.ValidateAttachmentID(v.AttachmentID) == nil && (v.UploadID == "" || attachments.ValidateUploadID(v.UploadID) == nil) && v.OwnerGeneration >= 0 && (v.State == "queued" || v.State == "claimed" || v.State == "retry_wait" || v.State == "succeeded" || v.State == "rejected" || v.State == "expired")
	})
	out.BlobTables, err = normalizeMutationFamilyUnlessError(err, out.BlobTables, func(v RevisionMutationBlobTable) (string, bool) {
		return string(v), v == RevisionMutationBlobMetadataTable || v == RevisionMutationUploadPartsTable
	})
	out.Blobs, err = normalizeMutationFamilyUnlessError(err, out.Blobs, func(v attachments.BlobObject) (string, bool) { return v.Key, v.Validate() == nil })
	out.Parts, err = normalizeMutationFamilyUnlessError(err, out.Parts, func(v RevisionMutationPart) (string, bool) {
		return mutationKey(v.UploadID, strconv.FormatInt(v.PartNumber, 10)), attachments.ValidateUploadID(v.UploadID) == nil && v.PartNumber > 0 && v.Object.Validate() == nil
	})
	out.WriteObligations, err = normalizeMutationFamilyUnlessError(err, out.WriteObligations, func(v RevisionMutationWriteObligation) (string, bool) {
		validOrigin := v.OriginKind == "upload" || v.OriginKind == "processor_preview" || v.OriginKind == "import_attachment" || v.OriginKind == "export_artifact" || v.OriginKind == "import_archive"
		validBackend := (v.BackendKind == attachments.BackendKindLocal && v.NamespaceID != "" && v.IncarnationID != "") || (v.BackendKind == attachments.BackendKindS3 && v.NamespaceID == "" && v.IncarnationID == "")
		validOutcome := v.Outcome == "registered" || v.Outcome == "materialized" || v.Outcome == "failed" || v.Outcome == "local_excluded" || v.Outcome == "remote_unknown"
		return v.ObligationID, mutationWriteObligationID(v.ObligationID) && mutationProject(v.ProjectID) && validOrigin && v.OriginID != "" && v.OriginGeneration > 0 && v.OriginOwnerGeneration >= 0 && v.RequestSeq >= 0 && v.TemporaryDevice >= 0 && v.TemporaryInode >= 0 && (v.RecordID == "" || recordidentity.IsValid(v.RecordID)) && v.RecordEpoch >= 0 && validBackend && v.CanonicalKey == "sha256/"+hex.EncodeToString(v.SHA256[:]) && v.SizeBytes > 0 && validOutcome && v.CleanupGeneration >= 0 && (v.QuotaSettlementID == "" || mutationWriteObligationID(v.QuotaSettlementID))
	})
	out.Publications, err = normalizeMutationFamilyUnlessError(err, out.Publications, func(v RevisionMutationPublication) (string, bool) {
		owner := (v.OwnerKind == attachments.BlobPublicationOwnerUpload && attachments.ValidateUploadID(v.OwnerID) == nil) || (v.OwnerKind == attachments.BlobPublicationOwnerProcessorPreview && attachments.ValidateProcessorJobID(v.OwnerID) == nil)
		return v.PublicationID, mutationPrefixedID(v.PublicationID, "bpi_") && owner && (v.ProjectID == "" || mutationProject(v.ProjectID)) && v.OwnerGeneration >= 0 && v.CleanupGeneration >= 0 && v.SizeBytes >= 0 && (v.BackendKind == "" || v.BackendKind == attachments.BackendKindLocal || v.BackendKind == attachments.BackendKindS3) && mutationBlobKey(v.BlobKey) && len(v.ObjectVersion) <= 1024 && (v.State == "prepared" || v.State == "published" || v.State == "cleanup_claimed" || v.State == "retry_wait" || v.State == "completed")
	})
	out.Payloads, err = normalizeMutationFamilyUnlessError(err, out.Payloads, func(v RevisionMutationPayload) (string, bool) {
		return hex.EncodeToString(v.PayloadDigest[:]), v.PayloadDigest != [32]byte{}
	})
	out.CaptureIntents, err = normalizeMutationFamilyUnlessError(err, out.CaptureIntents, func(v RevisionMutationCaptureIntent) (string, bool) {
		return v.IntentID, mutationCaptureIntentID(v.IntentID) && recordidentity.IsValid(v.RecordID) && (v.SnapshotID == "" || evidence.ValidSnapshotID(v.SnapshotID)) && v.Kind.Kind != "" && v.Kind.SchemaVersion > 0 && v.PreviewDigest != [32]byte{} && v.SourceDigest != [32]byte{} && v.EstimatedSizeBytes > 0 && !v.CreatedAt.IsZero() && !v.ValidUntil.IsZero()
	})
	out.Snapshots, err = normalizeMutationFamilyUnlessError(err, out.Snapshots, func(v RevisionMutationSnapshot) (string, bool) {
		return v.SnapshotID, evidence.ValidSnapshotID(v.SnapshotID) && recordidentity.IsValid(v.RecordID) && v.PayloadDigest != [32]byte{} && ValidSubjectSourceID(v.SourceKind, v.SourceID) && v.AuthorizationDigest != [32]byte{}
	})
	out.Lineages, err = normalizeMutationFamilyUnlessError(err, out.Lineages, func(v RevisionMutationLineage) (string, bool) {
		return v.SnapshotID, evidence.ValidSnapshotID(v.SnapshotID) && evidence.ValidSnapshotID(v.CopiedFromSnapshotID) && v.SnapshotID != v.CopiedFromSnapshotID
	})
	out.Dependencies, err = normalizeMutationFamilyUnlessError(err, out.Dependencies, func(v RevisionMutationDependency) (string, bool) {
		return mutationKey(v.ResultSnapshotID, v.SourceRecordID, v.SourceRevisionID, v.SourceSnapshotID), evidence.ValidSnapshotID(v.ResultSnapshotID) && recordidentity.IsValid(v.SourceRecordID) && (v.SourceRevisionID == "" || ValidRevisionID(v.SourceRevisionID)) && (v.SourceSnapshotID == "" || evidence.ValidSnapshotID(v.SourceSnapshotID))
	})
	out.Completions, err = normalizeMutationFamilyUnlessError(err, out.Completions, func(v RevisionMutationCompletion) (string, bool) {
		return v.ResultSnapshotID, evidence.ValidSnapshotID(v.ResultSnapshotID) && v.PayloadDigest != [32]byte{}
	})
	out.Revisions, err = normalizeMutationFamilyUnlessError(err, out.Revisions, func(v RevisionMutationRevision) (string, bool) {
		return v.RevisionID, recordidentity.IsValid(v.RecordID) && ValidRevisionID(v.RevisionID)
	})
	out.RevisionParticipants, err = normalizeMutationFamilyUnlessError(err, out.RevisionParticipants, func(v RevisionMutationRevisionParticipant) (string, bool) {
		return mutationKey(v.RevisionID, v.UserID), ValidRevisionID(v.RevisionID) && mutationPrefixedID(v.UserID, "usr_")
	})
	out.Followers, err = normalizeMutationFamilyUnlessError(err, out.Followers, func(v RevisionMutationFollower) (string, bool) {
		return mutationKey(v.RecordID, v.UserID), mutationRecord(v.ProjectID, v.RecordID) && mutationPrefixedID(v.UserID, "usr_") && v.RecordFenceEpoch >= 0 && (v.ManualPreference == "" || v.ManualPreference == "default" || v.ManualPreference == "watching" || v.ManualPreference == "muted")
	})
	out.Actions, err = normalizeMutationFamilyUnlessError(err, out.Actions, func(v RevisionMutationAction) (string, bool) {
		return v.ActionID, mutationRecord(v.ProjectID, v.RecordID) && mutationPrefixedID(v.ActionID, "ract_") && v.RecordFenceEpoch >= 0 && (v.Status == "open" || v.Status == "completed" || v.Status == "cancelled")
	})
	out.Generations, err = normalizeMutationFamilyUnlessError(err, out.Generations, func(v RevisionMutationGeneration) (string, bool) {
		return strconv.FormatInt(v.Generation, 10), mutationProject(v.ProjectID) && v.Generation > 0 && (v.State == "published" || v.State == "building")
	})
	out.Documents, err = normalizeMutationFamilyUnlessError(err, out.Documents, func(v RevisionMutationDocument) (string, bool) {
		return mutationKey(strconv.FormatInt(v.Generation, 10), v.RecordID), mutationRecord(v.ProjectID, v.RecordID) && v.Generation > 0
	})
	out.SearchSubjects, err = normalizeMutationFamilyUnlessError(err, out.SearchSubjects, func(v RevisionMutationSearchSubject) (string, bool) {
		return mutationKey(strconv.FormatInt(v.Generation, 10), v.RecordID, string(v.SubjectKind), v.SourceID, string(v.RelationRole)), mutationRecord(v.ProjectID, v.RecordID) && v.Generation > 0 && ValidSubjectSourceID(v.SubjectKind, v.SourceID) && ValidRelationRole(v.RelationRole)
	})
	out.NewRevisionEffects, err = normalizeMutationFamilyUnlessError(err, out.NewRevisionEffects, mutationNewRevisionEffectKey)
	if err != nil {
		return RevisionMutationResources{}, err
	}
	// PostgreSQL generation columns use numeric ordering, including across
	// decimal digit boundaries. Preserve the normalized identity order within
	// each generation while matching that lock order.
	sort.SliceStable(out.Generations, func(i, j int) bool { return out.Generations[i].Generation < out.Generations[j].Generation })
	sort.SliceStable(out.Documents, func(i, j int) bool { return out.Documents[i].Generation < out.Documents[j].Generation })
	sort.SliceStable(out.SearchSubjects, func(i, j int) bool { return out.SearchSubjects[i].Generation < out.SearchSubjects[j].Generation })
	return out, nil
}

func normalizeMutationFamilyUnlessError[T comparable](previous error, items []T, identity func(T) (string, bool)) ([]T, error) {
	if previous != nil {
		return nil, previous
	}
	return normalizeMutationFamily(items, identity)
}
func normalizeMutationFamily[T comparable](items []T, identity func(T) (string, bool)) ([]T, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		if _, valid := identity(items[0]); !valid {
			return nil, fmt.Errorf("%w: %T", ErrInvalidRevisionMutationResources, items[0])
		}
		return items, nil
	}
	unique := make(map[string]T, len(items))
	keys := make([]string, 0, len(items))
	for _, item := range items {
		key, valid := identity(item)
		if !valid {
			return nil, fmt.Errorf("%w: %T", ErrInvalidRevisionMutationResources, item)
		}
		if old, exists := unique[key]; exists {
			if old != item {
				return nil, fmt.Errorf("%w: contradictory %T", ErrInvalidRevisionMutationResources, item)
			}
			continue
		}
		unique[key] = item
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := items[:0]
	for _, key := range keys {
		out = append(out, unique[key])
	}
	return out, nil
}
func mutationProject(project string) bool {
	return recordauth.ValidateProjectID(recordauth.ProjectID(project)) == nil
}
func mutationRecord(project, record string) bool {
	return mutationProject(project) && recordidentity.IsValid(record)
}
func mutationKey(values ...string) string { return strings.Join(values, "\x00") }
func mutationCaptureIntentID(value string) bool {
	if !strings.HasPrefix(value, "evi_") || len(value) != 28 {
		return false
	}
	digest, err := hex.DecodeString(value[4:])
	return err == nil && hex.EncodeToString(digest) == value[4:]
}

func mutationWriteObligationID(value string) bool {
	if len(value) != 36 || !strings.HasPrefix(value, "rwo_") {
		return false
	}
	for _, character := range value[4:] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func mutationBirthID(value, prefix string) bool {
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, c := range value[len(prefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func mutationPrefixedID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || len(value) > len(prefix)+64 {
		return false
	}
	for _, c := range value[len(prefix):] {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
func mutationBlobKey(key string) bool {
	if !strings.HasPrefix(key, "sha256/") || len(key) != 71 {
		return false
	}
	digest, err := hex.DecodeString(key[7:])
	return err == nil && hex.EncodeToString(digest) == key[7:]
}
func mutationAttachmentState(state string) bool {
	switch state {
	case "created", "uploading", "quarantined", "available", "rejected", "expired":
		return true
	}
	return false
}
func mutationNewRevisionEffectKey(v RevisionMutationNewRevisionEffect) (string, bool) {
	key := mutationKey(v.RevisionID, string(v.Kind), strconv.FormatInt(v.Ordinal, 10))
	if !mutationRecord(v.ProjectID, v.RecordID) || !ValidRevisionID(v.RevisionID) || v.Ordinal < 0 {
		return key, false
	}
	switch v.Kind {
	case RevisionMutationNewAttachmentReference:
		return key, attachments.ValidateAttachmentID(v.AttachmentID) == nil && v.SnapshotID == "" && v.ActivityID == "" && v.EventKind == ""
	case RevisionMutationNewEvidenceReference:
		return key, evidence.ValidSnapshotID(v.SnapshotID) && v.AttachmentID == "" && v.ActivityID == "" && v.EventKind == ""
	case RevisionMutationNewRevisionRows:
		kind := DomainActivityKind(v.EventKind)
		return key, v.Ordinal == 0 && v.AttachmentID == "" && v.SnapshotID == "" &&
			v.ActivityID == "rac_"+strings.TrimPrefix(v.RevisionID, "rrv_") &&
			(kind == DomainActivityRecordCreated || kind == DomainActivityRecordRevised || kind == DomainActivityRecordRestored)
	case RevisionMutationNewCollaborationActivity:
		key = mutationKey(string(v.Kind), v.ActivityID)
		return key, v.Ordinal == 0 && v.AttachmentID == "" && v.SnapshotID == "" && mutationPrefixedID(v.ActivityID, "rac_") &&
			(v.EventKind == RevisionMutationEventOwnerChanged || v.EventKind == RevisionMutationEventParticipantChanged || v.EventKind == RevisionMutationEventFollowUpChanged)
	case RevisionMutationNewCollaborationOutbox:
		key = mutationKey(v.RevisionID, string(v.Kind), string(v.EventKind))
		return key, v.Ordinal == 0 && v.AttachmentID == "" && v.SnapshotID == "" && v.ActivityID == "" &&
			(v.EventKind == RevisionMutationEventOwnerChanged || v.EventKind == RevisionMutationEventParticipantChanged)
	default:
		return key, false
	}
}

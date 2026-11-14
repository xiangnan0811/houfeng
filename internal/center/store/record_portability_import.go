package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

type RecordImportJob struct {
	ImportJobID        string
	PlanID             string
	ActorID            string
	JobState           string
	LockVersion        uint64
	ArchiveDigest      [32]byte
	DestinationSubject records.SubjectReference
	ExpiresAt          time.Time
}

type RecordImportArtifact struct {
	ArtifactID      string
	ImportJobID     string
	ArtifactRole    string
	BackendKind     string
	BlobKey         string
	ObjectVersionID string
	NamespaceID     string
	ObligationID    string
	SHA256          [32]byte
	ByteSize        uint64
	ExpiresAt       time.Time
}

type PublishRecordImportArtifactInput struct {
	ImportJobID     string
	ArtifactRole    string
	BackendKind     string
	BlobKey         string
	ObjectVersionID string
	NamespaceID     string
	ObligationID    string
	SHA256          [32]byte
	ByteSize        uint64
	ExpiresAt       time.Time
}

type ImportRemap struct {
	EntityKind        string
	SourceID          string
	TargetID          string
	BirthAllocationID string
	// OwnerTargetRecordID is derived from the archive's source owner and the
	// plan's record remap; it is required for new evidence and attachment rows.
	OwnerTargetRecordID string
}

type ImportDocumentPlan struct {
	SourceID string
	TargetID string
	Title    string
	Body     string
}

type QuarantinedEvidence struct {
	Kind       string
	Schema     string
	Digest     string
	ByteSize   int64
	Reason     string
	ObservedAt string
}

type RecordImportPlan struct {
	ImportPlanID       string
	ImportJobID        string
	PlanDigest         [32]byte
	ObjectCount        uint64
	RemapCount         uint64
	LockVersion        uint64
	JobState           string
	DestinationSubject records.SubjectReference
	Remaps             []ImportRemap
	Documents          []ImportDocumentPlan
	Quarantine         []QuarantinedEvidence
	AppliedRecordIDs   []string
	ExpiresAt          time.Time
}

type ClaimRecordImportJobInput struct {
	ActorID            string
	IdempotencyKey     string
	ArchiveDigest      [32]byte
	DestinationSubject records.SubjectReference
	ExpiresAt          time.Time
}

type SaveRecordImportPlanInput struct {
	ImportJobID        string
	Actor              recordauth.ActorScope
	ArchiveDigest      [32]byte
	PlanDigest         [32]byte
	ObjectCount        uint64
	RemapCount         uint64
	Remaps             []ImportRemap
	Allocations        []recordbirth.Allocation
	Quarantine         []QuarantinedEvidence
	Documents          []ImportDocumentPlan
	DestinationSubject records.SubjectReference
	ExpiresAt          time.Time
}

func validImportDestination(destination records.SubjectReference) bool {
	return destination.RegistryVersion == records.SubjectRegistryVersionV1 &&
		records.ValidSubjectKind(destination.Kind) &&
		destination.Role == records.RelationRoleAffected &&
		destination.Primary &&
		records.ValidSubjectSourceID(destination.Kind, destination.SourceID)
}

func decodeImportDestination(kind, sourceID *string) (records.SubjectReference, error) {
	if kind == nil && sourceID == nil {
		return records.SubjectReference{}, ErrRecordImportCASConflict
	}
	if kind == nil || sourceID == nil {
		return records.SubjectReference{}, ErrRecordPortabilityUnavailable
	}
	destination := records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            records.SubjectKind(*kind),
		Role:            records.RelationRoleAffected,
		SourceID:        *sourceID,
		Primary:         true,
	}
	if !validImportDestination(destination) {
		return records.SubjectReference{}, ErrRecordPortabilityUnavailable
	}
	return destination, nil
}

type AdvanceRecordImportJobInput struct {
	ImportJobID      string
	LockVersion      uint64
	JobState         string
	FailureCode      string
	AppliedRecordIDs []string
}

type RecordOriginTombstone struct {
	OriginDigest [32]byte
}

type RecordOrigin struct {
	OriginID     string
	OriginDigest [32]byte
}

type InsertRecordOriginInput struct {
	OriginKind   string
	OriginDigest [32]byte
	SourceRecord string
}

func (repository *PostgresRecordPortabilityRepository) ClaimImportJob(
	ctx context.Context,
	input ClaimRecordImportJobInput,
) (RecordImportJob, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		input.ActorID == "" || input.IdempotencyKey == "" || input.ArchiveDigest == [32]byte{} ||
		!validImportDestination(input.DestinationSubject) {
		return RecordImportJob{}, ErrRecordPortabilityUnavailable
	}
	var claimed RecordImportJob
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var loadExisting = func() error {
			var digest []byte
			var destinationKind, destinationSourceID *string
			err := transaction.tx.QueryRow(ctx, `
				select import_job_id, actor_id, job_state, lock_version, archive_digest,
				       destination_subject_kind, destination_subject_source_id, expires_at
				from public.record_import_jobs
				where project_id = $1 and actor_id = $2 and idempotency_key = $3
				for update
			`, recordauth.ProjectIDDefault, input.ActorID, input.IdempotencyKey).Scan(
				&claimed.ImportJobID, &claimed.ActorID, &claimed.JobState, &claimed.LockVersion,
				&digest, &destinationKind, &destinationSourceID, &claimed.ExpiresAt,
			)
			if err != nil {
				return err
			}
			if len(digest) != sha256.Size {
				return ErrRecordPortabilityUnavailable
			}
			copy(claimed.ArchiveDigest[:], digest)
			destination, err := decodeImportDestination(destinationKind, destinationSourceID)
			if err != nil {
				return err
			}
			claimed.DestinationSubject = destination
			if claimed.ArchiveDigest != input.ArchiveDigest || claimed.DestinationSubject != input.DestinationSubject {
				return ErrRecordImportCASConflict
			}
			return attachImportPlanID(ctx, transaction, &claimed)
		}

		err := loadExisting()
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		jobID, err := ids.New("rij")
		if err != nil {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		tag, err := transaction.tx.Exec(ctx, `
			insert into public.record_import_jobs (
				import_job_id, project_id, actor_id, idempotency_key, job_state,
				identity_classification, archive_digest, lock_version,
				destination_subject_kind, destination_subject_source_id,
				expires_at, created_at, updated_at
			) values ($1, $2, $3, $4, $5, $6, $7, 1, $8, $9, $10, $11, $11)
			on conflict (project_id, actor_id, idempotency_key) do nothing
		`, jobID, recordauth.ProjectIDDefault, input.ActorID, input.IdempotencyKey,
			RecordImportJobStateQuarantined, "unknown", input.ArchiveDigest[:],
			string(input.DestinationSubject.Kind), input.DestinationSubject.SourceID,
			input.ExpiresAt.UTC(), now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return loadExisting()
		}
		claimed = RecordImportJob{
			ImportJobID:        jobID,
			ActorID:            input.ActorID,
			JobState:           RecordImportJobStateQuarantined,
			LockVersion:        1,
			ArchiveDigest:      input.ArchiveDigest,
			DestinationSubject: input.DestinationSubject,
			ExpiresAt:          input.ExpiresAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return RecordImportJob{}, err
	}
	return claimed, nil
}

func attachImportPlanID(ctx context.Context, transaction *RecordPlatformTransaction, job *RecordImportJob) error {
	var planID string
	err := transaction.tx.QueryRow(ctx, `
		select import_plan_id from public.record_import_plans where import_job_id = $1
	`, job.ImportJobID).Scan(&planID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	job.PlanID = planID
	return nil
}

func (repository *PostgresRecordPortabilityRepository) SaveImportPlan(
	ctx context.Context,
	input SaveRecordImportPlanInput,
) (RecordImportPlan, error) {
	actor, actorErr := recordauth.NormalizeActorScope(input.Actor)
	if ctx == nil || repository == nil || repository.platform == nil || repository.witness == nil || input.ImportJobID == "" || actorErr != nil ||
		input.ArchiveDigest == ([32]byte{}) || input.PlanDigest == ([32]byte{}) || !validImportDestination(input.DestinationSubject) {
		return RecordImportPlan{}, ErrRecordPortabilityUnavailable
	}
	if input.RemapCount != uint64(len(input.Remaps)) || input.ObjectCount < input.RemapCount {
		return RecordImportPlan{}, ErrRecordImportCASConflict
	}
	recordsBySource := make(map[string]ImportRemap)
	for _, remap := range input.Remaps {
		if remap.SourceID == "" || remap.TargetID == "" {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
		switch remap.EntityKind {
		case "record":
			if remap.BirthAllocationID == "" || remap.OwnerTargetRecordID != "" {
				return RecordImportPlan{}, ErrRecordImportCASConflict
			}
			if _, exists := recordsBySource[remap.SourceID]; exists {
				return RecordImportPlan{}, ErrRecordImportCASConflict
			}
			recordsBySource[remap.SourceID] = remap
		case "evidence", "attachment":
			if remap.BirthAllocationID != "" || remap.OwnerTargetRecordID == "" {
				return RecordImportPlan{}, ErrRecordImportCASConflict
			}
		default:
			if remap.BirthAllocationID != "" || remap.OwnerTargetRecordID != "" {
				return RecordImportPlan{}, ErrRecordImportCASConflict
			}
		}
	}
	if len(recordsBySource) == 0 || len(recordsBySource) != len(input.Documents) {
		return RecordImportPlan{}, ErrRecordImportCASConflict
	}
	for _, document := range input.Documents {
		remap, exists := recordsBySource[document.SourceID]
		if !exists || remap.TargetID != document.TargetID {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
		delete(recordsBySource, document.SourceID)
	}
	targets := make([]ImportRemap, 0, len(input.Documents))
	for _, remap := range input.Remaps {
		if remap.EntityKind == "record" {
			targets = append(targets, remap)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].TargetID < targets[j].TargetID })
	for index := 1; index < len(targets); index++ {
		if targets[index-1].TargetID == targets[index].TargetID {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
	}
	for _, remap := range input.Remaps {
		if remap.EntityKind != "evidence" && remap.EntityKind != "attachment" {
			continue
		}
		index := sort.Search(len(targets), func(index int) bool {
			return targets[index].TargetID >= remap.OwnerTargetRecordID
		})
		if index == len(targets) || targets[index].TargetID != remap.OwnerTargetRecordID {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
	}
	if len(input.Allocations) != len(targets) {
		return RecordImportPlan{}, ErrRecordImportCASConflict
	}
	allocations := make(map[string]recordbirth.Allocation, len(input.Allocations))
	for _, allocation := range input.Allocations {
		if allocation.Validate() != nil || allocation.Request.ProjectID != string(actor.ProjectID) ||
			allocation.Request.ActorID != actor.UserID || allocation.Request.ActorScopeDigest != actor.CanonicalHash() ||
			allocation.Request.Purpose != recordbirth.PurposeImport {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
		if _, exists := allocations[allocation.RecordID]; exists {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
		allocations[allocation.RecordID] = allocation
	}
	for _, target := range targets {
		allocation, ok := allocations[target.TargetID]
		if !ok || allocation.AllocationID != target.BirthAllocationID {
			return RecordImportPlan{}, ErrRecordImportCASConflict
		}
	}
	sourceKind, ok := recordSubjectSourceKind(input.DestinationSubject.Kind)
	if !ok {
		return RecordImportPlan{}, ErrRecordPortabilityUnavailable
	}
	key := repository.witness.sourceKey(actor.ProjectID, sourceKind, input.DestinationSubject.SourceID)
	authority, err := repository.witness.Prefetch(ctx, []sourceretirement.SourceKey{key})
	if err != nil {
		return RecordImportPlan{}, err
	}
	var saved RecordImportPlan
	err = repository.platform.runSourceAuthorizedBirthMutationTransaction(ctx, repository.continuity, func(ctx context.Context, transaction *RecordPlatformTransaction, proof []byte) error {
		saved = RecordImportPlan{}
		namespaces, err := revisionBatchNamespaces(ctx, transaction.tx, nil, nil, nil, input.ImportJobID)
		if err != nil {
			return err
		}
		if err := lockLocalBlobNamespaces(ctx, transaction.tx, namespaces); err != nil {
			return err
		}
		if err := lockComparisonDecisionForWriter(ctx, transaction.tx); err != nil {
			return err
		}
		recordIDs := make([]string, len(targets))
		for index, target := range targets {
			recordIDs[index] = target.TargetID
		}
		before, err := discoverEffectRecordResources(ctx, transaction.tx, recordIDs, []string{actor.UserID})
		if err != nil {
			return err
		}
		before.Sources = append(before.Sources, records.RevisionMutationSource{
			ProjectID: string(actor.ProjectID), SourceKind: input.DestinationSubject.Kind, SourceID: input.DestinationSubject.SourceID,
		})
		for _, target := range targets {
			allocation := allocations[target.TargetID]
			before.Allocations = append(before.Allocations, records.RevisionMutationAllocation{
				AllocationID: allocation.AllocationID, Request: allocation.Request, RecordID: allocation.RecordID,
			})
		}
		declared, err := normalizeRecordMutationDeclarations(before)
		if err != nil {
			return err
		}
		declared.continuityProof = proof
		if err := verifyRevisionPrefetchedRoutes(authority, declared); err != nil {
			return err
		}
		if err := acquireRecordMutationResourceRange(ctx, transaction.tx, declared, 10, 60); err != nil {
			return err
		}
		lockCommand, err := json.Marshal(struct {
			ImportJobID  string `json:"import_job_id"`
			NamespaceIDs string `json:"namespace_ids"`
		}{input.ImportJobID, strings.Join(namespaces, ",")})
		if err != nil {
			return err
		}
		if _, err := transaction.tx.Exec(ctx, `select public.record_import_plan_lock($1)`, lockCommand); err != nil {
			return err
		}
		var destinationKind, destinationSourceID *string
		var jobActor string
		var archiveDigest []byte
		if err := transaction.tx.QueryRow(ctx, `
			select actor_id, archive_digest, destination_subject_kind, destination_subject_source_id
			from public.record_import_jobs
			where import_job_id = $1
			for update
		`, input.ImportJobID).Scan(&jobActor, &archiveDigest, &destinationKind, &destinationSourceID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrRecordImportNotFound
			}
			return err
		}
		destination, err := decodeImportDestination(destinationKind, destinationSourceID)
		if err != nil {
			return err
		}
		if destination != input.DestinationSubject || jobActor != actor.UserID || !bytes.Equal(archiveDigest, input.ArchiveDigest[:]) {
			return ErrRecordImportCASConflict
		}
		after, err := discoverEffectRecordResources(ctx, transaction.tx, recordIDs, []string{actor.UserID})
		if err != nil {
			return err
		}
		after.Sources = append(after.Sources, records.RevisionMutationSource{
			ProjectID: string(actor.ProjectID), SourceKind: input.DestinationSubject.Kind, SourceID: input.DestinationSubject.SourceID,
		})
		after.Allocations = before.Allocations
		if !reflect.DeepEqual(before, after) {
			return &pgconn.PgError{Code: "40001", Message: "import planning route changed; restart transaction"}
		}
		member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, transaction.tx, actor.ProjectID, actor.UserID)
		if err != nil || member.CanonicalHash() != actor.CanonicalHash() {
			return recordauth.ErrDenied
		}
		resolver, err := revisionTransactionSubjectResolver(transaction.tx, authority)
		if err != nil {
			return err
		}
		live, err := resolver.live.Resolve(ctx, member, input.DestinationSubject)
		if err != nil {
			return err
		}
		resolved, err := resolver.Resolve(ctx, member, RecordSubjectReadInput{
			Reference: input.DestinationSubject, IdentitySnapshot: live.IdentitySnapshot, CaptureAuthorization: live.CaptureAuthorization,
		})
		if err != nil {
			return err
		}
		if resolved.CaptureAuthorization.State != recordauth.SourceStateLive || resolved.CaptureAuthorization.Digest != live.CaptureAuthorization.Digest {
			return ErrRecordSubjectUnavailable
		}
		values, err := records.ImportedRevisionValues(member, "", "")
		if err != nil {
			return err
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityImport, records.RecordAuthorizationEvidence{
			ProjectID: resolved.ProjectID, Visibility: values.VisibilityScope, Sources: []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
		}); err != nil {
			return err
		}
		for _, root := range after.RootObservations {
			if root.Exists {
				return ErrRecordImportCASConflict
			}
		}
		planID, err := ids.New("rip")
		if err != nil {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		if _, err := transaction.tx.Exec(ctx, `
			insert into public.record_import_plans (
				import_plan_id, import_job_id, plan_digest, object_count, remap_count, expires_at, created_at
			) values ($1, $2, $3, $4, $5, $6, $7)
		`, planID, input.ImportJobID, input.PlanDigest[:], int64(input.ObjectCount), int64(input.RemapCount),
			input.ExpiresAt.UTC(), now); err != nil {
			return err
		}
		// Dependent mappings refer to a record mapping already registered in
		// this transaction. The digest retains the original archive order.
		for pass := range 2 {
			for _, remap := range input.Remaps {
				if (remap.EntityKind == "record") != (pass == 0) {
					continue
				}
				source := sha256SumString(remap.SourceID)
				body, err := json.Marshal(struct {
					PlanID              string    `json:"import_plan_id"`
					EntityKind          string    `json:"entity_kind"`
					SourceID            string    `json:"source_id"`
					SourceDigest        string    `json:"source_identity_digest"`
					TargetID            string    `json:"target_id"`
					CreatedAt           time.Time `json:"created_at"`
					BirthAllocationID   string    `json:"birth_allocation_id"`
					OwnerTargetRecordID string    `json:"owner_target_record_id"`
				}{planID, remap.EntityKind, remap.SourceID, hex.EncodeToString(source[:]), remap.TargetID, now, remap.BirthAllocationID, remap.OwnerTargetRecordID})
				if err != nil {
					return err
				}
				if _, err := transaction.tx.Exec(ctx, `select public.record_import_mapping_insert($1)`, body); err != nil {
					return err
				}
			}
		}
		saved = RecordImportPlan{
			ImportPlanID: planID, ImportJobID: input.ImportJobID, PlanDigest: input.PlanDigest,
			ObjectCount: input.ObjectCount, RemapCount: input.RemapCount, Remaps: input.Remaps,
			Documents: input.Documents, Quarantine: input.Quarantine,
			DestinationSubject: destination, ExpiresAt: input.ExpiresAt.UTC(),
			LockVersion: 1, JobState: RecordImportJobStatePlanned,
		}
		return nil
	})
	if err != nil {
		return RecordImportPlan{}, err
	}
	return saved, nil
}

func (repository *PostgresRecordPortabilityRepository) LoadImportPlan(
	ctx context.Context,
	planID string,
) (RecordImportPlan, error) {
	if ctx == nil || repository == nil || repository.platform == nil || planID == "" {
		return RecordImportPlan{}, ErrRecordPortabilityUnavailable
	}
	var plan RecordImportPlan
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var digest []byte
		var objectCount, remapCount int64
		var destinationKind, destinationSourceID *string
		if err := transaction.tx.QueryRow(ctx, `
			select plan.import_plan_id, plan.import_job_id, plan.plan_digest,
			       plan.object_count, plan.remap_count, plan.expires_at,
			       job.destination_subject_kind, job.destination_subject_source_id
			from public.record_import_plans as plan
			join public.record_import_jobs as job on job.import_job_id = plan.import_job_id
			where plan.import_plan_id = $1
		`, planID).Scan(
			&plan.ImportPlanID, &plan.ImportJobID, &digest, &objectCount, &remapCount,
			&plan.ExpiresAt, &destinationKind, &destinationSourceID,
		); err != nil {
			return err
		}
		destination, err := decodeImportDestination(destinationKind, destinationSourceID)
		if err != nil {
			return err
		}
		if len(digest) != sha256.Size {
			return ErrRecordPortabilityUnavailable
		}
		copy(plan.PlanDigest[:], digest)
		plan.ObjectCount = uint64(objectCount)
		plan.RemapCount = uint64(remapCount)
		plan.DestinationSubject = destination
		rows, err := transaction.tx.Query(ctx, `
			select entity_kind, source_id, target_id, coalesce(birth_allocation_id, ''), coalesce(owner_target_record_id, '')
			from public.record_import_entity_mappings
			where import_plan_id = $1
			order by entity_kind, source_id
		`, planID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var remap ImportRemap
			if err := rows.Scan(&remap.EntityKind, &remap.SourceID, &remap.TargetID, &remap.BirthAllocationID, &remap.OwnerTargetRecordID); err != nil {
				return err
			}
			plan.Remaps = append(plan.Remaps, remap)
		}
		return rows.Err()
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordImportPlan{}, ErrRecordImportNotFound
		}
		return RecordImportPlan{}, err
	}
	return plan, nil
}

// CheckImportReplayReadFence reauthorizes the destination and every imported
// record against current APP membership after acquiring the read fences.
// Callers supply the entire record set ordered by record ID.
func (repository *PostgresRecordPortabilityRepository) CheckImportReplayReadFence(
	ctx context.Context,
	actor recordauth.ActorScope,
	destination records.SubjectReference,
	current []records.CurrentRecordAuthorization,
) error {
	actor, actorErr := recordauth.NormalizeActorScope(actor)
	if ctx == nil || repository == nil || repository.platform == nil || repository.authorization == nil ||
		repository.witness == nil || actorErr != nil || !validImportDestination(destination) || len(current) == 0 {
		return ErrRecordPortabilityUnavailable
	}
	ids := make([]string, 0, len(current))
	previous := ""
	for _, binding := range current {
		if binding.RecordID == "" || binding.RecordID <= previous || binding.Evidence.ProjectID != actor.ProjectID {
			return records.ErrRecordRevisionConflict
		}
		ids = append(ids, binding.RecordID)
		previous = binding.RecordID
	}
	prepared, err := repository.authorization.prepareCurrentAuthorization(ctx, ids)
	if err != nil {
		return err
	}
	kind, ok := recordSubjectSourceKind(destination.Kind)
	if !ok {
		return ErrRecordSubjectUnavailable
	}
	authority, err := repository.witness.Prefetch(ctx, []sourceretirement.SourceKey{
		repository.witness.sourceKey(actor.ProjectID, kind, destination.SourceID),
	})
	if err != nil {
		return err
	}
	return repository.platform.runSourceAuthorizedTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		// Protected membership row acquisition requires the comparison fence at
		// rank 5, before the actor and group rows acquired at rank 10.
		if err := lockComparisonDecisionForWriter(ctx, transaction.tx); err != nil {
			return err
		}
		// Lock the current actor route at rank 10, before waiting on any record
		// fence. The table lock covers membership rows that do not yet exist.
		resources, err := discoverEffectRecordResources(ctx, transaction.tx, nil, []string{actor.UserID})
		if err != nil {
			var missing *recordMutationPrincipalMissing
			if errors.As(err, &missing) {
				return recordauth.ErrDenied
			}
			return err
		}
		declared, err := normalizeRecordMutationDeclarations(resources)
		if err != nil {
			return err
		}
		if err := acquireRecordMutationResourceRange(ctx, transaction.tx, declared, 10, 10); err != nil {
			return err
		}
		if _, err := transaction.tx.Exec(ctx, `lock table public.record_access_group_members in share mode nowait`); err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) && postgresError.Code == "55P03" {
				return ErrRecordSubjectUnavailable
			}
			return err
		}
		for _, binding := range current {
			if err := assertRecordReadFence(ctx, transaction.tx, binding.RecordID); err != nil {
				return err
			}
		}
		for _, binding := range current {
			var recordID string
			err := transaction.tx.QueryRow(ctx, `
				select roots.record_id from public.records roots
				where roots.project_id = $1 and roots.record_id = $2
				  and roots.current_revision_id = $3 and roots.lock_version = $4
				  and roots.authorization_epoch = $5 and roots.lifecycle = $6
				for share of roots`, string(actor.ProjectID), binding.RecordID,
				binding.CurrentRevisionID, binding.LockVersion, binding.AuthorizationEpoch, string(binding.Lifecycle)).Scan(&recordID)
			if errors.Is(err, pgx.ErrNoRows) {
				return records.ErrRecordRevisionConflict
			}
			if err != nil {
				return err
			}
		}
		member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, transaction.tx, actor.ProjectID, actor.UserID)
		if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
			return recordauth.ErrDenied
		}
		if err != nil {
			return ErrRecordSubjectUnavailable
		}
		if member.CanonicalHash() != actor.CanonicalHash() {
			return recordauth.ErrDenied
		}
		resolver, err := revisionTransactionSubjectResolver(transaction.tx, authority)
		if err != nil {
			return err
		}
		live, err := resolver.live.Resolve(ctx, member, destination)
		if err != nil {
			return err
		}
		resolved, err := resolver.Resolve(ctx, member, RecordSubjectReadInput{
			Reference: destination, IdentitySnapshot: live.IdentitySnapshot, CaptureAuthorization: live.CaptureAuthorization,
		})
		if err != nil {
			return err
		}
		if resolved.CaptureAuthorization.State != recordauth.SourceStateLive || resolved.CaptureAuthorization.Digest != live.CaptureAuthorization.Digest {
			return ErrRecordSubjectUnavailable
		}
		values, err := records.ImportedRevisionValues(member, "", "")
		if err != nil {
			return err
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityImport, records.RecordAuthorizationEvidence{
			ProjectID: resolved.ProjectID, Visibility: values.VisibilityScope, Sources: []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
		}); err != nil {
			return err
		}
		for _, binding := range current {
			reloaded, err := prepared.resolveCurrentAuthorizationInTransaction(ctx, transaction.tx, member, binding.RecordID)
			if err != nil {
				return err
			}
			if reloaded.RecordID != binding.RecordID || reloaded.CurrentRevisionID != binding.CurrentRevisionID ||
				reloaded.LockVersion != binding.LockVersion || reloaded.AuthorizationEpoch != binding.AuthorizationEpoch || reloaded.Lifecycle != binding.Lifecycle {
				return records.ErrRecordRevisionConflict
			}
			if err := records.AuthorizeRecordResource(member, recordauth.CapabilityRecordRead, reloaded.Evidence); err != nil {
				return err
			}
		}
		return nil
	})
}

func (repository *PostgresRecordPortabilityRepository) AdvanceImportJob(
	ctx context.Context,
	input AdvanceRecordImportJobInput,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || input.ImportJobID == "" || input.LockVersion == 0 {
		return ErrRecordPortabilityUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		tag, err := transaction.tx.Exec(ctx, `
			update public.record_import_jobs
			set job_state = $1, failure_code = $2, lock_version = lock_version + 1, updated_at = now()
			where import_job_id = $3 and lock_version = $4
		`, input.JobState, input.FailureCode, input.ImportJobID, input.LockVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrRecordImportCASConflict
		}
		return nil
	})
}

func (repository *PostgresRecordPortabilityRepository) LoadOriginTombstone(
	ctx context.Context,
	digest [32]byte,
) (RecordOriginTombstone, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return RecordOriginTombstone{}, ErrRecordPortabilityUnavailable
	}
	var found [32]byte
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var raw []byte
		if err := transaction.tx.QueryRow(ctx, `
			select origin_digest from public.record_origin_tombstones
			where project_id = $1 and origin_digest = $2
		`, recordauth.ProjectIDDefault, digest[:]).Scan(&raw); err != nil {
			return err
		}
		copy(found[:], raw)
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordOriginTombstone{}, ErrRecordImportNotFound
		}
		return RecordOriginTombstone{}, err
	}
	return RecordOriginTombstone{OriginDigest: found}, nil
}

func (repository *PostgresRecordPortabilityRepository) LoadOrigin(
	ctx context.Context,
	digest [32]byte,
) (RecordOrigin, error) {
	if ctx == nil || repository == nil || repository.platform == nil || digest == [32]byte{} {
		return RecordOrigin{}, ErrRecordPortabilityUnavailable
	}
	var origin RecordOrigin
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var raw []byte
		if err := loadOriginOnTx(ctx, transaction, digest, &origin, &raw); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordOrigin{}, ErrRecordImportNotFound
		}
		return RecordOrigin{}, err
	}
	return origin, nil
}

func loadOriginOnTx(
	ctx context.Context,
	transaction *RecordPlatformTransaction,
	digest [32]byte,
	origin *RecordOrigin,
	raw *[]byte,
) error {
	if err := transaction.tx.QueryRow(ctx, `
		select origin_id, origin_digest from public.record_origins
		where project_id = $1 and origin_digest = $2
	`, recordauth.ProjectIDDefault, digest[:]).Scan(&origin.OriginID, raw); err != nil {
		return err
	}
	if len(*raw) != sha256.Size {
		return ErrRecordPortabilityUnavailable
	}
	copy(origin.OriginDigest[:], *raw)
	return nil
}

func (repository *PostgresRecordPortabilityRepository) InsertOrigin(
	ctx context.Context,
	input InsertRecordOriginInput,
) (RecordOrigin, error) {
	if ctx == nil || repository == nil || repository.platform == nil || input.OriginDigest == [32]byte{} {
		return RecordOrigin{}, ErrRecordPortabilityUnavailable
	}
	originID, err := ids.New("ror")
	if err != nil {
		return RecordOrigin{}, ErrRecordPortabilityUnavailable
	}
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		return insertOriginOnTx(ctx, transaction, originID, input)
	})
	if err != nil {
		return RecordOrigin{}, err
	}
	return RecordOrigin{OriginID: originID, OriginDigest: input.OriginDigest}, nil
}

func assertOriginAvailableOnTx(
	ctx context.Context,
	transaction *RecordPlatformTransaction,
	digest [32]byte,
) error {
	var origin RecordOrigin
	var raw []byte
	err := loadOriginOnTx(ctx, transaction, digest, &origin, &raw)
	if err == nil {
		return ErrRecordOriginConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func insertOriginOnTx(
	ctx context.Context,
	transaction *RecordPlatformTransaction,
	originID string,
	input InsertRecordOriginInput,
) error {
	_, err := transaction.tx.Exec(ctx, `
		insert into public.record_origins (
			origin_id, project_id, origin_kind, origin_digest, source_record_id, created_at
		) values ($1, $2, $3, $4, $5, now())
	`, originID, recordauth.ProjectIDDefault, input.OriginKind, input.OriginDigest[:], nullableSourceRecord(input.SourceRecord))
	return mapOriginInsertError(err)
}

func lockImportJobForApplyOnTx(
	ctx context.Context,
	transaction *RecordPlatformTransaction,
	finish records.RevisionCommitFinish,
) error {
	var state, actorID string
	var lockVersion int64
	if err := transaction.tx.QueryRow(ctx, `
		select job_state, actor_id, lock_version
		from public.record_import_jobs
		where import_job_id = $1
		for update
	`, finish.ImportJobID).Scan(&state, &actorID, &lockVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRecordImportNotFound
		}
		return err
	}
	if state != RecordImportJobStatePlanned || actorID != finish.Actor.UserID || uint64(lockVersion) != finish.JobLockVersion {
		return ErrRecordImportCASConflict
	}
	return nil
}

func markImportJobAppliedOnTx(
	ctx context.Context,
	transaction *RecordPlatformTransaction,
	finish records.RevisionCommitFinish,
) error {
	tag, err := transaction.tx.Exec(ctx, `
		update public.record_import_jobs
		set job_state = $1, failure_code = '', lock_version = lock_version + 1, updated_at = now()
		where import_job_id = $2 and lock_version = $3 and job_state = $4 and actor_id = $5
	`, RecordImportJobStateApplied, finish.ImportJobID, finish.JobLockVersion,
		RecordImportJobStatePlanned, finish.Actor.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrRecordImportCASConflict
	}
	return nil
}

func mapOriginInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrRecordOriginConflict
	}
	return err
}

func (repository *PostgresRecordPortabilityRepository) LoadImportJob(
	ctx context.Context,
	importJobID string,
) (RecordImportJob, error) {
	if ctx == nil || repository == nil || repository.platform == nil || importJobID == "" {
		return RecordImportJob{}, ErrRecordPortabilityUnavailable
	}
	var job RecordImportJob
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var digest []byte
		var destinationKind, destinationSourceID *string
		if err := transaction.tx.QueryRow(ctx, `
			select import_job_id, actor_id, job_state, lock_version, archive_digest,
			       destination_subject_kind, destination_subject_source_id, expires_at
			from public.record_import_jobs
			where import_job_id = $1
		`, importJobID).Scan(
			&job.ImportJobID, &job.ActorID, &job.JobState, &job.LockVersion, &digest,
			&destinationKind, &destinationSourceID, &job.ExpiresAt,
		); err != nil {
			return err
		}
		if len(digest) != sha256.Size {
			return ErrRecordPortabilityUnavailable
		}
		destination, err := decodeImportDestination(destinationKind, destinationSourceID)
		if err != nil {
			return err
		}
		copy(job.ArchiveDigest[:], digest)
		job.DestinationSubject = destination
		return attachImportPlanID(ctx, transaction, &job)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordImportJob{}, ErrRecordImportNotFound
		}
		return RecordImportJob{}, err
	}
	return job, nil
}

func (repository *PostgresRecordPortabilityRepository) PublishImportArtifact(
	ctx context.Context,
	input PublishRecordImportArtifactInput,
) (RecordImportArtifact, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		repository.newImportArtifactID == nil || input.ImportJobID == "" ||
		input.ArtifactRole != "archive" ||
		(input.BackendKind != "local" && input.BackendKind != "s3") ||
		input.BlobKey == "" || input.ObjectVersionID == "" || input.ObligationID == "" ||
		input.ByteSize == 0 || input.SHA256 == [32]byte{} || input.ExpiresAt.IsZero() {
		return RecordImportArtifact{}, ErrRecordPortabilityUnavailable
	}
	if repository.pool == nil {
		return RecordImportArtifact{}, ErrRecordPortabilityUnavailable
	}
	var replay bool
	if err := repository.pool.QueryRow(ctx, `select exists(select 1 from public.record_import_artifacts where import_job_id=$1 and artifact_role='archive')`, input.ImportJobID).Scan(&replay); err != nil {
		return RecordImportArtifact{}, err
	}
	var prepared *preparedImportWrite
	var dispatch attachments.WriteDispatch
	if !replay {
		dispatch.ObligationID = input.ObligationID
		if err := repository.pool.QueryRow(ctx, `select writer_instance::text from public.record_write_obligations where obligation_id=$1 and origin_kind='import_archive' and origin_id=$2`, input.ObligationID, input.ImportJobID).Scan(&dispatch.WriterInstance); err != nil {
			return RecordImportArtifact{}, err
		}
		local := &PostgresLocalBlobRepository{pool: repository.pool, witness: repository.witness}
		var err error
		prepared, err = local.prepareImportWrite(ctx, dispatch)
		if err != nil {
			return RecordImportArtifact{}, err
		}
		if prepared == nil {
			return RecordImportArtifact{}, ErrRecordPortabilityUnavailable
		}
	}
	var published RecordImportArtifact
	run := func(ctx context.Context, transaction *RecordPlatformTransaction, proof []byte) error {
		published = RecordImportArtifact{}
		if err := lockAttachmentWriterPrefix(ctx, transaction.tx, attachmentWriterRoute{
			obligationIDs: []string{input.ObligationID}, namespaces: []string{input.NamespaceID},
		}); err != nil {
			return err
		}
		var exists bool
		if err := transaction.tx.QueryRow(ctx, `select exists(select 1 from public.record_import_artifacts where import_job_id=$1 and artifact_role='archive')`, input.ImportJobID).Scan(&exists); err != nil {
			return err
		}
		if replay && !exists {
			return importWriteChanged()
		}
		if !exists {
			if err := prepared.acquireRoute(ctx, transaction.tx, dispatch, proof, false); err != nil {
				return err
			}
		}
		artifactID, err := repository.newImportArtifactID()
		if err != nil || artifactID == "" {
			return ErrRecordPortabilityUnavailable
		}
		artifact := RecordImportArtifact{
			ArtifactID: artifactID, ImportJobID: input.ImportJobID, ArtifactRole: input.ArtifactRole,
			BackendKind: input.BackendKind, BlobKey: input.BlobKey, ObjectVersionID: input.ObjectVersionID,
			NamespaceID: input.NamespaceID, ObligationID: input.ObligationID,
			SHA256: input.SHA256, ByteSize: input.ByteSize, ExpiresAt: input.ExpiresAt.UTC().Truncate(time.Microsecond),
		}
		command, err := json.Marshal(artifactPublicationCommand{
			ArtifactID: artifact.ArtifactID, JobID: artifact.ImportJobID, Kind: artifact.ArtifactRole,
			BackendKind: artifact.BackendKind, NamespaceID: artifact.NamespaceID,
			Key: artifact.BlobKey, Version: artifact.ObjectVersionID, SHA256: hex.EncodeToString(artifact.SHA256[:]),
			Size: artifact.ByteSize, ExpiresAt: artifact.ExpiresAt, ObligationID: artifact.ObligationID,
		})
		if err != nil {
			return err
		}
		if err := transaction.tx.QueryRow(ctx, `select import_artifact_id from public.record_import_artifact_publish($1)`, command).
			Scan(&artifact.ArtifactID); err != nil {
			return err
		}
		published = artifact
		return nil
	}
	var err error
	if replay {
		err = repository.platform.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			return run(ctx, transaction, nil)
		})
	} else {
		err = repository.platform.runRecordBirthMutationTransaction(ctx, repository.continuity, run)
	}
	if err != nil {
		return RecordImportArtifact{}, err
	}
	return published, nil
}

func (repository *PostgresRecordPortabilityRepository) LoadImportArtifact(
	ctx context.Context,
	importJobID string,
) (RecordImportArtifact, error) {
	if ctx == nil || repository == nil || repository.platform == nil || importJobID == "" {
		return RecordImportArtifact{}, ErrRecordPortabilityUnavailable
	}
	var artifact RecordImportArtifact
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var digest []byte
		var byteSize int64
		if err := transaction.tx.QueryRow(ctx, `
			select import_artifact_id, import_job_id, artifact_role, backend_kind, blob_key,
			       object_version_id, sha256, byte_size, expires_at, coalesce(namespace_id::text, ''), coalesce(obligation_id, '')
			from public.record_import_artifacts
			where import_job_id = $1 and artifact_role = 'archive'
		`, importJobID).Scan(
			&artifact.ArtifactID, &artifact.ImportJobID, &artifact.ArtifactRole, &artifact.BackendKind,
			&artifact.BlobKey, &artifact.ObjectVersionID, &digest, &byteSize, &artifact.ExpiresAt,
			&artifact.NamespaceID, &artifact.ObligationID,
		); err != nil {
			return err
		}
		if byteSize <= 0 || len(digest) != sha256.Size {
			return ErrRecordPortabilityUnavailable
		}
		artifact.ByteSize = uint64(byteSize)
		copy(artifact.SHA256[:], digest)
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordImportArtifact{}, ErrRecordImportNotFound
		}
		return RecordImportArtifact{}, err
	}
	return artifact, nil
}

func nullableSourceRecord(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func sha256SumString(value string) [32]byte {
	return sha256.Sum256([]byte(value))
}

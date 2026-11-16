package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

type importWriteMapping struct {
	Kind, SourceID, TargetID, AllocationID, OwnerRecordID string
	SourceDigest                                          []byte
}

type importWriteTarget struct {
	RecordID, AttachmentID string
	PlanDigest             []byte
	RecordEpoch            *int64
	BoundAt, HandedOffAt   *time.Time
}

type importWriteRoute struct {
	ProjectID, JobID, ActorID, State, OwnerID string
	Version, OwnerGeneration                  int64
	LeaseExpiresAt                            *time.Time
	ExpiresAt                                 time.Time
	ArchiveDigest                             []byte
	Destination                               records.SubjectReference
	PlanID                                    string
	PlanDigest                                []byte
	PlanExpiresAt                             *time.Time
	ObjectCount, RemapCount                   *int64
	Mappings                                  []importWriteMapping
	Targets                                   []importWriteTarget
}

type preparedImportWrite struct {
	obligationID string
	route        importWriteRoute
	allocations  []records.RevisionMutationAllocation
	authority    *PreparedSourceAuthority
}

// A replay is an observation of an already committed identity, not permission
// to send again. The restricted command still checks the exact principal and
// command/dispatch identity, including conflicts.
func managedWriteAlreadyRecorded(ctx context.Context, db currentRecordAuthorizationDB, request any) (bool, error) {
	switch command := request.(type) {
	case attachments.WriteRegistration:
		var exists bool
		err := db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations where obligation_id=$1)`, command.ObligationID).Scan(&exists)
		return exists, err
	case attachments.WriteDispatch:
		var dispatched bool
		err := db.QueryRow(ctx, `select dispatch_id is not null from public.record_write_obligations where obligation_id=$1`, command.ObligationID).Scan(&dispatched)
		return dispatched, err
	default:
		return false, nil
	}
}

// Discovery and W2 observation occur before the writer transaction. All of
// these facts are candidates; the admitted transaction rechecks the complete
// route after acquiring its canonical resource set.
func (repository *PostgresLocalBlobRepository) prepareImportWrite(ctx context.Context, request any) (*preparedImportWrite, error) {
	var registration attachments.WriteRegistration
	var obligationID string
	switch command := request.(type) {
	case attachments.WriteRegistration:
		if command.Kind != "import_archive" && command.Kind != "import_attachment" {
			return nil, nil
		}
		registration = command
	case attachments.WriteDispatch:
		obligationID = command.ObligationID
		var raw []byte
		var binding *string
		if err := repository.pool.QueryRow(ctx, `select registration_command,encode(storage_binding,'hex') from public.record_write_obligations where obligation_id=$1 and writer_instance::text=$2`, command.ObligationID, command.WriterInstance).Scan(&raw, &binding); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &registration); err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(registration.StorageBinding, binding) {
			return nil, importWriteChanged()
		}
		if registration.Kind != "import_archive" && registration.Kind != "import_attachment" {
			return nil, nil
		}
	default:
		return nil, nil
	}
	if repository.witness == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	route, err := readImportWriteRoute(ctx, repository.pool, registration.ID, obligationID)
	if err != nil {
		return nil, err
	}
	if err := validateImportWriteRoute(registration, route, obligationID != ""); err != nil {
		return nil, err
	}
	prepared := &preparedImportWrite{route: route, obligationID: obligationID}
	var allocationTx pgx.Tx
	for _, mapping := range route.Mappings {
		if mapping.Kind != "record" {
			continue
		}
		if allocationTx == nil {
			allocationTx, err = repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				return nil, err
			}
			defer func() { _ = allocationTx.Rollback(ctx) }()
		}
		command, err := json.Marshal(struct {
			ProjectID    string `json:"project_id"`
			ActorID      string `json:"actor_id"`
			AllocationID string `json:"allocation_id"`
		}{route.ProjectID, route.ActorID, mapping.AllocationID})
		if err != nil {
			return nil, err
		}
		var raw []byte
		if err := allocationTx.QueryRow(ctx, `select public.record_birth_allocation_get($1)`, command).Scan(&raw); err != nil {
			return nil, err
		}
		var projection birthAllocationProjection
		if err := decodeBirthJSON(raw, &projection); err != nil {
			return nil, err
		}
		allocation, err := projection.allocation()
		if err != nil || allocation.AllocationID != mapping.AllocationID || allocation.RecordID != mapping.TargetID ||
			allocation.Request.ProjectID != route.ProjectID || allocation.Request.ActorID != route.ActorID || allocation.Request.Purpose != recordbirth.PurposeImport {
			return nil, ErrRecordImportCASConflict
		}
		prepared.allocations = append(prepared.allocations, records.RevisionMutationAllocation{
			AllocationID: allocation.AllocationID, Request: allocation.Request, RecordID: allocation.RecordID,
		})
	}
	if allocationTx != nil {
		if err := allocationTx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	sourceKind, ok := recordSubjectSourceKind(route.Destination.Kind)
	if !ok {
		return nil, ErrRecordSubjectUnavailable
	}
	key := repository.witness.sourceKey(recordauth.ProjectID(route.ProjectID), sourceKind, route.Destination.SourceID)
	prepared.authority, err = repository.witness.Prefetch(ctx, []sourceretirement.SourceKey{key})
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func readImportWriteRoute(ctx context.Context, db currentRecordAuthorizationDB, jobID, obligationID string) (importWriteRoute, error) {
	var route importWriteRoute
	var kind, sourceID *string
	err := db.QueryRow(ctx, `select j.project_id,j.import_job_id,j.actor_id,j.job_state,j.lock_version,
		j.owner_id,j.owner_generation,j.lease_expires_at,j.expires_at,j.archive_digest,
		j.destination_subject_kind,j.destination_subject_source_id,
		coalesce(p.import_plan_id,''),p.plan_digest,p.expires_at,p.object_count,p.remap_count
		from public.record_import_jobs j left join public.record_import_plans p on p.import_job_id=j.import_job_id
		where j.import_job_id=$1`, jobID).Scan(&route.ProjectID, &route.JobID, &route.ActorID, &route.State, &route.Version,
		&route.OwnerID, &route.OwnerGeneration, &route.LeaseExpiresAt, &route.ExpiresAt, &route.ArchiveDigest,
		&kind, &sourceID, &route.PlanID, &route.PlanDigest, &route.PlanExpiresAt, &route.ObjectCount, &route.RemapCount)
	if err != nil {
		return route, err
	}
	route.Destination, err = decodeImportDestination(kind, sourceID)
	if err != nil {
		return route, err
	}
	if route.PlanID != "" {
		rows, err := db.Query(ctx, `select entity_kind,source_id,source_identity_digest,target_id,
			coalesce(birth_allocation_id,''),coalesce(owner_target_record_id,'')
			from public.record_import_entity_mappings where import_plan_id=$1
			order by entity_kind collate "C",source_identity_digest`, route.PlanID)
		if err != nil {
			return route, err
		}
		for rows.Next() {
			var mapping importWriteMapping
			if err := rows.Scan(&mapping.Kind, &mapping.SourceID, &mapping.SourceDigest, &mapping.TargetID, &mapping.AllocationID, &mapping.OwnerRecordID); err != nil {
				rows.Close()
				return route, err
			}
			route.Mappings = append(route.Mappings, mapping)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return route, err
		}
	}
	if obligationID != "" {
		rows, err := db.Query(ctx, `select record_id,coalesce(attachment_id,''),plan_digest,record_epoch,bound_at,handed_off_at
			from public.record_write_obligation_targets where obligation_id=$1 order by record_id collate "C"`, obligationID)
		if err != nil {
			return route, err
		}
		for rows.Next() {
			var target importWriteTarget
			if err := rows.Scan(&target.RecordID, &target.AttachmentID, &target.PlanDigest, &target.RecordEpoch, &target.BoundAt, &target.HandedOffAt); err != nil {
				rows.Close()
				return route, err
			}
			route.Targets = append(route.Targets, target)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return route, err
		}
	}
	return route, nil
}

func validateImportWriteRoute(registration attachments.WriteRegistration, route importWriteRoute, dispatch bool) error {
	if route.ProjectID != registration.ProjectID || route.JobID != registration.ID || route.Version != registration.Generation ||
		registration.ArchiveDigest == nil || hex.EncodeToString(route.ArchiveDigest) != *registration.ArchiveDigest {
		return ErrRecordImportCASConflict
	}
	if registration.Kind == "import_archive" {
		if route.State != "uploaded" && route.State != "quarantined" && route.State != "dry_run" {
			return ErrRecordImportCASConflict
		}
	} else if registration.Kind == "import_attachment" {
		if route.State != "planned" || registration.PlanDigest == nil || registration.TargetRecordID == nil || registration.TargetAttachmentID == nil ||
			route.PlanID == "" || hex.EncodeToString(route.PlanDigest) != *registration.PlanDigest {
			return ErrRecordImportCASConflict
		}
		recordsMatched, attachmentsMatched := 0, 0
		for _, mapping := range route.Mappings {
			if mapping.Kind == "record" && mapping.TargetID == *registration.TargetRecordID && mapping.AllocationID != "" {
				recordsMatched++
			}
			if mapping.Kind == "attachment" && mapping.TargetID == *registration.TargetAttachmentID && mapping.OwnerRecordID == *registration.TargetRecordID {
				attachmentsMatched++
			}
		}
		if recordsMatched != 1 || attachmentsMatched != 1 {
			return ErrRecordImportCASConflict
		}
	} else {
		return ErrRecordImportCASConflict
	}
	recordsSeen := make(map[string]struct{})
	allocationsSeen := make(map[string]struct{})
	for _, mapping := range route.Mappings {
		if mapping.Kind != "record" {
			continue
		}
		if mapping.AllocationID == "" {
			return ErrRecordImportCASConflict
		}
		if _, exists := recordsSeen[mapping.TargetID]; exists {
			return ErrRecordImportCASConflict
		}
		if _, exists := allocationsSeen[mapping.AllocationID]; exists {
			return ErrRecordImportCASConflict
		}
		recordsSeen[mapping.TargetID] = struct{}{}
		allocationsSeen[mapping.AllocationID] = struct{}{}
	}
	if dispatch {
		if registration.Kind == "import_archive" && len(route.Targets) != len(recordsSeen) {
			return ErrRecordImportCASConflict
		}
		if registration.Kind == "import_attachment" && len(route.Targets) != 1 {
			return ErrRecordImportCASConflict
		}
		for _, target := range route.Targets {
			if target.RecordEpoch != nil || target.BoundAt != nil || target.HandedOffAt != nil || !bytes.Equal(target.PlanDigest, route.PlanDigest) {
				return ErrRecordImportCASConflict
			}
			if _, ok := recordsSeen[target.RecordID]; !ok {
				return ErrRecordImportCASConflict
			}
			if registration.Kind == "import_archive" && target.AttachmentID != "" {
				return ErrRecordImportCASConflict
			}
			if registration.Kind == "import_attachment" && (target.RecordID != *registration.TargetRecordID || target.AttachmentID != *registration.TargetAttachmentID) {
				return ErrRecordImportCASConflict
			}
		}
	}
	return nil
}

func importWriteChanged() error {
	return &pgconn.PgError{Code: "40001", Message: "import write route changed; restart with source prefetch"}
}

func (prepared *preparedImportWrite) acquireAndAuthorize(ctx context.Context, tx pgx.Tx, request any, proof []byte) error {
	if err := prepareManagedWriteMutation(ctx, tx, request); err != nil {
		return err
	}
	if replayed, err := managedWriteAlreadyRecorded(ctx, tx, request); err != nil || replayed {
		return err
	}
	return prepared.acquireRoute(ctx, tx, request, proof, true)
}

// Artifact publication uses the same persisted import route after its own
// namespace/graph prefix, but a dispatched obligation is not a publication replay.
func (prepared *preparedImportWrite) acquireRoute(ctx context.Context, tx pgx.Tx, request any, proof []byte, allowWriteReplay bool) error {
	route, err := readImportWriteRoute(ctx, tx, prepared.route.JobID, prepared.obligationID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(route, prepared.route) {
		return importWriteChanged()
	}
	var recordIDs []string
	for _, allocation := range prepared.allocations {
		recordIDs = append(recordIDs, allocation.RecordID)
	}
	before, err := discoverEffectRecordResources(ctx, tx, recordIDs, []string{route.ActorID})
	if err != nil {
		return err
	}
	graphBefore := before
	before.Sources = append(before.Sources, records.RevisionMutationSource{ProjectID: route.ProjectID, SourceKind: route.Destination.Kind, SourceID: route.Destination.SourceID})
	before.Allocations = append(before.Allocations, prepared.allocations...)
	before.ImportJobs = append(before.ImportJobs, records.RevisionMutationImportJob{
		ProjectID: route.ProjectID, JobID: route.JobID, ActorID: route.ActorID, State: route.State,
		LockVersion: route.Version, OwnerID: route.OwnerID, OwnerGeneration: route.OwnerGeneration,
	})
	before.BlobTables = append(before.BlobTables, records.RevisionMutationBlobMetadataTable, records.RevisionMutationUploadPartsTable)
	if dispatch, ok := request.(attachments.WriteDispatch); ok {
		before.WriteObligations, err = readAttachmentWriteObligations(ctx, tx, []string{dispatch.ObligationID})
		if err != nil {
			return err
		}
		if len(before.WriteObligations) != 1 {
			return pgx.ErrNoRows
		}
	}
	declared, err := normalizeRecordMutationDeclarations(before)
	if err != nil {
		return err
	}
	declared.continuityProof = proof
	if err := verifyRevisionPrefetchedRoutes(prepared.authority, declared); err != nil {
		return err
	}
	if err := acquireRecordMutationResourceRange(ctx, tx, declared, 10, 10); err != nil {
		return err
	}
	// A missing membership row cannot be locked. Runtime may insert one
	// directly, so fence the complete membership set before higher ranks and
	// rediscover it below. NOWAIT prevents a backwards wait on a concurrent
	// membership writer that already holds a row acquired above.
	if _, err := tx.Exec(ctx, `lock table public.record_access_group_members in share mode nowait`); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "55P03" {
			return importWriteChanged()
		}
		return err
	}
	if err := acquireRecordMutationResourceRange(ctx, tx, declared, 20, 75); err != nil {
		return err
	}
	if allowWriteReplay {
		// A concurrent exact replay needs no new permission; SQL checks its identity.
		replayed, err := managedWriteAlreadyRecorded(ctx, tx, request)
		if err != nil || replayed {
			return err
		}
	}
	afterRoute, err := readImportWriteRoute(ctx, tx, route.JobID, prepared.obligationID)
	if err != nil || !reflect.DeepEqual(route, afterRoute) {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return importWriteChanged()
	}
	if dispatch, ok := request.(attachments.WriteDispatch); ok {
		obligations, err := readAttachmentWriteObligations(ctx, tx, []string{dispatch.ObligationID})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before.WriteObligations, obligations) {
			return importWriteChanged()
		}
	}
	after, err := discoverEffectRecordResources(ctx, tx, recordIDs, []string{route.ActorID})
	if err != nil {
		return err
	}
	for _, root := range after.RootObservations {
		if root.Exists {
			return ErrRecordImportCASConflict
		}
	}
	if !reflect.DeepEqual(graphBefore, after) {
		return importWriteChanged()
	}
	member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, tx, recordauth.ProjectID(route.ProjectID), route.ActorID)
	if err != nil {
		return recordauth.ErrDenied
	}
	for _, allocation := range prepared.allocations {
		if allocation.Request.ActorScopeDigest != member.CanonicalHash() {
			return recordauth.ErrDenied
		}
	}
	resolver, err := revisionTransactionSubjectResolver(tx, prepared.authority)
	if err != nil {
		return err
	}
	live, err := resolver.live.Resolve(ctx, member, route.Destination)
	if err != nil {
		return err
	}
	resolved, err := resolver.Resolve(ctx, member, RecordSubjectReadInput{
		Reference: route.Destination, IdentitySnapshot: live.IdentitySnapshot, CaptureAuthorization: live.CaptureAuthorization,
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
	return records.AuthorizeRecordResource(member, recordauth.CapabilityImport, records.RecordAuthorizationEvidence{
		ProjectID: resolved.ProjectID, Visibility: values.VisibilityScope, Sources: []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
	})
}

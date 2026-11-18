package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// acquireRecordMutationResources executes the closed declaration after source,
// asset-graph, namespace and comparison-fence acquisition. It does not claim
// absent idempotency keys or authorize the observations supplied by a caller.
// The caller must rediscover and compare its whole route after acquisition.
func acquireRecordMutationResources(ctx context.Context, tx pgx.Tx, resources recordMutationResources) error {
	return acquireRecordMutationResourceRange(ctx, tx, resources, 10, 75)
}

// Call these two phases around the actual sorted idempotency claims. The
// second phase locks existing claim rows again before acquiring job/effect rows.
func acquireRecordMutationResourcesBeforeClaims(ctx context.Context, tx pgx.Tx, resources recordMutationResources) error {
	if err := acquireRecordMutationResourceRange(ctx, tx, resources, 10, 70); err != nil {
		return err
	}
	return acquireRecordMutationIdempotencyScopes(ctx, tx, resources)
}

func acquireRecordMutationResourcesAfterClaims(ctx context.Context, tx pgx.Tx, resources recordMutationResources) error {
	return acquireRecordMutationResourceRange(ctx, tx, resources, 71, 75)
}

// Terminal outcomes may report a real late result after a gate closes. This
// phase never initializes a missing epoch and grants no content/send authority.
// The caller must still validate the exact terminal owner and observed lease.
func acquireTerminalRecordMutationResources(ctx context.Context, tx pgx.Tx, resources recordMutationResources) error {
	return acquireRecordMutationResourceRangeMode(ctx, tx, resources, 10, 60, true)
}

func acquireRecordMutationIdempotencyScopes(ctx context.Context, tx pgx.Tx, resources recordMutationResources) error {
	type physicalScope struct {
		key  recordplatform.IdempotencyKey
		hash int32
	}
	if len(resources.resources.Idempotency) == 0 {
		return nil
	}
	scopes := make([]physicalScope, len(resources.resources.Idempotency))
	frames := make([]string, len(scopes))
	for index, item := range resources.resources.Idempotency {
		frame, err := recordIdempotencyScopeFrame(item.Key)
		if err != nil {
			return err
		}
		frames[index], scopes[index].key = frame, item.Key
	}
	rows, err := tx.Query(ctx, `select pg_catalog.hashtext(value) from unnest($1::text[]) with ordinality as keys(value,ordinal) order by ordinal`, frames)
	if err != nil {
		return fmt.Errorf("order record idempotency scopes: %w", err)
	}
	index := 0
	for rows.Next() {
		if index >= len(scopes) {
			rows.Close()
			return records.ErrInvalidRevisionMutationResources
		}
		if err := rows.Scan(&scopes[index].hash); err != nil {
			rows.Close()
			return fmt.Errorf("read record idempotency scope: %w", err)
		}
		index++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read record idempotency scopes: %w", err)
	}
	if index != len(scopes) {
		return records.ErrInvalidRevisionMutationResources
	}
	// Different full keys may have the same physical advisory hash. Sorting
	// that physical identity prevents opposite lock order across collisions.
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].hash < scopes[j].hash })
	for index, scope := range scopes {
		if index > 0 && scopes[index-1].hash == scope.hash {
			continue
		}
		if err := lockRecordIdempotencyScope(ctx, tx, scope.key); err != nil {
			return err
		}
	}
	// All physical scopes are held. Exact row identities retain the existing
	// canonical compound-key ordering and fingerprint checks.
	for _, item := range resources.resources.Idempotency {
		if _, err := tx.Exec(ctx, `select 1 from public.record_idempotency_keys where project_id=$1 and operation_kind=$2 and idempotency_key=$3 for update`, item.Key.ProjectID, item.Key.OperationKind, item.Key.Key); err != nil {
			return fmt.Errorf("lock record idempotency row: %w", err)
		}
	}
	return nil
}

// Revision batches interleave their actual root creation and idempotency claim
// with the fixed ranks. The bounds are private coordinator phases, never part
// of a resource declaration or a caller-supplied SQL/lock abstraction.
func acquireRecordMutationResourceRange(ctx context.Context, tx pgx.Tx, resources recordMutationResources, firstRank, lastRank uint8) error {
	return acquireRecordMutationResourceRangeMode(ctx, tx, resources, firstRank, lastRank, false)
}

func acquireRecordMutationResourceRangeMode(ctx context.Context, tx pgx.Tx, resources recordMutationResources, firstRank, lastRank uint8, terminal bool) error {
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) {
		return records.ErrInvalidRevisionMutationResources
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var blobFenced bool
	var idempotencyFenced bool
	var lastDependency string
	for _, declaration := range resources.order {
		if declaration.rank < firstRank || declaration.rank > lastRank {
			continue
		}
		var err error
		switch declaration.family {
		case recordMutationUsers:
			item := resources.resources.Users[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.users where user_id=$1 for update`, item.UserID)
		case recordMutationGroups:
			item := resources.resources.Groups[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_access_groups where project_id=$1 and group_id=$2 for update`, item.ProjectID, item.GroupID)
		case recordMutationMemberships:
			item := resources.resources.Memberships[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "membership", "group_id": item.GroupID, "user_id": item.UserID})
		case recordMutationSources:
			item := resources.resources.Sources[declaration.index]
			switch item.SourceKind {
			case records.SubjectKindVPS:
				_, err = tx.Exec(ctx, `select 1 from public.vps_assets where vps_id=$1 for update`, item.SourceID)
			case records.SubjectKindMonitoringInstance:
				_, err = tx.Exec(ctx, `select 1 from public.monitoring_instances where monitoring_instance_id=$1 for update`, item.SourceID)
			case records.SubjectKindTarget:
				_, err = tx.Exec(ctx, `select 1 from public.targets where target_id=$1 for update`, item.SourceID)
			default:
				return records.ErrInvalidRevisionMutationResources
			}
		case recordMutationReservations:
			item := resources.resources.Reservations[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "reservation", "project_id": item.ProjectID, "record_id": item.RecordID, "reservation_id": item.ReservationID})
		case recordMutationEpochScopes:
			item := resources.resources.Records[declaration.index]
			if !terminal {
				_, err = tx.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values($1,'record',$2,0) on conflict(project_id,object_kind,object_id) do nothing`, item.ProjectID, item.RecordID)
			}
			if err == nil {
				var epoch int64
				err = tx.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id=$1 and object_kind='record' and object_id=$2 for update`, item.ProjectID, item.RecordID).Scan(&epoch)
				if terminal && errors.Is(err, pgx.ErrNoRows) {
					return recordplatform.ErrContentDeliveryEpochMissing
				}
				if err == nil && epoch < 0 {
					return records.ErrRecordDeletionReserved
				}
			}
		case recordMutationLeaseScopes:
			item := resources.resources.Records[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "deletion_fence", "project_id": item.ProjectID, "record_id": item.RecordID})
		case recordMutationContentLeaseScopes:
			item := resources.resources.Records[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.object_content_leases where project_id=$1 and object_kind='record' and object_id=$2 for update`, item.ProjectID, item.RecordID)
		case recordMutationRecordGateScopes:
			item := resources.resources.RecordGateScopes[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "gate", "project_id": item.ProjectID, "record_id": item.RecordID})
		case recordMutationBirthContinuity:
			proof := resources.continuityProof
			if proof == nil {
				proof = []byte{}
			}
			_, err = tx.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, proof)
		case recordMutationAllocations:
			item := resources.resources.Allocations[declaration.index]
			command := struct {
				birthRequestCommand
				AllocationID string `json:"allocation_id"`
				RecordID     string `json:"record_id"`
			}{birthRequest(item.Request), item.AllocationID, item.RecordID}
			var data []byte
			data, err = json.Marshal(command)
			if err == nil {
				_, err = tx.Exec(ctx, `select public.record_birth_allocation_lock($1)`, data)
			}
		case recordMutationRoots:
			item := resources.resources.Records[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.records where project_id=$1 and record_id=$2 for update`, item.ProjectID, item.RecordID)
		case recordMutationProjects:
			item := resources.resources.Projects[declaration.index]
			switch item.Kind {
			case records.RevisionMutationEvidenceCapacity:
				_, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, evidenceProjectCapacityLockDomain+item.ProjectID)
			case records.RevisionMutationAttachmentQuota:
				_, _, err = lockAttachmentQuotaAccount(ctx, tx, item.ProjectID)
			case records.RevisionMutationSearchCoordination:
				err = lockRecordSearchCoordination(ctx, tx, item.ProjectID)
			default:
				return records.ErrInvalidRevisionMutationResources
			}
		case recordMutationGenerations:
			item := resources.resources.Generations[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_search_generations where project_id=$1 and generation=$2 for update`, item.ProjectID, item.Generation)
		case recordMutationIdempotency:
			if !idempotencyFenced {
				err = acquireRecordMutationIdempotencyScopes(ctx, tx, resources)
				idempotencyFenced = err == nil
			}
		case recordMutationImportJobs:
			item := resources.resources.ImportJobs[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_import_jobs where project_id=$1 and import_job_id=$2 for update`, item.ProjectID, item.JobID)
		case recordMutationExportJobs:
			item := resources.resources.ExportJobs[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "export_job", "project_id": item.ProjectID, "export_job_id": item.JobID})
		case recordMutationSearchRebuildJobs:
			item := resources.resources.SearchRebuildJobs[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_search_rebuild_jobs where project_id=$1 and job_id=$2 for update`, item.ProjectID, item.JobID)
		case recordMutationDrafts:
			item := resources.resources.Drafts[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_drafts where project_id=$1 and draft_id=$2 for update`, item.ProjectID, item.DraftID)
		case recordMutationAttachments:
			item := resources.resources.Attachments[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_attachments where project_id=$1 and attachment_id=$2 for update`, item.ProjectID, item.AttachmentID)
		case recordMutationUploads:
			item := resources.resources.Uploads[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.attachment_uploads where project_id=$1 and upload_id=$2 for update`, item.ProjectID, item.UploadID)
		case recordMutationWorkspaces:
			item := resources.resources.Workspaces[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.content_processor_workspaces where workspace_id=$1 for update`, item.WorkspaceID)
		case recordMutationJobs:
			item := resources.resources.Jobs[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.attachment_processor_jobs where processor_job_id=$1 for update`, item.ProcessorJobID)
		case recordMutationBlobTables:
			if !blobFenced {
				_, err = tx.Exec(ctx, `select public.record_revision_blob_fence()`)
				blobFenced = err == nil
			}
		case recordMutationBlobs, recordMutationParts, recordMutationImports:
			// The strong table fence covers all existing rows and absent keys in
			// both Blob tables. Do not attempt an unauthorized row UPDATE lock.
			if !blobFenced {
				return fmt.Errorf("%w: missing Blob table fence", records.ErrInvalidRevisionMutationResources)
			}
		case recordMutationWriteObligations:
			if !blobFenced {
				return fmt.Errorf("%w: missing Blob table fence", records.ErrInvalidRevisionMutationResources)
			}
			item := resources.resources.WriteObligations[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "write_obligation", "obligation_id": item.ObligationID})
		case recordMutationPublications:
			if !blobFenced {
				return fmt.Errorf("%w: missing Blob table fence", records.ErrInvalidRevisionMutationResources)
			}
			item := resources.resources.Publications[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.blob_publication_intents where publication_id=$1 for update`, item.PublicationID)
		case recordMutationCaptureIntents:
			item := resources.resources.CaptureIntents[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "evidence_intent", "intent_id": item.IntentID})
		case recordMutationPayloads:
			item := resources.resources.Payloads[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "evidence_payload", "payload_digest": hex.EncodeToString(item.PayloadDigest[:])})
		case recordMutationSnapshots:
			item := resources.resources.Snapshots[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "evidence_snapshot", "snapshot_id": item.SnapshotID})
		case recordMutationLineages:
			item := resources.resources.Lineages[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "evidence_lineage", "snapshot_id": item.SnapshotID})
		case recordMutationDependencies:
			item := resources.resources.Dependencies[declaration.index]
			if item.ResultSnapshotID != lastDependency {
				err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "comparison_dependencies", "result_snapshot_id": item.ResultSnapshotID})
				lastDependency = item.ResultSnapshotID
			}
		case recordMutationCompletions:
			item := resources.resources.Completions[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "comparison_completion", "result_snapshot_id": item.ResultSnapshotID})
		case recordMutationRevisions:
			item := resources.resources.Revisions[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "revision", "record_id": item.RecordID, "revision_id": item.RevisionID})
		case recordMutationRevisionParticipants:
			item := resources.resources.RevisionParticipants[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "revision_participant", "revision_id": item.RevisionID, "user_id": item.UserID})
		case recordMutationFollowers:
			item := resources.resources.Followers[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_followers where project_id=$1 and record_id=$2 and user_id=$3 for update`, item.ProjectID, item.RecordID, item.UserID)
		case recordMutationActions:
			item := resources.resources.Actions[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_actions where project_id=$1 and record_id=$2 and action_id=$3 for update`, item.ProjectID, item.RecordID, item.ActionID)
		case recordMutationDocuments:
			item := resources.resources.Documents[declaration.index]
			_, err = tx.Exec(ctx, `select 1 from public.record_search_documents where project_id=$1 and generation=$2 and record_id=$3 for update`, item.ProjectID, item.Generation, item.RecordID)
		case recordMutationSearchSubjects:
			item := resources.resources.SearchSubjects[declaration.index]
			err = lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "search_subject", "generation": item.Generation, "record_id": item.RecordID, "subject_kind": item.SubjectKind, "source_id": item.SourceID, "relation_role": item.RelationRole})
		case recordMutationNewRevisionEffects:
			// Future effect identities have no existing row to lock. Their
			// record epoch/root and the relevant participant scopes are held.
		default:
			return records.ErrInvalidRevisionMutationResources
		}
		if err != nil {
			return fmt.Errorf("acquire record mutation family %d: %w", declaration.family, err)
		}
	}
	if firstRank <= 30 && lastRank >= 60 {
		return recheckRecordMutationFenceState(ctx, tx, resources.resources, terminal)
	}
	return nil
}

func lockRecordMutationProtectedRow(ctx context.Context, tx pgx.Tx, identity map[string]any) error {
	body, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `select public.record_mutation_rows_lock($1)`, body)
	return err
}

// This is a fresh READ COMMITTED statement sequence after the epoch, lease,
// gate and root ranks. A newly observed earlier reservation requires a whole
// transaction retry; a previously observed active deletion closes the gate.
func recheckRecordMutationFenceState(ctx context.Context, tx pgx.Tx, resources records.RevisionMutationResources, terminal bool) error {
	for _, record := range resources.Records {
		observed, err := readRevisionMutationRows[records.RevisionMutationReservation](ctx, tx, `
			select project_id,object_id,reservation_id,state,fence_epoch,owner_id,owner_generation,coalesce(owner_expires_at,'0001-01-01 00:00:00+00'::timestamptz) from public.deletion_reservations
			where project_id=$1 and object_kind='record' and object_id=$2
			  and state in ('previewed','fenced','committed') order by reservation_id collate "C"`, record.ProjectID, record.RecordID)
		if err != nil {
			return err
		}
		for index := range observed {
			observed[index].OwnerExpiresAt = observed[index].OwnerExpiresAt.UTC()
		}
		expected := make([]records.RevisionMutationReservation, 0)
		for _, reservation := range resources.Reservations {
			if reservation.ProjectID == record.ProjectID && reservation.RecordID == record.RecordID {
				expected = append(expected, reservation)
			}
		}
		if !slices.Equal(expected, observed) {
			return &pgconn.PgError{Code: "40001", Message: "record deletion reservation changed; restart transaction"}
		}
		for _, reservation := range observed {
			if !terminal && reservation.State != "previewed" {
				return records.ErrRecordDeletionReserved
			}
		}
		var blocked bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from public.deletion_fence_leases where project_id=$1 and object_kind='record' and object_id=$2 and expires_at>clock_timestamp()) or exists(select 1 from public.record_deletion_gates where project_id=$1 and record_id=$2)`, record.ProjectID, record.RecordID).Scan(&blocked); err != nil {
			return fmt.Errorf("recheck record mutation fence: %w", err)
		}
		if blocked && !terminal {
			return records.ErrRecordDeletionReserved
		}
		root := records.RevisionMutationRootObservation{ProjectID: record.ProjectID, RecordID: record.RecordID}
		err = tx.QueryRow(ctx, `select coalesce(current_revision_id,''),lifecycle,lock_version,authorization_epoch from public.records where project_id=$1 and record_id=$2`, record.ProjectID, record.RecordID).Scan(&root.CurrentRevisionID, &root.Lifecycle, &root.LockVersion, &root.AuthorizationEpoch)
		if err == nil {
			root.Exists = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("recheck record root: %w", err)
		}
		for _, expectedRoot := range resources.RootObservations {
			if expectedRoot.ProjectID == record.ProjectID && expectedRoot.RecordID == record.RecordID && expectedRoot != root {
				return &pgconn.PgError{Code: "40001", Message: "record root changed; restart transaction"}
			}
		}
	}
	return nil
}

package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// revisionBatchAcquisition holds only acquisition state. Commands still execute
// in input order and reread their root after preceding commands have changed it.
// A claimed key is consumed once; later occurrences reread the already-locked
// completed row, preserving both replay and conflicting-fingerprint behavior.
type revisionBatchAcquisition struct {
	claims          map[recordplatform.IdempotencyKey]recordplatform.IdempotencyClaimResultV1
	created         map[int]bool
	importReplayed  bool
	continuityProof []byte
}

func acquireRevisionBatch(ctx context.Context, transaction *RecordPlatformTransaction, registry records.RevisionParticipantRegistry, authority *PreparedSourceAuthority, commands []records.RevisionCommitCommand, finish *records.RevisionCommitFinish, proof []byte) (*revisionBatchAcquisition, error) {
	batch := &revisionBatchAcquisition{
		claims:          make(map[recordplatform.IdempotencyKey]recordplatform.IdempotencyClaimResultV1, len(commands)),
		created:         make(map[int]bool),
		continuityProof: proof,
	}
	attachmentIDs, draftIDs, explicitNamespaces := []string{}, []string{}, []string{}
	firstKeys := make(map[recordplatform.IdempotencyKey]int, len(commands))
	keyOrder := make([]int, 0, len(commands))
	recordOrder := make([]string, 0, len(commands))
	allReplay := true
	for index, command := range commands {
		if _, exists := firstKeys[command.Idempotency.Key]; exists {
			continue
		}
		firstKeys[command.Idempotency.Key] = index
		keyOrder = append(keyOrder, index)
		replay, err := peekRevisionReplay(ctx, transaction.tx, command)
		if err != nil {
			return nil, err
		}
		if replay {
			continue
		}
		allReplay = false
		attachmentIDs = append(attachmentIDs, command.Input.AttachmentIDs()...)
		if command.DraftID != "" {
			draftIDs = append(draftIDs, command.DraftID)
		}
		for _, item := range command.ImportedAttachments {
			if item.NamespaceID != "" {
				explicitNamespaces = append(explicitNamespaces, item.NamespaceID)
			}
		}
	}
	importJobID := ""
	if finish != nil && !allReplay {
		importJobID = finish.ImportJobID
	}
	needsBlob := importJobID != "" || len(attachmentIDs) != 0 || len(draftIDs) != 0 || len(explicitNamespaces) != 0
	var namespaces []string
	if needsBlob {
		var err error
		namespaces, err = revisionBatchNamespaces(ctx, transaction.tx, attachmentIDs, draftIDs, explicitNamespaces, importJobID)
		if err != nil {
			return nil, err
		}
		if err := lockLocalBlobNamespaces(ctx, transaction.tx, namespaces); err != nil {
			return nil, err
		}
	}
	if err := lockComparisonDecisionForWriter(ctx, transaction.tx); err != nil {
		return nil, err
	}
	var plan revisionMutationBatchPlan
	resources, err := discoverRevisionBatchResources(ctx, transaction.tx, registry, commands, &plan)
	if err != nil {
		return nil, err
	}
	resources, err = addRevisionImportJob(ctx, transaction.tx, resources, finish)
	if err != nil {
		return nil, err
	}
	resources, err = addRevisionBatchBlobFence(resources, needsBlob)
	if err != nil {
		return nil, err
	}
	if len(resources.resources.Births) != 0 && proof == nil {
		return nil, &pgconn.PgError{Code: "40001", Message: "revision birth appeared after replay preflight"}
	}
	resources.continuityProof = proof
	if err := verifyRevisionPrefetchedRoutes(authority, resources); err != nil {
		return nil, err
	}
	beforeBirth := resources
	if len(resources.resources.Births) != 0 {
		firstBirth := slices.IndexFunc(resources.order, func(declaration recordMutationDeclaration) bool {
			return declaration.family == recordMutationBirths
		})
		if firstBirth < 0 {
			return nil, records.ErrInvalidRevisionMutationResources
		}
		// Birth declarations follow every root at rank 60. The restricted
		// consume below acquires them; generic acquisition must not claim them.
		beforeBirth.order = resources.order[:firstBirth]
	}
	if err := acquireRecordMutationResourceRange(ctx, transaction.tx, beforeBirth, 10, 60); err != nil {
		return nil, err
	}
	firstMutation := make(map[string]int, len(commands))
	for _, index := range keyOrder {
		command := commands[index]
		replay, err := peekRevisionReplay(ctx, transaction.tx, command)
		if err != nil {
			return nil, err
		}
		if replay != plan.replays[index] {
			return nil, &pgconn.PgError{Code: "40001", Message: "revision replay changed; restart transaction"}
		}
		if !replay {
			if _, exists := firstMutation[command.RecordID]; !exists {
				firstMutation[command.RecordID] = index
				recordOrder = append(recordOrder, command.RecordID)
			}
		}
	}
	slices.Sort(recordOrder)
	birthIndex := 0
	speculative := make(map[string]bool, len(recordOrder))
	for _, recordID := range recordOrder {
		index := firstMutation[recordID]
		command := commands[index]
		if command.Idempotency.Key.OperationKind == recordplatform.OperationKindRecordCreate {
			if birthIndex >= len(resources.resources.Births) {
				return nil, records.ErrInvalidRevisionMutationResources
			}
			birth := resources.resources.Births[birthIndex]
			if birth.Creation.RecordID != recordID || birth.AllocationID != command.BirthPermit.AllocationID ||
				birth.Request != command.BirthRequest || birth.Creation != command.BirthPermit.Creation {
				return nil, records.ErrInvalidRevisionMutationResources
			}
			permit := command.BirthPermit
			permit.ContinuityProof = proof
			if err := consumeRecordBirthOnTx(ctx, transaction.tx, command.BirthRequest, permit); err != nil {
				return nil, err
			}
			if _, err := lockRecordRoot(ctx, transaction.tx, recordID); err != nil {
				return nil, err
			}
			batch.created[index] = true
			speculative[recordID] = true
			birthIndex++
		}
	}
	if birthIndex != len(resources.resources.Births) {
		return nil, records.ErrInvalidRevisionMutationResources
	}
	if err := acquireRecordMutationResourceRange(ctx, transaction.tx, resources, 70, 70); err != nil {
		return nil, err
	}
	if err := acquireRecordMutationIdempotencyScopes(ctx, transaction.tx, resources); err != nil {
		return nil, err
	}
	transaction.registerAcquiredIdempotencyScopes(resources)
	slices.SortFunc(keyOrder, func(a, b int) int {
		left, right := commands[a].Idempotency.Key, commands[b].Idempotency.Key
		if order := cmp.Compare(left.ProjectID, right.ProjectID); order != 0 {
			return order
		}
		if order := cmp.Compare(left.OperationKind, right.OperationKind); order != 0 {
			return order
		}
		return cmp.Compare(left.Key, right.Key)
	})
	for _, index := range keyOrder {
		command := commands[index]
		claim, err := transaction.ClaimIdempotency(ctx, command.Idempotency)
		if err != nil {
			return nil, err
		}
		if (claim.ReplayResult == nil) == (claim.Owner == nil) {
			return nil, fmt.Errorf("%w: missing idempotency owner", records.ErrInvalidRevisionCommand)
		}
		if (claim.ReplayResult != nil) != plan.replays[index] {
			return nil, &pgconn.PgError{Code: "40001", Message: "revision claim changed; restart transaction"}
		}
		batch.claims[command.Idempotency.Key] = claim
	}
	if err := acquireRecordMutationResourceRange(ctx, transaction.tx, resources, 71, 71); err != nil {
		return nil, err
	}
	if finish != nil {
		job := resources.resources.ImportJobs[0]
		batch.importReplayed = allReplay && job.State == RecordImportJobStateApplied && job.LockVersion > 0 && uint64(job.LockVersion-1) == finish.JobLockVersion
		if !batch.importReplayed {
			if err := lockImportJobForApplyOnTx(ctx, transaction, *finish); err != nil {
				return nil, err
			}
		}
	}
	if err := acquireRecordMutationResourceRange(ctx, transaction.tx, resources, 72, 75); err != nil {
		return nil, err
	}
	if needsBlob {
		current, err := revisionBatchNamespaces(ctx, transaction.tx, attachmentIDs, draftIDs, explicitNamespaces, importJobID)
		if err != nil {
			return nil, err
		}
		for _, namespace := range current {
			if _, held := slices.BinarySearch(namespaces, namespace); !held {
				return nil, &pgconn.PgError{Code: "40001", Message: "revision namespace discovery changed; restart transaction"}
			}
		}
	}
	var recheckedPlan revisionMutationBatchPlan
	recheckCtx := context.WithValue(ctx, revisionMutationSpeculativeRootsKey{}, speculative)
	current, err := discoverRevisionBatchResources(recheckCtx, transaction.tx, registry, commands, &recheckedPlan)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	current, err = addRevisionImportJob(ctx, transaction.tx, current, finish)
	if err == nil {
		current, err = addRevisionBatchBlobFence(current, needsBlob)
	}
	if err != nil || !reflect.DeepEqual(resources.resources, current.resources) || !reflect.DeepEqual(plan, recheckedPlan) {
		return nil, &pgconn.PgError{Code: "40001", Message: "revision resources changed; restart transaction"}
	}
	if err := authorizeRevisionMutationBatch(ctx, transaction.tx, authority, commands, plan); err != nil {
		return nil, err
	}
	transaction.registerAcquiredRecordMutationResources(resources)
	return batch, nil
}

func addRevisionBatchBlobFence(resources recordMutationResources, needed bool) (recordMutationResources, error) {
	if !needed {
		return resources, nil
	}
	return normalizeRecordMutationDeclarations(resources.resources, records.RevisionMutationResources{
		BlobTables: []records.RevisionMutationBlobTable{records.RevisionMutationBlobMetadataTable, records.RevisionMutationUploadPartsTable},
	})
}

func (batch *revisionBatchAcquisition) claim(ctx context.Context, transaction *RecordPlatformTransaction, command records.RevisionCommitCommand) (recordplatform.IdempotencyClaimResultV1, error) {
	if claim, first := batch.claims[command.Idempotency.Key]; first {
		delete(batch.claims, command.Idempotency.Key)
		return claim, nil
	}
	// This row was acquired in the prefix and completed by the preceding
	// occurrence. Re-reading it acquires no new key and checks the new fingerprint.
	return transaction.ClaimIdempotency(ctx, command.Idempotency)
}

// Discovery is deliberately a projection, not participant validation: replay
// and canonical no-op need not retain captures, snapshots or attachment rows.
// The second projection runs under all roots/drafts and the strong Blob fence.
func revisionBatchNamespaces(ctx context.Context, tx pgx.Tx, attachmentIDs, draftIDs, explicit []string, importJobID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		with chosen as (
			select attachment_id,blob_key,preview_blob_key from public.record_attachments
			where attachment_id=any($1::text[]) or draft_id=any($2::text[])
		), uploads as (
			select upload_id from public.attachment_uploads where attachment_id in (select attachment_id from chosen)
		), namespaces as (
			select unnest($3::uuid[]) as namespace_id
			union select b.namespace_id from public.blob_objects b join chosen a on b.blob_key=a.blob_key or b.blob_key=a.preview_blob_key
			union select p.namespace_id from public.attachment_upload_parts p where p.upload_id in (select upload_id from uploads)
			union select o.namespace_id from public.record_write_obligations o
			where (o.origin_kind='upload' and o.origin_id in (select upload_id from uploads))
			   or (o.origin_kind='processor_preview' and o.origin_id in
			       (select processor_job_id from public.attachment_processor_jobs where upload_id in (select upload_id from uploads)))
			   or (o.origin_kind in ('import_archive','import_attachment') and o.origin_id=$4 and $4<>'')
		)
		select namespace_id::text from namespaces where namespace_id is not null order by namespace_id::text collate "C"`, attachmentIDs, draftIDs, explicit, importJobID)
	if err != nil {
		return nil, fmt.Errorf("discover revision namespaces: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func lockLocalBlobNamespaces(ctx context.Context, tx pgx.Tx, namespaces []string) error {
	for _, namespace := range namespaces {
		body, err := json.Marshal(struct {
			NamespaceID string `json:"namespace_id"`
		}{namespace})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select public.record_revision_namespace_lock($1)`, body); err != nil {
			return fmt.Errorf("lock local Blob namespace: %w", err)
		}
	}
	return nil
}

func lockDraftAttachmentNamespaces(ctx context.Context, tx pgx.Tx, draftIDs []string) ([]string, error) {
	namespaces, err := revisionBatchNamespaces(ctx, tx, nil, draftIDs, nil, "")
	if err != nil {
		return nil, err
	}
	if err := lockLocalBlobNamespaces(ctx, tx, namespaces); err != nil {
		return nil, err
	}
	return namespaces, nil
}

// Called only after the discovered record roots and drafts have been locked.
func fenceDraftAttachmentNamespaces(ctx context.Context, tx pgx.Tx, draftIDs, namespaces []string) error {
	if _, err := tx.Exec(ctx, `select public.record_revision_blob_fence()`); err != nil {
		return err
	}
	current, err := revisionBatchNamespaces(ctx, tx, nil, draftIDs, nil, "")
	if err != nil {
		return err
	}
	for _, namespace := range current {
		if _, held := slices.BinarySearch(namespaces, namespace); !held {
			return &pgconn.PgError{Code: "40001", Message: "draft namespace discovery changed; restart transaction"}
		}
	}
	return nil
}

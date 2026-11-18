package store

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// beginAttachmentDraftWriter prepares W2 outside the writer transaction and
// returns only after the complete draft route, quota and owned content rows
// have been acquired and compared. An empty uploadID denotes a reservation.
func (repository *PostgresAttachmentRepository) beginAttachmentDraftWriter(ctx context.Context, projectID, draftID, uploadID, authorID string, capability recordauth.Capability) (pgx.Tx, error) {
	for attempt := 0; ; attempt++ {
		readTx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, err
		}
		var uploadRoute attachmentUploadRoute
		if uploadID != "" {
			uploadRoute, err = loadAttachmentUploadRoute(ctx, readTx, attachments.UploadMutationCommand{ProjectID: projectID, UploadID: uploadID, AuthorID: authorID})
			if err == nil {
				draftID = uploadRoute.originDraftID
			}
		}
		var route attachmentDraftRoute
		if err == nil {
			route, err = loadAttachmentDraftRoute(ctx, readTx, attachments.ReserveUploadCommand{ProjectID: projectID, DraftID: draftID, AuthorID: authorID})
		}
		if err != nil {
			_ = readTx.Rollback(ctx)
			return nil, err
		}
		if err := readTx.Commit(ctx); err != nil {
			return nil, err
		}
		recordID := ""
		if route.recordID != nil {
			recordID = *route.recordID
		}
		var prepared currentRecordAuthorizationTransactionResolver
		if recordID != "" {
			if nilRecordSubjectDependency(repository.authorization) {
				return nil, ErrRecordSubjectUnavailable
			}
			prepared, err = repository.authorization.prepareCurrentAuthorization(ctx, []string{recordID})
			if err != nil {
				return nil, err
			}
		}
		transaction, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return nil, err
		}
		tx, ok := transaction.(pgx.Tx)
		if !ok {
			_ = transaction.Rollback(ctx)
			return nil, attachments.ErrAttachmentConflict
		}
		resources, err := acquireDraftMutationWithQuota(ctx, tx, []string{draftID}, projectID)
		if err == nil {
			if len(resources.resources.Drafts) != 1 || resources.resources.Drafts[0].ProjectID != projectID ||
				resources.resources.Drafts[0].RecordID != recordID || resources.resources.Drafts[0].AuthorID != authorID {
				err = draftRouteChanged()
			}
		}
		if err == nil && uploadID != "" {
			var current attachmentUploadRoute
			current, err = loadAttachmentUploadRoute(ctx, tx, attachments.UploadMutationCommand{ProjectID: projectID, UploadID: uploadID, AuthorID: authorID})
			if err != nil || !reflect.DeepEqual(uploadRoute, current) {
				err = draftRouteChanged()
			}
		}
		if err == nil {
			members := repository.members
			if nilRecordSubjectDependency(members) {
				members = NewPostgresCollaborationMembershipReader()
			}
			actor, memberErr := members.ReadMemberActor(ctx, tx, recordauth.ProjectID(projectID), authorID)
			if memberErr != nil {
				err = recordauth.ErrDenied
			} else if recordID == "" {
				if actor.Role != recordauth.RoleProjectAdmin {
					err = recordauth.ErrDenied
				}
			} else {
				var current records.CurrentRecordAuthorization
				current, err = prepared.resolveCurrentAuthorizationInTransaction(ctx, tx, actor, recordID)
				if err == nil {
					err = records.AuthorizeRecordResource(actor, capability, current.Evidence)
				}
			}
		}
		if err == nil {
			return tx, nil
		}
		_ = tx.Rollback(ctx)
		if draftSerializationFailure(err) && attempt < 2 {
			continue
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, attachments.ErrAttachmentOwnerNotFound
		}
		return nil, err
	}
}

// An S3 temporary version is a late physical fact, not a permit to publish.
// Its exact upload/key route remains reportable after a gate closes while the
// durable owner rows and epoch still exist. Missing owners are never recreated.
func (repository *PostgresAttachmentRepository) beginAttachmentUploadOutcome(ctx context.Context, command attachments.UploadMutationCommand) (pgx.Tx, attachmentUploadRoute, *string, error) {
	for attempt := 0; ; attempt++ {
		transaction, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return nil, attachmentUploadRoute{}, nil, err
		}
		tx, ok := transaction.(pgx.Tx)
		if !ok {
			_ = transaction.Rollback(ctx)
			return nil, attachmentUploadRoute{}, nil, attachments.ErrAttachmentConflict
		}
		route, err := loadAttachmentUploadRoute(ctx, tx, command)
		var draft attachmentDraftRoute
		if err == nil {
			draft, err = loadAttachmentDraftRoute(ctx, tx, attachments.ReserveUploadCommand{ProjectID: command.ProjectID, DraftID: route.originDraftID, AuthorID: command.AuthorID})
		}
		var namespaces []string
		prefix := attachmentWriterRoute{draftIDs: []string{route.originDraftID}, heldNamespaces: &namespaces}
		if err == nil {
			err = lockAttachmentWriterPrefix(ctx, tx, prefix)
		}
		var observed recordMutationResources
		if err == nil {
			observed, err = discoverDraftMutationResources(ctx, tx, []string{route.originDraftID}, false)
		}
		if err == nil {
			err = acquireTerminalRecordMutationResources(ctx, tx, observed)
		}
		if err == nil {
			err = acquireRecordMutationResourceRange(ctx, tx, observed, 70, 75)
		}
		if err == nil {
			var current recordMutationResources
			current, err = discoverDraftMutationResources(ctx, tx, []string{route.originDraftID}, false)
			if err != nil || !reflect.DeepEqual(observed.resources, current.resources) {
				err = draftRouteChanged()
			}
		}
		if err == nil {
			var current attachmentUploadRoute
			current, err = loadAttachmentUploadRoute(ctx, tx, command)
			if err != nil || current != route {
				err = draftRouteChanged()
			}
		}
		if err == nil {
			var current attachmentDraftRoute
			current, err = loadAttachmentDraftRoute(ctx, tx, attachments.ReserveUploadCommand{ProjectID: command.ProjectID, DraftID: route.originDraftID, AuthorID: command.AuthorID})
			if err != nil || current.projectID != draft.projectID || !equalOptionalAttachmentRecordID(current.recordID, draft.recordID) {
				err = draftRouteChanged()
			}
		}
		if err == nil {
			var current []string
			current, err = discoverAttachmentWriterNamespaces(ctx, tx, prefix)
			if err == nil && !slices.Equal(namespaces, current) {
				err = draftRouteChanged()
			}
		}
		if err == nil {
			return tx, route, draft.recordID, nil
		}
		_ = tx.Rollback(ctx)
		if draftSerializationFailure(err) && attempt < 2 {
			continue
		}
		return nil, attachmentUploadRoute{}, nil, err
	}
}

package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type PostgresRecordDraftRepository struct {
	platform        *PostgresRecordPlatformRepository
	newCheckpointID func() (string, error)
}

var _ records.DraftRepository = (*PostgresRecordDraftRepository)(nil)

const maxExpiredDraftCleanupBatchSize = uint64(100)

func NewPostgresRecordDraftRepository(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordDraftRepository {
	return &PostgresRecordDraftRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate),
		newCheckpointID: func() (string, error) {
			return ids.New("rdc")
		},
	}
}

func (repository *PostgresRecordDraftRepository) GetDraftRouting(
	ctx context.Context,
	draftID string,
	authorID string,
) (records.DraftRouting, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		records.ValidateDraftID(draftID) != nil || recordauth.ValidateActorUserID(authorID) != nil {
		return records.DraftRouting{}, fmt.Errorf("%w: routing lookup", records.ErrInvalidDraftCommand)
	}

	var routing records.DraftRouting
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		loaded, err := scanRecordDraftRoutingRow(transaction.tx.QueryRow(ctx, `
			select drafts.draft_id, drafts.project_id, drafts.record_id,
			       drafts.base_revision_id, drafts.author_id, drafts.updated_at
			from public.record_drafts drafts
			where drafts.draft_id = $1 and drafts.author_id = $2
			  and (
				drafts.record_id is null
				or not exists (
					select 1
					from public.deletion_reservations reservations
					where reservations.project_id = drafts.project_id
					  and reservations.object_kind = $3
					  and reservations.object_id = drafts.record_id
					  and reservations.state in ('fenced', 'committed')
				)
			  )`, draftID, authorID, recordObjectKind), draftID, authorID)
		if err != nil {
			return err
		}
		if loaded.RecordID != "" {
			if err := assertRecordReadFence(ctx, transaction.tx, loaded.RecordID); err != nil {
				return err
			}
		}
		routing = loaded
		return nil
	})
	if err != nil {
		return records.DraftRouting{}, err
	}
	return routing, nil
}

func (repository *PostgresRecordDraftRepository) ListDraftRoutings(
	ctx context.Context,
	authorID string,
	limit uint64,
) ([]records.DraftRouting, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		recordauth.ValidateActorUserID(authorID) != nil || limit == 0 || limit > 100 {
		return nil, fmt.Errorf("%w: routing list", records.ErrInvalidDraftCommand)
	}

	routings := make([]records.DraftRouting, 0, limit)
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		rows, err := transaction.tx.Query(ctx, `
			select drafts.draft_id, drafts.project_id, drafts.record_id,
			       drafts.base_revision_id, drafts.author_id, drafts.updated_at
			from public.record_drafts drafts
			where drafts.author_id = $1
			  and (
				drafts.record_id is null
				or not exists (
					select 1
					from public.deletion_reservations reservations
					where reservations.project_id = drafts.project_id
					  and reservations.object_kind = $3
					  and reservations.object_id = drafts.record_id
					  and reservations.state in ('fenced', 'committed')
				)
			  )
			order by drafts.updated_at desc, drafts.draft_id desc
			limit $2`, authorID, int64(limit), recordObjectKind)
		if err != nil {
			return fmt.Errorf("list record draft routing metadata: %w", err)
		}
		if rows == nil {
			return records.ErrInvalidDraftCommand
		}
		defer rows.Close()

		candidates := make([]records.DraftRouting, 0, limit)
		for rows.Next() {
			routing, err := scanRecordDraftRoutingRow(rows, "", authorID)
			if err != nil {
				return fmt.Errorf("scan record draft routing metadata: %w", err)
			}
			candidates = append(candidates, routing)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate record draft routing metadata: %w", err)
		}
		rows.Close()

		for _, routing := range candidates {
			if routing.RecordID != "" {
				if err := assertRecordReadFence(ctx, transaction.tx, routing.RecordID); err != nil {
					if errors.Is(err, records.ErrRecordDeletionReserved) {
						continue
					}
					return err
				}
			}
			routings = append(routings, routing)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return routings, nil
}

type recordDraftRoutingScanner interface {
	Scan(...any) error
}

func scanRecordDraftRoutingRow(
	row recordDraftRoutingScanner,
	draftID string,
	authorID string,
) (records.DraftRouting, error) {
	var recordID *string
	var baseRevisionID *string
	var projectID string
	var routing records.DraftRouting
	err := row.Scan(
		&routing.DraftID,
		&projectID,
		&recordID,
		&baseRevisionID,
		&routing.AuthorID,
		&routing.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return records.DraftRouting{}, records.ErrDraftNotFound
	}
	if err != nil {
		return records.DraftRouting{}, fmt.Errorf("load record draft routing metadata: %w", err)
	}
	routing.ProjectID = recordauth.ProjectID(projectID)
	if recordID != nil {
		routing.RecordID = *recordID
	}
	if baseRevisionID != nil {
		routing.BaseRevisionID = *baseRevisionID
	}
	routing.UpdatedAt = routing.UpdatedAt.UTC()
	if (draftID != "" && routing.DraftID != draftID) || routing.AuthorID != authorID || routing.Validate() != nil {
		return records.DraftRouting{}, records.ErrDraftNotFound
	}
	return routing, nil
}

func (repository *PostgresRecordDraftRepository) GetDraft(
	ctx context.Context,
	draftID string,
	authorID string,
) (records.Draft, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		records.ValidateDraftID(draftID) != nil || recordauth.ValidateActorUserID(authorID) != nil {
		return records.Draft{}, fmt.Errorf("%w: lookup", records.ErrInvalidDraftCommand)
	}

	var draft records.Draft
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var recordID *string
		err := transaction.tx.QueryRow(ctx, `
			select drafts.record_id
			from public.record_drafts drafts
			where drafts.draft_id = $1 and drafts.author_id = $2
			  and (
				drafts.record_id is null
				or not exists (
					select 1
					from public.deletion_reservations reservations
					where reservations.project_id = drafts.project_id
					  and reservations.object_kind = $3
					  and reservations.object_id = drafts.record_id
					  and reservations.state in ('fenced', 'committed')
				)
			  )`, draftID, authorID, recordObjectKind).Scan(&recordID)
		if errors.Is(err, pgx.ErrNoRows) {
			return records.ErrDraftNotFound
		}
		if err != nil {
			return fmt.Errorf("load record draft routing metadata: %w", err)
		}
		if recordID != nil {
			if err := assertRecordReadFence(ctx, transaction.tx, *recordID); err != nil {
				return err
			}
		}

		draft, err = loadRecordDraft(ctx, transaction.tx, draftID, authorID)
		return err
	})
	if err != nil {
		return records.Draft{}, err
	}
	return draft, nil
}

func loadRecordDraft(ctx context.Context, tx pgx.Tx, draftID, authorID string) (records.Draft, error) {
	return scanRecordDraftRow(tx.QueryRow(ctx, `
		select draft_id, project_id, record_id, base_revision_id, author_id,
		       payload, payload_hash, draft_version, etag_digest,
		       warning_at, created_at, updated_at, expires_at
		from public.record_drafts
		where draft_id = $1 and author_id = $2
		for key share`, draftID, authorID), draftID, authorID)
}

func loadRecordDraftForUpdate(ctx context.Context, tx pgx.Tx, draftID, authorID string) (records.Draft, error) {
	return scanRecordDraftRow(tx.QueryRow(ctx, `
		select draft_id, project_id, record_id, base_revision_id, author_id,
		       payload, payload_hash, draft_version, etag_digest,
		       warning_at, created_at, updated_at, expires_at
		from public.record_drafts
		where draft_id = $1 and author_id = $2
		for update`, draftID, authorID), draftID, authorID)
}

func scanRecordDraftRow(row pgx.Row, draftID, authorID string) (records.Draft, error) {
	var projectID string
	var recordID *string
	var baseRevisionID *string
	var persistedAuthorID string
	var payloadJSON []byte
	var payloadHash []byte
	var version int64
	var etagDigest []byte
	var warningAt time.Time
	var createdAt time.Time
	var updatedAt time.Time
	var expiresAt time.Time
	var persistedDraftID string
	err := row.Scan(
		&persistedDraftID,
		&projectID,
		&recordID,
		&baseRevisionID,
		&persistedAuthorID,
		&payloadJSON,
		&payloadHash,
		&version,
		&etagDigest,
		&warningAt,
		&createdAt,
		&updatedAt,
		&expiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return records.Draft{}, records.ErrDraftNotFound
	}
	if err != nil {
		return records.Draft{}, fmt.Errorf("load record draft: %w", err)
	}

	payload, err := records.NewDraftPayload(payloadJSON)
	if err != nil {
		return records.Draft{}, fmt.Errorf("validate persisted record draft payload: %w", err)
	}
	wantPayloadHash := payload.Hash()
	if !bytes.Equal(payloadHash, wantPayloadHash[:]) || version <= 0 {
		return records.Draft{}, fmt.Errorf("%w: persisted payload authority", records.ErrInvalidDraftCommand)
	}
	etag, err := records.NewDraftETag(persistedDraftID, persistedAuthorID, uint64(version), payload)
	if err != nil {
		return records.Draft{}, fmt.Errorf("validate persisted record draft etag: %w", err)
	}
	wantETagDigest, err := etag.Digest()
	if err != nil || !bytes.Equal(etagDigest, wantETagDigest[:]) {
		return records.Draft{}, fmt.Errorf("%w: persisted etag authority", records.ErrInvalidDraftCommand)
	}

	draft := records.Draft{
		DraftID:   persistedDraftID,
		ProjectID: recordauth.ProjectID(projectID),
		AuthorID:  persistedAuthorID,
		Payload:   payload,
		Version:   uint64(version),
		ETag:      etag,
		WarningAt: warningAt.UTC(),
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
		ExpiresAt: expiresAt.UTC(),
	}
	if recordID != nil {
		draft.RecordID = *recordID
	}
	if baseRevisionID != nil {
		draft.BaseRevisionID = *baseRevisionID
	}
	if draft.DraftID != draftID || draft.AuthorID != authorID || draft.Validate() != nil {
		return records.Draft{}, fmt.Errorf("%w: persisted draft shape", records.ErrInvalidDraftCommand)
	}
	return draft, nil
}

func (repository *PostgresRecordDraftRepository) PatchDraft(
	ctx context.Context,
	command records.DraftPatchCommand,
) (records.Draft, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return records.Draft{}, fmt.Errorf("%w: repository", records.ErrInvalidDraftCommand)
	}
	if err := command.Validate(); err != nil {
		return records.Draft{}, err
	}

	var updated records.Draft
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var recordID *string
		err := transaction.tx.QueryRow(ctx, `
			select drafts.record_id
			from public.record_drafts drafts
			where drafts.draft_id = $1 and drafts.author_id = $2
			  and (
				drafts.record_id is null
				or not exists (
					select 1
					from public.deletion_reservations reservations
					where reservations.project_id = drafts.project_id
					  and reservations.object_kind = $3
					  and reservations.object_id = drafts.record_id
					  and reservations.state in ('fenced', 'committed')
				)
			  )`, command.DraftID, command.AuthorID, recordObjectKind).Scan(&recordID)
		if errors.Is(err, pgx.ErrNoRows) {
			return records.ErrDraftNotFound
		}
		if err != nil {
			return fmt.Errorf("load record draft routing metadata for patch: %w", err)
		}
		if recordID != nil {
			if err := assertRecordMutationFence(ctx, transaction.tx, *recordID); err != nil {
				return err
			}
		}

		server, err := loadRecordDraftForUpdate(ctx, transaction.tx, command.DraftID, command.AuthorID)
		if err != nil {
			return err
		}
		if server.ETag != command.IfMatch {
			return &records.DraftConflictError{Server: server, LocalPayload: command.Payload}
		}
		oldETagDigest, err := server.ETag.Digest()
		if err != nil {
			return err
		}
		if server.Payload.Hash() == command.Payload.Hash() {
			var updatedAt time.Time
			var warningAt time.Time
			var expiresAt time.Time
			err := transaction.tx.QueryRow(ctx, `
				update public.record_drafts
				set updated_at = transaction_timestamp(),
				    warning_at = transaction_timestamp() + (($4::bigint - $5::bigint) * interval '1 microsecond'),
				    expires_at = transaction_timestamp() + ($4 * interval '1 microsecond')
				where draft_id = $1 and author_id = $2 and etag_digest = $3
				returning updated_at, warning_at, expires_at`,
				command.DraftID,
				command.AuthorID,
				oldETagDigest[:],
				command.Policy.DraftTTL.Microseconds(),
				command.Policy.WarningLead.Microseconds(),
			).Scan(&updatedAt, &warningAt, &expiresAt)
			if errors.Is(err, pgx.ErrNoRows) {
				return &records.DraftConflictError{Server: server, LocalPayload: command.Payload}
			}
			if err != nil {
				return fmt.Errorf("refresh record draft retention: %w", err)
			}
			updated = server
			updated.UpdatedAt = updatedAt.UTC()
			updated.WarningAt = warningAt.UTC()
			updated.ExpiresAt = expiresAt.UTC()
			if err := updated.Validate(); err != nil {
				return fmt.Errorf("load refreshed record draft: %w", err)
			}
			return nil
		}

		checkpointID, err := repository.newCheckpointID()
		if err != nil {
			return fmt.Errorf("issue record draft checkpoint id: %w", err)
		}
		nextVersion := server.Version + 1
		nextETag, err := records.NewDraftETag(server.DraftID, server.AuthorID, nextVersion, command.Payload)
		if err != nil {
			return err
		}
		nextETagDigest, err := nextETag.Digest()
		if err != nil {
			return err
		}
		payloadHash := command.Payload.Hash()

		var updatedAt time.Time
		var warningAt time.Time
		var expiresAt time.Time
		err = transaction.tx.QueryRow(ctx, `
			update public.record_drafts
			set payload = $3,
			    payload_hash = $4,
			    draft_version = $5,
			    etag_digest = $6,
			    updated_at = transaction_timestamp(),
			    warning_at = transaction_timestamp() + (($8::bigint - $9::bigint) * interval '1 microsecond'),
			    expires_at = transaction_timestamp() + ($8 * interval '1 microsecond')
			where draft_id = $1 and author_id = $2 and etag_digest = $7
			returning updated_at, warning_at, expires_at`,
			command.DraftID,
			command.AuthorID,
			command.Payload.JSON(),
			payloadHash[:],
			int64(nextVersion),
			nextETagDigest[:],
			oldETagDigest[:],
			command.Policy.DraftTTL.Microseconds(),
			command.Policy.WarningLead.Microseconds(),
		).Scan(&updatedAt, &warningAt, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return &records.DraftConflictError{Server: server, LocalPayload: command.Payload}
		}
		if err != nil {
			return fmt.Errorf("patch record draft: %w", err)
		}

		if _, err := transaction.tx.Exec(ctx, `
			insert into public.record_draft_checkpoints (
				checkpoint_id, draft_id, checkpoint_bucket,
				checkpoint_payload, checkpoint_payload_hash, checkpoint_draft_version,
				created_at, checkpoint_expires_at
			) values (
				$1, $2,
				date_bin($3 * interval '1 microsecond', transaction_timestamp(), timestamptz '2000-01-01 00:00:00+00'),
				$4, $5, $6,
				transaction_timestamp(),
				transaction_timestamp() + ($7 * interval '1 microsecond')
			)
			on conflict (draft_id, checkpoint_bucket) do nothing`,
			checkpointID,
			command.DraftID,
			command.Policy.CheckpointBucket.Microseconds(),
			command.Payload.JSON(),
			payloadHash[:],
			int64(nextVersion),
			command.Policy.CheckpointTTL.Microseconds(),
		); err != nil {
			return fmt.Errorf("insert record draft checkpoint: %w", err)
		}
		if _, err := transaction.tx.Exec(ctx, `
			delete from public.record_draft_checkpoints
			where draft_id = $1
			  and checkpoint_expires_at <= transaction_timestamp()`, command.DraftID); err != nil {
			return fmt.Errorf("delete expired record draft checkpoints: %w", err)
		}
		if _, err := transaction.tx.Exec(ctx, `
			delete from public.record_draft_checkpoints
			where draft_id = $1
			  and checkpoint_id in (
				select checkpoint_id
				from public.record_draft_checkpoints
				where draft_id = $1
				order by created_at desc, checkpoint_id desc
				offset $2
			  )`, command.DraftID, command.Policy.CheckpointLimit); err != nil {
			return fmt.Errorf("prune record draft checkpoints: %w", err)
		}

		updated = server
		updated.Payload = command.Payload
		updated.Version = nextVersion
		updated.ETag = nextETag
		updated.UpdatedAt = updatedAt.UTC()
		updated.WarningAt = warningAt.UTC()
		updated.ExpiresAt = expiresAt.UTC()
		if err := updated.Validate(); err != nil {
			return fmt.Errorf("load patched record draft: %w", err)
		}
		return nil
	})
	if err != nil {
		return records.Draft{}, err
	}
	return updated, nil
}

func (repository *PostgresRecordDraftRepository) DeleteDraft(
	ctx context.Context,
	command records.DraftDeleteCommand,
) error {
	if ctx == nil || repository == nil || repository.platform == nil {
		return fmt.Errorf("%w: repository", records.ErrInvalidDraftCommand)
	}
	if err := command.Validate(); err != nil {
		return err
	}

	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var recordID *string
		err := transaction.tx.QueryRow(ctx, `
			select drafts.record_id
			from public.record_drafts drafts
			where drafts.draft_id = $1 and drafts.author_id = $2
			  and (
				drafts.record_id is null
				or not exists (
					select 1
					from public.deletion_reservations reservations
					where reservations.project_id = drafts.project_id
					  and reservations.object_kind = $3
					  and reservations.object_id = drafts.record_id
					  and reservations.state in ('fenced', 'committed')
				)
			  )`, command.DraftID, command.AuthorID, recordObjectKind).Scan(&recordID)
		if errors.Is(err, pgx.ErrNoRows) {
			return records.ErrDraftNotFound
		}
		if err != nil {
			return fmt.Errorf("load record draft routing metadata for cleanup: %w", err)
		}
		if recordID != nil {
			if err := assertRecordMutationFence(ctx, transaction.tx, *recordID); err != nil {
				return err
			}
		}

		server, err := loadRecordDraftForUpdate(ctx, transaction.tx, command.DraftID, command.AuthorID)
		if err != nil {
			return err
		}
		if command.Reason == records.DraftDeletePublished && server.ETag != command.IfMatch {
			return records.ErrDraftConflict
		}

		if _, err := transaction.tx.Exec(ctx, `
			delete from public.record_draft_checkpoints
			where draft_id = $1`, command.DraftID); err != nil {
			return fmt.Errorf("delete record draft checkpoints: %w", err)
		}

		var deleted pgconn.CommandTag
		if command.Reason == records.DraftDeletePublished {
			etagDigest, err := command.IfMatch.Digest()
			if err != nil {
				return err
			}
			deleted, err = transaction.tx.Exec(ctx, `
				delete from public.record_drafts
				where draft_id = $1 and author_id = $2 and etag_digest = $3`,
				command.DraftID, command.AuthorID, etagDigest[:])
			if err != nil {
				return fmt.Errorf("delete published record draft: %w", err)
			}
		} else {
			deleted, err = transaction.tx.Exec(ctx, `
				delete from public.record_drafts
				where draft_id = $1 and author_id = $2`, command.DraftID, command.AuthorID)
			if err != nil {
				return fmt.Errorf("delete record draft: %w", err)
			}
		}
		if deleted.RowsAffected() != 1 {
			return records.ErrDraftNotFound
		}
		return nil
	})
}

func (repository *PostgresRecordDraftRepository) ClaimExpiredDrafts(
	ctx context.Context,
	limit uint64,
) ([]string, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return nil, fmt.Errorf("%w: repository", records.ErrInvalidDraftCommand)
	}
	if limit == 0 || limit > maxExpiredDraftCleanupBatchSize {
		return nil, fmt.Errorf("%w: cleanup limit", records.ErrInvalidDraftCommand)
	}

	claimed := make([]string, 0, limit)
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		rows, err := transaction.tx.Query(ctx, `
			select draft_id
			from public.record_drafts
			where expires_at <= transaction_timestamp()
			order by expires_at, draft_id
			for update skip locked
			limit $1`, int64(limit))
		if err != nil {
			return fmt.Errorf("claim expired record drafts: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var draftID string
			if err := rows.Scan(&draftID); err != nil {
				return fmt.Errorf("scan expired record draft: %w", err)
			}
			if err := records.ValidateDraftID(draftID); err != nil {
				return fmt.Errorf("validate expired record draft: %w", err)
			}
			claimed = append(claimed, draftID)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate expired record drafts: %w", err)
		}
		if len(claimed) == 0 {
			return nil
		}

		if _, err := transaction.tx.Exec(ctx, `
			delete from public.record_draft_checkpoints
			where draft_id = any($1::text[])`, claimed); err != nil {
			return fmt.Errorf("delete expired record draft checkpoints: %w", err)
		}
		deleted, err := transaction.tx.Exec(ctx, `
			delete from public.record_drafts
			where draft_id = any($1::text[])`, claimed)
		if err != nil {
			return fmt.Errorf("delete expired record drafts: %w", err)
		}
		if deleted.RowsAffected() != int64(len(claimed)) {
			return fmt.Errorf("delete expired record drafts: expected %d rows, deleted %d", len(claimed), deleted.RowsAffected())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (repository *PostgresRecordDraftRepository) CreateDraft(
	ctx context.Context,
	command records.DraftCreateCommand,
) (records.Draft, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return records.Draft{}, fmt.Errorf("%w: repository", records.ErrInvalidDraftCommand)
	}
	if err := command.Validate(); err != nil {
		return records.Draft{}, err
	}
	etag, err := records.NewDraftETag(command.DraftID, command.AuthorID, 1, command.Payload)
	if err != nil {
		return records.Draft{}, err
	}
	etagDigest, err := etag.Digest()
	if err != nil {
		return records.Draft{}, err
	}
	payloadHash := command.Payload.Hash()

	var createdAt time.Time
	var updatedAt time.Time
	var warningAt time.Time
	var expiresAt time.Time
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if command.RecordID != "" {
			if err := assertRecordMutationFence(ctx, transaction.tx, command.RecordID); err != nil {
				return err
			}
			var currentRevisionID string
			var lifecycle string
			err := transaction.tx.QueryRow(ctx, `
				select current_revision_id, lifecycle
				from public.records
				where record_id = $1
				for key share`, command.RecordID).Scan(&currentRevisionID, &lifecycle)
			if errors.Is(err, pgx.ErrNoRows) {
				return records.ErrRecordNotFound
			}
			if err != nil {
				return fmt.Errorf("load record root for draft: %w", err)
			}
			if currentRevisionID != command.BaseRevisionID || lifecycle != string(records.LifecycleActive) {
				return records.ErrDraftRevisionConflict
			}
		}
		return transaction.tx.QueryRow(ctx, `
			insert into public.record_drafts (
				draft_id, project_id, record_id, base_revision_id, author_id,
				payload, payload_hash, draft_version, etag_digest,
				warning_at, created_at, updated_at, expires_at
			) values (
				$1, $2, $3, $4, $5,
				$6, $7, 1, $8,
				transaction_timestamp() + (($9::bigint - $10::bigint) * interval '1 microsecond'),
				transaction_timestamp(), transaction_timestamp(),
				transaction_timestamp() + ($9 * interval '1 microsecond')
			)
			returning created_at, updated_at, warning_at, expires_at`,
			command.DraftID,
			command.ProjectID,
			nullableRecordString(command.RecordID),
			nullableRecordString(command.BaseRevisionID),
			command.AuthorID,
			command.Payload.JSON(),
			payloadHash[:],
			etagDigest[:],
			command.Policy.DraftTTL.Microseconds(),
			command.Policy.WarningLead.Microseconds(),
		).Scan(&createdAt, &updatedAt, &warningAt, &expiresAt)
	})
	if err != nil {
		return records.Draft{}, err
	}
	draft := records.Draft{
		DraftID:        command.DraftID,
		ProjectID:      command.ProjectID,
		RecordID:       command.RecordID,
		BaseRevisionID: command.BaseRevisionID,
		AuthorID:       command.AuthorID,
		Payload:        command.Payload,
		Version:        1,
		ETag:           etag,
		WarningAt:      warningAt.UTC(),
		CreatedAt:      createdAt.UTC(),
		UpdatedAt:      updatedAt.UTC(),
		ExpiresAt:      expiresAt.UTC(),
	}
	if err := draft.Validate(); err != nil {
		return records.Draft{}, fmt.Errorf("load created record draft: %w", err)
	}
	return draft, nil
}

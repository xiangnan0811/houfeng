package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
)

const searchRebuildCoverageDigestDomainV1 = "houfeng.record-search.rebuild-coverage.v1"

// ErrRebuildLeaseHeld reports that another worker holds a live lease on the
// building generation. Only one rebuild may write a generation at a time.
var ErrRebuildLeaseHeld = errors.New("record search rebuild lease held")

type PostgresRecordSearchRebuildStore struct {
	platform *PostgresRecordPlatformRepository
}

func NewPostgresRecordSearchRebuildStore(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordSearchRebuildStore {
	return &PostgresRecordSearchRebuildStore{platform: NewPostgresRecordPlatformRepository(pool, gate)}
}

// RecordSearchRebuildNeeded compares how many records the index should hold
// against how many the published generation actually holds, and reports an
// unfinished building generation as work to resume.
//
// Deletion-reserved records are excluded from both sides. Counting them as
// expected but skipping them while projecting would leave a permanent shortfall
// and rebuild forever.
func (store *PostgresRecordSearchRebuildStore) RecordSearchRebuildNeeded(ctx context.Context) (bool, error) {
	if ctx == nil || store == nil || store.platform == nil {
		return false, fmt.Errorf("%w: rebuild store", recordsearch.ErrInvalidRebuild)
	}
	var needed bool
	err := store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		return transaction.tx.QueryRow(ctx, `
			select
			  exists (
			    select 1 from public.record_search_generations
			    where project_id = $1 and generation_state = 'building'
			  )
			  or (
			    select count(*) from public.records as record
			    where record.project_id = $1
			      and not exists (
			        select 1 from public.deletion_reservations as reservation
			        where reservation.project_id = record.project_id
			          and reservation.object_kind = 'record'
			          and reservation.object_id = record.record_id
			          and reservation.state in ('fenced', 'committed')
			      )
			  ) > (
			    select count(*)
			    from public.record_search_documents as document
			    join public.record_search_generations as generation
			      on generation.generation = document.generation
			    where generation.project_id = $1 and generation.generation_state = 'published'
			  )`,
			recordplatform.ProjectIDDefault,
		).Scan(&needed)
	})
	return needed, err
}

// ClaimRecordSearchRebuild takes over an abandoned building generation or opens
// a new one. Taking over rather than restarting is what makes a crash mid-rebuild
// cheap: the persisted checkpoint and count carry the earlier attempt's work.
func (store *PostgresRecordSearchRebuildStore) ClaimRecordSearchRebuild(
	ctx context.Context,
	claim recordsearch.RebuildClaim,
) (recordsearch.RebuildLease, error) {
	if ctx == nil || store == nil || store.platform == nil ||
		claim.OwnerID == "" || claim.LeaseDuration <= 0 {
		return recordsearch.RebuildLease{}, fmt.Errorf("%w: claim", recordsearch.ErrInvalidRebuild)
	}
	jobID, err := ids.New("rsj")
	if err != nil {
		return recordsearch.RebuildLease{}, fmt.Errorf("mint record search rebuild job id: %w", err)
	}
	var lease recordsearch.RebuildLease
	err = store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		generation, found, err := lockBuildingSearchGeneration(ctx, transaction.tx)
		if err != nil {
			return err
		}
		if !found {
			lease, err = openSearchRebuildGeneration(ctx, transaction.tx, claim, jobID)
			return err
		}
		lease, err = takeOverSearchRebuildJob(ctx, transaction.tx, claim, generation, jobID)
		return err
	})
	if err != nil {
		return recordsearch.RebuildLease{}, err
	}
	return lease, nil
}

func lockBuildingSearchGeneration(ctx context.Context, tx pgx.Tx) (int64, bool, error) {
	var generation int64
	err := tx.QueryRow(ctx, `
		select generation
		from public.record_search_generations
		where project_id = $1 and generation_state = 'building'
		for update`, recordplatform.ProjectIDDefault,
	).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("lock building search generation: %w", err)
	}
	if generation <= 0 {
		return 0, false, fmt.Errorf("%w: building generation", recordsearch.ErrInvalidRebuild)
	}
	return generation, true, nil
}

func openSearchRebuildGeneration(
	ctx context.Context,
	tx pgx.Tx,
	claim recordsearch.RebuildClaim,
	jobID string,
) (recordsearch.RebuildLease, error) {
	var generation int64
	if err := tx.QueryRow(ctx, `
		insert into public.record_search_generations (generation, project_id, generation_state)
		select coalesce(max(generation), 0) + 1, $1, 'building'
		from public.record_search_generations
		returning generation`, recordplatform.ProjectIDDefault,
	).Scan(&generation); err != nil {
		return recordsearch.RebuildLease{}, fmt.Errorf("open building search generation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.record_search_rebuild_jobs (
		  job_id, generation, project_id, job_state, owner_id, lease_expires_at
		) values ($1, $2, $3, 'running', $4, transaction_timestamp() + $5::interval)`,
		jobID, generation, recordplatform.ProjectIDDefault, claim.OwnerID,
		searchRebuildInterval(claim.LeaseDuration),
	); err != nil {
		return recordsearch.RebuildLease{}, fmt.Errorf("open search rebuild job: %w", err)
	}
	return recordsearch.RebuildLease{JobID: jobID, Generation: uint64(generation)}, nil
}

// takeOverSearchRebuildJob claims the running job on an existing building
// generation, or opens a replacement job when the previous one is already
// finished but the generation was never published.
func takeOverSearchRebuildJob(
	ctx context.Context,
	tx pgx.Tx,
	claim recordsearch.RebuildClaim,
	generation int64,
	jobID string,
) (recordsearch.RebuildLease, error) {
	var (
		existingJobID string
		jobState      string
		ownerID       string
		leaseExpired  bool
		resumeAfter   *string
		processed     int64
	)
	err := tx.QueryRow(ctx, `
		select job_id, job_state, owner_id,
		       lease_expires_at <= transaction_timestamp(), resume_after_record_id, processed_count
		from public.record_search_rebuild_jobs
		where generation = $1 and project_id = $2
		order by job_state = 'running' desc, started_at desc
		limit 1
		for update`, generation, recordplatform.ProjectIDDefault,
	).Scan(&existingJobID, &jobState, &ownerID, &leaseExpired, &resumeAfter, &processed)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, `
			insert into public.record_search_rebuild_jobs (
			  job_id, generation, project_id, job_state, owner_id, lease_expires_at
			) values ($1, $2, $3, 'running', $4, transaction_timestamp() + $5::interval)`,
			jobID, generation, recordplatform.ProjectIDDefault, claim.OwnerID,
			searchRebuildInterval(claim.LeaseDuration),
		); err != nil {
			return recordsearch.RebuildLease{}, fmt.Errorf("open search rebuild job for orphan generation: %w", err)
		}
		return recordsearch.RebuildLease{JobID: jobID, Generation: uint64(generation)}, nil
	}
	if err != nil {
		return recordsearch.RebuildLease{}, fmt.Errorf("lock search rebuild job: %w", err)
	}
	if jobState == "running" && !leaseExpired && ownerID != claim.OwnerID {
		return recordsearch.RebuildLease{}, ErrRebuildLeaseHeld
	}
	if processed < 0 {
		return recordsearch.RebuildLease{}, fmt.Errorf("%w: processed count", recordsearch.ErrInvalidRebuild)
	}
	if jobState != "running" {
		// The previous job ended without publishing. Its checkpoint still describes
		// the generation, so the replacement resumes rather than rescanning.
		if _, err := tx.Exec(ctx, `
			insert into public.record_search_rebuild_jobs (
			  job_id, generation, project_id, job_state, owner_id, lease_expires_at,
			  resume_after_record_id, processed_count
			) values ($1, $2, $3, 'running', $4, transaction_timestamp() + $5::interval, $6, $7)`,
			jobID, generation, recordplatform.ProjectIDDefault, claim.OwnerID,
			searchRebuildInterval(claim.LeaseDuration), resumeAfter, processed,
		); err != nil {
			return recordsearch.RebuildLease{}, fmt.Errorf("replace finished search rebuild job: %w", err)
		}
		existingJobID = jobID
	} else if _, err := tx.Exec(ctx, `
		update public.record_search_rebuild_jobs
		set owner_id = $2,
		    lease_expires_at = transaction_timestamp() + $3::interval,
		    updated_at = transaction_timestamp()
		where job_id = $1`,
		existingJobID, claim.OwnerID, searchRebuildInterval(claim.LeaseDuration),
	); err != nil {
		return recordsearch.RebuildLease{}, fmt.Errorf("renew search rebuild lease: %w", err)
	}
	lease := recordsearch.RebuildLease{
		JobID: existingJobID, Generation: uint64(generation), Projected: uint64(processed),
	}
	if resumeAfter != nil {
		lease.ResumeAfter = *resumeAfter
	}
	return lease, nil
}

// ProjectRecordSearchRebuildBatch projects one bounded stretch of records into
// the building generation and advances the job checkpoint in the same
// transaction, so a crash resumes from work that actually landed.
func (store *PostgresRecordSearchRebuildStore) ProjectRecordSearchRebuildBatch(
	ctx context.Context,
	batch recordsearch.RebuildBatch,
) (recordsearch.RebuildBatchResult, error) {
	if ctx == nil || store == nil || store.platform == nil || batch.JobID == "" ||
		batch.Generation == 0 || batch.Generation > math.MaxInt64 || batch.OwnerID == "" ||
		batch.Limit == 0 || batch.LeaseDuration <= 0 {
		return recordsearch.RebuildBatchResult{}, fmt.Errorf("%w: batch", recordsearch.ErrInvalidRebuild)
	}
	var result recordsearch.RebuildBatchResult
	err := store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		if err := assertOwnedSearchRebuildJob(ctx, transaction.tx, batch); err != nil {
			return err
		}
		candidates, err := listSearchRebuildCandidates(ctx, transaction.tx, batch)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			result = recordsearch.RebuildBatchResult{Drained: true}
			return finishSearchRebuildBatch(ctx, transaction.tx, batch, result)
		}
		for _, candidate := range candidates {
			projected, err := projectSearchRebuildRecord(ctx, transaction.tx, batch, candidate)
			if err != nil {
				return err
			}
			if projected {
				result.Projected++
			}
			result.ResumeAfter = candidate.recordID
		}
		result.Drained = uint32(len(candidates)) < batch.Limit
		return finishSearchRebuildBatch(ctx, transaction.tx, batch, result)
	})
	if err != nil {
		return recordsearch.RebuildBatchResult{}, err
	}
	return result, nil
}

func assertOwnedSearchRebuildJob(ctx context.Context, tx pgx.Tx, batch recordsearch.RebuildBatch) error {
	var owned bool
	err := tx.QueryRow(ctx, `
		select job_state = 'running' and owner_id = $2 and lease_expires_at > transaction_timestamp()
		from public.record_search_rebuild_jobs
		where job_id = $1 and generation = $3
		for update`, batch.JobID, batch.OwnerID, int64(batch.Generation),
	).Scan(&owned)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRebuildLeaseHeld
	}
	if err != nil {
		return fmt.Errorf("lock owned search rebuild job: %w", err)
	}
	if !owned {
		return ErrRebuildLeaseHeld
	}
	return nil
}

type searchRebuildCandidate struct {
	recordID           string
	currentRevisionID  string
	lockVersion        uint64
	authorizationEpoch uint64
}

// listSearchRebuildCandidates walks record roots in id order after the
// checkpoint. Ordering by identity rather than time is what makes the checkpoint
// meaningful while records are being written underneath the rebuild.
func listSearchRebuildCandidates(
	ctx context.Context,
	tx pgx.Tx,
	batch recordsearch.RebuildBatch,
) ([]searchRebuildCandidate, error) {
	rows, err := tx.Query(ctx, `
		select record.record_id, record.current_revision_id, record.lock_version, record.authorization_epoch
		from public.records as record
		where record.project_id = $1
		  and record.current_revision_id is not null
		  and ($2::text is null or record.record_id > $2::text)
		order by record.record_id
		limit $3`,
		recordplatform.ProjectIDDefault, nullableRecordString(batch.ResumeAfter), int64(batch.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list search rebuild candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]searchRebuildCandidate, 0, batch.Limit)
	for rows.Next() {
		var (
			candidate  searchRebuildCandidate
			revisionID string
			lock       int64
			epoch      int64
		)
		if err := rows.Scan(&candidate.recordID, &revisionID, &lock, &epoch); err != nil {
			return nil, fmt.Errorf("scan search rebuild candidate: %w", err)
		}
		if lock < 0 || epoch < 0 {
			return nil, fmt.Errorf("%w: candidate versions", recordsearch.ErrInvalidRebuild)
		}
		candidate.currentRevisionID = revisionID
		candidate.lockVersion = uint64(lock)
		candidate.authorizationEpoch = uint64(epoch)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read search rebuild candidates: %w", err)
	}
	return candidates, nil
}

// projectSearchRebuildRecord re-derives one record from its stored revision.
// A record under a deletion reservation is skipped rather than failed: the
// deletion adapter owns removing it, and blocking the whole rebuild on an
// in-flight delete would leave the index unbuildable until it finished.
func projectSearchRebuildRecord(
	ctx context.Context,
	tx pgx.Tx,
	batch recordsearch.RebuildBatch,
	candidate searchRebuildCandidate,
) (bool, error) {
	stored, err := loadStoredRecordRevision(ctx, tx, records.StoredRecordRevisionRequest{
		RecordID:           candidate.recordID,
		RevisionID:         candidate.currentRevisionID,
		CurrentRevisionID:  candidate.currentRevisionID,
		LockVersion:        candidate.lockVersion,
		AuthorizationEpoch: candidate.authorizationEpoch,
	})
	if err != nil {
		if errors.Is(err, records.ErrRecordRevisionConflict) || errors.Is(err, records.ErrRecordDeletionReserved) {
			return false, nil
		}
		return false, err
	}
	facts, err := recordSearchDocumentFactsFromRevision(ctx, tx, recordSearchProjectionSource{
		RecordID:           stored.RecordID,
		RevisionID:         stored.RevisionID,
		LockVersion:        stored.LockVersion,
		AuthorizationEpoch: stored.AuthorizationEpoch,
		Lifecycle:          stored.Lifecycle,
		Input:              stored.Input,
		RecordUpdatedAt:    stored.RecordUpdatedAt,
	})
	if err != nil {
		return false, err
	}
	generations, err := upsertRecordSearchDocument(ctx, tx, facts, int64(batch.Generation))
	if err != nil {
		return false, err
	}
	if err := replaceRecordSearchSubjects(ctx, tx, facts, generations); err != nil {
		return false, err
	}
	// No generation written means a live commit already placed a newer projection
	// there, which is the outcome the fence exists to produce.
	return len(generations) > 0, nil
}

func finishSearchRebuildBatch(
	ctx context.Context,
	tx pgx.Tx,
	batch recordsearch.RebuildBatch,
	result recordsearch.RebuildBatchResult,
) error {
	if _, err := tx.Exec(ctx, `
		update public.record_search_rebuild_jobs
		set resume_after_record_id = coalesce($2, resume_after_record_id),
		    processed_count = processed_count + $3,
		    lease_expires_at = transaction_timestamp() + $4::interval,
		    updated_at = transaction_timestamp()
		where job_id = $1`,
		batch.JobID, nullableRecordString(result.ResumeAfter), int64(result.Projected),
		searchRebuildInterval(batch.LeaseDuration),
	); err != nil {
		return fmt.Errorf("advance search rebuild checkpoint: %w", err)
	}
	return nil
}

// PublishRecordSearchRebuild swaps the building generation in for the published
// one. The state transition is the CAS: the partial unique indexes allow exactly
// one published and one building generation, so a second publisher loses rather
// than producing two live indexes.
func (store *PostgresRecordSearchRebuildStore) PublishRecordSearchRebuild(
	ctx context.Context,
	publish recordsearch.RebuildPublish,
) (recordsearch.RebuildCoverage, error) {
	if ctx == nil || store == nil || store.platform == nil || publish.JobID == "" ||
		publish.Generation == 0 || publish.Generation > math.MaxInt64 || publish.OwnerID == "" {
		return recordsearch.RebuildCoverage{}, fmt.Errorf("%w: publish", recordsearch.ErrInvalidRebuild)
	}
	var coverage recordsearch.RebuildCoverage
	err := store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		if err := assertOwnedSearchRebuildJob(ctx, transaction.tx, recordsearch.RebuildBatch{
			JobID: publish.JobID, Generation: publish.Generation, OwnerID: publish.OwnerID,
			Limit: 1, LeaseDuration: time.Minute,
		}); err != nil {
			return err
		}
		var state string
		if err := transaction.tx.QueryRow(ctx, `
			select generation_state
			from public.record_search_generations
			where generation = $1 and project_id = $2
			for update`, int64(publish.Generation), recordplatform.ProjectIDDefault,
		).Scan(&state); err != nil {
			return fmt.Errorf("lock search generation for publish: %w", err)
		}
		if state != "building" {
			return fmt.Errorf("%w: generation state %q", recordsearch.ErrInvalidRebuild, state)
		}
		measured, err := measureSearchGenerationCoverage(ctx, transaction.tx, int64(publish.Generation))
		if err != nil {
			return err
		}
		if _, err := transaction.tx.Exec(ctx, `
			update public.record_search_generations
			set generation_state = 'superseded',
			    superseded_at = transaction_timestamp(),
			    updated_at = transaction_timestamp()
			where project_id = $1 and generation_state = 'published'`,
			recordplatform.ProjectIDDefault,
		); err != nil {
			return fmt.Errorf("supersede published search generation: %w", err)
		}
		if _, err := transaction.tx.Exec(ctx, `
			update public.record_search_generations
			set generation_state = 'published',
			    published_at = transaction_timestamp(),
			    document_count = $2,
			    coverage_digest = $3,
			    updated_at = transaction_timestamp()
			where generation = $1`,
			int64(publish.Generation), int64(measured.DocumentCount), measured.CoverageDigest[:],
		); err != nil {
			return fmt.Errorf("publish search generation: %w", err)
		}
		if _, err := transaction.tx.Exec(ctx, `
			update public.record_search_rebuild_jobs
			set job_state = 'completed',
			    finished_at = transaction_timestamp(),
			    updated_at = transaction_timestamp()
			where job_id = $1`, publish.JobID,
		); err != nil {
			return fmt.Errorf("complete search rebuild job: %w", err)
		}
		coverage = measured
		return nil
	})
	if err != nil {
		return recordsearch.RebuildCoverage{}, err
	}
	return coverage, nil
}

// measureSearchGenerationCoverage records what was actually published rather than
// what the worker counted, so the audit trail cannot drift from the rows.
func measureSearchGenerationCoverage(
	ctx context.Context,
	tx pgx.Tx,
	generation int64,
) (recordsearch.RebuildCoverage, error) {
	var count int64
	var material []byte
	if err := tx.QueryRow(ctx, `
		select count(*)::bigint,
		       coalesce(
		         pg_catalog.convert_to(
		           string_agg(record_id || ':' || encode(document_digest, 'hex'), E'\n' order by record_id),
		           'UTF8'),
		         ''::bytea)
		from public.record_search_documents
		where generation = $1`, generation,
	).Scan(&count, &material); err != nil {
		return recordsearch.RebuildCoverage{}, fmt.Errorf("measure search generation coverage: %w", err)
	}
	if count < 0 {
		return recordsearch.RebuildCoverage{}, fmt.Errorf("%w: coverage count", recordsearch.ErrInvalidRebuild)
	}
	return recordsearch.RebuildCoverage{
		DocumentCount: uint64(count),
		CoverageDigest: digestRecordPurgeBytes(
			searchRebuildCoverageDigestDomainV1, recordPurgeUint64(uint64(count)), material,
		),
	}, nil
}

// FailRecordSearchRebuild marks the generation failed so a later pass opens a
// fresh one. A failed generation is left in place rather than deleted: its rows
// are evidence for why the rebuild stopped, and generation retirement removes
// them deliberately.
func (store *PostgresRecordSearchRebuildStore) FailRecordSearchRebuild(
	ctx context.Context,
	failure recordsearch.RebuildFailure,
) error {
	if ctx == nil || store == nil || store.platform == nil || failure.JobID == "" ||
		failure.Generation == 0 || failure.Generation > math.MaxInt64 || failure.Reason == "" {
		return fmt.Errorf("%w: failure", recordsearch.ErrInvalidRebuild)
	}
	return store.platform.RunRecordPlatformTransaction(ctx, func(
		ctx context.Context,
		transaction *RecordPlatformTransaction,
	) error {
		if _, err := transaction.tx.Exec(ctx, `
			update public.record_search_rebuild_jobs
			set job_state = 'failed',
			    failure_reason = $2,
			    finished_at = transaction_timestamp(),
			    updated_at = transaction_timestamp()
			where job_id = $1 and job_state = 'running'`, failure.JobID, failure.Reason,
		); err != nil {
			return fmt.Errorf("fail search rebuild job: %w", err)
		}
		if _, err := transaction.tx.Exec(ctx, `
			update public.record_search_generations
			set generation_state = 'failed',
			    failure_reason = $2,
			    updated_at = transaction_timestamp()
			where generation = $1 and project_id = $3 and generation_state = 'building'`,
			int64(failure.Generation), failure.Reason, recordplatform.ProjectIDDefault,
		); err != nil {
			return fmt.Errorf("fail search generation: %w", err)
		}
		return nil
	})
}

func searchRebuildInterval(duration time.Duration) string {
	return fmt.Sprintf("%d milliseconds", duration.Milliseconds())
}

var _ recordsearch.RebuildStore = (*PostgresRecordSearchRebuildStore)(nil)

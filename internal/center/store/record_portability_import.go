package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type RecordImportJob struct {
	ImportJobID   string
	PlanID        string
	ActorID       string
	JobState      string
	LockVersion   uint64
	ArchiveDigest [32]byte
	ExpiresAt     time.Time
}

type RecordImportArtifact struct {
	ArtifactID      string
	ImportJobID     string
	ArtifactRole    string
	BackendKind     string
	BlobKey         string
	ObjectVersionID string
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
	SHA256          [32]byte
	ByteSize        uint64
	ExpiresAt       time.Time
}

type ImportRemap struct {
	EntityKind string
	SourceID   string
	TargetID   string
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
	ImportPlanID     string
	ImportJobID      string
	PlanDigest       [32]byte
	ObjectCount      uint64
	RemapCount       uint64
	LockVersion      uint64
	JobState         string
	Remaps           []ImportRemap
	Documents        []ImportDocumentPlan
	Quarantine       []QuarantinedEvidence
	AppliedRecordIDs []string
	ExpiresAt        time.Time
}

type ClaimRecordImportJobInput struct {
	ActorID        string
	IdempotencyKey string
	ArchiveDigest  [32]byte
	ExpiresAt      time.Time
}

type SaveRecordImportPlanInput struct {
	ImportJobID string
	PlanDigest  [32]byte
	ObjectCount uint64
	RemapCount  uint64
	Remaps      []ImportRemap
	Quarantine  []QuarantinedEvidence
	Documents   []ImportDocumentPlan
	ExpiresAt   time.Time
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
		input.ActorID == "" || input.IdempotencyKey == "" || input.ArchiveDigest == [32]byte{} {
		return RecordImportJob{}, ErrRecordPortabilityUnavailable
	}
	var claimed RecordImportJob
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var digest []byte
		err := transaction.tx.QueryRow(ctx, `
			select import_job_id, actor_id, job_state, lock_version, archive_digest, expires_at
			from public.record_import_jobs
			where project_id = $1 and actor_id = $2 and idempotency_key = $3
		`, recordauth.ProjectIDDefault, input.ActorID, input.IdempotencyKey).Scan(
			&claimed.ImportJobID, &claimed.ActorID, &claimed.JobState, &claimed.LockVersion, &digest, &claimed.ExpiresAt,
		)
		if err == nil {
			copy(claimed.ArchiveDigest[:], digest)
			if claimed.ArchiveDigest != input.ArchiveDigest {
				return ErrRecordImportCASConflict
			}
			return attachImportPlanID(ctx, transaction, &claimed)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		jobID, err := ids.New("rij")
		if err != nil {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		if _, err := transaction.tx.Exec(ctx, `
			insert into public.record_import_jobs (
				import_job_id, project_id, actor_id, idempotency_key, job_state,
				identity_classification, archive_digest, lock_version, expires_at, created_at, updated_at
			) values ($1, $2, $3, $4, $5, $6, $7, 1, $8, $9, $9)
		`, jobID, recordauth.ProjectIDDefault, input.ActorID, input.IdempotencyKey,
			RecordImportJobStateQuarantined, "unknown", input.ArchiveDigest[:], input.ExpiresAt.UTC(), now,
		); err != nil {
			return err
		}
		claimed = RecordImportJob{
			ImportJobID: jobID, ActorID: input.ActorID, JobState: RecordImportJobStateQuarantined,
			LockVersion: 1, ArchiveDigest: input.ArchiveDigest, ExpiresAt: input.ExpiresAt.UTC(),
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
	if ctx == nil || repository == nil || repository.platform == nil || input.ImportJobID == "" {
		return RecordImportPlan{}, ErrRecordPortabilityUnavailable
	}
	var saved RecordImportPlan
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
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
			input.ExpiresAt.UTC(), now,
		); err != nil {
			return err
		}
		for _, remap := range input.Remaps {
			source := sha256SumString(remap.SourceID)
			if _, err := transaction.tx.Exec(ctx, `
				insert into public.record_import_entity_mappings (
					import_plan_id, entity_kind, source_id, source_identity_digest, target_id, created_at
				) values ($1, $2, $3, $4, $5, $6)
			`, planID, remap.EntityKind, remap.SourceID, source[:], remap.TargetID, now); err != nil {
				return err
			}
		}
		saved = RecordImportPlan{
			ImportPlanID: planID, ImportJobID: input.ImportJobID, PlanDigest: input.PlanDigest,
			ObjectCount: input.ObjectCount, RemapCount: input.RemapCount, Remaps: input.Remaps,
			Documents: input.Documents, Quarantine: input.Quarantine, ExpiresAt: input.ExpiresAt.UTC(),
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
		if err := transaction.tx.QueryRow(ctx, `
			select import_plan_id, import_job_id, plan_digest, object_count, remap_count, expires_at
			from public.record_import_plans
			where import_plan_id = $1
		`, planID).Scan(
			&plan.ImportPlanID, &plan.ImportJobID, &digest, &objectCount, &remapCount, &plan.ExpiresAt,
		); err != nil {
			return err
		}
		if len(digest) != sha256.Size {
			return ErrRecordPortabilityUnavailable
		}
		copy(plan.PlanDigest[:], digest)
		plan.ObjectCount = uint64(objectCount)
		plan.RemapCount = uint64(remapCount)
		rows, err := transaction.tx.Query(ctx, `
			select entity_kind, source_id, target_id
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
			if err := rows.Scan(&remap.EntityKind, &remap.SourceID, &remap.TargetID); err != nil {
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
	if state != RecordImportJobStatePlanned || actorID != finish.ActorID || uint64(lockVersion) != finish.JobLockVersion {
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
		RecordImportJobStatePlanned, finish.ActorID)
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
		if err := transaction.tx.QueryRow(ctx, `
			select import_job_id, actor_id, job_state, lock_version, archive_digest, expires_at
			from public.record_import_jobs
			where import_job_id = $1
		`, importJobID).Scan(&job.ImportJobID, &job.ActorID, &job.JobState, &job.LockVersion, &digest, &job.ExpiresAt); err != nil {
			return err
		}
		if len(digest) != sha256.Size {
			return ErrRecordPortabilityUnavailable
		}
		copy(job.ArchiveDigest[:], digest)
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
		input.BlobKey == "" || input.ObjectVersionID == "" ||
		input.ByteSize == 0 || input.SHA256 == [32]byte{} || input.ExpiresAt.IsZero() {
		return RecordImportArtifact{}, ErrRecordPortabilityUnavailable
	}
	var published RecordImportArtifact
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var existing RecordImportArtifact
		var digest []byte
		var byteSize int64
		err := transaction.tx.QueryRow(ctx, `
			select import_artifact_id, import_job_id, artifact_role, backend_kind, blob_key,
			       object_version_id, sha256, byte_size, expires_at
			from public.record_import_artifacts
			where import_job_id = $1 and artifact_role = $2
		`, input.ImportJobID, input.ArtifactRole).Scan(
			&existing.ArtifactID, &existing.ImportJobID, &existing.ArtifactRole, &existing.BackendKind,
			&existing.BlobKey, &existing.ObjectVersionID, &digest, &byteSize, &existing.ExpiresAt,
		)
		if err == nil {
			if len(digest) != sha256.Size || byteSize <= 0 {
				return ErrRecordPortabilityUnavailable
			}
			copy(existing.SHA256[:], digest)
			existing.ByteSize = uint64(byteSize)
			if existing.SHA256 != input.SHA256 || existing.BlobKey != input.BlobKey {
				return ErrRecordImportCASConflict
			}
			published = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		artifactID, err := repository.newImportArtifactID()
		if err != nil || artifactID == "" {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		artifact := RecordImportArtifact{
			ArtifactID: artifactID, ImportJobID: input.ImportJobID, ArtifactRole: input.ArtifactRole,
			BackendKind: input.BackendKind, BlobKey: input.BlobKey, ObjectVersionID: input.ObjectVersionID,
			SHA256: input.SHA256, ByteSize: input.ByteSize, ExpiresAt: input.ExpiresAt.UTC(),
		}
		if _, err := transaction.tx.Exec(ctx, `
			insert into public.record_import_artifacts (
				import_artifact_id, import_job_id, artifact_role, backend_kind,
				blob_key, object_version_id, sha256, byte_size, expires_at, created_at
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`,
			artifact.ArtifactID, artifact.ImportJobID, artifact.ArtifactRole, artifact.BackendKind,
			artifact.BlobKey, artifact.ObjectVersionID, artifact.SHA256[:], int64(artifact.ByteSize),
			artifact.ExpiresAt, now,
		); err != nil {
			return err
		}
		published = artifact
		return nil
	})
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
			       object_version_id, sha256, byte_size, expires_at
			from public.record_import_artifacts
			where import_job_id = $1 and artifact_role = 'archive'
		`, importJobID).Scan(
			&artifact.ArtifactID, &artifact.ImportJobID, &artifact.ArtifactRole, &artifact.BackendKind,
			&artifact.BlobKey, &artifact.ObjectVersionID, &digest, &byteSize, &artifact.ExpiresAt,
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

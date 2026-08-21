package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
)

var (
	ErrRecordPortabilityUnavailable = errors.New("record portability unavailable")
	ErrRecordExportJobConflict      = errors.New("record export job conflict")
	ErrRecordExportJobCASConflict   = errors.New("record export job cas conflict")
	ErrRecordExportNotFound         = errors.New("record export not found")
	ErrRecordImportCASConflict      = errors.New("record import cas conflict")
	ErrRecordImportNotFound         = errors.New("record import not found")
	ErrRecordOriginTombstoned       = errors.New("record origin tombstoned")
	ErrRecordOriginConflict         = errors.New("record origin already exists")
)

const (
	RecordExportKindMarkdown        = "markdown"
	RecordExportKindComparisonJSON  = "comparison_json"
	RecordExportKindEvidenceJSON    = "evidence_json"
	RecordExportKindArchive         = "archive"
	RecordExportKindPDF             = "pdf"
	RecordExportModeSafe            = "safe"
	RecordExportModeSensitiveTopo   = "sensitive_topology"
	RecordExportJobStatePreviewed   = "previewed"
	RecordExportJobStateStaging     = "staging"
	RecordExportJobStatePublished   = "published"
	RecordExportJobStateExpired     = "expired"
	RecordExportJobStateRevoked     = "revoked"
	RecordExportJobStateFailed      = "failed"
	RecordImportJobStateQuarantined = "quarantined"
	RecordImportJobStatePlanned     = "planned"
	RecordImportJobStateApplying    = "applying"
	RecordImportJobStateApplied     = "applied"
	RecordImportJobStateFailed      = "failed"
)

type RecordExportJob struct {
	ExportJobID        string
	ProjectID          recordauth.ProjectID
	ActorID            string
	IdempotencyKey     string
	ExportKind         string
	ExportMode         string
	JobState           string
	FailureCode        string
	LockVersion        uint64
	RequestFingerprint [32]byte
	InventoryDigest    [32]byte
	AuthorizationEpoch uint64
	RecordID           string
	RevisionID         string
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ClaimRecordExportJobInput struct {
	ActorID            string
	IdempotencyKey     string
	ExportKind         string
	ExportMode         string
	RequestFingerprint [32]byte
	InventoryDigest    [32]byte
	AuthorizationEpoch uint64
	RecordID           string
	RevisionID         string
	ExpiresAt          time.Time
}

type AdvanceRecordExportJobInput struct {
	ExportJobID string
	LockVersion uint64
	JobState    string
	FailureCode string
	ExpiresAt   time.Time
}

type RecordExportArtifact struct {
	ArtifactID   string
	ExportJobID  string
	ArtifactKind string
	ContentType  string
	BackendKind  string
	BlobKey      string
	SHA256       [32]byte
	ByteSize     uint64
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	CreatedAt    time.Time
}

type PublishRecordExportArtifactInput struct {
	ExportJobID  string
	ArtifactKind string
	ContentType  string
	BackendKind  string
	BlobKey      string
	SHA256       [32]byte
	ByteSize     uint64
	ExpiresAt    time.Time
}

type PostgresRecordPortabilityRepository struct {
	platform            *PostgresRecordPlatformRepository
	newExportJobID      func() (string, error)
	newExportArtifactID func() (string, error)
	newImportArtifactID func() (string, error)
}

func NewPostgresRecordPortabilityRepository(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordPortabilityRepository {
	return &PostgresRecordPortabilityRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate),
		newExportJobID: func() (string, error) {
			return ids.New("rej")
		},
		newExportArtifactID: func() (string, error) {
			return ids.New("rxa")
		},
		newImportArtifactID: func() (string, error) {
			return ids.New("ria")
		},
	}
}

func (repository *PostgresRecordPortabilityRepository) ClaimExportJob(
	ctx context.Context,
	input ClaimRecordExportJobInput,
) (RecordExportJob, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return RecordExportJob{}, ErrRecordPortabilityUnavailable
	}
	if err := validateClaimRecordExportJobInput(input); err != nil {
		return RecordExportJob{}, err
	}
	var claimed RecordExportJob
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		existing, err := loadRecordExportJobByIdempotency(ctx, transaction.tx, input.ActorID, input.IdempotencyKey)
		if err == nil {
			if existing.RequestFingerprint != input.RequestFingerprint ||
				existing.ExportKind != input.ExportKind ||
				existing.ExportMode != input.ExportMode ||
				existing.InventoryDigest != input.InventoryDigest {
				return ErrRecordExportJobConflict
			}
			claimed = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		jobID, err := repository.newExportJobID()
		if err != nil || jobID == "" {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		job := RecordExportJob{
			ExportJobID:        jobID,
			ProjectID:          recordauth.ProjectIDDefault,
			ActorID:            input.ActorID,
			IdempotencyKey:     input.IdempotencyKey,
			ExportKind:         input.ExportKind,
			ExportMode:         input.ExportMode,
			JobState:           RecordExportJobStatePreviewed,
			LockVersion:        1,
			RequestFingerprint: input.RequestFingerprint,
			InventoryDigest:    input.InventoryDigest,
			AuthorizationEpoch: input.AuthorizationEpoch,
			RecordID:           input.RecordID,
			RevisionID:         input.RevisionID,
			ExpiresAt:          input.ExpiresAt.UTC(),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := insertRecordExportJob(ctx, transaction.tx, job); err != nil {
			return err
		}
		claimed = job
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRecordExportJobConflict) || errors.Is(err, ErrRecordPortabilityUnavailable) ||
			errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
			return RecordExportJob{}, err
		}
		return RecordExportJob{}, fmt.Errorf("%w: claim export job", ErrRecordPortabilityUnavailable)
	}
	return claimed, nil
}

func (repository *PostgresRecordPortabilityRepository) AdvanceExportJob(
	ctx context.Context,
	input AdvanceRecordExportJobInput,
) error {
	if ctx == nil || repository == nil || repository.platform == nil ||
		input.ExportJobID == "" || input.LockVersion == 0 || !knownRecordExportJobState(input.JobState) {
		return ErrRecordPortabilityUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		tag, err := transaction.tx.Exec(ctx, `
			update public.record_export_jobs
			set job_state = $1,
			    failure_code = $2,
			    expires_at = $3,
			    lock_version = lock_version + 1,
			    updated_at = now()
			where export_job_id = $4
			  and lock_version = $5
		`, input.JobState, input.FailureCode, input.ExpiresAt.UTC(), input.ExportJobID, int64(input.LockVersion))
		if err != nil {
			return fmt.Errorf("advance export job: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrRecordExportJobCASConflict
		}
		return nil
	})
}

func (repository *PostgresRecordPortabilityRepository) LoadExportJob(
	ctx context.Context,
	exportJobID string,
) (RecordExportJob, error) {
	if ctx == nil || repository == nil || repository.platform == nil || exportJobID == "" {
		return RecordExportJob{}, ErrRecordPortabilityUnavailable
	}
	var job RecordExportJob
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		loaded, err := loadRecordExportJobByID(ctx, transaction.tx, exportJobID)
		if err != nil {
			return err
		}
		job = loaded
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordExportJob{}, ErrRecordExportNotFound
		}
		if errors.Is(err, ErrRecordPortabilityUnavailable) || errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
			return RecordExportJob{}, err
		}
		return RecordExportJob{}, fmt.Errorf("%w: load export job", ErrRecordPortabilityUnavailable)
	}
	return job, nil
}

func (repository *PostgresRecordPortabilityRepository) PublishExportArtifact(
	ctx context.Context,
	input PublishRecordExportArtifactInput,
) (RecordExportArtifact, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		repository.newExportArtifactID == nil || !knownRecordExportKind(input.ArtifactKind) ||
		!knownExportArtifactContentType(input.ContentType) ||
		(input.BackendKind != "local" && input.BackendKind != "s3") ||
		input.ExportJobID == "" || input.BlobKey == "" || input.ByteSize == 0 ||
		input.SHA256 == [32]byte{} || input.ExpiresAt.IsZero() {
		return RecordExportArtifact{}, ErrRecordPortabilityUnavailable
	}
	var published RecordExportArtifact
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		artifactID, err := repository.newExportArtifactID()
		if err != nil || artifactID == "" {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		artifact := RecordExportArtifact{
			ArtifactID:   artifactID,
			ExportJobID:  input.ExportJobID,
			ArtifactKind: input.ArtifactKind,
			ContentType:  input.ContentType,
			BackendKind:  input.BackendKind,
			BlobKey:      input.BlobKey,
			SHA256:       input.SHA256,
			ByteSize:     input.ByteSize,
			ExpiresAt:    input.ExpiresAt.UTC(),
			CreatedAt:    now,
		}
		if _, err := transaction.tx.Exec(ctx, `
			insert into public.record_export_artifacts (
				export_artifact_id, export_job_id, artifact_kind, content_type, backend_kind,
				blob_key, sha256, byte_size, expires_at, created_at
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`,
			artifact.ArtifactID, artifact.ExportJobID, artifact.ArtifactKind, artifact.ContentType,
			artifact.BackendKind, artifact.BlobKey, artifact.SHA256[:], int64(artifact.ByteSize),
			artifact.ExpiresAt, artifact.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert export artifact: %w", err)
		}
		published = artifact
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRecordPortabilityUnavailable) || errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
			return RecordExportArtifact{}, err
		}
		return RecordExportArtifact{}, fmt.Errorf("%w: publish export artifact", ErrRecordPortabilityUnavailable)
	}
	return published, nil
}

func (repository *PostgresRecordPortabilityRepository) LoadExportArtifact(
	ctx context.Context,
	exportJobID string,
) (RecordExportArtifact, error) {
	if ctx == nil || repository == nil || repository.platform == nil || exportJobID == "" {
		return RecordExportArtifact{}, ErrRecordPortabilityUnavailable
	}
	var artifact RecordExportArtifact
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var digest []byte
		var byteSize int64
		var revokedAt *time.Time
		err := transaction.tx.QueryRow(ctx, `
			select artifact.export_artifact_id, artifact.export_job_id, artifact.artifact_kind,
			       artifact.content_type, artifact.backend_kind, artifact.blob_key, artifact.sha256,
			       artifact.byte_size, artifact.expires_at, artifact.revoked_at, artifact.created_at
			from public.record_export_artifacts as artifact
			join public.record_export_jobs as job on job.export_job_id = artifact.export_job_id
			where artifact.export_job_id = $1 and artifact.artifact_kind = job.export_kind
		`, exportJobID).Scan(
			&artifact.ArtifactID,
			&artifact.ExportJobID,
			&artifact.ArtifactKind,
			&artifact.ContentType,
			&artifact.BackendKind,
			&artifact.BlobKey,
			&digest,
			&byteSize,
			&artifact.ExpiresAt,
			&revokedAt,
			&artifact.CreatedAt,
		)
		if err != nil {
			return err
		}
		if byteSize <= 0 || len(digest) != sha256.Size {
			return ErrRecordPortabilityUnavailable
		}
		artifact.ByteSize = uint64(byteSize)
		copy(artifact.SHA256[:], digest)
		artifact.RevokedAt = revokedAt
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordExportArtifact{}, ErrRecordExportNotFound
		}
		if errors.Is(err, ErrRecordPortabilityUnavailable) || errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
			return RecordExportArtifact{}, err
		}
		return RecordExportArtifact{}, fmt.Errorf("%w: load export artifact", ErrRecordPortabilityUnavailable)
	}
	return artifact, nil
}

func (repository *PostgresRecordPortabilityRepository) RevokeExport(
	ctx context.Context,
	exportJobID string,
	lockVersion uint64,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || exportJobID == "" || lockVersion == 0 {
		return ErrRecordPortabilityUnavailable
	}
	now := time.Now().UTC()
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if _, err := transaction.tx.Exec(ctx, `
			update public.record_export_artifacts
			set revoked_at = $1
			where export_job_id = $2
			  and revoked_at is null
		`, now, exportJobID); err != nil {
			return fmt.Errorf("revoke export artifact: %w", err)
		}
		tag, err := transaction.tx.Exec(ctx, `
			update public.record_export_jobs
			set job_state = $1,
			    lock_version = lock_version + 1,
			    updated_at = now()
			where export_job_id = $2
			  and lock_version = $3
		`, RecordExportJobStateRevoked, exportJobID, int64(lockVersion))
		if err != nil {
			return fmt.Errorf("revoke export job: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrRecordExportJobCASConflict
		}
		return nil
	})
}

func validateClaimRecordExportJobInput(input ClaimRecordExportJobInput) error {
	if recordauth.ValidateActorUserID(input.ActorID) != nil ||
		input.IdempotencyKey == "" ||
		!knownRecordExportKind(input.ExportKind) ||
		!knownRecordExportMode(input.ExportMode) ||
		input.RequestFingerprint == [32]byte{} ||
		input.InventoryDigest == [32]byte{} ||
		input.AuthorizationEpoch == 0 ||
		input.ExpiresAt.IsZero() {
		return ErrRecordPortabilityUnavailable
	}
	if input.RevisionID != "" && input.RecordID == "" {
		return ErrRecordPortabilityUnavailable
	}
	return nil
}

func knownRecordExportKind(kind string) bool {
	switch kind {
	case RecordExportKindMarkdown, RecordExportKindComparisonJSON, RecordExportKindEvidenceJSON,
		RecordExportKindArchive, RecordExportKindPDF:
		return true
	default:
		return false
	}
}

func knownRecordExportMode(mode string) bool {
	return mode == RecordExportModeSafe || mode == RecordExportModeSensitiveTopo
}

func knownExportArtifactContentType(value string) bool {
	switch value {
	case "text/markdown", "application/json", "application/zip", "application/pdf":
		return true
	default:
		return false
	}
}

func knownRecordExportJobState(state string) bool {
	switch state {
	case RecordExportJobStatePreviewed, RecordExportJobStateStaging, RecordExportJobStatePublished,
		RecordExportJobStateExpired, RecordExportJobStateRevoked, RecordExportJobStateFailed:
		return true
	default:
		return false
	}
}

func loadRecordExportJobByID(ctx context.Context, tx pgx.Tx, exportJobID string) (RecordExportJob, error) {
	return scanRecordExportJob(ctx, tx, `
		select export_job_id, project_id, actor_id, idempotency_key, export_kind, export_mode,
		       job_state, failure_code, lock_version, request_fingerprint, inventory_digest,
		       authorization_epoch, coalesce(record_id, ''), coalesce(revision_id, ''),
		       expires_at, created_at, updated_at
		from public.record_export_jobs
		where export_job_id = $1
	`, exportJobID)
}

func loadRecordExportJobByIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	actorID, idempotencyKey string,
) (RecordExportJob, error) {
	return scanRecordExportJob(ctx, tx, `
		select export_job_id, project_id, actor_id, idempotency_key, export_kind, export_mode,
		       job_state, failure_code, lock_version, request_fingerprint, inventory_digest,
		       authorization_epoch, coalesce(record_id, ''), coalesce(revision_id, ''),
		       expires_at, created_at, updated_at
		from public.record_export_jobs
		where project_id = $1
		  and actor_id = $2
		  and idempotency_key = $3
	`, string(recordauth.ProjectIDDefault), actorID, idempotencyKey)
}

func scanRecordExportJob(ctx context.Context, tx pgx.Tx, query string, args ...any) (RecordExportJob, error) {
	var job RecordExportJob
	var fingerprint, inventory []byte
	var lockVersion, authorizationEpoch int64
	err := tx.QueryRow(ctx, query, args...).Scan(
		&job.ExportJobID,
		&job.ProjectID,
		&job.ActorID,
		&job.IdempotencyKey,
		&job.ExportKind,
		&job.ExportMode,
		&job.JobState,
		&job.FailureCode,
		&lockVersion,
		&fingerprint,
		&inventory,
		&authorizationEpoch,
		&job.RecordID,
		&job.RevisionID,
		&job.ExpiresAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return RecordExportJob{}, err
	}
	if lockVersion <= 0 || authorizationEpoch <= 0 ||
		len(fingerprint) != sha256.Size || len(inventory) != sha256.Size {
		return RecordExportJob{}, ErrRecordPortabilityUnavailable
	}
	job.LockVersion = uint64(lockVersion)
	job.AuthorizationEpoch = uint64(authorizationEpoch)
	copy(job.RequestFingerprint[:], fingerprint)
	copy(job.InventoryDigest[:], inventory)
	return job, nil
}

func insertRecordExportJob(ctx context.Context, tx pgx.Tx, job RecordExportJob) error {
	_, err := tx.Exec(ctx, `
		insert into public.record_export_jobs (
			export_job_id, project_id, actor_id, idempotency_key, export_kind, export_mode,
			job_state, failure_code, lock_version, request_fingerprint, inventory_digest,
			authorization_epoch, record_id, revision_id, expires_at, created_at, updated_at
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			nullif($13, ''), nullif($14, ''), $15, $16, $17
		)
	`,
		job.ExportJobID,
		string(job.ProjectID),
		job.ActorID,
		job.IdempotencyKey,
		job.ExportKind,
		job.ExportMode,
		job.JobState,
		job.FailureCode,
		int64(job.LockVersion),
		job.RequestFingerprint[:],
		job.InventoryDigest[:],
		int64(job.AuthorizationEpoch),
		job.RecordID,
		job.RevisionID,
		job.ExpiresAt,
		job.CreatedAt,
		job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert export job: %w", err)
	}
	return nil
}

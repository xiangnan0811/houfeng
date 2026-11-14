package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
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

type artifactPublicationCommand struct {
	ArtifactID   string    `json:"artifact_id"`
	JobID        string    `json:"job_id"`
	Kind         string    `json:"kind"`
	ContentType  string    `json:"content_type,omitempty"`
	BackendKind  string    `json:"backend_kind"`
	NamespaceID  string    `json:"namespace_id"`
	Key          string    `json:"key"`
	Version      string    `json:"version"`
	SHA256       string    `json:"sha256"`
	Size         uint64    `json:"size"`
	ExpiresAt    time.Time `json:"expires_at"`
	ObligationID string    `json:"obligation_id"`
}

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
	OwnerID            string
	OwnerGeneration    int64
	OwnerExpiresAt     time.Time
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ClaimRecordExportJobInput struct {
	Actor              recordauth.ActorScope
	IdempotencyKey     string
	ExportKind         string
	ExportMode         string
	RequestFingerprint [32]byte
	InventoryDigest    [32]byte
	AuthorizationEpoch uint64
	RecordLockVersion  uint64
	RecordID           string
	RevisionID         string
	ExpiresAt          time.Time
}

type AdvanceRecordExportJobInput struct {
	Actor             recordauth.ActorScope
	ExpectedJob       RecordExportJob
	RecordLockVersion uint64
	JobState          string
	FailureCode       string
}

type RecordExportArtifact struct {
	ArtifactID      string
	ExportJobID     string
	ArtifactKind    string
	ContentType     string
	BackendKind     string
	BlobKey         string
	ObjectVersionID string
	NamespaceID     string
	ObligationID    string
	SHA256          [32]byte
	ByteSize        uint64
	ExpiresAt       time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
}

type PublishRecordExportArtifactInput struct {
	ExportJobID     string
	ArtifactKind    string
	ContentType     string
	BackendKind     string
	BlobKey         string
	ObjectVersionID string
	NamespaceID     string
	ObligationID    string
	SHA256          [32]byte
	ByteSize        uint64
	ExpiresAt       time.Time
}

type PostgresRecordPortabilityRepository struct {
	pool                *pgxpool.Pool
	platform            *PostgresRecordPlatformRepository
	witness             *WitnessedRecordSubjectTombstoneReader
	continuity          recordbirth.ContinuitySource
	authorization       *PostgresCurrentRecordAuthorizationSource
	newExportJobID      func() (string, error)
	newExportArtifactID func() (string, error)
	newImportArtifactID func() (string, error)
}

func NewPostgresRecordPortabilityRepository(pool *pgxpool.Pool, gate AdmissionGate, witness *WitnessedRecordSubjectTombstoneReader, continuity recordbirth.ContinuitySource) *PostgresRecordPortabilityRepository {
	return &PostgresRecordPortabilityRepository{
		pool:     pool,
		platform: NewPostgresRecordPlatformRepository(pool, gate),
		authorization: newPostgresCurrentRecordAuthorizationSource(pool,
			&RecordSubjectReadResolver{tombstones: witness}, gate),
		witness:    witness,
		continuity: continuity,
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
	actor, _ := recordauth.NormalizeActorScope(input.Actor)
	prepared, err := repository.prepareExportAuthorization(ctx, input.RecordID, input.RevisionID)
	if err != nil {
		return RecordExportJob{}, err
	}
	var claimed RecordExportJob
	var observedJobID string
	err = repository.platform.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claimed = RecordExportJob{}
		existing, lookupErr := loadRecordExportJobByIdempotency(ctx, transaction.tx, actor.UserID, input.IdempotencyKey)
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return lookupErr
		}
		var observed *RecordExportJob
		if lookupErr == nil {
			if (observedJobID != "" && existing.ExportJobID != observedJobID) || !exportJobMatchesClaim(existing, actor, input) {
				return ErrRecordExportJobConflict
			}
			observedJobID = existing.ExportJobID
			observed = &existing
		} else if observedJobID != "" {
			return ErrRecordExportJobConflict
		}
		if err := acquireExportJobMutation(ctx, transaction.tx, input.RecordID, input.RevisionID, actor.UserID, observed, false); err != nil {
			return err
		}
		// The rank-10 actor row serializes absent actor/key claims, even across
		// records. A newly discovered job needs a fresh rank-71 acquisition.
		current, currentErr := loadRecordExportJobByIdempotency(ctx, transaction.tx, actor.UserID, input.IdempotencyKey)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			if currentErr == nil {
				return exportJobRouteChanged()
			}
			if !errors.Is(currentErr, pgx.ErrNoRows) {
				return currentErr
			}
		} else if currentErr != nil {
			if errors.Is(currentErr, pgx.ErrNoRows) {
				return ErrRecordExportJobConflict
			}
			return currentErr
		} else if !sameExportJobRoute(existing, current) {
			// Never retry an observed job as a fresh allocation under the same key.
			return ErrRecordExportJobConflict
		}
		if err := prepared.authorize(ctx, transaction.tx, actor, input.RecordID, input.RevisionID,
			input.ExportMode, input.AuthorizationEpoch, input.RecordLockVersion); err != nil {
			return err
		}
		if observed != nil {
			claimed = current
			return nil
		}
		var live bool
		if err := transaction.tx.QueryRow(ctx, `select clock_timestamp() < $1`, input.ExpiresAt.UTC()).Scan(&live); err != nil {
			return err
		}
		if !live {
			return ErrRecordExportJobConflict
		}
		jobID, err := repository.newExportJobID()
		if err != nil || jobID == "" {
			return ErrRecordPortabilityUnavailable
		}
		now := time.Now().UTC()
		job := RecordExportJob{
			ExportJobID: jobID, ProjectID: actor.ProjectID, ActorID: actor.UserID,
			IdempotencyKey: input.IdempotencyKey, ExportKind: input.ExportKind, ExportMode: input.ExportMode,
			JobState: RecordExportJobStatePreviewed, LockVersion: 1,
			RequestFingerprint: input.RequestFingerprint, InventoryDigest: input.InventoryDigest,
			AuthorizationEpoch: input.AuthorizationEpoch, RecordID: input.RecordID, RevisionID: input.RevisionID,
			ExpiresAt: input.ExpiresAt.UTC(), CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRecordExportJob(ctx, transaction.tx, job, input.RecordLockVersion); err != nil {
			return err
		}
		stored, err := loadRecordExportJobByID(ctx, transaction.tx, job.ExportJobID)
		if err != nil {
			return err
		}
		claimed = stored
		return nil
	})
	if err != nil {
		return RecordExportJob{}, fmt.Errorf("claim export job: %w", err)
	}
	return claimed, nil
}

func (repository *PostgresRecordPortabilityRepository) AdvanceExportJob(
	ctx context.Context,
	input AdvanceRecordExportJobInput,
) error {
	if ctx == nil || repository == nil || repository.platform == nil {
		return ErrRecordPortabilityUnavailable
	}
	actor, err := recordauth.NormalizeActorScope(input.Actor)
	job := input.ExpectedJob
	if err != nil || actor.ProjectID != job.ProjectID || actor.UserID != job.ActorID ||
		job.ExportJobID == "" || job.LockVersion == 0 || job.LockVersion > math.MaxInt64 || !recordidentity.IsValid(job.RecordID) ||
		!validStoredRecordIdentity(job.RevisionID, "rrv_") || job.AuthorizationEpoch == 0 || job.AuthorizationEpoch > math.MaxInt64 || job.ExpiresAt.IsZero() {
		return ErrRecordPortabilityUnavailable
	}
	// This caller has no export-job lease. A leased job belongs to a distinct
	// owner and cannot be advanced or finalized by the actor's old CAS alone.
	if job.OwnerID != "" || job.OwnerGeneration != 0 || !job.OwnerExpiresAt.IsZero() {
		return ErrRecordExportJobCASConflict
	}
	terminal := input.JobState == RecordExportJobStateFailed && input.FailureCode == "staging_failed" &&
		job.JobState == RecordExportJobStateStaging
	if !terminal && !((job.JobState == RecordExportJobStatePreviewed && input.JobState == RecordExportJobStateStaging) ||
		(job.JobState == RecordExportJobStateStaging && input.JobState == RecordExportJobStatePublished)) {
		return ErrRecordExportJobCASConflict
	}
	if !terminal && (input.FailureCode != "" || input.RecordLockVersion == 0 || input.RecordLockVersion > math.MaxInt64) {
		return ErrRecordPortabilityUnavailable
	}
	var prepared *preparedExportAuthorization
	if !terminal {
		prepared, err = repository.prepareExportAuthorization(ctx, job.RecordID, job.RevisionID)
		if err != nil {
			return err
		}
	}
	return repository.platform.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		observed, err := loadRecordExportJobByID(ctx, transaction.tx, job.ExportJobID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRecordExportJobCASConflict
		}
		if err != nil {
			return err
		}
		if !sameExportJobRoute(job, observed) {
			return ErrRecordExportJobCASConflict
		}
		if err := acquireExportJobMutation(ctx, transaction.tx, job.RecordID, job.RevisionID, actor.UserID, &observed, terminal); err != nil {
			return err
		}
		current, err := loadRecordExportJobByID(ctx, transaction.tx, job.ExportJobID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRecordExportJobCASConflict
		}
		if err != nil {
			return err
		}
		if !sameExportJobRoute(job, current) {
			return ErrRecordExportJobCASConflict
		}
		if !terminal {
			if err := prepared.authorize(ctx, transaction.tx, actor, job.RecordID, job.RevisionID,
				job.ExportMode, job.AuthorizationEpoch, input.RecordLockVersion); err != nil {
				return err
			}
		}
		command := exportJobIdentityCommand(job)
		command["expected_state"] = job.JobState
		command["expected_lock_version"] = strconv.FormatUint(job.LockVersion, 10)
		command["new_state"] = input.JobState
		command["failure_code"] = input.FailureCode
		if terminal {
			command["record_lock_version"] = "0"
		} else {
			command["record_lock_version"] = strconv.FormatUint(input.RecordLockVersion, 10)
		}
		data, err := json.Marshal(command)
		if err != nil {
			return err
		}
		tag, err := transaction.tx.Exec(ctx, `select public.record_export_job_advance($1)`, data)
		if err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) && postgresError.Code == "55000" {
				return fmt.Errorf("%w: %w", ErrRecordExportJobCASConflict, err)
			}
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
		input.ExportJobID == "" || input.BlobKey == "" || input.ObjectVersionID == "" || input.ObligationID == "" || input.ByteSize == 0 ||
		input.SHA256 == [32]byte{} || input.ExpiresAt.IsZero() {
		return RecordExportArtifact{}, ErrRecordPortabilityUnavailable
	}
	var published RecordExportArtifact
	err := repository.platform.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		published = RecordExportArtifact{}
		if err := lockAttachmentWriterPrefix(ctx, transaction.tx, attachmentWriterRoute{
			obligationIDs: []string{input.ObligationID}, namespaces: []string{input.NamespaceID},
		}); err != nil {
			return err
		}
		artifactID, err := repository.newExportArtifactID()
		if err != nil || artifactID == "" {
			return ErrRecordPortabilityUnavailable
		}
		artifact := RecordExportArtifact{
			ArtifactID:      artifactID,
			ExportJobID:     input.ExportJobID,
			ArtifactKind:    input.ArtifactKind,
			ContentType:     input.ContentType,
			BackendKind:     input.BackendKind,
			BlobKey:         input.BlobKey,
			ObjectVersionID: input.ObjectVersionID,
			NamespaceID:     input.NamespaceID,
			ObligationID:    input.ObligationID,
			SHA256:          input.SHA256,
			ByteSize:        input.ByteSize,
			ExpiresAt:       input.ExpiresAt.UTC().Truncate(time.Microsecond),
		}
		command, err := json.Marshal(artifactPublicationCommand{
			ArtifactID: artifact.ArtifactID, JobID: artifact.ExportJobID, Kind: artifact.ArtifactKind,
			ContentType: artifact.ContentType, BackendKind: artifact.BackendKind, NamespaceID: artifact.NamespaceID,
			Key: artifact.BlobKey, Version: artifact.ObjectVersionID, SHA256: hex.EncodeToString(artifact.SHA256[:]),
			Size: artifact.ByteSize, ExpiresAt: artifact.ExpiresAt, ObligationID: artifact.ObligationID,
		})
		if err != nil {
			return err
		}
		if err := transaction.tx.QueryRow(ctx, `select export_artifact_id, created_at from public.record_export_artifact_publish($1)`, command).
			Scan(&artifact.ArtifactID, &artifact.CreatedAt); err != nil {
			return fmt.Errorf("publish export artifact: %w", err)
		}
		published = artifact
		return nil
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "40001" {
			return RecordExportArtifact{}, err
		}
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
			       artifact.byte_size, artifact.expires_at, artifact.revoked_at, artifact.created_at,
			       coalesce(artifact.object_version_id, ''), coalesce(artifact.namespace_id::text, ''), coalesce(artifact.obligation_id, '')
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
			&artifact.ObjectVersionID, &artifact.NamespaceID, &artifact.ObligationID,
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
	command, err := json.Marshal(struct {
		JobID       string `json:"job_id"`
		LockVersion uint64 `json:"lock_version"`
	}{exportJobID, lockVersion})
	if err != nil {
		return err
	}
	return repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var changed int64
		if err := transaction.tx.QueryRow(ctx, `select public.record_export_artifact_revoke($1)`, command).Scan(&changed); err != nil {
			return fmt.Errorf("revoke export artifact and job: %w", err)
		}
		if changed != 1 {
			return ErrRecordExportJobCASConflict
		}
		return nil
	})
}

func validateClaimRecordExportJobInput(input ClaimRecordExportJobInput) error {
	actor, err := recordauth.NormalizeActorScope(input.Actor)
	if err != nil || actor.ProjectID != recordauth.ProjectIDDefault ||
		input.IdempotencyKey == "" || !knownRecordExportKind(input.ExportKind) || !knownRecordExportMode(input.ExportMode) ||
		input.RequestFingerprint == [32]byte{} || input.InventoryDigest == [32]byte{} ||
		input.AuthorizationEpoch == 0 || input.AuthorizationEpoch > math.MaxInt64 ||
		input.RecordLockVersion == 0 || input.RecordLockVersion > math.MaxInt64 ||
		!recordidentity.IsValid(input.RecordID) || !validStoredRecordIdentity(input.RevisionID, "rrv_") || input.ExpiresAt.IsZero() {
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

func loadRecordExportJobByID(ctx context.Context, tx pgx.Tx, exportJobID string) (RecordExportJob, error) {
	return scanRecordExportJob(ctx, tx, `
		select export_job_id, project_id, actor_id, idempotency_key, export_kind, export_mode,
		       job_state, failure_code, lock_version, request_fingerprint, inventory_digest,
		       authorization_epoch, coalesce(record_id, ''), coalesce(revision_id, ''),
		       owner_id, owner_generation, coalesce(lease_expires_at,'0001-01-01 00:00:00+00'::timestamptz),
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
		       owner_id, owner_generation, coalesce(lease_expires_at,'0001-01-01 00:00:00+00'::timestamptz),
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
		&job.OwnerID,
		&job.OwnerGeneration,
		&job.OwnerExpiresAt,
		&job.ExpiresAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return RecordExportJob{}, err
	}
	if lockVersion <= 0 || authorizationEpoch <= 0 || job.OwnerGeneration < 0 ||
		len(fingerprint) != sha256.Size || len(inventory) != sha256.Size {
		return RecordExportJob{}, ErrRecordPortabilityUnavailable
	}
	job.LockVersion = uint64(lockVersion)
	job.AuthorizationEpoch = uint64(authorizationEpoch)
	copy(job.RequestFingerprint[:], fingerprint)
	copy(job.InventoryDigest[:], inventory)
	return job, nil
}

func exportJobIdentityCommand(job RecordExportJob) map[string]string {
	return map[string]string{
		"export_job_id": job.ExportJobID, "project_id": string(job.ProjectID), "actor_id": job.ActorID,
		"idempotency_key": job.IdempotencyKey, "export_kind": job.ExportKind, "export_mode": job.ExportMode,
		"request_fingerprint": hex.EncodeToString(job.RequestFingerprint[:]), "inventory_digest": hex.EncodeToString(job.InventoryDigest[:]),
		"authorization_epoch": strconv.FormatUint(job.AuthorizationEpoch, 10), "record_id": job.RecordID, "revision_id": job.RevisionID,
		"expires_at": job.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func insertRecordExportJob(ctx context.Context, tx pgx.Tx, job RecordExportJob, recordLockVersion uint64) error {
	command := exportJobIdentityCommand(job)
	command["created_at"] = job.CreatedAt.UTC().Format(time.RFC3339Nano)
	command["updated_at"] = job.UpdatedAt.UTC().Format(time.RFC3339Nano)
	command["record_lock_version"] = strconv.FormatUint(recordLockVersion, 10)
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select public.record_export_job_claim($1)`, data); err != nil {
		return fmt.Errorf("insert export job: %w", err)
	}
	return nil
}

// Export material is prepared before this transaction. Its record and revision
// authority must be prefetched there as well; only APP facts are read while the
// asset graph and ranked writer locks are held.
type preparedExportAuthorization struct {
	current   currentRecordAuthorizationTransactionResolver
	revision  recordRevisionAuthorizationSnapshot
	authority *PreparedSourceAuthority
}

func (repository *PostgresRecordPortabilityRepository) prepareExportAuthorization(ctx context.Context, recordID, revisionID string) (*preparedExportAuthorization, error) {
	if repository == nil || repository.authorization == nil || repository.witness == nil {
		return nil, ErrRecordPortabilityUnavailable
	}
	current, err := repository.authorization.prepareCurrentAuthorization(ctx, []string{recordID})
	if err != nil {
		return nil, err
	}
	revision, err := repository.authorization.loadRevision(ctx, recordID, revisionID)
	if err != nil {
		return nil, err
	}
	keys := make([]sourceretirement.SourceKey, 0, len(revision.subjects))
	for _, subject := range revision.subjects {
		kind, ok := recordSubjectSourceKind(subject.Reference.Kind)
		if !ok {
			return nil, ErrRecordSubjectUnavailable
		}
		keys = append(keys, repository.witness.sourceKey(revision.projectID, kind, subject.Reference.SourceID))
	}
	var authority *PreparedSourceAuthority
	if prepared, ok := current.(*preparedCurrentRecordAuthorization); ok && prepared.authority != nil {
		covered := true
		for _, key := range keys {
			if _, ok := prepared.authority.observations[key]; !ok {
				covered = false
				break
			}
		}
		if covered {
			authority = prepared.authority
		}
	}
	if authority == nil {
		authority, err = repository.witness.Prefetch(ctx, keys)
		if err != nil {
			return nil, err
		}
	}
	return &preparedExportAuthorization{current: current, revision: revision, authority: authority}, nil
}

func (prepared *preparedExportAuthorization) authorize(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope,
	recordID, revisionID, mode string, authorizationEpoch, lockVersion uint64) error {
	member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, tx, actor.ProjectID, actor.UserID)
	if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
		return recordauth.ErrDenied
	}
	if err != nil {
		return err
	}
	if member.CanonicalHash() != actor.CanonicalHash() {
		return recordauth.ErrDenied
	}
	if err := recordauth.AllowsCapability(member, recordauth.CapabilityExport); err != nil {
		return err
	}
	if mode == RecordExportModeSensitiveTopo {
		if err := recordauth.AllowsCapability(member, recordauth.CapabilityExportSensitiveTopology); err != nil {
			return err
		}
	} else if mode != RecordExportModeSafe {
		return ErrRecordPortabilityUnavailable
	}
	current, err := prepared.current.resolveCurrentAuthorizationInTransaction(ctx, tx, member, recordID)
	if err != nil {
		return err
	}
	if current.RecordID != recordID || current.AuthorizationEpoch != authorizationEpoch || current.LockVersion != lockVersion ||
		records.ValidateLifecycle(current.Lifecycle) != nil {
		return ErrRecordExportJobConflict
	}
	if err := records.AuthorizeRecordResource(member, recordauth.CapabilityRecordRead, current.Evidence); err != nil {
		return err
	}
	loader := &PostgresCurrentRecordAuthorizationSource{db: tx}
	revision, err := loader.loadRecordRevisionAuthorizationSnapshot(ctx, recordID, revisionID)
	if err != nil {
		return err
	}
	if revision.projectID != member.ProjectID || revision.currentRevisionID != current.CurrentRevisionID ||
		revision.authorizationEpoch != current.AuthorizationEpoch || revision.lockVersion != current.LockVersion ||
		revision.lifecycle != current.Lifecycle || len(revision.subjects) != len(prepared.revision.subjects) {
		return ErrRecordExportJobConflict
	}
	for index, subject := range revision.subjects {
		if subject.Reference != prepared.revision.subjects[index].Reference {
			return ErrRecordExportJobConflict
		}
	}
	live := transactionRecordSubjectSource{db: tx}
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live), newMonitoringInstanceRecordSubjectAdapter(live), newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		return err
	}
	loader.resolver = NewRecordSubjectReadResolver(registry, prepared.authority.ForTransaction(tx))
	sources, err := loader.resolveRecordAuthorizationSubjects(ctx, member, revision.projectID, revision.subjects)
	if err != nil {
		return err
	}
	return records.AuthorizeRecordResource(member, recordauth.CapabilityRecordRead, records.RecordAuthorizationEvidence{
		ProjectID: revision.projectID, Visibility: revision.visibility, Sources: sources,
	})
}

type exportMutationObservation struct {
	resources recordMutationResources
	revision  recordRevisionAuthorizationSnapshot
}

func discoverExportJobMutation(ctx context.Context, tx pgx.Tx, recordID, revisionID, actorID string,
	job *RecordExportJob, terminal bool) (exportMutationObservation, error) {
	var users []string
	if !terminal {
		users = []string{actorID}
	}
	set, err := discoverEffectRecordResources(ctx, tx, []string{recordID}, users)
	if err != nil {
		return exportMutationObservation{}, err
	}
	var revision recordRevisionAuthorizationSnapshot
	if !terminal {
		loader := &PostgresCurrentRecordAuthorizationSource{db: tx}
		revision, err = loader.loadRecordRevisionAuthorizationSnapshot(ctx, recordID, revisionID)
		if err != nil {
			return exportMutationObservation{}, err
		}
		appendRevisionMutationVisibility(&set, revision.visibility)
		for _, subject := range revision.subjects {
			appendRevisionMutationSource(&set, subject.CaptureAuthorization)
		}
		set.Revisions = append(set.Revisions, records.RevisionMutationRevision{RecordID: recordID, RevisionID: revisionID})
	}
	if job != nil {
		set.ExportJobs = append(set.ExportJobs, records.RevisionMutationExportJob{
			ProjectID: string(job.ProjectID), JobID: job.ExportJobID, ActorID: job.ActorID, State: job.JobState, OwnerID: job.OwnerID,
			RecordID: job.RecordID, RevisionID: job.RevisionID, LockVersion: int64(job.LockVersion), OwnerGeneration: job.OwnerGeneration,
			LeaseExpiresAt: job.OwnerExpiresAt, AuthorizationEpoch: int64(job.AuthorizationEpoch), RequestFingerprint: job.RequestFingerprint, InventoryDigest: job.InventoryDigest,
		})
	}
	resources, err := normalizeRecordMutationDeclarations(set)
	if err != nil {
		return exportMutationObservation{}, err
	}
	return exportMutationObservation{resources: resources, revision: revision}, nil
}

func acquireExportJobMutation(ctx context.Context, tx pgx.Tx, recordID, revisionID, actorID string,
	job *RecordExportJob, terminal bool) error {
	before, err := discoverExportJobMutation(ctx, tx, recordID, revisionID, actorID, job, terminal)
	if err != nil {
		return err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return err
	}
	if terminal {
		err = acquireTerminalRecordMutationResources(ctx, tx, before.resources)
		if err == nil {
			err = acquireRecordMutationResourceRange(ctx, tx, before.resources, 70, 75)
		}
	} else {
		err = acquireRecordMutationResources(ctx, tx, before.resources)
	}
	if err != nil {
		return err
	}
	after, err := discoverExportJobMutation(ctx, tx, recordID, revisionID, actorID, job, terminal)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before.resources.resources, after.resources.resources) || !reflect.DeepEqual(before.revision, after.revision) {
		return exportJobRouteChanged()
	}
	return nil
}

func exportJobRouteChanged() error {
	return &pgconn.PgError{Code: "40001", Message: "export job or authorization route changed; restart transaction"}
}

func exportJobMatchesClaim(job RecordExportJob, actor recordauth.ActorScope, input ClaimRecordExportJobInput) bool {
	return job.ProjectID == actor.ProjectID && job.ActorID == actor.UserID && job.IdempotencyKey == input.IdempotencyKey &&
		job.RecordID == input.RecordID && job.RevisionID == input.RevisionID && job.AuthorizationEpoch == input.AuthorizationEpoch &&
		job.ExportKind == input.ExportKind && job.ExportMode == input.ExportMode &&
		job.RequestFingerprint == input.RequestFingerprint && job.InventoryDigest == input.InventoryDigest
}

func sameExportJobRoute(a, b RecordExportJob) bool {
	return a.ExportJobID == b.ExportJobID && a.ProjectID == b.ProjectID && a.ActorID == b.ActorID &&
		a.IdempotencyKey == b.IdempotencyKey && a.ExportKind == b.ExportKind && a.ExportMode == b.ExportMode &&
		a.JobState == b.JobState && a.FailureCode == b.FailureCode && a.LockVersion == b.LockVersion &&
		a.RequestFingerprint == b.RequestFingerprint && a.InventoryDigest == b.InventoryDigest &&
		a.AuthorizationEpoch == b.AuthorizationEpoch && a.RecordID == b.RecordID && a.RevisionID == b.RevisionID &&
		a.OwnerID == b.OwnerID && a.OwnerGeneration == b.OwnerGeneration && a.OwnerExpiresAt.Equal(b.OwnerExpiresAt) &&
		a.ExpiresAt.Equal(b.ExpiresAt)
}

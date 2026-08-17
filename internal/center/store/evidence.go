package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

const (
	EvidencePayloadEncodingCanonicalJSONGzipV1 = "canonical_json_gzip_v1"
	EvidencePayloadOrphanGracePeriod           = 24 * time.Hour

	maxEvidenceLifecycleBatchSize = uint64(100)
	maxEvidenceCompressedBytes    = uint64(6 * 1024 * 1024)

	evidencePayloadVersionDigestDomainV1   = "houfeng.evidence.payload-version.v1"
	evidencePayloadGCReceiptDigestDomainV1 = "houfeng.evidence.payload-gc-receipt.v1"
)

var (
	ErrInvalidEvidencePersistence  = errors.New("invalid evidence persistence input")
	ErrEvidencePersistenceConflict = errors.New("evidence persistence conflict")
)

type EvidencePayloadMetadata struct {
	Digest              [sha256.Size]byte
	Encoding            string
	CanonicalSizeBytes  uint64
	CompressedSizeBytes uint64
}

type EvidencePayloadGCReceipt = evidence.PayloadGCReceipt

// PostgresEvidenceRepository owns capture-intent and content-addressed payload
// lifecycle primitives. Worker scheduling and revision participation live at
// higher layers.
type PostgresEvidenceRepository struct {
	platform             *PostgresRecordPlatformRepository
	registry             evidence.Registry
	current              evidenceCurrentRecordAuthorizationSource
	subjects             currentRecordSubjectResolver
	loadEvidenceSnapshot func(context.Context, string, bool) (persistedEvidenceSnapshot, error)
}

var (
	_ evidence.CaptureIntentBindingSource = (*PostgresEvidenceRepository)(nil)
	_ evidence.CapturePayloadSink         = (*PostgresEvidenceRepository)(nil)
	_ evidence.ProjectCapacitySource      = (*PostgresEvidenceRepository)(nil)
	_ evidence.MaintenanceRepository      = (*PostgresEvidenceRepository)(nil)
)

func NewPostgresEvidenceRepository(pool *pgxpool.Pool, gate AdmissionGate) *PostgresEvidenceRepository {
	return &PostgresEvidenceRepository{platform: NewPostgresRecordPlatformRepository(pool, gate)}
}

func (repository *PostgresEvidenceRepository) LoadCaptureIntentBinding(
	ctx context.Context,
	recordID string,
	intentID string,
) (evidence.CaptureIntentBinding, error) {
	if ctx == nil || !validEvidenceStoreID(recordID, "rec_") || !evidence.ValidCaptureIntentID(intentID) {
		return evidence.CaptureIntentBinding{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.CaptureIntentBinding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		persistedRecordID, kind, snapshotID, persistedIntentID string
		schemaVersion, estimatedSize                           int64
		previewDigest, sourceDigest                            []byte
		selectionJSON, previewJSON                             []byte
		createdAt, validUntil                                  time.Time
	)
	err = tx.QueryRow(ctx, `
		select record_id, kind, schema_version, preview_digest,
		       source_digest, selection, preview,
		       coalesce(snapshot_id, ''), estimated_size_bytes,
		       created_at, valid_until, intent_id
		from public.evidence_capture_intents
		where record_id = $1
		  and intent_id = $2
		  and valid_until > transaction_timestamp()`,
		recordID,
		intentID,
	).Scan(
		&persistedRecordID,
		&kind,
		&schemaVersion,
		&previewDigest,
		&sourceDigest,
		&selectionJSON,
		&previewJSON,
		&snapshotID,
		&estimatedSize,
		&createdAt,
		&validUntil,
		&persistedIntentID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return evidence.CaptureIntentBinding{}, evidence.ErrCaptureIntentUnavailable
	}
	if err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("load evidence capture intent binding: %w", err)
	}
	if persistedRecordID != recordID || persistedIntentID != intentID ||
		schemaVersion < 1 || schemaVersion > int64(^uint16(0)) ||
		len(previewDigest) != sha256.Size || len(sourceDigest) != sha256.Size ||
		!evidence.ValidSnapshotID(snapshotID) || estimatedSize < 1 ||
		uint64(estimatedSize) > evidence.MaxCanonicalPayloadBytes ||
		len(selectionJSON) == 0 || uint64(len(selectionJSON)) > evidence.MaxCanonicalPayloadBytes ||
		len(previewJSON) == 0 || uint64(len(previewJSON)) > evidence.MaxCanonicalPayloadBytes ||
		!evidencePostgresTimestampExact(createdAt) || !evidencePostgresTimestampExact(validUntil) {
		return evidence.CaptureIntentBinding{}, ErrEvidencePersistenceConflict
	}
	var selection evidence.Selection
	if err := json.Unmarshal(selectionJSON, &selection); err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence selection: %w", ErrEvidencePersistenceConflict, err)
	}
	if !equalEvidenceJSON(selectionJSON, selection) {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence selection", ErrEvidencePersistenceConflict)
	}
	var preview evidence.Preview
	if err := json.Unmarshal(previewJSON, &preview); err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence preview: %w", ErrEvidencePersistenceConflict, err)
	}
	if !equalEvidenceJSON(previewJSON, preview) {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence preview", ErrEvidencePersistenceConflict)
	}
	key := evidence.KindKey{Kind: evidence.KindName(kind), SchemaVersion: evidence.SchemaVersion(schemaVersion)}
	intent := evidence.Intent{
		ID: persistedIntentID, Key: key, Selection: selection, ValidUntil: validUntil.UTC(),
	}
	copy(intent.PreviewDigest[:], previewDigest)
	normalizedIntent, normalizedPreview := normalizeEvidenceCaptureIntentTimestamps(intent, preview)
	if !reflect.DeepEqual(intent, normalizedIntent) || !reflect.DeepEqual(preview, normalizedPreview) ||
		!evidenceCaptureIntentTimestampsExact(intent, preview) {
		return evidence.CaptureIntentBinding{}, ErrEvidencePersistenceConflict
	}
	binding := evidence.CaptureIntentBinding{
		RecordID: persistedRecordID, SnapshotID: snapshotID, Intent: intent, Preview: preview,
	}
	if binding.Validate() != nil || !bytes.Equal(sourceDigest, preview.SourceDigest[:]) ||
		estimatedSize != int64(preview.EstimatedCanonicalBytes) ||
		!createdAt.Equal(preview.PreviewedAt) || !validUntil.Equal(preview.ValidUntil) {
		return evidence.CaptureIntentBinding{}, ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("commit evidence capture intent load: %w", err)
	}
	return binding, nil
}

func (repository *PostgresEvidenceRepository) PersistCaptureIntent(
	ctx context.Context,
	recordID string,
	snapshotID string,
	intent evidence.Intent,
	preview evidence.Preview,
) error {
	intent, preview = normalizeEvidenceCaptureIntentTimestamps(intent, preview)
	if ctx == nil || !validEvidenceStoreID(recordID, "rec_") || !evidence.ValidSnapshotID(snapshotID) ||
		!evidence.ValidCaptureIntentID(intent.ID) ||
		intent.ID != preview.IntentID || intent.Key != preview.Key || intent.Key.SchemaVersion == 0 ||
		intent.Selection.Key != intent.Key || preview.Selection.Key != intent.Key ||
		!reflect.DeepEqual(intent.Selection, preview.Selection) || intent.PreviewDigest == [sha256.Size]byte{} ||
		preview.SourceDigest == [sha256.Size]byte{} || preview.EstimatedCanonicalBytes == 0 ||
		preview.EstimatedCanonicalBytes > evidence.MaxCanonicalPayloadBytes || preview.PreviewedAt.IsZero() ||
		!evidenceCaptureIntentTimestampsExact(intent, preview) ||
		!preview.ValidUntil.Equal(preview.PreviewedAt.Add(evidence.CaptureIntentTTL)) ||
		!intent.ValidUntil.Equal(preview.ValidUntil) {
		return ErrInvalidEvidencePersistence
	}
	selectionJSON, err := json.Marshal(intent.Selection)
	if err != nil {
		return fmt.Errorf("marshal evidence intent selection: %w", err)
	}
	if len(selectionJSON) == 0 || uint64(len(selectionJSON)) > evidence.MaxCanonicalPayloadBytes {
		return ErrInvalidEvidencePersistence
	}
	previewJSON, err := json.Marshal(preview)
	if err != nil {
		return fmt.Errorf("marshal evidence intent preview: %w", err)
	}
	if len(previewJSON) == 0 || uint64(len(previewJSON)) > evidence.MaxCanonicalPayloadBytes {
		return ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_capture_intents (
			intent_id, record_id, kind, schema_version, preview_digest,
			source_digest, selection, preview, snapshot_id,
			estimated_size_bytes, created_at, valid_until
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (intent_id) do nothing`,
		intent.ID,
		recordID,
		string(intent.Key.Kind),
		int64(intent.Key.SchemaVersion),
		intent.PreviewDigest[:],
		preview.SourceDigest[:],
		selectionJSON,
		previewJSON,
		snapshotID,
		int64(preview.EstimatedCanonicalBytes),
		preview.PreviewedAt.UTC(),
		preview.ValidUntil.UTC(),
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return fmt.Errorf("%w: insert evidence capture intent: %w", ErrEvidencePersistenceConflict, err)
		}
		return fmt.Errorf("insert evidence capture intent: %w", err)
	}
	if inserted.RowsAffected() != 1 {
		return ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit evidence capture intent: %w", err)
	}
	return nil
}

func (repository *PostgresEvidenceRepository) PersistPayload(
	ctx context.Context,
	snapshot evidence.CanonicalSnapshot,
) (EvidencePayloadMetadata, error) {
	canonical := snapshot.Bytes()
	digest := snapshot.Hash()
	if ctx == nil || validateEvidencePayloadBinding(canonical, digest, snapshot.Size()) != nil {
		return EvidencePayloadMetadata{}, ErrInvalidEvidencePersistence
	}
	compressed, err := deterministicEvidenceGzip(canonical)
	if err != nil {
		return EvidencePayloadMetadata{}, err
	}
	if len(compressed) == 0 || uint64(len(compressed)) > maxEvidenceCompressedBytes {
		return EvidencePayloadMetadata{}, ErrInvalidEvidencePersistence
	}
	metadata := EvidencePayloadMetadata{
		Digest: digest, Encoding: EvidencePayloadEncodingCanonicalJSONGzipV1,
		CanonicalSizeBytes: uint64(len(canonical)), CompressedSizeBytes: uint64(len(compressed)),
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return EvidencePayloadMetadata{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_payloads (
			payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload
		) values ($1, $2, $3, $4)
		on conflict (payload_digest) do nothing`,
		digest[:], int64(metadata.CanonicalSizeBytes), int64(metadata.CompressedSizeBytes), compressed,
	)
	if err != nil {
		return EvidencePayloadMetadata{}, fmt.Errorf("insert evidence payload: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var storedEncoding string
		var storedCanonicalSize, storedCompressedSize int64
		var storedCompressed []byte
		if err := tx.QueryRow(ctx, `
			select payload_encoding, canonical_size_bytes, compressed_size_bytes, compressed_payload
			from public.evidence_payloads
			where payload_digest = $1`, digest[:],
		).Scan(&storedEncoding, &storedCanonicalSize, &storedCompressedSize, &storedCompressed); err != nil {
			return EvidencePayloadMetadata{}, fmt.Errorf("read existing evidence payload: %w", err)
		}
		if storedEncoding != metadata.Encoding || storedCanonicalSize != int64(metadata.CanonicalSizeBytes) ||
			storedCompressedSize != int64(metadata.CompressedSizeBytes) || !bytes.Equal(storedCompressed, compressed) {
			return EvidencePayloadMetadata{}, ErrEvidencePersistenceConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return EvidencePayloadMetadata{}, fmt.Errorf("commit evidence payload: %w", err)
	}
	return metadata, nil
}

func (repository *PostgresEvidenceRepository) PersistCapturePayload(
	ctx context.Context,
	snapshot evidence.CanonicalSnapshot,
) error {
	_, err := repository.PersistPayload(ctx, snapshot)
	return err
}

func (repository *PostgresEvidenceRepository) ReadProjectEvidenceCapacity(
	ctx context.Context,
	projectID string,
) (evidence.ProjectCapacityUsage, error) {
	if ctx == nil || recordauth.ValidateProjectID(recordauth.ProjectID(projectID)) != nil {
		return evidence.ProjectCapacityUsage{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.ProjectCapacityUsage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var values [8]int64
	err = tx.QueryRow(ctx, `
		with project_snapshots as materialized (
			select snapshot.logical_size_bytes, snapshot.payload_digest
			from public.evidence_snapshots as snapshot
			join public.records as record on record.record_id = snapshot.record_id
			where record.project_id = $1
		), project_payloads as materialized (
			select distinct snapshot.payload_digest,
			       payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_snapshots as snapshot
			join public.records as record on record.record_id = snapshot.record_id
			join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
			where record.project_id = $1
		), global_orphans as materialized (
			select payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_payloads as payload
			where not exists (
				select 1 from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			)
		)
		select
			(select count(*)::bigint from project_snapshots),
			(select coalesce(sum(logical_size_bytes), 0)::bigint from project_snapshots),
			(select count(*)::bigint from project_payloads),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from project_payloads),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from project_payloads),
			(select count(*)::bigint from global_orphans),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from global_orphans),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from global_orphans)`,
		projectID,
	).Scan(
		&values[0], &values[1], &values[2], &values[3],
		&values[4], &values[5], &values[6], &values[7],
	)
	if err != nil {
		return evidence.ProjectCapacityUsage{}, fmt.Errorf("read project evidence capacity: %w", err)
	}
	unsigned, err := evidenceAccountingUint64(values[:]...)
	if err != nil {
		return evidence.ProjectCapacityUsage{}, err
	}
	usage := evidence.ProjectCapacityUsage{
		ProjectID:            projectID,
		LogicalSnapshotCount: unsigned[0], LogicalSnapshotBytes: unsigned[1],
		PhysicalPayloadCount: unsigned[2], PhysicalCanonicalBytes: unsigned[3], PhysicalCompressedBytes: unsigned[4],
		OrphanPayloadCount: unsigned[5], OrphanCanonicalBytes: unsigned[6], OrphanCompressedBytes: unsigned[7],
	}
	if usage.Validate(projectID) != nil {
		return evidence.ProjectCapacityUsage{}, ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.ProjectCapacityUsage{}, fmt.Errorf("commit project evidence capacity read: %w", err)
	}
	return usage, nil
}

func (repository *PostgresEvidenceRepository) ReadEvidenceCapacityAggregate(
	ctx context.Context,
) (evidence.EvidenceCapacityAggregate, error) {
	if ctx == nil {
		return evidence.EvidenceCapacityAggregate{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.EvidenceCapacityAggregate{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var values [8]int64
	err = tx.QueryRow(ctx, `
		with project_totals as materialized (
			select record.project_id,
			       coalesce(sum(snapshot.logical_size_bytes), 0)::bigint as logical_size_bytes
			from public.records as record
			left join public.evidence_snapshots as snapshot on snapshot.record_id = record.record_id
			group by record.project_id
		), referenced_payloads as materialized (
			select distinct snapshot.payload_digest,
			       payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_snapshots as snapshot
			join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
		), global_orphans as materialized (
			select payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_payloads as payload
			where not exists (
				select 1 from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			)
		)
		select
			(select count(*)::bigint from project_totals),
			(select coalesce(sum(logical_size_bytes), 0)::bigint from project_totals),
			(select coalesce(max(logical_size_bytes), 0)::bigint from project_totals),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from referenced_payloads),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from referenced_payloads),
			(select count(*)::bigint from global_orphans),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from global_orphans),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from global_orphans)`,
	).Scan(
		&values[0], &values[1], &values[2], &values[3],
		&values[4], &values[5], &values[6], &values[7],
	)
	if err != nil {
		return evidence.EvidenceCapacityAggregate{}, fmt.Errorf("read evidence capacity aggregate: %w", err)
	}
	unsigned, err := evidenceAccountingUint64(values[:]...)
	if err != nil {
		return evidence.EvidenceCapacityAggregate{}, err
	}
	aggregate := evidence.EvidenceCapacityAggregate{
		ProjectCount: unsigned[0], LogicalSnapshotBytes: unsigned[1], HighestProjectLogicalBytes: unsigned[2],
		PhysicalCanonicalBytes: unsigned[3], PhysicalCompressedBytes: unsigned[4],
		OrphanPayloadCount: unsigned[5], OrphanCanonicalBytes: unsigned[6], OrphanCompressedBytes: unsigned[7],
	}
	if aggregate.Validate() != nil {
		return evidence.EvidenceCapacityAggregate{}, ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.EvidenceCapacityAggregate{}, fmt.Errorf("commit evidence capacity aggregate read: %w", err)
	}
	return aggregate, nil
}

func (repository *PostgresEvidenceRepository) ReadEvidenceLifecycleBacklog(
	ctx context.Context,
	limit uint64,
) (evidence.EvidenceLifecycleBacklog, error) {
	if ctx == nil || limit == 0 || limit > maxEvidenceLifecycleBatchSize {
		return evidence.EvidenceLifecycleBacklog{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.EvidenceLifecycleBacklog{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	probeLimit := int64(limit + 1)
	var expiredCount, orphanCount int64
	err = tx.QueryRow(ctx, `
		with expired_intents as materialized (
			select intent_id
			from public.evidence_capture_intents
			where valid_until <= transaction_timestamp()
			order by valid_until, intent_id
			limit $1
		), eligible_orphans as materialized (
			select payload.payload_digest
			from public.evidence_payloads as payload
			where payload.created_at <= transaction_timestamp() - ($2 * interval '1 microsecond')
			  and not exists (
				select 1 from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			  )
			order by payload.created_at, payload.payload_digest
			limit $1
		)
		select (select count(*)::bigint from expired_intents),
		       (select count(*)::bigint from eligible_orphans)`,
		probeLimit,
		EvidencePayloadOrphanGracePeriod.Microseconds(),
	).Scan(&expiredCount, &orphanCount)
	if err != nil {
		return evidence.EvidenceLifecycleBacklog{}, fmt.Errorf("read evidence lifecycle backlog: %w", err)
	}
	if expiredCount < 0 || orphanCount < 0 || expiredCount > probeLimit || orphanCount > probeLimit {
		return evidence.EvidenceLifecycleBacklog{}, ErrEvidencePersistenceConflict
	}
	backlog := evidence.EvidenceLifecycleBacklog{
		ExpiredIntentCount:         min(uint64(expiredCount), limit),
		EligibleOrphanPayloadCount: min(uint64(orphanCount), limit),
		MoreExpiredIntents:         uint64(expiredCount) > limit,
		MoreEligibleOrphanPayloads: uint64(orphanCount) > limit,
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.EvidenceLifecycleBacklog{}, fmt.Errorf("commit evidence lifecycle backlog read: %w", err)
	}
	return backlog, nil
}

func evidenceAccountingUint64(values ...int64) ([]uint64, error) {
	converted := make([]uint64, len(values))
	for index, value := range values {
		if value < 0 {
			return nil, ErrEvidencePersistenceConflict
		}
		converted[index] = uint64(value)
	}
	return converted, nil
}

func (repository *PostgresEvidenceRepository) DeleteExpiredCaptureIntents(
	ctx context.Context,
	limit uint64,
) ([]string, error) {
	if ctx == nil || limit == 0 || limit > maxEvidenceLifecycleBatchSize {
		return nil, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		delete from public.evidence_capture_intents
		where intent_id in (
			select intent_id
			from public.evidence_capture_intents
				where valid_until <= transaction_timestamp()
				order by valid_until, intent_id
				limit $1
			)
		returning intent_id`, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("delete expired evidence capture intents: %w", err)
	}
	deleted := make([]string, 0)
	for rows.Next() {
		var intentID string
		if err := rows.Scan(&intentID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan expired evidence capture intent: %w", err)
		}
		if !evidence.ValidCaptureIntentID(intentID) {
			rows.Close()
			return nil, ErrEvidencePersistenceConflict
		}
		deleted = append(deleted, intentID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate expired evidence capture intents: %w", err)
	}
	rows.Close()
	sort.Strings(deleted)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit expired evidence capture intent cleanup: %w", err)
	}
	return deleted, nil
}

func (repository *PostgresEvidenceRepository) CollectUnreferencedPayloads(
	ctx context.Context,
	limit uint64,
) ([]EvidencePayloadGCReceipt, error) {
	if ctx == nil || limit == 0 || limit > maxEvidenceLifecycleBatchSize {
		return nil, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		with candidates as materialized (
			select payload.payload_digest
			from public.evidence_payloads as payload
			where payload.created_at <= transaction_timestamp() - ($2 * interval '1 microsecond')
			  and not exists (
				select 1
				from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			  )
				order by payload.created_at, payload.payload_digest
				limit $1
			), deleted as (
			delete from public.evidence_payloads as payload
			using candidates
			where payload.payload_digest = candidates.payload_digest
			  and not exists (
				select 1
				from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			  )
			returning payload.payload_digest, payload.payload_encoding,
			          payload.canonical_size_bytes, payload.compressed_size_bytes,
			          payload.created_at, transaction_timestamp() as deleted_at
		)
		select payload_digest, payload_encoding, canonical_size_bytes,
		       compressed_size_bytes, created_at, deleted_at
		from deleted
		order by created_at, payload_digest`, int64(limit), EvidencePayloadOrphanGracePeriod.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("delete unreferenced evidence payloads: %w", err)
	}
	type deletedPayload struct {
		digest                        [sha256.Size]byte
		encoding                      string
		canonicalSize, compressedSize uint64
		createdAt, deletedAt          time.Time
	}
	deleted := make([]deletedPayload, 0)
	for rows.Next() {
		var rawDigest []byte
		var canonicalSize, compressedSize int64
		var item deletedPayload
		if err := rows.Scan(&rawDigest, &item.encoding, &canonicalSize, &compressedSize, &item.createdAt, &item.deletedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan unreferenced evidence payload: %w", err)
		}
		if len(rawDigest) != sha256.Size || item.encoding != EvidencePayloadEncodingCanonicalJSONGzipV1 ||
			canonicalSize < 1 || uint64(canonicalSize) > evidence.MaxCanonicalPayloadBytes ||
			compressedSize < 1 || uint64(compressedSize) > maxEvidenceCompressedBytes ||
			item.createdAt.IsZero() || item.deletedAt.IsZero() {
			rows.Close()
			return nil, ErrEvidencePersistenceConflict
		}
		copy(item.digest[:], rawDigest)
		item.canonicalSize = uint64(canonicalSize)
		item.compressedSize = uint64(compressedSize)
		item.createdAt = item.createdAt.UTC()
		item.deletedAt = item.deletedAt.UTC()
		deleted = append(deleted, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate unreferenced evidence payloads: %w", err)
	}
	rows.Close()
	receipts := make([]EvidencePayloadGCReceipt, 0, len(deleted))
	for _, item := range deleted {
		versionDigest := evidencePayloadVersionDigest(item.digest, item.encoding, item.canonicalSize, item.compressedSize, item.createdAt)
		receiptDigest := evidencePayloadGCReceiptDigest(versionDigest, item.deletedAt)
		if _, err := tx.Exec(ctx, `
			insert into public.evidence_payload_gc_receipts (
				payload_version_digest, receipt_digest, deleted_at
			) values ($1, $2, $3)`, versionDigest[:], receiptDigest[:], item.deletedAt); err != nil {
			return nil, fmt.Errorf("insert evidence payload GC receipt: %w", err)
		}
		receipts = append(receipts, EvidencePayloadGCReceipt{
			PayloadVersionDigest: versionDigest,
			ReceiptDigest:        receiptDigest,
			DeletedAt:            item.deletedAt,
			CanonicalSizeBytes:   item.canonicalSize,
			CompressedSizeBytes:  item.compressedSize,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit evidence payload GC: %w", err)
	}
	return receipts, nil
}

func (repository *PostgresEvidenceRepository) startAdmittedTransaction(ctx context.Context) (pgx.Tx, error) {
	if repository == nil || repository.platform == nil {
		return nil, ErrRecordPlatformAdmissionUnavailable
	}
	tx, err := repository.platform.startTransaction(ctx)
	if err != nil {
		return nil, err
	}
	if err := repository.platform.admit(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func deterministicEvidenceGzip(canonical []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create evidence gzip writer: %w", err)
	}
	writer.Header.ModTime = time.Unix(0, 0).UTC()
	writer.Header.OS = 255
	if _, err := writer.Write(canonical); err != nil {
		return nil, fmt.Errorf("compress evidence payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish evidence payload compression: %w", err)
	}
	return compressed.Bytes(), nil
}

func validateEvidencePayloadBinding(canonical []byte, digest [sha256.Size]byte, size uint64) error {
	if len(canonical) == 0 || uint64(len(canonical)) > evidence.MaxCanonicalPayloadBytes ||
		size != uint64(len(canonical)) || digest == [sha256.Size]byte{} || evidence.CanonicalPayloadDigest(canonical) != digest {
		return ErrInvalidEvidencePersistence
	}
	return nil
}

func evidencePostgresTimestampExact(value time.Time) bool {
	return !value.IsZero() && value.Equal(value.Truncate(time.Microsecond))
}

func normalizeEvidenceCaptureIntentTimestamps(
	intent evidence.Intent,
	preview evidence.Preview,
) (evidence.Intent, evidence.Preview) {
	intent.Selection.RequestedWindow = normalizeEvidenceTimeWindow(intent.Selection.RequestedWindow)
	intent.ValidUntil = normalizeEvidenceTime(intent.ValidUntil)
	preview.Selection.RequestedWindow = normalizeEvidenceTimeWindow(preview.Selection.RequestedWindow)
	preview.RequestedWindow = normalizeEvidenceTimeWindow(preview.RequestedWindow)
	preview.ActualWindow = normalizeEvidenceTimeWindow(preview.ActualWindow)
	preview.ObservedAt = normalizeEvidenceTime(preview.ObservedAt)
	preview.PreviewedAt = normalizeEvidenceTime(preview.PreviewedAt)
	preview.ValidUntil = normalizeEvidenceTime(preview.ValidUntil)
	return intent, preview
}

func normalizeEvidenceTimeWindow(window evidence.TimeWindow) evidence.TimeWindow {
	return evidence.TimeWindow{
		Start: normalizeEvidenceTime(window.Start),
		End:   normalizeEvidenceTime(window.End),
	}
}

func normalizeEvidenceTime(value time.Time) time.Time {
	return value.Round(0).UTC()
}

func evidenceCaptureIntentTimestampsExact(intent evidence.Intent, preview evidence.Preview) bool {
	values := []time.Time{
		intent.Selection.RequestedWindow.Start,
		intent.Selection.RequestedWindow.End,
		intent.ValidUntil,
		preview.Selection.RequestedWindow.Start,
		preview.Selection.RequestedWindow.End,
		preview.RequestedWindow.Start,
		preview.RequestedWindow.End,
		preview.ActualWindow.Start,
		preview.ActualWindow.End,
		preview.ObservedAt,
		preview.PreviewedAt,
		preview.ValidUntil,
	}
	for _, value := range values {
		if !evidencePostgresTimestampExact(value) {
			return false
		}
	}
	return true
}

func evidencePayloadVersionDigest(
	payloadDigest [sha256.Size]byte,
	encoding string,
	canonicalSize uint64,
	compressedSize uint64,
	createdAt time.Time,
) [sha256.Size]byte {
	hasher := sha256.New()
	writeEvidenceDigestField(hasher, []byte(evidencePayloadVersionDigestDomainV1))
	writeEvidenceDigestField(hasher, payloadDigest[:])
	writeEvidenceDigestField(hasher, []byte(encoding))
	writeEvidenceDigestUint64(hasher, canonicalSize)
	writeEvidenceDigestUint64(hasher, compressedSize)
	writeEvidenceDigestUint64(hasher, uint64(createdAt.UTC().UnixMicro()))
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func evidencePayloadGCReceiptDigest(versionDigest [sha256.Size]byte, deletedAt time.Time) [sha256.Size]byte {
	hasher := sha256.New()
	writeEvidenceDigestField(hasher, []byte(evidencePayloadGCReceiptDigestDomainV1))
	writeEvidenceDigestField(hasher, versionDigest[:])
	writeEvidenceDigestUint64(hasher, uint64(deletedAt.UTC().UnixMicro()))
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

type evidenceDigestWriter interface {
	Write([]byte) (int, error)
}

func writeEvidenceDigestField(writer evidenceDigestWriter, value []byte) {
	writeEvidenceDigestUint64(writer, uint64(len(value)))
	_, _ = writer.Write(value)
}

func writeEvidenceDigestUint64(writer evidenceDigestWriter, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}

func validEvidenceStoreID(value string, prefix string) bool {
	if len(value) < len(prefix)+1 || len(value) > len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

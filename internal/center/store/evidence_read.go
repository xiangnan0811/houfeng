package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type evidenceCurrentRecordAuthorizationSource interface {
	ResolveCurrentRecordAuthorization(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error)
}

type persistedEvidenceSnapshot struct {
	recordID         string
	snapshotID       string
	envelope         evidence.SnapshotEnvelope
	canonicalPayload []byte
	payloadDigest    [sha256.Size]byte
	referenced       bool
}

type persistedEvidenceSnapshotRow struct {
	recordID, snapshotID, kind, sourceKind, sourceID                string
	schemaVersion                                                   int64
	subjectJSON, sourceJSON, authorizationJSON, authorizationDigest []byte
	requestedStart, requestedEnd, actualStart, actualEnd            time.Time
	observedAt, capturedAt, referencedAt                            time.Time
	sourceRevision, sourceWatermark                                 string
	sourceDigest                                                    []byte
	producerVersion, calculationVersion                             string
	actualPrecisionJSON, bucketWidthJSON, unitsJSON, qualityJSON    []byte
	quotaJSON, retentionJSON                                        []byte
	sensitivity                                                     string
	redactionJSON, canonicalHash                                    []byte
	logicalSize                                                     int64
	payloadDigest                                                   []byte
	referenced                                                      bool
	payloadEncoding                                                 string
	payloadCanonicalSize, payloadCompressedSize                     int64
	compressedPayload                                               []byte
}

var (
	_ evidence.SnapshotReadSource              = (*PostgresEvidenceRepository)(nil)
	_ evidence.AuthorizedSnapshotSource        = (*PostgresEvidenceRepository)(nil)
	_ evidence.ExistingSnapshotReferenceSource = (*PostgresEvidenceRepository)(nil)
)

func NewPostgresEvidenceRepositoryWithReadSources(
	pool *pgxpool.Pool,
	gate AdmissionGate,
	registry evidence.Registry,
	current records.CurrentRecordAuthorizationSource,
	subjects *RecordSubjectReadResolver,
) (*PostgresEvidenceRepository, error) {
	if pool == nil || nilRecordSubjectDependency(gate) || len(registry.Keys()) == 0 ||
		nilRecordSubjectDependency(current) || nilRecordSubjectDependency(subjects) {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	repository := NewPostgresEvidenceRepository(pool, gate)
	repository.registry = registry
	repository.current = current
	repository.subjects = subjects
	repository.loadEvidenceSnapshot = repository.loadPostgresEvidenceSnapshot
	return repository, nil
}

func (repository *PostgresEvidenceRepository) LoadEvidenceSnapshot(
	ctx context.Context,
	actor evidence.ActorScope,
	snapshotID string,
) (evidence.SnapshotReadState, error) {
	persisted, current, source, available, snapshot, err := repository.loadAndAuthorizeEvidenceSnapshot(ctx, actor, snapshotID, true)
	if err != nil {
		return evidence.SnapshotReadState{}, err
	}
	return evidence.SnapshotReadState{
		RecordID: persisted.recordID, SnapshotID: persisted.snapshotID,
		Envelope: snapshot.Envelope(), CanonicalPayload: snapshot.Bytes(),
		RecordScope: current, SourceAuthorization: source, SourceAvailable: available,
	}, nil
}

func (repository *PostgresEvidenceRepository) LoadAuthorizedEvidenceSnapshot(
	ctx context.Context,
	actor evidence.ActorScope,
	snapshotID string,
) (evidence.AuthorizedSnapshot, error) {
	persisted, _, _, _, snapshot, err := repository.loadAndAuthorizeEvidenceSnapshot(ctx, actor, snapshotID, true)
	if err != nil {
		return evidence.AuthorizedSnapshot{}, err
	}
	return evidence.AuthorizedSnapshot{
		RecordID: persisted.recordID, SnapshotID: persisted.snapshotID,
		Key: persisted.envelope.Key, Snapshot: snapshot,
	}, nil
}

func (repository *PostgresEvidenceRepository) ReauthorizeExistingSnapshot(
	ctx context.Context,
	actor evidence.ActorScope,
	recordID string,
	snapshotID string,
) (evidence.ExistingSnapshotReferenceState, error) {
	persisted, _, source, _, _, err := repository.loadAndAuthorizeEvidenceSnapshot(ctx, actor, snapshotID, false)
	if err != nil {
		return evidence.ExistingSnapshotReferenceState{}, err
	}
	if persisted.recordID != recordID {
		return evidence.ExistingSnapshotReferenceState{}, evidence.ErrSnapshotNotFound
	}
	return evidence.ExistingSnapshotReferenceState{
		RecordID: persisted.recordID, SnapshotID: persisted.snapshotID, Key: persisted.envelope.Key,
		SourceType: persisted.envelope.Source.Type, SourceID: persisted.envelope.Source.ID,
		CaptureAuthorizationDigest: persisted.envelope.Authorization.Digest,
		PayloadDigest:              persisted.payloadDigest, Authorization: source,
	}, nil
}

func (repository *PostgresEvidenceRepository) loadAndAuthorizeEvidenceSnapshot(
	ctx context.Context,
	actor recordauth.ActorScope,
	snapshotID string,
	withPayload bool,
) (persistedEvidenceSnapshot, recordauth.ResourceScope, recordauth.SourceAuthorization, bool, evidence.CanonicalSnapshot, error) {
	if ctx == nil || repository == nil || !evidence.ValidSnapshotID(snapshotID) ||
		len(repository.registry.Keys()) == 0 || nilRecordSubjectDependency(repository.current) ||
		nilRecordSubjectDependency(repository.subjects) || repository.loadEvidenceSnapshot == nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrEvidenceServiceUnavailable
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil || !reflect.DeepEqual(normalizedActor, actor) {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrSnapshotNotFound
	}
	// Resolve only bounded metadata first. Payload bytes are deliberately loaded
	// after both record and current-source authorization so corrupt compressed
	// content cannot become a permission oracle.
	persisted, err := repository.loadEvidenceSnapshot(ctx, snapshotID, false)
	if err != nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, err
	}
	if persisted.snapshotID != snapshotID || !validEvidenceStoreID(persisted.recordID, "rec_") || !persisted.referenced ||
		persisted.payloadDigest == [sha256.Size]byte{} || persisted.envelope.CanonicalHash != persisted.payloadDigest {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrSnapshotNotFound
	}
	recordScope, source, available, err := repository.authorizePersistedEvidenceSnapshot(ctx, normalizedActor, persisted)
	if err != nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, err
	}
	kind, err := repository.registry.LookupKey(persisted.envelope.Key)
	if err != nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, err
	}
	normalizedEnvelope, err := evidence.RestoreSnapshotEnvelopeMetadata(kind.Descriptor(), persisted.envelope)
	if err != nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrEvidenceServiceUnavailable
	}
	persisted.envelope = normalizedEnvelope
	if !withPayload {
		return persisted, recordScope, source, available, evidence.CanonicalSnapshot{}, nil
	}
	withPayloadPersisted, err := repository.loadEvidenceSnapshot(ctx, snapshotID, true)
	if err != nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, err
	}
	withPayloadEnvelope, err := evidence.RestoreSnapshotEnvelopeMetadata(kind.Descriptor(), withPayloadPersisted.envelope)
	if err != nil {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrEvidenceServiceUnavailable
	}
	withPayloadPersisted.envelope = withPayloadEnvelope
	if !samePersistedEvidenceSnapshotMetadata(persisted, withPayloadPersisted) {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrEvidenceServiceUnavailable
	}
	snapshot, err := evidence.RestoreCanonicalSnapshot(kind.Descriptor(), persisted.envelope, withPayloadPersisted.canonicalPayload)
	if err != nil || snapshot.Hash() != persisted.payloadDigest {
		return persistedEvidenceSnapshot{}, recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false,
			evidence.CanonicalSnapshot{}, evidence.ErrEvidenceServiceUnavailable
	}
	return persisted, recordScope, source, available, snapshot, nil
}

func samePersistedEvidenceSnapshotMetadata(left, right persistedEvidenceSnapshot) bool {
	left.canonicalPayload = nil
	right.canonicalPayload = nil
	return reflect.DeepEqual(left, right)
}

func (repository *PostgresEvidenceRepository) authorizePersistedEvidenceSnapshot(
	ctx context.Context,
	actor recordauth.ActorScope,
	persisted persistedEvidenceSnapshot,
) (recordauth.ResourceScope, recordauth.SourceAuthorization, bool, error) {
	current, err := repository.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), persisted.recordID)
	if err != nil {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrSnapshotNotFound
	}
	if current.RecordID != persisted.recordID || current.Evidence.ProjectID != actor.ProjectID ||
		records.ValidateLifecycle(current.Lifecycle) != nil ||
		records.AuthorizeRecordResource(actor, recordauth.CapabilityEvidenceRead, current.Evidence) != nil {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrSnapshotNotFound
	}
	subjectKind, ok := evidenceReadSubjectKind(persisted.envelope.Source.Type)
	if !ok || persisted.envelope.Source.ID != persisted.envelope.Authorization.SourceID ||
		persisted.envelope.Source.Type != string(persisted.envelope.Authorization.Kind) {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrEvidenceServiceUnavailable
	}
	identity, err := records.NewSubjectIdentitySnapshot(subjectKind, persisted.envelope.Source.Fields)
	if err != nil {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrEvidenceServiceUnavailable
	}
	resolved, err := repository.subjects.Resolve(ctx, actor.Clone(), RecordSubjectReadInput{
		Reference: records.SubjectReference{
			RegistryVersion: records.SubjectRegistryVersionV1, Kind: subjectKind,
			Role: records.RelationRoleEvidenceSource, SourceID: persisted.envelope.Source.ID,
		},
		IdentitySnapshot: identity, CaptureAuthorization: persisted.envelope.Authorization,
	})
	if err != nil {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrSnapshotNotFound
	}
	source, err := recordauth.NormalizeSourceAuthorization(resolved.CaptureAuthorization)
	if err != nil || !reflect.DeepEqual(source, resolved.CaptureAuthorization) ||
		resolved.ProjectID != actor.ProjectID || resolved.StableID != persisted.envelope.Source.ID ||
		resolved.IdentitySnapshot.Kind() != subjectKind || !reflect.DeepEqual(resolved.IdentitySnapshot.Fields(), identity.Fields()) ||
		source.Kind != persisted.envelope.Authorization.Kind || source.SourceID != persisted.envelope.Authorization.SourceID ||
		source.CaptureScope.CanonicalHash != persisted.envelope.Authorization.CaptureScope.CanonicalHash {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrEvidenceServiceUnavailable
	}
	recordScope := recordauth.ResourceScope{
		Version: recordauth.ResourceScopeVersionV1, ProjectID: current.Evidence.ProjectID,
		Visibility: current.Evidence.Visibility,
		Sources:    append([]recordauth.SourceAuthorization(nil), current.Evidence.Sources...),
	}
	intersection := recordScope
	intersection.Sources = append(append([]recordauth.SourceAuthorization(nil), recordScope.Sources...), source)
	if err := recordauth.Authorize(actor, recordauth.CapabilityEvidenceRead, intersection); err != nil {
		return recordauth.ResourceScope{}, recordauth.SourceAuthorization{}, false, evidence.ErrSnapshotNotFound
	}
	return recordScope, source, source.State == recordauth.SourceStateLive, nil
}

func evidenceReadSubjectKind(sourceType string) (records.SubjectKind, bool) {
	switch recordauth.SourceKind(sourceType) {
	case recordauth.SourceKindVPS:
		return records.SubjectKindVPS, true
	case recordauth.SourceKindMonitoringInstance:
		return records.SubjectKindMonitoringInstance, true
	case recordauth.SourceKindTarget:
		return records.SubjectKindTarget, true
	default:
		return "", false
	}
}

func (repository *PostgresEvidenceRepository) loadPostgresEvidenceSnapshot(
	ctx context.Context,
	snapshotID string,
	withPayload bool,
) (persistedEvidenceSnapshot, error) {
	if ctx == nil || !evidence.ValidSnapshotID(snapshotID) {
		return persistedEvidenceSnapshot{}, evidence.ErrSnapshotNotFound
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return persistedEvidenceSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var row persistedEvidenceSnapshotRow
	if withPayload {
		err = scanPersistedEvidenceSnapshotRow(tx.QueryRow(ctx, fullEvidenceSnapshotReadSQL, snapshotID), &row, true)
	} else {
		err = scanPersistedEvidenceSnapshotRow(tx.QueryRow(ctx, metadataEvidenceSnapshotReadSQL, snapshotID), &row, false)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedEvidenceSnapshot{}, evidence.ErrSnapshotNotFound
	}
	if err != nil {
		return persistedEvidenceSnapshot{}, fmt.Errorf("load evidence snapshot: %w", err)
	}
	persisted, err := decodePersistedEvidenceSnapshot(row, withPayload)
	if err != nil {
		return persistedEvidenceSnapshot{}, evidence.ErrEvidenceServiceUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return persistedEvidenceSnapshot{}, fmt.Errorf("commit evidence snapshot read: %w", err)
	}
	return persisted, nil
}

const evidenceSnapshotReadColumns = `
	snapshot.record_id, snapshot.snapshot_id, snapshot.kind, snapshot.schema_version,
	snapshot.source_kind, snapshot.source_id,
	snapshot.subject_identity_snapshot, snapshot.source_identity_snapshot,
	snapshot.capture_authorization, snapshot.capture_authorization_digest,
	snapshot.requested_started_at, snapshot.requested_ended_at,
	snapshot.actual_started_at, snapshot.actual_ended_at,
	snapshot.observed_at, snapshot.captured_at, snapshot.referenced_at,
	snapshot.source_revision, snapshot.source_watermark, snapshot.source_digest,
	snapshot.producer_version, snapshot.calculation_version,
	snapshot.actual_precision, snapshot.bucket_width, snapshot.unit_semantics, snapshot.quality,
	snapshot.quota_outcome, snapshot.retention, snapshot.sensitivity_level, snapshot.redaction,
	snapshot.canonical_hash, snapshot.logical_size_bytes, snapshot.payload_digest,
	exists (
		select 1 from public.record_revision_evidence as reference
		where reference.record_id = snapshot.record_id and reference.snapshot_id = snapshot.snapshot_id
	)`

const metadataEvidenceSnapshotReadSQL = `select ` + evidenceSnapshotReadColumns + `,
	payload.payload_encoding, payload.canonical_size_bytes, payload.compressed_size_bytes
	from public.evidence_snapshots as snapshot
	join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
	where snapshot.snapshot_id = $1`

const fullEvidenceSnapshotReadSQL = `select ` + evidenceSnapshotReadColumns + `,
	payload.payload_encoding, payload.canonical_size_bytes, payload.compressed_size_bytes, payload.compressed_payload
	from public.evidence_snapshots as snapshot
	join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
	where snapshot.snapshot_id = $1`

func scanPersistedEvidenceSnapshotRow(row pgx.Row, value *persistedEvidenceSnapshotRow, withPayload bool) error {
	destinations := []any{
		&value.recordID, &value.snapshotID, &value.kind, &value.schemaVersion,
		&value.sourceKind, &value.sourceID, &value.subjectJSON, &value.sourceJSON,
		&value.authorizationJSON, &value.authorizationDigest,
		&value.requestedStart, &value.requestedEnd, &value.actualStart, &value.actualEnd,
		&value.observedAt, &value.capturedAt, &value.referencedAt,
		&value.sourceRevision, &value.sourceWatermark, &value.sourceDigest,
		&value.producerVersion, &value.calculationVersion,
		&value.actualPrecisionJSON, &value.bucketWidthJSON, &value.unitsJSON, &value.qualityJSON,
		&value.quotaJSON, &value.retentionJSON, &value.sensitivity, &value.redactionJSON,
		&value.canonicalHash, &value.logicalSize, &value.payloadDigest, &value.referenced,
	}
	destinations = append(destinations, &value.payloadEncoding, &value.payloadCanonicalSize,
		&value.payloadCompressedSize)
	if withPayload {
		destinations = append(destinations, &value.compressedPayload)
	}
	return row.Scan(destinations...)
}

func decodePersistedEvidenceSnapshot(row persistedEvidenceSnapshotRow, withPayload bool) (persistedEvidenceSnapshot, error) {
	if !validEvidenceStoreID(row.recordID, "rec_") || !evidence.ValidSnapshotID(row.snapshotID) ||
		row.schemaVersion < 1 || row.schemaVersion > int64(^uint16(0)) ||
		row.sourceKind == "" || row.sourceID == "" || len(row.authorizationDigest) != sha256.Size ||
		len(row.sourceDigest) != sha256.Size || len(row.canonicalHash) != sha256.Size || len(row.payloadDigest) != sha256.Size ||
		row.logicalSize < 1 || uint64(row.logicalSize) > evidence.MaxCanonicalPayloadBytes ||
		!evidencePostgresTimestampExact(row.requestedStart) || !evidencePostgresTimestampExact(row.requestedEnd) ||
		!evidencePostgresTimestampExact(row.actualStart) || !evidencePostgresTimestampExact(row.actualEnd) ||
		!evidencePostgresTimestampExact(row.observedAt) || !evidencePostgresTimestampExact(row.capturedAt) ||
		!evidencePostgresTimestampExact(row.referencedAt) {
		return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
	}
	envelope := evidence.SnapshotEnvelope{
		Key:             evidence.KindKey{Kind: evidence.KindName(row.kind), SchemaVersion: evidence.SchemaVersion(row.schemaVersion)},
		RequestedWindow: evidence.TimeWindow{Start: row.requestedStart.UTC(), End: row.requestedEnd.UTC()},
		ActualWindow:    evidence.TimeWindow{Start: row.actualStart.UTC(), End: row.actualEnd.UTC()},
		ObservedAt:      row.observedAt.UTC(), CapturedAt: row.capturedAt.UTC(), ReferencedAt: row.referencedAt.UTC(),
		SourceRevision: row.sourceRevision, SourceWatermark: row.sourceWatermark,
		ProducerVersion: row.producerVersion, CalculationVersion: row.calculationVersion,
		Sensitivity: evidence.Sensitivity(row.sensitivity), CanonicalSize: uint64(row.logicalSize),
	}
	jsonValues := []struct {
		raw  []byte
		dest any
	}{
		{row.subjectJSON, &envelope.Subject}, {row.sourceJSON, &envelope.Source},
		{row.authorizationJSON, &envelope.Authorization}, {row.actualPrecisionJSON, &envelope.ActualPrecision},
		{row.bucketWidthJSON, &envelope.BucketWidth}, {row.unitsJSON, &envelope.Units},
		{row.qualityJSON, &envelope.Quality}, {row.quotaJSON, &envelope.QuotaOutcome},
		{row.retentionJSON, &envelope.Retention}, {row.redactionJSON, &envelope.Redaction},
	}
	for _, value := range jsonValues {
		if len(value.raw) == 0 || uint64(len(value.raw)) > evidence.MaxCanonicalPayloadBytes ||
			json.Unmarshal(value.raw, value.dest) != nil || !equalEvidenceJSON(value.raw, reflect.ValueOf(value.dest).Elem().Interface()) {
			return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
		}
	}
	var authorizationDigest [sha256.Size]byte
	copy(authorizationDigest[:], row.authorizationDigest)
	copy(envelope.SourceDigest[:], row.sourceDigest)
	copy(envelope.CanonicalHash[:], row.canonicalHash)
	var payloadDigest [sha256.Size]byte
	copy(payloadDigest[:], row.payloadDigest)
	if envelope.Authorization.Kind != recordauth.SourceKind(row.sourceKind) || envelope.Authorization.SourceID != row.sourceID ||
		envelope.Authorization.Digest == [sha256.Size]byte{} || envelope.Authorization.Digest != authorizationDigest ||
		envelope.Source.Type != row.sourceKind ||
		envelope.Source.ID != row.sourceID || envelope.CanonicalHash != payloadDigest {
		return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
	}
	persisted := persistedEvidenceSnapshot{
		recordID: row.recordID, snapshotID: row.snapshotID, envelope: envelope,
		payloadDigest: payloadDigest, referenced: row.referenced,
	}
	if row.payloadEncoding != EvidencePayloadEncodingCanonicalJSONGzipV1 ||
		row.payloadCanonicalSize != row.logicalSize || row.payloadCompressedSize < 1 ||
		uint64(row.payloadCompressedSize) > maxEvidenceCompressedBytes {
		return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
	}
	if !withPayload {
		return persisted, nil
	}
	if int64(len(row.compressedPayload)) != row.payloadCompressedSize {
		return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
	}
	reader, err := gzip.NewReader(bytes.NewReader(row.compressedPayload))
	if err != nil {
		return persistedEvidenceSnapshot{}, err
	}
	canonical, readErr := io.ReadAll(io.LimitReader(reader, int64(evidence.MaxCanonicalPayloadBytes)+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(canonical)) != row.payloadCanonicalSize ||
		uint64(len(canonical)) > evidence.MaxCanonicalPayloadBytes || evidence.CanonicalPayloadDigest(canonical) != payloadDigest {
		return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
	}
	deterministic, err := deterministicEvidenceGzip(canonical)
	if err != nil || !bytes.Equal(deterministic, row.compressedPayload) {
		return persistedEvidenceSnapshot{}, ErrEvidencePersistenceConflict
	}
	persisted.canonicalPayload = canonical
	return persisted, nil
}

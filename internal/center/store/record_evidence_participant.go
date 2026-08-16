package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

const evidenceProjectCapacityLockDomain = "houfeng.evidence.capacity.project.v1:"

type recordEvidenceRevisionParticipant struct {
	capacityPolicy evidence.CapacityPolicy
}

func NewRecordEvidenceRevisionParticipant() records.RevisionParticipant {
	return recordEvidenceRevisionParticipant{capacityPolicy: evidence.DefaultCapacityPolicy()}
}

func NewRecordEvidenceRevisionParticipantWithCapacityPolicy(
	policy evidence.CapacityPolicy,
) (records.RevisionParticipant, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return recordEvidenceRevisionParticipant{capacityPolicy: policy}, nil
}

func (recordEvidenceRevisionParticipant) Name() string { return "evidence" }

func (participant recordEvidenceRevisionParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	preparation := committed.EvidencePreparation
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) ||
		!validEvidenceStoreID(committed.Result.RecordID, "rec_") ||
		!validEvidenceStoreID(committed.Result.RevisionID, "rrv_") ||
		preparation.ValidateForRecord(committed.Result.RecordID) != nil ||
		!slices.Equal(preparation.SnapshotIDs(), committed.Input.EvidenceSnapshotIDs()) {
		return fmt.Errorf("%w: evidence preparation", records.ErrInvalidRevisionCommand)
	}

	captures := preparation.Captures()
	for _, capture := range captures {
		if capture.Validate() != nil || !evidenceParticipantCaptureTimestampsExact(capture) {
			return fmt.Errorf("%w: prepared capture", records.ErrInvalidRevisionCommand)
		}
	}
	if len(captures) > 0 {
		if err := participant.enforceProjectCapacity(ctx, tx, committed.Result.RecordID, captures); err != nil {
			return err
		}
	}
	for _, capture := range captures {
		if err := consumeEvidenceCaptureIntent(ctx, tx, capture); err != nil {
			return err
		}
		if err := insertEvidenceSnapshot(ctx, tx, capture); err != nil {
			return err
		}
	}
	for _, reference := range preparation.References() {
		if err := validatePersistedEvidenceReference(ctx, tx, reference); err != nil {
			return err
		}
	}
	for ordinal, snapshotID := range preparation.SnapshotIDs() {
		inserted, err := tx.Exec(ctx, `
			insert into public.record_revision_evidence (
				record_id, revision_id, ordinal, snapshot_id
			) values ($1, $2, $3, $4)`,
			committed.Result.RecordID,
			committed.Result.RevisionID,
			int64(ordinal),
			snapshotID,
		)
		if err != nil {
			return fmt.Errorf("insert record revision evidence reference: %w", err)
		}
		if inserted.RowsAffected() != 1 {
			return ErrEvidencePersistenceConflict
		}
	}
	return nil
}

func (participant recordEvidenceRevisionParticipant) enforceProjectCapacity(
	ctx context.Context,
	tx pgx.Tx,
	recordID string,
	captures []evidence.PreparedCapture,
) error {
	if participant.capacityPolicy.Validate() != nil {
		return evidence.ErrCapacityUnavailable
	}
	var projectID string
	if err := tx.QueryRow(ctx, `
		select project_id
		from public.records
		where record_id = $1`, recordID).Scan(&projectID); err != nil {
		return fmt.Errorf("%w: resolve evidence capacity project: %w", evidence.ErrCapacityUnavailable, err)
	}
	if recordauth.ValidateProjectID(recordauth.ProjectID(projectID)) != nil {
		return evidence.ErrCapacityUnavailable
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, evidenceProjectCapacityLockDomain+projectID); err != nil {
		return fmt.Errorf("%w: lock evidence project capacity: %w", evidence.ErrCapacityUnavailable, err)
	}
	var usedLogicalBytes int64
	if err := tx.QueryRow(ctx, `
		select coalesce(sum(snapshot.logical_size_bytes), 0)::bigint
		from public.evidence_snapshots as snapshot
		join public.records as record on record.record_id = snapshot.record_id
		where record.project_id = $1`, projectID).Scan(&usedLogicalBytes); err != nil {
		return fmt.Errorf("%w: read evidence project capacity: %w", evidence.ErrCapacityUnavailable, err)
	}
	if usedLogicalBytes < 0 {
		return ErrEvidencePersistenceConflict
	}
	used := uint64(usedLogicalBytes)
	for _, capture := range captures {
		additional := capture.Snapshot().Size()
		if additional == 0 || additional > math.MaxInt64 {
			return ErrEvidencePersistenceConflict
		}
		evaluation, err := participant.capacityPolicy.Evaluate(used, additional)
		if err != nil {
			return fmt.Errorf("%w: evaluate final evidence capacity: %w", ErrEvidencePersistenceConflict, err)
		}
		if evaluation.Outcome != capture.Preview().QuotaOutcome ||
			evaluation.Outcome.Status == evidence.QuotaExceeded ||
			evaluation.Outcome.Status == evidence.QuotaUnavailable {
			return fmt.Errorf("%w: %w", ErrEvidencePersistenceConflict, evidence.ErrPreviewStale)
		}
		used = evaluation.ProjectedBytes
	}
	return nil
}

type persistedEvidenceCaptureIntent struct {
	recordID      string
	kind          string
	schemaVersion int64
	previewDigest []byte
	sourceDigest  []byte
	selectionJSON []byte
	previewJSON   []byte
	snapshotID    string
	estimatedSize int64
	createdAt     time.Time
	validUntil    time.Time
}

func consumeEvidenceCaptureIntent(ctx context.Context, tx pgx.Tx, capture evidence.PreparedCapture) error {
	if err := capture.Validate(); err != nil || !evidenceParticipantCaptureTimestampsExact(capture) {
		return fmt.Errorf("%w: prepared capture", records.ErrInvalidRevisionCommand)
	}
	intent := capture.Intent()
	var persisted persistedEvidenceCaptureIntent
	err := tx.QueryRow(ctx, `
		delete from public.evidence_capture_intents
		where intent_id = $1
		  and valid_until > transaction_timestamp()
		returning record_id, kind, schema_version, preview_digest,
		          source_digest, selection, preview,
		          coalesce(snapshot_id, ''), estimated_size_bytes,
		          created_at, valid_until`,
		intent.ID,
	).Scan(
		&persisted.recordID,
		&persisted.kind,
		&persisted.schemaVersion,
		&persisted.previewDigest,
		&persisted.sourceDigest,
		&persisted.selectionJSON,
		&persisted.previewJSON,
		&persisted.snapshotID,
		&persisted.estimatedSize,
		&persisted.createdAt,
		&persisted.validUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEvidencePersistenceConflict
	}
	if err != nil {
		return fmt.Errorf("consume evidence capture intent: %w", err)
	}
	if !persistedEvidenceIntentMatchesCapture(persisted, capture) {
		return ErrEvidencePersistenceConflict
	}
	return nil
}

func evidenceParticipantCaptureTimestampsExact(capture evidence.PreparedCapture) bool {
	intent := capture.Intent()
	preview := capture.Preview()
	envelope := capture.Snapshot().Envelope()
	values := []time.Time{
		intent.ValidUntil,
		preview.RequestedWindow.Start,
		preview.RequestedWindow.End,
		preview.ActualWindow.Start,
		preview.ActualWindow.End,
		preview.ObservedAt,
		preview.PreviewedAt,
		preview.ValidUntil,
		envelope.RequestedWindow.Start,
		envelope.RequestedWindow.End,
		envelope.ActualWindow.Start,
		envelope.ActualWindow.End,
		envelope.ObservedAt,
		envelope.CapturedAt,
		envelope.ReferencedAt,
	}
	for _, value := range values {
		if !evidencePostgresTimestampExact(value) {
			return false
		}
	}
	return true
}

func persistedEvidenceIntentMatchesCapture(persisted persistedEvidenceCaptureIntent, capture evidence.PreparedCapture) bool {
	intent := capture.Intent()
	preview := capture.Preview()
	return persisted.recordID == capture.RecordID() &&
		persisted.kind == string(intent.Key.Kind) &&
		persisted.schemaVersion == int64(intent.Key.SchemaVersion) &&
		bytes.Equal(persisted.previewDigest, intent.PreviewDigest[:]) &&
		bytes.Equal(persisted.sourceDigest, preview.SourceDigest[:]) &&
		equalEvidenceJSON(persisted.selectionJSON, intent.Selection) &&
		equalEvidenceJSON(persisted.previewJSON, preview) &&
		persisted.snapshotID == capture.SnapshotID() &&
		persisted.estimatedSize == int64(preview.EstimatedCanonicalBytes) &&
		persisted.createdAt.Equal(preview.PreviewedAt) &&
		persisted.validUntil.Equal(preview.ValidUntil) &&
		persisted.validUntil.Equal(intent.ValidUntil)
}

func insertEvidenceSnapshot(ctx context.Context, tx pgx.Tx, capture evidence.PreparedCapture) error {
	snapshot := capture.Snapshot()
	envelope := snapshot.Envelope()
	subjectJSON, err := marshalEvidenceParticipantJSON(envelope.Subject)
	if err != nil {
		return err
	}
	sourceJSON, err := marshalEvidenceParticipantJSON(envelope.Source)
	if err != nil {
		return err
	}
	authorizationJSON, err := marshalEvidenceParticipantJSON(envelope.Authorization)
	if err != nil {
		return err
	}
	actualPrecisionJSON, err := marshalEvidenceParticipantJSON(envelope.ActualPrecision)
	if err != nil {
		return err
	}
	bucketWidthJSON, err := marshalEvidenceParticipantJSON(envelope.BucketWidth)
	if err != nil {
		return err
	}
	unitsJSON, err := marshalEvidenceParticipantJSON(envelope.Units)
	if err != nil {
		return err
	}
	qualityJSON, err := marshalEvidenceParticipantJSON(envelope.Quality)
	if err != nil {
		return err
	}
	quotaJSON, err := marshalEvidenceParticipantJSON(envelope.QuotaOutcome)
	if err != nil {
		return err
	}
	retentionJSON, err := marshalEvidenceParticipantJSON(envelope.Retention)
	if err != nil {
		return err
	}
	redactionJSON, err := marshalEvidenceParticipantJSON(envelope.Redaction)
	if err != nil {
		return err
	}
	digest := snapshot.Hash()
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_snapshots (
			snapshot_id, record_id, kind, schema_version, source_kind, source_id,
			subject_identity_snapshot, source_identity_snapshot,
			capture_authorization, capture_authorization_digest,
			requested_started_at, requested_ended_at,
			actual_started_at, actual_ended_at,
			observed_at, captured_at, referenced_at,
			source_revision, source_watermark, source_digest,
			producer_version, calculation_version,
			actual_precision, bucket_width, unit_semantics, quality,
			quota_outcome, retention, sensitivity_level, redaction,
			canonical_hash, logical_size_bytes, payload_digest
		) values (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22,
			$23, $24, $25, $26, $27, $28, $29, $30,
			$31, $32, $33
		)`,
		capture.SnapshotID(),
		capture.RecordID(),
		string(envelope.Key.Kind),
		int64(envelope.Key.SchemaVersion),
		envelope.Source.Type,
		envelope.Source.ID,
		subjectJSON,
		sourceJSON,
		authorizationJSON,
		envelope.Authorization.Digest[:],
		envelope.RequestedWindow.Start,
		envelope.RequestedWindow.End,
		envelope.ActualWindow.Start,
		envelope.ActualWindow.End,
		envelope.ObservedAt,
		envelope.CapturedAt,
		envelope.ReferencedAt,
		envelope.SourceRevision,
		envelope.SourceWatermark,
		envelope.SourceDigest[:],
		envelope.ProducerVersion,
		envelope.CalculationVersion,
		actualPrecisionJSON,
		bucketWidthJSON,
		unitsJSON,
		qualityJSON,
		quotaJSON,
		retentionJSON,
		string(envelope.Sensitivity),
		redactionJSON,
		digest[:],
		int64(snapshot.Size()),
		digest[:],
	)
	if err != nil {
		return fmt.Errorf("insert evidence snapshot: %w", err)
	}
	if inserted.RowsAffected() != 1 {
		return ErrEvidencePersistenceConflict
	}
	return nil
}

func validatePersistedEvidenceReference(ctx context.Context, tx pgx.Tx, reference evidence.PreparedReference) error {
	if err := reference.Validate(); err != nil {
		return fmt.Errorf("%w: prepared reference", records.ErrInvalidRevisionCommand)
	}
	var (
		recordID, snapshotID, kind, sourceType, sourceID string
		schemaVersion                                    int64
		authorizationDigest, payloadDigest               []byte
	)
	err := tx.QueryRow(ctx, `
		select record_id, snapshot_id, kind, schema_version,
		       source_kind, source_id, capture_authorization_digest,
		       payload_digest
		from public.evidence_snapshots
		where record_id = $1 and snapshot_id = $2`,
		reference.RecordID(),
		reference.SnapshotID(),
	).Scan(
		&recordID,
		&snapshotID,
		&kind,
		&schemaVersion,
		&sourceType,
		&sourceID,
		&authorizationDigest,
		&payloadDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEvidencePersistenceConflict
	}
	if err != nil {
		return fmt.Errorf("validate existing evidence snapshot reference: %w", err)
	}
	expectedAuthorizationDigest := reference.CaptureAuthorizationDigest()
	expectedPayloadDigest := reference.PayloadDigest()
	if recordID != reference.RecordID() || snapshotID != reference.SnapshotID() ||
		kind != string(reference.Key().Kind) || schemaVersion != int64(reference.Key().SchemaVersion) ||
		sourceType != reference.SourceType() || sourceID != reference.SourceID() ||
		!bytes.Equal(authorizationDigest, expectedAuthorizationDigest[:]) ||
		!bytes.Equal(payloadDigest, expectedPayloadDigest[:]) {
		return ErrEvidencePersistenceConflict
	}
	return nil
}

func marshalEvidenceParticipantJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence snapshot metadata: %w", err)
	}
	if len(encoded) == 0 || uint64(len(encoded)) > evidence.MaxCanonicalPayloadBytes {
		return nil, ErrInvalidEvidencePersistence
	}
	return encoded, nil
}

func equalEvidenceJSON(persisted []byte, expected any) bool {
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return false
	}
	decode := func(encoded []byte) (any, bool) {
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, false
		}
		return value, true
	}
	persistedValue, ok := decode(persisted)
	if !ok {
		return false
	}
	expectedValue, ok := decode(expectedJSON)
	return ok && reflect.DeepEqual(persistedValue, expectedValue)
}

func nilRecordEvidenceParticipantTx(tx pgx.Tx) bool {
	if tx == nil {
		return true
	}
	value := reflect.ValueOf(tx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

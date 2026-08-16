package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationEvidenceIntentPayloadAndOrphanLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-lifecycle", 2)
	repository := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)

	var databaseNow time.Time
	if err := fixture.db.QueryRow(ctx, `select transaction_timestamp()`).Scan(&databaseNow); err != nil {
		t.Fatalf("read database time: %v", err)
	}
	intent, preview := storeEvidenceIntentFixture()
	preview.PreviewedAt = databaseNow.UTC()
	preview.ValidUntil = preview.PreviewedAt.Add(evidence.CaptureIntentTTL)
	intent.ValidUntil = preview.ValidUntil
	if err := repository.PersistCaptureIntent(ctx, "rec_evidencelifecycle", "evs_evidencelifecycle", intent, preview); err != nil {
		t.Fatalf("PersistCaptureIntent() error = %v", err)
	}
	var persistedRecordID, persistedSnapshotID string
	var persistedCreatedAt, persistedValidUntil time.Time
	if err := fixture.db.QueryRow(ctx, `
		select record_id, snapshot_id, created_at, valid_until
		from public.evidence_capture_intents
		where intent_id = $1`, intent.ID,
	).Scan(&persistedRecordID, &persistedSnapshotID, &persistedCreatedAt, &persistedValidUntil); err != nil {
		t.Fatalf("read persisted evidence intent: %v", err)
	}
	if persistedRecordID != "rec_evidencelifecycle" || persistedSnapshotID != "evs_evidencelifecycle" ||
		!persistedCreatedAt.Equal(preview.PreviewedAt) || !persistedValidUntil.Equal(preview.ValidUntil) {
		t.Fatalf("persisted intent = %q %s..%s", persistedRecordID, persistedCreatedAt, persistedValidUntil)
	}
	loadedBinding, err := repository.LoadCaptureIntentBinding(ctx, "rec_evidencelifecycle", intent.ID)
	if err != nil {
		t.Fatalf("LoadCaptureIntentBinding(live) error = %v", err)
	}
	wantBinding := evidence.CaptureIntentBinding{
		RecordID: "rec_evidencelifecycle", SnapshotID: "evs_evidencelifecycle",
		Intent: intent, Preview: preview,
	}
	if !reflect.DeepEqual(loadedBinding, wantBinding) {
		t.Fatalf("LoadCaptureIntentBinding(live) = %#v, want %#v", loadedBinding, wantBinding)
	}
	if deleted, err := repository.DeleteExpiredCaptureIntents(ctx, 10); err != nil || len(deleted) != 0 {
		t.Fatalf("DeleteExpiredCaptureIntents(live) = (%#v, %v), want empty", deleted, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.evidence_capture_intents
		set created_at = transaction_timestamp() - interval '16 minutes',
		    valid_until = transaction_timestamp() - interval '1 minute'
		where intent_id = $1`, intent.ID); err != nil {
		t.Fatalf("expire evidence intent: %v", err)
	}
	if _, err := repository.LoadCaptureIntentBinding(ctx, "rec_evidencelifecycle", intent.ID); !errors.Is(err, evidence.ErrCaptureIntentUnavailable) {
		t.Fatalf("LoadCaptureIntentBinding(expired) error = %v, want ErrCaptureIntentUnavailable", err)
	}
	if deleted, err := repository.DeleteExpiredCaptureIntents(ctx, 10); err != nil || !reflect.DeepEqual(deleted, []string{intent.ID}) {
		t.Fatalf("DeleteExpiredCaptureIntents(expired) = (%#v, %v), want intent", deleted, err)
	}

	storedSnapshot := storeEvidenceSnapshotFixture(t, "persisted payload")
	first, err := repository.PersistPayload(ctx, storedSnapshot)
	if err != nil {
		t.Fatalf("PersistPayload(first) error = %v", err)
	}
	second, err := repository.PersistPayload(ctx, storedSnapshot)
	if err != nil || second != first {
		t.Fatalf("PersistPayload(replay) = (%#v, %v), want %#v", second, err, first)
	}
	var storedEncoding string
	var storedCompressed []byte
	if err := fixture.db.QueryRow(ctx, `
		select payload_encoding, compressed_payload
		from public.evidence_payloads
		where payload_digest = $1`, first.Digest[:],
	).Scan(&storedEncoding, &storedCompressed); err != nil {
		t.Fatalf("read persisted evidence payload: %v", err)
	}
	if storedEncoding != EvidencePayloadEncodingCanonicalJSONGzipV1 {
		t.Fatalf("persisted payload encoding = %q", storedEncoding)
	}
	reader, err := gzip.NewReader(bytes.NewReader(storedCompressed))
	if err != nil {
		t.Fatalf("gzip.NewReader(persisted payload) error = %v", err)
	}
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read persisted payload: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close persisted payload reader: %v", err)
	}
	if !bytes.Equal(decompressed, storedSnapshot.Bytes()) {
		t.Fatalf("persisted payload bytes = %q, want %q", decompressed, storedSnapshot.Bytes())
	}
	conflictingSnapshot := storeEvidenceSnapshotFixture(t, "expected replay payload")
	conflictingBytes := storeEvidenceSnapshotFixture(t, "different replay value")
	conflictingCompressed, err := deterministicEvidenceGzip(conflictingBytes.Bytes())
	if err != nil {
		t.Fatalf("compress conflicting evidence payload: %v", err)
	}
	conflictingDigest := conflictingSnapshot.Hash()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_payloads (
			payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload
		) values ($1, $2, $3, $4)`,
		conflictingDigest[:], int64(conflictingSnapshot.Size()), int64(len(conflictingCompressed)), conflictingCompressed,
	); err != nil {
		t.Fatalf("seed conflicting evidence payload: %v", err)
	}
	if _, err := repository.PersistPayload(ctx, conflictingSnapshot); !errors.Is(err, ErrEvidencePersistenceConflict) {
		t.Fatalf("PersistPayload(conflicting replay) error = %v, want ErrEvidencePersistenceConflict", err)
	}

	young := storeEvidenceSnapshotFixture(t, "young orphan")
	old := storeEvidenceSnapshotFixture(t, "old orphan")
	referenced := storeEvidenceSnapshotFixture(t, "old referenced payload")
	seedEvidencePayloadAt(t, ctx, fixture, young, databaseNow.Add(-EvidencePayloadOrphanGracePeriod+time.Minute))
	seedEvidencePayloadAt(t, ctx, fixture, old, databaseNow.Add(-EvidencePayloadOrphanGracePeriod-time.Minute))
	seedEvidencePayloadAt(t, ctx, fixture, referenced, databaseNow.Add(-EvidencePayloadOrphanGracePeriod-time.Minute))

	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_evidencegc",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Evidence GC reference"),
		"evidence-gc-record",
	))
	if err != nil {
		t.Fatalf("CommitRevision(evidence GC record) error = %v", err)
	}
	seedEvidenceSnapshotReference(t, ctx, fixture, record.RecordID, referenced, databaseNow)

	receipts, err := repository.CollectUnreferencedPayloads(ctx, 10)
	if err != nil {
		t.Fatalf("CollectUnreferencedPayloads() error = %v", err)
	}
	if len(receipts) != 1 || receipts[0].PayloadVersionDigest == [32]byte{} ||
		receipts[0].ReceiptDigest == [32]byte{} || receipts[0].DeletedAt.IsZero() {
		t.Fatalf("CollectUnreferencedPayloads() = %#v, want one content-free receipt", receipts)
	}
	assertEvidencePayloadExists(t, ctx, fixture, young.Hash(), true)
	assertEvidencePayloadExists(t, ctx, fixture, old.Hash(), false)
	assertEvidencePayloadExists(t, ctx, fixture, referenced.Hash(), true)

	var receiptDigest []byte
	var receiptDeletedAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select receipt_digest, deleted_at
		from public.evidence_payload_gc_receipts
		where payload_version_digest = $1`, receipts[0].PayloadVersionDigest[:],
	).Scan(&receiptDigest, &receiptDeletedAt); err != nil {
		t.Fatalf("read evidence payload GC receipt: %v", err)
	}
	if !bytes.Equal(receiptDigest, receipts[0].ReceiptDigest[:]) || !receiptDeletedAt.Equal(receipts[0].DeletedAt) {
		t.Fatalf("persisted GC receipt = %x/%s, want %x/%s", receiptDigest, receiptDeletedAt, receipts[0].ReceiptDigest, receipts[0].DeletedAt)
	}
	rows, err := fixture.db.Query(ctx, `
		select column_name
		from information_schema.columns
		where table_schema = 'public' and table_name = 'evidence_payload_gc_receipts'
		order by ordinal_position`)
	if err != nil {
		t.Fatalf("read evidence payload GC receipt columns: %v", err)
	}
	columns := make([]string, 0, 4)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan evidence payload GC receipt column: %v", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate evidence payload GC receipt columns: %v", err)
	}
	rows.Close()
	wantColumns := []string{"payload_version_digest", "receipt_digest", "deleted_at", "created_at"}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("evidence payload GC receipt columns = %#v, want %#v", columns, wantColumns)
	}

	receiptConflict := storeEvidenceSnapshotFixture(t, "receipt conflict orphan")
	receiptConflictCreatedAt := databaseNow.Add(-EvidencePayloadOrphanGracePeriod - time.Minute)
	seedEvidencePayloadAt(t, ctx, fixture, receiptConflict, receiptConflictCreatedAt)
	receiptConflictCompressed, err := deterministicEvidenceGzip(receiptConflict.Bytes())
	if err != nil {
		t.Fatalf("compress receipt-conflict payload: %v", err)
	}
	receiptConflictVersion := evidencePayloadVersionDigest(
		receiptConflict.Hash(),
		EvidencePayloadEncodingCanonicalJSONGzipV1,
		receiptConflict.Size(),
		uint64(len(receiptConflictCompressed)),
		receiptConflictCreatedAt,
	)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_payload_gc_receipts (
			payload_version_digest, receipt_digest, deleted_at
		) values ($1, decode(repeat('77', 32), 'hex'), $2)`,
		receiptConflictVersion[:], databaseNow,
	); err != nil {
		t.Fatalf("seed conflicting evidence payload GC receipt: %v", err)
	}
	if _, err := repository.CollectUnreferencedPayloads(ctx, 10); err == nil {
		t.Fatal("CollectUnreferencedPayloads(receipt conflict) succeeded, want rollback error")
	}
	assertEvidencePayloadExists(t, ctx, fixture, receiptConflict.Hash(), true)
}

func seedEvidencePayloadAt(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	snapshot evidence.CanonicalSnapshot,
	createdAt time.Time,
) {
	t.Helper()
	compressed, err := deterministicEvidenceGzip(snapshot.Bytes())
	if err != nil {
		t.Fatalf("deterministicEvidenceGzip() error = %v", err)
	}
	digest := snapshot.Hash()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_payloads (
			payload_digest, canonical_size_bytes, compressed_size_bytes,
			compressed_payload, created_at
		) values ($1, $2, $3, $4, $5)`,
		digest[:], int64(snapshot.Size()), int64(len(compressed)), compressed, createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed evidence payload: %v", err)
	}
}

func seedEvidenceSnapshotReference(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	snapshot evidence.CanonicalSnapshot,
	now time.Time,
) {
	t.Helper()
	digest := snapshot.Hash()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_snapshots (
			snapshot_id, record_id, kind, schema_version, source_kind, source_id,
			subject_identity_snapshot, source_identity_snapshot,
			capture_authorization, capture_authorization_digest,
			requested_started_at, requested_ended_at, actual_started_at, actual_ended_at,
			observed_at, captured_at, referenced_at, source_revision, source_digest,
			producer_version, calculation_version, actual_precision, bucket_width,
			unit_semantics, quality, quota_outcome, retention, sensitivity_level,
			redaction, canonical_hash, logical_size_bytes, payload_digest
		) values (
			'evs_evidencegc', $1, 'monitoring.host', 1, 'monitoring_instance',
			'mi_0123456789abcdef', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			decode(repeat('11', 32), 'hex'),
			$2, $3, $2, $3, $3, $4, $5, 'revision-1',
			decode(repeat('22', 32), 'hex'), 'producer-1', 'calculation-1',
			'{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			'{}'::jsonb, 'normal', '[]'::jsonb, $6, $7, $6
		)`,
		recordID,
		now.Add(-time.Hour).UTC(),
		now.UTC(),
		now.Add(time.Minute).UTC(),
		now.Add(2*time.Minute).UTC(),
		digest[:],
		int64(snapshot.Size()),
	); err != nil {
		t.Fatalf("seed referenced evidence snapshot: %v", err)
	}
}

func assertEvidencePayloadExists(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	digest [32]byte,
	want bool,
) {
	t.Helper()
	var exists bool
	if err := fixture.db.QueryRow(ctx, `
		select exists (
			select 1 from public.evidence_payloads where payload_digest = $1
		)`, digest[:],
	).Scan(&exists); err != nil {
		t.Fatalf("check evidence payload existence: %v", err)
	}
	if exists != want {
		t.Fatalf("evidence payload %x exists = %t, want %t", digest, exists, want)
	}
}

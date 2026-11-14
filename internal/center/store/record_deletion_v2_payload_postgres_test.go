package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recorddeletion"
	storemigrate "houfeng/internal/center/store/migrate"
)

// testV2EvidencePayloadFinalMaterialization is registered by the transaction
// locks integration test. It blocks the actual TOAST index, not the evidence
// table or an artificial failure in the payload function.
func testV2EvidencePayloadFinalMaterialization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v2-toast-runtime", 2)
	workers := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-toast-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workers); err != nil {
		t.Fatal(err)
	}
	record := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-toast-record")

	// Use the real canonical snapshot builder and gzip codec. Random bytes in a
	// legal evidence field make the gzip stream large enough to be external.
	random := make([]byte, 8192)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	snapshot := storeEvidenceSnapshotFixture(t, hex.EncodeToString(random))
	compressed, err := deterministicEvidenceGzip(snapshot.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	digest := snapshot.Hash()
	decoded, err := decodeEvidencePayloadBytes(compressed, int64(snapshot.Size()), int64(len(compressed)), digest)
	if err != nil || !bytes.Equal(decoded, snapshot.Bytes()) {
		t.Fatalf("canonical evidence codec round trip: %v", err)
	}
	seedEvidencePayloadAt(t, ctx, fixture, snapshot, time.Now())
	envelope := snapshot.Envelope()
	encoded := make([][]byte, 10)
	for i, value := range []any{envelope.Subject, envelope.Source, envelope.Authorization, envelope.ActualPrecision, envelope.BucketWidth,
		envelope.Units, envelope.Quality, envelope.QuotaOutcome, envelope.Retention, envelope.Redaction} {
		encoded[i], err = marshalEvidenceParticipantJSON(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	const snapshotID = "evs_v2payloadtoast"
	if _, err := fixture.db.Exec(ctx, `insert into public.evidence_snapshots (
		snapshot_id,record_id,kind,schema_version,source_kind,source_id,
		subject_identity_snapshot,source_identity_snapshot,capture_authorization,capture_authorization_digest,
		requested_started_at,requested_ended_at,actual_started_at,actual_ended_at,observed_at,captured_at,referenced_at,
		source_revision,source_watermark,source_digest,producer_version,calculation_version,
		actual_precision,bucket_width,unit_semantics,quality,quota_outcome,retention,sensitivity_level,redaction,
		canonical_hash,logical_size_bytes,payload_digest
	) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,
		$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$31)`,
		snapshotID, record.RecordID, string(envelope.Key.Kind), int64(envelope.Key.SchemaVersion), envelope.Source.Type, envelope.Source.ID,
		encoded[0], encoded[1], encoded[2], envelope.Authorization.Digest[:], envelope.RequestedWindow.Start, envelope.RequestedWindow.End,
		envelope.ActualWindow.Start, envelope.ActualWindow.End, envelope.ObservedAt, envelope.CapturedAt, envelope.ReferencedAt,
		envelope.SourceRevision, envelope.SourceWatermark, envelope.SourceDigest[:], envelope.ProducerVersion, envelope.CalculationVersion,
		encoded[3], encoded[4], encoded[5], encoded[6], encoded[7], encoded[8], string(envelope.Sensitivity), encoded[9], digest[:], int64(snapshot.Size())); err != nil {
		t.Fatalf("seed canonical evidence snapshot: %v", err)
	}

	// This fixture has one evidence payload. Its only TOAST value must consist
	// of several chunks whose total is the stored gzip length, proving that the
	// actual bytes are out of line rather than merely assuming a size threshold.
	var toastOID, indexOID uint32
	var toastName, indexName string
	if err := fixture.db.QueryRow(ctx, `select t.oid,t.relname,i.indexrelid,ic.relname
		from pg_catalog.pg_class p join pg_catalog.pg_class t on t.oid=p.reltoastrelid
		join pg_catalog.pg_index i on i.indrelid=t.oid and i.indisvalid
		join pg_catalog.pg_class ic on ic.oid=i.indexrelid
		where p.oid='public.evidence_payloads'::regclass`).Scan(&toastOID, &toastName, &indexOID, &indexName); err != nil {
		t.Fatalf("locate payload TOAST relation and index: %v", err)
	}
	toastTable := pgx.Identifier{"pg_toast", toastName}.Sanitize()
	toastIndex := pgx.Identifier{"pg_toast", indexName}.Sanitize()
	var values, chunks int64
	var chunkBytes int64
	if err := fixture.db.QueryRow(ctx, `select count(distinct chunk_id),count(*),coalesce(sum(octet_length(chunk_data)),0) from `+toastTable).Scan(&values, &chunks, &chunkBytes); err != nil || values != 1 || chunks < 2 || chunkBytes != int64(len(compressed)) {
		t.Fatalf("evidence payload is not the sole external TOAST value: values=%d chunks=%d bytes=%d want=%d: %v", values, chunks, chunkBytes, len(compressed), err)
	}

	expiry := issueV2WorkerFixture(t, ctx, fixture, runtime, record.RecordID, "toast")
	waitServingTestFenceExpired(t, ctx, fixture, expiry)
	repository := NewPostgresRecordDeletionRepository(workers, allowRecordPlatformAdmissionGate)
	claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_toast", OwnerLeaseDuration: 30 * time.Second})
	if err != nil || claim == nil {
		t.Fatalf("claim: %v, %v", claim, err)
	}
	target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
	normal, err := workers.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = normal.Rollback(context.Background()) }()
	authority, err := deletionAdapterEnvelope(ctx, normal, target, recorddeletion.RecordEvidenceSurfaceDigest())
	if err != nil {
		t.Fatal(err)
	}
	body, err := encodeRecordPurgeCommand(struct {
		Authority  []byte `json:"authority"`
		SnapshotID string `json:"snapshot_id"`
	}{authority, snapshotID})
	if err != nil {
		t.Fatal(err)
	}
	var returned []byte
	if err := normal.QueryRow(ctx, `select public.record_evidence_purge_payload_v2($1::bytea)`, body).Scan(&returned); err != nil || !bytes.Equal(returned, compressed) {
		t.Fatalf("live evidence payload differs from actual gzip: %v", err)
	}
	if err := normal.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// PostgreSQL LOCK TABLE rejects relkind TOAST. Non-concurrent REINDEX of
	// its actual index holds ShareLock on the TOAST relation and
	// AccessExclusiveLock on the index until the independent holder ends.
	// Fetching this external datum must open that index with AccessShareLock.
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `reindex index `+toastIndex); err != nil {
		t.Fatalf("hold actual TOAST index: %v", err)
	}
	var held bool
	if err := holder.QueryRow(ctx, `select exists(select 1 from pg_locks where pid=pg_backend_pid() and relation=$1::oid and mode='ShareLock' and granted)
		and exists(select 1 from pg_locks where pid=pg_backend_pid() and relation=$2::oid and mode='AccessExclusiveLock' and granted)`, toastOID, indexOID).Scan(&held); err != nil || !held {
		t.Fatalf("TOAST relation/index locks not held: %t, %v", held, err)
	}
	worker, err := workers.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var workerPID int
	if err := worker.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		_ = worker.Rollback(context.Background())
		t.Fatal(err)
	}
	type result struct {
		payload []byte
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var payload []byte
		err := worker.QueryRow(ctx, `select public.record_evidence_purge_payload_v2($1::bytea)`, body).Scan(&payload)
		done <- result{payload, err}
	}()
	received := false
	defer func() {
		cancel()
		_ = holder.Rollback(context.Background())
		if !received {
			<-done
		}
		_ = worker.Rollback(context.Background())
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
	var blockedLive bool
	if err := fixture.db.QueryRow(ctx, `select clock_timestamp()<$3::timestamptz and exists
		(select 1 from pg_catalog.pg_locks where pid=$1 and relation=$2::oid and mode='AccessShareLock' and not granted)
		and $4::integer=any(pg_catalog.pg_blocking_pids($1::integer))`, workerPID, indexOID, claim.Owner.ExpiresAt, holderPID).Scan(&blockedLive); err != nil || !blockedLive {
		t.Fatalf("payload did not wait on the actual TOAST index while owner was live: %t, %v", blockedLive, err)
	}
	waitV2DeadlineInTransaction(t, ctx, fixture, holder, claim.Owner.ExpiresAt)
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	received = true
	var pgErr *pgconn.PgError
	if !errors.As(got.err, &pgErr) || pgErr.Code != "42501" || len(got.payload) != 0 {
		t.Fatalf("expired payload read: bytes=%d error=%v, want original owner 42501 and no bytes", len(got.payload), got.err)
	}
}

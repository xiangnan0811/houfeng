package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

// This independent read-only transaction checks the committed row, the
// revision reference, and the actual stored compressed bytes. It does not
// treat the service result or a nonzero count as proof of the dependency.
func verifyProduced(ctx context.Context, pool *pgxpool.Pool, gate store.AdmissionGate, out result,
	expected evidence.CanonicalSnapshot, subject records.RevisionSubject) error {
	fail := errors.New("comparison readback unavailable")
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fail
	}
	defer tx.Rollback(ctx)
	if gate.Admit(ctx, tx) != nil {
		return fail
	}
	var kind, sourceKind, sourceID, encoding string
	var schema, canonicalSize, compressedSize int64
	var payloadDigest, canonicalHash, compressed []byte
	err = tx.QueryRow(ctx, `
		select s.kind, s.schema_version, s.source_kind, s.source_id,
		       s.payload_digest, s.canonical_hash, p.payload_encoding,
		       p.canonical_size_bytes, p.compressed_size_bytes, p.compressed_payload
		from public.evidence_snapshots s
		join public.evidence_payloads p on p.payload_digest=s.payload_digest
		join public.record_revision_evidence ref
		  on ref.record_id=s.record_id and ref.snapshot_id=s.snapshot_id and ref.ordinal=0
		join public.record_revisions saved
		  on saved.record_id=ref.record_id and saved.revision_id=ref.revision_id
		join public.record_revisions compared
		  on compared.record_id=saved.record_id and compared.revision_id=saved.base_revision_id
		join public.record_revisions base
		  on base.record_id=compared.record_id and base.revision_id=compared.base_revision_id
		join public.records r on r.record_id=s.record_id and r.current_revision_id=saved.revision_id
		where s.record_id=$1 and s.snapshot_id=$2 and saved.revision_id=$3
		  and compared.revision_id=$4 and base.revision_id=$5 and r.project_id='default'
		  and r.lifecycle='active' and base.base_revision_id is null
		  and base.revision_no=1 and compared.revision_no=2 and saved.revision_no=3`,
		out.RecordID, out.SnapshotID, out.RevisionID, out.ComparedRevisionID, out.BaseRevisionID,
	).Scan(&kind, &schema, &sourceKind, &sourceID, &payloadDigest, &canonicalHash,
		&encoding, &canonicalSize, &compressedSize, &compressed)
	envelope := expected.Envelope()
	expectedDigest := expected.Hash()
	if err != nil || kind != string(evidence.KindComparisonResult) || schema != 1 ||
		sourceKind != string(subject.Kind) || sourceID != subject.SourceID ||
		envelope.Source.Type != sourceKind || envelope.Source.ID != sourceID ||
		encoding != "canonical_json_gzip_v1" || canonicalSize <= 0 || uint64(canonicalSize) != expected.Size() ||
		uint64(canonicalSize) > evidence.MaxCanonicalPayloadBytes || compressedSize <= 0 ||
		compressedSize != int64(len(compressed)) || len(payloadDigest) != sha256.Size || len(canonicalHash) != sha256.Size ||
		!bytes.Equal(payloadDigest, expectedDigest[:]) || !bytes.Equal(canonicalHash, expectedDigest[:]) {
		return fail
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return fail
	}
	actual, readErr := io.ReadAll(io.LimitReader(reader, int64(evidence.MaxCanonicalPayloadBytes)+1))
	closeErr := reader.Close()
	actualDigest := evidence.CanonicalPayloadDigest(actual)
	if readErr != nil || closeErr != nil || len(actual) != int(canonicalSize) ||
		actualDigest != expectedDigest || !bytes.Equal(actual, expected.Bytes()) {
		return fail
	}
	var document struct {
		Version string `json:"version"`
		Items   []struct {
			RecordID           string `json:"record_id"`
			RevisionID         string `json:"revision_id"`
			RevisionContext    string `json:"revision_context"`
			OriginalSnapshotID string `json:"original_snapshot_id"`
		} `json:"items"`
	}
	if json.Unmarshal(actual, &document) != nil || document.Version != "comparison_result/v1" || len(document.Items) != 2 ||
		document.Items[0].RecordID != out.RecordID || document.Items[0].RevisionID != out.BaseRevisionID ||
		document.Items[1].RecordID != out.RecordID || document.Items[1].RevisionID != out.ComparedRevisionID ||
		document.Items[0].RevisionContext != "bound" || document.Items[1].RevisionContext != "bound" ||
		document.Items[0].OriginalSnapshotID != "" || document.Items[1].OriginalSnapshotID != "" {
		return fail
	}
	if err := tx.Commit(ctx); err != nil {
		return fail
	}
	return nil
}

package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recorddeletion"
)

// ReadComparisonDecisionForTest lets the external production fixture exercise
// the private reader in its own READ COMMITTED transaction. The callback runs
// only after the real PostgreSQL header has been scanned.
func ReadComparisonDecisionForTest(ctx context.Context, tx pgx.Tx, target *recorddeletion.PreviewTarget, afterHeader func() error) ([32]byte, recorddeletion.AdapterHealthSnapshot, recorddeletion.AdapterPreviewSnapshot, error) {
	decision, err := readComparisonDeletionDecision(ctx, &comparisonDecisionBarrierTx{Tx: tx, afterHeader: afterHeader}, target)
	return decision.IndexDigest, decision.Health, decision.Preview, err
}

type comparisonDecisionBarrierTx struct {
	pgx.Tx
	afterHeader func() error
}

func (tx *comparisonDecisionBarrierTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &comparisonDecisionBarrierRows{Rows: rows, afterHeader: tx.afterHeader}, nil
}

type comparisonDecisionBarrierRows struct {
	pgx.Rows
	afterHeader func() error
	scanned     bool
}

func (rows *comparisonDecisionBarrierRows) Scan(dest ...any) error {
	if err := rows.Rows.Scan(dest...); err != nil {
		return err
	}
	if !rows.scanned {
		rows.scanned = true
		if rows.afterHeader != nil {
			return rows.afterHeader()
		}
	}
	return nil
}

// CorruptComparisonPayloadForTest injects damage that the normal foreign key
// and immutable-row protections prevent, and restores the exact original row.
// It connects only to the temporary database created by the integration fixture.
func CorruptComparisonPayloadForTest(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, snapshotID, damage string) {
	t.Helper()
	var databaseName string
	if err := runtime.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "houfeng_delivery_") {
		t.Fatalf("refusing corruption injection in non-fixture database %q", databaseName)
	}
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = databaseName
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	var digest, compressed []byte
	var encoding string
	var canonicalSize, compressedSize int64
	var createdAt time.Time
	if err := admin.QueryRow(ctx, `select payload.payload_digest, payload.payload_encoding,
		payload.canonical_size_bytes, payload.compressed_size_bytes, payload.compressed_payload, payload.created_at
		from public.evidence_snapshots as snapshot join public.evidence_payloads as payload
		  on payload.payload_digest = snapshot.payload_digest where snapshot.snapshot_id = $1`, snapshotID,
	).Scan(&digest, &encoding, &canonicalSize, &compressedSize, &compressed, &createdAt); err != nil {
		t.Fatal(err)
	}
	var malformedDigest [32]byte
	var malformedCompressed []byte
	if damage == "canonical" || damage == "mismatch" {
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		if damage == "canonical" {
			if len(canonical) == 0 || canonical[0] != '{' {
				t.Fatal("fixture comparison is not a canonical JSON object")
			}
			canonical[0] = '!'
		} else {
			changed := bytes.Replace(canonical, []byte(`"comparison_result/v1"`), []byte(`"comparison_result/v2"`), 1)
			if bytes.Equal(changed, canonical) {
				t.Fatal("fixture comparison version not found")
			}
			canonical = changed
		}
		malformedDigest = evidence.CanonicalPayloadDigest(canonical)
		malformedCompressed, err = deterministicEvidenceGzip(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if damage == "canonical" {
			var exists bool
			if err := admin.QueryRow(ctx, `select exists (select 1 from public.evidence_payloads where payload_digest = $1)`, malformedDigest[:]).Scan(&exists); err != nil || exists {
				t.Fatalf("malformed fixture digest already exists or cannot be checked: exists=%v error=%v", exists, err)
			}
		}
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `set local session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	switch damage {
	case "missing":
		_, err = tx.Exec(ctx, `delete from public.evidence_payloads where payload_digest = $1`, digest)
	case "compressed":
		_, err = tx.Exec(ctx, `update public.evidence_payloads set compressed_payload = set_byte(compressed_payload, 0, 0) where payload_digest = $1`, digest)
	case "mismatch":
		_, err = tx.Exec(ctx, `update public.evidence_payloads set compressed_payload = $2, compressed_size_bytes = $3 where payload_digest = $1`, digest, malformedCompressed, int64(len(malformedCompressed)))
	case "canonical":
		if _, err = tx.Exec(ctx, `update public.evidence_payloads set payload_digest = $2, compressed_payload = $3, compressed_size_bytes = $4 where payload_digest = $1`, digest, malformedDigest[:], malformedCompressed, int64(len(malformedCompressed))); err == nil {
			_, err = tx.Exec(ctx, `update public.evidence_snapshots set payload_digest = $2, canonical_hash = $2 where payload_digest = $1`, digest, malformedDigest[:])
		}
		if err == nil {
			_, err = tx.Exec(ctx, `update public.comparison_result_dependency_index_completions set payload_digest = $2 where payload_digest = $1`, digest, malformedDigest[:])
		}
	default:
		t.Fatalf("unknown comparison payload damage %q", damage)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		repair, err := admin.Begin(cleanup)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = repair.Rollback(cleanup) }()
		if _, err := repair.Exec(cleanup, `set local session_replication_role = replica`); err != nil {
			t.Error(err)
			return
		}
		switch damage {
		case "missing":
			_, err = repair.Exec(cleanup, `insert into public.evidence_payloads
				(payload_digest, payload_encoding, canonical_size_bytes, compressed_size_bytes, compressed_payload, created_at)
				values ($1, $2, $3, $4, $5, $6)`, digest, encoding, canonicalSize, compressedSize, compressed, createdAt)
		case "compressed":
			_, err = repair.Exec(cleanup, `update public.evidence_payloads set compressed_payload = $2 where payload_digest = $1`, digest, compressed)
		case "mismatch":
			_, err = repair.Exec(cleanup, `update public.evidence_payloads set compressed_payload = $2, compressed_size_bytes = $3 where payload_digest = $1`, digest, compressed, compressedSize)
		case "canonical":
			if _, err = repair.Exec(cleanup, `update public.evidence_payloads set payload_digest = $2, compressed_payload = $3, compressed_size_bytes = $4 where payload_digest = $1`, malformedDigest[:], digest, compressed, compressedSize); err == nil {
				_, err = repair.Exec(cleanup, `update public.evidence_snapshots set payload_digest = $2, canonical_hash = $2 where payload_digest = $1`, malformedDigest[:], digest)
			}
			if err == nil {
				_, err = repair.Exec(cleanup, `update public.comparison_result_dependency_index_completions set payload_digest = $2 where payload_digest = $1`, malformedDigest[:], digest)
			}
		}
		if err != nil {
			t.Error(fmt.Errorf("restore comparison payload: %w", err))
			return
		}
		if err := repair.Commit(cleanup); err != nil {
			t.Error(err)
		}
	})
}

// DamageComparisonIndexForTest injects one named historical/corruption case
// through the isolated fixture owner, then restores the exact original rows.
// Runtime is deliberately never granted these raw mutations.
func DamageComparisonIndexForTest(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, snapshotID, damage, recordID, revisionID, replacementRevisionID string) error {
	t.Helper()
	var databaseName string
	if err := runtime.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&databaseName); err != nil {
		return err
	}
	if !strings.HasPrefix(databaseName, "houfeng_delivery_") {
		return fmt.Errorf("refusing comparison damage in non-fixture database %q", databaseName)
	}
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil {
		return err
	}
	config.ConnConfig.Database = databaseName
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	t.Cleanup(admin.Close)
	type dependencyRow struct {
		recordID, projectID          string
		revisionID, sourceSnapshotID *string
		createdAt                    time.Time
	}
	var original []dependencyRow
	rows, err := admin.Query(ctx, `select source_record_id, source_revision_id, source_snapshot_id, project_id, created_at
		from public.comparison_result_dependencies where result_snapshot_id=$1`, snapshotID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var row dependencyRow
		if err := rows.Scan(&row.recordID, &row.revisionID, &row.sourceSnapshotID, &row.projectID, &row.createdAt); err != nil {
			rows.Close()
			return err
		}
		original = append(original, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var completionDigest []byte
	var completionProject string
	var indexedAt time.Time
	err = admin.QueryRow(ctx, `select payload_digest,project_id,indexed_at from public.comparison_result_dependency_index_completions
		where result_snapshot_id=$1`, snapshotID).Scan(&completionDigest, &completionProject, &indexedAt)
	hadCompletion := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	switch damage {
	case "missing_dependency", "wrong_revision":
		var tag pgconn.CommandTag
		tag, err = tx.Exec(ctx, `delete from public.comparison_result_dependencies where result_snapshot_id=$1
			and source_record_id=$2 and source_revision_id=$3 and source_snapshot_id is null`, snapshotID, recordID, revisionID)
		if err == nil && tag.RowsAffected() != 1 {
			err = fmt.Errorf("comparison damage removed %d rows, want one", tag.RowsAffected())
		}
		if err == nil && damage == "wrong_revision" {
			_, err = tx.Exec(ctx, `insert into public.comparison_result_dependencies
				(result_snapshot_id,source_record_id,source_revision_id) values ($1,$2,$3)`, snapshotID, recordID, replacementRevisionID)
		}
	case "missing_completion", "drop_index":
		_, err = tx.Exec(ctx, `delete from public.comparison_result_dependency_index_completions where result_snapshot_id=$1`, snapshotID)
		if err == nil && damage == "drop_index" {
			_, err = tx.Exec(ctx, `delete from public.comparison_result_dependencies where result_snapshot_id=$1`, snapshotID)
		}
	case "wrong_completion":
		wrong := sha256.Sum256([]byte("different comparison completion"))
		_, err = tx.Exec(ctx, `update public.comparison_result_dependency_index_completions set payload_digest=$2 where result_snapshot_id=$1`, snapshotID, wrong[:])
	case "extra_dependencies":
		for index := 0; index < 5 && err == nil; index++ {
			_, err = tx.Exec(ctx, `insert into public.comparison_result_dependencies
				(result_snapshot_id,source_record_id,source_revision_id) values ($1,$2,$3)`,
				snapshotID, fmt.Sprintf("rec_g2extra%d", index), fmt.Sprintf("rrv_g2extra%d", index))
		}
	default:
		return fmt.Errorf("unknown comparison damage %q", damage)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		repair, err := admin.Begin(cleanup)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = repair.Rollback(cleanup) }()
		if _, err = repair.Exec(cleanup, `delete from public.comparison_result_dependency_index_completions where result_snapshot_id=$1`, snapshotID); err == nil {
			_, err = repair.Exec(cleanup, `delete from public.comparison_result_dependencies where result_snapshot_id=$1`, snapshotID)
		}
		for _, row := range original {
			if err != nil {
				break
			}
			_, err = repair.Exec(cleanup, `insert into public.comparison_result_dependencies
				(result_snapshot_id,source_record_id,source_revision_id,source_snapshot_id,project_id,created_at)
				values ($1,$2,$3,$4,$5,$6)`, snapshotID, row.recordID, row.revisionID, row.sourceSnapshotID, row.projectID, row.createdAt)
		}
		if err == nil && hadCompletion {
			_, err = repair.Exec(cleanup, `insert into public.comparison_result_dependency_index_completions
				(result_snapshot_id,payload_digest,project_id,indexed_at) values ($1,$2,$3,$4)`, snapshotID, completionDigest, completionProject, indexedAt)
		}
		if err == nil {
			err = repair.Commit(cleanup)
		}
		if err != nil {
			t.Errorf("restore comparison index: %v", err)
		}
	})
	return nil
}

// SeedComparisonClientInventoryForTest creates existing rows only in the
// isolated fixture so direct runtime UPDATE and DELETE can be checked too.
func SeedComparisonClientInventoryForTest(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, recordID string) {
	t.Helper()
	var databaseName string
	if err := runtime.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "houfeng_delivery_") {
		t.Fatalf("refusing comparison seed in non-fixture database %q", databaseName)
	}
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = databaseName
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into public.comparison_client_workbenches
		(user_id,browser_id,workbench_digest,destination_record_id,source_record_ids,generation)
		values ('usr_guardexisting','guard_existing',decode(repeat('cd',32),'hex'),$1,array[$1],1)`, recordID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into public.comparison_client_save_attempts
		(user_id,browser_id,save_digest,destination_record_id,source_record_ids,idempotency_key,generation)
		values ('usr_guardexisting','guard_existing',decode(repeat('cd',32),'hex'),$1,array[$1],'guard_existing',1)`, recordID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, `delete from public.comparison_client_save_attempts
			where user_id='usr_guardexisting' and browser_id='guard_existing'`); err != nil {
			t.Error(err)
		}
		if _, err := admin.Exec(cleanup, `delete from public.comparison_client_workbenches
			where user_id='usr_guardexisting' and browser_id='guard_existing'`); err != nil {
			t.Error(err)
		}
	})
}

package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/records"
)

// acquireUnreferencedEvidencePayloads discovers candidate digests without locks,
// then acquires their actual payload rows in sorted order. The deleting
// statement must recheck age and references in a fresh READ COMMITTED snapshot.
func acquireUnreferencedEvidencePayloads(ctx context.Context, tx pgx.Tx, limit uint64, grace time.Duration) ([][]byte, error) {
	rows, err := tx.Query(ctx, `select payload.payload_digest from public.evidence_payloads payload
		where ($1::bigint=0 or payload.created_at<=clock_timestamp()-($1::bigint * interval '1 microsecond'))
		  and not exists(select 1 from public.evidence_snapshots snapshot where snapshot.payload_digest=payload.payload_digest)
		order by payload.created_at,payload.payload_digest limit nullif($2::bigint,0)`, grace.Microseconds(), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("discover unreferenced evidence payloads: %w", err)
	}
	var digests [][]byte
	var resources records.RevisionMutationResources
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if len(raw) != sha256.Size {
			rows.Close()
			return nil, ErrEvidencePersistenceConflict
		}
		var digest [sha256.Size]byte
		copy(digest[:], raw)
		resources.Payloads = append(resources.Payloads, records.RevisionMutationPayload{PayloadDigest: digest})
		digests = append(digests, append([]byte(nil), raw...))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	declared, err := normalizeRecordMutationDeclarations(resources)
	if err != nil {
		return nil, err
	}
	if err := acquireRecordMutationResources(ctx, tx, declared); err != nil {
		return nil, err
	}
	return digests, nil
}

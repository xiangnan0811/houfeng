package migrate

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresAppACLManifestRuntimeReader loads the application ACL manifest
// runtime snapshot from PostgreSQL in a repeatable, read-only transaction.
type PostgresAppACLManifestRuntimeReader struct {
	db *pgxpool.Pool
}

// NewPostgresAppACLManifestRuntimeReader binds a PostgreSQL pool to the
// read-only runtime snapshot reader.
func NewPostgresAppACLManifestRuntimeReader(db *pgxpool.Pool) *PostgresAppACLManifestRuntimeReader {
	return &PostgresAppACLManifestRuntimeReader{db: db}
}

// ReadAppACLManifestRuntimeSnapshotV1 reads every persisted revision, the
// nullable singleton head, and the applied application migration ledger.
func (reader *PostgresAppACLManifestRuntimeReader) ReadAppACLManifestRuntimeSnapshotV1(ctx context.Context) (snapshot AppACLManifestRuntimeSnapshotV1, err error) {
	if reader == nil || reader.db == nil {
		return AppACLManifestRuntimeSnapshotV1{}, fmt.Errorf("app ACL manifest PostgreSQL reader has no pool")
	}

	tx, err := reader.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return AppACLManifestRuntimeSnapshotV1{}, fmt.Errorf("begin read-only app ACL manifest snapshot: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if snapshot.Manifests, err = readAppACLManifestRevisionsV1(ctx, tx); err != nil {
		return AppACLManifestRuntimeSnapshotV1{}, err
	}
	if snapshot.Head, err = readAppACLManifestHeadV1(ctx, tx); err != nil {
		return AppACLManifestRuntimeSnapshotV1{}, err
	}
	if snapshot.AppliedMigrations, err = readAppliedAppMigrationsV1(ctx, tx); err != nil {
		return AppACLManifestRuntimeSnapshotV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLManifestRuntimeSnapshotV1{}, fmt.Errorf("commit read-only app ACL manifest snapshot: %w", err)
	}
	return snapshot, nil
}

func readAppACLManifestRevisionsV1(ctx context.Context, tx pgx.Tx) ([]AppACLManifestPersistedV1, error) {
	rows, err := tx.Query(ctx, `
		select manifest_revision,
		       previous_manifest_digest,
		       canonical_migration_set,
		       sorted_migration_set_digest,
		       canonical_privilege_set,
		       privilege_set_digest,
		       manifest_digest
		from public.app_acl_manifest_revisions
		order by manifest_revision
	`)
	if err != nil {
		return nil, fmt.Errorf("read app ACL manifest revisions: %w", err)
	}
	defer rows.Close()

	manifests := make([]AppACLManifestPersistedV1, 0)
	for rows.Next() {
		var revision int64
		var previousDigest, migrationSet, migrationSetDigest, privilegeSet, privilegeSetDigest, manifestDigest []byte
		if err := rows.Scan(
			&revision,
			&previousDigest,
			&migrationSet,
			&migrationSetDigest,
			&privilegeSet,
			&privilegeSetDigest,
			&manifestDigest,
		); err != nil {
			return nil, fmt.Errorf("scan app ACL manifest revision: %w", err)
		}
		manifestRevision, err := appACLManifestRevisionFromInt64(revision)
		if err != nil {
			return nil, err
		}
		previous, err := appACLManifestDigestFromBytes("previous manifest digest", previousDigest)
		if err != nil {
			return nil, err
		}
		migrationDigest, err := appACLManifestDigestFromBytes("migration set digest", migrationSetDigest)
		if err != nil {
			return nil, err
		}
		privilegeDigest, err := appACLManifestDigestFromBytes("privilege set digest", privilegeSetDigest)
		if err != nil {
			return nil, err
		}
		digest, err := appACLManifestDigestFromBytes("manifest digest", manifestDigest)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, AppACLManifestPersistedV1{
			ManifestRevision:       manifestRevision,
			PreviousManifestDigest: previous,
			CanonicalMigrationSet:  append([]byte(nil), migrationSet...),
			MigrationSetDigest:     migrationDigest,
			CanonicalPrivilegeSet:  append([]byte(nil), privilegeSet...),
			PrivilegeSetDigest:     privilegeDigest,
			ManifestDigest:         digest,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL manifest revisions: %w", err)
	}
	return manifests, nil
}

func readAppACLManifestHeadV1(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
	var revision pgtype.Int8
	var digest []byte
	if err := tx.QueryRow(ctx, `
		select manifest_revision, manifest_digest
		from public.app_acl_manifest_head
		where singleton
	`).Scan(&revision, &digest); err != nil {
		return nil, fmt.Errorf("read app ACL manifest head: %w", err)
	}
	if !revision.Valid {
		if digest != nil {
			return nil, fmt.Errorf("app ACL manifest head has a digest without a revision")
		}
		return nil, nil
	}
	manifestRevision, err := appACLManifestRevisionFromInt64(revision.Int64)
	if err != nil {
		return nil, err
	}
	manifestDigest, err := appACLManifestDigestFromBytes("app ACL manifest head digest", digest)
	if err != nil {
		return nil, err
	}
	return &AppACLManifestHeadV1{ManifestRevision: manifestRevision, ManifestDigest: manifestDigest}, nil
}

func readAppliedAppMigrationsV1(ctx context.Context, tx pgx.Tx) ([]MigrationChecksumEntry, error) {
	rows, err := tx.Query(ctx, `
		select name, checksum
		from public.schema_migrations
		order by name
	`)
	if err != nil {
		return nil, fmt.Errorf("read applied application migration ledger: %w", err)
	}
	defer rows.Close()

	entries := make([]MigrationChecksumEntry, 0)
	for rows.Next() {
		var filename, checksumHex string
		if err := rows.Scan(&filename, &checksumHex); err != nil {
			return nil, fmt.Errorf("scan applied application migration ledger: %w", err)
		}
		if len(checksumHex) != 64 || checksumHex != strings.ToLower(checksumHex) {
			return nil, fmt.Errorf("applied application migration %q has a non-canonical checksum", filename)
		}
		checksumBytes, err := hex.DecodeString(checksumHex)
		if err != nil || len(checksumBytes) != 32 {
			return nil, fmt.Errorf("decode applied application migration checksum for %q", filename)
		}
		var checksum [32]byte
		copy(checksum[:], checksumBytes)
		entries = append(entries, MigrationChecksumEntry{Filename: filename, Checksum: checksum})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied application migration ledger: %w", err)
	}
	return entries, nil
}

func appACLManifestRevisionFromInt64(revision int64) (uint64, error) {
	if revision < 1 || revision > 999999 {
		return 0, fmt.Errorf("app ACL manifest revision %d is outside v1 bounds", revision)
	}
	return uint64(revision), nil
}

func appACLManifestDigestFromBytes(field string, value []byte) ([32]byte, error) {
	if len(value) != 32 {
		return [32]byte{}, fmt.Errorf("%s has length %d, want 32", field, len(value))
	}
	var digest [32]byte
	copy(digest[:], value)
	return digest, nil
}

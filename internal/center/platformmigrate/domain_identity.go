package platformmigrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DomainKind identifies the local PostgreSQL recovery domain being provisioned.
// The migration for each domain pins its own closed value in SQL as a second
// line of defense.
type DomainKind string

const (
	DomainKindApplication     DomainKind = "application"
	DomainKindDeletionLedger  DomainKind = "deletion_ledger"
	DomainKindDeletionWitness DomainKind = "deletion_witness"
	DomainKindRecoveryControl DomainKind = "recovery_control"
)

// Validate rejects values that are not owned by a local PostgreSQL domain.
func (kind DomainKind) Validate() error {
	switch kind {
	case DomainKindApplication, DomainKindDeletionLedger, DomainKindDeletionWitness, DomainKindRecoveryControl:
		return nil
	default:
		return fmt.Errorf("unknown record-platform domain kind %q", kind)
	}
}

// NewDomainID produces the immutable domain identifier used in local identity
// rows. It intentionally does not derive identity from a DSN or database name.
func NewDomainID() (string, error) {
	return newDomainID(rand.Reader)
}

func newDomainID(reader io.Reader) (string, error) {
	var entropy [32]byte
	if _, err := io.ReadFull(reader, entropy[:]); err != nil {
		return "", fmt.Errorf("read domain ID randomness: %w", err)
	}
	return "rd-" + hex.EncodeToString(entropy[:]), nil
}

// DomainIdentity is the local, immutable physical identity captured during
// first provisioning. It deliberately contains no DSN or endpoint data.
type DomainIdentity struct {
	ID                       string
	Kind                     DomainKind
	IdentityEpoch            int64
	Mode                     string
	PostgresSystemIdentifier string
	DatabaseOID              uint32
	DatabaseName             string
}

// ProvisionPostgresDomainIdentity records the physical PostgreSQL identity of
// a migrated local domain exactly once. Repeated calls only succeed when the
// database and requested domain kind are byte-for-byte the original identity.
func ProvisionPostgresDomainIdentity(ctx context.Context, db *pgxpool.Pool, kind DomainKind) (identity DomainIdentity, err error) {
	if err := kind.Validate(); err != nil {
		return DomainIdentity{}, err
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return DomainIdentity{}, fmt.Errorf("begin domain identity transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `lock table public.record_platform_domain_identity in share row exclusive mode`); err != nil {
		return DomainIdentity{}, fmt.Errorf("lock local domain identity: %w", err)
	}

	var databaseOID uint32
	var databaseName string
	if err := tx.QueryRow(ctx, `
		select oid, datname
		from pg_database
		where datname = current_database()
	`).Scan(&databaseOID, &databaseName); err != nil {
		return DomainIdentity{}, fmt.Errorf("read PostgreSQL database identity: %w", err)
	}

	var systemIdentifier string
	if err := tx.QueryRow(ctx, `select system_identifier::text from pg_control_system()`).Scan(&systemIdentifier); err != nil {
		return DomainIdentity{}, fmt.Errorf("read PostgreSQL system identifier: %w", err)
	}

	rows, err := tx.Query(ctx, `
		select domain_id, domain_kind, identity_epoch, identity_mode,
		       coalesce(postgres_system_identifier, ''), database_oid, database_name
		from public.record_platform_domain_identity
		order by provisioned_at, domain_id
	`)
	if err != nil {
		return DomainIdentity{}, fmt.Errorf("read local domain identity: %w", err)
	}
	defer rows.Close()

	var existing []DomainIdentity
	for rows.Next() {
		var row DomainIdentity
		var storedKind string
		if err := rows.Scan(
			&row.ID,
			&storedKind,
			&row.IdentityEpoch,
			&row.Mode,
			&row.PostgresSystemIdentifier,
			&row.DatabaseOID,
			&row.DatabaseName,
		); err != nil {
			return DomainIdentity{}, fmt.Errorf("scan local domain identity: %w", err)
		}
		row.Kind = DomainKind(storedKind)
		existing = append(existing, row)
	}
	if err := rows.Err(); err != nil {
		return DomainIdentity{}, fmt.Errorf("iterate local domain identity: %w", err)
	}

	if len(existing) > 0 {
		if len(existing) != 1 {
			return DomainIdentity{}, fmt.Errorf("local domain identity has multiple provisioned rows")
		}
		row := existing[0]
		if row.Kind != kind || row.Mode != "postgres_system" || row.IdentityEpoch != 1 ||
			row.PostgresSystemIdentifier != systemIdentifier || row.DatabaseOID != databaseOID || row.DatabaseName != databaseName {
			return DomainIdentity{}, fmt.Errorf("local domain identity does not exactly match this PostgreSQL domain")
		}
		if err := tx.Commit(ctx); err != nil {
			return DomainIdentity{}, fmt.Errorf("commit existing domain identity verification: %w", err)
		}
		return row, nil
	}

	domainID, err := NewDomainID()
	if err != nil {
		return DomainIdentity{}, err
	}
	identity = DomainIdentity{
		ID:                       domainID,
		Kind:                     kind,
		IdentityEpoch:            1,
		Mode:                     "postgres_system",
		PostgresSystemIdentifier: systemIdentifier,
		DatabaseOID:              databaseOID,
		DatabaseName:             databaseName,
	}
	if _, err := tx.Exec(ctx, `
		insert into public.record_platform_domain_identity (
			domain_id,
			domain_kind,
			identity_epoch,
			identity_mode,
			postgres_system_identifier,
			database_oid,
			database_name
		) values ($1, $2, $3, $4, $5, $6, $7)
	`,
		identity.ID,
		string(identity.Kind),
		identity.IdentityEpoch,
		identity.Mode,
		identity.PostgresSystemIdentifier,
		identity.DatabaseOID,
		identity.DatabaseName,
	); err != nil {
		return DomainIdentity{}, fmt.Errorf("insert local domain identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return DomainIdentity{}, fmt.Errorf("commit local domain identity: %w", err)
	}
	return identity, nil
}

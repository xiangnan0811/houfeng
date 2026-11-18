package recordbirth

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/sourceretirement"
)

// NewPostgresContinuityReader binds a dedicated W2 reader to the physical
// identity obtained from protected control. It performs no database I/O;
// Continuity checks the live W2 and admitted APP identities on each call.
// The caller retains ownership of both pools.
func NewPostgresContinuityReader(reader, application *pgxpool.Pool, expected sourceretirement.DomainIdentity) (*PostgresAuthority, error) {
	if reader == nil || application == nil || reader == application {
		return nil, fmt.Errorf("%w: distinct continuity and application pools are required", ErrInvalid)
	}
	identity := postgresPhysicalIdentity{
		ID: expected.ID, SystemIdentifier: expected.SystemIdentifier,
		DatabaseOID: expected.DatabaseOID, DatabaseName: expected.DatabaseName,
		Epoch: expected.Epoch,
	}
	if !validPostgresPhysicalIdentity(identity) {
		return nil, fmt.Errorf("%w: invalid continuity reader identity", ErrInvalid)
	}
	return &PostgresAuthority{pool: reader, application: application, expected: &identity}, nil
}

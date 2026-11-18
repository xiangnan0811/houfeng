package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordbirth"
)

// ImportBirthReplayFixture exposes the real admitted APP and W2 authorities to
// the external import workflow test. It does not model deployment recovery.
type ImportBirthReplayFixture struct {
	Runtime     *pgxpool.Pool
	AppOwner    *pgxpool.Pool
	Witness     *pgxpool.Pool
	Gate        AdmissionGate
	Tombstones  *WitnessedRecordSubjectTombstoneReader
	Authority   recordbirth.Authority
	Reader      recordbirth.ContinuityReader
	Destination string
	Refresh     func()
}

func NewImportBirthReplayFixture(t *testing.T, ctx context.Context) ImportBirthReplayFixture {
	t.Helper()
	fixture := newSourceRetirementPGFixture(t, ctx)
	fixture.refresh()
	authority, reader, _ := birthTestAuthority(t, ctx, fixture)
	vps, _ := fixture.newVPS(t, ctx, "import birth confirmation")
	return ImportBirthReplayFixture{
		Runtime: fixture.runtime, AppOwner: fixture.app.db, Witness: fixture.witnessDB,
		Gate: fixture.gate, Tombstones: fixture.tombstoneReader(t), Authority: authority, Reader: reader,
		Destination: vps.VPSID, Refresh: fixture.refresh,
	}
}

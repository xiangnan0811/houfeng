package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

func TestPostgresRuntimeFactsRepositoryImplementsRuntimeFactsRepository(t *testing.T) {
	t.Parallel()

	var repo runtimefacts.Repository = (*PostgresRuntimeFactsRepository)(nil)
	if repo == nil {
		t.Fatal("repository interface assignment returned nil")
	}
}

func TestGetNodeRuntimeFactsReturnsLatestHostSample(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsNodeExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			case runtimeFactsLatestHostSampleSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "nd_001"
					*(dest[1].(*time.Time)) = observedAt
					*(dest[2].(*time.Time)) = observedAt.Add(2 * time.Second)
					*(dest[3].(*string)) = "agent/v0.1.0"
					*(dest[4].(*string)) = "fp-001"
					*(dest[5].(*float64)) = 50
					*(dest[6].(*float64)) = 0.8
					*(dest[7].(*float64)) = 0.7
					*(dest[8].(*float64)) = 0.6
					*(dest[9].(*float64)) = 72
					*(dest[10].(*int64)) = 2147483648
					*(dest[11].(*float64)) = 0
					*(dest[12].(*float64)) = 61
					*(dest[13].(*float64)) = 22
					*(dest[14].(*int64)) = 1200
					*(dest[15].(*int64)) = 900
					*(dest[16].(*float64)) = 1.2
					*(dest[17].(*float64)) = 0.1
					*(dest[18].(*int64)) = 400
					*(dest[19].(*int64)) = 300
					*(dest[20].(*float64)) = 5
					*(dest[21].(*int64)) = 3600
					*(dest[22].(*bool)) = false
					*(dest[23].(*bool)) = false
					*(dest[24].(*string)) = "sync_001"
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		},
	}}

	facts, err := repo.GetNodeRuntimeFacts(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("GetNodeRuntimeFacts() error = %v", err)
	}
	if facts.NodeID != "nd_001" {
		t.Fatalf("NodeID = %q, want %q", facts.NodeID, "nd_001")
	}
	if facts.LatestHostSample == nil {
		t.Fatal("LatestHostSample = nil, want non-nil")
	}
	if facts.LatestHostSample.AgentVersion != "agent/v0.1.0" {
		t.Fatalf("LatestHostSample.AgentVersion = %q, want %q", facts.LatestHostSample.AgentVersion, "agent/v0.1.0")
	}
	if facts.LatestHostSample.CPUIOWaitPct != 1.2 {
		t.Fatalf("LatestHostSample.CPUIOWaitPct = %v, want %v", facts.LatestHostSample.CPUIOWaitPct, 1.2)
	}
}

func TestGetNodeRuntimeFactsReturnsNilHostSampleWhenNodeHasNoFactsYet(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsNodeExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			case runtimeFactsLatestHostSampleSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		},
	}}

	facts, err := repo.GetNodeRuntimeFacts(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("GetNodeRuntimeFacts() error = %v", err)
	}
	if facts.LatestHostSample != nil {
		t.Fatalf("LatestHostSample = %#v, want nil", facts.LatestHostSample)
	}
}

func TestGetNodeRuntimeFactsReturnsNodeNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRuntimeFactsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	_, err := repo.GetNodeRuntimeFacts(context.Background(), "nd_missing")
	if !errors.Is(err, nodes.ErrNodeNotFound) {
		t.Fatalf("GetNodeRuntimeFacts() error = %v, want ErrNodeNotFound", err)
	}
}

func TestGetTargetRuntimeFactsReturnsLatestProbeObservations(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 24, 9, 5, 0, 0, time.UTC)
	latency := 0
	httpStatus := 200
	tlsExpiryDays := 14
	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsTargetExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != runtimeFactsLatestProbeObservationsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeRuntimeFactsRows{rows: []fakeRuntimeFactsScan{{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "nd_001"
				*(dest[1].(*string)) = "tg_001"
				*(dest[2].(*string)) = "pb_001"
				*(dest[3].(*string)) = "http"
				*(dest[4].(*time.Time)) = observedAt
				*(dest[5].(*time.Time)) = observedAt.Add(1500 * time.Millisecond)
				*(dest[6].(*string)) = "agent/v0.1.0"
				*(dest[7].(*string)) = "fp-001"
				*(dest[8].(*string)) = "success"
				*(dest[9].(**int)) = &latency
				*(dest[10].(**int)) = &httpStatus
				*(dest[11].(**int)) = &tlsExpiryDays
				*(dest[12].(*string)) = ""
				*(dest[13].(*string)) = ""
				*(dest[14].(*bool)) = false
				*(dest[15].(*bool)) = false
				*(dest[16].(*string)) = "sync_001"
				return nil
			}}}}, nil
		},
	}}

	facts, err := repo.GetTargetRuntimeFacts(context.Background(), "tg_001")
	if err != nil {
		t.Fatalf("GetTargetRuntimeFacts() error = %v", err)
	}
	if facts.TargetID != "tg_001" {
		t.Fatalf("TargetID = %q, want %q", facts.TargetID, "tg_001")
	}
	if len(facts.LatestProbeObservations) != 1 {
		t.Fatalf("len(LatestProbeObservations) = %d, want 1", len(facts.LatestProbeObservations))
	}
	observation := facts.LatestProbeObservations[0]
	if observation.ProbeKind != "http" {
		t.Fatalf("ProbeKind = %q, want %q", observation.ProbeKind, "http")
	}
	if observation.LatencyMS == nil || *observation.LatencyMS != 0 {
		t.Fatalf("LatencyMS = %v, want 0", observation.LatencyMS)
	}
	if observation.HTTPStatus == nil || *observation.HTTPStatus != 200 {
		t.Fatalf("HTTPStatus = %v, want 200", observation.HTTPStatus)
	}
	if observation.TLSExpiryDays == nil || *observation.TLSExpiryDays != 14 {
		t.Fatalf("TLSExpiryDays = %v, want 14", observation.TLSExpiryDays)
	}
}

func TestGetTargetRuntimeFactsReturnsEmptyFactsForKnownTarget(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch sql {
			case runtimeFactsTargetExistsSQL:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			default:
				return fakeRuntimeFactsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
		},
		query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &fakeRuntimeFactsRows{}, nil
		},
	}}

	facts, err := repo.GetTargetRuntimeFacts(context.Background(), "tg_001")
	if err != nil {
		t.Fatalf("GetTargetRuntimeFacts() error = %v", err)
	}
	if len(facts.LatestProbeObservations) != 0 {
		t.Fatalf("len(LatestProbeObservations) = %d, want 0", len(facts.LatestProbeObservations))
	}
}

func TestGetTargetRuntimeFactsReturnsTargetNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresRuntimeFactsRepository{db: fakeRuntimeFactsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRuntimeFactsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	_, err := repo.GetTargetRuntimeFacts(context.Background(), "tg_missing")
	if !errors.Is(err, targets.ErrTargetNotFound) {
		t.Fatalf("GetTargetRuntimeFacts() error = %v, want ErrTargetNotFound", err)
	}
}

func TestRuntimeFactSQLLocksLatestOrderingAndProbeJoinShape(t *testing.T) {
	t.Parallel()

	if !strings.Contains(runtimeFactsLatestHostSampleSQL, "order by observed_at desc, id desc") {
		t.Fatalf("runtimeFactsLatestHostSampleSQL = %q, want latest host sample ordering", runtimeFactsLatestHostSampleSQL)
	}
	if !strings.Contains(runtimeFactsLatestProbeObservationsSQL, "join probe_items") {
		t.Fatalf("runtimeFactsLatestProbeObservationsSQL = %q, want probe_items join", runtimeFactsLatestProbeObservationsSQL)
	}
	if !strings.Contains(runtimeFactsLatestProbeObservationsSQL, "distinct on (po.probe_item_id, po.node_id)") {
		t.Fatalf("runtimeFactsLatestProbeObservationsSQL = %q, want distinct on probe_item_id,node_id", runtimeFactsLatestProbeObservationsSQL)
	}
}

type fakeRuntimeFactsQueryer struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func (f fakeRuntimeFactsQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

func (f fakeRuntimeFactsQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeRuntimeFactsRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

type fakeRuntimeFactsRow struct {
	scan func(dest ...any) error
}

func (f fakeRuntimeFactsRow) Scan(dest ...any) error {
	return f.scan(dest...)
}

type fakeRuntimeFactsScan struct {
	scan func(dest ...any) error
}

type fakeRuntimeFactsRows struct {
	rows []fakeRuntimeFactsScan
	idx  int
	err  error
}

func (f *fakeRuntimeFactsRows) Close()                                       {}
func (f *fakeRuntimeFactsRows) Err() error                                   { return f.err }
func (f *fakeRuntimeFactsRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeRuntimeFactsRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRuntimeFactsRows) RawValues() [][]byte                          { return nil }
func (f *fakeRuntimeFactsRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeRuntimeFactsRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeRuntimeFactsRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeRuntimeFactsRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

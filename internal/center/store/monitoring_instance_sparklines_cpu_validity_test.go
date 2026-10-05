package store

import (
	"context"
	stdsql "database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeSparklineQueryer struct {
	rows []fakeSparklineRow
}

func (f fakeSparklineQueryer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &fakeSparklineRows{rows: f.rows}, nil
}

type fakeSparklineRow struct {
	monitoringInstanceID string
	observedAt           time.Time
	values               [19]float64
	cpuRatesValid        *bool
}

type fakeSparklineRows struct {
	rows []fakeSparklineRow
	idx  int
}

func (f *fakeSparklineRows) Close()                                       {}
func (f *fakeSparklineRows) Err() error                                   { return nil }
func (f *fakeSparklineRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeSparklineRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeSparklineRows) RawValues() [][]byte                          { return nil }
func (f *fakeSparklineRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeSparklineRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeSparklineRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeSparklineRows) Scan(dest ...any) error {
	row := f.rows[f.idx-1]
	*(dest[0].(*string)) = row.monitoringInstanceID
	*(dest[1].(*time.Time)) = row.observedAt
	for index := range row.values {
		*(dest[index+2].(*float64)) = row.values[index]
	}
	marker := dest[21].(*stdsql.NullBool)
	if row.cpuRatesValid != nil {
		marker.Valid = true
		marker.Bool = *row.cpuRatesValid
	}
	return nil
}

func TestMonitoringInstanceSparklinesSkipUnusableCPUButKeepOtherMetrics(t *testing.T) {
	t.Parallel()

	since := time.Now().UTC().Add(-2 * time.Hour)
	result, err := (&PostgresMonitoringInstanceSparklinesRepository{db: fakeSparklineQueryer{rows: []fakeSparklineRow{
		{
			monitoringInstanceID: "mi_001",
			observedAt:           since.Add(10 * time.Minute),
			values:               [19]float64{0, 0, 0, 0, 1},
			cpuRatesValid:        new(false),
		},
		{
			monitoringInstanceID: "mi_001",
			observedAt:           since.Add(20 * time.Minute),
			values:               [19]float64{40, 0, 0, 0, 3},
			cpuRatesValid:        new(true),
		},
		{
			monitoringInstanceID: "mi_001",
			observedAt:           since.Add(30 * time.Minute),
			values:               [19]float64{20, 0, 0, 0, 5},
		},
	}}}).GetMonitoringInstanceSparklines(context.Background(), []string{"cpu_usage_pct", "mem_used_pct"}, since, 1)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceSparklines() error = %v", err)
	}
	cpu := result["mi_001"]["cpu_usage_pct"][0]
	if cpu == nil || *cpu != 30 {
		t.Fatalf("CPU sparkline = %#v, want average 30 from usable rows", cpu)
	}
	memory := result["mi_001"]["mem_used_pct"][0]
	if memory == nil || *memory != 3 {
		t.Fatalf("memory sparkline = %#v, want average 3 from all rows", memory)
	}
}

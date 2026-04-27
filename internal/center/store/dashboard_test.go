package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/incidents"
)

func TestPostgresDashboardRepositoryReturnsOverviewAndRecentEvents(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	repo := &PostgresDashboardRepository{db: fakeDashboardQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return fakeRow{scan: func(dest ...any) error {
				*(dest[0].(*int)) = 5
				*(dest[1].(*int)) = 4
				*(dest[2].(*int)) = 2
				*(dest[3].(*int)) = 1
				*(dest[4].(*int)) = 1
				*(dest[5].(*int)) = 0
				*(dest[6].(*int)) = 1
				*(dest[7].(*int)) = 1
				*(dest[8].(*int)) = 3
				*(dest[9].(*int)) = 2
				return nil
			}}
		},
		query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &fakeDashboardRows{rows: []fakeDashboardScan{{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "evt_001"
				*(dest[1].(*incidents.ObjectType)) = incidents.ObjectTypeNode
				*(dest[2].(*string)) = "nd_001"
				*(dest[3].(*incidents.EventType)) = incidents.EventIncidentStarted
				*(dest[4].(*string)) = string(incidents.SeverityAlert)
				*(dest[5].(*string)) = "磁盘使用率 92.0%"
				*(dest[6].(*[]byte)) = []byte(`{"incident_id":"inc_001","incident_class":"node_disk_pressure","attempts":3,"maintenance":false}`)
				*(dest[7].(*time.Time)) = now
				return nil
			}}}}, nil
		},
	}}

	overview, err := repo.GetDashboardOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetDashboardOverview() error = %v", err)
	}
	if overview.TotalNodeCount != 5 || overview.TotalTargetCount != 4 {
		t.Fatalf("total counts = (%d,%d), want (5,4)", overview.TotalNodeCount, overview.TotalTargetCount)
	}
	if overview.AbnormalNodeCount != 2 || overview.RecentRecoveryCount != 2 {
		t.Fatalf("overview = %#v, want populated counts", overview)
	}
	if len(overview.RecentEvents) != 1 || overview.RecentEvents[0].IncidentID != "inc_001" {
		t.Fatalf("RecentEvents = %#v, want decoded event payload", overview.RecentEvents)
	}
}

func TestPostgresDashboardRepositoryListEventsBuildsFilters(t *testing.T) {
	capturedSQL := ""
	repo := &PostgresDashboardRepository{db: fakeDashboardQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRow{scan: func(dest ...any) error { return nil }}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			capturedSQL = sql
			return &fakeDashboardRows{}, nil
		},
	}}

	_, err := repo.ListEvents(context.Background(), EventsFilter{
		ObjectType: incidents.ObjectTypeTarget,
		ObjectID:   "tg_001",
		Severity:   incidents.SeverityAlert,
		EventType:  incidents.EventIncidentEscalated,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, want := range []string{"object_type = $1", "object_id = $2", "severity = $3", "event_type = $4"} {
		if !containsSQL([]string{capturedSQL}, want) {
			t.Fatalf("capturedSQL = %q, want %q", capturedSQL, want)
		}
	}
}

type fakeDashboardQueryer struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func (f fakeDashboardQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

func (f fakeDashboardQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return f.query(ctx, sql, args...)
}

type fakeDashboardScan struct{ scan func(dest ...any) error }

type fakeDashboardRows struct {
	rows []fakeDashboardScan
	idx  int
}

func (f *fakeDashboardRows) Close()                                       {}
func (f *fakeDashboardRows) Err() error                                   { return nil }
func (f *fakeDashboardRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeDashboardRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeDashboardRows) RawValues() [][]byte                          { return nil }
func (f *fakeDashboardRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeDashboardRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeDashboardRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeDashboardRows) Scan(dest ...any) error { return f.rows[f.idx-1].scan(dest...) }

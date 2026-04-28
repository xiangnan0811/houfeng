package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/incidents"
)

func TestPostgresDashboardRepositoryReturnsOverviewAndRecentEvents(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-5 * time.Minute)
	lastSuccess := now.Add(-10 * time.Minute)
	lastFailure := now.Add(-2 * time.Minute)
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
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			switch {
			case strings.Contains(sql, "from state_change_events"):
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
			case strings.Contains(sql, "from nodes"):
				return &fakeDashboardRows{rows: []fakeDashboardScan{{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "nd_001"
					*(dest[1].(*string)) = "Tokyo Edge"
					*(dest[2].(*string)) = "ap-northeast-1"
					*(dest[3].(*string)) = "Tokyo"
					*(dest[4].(*string)) = "aws"
					*(dest[5].(*string)) = "在用"
					*(dest[6].(*string)) = "启用"
					*(dest[7].(*string)) = string(incidents.SeverityAlert)
					*(dest[8].(**time.Time)) = &lastHeartbeat
					*(dest[9].(*int)) = 2
					*(dest[10].(*string)) = "磁盘使用率 92.0%"
					return nil
				}}}}, nil
			case strings.Contains(sql, "from targets"):
				return &fakeDashboardRows{rows: []fakeDashboardScan{{scan: func(dest ...any) error {
					basePort := 443
					*(dest[0].(*string)) = "tg_001"
					*(dest[1].(*string)) = "Blog"
					*(dest[2].(*string)) = "service"
					*(dest[3].(*string)) = "blog.example.com"
					*(dest[4].(**int)) = &basePort
					*(dest[5].(*string)) = "启用"
					*(dest[6].(*string)) = string(incidents.SeverityCritical)
					*(dest[7].(**time.Time)) = &lastSuccess
					*(dest[8].(**time.Time)) = &lastFailure
					*(dest[9].(*int)) = 1
					*(dest[10].(*string)) = "HTTPS 探测连续失败"
					return nil
				}}}}, nil
			default:
				return &fakeDashboardRows{}, nil
			}
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
	if len(overview.AbnormalNodes) != 1 || overview.AbnormalNodes[0].NodeID != "nd_001" {
		t.Fatalf("AbnormalNodes = %#v, want node summary", overview.AbnormalNodes)
	}
	if overview.AbnormalNodes[0].CurrentPrimaryIssueSummary != "磁盘使用率 92.0%" || overview.AbnormalNodes[0].LastHeartbeatAt == nil {
		t.Fatalf("AbnormalNodes[0] = %#v, want issue summary and heartbeat", overview.AbnormalNodes[0])
	}
	if len(overview.AbnormalTargets) != 1 || overview.AbnormalTargets[0].TargetID != "tg_001" {
		t.Fatalf("AbnormalTargets = %#v, want target summary", overview.AbnormalTargets)
	}
	if overview.AbnormalTargets[0].BasePort == nil || *overview.AbnormalTargets[0].BasePort != 443 || overview.AbnormalTargets[0].LastFailureAt == nil {
		t.Fatalf("AbnormalTargets[0] = %#v, want base port and failure timestamp", overview.AbnormalTargets[0])
	}
}

func TestPostgresDashboardRepositoryBuildsAbnormalSummaryQueries(t *testing.T) {
	capturedSQL := []string{}
	repo := &PostgresDashboardRepository{db: fakeDashboardQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRow{scan: func(dest ...any) error { return nil }}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			capturedSQL = append(capturedSQL, sql)
			return &fakeDashboardRows{}, nil
		},
	}}

	_, err := repo.GetDashboardOverview(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetDashboardOverview() error = %v", err)
	}

	for _, want := range []string{
		"from nodes",
		"from targets",
		"current_health_status <> '正常'",
		"when '严重' then 3",
		"current_active_incident_count desc",
	} {
		if !containsSQL(capturedSQL, want) {
			t.Fatalf("capturedSQL = %#v, want %q", capturedSQL, want)
		}
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

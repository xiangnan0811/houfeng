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
			if strings.Contains(sql, "from center_settings") {
				return fakeRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					*(dest[1].(*bool)) = true
					*(dest[2].(*bool)) = true
					*(dest[3].(*bool)) = false
					return nil
				}}
			}
			return fakeRow{scan: func(dest ...any) error {
				*(dest[0].(*int)) = 5
				*(dest[1].(*int)) = 4
				*(dest[2].(*int)) = 2
				*(dest[3].(*int)) = 1
				*(dest[4].(*int)) = 1
				*(dest[5].(*int)) = 0
				*(dest[6].(*int)) = 1
				*(dest[7].(*int)) = 1
				*(dest[8].(*int)) = 2
				*(dest[9].(*int)) = 1
				*(dest[10].(*int)) = 1
				*(dest[11].(*int)) = 1
				*(dest[12].(*int)) = 1
				*(dest[13].(*int)) = 3
				*(dest[14].(*int)) = 2
				return nil
			}}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			switch {
			case strings.Contains(sql, "node_groups"):
				return &fakeDashboardRows{rows: []fakeDashboardScan{{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "production"
					*(dest[1].(*int)) = 3
					*(dest[2].(*int)) = 2
					*(dest[3].(*int)) = 1
					*(dest[4].(*int)) = 1
					*(dest[5].(*int)) = 1
					*(dest[6].(*int)) = 0
					*(dest[7].(*int)) = 1
					*(dest[8].(*int)) = 1
					return nil
				}}}}, nil
			case strings.Contains(sql, "hour_buckets"):
				// 24-bucket trend query for the dashboard sparkline.
				// Each bucket returns (started, recovered) ints.
				rows := make([]fakeDashboardScan, 0, 24)
				for i := 0; i < 24; i++ {
					started, recovered := i%5, i%3
					rows = append(rows, fakeDashboardScan{scan: func(s, r int) func(dest ...any) error {
						return func(dest ...any) error {
							*(dest[0].(*int)) = s
							*(dest[1].(*int)) = r
							return nil
						}
					}(started, recovered)})
				}
				return &fakeDashboardRows{rows: rows}, nil
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
					*(dest[2].(*string)) = "production"
					*(dest[3].(*string)) = "ap-northeast-1"
					*(dest[4].(*string)) = "Tokyo"
					*(dest[5].(*string)) = "aws"
					*(dest[6].(*string)) = "在用"
					*(dest[7].(*string)) = "启用"
					*(dest[8].(*string)) = string(incidents.SeverityAlert)
					*(dest[9].(**time.Time)) = &lastHeartbeat
					*(dest[10].(*int)) = 2
					*(dest[11].(*string)) = "磁盘使用率 92.0%"
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
					*(dest[6].(*string)) = "production"
					*(dest[7].(*string)) = string(incidents.SeverityCritical)
					*(dest[8].(**time.Time)) = &lastSuccess
					*(dest[9].(**time.Time)) = &lastFailure
					*(dest[10].(*int)) = 1
					*(dest[11].(*string)) = "HTTPS 探测连续失败"
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
	if overview.SnapshotGeneratedAt.IsZero() {
		t.Fatal("SnapshotGeneratedAt is zero, want dashboard generation time")
	}
	if overview.PendingOnboardingNodeCount != 2 || overview.PausedNodeCount != 1 || overview.RetiredNodeCount != 1 {
		t.Fatalf("node completeness counts = (%d,%d,%d), want (2,1,1)", overview.PendingOnboardingNodeCount, overview.PausedNodeCount, overview.RetiredNodeCount)
	}
	if overview.PausedTargetCount != 1 || overview.ArchivedTargetCount != 1 {
		t.Fatalf("target completeness counts = (%d,%d), want (1,1)", overview.PausedTargetCount, overview.ArchivedTargetCount)
	}
	if len(overview.GroupSummaries) != 1 || overview.GroupSummaries[0].Group != "production" {
		t.Fatalf("GroupSummaries = %#v, want production group summary", overview.GroupSummaries)
	}
	if overview.GroupSummaries[0].NodeCount != 3 || overview.GroupSummaries[0].TargetCount != 2 {
		t.Fatalf("GroupSummaries[0] = %#v, want full node/target counts", overview.GroupSummaries[0])
	}
	if !overview.NotificationStatus.TelegramConfigured || !overview.NotificationStatus.TelegramRuntimeManaged || !overview.NotificationStatus.TelegramRuntimeApplyActive {
		t.Fatalf("NotificationStatus = %#v, want configured runtime-managed telegram", overview.NotificationStatus)
	}
	if overview.NotificationStatus.FeishuConfigured {
		t.Fatalf("NotificationStatus = %#v, want feishu unconfigured", overview.NotificationStatus)
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
	if len(overview.NewIncidentTrend24h) != 24 || len(overview.RecoveryTrend24h) != 24 {
		t.Fatalf("trend lens = (%d,%d), want (24,24)", len(overview.NewIncidentTrend24h), len(overview.RecoveryTrend24h))
	}
	// Spot-check the synthetic per-bucket pattern from the fake (i%5, i%3).
	if overview.NewIncidentTrend24h[5] != 0 || overview.RecoveryTrend24h[5] != 2 {
		t.Fatalf("trend[5] = (%d,%d), want (0,2)", overview.NewIncidentTrend24h[5], overview.RecoveryTrend24h[5])
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

func TestPostgresDashboardRepositoryBuildsFullGroupSummaryQuery(t *testing.T) {
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

	groupSQL := firstSQLContaining(capturedSQL, "node_groups")
	if groupSQL == "" {
		t.Fatalf("capturedSQL = %#v, want full group summary query", capturedSQL)
	}
	for _, want := range []string{
		"from nodes",
		"from targets",
		"full outer join target_groups",
		`coalesce(nullif(btrim("group"), ''), '未分组')`,
		"count(*) filter (where current_health_status <> '正常')",
		"order by",
	} {
		if !strings.Contains(groupSQL, want) {
			t.Fatalf("groupSQL = %q, want %q", groupSQL, want)
		}
	}
	if strings.Contains(groupSQL, "limit $1") {
		t.Fatalf("groupSQL = %q, want group summaries unaffected by dashboard limit", groupSQL)
	}
}

func TestLoadDashboardNotificationStatus(t *testing.T) {
	t.Run("missing settings row returns false status", func(t *testing.T) {
		status, err := loadDashboardNotificationStatus(context.Background(), fakeDashboardQueryer{
			queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
				return fakeRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			},
			query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
				t.Fatal("Query() should not be called")
				return nil, nil
			},
		})
		if err != nil {
			t.Fatalf("loadDashboardNotificationStatus() error = %v", err)
		}
		if status != (incidents.DashboardNotificationStatus{}) {
			t.Fatalf("status = %#v, want zero false status", status)
		}
	})

	t.Run("configured settings row computes booleans only", func(t *testing.T) {
		status, err := loadDashboardNotificationStatus(context.Background(), fakeDashboardQueryer{
			queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if !strings.Contains(sql, "btrim(telegram_bot_token)") || !strings.Contains(sql, "btrim(feishu_webhook_url)") {
					t.Fatalf("sql = %q, want trimmed notification configuration checks", sql)
				}
				if strings.Contains(sql, "telegram_bot_token,") || strings.Contains(sql, "telegram_chat_id,") || strings.Contains(sql, "feishu_webhook_url,") {
					t.Fatalf("sql = %q, want booleans only without selecting secret values", sql)
				}
				return fakeRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					*(dest[1].(*bool)) = true
					*(dest[2].(*bool)) = true
					*(dest[3].(*bool)) = true
					return nil
				}}
			},
			query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
				t.Fatal("Query() should not be called")
				return nil, nil
			},
		})
		if err != nil {
			t.Fatalf("loadDashboardNotificationStatus() error = %v", err)
		}
		if !status.TelegramConfigured || !status.TelegramRuntimeManaged || !status.TelegramRuntimeApplyActive || !status.FeishuConfigured {
			t.Fatalf("status = %#v, want all configured booleans", status)
		}
	})
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

func TestPostgresDashboardRepositoryListEventsBuildsAdvancedContextFilters(t *testing.T) {
	from := time.Date(2026, time.April, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.April, 26, 0, 0, 0, 0, time.UTC)
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
		CreatedFrom:      &from,
		CreatedTo:        &to,
		Label:            "edge",
		NotificationOnly: true,
		Limit:            20,
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, want := range []string{
		"e.created_at >= $1",
		"e.created_at <= $2",
		"n.labels @> array[$3]::text[]",
		"t.labels @> array[$3]::text[]",
		"from notification_records nr",
		"nr.incident_id = e.payload ->> 'incident_id'",
	} {
		if !containsSQL([]string{capturedSQL}, want) {
			t.Fatalf("capturedSQL = %q, want %q", capturedSQL, want)
		}
	}
}

func TestPostgresDashboardRepositoryListEventsBuildsShortcutFilters(t *testing.T) {
	tests := []struct {
		name   string
		filter EventsFilter
		want   string
	}{
		{
			name:   "recovery only",
			filter: EventsFilter{RecoveryOnly: true},
			want:   "e.event_type = 'incident_recovered'",
		},
		{
			name:   "maintenance only",
			filter: EventsFilter{MaintenanceOnly: true},
			want:   "e.event_type in ('node_monitoring_maintenance_entered'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			_, err := repo.ListEvents(context.Background(), tt.filter)
			if err != nil {
				t.Fatalf("ListEvents() error = %v", err)
			}
			if !containsSQL([]string{capturedSQL}, tt.want) {
				t.Fatalf("capturedSQL = %q, want %q", capturedSQL, tt.want)
			}
		})
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

func firstSQLContaining(sqls []string, want string) string {
	for _, sql := range sqls {
		if strings.Contains(sql, want) {
			return sql
		}
	}
	return ""
}

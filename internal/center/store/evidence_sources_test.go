package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
)

func TestMonitoringEvidenceQueriesUseAbsoluteWindowsWithoutSparklineSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
	}{
		{name: "host raw", sql: monitoringEvidenceHostRawSQL},
		{name: "host daily", sql: monitoringEvidenceHostDailySQL},
		{name: "probe raw", sql: monitoringEvidenceProbeRawSQL},
		{name: "probe daily", sql: monitoringEvidenceProbeDailySQL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := strings.ToLower(tt.sql)
			if !strings.Contains(normalized, ">= $2") || !strings.Contains(normalized, "< $3") {
				t.Fatalf("evidence SQL = %q, want exact half-open observed window", tt.sql)
			}
			if strings.Contains(normalized, " limit ") || strings.Contains(normalized, "generate_series") {
				t.Fatalf("evidence SQL = %q, row-count truncation/zero-fill is forbidden", tt.sql)
			}
		})
	}

	for _, query := range []string{monitoringEvidenceHostDailySQL, monitoringEvidenceProbeDailySQL} {
		normalized := strings.ToLower(query)
		if !strings.Contains(normalized, "at time zone 'utc') >= $2") ||
			!strings.Contains(normalized, "at time zone 'utc') <= $3") {
			t.Fatalf("daily evidence SQL = %q, want only fully contained UTC aggregate days", query)
		}
	}
	if !strings.Contains(strings.ToLower(monitoringEvidenceProbeRawSQL), "having metric.name = 'success_ratio' or count(metric.value) > 0") {
		t.Fatalf("probe raw evidence SQL = %q, want empty optional metric rows omitted", monitoringEvidenceProbeRawSQL)
	}
	if !strings.Contains(strings.ToLower(monitoringEvidenceProbeDailySQL), "coalesce(metric.average, metric.minimum, metric.maximum, metric.p95) is not null") {
		t.Fatalf("probe daily evidence SQL = %q, want empty metric rows omitted", monitoringEvidenceProbeDailySQL)
	}
	for _, required := range []string{"disk_used_pct", "inode_used_pct", "net_in_bytes_per_sec", "disk_read_bytes_per_sec"} {
		if !strings.Contains(monitoringEvidenceHostRawSQL, required) {
			t.Fatalf("host evidence SQL omits required metric %q", required)
		}
	}
	for _, required := range []string{"http_status", "tls_expiry_days"} {
		if !strings.Contains(monitoringEvidenceProbeRawSQL, required) {
			t.Fatalf("probe evidence SQL omits required metric %q", required)
		}
	}

	if strings.Contains(strings.ToLower(getMonitoringInstanceSparklinesSQL), "< $2") {
		t.Fatalf("legacy sparkline unexpectedly gained an absolute upper bound; RED fixture no longer demonstrates the evidence distinction")
	}
	if !strings.Contains(strings.ToLower(getTargetSparklinesSQL), "latency_ms is not null") {
		t.Fatalf("legacy target sparkline fixture no longer demonstrates discarded failed observations")
	}
}

func TestIPQualityEvidenceQueryAllowlistExcludesRetentionOnlyJSON(t *testing.T) {
	t.Parallel()

	queries := []string{ipQualityEvidenceReportSQL, ipQualityEvidenceProvidersSQL, ipQualityEvidenceServicesSQL}
	normalized := strings.ToLower(strings.Join(queries, "\n"))
	for _, required := range []string{"ip_quality_assigned_vps_reports", "observed_at >= $2", "observed_at < $3", "stale_after_seconds"} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("ipQualityEvidenceReportSQL = %q, want %q", ipQualityEvidenceReportSQL, required)
		}
	}
	for _, forbidden := range []string{"raw_json", "diagnostics_json", "extra_json", "fingerprint", "sync_batch_id"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("ipQualityEvidenceReportSQL contains retention-only/diagnostic field %q", forbidden)
		}
	}
	if !strings.Contains(strings.ToLower(ipQualityEvidenceProvidersSQL), "status = 'success'") ||
		!strings.Contains(strings.ToLower(ipQualityEvidenceServicesSQL), "probe_status = 'success'") {
		t.Fatalf("IP quality evidence matrices must preserve only authoritative successful risk/unlock facts")
	}
}

func TestMonitoringEvidenceMergeUsesDailyAggregateForPartialRawRetentionDay(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	raw := []evidenceMetricRow{{
		SeriesID: "probe-a", BucketStart: day.Add(12 * time.Hour), BucketEnd: day.Add(13 * time.Hour),
		SourceLayer: "raw", SampleCount: 100, Metric: "success_ratio",
	}}
	daily := []evidenceMetricRow{{
		SeriesID: "probe-a", BucketStart: day, BucketEnd: day.Add(24 * time.Hour),
		SourceLayer: "daily_aggregate", SampleCount: 288, Metric: "success_ratio",
	}}
	if !uncoveredDaily(raw, daily) {
		t.Fatal("uncoveredDaily() = false, want partial raw retention day to require aggregate fallback")
	}
	merged := mergeEvidenceMetricRows(raw, daily, 24*time.Hour)
	if len(merged) != 1 || merged[0].SourceLayer != "daily_aggregate" || merged[0].SampleCount != 288 {
		t.Fatalf("mergeEvidenceMetricRows() = %#v, want complete daily aggregate", merged)
	}
}

func TestLoadMonitoringHostEvidenceScansNormalizedRowsAndProvenance(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	average, maximum := 25.0, 40.0
	row := evidenceMetricRow{
		SeriesKind: "host", BucketStart: start, BucketEnd: end, SourceLayer: "raw", SourceGranularity: 300,
		SampleCount: 12, MaintenanceCount: 2, BackfilledCount: 1, Metric: "cpu_usage_pct", Unit: "percent",
		Average: &average, Maximum: &maximum, ObservedStart: start.Add(5 * time.Minute), ObservedEnd: end.Add(-5 * time.Minute),
		Watermark: end, ProducerVersions: []string{"agent/v1"},
	}
	db := &evidenceSourceQueryDB{queries: []evidenceRows{
		newEvidenceRows(func(dest ...any) error { return scanEvidenceMetricRow(dest, row) }),
		newEvidenceRows(),
	}}
	repository := &PostgresRuntimeFactsRepository{db: db}
	capture, err := repository.LoadMonitoringHostEvidence(context.Background(), "mi_0123456789abcdef", evidence.TimeWindow{Start: start, End: end}, time.Hour, []string{"cpu_usage_pct"})
	if err != nil {
		t.Fatalf("LoadMonitoringHostEvidence() error = %v", err)
	}
	if len(capture.Buckets) != 1 || capture.Buckets[0].SampleCount != 12 || capture.Buckets[0].MaintenanceCount != 2 || capture.Buckets[0].BackfilledCount != 1 {
		t.Fatalf("capture buckets = %#v, want normalized counts", capture.Buckets)
	}
	if capture.Buckets[0].SourceLayer != adapters.MonitoringSourceRaw || capture.Buckets[0].SourceGranularity != 5*time.Minute || capture.ProducerVersion != "agent/v1" {
		t.Fatalf("capture provenance = %#v, want raw/5m/agent-v1", capture)
	}
	if capture.CoverageStart != row.ObservedStart || capture.CoverageEnd != row.ObservedEnd.Add(time.Microsecond) {
		t.Fatalf("capture coverage = %#v, want observed coverage", capture)
	}
}

func TestLoadIPQualityEvidenceDecodesAllowlistedReportAndMatrices(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	lat, lon := 35.0, 139.0
	latency := 25
	proxy := true
	db := &evidenceSourceQueryDB{queries: []evidenceRows{
		newEvidenceRows(func(dest ...any) error {
			if len(dest) != 21 {
				return errors.New("unexpected report scan destination count")
			}
			*(dest[0].(*string)) = "ipq_0123456789abcdef"
			*(dest[1].(*time.Time)) = now.Add(-time.Hour)
			*(dest[2].(*time.Time)) = now.Add(-time.Hour + time.Minute)
			*(dest[3].(*string)) = "agent/v1"
			*(dest[4].(*string)) = "203.0.113.10"
			*(dest[5].(*int)) = 4
			*(dest[6].(*string)) = "partial"
			*(dest[7].(*string)) = "AS64500"
			*(dest[8].(*string)) = "Example Transit"
			*(dest[9].(**float64)) = &lat
			*(dest[10].(**float64)) = &lon
			*(dest[11].(*string)) = "JP"
			*(dest[12].(*string)) = "Tokyo"
			*(dest[13].(*string)) = "US"
			*(dest[14].(*string)) = "California"
			*(dest[15].(*string)) = "medium"
			*(dest[16].(*bool)) = true
			*(dest[17].(*bool)) = true
			*(dest[18].(*string)) = "link"
			*(dest[19].(*[]byte)) = []byte(`{"expected_provider_count":2,"successful_provider_count":1,"failed_provider_count":1,"expected_service_count":1,"successful_service_count":0,"failed_service_count":1}`)
			*(dest[20].(*int64)) = 3600
			return nil
		}),
		newEvidenceRows(func(dest ...any) error {
			if len(dest) != 17 {
				return errors.New("unexpected provider scan destination count")
			}
			*(dest[0].(*string)) = "ipapi.is"
			*(dest[1].(*string)) = "success"
			*(dest[2].(*string)) = "default"
			*(dest[3].(**int)) = &latency
			*(dest[4].(*string)) = "isp"
			*(dest[5].(*string)) = "business"
			*(dest[6].(*string)) = "medium"
			*(dest[7].(*string)) = "50"
			*(dest[8].(*string)) = "JP"
			*(dest[9].(*string)) = "Tokyo"
			*(dest[10].(**bool)) = &proxy
			*(dest[16].(*string)) = ""
			return nil
		}),
		newEvidenceRows(func(dest ...any) error {
			if len(dest) != 8 {
				return errors.New("unexpected service scan destination count")
			}
			*(dest[0].(*string)) = "netflix"
			*(dest[1].(*string)) = "builtin"
			*(dest[2].(*string)) = "unknown"
			*(dest[3].(*string)) = "failure"
			*(dest[4].(**int)) = nil
			*(dest[5].(*string)) = ""
			*(dest[6].(*string)) = ""
			*(dest[7].(*string)) = "probe_timeout"
			return nil
		}),
	}}
	repository := &PostgresIPQualityRepository{db: db}
	report, err := repository.LoadIPQualityEvidence(context.Background(), "vps_0123456789abcdef", evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now})
	if err != nil {
		t.Fatalf("LoadIPQualityEvidence() error = %v", err)
	}
	if report.Status != "partial" || report.StaleAfter != time.Hour || !report.IsBackfilled || !report.Ambiguous || report.Coverage.ExpectedProviderCount != 2 {
		t.Fatalf("report = %#v, want normalized status/stale/coverage", report)
	}
	if len(report.Providers) != 1 || report.Providers[0].Provider != "ipapi.is" || report.Providers[0].IsProxy == nil || !*report.Providers[0].IsProxy {
		t.Fatalf("providers = %#v, want allowlisted provider facts", report.Providers)
	}
	if len(report.Services) != 1 || report.Services[0].ProbeStatus != "failure" || report.Services[0].UnlockType != "" || report.Services[0].ErrorCode != "probe_timeout" {
		t.Fatalf("services = %#v, want failed probe without unlock fact", report.Services)
	}
}

type evidenceRows interface {
	pgx.Rows
}

type evidenceSourceQueryDB struct {
	queries []evidenceRows
	index   int
}

func (db *evidenceSourceQueryDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if db.index >= len(db.queries) {
		return newEvidenceRows(), nil
	}
	rows := db.queries[db.index]
	db.index++
	return rows, nil
}

func (db *evidenceSourceQueryDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return evidenceSourceRow{err: pgx.ErrNoRows}
}

type evidenceSourceRow struct {
	err error
}

func (row evidenceSourceRow) Scan(...any) error { return row.err }

type evidenceRowsFixture struct {
	scans []func(...any) error
	index int
}

func newEvidenceRows(scans ...func(...any) error) *evidenceRowsFixture {
	return &evidenceRowsFixture{scans: scans}
}

func (rows *evidenceRowsFixture) Close()                                       {}
func (rows *evidenceRowsFixture) Err() error                                   { return nil }
func (rows *evidenceRowsFixture) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *evidenceRowsFixture) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *evidenceRowsFixture) RawValues() [][]byte                          { return nil }
func (rows *evidenceRowsFixture) Values() ([]any, error)                       { return nil, nil }
func (rows *evidenceRowsFixture) Conn() *pgx.Conn                              { return nil }
func (rows *evidenceRowsFixture) Next() bool {
	if rows.index >= len(rows.scans) {
		return false
	}
	rows.index++
	return true
}
func (rows *evidenceRowsFixture) Scan(dest ...any) error {
	return rows.scans[rows.index-1](dest...)
}

func scanEvidenceMetricRow(dest []any, row evidenceMetricRow) error {
	if len(dest) != 19 {
		return errors.New("unexpected monitoring scan destination count")
	}
	*(dest[0].(*string)) = row.SeriesID
	*(dest[1].(*string)) = row.SeriesKind
	*(dest[2].(*time.Time)) = row.BucketStart
	*(dest[3].(*time.Time)) = row.BucketEnd
	*(dest[4].(*string)) = row.SourceLayer
	*(dest[5].(*int64)) = row.SourceGranularity
	*(dest[6].(*int64)) = row.SampleCount
	*(dest[7].(*int64)) = row.MaintenanceCount
	*(dest[8].(*int64)) = row.BackfilledCount
	*(dest[9].(*string)) = row.Metric
	*(dest[10].(*string)) = row.Unit
	*(dest[11].(**float64)) = row.Average
	*(dest[12].(**float64)) = row.Minimum
	*(dest[13].(**float64)) = row.Maximum
	*(dest[14].(**float64)) = row.P95
	*(dest[15].(*time.Time)) = row.ObservedStart
	*(dest[16].(*time.Time)) = row.ObservedEnd
	*(dest[17].(*time.Time)) = row.Watermark
	*(dest[18].(*[]string)) = append([]string(nil), row.ProducerVersions...)
	return nil
}

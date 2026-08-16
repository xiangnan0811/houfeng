package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
)

const monitoringEvidenceHostRawSQL = `
	select
		''::text as series_id,
		'host'::text as series_kind,
		bucket_start,
		bucket_start + ($5::double precision * interval '1 second') as bucket_end,
		'raw'::text as source_layer,
		$5::bigint as source_granularity_seconds,
		count(*)::bigint as sample_count,
		count(*) filter (where h.maintenance_context)::bigint as maintenance_count,
		count(*) filter (where h.is_backfilled)::bigint as backfilled_count,
		metric.name,
		metric.unit,
		avg(metric.value)::double precision,
		min(metric.value)::double precision,
		max(metric.value)::double precision,
		percentile_cont(0.95) within group (order by metric.value),
		min(h.observed_at),
		max(h.observed_at),
		max(h.received_at),
		array_agg(distinct h.agent_version order by h.agent_version)
	from host_samples h
	cross join lateral (
		select case when $5 >= 86400 then date_trunc('day', h.observed_at at time zone 'UTC') at time zone 'UTC'
			else $2::timestamptz + floor(extract(epoch from (h.observed_at - $2::timestamptz)) / $5::double precision) * ($5::double precision * interval '1 second') end as bucket_start
	) bucketed
	cross join lateral (values
		('cpu_usage_pct'::text, 'percent'::text, h.cpu_usage_pct::double precision),
		('load_1'::text, 'load'::text, h.load_1::double precision),
		('load_5'::text, 'load'::text, h.load_5::double precision),
		('load_15'::text, 'load'::text, h.load_15::double precision),
		('mem_used_pct'::text, 'percent'::text, h.mem_used_pct::double precision),
		('mem_available_bytes'::text, 'bytes'::text, h.mem_available_bytes::double precision),
		('mem_total_bytes'::text, 'bytes'::text, h.mem_total_bytes::double precision),
		('swap_used_pct'::text, 'percent'::text, h.swap_used_pct::double precision),
		('disk_used_pct'::text, 'percent'::text, h.disk_used_pct::double precision),
		('disk_total_bytes'::text, 'bytes'::text, h.disk_total_bytes::double precision),
		('inode_used_pct'::text, 'percent'::text, h.inode_used_pct::double precision),
		('net_in_bytes_per_sec'::text, 'bytes_per_second'::text, h.net_in_bytes_per_sec::double precision),
		('net_out_bytes_per_sec'::text, 'bytes_per_second'::text, h.net_out_bytes_per_sec::double precision),
		('cpu_iowait_pct'::text, 'percent'::text, h.cpu_iowait_pct::double precision),
		('cpu_steal_pct'::text, 'percent'::text, h.cpu_steal_pct::double precision),
		('disk_read_bytes_per_sec'::text, 'bytes_per_second'::text, h.disk_read_bytes_per_sec::double precision),
		('disk_write_bytes_per_sec'::text, 'bytes_per_second'::text, h.disk_write_bytes_per_sec::double precision),
		('disk_busy_pct'::text, 'percent'::text, h.disk_busy_pct::double precision),
		('uptime_seconds'::text, 'seconds'::text, h.uptime_seconds::double precision)
		) metric(name, unit, value)
	where h.monitoring_instance_id = $1
		and h.observed_at >= $2
		and h.observed_at < $3
		and metric.name = any($4::text[])
	group by bucket_start, metric.name, metric.unit
	order by bucket_start, metric.name`

const monitoringEvidenceHostDailySQL = `
	select
		''::text as series_id,
		'host'::text as series_kind,
			(h.bucket_date::timestamp at time zone 'UTC') as bucket_start,
			((h.bucket_date + 1)::timestamp at time zone 'UTC') as bucket_end,
		'daily_aggregate'::text as source_layer,
		86400::bigint as source_granularity_seconds,
		case metric.name
			when 'cpu_usage_pct' then sample_count
			when 'load_5' then sample_count
			when 'mem_used_pct' then sample_count
			when 'cpu_iowait_pct' then sample_count
			when 'cpu_steal_pct' then sample_count
			when 'disk_busy_pct' then sample_count
		end::bigint as sample_count,
		maintenance_sample_count::bigint,
		backfilled_sample_count::bigint,
		metric.name,
		metric.unit,
		metric.average,
		null::double precision,
		metric.maximum,
		null::double precision,
			(h.bucket_date::timestamp at time zone 'UTC'),
			((h.bucket_date + 1)::timestamp at time zone 'UTC') - interval '1 microsecond',
		updated_at,
		array['retention-aggregate/v1']::text[]
	from monitoring_instance_host_sample_daily_aggregates h
	cross join lateral (values
		('cpu_usage_pct'::text, 'percent'::text, h.avg_cpu_usage_pct::double precision, h.max_cpu_usage_pct::double precision),
		('load_5'::text, 'load'::text, h.avg_load_5::double precision, h.max_load_5::double precision),
		('mem_used_pct'::text, 'percent'::text, h.avg_mem_used_pct::double precision, h.max_mem_used_pct::double precision),
		('cpu_iowait_pct'::text, 'percent'::text, h.avg_cpu_iowait_pct::double precision, h.max_cpu_iowait_pct::double precision),
		('cpu_steal_pct'::text, 'percent'::text, h.avg_cpu_steal_pct::double precision, h.max_cpu_steal_pct::double precision),
		('disk_busy_pct'::text, 'percent'::text, h.avg_disk_busy_pct::double precision, h.max_disk_busy_pct::double precision)
		) metric(name, unit, average, maximum)
		where h.monitoring_instance_id = $1
			and (h.bucket_date::timestamp at time zone 'UTC') >= $2
			and (h.bucket_date::timestamp at time zone 'UTC') < $3
			and ((h.bucket_date + 1)::timestamp at time zone 'UTC') <= $3
		and metric.name = any($4::text[])
	order by bucket_date, metric.name`

const monitoringEvidenceProbeRawSQL = `
	select
		po.probe_item_id as series_id,
		pi.probe_kind as series_kind,
		bucket_start,
		bucket_start + ($5::double precision * interval '1 second') as bucket_end,
		'raw'::text as source_layer,
		$5::bigint as source_granularity_seconds,
		count(*)::bigint as sample_count,
		count(*) filter (where po.maintenance_context)::bigint as maintenance_count,
		count(*) filter (where po.is_backfilled)::bigint as backfilled_count,
		metric.name,
		metric.unit,
		case when metric.name = 'http_status' then null else avg(metric.value)::double precision end,
		min(metric.value)::double precision,
		max(metric.value)::double precision,
		case when metric.name = 'http_status' then null else percentile_cont(0.95) within group (order by metric.value) end,
		min(po.observed_at),
		max(po.observed_at),
		max(po.received_at),
		array_agg(distinct po.agent_version order by po.agent_version)
	from probe_observations po
	join probe_items pi on pi.probe_item_id = po.probe_item_id
	cross join lateral (
		select case when $5 >= 86400 then date_trunc('day', po.observed_at at time zone 'UTC') at time zone 'UTC'
			else $2::timestamptz + floor(extract(epoch from (po.observed_at - $2::timestamptz)) / $5::double precision) * ($5::double precision * interval '1 second') end as bucket_start
	) bucketed
	cross join lateral (values
		('latency_ms'::text, 'ms'::text, po.latency_ms::double precision),
		('success_ratio'::text, 'ratio'::text, case when po.result_kind = 'success' then 1.0 else 0.0 end),
		('http_status'::text, 'status_code'::text, po.http_status::double precision),
		('tls_expiry_days'::text, 'days'::text, po.tls_expiry_days::double precision)
		) metric(name, unit, value)
	where po.target_id = $1
		and po.observed_at >= $2
		and po.observed_at < $3
		and metric.name = any($4::text[])
	group by po.probe_item_id, pi.probe_kind, bucket_start, metric.name, metric.unit
	having metric.name = 'success_ratio' or count(metric.value) > 0
	order by po.probe_item_id, bucket_start, metric.name`

const monitoringEvidenceProbeDailySQL = `
	select
		p.probe_item_id as series_id,
		p.probe_kind as series_kind,
			(p.bucket_date::timestamp at time zone 'UTC') as bucket_start,
			((p.bucket_date + 1)::timestamp at time zone 'UTC') as bucket_end,
		'daily_aggregate'::text as source_layer,
		86400::bigint as source_granularity_seconds,
		p.observation_count::bigint,
		p.maintenance_observation_count::bigint,
		p.backfilled_observation_count::bigint,
		metric.name,
		metric.unit,
		metric.average,
		metric.minimum,
		metric.maximum,
		metric.p95,
			(p.bucket_date::timestamp at time zone 'UTC'),
			((p.bucket_date + 1)::timestamp at time zone 'UTC') - interval '1 microsecond',
		p.updated_at,
		array['retention-aggregate/v1']::text[]
	from (
		select a.*, pi.probe_kind
		from target_probe_daily_aggregates a
		join probe_items pi on pi.probe_item_id = a.probe_item_id
			where a.target_id = $1
				and (a.bucket_date::timestamp at time zone 'UTC') >= $2
				and (a.bucket_date::timestamp at time zone 'UTC') < $3
				and ((a.bucket_date + 1)::timestamp at time zone 'UTC') <= $3
	) p
	cross join lateral (values
		('latency_ms'::text, 'ms'::text, p.avg_latency_ms::double precision, null::double precision, null::double precision, p.p95_latency_ms::double precision),
		('success_ratio'::text, 'ratio'::text, case when p.observation_count > 0 then p.success_count::double precision / p.observation_count else null end, null::double precision, null::double precision, null::double precision),
		('tls_expiry_days'::text, 'days'::text, null::double precision, p.min_tls_expiry_days::double precision, null::double precision, null::double precision)
	) metric(name, unit, average, minimum, maximum, p95)
	where metric.name = any($4::text[])
		and coalesce(metric.average, metric.minimum, metric.maximum, metric.p95) is not null
	order by p.probe_item_id, p.bucket_date, metric.name`

const ipQualityEvidenceReportSQL = `
	with ip_quality_stale_settings as (
		select case
			when coalesce(ip_quality_settings->>'stale_after_seconds', '') ~ '^[0-9]+$'
			then greatest((ip_quality_settings->>'stale_after_seconds')::integer, 60)
			else 604800
		end as stale_after_seconds
		from center_settings
		where settings_id = 'center'
	)
	select
		r.report_id,
		r.observed_at,
		r.received_at,
		r.agent_version,
		r.ip_address,
		r.ip_version,
		r.status,
		r.asn,
		r.organization,
		r.latitude,
		r.longitude,
		r.use_region_code,
		r.use_region_name,
		r.registered_region_code,
		r.registered_region_name,
		r.risk_level,
		r.is_backfilled,
		assigned.ambiguous,
		assigned.assignment_mode,
		r.coverage_json,
		coalesce((select stale_after_seconds from ip_quality_stale_settings), 604800) as stale_after_seconds
	from ip_quality_assigned_vps_reports assigned
	join ip_quality_reports r on r.report_id = assigned.report_id
	where assigned.vps_id = $1
		and r.observed_at >= $2
		and r.observed_at < $3
	order by r.observed_at desc, r.report_id desc
	limit 1`

const ipQualityEvidenceProvidersSQL = `
	select
		provider,
		status,
		source_type,
		latency_ms,
		usage_type,
		company_type,
		case when status = 'success' then risk_level else '' end,
		case when status = 'success' then risk_score else '' end,
		case when status = 'success' then region_code else '' end,
		case when status = 'success' then region_name else '' end,
		case when status = 'success' then is_proxy end,
		case when status = 'success' then is_tor end,
		case when status = 'success' then is_vpn end,
		case when status = 'success' then is_server end,
		case when status = 'success' then is_abuser end,
		case when status = 'success' then is_robot end,
		error_code
	from ip_quality_provider_results
	where report_id = $1
	order by provider`

const ipQualityEvidenceServicesSQL = `
	select
		service,
		source,
		status,
		probe_status,
		latency_ms,
		case when probe_status = 'success' then region else '' end,
		case when probe_status = 'success' then unlock_type else '' end,
		error_code
	from ip_quality_service_unlocks
	where report_id = $1
	order by service, source`

type monitoringEvidenceRows interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type evidenceMetricRow struct {
	SeriesID, SeriesKind                           string
	BucketStart, BucketEnd                         time.Time
	SourceLayer                                    string
	SourceGranularity                              int64
	SampleCount, MaintenanceCount, BackfilledCount int64
	Metric, Unit                                   string
	Average, Minimum, Maximum, P95                 *float64
	ObservedStart, ObservedEnd, Watermark          time.Time
	ProducerVersions                               []string
}

func (r *PostgresRuntimeFactsRepository) LoadMonitoringHostEvidence(
	ctx context.Context, sourceID string, window evidence.TimeWindow, precision time.Duration, metrics []string,
) (adapters.MonitoringSeriesCapture, error) {
	return r.loadMonitoringEvidence(ctx, true, sourceID, window, precision, metrics)
}

func (r *PostgresRuntimeFactsRepository) LoadMonitoringProbeEvidence(
	ctx context.Context, sourceID string, window evidence.TimeWindow, precision time.Duration, metrics []string,
) (adapters.MonitoringSeriesCapture, error) {
	return r.loadMonitoringEvidence(ctx, false, sourceID, window, precision, metrics)
}

func (r *PostgresRuntimeFactsRepository) loadMonitoringEvidence(
	ctx context.Context, host bool, sourceID string, window evidence.TimeWindow, precision time.Duration, metrics []string,
) (adapters.MonitoringSeriesCapture, error) {
	if r == nil || r.db == nil {
		return adapters.MonitoringSeriesCapture{}, fmt.Errorf("monitoring evidence source unavailable")
	}
	rawSQL, dailySQL := monitoringEvidenceProbeRawSQL, monitoringEvidenceProbeDailySQL
	if host {
		rawSQL, dailySQL = monitoringEvidenceHostRawSQL, monitoringEvidenceHostDailySQL
	}
	raw, err := queryEvidenceMetricRows(ctx, r.db, rawSQL, sourceID, window, precision, metrics)
	if err != nil {
		return adapters.MonitoringSeriesCapture{}, err
	}
	daily, err := queryEvidenceMetricRows(ctx, r.db, dailySQL, sourceID, window, precision, metrics)
	if err != nil {
		return adapters.MonitoringSeriesCapture{}, err
	}
	actualPrecision := precision
	if uncoveredDaily(raw, daily) {
		actualPrecision = 24 * time.Hour
		raw, err = queryEvidenceMetricRows(ctx, r.db, rawSQL, sourceID, window, actualPrecision, metrics)
		if err != nil {
			return adapters.MonitoringSeriesCapture{}, err
		}
	}
	rows := mergeEvidenceMetricRows(raw, daily, actualPrecision)
	return evidenceCaptureFromRows(window, actualPrecision, rows), nil
}

func queryEvidenceMetricRows(ctx context.Context, db monitoringEvidenceRows, sql string, sourceID string, window evidence.TimeWindow, precision time.Duration, metrics []string) ([]evidenceMetricRow, error) {
	args := []any{sourceID, window.Start, window.End, metrics}
	if strings.Contains(sql, "$5") {
		args = append(args, int64(precision/time.Second))
	}
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query monitoring evidence rows: %w", err)
	}
	defer rows.Close()
	out := make([]evidenceMetricRow, 0)
	for rows.Next() {
		var row evidenceMetricRow
		if err := rows.Scan(
			&row.SeriesID, &row.SeriesKind, &row.BucketStart, &row.BucketEnd, &row.SourceLayer, &row.SourceGranularity,
			&row.SampleCount, &row.MaintenanceCount, &row.BackfilledCount, &row.Metric, &row.Unit,
			&row.Average, &row.Minimum, &row.Maximum, &row.P95, &row.ObservedStart, &row.ObservedEnd, &row.Watermark, &row.ProducerVersions,
		); err != nil {
			return nil, fmt.Errorf("scan monitoring evidence row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring evidence rows: %w", err)
	}
	return out, nil
}

func uncoveredDaily(raw, daily []evidenceMetricRow) bool {
	type metricDay struct {
		series, day, metric string
	}
	rawCounts := make(map[metricDay]int64)
	for _, row := range raw {
		key := metricDay{series: row.SeriesID, day: row.BucketStart.UTC().Format("2006-01-02"), metric: row.Metric}
		rawCounts[key] += maxInt64(row.SampleCount)
	}
	for _, row := range daily {
		key := metricDay{series: row.SeriesID, day: row.BucketStart.UTC().Format("2006-01-02"), metric: row.Metric}
		if rawCounts[key] < maxInt64(row.SampleCount) {
			return true
		}
	}
	return false
}

func mergeEvidenceMetricRows(raw, daily []evidenceMetricRow, precision time.Duration) []evidenceMetricRow {
	type seriesDay struct {
		series, day string
	}
	dailyPreferred := make(map[seriesDay]struct{})
	if precision >= 24*time.Hour {
		type metricDay struct {
			series, day, metric string
		}
		rawCounts := make(map[metricDay]int64)
		for _, row := range raw {
			key := metricDay{series: row.SeriesID, day: row.BucketStart.UTC().Format("2006-01-02"), metric: row.Metric}
			rawCounts[key] += maxInt64(row.SampleCount)
		}
		for _, row := range daily {
			day := row.BucketStart.UTC().Format("2006-01-02")
			key := metricDay{series: row.SeriesID, day: day, metric: row.Metric}
			if rawCounts[key] < maxInt64(row.SampleCount) {
				dailyPreferred[seriesDay{series: row.SeriesID, day: day}] = struct{}{}
			}
		}
	}
	rows := make([]evidenceMetricRow, 0, len(raw)+len(daily))
	for _, row := range raw {
		key := seriesDay{series: row.SeriesID, day: row.BucketStart.UTC().Format("2006-01-02")}
		if _, preferDaily := dailyPreferred[key]; !preferDaily {
			rows = append(rows, row)
		}
	}
	for _, row := range daily {
		key := seriesDay{series: row.SeriesID, day: row.BucketStart.UTC().Format("2006-01-02")}
		if _, preferDaily := dailyPreferred[key]; preferDaily {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].SeriesID != rows[right].SeriesID {
			return rows[left].SeriesID < rows[right].SeriesID
		}
		if !rows[left].BucketStart.Equal(rows[right].BucketStart) {
			return rows[left].BucketStart.Before(rows[right].BucketStart)
		}
		return rows[left].Metric < rows[right].Metric
	})
	return rows
}

func evidenceCaptureFromRows(window evidence.TimeWindow, precision time.Duration, rows []evidenceMetricRow) adapters.MonitoringSeriesCapture {
	type bucketKey struct {
		series string
		start  time.Time
	}
	grouped := make(map[bucketKey]*adapters.MonitoringBucket)
	var observedStart, observedEnd, watermark time.Time
	producerVersions := make(map[string]struct{})
	for _, row := range rows {
		start, end := row.BucketStart.UTC(), row.BucketEnd.UTC()
		if start.Before(window.Start) {
			start = window.Start
		}
		if end.After(window.End) {
			end = window.End
		}
		if !end.After(start) {
			continue
		}
		key := bucketKey{series: row.SeriesID, start: start}
		bucket := grouped[key]
		if bucket == nil {
			bucket = &adapters.MonitoringBucket{
				SeriesID: row.SeriesID, SeriesKind: row.SeriesKind, Start: start, End: end,
				SourceLayer: adapters.MonitoringSourceLayer(row.SourceLayer), SourceGranularity: time.Duration(row.SourceGranularity) * time.Second,
				SampleCount: uint64(maxInt64(row.SampleCount)), MaintenanceCount: uint64(maxInt64(row.MaintenanceCount)), BackfilledCount: uint64(maxInt64(row.BackfilledCount)),
			}
			grouped[key] = bucket
		} else if bucket.SourceLayer != adapters.MonitoringSourceRaw && row.SourceLayer == string(adapters.MonitoringSourceRaw) {
			bucket.SourceLayer = adapters.MonitoringSourceRaw
		}
		bucket.Metrics = append(bucket.Metrics, adapters.MonitoringMetric{Name: row.Metric, Unit: row.Unit, Average: row.Average, Min: row.Minimum, Max: row.Maximum, P95: row.P95})
		if observedStart.IsZero() || row.ObservedStart.Before(observedStart) {
			observedStart = row.ObservedStart
		}
		if observedEnd.IsZero() || row.ObservedEnd.After(observedEnd) {
			observedEnd = row.ObservedEnd
		}
		if watermark.IsZero() || row.Watermark.After(watermark) {
			watermark = row.Watermark
		}
		for _, version := range row.ProducerVersions {
			if version != "" {
				producerVersions[version] = struct{}{}
			}
		}
	}
	out := make([]adapters.MonitoringBucket, 0, len(grouped))
	for _, bucket := range grouped {
		out = append(out, *bucket)
	}
	sort.Slice(out, func(left, right int) bool {
		if out[left].SeriesID != out[right].SeriesID {
			return out[left].SeriesID < out[right].SeriesID
		}
		return out[left].Start.Before(out[right].Start)
	})
	if observedStart.IsZero() {
		observedStart = window.Start
	}
	if observedEnd.IsZero() {
		observedEnd = window.Start
	}
	if observedEnd.Before(window.End) {
		observedEnd = observedEnd.Add(time.Microsecond)
	}
	if observedEnd.After(window.End) {
		observedEnd = window.End
	}
	if watermark.IsZero() {
		watermark = observedEnd
	}
	producers := make([]string, 0, len(producerVersions))
	for version := range producerVersions {
		producers = append(producers, version)
	}
	sort.Strings(producers)
	if len(producers) == 0 {
		producers = append(producers, "source/raw+aggregate-v1")
	}
	return adapters.MonitoringSeriesCapture{
		RequestedWindow: window, ActualPrecision: precision, CoverageStart: observedStart.UTC(), CoverageEnd: observedEnd.UTC(),
		ObservedAt: observedEnd.UTC().Add(-time.Microsecond), SourceWatermark: watermark.UTC().Format(time.RFC3339Nano), ProducerVersion: strings.Join(producers, ","), Buckets: out,
	}
}

func maxInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func (r *PostgresIPQualityRepository) LoadIPQualityEvidence(
	ctx context.Context, sourceID string, window evidence.TimeWindow,
) (adapters.IPQualityEvidenceReport, error) {
	if r == nil || r.db == nil {
		return adapters.IPQualityEvidenceReport{}, fmt.Errorf("IP quality evidence source unavailable")
	}
	rows, err := r.db.Query(ctx, ipQualityEvidenceReportSQL, sourceID, window.Start, window.End)
	if err != nil {
		return adapters.IPQualityEvidenceReport{}, fmt.Errorf("query IP quality evidence report: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return adapters.IPQualityEvidenceReport{}, err
		}
		return adapters.IPQualityEvidenceReport{}, fmt.Errorf("IP quality evidence report not found")
	}
	var report adapters.IPQualityEvidenceReport
	var coverageJSON []byte
	var staleSeconds int64
	if err := rows.Scan(
		&report.ReportID, &report.ObservedAt, &report.ReceivedAt, &report.AgentVersion,
		&report.IPAddress, &report.IPVersion, &report.Status, &report.ASN, &report.Organization,
		&report.Latitude, &report.Longitude, &report.UseRegionCode, &report.UseRegionName,
		&report.RegisteredRegionCode, &report.RegisteredRegionName, &report.RiskLevel,
		&report.IsBackfilled, &report.Ambiguous, &report.AssignmentMode, &coverageJSON, &staleSeconds,
	); err != nil {
		return adapters.IPQualityEvidenceReport{}, fmt.Errorf("scan IP quality evidence report: %w", err)
	}
	if err := rows.Err(); err != nil {
		return adapters.IPQualityEvidenceReport{}, err
	}
	report.StaleAfter = time.Duration(staleSeconds) * time.Second
	if len(coverageJSON) > 0 {
		var coverage struct {
			ExpectedProviderCount      int `json:"expected_provider_count"`
			SuccessfulProviderCount    int `json:"successful_provider_count"`
			FailedProviderCount        int `json:"failed_provider_count"`
			SkippedProviderCount       int `json:"skipped_provider_count"`
			NotConfiguredProviderCount int `json:"not_configured_provider_count"`
			ExpectedServiceCount       int `json:"expected_service_count"`
			SuccessfulServiceCount     int `json:"successful_service_count"`
			FailedServiceCount         int `json:"failed_service_count"`
			SkippedServiceCount        int `json:"skipped_service_count"`
			NotConfiguredServiceCount  int `json:"not_configured_service_count"`
		}
		if err := json.Unmarshal(coverageJSON, &coverage); err != nil {
			return adapters.IPQualityEvidenceReport{}, fmt.Errorf("decode IP quality evidence coverage: %w", err)
		}
		report.Coverage = adapters.IPQualityCoverage{
			ExpectedProviderCount: coverage.ExpectedProviderCount, SuccessfulProviderCount: coverage.SuccessfulProviderCount,
			FailedProviderCount: coverage.FailedProviderCount, SkippedProviderCount: coverage.SkippedProviderCount,
			NotConfiguredProviderCount: coverage.NotConfiguredProviderCount, ExpectedServiceCount: coverage.ExpectedServiceCount,
			SuccessfulServiceCount: coverage.SuccessfulServiceCount, FailedServiceCount: coverage.FailedServiceCount,
			SkippedServiceCount: coverage.SkippedServiceCount, NotConfiguredServiceCount: coverage.NotConfiguredServiceCount,
		}
	}
	report.Providers, err = loadIPQualityEvidenceProviders(ctx, r.db, report.ReportID)
	if err != nil {
		return adapters.IPQualityEvidenceReport{}, err
	}
	report.Services, err = loadIPQualityEvidenceServices(ctx, r.db, report.ReportID)
	if err != nil {
		return adapters.IPQualityEvidenceReport{}, err
	}
	return report, nil
}

func loadIPQualityEvidenceProviders(ctx context.Context, db ipQualityDB, reportID string) ([]adapters.IPQualityProviderEvidence, error) {
	rows, err := db.Query(ctx, ipQualityEvidenceProvidersSQL, reportID)
	if err != nil {
		return nil, fmt.Errorf("query IP quality evidence providers: %w", err)
	}
	defer rows.Close()
	out := make([]adapters.IPQualityProviderEvidence, 0)
	for rows.Next() {
		var provider adapters.IPQualityProviderEvidence
		if err := rows.Scan(
			&provider.Provider, &provider.Status, &provider.SourceType, &provider.LatencyMS,
			&provider.UsageType, &provider.CompanyType, &provider.RiskLevel, &provider.RiskScore,
			&provider.RegionCode, &provider.RegionName, &provider.IsProxy, &provider.IsTor, &provider.IsVPN,
			&provider.IsServer, &provider.IsAbuser, &provider.IsRobot, &provider.ErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan IP quality evidence provider: %w", err)
		}
		out = append(out, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate IP quality evidence providers: %w", err)
	}
	return out, nil
}

func loadIPQualityEvidenceServices(ctx context.Context, db ipQualityDB, reportID string) ([]adapters.IPQualityServiceEvidence, error) {
	rows, err := db.Query(ctx, ipQualityEvidenceServicesSQL, reportID)
	if err != nil {
		return nil, fmt.Errorf("query IP quality evidence services: %w", err)
	}
	defer rows.Close()
	out := make([]adapters.IPQualityServiceEvidence, 0)
	for rows.Next() {
		var service adapters.IPQualityServiceEvidence
		if err := rows.Scan(
			&service.Service, &service.Source, &service.Status, &service.ProbeStatus,
			&service.LatencyMS, &service.Region, &service.UnlockType, &service.ErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan IP quality evidence service: %w", err)
		}
		out = append(out, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate IP quality evidence services: %w", err)
	}
	return out, nil
}

package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MonitoringInstanceRuntimeSummary is the latest current-binding host sample
type MonitoringInstanceRuntimeSummary struct {
	ObservedAt        time.Time `json:"observed_at"`
	ReceivedAt        time.Time `json:"received_at"`
	UptimeSeconds     int64     `json:"uptime_seconds"`
	NetInBytesPerSec  *int64    `json:"net_in_bytes_per_sec"`
	NetOutBytesPerSec *int64    `json:"net_out_bytes_per_sec"`
}

// MonitoringInstanceRuntimeSummaries is the batch result returned by the
// runtime-summaries endpoint. MonitoringInstances contains every active
// monitoring instance; a nil value means that no eligible host sample exists.
type MonitoringInstanceRuntimeSummaries struct {
	ReadAt              time.Time
	MonitoringInstances map[string]*MonitoringInstanceRuntimeSummary
}

// MonitoringInstanceRuntimeSummariesRepository provides one batch read for
// the latest eligible host sample of every active monitoring instance.
type MonitoringInstanceRuntimeSummariesRepository interface {
	GetMonitoringInstanceRuntimeSummaries(context.Context) (MonitoringInstanceRuntimeSummaries, error)
}

type monitoringInstanceRuntimeSummariesQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PostgresMonitoringInstanceRuntimeSummariesRepository implements the batch
// runtime summary projection against monitoring_instances and host_samples.
type PostgresMonitoringInstanceRuntimeSummariesRepository struct {
	db monitoringInstanceRuntimeSummariesQueryer
}

func NewPostgresMonitoringInstanceRuntimeSummariesRepository(db *pgxpool.Pool) *PostgresMonitoringInstanceRuntimeSummariesRepository {
	return &PostgresMonitoringInstanceRuntimeSummariesRepository{db: db}
}

var _ MonitoringInstanceRuntimeSummariesRepository = (*PostgresMonitoringInstanceRuntimeSummariesRepository)(nil)

const getMonitoringInstanceRuntimeSummariesSQL = `
	with active_instances as (
		select
			mi.monitoring_instance_id,
			coalesce(mi.binding_fingerprint, '') as binding_fingerprint,
			mi.binding_epoch_started_at
		from monitoring_instances mi
		where mi.archived_at is null
			and (
				not exists (
					select 1
					from vps_monitoring_instance_links l
					where l.monitoring_instance_id = mi.monitoring_instance_id
						and l.unlinked_at is null
				)
				or exists (
					select 1
					from vps_monitoring_instance_links l
					join vps_assets v on v.vps_id = l.vps_id
					where l.monitoring_instance_id = mi.monitoring_instance_id
						and l.unlinked_at is null
						and v.lifecycle_status not in ('cancelled', 'archived')
				)
			)
	), latest_samples as (
		select distinct on (hs.monitoring_instance_id)
			hs.monitoring_instance_id,
			hs.observed_at,
			hs.received_at,
			hs.uptime_seconds,
			hs.net_in_bytes_per_sec,
			hs.net_out_bytes_per_sec,
			hs.network_rates_valid
		from host_samples hs
		join active_instances mi on mi.monitoring_instance_id = hs.monitoring_instance_id
		where mi.binding_fingerprint <> ''
			and mi.binding_epoch_started_at is not null
			and hs.fingerprint = mi.binding_fingerprint
			and hs.received_at >= mi.binding_epoch_started_at
			and hs.observed_at <= $1
			and hs.received_at <= $1
		order by hs.monitoring_instance_id,
			hs.observed_at desc,
			hs.is_backfilled asc,
			hs.received_at desc,
			hs.id desc
	)
	select
		mi.monitoring_instance_id,
		latest.observed_at,
		latest.received_at,
		latest.uptime_seconds,
		latest.net_in_bytes_per_sec,
		latest.net_out_bytes_per_sec,
		latest.network_rates_valid
	from active_instances mi
	left join latest_samples latest on latest.monitoring_instance_id = mi.monitoring_instance_id
	order by mi.monitoring_instance_id`

func (r *PostgresMonitoringInstanceRuntimeSummariesRepository) GetMonitoringInstanceRuntimeSummaries(ctx context.Context) (MonitoringInstanceRuntimeSummaries, error) {
	readAt := time.Now().UTC()
	rows, err := r.db.Query(ctx, getMonitoringInstanceRuntimeSummariesSQL, readAt)
	if err != nil {
		return MonitoringInstanceRuntimeSummaries{}, fmt.Errorf("query monitoring instance runtime summaries: %w", err)
	}
	defer rows.Close()

	result := MonitoringInstanceRuntimeSummaries{
		ReadAt:              readAt,
		MonitoringInstances: make(map[string]*MonitoringInstanceRuntimeSummary),
	}
	for rows.Next() {
		var (
			monitoringInstanceID string
			observedAt           sql.NullTime
			receivedAt           sql.NullTime
			uptimeSeconds        sql.NullInt64
			netInBytesPerSec     sql.NullInt64
			netOutBytesPerSec    sql.NullInt64
			networkRatesValid    sql.NullBool
		)
		if err := rows.Scan(
			&monitoringInstanceID,
			&observedAt,
			&receivedAt,
			&uptimeSeconds,
			&netInBytesPerSec,
			&netOutBytesPerSec,
			&networkRatesValid,
		); err != nil {
			return MonitoringInstanceRuntimeSummaries{}, fmt.Errorf("scan monitoring instance runtime summary: %w", err)
		}
		if !observedAt.Valid || !receivedAt.Valid || !uptimeSeconds.Valid {
			result.MonitoringInstances[monitoringInstanceID] = nil
			continue
		}

		summary := &MonitoringInstanceRuntimeSummary{
			ObservedAt:    observedAt.Time,
			ReceivedAt:    receivedAt.Time,
			UptimeSeconds: uptimeSeconds.Int64,
		}
		if networkRatesValid.Valid && networkRatesValid.Bool && netInBytesPerSec.Valid && netOutBytesPerSec.Valid {
			netIn := netInBytesPerSec.Int64
			netOut := netOutBytesPerSec.Int64
			summary.NetInBytesPerSec = &netIn
			summary.NetOutBytesPerSec = &netOut
		}
		result.MonitoringInstances[monitoringInstanceID] = summary
	}
	if err := rows.Err(); err != nil {
		return MonitoringInstanceRuntimeSummaries{}, fmt.Errorf("iterate monitoring instance runtime summaries: %w", err)
	}
	return result, nil
}

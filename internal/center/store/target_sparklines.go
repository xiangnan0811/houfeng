package store

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TargetSparklinesRepository provides downsampled metric time-series for all
// active targets, grouped by target_id. Each metric returns exactly downSample
// bucket-level average values spanning the window [since, now].
type TargetSparklinesRepository interface {
	GetTargetSparklines(ctx context.Context, metrics []string, since time.Time, downsample int) (map[string]map[string][]float64, error)
}

type targetSparklinesQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PostgresTargetSparklinesRepository implements TargetSparklinesRepository against
// the probe_observations table.
type PostgresTargetSparklinesRepository struct {
	db targetSparklinesQueryer
}

func NewPostgresTargetSparklinesRepository(db *pgxpool.Pool) *PostgresTargetSparklinesRepository {
	return &PostgresTargetSparklinesRepository{db: db}
}

// ValidTargetSparklineMetrics contains every numeric column in probe_observations
// that can be requested as a sparkline metric.
var ValidTargetSparklineMetrics = map[string]bool{
	"latency": true,
}

const getTargetSparklinesSQL = `
	select
		target_id,
		observed_at,
		latency_ms
	from probe_observations
	where observed_at >= $1
	  and latency_ms is not null
	order by target_id, observed_at asc`

func (r *PostgresTargetSparklinesRepository) GetTargetSparklines(ctx context.Context, metrics []string, since time.Time, downsample int) (map[string]map[string][]float64, error) {
	if downsample <= 0 {
		return nil, fmt.Errorf("downsample must be positive, got %d", downsample)
	}

	rows, err := r.db.Query(ctx, getTargetSparklinesSQL, since)
	if err != nil {
		return nil, fmt.Errorf("query probe_observations for sparklines: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	windowDuration := now.Sub(since)
	if windowDuration <= 0 {
		return map[string]map[string][]float64{}, nil
	}

	// Accumulators: per-target -> per-metric -> per-bucket -> (sum, count)
	type bucketAcc struct {
		sum   float64
		count int
	}
	type metricAcc map[string][]bucketAcc
	targetAcc := make(map[string]metricAcc)

	for rows.Next() {
		var targetID string
		var observedAt time.Time
		var latencyMs float64

		if err := rows.Scan(&targetID, &observedAt, &latencyMs); err != nil {
			return nil, fmt.Errorf("scan probe_observation row: %w", err)
		}

		// Determine bucket index.
		bucketIdx := int(float64(observedAt.Sub(since)) / float64(windowDuration) * float64(downsample))
		if bucketIdx < 0 {
			bucketIdx = 0
		}
		if bucketIdx >= downsample {
			bucketIdx = downsample - 1
		}

		// Initialize per-target accumulator if needed.
		ma, ok := targetAcc[targetID]
		if !ok {
			ma = make(metricAcc, len(metrics))
			for _, m := range metrics {
				ma[m] = make([]bucketAcc, downsample)
			}
			targetAcc[targetID] = ma
		}

		// Skip NaN / Inf values.
		if math.IsNaN(latencyMs) || math.IsInf(latencyMs, 0) {
			continue
		}

		// Accumulate latency metric (only metric supported for now).
		for _, m := range metrics {
			if m == "latency" {
				ma[m][bucketIdx].sum += latencyMs
				ma[m][bucketIdx].count++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate probe_observations: %w", err)
	}

	// Build the result map.
	result := make(map[string]map[string][]float64, len(targetAcc))
	for targetID, ma := range targetAcc {
		targetResult := make(map[string][]float64, len(metrics))
		for _, m := range metrics {
			buckets := make([]float64, downsample)
			acc := ma[m]
			for b := 0; b < downsample; b++ {
				if acc[b].count > 0 {
					buckets[b] = math.Round(acc[b].sum/float64(acc[b].count)*10) / 10
				}
			}
			targetResult[m] = buckets
		}
		result[targetID] = targetResult
	}

	return result, nil
}

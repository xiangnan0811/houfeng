package store

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NodeSparklinesRepository provides downsampled metric time-series for all
// active nodes, grouped by node_id. Each metric returns exactly downSample
// bucket-level average values spanning the window [since, now].
type NodeSparklinesRepository interface {
	GetNodeSparklines(ctx context.Context, metrics []string, since time.Time, downsample int) (map[string]map[string][]float64, error)
}

type sparklinesQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PostgresNodeSparklinesRepository implements NodeSparklinesRepository against
// the host_samples table.
type PostgresNodeSparklinesRepository struct {
	db sparklinesQueryer
}

func NewPostgresNodeSparklinesRepository(db *pgxpool.Pool) *PostgresNodeSparklinesRepository {
	return &PostgresNodeSparklinesRepository{db: db}
}

// ValidSparklineMetrics contains every numeric column in host_samples that can
// be requested as a sparkline metric. The key is the snake_case column name
// matching the JSON tag used by runtimefacts.HostSample.
var ValidSparklineMetrics = map[string]bool{
	"cpu_usage_pct":            true,
	"load_1":                   true,
	"load_5":                   true,
	"load_15":                  true,
	"mem_used_pct":             true,
	"mem_available_bytes":      true,
	"swap_used_pct":            true,
	"disk_used_pct":            true,
	"inode_used_pct":           true,
	"net_in_bytes_per_sec":     true,
	"net_out_bytes_per_sec":    true,
	"cpu_iowait_pct":           true,
	"cpu_steal_pct":            true,
	"disk_read_bytes_per_sec":  true,
	"disk_write_bytes_per_sec": true,
	"disk_busy_pct":            true,
	"uptime_seconds":           true,
}

const getNodeSparklinesSQL = `
	select
		node_id,
		observed_at,
		cpu_usage_pct,
		load_1,
		load_5,
		load_15,
		mem_used_pct,
		mem_available_bytes,
		swap_used_pct,
		disk_used_pct,
		inode_used_pct,
		net_in_bytes_per_sec,
		net_out_bytes_per_sec,
		cpu_iowait_pct,
		cpu_steal_pct,
		disk_read_bytes_per_sec,
		disk_write_bytes_per_sec,
		disk_busy_pct,
		uptime_seconds
	from host_samples
	where observed_at >= $1
	order by node_id, observed_at asc`

func (r *PostgresNodeSparklinesRepository) GetNodeSparklines(ctx context.Context, metrics []string, since time.Time, downsample int) (map[string]map[string][]float64, error) {
	if downsample <= 0 {
		return nil, fmt.Errorf("downsample must be positive, got %d", downsample)
	}

	rows, err := r.db.Query(ctx, getNodeSparklinesSQL, since)
	if err != nil {
		return nil, fmt.Errorf("query host_samples for sparklines: %w", err)
	}
	defer rows.Close()

	// Build metric index (column position in the scan) from the fixed query order.
	metricIndex := func(m string) int {
		switch m {
		case "cpu_usage_pct":
			return 0
		case "load_1":
			return 1
		case "load_5":
			return 2
		case "load_15":
			return 3
		case "mem_used_pct":
			return 4
		case "mem_available_bytes":
			return 5
		case "swap_used_pct":
			return 6
		case "disk_used_pct":
			return 7
		case "inode_used_pct":
			return 8
		case "net_in_bytes_per_sec":
			return 9
		case "net_out_bytes_per_sec":
			return 10
		case "cpu_iowait_pct":
			return 11
		case "cpu_steal_pct":
			return 12
		case "disk_read_bytes_per_sec":
			return 13
		case "disk_write_bytes_per_sec":
			return 14
		case "disk_busy_pct":
			return 15
		case "uptime_seconds":
			return 16
		default:
			return -1
		}
	}

	// Pre-build a list of metric indices for efficient scanning.
	metricIndices := make([]int, len(metrics))
	for i, m := range metrics {
		metricIndices[i] = metricIndex(m)
	}

	now := time.Now()
	windowDuration := now.Sub(since)
	if windowDuration <= 0 {
		return map[string]map[string][]float64{}, nil
	}

	// Accumulators: per-node -> per-metric -> per-bucket -> (sum, count)
	type bucketAcc struct {
		sum   float64
		count int
	}
	type metricAcc map[string][]bucketAcc // metric name -> bucket slice
	nodeAcc := make(map[string]metricAcc)

	for rows.Next() {
		var nodeID string
		var observedAt time.Time
		// 17 numeric columns matching the SELECT order.
		var vals [17]float64

		if err := rows.Scan(
			&nodeID,
			&observedAt,
			&vals[0],  // cpu_usage_pct
			&vals[1],  // load_1
			&vals[2],  // load_5
			&vals[3],  // load_15
			&vals[4],  // mem_used_pct
			&vals[5],  // mem_available_bytes
			&vals[6],  // swap_used_pct
			&vals[7],  // disk_used_pct
			&vals[8],  // inode_used_pct
			&vals[9],  // net_in_bytes_per_sec
			&vals[10], // net_out_bytes_per_sec
			&vals[11], // cpu_iowait_pct
			&vals[12], // cpu_steal_pct
			&vals[13], // disk_read_bytes_per_sec
			&vals[14], // disk_write_bytes_per_sec
			&vals[15], // disk_busy_pct
			&vals[16], // uptime_seconds
		); err != nil {
			return nil, fmt.Errorf("scan host_sample row: %w", err)
		}

		// Determine bucket index.
		bucketIdx := int(float64(observedAt.Sub(since)) / float64(windowDuration) * float64(downsample))
		if bucketIdx < 0 {
			bucketIdx = 0
		}
		if bucketIdx >= downsample {
			bucketIdx = downsample - 1
		}

		// Initialize per-node accumulator if needed.
		ma, ok := nodeAcc[nodeID]
		if !ok {
			ma = make(metricAcc, len(metrics))
			for _, m := range metrics {
				ma[m] = make([]bucketAcc, downsample)
			}
			nodeAcc[nodeID] = ma
		}

		// Accumulate each requested metric.
		for i, m := range metrics {
			idx := metricIndices[i]
			if idx < 0 {
				continue
			}
			v := vals[idx]
			// Some integer columns come back as 0.0 when NULL; that's fine for
			// aggregation (skipping NaN / Inf values).
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			ma[m][bucketIdx].sum += v
			ma[m][bucketIdx].count++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate host_samples: %w", err)
	}

	// Build the result map. Nodes with no rows in the window are absent.
	result := make(map[string]map[string][]float64, len(nodeAcc))
	for nodeID, ma := range nodeAcc {
		nodeResult := make(map[string][]float64, len(metrics))
		for _, m := range metrics {
			buckets := make([]float64, downsample)
			acc := ma[m]
			for b := 0; b < downsample; b++ {
				if acc[b].count > 0 {
					buckets[b] = math.Round(acc[b].sum/float64(acc[b].count)*10) / 10
				}
			}
			nodeResult[m] = buckets
		}
		result[nodeID] = nodeResult
	}

	return result, nil
}

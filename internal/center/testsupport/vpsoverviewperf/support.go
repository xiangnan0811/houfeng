// Package vpsoverviewperf owns shared PostgreSQL overview performance-test
// fixtures and assertions. Production packages must not import it.
package vpsoverviewperf

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/vpsoverview"
)

const ExpectedQueryCount = 21

var expectedRelations = []string{
	"monitoring_instances",
	"subscriptions",
	"services",
	"domains",
}

// SeedAuthority is intentionally called by each package-level performance gate
// against its own isolated database. Keeping the SQL here makes fixture/schema
// changes single-owner while preserving store/handler test isolation.
func SeedAuthority(ctx context.Context, pool *pgxpool.Pool, vpsID string) error {
	fixtures := []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "vps",
			sql: `insert into public.vps_assets (
				vps_id, display_name, provider_name, product_name, country, region, city,
				datacenter, ipv4, lifecycle_status, usage_status, renewal_decision, labels, updated_at
			) values ($1, 'perf-vps', 'Performance Provider', 'Performance VPS', 'JP', 'Tokyo', 'Tokyo',
				'perf-dc', '192.0.2.10', 'active', 'in_use', 'keep', '{}', now() - interval '6 minutes')`,
			args: []any{vpsID},
		},
		{
			name: "monitoring instance",
			sql: `insert into public.monitoring_instances (
				monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
				monitoring_status, binding_status, current_health_status, last_heartbeat_at, updated_at
			) values ('mi_overview_perf', 'perf-monitor', 'Tokyo', 'Tokyo', 'Performance Provider', '在用',
				'启用', '已绑定', '正常', now() - interval '1 minute', now() - interval '1 minute')`,
		},
		{
			name: "monitoring link",
			sql: `insert into public.vps_monitoring_instance_links (
				link_id, vps_id, monitoring_instance_id, note
			) values ('vnl_overview_perf', $1, 'mi_overview_perf', '')`,
			args: []any{vpsID},
		},
		{
			name: "IP quality report",
			sql: `insert into public.ip_quality_reports (
				report_id, monitoring_instance_id, observed_at, received_at, agent_version,
				fingerprint, sync_batch_id, ip_address, ip_version, status, risk_level
			) values ('ipq_overview_perf', 'mi_overview_perf', now() - interval '2 minutes',
				now() - interval '2 minutes', 'perf-agent/v1', 'perf-fingerprint', 'perf-sync',
				'192.0.2.10', 4, 'success', 'low')`,
		},
		{
			name: "subscription",
			sql: `insert into public.subscriptions (
				subscription_id, vps_id, price, currency, billing_cycle, billing_months,
				monthly_price, started_at, renew_at, status, updated_at
			) values ('sub_overview_perf', $1, 20, 'USD', 'monthly', 1, 20,
				current_date - 30, current_date + 30, 'active', now() - interval '3 minutes')`,
			args: []any{vpsID},
		},
		{
			name: "service",
			sql: `insert into public.asset_services (
				service_id, vps_id, name, service_type, status, updated_at
			) values ('svc_overview_perf', $1, 'perf-api', 'api', 'active', now() - interval '4 minutes')`,
			args: []any{vpsID},
		},
		{
			name: "domain",
			sql: `insert into public.asset_domains (
				domain_id, vps_id, service_id, domain_name, status, updated_at
			) values ('dom_overview_perf', $1, 'svc_overview_perf', 'perf-overview.example',
				'active', now() - interval '5 minutes')`,
			args: []any{vpsID},
		},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, fixture.sql, fixture.args...); err != nil {
			return fmt.Errorf("insert %s: %w", fixture.name, err)
		}
	}
	return nil
}

// PrepareMeasurement refreshes statistics for every table read by the
// production overview path, then completes the checkpoint caused by the
// million-row fixture before the latency window starts. Without the explicit
// checkpoint, PostgreSQL can still be flushing the artificial bulk seed while
// a measured request is running, which makes this gate depend on checkpoint
// timing rather than overview query latency.
func PrepareMeasurement(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
			analyze public.record_activity_subjects;
			analyze public.record_activity_projection;
		analyze public.vps_assets;
		analyze public.monitoring_instances;
		analyze public.vps_monitoring_instance_links;
		analyze public.ip_quality_reports;
			analyze public.subscriptions;
			analyze public.asset_services;
			analyze public.asset_domains;
			checkpoint`)
	return err
}

// OpenTracedPool opens a second pool against the isolated fixture database so
// setup queries never pollute runtime query statistics.
func OpenTracedPool(
	t testing.TB,
	ctx context.Context,
	source *pgxpool.Pool,
	trace *QueryTrace,
) *pgxpool.Pool {
	t.Helper()
	if source == nil || trace == nil {
		t.Fatal("overview performance traced pool requires source and trace")
	}
	config := source.Config().Copy()
	config.MinConns = 0
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open overview performance traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// QueryTrace counts runtime PostgreSQL executions and terminal query errors.
type QueryTrace struct {
	mu          sync.Mutex
	queryCount  int
	queryErrors int
}

var _ pgx.QueryTracer = (*QueryTrace)(nil)

func (trace *QueryTrace) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryStartData,
) context.Context {
	trace.mu.Lock()
	trace.queryCount++
	trace.mu.Unlock()
	return ctx
}

func (trace *QueryTrace) TraceQueryEnd(
	_ context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryEndData,
) {
	if data.Err == nil {
		return
	}
	trace.mu.Lock()
	trace.queryErrors++
	trace.mu.Unlock()
}

func (trace *QueryTrace) Reset() {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.queryCount = 0
	trace.queryErrors = 0
}

func (trace *QueryTrace) Snapshot() QueryStats {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return QueryStats{Count: trace.queryCount, Errors: trace.queryErrors}
}

// QueryStats is one immutable snapshot from QueryTrace.
type QueryStats struct {
	Count  int
	Errors int
}

func (stats QueryStats) Validate() error {
	if stats.Errors != 0 {
		return fmt.Errorf("PostgreSQL query errors = %d, want 0", stats.Errors)
	}
	if stats.Count != ExpectedQueryCount {
		return fmt.Errorf("PostgreSQL queries = %d, want stable %d", stats.Count, ExpectedQueryCount)
	}
	return nil
}

func (stats QueryStats) ErrorRatePercent() float64 {
	if stats.Count == 0 {
		return 0
	}
	return 100 * float64(stats.Errors) / float64(stats.Count)
}

// ValidateHealthyOverview rejects partial responses so source degradation
// cannot make the performance lane pass by doing less work.
func ValidateHealthyOverview(overview vpsoverview.Overview, expectedVPSID string) error {
	if overview.Identity.VPSID != expectedVPSID {
		return fmt.Errorf("identity vps_id = %q, want %q", overview.Identity.VPSID, expectedVPSID)
	}
	sections := []struct {
		name  string
		state string
	}{
		{name: "overall", state: overview.Summary.Overall.Section.State},
		{name: "monitoring", state: overview.Summary.Monitoring.Section.State},
		{name: "ip_quality", state: overview.Summary.IPQuality.Section.State},
		{name: "renewal", state: overview.Summary.Renewal.Section.State},
		{name: "activity", state: overview.RecentActivity.Section.State},
	}
	for _, section := range sections {
		if section.state != vpsoverview.SectionReady {
			return fmt.Errorf("%s section state = %q, want %q", section.name, section.state, vpsoverview.SectionReady)
		}
	}
	if len(overview.RecentActivity.Items) == 0 {
		return fmt.Errorf("recent activity is empty")
	}
	if len(overview.Relations) != len(expectedRelations) {
		return fmt.Errorf("relations length = %d, want %d", len(overview.Relations), len(expectedRelations))
	}
	for index, expectedKind := range expectedRelations {
		relation := overview.Relations[index]
		if relation.Kind != expectedKind {
			return fmt.Errorf("relation %d kind = %q, want %q", index, relation.Kind, expectedKind)
		}
		if relation.Count != 1 {
			return fmt.Errorf("relation %q count = %d, want 1", relation.Kind, relation.Count)
		}
		if relation.Section.State != vpsoverview.SectionReady {
			return fmt.Errorf(
				"relation %q section state = %q, want %q",
				relation.Kind,
				relation.Section.State,
				vpsoverview.SectionReady,
			)
		}
	}
	return nil
}

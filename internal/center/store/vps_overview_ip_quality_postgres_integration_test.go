package store

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	centerrecords "houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsoverview"
)

func TestPostgresOverviewDisabledIPQualityDoesNotJudgeLeftoverOrMissingReport(t *testing.T) {
	t.Parallel()

	t.Run("default disabled without report", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pool := openTemporaryOverviewIPQualityPostgresSchema(t, ctx)
		vps := createOverviewIPQualityVPS(t, ctx, pool, "203.0.113.80")
		seedHealthyNonIPOverviewSources(t, ctx, pool, vps.VPSID, "mi_overview_ipq_none")
		assertCheapIPQualityDisabled(t, ctx, pool)

		overview := loadOverviewThroughBootstrapEquivalent(t, ctx, pool, vps.VPSID)
		if overview.Summary.IPQuality.Status != "not_configured" {
			t.Fatalf("ip quality status = %q, want not_configured", overview.Summary.IPQuality.Status)
		}
		if overview.Summary.IPQuality.Section.ReasonCode != "" {
			t.Fatalf("reason = %q, want empty when no leftover report exists", overview.Summary.IPQuality.Section.ReasonCode)
		}
		assertOverviewDoesNotJudgeLeftoverIP(t, overview, "healthy")
	})

	t.Run("disabled leftover high-risk report", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pool := openTemporaryOverviewIPQualityPostgresSchema(t, ctx)
		vps := createOverviewIPQualityVPS(t, ctx, pool, "203.0.113.81")
		seedDisabledCenterSettings(t, ctx, pool)
		seedHealthyNonIPOverviewSources(t, ctx, pool, vps.VPSID, "mi_leftover_ipq")
		seedLeftoverHighRiskIPQualityReport(t, ctx, pool, "mi_leftover_ipq", "203.0.113.81")
		assertCheapIPQualityDisabled(t, ctx, pool)

		summary, err := NewPostgresIPQualityRepository(pool).GetLatestVPSIPQualitySummary(ctx, vps.VPSID)
		if err != nil {
			t.Fatalf("GetLatestVPSIPQualitySummary: %v", err)
		}
		if summary == nil || summary.RiskLevel != "high" || summary.Status != "partial" || !summary.Stale {
			t.Fatalf("leftover summary = %#v, want high-risk stale partial still visible to the summary source", summary)
		}

		overview := loadOverviewThroughBootstrapEquivalent(t, ctx, pool, vps.VPSID)
		if overview.Summary.IPQuality.Status != "not_configured" {
			t.Fatalf("ip quality status = %q, leftover must not drive current judgement", overview.Summary.IPQuality.Status)
		}
		if overview.Summary.IPQuality.Section.ReasonCode != "" {
			t.Fatalf("reason = %q, want empty; leftover history must stay off Overview judgement", overview.Summary.IPQuality.Section.ReasonCode)
		}
		assertOverviewDoesNotJudgeLeftoverIP(t, overview, "healthy")
	})
}

func assertOverviewDoesNotJudgeLeftoverIP(t *testing.T, overview vpsoverview.Overview, wantOverall string) {
	t.Helper()
	forbidden := []string{
		vpsoverview.RuleIPQualityMissing,
		vpsoverview.RuleIPQualityStale,
		vpsoverview.RuleIPQualityPartial,
		vpsoverview.RuleIPQualityRiskElevated,
	}
	rules := overviewRuleIDs(overview)
	for _, rule := range forbidden {
		for _, got := range rules {
			if got == rule {
				t.Fatalf("anomalies = %#v, must not emit %s", rules, rule)
			}
		}
	}
	if overview.Summary.Overall.Status != wantOverall {
		t.Fatalf("overall = %s anomalies=%#v, want %s", overview.Summary.Overall.Status, rules, wantOverall)
	}
	if overview.Summary.IPQuality.Status == "high" || overview.Summary.IPQuality.Detail == "partial" {
		t.Fatalf("summary leaked leftover judgement fields: %#v", overview.Summary.IPQuality)
	}
}

func assertCheapIPQualityDisabled(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	enabled, err := NewPostgresSettingsRepository(pool).IPQualityEnabled(ctx)
	if err != nil {
		t.Fatalf("IPQualityEnabled: %v", err)
	}
	if enabled {
		t.Fatal("IPQualityEnabled = true, want default/disabled")
	}
}

func createOverviewIPQualityVPS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ipv4 string) vpsassets.Record {
	t.Helper()
	vps, err := NewPostgresVPSAssetRepository(pool).CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Leftover IP Quality",
		IPv4:            ipv4,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalKeep,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset: %v", err)
	}
	return vps
}

func seedDisabledCenterSettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into center_settings (settings_id, ip_quality_settings)
		values ('center', jsonb_build_object(
			'enabled', false,
			'frequency_seconds', 86400,
			'timeout_seconds', 15,
			'raw_retention_days', 90,
			'history_retention_days', 365,
			'stale_after_seconds', 604800,
			'services', '[]'::jsonb
		))`); err != nil {
		t.Fatalf("seed disabled center_settings: %v", err)
	}
}

func seedHealthyNonIPOverviewSources(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID, monitoringInstanceID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into public.monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, current_health_status, last_heartbeat_at, updated_at
		) values (
			$1, 'overview-ipq-monitor', 'Tokyo', 'Tokyo', 'Example', '在用',
			'启用', '已绑定', '正常', now() - interval '1 minute', now() - interval '1 minute'
		)`, monitoringInstanceID); err != nil {
		t.Fatalf("seed monitoring instance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.vps_monitoring_instance_links (
			link_id, vps_id, monitoring_instance_id, note
		) values ('vnl_overview_ipq', $1, $2, '')`, vpsID, monitoringInstanceID); err != nil {
		t.Fatalf("seed monitoring link: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, started_at, renew_at, status, updated_at
		) values (
			'sub_overview_ipq', $1, 20, 'USD', 'monthly', 1, 20,
			current_date - 30, current_date + 30, 'active', now() - interval '3 minutes'
		)`, vpsID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
}

func seedLeftoverHighRiskIPQualityReport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID, ipv4 string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into public.ip_quality_reports (
			report_id, monitoring_instance_id, observed_at, received_at, agent_version,
			fingerprint, sync_batch_id, ip_address, ip_version, status, risk_level
		) values (
			'ipq_leftover_high', $1, now() - interval '10 days',
			now() - interval '10 days', 'leftover-agent/v1', 'leftover-fingerprint', 'leftover-sync',
			$2, 4, 'partial', 'high'
		)`, monitoringInstanceID, ipv4); err != nil {
		t.Fatalf("seed leftover ip quality report: %v", err)
	}
}

func loadOverviewThroughBootstrapEquivalent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string) vpsoverview.Overview {
	t.Helper()
	vpsRepository := NewPostgresVPSAssetRepository(pool)
	sources, err := NewVPSOverviewRepository(
		vpsRepository,
		NewPostgresVPSMonitoringInstanceLinkRepository(pool),
		NewPostgresIPQualityRepository(pool),
		NewPostgresSettingsRepository(pool),
		NewPostgresSubscriptionRepository(pool),
		NewPostgresAssetServiceRepository(pool),
		NewPostgresAssetDomainRepository(pool),
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	subjects, err := centerrecords.NewSubjectAdapterRegistry([]centerrecords.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(vpsRepository),
		NewMonitoringInstanceRecordSubjectAdapter(NewPostgresMonitoringInstanceRepository(pool)),
		NewTargetRecordSubjectAdapter(NewPostgresTargetRepository(pool)),
	})
	if err != nil {
		t.Fatalf("subject registry: %v", err)
	}
	activityRepository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("NewActivityProjectionRepository: %v", err)
	}
	codec, err := activity.NewCursorCodec([]byte("houfeng-overview-ipq-integ-hmac-key!!"))
	if err != nil {
		t.Fatalf("activity cursor codec: %v", err)
	}
	activityService, err := activity.NewService(
		activityRepository, activityRepository, NewActivityLiveSubjectResolver(subjects), codec,
	)
	if err != nil {
		t.Fatalf("activity.NewService: %v", err)
	}
	service, err := vpsoverview.NewService(sources, activityService)
	if err != nil {
		t.Fatalf("vpsoverview.NewService: %v", err)
	}
	overview, err := service.Get(ctx, vpsoverview.Request{
		Actor: testOverviewServiceActor(t),
		VPSID: vpsID,
	})
	if err != nil {
		t.Fatalf("overview Get: %v", err)
	}
	return overview
}

func openTemporaryOverviewIPQualityPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for VPS overview IP quality PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for VPS overview IP quality PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_ov_ipq_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := storemigrate.Apply(ctx, testPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 1, 0, 0)`); err != nil {
		t.Fatalf("seed activity projection head: %v", err)
	}
	return testPool
}

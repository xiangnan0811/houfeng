package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/vpsassets"
)

func TestVPSCreateIdempotencyLostResponsePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporarySubscriptionIdempotencyPostgresSchema(t, ctx)

	vpsRepo := NewPostgresVPSAssetRepository(pool)
	initialVPSLabels := []string{"integration-default"}
	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Initial VPS display",
		ProviderName:    "Initial VPS provider",
		Country:         "Initial VPS country",
		Region:          "Initial VPS region",
		City:            "Initial VPS city",
		Datacenter:      "Initial VPS datacenter",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		Labels:          initialVPSLabels,
		Note:            "Initial VPS note",
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset error type = %T", err)
	}

	t.Run("experience log", func(t *testing.T) {
		repo := NewPostgresRenewalDecisionRepository(pool)
		input := renewals.CreateExperienceLogInput{
			VPSID:    vps.VPSID,
			Category: renewals.ExperienceNetwork,
			Severity: renewals.ExperienceSeverityWarning,
			Summary:  "lost-response packet loss",
			Details:  "private integration detail",
		}
		first, replayed, err := repo.CreateExperienceLogIdempotent(ctx, input, "lost-response-experience-001")
		if err != nil {
			t.Fatalf("first experience create error type = %T", err)
		}
		if replayed {
			t.Fatal("first experience create unexpectedly replayed")
		}
		second, replayed, err := repo.CreateExperienceLogIdempotent(ctx, input, "lost-response-experience-001")
		if err != nil {
			t.Fatalf("experience replay error type = %T", err)
		}
		if !replayed || second.ExperienceLogID != first.ExperienceLogID {
			t.Fatalf("experience replayed = %t, result ID match = %t", replayed, second.ExperienceLogID == first.ExperienceLogID)
		}
		assertIdempotentRowCounts(t, ctx, pool,
			`select count(*) from experience_logs where vps_id = $1`, vps.VPSID,
			`select count(*) from experience_log_create_idempotency where idempotency_key = $1`, "lost-response-experience-001",
		)
	})

	var serviceID string
	t.Run("asset service", func(t *testing.T) {
		repo := NewPostgresAssetServiceRepository(pool)
		input := assetservices.CreateInput{
			VPSID:       vps.VPSID,
			Name:        "Lost response API",
			ServiceType: assetservices.ServiceTypeAPI,
			Status:      assetservices.ServiceStatusActive,
			Note:        "private integration note",
		}
		first, replayed, err := repo.CreateAssetServiceIdempotent(ctx, input, "lost-response-service-001")
		if err != nil {
			t.Fatalf("first service create error type = %T", err)
		}
		if replayed {
			t.Fatal("first service create unexpectedly replayed")
		}
		second, replayed, err := repo.CreateAssetServiceIdempotent(ctx, input, "lost-response-service-001")
		if err != nil {
			t.Fatalf("service replay error type = %T", err)
		}
		if !replayed || second.ServiceID != first.ServiceID {
			t.Fatalf("service replayed = %t, result ID match = %t", replayed, second.ServiceID == first.ServiceID)
		}
		serviceID = first.ServiceID
		assertIdempotentRowCounts(t, ctx, pool,
			`select count(*) from asset_services where vps_id = $1`, vps.VPSID,
			`select count(*) from asset_service_create_idempotency where idempotency_key = $1`, "lost-response-service-001",
		)
	})

	t.Run("asset domain", func(t *testing.T) {
		repo := NewPostgresAssetDomainRepository(pool)
		input := assetdomains.CreateInput{
			VPSID:      vps.VPSID,
			ServiceID:  &serviceID,
			DomainName: "lost-response.example.com",
			Status:     assetdomains.DomainStatusActive,
			Note:       "private integration note",
		}
		first, replayed, err := repo.CreateAssetDomainIdempotent(ctx, input, "lost-response-domain-001")
		if err != nil {
			t.Fatalf("first domain create error type = %T", err)
		}
		if replayed {
			t.Fatal("first domain create unexpectedly replayed")
		}
		second, replayed, err := repo.CreateAssetDomainIdempotent(ctx, input, "lost-response-domain-001")
		if err != nil {
			t.Fatalf("domain replay error type = %T", err)
		}
		if !replayed || second.DomainID != first.DomainID {
			t.Fatalf("domain replayed = %t, result ID match = %t", replayed, second.DomainID == first.DomainID)
		}
		assertIdempotentRowCounts(t, ctx, pool,
			`select count(*) from asset_domains where vps_id = $1`, vps.VPSID,
			`select count(*) from asset_domain_create_idempotency where idempotency_key = $1`, "lost-response-domain-001",
		)
	})

	t.Run("linked monitoring instance", func(t *testing.T) {
		repo := NewPostgresMonitoringInstanceRepository(pool)
		request := monitoringinstances.LinkedCreateWireIdentity{
			Group: "integration-group",
		}
		first, firstLink, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, vps.VPSID, request, "lost-response-monitoring-001")
		if err != nil {
			t.Fatalf("first monitoring create error type = %T", err)
		}
		if replayed {
			t.Fatal("first monitoring create unexpectedly replayed")
		}
		firstDefaultsMatch := first.DisplayName == "Initial VPS display" &&
			first.Group == "integration-group" &&
			first.Region == "Initial VPS region" &&
			first.City == "Initial VPS city" &&
			first.Provider == "Initial VPS provider" &&
			first.LifecycleStatus == monitoringinstances.LifecyclePendingEnrollment &&
			reflect.DeepEqual(first.Labels, initialVPSLabels) &&
			first.Note == "Initial VPS note" &&
			firstLink.VPSID == vps.VPSID &&
			firstLink.MonitoringInstanceID == first.MonitoringInstanceID &&
			firstLink.Note == "created from vps detail"
		if !firstDefaultsMatch {
			t.Fatalf("first monitoring create derived expected defaults = %t", firstDefaultsMatch)
		}

		if _, err := pool.Exec(ctx, `
			update vps_assets
			set display_name = $2,
				provider_name = $3,
				country = $4,
				region = $5,
				city = $6,
				datacenter = $7,
				labels = $8,
				note = $9
			where vps_id = $1`,
			vps.VPSID,
			"Changed VPS display",
			"Changed VPS provider",
			"Changed VPS country",
			"Changed VPS region",
			"Changed VPS city",
			"Changed VPS datacenter",
			[]string{"changed-default"},
			"Changed VPS note",
		); err != nil {
			t.Fatalf("mutate VPS defaults error type = %T", err)
		}

		second, secondLink, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, vps.VPSID, request, "lost-response-monitoring-001")
		if err != nil {
			t.Fatalf("monitoring replay error type = %T", err)
		}
		if !replayed || second.MonitoringInstanceID != first.MonitoringInstanceID || secondLink.LinkID != firstLink.LinkID {
			t.Fatalf(
				"monitoring replayed = %t, result/link ID matches = %t/%t",
				replayed,
				second.MonitoringInstanceID == first.MonitoringInstanceID,
				secondLink.LinkID == firstLink.LinkID,
			)
		}
		storedDefaultsRemain := second.DisplayName == first.DisplayName &&
			second.Group == first.Group &&
			second.Region == first.Region &&
			second.City == first.City &&
			second.Provider == first.Provider &&
			second.LifecycleStatus == first.LifecycleStatus &&
			reflect.DeepEqual(second.Labels, first.Labels) &&
			second.Note == first.Note &&
			secondLink.Note == firstLink.Note
		if !storedDefaultsRemain {
			t.Fatalf("monitoring replay preserved first stored defaults = %t", storedDefaultsRemain)
		}
		assertIdempotentRowCounts(t, ctx, pool,
			`select count(*) from monitoring_instances where monitoring_instance_id = $1`, first.MonitoringInstanceID,
			`select count(*) from vps_monitoring_instance_create_idempotency where idempotency_key = $1`, "lost-response-monitoring-001",
		)
		var monitoringInstanceCount int
		if err := pool.QueryRow(ctx, `select count(*) from monitoring_instances`).Scan(&monitoringInstanceCount); err != nil {
			t.Fatalf("count all monitoring instances error type = %T", err)
		}
		if monitoringInstanceCount != 1 {
			t.Fatalf("monitoring instance rows = %d, want 1", monitoringInstanceCount)
		}
		var linkCount int
		if err := pool.QueryRow(ctx, `select count(*) from vps_monitoring_instance_links where vps_id = $1`, vps.VPSID).Scan(&linkCount); err != nil {
			t.Fatalf("count monitoring links error type = %T", err)
		}
		if linkCount != 1 {
			t.Fatalf("monitoring link rows = %d, want 1", linkCount)
		}
	})
}

func assertIdempotentRowCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	resultSQL string,
	resultArg any,
	receiptSQL string,
	receiptArg any,
) {
	t.Helper()
	var resultCount int
	if err := pool.QueryRow(ctx, resultSQL, resultArg).Scan(&resultCount); err != nil {
		t.Fatalf("count result rows error type = %T", err)
	}
	if resultCount != 1 {
		t.Fatalf("result rows = %d, want 1", resultCount)
	}
	var receiptCount int
	if err := pool.QueryRow(ctx, receiptSQL, receiptArg).Scan(&receiptCount); err != nil {
		t.Fatalf("count receipt rows error type = %T", err)
	}
	if receiptCount != 1 {
		t.Fatalf("receipt rows = %d, want 1", receiptCount)
	}
}

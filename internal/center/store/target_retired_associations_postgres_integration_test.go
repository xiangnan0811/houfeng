package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetrelations"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresIntegrationRetiredTargetRejectsCurrentAssociationsWithoutOrphans(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vr := NewPostgresVPSAssetRepository(pool)
	sr := NewPostgresAssetServiceRepository(pool)
	dr := NewPostgresAssetDomainRepository(pool)
	rr := NewPostgresAssetRelationRepository(pool)
	tr := NewPostgresTargetRepository(pool)
	owner := createVPSStateRepairCancellationVPS(t, ctx, vr, "target association owner", vpsassets.LifecycleActive)
	other := createVPSStateRepairCancellationVPS(t, ctx, vr, "other association owner", vpsassets.LifecycleActive)
	target := createVPSStateRepairCancellationTarget(t, ctx, tr, "association-retirement")
	if _, err := tr.ArchiveTarget(ctx, target.TargetID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"active", "paused", "retired", "unknown"} {
		t.Run(status, func(t *testing.T) {
			serviceInput := assetservices.CreateInput{VPSID: owner.VPSID, Name: "rejected service " + status, Status: assetservices.ServiceStatus(status), TargetID: &target.TargetID}
			if _, err := sr.CreateAssetService(ctx, serviceInput); !errors.Is(err, targets.ErrTargetMetadataConflict) {
				t.Fatalf("retired Target service create error = %v, want typed target conflict", err)
			}
			if _, _, err := sr.CreateAssetServiceIdempotent(ctx, serviceInput, "retired-service-"+status); !errors.Is(err, targets.ErrTargetMetadataConflict) {
				t.Fatalf("idempotent service create error = %v, want typed target conflict", err)
			}
			domainInput := assetdomains.CreateInput{VPSID: owner.VPSID, DomainName: status + ".rejected.example.test", Status: assetdomains.DomainStatus(status), TargetID: &target.TargetID}
			if _, err := dr.CreateAssetDomain(ctx, domainInput); !errors.Is(err, targets.ErrTargetMetadataConflict) {
				t.Fatalf("retired Target domain create error = %v, want typed target conflict", err)
			}
			if _, _, err := dr.CreateAssetDomainIdempotent(ctx, domainInput, "retired-domain-"+status); !errors.Is(err, targets.ErrTargetMetadataConflict) {
				t.Fatalf("idempotent domain create error = %v, want typed target conflict", err)
			}
		})
	}
	var objects, associations int
	if err := pool.QueryRow(ctx, `select (select count(*) from asset_services)+(select count(*) from asset_domains), (select count(*) from asset_service_associations)+(select count(*) from asset_domain_associations)`).Scan(&objects, &associations); err != nil {
		t.Fatal(err)
	}
	if objects != 0 || associations != 0 {
		t.Fatalf("rejected creates left %d objects and %d associations", objects, associations)
	}
	service, err := sr.CreateAssetService(ctx, assetservices.CreateInput{VPSID: owner.VPSID, Name: "existing service", Status: assetservices.ServiceStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	domain, err := dr.CreateAssetDomain(ctx, assetdomains.CreateInput{VPSID: owner.VPSID, DomainName: "existing.example.test", Status: assetdomains.DomainStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ kind, id string }{{assetrelations.Service, service.ServiceID}, {assetrelations.Domain, domain.DomainID}} {
		if _, err := rr.Link(ctx, other.VPSID, item.kind, assetrelations.LinkInput{ObjectID: item.id, TargetID: &target.TargetID}, "operator"); !errors.Is(err, assetrelations.ErrConflict) {
			t.Fatalf("existing %s link error = %v, want association conflict", item.kind, err)
		}
		links, err := rr.List(ctx, other.VPSID, item.kind, true)
		if err != nil || len(links) != 0 {
			t.Fatalf("rejected %s link persisted: %+v, %v", item.kind, links, err)
		}
	}
}

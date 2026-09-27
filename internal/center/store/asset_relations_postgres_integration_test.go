package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetrelations"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsfollowups"
)

func TestSharedAssociationTargetDirectControlsRequireConfirmationPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vr := NewPostgresVPSAssetRepository(pool)
	sr := NewPostgresAssetServiceRepository(pool)
	rr := NewPostgresAssetRelationRepository(pool)
	tr := NewPostgresTargetRepository(pool)
	a, err := vr.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "shared source"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := vr.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "shared destination"})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pause", "maintenance", "archive"} {
		t.Run(action, func(t *testing.T) {
			target := createVPSStateRepairDependencyStatusTarget(t, ctx, pool, targets.RunStatusEnabled)
			service, err := sr.CreateAssetService(ctx, assetservices.CreateInput{VPSID: a.VPSID, Name: "shared " + action, Status: assetservices.ServiceStatusPaused, TargetID: &target.TargetID})
			if err != nil {
				t.Fatal(err)
			}
			_, err = rr.Link(ctx, b.VPSID, assetrelations.Service, assetrelations.LinkInput{ObjectID: service.ServiceID, TargetID: &target.TargetID}, "operator")
			if err != nil {
				t.Fatal(err)
			}
			review, err := tr.GetTargetLifecycleReview(ctx, target.TargetID)
			if err != nil {
				t.Fatal(err)
			}
			if len(review.DependencyImpacts) != 2 {
				t.Fatalf("new association dependencies missing: %+v", review.DependencyImpacts)
			}
			for _, impact := range review.DependencyImpacts {
				if impact.RelationStatus != "current" || impact.RelationID == service.ServiceID || impact.Classification != assetlinks.DependencyCurrent {
					t.Fatalf("object state substituted for association state: %+v", impact)
				}
			}
			if _, err = tr.runTargetLifecycleAction(ctx, target.TargetID, action); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
				t.Fatalf("unconfirmed shared %s accepted: %v", action, err)
			}
			if _, err = tr.runTargetLifecycleAction(ctx, target.TargetID, action, assetlinks.GlobalActionConfirmation{PreviewDigest: review.PreviewDigest}); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
				t.Fatalf("digest without confirmation accepted: %v", err)
			}
			updated, err := tr.runTargetLifecycleAction(ctx, target.TargetID, action, assetlinks.GlobalActionConfirmation{PreviewDigest: review.PreviewDigest, ConfirmSharedImpact: true})
			if err != nil {
				t.Fatal(err)
			}
			if action == "archive" && updated.LifecycleStatus != "retired" {
				t.Fatalf("archive control failed to retire: %+v", updated)
			}
		})
	}
}

func TestAssetRelationsSharedIdentityArchiveAndHistoryPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	a, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "source"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "destination"})
	if err != nil {
		t.Fatal(err)
	}
	sr := NewPostgresAssetServiceRepository(pool)
	dr := NewPostgresAssetDomainRepository(pool)
	rr := NewPostgresAssetRelationRepository(pool)
	service, err := sr.CreateAssetService(ctx, assetservices.CreateInput{VPSID: a.VPSID, Name: "shared", URL: "https://original.test"})
	if err != nil {
		t.Fatal(err)
	}
	domain, err := dr.CreateAssetDomain(ctx, assetdomains.CreateInput{VPSID: a.VPSID, DomainName: "shared.example.test", ServiceID: &service.ServiceID})
	if err != nil {
		t.Fatal(err)
	}
	// A domain can reference a service only where that service is currently hosted.
	_, err = rr.Link(ctx, b.VPSID, assetrelations.Domain, assetrelations.LinkInput{ObjectID: domain.DomainID, ServiceID: &service.ServiceID}, "operator")
	if !errors.Is(err, assetrelations.ErrConflict) {
		t.Fatalf("domain without destination service = %v", err)
	}
	serviceLink, err := rr.Link(ctx, b.VPSID, assetrelations.Service, assetrelations.LinkInput{ObjectID: service.ServiceID, Address: "https://destination.test"}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = rr.Link(ctx, b.VPSID, assetrelations.Service, assetrelations.LinkInput{ObjectID: service.ServiceID}, "operator")
	if !errors.Is(err, assetrelations.ErrConflict) {
		t.Fatalf("duplicate link = %v", err)
	}
	_, err = rr.Link(ctx, b.VPSID, assetrelations.Domain, assetrelations.LinkInput{ObjectID: domain.DomainID, ServiceID: &service.ServiceID}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatal(err)
	}
	if err = archiveVPSRelations(ctx, tx, a.VPSID, "resource ended"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update vps_assets set lifecycle_status='archived',archived_at=now() where vps_id=$1`, a.VPSID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := rr.List(ctx, a.VPSID, assetrelations.Service, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].EndedAt == nil || history[0].EndReason != "resource ended" {
		t.Fatalf("history=%+v", history)
	}
	snapshot := string(history[0].Snapshot)
	current, err := rr.List(ctx, b.VPSID, assetrelations.Service, true)
	if err != nil || len(current) != 1 || current[0].AssociationID != serviceLink.AssociationID {
		t.Fatalf("other VPS affected: %+v %v", current, err)
	}
	listed, err := sr.ListAssetServicesForVPS(ctx, b.VPSID)
	if err != nil || len(listed) != 1 || listed[0].URL != "https://destination.test" || listed[0].ServiceID != service.ServiceID {
		t.Fatalf("placement projection: %+v %v", listed, err)
	}
	// Object status and metadata remain independently editable after one host archives.
	if _, err = sr.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusPaused, "global pause"); err != nil {
		t.Fatal(err)
	}
	if _, err = sr.UpdateStatus(ctx, service.ServiceID, assetservices.ServiceStatusActive, "resume independent service"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update asset_services set name='new name' where service_id=$1`, service.ServiceID); err != nil {
		t.Fatal(err)
	}
	history, err = rr.List(ctx, a.VPSID, assetrelations.Service, false)
	if err != nil || string(history[0].Snapshot) != snapshot {
		t.Fatalf("ended snapshot changed: %v", err)
	}
	if _, err = rr.Link(ctx, a.VPSID, assetrelations.Service, assetrelations.LinkInput{ObjectID: service.ServiceID}, "operator"); !errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
		t.Fatalf("archive accepted new link: %v", err)
	}
	// Both archive and restore leave ended placements historical. The ordinary
	// scoped endpoints are also the canonical overview's source, so neither may
	// make the still-active shared object look hosted here again.
	overview := &VPSOverviewRepository{services: sr, domains: dr}
	assertCurrentRelations := func(vpsID string, want int) {
		t.Helper()
		services, err := sr.ListAssetServicesForVPS(ctx, vpsID)
		if err != nil || len(services) != want {
			t.Fatalf("current services %s = %+v, %v; want %d", vpsID, services, err, want)
		}
		domains, err := dr.ListAssetDomainsForVPS(ctx, vpsID)
		if err != nil || len(domains) != want {
			t.Fatalf("current domains %s = %+v, %v; want %d", vpsID, domains, err, want)
		}
		serviceOverview, err := overview.LoadServiceRelation(ctx, vpsID)
		if err != nil || serviceOverview.Count != want {
			t.Fatalf("overview services %s = %+v, %v; want %d", vpsID, serviceOverview, err, want)
		}
		domainOverview, err := overview.LoadDomainRelation(ctx, vpsID)
		if err != nil || domainOverview.Count != want {
			t.Fatalf("overview domains %s = %+v, %v; want %d", vpsID, domainOverview, err, want)
		}
	}
	assertCurrentRelations(a.VPSID, 0)
	assertCurrentRelations(b.VPSID, 1)
	if _, err = NewPostgresAssetLifecycleRepository(pool).RestoreVPSFromArchive(ctx, a.VPSID, assetlifecycle.RestoreArchiveInput{Reason: "restore without reopening relations"}); err != nil {
		t.Fatal(err)
	}
	assertCurrentRelations(a.VPSID, 0)
	assertCurrentRelations(b.VPSID, 1)
	for _, kind := range []string{assetrelations.Service, assetrelations.Domain} {
		ended, err := rr.List(ctx, a.VPSID, kind, false)
		if err != nil || len(ended) != 1 || ended[0].EndedAt == nil {
			t.Fatalf("%s history lost after restore: %+v %v", kind, ended, err)
		}
	}
	if _, err = rr.End(ctx, b.VPSID, assetrelations.Service, history[0].AssociationID, "wrong scope", "operator"); !errors.Is(err, assetrelations.ErrNotFound) {
		t.Fatalf("cross-VPS end=%v", err)
	}
	if _, err = rr.End(ctx, b.VPSID, assetrelations.Service, serviceLink.AssociationID, "migration complete", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err = rr.End(ctx, b.VPSID, assetrelations.Service, serviceLink.AssociationID, "repeat", "operator"); !errors.Is(err, assetrelations.ErrConflict) {
		t.Fatalf("repeat end=%v", err)
	}
	identities, err := sr.ListAssetServices(ctx, assetservices.ListFilters{})
	if err != nil || len(identities) != 1 {
		t.Fatalf("historical identity disappeared: %+v %v", identities, err)
	}
}

func TestVPSFollowupsDedupeResolutionAndArchiveRollbackPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps, err := NewPostgresVPSAssetRepository(pool).CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "followups"})
	if err != nil {
		t.Fatal(err)
	}
	sr := NewPostgresAssetServiceRepository(pool)
	_, err = sr.CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, Name: "retained"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatal(err)
	}
	if err = archiveVPSRelations(ctx, tx, vps.VPSID, "rollback"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	rr := NewPostgresAssetRelationRepository(pool)
	current, err := rr.List(ctx, vps.VPSID, assetrelations.Service, true)
	if err != nil || len(current) != 1 {
		t.Fatalf("rollback left partial end: %+v %v", current, err)
	}
	fr := NewPostgresVPSFollowupRepository(pool)
	for i := 0; i < 3; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = upsertVPSFollowup(ctx, tx, vps.VPSID, "archived_online", "live", "review running agent", json.RawMessage(`{"session_id":"session_a"}`)); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	records, err := fr.List(ctx, vps.VPSID)
	if err != nil || len(records) != 1 {
		t.Fatalf("dedupe: %+v %v", records, err)
	}
	id := records[0].FollowupID
	if _, err = fr.Resolve(ctx, vps.VPSID, id, vpsfollowups.ResolveInput{Status: "ignored", Reason: ""}, "user"); !errors.Is(err, vpsfollowups.ErrInvalid) {
		t.Fatalf("missing reason = %v", err)
	}
	closed, err := fr.Resolve(ctx, vps.VPSID, id, vpsfollowups.ResolveInput{Status: "resolved", Reason: "Agent removed"}, "user_1")
	if err != nil {
		t.Fatal(err)
	}
	if closed.ResolutionReason != "Agent removed" || closed.ResolvedBy != "user_1" || closed.ResolvedAt == nil {
		t.Fatalf("resolution lacks evidence: %+v", closed)
	}
	if _, err = fr.Resolve(ctx, vps.VPSID, id, vpsfollowups.ResolveInput{Status: "ignored", Reason: "overwrite"}, "user_2"); !errors.Is(err, vpsfollowups.ErrConflict) {
		t.Fatalf("resolution overwritten: %v", err)
	}
	// Continued heartbeats after dismissal do not create another reminder for
	// the same archive episode; a distinct later episode does.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = upsertVPSFollowup(ctx, tx, vps.VPSID, "archived_online", "live", "continued heartbeat", nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	records, err = fr.List(ctx, vps.VPSID)
	if err != nil || len(records) != 1 || records[0].Status != "resolved" {
		t.Fatalf("closed occurrence respawned: %+v %v", records, err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = upsertVPSFollowup(ctx, tx, vps.VPSID, "archived_online", "new-archive-episode", "new heartbeat", nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	records, err = fr.List(ctx, vps.VPSID)
	if err != nil || len(records) != 2 {
		t.Fatalf("new occurrence not recorded: %+v %v", records, err)
	}
	if _, err = pool.Exec(ctx, `update vps_assets set lifecycle_status='archived',archived_at=now() where vps_id=$1`, vps.VPSID); err != nil {
		t.Fatal(err)
	}
	annotation, err := fr.Create(ctx, vps.VPSID, vpsfollowups.CreateInput{Kind: "migration", Summary: "Result added", Details: json.RawMessage(`{"source":"old","target":"new","result":"complete","created_by":"spoof"}`)}, "user_1")
	if err != nil {
		t.Fatal(err)
	}
	var details map[string]any
	if err = json.Unmarshal(annotation.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["created_by"] != "user_1" {
		t.Fatal("untrusted actor accepted")
	}
}

package assetlifecycle

import (
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestDigestCancellationPreviewTracksCancellationFacts(t *testing.T) {
	base := digestPreviewFixture()
	original := DigestCancellationPreview(base)
	changedDate := subscriptions.NewDate(time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC))
	changedTime := time.Date(2026, time.July, 1, 9, 30, 0, 0, time.UTC)
	changedString := "changed"

	tests := []struct {
		name   string
		change func(*CancellationPreview)
	}{
		{"VPS ID", func(p *CancellationPreview) { p.VPS.VPSID = "vps_002" }},
		{"VPS lifecycle status", func(p *CancellationPreview) { p.VPS.LifecycleStatus = vpsassets.LifecycleCancelled }},
		{"VPS usage status", func(p *CancellationPreview) { p.VPS.UsageStatus = vpsassets.UsageIdle }},
		{"VPS renewal decision", func(p *CancellationPreview) { p.VPS.RenewalDecision = vpsassets.RenewalCancel }},
		{"VPS archive fact", func(p *CancellationPreview) { p.VPS.ArchivedAt = &changedTime }},
		{"subscription ID", func(p *CancellationPreview) { p.Subscriptions[0].Record.SubscriptionID = "sub_003" }},
		{"subscription VPS ID", func(p *CancellationPreview) { p.Subscriptions[0].Record.VPSID = "vps_002" }},
		{"subscription started_at", func(p *CancellationPreview) { p.Subscriptions[0].Record.StartedAt = &changedDate }},
		{"subscription ends_at", func(p *CancellationPreview) { p.Subscriptions[0].Record.EndsAt = &changedDate }},
		{"subscription renew_at", func(p *CancellationPreview) { p.Subscriptions[0].Record.RenewAt = &changedDate }},
		{"subscription trial_ends_at", func(p *CancellationPreview) { p.Subscriptions[0].Record.TrialEndsAt = &changedDate }},
		{"subscription status", func(p *CancellationPreview) { p.Subscriptions[0].Record.Status = subscriptions.StatusCancelled }},
		{"subscription renewal mode", func(p *CancellationPreview) { p.Subscriptions[0].Record.RenewalMode = "manual" }},
		{"subscription auto renew", func(p *CancellationPreview) { p.Subscriptions[0].Record.AutoRenew = false }},
		{"subscription auto renew cancelled", func(p *CancellationPreview) { p.Subscriptions[0].Record.AutoRenewCancelled = true }},
		{"monitoring instance ID", func(p *CancellationPreview) { p.MonitoringInstanceLinks[0].MonitoringInstanceID = "mi_003" }},
		{"monitoring instance lifecycle", func(p *CancellationPreview) { p.MonitoringInstanceLinks[0].LifecycleStatus = "retired" }},
		{"monitoring status", func(p *CancellationPreview) { p.MonitoringInstanceLinks[0].MonitoringStatus = "paused" }},
		{"monitoring binding status", func(p *CancellationPreview) { p.MonitoringInstanceLinks[0].BindingStatus = "unlinked" }},
		{"monitoring instance archive fact", func(p *CancellationPreview) { p.MonitoringInstanceLinks[0].ArchivedAt = &changedTime }},
		{"dependency object type", func(p *CancellationPreview) { p.DependencyImpacts[0].ObjectType = "target" }},
		{"dependency object ID", func(p *CancellationPreview) { p.DependencyImpacts[0].ObjectID = "mi_003" }},
		{"dependency VPS ID", func(p *CancellationPreview) { p.DependencyImpacts[0].VPSID = "vps_002" }},
		{"dependency VPS lifecycle", func(p *CancellationPreview) { p.DependencyImpacts[0].VPSLifecycleStatus = "cancelled" }},
		{"dependency relation type", func(p *CancellationPreview) { p.DependencyImpacts[0].RelationType = "asset_domain" }},
		{"dependency relation ID", func(p *CancellationPreview) { p.DependencyImpacts[0].RelationID = "dom_003" }},
		{"dependency relation status", func(p *CancellationPreview) { p.DependencyImpacts[0].RelationStatus = "paused" }},
		{"dependency classification", func(p *CancellationPreview) { p.DependencyImpacts[0].Classification = assetlinks.DependencyHistorical }},
		{"service ID", func(p *CancellationPreview) { p.Services[0].ServiceID = "svc_003" }},
		{"service VPS ID", func(p *CancellationPreview) { p.Services[0].VPSID = "vps_002" }},
		{"service target ID", func(p *CancellationPreview) { p.Services[0].TargetID = &changedString }},
		{"service status", func(p *CancellationPreview) { p.Services[0].Status = assetservices.ServiceStatus("paused") }},
		{"domain ID", func(p *CancellationPreview) { p.Domains[0].DomainID = "dom_003" }},
		{"domain VPS ID", func(p *CancellationPreview) { p.Domains[0].VPSID = "vps_002" }},
		{"domain service ID", func(p *CancellationPreview) { p.Domains[0].ServiceID = &changedString }},
		{"domain target ID", func(p *CancellationPreview) { p.Domains[0].TargetID = &changedString }},
		{"domain status", func(p *CancellationPreview) { p.Domains[0].Status = assetdomains.DomainStatus("paused") }},
		{"target ID", func(p *CancellationPreview) { p.TargetLinks[0].TargetID = "target_003" }},
		{"target state", func(p *CancellationPreview) { p.TargetLinks[0].RunStatus = "paused" }},
		{"target service association", func(p *CancellationPreview) { p.TargetLinks[0].ServiceIDs[0] = "svc_003" }},
		{"target domain association", func(p *CancellationPreview) { p.TargetLinks[0].DomainIDs[0] = "dom_003" }},
		{"recommended step object type", func(p *CancellationPreview) { p.RecommendedSteps[0].ObjectType = "target" }},
		{"recommended step object ID", func(p *CancellationPreview) { p.RecommendedSteps[0].ObjectID = "target_003" }},
		{"recommended step type", func(p *CancellationPreview) { p.RecommendedSteps[0].StepType = "retire" }},
		{"recommended step from state", func(p *CancellationPreview) { p.RecommendedSteps[0].FromState = "active" }},
		{"recommended step to state", func(p *CancellationPreview) { p.RecommendedSteps[0].ToState = "cancelled" }},
		{"recommended step required", func(p *CancellationPreview) { p.RecommendedSteps[0].Required = false }},
		{"evaluated_on", func(p *CancellationPreview) { p.EvaluatedOn = changedDate }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preview := digestPreviewFixture()
			test.change(&preview)
			if got := DigestCancellationPreview(preview); got == original {
				t.Fatal("digest must change when a cancellation fact changes")
			}
		})
	}
}

func TestDigestCancellationPreviewIgnoresUpdateAndTelemetryChanges(t *testing.T) {
	base := digestPreviewFixture()
	changed := digestPreviewFixture()
	later := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	changed.VPS.UpdatedAt = later
	changed.Subscriptions[0].Record.UpdatedAt = later
	changed.MonitoringInstanceLinks[0].LastHeartbeatAt = &later
	changed.MonitoringInstanceLinks[0].LastSyncAt = &later
	changed.MonitoringInstanceLinks[0].CurrentHealthStatus = "unhealthy"
	changed.MonitoringInstanceLinks[0].CurrentActiveIncidentCount = 99
	changed.MonitoringInstanceLinks[0].CurrentPrimaryIssueSummary = "different incident"
	changed.Services[0].UpdatedAt = later
	changed.Domains[0].UpdatedAt = later
	changed.TargetLinks[0].LastLinkedAt = &later
	changed.VPS.SSHHost = "secret-host"
	changed.VPS.SSHUser = "secret-user"
	changed.VPS.SSHPort = 2200
	changed.Services[0].URL = "https://changed.example"
	changed.RecommendedSteps[0].Message = "different wording"

	if DigestCancellationPreview(changed) != DigestCancellationPreview(base) {
		t.Fatal("digest must ignore generic UpdatedAt, heartbeat, health, telemetry, and connection details")
	}
}

func TestDigestCancellationPreviewIsOrderIndependentWithoutMutatingInputs(t *testing.T) {
	base := digestPreviewFixture()
	reordered := digestPreviewFixture()
	reverse := func(length int, swap func(int, int)) {
		for i, j := 0, length-1; i < j; i, j = i+1, j-1 {
			swap(i, j)
		}
	}
	reverse(len(reordered.Subscriptions), func(i, j int) {
		reordered.Subscriptions[i], reordered.Subscriptions[j] = reordered.Subscriptions[j], reordered.Subscriptions[i]
	})
	reverse(len(reordered.MonitoringInstanceLinks), func(i, j int) {
		reordered.MonitoringInstanceLinks[i], reordered.MonitoringInstanceLinks[j] = reordered.MonitoringInstanceLinks[j], reordered.MonitoringInstanceLinks[i]
	})
	reverse(len(reordered.DependencyImpacts), func(i, j int) {
		reordered.DependencyImpacts[i], reordered.DependencyImpacts[j] = reordered.DependencyImpacts[j], reordered.DependencyImpacts[i]
	})
	reverse(len(reordered.Services), func(i, j int) {
		reordered.Services[i], reordered.Services[j] = reordered.Services[j], reordered.Services[i]
	})
	reverse(len(reordered.Domains), func(i, j int) {
		reordered.Domains[i], reordered.Domains[j] = reordered.Domains[j], reordered.Domains[i]
	})
	reverse(len(reordered.TargetLinks), func(i, j int) {
		reordered.TargetLinks[i], reordered.TargetLinks[j] = reordered.TargetLinks[j], reordered.TargetLinks[i]
	})
	reverse(len(reordered.RecommendedSteps), func(i, j int) {
		reordered.RecommendedSteps[i], reordered.RecommendedSteps[j] = reordered.RecommendedSteps[j], reordered.RecommendedSteps[i]
	})
	for i := range reordered.TargetLinks {
		reverse(len(reordered.TargetLinks[i].ServiceIDs), func(a, b int) {
			reordered.TargetLinks[i].ServiceIDs[a], reordered.TargetLinks[i].ServiceIDs[b] =
				reordered.TargetLinks[i].ServiceIDs[b], reordered.TargetLinks[i].ServiceIDs[a]
		})
		reverse(len(reordered.TargetLinks[i].DomainIDs), func(a, b int) {
			reordered.TargetLinks[i].DomainIDs[a], reordered.TargetLinks[i].DomainIDs[b] =
				reordered.TargetLinks[i].DomainIDs[b], reordered.TargetLinks[i].DomainIDs[a]
		})
	}

	wantSubscriptions := append([]SubscriptionImpact(nil), base.Subscriptions...)
	wantMonitoringInstances := append([]assetlinks.MonitoringInstanceSummary(nil), base.MonitoringInstanceLinks...)
	wantDependencies := append([]assetlinks.DependencyImpact(nil), base.DependencyImpacts...)
	wantServices := append([]assetservices.Record(nil), base.Services...)
	wantDomains := append([]assetdomains.Record(nil), base.Domains...)
	wantTargets := append([]TargetImpact(nil), base.TargetLinks...)
	for i := range wantTargets {
		wantTargets[i].ServiceIDs = append([]string(nil), base.TargetLinks[i].ServiceIDs...)
		wantTargets[i].DomainIDs = append([]string(nil), base.TargetLinks[i].DomainIDs...)
	}
	wantSteps := append([]RecommendedLifecycleStep(nil), base.RecommendedSteps...)

	if got, want := DigestCancellationPreview(reordered), DigestCancellationPreview(base); got != want {
		t.Fatalf("digest must not depend on input order: got %s, want %s", got, want)
	}
	if !reflect.DeepEqual(base.Subscriptions, wantSubscriptions) ||
		!reflect.DeepEqual(base.MonitoringInstanceLinks, wantMonitoringInstances) ||
		!reflect.DeepEqual(base.DependencyImpacts, wantDependencies) ||
		!reflect.DeepEqual(base.Services, wantServices) ||
		!reflect.DeepEqual(base.Domains, wantDomains) ||
		!reflect.DeepEqual(base.TargetLinks, wantTargets) ||
		!reflect.DeepEqual(base.RecommendedSteps, wantSteps) {
		t.Fatal("digest must not mutate caller-owned slices")
	}
}

func TestDigestCancellationPreviewDoesNotCollideOnDelimiters(t *testing.T) {
	targetOne := "target"
	targetTwo := "vps|target"
	first := CancellationPreview{
		Services: []assetservices.Record{{
			ServiceID: "svc_001", Status: assetservices.ServiceStatus("active|vps"), TargetID: &targetOne,
		}},
	}
	second := CancellationPreview{
		Services: []assetservices.Record{{
			ServiceID: "svc_001", Status: assetservices.ServiceStatus("active"), TargetID: &targetTwo,
		}},
	}
	if DigestCancellationPreview(first) == DigestCancellationPreview(second) {
		t.Fatal("distinct service facts must not collide when values contain delimiters")
	}
}

func digestPreviewFixture() CancellationPreview {
	started := subscriptions.NewDate(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	ends := subscriptions.NewDate(time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC))
	renew := subscriptions.NewDate(time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC))
	trialEnds := subscriptions.NewDate(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC))
	evaluated := subscriptions.NewDate(time.Date(2026, time.May, 30, 0, 0, 0, 0, time.UTC))
	linkedAt := time.Date(2026, time.May, 20, 8, 0, 0, 0, time.UTC)
	heartbeatAt := time.Date(2026, time.May, 29, 8, 0, 0, 0, time.UTC)
	syncAt := time.Date(2026, time.May, 29, 9, 0, 0, 0, time.UTC)
	targetLinkedAt := time.Date(2026, time.May, 21, 8, 0, 0, 0, time.UTC)
	serviceTarget := "target_001"
	domainTarget := "target_002"
	domainService := "svc_001"
	archivedAt := time.Date(2026, time.May, 15, 8, 0, 0, 0, time.UTC)

	return CancellationPreview{
		VPS: vpsassets.Record{
			VPSID: "vps_001", LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus: vpsassets.UsageInUse, RenewalDecision: vpsassets.RenewalKeep,
			UpdatedAt: linkedAt,
		},
		Subscriptions: []SubscriptionImpact{
			{Record: subscriptions.Record{
				SubscriptionID: "sub_001", VPSID: "vps_001", StartedAt: &started,
				EndsAt: &ends, RenewAt: &renew, TrialEndsAt: &trialEnds,
				Status: subscriptions.StatusActive, RenewalMode: "auto",
				AutoRenew: true, UpdatedAt: linkedAt,
			}},
			{Record: subscriptions.Record{
				SubscriptionID: "sub_002", VPSID: "vps_001", Status: subscriptions.StatusPaused,
			}},
		},
		MonitoringInstanceLinks: []assetlinks.MonitoringInstanceSummary{
			{
				MonitoringInstanceID: "mi_001", LifecycleStatus: "active", MonitoringStatus: "enabled",
				BindingStatus: "linked", ArchivedAt: &archivedAt, CurrentHealthStatus: "healthy",
				CurrentActiveIncidentCount: 1, CurrentPrimaryIssueSummary: "normal",
				LastHeartbeatAt: &heartbeatAt, LastSyncAt: &syncAt, LinkedAt: linkedAt,
			},
			{MonitoringInstanceID: "mi_002", LifecycleStatus: "idle", MonitoringStatus: "paused", BindingStatus: "unlinked"},
		},
		DependencyImpacts: []assetlinks.DependencyImpact{
			{
				ObjectType: "monitoring_instance", ObjectID: "mi_001", VPSID: "vps_001",
				VPSLifecycleStatus: "active", RelationType: "monitoring_instance_link",
				RelationID: "link_001", RelationStatus: "active", Classification: assetlinks.DependencyCurrent,
			},
			{
				ObjectType: "asset_service", ObjectID: "svc_001", VPSID: "vps_001",
				VPSLifecycleStatus: "active", RelationType: "asset_service", RelationID: "svc_001",
				RelationStatus: "active", Classification: assetlinks.DependencyCurrent,
			},
		},
		Services: []assetservices.Record{
			{ServiceID: "svc_001", VPSID: "vps_001", TargetID: &serviceTarget, Status: assetservices.ServiceStatusActive, UpdatedAt: linkedAt},
			{ServiceID: "svc_002", VPSID: "vps_001", TargetID: &domainTarget, Status: assetservices.ServiceStatus("paused")},
		},
		Domains: []assetdomains.Record{
			{DomainID: "dom_001", VPSID: "vps_001", ServiceID: &domainService, TargetID: &domainTarget, Status: assetdomains.DomainStatusActive, UpdatedAt: linkedAt},
			{DomainID: "dom_002", VPSID: "vps_001", Status: assetdomains.DomainStatus("expired")},
		},
		TargetLinks: []TargetImpact{
			{
				TargetID: "target_001", RunStatus: "enabled", ServiceIDs: []string{"svc_001", "svc_002"},
				DomainIDs: []string{"dom_001", "dom_002"}, LastLinkedAt: &targetLinkedAt,
			},
			{TargetID: "target_002", RunStatus: "paused", ServiceIDs: []string{"svc_002"}, DomainIDs: []string{"dom_002"}},
		},
		RecommendedSteps: []RecommendedLifecycleStep{
			{
				ObjectType: ObjectTypeVPS, ObjectID: "vps_001", StepType: StepTypeVPSLifecycle,
				FromState: "active/keep", ToState: "to_cancel/cancel", Required: true, Message: "cancel VPS",
			},
			{
				ObjectType: "monitoring_instance", ObjectID: "mi_001", StepType: "retain",
				FromState: "enabled/linked", ToState: "paused", Message: "pause monitoring",
			},
		},
		EvaluatedOn: evaluated,
	}
}

package assetlifecycle

import (
	"testing"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestDigestCancellationPreviewChangesWithRelatedIDs(t *testing.T) {
	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	base := CancellationPreview{
		VPS: vpsassets.Record{
			VPSID: "vps_001", LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus: vpsassets.UsageInUse, UpdatedAt: now,
		},
	}
	first := DigestCancellationPreview(base)
	withSub := CancellationPreview{
		VPS: base.VPS,
		Subscriptions: []SubscriptionImpact{{
			Record: subscriptions.Record{SubscriptionID: "sub_001", Status: subscriptions.StatusActive},
		}},
	}
	if DigestCancellationPreview(withSub) == first {
		t.Fatal("digest must change when a related subscription appears")
	}
	later := base
	later.VPS.UpdatedAt = now.Add(time.Minute)
	if DigestCancellationPreview(later) == first {
		t.Fatal("digest must change when vps updated_at advances")
	}
	renew := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	withRenew := CancellationPreview{
		VPS: base.VPS,
		Subscriptions: []SubscriptionImpact{{
			Record: subscriptions.Record{
				SubscriptionID: "sub_001", Status: subscriptions.StatusActive, RenewAt: &renew,
			},
		}},
	}
	withoutRenew := CancellationPreview{
		VPS: base.VPS,
		Subscriptions: []SubscriptionImpact{{
			Record: subscriptions.Record{SubscriptionID: "sub_001", Status: subscriptions.StatusActive},
		}},
	}
	if DigestCancellationPreview(withRenew) == DigestCancellationPreview(withoutRenew) {
		t.Fatal("digest must change when subscription renew_at changes")
	}
	paused := assetlinks.MonitoringInstanceSummary{MonitoringInstanceID: "mi_001", LifecycleStatus: "在用", MonitoringStatus: "暂停"}
	enabled := paused
	enabled.MonitoringStatus = "启用"
	if DigestCancellationPreview(CancellationPreview{VPS: base.VPS, MonitoringInstanceLinks: []assetlinks.MonitoringInstanceSummary{paused}}) ==
		DigestCancellationPreview(CancellationPreview{VPS: base.VPS, MonitoringInstanceLinks: []assetlinks.MonitoringInstanceSummary{enabled}}) {
		t.Fatal("digest must change when monitoring_status changes")
	}
	kept := subscriptions.Record{SubscriptionID: "sub_001", Status: subscriptions.StatusActive, AutoRenew: true}
	cancelledFlags := kept
	cancelledFlags.AutoRenewCancelled = true
	if DigestCancellationPreview(CancellationPreview{VPS: base.VPS, Subscriptions: []SubscriptionImpact{{Record: kept}}}) ==
		DigestCancellationPreview(CancellationPreview{VPS: base.VPS, Subscriptions: []SubscriptionImpact{{Record: cancelledFlags}}}) {
		t.Fatal("digest must change when auto_renew_cancelled changes")
	}
	targetA, targetB := "tg_a", "tg_b"
	firstEdge := CancellationPreview{
		VPS: base.VPS,
		Services: []assetservices.Record{{
			ServiceID: "svc_001", Status: assetservices.ServiceStatusActive, TargetID: &targetA,
		}},
		Domains: []assetdomains.Record{{
			DomainID: "dom_001", Status: assetdomains.DomainStatusActive, TargetID: &targetB,
		}},
		TargetLinks: []TargetImpact{
			{TargetID: targetA, RunStatus: "启用", ServiceIDs: []string{"svc_001"}},
			{TargetID: targetB, RunStatus: "启用", DomainIDs: []string{"dom_001"}},
		},
	}
	swappedEdge := firstEdge
	swappedEdge.Services = []assetservices.Record{{
		ServiceID: "svc_001", Status: assetservices.ServiceStatusActive, TargetID: &targetB,
	}}
	swappedEdge.Domains = []assetdomains.Record{{
		DomainID: "dom_001", Status: assetdomains.DomainStatusActive, TargetID: &targetA,
	}}
	swappedEdge.TargetLinks = []TargetImpact{
		{TargetID: targetA, RunStatus: "启用", DomainIDs: []string{"dom_001"}},
		{TargetID: targetB, RunStatus: "启用", ServiceIDs: []string{"svc_001"}},
	}
	if DigestCancellationPreview(firstEdge) == DigestCancellationPreview(swappedEdge) {
		t.Fatal("digest must change when service/domain target edges are swapped")
	}
	beforeRenew := CancellationPreview{
		VPS: base.VPS,
		RecommendedSteps: []RecommendedLifecycleStep{{
			ObjectType: ObjectTypeVPS, ObjectID: "vps_001", StepType: StepTypeVPSLifecycle,
			FromState: "active/keep", ToState: "to_cancel/cancel", Required: true,
		}},
	}
	afterRenew := beforeRenew
	afterRenew.RecommendedSteps = []RecommendedLifecycleStep{{
		ObjectType: ObjectTypeVPS, ObjectID: "vps_001", StepType: StepTypeVPSLifecycle,
		FromState: "active/keep", ToState: "cancelled/cancel", Required: true,
	}}
	if DigestCancellationPreview(beforeRenew) == DigestCancellationPreview(afterRenew) {
		t.Fatal("digest must change when recommended lifecycle steps change")
	}
}

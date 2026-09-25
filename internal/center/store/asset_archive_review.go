package store

import (
	"fmt"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func archiveBlockerDetails(review assetlifecycle.ArchiveReview) []assetlifecycle.BlockerDetail {
	details := make([]assetlifecycle.BlockerDetail, 0)
	add := func(code, kind, id, name, state, resolution string) {
		details = append(details, assetlifecycle.BlockerDetail{Code: code, ObjectType: kind, ObjectID: id, DisplayName: name, CurrentState: state, BlockedAction: "archive", ResolutionAction: resolution})
	}
	if review.VPS.LifecycleStatus != vpsassets.LifecycleToCancel && review.VPS.LifecycleStatus != vpsassets.LifecycleCancelled {
		resolution := "cancellation"
		if review.VPS.LifecycleStatus == vpsassets.LifecycleArchived {
			resolution = "restore_from_archive"
		}
		add("vps_lifecycle_not_archivable", "vps", review.VPS.VPSID, review.VPS.DisplayName, string(review.VPS.LifecycleStatus), resolution)
	}
	for _, impact := range review.Subscriptions {
		if impact.Record.Status == subscriptions.StatusActive {
			add("active_subscription", "subscription", impact.Record.SubscriptionID, impact.Record.DisplayName, string(impact.Record.Status), "correct_billing_status")
		}
	}
	for _, link := range review.MonitoringInstanceLinks {
		if (link.LifecycleStatus != monitoringinstances.LifecycleNoRenewal && link.LifecycleStatus != monitoringinstances.LifecycleRetired) || link.MonitoringStatus != monitoringinstances.MonitoringPaused {
			resolution := "retire_or_pause"
			if link.ArchivedAt != nil {
				resolution = "restore_from_archive"
			}
			add("monitoring_instance_running", "monitoring_instance", link.MonitoringInstanceID, link.DisplayName, link.LifecycleStatus+"/"+link.MonitoringStatus, resolution)
		}
	}
	effectiveTargets := make(map[string]bool)
	classify := func(kind, id, name, status string, targetID *string) {
		classification := assetlinks.ClassifyDependency(string(review.VPS.LifecycleStatus), kind, status)
		if classification == assetlinks.DependencyNeedsConfirmation {
			add("dependency_needs_confirmation", kind, id, name, status, "correct_dependency_status")
		}
		if targetID != nil && (classification == assetlinks.DependencyCurrent || classification == assetlinks.DependencyResidual) {
			effectiveTargets[*targetID] = true
		}
	}
	for _, service := range review.Services {
		classify("service", service.ServiceID, service.Name, string(service.Status), service.TargetID)
	}
	for _, domain := range review.Domains {
		classify("domain", domain.DomainID, domain.DomainName, string(domain.Status), domain.TargetID)
	}
	for _, target := range review.TargetLinks {
		if effectiveTargets[target.TargetID] && target.RunStatus != targets.RunStatusPaused && target.RunStatus != targets.RunStatusArchived {
			add("target_running", "target", target.TargetID, target.Name, target.RunStatus, "pause_or_archive")
		}
	}
	return details
}

func archiveBlockerMessage(detail assetlifecycle.BlockerDetail) string {
	switch detail.Code {
	case "vps_lifecycle_not_archivable":
		return "只有待取消或已取消的 VPS 可以归档；已归档对象须通过专用恢复入口处理。"
	case "active_subscription":
		return fmt.Sprintf("active 订阅 %s 仍阻止归档，请核对账单状态。", detail.ObjectID)
	case "monitoring_instance_running":
		return fmt.Sprintf("MonitoringInstance %s 必须满足不续费/已退役且监控暂停。", detail.ObjectID)
	case "target_running":
		return fmt.Sprintf("有效依赖的 Target %s 尚未暂停或归档。", detail.ObjectID)
	default:
		return fmt.Sprintf("%s %s 状态待确认，请先纠正依赖状态。", detail.ObjectType, detail.ObjectID)
	}
}

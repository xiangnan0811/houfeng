package assetlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/subscriptions"
)

func DigestCancellationPreview(preview CancellationPreview) string {
	lines := []string{
		fmt.Sprintf(
			"vps|%s|%s|%s|%s|%s",
			preview.VPS.VPSID,
			preview.VPS.LifecycleStatus,
			preview.VPS.UsageStatus,
			preview.VPS.RenewalDecision,
			preview.VPS.UpdatedAt.UTC().Format(time.RFC3339Nano),
		),
	}
	for _, subscription := range preview.Subscriptions {
		lines = append(lines, fmt.Sprintf(
			"sub|%s|%s|%s|%t|%t",
			subscription.Record.SubscriptionID,
			subscription.Record.Status,
			dateToken(subscription.Record.RenewAt),
			subscription.Record.AutoRenew,
			subscription.Record.AutoRenewCancelled,
		))
	}
	for _, monitoringInstance := range preview.MonitoringInstanceLinks {
		lines = append(lines, fmt.Sprintf(
			"mi|%s|%s|%s",
			monitoringInstance.MonitoringInstanceID,
			monitoringInstance.LifecycleStatus,
			monitoringInstance.MonitoringStatus,
		))
	}
	for _, service := range preview.Services {
		lines = append(lines, fmt.Sprintf(
			"svc|%s|%s|%s",
			service.ServiceID,
			service.Status,
			optionalID(service.TargetID),
		))
	}
	for _, domain := range preview.Domains {
		lines = append(lines, fmt.Sprintf(
			"dom|%s|%s|%s",
			domain.DomainID,
			domain.Status,
			optionalID(domain.TargetID),
		))
	}
	for _, target := range preview.TargetLinks {
		lines = append(lines, fmt.Sprintf(
			"tgt|%s|%s|%s|%s",
			target.TargetID,
			target.RunStatus,
			joinIDs(target.ServiceIDs),
			joinIDs(target.DomainIDs),
		))
	}
	for _, step := range preview.RecommendedSteps {
		lines = append(lines, fmt.Sprintf(
			"step|%s|%s|%s|%s|%s|%t",
			step.ObjectType,
			step.ObjectID,
			step.StepType,
			step.FromState,
			step.ToState,
			step.Required,
		))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func dateToken(value *subscriptions.Date) string {
	if value == nil {
		return ""
	}
	return value.Time.UTC().Format("2006-01-02")
}

func optionalID(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func joinIDs(values []string) string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return strings.Join(copied, ",")
}

func AttachCancellationPreviewDigest(preview *CancellationPreview) {
	preview.PreviewDigest = DigestCancellationPreview(*preview)
}

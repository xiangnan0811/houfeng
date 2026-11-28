package store

import (
	"fmt"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/vpsassets"
)

func archiveBlockerDetails(review assetlifecycle.ArchiveReview) []assetlifecycle.BlockerDetail {
	details := []assetlifecycle.BlockerDetail{}
	if review.VPS.LifecycleStatus != vpsassets.LifecycleActive {
		details = append(details, assetlifecycle.BlockerDetail{Code: "vps_lifecycle_not_archivable", ObjectType: "vps", ObjectID: review.VPS.VPSID, DisplayName: review.VPS.DisplayName, CurrentState: string(review.VPS.LifecycleStatus), BlockedAction: "archive", ResolutionAction: "restore_from_archive"})
	}
	return details
}
func archiveBlockerMessage(detail assetlifecycle.BlockerDetail) string {
	switch detail.Code {
	case "vps_lifecycle_not_archivable":
		return "只有管理中的 VPS 可以结束使用并归档。"
	case "receiver_observation_unhealthy":
		return "Center 暂无法确认心跳接收服务正常，请等待恢复后重试。"
	case "offline_observation_evidence_missing":
		return "缺少可信心跳或会话签发时间，无法判断是否已满 180 分钟。"
	case "continuous_offline_window_incomplete":
		return "距最近一次可信心跳或会话签发尚未满 180 分钟。"
	case "recent_trusted_online_signal":
		return fmt.Sprintf("监控实例 %s 最近 180 分钟内仍有可信在线信号。", detail.ObjectID)
	default:
		return detail.Code
	}
}

package store

type collaborationPruneRevisionFollowersFunctionCommand struct {
	RecordID    string   `json:"record_id"`
	KeepUserIDs []string `json:"keep_user_ids"`
	FenceEpoch  int64    `json:"fence_epoch"`
}

type collaborationPruneNotificationRecipientsFunctionCommand struct {
	NotificationID string   `json:"notification_id"`
	RecordID       string   `json:"record_id"`
	KeepUserIDs    []string `json:"keep_user_ids"`
	FenceEpoch     int64    `json:"fence_epoch"`
}

package store

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const maxCollaborationDeleteCommandBytes = 64 * 1024

type collaborationPurgeFunctionCommand struct {
	OperationID     string `json:"operation_id"`
	ReservationID   string `json:"reservation_id"`
	ProjectID       string `json:"project_id"`
	RecordID        string `json:"record_id"`
	FenceEpoch      int64  `json:"fence_epoch"`
	LedgerSequence  int64  `json:"ledger_sequence"`
	LedgerEntryHash string `json:"ledger_entry_hash"`
}

type collaborationRemoveFollowerFunctionCommand struct {
	RecordID   string `json:"record_id"`
	UserID     string `json:"user_id"`
	Version    int64  `json:"version"`
	FenceEpoch int64  `json:"fence_epoch"`
}

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

func encodeCollaborationDeleteCommand(command any) ([]byte, error) {
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) == 0 || len(encoded) > maxCollaborationDeleteCommandBytes {
		return nil, fmt.Errorf("encode collaboration controlled delete command")
	}
	return encoded, nil
}

func encodeCollaborationLedgerHash(value [32]byte) string {
	return hex.EncodeToString(value[:])
}

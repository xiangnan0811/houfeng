//go:build linux

package deploy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"

	"golang.org/x/sys/unix"
)

type ordinaryRestorationRecord struct {
	Version              int    `json:"version"`
	OperationID          string `json:"operation_id"`
	IntentSHA256         string `json:"intent_sha256"`
	FenceInventorySHA256 string `json:"fence_inventory_sha256"`
	MaintenanceNetworkID string `json:"maintenance_network_id"`
}

type ordinaryReadyRecord struct {
	Version           int    `json:"version"`
	OperationID       string `json:"operation_id"`
	IntentSHA256      string `json:"intent_sha256"`
	RestorationSHA256 string `json:"restoration_sha256"`
}

func ordinaryPhaseIntentMatches(dir int, intent ordinaryUpgradeIntent, digest string) bool {
	if !fenceID.MatchString(digest) || ordinaryValidateIntent(intent) != nil {
		return false
	}
	stored, exists, err := ordinaryReadOperationFile(dir, "intent.json")
	canonical, encodeErr := json.Marshal(intent)
	return err == nil && exists && encodeErr == nil && bytes.Equal(stored, canonical) && ordinaryDigestHex(stored) == digest
}

func ordinaryReadRestoration(dir int, intent ordinaryUpgradeIntent, intentDigest string) (ordinaryRestorationRecord, []byte, bool, error) {
	var record ordinaryRestorationRecord
	if !ordinaryPhaseIntentMatches(dir, intent, intentDigest) {
		return record, nil, false, errOrdinaryUpgrade
	}
	body, exists, err := ordinaryReadOperationFile(dir, "restoring.json")
	if err != nil || !exists {
		return record, nil, exists, err
	}
	journal, err := json.Marshal(intent.Fence)
	if err != nil || fenceDecode(body, &record) != nil || record.Version != 1 || record.OperationID != intent.Request.OperationID || record.IntentSHA256 != intentDigest || record.FenceInventorySHA256 != ordinaryDigestHex(journal) || !fenceID.MatchString(record.MaintenanceNetworkID) {
		return ordinaryRestorationRecord{}, nil, false, errOrdinaryUpgrade
	}
	return record, body, true, nil
}

// Called only after the exact participant is joined, all of its backend
// registrations have settled and target database/files independently verify.
// This fact precedes network restoration so retries never rerun the fence.
func ordinaryRecordRestoration(dir int, intent ordinaryUpgradeIntent, intentDigest string, fence ComposeOfflineFence) error {
	journal, err := json.Marshal(intent.Fence)
	if err != nil || !ordinaryPhaseIntentMatches(dir, intent, intentDigest) || fence.RecoveryID != intent.Request.OperationID || fence.DatabaseContainerID != intent.Request.DatabaseContainerID || !fenceID.MatchString(fence.MaintenanceNetworkID) || ordinaryDigestHex(journal) != hex.EncodeToString(fence.InventoryDigest[:]) {
		return errOrdinaryUpgrade
	}
	expected := ordinaryRestorationRecord{Version: 1, OperationID: intent.Request.OperationID, IntentSHA256: intentDigest, FenceInventorySHA256: ordinaryDigestHex(journal), MaintenanceNetworkID: fence.MaintenanceNetworkID}
	body, err := json.Marshal(expected)
	if err != nil {
		return errOrdinaryUpgrade
	}
	old, exists, err := ordinaryReadOperationFile(dir, "restoring.json")
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !exists {
		if ordinaryPublishOperationFile(dir, "restoring.json", body) != nil {
			return errOrdinaryUpgrade
		}
		old, exists, err = ordinaryReadOperationFile(dir, "restoring.json")
	}
	if err != nil || !exists || !bytes.Equal(old, body) || unix.Fsync(dir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryReadReady(dir int, intent ordinaryUpgradeIntent, intentDigest string) (ordinaryReadyRecord, bool, error) {
	var ready ordinaryReadyRecord
	_, restoringBody, exists, err := ordinaryReadRestoration(dir, intent, intentDigest)
	if err != nil || !exists {
		return ready, false, errOrdinaryUpgrade
	}
	body, exists, err := ordinaryReadOperationFile(dir, "ready.json")
	if err != nil || !exists {
		return ready, exists, err
	}
	if fenceDecode(body, &ready) != nil || ready.Version != 1 || ready.OperationID != intent.Request.OperationID || ready.IntentSHA256 != intentDigest || ready.RestorationSHA256 != ordinaryDigestHex(restoringBody) {
		return ordinaryReadyRecord{}, false, errOrdinaryUpgrade
	}
	return ready, true, nil
}

// After exact network restoration and its readback, publish READY then change
// the disposition while controller lock and ordinary EX are both still held.
func ordinaryCommitReady(dirs []composeLaunchDirectory, guardFD, operationDir int, intent ordinaryUpgradeIntent, intentDigest string) error {
	if len(dirs) < 2 {
		return errOrdinaryUpgrade
	}
	_, restoringBody, exists, err := ordinaryReadRestoration(operationDir, intent, intentDigest)
	if err != nil || !exists {
		return errOrdinaryUpgrade
	}
	expected := ordinaryReadyRecord{Version: 1, OperationID: intent.Request.OperationID, IntentSHA256: intentDigest, RestorationSHA256: ordinaryDigestHex(restoringBody)}
	body, err := json.Marshal(expected)
	if err != nil {
		return errOrdinaryUpgrade
	}
	old, exists, err := ordinaryReadOperationFile(operationDir, "ready.json")
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !exists {
		if ordinaryPublishOperationFile(operationDir, "ready.json", body) != nil {
			return errOrdinaryUpgrade
		}
		old, exists, err = ordinaryReadOperationFile(operationDir, "ready.json")
	}
	if err != nil || !exists || !bytes.Equal(old, body) || unix.Fsync(operationDir) != nil {
		return errOrdinaryUpgrade
	}
	anchor, err := ordinaryGuardIdentity(guardFD)
	if err != nil || !ordinaryGuardMatches(dirs, guardFD, anchor) {
		return errOrdinaryUpgrade
	}
	parent := dirs[len(dirs)-1].fd
	state, exists, err := ordinaryReadDisposition(parent)
	if err != nil || !exists || !ordinaryDispositionMatches(state, dirs, anchor) || state.OperationID != intent.Request.OperationID || state.IntentSHA256 != intentDigest || (state.State != "ACTIVE" && state.State != "COMMITTED_READY_FOR_RESTART") {
		return errOrdinaryUpgrade
	}
	if state.State == "ACTIVE" {
		state.State = "COMMITTED_READY_FOR_RESTART"
		if ordinaryWriteDisposition(dirs, state, false) != nil {
			return errOrdinaryUpgrade
		}
	}
	observed, exists, err := ordinaryReadDisposition(parent)
	if err != nil || !exists || observed != state || !ordinaryGuardMatches(dirs, guardFD, anchor) || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

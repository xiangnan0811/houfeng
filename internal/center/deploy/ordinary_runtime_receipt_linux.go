//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sys/unix"
)

type ordinaryRuntimeProofRecord struct {
	Version      int                      `json:"version"`
	OperationID  string                   `json:"operation_id"`
	IntentSHA256 string                   `json:"intent_sha256"`
	Physical     ordinaryPhysicalDatabase `json:"physical"`
	Processes    []ordinaryRuntimeProcess `json:"processes"`
}

// The receipt records operational startup evidence only. It does not claim
// customer content access, writes, or an actual processor job completion.
func ordinaryRecordRuntimeProof(ctx context.Context, observer *pgx.Conn, operationDir int, intent ordinaryUpgradeIntent, intentDigest string) error {
	body, exists, err := ordinaryReadOperationFile(operationDir, "intent.json")
	if err != nil || !exists || ordinaryDigestHex(body) != intentDigest || ordinaryValidateIntent(intent) != nil {
		return errOrdinaryUpgrade
	}
	var stored ordinaryUpgradeIntent
	if fenceDecode(body, &stored) != nil {
		return errOrdinaryUpgrade
	}
	expected, err := json.Marshal(intent)
	if err != nil || !bytes.Equal(expected, body) {
		return errOrdinaryUpgrade
	}
	if _, present, err := ordinaryReadReady(operationDir, intent, intentDigest); err != nil || !present {
		return errOrdinaryUpgrade
	}
	if err := ordinaryVerifyTargetPostconditions(ctx, observer, intent, true); err != nil {
		return err
	}
	if err := ordinaryVerifyComposeRuntime(ctx, intent); err != nil {
		return ordinaryUpgradePhase("runtime-compose-proof", err)
	}
	processes, err := ordinaryVerifySelectedProcesses(ctx, intent)
	if err != nil {
		return ordinaryUpgradePhase("runtime-process-proof", err)
	}
	if err := ordinaryVerifyTargetPostconditions(ctx, observer, intent, true); err != nil {
		return err
	}
	if err := ordinaryVerifyComposeRuntime(ctx, intent); err != nil {
		return ordinaryUpgradePhase("runtime-compose-proof", err)
	}
	record := ordinaryRuntimeProofRecord{Version: 1, OperationID: intent.Request.OperationID, IntentSHA256: intentDigest, Physical: intent.Physical, Processes: processes}
	encoded, err := json.Marshal(record)
	if err != nil {
		return errOrdinaryUpgrade
	}
	name := "runtime-" + ordinaryDigestHex(encoded) + ".json"
	old, exists, err := ordinaryReadOperationFile(operationDir, name)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !exists {
		if ordinaryPublishOperationFile(operationDir, name, encoded) != nil {
			return errOrdinaryUpgrade
		}
		old, exists, err = ordinaryReadOperationFile(operationDir, name)
	}
	if err != nil || !exists || !bytes.Equal(old, encoded) || unix.Fsync(operationDir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

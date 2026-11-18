//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/sys/unix"
)

type ordinaryParticipantRecord struct {
	Version      int                    `json:"version"`
	OperationID  string                 `json:"operation_id"`
	IntentSHA256 string                 `json:"intent_sha256"`
	Identity     ordinaryNativeIdentity `json:"identity"`
}

var ordinaryParticipantRecordName = regexp.MustCompile(`^participant-[1-9][0-9]*-[1-9][0-9]*\.json$`)

var ordinaryRuntimeReceiptName = regexp.MustCompile(`^runtime-[a-f0-9]{64}\.json$`)
var ordinaryOperationTemporaryName = regexp.MustCompile(`^\.ordinary-launch-[a-f0-9]{32}$`)
var ordinaryDrainEvidenceName = regexp.MustCompile(`^drain-[a-f0-9]{32}\.json$`)

func ordinaryParticipantName(identity ordinaryNativeIdentity) string {
	return fmt.Sprintf("participant-%d-%d.json", identity.PID, identity.StartTime)
}

// Publish the real child identity and fsync the operation directory before
// releasing its inert EXECUTE gate. Root never puts secret bytes in this file.
func ordinaryRecordParticipant(dir int, operationID string, intentHash [32]byte, identity ordinaryNativeIdentity) error {
	if !fenceRID.MatchString(operationID) || intentHash == [32]byte{} || identity.PID <= 0 || identity.StartTime == 0 {
		return errOrdinaryUpgrade
	}
	record := ordinaryParticipantRecord{Version: 1, OperationID: operationID, IntentSHA256: hex.EncodeToString(intentHash[:]), Identity: identity}
	body, err := json.Marshal(record)
	if err != nil {
		return errOrdinaryUpgrade
	}
	name := ordinaryParticipantName(identity)
	prior, exists, err := ordinaryReadOperationFile(dir, name)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !exists {
		if ordinaryPublishOperationFile(dir, name, body) != nil {
			return errOrdinaryUpgrade
		}
		prior, exists, err = ordinaryReadOperationFile(dir, name)
	}
	if err != nil || !exists || !bytes.Equal(prior, body) || unix.Fsync(dir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

// Called before ordinary EX while holding the controller-only lock. Only a
// recorded exact participant may be pinned, signalled, and joined here.
func ordinaryJoinRecordedParticipants(ctx context.Context, operationDir int, operationID, intentDigest, guardRoot string) error {
	if ctx == nil || !fenceRID.MatchString(operationID) || !fenceID.MatchString(intentDigest) {
		return errOrdinaryUpgrade
	}
	copyFD, err := unix.Openat(operationDir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(copyFD), "ordinary operation records")
	names, err := file.Readdirnames(-1)
	_ = file.Close()
	if err != nil {
		return errOrdinaryUpgrade
	}
	for _, name := range names {
		if ordinaryOperationTemporaryName.MatchString(name) {
			// No other root controller can run under the held controller lock.
			// A temp has not been published and cannot authorize a child ACK.
			var stat unix.Stat_t
			if unix.Fstatat(operationDir, name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || (stat.Gid != 0 && stat.Gid != composeLaunchRuntimeGID) || (stat.Mode&0o7777 != 0o440 && stat.Mode&0o7777&^0o600 != 0) || stat.Nlink != 1 || stat.Size > fenceOutputLimit || unix.Unlinkat(operationDir, name, 0) != nil || unix.Fsync(operationDir) != nil {
				return errOrdinaryUpgrade
			}
			continue
		}
		if ordinaryDrainEvidenceName.MatchString(name) {
			var stat unix.Stat_t
			if unix.Fstatat(operationDir, name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Size <= 0 || stat.Size > fenceOutputLimit {
				return errOrdinaryUpgrade
			}
			continue
		}
		if !ordinaryParticipantRecordName.MatchString(name) {
			if name != "intent.json" && name != "restoring.json" && name != "ready.json" && name != "namespace-recheck.json" && name != "compose-override.yaml" && name != "backends" && !ordinaryRuntimeReceiptName.MatchString(name) {
				return errOrdinaryUpgrade
			}
			continue
		}
		body, exists, err := ordinaryReadOperationFile(operationDir, name)
		if err != nil || !exists {
			return errOrdinaryUpgrade
		}
		var record ordinaryParticipantRecord
		if fenceDecode(body, &record) != nil || record.Version != 1 || record.OperationID != operationID || record.IntentSHA256 != intentDigest || ordinaryParticipantName(record.Identity) != name {
			return errOrdinaryUpgrade
		}
		if err := ordinaryJoinRecordedNative(ctx, record.Identity, guardRoot); err != nil {
			return errOrdinaryUpgrade
		}
	}
	return nil
}

// Kept bounded when reading an operator-provided one-shot request.
func ordinaryReadRequestFile(path string) (ordinaryUpgradeRequest, error) {
	body, err := readOrdinaryPrivateInput(path, 16<<10)
	if err != nil {
		return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
	}
	defer clear(body)
	request, err := decodeOrdinaryUpgradeRequest(body)
	if err != nil {
		return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
	}
	work, err := filepath.EvalSymlinks(request.WorkingDirectory)
	if err != nil || work != request.WorkingDirectory {
		return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
	}
	data := filepath.Join(work, "data")
	if resolved, err := filepath.EvalSymlinks(data); err == nil {
		data = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
	}
	if fenceOverlap(filepath.Dir(path), data) {
		return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
	}
	return request, nil
}

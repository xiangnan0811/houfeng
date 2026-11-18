//go:build linux

package deploy

import (
	"encoding/hex"
	"path/filepath"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordbirth"
)

// READY permits normal shared managers only when the running admin executable
// is the exact selected target recorded in the immutable operation intent.
func ordinaryReadyAllowsShared(root string, state ordinaryDisposition) bool {
	if state.State != "COMMITTED_READY_FOR_RESTART" || !fenceRID.MatchString(state.OperationID) || !fenceID.MatchString(state.IntentSHA256) {
		return false
	}
	path := filepath.Join(filepath.Dir(root), "ordinary-operations", state.OperationID)
	dirs, err := ordinaryImageDirectories(path)
	if err != nil {
		return false
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1]
	if parent.before.Uid != 0 || parent.before.Gid != composeLaunchRuntimeGID || parent.before.Mode&0o7777 != 0o750 {
		return false
	}
	body, exists, err := ordinaryReadOperationFile(parent.fd, "intent.json")
	var intent ordinaryUpgradeIntent
	if err != nil || !exists || ordinaryDigestHex(body) != state.IntentSHA256 || fenceDecode(body, &intent) != nil || ordinaryValidateIntent(intent) != nil || intent.Request.OperationID != state.OperationID || !composeLaunchDirectoriesUnchanged(dirs) {
		return false
	}
	if _, present, err := ordinaryReadReady(parent.fd, intent, state.IntentSHA256); err != nil || !present {
		return false
	}
	digest, err := recordbirth.DigestExecutable("/proc/self/exe")
	return err == nil && hex.EncodeToString(digest[:]) == intent.Target.AdminSHA256 && composeLaunchDirectoriesUnchanged(dirs)
}

//go:build linux

package deploy

import (
	"golang.org/x/sys/unix"
)

// ordinaryActivateIntent requires the controller lock and the same ordinary EX
// held throughout. The intent directory is root-owned and has already been
// pinned beneath the protected operation root by the caller.
func ordinaryActivateIntent(dirs []composeLaunchDirectory, guardFD, intentDir int, operationID string) (ordinaryUpgradeIntent, string, error) {
	var intent ordinaryUpgradeIntent
	if len(dirs) < 2 || !fenceRID.MatchString(operationID) {
		return intent, "", errOrdinaryUpgrade
	}
	var stat unix.Stat_t
	if unix.Fstat(intentDir, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != composeLaunchRuntimeGID || stat.Mode&0o7777 != 0o750 || stat.Nlink == 0 {
		return intent, "", errOrdinaryUpgrade
	}
	body, exists, err := ordinaryReadOperationFile(intentDir, "intent.json")
	if err != nil || !exists || fenceDecode(body, &intent) != nil || ordinaryValidateIntent(intent) != nil || intent.Request.OperationID != operationID || unix.Fsync(intentDir) != nil {
		return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
	}
	digest := ordinaryDigestHex(body)
	anchor, err := ordinaryGuardIdentity(guardFD)
	if err != nil || !ordinaryGuardMatches(dirs, guardFD, anchor) {
		return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
	}
	marker, exists, err := ordinaryReadRootMarker(dirs[len(dirs)-2].fd)
	if err != nil || !exists || marker.Phase != "COMMITTED" || !ordinaryDispositionMatches(ordinaryMarkerDisposition(marker), dirs, anchor) {
		return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
	}
	parent := dirs[len(dirs)-1].fd
	state, exists, err := ordinaryReadDisposition(parent)
	if err != nil || !exists || state.BootstrapName != marker.BootstrapName || !ordinaryDispositionMatches(state, dirs, anchor) {
		return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
	}
	switch state.State {
	case "CLEAN":
		state.State, state.OperationID, state.IntentSHA256 = "ACTIVE", operationID, digest
		if err := ordinaryWriteDisposition(dirs, state, false); err != nil {
			return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
		}
	case "ACTIVE", "COMMITTED_READY_FOR_RESTART":
		if state.OperationID != operationID || state.IntentSHA256 != digest {
			return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
		}
	default:
		return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
	}
	observed, exists, err := ordinaryReadDisposition(parent)
	if err != nil || !exists || observed != state || !ordinaryGuardMatches(dirs, guardFD, anchor) || unix.Fsync(parent) != nil {
		return ordinaryUpgradeIntent{}, "", errOrdinaryUpgrade
	}
	// Return the recorded phase so replay after READY cannot accidentally
	// rerun the pre-recreation transition.
	return intent, state.State, nil
}

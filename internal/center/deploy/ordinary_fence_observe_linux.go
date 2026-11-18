//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"net"

	"golang.org/x/sys/unix"
)

// ordinaryObservedFenceEndpoint re-observes the exact journalled containers
// and network receipt. During restoration it accepts only original networks
// and the recorded maintenance network, never unrelated endpoints.
func ordinaryObservedFenceEndpoint(ctx context.Context, intent ordinaryUpgradeIntent, restoring bool) (string, string, error) {
	r := intent.Fence.Request
	if ctx == nil || !fenceRequestValid(r) {
		return "", "", errOrdinaryUpgrade
	}
	dir, err := fenceJournalDir(r.ControlConfigPath)
	if err != nil {
		return "", "", errOrdinaryUpgrade
	}
	defer unix.Close(dir)
	stored, exists, err := fenceReadJournal(dir, r)
	if err != nil || !exists {
		return "", "", errOrdinaryUpgrade
	}
	want, err1 := json.Marshal(intent.Fence)
	got, err2 := json.Marshal(stored)
	if err1 != nil || err2 != nil || !bytes.Equal(want, got) {
		return "", "", errOrdinaryUpgrade
	}
	receipt, exists, err := fenceReadReceipt(dir, r.RecoveryID)
	if err != nil || !exists || !fenceJournalDirMatches(r.ControlConfigPath, dir) {
		return "", "", errOrdinaryUpgrade
	}
	network, networkExists, err := fenceFindNetwork(ctx, r)
	if err != nil || (networkExists && (network.ID != receipt.NetworkID || fenceValidateNetworkMembers(network, r.DatabaseContainerID) != nil)) || (!restoring && !networkExists) {
		return "", "", errOrdinaryUpgrade
	}
	project, err := fenceInventory(ctx, r)
	if err != nil || len(project) != len(stored.Containers) {
		return "", "", errOrdinaryUpgrade
	}
	var db fenceObservation
	var original fenceContainer
	for i, observed := range project {
		old := stored.Containers[i]
		if !fenceSameAnchors(old, observed.anchor) || observed.anchor.Restart.Name != "no" || !fenceAllowedNetworks(old, observed, receipt.NetworkID, observed.anchor.ID == r.DatabaseContainerID) {
			return "", "", errOrdinaryUpgrade
		}
		if observed.anchor.ID == r.DatabaseContainerID {
			original, db = old, observed
		} else if observed.state != "exited" && observed.state != "created" {
			return "", "", errOrdinaryUpgrade
		}
	}
	if original.ID == "" || (!restoring && (len(db.anchor.Networks) != 1 || !networkExists || len(network.Containers) != 1)) || (!networkExists && !ordinaryExactNetworks(original.Networks, db.anchor.Networks)) {
		return "", "", errOrdinaryUpgrade
	}
	if attached, ok := fenceDBNetwork(db, receipt.NetworkID); ok && net.ParseIP(attached.IP) != nil {
		return attached.IP, receipt.NetworkID, nil
	}
	if !restoring {
		return "", "", errOrdinaryUpgrade
	}
	for _, originalNetwork := range original.Networks {
		if attached, ok := fenceDBNetwork(db, originalNetwork.ID); ok && net.ParseIP(attached.IP) != nil {
			return attached.IP, receipt.NetworkID, nil
		}
	}
	return "", "", errOrdinaryUpgrade
}

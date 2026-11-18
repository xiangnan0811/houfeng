//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"

	"golang.org/x/sys/unix"
)

// ordinaryRestoreFenceNetworks is called only after the coordinator has durably
// committed to restoration and joined/removed its maintenance participant. It
// never restarts old containers or changes their disabled restart policies.
// A retry may observe any subset of original database networks already restored.
func ordinaryRestoreFenceNetworks(ctx context.Context, r ComposeOfflineFenceRequest) error {
	if ctx == nil || !fenceRequestValid(r) {
		return errComposeOfflineFence
	}
	dir, err := fenceJournalDir(r.ControlConfigPath)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	journal, exists, err := fenceReadJournal(dir, r)
	if err != nil || !exists {
		return errComposeOfflineFence
	}
	receipt, exists, err := fenceReadReceipt(dir, r.RecoveryID)
	if err != nil || !exists || !fenceJournalDirMatches(r.ControlConfigPath, dir) {
		return errComposeOfflineFence
	}
	network, networkExists, err := fenceFindNetwork(ctx, r)
	if err != nil || (networkExists && (network.ID != receipt.NetworkID || fenceValidateNetworkMembers(network, r.DatabaseContainerID) != nil)) {
		return errComposeOfflineFence
	}
	project, err := fenceInventory(ctx, r)
	if err != nil || len(project) != len(journal.Containers) {
		return errComposeOfflineFence
	}
	var original fenceContainer
	var db fenceObservation
	for i, observed := range project {
		old := journal.Containers[i]
		if !fenceSameAnchors(old, observed.anchor) || observed.anchor.Restart.Name != "no" || !fenceAllowedNetworks(old, observed, receipt.NetworkID, observed.anchor.ID == r.DatabaseContainerID) {
			return errComposeOfflineFence
		}
		if observed.anchor.ID == r.DatabaseContainerID {
			original, db = old, observed
		} else if observed.state != "created" && observed.state != "exited" {
			return errComposeOfflineFence
		}
	}
	if original.ID == "" || len(original.Networks) == 0 {
		return errComposeOfflineFence
	}
	if !networkExists && !ordinaryExactNetworks(original.Networks, db.anchor.Networks) {
		return errComposeOfflineFence
	}
	for _, anchor := range original.Networks {
		if _, attached := fenceDBNetwork(db, anchor.ID); attached {
			continue
		}
		args := []string{"network", "connect"}
		for _, alias := range anchor.Aliases {
			args = append(args, "--alias", alias)
		}
		args = append(args, anchor.ID, r.DatabaseContainerID)
		if _, err := fenceDocker(ctx, args...); err != nil {
			return err
		}
		db, err = fenceInspect(ctx, r.DatabaseContainerID)
		if err != nil || !fenceSameAnchors(original, db.anchor) || !fenceAllowedNetworks(original, db, receipt.NetworkID, true) {
			return errComposeOfflineFence
		}
	}
	if networkExists {
		if _, attached := fenceDBNetwork(db, receipt.NetworkID); attached {
			if _, err := fenceDocker(ctx, "network", "disconnect", receipt.NetworkID, r.DatabaseContainerID); err != nil {
				return err
			}
		}
		db, err = fenceInspect(ctx, r.DatabaseContainerID)
		if err != nil || !fenceSameAnchors(original, db.anchor) || !ordinaryExactNetworks(original.Networks, db.anchor.Networks) {
			return errComposeOfflineFence
		}
		network, networkExists, err = fenceFindNetwork(ctx, r)
		if err != nil || !networkExists || network.ID != receipt.NetworkID || len(network.Containers) != 0 {
			return errComposeOfflineFence
		}
		if _, err := fenceDocker(ctx, "network", "rm", receipt.NetworkID); err != nil {
			return err
		}
	}
	if !ordinaryExactNetworks(original.Networks, db.anchor.Networks) || !fenceJournalDirMatches(r.ControlConfigPath, dir) || unix.Fsync(dir) != nil {
		return errComposeOfflineFence
	}
	project, err = fenceInventory(ctx, r)
	if err != nil || len(project) != len(journal.Containers) {
		return errComposeOfflineFence
	}
	for i, observed := range project {
		old := journal.Containers[i]
		if !fenceSameAnchors(old, observed.anchor) || observed.anchor.Restart.Name != "no" || !ordinaryExactNetworks(old.Networks, observed.anchor.Networks) || (observed.anchor.ID != r.DatabaseContainerID && observed.state != "created" && observed.state != "exited") {
			return errComposeOfflineFence
		}
	}
	if _, exists, err := fenceFindNetwork(ctx, r); err != nil || exists {
		return errComposeOfflineFence
	}
	return nil
}

func ordinaryExactNetworks(a, b []fenceNetworkAnchor) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}

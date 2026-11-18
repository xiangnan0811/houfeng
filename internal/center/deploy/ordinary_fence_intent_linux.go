//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"golang.org/x/sys/unix"
)

// Capture the same Docker anchors consumed by fencePerform before ACTIVE is
// published, so the immutable intent binds the original restart and networks.
func ordinaryCaptureFenceIntent(ctx context.Context, request ComposeOfflineFenceRequest) (fenceJournal, error) {
	var journal fenceJournal
	if ctx == nil || !fenceRequestValid(request) || fenceDockerBinary() != nil {
		return journal, errOrdinaryUpgrade
	}
	swarm, err := fenceDocker(ctx, "info", "--format", "{{.Swarm.LocalNodeState}}")
	if err != nil || strings.TrimSpace(string(swarm)) != "inactive" {
		return journal, errOrdinaryUpgrade
	}
	project, err := fenceInventory(ctx, request)
	if err != nil {
		return journal, errOrdinaryUpgrade
	}
	journal = fenceJournal{Version: 1, Request: request}
	for _, observed := range project {
		journal.Containers = append(journal.Containers, observed.anchor)
	}
	return journal, nil
}

// Publish and sync the exact inventory record consumed by the private fence
// engine. The caller holds the project controller lock and ordinary EX.
func ordinaryPersistFenceIntent(journal fenceJournal) error {
	if journal.Version != 1 || !fenceRequestValid(journal.Request) {
		return errOrdinaryUpgrade
	}
	dir, err := fenceJournalDir(journal.Request.ControlConfigPath)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(dir)
	stored, exists, err := fenceReadJournal(dir, journal.Request)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !exists {
		if err := fencePublish(dir, journal.Request.RecoveryID+".json", journal); err != nil {
			return errOrdinaryUpgrade
		}
		stored, exists, err = fenceReadJournal(dir, journal.Request)
	}
	expected, err1 := json.Marshal(journal)
	actual, err2 := json.Marshal(stored)
	if err != nil || !exists || err1 != nil || err2 != nil || !bytes.Equal(expected, actual) || unix.Fsync(dir) != nil || !fenceJournalDirMatches(journal.Request.ControlConfigPath, dir) {
		return errOrdinaryUpgrade
	}
	return nil
}

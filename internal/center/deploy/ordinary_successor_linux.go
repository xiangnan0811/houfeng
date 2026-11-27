//go:build linux

package deploy

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type ordinaryReadyPredecessor struct {
	link   ordinaryIntentLink
	intent ordinaryUpgradeIntent
}

// Called with the controller lock and the exclusive ordinary guard. The
// disposition is the authority for selecting the previous immutable operation.
func ordinaryLoadReadyPredecessor(request ordinaryUpgradeRequest, state ordinaryDisposition) (ordinaryReadyPredecessor, error) {
	var prior ordinaryReadyPredecessor
	if state.State != "COMMITTED_READY_FOR_RESTART" || state.OperationID == request.OperationID {
		return prior, errOrdinaryUpgrade
	}
	dirs, err := ordinaryOpenOperationDirectories(request.WorkingDirectory, state.OperationID, false)
	if err != nil {
		return prior, err
	}
	defer dirs.Close()
	dir := dirs.dirs[len(dirs.dirs)-1].fd
	body, exists, err := ordinaryReadOperationFile(dir, "intent.json")
	if err != nil || !exists || fenceDecode(body, &prior.intent) != nil {
		return ordinaryReadyPredecessor{}, errOrdinaryUpgrade
	}
	old := prior.intent.Request
	if old.OperationID != state.OperationID || old.WorkingDirectory != request.WorkingDirectory || old.Project != request.Project ||
		old.DatabaseContainerID != request.DatabaseContainerID || old.TargetImageID == request.TargetImageID {
		return ordinaryReadyPredecessor{}, errOrdinaryUpgrade
	}
	if _, present, err := ordinaryReadReady(dir, prior.intent, state.IntentSHA256); err != nil || !present || !composeLaunchDirectoriesUnchanged(dirs.dirs) {
		return ordinaryReadyPredecessor{}, errOrdinaryUpgrade
	}
	prior.link = ordinaryIntentLink{OperationID: state.OperationID, IntentSHA256: state.IntentSHA256}
	return prior, nil
}

// READY is published before recreation. Authenticate the live database and
// actual selected executables against the previous target before fencing them.
// The incoming recipe can differ, and a database restart between operations is
// allowed. The new intent still pins its own exact postmaster start time.
func ordinaryVerifyReadyPredecessor(ctx context.Context, observer *pgx.Conn, physical ordinaryPhysicalDatabase, inventory fenceJournal, prior ordinaryUpgradeIntent) error {
	if physical.SystemIdentifier != prior.Physical.SystemIdentifier || physical.DatabaseOID != prior.Physical.DatabaseOID {
		return errOrdinaryUpgrade
	}
	oldDB, found := ordinaryIntentDatabase(prior)
	if !found {
		return errOrdinaryUpgrade
	}
	var sameDatabase bool
	for _, container := range inventory.Containers {
		if container.ID == oldDB.ID {
			sameDatabase = fenceSameAnchors(oldDB, container) && oldDB.Restart == container.Restart && ordinaryExactNetworks(oldDB.Networks, container.Networks)
			break
		}
	}
	if !sameDatabase {
		return errOrdinaryUpgrade
	}
	if err := ordinaryVerifyTargetPostconditions(ctx, observer, prior, true); err != nil {
		return err
	}
	processes, err := ordinaryVerifySelectedProcesses(ctx, prior)
	if err != nil {
		return err
	}
	// Bind the live observations to the same containers the fence will stop.
	for _, process := range processes {
		var captured bool
		for _, container := range inventory.Containers {
			if container.ID == process.ContainerID && container.Service == process.Service && container.Image == prior.Request.TargetImageID {
				captured = true
				break
			}
		}
		if !captured {
			return errOrdinaryUpgrade
		}
	}
	return nil
}

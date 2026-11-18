//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// RunOrdinaryComposeUpgrade runs the host-owned cold controller for one exact
// protected request. The target executable never receives the Docker socket.
func RunOrdinaryComposeUpgrade(ctx context.Context, requestFile string) error {
	if ctx == nil || ctx.Err() != nil {
		return ordinaryUpgradePhase("request-read", errOrdinaryUpgrade)
	}
	if err := ordinaryControllerExecutablePreflight(); err != nil {
		return ordinaryUpgradePhase("executable-preflight", err)
	}
	request, err := ordinaryReadRequestFile(requestFile)
	if err != nil {
		return ordinaryUpgradePhase("request-read", err)
	}
	guardRoot := filepath.Join(request.WorkingDirectory, "maintenance", "ordinary")
	lockCtx, cancelLockWait := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelLockWait()
	err = ordinaryWithControllerLock(lockCtx, request.WorkingDirectory, func() error {
		if err := ordinaryBootstrapGuard(request.WorkingDirectory); err != nil {
			return ordinaryUpgradePhase("bootstrap", err)
		}
		state, err := ordinaryObserveDisposition(guardRoot)
		if err != nil || (state.State != "CLEAN" && state.OperationID != request.OperationID) {
			return ordinaryUpgradePhase("disposition", errOrdinaryUpgrade)
		}
		if err := ordinaryPrepareControllerRoots(request, state.State == "CLEAN"); err != nil {
			return ordinaryUpgradePhase("controller-roots", err)
		}
		dirs, err := ordinaryOpenOperationDirectories(request.WorkingDirectory, request.OperationID, state.State == "CLEAN")
		if err != nil {
			return ordinaryUpgradePhase("operation-directories", err)
		}
		defer dirs.Close()
		operationDir := dirs.dirs[len(dirs.dirs)-1].fd
		if state.State != "CLEAN" {
			joinCtx, cancelJoin := context.WithTimeout(ctx, 30*time.Second)
			err := ordinaryJoinRecordedParticipants(joinCtx, operationDir, request.OperationID, state.IntentSHA256, guardRoot)
			if err != nil && joinCtx.Err() != nil {
				err = joinCtx.Err()
			}
			cancelJoin()
			if err != nil {
				return ordinaryUpgradePhase("participant-recovery", err)
			}
		}
		var intent ordinaryUpgradeIntent
		var intentDigest, override string
		guardCtx, cancelGuardWait := context.WithTimeout(ctx, 2*time.Minute)
		defer cancelGuardWait()
		err = withOrdinaryComposeGuardFD(guardCtx, guardRoot, true, func(guardFD int) error {
			current, err := ordinaryObserveDisposition(guardRoot)
			if err != nil || current != state {
				return ordinaryUpgradePhase("disposition", errOrdinaryUpgrade)
			}
			expectedOverride, err := ordinaryComposeOverride(request)
			if err != nil {
				return ordinaryUpgradePhase("compose-recipe", err)
			}
			priorOverride, overrideExists, err := ordinaryReadOperationFile(operationDir, "compose-override.yaml")
			if err != nil || (overrideExists && !bytes.Equal(priorOverride, expectedOverride)) {
				return ordinaryUpgradePhase("compose-recipe", errOrdinaryUpgrade)
			}
			passwords, identities, err := ordinaryReadUpgradeCredentials(request)
			if err != nil {
				return ordinaryUpgradePhase("credentials", err)
			}
			initialRecipe, err := ordinaryCaptureComposeRecipe(request.WorkingDirectory)
			if err != nil {
				return ordinaryUpgradePhase("compose-recipe", err)
			}
			commitment, err := ordinaryEnsureSecretCommitments(request, identities, initialRecipe.Environment.SHA256, current.State == "CLEAN")
			if errors.Is(err, errOrdinaryEnvironmentCommitment) {
				return ordinaryUpgradePhase("compose-recipe", errOrdinaryUpgrade)
			}
			if err != nil {
				return ordinaryUpgradePhase("credentials", err)
			}
			imageCtx, cancelImage := context.WithTimeout(ctx, 2*time.Minute)
			image, err := ordinaryPrepareImage(imageCtx, request)
			if err != nil && imageCtx.Err() != nil {
				err = imageCtx.Err()
			}
			cancelImage()
			if err != nil {
				return ordinaryUpgradePhase("image-prepare", err)
			}
			body, exists, err := ordinaryReadOperationFile(operationDir, "intent.json")
			if err != nil {
				return ordinaryUpgradePhase("intent", err)
			}
			if current.State == "CLEAN" {
				captured, err := ordinaryCaptureUpgradeIntent(ctx, request, dirs, image, identities, passwords, commitment)
				if err != nil {
					return ordinaryUpgradePhase("predecessor-capture", err)
				}
				if exists {
					expected, err := json.Marshal(captured)
					if err != nil || !bytes.Equal(body, expected) {
						return ordinaryUpgradePhase("intent", errOrdinaryUpgrade)
					}
				}
				if _, err := ordinaryPublishIntent(operationDir, captured); err != nil {
					return ordinaryUpgradePhase("intent", err)
				}
			} else if !exists {
				return ordinaryUpgradePhase("intent", errOrdinaryUpgrade)
			}
			stored, present, err := ordinaryReadOperationFile(operationDir, "intent.json")
			if err != nil || !present || fenceDecode(stored, &intent) != nil || ordinaryValidateIntent(intent) != nil || intent.Request != request || !ordinaryImageMatchesIntent(image, intent) || len(identities) != len(intent.Secrets) {
				return ordinaryUpgradePhase("intent", errOrdinaryUpgrade)
			}
			for i := range identities {
				if !ordinarySecretMetadataMatches(identities[i], intent.Secrets[i]) {
					return ordinaryUpgradePhase("credentials", errOrdinaryUpgrade)
				}
			}
			if commitment != intent.SecretCommitmentSHA256 {
				return ordinaryUpgradePhase("credentials", errOrdinaryUpgrade)
			}
			intentDigest = ordinaryDigestHex(stored)
			if _, err := ordinaryStageSecretFiles(intent); err != nil {
				return ordinaryUpgradePhase("secret-store", err)
			}
			composeCtx, cancelCompose := context.WithTimeout(ctx, 30*time.Second)
			override, err = ordinaryPrepareComposeOverride(composeCtx, operationDir, intent)
			if err != nil && composeCtx.Err() != nil {
				err = composeCtx.Err()
			}
			cancelCompose()
			if err != nil {
				return ordinaryUpgradePhase("compose-preflight", err)
			}
			guardDirs, err := openComposeLaunchDirectories(guardRoot)
			if err != nil {
				return ordinaryUpgradePhase("activate", err)
			}
			defer func() {
				for _, dir := range guardDirs {
					_ = unix.Close(dir.fd)
				}
			}()
			activated, _, err := ordinaryActivateIntent(guardDirs, guardFD, operationDir, request.OperationID)
			activatedBody, encodeErr := json.Marshal(activated)
			if err != nil || encodeErr != nil || !bytes.Equal(activatedBody, stored) {
				return ordinaryUpgradePhase("activate", errOrdinaryUpgrade)
			}
			if current.State == "COMMITTED_READY_FOR_RESTART" {
				if _, present, err := ordinaryReadReady(operationDir, intent, intentDigest); err != nil || !present {
					return ordinaryUpgradePhase("ready", errOrdinaryUpgrade)
				}
				return nil
			}
			return ordinaryRunFencedTransition(ctx, dirs, guardDirs, guardFD, operationDir, intent, intentDigest, image, passwords)
		})
		if err != nil {
			return ordinaryUpgradePhase("guard", err)
		}
		// Selected target managers take their own shared guard, so recreation
		// follows release of ordinary EX while controller serialization remains.
		return ordinaryRecreateTarget(ctx, operationDir, intent, intentDigest, override)
	})
	return ordinaryUpgradePhase("controller-lock", err)
}

func ordinaryRunFencedTransition(ctx context.Context, dirs *ordinaryOperationDirectories, guardDirs []composeLaunchDirectory, guardFD, operationDir int, intent ordinaryUpgradeIntent, digest string, image ordinaryImageTarget, passwords ComposeInitPasswords) error {
	_, _, restoring, err := ordinaryReadRestoration(operationDir, intent, digest)
	if err != nil {
		return ordinaryUpgradePhase("restore-check", err)
	}
	var fence ComposeOfflineFence
	var address string
	if !restoring {
		if err := ordinaryPersistFenceIntent(intent.Fence); err != nil {
			return ordinaryUpgradePhase("fence-intent", err)
		}
		if err := ordinaryFenceDockerPerform(ctx, intent, passwords.Bootstrap, operationDir, &fence); err != nil {
			return ordinaryUpgradePhase("fence", err)
		}
		address = fence.DatabaseAddress
	} else {
		address, _, err = ordinaryObservedFenceEndpoint(ctx, intent, true)
		if err != nil {
			return ordinaryUpgradePhase("restore-network", err)
		}
	}
	observer, physical, err := ordinaryOpenControlObserver(ctx, address, passwords.Bootstrap, intent.Request.OperationID)
	if err != nil || !ordinarySamePhysical(physical, intent.Physical) {
		ordinaryCloseControlObserver(observer)
		return ordinaryUpgradePhase("fence-observer", errOrdinaryUpgrade)
	}
	closed := false
	defer func() {
		if !closed {
			ordinaryCloseControlObserver(observer)
		}
	}()
	records, err := ordinaryReadBackendRecords(dirs.backendPath, intent.Request.OperationID)
	if err != nil {
		return ordinaryUpgradePhase("settlement-records", err)
	}
	if err := ordinaryBoundedDrain(ctx, observer, physical, records, operationDir); err != nil {
		return ordinaryUpgradePhase("pre-fence-drain", err)
	}
	if err := ordinaryObserverRequireIntentSourceAbsent(ctx, observer, intent); err != nil {
		return ordinaryUpgradePhase("predecessor-catalog", err)
	}
	if err := ordinaryRecordNamespaceRecheck(ctx, operationDir, intent, digest, observer); err != nil {
		return ordinaryUpgradePhase("namespace-recheck", err)
	}
	if !restoring {
		if err := ordinaryRunTargetParticipant(ctx, dirs, guardFD, intent, digest, image, passwords, address); err != nil {
			return ordinaryUpgradePhase("participant-execute", err)
		}
		records, err = ordinaryReadBackendRecords(dirs.backendPath, intent.Request.OperationID)
		if err != nil {
			return ordinaryUpgradePhase("settlement-records", err)
		}
		if err := ordinaryBoundedDrain(ctx, observer, physical, records, operationDir); err != nil {
			return ordinaryUpgradePhase("participant-drain", err)
		}
		if err := ordinaryVerifyTargetPostconditions(ctx, observer, intent, false); err != nil {
			return ordinaryUpgradePhase("postcondition", err)
		}
		if err := ordinaryRecordRestoration(operationDir, intent, digest, fence); err != nil {
			return ordinaryUpgradePhase("restore-intent", err)
		}
	} else if err := ordinaryVerifyTargetPostconditions(ctx, observer, intent, false); err != nil {
		return ordinaryUpgradePhase("postcondition", err)
	}
	if err := observer.Close(ctx); err != nil {
		return ordinaryUpgradePhase("observer-close", err)
	}
	closed = true
	if err := ordinaryRestoreFenceNetworks(ctx, intent.Fence.Request); err != nil {
		return ordinaryUpgradePhase("restore-network", err)
	}
	if err := ordinaryCommitReady(guardDirs, guardFD, operationDir, intent, digest); err != nil {
		return ordinaryUpgradePhase("ready", err)
	}
	return nil
}

func ordinaryFenceDockerPerform(ctx context.Context, intent ordinaryUpgradeIntent, password string, operationDir int, result *ComposeOfflineFence) error {
	if err := fenceDockerBinary(); err != nil {
		return err
	}
	swarm, err := fenceDocker(ctx, "info", "--format", "{{.Swarm.LocalNodeState}}")
	if err != nil || strings.TrimSpace(string(swarm)) != "inactive" {
		return errComposeOfflineFence
	}
	return fencePerformWithDrain(ctx, intent.Fence.Request, result, func() error {
		observed, err := fenceInspect(ctx, intent.Request.DatabaseContainerID)
		if err != nil {
			return ordinaryUpgradePhase("pre-fence-drain", err)
		}
		var original fenceContainer
		for _, container := range intent.Fence.Containers {
			if container.ID == intent.Request.DatabaseContainerID {
				original = container
				break
			}
		}
		for _, current := range observed.anchor.Networks {
			for _, old := range original.Networks {
				if current.ID != old.ID {
					continue
				}
				endpoint, ok := fenceDBNetwork(observed, current.ID)
				if !ok {
					return ordinaryUpgradePhase("pre-fence-drain", errOrdinaryUpgrade)
				}
				observer, physical, err := ordinaryOpenControlObserver(ctx, endpoint.IP, password, intent.Request.OperationID)
				if err != nil || !ordinarySamePhysical(physical, intent.Physical) {
					ordinaryCloseControlObserver(observer)
					return ordinaryUpgradePhase("pre-fence-drain", errOrdinaryUpgrade)
				}
				defer ordinaryCloseControlObserver(observer)
				if err := ordinaryBoundedDrain(ctx, observer, physical, nil, operationDir); err != nil {
					return ordinaryUpgradePhase("pre-fence-drain", err)
				}
				if err := observer.Close(ctx); err != nil {
					return ordinaryUpgradePhase("pre-fence-drain", errOrdinaryUpgrade)
				}
				return nil
			}
		}
		// An exact replay can already have disconnected all original networks;
		// the isolated-network drain below still checks every client backend.
		return nil
	})
}

func ordinaryRunTargetParticipant(ctx context.Context, dirs *ordinaryOperationDirectories, guardFD int, intent ordinaryUpgradeIntent, digest string, image ordinaryImageTarget, passwords ComposeInitPasswords, address string) error {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		return ordinaryUpgradePhase("participant-intent", errOrdinaryUpgrade)
	}
	var hash [32]byte
	copy(hash[:], decoded)
	binding := ordinaryNativeBinding{OperationID: intent.Request.OperationID, IntentSHA256: hash, AdminPath: filepath.Join(image.Directory, "houfeng-record-platform-admin"), AdminSHA256: image.AdminSHA256, GuardFD: guardFD}
	child, identity, err := ordinarySpawnNative(ctx, binding)
	if err != nil {
		return ordinaryUpgradePhase("participant-spawn", err)
	}
	joined := false
	defer func() {
		if !joined {
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			_, _ = child.Join(cancelled)
		}
	}()
	if err := ordinaryRecordParticipant(dirs.dirs[len(dirs.dirs)-1].fd, intent.Request.OperationID, hash, identity); err != nil {
		return ordinaryUpgradePhase("participant-record", err)
	}
	input := ordinaryParticipantInput{Version: 1, OperationDirectory: dirs.path, DatabaseAddress: address, Passwords: ordinaryParticipantPasswords{Bootstrap: passwords.Bootstrap, Runtime: passwords.Runtime, PlatformAdmin: passwords.PlatformAdmin, DeletionWorker: passwords.DeletionWorker, Migrator: passwords.Migrator}}
	payload, err := json.Marshal(input)
	if err != nil {
		return ordinaryUpgradePhase("participant-execute", errOrdinaryUpgrade)
	}
	defer clear(payload)
	if err := child.Execute(payload); err != nil {
		return ordinaryUpgradePhase("participant-execute", err)
	}
	status, err := child.Join(ctx)
	joined = true
	if err != nil {
		return ordinaryUpgradePhase("participant-execute", err)
	}
	if status != ordinaryNativeSucceeded {
		return ordinaryUpgradePhase("participant-execute/"+string(status), errOrdinaryUpgrade)
	}
	return nil
}

func ordinaryRecreateTarget(ctx context.Context, operationDir int, intent ordinaryUpgradeIntent, digest, override string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	passwords, identities, err := ordinaryReadUpgradeCredentials(intent.Request)
	if err != nil || len(identities) != len(intent.Secrets) {
		return ordinaryUpgradePhase("credentials", errOrdinaryUpgrade)
	}
	for i := range identities {
		if !ordinarySecretMetadataMatches(identities[i], intent.Secrets[i]) {
			return ordinaryUpgradePhase("credentials", errOrdinaryUpgrade)
		}
	}
	recipe, err := ordinaryCaptureComposeRecipe(intent.Request.WorkingDirectory)
	if err != nil || !ordinaryEnvironmentMetadataMatches(recipe.Environment, intent.Recipe.Environment) {
		return ordinaryUpgradePhase("compose-recipe", errOrdinaryUpgrade)
	}
	commitment, err := ordinaryEnsureSecretCommitments(intent.Request, identities, recipe.Environment.SHA256, false)
	if errors.Is(err, errOrdinaryEnvironmentCommitment) {
		return ordinaryUpgradePhase("compose-recipe", errOrdinaryUpgrade)
	}
	if err != nil || commitment != intent.SecretCommitmentSHA256 {
		return ordinaryUpgradePhase("credentials", errOrdinaryUpgrade)
	}
	if _, err := ordinaryStageSecretFiles(intent); err != nil {
		return ordinaryUpgradePhase("secret-store", err)
	}
	dbCtx, cancelDB := context.WithTimeout(ctx, 30*time.Second)
	err = ordinaryRestoreDatabaseRestart(dbCtx, intent, digest, passwords.Bootstrap, operationDir)
	cancelDB()
	if err != nil {
		return ordinaryUpgradePhase("database-restart", err)
	}
	_, databaseHealth, err := ordinaryRuntimePIDAndHealth(ctx, intent.Request.DatabaseContainerID)
	if err != nil || databaseHealth != "healthy" {
		return ordinaryUpgradePhase("database-health", errOrdinaryUpgrade)
	}
	observer, physical, err := ordinaryOriginalObserver(ctx, intent.Request, intent.Fence, passwords.Bootstrap)
	if err != nil || !ordinarySamePhysical(physical, intent.Physical) {
		ordinaryCloseControlObserver(observer)
		return ordinaryUpgradePhase("database-recheck", errOrdinaryUpgrade)
	}
	if ordinarySamePhysical(physical, intent.Physical) {
		proofCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		proofErr := ordinaryRecordRuntimeProof(proofCtx, observer, operationDir, intent, digest)
		cancel()
		if proofErr == nil {
			ordinaryCloseControlObserver(observer)
			return nil
		}
	}
	ordinaryCloseControlObserver(observer)
	for _, service := range ordinaryOneShotServices {
		step, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, err := ordinaryComposeCommand(step, intent, override, "up", "--detach", "--force-recreate", "--no-deps", "--no-build", "--pull", "never", service)
		if err != nil {
			if step.Err() != nil {
				err = step.Err()
			}
			cancel()
			return ordinaryUpgradePhase("recreate-start:"+service, err)
		}
		err = ordinaryWaitOneShot(step, intent, service)
		if err != nil {
			if step.Err() != nil {
				err = step.Err()
			}
			cancel()
			return ordinaryUpgradePhase("recreate-wait:"+service, err)
		}
		cancel()
	}
	for _, service := range ordinaryLongRunningServices {
		step, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, err := ordinaryComposeCommand(step, intent, override, "up", "--detach", "--wait", "--wait-timeout", "110", "--force-recreate", "--no-deps", "--no-build", "--pull", "never", service)
		if err != nil {
			if step.Err() != nil {
				err = step.Err()
			}
			cancel()
			return ordinaryUpgradePhase("recreate-wait:"+service, err)
		}
		cancel()
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	lastProofErr := error(errOrdinaryUpgrade)
	for {
		observer, physical, err := ordinaryOriginalObserver(deadline, intent.Request, intent.Fence, passwords.Bootstrap)
		if err == nil && ordinarySamePhysical(physical, intent.Physical) {
			proofErr := ordinaryRecordRuntimeProof(deadline, observer, operationDir, intent, digest)
			ordinaryCloseControlObserver(observer)
			if proofErr == nil {
				return nil
			}
			lastProofErr = proofErr
		} else {
			ordinaryCloseControlObserver(observer)
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-deadline.Done():
			timer.Stop()
			return ordinaryUpgradePhase("runtime-proof", lastProofErr)
		case <-timer.C:
		}
	}
}

func ordinaryWaitOneShot(ctx context.Context, intent ordinaryUpgradeIntent, service string) error {
	ids, err := fenceListContainers(ctx)
	if err != nil {
		return errOrdinaryUpgrade
	}
	var found fenceObservation
	for _, id := range ids {
		observation, err := fenceInspect(ctx, id)
		if err != nil {
			return errOrdinaryUpgrade
		}
		if observation.anchor.Project != intent.Request.Project || observation.anchor.Service != service {
			continue
		}
		if found.anchor.ID != "" || observation.anchor.WorkingDirectory != intent.Request.WorkingDirectory || observation.anchor.Image != intent.Request.TargetImageID || observation.scheduler || observation.anchor.Restart != (fenceRestart{Name: "no"}) || (observation.state != "running" && observation.state != "exited") {
			return errOrdinaryUpgrade
		}
		found = observation
	}
	if found.anchor.ID == "" {
		return errOrdinaryUpgrade
	}
	started, restarts, _, err := ordinaryOneShotState(ctx, found.anchor.ID)
	if err != nil || restarts != 0 || started.IsZero() {
		return errOrdinaryUpgrade
	}
	status, err := fenceDocker(ctx, "wait", found.anchor.ID)
	if err != nil || string(bytes.TrimSpace(status)) != "0" {
		return errOrdinaryUpgrade
	}
	after, err := fenceInspect(ctx, found.anchor.ID)
	if err != nil || after.state != "exited" || after.anchor.Restart != (fenceRestart{Name: "no"}) || !fenceSameAnchors(found.anchor, after.anchor) {
		return errOrdinaryUpgrade
	}
	afterStarted, afterRestarts, exitCode, err := ordinaryOneShotState(ctx, found.anchor.ID)
	if err != nil || !afterStarted.Equal(started) || afterRestarts != 0 || exitCode != 0 {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryOneShotState(ctx context.Context, id string) (time.Time, int, int, error) {
	var started time.Time
	var restarts, exitCode int
	if !fenceID.MatchString(id) {
		return started, 0, 0, errOrdinaryUpgrade
	}
	body, err := fenceDocker(ctx, "inspect", "--format", "{{json .State.StartedAt}}\t{{json .RestartCount}}\t{{json .State.ExitCode}}", id)
	if err != nil {
		return started, 0, 0, errOrdinaryUpgrade
	}
	fields := bytes.Split(bytes.TrimSpace(body), []byte{'\t'})
	if len(fields) != 3 || json.Unmarshal(fields[0], &started) != nil || json.Unmarshal(fields[1], &restarts) != nil || json.Unmarshal(fields[2], &exitCode) != nil || restarts < 0 {
		return time.Time{}, 0, 0, errOrdinaryUpgrade
	}
	return started, restarts, exitCode, nil
}

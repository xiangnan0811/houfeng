//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type ordinaryNamespaceRecipe struct {
	Environment map[string]*string `json:"environment"`
	Volumes     []struct {
		Type     string `json:"type"`
		Source   string `json:"source"`
		Target   string `json:"target"`
		ReadOnly bool   `json:"read_only"`
	} `json:"volumes"`
}

// The DB service is unaffected by the target-image/secret override. Read the
// pinned recipe directly before intent publication; never retain config output.
func ordinaryCaptureDatabaseRestart(ctx context.Context, request ordinaryUpgradeRequest, expected ordinaryComposeRecipe, commitment string) (fenceRestart, ordinaryNamespaceRecipe, error) {
	before, err := ordinaryCaptureComposeRecipe(request.WorkingDirectory)
	if err != nil || before.Compose != expected.Compose || before.Environment != expected.Environment || !ordinaryVerifyEnvironmentCommitment(request, commitment, before.Environment.SHA256) || before.ProxyName != expected.ProxyName || before.Proxy != expected.Proxy || !ordinaryComposeOverrideMatches(ordinaryUpgradeIntent{Request: request, Recipe: expected}) {
		return fenceRestart{}, ordinaryNamespaceRecipe{}, errOrdinaryUpgrade
	}
	override := filepath.Join(request.WorkingDirectory, "maintenance", "ordinary-operations", request.OperationID, "compose-override.yaml")
	args := []string{"--host", "unix:///var/run/docker.sock", "compose", "--project-name", request.Project, "--project-directory", request.WorkingDirectory, "--file", filepath.Join(request.WorkingDirectory, "compose.yaml"), "--file", filepath.Join(request.WorkingDirectory, expected.ProxyName), "--file", override, "config", "--format", "json"}
	command := exec.CommandContext(ctx, "/usr/bin/docker", args...)
	command.WaitDelay = 3 * time.Second
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent", "HOUFENG_IMAGE=" + request.TargetImageID}
	var output, stderr fenceLimitedBuffer
	defer func() { clear(output.Bytes()); clear(stderr.Bytes()) }()
	command.Stdout, command.Stderr = &output, &stderr
	if command.Run() != nil || output.overflow || stderr.overflow {
		return fenceRestart{}, ordinaryNamespaceRecipe{}, errOrdinaryUpgrade
	}
	var config struct {
		Services struct {
			Database  ordinaryRuntimeRecipeService `json:"db"`
			Namespace ordinaryNamespaceRecipe      `json:"houfeng-local-namespace-init"`
		} `json:"services"`
	}
	if json.Unmarshal(output.Bytes(), &config) != nil {
		return fenceRestart{}, ordinaryNamespaceRecipe{}, errOrdinaryUpgrade
	}
	policy, valid := ordinaryRecipeRestart(config.Services.Database.Restart)
	after, err := ordinaryCaptureComposeRecipe(request.WorkingDirectory)
	if err != nil || after.Compose != expected.Compose || after.Environment != expected.Environment || !ordinaryVerifyEnvironmentCommitment(request, commitment, after.Environment.SHA256) || after.ProxyName != expected.ProxyName || after.Proxy != expected.Proxy || !ordinaryComposeOverrideMatches(ordinaryUpgradeIntent{Request: request, Recipe: expected}) || !valid || policy.Name == "no" {
		return fenceRestart{}, ordinaryNamespaceRecipe{}, errOrdinaryUpgrade
	}
	return policy, config.Services.Namespace, nil
}

func ordinaryIntentDatabase(intent ordinaryUpgradeIntent) (fenceContainer, bool) {
	for _, item := range intent.Fence.Containers {
		if item.ID == intent.Request.DatabaseContainerID && item.Service == "db" {
			return item, true
		}
	}
	return fenceContainer{}, false
}

// READY and the exact restoration receipt precede this transition. The DB is
// retained; only the restart policy recorded before the fence may be restored.
func ordinaryRestoreDatabaseRestart(ctx context.Context, intent ordinaryUpgradeIntent, digest, password string, operationDir int) error {
	if _, present, err := ordinaryReadReady(operationDir, intent, digest); err != nil || !present {
		return errOrdinaryUpgrade
	}
	original, found := ordinaryIntentDatabase(intent)
	if !found || original.Restart.Name == "no" {
		return errOrdinaryUpgrade
	}
	check := func(wantRestart fenceRestart) error {
		db, err := fenceInspect(ctx, original.ID)
		if err != nil || db.state != "running" || !fenceSameAnchors(original, db.anchor) || !ordinaryExactNetworks(original.Networks, db.anchor.Networks) || db.anchor.Restart != wantRestart {
			return errOrdinaryUpgrade
		}
		observer, physical, err := ordinaryOriginalObserver(ctx, intent.Request, intent.Fence, password)
		if err != nil || !ordinarySamePhysical(physical, intent.Physical) {
			ordinaryCloseControlObserver(observer)
			return errOrdinaryUpgrade
		}
		if err := observer.Close(ctx); err != nil {
			return errOrdinaryUpgrade
		}
		return nil
	}
	current, err := fenceInspect(ctx, original.ID)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if current.anchor.Restart == original.Restart {
		return check(original.Restart)
	}
	if current.anchor.Restart != (fenceRestart{Name: "no"}) || check(fenceRestart{Name: "no"}) != nil {
		return errOrdinaryUpgrade
	}
	policy := original.Restart.Name
	if original.Restart.MaximumRetryCount != 0 {
		if policy != "on-failure" || original.Restart.MaximumRetryCount < 0 {
			return errOrdinaryUpgrade
		}
		policy += ":" + strconv.Itoa(original.Restart.MaximumRetryCount)
	}
	if _, err := fenceDocker(ctx, "update", "--restart="+policy, original.ID); err != nil {
		return errOrdinaryUpgrade
	}
	return check(original.Restart)
}

func ordinaryDatabaseRecipeMatches(intent ordinaryUpgradeIntent, config map[string]ordinaryRuntimeRecipeService) bool {
	db, found := ordinaryIntentDatabase(intent)
	service, present := config["db"]
	policy, valid := ordinaryRecipeRestart(service.Restart)
	return found && present && valid && policy.Name != "no" && policy == db.Restart
}

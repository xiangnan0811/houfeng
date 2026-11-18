//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var ordinaryComposeVersion = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:[-+].*)?$`)

// The fixed override contains only image IDs and root-private file paths. It
// explicitly replaces all environment-backed Compose secret definitions.
func ordinaryComposeOverride(request ordinaryUpgradeRequest) ([]byte, error) {
	if !fenceRID.MatchString(request.OperationID) || !filepath.IsAbs(request.WorkingDirectory) || filepath.Clean(request.WorkingDirectory) != request.WorkingDirectory || !strings.HasPrefix(request.TargetImageID, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(request.TargetImageID, "sha256:")) {
		return nil, errOrdinaryUpgrade
	}
	quote := func(value string) string {
		// Compose interpolates dollar signs even inside quoted YAML scalars.
		raw, _ := json.Marshal(strings.ReplaceAll(value, "$", "$$"))
		return string(raw)
	}
	var body strings.Builder
	body.WriteString("services:\n")
	for _, service := range []string{"houfeng-storage-init", "houfeng-secrets-init", "houfeng-db-init", "houfeng-local-namespace-init", "houfeng-record-authority", "houfeng", "houfeng-content-processor"} {
		body.WriteString("  ")
		body.WriteString(service)
		body.WriteString(":\n    image: ")
		body.WriteString(quote(request.TargetImageID))
		body.WriteByte('\n')
	}
	body.WriteString("secrets:\n")
	for _, name := range ordinarySecretNames {
		body.WriteString("  ")
		body.WriteString(name)
		body.WriteString(": !override\n    file: ")
		body.WriteString(quote(filepath.Join(request.WorkingDirectory, "maintenance", "ordinary-secrets", request.OperationID, name)))
		body.WriteByte('\n')
	}
	return []byte(body.String()), nil
}

func ordinaryPrepareComposeOverride(ctx context.Context, operationDir int, intent ordinaryUpgradeIntent) (string, error) {
	if ctx == nil || ordinaryValidateIntent(intent) != nil {
		return "", errOrdinaryUpgrade
	}
	if err := ordinaryStageComposeOverride(operationDir, intent.Request, intent.Recipe.OverrideSHA256); err != nil {
		return "", errOrdinaryUpgrade
	}
	version, err := fenceDocker(ctx, "compose", "version", "--short")
	if err != nil || !ordinaryComposeVersionSupported(strings.TrimSpace(string(version))) {
		return "", errOrdinaryUpgrade
	}
	override := filepath.Join(intent.Request.WorkingDirectory, "maintenance", "ordinary-operations", intent.Request.OperationID, "compose-override.yaml")
	if _, err := ordinaryComposeCommand(ctx, intent, override, "config", "--quiet"); err != nil {
		return "", errOrdinaryUpgrade
	}
	return override, nil
}

func ordinaryStageComposeOverride(operationDir int, request ordinaryUpgradeRequest, digest string) error {
	body, err := ordinaryComposeOverride(request)
	if err != nil || ordinaryDigestHex(body) != digest {
		return errOrdinaryUpgrade
	}
	old, exists, err := ordinaryReadOperationFile(operationDir, "compose-override.yaml")
	if err != nil {
		return errOrdinaryUpgrade
	}
	if exists && !bytes.Equal(old, body) {
		return errOrdinaryUpgrade
	}
	temp := ".ordinary-launch-" + digest[:32]
	var stale unix.Stat_t
	if statErr := unix.Fstatat(operationDir, temp, &stale, unix.AT_SYMLINK_NOFOLLOW); statErr == nil {
		if stale.Mode&unix.S_IFMT != unix.S_IFREG || stale.Uid != 0 || (stale.Gid != 0 && stale.Gid != composeLaunchRuntimeGID) || stale.Nlink != 1 || (stale.Mode&0o7777 != 0o600 && stale.Mode&0o7777 != 0o440) || stale.Size > int64(len(body)) || unix.Unlinkat(operationDir, temp, 0) != nil || unix.Fsync(operationDir) != nil {
			return errOrdinaryUpgrade
		}
	} else if !errors.Is(statErr, unix.ENOENT) {
		return errOrdinaryUpgrade
	}
	if !exists {
		fd, err := unix.Openat2(operationDir, temp, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
		if err != nil {
			return errOrdinaryUpgrade
		}
		file := os.NewFile(uintptr(fd), "compose override staging")
		if unix.Fchown(fd, 0, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o440) != nil {
			_ = file.Close()
			return errOrdinaryUpgrade
		}
		n, writeErr := file.Write(body)
		syncErr, closeErr := file.Sync(), file.Close()
		if writeErr != nil || n != len(body) || syncErr != nil || closeErr != nil || unix.Renameat2(operationDir, temp, operationDir, "compose-override.yaml", unix.RENAME_NOREPLACE) != nil || unix.Fsync(operationDir) != nil {
			return errOrdinaryUpgrade
		}
		old, exists, err = ordinaryReadOperationFile(operationDir, "compose-override.yaml")
	}
	if err != nil || !exists || !bytes.Equal(old, body) || unix.Fsync(operationDir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryComposeVersionSupported(version string) bool {
	parts := ordinaryComposeVersion.FindStringSubmatch(version)
	if len(parts) != 4 {
		return false
	}
	major, e1 := strconv.Atoi(parts[1])
	minor, e2 := strconv.Atoi(parts[2])
	patch, e3 := strconv.Atoi(parts[3])
	if e1 != nil || e2 != nil || e3 != nil {
		return false
	}
	if major == 2 && minor == 24 && patch == 4 && strings.Contains(version, "-") && !strings.Contains(version, "-desktop.") {
		return false
	}
	return major > 2 || (major == 2 && (minor > 24 || (minor == 24 && patch >= 4)))
}

// Compose receives no raw credentials in its environment or arguments. It
// reads the root-owned fixed override and the pinned operator Compose recipe.
func ordinaryComposeCommand(ctx context.Context, intent ordinaryUpgradeIntent, override string, args ...string) ([]byte, error) {
	if ctx == nil || ordinaryValidateIntent(intent) != nil || override != filepath.Join(intent.Request.WorkingDirectory, "maintenance", "ordinary-operations", intent.Request.OperationID, "compose-override.yaml") {
		return nil, errOrdinaryUpgrade
	}
	before, err := ordinaryCaptureComposeRecipe(intent.Request.WorkingDirectory)
	if err != nil || before.Compose != intent.Recipe.Compose || !ordinaryEnvironmentMetadataMatches(before.Environment, intent.Recipe.Environment) || !ordinaryVerifyEnvironmentCommitment(intent.Request, intent.SecretCommitmentSHA256, before.Environment.SHA256) || before.ProxyName != intent.Recipe.ProxyName || before.Proxy != intent.Recipe.Proxy || !ordinaryComposeOverrideMatches(intent) {
		return nil, errOrdinaryUpgrade
	}
	base := []string{"--host", "unix:///var/run/docker.sock", "compose", "--project-name", intent.Request.Project, "--project-directory", intent.Request.WorkingDirectory, "--file", filepath.Join(intent.Request.WorkingDirectory, "compose.yaml"), "--file", filepath.Join(intent.Request.WorkingDirectory, intent.Recipe.ProxyName), "--file", override}
	command := exec.CommandContext(ctx, "/usr/bin/docker", append(base, args...)...)
	command.WaitDelay = 3 * time.Second
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent", "HOUFENG_IMAGE=" + intent.Request.TargetImageID}
	var out, stderr fenceLimitedBuffer
	command.Stdout, command.Stderr = &out, &stderr
	if command.Run() != nil || out.overflow || stderr.overflow {
		return nil, errOrdinaryUpgrade
	}
	after, err := ordinaryCaptureComposeRecipe(intent.Request.WorkingDirectory)
	if err != nil || after.Compose != intent.Recipe.Compose || !ordinaryEnvironmentMetadataMatches(after.Environment, intent.Recipe.Environment) || !ordinaryVerifyEnvironmentCommitment(intent.Request, intent.SecretCommitmentSHA256, after.Environment.SHA256) || after.ProxyName != intent.Recipe.ProxyName || after.Proxy != intent.Recipe.Proxy || !ordinaryComposeOverrideMatches(intent) {
		return nil, errOrdinaryUpgrade
	}
	return out.Bytes(), nil
}

func ordinaryComposeOverrideMatches(intent ordinaryUpgradeIntent) bool {
	dirs, err := ordinaryOpenOperationDirectories(intent.Request.WorkingDirectory, intent.Request.OperationID, false)
	if err != nil {
		return false
	}
	defer dirs.Close()
	body, exists, err := ordinaryReadOperationFile(dirs.dirs[len(dirs.dirs)-1].fd, "compose-override.yaml")
	expected, encodeErr := ordinaryComposeOverride(intent.Request)
	return err == nil && exists && encodeErr == nil && bytes.Equal(body, expected) && ordinaryDigestHex(body) == intent.Recipe.OverrideSHA256 && composeLaunchDirectoriesUnchanged(dirs.dirs)
}

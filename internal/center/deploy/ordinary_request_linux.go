//go:build linux

package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var errOrdinaryUpgrade = errors.New("ordinary Compose upgrade request or state is invalid")

// Paths identify protected password files, not URLs or password values.
type ordinaryUpgradeSecrets struct {
	Bootstrap       string `json:"bootstrap"`
	Runtime         string `json:"runtime"`
	PlatformAdmin   string `json:"platform_admin"`
	DeletionWorker  string `json:"deletion_worker"`
	Migrator        string `json:"migrator"`
	InitialPassword string `json:"initial_password"`
	SessionHMACKey  string `json:"session_hmac_key"`
}

type ordinaryUpgradeRequest struct {
	Version             int                    `json:"version"`
	OperationID         string                 `json:"operation_id"`
	Project             string                 `json:"project"`
	WorkingDirectory    string                 `json:"working_directory"`
	DatabaseContainerID string                 `json:"database_container_id"`
	TargetImageID       string                 `json:"target_image_id"`
	Secrets             ordinaryUpgradeSecrets `json:"secrets"`
}

// Canonical compact JSON with a final newline also rules out duplicate keys,
// reordered keys, case aliases, whitespace variants, and trailing input.
func decodeOrdinaryUpgradeRequest(body []byte) (ordinaryUpgradeRequest, error) {
	var request ordinaryUpgradeRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return request, errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(request)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || request.Version != 1 || !fenceRID.MatchString(request.OperationID) || !fenceProject.MatchString(request.Project) || !fenceID.MatchString(request.DatabaseContainerID) || !strings.HasPrefix(request.TargetImageID, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(request.TargetImageID, "sha256:")) {
		return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
	}
	paths := []string{request.WorkingDirectory, request.Secrets.Bootstrap, request.Secrets.Runtime, request.Secrets.PlatformAdmin, request.Secrets.DeletionWorker, request.Secrets.Migrator, request.Secrets.InitialPassword, request.Secrets.SessionHMACKey}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path == "/" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 || seen[path] {
			return ordinaryUpgradeRequest{}, errOrdinaryUpgrade
		}
		seen[path] = true
	}
	return request, nil
}

// Read a root-owned private input through pinned safe ancestors, checking both
// the descriptor and pathname again after the bounded read.
func readOrdinaryPrivateInput(path string, limit int64) ([]byte, error) {
	body, _, err := readOrdinaryPrivateIdentity(path, limit)
	return body, err
}

func readOrdinaryPrivateIdentity(path string, limit int64) ([]byte, ordinarySecretIdentity, error) {
	if os.Getuid() != 0 || os.Geteuid() != 0 || limit <= 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, ordinarySecretIdentity{}, errOrdinaryUpgrade
	}
	dirs, err := ordinaryOpenRootDirectories(filepath.Dir(path))
	if err != nil {
		return nil, ordinarySecretIdentity{}, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1].fd
	name := filepath.Base(path)
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if err != nil {
		return nil, ordinarySecretIdentity{}, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary private input")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || (before.Mode&0o7777 != 0o600 && before.Mode&0o7777 != 0o400) || before.Uid != 0 || before.Nlink != 1 || before.Size <= 0 || before.Size > limit {
		return nil, ordinarySecretIdentity{}, errOrdinaryUpgrade
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) || !ordinaryRootDirectoriesUnchanged(dirs) {
		clear(body)
		return nil, ordinarySecretIdentity{}, errOrdinaryUpgrade
	}
	identity := ordinarySecretIdentity{Path: path, Device: uint64(before.Dev), Inode: before.Ino, Size: before.Size, SHA256: ordinaryDigestHex(body)}
	return body, identity, nil
}

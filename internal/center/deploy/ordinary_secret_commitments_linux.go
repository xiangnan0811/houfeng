//go:build linux

package deploy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var errOrdinaryEnvironmentCommitment = errors.New("ordinary environment commitment mismatch")

type ordinarySecretCommitment struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ordinarySecretCommitments struct {
	Version           int                        `json:"version"`
	Nonce             string                     `json:"nonce"`
	EnvironmentSHA256 string                     `json:"environment_sha256"`
	Secrets           []ordinarySecretCommitment `json:"secrets"`
}

func ordinarySecretMetadataMatches(a, b ordinarySecretIdentity) bool {
	return a.Path == b.Path && a.Device == b.Device && a.Inode == b.Inode
}

// The nonce prevents the group-readable intent digest becoming an offline
// verifier for any individual password. The record itself stays root-private.
func ordinaryEnsureSecretCommitments(request ordinaryUpgradeRequest, identities []ordinarySecretIdentity, environmentSHA256 string, create bool) (string, error) {
	if len(identities) != len(ordinarySecretNames) || !fenceID.MatchString(environmentSHA256) {
		return "", errOrdinaryUpgrade
	}
	dirs, err := ordinaryOpenPrivateDirectories(filepath.Join(request.WorkingDirectory, "maintenance"), "ordinary-secrets", request.OperationID, create)
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	defer dirs.Close()
	path := filepath.Join(request.WorkingDirectory, "maintenance", "ordinary-secrets", request.OperationID, "secret-commitments.json")
	body, _, err := readOrdinaryPrivateIdentity(path, 16<<10)
	if err != nil && create {
		parent := dirs.operationFD
		var existing unix.Stat_t
		if statErr := unix.Fstatat(parent, "secret-commitments.json", &existing, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(statErr, unix.ENOENT) {
			return "", errOrdinaryUpgrade
		}
		if err := unix.Fstatat(parent, ".pending-secret-commitments.json", &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
			if existing.Mode&unix.S_IFMT != unix.S_IFREG || existing.Uid != 0 || existing.Gid != 0 || existing.Nlink != 1 || existing.Mode&0o7777 != 0o600 || unix.Unlinkat(parent, ".pending-secret-commitments.json", 0) != nil || unix.Fsync(parent) != nil {
				return "", errOrdinaryUpgrade
			}
		} else if !errors.Is(err, unix.ENOENT) {
			return "", errOrdinaryUpgrade
		}
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", errOrdinaryUpgrade
		}
		record := ordinarySecretCommitments{Version: 2, Nonce: hex.EncodeToString(nonce[:]), EnvironmentSHA256: environmentSHA256}
		for _, identity := range identities {
			record.Secrets = append(record.Secrets, ordinarySecretCommitment{Size: identity.Size, SHA256: identity.SHA256})
		}
		body, err = json.Marshal(record)
		if err != nil {
			return "", errOrdinaryUpgrade
		}
		fd, err := unix.Openat2(parent, ".pending-secret-commitments.json", &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
		if err != nil {
			return "", errOrdinaryUpgrade
		}
		file := os.NewFile(uintptr(fd), "secret commitments")
		n, writeErr := file.Write(body)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || n != len(body) || syncErr != nil || closeErr != nil || unix.Renameat2(parent, ".pending-secret-commitments.json", parent, "secret-commitments.json", unix.RENAME_NOREPLACE) != nil || unix.Fsync(parent) != nil {
			return "", errOrdinaryUpgrade
		}
		body, _, err = readOrdinaryPrivateIdentity(path, 16<<10)
	}
	if err != nil || !dirs.unchanged() {
		return "", errOrdinaryUpgrade
	}
	defer clear(body)
	var record ordinarySecretCommitments
	if fenceDecode(body, &record) != nil || record.Version != 2 || !fenceID.MatchString(record.Nonce) || len(record.Secrets) != len(identities) {
		return "", errOrdinaryUpgrade
	}
	if record.EnvironmentSHA256 != environmentSHA256 {
		return "", errOrdinaryEnvironmentCommitment
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, body) {
		return "", errOrdinaryUpgrade
	}
	for i, identity := range identities {
		if identity.Size <= 0 || !fenceID.MatchString(identity.SHA256) || record.Secrets[i].Size != identity.Size || record.Secrets[i].SHA256 != identity.SHA256 {
			return "", errOrdinaryUpgrade
		}
	}
	return ordinaryDigestHex(body), nil
}

// Every Compose call checks the current .env against the nonce-bound private
// record; the shared intent retains only its inode and device.
func ordinaryVerifyEnvironmentCommitment(request ordinaryUpgradeRequest, commitment, environmentSHA256 string) bool {
	if !fenceID.MatchString(commitment) || !fenceID.MatchString(environmentSHA256) {
		return false
	}
	path := filepath.Join(request.WorkingDirectory, "maintenance", "ordinary-secrets", request.OperationID, "secret-commitments.json")
	body, _, err := readOrdinaryPrivateIdentity(path, 16<<10)
	if err != nil {
		return false
	}
	defer clear(body)
	var record ordinarySecretCommitments
	if fenceDecode(body, &record) != nil || record.Version != 2 || !fenceID.MatchString(record.Nonce) || record.EnvironmentSHA256 != environmentSHA256 || len(record.Secrets) != len(ordinarySecretNames) {
		return false
	}
	canonical, err := json.Marshal(record)
	return err == nil && bytes.Equal(canonical, body) && ordinaryDigestHex(body) == commitment
}

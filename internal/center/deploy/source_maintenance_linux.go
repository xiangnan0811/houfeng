//go:build linux

package deploy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/sourceretirement"
)

const sourceConfigLimit = 16384

type sourceConfigWire struct {
	GuardDirectory string `json:"guard_directory"`
	GuardIdentity  struct {
		DirectoryDevice uint64  `json:"directory_device"`
		DirectoryInode  uint64  `json:"directory_inode"`
		FileDevice      uint64  `json:"file_device"`
		FileInode       uint64  `json:"file_inode"`
		Nonce           []uint8 `json:"nonce"`
	} `json:"guard_identity"`
	Witness struct {
		ID               string `json:"id"`
		SystemIdentifier string `json:"system_identifier"`
		DatabaseOID      uint32 `json:"database_oid"`
		DatabaseName     string `json:"database_name"`
		Epoch            uint64 `json:"epoch"`
	} `json:"witness"`
	ReaderDatabaseURL      string `json:"reader_database_url"`
	BirthInstalled         bool   `json:"birth_installed"`
	BirthLineageID         string `json:"birth_lineage_id"`
	BirthProtocolVersion   uint64 `json:"birth_protocol_version"`
	BirthReaderDatabaseURL string `json:"birth_reader_database_url"`
}

func sourceWire(config SourceMaintenanceConfig) sourceConfigWire {
	var w sourceConfigWire
	w.GuardDirectory, w.ReaderDatabaseURL = config.GuardDirectory, config.ReaderDatabaseURL
	w.BirthInstalled, w.BirthLineageID = config.BirthInstalled, config.BirthLineageID
	w.BirthProtocolVersion, w.BirthReaderDatabaseURL = config.BirthProtocolVersion, config.BirthReaderDatabaseURL
	w.GuardIdentity.DirectoryDevice, w.GuardIdentity.DirectoryInode = config.GuardIdentity.DirectoryDevice, config.GuardIdentity.DirectoryInode
	w.GuardIdentity.FileDevice, w.GuardIdentity.FileInode = config.GuardIdentity.FileDevice, config.GuardIdentity.FileInode
	w.GuardIdentity.Nonce = config.GuardIdentity.Nonce[:]
	w.Witness.ID, w.Witness.SystemIdentifier, w.Witness.DatabaseOID = config.Witness.ID, config.Witness.SystemIdentifier, config.Witness.DatabaseOID
	w.Witness.DatabaseName, w.Witness.Epoch = config.Witness.DatabaseName, config.Witness.Epoch
	return w
}

// rejectDuplicateJSON walks all objects before decoding into the strict schema.
func rejectDuplicateJSON(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errSourceMaintenance
			}
			seen[name] = true
			if err := rejectDuplicateJSON(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := rejectDuplicateJSON(dec); err != nil {
				return err
			}
		}
	default:
		return errSourceMaintenance
	}
	_, err = dec.Token()
	return err
}

func exactSourceKeys(raw json.RawMessage, names ...string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := object[name]; !ok {
			return false
		}
	}
	return true
}

func exactSourceConfigKeys(data []byte) bool {
	var object map[string]json.RawMessage
	if !exactSourceKeys(data, "guard_directory", "guard_identity", "witness", "reader_database_url",
		"birth_installed", "birth_lineage_id", "birth_protocol_version", "birth_reader_database_url") ||
		json.Unmarshal(data, &object) != nil {
		return false
	}
	for _, name := range []string{"birth_installed", "birth_lineage_id", "birth_protocol_version", "birth_reader_database_url"} {
		if bytes.Equal(bytes.TrimSpace(object[name]), []byte("null")) {
			return false
		}
	}
	return exactSourceKeys(object["guard_identity"], "directory_device", "directory_inode", "file_device", "file_inode", "nonce") &&
		exactSourceKeys(object["witness"], "id", "system_identifier", "database_oid", "database_name", "epoch")
}

func sourceConfigParent(path string) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || path == "/" {
		return -1, errSourceMaintenance
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: unix.RESOLVE_NO_SYMLINKS})
	if errors.Is(err, unix.ENOENT) {
		return -1, os.ErrNotExist
	}
	if err != nil {
		return -1, errSourceMaintenance
	}
	var s unix.Stat_t
	if unix.Fstat(fd, &s) != nil || s.Mode&unix.S_IFMT != unix.S_IFDIR || s.Nlink == 0 {
		unix.Close(fd)
		return -1, errSourceMaintenance
	}
	return fd, nil
}

func privateSourceConfigParent(fd int) bool {
	var s unix.Stat_t
	return unix.Fstat(fd, &s) == nil && s.Mode&unix.S_IFMT == unix.S_IFDIR &&
		s.Mode&0o077 == 0 && s.Uid == uint32(os.Geteuid()) && s.Nlink != 0
}

func checkSourceConfigParent(path string) error {
	fd, err := sourceConfigParent(path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if !privateSourceConfigParent(fd) {
		return errSourceMaintenance
	}
	return nil
}

func sameSourceFile(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid &&
		a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size &&
		a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func loadSourceMaintenanceConfig(path string) (SourceMaintenanceConfig, error) {
	var result SourceMaintenanceConfig
	dir, err := sourceConfigParent(path)
	if err != nil {
		return result, err
	}
	defer unix.Close(dir)
	return loadSourceMaintenanceConfigInParent(path, dir, nil)
}

// Read and authenticate the named file through the caller's protected parent.
// identity, when requested, is the exact inode and metadata that were read.
func loadSourceMaintenanceConfigInParent(path string, dir int, identity *unix.Stat_t) (SourceMaintenanceConfig, error) {
	var result SourceMaintenanceConfig
	fd, err := unix.Openat2(dir, filepath.Base(path), &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK), Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return result, os.ErrNotExist
	}
	if err != nil {
		return result, errSourceMaintenance
	}
	if !privateSourceConfigParent(dir) {
		unix.Close(fd)
		return result, errSourceMaintenance
	}
	file := os.NewFile(uintptr(fd), "source-maintenance-config")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != uint32(os.Geteuid()) || (before.Mode&0o7777 != 0o600 && before.Mode&0o7777 != 0o400) || before.Nlink != 1 || before.Size <= 0 || before.Size > sourceConfigLimit {
		return result, errSourceMaintenance
	}
	data, err := io.ReadAll(io.LimitReader(file, sourceConfigLimit+1))
	if err != nil || len(data) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(dir, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) {
		return result, errSourceMaintenance
	}
	currentDir, err := sourceConfigParent(path)
	if err != nil {
		return result, errSourceMaintenance
	}
	var originalDir, currentDirStat unix.Stat_t
	dirOK := unix.Fstat(dir, &originalDir) == nil && unix.Fstat(currentDir, &currentDirStat) == nil && originalDir.Dev == currentDirStat.Dev && originalDir.Ino == currentDirStat.Ino
	unix.Close(currentDir)
	if !dirOK {
		return result, errSourceMaintenance
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := rejectDuplicateJSON(dec); err != nil {
		return result, errSourceMaintenance
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return result, errSourceMaintenance
	}
	if !exactSourceConfigKeys(data) {
		return result, errSourceMaintenance
	}
	var w sourceConfigWire
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil || len(w.GuardIdentity.Nonce) != 32 {
		return result, errSourceMaintenance
	}
	result.GuardDirectory, result.ReaderDatabaseURL = w.GuardDirectory, w.ReaderDatabaseURL
	result.BirthInstalled, result.BirthLineageID = w.BirthInstalled, w.BirthLineageID
	result.BirthProtocolVersion, result.BirthReaderDatabaseURL = w.BirthProtocolVersion, w.BirthReaderDatabaseURL
	result.GuardIdentity.DirectoryDevice, result.GuardIdentity.DirectoryInode = w.GuardIdentity.DirectoryDevice, w.GuardIdentity.DirectoryInode
	result.GuardIdentity.FileDevice, result.GuardIdentity.FileInode = w.GuardIdentity.FileDevice, w.GuardIdentity.FileInode
	copy(result.GuardIdentity.Nonce[:], w.GuardIdentity.Nonce)
	result.Witness = sourceretirement.DomainIdentity{ID: w.Witness.ID, SystemIdentifier: w.Witness.SystemIdentifier, DatabaseOID: w.Witness.DatabaseOID, DatabaseName: w.Witness.DatabaseName, Epoch: w.Witness.Epoch}
	if identity != nil {
		*identity = after
	}
	return result, nil
}

// Explicit provisioning alone may finish the narrow link/unlink cutpoint of a
// previous publication. Only the temporary name linked to this exact inode is
// removed; ordinary participants never repair or provision anything.
func finishSourceConfigPublication(path string) error {
	dir, err := sourceConfigParent(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errSourceMaintenance
	}
	defer unix.Close(dir)
	if !privateSourceConfigParent(dir) {
		return errSourceMaintenance
	}
	var target unix.Stat_t
	if err := unix.Fstatat(dir, filepath.Base(path), &target, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return errSourceMaintenance
	}
	if target.Nlink == 1 {
		return nil
	}
	if target.Nlink != 2 || target.Mode&unix.S_IFMT != unix.S_IFREG || target.Uid != uint32(os.Geteuid()) || target.Mode&0o7777 != 0o600 {
		return errSourceMaintenance
	}
	dup, err := unix.Dup(dir)
	if err != nil {
		return errSourceMaintenance
	}
	file := os.NewFile(uintptr(dup), "source-maintenance-directory")
	names, err := file.Readdirnames(-1)
	file.Close()
	if err != nil {
		return errSourceMaintenance
	}
	for _, name := range names {
		const prefix = ".source-authority-"
		if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
			continue
		}
		if _, err := hex.DecodeString(name[len(prefix):]); err != nil {
			continue
		}
		var candidate unix.Stat_t
		if unix.Fstatat(dir, name, &candidate, unix.AT_SYMLINK_NOFOLLOW) != nil || candidate.Dev != target.Dev || candidate.Ino != target.Ino {
			continue
		}
		if unix.Unlinkat(dir, name, 0) != nil || unix.Fsync(dir) != nil {
			return errSourceMaintenance
		}
		return nil
	}
	return errSourceMaintenance
}

// Sync exact replays as well: a previous publisher may have died after linking
// the file but before the directory entry reached durable storage.
func syncSourceConfigReplay(path string, expected SourceMaintenanceConfig) error {
	dir, err := sourceConfigParent(path)
	if err != nil {
		return errSourceMaintenance
	}
	defer unix.Close(dir)
	return syncSourceConfigReplayInParent(path, dir, expected)
}

func syncSourceConfigReplayInParent(path string, dir int, expected SourceMaintenanceConfig) error {
	var before, after unix.Stat_t
	current, err := loadSourceMaintenanceConfigInParent(path, dir, &before)
	if err != nil || current != expected || validateSourceConfig(path, current) != nil {
		return errSourceMaintenance
	}
	fd, err := unix.Openat2(dir, filepath.Base(path), &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK), Resolve: maintenanceGuardResolve})
	if err != nil {
		return errSourceMaintenance
	}
	defer unix.Close(fd)
	var file, named unix.Stat_t
	if unix.Fstat(fd, &file) != nil || unix.Fstatat(dir, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameSourceFile(before, file) || !sameSourceFile(file, named) || unix.Fsync(fd) != nil || unix.Fsync(dir) != nil {
		return errSourceMaintenance
	}
	current, err = loadSourceMaintenanceConfigInParent(path, dir, &after)
	if err != nil || current != expected || !sameSourceFile(before, after) {
		return errSourceMaintenance
	}
	return nil
}

func publishSourceMaintenanceConfig(path string, config SourceMaintenanceConfig) error {
	dir, err := sourceConfigParent(path)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	if !privateSourceConfigParent(dir) {
		return errSourceMaintenance
	}
	data, err := json.Marshal(sourceWire(config))
	if err != nil || len(data) > sourceConfigLimit {
		return errSourceMaintenance
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return errSourceMaintenance
	}
	name := ".source-authority-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errSourceMaintenance
	}
	defer unix.Unlinkat(dir, name, 0)
	file := os.NewFile(uintptr(fd), "source-maintenance-temporary")
	if _, err := file.Write(data); err != nil {
		file.Close()
		return errSourceMaintenance
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errSourceMaintenance
	}
	if err := file.Close(); err != nil {
		return errSourceMaintenance
	}
	if err := unix.Linkat(dir, name, dir, filepath.Base(path), 0); err != nil {
		if errors.Is(err, unix.EEXIST) {
			old, e := LoadSourceMaintenanceConfig(path)
			if e == nil && old == config {
				return syncSourceConfigReplay(path, config)
			}
		}
		return errSourceMaintenance
	}
	if err := unix.Unlinkat(dir, name, 0); err != nil {
		return errSourceMaintenance
	}
	if err := unix.Fsync(dir); err != nil {
		return errSourceMaintenance
	}
	return nil
}

// publishSourceMaintenanceConfigReplacement requires the caller to hold the
// existing guard exclusively for the entire read, publication, and follow-up
// ceremony. It does not establish participant fencing or verify W2 admission.
func publishSourceMaintenanceConfigReplacement(path string, expected, next SourceMaintenanceConfig) error {
	if validateSourceConfig(path, expected) != nil || validateSourceConfig(path, next) != nil ||
		expected.GuardDirectory != next.GuardDirectory || expected.GuardIdentity != next.GuardIdentity || expected.Witness != next.Witness ||
		expected.BirthInstalled && (!next.BirthInstalled || expected.BirthLineageID != next.BirthLineageID || expected.BirthProtocolVersion != next.BirthProtocolVersion) {
		return errSourceMaintenance
	}
	dir, err := sourceConfigParent(path)
	if err != nil {
		return errSourceMaintenance
	}
	defer unix.Close(dir)
	var original unix.Stat_t
	current, err := loadSourceMaintenanceConfigInParent(path, dir, &original)
	if err != nil || validateSourceConfig(path, current) != nil {
		return errSourceMaintenance
	}
	if current == next {
		return syncSourceConfigReplayInParent(path, dir, next)
	}
	if current != expected {
		return errSourceMaintenance
	}
	data, err := json.Marshal(sourceWire(next))
	if err != nil || len(data) == 0 || len(data) > sourceConfigLimit {
		return errSourceMaintenance
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return errSourceMaintenance
	}
	name := ".source-authority-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errSourceMaintenance
	}
	file := os.NewFile(uintptr(fd), "source-maintenance-replacement")
	defer file.Close()
	var temp unix.Stat_t
	if unix.Fstat(fd, &temp) != nil {
		return errSourceMaintenance
	}
	tempDevice, tempInode := temp.Dev, temp.Ino
	defer func() {
		var named unix.Stat_t
		if unix.Fstatat(dir, name, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && named.Dev == tempDevice && named.Ino == tempInode {
			_ = unix.Unlinkat(dir, name, 0)
		}
	}()
	if temp.Mode&unix.S_IFMT != unix.S_IFREG || temp.Uid != original.Uid || temp.Nlink != 1 ||
		(temp.Gid != original.Gid && unix.Fchown(fd, -1, int(original.Gid)) != nil) {
		return errSourceMaintenance
	}
	written, writeErr := file.Write(data)
	if writeErr != nil || written != len(data) || unix.Fchmod(fd, original.Mode&0o7777) != nil ||
		unix.Fstat(fd, &temp) != nil || temp.Mode&unix.S_IFMT != unix.S_IFREG || temp.Mode&0o7777 != original.Mode&0o7777 ||
		temp.Uid != original.Uid || temp.Gid != original.Gid || temp.Nlink != 1 || temp.Size != int64(len(data)) || file.Sync() != nil {
		return errSourceMaintenance
	}
	var beforeRename, namedTemp unix.Stat_t
	current, err = loadSourceMaintenanceConfigInParent(path, dir, &beforeRename)
	if err != nil || current != expected || !sameSourceFile(original, beforeRename) ||
		unix.Fstatat(dir, name, &namedTemp, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(temp, namedTemp) {
		return errSourceMaintenance
	}
	if unix.Renameat(dir, name, dir, filepath.Base(path)) != nil {
		return errSourceMaintenance
	}
	var renamed, named unix.Stat_t
	if unix.Fstat(fd, &renamed) != nil || unix.Fstatat(dir, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		renamed.Dev != tempDevice || renamed.Ino != tempInode || !sameSourceFile(renamed, named) {
		return errSourceMaintenance
	}
	if file.Close() != nil {
		return errSourceMaintenance
	}
	return syncSourceConfigReplayInParent(path, dir, next)
}

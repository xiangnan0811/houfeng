//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store/migrate"
)

const ordinaryContractLimit = 8 << 20

// ordinaryTargetContract contains the compiler identity and private protocol.
// SourceIdentitySHA256 identifies the C81 migration/privilege bytes; the intent's
// ContractSHA256 separately binds the complete wire, including protocol version.
type ordinaryTargetContract struct {
	Version               int    `json:"version"`
	NativeProtocolVersion int    `json:"native_protocol_version"`
	SchemaVersion         uint64 `json:"schema_version"`
	CanonicalMigrationSet []byte `json:"canonical_migration_set"`
	CanonicalPrivilegeSet []byte `json:"canonical_privilege_set"`
	SourceIdentitySHA256  string `json:"source_identity_sha256"`
}

type ordinaryImageTarget struct {
	Version         int                    `json:"version"`
	ImageID         string                 `json:"image_id"`
	Platform        string                 `json:"platform"`
	Directory       string                 `json:"directory"`
	AdminSHA256     [32]byte               `json:"admin_sha256"`
	CenterSHA256    [32]byte               `json:"center_sha256"`
	ProcessorSHA256 [32]byte               `json:"processor_sha256"`
	Contract        ordinaryTargetContract `json:"contract"`
}

func ordinaryContractDigest(migration, privilege []byte) string {
	payload := make([]byte, 0, 8+len(migration)+len(privilege))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(migration)))
	payload = append(payload, length[:]...)
	payload = append(payload, migration...)
	binary.BigEndian.PutUint32(length[:], uint32(len(privilege)))
	payload = append(payload, length[:]...)
	payload = append(payload, privilege...)
	h := sha256.New()
	_, _ = h.Write([]byte("HOUFENG-ORDINARY-TARGET-CONTRACT-V1\x00"))
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func ordinaryDecodeTargetContract(body []byte) (ordinaryTargetContract, error) {
	var value ordinaryTargetContract
	if len(body) == 0 || len(body) > ordinaryContractLimit {
		return value, errOrdinaryUpgrade
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return ordinaryTargetContract{}, errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || value.Version != 1 || value.NativeProtocolVersion != ordinaryNativeProtocolVersion || value.SchemaVersion <= 0 || len(value.CanonicalMigrationSet) == 0 || len(value.CanonicalPrivilegeSet) == 0 || value.SourceIdentitySHA256 != ordinaryContractDigest(value.CanonicalMigrationSet, value.CanonicalPrivilegeSet) {
		return ordinaryTargetContract{}, errOrdinaryUpgrade
	}
	return value, nil
}

func ordinaryCurrentTargetContract() (ordinaryTargetContract, error) {
	identity, err := migrate.CompileAppACLCurrentSourceIdentity(composeDatabaseName, platformmigrate.AppRoleSetV2{
		CenterRuntime:  composeRuntimeRole,
		PlatformAdmin:  composeAdminRole,
		DeletionWorker: composeDeletionWorkerRole,
		Migrator:       composeMigratorRole,
	})
	if err != nil {
		return ordinaryTargetContract{}, err
	}
	contract := ordinaryTargetContract{
		Version: 1, NativeProtocolVersion: ordinaryNativeProtocolVersion, SchemaVersion: identity.SchemaVersion,
		CanonicalMigrationSet: identity.CanonicalMigrationSet,
		CanonicalPrivilegeSet: identity.CanonicalPrivilegeSet,
	}
	contract.SourceIdentitySHA256 = ordinaryContractDigest(contract.CanonicalMigrationSet, contract.CanonicalPrivilegeSet)
	return contract, nil
}

// WriteOrdinaryTargetContract emits the current binary's embedded migration and
// Compose privilege identities in the fixed private probe wire format.
func WriteOrdinaryTargetContract(w io.Writer) error {
	contract, err := ordinaryCurrentTargetContract()
	if err != nil {
		return err
	}
	body, err := json.Marshal(contract)
	if err != nil || len(body)+1 > ordinaryContractLimit {
		return errOrdinaryUpgrade
	}
	body = append(body, '\n')
	n, err := w.Write(body)
	if err != nil {
		return err
	}
	if n != len(body) {
		return io.ErrShortWrite
	}
	return nil
}

// docker info and image inspect are obtained from the fixed local socket. The
// security options are daemon properties, not inherited client configuration.
type ordinaryDockerInfo struct {
	OSType          string   `json:"OSType"`
	Architecture    string   `json:"Architecture"`
	SecurityOptions []string `json:"SecurityOptions"`
}

type ordinaryDockerImageConfig struct {
	Volumes map[string]json.RawMessage `json:"Volumes"`
}

type ordinaryDockerImage struct {
	ID           string                     `json:"Id"`
	OS           string                     `json:"Os"`
	Architecture string                     `json:"Architecture"`
	Variant      string                     `json:"Variant"`
	Config       *ordinaryDockerImageConfig `json:"Config"`
}

func ordinaryDockerPlatform(info ordinaryDockerInfo, image ordinaryDockerImage, imageID string) (string, error) {
	daemonArch := info.Architecture
	switch daemonArch {
	case "x86_64":
		daemonArch = "amd64"
	case "aarch64":
		daemonArch = "arm64"
	}
	validVariant := image.Variant == "" || (runtime.GOARCH == "arm64" && image.Variant == "v8")
	if !strings.HasPrefix(imageID, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(imageID, "sha256:")) || image.ID != imageID || info.OSType != "linux" || image.OS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") || daemonArch != runtime.GOARCH || image.Architecture != runtime.GOARCH || !validVariant || image.Config == nil || len(image.Config.Volumes) != 0 {
		return "", errOrdinaryUpgrade
	}
	for _, option := range info.SecurityOptions {
		option = strings.ToLower(option)
		if strings.Contains(option, "rootless") || strings.Contains(option, "userns") {
			return "", errOrdinaryUpgrade
		}
	}
	return fmt.Sprintf("linux/%s", runtime.GOARCH), nil
}

// A created container has never run. An exited container, even with exit code
// zero, is not an acceptable source: it may have modified its writable layer.
type ordinaryImageContainer struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Image        string `json:"Image"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
	Config struct {
		Image      string            `json:"Image"`
		Labels     map[string]string `json:"Labels"`
		Entrypoint []string          `json:"Entrypoint"`
		User       string            `json:"User"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode    string `json:"NetworkMode"`
		ReadonlyRootfs bool   `json:"ReadonlyRootfs"`
		LogConfig      struct {
			Type string `json:"Type"`
		} `json:"LogConfig"`
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts []json.RawMessage `json:"Mounts"`
}

func ordinaryValidateImageContainer(container ordinaryImageContainer, name, token, owner, imageID string) error {
	if !fenceID.MatchString(container.ID) || container.Name != "/"+name || container.Image != imageID || container.Config.Image != imageID || container.Config.Labels["houfeng.ordinary.target"] != token || container.Config.Labels["houfeng.ordinary.operation"] != owner || len(container.Config.Entrypoint) != 1 || container.Config.Entrypoint[0] != "/bin/false" || container.Config.User != "10002:10001" || container.State.Status != "created" || container.State.Running || container.State.StartedAt != "0001-01-01T00:00:00Z" || container.RestartCount != 0 || container.HostConfig.NetworkMode != "none" || container.HostConfig.LogConfig.Type != "none" || container.HostConfig.RestartPolicy.Name != "no" || !container.HostConfig.ReadonlyRootfs || len(container.Mounts) != 0 {
		return errOrdinaryUpgrade
	}
	return nil
}

// All directory components are root controlled, including those above the
// maintenance sibling; ownership by the unprivileged publisher is unsafe here.
func ordinaryImageDirectories(path string) ([]composeLaunchDirectory, error) {
	dirs, err := openComposeLaunchDirectories(path)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	for _, dir := range dirs {
		if dir.before.Uid != 0 || dir.before.Mode&0o022 != 0 {
			for _, opened := range dirs {
				_ = unix.Close(opened.fd)
			}
			return nil, errOrdinaryUpgrade
		}
	}
	return dirs, nil
}

// Creating a subdirectory changes only the parent's link count. Preserve its
// pinned inode and metadata while refreshing that expected count change.
func ordinaryImageRefreshDirectory(dirs []composeLaunchDirectory) bool {
	if len(dirs) < 2 {
		return false
	}
	last := len(dirs) - 1
	var current, named unix.Stat_t
	if unix.Fstat(dirs[last].fd, &current) != nil || unix.Fstatat(dirs[last-1].fd, dirs[last].name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return false
	}
	old := dirs[last].before
	if old.Dev != current.Dev || old.Ino != current.Ino || old.Mode != current.Mode || old.Uid != current.Uid || old.Gid != current.Gid || current.Nlink == 0 || !sameComposeLaunchDirectory(current, named) {
		return false
	}
	dirs[last].before.Nlink = current.Nlink
	return composeLaunchDirectoriesUnchanged(dirs)
}

func ordinaryImageMkdir(parent int, name string) error {
	created := false
	if err := unix.Mkdirat(parent, name, 0o750); err == nil {
		created = true
	} else if !errors.Is(err, unix.EEXIST) {
		return errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	if created && (unix.Fchown(fd, 0, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o750) != nil || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil) {
		return errOrdinaryUpgrade
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Gid != composeLaunchRuntimeGID || st.Mode&0o7777 != 0o750 {
		return errOrdinaryUpgrade
	}
	return nil
}

type ordinaryImageDirectoryIntent struct {
	Version      int    `json:"version"`
	Phase        string `json:"phase"`
	ParentDevice uint64 `json:"parent_device"`
	ParentInode  uint64 `json:"parent_inode"`
	Name         string `json:"name"`
	Project      string `json:"project"`
	OperationID  string `json:"operation_id"`
	Nonce        string `json:"nonce"`
}

type ordinaryImageDirectoryCommit struct {
	Version int                          `json:"version"`
	Phase   string                       `json:"phase"`
	Intent  ordinaryImageDirectoryIntent `json:"intent"`
	Device  uint64                       `json:"device"`
	Inode   uint64                       `json:"inode"`
}

// The intent is durable before mkdir. A recovered uncommitted directory may
// only be the empty root-owned intermediate created under that exact intent.
func ordinaryImageMkdirJournalled(parent int, name, project, operation string) error {
	if (name != "ordinary-targets" && !fenceID.MatchString(name)) || !fenceProject.MatchString(project) || !fenceRID.MatchString(operation) {
		return errOrdinaryUpgrade
	}
	var parentStat unix.Stat_t
	if unix.Fstat(parent, &parentStat) != nil || parentStat.Mode&unix.S_IFMT != unix.S_IFDIR || parentStat.Uid != 0 || parentStat.Mode&0o022 != 0 {
		return errOrdinaryUpgrade
	}
	intentName := "." + name + ".mkdir.intent.json"
	commitName := "." + name + ".mkdir.commit.json"
	body, found, err := ordinaryReadImageRecord(parent, intentName)
	if err != nil {
		return errOrdinaryUpgrade
	}
	var intent ordinaryImageDirectoryIntent
	if found {
		if ordinaryReadCanonicalRecord(body, &intent) != nil || intent.Version != 1 || intent.Phase != "PREPARED" || intent.ParentDevice != uint64(parentStat.Dev) || intent.ParentInode != parentStat.Ino || intent.Name != name || !fenceProject.MatchString(intent.Project) || !fenceRID.MatchString(intent.OperationID) || !ordinaryImageNonceValid(intent.Nonce) {
			return errOrdinaryUpgrade
		}
	} else {
		var existing unix.Stat_t
		if err := unix.Fstatat(parent, name, &existing, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return errOrdinaryUpgrade
		}
		if err := unix.Fstatat(parent, commitName, &existing, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return errOrdinaryUpgrade
		}
		if err := unix.Fstatat(parent, ".record-"+commitName, &existing, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return errOrdinaryUpgrade
		}
		if ordinaryRemoveImagePartial(parent, ".record-"+intentName) != nil {
			return errOrdinaryUpgrade
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return errOrdinaryUpgrade
		}
		intent = ordinaryImageDirectoryIntent{Version: 1, Phase: "PREPARED", ParentDevice: uint64(parentStat.Dev), ParentInode: parentStat.Ino, Name: name, Project: project, OperationID: operation, Nonce: hex.EncodeToString(nonce[:])}
	}
	if ordinaryImageRecord(parent, intentName, intent) != nil {
		return errOrdinaryUpgrade
	}
	commitBody, committed, err := ordinaryReadImageRecord(parent, commitName)
	if err != nil {
		return errOrdinaryUpgrade
	}
	var commit ordinaryImageDirectoryCommit
	if committed {
		if ordinaryReadCanonicalRecord(commitBody, &commit) != nil || commit.Version != 1 || commit.Phase != "COMMITTED" || commit.Intent != intent || commit.Inode == 0 {
			return errOrdinaryUpgrade
		}
	} else {
		if intent.Project != project || intent.OperationID != operation {
			return errOrdinaryUpgrade
		}
		if err := unix.Mkdirat(parent, name, 0o750); err != nil && !errors.Is(err, unix.EEXIST) {
			return errOrdinaryUpgrade
		}
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	var before, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || before.Mode&unix.S_IFMT != unix.S_IFDIR || before.Uid != 0 || before.Nlink == 0 || before.Dev != named.Dev || before.Ino != named.Ino || before.Mode != named.Mode || before.Uid != named.Uid || before.Gid != named.Gid {
		return errOrdinaryUpgrade
	}
	if committed {
		if uint64(before.Dev) != commit.Device || before.Ino != commit.Inode || before.Gid != composeLaunchRuntimeGID || before.Mode&0o7777 != 0o750 {
			return errOrdinaryUpgrade
		}
		if ordinaryImageRecord(parent, commitName, commit) != nil {
			return errOrdinaryUpgrade
		}
		var current unix.Stat_t
		if unix.Fstat(parent, &current) != nil || current.Dev != parentStat.Dev || current.Ino != parentStat.Ino || current.Uid != parentStat.Uid || current.Mode != parentStat.Mode || current.Gid != parentStat.Gid {
			return errOrdinaryUpgrade
		}
		return nil
	}
	if (before.Gid != 0 && before.Gid != composeLaunchRuntimeGID) || (before.Mode&0o7777 != 0o700 && before.Mode&0o7777 != 0o750) || ordinaryImageDirectoryEmpty(fd) != nil {
		return errOrdinaryUpgrade
	}
	if unix.Fchown(fd, 0, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o750) != nil || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	var after, afterNamed, afterParent unix.Stat_t
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, name, &afterNamed, unix.AT_SYMLINK_NOFOLLOW) != nil || unix.Fstat(parent, &afterParent) != nil || after.Dev != before.Dev || after.Ino != before.Ino || after.Uid != 0 || after.Gid != composeLaunchRuntimeGID || after.Mode&0o7777 != 0o750 || after.Dev != afterNamed.Dev || after.Ino != afterNamed.Ino || after.Mode != afterNamed.Mode || after.Uid != afterNamed.Uid || after.Gid != afterNamed.Gid || afterParent.Dev != parentStat.Dev || afterParent.Ino != parentStat.Ino || afterParent.Uid != parentStat.Uid || afterParent.Gid != parentStat.Gid || afterParent.Mode != parentStat.Mode || ordinaryImageDirectoryEmpty(fd) != nil {
		return errOrdinaryUpgrade
	}
	commit = ordinaryImageDirectoryCommit{Version: 1, Phase: "COMMITTED", Intent: intent, Device: uint64(after.Dev), Inode: after.Ino}
	return ordinaryImageRecord(parent, commitName, commit)
}

func ordinaryImageNonceValid(value string) bool {
	decoded, err := hex.DecodeString(value)
	return len(value) == 32 && err == nil && hex.EncodeToString(decoded) == value
}

// Immutable records are published by rename and only ever compared on replay.
func ordinaryImageRecord(parent int, name string, value any) error {
	body, err := json.Marshal(value)
	if err != nil || len(body)+1 > ordinaryContractLimit {
		return errOrdinaryUpgrade
	}
	body = append(body, '\n')
	temp := ".record-" + name
	if ordinaryRemoveImagePartial(parent, temp) != nil {
		return errOrdinaryUpgrade
	}
	old, exists, err := ordinaryReadImageRecord(parent, name)
	if err != nil {
		return err
	}
	if exists {
		if !bytes.Equal(old, body) || unix.Fsync(parent) != nil {
			return errOrdinaryUpgrade
		}
		return nil
	}
	fd, err := unix.Openat2(parent, temp, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary image record")
	defer func() { _ = file.Close(); _ = unix.Unlinkat(parent, temp, 0) }()
	if unix.Fchown(fd, 0, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o550) != nil {
		return errOrdinaryUpgrade
	}
	if n, err := file.Write(body); err != nil || n != len(body) || file.Sync() != nil {
		return errOrdinaryUpgrade
	}
	if unix.Renameat2(parent, temp, parent, name, unix.RENAME_NOREPLACE) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryReadImageRecord(parent int, name string) ([]byte, bool, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary image record")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o550 || before.Uid != 0 || before.Gid != composeLaunchRuntimeGID || before.Nlink != 1 || before.Size <= 0 || before.Size > ordinaryContractLimit {
		return nil, false, errOrdinaryUpgrade
	}
	body, err := io.ReadAll(io.LimitReader(file, ordinaryContractLimit+1))
	if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) {
		return nil, false, errOrdinaryUpgrade
	}
	return body, true, nil
}

func ordinaryImageReadExecutable(parent int, name string) ([32]byte, bool, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return [32]byte{}, false, nil
	}
	if err != nil {
		return [32]byte{}, false, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary target executable")
	defer file.Close()
	digest, err := ordinaryTargetExecutable(file)
	if err != nil {
		return [32]byte{}, false, errOrdinaryUpgrade
	}
	var descriptor, named unix.Stat_t
	if unix.Fstat(fd, &descriptor) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(descriptor, named) {
		return [32]byte{}, false, errOrdinaryUpgrade
	}
	return digest, true, nil
}

func ordinaryCheckDockerSocketIsolation() error {
	var st unix.Stat_t
	if unix.Lstat("/var/run/docker.sock", &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != 0 || st.Mode&0o006 != 0 || (st.Gid == composeLaunchRuntimeGID && st.Mode&0o060 != 0) {
		return errOrdinaryUpgrade
	}
	// An ACL could grant access despite otherwise restrictive mode bits.
	if _, err := unix.Getxattr("/var/run/docker.sock", "system.posix_acl_access", nil); err != unix.ENODATA && err != unix.ENOTSUP {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryProbeAndValidateContract(ctx context.Context, directory string, digest [32]byte, expected ordinaryTargetContract) (ordinaryTargetContract, error) {
	if ordinaryCheckDockerSocketIsolation() != nil {
		return ordinaryTargetContract{}, errOrdinaryUpgrade
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := ordinaryProbeTargetContract(ctx, filepath.Join(directory, "houfeng-record-platform-admin"), digest)
	if err != nil {
		if errors.Is(err, errOrdinaryControllerExecutable) {
			return ordinaryTargetContract{}, err
		}
		return ordinaryTargetContract{}, errOrdinaryUpgrade
	}
	got, err := ordinaryDecodeTargetContract(body)
	if err != nil || got.Version != expected.Version || got.NativeProtocolVersion != expected.NativeProtocolVersion || got.SchemaVersion != expected.SchemaVersion || got.SourceIdentitySHA256 != expected.SourceIdentitySHA256 || !bytes.Equal(got.CanonicalMigrationSet, expected.CanonicalMigrationSet) || !bytes.Equal(got.CanonicalPrivilegeSet, expected.CanonicalPrivilegeSet) {
		return ordinaryTargetContract{}, errOrdinaryUpgrade
	}
	return got, nil
}

type ordinaryImageIntent struct {
	Version     int    `json:"version"`
	Phase       string `json:"phase"`
	Project     string `json:"project"`
	OperationID string `json:"operation_id"`
	ImageID     string `json:"image_id"`
	Platform    string `json:"platform"`
	Name        string `json:"name"`
	Token       string `json:"token"`
}

type ordinaryImageFileRecord struct {
	Version int      `json:"version"`
	Name    string   `json:"name"`
	SHA256  [32]byte `json:"sha256"`
}

type ordinaryImageContainerRecord struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
}

func ordinaryReadCanonicalRecord(body []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryInspectDockerImage(ctx context.Context, imageID string) (string, error) {
	if fenceDockerBinary() != nil {
		return "", errOrdinaryUpgrade
	}
	infoJSON, err := fenceDocker(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	var info ordinaryDockerInfo
	if json.Unmarshal(infoJSON, &info) != nil {
		return "", errOrdinaryUpgrade
	}
	imageJSON, err := fenceDocker(ctx, "image", "inspect", imageID)
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	var images []ordinaryDockerImage
	if json.Unmarshal(imageJSON, &images) != nil || len(images) != 1 {
		return "", errOrdinaryUpgrade
	}
	return ordinaryDockerPlatform(info, images[0], imageID)
}

func ordinaryFindImageContainer(ctx context.Context, intent ordinaryImageIntent) (ordinaryImageContainer, bool, error) {
	var result ordinaryImageContainer
	output, err := fenceDocker(ctx, "ps", "-a", "--no-trunc", "--filter", "name=^/"+intent.Name+"$", "--format", "{{.ID}}")
	if err != nil {
		return result, false, errOrdinaryUpgrade
	}
	ids := strings.Fields(string(output))
	labelOutput, err := fenceDocker(ctx, "ps", "-a", "--no-trunc", "--filter", "label=houfeng.ordinary.target="+intent.Token, "--format", "{{.ID}}")
	if err != nil {
		return result, false, errOrdinaryUpgrade
	}
	labelIDs := strings.Fields(string(labelOutput))
	if len(labelIDs) != len(ids) || len(ids) > 1 || (len(ids) == 1 && labelIDs[0] != ids[0]) {
		return result, false, errOrdinaryUpgrade
	}
	if len(ids) == 0 {
		return result, false, nil
	}
	if len(ids) != 1 || !fenceID.MatchString(ids[0]) {
		return result, false, errOrdinaryUpgrade
	}
	body, err := fenceDocker(ctx, "container", "inspect", ids[0])
	if err != nil {
		return result, false, errOrdinaryUpgrade
	}
	var containers []ordinaryImageContainer
	if json.Unmarshal(body, &containers) != nil || len(containers) != 1 || containers[0].ID != ids[0] || ordinaryValidateImageContainer(containers[0], intent.Name, intent.Token, intent.Project+"/"+intent.OperationID, intent.ImageID) != nil {
		return result, false, errOrdinaryUpgrade
	}
	return containers[0], true, nil
}

func ordinaryInstallImageBinary(ctx context.Context, intent ordinaryImageIntent, containerID, directory, name string, parent int) ([32]byte, error) {
	var zero [32]byte
	if observed, ok, err := ordinaryFindImageContainer(ctx, intent); err != nil || !ok || observed.ID != containerID {
		return zero, errOrdinaryUpgrade
	}
	digest, exists, err := ordinaryImageReadExecutable(parent, name)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	copyName := name + ".copy.json"
	if ordinaryRemoveImagePartial(parent, ".record-"+copyName) != nil || ordinaryRemoveImagePartial(parent, ".record-"+name+".json") != nil {
		return zero, errOrdinaryUpgrade
	}
	body, hasIntent, err := ordinaryReadImageRecord(parent, copyName)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	var copyIntent ordinaryImageCopyIntent
	if hasIntent {
		if ordinaryReadCanonicalRecord(body, &copyIntent) != nil || copyIntent.Version != 1 || copyIntent.Name != name || copyIntent.ContainerID != containerID || !ordinaryValidImageTemp(copyIntent.Temp) {
			return zero, errOrdinaryUpgrade
		}
	} else {
		if exists {
			return zero, errOrdinaryUpgrade
		}
		temp, err := ordinaryLaunchTempName()
		if err != nil {
			return zero, errOrdinaryUpgrade
		}
		copyIntent = ordinaryImageCopyIntent{Version: 1, Name: name, ContainerID: containerID, Temp: temp}
		if ordinaryImageRecord(parent, copyName, copyIntent) != nil {
			return zero, errOrdinaryUpgrade
		}
	}
	body, recorded, err := ordinaryReadImageRecord(parent, name+".json")
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	if recorded {
		var record ordinaryImageFileRecord
		if ordinaryReadCanonicalRecord(body, &record) != nil || record.Version != 1 || record.Name != name || !exists || record.SHA256 != digest {
			return zero, errOrdinaryUpgrade
		}
	}
	var tempStat unix.Stat_t
	tempErr := unix.Fstatat(parent, copyIntent.Temp, &tempStat, unix.AT_SYMLINK_NOFOLLOW)
	if tempErr != nil && !errors.Is(tempErr, unix.ENOENT) {
		return zero, errOrdinaryUpgrade
	}
	if exists && tempErr == nil {
		return zero, errOrdinaryUpgrade
	}
	if !exists {
		if ordinaryRemoveImagePartial(parent, copyIntent.Temp) != nil {
			return zero, errOrdinaryUpgrade
		}
		digest, err = ordinaryCopyImageBinaryWithTemp(ctx, containerID, name, directory, copyIntent.Temp)
		if err != nil {
			return zero, errOrdinaryUpgrade
		}
	}
	if observed, ok, err := ordinaryFindImageContainer(ctx, intent); err != nil || !ok || observed.ID != containerID {
		return zero, errOrdinaryUpgrade
	}
	if ordinaryImageRecord(parent, name+".json", ordinaryImageFileRecord{Version: 1, Name: name, SHA256: digest}) != nil {
		return zero, errOrdinaryUpgrade
	}
	return digest, nil
}

// ordinaryPrepareImage runs under the controller's project lock. Its records
// and installed files are a root-owned sibling of the unprivileged journals.
func ordinaryPrepareImage(ctx context.Context, request ordinaryUpgradeRequest) (ordinaryImageTarget, error) {
	var zero ordinaryImageTarget
	if ctx == nil || os.Getuid() != 0 || os.Geteuid() != 0 || !fenceRID.MatchString(request.OperationID) || !fenceProject.MatchString(request.Project) || !filepath.IsAbs(request.WorkingDirectory) || filepath.Clean(request.WorkingDirectory) != request.WorkingDirectory || !strings.HasPrefix(request.TargetImageID, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(request.TargetImageID, "sha256:")) {
		return zero, errOrdinaryUpgrade
	}
	platform, err := ordinaryInspectDockerImage(ctx, request.TargetImageID)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	contract, err := ordinaryCurrentTargetContract()
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	maintenance := filepath.Join(request.WorkingDirectory, "maintenance")
	maintenanceDirs, err := ordinaryImageDirectories(maintenance)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	if ordinaryImageMkdirJournalled(maintenanceDirs[len(maintenanceDirs)-1].fd, "ordinary-targets", request.Project, request.OperationID) != nil || !ordinaryImageRefreshDirectory(maintenanceDirs) {
		for _, dir := range maintenanceDirs {
			_ = unix.Close(dir.fd)
		}
		return zero, errOrdinaryUpgrade
	}
	for _, dir := range maintenanceDirs {
		_ = unix.Close(dir.fd)
	}
	root := filepath.Join(maintenance, "ordinary-targets")
	rootDirs, err := ordinaryImageDirectories(root)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	imageHex := strings.TrimPrefix(request.TargetImageID, "sha256:")
	if ordinaryImageMkdirJournalled(rootDirs[len(rootDirs)-1].fd, imageHex, request.Project, request.OperationID) != nil || !ordinaryImageRefreshDirectory(rootDirs) {
		for _, dir := range rootDirs {
			_ = unix.Close(dir.fd)
		}
		return zero, errOrdinaryUpgrade
	}
	for _, dir := range rootDirs {
		_ = unix.Close(dir.fd)
	}
	directory := filepath.Join(root, imageHex)
	dirs, err := ordinaryImageDirectories(directory)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1].fd
	if ordinaryImageKnownEntries(parent, false) != nil {
		return zero, errOrdinaryUpgrade
	}
	var intent ordinaryImageIntent
	body, exists, err := ordinaryReadImageRecord(parent, "intent.json")
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	if exists {
		if ordinaryReadCanonicalRecord(body, &intent) != nil {
			return zero, errOrdinaryUpgrade
		}
		decodedToken, tokenErr := hex.DecodeString(intent.Token)
		if intent.Version != 1 || intent.Phase != "CREATE" || !fenceProject.MatchString(intent.Project) || !fenceRID.MatchString(intent.OperationID) || intent.ImageID != request.TargetImageID || intent.Platform != platform || len(intent.Token) != 32 || tokenErr != nil || hex.EncodeToString(decodedToken) != intent.Token || intent.Name != "houfeng-ordinary-target-"+intent.Token {
			return zero, errOrdinaryUpgrade
		}
	} else {
		if ordinaryRemoveImagePartial(parent, ".record-intent.json") != nil {
			return zero, errOrdinaryUpgrade
		}
		if ordinaryImageDirectoryEmpty(parent) != nil {
			return zero, errOrdinaryUpgrade
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return zero, errOrdinaryUpgrade
		}
		token := hex.EncodeToString(nonce[:])
		intent = ordinaryImageIntent{Version: 1, Phase: "CREATE", Project: request.Project, OperationID: request.OperationID, ImageID: request.TargetImageID, Platform: platform, Name: "houfeng-ordinary-target-" + token, Token: token}
		if ordinaryImageRecord(parent, "intent.json", intent) != nil {
			return zero, errOrdinaryUpgrade
		}
	}
	if !composeLaunchDirectoriesUnchanged(dirs) {
		return zero, errOrdinaryUpgrade
	}
	completeBody, complete, err := ordinaryReadImageRecord(parent, "complete.json")
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	if ordinaryImageKnownEntries(parent, complete) != nil {
		return zero, errOrdinaryUpgrade
	}
	if complete {
		createBody, created, createErr := ordinaryReadImageRecord(parent, "create.json")
		var createIntent ordinaryImageIntent
		if createErr != nil || !created || ordinaryReadCanonicalRecord(createBody, &createIntent) != nil || createIntent != intent {
			return zero, errOrdinaryUpgrade
		}
		containerBody, recorded, recordErr := ordinaryReadImageRecord(parent, "container.json")
		var provenance ordinaryImageContainerRecord
		if recordErr != nil || !recorded || ordinaryReadCanonicalRecord(containerBody, &provenance) != nil || provenance.Version != 1 || !fenceID.MatchString(provenance.ID) {
			return zero, errOrdinaryUpgrade
		}
		var target ordinaryImageTarget
		if ordinaryReadCanonicalRecord(completeBody, &target) != nil || target.Version != 1 || target.ImageID != intent.ImageID || target.Platform != platform || target.Directory != directory || target.Contract.Version != contract.Version || target.Contract.NativeProtocolVersion != contract.NativeProtocolVersion || target.Contract.SchemaVersion != contract.SchemaVersion || target.Contract.SourceIdentitySHA256 != contract.SourceIdentitySHA256 || !bytes.Equal(target.Contract.CanonicalMigrationSet, contract.CanonicalMigrationSet) || !bytes.Equal(target.Contract.CanonicalPrivilegeSet, contract.CanonicalPrivilegeSet) {
			return zero, errOrdinaryUpgrade
		}
		for _, file := range []struct {
			name   string
			digest [32]byte
		}{
			{"houfeng-record-platform-admin", target.AdminSHA256}, {"houfeng-center", target.CenterSHA256}, {"houfeng-content-processor", target.ProcessorSHA256},
		} {
			digest, present, err := ordinaryImageReadExecutable(parent, file.name)
			record, recorded, recordErr := ordinaryReadImageRecord(parent, file.name+".json")
			var item ordinaryImageFileRecord
			copyBody, copied, copyErr := ordinaryReadImageRecord(parent, file.name+".copy.json")
			var copyIntent ordinaryImageCopyIntent
			if copyErr != nil || !copied || ordinaryReadCanonicalRecord(copyBody, &copyIntent) != nil || copyIntent.Version != 1 || copyIntent.Name != file.name || copyIntent.ContainerID != provenance.ID || !ordinaryValidImageTemp(copyIntent.Temp) {
				return zero, errOrdinaryUpgrade
			}
			if err != nil || !present || digest != file.digest || recordErr != nil || !recorded || ordinaryReadCanonicalRecord(record, &item) != nil || item.Version != 1 || item.Name != file.name || item.SHA256 != digest {
				return zero, errOrdinaryUpgrade
			}
		}
		if ordinaryCleanupImageContainer(ctx, intent, provenance.ID) != nil || !composeLaunchDirectoriesUnchanged(dirs) {
			return zero, errOrdinaryUpgrade
		}
		if _, err := ordinaryProbeAndValidateContract(ctx, directory, target.AdminSHA256, contract); err != nil {
			if errors.Is(err, errOrdinaryControllerExecutable) {
				return zero, err
			}
			return zero, errOrdinaryUpgrade
		}
		return target, nil
	}
	if intent.Project != request.Project || intent.OperationID != request.OperationID {
		return zero, errOrdinaryUpgrade
	}
	container, found, err := ordinaryFindImageContainer(ctx, intent)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	attempt, attempted, err := ordinaryReadImageRecord(parent, "create.json")
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	if attempted {
		var recorded ordinaryImageIntent
		if ordinaryReadCanonicalRecord(attempt, &recorded) != nil || recorded != intent {
			return zero, errOrdinaryUpgrade
		}
	} else if found || ordinaryImageRecord(parent, "create.json", intent) != nil {
		return zero, errOrdinaryUpgrade
	}
	if !found {
		// No extraction or container identity was ever recorded. Reusing the
		// same intent and immutable image is safe only while this directory
		// contains exactly the two pre-create records.
		if ordinaryImageBeforeCreateOnly(parent) != nil || !composeLaunchDirectoriesUnchanged(dirs) {
			return zero, errOrdinaryUpgrade
		}
		createdID, createErr := fenceDocker(ctx, "create", "--pull", "never", "--name", intent.Name, "--label", "houfeng.ordinary.target="+intent.Token, "--label", "houfeng.ordinary.operation="+intent.Project+"/"+intent.OperationID, "--network", "none", "--read-only", "--no-healthcheck", "--log-driver", "none", "--restart", "no", "--user", "10002:10001", "--entrypoint", "/bin/false", intent.ImageID)
		container, found, err = ordinaryFindImageContainer(ctx, intent)
		if err != nil || !found || (createErr == nil && strings.TrimSpace(string(createdID)) != container.ID) {
			return zero, errOrdinaryUpgrade
		}
	}
	containerBody, recorded, err := ordinaryReadImageRecord(parent, "container.json")
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	if recorded {
		var provenance ordinaryImageContainerRecord
		if ordinaryReadCanonicalRecord(containerBody, &provenance) != nil || provenance.Version != 1 || provenance.ID != container.ID {
			return zero, errOrdinaryUpgrade
		}
	} else {
		for _, name := range []string{"houfeng-record-platform-admin", "houfeng-center", "houfeng-content-processor"} {
			_, present, err := ordinaryImageReadExecutable(parent, name)
			if err != nil || present {
				return zero, errOrdinaryUpgrade
			}
		}
		if ordinaryImageRecord(parent, "container.json", ordinaryImageContainerRecord{Version: 1, ID: container.ID}) != nil {
			return zero, errOrdinaryUpgrade
		}
	}
	target := ordinaryImageTarget{Version: 1, ImageID: intent.ImageID, Platform: platform, Directory: directory}
	target.AdminSHA256, err = ordinaryInstallImageBinary(ctx, intent, container.ID, directory, "houfeng-record-platform-admin", parent)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	target.CenterSHA256, err = ordinaryInstallImageBinary(ctx, intent, container.ID, directory, "houfeng-center", parent)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	target.ProcessorSHA256, err = ordinaryInstallImageBinary(ctx, intent, container.ID, directory, "houfeng-content-processor", parent)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	target.Contract, err = ordinaryProbeAndValidateContract(ctx, directory, target.AdminSHA256, contract)
	if err != nil {
		if errors.Is(err, errOrdinaryControllerExecutable) {
			return zero, err
		}
		return zero, errOrdinaryUpgrade
	}
	if !ordinaryImageRefreshDirectory(dirs) {
		return zero, errOrdinaryUpgrade
	}
	if ordinaryImageRecord(parent, "complete.json", target) != nil || ordinaryCleanupImageContainer(ctx, intent, container.ID) != nil || !composeLaunchDirectoriesUnchanged(dirs) {
		return zero, errOrdinaryUpgrade
	}
	return target, nil
}

func ordinaryCleanupImageContainer(ctx context.Context, intent ordinaryImageIntent, expectedID string) error {
	container, found, err := ordinaryFindImageContainer(ctx, intent)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !found {
		return nil
	}
	if container.ID != expectedID {
		return errOrdinaryUpgrade
	}
	if _, err := fenceDocker(ctx, "rm", container.ID); err != nil {
		return errOrdinaryUpgrade
	}
	_, found, err = ordinaryFindImageContainer(ctx, intent)
	if err != nil || found {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryImageBeforeCreateOnly(parent int) error {
	fd, err := unix.Openat(parent, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary target directory")
	defer file.Close()
	names, err := file.Readdirnames(3)
	if (err != nil && err != io.EOF) || len(names) != 2 {
		return errOrdinaryUpgrade
	}
	if !((names[0] == "intent.json" && names[1] == "create.json") || (names[1] == "intent.json" && names[0] == "create.json")) {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryImageKnownEntries(parent int, complete bool) error {
	fd, err := unix.Openat(parent, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary target directory")
	defer file.Close()
	names, err := file.Readdirnames(32)
	if (err != nil && err != io.EOF) || len(names) == 32 {
		return errOrdinaryUpgrade
	}
	for _, name := range names {
		switch name {
		case "intent.json", "create.json", "container.json", "complete.json",
			"houfeng-record-platform-admin", "houfeng-record-platform-admin.json", "houfeng-record-platform-admin.copy.json",
			"houfeng-center", "houfeng-center.json", "houfeng-center.copy.json",
			"houfeng-content-processor", "houfeng-content-processor.json", "houfeng-content-processor.copy.json":
		case ".record-intent.json", ".record-create.json", ".record-container.json", ".record-complete.json",
			".record-houfeng-record-platform-admin.json", ".record-houfeng-record-platform-admin.copy.json",
			".record-houfeng-center.json", ".record-houfeng-center.copy.json",
			".record-houfeng-content-processor.json", ".record-houfeng-content-processor.copy.json":
			if complete {
				return errOrdinaryUpgrade
			}
		default:
			if complete || !ordinaryImageJournalledTemp(parent, name) {
				return errOrdinaryUpgrade
			}
		}
	}
	return nil
}

func ordinaryImageDirectoryEmpty(parent int) error {
	fd, err := unix.Openat(parent, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary target directory")
	defer file.Close()
	names, err := file.Readdirnames(1)
	if err != io.EOF || len(names) != 0 {
		return errOrdinaryUpgrade
	}
	return nil
}

// Names passed here are fixed record temporaries or are bound by a durable
// copy intent. Never unlink a link, foreign owner, or unexpected file mode.
func ordinaryRemoveImagePartial(parent int, name string) error {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	var st, named unix.Stat_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || (st.Gid != 0 && st.Gid != composeLaunchRuntimeGID) || (st.Mode&0o7777 != 0o550 && st.Mode&0o7777&^0o600 != 0) || st.Nlink != 1 || !sameSourceFile(st, named) {
		return errOrdinaryUpgrade
	}
	if unix.Unlinkat(parent, name, 0) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

type ordinaryImageCopyIntent struct {
	Version     int    `json:"version"`
	Name        string `json:"name"`
	ContainerID string `json:"container_id"`
	Temp        string `json:"temp"`
}

func ordinaryValidImageTemp(name string) bool {
	const prefix = ".ordinary-launch-"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
		return false
	}
	value, err := hex.DecodeString(name[len(prefix):])
	return err == nil && hex.EncodeToString(value) == name[len(prefix):]
}

func ordinaryImageJournalledTemp(parent int, temp string) bool {
	if !ordinaryValidImageTemp(temp) {
		return false
	}
	body, found, err := ordinaryReadImageRecord(parent, "container.json")
	var container ordinaryImageContainerRecord
	if err != nil || !found || ordinaryReadCanonicalRecord(body, &container) != nil || container.Version != 1 || !fenceID.MatchString(container.ID) {
		return false
	}
	matches := 0
	for _, name := range []string{"houfeng-record-platform-admin", "houfeng-center", "houfeng-content-processor"} {
		body, found, err := ordinaryReadImageRecord(parent, name+".copy.json")
		if err != nil {
			return false
		}
		if !found {
			continue
		}
		var record ordinaryImageCopyIntent
		if ordinaryReadCanonicalRecord(body, &record) != nil || record.Version != 1 || record.Name != name || record.ContainerID != container.ID || !ordinaryValidImageTemp(record.Temp) {
			return false
		}
		if record.Temp == temp {
			matches++
		}
	}
	return matches == 1
}

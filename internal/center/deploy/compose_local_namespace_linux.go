//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/store"
)

const (
	composeNamespaceRoot         = "/var/lib/houfeng/attachments"
	composeNamespaceIdentityDir  = "/var/lib/houfeng/local-namespace-init"
	composeNamespacePasswordPath = "/run/secrets/houfeng_platform_admin_password"
	composeNamespaceUID          = 10001
	composeNamespaceGID          = 10001
)

var ErrComposeLocalNamespace = errors.New("Compose local namespace initialization failed")

var errComposeNamespaceSourceAdoption = errors.New("source adoption or recovery belongs to the source path")

type composeLocalNamespaceRepository interface {
	attachments.LocalNamespaceAdministrator
	attachments.LocalNamespaceRepository
	ComposeSourceEnrolled(context.Context) (bool, error)
}

type composeNamespaceIdentity struct {
	Version       int    `json:"version"`
	NamespaceID   string `json:"namespace_id"`
	IncarnationID string `json:"incarnation_id"`
	RequestID     string `json:"request_id"`
}

// InitializeComposeLocalNamespace requires the caller to hold the ordinary
// management guard and to ensure external writers and restart controllers are
// stopped. This initializer does not establish that external stop proof.
func InitializeComposeLocalNamespace(ctx context.Context, backend string) error {
	if os.Getuid() != composeNamespaceUID || os.Geteuid() != composeNamespaceUID || os.Getgid() != composeNamespaceGID || os.Getegid() != composeNamespaceGID {
		return ErrComposeLocalNamespace
	}
	switch backend {
	case "s3":
		return nil
	case "local":
	default:
		return ErrComposeLocalNamespace
	}
	password, err := readComposeNamespacePassword(composeNamespacePasswordPath)
	if err != nil {
		return ErrComposeLocalNamespace
	}
	defer clear(password)
	pool, err := openComposePostgres(ctx, composePostgresEndpoint{
		Host: composeDatabaseHost, Port: composeDatabasePort, Database: composeDatabaseName,
		Role: composeAdminRole, Password: string(password),
	})
	if err != nil || pool == nil {
		if pool != nil {
			pool.Close()
		}
		return ErrComposeLocalNamespace
	}
	defer pool.Close()
	var direct bool
	if err := pool.QueryRow(ctx, `select session_user = $1 and current_user = session_user and current_setting('role') = 'none' and current_database() = $2`, composeAdminRole, composeDatabaseName).Scan(&direct); err != nil || !direct {
		return ErrComposeLocalNamespace
	}
	repository := store.NewPostgresLocalBlobRepository(pool, nil, nil)
	hasBinding := func(ctx context.Context, _ string) (bool, error) {
		var exists bool
		err := pool.QueryRow(ctx, `select exists(select 1 from public.local_blob_namespaces)`).Scan(&exists)
		return exists, err
	}
	if err := initializeComposeLocalNamespace(ctx, composeNamespaceIdentityDir, composeNamespaceRoot, composeNamespaceUID, composeNamespaceGID, repository, hasBinding); err != nil {
		if errors.Is(err, errComposeNamespaceSourceAdoption) {
			return errComposeNamespaceSourceAdoption
		}
		return ErrComposeLocalNamespace
	}
	return nil
}

// The descriptor-based reader is specific to the separately staged credential;
// it does not relax the private one-shot request reader.
func readComposeNamespacePassword(path string) ([]byte, error) {
	dirs, err := openComposeLaunchDirectories(filepath.Dir(path))
	if err != nil {
		return nil, ErrComposeLocalNamespace
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1]
	if parent.before.Uid != 10002 || parent.before.Gid != composeNamespaceGID || parent.before.Mode&07777 != 0750 {
		return nil, ErrComposeLocalNamespace
	}
	fd, err := unix.Openat2(parent.fd, filepath.Base(path), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, ErrComposeLocalNamespace
	}
	file := os.NewFile(uintptr(fd), "namespace-credential")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0440 || before.Uid != 10002 || before.Gid != composeNamespaceGID || before.Nlink != 1 || before.Size <= 0 {
		return nil, ErrComposeLocalNamespace
	}
	data, err := io.ReadAll(file)
	if err != nil || int64(len(data)) != before.Size || !validComposePassword(string(data)) || unix.Fstat(fd, &after) != nil || unix.Fstatat(parent.fd, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameComposeNamespaceFile(before, after) || !sameComposeNamespaceFile(before, named) || !composeLaunchDirectoriesUnchanged(dirs) {
		clear(data)
		return nil, ErrComposeLocalNamespace
	}
	return data, nil
}
func sameComposeNamespaceFile(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func initializeComposeLocalNamespace(ctx context.Context, identityDir, root string, uid, gid uint32, repository composeLocalNamespaceRepository, hasBinding func(context.Context, string) (bool, error)) error {
	if ctx == nil || repository == nil || hasBinding == nil || !filepath.IsAbs(identityDir) || filepath.Clean(identityDir) != identityDir || !filepath.IsAbs(root) || filepath.Clean(root) != root || identityDir == root || strings.HasPrefix(identityDir, root+"/") {
		return ErrComposeLocalNamespace
	}
	enrolled, err := repository.ComposeSourceEnrolled(ctx)
	if err != nil {
		return ErrComposeLocalNamespace
	}
	if enrolled {
		// Source-recovery bindings belong to the source recovery path, not workline 00.
		binding, err := repository.LoadLocalNamespace(ctx, root)
		if err != nil || binding.SourceRecoveryID != nil {
			return errComposeNamespaceSourceAdoption
		}
		blob, err := attachments.NewLocalBlobStore(ctx, root, repository)
		if err != nil {
			return errComposeNamespaceSourceAdoption
		}
		if err := blob.Close(); err != nil {
			return errComposeNamespaceSourceAdoption
		}
		return nil
	}
	// Provisioning remains restricted to a direct administrator without source enrollment.
	if err := repository.CheckLocalProvisionAuthority(ctx); err != nil {
		return ErrComposeLocalNamespace
	}
	dir, err := unix.Openat2(unix.AT_FDCWD, identityDir, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return ErrComposeLocalNamespace
	}
	defer unix.Close(dir)
	if !composeNamespaceStat(dir, unix.S_IFDIR, uid, gid, 0700, false) {
		return ErrComposeLocalNamespace
	}
	lock, err := unix.Openat2(dir, "bootstrap.lock", &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CREAT | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Mode: 0600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return ErrComposeLocalNamespace
	}
	defer unix.Close(lock)
	if !composeNamespaceStat(lock, unix.S_IFREG, uid, gid, 0600, true) || unix.Fsync(lock) != nil || unix.Fsync(dir) != nil {
		return ErrComposeLocalNamespace
	}
	for {
		err = unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			return ErrComposeLocalNamespace
		}
		select {
		case <-ctx.Done():
			return ErrComposeLocalNamespace
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	var lockStat, namedLock unix.Stat_t
	if unix.Fstat(lock, &lockStat) != nil || unix.Fstatat(dir, "bootstrap.lock", &namedLock, unix.AT_SYMLINK_NOFOLLOW) != nil || lockStat.Dev != namedLock.Dev || lockStat.Ino != namedLock.Ino {
		return ErrComposeLocalNamespace
	}
	identity, absent, err := readComposeNamespaceIdentity(dir, uid, gid)
	if err != nil {
		return ErrComposeLocalNamespace
	}
	// The initializer has no blob-table credential. Cold upgrades separately
	// recheck existing local references with the fenced bootstrap observer;
	// this participant checks the physical tree under its existing guard.
	if absent {
		if err := composeNamespaceRequireFresh(ctx, root, uid, gid, hasBinding); err != nil {
			return ErrComposeLocalNamespace
		}
		identity = composeNamespaceIdentity{Version: 1}
		if identity.NamespaceID, err = composeNamespaceUUID(); err != nil {
			return ErrComposeLocalNamespace
		}
		if identity.IncarnationID, err = composeNamespaceUUID(); err != nil {
			return ErrComposeLocalNamespace
		}
		if identity.RequestID, err = composeNamespaceUUID(); err != nil {
			return ErrComposeLocalNamespace
		}
		if err := publishComposeNamespaceIdentity(dir, identity); err != nil {
			return ErrComposeLocalNamespace
		}
	}
	_, err = attachments.ProvisionLocalNamespace(ctx, attachments.LocalProvisionRequest{Root: root, NamespaceID: identity.NamespaceID, IncarnationID: identity.IncarnationID, RequestID: identity.RequestID, OwnerUID: uid, OwnerGID: gid}, repository)
	if err != nil {
		return ErrComposeLocalNamespace
	}
	return nil
}

// Both the initializer and cold controller use this exact absent-identity
// rule before publishing any namespace identity or fencing the old workload.
func composeNamespaceRequireFresh(ctx context.Context, root string, uid, gid uint32, hasBinding func(context.Context, string) (bool, error)) error {
	bound, err := hasBinding(ctx, root)
	if err != nil || bound || !composeNamespaceRootEmpty(root, uid, gid) {
		return ErrComposeLocalNamespace
	}
	return nil
}

func composeNamespaceStat(fd int, kind, uid, gid, mode uint32, singleLink bool) bool {
	var st unix.Stat_t
	return unix.Fstat(fd, &st) == nil && st.Mode&unix.S_IFMT == kind && st.Mode&07777 == mode && st.Uid == uid && st.Gid == gid && st.Nlink > 0 && (!singleLink || st.Nlink == 1)
}

func composeNamespaceRootEmpty(path string, uid, gid uint32) bool {
	return attachments.FreshLocalNamespaceRoot(path, uid, gid)
}

func composeNamespaceUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	var result [36]byte
	hex.Encode(result[0:8], b[0:4])
	result[8] = '-'
	hex.Encode(result[9:13], b[4:6])
	result[13] = '-'
	hex.Encode(result[14:18], b[6:8])
	result[18] = '-'
	hex.Encode(result[19:23], b[8:10])
	result[23] = '-'
	hex.Encode(result[24:36], b[10:16])
	return string(result[:]), nil
}

func validComposeNamespaceUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func readComposeNamespaceIdentity(dir int, uid, gid uint32) (composeNamespaceIdentity, bool, error) {
	var identity composeNamespaceIdentity
	fd, err := unix.Openat2(dir, "identity.json", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if errors.Is(err, unix.ENOENT) {
		return identity, true, nil
	}
	if err != nil {
		return identity, false, err
	}
	file := os.NewFile(uintptr(fd), "namespace-identity")
	defer file.Close()
	if !composeNamespaceStat(fd, unix.S_IFREG, uid, gid, 0600, true) {
		return identity, false, ErrComposeLocalNamespace
	}
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil {
		return identity, false, ErrComposeLocalNamespace
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 || json.Unmarshal(data, &identity) != nil || unix.Fstat(fd, &after) != nil || unix.Fstatat(dir, "identity.json", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameComposeNamespaceFile(before, after) || !sameComposeNamespaceFile(before, named) {
		return identity, false, ErrComposeLocalNamespace
	}
	canonical, err := json.Marshal(identity)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) || identity.Version != 1 || !validComposeNamespaceUUID(identity.NamespaceID) || !validComposeNamespaceUUID(identity.IncarnationID) || !validComposeNamespaceUUID(identity.RequestID) || identity.NamespaceID == identity.IncarnationID || identity.NamespaceID == identity.RequestID || identity.IncarnationID == identity.RequestID {
		return identity, false, ErrComposeLocalNamespace
	}
	return identity, false, nil
}

func publishComposeNamespaceIdentity(dir int, identity composeNamespaceIdentity) error {
	body, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	name, err := composeNamespaceUUID()
	if err != nil {
		return err
	}
	name = ".identity-" + name
	fd, err := unix.Openat2(dir, name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return err
	}
	defer unix.Unlinkat(dir, name, 0)
	file := os.NewFile(uintptr(fd), "namespace-identity")
	body = append(body, '\n')
	n, writeErr := file.Write(body)
	if writeErr == nil && n != len(body) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := unix.Renameat2(dir, name, dir, "identity.json", unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return unix.Fsync(dir)
}

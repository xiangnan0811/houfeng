//go:build linux

package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	localMarkerName        = ".houfeng-local-namespace"
	localMarkerTempPrefix  = ".houfeng-local-namespace.tmp-"
	localProvisionLockName = ".houfeng-local-provision.lock"
	localResolve           = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV
)

type LocalProvisionRequest struct {
	Root             string
	NamespaceID      string
	IncarnationID    string
	RequestID        string
	OwnerUID         uint32
	OwnerGID         uint32
	SourceRecoveryID *string
}

type localNamespaceMarker struct {
	Version            int    `json:"version"`
	NamespaceID        string `json:"namespace_id"`
	IncarnationID      string `json:"incarnation_id"`
	RequestID          string `json:"request_id"`
	Nonce              string `json:"nonce"`
	ProvisionLockInode uint64 `json:"provision_lock_inode"`
	ProvisionLockNonce string `json:"provision_lock_nonce"`
}

type localBlobDescriptors struct {
	root      int
	digest    int
	locks     int
	provision int
}

func emptyLocalDescriptors() localBlobDescriptors {
	return localBlobDescriptors{root: -1, digest: -1, locks: -1, provision: -1}
}

func (files *localBlobDescriptors) close() error {
	var result error
	for _, fd := range []*int{&files.provision, &files.locks, &files.digest, &files.root} {
		if *fd >= 0 {
			result = errors.Join(result, unix.Close(*fd))
			*fd = -1
		}
	}
	return result
}

func openLocalAt(directory int, name string, flags int, mode uint32) (int, error) {
	return unix.Openat2(directory, name, &unix.OpenHow{
		Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Mode:  uint64(mode), Resolve: localResolve,
	})
}

func localStat(fd int, kind uint32, uid, gid uint32, mode uint32) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return stat, err
	}
	if stat.Mode&unix.S_IFMT != kind || stat.Mode&0o7777 != mode || stat.Uid != uid || stat.Gid != gid {
		return stat, ErrLocalIdentityChanged
	}
	if kind == unix.S_IFREG && stat.Nlink == 0 {
		return stat, ErrLocalIdentityChanged
	}
	return stat, nil
}

func localFlock(ctx context.Context, fd, operation int, cutpoint func(localBlobCutpoint) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(fd, operation|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return err
		}
		if cutpoint != nil && (errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)) {
			if err := cutpoint(localBlobGuardWaiting); err != nil {
				return err
			}
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func localReadFile(fd int, maximum int) ([]byte, error) {
	data := make([]byte, maximum+1)
	n, err := unix.Pread(fd, data, 0)
	if err != nil {
		return nil, err
	}
	if n > maximum {
		return nil, ErrLocalIdentityChanged
	}
	return data[:n], nil
}

func localWriteFile(fd int, data []byte) error {
	for position := 0; position < len(data); {
		n, err := unix.Pwrite(fd, data[position:], int64(position))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		position += n
	}
	return unix.Fsync(fd)
}

func openLocalRoot(path string, create bool, uid, gid uint32) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, 0) {
		return -1, ErrInvalidBlobStoreConfig
	}
	if create {
		parent, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
			Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
			Resolve: unix.RESOLVE_NO_SYMLINKS,
		})
		if err != nil {
			return -1, err
		}
		defer unix.Close(parent)
		var stat unix.Stat_t
		if err := unix.Fstat(parent, &stat); err != nil {
			return -1, err
		}
		if stat.Mode&0o022 != 0 || (stat.Uid != uid && stat.Uid != 0) {
			return -1, ErrLocalIdentityChanged
		}
		if err := unix.Mkdirat(parent, filepath.Base(path), 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, err
		}
		if err := unix.Fsync(parent); err != nil {
			return -1, err
		}
	}
	// BlobRoot itself may be a mount. It becomes the fixed anchor; descendants
	// are always opened with NO_XDEV, BENEATH and NO_SYMLINKS.
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return -1, err
	}
	if _, err := localStat(fd, unix.S_IFDIR, uid, gid, 0o700); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func openLocalProvisionLock(ctx context.Context, root int, provision, create bool, uid, gid uint32) (int, error) {
	fd, err := openLocalAt(root, localProvisionLockName, unix.O_RDWR, 0)
	if errors.Is(err, unix.ENOENT) && create {
		fd, err = openLocalAt(root, localProvisionLockName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, 0o600)
		if errors.Is(err, unix.EEXIST) {
			fd, err = openLocalAt(root, localProvisionLockName, unix.O_RDWR, 0)
		}
	}
	if err != nil {
		return -1, err
	}
	fail := func(err error) (int, error) { unix.Close(fd); return -1, err }
	operation := unix.LOCK_SH
	if provision {
		operation = unix.LOCK_EX
	}
	if err := localFlock(ctx, fd, operation, nil); err != nil {
		return fail(err)
	}
	stat, err := localStat(fd, unix.S_IFREG, uid, gid, 0o600)
	if err != nil || stat.Nlink != 1 {
		return fail(ErrLocalIdentityChanged)
	}
	if stat.Size == 0 && create {
		nonce, err := newLocalUUID()
		if err != nil {
			return fail(err)
		}
		if err := localWriteFile(fd, []byte(nonce)); err != nil {
			return fail(err)
		}
		if err := unix.Fsync(root); err != nil {
			return fail(err)
		}
	}
	data, err := localReadFile(fd, 36)
	if err != nil || !validLocalUUID(string(data)) {
		return fail(ErrLocalIdentityChanged)
	}
	var named unix.Stat_t
	if err := unix.Fstatat(root, localProvisionLockName, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || named.Ino != stat.Ino || named.Dev != stat.Dev {
		return fail(ErrLocalIdentityChanged)
	}
	return fd, nil
}

func validLocalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
		} else if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func localBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if !validLocalUUID(value) {
		return "", ErrLocalIdentityChanged
	}
	return value, nil
}

func readLocalMarker(root int, uid, gid uint32) (localNamespaceMarker, unix.Stat_t, []byte, error) {
	var marker localNamespaceMarker
	var stat unix.Stat_t
	fd, err := openLocalAt(root, localMarkerName, unix.O_RDONLY, 0)
	if err != nil {
		return marker, stat, nil, err
	}
	defer unix.Close(fd)
	stat, err = localStat(fd, unix.S_IFREG, uid, gid, 0o600)
	if err != nil || stat.Nlink != 1 {
		return marker, stat, nil, ErrLocalIdentityChanged
	}
	data, err := localReadFile(fd, 4096)
	if err != nil {
		return marker, stat, nil, err
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		return marker, stat, nil, ErrLocalIdentityChanged
	}
	canonical, err := json.Marshal(marker)
	if err != nil || !bytes.Equal(canonical, data) || marker.Version != 2 || !validLocalUUID(marker.NamespaceID) || !validLocalUUID(marker.IncarnationID) || !validLocalUUID(marker.RequestID) || !validLocalUUID(marker.Nonce) {
		return marker, stat, nil, ErrLocalIdentityChanged
	}
	return marker, stat, data, nil
}

// A marker nonce can be reconstructed after an interrupted temporary write
// from the durable random provision-lock nonce and the request identity.
func localMarkerForRequest(request LocalProvisionRequest, lockStat unix.Stat_t, lockNonce []byte) localNamespaceMarker {
	hash := sha256.New()
	_, _ = hash.Write([]byte("HOUFENG-LOCAL-NAMESPACE-MARKER-NONCE-V1\x00"))
	_, _ = hash.Write(lockNonce)
	_, _ = hash.Write([]byte(request.NamespaceID + request.IncarnationID + request.RequestID))
	digest := hash.Sum(nil)
	digest[6] = (digest[6] & 0x0f) | 0x40
	digest[8] = (digest[8] & 0x3f) | 0x80
	nonce := fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
	return localNamespaceMarker{Version: 2, NamespaceID: request.NamespaceID, IncarnationID: request.IncarnationID, RequestID: request.RequestID, Nonce: nonce,
		ProvisionLockInode: lockStat.Ino, ProvisionLockNonce: string(lockNonce)}
}

// The request-bound name and exact content prefix identify an interrupted
// write, including an empty file. Never remove a mismatched or unsafe file.
func cleanupLocalMarkerTemp(root int, name string, expected []byte, uid, gid uint32) error {
	fd, err := openLocalAt(root, name, unix.O_RDONLY, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	before, err := localStat(fd, unix.S_IFREG, uid, gid, 0o600)
	if err != nil || before.Nlink != 1 {
		return ErrLocalIdentityChanged
	}
	data, err := localReadFile(fd, 4096)
	if err != nil || !bytes.HasPrefix(expected, data) {
		return ErrLocalIdentityChanged
	}
	var after, named unix.Stat_t
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(root, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		after.Dev != named.Dev || after.Ino != named.Ino || after.Mode != named.Mode || after.Uid != named.Uid || after.Gid != named.Gid || after.Nlink != named.Nlink || after.Size != named.Size || after.Mtim != named.Mtim || after.Ctim != named.Ctim {
		return ErrLocalIdentityChanged
	}
	if err := unix.Unlinkat(root, name, 0); err != nil {
		return err
	}
	return unix.Fsync(root)
}

// ProvisionLocalNamespace is an offline, one-shot filesystem participant. A
// source-enabled tree requires its outer coordinator and is not activated here.
func ProvisionLocalNamespace(ctx context.Context, request LocalProvisionRequest, repository LocalNamespaceAdministrator) (LocalNamespace, error) {
	var result LocalNamespace
	if ctx == nil || nilUploadServiceDependency(repository) || !validLocalUUID(request.NamespaceID) || !validLocalUUID(request.IncarnationID) || !validLocalUUID(request.RequestID) || request.OwnerUID != uint32(os.Geteuid()) || request.OwnerGID != uint32(os.Getegid()) {
		return result, ErrInvalidBlobStoreConfig
	}
	if request.SourceRecoveryID != nil {
		return result, fmt.Errorf("%w: source outer local handoff capability unavailable", ErrLocalNamespaceClosed)
	}
	if err := repository.CheckLocalProvisionAuthority(ctx); err != nil {
		return result, err
	}
	existing, lookupErr := repository.LoadLocalNamespace(ctx, request.Root)
	if lookupErr != nil && !errors.Is(lookupErr, ErrBlobNotFound) {
		return result, lookupErr
	}
	known := lookupErr == nil
	if known && (existing.NamespaceID != request.NamespaceID || existing.IncarnationID != request.IncarnationID || existing.RequestID != request.RequestID) {
		return result, ErrLocalIdentityChanged
	}
	files := emptyLocalDescriptors()
	defer files.close()
	var err error
	files.root, err = openLocalRoot(request.Root, !known, request.OwnerUID, request.OwnerGID)
	if err != nil {
		return result, err
	}
	files.provision, err = openLocalProvisionLock(ctx, files.root, true, !known, request.OwnerUID, request.OwnerGID)
	if err != nil {
		return result, err
	}
	tempName := localMarkerTempPrefix + request.RequestID
	marker, _, markerData, err := readLocalMarker(files.root, request.OwnerUID, request.OwnerGID)
	if errors.Is(err, unix.ENOENT) {
		if known {
			return result, ErrLocalIdentityChanged
		}
		lockStat, lockErr := localStat(files.provision, unix.S_IFREG, request.OwnerUID, request.OwnerGID, 0o600)
		if lockErr != nil {
			return result, lockErr
		}
		lockNonce, lockErr := localReadFile(files.provision, 36)
		if lockErr != nil || !validLocalUUID(string(lockNonce)) {
			return result, ErrLocalIdentityChanged
		}
		marker = localMarkerForRequest(request, lockStat, lockNonce)
		data, marshalErr := json.Marshal(marker)
		if marshalErr != nil {
			return result, marshalErr
		}
		if err := cleanupLocalMarkerTemp(files.root, tempName, data, request.OwnerUID, request.OwnerGID); err != nil {
			return result, err
		}
		// An unbound root may contain only the exact empty released skeleton.
		if err := localValidateUnboundEntries(files.root, false, true); err != nil {
			return result, err
		}
		fd, createErr := openLocalAt(files.root, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
		if createErr != nil {
			return result, createErr
		}
		writeErr := localWriteFile(fd, data)
		closeErr := unix.Close(fd)
		if err := errors.Join(writeErr, closeErr); err != nil {
			return result, err
		}
		if err := unix.Renameat2(files.root, tempName, files.root, localMarkerName, unix.RENAME_NOREPLACE); err != nil {
			return result, err
		}
		if err := unix.Fsync(files.root); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	} else {
		if marker.NamespaceID != request.NamespaceID || marker.IncarnationID != request.IncarnationID || marker.RequestID != request.RequestID {
			return result, ErrLocalIdentityChanged
		}
		if err := cleanupLocalMarkerTemp(files.root, tempName, markerData, request.OwnerUID, request.OwnerGID); err != nil {
			return result, err
		}
	}
	if !known {
		if err := localValidateUnboundEntries(files.root, true, true); err != nil {
			return result, err
		}
		for _, name := range []string{"sha256", ".locks"} {
			if err := unix.Mkdirat(files.root, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
				return result, err
			}
			fd, err := openLocalAt(files.root, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
			if err != nil {
				return result, err
			}
			entries, readErr := localDirectoryNames(fd)
			closeErr := unix.Close(fd)
			if readErr != nil || closeErr != nil {
				return result, errors.Join(readErr, closeErr)
			}
			if len(entries) != 0 {
				return result, fmt.Errorf("%w: legacy_capability_unresolved", ErrLocalNamespaceClosed)
			}
		}
	}
	if err := unix.Fsync(files.root); err != nil {
		return result, err
	}
	files.digest, err = openLocalAt(files.root, "sha256", unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return result, err
	}
	files.locks, err = openLocalAt(files.root, ".locks", unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return result, err
	}
	result, err = inspectLocalNamespace(request.Root, files, request.OwnerUID, request.OwnerGID)
	if err != nil {
		return result, err
	}
	if known && !sameLocalNamespaceIdentity(existing, result) {
		return result, ErrLocalIdentityChanged
	}
	bound, err := repository.ProvisionLocalNamespace(ctx, result)
	if err != nil {
		resolved, resolveErr := repository.LoadLocalNamespace(ctx, request.Root)
		if resolveErr != nil || !sameLocalNamespaceIdentity(result, resolved) {
			return result, errors.Join(ErrLocalNamespaceClosed, err, resolveErr)
		}
		bound = resolved
	}
	if !sameLocalNamespaceIdentity(result, bound) {
		return result, ErrLocalIdentityChanged
	}
	admitted, err := repository.AdmitLocalNamespace(ctx, LocalNamespaceAdmission{NamespaceID: result.NamespaceID, IncarnationID: result.IncarnationID, RequestID: result.RequestID, MarkerDigest: result.MarkerDigest})
	if err != nil {
		admitted, err = repository.LoadLocalNamespace(ctx, request.Root)
		if err != nil {
			return result, errors.Join(ErrLocalNamespaceClosed, err)
		}
	}
	if admitted.AdmissionState != "active" || !sameLocalNamespaceIdentity(result, admitted) {
		return result, ErrLocalNamespaceClosed
	}
	return admitted, nil
}

// The accepted names are shared by provisioning and its read-only preflight.
func localValidateUnboundEntries(root int, markerPresent, allowProvisionLock bool) error {
	entries, err := localDirectoryNames(root)
	if err != nil {
		return err
	}
	for _, name := range entries {
		if (allowProvisionLock && name == localProvisionLockName) || (markerPresent && (name == localMarkerName || name == "sha256" || name == ".locks")) {
			continue
		}
		if !markerPresent && name == "sha256" {
			var rootStat unix.Stat_t
			if unix.Fstat(root, &rootStat) != nil {
				return ErrLocalIdentityChanged
			}
			fd, err := openLocalAt(root, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
			if err != nil {
				return ErrLocalIdentityChanged
			}
			stat, statErr := localStat(fd, unix.S_IFDIR, rootStat.Uid, rootStat.Gid, 0o700)
			children, readErr := localDirectoryNames(fd)
			closeErr := unix.Close(fd)
			if statErr != nil || stat.Nlink == 0 || readErr != nil || closeErr != nil || len(children) != 0 {
				return fmt.Errorf("%w: legacy_capability_unresolved", ErrLocalNamespaceClosed)
			}
			continue
		}
		return fmt.Errorf("%w: legacy_capability_unresolved", ErrLocalNamespaceClosed)
	}
	return nil
}

func localDirectoryNames(directory int) ([]string, error) {
	fd, err := openLocalAt(directory, ".", unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "local-blob-directory")
	defer file.Close()
	return file.Readdirnames(-1)
}

func inspectLocalNamespace(path string, files localBlobDescriptors, uid, gid uint32) (LocalNamespace, error) {
	var n LocalNamespace
	root, err := localStat(files.root, unix.S_IFDIR, uid, gid, 0o700)
	if err != nil {
		return n, err
	}
	digest, err := localStat(files.digest, unix.S_IFDIR, uid, gid, 0o700)
	if err != nil {
		return n, err
	}
	locks, err := localStat(files.locks, unix.S_IFDIR, uid, gid, 0o700)
	if err != nil {
		return n, err
	}
	marker, markerStat, body, err := readLocalMarker(files.root, uid, gid)
	if err != nil {
		return n, err
	}
	provision, err := localStat(files.provision, unix.S_IFREG, uid, gid, 0o600)
	if err != nil || provision.Nlink != 1 || provision.Dev != root.Dev || provision.Ino != marker.ProvisionLockInode {
		return n, ErrLocalIdentityChanged
	}
	provisionNonce, err := localReadFile(files.provision, 36)
	if err != nil || !validLocalUUID(string(provisionNonce)) || string(provisionNonce) != marker.ProvisionLockNonce {
		return n, ErrLocalIdentityChanged
	}
	if root.Dev != digest.Dev || root.Dev != locks.Dev || root.Dev != markerStat.Dev {
		return n, ErrLocalIdentityChanged
	}
	boot, err := localBootID()
	if err != nil {
		return n, err
	}
	hash := sha256.Sum256(body)
	n = LocalNamespace{NamespaceID: marker.NamespaceID, IncarnationID: marker.IncarnationID, RequestID: marker.RequestID,
		RootPath: path, BootID: boot, Device: root.Dev, RootInode: root.Ino, DigestInode: digest.Ino, LocksInode: locks.Ino,
		MarkerInode: markerStat.Ino, OwnerUID: uid, OwnerGID: gid, MarkerDigest: hexDigest(hash), MarkerNonce: marker.Nonce}
	return n, nil
}

func sameLocalNamespaceIdentity(left, right LocalNamespace) bool {
	if !sameOptionalString(left.SourceRecoveryID, right.SourceRecoveryID) {
		return false
	}
	left.SourceRecoveryID, right.SourceRecoveryID = nil, nil
	left.AdmissionState, right.AdmissionState = "", ""
	return left == right
}

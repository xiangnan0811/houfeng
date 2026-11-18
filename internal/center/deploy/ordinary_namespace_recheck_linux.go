//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
)

type ordinaryNamespaceBaseline struct {
	Check        bool   `json:"check"`
	Root         string `json:"root"`
	RootDevice   uint64 `json:"root_device"`
	RootInode    uint64 `json:"root_inode"`
	DigestDevice uint64 `json:"digest_device"`
	DigestInode  uint64 `json:"digest_inode"`
}

// Capture only unbound local roots; an admitted namespace has a separate exact
// physical identity and can legitimately have existing local blob references.
func ordinaryCaptureNamespaceBaseline(recipe ordinaryNamespaceRecipe) (ordinaryNamespaceBaseline, error) {
	var result ordinaryNamespaceBaseline
	mounts, err := ordinaryNamespaceMountsFromRecipe(recipe)
	if err != nil || mounts.backend == "s3" {
		return result, err
	}
	dir, err := unix.Openat2(unix.AT_FDCWD, mounts.identityDir, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err == nil {
		_, absent, readErr := readComposeNamespaceIdentity(dir, composeNamespaceUID, composeNamespaceGID)
		_ = unix.Close(dir)
		if readErr != nil {
			return result, errOrdinaryUpgrade
		}
		if !absent {
			return result, nil
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return result, errOrdinaryUpgrade
	}
	result.Check, result.Root = true, mounts.blobRoot
	if !attachments.ColdReleasedLocalNamespaceRoot(result.Root, composeNamespaceUID, composeNamespaceGID) {
		return ordinaryNamespaceBaseline{}, errOrdinaryUpgrade
	}
	root, err := unix.Openat2(unix.AT_FDCWD, result.Root, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return ordinaryNamespaceBaseline{}, errOrdinaryUpgrade
	}
	defer unix.Close(root)
	var st unix.Stat_t
	if unix.Fstat(root, &st) != nil || !composeNamespaceStat(root, unix.S_IFDIR, composeNamespaceUID, composeNamespaceGID, 0700, false) || st.Nlink == 0 {
		return ordinaryNamespaceBaseline{}, errOrdinaryUpgrade
	}
	result.RootDevice, result.RootInode = uint64(st.Dev), st.Ino
	digest, err := unix.Openat2(root, "sha256", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return ordinaryNamespaceBaseline{}, errOrdinaryUpgrade
	}
	defer unix.Close(digest)
	if unix.Fstat(digest, &st) != nil || !composeNamespaceStat(digest, unix.S_IFDIR, composeNamespaceUID, composeNamespaceGID, 0700, false) || st.Nlink == 0 {
		return ordinaryNamespaceBaseline{}, errOrdinaryUpgrade
	}
	result.DigestDevice, result.DigestInode = uint64(st.Dev), st.Ino
	return result, nil
}

func ordinaryNamespaceBaselineMatches(b ordinaryNamespaceBaseline) bool {
	if !b.Check {
		return b == (ordinaryNamespaceBaseline{})
	}
	if !filepath.IsAbs(b.Root) || filepath.Clean(b.Root) != b.Root || b.Root == "/" || b.RootInode == 0 || b.DigestInode == 0 {
		return false
	}
	return true
}

type ordinaryNamespaceRecheckRecord struct {
	Version      int                       `json:"version"`
	OperationID  string                    `json:"operation_id"`
	IntentSHA256 string                    `json:"intent_sha256"`
	Namespace    ordinaryNamespaceBaseline `json:"namespace"`
}

// The root controller retains the exclusive guard and all writers remain fenced.
// The initializer later repeats the physical check with its own credential; the
// bootstrap observer here provides the independent database half of the proof.
func ordinaryRecordNamespaceRecheck(ctx context.Context, dir int, intent ordinaryUpgradeIntent, digest string, observer ordinaryNamespaceQueryer) error {
	if !ordinaryPhaseIntentMatches(dir, intent, digest) || !ordinaryNamespaceBaselineMatches(intent.Namespace) {
		return errOrdinaryUpgrade
	}
	record := ordinaryNamespaceRecheckRecord{Version: 1, OperationID: intent.Request.OperationID, IntentSHA256: digest, Namespace: intent.Namespace}
	body, err := json.Marshal(record)
	if err != nil {
		return errOrdinaryUpgrade
	}
	old, exists, err := ordinaryReadOperationFile(dir, "namespace-recheck.json")
	if err != nil || (exists && !bytes.Equal(old, body)) {
		return errOrdinaryUpgrade
	}
	copyFD, err := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(copyFD), "ordinary operation records")
	names, readErr := file.Readdirnames(-1)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return errOrdinaryUpgrade
	}
	for _, name := range names {
		if ordinaryParticipantRecordName.MatchString(name) {
			if !exists {
				return errOrdinaryUpgrade
			}
			return nil
		}
	}
	if intent.Namespace.Check {
		root := intent.Namespace.Root
		if !attachments.ColdReleasedLocalNamespaceRoot(root, composeNamespaceUID, composeNamespaceGID) {
			return errOrdinaryUpgrade
		}
		var rootStat, digestStat unix.Stat_t
		if unix.Fstatat(unix.AT_FDCWD, root, &rootStat, unix.AT_SYMLINK_NOFOLLOW) != nil || uint64(rootStat.Dev) != intent.Namespace.RootDevice || rootStat.Ino != intent.Namespace.RootInode || unix.Fstatat(unix.AT_FDCWD, filepath.Join(root, "sha256"), &digestStat, unix.AT_SYMLINK_NOFOLLOW) != nil || uint64(digestStat.Dev) != intent.Namespace.DigestDevice || digestStat.Ino != intent.Namespace.DigestInode {
			return errOrdinaryUpgrade
		}
		var namespacesExist, obligationsExist bool
		c81 := intent.Predecessor.CatalogClass == ordinaryPredecessorC81
		if observer.QueryRow(ctx, `select to_regclass('public.local_blob_namespaces') is not null, to_regclass('public.record_write_obligations') is not null`).Scan(&namespacesExist, &obligationsExist) != nil || namespacesExist != c81 || obligationsExist != c81 || ordinaryNamespaceNoLocalReferences(ctx, observer, c81) != nil {
			return errOrdinaryUpgrade
		}
		if c81 {
			var bound bool
			if observer.QueryRow(ctx, `select exists(select 1 from public.local_blob_namespaces)`).Scan(&bound) != nil || bound {
				return errOrdinaryUpgrade
			}
		}
	}
	if !exists {
		if ordinaryPublishOperationFile(dir, "namespace-recheck.json", body) != nil {
			return errOrdinaryUpgrade
		}
		old, exists, err = ordinaryReadOperationFile(dir, "namespace-recheck.json")
	}
	if err != nil || !exists || !bytes.Equal(old, body) || unix.Fsync(dir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

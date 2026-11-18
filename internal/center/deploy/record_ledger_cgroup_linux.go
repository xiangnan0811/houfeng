//go:build linux

package deploy

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const ledgerCgroupRoot = "/sys/fs/cgroup"

type ledgerCgroupIdentity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type ledgerCgroup struct {
	identity ledgerCgroupIdentity
	fd       int
}

func ledgerCgroupView() error {
	// A subtree bind mount is not an inventory of the host cgroup hierarchy.
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return ErrRecordLedgerFence
	}
	found := 0
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) >= 5 && len(right) >= 1 && right[0] == "cgroup2" && left[4] == ledgerCgroupRoot {
			if left[3] != "/" {
				return ErrRecordLedgerFence
			}
			found++
		}
	}
	var a, bstat unix.Stat_t
	if found != 1 || unix.Stat("/proc/1/ns/cgroup", &a) != nil || unix.Stat("/proc/self/ns/cgroup", &bstat) != nil || a.Dev != bstat.Dev || a.Ino != bstat.Ino {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerOwnCgroup() (string, error) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	var path string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(line, "0::") {
			if path != "" {
				return "", ErrRecordLedgerFence
			}
			path = strings.TrimPrefix(line, "0::")
		}
	}
	if !strings.HasPrefix(path, "/") || filepath.Clean(path) != path {
		return "", ErrRecordLedgerFence
	}
	return filepath.Join(ledgerCgroupRoot, path), nil
}
func ledgerOpenCgroups(paths []string) ([]ledgerCgroup, error) {
	if len(paths) == 0 {
		return nil, ErrRecordLedgerFence
	}
	if err := ledgerCgroupView(); err != nil {
		return nil, err
	}
	own, err := ledgerOwnCgroup()
	if err != nil {
		return nil, err
	}
	root, err := unix.Open(ledgerCgroupRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	defer unix.Close(root)
	var fs unix.Statfs_t
	if unix.Fstatfs(root, &fs) != nil || uint64(fs.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return nil, ErrRecordLedgerFence
	}
	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)
	groups := make([]ledgerCgroup, 0, len(ordered))
	fail := func() ([]ledgerCgroup, error) { ledgerCloseCgroups(groups); return nil, ErrRecordLedgerFence }
	for i, path := range ordered {
		if path == ledgerCgroupRoot || !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasPrefix(path, ledgerCgroupRoot+"/") || path == own || strings.HasPrefix(own, path+"/") {
			return fail()
		}
		for j := range i {
			if fenceOverlap(ordered[j], path) {
				return fail()
			}
		}
		rel := strings.TrimPrefix(path, ledgerCgroupRoot+"/")
		fd, e := unix.Openat2(root, rel, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
		if e != nil {
			return fail()
		}
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Nlink == 0 {
			unix.Close(fd)
			return fail()
		}
		groups = append(groups, ledgerCgroup{identity: ledgerCgroupIdentity{Path: path, Device: uint64(st.Dev), Inode: st.Ino}, fd: fd})
	}
	return groups, nil
}

func ledgerCloseCgroups(groups []ledgerCgroup) {
	for _, group := range groups {
		_ = unix.Close(group.fd)
	}
}

func ledgerCgroupFile(group ledgerCgroup, name string, write bool, value string) ([]byte, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if write {
		flags = unix.O_WRONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	}
	fd, err := unix.Openat2(group.fd, name, &unix.OpenHow{Flags: uint64(flags), Resolve: maintenanceGuardResolve})
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	file := os.NewFile(uintptr(fd), "ledger cgroup control")
	defer file.Close()
	if write {
		n, e := file.WriteString(value)
		if e != nil || n != len(value) {
			return nil, ErrRecordLedgerFence
		}
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil || len(data) == 1<<20 {
		return nil, ErrRecordLedgerFence
	}
	return data, nil
}

func ledgerCgroupEvents(data []byte) (bool, bool, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || values[fields[0]] != "" {
			return false, false, ErrRecordLedgerFence
		}
		values[fields[0]] = fields[1]
	}
	if (values["frozen"] != "0" && values["frozen"] != "1") || (values["populated"] != "0" && values["populated"] != "1") {
		return false, false, ErrRecordLedgerFence
	}
	return values["frozen"] == "1", values["populated"] == "1", nil
}

func ledgerCheckCgroupIdentity(group ledgerCgroup) error {
	root, err := unix.Open(ledgerCgroupRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer unix.Close(root)
	rel := strings.TrimPrefix(group.identity.Path, ledgerCgroupRoot+"/")
	if rel == group.identity.Path || rel == "" {
		return ErrRecordLedgerFence
	}
	fd, err := unix.Openat2(root, rel, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer unix.Close(fd)
	var a, b unix.Stat_t
	if unix.Fstat(fd, &a) != nil || unix.Fstat(group.fd, &b) != nil || uint64(a.Dev) != group.identity.Device || a.Ino != group.identity.Inode || a.Dev != b.Dev || a.Ino != b.Ino || a.Nlink == 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerCgroupState(group ledgerCgroup) (bool, bool, error) {
	if err := ledgerCheckCgroupIdentity(group); err != nil {
		return false, false, err
	}
	data, err := ledgerCgroupFile(group, "cgroup.events", false, "")
	if err != nil {
		return false, false, err
	}
	return ledgerCgroupEvents(data)
}

func ledgerWaitCgroup(ctx context.Context, group ledgerCgroup, requireEmpty bool) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frozen, populated, err := ledgerCgroupState(group)
		if err != nil {
			return err
		}
		if frozen && (!requireEmpty || !populated) {
			if requireEmpty {
				procs, e := ledgerCgroupFile(group, "cgroup.procs", false, "")
				if e != nil || strings.TrimSpace(string(procs)) != "" {
					return ErrRecordLedgerFence
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ledgerFenceCgroups(ctx context.Context, groups []ledgerCgroup) error {
	// Freeze every registered subtree before killing any of them: a cancelled or
	// failed ceremony never thaws an already fenced producer.
	for _, group := range groups {
		if err := ledgerCheckCgroupIdentity(group); err != nil {
			return err
		}
		if _, err := ledgerCgroupFile(group, "cgroup.freeze", true, "1"); err != nil {
			return err
		}
	}
	for _, group := range groups {
		if err := ledgerWaitCgroup(ctx, group, false); err != nil {
			return err
		}
	}
	for _, group := range groups {
		if _, err := ledgerCgroupFile(group, "cgroup.kill", true, "1"); err != nil {
			return err
		}
	}
	for _, group := range groups {
		if err := ledgerWaitCgroup(ctx, group, true); err != nil {
			return err
		}
	}
	return nil
}

func ledgerVerifyCgroups(groups []ledgerCgroup) error {
	for _, group := range groups {
		frozen, populated, err := ledgerCgroupState(group)
		if err != nil || !frozen || populated {
			return ErrRecordLedgerFence
		}
		data, err := ledgerCgroupFile(group, "cgroup.procs", false, "")
		if err != nil || strings.TrimSpace(string(data)) != "" {
			return ErrRecordLedgerFence
		}
		freeze, err := ledgerCgroupFile(group, "cgroup.freeze", false, "")
		if err != nil || strings.TrimSpace(string(freeze)) != strconv.Itoa(1) {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

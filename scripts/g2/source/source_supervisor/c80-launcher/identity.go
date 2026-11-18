//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func openDirectory(path string, trusted bool) (int, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "//") {
		return -1, denied
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, denied
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return -1, denied
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, denied
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || trusted && (st.Uid != 0 || st.Mode&0022 != 0) {
			unix.Close(fd)
			return -1, denied
		}
	}
	return fd, nil
}

func openAbsolute(path string, flags int, trustedParents bool) (int, error) {
	index := strings.LastIndexByte(path, '/')
	if index <= 0 || index == len(path)-1 {
		return -1, denied
	}
	dir, err := openDirectory(path[:index], trustedParents)
	if err != nil {
		return -1, err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, path[index+1:], flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, denied
	}
	return fd, nil
}

func unchanged(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Size == b.Size && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func boundedFD(fd int, uid, gid uint32, mode uint32, limit int) ([]byte, error) {
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != uid || before.Gid != gid || before.Mode&07777 != mode || before.Nlink != 1 || before.Size <= 0 || before.Size > int64(limit) {
		return nil, denied
	}
	body := make([]byte, int(before.Size))
	for offset := 0; offset < len(body); {
		n, err := unix.Pread(fd, body[offset:], int64(offset))
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return nil, denied
		}
		offset += n
	}
	if unix.Fstat(fd, &after) != nil || !unchanged(before, after) {
		return nil, denied
	}
	return body, nil
}

func readPrivateAtBounded(dir int, name string, uid, gid uint32, limit int) ([]byte, error) {
	if strings.Contains(name, "/") || name == "" {
		return nil, denied
	}
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, denied
	}
	defer unix.Close(fd)
	return boundedFD(fd, uid, gid, 0600, limit)
}

func readPrivateAt(dir int, name string, uid, gid uint32) ([]byte, error) {
	return readPrivateAtBounded(dir, name, uid, gid, maxFrame)
}

func readGenesisAt(dir int) ([]byte, error) {
	return readPrivateAtBounded(dir, "realm-genesis.json", 0, 0, maxGenesis)
}

func sha(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}

func hashFD(fd int) (string, error) {
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size <= 0 {
		return "", denied
	}
	h := sha256.New()
	block := make([]byte, 128<<10)
	var offset int64
	for offset < before.Size {
		want := int64(len(block))
		if remaining := before.Size - offset; remaining < want {
			want = remaining
		}
		n, err := unix.Pread(fd, block[:int(want)], offset)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return "", denied
		}
		_, _ = h.Write(block[:n])
		offset += int64(n)
	}
	if unix.Fstat(fd, &after) != nil || !unchanged(before, after) {
		return "", denied
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkedProgram(path, expected string, mode uint32, group uint32) (int, error) {
	fd, err := openAbsolute(path, unix.O_RDONLY, true)
	if err != nil {
		return -1, err
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != 0 || info.Gid != group || info.Mode&07777 != mode || info.Nlink != 1 {
		unix.Close(fd)
		return -1, denied
	}
	if _, err := unix.Fgetxattr(fd, "security.capability", nil); err != unix.ENODATA && err != unix.ENOTSUP {
		unix.Close(fd)
		return -1, denied
	}
	digest, err := hashFD(fd)
	if err != nil || digest != expected {
		unix.Close(fd)
		return -1, denied
	}
	return fd, nil
}

func sameFD(a, b int) bool {
	var first, second unix.Stat_t
	return unix.Fstat(a, &first) == nil && unix.Fstat(b, &second) == nil && first.Dev == second.Dev && first.Ino == second.Ino && first.Mode&unix.S_IFMT == second.Mode&unix.S_IFMT
}

func validBootID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func bootID() (string, error) {
	body, err := readProc("/proc/sys/kernel/random/boot_id", 128)
	value := strings.TrimSpace(string(body))
	if err != nil || !validBootID(value) {
		return "", denied
	}
	return value, nil
}

func readProc(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, denied
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, denied
	}
	return body, nil
}

func processStart(pid int) (uint64, error) {
	body, err := readProc(fmt.Sprintf("/proc/%d/stat", pid), 65536)
	if err != nil {
		return 0, denied
	}
	idx := bytes.LastIndex(body, []byte(") "))
	if idx < 0 {
		return 0, denied
	}
	fields := strings.Fields(string(body[idx+2:]))
	if len(fields) <= 19 || fields[0] == "Z" || fields[0] == "X" || fields[0] == "x" {
		return 0, denied
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, denied
	}
	return start, nil
}

func processStatusAt(path string, required ...string) (map[string]string, error) {
	body, err := readProc(path, 65536)
	if err != nil {
		return nil, denied
	}
	wanted := make(map[string]bool, len(required))
	for _, name := range required {
		wanted[name] = true
	}
	values := make(map[string]string, len(required))
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !wanted[key] {
			continue
		}
		if _, exists := values[key]; exists {
			return nil, denied
		}
		values[key] = strings.TrimSpace(value)
	}
	if len(values) != len(wanted) {
		return nil, denied
	}
	return values, nil
}

func processStatus(pid int, required ...string) (map[string]string, error) {
	return processStatusAt(fmt.Sprintf("/proc/%d/status", pid), required...)
}

func coordinatorThreads(pid int) error {
	path := fmt.Sprintf("/proc/%d/task", pid)
	for range 3 {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) == 0 {
			return denied
		}
		before := make([]string, 0, len(entries))
		stable := true
		for _, entry := range entries {
			name := entry.Name()
			if _, err := strconv.Atoi(name); err != nil {
				return denied
			}
			before = append(before, name)
			status, err := processStatusAt(path+"/"+name+"/status", "Uid", "Gid", "NoNewPrivs")
			if err != nil {
				stable = false
				break
			}
			if strings.Join(strings.Fields(status["Uid"]), " ") != "10002 10002 10002 10002" || strings.Join(strings.Fields(status["Gid"]), " ") != "10002 10002 10002 10002" || status["NoNewPrivs"] != "0" {
				return denied
			}
		}
		if !stable {
			continue
		}
		after, err := os.ReadDir(path)
		if err != nil {
			return denied
		}
		names := make([]string, 0, len(after))
		for _, entry := range after {
			names = append(names, entry.Name())
		}
		sort.Strings(before)
		sort.Strings(names)
		if strings.Join(before, ",") == strings.Join(names, ",") {
			return nil
		}
	}
	return denied
}

func coordinatorProtected(pid int) bool {
	var info unix.Stat_t
	return unix.Stat(fmt.Sprintf("/proc/%d", pid), &info) == nil && info.Uid == 0 && info.Gid == 0
}

func procExecutable(pid int, expectedPath, expectedSHA string) error {
	path := fmt.Sprintf("/proc/%d/exe", pid)
	link, err := os.Readlink(path)
	if err != nil || link != expectedPath {
		return denied
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return denied
	}
	defer unix.Close(fd)
	digest, err := hashFD(fd)
	if err != nil || digest != expectedSHA {
		return denied
	}
	return nil
}

func openLive(pid int, start uint64) (int, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return -1, denied
	}
	if !live(fd) {
		unix.Close(fd)
		return -1, denied
	}
	actual, err := processStart(pid)
	if err != nil || actual != start || !live(fd) {
		unix.Close(fd)
		return -1, denied
	}
	return fd, nil
}

func live(fd int) bool {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, 0)
	return err == nil && n == 0 && poll[0].Revents == 0
}

func coordinatorIdentity(pin coordinator, adminSHA string) (int, error) {
	if os.Getppid() != pin.PID || pin.ExecutableSHA256 != adminSHA {
		return -1, denied
	}
	fd, err := openLive(pin.PID, pin.Starttime)
	if err != nil {
		return -1, denied
	}
	status, err := processStatus(pin.PID, "Uid", "Gid", "NoNewPrivs")
	if err != nil || strings.Join(strings.Fields(status["Uid"]), " ") != "10002 10002 10002 10002" || strings.Join(strings.Fields(status["Gid"]), " ") != "10002 10002 10002 10002" || status["NoNewPrivs"] != "0" || coordinatorThreads(pin.PID) != nil || !coordinatorProtected(pin.PID) || procExecutable(pin.PID, adminPath, pin.ExecutableSHA256) != nil || !live(fd) || os.Getppid() != pin.PID {
		unix.Close(fd)
		return -1, denied
	}
	return fd, nil
}

func readDirNames(fd int) ([]string, error) {
	child, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, denied
	}
	file := os.NewFile(uintptr(child), "directory")
	if file == nil {
		unix.Close(child)
		return nil, denied
	}
	names, err := file.Readdirnames(-1)
	_ = file.Close()
	if err != nil {
		return nil, denied
	}
	sort.Strings(names)
	return names, nil
}

func verifyPhaseChain(dir int, intent launchIntent) error {
	var st unix.Stat_t
	if unix.Fstat(dir, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != serviceUID || st.Gid != serviceGID || st.Mode&07777 != 0700 {
		return denied
	}
	names, err := readDirNames(dir)
	if err != nil {
		return denied
	}
	var phases []string
	for _, name := range names {
		if strings.HasPrefix(name, "phase-") {
			phases = append(phases, name)
		}
	}
	if len(phases) == 0 || len(phases) > 65536 || uint64(len(phases)) != intent.PhaseGeneration {
		return denied
	}
	var prior [32]byte
	previousPhase := ""
	for i, name := range phases {
		generation := uint64(i + 1)
		if name != fmt.Sprintf("phase-%020d.json", generation) {
			return denied
		}
		raw, err := readPrivateAt(dir, name, serviceUID, serviceGID)
		if err != nil {
			return denied
		}
		phase, err := parsePhase(raw)
		if err != nil || phase.Generation != generation || phase.Previous != prior || phase.Application != intent.Application || phase.RecoveryID != intent.RecoveryID || !allowedTransition(previousPhase, phase.Phase) {
			return denied
		}
		prior = sha256.Sum256(raw)
		previousPhase = phase.Phase
	}
	if previousPhase != "predecessor_callback" {
		return denied
	}
	raw, err := readPrivateAt(dir, fmt.Sprintf("predecessor-callback-intent-%020d.json", intent.PhaseGeneration), serviceUID, serviceGID)
	if err != nil {
		return denied
	}
	actual, _, err := parseIntent(raw, false)
	if err != nil || actual != intent {
		return denied
	}
	after, err := readDirNames(dir)
	if err != nil {
		return denied
	}
	var current []string
	for _, name := range after {
		if strings.HasPrefix(name, "phase-") {
			current = append(current, name)
		}
	}
	if strings.Join(current, "\n") != strings.Join(phases, "\n") {
		return denied
	}
	return nil
}

func allowedTransition(previous, current string) bool {
	switch previous {
	case "":
		return current == "quiescent"
	case "quiescent":
		return current == "predecessor_callback" || current == "migration_callback" || current == "refencing"
	case "predecessor_callback", "migration_callback":
		return current == "refencing"
	case "refencing":
		return current == "quiescent"
	default:
		return false
	}
}

// realmDocument permits the other root-authored genesis and APP fields, but
// still checks duplicate keys throughout those documents before projecting the
// fields needed to authenticate this one launch.
func realmDocument(raw []byte, output any) error {
	if checkJSON(raw, true) != nil || json.Unmarshal(raw, output) != nil {
		return denied
	}
	return nil
}

func genesisDocument(raw []byte, output *genesisFacts) error {
	if checkJSONBounded(raw, true, maxGenesis) != nil || json.Unmarshal(raw, output) != nil {
		return denied
	}
	return nil
}

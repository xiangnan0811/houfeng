//go:build linux && amd64 && !cgo

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

type executableKey struct{ dev, ino uint64 }
type lineageExecutable struct {
	role, digest string
	file         *os.File
}
type lineageNode struct {
	cookie, parent, startNS, generation, forkSeq, execSeq uint64
	tgid, tid                                             uint32
	executable                                            executableKey
	role, digest                                          string
	exitCode                                              int32
	tainted, exited                                       bool
	managedContainerd                                     bool
}

type daemonLineage struct {
	root        uint64
	executables map[executableKey]lineageExecutable
	nodes       map[uint64]*lineageNode
}

// kernelDevice converts Linux userspace's encoded stat device number to the
// kernel dev_t encoding (major:12, minor:20) used by guard i_sb->s_dev.
func kernelDevice(dev uint64) (uint64, error) {
	major, minor := unix.Major(dev), unix.Minor(dev)
	if major >= 1<<12 || minor >= 1<<20 {
		return 0, errGuardProtocol
	}
	return uint64(major)<<20 | uint64(minor), nil
}

func (l *daemonLineage) addExecutable(role, name, expected string) (executableKey, error) {
	if l == nil || role == "" {
		return executableKey{}, errGuardProtocol
	}
	file, digest, err := openTrustedRegular(name, 128<<20)
	if err != nil {
		return executableKey{}, err
	}
	if digest != expected {
		_ = file.Close()
		return executableKey{}, errGuardProtocol
	}
	var st unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &st); err != nil {
		_ = file.Close()
		return executableKey{}, err
	}
	dev, err := kernelDevice(uint64(st.Dev))
	if err != nil || st.Ino == 0 {
		_ = file.Close()
		return executableKey{}, errGuardProtocol
	}
	key := executableKey{dev, st.Ino}
	if l.executables == nil {
		l.executables = make(map[executableKey]lineageExecutable)
	}
	if prior, exists := l.executables[key]; exists {
		_ = file.Close()
		if prior.role != role || prior.digest != digest {
			return executableKey{}, errGuardProtocol
		}
		return key, nil
	}
	l.executables[key] = lineageExecutable{role: role, digest: digest, file: file}
	return key, nil
}

func allowedLineageStep(from, to string) bool {
	if from == "containerd_launcher" {
		return to == "containerd"
	}
	if from == to && from != "" {
		return true
	}
	switch from {
	case "helper":
		return to == "dockerd"
	case "dockerd":
		return to == "containerd_launcher" || to == "wrapper"
	case "containerd":
		return to == "shim"
	case "shim":
		return to == "wrapper"
	case "wrapper":
		return to == "runc"
	case "runc":
		return to == "exec_target"
	}
	return false
}

func (l *daemonLineage) observe(r guardRecord) error {
	if l == nil || r.Kind < 9 || r.Kind > 12 {
		return errGuardProtocol
	}
	tgid, tid := guardU32(r.Detail[:], 0), guardU32(r.Detail[:], 4)
	key := executableKey{guardU64(r.Detail[:], 8), guardU64(r.Detail[:], 16)}
	generation := guardU64(r.Detail[:], 24)
	if r.Sequence == 0 || r.TaskCookie == 0 || r.TaskStartNS == 0 || tgid == 0 || tid == 0 || key.ino == 0 || generation == 0 {
		return errGuardProtocol
	}
	if l.nodes == nil {
		l.nodes = make(map[uint64]*lineageNode)
	}
	match, known := l.executables[key]
	switch r.Kind {
	case 12:
		if l.nodes[r.TaskCookie] != nil || !known || match.role != "helper" || generation != 1 {
			return errGuardProtocol
		}
		if l.root == 0 {
			l.root = r.TaskCookie
		}
		l.nodes[r.TaskCookie] = &lineageNode{cookie: r.TaskCookie, parent: r.ParentCookie, startNS: r.TaskStartNS, generation: generation, forkSeq: r.Sequence, execSeq: r.Sequence, tgid: tgid, tid: tid, executable: key, role: match.role, digest: match.digest}
	case 9:
		parent := l.nodes[r.ParentCookie]
		if parent == nil || parent.exited || l.nodes[r.TaskCookie] != nil || generation != parent.generation || key != parent.executable {
			return errGuardProtocol
		}
		l.nodes[r.TaskCookie] = &lineageNode{cookie: r.TaskCookie, parent: r.ParentCookie, startNS: r.TaskStartNS, generation: generation, forkSeq: r.Sequence, execSeq: parent.execSeq, tgid: tgid, tid: tid, executable: key, role: parent.role, digest: parent.digest, tainted: parent.tainted, managedContainerd: parent.managedContainerd}
	case 10:
		n := l.nodes[r.TaskCookie]
		if n == nil || n.exited || n.parent != r.ParentCookie || n.startNS != r.TaskStartNS || n.tgid != tgid || n.tid != tid || n.generation == ^uint64(0) || generation != n.generation+1 {
			return errGuardProtocol
		}
		valid := known && allowedLineageStep(n.role, match.role) && !n.tainted
		if valid && match.role == "containerd_launcher" {
			root, parent := l.nodes[l.root], l.nodes[n.parent]
			valid = root != nil && parent != nil && n.cookie != root.cookie && n.tgid == n.tid &&
				n.tgid != root.tgid && n.role == "dockerd" && n.execSeq <= n.forkSeq &&
				parent.tgid == root.tgid && parent.role == "dockerd" && !parent.tainted && !parent.exited
		}
		if valid && match.role == "containerd" {
			if n.role == "containerd_launcher" {
				valid = n.tgid == n.tid && n.execSeq > n.forkSeq
				if valid {
					n.managedContainerd = true
				}
			} else {
				valid = n.role == "containerd" && n.managedContainerd
			}
		}
		if !valid {
			n.tainted = true
			n.role = ""
			n.digest = ""
		} else {
			n.role, n.digest = match.role, match.digest
		}
		n.executable, n.generation, n.execSeq = key, generation, r.Sequence
	case 11:
		n := l.nodes[r.TaskCookie]
		if n == nil || n.exited || n.parent != r.ParentCookie || n.startNS != r.TaskStartNS || n.tgid != tgid || n.tid != tid || generation != n.generation || key != n.executable {
			return errGuardProtocol
		}
		n.exitCode, n.exited = int32(guardU32(r.Detail[:], 32)), true
	}
	return nil
}

// ancestry returns only an untainted live wrapper/runc descendant of the
// watched daemon. The guard's retained parent edges survive reparenting; no
// current /proc PPID is substituted for them.
func (l *daemonLineage) ancestry(cookie uint64, role string) ([]any, error) {
	if l == nil || l.root == 0 {
		return nil, errGuardProtocol
	}
	var reverse []*lineageNode
	seen := make(map[uint64]bool)
	for cookie != 0 {
		if seen[cookie] {
			return nil, errGuardProtocol
		}
		seen[cookie] = true
		n := l.nodes[cookie]
		if n == nil || n.tainted || n.digest == "" {
			return nil, errGuardProtocol
		}
		if len(reverse) == 0 && (n.exited || n.role != role) {
			return nil, errGuardProtocol
		}
		reverse = append(reverse, n)
		if cookie == l.root {
			break
		}
		cookie = n.parent
		if len(reverse) > 256 {
			return nil, errGuardProtocol
		}
	}
	if len(reverse) == 0 || reverse[len(reverse)-1].cookie != l.root || reverse[len(reverse)-1].role != "dockerd" {
		return nil, errGuardProtocol
	}
	out := make([]any, 0, len(reverse))
	for i := len(reverse) - 1; i >= 0; i-- {
		n := reverse[i]
		out = append(out, map[string]any{"task_cookie": jsonU64(n.cookie), "parent_cookie": jsonU64(n.parent), "exec_generation": jsonU64(n.generation), "exe_sha256": n.digest, "fork_sequence": jsonU64(n.forkSeq), "exec_sequence": jsonU64(n.execSeq)})
	}
	return out, nil
}

//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func lifecycleGrant(cid, rootfs string) *brokerGrant {
	hostname, hosts, resolv := runtimeSystemPaths(cid)
	return &brokerGrant{
		id: 1, generation: 1, phase: "admitted", purpose: "postgres-live-app", containerID: cid,
		config: map[string]any{"HostConfig": map[string]any{"NetworkMode": "g2-initial-app", "PidsLimit": json.Number("256")}},
		facts: &runtimeContainerFacts{
			rootfs: rootfs, hostname: "g2-container", path: "/usr/bin/id", cwd: "/", user: "999:999",
			hostnamePath: hostname, hostsPath: hosts, resolvPath: resolv,
			networkMode: "g2-initial-app", ipcMode: "private", cgroupnsMode: "private",
			shmSize: defaultShmSize, pidsLimit: 256, hasPidsLimit: true, readonlyRootfs: true, noNewPrivileges: true,
			sysInfoValid: true, sysInfo: runtimeSysInfo{cgroupUnified: true, memoryLimit: true, swapLimit: true, cpuCfs: true, pidsLimit: true, seccomp: true, cgroupNamespaces: true},
			env: map[string]string{"A": "B"}, configEnv: []string{"A=B"},
		},
	}
}

func TestRuntimeProspectiveSystemPaths(t *testing.T) {
	cid := strings.Repeat("a", 64)
	fields := []string{"HostnamePath", "HostsPath", "ResolvConfPath"}
	item := map[string]any{"HostnamePath": "", "HostsPath": "", "ResolvConfPath": ""}
	facts := &runtimeContainerFacts{}
	if err := inspectRuntimeSystemPaths(cid, item, facts, nil); err != nil {
		t.Fatal(err)
	}
	hostname := "/var/lib/g2/docker/containers/" + cid + "/hostname"
	hosts := "/var/lib/g2/docker/containers/" + cid + "/hosts"
	resolv := "/var/lib/g2/docker/containers/" + cid + "/resolv.conf"
	if facts.hostnamePath != hostname || facts.hostsPath != hosts || facts.resolvPath != resolv || facts.runtimeReady {
		t.Fatal("incorrect prospective paths")
	}
	grant := lifecycleGrant(cid, "/var/lib/g2/rootfs")
	if _, err := expectedRuntimeSpec(nil, grant); err != errGuardProtocol {
		t.Fatalf("prospective spec: %v", err)
	}
	for _, field := range fields {
		for name, value := range map[string]any{"null": nil, "type": 1, "relative": "relative", "unclean": hostname + "/../hostname", "other-cid": strings.Replace(hostname, cid, strings.Repeat("b", 64), 1)} {
			t.Run(field+"/"+name, func(t *testing.T) {
				m := map[string]any{"HostnamePath": "", "HostsPath": "", "ResolvConfPath": ""}
				m[field] = value
				f := &runtimeContainerFacts{}
				if err := inspectRuntimeSystemPaths(cid, m, f, nil); err != errGuardProtocol || f.runtimeReady {
					t.Fatalf("accepted: %v", err)
				}
			})
		}
		t.Run(field+"/missing", func(t *testing.T) {
			m := map[string]any{"HostnamePath": "", "HostsPath": "", "ResolvConfPath": ""}
			delete(m, field)
			f := &runtimeContainerFacts{}
			if err := inspectRuntimeSystemPaths(cid, m, f, nil); err != errGuardProtocol || f.runtimeReady {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

func TestRuntimeProspectiveAuthority(t *testing.T) {
	rootTestPinProc(t)
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	listener, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(listener)
	pid := uint32(os.Getpid())
	grant := lifecycleGrant(strings.Repeat("a", 64), "/var/lib/g2/rootfs")
	grant.createBound, grant.facts.runtimeReady = true, true
	lineage := &daemonLineage{root: 1, nodes: map[uint64]*lineageNode{
		1: {cookie: 1, role: "dockerd", digest: "dockerd"},
		2: {cookie: 2, parent: 1, role: "runc", digest: runtimeRuncSHA256, startNS: 1, tgid: pid, tid: pid, execSeq: 2},
	}}
	b := &authorityBroker{checkpoint: &checkpointController{lifecycle: &checkpointLifecycle{lineage: lineage}}}
	inv := &runtimeInvocation{id: 1, grant: grant, grantID: 1, generation: 1, taskCookie: 2, startNS: 1, tgid: pid, pid: pid, wrapperExecSequence: 1, pidfd: pidfd, listener: listener}
	if _, err := b.runtimeNotificationIdentity(inv); err != nil {
		t.Fatalf("positive identity: %v", err)
	}
	if _, err := buildExpectedRuntimeSpecStruct(nil, grant); err != nil {
		t.Fatalf("positive builder: %v", err)
	}
	grant.createBound = false
	if _, err := b.runtimeNotificationIdentity(inv); err != errGuardProtocol {
		t.Fatalf("unbound identity: %v", err)
	}
	grant.createBound, grant.facts.runtimeReady = true, false
	if _, err := b.runtimeNotificationIdentity(inv); err != errGuardProtocol {
		t.Fatalf("prospective identity: %v", err)
	}
	if _, err := buildExpectedRuntimeSpecStruct(nil, grant); err != errGuardProtocol {
		t.Fatalf("prospective builder: %v", err)
	}
}

var lifecyclePasswd = []byte("postgres:x:999:999:postgres:/var/lib/postgresql:/bin/sh\n")
var lifecycleGroup = []byte("postgres:x:999:\n")

type lifecycleFixture struct {
	grant                *brokerGrant
	bundle, root, system string
	admitted             *runtimeContainerFacts
	dirFD, configFD      int
	raw                  []byte
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	if os.Getenv("G2_RUNTIME_FACTS_FIXTURE") != "1" {
		t.Skip("isolated root fixture not requested")
	}
	if os.Geteuid() != 0 {
		t.Fatal("fixture requires uid 0")
	}
	var st unix.Stat_t
	var fs unix.Statfs_t
	if err := unix.Lstat("/var/lib/g2", &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0755 {
		t.Fatalf("fixture root metadata: %v", err)
	}
	if err := unix.Statfs("/var/lib/g2", &fs); err != nil || fs.Type != unix.TMPFS_MAGIC {
		t.Fatalf("fixture root is not tmpfs: %v", err)
	}
	rootTestPinProc(t)
	hash := sha256.Sum256([]byte(t.Name()))
	cid := hex.EncodeToString(hash[:])
	base := filepath.Join("/var/lib/g2/lifecycle", cid)
	system := filepath.Join(runtimeDockerDataRoot, "containers", cid)
	for _, parent := range []string{filepath.Dir(base), filepath.Dir(system)} {
		if err := os.MkdirAll(parent, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(base, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.Mkdir(system, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(system) })
	bundle := filepath.Join(base, "bundle", cid)
	if err := os.MkdirAll(bundle, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "rootfs")
	grant := lifecycleGrant(cid, root)
	grant.facts.mounts = []runtimeMountFact{{typ: "bind", source: filepath.Join(base, "source"), destination: "/data", propagation: "rprivate"}}
	if err := os.Mkdir(grant.facts.mounts[0].source, 0755); err != nil {
		t.Fatal(err)
	}
	model := *grant.facts
	model.runtimeReady = true
	model.passwdRaw = lifecyclePasswd
	model.groupRaw = lifecycleGroup
	model.hasPasswd = true
	model.hasGroup = true
	copyGrant := *grant
	copyGrant.facts = &model
	spec, err := buildExpectedRuntimeSpecStruct(nil, &copyGrant)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Root.Path != root || spec.Process.User.UID != 999 || spec.Process.User.GID != 999 {
		t.Fatal("unexpected root/user")
	}
	for target, source := range map[string]string{"/etc/hostname": "/var/lib/g2/docker/containers/" + cid + "/hostname", "/etc/hosts": "/var/lib/g2/docker/containers/" + cid + "/hosts", "/etc/resolv.conf": "/var/lib/g2/docker/containers/" + cid + "/resolv.conf"} {
		found := false
		for _, mount := range spec.Mounts {
			if mount.Destination == target && mount.Source == source {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing mount %s", target)
		}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(bundle, "config.json")
	if err := os.WriteFile(config, raw, 0444); err != nil {
		t.Fatal(err)
	}
	dirFD, err := unix.Open(bundle, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	configFD, err := unix.Open(config, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(dirFD)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(dirFD); _ = unix.Close(configFD) })
	return &lifecycleFixture{grant: grant, admitted: grant.facts, bundle: bundle, root: root, system: system, dirFD: dirFD, configFD: configFD, raw: raw}
}

func (f *lifecycleFixture) prepare(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(f.root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"passwd": lifecyclePasswd, "group": lifecycleGroup} {
		if err := os.WriteFile(filepath.Join(f.root, "etc", name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"hostname", "hosts", "resolv.conf"} {
		if err := os.WriteFile(filepath.Join(f.system, name), []byte("fixture\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *lifecycleFixture) verify() (*runtimeContainerFacts, unix.Stat_t, error) {
	candidate, stat, _, err := verifyRuntimeBundle(nil, f.grant, f.bundle, f.dirFD, f.configFD, hashBytes(f.raw))
	return candidate, stat, err
}

func (f *lifecycleFixture) alterSpec(t *testing.T, alter func(map[string]any)) {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal(f.raw, &spec); err != nil {
		t.Fatal(err)
	}
	alter(spec)
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(f.bundle, "config.json")
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0444); err != nil {
		t.Fatal(err)
	}
	f.raw = raw
}

func (f *lifecycleFixture) snapshot() runtimeContainerFacts {
	copy := *f.grant.facts
	copy.mounts = append([]runtimeMountFact(nil), copy.mounts...)
	copy.configEnv = append([]string(nil), copy.configEnv...)
	copy.env = make(map[string]string, len(copy.env))
	for k, v := range f.grant.facts.env {
		copy.env[k] = v
	}
	return copy
}

func (f *lifecycleFixture) unchanged(t *testing.T, before runtimeContainerFacts) {
	t.Helper()
	if f.grant.facts != f.admitted || !reflect.DeepEqual(*f.grant.facts, before) || f.grant.createBound || f.grant.bundleDirFD != 0 || f.grant.bundleConfigFD != 0 || f.grant.bundlePath != "" || f.grant.bundleSHA != "" || f.grant.bundleStat != (unix.Stat_t{}) {
		t.Fatal("failure published state")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(f.configFD, &stat); err != nil {
		t.Fatalf("caller fd lost: %v", err)
	}
}

func TestRuntimeFirstCreateSnapshot(t *testing.T) {
	f := newLifecycleFixture(t)
	before := f.snapshot()
	if candidate, stat, stage, err := verifyRuntimeBundle(nil, f.grant, f.bundle, f.dirFD, f.configFD, hashBytes(f.raw)); err == nil || candidate != nil || stat != (unix.Stat_t{}) || stage != (ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleMaterialize}) {
		t.Fatalf("unmaterialized create stage=%v err=%v", stage, err)
	}
	f.unchanged(t, before)
	f.prepare(t)
	item := map[string]any{"HostnamePath": f.grant.facts.hostnamePath, "HostsPath": f.grant.facts.hostsPath, "ResolvConfPath": f.grant.facts.resolvPath}
	inspected := &runtimeContainerFacts{}
	if err := inspectRuntimeSystemPaths(f.grant.containerID, item, inspected, nil); err != nil {
		t.Fatalf("nonempty system paths: %v", err)
	}
	candidate, saved, successDiagnostic, err := verifyRuntimeBundle(nil, f.grant, f.bundle, f.dirFD, f.configFD, hashBytes(f.raw))
	if err != nil || successDiagnostic != (ownerProbeRuntimeDiagnostic{}) {
		t.Fatalf("valid create: %v", err)
	}
	var actual unix.Stat_t
	if err := unix.Fstat(f.configFD, &actual); err != nil || !sameTrustedStat(saved, actual) {
		t.Fatalf("saved stat: %v", err)
	}
	if !candidate.runtimeReady || !candidate.hasPasswd || !candidate.hasGroup || string(candidate.passwdRaw) != string(lifecyclePasswd) || string(candidate.groupRaw) != string(lifecycleGroup) || !reflect.DeepEqual(candidate.mounts, before.mounts) || candidate.hostnamePath != before.hostnamePath || candidate.hostsPath != before.hostsPath || candidate.resolvPath != before.resolvPath {
		t.Fatal("incorrect candidate")
	}
	f.unchanged(t, before)

	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *lifecycleFixture)
	}{
		{"wrong-cid", func(t *testing.T, f *lifecycleFixture) { f.grant.containerID = strings.Repeat("b", 64) }},
		{"missing-system", func(t *testing.T, f *lifecycleFixture) {
			if err := os.Remove(filepath.Join(f.system, "hosts")); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink-system", func(t *testing.T, f *lifecycleFixture) {
			p := filepath.Join(f.system, "hosts")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/dev/null", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"writable-system", func(t *testing.T, f *lifecycleFixture) {
			if err := os.Chmod(filepath.Join(f.system, "hosts"), 0666); err != nil {
				t.Fatal(err)
			}
		}},
		{"device-rootfs", func(t *testing.T, f *lifecycleFixture) { f.grant.facts.rootfs = "/dev/null" }},
		{"user-read", func(t *testing.T, f *lifecycleFixture) {
			if err := os.Chmod(filepath.Join(f.root, "etc", "passwd"), 0666); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong-spec-root", func(t *testing.T, f *lifecycleFixture) {
			f.alterSpec(t, func(spec map[string]any) { spec["root"].(map[string]any)["path"] = "/var/lib/g2/other" })
		}},
		{"wrong-spec-user", func(t *testing.T, f *lifecycleFixture) {
			f.alterSpec(t, func(spec map[string]any) { spec["process"].(map[string]any)["user"].(map[string]any)["uid"] = 1 })
		}},
		{"wrong-spec-env", func(t *testing.T, f *lifecycleFixture) {
			f.alterSpec(t, func(spec map[string]any) { spec["process"].(map[string]any)["env"] = "not-an-array" })
		}},
		{"wrong-spec-unknown", func(t *testing.T, f *lifecycleFixture) {
			f.alterSpec(t, func(spec map[string]any) { spec["!unknown"] = true })
		}},
		{"wrong-spec-mount-options", func(t *testing.T, f *lifecycleFixture) {
			f.alterSpec(t, func(spec map[string]any) {
				for _, value := range spec["mounts"].([]any) {
					mount := value.(map[string]any)
					if mount["destination"] == "/proc" {
						mount["options"] = []any{"nodev", "noexec", "nosuid"}
						return
					}
				}
				t.Fatal("proc mount missing")
			})
		}},
		{"wrong-digest", func(t *testing.T, f *lifecycleFixture) { f.raw = append(f.raw, 'x') }},
		{"config-replaced", func(t *testing.T, f *lifecycleFixture) {
			p := filepath.Join(f.bundle, "config.json")
			if err := os.Rename(p, p+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, f.raw, 0444); err != nil {
				t.Fatal(err)
			}
		}},
		{"bundle-replaced", func(t *testing.T, f *lifecycleFixture) {
			if err := os.Rename(f.bundle, f.bundle+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(f.bundle, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.bundle, "config.json"), f.raw, 0444); err != nil {
				t.Fatal(err)
			}
		}},
		{"config-content", func(t *testing.T, f *lifecycleFixture) {
			p := filepath.Join(f.bundle, "config.json")
			if err := os.Chmod(p, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, 0444); err != nil {
				t.Fatal(err)
			}
		}},
		{"external-system-source", func(t *testing.T, f *lifecycleFixture) {
			var spec map[string]any
			if err := json.Unmarshal(f.raw, &spec); err != nil {
				t.Fatal(err)
			}
			for _, value := range spec["mounts"].([]any) {
				mount := value.(map[string]any)
				if mount["destination"] == "/etc/hosts" {
					mount["source"] = "/var/lib/g2/external"
				}
			}
			raw, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(f.bundle, "config.json")
			if err := os.Chmod(p, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, raw, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, 0444); err != nil {
				t.Fatal(err)
			}
			f.raw = raw
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.prepare(t)
			if _, _, err := f.verify(); err != nil {
				t.Fatalf("positive control: %v", err)
			}
			tc.mutate(t, f)
			before := f.snapshot()
			candidate, stat, stage, err := verifyRuntimeBundle(nil, f.grant, f.bundle, f.dirFD, f.configFD, hashBytes(f.raw))
			if err == nil || candidate != nil || stat != (unix.Stat_t{}) {
				t.Fatalf("invalid create accepted: %v", err)
			}
			switch tc.name {
			case "wrong-spec-root", "wrong-spec-user", "wrong-spec-env", "wrong-spec-unknown":
				want := map[string]ownerProbeRuntimeStage{"wrong-spec-root": 180, "wrong-spec-user": 156, "wrong-spec-env": 163, "wrong-spec-unknown": 239}[tc.name]
				if stage != (ownerProbeRuntimeDiagnostic{stage: want}) || err != errGuardProtocol {
					t.Fatalf("wrong-spec stage=%v err=%v", stage, err)
				}
			case "wrong-spec-mount-options":
				want := ownerProbeRuntimeDiagnostic{stage: 188, mountOptions: ownerProbeMountOptionsDiagnostic{1, 15, 6, 5}}
				if stage != want || err != errGuardProtocol {
					t.Fatalf("mount-options diagnostic=%v err=%v", stage, err)
				}
			default:
				if stage.mountOptions != (ownerProbeMountOptionsDiagnostic{}) {
					t.Fatalf("unexpected witness: %v", stage)
				}
			}
			f.unchanged(t, before)
		})
	}
	t.Run("closed-config", func(t *testing.T) {
		f := newLifecycleFixture(t)
		f.prepare(t)
		if _, _, err := f.verify(); err != nil {
			t.Fatalf("positive control: %v", err)
		}
		before := f.snapshot()
		fd, err := unix.FcntlInt(uintptr(f.configFD), unix.F_DUPFD_CLOEXEC, 128)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Close(fd); err != nil {
			t.Fatal(err)
		}
		candidate, stat, _, err := verifyRuntimeBundle(nil, f.grant, f.bundle, f.dirFD, fd, hashBytes(f.raw))
		if err == nil || candidate != nil || stat != (unix.Stat_t{}) {
			t.Fatalf("closed fd accepted: %v", err)
		}
		f.unchanged(t, before)
	})
}

func TestRuntimeAcceptedSnapshotRecheck(t *testing.T) {
	f := newLifecycleFixture(t)
	f.prepare(t)
	candidate, saved, err := f.verify()
	if err != nil {
		t.Fatal(err)
	}
	f.grant.facts, f.grant.createBound = candidate, true
	f.grant.bundleDirFD, f.grant.bundleConfigFD, f.grant.bundlePath, f.grant.bundleSHA, f.grant.bundleStat = f.dirFD, f.configFD, f.bundle, hashBytes(f.raw), saved
	if err := recheckRuntimeBundle(f.grant); err != nil {
		t.Fatalf("positive bundle recheck: %v", err)
	}
	if err := verifyRuntimeUserFiles(candidate); err != nil {
		t.Fatalf("positive user recheck: %v", err)
	}
	if _, err := materializeRuntimeFacts(candidate); err != errGuardProtocol {
		t.Fatalf("accepted rematerialization: %v", err)
	}
	for _, name := range []string{"passwd", "group"} {
		p := filepath.Join(f.root, "etc", name)
		original, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(original, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyRuntimeUserFiles(candidate); err != errGuardProtocol {
			t.Fatalf("changed %s accepted: %v", name, err)
		}
		if err := os.WriteFile(p, original, 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(f.bundle, "config.json")
	if err := os.Rename(p, p+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, f.raw, 0444); err != nil {
		t.Fatal(err)
	}
	if err := recheckRuntimeBundle(f.grant); err == nil {
		t.Fatal("replacement config accepted")
	}
	t.Run("bundle-replaced", func(t *testing.T) {
		g := newLifecycleFixture(t)
		g.prepare(t)
		accepted, stat, err := g.verify()
		if err != nil {
			t.Fatalf("positive control: %v", err)
		}
		g.grant.facts, g.grant.createBound = accepted, true
		g.grant.bundleDirFD, g.grant.bundleConfigFD, g.grant.bundlePath, g.grant.bundleSHA, g.grant.bundleStat = g.dirFD, g.configFD, g.bundle, hashBytes(g.raw), stat
		if err := recheckRuntimeBundle(g.grant); err != nil {
			t.Fatalf("positive recheck: %v", err)
		}
		if err := os.Rename(g.bundle, g.bundle+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(g.bundle, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g.bundle, "config.json"), g.raw, 0444); err != nil {
			t.Fatal(err)
		}
		if err := recheckRuntimeBundle(g.grant); err == nil {
			t.Fatal("replacement bundle accepted")
		}
	})
	if string(candidate.passwdRaw) != string(lifecyclePasswd) || string(candidate.groupRaw) != string(lifecycleGroup) {
		t.Fatal("snapshot changed")
	}
}

//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestManagedContainerdPolicy(t *testing.T) {
	good := []string{containerdLauncherPath, "--config", managedContainerdConfig}
	if !managedContainerdArgsValid(good) {
		t.Fatal("valid argv rejected")
	}
	for _, args := range [][]string{{}, {"containerd", "--config", managedContainerdConfig}, {containerdLauncherPath, "--config", "/wrong"}, append(append([]string{}, good...), "extra")} {
		if managedContainerdArgsValid(args) {
			t.Fatalf("invalid argv accepted: %v", args)
		}
	}
	for _, tc := range []struct {
		env   []string
		valid bool
	}{
		{[]string{"A=x", "PATH=" + baseGuestPath, "B=y"}, true},
		{[]string{"A=x"}, false}, {[]string{"PATH=" + baseGuestPath, "PATH=" + baseGuestPath}, false},
		{[]string{"PATH=/wrong"}, false},
	} {
		before := append([]string{}, tc.env...)
		err := replaceExactPath(tc.env, baseGuestPath, managedDockerPath)
		if tc.valid {
			before[1] = "PATH=" + managedDockerPath
			if err != nil || !reflect.DeepEqual(tc.env, before) {
				t.Fatalf("replacement: %v %v", tc.env, err)
			}
		} else if err != errGuardProtocol || !reflect.DeepEqual(tc.env, before) {
			t.Fatalf("invalid replacement changed slice: %v %v", tc.env, err)
		}
	}
}

func managedFixtureFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(name, data, 0500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(name) })
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func managedFixtureDir(t *testing.T) {
	t.Helper()
	ownerRootPrerequisites(t)
	if err := os.Mkdir(containerdLauncherDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(containerdLauncherDir, 0700)
		_ = os.Remove(containerdLauncherPath)
		_ = os.Remove(containerdLauncherDir)
	})
}

func TestManagedContainerdPreflight(t *testing.T) {
	managedFixtureDir(t)
	t.Cleanup(func() { _ = os.Remove("/opt/g2/launcher-owned") })
	managedFixtureFile(t, containerdLauncherPath, []byte("launcher"))
	if err := os.Chmod(containerdLauncherDir, 0500); err != nil {
		t.Fatal(err)
	}
	if err := managedContainerdPreflight(); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if err := os.Chmod(containerdLauncherPath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := managedContainerdPreflight(); err == nil {
		t.Fatal("wrong launcher file mode accepted")
	}
	if err := os.Chmod(containerdLauncherPath, 0500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(containerdLauncherDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := managedContainerdPreflight(); err == nil {
		t.Fatal("writable directory accepted")
	}
	extra := filepath.Join(containerdLauncherDir, "extra")
	if err := os.WriteFile(extra, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(containerdLauncherDir, 0500); err != nil {
		t.Fatal(err)
	}
	if err := managedContainerdPreflight(); err == nil {
		t.Fatal("extra entry accepted")
	}
	if err := os.Chmod(containerdLauncherDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(containerdLauncherPath, "/opt/g2/launcher-owned"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/opt/g2/launcher-owned", containerdLauncherPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(containerdLauncherDir, 0500); err != nil {
		t.Fatal(err)
	}
	if err := managedContainerdPreflight(); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(containerdLauncherDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(containerdLauncherPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename("/opt/g2/launcher-owned", containerdLauncherPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(containerdLauncherDir, 0500); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("/run/g2-docker", 0700); err != nil {
		t.Fatal(err)
	}
	if err := managedContainerdPreflight(); err == nil {
		t.Fatal("stale execroot accepted")
	}
	if err := os.Remove("/run/g2-docker"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("/run/containerd", 0700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove("/run/containerd")
	if err := os.Symlink("/nonexistent", "/run/containerd/containerd.sock"); err != nil {
		t.Fatal(err)
	}
	defer os.Remove("/run/containerd/containerd.sock")
	if err := managedContainerdPreflight(); err == nil {
		t.Fatal("stale socket accepted")
	}
}

func TestManagedContainerdBinding(t *testing.T) {
	managedFixtureDir(t)
	data := []byte("authority-fixture")
	digest := managedFixtureFile(t, "/opt/g2/g2-bpf-authority", data)
	managedFixtureFile(t, "/opt/g2/g2-bpf-runtime", data)
	managedFixtureFile(t, containerdLauncherPath, data)
	if err := os.Chmod(containerdLauncherDir, 0500); err != nil {
		t.Fatal(err)
	}
	manifest := bootstrapManifest{BrokerSHA256: digest, Executables: make(map[string]bootstrapExecutable)}
	for _, role := range []string{"dockerd", "containerd", "shim", "runc"} {
		name := "/usr/bin/" + role
		hash := managedFixtureFile(t, name, []byte(role))
		manifest.Executables[role] = bootstrapExecutable{Path: name, SHA256: hash}
	}
	makeBroker := func() *authorityBroker {
		return &authorityBroker{manifest: manifest, transcript: newAuthorityTranscript("boot", "epoch"), checkpoint: &checkpointController{lifecycle: &checkpointLifecycle{lineage: &daemonLineage{}}}}
	}
	b := makeBroker()
	if err := b.installBrokerLineage(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, e := range b.checkpoint.lifecycle.lineage.executables {
			_ = e.file.Close()
		}
	}()
	if len(b.transcript.entries) != 1 || b.transcript.entries[0].record["kind"] != "containerd_launcher" || len(b.checkpoint.lifecycle.lineage.executables) != 7 {
		t.Fatal("binding not published")
	}
	body := b.transcript.entries[0].record["body"].(map[string]any)
	if len(body) != 8 || body["exe_sha256"] != digest || body["target_sha256"] != manifest.Executables["containerd"].SHA256 {
		t.Fatal("binding differs")
	}
	launcher, target := executableKey{}, executableKey{}
	for key, executable := range b.checkpoint.lifecycle.lineage.executables {
		if executable.role == "containerd_launcher" {
			launcher = key
		}
		if executable.role == "containerd" {
			target = key
		}
	}
	if launcher == target || body["exe_dev"] != jsonU64(launcher.dev) || body["exe_ino"] != jsonU64(launcher.ino) || body["target_dev"] != jsonU64(target.dev) || body["target_ino"] != jsonU64(target.ino) {
		t.Fatal("binding does not use held keys")
	}
	if err := b.installBrokerLineage(); err != nil || len(b.transcript.entries) != 1 {
		t.Fatal("reentry appended binding")
	}
	beforeFDs, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	failed := makeBroker()
	failed.transcript.total = maxAuthorityTranscriptBytes
	if err := failed.installBrokerLineage(); err == nil || !failed.checkpoint.invalid || !failed.checkpoint.faulted || len(failed.checkpoint.lifecycle.lineage.executables) != 0 || len(failed.transcript.entries) != 0 {
		t.Fatal("append failure did not fault")
	}
	if failed.transcript.tail != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatal("append failure changed transcript tail")
	}
	afterFDs, err := os.ReadDir("/proc/self/fd")
	if err != nil || len(afterFDs) != len(beforeFDs) {
		t.Fatal("append failure leaked candidate FDs")
	}
	failed.transcript.total = 0
	if err := failed.installBrokerLineage(); err == nil || len(failed.transcript.entries) != 0 {
		t.Fatal("faulted epoch reinstalled")
	}
	checkRejected := func(candidate *authorityBroker, reason string) {
		t.Helper()
		before, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		if err := candidate.installBrokerLineage(); err == nil {
			t.Fatalf("%s accepted", reason)
		}
		after, err := os.ReadDir("/proc/self/fd")
		if err != nil || len(after) != len(before) || len(candidate.checkpoint.lifecycle.lineage.executables) != 0 ||
			len(candidate.transcript.entries) != 0 || candidate.transcript.tail != "0000000000000000000000000000000000000000000000000000000000000000" {
			t.Fatalf("%s leaked or published candidate", reason)
		}
	}
	wrong := makeBroker()
	wrong.manifest.BrokerSHA256 = "wrong"
	checkRejected(wrong, "digest mismatch")
	if err := os.Chmod(containerdLauncherDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(containerdLauncherPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Link("/opt/g2/g2-bpf-authority", containerdLauncherPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(containerdLauncherDir, 0500); err != nil {
		t.Fatal(err)
	}
	alias := makeBroker()
	checkRejected(alias, "aliased launcher")
}

func TestManagedContainerdRegistrationACKDelivery(t *testing.T) {
	registration := &daemonRegistration{cookie: 42, watchSequence: 17}
	principal := &controlPrincipal{ID: 9}
	ack, err := canonicalControl(map[string]any{"status": "OK", "body": map[string]any{
		"daemon_cookie": jsonU64(42), "watch_sequence": jsonU64(17),
	}})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := canonicalControl(map[string]any{"status": "BPF_AUTH_DENIED", "body": map[string]any{"code": "BPF_AUTH_DENIED"}})
	if err != nil {
		t.Fatal(err)
	}
	b := &authorityBroker{checkpoint: &checkpointController{}, daemons: map[uint64]*daemonRegistration{principal.ID: registration}}
	if !b.matchesDaemonRegistrationACK(principal, ack) || b.matchesDaemonRegistrationACK(principal, denied) || b.matchesDaemonRegistrationACK(nil, ack) {
		t.Fatal("registration replay classification differs")
	}
	for _, tc := range []struct {
		name                  string
		packet                []byte
		daemon, replay, fault bool
	}{
		{"initial", ack, true, false, true},
		{"exact-replay", ack, false, true, true},
		{"denied-replay", denied, false, true, false},
		{"other-response", ack, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pair[0])
			if err := unix.Close(pair[1]); err != nil {
				t.Fatal(err)
			}
			candidate := &authorityBroker{checkpoint: &checkpointController{}, daemons: map[uint64]*daemonRegistration{principal.ID: registration}}
			if tc.replay {
				err = candidate.deliverReplayedBrokerACK(pair[0], tc.packet, "REGISTER_DAEMON", principal)
			} else {
				err = candidate.deliverBrokerACK(pair[0], tc.packet, tc.daemon)
			}
			if err != unix.EPIPE {
				t.Fatalf("original send error differs: %v", err)
			}
			if candidate.checkpoint.invalid != tc.fault || candidate.checkpoint.faulted != tc.fault {
				t.Fatal("ACK delivery fault state differs")
			}
		})
	}
	t.Run("successful-delivery", func(t *testing.T) {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		candidate := &authorityBroker{checkpoint: &checkpointController{}}
		if err := candidate.deliverBrokerACK(pair[0], ack, true); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, len(ack))
		n, _, err := unix.Recvfrom(pair[1], buffer, 0)
		if err != nil || n != len(ack) || !reflect.DeepEqual(buffer, ack) || candidate.checkpoint.invalid || candidate.checkpoint.faulted {
			t.Fatal("successful ACK delivery differs")
		}
	})
}

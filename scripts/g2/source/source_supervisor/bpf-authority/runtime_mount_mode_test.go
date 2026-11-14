//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRuntimeMountModeDiagnostic(t *testing.T) {
	f := newLifecycleFixture(t)
	cid := filepath.Base(f.system)
	volumeParent := filepath.Join(runtimeDockerDataRoot, "volumes")
	if err := os.MkdirAll(volumeParent, 0755); err != nil {
		t.Fatal(err)
	}
	makeVolume := func(id string) string {
		t.Helper()
		parent := filepath.Join(volumeParent, id)
		if err := os.Mkdir(parent, 0755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(parent) })
		path := filepath.Join(parent, "_data")
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	volume := makeVolume(cid)
	malformed := makeVolume("g" + cid[1:])
	uppercase := makeVolume("A" + cid[1:])
	short := makeVolume(cid[:63])
	wrongLeaf := filepath.Join(filepath.Dir(volume), "other")
	if err := os.Mkdir(wrongLeaf, 0755); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(volume, "extra")
	if err := os.Mkdir(extra, 0755); err != nil {
		t.Fatal(err)
	}
	wrongRoot := filepath.Join(filepath.Dir(f.root), "docker", "volumes", cid, "_data")
	if err := os.MkdirAll(wrongRoot, 0755); err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []string{"hostname", "hosts", "resolv.conf"} {
		file, err := os.OpenFile(filepath.Join(f.system, leaf), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}

	check := func(path string, mode uint32, stage string) {
		t.Helper()
		if err := unix.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		diagnostic := admitDiagnostic{stage: "PREVIOUS_CONTEXT"}
		err := runtimeMountIdentity(path, &diagnostic)
		if stage == "" {
			if err != nil || diagnostic.stage != "PREVIOUS_CONTEXT" || diagnostic.observed != nil {
				t.Fatalf("passing control: path=%q stage=%q observed=%v returned=%v", path, diagnostic.stage, diagnostic.observed, err)
			}
			return
		}
		if len(stage) > 64 {
			t.Fatalf("stage exceeds diagnostic bound: %q", stage)
		}
		if err != errGuardProtocol || diagnostic.stage != stage || diagnostic.observed != errGuardProtocol {
			t.Fatalf("mode rejection: path=%q stage=%q observed=%v returned=%v", path, diagnostic.stage, diagnostic.observed, err)
		}
		if err := runtimeMountIdentity(path, nil); err != errGuardProtocol {
			t.Fatalf("nil diagnostic changed rejection: %v", err)
		}
		first := errors.New("first rejection")
		latched := admitDiagnostic{stage: "FIRST", observed: first}
		if err := runtimeMountIdentity(path, &latched); err != errGuardProtocol || latched.stage != "FIRST" || latched.observed != first {
			t.Fatalf("first rejection changed: stage=%q observed=%v returned=%v", latched.stage, latched.observed, err)
		}
	}

	check(volume, 0755, "")
	check(volume, 01777, "ADMIT_MOUNT_MODE_VOLUME_01777")
	check(volume, 0775, "ADMIT_MOUNT_MODE_VOLUME_OTHER_WRITABLE")
	if err := unix.Chmod(volume, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		leaf  string
		stage string
	}{
		{"hostname", "ADMIT_MOUNT_MODE_HOSTNAME"},
		{"hosts", "ADMIT_MOUNT_MODE_HOSTS"},
		{"resolv.conf", "ADMIT_MOUNT_MODE_RESOLVCONF"},
	} {
		path := filepath.Join(f.system, tc.leaf)
		check(path, 0644, "")
		check(path, 0664, tc.stage)
	}
	for _, path := range []string{malformed, uppercase, short, wrongRoot, wrongLeaf, extra} {
		check(path, 0777, "ADMIT_MOUNT_MODE")
	}
	nonClean := filepath.Dir(volume) + "/../" + cid + "/_data"
	diagnostic := admitDiagnostic{}
	if err := runtimeMountIdentity(nonClean, &diagnostic); err != errGuardProtocol || diagnostic.stage != "ADMIT_MOUNT_PATH" || diagnostic.observed != errGuardProtocol {
		t.Fatalf("non-clean path: stage=%q observed=%v returned=%v", diagnostic.stage, diagnostic.observed, err)
	}
}

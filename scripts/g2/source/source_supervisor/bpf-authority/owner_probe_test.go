//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func ownerInspectionFixture(purpose string) map[string]any {
	flag := "-u"
	if purpose == "postgres-gid" {
		flag = "-g"
	}
	return map[string]any{
		"Id": strings.Repeat("a", 64), "Image": postgresImageID,
		"State":      map[string]any{"Status": "created", "Running": false, "Pid": json.Number("0")},
		"Config":     map[string]any{"Tty": false, "AttachStdout": true, "AttachStderr": true, "Entrypoint": []any{"/usr/bin/id"}, "Cmd": []any{flag, "postgres"}},
		"HostConfig": map[string]any{"NetworkMode": "none", "ReadonlyRootfs": true, "Privileged": false, "CapDrop": []any{"ALL"}, "SecurityOpt": []any{"no-new-privileges"}, "Binds": nil, "Mounts": []any{map[string]any{"Type": "bind", "Source": "/run/g2/postgres-owner-probe", "Target": "/var/lib/postgresql/data", "ReadOnly": true}}},
		"Mounts":     []any{map[string]any{"Type": "bind", "Source": "/run/g2/postgres-owner-probe", "Destination": "/var/lib/postgresql/data", "RW": false, "Propagation": "rprivate", "Name": ""}},
	}
}

func TestOwnerProbeInspection(t *testing.T) {
	cid := strings.Repeat("a", 64)
	for _, purpose := range []string{"postgres-uid", "postgres-gid"} {
		t.Run(purpose, func(t *testing.T) {
			item := ownerInspectionFixture(purpose)
			if err := validateOwnerProbeInspection(item, cid, purpose); err != nil {
				t.Fatalf("valid inspection: %v", err)
			}
			for _, mutate := range []struct {
				name string
				fn   func(map[string]any)
			}{
				{"image", func(m map[string]any) { m["Image"] = "wrong" }},
				{"cid", func(m map[string]any) { m["Id"] = "wrong" }},
				{"argv", func(m map[string]any) { m["Config"].(map[string]any)["Cmd"] = []any{"-x", "postgres"} }},
				{"state", func(m map[string]any) { m["State"].(map[string]any)["Running"] = true }},
				{"source", func(m map[string]any) { m["Mounts"].([]any)[0].(map[string]any)["Source"] = "/wrong" }},
				{"target", func(m map[string]any) { m["Mounts"].([]any)[0].(map[string]any)["Destination"] = "/wrong" }},
				{"type", func(m map[string]any) { m["Mounts"].([]any)[0].(map[string]any)["Type"] = "volume" }},
				{"writable", func(m map[string]any) { m["Mounts"].([]any)[0].(map[string]any)["RW"] = true }},
				{"extra", func(m map[string]any) { m["Mounts"] = append(m["Mounts"].([]any), map[string]any{}) }},
				{"volume-only", func(m map[string]any) {
					m["HostConfig"].(map[string]any)["Mounts"] = nil
					m["Mounts"].([]any)[0].(map[string]any)["Type"] = "volume"
				}},
				{"requested-extra-field", func(m map[string]any) {
					m["HostConfig"].(map[string]any)["Mounts"].([]any)[0].(map[string]any)["BindOptions"] = map[string]any{}
				}},
				{"requested-source", func(m map[string]any) {
					m["HostConfig"].(map[string]any)["Mounts"].([]any)[0].(map[string]any)["Source"] = "/wrong"
				}},
				{"requested-target", func(m map[string]any) {
					m["HostConfig"].(map[string]any)["Mounts"].([]any)[0].(map[string]any)["Target"] = "/wrong"
				}},
				{"requested-writable", func(m map[string]any) {
					m["HostConfig"].(map[string]any)["Mounts"].([]any)[0].(map[string]any)["ReadOnly"] = false
				}},
				{"binds", func(m map[string]any) { m["HostConfig"].(map[string]any)["Binds"] = []any{"bad"} }},
				{"propagation", func(m map[string]any) { m["Mounts"].([]any)[0].(map[string]any)["Propagation"] = "shared" }},
				{"name", func(m map[string]any) { m["Mounts"].([]any)[0].(map[string]any)["Name"] = "volume" }},
			} {
				t.Run(mutate.name, func(t *testing.T) {
					changed := ownerInspectionFixture(purpose)
					mutate.fn(changed)
					if err := validateOwnerProbeInspection(changed, cid, purpose); err == nil {
						t.Fatal("invalid inspection accepted")
					}
				})
			}
		})
	}
}

func TestOwnerProbeSourcePolicy(t *testing.T) {
	good := unix.Stat_t{Mode: unix.S_IFDIR | 0500, Uid: 0, Gid: 0}
	if !ownerProbeSourceStatValid(good) {
		t.Fatal("valid metadata rejected")
	}
	for _, tc := range []struct {
		name   string
		change func(*unix.Stat_t)
	}{
		{"uid", func(s *unix.Stat_t) { s.Uid = 1 }},
		{"gid", func(s *unix.Stat_t) { s.Gid = 1 }},
		{"file", func(s *unix.Stat_t) { s.Mode = unix.S_IFREG | 0500 }},
		{"special", func(s *unix.Stat_t) { s.Mode |= unix.S_ISUID }},
		{"writable", func(s *unix.Stat_t) { s.Mode |= 0200 }},
		{"other-mode", func(s *unix.Stat_t) { s.Mode = unix.S_IFDIR | 0400 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := good
			tc.change(&s)
			if ownerProbeSourceStatValid(s) {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func ownerRootPrerequisites(t *testing.T) {
	t.Helper()
	if os.Getenv("G2_RUNTIME_FACTS_FIXTURE") != "1" || os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Fatal("isolated root fixture required")
	}
	var st unix.Stat_t
	var fs unix.Statfs_t
	if err := unix.Lstat("/run/g2", &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0700 {
		t.Fatalf("source parent metadata: %v", err)
	}
	if err := unix.Statfs("/run/g2", &fs); err != nil || fs.Type != unix.TMPFS_MAGIC {
		t.Fatalf("source parent is not tmpfs: %v", err)
	}
	if _, err := os.Lstat(ownerProbeSourcePath); !os.IsNotExist(err) {
		t.Fatalf("source must be absent: %v", err)
	}
	rootTestPinProc(t)
}

func ownerSourceFixture(t *testing.T) int {
	t.Helper()
	if err := os.Mkdir(ownerProbeSourcePath, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, path := range []string{ownerProbeSourcePath, ownerProbeSourcePath + ".held"} {
			_ = os.Chmod(path, 0700)
			_ = os.RemoveAll(path)
		}
	})
	fd, err := unix.Open(ownerProbeSourcePath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func ownerSourceFacts(identity ownerProbeSourceIdentity) *runtimeContainerFacts {
	return &runtimeContainerFacts{mounts: []runtimeMountFact{{typ: "bind", source: ownerProbeSourcePath, destination: ownerProbeTargetPath, propagation: "rprivate", readonly: true}}, ownerProbeSource: identity, hasOwnerProbeSource: true}
}

func TestOwnerProbeSourceCustody(t *testing.T) {
	ownerRootPrerequisites(t)
	for _, tc := range []struct {
		name   string
		change func(*testing.T)
	}{
		{"entry", func(t *testing.T) {
			if err := os.Chmod(ownerProbeSourcePath, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ownerProbeSourcePath, "entry"), []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(ownerProbeSourcePath, 0500); err != nil {
				t.Fatal(err)
			}
		}},
		{"entry-restored", func(t *testing.T) {
			if err := os.Chmod(ownerProbeSourcePath, 0700); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(ownerProbeSourcePath, "entry")
			if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(ownerProbeSourcePath, 0500); err != nil {
				t.Fatal(err)
			}
		}},
		{"replacement", func(t *testing.T) {
			if err := os.Rename(ownerProbeSourcePath, ownerProbeSourcePath+".held"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(ownerProbeSourcePath, 0500); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T) {
			if err := os.Rename(ownerProbeSourcePath, ownerProbeSourcePath+".held"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(ownerProbeSourcePath+".held", ownerProbeSourcePath); err != nil {
				t.Fatal(err)
			}
		}},
		{"mode", func(t *testing.T) {
			if err := os.Chmod(ownerProbeSourcePath, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent-untrusted", func(t *testing.T) {
			if err := os.Chmod("/run/g2", 0777); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod("/run/g2", 0700) })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fd := ownerSourceFixture(t)
			initial, err := readOwnerProbeSourceIdentity()
			if err != nil {
				t.Fatal(err)
			}
			again, err := readOwnerProbeSourceIdentity()
			if err != nil || !sameTrustedStat(initial.stat, again.stat) || initial.mountID != again.mountID {
				t.Fatalf("repeat identity: %v", err)
			}
			facts := ownerSourceFacts(initial)
			tc.change(t)
			if err := verifyRuntimeMountSources(facts, nil); err == nil {
				t.Fatal("changed source accepted")
			}
			var held unix.Stat_t
			if err := unix.Fstat(fd, &held); err != nil {
				t.Fatalf("held descriptor lost: %v", err)
			}
		})
	}
	t.Run("mount-mismatch", func(t *testing.T) {
		ownerSourceFixture(t)
		identity, err := readOwnerProbeSourceIdentity()
		if err != nil {
			t.Fatal(err)
		}
		facts := ownerSourceFacts(identity)
		facts.ownerProbeSource.mountID++
		if err := verifyRuntimeMountSources(facts, nil); err == nil {
			t.Fatal("mount mismatch accepted")
		}
	})
}

func TestOwnerProbeSourceSnapshot(t *testing.T) {
	ownerRootPrerequisites(t)
	for _, change := range []string{"replace", "remove", "missing-baseline", "missing-mount"} {
		t.Run(change, func(t *testing.T) {
			fd := ownerSourceFixture(t)
			f := newLifecycleFixture(t)
			f.prepare(t)
			identity, err := readOwnerProbeSourceIdentity()
			if err != nil {
				t.Fatal(err)
			}
			f.admitted.mounts = ownerSourceFacts(identity).mounts
			f.admitted.ownerProbeSource, f.admitted.hasOwnerProbeSource = identity, true
			before := f.snapshot()
			candidate, err := materializeRuntimeFacts(f.admitted)
			if err != nil || !candidate.hasOwnerProbeSource || candidate.ownerProbeSource != identity {
				t.Fatalf("candidate baseline: %v", err)
			}
			switch change {
			case "replace":
				if err := os.Rename(ownerProbeSourcePath, ownerProbeSourcePath+".held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(ownerProbeSourcePath, 0500); err != nil {
					t.Fatal(err)
				}
			case "remove":
				if err := os.Remove(ownerProbeSourcePath); err != nil {
					t.Fatal(err)
				}
			case "missing-baseline":
				f.admitted.hasOwnerProbeSource = false
			case "missing-mount":
				f.admitted.mounts = nil
			}
			changed := f.snapshot()
			if result, err := materializeRuntimeFacts(f.admitted); err == nil || result != nil {
				t.Fatal("invalid candidate accepted")
			}
			if !reflect.DeepEqual(*f.admitted, changed) {
				t.Fatal("failed materialization modified admitted facts")
			}
			if change == "replace" || change == "remove" {
				if !reflect.DeepEqual(*f.admitted, before) {
					t.Fatal("source mutation changed admitted facts")
				}
			}
			for _, held := range []int{fd, f.dirFD, f.configFD} {
				var st unix.Stat_t
				if err := unix.Fstat(held, &st); err != nil {
					t.Fatalf("caller descriptor lost: %v", err)
				}
			}
		})
	}
	t.Run("ordinary", func(t *testing.T) {
		f := newLifecycleFixture(t)
		f.prepare(t)
		if _, err := materializeRuntimeFacts(f.admitted); err != nil {
			t.Fatalf("ordinary source: %v", err)
		}
	})
}

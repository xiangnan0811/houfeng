//go:build linux

package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"houfeng/db/migrations"
	"houfeng/internal/center/store/migrate"
)

func TestOrdinaryTargetContractStrictWire(t *testing.T) {
	value := ordinaryTargetContract{Version: 1, NativeProtocolVersion: ordinaryNativeProtocolVersion, SchemaVersion: 81, CanonicalMigrationSet: []byte("migration"), CanonicalPrivilegeSet: []byte("privilege")}
	value.SourceIdentitySHA256 = ordinaryContractDigest(value.CanonicalMigrationSet, value.CanonicalPrivilegeSet)
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	got, err := ordinaryDecodeTargetContract(body)
	if err != nil || got.SourceIdentitySHA256 != value.SourceIdentitySHA256 || !bytes.Equal(got.CanonicalMigrationSet, value.CanonicalMigrationSet) {
		t.Fatal("valid contract rejected")
	}
	for name, bad := range map[string][]byte{
		"no newline":              body[:len(body)-1],
		"extra newline":           append(bytes.Clone(body), '\n'),
		"duplicate field":         bytes.Replace(body, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"unknown field":           bytes.Replace(body, []byte(`"version":1`), []byte(`"version":1,"other":1`), 1),
		"changed bytes":           bytes.Replace(body, []byte(`bWlncmF0aW9u`), []byte(`cHJpdmlsZWdl`), 1),
		"missing native protocol": bytes.Replace(body, []byte(`,"native_protocol_version":3`), nil, 1),
		"older native protocol":   bytes.Replace(body, []byte(`"native_protocol_version":3`), []byte(`"native_protocol_version":2`), 1),
		"newer native protocol":   bytes.Replace(body, []byte(`"native_protocol_version":3`), []byte(`"native_protocol_version":4`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ordinaryDecodeTargetContract(bad); err == nil {
				t.Fatal("accepted invalid contract")
			}
		})
	}
	if ordinaryContractDigest([]byte("a"), []byte("bc")) == ordinaryContractDigest([]byte("ab"), []byte("c")) {
		t.Fatal("ambiguous contract framing")
	}
	frame := append([]byte("HOUFENG-ORDINARY-TARGET-CONTRACT-V1\x00"), []byte{0, 0, 0, 11, 0, 0, 0, 1, 'a', 0, 0, 0, 2, 'b', 'c'}...)
	expected := sha256.Sum256(frame)
	if ordinaryContractDigest([]byte("a"), []byte("bc")) != hex.EncodeToString(expected[:]) {
		t.Fatal("incorrect domain-separated framing")
	}
}

func TestOrdinaryDockerPlatformRefusesMutableAndForeignInputs(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("unsupported controller architecture")
	}
	id := "sha256:" + strings.Repeat("a", 64)
	info := ordinaryDockerInfo{OSType: "linux", Architecture: runtime.GOARCH, SecurityOptions: []string{"name=seccomp,profile=default"}}
	image := ordinaryDockerImage{ID: id, OS: "linux", Architecture: runtime.GOARCH, Config: &ordinaryDockerImageConfig{}}
	if _, err := ordinaryDockerPlatform(info, image, id); err != nil {
		t.Fatal("valid image rejected")
	}
	cases := []struct {
		name   string
		mutate func(*ordinaryDockerInfo, *ordinaryDockerImage)
	}{
		{"rootless", func(i *ordinaryDockerInfo, _ *ordinaryDockerImage) { i.SecurityOptions = []string{"name=rootless"} }},
		{"userns", func(i *ordinaryDockerInfo, _ *ordinaryDockerImage) { i.SecurityOptions = []string{"name=userns"} }},
		{"wrong id", func(_ *ordinaryDockerInfo, i *ordinaryDockerImage) { i.ID = "sha256:" + strings.Repeat("b", 64) }},
		{"foreign os", func(_ *ordinaryDockerInfo, i *ordinaryDockerImage) { i.OS = "windows" }},
		{"foreign arch", func(_ *ordinaryDockerInfo, i *ordinaryDockerImage) { i.Architecture = "386" }},
		{"missing config", func(_ *ordinaryDockerInfo, i *ordinaryDockerImage) { i.Config = nil }},
		{"volume", func(_ *ordinaryDockerInfo, i *ordinaryDockerImage) {
			i.Config.Volumes = map[string]json.RawMessage{"/data": []byte(`{}`)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, im := info, image
			config := *image.Config
			im.Config = &config
			tc.mutate(&i, &im)
			if _, err := ordinaryDockerPlatform(i, im, id); err == nil {
				t.Fatal("accepted unsupported image or daemon")
			}
		})
	}
}

func TestOrdinaryImageContainerProvenance(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	makeContainer := func() ordinaryImageContainer {
		var c ordinaryImageContainer
		c.ID, c.Name, c.Image = strings.Repeat("b", 64), "/target-name", id
		c.Config.Image = id
		c.Config.Labels = map[string]string{"houfeng.ordinary.target": "owned-token", "houfeng.ordinary.operation": "project/operation"}
		c.Config.Entrypoint, c.Config.User = []string{"/bin/false"}, "10002:10001"
		c.State.Status, c.State.StartedAt = "created", "0001-01-01T00:00:00Z"
		c.HostConfig.NetworkMode, c.HostConfig.ReadonlyRootfs = "none", true
		c.HostConfig.LogConfig.Type, c.HostConfig.RestartPolicy.Name = "none", "no"
		return c
	}
	if ordinaryValidateImageContainer(makeContainer(), "target-name", "owned-token", "project/operation", id) != nil {
		t.Fatal("valid provenance rejected")
	}
	cases := []struct {
		name   string
		mutate func(*ordinaryImageContainer)
	}{
		{"foreign label", func(c *ordinaryImageContainer) { c.Config.Labels["houfeng.ordinary.target"] = "foreign" }},
		{"foreign operation", func(c *ordinaryImageContainer) { c.Config.Labels["houfeng.ordinary.operation"] = "project/other" }},
		{"foreign name", func(c *ordinaryImageContainer) { c.Name = "/different" }},
		{"different image", func(c *ordinaryImageContainer) { c.Image = "sha256:" + strings.Repeat("c", 64) }},
		{"started then stopped", func(c *ordinaryImageContainer) { c.State.Status = "exited" }},
		{"restart", func(c *ordinaryImageContainer) { c.RestartCount = 1 }},
		{"mount", func(c *ordinaryImageContainer) { c.Mounts = []json.RawMessage{[]byte(`{}`)} }},
		{"network", func(c *ordinaryImageContainer) { c.HostConfig.NetworkMode = "bridge" }},
		{"log driver", func(c *ordinaryImageContainer) { c.HostConfig.LogConfig.Type = "json-file" }},
		{"entrypoint", func(c *ordinaryImageContainer) { c.Config.Entrypoint = []string{"/bin/sh"} }},
		{"writable", func(c *ordinaryImageContainer) { c.HostConfig.ReadonlyRootfs = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := makeContainer()
			tc.mutate(&c)
			if ordinaryValidateImageContainer(c, "target-name", "owned-token", "project/operation", id) == nil {
				t.Fatal("accepted foreign or altered container")
			}
		})
	}
}

func TestOrdinaryTargetContractWriterUsesCurrentCompiler(t *testing.T) {
	var output bytes.Buffer
	if err := WriteOrdinaryTargetContract(&output); err != nil {
		t.Fatal(err)
	}
	got, err := ordinaryDecodeTargetContract(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	migrationsBody, err := migrate.CanonicalMigrationSetFromFS(migrations.FS)
	if err != nil || !bytes.Equal(got.CanonicalMigrationSet, migrationsBody) || got.SchemaVersion != 81 {
		t.Fatal("wire does not contain embedded current migrations")
	}
	privileges, err := migrate.ParseCanonicalPrivilegeSetBodyV2(got.CanonicalPrivilegeSet)
	if err != nil || len(privileges.Privileges) == 0 || len(privileges.RoleBindings) != 3 {
		t.Fatal("wire does not contain current Compose privilege bindings")
	}
	bindings := map[migrate.AppACLSubject]string{}
	for _, role := range privileges.RoleBindings {
		bindings[role.Subject] = role.CatalogRole
	}
	if bindings[migrate.AppACLSubjectCenterRuntime] != composeRuntimeRole || bindings[migrate.AppACLSubjectPlatformAdmin] != composeAdminRole || bindings[migrate.AppACLSubjectRecordDeletionWorker] != composeDeletionWorkerRole {
		t.Fatal("wire has incorrect Compose role bindings")
	}
}

func TestOrdinaryImageRecordImmutableReplay(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership for image records")
	}
	directory := t.TempDir()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	original := ordinaryImageFileRecord{Version: 1, Name: "admin", SHA256: [32]byte{1, 2, 3}}
	if err := os.WriteFile(filepath.Join(directory, ".record-record.json"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryImageRecord(fd, "record.json", original); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryImageRecord(fd, "record.json", original); err != nil {
		t.Fatalf("exact replay failed: %v", err)
	}
	changed := original
	changed.SHA256[0]++
	if err := ordinaryImageRecord(fd, "record.json", changed); err == nil {
		t.Fatal("accepted superseding record")
	}
	body, present, err := ordinaryReadImageRecord(fd, "record.json")
	if err != nil || !present {
		t.Fatal("lost original record")
	}
	var decoded ordinaryImageFileRecord
	if ordinaryReadCanonicalRecord(body, &decoded) != nil || decoded != original {
		t.Fatal("replay changed original record")
	}
	if err := os.Link(filepath.Join(directory, "record.json"), filepath.Join(directory, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ordinaryReadImageRecord(fd, "record.json"); err == nil {
		t.Fatal("accepted multiply linked record")
	}
}

func TestOrdinaryImageRecordRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := os.Symlink("missing", filepath.Join(directory, "record.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ordinaryReadImageRecord(fd, "record.json"); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestOrdinaryImageBeforeCreateOnly(t *testing.T) {
	directory := t.TempDir()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	for _, name := range []string{"intent.json", "create.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("record"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if ordinaryImageBeforeCreateOnly(fd) != nil {
		t.Fatal("clean pre-create state rejected")
	}
	if ordinaryImageKnownEntries(fd, false) != nil {
		t.Fatal("known records rejected")
	}
	if err := os.WriteFile(filepath.Join(directory, "unexpected"), []byte("unknown"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ordinaryImageBeforeCreateOnly(fd) == nil {
		t.Fatal("accepted unknown partial artifact")
	}
	if ordinaryImageKnownEntries(fd, false) == nil {
		t.Fatal("accepted foreign artifact")
	}
}

func TestOrdinaryImageJournalledPartial(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership for image records")
	}
	directory := t.TempDir()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	containerID := strings.Repeat("a", 64)
	name := "houfeng-center"
	temp := ".ordinary-launch-" + strings.Repeat("b", 32)
	if ordinaryImageRecord(fd, "container.json", ordinaryImageContainerRecord{Version: 1, ID: containerID}) != nil {
		t.Fatal("container record failed")
	}
	if ordinaryImageRecord(fd, name+".copy.json", ordinaryImageCopyIntent{Version: 1, Name: name, ContainerID: containerID, Temp: temp}) != nil {
		t.Fatal("copy intent failed")
	}
	if err := os.WriteFile(filepath.Join(directory, temp), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ordinaryImageKnownEntries(fd, false) != nil {
		t.Fatal("journalled partial rejected")
	}
	if ordinaryImageKnownEntries(fd, true) == nil {
		t.Fatal("completed manifest accepted partial")
	}
	if ordinaryRemoveImagePartial(fd, temp) != nil {
		t.Fatal("journalled partial could not be removed")
	}
	if _, err := os.Lstat(filepath.Join(directory, temp)); !os.IsNotExist(err) {
		t.Fatal("partial remains")
	}
	foreign := ".ordinary-launch-" + strings.Repeat("c", 32)
	if err := os.WriteFile(filepath.Join(directory, foreign), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ordinaryImageKnownEntries(fd, false) == nil {
		t.Fatal("accepted unjournalled partial")
	}
}

func TestOrdinaryImageDirectoryJournalRecovery(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned directory creation")
	}
	tests := []struct {
		name      string
		state     string
		operation string
		accepted  bool
	}{
		{"before mkdir", "absent", "op", true},
		{"mkdir before ownership", "empty700", "op", true},
		{"mkdir before chmod", "empty750", "op", true},
		{"foreign contents", "nonempty", "op", false},
		{"symlink", "symlink", "op", false},
		{"different owner", "foreign", "op", false},
		{"writable mode", "unsafe-mode", "op", false},
		{"wrong parent identity", "wrong-parent", "op", false},
		{"other operation", "empty700", "other", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parentPath := t.TempDir()
			fd, err := unix.Open(parentPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil {
				t.Fatal("stat parent")
			}
			intent := ordinaryImageDirectoryIntent{Version: 1, Phase: "PREPARED", ParentDevice: uint64(st.Dev), ParentInode: st.Ino, Name: "ordinary-targets", Project: "project", OperationID: "op", Nonce: strings.Repeat("a", 32)}
			if tc.state == "wrong-parent" {
				intent.ParentInode++
			}
			if ordinaryImageRecord(fd, ".ordinary-targets.mkdir.intent.json", intent) != nil {
				t.Fatal("publish intent")
			}
			child := filepath.Join(parentPath, "ordinary-targets")
			if tc.state == "symlink" {
				if err := os.Symlink(parentPath, child); err != nil {
					t.Fatal(err)
				}
			} else if tc.state != "absent" {
				if err := os.Mkdir(child, 0o700); err != nil {
					t.Fatal(err)
				}
				if tc.state == "empty750" && os.Chmod(child, 0o750) != nil {
					t.Fatal("chmod")
				}
				if tc.state == "unsafe-mode" && os.Chmod(child, 0o777) != nil {
					t.Fatal("chmod")
				}
				if tc.state == "nonempty" && os.WriteFile(filepath.Join(child, "foreign"), []byte("x"), 0o600) != nil {
					t.Fatal("write")
				}
				if tc.state == "foreign" && os.Chown(child, 10002, 10001) != nil {
					t.Fatal("chown")
				}
			}
			err = ordinaryImageMkdirJournalled(fd, "ordinary-targets", "project", tc.operation)
			if (err == nil) != tc.accepted {
				t.Fatalf("recovery acceptance = %v, err %v", err == nil, err)
			}
			if tc.accepted {
				var result unix.Stat_t
				if unix.Lstat(child, &result) != nil || result.Uid != 0 || result.Gid != composeLaunchRuntimeGID || result.Mode&0o7777 != 0o750 {
					t.Fatal("wrong committed directory metadata")
				}
				if ordinaryImageMkdirJournalled(fd, "ordinary-targets", "project", "later") != nil {
					t.Fatal("committed directory replay failed")
				}
			}
		})
	}
}

func TestOrdinaryImageDirectoryRejectsUnrecordedAndReplacement(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned directory creation")
	}
	parentPath := t.TempDir()
	fd, err := unix.Open(parentPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	child := filepath.Join(parentPath, "ordinary-targets")
	if err := os.Mkdir(child, 0o750); err != nil {
		t.Fatal(err)
	}
	if ordinaryImageMkdirJournalled(fd, "ordinary-targets", "project", "op") == nil {
		t.Fatal("accepted pre-existing unknown directory")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if ordinaryImageMkdirJournalled(fd, "ordinary-targets", "project", "op") != nil {
		t.Fatal("initial creation failed")
	}
	other := filepath.Join(parentPath, "replacement")
	if err := os.Mkdir(other, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, child); err != nil {
		t.Fatal(err)
	}
	if ordinaryImageMkdirJournalled(fd, "ordinary-targets", "project", "op") == nil {
		t.Fatal("accepted replaced directory")
	}
}

package config

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// This test uses real process credentials and filesystem ownership. The parent
// must have permission to switch UID/GID; an ordinary host test cannot replace
// the Compose UID 10001/10002 acceptance run.
func TestBirthProducerCredentialStagedOwnership(t *testing.T) {
	const childPathEnv = "HOUFENG_TEST_BIRTH_STAGED_CREDENTIAL"
	const childExpectedEnv = "HOUFENG_TEST_BIRTH_STAGED_EXPECTED"
	if path := os.Getenv(childPathEnv); path != "" {
		if os.Geteuid() != 10001 || os.Getegid() != 10001 {
			t.Fatal("staged credential child did not run as UID/GID 10001")
		}
		value, err := readBirthCredential(path)
		if os.Getenv(childExpectedEnv) == "accepted" {
			if err != nil || value != "fixture-password" {
				t.Fatalf("staged credential was not accepted: %v", err)
			}
		} else if err == nil {
			t.Fatal("incorrect staged credential ownership or permissions were accepted")
		}
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(key, "PG") || key == "SSL_CERT_FILE" || key == "SSL_CERT_DIR" {
				t.Setenv(key, "")
			}
		}
		t.Setenv(birthReaderDatabaseURLEnv, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable")
		t.Setenv(birthReaderIdentityEnv, "/identity-read-at-startup")
		t.Setenv(birthReaderCredentialEnv, path)
		reader, readerErr := LoadBirthReaderConfig(RecordPlatformModeRuntimeAdmission)
		if os.Getenv(childExpectedEnv) == "accepted" {
			if readerErr != nil || reader.Connection == nil || reader.Connection.ConnConfig.Password != "fixture-password" {
				t.Fatalf("staged reader credential was not accepted: %v", readerErr)
			}
		} else if readerErr == nil {
			t.Fatal("reader accepted incorrect staged credential ownership or permissions")
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or an isolated container with permission to switch to UID/GID 10001")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o711); err != nil {
		t.Fatal(err)
	}
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	copyPath := filepath.Join(dir, "config-test")
	copied, err := os.OpenFile(copyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(copied, source); err != nil {
		copied.Close()
		t.Fatal(err)
	}
	if err := copied.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(copyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Open(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	directory, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	credential := filepath.Join(dir, "credential")
	if err := os.WriteFile(credential, []byte("fixture-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		owner    int
		group    int
		mode     os.FileMode
		expected string
	}{
		{name: "staged", owner: 10002, group: 10001, mode: 0o040, expected: "accepted"},
		{name: "different owner", owner: 10003, group: 10001, mode: 0o040},
		{name: "different group", owner: 10002, group: 10003, mode: 0o040},
		{name: "group writable", owner: 10002, group: 10001, mode: 0o060},
		{name: "world readable", owner: 10002, group: 10001, mode: 0o044},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Chown(credential, test.owner, test.group); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(credential, test.mode); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("/proc/self/fd/3", "-test.run=^TestBirthProducerCredentialStagedOwnership$", "-test.count=1")
			command.ExtraFiles = []*os.File{binary, directory}
			command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001, Groups: []uint32{10001}}}
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, childPathEnv+"=") && !strings.HasPrefix(entry, childExpectedEnv+"=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, childPathEnv+"=/proc/self/fd/4/credential", childExpectedEnv+"="+test.expected)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("UID/GID 10001 credential check failed: %v\n%s", err, output)
			}
		})
	}
}

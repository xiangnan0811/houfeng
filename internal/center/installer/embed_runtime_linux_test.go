//go:build linux

package installer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestScriptMissingMinisignRuntimeBehavior(t *testing.T) {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("/run/systemd/system is not available in this test environment")
	}

	tests := []struct {
		name               string
		extraArgs          []string
		wantOutput         string
		wantBootstrap      bool
		wantReleaseAttempt bool
	}{
		{
			name:       "non-interactive default fails before release download",
			extraArgs:  nil,
			wantOutput: "rerun with --install-missing-deps or install minisign manually",
		},
		{
			name:       "explicit opt-out fails before release download",
			extraArgs:  []string{"--no-install-missing-deps"},
			wantOutput: "dependency installation was disabled by --no-install-missing-deps",
		},
		{
			name:               "explicit consent bootstraps minisign before release download",
			extraArgs:          []string{"--install-missing-deps"},
			wantOutput:         "minisign installed to /usr/local/bin/minisign",
			wantBootstrap:      true,
			wantReleaseAttempt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newInstallerRuntimeTestEnv(t)
			output, err := env.run(t, tt.extraArgs...)
			if err == nil {
				t.Fatal("installer should stop in the fake release environment")
			}
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("installer output missing %q:\n%s", tt.wantOutput, output)
			}
			if got := env.exists("minisign-bootstrap-downloaded"); got != tt.wantBootstrap {
				t.Fatalf("minisign bootstrap downloaded = %v, want %v\noutput:\n%s", got, tt.wantBootstrap, output)
			}
			if got := env.exists("release-attempted"); got != tt.wantReleaseAttempt {
				t.Fatalf("release download attempted = %v, want %v\noutput:\n%s", got, tt.wantReleaseAttempt, output)
			}
		})
	}
}

type installerRuntimeTestEnv struct {
	dir     string
	binDir  string
	markers string
	script  string
}

func newInstallerRuntimeTestEnv(t *testing.T) installerRuntimeTestEnv {
	t.Helper()

	dir := t.TempDir()
	env := installerRuntimeTestEnv{
		dir:     dir,
		binDir:  filepath.Join(dir, "bin"),
		markers: filepath.Join(dir, "markers"),
		script:  filepath.Join(dir, "install.sh"),
	}
	mustMkdir(t, env.binDir)
	mustMkdir(t, env.markers)
	mustWriteExecutable(t, env.script, Script)

	env.writeCommand(t, "id", `#!/bin/sh
if [ "$1" = "-u" ]; then
  echo 0
  exit 0
fi
exit 1
`)
	env.writeCommand(t, "uname", `#!/bin/sh
case "$1" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
  *) exit 1 ;;
esac
`)
	env.writeCommand(t, "systemctl", "#!/bin/sh\nexit 0\n")
	env.writeCommand(t, "getent", "#!/bin/sh\nexit 1\n")
	env.writeCommand(t, "groupadd", "#!/bin/sh\nexit 0\n")
	env.writeCommand(t, "useradd", "#!/bin/sh\nexit 0\n")
	env.writeCommand(t, "chown", "#!/bin/sh\nexit 0\n")
	env.writeFakeInstall(t)
	env.writeFakeCurl(t)
	env.writeFakeSHA256Sum(t)
	env.writeFakeTar(t)

	for _, name := range []string{"awk", "cat", "chmod", "cp", "grep", "mkdir", "mktemp", "rm", "touch", "tr"} {
		env.symlinkHostCommand(t, name)
	}

	return env
}

func (env installerRuntimeTestEnv) run(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()

	args := []string{
		env.script,
		"--server-url", "https://center.example.com",
		"--enrollment-token-stdin",
		"--version", "v1.2.3",
		"--release-repo", "owner/repo",
	}
	args = append(args, extraArgs...)

	cmd := exec.Command("/bin/sh", args...)
	cmd.Env = []string{
		"PATH=" + env.binDir,
		"FAKE_INSTALLER_BIN_DIR=" + env.binDir,
		"FAKE_INSTALLER_MARKERS=" + env.markers,
	}
	cmd.Stdin = strings.NewReader("enroll_secret\n")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func (env installerRuntimeTestEnv) exists(name string) bool {
	_, err := os.Stat(filepath.Join(env.markers, name))
	return err == nil
}

func (env installerRuntimeTestEnv) writeCommand(t *testing.T, name, content string) {
	t.Helper()
	mustWriteExecutable(t, filepath.Join(env.binDir, name), content)
}

func (env installerRuntimeTestEnv) writeFakeInstall(t *testing.T) {
	t.Helper()
	env.writeCommand(t, "install", `#!/bin/sh
last=""
prev=""
for arg in "$@"; do
  prev="$last"
  last="$arg"
done
if [ "$last" = "/usr/local/bin/minisign" ]; then
  cp "$prev" "$FAKE_INSTALLER_BIN_DIR/minisign"
  chmod 0755 "$FAKE_INSTALLER_BIN_DIR/minisign"
  touch "$FAKE_INSTALLER_MARKERS/minisign-installed"
fi
exit 0
`)
}

func (env installerRuntimeTestEnv) writeFakeCurl(t *testing.T) {
	t.Helper()
	env.writeCommand(t, "curl", `#!/bin/sh
dest=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      dest="$2"
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
printf '%s\n' "$url" >> "$FAKE_INSTALLER_MARKERS/curl.log"
case "$url" in
  *minisign-0.12-linux.tar.gz)
    touch "$FAKE_INSTALLER_MARKERS/minisign-bootstrap-downloaded"
    printf 'fake minisign tarball' > "$dest"
    ;;
  *houfeng-agent_*)
    touch "$FAKE_INSTALLER_MARKERS/release-attempted"
    echo "release asset download blocked by test" >&2
    exit 77
    ;;
  *)
    printf 'fake' > "$dest"
    ;;
esac
`)
}

func (env installerRuntimeTestEnv) writeFakeSHA256Sum(t *testing.T) {
	t.Helper()
	env.writeCommand(t, "sha256sum", `#!/bin/sh
case "$1" in
  *minisign-0.12-linux.tar.gz)
    printf '%s  %s\n' '9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73' "$1"
    ;;
  *)
    printf '%s  %s\n' '0000000000000000000000000000000000000000000000000000000000000000' "$1"
    ;;
esac
`)
}

func (env installerRuntimeTestEnv) writeFakeTar(t *testing.T) {
	t.Helper()
	env.writeCommand(t, "tar", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-C" ]; then
    out="$2"
    shift 2
  else
    shift
  fi
done
[ -n "$out" ] || exit 1
mkdir -p "$out/minisign-linux/x86_64"
cat > "$out/minisign-linux/x86_64/minisign" <<'EOF_MINISIGN'
#!/bin/sh
echo "fake minisign"
exit 0
EOF_MINISIGN
chmod 0755 "$out/minisign-linux/x86_64/minisign"
`)
}

func (env installerRuntimeTestEnv) symlinkHostCommand(t *testing.T, name string) {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("find host command %s: %v", name, err)
	}
	if err := os.Symlink(path, filepath.Join(env.binDir, name)); err != nil {
		t.Fatalf("symlink host command %s: %v", name, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func TestScriptMissingDependencyFlagsAreMutuallyExclusiveAtRuntime(t *testing.T) {
	env := newInstallerRuntimeTestEnv(t)
	output, err := env.run(t, "--install-missing-deps", "--no-install-missing-deps")
	if err == nil {
		t.Fatal("installer should reject mutually exclusive dependency flags")
	}
	want := "--install-missing-deps and --no-install-missing-deps are mutually exclusive"
	if !strings.Contains(output, want) {
		t.Fatalf("installer output missing %q:\n%s", want, output)
	}
}
